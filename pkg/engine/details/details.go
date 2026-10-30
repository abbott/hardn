// Package details is the System Details report's reader (refactor plan slice
// 1, next/s1-readers): P-17's seven sections of the host, read through
// host.System and returned as data, with nothing rendered. Read takes detect's
// facts, so the platform is not detected again, and gives:
//
//   - OS and Kernel: the distribution and the uptime, the header probe's
//     (pkg/engine/header), and /proc/sys/kernel/osrelease;
//   - Host and IPs: the hostname, the header probe's, and every global address
//     in both families, `ip -o addr show scope global`;
//   - Users: uid 1000 and up, but nobody and nfsnobody, each with what lets
//     them administer, as the users check reads it (users.Elevation);
//   - CPU and Load: /proc/cpuinfo and /proc/loadavg;
//   - Disk: /proc/self/mounts' line for /, then `df -kP /`, or on ZFS the
//     dataset mounted there and its pool (`zpool list`, `zfs get`);
//   - Memory: /proc/meminfo's MemTotal and MemAvailable;
//   - Last Login: root and each listed user, in one `lslogins` call, with its
//     blank guarded (logins.go).
//
// Each section keeps what it read when another fails, and a fact keeps its
// section's others: what could not be read is in the section's Unread, by
// source and why, and a zero fact is not established. Nothing is guessed. It
// returns facts and states, not rows or words: unit (c)'s screen and
// pkg/output's document map them. It reads files and runs read-only commands
// alone, each bounded (bounds), and writes nothing.
// docs/engine/readers-system-details.md has each source and the real form of
// each line.
package details

import (
	"context"
	"fmt"
	"time"

	"github.com/abbott/hardn/pkg/engine/header"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
)

// Report is what Read read of the host, in P-17's order.
type Report struct {
	OS     OSKernel
	Host   HostIPs
	Users  Users
	CPU    CPU
	Disk   Disk
	Memory Memory
	Logins Logins
}

// Unread says what of a section could not be read: its source, a file or a
// command (SourceIP and the others), and why. Its Err is the host's, so
// errors.Is finds fs.ErrPermission, host.ErrNotFound and
// context.DeadlineExceeded, the last where a command hit its bound.
type Unread struct {
	Source string
	Err    error
}

// The commands, as a section's Unread names them.
const (
	SourceIP       = "ip addr"
	SourceDF       = "df"
	SourceZpool    = "zpool list"
	SourceZFS      = "zfs get"
	SourceLslogins = "lslogins"
)

// bounds are how long Read waits for a command, whatever the command does.
// lslogins' is its own: as root it takes the password lock for each account,
// up to 15 s where another tool holds it (lslogins.c v2.41:831, lckpwdf; read
// from glibc, not run), and it reads the whole of wtmp, about 0.9 s on the VMs'
// 34 to 36 MB (vm-runs/s1/08-lslogins.log). 20 s is one such wait and a wtmp
// about five times theirs. Past grace, a command that has not ended is left
// behind: host.Real kills it when ctx is done, but its Wait returns only once
// the process has gone, and one stuck in the kernel on a hung pool or mount
// cannot go (os/exec's Cmd.Wait).
type bounds struct {
	command, lslogins, grace time.Duration
}

var defaultBounds = bounds{command: 5 * time.Second, lslogins: 20 * time.Second, grace: time.Second}

// Read reads the host. f is detect's facts for it. A done ctx leaves every fact
// not yet read with ctx's error. It writes nothing, and adds no line of its own
// to a run log: each command is the host's to log, so a draw takes a host with
// no run log, as `hardn status` does (cmd/hardn/status.go:29).
func Read(ctx context.Context, sys host.System, f detect.Facts) Report {
	return read(ctx, sys, f, defaultBounds)
}

func read(ctx context.Context, sys host.System, f detect.Facts, b bounds) Report {
	sys = boundedSystem{System: sys, bounds: b}
	hd := header.Read(ctx, sys, f)
	users := readUsers(ctx, sys, f)
	return Report{
		OS:     readOS(ctx, sys, hd),
		Host:   readHost(ctx, sys, hd),
		Users:  users,
		CPU:    readCPU(ctx, sys),
		Disk:   readDisk(ctx, sys),
		Memory: readMemory(ctx, sys),
		Logins: readLogins(ctx, sys, users.List, b.command),
	}
}

// boundedSystem is sys with every command Read runs bounded, those of the
// packages it reads through too (header.Read's, the elevation tools' checks).
type boundedSystem struct {
	host.System
	bounds bounds
}

// Run runs the command under its bound, and returns at the bound plus grace
// whether or not the command has ended: the error then wraps ctx's.
func (s boundedSystem) Run(ctx context.Context, name string, args ...string) (host.Result, error) {
	limit := s.bounds.command
	if name == "lslogins" {
		limit = s.bounds.lslogins
	}
	ctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	type answer struct {
		res host.Result
		err error
	}
	done := make(chan answer, 1)
	go func() {
		res, err := s.System.Run(ctx, name, args...)
		done <- answer{res, err}
	}()
	select {
	case a := <-done:
		return a.res, a.err
	case <-ctx.Done():
	}
	timer := time.NewTimer(s.bounds.grace)
	defer timer.Stop()
	select {
	case a := <-done:
		return a.res, a.err
	case <-timer.C:
		argv := append([]string{name}, args...)
		return host.Result{Argv: argv, ExitCode: -1}, fmt.Errorf("details: %s did not end: %w", host.FormatArgv(argv), ctx.Err())
	}
}
