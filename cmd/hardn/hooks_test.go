package main

import (
	"context"
	"os"
	"testing"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/output"
	configscreen "github.com/abbott/hardn/pkg/screens/config"
	kernelscreen "github.com/abbott/hardn/pkg/screens/kernelsettings"
	logforwardscreen "github.com/abbott/hardn/pkg/screens/logforward"
	"github.com/abbott/hardn/pkg/screens/mainmenu"
	runallscreen "github.com/abbott/hardn/pkg/screens/runall"
	timesyncscreen "github.com/abbott/hardn/pkg/screens/timesync"
	updatesscreen "github.com/abbott/hardn/pkg/screens/updates"
	"github.com/abbott/hardn/pkg/ui/screen"
)

// TestMenuOptions: menuOptions, as main fills the main menu's Options, sets
// every field of them, the User Management, SSH Login, DNS, Firewall,
// Backup, Auto Updates, Run All, Configuration (its group's Syslog Settings,
// NTP Settings and Kernel Settings too), System Details and Logs items' (the
// Firewall item's description's too), the status's and terminal's, the
// input and stdout, on the configuration given. What each item runs is
// its own tests' (TestBackupThenDNS, TestFirewallHook, TestUsersHook,
// TestSSHHook, TestUpdatesHook, TestRunAllHook, TestWithStatus); that
// main.go runs the main menu on menuOptions is held by reading, and by
// the VM checks that choose 3 and 5. stdout is os.Stdout, and hardn's
// version reaches the status, Run All and Auto Updates, as each records it.
func TestMenuOptions(t *testing.T) {
	h, c := newRunAllHost(t, "debian13", runAllYML)
	useSSHHost(t, h)
	statusWas, runAllWas, updatesWas, logForwardWas, timeSyncWas, kernelWas, groupWas := menuStatus, runAllRun, updatesRun, logForwardRun, timeSyncRun, kernelSettingsRun, configGroupRun
	t.Cleanup(func() {
		menuStatus, runAllRun, updatesRun, logForwardRun, timeSyncRun, kernelSettingsRun, configGroupRun = statusWas, runAllWas, updatesWas, logForwardWas, timeSyncWas, kernelWas, groupWas
	})
	var status mainmenu.StatusOptions
	var runAll runallscreen.Options
	var updates updatesscreen.Options
	menuStatus = func(_ context.Context, o mainmenu.StatusOptions) (screen.MainMenu, error) {
		status = o
		return screen.MainMenu{}, nil
	}
	runAllRun = func(_ context.Context, o runallscreen.Options) error { runAll = o; return nil }
	updatesRun = func(_ context.Context, o updatesscreen.Options) error { updates = o; return nil }
	var logForward logforwardscreen.Options
	logForwardRun = func(_ context.Context, o logforwardscreen.Options) error { logForward = o; return nil }
	var timeSync timesyncscreen.Options
	timeSyncRun = func(_ context.Context, o timesyncscreen.Options) error { timeSync = o; return nil }
	var kernel kernelscreen.Options
	kernelSettingsRun = func(_ context.Context, o kernelscreen.Options) error { kernel = o; return nil }
	const version = "0.4.0-synthetic"
	o := menuOptions(c, config.Found{}, version)
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
	// Configuration (item 8) runs the group with its four screens.
	var group configscreen.GroupOptions
	configGroupRun = func(_ context.Context, g configscreen.GroupOptions) error { group = g; return nil }
	if err := o.Configuration(); err != nil {
		t.Fatal(err)
	}
	if group.Prompter.In == nil || group.Prompter.Out == nil || group.File == nil || group.DNS == nil || group.LogForwarding == nil || group.TimeSync == nil {
		t.Fatalf("the Configuration group's options: %+v", group)
	}
	if err := group.LogForwarding(); err != nil {
		t.Fatal(err)
	}
	// The Syslog Settings screen (the group's 4) runs on the host and the engine
	// hardn builds, with the main menu's configuration, each change a
	// record of the audit log.
	if logForward.Config != c || logForward.System == nil || logForward.NewEngine == nil || logForward.Ran == nil ||
		logForward.ApplyContext == nil || logForward.Prompter.In == nil || logForward.LogPath != output.LegacyLogPath || logForward.DryRun != c.DryRun {
		t.Errorf("the Syslog Settings screen's options: %+v", logForward)
	}
	// The hardn.yml screen (the group's 1), the same: its saves go through
	// the engine too.
	configWas := configRun
	t.Cleanup(func() { configRun = configWas })
	var file configscreen.Options
	configRun = func(_ context.Context, o configscreen.Options) error { file = o; return nil }
	if err := group.File(); err != nil {
		t.Fatal(err)
	}
	if file.Config != c || file.System == nil || file.NewEngine == nil || file.Ran == nil ||
		file.ApplyContext == nil || file.Prompter.In == nil || file.LogPath != output.LegacyLogPath || file.DryRun != c.DryRun {
		t.Errorf("the hardn.yml screen's options: %+v", file)
	}
	// The NTP Settings screen (the group's 3), the same.
	if err := group.TimeSync(); err != nil {
		t.Fatal(err)
	}
	if timeSync.Config != c || timeSync.System == nil || timeSync.NewEngine == nil || timeSync.Ran == nil ||
		timeSync.ApplyContext == nil || timeSync.Prompter.In == nil || timeSync.LogPath != output.LegacyLogPath || timeSync.DryRun != c.DryRun {
		t.Errorf("the NTP Settings screen's options: %+v", timeSync)
	}
	// The Kernel Settings screen (main menu 4), the same.
	if err := o.Kernel(); err != nil {
		t.Fatal(err)
	}
	if kernel.Config != c || kernel.System == nil || kernel.NewEngine == nil || kernel.Ran == nil ||
		kernel.ApplyContext == nil || kernel.Prompter.In == nil || kernel.LogPath != output.LegacyLogPath || kernel.DryRun != c.DryRun {
		t.Errorf("the Kernel Settings screen's options: %+v", kernel)
	}
	if status.Version != "v"+version || runAll.Version != version || updates.Version != version {
		t.Errorf("hardn's version: status %q, Run All %q, Auto Updates %q; want %q", status.Version, runAll.Version, updates.Version, version)
	}
	if status.Now == nil {
		t.Error("the status has no clock: the end-of-support notice counts from the engine's (revertNow)")
	}
	for name, ok := range map[string]bool{
		"User Management": o.Users != nil, "SSH Login": o.SSH != nil, "Kernel": o.Kernel != nil, "Firewall": o.Firewall != nil, "the Firewall item's description": o.FirewallPVE != nil, "the Firewall item's description on Rocky Linux and AlmaLinux": o.FirewallEL != nil,
		"Backup": o.Backup != nil, "Auto Updates": o.Updates != nil, "Run All": o.RunAll != nil,
		"Configuration": o.Configuration != nil, "System Details": o.SystemDetails != nil, "Logs": o.Logs != nil,
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
		if o := menuOptions(c, config.Found{}, "0.4.0"); o.Warned != tc.warned {
			t.Errorf("logFile %s: told warned %v, want %v", tc.logFile, o.Warned, tc.warned)
		}
	}
}
