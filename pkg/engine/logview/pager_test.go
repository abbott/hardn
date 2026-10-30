package logview_test

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/engine/logview"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/hostfake"
)

// The first line of less --version on the VMs, all the log holds of it:
// _support/hardn-audit-2026-10-16/vm-runs/s1/01-tools.log.
const (
	gnuVersion  = "less 668 (GNU regular expressions)\n"
	busyVersion = "less: unrecognized option: version\n"
)

// TestPager holds exactly what would run, per host shape, and that anything
// but a positive answer is no pager.
func TestPager(t *testing.T) {
	want := logview.Command{
		Argv: []string{"less", "--", "/var/log/hardn.log"},
		Env:  []string{"LESSSECURE=1", "LESSHISTFILE=-", "TERM=xterm-256color", "LC_ALL=C.UTF-8"},
	}
	installed := func(f *hostfake.System) { f.AddFile("/usr/bin/less", []byte("ELF"), 0o755, 0, 0) }
	busybox := func(f *hostfake.System) {
		f.AddFile("/bin/busybox", []byte("ELF"), 0o755, 0, 0)
		f.AddSymlink("/usr/bin/less", "/bin/busybox")
	}
	scripted := func(res hostfake.Response) func(f *hostfake.System) {
		return func(f *hostfake.System) { f.Respond(res, "less", "--version") }
	}
	for _, tt := range []struct {
		name string
		host []func(*hostfake.System)
		want bool
	}{
		{"Debian 13 and Ubuntu 26.04: GNU less 668", []func(*hostfake.System){installed, scripted(hostfake.Response{Stdout: gnuVersion})}, true},
		{"Alpine 3.23's less package [I, not run]: the same check", []func(*hostfake.System){installed, scripted(hostfake.Response{Stdout: "less 643 (PCRE2 regular expressions)\n"})}, true},
		{"the version on stderr, as the VMs' log merges them", []func(*hostfake.System){installed, scripted(hostfake.Response{Stderr: gnuVersion})}, true},
		{"the version alone", []func(*hostfake.System){installed, scripted(hostfake.Response{Stdout: "less 668\n"})}, true},
		{"less in another directory of the search path", []func(*hostfake.System){func(f *hostfake.System) { f.AddFile("/bin/less", []byte("ELF"), 0o755, 0, 0) }, scripted(hostfake.Response{Stdout: gnuVersion})}, true},
		{"Alpine 3.23: BusyBox's, through its link, exit 1", []func(*hostfake.System){busybox, scripted(hostfake.Response{Stderr: busyVersion, ExitCode: 1})}, false},
		{"BusyBox's, exit 0", []func(*hostfake.System){busybox, scripted(hostfake.Response{Stderr: busyVersion})}, false},
		{"no less", nil, false},
		{"no less, though the command would answer", []func(*hostfake.System){scripted(hostfake.Response{Stdout: gnuVersion})}, false},
		{"not executable", []func(*hostfake.System){func(f *hostfake.System) { f.AddFile("/usr/bin/less", []byte("ELF"), 0o644, 0, 0) }, scripted(hostfake.Response{Stdout: gnuVersion})}, false},
		{"a directory", []func(*hostfake.System){func(f *hostfake.System) { f.AddDir("/usr/bin/less", 0o755, 0, 0) }, scripted(hostfake.Response{Stdout: gnuVersion})}, false},
		{"a dangling link", []func(*hostfake.System){func(f *hostfake.System) { f.AddSymlink("/usr/bin/less", "/nowhere") }, scripted(hostfake.Response{Stdout: gnuVersion})}, false},
		{"exit 1 with the right words", []func(*hostfake.System){installed, scripted(hostfake.Response{Stdout: gnuVersion, ExitCode: 1})}, false},
		{"could not run", []func(*hostfake.System){installed, scripted(hostfake.Response{Err: errors.New("exec format error")})}, false},
		{"not scripted", []func(*hostfake.System){installed}, false},
		{"no output", []func(*hostfake.System){installed, scripted(hostfake.Response{})}, false},
		{"another program", []func(*hostfake.System){installed, scripted(hostfake.Response{Stdout: "most 5.2.0\n"})}, false},
		{"less with no number", []func(*hostfake.System){installed, scripted(hostfake.Response{Stdout: "less version 668\n"})}, false},
		{"less and a word", []func(*hostfake.System){installed, scripted(hostfake.Response{Stdout: "less 668x (beta)\n"})}, false},
		{"not the first line", []func(*hostfake.System){installed, scripted(hostfake.Response{Stdout: "a notice\nless 668\n"})}, false},
		{"not at the start", []func(*hostfake.System){installed, scripted(hostfake.Response{Stdout: " less 668\n"})}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := hostfake.New(host.Options{})
			for _, add := range tt.host {
				add(f)
			}
			got, ok := logview.Pager(context.Background(), f, "xterm-256color")
			if ok != tt.want {
				t.Fatalf("Pager = %+v, %t, want %t", got, ok, tt.want)
			}
			if !ok {
				if !reflect.DeepEqual(got, logview.Command{}) {
					t.Errorf("no pager, yet %+v", got)
				}
				return
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("Pager = %+v\nwant    %+v", got, want)
			}
		})
	}
}

// TestPagerDoneContext: a done ctx is no pager.
func TestPagerDoneContext(t *testing.T) {
	f := hostfake.New(host.Options{})
	f.AddFile("/usr/bin/less", []byte("ELF"), 0o755, 0, 0)
	f.Respond(hostfake.Response{Stdout: gnuVersion}, "less", "--version")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got, ok := logview.Pager(ctx, f, "vt100"); ok {
		t.Errorf("Pager = %+v", got)
	}
}

// deadlined records the deadline less --version runs under, and when hang is set
// never ends, whatever ctx says, as a process stuck in the kernel on a hung file
// system: host.Real's Run could not return.
type deadlined struct {
	*hostfake.System
	left    time.Duration
	hang    bool
	release chan struct{}
}

func (d *deadlined) Run(ctx context.Context, name string, args ...string) (host.Result, error) {
	if at, ok := ctx.Deadline(); ok {
		d.left = time.Until(at)
	}
	if d.hang {
		<-d.release
		return host.Result{}, ctx.Err()
	}
	return d.System.Run(ctx, name, args...)
}

// TestPagerBound: less --version runs under 5 s, and one that will not end is
// left behind past its grace: a timeout is no pager, and the draw goes on.
func TestPagerBound(t *testing.T) {
	f := hostfake.New(host.Options{})
	f.AddFile("/usr/bin/less", []byte("ELF"), 0o755, 0, 0)
	f.Respond(hostfake.Response{Stdout: gnuVersion}, "less", "--version")
	d := &deadlined{System: f}
	if _, ok := logview.Pager(context.Background(), d, "vt100"); !ok {
		t.Fatal("no pager")
	}
	if d.left <= 4*time.Second || d.left > 5*time.Second {
		t.Errorf("ran with %v left", d.left)
	}
	d = &deadlined{System: f, hang: true, release: make(chan struct{})}
	defer close(d.release)
	done := make(chan bool, 1)
	go func() {
		_, ok := logview.PagerBounded(context.Background(), d, "vt100", 20*time.Millisecond, 10*time.Millisecond)
		done <- ok
	}()
	select {
	case ok := <-done:
		if ok {
			t.Error("a pager, from a check that did not end")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Pager did not return")
	}
}

// TestCommandRun holds the whole environment that reaches less, on the fake:
// the command's and Run's PATH alone. Nothing else is asked of the host than
// the check.
func TestCommandRun(t *testing.T) {
	ctx := context.Background()
	f := hostfake.New(host.Options{})
	f.AddFile("/usr/bin/less", []byte("ELF"), 0o755, 0, 0)
	f.Respond(hostfake.Response{Stdout: gnuVersion}, "less", "--version")
	f.Respond(hostfake.Response{}, "less", "--", "/var/log/hardn.log")
	cmd, ok := logview.Pager(ctx, f, "tmux-256color")
	if !ok {
		t.Fatal("no pager")
	}
	if runs := f.Runs(); !reflect.DeepEqual(runs, [][]string{{"less", "--version"}}) {
		t.Errorf("Runs = %q", runs)
	}
	if err := cmd.Run(ctx, f); err != nil {
		t.Fatal(err)
	}
	calls := f.Calls()
	last := calls[len(calls)-1]
	if last.Op != "run-interactive" || !reflect.DeepEqual(last.Argv, []string{"less", "--", "/var/log/hardn.log"}) {
		t.Errorf("call = %v", last)
	}
	wantEnv := []string{"LESSSECURE=1", "LESSHISTFILE=-", "TERM=tmux-256color", "LC_ALL=C.UTF-8", "PATH=" + host.SearchPath}
	if !reflect.DeepEqual(last.Env, wantEnv) {
		t.Errorf("Env = %q, want %q", last.Env, wantEnv)
	}
	for _, c := range calls {
		switch c.Op {
		case "stat", "run", "run-interactive":
		default:
			t.Errorf("call %v", c)
		}
	}
}

// TestPagerCheckIsACommand: the check is a command, so a host with a run log
// logs it, which is why a draw reads on a host with none, as the status host
// is (cmd/hardn/status.go:29).
func TestPagerCheckIsACommand(t *testing.T) {
	var log bytes.Buffer
	f := hostfake.New(host.Options{RunLog: &log})
	f.AddFile("/usr/bin/less", []byte("ELF"), 0o755, 0, 0)
	f.Respond(hostfake.Response{Stdout: gnuVersion}, "less", "--version")
	if _, ok := logview.Pager(context.Background(), f, "vt100"); !ok {
		t.Fatal("no pager")
	}
	if !strings.Contains(log.String(), "$ less --version") {
		t.Errorf("run log: %q", log.String())
	}
}

// TestSafeTerm: TERM comes from whoever started a root process: anything but a
// terminal's name is the fallback, vt100, as the VMs' less ran under.
func TestSafeTerm(t *testing.T) {
	for in, want := range map[string]string{
		"xterm-256color":        "xterm-256color",
		"screen.xterm-256color": "screen.xterm-256color",
		"tmux-256color":         "tmux-256color",
		"rxvt-unicode-256color": "rxvt-unicode-256color",
		"xterm-kitty":           "xterm-kitty",
		"vt100":                 "vt100",
		"linux":                 "linux",
		"dumb":                  "dumb",
		"foot+base":             "foot+base",
		"":                      "vt100",
		" ":                     "vt100",
		"../../tmp/evil":        "vt100",
		"/tmp/evil":             "vt100",
		"..":                    "vt100",
		"a..b":                  "vt100",
		".hidden":               "vt100",
		"-option":               "vt100",
		"xterm\x1b]0;x\x07":     "vt100",
		"xterm\nLESSOPEN=x":     "vt100",
		"xterm color":           "vt100",
		"xterm=x":               "vt100",
		"xterm\x00":             "vt100",
		"caf\xc3\xa9":           "vt100",
	} {
		if got := logview.SafeTerm(in); got != want {
			t.Errorf("SafeTerm(%q) = %q, want %q", in, got, want)
		}
	}
	long := bytes.Repeat([]byte("a"), 65)
	if got := logview.SafeTerm(string(long)); got != "vt100" {
		t.Errorf("65 bytes: %q", got)
	}
	if got := logview.SafeTerm(string(long[:64])); got != string(long[:64]) {
		t.Errorf("64 bytes: %q", got)
	}
}
