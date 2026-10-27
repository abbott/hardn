package backup

import (
	"context"
	"errors"
	"io/fs"
	"slices"
	"syscall"
	"testing"

	"github.com/abbott/hardn/pkg/host/hostfake"
)

// Runs lists the runs under a backupPath newest first, each with its
// manifest, and changes nothing: an entry that is no run directory, and a
// run that recorded no backup, are not runs; a manifest Load refuses is
// left out and its error returned with the runs read; a path not there
// has none; a path another user can change is refused, naming it.
func TestRuns(t *testing.T) {
	ctx := context.Background()
	eachWorld(t, func(t *testing.T, w world) {
		w.file("etc/a", "a\n", 0o644)
		w.file("etc/b", "b\n", 0o644)
		dir := w.path("var/backups/hardn")
		ids := []string{"20261017T120000Z-0a1b2c3d", "20261019T090000Z-1a2b3c4d", "20261018T150000Z-2a3b4c5d"}
		for i, id := range ids {
			run, err := Open(w.sys, Settings{Path: dir, Enabled: i != 1}, id)
			must(t, err)
			_, err = run.Backup(ctx, w.path("etc/a"), RollbackRequired)
			must(t, err)
			if i == 2 {
				_, err = run.Backup(ctx, w.path("etc/b"), RollbackRequired)
				must(t, err)
			}
		}
		w.dir("var/backups/hardn/20261020T080000Z-3a4b5c6d", 0o700) // no manifest: no backup recorded
		w.file("var/backups/hardn/notes", "", 0o600)                // not a directory
		w.dir("var/backups/hardn/.hidden", 0o700)                   // not a run id
		calls := 0
		if f, ok := w.sys.(*hostfake.System); ok {
			calls = len(f.Calls())
		}
		runs, err := Runs(ctx, w.sys, dir)
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, m := range runs {
			got = append(got, m.RunID)
		}
		if want := []string{ids[1], ids[2], ids[0]}; !slices.Equal(got, want) {
			t.Fatalf("runs %q, want %q", got, want)
		}
		if len(runs[1].Entries) != 2 || runs[0].BackupsEnabled || !runs[0].Entries[0].TakenAnyway || runs[1].Dir != dir+"/"+ids[2] {
			t.Errorf("manifests %+v", runs)
		}
		if f, ok := w.sys.(*hostfake.System); ok {
			for _, c := range f.Calls()[calls:] {
				switch c.Op {
				case "write-atomic", "write-in-place", "mkdir", "remove", "removeall", "rename", "chmod", "chown":
					t.Errorf("Runs changed something: %s", c)
				}
			}
		}

		// A manifest Load refuses: left out, its error returned.
		w.file("var/backups/hardn/"+ids[0]+"/"+ManifestName, "{}\n", 0o600)
		runs, err = Runs(ctx, w.sys, dir)
		if !errors.Is(err, ErrManifest) || len(runs) != 2 {
			t.Errorf("a bad manifest: %d runs, %v", len(runs), err)
		}

		if runs, err := Runs(ctx, w.sys, w.path("srv/none")); err != nil || runs != nil {
			t.Errorf("a path not there: %v, %v", runs, err)
		}
		if _, err := Runs(ctx, w.sys, "backups"); !errors.Is(err, fs.ErrInvalid) {
			t.Errorf("a relative path: %v", err)
		}
	})
	w := realWorld(t)
	w.dir("srv/shared/backups", 0o700)
	w.dir("srv/shared", 0o777)
	_, err := Runs(ctx, w.sys, w.path("srv/shared/backups"))
	var de *DirError
	if !errors.As(err, &de) || de.Dir != w.canonical("srv/shared") || !errors.Is(err, ErrUnsafeDir) {
		t.Errorf("a directory others can change: %v", err)
	}
}

// Check's refusal of a directory names it (DirError), its text as it was:
// another user can change it, or it is not a directory.
func TestDirError(t *testing.T) {
	ctx := context.Background()
	w := realWorld(t)
	w.dir("srv/shared", 0o777)
	w.file("etc/os-release", "ID=debian\n", 0o644)
	for _, c := range []struct {
		path, dir string
		want      error
	}{
		{"srv/shared/backups", "srv/shared", ErrUnsafeDir},
		{"etc/os-release", "etc/os-release", syscall.ENOTDIR},
		{"etc/os-release/backups", "etc/os-release", syscall.ENOTDIR},
	} {
		err := Check(ctx, w.sys, Settings{Path: w.path(c.path), Enabled: true}, runID, nil)
		var de *DirError
		if !errors.As(err, &de) || de.Dir != w.canonical(c.dir) || !errors.Is(err, c.want) {
			t.Errorf("%s: %v, want a DirError for %s, %v", c.path, err, c.dir, c.want)
			continue
		}
		if want := "backup: " + w.canonical(c.dir) + ": " + c.want.Error(); err.Error() != want {
			t.Errorf("%s: %q, want %q", c.path, err, want)
		}
	}
}

// A backup that cannot be written, at its run directory, its copy or its
// manifest, is a WriteError naming the file and the backupPath: ErrWrite,
// and the write's own error, in the words it had.
func TestWriteError(t *testing.T) {
	ctx := context.Background()
	for _, fail := range []string{"mkdir:20261017T120000Z-0a1b2c3d", "write:0001-a", "write:" + ManifestName} {
		w := realWorld(t)
		w.file("etc/a", "a\n", 0o644)
		sys := &failing{System: w.sys, fail: map[string]error{fail: &fs.PathError{Op: "mkdir", Path: "x", Err: syscall.EROFS}}}
		run, _ := Open(sys, Settings{Path: w.path("backups"), Enabled: true}, runID)
		_, err := run.Backup(ctx, w.path("etc/a"), RollbackRequired)
		var we *WriteError
		if !errors.As(err, &we) || !errors.Is(err, ErrWrite) || !errors.Is(err, syscall.EROFS) || we.Path != w.path("etc/a") || we.BackupPath != w.path("backups") {
			t.Errorf("%s: %v (%+v)", fail, err, we)
			continue
		}
		if want := "backup: mkdir x: read-only file system"; err.Error() != want {
			t.Errorf("%s: %q, want %q", fail, err, want)
		}
		if len(run.Manifest().Entries) != 0 {
			t.Errorf("%s: an entry recorded", fail)
		}
	}
}
