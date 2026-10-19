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
		// A value naming the keys file is that file, as the user screens
		// read it and origin/main wrote it.
		{".ssh_%u/authorized_keys", []string{".ssh/authorized_keys", ".ssh_%u/authorized_keys"}},
		{"/etc/ssh/keys/%u_authorized_keys", []string{".ssh/authorized_keys", "/etc/ssh/keys/%u_authorized_keys"}},
		{".ssh/authorized_keys", []string{".ssh/authorized_keys"}},
		{".ssh/authorized_keys/", []string{".ssh/authorized_keys"}},
		// A per-user file without that name is taken as a directory, as
		// v0.3.2 took it (plan §3.17 decides the accepted forms).
		{"/etc/ssh/authorized_keys/%u", []string{".ssh/authorized_keys", "/etc/ssh/authorized_keys/%u/authorized_keys"}},
	} {
		if got := CreateUserAuthorizedKeysFiles(tc.sshKeyPath); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%q: got %q, want %q", tc.sshKeyPath, got, tc.want)
		}
	}
}
