package host

import (
	"context"
	"fmt"
	"os/exec"
	"syscall"
)

// StartDetached starts name with args in a new session and does not wait
// for it (see System). Resolution, environment and working directory are
// Run's. The process gets setsid(2) from os/exec (SysProcAttr.Setsid), so it
// leads a session and a process group of its own, with no controlling
// terminal; its stdin, stdout and stderr are left nil, which os/exec
// connects to the null device. It is started with exec.Command, not
// CommandContext, so no context ever kills it. While hardn runs, a
// goroutine waits for it, so it does not linger as a zombie; once hardn
// exits, it is reparented and its new parent reaps it.
func (r *Real) StartDetached(ctx context.Context, name string, args ...string) (int, error) {
	argv := append([]string{name}, args...)
	id := r.log.Begin(argv, -1)
	pid, err := r.startDetached(ctx, argv)
	r.log.Detached(id, pid, err)
	return pid, err
}

func (r *Real) startDetached(ctx context.Context, argv []string) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, fmt.Errorf("host: start %s: %w", FormatArgv(argv), err)
	}
	path, err := r.resolve(argv[0])
	if err != nil {
		return 0, err
	}
	cmd := exec.Command(path, argv[1:]...)
	cmd.Args[0] = argv[0] // as Run sets it; busybox applets dispatch on it
	cmd.Env = append([]string(nil), commandEnv...)
	cmd.Dir = "/"
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	pid, err := r.spawn(cmd)
	if err != nil {
		return 0, fmt.Errorf("host: start %s: %w", FormatArgv(argv), err)
	}
	return pid, nil
}

func spawnCmd(cmd *exec.Cmd) (int, error) {
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	go func() { _ = cmd.Wait() }()
	return cmd.Process.Pid, nil
}

// Detached writes the outcome of entry id, a StartDetached: the process id,
// or why it did not start.
func (l *RunLog) Detached(id, pid int, err error) {
	var line string
	if err != nil {
		line = fmt.Sprintf("run %d: not started: %v\n", id, err)
	} else {
		line = fmt.Sprintf("run %d: started detached, pid %d, not waited for\n", id, pid)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.write([]byte(line))
}
