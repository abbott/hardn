package environment_test

import (
	"bytes"
	"context"
	"io/fs"
	"strings"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/header"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/host/sudoers"
	"github.com/abbott/hardn/pkg/output"
	envscreen "github.com/abbott/hardn/pkg/screens/environment"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/prompt"
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/uitest"
)

// The hosts the Environment screen's tests run on, on hostfake, as the
// gallery's Environment hosts are drawn (gallery-environment.md §3.1) and
// the Environment control's tests have them: Debian 13 (sudo), Ubuntu
// 26.04 (sudo-rs), Alpine 3.24 after setup-alpine (doas, 20-wheel.conf),
// Alpine 3.23's cloud image (doas, wheel.conf, read after hardn's rule),
// Proxmox VE 9 (neither tool). visudo and doas -C pass every file but
// where a host says otherwise: the tests' model, not the tools. Synthetic
// values only: web-01, alice. now, lookup and debianMain are
// parity_test.go's.

const (
	hardnBinary = "/usr/local/bin/hardn"
	keptPath    = "/home/alice/hardn.yml"
	// shortName is the hosts' name as the kernel has it, the phrase a
	// lockout-risk change is confirmed by typing.
	shortName = "web-01"
	// doasConf is Alpine's stock /etc/doas.conf, its rule commented out.
	doasConf = "# see doas.conf(5) for configuration details\n\n# permit persist :wheel\n"
	// legacyLines are origin/main's /etc/sudoers.d/<user> once both of
	// its writers ran (pkg/characterization's
	// ops/setup_sudo_preservation/debian.golden, step 9).
	legacyLines = "alice ALL=(ALL) NOPASSWD: ALL\nDefaults:alice env_keep += \"HARDN_CONFIG\"\n"
	// editedEnv is hardn-env-alice edited by hand to grant alice sudo
	// too: a file sudoers.PlanFor writes for no Policy.
	editedEnv = sudoers.Header + "Defaults:alice env_keep += \"HARDN_CONFIG\"\nalice ALL=(ALL) NOPASSWD: ALL\n"
	envFile   = sudoers.DropInDir + "/hardn-env-alice"
	grantFile = sudoers.DropInDir + "/hardn-grant-alice"
	ruleFile  = sudoers.DoasDir + "/hardn-alice.conf"
)

// The environments hardn runs in: alice's sudo or doas, HARDN_CONFIG
// dropped by it or kept; a root login.
var (
	viaSudo   = map[string]string{"SUDO_USER": "alice", "USER": "root"}
	viaSudoKe = map[string]string{"SUDO_USER": "alice", "USER": "root", "HARDN_CONFIG": keptPath}
	viaDoas   = map[string]string{"DOAS_USER": "alice", "USER": "root"}
	viaDoasKe = map[string]string{"DOAS_USER": "alice", "USER": "root", "HARDN_CONFIG": keptPath}
	atRoot    = map[string]string{"USER": "root"}
)

// The hosts, by name: the gallery's ten, then the control's refusals
// and failures the gallery does not show.
const (
	hostDebian     = "debian13"         // sudo, alice, not kept
	hostUbuntu     = "ubuntu2604"       // sudo-rs, alice, not kept
	hostAlpine     = "alpine324"        // doas through 20-wheel.conf; no rule of hardn's: refused
	hostSetenv     = "alpine324-setenv" // the same, setenv on 20-wheel.conf, doas kept the variable: refused again
	hostOverridden = "doas-overridden"  // the cloud image, hardn's password rule for alice keeping it; wheel.conf after
	hostPVE        = "pve9"             // neither tool; a root login
	hostKept       = "kept"             // hardn-env-alice there; sudo kept the variable
	hostRootLogin  = "root-login"       // Debian 13, a root login
	hostGrant      = "grant"            // hardn-grant-alice (NOPASSWD), no env file
	hostLegacy     = "legacy"           // origin/main's /etc/sudoers.d/alice, its grant and line
	hostEdited     = "edited"           // hardn-env-alice edited by hand
	hostNotRoots   = "not-roots"        // hardn-env-alice kept, owned by uid 1000: the journal refuses the stop
	hostVisudo     = "visudo-fails"     // visudo -c refuses the configuration
)

// testHost is one host: the fake, the facts detect gives of it, and
// hardn's environment there.
type testHost struct {
	*hostfake.System
	facts detect.Facts
	env   map[string]string
}

// newHost returns the host named name: its elevation files and tools, the
// directories the engine writes in, the kernel's hostname, and hardn's
// binary for the revert timer, every directory above it root's.
func newHost(t *testing.T, name string) *testHost {
	t.Helper()
	h := &testHost{System: hostfake.New(host.Options{})}
	for _, d := range []string{"/etc", "/etc/init.d", "/etc/runlevels", "/etc/runlevels/default", "/var", "/var/lib", "/var/backups", "/run", "/var/log", "/usr", "/usr/local", "/usr/local/bin", "/proc", "/proc/sys", "/proc/sys/kernel"} {
		h.AddDir(d, 0o755, 0, 0)
	}
	h.AddFile(hardnBinary, []byte("ELF"), 0o755, 0, 0)
	h.AddFile(header.HostnameFile, []byte(shortName+"\n"), 0o644, 0, 0)
	visudoFails := false
	switch name {
	case hostDebian, hostRootLogin, hostKept, hostGrant, hostLegacy, hostEdited, hostNotRoots, hostVisudo:
		h.sudo(detect.DistroDebian, "13", detect.SudoOriginal)
	case hostUbuntu:
		h.sudo(detect.DistroUbuntu, "26.04", detect.SudoRS)
	case hostAlpine:
		h.doas("3.24", "20-wheel.conf", "permit persist :wheel\n")
	case hostSetenv:
		h.doas("3.24", "20-wheel.conf", "permit persist setenv { HARDN_CONFIG } :wheel\n")
	case hostOverridden:
		h.doas("3.23", "wheel.conf", "permit nopass :wheel\n")
	case hostPVE:
		h.facts = platform(detect.DistroProxmox, "9", detect.Systemd)
		h.facts.Elevation = detect.Elevation{Sudo: detect.Tool{Present: detect.No}, Doas: detect.Tool{Present: detect.No}}
	default:
		t.Fatalf("no host %q", name)
	}
	h.env = viaSudo
	switch name {
	case hostAlpine, hostOverridden:
		h.env = viaDoas
	case hostSetenv:
		h.env = viaDoasKe
	case hostPVE, hostRootLogin:
		h.env = atRoot
	case hostKept, hostLegacy, hostNotRoots:
		h.env = viaSudoKe
	case hostVisudo:
		visudoFails = true
	}
	switch name {
	case hostOverridden:
		h.with(t, sudoers.Policy{User: "alice", Grant: sudoers.GrantPassword, Env: []string{"HARDN_CONFIG"}})
	case hostKept, hostNotRoots:
		h.with(t, sudoers.Policy{User: "alice", Env: []string{"HARDN_CONFIG"}})
	case hostGrant:
		h.with(t, sudoers.Policy{User: "alice", Grant: sudoers.GrantNoPassword})
	case hostLegacy:
		h.AddFile(sudoers.DropInDir+"/alice", []byte(legacyLines), 0o440, 0, 0)
	case hostEdited:
		h.AddFile(envFile, []byte(editedEnv), 0o440, 0, 0)
	}
	if name == hostNotRoots {
		e, _ := h.Entry(envFile)
		h.AddFile(envFile, e.Data, 0o440, 1000, 1000)
	}
	h.Handler = func(argv []string, _ []byte) hostfake.Response {
		cmd := strings.Join(argv, " ")
		switch {
		case cmd == "visudo -c" && visudoFails:
			return hostfake.Response{Stderr: sudoers.DropInDir + "/alice:1:21: syntax error\n", ExitCode: 1}
		case cmd == "visudo -c":
			return hostfake.Response{Stdout: sudoers.MainConfig + ": parsed OK\n"}
		case len(argv) == 3 && argv[0] == "visudo" && argv[1] == "-cf":
			return hostfake.Response{Stdout: argv[2] + ": parsed OK\n"}
		case len(argv) == 3 && argv[0] == "doas" && argv[1] == "-C":
			return hostfake.Response{}
		// A lockout-risk plan on OpenRC: hardn's boot unit checked and
		// enabled, its link made as rc-update makes it, then the revert
		// timer's sleeper started.
		case len(argv) == 3 && argv[0] == "sh" && argv[1] == "-n", argv[0] == "/bin/sh":
			return hostfake.Response{}
		case cmd == "rc-update add "+revert.BootUnitName+" default":
			h.AddSymlink(revert.OpenRCDefault+"/"+revert.BootUnitName, revert.OpenRCInitDir+"/"+revert.BootUnitName)
			return hostfake.Response{}
		}
		return hostfake.Response{Err: hostfake.ErrUnscripted, ExitCode: -1}
	}
	return h
}

// sudo makes h a host of distro's release with sudo of flavor: Debian
// 13's /etc/sudoers in shape and /etc/sudoers.d.
func (h *testHost) sudo(distro detect.Distro, version string, flavor detect.SudoFlavor) {
	h.AddFile(sudoers.MainConfig, []byte(debianMain), 0o440, 0, 0)
	h.AddDir(sudoers.DropInDir, 0o755, 0, 0)
	h.facts = platform(distro, version, detect.Systemd)
	h.facts.Elevation = detect.Elevation{Sudo: detect.Tool{Present: detect.Yes, Path: "/usr/bin/sudo"}, SudoFlavor: flavor, Doas: detect.Tool{Present: detect.No}}
}

// doas makes h an Alpine host of version with doas: the stock doas.conf
// and the drop-in file holding rule.
func (h *testHost) doas(version, file, rule string) {
	h.AddFile(sudoers.DoasConfig, []byte(doasConf), 0o640, 0, 0)
	h.AddDir(sudoers.DoasDir, 0o750, 0, 0)
	h.AddFile(sudoers.DoasDir+"/"+file, []byte(rule), 0o640, 0, 0)
	h.facts = platform(detect.DistroAlpine, version, detect.OpenRC)
	h.facts.Elevation = detect.Elevation{Sudo: detect.Tool{Present: detect.No}, Doas: detect.Tool{Present: detect.Yes, Path: "/usr/bin/doas"}}
}

// platform is a supported release's facts, its init running.
func platform(distro detect.Distro, version string, init detect.InitKind) detect.Facts {
	return detect.Facts{Platform: detect.Platform{Distro: distro, Version: version, Support: detect.Supported},
		Init: detect.Init{Kind: init, Running: detect.Yes}, Container: detect.Container{Detected: detect.No}}
}

// with writes hardn's files for pol on h as sudoers.PlanFor plans them,
// so that their bytes are the package's own.
func (h *testHost) with(t *testing.T, pol sudoers.Policy) {
	t.Helper()
	st, err := sudoers.Read(context.Background(), h, sudoers.Choose(h.facts, sudoers.ToolUnknown), []string{pol.User})
	if err != nil {
		t.Fatal(err)
	}
	p, err := sudoers.PlanFor(st, []sudoers.Policy{pol})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range p.Changes {
		h.AddFile(c.Path, c.Data, c.Mode, 0, 0)
	}
}

// session is the session applying a change, an SSH one, as the journal
// of a lockout-risk plan needs (revert.ReadSession).
var session = revert.Session{Remote: true, Conn: "192.0.2.10 50022 192.0.2.20 22", Source: "SSH_CONNECTION"}

// played is one run of the screen: what it wrote, with the answers
// echoed, what each change did (Options.Ran), how many engines it made,
// what Run returned and how many lines were left unread.
type played struct {
	out     string
	runs    []output.Run
	engines int
	err     error
	unread  int
}

// play runs the Environment screen on h, plain at 120 columns (not a
// terminal), the user typing lines; a dry run with dryRun.
func play(h *testHost, dryRun bool, lines ...string) played {
	return playWith(h, dryRun, nil, lines...)
}

// playWith is play with the options set changes, when not nil. The
// change's engine is built as cmd/hardn builds it: its clock, h's facts,
// the default backups, an SSH session, hardn's path for the revert timer.
func playWith(h *testHost, dryRun bool, set func(*envscreen.Options), lines ...string) played {
	var out bytes.Buffer
	env := term.Resolve(term.Options{}, term.Facts{Width: 120})
	ty := uitest.NewTypist(&out, lines...)
	var p played
	o := envscreen.Options{
		Prompter: nav.Prompter{Ctx: component.NewCtx(env), In: prompt.New(ty, false), Out: env.Writer(&out)},
		System:   h, Facts: h.facts, Getenv: lookup(h.env), DryRun: dryRun, LogPath: output.LegacyLogPath,
		Now: func() time.Time { return now },
		NewEngine: func(progress func(engine.Event)) *engine.Engine {
			p.engines++
			return engine.New(h, engine.Options{
				Revert:   revert.Options{Now: func() time.Time { return now }, Hardn: hardnBinary},
				Detect:   func(context.Context, host.System) (detect.Facts, error) { return h.facts, nil },
				Hold:     func() func() { return func() {} },
				PID:      5000,
				Session:  session,
				Backups:  backup.Settings{Path: backup.DefaultPath, Enabled: true},
				Progress: progress,
			})
		},
		Ran: func(r output.Run) { p.runs = append(p.runs, r) },
	}
	if set != nil {
		set(&o)
	}
	p.err = envscreen.Run(context.Background(), o)
	p.out, p.unread = out.String(), ty.Unread()
	return p
}

// writes are the calls on h, from the nth on, that could change the host
// outside /run/hardn, the engine's run directory: every call but a read
// or a lookup, a command run or started whatever it does, a lock taken.
func writes(h *testHost, n int) []string {
	var out []string
	for _, c := range h.Calls()[n:] {
		switch c.Op {
		case "read", "read-nofollow", "stat", "lstat", "readdir", "readlink", "resolve-symlink", "lookup-user", "lookup-group":
		case "run", "start-detached":
			out = append(out, c.Op+" "+strings.Join(c.Argv, " "))
		default:
			if !strings.HasPrefix(c.Path, "/run/hardn") {
				out = append(out, c.Op+" "+c.Path)
			}
		}
	}
	return out
}

// changed are the files under /etc the calls from the nth on wrote or
// removed, in order: hardn's journal, backups and run directory left out.
func changed(h *testHost, n int) []string {
	var out []string
	for _, c := range h.Calls()[n:] {
		switch c.Op {
		case "write-atomic", "write-in-place", "remove", "removeall", "rename":
			if strings.HasPrefix(c.Path, "/etc/") {
				out = append(out, c.Op+" "+c.Path)
			}
		}
	}
	return out
}

// content is name's content on h, or "<absent>".
func content(h *testHost, name string) string {
	e, ok := h.Entry(name)
	if !ok {
		return "<absent>"
	}
	return string(e.Data)
}

// entry is name's mode and owner on h.
func entry(h *testHost, name string) (fs.FileMode, int, int) {
	e, _ := h.Entry(name)
	return e.Mode.Perm(), e.UID, e.GID
}

// screens are the Environment screens out shows, each from its title bar
// to its prompt: one per round.
func screens(out string) []string {
	var got []string
	for _, part := range strings.Split(out, "# Environment Variable Settings #")[1:] {
		end := strings.Index(part, "> Enter selection")
		if end < 0 {
			end = len(part)
		}
		got = append(got, part[:end])
	}
	return got
}

// row is the value of the screen's row key in s, its note after it, ""
// when it has none; an item's line is no row.
func row(s, key string) string {
	for _, l := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(l); strings.Contains(t, key+"  ") && !strings.Contains(t, ") "+key) {
			_, v, _ := strings.Cut(t, key)
			return strings.Join(strings.Fields(v), " ")
		}
	}
	return ""
}

// item is item key's label and description in s, as one line; "" when
// it has none.
func item(s, key string) string {
	for _, l := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(l); strings.HasPrefix(t, key+") ") {
			return strings.Join(strings.Fields(strings.TrimPrefix(t, key+") ")), " ")
		}
	}
	return ""
}

// flat is out with each callout's continued lines joined ("\n# "), a
// word broken after its hyphen made whole again, its white space as
// single spaces, so that a wrapped sentence reads whole.
func flat(out string) string {
	out = strings.ReplaceAll(out, "-\n# ", "-")
	return strings.Join(strings.Fields(strings.ReplaceAll(out, "\n# ", " ")), " ")
}

// inOrder returns the first of want that out does not show after what
// came before it, each compared flat; "" when it shows them all, in
// order.
func inOrder(out string, want ...string) string {
	f, at := flat(out), 0
	for _, w := range want {
		i := strings.Index(f[at:], strings.Join(strings.Fields(w), " "))
		if i < 0 {
			return w
		}
		at += i + 1
	}
	return ""
}
