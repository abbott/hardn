package cmd

import (
	"reflect"
	"testing"
)

// TestCreateUserAuthorizedKeysFiles checks the AuthorizedKeysFile paths
// V5's gate gives hardn -c.
func TestCreateUserAuthorizedKeysFiles(t *testing.T) {
	if authorizedKeysSSHUOnly {
		t.Fatal("authorizedKeysSSHUOnly is set: hardn -c points AuthorizedKeysFile away from the keys it writes (V5)")
	}
	for _, tc := range []struct {
		sshKeyPath string
		want       []string
	}{
		{".ssh_%u", []string{".ssh/authorized_keys", ".ssh_%u/authorized_keys"}},
		{".ssh_%u/", []string{".ssh/authorized_keys", ".ssh_%u/authorized_keys"}},
		{"/etc/ssh/keys/%u", []string{".ssh/authorized_keys", "/etc/ssh/keys/%u/authorized_keys"}},
		{".ssh", []string{".ssh/authorized_keys"}},
		{"", []string{".ssh/authorized_keys"}},
	} {
		if got := CreateUserAuthorizedKeysFiles(tc.sshKeyPath); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%q: got %q, want %q", tc.sshKeyPath, got, tc.want)
		}
	}
}
