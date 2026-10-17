package dns_test

import (
	"context"
	"errors"
	"io/fs"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/host/dns"
)

// TestApplyResolved: on systemd-resolved hosts (Ubuntu's stub link,
// Debian 12 cloud's uplink link) the drop-in is written and resolved
// reloaded; resolvectl then lists exactly the asked-for global servers,
// although the main resolved.conf sets one too. /etc/resolv.conf, a
// symlink, is never written. The next plan is empty; Rollback puts the
// host back, the directory it created included, and is idempotent.
func TestApplyResolved(t *testing.T) {
	for _, fixture := range []string{"ubuntu-26.04", "debian-12-cloud"} {
		t.Run(fixture, func(t *testing.T) {
			w := newWorld(t, fixture)
			ctx := context.Background()
			c := dns.Config{Nameservers: []string{ns1, ns6}}
			p := w.plan(t, c)
			res, err := dns.Apply(ctx, w.h, p, dns.Options{Backups: backups(t, w.h)})
			if err != nil || !slices.Equal(stepNames(res.Done), []string{"write-drop-in", "reload-resolved"}) {
				t.Fatalf("apply: %v, done %v", err, res.Done)
			}
			if !slices.Equal(w.loaded, []string{ns1, ns6}) || content(t, w, dns.DropIn) != string(p.Changes[0].After) {
				t.Fatalf("loaded %q, drop-in %q", w.loaded, content(t, w, dns.DropIn))
			}
			if len(res.Backups) != 1 || res.Backups[0].Entry.Kind != backup.KindAbsent || res.Backups[0].Entry.Path != dns.DropIn {
				t.Errorf("backups %+v", res.Backups)
			}
			if got := writes(w.h); !slices.Equal(got, []string{"mkdir " + dns.DropInDir, "write-atomic " + dns.DropIn}) {
				t.Errorf("writes %q", got)
			}
			if p := w.plan(t, c); len(p.Changes) != 0 {
				t.Errorf("second plan %q", steps(p))
			}
			for range 2 {
				if err := dns.Rollback(ctx, w.h, p); err != nil {
					t.Fatal(err)
				}
				if _, ok := w.h.Entry(dns.DropInDir); ok || !slices.Equal(w.loaded, []string{"192.0.2.99"}) {
					t.Fatalf("after rollback: dir there %v, loaded %q", ok, w.loaded)
				}
			}
		})
	}
}

// TestApplyFile: Debian's, Alpine's and Proxmox VE's files are rewritten
// in place of the old, with their mode and owner, after a backup of their
// bytes; Alpine's udhcpc.conf only when asked. Rollback writes the old
// bytes back.
func TestApplyFile(t *testing.T) {
	for _, tt := range []struct {
		fixture string
		c       dns.Config
		paths   []string
	}{
		{"debian-13", dns.Config{Nameservers: []string{ns1}}, []string{dns.ResolvConf}},
		{"alpine-3.24", dns.Config{Nameservers: []string{ns1, ns2}, UDHCPCNoResolv: true}, []string{dns.UDHCPCConf, dns.ResolvConf}},
		{"proxmox", dns.Config{Nameservers: []string{ns1}, Search: []string{"example.net"}, ReplaceSearch: true}, []string{dns.ResolvConf}},
	} {
		t.Run(tt.fixture, func(t *testing.T) {
			w := newWorld(t, tt.fixture)
			ctx := context.Background()
			w.h.AddFile(dns.ResolvConf, []byte(content(t, w, dns.ResolvConf)), 0o640, 0, 4)
			before := map[string]string{}
			for _, p := range tt.paths {
				before[p] = content(t, w, p)
			}
			p := w.plan(t, tt.c)
			res, err := dns.Apply(ctx, w.h, p, dns.Options{Backups: backups(t, w.h)})
			if err != nil || len(res.Done) != len(tt.paths) || len(res.Backups) != len(tt.paths) {
				t.Fatalf("apply: %v, done %v", err, res.Done)
			}
			for i, c := range p.Changes {
				if c.Path != tt.paths[i] || content(t, w, c.Path) != string(c.After) || res.Backups[i].Entry.Kind != backup.KindFile {
					t.Errorf("%s: %q", c.Path, content(t, w, c.Path))
				}
			}
			if e, _ := w.h.Entry(dns.ResolvConf); e.Mode.Perm() != 0o640 || e.GID != 4 {
				t.Errorf("mode %v gid %d", e.Mode, e.GID)
			}
			if tt.c.UDHCPCNoResolv && !strings.Contains(content(t, w, dns.UDHCPCConf), "\nRESOLV_CONF=\"no\"\n") {
				t.Errorf("udhcpc.conf %q", content(t, w, dns.UDHCPCConf))
			}
			if p := w.plan(t, tt.c); len(p.Changes) != 0 {
				t.Errorf("second plan %q", steps(p))
			}
			for range 2 {
				if err := dns.Rollback(ctx, w.h, p); err != nil {
					t.Fatal(err)
				}
				for _, path := range tt.paths {
					if content(t, w, path) != before[path] {
						t.Fatalf("%s after rollback: %q", path, content(t, w, path))
					}
				}
			}
		})
	}
}

// TestApplyRefuses: before anything changes, a done context, a plan
// that does not follow from its state, no backup run, and a host changed
// since the plan are refused; nothing is written.
func TestApplyRefuses(t *testing.T) {
	c := dns.Config{Nameservers: []string{ns1}}
	tests := []struct {
		name string
		edit func(w *world, p *dns.Plan, o *dns.Options, ctx *context.Context)
		want error
	}{
		{"done context", func(_ *world, _ *dns.Plan, _ *dns.Options, ctx *context.Context) {
			c, cancel := context.WithCancel(*ctx)
			cancel()
			*ctx = c
		}, context.Canceled},
		{"edited bytes", func(_ *world, p *dns.Plan, _ *dns.Options, _ *context.Context) {
			p.Changes[0].After = []byte("nameserver 192.0.2.66\n")
		}, fs.ErrInvalid},
		{"edited manager", func(_ *world, p *dns.Plan, _ *dns.Options, _ *context.Context) {
			p.From.Manager = dns.ManagerProxmox
		}, fs.ErrInvalid},
		{"edited want", func(_ *world, p *dns.Plan, _ *dns.Options, _ *context.Context) {
			p.Want.Nameservers = []string{"192.0.2.66"}
		}, fs.ErrInvalid},
		{"no backups", func(_ *world, _ *dns.Plan, o *dns.Options, _ *context.Context) { o.Backups = nil }, dns.ErrNoBackup},
		{"edited since", func(w *world, _ *dns.Plan, _ *dns.Options, _ *context.Context) {
			w.h.AddFile(dns.ResolvConf, []byte("nameserver 192.0.2.2\n"), 0o644, 0, 0)
		}, dns.ErrChanged},
		{"now a link", func(w *world, _ *dns.Plan, _ *dns.Options, _ *context.Context) {
			w.h.AddSymlink(dns.ResolvConf, "../run/systemd/resolve/stub-resolv.conf")
		}, dns.ErrChanged},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := newWorld(t, "debian-13")
			p := w.plan(t, c)
			ctx := context.Background()
			o := dns.Options{Backups: backups(t, w.h)}
			tt.edit(w, &p, &o, &ctx)
			res, err := dns.Apply(ctx, w.h, p, o)
			if !errors.Is(err, tt.want) || len(res.Done) != 0 {
				t.Fatalf("err %v, want %v; done %v", err, tt.want, res.Done)
			}
			if got := writes(w.h); len(got) != 0 {
				t.Errorf("writes %q", got)
			}
		})
	}
}

// TestApplyFails: a failed or ineffective step is undone with the steps
// before it, last first, and the host reads as before; a Ctrl-C during the
// reload does not stop the undo.
func TestApplyFails(t *testing.T) {
	c := dns.Config{Nameservers: []string{ns1}}
	tests := []struct {
		name    string
		fixture string
		seed    func(w *world) (host.System, context.Context)
		want    error
		done    []string
	}{
		{"reload fails", "ubuntu-26.04", func(w *world) (host.System, context.Context) {
			w.reload = "fail"
			w.hook = func(cmd string) {
				if _, there := w.h.Entry(dns.DropIn); strings.HasSuffix(cmd, "try-reload-or-restart "+dns.ResolvedUnit) && !there {
					w.reload = "" // the undo's reload works
				}
			}
			return w.h, context.Background()
		}, errExit, []string{"write-drop-in", "reload-resolved"}},
		{"reload without effect", "ubuntu-26.04", func(w *world) (host.System, context.Context) {
			w.reload = "inert"
			return w.h, context.Background()
		}, dns.ErrNotEffective, []string{"write-drop-in", "reload-resolved"}},
		{"cancelled during the reload", "ubuntu-26.04", func(w *world) (host.System, context.Context) {
			ctx, cancel := context.WithCancel(context.Background())
			w.hook = func(cmd string) {
				if strings.HasSuffix(cmd, "try-reload-or-restart "+dns.ResolvedUnit) {
					cancel()
				}
			}
			return w.h, ctx
		}, context.Canceled, []string{"write-drop-in", "reload-resolved"}},
		{"write fails", "debian-13", func(w *world) (host.System, context.Context) {
			return faulty{System: w.h, fail: map[string]error{"write-atomic " + dns.ResolvConf: errDisk}}, context.Background()
		}, errDisk, []string{"write-resolv-conf"}},
		{"second write fails", "alpine-3.24", func(w *world) (host.System, context.Context) {
			return faulty{System: w.h, fail: map[string]error{"write-atomic " + dns.ResolvConf: errDisk}}, context.Background()
		}, errDisk, []string{"udhcpc-no-resolv", "write-resolv-conf"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := newWorld(t, tt.fixture)
			cfg := c
			cfg.UDHCPCNoResolv = tt.fixture == "alpine-3.24"
			p := w.plan(t, cfg)
			before := map[string]string{}
			for _, path := range []string{dns.ResolvConf, dns.DropIn, dns.UDHCPCConf} {
				before[path] = content(t, w, path)
			}
			loaded := slices.Clone(w.loaded)
			sys, ctx := tt.seed(w)
			res, err := dns.Apply(ctx, sys, p, dns.Options{Backups: backups(t, w.h), RestoreTimeout: time.Minute})
			var xe *host.ExitError
			if tt.want == errExit && errors.As(err, &xe) && xe.ExitCode == 1 {
				err = errExit
			}
			if !errors.Is(err, tt.want) || !res.Restored || !slices.Equal(stepNames(res.Done), tt.done) {
				t.Fatalf("err %v, want %v; done %v, restored %v", err, tt.want, res.Done, res.Restored)
			}
			for path, was := range before {
				if got := content(t, w, path); got != was {
					t.Errorf("%s: %q, was %q", path, got, was)
				}
			}
			if _, ok := w.h.Entry(dns.DropInDir); ok || !slices.Equal(w.loaded, loaded) {
				t.Errorf("drop-in dir left %v, loaded %q, was %q", ok, w.loaded, loaded)
			}
		})
	}
}

// TestApplyChangedDuringRun: a file edited after the re-read is not
// written over: its step refuses before acting, so it is not undone,
// and the steps before it are.
func TestApplyChangedDuringRun(t *testing.T) {
	w := newWorld(t, "alpine-3.24")
	p := w.plan(t, dns.Config{Nameservers: []string{ns1}, UDHCPCNoResolv: true})
	edited, stock := "nameserver 192.0.2.2\n", content(t, w, dns.UDHCPCConf)
	sys := hookOnWrite{faulty{System: w.h}, func(name string) {
		if name == dns.UDHCPCConf {
			w.h.AddFile(dns.ResolvConf, []byte(edited), 0o644, 0, 0)
		}
	}}
	res, err := dns.Apply(context.Background(), sys, p, dns.Options{Backups: backups(t, w.h)})
	if !errors.Is(err, dns.ErrChanged) || !res.Restored || !slices.Equal(stepNames(res.Done), []string{"udhcpc-no-resolv"}) {
		t.Fatalf("err %v, done %v, restored %v", err, res.Done, res.Restored)
	}
	if content(t, w, dns.ResolvConf) != edited || content(t, w, dns.UDHCPCConf) != stock {
		t.Errorf("resolv.conf %q, udhcpc.conf %q", content(t, w, dns.ResolvConf), content(t, w, dns.UDHCPCConf))
	}
}

// TestRollbackLeavesOthers: a file someone changed after the apply is left
// as they left it, and the rollback says so.
func TestRollbackLeavesOthers(t *testing.T) {
	w := newWorld(t, "debian-13")
	p := w.plan(t, dns.Config{Nameservers: []string{ns1}})
	if _, err := dns.Apply(context.Background(), w.h, p, dns.Options{Backups: backups(t, w.h)}); err != nil {
		t.Fatal(err)
	}
	w.h.AddFile(dns.ResolvConf, []byte("nameserver 192.0.2.3\n"), 0o644, 0, 0) // a DHCP lease
	err := dns.Rollback(context.Background(), w.h, p)
	if !errors.Is(err, dns.ErrNotRestored) || !errors.Is(err, dns.ErrChanged) || content(t, w, dns.ResolvConf) != "nameserver 192.0.2.3\n" {
		t.Fatalf("err %v, file %q", err, content(t, w, dns.ResolvConf))
	}
	p.Changes[0].After = []byte("x\n")
	if err := dns.Rollback(context.Background(), w.h, p); !errors.Is(err, dns.ErrNotRestored) || !errors.Is(err, fs.ErrInvalid) {
		t.Errorf("edited plan: %v", err)
	}
}

// hookOnWrite runs after each successful atomic write.
type hookOnWrite struct {
	faulty
	after func(name string)
}

func (h hookOnWrite) WriteFileAtomic(ctx context.Context, name string, data []byte, perm fs.FileMode, uid, gid int) error {
	err := h.faulty.WriteFileAtomic(ctx, name, data, perm, uid, gid)
	if err == nil {
		h.after(name)
	}
	return err
}

// errExit stands for systemctl's exit 1 in TestApplyFails.
var errExit = errors.New("exit status 1")

func stepNames(ss []dns.Step) []string {
	var out []string
	for _, s := range ss {
		out = append(out, s.String())
	}
	return out
}

// TestApplyReloadOnly: with hardn's drop-in already on disk but not
// loaded, the plan only reloads; when that fails there is no file to put
// back and nothing to reload back to, so it is not run again.
func TestApplyReloadOnly(t *testing.T) {
	w := newWorld(t, "debian-12-cloud")
	c := dns.Config{Nameservers: []string{ns1}}
	dropIn := string(w.plan(t, c).Changes[0].After)
	w.h.AddFile(dns.DropIn, []byte(dropIn), 0o644, 0, 0)
	p := w.plan(t, c)
	if !slices.Equal(steps(p), []string{"reload-resolved"}) || !strings.Contains(p.Findings[0].Detail, "first three nameservers") {
		t.Fatalf("steps %q, findings %+v", steps(p), p.Findings)
	}
	w.reload = "fail"
	res, err := dns.Apply(context.Background(), w.h, p, dns.Options{})
	var xe *host.ExitError
	if !errors.As(err, &xe) || !res.Restored || !slices.Equal(stepNames(res.Done), []string{"reload-resolved"}) {
		t.Fatalf("err %v, done %v, restored %v", err, res.Done, res.Restored)
	}
	n := 0
	for _, r := range w.h.Runs() {
		if strings.Join(r, " ") == strings.Join([]string{"systemctl", "try-reload-or-restart", dns.ResolvedUnit}, " ") {
			n++
		}
	}
	if n != 1 {
		t.Errorf("reloaded %d times", n)
	}
	// The drop-in was there before the plan: its rollback leaves it.
	if err := dns.Rollback(context.Background(), w.h, p); err != nil || content(t, w, dns.DropIn) != dropIn {
		t.Errorf("rollback: %v, drop-in %q", err, content(t, w, dns.DropIn))
	}
}
