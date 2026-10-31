package main

// The main menu's hooks, as main sets them on the one main menu hardn
// builds (main.go:213): the DNS, Backup, Environment, System Details and
// Logs items' screens (dns.go, backup.go, environment.go, readers.go) and
// its status and terminal (status.go). One function, so that a test holds each of them set
// (TestWithHooks): a main menu without one panics when its item is
// chosen, or on its first pass for the status.

import (
	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/menu"
)

// hookedMenu is what withHooks sets the hooks of: pkg/menu's main menu,
// and the tests' record of what was set.
type hookedMenu interface {
	dnsMenu
	backupMenu
	environmentMenu
	readerMenu
	statusMenu
}

// The main menu is a hookedMenu.
var _ hookedMenu = (*menu.MainMenu)(nil)

// withHooks returns m with every hook of its set on c, the configuration
// m was given, and hardn's version: its DNS, Backup, Environment, System
// Details and Logs items (withDNSScreen, withBackupScreen,
// withEnvironmentScreen, withReaderScreens) and its status and terminal
// (withStatus).
func withHooks[M hookedMenu](m M, c *config.Config, version string) M {
	return withStatus(withReaderScreens(withEnvironmentScreen(withBackupScreen(withDNSScreen(m, c), c), c), c), c, version)
}
