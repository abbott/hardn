// Package dnfauto reads dnf-automatic on Rocky Linux and AlmaLinux: its
// package, its timers, and /etc/dnf/automatic.conf's [commands]; and edits
// that file's upgrade_type to security and back (docs/engine/el-updates.md).
// It changes the host only through the bytes its callers write.
package dnfauto

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/packages"
)

// The package, its file and its timers (rocky10-test-110's rpm,
// dnf-automatic 4.20.0).
const (
	Package = "dnf-automatic"
	Conf    = "/etc/dnf/automatic.conf"
	// InstallTimer runs dnf-automatic --installupdates: it installs
	// updates whatever apply_updates says.
	InstallTimer = "dnf-automatic-install.timer"
	// PlainTimer runs dnf-automatic as automatic.conf says: it installs
	// updates where apply_updates is true.
	PlainTimer = "dnf-automatic.timer"
	// Security is upgrade_type's value for security updates alone; the
	// other is "default", every update.
	Security = "security"
	// Line is the line hardn writes in [commands].
	Line = "upgrade_type = security"
)

// reboot's values in [commands]: never restart (the stock file's, and
// unset), restart after any update, or where an update needs it.
const (
	RebootNever       = "never"
	RebootWhenChanged = "when-changed"
	RebootWhenNeeded  = "when-needed"
)

// keys are the [commands] keys hardn reads. dnf matches a key's name
// case-sensitively and ignores one it doesn't know (its config loader's
// hasattr; VM-read 2026-11-10), so another case of one is ErrConf.
var keys = []string{"upgrade_type", "apply_updates", "reboot"}

// ErrConf: automatic.conf is not in a form hardn reads or edits: no
// [commands], a key of keys twice there or in another case, or a value
// dnf doesn't take.
var ErrConf = errors.New("automatic.conf is not in a form hardn reads")

// Timer is one timer as systemctl prints it ("" where not read, with Err).
type Timer struct {
	Name, Active, Enabled string
	Err                   error
}

// On: active and enabled.
func (t Timer) On() bool { return t.Active == "active" && t.Enabled == "enabled" }

// Settings are [commands]' keys as dnf-automatic reads them: UpgradeType
// upgrade_type ("default" where unset), Apply apply_updates (false where
// unset), Reboot reboot (RebootNever where unset).
type Settings struct {
	UpgradeType string
	Apply       bool
	Reboot      string
}

// Updates is dnf-automatic as Read reads it.
type Updates struct {
	Package    detect.Tri
	PackageErr error
	// Install and Plain are the two timers that may install updates; read
	// wherever rpm answered: not-found where the package isn't installed.
	Install, Plain Timer
	// Settings are automatic.conf's, read where the package is installed;
	// ConfErr why not.
	Settings
	ConfErr error
}

// Err is the first read that failed.
func (u Updates) Err() error {
	return errors.Join(u.PackageErr, u.Install.Err, u.Plain.Err, u.ConfErr)
}

// Applies is the timer that installs updates, and whether one does in
// some form: the install timer, or the plain one where apply_updates is
// true. The install timer comes first.
func (u Updates) Applies() (Timer, bool) {
	switch {
	case u.Install.Active == "active" || u.Install.Enabled == "enabled":
		return u.Install, true
	case u.Apply && (u.Plain.Active == "active" || u.Plain.Enabled == "enabled"):
		return u.Plain, true
	}
	return Timer{}, false
}

// Read reads dnf-automatic: rpm -q; where rpm answered, the two timers (a
// mask outlives the package); where installed, automatic.conf. It changes
// nothing.
func Read(ctx context.Context, sys host.System) Updates {
	var u Updates
	u.Package, u.PackageErr = packages.Installed(ctx, sys, packages.Dnf, Package)
	if u.PackageErr != nil {
		return u
	}
	u.Install, u.Plain = readTimer(ctx, sys, InstallTimer), readTimer(ctx, sys, PlainTimer)
	if u.Package != detect.Yes {
		return u
	}
	data, err := sys.ReadFile(ctx, Conf)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		u.Settings = Settings{UpgradeType: "default", Reboot: RebootNever} // dnf-automatic's defaults, with no file
	case err != nil:
		u.ConfErr = fmt.Errorf("dnfauto: %w", err)
	default:
		u.Settings, u.ConfErr = Parse(data)
	}
	return u
}

func readTimer(ctx context.Context, sys host.System, name string) Timer {
	t := Timer{Name: name}
	var e1, e2 error
	t.Active, e1 = detect.SystemctlIsActive(ctx, sys, name)
	t.Enabled, e2 = detect.SystemctlIsEnabled(ctx, sys, name)
	if err := errors.Join(e1, e2); err != nil {
		t.Err = fmt.Errorf("dnfauto: %w", err)
	}
	return t
}

// line is one line of the file, split: its section header, or its key and
// value, or neither (a comment, a blank).
type line struct {
	section, key, value string
}

func parseLine(l string) line {
	t := strings.TrimSpace(l)
	switch {
	case t == "" || t[0] == '#' || t[0] == ';':
		return line{}
	case t[0] == '[' && strings.HasSuffix(t, "]"):
		return line{section: strings.TrimSpace(t[1 : len(t)-1])}
	}
	k, v, ok := strings.Cut(t, "=")
	if !ok {
		return line{}
	}
	return line{key: strings.TrimSpace(k), value: strings.TrimSpace(v)}
}

// commands are the indexes of [commands]' lines (its header excluded), and
// whether the file has that section; the first [commands] alone counts.
func commands(lines []string) (idx []int, header int, found bool) {
	in := false
	for i, l := range lines {
		p := parseLine(l)
		if p.section != "" {
			if in {
				break
			}
			if in = p.section == "commands"; in {
				header, found = i, true
			}
			continue
		}
		if in {
			idx = append(idx, i)
		}
	}
	return idx, header, found
}

// find is the line of each of keys in [commands] (idx): a key twice, or
// one in another case, is ErrConf.
func find(lines []string, idx []int) (map[string]int, error) {
	at := map[string]int{}
	for _, i := range idx {
		k := parseLine(lines[i]).key
		for _, want := range keys {
			switch {
			case !strings.EqualFold(k, want):
				continue
			case k != want:
				return nil, fmt.Errorf("dnfauto: %s in [commands], which dnf doesn't read as %s: %w", k, want, ErrConf)
			}
			if _, ok := at[k]; ok {
				return nil, fmt.Errorf("dnfauto: %s twice in [commands]: %w", k, ErrConf)
			}
			at[k] = i
		}
	}
	return at, nil
}

// Parse reads [commands] as dnf-automatic would: upgrade_type default or
// security, and reboot never, when-changed or when-needed, each exactly
// (libdnf checks an enum's value as written); apply_updates a boolean in
// any case (yes, no, true, false, 1, 0, on, off: libdnf's OptionBool
// lowercases it). Anything else, a key twice, or a key in another case is
// ErrConf.
func Parse(data []byte) (Settings, error) {
	lines := strings.Split(string(data), "\n")
	idx, _, _ := commands(lines)
	s := Settings{UpgradeType: "default", Reboot: RebootNever}
	at, err := find(lines, idx)
	if err != nil {
		return Settings{}, err
	}
	if i, ok := at["upgrade_type"]; ok {
		v := parseLine(lines[i]).value
		if v != "default" && v != Security {
			return Settings{}, fmt.Errorf("dnfauto: upgrade_type %q: %w", v, ErrConf)
		}
		s.UpgradeType = v
	}
	if i, ok := at["apply_updates"]; ok {
		v := parseLine(lines[i]).value
		switch strings.ToLower(v) {
		case "yes", "true", "1", "on":
			s.Apply = true
		case "no", "false", "0", "off":
		default:
			return Settings{}, fmt.Errorf("dnfauto: apply_updates %q: %w", v, ErrConf)
		}
	}
	if i, ok := at["reboot"]; ok {
		v := parseLine(lines[i]).value
		if v != RebootNever && v != RebootWhenChanged && v != RebootWhenNeeded {
			return Settings{}, fmt.Errorf("dnfauto: reboot %q: %w", v, ErrConf)
		}
		s.Reboot = v
	}
	return s, nil
}

// SetSecurity is data with [commands] upgrade_type set to security:
// Line in place of the line that set it (replaced), or added right under
// [commands]'s header (replaced ""). No [commands], or a key of keys
// twice there or in another case, is ErrConf.
func SetSecurity(data []byte) (out []byte, replaced string, err error) {
	lines := strings.Split(string(data), "\n")
	idx, header, found := commands(lines)
	if !found {
		return nil, "", fmt.Errorf("dnfauto: no [commands] section: %w", ErrConf)
	}
	keyAt, err := find(lines, idx)
	if err != nil {
		return nil, "", err
	}
	if at, ok := keyAt["upgrade_type"]; ok {
		replaced, lines[at] = lines[at], Line
	} else {
		lines = append(lines[:header+1], append([]string{Line}, lines[header+1:]...)...)
	}
	return []byte(strings.Join(lines, "\n")), replaced, nil
}

// Revert is SetSecurity undone: the one Line in [commands] put back as
// replaced, or removed where replaced is "". No such line is ErrConf.
func Revert(data []byte, replaced string) ([]byte, error) {
	lines := strings.Split(string(data), "\n")
	idx, _, found := commands(lines)
	at := -1
	for _, i := range idx {
		if lines[i] == Line {
			if at >= 0 {
				return nil, fmt.Errorf("dnfauto: %q twice in [commands]: %w", Line, ErrConf)
			}
			at = i
		}
	}
	if !found || at < 0 {
		return nil, fmt.Errorf("dnfauto: no %q in [commands]: %w", Line, ErrConf)
	}
	if replaced != "" {
		lines[at] = replaced
	} else {
		lines = append(lines[:at], lines[at+1:]...)
	}
	return []byte(strings.Join(lines, "\n")), nil
}
