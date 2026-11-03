package environment_test

import (
	"context"
	"io/fs"
	"strings"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/host/sudoers"
)

// The hosts the Environment control reads and plans on, on hostfake, as
// the gallery's Environment mocks draw them (gallery-environment.md
// §3.1): Debian 13 (sudo), Ubuntu 26.04 (sudo-rs), Alpine 3.24 after
// setup-alpine (doas, 20-wheel.conf), Alpine 3.23's cloud image (doas,
// wheel.conf, which sorts before hardn's rule, and a local zz-local.conf,
// which sorts after it), Proxmox VE 9 (neither
// tool). visudo and doas -C pass every file: the tests' model, not the
// tools. Synthetic values only: web-01.example.net, alice, bob.

const (
	hostname    = "web-01.example.net"
	hardnBinary = "/usr/local/bin/hardn"
	// debianMain is Debian 13's /etc/sudoers in shape: the %sudo rule,
	// then the includedir as the last directive (sudoers.md §3).
	debianMain = "Defaults\tenv_reset\nroot\tALL=(ALL:ALL) ALL\n%sudo\tALL=(ALL:ALL) ALL\n\n@includedir /etc/sudoers.d\n"
	// doasConf is Alpine's stock /etc/doas.conf, its rule commented out.
	doasConf = "# see doas.conf(5) for configuration details\n\n# permit persist :wheel\n"
	// legacyLines are origin/main's /etc/sudoers.d/<user> once both of
	// its writers ran (ops/setup_sudo_preservation/debian.golden, step 9,
	// frozen in pkg/screens/environment/testdata/legacy).
	legacyLines = "alice ALL=(ALL) NOPASSWD: ALL\nDefaults:alice env_keep += \"HARDN_CONFIG\"\n"
	keptValue   = "/home/alice/hardn.yml"
)

// now is the engine's clock and the reports'.
var now = time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)

// The environments hardn runs in: alice's sudo or doas, HARDN_CONFIG
// dropped or kept; a root login.
var (
	sudoAlice = map[string]string{"SUDO_USER": "alice", "USER": "root"}
	sudoKept  = map[string]string{"SUDO_USER": "alice", "USER": "root", "HARDN_CONFIG": keptValue}
	doasAlice = map[string]string{"DOAS_USER": "alice", "USER": "root"}
	rootLogin = map[string]string{"USER": "root"}
)

// lookup is getenv over vars.
func lookup(vars map[string]string) func(string) string {
	return func(k string) string { return vars[k] }
}

// The hosts, by name.
const (
	debian13    = "debian13"
	ubuntu2604  = "ubuntu2604"
	alpine324   = "alpine324"
	alpineCloud = "alpine-cloud"
	pve9        = "pve9"
)

// testHost is one host: the fake, and the facts detect gives of it.
type testHost struct {
	*hostfake.System
	facts detect.Facts
}

// newHost returns the host named name: its elevation files and tools, the
// directories the engine writes in, and hardn's binary for the revert
// timer.
func newHost(t *testing.T, name string) *testHost {
	t.Helper()
	h := &testHost{System: hostfake.New(host.Options{})}
	for _, d := range []string{"/etc", "/var/lib", "/var/backups", "/run", "/var/log", "/usr/local/bin"} {
		h.AddDir(d, 0o755, 0, 0)
	}
	h.AddFile(hardnBinary, []byte("ELF"), 0o755, 0, 0)
	sudo := detect.Tool{Present: detect.Yes, Path: "/usr/bin/sudo"}
	none := detect.Tool{Present: detect.No}
	doas := detect.Tool{Present: detect.Yes, Path: "/usr/bin/doas"}
	switch name {
	case debian13, ubuntu2604:
		h.AddFile(sudoers.MainConfig, []byte(debianMain), 0o440, 0, 0)
		h.AddDir(sudoers.DropInDir, 0o755, 0, 0)
		h.AddFile(sudoers.DropInDir+"/README", []byte("# files here are read by sudo\n"), 0o440, 0, 0)
		version, flavor := "13", detect.SudoOriginal
		h.facts = platform(detect.DistroDebian, version, detect.Systemd)
		if name == ubuntu2604 {
			flavor = detect.SudoRS
			h.facts = platform(detect.DistroUbuntu, "26.04", detect.Systemd)
		}
		h.facts.Elevation = detect.Elevation{Sudo: sudo, SudoFlavor: flavor, Doas: none}
	case alpine324, alpineCloud:
		h.AddFile(sudoers.DoasConfig, []byte(doasConf), 0o640, 0, 0)
		h.AddDir(sudoers.DoasDir, 0o750, 0, 0)
		file, rule, version := "20-wheel.conf", "permit persist :wheel\n", "3.24"
		if name == alpineCloud {
			file, rule, version = "wheel.conf", "permit nopass :wheel\n", "3.23"
		}
		h.AddFile(sudoers.DoasDir+"/"+file, []byte(rule), 0o640, 0, 0)
		if name == alpineCloud {
			h.AddFile(sudoers.DoasDir+"/zz-local.conf", []byte(rule), 0o640, 0, 0)
		}
		h.facts = platform(detect.DistroAlpine, version, detect.OpenRC)
		h.facts.Elevation = detect.Elevation{Sudo: none, Doas: doas}
	case pve9:
		h.facts = platform(detect.DistroProxmox, "9", detect.Systemd)
		h.facts.Elevation = detect.Elevation{Sudo: none, Doas: none}
	default:
		t.Fatalf("no host %q", name)
	}
	h.Handler = func(argv []string, _ []byte) hostfake.Response {
		switch {
		case strings.Join(argv, " ") == "visudo -c":
			return hostfake.Response{Stdout: sudoers.MainConfig + ": parsed OK\n"}
		case len(argv) == 3 && argv[0] == "visudo" && argv[1] == "-cf":
			return hostfake.Response{Stdout: argv[2] + ": parsed OK\n"}
		case len(argv) == 3 && argv[0] == "doas" && argv[1] == "-C":
			return hostfake.Response{}
		}
		return hostfake.Response{Err: hostfake.ErrUnscripted, ExitCode: -1}
	}
	return h
}

// platform is a supported release's facts, its init running.
func platform(distro detect.Distro, version string, init detect.InitKind) detect.Facts {
	return detect.Facts{Platform: detect.Platform{Distro: distro, Version: version, Support: detect.Supported},
		Init: detect.Init{Kind: init, Running: detect.Yes}, Container: detect.Container{Detected: detect.No}}
}

// with writes hardn's files for pol on h as sudoers.PlanFor plans them,
// so that their bytes are the package's own.
func (h *testHost) with(t *testing.T, pol sudoers.Policy) *testHost {
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
	return h
}

// moded gives name on h, a file of hardn's that with wrote, mode perm and
// owner uid:gid, its bytes as they are: hardn's content, another mode or
// owner.
func (h *testHost) moded(t *testing.T, name string, perm fs.FileMode, uid, gid int) *testHost {
	t.Helper()
	e, ok := h.Entry(name)
	if !ok {
		t.Fatalf("%s: not there", name)
	}
	h.AddFile(name, e.Data, perm, uid, gid)
	return h
}

// session is the session applying a change, an SSH one, as the journal of
// a lockout-risk plan needs (revert.ReadSession).
var session = revert.Session{Remote: true, Conn: "192.0.2.10 50022 192.0.2.20 22", Source: "SSH_CONNECTION"}

// engineOn is the engine on h as cmd/hardn builds it: its clock, h's
// facts, the default backups, an SSH session, and the apply's progress
// to events when set.
func engineOn(h *testHost, events *[]engine.Event) *engine.Engine {
	return engine.New(h, engine.Options{
		Revert:  revert.Options{Now: func() time.Time { return now }, Hardn: hardnBinary},
		Detect:  func(context.Context, host.System) (detect.Facts, error) { return h.facts, nil },
		Hold:    func() func() { return func() {} },
		PID:     5000,
		Session: session,
		Backups: backup.Settings{Path: backup.DefaultPath, Enabled: true},
		Progress: func(ev engine.Event) {
			if events != nil {
				*events = append(*events, ev)
			}
		},
	})
}

// writesAfter are the calls after the first n that changed something
// outside /run/hardn.
func writesAfter(h *testHost, n int) []string {
	var out []string
	for _, c := range h.Calls()[n:] {
		switch c.Op {
		case "write-atomic", "write-in-place", "remove", "removeall", "rename", "mkdir", "chmod", "chown", "append":
			if !strings.HasPrefix(c.Path, "/run/hardn") {
				out = append(out, c.Op+" "+c.Path)
			}
		}
	}
	return out
}

// changedFiles are the files under /etc that the calls after the first n
// wrote or removed, in order: hardn's journal, backups and run directory
// left out.
func changedFiles(h *testHost, n int) []string {
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
