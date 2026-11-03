package firewall_test

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

// The record of origin/main's Firewall path (refactor plan slice 4, unit
// (d), next/s4-replace; docs/engine/parity-firewall.md §8): the goldens
// pkg/characterization recorded of it, frozen under legacyDir before the
// code they ran was deleted, each at the path it had under
// pkg/characterization/testdata/golden, its bytes as at 93cea813 (git show
// 93cea813:pkg/characterization/testdata/golden/<path> equals it). Nothing
// regenerates them: the tests that wrote them are retired (but
// TestOperationGoldens, which still writes its own
// ops/configure_secure_firewall, copied here), and this package's -update
// writes testdata/golden/parity.golden and parity-firewall.md's region
// alone.

// legacyRecord is each frozen file, by its path under legacyDir, with its
// SHA-256; read is the TestParity scenarios that read it, comma-separated,
// or "not a row" for one TestParityGoldens names as not a row, with why
// (notRows).
var legacyRecord = []struct{ path, sum, read string }{
	{"menu/firewall_configure_dry_run.golden", "1d756fbbb7dc24badb97b295478c859dbbd4df72fb71f0205d21ac20a8acb264", "dry-configure"},
	{"menu/firewall_invalid_key.golden", "37c7ee95fb1cc39d82fc31cce3ae300212b8bdc13f32df14e9d3e02987df3a89", "not a row"},
	{"menu/firewall_item1_active_no.golden", "277db7bd2e2502352e16ac2e459de1bd8e0fcb387f70897576bb73fd1d46a044", "disable-no"},
	{"menu/firewall_item1_active_yes_dry_run.golden", "215f850c3c73f67eb72be22dbdb47d264135f620038994e1ab1fd10681f0099e", "dry-disable"},
	{"menu/firewall_item1_active_yes_stub.golden", "e0feb4802c60ecd968a48060b3d76bd92489d71b91ef28dc294fb92b99a24d7c", "disable"},
	{"menu/firewall_item1_inactive_dry_run.golden", "6de7ff5a8e424cf4bc22ef1d861cdf0e3c16e4a5450fac8a3e04e9a8881d4a43", "dry-enable"},
	{"menu/firewall_item1_inactive_enable_runs_configure.golden", "f27ea83d96620b68c468c71f9b42e3b6cd6b302ff086d9639c149d5dac33b151", "enable"},
	{"menu/firewall_item1_not_installed_dry_run.golden", "f480ec9e33ffd2b7daf37dcaaa1551543b1a7fa6896e647df9111653412e82b6", "dry-install"},
	{"menu/firewall_item1_not_installed_stub.golden", "59da61f69f1c9f54d758e1ce5e56553a44e536853d8dc9d7aa8d040451c9c20e", "install, install-stock"},
	{"menu/firewall_item2_active_rules_are_reset.golden", "11a8e4fe20adb3e113e1cc027c00e9ce96033be8421cc16971244264edf7d6e3", "active-rules"},
	{"menu/firewall_item2_allows_configured_port_not_sshds.golden", "de40afa20f911bfe4f387f16b1e6b44049d131269b2b1edb45bcea8fc4fd1062", "configure"},
	{"menu/firewall_item2_fails_part_way.golden", "9d19f636170d637c5ac3f011c33b971dacb0c9eb78d878ca270b202b32661112", "fails"},
	{"menu/firewall_item2_not_installed_hidden_still_runs.golden", "e1913ffb3818de324795283e795c03acd06f5dde5b01c04057d485cb5e746f7d", "no-ufw-item2"},
	{"menu/firewall_item2_profiles.golden", "12b7d2d5f271273f23f417f12c28e9f945144d9dc879473004a775f291f2b422", "profiles"},
	{"menu/firewall_open_active_no_rule.golden", "a1e8a7283600fe4b953c3cebbcea4fe3e736a00bf24cf60445ad34d743e7191a", "not a row"},
	{"menu/firewall_open_active_rule_listed_by_its_comment.golden", "029a1f7835522f1d46763b744892fd3f5988b959dcd01230e9c6683b44707d67", "not a row"},
	{"menu/firewall_open_active_rules_listed_if_status_is_verbose.golden", "63d338fc4cd91981bb6e41049d6d20c5653d794b4eac17a9de44c70f00455ae6", "not a row"},
	{"menu/firewall_open_active_rules_never_listed.golden", "4b28785cc2267d01709f7f7d76a0ce0b2359de72772c3d1cd4bb92a2c86598e9", "not a row"},
	{"menu/firewall_open_inactive.golden", "b27e902ac384d9daa76023d7be400fae46045d0989482ea67489812f8a4cfd66", "not a row"},
	{"menu/firewall_open_not_installed.golden", "5223a255da196499d240ea8110c28d0ef175749286e824484eef7467485107fb", "not a row"},
	{"menu/firewall_open_port_2230_while_sshd_says_22.golden", "a42d2226feb35ced4e7cce4d6e6a1c0600dcc2e64f7df9beb9a8301fc9f9d156", "not a row"},
	{"menu/firewall_open_port_22_warns.golden", "c97bdee11d5a6e3e18aadb078b0a6687580d61c1e9b7f271c4cf3297c27259d8", "not a row"},
	{"menu/firewall_open_profiles.golden", "6bcefa55e272b237aeb2364c94fcd99f46d3d49a4196820a1547ab62da9b8311", "not a row"},
	{"menu/firewall_open_status_fails_reads_inactive.golden", "35fc7704d7551849d91a296992a6f46802061a4b4d6fe01fb1c823c2ad4b24e3", "not a row"},
	{"menu/firewall_profiles_add.golden", "72370757b823a0564c3a9fbe75f096bf2b21c073b108efe70960c52c16f040dc", "add"},
	{"menu/firewall_profiles_add_bad_port_after_all_answers.golden", "6ce6489d01938bfcf49d4b859d126329eeaa7750eccaee853c76c6a236a16bc0", "add-bad-port"},
	{"menu/firewall_profiles_add_duplicate_name.golden", "3bcde6e2d7d702824845053908bc0a3c37d9ef5d777742538cdeca16b3bb74f1", "add-case"},
	{"menu/firewall_profiles_add_empty_name.golden", "e7b8759ebaf6674275daf3f5107bbb7a1e5d97700fbc7d91d12be5d1d919ac8e", "add-no-name"},
	{"menu/firewall_profiles_add_empty_ports.golden", "1c79268d09ce556d3410429969c62c9b8aa968f41eb60abaf19ca1d1468c1fbb", "add-no-ports"},
	{"menu/firewall_profiles_add_plan_only_refused.golden", "4d2f266b548b74aed2462d55f56e2689cf3a3f74b9145396d2bbc265edfbe1ae", "dry-add"},
	{"menu/firewall_profiles_add_port_check_is_a_slash.golden", "3069d9023a107599ad6ad1d6d717237b4de34f79c7c128026a4cf124d93fda11", "add-slash"},
	{"menu/firewall_profiles_apply_dry_run.golden", "0d75212f28b639c2e6ea1247974a9d1fbad303351210e241ffb0912e2e8b62e1", "dry-apply"},
	{"menu/firewall_profiles_apply_stub.golden", "5a986d5561a285afd7f5ed99a31dd322cdd0f12cb30a09f851705ece159ca45d", "apply"},
	{"menu/firewall_profiles_hidden_items_none_to_act_on.golden", "12a988a1e4098d7a0b079a180bf1d80a3992098412a62a7b40097dad9fb80b8d", "not a row"},
	{"menu/firewall_profiles_invalid.golden", "6551317567ce9c0d8b1e5828483e0854104b91af1ec2874f67175b5813e3c8bb", "not a row"},
	{"menu/firewall_profiles_none.golden", "ab79822355157395c54024ef1efb45e9bc202075e33008f40a9f3ebca228940b", "not a row"},
	{"menu/firewall_profiles_not_installed_hidden_still_runs.golden", "d27095206d087e1649ba9a9002f3ad6980e8ddddb5a92b01bab75f10c99ffd51", "not a row"},
	{"menu/firewall_profiles_quit_q.golden", "79b000a83cee777c737187ecd40bd08bc28a98a8ea8178994b329f1b53cf86fe", "not a row"},
	{"menu/firewall_profiles_remove_bad_number.golden", "472cd70ae003f729be2615f810456023ec372b76907e71ff248f0e82a94d4f48", "not a row"},
	{"menu/firewall_profiles_remove_no.golden", "64522fa8dea66605feb8a2e610e3518b0a6be1428a32a563240d96d16d31d13b", "remove-no"},
	{"menu/firewall_profiles_remove_plan_only_refused.golden", "52acabfb7c705ef65f24d703345df5aeeffad4be8b893cd2ab63b9283dc4f9d0", "dry-remove"},
	{"menu/firewall_profiles_remove_yes.golden", "037746c40a7874404c6cdc89fb0c8c6f520a8e475cb583c4db804c281d8b9310", "remove"},
	{"menu/firewall_profiles_two.golden", "ffa174a50a62ac19ef318ce8c7a664c7f50333e250beddf61f777d824a3c00b6", "not a row"},
	{"menu/firewall_proxmox_active_disable_stub.golden", "dfe894517dfb32f4618683bd4408dd815b365571d027f9c30ea1c721ea3d2a69", "pve-active"},
	{"menu/firewall_proxmox_item1_refused.golden", "80df2448ab163f3e404d55c6eaa535f02afd6eba35c9823d86d8ced41b195661", "pve-item1"},
	{"menu/firewall_proxmox_item2_refused.golden", "5bbb5fc9664c57371bf7bd0a2fab7d910d6b77c1dc6bbeb19ea52b503b661b17", "pve-item2"},
	{"menu/firewall_proxmox_open_says_nothing.golden", "498ac3368d5700c58c99c4803b2fd50b95dfe51fd1aa544e67b0d57534b5edd0", "pve-open"},
	{"menu/firewall_quit_q.golden", "e86c5eb9fb371410cf528d54bbc2dda6f337a4fa365ff2423ec562f05fcc5d05", "not a row"},
	{"menu/firewall_return_0.golden", "acf6c205e6ce2d8290281fe983a3db6be3a371b4167445d5f847fa9ec38dd037", "not a row"},
	{"ops/configure_secure_firewall/alpine.golden", "966a0d881bbedc6f7d26d9dad2c271627f950bc7ecd068271a4ed2af18dba319", "w, w-stock"},
	{"ops/configure_secure_firewall/debian.golden", "14b1dc2e03e1de76f7686c1f74cab1686108b14977d10d8e59cb37a988358896", "w"},
	{"ops/configure_secure_firewall/proxmox.golden", "feaac3bbc3ed3c65dc80ec0434f346d83ae0f059b997772dab70f8e294667d83", "w"},
	{"ops/configure_secure_firewall/ubuntu.golden", "37deba9b2eff4ab8e1aaac3ee743f61d1daff306cb0f43b767ee48247c02da4e", "w"},
	{"ops/configure_secure_firewall_profiles/alpine.golden", "5616c4fae98cbacfa2e75db6d1fe0793407838caeb74a97bd950ab8154b1ac9a", "item2-profile"},
	{"ops/configure_secure_firewall_profiles/debian.golden", "13c1b716ec3a41525228a0466b521322e509eded8ea7cf4f7ed49538e48a7635", "item2-profile"},
	{"ops/configure_secure_firewall_profiles/proxmox.golden", "662e956c2a728d3b350c8a2ad11552d1ee73b203eebbdb5cf9101aef64dff4aa", "item2-profile"},
	{"ops/configure_secure_firewall_profiles/ubuntu.golden", "006654dd9d5108df9ec9014a177b625698fa4e98023c0d3e02e9b24f4d5cb356", "item2-profile"},
}

// TestLegacyRecord holds the record: every file listed is under
// legacyDir with the bytes it was frozen with (its SHA-256), no other
// file is there, the scenarios reading each file, on the distributions
// they run on, are the ones listed, and each listed as not a row is a
// not-row.
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
			t.Errorf("%s is not as frozen at 93cea813: SHA-256 %x, want %s", f.path, sum, f.sum)
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
	readBy := map[string][]string{}
	for _, sc := range scenarios {
		for _, d := range distros {
			if len(sc.Only) > 0 && !slices.Contains(sc.Only, d.Name) {
				continue
			}
			g := strings.ReplaceAll(sc.Golden, "{d}", d.Name)
			if !slices.Contains(readBy[g], sc.Name) {
				readBy[g] = append(readBy[g], sc.Name)
			}
		}
	}
	for g, names := range readBy {
		if got := strings.Join(names, ", "); reads[g] != got {
			t.Errorf("%s is read by %q, which the record lists as %q", g, got, reads[g])
		}
	}
	for g, r := range reads {
		if r != "not a row" && readBy[g] == nil {
			t.Errorf("%s is listed as read by %q, but no scenario reads it", g, r)
		}
	}
}
