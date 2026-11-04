package main

import (
	"testing"

	"github.com/abbott/hardn/pkg/config"
)

// TestMenuOptions: menuOptions, as main fills the main menu's Options, sets
// every field of them, the User Management, SSH Login, DNS, Firewall,
// Backup, Auto Updates, Run All, Environment, System Details and Logs
// items' (the Firewall item's description's too), the status's and
// terminal's, the input and stdout, on the configuration given. What each
// item runs is its own tests' (TestBackupThenDNS, TestBackupThenEnvironment,
// TestFirewallHook, TestUsersHook, TestSSHHook, TestUpdatesHook,
// TestRunAllHook, TestWithStatus); that main.go runs the main menu on
// menuOptions is held by reading, and by the VM checks that choose 3, 5
// and 8 (screen-environment.md §4).
func TestMenuOptions(t *testing.T) {
	c := config.DefaultConfig()
	o := menuOptions(c, "0.4.0")
	for name, ok := range map[string]bool{
		"User Management": o.Users != nil, "SSH Login": o.SSH != nil, "DNS": o.DNS != nil, "Firewall": o.Firewall != nil, "the Firewall item's description": o.FirewallPVE != nil,
		"Backup": o.Backup != nil, "Auto Updates": o.Updates != nil, "Run All": o.RunAll != nil, "Environment": o.Environment != nil,
		"System Details": o.SystemDetails != nil, "Logs": o.Logs != nil,
		"the status": o.Status != nil, "the terminal": o.Terminal != nil, "the input": o.Input != nil, "stdout": o.Out != nil,
		"the configuration": o.Config == c,
	} {
		if !ok {
			t.Errorf("menuOptions did not set %s", name)
		}
	}
}

// TestMenuOptionsWarned: the main menu is told exactly where loading
// hardn.yml warned, so that it keeps the warning on screen.
func TestMenuOptionsWarned(t *testing.T) {
	for _, tc := range []struct {
		logFile string
		warned  bool
	}{
		{config.DefaultConfig().LogFile, false},
		{"/srv/log/hardn.log", true},
	} {
		c := config.DefaultConfig()
		c.LogFile = tc.logFile
		if o := menuOptions(c, "0.4.0"); o.Warned != tc.warned {
			t.Errorf("logFile %s: told warned %v, want %v", tc.logFile, o.Warned, tc.warned)
		}
	}
}
