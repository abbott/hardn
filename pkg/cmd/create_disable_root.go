package cmd

import (
	"fmt"
	"strings"
)

// RefuseCreateDisableRoot returns the usage error for -c (create) combined
// with -d (disableRoot) where -c's user, name, gets no key (keys empty), or
// nil when there is nothing to refuse (refactor plan slice 5, unit (d)): -d
// turns off root's SSH login, and a user -c creates with no key and no
// password can't log in either, so the two together would leave the host
// with no SSH login. With keys, -c's user can log in, and the two may run
// together; a key that is not one is the control's refusal, before
// anything is written.
func RefuseCreateDisableRoot(create, disableRoot bool, name string, keys []string) error {
	if !create || !disableRoot || len(keys) > 0 {
		return nil
	}
	return usageError(fmt.Sprintf("hardn -c -d would leave no SSH login: %s has no key, and -d turns off root's. Give %s a key first.", name, name))
}

// RefuseCreateDisableRootAccess returns the usage error for -c with -d
// where sshd's configuration sets lists, any of AllowUsers, AllowGroups,
// DenyUsers and DenyGroups (ssh.ReadAccessLists), or could not be read
// (err), or nil where it sets none (the maintainer's decision on the
// lockout review, 2026-11-03; proposed words): -d leaves those lists in
// place, and a key does not show that name, whom -c creates, is let in by
// them, so -c -d could leave no SSH login. -c alone, then a login as name,
// then -d, shows it.
func RefuseCreateDisableRootAccess(name string, lists []string, err error) error {
	then := fmt.Sprintf("Run -c, check %s can log in, then run -d.", name)
	switch {
	case err != nil:
		return usageError(fmt.Sprintf("hardn -c -d: hardn can't read sshd's configuration (sshd -T), so it can't tell whether %s can log in. %s", name, then))
	case len(lists) == 0:
		return nil
	}
	which, verb := lists[len(lists)-1], "decides"
	if len(lists) > 1 {
		which, verb = strings.Join(lists[:len(lists)-1], ", ")+" and "+which, "decide"
	}
	return usageError(fmt.Sprintf("hardn -c -d: sshd's %s %s who can log in, and hardn can't tell whether %s can. %s", which, verb, name, then))
}

// DisableRootNotRun is what hardn -c -d says on stderr where -c, which runs
// first, did not create name (refused, or failed and rolled back): -d is
// not run, so root's SSH login stays (proposed words).
func DisableRootNotRun(name string) string {
	return fmt.Sprintf("-d not run: %s wasn't created.", name)
}
