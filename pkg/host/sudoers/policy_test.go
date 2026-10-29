package sudoers_test

import (
	"context"
	"errors"
	"io/fs"
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/sudoers"
)

// hostFor is a host of choice c's tool, with no file of hardn's.
func hostFor(t *testing.T, c sudoers.Choice) *hostfake.System {
	if c.Tool == sudoers.ToolDoas {
		return doasHost(t)
	}
	return sudoHost(t)
}

// install plans pol on h and applies it, as hardn writes it.
func install(t *testing.T, h *hostfake.System, c sudoers.Choice, pol sudoers.Policy) {
	t.Helper()
	_, p := plan(t, h, c, pol)
	if _, err := sudoers.Apply(context.Background(), h, p, opts); err != nil {
		t.Fatalf("Apply %+v: %v", pol, err)
	}
}

// readBack reads h for user and returns the Policy its files hold.
func readBack(t *testing.T, h *hostfake.System, c sudoers.Choice, user string) (sudoers.State, sudoers.Policy, error) {
	t.Helper()
	st, err := sudoers.Read(context.Background(), h, c, []string{user})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	pol, err := sudoers.PolicyOf(st, user)
	return st, pol, err
}

// policies are every Policy PlanFor takes for alice on c's tool, over
// lists of variables: each grant with each list (doas refuses variables
// without a grant).
func policies(c sudoers.Choice, lists [][]string) []sudoers.Policy {
	var out []sudoers.Policy
	for _, g := range []sudoers.Grant{sudoers.GrantNone, sudoers.GrantPassword, sudoers.GrantNoPassword} {
		for _, env := range lists {
			if c.Tool == sudoers.ToolDoas && g == sudoers.GrantNone && len(env) > 0 {
				continue
			}
			out = append(out, sudoers.Policy{User: "alice", Grant: g, Env: env})
		}
	}
	return out
}

// varLists are random lists of variable names, one to six each, every name
// one PlanFor takes and none repeated in a list; seeded, so a failure is
// the same on every run.
func varLists(n int) [][]string {
	const first = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz_"
	const rest = first + "0123456789"
	r := rand.New(rand.NewPCG(9, 29))
	var out [][]string
	for range n {
		var list []string
		for len(list) < 1+r.IntN(6) {
			b := []byte{first[r.IntN(len(first))]}
			for range r.IntN(12) {
				b = append(b, rest[r.IntN(len(rest))])
			}
			if !slices.Contains(list, string(b)) {
				list = append(list, string(b))
			}
		}
		out = append(out, list)
	}
	return out
}

// samePolicy: a and b are the same Policy, the variables in order.
func samePolicy(a, b sudoers.Policy) bool {
	return a.User == b.User && a.Grant == b.Grant && slices.Equal(a.Env, b.Env)
}

var tools = map[string]sudoers.Choice{"sudo": sudoChoice, "sudo-rs": rsChoice, "doas": doasChoice}

// TestPolicyOf: for every tool (sudo, sudo-rs, doas), grant and list of
// variables hardn writes (none, one, several, and random lists), the files
// Apply wrote read back as the Policy that wrote them, which plans nothing
// again; the variables in their order.
func TestPolicyOf(t *testing.T) {
	lists := append([][]string{nil, keep, {"HARDN_CONFIG", "EDITOR"}, {"EDITOR", "HARDN_CONFIG", "LANG", "_x1"}}, varLists(24)...)
	for name, c := range tools {
		for _, pol := range policies(c, lists) {
			h := hostFor(t, c)
			install(t, h, c, pol)
			st, got, err := readBack(t, h, c, pol.User)
			if err != nil || !samePolicy(got, pol) {
				t.Errorf("%s %+v: read back %+v, %v", name, pol, got, err)
				continue
			}
			if p, err := sudoers.PlanFor(st, []sudoers.Policy{got}); err != nil || len(p.Changes) != 0 {
				t.Errorf("%s %+v: the Policy read plans %+v, %v", name, pol, p.Changes, err)
			}
		}
	}
}

// TestPolicyOfAfterChanges: from any Policy hardn wrote to any other, the
// files read back as the last one, which plans nothing again.
func TestPolicyOfAfterChanges(t *testing.T) {
	lists := [][]string{nil, keep, {"EDITOR"}, {"HARDN_CONFIG", "EDITOR", "LANG"}}
	for name, c := range tools {
		pols := policies(c, lists)
		for _, from := range pols {
			for _, to := range pols {
				h := hostFor(t, c)
				install(t, h, c, from)
				install(t, h, c, to)
				st, got, err := readBack(t, h, c, "alice")
				if err != nil || !samePolicy(got, to) {
					t.Errorf("%s %+v then %+v: read back %+v, %v", name, from, to, got, err)
					continue
				}
				if p, err := sudoers.PlanFor(st, []sudoers.Policy{got}); err != nil || len(p.Changes) != 0 {
					t.Errorf("%s %+v then %+v: plans %+v, %v", name, from, to, p.Changes, err)
				}
			}
		}
	}
}

// TestPolicyOfCarries: a change of one part of the Policy read carries the
// other. Slice 9 adds HARDN_CONFIG to a granted user's variables: the
// environment file alone is written, the grant left as it is; slice 5
// grants a user whose variables hardn keeps: the grant file alone. With an
// empty Policy for the part not changed, each would remove the other's
// file (gallery-environment.md §3.2).
func TestPolicyOfCarries(t *testing.T) {
	const grant, env = "/etc/sudoers.d/hardn-grant-alice", "/etc/sudoers.d/hardn-env-alice"
	for _, tc := range []struct {
		name   string
		before sudoers.Policy
		change func(*sudoers.Policy)
		write  string // the one file the change writes
		alone  sudoers.Policy
		lost   string // the file the part not carried removes
	}{
		{"slice 9: keep HARDN_CONFIG", sudoers.Policy{User: "alice", Grant: sudoers.GrantNoPassword, Env: []string{"EDITOR"}},
			func(p *sudoers.Policy) { p.Env = append(p.Env, "HARDN_CONFIG") }, env,
			sudoers.Policy{User: "alice", Env: []string{"EDITOR", "HARDN_CONFIG"}}, grant},
		{"slice 5: grant", sudoers.Policy{User: "alice", Env: keep},
			func(p *sudoers.Policy) { p.Grant = sudoers.GrantNoPassword }, grant,
			sudoers.Policy{User: "alice", Grant: sudoers.GrantNoPassword}, env},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := sudoHost(t)
			install(t, h, sudoChoice, tc.before)
			st, pol, err := readBack(t, h, sudoChoice, "alice")
			if err != nil {
				t.Fatal(err)
			}
			tc.change(&pol)
			p, err := sudoers.PlanFor(st, []sudoers.Policy{pol})
			if err != nil || len(p.Changes) != 1 || p.Changes[0].Path != tc.write || p.Changes[0].Remove || p.Changes[0].Risk != sudoers.RiskNone {
				t.Errorf("carried: %+v, %v; want %s alone written", p.Changes, err, tc.write)
			}
			p, err = sudoers.PlanFor(st, []sudoers.Policy{tc.alone})
			if err != nil || !slices.ContainsFunc(p.Changes, func(c sudoers.Change) bool { return c.Path == tc.lost && c.Remove }) {
				t.Errorf("not carried: %+v, %v; want %s removed", p.Changes, err, tc.lost)
			}
		})
	}
}

// TestPolicyOfEdited: a file of hardn's holding anything PlanFor does not
// write for some Policy is an EditedError naming it, never a Policy: a
// rule in another form, a comment or a rule added, the header or the
// newline missing, another user's rule, an empty list, a name repeated or
// not a variable's, doas options hardn never writes.
func TestPolicyOfEdited(t *testing.T) {
	const (
		grant = "/etc/sudoers.d/hardn-grant-alice"
		env   = "/etc/sudoers.d/hardn-env-alice"
		rule  = "/etc/doas.d/hardn-alice.conf"
		hdr   = sudoers.Header
	)
	for _, tc := range []struct {
		name, path, data string
	}{
		{"sudo: ALL=(ALL:ALL)", grant, hdr + "alice ALL=(ALL:ALL) ALL\n"},
		{"sudo: a comment added", grant, hdr + "alice ALL=(ALL) ALL\n# kept by hand\n"},
		{"sudo: no header", grant, "alice ALL=(ALL) ALL\n"},
		{"sudo: no newline", grant, hdr + "alice ALL=(ALL) NOPASSWD: ALL"},
		{"sudo: bob's grant", grant, hdr + "bob ALL=(ALL) ALL\n"},
		{"sudo: empty", grant, ""},
		{"sudo: env_keep replaced", env, hdr + "Defaults:alice env_keep = \"HARDN_CONFIG\"\n"},
		{"sudo: an empty list", env, hdr + "Defaults:alice env_keep += \"\"\n"},
		{"sudo: two spaces", env, hdr + "Defaults:alice env_keep += \"HARDN_CONFIG  EDITOR\"\n"},
		{"sudo: a name repeated", env, hdr + "Defaults:alice env_keep += \"HARDN_CONFIG HARDN_CONFIG\"\n"},
		{"sudo: not a variable's name", env, hdr + "Defaults:alice env_keep += \"HARDN-CONFIG\"\n"},
		{"sudo: bob's list", env, hdr + "Defaults:bob env_keep += \"HARDN_CONFIG\"\n"},
		{"sudo: a grant added", env, hdr + "Defaults:alice env_keep += \"HARDN_CONFIG\"\nalice ALL=(ALL) NOPASSWD: ALL\n"},
		{"doas: keepenv", rule, hdr + "permit persist keepenv alice\n"},
		{"doas: the group's rule", rule, hdr + "permit nopass setenv { HARDN_CONFIG } :wheel\n"},
		{"doas: an empty setenv", rule, hdr + "permit persist setenv {  } alice\n"},
		{"doas: a value set", rule, hdr + "permit persist setenv { HARDN_CONFIG=/etc/hardn.yml } alice\n"},
		{"doas: deny", rule, hdr + "deny alice\n"},
		{"doas: as root", rule, hdr + "permit persist alice as root\n"},
		{"doas: two rules", rule, hdr + "permit persist alice\npermit nopass alice\n"},
		{"doas: no persist", rule, hdr + "permit setenv { HARDN_CONFIG } alice\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, mode := sudoChoice, sudoers.SudoMode
			if tc.path == rule {
				c, mode = doasChoice, sudoers.DoasMode
			}
			h := hostFor(t, c)
			h.AddFile(tc.path, []byte(tc.data), mode, 0, 0)
			_, pol, err := readBack(t, h, c, "alice")
			var e *sudoers.EditedError
			if !errors.As(err, &e) || e.Path != tc.path || !errors.Is(err, sudoers.ErrEdited) || !samePolicy(pol, sudoers.Policy{}) {
				t.Errorf("PolicyOf = %+v, %v; want an EditedError naming %s", pol, err, tc.path)
			}
			if want := "sudoers: " + tc.path + ": hardn's file holds a rule hardn did not write"; err == nil || err.Error() != want {
				t.Errorf("text %q, want %q", err, want)
			}
		})
	}
	h := sudoHost(t)
	h.AddFile(grant, []byte(hdr+"alice ALL=(ALL:ALL) ALL\n"), sudoers.SudoMode, 0, 0)
	h.AddFile(env, []byte(hdr+"Defaults:alice env_keep = \"HARDN_CONFIG\"\n"), sudoers.SudoMode, 0, 0)
	_, _, err := readBack(t, h, sudoChoice, "alice")
	var named []string
	if j, ok := err.(interface{ Unwrap() []error }); ok {
		for _, e := range j.Unwrap() {
			var ed *sudoers.EditedError
			if errors.As(e, &ed) {
				named = append(named, ed.Path)
			}
		}
	}
	if !slices.Equal(named, []string{grant, env}) {
		t.Errorf("both edited: %v; want each named, the grant first", err)
	}
}

// TestPolicyOfMode: the content is read, not the mode or owner. PlanFor
// plans the same bytes rewritten 0440 root:root, as for any Policy
// (TestPlanIdempotent); the plan's check (Plan.Check), which the journal
// runs before the engine applies it (pkg/host/revert/plans), takes that
// only where the old file is root's alone: 0644 root:root, not 0644
// owned by 1000:1000, which the engine refuses rather than rewrites.
func TestPolicyOfMode(t *testing.T) {
	const env = "/etc/sudoers.d/hardn-env-alice"
	for _, tc := range []struct {
		uid, gid int
		checks   bool
	}{{0, 0, true}, {1000, 1000, false}} {
		h := sudoHost(t)
		h.AddFile(env, []byte(sudoers.Header+"Defaults:alice env_keep += \"HARDN_CONFIG\"\n"), 0o644, tc.uid, tc.gid)
		st, pol, err := readBack(t, h, sudoChoice, "alice")
		if err != nil || !samePolicy(pol, sudoers.Policy{User: "alice", Env: keep}) {
			t.Fatalf("%d:%d: PolicyOf = %+v, %v", tc.uid, tc.gid, pol, err)
		}
		p, err := sudoers.PlanFor(st, []sudoers.Policy{pol})
		if err != nil || len(p.Changes) != 1 || p.Changes[0].Path != env || string(p.Changes[0].Data) != string(st.Files[env].Data) || p.Changes[0].Mode != sudoers.SudoMode {
			t.Errorf("%d:%d: plan = %+v, %v; want the same bytes written 0440 root:root", tc.uid, tc.gid, p.Changes, err)
		}
		if err := p.Check(); (err == nil) != tc.checks || err != nil && !errors.Is(err, fs.ErrInvalid) {
			t.Errorf("%d:%d: Check = %v, want it to pass %v", tc.uid, tc.gid, err, tc.checks)
		}
	}
}

// TestPolicyOfRefuses: a user Read did not read, a name hardn does not
// write into a rule, and a tool that is not sudo or doas.
func TestPolicyOfRefuses(t *testing.T) {
	st, err := sudoers.Read(context.Background(), sudoHost(t), sudoChoice, []string{"alice"})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		st   sudoers.State
		user string
		want error
	}{
		{"not read", st, "bob", fs.ErrInvalid},
		{"a user name hardn does not write", st, "Alice", sudoers.ErrUserName},
		{"no tool", sudoers.State{}, "alice", fs.ErrInvalid},
	} {
		if pol, err := sudoers.PolicyOf(tc.st, tc.user); !errors.Is(err, tc.want) || !samePolicy(pol, sudoers.Policy{}) {
			t.Errorf("%s: PolicyOf = %+v, %v, want %v", tc.name, pol, err, tc.want)
		}
	}
}
