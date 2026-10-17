package screen

import (
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/nav"
)

// SubMenu is the sub-menu layout (brief 1B), after Figma's 01-USER. Here
// with no colour and ASCII glyphs, its fixture (UserFixture), the blank
// lines being theme.SectionGap:
//
//	hardn  v0.4.0              https://github.com/abbott/hardn
//
//	  hardn > User Management
//
//	  [+] Non-root             Configured UID >= 1000
//
//	# User Configuration -------------------------------------------
//
//	  [george]
//
//	  [ ] Sudo                 Enabled, No password
//	   -  SSH Keys             7 keys
//	   -  UID:GID              1000:1000
//	   -  Directory            /home/george
//
//	  Select an option
//
//	   1) Manage User          Configure sudo, SSH keys
//	   2) Create User          Configure a new user
//
//	   0) Return
//
//	  > Enter your choice [0-2, b or q]:
//
// The blocks: Header, Breadcrumb, SubjectLine, StatusList, each Section,
// the MenuHeading, the Menu, then the PromptLine after theme.PromptGap
// blank lines. A block that renders nothing is left out with its gap, so
// one layout serves 01-USER (status, a section, a menu), 02-SSH_LOGIN and
// 05-BACKUP (status rows, a section), USER-MANAGE_USER-MANAGEMENT and
// USER-SSH_KEYS (a subject line and a menu) and
// USER-MANAGE_USER-SELECT_USER (a menu of users under its own heading).
// Figma's frames open with a TitleBar where the brief's layout has the
// Header and a Breadcrumb: two lines more (P-08, DEV-053). A sub-menu has
// no compact form: 01-USER takes 23 lines.
type SubMenu struct {
	// Header is the wordmark, the version and the repository link, as on
	// the main menu.
	Header component.Header
	// Breadcrumb says where the sub-menu sits, such as "hardn › User
	// Management".
	Breadcrumb component.Breadcrumb
	// Subject names what the screen acts on, such as "Managing: george";
	// with no subject it is left out.
	Subject component.SubjectLine
	// Status is the area's status rows; with none the block is left out.
	Status component.StatusList
	// Sections are the area's details, in order.
	Sections []Section
	// Heading is the menu heading's title, such as "Select an option",
	// with no mode beside it; empty shows none.
	Heading string
	// Menu is the numbered menu and its Return item. Its Compact is
	// ignored: a sub-menu has no compact form.
	Menu component.Menu
	// Prompt is the prompt's label, such as "Enter your choice".
	Prompt string
}

// Render renders the sub-menu for c, down to its prompt. See SubMenu for
// the layout. The menu is laid out in one column whatever vm.Menu.Compact
// says, as nav.Run draws it from Screen.
func (vm SubMenu) Render(c component.Ctx) string {
	menu := vm.Menu
	menu.Compact = false
	return withMenu(c, vm.above(c), menu, vm.Prompt)
}

// Screen returns what nav.Run draws for the sub-menu, with m as its menu
// in place of vm.Menu: Above holds every block down to the menu heading,
// so that nav.Run draws what Render renders. Call it in each round of
// Run's draw.
func (vm SubMenu) Screen(c component.Ctx, m nav.Menu) nav.Screen {
	return nav.Screen{Above: vm.above(c), Menu: m, Label: vm.Prompt}
}

// above renders every block above the menu.
func (vm SubMenu) above(c component.Ctx) string {
	blocks := []string{vm.Header.Render(c), vm.Breadcrumb.Render(c), vm.Subject.Render(c), vm.Status.Render(c)}
	for _, s := range vm.Sections {
		blocks = append(blocks, s.Render(c))
	}
	blocks = append(blocks, component.MenuHeading{Title: vm.Heading}.Render(c))
	return join(blocks...)
}

// Section is one titled block of a sub-menu's details, as Figma's "User
// Configuration" on 01-USER: the SectionHeader, then the Badge naming
// what the rows describe, then the rows, each block after theme.SectionGap
// blank lines.
type Section struct {
	// Header is the section's header, such as "User Configuration".
	Header component.SectionHeader
	// Badge names what the rows describe, such as the user "george";
	// empty shows none.
	Badge component.Badge
	// Rows are the key-value rows.
	Rows component.BulletList
}

// Render renders the section for c. A section with neither a badge nor a
// row renders as the empty string, so the layout leaves it out with its
// gap rather than show a header over nothing.
func (s Section) Render(c component.Ctx) string {
	body := join(s.Badge.Render(c), s.Rows.Render(c))
	if body == "" {
		return ""
	}
	return join(s.Header.Render(c), body)
}
