package main

// The main menu's Options, as main fills them for the one main menu hardn
// runs (runMenu): the User Management, SSH Login, DNS, Firewall, Backup,
// Auto Updates, Run All, System Details and Logs items'
// screens (users.go, ssh.go, dns.go, firewall.go, backup.go, updates.go,
// runall.go, readers.go), its status and terminal
// (status.go), its input and stdout. One function, so that a test holds
// each of them filled (TestMenuOptions): a main menu without one panics
// when its item is chosen, or on its first pass for the status.

import (
	"context"
	"errors"
	"os"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/screens/mainmenu"
	"github.com/abbott/hardn/pkg/ui/prompt"
)

// menuOptions returns the main menu's Options on c, the configuration every
// screen reads, and hardn's version: its User Management, SSH Login, DNS,
// Firewall, Backup, Auto Updates, Run All, System Details and
// Logs items (withUsersScreen, withSSHScreen, withDNSScreen,
// withFirewallScreen, withBackupScreen, withUpdatesScreen,
// withRunAllScreen, withReaderScreens), its status
// and terminal (withStatus), whether c warned at load or --no-color was
// given (warnNoColor), for the main menu to keep the warning, and the line reader every prompt shares and stdout.
func menuOptions(c *config.Config, version string) mainmenu.Options {
	m := mainmenu.Options{Config: c, Warned: noColorGiven() || loadWarnings(c) != "", Input: engineInput, Out: os.Stdout}
	m = withUpdatesScreen(withBackupScreen(withDNSScreen(withSSHScreen(withUsersScreen(m, c), c), c), c), c, version)
	m = withRunAllScreen(m, c, version)
	return withStatus(withReaderScreens(withFirewallScreen(m, c), c), c, version)
}

// reraise ends hardn by SIGINT after Ctrl-C at the main menu: a seam for
// the tests.
var reraise = prompt.Reraise

// runMenu runs the main menu on o until the user leaves it, and ends hardn
// as the main menu's error says (mainmenu.Run): by SIGINT after Ctrl-C
// (reraise), so that a shell running hardn in a loop or a script stops;
// with exit status 130 at the end of the input (prompt.ExitCode); and with
// 1, once the error is said on stderr (sayError), for any other.
func runMenu(ctx context.Context, o mainmenu.Options) {
	err := mainmenu.Run(ctx, o)
	if err == nil {
		return
	}
	if errors.Is(err, prompt.ErrInterrupted) {
		reraise()
	}
	code, aborted := prompt.ExitCode(err)
	if !aborted {
		sayError("%v", err)
		code = 1
	}
	exit(code)
}
