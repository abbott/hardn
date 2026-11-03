package cmd

import (
	"testing"

	"github.com/abbott/hardn/pkg/output"
)

// TestRefuseCreateDisableRoot: -c with -d is refused, a usage error, where
// -c's user gets no key; -c alone, -d alone, and -c with keys are not.
func TestRefuseCreateDisableRoot(t *testing.T) {
	for _, tc := range []struct {
		name                string
		create, disableRoot bool
		keys                []string
		want                string // "" for no refusal
	}{
		{"-c alone", true, false, nil, ""},
		{"-d alone", false, true, nil, ""},
		{"-c -d, a key", true, true, []string{"ssh-ed25519 AAAA george@web-01"}, ""},
		{"-c -d, two keys", true, true, []string{"ssh-ed25519 AAAA george@web-01", "ssh-ed25519 AAAB carol@web-01"}, ""},
		{"-c -d, no key", true, true, []string{},
			"hardn -c -d would leave no SSH login: carol has no key, and -d turns off root's. Give carol a key first."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := RefuseCreateDisableRoot(tc.create, tc.disableRoot, "carol", tc.keys)
			switch {
			case tc.want == "" && err != nil:
				t.Errorf("refused: %v", err)
			case tc.want != "" && (err == nil || err.Error() != tc.want):
				t.Errorf("got %v\nwant %s", err, tc.want)
			case tc.want != "" && (output.CodeOf(err) != "USAGE" || output.ExitCode(output.Run{Err: err}) != ExitUsage):
				t.Errorf("%v: code %s, want USAGE", err, output.CodeOf(err))
			}
		})
	}
}
