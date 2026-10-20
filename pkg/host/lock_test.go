package host

import (
	"bufio"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// lockHelperEnv names the lock file TestLockHelperProcess holds.
const lockHelperEnv = "HARDN_TEST_LOCK_HELPER"

// TestLockHelperProcess is not a test: TestLockReleasedWhenHolderDies runs
// this test binary again with it alone, as the process that holds the lock
// and is killed. It runs nothing else.
func TestLockHelperProcess(t *testing.T) {
	name := os.Getenv(lockHelperEnv)
	if name == "" {
		t.Skip("helper process for TestLockReleasedWhenHolderDies")
	}
	unlock, err := NewReal(Options{}).Lock(context.Background(), name, []byte("pid helper\n"))
	if err != nil {
		os.Exit(3)
	}
	_ = unlock // held until the process is killed: never released here
	_, _ = os.Stdout.WriteString("locked\n")
	time.Sleep(time.Minute)
	os.Exit(4)
}

// TestLockReleasedWhenHolderDies: a holder killed with SIGKILL, which runs
// no clean-up of its own, leaves its lock file behind, and the file is not
// held: the kernel released the lock with the holder's open file
// (docs/engine/engine.md, the run lock).
func TestLockReleasedWhenHolderDies(t *testing.T) {
	ctx := context.Background()
	name := filepath.Join(t.TempDir(), "engine.lock")
	cmd := exec.Command(os.Args[0], "-test.run=^TestLockHelperProcess$", "-test.count=1")
	cmd.Env = append(os.Environ(), lockHelperEnv+"="+name)
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	line, err := bufio.NewReader(out).ReadString('\n')
	if err != nil || line != "locked\n" {
		t.Fatalf("helper: %q, %v", line, err)
	}
	r := NewReal(Options{})
	if _, err := r.Lock(ctx, name, []byte("pid test\n")); !errors.Is(err, ErrLocked) {
		t.Fatalf("Lock while the helper holds it: %v, want ErrLocked", err)
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	if data, err := os.ReadFile(name); err != nil || string(data) != "pid helper\n" {
		t.Errorf("the dead holder's lock file: %q, %v; want it left with its holder line", data, err)
	}
	unlock, err := r.Lock(ctx, name, []byte("pid test\n"))
	if err != nil {
		t.Fatalf("Lock after the holder died: %v", err)
	}
	if err := unlock(); err != nil {
		t.Error(err)
	}
}
