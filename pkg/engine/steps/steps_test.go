package steps

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/netip"
	"slices"
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
		if !errors.Is(err, dns.ErrChanged) || errors.Is(err, dns.ErrNotRestored) || !dropped(ran) || ran.Undone {
			t.Errorf("Apply on a host changed since: %+v, %v; want refused, the step dropped, nothing undone", ran, err)
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
		t.Run("alpine: "+tc.name, func(t *testing.T) {
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

// TestDNSChange: steps.DNS carries the plan's findings, coded and worded
// (D1), diffs a created file against /dev/null (D2), and, when the reload
// has no effect, words the failure and says what it did is undone (D3):
// dns.Apply having undone its steps, the step is dropped; its own undo
// not finished, the step stays for the journal's rollback
// (next/s2-control; gallery-dns.md §8).
func TestDNSChange(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name     string
		undoFail bool // the undo's restart of systemd-resolved fails
	}{{"undone by itself", false}, {"its own undo not finished", true}} {
		t.Run(tc.name, func(t *testing.T) {
			restarts := 0
			h, run := unitHost(t, func(cmd string) (hostfake.Response, bool) {
				switch cmd {
				case "systemctl is-active " + dns.ResolvedUnit:
					return hostfake.Response{Stdout: "active\n"}, true
				case "resolvectl --no-pager dns":
					return hostfake.Response{Stdout: "Global:\nLink 2 (eth0): 192.0.2.1\n"}, true
				case "resolvectl --no-pager domain":
					return hostfake.Response{Stdout: "Global:\nLink 2 (eth0): example.net\n"}, true
				case "systemctl reload-or-restart " + dns.ResolvedUnit:
					if restarts++; tc.undoFail {
						return hostfake.Response{Stderr: "Job failed\n", ExitCode: 1}, true
					}
					return hostfake.Response{}, true
				case "systemctl try-reload-or-restart " + dns.ResolvedUnit:
					return hostfake.Response{}, true // loads nothing new
				}
				return hostfake.Response{}, false
			})
			h.AddSymlink(dns.ResolvConf, "../run/systemd/resolve/stub-resolv.conf")
			f := detect.Facts{Platform: detect.Platform{Distro: detect.DistroUbuntu, Version: "26.04", Support: detect.Supported},
				Init: detect.Init{Kind: detect.Systemd, Running: detect.Yes}, Container: detect.Container{Detected: detect.No}}
			s, err := dns.Read(ctx, h, f)
			if err != nil {
				t.Fatal(err)
			}
			p, err := dns.PlanDNS(s, dns.Config{Nameservers: []string{"192.0.2.54"}})
			if err != nil {
				t.Fatal(err)
			}
			c, err := DNS("dns.nameservers/set", p, "Set the nameservers to 192.0.2.54")
			if err != nil {
				t.Fatal(err)
			}
			want := []engine.Finding{{Code: "DNS_PER_LINK", Detail: "eth0 still uses its own nameserver 192.0.2.1", Source: "resolvectl dns"}}
			if !slices.Equal(c.Findings, want) || !strings.HasPrefix(c.Diff, "--- /dev/null\n+++ "+dns.DropIn+"\n") {
				t.Errorf("findings %+v, diff\n%s", c.Findings, c.Diff)
			}
			ran, err := c.Apply(ctx, h, run)
			var fl *Failure
			if !errors.Is(err, dns.ErrNotEffective) || !errors.As(err, &fl) || fl.Text != "Reloading systemd-resolved had no effect" || !ran.Undone ||
				errors.Is(err, dns.ErrNotRestored) != tc.undoFail || dropped(ran) == tc.undoFail || restarts != 1 {
				t.Errorf("Apply = %+v, %v (restarts %d); want the reload's failure in words, undone, the step dropped %v", ran, err, restarts, !tc.undoFail)
			}
			if _, ok := h.Entry(dns.DropIn); ok {
				t.Error("the drop-in is left")
			}
		})
	}
	if got := (&Failure{Text: "Saving /etc/hardn/hardn.yml failed"}).Error(); got != "Saving /etc/hardn/hardn.yml failed" {
		t.Errorf("a Failure with no error: %q", got)
	}
}

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

// TestAlpineRisk: Alpine's change takes the highest class of its steps:
// a service restart once crond is started, a package install for
// apk-cron's install alone.
func TestAlpineRisk(t *testing.T) {
	for _, tc := range []struct {
		name string
		u    alpine.Updates
		want engine.RiskClass
	}{
		{"apk-cron's install alone", alpine.Updates{Package: detect.No, Script: alpine.ScriptAbsent, Runlevel: detect.Yes, Crond: alpine.CrondStarted, Legacy: alpine.LegacyAbsent},
			engine.RiskPackageInstall},
		{"install, runlevel and start", alpine.Updates{Package: detect.No, Script: alpine.ScriptAbsent, Runlevel: detect.No, Crond: alpine.CrondStopped, Legacy: alpine.LegacyAbsent},
			engine.RiskServiceRestart},
		{"crond started alone", alpine.Updates{Package: detect.Yes, Script: alpine.ScriptInstalled, Runlevel: detect.Yes, Crond: alpine.CrondStopped, Legacy: alpine.LegacyAbsent},
			engine.RiskServiceRestart},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, err := alpine.PlanUpdates(tc.u)
			if err != nil {
				t.Fatal(err)
			}
			c, err := Alpine("alpine", p, "Alpine's updates")
			if err != nil || c.Risk != tc.want {
				t.Errorf("Alpine = risk %v, %v; want %v", c.Risk, err, tc.want)
			}
		})
	}
}

// unitHost is a fake host for the unit changes: the backup and run
// directories, and handle answering its commands.
func unitHost(t *testing.T, handle func(cmd string) (hostfake.Response, bool)) (*hostfake.System, engine.Run) {
	t.Helper()
	h := hostfake.New(host.Options{})
	h.AddDir("/var/backups", 0o755, 0, 0)
	run := runOn(t, h)
	h.AddDir(revert.DefaultRunDir, 0o755, 0, 0)
	h.AddDir(run.Dir, 0o700, 0, 0)
	h.Handler = func(argv []string, _ []byte) hostfake.Response {
		if r, ok := handle(strings.Join(argv, " ")); ok {
			return r
		}
		t.Errorf("unexpected command %q", argv)
		return hostfake.Response{Err: hostfake.ErrUnscripted, ExitCode: -1}
	}
	return h, run
}

// TestPlanStepKept: an apply that wrote keeps its unit's plan step in the
// journal (no Ran.Rollback), for sudoers, DNS, Alpine's updates and
// Proxmox VE's keys; and sudoers drops it when its backup fails before
// sudoers.Apply wrote anything.
func TestPlanStepKept(t *testing.T) {
	ctx := context.Background()
	const grantFile = sudoers.DropInDir + "/hardn-grant-alice"
	sudoersChange := func(t *testing.T, h *hostfake.System) engine.Change {
		h.AddFile(sudoers.MainConfig, []byte("Defaults env_reset\nroot ALL=(ALL:ALL) ALL\n@includedir /etc/sudoers.d\n"), 0o440, 0, 0)
		h.AddDir(sudoers.DropInDir, 0o755, 0, 0)
		st, err := sudoers.Read(ctx, h, sudoers.Choice{Tool: sudoers.ToolSudo, Flavor: detect.SudoOriginal}, []string{"alice"})
		if err != nil {
			t.Fatal(err)
		}
		p, err := sudoers.PlanFor(st, []sudoers.Policy{{User: "alice", Grant: sudoers.GrantNoPassword}})
		if err != nil || len(p.Changes) != 1 {
			t.Fatalf("PlanFor = %+v, %v", p, err)
		}
		c, err := Sudoers("sudoers/alice", p, "alice's grant")
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	visudo := func(cmd string) (hostfake.Response, bool) {
		return hostfake.Response{Stdout: "parsed OK\n"}, strings.HasPrefix(cmd, "visudo -c")
	}
	t.Run("sudoers: applied", func(t *testing.T) {
		h, run := unitHost(t, visudo)
		ran, err := sudoersChange(t, h).Apply(ctx, h, run)
		if _, ok := h.Entry(grantFile); err != nil || ran.Rollback != nil || !ok {
			t.Errorf("Apply = %+v, %v; want the grant written and the step kept", ran, err)
		}
	})
	t.Run("sudoers: its backup fails before it writes", func(t *testing.T) {
		h, run := unitHost(t, visudo)
		c := sudoersChange(t, h)
		h.AddDir("/var/backups", 0o777, 1000, 1000) // another user can change it
		ran, err := c.Apply(ctx, h, run)
		if _, ok := h.Entry(grantFile); !errors.Is(err, backup.ErrUnsafeDir) || !dropped(ran) || ok {
			t.Errorf("Apply = %+v, %v; want the backup refused, nothing written and the step dropped", ran, err)
		}
	})
	t.Run("dns: applied", func(t *testing.T) {
		h, run := unitHost(t, func(cmd string) (hostfake.Response, bool) {
			return hostfake.Response{Stdout: "inactive\n", ExitCode: 3}, cmd == "systemctl is-active "+dns.ResolvedUnit
		})
		h.AddFile(dns.ResolvConf, []byte("search example.net\nnameserver 192.0.2.1\n"), 0o644, 0, 0)
		st, err := dns.Read(ctx, h, detect.Facts{Platform: detect.Platform{Distro: detect.DistroDebian, Version: "13", Support: detect.Supported},
			Init: detect.Init{Kind: detect.Systemd, Running: detect.Yes}, Container: detect.Container{Detected: detect.No}})
		if err != nil {
			t.Fatal(err)
		}
		p, err := dns.PlanDNS(st, dns.Config{Nameservers: []string{"192.0.2.53"}})
		if err != nil {
			t.Fatal(err)
		}
		c, err := DNS("dns", p, "the nameservers")
		if err != nil {
			t.Fatal(err)
		}
		ran, err := c.Apply(ctx, h, run)
		if e, _ := h.Entry(dns.ResolvConf); err != nil || ran.Rollback != nil || !strings.Contains(string(e.Data), "192.0.2.53") {
			t.Errorf("Apply = %+v, %v; %q; want written and the step kept", ran, err, e.Data)
		}
	})
	t.Run("alpine: applied", func(t *testing.T) {
		crond := "stopped"
		h, run := unitHost(t, func(cmd string) (hostfake.Response, bool) {
			switch cmd {
			case "apk info -e " + alpine.Package:
				return hostfake.Response{Stdout: alpine.Package + "\n"}, true
			case "rc-service crond status":
				if crond == "started" {
					return hostfake.Response{Stdout: " * status: started\n"}, true
				}
				return hostfake.Response{Stdout: " * status: stopped\n", ExitCode: 3}, true
			case "rc-service crond start":
				crond = "started"
				return hostfake.Response{}, true
			}
			return hostfake.Response{}, false
		})
		h.AddFile(alpine.Script, []byte("#!/bin/sh\nexec apk -U upgrade\n"), 0o755, 0, 0)
		h.AddFile(alpine.InitScript, []byte("#!/sbin/openrc-run\n"), 0o755, 0, 0)
		h.AddFile(alpine.RunlevelLink, nil, 0o755, 0, 0)
		u, err := alpine.ReadUpdates(ctx, h, detect.Facts{Platform: detect.Platform{Distro: detect.DistroAlpine, Version: "3.24", Support: detect.Supported},
			Init: detect.Init{Kind: detect.OpenRC, Running: detect.Yes}, Container: detect.Container{Detected: detect.No}})
		if err != nil {
			t.Fatal(err)
		}
		p, err := alpine.PlanUpdates(u)
		if err != nil {
			t.Fatal(err)
		}
		c, err := Alpine("alpine", p, "crond started")
		if err != nil {
			t.Fatal(err)
		}
		if ran, err := c.Apply(ctx, h, run); err != nil || ran.Rollback != nil || crond != "started" {
			t.Errorf("Apply = %+v, %v; crond %s; want started and the step kept", ran, err, crond)
		}
	})
	t.Run("pve keys: applied", func(t *testing.T) {
		const (
			keyWeb01 = "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAAAQQCRkZGRkZGRkZGRkZGRkZGRkZGRkZGRkZGRkZGRkZGRkZGRkZGRkZGRkZGRkZGRkZGRkZGRkZGRkZGRkZGRkZGR root@web-01"
			keyAlice = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIKGhoaGhoaGhoaGhoaGhoaGhoaGhoaGhoaGhoaGhoaGh alice@web-01.example.net"
			wwwData  = 33 // the group pmxcfs reports under /etc/pve
		)
		h, run := unitHost(t, func(cmd string) (hostfake.Response, bool) {
			return hostfake.Response{Stdout: "install ok installed\t9.2.22\n"}, strings.HasPrefix(cmd, "dpkg-query --show ")
		})
		h.AddFile("/usr/bin/pveversion", []byte("#!/usr/bin/perl\n"), 0o755, 0, 0)
		h.AddFile("/proc/self/mounts", []byte("/dev/fuse /etc/pve fuse rw,nosuid,nodev,relatime,user_id=0,group_id=0,default_permissions,allow_other 0 0\n"), 0o444, 0, 0)
		h.AddDir("/root", 0o700, 0, 0)
		h.AddDir("/root/.ssh", 0o700, 0, 0)
		h.AddDir("/etc/pve", 0o755, 0, wwwData)
		h.AddDir("/etc/pve/priv", 0o700, 0, wwwData)
		h.AddDir(pve.NodesDir+"/web-01", 0o755, 0, wwwData)
		h.AddFile(pve.ClusterKeys, []byte(keyWeb01+"\n"), 0o600, 0, wwwData)
		h.AddSymlink(pve.RootKeys, pve.ClusterKeys)
		h.AddFile(pve.NodeKey, []byte(keyWeb01+"\n"), 0o644, 0, 0)
		up := detect.Proxmox{Installed: detect.Yes, Version: "9.2.22", ClusterFS: detect.Yes, ClusterFSType: "fuse", EtcPVE: detect.Yes}
		k, err := pve.ReadKeys(ctx, h, up)
		if err != nil {
			t.Fatal(err)
		}
		p, err := pve.PlanKeys(k, pve.KeyEdit{Add: []string{keyAlice}})
		if err != nil {
			t.Fatal(err)
		}
		c, err := PVEKeys("pve.keys", up, p, "alice's key for root")
		if err != nil {
			t.Fatal(err)
		}
		ran, err := c.Apply(ctx, h, run)
		if e, _ := h.Entry(pve.ClusterKeys); err != nil || ran.Rollback != nil || !strings.Contains(string(e.Data), keyAlice) {
			t.Errorf("Apply = %+v, %v; keys %q; want alice's key and the step kept", ran, err, e.Data)
		}
	})
}

// TestSameAsRead: the write of 00-hardn.conf runs only while the engine's
// backup, taken just before it, shows the file as the plan read it.
func TestSameAsRead(t *testing.T) {
	const sum = "2c26b46b68ffc68ff99b453c1d30413413422d706483bfa0f98a5e886266e7ae"
	file := func(s string) backup.Entry {
		return backup.Entry{Kind: backup.KindFile, File: &backup.Content{SHA256: s}}
	}
	for _, tc := range []struct {
		name   string
		e      backup.Entry
		ok     bool
		absent bool
		want   error // nil: the write may go on
		none   bool  // an error that is not ErrChanged
	}{
		{"as read", file(sum), true, false, nil, false},
		{"absent, as read", backup.Entry{Kind: backup.KindAbsent}, true, true, nil, false},
		{"another content", file("00" + sum[2:]), true, false, ErrChanged, false},
		{"written since it was read absent", file(sum), true, true, ErrChanged, false},
		{"removed since it was read", backup.Entry{Kind: backup.KindAbsent}, true, false, ErrChanged, false},
		{"no backup taken", backup.Entry{}, false, false, nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := sameAsRead(tc.e, tc.ok, sshd.HardnDropIn, tc.absent, sum)
			switch {
			case tc.none && (err == nil || errors.Is(err, ErrChanged)):
				t.Errorf("sameAsRead = %v, want another refusal", err)
			case !tc.none && !errors.Is(err, tc.want):
				t.Errorf("sameAsRead = %v, want %v", err, tc.want)
			}
		})
	}
}

// TestPlanSave: the save of hardn.yml is planned from the file at the
// loaded path, its diff the file against what config.Save writes; refused
// for a configuration loaded from none, a file changed since it was read
// or gone, and a symlink, each with a report; its Apply refuses a file
// whose backup is not what the plan read and a configuration changed since
// the plan, and otherwise saves through config.Save; the lost comments are
// its finding (next/s2-control).
func TestPlanSave(t *testing.T) {
	ctx := context.Background()
	const path = "/etc/hardn/hardn.yml"
	h, run := unitHost(t, func(string) (hostfake.Response, bool) { return hostfake.Response{}, false })
	h.AddFile(path, []byte("# hardn\nnameservers:\n  - 192.0.2.54\n"), 0o600, 0, 0)
	cfg, err := config.Read(ctx, h, path)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Nameservers = []string{"192.0.2.53"}
	for name, tc := range map[string]struct {
		cfg  *config.Config
		code string
		err  error
	}{
		"no configuration":          {nil, "CONFIG_NOT_LOADED", config.ErrNotLoaded},
		"not loaded from a file":    {&config.Config{Nameservers: []string{"192.0.2.53"}}, "CONFIG_NOT_LOADED", config.ErrNotLoaded},
		"changed since it was read": {cfg, CodeConfigChanged, ErrConfigChanged},
	} {
		t.Run(name, func(t *testing.T) {
			if tc.code == CodeConfigChanged {
				h.AddFile(path, []byte("nameservers: []\n"), 0o600, 0, 0)
				defer h.AddFile(path, []byte("# hardn\nnameservers:\n  - 192.0.2.54\n"), 0o600, 0, 0)
			}
			_, err := PlanSave(ctx, h, "dns.nameservers/save", tc.cfg)
			var r *Refusal
			if !errors.As(err, &r) || r.Code != tc.code || r.Why == "" || r.Fix == "" || !errors.Is(err, tc.err) {
				t.Errorf("PlanSave = %v; want a %s refusal", err, tc.code)
			}
		})
	}
	h.AddSymlink("/etc/hardn/link.yml", path)
	linked, err := config.Read(ctx, h, "/etc/hardn/link.yml") // as LoadConfig reads it, through the link
	if err != nil {
		t.Fatal(err)
	}
	var r *Refusal
	if _, err := PlanSave(ctx, h, "dns.nameservers/save", linked); !errors.Is(err, host.ErrSymlink) || !errors.As(err, &r) ||
		r.Why != "/etc/hardn/link.yml is a link" || r.Fix != "edit the file it links to by hand" {
		t.Errorf("PlanSave through a link = %v; want it refused with a report, as config.Save would refuse it", err)
	}
	h.AddFile("/etc/hardn/gone.yml", []byte("nameservers: []\n"), 0o600, 0, 0)
	gone, err := config.Read(ctx, h, "/etc/hardn/gone.yml")
	if err != nil || h.Remove(ctx, "/etc/hardn/gone.yml") != nil {
		t.Fatal(err)
	}
	if _, err := PlanSave(ctx, h, "dns.nameservers/save", gone); !errors.Is(err, ErrConfigChanged) || !errors.Is(err, fs.ErrNotExist) ||
		!errors.As(err, &r) || r.Why != "/etc/hardn/gone.yml changed since it was read" {
		t.Errorf("PlanSave of a file gone = %v; want CONFIG_CHANGED with a report", err)
	}
	c, err := PlanSave(ctx, h, "dns.nameservers/save", cfg, "dns.nameservers/set")
	if err != nil {
		t.Fatal(err)
	}
	if c.Target != path || !slices.Equal(c.Backups, []string{path}) || !slices.Equal(c.After, []string{"dns.nameservers/set"}) ||
		c.Stage != engine.StageFiles || c.Risk != engine.RiskPlain || len(c.Findings) != 1 || c.Findings[0].Code != CodeCommentsLost {
		t.Fatalf("PlanSave = %+v", c)
	}
	marshalled, err := config.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	was, will := "# hardn\nnameservers:\n  - 192.0.2.54\n", string(marshalled)
	if want := engine.FileDiff(path, &was, &will); c.Diff != want || !strings.Contains(c.Diff, "\n-# hardn\n") || !strings.Contains(c.Diff, "\n+    - 192.0.2.53\n") {
		t.Errorf("diff\n%s\nwant\n%s", c.Diff, want)
	}
	entry := func() backup.Entry {
		e, err := run.Store.Backup(ctx, path, backup.RollbackRequired)
		if err != nil {
			t.Fatal(err)
		}
		return e.Entry
	}
	h.AddFile(path, []byte("nameservers: []\n"), 0o600, 0, 0) // changed between the plan and the apply
	run.Backups = map[string]backup.Entry{path: entry()}
	if _, err := c.Apply(ctx, h, run); !errors.Is(err, ErrConfigChanged) {
		t.Errorf("Apply of a file changed since the plan: %v", err)
	}
	if e, _ := h.Entry(path); string(e.Data) != "nameservers: []\n" {
		t.Errorf("written though refused: %q", e.Data)
	}
	h.AddFile(path, []byte("# hardn\nnameservers:\n  - 192.0.2.54\n"), 0o600, 0, 0)
	run.Backups = map[string]backup.Entry{path: entry()}
	cfg.Nameservers = []string{"192.0.2.52"} // changed between the plan and the apply: not the diff shown
	if _, err := c.Apply(ctx, h, run); !errors.Is(err, engine.ErrPlanChanged) {
		t.Errorf("Apply of a configuration changed since the plan: %v", err)
	}
	cfg.Nameservers = []string{"192.0.2.53"}
	if ran, err := c.Apply(ctx, h, run); err != nil || ran.Detail != "saved "+path {
		t.Fatalf("Apply = %+v, %v", ran, err)
	}
	if e, _ := h.Entry(path); !strings.Contains(string(e.Data), "- 192.0.2.53\n") || strings.Contains(string(e.Data), "#") {
		t.Errorf("saved:\n%s", e.Data)
	}
}
