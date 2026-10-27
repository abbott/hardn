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
//     the ModeIndicator, the Menu and the PromptLine, in that full form at
//     every height: its compact form, which fits theme.CompactBudget lines
//     with Figma's seven rows on a terminal shorter than
//     theme.CompactBelow, is gated (CompactMainMenu, DISABLED 2026-10-23;
//     see MainMenu).
//   - SubMenu, after 01-USER: the TitleBar, a SubjectLine, the area's
//     StatusList, its sections (Section: a SectionHeader, a Badge and a
//     BulletList), a MenuHeading, the Menu and the PromptLine.
//   - WorkflowStep, after USER-MANAGE_SUDO-CONFIRMATION: the TitleBar and
//     a SubjectLine above a run of the canonical flow (pkg/ui/flow), which
//     writes each step and asks its prompts.
//
// Sub-screens open with Figma's single title bar, not brief 1B's Header
// and Breadcrumb: proposal P-08, decided by the maintainer (2026-10-17).
// The section trail lives inside the bar, as "User Management › Manage
// User", never with the product name, and is off by default: TitleTrail
// gates it, and each layout keeps its parents in the view-model, so the
// bar shows the screen's title alone, as Figma's frames do, until the
// constant is flipped (DEV-053, DEV-054). The main menu keeps its Header
// and ContextBar.
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
// # Screens of hardn
//
// DNS is the DNS screen (slice 2, next/s2-screen): origin/main's, on the
// sub-menu layout, filled from a reading of the host and of hardn.yml that
// pkg/screens/dns makes, which also runs its items; Review and Step give
// its workflows' review and title bar (docs/engine/screen-dns.md).
//
// Backup is the Backup screen (slice 3, next/s3-screen): Figma's 05-BACKUP
// as the maintainer adopted its mock, with no status row, filled from a
// reading of the backup settings that pkg/screens/backup makes, which also
// runs its items; TurnStep, TurnReview, PathStep and PathReview give its
// workflows, BackupRunsMenu and BackupRun the runs, read only
// (docs/engine/screen-backup.md). Figma's frame stays among the mocks as
// its record (sub-menu/05-backup).
//
// # Fixtures and mocks
//
// MainMenuFixture, UserFixture and ManageSudoFixture fill the three
// layouts with synthetic data after the Figma screenshots each is named
// for. Mocks is the dev-only gallery's catalogue of screen mocks, which
// the goldens pin: those three and every other Figma frame that fits a
// layout, each with its source label (Source). A workflow's mock is its
// sequences, the canonical flow played on scripted answers and fixture
// plans (Sequence.Play), shown step by step. Nothing but the tests and the
// gallery uses them. Figma's mock host BOS-SAN-L01 and user george are
// mock data; the other user is alice, the hosts' addresses are from
// 192.0.2.0/24 and the SSH port is 2230.
package screen
