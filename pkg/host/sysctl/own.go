package sysctl

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/abbott/hardn/pkg/host"
)

// KV is a key and a value.
type KV struct {
	Key string
	N   int
}

// Entry is one line of hardn's file: a key, or one with "*" for every
// interface, the value hardn sets, and what each key it changed read
// before.
type Entry struct {
	Key    string
	N      int
	Before []KV
}

// Own is hardn's file: Header, then each Entry as its "# before:" lines and
// its "key = value" line.
type Own struct {
	Entries []Entry
	// Text is the file as read; "" for one not read from the host.
	Text string
}

// Bytes is the file.
func (o Own) Bytes() []byte {
	var b bytes.Buffer
	b.WriteString(Header)
	for _, e := range o.Entries {
		for _, kv := range e.Before {
			fmt.Fprintf(&b, "%s%s = %d\n", beforeMark, kv.Key, kv.N)
		}
		fmt.Fprintf(&b, "%s = %d\n", e.Key, e.N)
	}
	return b.Bytes()
}

// Settings is how many of the baseline's settings (Specs) the file's lines
// set: Source routing's two keys are one, and so is a per-interface
// setting written as a line for each interface (busybox).
func (o Own) Settings() int {
	seen := map[string]bool{}
	for _, e := range o.Entries {
		if s, ok := specOf(e.Key); ok {
			seen[s.Name] = true
		}
	}
	return len(seen)
}

// HasMark reports whether data opens with hardn's mark.
func HasMark(data []byte) bool { return bytes.HasPrefix(data, []byte(Header)) }

// ParseOwn reads hardn's file. A line hardn does not write, or a key hardn
// never writes (Writable), is ErrMalformed, naming the line.
func ParseOwn(data []byte) (Own, error) {
	bad := func(n int, what string) (Own, error) {
		return Own{}, fmt.Errorf("sysctl: %s:%d: %s: %w", File, n, what, ErrMalformed)
	}
	if !HasMark(data) {
		return bad(1, "no mark of hardn's")
	}
	pair := func(text string) (KV, bool) {
		k, v, ok := strings.Cut(text, " = ")
		n, err := strconv.Atoi(v)
		return KV{Key: k, N: n}, ok && err == nil && Writable(k)
	}
	var o Own
	var before []KV
	for i, line := range strings.Split(string(data), "\n")[1:] {
		rest, isBefore := strings.CutPrefix(line, beforeMark)
		switch kv, ok := pair(rest); {
		case line == "":
		case isBefore && ok && !strings.Contains(kv.Key, glob):
			before = append(before, kv)
		case isBefore || !ok:
			return bad(i+2, "a line hardn does not write")
		default:
			o.Entries = append(o.Entries, Entry{Key: kv.Key, N: kv.N, Before: before})
			before = nil
		}
	}
	if len(before) > 0 {
		return bad(strings.Count(string(data), "\n")+1, "a before line with no key under it")
	}
	return o, nil
}

// readOwn reads File: nil where it is not there; foreign where a file is
// there without hardn's mark.
func readOwn(ctx context.Context, sys host.System) (own *Own, foreign bool, err error) {
	data, ok, err := host.ReadFileIfThere(ctx, sys, File)
	switch {
	case err != nil || !ok:
		return nil, false, err
	case !HasMark(data):
		return nil, true, nil
	}
	o, err := ParseOwn(data)
	if err != nil {
		return nil, false, err
	}
	o.Text = string(data)
	return &o, false, nil
}

// add sets key to n, recording before for the keys no line has recorded
// yet: the first record of a key is the value it had before hardn.
func (o *Own) add(key string, n int, before []KV) {
	recorded := func(k string) bool {
		return slices.ContainsFunc(o.Entries, func(e Entry) bool {
			return slices.ContainsFunc(e.Before, func(kv KV) bool { return kv.Key == k })
		})
	}
	var fresh []KV
	for _, kv := range before {
		if !recorded(kv.Key) {
			fresh = append(fresh, kv)
		}
	}
	if i := slices.IndexFunc(o.Entries, func(e Entry) bool { return e.Key == key }); i >= 0 {
		o.Entries[i].N = n
		o.Entries[i].Before = append(o.Entries[i].Before, fresh...)
		return
	}
	o.Entries = append(o.Entries, Entry{Key: key, N: n, Before: fresh})
}

// Current is key's value as r read it; ok is false for a key r did not
// read.
func (r Reading) Current(key string) (int, bool) {
	for _, row := range r.Rows {
		for _, v := range row.Values {
			if v.Key == key && v.Err == nil {
				return v.N, true
			}
		}
	}
	return 0, false
}

// Next is hardn's file as Apply settings writes it: the lines it has,
// and a line for each key below the baseline that no later file holds,
// under the value it has now. A row every interface has is one line with
// "*" where the tool reads one (procps, and systemd at boot, which sets an
// interface as it appears), and a line for each key below the baseline
// where it does not (busybox). A line hardn wrote is left out where a
// later file now holds its row, and where it names one key the kernel no
// longer has (an interface that went away). No line lowers a value: one
// whose key now reads above its value, where a higher value passes, takes
// that value. No other value the key reads is taken: hardn writes strict
// (1) for no reverse-path filter, and 1 for no unprivileged BPF.
func (r Reading) Next() Own {
	var o Own
	if r.Own != nil {
		for _, e := range r.Own.Entries {
			s, _ := specOf(e.Key)
			now, has := r.Current(e.Key)
			if r.held(s) || (!has && !strings.Contains(e.Key, glob)) {
				continue
			}
			e.Before = slices.Clone(e.Before)
			if has && s.AtLeast && now > s.Want {
				e.N = now
			}
			o.Entries = append(o.Entries, e)
		}
	}
	for _, row := range r.Todo() {
		s := row.Spec
		if s.Kind == PerInterface && r.Tool != Busybox {
			var before []KV
			for _, v := range row.Values {
				if v.N != s.Want {
					before = append(before, KV{Key: v.Key, N: v.N})
				}
			}
			o.add(confKey(s.Family, glob, s.Leaf), s.Want, before)
			continue
		}
		for _, v := range row.Low {
			o.add(v.Key, s.Want, []KV{{Key: v.Key, N: v.N}})
		}
	}
	order := func(key string) int {
		s, _ := specOf(key)
		return slices.IndexFunc(Specs, func(t Spec) bool { return t.Name == s.Name })
	}
	slices.SortStableFunc(o.Entries, func(a, b Entry) int { return order(a.Key) - order(b.Key) })
	return o
}

// held reports whether a later file holds s's row.
func (r Reading) held(s Spec) bool {
	return slices.ContainsFunc(r.Rows, func(row Row) bool { return row.State == Held && row.Spec.Name == s.Name })
}

// Change is one key Apply settings sets now: from the value it reads, To
// the baseline's.
type Change struct {
	Key      string
	From, To int
}

// Changes are the keys Apply settings sets now: each key below the
// baseline of each row it sets (Todo), and no other. A key at the baseline
// or above it, a held row's, a row's left alone and a line the file
// already has are not set again.
func (r Reading) Changes() []Change {
	var out []Change
	for _, row := range r.Todo() {
		for _, v := range row.Low {
			out = append(out, Change{Key: v.Key, From: v.N, To: row.Spec.Want})
		}
	}
	return out
}

// Restore are the values Remove settings puts back: each key's first
// record, in the file's order.
func (o Own) Restore() []KV {
	var out []KV
	for _, e := range o.Entries {
		for _, kv := range e.Before {
			if !slices.ContainsFunc(out, func(x KV) bool { return x.Key == kv.Key }) {
				out = append(out, kv)
			}
		}
	}
	return out
}
