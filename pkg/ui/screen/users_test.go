package screen

import (
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"

	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/term"
)

// The User Management screens as pkg/screens/users fills them on its
// debian13 and pve9 hosts; the hosts behind each are that package's
// goldens. Synthetic values only.
var (
	usrTestStatus = component.StatusList{Rows: []component.StatusRow{{Label: "User", State: component.StatusConfigured, Detail: "non-root, sudo"}}}
	usrTestGeorge = []component.KeyValue{{Key: "Sudo", Value: "Enabled,", State: component.ValueWarn, Note: "No password to ask for"},
		{Key: UsersByKey, Value: "/etc/sudoers.d/90-cloud-init-users"}, UsersKeysRow(2), {Key: UsersPasswordKey, Value: "None"}}
	usrTestAlice = []component.KeyValue{{Key: "Sudo", Value: "Enabled", State: component.ValueOK}, {Key: UsersByKey, Value: "group sudo"},
		UsersKeysRow(0), {Key: UsersPasswordKey, Value: "Set"}}
	usrTestTwo    = Users{Status: usrTestStatus, Tool: "sudo", Users: []UsersSection{{"george", usrTestGeorge}, {"alice", usrTestAlice}}}
	usrTestNone   = Users{Status: component.StatusList{Rows: []component.StatusRow{{Label: "User", State: component.StatusNotConfigured, Detail: "root only"}}}}
	usrTestManage = UsersManage{Name: "george", Rows: usrTestGeorge, Admin: true, NoPassword: true}
	usrTestKeys   = UsersKeys{Name: "george", Keys: []component.KeyValue{{Key: "SHA256:AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE", Value: "george@laptop-1"},
		{Key: "SHA256:AgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgI", Value: "george@laptop-2"}}}
)

// TestUsersHeights (from the gallery's, whose counts these are,
// gallery-users.md §4): each screen at 80x24 and 80x40, in Unicode and
// ASCII, is as many lines at either height, the prompt on its last: full
// sections at every height. pkg/screens/users' holds every host's.
func TestUsersHeights(t *testing.T) {
	draws := map[string]struct {
		vm    SubMenu
		lines int
	}{
		"two users": {usrTestTwo.SubMenu(), 26}, "root only": {usrTestNone.SubMenu(), 15}, "select": {UsersSelect{Names: []string{"george", "alice"}}.SubMenu(), 10},
		"manage": {usrTestManage.SubMenu(), 20}, "keys": {usrTestKeys.SubMenu(), 19},
	}
	for name, d := range draws {
		for _, rows := range []int{24, 40} {
			for _, unicode := range []bool{true, false} {
				env := term.Fixed(80, colorprofile.TrueColor, unicode, true)
				env.Height = rows
				lines := strings.Split(ansi.Strip(d.vm.Render(component.NewCtx(env))), "\n")
				if len(lines) != d.lines || !strings.HasSuffix(lines[len(lines)-1], " or q]:") {
					t.Errorf("%s at 80x%d, unicode %v: %d lines, the last %q; want %d, the prompt", name, rows, unicode, len(lines), lines[len(lines)-1], d.lines)
				}
			}
		}
	}
}

// TestUsersItems: Manage User only with a user, Create keeping 2; Manage
// User's items by the user's rights, SSH Keys keeping 3; Remove SSH key
// only with a key; a user not read, Return alone.
func TestUsersItems(t *testing.T) {
	items := func(vm SubMenu) string {
		var out []string
		for _, it := range vm.Menu.Items {
			out = append(out, it.Key+" "+it.Label+" ("+it.Description+")")
		}
		return strings.Join(out, ", ")
	}
	for _, tt := range []struct {
		name string
		vm   SubMenu
		want string
	}{
		{"two users", usrTestTwo.SubMenu(), "1 Manage User (Configure sudo, SSH keys), 2 Create User (Configure a new user)"},
		{"doas", Users{Tool: "doas", Users: usrTestTwo.Users}.SubMenu(), "1 Manage User (Configure doas, SSH keys), 2 Create User (Configure a new user)"},
		{"root only", usrTestNone.SubMenu(), "2 Create User (Configure a new user)"},
		{"admin, no password", usrTestManage.SubMenu(), "1 Remove admin rights (Take them away), 2 Ask for password (For admin rights), 3 SSH Keys (Add or remove SSH keys)"},
		{"admin, a password", UsersManage{Name: "alice", Admin: true}.SubMenu(), "1 Remove admin rights (Take them away), 2 Don't ask password (For admin rights), 3 SSH Keys (Add or remove SSH keys)"},
		{"not an admin", UsersManage{Name: "alice"}.SubMenu(), "1 Grant admin rights (Let alice become root), 3 SSH Keys (Add or remove SSH keys)"},
		{"not listed", UsersManage{Name: "alice", NotRead: "alice isn't a user hardn manages here."}.SubMenu(), ""},
		{"keys", usrTestKeys.SubMenu(), "1 Add SSH key (), 2 Remove SSH key ()"},
		{"no keys", UsersKeys{Name: "alice"}.SubMenu(), "1 Add SSH key ()"},
		{"keys not read", UsersKeys{Name: "alice", NotRead: "x."}.SubMenu(), ""},
	} {
		if got := items(tt.vm); got != tt.want {
			t.Errorf("%s: %s, want %s", tt.name, got, tt.want)
		}
	}
}

// TestUsersChanges: each change's title, question and review, the keys
// counted as they stand after it.
func TestUsersChanges(t *testing.T) {
	sudo := component.KeyValue{Key: "Sudo", Value: "Disabled"}
	for _, tt := range []struct {
		c                     UsersChange
		title, question, rows string
	}{
		{UsersRemoveChange("alice", sudo), "Remove admin rights", "Remove alice's admin rights?", "Sudo Disabled"},
		{UsersGrantChange("alice", sudo), "Grant admin rights", "Grant alice admin rights?", "Sudo Disabled"},
		{UsersAskChange("alice", sudo), "Ask for password", "Ask alice's password for admin rights?", "Sudo Disabled"},
		{UsersStopAskingChange("alice", sudo), "Don't ask password", "Stop asking alice's password for admin rights?", "Sudo Disabled"},
		{UsersAddKeyChange("alice", 0), "Add SSH key", "Add the key for alice?", "SSH Keys 1 key"},
		{UsersRemoveKeyChange("george", 2), "Remove SSH key", "Remove the key from george?", "SSH Keys 1 key"},
		{UsersRemoveKeyChange("george", 1), "Remove SSH key", "Remove the key from george?", "SSH Keys None"},
	} {
		var rows []string
		for _, kv := range tt.c.Review.Items {
			rows = append(rows, kv.Key+" "+kv.Value)
		}
		if tt.c.Step.Title.Title != tt.title || tt.c.Step.Title.Parents[0] != usersTitle || tt.c.Question != tt.question || strings.Join(rows, ", ") != tt.rows {
			t.Errorf("%+v: want %q, %q, %q", tt.c, tt.title, tt.question, tt.rows)
		}
	}
	if rv := UsersCreateReview(sudo, false); len(rv.Items) != 3 || rv.Items[1].Value != "None" || rv.Items[2].Key != UsersPasswordKey || UsersCreateQuestion("carol") != "Create user 'carol'?" {
		t.Errorf("Create: %+v", rv)
	}
}
