package cmd

import (
	"errors"
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

// TestRefuseCreateDisableRootAccess: -c with -d is refused, a usage error,
// where sshd's configuration sets any of AllowUsers, AllowGroups,
// DenyUsers and DenyGroups, each named, or could not be read; none set is
// not refused.
func TestRefuseCreateDisableRootAccess(t *testing.T) {
	then := " Run -c, check carol can log in, then run -d."
	for _, tc := range []struct {
		name  string
		lists []string
		err   error
		want  string // "" for no refusal
	}{
		{"none", nil, nil, ""},
		{"AllowUsers", []string{"AllowUsers"}, nil, "hardn -c -d: sshd's AllowUsers decides who can log in, and hardn can't tell whether carol can." + then},
		{"AllowGroups", []string{"AllowGroups"}, nil, "hardn -c -d: sshd's AllowGroups decides who can log in, and hardn can't tell whether carol can." + then},
		{"DenyUsers", []string{"DenyUsers"}, nil, "hardn -c -d: sshd's DenyUsers decides who can log in, and hardn can't tell whether carol can." + then},
		{"DenyGroups", []string{"DenyGroups"}, nil, "hardn -c -d: sshd's DenyGroups decides who can log in, and hardn can't tell whether carol can." + then},
		{"three", []string{"AllowUsers", "AllowGroups", "DenyGroups"}, nil,
			"hardn -c -d: sshd's AllowUsers, AllowGroups and DenyGroups decide who can log in, and hardn can't tell whether carol can." + then},
		{"not read", nil, errors.New("sshd -T: exit status 255"), "hardn -c -d: hardn can't read sshd's configuration (sshd -T), so it can't tell whether carol can log in." + then},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := RefuseCreateDisableRootAccess("carol", tc.lists, tc.err)
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
