package steps

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/alpine"
	"github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/dns"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/pve"
	"github.com/abbott/hardn/pkg/host/reload"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/host/revert/plans"
	"github.com/abbott/hardn/pkg/host/sshd"
	"github.com/abbott/hardn/pkg/host/sshd/migrate"
	"github.com/abbott/hardn/pkg/host/sudoers"
	"github.com/abbott/hardn/pkg/host/ufw"
)

// Synthetic values only: alice, web-01.example.net, 192.0.2.0/24, 2230.
// The planners' scenarios run through the engine on p2drive's fake VM
// (internal/tools/p2drive/engine_test.go); these are the parts that need no
// host, and each unit change's "wrote nothing" rule on hostfake.

func TestSetDirectives(t *testing.T) {
	port := func(ps ...string) []migrate.Directive {
		var out []migrate.Directive
		for _, p := range ps {
			out = append(out, migrate.Directive{Keyword: "Port", Args: []string{p}})
		}
		return out
	}
	const keys = "AuthorizedKeysFile .ssh/authorized_keys .ssh_%u/authorized_keys\n"
	for _, tc := range []struct {
		name string
		old  string
		set  []migrate.Directive
		want string
		err  bool
	}{
		{"a new file", "", port("22", "2230"), dropInHeader + setMarker + "\nPort 22\nPort 2230\n", false},
		{"hardn's file again", dropInHeader + setMarker + "\nPort 22\nPort 2230\n",
			[]migrate.Directive{{Keyword: "PasswordAuthentication", Args: []string{"no"}}},
			dropInHeader + setMarker + "\nPasswordAuthentication no\nPort 22\nPort 2230\n", false},
		{"the clean-up drops the held port", dropInHeader + setMarker + "\nPort 22\nPort 2230\n", port("2230"),
			dropInHeader + setMarker + "\nPort 2230\n", false},
		{"a migrated file: the labels stay with their lines",
			"# Managed by hardn.\n# From /etc/ssh/sshd_config.d/manage.conf:\nProtocol 2\nListenAddress 0.0.0.0:2230\nport=2230\n# Every key file the files hardn replaced named, so that no key stops working:\n" + keys,
			port("2230", "2231"),
			"# Managed by hardn.\n" + setMarker + "\nPort 2230\nPort 2231\n# From /etc/ssh/sshd_config.d/manage.conf:\nProtocol 2\nListenAddress 0.0.0.0:2230\n# Every key file the files hardn replaced named, so that no key stops working:\n" + keys, false},
		{"a header up to a blank line", "# site header\n\n  Port 2222\nPasswordAuthentication no",
			port("22", "2230"), "# site header\n\n" + setMarker + "\nPort 22\nPort 2230\nPasswordAuthentication no\n", false},
		{"no leading comment", "PermitRootLogin yes\nAllowUsers alice\n",
			[]migrate.Directive{{Keyword: "PermitRootLogin", Args: []string{"prohibit-password"}}},
			setMarker + "\nPermitRootLogin prohibit-password\nAllowUsers alice\n", false},
		{"a Match block", "Port 22\nMatch User alice\n  PasswordAuthentication yes\n", port("2230"), "", true},
		{"an Include", "Include /etc/ssh/extra/*.conf\n", port("2230"), "", true},
		{"an argument that needs quotes", "", []migrate.Directive{{Keyword: "Banner", Args: []string{"a b"}}}, "", true},
		{"no argument", "", []migrate.Directive{{Keyword: "Port"}}, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var old []byte
			if tc.old != "" {
				old = []byte(tc.old)
			}
			got, err := setDirectives(old, tc.set)
			switch {
			case tc.err && !errors.Is(err, fs.ErrInvalid):
				t.Fatalf("got %q, %v; want fs.ErrInvalid", got, err)
			case !tc.err && err != nil:
				t.Fatalf("error %v", err)
			case !tc.err && string(got) != tc.want:
				t.Errorf("got\n%s\nwant\n%s", got, tc.want)
			}
		})
	}
}

// TestSaysAlready: 00-hardn.conf with set's lines in another layout (a
// migrated file's label) needs no write; a value, a line or an order that
// differs does.
func TestSaysAlready(t *testing.T) {
	d := func(k string, v ...string) migrate.Directive { return migrate.Directive{Keyword: k, Args: v} }
	const migrated = "# Managed by hardn.\n# From /etc/ssh/sshd_config.d/manage.conf:\nPasswordAuthentication no\nPort 2230\n"
	for _, tc := range []struct {
		name string
		old  string
		set  []migrate.Directive
		want bool
	}{
		{"a migrated file, under its own label", migrated, []migrate.Directive{d("PasswordAuthentication", "no")}, true},
		{"its Port too", migrated, []migrate.Directive{d("Port", "2230"), d("PasswordAuthentication", "no")}, true},
		{"another value", migrated, []migrate.Directive{d("PasswordAuthentication", "yes")}, false},
		{"a keyword it lacks", migrated, []migrate.Directive{d("KbdInteractiveAuthentication", "no")}, false},
		{"a Port line more", "Port 22\nPort 2230\n", []migrate.Directive{d("Port", "2230")}, false},
		{"the Port lines in another order", "Port 2230\nPort 22\n", []migrate.Directive{d("Port", "22"), d("Port", "2230")}, false},
		{"an alias of the keyword", "ChallengeResponseAuthentication no\n", []migrate.Directive{d("KbdInteractiveAuthentication", "no")}, true},
		{"the = form", "PasswordAuthentication=no\n", []migrate.Directive{d("PasswordAuthentication", "no")}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := saysAlready([]byte(tc.old), tc.set); got != tc.want {
				t.Errorf("saysAlready = %v, want %v", got, tc.want)
			}
		})
	}
}

func listeners(ss ...string) []sshd.Listener {
	var out []sshd.Listener
	for _, s := range ss {
		ap := netip.MustParseAddrPort(s)
		out = append(out, sshd.Listener{Addr: ap.Addr(), Port: ap.Port()})
	}
	return out
}

func TestWanted(t *testing.T) {
	after := sshd.Effective{
		Port:                   sshd.Set{Values: []string{"22", "2230"}, Known: true},
		Listeners:              sshd.Listeners{SSHD: listeners("[::]:22", "0.0.0.0:22", "[::]:2230", "0.0.0.0:2230"), Known: true},
		PermitRootLogin:        sshd.Setting{Value: "prohibit-password", Known: true},
		PasswordAuthentication: sshd.Setting{Value: "yes", Known: true},
	}
	pinned := after
	pinned.Port.Values = []string{"2230", "2231"}
	pinned.Listeners.SSHD = listeners("0.0.0.0:2230")
	cleaned := after
	cleaned.Port.Values = []string{"2230"}
	cleaned.Listeners.SSHD = listeners("[::]:2230", "0.0.0.0:2230")
	d := func(k, v string) migrate.Directive { return migrate.Directive{Keyword: k, Args: []string{v}} }
	for _, tc := range []struct {
		name  string
		after sshd.Effective
		set   []migrate.Directive
		gone  []uint16
		ok    bool
	}{
		{"the ports", after, []migrate.Directive{d("Port", "22"), d("Port", "2230")}, nil, true},
		{"a port sshd does not show", after, []migrate.Directive{d("Port", "2231")}, nil, false},
		{"a port no listener has: a ListenAddress pins its own", pinned, []migrate.Directive{d("Port", "2230"), d("Port", "2231")}, nil, false},
		{"without-password is prohibit-password", after, []migrate.Directive{d("PermitRootLogin", "without-password")}, nil, true},
		{"another value", after, []migrate.Directive{d("PasswordAuthentication", "no")}, nil, false},
		{"Unknown is not a pass", after, []migrate.Directive{d("KbdInteractiveAuthentication", "no")}, nil, false},
		{"a keyword not checked", after, []migrate.Directive{d("MaxAuthTries", "3")}, nil, true},
		{"the clean-up: 22 gone", cleaned, []migrate.Directive{d("Port", "2230")}, []uint16{22}, true},
		{"the clean-up: 22 still listening", after, []migrate.Directive{d("Port", "2230")}, []uint16{22}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := wanted(tc.after, tc.set, tc.gone)
			if (err == nil) != tc.ok || err != nil && !errors.Is(err, ErrNotInEffect) {
				t.Errorf("wanted: %v, want ok %v", err, tc.ok)
			}
		})
	}
}

func TestCleanupOf(t *testing.T) {
	at := func(s string) revert.Listener {
		ap := netip.MustParseAddrPort(s)
		return revert.Listener{Addr: ap.Addr(), Port: ap.Port()}
	}
	run := func(argv ...string) revert.Step { return revert.Step{Op: revert.OpRun, Argv: argv, Why: firewallWhy} }
	j := revert.Journal{
		ConfirmVia: []revert.Listener{at("[::]:2230"), at("0.0.0.0:2230")},
		Undo:       []revert.Step{run("ufw", "disable"), run("ufw", "delete", "allow", "2230/tcp"), run("ufw", "delete", "allow", "22/tcp")},
		Reload:     []revert.Reload{{Service: revert.ReloadSSH, Keywords: []string{"Port"}, After: []revert.Listener{at("[::]:22"), at("0.0.0.0:22")}}},
	}
	c := CleanupOf(j)
	if fmt.Sprint(c.Drop) != "[22]" || fmt.Sprint(c.Firewall) != "[[ufw delete allow 2230/tcp] [ufw delete allow 22/tcp]]" {
		t.Errorf("CleanupOf = %+v", c)
	}
	j.ConfirmVia = nil
	if c := CleanupOf(j); c.Drop != nil || c.Firewall != nil {
		t.Errorf("a change that moved no port: %+v, want none", c)
	}
}

func TestConfirmTimeout(t *testing.T) {
	d := func(v time.Duration) *time.Duration { return &v }
	for _, tc := range []struct {
		name string
		flag *time.Duration
		key  string
		want time.Duration
		err  bool
	}{
		{"the default", nil, "", revert.DefaultTimeout, false},
		{"the key in seconds", nil, "240", 240 * time.Second, false},
		{"the key as a duration", nil, "5m", 5 * time.Minute, false},
		{"the flag wins", d(90 * time.Second), "5m", 90 * time.Second, false},
		{"a key below the floor", nil, "30s", 0, true},
		{"a key that is not one", nil, "soon", 0, true},
		{"a flag below the floor", d(10 * time.Second), "", 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ConfirmTimeout(tc.flag, &config.Config{ConfirmTimeout: tc.key})
			if tc.err != (err != nil) || err != nil && !errors.Is(err, revert.ErrTimeout) || got != tc.want {
				t.Errorf("ConfirmTimeout = %v, %v; want %v (error %v)", got, err, tc.want, tc.err)
			}
		})
	}
	if got, err := ConfirmTimeout(nil, nil); err != nil || got != revert.DefaultTimeout {
		t.Errorf("no configuration: %v, %v", got, err)
	}
}

// TestNotInEffect: the rollback refusal on the D9 host becomes the report
// P-39 (a) asks for; every other refusal is left as it is.
func TestNotInEffect(t *testing.T) {
	refused := fmt.Errorf("migrate: the rollback's reload would be refused: %w", reload.ErrRestoredListeners)
	listen := func(vs ...string) reload.SocketDropIns {
		return reload.SocketDropIns{Read: true, Files: []reload.SocketDropIn{{Path: migrate.ListenConf, Listen: vs}}}
	}
	at22 := listeners("[::]:22", "0.0.0.0:22")
	for _, tc := range []struct {
		name   string
		err    error
		drops  reload.SocketDropIns
		socket []sshd.Listener
		d9     bool
	}{
		{"listen.conf on 2230, the socket on 22 (D9)", refused, listen("", "2230"), at22, true},
		{"the socket on 22 and 2230 (configured for more)", refused, listen("", "2230"), listeners("[::]:22", "[::]:2230"), false},
		{"another refusal", fmt.Errorf("x: %w", reload.ErrSocketDropIn), listen("", "2230"), at22, false},
		{"no listen.conf", refused, reload.SocketDropIns{Read: true}, at22, false},
		{"a ListenStream that is a path", refused, listen("", "/run/sshd.sock"), at22, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := notInEffect(tc.err, tc.drops, tc.socket)
			var r *Refusal
			switch {
			case errors.As(err, &r) != tc.d9:
				t.Fatalf("notInEffect = %v, want a report %v", err, tc.d9)
			case !tc.d9 && err != tc.err:
				t.Errorf("notInEffect = %v, want %v as it was", err, tc.err)
			case tc.d9 && (r.Code != CodeListenConfNotInEffect || !errors.Is(err, ErrListenConfNotInEffect) || !errors.Is(err, reload.ErrRestoredListeners)):
				t.Errorf("report %+v", r)
			case tc.d9:
				// The firewall first, then the fix that keeps this session,
				// then the reboot, which needs the console.
				fw, restart, reboot := strings.Index(r.Fix, "firewall lets port 2230 through"), strings.Index(r.Fix, "`systemctl restart ssh.socket`"), strings.Index(r.Fix, "reboot only with console access")
				if fw < 0 || restart < fw || reboot < restart {
					t.Errorf("fix %q: want the firewall, the restart, then the reboot", r.Fix)
				}
			}
		})
	}
}

// TestNoRunDir: sshd -T failing for a missing privilege separation
// directory is a report whose fix fits how sshd is started: a connection
// starts ssh.service only where ssh.socket listens.
func TestNoRunDir(t *testing.T) {
	ee := &sshd.EnvError{Kind: sshd.EnvPrivsepDir, Path: sshd.PrivsepDir, Err: sshd.ErrEnvironment}
	for _, tc := range []struct {
		name string
		ssh  detect.SSH
		fix  string
	}{
		{"ssh.socket", detect.SSH{Mode: detect.SSHSocket}, "open an SSH connection to this host"},
		{"ssh.service", detect.SSH{Mode: detect.SSHService}, "`systemctl start ssh.service`"},
		{"OpenRC", detect.SSH{Mode: detect.SSHOpenRC}, "owned by root that no other user can write to"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := noRunDir(tc.ssh, fmt.Errorf("sshd's effective configuration: %w", ee))
			var r *Refusal
			if !errors.As(err, &r) || r.Code != CodeNoRunDir || !errors.Is(err, ErrNoRunDir) || !errors.Is(err, sshd.ErrEnvironment) ||
				!strings.Contains(err.Error(), CodeNoRunDir) || !strings.Contains(r.Fix, tc.fix) {
				t.Errorf("noRunDir = %v; want the report with %q", err, tc.fix)
			}
			if errors.Is(err, ErrListenConfNotInEffect) {
				t.Errorf("noRunDir = %v: another code's sentinel", err)
			}
		})
	}
	other := &sshd.EnvError{Kind: sshd.EnvHostKeys, Err: sshd.ErrEnvironment}
	if err := noRunDir(detect.SSH{Mode: detect.SSHService}, other); err != error(other) {
		t.Errorf("another environment error: %v, want it as it was", err)
	}
}

// runOn is what the engine gives a change's Apply: a backup run on sys.
func runOn(t *testing.T, sys host.System) engine.Run {
	t.Helper()
	id := revert.NewID(time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC))
	store, err := backup.Open(sys, backup.Settings{Path: backup.DefaultPath, Enabled: true}, id)
	if err != nil {
		t.Fatal(err)
	}
	return engine.Run{ID: id, Dir: revert.DefaultRunDir + "/" + id, Store: store}
}

// dropped reports whether ran removes the change's plan step.
func dropped(ran engine.Ran) bool { return ran.Rollback != nil && len(ran.Rollback.Steps) == 0 }

// TestPlanStepDropped: each unit's change keeps its plan step in the
// journal for an apply that wrote, and drops it for one refused before it
// wrote anything, by that unit's own signal; and a plan the unit's
// rollback would refuse is refused before it becomes a change.
func TestPlanStepDropped(t *testing.T) {
	ctx := context.Background()
	newHost := func() *hostfake.System {
		h := hostfake.New(host.Options{})
		h.AddDir("/var/backups", 0o755, 0, 0)
		return h
	}
	t.Run("pve keys: refused before its backup", func(t *testing.T) {
		const key = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIKGhoaGhoaGhoaGhoaGhoaGhoaGhoaGhoaGhoaGhoaGh alice@web-01.example.net"
		p, err := pve.PlanKeys(pve.Keys{Target: pve.ClusterKeys}, pve.KeyEdit{Add: []string{key}})
		if err != nil {
			t.Fatal(err)
		}
		c, err := PVEKeys("pve.keys", detect.Proxmox{Installed: detect.No}, p, "Add alice's key to root's keys")
		if err != nil || c.Risk != engine.RiskPlain || len(c.Rollback.Steps) != 1 || c.Rollback.Steps[0].Unit != plans.UnitPVEKeys {
			t.Fatalf("PVEKeys = %+v, %v", c, err)
		}
		h := newHost()
		ran, err := c.Apply(ctx, h, runOn(t, h))
		if !errors.Is(err, pve.ErrNotApplicable) || !dropped(ran) {
			t.Errorf("Apply off Proxmox VE: %+v, %v; want the step dropped", ran, err)
		}
	})
	t.Run("dns: refused before any step", func(t *testing.T) {
		from := dns.State{Host: dns.Host{Distro: detect.DistroDebian, Systemd: detect.No}, Manager: dns.ManagerFile,
			ResolvConf:  dns.Entry{Kind: dns.EntryFile, Data: []byte("nameserver 192.0.2.1\n"), Mode: 0o644},
			Resolved:    dns.Resolved{Active: detect.No},
			Nameservers: []string{"192.0.2.1"}}
		p, err := dns.PlanDNS(from, dns.Config{Nameservers: []string{"192.0.2.53"}})
		if err != nil {
			t.Fatal(err)
		}
		c, err := DNS("dns", p, "Nameservers 192.0.2.53")
		if err != nil || c.Risk != engine.RiskPlain || c.Rollback.Steps[0].Unit != plans.UnitDNS || !strings.Contains(c.Diff, "+nameserver 192.0.2.53") {
			t.Fatalf("DNS = %+v, %v", c, err)
		}
		h := newHost()
		h.AddFile(dns.ResolvConf, []byte("nameserver 192.0.2.9\n"), 0o644, 0, 0) // changed since the plan
		ran, err := c.Apply(ctx, h, runOn(t, h))
		if !errors.Is(err, dns.ErrChanged) || errors.Is(err, dns.ErrNotRestored) || !dropped(ran) {
			t.Errorf("Apply on a host changed since: %+v, %v; want refused and the step dropped", ran, err)
		}
	})
	for _, tc := range []struct {
		name string
		err  error
		drop bool
	}{
		{"applied", nil, false},
		{"refused, or undone by itself", errors.New("refused"), true},
		{"its own undo did not finish", fmt.Errorf("x: %w", alpine.ErrNotRestored), false},
	} {
		t.Run("alpine and dns: "+tc.name, func(t *testing.T) {
			ran, err := unitRan(tc.err, alpine.ErrNotRestored, "applied")
			if err != tc.err || dropped(ran) != tc.drop {
				t.Errorf("unitRan = %+v, %v; want dropped %v", ran, err, tc.drop)
			}
		})
	}
	t.Run("a plan its rollback would refuse", func(t *testing.T) {
		for name, err := range map[string]error{
			"sudoers": second(Sudoers("sudoers", sudoers.Plan{}, "x")),
			"dns":     second(DNS("dns", dns.Plan{}, "x")),
			"alpine":  second(Alpine("alpine", alpine.Plan{}, "x")),
			"pve":     second(PVEKeys("pve", detect.Proxmox{}, pve.KeysPlan{Target: pve.ClusterKeys, Added: []string{"x"}}, "x")),
		} {
			if !errors.Is(err, plans.ErrPlan) {
				t.Errorf("%s: %v, want plans.ErrPlan", name, err)
			}
		}
	})
}

func second(_ engine.Change, err error) error { return err }

func TestRefusal(t *testing.T) {
	r := &Refusal{Code: "EXAMPLE_CODE", Why: "why", Fix: "fix", Err: reload.ErrNotInEffect}
	if got := r.Error(); got != "why. To go on: fix (EXAMPLE_CODE): "+reload.ErrNotInEffect.Error() || !errors.Is(r, reload.ErrNotInEffect) {
		t.Errorf("Refusal %q", got)
	}
	if errors.Is(r, ErrNoRunDir) || errors.Is(r, ErrListenConfNotInEffect) {
		t.Errorf("a code without a sentinel matches one: %v", r)
	}
}

// TestFindingCode: a primitive's stable kind code as an engine.Finding's,
// its prefix said once.
func TestFindingCode(t *testing.T) {
	for _, tc := range []struct{ prefix, kind, want string }{
		{"PVE", pve.FindingRootKeyOnly.String(), "PVE_ROOT_KEY_ONLY"},
		{"SSHD", sshd.FindingWarning.String(), "SSHD_WARNING"},
		{"SSHD", sshd.FindingOverridden.String(), "SSHD_OVERRIDDEN"},
		{"UFW", ufw.FindingUnknown.String(), "UFW_UNKNOWN"},
		{"UFW", ufw.FindingAtStart.String(), "UFW_SSH_NOT_ALLOWED_AT_START"},
	} {
		if got := findingCode(tc.prefix, tc.kind); got != tc.want {
			t.Errorf("findingCode(%q, %q) = %q, want %q", tc.prefix, tc.kind, got, tc.want)
		}
	}
	d := droppedFindings([]migrate.Carried{{Origin: sshd.Origin{File: migrate.ManageConf, Line: 3, Text: "Protocol 2"}, Why: "sshd ignores it"}})
	if len(d) != 1 || d[0].Code != CodeMigrateDropped || d[0].Source != migrate.ManageConf+":3" || !strings.Contains(d[0].Detail, `"Protocol 2": sshd ignores it`) {
		t.Errorf("droppedFindings = %+v", d)
	}
}
