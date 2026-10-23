package cmd

import "testing"

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
		{"no -n", false, []string{FlagDisableRoot}, ""},
		{"-n alone", true, nil, ""},
		{"one", true, []string{FlagDisableRoot},
			"hardn: -n/--dry-run only previews changes, so it can't be combined with -d/--disable-root, which applies them. Nothing was changed."},
		{"two", true, []string{FlagCreateUser, CommandSetupSudoEnv},
			"hardn: -n/--dry-run only previews changes, so it can't be combined with -c/--create-user and setup-sudo-env, which apply them. Nothing was changed."},
		{"three", true, []string{FlagRunAll, FlagConfigureUFW, FlagDisableRoot},
			"hardn: -n/--dry-run only previews changes, so it can't be combined with -r/--run-all, -w/--configure-ufw and -d/--disable-root, which apply them. Nothing was changed."},
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
