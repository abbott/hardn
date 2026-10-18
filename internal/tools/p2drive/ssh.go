package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"slices"
	"strconv"
	"strings"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/pve"
	"github.com/abbott/hardn/pkg/host/reload"
	"github.com/abbott/hardn/pkg/host/sshd"
	"github.com/abbott/hardn/pkg/host/sshd/migrate"
	"github.com/abbott/hardn/pkg/host/ufw"
)

// errNoWriteStrategy: sshd_config's first directive is not exactly the stock
// Include, a case brief 2.2 gives no write strategy for (proposal P-38).
var errNoWriteStrategy = errors.New("no write strategy for this sshd_config")

func setupPort(fs *flag.FlagSet) func(context.Context, *driver) error {
	o := lockoutFlags(fs)
	port := fs.Uint("port", 2230, "the new SSH port")
	mode := ufwFlag(fs)
	return func(ctx context.Context, d *driver) error {
		m, err := parseUFW(*mode)
		if err != nil {
			return err
		}
		if *port == 0 || *port > 65535 {
			return fmt.Errorf("--port %d: 1 to 65535: %w", *port, errUsage)
		}
		return d.lockoutCommand(ctx, o, func(b base) (lockout, error) {
			return d.planSSH(ctx, b, fmt.Sprintf("SSH port %d added, the old ports held until confirmed", *port), nil, uint16(*port), m)
		})
	}
}

func setupKeyOnly(fs *flag.FlagSet) func(context.Context, *driver) error {
	o := lockoutFlags(fs)
	mode := ufwFlag(fs)
	return func(ctx context.Context, d *driver) error {
		m, err := parseUFW(*mode)
		if err != nil {
			return err
		}
		set := []migrate.Directive{{Keyword: "PasswordAuthentication", Args: []string{"no"}}, {Keyword: "KbdInteractiveAuthentication", Args: []string{"no"}}}
		return d.lockoutCommand(ctx, o, func(b base) (lockout, error) {
			return d.planSSH(ctx, b, "SSH logins by key only (password and keyboard-interactive off)", set, 0, m)
		})
	}
}

func setupPVERoot(fs *flag.FlagSet) func(context.Context, *driver) error {
	o := lockoutFlags(fs)
	mode := ufwFlag(fs)
	return func(ctx context.Context, d *driver) error {
		m, err := parseUFW(*mode)
		if err != nil {
			return err
		}
		set := []migrate.Directive{{Keyword: "PermitRootLogin", Args: []string{pve.RootLogin}}}
		return d.lockoutCommand(ctx, o, func(b base) (lockout, error) {
			if ok, err := pve.Applies(b.facts.Proxmox); err != nil {
				return lockout{}, err
			} else if !ok {
				return lockout{}, fmt.Errorf("pve-root is for a Proxmox VE host: %w", pve.ErrNotApplicable)
			}
			return d.planSSH(ctx, b, "root's SSH login key-only (PermitRootLogin prohibit-password)", set, 0, m)
		})
	}
}

// planSSH plans a change of 00-hardn.conf: set in force and, with port, a
// Port line for it beside the ports sshd listens on now (reload.HoldPorts),
// validated in context (sshd.Validate over a copy) before anything changes,
// and its reload planned (reload.PlanFor), so that every refusal comes
// before the timer is armed.
func (d *driver) planSSH(ctx context.Context, b base, summary string, set []migrate.Directive, port uint16, mode ufwMode) (lockout, error) {
	lc := lockout{base: b, summary: summary}
	inc, err := sshd.DetectInclude(ctx, d.sys)
	switch {
	case err != nil:
		return lc, err
	case inc.Mode == sshd.IncludeOther:
		return lc, fmt.Errorf("%s line %d %q is not exactly %q: %w (brief 2.2 covers the stock Include and none; proposal P-38)", sshd.MainConfig, inc.Line, inc.Text, sshd.IncludeLine, errNoWriteStrategy)
	case inc.Mode != sshd.IncludeFirst:
		return lc, fmt.Errorf("%s has no Include (%v): hardn's block at its top is not written by p2drive: %w", sshd.MainConfig, inc.Mode, errNoWriteStrategy)
	}
	found, err := migrate.Find(ctx, d.sys, migrate.FindRequest{})
	if err != nil {
		return lc, err
	}
	if len(found.Artifacts) > 0 {
		return lc, fmt.Errorf("legacy hardn SSH files (%s): run `p2drive migrate` first (docs/safety/sshd-migrate.md)", artifactPaths(found))
	}
	pveHost, err := pve.Applies(b.facts.Proxmox)
	if err != nil {
		return lc, err
	}
	if port != 0 {
		if err := pve.CheckPort(b.facts.Proxmox, []uint16{port}); err != nil {
			return lc, err
		}
		if !b.before.Listeners.Known {
			return lc, fmt.Errorf("the listeners sshd configures now: %w", reload.ErrListenersUnknown)
		}
		if b.before.Port.Has(strconv.Itoa(int(port))) {
			return lc, fmt.Errorf("sshd already listens on port %d: nothing to add: %w", port, errUsage)
		}
		var ports []migrate.Directive
		for _, p := range reload.HoldPorts(b.before.Listeners.All, []uint16{port}) {
			ports = append(ports, migrate.Directive{Keyword: "Port", Args: []string{strconv.Itoa(int(p))}})
		}
		set = append(ports, set...)
	}
	var groups pve.Groups
	if pveHost {
		if groups, err = pve.RootGroups(ctx, d.sys); err != nil {
			return lc, err
		}
		var findings []pve.Finding
		if set, findings, err = pve.PlanSSH(b.facts.Proxmox, set, groups); err != nil {
			return lc, err
		}
		for _, f := range findings {
			d.say("Proxmox VE: %v %s", f.Kind, f.Detail)
		}
	}
	old, absent, err := readOptional(ctx, d.sys, sshd.HardnDropIn)
	if err != nil {
		return lc, err
	}
	data, err := setDirectives(old, set)
	if err != nil {
		return lc, err
	}
	d.say("%s, as it will be:\n%s", sshd.HardnDropIn, indent(data))
	rep, err := sshd.Validate(ctx, d.sys, sshd.Request{Changes: []sshd.Change{{Path: sshd.HardnDropIn, Data: data}},
		RunDir: b.runDir, Platform: b.facts.Platform, Binary: b.binary})
	if err != nil {
		return lc, fmt.Errorf("validation in context: %w", err)
	}
	for _, f := range rep.Findings {
		d.say("validation finding %v: %s:%d %s", f.Kind, f.File, f.Line, f.Detail)
	}
	cand, _ := sshd.Evaluate(sshd.EffectiveInputs{Effective: rep.Effective})
	drops, err := d.socketDropIns(ctx, b)
	if err != nil {
		return lc, err
	}
	kws := keywordsOf(set)
	rp, err := reload.PlanFor(reload.Input{SSH: b.facts.SSH, DropIns: drops,
		Change: reload.Change{Keywords: kws, Before: b.before.Listeners, After: cand.Listeners.SSHD}})
	if err != nil {
		return lc, err
	}
	lc.adds = rp.Adds
	if lc.ufw, err = d.planUFW(ctx, b, mode, portsOf(rp.Adds)); err != nil {
		return lc, err
	}
	sum := sha256.Sum256(old)
	lc.files = &files{
		paths: []string{sshd.HardnDropIn}, keywords: kws, restored: b.before.Listeners.SSHD, reload: rp,
		write: func(ctx context.Context, run *backup.Run) (reload.Plan, error) {
			if err := sameAsRead(run, sshd.HardnDropIn, absent, hex.EncodeToString(sum[:])); err != nil {
				return reload.Plan{}, err
			}
			return rp, host.WriteFileAtomicKeep(ctx, d.sys, sshd.HardnDropIn, data, 0o644, 0, 0)
		},
		want: func(ctx context.Context, after sshd.Effective) error {
			if err := wanted(after, set); err != nil || !pveHost {
				return err
			}
			return d.checkPVE(ctx, b, after, groups)
		},
	}
	return lc, nil
}

// checkPVE reads, after the reload, what Proxmox VE needs of sshd
// (pve.CheckSSH over sshd -T and the files): root key-only is required;
// root by key from the other nodes and port 22 are shown.
func (d *driver) checkPVE(ctx context.Context, b base, after sshd.Effective, groups pve.Groups) error {
	found, err := migrate.Find(ctx, d.sys, migrate.FindRequest{})
	if err != nil {
		return err
	}
	chk, err := pve.CheckSSH(b.facts.Proxmox, pve.SSHInput{Effective: after, Main: found.Main, DropIns: found.DropIns, Root: groups})
	if err != nil {
		return err
	}
	d.say("Proxmox VE: root key-only %v, root by key %v, port 22 %v", chk.RootKeyOnly, chk.RootByKey, chk.Port22)
	for _, f := range chk.Findings {
		d.say("  %v: %s:%d %s", f.Kind, f.File, f.Line, f.Detail)
	}
	if chk.RootKeyOnly != detect.Yes {
		return fmt.Errorf("Proxmox VE: root's login is not key-only (%v)", chk.RootKeyOnly)
	}
	return nil
}

func setupUFWEnable(fs *flag.FlagSet) func(context.Context, *driver) error {
	o := lockoutFlags(fs)
	return func(ctx context.Context, d *driver) error {
		return d.lockoutCommand(ctx, o, func(b base) (lockout, error) {
			lc := lockout{base: b, summary: "ufw enabled with default deny incoming, after allowing the SSH listeners"}
			if !b.before.Listeners.Known {
				return lc, fmt.Errorf("the listeners sshd configures now: %w", ufw.ErrListenersUnknown)
			}
			p, err := d.planUFW(ctx, b, ufwEnable, nil)
			if err != nil {
				return lc, err
			}
			lc.ufw = p
			return lc, nil
		})
	}
}

func setupBroken(fs *flag.FlagSet) func(context.Context, *driver) error {
	return func(ctx context.Context, d *driver) error {
		b, err := d.begin(ctx)
		defer d.end(ctx, b)
		if err != nil {
			return err
		}
		old, _, err := readOptional(ctx, d.sys, sshd.HardnDropIn)
		if err != nil {
			return err
		}
		// A value sshd refuses: "unsupported option" in sshd -t, exit 255.
		data, err := setDirectives(old, []migrate.Directive{{Keyword: "PermitRootLogin", Args: []string{"maybe"}}})
		if err != nil {
			return err
		}
		_, verr := sshd.Validate(ctx, d.sys, sshd.Request{Changes: []sshd.Change{{Path: sshd.HardnDropIn, Data: data}},
			RunDir: b.runDir, Platform: b.facts.Platform, Binary: b.binary})
		now, _, err := readOptional(ctx, d.sys, sshd.HardnDropIn)
		switch {
		case err != nil:
			return err
		case !bytes.Equal(now, old):
			return fmt.Errorf("%s changed during the validation", sshd.HardnDropIn)
		}
		var ce *sshd.ConfigError
		if !errors.As(verr, &ce) {
			return fmt.Errorf("the broken candidate was not refused as a configuration error: %v", verr)
		}
		for _, p := range ce.Problems {
			d.say("refused: %s line %d: %s", p.File, p.Line, p.Message)
		}
		d.say("the broken candidate was refused before it took effect: %s is as it was, nothing reloaded", sshd.HardnDropIn)
		return nil
	}
}

// wanted checks that sshd -T shows what set asked for: the Port set and the
// lockout-relevant settings (brief 2.2: confirm the effective values). An
// Unknown value is not a pass.
func wanted(after sshd.Effective, set []migrate.Directive) error {
	var bad []string
	for _, dir := range set {
		v := strings.ToLower(dir.Args[0])
		switch strings.ToLower(dir.Keyword) {
		case "port":
			if !after.Port.Known || !after.Port.Has(v) {
				bad = append(bad, fmt.Sprintf("port %s not in %v (known %v)", v, after.Port.Values, after.Port.Known))
			}
		case "permitrootlogin":
			bad = appendSetting(bad, "permitrootlogin", v, after.PermitRootLogin)
		case "passwordauthentication":
			bad = appendSetting(bad, "passwordauthentication", v, after.PasswordAuthentication)
		case "kbdinteractiveauthentication":
			bad = appendSetting(bad, "kbdinteractiveauthentication", v, after.KbdInteractiveAuthentication)
		case "pubkeyauthentication":
			bad = appendSetting(bad, "pubkeyauthentication", v, after.PubkeyAuthentication)
		}
	}
	if len(bad) > 0 {
		return fmt.Errorf("sshd -T does not show the change: %s", strings.Join(bad, "; "))
	}
	return nil
}

func appendSetting(bad []string, kw, want string, s sshd.Setting) []string {
	if want == "without-password" {
		want = "prohibit-password"
	}
	if !s.Known || s.Value != want {
		return append(bad, fmt.Sprintf("%s %q, want %q (known %v)", kw, s.Value, want, s.Known))
	}
	return bad
}

// readOptional reads a root-controlled file without following a link;
// absent is not an error.
func readOptional(ctx context.Context, sys host.System, name string) (data []byte, absent bool, err error) {
	data, _, err = sys.ReadFileNoFollow(ctx, name)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, true, nil
	}
	return data, false, err
}

// sameAsRead checks the backup just taken of name shows it as planned:
// absent, or the content whose SHA-256 is sum. A file changed since the
// plan is not overwritten.
func sameAsRead(run *backup.Run, name string, absent bool, sum string) error {
	for _, e := range run.Manifest().Entries {
		if e.Path != name {
			continue
		}
		if absent && e.Kind == backup.KindAbsent || !absent && e.Kind == backup.KindFile && e.File != nil && e.File.SHA256 == sum {
			return nil
		}
		return fmt.Errorf("%s changed since the plan (%s now)", name, e.Kind)
	}
	return fmt.Errorf("%s: no backup taken", name)
}

// socketDropIns reads ssh.socket's drop-ins on a socket-activated host,
// where a listener change needs them (reload.PlanFor).
func (d *driver) socketDropIns(ctx context.Context, b base) (reload.SocketDropIns, error) {
	if !sshd.SocketMayListen(b.facts.SSH) {
		return reload.SocketDropIns{}, nil
	}
	return reload.ReadSocketDropIns(ctx, d.sys)
}

func artifactPaths(f migrate.Found) string {
	var ps []string
	for _, a := range f.Artifacts {
		ps = append(ps, a.Path)
	}
	return strings.Join(slices.Compact(ps), ", ")
}

func indent(data []byte) string {
	return "    " + strings.ReplaceAll(strings.TrimRight(string(data), "\n"), "\n", "\n    ")
}
