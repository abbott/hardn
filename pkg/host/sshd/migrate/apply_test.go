package migrate

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"maps"
	"path"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/reload"
	"github.com/abbott/hardn/pkg/host/sshd"
)

// applyHost plans sc on a fresh host and returns what Apply needs.
func applyHost(t *testing.T, sc scenario) (*testHost, Plan, Options) {
	t.Helper()
	h, p, err := plan(t, sc)
	if err != nil {
		t.Fatal(err)
	}
	run, err := backup.Open(h, backup.Settings{Path: backupRoot, Enabled: true}, backup.NewRunID(time.Date(2026, 10, 17, 14, 30, 5, 0, time.UTC)))
	if err != nil {
		t.Fatal(err)
	}
	return h, p, Options{RunDir: runDir, Backup: run}
}

// snapshot is every file a migration may touch, by path ("" when absent).
func snapshot(h *testHost) map[string]string {
	s := map[string]string{}
	for _, p := range []string{sshd.MainConfig, sshd.HardnDropIn, ManageConf, HardnConf, customConf, ListenConf, sshd.DropInDir + "/50-cloud-init.conf"} {
		if e, ok := h.Entry(p); ok {
			s[p] = string(e.Data)
		} else {
			s[p] = ""
		}
	}
	return s
}

// sshdFile reports whether p is one of the paths whose changes Apply validates first.
func sshdFile(p string) bool {
	return p == sshd.MainConfig || path.Dir(p) == sshd.DropInDir
}

// checkValidatedFirst fails when a write or removal of an sshd file is not
// preceded, since the previous one, by an sshd -t run: every state on disk
// was validated before it existed. It returns the mutations outside the run
// directory and the backup store, in order.
func checkValidatedFirst(t *testing.T, h *testHost, from int) []hostfake.Call {
	t.Helper()
	validated, last := false, ""
	var muts []hostfake.Call
	for _, c := range h.Calls()[from:] {
		switch {
		case c.Op == "run" && len(c.Argv) > 1 && c.Argv[0] == "sshd" && c.Argv[1] == "-t" && c.ExitCode == 0:
			validated = true
		case slices.Contains([]string{"write-atomic", "write-in-place", "remove", "removeall", "rename", "mkdir", "chmod", "chown"}, c.Op):
			if strings.HasPrefix(c.Path, runDir+"/") || strings.HasPrefix(c.Path, backupRoot) || c.Err != nil {
				continue
			}
			// OpRestoreMain validates its two writes together: in between,
			// sshd_config has no Include, so 00-hardn.conf is not read yet.
			pair := last == sshd.HardnDropIn && c.Path == sshd.MainConfig
			if sshdFile(c.Path) && !validated && !pair {
				t.Errorf("%s with no sshd -t since the previous change", c)
			}
			validated, last = false, c.Path
			muts = append(muts, c)
		}
	}
	return muts
}

// planReload plans the reload after p with reload.PlanFor, as the engine
// does: at plan time from Check's configuration and the socket drop-ins as
// the plan leaves them, or after Apply from the drop-ins read again.
func planReload(t *testing.T, h *testHost, sc scenario, p Plan, after sshd.Effective, dropIns func(reload.SocketDropIns) reload.SocketDropIns) {
	t.Helper()
	d, err := reload.ReadSocketDropIns(context.Background(), h)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reload.PlanFor(reload.Input{SSH: sc.ssh, Change: p.Reload(after), DropIns: dropIns(d)}); err != nil {
		t.Errorf("reload.PlanFor(%+v) = %v", p.Reload(after), err)
	}
}

// TestApplyScenarios migrates every host shape: Check writes nothing and
// the reload that follows plans before anything changes; each state on
// disk is validated before it exists, every file is backed up first, the
// files end as the plan says, the effective login paths are kept, the
// reload plans after Apply too, and restoring the backups gives the host
// back as it was.
func TestApplyScenarios(t *testing.T) {
	for _, sc := range scenarios(t) {
		t.Run(sc.name, func(t *testing.T) {
			h, p, opts := applyHost(t, sc)
			was := snapshot(h)
			from := len(h.Calls())
			checked, _, err := Check(context.Background(), h, p, opts)
			if err != nil {
				t.Fatal(err)
			}
			if muts := checkValidatedFirst(t, h, from); len(muts) != 0 {
				t.Errorf("Check changed the host: %v", muts)
			}
			planReload(t, h, sc, p, checked, p.SocketDropIns)
			from = len(h.Calls())
			rep, err := Apply(context.Background(), h, p, opts)
			if err != nil {
				t.Fatal(err)
			}
			muts := checkValidatedFirst(t, h, from)
			changedYet := false
			for _, c := range h.Calls()[from:] {
				switch {
				case c.Op == "write-atomic" && strings.HasPrefix(c.Path, backupRoot) && changedYet:
					t.Errorf("%s after the first change: every backup comes first", c)
				case len(muts) > 0 && c.Op == muts[0].Op && c.Path == muts[0].Path:
					changedYet = true
				}
			}
			if len(rep.Done) != len(p.Ops) {
				t.Errorf("Done %d of %d steps", len(rep.Done), len(p.Ops))
			}
			var paths []string
			for _, op := range p.Ops {
				paths = append(paths, op.paths()...)
			}
			var backedUp []string
			for _, r := range rep.Backups {
				backedUp = append(backedUp, r.Entry.Path)
				if r.Outcome != backup.Taken || !r.Entry.RollbackRequired {
					t.Errorf("backup of %s: %v, rollback required %v", r.Entry.Path, r.Outcome, r.Entry.RollbackRequired)
				}
			}
			if !slices.Equal(backedUp, paths) {
				t.Errorf("backed up %q, want %q", backedUp, paths)
			}
			var changed []string
			for _, m := range muts {
				changed = append(changed, m.Path)
			}
			if !slices.Equal(changed, paths) {
				t.Errorf("changed %q, want %q in that order", changed, paths)
			}

			now := snapshot(h)
			for _, op := range p.Ops {
				switch op.Kind {
				case OpWriteDropIn:
					checkOwned(t, h, sshd.HardnDropIn, p.DropIn)
				case OpRestoreMain:
					checkOwned(t, h, sshd.HardnDropIn, p.DropIn)
					if now[sshd.MainConfig] != string(op.Data) {
						t.Errorf("sshd_config is not the restored structure")
					}
				default:
					if _, ok := h.Entry(op.Path); ok {
						t.Errorf("%s is still there", op.Path)
					}
				}
			}
			if ns := narrowings(p.Before, h.before(t, sc.ssh, sc.socket), p.keepSSHD(), p.Hold); len(ns) > 0 {
				t.Errorf("the installed files narrow the login paths: %v", ns)
			}
			if !p.Listening.Known {
				t.Errorf("Listening %+v is not Known", p.Listening)
			}
			planReload(t, h, sc, p, rep.After, func(d reload.SocketDropIns) reload.SocketDropIns { return d })
			if ov := overlaps(rep.After.Listeners.SSHD); len(ov) > 0 {
				t.Errorf("the migrated configuration listens twice on %q", ov)
			}

			m, err := backup.Load(context.Background(), h, opts.Backup.Dir())
			if err != nil {
				t.Fatal(err)
			}
			if _, err := backup.Restore(context.Background(), h, m); err != nil {
				t.Fatal(err)
			}
			if got := snapshot(h); !mapsEqual(got, was) {
				t.Errorf("after restoring the backups:\n got %q\nwant %q", got, was)
			}
		})
	}
}

func mapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// checkOwned: 00-hardn.conf has the plan's content, is root's and 0644.
func checkOwned(t *testing.T, h *testHost, name string, want []byte) {
	t.Helper()
	e, ok := h.Entry(name)
	if !ok || !bytes.Equal(e.Data, want) || e.Mode.Perm() != 0o644 || e.UID != 0 || e.GID != 0 {
		t.Errorf("%s: ok %v, mode %v, owner %d:%d, content matches %v", name, ok, e.Mode, e.UID, e.GID, bytes.Equal(e.Data, want))
	}
}

// TestApplyRefusesNarrowing: settings that would end password logins (the
// cloud-init line wins today) are refused before anything is backed up or
// written.
func TestApplyRefusesNarrowing(t *testing.T) {
	sc := named(t, "v032-ubuntu")
	sc.desired = []Directive{{"PasswordAuthentication", []string{"no"}}, {"AuthenticationMethods", []string{"publickey,password"}}}
	h, p, opts := applyHost(t, sc)
	was := snapshot(h)
	from := len(h.Calls())
	rep, err := Apply(context.Background(), h, p, opts)
	var ne *NarrowError
	if !errors.Is(err, ErrFewerLoginPaths) || !errors.As(err, &ne) {
		t.Fatalf("Apply = %v, want a *NarrowError", err)
	}
	var ks []string
	for _, n := range ne.Narrowings {
		ks = append(ks, n.Keyword)
	}
	if want := []string{"passwordauthentication", "authenticationmethods"}; !slices.Equal(ks, want) {
		t.Errorf("narrowings %v, want %v", ne.Narrowings, want)
	}
	if len(rep.Backups) != 0 || len(rep.Done) != 0 || len(checkValidatedFirst(t, h, from)) != 0 || !mapsEqual(snapshot(h), was) {
		t.Errorf("a refused plan backed up %d, did %d steps, or changed the host", len(rep.Backups), len(rep.Done))
	}
	if _, ok := h.Entry(opts.Backup.Dir()); ok {
		t.Error("a refused plan created the backup run directory")
	}
}

// TestApplyStopsMidway: when a step's own state fails validation, the
// steps before it stay done, the rest do not run, and the backups restore
// the host.
func TestApplyStopsMidway(t *testing.T) {
	sc := named(t, "upgraded-ubuntu") // write, remove, remove, remove listen.conf
	h, p, opts := applyHost(t, sc)
	was := snapshot(h)
	h.failT = 3 // the whole plan, the write, then the first removal's state
	rep, err := Apply(context.Background(), h, p, opts)
	if !errors.Is(err, sshd.ErrConfig) {
		t.Fatalf("Apply = %v, want sshd.ErrConfig", err)
	}
	if len(rep.Done) != 1 || rep.Done[0].Kind != OpWriteDropIn {
		t.Fatalf("Done = %v, want the 00-hardn.conf write only", rep.Done)
	}
	if _, ok := h.Entry(ManageConf); !ok {
		t.Error("manage.conf removed after the failed validation")
	}
	m, err := backup.Load(context.Background(), h, opts.Backup.Dir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backup.Restore(context.Background(), h, m); err != nil {
		t.Fatal(err)
	}
	if got := snapshot(h); !mapsEqual(got, was) {
		t.Errorf("after restoring:\n got %q\nwant %q", got, was)
	}
}

func TestApplyRefuses(t *testing.T) {
	sc := named(t, "v032-ubuntu")
	tests := []struct {
		name string
		edit func(*testHost, *Plan, *Options)
		err  error
	}{
		{"before not established", func(_ *testHost, p *Plan, _ *Options) { p.Before = sshd.Effective{} }, ErrNotEstablished},
		{"before with an unread listener", func(_ *testHost, p *Plan, _ *Options) { p.Before.Listeners.SSHD = nil }, ErrNotEstablished},
		{"no backup run", func(_ *testHost, _ *Plan, o *Options) { o.Backup = nil }, fs.ErrInvalid},
		{"the migrated configuration is invalid", func(h *testHost, _ *Plan, _ *Options) { h.failT = 1 }, sshd.ErrConfig},
		{"the migrated configuration loses a listener", func(_ *testHost, p *Plan, _ *Options) {
			p.Hold = append(p.Hold, sshd.Listener{Addr: p.Hold[0].Addr, Port: 2231})
		}, ErrFewerLoginPaths},
		{"Debian without /run/sshd", func(h *testHost, p *Plan, _ *Options) {
			p.Platform = debian13
			_ = h.Remove(context.Background(), sshd.PrivsepDir)
		}, sshd.ErrEnvironment},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, p, opts := applyHost(t, sc)
			tt.edit(h, &p, &opts)
			was := snapshot(h)
			from := len(h.Calls())
			if _, err := Apply(context.Background(), h, p, opts); !errors.Is(err, tt.err) {
				t.Fatalf("Apply = %v, want %v", err, tt.err)
			}
			if len(checkValidatedFirst(t, h, from)) != 0 || !mapsEqual(snapshot(h), was) {
				t.Error("a refused plan changed the host")
			}
		})
	}
}

// TestApplyRefusesChanged: a file changed between the plan and Apply is
// not written over or removed.
func TestApplyRefusesChanged(t *testing.T) {
	for _, path := range []string{ManageConf, ListenConf, sshd.HardnDropIn} {
		t.Run(path, func(t *testing.T) {
			h, p, opts := applyHost(t, named(t, "v032-ubuntu"))
			h.AddFile(path, []byte("# edited by bob\n"), 0o644, 0, 0)
			from := len(h.Calls())
			_, err := Apply(context.Background(), h, p, opts)
			if !errors.Is(err, ErrChanged) || !strings.Contains(err.Error(), path) {
				t.Fatalf("Apply = %v, want ErrChanged naming %s", err, path)
			}
			if muts := checkValidatedFirst(t, h, from); len(muts) != 0 {
				t.Errorf("a refused plan changed %v", muts)
			}
			if e, _ := h.Entry(path); string(e.Data) != "# edited by bob\n" {
				t.Errorf("%s was changed", path)
			}
		})
	}
}

// TestApplyRefusesOverlap: a ListenAddress the migration adds beside one
// of a file it leaves, which no host can bind together, is refused.
func TestApplyRefusesOverlap(t *testing.T) {
	sc := named(t, "v032-ubuntu")
	sc.files = maps.Clone(sc.files)
	sc.files[ManageConf] = strings.Replace(sc.files[ManageConf], "ListenAddress 0.0.0.0:2230", "ListenAddress [::]:2230", 1)
	sc.files[sshd.DropInDir+"/20-site.conf"] = "ListenAddress 192.0.2.10:2230\n"
	h, p, opts := applyHost(t, sc)
	was := snapshot(h)
	if _, err := Apply(context.Background(), h, p, opts); !errors.Is(err, ErrListenerOverlap) || !strings.Contains(err.Error(), "0.0.0.0:2230 and 192.0.2.10:2230") {
		t.Fatalf("Apply = %v, want ErrListenerOverlap naming 0.0.0.0:2230 and 192.0.2.10:2230", err)
	}
	if !mapsEqual(snapshot(h), was) {
		t.Error("a refused plan changed the host")
	}
}

func TestApplyEmptyAndDone(t *testing.T) {
	h := newHost(t, map[string]string{sshd.MainConfig: stockMain})
	if rep, err := Apply(context.Background(), h, Plan{}, Options{}); err != nil || len(h.Calls()) != 0 || len(rep.Done) != 0 {
		t.Errorf("Apply(empty plan) = %+v, %v with %d calls", rep, err, len(h.Calls()))
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, p, opts := applyHost(t, named(t, "v032-ubuntu"))
	if _, err := Apply(ctx, h, p, opts); !errors.Is(err, context.Canceled) {
		t.Errorf("Apply with a done context = %v", err)
	}
}

// TestApplyAlpineOrder: on Alpine, 00-hardn.conf is written before the
// sshd_config that includes it, so no state on disk reads a missing file,
// and the note names the drop-ins the Include starts reading.
func TestApplyAlpineOrder(t *testing.T) {
	sc := named(t, "main-alpine") // with 20-site.conf
	h, p, opts := applyHost(t, sc)
	if !strings.Contains(p.Ops[0].Note, sshd.DropInDir+"/20-site.conf") {
		t.Errorf("note %q does not say sshd now reads 20-site.conf", p.Ops[0].Note)
	}
	from := len(h.Calls())
	if _, err := Apply(context.Background(), h, p, opts); err != nil {
		t.Fatal(err)
	}
	var writes []string
	for _, c := range checkValidatedFirst(t, h, from) {
		writes = append(writes, c.Path)
	}
	if want := []string{sshd.HardnDropIn, sshd.MainConfig}; !slices.Equal(writes, want) {
		t.Errorf("writes %q, want %q", writes, want)
	}
	if p.Platform.Distro != detect.DistroAlpine {
		t.Fatal("scenario is not Alpine")
	}
}

// TestApplyFailures: a backup, write or removal that fails stops Apply
// there; the steps in Done ran, and restoring the run's backups gives the
// host back as it was, including after the Alpine step's first write.
func TestApplyFailures(t *testing.T) {
	errDisk := errors.New("injected: no space left on device")
	for _, tt := range []struct {
		name, scen string
		fail       func(op, name string) error
		done       int  // steps carried out
		backups    bool // the run has backups to restore
	}{
		{name: "backup", scen: "v032-ubuntu", fail: func(op, name string) error {
			if op == "write-atomic" && strings.HasPrefix(name, backupRoot+"/") {
				return errDisk
			}
			return nil
		}},
		{name: "write", scen: "v032-ubuntu", fail: failOn("write-atomic", sshd.HardnDropIn, errDisk), backups: true},
		{name: "removal", scen: "upgraded-ubuntu", fail: failOn("remove", HardnConf, errDisk), done: 2, backups: true},
		{name: "listen.conf removal", scen: "v032-ubuntu", fail: failOn("remove", ListenConf, errDisk), done: 2, backups: true},
		{name: "between the Alpine writes", scen: "v032-alpine", fail: failOn("write-atomic", sshd.MainConfig, errDisk), backups: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			h, p, opts := applyHost(t, named(t, tt.scen))
			was := snapshot(h)
			h.fail = tt.fail
			rep, err := Apply(context.Background(), h, p, opts)
			if !errors.Is(err, errDisk) {
				t.Fatalf("Apply = %v, want the injected error", err)
			}
			if len(rep.Done) != tt.done {
				t.Errorf("Done %d steps, want %d", len(rep.Done), tt.done)
			}
			h.fail = nil
			if !tt.backups {
				if !mapsEqual(snapshot(h), was) {
					t.Error("a failed backup changed the host")
				}
				return
			}
			m, err := backup.Load(context.Background(), h, opts.Backup.Dir())
			if err != nil {
				t.Fatal(err)
			}
			if _, err := backup.Restore(context.Background(), h, m); err != nil {
				t.Fatal(err)
			}
			if got := snapshot(h); !mapsEqual(got, was) {
				t.Errorf("after restoring:\n got %q\nwant %q", got, was)
			}
		})
	}
}

// TestApplyRefusesSiteNarrowing: on Alpine the restored Include starts
// reading the other drop-ins; one that narrows a login path (MaxAuthTries 4
// below sshd's 6) is refused before anything is written, with every
// narrowing named.
func TestApplyRefusesSiteNarrowing(t *testing.T) {
	sc := named(t, "main-alpine")
	sc.files = maps.Clone(sc.files)
	sc.files[sshd.DropInDir+"/20-site.conf"] = "MaxAuthTries 4\n"
	h, p, opts := applyHost(t, sc)
	was := snapshot(h)
	_, err := Apply(context.Background(), h, p, opts)
	var ne *NarrowError
	if !errors.As(err, &ne) || !errors.Is(err, ErrFewerLoginPaths) {
		t.Fatalf("Apply = %v, want a *NarrowError", err)
	}
	if want := `migrate: the migrated configuration: fewer working login paths than the installed configuration: maxauthtries: "6" before, "4" after`; err.Error() != want {
		t.Errorf("error %q, want %q", err, want)
	}
	if !mapsEqual(snapshot(h), was) {
		t.Error("a refused plan changed the host")
	}
}

// TestApplyAlpineFlavour: a drop-in the restored Include starts reading
// that sets UsePAM is in Plan.Keywords, so the reload refuses
// (reload.ErrFlavour) rather than signal a binary that may not be the one
// running.
func TestApplyAlpineFlavour(t *testing.T) {
	sc := named(t, "main-alpine")
	sc.files = maps.Clone(sc.files)
	sc.files[sshd.DropInDir+"/20-site.conf"] = "UsePAM yes\n"
	h, p, opts := applyHost(t, sc)
	if !slices.Contains(p.Keywords, "usepam") {
		t.Errorf("Keywords %q lack usepam", p.Keywords)
	}
	after, _, err := Check(context.Background(), h, p, opts)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reload.PlanFor(reload.Input{SSH: sc.ssh, Change: p.Reload(after)}); !errors.Is(err, reload.ErrFlavour) {
		t.Errorf("reload.PlanFor = %v, want reload.ErrFlavour", err)
	}
}

// TestApplyNotEstablished: a migrated configuration whose sshd -T lacks a
// value Apply compares, or a plan whose files are not its Sources, is
// refused before anything is written.
func TestApplyNotEstablished(t *testing.T) {
	h, p, opts := applyHost(t, named(t, "v032-debian"))
	was := snapshot(h)
	h.filterT = func(out string) string { return strings.Replace(out, "pubkeyauthentication yes\n", "", 1) }
	if _, err := Apply(context.Background(), h, p, opts); !errors.Is(err, ErrNotEstablished) || !strings.Contains(err.Error(), "pubkeyauthentication") {
		t.Errorf("Apply = %v, want ErrNotEstablished naming pubkeyauthentication", err)
	}
	h.filterT = nil
	p.Sources = p.Sources[1:]
	if _, err := Apply(context.Background(), h, p, opts); !errors.Is(err, fs.ErrInvalid) {
		t.Errorf("Apply with a file outside Sources = %v, want fs.ErrInvalid", err)
	}
	if !mapsEqual(snapshot(h), was) {
		t.Error("a refused plan changed the host")
	}
}

// TestNarrowings pins each rule, and that widening is not a narrowing.
func TestNarrowings(t *testing.T) {
	base := "port 22\nlistenaddress [::]:22\nlistenaddress 0.0.0.0:22\npermitrootlogin prohibit-password\npubkeyauthentication yes\n" +
		"passwordauthentication yes\nkbdinteractiveauthentication no\npermitemptypasswords no\nusepam yes\n" +
		"authorizedkeysfile .ssh/authorized_keys .ssh_%u/authorized_keys\nauthenticationmethods any\nciphers aes256-gcm@openssh.com\nallowusers alice\nallowusers bob\n" +
		"hostkey /etc/ssh/ssh_host_rsa_key\nhostkey /etc/ssh/ssh_host_ed25519_key\nauthorizedkeyscommand /usr/local/bin/keys\ngssapiauthentication yes\n"
	eff := func(s string) sshd.Effective {
		e, _ := sshd.Evaluate(sshd.EffectiveInputs{Effective: []byte(s)})
		return e
	}
	edit := func(old, new string) string { return strings.Replace(base, old, new, 1) }
	tests := []struct {
		name  string
		after string
		hold  []sshd.Listener
		want  []string
	}{
		{"same", base, nil, nil},
		{"pubkey off", edit("pubkeyauthentication yes", "pubkeyauthentication no"), nil, []string{"pubkeyauthentication"}},
		{"password off", edit("passwordauthentication yes", "passwordauthentication no"), nil, []string{"passwordauthentication"}},
		{"keyboard-interactive on", edit("kbdinteractiveauthentication no", "kbdinteractiveauthentication yes"), nil, nil},
		{"PAM off", edit("usepam yes", "usepam no"), nil, []string{"usepam"}},
		{"root lower", edit("permitrootlogin prohibit-password", "permitrootlogin no"), nil, []string{"permitrootlogin"}},
		{"root higher", edit("permitrootlogin prohibit-password", "permitrootlogin yes"), nil, nil},
		{"a user no longer allowed", edit("allowusers bob\n", ""), nil, []string{"allowusers"}},
		{"no restriction", strings.ReplaceAll(base, "allowusers alice\nallowusers bob\n", ""), nil, nil},
		{"a user denied", base + "denyusers bob\n", nil, []string{"denyusers"}},
		{"a key file dropped", edit(" .ssh_%u/authorized_keys", ""), nil, []string{"authorizedkeysfile"}},
		{"a key file added", edit(".ssh_%u/authorized_keys", ".ssh_%u/authorized_keys /etc/ssh/keys/%u"), nil, nil},
		{"publickey only", edit("authenticationmethods any", "authenticationmethods publickey"), nil, []string{"authenticationmethods"}},
		{"ciphers changed", edit("ciphers aes256-gcm@openssh.com", "ciphers chacha20-poly1305@openssh.com"), nil, []string{"ciphers"}},
		{"a host key dropped", edit("hostkey /etc/ssh/ssh_host_rsa_key\n", ""), nil, []string{"hostkey"}},
		{"an AuthorizedKeysCommand removed", edit("authorizedkeyscommand /usr/local/bin/keys", "authorizedkeyscommand none"), nil, []string{"authorizedkeyscommand"}},
		{"GSSAPI off", edit("gssapiauthentication yes", "gssapiauthentication no"), nil, []string{"gssapiauthentication"}},
		{"IPv6 dropped", edit("listenaddress [::]:22\n", ""), nil, []string{"listenaddress"}},
		{"a port added", base + "port 2230\nlistenaddress 0.0.0.0:2230\n", nil, nil},
		{"held dual-stack port on IPv4 only", base + "listenaddress 0.0.0.0:2230\n",
			[]sshd.Listener{{Addr: eff(base).Listeners.SSHD[0].Addr, Port: 2230}}, []string{"listenaddress"}},
		{"held dual-stack port on both", base + "listenaddress 0.0.0.0:2230\nlistenaddress [::]:2230\n",
			[]sshd.Listener{{Addr: eff(base).Listeners.SSHD[0].Addr, Port: 2230}}, nil},
		{"empty passwords on", edit("permitemptypasswords no", "permitemptypasswords yes"), nil, nil},
		{"a group no longer allowed", base + "allowgroups ssh\n", nil, []string{"allowgroups"}},
		{"a group denied", base + "denygroups wheel\n", nil, []string{"denygroups"}},
		{"MaxAuthTries changed", base + "maxauthtries 4\n", nil, []string{"maxauthtries"}},
		{"revoked keys added", base + "revokedkeys /etc/ssh/revoked\n", nil, []string{"revokedkeys"}},
		{"a CA added", base + "trustedusercakeys /etc/ssh/ca.pub\n", nil, []string{"trustedusercakeys"}},
		{"a ForceCommand", base + "forcecommand /usr/bin/false\n", nil, []string{"forcecommand"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got []string
			for _, n := range narrowings(eff(base), eff(tt.after), eff(base).Listeners.SSHD, tt.hold) {
				got = append(got, n.Keyword)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("narrowings = %q, want %q", got, tt.want)
			}
		})
	}
	// Methods that are on before and off after; allowed groups that shrink.
	on := base + "kbdinteractiveauthentication yes\nhostbasedauthentication yes\nkerberosauthentication yes\npermitemptypasswords yes\nallowgroups ssh\nallowgroups adm\n"
	on = strings.Replace(on, "kbdinteractiveauthentication no\n", "", 1)
	on = strings.Replace(on, "permitemptypasswords no\n", "", 1)
	for _, tt := range []struct{ name, old, new, want string }{
		{"keyboard-interactive off", "kbdinteractiveauthentication yes", "kbdinteractiveauthentication no", "kbdinteractiveauthentication"},
		{"host-based off", "hostbasedauthentication yes", "hostbasedauthentication no", "hostbasedauthentication"},
		{"Kerberos off", "kerberosauthentication yes", "kerberosauthentication no", "kerberosauthentication"},
		{"empty passwords off", "permitemptypasswords yes", "permitemptypasswords no", "permitemptypasswords"},
		{"a group dropped", "allowgroups adm\n", "", "allowgroups"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var got []string
			for _, n := range narrowings(eff(on), eff(strings.Replace(on, tt.old, tt.new, 1)), nil, nil) {
				got = append(got, n.Keyword)
			}
			if !slices.Equal(got, []string{tt.want}) {
				t.Errorf("narrowings = %q, want [%s]", got, tt.want)
			}
		})
	}
	// keep is sshd's: [::] there is IPv6 only, so 0.0.0.0:22 is not covered by it.
	if ns := narrowings(eff(base), eff(edit("listenaddress 0.0.0.0:22\n", "")), eff(base).Listeners.SSHD, nil); len(ns) != 1 {
		t.Errorf("sshd's IPv4 listener dropped: narrowings %v", ns)
	}
	// Without keep (a socket host whose socket set is held), sshd's set is not compared.
	if ns := narrowings(eff(base), eff(edit("listenaddress 0.0.0.0:22\n", "")), nil, nil); len(ns) != 0 {
		t.Errorf("narrowings without keep = %v", ns)
	}
}

// TestCheckRollback: the rollback's reload, as the journal keeps it
// (Plan.Rollback), planned at plan time over the drop-ins as they are
// before the change. Every host shape passes but the two whose rollback's
// daemon-reload would not give ssh.socket back what it is configured for
// now: listen.conf written without a daemon-reload (D9, the socket still on
// 22), and a socket configured for 22 beside listen.conf's 2230; both are
// reload.ErrRestoredListeners, before anything changes.
func TestCheckRollback(t *testing.T) {
	more := named(t, "v032-ubuntu")
	more.name, more.socket = "v032-ubuntu, the socket on 22 too", socket22+"Listen=[::]:2230 (Stream)\n"
	for _, sc := range append(scenarios(t), more) {
		t.Run(sc.name, func(t *testing.T) {
			h, p, err := plan(t, sc)
			if err != nil {
				t.Fatal(err)
			}
			c := p.Rollback()
			if !c.Rollback || !slices.Equal(c.Keywords, p.Keywords) || !slices.Equal(c.After, p.Listening.All) {
				t.Errorf("Rollback = %+v, want Keywords and After = Listening.All %v", c, p.Listening.All)
			}
			removes := slices.ContainsFunc(p.Ops, func(op Op) bool { return op.Kind == OpRemoveSocketDropIn })
			if want := removes && sc.ssh.Mode == detect.SSHSocket; want != slices.Equal(c.RestoredDropIns, []string{ListenConf}) || !want && c.RestoredDropIns != nil {
				t.Errorf("RestoredDropIns %v, want listen.conf %v", c.RestoredDropIns, want)
			}
			d, err := reload.ReadSocketDropIns(context.Background(), h)
			if err != nil {
				t.Fatal(err)
			}
			err = p.CheckRollback(d)
			if refused := sc.name == "v032-ubuntu-unreloaded" || sc.name == more.name; refused != errors.Is(err, reload.ErrRestoredListeners) || !refused && err != nil {
				t.Errorf("CheckRollback = %v, want refused %v", err, refused)
			}
		})
	}
}
