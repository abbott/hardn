package authkeys_test

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/host/authkeys"
)

// TestRSABits: an ssh-rsa blob's modulus length, leading zeros not
// counted; 0 for another type or a blob that does not parse. Synthetic
// moduli, built here.
func TestRSABits(t *testing.T) {
	e := []byte{1, 0, 1}
	key := func(typ string, b []byte) authkeys.Key {
		return authkeys.Key{Type: typ, Blob: base64.StdEncoding.EncodeToString(b)}
	}
	for _, tc := range []struct {
		name string
		k    authkeys.Key
		want int
	}{
		{"1024 bits", key("ssh-rsa", blobOf("ssh-rsa", e, filled(0, 0xc5, 129))), 1024},
		{"4096 bits", key("ssh-rsa", blobOf("ssh-rsa", e, filled(0, 0xc5, 513))), 4096},
		{"4095 bits", key("ssh-rsa", blobOf("ssh-rsa", e, filled(0x7f, 0xc5, 512))), 4095},
		{"the 2048-bit key", mustParse(t, rsaKey), 2048},
		{"ed25519", mustParse(t, edKey), 0},
		{"cut short", key("ssh-rsa", blobOf("ssh-rsa", e)), 0},
		{"another type's blob", key("ssh-rsa", blobOf("ssh-ed25519", e, filled(0, 0xc5, 129))), 0},
		{"not base64", authkeys.Key{Type: "ssh-rsa", Blob: "!"}, 0},
	} {
		if got := tc.k.RSABits(); got != tc.want {
			t.Errorf("%s: RSABits = %d, want %d", tc.name, got, tc.want)
		}
	}
}

func mustParse(t *testing.T, line string) authkeys.Key {
	t.Helper()
	k, err := authkeys.ParsePublic(line)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// TestTypeAccepted: against the VMs' default PubkeyAcceptedAlgorithms
// (sshd -T, Debian 13, Ubuntu 26.04 and Alpine 3.23 alike) and the -o form
// "rsa-sha2-512,rsa-sha2-256" (01-inspect-b-*.log).
func TestTypeAccepted(t *testing.T) {
	def := strings.Split("ssh-ed25519-cert-v01@openssh.com,ecdsa-sha2-nistp256-cert-v01@openssh.com,ecdsa-sha2-nistp384-cert-v01@openssh.com,"+
		"ecdsa-sha2-nistp521-cert-v01@openssh.com,sk-ssh-ed25519-cert-v01@openssh.com,sk-ecdsa-sha2-nistp256-cert-v01@openssh.com,"+
		"rsa-sha2-512-cert-v01@openssh.com,rsa-sha2-256-cert-v01@openssh.com,ssh-ed25519,ecdsa-sha2-nistp256,ecdsa-sha2-nistp384,"+
		"ecdsa-sha2-nistp521,sk-ssh-ed25519@openssh.com,sk-ecdsa-sha2-nistp256@openssh.com,rsa-sha2-512,rsa-sha2-256", ",")
	rsaOnly := []string{"rsa-sha2-512", "rsa-sha2-256"}
	for _, tc := range []struct {
		typ   string
		algos []string
		want  bool
	}{
		{"ssh-ed25519", def, true},
		{"ssh-rsa", def, true},
		{"ecdsa-sha2-nistp384", def, true},
		{"sk-ssh-ed25519@openssh.com", def, true},
		{"ssh-rsa", rsaOnly, true},
		{"ssh-rsa", []string{"rsa-sha2-256"}, true},
		{"ssh-rsa", []string{"ssh-rsa"}, true},
		{"ssh-ed25519", rsaOnly, false},
		{"ssh-rsa", []string{"ssh-ed25519", "rsa-sha2-512-cert-v01@openssh.com"}, false},
		{"ssh-ed25519", []string{"ssh-ed25519-cert-v01@openssh.com"}, false},
		{"ssh-ed25519", nil, true},
	} {
		if got := authkeys.TypeAccepted(tc.typ, tc.algos); got != tc.want {
			t.Errorf("TypeAccepted(%s, %v) = %v, want %v", tc.typ, tc.algos, got, tc.want)
		}
	}
}
