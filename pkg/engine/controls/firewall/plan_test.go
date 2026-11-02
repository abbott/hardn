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
		case cmd == "systemctl is-enabled ufw.service" && l.u.boot == "":
			return hostfake.Response{Stdout: "enabled\n"}
		case cmd == "systemctl is-enabled ufw.service" && l.u.boot != "fail":
			return hostfake.Response{Stdout: l.u.boot + "\n", ExitCode: 1}
		case argv[0] == "ufw" && l.u.missing:
			return inner(argv, stdin)
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
		l.u.rules = slices.DeleteFunc(l.u.rules, func(r rule) bool { return r.app == argv[8] })
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

// TestEnableStray pins decision 2's edge: a socket on 22 with every configured
// listener bound is not refused, as ss does not say whose it is. From a console
// the plan lets the configured 2230 through and not 22, so ufw refuses that
// socket once it starts; 2230 stays reachable, and with a session on 22 the
// session's port is kept (TestEnableOrder). The next Enable then names it.
func TestEnableStray(t *testing.T) {
	ctx := context.Background()
	l := liveOn(t, site{plat: debian13(), ports: []uint16{2230}, bound: []string{"0.0.0.0:2230", "[::]:2230", "0.0.0.0:22", "[::]:22"}, ufw: fw{}})
	e, p := planOn(t, l, firewall.ActionEnable, console, nil)
	if p.Err() != nil {
		t.Fatal(p.Err())
	}
	if got := commands(p); !slices.Equal(got, []string{"ufw allow 2230/tcp", "ufw --force enable"}) {
		t.Errorf("commands %q", got)
	}
	if _, err := e.Apply(ctx, p); err != nil {
		t.Fatal(err)
	}
	_, p = planOn(t, l, firewall.ActionEnable, console, nil)
	refused(t, p, "ufw refuses SSH on 0.0.0.0:22, and hardn doesn't change a rule it finds. Fix it with ufw allow or ufw delete, then try again.", firewall.ErrRefused)
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
// after ufw's, with its undo, checked by reading again. ufw's stays lockout-risk;
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
		"systemd, masked":     {debian13(), "masked", "systemctl enable ufw.service", "[systemctl disable ufw.service]"},
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
			want := []string{"ufw allow 22/tcp", "ufw --force enable", tc.cmd}
			if got := commands(p); !slices.Equal(got, want) {
				t.Fatalf("commands %q, want %q", got, want)
			}
			c := p.Changes[len(p.Changes)-1]
			if c.ID != firewall.ID+"/boot" || c.Risk != engine.RiskPlain || c.Stage != engine.StageFirewall || c.Title != "Start ufw at boot" ||
				!slices.Equal(c.After, []string{firewall.ID + "/firewall"}) || undoWords(c) != tc.undo {
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
		blocks   = "A saved ufw rule would block SSH on port 22, for some addresses or all. Remove it with ufw delete, then try again."
		ipv6     = "ufw's IPv6 is off in /etc/default/ufw, so it would refuse SSH on port 22. Set IPV6=yes, then try again."
		refused6 = "ufw refuses SSH on [::]:22, and hardn doesn't change a rule it finds. Fix it with ufw allow or ufw delete, then try again."
	)
	for name, tc := range map[string]struct {
		s     site
		words string
		want  error
	}{
		"a saved deny":               {site{plat: debian13(), ufw: fw{rules: []rule{{action: "deny", port: "22"}}}}, blocks, ufw.ErrBlocks},
		"a blocklist":                {site{plat: debian13(), ufw: fw{rules: []rule{{action: "deny", port: "22", src: "198.51.100.0/24"}, {action: "allow", port: "22"}}}}, blocks, ufw.ErrBlocks},
		"IPv6 off":                   {site{plat: debian13(), ufw: fw{noV6: true}}, ipv6, ufw.ErrBlocks},
		"IPv6 off, active":           {site{plat: debian13(), ufw: fw{active: true, noV6: true, rules: allow("22")}}, ipv6, firewall.ErrRefused},
		"an allowlist of one family": {site{plat: debian13(), ufw: fw{active: true, rules: []rule{{action: "allow", port: "22", src: "192.0.2.0/24"}}}}, refused6, firewall.ErrRefused},
		"an active deny":             {site{plat: debian13(), ufw: fw{active: true, rules: []rule{{action: "deny", port: "22"}}}}, refused6, firewall.ErrRefused},
		"a dual-stack socket refused": {site{plat: ubuntu2604(), ufw: fw{active: true, noKernel6: true, rules: []rule{{action: "deny", port: "22"}}},
			failT: &hostfake.Response{Stdout: "port 22\nlistenaddress [::]:22\npermitrootlogin without-password\npubkeyauthentication yes\npasswordauthentication yes\nkbdinteractiveauthentication no\nusepam yes\n"}}, refused6, firewall.ErrRefused},
		"not read":                    {site{plat: debian13(), ufw: fw{active: true, garbage: true}}, "hardn can't read ufw's state. Check ufw status verbose, then try again.", ufw.ErrStateUnknown},
		"enabled, not running":        {site{plat: debian13(), ufw: fw{enabled: "yes"}}, "ufw is set to start at boot but isn't running. Check that its rules let SSH through, then run ufw enable.", ufw.ErrEnabledMismatch},
		"running, not enabled":        {site{plat: debian13(), ufw: fw{active: true, enabled: "no", rules: allow("22")}}, "ufw is running but isn't set to start at boot. Run ufw enable.", ufw.ErrEnabledMismatch},
		"boot not read, nothing else": {site{plat: debian13(), ufw: fw{active: true, rules: allow("22"), boot: "fail"}}, "hardn can't tell if ufw starts at boot. Check its service, then try again.", firewall.ErrBootUnknown},
		"Proxmox VE not established":  {site{plat: debian13(), ufw: allow22, facts: func(f *detect.Facts) { f.Proxmox = detect.Proxmox{} }}, "hardn can't tell if this host runs Proxmox VE, where it leaves the firewall alone.", ufw.ErrProxmoxUnknown},
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
	refused(t, p, "ufw isn't running, so hardn saves no rule for it. Enable the firewall, then apply.", ufw.ErrInactive)
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
		"Proxmox VE":              {site{plat: pve9(), pve: pf{cluster: "[OPTIONS]\nenable: 1\n"}}, firewall.ItemNone, false},
		"Proxmox not established": {site{plat: debian13(), ufw: allow22, facts: func(f *detect.Facts) { f.Proxmox = detect.Proxmox{} }}, firewall.ItemEnable, false},
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
			"ufw allow 22/tcp", "ufw --force enable", "systemctl enable ufw.service",
			"systemctl disable ufw.service", "ufw disable", "ufw delete allow 22/tcp"}},
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
