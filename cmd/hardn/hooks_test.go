package main

import (
	"testing"

	"github.com/abbott/hardn/pkg/config"
)

// hooksSet records every hook withHooks sets: the items' (hookSet,
// backup_test.go) and the status's and terminal's (statusSet,
// status_test.go).
type hooksSet struct {
	hookSet
	statusSet
	warned bool
}

func (h *hooksSet) SetWarnedAtLoad(warned bool) { h.warned = warned }

// TestWithHooks: withHooks, as main builds the main menu, sets every hook
// of it, the DNS, Firewall, Backup, Environment, System Details and Logs items' (the
// Firewall item's description's too) and the status's and terminal's, and returns the menu it was given. What each hook runs is
// its own tests' (TestBackupThenDNS, TestBackupThenEnvironment,
// TestFirewallHook, TestWithStatus); that main.go:217 calls withHooks is held by reading,
// and by the VM checks that choose 3, 5 and 8 (screen-environment.md §4).
func TestWithHooks(t *testing.T) {
	set := &hooksSet{}
	if got := withHooks(set, config.DefaultConfig(), "0.4.0"); got != set {
		t.Errorf("withHooks returned %p, want the menu given, %p", got, set)
	}
	for name, ok := range map[string]bool{
		"DNS": set.dns != nil, "Firewall": set.firewall != nil, "the Firewall item's description": set.firewallPVE != nil,
		"Backup": set.backup != nil, "Environment": set.environment != nil,
		"System Details": set.systemDetails != nil, "Logs": set.logs != nil,
		"the status": set.read != nil, "the terminal": set.env != nil,
	} {
		if !ok {
			t.Errorf("withHooks did not set %s", name)
		}
	}
}

// TestWithHooksWarned: the menu is told exactly where loading hardn.yml warned, so that
// it keeps the warning on screen.
func TestWithHooksWarned(t *testing.T) {
	for _, tc := range []struct {
		logFile string
		warned  bool
	}{
		{config.DefaultConfig().LogFile, false},
		{"/srv/log/hardn.log", true},
	} {
		c := config.DefaultConfig()
		c.LogFile = tc.logFile
		set := withHooks(&hooksSet{}, c, "0.4.0")
		if set.warned != tc.warned {
			t.Errorf("logFile %s: told warned %v, want %v", tc.logFile, set.warned, tc.warned)
		}
	}
}
