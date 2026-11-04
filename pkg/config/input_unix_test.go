//go:build linux || darwin

package config

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/ui/prompt"
	"github.com/abbott/hardn/pkg/ui/uitest"
)

// TestInterrupt checks that Ctrl-C at the first-run question ends hardn
// by SIGINT (reraise), which a shell reports with status 130 (brief Phase
// 3), and prints nothing more: SIGINT is sent to the test process every
// 10 ms while the question waits on a pipe that never ends.
// TestInterruptChild checks that the process then dies of the signal.
func TestInterrupt(t *testing.T) {
	// A SIGINT that lands outside the read's relay reaches the
	// Interrupter's guard rather than ending the test binary.
	sig := uitest.NewInterrupter(t)
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pr.Close(); _ = pw.Close() })
	useReader(t, prompt.New(pr, false))
	// The sender is stopped and waited for before the guard is stopped
	// (cleanups run last first), and each SIGINT it sent has been
	// delivered (sig.Send), so none is still on its way after the guard
	// stops, when it would kill the test binary.
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
				_, _ = pw.WriteString("not interrupted\n")
				return
			case <-tick.C:
				if err := sig.Send(); err != nil {
					t.Error(err)
					return
				}
			}
		}
	}()
	out, code, ended := ask(t, func() { ShouldCreateDefaultConfig() })
	if !ended || exited(code) != sigint {
		t.Errorf("ended %v with %d, want hardn ended by SIGINT", ended, code)
	}
	if want := "No configuration file found. Would you like to create one? [Y/n]\n"; out != want {
		t.Errorf("printed %q, want %q", out, want)
	}
}

// TestInterruptChild runs this test binary as a child process that waits
// at the first-run question on a pipe that never ends, with the
// production input, exit and reraise, and sends it SIGINT, as Ctrl-C
// does: the child must die of SIGINT, as origin/main's bufio reader did,
// so that a shell running hardn in a loop or a script stops, rather than
// exit with status 130, which a shell takes as a normal exit and goes on.
// A signal.Notify the child keeps, standing in for any other in hardn,
// catches a SIGINT sent before the read waits, and prompt.Reraise must
// undo it.
func TestInterruptChild(t *testing.T) {
	const question = "No configuration file found. Would you like to create one? [Y/n]\n"
	if os.Getenv("HARDN_CONFIG_CHILD") == "interrupt" {
		signal.Notify(make(chan os.Signal, 64), os.Interrupt)
		fmt.Println("ready")
		ShouldCreateDefaultConfig()
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
	cmd.Env = append(os.Environ(), "HARDN_CONFIG_CHILD=interrupt")
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
	if string(rest) != question {
		t.Errorf("child printed %q after ready, want the question alone", rest)
	}
}
