package main

// The main menu's hooks, as main sets them on the one main menu hardn
// builds (main.go:217): the User Management, SSH Login, DNS, Firewall,
// Backup, Environment, System Details and Logs items' screens (users.go,
// ssh.go, dns.go, firewall.go, backup.go, environment.go, readers.go) and
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
	usersMenu
	sshMenu
	dnsMenu
	firewallMenu
	backupMenu
	environmentMenu
	readerMenu
	statusMenu
	SetWarnedAtLoad(warned bool)
}

// The main menu is a hookedMenu.
var _ hookedMenu = (*menu.MainMenu)(nil)

// withHooks returns m with every hook of its set on c, the configuration
// m was given, and hardn's version: its User Management, SSH Login, DNS,
// Firewall, Backup, Environment, System Details and Logs items
// (withUsersScreen, withSSHScreen, withDNSScreen, withFirewallScreen,
// withBackupScreen, withEnvironmentScreen, withReaderScreens) and its
// status and terminal
// (withStatus); and whether c warned at load, for the menu to keep the warning.
func withHooks[M hookedMenu](m M, c *config.Config, version string) M {
	m.SetWarnedAtLoad(loadWarnings(c) != "")
	return withStatus(withReaderScreens(withEnvironmentScreen(withFirewallScreen(withBackupScreen(withDNSScreen(withSSHScreen(withUsersScreen(m, c), c), c), c), c), c), c), c, version)
}
