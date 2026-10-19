// Package migrate moves the SSH files earlier versions of hardn wrote into
// the one drop-in hardn now owns, /etc/ssh/sshd_config.d/00-hardn.conf
// (refactor plan P2, next/p2-sshd-migrate; decision §3.17, approved
// 2026-10-17: migrate the legacy artifacts automatically, with a backup and
// a printed note).
//
// Find reads the host and names the artifacts: v0.3.x's manage.conf (or the
// custom sshConfigFile path its hardn.yml named) and ssh.socket.d's
// listen.conf, the unreleased builds' hardn.conf, and an sshd_config hardn
// wrote whole on Alpine. PlanFor turns them, and the settings hardn wants,
// into steps without any I/O: legacy lines that take effect today move into
// 00-hardn.conf, every key file they name stays read (R8), and the legacy
// files are removed. Check validates the state the whole plan leads to
// with sshd.Validate and refuses one with fewer working login paths than
// the configuration installed now, writing nothing; Apply runs Check, takes
// a backup of every file it changes, checks that each is as the plan read
// it, and validates each step's state before writing; it reloads nothing.
// Plan.Reload is the reload.Change for the reload that follows.
// Everything goes through host.System.
//
// docs/safety/sshd-migrate.md describes the guarantees, the platform facts
// they rely on and what is written but not yet run on a Linux host.
package migrate

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/sshd"
)

// The legacy files hardn wrote (docs/refactor/phase0/side-effects.md §10).
const (
	// ManageConf is v0.3.1's and v0.3.2's default sshConfigFile.
	ManageConf = sshd.DropInDir + "/manage.conf"
	// HardnConf is the drop-in the unreleased builds after v0.3.2 hard-code.
	HardnConf = sshd.DropInDir + "/hardn.conf"
	// ListenConf is the ssh.socket drop-in v0.3.x wrote on every systemd host.
	ListenConf = "/etc/systemd/system/ssh.socket.d/listen.conf"
)

// What hardn's generated files start with, verbatim.
const (
	// markerV03 opens v0.3.x's drop-in (v0.3.2 pkg/ssh/ssh.go).
	markerV03 = "### Reference\n### https://cryptsus.com/blog/how-to-secure-your-ssh-server-with-public-key-elliptic-curve-ed25519-crypto.html\n"
	// markerMain opens the unreleased builds' file (FileSSHRepository.SaveSSHConfig).
	markerMain = "# SSH configuration managed by Hardn\n"
	// v0.3.x's Alpine sshd_config has no comment: it starts with alpineV03
	// and holds the line alpineV03Line (v0.3.2 pkg/ssh/ssh.go).
	alpineV03     = "Protocol 2\nStrictModes yes\n\nPort "
	alpineV03Line = "HostbasedAcceptedKeyTypes ecdsa-sha2-nistp256,ecdsa-sha2-nistp384,ecdsa-sha2-nistp521,ssh-ed25519"
)

// listenV03 is v0.3.x's listen.conf, exactly.
var listenV03 = regexp.MustCompile(`\A\[Socket\]\nListenStream=\nListenStream=([0-9]{1,5})\n\z`)

// Kind is which legacy file an Artifact is.
type Kind uint8

const (
	KindManageConf    Kind = iota + 1 // ManageConf (v0.3.x)
	KindCustom                        // the sshConfigFile path of a v0.3.x hardn.yml
	KindHardnConf                     // HardnConf (unreleased builds)
	KindListenConf                    // ListenConf (v0.3.x)
	KindGeneratedMain                 // sshd_config written whole by hardn (Alpine)
)

// String is the artifact's name in notes and errors, such as manage.conf.
func (k Kind) String() string {
	switch k {
	case KindManageConf:
		return "manage.conf"
	case KindCustom:
		return "sshConfigFile"
	case KindHardnConf:
		return "hardn.conf"
	case KindListenConf:
		return "listen.conf"
	case KindGeneratedMain:
		return "generated sshd_config"
	}
	return "unknown"
}

// Generation is which hardn wrote an artifact.
type Generation uint8

const (
	GenV03  Generation = iota + 1 // v0.3.1 or v0.3.2
	GenMain                       // a build of the unreleased main branch after v0.3.2
)

// String names the hardn that wrote an artifact, as the notes say it.
func (g Generation) String() string {
	switch g {
	case GenV03:
		return "hardn v0.3.x"
	case GenMain:
		return "an unreleased hardn build"
	}
	return "unknown"
}

// Artifact is a file a legacy hardn wrote, recognised by its content.
type Artifact struct {
	Kind  Kind
	Path  string
	Gen   Generation
	Data  []byte
	Ports []uint16 // KindListenConf: its ListenStream port
}

// Left is a file Find or PlanFor leaves as it is, and why.
type Left struct {
	Path string
	Why  string
}

// Found is what Find read.
type Found struct {
	Main    []byte // sshd_config
	Include sshd.Include
	// DropIns are sshd_config.d's *.conf files (no leading "."), in C byte
	// order, as sshd reads them once sshd_config includes them.
	DropIns   []sshd.File
	Artifacts []Artifact
	Left      []Left
}

// FindRequest says where to look besides the fixed paths.
type FindRequest struct {
	// ConfigFile is the sshConfigFile value of the loaded hardn.yml, "" for
	// none: v0.3.x wrote its drop-in there (the unreleased builds ignore
	// the key).
	ConfigFile string
}

// Find reads sshd_config, sshd_config.d, the legacy paths and ConfigFile,
// and recognises hardn's files by the lines hardn wrote first. It changes
// nothing. A file at a legacy path that hardn did not write, or that sshd
// does not read, is in Left. Errors: the host's (a missing sshd_config),
// sshd.ErrNotRegular for a configuration file that is not a regular file,
// and ctx's.
func Find(ctx context.Context, sys host.System, req FindRequest) (Found, error) {
	var f Found
	if err := ctx.Err(); err != nil {
		return f, fmt.Errorf("migrate: %w", err)
	}
	main, err := readRegular(ctx, sys, sshd.MainConfig)
	if err != nil {
		return f, err
	}
	f.Main, f.Include = main, sshd.ParseInclude(main)
	if f.DropIns, err = readDropIns(ctx, sys); err != nil {
		return f, err
	}
	if g := mainGen(main); g != 0 {
		f.Artifacts = append(f.Artifacts, Artifact{Kind: KindGeneratedMain, Path: sshd.MainConfig, Gen: g, Data: main})
	}
	for _, c := range candidates(req.ConfigFile, main, &f) {
		data, ok, err := readLegacy(ctx, sys, c.path, &f)
		if err != nil {
			return f, err
		}
		if !ok {
			continue
		}
		g := dropInGen(data)
		dir, name := path.Split(c.path)
		switch {
		case g == 0:
			f.Left = append(f.Left, Left{c.path, "it does not start with the lines hardn wrote: not hardn's, or edited at the top"})
		case dir != sshd.DropInDir+"/" || !included(name):
			f.Left = append(f.Left, Left{c.path, "written by " + g.String() + ", but sshd does not read it (not a *.conf file in " + sshd.DropInDir + ")"})
		default:
			f.Artifacts = append(f.Artifacts, Artifact{Kind: c.kind, Path: c.path, Gen: g, Data: data})
		}
	}
	data, ok, err := readLegacy(ctx, sys, ListenConf, &f)
	if err != nil || !ok {
		return f, err
	}
	port := 0
	if m := listenV03.FindSubmatch(data); m != nil {
		port, _ = strconv.Atoi(string(m[1]))
	}
	if port < 1 || port > 65535 {
		f.Left = append(f.Left, Left{ListenConf, "not the form hardn v0.3.x wrote ([Socket], ListenStream=, ListenStream=<port>)"})
		return f, nil
	}
	f.Artifacts = append(f.Artifacts, Artifact{Kind: KindListenConf, Path: ListenConf, Gen: GenV03, Data: data, Ports: []uint16{uint16(port)}})
	return f, nil
}

type candidate struct {
	kind Kind
	path string
}

// candidates are the legacy drop-in paths: the two fixed ones, and
// sshConfigFile when it names another file. An sshConfigFile hardn cannot
// act on goes to f.Left.
func candidates(configFile string, main []byte, f *Found) []candidate {
	cs := []candidate{{KindManageConf, ManageConf}, {KindHardnConf, HardnConf}}
	switch p := configFile; {
	case p == "" || p == ManageConf || p == HardnConf || p == sshd.HardnDropIn:
	case !path.IsAbs(p) || path.Clean(p) != p:
		f.Left = append(f.Left, Left{p, "sshConfigFile is not a clean absolute path, so where v0.3.x wrote it is not known"})
	case p == sshd.MainConfig:
		if dropInGen(main) != 0 {
			f.Left = append(f.Left, Left{p, "v0.3.x wrote its drop-in over sshd_config (sshConfigFile); restoring the distribution's file there is not automated"})
		}
	default:
		cs = append(cs, candidate{KindCustom, p})
	}
	return cs
}

// readLegacy reads a legacy path: ok is false when there is nothing to
// migrate there (absent, or not a regular file, which goes to f.Left).
func readLegacy(ctx context.Context, sys host.System, name string, f *Found) (data []byte, ok bool, err error) {
	fi, err := sys.Lstat(ctx, name)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil, false, nil
	case err != nil:
		return nil, false, fmt.Errorf("migrate: %w", err)
	case fi.Mode()&fs.ModeSymlink != 0:
		f.Left = append(f.Left, Left{name, "a symlink (hardn wrote a regular file)"})
		return nil, false, nil
	case !fi.Mode().IsRegular():
		f.Left = append(f.Left, Left{name, "not a regular file"})
		return nil, false, nil
	}
	if data, _, err = sys.ReadFileNoFollow(ctx, name); err != nil {
		return nil, false, fmt.Errorf("migrate: %w", err)
	}
	return data, true, nil
}

// dropInGen is the generation whose drop-in data starts as, or 0.
func dropInGen(data []byte) Generation {
	switch s := string(data); {
	case strings.HasPrefix(s, markerV03):
		return GenV03
	case strings.HasPrefix(s, markerMain):
		return GenMain
	}
	return 0
}

// mainGen is the generation that wrote sshd_config whole, or 0: the
// unreleased builds by their first line, v0.3.x by its first lines and
// its key-types line (it wrote no comment).
func mainGen(data []byte) Generation {
	s := string(data)
	switch {
	case strings.HasPrefix(s, markerMain):
		return GenMain
	case strings.HasPrefix(s, alpineV03) && strings.Contains("\n"+s, "\n"+alpineV03Line+"\n"):
		return GenV03
	}
	return 0
}

// readDropIns reads the drop-ins sshd's Include matches, following symlinks
// as sshd does; a missing directory holds none.
func readDropIns(ctx context.Context, sys host.System) ([]sshd.File, error) {
	entries, err := sys.ReadDir(ctx, sshd.DropInDir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	var drops []sshd.File
	for _, e := range entries { // sorted by name: C byte order
		if !included(e.Name()) {
			continue
		}
		p := sshd.DropInDir + "/" + e.Name()
		data, err := readRegular(ctx, sys, p)
		if err != nil {
			return nil, err
		}
		drops = append(drops, sshd.File{Path: p, Data: data})
	}
	return drops, nil
}

// readRegular reads name as sshd does, following a symlink, and refuses
// anything but a regular file.
func readRegular(ctx context.Context, sys host.System, name string) ([]byte, error) {
	fi, err := sys.Stat(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("migrate: %s: %w", name, sshd.ErrNotRegular)
	}
	data, err := sys.ReadFile(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return data, nil
}

// included reports whether sshd's "*.conf" matches name (glob(3) never
// matches a leading "." with "*").
func included(name string) bool {
	return strings.HasSuffix(name, ".conf") && !strings.HasPrefix(name, ".")
}
