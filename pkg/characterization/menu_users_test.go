package characterization

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	osuser "os/user"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/application"
	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/infrastructure"
	"github.com/abbott/hardn/pkg/interfaces"
	"github.com/abbott/hardn/pkg/menu"
	"github.com/abbott/hardn/pkg/osdetect"
)

// The User Management screen (main menu 1), its Manage User, Manage Sudo
// and SSH key sub-screens and its Create a user form, refactor plan slice
// 5's evidence: each option on a script, in a child process of this test
// binary, as TestLegacyMenuFirewall ran slice 4's (frozen since under
// pkg/menu/testdata/legacy). The mock commander answers id, getent and
// groups as a usrHost says, and the real file system under a temp dir
// stands for /. What hardn -c -u alice runs, live, is recorded by
// TestOperationGoldens (ops/create_user_c_path/*.golden; an existing user,
// ops/configure_sudo/*.golden), and its refusal under -n by cmd/hardn
// TestDryRunRefusesOperations: not repeated here.

const (
	usrChild   = "HARDN_CHARACTERIZATION_USERS"
	usrDir     = "HARDN_CHARACTERIZATION_USERS_DIR"
	usrTrailer = "\x00trailer\n"
	// usrHostUser stands, in a script and a golden, for a name that exists
	// on the machine running the test, as os/user finds it.
	usrHostUser = "$HOSTUSER"
	// A second synthetic key; the key body is not a real key.
	usrKey2 = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIEXAMPLEKEYEXAMPLEKEYEXAMPLEKEYEXAMPLEKEY1 george-example"
)

// usrUser is one account on the fixture host.
type usrUser struct {
	name    string
	uid     int
	sudo    bool   // in the sudo (wheel on Alpine) group
	sudoers string // /etc/sudoers.d/<name>, "" for none
	keys    []string
}

// usrCase is one run of the screen.
type usrCase struct {
	name, screen, script string
	alpine               bool
	username             string // hardn.yml's username
	sudoNoPassword       bool   // hardn.yml's sudoNoPassword
	cfgKeys              []string
	dryRun               bool
	users                []usrUser
	needsHostUser        bool
	notes, defects       []string
}

func usrAlice(sudoers string, keys ...string) usrUser {
	return usrUser{name: "alice", uid: 1000, sudo: true, sudoers: sudoers, keys: keys}
}

const (
	nopasswd = "alice ALL=(ALL) NOPASSWD: ALL\n"
	passwd   = "alice ALL=(ALL) ALL\n"
)

func usrCases() []usrCase {
	const (
		rootStands  = "hardn.yml's username is root here: the screen decides by os/user on the machine running it (user_options.go:69), not by the host's id, so the test needs a name every machine has."
		lookupHost  = "Whether a name exists is asked of os/user (user_options.go:69, :600, :667; ssh_keys_options.go:76), the repository asks `id` (os_user_repository.go:60): the same host on a real run, two sources here."
		v2          = "V2: under dry run the same commands and writes run; only the success line is left out (user_options.go:401, :491, :542, :748, :835)."
		sudoIgnored = "The answer to \"Allow sudo access?\" is not passed on: CreateUser is called with hasSudo true (user_options.go:744); the summary says Disabled and the user gets sudo. V22."
		v17         = "V17: adduser --disabled-password and no password set; with \"Required\" sudo the user has no password to give it."
		v16         = "V16: /etc/sudoers.d/<user> written in place, mode 0440, no visudo -c; any env_keep line there is dropped."
		v22Append   = "V22: each key is appended again through su - <user> -c 'cat >> ~/.ssh/authorized_keys' with no dedupe (os_user_repository.go:218); the FileSSHRepository write after it dedupes (file_ssh_repository.go:316)."
		v21         = "V21: root writes /home/<user>/.ssh/authorized_keys, home assumed, and runs sh -c \"chown -R u:u <dir>\" (file_ssh_repository.go:339)."
		removeNoop  = "V22: Remove SSH key is a no-op that reports success: it calls CreateUser with the other keys (user_options.go:538), which appends them and removes nothing; the removed key stays in authorized_keys."
		sudoRewrite = "Every key change and every sudo toggle goes through CreateUser (user_options.go:397, :538; menu_manager.go:43): sudoers rewritten, every key appended again."
		polarity    = "The form asks \"Allow sudo without password?\" (user_options.go:687); the Figma frame USER-CREATE_USER asks \"Require sudo password?\": the same y means the opposite (proposals.md P-11)."
		hidden      = "The menu hides this item here, and the switch still takes it (user_options.go:79-91, :787-842)."
		pauses2     = "Answering n prints one pause and reads two lines: one inside the box's closure, one after it with no prompt (user_options.go:370-372, :411)."
		mutates     = "hardn.yml is saved to ./hardn.yml in the working directory, not the file hardn loaded, and holds the whole configuration (user_options.go:610, :775; ssh_keys_options.go:68, :137)."
		alpineSudo  = "V14: on Alpine the user is added to wheel and /etc/sudoers.d written; sudo is never installed, and nothing says so (os_user_repository.go:96-108)."
		typo        = "The success line says \"creatingd\" or \"updatingd\" (user_options.go:839)."
		planOnly    = "The trunk refuses main menu 1 under plan only before this screen is built (main_menu.go:819-824); origin/main opened it, and this is what it ran."
	)
	present := "root"
	key1 := fixtureKey
	alice := usrAlice(nopasswd, key1)
	alicePw := usrAlice(passwd, key1)
	twoKeys := usrAlice(nopasswd, key1, usrKey2)
	noKeys := usrAlice(nopasswd)
	const create = "2\ngeorge\n"
	return []usrCase{
		// Open.
		{name: "open_no_configured_user", screen: "open, no username in hardn.yml, q", script: "q\n", users: []usrUser{alice},
			notes: []string{"With no username the menu offers 1 to 3; item 4, Create user, is hidden (user_options.go:132)."}},
		{name: "open_configured_user_on_host", screen: "open, hardn.yml's username present on the host, q", script: "q\n", username: present, users: []usrUser{alice},
			notes: []string{rootStands, "With the configured user present the menu offers only Manage a user and Create a user (user_options.go:79-91)."}},
		{name: "open_configured_user_absent", screen: "open, hardn.yml's username absent from the host, q", script: "q\n", username: "alice", sudoNoPassword: true,
			notes: []string{"Absent: the menu offers Change username, the sudo-password toggle, Manage SSH keys and Create user (user_options.go:92-139).", "The toggle's title names what choosing it does: with sudoNoPassword true it reads \"Require sudo password\" (user_options.go:110-122)."}},

		// Create a user (simplified menu, item 2).
		{name: "create_sudo_no_password_with_key", screen: "2, george, sudo y, without password y, key y and a key, create y, Enter, q",
			script: create + "y\ny\ny\n" + usrKey2 + "\ny\n\nq\n", username: present, users: []usrUser{alice},
			defects: []string{polarity, v17, v16, v22Append, v21}, notes: []string{rootStands}},
		{name: "create_sudo_password_required_no_key", screen: "2, george, sudo y, without password n, key n, create y, Enter, q",
			script: create + "y\nn\nn\ny\n\nq\n", username: present, users: []usrUser{alice},
			defects: []string{polarity, v17, v16}, notes: []string{rootStands}},
		{name: "create_sudo_no_still_grants_sudo", screen: "2, george, sudo n, key n, create y, Enter, q",
			script: create + "n\nn\ny\n\nq\n", username: present, users: []usrUser{alice},
			defects: []string{sudoIgnored, v17, v16}, notes: []string{rootStands, "With sudo n the password question is not asked; sudoNoPassword stays false, so the grant asks for a password the user does not have."}},
		{name: "create_key_yes_empty", screen: "2, george, sudo y, without password y, key y and an empty line, create y, Enter, q",
			script: create + "y\ny\ny\n\ny\n\nq\n", username: present, users: []usrUser{alice},
			notes: []string{rootStands, "An empty key is dropped without a word; the summary says None configured."}},
		{name: "create_declined", screen: "2, george, sudo y, without password y, key n, create n, Enter, q",
			script: create + "y\ny\nn\nn\n\nq\n", username: present, users: []usrUser{alice}, notes: []string{rootStands}},
		{name: "create_empty_name", screen: "2, an empty name, Enter, q", script: "2\n\n\nq\n", username: present, users: []usrUser{alice}, notes: []string{rootStands}},
		{name: "create_bad_name", screen: "2, George (an upper-case letter), Enter, q", script: "2\nGeorge\n\nq\n", username: present, users: []usrUser{alice}, notes: []string{rootStands}},
		{name: "create_reserved_name", screen: "2, admin, Enter, q", script: "2\nadmin\n\nq\n", username: present, users: []usrUser{alice}, notes: []string{rootStands}},
		{name: "create_existing_name", screen: "2, a name present on the machine, Enter, q", script: "2\n" + usrHostUser + "\n\nq\n", username: present, users: []usrUser{alice},
			needsHostUser: true, notes: []string{rootStands, lookupHost, usrHostUser + " stands for the account running the test, which os/user finds; the fixture host does not hold it."}},
		{name: "create_dry_run_mutates", screen: "2 in dry-run mode, george, sudo y, without password y, key y and a key, create y, Enter, q",
			script: create + "y\ny\ny\n" + usrKey2 + "\ny\n\nq\n", username: present, users: []usrUser{alice}, dryRun: true,
			defects: []string{v2, "Under dry run the screen says \"Creating user 'george'...\" and then nothing: no success line, no plan."}, notes: []string{rootStands, planOnly}},

		// Manage a user (simplified menu, item 1).
		{name: "manage_select_user_menu", screen: "1, user 1 (alice), 0, q", script: "1\n1\n0\nq\n", username: present, users: []usrUser{alice},
			notes: []string{rootStands}},
		{name: "manage_select_0", screen: "1, 0, q", script: "1\n0\nq\n", username: present, users: []usrUser{alice}, notes: []string{rootStands}},
		{name: "manage_select_bad_number", screen: "1, 9, Enter, q", script: "1\n9\n\nq\n", username: present, users: []usrUser{alice}, notes: []string{rootStands}},
		{name: "manage_invalid_option", screen: "1, 1 (alice), 7, Enter, q", script: "1\n1\n7\n\nq\n", username: present, users: []usrUser{alice}, notes: []string{rootStands}},
		{name: "manage_sudo_require_password", screen: "1, 1 (alice, NOPASSWD), 1 (Sudo Method), y, Enter, q", script: "1\n1\n1\ny\n\nq\n", username: present, users: []usrUser{alice},
			defects: []string{sudoRewrite, v16, v22Append, v17}, notes: []string{rootStands}},
		{name: "manage_sudo_require_password_dry_run", screen: "1, 1 (alice, NOPASSWD), 1 (Sudo Method) in dry-run mode, y, Enter, q", script: "1\n1\n1\ny\n\nq\n", username: present, users: []usrUser{alice}, dryRun: true,
			defects: []string{v2, sudoRewrite}, notes: []string{rootStands, planOnly}},
		{name: "manage_sudo_require_password_answer_n", screen: "1, 1 (alice, NOPASSWD), 1 (Sudo Method), n, Enter, Enter, q", script: "1\n1\n1\nn\n\n\nq\n", username: present, users: []usrUser{alice},
			defects: []string{pauses2}, notes: []string{rootStands}},
		{name: "manage_sudo_drop_password", screen: "1, 1 (alice, password required), 1 (Sudo Method), Enter, q", script: "1\n1\n1\n\nq\n", username: present, users: []usrUser{alicePw},
			defects: []string{"Dropping the password asks nothing: one key writes NOPASSWD (user_options.go:339-344, :397). " + sudoRewrite, v16, v22Append}, notes: []string{rootStands}},
		{name: "manage_sudo_drop_password_dry_run", screen: "1, 1 (alice, password required), 1 (Sudo Method) in dry-run mode, Enter, q", script: "1\n1\n1\n\nq\n", username: present, users: []usrUser{alicePw}, dryRun: true,
			defects: []string{v2, sudoRewrite}, notes: []string{rootStands, planOnly}},
		{name: "manage_keys_add", screen: "1, 1 (alice), 2 (Manage SSH keys), 1, a key, Enter, q", script: "1\n1\n2\n1\n" + usrKey2 + "\n\nq\n", username: present, users: []usrUser{alice},
			defects: []string{v21}, notes: []string{rootStands, "The key is not checked: anything typed is written (user_options.go:480-487)."}},
		{name: "manage_keys_add_dry_run", screen: "1, 1 (alice), 2, 1 in dry-run mode, a key, Enter, q", script: "1\n1\n2\n1\n" + usrKey2 + "\n\nq\n", username: present, users: []usrUser{alice}, dryRun: true,
			defects: []string{v2}, notes: []string{rootStands, planOnly}},
		{name: "manage_keys_remove_is_a_noop", screen: "1, 1 (alice, two keys), 2, 2 (Remove SSH key), 1, Enter, q", script: "1\n1\n2\n2\n1\n\nq\n", username: present, users: []usrUser{twoKeys},
			defects: []string{removeNoop, sudoRewrite, v16}, notes: []string{rootStands}},
		{name: "manage_keys_remove_hidden_no_keys", screen: "1, 1 (alice, no key), 2, 2 (hidden), Enter, q", script: "1\n1\n2\n2\n\nq\n", username: present, users: []usrUser{noKeys},
			notes: []string{rootStands, "Remove SSH key is hidden with no key (user_options.go:458) and still taken: \"No SSH keys to remove.\""}},

		// Hidden items.
		{name: "hidden_3_ssh_keys_menu", screen: "3 (hidden), then 0, q", script: "3\n0\nq\n", username: present, users: []usrUser{alice},
			notes: []string{rootStands, hidden, "The SSH Key Management box says \"Last login: 2023-10-01\" and \"This is a bottom line example.\" whoever the user (ssh_keys_menu.go:62, :155)."}},
		{name: "hidden_4_declined", screen: "4 (hidden), no SSH keys configured, n, Enter, q", script: "4\nn\n\nq\n", username: present, users: []usrUser{alice},
			notes: []string{rootStands, hidden}},
		{name: "hidden_4_updates_configured_user", screen: "4 (hidden), no SSH keys configured, y", script: "4\ny\n", username: present, sudoNoPassword: true, users: []usrUser{alice},
			defects: []string{"A hidden item rewrites the configured user's sudoers with NOPASSWD and leaves the screen (user_options.go:831, :842); here that user is root.", v16},
			notes:   []string{rootStands, hidden, typo}},
		{name: "hidden_4_no_username", screen: "no username, 4 (hidden), Enter, q", script: "4\n\nq\n", users: []usrUser{alice},
			notes: []string{hidden}},

		// The menu for an absent configured user.
		{name: "absent_1_change_username", screen: "absent user alice, 1, george, Enter, q", script: "1\ngeorge\n\nq\n", username: "alice", sudoNoPassword: true,
			defects: []string{mutates}},
		{name: "absent_2_toggle_sudo_password", screen: "absent user alice, 2 (Require sudo password), Enter, q", script: "2\n\nq\n", username: "alice", sudoNoPassword: true,
			defects: []string{mutates}, notes: []string{"Item 2 changes hardn.yml only; nothing on the host changes."}},
		{name: "absent_2_toggle_sudo_password_dry_run", screen: "absent user alice, 2 in dry-run mode, Enter, q", script: "2\n\nq\n", username: "alice", sudoNoPassword: true, dryRun: true,
			defects: []string{mutates, "Saved under dry run."}, notes: []string{planOnly}},
		{name: "absent_3_add_key_saves_config", screen: "absent user alice, 3, 1, a key, Enter, 0, q", script: "3\n1\n" + usrKey2 + "\n\n0\nq\n", username: "alice", sudoNoPassword: true,
			defects: []string{mutates}, notes: []string{"The key is added to hardn.yml only; with the user absent nothing reaches the host (ssh_keys_options.go:75-87)."}},
		{name: "absent_3_invalid_key", screen: "absent user alice, 3, 1, a line not starting ssh-, Enter, 0, q", script: "3\n1\nnot-a-key\n\n0\nq\n", username: "alice", sudoNoPassword: true},
		{name: "absent_4_create_user", screen: "absent user alice, one key configured, 4 (Create user)", script: "4\n", username: "alice", sudoNoPassword: true, cfgKeys: []string{fixtureKey},
			defects: []string{v17, v16, v22Append, v21}, notes: []string{"Item 4 leaves the screen with no pause (user_options.go:842).", typo}},
		{name: "absent_4_create_user_dry_run", screen: "absent user alice, one key configured, 4 in dry-run mode", script: "4\n", username: "alice", sudoNoPassword: true, cfgKeys: []string{fixtureKey}, dryRun: true,
			defects: []string{v2}, notes: []string{planOnly}},

		// Alpine.
		{name: "alpine_create_user_with_sudo", screen: "Alpine, 2, george, sudo y, without password y, key y and a key, create y, Enter, q",
			script: create + "y\ny\ny\n" + usrKey2 + "\ny\n\nq\n", alpine: true, username: present, users: []usrUser{alice},
			defects: []string{alpineSudo, v17, v16, v21}, notes: []string{rootStands}},
		{name: "alpine_manage_sudo_require_password", screen: "Alpine, 1, 1 (alice in wheel, NOPASSWD), 1 (Sudo Method), y, Enter, q", script: "1\n1\n1\ny\n\nq\n", alpine: true, username: present, users: []usrUser{alice},
			defects: []string{alpineSudo, sudoRewrite, v16}, notes: []string{rootStands}},

		// Keys.
		{name: "invalid_key", screen: "7 (no such option), Enter, q", script: "7\n\nq\n", username: present, users: []usrUser{alice}, notes: []string{rootStands}},
		{name: "return_0", screen: "0", script: "0\n", username: present, users: []usrUser{alice}, notes: []string{rootStands}},
		{name: "quit_q", screen: "q", script: "q\n", username: present, users: []usrUser{alice}, notes: []string{rootStands}},
	}
}

// TestLegacyMenuUsers records origin/main's User Management screen, each
// option in a child process of this test binary whose stdin is the script,
// not a terminal, and whose working directory and HOME are a temp dir
// (HOME holding a .hardn.yml, so the repository's own LoadConfig finds a
// file and asks nothing); a second temp dir stands for /. Each golden
// holds the screen as printed (ESC as \x1b), the calls the repositories
// made in order, the files under / as they end with their modes, and
// ./hardn.yml as written. The trunk differs from origin/main in these
// files by its pauses, which say "Press Enter" (DEV-050). The screen is
// built as main_menu.go:781 and menu_factory.go:45-60 build it.
func TestLegacyMenuUsers(t *testing.T) {
	if name := os.Getenv(usrChild); name != "" {
		runUsersChild(name, os.Getenv(usrDir))
		os.Exit(0)
	}
	for _, name := range []string{"alice", "george"} {
		if _, err := osuser.Lookup(name); err == nil {
			t.Skipf("this machine has a user %s; the cases need it absent", name)
		}
	}
	if _, err := osuser.Lookup("root"); err != nil {
		t.Skip("this machine has no root user")
	}
	hostUser := ""
	if u, err := osuser.Current(); err == nil {
		hostUser = u.Username
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range usrCases() {
		t.Run(tc.name, func(t *testing.T) {
			if tc.needsHostUser && (hostUser == "" || hostUser == "root" || strings.ToLower(hostUser) != hostUser || strings.ContainsAny(hostUser, " .")) {
				t.Skipf("the account running the test (%q) is not a name the form takes", hostUser)
			}
			dir, root := t.TempDir(), t.TempDir()
			seeds := usrSeed(t, tc, root)
			home := filepath.Join(dir, "home")
			if err := os.MkdirAll(home, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(home, ".hardn.yml"), []byte("sshKeyPath: .ssh_%u\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			script := strings.ReplaceAll(tc.script, usrHostUser, hostUser)

			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			cmd := exec.CommandContext(ctx, exe, "-test.run=^TestLegacyMenuUsers$")
			cmd.Env = append(os.Environ(), usrChild+"="+tc.name, usrDir+"="+root, "NO_COLOR=", "RUNEWIDTH_EASTASIAN=0", "HARDN_CONFIG=", "HOME="+home)
			cmd.Dir = dir
			cmd.Stdin = strings.NewReader(script)
			out, err := cmd.Output()
			if err != nil {
				t.Fatalf("child: %v\n%s", err, out)
			}
			screen, trailer, _ := strings.Cut(string(out), usrTrailer)

			host := "Debian 13"
			if tc.alpine {
				host = "Alpine 3.23"
			}
			var b strings.Builder
			b.WriteString(baselineHeader + "\n")
			b.WriteString("# Generated by TestLegacyMenuUsers (pkg/characterization); regenerate with -update. Do not edit.\n")
			fmt.Fprintf(&b, "# Screen: User Management (main menu 1) %s (pkg/menu), run in a child process\n", tc.screen)
			fmt.Fprintf(&b, "# Script: stdin %q, not a terminal, so nothing typed is echoed\n", tc.script)
			fmt.Fprintf(&b, "# Start: %s; hardn.yml username %q, sudoNoPassword %t, sshKeys %d; dryRun %t\n", host, tc.username, tc.sudoNoPassword, len(tc.cfgKeys), tc.dryRun)
			fmt.Fprintf(&b, "# Host users (UID >= 1000): %s\n", usrWords(tc))
			b.WriteString("# Fixture: the mock commander runs nothing, so adduser leaves /etc/passwd as seeded (a created user is not listed on redraw) and su ... cat >> writes no file; only the FileSSHRepository and sudoers writes reach the files below.\n")
			fmt.Fprintf(&b, "# Pauses: %d (\"Press Enter to ...\"; origin/main says \"Press any key to ...\", DEV-050)\n", strings.Count(screen, "Press Enter to"))
			for _, d := range tc.defects {
				fmt.Fprintf(&b, "# KNOWN-DEFECT: %s\n", d)
			}
			for _, n := range tc.notes {
				fmt.Fprintf(&b, "# NOTE: %s\n", n)
			}
			b.WriteString("# The screen as printed, ESC as the four characters \\x1b, each line ending in $; <no-newline> marks text with no final newline.\n")
			b.WriteString("# ----\n")
			b.WriteString(usrVisible(screen))
			b.WriteString("# ----\n")
			b.WriteString(trailer)
			b.WriteString(usrFiles(t, root, seeds))
			if saved, err := os.ReadFile(filepath.Join(dir, "hardn.yml")); err != nil {
				b.WriteString("# ./hardn.yml: not written\n")
			} else {
				b.WriteString("# ./hardn.yml as written:\n")
				b.Write(saved)
			}
			got := strings.ReplaceAll(strings.ReplaceAll(b.String(), root, "$ROOT"), dir, "$CWD")
			if hostUser != "" && tc.needsHostUser {
				got = strings.ReplaceAll(got, hostUser, usrHostUser)
			}
			for _, d := range []string{dir, root} {
				if resolved, err := filepath.EvalSymlinks(d); err == nil && strings.Contains(got, resolved) {
					t.Fatalf("golden would contain the temp dir %s", resolved)
				}
			}
			assertGolden(t, "menu/users_"+tc.name+".golden", got)
		})
	}
}

func usrWords(tc usrCase) string {
	if len(tc.users) == 0 {
		return "none"
	}
	var parts []string
	for _, u := range tc.users {
		group := "sudo"
		if tc.alpine {
			group = "wheel"
		}
		s := fmt.Sprintf("%s uid %d", u.name, u.uid)
		if u.sudo {
			s += ", in " + group
		}
		if u.sudoers != "" {
			s += fmt.Sprintf(", sudoers.d %q", strings.TrimSpace(u.sudoers))
		}
		s += fmt.Sprintf(", authorized_keys lines %d", len(u.keys))
		parts = append(parts, s)
	}
	return strings.Join(parts, "; ")
}

// usrSeed writes the fixture host under root and returns what it wrote.
func usrSeed(t *testing.T, tc usrCase, root string) map[string]string {
	t.Helper()
	group := "sudo"
	if tc.alpine {
		group = "wheel"
	}
	passwdLines := []string{"root:x:0:0:root:/root:/bin/bash", "daemon:x:1:1:daemon:/usr/sbin:/usr/sbin/nologin"}
	var members []string
	seeds := map[string]string{}
	for _, u := range tc.users {
		passwdLines = append(passwdLines, fmt.Sprintf("%s:x:%d:%d::/home/%s:/bin/bash", u.name, u.uid, u.uid, u.name))
		if u.sudo {
			members = append(members, u.name)
		}
		if u.sudoers != "" {
			seeds["/etc/sudoers.d/"+u.name] = u.sudoers
		}
		if len(u.keys) > 0 {
			seeds["/home/"+u.name+"/.ssh/authorized_keys"] = strings.Join(u.keys, "\n") + "\n"
		}
	}
	seeds["/etc/passwd"] = strings.Join(passwdLines, "\n") + "\n"
	seeds["/etc/group"] = fmt.Sprintf("root:x:0:\n%s:x:27:%s\n", group, strings.Join(members, ","))
	for name, content := range seeds {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		perm := fs.FileMode(0o644)
		switch {
		case strings.HasPrefix(name, "/etc/sudoers.d/"):
			perm = 0o440
		case strings.HasSuffix(name, "authorized_keys"):
			perm = 0o600
		}
		if err := os.WriteFile(path, []byte(content), perm); err != nil {
			t.Fatal(err)
		}
	}
	return seeds
}

// usrFiles lists the files under root, by hardn's paths, with their modes
// and, unless seeded and unchanged, their content.
func usrFiles(t *testing.T, root string, seeds map[string]string) string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(files)
	var b strings.Builder
	b.WriteString("# Files under / afterwards:\n")
	for _, path := range files {
		name := strings.TrimPrefix(path, root)
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		seed, seeded := seeds[name]
		switch {
		case seeded && seed == string(data):
			fmt.Fprintf(&b, "#   %s mode=%s, as seeded\n", name, mode(info.Mode().Perm()))
		default:
			state := "written"
			if seeded {
				state = "changed"
			}
			fmt.Fprintf(&b, "#   %s mode=%s, %s:\n", name, mode(info.Mode().Perm()), state)
			for _, l := range strings.Split(strings.TrimSuffix(string(data), "\n"), "\n") {
				b.WriteString("#     | " + l + "\n")
			}
		}
	}
	return b.String()
}

func usrVisible(screen string) string {
	lines := strings.Split(strings.ReplaceAll(screen, "\x1b", `\x1b`), "\n")
	var b strings.Builder
	for i, l := range lines {
		switch {
		case i < len(lines)-1:
			b.WriteString(l + "$\n")
		case l != "":
			b.WriteString(l + "<no-newline>\n")
		}
	}
	return b.String()
}

// runUsersChild runs the screen of case name, in the child process, over
// root: hardn's defaults with the case's username, sudoNoPassword and
// keys, the managers built by infrastructure.ServiceFactory over the real
// file system under root and the mock commander, both recorded. The mock
// answers id, getent and groups for the fixture's users; george exists
// only once adduser has run. Then it reports, after usrTrailer, what the
// repositories did.
func runUsersChild(name, root string) {
	var tc usrCase
	for _, c := range usrCases() {
		if c.name == name {
			tc = c
		}
	}
	if tc.name == "" || root == "" {
		fmt.Fprintf(os.Stderr, "no users case %q in %q\n", name, root)
		os.Exit(2)
	}
	cfg := config.DefaultConfig()
	cfg.Username, cfg.SudoNoPassword, cfg.SshKeys, cfg.DryRun = tc.username, tc.sudoNoPassword, tc.cfgKeys, tc.dryRun
	cfg.SshPort = fixturePort

	group := "sudo"
	osInfo := &osdetect.OSInfo{OsType: "debian", OsVersion: "13", OsCodename: "trixie"}
	if tc.alpine {
		group = "wheel"
		osInfo = &osdetect.OSInfo{OsType: "alpine", OsVersion: "3.23.0", OsCodename: "3.23.0"}
	}
	mc := interfaces.NewMockCommander()
	exists := func(u string, uid int, groups string) {
		delete(mc.CommandErrors, "id "+u)
		mc.CommandOutputs[fmt.Sprintf("id -u %s", u)] = []byte(fmt.Sprintf("%d\n", uid))
		mc.CommandOutputs[fmt.Sprintf("id -g %s", u)] = []byte(fmt.Sprintf("%d\n", uid))
		mc.CommandOutputs["getent passwd "+u] = []byte(fmt.Sprintf("%s:x:%d:%d::/home/%s:/bin/bash\n", u, uid, uid, u))
		mc.CommandOutputs["groups "+u] = []byte(u + " : " + groups + "\n")
	}
	known := map[string]bool{}
	for _, u := range tc.users {
		groups := u.name
		if u.sudo {
			groups += " " + group
		}
		exists(u.name, u.uid, groups)
		known[u.name] = true
	}
	for _, u := range []string{"alice", "george"} {
		if !known[u] {
			mc.CommandErrors["id "+u] = fmt.Errorf("id: '%s': no such user", u)
		}
	}
	log := &trace{}
	commander := &recordingCommander{inner: mc, log: log, afterExec: func(argv []string) {
		if argv[0] == "adduser" && mc.CommandErrors["id "+argv[len(argv)-1]] != nil {
			exists(argv[len(argv)-1], 1001, argv[len(argv)-1])
		}
	}}
	provider := &interfaces.Provider{FS: recordingFS{inner: usrRootFS(root), log: log}, Commander: commander, Network: interfaces.NewMockNetworkOperations()}
	factory := infrastructure.NewServiceFactory(provider, osInfo)
	factory.SetConfig(cfg)
	mm := application.NewMenuManager(factory.CreateUserManager(), factory.CreateSSHManager(), nil, nil, nil, factory.CreateHostInfoManager())
	menu.NewUserMenu(mm, cfg, osInfo).Show()

	fmt.Print(usrTrailer)
	fmt.Printf("# The repositories' calls, in order:\n%s", log)
}

// usrRootFS is the real file system under a temp dir standing for /.
type usrRootFS string

var _ interfaces.FileSystem = usrRootFS("")

func (r usrRootFS) at(name string) string { return filepath.Join(string(r), name) }

func (r usrRootFS) ReadFile(name string) ([]byte, error) { return os.ReadFile(r.at(name)) }

// WriteFile writes as root would: a file root cannot write by its mode
// (sudoers.d's 0440) is written all the same, its mode kept, as
// os.WriteFile keeps the mode of a file that exists.
func (r usrRootFS) WriteFile(name string, data []byte, perm fs.FileMode) (err error) {
	path := r.at(name)
	if info, serr := os.Stat(path); serr == nil && info.Mode().Perm()&0o200 == 0 {
		if cerr := os.Chmod(path, info.Mode().Perm()|0o200); cerr != nil {
			return cerr
		}
		defer func() {
			if cerr := os.Chmod(path, info.Mode().Perm()); cerr != nil && err == nil {
				err = cerr
			}
		}()
	}
	return os.WriteFile(path, data, perm)
}
func (r usrRootFS) MkdirAll(path string, perm fs.FileMode) error {
	return os.MkdirAll(r.at(path), perm)
}
func (r usrRootFS) Stat(name string) (os.FileInfo, error) { return os.Stat(r.at(name)) }
func (r usrRootFS) Remove(name string) error              { return os.Remove(r.at(name)) }
func (r usrRootFS) RemoveAll(path string) error           { return os.RemoveAll(r.at(path)) }
