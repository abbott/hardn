// Package ssh holds the SSH area's controls (refactor plan Phase 4,
// next/p4-check-ssh): SSH Login (root's login), SSH Auth (key-only
// authentication) and SSH Port (a port other than 22), Figma's three SSH
// rows. In v0.4 they are check-only: Check reads the host, and Plan
// returns engine.ErrNotMigrated (engine.CheckOnly) until slice 6 migrates
// them.
//
// Each Check reads the host as sshd does, through the Phase 2 readers and
// never from the files alone (brief §3 principle 3): sshd -T over the
// installed files, with ssh.socket's listeners where the socket may hold
// them (sshd.ReadEffective); the files' Match blocks (migrate.Find,
// sshd.ReadOrder); the bound sockets (reload.ReadBound); and on Proxmox VE
// pve.CheckSSH. A read that fails is the check's error, which the engine
// reports as Unknown; what it read but cannot establish a state from is
// Unknown with findings: never a guess. docs/engine/check-ssh.md has every
// status line each control can show.
package ssh

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/pve"
	"github.com/abbott/hardn/pkg/host/reload"
	"github.com/abbott/hardn/pkg/host/sshd"
	"github.com/abbott/hardn/pkg/host/sshd/migrate"
)

// The finding codes of this package (pkg/output's registry holds them).
const (
	// CodeMatchAllowsMore: a Match block lets in more than the global
	// value does, for the connections it matches (root's login, a
	// password).
	CodeMatchAllowsMore = "SSH_MATCH_ALLOWS_MORE"
	// CodeNotInEffect: a listener sshd's configuration names is not bound,
	// so the configuration is not what accepts connections.
	CodeNotInEffect = "SSH_PORT_NOT_IN_EFFECT"
)

// Controls are the SSH controls in Figma's order: SSH Login, SSH Auth, SSH
// Port.
func Controls() []engine.Control { return []engine.Control{Login{}, Auth{}, Port{}} }

// reading is one reading of sshd's configuration.
type reading struct {
	eff   sshd.Effective
	found []sshd.Finding // ReadEffective's
	files migrate.Found  // sshd_config and its drop-ins, for their Match blocks
}

// read reads sshd's configuration: on OpenRC the binary Alpine's init
// script runs (reload.OpenRCFlavour), sshd -T over the installed files and
// ssh.socket's listeners (sshd.ReadEffective), then the files themselves
// (migrate.Find), as pkg/engine/steps reads them for a plan.
func read(ctx context.Context, sys host.System, f detect.Facts) (reading, error) {
	var r reading
	var bin string
	if f.Init.Kind == detect.OpenRC {
		b, err := reload.OpenRCFlavour(ctx, sys)
		if err != nil {
			return r, err
		}
		bin = b
	}
	var err error
	if r.eff, r.found, err = sshd.ReadEffective(ctx, sys, sshd.EffectiveRequest{SSH: f.SSH, Binary: bin}); err != nil {
		return r, fmt.Errorf("sshd's effective configuration: %w", err)
	}
	if r.files, err = migrate.Find(ctx, sys, migrate.FindRequest{}); err != nil {
		return r, fmt.Errorf("sshd's files: %w", err)
	}
	return r, nil
}

// allows ranks each value of a keyword by what it lets in: the higher, the
// more.
var allows = map[string]map[string]int{
	"permitrootlogin":              {"no": 0, "forced-commands-only": 1, "prohibit-password": 2, "yes": 3},
	"passwordauthentication":       {"no": 0, "yes": 1},
	"kbdinteractiveauthentication": {"no": 0, "yes": 1},
}

// matchAllows lists the lines under a Match block that set keyword k to a
// value that lets in more than global, sshd -T's value. Which connections
// a block matches is not evaluated, so every such block counts. ok is
// false when such a line's value is not one allows ranks, so what the
// block allows is not established.
func (r reading) matchAllows(k, global string) (more []sshd.Line, ok bool) {
	for _, l := range sshd.ReadOrder(r.files.Main, r.files.DropIns) {
		if l.Match == nil || l.Keyword != k {
			continue
		}
		v := ""
		if len(l.Args) > 0 {
			v = strings.ToLower(l.Args[0])
		}
		if v == "without-password" {
			v = "prohibit-password"
		}
		rank, known := allows[k][v]
		if !known {
			return nil, false
		}
		if rank > allows[k][global] {
			more = append(more, l)
		}
	}
	return more, true
}

// matchFindings are ls (matchAllows') as findings, and as evidence.
func matchFindings(ls []sshd.Line) (found []engine.Finding, ev []string) {
	for _, l := range ls {
		found = append(found, engine.Finding{Code: CodeMatchAllowsMore, Detail: fmt.Sprintf("%q under %q", l.Text, l.Match.Text), Source: at(l.File, l.Line)})
		ev = append(ev, fmt.Sprintf("%s: %s (%s)", at(l.File, l.Line), l.Text, l.Match.Text))
	}
	return found, ev
}

// said is the evidence of sshd -T's values for keys, as it printed them.
func (r reading) said(keys ...string) []string {
	var ev []string
	for _, k := range keys {
		vs := r.eff.Keywords[k]
		if len(vs) == 0 {
			ev = append(ev, "sshd -T: no "+k)
		}
		for _, v := range vs {
			ev = append(ev, "sshd -T: "+k+" "+v)
		}
	}
	return ev
}

// conflicts are ReadEffective's findings of another file's line that sets
// one of keys where hardn's 00-hardn.conf sets it too: read before hardn's
// (overridden: sshd uses it), after it (shadowed: no effect while hardn's
// line is there), or adding values (accumulates). Match blocks are
// matchAllows'.
func (r reading) conflicts(keys ...string) []engine.Finding {
	var out []engine.Finding
	for _, c := range r.eff.Conflicts {
		if c.Kind == sshd.FindingMatchOverride || !slices.Contains(keys, c.Keyword) {
			continue
		}
		for _, f := range r.found {
			if f.Kind == c.Kind && f.File == c.Other.File && f.Line == c.Other.Line {
				out = append(out, sshdFinding(f))
				break
			}
		}
	}
	return out
}

// noReport starts the note Evaluate adds when hardn's lines set no keyword
// (no 00-hardn.conf): about the conflict report, not about a value.
const noReport = "the conflict report is not established"

// effKeys are the keywords ReadEffective's sources-disagree and
// effective-unknown findings can name, in sshd -T's lower case
// (sshd-effective.md §2.2 to §2.6).
var effKeys = []string{"permitrootlogin", "passwordauthentication", "kbdinteractiveauthentication", "pubkeyauthentication",
	"port", "listenaddress", "allowusers", "allowgroups", "denyusers", "denygroups", "acceptenv"}

// unknowns are ReadEffective's findings that say why a value of keys is
// Unknown (sources-disagree, effective-unknown), for a status Unknown for
// that reason. A finding has no keyword field: one whose detail names a
// keyword of effKeys is kept when it is one of keys, and one that names
// none (sshd -T's output unreadable, the unit that listens not up) is
// kept for every control.
func (r reading) unknowns(keys ...string) []engine.Finding {
	var out []engine.Finding
	for _, f := range r.found {
		if f.Kind != sshd.FindingDisagree && (f.Kind != sshd.FindingUnknown || strings.HasPrefix(f.Detail, noReport)) {
			continue
		}
		named := slices.DeleteFunc(slices.Clone(effKeys), func(k string) bool { return !strings.Contains(f.Detail, k) })
		if len(named) == 0 || slices.ContainsFunc(named, func(k string) bool { return slices.Contains(keys, k) }) {
			out = append(out, sshdFinding(f))
		}
	}
	return out
}

// proxmox is pve.CheckSSH over this reading, with root's groups when
// withRoot (RootByKey needs them; Port22 does not).
func (r reading) proxmox(ctx context.Context, sys host.System, p detect.Proxmox, withRoot bool) (pve.SSHCheck, error) {
	var g pve.Groups
	if withRoot {
		var err error
		if g, err = pve.RootGroups(ctx, sys); err != nil {
			return pve.SSHCheck{}, err
		}
	}
	return pve.CheckSSH(p, pve.SSHInput{Effective: r.eff, Main: r.files.Main, DropIns: r.files.DropIns, Root: g})
}

// failed is the status of a check whose reading failed: Unknown, saying
// what the error shows of the host where it does, with the error, which
// Engine.Check reports (its code is pkg/output's: SSHD_ENV_NO_RUNDIR,
// COMMAND_NOT_FOUND, SSHD_CONFIG_INVALID, ...).
func failed(s detect.SSH, err error) (engine.Status, error) {
	var env *sshd.EnvError
	var pe *fs.PathError
	d := "can't read"
	switch {
	case errors.Is(err, host.ErrNotFound):
		d = "sshd not installed"
	case errors.As(err, &pe) && pe.Path == sshd.MainConfig && errors.Is(err, fs.ErrNotExist):
		d = "no sshd_config"
	case errors.As(err, &env) && env.Kind == sshd.EnvPrivsepDir && s.Mode == detect.SSHSocket:
		d = "sshd not started yet" // ssh.socket starts it at the first connection (steps.md §2.15)
	case errors.As(err, &env) && env.Kind == sshd.EnvPrivsepDir && s.Mode == detect.SSHService:
		d = "sshd not running" // /run/sshd is gone while ssh.service is stopped
	case errors.Is(err, sshd.ErrConfig):
		d = "sshd config invalid"
	}
	return engine.Status{State: engine.Unknown, Detail: d}, err
}

// unknown is an Unknown status with detail, evidence and findings.
func unknown(detail string, ev []string, found []engine.Finding) engine.Status {
	return engine.Status{State: engine.Unknown, Detail: detail, Evidence: ev, Findings: found}
}

func sshdFinding(f sshd.Finding) engine.Finding {
	return engine.Finding{Code: code("SSHD", f.Kind.String()), Detail: f.Detail, Source: at(f.File, f.Line)}
}

func pveFinding(f pve.Finding) engine.Finding {
	return engine.Finding{Code: code("PVE", f.Kind.String()), Detail: f.Detail, Source: at(f.File, f.Line)}
}

// code is a primitive's finding kind ("root-not-key-only") as a code
// under its prefix ("PVE_ROOT_NOT_KEY_ONLY"), as pkg/engine/steps derives
// them (steps.md §2.23).
func code(prefix, kind string) string {
	k := strings.ToUpper(strings.ReplaceAll(kind, "-", "_"))
	if strings.HasPrefix(k, prefix+"_") {
		return k
	}
	return prefix + "_" + k
}

// at is file:line, file alone, or "".
func at(file string, line int) string {
	if file != "" && line > 0 {
		return fmt.Sprintf("%s:%d", file, line)
	}
	return file
}
