package screen

import (
	"slices"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/ui/component"
)

// keysHost is n plain keys for alice after george's two: one weak and shared
// with root, one refused.
func keysHost(n int) SSHKeys {
	alice := SSHKeyAccount{Name: "alice"}
	for range n {
		alice.Keys = append(alice.Keys, SSHKey{Type: "ED25519", Comment: "alice@host"})
	}
	return SSHKeys{Accounts: []SSHKeyAccount{
		{Name: "root", Keys: []SSHKey{{Type: "RSA", Bits: 1024, Comment: "george@old", Weak: true, Shared: []string{"george"}, ID: "a"}}},
		{Name: "george", Keys: []SSHKey{
			{Type: "ED25519", ID: "b"},
			{Type: "RSA", Bits: 1024, Comment: "george@old", Weak: true, Shared: []string{"root"}, ID: "a"},
			{Type: "ECDSA", Bits: 256, Comment: "george@desk", Refused: true, ID: "c"},
		}},
		alice,
	}}
}

// TestSSHKeyRow: a key's row is its type and size, its comment, and what marks
// it; a weak or shared key warns, a refused one does not.
func TestSSHKeyRow(t *testing.T) {
	for _, tt := range []struct {
		key  SSHKey
		want component.KeyValue
	}{
		{SSHKey{Type: "ED25519", Comment: "george@laptop-1"}, component.KeyValue{Key: "ED25519", Value: "george@laptop-1"}},
		{SSHKey{Type: "RSA", Bits: 4096}, component.KeyValue{Key: "RSA 4096", Value: "No comment"}},
		{SSHKey{Type: "RSA", Bits: 1024, Comment: "x", Weak: true}, component.KeyValue{Key: "RSA 1024", Value: "x", Note: "Weak", State: component.ValueWarn}},
		{SSHKey{Type: "DSA", Comment: "x", Weak: true, Refused: true}, component.KeyValue{Key: "DSA", Value: "x", Note: "Weak, refused by sshd", State: component.ValueWarn}},
		{SSHKey{Type: "ECDSA", Bits: 256, Comment: "x", Refused: true}, component.KeyValue{Key: "ECDSA 256", Value: "x", Note: "Refused by sshd"}},
		{SSHKey{Type: "ED25519", Comment: "x", Shared: []string{"bob"}}, component.KeyValue{Key: "ED25519", Value: "x", Note: "Shared with bob", State: component.ValueWarn}},
		{SSHKey{Type: "ED25519", Comment: "x", Weak: true, Shared: []string{"bob", "carol"}}, component.KeyValue{Key: "ED25519", Value: "x", Note: "Weak, shared with 3 accounts", State: component.ValueWarn}},
		{SSHKey{Type: "ED25519", Comment: "x", Shared: []string{"bob", "carol", "dave"}}, component.KeyValue{Key: "ED25519", Value: "x", Note: "Shared with 4 accounts", State: component.ValueWarn}},
	} {
		tt.want.KeepNote = true
		if got := tt.key.row(); got != tt.want {
			t.Errorf("%+v:\n got %+v\nwant %+v", tt.key, got, tt.want)
		}
	}
}

// TestSSHKeysWhole: up to SSHKeysFold keys, every key is listed under its
// account, those to review first, with no item; the summary counts a shared key
// once.
func TestSSHKeysWhole(t *testing.T) {
	v := keysHost(SSHKeysFold - 4)
	sub := v.SubMenu()
	if len(sub.Menu.Items) != 0 || sub.Heading != "" || sub.Title.Title != "SSH Keys" || !slices.Equal(sub.Title.Parents, []string{"SSH"}) {
		t.Errorf("menu %+v, heading %q, title %+v", sub.Menu.Items, sub.Heading, sub.Title)
	}
	var titles []string
	for _, s := range sub.Sections {
		titles = append(titles, s.Header.Title)
	}
	if !slices.Equal(titles, []string{"Summary", "root", "george", "alice"}) {
		t.Fatalf("sections %v", titles)
	}
	want := []component.KeyValue{
		{Key: "Keys", Value: "10 in 3 accounts"},
		{Key: "Weak", Value: "2 keys", State: component.ValueWarn},
		{Key: "Shared", Value: "1 key", State: component.ValueWarn},
		{Key: "Refused by sshd", Value: "1 key"},
	}
	if got := sub.Sections[0].Rows.Items; !slices.Equal(got, want) {
		t.Errorf("summary %+v", got)
	}
	var keys []string
	for _, r := range sub.Sections[2].Rows.Items {
		keys = append(keys, r.Key)
	}
	if !slices.Equal(keys, []string{"RSA 1024", "ECDSA 256", "ED25519"}) {
		t.Errorf("george's rows %v", keys)
	}
	if n := len(sub.Sections[3].Rows.Items); n != SSHKeysFold-4 {
		t.Errorf("alice has %d rows", n)
	}
	if got := v.Entry(); got != "10 keys, 3 to review" {
		t.Errorf("entry %q", got)
	}
}

// TestSSHKeysFolded: with more keys than SSHKeysFold the screen lists the keys to
// review, a count of each account's others, and the accounts with none as a row
// each; item 1 lists every key, and then folds them again.
func TestSSHKeysFolded(t *testing.T) {
	v := keysHost(SSHKeysFold)
	sub := v.SubMenu()
	var titles []string
	for _, s := range sub.Sections {
		titles = append(titles, s.Header.Title)
	}
	if !slices.Equal(titles, []string{"Summary", "root", "george", "Nothing to review"}) {
		t.Fatalf("sections %v", titles)
	}
	if got := sub.Sections[2].Rows.Items; len(got) != 3 || got[2] != (component.KeyValue{Key: "1 more key", State: component.ValueNone}) {
		t.Errorf("george's rows %+v", got)
	}
	if got := sub.Sections[3].Rows.Items; !slices.Equal(got, []component.KeyValue{{Key: "alice", Value: "10 keys"}}) {
		t.Errorf("the rest %+v", got)
	}
	want := []component.MenuItem{{Key: "1", Label: "All keys", Description: "14 keys"}}
	if !slices.Equal(sub.Menu.Items, want) || sub.Heading != "Select an option" {
		t.Errorf("menu %+v, heading %q", sub.Menu.Items, sub.Heading)
	}
	v.All = true
	sub = v.SubMenu()
	if n := len(sub.Sections); n != 4 || len(sub.Sections[3].Rows.Items) != SSHKeysFold || sub.Menu.Items[0].Label != "Keys to review" {
		t.Errorf("all: %d sections, menu %+v", n, sub.Menu.Items)
	}
}

// TestSSHKeysEdges: a clean host says there is none to review; a file that could
// not be read is a row of its account, counted, and shown folded too; a host with
// no key says so.
func TestSSHKeysEdges(t *testing.T) {
	clean := SSHKeys{Accounts: []SSHKeyAccount{{Name: "george", Keys: []SSHKey{{Type: "ED25519"}}}}}
	want := []component.KeyValue{{Key: "Keys", Value: "1 in 1 account"}, {Key: "To review", Value: "None", State: component.ValueOK}}
	if got := clean.SubMenu().Sections[0].Rows.Items; !slices.Equal(got, want) || clean.Entry() != "1 key" {
		t.Errorf("clean %+v, entry %q", got, clean.Entry())
	}
	if got := (SSHKeys{}).SubMenu().Sections; len(got) != 1 || !slices.Equal(got[0].Rows.Items, []component.KeyValue{{Key: "Keys", Value: "None"}}) {
		t.Errorf("no key: %+v", got)
	}
	unread := keysHost(SSHKeysFold)
	unread.Accounts[2].Unread = []string{"/home/alice/.ssh/authorized_keys"}
	sub := unread.SubMenu()
	if got := sub.Sections[0].Rows.Items; got[len(got)-1] != (component.KeyValue{Key: "Not read", Value: "1 file", State: component.ValueBad}) {
		t.Errorf("summary %+v", got)
	}
	got := sub.Sections[3]
	if got.Header.Title != "alice" || len(got.Rows.Items) != 2 ||
		got.Rows.Items[0] != (component.KeyValue{Key: "Can't read", Value: "/home/alice/.ssh/authorized_keys", State: component.ValueBad}) ||
		got.Rows.Items[1].Key != "10 more keys" {
		t.Errorf("alice %+v", got)
	}
	if e := unread.Entry(); e != "14 keys, 3 to review, 1 file not read" {
		t.Errorf("entry %q", e)
	}
	// Only a key is one to review: a file not read is never counted as one.
	none := SSHKeys{Accounts: []SSHKeyAccount{{Name: "alice", Unread: []string{"/a", "/b"}}}}
	if e := none.Entry(); e != "0 keys, 2 files not read" {
		t.Errorf("entry, no key %q", e)
	}
}

// TestSSHKeysEntry: SSH Login shows the item only where Keys is set, after its
// others, and draws as before without it.
func TestSSHKeysEntry(t *testing.T) {
	plain := SSH{Ports: "22"}
	with := plain
	with.Keys = "3 keys"
	items := with.SubMenu().Menu.Items
	if last := items[len(items)-1]; last != (component.MenuItem{Key: "6", Label: "SSH keys", Description: "3 keys"}) || len(items) != 4 {
		t.Errorf("items %+v", items)
	}
	if out := plain.SubMenu().Render(plainAt(24, false)); strings.Contains(out, "SSH keys") || len(plain.SubMenu().Menu.Items) != 3 {
		t.Errorf("an item with no Keys:\n%s", out)
	}
}

// TestSSHKeysNotRead: with the keys not read the screen says so and lists none,
// whatever else the view holds, with no item; SSH Login's item says the same.
func TestSSHKeysNotRead(t *testing.T) {
	v := keysHost(SSHKeysFold)
	v.NotRead = true
	sub := v.SubMenu()
	want := []component.KeyValue{{Key: "Keys", Value: "Can't read", State: component.ValueUnknown}}
	if len(sub.Sections) != 1 || !slices.Equal(sub.Sections[0].Rows.Items, want) || len(sub.Menu.Items) != 0 || v.Entry() != "Can't read" {
		t.Errorf("sections %+v, menu %+v, entry %q", sub.Sections, sub.Menu.Items, v.Entry())
	}
}

// TestSSHKeysNotKeys: a file with lines that are no key is a warning row of its
// account, counted in the summary, not among the keys to review, and shown folded too.
func TestSSHKeysNotKeys(t *testing.T) {
	v := keysHost(SSHKeysFold)
	v.Accounts[2].NotKeys = []SSHKeyLines{{Path: "/home/alice/.ssh/authorized_keys", Lines: 2}, {Path: "/home/alice/.ssh/authorized_keys2", Lines: 1}}
	sub := v.SubMenu()
	if got := sub.Sections[0].Rows.Items; got[len(got)-1] != (component.KeyValue{Key: "Not a key", Value: "3 lines", State: component.ValueWarn}) {
		t.Errorf("summary %+v", got)
	}
	got := sub.Sections[3]
	if got.Header.Title != "alice" || len(got.Rows.Items) != 3 ||
		got.Rows.Items[0] != (component.KeyValue{Key: "Not a key", Value: "/home/alice/.ssh/authorized_keys", Note: "2 lines", State: component.ValueWarn, KeepNote: true}) ||
		got.Rows.Items[1].Note != "1 line" || got.Rows.Items[2].Key != "10 more keys" {
		t.Errorf("alice %+v", got)
	}
	if e := v.Entry(); e != "14 keys, 3 to review" {
		t.Errorf("entry %q", e)
	}
}
