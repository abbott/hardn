package dns_test

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/host/dns"
	"github.com/abbott/hardn/pkg/host/hostfake"
)

// TestApplyResolved: on systemd-resolved hosts (Ubuntu's stub link,
// Debian 12 cloud's uplink link) the drop-in is written, its directory
// created 0755 whatever the umask, and resolved reloaded; resolvectl then
// lists exactly the asked-for global servers and search domains, although
// the main resolved.conf and an earlier drop-in set some too. That shows
// the tests' model of resolved (world.load), not resolved: on a host it is
// gate 5's. /etc/resolv.conf, a symlink, is never written. The next plan
// is empty; Rollback puts the host back, the directory it created included,
// and is idempotent.
func TestApplyResolved(t *testing.T) {
	const site = dns.DropInDir + "/50-site.conf"
	for _, tt := range []struct {
		fixture string
		site    bool // an earlier drop-in sets DNS= and Domains= too
		c       dns.Config
		writes  []string
	}{
		{"ubuntu-26.04", false, dns.Config{Nameservers: []string{ns1, ns6}},
			[]string{"mkdir " + dns.DropInDir, "chmod " + dns.DropInDir, "write-atomic " + dns.DropIn}},
		{"debian-12-cloud", true, dns.Config{Nameservers: []string{ns1, ns6}, Search: []string{"lan"}, ReplaceSearch: true},
			[]string{"write-atomic " + dns.DropIn}},
	} {
		t.Run(tt.fixture, func(t *testing.T) {
			w := newWorld(t, tt.fixture)
			if tt.site {
				w.h.AddFile(site, []byte("[Resolve]\nDNS=192.0.2.98\nDomains=site.example.net\n"), 0o644, 0, 0)
				w.load()
			}
			before, domains := slices.Clone(w.loaded), slices.Clone(w.domains)
			ctx := context.Background()
			p := w.plan(t, tt.c)
			res, err := dns.Apply(ctx, w.h, p, dns.Options{Backups: backups(t, w.h)})
			if err != nil || !slices.Equal(stepNames(res.Done), []string{"write-drop-in", "reload-resolved"}) {
				t.Fatalf("apply: %v, done %v", err, res.Done)
			}
			if !slices.Equal(w.loaded, tt.c.Nameservers) || content(t, w, dns.DropIn) != string(p.Changes[0].After) {
				t.Fatalf("loaded %q, drop-in %q", w.loaded, content(t, w, dns.DropIn))
			}
			if tt.c.ReplaceSearch && !slices.Equal(w.domains, tt.c.Search) || !tt.c.ReplaceSearch && !slices.Equal(w.domains, domains) {
				t.Errorf("domains %q", w.domains)
			}
			if e, _ := w.h.Entry(dns.DropInDir); e.Mode.Perm() != 0o755 {
				t.Errorf("directory %v", e.Mode)
			}
			if len(res.Backups) != 1 || res.Backups[0].Entry.Kind != backup.KindAbsent || res.Backups[0].Entry.Path != dns.DropIn {
				t.Errorf("backups %+v", res.Backups)
			}
			if got := writes(w.h); !slices.Equal(got, tt.writes) {
				t.Errorf("writes %q", got)
			}
			if p := w.plan(t, tt.c); len(p.Changes) != 0 {
				t.Errorf("second plan %q", steps(p))
			}
			for range 2 {
				if err := dns.Rollback(ctx, w.h, p); err != nil {
					t.Fatal(err)
				}
				if _, ok := w.h.Entry(dns.DropInDir); ok != tt.site || !slices.Equal(w.loaded, before) || !slices.Equal(w.domains, domains) {
					t.Fatalf("after rollback: dir there %v, loaded %q %q", ok, w.loaded, w.domains)
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

// env is what a TestApplyRefuses row may change before Apply runs.
type env struct {
	t   *testing.T
	w   *world
	p   *dns.Plan
	o   *dns.Options
	ctx context.Context
	sys host.System
}

// TestApplyRefuses: before anything changes, a done context, a plan
// that does not follow from its state, no backup run, a re-read that
// fails, a host changed since the plan, a backup that fails and a backup
// copy that is not what the plan read are refused; nothing is written.
func TestApplyRefuses(t *testing.T) {
	c := dns.Config{Nameservers: []string{ns1}}
	tests := []struct {
		name string
		edit func(e *env)
		want error
	}{
		{"done context", func(e *env) {
			c, cancel := context.WithCancel(e.ctx)
			cancel()
			e.ctx = c
		}, context.Canceled},
		{"edited bytes", func(e *env) { e.p.Changes[0].After = []byte("nameserver 192.0.2.66\n") }, fs.ErrInvalid},
		{"edited manager", func(e *env) { e.p.From.Manager = dns.ManagerProxmox }, fs.ErrInvalid},
		{"edited want", func(e *env) { e.p.Want.Nameservers = []string{"192.0.2.66"} }, fs.ErrInvalid},
		{"no backups", func(e *env) { e.o.Backups = nil }, dns.ErrNoBackup},
		{"re-read fails", func(e *env) {
			e.sys = faulty{System: e.w.h, fail: map[string]error{"stat /usr/sbin/dhclient": errDisk}}
		}, errDisk},
		{"edited since", func(e *env) { e.w.h.AddFile(dns.ResolvConf, []byte("nameserver 192.0.2.2\n"), 0o644, 0, 0) }, dns.ErrChanged},
		{"now a link", func(e *env) { e.w.h.AddSymlink(dns.ResolvConf, "../run/systemd/resolve/stub-resolv.conf") }, dns.ErrChanged},
		{"backup fails", func(e *env) {
			e.o.Backups = backups(e.t, faulty{System: e.w.h, fail: map[string]error{"read-nofollow " + dns.ResolvConf: errDisk}})
		}, errDisk},
		{"edited during the backup", func(e *env) {
			e.o.Backups = backups(e.t, onRead{e.w.h, func(name string) {
				if name == dns.ResolvConf {
					e.w.h.AddFile(dns.ResolvConf, []byte("nameserver 192.0.2.2\n"), 0o644, 0, 0)
				}
			}})
		}, dns.ErrChanged},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := newWorld(t, "debian-13")
			p := w.plan(t, c)
			e := &env{t: t, w: w, p: &p, o: &dns.Options{Backups: backups(t, w.h)}, ctx: context.Background(), sys: w.h}
			tt.edit(e)
			res, err := dns.Apply(e.ctx, e.sys, p, *e.o)
			if !errors.Is(err, tt.want) || len(res.Done) != 0 {
				t.Fatalf("err %v, want %v; done %v", err, tt.want, res.Done)
			}
			if got := writes(w.h); len(got) != 0 {
				t.Errorf("writes %q", got)
			}
		})
	}
}

// onRead runs before each ReadFileNoFollow.
type onRead struct {
	*hostfake.System
	before func(name string)
}

func (o onRead) ReadFileNoFollow(ctx context.Context, name string) ([]byte, fs.FileInfo, error) {
	o.before(name)
	return o.System.ReadFileNoFollow(ctx, name)
}

// TestApplyFails: a failed or ineffective step is undone with the steps
// before it, last first, and the host reads as before, systemd-resolved
// running again if the forward restart stopped it; a Ctrl-C during the
// reload does not stop the undo. An undo whose reload fails or does not
// take effect is not Restored and says so (ErrNotRestored); the files are
// back all the same.
func TestApplyFails(t *testing.T) {
	tests := []struct {
		name     string
		fixture  string
		search   bool // the plan sets the search domains
		seed     func(w *world) (host.System, context.Context)
		want     error
		done     []string
		restored bool
	}{
		{"reload fails", "ubuntu-26.04", false, func(w *world) (host.System, context.Context) {
			w.reload = "fail"
			return w.h, context.Background()
		}, errExit, []string{"write-drop-in", "reload-resolved"}, true},
		{"restart leaves resolved failed", "ubuntu-26.04", false, func(w *world) (host.System, context.Context) {
			w.reload = "down"
			return w.h, context.Background()
		}, errExit, []string{"write-drop-in", "reload-resolved"}, true},
		{"reload without effect", "ubuntu-26.04", false, func(w *world) (host.System, context.Context) {
			w.reload = "inert"
			return w.h, context.Background()
		}, dns.ErrNotEffective, []string{"write-drop-in", "reload-resolved"}, true},
		{"search domains rejected", "ubuntu-26.04", true, func(w *world) (host.System, context.Context) {
			w.rejectDomains = true
			w.hook = func(cmd string) {
				if strings.Contains(cmd, " reload-or-restart ") {
					w.rejectDomains = false // the old files parse
				}
			}
			return w.h, context.Background()
		}, dns.ErrNotEffective, []string{"write-drop-in", "reload-resolved"}, true},
		{"cancelled during the reload", "ubuntu-26.04", false, func(w *world) (host.System, context.Context) {
			ctx, cancel := context.WithCancel(context.Background())
			w.hook = func(cmd string) {
				if strings.HasSuffix(cmd, "try-reload-or-restart "+dns.ResolvedUnit) {
					cancel()
				}
			}
			return w.h, ctx
		}, context.Canceled, []string{"write-drop-in", "reload-resolved"}, true},
		{"mkdir fails", "ubuntu-26.04", false, func(w *world) (host.System, context.Context) {
			return faulty{System: w.h, fail: map[string]error{"mkdir " + dns.DropInDir: errDisk}}, context.Background()
		}, errDisk, []string{"write-drop-in"}, true},
		{"undo's reload fails", "ubuntu-26.04", false, func(w *world) (host.System, context.Context) {
			w.reload, w.restart = "fail", "fail"
			return w.h, context.Background()
		}, dns.ErrNotRestored, []string{"write-drop-in", "reload-resolved"}, false},
		{"undo's reload without effect", "ubuntu-26.04", true, func(w *world) (host.System, context.Context) {
			w.rejectDomains, w.restart = true, "inert"
			return w.h, context.Background()
		}, dns.ErrNotRestored, []string{"write-drop-in", "reload-resolved"}, false},
		{"write fails", "debian-13", false, func(w *world) (host.System, context.Context) {
			return faulty{System: w.h, fail: map[string]error{"write-atomic " + dns.ResolvConf: errDisk}}, context.Background()
		}, errDisk, []string{"write-resolv-conf"}, true},
		{"file reads back otherwise", "debian-13", false, func(w *world) (host.System, context.Context) {
			return &wrongMode{System: w.h}, context.Background()
		}, dns.ErrNotEffective, []string{"write-resolv-conf"}, true},
		{"second write fails", "alpine-3.24", false, func(w *world) (host.System, context.Context) {
			return faulty{System: w.h, fail: map[string]error{"write-atomic " + dns.ResolvConf: errDisk}}, context.Background()
		}, errDisk, []string{"udhcpc-no-resolv", "write-resolv-conf"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := newWorld(t, tt.fixture)
			cfg := dns.Config{Nameservers: []string{ns1}, UDHCPCNoResolv: tt.fixture == "alpine-3.24"}
			if tt.search {
				cfg.Search, cfg.ReplaceSearch = []string{"lan"}, true
			}
			p := w.plan(t, cfg)
			before := map[string]string{}
			for _, path := range []string{dns.ResolvConf, dns.DropIn, dns.UDHCPCConf} {
				before[path] = content(t, w, path)
			}
			loaded, domains, active := slices.Clone(w.loaded), slices.Clone(w.domains), w.active
			sys, ctx := tt.seed(w)
			res, err := dns.Apply(ctx, sys, p, dns.Options{Backups: backups(t, w.h), RestoreTimeout: time.Minute})
			var xe *host.ExitError
			if tt.want == errExit && errors.As(err, &xe) && xe.ExitCode == 1 {
				err = errExit
			}
			if !errors.Is(err, tt.want) || res.Restored != tt.restored || errors.Is(err, dns.ErrNotRestored) == tt.restored ||
				!slices.Equal(stepNames(res.Done), tt.done) {
				t.Fatalf("err %v, want %v; done %v, restored %v", err, tt.want, res.Done, res.Restored)
			}
			for path, was := range before {
				if got := content(t, w, path); got != was {
					t.Errorf("%s: %q, was %q", path, got, was)
				}
			}
			if _, ok := w.h.Entry(dns.DropInDir); ok {
				t.Error("drop-in directory left")
			}
			if tt.restored && (!slices.Equal(w.loaded, loaded) || !slices.Equal(w.domains, domains) || w.active != active) {
				t.Errorf("resolved %s, loaded %q %q, was %q %q", w.active, w.loaded, w.domains, loaded, domains)
			}
		})
	}
}

// wrongMode writes its first file 0600, whatever the mode asked for.
type wrongMode struct {
	*hostfake.System
	done bool
}

func (w *wrongMode) WriteFileAtomic(ctx context.Context, name string, data []byte, perm fs.FileMode, uid, gid int) error {
	if !w.done {
		w.done, perm = true, 0o600
	}
	return w.System.WriteFileAtomic(ctx, name, data, perm, uid, gid)
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
// loaded, the plan only reloads. When that fails, there is no file to put
// back and nothing to reload back to, so it is not run again; the host is
// as it was, so the undo is Restored. When the restart leaves resolved
// stopped, the undo starts it, which loads the drop-in: not Restored. A
// reload-only plan that succeeded cannot be rolled back.
func TestApplyReloadOnly(t *testing.T) {
	c := dns.Config{Nameservers: []string{ns1}}
	setup := func(t *testing.T) (*world, dns.Plan, string) {
		w := newWorld(t, "debian-12-cloud")
		dropIn := string(w.plan(t, c).Changes[0].After)
		w.h.AddFile(dns.DropIn, []byte(dropIn), 0o644, 0, 0)
		p := w.plan(t, c)
		if !slices.Equal(steps(p), []string{"reload-resolved"}) || !strings.Contains(p.Findings[0].Detail, "first three nameservers") {
			t.Fatalf("steps %q, findings %+v", steps(p), p.Findings)
		}
		return w, p, dropIn
	}
	reloads := func(w *world) (n int) {
		for _, r := range w.h.Runs() {
			if strings.Join(r, " ") == "systemctl try-reload-or-restart "+dns.ResolvedUnit {
				n++
			}
		}
		return n
	}
	ctx := context.Background()

	w, p, dropIn := setup(t)
	w.reload = "fail"
	res, err := dns.Apply(ctx, w.h, p, dns.Options{})
	var xe *host.ExitError
	if !errors.As(err, &xe) || !res.Restored || !slices.Equal(stepNames(res.Done), []string{"reload-resolved"}) || reloads(w) != 1 {
		t.Fatalf("err %v, done %v, restored %v, reloads %d", err, res.Done, res.Restored, reloads(w))
	}
	// The drop-in was there before the plan: its rollback leaves it.
	if err := dns.Rollback(ctx, w.h, p); err != nil || content(t, w, dns.DropIn) != dropIn {
		t.Errorf("rollback: %v, drop-in %q", err, content(t, w, dns.DropIn))
	}

	w, p, _ = setup(t)
	w.reload = "down"
	res, err = dns.Apply(ctx, w.h, p, dns.Options{})
	if !errors.Is(err, dns.ErrNotRestored) || res.Restored || w.active != "active" {
		t.Errorf("restart stopped resolved: err %v, restored %v, resolved %s", err, res.Restored, w.active)
	}

	w, p, dropIn = setup(t)
	if _, err := dns.Apply(ctx, w.h, p, dns.Options{}); err != nil {
		t.Fatal(err)
	}
	if err := dns.Rollback(ctx, w.h, p); !errors.Is(err, dns.ErrNotRestored) || content(t, w, dns.DropIn) != dropIn || !slices.Equal(w.loaded, c.Nameservers) {
		t.Errorf("rollback: %v, drop-in %q, loaded %q", err, content(t, w, dns.DropIn), w.loaded)
	}
}

// TestPlanJSON: a plan survives encoding/json, the Err fields aside (a
// finding keeps their text), so a journal can keep it: Apply and Rollback
// accept the decoded plan.
func TestPlanJSON(t *testing.T) {
	for _, tt := range []struct {
		fixture, udhcpc string
	}{
		{"alpine-3.24", "[ -f /x ] && RESOLV_CONF=no\n"},
		{"ubuntu-26.04", ""},
	} {
		t.Run(tt.fixture, func(t *testing.T) {
			w := newWorld(t, tt.fixture)
			if tt.udhcpc != "" {
				w.h.AddFile(dns.UDHCPCConf, []byte(tt.udhcpc), 0o644, 0, 0)
			}
			p := w.plan(t, dns.Config{Nameservers: []string{ns1}})
			if tt.udhcpc != "" && (p.From.UDHCPC.Err == nil || !strings.Contains(p.Findings[0].Detail, "not a plain assignment")) {
				t.Fatalf("udhcpc %v, findings %+v", p.From.UDHCPC.Err, p.Findings)
			}
			b, err := json.Marshal(p)
			if err != nil {
				t.Fatal(err)
			}
			var got dns.Plan
			if err := json.Unmarshal(b, &got); err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(steps(got), steps(p)) || got.Findings[0] != p.Findings[0] || got.From.UDHCPC.Err != nil {
				t.Fatalf("decoded %+v", got)
			}
			if _, err := dns.Apply(context.Background(), w.h, got, dns.Options{Backups: backups(t, w.h)}); err != nil {
				t.Fatal(err)
			}
			if err := dns.Rollback(context.Background(), w.h, got); err != nil {
				t.Fatal(err)
			}
		})
	}
}
