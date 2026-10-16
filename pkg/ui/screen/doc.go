// Package screen holds hardn's screen layouts (brief 1B): the main menu,
// the sub-menu and the workflow step. A layout is a view-model that
// composes the components' view-models (pkg/ui/component) and lays them
// out in one order, so every screen is one of these layouts filled with
// data. A screen that fits none of them is a proposal for a new layout,
// never a one-off.
//
// # Layouts
//
//   - MainMenu, after Figma's 00-TUI-MAIN: the Header, ContextBar,
//     HostBlock, RiskSummary, StatusList and Divider, the MenuHeading with
//     the ModeIndicator, the Menu and the PromptLine. On a terminal
//     shorter than theme.CompactBelow it renders its compact form, which
//     fits theme.CompactBudget lines (see MainMenu).
//   - SubMenu, after 01-USER: the Header and a Breadcrumb, a SubjectLine,
//     the area's StatusList, its sections (Section: a SectionHeader, a
//     Badge and a BulletList), a MenuHeading, the Menu and the PromptLine.
//   - WorkflowStep, after USER-MANAGE_SUDO-CONFIRMATION: the Header, a
//     Breadcrumb and a SubjectLine above a run of the canonical flow
//     (pkg/ui/flow), which writes each step and asks its prompts.
//
// Sub-screens open with the Header and a Breadcrumb, as brief 1B's
// layouts have them, where Figma's frames draw a TitleBar; proposal P-08
// puts the choice to the maintainer (DEV-053).
//
// # Rendering
//
// Render is a pure function of the view-model and the render context
// (component.Ctx): no I/O, no clock, no environment reads. The blocks are
// joined with theme.SectionGap blank lines, and a block that renders
// nothing is left out with its gap, as an empty StatusList is. A menu
// layout ends with its PromptLine, after theme.PromptGap blank lines, its
// hint generated from the menu's keys and the navigation inputs
// (keys.Navigation), as nav.Menu.Accepts generates it.
//
// # Running
//
// A menu layout's Render is exactly what nav.Run draws for it, up to the
// space the line reader writes after the prompt: Screen returns the
// nav.Screen to draw, its Above holding everything above the menu, and
// nav.Run draws the menu from the options it dispatches. A WorkflowStep's
// Run checks the flow (flow.Flow.Check), writes the screen, then runs the
// flow under it, and sets the two apart with theme.SectionGap blank lines
// as the flow and its form expect of a caller that draws above them; a
// flow refused before it writes leaves no screen.
//
// # Fixtures and mocks
//
// MainMenuFixture, UserFixture and ManageSudoFixture fill the three
// layouts with synthetic data after the Figma screenshots each is named
// for. Mocks is the dev-only gallery's catalogue of screen mocks, which
// the goldens pin: those three, each with its source label (Source). A
// workflow's mock is its sequences, the canonical flow
// played on scripted answers and fixture plans (Sequence.Play), shown
// step by step. Nothing but the tests and the gallery uses them. Figma's
// mock host BOS-SAN-L01 and user george are mock data; the address is
// from 192.0.2.0/24 and the SSH port is 2230.
package screen
