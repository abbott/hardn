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
}

// TestWithHooks: withHooks, as main builds the main menu, sets every hook
// of it, the DNS, Backup, Environment, System Details and Logs items' and the status's and
// terminal's, and returns the menu it was given. What each hook runs is
// its own tests' (TestBackupThenDNS, TestBackupThenEnvironment,
// TestWithStatus); that main.go:213 calls withHooks is held by reading,
// and by the VM checks that choose 3, 5 and 8 (screen-environment.md §4).
func TestWithHooks(t *testing.T) {
	set := &hooksSet{}
	if got := withHooks(set, config.DefaultConfig(), "0.4.0"); got != set {
		t.Errorf("withHooks returned %p, want the menu given, %p", got, set)
	}
	for name, ok := range map[string]bool{
		"DNS": set.dns != nil, "Backup": set.backup != nil, "Environment": set.environment != nil,
		"System Details": set.systemDetails != nil, "Logs": set.logs != nil,
		"the status": set.read != nil, "the terminal": set.env != nil,
	} {
		if !ok {
			t.Errorf("withHooks did not set %s", name)
		}
	}
}
