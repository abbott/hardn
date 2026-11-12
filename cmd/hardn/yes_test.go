package main

import (
	"strings"
	"testing"

	hostdns "github.com/abbott/hardn/pkg/host/dns"
	"github.com/abbott/hardn/pkg/ui/term"
)

// confirmationWords are output.Confirmation's refusal off a terminal, as
// output.Human says it: the plan shown, nothing applied, --yes named.
const confirmationWords = "run it again with --yes to apply it (CONFIRMATION_REQUIRED)"

// yesFlag is one operation flag as TestYesFlag runs it: run runs hardn
// with args (the flag's own among them) on a fresh host whose plan has
// changes, or none where nothing is set, stdout's render environment env,
// typing lines, and reports whether the change was applied.
type yesFlag struct {
	name string
	args []string
	run  func(t *testing.T, nothing bool, env func() term.Env, lines []string, args ...string) (result, string, bool)
}

// TestYesFlag (refactor plan §3.11, approved 2026-11-04: "operation flags
// require --yes without a TTY"): for each flag whose plan has no lockout
// risk (-g, -c), off a terminal (stdin not one)
// without --yes a plan with changes is shown and not applied, exit status
// 2, CONFIRMATION_REQUIRED naming --yes; with --yes it is applied, 0; on
// a terminal it is applied unasked, with --yes or without, as before;
// under plan only (-n) --yes changes nothing, 3; and a plan with nothing
// to change is 0 off a terminal without --yes. -d's lockout-risk plan
// (the lockout rule; -w's and -r's are in their own tables): off a
// terminal refused, PLAN_REFUSED, 1, --yes or not; on a terminal the
// hostname typed, --yes or not.
func TestYesFlag(t *testing.T) {
	plain := []yesFlag{
		{"-g", []string{"-g"}, func(t *testing.T, nothing bool, env func() term.Env, lines []string, args ...string) (result, string, bool) {
			host := "debian13"
			if nothing {
				host = "configured"
			}
			h, cfg := newDNSHost(t, host, dnsTestList)
			was := h.content(hostdns.ResolvConf)
			r, s := runG(t, h, cfg, func(*dnsSeams) {
				if env != nil {
					stdoutEnv = env
				}
			}, args...)
			return r, stripANSI(s.out.String()), h.content(hostdns.ResolvConf) != was
		}},
		{"-c", []string{"-c", "-u", "carol"}, func(t *testing.T, _ bool, env func() term.Env, lines []string, args ...string) (result, string, bool) {
			h, cfg := newUsersHost(t)
			r, s := runC(t, h, cfg, func(*dnsSeams) {
				if env != nil {
					stdoutEnv = env
				}
			}, args...)
			return r, stripANSI(s.out.String()), len(h.commands("useradd")) > 0
		}},
	}
	for _, f := range plain {
		for _, tc := range []struct {
			name     string
			yes, dry bool
			nothing  bool
			env      func() term.Env
			code     int
			applied  bool
			said     []string
		}{
			{name: "off a terminal", code: 2, said: []string{"No actual changes were made to your system.", confirmationWords}},
			{name: "off a terminal, --yes", yes: true, applied: true, said: []string{"Operation Successful"}},
			{name: "stdout a terminal, stdin not", env: func() term.Env { return ttyEnv(true, false) }, code: 2, said: []string{confirmationWords}},
			{name: "on a terminal", env: onTTY, applied: true, said: []string{"Operation Successful"}},
			{name: "on a terminal, --yes", env: onTTY, yes: true, applied: true, said: []string{"Operation Successful"}},
			{name: "stdin a terminal, stdout not", env: func() term.Env { return ttyEnv(false, true) }, applied: true, said: []string{"Operation Successful"}},
			{name: "-n, --yes", yes: true, dry: true, code: 3, said: []string{"No actual changes were made to your system."}},
			{name: "nothing to change", nothing: true, said: []string{"Nothing needs changing."}},
		} {
			if tc.nothing && f.name == "-c" {
				continue // -c always creates: a name taken is refused (TestCreateUserFlag)
			}
			args := append([]string{}, f.args...)
			if tc.yes {
				args = append([]string{"--yes"}, args...)
			}
			if tc.dry {
				args = append([]string{"-n"}, args...)
			}
			t.Run(f.name+", "+tc.name, func(t *testing.T) {
				r, out, applied := f.run(t, tc.nothing, tc.env, nil, args...)
				if got := map[bool]int{true: r.code}[r.ended]; got != tc.code {
					t.Fatalf("exit status %d (ended %v), want %d; report:\n%s\nstderr:\n%s", r.code, r.ended, tc.code, out, r.stderr)
				}
				if w := inOrder(out, tc.said...); w != "" {
					t.Errorf("the report lacks %q:\n%s", w, out)
				}
				if applied != tc.applied {
					t.Errorf("applied %t, want %t:\n%s", applied, tc.applied, out)
				}
				if tc.code != 2 && strings.Contains(out, "CONFIRMATION_REQUIRED") {
					t.Errorf("CONFIRMATION_REQUIRED said:\n%s", out)
				}
			})
		}
	}

	// -d: lockout risk, the hostname typed on a terminal alone.
	for _, tc := range []struct {
		name    string
		args    []string
		env     func() term.Env
		lines   []string
		code    int
		applied bool
		said    []string
	}{
		{"off a terminal", []string{"-d"}, nil, nil, 1, false, []string{"Lockout risk", "PLAN_REFUSED"}},
		{"off a terminal, --yes", []string{"--yes", "-d"}, nil, nil, 1, false, []string{"Lockout risk", "PLAN_REFUSED"}},
		{"on a terminal, --yes", []string{"--yes", "-d"}, onTTY, []string{"web-01"}, 0, true, []string{"Type the hostname", "Operation Successful", "sudo hardn confirm"}},
		{"-n, --yes", []string{"-n", "--yes", "-d"}, nil, nil, 3, false, []string{"Lockout risk", "No actual changes were made to your system."}},
	} {
		t.Run("-d, "+tc.name, func(t *testing.T) {
			h, cfg := newSSHHost(t, "debian13")
			was := h.rootLogin()
			r, s := runD(t, h, cfg, tc.env, tc.lines, nil, tc.args...)
			out := stripANSI(s.out.String())
			if got := map[bool]int{true: r.code}[r.ended]; got != tc.code {
				t.Fatalf("exit status %d (ended %v), want %d; report:\n%s\nstderr:\n%s", r.code, r.ended, tc.code, out, r.stderr)
			}
			if w := inOrder(out, tc.said...); w != "" {
				t.Errorf("the report lacks %q:\n%s", w, out)
			}
			if applied := h.rootLogin() != was; applied != tc.applied || strings.Contains(out, "CONFIRMATION_REQUIRED") {
				t.Errorf("applied %t, want %t:\n%s", applied, tc.applied, out)
			}
		})
	}
}
