package config

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/abbott/hardn/pkg/host"
)

// RootLogin is the value of the permitRootLogin key: what hardn sets sshd's
// PermitRootLogin to (refactor plan §3.17, approved 2026-10-17). A bool
// cannot say prohibit-password, which Proxmox VE needs (brief 2.5).
type RootLogin string

const (
	RootLoginNo               RootLogin = "no"
	RootLoginProhibitPassword RootLogin = "prohibit-password" // keys only
	RootLoginYes              RootLogin = "yes"
)

// rootLoginKey is the key in hardn.yml.
const rootLoginKey = "permitRootLogin"

// Valid reports whether r is one of the three values.
func (r RootLogin) Valid() bool {
	return r == RootLoginNo || r == RootLoginProhibitPassword || r == RootLoginYes
}

// Deprecation is a value read in a form that is going away. It still works;
// the caller shows it as a warning (brief §4: add the new form, and
// deprecate the old one with a warning).
type Deprecation struct {
	Key   string // permitRootLogin
	Line  int    // the value's line in the file, 1-based
	Found string // the value as written, such as false
	Use   string // what to write instead, which means the same, such as "no"
}

// String is the warning to show for d.
func (d Deprecation) String() string {
	return fmt.Sprintf("%s: %s (line %d) is deprecated; write %q, which means the same", d.Key, d.Found, d.Line, d.Use)
}

// loaded is what the YAML said beyond Config's fields, and where it was.
type loaded struct {
	rootLogin    RootLogin // permitRootLogin's string value; "" for the bool or no value
	deprecations []Deprecation
	path         string // the absolute path LoadConfig read or created; "" for none
	sum          string // the SHA-256 of the bytes read there or last saved (LoadedFrom); "" for none
	dryRun       bool   // dryRun as the file gave it, its default false included: what SaveConfig writes (dryrun.go)
	// What set the session's mode, and the value it set (ResolveDryRun).
	source   DryRunSource
	resolved bool
}

// prohibitPasswordApplied: DISABLED 2026-10-17. Flip to true once the SSH
// code hardn runs applies Config.RootLogin() (p3-engine, with p2-pve). The
// legacy SSH writers this binary still runs (cmd/hardn's ConfigureSSH,
// through FileSSHRepository) read the PermitRootLogin bool and write
// "PermitRootLogin no" for prohibit-password: on a Proxmox VE host where
// root's key is the only login, that ends it (R15). Until then LoadConfig
// refuses the value, as it did when the key was a bool; the YAML decoding
// (UnmarshalYAML) reads it.
const prohibitPasswordApplied = false

// ErrRootLoginNotApplied: the configuration says prohibit-password, which
// the SSH code of this build would apply as "no" (prohibitPasswordApplied).
var ErrRootLoginNotApplied = errors.New(`permitRootLogin "prohibit-password" is not applied by this build's SSH code, which would write PermitRootLogin no; write "no" or "yes"`)

// ErrNotLoaded: the Config was not loaded from a file whose absolute path
// is known, so Save has nowhere to write (§3.19: the loaded path).
var ErrNotLoaded = errors.New("the configuration was not loaded from a file")

// checkLoaded refuses a value the legacy code would apply as another.
func checkLoaded(c *Config) error {
	if !prohibitPasswordApplied && c.RootLogin() == RootLoginProhibitPassword {
		return ErrRootLoginNotApplied
	}
	return nil
}

// setPath records name, made absolute, as where c was loaded from; a name
// that cannot be made absolute records none, so Save refuses.
func (c *Config) setPath(name string) {
	c.loaded.path = ""
	if p, err := filepath.Abs(name); err == nil {
		c.loaded.path = p
	}
}

// Path is the absolute path the configuration was loaded from (LoadConfig:
// the file it read, or the default file it created), "" for none. Save
// writes there.
func (c *Config) Path() string { return c.loaded.path }

// plain has Config's fields and none of its methods: the default decoding
// and encoding.
type plain Config

// UnmarshalYAML reads hardn.yml as yaml.v3 decodes Config's fields, except
// permitRootLogin. That key takes "no", "prohibit-password" or "yes", and
// still the old bool (true or false, or one of YAML 1.1's spellings of it
// that a bool field accepted, such as on, off, Yes), which reads as "yes"
// or "no" and records a Deprecation. Any other value is the error the bool
// field gave. PermitRootLogin, the bool legacy code reads, is true exactly
// for "yes", so legacy code would write PermitRootLogin no for
// prohibit-password: LoadConfig refuses that value until the SSH code
// applies it (prohibitPasswordApplied).
func (c *Config) UnmarshalYAML(n *yaml.Node) error {
	c.loaded = loaded{}
	var errs []string
	node, values := n, []*yaml.Node(nil)
	if n.Kind == yaml.MappingNode {
		node = &yaml.Node{}
		*node = *n
		node.Content = nil
		for i := 0; i+1 < len(n.Content); i += 2 {
			if k := n.Content[i]; k.Kind == yaml.ScalarNode && k.Value == rootLoginKey {
				if len(values) > 0 { // as the decoder reports a repeated field
					errs = append(errs, fmt.Sprintf("line %d: field %s already set in type config.Config", k.Line, rootLoginKey))
				}
				values = append(values, n.Content[i+1])
				continue
			}
			node.Content = append(node.Content, n.Content[i], n.Content[i+1])
		}
	}
	if err := node.Decode((*plain)(c)); err != nil {
		var te *yaml.TypeError
		if !errors.As(err, &te) {
			return err
		}
		for _, e := range te.Errors { // the decoder names the type it decodes into
			errs = append(errs, strings.ReplaceAll(e, "config.plain", "config.Config"))
		}
	}
	c.loaded.dryRun = c.DryRun
	if len(values) > 0 {
		var te *yaml.TypeError
		if err := c.readRootLogin(values[0]); errors.As(err, &te) {
			errs = append(errs, te.Errors...)
		} else if err != nil {
			return err
		}
	}
	if len(errs) > 0 {
		return &yaml.TypeError{Errors: errs}
	}
	return nil
}

// readRootLogin reads permitRootLogin's value node.
func (c *Config) readRootLogin(v *yaml.Node) error {
	for v.Kind == yaml.AliasNode && v.Alias != nil {
		v = v.Alias
	}
	switch {
	case v.Kind == yaml.ScalarNode && v.ShortTag() == "!!null":
		return nil // a bool field kept its value for a null
	case v.Kind == yaml.ScalarNode && v.ShortTag() == "!!str" && RootLogin(v.Value).Valid():
		c.loaded.rootLogin = RootLogin(v.Value)
		c.PermitRootLogin = c.loaded.rootLogin == RootLoginYes
		return nil
	}
	var b bool
	if err := v.Decode(&b); err != nil {
		return err
	}
	c.PermitRootLogin = b
	use := RootLoginNo
	if b {
		use = RootLoginYes
	}
	c.loaded.deprecations = append(c.loaded.deprecations, Deprecation{Key: rootLoginKey, Line: v.Line, Found: v.Value, Use: string(use)})
	return nil
}

// RootLogin is permitRootLogin's value: the string it was given, or "yes"
// or "no" from PermitRootLogin when it was given the bool, nothing, or
// PermitRootLogin has changed since.
func (c *Config) RootLogin() RootLogin {
	if r := c.loaded.rootLogin; r != "" && (r == RootLoginYes) == c.PermitRootLogin {
		return r
	}
	if c.PermitRootLogin {
		return RootLoginYes
	}
	return RootLoginNo
}

// SetRootLogin sets permitRootLogin, and PermitRootLogin to match.
func (c *Config) SetRootLogin(r RootLogin) error {
	if !r.Valid() {
		return fmt.Errorf("config: %s %q: not no, prohibit-password or yes", rootLoginKey, r)
	}
	c.loaded.rootLogin, c.PermitRootLogin = r, r == RootLoginYes
	return nil
}

// Deprecations lists the deprecated forms the configuration was read in,
// for the caller to show as warnings.
func (c *Config) Deprecations() []Deprecation { return slices.Clone(c.loaded.deprecations) }

// MarshalYAML encodes c as yaml.v3 encodes its fields, so the legacy
// SaveConfig writes what it wrote before, permitRootLogin as a bool, with
// one exception: a prohibit-password value, which the bool cannot carry, is
// written as that string rather than lost as false. The canonical string
// is written by Save only, when it changes the value (§3.19; Edit).
func (c Config) MarshalYAML() (any, error) {
	if c.RootLogin() != RootLoginProhibitPassword {
		return (*plain)(&c), nil
	}
	return c.canonical()
}

// canonical is c as a YAML mapping with permitRootLogin as its string.
func (c *Config) canonical() (*yaml.Node, error) {
	var n yaml.Node
	if err := n.Encode((*plain)(c)); err != nil {
		return nil, err
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == rootLoginKey {
			v := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: string(c.RootLogin())}
			if c.RootLogin() != RootLoginProhibitPassword {
				v.Style = yaml.DoubleQuotedStyle // YAML 1.1 reads a bare yes or no as a bool
			}
			n.Content[i+1] = v
		}
	}
	return &n, nil
}

// Save writes c to the file it was loaded from, c.Path() (refactor plan
// §3.19: one save function, to the loaded path, atomically; amended by the
// maintainer on 2026-10-22: "edit only the keys that changed, keeping the
// rest of the file and its comments"), and refuses a Config that was not
// loaded from one (ErrNotLoaded): the path is the loader's, so no caller
// can save elsewhere. It reads the file, not through a symlink
// (host.ErrSymlink), and refuses bytes that are not the ones c was read
// from or last saved (ErrChanged: the edit would undo what changed them).
// What it writes is Edit's: the settings c changes, edited in place, the
// rest of the file as it was; with settings named, those alone (Edit).
// Edit refuses what would not read back as the configuration to save
// (ErrNotEditable, ErrEditUnsafe), and with nothing changed nothing is
// written. host.WriteFileAtomicKeep keeps the file's mode and owner. Once
// written, c records the bytes written as its own (LoadedFrom).
func Save(ctx context.Context, sys host.System, c *Config, settings ...string) error {
	path := c.loaded.path
	if path == "" {
		return fmt.Errorf("config: %w", ErrNotLoaded)
	}
	data, _, err := sys.ReadFileNoFollow(ctx, path)
	if err != nil {
		return fmt.Errorf("config: saving %s: %w", path, err)
	}
	if !c.LoadedFrom(data) {
		return fmt.Errorf("config: saving %s: %w", path, ErrChanged)
	}
	out, err := Edit(data, c, settings...)
	if err != nil {
		return fmt.Errorf("config: saving %s: %w", path, err)
	}
	if bytes.Equal(out, data) {
		return nil
	}
	if err := host.WriteFileAtomicKeep(ctx, sys, path, out, 0o644, -1, -1); err != nil {
		return fmt.Errorf("config: saving %s: %w", path, err)
	}
	c.loaded.sum = sum(out)
	return nil
}
