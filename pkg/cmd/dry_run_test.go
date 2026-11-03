package cmd

import (
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/output"
)

// TestRefuseDryRunOperations checks the refusal V1's gate gives: none
// without -n or without an operation, else one naming every operation.
func TestRefuseDryRunOperations(t *testing.T) {
	if dryRunFlagApplies {
		t.Fatal("dryRunFlagApplies is set: hardn -n would apply operation flags (V1)")
	}
	for _, tc := range []struct {
		name   string
		dryRun bool
		ops    []string
		want   string // "" for no refusal
	}{
		{"no -n", false, []string{FlagRunAll}, ""},
		{"-n alone", true, nil, ""},
		{"one", true, []string{FlagRunAll},
			"hardn: -n/--dry-run only previews changes, so it can't be combined with -r/--run-all, which applies them. Nothing was changed."},
		{"two", true, []string{FlagRunAll, FlagInstallAll},
			"hardn: -n/--dry-run only previews changes, so it can't be combined with -r/--run-all and -a/--install-all, which apply them. Nothing was changed."},
		{"three", true, []string{FlagRunAll, FlagInstallLinux, FlagInstallAll},
			"hardn: -n/--dry-run only previews changes, so it can't be combined with -r/--run-all, -l/--install-linux and -a/--install-all, which apply them. Nothing was changed."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := RefuseDryRunOperations(tc.dryRun, tc.ops)
			switch {
			case tc.want == "" && err != nil:
				t.Errorf("refused: %v", err)
			case tc.want != "" && (err == nil || err.Error() != tc.want):
				t.Errorf("got %v\nwant %s", err, tc.want)
			}
		})
	}
}

// TestRefuseConfigDryRunOperations checks decision 9's refusal: none
// unless hardn.yml says dryRun: true and an operation is named, else one
// naming the file, every operation and --dry-run=false.
func TestRefuseConfigDryRunOperations(t *testing.T) {
	const path = "/etc/hardn/hardn.yml"
	for _, tc := range []struct {
		name   string
		dryRun bool
		ops    []string
		want   string // "" for no refusal
	}{
		{"file false", false, []string{FlagRunAll}, ""},
		{"file true alone", true, nil, ""},
		{"one", true, []string{FlagRunAll},
			"hardn: /etc/hardn/hardn.yml sets plan only (dryRun: true), so -r/--run-all can't run. Nothing was changed. To apply, add --dry-run=false."},
		{"two", true, []string{FlagRunAll, FlagInstallAll},
			"hardn: /etc/hardn/hardn.yml sets plan only (dryRun: true), so -r/--run-all and -a/--install-all can't run. Nothing was changed. To apply, add --dry-run=false."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := RefuseConfigDryRunOperations(tc.dryRun, path, tc.ops)
			switch {
			case tc.want == "" && err != nil:
				t.Errorf("refused: %v", err)
			case tc.want != "" && (err == nil || err.Error() != tc.want):
				t.Errorf("got %v\nwant %s", err, tc.want)
			}
		})
	}
}

// TestRefusalCodes: both refusals are usage errors, code USAGE (exit
// status 2), their words unchanged by it.
func TestRefusalCodes(t *testing.T) {
	for _, err := range []error{
		RefuseDryRunOperations(true, []string{FlagRunAll}),
		RefuseConfigDryRunOperations(true, "/etc/hardn/hardn.yml", []string{FlagRunAll}),
	} {
		if code := output.CodeOf(err); code != "USAGE" || output.ExitCode(output.Run{Err: err}) != ExitUsage || strings.Contains(err.Error(), "usage") {
			t.Errorf("%v: code %s", err, code)
		}
	}
}
