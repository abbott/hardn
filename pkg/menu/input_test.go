package menu

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"github.com/abbott/hardn/pkg/adapter/secondary"
	"github.com/abbott/hardn/pkg/application"
	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/domain/service"
	"github.com/abbott/hardn/pkg/interfaces"
	"github.com/abbott/hardn/pkg/ui/prompt"
)

// exited is what the stubbed exit panics with, in place of ending the
// test binary: the exit status hardn would have ended with, or sigint.
type exited int

// sigint is what the stubbed reraise panics with: hardn ended by SIGINT
// after Ctrl-C (prompt.Reraise), which a parent sees as a signal, not an
// exit status.
const sigint exited = -2

// useReader makes every legacy prompt read from r and stubs exit and
// reraise, until the test ends.
func useReader(t *testing.T, r *prompt.Reader) {
	t.Helper()
	prevInput, prevExit, prevReraise := input, exit, reraise
	input = func() *prompt.Reader { return r }
	exit = func(code int) { panic(exited(code)) }
	reraise = func() { panic(sigint) }
	t.Cleanup(func() { input, exit, reraise = prevInput, prevExit, prevReraise })
}

// scripted makes every legacy prompt read script, as from a pipe or a
// file: not a terminal, so nothing is flushed.
func scripted(t *testing.T, script string) {
	t.Helper()
	useReader(t, prompt.New(strings.NewReader(script), false))
}

// session runs fn as hardn runs a screen and returns what it printed, and
// the exit status it ended hardn with, if it did.
func session(t *testing.T, fn func()) (out string, code int, ended bool) {
	t.Helper()
	out = captureStdout(t, func() {
		defer func() {
			if r := recover(); r != nil {
				e, ok := r.(exited)
				if !ok {
					panic(r)
				}
				code, ended = int(e), true
			}
		}()
		fn()
	})
	return out, code, ended
}

// reads are the legacy input functions, each a line read now.
var reads = []struct {
	name string
	read func() string
}{
	{"ReadInput", ReadInput},
	{"ReadKey", ReadKey},
	{"ReadMenuInput", ReadMenuInput},
}

// TestReads checks what each legacy read returns for a line: trimmed of
// spaces, and a menu choice in lower case. Without a terminal no echoed
// Enter ended the prompt's line, so the read ends it.
func TestReads(t *testing.T) {
	tests := []struct {
		name   string
		read   func() string
		script string
		want   string
	}{
		{"a text answer", ReadInput, "alice\n", "alice"},
		{"spaces and CRLF trimmed", ReadInput, "  alice  \r\n", "alice"},
		{"an empty answer", ReadInput, "\n", ""},
		{"a key's line", ReadKey, "x\n", "x"},
		{"Enter alone", ReadKey, "\n", ""},
		{"a two-digit choice", ReadMenuInput, " 10 \n", "10"},
		{"q quits on Enter", ReadMenuInput, "q\n", "q"},
		{"Q reads as q", ReadMenuInput, "Q\n", "q"},
		{"a last line with no Enter", ReadMenuInput, "0", "0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scripted(t, tt.script)
			var got string
			out, code, ended := session(t, func() { got = tt.read() })
			if ended || got != tt.want {
				t.Errorf("read = %q (ended %v, %d); want %q", got, ended, code, tt.want)
			}
			if out != "\n" {
				t.Errorf("printed %q, want the prompt's line ended", out)
			}
		})
	}
	t.Run("typeahead is dropped on a terminal", func(t *testing.T) {
		// One read takes both lines into the Reader's buffer, as if the
		// second were typed while the first answer's step ran: the next
		// read flushes it and meets the end of the input. A
		// strings.Reader has no descriptor, so only the buffer is
		// flushed; prompt's tests flush a pseudo-terminal's queue.
		useReader(t, prompt.New(strings.NewReader("1\ntypeahead\n"), true))
		var first string
		out, code, ended := session(t, func() { first = ReadMenuInput(); ReadKey() })
		if first != "1" || !ended || code != prompt.ExitInterrupted {
			t.Errorf("read %q, then ended %v with %d; want \"1\", then the typeahead dropped and exit status %d", first, ended, code, prompt.ExitInterrupted)
		}
		if out != "\n" {
			t.Errorf("printed %q, want only the aborted prompt's line ended", out)
		}
	})
	t.Run("a line too long reads as empty", func(t *testing.T) {
		scripted(t, strings.Repeat("1", prompt.MaxLine+1)+"\n2\n")
		var first, second string
		_, _, ended := session(t, func() { first, second = ReadMenuInput(), ReadMenuInput() })
		if ended || first != "" || second != "2" {
			t.Errorf("reads = %q, %q (ended %v); want \"\", \"2\"", first, second, ended)
		}
	})
}

// TestPastedAnswers checks that answers pasted at once into back-to-back
// free-text questions each reach their own, on a terminal Reader as with
// scripted input: ReadInput keeps typeahead, as origin/main's one bufio
// reader did and pkg/config's questions do. In each case one read takes
// every line into the Reader's buffer, as a paste arrives. A menu choice
// and a pause, which follow long steps, still discard it on a terminal,
// and not with scripted input. The firewall screen's four profile
// questions take their answers so; the fourth, a port with no protocol,
// stops the screen before it saves the configuration.
func TestPastedAnswers(t *testing.T) {
	for _, tty := range []bool{true, false} {
		t.Run(fmt.Sprintf("ReadInput keeps them, terminal %v", tty), func(t *testing.T) {
			useReader(t, prompt.New(strings.NewReader("alice\nAlice Example\n2230\n"), tty))
			var got []string
			_, code, ended := session(t, func() {
				for range 3 {
					got = append(got, ReadInput())
				}
			})
			if want := []string{"alice", "Alice Example", "2230"}; ended || strings.Join(got, "|") != strings.Join(want, "|") {
				t.Errorf("reads = %q (ended %v, %d); want %q", got, ended, code, want)
			}
		})
	}
	for _, r := range reads[1:] {
		for _, tty := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s after ReadInput, terminal %v", r.name, tty), func(t *testing.T) {
				useReader(t, prompt.New(strings.NewReader("alice\n1\n"), tty))
				var first, second string
				_, code, ended := session(t, func() { first = ReadInput(); second = r.read() })
				switch {
				case first != "alice":
					t.Errorf("ReadInput = %q, want alice", first)
				case tty && (!ended || code != prompt.ExitInterrupted):
					t.Errorf("%s = %q (ended %v, %d); want the typeahead dropped, then exit status %d", r.name, second, ended, code, prompt.ExitInterrupted)
				case !tty && (ended || second != "1"):
					t.Errorf("%s = %q (ended %v, %d); want the scripted line 1", r.name, second, ended, code)
				}
			})
		}
	}
	t.Run("the firewall profile's questions", func(t *testing.T) {
		useReader(t, prompt.New(strings.NewReader("Example\nExample Service\nAn example\n8080\n"), true))
		cfg := config.DefaultConfig()
		out, code, ended := session(t, func() { NewFirewallMenu(nil, cfg, nil).addAppProfile() })
		if ended {
			t.Fatalf("ended hardn with %d:\n%s", code, out)
		}
		for _, w := range []string{"Enter profile name", "Enter profile title", "Enter profile description", "Enter ports", "Invalid port format '8080'"} {
			if !strings.Contains(out, w) {
				t.Errorf("output lacks %q:\n%s", w, out)
			}
		}
		if len(cfg.UfwAppProfiles) != 0 {
			t.Errorf("profiles = %v, want none added", cfg.UfwAppProfiles)
		}
	})
}

// TestEnterRequired checks that no read returns before Enter: origin/main
// took a menu's q, and any key at a pause, without one (brief §2).
func TestEnterRequired(t *testing.T) {
	for _, r := range []struct {
		name string
		read func() string
		key  string
	}{
		{"ReadMenuInput", ReadMenuInput, "q"},
		{"ReadKey", ReadKey, "x"},
	} {
		t.Run(r.name, func(t *testing.T) {
			pr, pw, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = pr.Close(); _ = pw.Close() })
			useReader(t, prompt.New(pr, false))
			out, _, _ := session(t, func() {
				done := make(chan string, 1)
				go func() { done <- r.read() }()
				_, _ = pw.WriteString(r.key)
				select {
				case got := <-done:
					t.Errorf("%s returned %q before Enter", r.name, got)
					return
				case <-time.After(200 * time.Millisecond):
				}
				_, _ = pw.WriteString("\n")
				select {
				case got := <-done:
					if got != r.key {
						t.Errorf("%s = %q, want %q", r.name, got, r.key)
					}
				case <-time.After(10 * time.Second):
					t.Errorf("%s did not return on Enter", r.name)
				}
			})
			if out != "\n" {
				t.Errorf("printed %q, want the prompt's line ended", out)
			}
		})
	}
}

// TestAbort checks that the end of the input at any legacy read ends
// hardn with exit status 130, after ending the prompt's line, where
// origin/main's menus spun forever (side-effects D6), and that a read
// error ends it with status 1.
func TestAbort(t *testing.T) {
	for _, r := range reads {
		t.Run(r.name, func(t *testing.T) {
			scripted(t, "")
			out, code, ended := session(t, func() { r.read() })
			if !ended || code != prompt.ExitInterrupted {
				t.Errorf("ended %v with %d, want exit status %d", ended, code, prompt.ExitInterrupted)
			}
			if out != "\n" {
				t.Errorf("printed %q, want the prompt's line ended", out)
			}
		})
	}
	t.Run("a read error", func(t *testing.T) {
		useReader(t, prompt.New(iotest.ErrReader(errors.New("input/output error")), false))
		out, code, ended := session(t, func() { ReadMenuInput() })
		if !ended || code != 1 {
			t.Errorf("ended %v with %d, want exit status 1", ended, code)
		}
		if want := "\nprompt: read: input/output error\n"; out != want {
			t.Errorf("printed %q, want %q", out, want)
		}
	})
}

// newEnvironmentMenu builds the Environment screen on the repo's mock
// file system and commander: nothing is read from or run on this host.
func newEnvironmentMenu(t *testing.T) *EnvironmentSettingsMenu {
	t.Helper()
	t.Setenv("HARDN_CONFIG", "")
	t.Setenv("SUDO_USER", "")
	t.Setenv("USER", "alice")
	repo := secondary.NewFileEnvironmentRepository(interfaces.NewMockFileSystem(), interfaces.NewMockCommander())
	env := application.NewEnvironmentManager(service.NewEnvironmentServiceImpl(repo))
	mm := application.NewMenuManager(nil, nil, nil, nil, nil, nil, nil, env, nil, nil)
	return NewEnvironmentSettingsMenu(mm, config.DefaultConfig())
}

// TestScreens drives legacy screens with scripted stdin: each pause waits
// for one line, says "Press Enter", and leaves the next line to the next
// prompt. (The main menu's invalid choice paused too until slice 2b's unit
// (c), which refuses it under the prompt: TestMainMenuKeys.)
func TestScreens(t *testing.T) {
	const pause = "Press Enter to continue..."
	tests := []struct {
		name   string
		script string
		run    func(t *testing.T)
		want   []string // in the output, in order
		pauses int
	}{
		{
			name: "help", script: "\n",
			run:    func(*testing.T) { NewHelpMenu().Show() },
			want:   []string{"Command Line Usage:", "Press Enter to return to the main menu..."},
			pauses: 0,
		},
		{
			name: "environment: guide, an invalid choice, q", script: "2\n\nx\n\nq\n",
			run:    func(t *testing.T) { newEnvironmentMenu(t).Show() },
			want:   []string{"Environment Variable Settings", "Environment Variables Guide", pause, "Invalid option. Please try again.", pause},
			pauses: 2,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scripted(t, tt.script+"next\n")
			out, code, ended := session(t, func() { tt.run(t) })
			if ended {
				t.Fatalf("ended hardn with %d:\n%s", code, out)
			}
			rest := out
			for _, w := range tt.want {
				_, after, ok := strings.Cut(rest, w)
				if !ok {
					t.Fatalf("output lacks %q after the text before it:\n%s", w, out)
				}
				rest = after
			}
			if n := strings.Count(out, pause); n != tt.pauses {
				t.Errorf("%d continue pauses, want %d", n, tt.pauses)
			}
			if strings.Contains(out, "any key") {
				t.Errorf("output still says \"any key\":\n%s", out)
			}
			var next string
			if _, _, ended := session(t, func() { next = ReadInput() }); ended || next != "next" {
				t.Errorf("the line after the screen's = %q (ended %v): the screen read too few or too many lines", next, ended)
			}
		})
	}
	t.Run("the end of the input at a pause ends hardn there", func(t *testing.T) {
		scripted(t, "2\n")
		m := newEnvironmentMenu(t)
		out, code, ended := session(t, func() { m.Show() })
		if !ended || code != prompt.ExitInterrupted {
			t.Fatalf("ended %v with %d, want exit status %d", ended, code, prompt.ExitInterrupted)
		}
		if !strings.HasSuffix(out, pause+"\n") || strings.Count(out, "Environment Variable Settings") != 1 {
			t.Errorf("want the guide's pause last, and the menu not drawn again:\n%s", out)
		}
	})
}

// TestSharedStdin runs this test binary as a child process whose stdin
// is a pipe holding two answers, so that the first read takes both into
// its buffer: the config prompt's, then a menu choice's. Both read
// through prompt.Stdin, so the menu gets its line; origin/main's two
// bufio readers lost it to the first (plan risk R17, side-effects D5).
// The child then ends at the end of the input, by os.Exit with status
// 130.
func TestSharedStdin(t *testing.T) {
	if os.Getenv("HARDN_MENU_CHILD") == "shared" {
		create := config.ShouldCreateDefaultConfig()
		choice := ReadMenuInput()
		fmt.Printf("child: %v %q\n", create, choice)
		ReadKey()
		os.Exit(0)
	}
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pr.Close() })
	if _, err := pw.WriteString("n\n1\n"); err != nil {
		t.Fatal(err)
	}
	_ = pw.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestSharedStdin$")
	cmd.Env = append(os.Environ(), "HARDN_MENU_CHILD=shared")
	cmd.Stdin = pr
	out, err := cmd.Output()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != prompt.ExitInterrupted {
		t.Errorf("child: %v; want exit status %d", err, prompt.ExitInterrupted)
	}
	if want := `child: false "1"`; !strings.Contains(string(out), want) {
		t.Errorf("child printed:\n%s\nwant %s", out, want)
	}
}
