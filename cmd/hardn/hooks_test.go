package main

import (
	"context"
	"os"
	"testing"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/screens/mainmenu"
	runallscreen "github.com/abbott/hardn/pkg/screens/runall"
	updatesscreen "github.com/abbott/hardn/pkg/screens/updates"
	"github.com/abbott/hardn/pkg/ui/screen"
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
// and 8 (screen-environment.md §4). stdout is os.Stdout, and hardn's
// version reaches the status, Run All and Auto Updates, as each records it.
func TestMenuOptions(t *testing.T) {
	h, c := newRunAllHost(t, "debian13", runAllYML)
	useSSHHost(t, h)
	statusWas, runAllWas, updatesWas := menuStatus, runAllRun, updatesRun
	t.Cleanup(func() { menuStatus, runAllRun, updatesRun = statusWas, runAllWas, updatesWas })
	var status mainmenu.StatusOptions
	var runAll runallscreen.Options
	var updates updatesscreen.Options
	menuStatus = func(_ context.Context, o mainmenu.StatusOptions) (screen.MainMenu, error) { status = o; return screen.MainMenu{}, nil }
	runAllRun = func(_ context.Context, o runallscreen.Options) error { runAll = o; return nil }
	updatesRun = func(_ context.Context, o updatesscreen.Options) error { updates = o; return nil }
	const version = "0.4.0-synthetic"
	o := menuOptions(c, version)
	if o.Out != os.Stdout {
		t.Errorf("the main menu's Out %v, want os.Stdout", o.Out)
	}
	if _, err := o.Status(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := o.RunAll(); err != nil {
		t.Fatal(err)
	}
	if err := o.Updates(); err != nil {
		t.Fatal(err)
	}
	if status.Version != "v"+version || runAll.Version != version || updates.Version != version {
		t.Errorf("hardn's version: status %q, Run All %q, Auto Updates %q; want %q", status.Version, runAll.Version, updates.Version, version)
	}
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
