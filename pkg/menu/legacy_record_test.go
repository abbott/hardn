package menu

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// The parity record of refactor plan slice 2b, unit (d), next/s2b-replace
// (docs/engine/dryrun-replace.md §4): the goldens of origin/main's
// behaviour that the unit's deletions and changes would otherwise lose or
// rewrite, frozen under testdata/legacy byte for byte as the revision
// named recorded them, each at the path it had under its package's
// testdata/golden. Each is the trunk's characterization of origin/main
// (which has no goldens); where the trunk had changed origin/main's
// screen by then, the file's header or its record below names the edit.
// Nothing writes there: this package's -update writes under
// testdata/golden alone (assertRenderGolden), and pkg/characterization's
// under its own testdata.
const legacyDir = "testdata/legacy"

// legacyRecord is each frozen file, by its path under legacyDir: the
// package whose test recorded it, the revision whose bytes it holds
// (git show <rev>:<package>/testdata/golden/<path> equals it), its
// SHA-256, and what it records.
var legacyRecord = []struct {
	path, from, rev, sum, records string
}{
	{"menu/dry_run_enable.golden", "pkg/characterization", "1467f0d", "d476cdc394c3747a982852a6331ae12eb1661aeaf2aa36b1bc1d98cdaa84360c",
		"the Dry-Run item (6), live, 1 to enable: three cleared screens and three pauses, and ./hardn.yml saved whole with dryRun: true"},
	{"menu/dry_run_disable.golden", "pkg/characterization", "1467f0d", "5c39450332f3f305f3340a7ed0a4fac887d6fe3ee42032967ca0c40ed91712e2",
		"the Dry-Run item under dry run, 1 to disable: its Enabled menu never printed, and ./hardn.yml saved whole with dryRun: false"},
	{"menu/dry_run_return.golden", "pkg/characterization", "1467f0d", "31bfd431627bfb22a46bf661029bd5a927af75856d5f519fdc42e9d04339504d",
		"the Dry-Run item, live, 0 to keep it: nothing changed, and ./hardn.yml saved all the same"},
	{"menu/dry_run_quit.golden", "pkg/characterization", "1467f0d", "cfb357955a2b5838980b4483b2b088c282f2c9fd3ad7499955f862001a6687fd",
		"the Dry-Run item, live, q at its menu: nothing saved, \"is now DISABLED\" and a second pause"},
	{"main_menu/alpine-update-available.golden", "pkg/menu", "cc04c17", "deeb697cc3e955e5b12bd99ac0282cb0721a8ed4d1db87a6d033e14574d8de27",
		"origin/main's main menu on pkg/style, Alpine, live: the status block, \"Dry-run Mode Disabled\", items 1 to 10 with 6 Dry-Run, [0-10 or q]; " +
			"two trunk edits: the exit row's \"q to quit\" (DEV-030, 56f0d93), in all four, and no update notice (V26's gate, 4f6bdd4)"},
	{"main_menu/debian-figma.golden", "pkg/menu", "cc04c17", "af9ae6106dae728aab095f06bc9aa0c6591e8064443f8ff5fbaa8e6e63cccf2c",
		"the same on Debian 13 in dry run: \"Dry-run Mode Enabled\""},
	{"main_menu/debian-figma-no-color.golden", "pkg/menu", "cc04c17", "6dd02fd7c9f57fa14cef58919a4cb3d64c2294c3a3006117e0acafb113fd0721",
		"the same with style.UseColors off"},
	{"main_menu/ubuntu-fresh.golden", "pkg/menu", "cc04c17", "837b5c63fb22d498662108d56bd1f713e2990cd2e9484885ef521d06b666be8a",
		"the same on a fresh Ubuntu 24.04 host, live"},
	{"run_all/option1-live-cancel.golden", "pkg/menu", "cc04c17", "f7c75cbf59e5f256c0652ed86b12c542f9775cb1779be62cc0d1b85f49be4b05",
		"Run All, live, option 1 declined at its typed yes: its menu with option 2, \"Enable dry-run mode and run\", [0-2 or q]; " +
			"two trunk edits: the typed yes (A4's gate, 5cb0fd6), and \"Press Enter\" where origin/main said \"Press any key\" (DEV-050)"},
}

// TestLegacyRecord holds the record: every file listed is under
// legacyDir with the bytes it was frozen with (its SHA-256), and no other
// file is there.
func TestLegacyRecord(t *testing.T) {
	var found []string
	err := filepath.WalkDir(legacyDir, func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			rel, _ := filepath.Rel(legacyDir, path)
			found = append(found, filepath.ToSlash(rel))
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	var listed []string
	for _, f := range legacyRecord {
		listed = append(listed, f.path)
		data, err := os.ReadFile(filepath.Join(legacyDir, filepath.FromSlash(f.path)))
		if err != nil {
			t.Errorf("%s: %v", f.path, err)
			continue
		}
		if sum := sha256.Sum256(data); hex.EncodeToString(sum[:]) != f.sum {
			t.Errorf("%s is not as frozen from %s at %s: SHA-256 %x, want %s", f.path, f.from, f.rev, sum, f.sum)
		}
	}
	slices.Sort(found)
	slices.Sort(listed)
	if !slices.Equal(found, listed) {
		t.Errorf("%s holds %q, want the record's %q", legacyDir, found, listed)
	}
}
