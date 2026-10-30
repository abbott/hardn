package environment_test

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The record of origin/main's Environment path (refactor plan slice 9,
// unit (d), next/s9-replace; docs/engine/parity-environment.md §8): the
// goldens pkg/characterization recorded of it, frozen under legacyDir
// before the code they ran was deleted, each at the path it had under
// pkg/characterization/testdata/golden, its bytes as at e28c5a1 (git show
// e28c5a1:pkg/characterization/testdata/golden/<path> equals it). Nothing
// regenerates them: the tests that wrote them are retired (but
// TestOperationGoldens, which still writes its own ops/configure_sudo,
// copied here), and this package's -update writes
// testdata/golden/parity.golden and parity-environment.md's region alone.

// legacyRecord is each frozen file, by its path under legacyDir, with its
// SHA-256; read is the TestParity scenario that reads it, or "not a row"
// for one TestParityGoldens names as not a row, with why (notRows).
var legacyRecord = []struct{ path, sum, read string }{
	{"environment/keep_already.golden", "33963dfa416cdfae78346cff147b55515004c103dcad54391728f9dfbb00c95b", "already"},
	{"environment/keep_commented.golden", "3c203e3e58abb4b348db5559f04c0c333948d7d2c05436c7b41aecf5db6e5a5d", "commented"},
	{"environment/keep_dotted.golden", "d4e620d847a06500662fbba7116c47ef343eef73f3c41abe78a2e1f60466728a", "dotted"},
	{"environment/keep_new.golden", "9bda17b6831289cc7c02d1b04b0732b27c0d9038327acd28e5ca8feee0026b34", "new"},
	{"environment/keep_no_sudoers_d.golden", "32222a8f73fbb9baf1ffdbc05e515a89e04cd55bcd069cda0124b4c2db9ae007", "no-sudoers-d"},
	{"environment/keep_no_user.golden", "4d977f4b5ba4cd7975cc73ef2b8593640b82d483773d1552e4000a5a8c29256e", "no-user"},
	{"environment/keep_root.golden", "9c2f01e7c77160c48b34147df2a021b77bb3f3f23fe5e31157d3380e226e2d22", "root"},
	{"environment/keep_tmp_dir.golden", "5105d1c78f1c2e73df8deda7ee5b34e0aef152efca8dd59828d92696df9cf40b", "tmp-dir"},
	{"environment/keep_tmp_symlink.golden", "1a0874ebf6a9d3d37e7810aab7bb1c52e43e1001c4823738b083d7d9b57a73b9", "tmp-symlink"},
	{"environment/keep_visudo_fails.golden", "e88b01eeaa274018a109460651caf311eb32983fabcb579789f37a64860fcfaf", "visudo-fails"},
	{"environment/notice_doas.golden", "1fc5a587bda3881c82840ca25c59a67a1e6d89cb3761743f94715a70d03523db", "not a row"},
	{"environment/notice_kept.golden", "bed649e95639d894b579553f6b7f914f5e4542272948bf1ffc0e242b357623fc", "not a row"},
	{"environment/notice_lost.golden", "401875261cbb517fa60bf37598f03af7369d878911c85267f356fce42ce167d2", "not a row"},
	{"environment/notice_lost_unset.golden", "3a17592011174ca22c680d341948202d2c79c15233275a82728a0677f628d5c4", "not a row"},
	{"environment/notice_no_sudo_user.golden", "62fe5fdbfa52c162549998d7199d3d336e6a41cfbf3e1d3cb95c288f219c17cc", "not a row"},
	{"environment/notice_not_sudo.golden", "fd3eb45e6f9b950f3391ba4ae0ed691a5773c4f6790549c2e3a1c03ffa7e58cf", "not a row"},
	{"environment/notice_profile_prints.golden", "accd2c86a05a5da5dc2d339e01ca8f85640a6824c3b897ff91c7b075c03ee290", "not a row"},
	{"environment/notice_setup_sudo_env.golden", "b51787dd36c2e80b7a2d16a0033cc4121f5bdafae1719a3736f68dc728400706", "not a row"},
	{"environment/notice_su_fails.golden", "e669bfac4473feb6dfacd0c20847c3f38e02ccec3ebbb7172116317bd6763986", "not a row"},
	{"menu/environment_guide.golden", "bb283320d8c7af6bb46ff46b86ff7e61fb310926ad645bb000fddc46a4e558f4", "not a row"},
	{"menu/environment_invalid.golden", "e1b94359004c48b467af1a228a4ba083d3d953dc510b2522ae52003d46802f9e", "not a row"},
	{"menu/environment_keep_dry_run.golden", "dd0a29fe3fd22ffc11ea940227812ea267669ebd537b3819a84bf673c7d6473b", "not a row"},
	{"menu/environment_keep_not_root.golden", "8d0951e643d40373724baa1d347e9b9c6803786950b9953639513918d08418fe", "not a row"},
	{"menu/environment_open.golden", "24a9601c3b7c8b22abbbf9c74e196af8a9b328e54a39983902eaa8cdfeab1964", "not a row"},
	{"menu/environment_open_commented.golden", "8e52888589338caff7f89dbb1ef006016e42089a6557e09846e0f8f165ca5726", "not a row"},
	{"menu/environment_open_elsewhere.golden", "66c42f49943dcb3a01222da8d51664ab40f26b26da1ffc4b227b686370029638", "not a row"},
	{"menu/environment_open_no_user.golden", "e4ba6b8aae7438dc344390c120e17dbe3ca72cc926d0da3f44ce2d266aa75913", "not a row"},
	{"menu/environment_open_root.golden", "1ea21831982d0d4388aa929040501e2e197f27bee983abc81f4ac2808a6b0d0d", "not a row"},
	{"menu/environment_open_unset.golden", "d03adb8f3499b22699e2d080dd3d746b179786fc81de4a58041116cff05dfa78", "not a row"},
	{"menu/environment_open_user.golden", "ed317db469f28f78d6785d28314e11a8ab732f31544d6389a447dc944986f7e0", "not a row"},
	{"menu/environment_return.golden", "7a9da2efa85bf646d5bda1b5e55be3224c33df5a53f6bbd6edc472ead1c47b5a", "not a row"},
	{"ops/configure_sudo/alpine.golden", "45524b7f7264ce86e4727abb96e29037a96e9341d5d9a8a8d6b241f8b937615d", "not a row"},
	{"ops/configure_sudo/debian.golden", "39aa06051a2361aeda2ee0f0302481754859e3422bb3343430c3709b2e4d3e07", "not a row"},
	{"ops/configure_sudo/proxmox.golden", "be97cf922ba9efde277d553e2b12bb6be829d51bf470abe91e3fa3c49726a014", "not a row"},
	{"ops/configure_sudo/ubuntu.golden", "fafb21aaede25ca48fbcf72faf3ed7fde2e7389bc694ddb651b0878fe8b92691", "not a row"},
	{"ops/setup_sudo_preservation/alpine.golden", "0549b7c99f372e4aec3c37c40a56d6837a0e14f20979958cb58aa4deab7e0139", "grant"},
	{"ops/setup_sudo_preservation/debian.golden", "9b5562ecc8a7b886c247a432101090c9af171296fbd920f9b1e21336077081f7", "grant"},
	{"ops/setup_sudo_preservation/proxmox.golden", "7a5c418dedc8c01cc631aae1f0ddab1d3317142414d3f478757f43aa837c617a", "grant"},
	{"ops/setup_sudo_preservation/ubuntu.golden", "e43fe9fc0b01dfc17ace47b6f0e51a14c00aa5c589d8144bca6c8d0980ad80cf", "grant"},
}

// TestLegacyRecord holds the record: every file listed is under
// legacyDir with the bytes it was frozen with (its SHA-256), no other
// file is there, each file a scenario reads, on every distribution, is
// one listed as read by it, and each listed as not a row is a not-row.
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
			t.Errorf("%s is not as frozen at e28c5a1: SHA-256 %x, want %s", f.path, sum, f.sum)
		}
		if f.read == "not a row" && notRows[f.path] == "" {
			t.Errorf("%s is listed as not a row, but notRows does not name it", f.path)
		}
	}
	slices.Sort(found)
	slices.Sort(listed)
	if !slices.Equal(found, listed) {
		t.Errorf("%s holds %q, want the record's %q", legacyDir, found, listed)
	}
	for _, sc := range envScenarios {
		for _, d := range envDistros {
			g := strings.ReplaceAll(sc.Golden, "{d}", d.Name)
			if got := reads[g]; got != sc.Name {
				t.Errorf("scenario %s reads %s, which the record lists as %q", sc.Name, g, got)
			}
		}
	}
}
