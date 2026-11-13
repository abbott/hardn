package ssh_test

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/engine/controls/ssh"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/sshd"
)

// drop takes keyword k's line out of sshd -T's output.
func drop(k string) func(string) string {
	return func(out string) string {
		var ls []string
		for _, l := range strings.Split(out, "\n") {
			if kw, _, _ := strings.Cut(l, " "); kw != k {
				ls = append(ls, l)
			}
		}
		return strings.Join(ls, "\n")
	}
}

// TestReadKeySettings: sshd -T's AuthorizedKeysFile, PubkeyAcceptedAlgorithms
// and RequiredRSASize; no requiredrsasize line (before OpenSSH 9.1) is 0, and
// no algorithms line nil; an sshd that is not there, a configuration it
// refuses, no authorizedkeysfile line and a size that is no number are errors,
// never a default. It writes nothing.
func TestReadKeySettings(t *testing.T) {
	missing := &hostfake.Response{Err: fmt.Errorf("host: run sshd: %w", host.ErrNotFound)}
	invalid := &hostfake.Response{Stderr: "/etc/ssh/sshd_config line 1: Bad configuration option: Bogus\r\n", ExitCode: 255}
	algos := []string{"ssh-ed25519", "ecdsa-sha2-nistp256", "rsa-sha2-512", "rsa-sha2-256"}
	for _, tt := range []struct {
		name string
		site site
		want ssh.KeySettings
		err  bool
	}{
		{name: "debian", site: site{plat: debian13()}, want: ssh.KeySettings{Files: []string{".ssh/authorized_keys", ".ssh/authorized_keys2"}, Algorithms: algos}},
		{name: "alpine, its own file and size", site: site{plat: alpine324(), drops: drops("10-site.conf", "AuthorizedKeysFile /etc/ssh/keys/%u none\n"),
			edit: func(out string) string { return out + "requiredrsasize 3072\n" }},
			want: ssh.KeySettings{Files: []string{"/etc/ssh/keys/%u", "none"}, Algorithms: algos, RSAMin: 3072}},
		{name: "no algorithms line", site: site{plat: debian13(), edit: drop("pubkeyacceptedalgorithms")},
			want: ssh.KeySettings{Files: []string{".ssh/authorized_keys", ".ssh/authorized_keys2"}}},
		{name: "no authorizedkeysfile line", site: site{plat: debian13(), edit: drop("authorizedkeysfile")}, err: true},
		{name: "a size that is no number", site: site{plat: debian13(), edit: func(out string) string { return out + "requiredrsasize big\n" }}, err: true},
		{name: "a negative size", site: site{plat: debian13(), edit: func(out string) string { return out + "requiredrsasize -1\n" }}, err: true},
		{name: "no sshd", site: site{plat: debian13(), failT: missing}, err: true},
		{name: "a configuration sshd refuses", site: site{plat: debian13(), failT: invalid}, err: true},
		{name: "no sshd_config", site: site{plat: debian13(), noMain: true}, err: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f, facts := tt.site.build(t)
			got, err := ssh.ReadKeySettings(context.Background(), f, facts)
			if (err != nil) != tt.err || !slices.Equal(got.Files, tt.want.Files) || !slices.Equal(got.Algorithms, tt.want.Algorithms) || got.RSAMin != tt.want.RSAMin {
				t.Errorf("ReadKeySettings = %+v, %v", got, err)
			}
			readOnly(t, f, tt.site.plat.bin)
		})
	}
	if _, err := ssh.KeySettingsOf(sshd.Effective{}); err == nil {
		t.Error("KeySettingsOf of nothing read: no error")
	}
}
