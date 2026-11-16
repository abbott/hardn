package config

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/ui/prompt"
)

// TestCreateDefaultConfigCheckReleases: the release question is the last
// of the hardn.yml questions, its default yes, and the file written holds
// checkReleases either way, with no question too: Enter writes true, the
// end of the input false.
func TestCreateDefaultConfigCheckReleases(t *testing.T) {
	prevInteractive, prevExample := isInteractive, ensureExample
	ensureExample = func() error { return errors.New("test: /etc/hardn not written") }
	t.Cleanup(func() { isInteractive, ensureExample = prevInteractive, prevExample })
	for _, tc := range []struct {
		name, answers string
		asked, want   bool
		said          string
	}{
		{"Enter", "\n\n\n\n\n\n\n", true, true, ""},
		{"y", "\n\n\n\n\n\ny\n", true, true, ""},
		{"n", "\n\n\n\n\n\nn\n", true, false, ""},
		{"no", "\n\n\n\n\n\nno\n", true, false, ""},
		{"neither", "\n\n\n\n\n\nmaybe\n", true, false, "Taken as no.\n"},
		{"the end of the input", "\n\n\n\n", true, false, ""},
		{"the end of the input at the question", "\n\n\n\n\n\n", true, false, ""},
		{"no question", "", false, false, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isInteractive = func() bool { return tc.asked }
			useReader(t, prompt.New(strings.NewReader(tc.answers), true))
			path := filepath.Join(t.TempDir(), "hardn.yml")
			var err error
			out, _, ended := ask(t, func() { err = CreateDefaultConfig(path, DefaultConfig()) })
			if err != nil || ended {
				t.Fatalf("CreateDefaultConfig: %v (ended %v)", err, ended)
			}
			if tc.asked {
				if want := "Add SSH public key (optional, press Enter to skip): Enter NTP server (optional, press Enter to skip): " +
					"Enter syslog server (optional, press Enter to skip): " + ReleaseQuestion() + " " + tc.said + "Created configuration file at "; !strings.Contains(out, want) {
					t.Errorf("the question is not the last:\n%s", out)
				}
			} else if strings.Contains(out, "releases") {
				t.Errorf("asked with no terminal:\n%s", out)
			}
			b, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			word := map[bool]string{true: "true", false: "false"}[tc.want]
			if !strings.Contains("\n"+string(b), "\ncheckReleases: "+word+"\n") {
				t.Errorf("no checkReleases: %s in the file:\n%s", word, b)
			}
			if c, err := LoadConfig(path); err != nil || c.CheckReleases != tc.want || !c.FileSets("checkReleases") {
				t.Errorf("read back: %v, %+v", err, c)
			}
		})
	}
}

// TestAskCheckReleases: the question alone, on the prompt kit: its words,
// Enter, y or yes a yes, n or no a no, anything else a no said; the end of
// the input is no answer; Ctrl-C ends hardn.
func TestAskCheckReleases(t *testing.T) {
	if got := ReleaseQuestion(); got != "Check for new hardn releases, once a day? [Y/n]:" {
		t.Errorf("the question: %q", got)
	}
	for _, tc := range []struct {
		script        string
		yes, answered bool
		said          string
	}{
		{"\n", true, true, ""},
		{"y\n", true, true, ""},
		{" YES \r\n", true, true, ""},
		{"n\n", false, true, ""},
		{"No\n", false, true, ""},
		{"nope\n", false, true, "Taken as no.\n"},
		{"", false, false, ""},
	} {
		useReader(t, prompt.New(strings.NewReader(tc.script), true))
		var yes, answered bool
		out, _, ended := ask(t, func() { yes, answered = AskCheckReleases() })
		if ended || yes != tc.yes || answered != tc.answered || !strings.HasPrefix(out, ReleaseQuestion()+" ") || !strings.HasSuffix(out, tc.said) || tc.said == "" && strings.Contains(out, "Taken") {
			t.Errorf("%q: yes %t, answered %t (ended %v); printed %q", tc.script, yes, answered, ended, out)
		}
	}
	useReader(t, prompt.New(interruptedInput{}, true))
	if _, code, ended := ask(t, func() { AskCheckReleases() }); !ended || code != int(sigint) {
		t.Errorf("Ctrl-C: ended %v with %d, want SIGINT", ended, code)
	}
}

// interruptedInput is an input whose read ends by Ctrl-C.
type interruptedInput struct{}

func (interruptedInput) Read([]byte) (int, error) { return 0, prompt.ErrInterrupted }

// TestEditKeys: EditKeys writes a named setting the file gives no value,
// its default too, in place, with every other byte kept; one the file sets
// is edited as Edit edits it, and left alone when it is already the value.
func TestEditKeys(t *testing.T) {
	const head = "# hardn's settings\nusername: alice # the admin\n\n# SSH\nsshPort: 2230\n"
	for _, tc := range []struct {
		name, in string
		on       bool
		want     string
	}{
		{"no key, false", head, false, head + "checkReleases: false\n"},
		{"no key, true", head, true, head + "checkReleases: true\n"},
		{"no key, no last line break", "sshPort: 2230", false, "sshPort: 2230\ncheckReleases: false"},
		{"a null key", "checkReleases: # todo\nsshPort: 2230\n", false, "checkReleases: false # todo\nsshPort: 2230\n"},
		{"set false, false", "checkReleases: false # off\n", false, "checkReleases: false # off\n"},
		{"set false, true", "checkReleases: false # off\n", true, "checkReleases: true # off\n"},
		{"comments alone", "# nothing yet\n", false, "# nothing yet\ncheckReleases: false\n"},
		{"an empty file", "", true, "checkReleases: true\n"},
	} {
		c := DefaultConfig()
		if err := yaml.Unmarshal([]byte(tc.in), c); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		c.CheckReleases = tc.on
		c.SshPort = 9 // not named: not written
		got, err := EditKeys([]byte(tc.in), c, "checkReleases")
		if err != nil || string(got) != tc.want {
			t.Errorf("%s: %v\n%q\nwant\n%q", tc.name, err, got, tc.want)
		}
		// Edit never writes the default.
		if plain, err := Edit([]byte(tc.in), c, "checkReleases"); err != nil || !tc.on && string(plain) != tc.in {
			t.Errorf("%s: Edit: %v, %q", tc.name, err, plain)
		}
	}
	if _, err := EditKeys([]byte(head), DefaultConfig()); !errors.Is(err, fs.ErrInvalid) {
		t.Errorf("no setting named: %v", err)
	}
	var e *EditError
	if _, err := EditKeys([]byte("base: &b\n  x: 1\n<<: *b\n"), DefaultConfig(), "checkReleases"); !errors.As(err, &e) || !errors.Is(err, ErrNotEditable) {
		t.Errorf("a file that merges: %v", err)
	}
}

// TestSaveKeys: SaveKeys writes the key to the file the configuration was
// read from, which then sets it (FileSets), and keeps Save's refusals.
func TestSaveKeys(t *testing.T) {
	ctx := context.Background()
	const path, was = "/etc/hardn/hardn.yml", "# mine\nsshPort: 2230\n"
	f := hostfake.New(host.Options{})
	f.AddDir("/etc/hardn", 0o755, 0, 0)
	f.AddFile(path, []byte(was), 0o640, 0, 0)
	c, err := Read(ctx, f, path)
	if err != nil || c.FileSets("checkReleases") {
		t.Fatalf("Read: %v, sets the key %t", err, c.FileSets("checkReleases"))
	}
	if err := SaveKeys(ctx, f, c, "checkReleases"); err != nil {
		t.Fatal(err)
	}
	e, _ := f.Entry(path)
	if string(e.Data) != was+"checkReleases: false\n" || e.Mode.Perm() != 0o640 || !c.FileSets("checkReleases") || !c.LoadedFrom(e.Data) {
		t.Errorf("saved %q, mode %v, sets the key %t", e.Data, e.Mode.Perm(), c.FileSets("checkReleases"))
	}
	f.AddFile(path, []byte(was+"# changed\n"), 0o640, 0, 0)
	if err := SaveKeys(ctx, f, c, "checkReleases"); !errors.Is(err, ErrChanged) {
		t.Errorf("a file changed since it was read: %v", err)
	}
}
