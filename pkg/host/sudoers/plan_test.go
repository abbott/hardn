package sudoers_test

import (
	"context"
	"errors"
	"io/fs"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/sudoers"
)

var (
	sudoChoice = sudoers.Choose(debian, sudoers.ToolUnknown)
	rsChoice   = sudoers.Choose(ubuntu, sudoers.ToolUnknown)
	doasChoice = sudoers.Choose(alpine, sudoers.ToolUnknown)
	keep       = []string{"HARDN_CONFIG"}
)

// TestPlanContent: the files and their exact content, per tool and grant.
// sudo's lines are origin/main's (ConfigureSudo, setup-sudo-env), each in
// its own file; doas's rule is 20-wheel.conf's shape, one file per user.
func TestPlanContent(t *testing.T) {
	for _, tc := range []struct {
		name   string
		c      sudoers.Choice
		pol    sudoers.Policy
		files  map[string]string
		risks  []sudoers.Risk
		checks []string
	}{
		{"sudo password", sudoChoice, sudoers.Policy{User: "alice", Grant: sudoers.GrantPassword},
			map[string]string{"/etc/sudoers.d/hardn-grant-alice": "# Managed by hardn\nalice ALL=(ALL) ALL\n"},
			[]sudoers.Risk{sudoers.RiskLockout}, []string{"visudo -cf /etc/sudoers.d/hardn-grant-alice"}},
		{"sudo no password, env", sudoChoice, sudoers.Policy{User: "alice", Grant: sudoers.GrantNoPassword, Env: keep},
			map[string]string{
				"/etc/sudoers.d/hardn-grant-alice": "# Managed by hardn\nalice ALL=(ALL) NOPASSWD: ALL\n",
				"/etc/sudoers.d/hardn-env-alice":   "# Managed by hardn\nDefaults:alice env_keep += \"HARDN_CONFIG\"\n",
			},
			[]sudoers.Risk{sudoers.RiskNone, sudoers.RiskNone},
			[]string{"visudo -cf /etc/sudoers.d/hardn-grant-alice", "visudo -cf /etc/sudoers.d/hardn-env-alice"}},
		{"sudo env only, two variables", sudoChoice, sudoers.Policy{User: "alice.smith", Env: []string{"HARDN_CONFIG", "HARDN_ASCII"}},
			map[string]string{"/etc/sudoers.d/hardn-env-alice_2esmith": "# Managed by hardn\nDefaults:alice.smith env_keep += \"HARDN_CONFIG HARDN_ASCII\"\n"},
			[]sudoers.Risk{sudoers.RiskNone}, []string{"visudo -cf /etc/sudoers.d/hardn-env-alice_2esmith"}},
		{"sudo-rs is the same", rsChoice, sudoers.Policy{User: "alice", Grant: sudoers.GrantPassword},
			map[string]string{"/etc/sudoers.d/hardn-grant-alice": "# Managed by hardn\nalice ALL=(ALL) ALL\n"},
			[]sudoers.Risk{sudoers.RiskLockout}, []string{"visudo -cf /etc/sudoers.d/hardn-grant-alice"}},
		{"doas password, env", doasChoice, sudoers.Policy{User: "alice", Grant: sudoers.GrantPassword, Env: keep},
			map[string]string{"/etc/doas.d/hardn-alice.conf": "# Managed by hardn\npermit persist setenv { HARDN_CONFIG } alice\n"},
			[]sudoers.Risk{sudoers.RiskLockout}, []string{"doas -C /etc/doas.d/hardn-alice.conf"}},
		{"doas no password", doasChoice, sudoers.Policy{User: "alice", Grant: sudoers.GrantNoPassword},
			map[string]string{"/etc/doas.d/hardn-alice.conf": "# Managed by hardn\npermit nopass alice\n"},
			[]sudoers.Risk{sudoers.RiskNone}, []string{"doas -C /etc/doas.d/hardn-alice.conf"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := sudoHost(t)
			if tc.c.Tool == sudoers.ToolDoas {
				h = doasHost(t)
			}
			_, p := plan(t, h, tc.c, tc.pol)
			if len(p.Changes) != len(tc.files) {
				t.Fatalf("changes = %+v", p.Changes)
			}
			for i, c := range p.Changes {
				want := sudoers.SudoMode
				if tc.c.Tool == sudoers.ToolDoas {
					want = sudoers.DoasMode
				}
				if string(c.Data) != tc.files[c.Path] || c.Mode != want || c.Remove || c.Risk != tc.risks[i] || strings.Join(c.Check, " ") != tc.checks[i] {
					t.Errorf("change %d = %s %q %04o risk %s check %q", i, c.Path, c.Data, uint32(c.Mode), c.Risk, c.Check)
				}
			}
			for _, c := range h.Calls() {
				if c.Op != "read" && c.Op != "read-nofollow" && c.Op != "stat" && c.Op != "lstat" && c.Op != "readdir" {
					t.Errorf("Read made %s", c)
				}
			}
		})
	}
}

// TestPlanFindings: files sudo reads after hardn's (the last match wins)
// are reported, hardn's own files and the names sudo skips are not, and
// origin/main's /etc/sudoers.d/<user> is reported as legacy (V16). Lines
// of /etc/sudoers after the includedir are reported unless they are
// comments; "#1000" there is a rule for uid 1000, not a comment.
func TestPlanFindings(t *testing.T) {
	h := sudoHost(t)
	for _, n := range []string{"alice", "zz-local", "local.conf", "old~", "hardn-grant-bob", "hardn-zzz", "hardn-grant-z_61"} {
		h.AddFile(sudoers.DropInDir+"/"+n, []byte("# x\n"), 0o440, 0, 0)
	}
	h.AddFile(sudoers.MainConfig, []byte(debianMain+"# a comment\n\nbob ALL=(ALL) ALL\n#include /etc/sudoers.local\n#1000 ALL=(ALL) !ALL\n# 1000 is alice\n#\n"), 0o440, 0, 0)
	st, p := plan(t, h, sudoChoice, sudoers.Policy{User: "alice", Grant: sudoers.GrantPassword}, sudoers.Policy{User: "bob", Env: keep})
	wantEntries := "90-cloud-init-users README alice hardn-grant-bob hardn-grant-z_61 hardn-zzz zz-local"
	if got := strings.Join(st.Entries, " "); got != wantEntries || st.Main != sudoers.MainConfig {
		t.Errorf("Entries = %q, Main %s, want %q", got, st.Main, wantEntries)
	}
	after := "sudoers-after-include /etc/sudoers read after the drop-ins: a rule here that matches the user wins over hardn's"
	later := " read after hardn's rule for alice, bob: a rule there that matches wins (the last match wins)"
	want := []string{
		after, after, after,
		"sudoers-legacy-file /etc/sudoers.d/alice /etc/sudoers.d/alice is from an earlier hardn; hardn's rule wins",
		"sudoers-later-file /etc/sudoers.d/hardn-grant-z_61" + later,
		"sudoers-later-file /etc/sudoers.d/hardn-zzz" + later,
		"sudoers-later-file /etc/sudoers.d/zz-local" + later,
	}
	if got := findings(p); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("findings:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if p.Findings[0].Line != 8 || p.Findings[1].Line != 9 || p.Findings[2].Line != 10 {
		t.Errorf("after-include lines = %d, %d, %d, want 8, 9, 10", p.Findings[0].Line, p.Findings[1].Line, p.Findings[2].Line)
	}
}

func findings(p sudoers.Plan) []string {
	var out []string
	for _, f := range p.Findings {
		out = append(out, f.Kind.String()+" "+f.File+" "+f.Detail)
	}
	return out
}

// TestPlanFindingsPolicies: every policy is scanned. A later file is
// reported against the first of hardn's files that will hold a rule for
// the user (the environment file sorts before the grant); a policy that
// removes hardn's files has no rule for a later file to override, but a
// legacy file's grant outlives it, as it does any policy that grants
// nothing; a legacy file read after hardn's rule may override it. The
// legacy file's detail is in the user's words (next/s9-control). doas's
// later files are its *.conf names, hardn's own decoded as on sudo.
func TestPlanFindingsPolicies(t *testing.T) {
	const laterAlice = " read after hardn's rule for alice: a rule there that matches wins (the last match wins)"
	for _, tc := range []struct {
		name    string
		c       sudoers.Choice
		entries []string
		pol     sudoers.Policy
		want    []string
	}{
		{"removal: the legacy grant still applies", sudoChoice, []string{"alice", "zz-local"}, sudoers.Policy{User: "alice"}, []string{
			"sudoers-legacy-file /etc/sudoers.d/alice /etc/sudoers.d/alice may still let alice use sudo",
		}},
		{"environment only", sudoChoice, []string{"alice", "zz-local"}, sudoers.Policy{User: "alice", Env: keep}, []string{
			"sudoers-legacy-file /etc/sudoers.d/alice /etc/sudoers.d/alice may still let alice use sudo",
			"sudoers-later-file /etc/sudoers.d/zz-local" + laterAlice,
		}},
		{"grant and environment: from the environment file", sudoChoice, []string{"hardn-f"}, alicePolicy, []string{
			"sudoers-later-file /etc/sudoers.d/hardn-f" + laterAlice,
		}},
		{"grant only: not before the grant", sudoChoice, []string{"hardn-f"}, sudoers.Policy{User: "alice", Grant: sudoers.GrantPassword}, nil},
		{"legacy file read after hardn's", sudoChoice, []string{"svc-alice"}, sudoers.Policy{User: "svc-alice", Grant: sudoers.GrantPassword}, []string{
			"sudoers-legacy-file /etc/sudoers.d/svc-alice /etc/sudoers.d/svc-alice may override hardn's rule",
		}},
		{"legacy file read after hardn's, no grant", sudoChoice, []string{"svc-alice"}, sudoers.Policy{User: "svc-alice", Env: keep}, []string{
			"sudoers-legacy-file /etc/sudoers.d/svc-alice /etc/sudoers.d/svc-alice may override hardn's rule and still let svc-alice use sudo",
		}},
		{"doas", doasChoice, []string{"10-local.conf", "hardn-bob.conf", "hardn-x_zz.conf", "zz-local.conf", "zz-local"}, sudoers.Policy{User: "alice", Grant: sudoers.GrantPassword}, []string{
			"sudoers-later-file /etc/doas.d/hardn-x_zz.conf" + laterAlice,
			"sudoers-later-file /etc/doas.d/zz-local.conf" + laterAlice,
		}},
		{"doas removal", doasChoice, []string{"zz-local.conf"}, sudoers.Policy{User: "alice"}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, dir := sudoHost(t), sudoers.DropInDir
			if tc.c.Tool == sudoers.ToolDoas {
				h, dir = doasHost(t), sudoers.DoasDir
			}
			for _, n := range tc.entries {
				h.AddFile(dir+"/"+n, []byte("# x\n"), 0o440, 0, 0)
			}
			_, p := plan(t, h, tc.c, tc.pol)
			if got := findings(p); strings.Join(got, "\n") != strings.Join(tc.want, "\n") {
				t.Errorf("findings:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(tc.want, "\n"))
			}
		})
	}
}

// TestReadMain: sudo-rs reads /etc/sudoers-rs instead of /etc/sudoers when
// it exists (by stat), so that is the file that must include the drop-in
// directory and whose later lines are reported; the sudo project's sudo
// never reads it.
func TestReadMain(t *testing.T) {
	const rsMain = "@includedir /etc/sudoers.d\nalice ALL=(ALL) !ALL\n"
	for _, tc := range []struct {
		name  string
		c     sudoers.Choice
		setup func(*hostfake.System)
		main  string
	}{
		{"sudo-rs, no sudoers-rs", rsChoice, func(*hostfake.System) {}, sudoers.MainConfig},
		{"sudo-rs, sudoers-rs", rsChoice, func(h *hostfake.System) {
			h.AddFile(sudoers.SudoersRS, []byte(rsMain), 0o440, 0, 0)
		}, sudoers.SudoersRS},
		{"sudo-rs, sudoers-rs a symlink to a file", rsChoice, func(h *hostfake.System) {
			h.AddFile("/etc/sudoers-rs.real", []byte(rsMain), 0o440, 0, 0)
			h.AddSymlink(sudoers.SudoersRS, "/etc/sudoers-rs.real")
		}, sudoers.SudoersRS},
		{"sudo-rs, sudoers-rs a dangling symlink", rsChoice, func(h *hostfake.System) {
			h.AddSymlink(sudoers.SudoersRS, "/etc/missing")
		}, sudoers.MainConfig},
		{"sudo, sudoers-rs ignored", sudoChoice, func(h *hostfake.System) {
			h.AddFile(sudoers.SudoersRS, []byte("root ALL=(ALL:ALL) ALL\n"), 0o440, 0, 0)
		}, sudoers.MainConfig},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := sudoHost(t)
			tc.setup(h)
			st, p := plan(t, h, tc.c, sudoers.Policy{User: "alice", Grant: sudoers.GrantPassword})
			if st.Main != tc.main || p.Main != tc.main {
				t.Errorf("Main = %s, %s, want %s", st.Main, p.Main, tc.main)
			}
			if tc.main == sudoers.SudoersRS && (len(p.Findings) != 1 || p.Findings[0].File != sudoers.SudoersRS || p.Findings[0].Line != 2) {
				t.Errorf("findings = %+v, want line 2 of %s", p.Findings, sudoers.SudoersRS)
			}
		})
	}
}

// TestPlanIdempotent: once installed, the same policies plan nothing; a
// file with the right content and the wrong mode or owner is rewritten.
func TestPlanIdempotent(t *testing.T) {
	for _, c := range []sudoers.Choice{sudoChoice, doasChoice} {
		h := sudoHost(t)
		if c.Tool == sudoers.ToolDoas {
			h = doasHost(t)
		}
		pol := sudoers.Policy{User: "alice", Grant: sudoers.GrantNoPassword, Env: keep}
		_, p := plan(t, h, c, pol)
		if _, err := sudoers.Apply(context.Background(), h, p, sudoers.Options{RunDir: runDir}); err != nil {
			t.Fatal(err)
		}
		if _, p2 := plan(t, h, c, pol); len(p2.Changes) != 0 {
			t.Errorf("%s: second plan = %+v", c.Tool, p2.Changes)
		}
		e, _ := h.Entry(p.Changes[0].Path)
		h.AddFile(p.Changes[0].Path, e.Data, 0o644, 0, 0)
		if _, p3 := plan(t, h, c, pol); len(p3.Changes) != 1 || p3.Changes[0].Path != p.Changes[0].Path {
			t.Errorf("%s: plan after chmod = %+v", c.Tool, p3.Changes)
		}
	}
}

// TestPlanRemove: GrantNone and no Env remove hardn's files, and removing
// a grant is a lockout risk; nothing else is touched.
func TestPlanRemove(t *testing.T) {
	h := sudoHost(t)
	h.AddFile(sudoers.DropInDir+"/hardn-grant-alice", []byte("# Managed by hardn\nalice ALL=(ALL) ALL\n"), 0o440, 0, 0)
	h.AddFile(sudoers.DropInDir+"/hardn-env-alice", []byte("x\n"), 0o440, 0, 0)
	_, p := plan(t, h, sudoChoice, sudoers.Policy{User: "alice"}, sudoers.Policy{User: "bob"})
	if len(p.Changes) != 2 || !p.Changes[0].Remove || p.Changes[0].Risk != sudoers.RiskLockout ||
		!p.Changes[1].Remove || p.Changes[1].Risk != sudoers.RiskNone || p.Changes[0].Check != nil {
		t.Fatalf("changes = %+v", p.Changes)
	}
	d := doasHost(t)
	d.AddFile(sudoers.DoasDir+"/hardn-alice.conf", []byte("permit persist alice\n"), 0o640, 0, 0)
	_, p = plan(t, d, doasChoice, sudoers.Policy{User: "alice"}, sudoers.Policy{User: "wheel"})
	if len(p.Changes) != 1 || p.Changes[0].Path != "/etc/doas.d/hardn-alice.conf" || !p.Changes[0].Remove || p.Changes[0].Risk != sudoers.RiskLockout {
		t.Fatalf("doas changes = %+v", p.Changes)
	}
}

func TestPlanRefuses(t *testing.T) {
	h := sudoHost(t)
	st, err := sudoers.Read(context.Background(), h, sudoChoice, []string{"alice"})
	if err != nil {
		t.Fatal(err)
	}
	d := doasHost(t)
	dst, err := sudoers.Read(context.Background(), d, doasChoice, []string{"alice"})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		st   sudoers.State
		ps   []sudoers.Policy
		want error
	}{
		{"doas env without a grant", dst, []sudoers.Policy{{User: "alice", Env: keep}}, sudoers.ErrEnvNeedsGrant},
		{"bad variable", st, []sudoers.Policy{{User: "alice", Env: []string{"HARDN CONFIG"}}}, sudoers.ErrVarName},
		{"repeated variable", st, []sudoers.Policy{{User: "alice", Env: []string{"A", "A"}}}, sudoers.ErrVarName},
		{"bad user", st, []sudoers.Policy{{User: "ALL", Grant: sudoers.GrantPassword}}, sudoers.ErrUserName},
		{"user not read", st, []sudoers.Policy{{User: "bob", Grant: sudoers.GrantPassword}}, fs.ErrInvalid},
		{"repeated user", st, []sudoers.Policy{{User: "alice"}, {User: "alice"}}, fs.ErrInvalid},
		{"unknown grant", st, []sudoers.Policy{{User: "alice", Grant: 9}}, fs.ErrInvalid},
		{"no tool", sudoers.State{}, []sudoers.Policy{{User: "alice"}}, fs.ErrInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, err := sudoers.PlanFor(tc.st, tc.ps)
			if !errors.Is(err, tc.want) || len(p.Changes) != 0 {
				t.Errorf("PlanFor = %+v, %v, want %v", p.Changes, err, tc.want)
			}
		})
	}
}

// TestReadRefuses: a drop-in directory the tool does not read is an
// error, not an empty plan: a grant there would have no effect.
func TestReadRefuses(t *testing.T) {
	for _, tc := range []struct {
		name  string
		c     sudoers.Choice
		setup func(*hostfake.System)
		want  error
	}{
		{"no includedir", sudoChoice, func(h *hostfake.System) {
			h.AddFile(sudoers.MainConfig, []byte("root ALL=(ALL:ALL) ALL\n# includedir /etc/sudoers.d\n"), 0o440, 0, 0)
		}, sudoers.ErrNotIncluded},
		{"includedir of another directory", sudoChoice, func(h *hostfake.System) {
			h.AddFile(sudoers.MainConfig, []byte("@includedir /etc/sudoers.local\n"), 0o440, 0, 0)
		}, sudoers.ErrNotIncluded},
		{"no sudoers.d", sudoChoice, func(h *hostfake.System) {
			if err := h.RemoveAll(context.Background(), sudoers.DropInDir); err != nil {
				t.Fatal(err)
			}
		}, sudoers.ErrNotIncluded},
		{"no sudoers", sudoChoice, func(h *hostfake.System) {
			if err := h.Remove(context.Background(), sudoers.MainConfig); err != nil {
				t.Fatal(err)
			}
		}, fs.ErrNotExist},
		{"symlink at hardn's file", sudoChoice, func(h *hostfake.System) {
			h.AddSymlink(sudoers.DropInDir+"/hardn-grant-alice", "/etc/shadow")
		}, host.ErrSymlink},
		{"directory at hardn's file", sudoChoice, func(h *hostfake.System) {
			h.AddDir(sudoers.DropInDir+"/hardn-env-alice", 0o755, 0, 0)
		}, nil},
		{"no doas.d", doasChoice, func(h *hostfake.System) {
			if err := h.RemoveAll(context.Background(), sudoers.DoasDir); err != nil {
				t.Fatal(err)
			}
		}, sudoers.ErrNotIncluded},
		{"doas.d a symlink", doasChoice, func(h *hostfake.System) {
			if err := h.RemoveAll(context.Background(), sudoers.DoasDir); err != nil {
				t.Fatal(err)
			}
			h.AddDir("/etc/doas.real", 0o750, 0, 0)
			h.AddSymlink(sudoers.DoasDir, "/etc/doas.real")
		}, sudoers.ErrNotIncluded},
		{"not applicable", sudoers.Choose(proxmox, sudoers.ToolUnknown), func(*hostfake.System) {}, sudoers.ErrNoTool},
		{"bad user", sudoChoice, func(*hostfake.System) {}, sudoers.ErrUserName},
		// /etc/sudoers includes the directory; sudo-rs reads sudoers-rs.
		{"sudo-rs: sudoers-rs without the includedir", rsChoice, func(h *hostfake.System) {
			h.AddFile(sudoers.SudoersRS, []byte("root ALL=(ALL:ALL) ALL\n"), 0o440, 0, 0)
		}, sudoers.ErrNotIncluded},
		{"sudoers-rs, flavour not established", sudoers.Choice{Tool: sudoers.ToolSudo}, func(h *hostfake.System) {
			h.AddFile(sudoers.SudoersRS, []byte(debianMain), 0o440, 0, 0)
		}, sudoers.ErrNotIncluded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := sudoHost(t)
			if tc.c.Tool == sudoers.ToolDoas {
				h = doasHost(t)
			}
			tc.setup(h)
			users := []string{"alice"}
			if tc.name == "bad user" {
				users = []string{"Alice"}
			}
			_, err := sudoers.Read(context.Background(), h, tc.c, users)
			if err == nil || tc.want != nil && !errors.Is(err, tc.want) {
				t.Errorf("Read = %v, want %v", err, tc.want)
			}
		})
	}
}
