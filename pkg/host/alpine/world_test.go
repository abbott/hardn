package alpine_test

import (
	"context"
	"errors"
	"io/fs"
	"slices"
	"testing"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/alpine"
	"github.com/abbott/hardn/pkg/host/hostfake"
)

// TestParseServerWorld: World's entries that keep an OpenSSH server
// installed, by name, constraints and tags cut, in World's order, each
// once; "!" entries and other packages are not.
func TestParseServerWorld(t *testing.T) {
	for _, tc := range []struct {
		name, world string
		want        []string
	}{
		{"the Alpine 3.23 VM's", "alpine-base\nchrony\ncloud-init\ndoas\nopenssh\nopenssh-server-pam\ntzdata\n", []string{"openssh", "openssh-server-pam"}},
		{"openssh-server alone", "alpine-base\nopenssh-server\n", []string{"openssh-server"}},
		{"constraints and tags", "openssh>9 openssh-server-krb5@edge openssh-server=10.3_p1-r2\n", []string{"openssh", "openssh-server-krb5", "openssh-server"}},
		{"twice", "openssh\nopenssh~10\n", []string{"openssh"}},
		{"never installed", "!openssh-server\nalpine-base\n", nil},
		{"the client and keygen are not servers", "openssh-client-default\nopenssh-keygen\nopenssh-sftp-server\n", nil},
		{"empty", "", nil},
	} {
		if got := alpine.ParseServerWorld([]byte(tc.world)); !slices.Equal(got, tc.want) {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}
}

// TestServerWorld reads World; a World that cannot be read is the error.
func TestServerWorld(t *testing.T) {
	ctx := context.Background()
	h := hostfake.New(host.Options{})
	if _, err := alpine.ServerWorld(ctx, h); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("no World: %v, want fs.ErrNotExist", err)
	}
	h.AddFile(alpine.World, []byte("alpine-base\nopenssh\n"), 0o644, 0, 0)
	if got, err := alpine.ServerWorld(ctx, h); err != nil || !slices.Equal(got, []string{"openssh"}) {
		t.Errorf("ServerWorld = %q, %v", got, err)
	}
}
