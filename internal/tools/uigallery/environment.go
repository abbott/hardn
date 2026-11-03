package main

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/controls/environment"
	probe "github.com/abbott/hardn/pkg/engine/header"
	"github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/host/sudoers"
	"github.com/abbott/hardn/pkg/output"
	envscreen "github.com/abbott/hardn/pkg/screens/environment"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/screen"
)

// The Environment screen on the simulated hosts (refactor plan slice 9,
// unit (c), next/s9-screen): hardn's Environment screen itself
// (pkg/screens/environment), drawn from each host's elevation files and
// hardn's environment there through the Environment control, its one
// change on the real engine, on the hosts the Environment mocks were drawn
// for, which it replaces (docs/engine/screen-environment.md). The main
// menu's 8, "Keep HARDN_CONFIG", leads to it on Debian 13, as hardn's main
// menu 8 runs it since next/s9-replace, under plan only too (-sequence
// dry-run); "play environment" is the menu of the hosts and "play
// environment/<host>" one. The listing of the mocks,
// "uigallery environment", was retired with them: git keeps it
// (environment.go, next/s9-parity), and gallery-environment.md records
// what each showed.

// envPrefix names the Environment screen's hosts in play: "environment"
// is the menu of them and "environment/<host>" one.
const envPrefix = "environment"

// envMainHost is the host the main menu's 8 leads to: Debian 13, as the
// mocks' first host.
const envMainHost = envPrefix + "/debian13"

// envLinks lead from a menu mock's item to the Environment screen on a
// host, by the mock's name and the item's key, beside links: the main
// menu's 8, which said the gallery had no screen for it until this unit.
var envLinks = map[string]string{mainMenuMock + "/8": envMainHost}

// hardnConfig is the one variable the screen keeps (P-07 (c)).
const hardnConfig = environment.Variable

// envHost is one simulated host the Environment screen runs on.
type envHost struct {
	// name is the host's, after envPrefix: "debian13".
	name string
	// label and about are its item in the menu of the hosts.
	label, about string
	// note says what the host is.
	note string
	// host returns the simulated host, fresh.
	host func() *fakeHost
	// env is hardn's environment there, what the screen reads of it:
	// SUDO_USER or DOAS_USER, USER, HARDN_CONFIG.
	env map[string]string
}

// The environments hardn runs in: alice's sudo or doas, HARDN_CONFIG
// dropped by it or kept; root's login.
var (
	sudoAlice = map[string]string{"SUDO_USER": "alice", "USER": "root"}
	doasAlice = map[string]string{"DOAS_USER": "alice", "USER": "root"}
	keptAlice = map[string]string{"SUDO_USER": "alice", "USER": "root", hardnConfig: "/home/alice/hardn.yml"}
	doasKept  = map[string]string{"DOAS_USER": "alice", "USER": "root", hardnConfig: "/home/alice/hardn.yml"}
	rootLogin = map[string]string{"USER": "root"}
)

// envHosts are the hosts, in the menu's order, each drawing the options
// the maintainer adopted (gallery-environment.md §5): the mocks' ten.
func envHosts() []envHost {
	const (
		deb    = hostFQDN + " runs Debian 13, sudo 1.9.16p2. "
		viaSud = "alice ran sudo hardn; "
		drop   = "sudo dropped HARDN_CONFIG."
		setup  = hostFQDN + " runs Alpine 3.24 after setup-alpine, doas; alice is in wheel. alice ran doas hardn. "
		wheel  = setup + "/etc/doas.d/20-wheel.conf says permit persist :wheel. "
		cloud  = hostFQDN + " runs Alpine 3.23's cloud image, doas, as the maintainer's VM; alice is in wheel, and the image's /etc/doas.d/wheel.conf says permit nopass :wheel. alice ran doas hardn. "
	)
	return []envHost{
		{name: "debian13", label: "Debian 13", about: "sudo, not kept", host: named(newHost), env: sudoAlice,
			note: deb + viaSud + drop},
		{name: "ubuntu2604", label: "Ubuntu 26.04", about: "sudo-rs, not kept", host: named(socketHost), env: sudoAlice,
			note: hostFQDN + " runs Ubuntu 26.04, sudo-rs 0.2.13. " + viaSud + drop},
		{name: "alpine324", label: "Alpine 3.24", about: "doas through wheel", host: named(alpineWheel), env: doasAlice,
			note: wheel + "hardn has no rule for alice, so item 1 refuses before asking."},
		{name: "alpine324-setenv", label: "Alpine, setenv on wheel", about: "The refusal followed", host: named(wheelSetenv), env: doasKept,
			note: setup + "alice did what item 1's refusal says: 20-wheel.conf says permit persist setenv { HARDN_CONFIG } :wheel, and doas kept /home/alice/hardn.yml. hardn has no rule for alice, so the screen says Not kept by hardn and item 1 refuses again, as its refusal said."},
		{name: "doas-overridden", label: "doas, overridden", about: "hardn's rule, zz-local.conf after", host: named(doasOverridden), env: doasAlice,
			note: cloud + "A local /etc/doas.d/zz-local.conf says permit nopass :wheel too. hardn's rule for alice keeps HARDN_CONFIG, but zz-local.conf's wins, so doas dropped it. Stopping rewrites a rule that asks for alice's password."},
		{name: "pve9", label: "Proxmox VE 9", about: "no sudo", host: named(func() *fakeHost { return simulate(pve9Platform) }), env: rootLogin,
			note: hostFQDN + " is a Proxmox VE 9 node: no sudo, no doas. root logged in."},
		{name: "kept", label: "Kept", about: "hardn-env-alice there", host: named(keptHost), env: keptAlice,
			note: deb + "hardn's /etc/sudoers.d/hardn-env-alice keeps HARDN_CONFIG; " + viaSud + "it kept /home/alice/hardn.yml."},
		{name: "root-login", label: "Root login", about: "Not run with sudo", host: named(newHost), env: rootLogin,
			note: deb + "root logged in and ran hardn: no SUDO_USER."},
		{name: "grant", label: "hardn's grant", about: "hardn-grant-alice there", host: named(grantHost), env: sudoAlice,
			note: deb + "hardn's /etc/sudoers.d/hardn-grant-alice lets alice use sudo without a password; " + viaSud + drop},
		{name: "legacy", label: "Legacy file", about: "/etc/sudoers.d/alice", host: named(legacyHost), env: keptAlice,
			note: deb + "origin/main's /etc/sudoers.d/alice holds alice's grant and its env_keep line; " + viaSud + "it kept /home/alice/hardn.yml."},
	}
}

// findEnvHost returns the host named name, after envPrefix.
func findEnvHost(name string) (envHost, bool) {
	i := slices.IndexFunc(envHosts(), func(h envHost) bool { return h.name == name })
	if i < 0 {
		return envHost{}, false
	}
	return envHosts()[i], true
}

// named returns host with the kernel's hostname, the simulated host's
// short name, as hostname(1) prints it: the phrase the screen asks a
// lockout-risk change to be confirmed by (probe.HostnameFile).
func named(host func() *fakeHost) func() *fakeHost {
	return func() *fakeHost {
		h := host()
		h.f.AddFile(probe.HostnameFile, []byte(hostName+"\n"), 0o644, 0, 0)
		return h
	}
}

// withDoasFiles gives h doas's configuration as Alpine's package installs
// it, /etc/doas.conf 0640 with its rule commented out and /etc/doas.d 0750
// (sudoers.md §3: aports 3.24-stable's APKBUILD and doas.conf; on the 3.23
// cloud image, /etc/doas.conf's content was not read [I]), and the
// drop-in name holding rule (the model's mode, 0640 [I]).
func withDoasFiles(h *fakeHost, name, rule string) *fakeHost {
	h.f.AddFile(sudoers.DoasConfig, []byte("# Uncomment to allow group \"wheel\" to become root.\n# permit persist :wheel\n"), 0o640, 0, 0)
	h.f.AddDir(sudoers.DoasDir, 0o750, 0, 0)
	h.f.AddFile(sudoers.DoasDir+"/"+name, []byte(rule), 0o640, 0, 0)
	return h
}

// alpineWheel is Alpine 3.24 after setup-alpine: 20-wheel.conf, "permit
// persist :wheel" (setup-user, sudoers.md §3; the release aports'
// 3.24-stable sources cover).
func alpineWheel() *fakeHost {
	return withDoasFiles(simulate(alpine324Platform), "20-wheel.conf", "permit persist :wheel\n")
}

// wheelSetenv is alpineWheel after its user did what item 1's refusal
// says (the control's): setenv on 20-wheel.conf's rule, a group's, which
// keeps the variable for every member (doas.conf(5), sudoers.md:94 [I]:
// doas's setenv has run on no host).
func wheelSetenv() *fakeHost {
	return withDoasFiles(simulate(alpine324Platform), "20-wheel.conf", "permit persist setenv { HARDN_CONFIG } :wheel\n")
}

// alpineCloud is Alpine 3.23's cloud image as the maintainer's VM: only
// wheel.conf, "permit nopass :wheel" (docs/safety/vmtest.md:562, :570),
// which ships in the image (:580) and sorts before zz-hardn-<stem>.conf.
func alpineCloud() *fakeHost {
	return withDoasFiles(simulate(alpine323CloudPlatform), "wheel.conf", "permit nopass :wheel\n")
}

// withHardnFiles writes hardn's files for pol on h as sudoers.PlanFor
// plans them, so that their bytes are the package's own.
func withHardnFiles(h *fakeHost, pol sudoers.Policy) *fakeHost {
	ctx := context.Background()
	f, err := detect.Detect(ctx, h.system())
	if err != nil {
		panic(err) // not reached: the simulated hosts are detected (TestEnvironmentScreenHosts)
	}
	st, err := sudoers.Read(ctx, h.system(), sudoers.Choose(f, sudoers.ToolUnknown), []string{pol.User})
	if err != nil {
		panic(err)
	}
	p, err := sudoers.PlanFor(st, []sudoers.Policy{pol})
	if err != nil {
		panic(err)
	}
	for _, c := range p.Changes {
		h.f.AddFile(c.Path, c.Data, c.Mode, 0, 0)
	}
	return h
}

// keptHost is Debian 13 with hardn's env_keep file for alice.
func keptHost() *fakeHost {
	return withHardnFiles(newHost(), sudoers.Policy{User: "alice", Env: []string{hardnConfig}})
}

// grantHost is Debian 13 with hardn's grant for alice, without a password.
func grantHost() *fakeHost {
	return withHardnFiles(newHost(), sudoers.Policy{User: "alice", Grant: sudoers.GrantNoPassword})
}

// doasOverridden is the cloud image with hardn's rule for alice keeping
// HARDN_CONFIG, a rule that asks alice's password, persist, as Alpine's own
// 20-wheel.conf (sudoers.md §3): nothing in hardn writes a first rule
// for a user today (decision 1 (a)); a later unit may. A local
// zz-local.conf, "permit nopass :wheel", sorts after hardn's
// zz-hardn-alice.conf (slice 5, unit (b)), so doas's last match is its.
func doasOverridden() *fakeHost {
	h := withHardnFiles(alpineCloud(), sudoers.Policy{User: "alice", Grant: sudoers.GrantPassword, Env: []string{hardnConfig}})
	h.f.AddFile(sudoers.DoasDir+"/zz-local.conf", []byte("permit nopass :wheel\n"), 0o640, 0, 0)
	return h
}

// legacyLines are origin/main's /etc/sudoers.d/<user> once both of its
// writers ran: ConfigureSudo's grant, then setup-sudo-env's line appended
// (ops/setup_sudo_preservation/debian.golden, step 9, frozen in
// pkg/screens/environment/testdata/legacy since next/s9-replace).
const legacyLines = "alice ALL=(ALL) NOPASSWD: ALL\nDefaults:alice env_keep += \"HARDN_CONFIG\"\n"

// legacyHost is Debian 13 with origin/main's /etc/sudoers.d/alice.
func legacyHost() *fakeHost {
	h := newHost()
	h.f.AddFile(sudoers.DropInDir+"/alice", []byte(legacyLines), 0o440, 0, 0)
	return h
}

// lookup is getenv over env, hardn's environment on a host.
func lookup(env map[string]string) func(string) string {
	return func(k string) string { return env[k] }
}

// envHostMenu is the menu of the Environment screen's hosts: the sub-menu
// layout, an item per host.
func envHostMenu() screen.SubMenu {
	var items []component.MenuItem
	for i, eh := range envHosts() {
		items = append(items, component.MenuItem{Key: fmt.Sprint(i + 1), Label: eh.label, Description: eh.about})
	}
	return screen.SubMenu{
		Title:   component.TitleBar{Title: "Environment hosts"},
		Heading: "Select a host",
		Menu:    component.Menu{Items: items, Exit: component.MenuItem{Key: "0", Label: "Return"}},
		Prompt:  "Enter your choice",
	}
}

// openEnv returns the action that opens what name names: the hosts' menu
// for envPrefix alone, the Environment screen on one host for
// "environment/<host>".
func (s *session) openEnv(name string) func() error {
	if eh, ok := findEnvHost(strings.TrimPrefix(name, envPrefix+"/")); ok && name != envPrefix {
		return func() error { return s.envScreen(context.Background(), eh, eh.host()) }
	}
	return func() error {
		vm, all := envHostMenu(), envHosts()
		menu := nav.Menu{Exit: vm.Menu.Exit}
		for i, it := range vm.Menu.Items {
			eh := all[i]
			menu.Options = append(menu.Options, nav.Option{Item: it, Action: func() error { return s.envScreen(context.Background(), eh, eh.host()) }})
		}
		return s.p.Run(func() nav.Screen { return vm.Screen(s.p.Ctx, menu) })
	}
}

// envScreen runs the Environment screen on h, eh's host, until the user
// goes back from it: its note above it; the facts detect's, as the engine
// establishes them on the host; hardn's environment eh's; the change's
// engine the gallery's, over the host as the engine reaches it, its
// backups the defaults (the Environment hosts have no hardn.yml), with the
// gallery's hold, its apply recorded for the end of play. A dry run when
// play plays the dry-run sequence.
func (s *session) envScreen(ctx context.Context, eh envHost, h *fakeHost) error {
	sys, title := h.system(), envPrefix+"/"+eh.name
	facts, err := detect.Detect(ctx, sys)
	if err != nil {
		return fmt.Errorf("%s: %w", title, err)
	}
	if err := s.writeNote(title, eh.note); err != nil {
		return err
	}
	var rec *recorder
	return envscreen.Run(ctx, envscreen.Options{
		Prompter: s.p, System: sys, Facts: facts, Getenv: lookup(eh.env), DryRun: s.sequence == "dry-run",
		LogPath: output.LegacyLogPath, Now: clock,
		NewEngine: func(progress func(engine.Event)) *engine.Engine {
			r := &recorder{host: h, f: h.f, title: title}
			rec = r
			s.mu.Lock()
			s.engineRuns = append(s.engineRuns, r)
			s.mu.Unlock()
			r.begin()
			return engine.New(sys, engine.Options{
				Revert:   revert.Options{Now: clock, Hardn: galleryHardn},
				Session:  applySession,
				Backups:  backup.Settings{Path: backup.DefaultPath, Enabled: true},
				Hold:     s.hold,
				PID:      applyPID,
				Progress: func(ev engine.Event) { r.event(ev); progress(ev) },
			})
		},
		Ran: func(run output.Run) {
			rec.end()
			rec.setPlan(run.Plan)
			rec.failedWith(run.Err)
		},
	})
}

// envStart returns the hosts' menu, or the host, that args name
// ("environment", "environment/<host>"), and whether they name one; an
// error for a host that is not one.
func envStart(args []string) (string, bool, error) {
	if len(args) != 1 || (args[0] != envPrefix && !strings.HasPrefix(args[0], envPrefix+"/")) {
		return "", false, nil
	}
	if name := strings.TrimPrefix(args[0], envPrefix+"/"); args[0] != envPrefix {
		if _, ok := findEnvHost(name); !ok {
			var names []string
			for _, h := range envHosts() {
				names = append(names, envPrefix+"/"+h.name)
			}
			return "", false, fmt.Errorf("no environment host %q; the hosts are %s", args[0], strings.Join(names, ", "))
		}
	}
	return args[0], true, nil
}
