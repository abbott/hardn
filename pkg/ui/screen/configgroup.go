package screen

import "github.com/abbott/hardn/pkg/ui/component"

// The Configuration group (the maintainer, 2026-11-14): main menu 8, a menu of
// the screens that are settings and no hardening step of their own on the main
// menu: hardn.yml (Config), Syslog Settings, NTP Settings and Kernel Settings
// (gallery-sysctl.md, decision 1). It revises
// gallery-logforward.md's decision 8 and gallery-timesync.md's decision 9,
// which had the two on the main menu. Here with no colour and ASCII glyphs:
//
//	# Configuration ################################################
//
//	   1) hardn.yml            View the file hardn reads
//	   2) Syslog Settings      Send logs to a syslog server
//	   3) NTP Settings         Keep the clock in sync
//	   4) Kernel Settings      Harden network and kernel
//
//	   0) Return
//
//	    > Option [0-4 or q]:
//
// It reads nothing: its items say what each screen is, with no state beside
// them. There is no Figma frame.

// The Configuration group's items, by key.
const (
	ConfigFileItem       = "1" // hardn.yml: the Configuration screen (Config)
	ConfigLogForwardItem = "2" // Syslog Settings
	ConfigTimeSyncItem   = "3" // NTP Settings
	ConfigKernelItem     = "4" // Kernel Settings
)

// The Configuration group's words. The Syslog Settings and NTP Settings items say
// what the main menu's said.
const (
	configFileLabel       = "hardn.yml"
	configFileDesc        = "View the file hardn reads"
	configLogForwardLabel = logFwdTitle
	configLogForwardDesc  = "Send logs to a syslog server"
	configTimeSyncLabel   = timeTitle
	configTimeSyncDesc    = "Keep the clock in sync"
	configKernelDesc      = "Harden network and kernel"
)

// ConfigGroup is the Configuration group. It has no field: nothing on it
// depends on the host or on hardn.yml.
type ConfigGroup struct{}

// SubMenu returns the screen: the title bar, the four items, Return, "Option".
func (ConfigGroup) SubMenu() SubMenu {
	return SubMenu{
		Title: trail(configTitle),
		Menu: component.Menu{Items: []component.MenuItem{
			{Key: ConfigFileItem, Label: configFileLabel, Description: configFileDesc},
			{Key: ConfigLogForwardItem, Label: configLogForwardLabel, Description: configLogForwardDesc},
			{Key: ConfigTimeSyncItem, Label: configTimeSyncLabel, Description: configTimeSyncDesc},
			{Key: ConfigKernelItem, Label: kernelTitle, Description: configKernelDesc},
		}, Exit: returnItem()},
		Prompt: menuPrompt,
	}
}
