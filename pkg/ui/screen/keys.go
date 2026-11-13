package screen

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/abbott/hardn/pkg/ui/component"
)

// The SSH Keys screen (the roadmap's "SSH key review", next/key-review;
// docs/engine/gallery-keys.md, adopted 2026-11-13, and screen-keys.md): under
// SSH Login, read only, on the sub-menu layout as the Configuration screen is.
// Every key sshd reads, by account: its type, size and comment, a weak key, a
// key sshd refuses and a key several accounts hold each marked. There is no
// Figma frame. pkg/screens/sshkeys fills SSHKeys: pkg/ui cannot read a host,
// and what is weak or refused is its caller's to say.

// The SSH Keys screen's words (gallery-keys.md, "Words"; screen-keys.md for
// "Not a key" and the keys' "Can't read").
const (
	sshKeysTitle       = "SSH Keys"
	sshKeysSummary     = "Summary"
	sshKeysRest        = "Nothing to review"
	sshKeysCountKey    = "Keys"
	sshKeysReviewKey   = "To review"
	sshKeysNoneMarked  = "None"
	sshKeysWeakKey     = "Weak"
	sshKeysSharedKey   = "Shared"
	sshKeysRefusedKey  = "Refused by sshd"
	sshKeysUnreadKey   = "Not read"
	sshKeysCantRead    = "Can't read"
	sshKeysNotKey      = "Not a key"
	sshKeysNoComment   = "No comment"
	sshKeysSharedWith  = "shared with %s"
	sshKeysSharedMany  = "shared with %d accounts"
	sshKeysEntryLabel  = "SSH keys"
	sshKeysEntryAbout  = "%s, %d to review"
	sshKeysEntryUnread = "%s, %s not read"
	sshKeysAllLabel    = "All keys"
	sshKeysMarkedLabel = "Keys to review"
	sshKeysMarkedAbout = "Fold the rest"
)

// SSHKeysItem is the screen's one item: All keys, and Keys to review back.
const SSHKeysItem = "1"

// SSHKeysFold is the most keys the screen lists whole: with more, it opens
// folded, on the keys to review alone.
const SSHKeysFold = 10

// sshKeysNamed is the most other accounts a shared key's note names: with more
// it counts them, the key's own among them, so the note stays beside the comment.
const sshKeysNamed = 1

// SSHKey is one key in a file sshd reads for an account.
type SSHKey struct {
	// Type is the key's type in words, such as "RSA" or "ED25519"; Bits its
	// size, 0 where the type has one size and none is shown.
	Type string
	Bits int
	// Comment is the key's comment, made safe to show by the caller; "" says
	// "No comment".
	Comment string
	// Weak: the key is weak by the caller's rule.
	Weak bool
	// Refused: this host's sshd does not take the key.
	Refused bool
	// Shared are the other accounts that hold the same key, in order.
	Shared []string
	// ID is the same for the same key and for no other, such as its
	// fingerprint: the summary counts a shared key once by it. It is not shown.
	ID string
}

// marked reports whether the key is one to review.
func (k SSHKey) marked() bool { return k.Weak || k.Refused || len(k.Shared) > 0 }

// row is the key's row: type and size, the comment, and what marks it. A weak or
// shared key is a warning; a refused one cannot log in, so it is none.
func (k SSHKey) row() component.KeyValue {
	row := component.KeyValue{Key: k.Type, Value: k.Comment, KeepNote: true}
	if k.Bits > 0 {
		row.Key += " " + strconv.Itoa(k.Bits)
	}
	if row.Value == "" {
		row.Value = sshKeysNoComment
	}
	var marks []string
	if k.Weak {
		marks = append(marks, sshKeysWeakKey)
	}
	if k.Refused {
		marks = append(marks, sshKeysRefusedKey)
	}
	switch n := len(k.Shared); {
	case n > sshKeysNamed:
		marks = append(marks, fmt.Sprintf(sshKeysSharedMany, n+1))
	case n > 0:
		marks = append(marks, fmt.Sprintf(sshKeysSharedWith, strings.Join(k.Shared, ", ")))
	}
	if len(marks) > 0 {
		row.Note = strings.ToUpper(marks[0][:1]) + marks[0][1:]
		for _, m := range marks[1:] {
			row.Note += ", " + strings.ToLower(m[:1]) + m[1:]
		}
	}
	if k.Weak || len(k.Shared) > 0 {
		row.State = component.ValueWarn
	}
	return row
}

// SSHKeyAccount is one account's keys.
type SSHKeyAccount struct {
	// Name is the account.
	Name string
	// Keys are its keys, in the files' order.
	Keys []SSHKey
	// Unread are the files sshd reads for it that could not be read.
	Unread []string
	// NotKeys are its files with lines that are no key, in order.
	NotKeys []SSHKeyLines
}

// SSHKeyLines is a file with lines that are neither a key, a blank line nor a
// comment: its path, made safe to show by the caller, and how many.
type SSHKeyLines struct {
	Path  string
	Lines int
}

// marked are the account's keys to review.
func (a SSHKeyAccount) marked() []SSHKey {
	var out []SSHKey
	for _, k := range a.Keys {
		if k.marked() {
			out = append(out, k)
		}
	}
	return out
}

// SSHKeys is the SSH Keys screen's reading of the host, which its caller fills.
type SSHKeys struct {
	// Accounts are the accounts with a key, or a file that could not be read,
	// in order.
	Accounts []SSHKeyAccount
	// All: every key is listed, though there are more than SSHKeysFold.
	All bool
	// NotRead: the keys are not known, as sshd's settings or the accounts
	// could not be read. The screen says so and lists none.
	NotRead bool
}

// count is how many keys there are, a key several accounts hold once for each
// of them.
func (v SSHKeys) count() int {
	n := 0
	for _, a := range v.Accounts {
		n += len(a.Keys)
	}
	return n
}

// folds reports whether there are more keys than the screen lists whole.
func (v SSHKeys) folds() bool { return v.count() > SSHKeysFold }

// plural is n and one, or n and many: "1 key", "2 keys".
func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}

// summary is the first section: how many keys in how many accounts, then a row
// for each kind of key to review that the host has, or "None".
func (v SSHKeys) summary() Section {
	if v.NotRead {
		return configSection(sshKeysSummary, []component.KeyValue{{Key: sshKeysCountKey, Value: sshKeysCantRead, State: component.ValueUnknown}})
	}
	var weak, shared, refused, unread, notKey, with int
	var held []string // the shared keys, each once
	for _, a := range v.Accounts {
		if len(a.Keys) > 0 {
			with++
		}
		unread += len(a.Unread)
		for _, f := range a.NotKeys {
			notKey += f.Lines
		}
		for _, k := range a.Keys {
			if k.Weak {
				weak++
			}
			if k.Refused {
				refused++
			}
			if len(k.Shared) > 0 && !slices.Contains(held, k.ID) {
				held = append(held, k.ID)
			}
		}
	}
	shared = len(held)
	rows := []component.KeyValue{{Key: sshKeysCountKey, Value: fmt.Sprintf("%d in %s", v.count(), plural(with, "account", "accounts"))}}
	if v.count() == 0 {
		rows[0].Value = sshKeysNoneMarked
	}
	for _, r := range []struct {
		key   string
		n     int
		state component.ValueState
		one   string
		many  string
	}{
		{sshKeysWeakKey, weak, component.ValueWarn, "key", "keys"},
		{sshKeysSharedKey, shared, component.ValueWarn, "key", "keys"},
		{sshKeysRefusedKey, refused, component.ValuePlain, "key", "keys"},
		{sshKeysUnreadKey, unread, component.ValueBad, "file", "files"},
		{sshKeysNotKey, notKey, component.ValueWarn, "line", "lines"},
	} {
		if r.n > 0 {
			rows = append(rows, component.KeyValue{Key: r.key, Value: plural(r.n, r.one, r.many), State: r.state})
		}
	}
	if len(rows) == 1 && v.count() > 0 {
		rows = append(rows, component.KeyValue{Key: sshKeysReviewKey, Value: sshKeysNoneMarked, State: component.ValueOK})
	}
	return configSection(sshKeysSummary, rows)
}

// sections are the summary, then a section for each account, under its name: its
// files that could not be read, those with lines that are no key, then its
// keys, those to review first. Not read, there is the summary alone. Folded, an
// account shows its keys to review and a count of the rest, and the accounts with
// none are one row each under "Nothing to review".
func (v SSHKeys) sections() []Section {
	out := []Section{v.summary()}
	if v.NotRead {
		return out
	}
	fold := v.folds() && !v.All
	var rest []component.KeyValue
	for _, a := range v.Accounts {
		marked := a.marked()
		if fold && len(marked) == 0 && len(a.Unread) == 0 && len(a.NotKeys) == 0 {
			rest = append(rest, component.KeyValue{Key: a.Name, Value: plural(len(a.Keys), "key", "keys")})
			continue
		}
		var rows []component.KeyValue
		for _, p := range a.Unread {
			rows = append(rows, component.KeyValue{Key: sshKeysCantRead, Value: p, State: component.ValueBad})
		}
		for _, f := range a.NotKeys {
			rows = append(rows, component.KeyValue{Key: sshKeysNotKey, Value: f.Path, Note: plural(f.Lines, "line", "lines"), State: component.ValueWarn, KeepNote: true})
		}
		for _, k := range marked {
			rows = append(rows, k.row())
		}
		if more := len(a.Keys) - len(marked); fold && more > 0 {
			rows = append(rows, component.KeyValue{Key: plural(more, "more key", "more keys"), State: component.ValueNone})
		} else {
			for _, k := range a.Keys {
				if !k.marked() {
					rows = append(rows, k.row())
				}
			}
		}
		out = append(out, configSection(a.Name, rows))
	}
	return append(out, configSection(sshKeysRest, rest))
}

// SubMenu returns the screen: the title bar under SSH Login; the sections; with
// more keys than SSHKeysFold, the item that lists them all, or folds them again;
// Return; "Option". The screen only reads.
func (v SSHKeys) SubMenu() SubMenu {
	sub := SubMenu{
		Title:    trail(sshTitle, sshKeysTitle),
		Sections: slices.DeleteFunc(v.sections(), func(s Section) bool { return len(s.Rows.Items) == 0 }),
		Menu:     component.Menu{Exit: returnItem()},
		Prompt:   menuPrompt,
	}
	if v.folds() && !v.NotRead {
		item := component.MenuItem{Key: SSHKeysItem, Label: sshKeysAllLabel, Description: plural(v.count(), "key", "keys")}
		if v.All {
			item.Label, item.Description = sshKeysMarkedLabel, sshKeysMarkedAbout
		}
		sub.Heading, sub.Menu.Items = fixtureHeading, []component.MenuItem{item}
	}
	return sub
}

// Entry is SSH Login's description of the item that opens the screen (SSH.Keys):
// the keys, and how many of them there are to review: "14 keys, 3 to review",
// "3 keys". Only a key is counted there. A file that could not be read is said
// after it, as its keys are not known: "2 keys, 1 file not read". Lines that are
// no key are the screen's to show. "Can't read" where the keys are not read.
func (v SSHKeys) Entry() string {
	if v.NotRead {
		return sshKeysCantRead
	}
	review, unread := 0, 0
	for _, a := range v.Accounts {
		review += len(a.marked())
		unread += len(a.Unread)
	}
	out := plural(v.count(), "key", "keys")
	if review > 0 {
		out = fmt.Sprintf(sshKeysEntryAbout, out, review)
	}
	if unread > 0 {
		out = fmt.Sprintf(sshKeysEntryUnread, out, plural(unread, "file", "files"))
	}
	return out
}
