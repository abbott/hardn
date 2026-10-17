package form

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"

	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/prompt"
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/theme"
	"github.com/abbott/hardn/pkg/ui/uitest"
)

// newPrompter returns a Prompter for env whose user types lines, and the
// transcript it writes, the echoed answers included.
func newPrompter(env term.Env, lines []string) (nav.Prompter, *strings.Builder) {
	out := new(strings.Builder)
	return nav.Prompter{Ctx: component.NewCtx(env), In: prompt.New(uitest.NewTypist(out, lines...), false), Out: out}, out
}

// play runs f in ASCII at 80 columns with the user typing lines, and
// returns its error and the transcript without escape sequences.
func play(f Form, lines ...string) (string, error) {
	return playAt(80, f, lines...)
}

// playAt is play at width columns.
func playAt(width int, f Form, lines ...string) (string, error) {
	p, out := newPrompter(term.Fixed(width, colorprofile.NoTTY, false, true), lines)
	err := f.Run(p)
	return ansi.Strip(out.String()), err
}

// validUsername is origin/main's username rule (pkg/menu user_options.go
// validateUsername), in its words, without its length and reserved-name
// checks but root's.
func validUsername(name string) error {
	switch {
	case name == "":
		return errors.New("Username cannot be empty")
	case strings.Contains(name, " "):
		return errors.New("Username cannot contain spaces")
	case !regexp.MustCompile(`^[a-z][a-z0-9_-]*$`).MatchString(name):
		return errors.New("Username must start with a lowercase letter and contain only lowercase letters, numbers, hyphens, and underscores")
	case name == "root":
		return fmt.Errorf("'%s' is a reserved system username and cannot be used", name)
	}
	return nil
}

// newUser holds the answers of the create-user form.
type newUser struct {
	name                   string
	sudo, password, addKey bool
	key                    string
}

// createUser is Figma's create-user form (USER-CREATE_USER): its four
// questions, the third asked only after sudo is granted, and a key
// question, asked only when a key is added, shortened from origin/main's
// "Paste SSH public key (e.g., ssh-ed25519 AAAAC3NzaC1lZDI1...):" (pkg/menu
// ssh_keys_options.go:53). Figma shows no default; P-11 proposes one for
// the password question.
func createUser(u *newUser) Form {
	return Form{Fields: []Field{
		&Text{Title: "Username", Validate: validUsername, Value: &u.name},
		&YesNo{Title: "Grant sudo privilege?", Value: &u.sudo},
		&YesNo{Title: "Require sudo password?", Value: &u.password, Skip: func() bool { return !u.sudo }},
		&YesNo{Title: "Add SSH key?", Value: &u.addKey},
		&Text{Title: "Paste SSH public key", Value: &u.key, Skip: func() bool { return !u.addKey }},
	}}
}

// sshKey is a synthetic public key for alice.
const sshKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIExampleKeyForTestsOnly alice@web-01.example.net"

// users are a form choice's options, george and alice. Figma draws a
// choice of user only as a menu screen (USER-MANAGE_USER-SELECT_USER),
// which nav builds; no Figma screen draws a form's choice (DEV-046).
func users() []Option[string] {
	return []Option[string]{{Label: "george", Value: "george"}, {Label: "alice", Value: "alice"}}
}

// TestCreateUser answers Figma's form as Figma shows it, and checks the
// values bound and the transcript: one line per field, numbered, a blank
// line between fields, the skipped key question left out.
func TestCreateUser(t *testing.T) {
	var u newUser
	out, err := play(createUser(&u), "george", "y", "n", "n")
	if err != nil || u != (newUser{name: "george", sudo: true}) {
		t.Fatalf("Run: %v, %+v", err, u)
	}
	want := "   1. Username [0 or q]: george\n\n" +
		"   2. Grant sudo privilege? [y/N, 0 or q]: y\n\n" +
		"   3. Require sudo password? [y/N, 0 or q]: n\n\n" +
		"   4. Add SSH key? [y/N, 0 or q]: n\n"
	if out != want {
		t.Errorf("transcript\n%s\nwant\n%s", out, want)
	}
}

// TestValidation checks that a value Validate rejects is refused under
// the prompt in the validation's words, and only that prompt asked again:
// a head above it is written once.
func TestValidation(t *testing.T) {
	var name string
	f := Form{Fields: []Field{&Text{Title: "Username", Description: "Specify username to create", Validate: validUsername, Value: &name}}}
	out, err := play(f, "", "George", "root", "george")
	if err != nil || name != "george" {
		t.Fatalf("Run: %v, %q", err, name)
	}
	for _, want := range []string{
		"[!] Error: Username cannot be empty\n",
		"[!] Error: Username must start with a lowercase letter",
		"[!] Error: 'root' is a reserved system username",
	} {
		if strings.Count(out, want) != 1 {
			t.Errorf("transcript holds %q %d times, want once:\n%s", want, strings.Count(out, want), out)
		}
	}
	if n := strings.Count(out, "Specify username to create"); n != 1 {
		t.Errorf("the head is written %d times, want once:\n%s", n, out)
	}
	if n := strings.Count(out, "  > [0 or q]: "); n != 4 {
		t.Errorf("the prompt is asked %d times, want 4:\n%s", n, out)
	}
	// A y/N answer and a choice are validated as a text is.
	var password bool
	var who string
	f = Form{Fields: []Field{
		&YesNo{Title: "Require sudo password?", Value: &password, Validate: func(yes bool) error {
			if !yes {
				return errors.New("A user password is required for sudo usage.") // Figma's warning
			}
			return nil
		}},
		&Select[string]{Title: "Select a user", Options: users(), Value: &who, Validate: func(name string) error {
			if name == "george" {
				return errors.New("george is already managed")
			}
			return nil
		}},
	}}
	out, err = play(f, "n", "y", "1", "2")
	if err != nil || !password || who != "alice" ||
		!strings.Contains(out, "[!] Error: A user password is required for sudo usage.\n") ||
		!strings.Contains(out, "[!] Error: george is already managed\n") {
		t.Errorf("Run: %v, %t, %q:\n%s", err, password, who, out)
	}
}

// TestRefusals checks the lines a field does not take: nav's words, and
// the value left as it was until a line is taken.
func TestRefusals(t *testing.T) {
	var yes bool
	var who string
	tests := []struct {
		name  string
		field Field
		lines []string
		words string
	}{
		{"y/N", &YesNo{Title: "Grant sudo privilege?", Value: &yes}, []string{"maybe", "y"}, nav.InvalidAnswer},
		{"select", &Select[string]{Title: "Select a user", Options: users(), Value: &who}, []string{"3", "", "01", "2"}, nav.InvalidOption},
	}
	for _, tt := range tests {
		out, err := play(Form{Fields: []Field{tt.field}}, tt.lines...)
		if n := strings.Count(out, tt.words); err != nil || n != len(tt.lines)-1 {
			t.Errorf("%s: %v, %d refusals; want nil, %d:\n%s", tt.name, err, n, len(tt.lines)-1, out)
		}
	}
	if !yes || who != "alice" {
		t.Errorf("bound %t and %q, want true and alice", yes, who)
	}
}

// TestDefaults checks that an empty line takes a field's default, and
// that the echo then shows it.
func TestDefaults(t *testing.T) {
	path, sudo, password := "unchanged", true, false
	f := Form{Fields: []Field{
		&Text{Title: "Backup path", Default: "/var/backups/hardn", Value: &path},
		&YesNo{Title: "Grant sudo privilege?", Value: &sudo},
		&YesNo{Title: "Require sudo password?", Default: true, Value: &password},
	}}
	out, err := play(f, "", " ", "")
	if err != nil || path != "/var/backups/hardn" || sudo || !password {
		t.Fatalf("Run: %v, %q, %t, %t", err, path, sudo, password)
	}
	for _, want := range []string{
		"   1. Backup path [0 or q]: \n   1. Backup path: /var/backups/hardn\n",
		"   2. Grant sudo privilege? [y/N, 0 or q]:  \n   2. Grant sudo privilege? [y/N]: n\n",
		"   3. Require sudo password? [Y/n, 0 or q]: \n   3. Require sudo password? [Y/n]: y\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("transcript\n%s\nwant it to hold\n%s", out, want)
		}
	}
	// A text field with no default takes an empty line as an empty value,
	// and echoes nothing.
	f = Form{Fields: []Field{&Text{Title: "Username", Value: &path}}}
	if out, err := play(f, ""); err != nil || path != "" || out != "  Username [0 or q]: \n" {
		t.Errorf("Run: %v, %q, transcript %q", err, path, out)
	}
}

// TestLayout checks when a field is asked on one line and when under a
// head of its own: a description that shows nothing leaves it on one
// line; a choice's options go under its head, and its prompt line asks
// "Enter your choice"; and a question the one line would cut goes under a
// head, whose title has the line to itself, with the hint on the prompt
// line.
func TestLayout(t *testing.T) {
	var name string
	var sudo, password bool
	// newName is origin/main's label for a text default (pkg/menu
	// user_options.go:580).
	const newName = "Enter new username (leave empty to keep current)"
	tests := []struct {
		name  string
		width int
		form  Form
		lines []string
		want  string
	}{
		{"a description that shows nothing", 80, Form{Fields: []Field{
			&Text{Title: "Username", Description: "\x1b[0m", Value: &name},
		}}, []string{"george"}, "  Username [0 or q]: george\n"},
		// newName and "[0 or q]:" fill theme.ContentWidth, 64 columns,
		// exactly: one column fewer cuts the question.
		{"a question too long for its line", 63, Form{Fields: []Field{
			&Text{Title: newName, Value: &name},
			&YesNo{Title: "Grant sudo privilege?", Value: &sudo},
		}}, []string{"george", "y"},
			"   1. " + newName + "\n\n  > [0 or q]: george\n   1. " + newName + ": george\n\n" +
				"   2. Grant sudo privilege? [y/N, 0 or q]: y\n"},
		{"a question that fills its line", 64, Form{Fields: []Field{
			&Text{Title: newName, Value: &name},
			&YesNo{Title: "Grant sudo privilege?", Value: &sudo},
		}}, []string{"george", "y"},
			"   1. " + newName + " [0 or q]: george\n\n   2. Grant sudo privilege? [y/N, 0 or q]: y\n"},
		{"a choice", 80, Form{Fields: []Field{
			&Select[string]{Title: "Select a user", Options: users(), Value: &name},
		}}, []string{"2"},
			"  Select a user\n\n   1) george\n   2) alice\n\n  > Enter your choice [1-2, 0 or q]: 2\n  Select a user: alice\n"},
		{"a narrow terminal", 40, Form{Fields: []Field{
			&YesNo{Title: "Grant sudo privilege?", Value: &sudo},
			&YesNo{Title: "Require sudo password?", Value: &password},
		}}, []string{"y", "n"},
			"   1. Grant sudo privilege?\n\n  > [y/N, 0 or q]: y\n   1. Grant sudo privilege? [y/N]: y\n\n" +
				"   2. Require sudo password?\n\n  > [y/N, 0 or q]: n\n   2. Require sudo password? [y/N]: n\n"},
	}
	for _, tt := range tests {
		if out, err := playAt(tt.width, tt.form, tt.lines...); err != nil || out != tt.want {
			t.Errorf("%s: %v, transcript\n%s\nwant\n%s", tt.name, err, out, tt.want)
		}
	}
}

// TestNarrow checks the create-user form at every width from 20 to 130:
// each question shows whole wherever its head's line has room for it, and
// its hint whole wherever the prompt line has room for it, so no question
// is cut before its hint or dropped for it.
func TestNarrow(t *testing.T) {
	questions := []struct{ title, hint string }{
		{"Username", "[0 or q]:"},
		{"Grant sudo privilege?", "[y/N, 0 or q]:"},
		{"Require sudo password?", "[y/N, 0 or q]:"},
		{"Add SSH key?", "[y/N, 0 or q]:"},
		{"Paste SSH public key", "[0 or q]:"},
	}
	for w := 20; w <= 130; w++ {
		var u newUser
		out, err := playAt(w, createUser(&u), "george", "y", "n", "y", "key")
		if err != nil {
			t.Fatalf("width %d: %v", w, err)
		}
		room := min(w, theme.ContentWidth)
		for i, q := range questions {
			// field is the transcript from the field's number to the next's.
			field, _, _ := strings.Cut(out[strings.Index(out, fmt.Sprintf("   %d. ", i+1)):], fmt.Sprintf("   %d. ", i+2))
			head := fmt.Sprintf("   %d. %s", i+1, q.title)
			if lipgloss.Width(head) <= room && !strings.HasPrefix(field, head+" "+q.hint) && !strings.HasPrefix(field, head+"\n") {
				t.Errorf("width %d: %q is not shown whole:\n%s", w, q.title, field)
			}
			if lipgloss.Width("  > "+q.hint) <= room && !strings.HasPrefix(field, head+" "+q.hint) && !strings.Contains(field, "\n  > "+q.hint) {
				t.Errorf("width %d: the hint of %q is not shown whole:\n%s", w, q.title, field)
			}
		}
	}
}

// TestSkip checks that a skipped field takes no number and is put back
// as it was before Run, even after an answer on a path the user left.
func TestSkip(t *testing.T) {
	u := newUser{password: true, key: "kept"}
	// sudo granted, the password question answered n; back to sudo, now
	// refused, so the password question is skipped and put back.
	out, err := play(createUser(&u), "george", "y", "n", "0", "0", "n", "n")
	if err != nil || u != (newUser{name: "george", password: true, key: "kept"}) {
		t.Fatalf("Run: %v, %+v", err, u)
	}
	if !strings.HasSuffix(out, "   2. Grant sudo privilege? [y/N, 0 or q]: n\n\n   3. Add SSH key? [y/N, 0 or q]: n\n") {
		t.Errorf("the field after a skipped one is not numbered 3:\n%s", out)
	}
}

// TestDynamic checks that TitleFunc and OptionsFunc are called each time
// their field is asked, from the answers before it.
func TestDynamic(t *testing.T) {
	var name, who string
	var create bool
	f := Form{Fields: []Field{
		&Text{Title: "Username", Validate: validUsername, Value: &name},
		&Select[string]{
			Title: "Copy SSH keys from",
			OptionsFunc: func() []Option[string] {
				var o []Option[string]
				for _, u := range users() {
					if u.Value != name {
						o = append(o, u)
					}
				}
				return o
			},
			Value: &who,
		},
		&YesNo{TitleFunc: func() string { return fmt.Sprintf("Create user '%s'?", name) }, Value: &create},
	}}
	// george first, then back to the name: alice, whom the choice then
	// leaves out.
	out, err := play(f, "george", "1", "0", "0", "alice", "1", "y")
	if err != nil || name != "alice" || who != "george" || !create {
		t.Fatalf("Run: %v, %q, %q, %t", err, name, who, create)
	}
	for _, want := range []string{"   1) alice\n", "   1) george\n", "   3. Create user 'alice'? [y/N, 0 or q]: y\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("transcript\n%s\nwant it to hold %q", out, want)
		}
	}
}

// TestNavigation checks back, quit and abort at every field of the form:
// back returns to the field asked before it, and leaves the form from the
// first; quit and abort end it. Whenever Run fails, every value is as it
// was before.
func TestNavigation(t *testing.T) {
	answers := []string{"george", "y", "n", "y", sshKey}
	before := newUser{name: "alice", addKey: true, key: "kept"}
	for k := range answers {
		for _, end := range []struct {
			line string
			want error
		}{{"q", nav.ErrQuit}, {"", prompt.ErrEndOfInput}} {
			u := before
			lines := append([]string(nil), answers[:k]...)
			if end.line != "" {
				lines = append(lines, end.line)
			}
			if _, err := play(createUser(&u), lines...); !errors.Is(err, end.want) || u != before {
				t.Errorf("%q at field %d: %v, %+v; want %v and the values before", end.line, k+1, err, u, end.want)
			}
		}
		u := before
		lines := append(append([]string(nil), answers[:k]...), "0")
		if k > 0 {
			lines = append(lines, answers[k-1:]...)
		}
		_, err := play(createUser(&u), lines...)
		switch {
		case k == 0 && (!errors.Is(err, nav.ErrBack) || u != before):
			t.Errorf("0 at field 1: %v, %+v; want ErrBack and the values before", err, u)
		case k > 0 && (err != nil || u != newUser{name: "george", sudo: true, addKey: true, key: sshKey}):
			t.Errorf("0 at field %d, then the rest: %v, %+v", k+1, err, u)
		}
	}
	// b does not go back: back is 0 only (P-25). A text field takes it as
	// its value, and a y/N field refuses it. Ctrl-C aborts as the end of
	// the input does, as the reader reports it (its own tests send
	// SIGINT), and ErrAborted is the reader's sentinel, which both wrap.
	var u newUser
	out, err := play(createUser(&u), "b", "b", "y", "n", "n")
	if err != nil || u != (newUser{name: "b", sudo: true}) || strings.Count(out, "1. Username") != 1 || strings.Count(out, nav.InvalidAnswer) != 1 {
		t.Errorf("b at fields 1 and 2: %v, %+v:\n%s", err, u, out)
	}
	if ErrAborted != prompt.ErrAborted {
		t.Error("ErrAborted is not prompt.ErrAborted")
	}
}

// TestFieldErrors checks the fields that cannot be asked: Run returns
// ErrField, writes nothing for a field it cannot show, and puts back
// every value.
func TestFieldErrors(t *testing.T) {
	name := "alice"
	tests := []struct {
		name  string
		field Field
	}{
		{"nil", nil},
		{"a nil *Text", (*Text)(nil)},
		{"a nil *YesNo", (*YesNo)(nil)},
		{"a nil *Select", (*Select[string])(nil)},
		{"no value", &Text{Title: "Username"}},
		{"no title", &Text{Value: new(string)}},
		{"a title that shows nothing", &YesNo{Title: "\x1b[0m", Value: new(bool)}},
		{"no title with a description", &Text{Description: "Specify username to create", Value: new(string)}},
		{"no options", &Select[int]{Title: "Select a user", Value: new(int)}},
		{"an option with no label", &Select[string]{Title: "Select a user", Options: []Option[string]{{Value: "george"}}, Value: new(string)}},
	}
	for _, tt := range tests {
		f := Form{Fields: []Field{&Text{Title: "Username", Value: &name}, tt.field}}
		out, err := play(f, "george")
		if !errors.Is(err, ErrField) || name != "alice" || strings.Count(out, "\n") > 1 {
			t.Errorf("%s: %v, %q, transcript %q; want ErrField, alice, the first field only", tt.name, err, name, out)
		}
	}
}

// errDiskFull is failWriter's error.
var errDiskFull = errors.New("disk full")

// failWriter fails each write once left writes have succeeded.
type failWriter struct{ left *int }

func (w failWriter) Write(b []byte) (int, error) {
	if *w.left == 0 {
		return 0, errDiskFull
	}
	*w.left--
	return len(b), nil
}

// TestWriteFails checks that a write that fails ends the form, the
// values put back: the first prompt, which the reader writes, and the
// form's own writes, the gap before the next field and an echo. The
// reader's newline after a line read from a pipe comes between the
// prompt and either of the form's writes, so they are the third write.
func TestWriteFails(t *testing.T) {
	var u newUser
	sudo := true
	tests := []struct {
		name  string
		form  Form
		lines []string
		left  int
		by    string // the prefix of the error: whose write failed
	}{
		{"the prompt", createUser(&u), []string{"george"}, 0, "prompt: write: "},
		{"the gap", createUser(&u), []string{"george"}, 2, "form: write: "},
		{"the echo", Form{Fields: []Field{&YesNo{Title: "Grant sudo privilege?", Value: &sudo}}}, []string{""}, 2, "form: write: "},
	}
	for _, tt := range tests {
		p, _ := newPrompter(term.Fixed(80, colorprofile.NoTTY, false, true), tt.lines)
		p.Out = failWriter{&tt.left}
		err := tt.form.Run(p)
		if !errors.Is(err, errDiskFull) || !strings.HasPrefix(fmt.Sprint(err), tt.by) || u != (newUser{}) || !sudo {
			t.Errorf("%s: %v, %+v, %t; want %q from errDiskFull and the values before", tt.name, err, u, sudo, tt.by)
		}
	}
}

// TestGolden pins each kind of field in its states, asked, refused and
// answered, and Figma's form, on the core set, as transcripts with the
// answers echoed as a terminal shows them.
func TestGolden(t *testing.T) {
	var name, who string
	var yes bool
	u := new(newUser)
	tests := []struct {
		name  string
		form  Form
		lines []string
	}{
		// text: refused by its validation, then answered on its line.
		{"text", Form{Fields: []Field{&Text{Title: "Username", Validate: validUsername, Value: &name}}}, []string{"George", "george"}},
		// text-description: asked under its head, refused, then echoed.
		{"text-description", Form{Fields: []Field{
			&Text{Title: "Username", Description: "Specify username to create", Validate: validUsername, Value: &name},
		}}, []string{"", "george"}},
		// yes-no: refused, then the default taken and echoed.
		{"yes-no", Form{Fields: []Field{&YesNo{Title: "Grant sudo privilege?", Value: &yes}}}, []string{"maybe", ""}},
		// select: refused, then a choice echoed by its label.
		{"select", Form{Fields: []Field{&Select[string]{Title: "Select a user", Options: users(), Value: &who}}}, []string{"3", "2"}},
		// create-user: Figma's form as Figma answers it.
		{"create-user", createUser(u), []string{"george", "y", "n", "n"}},
		// back: back from field 3 to 2, whose new answer skips 3; the key
		// question, now asked, is numbered 4. The key typed is cut short,
		// so that its echo, which is the terminal's, fits 60 columns.
		{"back", createUser(u), []string{"george", "y", "0", "n", "y", sshKey[:20]}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			uitest.Golden(t, "form/"+tt.name, uitest.Core(), func(env term.Env) string {
				p, out := newPrompter(env, tt.lines)
				if err := tt.form.Run(p); err != nil {
					t.Errorf("%s: %v", env.Profile, err)
				}
				return strings.TrimSuffix(out.String(), "\n")
			})
		})
	}
}
