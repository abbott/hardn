package backup_test

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// The record of origin/main's Backup path (refactor plan slice 3, unit
// (d), next/s3-replace; docs/engine/parity-backup.md §8): the goldens
// pkg/characterization recorded of it, frozen under legacyDir before the
// code they ran was deleted, each at the path it had under
// pkg/characterization/testdata/golden, its bytes as at a2cf7bd (git show
// a2cf7bd:pkg/characterization/testdata/golden/<path> equals it). Nothing
// regenerates them: the tests that wrote them are retired, and this
// package's -update writes testdata/golden/parity.golden and
// parity-backup.md's region alone. backups/legacy_backupfile.golden is
// TestLegacyBackupFile's run of origin/main's two BackupFile functions,
// retired with them: kept because gallery-backup.md (§2.7, §7) cites it
// as run evidence and pkg/characterization's backups/engine_run.golden
// names it in its header.

// legacyRecord is each frozen file, by its path under legacyDir, with its
// SHA-256; read is the TestParity scenario that reads it, or why none
// does (parity-backup.md §2's "Not rows", and the BackupFile record).
var legacyRecord = []struct{ path, sum, read string }{
	{"backups/legacy_backupfile.golden", "c567eb924dc5cd895357ff9ca73e41b8e1fe473e0fa62c44266f38606c1cb7f7", "not a row: utils.BackupFile and the repository's, which nothing called"},
	{"menu/backup_disabled_3.golden", "9874a6337a833fb91754d9de8034f667d2e70456cdd9e5a9ebd3b14580df8466", "not a row: a pause, no call"},
	{"menu/backup_invalid.golden", "e6b9a6ab085374b01160ee07315626a39ff1250fd83514fe5c4f1592fd20e511", "not a row: a key that changes nothing"},
	{"menu/backup_open.golden", "3d7181350aa523d67f8d24be54f0f03be2638c587664bc74e06086cb1fcfc86d", "open"},
	{"menu/backup_open_dry_run.golden", "4f54a03a2cef172488736b2a37a1911f817bbbd7387fc31957607d3350311f21", "open-dry-run"},
	{"menu/backup_open_file.golden", "59a7e06b14b320b122f159465f03d7ec5415dc2b2e32884400de9e3b4e063d2e", "open-file"},
	{"menu/backup_open_missing.golden", "a519deac32f0ab50eb65d11a899d073aad6e36a17b86c7e722165aaf6633e66e", "open-missing"},
	{"menu/backup_path.golden", "4ca43472a24a1447b9653a845065840fd059deeacb66202006d9ef905d58bd7c", "path"},
	{"menu/backup_path_dry_run.golden", "9c0a9d4ee9f85d21b627714afe0679ca694166c00b6232729cd3d92fde5e6fdc", "path-dry-run"},
	{"menu/backup_path_empty.golden", "4cae9bf085a1528db0d8a0d7e204b152151117de47c5ee1dfc2ebada195997c1", "path-empty"},
	{"menu/backup_path_fails.golden", "6a2d0151c6da1264cb0027fba81671b564354d415295e8e28a7525afd13ed795", "path-under-file"},
	{"menu/backup_path_relative.golden", "7a9823f0ab9949be6bfb97c8611c05f616e3e4118d3cfa477ef3aa116ec07530", "path-relative"},
	{"menu/backup_path_tilde.golden", "0acd0699580ec9c06b7d47eb356fb79230ab08c10e3ee1994d63679d330f32e3", "path-tilde"},
	{"menu/backup_path_unsafe.golden", "83629e29b540dfc15bdc6e4f9dc0545924d84a8ddbd5653b6b5e7c94a542761f", "path-unsafe"},
	{"menu/backup_return.golden", "e824ddd32a46f680f6ee8b91bf3b9ace3cf157d45dddbcbce8cf043968dc4863", "not a row: 0"},
	{"menu/backup_toggle_off.golden", "ea75a3b408268482631bdea43dd400eb7362f9cdba0d9470ea039bfef7772080", "toggle-off"},
	{"menu/backup_toggle_on.golden", "3aec5cf09fc4afe9cd8eb5383c6516e4cdf388cc310869e2a90f3f04843ec25e", "toggle-on"},
	{"menu/backup_toggle_on_dry_run.golden", "4c28c06b774e32304bdd3288945be927eb8e0ddbc83d3f2f26b24e92a7501a47", "toggle-on-dry-run"},
	{"menu/backup_toggle_on_fails.golden", "aab1226d01eb791a7d0e6452bff9f89ec7dacb22711d2bccbaac9f253feb18b5", "toggle-on-file"},
	{"menu/backup_verify.golden", "3760926d1171c6a5aa15c4e333c30bd35ac834cd59b5509e19d284324d4a4c34", "verify"},
	{"menu/backup_verify_dry_run.golden", "a622ed12f6717134b04c55aac912b57cf37ac9778801ef8ab5f9b35708274d32", "verify-dry-run"},
	{"menu/backup_verify_fails.golden", "3e7a10a695f5bae78bd4119ef8e176da16cc2b14d76f33fa21607eda090853b7", "verify-file"},
}

// TestLegacyRecord holds the record: every file listed is under
// legacyDir with the bytes it was frozen with (its SHA-256), no other
// file is there, and each file a scenario names TestParity reads is one
// listed as read by it.
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
	reads := map[string]string{}
	for _, f := range legacyRecord {
		listed = append(listed, f.path)
		reads[f.path] = f.read
		data, err := os.ReadFile(filepath.Join(legacyDir, filepath.FromSlash(f.path)))
		if err != nil {
			t.Errorf("%s: %v", f.path, err)
			continue
		}
		if sum := sha256.Sum256(data); hex.EncodeToString(sum[:]) != f.sum {
			t.Errorf("%s is not as frozen at a2cf7bd: SHA-256 %x, want %s", f.path, sum, f.sum)
		}
	}
	slices.Sort(found)
	slices.Sort(listed)
	if !slices.Equal(found, listed) {
		t.Errorf("%s holds %q, want the record's %q", legacyDir, found, listed)
	}
	for _, sc := range backupScenarios {
		if got := reads[sc.Golden]; got != sc.Name {
			t.Errorf("scenario %s reads %s, which the record lists as %q", sc.Name, sc.Golden, got)
		}
	}
}
