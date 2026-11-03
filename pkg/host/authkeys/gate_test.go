package authkeys_test

import (
	"encoding/base64"
	"encoding/binary"
	"testing"

	"github.com/abbott/hardn/pkg/host/authkeys"
)

// TestRSABitsShort: a blob cut short in its modulus is 0 bits, not the
// length of what is there.
func TestRSABitsShort(t *testing.T) {
	blob := binary.BigEndian.AppendUint32(nil, 7)
	blob = append(blob, "ssh-rsa"...)
	blob = binary.BigEndian.AppendUint32(blob, 3)
	blob = append(blob, 1, 0, 1)
	blob = binary.BigEndian.AppendUint32(blob, 257)
	blob = append(blob, 0, 0xc1, 0x41, 0x41)
	k := authkeys.Key{Type: "ssh-rsa", Blob: base64.StdEncoding.EncodeToString(blob)}
	if n := k.RSABits(); n != 0 {
		t.Errorf("RSABits of a cut-short modulus: %d, want 0", n)
	}
}
