package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/controls"
	"github.com/abbott/hardn/pkg/engine/risk"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
)

// fixtureConfig is where the fixtures keep hardn.yml, when they have one:
// the system-wide path, the first hardn looks at.
const fixtureConfig = "/etc/hardn/hardn.yml"

// testEnv is run's environment over sys, on Linux as root, its output in
// out and errOut, hardn.yml looked for at fixtureConfig alone.
func testEnv(sys host.System, out, errOut *strings.Builder) env {
	return env{goos: "linux", euid: func() int { return 0 }, stdout: out, stderr: errOut,
		system: func() host.System { return sys }, controls: controls.Status, configs: []string{fixtureConfig}}
}

// runOn runs p4status with args over the fixture name, and returns the
// fake, the exit status and the output.
func runOn(t *testing.T, name string, args ...string) (f *hostfake.System, code int, out, errOut string) {
	t.Helper()
	f, sys := loadHost(t, name)
	var o, e strings.Builder
	code = run(context.Background(), args, testEnv(sys, &o, &e))
	return f, code, o.String(), e.String()
}

// reads are the fake's records of the methods that only read.
var reads = []string{"read", "read-nofollow", "stat", "lstat", "readdir", "readlink", "resolve-symlink", "lookup-user", "lookup-group"}

// TestRunOverHosts runs p4status over each fixture as on a host: it exits
// 0, makes no call on the host but reads and allowed commands, each
// answered, and lists every command it ran, with its exit status, in
// order, and no refusal; no check fails.
func TestRunOverHosts(t *testing.T) {
	for _, name := range fixtureHosts {
		t.Run(name, func(t *testing.T) {
			f, code, out, errOut := runOn(t, name)
			if code != exitOK || errOut != "" {
				t.Fatalf("exit %d, stderr %q", code, errOut)
			}
			var want []string
			for _, c := range f.Calls() {
				switch {
				case c.Op == "run":
					if !allowed(c.Argv) || errors.Is(c.Err, hostfake.ErrUnscripted) {
						t.Errorf("ran %v (allowed %t): %v", c.Argv, allowed(c.Argv), c.Err)
					}
					want = append(want, fmt.Sprintf("  %-10s %s", fmt.Sprintf("exit %d", c.ExitCode), shellWords(c.Argv)))
					if errors.Is(c.Err, host.ErrNotFound) {
						want[len(want)-1] = fmt.Sprintf("  %-10s %s: %v", "failed", shellWords(c.Argv), c.Err)
					}
				case !slices.Contains(reads, c.Op):
					t.Errorf("a call that is not a read: %v", c)
				}
			}
			if got := section(out, "commands ("); !slices.Equal(got[1:], want) {
				t.Errorf("commands listed:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
			}
			if !strings.Contains(out, fmt.Sprintf("commands (%d run, 0 refused, in order)", len(want))) {
				t.Errorf("no command count %d in\n%s", len(want), out)
			}
			for _, w := range []string{"\n  refused ", "\n  error ", "\n  incomplete ", "== incomplete"} {
				if strings.Contains(out, w) {
					t.Errorf("%q in the output:\n%s", w, out)
				}
			}
		})
	}
}

// section returns the lines of out's evidence under the heading that
// starts with heading, the heading first, down to the next blank line.
func section(out, heading string) []string {
	var got []string
	for _, l := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(l, heading):
			got = append(got, l)
		case len(got) > 0 && l == "":
			return got
		case len(got) > 0:
			got = append(got, l)
		}
	}
	return got
}

// menuOf is the main menu p4status printed: its output before the
// evidence, without the blank lines between them.
func menuOf(out string) string {
	menu, _, _ := strings.Cut(out, evidenceTitle)
	return strings.TrimRight(menu, "\n")
}

// galleryMenu is the main menu of the gallery's golden file of a status
// host, in its plain-80 cell: from the header's line to the prompt's, each
// line's "$" taken off.
func galleryMenu(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "uigallery", "testdata", "golden", "status", name+".golden"))
	if err != nil {
		t.Fatal(err)
	}
	_, cell, ok := strings.Cut(string(data), "\n=== plain-80:")
	if !ok {
		t.Fatalf("%s: no plain-80 cell", name)
	}
	var lines []string
	for _, l := range strings.Split(cell, "\n")[1:] {
		l = strings.TrimSuffix(l, "$")
		if strings.HasPrefix(l, "hardn ") || len(lines) > 0 {
			lines = append(lines, l)
		}
		if strings.HasPrefix(l, "    > Enter selection") {
			return strings.Join(lines, "\n")
		}
	}
	t.Fatalf("%s: no main menu in the plain-80 cell", name)
	return ""
}

// TestMainMenuAsGallery: the main menu p4status prints by default over a
// fixture is, line for line, the gallery's main menu of the status host
// the fixture was made from, as its golden file pins it (plain, 80
// columns).
func TestMainMenuAsGallery(t *testing.T) {
	for _, name := range fixtureHosts {
		t.Run(name, func(t *testing.T) {
			_, code, out, _ := runOn(t, name)
			if code != exitOK {
				t.Fatalf("exit %d", code)
			}
			if got, want := menuOf(out), galleryMenu(t, name); got != want {
				t.Errorf("main menu:\n%s\nthe gallery's:\n%s", got, want)
			}
		})
	}
}

// rogue is a control that tries to change the host: a command no check
// runs, a write, and the lock the engine's apply takes.
type rogue struct{ engine.CheckOnly }

func (rogue) ID() string                          { return "test.rogue" }
func (rogue) Title() string                       { return "Rogue" }
func (rogue) Applies(detect.Facts) (bool, string) { return true, "" }
func (rogue) Check(ctx context.Context, sys host.System, _ detect.Facts) (engine.Status, error) {
	_, runErr := sys.Run(ctx, "systemctl", "restart", "ssh.service")
	writeErr := sys.WriteFileAtomic(ctx, "/etc/ssh/sshd_config.d/00-hardn.conf", []byte("Port 2230\n"), 0o644, 0, 0)
	_, lockErr := sys.Lock(ctx, "/run/hardn/engine.lock", []byte("pid 1"))
	return engine.Status{State: engine.Configured, Detail: "changed"}, errors.Join(runErr, writeErr, lockErr)
}

// TestRefused: a command off the allowlist and a write are refused with no
// call on the host, listed in order among the commands, counted on
// stderr and above the main menu with the row that made them, and make
// the run exit 1; the control that made them is Unknown with the refusal
// as its error, and its row in the evidence says how many were refused.
func TestRefused(t *testing.T) {
	f, sys := loadHost(t, "debian13")
	var out, errOut strings.Builder
	e := testEnv(sys, &out, &errOut)
	e.controls = func(c *config.Config) []engine.Control { return append(controls.Status(c), rogue{}) }
	if code := run(context.Background(), nil, e); code != exitRefused {
		t.Errorf("exit %d, want %d", code, exitRefused)
	}
	if !strings.Contains(errOut.String(), "3 call(s) refused") {
		t.Errorf("stderr %q", errOut.String())
	}
	for _, c := range f.Calls() {
		if c.Op != "run" && !slices.Contains(reads, c.Op) || c.Op == "run" && !allowed(c.Argv) {
			t.Errorf("the host saw %v", c)
		}
	}
	if _, ok := f.Entry("/etc/ssh/sshd_config.d/00-hardn.conf"); ok {
		t.Error("the file was written")
	}
	cmds := strings.Join(section(out.String(), "commands ("), "\n")
	for _, w := range []string{
		"  refused    Run systemctl restart ssh.service: not a command the checks run\n",
		"  refused    WriteFileAtomic /etc/ssh/sshd_config.d/00-hardn.conf: it changes the host\n",
		"  refused    Lock /run/hardn/engine.lock: it changes the host",
		"refused, in order)",
	} {
		if !strings.Contains(cmds, w) {
			t.Errorf("no %q in\n%s", w, cmds)
		}
	}
	if !strings.HasPrefix(out.String(), "== incomplete: 3 call(s) refused, in test.rogue\n\nhardn ") {
		t.Errorf("no line above the main menu:\n%s", out.String())
	}
	row := strings.Join(section(out.String(), "row test.rogue"), "\n") + "\n"
	for _, w := range []string{
		"  incomplete 3 call(s) refused\n  state      unknown\n",
		`  error      UNCLASSIFIED_ERROR p4status: systemctl restart ssh.service: not a command the checks run: refused\x0a`,
	} {
		if !strings.Contains(row, w) {
			t.Errorf("no %q in the rogue's row:\n%s", w, row)
		}
	}
	if strings.Count(out.String(), "\n  incomplete ") != 1 {
		t.Errorf("another part marked incomplete:\n%s", out.String())
	}
}

// TestRefusedTolerated: a refused call that its check carries on without
// (ss, which SSH Login and SSH Auth on ssh.service read for §2.8a of
// check-ssh.md and do without) leaves those rows as they were, so the run
// names every row that made one, above the main menu and in the
// evidence, whatever its state.
func TestRefusedTolerated(t *testing.T) {
	saved := rules
	t.Cleanup(func() { rules = saved })
	rules = slices.DeleteFunc(slices.Clone(rules), func(r rule) bool { return r.name == "ss" })
	_, code, out, errOut := runOn(t, "hardened")
	if code != exitRefused || !strings.Contains(errOut, "4 call(s) refused") {
		t.Errorf("exit %d, stderr %q", code, errOut)
	}
	if !strings.HasPrefix(out, "== incomplete: 4 call(s) refused, in firewall.incoming, ssh.login, ssh.auth, ssh.port\n\nhardn ") {
		t.Errorf("no line above the main menu:\n%s", out)
	}
	for id, state := range map[string]string{
		"firewall.incoming": "unknown", "ssh.login": "configured", "ssh.auth": "configured", "ssh.port": "unknown",
	} {
		row := strings.Join(section(out, "row "+id), "\n")
		if w := "  incomplete 1 call(s) refused\n  state      " + state + "\n"; !strings.Contains(row, w) {
			t.Errorf("no %q in\n%s", w, row)
		}
	}
}

// TestHold: the engine's signal hold, which only an apply takes, is a
// refusal, listed among the commands with its reason.
func TestHold(t *testing.T) {
	g := newGuard(hostfake.New(host.Options{}))
	g.hold()()
	if g.refusals() != 1 {
		t.Fatalf("recorded %+v", g.calls())
	}
	b := evidenceWriter{calls: g.calls()}
	b.commands()
	if w := "  refused    Hold the engine's signal hold: only an apply takes it\n"; !strings.Contains(b.String(), w) {
		t.Errorf("no %q in\n%s", w, b.String())
	}
}

// failWriter is a stdout that cannot be written, as a closed pipe.
type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("broken pipe") }

// TestWriteFails: stdout that cannot be written is said on stderr, and
// the run exits 1.
func TestWriteFails(t *testing.T) {
	_, sys := loadHost(t, "debian13")
	var errOut strings.Builder
	e := testEnv(sys, nil, &errOut)
	e.stdout = failWriter{}
	if code := run(context.Background(), nil, e); code != exitRefused || errOut.String() != "p4status: broken pipe\n" {
		t.Errorf("exit %d, stderr %q", code, errOut.String())
	}
}

// TestCheckError: a check that fails shows Unknown on its row, with its
// error and the error's code in the evidence, and the factor the risk
// adds for it; the run, which refused nothing, exits 0.
func TestCheckError(t *testing.T) {
	f, sys := loadHost(t, "debian13")
	if err := f.Remove(context.Background(), "/sys/kernel/security/apparmor/profiles"); err != nil {
		t.Fatal(err)
	}
	var out, errOut strings.Builder
	if code := run(context.Background(), nil, testEnv(sys, &out, &errOut)); code != exitOK {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	if !strings.Contains(menuOf(out.String()), "AppArmor             Unknown can't read") {
		t.Errorf("main menu:\n%s", menuOf(out.String()))
	}
	row := strings.Join(section(out.String(), "row apparmor.enforce"), "\n")
	if !strings.Contains(row, "  state      unknown\n  detail     can't read\n") || !strings.Contains(row, "  error      FILE_NOT_FOUND ") {
		t.Errorf("AppArmor's row:\n%s", row)
	}
	if r := strings.Join(section(out.String(), "risk"), "\n"); !strings.Contains(r, "  factor     Counted as not configured (unknown, row apparmor.enforce, cost 0)") {
		t.Errorf("risk:\n%s", r)
	}
}

// TestEvidence checks the evidence of Proxmox VE 9 against what the
// fixture holds: detect's facts, each row, the header's facts and the
// risk.
func TestEvidence(t *testing.T) {
	_, code, out, _ := runOn(t, "pve9")
	if code != exitOK {
		t.Fatalf("exit %d", code)
	}
	for heading, want := range map[string][]string{
		"facts (detect)": {"  platform   distro=proxmox like= version=9 ", "  proxmox    installed=yes version=9.0.10 ",
			" etc-pve=yes\n"},
		"row users.admin (User)":           {"  state      not-configured\n  detail     root only\n"},
		"row firewall.incoming (Firewall)": {"  detail     disabled\n"},
		"row ssh.login (SSH Login)":        {"  detail     root allowed\n", "  evidence   sshd -T: permitrootlogin yes\n"},
		"header": {"  hostname   web-01\n", "  ipv4       default route yes, vmbr0, 192.0.2.20\n", "  primary    192.0.2.20\n",
			"  uptime     41 days, 1 hour (", "  reboot     pending no; running 6.17.2-1-pve; own kernel\n"},
		// Auto Updates is Not Applicable on Proxmox VE (slice 7 decision 7
		// (a)): it leaves the risk level, and the uptime has no factor.
		"risk": {"  band       critical, score 18.8, forced false\n"},
	} {
		got := strings.Join(section(out, heading), "\n") + "\n"
		for _, w := range want {
			if !strings.Contains(got, w) {
				t.Errorf("%s: no %q in\n%s", heading, w, got)
			}
		}
	}
	for _, id := range []string{"users.admin", "firewall.incoming", "ssh.login", "ssh.auth", "ssh.port", "apparmor.enforce", "updates.auto"} {
		if !strings.Contains(out, "\nrow "+id+" (") {
			t.Errorf("no row %s", id)
		}
	}
}

// cancelAt is a host on which reading name ends the reading, as Ctrl-C
// would then.
type cancelAt struct {
	host.System
	name   string
	cancel context.CancelFunc
}

func (c cancelAt) ReadFile(ctx context.Context, name string) ([]byte, error) {
	if name == c.name {
		c.cancel()
	}
	return c.System.ReadFile(ctx, name)
}

// TestReadCancelledInHeader: Ctrl-C during the header probe, after every
// check, is a reading cut short as any other: no main menu, and exit 1.
func TestReadCancelledInHeader(t *testing.T) {
	_, sys := loadHost(t, "debian13")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var out, errOut strings.Builder
	if code := run(ctx, nil, testEnv(cancelAt{sys, "/proc/sys/kernel/hostname", cancel}, &out, &errOut)); code != exitRefused {
		t.Errorf("exit %d", code)
	}
	if !strings.HasPrefix(out.String(), evidenceTitle) || !strings.Contains(out.String(), "\nnot read: CANCELLED header: context canceled\n") {
		t.Errorf("stdout:\n%s\nstderr: %s", out.String(), errOut.String())
	}
}

// TestReadFails: a reading cut short (here by ctx) prints detect's facts,
// the error and the commands run, no main menu, and exits 1.
func TestReadFails(t *testing.T) {
	_, sys := loadHost(t, "debian13")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var out, errOut strings.Builder
	if code := run(ctx, nil, testEnv(sys, &out, &errOut)); code != exitRefused {
		t.Errorf("exit %d", code)
	}
	if !strings.HasPrefix(out.String(), evidenceTitle) || !strings.Contains(out.String(), "\nnot read: ") || !strings.Contains(errOut.String(), "the host was not read") {
		t.Errorf("stdout:\n%s\nstderr: %s", out.String(), errOut.String())
	}
}

// TestGates: a wrong flag, another platform than Linux and a user other
// than root exit 2 before the host is built; -h exits 0.
func TestGates(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		goos string
		euid int
		code int
		want string
	}{
		{"macOS", nil, "darwin", 0, exitUsage, "this is darwin"},
		{"not root", nil, "linux", 1000, exitUsage, "p4status: run as root\n"},
		{"profile", []string{"-profile", "sepia"}, "linux", 0, exitUsage, `-profile "sepia"`},
		{"width", []string{"-width", "0"}, "linux", 0, exitUsage, "must be positive"},
		{"argument", []string{"apply"}, "linux", 0, exitUsage, "unexpected arguments"},
		{"flag", []string{"-yes"}, "linux", 0, exitUsage, "-yes"},
		{"help", []string{"-h"}, "linux", 0, exitOK, "usage: sudo ./p4status"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out, errOut strings.Builder
			e := env{goos: tc.goos, euid: func() int { return tc.euid }, stdout: &out, stderr: &errOut, controls: controls.Status,
				system: func() host.System { t.Fatal("the host was built"); return nil }}
			if code := run(context.Background(), tc.args, e); code != tc.code || out.Len() != 0 || !strings.Contains(errOut.String(), tc.want) {
				t.Errorf("exit %d, stdout %q, stderr %q", code, out.String(), errOut.String())
			}
		})
	}
}

// TestProfiles: -profile picks the main menu's colours, each as its
// terminal would show them, and -width its width; the evidence is plain
// in every profile.
func TestProfiles(t *testing.T) {
	for _, tc := range []struct {
		profile   string
		has, hasn []string
	}{
		{"plain", nil, []string{"\x1b"}},
		{"none", []string{"\x1b[1m"}, []string{"38;", "\x1b[3"}},
		{"16", []string{"\x1b[3"}, []string{"38;5;", "38;2;"}},
		{"256", []string{"38;5;"}, []string{"38;2;"}},
		{"truecolor", []string{"38;2;"}, nil},
	} {
		t.Run(tc.profile, func(t *testing.T) {
			_, code, out, _ := runOn(t, "hardened", "-profile", tc.profile)
			menu, ev, _ := strings.Cut(out, evidenceTitle)
			if code != exitOK || strings.Contains(ev, "\x1b") {
				t.Fatalf("exit %d, evidence with escapes", code)
			}
			for _, w := range tc.has {
				if !strings.Contains(menu, w) {
					t.Errorf("no %q in the main menu", w)
				}
			}
			for _, w := range tc.hasn {
				if strings.Contains(menu, w) {
					t.Errorf("%q in the main menu", w)
				}
			}
		})
	}
	_, _, out, _ := runOn(t, "hardened", "-width", "60")
	for _, l := range strings.Split(menuOf(out), "\n") {
		if utf8.RuneCountInString(l) > 60 {
			t.Errorf("wider than 60: %q", l)
		}
	}
}

// TestStatusControls: the rows p4status checks are the main menu's one
// list, controls.Status, which pkg/engine/controls's TestStatus holds to
// the score's rows, as hardn's main menu and the gallery check them; and
// hardn.yml is looked for where hardn looks, each path absolute.
func TestStatusControls(t *testing.T) {
	e := production()
	var got, want []string
	for _, c := range e.controls(nil) {
		got = append(got, c.ID())
	}
	for _, r := range risk.Rows() {
		want = append(want, r.ID)
	}
	if !slices.Equal(got, want) {
		t.Errorf("controls %v, want %v", got, want)
	}
	t.Setenv("HARDN_CONFIG", "")
	if paths := configPaths(); len(paths) == 0 || paths[0] != fixtureConfig || !slices.ContainsFunc(paths, func(p string) bool { return strings.HasSuffix(p, "/hardn.yml") && filepath.IsAbs(p) }) {
		t.Errorf("hardn.yml looked for at %q", paths)
	}
	t.Setenv("HARDN_CONFIG", "hardn-test.yml")
	if paths := configPaths(); len(paths) != 1 || !filepath.IsAbs(paths[0]) || filepath.Base(paths[0]) != "hardn-test.yml" {
		t.Errorf("with HARDN_CONFIG, hardn.yml looked for at %q", paths)
	}
}

// TestClean: a control character read from the host is escaped, and a
// format or line-separating character too (one that reorders the line
// around it, or breaks it).
func TestClean(t *testing.T) {
	if got := clean("a\x1b[31mb\tc\u0085 root\u202edisallowed\u2066x\u2028y\U000e0001z é"); got != `a\x1b[31mb\x09c\x85 root\u202edisallowed\u2066x\u2028y\U000e0001z é` {
		t.Errorf("clean = %q", got)
	}
}

// TestShellWords: a command of the evidence is quoted for a POSIX shell,
// so that pasted it runs what ran: dpkg-query's format is not expanded.
func TestShellWords(t *testing.T) {
	for _, tc := range []struct {
		argv []string
		want string
	}{
		{[]string{"sshd", "-T", "-f", "/etc/ssh/sshd_config", "-C", "user=root,host=web-01.example.net,addr=192.0.2.7"},
			"sshd -T -f /etc/ssh/sshd_config -C user=root,host=web-01.example.net,addr=192.0.2.7"},
		{[]string{"dpkg-query", "--show", `--showformat=${Status}\t${Version}\n`, "unattended-upgrades"},
			`dpkg-query --show '--showformat=${Status}\t${Version}\n' unattended-upgrades`},
		{[]string{"apt-config", "dump", "APT::Periodic"}, "apt-config dump APT::Periodic"},
		{[]string{"echo", "", "it's", "a b"}, `echo '' 'it'\''s' 'a b'`},
		{[]string{"X=1", "~", "$HOME"}, `'X=1' '~' '$HOME'`},
	} {
		if got := shellWords(tc.argv); got != tc.want {
			t.Errorf("shellWords(%q) = %s, want %s", tc.argv, got, tc.want)
		}
	}
}
