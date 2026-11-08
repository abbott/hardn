package firewall_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/controls/firewall"
	"github.com/abbott/hardn/pkg/engine/steps"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/packages"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/host/ufw"
	"github.com/abbott/hardn/pkg/output"
	"github.com/abbott/hardn/pkg/screens/screenkit"
)

// The plans run through the real engine on hostfake, ufw answering from the
// model of firewall_test.go, which here does what it is asked (live). What ufw
// answers to an application's allow is from memory of ufw, not captured:
// docs/safety/ufw.md §3 and §4.

const (
	hardnBinary = "/usr/local/bin/hardn"
	hardnYML    = "/etc/hardn/hardn.yml"
	wordsNoSS   = "hardn can't tell which ports SSH uses: ss isn't installed. Install iproute2, then try again."
	wordsSSBad  = "hardn can't tell which ports SSH uses: ss failed. Check that it runs, then try again."
	wordsStale  = "sshd isn't listening where its config says, so hardn can't tell which ports SSH uses. Reload sshd or restart ssh.socket, then try again."
)

var (
	remote22 = revert.Session{Remote: true, Conn: "192.0.2.10 50022 192.0.2.20 22", Source: "SSH_CONNECTION"}
	console  = revert.Session{Source: "no SSH_CONNECTION"}
	// profiles as hardn.yml holds them.
	web = config.UfwAppProfile{Name: "Web", Title: "Web Server", Description: "A web server", Ports: []string{"80/tcp", "443/tcp"}}
	dns = config.UfwAppProfile{Name: "DNS", Title: "DNS", Description: "Names", Ports: []string{"53/udp"}}
)

// live is a site whose ufw and boot services change as they are told.
type live struct {
	*hostfake.System
	facts detect.Facts
	u     fw
	fail  string // the command that exits 1 and does nothing
	// bootStuck: systemctl enable and rc-update add answer as done and change
	// nothing.
	bootStuck bool
}

// liveOn builds s, with what the engine needs to apply a plan.
func liveOn(t *testing.T, s site) *live {
	t.Helper()
	h, facts := s.build(t)
	l := &live{System: h, facts: facts, u: s.ufw}
	for _, d := range []string{"/var/lib", "/var/backups", "/run", "/var/log", "/usr/local/bin", "/etc/ufw/applications.d", "/usr/lib/systemd/system", "/etc/systemd/system", "/etc/init.d", "/etc/runlevels/default"} {
		h.AddDir(d, 0o755, 0, 0)
	}
	if s.plat.facts.Init.Kind == detect.OpenRC {
		h.AddFile("/etc/init.d/ufw", []byte("#!/sbin/openrc-run\n"), 0o755, 0, 0)
	}
	h.AddFile(hardnBinary, []byte("ELF"), 0o755, 0, 0)
	inner := h.Handler
	h.Handler = func(argv []string, stdin []byte) hostfake.Response {
		cmd := strings.Join(argv, " ")
		switch {
		case cmd == l.fail:
			return hostfake.Response{Stderr: "ERROR: failed\n", ExitCode: 1}
		case cmd == "ufw status verbose" && l.u.fail != nil:
			return *l.u.fail
		case cmd == "systemctl is-enabled ufw.service" && l.u.boot == "":
			return hostfake.Response{Stdout: "enabled\n"}
		case cmd == "systemctl is-enabled ufw.service" && l.u.boot != "fail":
			return hostfake.Response{Stdout: l.u.boot + "\n", ExitCode: 1}
		case argv[0] == "ufw" && l.u.missing:
			return inner(argv, stdin)
		case argv[0] == "dpkg-query" && argv[len(argv)-1] == "ufw" && l.u.missing:
			return hostfake.Response{Stderr: "dpkg-query: no packages found matching ufw\n", ExitCode: 1}
		case argv[0] == "dpkg-query" && argv[len(argv)-1] == "ufw":
			return hostfake.Response{Stdout: "install ok installed\t0.36.2-9\n"}
		case cmd == "apk info -e ufw" && l.u.missing:
			return hostfake.Response{ExitCode: 1}
		case cmd == "apk info -e ufw":
			return hostfake.Response{Stdout: "ufw\n"}
		case cmd == "apt-get install -s -y ufw":
			return aptSim
		case cmd == "apk add --simulate ufw":
			return apkSim
		}
		if out, ok := l.run(argv); ok {
			return hostfake.Response{Stdout: out}
		}
		return inner(argv, stdin)
	}
	return l
}

// run is what l does for a command that changes it.
func (l *live) run(argv []string) (string, bool) {
	ctx := context.Background()
	cmd := strings.Join(argv, " ")
	defer func() {
		if argv[0] == "ufw" && !l.u.missing {
			l.u.files(l.System)
		}
	}()
	switch {
	case cmd == "ufw status verbose":
		return l.u.status(), true
	case cmd == "ufw --force enable":
		l.u.active, l.u.enabled = true, "yes"
		return "Firewall is active and enabled on system startup\n", true
	case cmd == "ufw disable":
		l.u.active, l.u.enabled = false, "no"
		return "Firewall stopped and disabled on system startup\n", true
	case len(argv) == 4 && argv[1] == "default" && argv[3] == "incoming":
		l.u.incoming = argv[2]
		return "Default incoming policy changed to '" + argv[2] + "'\n", true
	case len(argv) == 3 && argv[1] == "allow":
		port := strings.TrimSuffix(argv[2], "/tcp")
		if slices.ContainsFunc(l.u.rules, func(r rule) bool { return r.port == port && r.app == "" }) {
			return "Skipping adding existing rule\nSkipping adding existing rule (v6)\n", true
		}
		l.u.rules = append(l.u.rules, rule{action: "allow", port: port})
		return "Rule added\nRule added (v6)\n", true
	case len(argv) == 4 && argv[1] == "delete":
		port := strings.TrimSuffix(argv[3], "/tcp")
		l.u.rules = slices.DeleteFunc(l.u.rules, func(r rule) bool { return r.port == port && r.app == "" })
		return "Rule deleted\nRule deleted (v6)\n", true
	case len(argv) == 8 && argv[1] == "allow" && argv[6] == "app":
		name := argv[7]
		data, err := l.ReadFile(ctx, ufw.ProfileFile)
		if err != nil || !strings.Contains(string(data), "["+name+"]") {
			return "", false // not reached in a plan that writes the file first: the handler fails the test
		}
		if slices.ContainsFunc(l.u.rules, func(r rule) bool { return r.app == name }) {
			return "Skipping adding existing rule\nSkipping adding existing rule (v6)\n", true
		}
		l.u.rules = append(l.u.rules, rule{action: "allow", port: "80", app: name})
		return "Rule added\nRule added (v6)\n", true
	case len(argv) == 9 && argv[1] == "delete" && argv[7] == "app":
		l.u.rules = slices.DeleteFunc(l.u.rules, func(r rule) bool { return r.app == argv[8] && r.iface == "" })
		return "Rule deleted\nRule deleted (v6)\n", true
	case len(argv) == 11 && argv[1] == "allow" && argv[2] == "in" && argv[3] == "on" && argv[9] == "app": // a profile on an interface
		name, iface := argv[10], argv[4]
		if slices.ContainsFunc(l.u.rules, func(r rule) bool { return r.app == name && r.iface == iface }) {
			return "Skipping adding existing rule\nSkipping adding existing rule (v6)\n", true
		}
		l.u.rules = append(l.u.rules, rule{action: "allow", port: "80", app: name, iface: iface})
		return "Rule added\nRule added (v6)\n", true
	case len(argv) == 5 && argv[1] == "allow" && argv[2] == "in" && argv[3] == "on": // a trusted interface
		iface := argv[4]
		if slices.ContainsFunc(l.u.rules, func(r rule) bool { return r.port == "" && r.iface == iface }) {
			return "Skipping adding existing rule\nSkipping adding existing rule (v6)\n", true
		}
		l.u.rules = append(l.u.rules, rule{action: "allow", iface: iface})
		return "Rule added\nRule added (v6)\n", true
	case len(argv) == 6 && argv[1] == "delete" && argv[3] == "in" && argv[4] == "on":
		l.u.rules = slices.DeleteFunc(l.u.rules, func(r rule) bool { return r.port == "" && r.iface == argv[5] })
		return "Rule deleted\nRule deleted (v6)\n", true
	case cmd == "systemctl enable ufw.service":
		if !l.bootStuck {
			l.u.boot = ""
		}
		return "", true
	case cmd == "systemctl disable ufw.service":
		l.u.boot = "disabled"
		return "", true
	case cmd == "rc-update add ufw default":
		if !l.bootStuck {
			l.AddFile(detect.OpenRCRunlevels+"/default/ufw", nil, 0o644, 0, 0)
		}
		return "", true
	case cmd == "rc-update del ufw default":
		_ = l.Remove(ctx, detect.OpenRCRunlevels+"/default/ufw")
		return "", true
	// the revert timer and the boot unit a lockout-risk plan installs
	case cmd == "systemctl --no-reload enable hardn-boot.service":
		l.AddSymlink("/etc/systemd/system/multi-user.target.wants/hardn-boot.service", "/etc/systemd/system/hardn-boot.service")
		return "", true
	case cmd == "rc-update add hardn-boot default":
		l.AddSymlink(detect.OpenRCRunlevels+"/default/hardn-boot", "/etc/init.d/hardn-boot")
		return "", true
	case argv[0] == "systemd-run", argv[0] == "systemd-analyze", argv[0] == "sh", argv[0] == "/bin/sh", argv[0] == "systemctl" && strings.Contains(cmd, "hardn"), cmd == "systemctl daemon-reload", argv[0] == "setsid", argv[0] == "rc-service", argv[0] == "rc-update":
		return "", true
	}
	return "", false
}

func engineOn(l *live, session revert.Session) *engine.Engine {
	return engine.New(l, engine.Options{
		Revert:  revert.Options{Now: func() time.Time { return time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC) }, Hardn: hardnBinary},
		Detect:  func(context.Context, host.System) (detect.Facts, error) { return l.facts, nil },
		Hold:    func() func() { return func() {} },
		PID:     5000,
		Session: session,
		Backups: backup.Settings{Path: backup.DefaultPath, Enabled: true},
	})
}

// planOn plans action on l through the engine as cmd/hardn builds it.
func planOn(t *testing.T, l *live, a firewall.Action, session revert.Session, cfg *config.Config) (*engine.Engine, *engine.Plan) {
	t.Helper()
	e := engineOn(l, session)
	p, err := e.Plan(context.Background(), []engine.Control{firewall.Incoming{Action: a}}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	return e, p
}

// commands are the plan's own, a control's changes alone (not the boot unit's).
func commands(p *engine.Plan) []string {
	var out []string
	for _, c := range p.Changes {
		if c.Control == firewall.ID {
			out = append(out, c.Commands...)
		}
	}
	return out
}

// refused checks the plan is refused with words, as a screen and pkg/output
// both say it, the refusal's code the registry's for what it wraps, and no
// change.
func refused(t *testing.T, p *engine.Plan, words string, want error) {
	t.Helper()
	if !p.Empty() || p.Err() == nil || len(p.Items) != 1 {
		t.Fatalf("plan: %d changes, %v", len(p.Changes), p.Err())
	}
	err := p.Items[0].Err
	var rf *steps.Refusal
	if !errors.As(err, &rf) {
		t.Fatalf("%v: not a refusal", err)
	}
	if got := screenkit.RefusalWords(err); got != words {
		t.Errorf("words\n got %q\nwant %q", got, words)
	}
	if got := output.CodeOf(err); got != rf.Code {
		t.Errorf("code %s, the registry's %s", rf.Code, got)
	}
	if e, ok := output.Lookup(rf.Code); !ok || e.Kind != output.KindError {
		t.Errorf("code %s is not a registered error", rf.Code)
	}
	if !errors.Is(err, want) {
		t.Errorf("%v is not %v", err, want)
	}
}

// changedNothing fails for a call that changed the host, the plan's run
// directory and the engine's own apart, or a ufw that is not a read.
func changedNothing(t *testing.T, l *live) {
	t.Helper()
	for _, c := range l.Calls() {
		switch c.Op {
		case "write-atomic", "write-in-place", "remove", "rename", "chmod", "chown", "append":
			if !strings.HasPrefix(c.Path, "/run/hardn") {
				t.Errorf("changed: %v", c)
			}
		case "run":
			if c.Argv[0] == "ufw" && strings.Join(c.Argv, " ") != "ufw status verbose" {
				t.Errorf("ran: %v", c)
			}
		}
	}
}

// TestEnableNotInEffect: decision 2 both ways, with a session and from a
// console: configured 2230 with sshd still on 22 (a stale listener, which the
// plan alone would refuse from a console), configured 22 with sshd on 2230. It
// refuses before any ufw command, so a stale listener is never refused.
func TestEnableNotInEffect(t *testing.T) {
	for name, s := range map[string]site{
		"configured 2230, sshd on 22":       {plat: debian13(), ports: []uint16{2230}, bound: stale22, ufw: fw{}},
		"configured 22, sshd on 2230":       {plat: debian13(), bound: []string{"0.0.0.0:2230", "[::]:2230"}, ufw: fw{}},
		"configured 2230, sshd on 22, on":   {plat: debian13(), ports: []uint16{2230}, bound: stale22, ufw: allow22},
		"socket: configured 2230, 22 bound": {plat: ubuntu2604(), ports: []uint16{2230}, socket: []string{"[::]:2230"}, bound: []string{"[::]:22"}, ufw: fw{}},
	} {
		for _, session := range []revert.Session{remote22, console} {
			t.Run(name+"/"+session.Source, func(t *testing.T) {
				l := liveOn(t, s)
				_, p := planOn(t, l, firewall.ActionEnable, session, nil)
				refused(t, p, wordsStale, firewall.ErrNotInEffect)
				changedNothing(t, l)
			})
		}
	}
}

// TestEnableSessionNotRead: a remote session whose connection was not read, the
// form revert.ReadSession returns under tmux, fails closed: its port cannot be
// kept, even with a socket on 22 as its only clue.
func TestEnableSessionNotRead(t *testing.T) {
	const words = "hardn can't tell which port this session uses. Run hardn outside tmux or screen, or from a console, then try again."
	for name, s := range map[string]site{
		"no stray": {plat: debian13(), ufw: fw{}},
		"a stray":  {plat: debian13(), ports: []uint16{2230}, bound: []string{"0.0.0.0:2230", "[::]:2230", "0.0.0.0:22", "[::]:22"}, ufw: fw{}},
	} {
		t.Run(name, func(t *testing.T) {
			l := liveOn(t, s)
			session := revert.Session{Remote: true, Source: "SSH_TTY"}
			_, p := planOn(t, l, firewall.ActionEnable, session, nil)
			refused(t, p, words, steps.ErrSessionPort)
			changedNothing(t, l)
			if got := output.CodeOf(p.Items[0].Err); got != "UFW_LISTENERS_UNKNOWN" {
				t.Errorf("code %s", got)
			}
		})
	}
}

// TestEnableNoSS: with ss missing or failing what accepts SSH is not read.
func TestEnableNoSS(t *testing.T) {
	l := liveOn(t, site{plat: noSS(debian13()), ufw: fw{}})
	_, p := planOn(t, l, firewall.ActionEnable, remote22, nil)
	refused(t, p, wordsNoSS, firewall.ErrNoSS)
	changedNothing(t, l)
	l = liveOn(t, site{plat: debian13(), ufw: fw{}, ssErr: &hostfake.Response{Stderr: "ss: bad\n", ExitCode: 1}})
	_, p = planOn(t, l, firewall.ActionEnable, console, nil)
	refused(t, p, wordsSSBad, firewall.ErrSSFailed)
	changedNothing(t, l)
	if got := output.CodeOf(p.Items[0].Err); got != "FIREWALL_SS_FAILED" {
		t.Errorf("code %s", got)
	}
}

// TestEnableOrder: the allows, then the policy, then ufw starts, lockout-risk,
// on a fresh inactive ufw and on one that was set to allow; the session's port
// is kept, and a console keeps none but what is configured; a second plan is
// empty.
// TestEnableNoServer: on a host established to have no SSH server
// (sshd.NoServer), Enable lets no SSH through: it denies by default and
// starts ufw, a remote session's port alone let through, lockout-risk as
// ever; once applied, the Firewall row is Configured. Where openssh-server
// is installed but sshd can't run, where ss shows a socket on 22 all the
// same, or where ss is missing, Enable is refused as before.
func TestEnableNoServer(t *testing.T) {
	ctx := context.Background()
	for name, tc := range map[string]struct {
		s       site
		session revert.Session
		want    []string
		does    firewall.Does
	}{
		"inactive, from the console": {site{plat: debian13(), failT: noSSHD, noMain: true, dpkg: noServer, bound: []string{}, ufw: fw{}}, console,
			[]string{"ufw --force enable"}, firewall.Does{StartsUFW: true}},
		"the policy allow": {site{plat: debian13(), failT: noSSHD, noMain: true, dpkg: noServer, bound: []string{}, ufw: fw{incoming: "allow"}}, console,
			[]string{"ufw default deny incoming", "ufw --force enable"}, firewall.Does{SetsDeny: true, StartsUFW: true}},
		"a remote session's port kept": {site{plat: debian13(), failT: noSSHD, noMain: true, dpkg: noServer, bound: []string{}, ufw: fw{}}, remote22,
			[]string{"ufw allow 22/tcp", "ufw --force enable"}, firewall.Does{Allows: true, StartsUFW: true}},
	} {
		t.Run(name, func(t *testing.T) {
			l := liveOn(t, tc.s)
			r := firewall.Read(ctx, l, l.facts, nil, tc.session)
			if r.First != firewall.ItemEnable || r.PlanErr != nil || r.Does != tc.does || !r.Sockets.NoServer {
				t.Errorf("read: item %d, %v, does %+v, no server %v", r.First, r.PlanErr, r.Does, r.Sockets.NoServer)
			}
			e, p := planOn(t, l, firewall.ActionEnable, tc.session, nil)
			if p.Err() != nil {
				t.Fatal(p.Err())
			}
			if got := commands(p); !slices.Equal(got, tc.want) {
				t.Errorf("commands %q, want %q", got, tc.want)
			}
			if p.Risk != engine.RiskLockout {
				t.Errorf("risk %v, want lockout", p.Risk)
			}
			if _, err := e.Apply(ctx, p); err != nil {
				t.Fatal(err)
			}
			if got := ranUFW(l); !slices.Equal(got, tc.want) {
				t.Errorf("ran %q, want %q", got, tc.want)
			}
			if st, err := (firewall.Incoming{}).Check(ctx, l, l.facts); err != nil || st.State != engine.Configured || st.Detail != "deny policy" {
				t.Errorf("after: %s %q, %v; want Configured \"deny policy\"", st.State, st.Detail, err)
			}
			if r := firewall.Read(ctx, l, l.facts, nil, tc.session); r.First != firewall.ItemDisable {
				t.Errorf("after: item 1 %d, want Disable: ufw is set", r.First)
			}
		})
	}
	for name, s := range map[string]site{
		"a socket on 22 no package explains": {plat: debian13(), failT: noSSHD, noMain: true, dpkg: noServer, bound: []string{"0.0.0.0:22"}, ufw: fw{}},
		"no ss":                              {plat: noSS(debian13()), failT: noSSHD, noMain: true, dpkg: noServer, ufw: fw{}},
	} {
		t.Run(name, func(t *testing.T) {
			l := liveOn(t, s)
			if _, p := planOn(t, l, firewall.ActionEnable, console, nil); p.Err() == nil || len(p.Changes) > 0 {
				t.Errorf("planned %d changes, error %v; want refused", len(p.Changes), p.Err())
			}
		})
	}
	t.Run("openssh-server installed, no sshd_config", func(t *testing.T) {
		l := liveOn(t, site{plat: debian13(), noMain: true, dpkg: map[string]string{"openssh-server": "install ok installed"}, ufw: fw{}})
		if _, p := planOn(t, l, firewall.ActionEnable, console, nil); p.Err() == nil || len(p.Changes) > 0 {
			t.Errorf("planned %d changes, error %v; want refused", len(p.Changes), p.Err())
		}
	})
}

func TestEnableOrder(t *testing.T) {
	ctx := context.Background()
	for name, tc := range map[string]struct {
		s       site
		session revert.Session
		want    []string
		undo    string
		does    firewall.Does
	}{
		"inactive": {site{plat: debian13(), ufw: fw{}}, console, []string{"ufw allow 22/tcp", "ufw --force enable"}, "[ufw disable]",
			firewall.Does{Allows: true, StartsUFW: true}},
		"the policy allow": {site{plat: debian13(), ufw: fw{incoming: "allow"}}, console, []string{"ufw allow 22/tcp", "ufw default deny incoming", "ufw --force enable"},
			"[ufw disable] [ufw default allow incoming]", firewall.Does{Allows: true, SetsDeny: true, StartsUFW: true}},
		"the session's port kept": {site{plat: debian13(), ports: []uint16{2230}, bound: []string{"0.0.0.0:2230", "[::]:2230", "0.0.0.0:22", "[::]:22"}, ufw: fw{}}, remote22,
			[]string{"ufw allow 22/tcp", "ufw allow 2230/tcp", "ufw --force enable"}, "[ufw disable]", firewall.Does{Allows: true, StartsUFW: true}},
		"active, a port not let through": {site{plat: debian13(), ufw: on}, console, []string{"ufw allow 22/tcp"}, "", firewall.Does{Allows: true}},
	} {
		t.Run(name, func(t *testing.T) {
			l := liveOn(t, tc.s)
			r := firewall.Read(ctx, l, l.facts, nil, tc.session)
			if r.First != firewall.ItemEnable || r.PlanErr != nil || r.Does != tc.does {
				t.Errorf("read: item %d, %v, does %+v", r.First, r.PlanErr, r.Does)
			}
			e, p := planOn(t, l, firewall.ActionEnable, tc.session, nil)
			if p.Err() != nil {
				t.Fatal(p.Err())
			}
			if got := commands(p); !slices.Equal(got, tc.want) {
				t.Errorf("commands %q, want %q", got, tc.want)
			}
			var c engine.Change
			for _, x := range p.Changes {
				if x.ID == firewall.ID+"/firewall" {
					c = x
				}
			}
			if c.Risk != engine.RiskLockout || p.Risk != engine.RiskLockout || c.Stage != engine.StageFirewall {
				t.Errorf("risk %v %v, stage %v", c.Risk, p.Risk, c.Stage)
			}
			if got := undoWords(c); got != tc.undo {
				t.Errorf("journalled undo %q, want %q", got, tc.undo)
			}
			if _, err := e.Apply(ctx, p); err != nil {
				t.Fatal(err)
			}
			if got := ranUFW(l); !slices.Equal(got, tc.want) {
				t.Errorf("ran %q, want %q", got, tc.want)
			}
			_, again := planOn(t, l, firewall.ActionEnable, tc.session, nil)
			if again.Err() != nil || len(commands(again)) != 0 {
				t.Errorf("again: %q, %v", commands(again), again.Err())
			}
		})
	}
}

// TestEnableStray: a socket on 22 with every configured listener bound may be
// sshd's or another program's, as ss does not say. Enable refuses while ufw would
// refuse it, with a session or from a console, on a fresh ufw and under an allow
// policy the plan would change; with a session on 22 that port is kept, and with
// a saved allow for it there is nothing to refuse.
func TestEnableStray(t *testing.T) {
	const words = "Port 22 is bound, sshd's config doesn't list it, and ufw would refuse it. Reload sshd, stop what's on it, or run ufw allow 22/tcp, then try again."
	on2230 := revert.Session{Remote: true, Conn: "192.0.2.10 50022 192.0.2.20 2230", Source: "SSH_CONNECTION"}
	both := []string{"0.0.0.0:2230", "[::]:2230", "0.0.0.0:22", "[::]:22"}
	for name, tc := range map[string]struct {
		u       fw
		session revert.Session
		want    []string
	}{
		"a console":                {fw{}, console, nil},
		"a session on 2230":        {fw{}, on2230, nil},
		"an allow policy":          {fw{incoming: "allow"}, console, nil},
		"active, 2230 let through": {fw{active: true, rules: allow("2230")}, on2230, nil},
		"a session on 22 keeps it": {fw{}, remote22, []string{"ufw allow 22/tcp", "ufw allow 2230/tcp", "ufw --force enable"}},
		"a saved allow for 22":     {fw{rules: allow("22")}, on2230, []string{"ufw allow 2230/tcp", "ufw --force enable"}},
	} {
		t.Run(name, func(t *testing.T) {
			l := liveOn(t, site{plat: debian13(), ports: []uint16{2230}, bound: both, ufw: tc.u})
			_, p := planOn(t, l, firewall.ActionEnable, tc.session, nil)
			if tc.want == nil {
				refused(t, p, words, firewall.ErrRefused)
				changedNothing(t, l)
				return
			}
			if p.Err() != nil || !slices.Equal(commands(p), tc.want) {
				t.Errorf("plan %q, %v", commands(p), p.Err())
			}
		})
	}
}

func undoWords(c engine.Change) string {
	var out []string
	for _, s := range c.Rollback.Steps {
		out = append(out, "["+strings.Join(s.Argv, " ")+"]")
	}
	return strings.Join(out, " ")
}

// ranUFW are the commands that changed ufw or its boot service.
func ranUFW(l *live) []string {
	var out []string
	for _, argv := range l.Runs() {
		cmd := strings.Join(argv, " ")
		switch {
		case argv[0] == "ufw" && argv[1] != "status", cmd == "systemctl enable ufw.service", cmd == "systemctl disable ufw.service", cmd == "rc-update add ufw default", cmd == "rc-update del ufw default":
			out = append(out, cmd)
		}
	}
	return out
}

// TestEnableBoot: where ufw's service does not start at boot, a plain change
// before ufw's, with its undo, checked by reading again. ufw's stays lockout-risk;
// a plan holding the boot change alone is plain.
func TestEnableBoot(t *testing.T) {
	ctx := context.Background()
	alpine := alpine324()
	alpine.noSS = false
	for name, tc := range map[string]struct {
		plat      platform
		boot      string
		cmd, undo string
	}{
		"systemd, disabled":   {debian13(), "disabled", "systemctl enable ufw.service", "[systemctl disable ufw.service]"},
		"systemd, linked":     {debian13(), "linked", "systemctl enable ufw.service", "[systemctl disable ufw.service]"},
		"OpenRC, no runlevel": {alpine, "disabled", "rc-update add ufw default", "[rc-update del ufw default]"},
	} {
		t.Run(name, func(t *testing.T) {
			l := liveOn(t, site{plat: tc.plat, ufw: fw{boot: tc.boot}})
			r := firewall.Read(ctx, l, l.facts, nil, console)
			if !r.Does.Boot || r.Boot != detect.No || r.PlanErr != nil {
				t.Errorf("read: boot %v, does %+v, %v", r.Boot, r.Does, r.PlanErr)
			}
			e, p := planOn(t, l, firewall.ActionEnable, console, nil)
			if p.Err() != nil {
				t.Fatal(p.Err())
			}
			want := []string{tc.cmd, "ufw allow 22/tcp", "ufw --force enable"}
			if got := commands(p); !slices.Equal(got, want) {
				t.Fatalf("commands %q, want %q", got, want)
			}
			i := slices.IndexFunc(p.Changes, func(c engine.Change) bool { return c.ID == firewall.ID+"/boot" })
			if i < 0 || p.Changes[i+1].ID != firewall.ID+"/firewall" {
				t.Fatalf("changes %+v", p.Changes)
			}
			c := p.Changes[i]
			if c.Risk != engine.RiskPlain || c.Stage != engine.StageFirewall || c.Title != "Start ufw at boot" || undoWords(c) != tc.undo {
				t.Errorf("boot change %+v", c)
			}
			if p.Risk != engine.RiskLockout {
				t.Errorf("plan risk %v", p.Risk)
			}
			if _, err := e.Apply(ctx, p); err != nil {
				t.Fatal(err)
			}
			if got := ranUFW(l); !slices.Equal(got, want) {
				t.Errorf("ran %q, want %q", got, want)
			}
		})
		// The same host active and let through: the boot change alone, plain.
		t.Run(name+", boot alone", func(t *testing.T) {
			l := liveOn(t, site{plat: tc.plat, ufw: fw{active: true, rules: allow("22"), boot: tc.boot}})
			r := firewall.Read(ctx, l, l.facts, nil, console)
			if r.First != firewall.ItemEnable || !r.DisableBeside || r.Does != (firewall.Does{Boot: true}) {
				t.Errorf("read: item %d, beside %t, does %+v", r.First, r.DisableBeside, r.Does)
			}
			e, p := planOn(t, l, firewall.ActionEnable, console, nil)
			if p.Err() != nil || p.Risk != engine.RiskPlain {
				t.Fatalf("plan: %v, risk %v", p.Err(), p.Risk)
			}
			if got := commands(p); !slices.Equal(got, []string{tc.cmd}) {
				t.Errorf("commands %q", got)
			}
			if _, err := e.Apply(ctx, p); err != nil {
				t.Fatal(err)
			}
			if r = firewall.Read(ctx, l, l.facts, nil, console); r.First != firewall.ItemDisable || r.DisableBeside {
				t.Errorf("after: item %d, beside %t, %v", r.First, r.DisableBeside, r.Boot)
			}
		})
	}
	// Answered as done and still not enabled: the check says so, and it is undone.
	l := liveOn(t, site{plat: debian13(), ufw: fw{active: true, rules: allow("22"), boot: "disabled"}})
	l.bootStuck = true
	e, p := planOn(t, l, firewall.ActionEnable, console, nil)
	if _, err := e.Apply(ctx, p); !errors.Is(err, ufw.ErrVerify) {
		t.Errorf("apply: %v", err)
	}
	if got := ranUFW(l); !slices.Equal(got, []string{"systemctl enable ufw.service", "systemctl disable ufw.service"}) {
		t.Errorf("ran %q", got)
	}
}

// TestEnableRefusals: each refusal in its words, with its port or address.
func TestEnableRefusals(t *testing.T) {
	const (
		blocks      = "A saved ufw rule would block SSH on port 22, for some addresses or all. Remove it with ufw delete, then try again."
		ipv6        = "ufw's IPv6 is off in /etc/default/ufw, so it would refuse SSH on port 22. Set IPV6=yes, then try again."
		refused6    = "ufw refuses SSH on [::]:22, and hardn doesn't change a rule it finds. Fix it with ufw allow or ufw delete, then try again."
		refused4    = "ufw refuses SSH on 0.0.0.0:22, and hardn doesn't change a rule it finds. Fix it with ufw allow or ufw delete, then try again."
		bootNotRead = "hardn can't tell if ufw starts at boot. Check its service, then try again."
		masked      = "ufw's service is masked, so it can't start at boot. Run systemctl unmask ufw.service, then try again."
	)
	for name, tc := range map[string]struct {
		s     site
		words string
		want  error
	}{
		"a saved deny":   {site{plat: debian13(), ufw: fw{rules: []rule{{action: "deny", port: "22"}}}}, blocks, ufw.ErrBlocks},
		"a blocklist":    {site{plat: debian13(), ufw: fw{rules: []rule{{action: "deny", port: "22", src: "198.51.100.0/24"}, {action: "allow", port: "22"}}}}, blocks, ufw.ErrBlocks},
		"IPv6 off":       {site{plat: debian13(), ufw: fw{noV6: true}}, ipv6, ufw.ErrBlocks},
		"an active deny": {site{plat: debian13(), ufw: fw{active: true, rules: []rule{{action: "deny", port: "22"}}}}, refused6, firewall.ErrRefused},
		// Refused on IPv6 alone, set but for a change Enable makes: refused, as
		// before (an IPv6 refusal with nothing to change is TestEnableNothing's).
		"an allowlist of one family, boot off":  {site{plat: debian13(), ufw: fw{active: true, boot: "disabled", rules: []rule{{action: "allow", port: "22", src: "192.0.2.0/24"}}}}, refused6, firewall.ErrRefused},
		"IPv4 from loopback only, IPv6 nothing": {site{plat: debian13(), ufw: fw{active: true, rules: []rule{{action: "allow", port: "22", src: "127.0.0.1"}}}}, refused6, firewall.ErrRefused},
		"a dual-stack socket refused": {site{plat: ubuntu2604(), ufw: fw{active: true, noKernel6: true, rules: []rule{{action: "deny", port: "22"}}},
			failT: &hostfake.Response{Stdout: "port 22\nlistenaddress [::]:22\npermitrootlogin without-password\npubkeyauthentication yes\npasswordauthentication yes\nkbdinteractiveauthentication no\nusepam yes\n"}}, refused6, firewall.ErrRefused},
		"not read":                     {site{plat: debian13(), ufw: fw{active: true, garbage: true}}, "hardn can't read ufw's state. Check ufw status verbose, then try again.", ufw.ErrStateUnknown},
		"enabled, not running":         {site{plat: debian13(), ufw: fw{enabled: "yes"}}, "ufw is set to start at boot but isn't running. Check that its rules let SSH through, then run ufw enable.", ufw.ErrEnabledMismatch},
		"running, not enabled":         {site{plat: debian13(), ufw: fw{active: true, enabled: "no", rules: allow("22")}}, "ufw is running but isn't set to start at boot. Run ufw enable.", ufw.ErrEnabledMismatch},
		"boot not read, nothing else":  {site{plat: debian13(), ufw: fw{active: true, rules: allow("22"), boot: "fail"}}, bootNotRead, firewall.ErrBootUnknown},
		"boot not read, inactive":      {site{plat: debian13(), ufw: fw{boot: "fail"}}, bootNotRead, firewall.ErrBootUnknown},
		"boot static, a port to allow": {site{plat: debian13(), ufw: fw{active: true, boot: "static"}}, bootNotRead, firewall.ErrBootUnknown},
		"masked, inactive":             {site{plat: debian13(), ufw: fw{boot: "masked"}}, masked, firewall.ErrBootBlocked},
		"masked, active":               {site{plat: debian13(), ufw: fw{active: true, rules: allow("22"), boot: "masked"}}, masked, firewall.ErrBootBlocked},
		"masked for now":               {site{plat: debian13(), ufw: fw{boot: "masked-runtime"}}, masked, firewall.ErrBootBlocked},
		"no unit":                      {site{plat: debian13(), ufw: fw{boot: "not-found"}}, "ufw's service isn't on this system, so it can't start at boot. Reinstall ufw, then try again.", firewall.ErrBootBlocked},
		"denied, boot off":             {site{plat: debian13(), ufw: fw{active: true, boot: "disabled", rules: []rule{{action: "deny", port: "22"}}}}, refused6, firewall.ErrRefused},
		"IPv6 off, active, boot off":   {site{plat: debian13(), ufw: fw{active: true, noV6: true, boot: "disabled", rules: allow("22")}}, ipv6, firewall.ErrRefused},
		"from loopback only":           {site{plat: debian13(), ufw: fw{active: true, noKernel6: true, rules: []rule{{action: "allow", port: "22", src: "127.0.0.1"}}}}, refused4, firewall.ErrRefused},
		"to loopback only":             {site{plat: debian13(), ufw: fw{active: true, noKernel6: true, rules: []rule{{action: "allow", port: "22", dst: "127.0.0.1"}}}}, refused4, firewall.ErrRefused},
		"saved, from loopback only":    {site{plat: debian13(), ufw: fw{noKernel6: true, rules: []rule{{action: "allow", port: "22", src: "127.0.0.1"}}}}, blocks, ufw.ErrBlocks},
		"Proxmox VE not established":   {site{plat: debian13(), ufw: allow22, facts: func(f *detect.Facts) { f.Proxmox = detect.Proxmox{} }}, "hardn can't tell if this host runs Proxmox VE, where it leaves the firewall alone.", ufw.ErrProxmoxUnknown},
	} {
		t.Run(name, func(t *testing.T) {
			l := liveOn(t, tc.s)
			_, p := planOn(t, l, firewall.ActionEnable, console, nil)
			if p.Err() != nil && len(p.Items) == 1 && p.Items[0].Err == nil {
				t.Fatalf("refused by the engine: %v", p.Err())
			}
			refused(t, p, tc.words, tc.want)
			changedNothing(t, l)
			r := firewall.Read(context.Background(), l, l.facts, nil, console)
			if r.First != firewall.ItemEnable || screenkit.RefusalWords(r.PlanErr) != tc.words {
				t.Errorf("read: item %d, %v", r.First, r.PlanErr)
			}
		})
	}
}

// TestEnableNothing: nothing to plan on a host that is set is an empty plan,
// not a refusal; item 1 is Disable there.
func TestEnableNothing(t *testing.T) {
	for name, s := range map[string]site{
		"allowed":      {plat: debian13(), ufw: allow22},
		"an allowlist": {plat: debian13(), ufw: fw{active: true, noKernel6: true, rules: []rule{{action: "allow", port: "22", src: "192.0.2.0/24"}}}},
		"a blocklist":  {plat: debian13(), ufw: fw{active: true, rules: []rule{{action: "deny", port: "22", src: "198.51.100.0/24"}, {action: "allow", port: "22"}}}},
		// Refused on IPv6 alone while IPv4 gets in (the maintainer, 2026-11-08).
		"IPv6 off, IPv4 allowed":     {plat: debian13(), ufw: fw{active: true, noV6: true, rules: allow("22")}},
		"an allowlist of one family": {plat: debian13(), ufw: fw{active: true, rules: []rule{{action: "allow", port: "22", src: "192.0.2.0/24"}}}},
	} {
		t.Run(name, func(t *testing.T) {
			l := liveOn(t, s)
			_, p := planOn(t, l, firewall.ActionEnable, console, nil)
			if p.Err() != nil || !p.Empty() {
				t.Errorf("plan: %d changes, %v", len(p.Changes), p.Err())
			}
			r := firewall.Read(context.Background(), l, l.facts, nil, console)
			if r.First != firewall.ItemDisable || r.DisableBeside || r.Plan != nil || r.PlanErr != nil {
				t.Errorf("read: item %d, beside %t, %v, %v", r.First, r.DisableBeside, r.Plan, r.PlanErr)
			}
		})
	}
}

// TestEnableLetsIPv6Through: refused on IPv6 alone where no rule decides 22
// for IPv6, Enable has a change to make, ufw allow 22/tcp, so item 1 stays
// Enable, letting SSH through, though the row is no lockout risk.
func TestEnableLetsIPv6Through(t *testing.T) {
	l := liveOn(t, site{plat: debian13(), ufw: fw{active: true, table: rows("22/tcp", "ALLOW IN", "Anywhere")}})
	r := firewall.Read(context.Background(), l, l.facts, nil, console)
	if r.First != firewall.ItemEnable || r.PlanErr != nil || !r.Does.Allows || !slices.ContainsFunc(r.Plan, func(c engine.Change) bool { return slices.Contains(c.Commands, "ufw allow 22/tcp") }) {
		t.Errorf("read: item %d, %v, does %+v, plan %v", r.First, r.PlanErr, r.Does, r.Plan)
	}
}

// TestDisable: one command, plain, never a reset, its rules left saved; its
// undo ufw's enable; nothing on an inactive ufw.
func TestDisable(t *testing.T) {
	ctx := context.Background()
	l := liveOn(t, site{plat: debian13(), ufw: allow22})
	e, p := planOn(t, l, firewall.ActionDisable, remote22, nil)
	if p.Err() != nil || len(p.Changes) != 1 {
		t.Fatalf("plan: %d changes, %v", len(p.Changes), p.Err())
	}
	c := p.Changes[0]
	if c.ID != firewall.ID+"/disable" || c.Risk != engine.RiskPlain || p.Risk != engine.RiskPlain || c.Stage != engine.StageFirewall ||
		c.StateBefore != "active" || c.StateAfter != "inactive" || len(c.Findings) != 0 || undoWords(c) != "[ufw --force enable]" {
		t.Errorf("change %+v", c)
	}
	if got := commands(p); !slices.Equal(got, []string{"ufw disable"}) {
		t.Errorf("commands %q", got)
	}
	if _, err := e.Apply(ctx, p); err != nil {
		t.Fatal(err)
	}
	if got := ranUFW(l); !slices.Equal(got, []string{"ufw disable"}) {
		t.Errorf("ran %q", got)
	}
	if l.u.active || len(l.u.rules) != 1 {
		t.Errorf("after: active %t, rules %v", l.u.active, l.u.rules)
	}
	if _, p = planOn(t, l, firewall.ActionDisable, remote22, nil); p.Err() != nil || !p.Empty() {
		t.Errorf("again: %d changes, %v", len(p.Changes), p.Err())
	}
	// A broken ufw has a way off: a saved line not read refuses nothing here.
	l = liveOn(t, site{plat: debian13(), ufw: allow22, edit: func(h *hostfake.System) {
		h.AddFile(ufw.UserRules, []byte("### tuple ### garbage\n"), 0o640, 0, 0)
	}})
	if _, p = planOn(t, l, firewall.ActionDisable, remote22, nil); p.Err() != nil || len(p.Changes) != 1 {
		t.Errorf("a saved line not read: %d changes, %v", len(p.Changes), p.Err())
	}
}

// TestNoUFW: every action on a host with no ufw says so, in words naming Install,
// as hardn -w does with no item 1 to show it.
func TestNoUFW(t *testing.T) {
	for _, a := range []firewall.Action{firewall.ActionEnable, firewall.ActionDisable, firewall.ActionApplyProfiles} {
		t.Run(fmt.Sprint("action ", a), func(t *testing.T) {
			l := liveOn(t, site{plat: debian13(), ufw: fw{missing: true}})
			_, p := planOn(t, l, a, remote22, &config.Config{UfwAppProfiles: []config.UfwAppProfile{web}})
			refused(t, p, "ufw isn't installed. Install it, then try again.", ufw.ErrNotInstalled)
		})
	}
}

// TestDisableStateNotRead: Disable reads the status alone. A settings file that
// cannot be read leaves an active ufw its way off, and a status that cannot be
// read is refused in the decided words, as Apply is.
func TestDisableStateNotRead(t *testing.T) {
	l := liveOn(t, site{plat: debian13(), ufw: with(allow22, func(u *fw) { u.noDefault = true })})
	r := firewall.Read(context.Background(), l, l.facts, nil, remote22)
	if r.StateErr == nil || !r.DisableBeside {
		t.Errorf("read: %v, beside %t", r.StateErr, r.DisableBeside)
	}
	if _, p := planOn(t, l, firewall.ActionDisable, remote22, nil); p.Err() != nil || !slices.Equal(commands(p), []string{"ufw disable"}) {
		t.Errorf("no settings file: %q, %v", commands(p), p.Err())
	}
	failing := &hostfake.Response{Stderr: "ERROR: You need to be root to run this script\n", ExitCode: 1}
	const words = "hardn can't read ufw's state. Check ufw status verbose, then try again."
	for _, a := range []firewall.Action{firewall.ActionDisable, firewall.ActionApplyProfiles} {
		l = liveOn(t, site{plat: debian13(), ufw: fw{fail: failing}})
		_, p := planOn(t, l, a, remote22, &config.Config{UfwAppProfiles: []config.UfwAppProfile{web}})
		refused(t, p, words, ufw.ErrStateUnknown)
	}
	// An inactive ufw with no settings file: not active, so it is refused, not "nothing".
	l = liveOn(t, site{plat: debian13(), ufw: fw{noDefault: true}})
	_, p := planOn(t, l, firewall.ActionDisable, remote22, nil)
	refused(t, p, words, ufw.ErrStateUnknown)
}

// cfgOn writes hardn.yml on l and reads it, as the screen has it.
func cfgOn(t *testing.T, l *live, yml string) *config.Config {
	t.Helper()
	l.AddDir("/etc/hardn", 0o755, 0, 0)
	l.AddFile(hardnYML, []byte(yml), 0o600, 0, 0)
	cfg, err := config.Read(context.Background(), l, hardnYML)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

// TestApply: hardn's file written with a diff and a backup, root's and 0644,
// then a rule for each profile, in that order; a rule ufw kept not undone; the
// second plan empty; refused on an inactive ufw, and for a bad profile.
func TestApply(t *testing.T) {
	ctx := context.Background()
	cfg := &config.Config{UfwAppProfiles: []config.UfwAppProfile{web, dns}}
	const wantFile = "[Web]\ntitle=Web Server\ndescription=A web server\nports=80/tcp|443/tcp\n\n[DNS]\ntitle=DNS\ndescription=Names\nports=53/udp\n\n"
	l := liveOn(t, site{plat: debian13(), ufw: allow22})
	e, p := planOn(t, l, firewall.ActionApplyProfiles, remote22, cfg)
	if p.Err() != nil || len(p.Changes) != 1 {
		t.Fatalf("plan: %d changes, %v", len(p.Changes), p.Err())
	}
	c := p.Changes[0]
	wantCmds := []string{"ufw allow from any to any app Web", "ufw allow from any to any app DNS"}
	if c.ID != firewall.ID+"/apply" || c.Risk != engine.RiskPlain || c.Stage != engine.StageFirewall || c.Target != ufw.ProfileFile ||
		!slices.Equal(c.Commands, wantCmds) || !slices.Equal(c.Backups, []string{ufw.ProfileFile}) || !strings.Contains(c.Diff, "+ports=80/tcp|443/tcp") {
		t.Errorf("change %+v", c)
	}
	n := len(l.Calls())
	if _, err := e.Apply(ctx, p); err != nil {
		t.Fatal(err)
	}
	var order []string
	for _, x := range l.Calls()[n:] {
		switch {
		case x.Op == "write-atomic" && x.Path == ufw.ProfileFile:
			order = append(order, "write")
			if x.Mode.Perm() != 0o644 || x.UID != 0 || x.GID != 0 {
				t.Errorf("written %v %d:%d", x.Mode, x.UID, x.GID)
			}
		case x.Op == "run" && slices.Contains(x.Argv, "app"):
			order = append(order, x.Argv[7])
		}
	}
	if !slices.Equal(order, []string{"write", "Web", "DNS"}) {
		t.Errorf("order %q", order)
	}
	if data, _ := l.ReadFile(ctx, ufw.ProfileFile); string(data) != wantFile {
		t.Errorf("file:\n%s", data)
	}
	if _, p = planOn(t, l, firewall.ActionApplyProfiles, remote22, cfg); p.Err() != nil || !p.Empty() {
		t.Errorf("again: %d changes, %v", len(p.Changes), p.Err())
	}

	// A rule there already: not repeated, and not undone. Only the file differs.
	l = liveOn(t, site{plat: debian13(), ufw: fw{active: true, rules: []rule{{action: "allow", port: "22"}, {action: "allow", port: "80", app: "Web"}}}})
	_, p = planOn(t, l, firewall.ActionApplyProfiles, remote22, &config.Config{UfwAppProfiles: []config.UfwAppProfile{web}})
	if p.Err() != nil || len(p.Changes) != 1 || len(p.Changes[0].Commands) != 0 || p.Changes[0].Diff == "" {
		t.Errorf("the file alone: %+v, %v", p.Changes, p.Err())
	}
	// The file right, its mode not.
	l = liveOn(t, site{plat: debian13(), ufw: fw{active: true, rules: []rule{{action: "allow", port: "22"}, {action: "allow", port: "80", app: "Web"}}}})
	l.AddFile(ufw.ProfileFile, ufw.ProfileBytes([]ufw.Profile{{Name: web.Name, Title: web.Title, Description: web.Description, Ports: web.Ports}}), 0o666, 0, 0)
	_, p = planOn(t, l, firewall.ActionApplyProfiles, remote22, &config.Config{UfwAppProfiles: []config.UfwAppProfile{web}})
	if p.Err() != nil || len(p.Changes) != 1 || len(p.Changes[0].Backups) != 1 {
		t.Errorf("its mode: %+v, %v", p.Changes, p.Err())
	}

	// The file right, its owner not.
	l = liveOn(t, site{plat: debian13(), ufw: fw{active: true, rules: []rule{{action: "allow", port: "22"}, {action: "allow", port: "80", app: "Web"}}}})
	l.AddFile(ufw.ProfileFile, ufw.ProfileBytes([]ufw.Profile{{Name: web.Name, Title: web.Title, Description: web.Description, Ports: web.Ports}}), 0o644, 1000, 0)
	_, p = planOn(t, l, firewall.ActionApplyProfiles, remote22, &config.Config{UfwAppProfiles: []config.UfwAppProfile{web}})
	if p.Err() != nil || len(p.Changes) != 1 || len(p.Changes[0].Backups) != 1 {
		t.Errorf("its owner: %+v, %v", p.Changes, p.Err())
	}

	// Kept since the plan: ufw says so, and the journal holds no delete.
	l = liveOn(t, site{plat: debian13(), ufw: allow22})
	e, p = planOn(t, l, firewall.ActionApplyProfiles, remote22, &config.Config{UfwAppProfiles: []config.UfwAppProfile{web}})
	l.u.rules = append(l.u.rules, rule{action: "allow", port: "80", app: "Web"})
	l.fail = "ufw status verbose"
	_, err := e.Apply(ctx, p)
	if err == nil {
		t.Fatal("no failure")
	}
	if got := ranUFW(l); slices.Contains(got, "ufw delete allow from any to any app Web") {
		t.Errorf("a kept rule undone: %q", got)
	}

	// No profile plans nothing, on any ufw.
	l = liveOn(t, site{plat: debian13(), ufw: fw{}})
	if _, p = planOn(t, l, firewall.ActionApplyProfiles, remote22, &config.Config{}); p.Err() != nil || !p.Empty() {
		t.Errorf("no profile: %d changes, %v", len(p.Changes), p.Err())
	}
	// Inactive: refused, with its words.
	l = liveOn(t, site{plat: debian13(), ufw: fw{}})
	_, p = planOn(t, l, firewall.ActionApplyProfiles, remote22, cfg)
	refused(t, p, "ufw isn't running, so hardn saves no rule for it. Enable the firewall (Firewall, option 1), then apply.", ufw.ErrInactive)
	changedNothing(t, l)
	// A bad profile: refused, naming it.
	bad := web
	bad.Ports = []string{"80,443/tcp"}
	l = liveOn(t, site{plat: debian13(), ufw: allow22})
	_, p = planOn(t, l, firewall.ActionApplyProfiles, remote22, &config.Config{UfwAppProfiles: []config.UfwAppProfile{dns, bad}})
	refused(t, p, "ufw can't read the profile Web. Correct it in hardn.yml, then try again.", ufw.ErrBadProfile)
	changedNothing(t, l)
	// Named twice.
	l = liveOn(t, site{plat: debian13(), ufw: allow22})
	_, p = planOn(t, l, firewall.ActionApplyProfiles, remote22, &config.Config{UfwAppProfiles: []config.UfwAppProfile{web, web}})
	refused(t, p, "ufw can't read the profile Web. Correct it in hardn.yml, then try again.", ufw.ErrBadProfile)
}

// TestApplyProfileInterface: a profile limited to an interface is applied as
// its own rule, "allow in on <interface> ... app <name>", beside a rule of the
// application on every interface, which does not stand for it; once applied,
// Applied says so of it and the plan is empty.
func TestApplyProfileInterface(t *testing.T) {
	ctx := context.Background()
	onTS := web
	onTS.Interface = "tailscale0"
	cfg := &config.Config{UfwAppProfiles: []config.UfwAppProfile{onTS}}
	l := liveOn(t, site{plat: debian13(), ufw: fw{active: true, rules: []rule{{action: "allow", port: "22"}, {action: "allow", port: "80", app: "Web"}}}})
	e, p := planOn(t, l, firewall.ActionApplyProfiles, remote22, cfg)
	if p.Err() != nil || len(p.Changes) != 1 || !slices.Equal(p.Changes[0].Commands, []string{"ufw allow in on tailscale0 from any to any app Web"}) {
		t.Fatalf("plan: %+v, %v", p.Changes, p.Err())
	}
	if _, err := e.Apply(ctx, p); err != nil {
		t.Fatal(err)
	}
	if r := firewall.Read(ctx, l, l.facts, cfg, remote22); !r.Applied(onTS) || !r.Applied(web) {
		t.Errorf("applied: on tailscale0 %t, everywhere %t; rules %+v", r.Applied(onTS), r.Applied(web), r.State.Rules)
	}
	if _, p = planOn(t, l, firewall.ActionApplyProfiles, remote22, cfg); p.Err() != nil || !p.Empty() {
		t.Errorf("again: %d changes, %v", len(p.Changes), p.Err())
	}
	bad := onTS
	bad.Interface = "eth0:1"
	l = liveOn(t, site{plat: debian13(), ufw: allow22})
	_, p = planOn(t, l, firewall.ActionApplyProfiles, remote22, &config.Config{UfwAppProfiles: []config.UfwAppProfile{bad}})
	refused(t, p, "ufw can't read the profile Web. Correct it in hardn.yml, then try again.", ufw.ErrBadProfile)
	changedNothing(t, l)
}

// TestApplyInterfaces: each trusted interface's "ufw allow in on <interface>",
// one plain change; the second plan empty; refused on an inactive ufw, for a
// name ufw would not take, and for one named twice; none plans nothing; the
// save writes hardn.yml's key.
func TestApplyInterfaces(t *testing.T) {
	ctx := context.Background()
	cfg := &config.Config{UfwTrustedInterfaces: []string{"tailscale0", "wg0"}}
	l := liveOn(t, site{plat: debian13(), ufw: allow22})
	e, p := planOn(t, l, firewall.ActionApplyInterfaces, remote22, cfg)
	if p.Err() != nil || len(p.Changes) != 1 {
		t.Fatalf("plan: %d changes, %v", len(p.Changes), p.Err())
	}
	c := p.Changes[0]
	if c.ID != firewall.ID+"/interfaces" || c.Risk != engine.RiskPlain || c.Stage != engine.StageFirewall ||
		!slices.Equal(c.Commands, []string{"ufw allow in on tailscale0", "ufw allow in on wg0"}) {
		t.Errorf("change %+v", c)
	}
	if _, err := e.Apply(ctx, p); err != nil {
		t.Fatal(err)
	}
	if r := firewall.Read(ctx, l, l.facts, cfg, remote22); !r.InterfaceApplied("tailscale0") || !r.InterfaceApplied("wg0") || r.InterfaceApplied("eth0") {
		t.Errorf("applied: %+v", r.State.Rules)
	}
	if _, p = planOn(t, l, firewall.ActionApplyInterfaces, remote22, cfg); p.Err() != nil || !p.Empty() {
		t.Errorf("again: %d changes, %v", len(p.Changes), p.Err())
	}
	l = liveOn(t, site{plat: debian13(), ufw: fw{}})
	if _, p = planOn(t, l, firewall.ActionApplyInterfaces, remote22, &config.Config{}); p.Err() != nil || !p.Empty() {
		t.Errorf("none: %d changes, %v", len(p.Changes), p.Err())
	}
	l = liveOn(t, site{plat: debian13(), ufw: fw{}})
	_, p = planOn(t, l, firewall.ActionApplyInterfaces, remote22, cfg)
	refused(t, p, "ufw isn't running, so hardn saves no rule for it. Enable the firewall (Firewall, option 1), then apply.", ufw.ErrInactive)
	changedNothing(t, l)
	for _, ifaces := range [][]string{{"eth0:1"}, {"wg0", "wg0"}} {
		l = liveOn(t, site{plat: debian13(), ufw: allow22})
		_, p = planOn(t, l, firewall.ActionApplyInterfaces, remote22, &config.Config{UfwTrustedInterfaces: ifaces})
		refused(t, p, "ufw can't use the interface "+ifaces[len(ifaces)-1]+". Correct it in hardn.yml, then try again.", ufw.ErrBadInterface)
		changedNothing(t, l)
	}
}

// TestApplyRollback: a rule ufw refuses, after one it added: the file goes back,
// and the rule that was added is deleted, the kept one left.
func TestApplyRollback(t *testing.T) {
	ctx := context.Background()
	l := liveOn(t, site{plat: debian13(), ufw: allow22})
	l.fail = "ufw allow from any to any app DNS"
	e, p := planOn(t, l, firewall.ActionApplyProfiles, remote22, &config.Config{UfwAppProfiles: []config.UfwAppProfile{web, dns}})
	n := len(l.Calls())
	_, err := e.Apply(ctx, p)
	if err == nil {
		t.Fatal("no failure")
	}
	if _, rerr := l.Stat(ctx, ufw.ProfileFile); rerr == nil {
		t.Errorf("the file is left")
	}
	if slices.ContainsFunc(l.u.rules, func(r rule) bool { return r.app == "Web" }) {
		t.Errorf("rules %v", l.u.rules)
	}
	// A backup restored first, then the undo: docs/safety/ufw.md §4.
	var order []string
	for _, c := range l.Calls()[n:] {
		switch {
		case c.Op == "remove" && c.Path == ufw.ProfileFile:
			order = append(order, "file")
		case c.Op == "run" && len(c.Argv) > 1 && c.Argv[1] == "delete":
			order = append(order, "delete")
		}
	}
	if !slices.Equal(order, []string{"file", "delete"}) {
		t.Errorf("rollback %q", order)
	}
}

// TestSave: Add and Remove save hardn.yml alone, a bad profile refused first.
func TestSave(t *testing.T) {
	ctx := context.Background()
	const yml = "ufwAppProfiles:\n  - name: DNS\n    title: DNS\n    description: Names\n    ports:\n      - 53/udp\n"
	l := liveOn(t, site{plat: debian13(), ufw: fw{}})
	cfg := cfgOn(t, l, yml)
	cfg.UfwAppProfiles = append(cfg.UfwAppProfiles, web)
	e, p := planOn(t, l, firewall.ActionSaveProfiles, remote22, cfg)
	if p.Err() != nil || len(p.Changes) != 1 || p.Changes[0].ID != firewall.ID+"/profiles" || p.Risk == engine.RiskLockout {
		t.Fatalf("add: %+v, %v", p.Changes, p.Err())
	}
	if _, err := e.Apply(ctx, p); err != nil {
		t.Fatal(err)
	}
	if got := ranUFW(l); len(got) != 0 {
		t.Errorf("ran %q", got)
	}
	cfg, err := config.Read(ctx, l, hardnYML)
	if err != nil || len(cfg.UfwAppProfiles) != 2 || cfg.UfwAppProfiles[1].Name != "Web" || !slices.Equal(cfg.UfwAppProfiles[1].Ports, web.Ports) {
		t.Fatalf("after add: %+v, %v", cfg.UfwAppProfiles, err)
	}
	cfg.UfwAppProfiles = cfg.UfwAppProfiles[1:]
	e, p = planOn(t, l, firewall.ActionSaveProfiles, remote22, cfg)
	if p.Err() != nil || len(p.Changes) != 1 {
		t.Fatalf("remove: %+v, %v", p.Changes, p.Err())
	}
	if _, err = e.Apply(ctx, p); err != nil {
		t.Fatal(err)
	}
	if cfg, err = config.Read(ctx, l, hardnYML); err != nil || len(cfg.UfwAppProfiles) != 1 || cfg.UfwAppProfiles[0].Name != "Web" {
		t.Errorf("after remove: %+v, %v", cfg.UfwAppProfiles, err)
	}

	l = liveOn(t, site{plat: debian13(), ufw: fw{}})
	cfg = cfgOn(t, l, yml)
	cfg.UfwAppProfiles = append(cfg.UfwAppProfiles, config.UfwAppProfile{Name: "all", Title: "x", Description: "y", Ports: []string{"80/tcp"}})
	_, p = planOn(t, l, firewall.ActionSaveProfiles, remote22, cfg)
	refused(t, p, "ufw can't read the profile all. Correct it in hardn.yml, then try again.", ufw.ErrBadProfile)
	changedNothing(t, l)

	l = liveOn(t, site{plat: debian13(), ufw: fw{}})
	cfg = cfgOn(t, l, yml)
	cfg.UfwAppProfiles = append(cfg.UfwAppProfiles, config.UfwAppProfile{Name: "dns", Title: "x", Description: "y", Ports: []string{"80/tcp"}})
	_, p = planOn(t, l, firewall.ActionSaveProfiles, remote22, cfg)
	refused(t, p, "ufw can't read the profile dns. Correct it in hardn.yml, then try again.", ufw.ErrBadProfile)
	changedNothing(t, l)
}

// TestProxmoxNoUFW: every action, Check and Read on Proxmox VE, no ufw command,
// and of ufw's files ufw.conf alone, read by Read for decision 7's row.
func TestProxmoxNoUFW(t *testing.T) {
	ctx := context.Background()
	const words = "hardn reads pve-firewall and doesn't change it: a wrong rule can cut off the web UI and the cluster."
	s := site{plat: pve9(), ufw: fw{active: true, enabled: "yes", rules: allow("22")}, pve: pf{cluster: "[OPTIONS]\nenable: 1\n"}}
	noUFW := func(t *testing.T, l *live, conf bool) {
		t.Helper()
		for _, c := range l.Calls() {
			switch {
			case c.Op == "run" && (c.Argv[0] == "ufw" || strings.Contains(strings.Join(c.Argv, " "), "ufw")):
				t.Errorf("ran %v", c.Argv)
			case c.Op != "run" && strings.HasPrefix(c.Path, "/etc/ufw") && !(conf && c.Path == ufw.ConfFile) && !strings.HasPrefix(c.Path, "/etc/ufw/applications.d"):
				t.Errorf("%s %s", c.Op, c.Path)
			}
		}
	}
	for _, a := range []firewall.Action{firewall.ActionInstall, firewall.ActionEnable, firewall.ActionDisable, firewall.ActionSaveProfiles, firewall.ActionApplyProfiles} {
		t.Run(fmt.Sprint("action ", a), func(t *testing.T) {
			l := liveOn(t, s)
			_, p := planOn(t, l, a, remote22, &config.Config{UfwAppProfiles: []config.UfwAppProfile{web}})
			refused(t, p, words, ufw.ErrNotApplicable)
			noUFW(t, l, false)
		})
	}
	t.Run("check", func(t *testing.T) {
		l := liveOn(t, s)
		_, _ = firewall.Incoming{}.Check(ctx, l, l.facts)
		noUFW(t, l, false)
	})
	t.Run("read", func(t *testing.T) {
		l := liveOn(t, s)
		r := firewall.Read(ctx, l, l.facts, &config.Config{UfwAppProfiles: []config.UfwAppProfile{web}}, remote22)
		noUFW(t, l, true)
		if !r.OnPVE || r.First != firewall.ItemNone || r.DisableBeside || r.Plan != nil || r.PVEUFWEnabled != detect.Yes || r.PVEUFWErr != nil || len(r.Profiles) != 1 {
			t.Errorf("reading %+v", r)
		}
		if !r.Sockets.Read || !r.Sockets.Established() {
			t.Errorf("sockets %+v", r.Sockets)
		}
	})
	t.Run("read, ufw left off", func(t *testing.T) {
		l := liveOn(t, site{plat: pve9(), ufw: fw{missing: true}, pve: pf{cluster: "[OPTIONS]\nenable: 1\n"}})
		if r := firewall.Read(ctx, l, l.facts, nil, remote22); r.PVEUFWEnabled != detect.No || r.PVEUFWErr != nil {
			t.Errorf("reading %+v", r)
		}
	})
}

// TestReadChangesNothing: Read writes nothing and runs only reads, on each kind
// of host; the list is here.
func TestReadChangesNothing(t *testing.T) {
	ctx := context.Background()
	alpine := alpine324()
	alpine.noSS = false
	for name, s := range map[string]site{
		"no ufw":     {plat: debian13(), ufw: fw{missing: true}},
		"inactive":   {plat: debian13(), ufw: fw{}},
		"set":        {plat: debian13(), ufw: allow22},
		"boot off":   {plat: debian13(), ufw: fw{active: true, rules: allow("22"), boot: "disabled"}},
		"socket":     {plat: ubuntu2604(), ufw: allow22},
		"Alpine":     {plat: alpine, ufw: fw{}},
		"Proxmox VE": {plat: pve9(), pve: pf{cluster: "[OPTIONS]\nenable: 1\n"}},
	} {
		t.Run(name, func(t *testing.T) {
			l := liveOn(t, s)
			firewall.Read(ctx, l, l.facts, nil, remote22)
			for _, c := range l.Calls() {
				switch c.Op {
				case "read", "read-nofollow", "stat", "lstat", "readdir", "readlink", "resolve-symlink", "lookup-user", "lookup-group":
				case "run":
					switch cmd := strings.Join(c.Argv, " "); {
					case cmd == "ufw status verbose", cmd == "ss -Htln", cmd == "systemctl is-enabled ufw.service",
						c.Argv[0] == "dpkg-query", cmd == "apt-get install -s -y ufw", cmd == "apk info -e ufw", cmd == "apk add --simulate ufw",
						cmd == "systemctl show -p Listen ssh.socket", strings.Contains(cmd, "sshd -T") || strings.HasSuffix(c.Argv[0], "sshd"),
						strings.HasPrefix(cmd, "systemctl is-active "):
					default:
						t.Errorf("ran %q", cmd)
					}
				default:
					t.Errorf("a call that is not a read: %v", c)
				}
			}
		})
	}
}

// TestReadFirst: item 1 as the gallery's first() has it.
func TestReadFirst(t *testing.T) {
	ctx := context.Background()
	noSS := debian13()
	noSS.noSS = true
	for name, tc := range map[string]struct {
		s      site
		first  firewall.Item
		beside bool
	}{
		"no ufw":                  {site{plat: debian13(), ufw: fw{missing: true}}, firewall.ItemInstall, false},
		"inactive":                {site{plat: debian13(), ufw: fw{}}, firewall.ItemEnable, false},
		"set":                     {site{plat: debian13(), ufw: allow22}, firewall.ItemDisable, false},
		"allow policy":            {site{plat: debian13(), ufw: fw{active: true, incoming: "allow", rules: allow("22")}}, firewall.ItemEnable, true},
		"refused":                 {site{plat: debian13(), ufw: on}, firewall.ItemEnable, true},
		"not in effect, active":   {site{plat: debian13(), ports: []uint16{2230}, bound: stale22, ufw: allow22}, firewall.ItemEnable, true},
		"set, ss not read":        {site{plat: noSS, ufw: allow22}, firewall.ItemInstallSS, true},
		"Proxmox VE":              {site{plat: pve9(), pve: pf{cluster: "[OPTIONS]\nenable: 1\n"}}, firewall.ItemNone, false},
		"Proxmox not established": {site{plat: debian13(), ufw: allow22, facts: func(f *detect.Facts) { f.Proxmox = detect.Proxmox{} }}, firewall.ItemEnable, false},
		"a status row not read":   {site{plat: debian13(), ufw: fw{active: true, noKernel6: true, table: rows("22/tcp", "ALLOW IN", "Anywhere") + "garbage ALLOW\n"}}, firewall.ItemEnable, true},
		"no Logging line":         {site{plat: debian13(), ufw: fw{fail: &hostfake.Response{Stdout: "Status: active\nDefault: deny (incoming), allow (outgoing), disabled (routed)\n"}}}, firewall.ItemEnable, false},
		"ufw status fails":        {site{plat: debian13(), ufw: fw{fail: &hostfake.Response{Stderr: "ERROR: You need to be root to run this script\n", ExitCode: 1}}}, firewall.ItemEnable, false},
	} {
		t.Run(name, func(t *testing.T) {
			l := liveOn(t, tc.s)
			r := firewall.Read(ctx, l, l.facts, nil, remote22)
			if r.First != tc.first || r.DisableBeside != tc.beside {
				t.Errorf("item %d, beside %t", r.First, r.DisableBeside)
			}
		})
	}
}

// TestReadProxmoxUnknown: with Proxmox VE not established nothing below it is
// read, sshd and ss included.
func TestReadProxmoxUnknown(t *testing.T) {
	l := liveOn(t, site{plat: debian13(), ufw: allow22, facts: func(f *detect.Facts) { f.Proxmox = detect.Proxmox{} }})
	r := firewall.Read(context.Background(), l, l.facts, nil, remote22)
	if !errors.Is(r.PlanErr, ufw.ErrProxmoxUnknown) || r.Sockets.Read {
		t.Errorf("reading %+v", r)
	}
	if runs := l.Runs(); len(runs) != 0 {
		t.Errorf("ran %q", runs)
	}
}

// TestApplied: a profile's own saved rule, by its name, a space in it as ufw
// holds it (docs/safety/ufw.md §3), on its interface or on every one as the
// profile says (ufw.AppApplied).
func TestApplied(t *testing.T) {
	r := firewall.Reading{State: ufw.State{Rules: []ufw.Rule{{Action: "allow", Dir: "in", App: "Web", Family: ufw.IPv4}, {Action: "allow", Dir: "in", App: "Mail%20Server", Family: ufw.IPv4},
		{Action: "allow", Dir: "in", App: "Tailscale", Iface: "tailscale0", Family: ufw.IPv4}}}}
	for _, tc := range []struct {
		p    config.UfwAppProfile
		want bool
	}{
		{config.UfwAppProfile{Name: "Web"}, true}, {config.UfwAppProfile{Name: "Mail Server"}, true}, {config.UfwAppProfile{Name: "DNS"}, false},
		{config.UfwAppProfile{Name: "Tailscale", Interface: "tailscale0"}, true}, {config.UfwAppProfile{Name: "Tailscale"}, false},
		{config.UfwAppProfile{Name: "Web", Interface: "tailscale0"}, false},
	} {
		if got := r.Applied(tc.p); got != tc.want {
			t.Errorf("%+v: %t", tc.p, got)
		}
	}
}

// TestSockets: the exported reading, in the form ss prints on Debian 13
// (a scope in an address, a program on another port), and NoneBound.
func TestSockets(t *testing.T) {
	ctx := context.Background()
	const real = "LISTEN 0      4096         0.0.0.0:5355 0.0.0.0:*\nLISTEN 0      4096   127.0.0.53%lo:53   0.0.0.0:*\n" +
		"LISTEN 0      128          0.0.0.0:22   0.0.0.0:*\nLISTEN 0      4096      127.0.0.54:53   0.0.0.0:*\n" +
		"LISTEN 0      4096            [::]:5355    [::]:*\nLISTEN 0      128             [::]:22      [::]:*\n"
	l := liveOn(t, site{plat: debian13(), ufw: allow22, ssErr: &hostfake.Response{Stdout: real}})
	sk := firewall.ReadSockets(ctx, l, l.facts)
	if !sk.Read || !sk.Established() || sk.NoSS || sk.SSFailed || sk.Why != "" || len(sk.Unbound) != 0 || len(sk.Bound) != 6 || sk.NoneBound() {
		t.Errorf("sockets %+v", sk)
	}
	if ls, ok := sk.Listeners(); !ok || !slices.Equal(ls.Ports(), []uint16{22}) {
		t.Errorf("listeners %v", ls.Ports())
	}

	l = liveOn(t, site{plat: debian13(), ports: []uint16{2230}, bound: []string{"0.0.0.0:80"}, ufw: fw{}})
	if sk = firewall.ReadSockets(ctx, l, l.facts); !sk.NoneBound() || sk.Established() || !slices.Equal(sk.Ports(), []uint16{22, 2230}) {
		t.Errorf("nothing on SSH's ports: %+v", sk)
	}
	l = liveOn(t, site{plat: debian13(), ports: []uint16{2230}, bound: []string{"0.0.0.0:2230", "[::]:2230", "0.0.0.0:22"}, ufw: fw{}})
	if sk = firewall.ReadSockets(ctx, l, l.facts); !sk.Established() || !sk.Stray(22) || sk.Stray(2230) {
		t.Errorf("a stray: %+v", sk)
	}
	if sk = firewall.ReadSockets(ctx, liveOn(t, site{plat: noSS(debian13()), ufw: fw{}}), l.facts); !sk.NoSS || sk.SSFailed {
		t.Errorf("no ss: %+v", sk)
	}
}

// TestRollback: a step that fails undoes what ran, in reverse order: Enable's
// and the boot change's, with the session open.
func TestRollback(t *testing.T) {
	ctx := context.Background()
	for name, tc := range map[string]struct {
		s    site
		fail string
		want []string
	}{
		"enable fails": {site{plat: debian13(), ufw: fw{incoming: "allow"}}, "ufw --force enable", []string{
			"ufw allow 22/tcp", "ufw default deny incoming", "ufw --force enable",
			"ufw disable", "ufw default allow incoming", "ufw delete allow 22/tcp"}},
		"the policy fails": {site{plat: debian13(), ufw: fw{incoming: "allow"}}, "ufw default deny incoming", []string{
			"ufw allow 22/tcp", "ufw default deny incoming",
			"ufw default allow incoming", "ufw delete allow 22/tcp"}},
		"the boot change fails": {site{plat: debian13(), ufw: fw{boot: "disabled"}}, "systemctl enable ufw.service", []string{
			"systemctl enable ufw.service", "systemctl disable ufw.service"}},
		"enable fails, boot off": {site{plat: debian13(), ufw: fw{boot: "disabled"}}, "ufw --force enable", []string{
			"systemctl enable ufw.service", "ufw allow 22/tcp", "ufw --force enable",
			"ufw disable", "ufw delete allow 22/tcp", "systemctl disable ufw.service"}},
	} {
		t.Run(name, func(t *testing.T) {
			l := liveOn(t, tc.s)
			l.fail = tc.fail
			e, p := planOn(t, l, firewall.ActionEnable, console, nil)
			if p.Err() != nil {
				t.Fatal(p.Err())
			}
			_, err := e.Apply(ctx, p)
			if err == nil {
				t.Fatal("no failure")
			}
			if got := ranUFW(l); !slices.Equal(got, tc.want) {
				t.Errorf("ran\n got %q\nwant %q", got, tc.want)
			}
		})
	}
}

// TestTimerRollback: the timer's rollback of an Enable that ran, where the boot
// change's undo fails: ufw is stopped first, as the undo that reverses a lockout
// must not wait on a service manager.
func TestTimerRollback(t *testing.T) {
	ctx := context.Background()
	alpine := alpine324()
	alpine.noSS = false
	for name, tc := range map[string]struct {
		plat platform
		undo string
	}{
		"systemd": {debian13(), "systemctl disable ufw.service"},
		"OpenRC":  {alpine, "rc-update del ufw default"},
	} {
		t.Run(name, func(t *testing.T) {
			l := liveOn(t, site{plat: tc.plat, ufw: fw{boot: "disabled"}})
			e, p := planOn(t, l, firewall.ActionEnable, remote22, nil)
			res, err := e.Apply(ctx, p)
			if err != nil {
				t.Fatal(err)
			}
			n := len(ranUFW(l))
			l.fail = tc.undo
			_, err = revert.Rollback(ctx, l, res.ID, revert.Options{Now: func() time.Time { return time.Date(2026, 11, 2, 12, 1, 0, 0, time.UTC) }, Hardn: hardnBinary, Reason: revert.ReasonTimeout})
			if err == nil {
				t.Fatal("the undo that fails: no error")
			}
			if l.u.active {
				t.Error("ufw left active")
			}
			if got, want := ranUFW(l)[n:], []string{"ufw disable", "ufw delete allow 22/tcp", tc.undo}; !slices.Equal(got, want) {
				t.Errorf("rollback ran %q, want %q", got, want)
			}
		})
	}
}

// installOn is a site with no ufw whose package tool answers as sim says, the
// install itself bringing ufw.
func installOn(t *testing.T, p platform, sim hostfake.Response) *live {
	t.Helper()
	l := liveOn(t, site{plat: p, ufw: fw{missing: true}})
	inner := l.Handler
	l.Handler = func(argv []string, stdin []byte) hostfake.Response {
		switch strings.Join(argv, " ") {
		case "apt-get install -s -y ufw", "apk add --simulate ufw":
			return sim
		case "apt-get install -y ufw", "apk add ufw":
			l.u.missing = false
			return hostfake.Response{Stdout: "ufw installed\n"}
		case "apt-get remove -y ufw", "apk del ufw":
			l.u.missing = true
			return hostfake.Response{}
		}
		return inner(argv, stdin)
	}
	return l
}

// The simulations, in the forms of deb13-vm-2.log and, for apk's success, of
// pkg/host/alpine's tests ([I]: no VM has shown it for ufw).
var (
	aptSim = hostfake.Response{Stdout: "Reading package lists...\nThe following NEW packages will be installed:\n  ufw\nInst ufw (0.36.2-9 Debian:13.7/stable [all])\nConf ufw (0.36.2-9 Debian:13.7/stable [all])\n"}
	apkSim = hostfake.Response{Stdout: "(1/1) Installing ufw (0.36.2-r0)\nOK: 9 MiB in 26 packages\n"}
)

// TestInstallSS: Install iproute2's plan on each tool: one package-install
// change, the tool's command, at the packages stage; none where iproute2 is
// installed.
func TestInstallSS(t *testing.T) {
	deb, alp := debian13(), alpine324()
	deb.noSS, alp.noSS = true, true
	for _, tc := range []struct {
		plat platform
		cmd  string
	}{{deb, "apt-get install -y iproute2"}, {alp, "apk add iproute2"}} {
		l := liveOn(t, site{plat: tc.plat, ufw: allow22})
		_, p := planOn(t, l, firewall.ActionInstallSS, console, nil)
		if p.Err() != nil || len(p.Changes) != 1 {
			t.Fatalf("%s: %d changes, %v", tc.cmd, len(p.Changes), p.Err())
		}
		c := p.Changes[0]
		if c.ID != firewall.ID+"/iproute2" || c.Target != "iproute2" || c.Risk != engine.RiskPackageInstall || c.Stage != engine.StagePackages || !slices.Equal(c.Commands, []string{tc.cmd}) {
			t.Errorf("%s: change %+v", tc.cmd, c)
		}
	}
	l := liveOn(t, site{plat: debian13(), ufw: allow22})
	if _, p := planOn(t, l, firewall.ActionInstallSS, console, nil); p.Err() != nil || !p.Empty() {
		t.Errorf("installed: %d changes, %v", len(p.Changes), p.Err())
	}
	// iproute2 installed, ss gone: nothing to install, said as a refusal.
	l = liveOn(t, site{plat: deb, iproute2: true, ufw: allow22})
	_, p := planOn(t, l, firewall.ActionInstallSS, console, nil)
	refused(t, p, "iproute2 is installed, but its ss isn't there. Reinstall iproute2, then try again.", firewall.ErrNoSS)
	changedNothing(t, l)
}

// TestInstall: through the engine on each tool: the tool's command, one
// package-install change, verified, its undo the tool's, a plain y/N.
func TestInstall(t *testing.T) {
	for _, tt := range []struct {
		name       string
		plat       platform
		sim        hostfake.Response
		argv, undo string
	}{
		{"apt", debian13(), aptSim, "apt-get install -y ufw", "apt-get remove -y ufw"},
		{"apk", alpine324(), apkSim, "apk add ufw", "apk del ufw"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			l := installOn(t, tt.plat, tt.sim)
			e, p := planOn(t, l, firewall.ActionInstall, console, nil)
			if p.Err() != nil || !slices.Equal(commands(p), []string{tt.argv}) || p.Risk != engine.RiskPackageInstall {
				t.Fatalf("plan: %v, %q, risk %v", p.Err(), commands(p), p.Risk)
			}
			c := p.Changes[0]
			if c.ID != firewall.ID+"/install" || c.Title != "Install ufw" || c.Stage != engine.StagePackages || c.StateBefore != "not installed" || c.StateAfter != "installed, inactive" {
				t.Errorf("change %+v", c)
			}
			if got := undoWords(c); !strings.Contains(got, tt.undo) {
				t.Errorf("undo %q, want %q", got, tt.undo)
			}
			if _, err := e.Apply(context.Background(), p); err != nil {
				t.Fatal(err)
			}
			if l.u.missing {
				t.Error("ufw not installed")
			}
		})
	}
}

// TestInstallInstalled: ufw there plans nothing, and simulates nothing.
func TestInstallInstalled(t *testing.T) {
	l := installOn(t, debian13(), aptSim)
	l.u.missing = false
	_, p := planOn(t, l, firewall.ActionInstall, console, nil)
	if p.Err() != nil || len(p.Changes) != 0 {
		t.Errorf("plan: %v, %q", p.Err(), commands(p))
	}
	for _, c := range l.Calls() {
		if c.Op == "run" && c.Argv[0] == "apt-get" {
			t.Errorf("ran %v", c.Argv)
		}
	}
}

// TestInstallRefusals: each in its words before anything changes, each error
// the registry's.
func TestInstallRefusals(t *testing.T) {
	notFound := hostfake.Response{Stdout: "Reading package lists...\n", Stderr: "E: Unable to locate package ufw\n", ExitCode: 100}
	noIndex := hostfake.Response{Stderr: "ERROR: unable to select packages:\n  ufw (no such package):\n    required by: world[ufw]\n", ExitCode: 1}
	remv := hostfake.Response{Stdout: "Remv nftables [1.1.3-1]\nRemv libnftables1 [1.1.3-1]\nInst ufw (0.36.2-9 Debian:13.7/stable [all])\nConf ufw (0.36.2-9 Debian:13.7/stable [all])\n"}
	upgrade := hostfake.Response{Stdout: "(1/2) Upgrading musl (1.2.5-r0 -> 1.2.5-r1)\n(2/2) Installing ufw (0.36.2-r0)\n"}
	for _, tt := range []struct {
		name  string
		plat  platform
		sim   hostfake.Response
		words string
		want  error
		code  string
	}{
		{"apt finds none", debian13(), notFound, "hardn can't find ufw in apt's package lists. Run apt-get update, then try again.", packages.ErrNotInRepos, "PACKAGE_NOT_IN_REPOS"},
		{"apk finds none", alpine324(), noIndex, "hardn can't find ufw in apk's package lists. Check the network and the community repository, run apk update, then try again.", packages.ErrNotInRepos, "PACKAGE_NOT_IN_REPOS"},
		{"a removal", debian13(), remv, "Installing ufw would also change other packages: nftables, libnftables1. Install it by hand, then try again.", packages.ErrOtherPackages, "PACKAGE_OTHER_CHANGES"},
		{"an upgrade", alpine324(), upgrade, "Installing ufw would also change other packages: musl. Install it by hand, then try again.", packages.ErrOtherPackages, "PACKAGE_OTHER_CHANGES"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			l := installOn(t, tt.plat, tt.sim)
			_, p := planOn(t, l, firewall.ActionInstall, console, nil)
			refused(t, p, tt.words, tt.want)
			if got := output.CodeOf(p.Err()); got != tt.code {
				t.Errorf("code %s, want %s", got, tt.code)
			}
			for _, c := range l.Calls() {
				if c.Op == "run" && slices.Contains([]string{"apt-get install -y ufw", "apk add ufw"}, strings.Join(c.Argv, " ")) {
					t.Errorf("ran %v", c.Argv)
				}
			}
		})
	}
	t.Run("no tool", func(t *testing.T) {
		l := installOn(t, debian13(), aptSim)
		l.facts.Platform.Distro = detect.DistroOther
		_, p := planOn(t, l, firewall.ActionInstall, console, nil)
		refused(t, p, "hardn can't install packages on this system.", packages.ErrNoTool)
	})
}

// TestInstallFails: a failed install is rolled back by its tool, whether the
// command failed or did not read as installed, by the journal's undo replaced with
// Report.Undo; one not found has done nothing, and has nothing to undo.
func TestInstallFails(t *testing.T) {
	for name, tc := range map[string]struct {
		plat      platform
		sim       hostfake.Response
		cmd, undo string
		answer    hostfake.Response
		code      string
		undone    bool
	}{
		"apt: not read as installed": {debian13(), aptSim, "apt-get install -y ufw", "apt-get remove -y ufw", hostfake.Response{Stdout: "ufw installed\n"}, "PACKAGE_VERIFY_FAILED", true},
		"apk: not read as installed": {alpine324(), apkSim, "apk add ufw", "apk del ufw", hostfake.Response{Stdout: "ufw installed\n"}, "PACKAGE_VERIFY_FAILED", true},
		"apt: the command fails":     {debian13(), aptSim, "apt-get install -y ufw", "apt-get remove -y ufw", hostfake.Response{Stderr: "E: Sub-process failed\n", ExitCode: 100}, "", true},
		"apt: not found":             {debian13(), aptSim, "apt-get install -y ufw", "apt-get remove -y ufw", hostfake.Response{Err: host.ErrNotFound, ExitCode: -1}, "", false},
	} {
		t.Run(name, func(t *testing.T) {
			l := installOn(t, tc.plat, tc.sim)
			inner := l.Handler
			l.Handler = func(argv []string, stdin []byte) hostfake.Response {
				if strings.Join(argv, " ") == tc.cmd {
					return tc.answer
				}
				return inner(argv, stdin)
			}
			e, p := planOn(t, l, firewall.ActionInstall, console, nil)
			_, err := e.Apply(context.Background(), p)
			if err == nil {
				t.Fatal("no failure")
			}
			if tc.code != "" && (!errors.Is(err, packages.ErrVerify) || output.CodeOf(err) != tc.code) {
				t.Errorf("apply: %v", err)
			}
			var ran []string
			for _, argv := range l.Runs() {
				ran = append(ran, strings.Join(argv, " "))
			}
			if got := slices.Contains(ran, tc.undo); got != tc.undone {
				t.Errorf("undo %q ran %t, want %t: %q", tc.undo, got, tc.undone, ran)
			}
		})
	}
}

// TestInstallNames: the plan says the other new packages the simulation lists.
func TestInstallNames(t *testing.T) {
	sim := hostfake.Response{Stdout: "Inst iptables (1.8.11-2 Debian:13.7/stable [amd64])\nInst ufw (0.36.2-9 Debian:13.7/stable [all])\nConf iptables (1.8.11-2 Debian:13.7/stable [amd64])\nConf ufw (0.36.2-9 Debian:13.7/stable [all])\n"}
	_, p := planOn(t, installOn(t, debian13(), sim), firewall.ActionInstall, console, nil)
	if p.Err() != nil || p.Changes[0].StateAfter != "installed, inactive, with iptables" {
		t.Errorf("plan: %v, %+v", p.Err(), p.Changes)
	}
}

// TestReadInstall: Read plans Install where item 1 is Install: its refusal is
// there before asking, and nothing changes.
func TestReadInstall(t *testing.T) {
	l := installOn(t, alpine324(), hostfake.Response{Stderr: "ERROR: unable to select packages:\n  ufw (no such package):\n", ExitCode: 1})
	r := firewall.Read(context.Background(), l, l.facts, nil, console)
	if r.First != firewall.ItemInstall || !errors.Is(r.PlanErr, packages.ErrNotInRepos) || len(r.Plan) != 0 {
		t.Errorf("reading %+v", r)
	}
	l = installOn(t, debian13(), aptSim)
	r = firewall.Read(context.Background(), l, l.facts, nil, console)
	if r.First != firewall.ItemInstall || r.PlanErr != nil || len(r.Plan) != 1 {
		t.Errorf("reading %+v", r)
	}
	changedNothing(t, l)
}
