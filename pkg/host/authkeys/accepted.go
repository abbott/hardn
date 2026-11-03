package authkeys

import (
	"encoding/base64"
	"slices"
)

// RSABits is an ssh-rsa key's modulus length in bits (RFC 4253 §6.6: the
// blob's type, e, then n), leading zeros not counted, as sshd compares it
// with RequiredRSASize; 0 for another type or a blob that does not parse.
func (k Key) RSABits() int {
	if k.Type != "ssh-rsa" {
		return 0
	}
	blob, err := base64.StdEncoding.DecodeString(k.Blob)
	if err != nil {
		return 0
	}
	w := wire{b: blob}
	typ := w.next()
	w.next() // e
	n := w.next()
	if w.short || string(typ) != k.Type {
		return 0
	}
	return bitLen(n)
}

// rsaAlgos are the signature algorithms an ssh-rsa key logs in by
// (RFC 8332): any one of them in PubkeyAcceptedAlgorithms accepts it.
var rsaAlgos = []string{"ssh-rsa", "rsa-sha2-256", "rsa-sha2-512"}

// TypeAccepted reports whether sshd's PubkeyAcceptedAlgorithms, as sshd -T
// prints them (algos), take a key of keyType: ssh-ed25519, the
// ecdsa-sha2-nistp* and sk- types by their own name, ssh-rsa by ssh-rsa,
// rsa-sha2-256 or rsa-sha2-512. algos empty (not read) is true. A
// certificate type (-cert-v01@openssh.com) is not a key's own and does not
// accept it.
func TypeAccepted(keyType string, algos []string) bool {
	if len(algos) == 0 {
		return true
	}
	if keyType == "ssh-rsa" {
		return slices.ContainsFunc(rsaAlgos, func(a string) bool { return slices.Contains(algos, a) })
	}
	return slices.Contains(algos, keyType)
}
