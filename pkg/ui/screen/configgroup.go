package screen

import "github.com/abbott/hardn/pkg/ui/component"

// The Configuration group (the maintainer, 2026-11-14): main menu 8, a menu of
// the screens that are settings and no hardening step of their own on the main
// menu: hardn.yml (Config), DNS Settings, NTP Settings and Syslog Settings, in
// the maintainer's order (docs/engine/gallery-menu-layout.md, decision 3 (a)):
// DNS came from main menu 4, and Kernel Settings went there. It revises
// gallery-logforward.md's decision 8 and gallery-timesync.md's decision 9,
// which had the two on the main menu. Here with no colour and ASCII glyphs:
//
//	# Configuration ################################################
//
//	   1) hardn.yml            View or edit the file hardn reads
//	   2) DNS Settings         Configure nameservers
//	   3) NTP Settings         Keep the clock in sync
//	   4) Syslog Settings      Send logs to a syslog server
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
	ConfigDNSItem        = "2" // DNS Settings: the DNS screen
	ConfigTimeSyncItem   = "3" // NTP Settings
	ConfigLogForwardItem = "4" // Syslog Settings
)

// The Configuration group's words. The Syslog Settings and NTP Settings items say
// what the main menu's said.
const (
	configFileLabel       = "hardn.yml"
	configFileDesc        = "View or edit the file hardn reads"
	configLogForwardLabel = logFwdTitle
	configLogForwardDesc  = "Send logs to a syslog server"
	configTimeSyncLabel   = timeTitle
	configTimeSyncDesc    = "Keep the clock in sync"
	configDNSLabel        = "DNS Settings"
	configDNSDesc         = "Configure nameservers"
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
			{Key: ConfigDNSItem, Label: configDNSLabel, Description: configDNSDesc},
			{Key: ConfigTimeSyncItem, Label: configTimeSyncLabel, Description: configTimeSyncDesc},
			{Key: ConfigLogForwardItem, Label: configLogForwardLabel, Description: configLogForwardDesc},
		}, Exit: returnItem()},
		Prompt: menuPrompt,
	}
}
