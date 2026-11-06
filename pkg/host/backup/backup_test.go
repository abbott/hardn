package backup

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/hostfake"
)

var update = flag.Bool("update", false, "rewrite testdata/manifest.json instead of comparing")

// world is a System with a root directory to work under: real files in
// t.TempDir(), or the fake's "/".
type world struct {
	name string
	sys  host.System
	root string
	// canon is root with no symlink in it, as checkTrusted and
	// ResolveSymlink report paths.
	canon   string
	file    func(rel, data string, perm fs.FileMode)
	dir     func(rel string, perm fs.FileMode)
	symlink func(rel, target string)
}

func (w world) path(rel string) string      { return filepath.Join(w.root, rel) }
func (w world) canonical(rel string) string { return filepath.Join(w.canon, rel) }

// realWorld works in t.TempDir(), which it makes the top of checkTrusted's
// walk (trustedTop): on Linux it is under /tmp, which checkTrusted refuses.
func realWorld(t *testing.T) world {
	root := t.TempDir()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.Chmod(root, 0o755)) // trusted whatever the umask
	canon, err := filepath.EvalSymlinks(root)
	must(err)
	prev := trustedTop
	trustedTop = canon
	t.Cleanup(func() { trustedTop = prev })
	return world{
		name: "real", sys: host.NewReal(host.Options{}), root: root, canon: canon,
		file: func(rel, data string, perm fs.FileMode) {
			p := filepath.Join(root, rel)
			must(os.MkdirAll(filepath.Dir(p), 0o755))
			must(os.WriteFile(p, []byte(data), perm))
			must(os.Chmod(p, perm))
		},
		dir: func(rel string, perm fs.FileMode) {
			p := filepath.Join(root, rel)
			must(os.MkdirAll(p, 0o755))
			must(os.Chmod(p, perm))
		},
		symlink: func(rel, target string) {
			p := filepath.Join(root, rel)
			must(os.MkdirAll(filepath.Dir(p), 0o755))
			must(os.Symlink(target, p))
		},
	}
}

func fakeWorld(*testing.T) world {
	f := hostfake.New(host.Options{})
	return world{
		name: "fake", sys: f, root: "/", canon: "/",
		file:    func(rel, data string, perm fs.FileMode) { f.AddFile("/"+rel, []byte(data), perm, 0, 0) },
		dir:     func(rel string, perm fs.FileMode) { f.AddDir("/"+rel, perm, 0, 0) },
		symlink: func(rel, target string) { f.AddSymlink("/"+rel, target) },
	}
}

// eachWorld runs test in a subtest per world, each made inside its subtest.
func eachWorld(t *testing.T, test func(t *testing.T, w world)) {
	for _, mk := range []struct {
		name string
		new  func(*testing.T) world
	}{{"real", realWorld}, {"fake", fakeWorld}} {
		t.Run(mk.name, func(t *testing.T) { test(t, mk.new(t)) })
	}
}

// fixedClock pins the manifest's creation time.
func fixedClock(t *testing.T) {
	prev := now
	now = func() time.Time { return time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { now = prev })
}

const runID = "20261017T120000Z-0a1b2c3d"

// A file, a path that does not exist yet and a Proxmox-style symlinked keys
// file are backed up, changed, and restored; a second restore changes
// nothing.
func TestBackupRestoreRoundTrip(t *testing.T) {
	fixedClock(t)
	ctx := context.Background()
	eachWorld(t, func(t *testing.T, w world) {
		w.file("etc/ssh/sshd_config", "Port 22\n", 0o644)
		w.file("etc/pve/priv/authorized_keys", "ssh-ed25519 KEY root@web-01.example.net\n", 0o600)
		w.symlink("root/.ssh/authorized_keys", w.path("etc/pve/priv/authorized_keys"))
		w.file("etc/sudoers.d/README", "", 0o440)
		run, err := Open(w.sys, Settings{Path: w.path("var/backups/hardn"), Enabled: true}, runID)
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range []string{"etc/ssh/sshd_config", "etc/sudoers.d/hardn-alice", "root/.ssh/authorized_keys"} {
			res, err := run.Backup(ctx, w.path(p), RollbackRequired)
			if err != nil || res.Outcome != Taken {
				t.Fatalf("Backup(%s) = %v, %v; want Taken", p, res.Outcome, err)
			}
		}
		m, err := Load(ctx, w.sys, run.Dir())
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(m, run.Manifest()) {
			t.Errorf("loaded manifest\n%+v\nwant\n%+v", m, run.Manifest())
		}
		if w.name == "fake" {
			compareGolden(t, w.sys, run.Dir())
		}
		wantKinds(t, m, KindFile, KindAbsent, KindSymlink)

		// The change: new content and mode, a new file, a write through
		// the link to the file it points to.
		must(t, w.sys.WriteFileAtomic(ctx, w.path("etc/ssh/sshd_config"), []byte("Port 2230\n"), 0o600, -1, -1))
		must(t, w.sys.WriteFileAtomic(ctx, w.path("etc/sudoers.d/hardn-alice"), []byte("alice ALL=(ALL) ALL\n"), 0o440, -1, -1))
		must(t, w.sys.WriteFileAtomic(ctx, w.path("etc/pve/priv/authorized_keys"), []byte("changed\n"), 0o600, -1, -1))

		got, err := Restore(ctx, w.sys, m)
		if err != nil {
			t.Fatal(err)
		}
		wantActions(t, got, Rewritten, Removed, Rewritten) // last taken first
		wantFile(t, w, "etc/ssh/sshd_config", "Port 22\n", 0o644)
		wantFile(t, w, "etc/pve/priv/authorized_keys", "ssh-ed25519 KEY root@web-01.example.net\n", 0o600)
		if _, err := w.sys.Lstat(ctx, w.path("etc/sudoers.d/hardn-alice")); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("created file still there: %v", err)
		}
		if target, err := w.sys.Readlink(ctx, w.path("root/.ssh/authorized_keys")); err != nil || target != w.path("etc/pve/priv/authorized_keys") {
			t.Errorf("symlink now -> %q (%v)", target, err)
		}

		again, err := Restore(ctx, w.sys, m)
		if err != nil {
			t.Fatal(err)
		}
		wantActions(t, again, Unchanged, Unchanged, Unchanged)
		for i, r := range again {
			if r.Seq != 3-i {
				t.Errorf("restored[%d].Seq = %d, want %d", i, r.Seq, 3-i)
			}
		}
	})
}

// The run directory and every copy are private to their owner.
func TestBackupModes(t *testing.T) {
	ctx := context.Background()
	w := realWorld(t)
	w.file("etc/hardn/hardn.yml", "username: alice\n", 0o644)
	run, err := Open(w.sys, Settings{Path: w.path("var/backups/hardn"), Enabled: true}, runID)
	if err != nil {
		t.Fatal(err)
	}
	res, err := run.Backup(ctx, w.path("etc/hardn/hardn.yml"), Optional)
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]fs.FileMode{
		run.Dir():                              fs.ModeDir | 0o700,
		filepath.Join(run.Dir(), ManifestName): 0o600,
		filepath.Join(run.Dir(), res.Entry.File.Backup): 0o600,
	} {
		if fi, err := os.Lstat(name); err != nil || fi.Mode() != want {
			t.Errorf("%s: mode %v (%v), want %v", name, fi.Mode(), err, want)
		}
	}
	if res.Entry.File.Backup != "0001-hardn.yml" {
		t.Errorf("copy named %q, want 0001-hardn.yml", res.Entry.File.Backup)
	}
	m := run.Manifest()
	m.Entries[0].File.SHA256 = "changed by the caller"
	if got := run.Manifest().Entries[0].File.SHA256; got == m.Entries[0].File.SHA256 {
		t.Errorf("Manifest() shares its entries' content with the run")
	}
}

// enableBackups=false skips optional backups and creates nothing, but takes
// the ones a rollback needs, and says so (§3.16).
func TestBackupDisabled(t *testing.T) {
	ctx := context.Background()
	w := fakeWorld(t)
	w.file("etc/ssh/sshd_config", "Port 22\n", 0o644)
	run, err := Open(w.sys, Settings{Path: "/var/backups/hardn"}, runID)
	if err != nil {
		t.Fatal(err)
	}
	res, err := run.Backup(ctx, "/etc/ssh/sshd_config", Optional)
	if err != nil || res.Outcome != Skipped || res.Entry.Path != "" {
		t.Fatalf("Optional = %+v, %v; want Skipped and no entry", res, err)
	}
	if _, err := w.sys.Lstat(ctx, "/var/backups/hardn"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("backup directory created for a skipped backup: %v", err)
	}
	if got, err := Restore(ctx, w.sys, run.Manifest()); got != nil || err != nil {
		t.Errorf("Restore(nothing taken) = %v, %v; want nothing to do", got, err)
	}
	res, err = run.Backup(ctx, "/etc/ssh/sshd_config", RollbackRequired)
	if err != nil || res.Outcome != TakenAnyway || !res.Entry.TakenAnyway || !res.Entry.RollbackRequired {
		t.Fatalf("RollbackRequired = %+v, %v; want TakenAnyway", res, err)
	}
	m, err := Load(ctx, w.sys, run.Dir())
	if err != nil || m.BackupsEnabled || len(m.Entries) != 1 || !m.Entries[0].TakenAnyway {
		t.Errorf("manifest = %+v, %v; want backupsEnabled false and one entry taken anyway", m, err)
	}
	if !strings.Contains(TakenAnyway.String(), "enableBackups is false") {
		t.Errorf("TakenAnyway.String() = %q", TakenAnyway)
	}
}

func TestOpenRefuses(t *testing.T) {
	sys := hostfake.New(host.Options{})
	for _, tt := range []struct {
		path, id string
	}{
		{"", runID}, {"backups", runID}, {"/var/backups/hardn", ""}, {"/var/backups/hardn", "../etc"},
		{"/var/backups/hardn", "a/b"}, {"/var/backups/hardn", "."}, {"/var/backups/hardn", "-x"},
		{"/var/backups/hardn", strings.Repeat("a", 65)},
	} {
		if _, err := Open(sys, Settings{Path: tt.path, Enabled: true}, tt.id); !errors.Is(err, ErrSettings) {
			t.Errorf("Open(%q, %q) = %v, want ErrSettings", tt.path, tt.id, err)
		}
	}
	if r, err := Open(sys, Settings{Path: "/var/backups/hardn/", Enabled: true}, runID); err != nil || r.Dir() != "/var/backups/hardn/"+runID {
		t.Errorf("trailing slash: %v, %v", r, err)
	}
	if id := NewRunID(time.Date(2026, 10, 17, 14, 30, 5, 0, time.UTC)); !validRunID(id) || !strings.HasPrefix(id, "20261017T143005Z-") || len(id) != 25 {
		t.Errorf("NewRunID = %q", id)
	}
}

func TestBackupRefuses(t *testing.T) {
	ctx := context.Background()
	t.Run("a directory, a relative path, a reused run id", func(t *testing.T) {
		w := fakeWorld(t)
		w.file("etc/ssh/sshd_config", "", 0o644)
		run, _ := Open(w.sys, Settings{Path: "/var/backups/hardn", Enabled: true}, runID)
		if _, err := run.Backup(ctx, "/etc/ssh", Optional); !errors.Is(err, ErrNotRegular) {
			t.Errorf("directory: %v, want ErrNotRegular", err)
		}
		if _, err := run.Backup(ctx, "etc/ssh/sshd_config", Optional); !errors.Is(err, fs.ErrInvalid) {
			t.Errorf("relative: %v, want ErrInvalid", err)
		}
		again, _ := Open(w.sys, Settings{Path: "/var/backups/hardn", Enabled: true}, runID)
		if _, err := again.Backup(ctx, "/etc/ssh/sshd_config", Optional); !errors.Is(err, ErrRunExists) {
			t.Errorf("reused run id: %v, want ErrRunExists", err)
		}
	})
	t.Run("a backup directory another user can change", func(t *testing.T) {
		w := realWorld(t)
		w.file("etc/sshd_config", "", 0o644)
		w.dir("srv/shared", 0o777)
		// The sticky bit (as on /tmp) is no exception.
		w.dir("srv/tmp", fs.ModeSticky|0o777)
		for _, rel := range []string{"srv/shared", "srv/tmp"} {
			run, _ := Open(w.sys, Settings{Path: w.path(rel + "/backups"), Enabled: true}, runID)
			_, err := run.Backup(ctx, w.path("etc/sshd_config"), RollbackRequired)
			wantUnsafe(t, err, w.canonical(rel))
		}
		if os.Geteuid() == 0 {
			t.Skip("as root, the test's directories are root's")
		}
		prev := geteuid
		geteuid = func() int { return os.Geteuid() + 1 }
		t.Cleanup(func() { geteuid = prev })
		run, _ := Open(w.sys, Settings{Path: w.path("var/backups"), Enabled: true}, runID)
		_, err := run.Backup(ctx, w.path("etc/sshd_config"), RollbackRequired)
		wantUnsafe(t, err, w.canon)
	})
}

// wantUnsafe fails unless err is ErrUnsafeDir for the directory dir.
func wantUnsafe(t *testing.T, err error, dir string) {
	t.Helper()
	if !errors.Is(err, ErrUnsafeDir) || !strings.Contains(err.Error(), " "+dir+": ") {
		t.Errorf("err = %v, want ErrUnsafeDir for %s", err, dir)
	}
}

// Nothing is created before the directories above the run directory pass
// the check, missing ones are created 0700 one at a time, and a failure
// removes what the call created, so the run can try again.
func TestBackupCreatesTheRunDirectory(t *testing.T) {
	ctx := context.Background()
	t.Run("nothing is created through a link in a directory another user can change", func(t *testing.T) {
		w := realWorld(t)
		w.file("etc/hardn/hardn.yml", "x", 0o644)
		w.dir("etc/cron.d", 0o755)
		w.dir("shared", 0o777)
		w.symlink("shared/backups", w.path("etc/cron.d"))
		run, _ := Open(w.sys, Settings{Path: w.path("shared/backups"), Enabled: true}, runID)
		for range 2 { // the second time too: not ErrRunExists
			_, err := run.Backup(ctx, w.path("etc/hardn/hardn.yml"), RollbackRequired)
			wantUnsafe(t, err, w.canonical("shared"))
		}
		if entries, err := os.ReadDir(w.path("etc/cron.d")); err != nil || len(entries) != 0 {
			t.Errorf("etc/cron.d holds %v (%v), want nothing", entries, err)
		}
	})
	t.Run("missing parents are created 0700", func(t *testing.T) {
		w := realWorld(t)
		w.file("etc/hardn/hardn.yml", "x", 0o644)
		run, _ := Open(w.sys, Settings{Path: w.path("var/backups/hardn"), Enabled: true}, runID)
		if _, err := run.Backup(ctx, w.path("etc/hardn/hardn.yml"), RollbackRequired); err != nil {
			t.Fatal(err)
		}
		for _, rel := range []string{"var", "var/backups", "var/backups/hardn", "var/backups/hardn/" + runID} {
			if fi, err := os.Lstat(w.path(rel)); err != nil || fi.Mode() != fs.ModeDir|0o700 {
				t.Errorf("%s: %v (%v), want a 0700 directory", rel, modeOf(fi), err)
			}
		}
	})
	t.Run("a failure removes what the call created", func(t *testing.T) {
		w := fakeWorld(t)
		w.file("etc/hardn/hardn.yml", "x", 0o644)
		sys := &failing{System: w.sys, fail: map[string]error{"mkdir:" + runID: errInjected}}
		run, _ := Open(sys, Settings{Path: "/var/backups/hardn", Enabled: true}, runID)
		if _, err := run.Backup(ctx, "/etc/hardn/hardn.yml", RollbackRequired); !errors.Is(err, errInjected) {
			t.Fatalf("err = %v, want the injected failure", err)
		}
		if _, err := w.sys.Lstat(ctx, "/var/backups"); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("/var/backups left behind: %v", err)
		}
		sys.fail = nil
		if _, err := run.Backup(ctx, "/etc/hardn/hardn.yml", RollbackRequired); err != nil {
			t.Errorf("second try: %v", err)
		}
	})
}

// A symlink a user planted in a directory they can change, at the path or on
// the way to it, is refused by Backup, as every guarded operation refuses it
// (V21), and Restore refuses a link whose directory became one since.
func TestBackupRefusesUntrustedSymlinks(t *testing.T) {
	ctx := context.Background()
	eachWorld(t, func(t *testing.T, w world) {
		w.file("etc/shadow", "root:old-hash\n", 0o600)
		w.dir("home/alice/.ssh", 0o777)
		w.symlink("home/alice/.ssh/authorized_keys", w.path("etc/shadow"))
		w.symlink("home/alice/.ssh/authorized_keys2", w.path("etc/sudoers.d/hardn-alice")) // to nothing
		w.symlink("srv/x/authorized_keys", w.path("etc/shadow"))
		w.dir("home/bob", 0o777)
		w.symlink("home/bob/.ssh", w.path("srv/x"))
		run, _ := Open(w.sys, Settings{Path: w.path("var/backups/hardn"), Enabled: true}, runID)
		for _, rel := range []string{"home/alice/.ssh/authorized_keys", "home/alice/.ssh/authorized_keys2", "home/bob/.ssh/authorized_keys"} {
			if _, err := run.Backup(ctx, w.path(rel), RollbackRequired); !errors.Is(err, host.ErrSymlinkParent) {
				t.Errorf("%s: err = %v, want ErrSymlinkParent", rel, err)
			}
		}
		if n := len(run.Manifest().Entries); n != 0 {
			t.Fatalf("%d entries recorded, want none", n)
		}

		w.dir("root/.ssh", 0o700)
		w.symlink("root/.ssh/authorized_keys", w.path("etc/shadow"))
		if _, err := run.Backup(ctx, w.path("root/.ssh/authorized_keys"), RollbackRequired); err != nil {
			t.Fatal(err)
		}
		must(t, w.sys.WriteFileAtomic(ctx, w.path("etc/shadow"), []byte("root:new-hash\n"), 0o600, -1, -1))
		must(t, w.sys.Chmod(ctx, w.path("root/.ssh"), 0o777))
		got, err := Restore(ctx, w.sys, run.Manifest())
		if !errors.Is(err, host.ErrSymlinkParent) {
			t.Errorf("Restore: err = %v, want ErrSymlinkParent", err)
		}
		wantActions(t, got, 0)
		wantFile(t, w, "etc/shadow", "root:new-hash\n", 0o600)
	})
}

// A relative target is resolved against the directory that holds the link,
// as the kernel reaches it, not against the path's spelling; a re-pointed
// link is ErrChanged; a target with ".." after another component is refused.
func TestBackupSymlinkResolution(t *testing.T) {
	ctx := context.Background()
	eachWorld(t, func(t *testing.T, w world) {
		w.file("real/target", "orig\n", 0o644)
		w.file("real/other", "other\n", 0o644)
		w.symlink("alias", w.path("real/sub"))
		w.symlink("real/sub/l", "../target")
		w.symlink("real/sub/m", "../target")
		w.symlink("real/sub/interior", "x/../target")
		run, _ := Open(w.sys, Settings{Path: w.path("var/backups/hardn"), Enabled: true}, runID)
		for _, rel := range []string{"alias/l", "alias/m"} {
			res, err := run.Backup(ctx, w.path(rel), RollbackRequired)
			if err != nil || res.Entry.SymlinkResolved != w.canonical("real/target") || res.Entry.File == nil {
				t.Fatalf("Backup(%s) = %+v, %v; want the content of %s", rel, res.Entry, err, w.canonical("real/target"))
			}
		}
		w.symlink("real/sub/dir", "..")
		w.symlink("real/sub/chain", "l")
		for rel, want := range map[string]error{
			"alias/interior": fs.ErrInvalid,   // ".." after another component
			"alias/dir":      ErrNotRegular,   // a link to a directory
			"alias/chain":    host.ErrSymlink, // a link to a link
		} {
			if _, err := run.Backup(ctx, w.path(rel), RollbackRequired); !errors.Is(err, want) {
				t.Errorf("%s: err = %v, want %v", rel, err, want)
			}
		}
		must(t, w.sys.WriteFileAtomic(ctx, w.path("real/target"), []byte("changed\n"), 0o644, -1, -1))
		must(t, w.sys.Remove(ctx, w.path("real/sub/m")))
		w.symlink("real/sub/m", "../other")
		got, err := Restore(ctx, w.sys, run.Manifest())
		if !errors.Is(err, ErrChanged) {
			t.Errorf("re-pointed link: err = %v, want ErrChanged", err)
		}
		wantActions(t, got, 0, Rewritten)
		wantFile(t, w, "real/target", "orig\n", 0o644)
		wantFile(t, w, "real/other", "other\n", 0o644)
	})
}

// A symlink to nothing is recorded without content; Restore removes the file
// a change then created through it, and leaves the link.
func TestBackupDanglingSymlink(t *testing.T) {
	ctx := context.Background()
	eachWorld(t, func(t *testing.T, w world) {
		w.dir("etc/pve/priv", 0o700)
		w.symlink("root/.ssh/authorized_keys", w.path("etc/pve/priv/authorized_keys"))
		w.symlink("root/.ssh/authorized_keys2", w.path("etc/pve/missing/authorized_keys"))
		run, _ := Open(w.sys, Settings{Path: w.path("var/backups/hardn"), Enabled: true}, runID)
		for _, rel := range []string{"root/.ssh/authorized_keys", "root/.ssh/authorized_keys2"} {
			res, err := run.Backup(ctx, w.path(rel), RollbackRequired)
			if err != nil || res.Entry.Kind != KindSymlink || res.Entry.File != nil {
				t.Fatalf("Backup(%s) = %+v, %v; want a symlink to nothing", rel, res.Entry, err)
			}
		}
		link := w.path("root/.ssh/authorized_keys")
		must(t, host.WriteFileThroughSymlink(ctx, w.sys, link, []byte("ssh-ed25519 KEY root@web-01.example.net\n"), 0o600, -1, -1))
		m, err := Load(ctx, w.sys, run.Dir())
		if err != nil {
			t.Fatal(err)
		}
		got, err := Restore(ctx, w.sys, m)
		if err != nil {
			t.Fatal(err)
		}
		wantActions(t, got, Unchanged, Removed)
		if _, err := w.sys.Lstat(ctx, w.path("etc/pve/priv/authorized_keys")); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("created file still there: %v", err)
		}
		if target, err := w.sys.Readlink(ctx, link); err != nil || target != w.path("etc/pve/priv/authorized_keys") {
			t.Errorf("symlink now -> %q (%v)", target, err)
		}
		again, err := Restore(ctx, w.sys, m)
		if err != nil {
			t.Fatal(err)
		}
		wantActions(t, again, Unchanged, Unchanged)
	})
}

// A copy or a manifest that cannot be written records nothing: the change
// must not go ahead, and the manifest on disk still lists every copy.
func TestBackupWriteFailures(t *testing.T) {
	ctx := context.Background()
	w := realWorld(t)
	w.file("etc/a", "a\n", 0o644)
	w.file("etc/b", "b\n", 0o644)
	sys := &failing{System: w.sys}
	run, _ := Open(sys, Settings{Path: w.path("backups"), Enabled: true}, runID)
	if _, err := run.Backup(ctx, w.path("etc/a"), RollbackRequired); err != nil {
		t.Fatal(err)
	}
	for _, fail := range []string{"0002-b", ManifestName} {
		sys.fail = map[string]error{"write:" + fail: errInjected}
		if _, err := run.Backup(ctx, w.path("etc/b"), RollbackRequired); !errors.Is(err, errInjected) {
			t.Errorf("failing %s: err = %v, want the injected failure", fail, err)
		}
		m, err := Load(ctx, w.sys, run.Dir())
		if err != nil || len(m.Entries) != 1 || len(run.Manifest().Entries) != 1 {
			t.Errorf("failing %s: %d entries on disk (%v), %d in memory; want 1", fail, len(m.Entries), err, len(run.Manifest().Entries))
		}
	}
}

var errInjected = errors.New("injected failure")

// failing fails the calls named in fail by operation and base name
// ("write:a", "remove:a", "mkdir:a") with the error given. A write failing
// with host.ErrNotDurable writes first, as host.Real does.
type failing struct {
	host.System
	fail map[string]error
}

func (f *failing) WriteFileAtomic(ctx context.Context, name string, data []byte, perm fs.FileMode, uid, gid int) error {
	err := f.fail["write:"+filepath.Base(name)]
	if err == nil || errors.Is(err, host.ErrNotDurable) {
		if werr := f.System.WriteFileAtomic(ctx, name, data, perm, uid, gid); werr != nil {
			return werr
		}
	}
	return err
}

func (f *failing) Remove(ctx context.Context, name string) error {
	if err := f.fail["remove:"+filepath.Base(name)]; err != nil {
		return err
	}
	return f.System.Remove(ctx, name)
}

func (f *failing) MkdirAll(ctx context.Context, name string, perm fs.FileMode) error {
	if err := f.fail["mkdir:"+filepath.Base(name)]; err != nil {
		return err
	}
	return f.System.MkdirAll(ctx, name, perm)
}

// Restore goes on past an entry it cannot write or remove, and reports a
// copy put back without its directory synced as Rewritten, with the error.
func TestRestoreFailures(t *testing.T) {
	ctx := context.Background()
	w := fakeWorld(t)
	w.file("etc/a", "a\n", 0o644)
	w.file("etc/b", "b\n", 0o644)
	sys := &failing{System: w.sys}
	run, _ := Open(sys, Settings{Path: "/var/backups/hardn", Enabled: true}, runID)
	for _, p := range []string{"/etc/a", "/etc/b", "/etc/c"} {
		if _, err := run.Backup(ctx, p, RollbackRequired); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range []string{"/etc/a", "/etc/b", "/etc/c"} {
		must(t, w.sys.WriteFileAtomic(ctx, p, []byte("changed\n"), 0o600, -1, -1))
	}
	sys.fail = map[string]error{
		"write:a":  errInjected,
		"write:b":  fmt.Errorf("host: write /etc/b: %w: %w", host.ErrNotDurable, errInjected),
		"remove:c": errInjected,
	}
	got, err := Restore(ctx, sys, run.Manifest())
	if !errors.Is(err, errInjected) || !errors.Is(err, host.ErrNotDurable) {
		t.Errorf("err = %v, want every failure", err)
	}
	wantActions(t, got, 0, Rewritten, 0) // c, b, a
	if len(got) == 3 && (got[0].Seq != 3 || !errors.Is(got[1].Err, host.ErrNotDurable) || got[2].Err == nil) {
		t.Errorf("restored = %+v", got)
	}
	wantFile(t, w, "etc/a", "changed\n", 0o600)
	wantFile(t, w, "etc/b", "b\n", 0o644)
	wantFile(t, w, "etc/c", "changed\n", 0o600)
}

// Restore refuses what it cannot undo safely and goes on with the rest.
// asRoot checks run directories against root's effective uid, as hardn runs, whoever runs
// the tests: a directory uid 1000 owns is then another user's, also where the tests run as
// uid 1000 (deb13-vm's login user, where these cases failed on 2026-11-06).
func asRoot(t *testing.T) {
	t.Helper()
	prev := geteuid
	geteuid = func() int { return 0 }
	t.Cleanup(func() { geteuid = prev })
}

func TestRestoreRefuses(t *testing.T) {
	asRoot(t)
	ctx := context.Background()
	w := fakeWorld(t)
	f, ok := w.sys.(*hostfake.System)
	if !ok {
		t.Fatal("fakeWorld is not over hostfake")
	}
	w.file("etc/ssh/sshd_config", "Port 22\n", 0o644)
	w.file("etc/hardn/hardn.yml", "username: alice\n", 0o600)
	w.file("etc/pve/priv/authorized_keys", "k\n", 0o600)
	w.symlink("root/.ssh/authorized_keys", "/etc/pve/priv/authorized_keys")
	run, _ := Open(w.sys, Settings{Path: "/var/backups/hardn", Enabled: true}, runID)
	for _, p := range []string{"/etc/ssh/sshd_config", "/etc/sudoers.d/hardn-alice", "/root/.ssh/authorized_keys", "/etc/hardn/hardn.yml"} {
		if _, err := run.Backup(ctx, p, RollbackRequired); err != nil {
			t.Fatal(err)
		}
	}
	m := run.Manifest()
	// Corrupt the copy of sshd_config, put a directory where the created
	// file was, replace the symlink with a file, and change hardn.yml.
	f.AddFile(filepath.Join(m.Dir, m.Entries[0].File.Backup), []byte("Port 2230\n"), 0o600, 0, 0)
	f.AddDir("/etc/sudoers.d/hardn-alice", 0o755, 0, 0)
	must(t, w.sys.Remove(ctx, "/root/.ssh/authorized_keys"))
	f.AddFile("/root/.ssh/authorized_keys", []byte("k\n"), 0o600, 0, 0)
	must(t, w.sys.WriteFileAtomic(ctx, "/etc/hardn/hardn.yml", []byte("username: bob\n"), 0o644, 0, 0))

	got, err := Restore(ctx, w.sys, m)
	for _, want := range []error{ErrCorrupt, ErrChanged} {
		if !errors.Is(err, want) {
			t.Errorf("err = %v, want it to include %v", err, want)
		}
	}
	wantActions(t, got, Rewritten, 0, 0, 0)
	wantFile(t, w, "etc/hardn/hardn.yml", "username: alice\n", 0o600)
	wantFile(t, w, "etc/ssh/sshd_config", "Port 22\n", 0o644) // untouched: its copy is corrupt

	// A manifest built by hand, not loaded, is checked as Load checks it.
	f.AddDir("/home/alice/r", 0o755, 1000, 1000)
	unsafe := Manifest{Dir: "/home/alice/r", Entries: []Entry{{Seq: 1, Path: "/etc/hardn/hardn.yml", Kind: KindAbsent}}}
	if got, err := Restore(ctx, w.sys, unsafe); got != nil || !errors.Is(err, ErrUnsafeDir) {
		t.Errorf("unsafe run directory: %v, %v; want ErrUnsafeDir and nothing done", got, err)
	}
	bad := Manifest{Dir: m.Dir, Entries: []Entry{{Seq: 1, Path: "etc/hardn/hardn.yml", Kind: KindAbsent}}}
	if got, err := Restore(ctx, w.sys, bad); !errors.Is(err, ErrManifest) || len(got) != 1 || got[0].Action != 0 {
		t.Errorf("relative path: %v, %v; want ErrManifest", got, err)
	}
	wantFile(t, w, "etc/hardn/hardn.yml", "username: alice\n", 0o600)
}

func TestLoadRefuses(t *testing.T) {
	asRoot(t)
	ctx := context.Background()
	good := `{"schemaVersion":1,"runId":"r","entries":[{"seq":1,"path":"/etc/a","kind":"file","file":{"backup":"0001-a","mode":"0644","uid":0,"gid":0,"size":0,"sha256":"e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"}}]}`
	for _, tt := range []struct{ name, from, to string }{
		{"valid", "", ""},
		{"schema version", `"schemaVersion":1`, `"schemaVersion":2`},
		{"copy outside the run directory", `"backup":"0001-a"`, `"backup":"../../etc/shadow"`},
		{"copy named as the manifest", `"backup":"0001-a"`, `"backup":"manifest.json"`},
		{"relative path", `"path":"/etc/a"`, `"path":"etc/a"`},
		{"unclean path", `"path":"/etc/a"`, `"path":"/etc/../etc/a"`},
		{"unknown kind", `"kind":"file"`, `"kind":"fifo"`},
		{"absent with content", `"kind":"file"`, `"kind":"absent"`},
		{"a file without content", `"file":`, `"unknown":`},
		{"a symlink without a target", `"kind":"file"`, `"kind":"symlink"`},
		{"a symlink resolved to a relative path", `"kind":"file"`, `"kind":"symlink","symlinkTarget":"x","symlinkResolved":"x"`},
		{"a target on a file", `"kind":"file"`, `"kind":"file","symlinkTarget":"/x"`},
		{"no seq", `"seq":1,`, ``},
		{"seq out of order", `}}]}`, `}},{"seq":1,"path":"/etc/b","kind":"absent"}]}`},
		{"mode too large", `"mode":"0644"`, `"mode":"10000"`},
		{"mode not octal", `"mode":"0644"`, `"mode":"0999"`},
		{"short sha256", `"sha256":"e3b0`, `"sha256":"e3`},
		{"not JSON", `{`, `[`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := hostfake.New(host.Options{})
			f.AddFile("/var/backups/hardn/r/manifest.json", []byte(strings.Replace(good, tt.from, tt.to, 1)), 0o600, 0, 0)
			_, err := Load(ctx, f, "/var/backups/hardn/r")
			if tt.from == "" {
				if err != nil {
					t.Errorf("valid manifest: %v", err)
				}
				return
			}
			if !errors.Is(err, ErrManifest) {
				t.Errorf("err = %v, want ErrManifest", err)
			}
		})
	}
	t.Run("a run directory another user can change", func(t *testing.T) {
		f := hostfake.New(host.Options{})
		f.AddDir("/home/alice", 0o755, 1000, 1000)
		f.AddFile("/home/alice/r/manifest.json", []byte(good), 0o600, 0, 0)
		if _, err := Load(ctx, f, "/home/alice/r"); !errors.Is(err, ErrUnsafeDir) {
			t.Errorf("err = %v, want ErrUnsafeDir", err)
		}
	})
}

func TestPerm(t *testing.T) {
	for _, tt := range []struct {
		mode fs.FileMode
		text string
	}{
		{0o644, "0644"}, {0o440, "0440"}, {fs.ModeSetuid | 0o755, "4755"},
		{fs.ModeSetgid | fs.ModeSticky | 0o770, "3770"},
	} {
		p := PermOf(tt.mode)
		b, err := p.MarshalText()
		var back Perm
		if err != nil || string(b) != tt.text || back.UnmarshalText(b) != nil || back.FileMode() != tt.mode {
			t.Errorf("%v: %q, %v; back %v", tt.mode, b, err, back.FileMode())
		}
	}
}

// Owners need root: a restore puts back an owner that is not root's.
func TestRestoreOwner(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("setting another user's ownership needs root")
	}
	ctx := context.Background()
	w := realWorld(t)
	w.file("home/alice/.ssh/authorized_keys", "alice-key\n", 0o600)
	keys := w.path("home/alice/.ssh/authorized_keys")
	must(t, os.Chown(keys, 1234, 5678))
	run, _ := Open(w.sys, Settings{Path: w.path("var/backups/hardn"), Enabled: true}, runID)
	if _, err := run.Backup(ctx, keys, RollbackRequired); err != nil {
		t.Fatal(err)
	}
	must(t, w.sys.WriteFileAtomic(ctx, keys, []byte("changed\n"), 0o644, 0, 0))
	if _, err := Restore(ctx, w.sys, run.Manifest()); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Lstat(keys)
	if uid, gid, ok := host.Owner(fi); err != nil || !ok || uid != 1234 || gid != 5678 {
		t.Errorf("owner %d:%d (%v), want 1234:5678", uid, gid, err)
	}
	wantFile(t, w, "home/alice/.ssh/authorized_keys", "alice-key\n", 0o600)
}

// compareGolden compares the run's manifest with testdata/manifest.json, the
// format's reference (docs/safety/atomic.md).
func compareGolden(t *testing.T, sys host.System, dir string) {
	t.Helper()
	got, err := sys.ReadFile(context.Background(), filepath.Join(dir, ManifestName))
	if err != nil {
		t.Fatal(err)
	}
	golden := filepath.Join("testdata", "manifest.json")
	if *update {
		must(t, os.WriteFile(golden, got, 0o644))
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("manifest differs from %s (-update rewrites it):\n%s", golden, got)
	}
}

func wantKinds(t *testing.T, m Manifest, kinds ...Kind) {
	t.Helper()
	var got []Kind
	for _, e := range m.Entries {
		got = append(got, e.Kind)
	}
	if !reflect.DeepEqual(got, kinds) {
		t.Errorf("kinds = %q, want %q", got, kinds)
	}
}

func wantActions(t *testing.T, got []Restored, actions ...Action) {
	t.Helper()
	var as []Action
	for _, r := range got {
		as = append(as, r.Action)
	}
	if !reflect.DeepEqual(as, actions) {
		t.Errorf("actions = %v, want %v (%+v)", as, actions, got)
	}
}

func wantFile(t *testing.T, w world, rel, data string, perm fs.FileMode) {
	t.Helper()
	got, fi, err := w.sys.ReadFileNoFollow(context.Background(), w.path(rel))
	if err != nil || string(got) != data || fi.Mode() != perm {
		t.Errorf("%s = %q (%v), want %q mode %v", rel, got, err, data, perm)
	}
}

func modeOf(fi fs.FileInfo) any {
	if fi == nil {
		return nil
	}
	return fi.Mode()
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
