//go:build linux || darwin

package menu

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/ui/prompt"
)

// TestMainMenuInterrupt checks that Ctrl-C at the main menu's prompt, on
// its layout (nav.Run asking through prompt.Stdin's reader), ends hardn by
// SIGINT (reraise) once the main menu is drawn, after ending the prompt's
// line: SIGINT is sent to the test process every 10 ms while the read
// waits on a pipe that never ends, as a terminal does. origin/main dropped
// Ctrl-C at its menu prompts, read in raw mode (side-effects D6).
// TestInterruptChild checks that the process then dies of the signal.
func TestMainMenuInterrupt(t *testing.T) {
	guard := make(chan os.Signal, 64)
	signal.Notify(guard, os.Interrupt)
	t.Cleanup(func() { signal.Stop(guard) })
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pr.Close(); _ = pw.Close() })
	m := liveMenu(t, plainEnv(), fixtureStatus(new(int)))
	useReader(t, prompt.New(pr, false))
	stop, sent := make(chan struct{}), make(chan struct{})
	t.Cleanup(func() { close(stop); <-sent })
	go func() {
		defer close(sent)
		tick := time.NewTicker(10 * time.Millisecond)
		defer tick.Stop()
		giveUp := time.After(10 * time.Second)
		for {
			select {
			case <-stop:
				return
			case <-giveUp:
				_, _ = pw.WriteString("q\n")
				return
			case <-tick.C:
				_ = syscall.Kill(os.Getpid(), syscall.SIGINT)
			}
		}
	}()
	out, code, ended := session(t, func() { m.ShowMainMenu() })
	if !ended || exited(code) != sigint || !strings.HasSuffix(out, "Enter selection [0-10 or q]: \n") {
		t.Errorf("ended %v with %d, want hardn ended by SIGINT at the prompt, its line ended:\n%q", ended, code, out)
	}
}

// TestInterruptChild runs this test binary as a child process that
// waits at the main menu's prompt on a pipe that never ends, with the production
// input, exit and reraise, and sends it SIGINT, as Ctrl-C does: the
// child must end the prompt's line and die of SIGINT, so that a shell
// running hardn in a loop or a script stops, rather than exit with
// status 130, which a shell takes as a normal exit and goes on. A
// signal.Notify the child keeps, standing in for any other in hardn,
// catches a SIGINT sent before the read waits, and prompt.Reraise must
// undo it.
func TestInterruptChild(t *testing.T) {
	if os.Getenv("HARDN_MENU_CHILD") == "interrupt" {
		signal.Notify(make(chan os.Signal, 64), os.Interrupt)
		fmt.Println("ready")
		childMenu().ShowMainMenu()
		os.Exit(0)
	}
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pr.Close(); _ = pw.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestInterruptChild$")
	cmd.Env = append(os.Environ(), "HARDN_MENU_CHILD=interrupt")
	cmd.Stdin = pr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	out := bufio.NewReader(stdout)
	if line, err := out.ReadString('\n'); line != "ready\n" {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatalf("child: %q, %v; want ready", line, err)
	}
	stop := make(chan struct{})
	go func() {
		tick := time.NewTicker(10 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-stop:
				return
			case <-tick.C:
				_ = cmd.Process.Signal(os.Interrupt)
			}
		}
	}()
	rest, _ := io.ReadAll(out)
	err = cmd.Wait()
	close(stop)
	var ended *exec.ExitError
	if !errors.As(err, &ended) {
		t.Fatalf("child: %v; want it killed by SIGINT", err)
	}
	if ws, ok := ended.Sys().(syscall.WaitStatus); !ok || !ws.Signaled() || ws.Signal() != syscall.SIGINT {
		t.Errorf("child: %v (exit status %d); want it killed by SIGINT", err, ended.ExitCode())
	}
	if !strings.HasSuffix(string(rest), "Enter selection [0-10 or q]: \n") {
		t.Errorf("child printed %q after ready, want the main menu, its prompt's line ended", rest)
	}
}
