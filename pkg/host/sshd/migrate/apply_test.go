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
	return h, p, Options{RunDir: runDir, Platform: sc.platform, Backup: run}
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

// TestApplyScenarios migrates every host shape: each state on disk is
// validated before it exists, every file is backed up first, the files end
// as the plan says, the effective login paths are kept, and restoring the
// backups gives the host back as it was.
func TestApplyScenarios(t *testing.T) {
	for _, sc := range scenarios(t) {
		t.Run(sc.name, func(t *testing.T) {
			h, p, opts := applyHost(t, sc)
			was := snapshot(h)
			from := len(h.Calls())
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
			if ns := narrowings(p.Before, h.before(t, sc.ssh, sc.socket), p.Hold); len(ns) > 0 {
				t.Errorf("the installed files narrow the login paths: %v", ns)
			}
			for _, l := range slices.Concat(p.Before.Listeners.SSHD, p.Hold) {
				if !slices.Contains(rep.Kept, l) {
					t.Errorf("Kept %v lacks %v", rep.Kept, l)
				}
			}
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
		{"Debian without /run/sshd", func(h *testHost, _ *Plan, o *Options) {
			o.Platform = debian13
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
	if opts.Platform.Distro != detect.DistroAlpine {
		t.Fatal("scenario is not Alpine")
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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got []string
			for _, n := range narrowings(eff(base), eff(tt.after), tt.hold) {
				got = append(got, n.Keyword)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("narrowings = %q, want %q", got, tt.want)
			}
		})
	}
}
