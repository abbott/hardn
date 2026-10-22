package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"reflect"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"
)

// The edit of hardn.yml (the maintainer's decision of 2026-10-22, amending
// refactor plan §3.19 for every slice: "edit only the keys that changed,
// keeping the rest of the file and its comments"; next/s2-config-edit,
// docs/engine/config-edit.md). Edit compares the configuration the file
// gives, its bytes decoded as the loaders decode them, with the one to
// save, setting by setting, and changes in the file's bytes the values
// that differ alone: every other byte stays as it was. It then reads the
// result back and refuses it unless it gives exactly the configuration to
// save.

var (
	// ErrNotEditable: hardn.yml is written in a form Edit does not change
	// in place (EditError says which, and where).
	ErrNotEditable = errors.New("hardn.yml is written in a form that is not edited in place")
	// ErrEditUnsafe: the edited bytes would not read back as the
	// configuration to save, so nothing is written.
	ErrEditUnsafe = errors.New("the edited hardn.yml would not read back as the configuration to save")
	// ErrChanged: the file's bytes are not the ones the configuration was
	// read from or last saved (LoadedFrom): an edit would undo what changed
	// them.
	ErrChanged = errors.New("hardn.yml changed since it was read")
)

// EditError is Edit's refusal: the setting it is about (Key, "" for the
// file), what about the file stops the edit, in a few words naming
// neither (Why, proposed: config-edit.md §5), the settings the save
// changes (Keys, in the configuration's order), and ErrNotEditable or
// ErrEditUnsafe, wrapping the cause if any (Err).
type EditError struct {
	Key, Why string
	Keys     []string
	Err      error
}

func (e *EditError) Error() string {
	at := "hardn.yml"
	if e.Key != "" {
		at = e.Key + " in hardn.yml"
	}
	return fmt.Sprintf("config: %s %s (saving %s): %v", at, e.Why, strings.Join(e.Keys, ", "), e.Err)
}

// Unwrap returns ErrNotEditable or ErrEditUnsafe, with its cause.
func (e *EditError) Unwrap() error { return e.Err }

// derivedKey is logsconfig, which is not a setting: LoadConfig sets
// LogsConfig.LogFilePath from logFile whatever the file says, and nothing
// reads it from the file. Edit neither compares nor writes it, so a file's
// logsconfig stays as it is and none is added.
const derivedKey = "logsconfig"

var bom = []byte("\xef\xbb\xbf")

// Edit returns original, hardn.yml's bytes as read, with the values of the
// settings to changes edited in place, and every other byte as it was: key
// order, blank lines, indentation, quoting, the comments of the file's
// head, above and beside each key, inside a list, and after it. A setting
// differs when its value in to is not the one the file gives it, its
// default included, so a default is never written: a setting the file does
// not have is added, at the end of its mapping, only when to's value is
// not its default. A scalar is replaced in its own quoting; a list in
// brackets stays in brackets; a block list keeps the items to keeps, each
// with its lines and comments, and its new items take its dash, indent and
// quoting (a removed or replaced item's comments go with it); a list the
// file has as nothing or [] takes the file's list style; a mapping is
// edited at the key that changed. Nothing changed: a copy of original.
//
// Edit refuses, writing nothing, with an EditError: a file not UTF-8, with
// a line break other than LF and CRLF, of more than one document or with a
// document end (...), not a mapping, or merging keys (<<) into a mapping it
// edits (ErrNotEditable); a changed setting with an anchor or alias, with a
// comment inside its brackets, or in a form it does not take apart
// (ErrNotEditable); and bytes that would not decode to exactly to, every
// setting compared, with every other key as before, unknown keys and
// logsconfig included (ErrEditUnsafe). Bytes
// that do not decode are ErrInvalidFile; a nil to, ErrNotLoaded. A UTF-8
// byte order mark, a leading --- and a directive are kept.
func Edit(original []byte, to *Config) ([]byte, error) {
	if to == nil {
		return nil, fmt.Errorf("config: edit: no configuration: %w", ErrNotLoaded)
	}
	from := DefaultConfig()
	if err := yaml.Unmarshal(original, from); err != nil {
		return nil, fmt.Errorf("config: %w: %w", ErrInvalidFile, err)
	}
	was, err := tree(from)
	if err != nil {
		return nil, err
	}
	want, err := tree(to)
	if err != nil {
		return nil, err
	}
	var keys []string
	for i := 0; i+1 < len(want.Content); i += 2 {
		if !same(valueOf(was, want.Content[i].Value), want.Content[i+1]) {
			keys = append(keys, want.Content[i].Value)
		}
	}
	if len(keys) == 0 {
		return slices.Clone(original), nil
	}
	head, src := []byte(nil), original
	if bytes.HasPrefix(src, bom) {
		head, src = bom, src[len(bom):]
	}
	e, err := newEditor(src, keys)
	if err != nil {
		return nil, err
	}
	if err := e.document(was, want); err != nil {
		return nil, err
	}
	body, err := e.apply()
	if err != nil {
		return nil, err
	}
	out := append(slices.Clone(head), body...)
	if err := check(original, out, want, keys); err != nil {
		return nil, err
	}
	return out, nil
}

// tree is c as a YAML mapping, setting by setting, in the configuration's
// order (canonical: permitRootLogin as its string), without logsconfig
// (derivedKey), and with a setting yaml.v3 leaves out when empty
// (omitempty, confirmTimeout) given its empty value, at the end.
func tree(c *Config) (*yaml.Node, error) {
	n, err := c.canonical()
	if err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	out := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value != derivedKey {
			out.Content = append(out.Content, n.Content[i], n.Content[i+1])
		}
	}
	t := reflect.TypeFor[plain]()
	for i := range t.NumField() {
		f := t.Field(i)
		name, opts, _ := strings.Cut(f.Tag.Get("yaml"), ",")
		if !f.IsExported() || name == "" || !strings.Contains(opts, "omitempty") || valueOf(out, name) != nil {
			continue
		}
		var z yaml.Node
		if err := z.Encode(reflect.Zero(f.Type).Interface()); err != nil {
			return nil, fmt.Errorf("config: %w", err)
		}
		out.Content = append(out.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: name}, &z)
	}
	return out, nil
}

// valueOf is the value of key name in mapping m; nil when it has none.
func valueOf(m *yaml.Node, name string) *yaml.Node {
	for i := 0; m != nil && i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == name {
			return m.Content[i+1]
		}
	}
	return nil
}

// same reports whether a and b, canonical values, encode alike.
func same(a, b *yaml.Node) bool {
	if a == nil || b == nil {
		return a == b
	}
	x, err1 := yaml.Marshal(a)
	y, err2 := yaml.Marshal(b)
	return err1 == nil && err2 == nil && bytes.Equal(x, y)
}

// check refuses out, the edit of original, unless it is one document that
// decodes to exactly want, every setting compared, and every key of
// original but the settings changed (keys), unknown keys and logsconfig
// included, decodes as before, with no key added but those.
func check(original, out []byte, want *yaml.Node, keys []string) error {
	unsafe := func(key string, cause error) error {
		err := ErrEditUnsafe
		if cause != nil {
			err = fmt.Errorf("%w: %w", ErrEditUnsafe, cause)
		}
		return &EditError{Key: key, Why: "cannot be edited safely", Keys: keys, Err: err}
	}
	if n, err := documents(out); err != nil || n > 1 {
		return unsafe("", err)
	}
	got := DefaultConfig()
	if err := yaml.Unmarshal(out, got); err != nil {
		return unsafe("", err)
	}
	have, err := tree(got)
	if err != nil {
		return unsafe("", err)
	}
	for i := 0; i+1 < len(want.Content); i += 2 {
		if name := want.Content[i].Value; !same(valueOf(have, name), want.Content[i+1]) {
			return unsafe(name, nil)
		}
	}
	before, err := keysOf(original)
	if err != nil {
		return unsafe("", err)
	}
	after, err := keysOf(out)
	if err != nil {
		return unsafe("", err)
	}
	for k, v := range after {
		if w, ok := before[k]; !slices.Contains(keys, k) && (!ok || !reflect.DeepEqual(v, w)) {
			return unsafe(k, nil)
		}
	}
	for k := range before {
		if _, ok := after[k]; !ok {
			return unsafe(k, nil)
		}
	}
	return nil
}

// keysOf is data's top-level mapping as yaml.v3 decodes it into any, by
// key; empty for no mapping.
func keysOf(data []byte) (map[string]any, error) {
	var v any
	if err := yaml.Unmarshal(data, &v); err != nil {
		return nil, err
	}
	out := map[string]any{}
	switch m := v.(type) {
	case nil:
	case map[string]any:
		out = m
	case map[any]any:
		for k, x := range m {
			out[fmt.Sprint(k)] = x
		}
	default:
		return nil, errors.New("not a mapping")
	}
	return out, nil
}

// documents counts the YAML documents in data.
func documents(data []byte) (int, error) {
	d := yaml.NewDecoder(bytes.NewReader(data))
	for n := 0; ; n++ {
		var doc yaml.Node
		if err := d.Decode(&doc); errors.Is(err, io.EOF) {
			return n, nil
		} else if err != nil {
			return n + 1, err
		}
	}
}
