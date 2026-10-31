package systemdetails_test

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// The record of origin/main's System Details and Logs (refactor plan slice 1, unit
// (d), next/s1-replace; docs/engine/parity-system-details.md §8): the goldens
// pkg/characterization recorded of them, frozen under legacyDir before the code they
// ran was deleted, each at the path it had under pkg/characterization/testdata/golden,
// its bytes as at 9d54134e. Nothing regenerates them: the tests that wrote them are
// retired, and this package's -update writes testdata/golden/parity.golden and
// parity-system-details.md's region alone.

// legacyRecord is each frozen file, by its path under legacyDir, with its SHA-256.
var legacyRecord = []struct{ path, sum string }{
	{"menu/logs_empty.golden", "4934c9c9ecc8596f8b00d4193a463b803e2e952cf67948c57f238b3746397c98"},
	{"menu/logs_escape.golden", "101e55a4584e19b15af7a7bd2e1588164cdf5a15eac6b0e2951f4c6d61bf0687"},
	{"menu/logs_large.golden", "85620c0ec435581d215d3183ff58ec86afeb94ed34f4cded58e6c91126aaf4ef"},
	{"menu/logs_missing.golden", "9ce587f60386197e72cbb8f7eb34fcc67652e55729332d7bdc2003d2dec015d4"},
	{"menu/logs_open.golden", "29c85933553bec55fd6a21e91d1df0366dc6afdb97d8bff7e1f3bc0aed839aeb"},
	{"menu/logs_other_path.golden", "f36092e883bfe905a90044192da9df645355a639e617911b9cdf6827b1334753"},
	{"menu/logs_other_path_stale.golden", "107ed13931233d579f506086cba485b1869fedda277c2e0909470b926e4fc154"},
	{"menu/logs_parser.golden", "be9e3a77417cf491412bd53e950e02785c6fa86f1b2137e9553ca81770a38d81"},
	{"menu/logs_print_flag.golden", "f11888ca38860fa34d0dc57301d9b080683c6d5b405dcd41cb316aa64af5f3f4"},
	{"menu/logs_print_flag_missing.golden", "6b671172c7725c04647fea3e4986a6bf3d7d7576c8a280ca150cef16991af952"},
	{"menu/logs_unreadable.golden", "6f96a34ada8b19d1cd1e6bcff3d316273a24fef6885bcb52594413a136c9062e"},
	{"menu/system_details_error.golden", "dcb2ebf934836fffd7cef318a7b9167c1e18cb63cd9d4b1bac31dc272b955c71"},
	{"menu/system_details_error_export.golden", "6ae2299224e156b15125f0b63b542bc8d60ba340e5985702f77e04fe649405d2"},
	{"menu/system_details_export_default.golden", "bde4b29730a341534dbf577b40f22147e43e5e655b6f903162c5e1265ab6bb9f"},
	{"menu/system_details_export_dry_run.golden", "a49d934cb9a74ec3d0455f909950616c2ec58159ab8fe5366cf00608db45a325"},
	{"menu/system_details_export_fails.golden", "1320f30a93ccf988816f50be319f6467ca1f5ee8afa2373b508eada70fab93a2"},
	{"menu/system_details_export_named.golden", "2c64fd0ad94df0fdfaf7331b2cd2a3586ebbcf2a0630b470ff9e6ea2d92f718c"},
	{"menu/system_details_export_overwrite.golden", "3b557ad0efa624a340a705e6807a2b66bc01e977f160e69fda6a14a6fbecd51d"},
	{"menu/system_details_export_zfs.golden", "585fedf2528fb4e64ff8c326efa89b816b8b3e5d9afff1e88d19a07e95643561"},
	{"menu/system_details_flags.golden", "cbe528722d1227fc8ad9213901c693b27396cf3b285289d776cfe063a4d3dfe5"},
	{"menu/system_details_invalid.golden", "41f13f11484780a1092e568cd79c9ffb5d012ff0ba56d5c5a33983f74413457b"},
	{"menu/system_details_login_hostname.golden", "a069370d210136cb18386f439ecce040fcd7712e79eb74f7fa268b9ab1deb721"},
	{"menu/system_details_never_logged_in.golden", "74d5039b4b829ab1dcbf10996dddd724712d1218ddd838c1650b8cc9e1c68c67"},
	{"menu/system_details_no_lastlog.golden", "43ba26481b34f1c2640b48eb1feb8541bb25fb5df37aff8f2595112731c3ab04"},
	{"menu/system_details_no_lscpu.golden", "f637f1032dc6b6c27a6534474ac1ffb00af75f6801a9b17a0bfe3755efc5810e"},
	{"menu/system_details_open.golden", "875df71e89285d945c641fa4d624f05120b5d2a5fccf3a5fdcd9bd7b95212488"},
	{"menu/system_details_refresh.golden", "4412bd635fa56f36f24bf314ec58622ef5b3dca4a006ce9e8e6c76b40da3f893"},
	{"menu/system_details_render.golden", "b5e6da038254f80227d39ae257f2e8010d364d5831b3471989f468c54d4425a4"},
	{"menu/system_details_render_zfs.golden", "70c270a8c2a50eba21d2519d66490f4ce3dd7d1b824904696c80711a9810974a"},
	{"menu/system_details_return.golden", "1c6921d432b7012204b2a36c0026e52e1beb30c09e04a4ba9f4a8981d525e15e"},
	{"menu/system_details_zfs_all_healthy.golden", "4fcb4d42354924fa2e003e8e63bdd655113cb1d3308ec3ba72249e14b0f9b99f"},
	{"menu/system_details_zfs_dataset.golden", "644578a9d53e03fcb978ad0ce99fe7a0a6fbdb0c64185a598ae7a95802f5de85"},
	{"menu/system_details_zfs_other_dataset.golden", "e8c2c9b620af85da9a613a229a11682b9334e8d7d367bb0007bef1359e15f469"},
}

// TestLegacyRecord holds the record: every file listed is under legacyDir with the
// bytes it was frozen with, and no other file is there.
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
			t.Errorf("%s is not as frozen at 9d54134e: SHA-256 %x, want %s", f.path, sum, f.sum)
		}
	}
	slices.Sort(found)
	slices.Sort(listed)
	if !slices.Equal(found, listed) {
		t.Errorf("%s holds %q, want the record's %q", legacyDir, found, listed)
	}
}
