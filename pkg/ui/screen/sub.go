package screen

import (
	"strings"

	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/theme"
)

// SubMenu is the sub-menu layout (brief 1B), after Figma's 01-USER. Here
// with no colour and ASCII glyphs, its fixture (UserFixture), the blank
// lines being theme.SectionGap:
//
//	# User Management ##############################################
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
//	    > Enter your choice [0-2 or q]:
//
// The blocks: the TitleBar, SubjectLine, StatusList, each Section, the
// MenuHeading, the Menu, then the PromptLine after theme.PromptGap blank
// lines. A block that renders nothing is left out with its gap, so one
// layout serves 01-USER (status, a section, a menu), 02-SSH_LOGIN and
// 05-BACKUP (status rows, a section), USER-MANAGE_USER-MANAGEMENT and
// USER-SSH_KEYS (a subject line and a menu) and
// USER-MANAGE_USER-SELECT_USER (a menu of users under its own heading).
// It opens with Figma's title bar, as P-08 decided (2026-10-17): no
// Header and no Breadcrumb line. The bar shows the screen's title alone;
// its section trail, "User Management › Manage User", shows only when
// TitleTrail is set (DEV-053). A sub-menu has no compact form: 01-USER
// takes 21 lines, as Figma's frame does.
type SubMenu struct {
	// Title is the title bar, such as "Manage User", with the sections
	// above the screen as its Parents, such as "User Management". The
	// parents show only when TitleTrail is set.
	Title component.TitleBar
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
	return vm.render(c, TitleTrail)
}

// render is Render with the trail shown when withTrail is set.
func (vm SubMenu) render(c component.Ctx, withTrail bool) string {
	menu := vm.Menu
	menu.Compact = false
	return withMenu(c, vm.above(c, withTrail), menu, vm.Prompt)
}

// Screen returns what nav.Run draws for the sub-menu, with m as its menu
// in place of vm.Menu: Above holds every block down to the menu heading,
// so that nav.Run draws what Render renders. Call it in each round of
// Run's draw.
func (vm SubMenu) Screen(c component.Ctx, m nav.Menu) nav.Screen {
	return nav.Screen{Above: vm.above(c, TitleTrail), Menu: m, Label: vm.Prompt}
}

// above renders every block above the menu, the title bar with its trail
// when withTrail is set.
func (vm SubMenu) above(c component.Ctx, withTrail bool) string {
	blocks := []string{titleBar(vm.Title, withTrail).Render(c), vm.Subject.Render(c), vm.Status.Render(c)}
	for _, s := range vm.Sections {
		blocks = append(blocks, s.Render(c))
	}
	blocks = append(blocks, component.MenuHeading{Title: vm.Heading}.Render(c))
	return join(blocks...)
}

// Section is one titled block of a sub-menu's details, as Figma's "User
// Configuration" on 01-USER: the SectionHeader, then the Badge naming
// what the rows describe, then the rows, then the code, each block after
// theme.SectionGap blank lines. Code is lines of captured output in the
// rows' place, such as the Logs screen's (DEV-066).
type Section struct {
	// Header is the section's header, such as "User Configuration".
	Header component.SectionHeader
	// Badge names what the rows describe, such as the user "george";
	// empty shows none.
	Badge component.Badge
	// Rows are the key-value rows.
	Rows component.BulletList
	// Code is lines of captured output; with none the block is left out.
	Code component.CodeBlock
}

// Render renders the section for c. A section with no badge, row or code
// renders as the empty string, so the layout leaves it out with its gap
// rather than show a header over nothing. A line of code is something though
// it is blank: a block of one blank line keeps its header and its empty row.
func (s Section) Render(c component.Ctx) string {
	body := join(s.Badge.Render(c), s.Rows.Render(c), s.Code.Render(c))
	switch {
	case body != "":
		return join(s.Header.Render(c), body)
	case len(s.Code.Lines) > 0:
		return s.Header.Render(c) + strings.Repeat("\n", theme.SectionGap+1)
	}
	return ""
}
