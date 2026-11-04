package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/screens/mainmenu"
	"github.com/abbott/hardn/pkg/ui/prompt"
	"github.com/abbott/hardn/pkg/ui/screen"
	"github.com/abbott/hardn/pkg/ui/term"
)

// childMenu is the main menu as a child process runs it, with the
// production input (prompt.Stdin), exit and reraise: Figma's fixture as
// its status, drawn as off a terminal, no item's screen set.
func childMenu() mainmenu.Options {
	return mainmenu.Options{
		Config:   config.DefaultConfig(),
		Status:   func(context.Context) (screen.MainMenu, error) { return screen.MainMenuFixture(), nil },
		Terminal: func() term.Env { return term.Resolve(term.Options{}, term.Facts{Width: 80}) },
		Input:    prompt.Stdin,
		Out:      os.Stdout,
	}
}

// TestRunMenuEnds: runMenu ends hardn as the main menu's error says: Exit
// or q ends nothing; the end of the input exits 130; Ctrl-C at the prompt
// ends hardn by SIGINT (reraise); any other error is printed on stdout and
// exits 1.
func TestRunMenuEnds(t *testing.T) {
	failed := errors.New("main menu: status: broken")
	for _, tc := range []struct {
		name   string
		input  string
		err    error // the status's
		code   int   // -1: not ended
		signal bool
	}{
		{name: "q", input: "q\n", code: -1},
		{name: "0", input: "0\n", code: -1},
		{name: "the end of the input", input: "", code: prompt.ExitInterrupted},
		{name: "ctrl-c", err: fmt.Errorf("status: %w", context.Canceled), signal: true, code: -1},
		{name: "another error", err: failed, code: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			exitWas, reraiseWas := exit, reraise
			t.Cleanup(func() { exit, reraise = exitWas, reraiseWas })
			code, signalled := -1, false
			exit = func(c int) { code = c }
			reraise = func() { signalled = true }
			o := childMenu()
			in := prompt.New(strings.NewReader(tc.input), false)
			o.Input = func() *prompt.Reader { return in }
			var out bytes.Buffer
			o.Out = &out
			if tc.err != nil {
				o.Status = func(context.Context) (screen.MainMenu, error) { return screen.MainMenu{}, tc.err }
			}
			said := captureStdout(t, func() { runMenu(context.Background(), o) })
			if signalled != tc.signal || (!tc.signal && code != tc.code) {
				t.Errorf("exit %d, signalled %v; want %d, %v", code, signalled, tc.code, tc.signal)
			}
			if tc.code == 1 && !strings.Contains(said, failed.Error()) {
				t.Errorf("the error is not printed: %q", said)
			}
		})
	}
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
		var b bytes.Buffer
		_, _ = b.ReadFrom(r)
		done <- b.String()
	}()
	defer func() { os.Stdout = orig }()
	fn()
	os.Stdout = orig
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return <-done
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
		runMenu(context.Background(), childMenu())
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
