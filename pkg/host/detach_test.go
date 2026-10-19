package host

import (
	"context"
	"errors"
	"os/exec"
	"reflect"
	"strings"
	"testing"
)

// TestRealStartDetached checks the command StartDetached would start, through
// the spawn seam: nothing is ever started.
func TestRealStartDetached(t *testing.T) {
	dir := t.TempDir()
	sh := touch(t, dir, "sh", 0o755)
	s := newSeamed(t, Options{}, []string{dir}, nil)
	var got *exec.Cmd
	s.spawn = func(cmd *exec.Cmd) (int, error) {
		got = cmd
		return 4321, nil
	}

	args := []string{"-c", `sleep "$1" && "$0" rollback "$2"`, "/usr/local/bin/hardn", "180", "20261018T120000Z-1a2b3c4d"}
	pid, err := s.StartDetached(context.Background(), "sh", args...)
	if err != nil || pid != 4321 {
		t.Fatalf("StartDetached = %d, %v; want 4321, nil", pid, err)
	}
	if got.Path != sh {
		t.Errorf("Path = %q, want %q (resolved from the search path)", got.Path, sh)
	}
	if want := append([]string{"sh"}, args...); !reflect.DeepEqual(got.Args, want) {
		t.Errorf("Args = %q, want %q", got.Args, want)
	}
	if want := []string{"PATH=" + SearchPath, "LC_ALL=C"}; !reflect.DeepEqual(got.Env, want) {
		t.Errorf("Env = %q, want %q", got.Env, want)
	}
	if got.Dir != "/" {
		t.Errorf("Dir = %q, want /", got.Dir)
	}
	if got.SysProcAttr == nil || !got.SysProcAttr.Setsid {
		t.Errorf("SysProcAttr = %+v, want Setsid", got.SysProcAttr)
	}
	// nil streams are the null device (os/exec); a pipe would tie the
	// process to hardn (system.md §2.5).
	if got.Stdin != nil || got.Stdout != nil || got.Stderr != nil {
		t.Errorf("Stdin, Stdout, Stderr = %v, %v, %v; want nil (the null device)", got.Stdin, got.Stdout, got.Stderr)
	}
	if got.Cancel != nil {
		t.Error("Cancel is set: a context could kill the detached process")
	}
	log := s.log.String()
	if !strings.Contains(log, "run 1: $ sh -c") || !strings.Contains(log, "run 1: started detached, pid 4321, not waited for") {
		t.Errorf("run log = %q", log)
	}
}

func TestRealStartDetachedErrors(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "sh", 0o755)
	boom := errors.New("fork: resource temporarily unavailable")
	tests := []struct {
		name  string
		ctx   func() context.Context
		cmd   string
		spawn error
		want  error
	}{
		{"not in the search path", context.Background, "missing", nil, ErrNotFound},
		{"relative path", context.Background, "./sh", nil, ErrNotFound},
		{"done context", func() context.Context {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			return ctx
		}, "sh", nil, context.Canceled},
		{"start fails", context.Background, "sh", boom, boom},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newSeamed(t, Options{}, []string{dir}, nil)
			spawned := 0
			s.spawn = func(*exec.Cmd) (int, error) {
				spawned++
				return 0, tt.spawn
			}
			pid, err := s.StartDetached(tt.ctx(), tt.cmd)
			if !errors.Is(err, tt.want) || pid != 0 {
				t.Fatalf("StartDetached = %d, %v; want 0, %v", pid, err, tt.want)
			}
			if tt.spawn == nil && spawned != 0 {
				t.Errorf("spawned %d times, want none", spawned)
			}
			if !strings.Contains(s.log.String(), "run 1: not started: ") {
				t.Errorf("run log = %q, want the failure", s.log.String())
			}
		})
	}
}
