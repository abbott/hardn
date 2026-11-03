package cmd

import (
	"testing"

	"github.com/abbott/hardn/pkg/output"
)

// TestRefuseCreateDisableRoot: -c with -d is refused, a usage error, in its
// words; -c alone, -d alone and neither are not.
func TestRefuseCreateDisableRoot(t *testing.T) {
	for _, tc := range []struct {
		name                string
		create, disableRoot bool
		want                string // "" for no refusal
	}{
		{"neither", false, false, ""},
		{"-c alone", true, false, ""},
		{"-d alone", false, true, ""},
		{"-c -d", true, true,
			"hardn -c -d: -d turns off root's SSH login before hardn can tell the new user can log in. Run -c, check the user can log in, then run -d."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := RefuseCreateDisableRoot(tc.create, tc.disableRoot)
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
