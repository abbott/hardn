package menu

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/abbott/hardn/pkg/application"
	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/domain/model"
	"github.com/abbott/hardn/pkg/osdetect"
	"github.com/abbott/hardn/pkg/security"
	"github.com/abbott/hardn/pkg/style"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/keys"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/prompt"
	"github.com/abbott/hardn/pkg/ui/screen"
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/utils"
	"github.com/abbott/hardn/pkg/version"
)

// MainMenu is the main menu of the application
type MainMenu struct {
	menuManager *application.MenuManager
	config      *config.Config
	osInfo      *osdetect.OSInfo

	// Version service for update checks
	versionService *version.Service

	// Update state fields
	updateAvailable bool
	latestVersion   string
	updateURL       string
	installURL      string

	// Security update fields
	securityUpdateAvailable bool
	securityUpdateDetails   string

	// dns runs the DNS item (SetDNS).
	dns func() error

	// backup runs the Backup item (SetBackup).
	backup func() error

	// environment runs the Environment item (SetEnvironment).
	environment func() error

	// status reads the host block, risk level and status rows (SetStatus).
	status func(ctx context.Context) (screen.MainMenu, error)

	// terminal is the terminal the main menu is drawn on (SetTerminal).
	terminal func() term.Env
}

// The main menu's words on its layout: origin/main's heading and prompt
// (pkg/style's Menu, style.go:668), unchanged (refactor plan slice 2b,
// decision 5; P-29 (a) still open).
const (
	mainHeading = "Select an option"
	mainPrompt  = "Enter selection"
)

// notInPlanOnly refuses, under plan only, a legacy screen that changes the
// host or saves ./hardn.yml whatever the mode (refactor plan slice 2b,
// unit (d), decision 6, adopted 2026-10-23): the main menu's User
// Management, before its screen, and Firewall's profile add and remove,
// until their slices put them on the flow. Backup's was lifted when its
// screen went on the flow (slice 3, unit (d), decision 10): under plan
// only it opens, and each change stops at its plan.
const notInPlanOnly = "Not available in plan only."

// errAgain ends a pass of the main menu once an item's screen has
// returned, so that the next pass reads the status again and draws the
// main menu under what the screen printed.
var errAgain = errors.New("menu: the main menu again")

// legacySecurityStatus: DISABLED 2026-10-23. Flip to true to restore.
//
// With it true, each pass of the main menu draws origin/main's block
// above its heading and items, in place of the layout's status blocks,
// from pkg/security: the version line, the host and
// uptime lines (MenuManager.GetHostInfo), the OS title bar, the risk
// level of GetSecurityRiskLevel (eight equal booleans) and the rows of
// DisplaySecurityStatusWithCustomPrinter: Users, Sudo (when missing),
// Sudo Method, Firewall, SSH Login, SSH Auth, SSH Port (from hardn.yml's
// port, not the host's), AppArmor and Auto Updates, read by
// CheckSecurityStatus from files and commands of its own (showLegacyStatus).
// With it false, the main menu goes live (the maintainer, 2026-10-23,
// refactor plan slice 2, next/s2-menu-live): the block is drawn from the
// status model, the controls' checks, the header probe and the risk
// level, through the hook cmd/hardn sets (SetStatus), as the gallery and
// p4status draw it, the DNS row included (docs/engine/menu-live.md §7
// says what changes). The security-update alert, which replaces the block
// when an update check finds a security release, is drawn either way;
// it needs update checks, off while pkg/version's updateCheckEnabled is
// false (V26).
const legacySecurityStatus = false

// NewMainMenu creates a new MainMenu
func NewMainMenu(
	menuManager *application.MenuManager,
	config *config.Config,
	osInfo *osdetect.OSInfo,
	versionService *version.Service,
) *MainMenu {
	return &MainMenu{
		menuManager:    menuManager,
		config:         config,
		osInfo:         osInfo,
		versionService: versionService,
	}
}

// SetDNS sets what the DNS item (3) runs: the DNS screen
// (pkg/screens/dns) on the plan/apply engine, which cmd/hardn assembles,
// so that pkg/menu builds no engine (refactor plan slice 2, unit (d)). It
// returns nil when the user goes back to the main menu, nav.ErrQuit when
// they quit with q, and an error wrapping prompt.ErrAborted at the end of
// the input or on Ctrl-C. The screen edits hardn.yml's list in the
// configuration this menu was given, so the main menu shows the list as
// saved when it returns. A main menu whose DNS item is chosen without one
// set panics: cmd/hardn sets it on the one main menu it builds.
func (m *MainMenu) SetDNS(run func() error) { m.dns = run }

// showDNS is the DNS item's action: the DNS screen (showScreen).
func (m *MainMenu) showDNS() error {
	return showScreen(m.dns, "menu: the DNS item has no screen (SetDNS)")
}

// SetBackup sets what the Backup item (5) runs: the Backup screen
// (pkg/screens/backup) on the plan/apply engine, which cmd/hardn
// assembles, as SetDNS sets the DNS item's (refactor plan slice 3, unit
// (d)). It returns as SetDNS's does. The screen adopts hardn.yml as each
// change saved it into the configuration this menu was given, so the main
// menu, and the DNS screen's next changes, take the backup settings as
// saved. A main menu whose Backup item is chosen without one set panics:
// cmd/hardn sets it on the one main menu it builds.
func (m *MainMenu) SetBackup(run func() error) { m.backup = run }

// showBackup is the Backup item's action: the Backup screen (showScreen).
func (m *MainMenu) showBackup() error {
	return showScreen(m.backup, "menu: the Backup item has no screen (SetBackup)")
}

// SetEnvironment sets what the Environment item (8) runs: the Environment
// screen (pkg/screens/environment) on the plan/apply engine, which
// cmd/hardn assembles, as SetDNS and SetBackup set theirs (refactor plan
// slice 9, unit (d)). It returns as SetDNS's does. The screen reads and
// saves no setting of hardn.yml, so the main menu's configuration is as it
// was. A main menu whose Environment item is chosen without one set
// panics: cmd/hardn sets it on the one main menu it builds.
func (m *MainMenu) SetEnvironment(run func() error) { m.environment = run }

// showEnvironment is the Environment item's action: the Environment screen
// (showScreen).
func (m *MainMenu) showEnvironment() error {
	return showScreen(m.environment, "menu: the Environment item has no screen (SetEnvironment)")
}

// showScreen runs an item's screen that cmd/hardn sets (SetDNS,
// SetBackup, SetEnvironment) from a cleared screen, as every item's screen starts, and
// panics with unset, a programming error, when none is set. Return, or a
// back from it, comes back here (errAgain); q quits from the screen as
// from this menu (nav.ErrQuit); the end of the input, Ctrl-C and any
// other error end hardn as at a legacy prompt (ShowMainMenu, endHardn).
func showScreen(run func() error, unset string) error {
	if run == nil {
		panic(unset)
	}
	utils.ClearScreen()
	err := run()
	if err == nil || errors.Is(err, nav.ErrBack) {
		return errAgain
	}
	return err
}

// SetStatus sets what reads the main menu's host block, risk level and
// status rows: the status model (the controls' checks, the header probe
// and the risk level), which cmd/hardn reads from the host it builds, so
// that pkg/menu runs no check and builds no engine, as with SetDNS
// (refactor plan slice 2, next/s2-menu-live). It is called on every pass
// of the main menu and returns the main menu's view-model with those
// blocks filled (pkg/screens/mainmenu.Status); the main menu fills in its
// heading, mode indicator, items and prompt and draws the whole layout
// (slice 2b, next/s2b-menu). An error ends hardn as at a legacy prompt: a
// cancelled context (Ctrl-C) by SIGINT, any other with exit status 1 once
// printed. A main menu drawn without one set panics: cmd/hardn sets it on
// the one main menu it builds.
func (m *MainMenu) SetStatus(read func(ctx context.Context) (screen.MainMenu, error)) {
	m.status = read
}

// SetTerminal sets the terminal the main menu is drawn on: stdout's, as
// cmd/hardn detects it (its width, colours and glyphs), asked again on
// every pass, so that each pass draws for the terminal as it is then. A
// main menu drawn without one set panics, as with SetStatus.
func (m *MainMenu) SetTerminal(env func() term.Env) { m.terminal = env }

// readStatus reads the status for one pass (SetStatus): the main menu's
// view-model with its status blocks filled, or none while
// legacySecurityStatus draws origin/main's block in their place. The
// checks only read, and run again on every pass, so each pass shows the
// host as it is then. Nothing bounds a pass: ctx is context.Background(),
// never done, so a command that blocks holds the screen until it returns
// or Ctrl-C ends hardn (docs/engine/menu-live.md §5). Ctrl-C while they
// run: no signal handler is registered for them, so Go's default ends
// hardn by SIGINT at once, as prompt.Reraise ends it at the menu's prompt;
// it does not reach the hook through ctx. A hook that returns a cancelled
// context's error, of a context of its own, ends hardn the same way: the
// error is returned as Ctrl-C's (prompt.ErrInterrupted).
func (m *MainMenu) readStatus(ctx context.Context) (screen.MainMenu, error) {
	if legacySecurityStatus {
		return screen.MainMenu{}, nil
	}
	if m.status == nil {
		panic("menu: the main menu has no status (SetStatus)")
	}
	vm, err := m.status(ctx)
	if errors.Is(err, context.Canceled) {
		return vm, fmt.Errorf("%w: %w", prompt.ErrInterrupted, err)
	}
	return vm, err
}

// showLegacyStatus is origin/main's block (legacySecurityStatus): the
// status pkg/security reads, drawn by displaySecurityStatus with
// origin/main's formatter, or a blank line when it is not read.
func (m *MainMenu) showLegacyStatus() {
	// Get security status
	securityStatus, err := security.CheckSecurityStatus(m.config, m.osInfo)

	// Create formatter for security status
	formatter := style.NewStatusFormatter([]string{
		"Host",
		"OS",
		"Uptime",
		"Risk Level",
		"SSH Root Login",
		"Firewall",
		"Users",
		"SSH Port",
		"SSH Auth",
		"AppArmor",
		"Auto Updates",
	}, 2) // 2 spaces buffer

	// Display security status if available
	if err == nil {
		m.displaySecurityStatus(securityStatus, formatter)
	} else {
		fmt.Println()
	}
}

// refresh any configuration values that might have been
// changed by sub-menus like RunAllMenu
func (m *MainMenu) refreshConfig() {
	// add ways for sub-menus to notify the main menu of changes,

	// For now, we're using a shared config pointer, so changes are automatically visible
}

const (
	repoURL             = "https://github.com/abbott/hardn"
	testVersionNumber   = "0.3.3"
	testReleaseURL      = "https://api.github.com/repos/abbott/hardn/releases/latest"
	testInstallScript   = "curl -sSL https://raw.githubusercontent.com/abbott/hardn/main/install.sh | sudo sh"
	testSecurityMessage = "Critical security vulnerability fixed - CVE-2023-1234"
)

// Check for new version (no-op, no goroutine, while checks are disabled)
func (m *MainMenu) CheckForUpdates() {
	if m.versionService == nil || m.versionService.CurrentVersion == "" || !version.UpdateCheckEnabled() {
		return
	}

	// Use Go outine to Avoid blocking display
	go func() {
		result := m.versionService.CheckForUpdates(&version.UpdateOptions{
			Debug: os.Getenv("HARDN_DEBUG") != "",
		})

		if result.Error != nil {
			return
		}

		m.applyUpdateResult(result)
	}()
}

// Apply update check result
func (m *MainMenu) applyUpdateResult(result version.CheckResult) {
	if result.UpdateAvailable {
		m.updateAvailable = true
		m.latestVersion = result.LatestVersion
		m.updateURL = result.ReleaseURL
		m.installURL = result.InstallURL
		m.securityUpdateAvailable = result.SecurityUpdateAvailable
		m.securityUpdateDetails = result.SecurityUpdateDetails
	}
}

// Update checks respecting environment variables
func (m *MainMenu) CheckForUpdatesWithEnvVars() {
	if m.versionService == nil {
		return
	}

	// Check for environment variables that trigger test modes
	if os.Getenv("HARDN_FORCE_UPDATE") != "" {
		m.SetTestUpdateAvailable(testVersionNumber)
	} else if os.Getenv("HARDN_FORCE_SECURITY") != "" {
		m.SetTestSecurityUpdate(testSecurityMessage)
	} else {
		m.CheckForUpdates()
	}
}

// SetTestUpdateAvailable sets test update information
func (m *MainMenu) SetTestUpdateAvailable(testVersion string) {
	if m.versionService == nil {
		return
	}

	result := m.versionService.CheckForUpdates(&version.UpdateOptions{
		ForceUpdate:   true,
		ForcedVersion: testVersion,
	})

	m.applyUpdateResult(result)
}

// SetTestSecurityUpdate sets test security update information
func (m *MainMenu) SetTestSecurityUpdate(details string) {
	if m.versionService == nil {
		return
	}

	// Use a shorter default message if none provided
	if details == "" {
		details = "CVE-2023-1234 fixed"
	} else if len(details) > 80 {
		// Truncate long security details to prevent layout issues
		details = details[:77] + "..."
	}

	result := m.versionService.CheckForUpdates(&version.UpdateOptions{
		ForceUpdate:         true,
		ForcedVersion:       m.versionService.CurrentVersion + ".1",
		ForceSecurityUpdate: true,
		SecurityDetails:     details,
	})

	m.applyUpdateResult(result)
}

// displaySecurityStatus displays the security status with appropriate borders
func (m *MainMenu) displaySecurityStatus(securityStatus *security.SecurityStatus, formatter *style.StatusFormatter) {
	// If security update is available, display special alert and return
	if m.securityUpdateAvailable && m.versionService != nil && m.versionService.CurrentVersion != "" {
		m.displaySecurityUpdateAlert(formatter)
		return
	}

	// Otherwise display the normal security status
	m.displayNormalSecurityStatus(securityStatus, formatter)
}

// displaySecurityUpdateAlert displays the security update alert
// Preserves exact formatting and messaging for security updates
func (m *MainMenu) displaySecurityUpdateAlert(formatter *style.StatusFormatter) {
	// Format hardn version header
	hardnBold := style.Bold + "hardn" + style.Reset
	hardnPad := " " + hardnBold + " "
	hardnLabel := style.Colored(style.BgGray02, hardnPad)
	currentVersion := "v" + m.versionService.CurrentVersion
	currentVersionPad := " " + currentVersion + " "
	currentVersionDim := style.Dimmed(currentVersionPad)
	currentVersionBg := style.Colored(style.BgGray03, currentVersionDim)
	hardnVersion := hardnLabel + currentVersionBg

	// Format security related content
	latestVersion := "v" + m.latestVersion
	securityHeader := style.Colored(style.BgDarkRed, " Update Binary ")

	// Display the alert - exact formatting preserved from original
	fmt.Println(hardnVersion)
	fmt.Println()
	fmt.Println()

	fmt.Println("  " + securityHeader)
	fmt.Println()
	fmt.Println("  " + m.securityUpdateDetails)

	fmt.Printf("  %s\n", style.Colored(style.Royal, m.updateURL))
	fmt.Println()

	infoFormatter := style.NewStatusFormatter([]string{
		"Build Date",
		"Git Commit",
	}, 2) // 2 spaces buffer

	// Add "  " prefix to each line for consistent indentation
	fmt.Println("    " + infoFormatter.FormatBullet("Version", latestVersion, "", "no-indent"))
	fmt.Println("    " + infoFormatter.FormatBullet("Build Date", m.versionService.BuildDate, "", "no-indent"))
	fmt.Println("    " + infoFormatter.FormatBullet("Git Commit", m.versionService.GitCommit, "", "no-indent"))
	fmt.Println()
	fmt.Println(style.Bolded("  Installer Script:"))
	fmt.Println(style.Colored(style.Royal, "  "+m.installURL))
	fmt.Println()
	fmt.Println()
	fmt.Print(style.Dimmed("Press Enter to exit... "))
}

// displayNormalSecurityStatus displays the normal security status in a box
func (m *MainMenu) displayNormalSecurityStatus(securityStatus *security.SecurityStatus, formatter *style.StatusFormatter) {
	// Format hardn version line with update info if available
	hardnLine := m.formatHardnVersionLine(formatter)

	// Display version information
	fmt.Println(hardnLine)

	repo := style.Dimmed(repoURL)

	// When update is available, also display the repo URL on a new line
	if m.updateAvailable {
		repoLine := formatter.FormatLine("", "", "", repo, style.Gray08, "", "no-indent")
		fmt.Println(repoLine)
	}

	// Get host information and format lines for display
	hostInfo, err := m.menuManager.GetHostInfo()
	hostLine := ""
	uptimeLine := ""
	if err == nil && hostInfo != nil {
		hostLine, uptimeLine = m.formatHostInfoLines(hostInfo, formatter)
	}

	// Format OS information for the box title
	osTitle := m.formatOSTitle()

	// Create a separate box for security status
	securityBox := style.NewBox(style.BoxConfig{
		Width:               64,
		ShowEmptyRow:        true,
		ShowTopShade:        true,
		ShowBottomSeparator: true,
		Indentation:         0,
		Title:               osTitle,
		TitleColor:          "skip",
	})

	// Draw the security box with all content
	securityBox.DrawBox(func(printLine func(string)) {
		// Display host information if available
		if hostLine != "" {
			printLine(hostLine)
			printLine(uptimeLine)
			printLine("")
		}

		// Display security status if available
		if securityStatus != nil {
			// Define indentation for all security status items
			indentSpaces := 2
			indentedPrintLine := style.IndentPrinter(printLine, indentSpaces)

			// Display risk level with appropriate color
			riskLevel, riskDescription, riskColor := security.GetSecurityRiskLevel(securityStatus)
			boldRiskLabel := style.Bold + "Risk Level" + style.Reset
			riskDescription = style.SymApprox + " " + riskDescription
			riskLine := formatter.FormatLine(style.SymDotTri, riskColor, boldRiskLabel, riskLevel, riskColor, riskDescription, "dark")

			// Use indented print function for risk level as well
			indentedPrintLine(riskLine)

			// Add empty line after risk level
			indentedPrintLine("")

			// Display security status items using the same indentation
			security.DisplaySecurityStatusWithCustomPrinter(m.config, securityStatus, formatter, indentedPrintLine, 0)

			// securityBox.DrawSeparator()
		}
	})
}

// formatOSTitle formats the OS information into a pretty title string
func (m *MainMenu) formatOSTitle() string {
	if m.osInfo == nil {
		return ""
	}

	boldOsType := style.Bolded(utils.Capitalize(m.osInfo.OsType))
	dimOsType := style.Dimmed(boldOsType, style.Gray15)
	regularVersion := style.Dimmed(m.osInfo.OsVersion, style.Gray15)
	grayCodename := style.Dimmed("("+utils.Capitalize(m.osInfo.OsCodename+")"), style.Gray15)

	// Format based on OS type
	switch m.osInfo.OsType {
	case "debian":
		// For Debian, format as "Debian X (Codename)"
		return fmt.Sprintf("%s %s %s", dimOsType, regularVersion, grayCodename)
	case "ubuntu":
		// For Ubuntu, format as "Ubuntu X.Y (Codename)"
		return fmt.Sprintf("%s %s %s", dimOsType, regularVersion, grayCodename)
	case "alpine":
		// For Alpine, format as "Alpine Linux X.Y.Z" - but "Linux" should not be bold
		return fmt.Sprintf("%s Linux %s", dimOsType, regularVersion)
	default:
		// Generic format for other OS types
		return fmt.Sprintf("%s %s", dimOsType, regularVersion)
	}
}

// formatHardnVersionLine formats the hardn version line with update information if available
func (m *MainMenu) formatHardnVersionLine(formatter *style.StatusFormatter) string {
	// Create common elements
	hardnBold := style.Bold + "hardn" + style.Reset
	// hardnPad := " " + hardnBold + " "
	// hardnLabel := style.Colored(style.BgGray02, hardnPad)
	currentVersion := "v" + m.versionService.CurrentVersion
	currentVersionPad := " " + currentVersion + " "
	currentVersionDim := style.Dimmed(currentVersionPad)
	currentVersionBg := style.Colored(style.BgGray03, currentVersionDim)
	hardnVersion := hardnBold + " " + currentVersionBg
	repo := style.Dimmed(repoURL)

	// Format differently based on update availability
	if m.updateAvailable {
		latestVersion := "v" + m.latestVersion
		message := latestVersion + " " + "available"
		notification := style.Colored(style.Royal, message)
		return formatter.FormatLine(
			"",
			"",
			hardnVersion,
			notification,
			style.Royal,
			"",
			"no-indent",
		)
	} else {
		return formatter.FormatLine(
			"",
			"",
			hardnVersion,
			repo,
			style.Gray10,
			"",
			"no-indent",
		)
	}
}

// formatHostInfoLines formats host information lines for display
func (m *MainMenu) formatHostInfoLines(hostInfo *model.HostInfo, formatter *style.StatusFormatter) (string, string) {
	// Get IP address if available
	ipAddress := "Unknown"
	if len(hostInfo.IPAddresses) > 0 {
		ipAddress = hostInfo.IPAddresses[0]
	}

	// Format hostname as highlighted label
	hostLabel := style.ColoredLabel(hostInfo.Hostname)

	// Format host info line
	hostLine := formatter.FormatLine("", "", hostLabel, ipAddress, "", "", "no-indent")
	// hostLine := formatter.FormatLine("", "", hostLabel, ipAddress, style.Dim, "", "bold", "no-indent")

	// Format uptime line
	uptime := m.menuManager.FormatUptime(hostInfo.Uptime)
	uptime = style.Dimmed(uptime)
	uptimeLine := formatter.FormatLine("", "", "", uptime, style.Gray10, "", "no-indent")

	return hostLine, uptimeLine
}

// ShowMainMenu displays the main menu and handles user input. The main
// menu is pkg/ui/screen's main-menu layout in its full form (refactor plan
// slice 2b, next/s2b-menu; decision 5 as adopted): the status blocks
// (SetStatus), "Select an option" with the mode indicator, origin/main's
// items (mainMenu), and "Enter selection" with its hint generated from
// them, asked through the one line reader every legacy screen reads
// (prompt.Stdin, the input seam). The screen is cleared once, as hardn
// starts; each pass then reads the status again and draws the main menu
// on the terminal as it is then, under what the item before it printed,
// not cleared (brief 1C), the legacy screens still clearing as they open.
// A line no item takes is refused under the prompt, "Invalid option.
// Please try again.", and asked again, the screen above kept (nav.Ask).
// 0 (Exit) and q clear the screen and return, as they did; the end of the
// input exits 130 and Ctrl-C ends hardn by SIGINT (endHardn).
func (m *MainMenu) ShowMainMenu(currentVersion, buildDate, gitCommit string) {
	// Initialize version service if not already done
	if m.versionService == nil && currentVersion != "" {
		m.versionService = version.NewService(currentVersion, buildDate, gitCommit)
	}

	// Check for updates when the menu starts
	m.CheckForUpdatesWithEnvVars()

	utils.ClearScreen()
	for {
		// Refresh any configuration that might have been changed
		m.refreshConfig()

		// If a security update is available, the alert replaces the main
		// menu: wait for Enter and exit, as origin/main did
		if m.securityUpdateAvailable {
			if m.versionService != nil && m.versionService.CurrentVersion != "" {
				m.displaySecurityUpdateAlert(nil)
			}
			ReadKey()
			utils.ClearScreen()
			return
		}

		switch err := m.pass(context.Background()); {
		case errors.Is(err, errAgain):
		case err == nil, errors.Is(err, nav.ErrQuit):
			utils.ClearScreen()
			return
		default:
			endHardn(err)
			return
		}
	}
}

// pass draws the main menu once, its status read again (readStatus) on
// the terminal as it is now (SetTerminal), and runs what the user
// chooses, through nav.Run, which draws the layout's Screen, asks, refuses
// a line no item takes under the prompt and asks again, and runs the
// item's action. It returns errAgain once an item's screen has returned,
// so that the next pass draws the main menu again; nil for 0, the Exit
// item; nav.ErrQuit for q; and the error that ends hardn otherwise: the
// end of the input or Ctrl-C at its prompt (prompt.ErrAborted), a status
// not read, a write that failed.
func (m *MainMenu) pass(ctx context.Context) error {
	if m.terminal == nil {
		panic("menu: the main menu has no terminal (SetTerminal)")
	}
	env := m.terminal()
	vm, err := m.readStatus(ctx)
	if err != nil {
		return err
	}
	vm.Heading, vm.Mode, vm.Prompt = mainHeading, m.mode(), mainPrompt
	p := nav.Prompter{Ctx: component.NewCtx(env), In: input(), Out: env.Writer(os.Stdout)}
	menu := m.mainMenu()
	drawn := vm.Screen(p.Ctx, menu)
	if legacySecurityStatus {
		// origin/main's block, then the layout from its heading down
		m.showLegacyStatus()
		drawn.Above = component.MenuHeading{Title: vm.Heading, Mode: &vm.Mode}.Render(p.Ctx)
	}
	return p.Run(func() nav.Screen { return drawn })
}

// mode is the mode indicator for the session as it is now (slice 2b,
// decisions 2 and 3): plan only or live, from the configuration every
// legacy screen reads (DryRun), and what set plan only (DryRunSource):
// -n or hardn.yml as hardn started, and nothing for live. Plan only with
// no source, which item 6 and Run All's option 2 set inside the session
// until unit (d) removed them, is still drawn so, "Mode Plan only", should
// anything write DryRun again; nothing in hardn does.
func (m *MainMenu) mode() component.ModeIndicator {
	mode := component.ModeIndicator{DryRun: m.config.DryRun}
	switch m.config.DryRunSource() {
	case config.DryRunFlag:
		mode.Source = component.ModeSourceFlag
	case config.DryRunFile:
		mode.Source = component.ModeSourceFile
	}
	return mode
}

// legacyScreen is one of origin/main's screens as a main-menu item opens
// it: a menu's Show, or a handler's (handled). It returns when the user
// leaves it, or ends hardn itself at the end of the input or on Ctrl-C
// (endHardn).
type legacyScreen interface{ Show() }

// handled is a legacy handler, Run All's, opened as a screen: its
// Handle, as origin/main's handleMenuChoice called it.
type handled struct{ handler interface{ Handle() } }

// Show runs the handler.
func (h handled) Show() { h.handler.Handle() }

// screens build what the main menu's items open: origin/main's screens,
// but the DNS, Backup and Environment items', which are the hooks
// cmd/hardn sets (SetDNS, SetBackup, SetEnvironment). Its environment
// entry, opened by no item since slice 9's unit (d), goes with
// environment_settings_menu.go.
type screens struct {
	users, sshLogin, firewall, runAll, environment, systemDetails, logs func(m *MainMenu) legacyScreen
}

// legacyScreens build each item's screen as origin/main's
// handleMenuChoice built it, the same constructor with the same
// arguments.
var legacyScreens = screens{
	users:       func(m *MainMenu) legacyScreen { return NewUserMenu(m.menuManager, m.config, m.osInfo) },
	sshLogin:    func(m *MainMenu) legacyScreen { return NewDisableRootMenu(m.menuManager, m.config, m.osInfo) },
	firewall:    func(m *MainMenu) legacyScreen { return NewFirewallMenu(m.menuManager, m.config, m.osInfo) },
	runAll:      func(m *MainMenu) legacyScreen { return handled{NewRunAllHandler(m.menuManager, m.config, m.osInfo)} },
	environment: func(m *MainMenu) legacyScreen { return NewEnvironmentSettingsMenu(m.menuManager, m.config) },
	systemDetails: func(m *MainMenu) legacyScreen {
		return NewSystemDetailsMenu(m.config, m.osInfo, m.menuManager.GetHostInfoManager())
	},
	logs: func(m *MainMenu) legacyScreen { return NewLogsMenu(m.menuManager, m.config) },
}

// openScreen opens the screen an item built. A seam for the tests, which
// record the screen each key built, its type and what it was built with,
// in place of opening it.
var openScreen = func(s legacyScreen) { s.Show() }

// mainMenu is the main menu's items, origin/main's unchanged in content,
// labels, keys and descriptions, but item 8's description, "Keep
// HARDN_CONFIG" where origin/main's said "Configure environment variable"
// (refactor plan slice 9, decision 7 (a), P-07 (c)); item 1's parenthesis
// its aside; each bound to the screen it opened (legacyScreens, and the
// DNS, Backup and Environment screens, showDNS, showBackup and
// showEnvironment); its Exit item "0) Exit", "q to quit" (DEV-030). Item 6,
// Dry-Run, is removed and its key left unused (slice 2b, unit (d);
// decision 1): 6 is refused as any key no item has, 7 to 10 keep their
// keys, and the hint, generated from the items, says "[0-5, 7-10 or q]".
// Each screen is built as its item is chosen, as origin/main built it,
// and ends the pass when it returns (errAgain). Under plan only, whatever
// set it, User Management (1), which changes the host and saves
// ./hardn.yml whatever the mode, is refused under the prompt before its
// screen is built, "Not available in plan only.", as a key no item has is
// refused (decision 6); live, it opens as before. Backup (5) and
// Environment (8) open either way: their screens stop each change at its
// plan under plan only (slice 3, decision 10; slice 9,
// gallery-environment.md §5, Made here).
func (m *MainMenu) mainMenu() nav.Menu {
	s := legacyScreens
	open := func(build func(*MainMenu) legacyScreen) func() error {
		return func() error {
			openScreen(build(m))
			return errAgain
		}
	}
	planOnly := func() string {
		if m.config.DryRun {
			return notInPlanOnly
		}
		return ""
	}
	item := func(key, label, description string) component.MenuItem {
		return component.MenuItem{Key: key, Label: label, Description: description}
	}
	return nav.Menu{
		Options: []nav.Option{
			{Item: component.MenuItem{Key: "1", Label: "User Management", Description: "Create, Configure", Aside: "sudo, SSH keys"}, Action: open(s.users), Refusal: planOnly},
			{Item: item("2", "SSH Login", "Toggle SSH root access"), Action: open(s.sshLogin)},
			{Item: item("3", "DNS", "Configure Nameservers"), Action: m.showDNS},
			{Item: item("4", "Firewall", "Configure UFW rules"), Action: open(s.firewall)},
			{Item: item("5", "Backup", "Configure Hardn backup settings"), Action: m.showBackup},
			{Item: item("7", "Run All", "Execute hardening operations"), Action: open(s.runAll)},
			{Item: item("8", "Environment", "Keep HARDN_CONFIG"), Action: m.showEnvironment},
			{Item: item("9", "System Details", "View system information"), Action: open(s.systemDetails)},
			{Item: item("10", "Logs", "View log file"), Action: open(s.logs)},
		},
		Exit: component.MenuItem{Key: "0", Label: "Exit", Description: keys.Set{keys.Quit()}.Help()},
	}
}
