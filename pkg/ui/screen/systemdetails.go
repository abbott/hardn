package screen

import "github.com/abbott/hardn/pkg/ui/component"

// The System Details screen (refactor plan slice 1, unit (c), next/s1-screen):
// main menu 9 and hardn system-details, read only, as the maintainer adopted the
// mock on 2026-10-30 (docs/engine/gallery-system-details.md §5, decisions 2 (a),
// 3 (a), 4 (a), 5 (a), with §6's words). There is no Figma frame: it is inferred
// from origin/main, with P-17's seven sections. pkg/ui cannot reach the host:
// pkg/screens/systemdetails reads it (pkg/engine/details), fills each section's
// rows, made safe to show, and runs the one item.

// The System Details screen's words (gallery-system-details.md §6, decided with
// decision 11): the main menu's label for the title, P-17's sections, Backup's
// heading and prompt. The rows' words are pkg/screens/systemdetails', which
// reads what they say.
const (
	sdTitle        = "System Details"
	sdRefresh      = "Refresh"
	sdRefreshAbout = "Read the host again"
)

// SystemDetailsRefreshItem is the screen's one item but Return, by key.
const SystemDetailsRefreshItem = "1"

// SystemDetails is the System Details report: each section's rows, in P-17's
// order, which pkg/screens/systemdetails fills from details.Report.
type SystemDetails struct {
	OS, Host, Users, CPU, Disk, Memory, Logins []component.KeyValue
}

// sections are the report's seven sections; a section with no row is left out.
func (d SystemDetails) sections() []Section {
	var out []Section
	for _, s := range []struct {
		title string
		rows  []component.KeyValue
	}{
		{"OS and Kernel", d.OS}, {"Host and IPs", d.Host}, {"Users", d.Users}, {"CPU and Load", d.CPU},
		{"Disk", d.Disk}, {"Memory", d.Memory}, {"Last Login", d.Logins},
	} {
		out = append(out, Section{Header: component.SectionHeader{Title: s.title}, Rows: component.BulletList{Items: s.rows}})
	}
	return out
}

// SubMenu returns the screen: the title bar, the report whole, its top scrolling
// off a short terminal (decision 4 (a)), one form at every height; item 1,
// Refresh; Return; "Select an option"; "Enter selection". No Export item
// (decision 5 (a)), and no pause: the screen ends at a menu (P-28 (a), for this
// screen alone). Plan only draws the same: no sub-menu shows the mode.
func (d SystemDetails) SubMenu() SubMenu {
	return SubMenu{
		Title:    trail(sdTitle),
		Sections: d.sections(),
		Heading:  fixtureHeading,
		Menu: component.Menu{Items: []component.MenuItem{
			{Key: SystemDetailsRefreshItem, Label: sdRefresh, Description: sdRefreshAbout},
		}, Exit: returnItem()},
		Prompt: mainPrompt,
	}
}

// Report renders the title bar and the report for c, with no menu and no prompt:
// hardn system-details. Each value is cut at its row on a terminal, as every
// screen's is, and whole under component.OffTerminal (DEV-067).
func (d SystemDetails) Report(c component.Ctx) string {
	blocks := []string{titleBar(trail(sdTitle), TitleTrail).Render(c)}
	for _, s := range d.sections() {
		blocks = append(blocks, s.Render(c))
	}
	return join(blocks...)
}
