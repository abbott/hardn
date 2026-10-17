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
// origin/main's /etc/sudoers.d/<user> is reported as legacy (V16).
func TestPlanFindings(t *testing.T) {
	h := sudoHost(t)
	for _, n := range []string{"alice", "zz-local", "local.conf", "old~", "hardn-grant-bob", "hardn-zzz", "hardn-grant-z_61"} {
		h.AddFile(sudoers.DropInDir+"/"+n, []byte("# x\n"), 0o440, 0, 0)
	}
	h.AddFile(sudoers.MainConfig, []byte(debianMain+"# a comment\n\nbob ALL=(ALL) ALL\n#include /etc/sudoers.local\n"), 0o440, 0, 0)
	st, p := plan(t, h, sudoChoice, sudoers.Policy{User: "alice", Grant: sudoers.GrantPassword}, sudoers.Policy{User: "zed", Env: keep})
	wantEntries := "90-cloud-init-users README alice hardn-grant-bob hardn-grant-z_61 hardn-zzz zz-local"
	if got := strings.Join(st.Entries, " "); got != wantEntries {
		t.Errorf("Entries = %q, want %q", got, wantEntries)
	}
	var got []string
	for _, f := range p.Findings {
		got = append(got, f.Kind.String()+" "+f.File+" "+f.Detail)
	}
	want := []string{
		"sudoers-after-include /etc/sudoers read after the drop-ins: a rule here that matches the user wins over hardn's",
		"sudoers-after-include /etc/sudoers read after the drop-ins: a rule here that matches the user wins over hardn's",
		"sudoers-legacy-file /etc/sudoers.d/alice origin/main's file for alice (V16); hardn's rule is in hardn-grant-alice; sudo reads it before hardn's: hardn's rule wins",
		"sudoers-later-file /etc/sudoers.d/hardn-grant-z_61 read after hardn's rule for alice: a rule there that matches wins (the last match wins)",
		"sudoers-later-file /etc/sudoers.d/hardn-zzz read after hardn's rule for alice: a rule there that matches wins (the last match wins)",
		"sudoers-later-file /etc/sudoers.d/zz-local read after hardn's rule for alice: a rule there that matches wins (the last match wins)",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("findings:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if p.Findings[0].Line != 8 || p.Findings[1].Line != 9 {
		t.Errorf("after-include lines = %d, %d, want 8, 9", p.Findings[0].Line, p.Findings[1].Line)
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
