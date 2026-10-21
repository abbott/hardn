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
	"github.com/abbott/hardn/pkg/engine/steps"
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
	// CodeMatchValueUnknown: a Match block sets a row's keyword to a value
	// hardn does not rank, so what the block lets in is not established.
	CodeMatchValueUnknown = "SSH_MATCH_VALUE_UNKNOWN"
	// Code22Bound: a socket on port 22 is bound while no configured
	// listener is on 22: sshd not reloaded since 22 was dropped, or
	// another program (ss does not say whose a socket is).
	Code22Bound = "SSH_PORT_22_BOUND"
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
// value that lets in more than global, sshd -T's value, and in odd those
// that set it to a value allows does not rank, so what the block lets in
// is not established. Which connections a block matches is not evaluated,
// so every such block counts.
func (r reading) matchAllows(k, global string) (more, odd []sshd.Line) {
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
		switch rank, known := allows[k][v]; {
		case !known:
			odd = append(odd, l)
		case rank > allows[k][global]:
			more = append(more, l)
		}
	}
	return more, odd
}

// matchFindings are ls (matchAllows') as findings of code, and as
// evidence.
func matchFindings(ls []sshd.Line, code string) (found []engine.Finding, ev []string) {
	for _, l := range ls {
		text, match := strings.TrimSpace(l.Text), strings.TrimSpace(l.Match.Text)
		found = append(found, engine.Finding{Code: code, Detail: fmt.Sprintf("%q under %q", text, match), Source: steps.SourceAt(l.File, l.Line)})
		ev = append(ev, fmt.Sprintf("%s: %s (%s)", steps.SourceAt(l.File, l.Line), text, match))
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
				out = append(out, steps.SSHDFinding(f))
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
// (sshd-effective.md §2.2 to §2.6), usepam SSH Auth's.
var effKeys = []string{"permitrootlogin", "passwordauthentication", "kbdinteractiveauthentication", "pubkeyauthentication",
	"port", "listenaddress", "allowusers", "allowgroups", "denyusers", "denygroups", "acceptenv", "usepam"}

// keyWords are the other words by which those findings name a keyword:
// sshd -T's "ports", and the listeners' words (ssh.socket's Listen, "the
// listeners", "listening"), which are SSH Port's.
var keyWords = map[string]string{"ports": "port", "listen": "listenaddress", "listens": "listenaddress",
	"listening": "listenaddress", "listener": "listenaddress", "listeners": "listenaddress"}

// names lists the keywords of effKeys a finding's detail names: its whole
// words, compared case-insensitively ("Port", "ListenAddress"), and those
// of keyWords.
func names(detail string) []string {
	var out []string
	for _, w := range strings.FieldsFunc(strings.ToLower(detail), func(c rune) bool { return c < 'a' || c > 'z' }) {
		if k, ok := keyWords[w]; ok {
			w = k
		}
		if slices.Contains(effKeys, w) && !slices.Contains(out, w) {
			out = append(out, w)
		}
	}
	return out
}

// unknowns are ReadEffective's findings that say why a value of keys is
// Unknown (sources-disagree, effective-unknown), for a status Unknown for
// that reason. A finding has no keyword field: one whose detail names a
// keyword (names) is kept when it is one of keys, and one that names none
// is kept for every control, as is every finding when sshd -T's output
// was not read at all.
func (r reading) unknowns(keys ...string) []engine.Finding {
	var out []engine.Finding
	for _, f := range r.found {
		if f.Kind != sshd.FindingDisagree && (f.Kind != sshd.FindingUnknown || strings.HasPrefix(f.Detail, noReport)) {
			continue
		}
		named := names(f.Detail)
		if r.eff.Keywords == nil || len(named) == 0 || slices.ContainsFunc(named, func(k string) bool { return slices.Contains(keys, k) }) {
			out = append(out, steps.SSHDFinding(f))
		}
	}
	return out
}

// bound is what ss -Htln shows against the configured listeners.
type bound struct {
	all     []reload.Bound
	bad     []string        // LISTEN lines ss printed that were not read
	missing []sshd.Listener // configured, and no bound socket covers them
}

// readBound reads the bound sockets (reload.ReadBound) against l, the
// configured listeners, by the rule reload.CheckInEffect uses
// (reload.Unbound). ss missing is host.ErrNotFound.
func readBound(ctx context.Context, sys host.System, l sshd.Listeners) (bound, error) {
	all, bad, err := reload.ReadBound(ctx, sys)
	if err != nil {
		return bound{}, err
	}
	return bound{all: all, bad: bad, missing: reload.Unbound(l.All, all)}, nil
}

// notInEffect is the finding of b.missing.
func (b bound) notInEffect(mode detect.SSHMode) engine.Finding {
	why := "an edit not reloaded"
	if mode == detect.SSHSocket {
		why = "ssh.socket not restarted after daemon-reload"
	}
	d := listeners(b.missing) + " configured, not bound: not in effect (" + why + ")"
	if len(b.bad) > 0 {
		d += fmt.Sprintf("; ss lines not read: %q", b.bad)
	}
	return engine.Finding{Code: CodeNotInEffect, Detail: d, Source: "ss -Htln"}
}

// on lists the bound sockets on one of ports, for the evidence.
func (b bound) on(ports []uint16) string {
	var s []string
	for _, x := range b.all {
		if slices.Contains(ports, x.Port) {
			s = append(s, x.String())
		}
	}
	if len(s) == 0 {
		return "none on " + portList(ports)
	}
	return strings.Join(s, " ")
}

// stale is SSH Login's and SSH Auth's status when this run shows that what
// sshd -T read is not what the running sshd uses: where sshd holds its own
// sockets (ssh.service, OpenRC), a configured listener that ss does not
// show bound means that sshd has not reread its files since they were
// edited (or could not bind), so its other settings are not established
// either (check-ssh.md §2.8a). ok is false when that is not shown: on a
// socket-activated host, where a daemon-reload without a restart of
// ssh.socket leaves the sockets behind and says nothing of sshd's other
// settings; with the listeners not established; and without ss's reading
// (missing or failing), where the row is sshd -T's.
func (r reading) stale(ctx context.Context, sys host.System, f detect.Facts, ev []string) (st engine.Status, ok bool) {
	if f.SSH.Mode == detect.SSHSocket || !r.eff.Listeners.Known {
		return st, false
	}
	b, err := readBound(ctx, sys, r.eff.Listeners)
	if err != nil || len(b.missing) == 0 {
		return st, false
	}
	ev = slices.Concat(ev, []string{"ss -Htln: " + b.on(r.eff.Listeners.Ports())})
	return unknown("config not in effect", ev, []engine.Finding{b.notInEffect(f.SSH.Mode)}), true
}

// proxmox is pve.CheckSSH over this reading, with root's groups when
// withRoot (RootByKey needs them; RootKeyOnly and Port22 do not).
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
	case errors.As(err, &env) && env.Kind == sshd.EnvPrivsepDir:
		d = noRunDir(s, env)
	case errors.Is(err, sshd.ErrConfig):
		d = "sshd config invalid"
	}
	return engine.Status{State: engine.Unknown, Detail: d}, err
}

// noRunDir is the detail of sshd -T failing on its privilege separation
// directory. Only where sshd said the directory is missing and detect's
// unit states establish ssh.service stopped does it say what that shows:
// /run/sshd is gone while ssh.service is stopped, and on a socket-activated
// host ssh.socket starts it at the first connection (steps.md §2.15).
// Anything else (the directory not root's alone, a unit state not
// established, OpenRC) is "can't read".
func noRunDir(s detect.SSH, env *sshd.EnvError) string {
	missing := slices.ContainsFunc(env.Tail, func(l string) bool { return strings.HasPrefix(l, "Missing privilege separation directory") })
	switch {
	case !missing || s.Service.IsActive() != detect.No:
	case s.Mode == detect.SSHService:
		return "sshd not running"
	case s.Mode == detect.SSHSocket && s.Socket.IsActive() == detect.Yes:
		return "sshd not started yet"
	case s.Mode == detect.SSHSocket:
		return "sshd not running"
	}
	return "can't read"
}

// unknown is an Unknown status with detail, evidence and findings.
func unknown(detail string, ev []string, found []engine.Finding) engine.Status {
	return engine.Status{State: engine.Unknown, Detail: detail, Evidence: ev, Findings: found}
}
