package header

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
)

// What the pending-reboot check reads.
const (
	RebootRequiredFile = "/var/run/reboot-required"   // Debian, Ubuntu, Proxmox VE: a package asks for a reboot
	KernelReleaseFile  = "/proc/sys/kernel/osrelease" // the running kernel's release, as uname -r prints it
	BootDir            = "/boot"                      // vmlinuz-<release>: the kernels installed (Debian, Ubuntu, Proxmox VE)
	ModulesDir         = "/lib/modules"               // <release>/: each installed kernel's modules (Alpine)
)

// The finding codes of this package (pkg/output's registry holds them):
// why a reboot is pending, one finding per source that shows it.
const (
	// CodeRebootRequired: /var/run/reboot-required exists.
	CodeRebootRequired = "REBOOT_REQUIRED"
	// CodeNewerKernel: /boot holds a kernel newer than the one running.
	CodeNewerKernel = "REBOOT_NEWER_KERNEL"
	// CodeModulesMissing: Alpine: the running kernel's modules are not
	// installed, while another kernel's are.
	CodeModulesMissing = "REBOOT_MODULES_MISSING"
)

// The findings' details (proposed wording, docs/engine/header.md §5.3).
const (
	DetailRebootRequired = "updates need a reboot"
	DetailNewerKernel    = "newer kernel installed"
	DetailModulesMissing = "running kernel's modules gone"
)

// Reboot is whether the host needs a reboot to run what is installed.
type Reboot struct {
	// Pending is Yes when a source shows a reboot pending, No when every
	// source that applies shows none, and Unknown otherwise.
	Pending detect.Tri
	// Running is the running kernel's release; "" when it was not read
	// (inside a container, whose kernel is the host's, it is not).
	Running string
	// Findings say why it is pending: one per source that shows it.
	Findings []engine.Finding
	// Evidence is what each source showed, one line each.
	Evidence []string
	// Err is why a source could not be read. Pending is Yes beside it
	// when another source shows a reboot pending.
	Err error
}

// source is what one source of the check showed.
type source struct {
	state    detect.Tri
	finding  *engine.Finding
	evidence string
	err      error
}

// family is a set of platforms the check reads alike.
type family uint8

const (
	familyUnknown family = iota
	familyDebian         // Debian, Ubuntu, Proxmox VE and their derivatives
	familyAlpine
)

// familyOf is the first of detect's platform, the platform's ID_LIKE,
// os-release's ID and its ID_LIKE that the check knows.
func familyOf(f detect.Facts) family {
	ids := append([]string{string(f.Platform.Distro), string(f.Platform.Like), f.OS.ID}, f.OS.IDLike...)
	for _, id := range ids {
		switch id {
		case "debian", "ubuntu", "proxmox":
			return familyDebian
		case "alpine":
			return familyAlpine
		}
	}
	return familyUnknown
}

// readReboot reads the sources that apply to the platform. Inside a
// container (detect's Container Yes) the kernel is the host's: the kernel
// sources are not read, and only a package's request is.
func readReboot(ctx context.Context, sys host.System, f detect.Facts) Reboot {
	fam, container := familyOf(f), f.Container.Detected == detect.Yes
	if fam == familyUnknown {
		why := errOr(f.OS.Err, fmt.Errorf("no reboot signal for %q: %w", f.OS.ID, detect.ErrNoSignal))
		return Reboot{Err: fmt.Errorf("header: reboot: %w", why)}
	}
	var r Reboot
	var srcs []source
	if fam == familyDebian {
		srcs = append(srcs, rebootRequired(ctx, sys))
	}
	if container {
		srcs = append(srcs, source{state: detect.No, evidence: "kernel: the host's (container)"})
	} else {
		running, src := readRelease(ctx, sys)
		r.Running = running
		switch {
		case running == "":
			srcs = append(srcs, src)
		case fam == familyDebian:
			srcs = append(srcs, src, newerKernel(ctx, sys, running))
		default:
			srcs = append(srcs, src, modulesMissing(ctx, sys, running))
		}
	}
	r.settle(srcs)
	return r
}

// settle sets Pending from the sources: Yes when one shows it, else
// Unknown when one could not be read, else No.
func (r *Reboot) settle(srcs []source) {
	r.Pending = detect.No
	var errs []error
	for _, s := range srcs {
		if s.evidence != "" {
			r.Evidence = append(r.Evidence, s.evidence)
		}
		if s.finding != nil {
			r.Findings = append(r.Findings, *s.finding)
		}
		switch s.state {
		case detect.Yes:
			r.Pending = detect.Yes
		case detect.Unknown:
			errs = append(errs, s.err)
		}
	}
	if len(errs) > 0 {
		r.Err = fmt.Errorf("header: reboot: %w", errors.Join(errs...))
		if r.Pending != detect.Yes {
			r.Pending = detect.Unknown
		}
	}
}

// readRelease reads the running kernel's release. The source it returns
// is the release's evidence, or the error when it could not be read.
func readRelease(ctx context.Context, sys host.System) (string, source) {
	data, err := sys.ReadFile(ctx, KernelReleaseFile)
	release := strings.TrimSpace(string(data))
	switch {
	case err != nil:
		return "", source{err: err}
	case release == "" || strings.ContainsAny(release, "/ \t\n"):
		return "", source{err: fmt.Errorf("%s: %q: %w", KernelReleaseFile, firstLine(data), detect.ErrUnrecognised)}
	}
	return release, source{state: detect.No, evidence: KernelReleaseFile + ": " + release}
}

// rebootRequired: /var/run/reboot-required exists. Its absence says
// nothing more than that no package asked: Debian writes it less often
// than Ubuntu (docs/engine/header.md §3), and the kernel source covers
// the kernel.
func rebootRequired(ctx context.Context, sys host.System) source {
	_, err := sys.Stat(ctx, RebootRequiredFile)
	switch {
	case err == nil:
		return source{state: detect.Yes, evidence: RebootRequiredFile + ": present",
			finding: &engine.Finding{Code: CodeRebootRequired, Detail: DetailRebootRequired, Source: RebootRequiredFile}}
	case missing(err):
		return source{state: detect.No, evidence: RebootRequiredFile + ": absent"}
	}
	return source{err: err}
}

// newerKernel compares the running kernel with the newest vmlinuz-<release>
// in /boot. A /boot with no kernel (not mounted; a kernel the hypervisor
// boots) shows nothing either way.
func newerKernel(ctx context.Context, sys host.System, running string) source {
	entries, err := sys.ReadDir(ctx, BootDir)
	if err != nil && !missing(err) {
		return source{err: err}
	}
	newest := ""
	for _, e := range entries {
		rel, ok := strings.CutPrefix(e.Name(), "vmlinuz-")
		if ok && rel != "" && !e.IsDir() && (newest == "" || compareKernels(rel, newest) > 0) {
			newest = rel
		}
	}
	if newest == "" {
		return source{err: fmt.Errorf("%s: no vmlinuz-<release>: %w", BootDir, detect.ErrNoSignal)}
	}
	path := BootDir + "/vmlinuz-" + newest
	if compareKernels(newest, running) > 0 {
		return source{state: detect.Yes, evidence: BootDir + ": newest vmlinuz-" + newest,
			finding: &engine.Finding{Code: CodeNewerKernel, Detail: DetailNewerKernel, Source: path}}
	}
	return source{state: detect.No, evidence: BootDir + ": newest vmlinuz-" + newest}
}

// modulesMissing: Alpine replaces the kernel's modules when it upgrades
// the kernel, so the running kernel's are gone until the reboot. A
// /lib/modules with no kernel's modules at all shows nothing either way.
func modulesMissing(ctx context.Context, sys host.System, running string) source {
	dir := ModulesDir + "/" + running
	fi, err := sys.Stat(ctx, dir)
	switch {
	case err == nil && fi.IsDir():
		return source{state: detect.No, evidence: dir + ": present"}
	case err == nil:
		return source{err: fmt.Errorf("%s: not a directory: %w", dir, detect.ErrUnrecognised)}
	case !missing(err):
		return source{err: err}
	}
	entries, err := sys.ReadDir(ctx, ModulesDir)
	if err != nil && !missing(err) {
		return source{err: err}
	}
	for _, e := range entries {
		if e.IsDir() {
			return source{state: detect.Yes, evidence: dir + ": absent; " + ModulesDir + ": " + e.Name(),
				finding: &engine.Finding{Code: CodeModulesMissing, Detail: DetailModulesMissing, Source: dir}}
		}
	}
	return source{err: fmt.Errorf("%s: no kernel's modules: %w", ModulesDir, detect.ErrNoSignal)}
}

// compareKernels compares two kernel releases by their versions, the
// release up to its flavour: the "-"-separated parts before the first
// that starts with a letter ("6.12.43+deb13" of "6.12.43+deb13-amd64",
// "6.17.0-14" of "6.17.0-14-generic", "6.14.8-2" of "6.14.8-2-pve"). Two
// flavours of one version are equal. The versions compare as dpkg compares
// versions (verrevcmp).
func compareKernels(a, b string) int { return verrevcmp(kernelVersion(a), kernelVersion(b)) }

func kernelVersion(release string) string {
	parts := strings.Split(release, "-")
	for i, p := range parts {
		if p != "" && isAlpha(p[0]) {
			return strings.Join(parts[:i], "-")
		}
	}
	return release
}

// verrevcmp is dpkg's comparison of two version strings (lib/dpkg/version.c):
// alternating runs of non-digits, compared by character with letters
// before other characters and "~" before everything, even the end, and
// runs of digits, compared as numbers.
func verrevcmp(a, b string) int {
	i, j := 0, 0
	for i < len(a) || j < len(b) {
		for i < len(a) && !isDigit(a[i]) || j < len(b) && !isDigit(b[j]) {
			if ac, bc := order(a, i), order(b, j); ac != bc {
				return ac - bc
			}
			i, j = i+1, j+1
		}
		for i < len(a) && a[i] == '0' {
			i++
		}
		for j < len(b) && b[j] == '0' {
			j++
		}
		first := 0
		for i < len(a) && isDigit(a[i]) && j < len(b) && isDigit(b[j]) {
			if first == 0 {
				first = int(a[i]) - int(b[j])
			}
			i, j = i+1, j+1
		}
		switch {
		case i < len(a) && isDigit(a[i]):
			return 1
		case j < len(b) && isDigit(b[j]):
			return -1
		case first != 0:
			return first
		}
	}
	return 0
}

// order is dpkg's weight of the character at s[i]: 0 for a digit or the
// end, the character for a letter, -1 for "~", the character plus 256
// for anything else.
func order(s string, i int) int {
	switch {
	case i >= len(s) || isDigit(s[i]):
		return 0
	case isAlpha(s[i]):
		return int(s[i])
	case s[i] == '~':
		return -1
	}
	return int(s[i]) + 256
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }
func isAlpha(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }
