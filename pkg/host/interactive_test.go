package host

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// TestRunInteractiveCommand holds what starts, through the exec seam: the
// name resolved as Run resolves it, the argv with no shell, the working
// directory, hardn's own three files with no pipe, the environment and hardn's
// process group, with OwnSessions held too; the run log, which has the argv and
// the exit and not the environment.
func TestRunInteractiveCommand(t *testing.T) {
	dir := t.TempDir()
	less := touch(t, dir, "less", 0o755)
	s := newSeamed(t, Options{}, []string{dir}, nil)
	defer OwnSessions()()
	if err := s.RunInteractive(context.Background(), []string{"TERM=vt100", "LESSSECURE=1", "PATH=/tmp/x"}, "less", "--", "/var/log/hardn.log"); err != nil {
		t.Fatal(err)
	}
	if len(s.cmds) != 1 {
		t.Fatalf("%d commands", len(s.cmds))
	}
	got := s.cmds[0]
	if got.Path != less || !reflect.DeepEqual(got.Args, []string{"less", "--", "/var/log/hardn.log"}) {
		t.Errorf("Path %q, Args %q", got.Path, got.Args)
	}
	if want := []string{"TERM=vt100", "LESSSECURE=1", "PATH=" + SearchPath}; !reflect.DeepEqual(got.Env, want) {
		t.Errorf("Env = %q, want %q", got.Env, want)
	}
	if got.Dir != "/" || got.Stdin != os.Stdin || got.Stdout != os.Stdout || got.Stderr != os.Stderr {
		t.Errorf("Dir %q, Stdin %v, Stdout %v, Stderr %v", got.Dir, got.Stdin, got.Stdout, got.Stderr)
	}
	if got.SysProcAttr != nil {
		t.Errorf("SysProcAttr = %+v: the child must stay in hardn's session and group", got.SysProcAttr)
	}
	if want := "run 1: $ less -- /var/log/hardn.log\nrun 1: exit 0\n"; s.log.String() != want {
		t.Errorf("run log = %q, want %q", s.log.String(), want)
	}
}

// TestRunInteractiveRefuses: nothing runs for a name Run would not run or a
// done ctx.
func TestRunInteractiveRefuses(t *testing.T) {
	s := newSeamed(t, Options{}, []string{t.TempDir()}, nil)
	ctx := context.Background()
	for _, name := range []string{"", "./less", "bin/less", "less", "/nowhere/less"} {
		if err := s.RunInteractive(ctx, nil, name); !errors.Is(err, ErrNotFound) {
			t.Errorf("%q: err = %v, want ErrNotFound", name, err)
		}
	}
	dir := t.TempDir()
	touch(t, dir, "less", 0o755)
	s.dirs = []string{dir}
	done, cancel := context.WithCancel(ctx)
	cancel()
	if err := s.RunInteractive(done, nil, "less"); !errors.Is(err, context.Canceled) {
		t.Errorf("done: err = %v", err)
	}
	if len(s.cmds) != 0 {
		t.Errorf("ran %v", s.cmds)
	}
}

// TestRunInteractiveExit: a real child's exit status, as Run gives one.
func TestRunInteractiveExit(t *testing.T) {
	ctx := context.Background()
	r := NewReal(Options{})
	if err := r.RunInteractive(ctx, nil, "sh", "-c", "exit 0"); err != nil {
		t.Errorf("exit 0: %v", err)
	}
	var xe *ExitError
	if err := r.RunInteractive(ctx, nil, "sh", "-c", "exit 7"); !errors.As(err, &xe) || xe.ExitCode != 7 {
		t.Errorf("exit 7: %v", err)
	}
	if err := r.RunInteractive(ctx, nil, "sh", "-c", "kill -KILL $$"); !errors.As(err, &xe) || xe.ExitCode != -1 {
		t.Errorf("killed: %v", err)
	}
}

// TestRunInteractiveContext: ctx done while the child runs gives it SIGTERM,
// which it can act on, as less puts the terminal back; one that takes no heed
// is killed after the wait.
func TestRunInteractiveContext(t *testing.T) {
	const loop = `touch "$0"/ready; while :; do sleep 0.05; done`
	for _, tt := range []struct {
		name, script string
		termed       bool
	}{
		{"heeds SIGTERM", `trap 'touch "$0"/termed; exit 9' TERM; ` + loop, true},
		{"ignores SIGTERM", `trap '' TERM; ` + loop, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			r := NewReal(Options{})
			r.interactiveWait = 200 * time.Millisecond
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- r.RunInteractive(ctx, nil, "sh", "-c", tt.script, dir) }()
			deadline := time.Now().Add(10 * time.Second)
			for {
				if _, err := os.Stat(filepath.Join(dir, "ready")); err == nil {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("the child did not start")
				}
				time.Sleep(10 * time.Millisecond)
			}
			cancel()
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Errorf("err = %v, want context.Canceled", err)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("RunInteractive did not return")
			}
			if _, err := os.Stat(filepath.Join(dir, "termed")); (err == nil) != tt.termed {
				t.Errorf("termed: %v, want %t", err, tt.termed)
			}
		})
	}
}

// childEnvVar names the mode TestInteractiveChild runs in.
const childEnvVar = "HARDN_INTERACTIVE_CHILD"

// inChild is a test process that plays hardn, in the mode given.
func inChild(mode string, extra ...string) *exec.Cmd {
	cmd := exec.Command(os.Args[0], "-test.run=^TestInteractiveChild$")
	cmd.Env = append(append(os.Environ(), childEnvVar+"="+mode), extra...)
	return cmd
}

// TestRunInteractiveExactEnvironment: a real child's whole environment, as
// env(1) prints it on hardn's own stdout, whatever hardn's own holds.
func TestRunInteractiveExactEnvironment(t *testing.T) {
	cmd := inChild("env", "PAGER=evil", "LESS=-r", "LESSOPEN=|evil %s", "LESSCLOSE=evil", "LESSKEY=/tmp/k",
		"EDITOR=evil", "VISUAL=evil", "SHELL=/bin/evil", "HOME=/tmp/evil", "LD_LIBRARY_PATH=/tmp/evil", "TERM=evil", "LC_ALL=evil")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		t.Fatalf("child: %v, %q", err, stdout.String())
	}
	got := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	want := []string{"TERM=vt100", "LESSSECURE=1", "PATH=" + SearchPath}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("environment = %q, want %q", got, want)
	}
}

// onTerminal is a test process on a pseudo-terminal of its own, session
// leader and so the foreground process group, as hardn is on an admin's
// terminal: what is typed at master reaches it and its child as a terminal
// sends it.
type onTerminal struct {
	cmd    *exec.Cmd
	master *os.File
	mu     sync.Mutex
	output bytes.Buffer
}

func startOnTerminal(t *testing.T, mode string, extra ...string) *onTerminal {
	t.Helper()
	master, slave := openPTY(t)
	cmd := inChild(mode, extra...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = slave, slave, slave
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	o := &onTerminal{cmd: cmd, master: master}
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	go func() {
		b := make([]byte, 4096)
		for {
			n, err := master.Read(b)
			o.mu.Lock()
			o.output.Write(b[:n])
			o.mu.Unlock()
			if err != nil {
				return
			}
		}
	}()
	return o
}

// expect waits for s in the terminal's output since the last expect.
func (o *onTerminal) expect(t *testing.T, s string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		o.mu.Lock()
		all := o.output.String()
		if i := strings.Index(all, s); i >= 0 {
			o.output.Next(i + len(s))
			o.mu.Unlock()
			return
		}
		o.mu.Unlock()
		if time.Now().After(deadline) {
			t.Fatalf("no %q on the terminal, which holds %q", s, all)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (o *onTerminal) typed(t *testing.T, s string) {
	t.Helper()
	if _, err := o.master.WriteString(s); err != nil {
		t.Fatal(err)
	}
}

// wait returns how the process ended: its exit code, or the signal.
func (o *onTerminal) wait(t *testing.T) (code int, sig syscall.Signal) {
	t.Helper()
	done := make(chan struct{})
	go func() { _ = o.cmd.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		o.mu.Lock()
		defer o.mu.Unlock()
		t.Fatalf("the process did not end; the terminal holds %q", o.output.String())
	}
	ws, _ := o.cmd.ProcessState.Sys().(syscall.WaitStatus)
	if ws.Signaled() {
		return -1, ws.Signal()
	}
	return ws.ExitStatus(), 0
}

// TestRunInteractiveCtrlC: on a pseudo-terminal, Ctrl-C while the child runs
// reaches the child, which has the terminal, and does not end "hardn"; once it
// returns, a second Ctrl-C ends "hardn" by Go's default, as before.
func TestRunInteractiveCtrlC(t *testing.T) {
	o := startOnTerminal(t, "ctrl-c")
	o.expect(t, "TTY012 READY\r\n")
	o.typed(t, "\x03")
	o.expect(t, "CAUGHT")
	o.expect(t, "RETURNED 7\r\n")
	o.typed(t, "\x03")
	if code, sig := o.wait(t); sig != syscall.SIGINT {
		t.Errorf("after the child: code %d, signal %v; want it ended by SIGINT", code, sig)
	}
}

// TestRunInteractiveCtrlBackslash: Ctrl-\ likewise; once it returns, Go's
// default for SIGQUIT is back.
func TestRunInteractiveCtrlBackslash(t *testing.T) {
	o := startOnTerminal(t, "ctrl-quit")
	o.expect(t, "READY\r\n")
	o.typed(t, "\x1c")
	o.expect(t, "CAUGHT")
	o.expect(t, "RETURNED 8\r\n")
	o.typed(t, "\x1c")
	if code, sig := o.wait(t); code != 2 && sig != syscall.SIGQUIT {
		t.Errorf("after the child: code %d, signal %v; want Go's default for SIGQUIT", code, sig)
	}
}

// TestRunInteractiveKeepsRegistrations: a registration hardn holds around the
// call stays: it gets Ctrl-C, too, and once the call returns it still does,
// where a signal.Reset would have dropped it. SIGINT ignored at the call stays
// ignored, in the child too: a shell cannot trap it, so the child goes on to
// read its line.
func TestRunInteractiveKeepsRegistrations(t *testing.T) {
	t.Run("registered", func(t *testing.T) {
		o := startOnTerminal(t, "registered")
		o.expect(t, "READY\r\n")
		o.typed(t, "\x03")
		o.expect(t, "CAUGHT")
		o.expect(t, "RETURNED 7, GOT IT\r\n")
		o.expect(t, "STILL HELD")
		if code, sig := o.wait(t); code != 0 || sig != 0 {
			t.Errorf("code %d, signal %v", code, sig)
		}
	})
	t.Run("ignored", func(t *testing.T) {
		o := startOnTerminal(t, "ignored")
		o.expect(t, "READY\r\n")
		o.typed(t, "\x03")
		o.typed(t, "a line\n")
		o.expect(t, "READ a line")
		o.expect(t, "RETURNED 0, STILL IGNORED")
		if code, sig := o.wait(t); code != 0 || sig != 0 {
			t.Errorf("code %d, signal %v", code, sig)
		}
	})
}

// TestRunInteractiveTerminalModes: a child that ends with the terminal in raw
// mode, as one killed does, leaves it as it found it, so the next prompt reads
// cooked; and a real "line" typed after it arrives whole.
func TestRunInteractiveTerminalModes(t *testing.T) {
	o := startOnTerminal(t, "modes")
	o.expect(t, "RETURNED 0, MODES KEPT")
	o.typed(t, "typed\n")
	o.expect(t, "A LINE: typed")
	if code, sig := o.wait(t); code != 0 || sig != 0 {
		t.Errorf("code %d, signal %v", code, sig)
	}
}

// TestRunInteractiveLess runs the installed less on the pseudo-terminal with
// the environment logview gives it, "--" before the path: the file shows, a
// shell escape is refused, q ends it, exit 0. It is macOS's less 581.2 here,
// and GNU less where the tests run on Linux; written, not run there.
func TestRunInteractiveLess(t *testing.T) {
	if _, err := NewReal(Options{}).resolve("less"); err != nil {
		t.Skip("no less")
	}
	name := filepath.Join(t.TempDir(), "hardn.log")
	if err := os.WriteFile(name, []byte("first line\nsecond line\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	o := startOnTerminal(t, "less", "HARDN_INTERACTIVE_FILE="+name)
	o.expect(t, "second line")
	o.typed(t, "!")
	o.expect(t, "not available")
	o.typed(t, "\r")
	o.typed(t, "q")
	o.expect(t, "RETURNED 0")
	if code, sig := o.wait(t); code != 0 || sig != 0 {
		t.Errorf("code %d, signal %v", code, sig)
	}
}

// TestInteractiveChild is the process the tests above start, hardn's part in
// each. It does nothing as a test of its own.
func TestInteractiveChild(t *testing.T) {
	mode := os.Getenv(childEnvVar)
	if mode == "" {
		t.Skip("run by the tests above")
	}
	ctx := context.Background()
	r := NewReal(Options{})
	returned := func(err error) string {
		var xe *ExitError
		switch {
		case err == nil:
			return "RETURNED 0"
		case errors.As(err, &xe):
			return fmt.Sprintf("RETURNED %d", xe.ExitCode)
		}
		return "RETURNED " + err.Error()
	}
	// trapped prints READY and waits for the signal; its trap exits code.
	trapped := func(sig string, code int) []string {
		return []string{"-c", fmt.Sprintf(`trap 'echo CAUGHT; exit %d' %s; echo READY; while :; do sleep 0.02; done`, code, sig)}
	}
	switch mode {
	case "env":
		if err := r.RunInteractive(ctx, []string{"TERM=vt100", "LESSSECURE=1", "PATH=/tmp/evil"}, "env"); err != nil {
			os.Exit(3)
		}
	case "ctrl-c":
		fds := `for fd in 0 1 2; do test -t $fd && printf "$fd" || printf "no$fd"; done`
		err := r.RunInteractive(ctx, nil, "sh", "-c", `printf TTY; `+fds+`; trap 'echo CAUGHT; exit 7' INT; echo " READY"; while :; do sleep 0.02; done`)
		fmt.Println(returned(err))
		time.Sleep(20 * time.Second)
	case "ctrl-quit":
		err := r.RunInteractive(ctx, nil, "sh", trapped("QUIT", 8)...)
		fmt.Println(returned(err))
		time.Sleep(20 * time.Second)
	case "registered":
		held := make(chan os.Signal, 8)
		signal.Notify(held, os.Interrupt)
		err := r.RunInteractive(ctx, nil, "sh", trapped("INT", 7)...)
		got := "NOT"
		select {
		case <-held:
			got = "GOT IT"
		case <-time.After(5 * time.Second):
		}
		fmt.Printf("%s, %s\n", returned(err), got)
		_ = syscall.Kill(os.Getpid(), syscall.SIGINT)
		select {
		case <-held:
			fmt.Println("STILL HELD")
		case <-time.After(5 * time.Second):
			fmt.Println("LOST")
		}
	case "ignored":
		signal.Ignore(os.Interrupt)
		err := r.RunInteractive(ctx, nil, "sh", "-c", `echo READY; read line; echo "READ $line"`)
		fmt.Printf("%s, STILL %s\n", returned(err), map[bool]string{true: "IGNORED", false: "CAUGHT"}[signal.Ignored(os.Interrupt)])
	case "modes":
		before, err := unix.IoctlGetTermios(0, getTermios)
		if err != nil {
			os.Exit(3)
		}
		err = r.RunInteractive(ctx, nil, "stty", "raw", "-echo")
		after, terr := unix.IoctlGetTermios(0, getTermios)
		state := "MODES KEPT"
		if terr != nil || *after != *before {
			state = "MODES CHANGED"
		}
		fmt.Printf("%s, %s\n", returned(err), state)
		var line string
		if _, err := fmt.Scanln(&line); err != nil {
			os.Exit(4)
		}
		fmt.Println("A LINE: " + line)
	case "less":
		env := []string{"LESSSECURE=1", "LESSHISTFILE=-", "TERM=vt100", "LC_ALL=C.UTF-8"}
		fmt.Println(returned(r.RunInteractive(ctx, env, "less", "--", os.Getenv("HARDN_INTERACTIVE_FILE"))))
	default:
		os.Exit(5)
	}
	os.Exit(0)
}
