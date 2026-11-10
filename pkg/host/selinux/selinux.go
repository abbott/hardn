// Package selinux reads SELinux's mode and compares files' labels with the
// policy's defaults, for the checks hardn makes before a change on a host
// where SELinux enforces (Rocky Linux, AlmaLinux: docs/engine/el-changes.md).
// It changes the host only to label an SSH port, and in RollbackPorts.
package selinux

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strconv"
	"strings"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
)

// EnforceFile is selinuxfs's mode: "1" enforcing, "0" permissive; absent
// when SELinux is disabled.
const EnforceFile = "/sys/fs/selinux/enforce"

// Matchpathcon is the command Mislabeled runs: -V compares each path's
// label with the policy's default and says "<path> verified." or "<path>
// has context <got>, should be <want>" (libselinux-utils; read on
// rocky10-test-110).
var Matchpathcon = []string{"matchpathcon", "-V"}

// ErrMislabeled: a file's label is not the policy's default, so a confined
// program (sshd) may be refused it.
var ErrMislabeled = errors.New("SELinux labels differ from the policy's")

// ErrNotRunnable: hardn's binary is of an SELinux type systemd does not
// run, so the revert timer and the boot unit would never start it.
var ErrNotRunnable = errors.New("systemd may not run a file of this SELinux type")

// RunnableType is the type of hardn's binary that systemd ran on
// rocky10-test-110 (a copy of /usr/bin/true installed in /usr/local/bin);
// admin_home_t (in /root, the policy's own label there) and user_tmp_t
// (moved from /tmp) failed with exit 203. Only it is let through.
const RunnableType = "bin_t"

// Stat is the command Type runs: coreutils' stat, the file's security
// context as "user:role:type:level".
var Stat = []string{"stat", "-c", "%C"}

// Type is path's SELinux type, the third field of its context (Stat).
func Type(ctx context.Context, sys host.System, path string) (string, error) {
	res, err := sys.Run(ctx, Stat[0], append(Stat[1:], "--", path)...)
	if err != nil {
		return "", fmt.Errorf("selinux: %w", err)
	}
	c := strings.TrimSpace(string(res.Stdout))
	f := strings.Split(c, ":")
	if len(f) < 4 || f[2] == "" {
		return "", fmt.Errorf("selinux: stat -c %%C %s printed %q: %w", path, c, detect.ErrUnrecognised)
	}
	return f[2], nil
}

// CheckRunnable is nil where SELinux does not enforce, or where path's
// type is RunnableType; ErrNotRunnable naming the type otherwise, and the
// error where the mode or the type is not established. It asks what the
// file is, not what the policy would label it: the policy labels /root's
// files admin_home_t, which systemd does not run.
func CheckRunnable(ctx context.Context, sys host.System, path string) error {
	switch on, err := Enforcing(ctx, sys); {
	case err != nil:
		return err
	case on != detect.Yes:
		return nil
	}
	t, err := Type(ctx, sys, path)
	switch {
	case err != nil:
		return err
	case t != RunnableType:
		return fmt.Errorf("selinux: %s is %s, not %s: %w", path, t, RunnableType, ErrNotRunnable)
	}
	return nil
}

// Enforcing reports whether SELinux enforces: Yes for EnforceFile "1", No
// for "0" (permissive: denials are logged, not made) or no selinuxfs, and
// Unknown with the error otherwise.
func Enforcing(ctx context.Context, sys host.System) (detect.Tri, error) {
	data, err := sys.ReadFile(ctx, EnforceFile)
	switch v := strings.TrimSpace(string(data)); {
	case errors.Is(err, fs.ErrNotExist):
		return detect.No, nil
	case err != nil:
		return detect.Unknown, fmt.Errorf("selinux: %w", err)
	case v == "1":
		return detect.Yes, nil
	case v == "0":
		return detect.No, nil
	default:
		return detect.Unknown, fmt.Errorf("selinux: %s: %q: %w", EnforceFile, data, detect.ErrUnrecognised)
	}
}

// Mislabeled returns matchpathcon -V's line for each of paths whose label
// differs from the policy's default. A path the policy gives no default
// ("should be <<none>>") is not mislabeled. A line in no form it knows, or
// matchpathcon failing to run, is an error.
func Mislabeled(ctx context.Context, sys host.System, paths ...string) ([]string, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	res, err := sys.Run(ctx, Matchpathcon[0], append(Matchpathcon[1:], paths...)...)
	var xe *host.ExitError
	if err != nil && !errors.As(err, &xe) {
		return nil, fmt.Errorf("selinux: %w", err)
	}
	var bad []string
	seen := 0
	for _, l := range strings.Split(string(res.Stdout), "\n") {
		switch l = strings.TrimSpace(l); {
		case l == "":
			continue
		case strings.HasSuffix(l, " verified."):
		case strings.Contains(l, " has context ") && strings.HasSuffix(l, ", should be <<none>>"):
		case strings.Contains(l, " has context ") && strings.Contains(l, ", should be "):
			bad = append(bad, l)
		default:
			return nil, fmt.Errorf("selinux: matchpathcon -V printed %q: %w", l, detect.ErrUnrecognised)
		}
		seen++
	}
	if seen != len(paths) {
		return nil, fmt.Errorf("selinux: matchpathcon -V answered %d of %d paths (exit %d, %q): %w", seen, len(paths), res.ExitCode,
			strings.TrimSpace(string(res.Stderr)), detect.ErrUnrecognised)
	}
	return bad, nil
}

// Check is nil where SELinux does not enforce, or where every path has the
// policy's label; ErrMislabeled naming the paths otherwise. Where whether
// SELinux enforces, or a label, is not established, it is that error: the
// caller refuses, as for a mislabeled file.
func Check(ctx context.Context, sys host.System, paths ...string) error {
	switch on, err := Enforcing(ctx, sys); {
	case err != nil:
		return err
	case on != detect.Yes:
		return nil
	}
	bad, err := Mislabeled(ctx, sys, paths...)
	if err != nil {
		return err
	}
	if len(bad) > 0 {
		return fmt.Errorf("selinux: %s: %w", strings.Join(bad, "; "), ErrMislabeled)
	}
	return nil
}

// SSHPortType is the type sshd may bind a port of.
const SSHPortType = "ssh_port_t"

// Semanage is the command PlanSSHPort reads and labels ports with
// (policycoreutils-python-utils).
const Semanage = "semanage"

var (
	// ErrNoSemanage: SELinux is on and semanage is not installed, so a new
	// SSH port cannot be labelled for sshd.
	ErrNoSemanage = errors.New("semanage is not installed")
	// ErrPortTaken: the policy gives the port to another service by name,
	// which semanage -a cannot change and -m would take from it.
	ErrPortTaken = errors.New("SELinux gives the port to another service")
)

// PortStep is the semanage that labels Port for sshd.
type PortStep struct {
	Port uint16
	Argv []string
}

// Unit names PortsUndo in hardn's journal (revert.OpPlan; plans.Registry).
// Stable: a journal written by one hardn is rolled back by the next.
const Unit = "selinux-ports"

// PortsUndo is the ports PortSteps label, for RollbackPorts.
type PortsUndo struct {
	Ports []uint16 `json:"ports"`
}

// Check refuses port 0 (fs.ErrInvalid).
func (u PortsUndo) Check() error {
	if slices.Contains(u.Ports, 0) {
		return fmt.Errorf("selinux: port 0: %w", fs.ErrInvalid)
	}
	return nil
}

// RollbackPorts takes away the ssh_port_t entry of each of u's ports that
// semanage port -l has: PlanSSHPort plans only a port without an entry of
// its own, so one there now is the change's, and a rollback taken up
// again, or one of a step that never ran, takes away nothing else. It
// goes on past a port that fails, and returns every error.
func RollbackPorts(ctx context.Context, sys host.System, u PortsUndo) error {
	if err := u.Check(); err != nil {
		return err
	}
	res, err := sys.Run(ctx, Semanage, "port", "-l")
	if err != nil {
		return fmt.Errorf("selinux: %w", err)
	}
	have := exactTCP(res.Stdout)
	var errs []error
	for _, p := range u.Ports {
		if have[p] != SSHPortType {
			continue
		}
		argv := []string{Semanage, "port", "-d", "-t", SSHPortType, "-p", "tcp", strconv.Itoa(int(p))}
		if _, err := sys.Run(ctx, argv[0], argv[1:]...); err != nil {
			errs = append(errs, fmt.Errorf("selinux: %s: %w", host.FormatArgv(argv), err))
		}
	}
	return errors.Join(errs...)
}

// PlanSSHPort plans TCP port labelled ssh_port_t, which sshd needs to bind
// it where SELinux is on: nil where SELinux is disabled (no selinuxfs) or
// the policy already labels the port so. A port another type names alone
// is ErrPortTaken; one only a range of another type holds (unreserved_port_t
// 1024-32767, on rocky10-test-110) is labelled: an entry of its own wins
// over a range. semanage missing is ErrNoSemanage. It changes nothing.
func PlanSSHPort(ctx context.Context, sys host.System, port uint16) (*PortStep, error) {
	if _, err := sys.Stat(ctx, EnforceFile); errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("selinux: %w", err)
	}
	res, err := sys.Run(ctx, Semanage, "port", "-l")
	switch {
	case errors.Is(err, host.ErrNotFound):
		return nil, fmt.Errorf("selinux: port %d: %w", port, ErrNoSemanage)
	case err != nil:
		return nil, fmt.Errorf("selinux: %w", err)
	}
	if t, ok := exactTCP(res.Stdout)[port]; ok {
		if t == SSHPortType {
			return nil, nil
		}
		return nil, fmt.Errorf("selinux: port %d is %s: %w", port, t, ErrPortTaken)
	}
	n := strconv.Itoa(int(port))
	return &PortStep{Port: port, Argv: []string{Semanage, "port", "-a", "-t", SSHPortType, "-p", "tcp", n}}, nil
}

// LabelsSSHPort reports whether semanage port -l labels TCP port
// ssh_port_t by an entry of its own (the check after the step).
func LabelsSSHPort(ctx context.Context, sys host.System, port uint16) (bool, error) {
	res, err := sys.Run(ctx, Semanage, "port", "-l")
	if err != nil {
		return false, fmt.Errorf("selinux: %w", err)
	}
	return exactTCP(res.Stdout)[port] == SSHPortType, nil
}

// exactTCP reads semanage port -l ("<type> <proto> <port>, <port>-<port>,
// ...") into the TCP ports named alone, by type; ranges are left out.
func exactTCP(out []byte) map[uint16]string {
	m := map[uint16]string{}
	for _, l := range strings.Split(string(out), "\n") {
		f := strings.Fields(l)
		if len(f) < 3 || f[1] != "tcp" {
			continue
		}
		for _, p := range strings.Split(strings.Join(f[2:], ""), ",") {
			if n, err := strconv.ParseUint(p, 10, 16); err == nil {
				m[uint16(n)] = f[0]
			}
		}
	}
	return m
}
