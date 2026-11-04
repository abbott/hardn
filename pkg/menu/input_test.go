package menu

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/config"
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

// TestSharedStdin runs this test binary as a child process whose stdin
// is a pipe holding two answers, so that the first read takes both into
// its buffer: the config prompt's, then the main menu's q. Both read
// through prompt.Stdin, so the main menu gets its line and quits;
// origin/main's two bufio readers lost it to the first (plan risk R17,
// side-effects D5), and the main menu would meet the end of the input,
// exit status 130.
func TestSharedStdin(t *testing.T) {
	if os.Getenv("HARDN_MENU_CHILD") == "shared" {
		create := config.ShouldCreateDefaultConfig()
		fmt.Printf("child: %v\n", create)
		childMenu().ShowMainMenu()
		fmt.Println("child: quit")
		os.Exit(0)
	}
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pr.Close() })
	if _, err := pw.WriteString("n\nq\n"); err != nil {
		t.Fatal(err)
	}
	_ = pw.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestSharedStdin$")
	cmd.Env = append(os.Environ(), "HARDN_MENU_CHILD=shared")
	cmd.Stdin = pr
	out, err := cmd.Output()
	if err != nil {
		t.Errorf("child: %v; want exit status 0", err)
	}
	if want := "child: false\n"; !strings.Contains(string(out), want) || !strings.HasSuffix(string(out), "child: quit\n") {
		t.Errorf("child printed:\n%s\nwant %q, then the main menu quit", out, want)
	}
}

// childMenu is the main menu as a child process runs it, with the
// production input, exit and reraise: Figma's fixture as its status, drawn
// as off a terminal.
func childMenu() *MainMenu {
	m := NewMainMenu(config.DefaultConfig())
	m.SetStatus(fixtureStatus(new(int)))
	m.SetTerminal(plainEnv)
	return m
}

// captureStdout returns what fn writes to os.Stdout.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdout
	os.Stdout = w
	done := make(chan string)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	defer func() { os.Stdout = orig }()
	fn()
	os.Stdout = orig
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return <-done
}
