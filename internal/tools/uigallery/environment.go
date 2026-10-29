package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strings"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/steps"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/host/sudoers"
	"github.com/abbott/hardn/pkg/output"
	"github.com/abbott/hardn/pkg/screens/screenkit"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/flow"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/screen"
)

// The Environment mocks (refactor plan slice 9, unit (a), next/s9-gallery):
// a proposal of the Environment screen, main menu 8, for the maintainer's
// review, nothing of it wired into hardn. There is no Figma frame: it
// starts from origin/main's two status lines and two items
// (pkg/menu/environment_settings_menu.go:36-56) on the design system. It
// is drawn from what real code reads of a simulated host (fakehost.go):
// the elevation tool as sudoers.Choose picks it from detect's facts, and
// hardn's files as sudoers.Read reads them. Each change is the canonical
// flow on the real engine (screenkit.Session.Change, as the DNS and
// Backup screens run theirs): its plan sudoers.PlanFor's, as the engine's
// change steps.Sudoers, journalled as plans.Sudoers' step.
//
// What real code lacks is the gallery's, each a stand-in for unit (b)'s
// work, labelled so here and in the page: who ran hardn (invoker: SUDO_USER
// or DOAS_USER, never origin/main's USER fallback); the reader that turns
// hardn's files back into the user's whole Policy (currentPolicy), which
// PlanFor needs, as an env-only Policy plans the removal of hardn's grant;
// the plan's findings carried onto the change (sudoersFindings), which
// steps.Sudoers drops; and origin/main's own check of its legacy file
// (legacyKeeps).
//
// "environment" lists the mocks with no input; "play environment" is the
// menu of the hosts and "play environment/<host>" the screen on one.

// envPrefix names the mocks: "environment <mock>" in the listing,
// "environment" and "environment/<host>" in play.
const envPrefix = "environment"

// hardnConfig is the one variable the screen keeps (P-07 (c)).
const hardnConfig = "HARDN_CONFIG"

// The screen's words: origin/main's title (environment_settings_menu.go:34),
// Backup's decided heading and prompt (screen-backup.md §5, P-29 (a)), the
// rest proposed (P-07 (c) for "Keep HARDN_CONFIG").
const (
	envTitle   = "Environment Variable Settings"
	envSection = hardnConfig
	envHeading = "Select an option"
	envPrompt  = "Enter selection"
	valueKey   = "Value"
	userKey    = "User"
	notSet     = "Not set"
	keptWord   = "Kept"
	notKept    = "Not kept"
	// legacyNote is the Kept row's note where origin/main's file keeps it
	// (treatment legacy-kept): the file, in the user's terms.
	legacyNote = "by %s"
	// noUserValue is the user row of a host where hardn was not run
	// through the tool, in status.warn.
	noUserValue = "Not run with %s"
	// mayOverride is the warning line under a Kept row, for a file the
	// tool reads after hardn's, and the words of the plan's finding of
	// that file (sudoers' sudoers-later-file; sudoersFindings), so that
	// the review says it as the screen does, either way item 1 turns.
	mayOverride = "%s may override hardn's rule"
	keepIt      = "Keep HARDN_CONFIG"
	stopIt      = "Stop keeping HARDN_CONFIG"
	guideLabel  = "Guide"
	guideAbout  = "How to set HARDN_CONFIG"
	// The flows' title bars and questions.
	keepTitle = "Keep HARDN_CONFIG"
	stopTitle = "Stop Keeping HARDN_CONFIG"
	askKeep   = "Keep HARDN_CONFIG under %s?"
	askStop   = "Stop keeping HARDN_CONFIG under %s?"
	// The change's summary, the plan's heading and the step's line: the
	// same at every length of the user's name, which the review's User
	// row says, within engine.TitleMax.
	keepSummary = keepIt
	stopSummary = stopIt
	// takesEffect is said after a change applied: this hardn was started
	// without it, so its Value row does not change.
	takesEffect = "Takes effect at the next %s hardn."
	// doasNotice is said above the review where the change writes the
	// user's first doas rule (sudoers.md §6: "the screen must say so").
	doasNotice = "The rule also lets %s use doas, in wheel or not."
	// The refusals, said before asking anything.
	noToolWords = "sudo isn't installed here."
	noUserWords = "hardn wasn't run with %s, so there's no user to keep it for. Run %s hardn as that user."
	editedWords = "%s holds a rule hardn didn't write. Put hardn's back, or remove it."
	// doasRuleWords: doas keeps a variable only through the rule that
	// permits the user, any rule, a group's too, and hardn writes only a
	// rule of the user's own (sudoers.ErrEnvNeedsGrant; docs/safety/
	// sudoers.md:54, :94).
	doasRuleWords = "hardn has no doas rule for %s to keep HARDN_CONFIG on. Add setenv { HARDN_CONFIG } to the rule that lets %s use doas."
)

// guideWords are item 2's (origin/main's guide, environment_settings_menu.go:
// 120-139, shortened, its "sudo hardn setup-sudo-env" now item 1, whose
// label names the tool): proposed.
var guideWords = []string{
	"HARDN_CONFIG names the hardn.yml hardn reads. Set it in your shell profile:",
	"export HARDN_CONFIG=/path/to/hardn.yml",
	"Then keep it with item 1.",
}

// The menu's keys.
const (
	keyKeep  = "1"
	keyGuide = "2"
)

// envTreatment is what the screen does where the maintainer must choose
// (docs/engine/gallery-environment.md §5): its zero value is each
// decision's recommended option, and a host draws that unless it names
// another, not recommended.
type envTreatment struct {
	// keepOnly: item 1 keeps alone, origin/main's one direction, and on a
	// kept host its plan is empty (decision 5 (b), treatment item1-keep).
	// False: item 1 names the way it turns, "Stop keeping HARDN_CONFIG"
	// once kept, as Backup's item 1 does (5 (a), item1-toggle).
	keepOnly bool
	// doasRule: on doas, item 1 writes a per-user rule (doasGrant) for a
	// user with no rule of hardn's, which also grants the user doas, and
	// says so (decision 1 (b), doas-rule). False: that user is refused
	// before asking, as sudoers.PlanFor refuses an env-only Policy
	// (ErrEnvNeedsGrant) (1 (a), doas-refuse).
	doasRule bool
	// noReader: the change plans an env-only Policy (GrantNone), all that
	// real code can plan today, which removes hardn's grant (§3.2). False:
	// the gallery's reader (currentPolicy) carries the user's whole Policy.
	noReader bool
	// hardnOnly: origin/main's /etc/sudoers.d/<user> is not read, so its
	// env_keep line does not count as kept (decision 3 (b),
	// legacy-hardn-only). False: it is read with origin/main's own check
	// (legacyKeeps) and counts (3 (a), legacy-kept).
	hardnOnly bool
}

// doasGrant is the grant of the per-user doas rule item 1 writes for a
// user hardn has none for (treatment doas-rule): persist, as Alpine's own
// 20-wheel.conf (sudoers.md §3). Which grant is unit (b)'s to decide; the
// rule that grants the user doas today is not read (sudoers.md §6).
const doasGrant = sudoers.GrantPassword

// envHost is one simulated host the Environment mocks run on.
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
	// t is the treatment it draws.
	t envTreatment
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

// envHosts are the hosts, in the menu's order. Each draws every
// decision's recommended option unless its note names another.
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
		{name: "debian13", label: "Debian 13", about: "sudo, not kept", host: newHost, env: sudoAlice,
			note: deb + viaSud + drop},
		{name: "ubuntu2604", label: "Ubuntu 26.04", about: "sudo-rs, not kept", host: socketHost, env: sudoAlice,
			note: hostFQDN + " runs Ubuntu 26.04, sudo-rs 0.2.13. " + viaSud + drop},
		{name: "alpine324", label: "Alpine 3.24", about: "doas through wheel", host: alpineWheel, env: doasAlice,
			note: wheel + "hardn has no rule for alice, so item 1 refuses before asking."},
		{name: "alpine324-rule", label: "Alpine, rule", about: "Treatment doas-rule", host: alpineWheel, env: doasAlice,
			t:    envTreatment{doasRule: true},
			note: wheel + "Drawn with decision 1 (b), not recommended: item 1 writes a rule for alice."},
		{name: "alpine324-setenv", label: "Alpine, setenv on wheel", about: "The refusal followed", host: wheelSetenv, env: doasKept,
			note: setup + "alice did what item 1's refusal says: 20-wheel.conf says permit persist setenv { HARDN_CONFIG } :wheel, and doas kept /home/alice/hardn.yml. hardn has no rule for alice, so the screen says Not kept and item 1 refuses again."},
		{name: "alpine323-cloud", label: "Alpine cloud image", about: "wheel.conf read after", host: alpineCloud, env: doasAlice,
			t:    envTreatment{doasRule: true},
			note: cloud + "Drawn with decision 1 (b), not recommended: wheel.conf sorts after the rule item 1 writes, so its rule wins for alice."},
		{name: "doas-overridden", label: "doas, overridden", about: "hardn's rule, wheel.conf after", host: doasOverridden, env: doasAlice,
			note: cloud + "hardn's rule for alice keeps HARDN_CONFIG, but wheel.conf's wins, so doas dropped it. Stopping rewrites a rule that asks for alice's password."},
		{name: "pve9", label: "Proxmox VE 9", about: "no sudo", host: func() *fakeHost { return simulate(pve9Platform) }, env: rootLogin,
			note: hostFQDN + " is a Proxmox VE 9 node: no sudo, no doas. root logged in."},
		{name: "kept", label: "Kept", about: "hardn-env-alice there", host: keptHost, env: keptAlice,
			note: deb + "hardn's /etc/sudoers.d/hardn-env-alice keeps HARDN_CONFIG; " + viaSud + "it kept /home/alice/hardn.yml."},
		{name: "kept-only", label: "Kept, keep only", about: "Treatment item1-keep", host: keptHost, env: keptAlice,
			t:    envTreatment{keepOnly: true},
			note: deb + "As kept, drawn with decision 5 (b), not recommended: item 1 keeps alone."},
		{name: "root-login", label: "Root login", about: "Not run with sudo", host: newHost, env: rootLogin,
			note: deb + "root logged in and ran hardn: no SUDO_USER."},
		{name: "grant", label: "hardn's grant", about: "hardn-grant-alice there", host: grantHost, env: sudoAlice,
			note: deb + "hardn's /etc/sudoers.d/hardn-grant-alice lets alice use sudo without a password; " + viaSud + drop},
		{name: "grant-no-reader", label: "Grant, no reader", about: "An env-only Policy", host: grantHost, env: sudoAlice,
			t:    envTreatment{noReader: true},
			note: deb + "As grant, planned as real code can today: an env-only Policy, which removes hardn's grant."},
		{name: "legacy", label: "Legacy file", about: "/etc/sudoers.d/alice", host: legacyHost, env: keptAlice,
			note: deb + "origin/main's /etc/sudoers.d/alice holds alice's grant and its env_keep line; " + viaSud + "it kept /home/alice/hardn.yml."},
		{name: "legacy-hardn-only", label: "Legacy, unread", about: "Treatment legacy-hardn-only", host: legacyHost, env: keptAlice,
			t:    envTreatment{hardnOnly: true},
			note: deb + "As legacy, drawn with decision 3 (b), not recommended: hardn's own files alone count."},
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
// says (doasRuleWords): setenv on 20-wheel.conf's rule, a group's, which
// keeps the variable for every member (doas.conf(5), sudoers.md:94 [I]:
// doas's setenv has run on no host).
func wheelSetenv() *fakeHost {
	return withDoasFiles(simulate(alpine324Platform), "20-wheel.conf", "permit persist setenv { HARDN_CONFIG } :wheel\n")
}

// alpineCloud is Alpine 3.23's cloud image as the maintainer's VM: only
// wheel.conf, "permit nopass :wheel" (docs/safety/vmtest.md:562, :570),
// which ships in the image (:580) and sorts after hardn-<stem>.conf.
func alpineCloud() *fakeHost {
	return withDoasFiles(simulate(alpine323CloudPlatform), "wheel.conf", "permit nopass :wheel\n")
}

// withHardnFiles writes hardn's files for pol on h as sudoers.PlanFor
// plans them, so that their bytes are the package's own.
func withHardnFiles(h *fakeHost, pol sudoers.Policy) *fakeHost {
	ctx := context.Background()
	f, err := detect.Detect(ctx, h.system())
	if err != nil {
		panic(err) // not reached: the simulated hosts are detected (TestEnvHosts)
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
// HARDN_CONFIG, as item 1 writes it under decision 1 (b) (doasGrant).
func doasOverridden() *fakeHost {
	return withHardnFiles(alpineCloud(), sudoers.Policy{User: "alice", Grant: doasGrant, Env: []string{hardnConfig}})
}

// legacyLines are origin/main's /etc/sudoers.d/<user> once both of its
// writers ran: ConfigureSudo's grant, then setup-sudo-env's line appended
// (pkg/characterization's ops/setup_sudo_preservation/debian.golden, step
// 9).
const legacyLines = "alice ALL=(ALL) NOPASSWD: ALL\nDefaults:alice env_keep += \"HARDN_CONFIG\"\n"

// legacyHost is Debian 13 with origin/main's /etc/sudoers.d/alice.
func legacyHost() *fakeHost {
	h := newHost()
	h.f.AddFile(sudoers.DropInDir+"/alice", []byte(legacyLines), 0o440, 0, 0)
	return h
}

// invoker is who ran hardn through tool t, from hardn's environment: SUDO_USER
// under sudo and sudo-rs (sudo(8) ENVIRONMENT; sudo-rs v0.2.13
// src/sudo/env/environment.rs, add_extra_env), DOAS_USER under doas
// (OpenDoas v6.8.2 env.c, createenv); "" for none, or root. origin/main
// fell back to USER, root's under a root login (file_environment_repository.go:
// 135-138). The gallery's: unit (b)'s.
func invoker(env map[string]string, t sudoers.Tool) string {
	if u := env[userVar(t)]; u != "root" {
		return u
	}
	return ""
}

// userVar is the variable tool t names its user in.
func userVar(t sudoers.Tool) string {
	if t == sudoers.ToolDoas {
		return "DOAS_USER"
	}
	return "SUDO_USER"
}

// toolWord is t's name in the screen's words: "sudo", or "doas".
func toolWord(t sudoers.Tool) string {
	if t == sudoers.ToolDoas {
		return "doas"
	}
	return "sudo"
}

// errEdited: hardn's file for the user holds what no Policy plans.
var errEdited = errors.New("hardn's file holds a rule hardn did not write")

// envKeepVars and setenvVars find the variables of hardn's env_keep line
// (sudo, sudoers.go's sudoEnv) and of the setenv on its doas rule
// (doasRule): candidates only, which currentPolicy confirms by planning
// them again.
var (
	envKeepVars = regexp.MustCompile(`(?m)^Defaults:\S+ env_keep \+= "([^"\n]*)"$`)
	setenvVars  = regexp.MustCompile(`(?m)^permit .*\bsetenv \{ ([^}\n]*) \}`)
)

// currentPolicy is the gallery's reader of hardn's files: the user's whole
// Policy, grant and variables, as st holds it. It takes the variables of
// hardn's env_keep line, or of its doas rule's setenv, whatever they are,
// tries them with each grant, and returns the Policy whose files hold
// exactly the content sudoers.PlanFor writes for it, PlanFor checking each
// variable's name as it does any Policy's (a mode or owner may differ,
// which the next plan puts right); errEdited, naming the file, when none
// does. Pure. No reader is in pkg/host/sudoers (PlanFor takes the user's
// whole state): unit (b)'s.
func currentPolicy(st sudoers.State, user string) (sudoers.Policy, error) {
	n, err := sudoers.FileNames(st.Choice.Tool, user)
	if err != nil {
		return sudoers.Policy{}, err
	}
	file, vars := n.Env, envKeepVars
	if st.Choice.Tool == sudoers.ToolDoas {
		file, vars = n.Grant, setenvVars
	}
	var env []string
	if m := vars.FindSubmatch(st.Files[file].Data); m != nil {
		env = strings.Fields(string(m[1]))
	}
	for _, g := range []sudoers.Grant{sudoers.GrantNone, sudoers.GrantPassword, sudoers.GrantNoPassword} {
		pol := sudoers.Policy{User: user, Grant: g, Env: env}
		p, err := sudoers.PlanFor(st, []sudoers.Policy{pol})
		if err == nil && !slices.ContainsFunc(p.Changes, func(c sudoers.Change) bool {
			return c.Remove || !c.Old.Exists || !bytes.Equal(c.Old.Data, c.Data)
		}) {
			return pol, nil
		}
	}
	for _, name := range []string{n.Env, n.Grant} {
		if f := st.Files[name]; name != "" && f.Exists {
			return sudoers.Policy{}, fmt.Errorf("%s: %w", name, errEdited)
		}
	}
	return sudoers.Policy{}, errEdited // not reached: with no file, GrantNone plans nothing
}

// target is the Policy item 1 plans from cur, the user's as read: cur with
// HARDN_CONFIG added to its variables (keep) or taken out of them (stop),
// the others carried as they are. Under noReader, an env-only Policy, as
// real code can plan today. On doas, a user with no rule of hardn's gets
// doasGrant under doasRule; otherwise GrantNone stays, for PlanFor to
// refuse (ErrEnvNeedsGrant).
func (t envTreatment) target(cur sudoers.Policy, tool sudoers.Tool, keep bool) sudoers.Policy {
	pol := cur
	if t.noReader {
		pol = sudoers.Policy{User: cur.User}
	}
	pol.Env = slices.Clone(pol.Env)
	switch {
	case !keep:
		pol.Env = slices.DeleteFunc(pol.Env, func(v string) bool { return v == hardnConfig })
	case !slices.Contains(pol.Env, hardnConfig):
		pol.Env = append(pol.Env, hardnConfig)
	}
	if keep && tool == sudoers.ToolDoas && pol.Grant == sudoers.GrantNone && t.doasRule {
		pol.Grant = doasGrant
	}
	return pol
}

// envReading is one reading of a host's environment, as the screen draws
// it: who ran hardn, what it sees of HARDN_CONFIG, the tool, hardn's files
// and what item 1 would plan.
type envReading struct {
	t      envTreatment
	value  string // HARDN_CONFIG as hardn sees it; "" not set
	choice sudoers.Choice
	user   string // invoker; "" none
	// err is why item 1 is refused before asking: the tool's choice,
	// sudoers.Read's or the reader's error; nil when it asks.
	err error
	st  sudoers.State
	cur sudoers.Policy
	// kept: hardn's files keep HARDN_CONFIG (cur.Env).
	kept bool
	// legacy is origin/main's /etc/sudoers.d/<user> when sudo reads it,
	// and legacyKeeps whether its env_keep line is there.
	legacy      string
	legacyKeeps bool
	// plan is what item 1 plans (sudoers.PlanFor, pure), planErr its
	// refusal; later the files PlanFor finds read after hardn's.
	plan    sudoers.Plan
	planErr error
	later   []string
}

// readEnv reads what the screen draws of sys, hardn's environment env, as
// the engine detects it (detect.Detect), under treatment t. It changes
// nothing.
func readEnv(ctx context.Context, sys host.System, env map[string]string, t envTreatment) envReading {
	r := envReading{t: t, value: env[hardnConfig]}
	f, err := detect.Detect(ctx, sys)
	if err != nil {
		// The tool is then not established: said as Unknown, and item 1
		// refuses with the error.
		r.err, r.choice.Err = err, err
		return r
	}
	r.choice = sudoers.Choose(f, sudoers.ToolUnknown)
	r.user = invoker(env, r.choice.Tool)
	switch {
	case r.choice.Err != nil:
		r.err = r.choice.Err
		return r
	case r.user == "":
		return r
	}
	if r.st, r.err = sudoers.Read(ctx, sys, r.choice, []string{r.user}); r.err != nil {
		return r
	}
	if r.cur, r.err = currentPolicy(r.st, r.user); r.err != nil {
		return r
	}
	r.kept = slices.Contains(r.cur.Env, hardnConfig)
	if r.choice.Tool == sudoers.ToolSudo && slices.Contains(r.st.Entries, r.user) {
		r.legacy = sudoers.DropInDir + "/" + r.user
		r.legacyKeeps = legacyKeeps(ctx, sys, r.legacy)
	}
	r.plan, r.planErr = sudoers.PlanFor(r.st, []sudoers.Policy{t.target(r.cur, r.choice.Tool, r.keeps())})
	for _, fd := range r.plan.Findings {
		if fd.Kind == sudoers.FindingLater {
			r.later = append(r.later, fd.File)
		}
	}
	return r
}

// legacyKeeps is origin/main's own check of its file (strings.Contains,
// file_environment_repository.go:124): whether name holds its env_keep
// line. Reading another file's rules is not pkg/host/sudoers' (sudoers.md
// §6); this is the gallery's, for treatment legacy-kept.
func legacyKeeps(ctx context.Context, sys host.System, name string) bool {
	data, _, err := sys.ReadFileNoFollow(ctx, name)
	return err == nil && strings.Contains(string(data), "env_keep += \"HARDN_CONFIG\"")
}

// keeps is the way item 1 turns: keep, unless hardn's files keep it
// already and item 1 turns both ways (not keepOnly).
func (r envReading) keeps() bool { return r.t.keepOnly || !r.kept }

// keptHere is whether HARDN_CONFIG is kept: by hardn's files, or by
// origin/main's line, unless only hardn's count (hardnOnly).
func (r envReading) keptHere() bool { return r.kept || r.legacyKeeps && !r.t.hardnOnly }

// risky is whether item 1's plan is lockout-risk (sudoers.RiskLockout).
func (r envReading) risky() bool {
	return slices.ContainsFunc(r.plan.Changes, func(c sudoers.Change) bool { return c.Risk == sudoers.RiskLockout })
}

// newRule is whether item 1 writes the user's first doas rule, which
// grants doas too.
func (r envReading) newRule() bool {
	return r.choice.Tool == sudoers.ToolDoas && r.cur.Grant == sudoers.GrantNone && r.keeps() && r.planErr == nil
}

// refusal is what item 1 says before asking, nil when it asks: no tool, no
// user, a read or the reader refused, or the plan refused (doas-refuse).
func (r envReading) refusal() component.Messages {
	tool := toolWord(r.choice.Tool)
	var text string
	switch {
	case r.choice.Tool == sudoers.ToolNone:
		text = noToolWords
	case r.choice.Err != nil:
		text = screenkit.RefusalWords(r.choice.Err)
	case r.user == "":
		text = fmt.Sprintf(noUserWords, tool, tool)
	case errors.Is(r.err, errEdited):
		name, _, _ := strings.Cut(r.err.Error(), ": ")
		text = fmt.Sprintf(editedWords, name)
	case r.err != nil:
		text = screenkit.RefusalWords(r.err)
	case errors.Is(r.planErr, sudoers.ErrEnvNeedsGrant):
		text = fmt.Sprintf(doasRuleWords, r.user, r.user)
	case r.planErr != nil:
		text = screenkit.RefusalWords(r.planErr)
	default:
		return nil
	}
	return screenkit.Warnings(text)
}

// underKey is the state row's key: "Under sudo", "Under doas".
func (r envReading) underKey() string { return "Under " + toolWord(r.choice.Tool) }

// keptRow is the state row where hardn's files keep HARDN_CONFIG: Kept,
// in status.warn where a file the tool reads after hardn's may override
// it, as what wins is not read (sudoers.md §6). The screen lists each such
// file under it (rows), the review the plan's finding of it (review).
func (r envReading) keptRow() component.KeyValue {
	row := component.KeyValue{Key: r.underKey(), Value: keptWord, State: component.ValueOK}
	if len(r.later) > 0 {
		row.State = component.ValueWarn
	}
	return row
}

// rows are the section's rows: the user who ran hardn through the tool,
// whether HARDN_CONFIG is kept for them, with a line for each file read
// after hardn's, and its value as hardn sees it.
func (r envReading) rows() []component.KeyValue {
	tool := toolWord(r.choice.Tool)
	var rows []component.KeyValue
	switch {
	case r.choice.Tool == sudoers.ToolNone:
		rows = append(rows, component.KeyValue{Key: r.underKey(), State: component.ValueNotApplicable})
	case r.choice.Err != nil:
		rows = append(rows, component.KeyValue{Key: r.underKey(), State: component.ValueUnknown})
	case r.user == "":
		rows = append(rows, component.KeyValue{Key: userKey, Value: fmt.Sprintf(noUserValue, tool), State: component.ValueWarn})
	default:
		rows = append(rows, component.KeyValue{Key: userKey, Value: r.user})
		under := component.KeyValue{Key: r.underKey(), Value: notKept, State: component.ValueWarn}
		switch {
		case r.err != nil:
			under = component.KeyValue{Key: r.underKey(), State: component.ValueUnknown}
		case r.kept:
			under = r.keptRow()
		case r.keptHere():
			under.Value, under.State, under.Note = keptWord, component.ValueOK, fmt.Sprintf(legacyNote, r.legacy)
		}
		rows = append(rows, under)
		if r.kept {
			for _, l := range r.later {
				rows = append(rows, component.KeyValue{Key: fmt.Sprintf(mayOverride, l), State: component.ValueWarn})
			}
		}
	}
	value := component.KeyValue{Key: valueKey, Value: r.value}
	if r.value == "" {
		value.Value = notSet
	}
	return append(rows, value)
}

// subMenu is the screen of r: the title bar; the section HARDN_CONFIG and
// its rows; item 1, labelled with the state row's key, as Backup's item 1
// is, keeping HARDN_CONFIG, or once kept stopping (not under keepOnly),
// marked Lockout risk where its plan is; item 2, the guide; Return; no
// status row.
func (r envReading) subMenu() screen.SubMenu {
	about := keepIt
	if !r.keeps() {
		about = stopIt
	}
	return screen.SubMenu{
		Title:    component.TitleBar{Title: envTitle},
		Sections: []screen.Section{{Header: component.SectionHeader{Title: envSection}, Rows: component.BulletList{Items: r.rows()}}},
		Heading:  envHeading,
		Menu: component.Menu{Items: []component.MenuItem{
			{Key: keyKeep, Label: r.underKey(), Description: about, Risk: r.refusal() == nil && r.risky()},
			{Key: keyGuide, Label: guideLabel, Description: guideAbout},
		}, Exit: component.MenuItem{Key: "0", Label: "Return"}},
		Prompt: envPrompt,
	}
}

// originScreen is origin/main's Environment screen, its words as they are
// (environment_settings_menu.go:34-64), on the design system: its two
// status lines as bullet items, under the proposed section's header (a
// section has one; origin/main's has none), its two items and Return. Its
// sudo line reads origin/main's file alone
// (file_environment_repository.go:104-125).
func (r envReading) originScreen() screen.SubMenu {
	cfg := component.KeyValue{Key: "HARDN_CONFIG environment variable is not set"}
	if r.value != "" {
		cfg = component.KeyValue{Key: "Current HARDN_CONFIG", Value: r.value, State: component.ValueOK}
	}
	pres := component.KeyValue{Key: "Sudo preservation", Value: "Disabled", State: component.ValueBad}
	if r.legacyKeeps {
		pres.Value, pres.State = "Enabled", component.ValueOK
	}
	return screen.SubMenu{
		Title:    component.TitleBar{Title: envTitle},
		Sections: []screen.Section{{Header: component.SectionHeader{Title: envSection}, Rows: component.BulletList{Items: []component.KeyValue{cfg, pres}}}},
		Heading:  envHeading,
		Menu: component.Menu{Items: []component.MenuItem{
			{Key: "1", Label: "Setup sudo environment preservation", Description: "Configure sudo to preserve HARDN_CONFIG"},
			{Key: "2", Label: "Show environment variables guide", Description: "Learn how to set up environment variables"},
		}, Exit: component.MenuItem{Key: "0", Label: "Return"}},
		Prompt: envPrompt,
	}
}

// sudoersFindings are a sudoers plan's findings as the change's, coded as
// the registry has them (SUDOERS_LATER_FILE, SUDOERS_LEGACY_FILE,
// SUDOERS_AFTER_INCLUDE), the later file's in the screen's words
// (mayOverride), the others' details as sudoers has them. steps.Sudoers
// drops them: the gallery's, unit (b)'s to carry, with their words.
func sudoersFindings(fs []sudoers.Finding) []engine.Finding {
	var out []engine.Finding
	for _, f := range fs {
		detail := f.Detail
		if f.Kind == sudoers.FindingLater {
			detail = fmt.Sprintf(mayOverride, f.File)
		}
		out = append(out, engine.Finding{Code: steps.FindingCode("SUDOERS", f.Kind.String()), Detail: detail, Source: steps.SourceAt(f.File, f.Line)})
	}
	return out
}

// envControl is item 1's control, the gallery's (unit (b)'s): it reads
// the user's files where the engine plans, the user's Policy through the
// gallery's reader, and plans t's target with sudoers.PlanFor, as
// steps.Sudoers' change, the plan's findings on it. Nothing to change
// plans nothing.
type envControl struct {
	user string
	t    envTreatment
	keep bool
}

func (envControl) ID() string                          { return "environment" }
func (envControl) Title() string                       { return "Environment" }
func (envControl) Applies(detect.Facts) (bool, string) { return true, "" }

// Check is not used: the screen reads the host itself (readEnv).
func (envControl) Check(context.Context, host.System, detect.Facts) (engine.Status, error) {
	return engine.Status{}, nil
}

func (c envControl) Plan(ctx context.Context, sys host.System, in engine.Input) ([]engine.Change, error) {
	choice := sudoers.Choose(in.Facts, sudoers.ToolUnknown)
	if choice.Err != nil {
		return nil, choice.Err
	}
	st, err := sudoers.Read(ctx, sys, choice, []string{c.user})
	if err != nil {
		return nil, err
	}
	cur := sudoers.Policy{User: c.user}
	if !c.t.noReader {
		if cur, err = currentPolicy(st, c.user); err != nil {
			return nil, err
		}
	}
	p, err := sudoers.PlanFor(st, []sudoers.Policy{c.t.target(cur, choice.Tool, c.keep)})
	if err != nil || len(p.Changes) == 0 {
		return nil, err
	}
	summary := keepSummary
	if !c.keep {
		summary = stopSummary
	}
	ch, err := steps.Sudoers("environment/"+c.user, p, summary)
	ch.Findings = sudoersFindings(p.Findings)
	return []engine.Change{ch}, err
}

// envRunner is one run of the Environment screen on a simulated host.
type envRunner struct {
	s     *session
	eh    envHost
	h     *fakeHost
	title string
}

// openEnv returns the action that opens what name names: the hosts' menu
// for envPrefix alone, the screen on one host for "environment/<host>".
func (s *session) openEnv(name string) func() error {
	if eh, ok := findEnvHost(strings.TrimPrefix(name, envPrefix+"/")); ok && name != envPrefix {
		return func() error { return s.envScreen(context.Background(), eh, eh.host()) }
	}
	return func() error {
		all := envHosts()
		vm := screen.SubMenu{Title: component.TitleBar{Title: "Environment hosts"}, Heading: "Select a host", Prompt: "Enter your choice",
			Menu: component.Menu{Exit: component.MenuItem{Key: "0", Label: "Return"}}}
		menu := nav.Menu{Exit: vm.Menu.Exit}
		for i, eh := range all {
			it := component.MenuItem{Key: fmt.Sprint(i + 1), Label: eh.label, Description: eh.about}
			vm.Menu.Items = append(vm.Menu.Items, it)
			menu.Options = append(menu.Options, nav.Option{Item: it, Action: func() error { return s.envScreen(context.Background(), eh, eh.host()) }})
		}
		return s.p.Run(func() nav.Screen { return vm.Screen(s.p.Ctx, menu) })
	}
}

// envScreen runs the Environment screen on h, eh's host, until the user
// goes back from it: its note above it, then each round the screen read
// again from the host.
func (s *session) envScreen(ctx context.Context, eh envHost, h *fakeHost) error {
	e := &envRunner{s: s, eh: eh, h: h, title: envPrefix + "/" + eh.name}
	if err := s.writeNote(e.title, eh.note); err != nil {
		return err
	}
	return s.p.Run(func() nav.Screen { return e.draw(ctx) })
}

// draw reads the host and returns the screen, its items bound: item 1
// says why it cannot run before asking anything, as the DNS and Backup
// screens do, or runs its change; item 2 says the guide.
func (e *envRunner) draw(ctx context.Context) nav.Screen {
	r := readEnv(ctx, e.h.system(), e.eh.env, e.eh.t)
	vm := r.subMenu()
	ks := e.kit()
	item1 := func() error { return e.change(ctx, r) }
	if msgs := r.refusal(); msgs != nil {
		item1 = ks.Say(msgs)
	}
	actions := map[string]func() error{keyKeep: item1, keyGuide: ks.Say(guide())}
	menu := nav.Menu{Exit: vm.Menu.Exit}
	for _, it := range vm.Menu.Items {
		menu.Options = append(menu.Options, nav.Option{Item: it, Action: actions[it.Key]})
	}
	return vm.Screen(e.s.p.Ctx, menu)
}

// guide is item 2's words.
func guide() component.Messages {
	var msgs component.Messages
	for _, w := range guideWords {
		msgs = append(msgs, component.Message{Text: w})
	}
	return msgs
}

// kit is the screen's screenkit.Session: the change's engine the gallery's,
// over the host as the engine reaches it, its backups the defaults, the
// gallery's hold, its apply recorded for the end of play; a dry run when
// play plays the dry-run sequence. The Environment screen saves no
// hardn.yml: its configuration is read from nowhere, so nothing is adopted.
func (e *envRunner) kit() screenkit.Session {
	s, h := e.s, e.h
	var rec *recorder
	return screenkit.Session{
		Name: "environment screen", Prompter: s.p, System: h.system(), Config: &config.Config{},
		DryRun: s.sequence == "dry-run", LogPath: output.LegacyLogPath, Now: clock,
		NewEngine: func(progress func(engine.Event)) *engine.Engine {
			r := &recorder{host: h, f: h.f, title: e.title}
			rec = r
			s.mu.Lock()
			s.engineRuns = append(s.engineRuns, r)
			s.mu.Unlock()
			r.begin()
			return engine.New(h.system(), engine.Options{
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
	}
}

// review is item 1's review: on doas, where it writes the user's first
// rule, the notice that the rule grants doas too; the section HARDN_CONFIG,
// the user and the state the change makes, in the screen's words (keptRow,
// or Not kept), the plan's findings listed under them by the flow.
func (r envReading) review() flow.Review {
	state := r.keptRow()
	if !r.keeps() {
		state = component.KeyValue{Key: r.underKey(), Value: notKept, State: component.ValueWarn}
	}
	rv := flow.Review{Title: envSection, Items: []component.KeyValue{{Key: userKey, Value: r.user}, state}}
	if r.newRule() {
		rv.Notice = &flow.Notice{Callout: component.Callout{Kind: component.MessageWarn, Body: []string{fmt.Sprintf(doasNotice, r.user)}}}
	}
	return rv
}

// change is item 1 through the flow, under its title bar: the review
// (review); the plan, the engine's, of envControl, its findings listed;
// the question, y/N, or the hostname typed for a lockout-risk plan; the
// apply. Then, once applied, that it takes effect at the next sudo: this
// hardn was started without it.
func (e *envRunner) change(ctx context.Context, r envReading) error {
	keep, tool := r.keeps(), toolWord(r.choice.Tool)
	title, question := keepTitle, fmt.Sprintf(askKeep, tool)
	if !keep {
		title, question = stopTitle, fmt.Sprintf(askStop, tool)
	}
	review := r.review()
	ks := e.kit()
	ran := ks.Ran
	ks.Ran = func(run output.Run) {
		ran(run)
		if run.Mode == output.ModeApply && run.Err == nil && run.Result != nil && !run.Result.Nothing && run.Result.Failed == nil {
			_ = ks.Say(component.Messages{{Text: fmt.Sprintf(takesEffect, tool)}})()
		}
	}
	return ks.Change(ctx, screenkit.Change{
		Step: screen.WorkflowStep{Title: component.TitleBar{Title: title}},
		Flow: flow.Flow{
			Review: func() flow.Review { return review },
			Confirm: func(p flow.Plan) flow.Confirm {
				if p.Typed() {
					return flow.Confirm{Question: question, Label: typeTheHostname, Phrase: hostName}
				}
				return flow.Confirm{Question: question}
			},
		},
		Controls: func(*config.Config) []engine.Control {
			return []engine.Control{envControl{user: r.user, t: r.t, keep: keep}}
		},
	})
}

// envMock is one mock the listing shows: its name, after envPrefix, its
// note, and its drawing.
type envMock struct {
	name, note string
	draw       func(ctx context.Context, c component.Ctx) (string, error)
}

// readHostNamed reads the host named name, fresh, as the screen does.
func readHostNamed(ctx context.Context, name string) (envReading, error) {
	eh, ok := findEnvHost(name)
	if !ok {
		return envReading{}, fmt.Errorf("no environment host %q", name)
	}
	return readEnv(ctx, eh.host().system(), eh.env, eh.t), nil
}

// onEnvHost returns a drawing of the host named name's reading.
func onEnvHost(name string, view func(envReading, component.Ctx) string) func(context.Context, component.Ctx) (string, error) {
	return func(ctx context.Context, c component.Ctx) (string, error) {
		r, err := readHostNamed(ctx, name)
		if err != nil {
			return "", err
		}
		return view(r, c), nil
	}
}

// envMocks are the listing's mocks, in order: the screen on each host,
// origin/main's screen on the design system, item 1 on alpine324 under
// each doas treatment, the guide, and the refusals.
func envMocks() []envMock {
	var mocks []envMock
	for _, eh := range envHosts() {
		mocks = append(mocks, envMock{name: "screen-" + eh.name, note: "Mock: the Environment screen. " + eh.note,
			draw: onEnvHost(eh.name, func(r envReading, c component.Ctx) string { return r.subMenu().Render(c) })})
	}
	return append(mocks,
		envMock{name: "origin", note: "origin/main's Environment screen, its words as they are (environment_settings_menu.go:34-64), on the design system, on the debian13 host: its two lines, under the proposed section's header (origin/main has none), and its two items, whose labels push the menu's descriptions under them.",
			draw: onEnvHost("debian13", func(r envReading, c component.Ctx) string { return r.originScreen().Render(c) })},
		envMock{name: "item1-doas-refuse", note: "Decision 1 (a), recommended: item 1 on alpine324 refuses before asking, as sudoers.PlanFor refuses an env-only Policy on doas (ErrEnvNeedsGrant).",
			draw: onEnvHost("alpine324", itemOne)},
		envMock{name: "item1-doas-rule", note: "Decision 1 (b), not recommended: item 1 on alpine324-rule writes a rule for alice, permit persist setenv { HARDN_CONFIG } alice, and says first that it grants alice doas. Its plan is lockout-risk (sudoers.RiskLockout: a grant that asks a password), so it is confirmed by typing the hostname.",
			draw: onEnvHost("alpine324-rule", itemOne)},
		envMock{name: "guide", note: "Mock: Guide (2), origin/main's guide shortened, under the screen's prompt.",
			draw: func(_ context.Context, c component.Ctx) (string, error) { return guide().Render(c), nil }},
		envMock{name: "refusals", note: "Mock: item 1's refusals before asking, on the root-login, pve9 and alpine324 hosts, and on a host whose hardn file was edited by hand.",
			draw: refusalsMock},
	)
}

// itemOne draws what item 1 does first on r's host: its refusal, or the
// notice and the review the flow shows above its plan (review).
func itemOne(r envReading, c component.Ctx) string {
	if msgs := r.refusal(); msgs != nil {
		return apart(r.subMenu().Render(c), msgs.Render(c))
	}
	rv := r.review()
	blocks := []string{r.subMenu().Render(c)}
	if rv.Notice != nil {
		blocks = append(blocks, rv.Notice.Callout.Render(c))
	}
	return apart(append(blocks, component.SectionHeader{Title: rv.Title}.Render(c), component.BulletList{Items: rv.Items}.Render(c))...)
}

// editedHost is Debian 13 whose hardn-env-alice was edited by hand to
// grant alice sudo as well: a file sudoers.PlanFor writes for no Policy.
func editedHost() *fakeHost {
	h := keptHost()
	h.f.AddFile(sudoers.DropInDir+"/hardn-env-alice", []byte(sudoers.Header+"Defaults:alice env_keep += \"HARDN_CONFIG\"\nalice ALL=(ALL) NOPASSWD: ALL\n"), 0o440, 0, 0)
	return h
}

// refusalsMock draws each refusal under a header naming its host.
func refusalsMock(ctx context.Context, c component.Ctx) (string, error) {
	var out []string
	for _, name := range []string{"root-login", "pve9", "alpine324"} {
		r, err := readHostNamed(ctx, name)
		if err != nil {
			return "", err
		}
		out = append(out, header(c, "item 1 on "+name), r.refusal().Render(c))
	}
	r := readEnv(ctx, editedHost().system(), sudoAlice, envTreatment{})
	return apart(append(out, header(c, "item 1, hardn-env-alice edited"), r.refusal().Render(c))...), nil
}

// envUsage is the listing's usage line.
const envUsage = "usage: go run ./internal/tools/uigallery environment [flags] [mock | all]..."

// envList is the listing: each mock args name (none, or "all", is every
// mock) under the gallery's header, "environment <mock>", its note, then
// its drawing, a write each; -list prints their names. It sizes itself as
// play does (look.sizeTo).
func envList(args []string, size sizer, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("uigallery environment", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintln(stderr, envUsage)
		fs.PrintDefaults()
	}
	list := fs.Bool("list", false, "print the names of the mocks it would list, and exit")
	look := lookFlags(fs, true)
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}
		return exitUsage
	}
	mocks, err := selectEnvMocks(fs.Args())
	if err != nil {
		fmt.Fprintf(stderr, "uigallery: %s: %v\n", envPrefix, err)
		return exitUsage
	}
	if *list {
		for _, m := range mocks {
			fmt.Fprintln(stdout, m.name)
		}
		return exitOK
	}
	look.sizeTo(fs, size)
	c, err := look.cell()
	if err != nil {
		fmt.Fprintf(stderr, "uigallery: %s: %v\n", envPrefix, err)
		return exitUsage
	}
	g, ok := look.gallery(stderr)
	if !ok {
		return exitError
	}
	rc, err := g.ctx(c.env)
	if err == nil {
		err = listEnv(context.Background(), rc, c.env.Writer(stdout), mocks)
	}
	if err != nil {
		fmt.Fprintf(stderr, "uigallery: %s: %v\n", envPrefix, err)
		return exitError
	}
	return exitOK
}

// listEnv writes each of mocks to w, rendered in rc, a write each: the
// gallery's header, the mock's note, then its drawing.
func listEnv(ctx context.Context, rc component.Ctx, w io.Writer, mocks []envMock) error {
	for _, m := range mocks {
		out, err := m.draw(ctx, rc)
		if err == nil {
			_, err = io.WriteString(w, apart(header(rc, envPrefix+" "+m.name),
				component.Callout{Kind: component.MessageInfo, Body: []string{m.note}}.Render(rc), out)+sectionBreak)
		}
		if err != nil {
			return fmt.Errorf("%s: %w", m.name, err)
		}
	}
	return nil
}

// selectEnvMocks returns the mocks args name, in order: every mock for
// none or "all". An argument that starts with "-" is a flag the flag
// package left unparsed, after a name.
func selectEnvMocks(args []string) ([]envMock, error) {
	all := envMocks()
	if len(args) == 0 || slices.Contains(args, "all") {
		return all, nil
	}
	for _, a := range args {
		switch found := slices.ContainsFunc(all, func(m envMock) bool { return m.name == a }); {
		case !found && strings.HasPrefix(a, "-"):
			return nil, fmt.Errorf("flag %s comes after a mock's name; flags go before them", a)
		case !found:
			return nil, fmt.Errorf("no mock %q; -list shows them", a)
		}
	}
	return slices.DeleteFunc(all, func(m envMock) bool { return !slices.Contains(args, m.name) }), nil
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

// envNote is play's note for the Environment mocks.
const envNote = "Play mode, mocks: the Environment screen proposed for slice 9, drawn from what real code reads of a simulated host " +
	"(sudoers.Choose, Read, PlanFor), each change on the real plan/apply engine (steps.Sudoers). Who ran hardn, the reader of hardn's files " +
	"and the plan's findings on the change are the gallery's, unit (b)'s to build. Nothing on this machine changes: every command and file is the fake's, " +
	"and its record is shown when play ends."
