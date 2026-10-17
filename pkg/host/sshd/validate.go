package sshd

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
)

const (
	// DefaultBinary is the sshd Validate runs, resolved from host.SearchPath.
	DefaultBinary = "sshd"
	// PrivsepDir is the privilege separation directory sshd -t and -T need,
	// as root, on Debian, Ubuntu and Proxmox VE. Only ssh.service creates it
	// (RuntimeDirectory=sshd); Validate never does.
	PrivsepDir = "/run/sshd"
	// WorkDir is the directory Validate creates in the run directory for the
	// temporary copy, and removes when it is done.
	WorkDir = "sshd-validate"
)

// Change is one file of the candidate configuration.
type Change struct {
	// Path is MainConfig, or a file in DropInDir whose name sshd's Include
	// matches (ends in ".conf", does not start with ".").
	Path string
	Data []byte
	// Remove validates the configuration as if the drop-in Path were gone
	// (a migration removing a legacy drop-in). Not allowed for MainConfig.
	Remove bool
}

// Request is what Validate checks.
type Request struct {
	// Changes overlay the files on the host: the configuration checked is
	// the host's, with these files replaced or removed.
	Changes []Change
	// RunDir is an existing absolute directory, reached with no symlink,
	// whose every directory from / is owned by root and writable by neither
	// group nor others. The copy is made in RunDir/WorkDir, which must not
	// exist yet.
	RunDir string
	// Platform decides the PrivsepDir check (detect.Detect's Platform).
	Platform detect.Platform
	// Binary is the sshd to run; "" is DefaultBinary. Alpine's init script
	// may run sshd.pam instead (p2-alpine, p2-reload).
	Binary string
	// Conn is where every Match probe starts; the zero value is
	// DefaultConn.
	Conn Conn
}

// Block is a Match line of the configuration.
type Block struct {
	File string // the real path: MainConfig or a drop-in
	Line int
	Text string
}

// Probe is the sshd -T -C run for one Match block.
type Probe struct {
	Block Block
	Conn  Conn
	// Reasons lists the criteria Conn could not be made to satisfy; empty
	// means it satisfies every one, so Output shows the block applied.
	Reasons []string
	Output  []byte // sshd -T -C's stdout
}

// FindingKind names a finding.
type FindingKind uint8

const (
	// FindingClobberable: no Include, so hardn's block sits at the top of
	// sshd_config, which cloud-init (ssh_pwauth) and Proxmox VE (pvecm
	// create and add set PermitRootLogin yes) rewrite.
	FindingClobberable FindingKind = iota + 1
	// FindingIncludeNotFirst: Include mode IncludeOther.
	FindingIncludeNotFirst
	// FindingMatchUntargeted: a Probe has Reasons.
	FindingMatchUntargeted
	// FindingWarning: sshd -t passed, and complained about a line.
	FindingWarning
)

func (k FindingKind) String() string {
	switch k {
	case FindingClobberable:
		return "clobberable"
	case FindingIncludeNotFirst:
		return "include-not-first"
	case FindingMatchUntargeted:
		return "match-untargeted"
	case FindingWarning:
		return "sshd-warning"
	}
	return "unknown"
}

// Finding is something the caller reports; it does not fail validation.
type Finding struct {
	Kind   FindingKind
	File   string
	Line   int
	Detail string
}

// Report is what Validate found. On an error it holds what was established
// before it.
type Report struct {
	// Include classifies the sshd_config the candidate leaves in place.
	Include Include
	// Files are the real paths of the configuration checked, in the order
	// sshd reads the drop-ins after sshd_config.
	Files     []string
	Effective []byte // sshd -T's stdout
	Probes    []Probe
	Findings  []Finding
	// Cleanup is the error removing the copy gave; it does not change the
	// verdict.
	Cleanup error
}

// Validate checks the host's sshd configuration with req.Changes applied,
// as sshd will read it once they are installed, and installs nothing. It
// returns nil only when sshd -t, sshd -T and every Match probe exit 0.
// Errors: *ConfigError (ErrConfig), *EnvError (ErrEnvironment), ErrUnexpected,
// ErrUnsupportedInclude, ErrNotIncluded, ErrUnsafeRunDir, ErrNotRegular,
// fs.ErrInvalid for a bad request, fs.ErrExist when RunDir/WorkDir exists,
// and the host's own errors (host.ErrNotFound when sshd is not installed).
func Validate(ctx context.Context, sys host.System, req Request) (rep Report, err error) {
	if err := ctx.Err(); err != nil {
		return rep, fmt.Errorf("sshd: validate: %w", err)
	}
	if req.Binary == "" {
		req.Binary = DefaultBinary
	}
	if req.Conn == (Conn{}) {
		req.Conn = DefaultConn()
	}
	if err := req.Conn.check(); err != nil {
		return rep, fmt.Errorf("%w: %w", err, fs.ErrInvalid)
	}
	if err := checkRunDir(ctx, sys, req.RunDir); err != nil {
		return rep, err
	}
	c, err := assemble(ctx, sys, req.Changes, &rep)
	if err != nil {
		return rep, err
	}
	if needsPrivsepDir(req.Platform) {
		if err := checkPrivsepDir(ctx, sys); err != nil {
			return rep, err
		}
	}
	work := filepath.Join(req.RunDir, WorkDir)
	if _, err := sys.Lstat(ctx, work); !errors.Is(err, fs.ErrNotExist) {
		if err == nil {
			err = fs.ErrExist
		}
		return rep, fmt.Errorf("sshd: %s: %w", work, err)
	}
	// work was absent, and only root can create a name in RunDir, so what
	// is there from now on is this call's to remove.
	defer func() { rep.Cleanup = sys.RemoveAll(context.WithoutCancel(ctx), work) }()
	if err := sys.MkdirAll(ctx, work+"/sshd_config.d", 0o700); err != nil {
		return rep, fmt.Errorf("sshd: %w", err)
	}
	err = c.check(ctx, sys, req, work, &rep)
	return rep, err
}

// config is the configuration to check: sshd_config and the drop-ins its
// Include reads, real paths and content.
type config struct {
	main  []byte
	drops []dropIn
}

// dropIn is one drop-in of the configuration checked.
type dropIn struct {
	path string // real path
	data []byte
}

// assemble reads the host's configuration, applies changes, and refuses
// what the copy could not reproduce.
func assemble(ctx context.Context, sys host.System, changes []Change, rep *Report) (c config, err error) {
	set, err := changeSet(changes)
	if err != nil {
		return c, err
	}
	if ch := set[MainConfig]; ch != nil {
		c.main = ch.Data
	} else if c.main, err = readRegular(ctx, sys, MainConfig); err != nil {
		return c, err
	}
	rep.Include = ParseInclude(c.main)
	switch rep.Include.Mode {
	case IncludeNone:
		rep.Findings = append(rep.Findings, Finding{FindingClobberable, MainConfig, 0,
			"no Include of " + DropInDir + ": hardn's block at the top of sshd_config can be rewritten by cloud-init and Proxmox VE"})
		for p, ch := range set {
			if p != MainConfig && !ch.Remove {
				return c, fmt.Errorf("sshd: %s: %w", p, ErrNotIncluded)
			}
		}
	case IncludeOther:
		rep.Findings = append(rep.Findings, Finding{FindingIncludeNotFirst, MainConfig, rep.Include.Line,
			"the first directive is not exactly " + IncludeLine + ": earlier settings beat the drop-ins"})
		fallthrough
	default:
		if c.drops, err = dropIns(ctx, sys, set); err != nil {
			return c, err
		}
	}
	for _, d := range includes(c.main) {
		if d.text != IncludeLine {
			return c, fmt.Errorf("sshd: %s line %d: %q: %w", MainConfig, d.line, d.text, ErrUnsupportedInclude)
		}
	}
	rep.Files = append(rep.Files, MainConfig)
	for _, d := range c.drops {
		if ds := includes(d.data); len(ds) > 0 {
			return c, fmt.Errorf("sshd: %s line %d: %q: %w", d.path, ds[0].line, ds[0].text, ErrUnsupportedInclude)
		}
		rep.Files = append(rep.Files, d.path)
	}
	return c, nil
}

// check writes the copy into work and runs sshd on it. Only IncludeLine is
// rewritten, to point at the copy of the drop-ins (assemble refused any
// other Include: it may read the real drop-in directory, directly or through
// another file, and the copy would then check the old drop-ins).
func (c config) check(ctx context.Context, sys host.System, req Request, work string, rep *Report) error {
	cfg := work + "/sshd_config"
	lines := strings.Split(string(c.main), "\n")
	for _, d := range includes(c.main) {
		lines[d.line-1] = "Include " + work + "/sshd_config.d/*.conf"
	}
	if err := sys.WriteFileAtomic(ctx, cfg, []byte(strings.Join(lines, "\n")), 0o600, -1, -1); err != nil {
		return fmt.Errorf("sshd: %w", err)
	}
	for _, d := range c.drops {
		if err := sys.WriteFileAtomic(ctx, work+"/sshd_config.d/"+filepath.Base(d.path), d.data, 0o600, -1, -1); err != nil {
			return fmt.Errorf("sshd: %w", err)
		}
	}
	paths := strings.NewReplacer(work+"/sshd_config.d/", DropInDir+"/", cfg, MainConfig)
	run := func(mode string, extra ...string) (host.Result, error) {
		res, err := sys.Run(ctx, req.Binary, append([]string{mode, "-f", cfg}, extra...)...)
		return res, classify(res, err, paths)
	}

	res, err := run("-t")
	if err != nil {
		return err
	}
	for _, p := range problems(paths.Replace(string(res.Combined))) {
		rep.Findings = append(rep.Findings, Finding{FindingWarning, p.File, p.Line, p.Message})
	}
	if res, err = run("-T"); err != nil {
		return err
	}
	rep.Effective = res.Stdout
	for _, m := range c.matches() {
		p := Probe{Block: m.Block}
		p.Conn, p.Reasons = target(m.args, req.Conn)
		if err := p.Conn.check(); err != nil {
			p.Conn, p.Reasons = req.Conn, append(p.Reasons, err.Error())
		}
		if len(p.Reasons) > 0 {
			rep.Findings = append(rep.Findings, Finding{FindingMatchUntargeted, m.File, m.Line, strings.Join(p.Reasons, "; ")})
		}
		if res, err = run("-T", "-C", p.Conn.Spec()); err != nil {
			return err
		}
		p.Output = res.Stdout
		rep.Probes = append(rep.Probes, p)
	}
	return nil
}

// match is a Match line and its criteria.
type match struct {
	Block
	args []string
}

// matches lists the Match lines of sshd_config, then of each drop-in in
// sshd's order. A Match in a drop-in ends with that file (servconf.c
// restores the including file's Match state).
func (c config) matches() []match {
	var ms []match
	add := func(file string, data []byte) {
		for _, d := range directives(data) {
			if d.keyword == "match" {
				args, _ := splitArgs(d.args) // unbalanced quotes fail sshd -t first
				ms = append(ms, match{Block{file, d.line, d.text}, args})
			}
		}
	}
	add(MainConfig, c.main)
	for _, d := range c.drops {
		add(d.path, d.data)
	}
	return ms
}

// included reports whether sshd's "*.conf" matches name: glob(3) needs a
// leading "." to be matched explicitly, so the atomic write's temporary
// ".<name>.hardn-<random>" files never match.
func included(name string) bool {
	return strings.HasSuffix(name, ".conf") && !strings.HasPrefix(name, ".")
}

// changeSet checks changes and indexes them by path.
func changeSet(changes []Change) (map[string]*Change, error) {
	set := map[string]*Change{}
	for i, c := range changes {
		dir, name := filepath.Split(c.Path)
		switch {
		case set[c.Path] != nil:
			return nil, fmt.Errorf("sshd: %s changed twice: %w", c.Path, fs.ErrInvalid)
		case c.Path == MainConfig && c.Remove:
			return nil, fmt.Errorf("sshd: removing %s: %w", MainConfig, fs.ErrInvalid)
		case c.Path != MainConfig && (dir != DropInDir+"/" || !included(name)):
			return nil, fmt.Errorf("sshd: %s is neither %s nor a *.conf file in %s: %w", c.Path, MainConfig, DropInDir, fs.ErrInvalid)
		}
		set[c.Path] = &changes[i]
	}
	return set, nil
}

// dropIns reads the drop-ins sshd's Include matches, with set applied, in
// sshd's order.
func dropIns(ctx context.Context, sys host.System, set map[string]*Change) ([]dropIn, error) {
	entries, err := sys.ReadDir(ctx, DropInDir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("sshd: %w", err)
	}
	var names []string
	for _, e := range entries {
		if included(e.Name()) {
			names = append(names, e.Name())
		}
	}
	for p, c := range set {
		if name := filepath.Base(p); p != MainConfig && !c.Remove && !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
	slices.Sort(names) // C byte order, as sshd's glob sorts
	var drops []dropIn
	for _, name := range names {
		p := DropInDir + "/" + name
		var data []byte
		switch c := set[p]; {
		case c != nil && c.Remove:
			continue
		case c != nil:
			data = c.Data
		default:
			if data, err = readRegular(ctx, sys, p); err != nil {
				return nil, err
			}
		}
		drops = append(drops, dropIn{p, data})
	}
	return drops, nil
}

// readRegular reads name as sshd does, following a symlink, and refuses
// anything but a regular file (a FIFO would block the read).
func readRegular(ctx context.Context, sys host.System, name string) ([]byte, error) {
	fi, err := sys.Stat(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("sshd: %w", err)
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("sshd: %s: %w", name, ErrNotRegular)
	}
	data, err := sys.ReadFile(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("sshd: %w", err)
	}
	return data, nil
}

func includes(data []byte) []directive {
	var ds []directive
	for _, d := range directives(data) {
		if d.keyword == "include" {
			ds = append(ds, d)
		}
	}
	return ds
}

// checkRunDir requires dir to be absolute and clean, free of characters an
// Include argument would need quoted or would glob, and every directory from
// / to it to be a real directory owned by root and writable by neither group
// nor others, so no other user can change the copy sshd checks.
func checkRunDir(ctx context.Context, sys host.System, dir string) error {
	if !filepath.IsAbs(dir) || filepath.Clean(dir) != dir || dir == "/" || strings.ContainsAny(dir, " \t\r\n\"'\\#*?[]{}") {
		return fmt.Errorf("sshd: run directory %q: %w", dir, fs.ErrInvalid)
	}
	for p := dir; ; p = filepath.Dir(p) {
		fi, err := sys.Lstat(ctx, p)
		if err != nil {
			return fmt.Errorf("sshd: run directory: %w", err)
		}
		if !rootOnly(fi) {
			return fmt.Errorf("sshd: %s: %w", p, ErrUnsafeRunDir)
		}
		if p == "/" {
			return nil
		}
	}
}

// rootOnly: a directory (not a symlink to one, for an Lstat), owned by root,
// writable by neither group nor others.
func rootOnly(fi fs.FileInfo) bool {
	uid, _, ok := host.Owner(fi)
	return fi.IsDir() && ok && uid == 0 && fi.Mode().Perm()&0o022 == 0
}

// needsPrivsepDir: Debian's openssh, which Ubuntu and Proxmox VE share, is
// built with its privilege separation directory in /run/sshd.
func needsPrivsepDir(p detect.Platform) bool {
	switch p.Distro {
	case detect.DistroDebian, detect.DistroUbuntu, detect.DistroProxmox:
		return true
	case detect.DistroOther:
		return p.Like == detect.DistroDebian || p.Like == detect.DistroUbuntu
	}
	return false
}

// checkPrivsepDir is sshd's own check (sshd.c), made before sshd runs: a
// directory, owned by root, writable by neither group nor others.
func checkPrivsepDir(ctx context.Context, sys host.System) error {
	fi, err := sys.Stat(ctx, PrivsepDir)
	if err == nil && !rootOnly(fi) {
		err = errors.New("not a directory owned by root and writable only by root")
	}
	if err != nil {
		return &EnvError{Kind: EnvPrivsepDir, Path: PrivsepDir, Err: err}
	}
	return nil
}
