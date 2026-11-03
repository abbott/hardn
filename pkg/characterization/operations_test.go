package characterization

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"

	hardncmd "github.com/abbott/hardn/pkg/cmd"
	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/domain/model"
	"github.com/abbott/hardn/pkg/infrastructure"
	"github.com/abbott/hardn/pkg/interfaces"
	"github.com/abbott/hardn/pkg/osdetect"
)

// Synthetic fixture values. Never real hosts, users or keys.
const (
	fixtureUser = "alice"
	fixturePort = 2230
	// An ed25519-shaped public key line; the key body is not a real key.
	fixtureKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIEXAMPLEKEYEXAMPLEKEYEXAMPLEKEYEXAMPLEKEY0 alice-example"
)

var fixtureNameservers = []string{"192.0.2.53", "192.0.2.54"}

// osCell is one column of the distro matrix.
type osCell struct {
	name     string // golden file stem
	osType   string
	version  string
	codename string
	proxmox  bool
}

func (c osCell) alpine() bool { return c.osType == "alpine" }

// osdetect.DetectOS sets OsCodename to the version on Alpine (osdetect.go:91-93).
var osCells = []osCell{
	{name: "debian", osType: "debian", version: "13", codename: "trixie"},
	{name: "ubuntu", osType: "ubuntu", version: "24.04", codename: "noble"},
	{name: "alpine", osType: "alpine", version: "3.23.0", codename: "3.23.0"},
	{name: "proxmox", osType: "debian", version: "13", codename: "trixie", proxmox: true},
}

// Fixture errors returned by the mock for commands that fail on the fixture host.
var (
	errNoSuchUser   = errors.New("fixture: id: 'alice': no such user")
	errResolvedOff  = errors.New("fixture: systemd-resolved inactive")
	errNoSystemctl  = errors.New("fixture: systemctl not installed")
	errNoResolvconf = errors.New("fixture: resolvconf not installed")
)

// opEnv is one run of one operation on one cell.
type opEnv struct {
	cell    osCell
	cfg     *config.Config
	fs      *interfaces.MockFileSystem
	mc      *interfaces.MockCommander
	cmd     *recordingCommander
	log     *trace
	factory *infrastructure.ServiceFactory
	// fixtureNotes describe host state that the maps alone do not show.
	fixtureNotes []string
	// seed copies the fixture into the seams; runCell calls it after setup.
	// Under viaSystem, fs and mc are inputs only: the hostfake.System behind
	// the recorders answers from them and never writes back, so a test that
	// runs viaSystem asserts on log, not on fs.Files or mc.ExecutedCommands.
	seed func()
}

// fixtureConfig is config.DefaultConfig() with synthetic overrides, then the
// dry-run flag applied the way cmd/hardn/main.go's Run applies -n
// (cfg.ResolveDryRun).
func fixtureConfig(dryRun bool) *config.Config {
	cfg := config.DefaultConfig()
	cfg.Username = fixtureUser
	cfg.SshPort = fixturePort
	cfg.SshAllowedUsers = []string{fixtureUser}
	cfg.SshKeys = []string{fixtureKey}
	cfg.EnableUfwSshPolicy = true
	cfg.ConfigureDns = true
	cfg.Nameservers = append([]string(nil), fixtureNameservers...)
	cfg.UfwAppProfiles = []config.UfwAppProfile{{
		Name:        "ExampleHTTPS",
		Title:       "Example web server (HTTPS)",
		Description: "Example HTTPS port",
		Ports:       []string{"8443/tcp"},
	}}
	cfg.DryRun = dryRun
	return cfg
}

// newOpEnv builds an opEnv whose recorders wrap the seams via selects: at
// most one; none means viaMocks, the seams the goldens are recorded on, which
// tests that assert on fs and mc need (under viaSystem they are inputs only).
func newOpEnv(cell osCell, dryRun bool, via ...seam) *opEnv {
	if len(via) > 1 {
		panic("newOpEnv: at most one seam")
	}
	sel := viaMocks
	if len(via) == 1 {
		sel = via[0]
	}
	fs := interfaces.NewMockFileSystem()
	mc := interfaces.NewMockCommander()
	innerFS, innerCmd, seed := legacySeams(sel, fs, mc)
	log := &trace{}
	rc := &recordingCommander{inner: innerCmd, log: log}
	provider := &interfaces.Provider{
		FS:        recordingFS{inner: innerFS, log: log},
		Commander: rc,
		Network:   interfaces.NewMockNetworkOperations(),
	}
	osInfo := &osdetect.OSInfo{OsType: cell.osType, OsVersion: cell.version, OsCodename: cell.codename, IsProxmox: cell.proxmox}
	cfg := fixtureConfig(dryRun)
	factory := infrastructure.NewServiceFactory(provider, osInfo)
	factory.SetConfig(cfg)
	env := &opEnv{cell: cell, cfg: cfg, fs: fs, mc: mc, cmd: rc, log: log, factory: factory, seed: seed}
	env.defaultHostProbes()
	return env
}

// defaultHostProbes models each distro's default resolver set-up (Phase 0
// findings: Ubuntu uses systemd-resolved; Debian and Proxmox standard installs
// and Alpine use a regular /etc/resolv.conf), and assumes UFW is installed
// everywhere (`which ufw` succeeds).
func (e *opEnv) defaultHostProbes() {
	switch {
	case e.cell.alpine():
		e.mc.CommandErrors["systemctl is-active systemd-resolved"] = errNoSystemctl
	case e.cell.osType == "ubuntu":
		// systemd-resolved active: the mock's default success.
	default:
		e.mc.CommandErrors["systemctl is-active systemd-resolved"] = errResolvedOff
	}
	e.mc.CommandErrors["which resolvconf"] = errNoResolvconf
}

// newUser makes `id alice` fail until `adduser ... alice` has run, as on a real host.
func (e *opEnv) newUser() {
	key := "id " + fixtureUser
	e.mc.CommandErrors[key] = errNoSuchUser
	e.cmd.afterExec = func(argv []string) {
		if argv[0] == "adduser" && argv[len(argv)-1] == fixtureUser {
			delete(e.mc.CommandErrors, key)
		}
	}
	e.fixtureNotes = append(e.fixtureNotes, "user alice does not exist until `adduser ... alice` runs; `id alice` succeeds afterwards")
}

// setDNSStrategy forces SaveDNSConfig's probes (file_dns_repository.go:35-55).
func (e *opEnv) setDNSStrategy(strategy string) {
	delete(e.mc.CommandErrors, "systemctl is-active systemd-resolved")
	delete(e.mc.CommandErrors, "which resolvconf")
	switch strategy {
	case "systemd-resolved":
		e.mc.CommandErrors["which resolvconf"] = errNoResolvconf
	case "resolvconf":
		e.mc.CommandErrors["systemctl is-active systemd-resolved"] = errResolvedOff
	case "direct":
		e.mc.CommandErrors["systemctl is-active systemd-resolved"] = errResolvedOff
		e.mc.CommandErrors["which resolvconf"] = errNoResolvconf
	default:
		panic("unknown DNS strategy " + strategy)
	}
	e.fixtureNotes = append(e.fixtureNotes, "DNS probe results forced to select the "+strategy+" strategy, whatever this distro ships")
}

// hardeningConfigFromCLI mirrors cmd/hardn/main.go's Run, its runAll
// branch (hardn -r).
func hardeningConfigFromCLI(cfg *config.Config) *model.HardeningConfig {
	return &model.HardeningConfig{
		CreateUser:         cfg.Username != "",
		Username:           cfg.Username,
		SudoNoPassword:     cfg.SudoNoPassword,
		SshKeys:            cfg.SshKeys,
		SshPort:            cfg.SshPort,
		SshListenAddresses: []string{cfg.SshListenAddress},
		SshAllowedUsers:    cfg.SshAllowedUsers,
		EnableFirewall:     cfg.EnableUfwSshPolicy,
		AllowedPorts:       []int{},
		FirewallProfiles:   []model.FirewallProfile{},
		ConfigureDns:       cfg.ConfigureDns,
		Nameservers:        cfg.Nameservers,
		EnableAppArmor:     cfg.EnableAppArmor,
		EnableLynis:        cfg.EnableLynis,
	}
}

// callResult is the error returned by one top-level call of a scenario.
type callResult struct {
	call string
	err  error
}

type opScenario struct {
	name  string // golden directory
	op    string // what is called
	entry string // how a user reaches it on origin/main
	// cli lists the command lines that reach it (so V1 applies), each with
	// the operation names cmd/hardn gives pkg/cmd's dry-run refusal.
	cli     []cliRun
	notes   []string
	setup   func(e *opEnv)
	run     func(e *opEnv) []callResult
	defects func(c osCell, o observed) []string
	// gates lists the P0.8 hazard gates' fixes for this operation: each
	// is a FIXED line when the cfg.DryRun=false trace shows the fix, else
	// the KNOWN-DEFECT lines it replaced.
	gates func(c osCell) []gateCheck
	// pveCheck says the operation asks the Proxmox check (proxmoxNote).
	pveCheck bool
	// gateNotes are NOTE lines that hold only while a gate's fix shows;
	// shown says which fixes show, by fixedText id.
	gateNotes func(c osCell, shown map[string]bool) []string
}

// cliRun is one command line that reaches a scenario's operation.
type cliRun struct {
	args string   // after "hardn -n"
	ops  []string // pkg/cmd's names for its operation flags
	at   string   // where cmd/hardn refuses it under -n
}

// Where cmd/hardn/main.go calls pkg/cmd's RefuseDryRunOperations.
const refusedInRoot = "cmd/hardn/main.go, the top of Run (refuseDryRunOperations), before the root check"

// fixedItem is a V-list (or A-) item a P0.8 hazard gate fixed: its id (a
// key of fixedText) and the unexported constant(s) that gate the fix.
type fixedItem struct {
	id   string
	gate string
}

// Gate constants, as the FIXED lines name them.
const (
	gateRunAll = "runAllWithoutConfirmation"
	gatePVE    = "proxmoxTreatedAsDebian"
)

// gateCheck is how a golden shows one P0.8 fix: the FIXED item when the
// cfg.DryRun=false trace shows the fix (shows), else the KNOWN-DEFECT items
// it replaced (known), so that flipping the gate's constant and running
// -update turns the marker back along with the body, as V1's marker turns
// back with pkg/cmd's refusal. A nil shows is a fix this harness cannot
// see (A4, in pkg/menu): its FIXED line is printed whatever the trace.
type gateCheck struct {
	fixed fixedItem
	known []string
	shows func(o observed) bool
}

// observed is what one cfg.DryRun=false trace shows about the gated
// behaviour, read back from the seams' log.
type observed struct {
	ufw      bool       // a ufw command ran (which ufw, ufw ..., yes | ufw enable)
	pveProbe bool       // the Proxmox check probed /etc/pve
	dns      bool       // the DNS step ran (its systemd-resolved probe)
	writes   int        // file writes
	ssh      []sshWrite // writes of sshd_config or hardn.conf, in order
}

// sshWrite is the directives of one sshd config write that the gates
// decide, each a list of the values of its lines, in order.
type sshWrite struct {
	permitRootLogin, allowUsers []string
}

// observe reads t, one cfg.DryRun=false run's trace.
func observe(t *trace) observed {
	var o observed
	for _, e := range t.entries {
		switch {
		case strings.HasPrefix(e.text, "exec   ufw ") || strings.HasPrefix(e.text, "exec   which ufw ") || strings.Contains(e.text, "| ufw enable"):
			o.ufw = true
		case strings.HasPrefix(e.text, "stat   /etc/pve "):
			o.pveProbe = true
		case strings.HasPrefix(e.text, "exec   systemctl is-active systemd-resolved "):
			o.dns = true
		case strings.HasPrefix(e.text, "write  "):
			o.writes++
			path := strings.Fields(e.text)[1]
			if path != "/etc/ssh/sshd_config" && path != "/etc/ssh/sshd_config.d/hardn.conf" {
				continue
			}
			var w sshWrite
			for _, line := range e.content {
				f := strings.Fields(strings.TrimPrefix(line, "|"))
				if len(f) < 2 {
					continue
				}
				value := strings.Join(f[1:], " ")
				switch strings.ToLower(f[0]) {
				case "permitrootlogin":
					w.permitRootLogin = append(w.permitRootLogin, value)
				case "allowusers":
					w.allowUsers = append(w.allowUsers, value)
				}
			}
			o.ssh = append(o.ssh, w)
		}
	}
	return o
}

// every reports whether at least one sshd config was written and each
// write's lines for one directive (pick) are all accepted by ok.
func (o observed) every(pick func(w sshWrite) []string, ok func(values []string) bool) bool {
	if len(o.ssh) == 0 {
		return false
	}
	for _, w := range o.ssh {
		if !ok(pick(w)) {
			return false
		}
	}
	return true
}

func allEqual(want string) func([]string) bool {
	return func(values []string) bool {
		for _, v := range values {
			if v != want {
				return false
			}
		}
		return len(values) > 0
	}
}

// keyOnly: every sshd config write sets PermitRootLogin prohibit-password.
func (o observed) keyOnly() bool {
	return o.every(func(w sshWrite) []string { return w.permitRootLogin }, allEqual("prohibit-password"))
}

// allowUsersWithoutRoot: some sshd config write has an AllowUsers line
// that leaves out root.
func (o observed) allowUsersWithoutRoot() bool {
	for _, w := range o.ssh {
		if len(w.allowUsers) > 0 && !slices.Contains(strings.Fields(w.allowUsers[0]), "root") {
			return true
		}
	}
	return false
}

// Short texts for the V-list (Phase 0 side-effect inventory, V1-V27), for
// the V-numbers a case cites. V20's, cited by setup_sudo_preservation
// alone, went with that case (slice 9, unit (d)); its frozen goldens keep
// it (pkg/screens/environment/testdata/legacy/ops). V5's, cited by
// create_user_c_path alone, went with that case (slice 5, unit (d)), and
// V4's and V11:disable-root's, cited by disable_root_ssh alone, with that
// case (slice 6, unit (d)).
var vText = map[string]string{
	"V1":  "CLI -n/--dry-run is ignored below the menu layer: the cfg.DryRun=true section repeats every command and write",
	"V6":  "password auth disabled and AuthenticationMethods publickey set in the same step that adds keys; nothing proves a key works",
	"V7":  "no sshd -t before the restart and no sshd -T after; restart instead of reload",
	"V8":  "the SSH port changes (sshd restart) before any firewall rule allows the new port",
	"V9":  "ufw default deny, disable and reset run before the allow rule; only cfg.SshPort is allowed, not the effective sshd port",
	"V10": "UFW is configured and enabled on a Proxmox host",
	"V11": "PermitRootLogin no is forced regardless of config",
	"V12": "no ssh.socket handling (socket-activated sshd on Ubuntu), so the port change may not take effect",
	"V13": "the drop-in is named hardn.conf, which sorts after 50-cloud-init.conf (sshd uses the first value)",
	"V14": "Alpine: sudo is never installed; the user is only added to wheel",
	"V15": "Alpine: the whole /etc/ssh/sshd_config is replaced (no Subsystem sftp, no Include)",
	"V16": "sudoers file written without visudo -c and non-atomically; the overwrite drops any env_keep line",
	"V17": "the user is created without a password",
	"V18": "writes are non-atomic and nothing is backed up first",
	"V21": "Alpine: root writes /home/<user>/.ssh/authorized_keys (home path assumed; write follows symlinks)",
	"V22": "the key is appended again with no dedupe (cat >>) on every run for an existing user",
	"V24": "a single AuthorizedKeysFile drops v0.3.2's ~/.ssh_<user>/authorized_keys, so keys on upgraded hosts stop working",
	"V27": "ListenAddress 0.0.0.0 makes sshd IPv4-only",

	// Variants: one V-number, wording specific to the recorded behaviour.
	"V11:proxmox":          "PermitRootLogin no is written on a Proxmox host, where root SSH is to stay key-only (prohibit-password)",
	"V11:config":           "the root-login value is forced regardless of permitRootLogin/disableRootSSH in the config (§3.17)",
	"V11:allowusers":       "on Proxmox the AllowUsers line written from sshAllowedUsers leaves out root, so sshd refuses root's key login, which clustering uses, whatever PermitRootLogin says (whether hardn adds root there is p2-pve's call)",
	"V19:systemd-resolved": "DNS: /etc/systemd/resolved.conf overwritten wholesale and systemd-resolved restarted; Domains=lan forced",
	"V19:resolvconf":       "DNS: /etc/resolvconf/resolv.conf.d/head overwritten; domain and search lan forced",
	"V19:direct":           "DNS: /etc/resolv.conf overwritten in place (a write through a symlink lands in its target); domain and search lan forced",
	"V19:empty":            "DNS: an empty nameserver list is accepted and written",
}

// dnsStrategyFor returns the strategy defaultHostProbes selects for a cell.
func dnsStrategyFor(c osCell) string {
	if c.osType == "ubuntu" {
		return "systemd-resolved"
	}
	return "direct"
}

func sshWriteDefects(c osCell) []string {
	var d []string
	if c.alpine() {
		d = append(d, "V15")
	} else {
		d = append(d, "V13")
	}
	if c.osType == "ubuntu" {
		d = append(d, "V12")
	}
	return d
}

func newUserDefects(c osCell) []string {
	d := []string{"V16", "V17"}
	if c.alpine() {
		d = append(d, "V14", "V21")
	}
	return d
}

// fixedText says what a P0.8 hazard gate does instead, for each fixed item.
var fixedText = map[string]string{
	"V1":          "hardn -n refuses every operation flag with exit status 2 before the root check, so nothing runs; the cfg.DryRun=true section is what the layers below the menus still do with DryRun set (A1), which no CLI flag reaches now",
	"V9:proxmox":  "on Proxmox no ufw command runs at all, so there is no default deny, disable or reset",
	"V10":         "on Proxmox, configuring or enabling UFW is refused with an error before any ufw command runs (pve-firewall manages the packet filter; brief §2)",
	"V11:proxmox": "on Proxmox, PermitRootLogin prohibit-password (key-only) is written where origin/main wrote no (brief §2)",
	"A4":          "Run All option 1 asks for a typed 'yes' before a live run, and any other answer cancels with nothing applied; hardn -r does not ask. Not observed by this harness: pkg/menu's TestRunAllConfirmation and run_all/option1-live-cancel.golden record it",
}

// proxmoxUFWGates is the Proxmox gate's fix for a UFW operation.
func proxmoxUFWGates(c osCell) []gateCheck {
	if !c.proxmox {
		return nil
	}
	noUFW := func(o observed) bool { return !o.ufw }
	return []gateCheck{
		{fixedItem{"V9:proxmox", gatePVE}, []string{"V9"}, noUFW},
		{fixedItem{"V10", gatePVE}, []string{"V10"}, noUFW},
	}
}

// proxmoxRootLoginGate is the Proxmox gate's fix where an operation writes
// PermitRootLogin no elsewhere.
func proxmoxRootLoginGate(c osCell) []gateCheck {
	if !c.proxmox {
		return nil
	}
	return []gateCheck{{fixedItem{"V11:proxmox", gatePVE}, []string{"V11:proxmox"}, observed.keyOnly}}
}

// proxmoxAllowUsersDefect is V11's AllowUsers half on the proxmox cell.
func proxmoxAllowUsersDefect(c osCell, o observed) []string {
	if c.proxmox && o.allowUsersWithoutRoot() {
		return []string{"V11:allowusers"}
	}
	return nil
}

var proxmoxNote = "Proxmox check (pkg/adapter/secondary/proxmox.go), once per ServiceFactory, before the first SSH root-login or UFW decision: osdetect's IsProxmox (set on the proxmox cell, so it probes nothing), else /etc/pve, else dpkg-query's pve-manager status, which must read 'install ok installed' (the mock's empty output reads as not installed; Alpine is not asked)."

// proxmoxOffNote replaces proxmoxNote on the proxmox cell when the body
// shows none of the Proxmox gate's fixes.
var proxmoxOffNote = "Proxmox check: none of its rules shows in the body, so this Proxmox host gets Debian's steps (proxmoxTreatedAsDebian set)."

// dryRunNote is the NOTE on cfg.DryRun; refused says whether pkg/cmd
// refuses an operation flag under -n (V1's gate).
func dryRunNote(refused bool) string {
	reach := ""
	if refused {
		reach = " (which now reaches only the menus)"
	}
	return "cfg.DryRun is set on the config passed to ServiceFactory.SetConfig, as cmd/hardn/main.go's Run does for -n" + reach + ". No layer below pkg/menu reads it."
}

// gatedHeader is a golden's first line when it holds FIXED lines naming
// gates distinct constants (and baselineHeader when it holds none).
func gatedHeader(gates int) string {
	switch gates {
	case 0:
		return baselineHeader
	case 1:
		return baselineHeader + ", with the P0.8 hazard gate below"
	}
	return baselineHeader + ", with the P0.8 hazard gates below"
}

func opScenarios() []opScenario {
	return []opScenario{
		{
			name:  "harden_system",
			op:    "MenuManager.HardenSystem -> SecurityManager.HardenSystem (user, SSH, UFW, DNS)",
			entry: "hardn -r (cmd/hardn/main.go, Run's runAll branch, menuManager.HardenSystem); menu Run All option 1 makes the same call with AllowedPorts=cfg.UfwAllowedPorts (pkg/menu/run_all_menu.go:226-268), in live mode only after a typed 'yes' (run_all_menu.go:155)",
			cli:   []cliRun{{"-r", []string{hardncmd.FlagRunAll}, refusedInRoot}},
			notes: []string{
				"HardeningConfig is built as cmd/hardn/main.go's Run builds it in its runAll branch; SshKeyPaths is left empty, so the adapter writes its default AuthorizedKeysFile.",
			},
			pveCheck: true,
			gateNotes: func(c osCell, shown map[string]bool) []string {
				if c.proxmox && shown["V10"] {
					return []string{"On Proxmox the refused UFW step ends HardenSystem with its error, so the DNS step after it does not run (Phase 2's p2-pve is to report pve-firewall instead)."}
				}
				return nil
			},
			setup: func(e *opEnv) { e.newUser() },
			run: func(e *opEnv) []callResult {
				err := e.factory.CreateMenuManager().HardenSystem(hardeningConfigFromCLI(e.cfg))
				return []callResult{{"MenuManager.HardenSystem", err}}
			},
			defects: func(c osCell, o observed) []string {
				d := []string{"V6", "V7", "V8", "V18", "V24", "V27"}
				if c.proxmox {
					d = append(d, "V11:config")
				} else {
					d = append(d, "V9", "V11")
				}
				if o.dns {
					d = append(d, "V19:"+dnsStrategyFor(c))
				}
				d = append(d, proxmoxAllowUsersDefect(c, o)...)
				d = append(d, sshWriteDefects(c)...)
				d = append(d, newUserDefects(c)...)
				return d
			},
			gates: func(c osCell) []gateCheck {
				g := []gateCheck{{fixed: fixedItem{"A4", gateRunAll}}}
				g = append(g, proxmoxUFWGates(c)...)
				return append(g, proxmoxRootLoginGate(c)...)
			},
		},
		// disable_root_ssh, origin/main's hardn -d and SSH Login menu
		// (SSHManager.DisableRootSSH), is retired (refactor plan slice 6,
		// unit (d), next/s6-replace): -d and main menu 2 run the SSH Login
		// control, so nothing reaches that call now. Its four goldens are
		// deleted, not frozen (the maintainer, 2026-11-03: hard cutover), and
		// with them the stubDisableRoot gate and the fixedText lines (V4,
		// V11:, V12:, V15: and V24:disable-root) that case alone cited.
		//
		// create_user_c_path, origin/main's hardn -c (UserManager.CreateUser,
		// then SSHManager.ConfigureSSH with pkg/cmd's
		// CreateUserAuthorizedKeysFiles), is retired (refactor plan slice 5,
		// unit (d), next/s5-replace): -c is the User control's Create, which
		// configures no sshd, so nothing reaches that pair now. Its four
		// goldens are deleted, not frozen (the maintainer, 2026-11-03: hard
		// cutover), and with them the V5 and V24:c-path lines that case alone
		// cited.
		{
			name: "configure_secure_firewall",
			op:   "FirewallManager.ConfigureSecureFirewall(cfg.SshPort, no extra ports, no profiles)",
			// What still reaches it once slice 4's unit (d) puts hardn -w and
			// the Firewall screen on the Firewall control: the firewall step
			// of SecurityManager.HardenSystem, which hardn -r and Run All's
			// live run call, until slice 8. Its goldens as they were, -w's,
			// are frozen in pkg/screens/firewall/testdata/legacy/ops.
			entry: "hardn -r (cmd/hardn/main.go, Run's runAll branch, menuManager.HardenSystem) and Run All's live run (pkg/menu/run_all_menu.go:228), through SecurityManager.HardenSystem's firewall step (pkg/application/security_manager.go:58); hardn -w and the Firewall screen move to the Firewall control in slice 4 unit (d)",
			cli:   []cliRun{{"-r", []string{hardncmd.FlagRunAll}, refusedInRoot}},
			run: func(e *opEnv) []callResult {
				err := e.factory.CreateFirewallManager().ConfigureSecureFirewall(e.cfg.SshPort, []int{}, []model.FirewallProfile{})
				return []callResult{{"FirewallManager.ConfigureSecureFirewall", err}}
			},
			pveCheck: true,
			defects: func(c osCell, o observed) []string {
				if c.proxmox {
					return nil
				}
				return []string{"V9"}
			},
			gates: proxmoxUFWGates,
		},
		// configure_secure_firewall_profiles, origin/main's Firewall menu
		// item 2 with cfg.UfwAppProfiles (MenuManager.ConfigureSecureFirewall),
		// is retired (refactor plan slice 4, unit (d), next/s4-replace): the
		// unit replaces the Firewall menu with the Firewall screen, and the
		// menu was that method's only caller. Its four goldens moved
		// unchanged to pkg/screens/firewall/testdata/legacy/ops, Firewall's
		// parity record (docs/engine/parity-firewall.md §8).
		{
			name: "configure_sudo",
			op:   "UserManager.CreateUser for an existing user -> OSUserRepository.ConfigureSudo, then AddSSHKey",
			// What still reaches it once slice 5's unit (d) puts hardn -c and
			// the User Management screen on the User control: the user step of
			// SecurityManager.HardenSystem, which hardn -r and Run All's live run
			// call for an existing username, until slice 8.
			entry: "hardn -r (cmd/hardn/main.go, Run's runAll branch, menuManager.HardenSystem) and Run All's live run (pkg/menu/run_all_menu.go:228) when alice exists, through SecurityManager.HardenSystem's user step (pkg/application/security_manager.go:34); hardn -c and the User Management screen move to the User control in slice 5 unit (d)",
			cli:   []cliRun{{"-r", []string{hardncmd.FlagRunAll}, refusedInRoot}},
			notes: []string{"Seed: /etc/sudoers.d/alice holds the env_keep line that setup-sudo-env writes, and alice's authorized_keys already holds the fixture key."},
			setup: func(e *opEnv) {
				e.fs.Directories["/etc/sudoers.d"] = true
				e.fs.Files["/etc/sudoers.d/alice"] = []byte("Defaults:alice env_keep += \"HARDN_CONFIG\"\n")
				e.fs.Files["/home/alice/.ssh/authorized_keys"] = []byte(fixtureKey + "\n")
				e.fixtureNotes = append(e.fixtureNotes, "user alice exists (`id alice` succeeds)")
			},
			run: func(e *opEnv) []callResult {
				err := e.factory.CreateUserManager().CreateUser(e.cfg.Username, true, e.cfg.SudoNoPassword, e.cfg.SshKeys)
				return []callResult{{"UserManager.CreateUser", err}}
			},
			defects: func(c osCell, o observed) []string {
				d := []string{"V16", "V18"}
				if c.alpine() {
					d = append(d, "V21")
				} else {
					d = append(d, "V22")
				}
				return d
			},
		},
		// setup_sudo_preservation, origin/main's -e and setup-sudo-env
		// (EnvironmentManager.SetupSudoPreservation), is retired (refactor
		// plan slice 9, unit (d), next/s9-replace): the unit puts -e and
		// setup-sudo-env on the Environment control and deletes the legacy
		// service, which then has no caller. Its four goldens moved
		// unchanged to pkg/screens/environment/testdata/legacy/ops,
		// Environment's parity record (docs/engine/parity-environment.md §8).
		dnsScenario("systemd-resolved"),
		dnsScenario("resolvconf"),
		dnsScenario("direct"),
		{
			name:  "dns_empty_nameservers",
			op:    "DNSManager.ConfigureDNS(no nameservers, \"lan\")",
			entry: dnsReach + ", with configureDns set and no nameservers in the config" + dnsMoved,
			cli:   []cliRun{{"-r", []string{hardncmd.FlagRunAll}, refusedInRoot}},
			notes: []string{"Resolver set-up as defaultHostProbes: systemd-resolved on Ubuntu, a regular /etc/resolv.conf elsewhere."},
			setup: func(e *opEnv) { e.cfg.Nameservers = nil },
			run: func(e *opEnv) []callResult {
				return []callResult{{"DNSManager.ConfigureDNS", e.factory.CreateDNSManager().ConfigureDNS(e.cfg.Nameservers, "lan")}}
			},
			defects: func(c osCell, o observed) []string {
				return []string{"V18", "V19:empty", "V19:" + dnsStrategyFor(c)}
			},
		},
	}
}

// What reaches the legacy DNS service since slice 2's unit (d) moved -g to
// the DNS control and replaced the DNS menu with the DNS screen: the DNS
// step of SecurityManager.HardenSystem, which hardn -r and Run All's live
// run call, until slice 8 (dnsReach, then the scenario's condition, then
// dnsMoved).
const (
	dnsReach = "hardn -r (cmd/hardn/main.go, Run's runAll branch, menuManager.HardenSystem) and Run All's live run (pkg/menu/run_all_menu.go:268), through SecurityManager.HardenSystem's DNS step (pkg/application/security_manager.go:68-75)"
	dnsMoved = "; hardn -g and the DNS screen use the DNS control since slice 2 unit (d)"
)

func dnsScenario(strategy string) opScenario {
	return opScenario{
		name:  "dns_" + strings.ReplaceAll(strategy, "-", "_"),
		op:    "DNSManager.ConfigureDNS(cfg.Nameservers, \"lan\") -> FileDNSRepository.SaveDNSConfig, " + strategy + " strategy",
		entry: dnsReach + dnsMoved,
		cli:   []cliRun{{"-r", []string{hardncmd.FlagRunAll}, refusedInRoot}},
		setup: func(e *opEnv) { e.setDNSStrategy(strategy) },
		run: func(e *opEnv) []callResult {
			return []callResult{{"DNSManager.ConfigureDNS", e.factory.CreateDNSManager().ConfigureDNS(e.cfg.Nameservers, "lan")}}
		},
		defects: func(c osCell, o observed) []string { return []string{"V18", "V19:" + strategy} },
	}
}

// markerLines renders a KNOWN-DEFECT line for each id in known (texts in
// vText) and a FIXED line for each fixed item (texts in fixedText), in
// V-number order, then the A- items of the verification pass.
func markerLines(known []string, fixed []fixedItem) []string {
	type item struct {
		rank, n int
		line    string
	}
	var items []item
	seen := map[string]string{} // id -> prefix
	add := func(id, text, prefix string) {
		if p, ok := seen[id]; ok {
			if p != prefix {
				panic(id + " is both fixed and a known defect")
			}
			return
		}
		seen[id] = prefix
		if text == "" {
			panic("no text for " + id)
		}
		base, _, _ := strings.Cut(id, ":")
		rank := 0
		if strings.HasPrefix(base, "A") {
			rank = 1
		}
		n, err := strconv.Atoi(base[1:])
		if err != nil || (base[0] != 'V' && base[0] != 'A') {
			panic("bad V- or A-number " + id)
		}
		items = append(items, item{rank, n, fmt.Sprintf("# %s: %s — %s", prefix, base, text)})
	}
	for _, f := range fixed {
		add(f.id, fixedText[f.id], "FIXED (P0.8, gated by "+f.gate+")")
	}
	for _, id := range known {
		add(id, vText[id], "KNOWN-DEFECT")
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].rank != items[j].rank {
			return items[i].rank < items[j].rank
		}
		return items[i].n < items[j].n
	})
	lines := make([]string, len(items))
	for i, it := range items {
		lines[i] = it.line
	}
	return lines
}

// runCell runs one scenario on one cell with one DryRun value and returns
// the section text and what its trace shows.
func runCell(t *testing.T, sc opScenario, cell osCell, dryRun bool, via seam) (fixture, section string, o observed) {
	t.Helper()
	e := newOpEnv(cell, dryRun, via)
	if sc.setup != nil {
		sc.setup(e)
	}
	e.seed()
	fixture = describeFixture(e)
	results := sc.run(e)

	var b strings.Builder
	for _, r := range results {
		fmt.Fprintf(&b, "result %s: %s\n", r.call, errText(r.err))
	}
	b.WriteString("trace:\n")
	b.WriteString(e.log.String())
	return fixture, b.String(), observe(e.log)
}

func describeFixture(e *opEnv) string {
	var b strings.Builder
	cfg := e.cfg
	fmt.Fprintf(&b, "host: os=%s version=%s codename=%s proxmox=%t\n", e.cell.osType, e.cell.version, e.cell.codename, e.cell.proxmox)
	fmt.Fprintf(&b, "config (DefaultConfig plus overrides): username=%s sshPort=%d sshListenAddress=%s sshAllowedUsers=%v permitRootLogin=%t sshKeyPath=%s sudoNoPassword=%t enableUfwSshPolicy=%t configureDns=%t nameservers=%v ufwAppProfiles=%d\n",
		cfg.Username, cfg.SshPort, cfg.SshListenAddress, cfg.SshAllowedUsers, cfg.PermitRootLogin, cfg.SshKeyPath, cfg.SudoNoPassword, cfg.EnableUfwSshPolicy, cfg.ConfigureDns, cfg.Nameservers, len(cfg.UfwAppProfiles))
	fmt.Fprintf(&b, "config sshKeys: %v\n", cfg.SshKeys)

	b.WriteString("commands that fail on this host (every other command succeeds with empty output):\n")
	var failing []string
	for k, v := range e.mc.CommandErrors {
		failing = append(failing, fmt.Sprintf("  %s -> %v", k, v))
	}
	sort.Strings(failing)
	if len(failing) == 0 {
		b.WriteString("  (none)\n")
	}
	for _, f := range failing {
		b.WriteString(f + "\n")
	}
	for _, n := range e.fixtureNotes {
		fmt.Fprintf(&b, "note: %s\n", n)
	}

	var dirs []string
	for d := range e.fs.Directories {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)
	for _, d := range dirs {
		fmt.Fprintf(&b, "seed dir: %s\n", d)
	}
	var files []string
	for f := range e.fs.Files {
		files = append(files, f)
	}
	sort.Strings(files)
	for _, f := range files {
		fmt.Fprintf(&b, "seed file: %s\n", f)
		seed := &trace{entries: []traceEntry{{}}}
		seed.addContent(e.fs.Files[f])
		for _, c := range seed.entries[0].content {
			b.WriteString("        " + c + "\n")
		}
	}
	return b.String()
}

// TestOperationGoldens records origin/main's commands and file writes for each
// operation, distro cell and DryRun value, with the P0.8 hazard gates as the
// code now has them: each gate's FIXED or KNOWN-DEFECT line follows what the
// body shows (gateCheck).
func TestOperationGoldens(t *testing.T) {
	testOperationGoldens(t, viaMocks)
}

// TestOperationGoldensThroughSystem runs every scenario again through the
// legacy interfaces' adapters over host.System (viaSystem) and compares with
// the same goldens. It never rewrites them: -update regenerates from the mocks.
func TestOperationGoldensThroughSystem(t *testing.T) {
	if *update {
		t.Skip("compare-only; -update regenerates the goldens through TestOperationGoldens")
	}
	testOperationGoldens(t, viaSystem)
}

func testOperationGoldens(t *testing.T, via seam) {
	// SetupSudoPreservation read SUDO_USER/USER (file_environment_repository.go:135-138)
	// and os.TempDir(), in the setup_sudo_preservation case, now retired; the
	// environment stays pinned as the goldens were recorded, so that they
	// match on every machine.
	t.Setenv("SUDO_USER", fixtureUser)
	t.Setenv("USER", "root")
	t.Setenv("HARDN_CONFIG", "")
	t.Setenv("TMPDIR", "")
	if err := os.Unsetenv("TMPDIR"); err != nil {
		t.Fatal(err)
	}

	// Whether pkg/cmd refuses an operation flag under -n (V1's gate).
	refused := hardncmd.RefuseDryRunOperations(true, []string{hardncmd.FlagRunAll}) != nil

	for _, sc := range opScenarios() {
		for _, cell := range osCells {
			t.Run(sc.name+"/"+cell.name, func(t *testing.T) {
				fixture, apply, o := runCell(t, sc, cell, false, via)
				fixtureDry, dry, _ := runCell(t, sc, cell, true, via)
				if fixture != fixtureDry {
					t.Fatalf("fixture differs between DryRun runs:\n%s\n---\n%s", fixture, fixtureDry)
				}

				// hardn -n with the scenario's flags, modelled: pkg/cmd's
				// refusal, which cmd/hardn/main.go calls first, before
				// anything else. cmd/hardn's TestDryRunRefusesOperations
				// drives the real command and shows that nothing reaches the
				// provider; this harness does not run cmd/hardn.
				var cliSections strings.Builder
				var fixed []fixedItem
				known := sc.defects(cell, o)
				for _, run := range sc.cli {
					fmt.Fprintf(&cliSections, "\n== hardn -n %s (modelled from pkg/cmd RefuseDryRunOperations, called at %s; cmd/hardn TestDryRunRefusesOperations runs the real command)\n", run.args, run.at)
					if err := hardncmd.RefuseDryRunOperations(true, run.ops); err != nil {
						fmt.Fprintf(&cliSections, "refused: exit status %d\nstderr: %s\ntrace: not recorded here; TestDryRunRefusesOperations checks that no call reaches the provider\n", hardncmd.ExitUsage, err)
						fixed = append(fixed, fixedItem{"V1", "dryRunFlagApplies"})
					} else {
						cliSections.WriteString("not refused: the operation runs as the cfg.DryRun=true section shows\n")
						known = append([]string{"V1"}, known...)
					}
				}

				// Each gate's fix is FIXED only where the body shows it.
				shown := map[string]bool{}
				pveShown := false
				if sc.gates != nil {
					for _, g := range sc.gates(cell) {
						if g.shows != nil && !g.shows(o) {
							known = append(known, g.known...)
							continue
						}
						fixed = append(fixed, g.fixed)
						shown[g.fixed.id] = true
						pveShown = pveShown || g.fixed.gate == gatePVE
					}
				}
				gates := map[string]bool{}
				for _, f := range fixed {
					gates[f.gate] = true
				}

				var b strings.Builder
				b.WriteString(gatedHeader(len(gates)) + "\n")
				b.WriteString("# Generated by TestOperationGoldens (pkg/characterization); regenerate with -update. Do not edit.\n")
				fmt.Fprintf(&b, "# Operation: %s\n", sc.op)
				fmt.Fprintf(&b, "# Reached by: %s\n", sc.entry)
				for _, l := range markerLines(known, fixed) {
					b.WriteString(l + "\n")
				}
				notes := append([]string{dryRunNote(refused)}, sc.notes...)
				if sc.gateNotes != nil {
					notes = append(notes, sc.gateNotes(cell, shown)...)
				}
				switch {
				case !sc.pveCheck:
				case o.pveProbe || pveShown:
					notes = append(notes, proxmoxNote)
				case cell.proxmox:
					notes = append(notes, proxmoxOffNote)
				}
				for _, n := range notes {
					fmt.Fprintf(&b, "# NOTE: %s\n", n)
				}
				b.WriteString("\n== fixture\n")
				b.WriteString(fixture)
				b.WriteString("\n== cfg.DryRun=false\n")
				b.WriteString(apply)
				b.WriteString("\n== cfg.DryRun=true\n")
				b.WriteString(dry)
				fmt.Fprintf(&b, "\ncfg.DryRun=true section identical to cfg.DryRun=false: %t\n", apply == dry)
				b.WriteString(cliSections.String())

				assertGolden(t, "ops/"+sc.name+"/"+cell.name+".golden", b.String())
			})
		}
	}
}
