package cmd

// RefuseCreateDisableRoot returns the usage error for -c (create) combined
// with -d (disableRoot), or nil when there is nothing to refuse (refactor
// plan slice 5, unit (d); the maintainer, 2026-11-03; proposed words).
// Since the maintainer's -c decision of 2026-11-03, "(a) Add sshKeys", -c's
// user may have a key, but a key in authorized_keys is not proof of a
// login: sshd's AllowUsers, AllowGroups, DenyUsers, DenyGroups,
// PubkeyAuthentication, AuthenticationMethods, PubkeyAcceptedAlgorithms or
// a Match block can still keep the user out, and -d, which turns off root's
// SSH login, is legacy until slice 6 rebuilds it. So the pair is refused
// whatever hardn.yml holds, before anything runs; -c, a login as its user,
// then -d, shows it.
func RefuseCreateDisableRoot(create, disableRoot bool) error {
	if !create || !disableRoot {
		return nil
	}
	return usageError("hardn -c -d: -d turns off root's SSH login before hardn can tell the new user can log in. Run -c, check the user can log in, then run -d.")
}
