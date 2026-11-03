package cmd

import "fmt"

// RefuseCreateDisableRoot returns the usage error for -c (create) combined
// with -d (disableRoot) where -c's user, name, gets no key (key ""), or nil
// when there is nothing to refuse (refactor plan slice 5, unit (d)): -d
// turns off root's SSH login, and a user -c creates with no key and no
// password can't log in either, so the two together would leave the host
// with no SSH login. With a key, -c's user can log in, and the two may run
// together.
func RefuseCreateDisableRoot(create, disableRoot bool, name, key string) error {
	if !create || !disableRoot || key != "" {
		return nil
	}
	return usageError(fmt.Sprintf("hardn -c -d would leave no SSH login: %s has no key, and -d turns off root's. Give %s a key first.", name, name))
}
