package detect

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/abbott/hardn/pkg/host"
)

// The container signals, in the order systemd's detect_container() checks
// them (src/basic/virt.c), less the ones that do not apply to hardn: OpenVZ,
// WSL, proot, PID 1's own $container, and the cgroup namespace.
const (
	ContainerManagerFile = "/run/host/container-manager" // written by the container manager
	SystemdContainerFile = "/run/systemd/container"      // systemd as PID 1 copies its $container here
	InitEnviron          = "/proc/1/environ"             // PID 1's container=; needs root
	PodmanEnvFile        = "/run/.containerenv"
	DockerEnvFile        = "/.dockerenv" // last of the files: it can end up in an image by accident
	SelfPIDNamespace     = "/proc/self/ns/pid"
)

// InitPIDNamespaceInode is the inode of the initial PID namespace, fixed by
// the kernel since 3.8 (PROC_PID_INIT_INO, 0xEFFFFFFC).
const InitPIDNamespaceInode = 0xEFFFFFFC

// Container is whether hardn runs inside a container.
type Container struct {
	Detected Tri
	Signal   string // the check that fired, e.g. "/proc/1/environ"
	// Manager is what that signal names (lxc, docker, podman,
	// systemd-nspawn, ...); "" for the PID namespace check, which names none.
	Manager string
	Err     error // checks that could not be made, when Detected is Unknown
}

// DetectContainer runs the checks in order and stops at the first that
// fires. When none fires it is No only if every check could be made: a
// /proc/1/environ that cannot be read (not root) leaves it Unknown.
func DetectContainer(ctx context.Context, sys host.System) Container {
	checks := []struct {
		signal string
		check  func() (string, Tri, error)
	}{
		{ContainerManagerFile, func() (string, Tri, error) { return oneLine(ctx, sys, ContainerManagerFile) }},
		{SystemdContainerFile, func() (string, Tri, error) { return oneLine(ctx, sys, SystemdContainerFile) }},
		{InitEnviron, func() (string, Tri, error) { return initContainerVar(ctx, sys) }},
		{PodmanEnvFile, func() (string, Tri, error) { _, t, err := stat(ctx, sys, PodmanEnvFile); return "podman", t, err }},
		{DockerEnvFile, func() (string, Tri, error) { _, t, err := stat(ctx, sys, DockerEnvFile); return "docker", t, err }},
		{SelfPIDNamespace, func() (string, Tri, error) { return pidNamespace(ctx, sys) }},
	}
	var errs []error
	for _, c := range checks {
		manager, t, err := c.check()
		switch t {
		case Yes:
			return Container{Detected: Yes, Signal: c.signal, Manager: manager}
		case Unknown:
			errs = append(errs, fmt.Errorf("%s: %w", c.signal, errOr(err, ErrUnrecognised)))
		}
	}
	if len(errs) > 0 {
		return Container{Err: fmt.Errorf("detect: container: %w", errors.Join(errs...))}
	}
	return Container{Detected: No}
}

// oneLine reads the first line of a file a manager writes its name to: Yes
// when it is not empty.
func oneLine(ctx context.Context, sys host.System, name string) (string, Tri, error) {
	data, err := sys.ReadFile(ctx, name)
	switch {
	case missing(err):
		return "", No, nil
	case err != nil:
		return "", Unknown, err
	}
	if line := firstLine(data); line != "" {
		return line, Yes, nil
	}
	return "", No, nil
}

// initContainerVar reads container= from PID 1's NUL-separated environment.
func initContainerVar(ctx context.Context, sys host.System) (string, Tri, error) {
	data, err := sys.ReadFile(ctx, InitEnviron)
	if err != nil {
		return "", Unknown, err
	}
	for _, kv := range bytes.Split(data, []byte{0}) {
		if v, ok := bytes.CutPrefix(kv, []byte("container=")); ok && len(v) > 0 {
			return string(v), Yes, nil
		}
	}
	return "", No, nil
}

// pidNamespace compares our PID namespace's inode, which the link
// /proc/self/ns/pid carries as "pid:[inode]" (namespaces(7)), with the
// initial namespace's. It names no manager.
func pidNamespace(ctx context.Context, sys host.System) (string, Tri, error) {
	target, err := sys.Readlink(ctx, SelfPIDNamespace)
	if err != nil {
		return "", Unknown, err
	}
	inode, ok := strings.CutPrefix(target, "pid:[")
	inode, ok2 := strings.CutSuffix(inode, "]")
	n, perr := strconv.ParseUint(inode, 10, 64)
	switch {
	case !ok || !ok2 || perr != nil:
		return "", Unknown, fmt.Errorf("link %q: %w", target, ErrUnrecognised)
	case n == InitPIDNamespaceInode:
		return "", No, nil
	}
	return "", Yes, nil
}
