package header

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
)

// What the pending-reboot check reads.
const (
	RebootRequiredFile = "/var/run/reboot-required"   // Debian, Ubuntu, Proxmox VE: a package asks for a reboot
	KernelReleaseFile  = "/proc/sys/kernel/osrelease" // the running kernel's release, as uname -r prints it
	KernelVersionFile  = "/proc/sys/kernel/version"   // the running kernel's build; Debian's names its package version
	BootDir            = "/boot"                      // vmlinuz-<release>: the kernels installed (Debian, Ubuntu, Proxmox VE)
	ModulesDir         = "/lib/modules"               // <release>/kernel/: each installed kernel's own modules (Alpine)
)

// KernelPackagePrefix names the package of a Debian-built kernel:
// linux-image-<release>, whose version the kernel's build names.
const KernelPackagePrefix = "linux-image-"

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
	// CodeKernelReplaced: a Debian-built kernel whose release dpkg has
	// installed as another build than the one running.
	CodeKernelReplaced = "REBOOT_KERNEL_REPLACED"
)

// The findings' details (proposed wording, docs/engine/header.md §5.3).
const (
	DetailRebootRequired = "package request"
	DetailNewerKernel    = "newer kernel installed"
	DetailModulesMissing = "running kernel removed"
	DetailKernelReplaced = "running kernel replaced"
)

// Reboot is whether the host needs a reboot to run what is installed.
type Reboot struct {
	// Pending is Yes when a source shows a reboot pending, No when every
	// source that applies shows none, and Unknown otherwise, as when no
	// source applies.
	Pending detect.Tri
	// HostKernel is true inside a container (detect's Container Yes),
	// whose kernel is the host's: the kernel's sources do not apply and
	// are not read, so Pending is the packages' request alone, and No
	// beside it says nothing of the kernel.
	HostKernel bool
	// Running is the running kernel's release; "" when it was not read
	// (inside a container it is not).
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
// sources do not apply, and only a package's request is read.
func readReboot(ctx context.Context, sys host.System, f detect.Facts) Reboot {
	fam, container := familyOf(f), f.Container.Detected == detect.Yes
	if fam == familyUnknown {
		why := errOr(f.OS.Err, fmt.Errorf("no reboot signal for %q: %w", f.OS.ID, detect.ErrNoSignal))
		return Reboot{Err: fmt.Errorf("header: reboot: %w", why)}
	}
	r := Reboot{HostKernel: container}
	var srcs []source
	if fam == familyDebian {
		srcs = append(srcs, rebootRequired(ctx, sys))
	}
	if !container {
		var ks []source
		r.Running, ks = kernelSources(ctx, sys, fam)
		srcs = append(srcs, ks...)
	}
	r.settle(srcs)
	if container {
		r.Evidence = append(r.Evidence, "kernel: the host's (container)")
	}
	return r
}

// kernelSources reads the running kernel's release and compares the
// kernel with what is installed: on Debian's family the newest in /boot
// and, for a kernel Debian built, its package's version; on Alpine its
// modules. release is "" when it was not read.
func kernelSources(ctx context.Context, sys host.System, fam family) (release string, srcs []source) {
	release, src := readRelease(ctx, sys)
	switch {
	case release == "":
		return "", []source{src}
	case fam == familyAlpine:
		return release, []source{src, modulesMissing(ctx, sys, release)}
	}
	srcs = []source{src, newerKernel(ctx, sys, release)}
	build, src, debian := readBuild(ctx, sys)
	switch {
	case !debian:
		return release, srcs
	case build == "":
		return release, append(srcs, src)
	}
	return release, append(srcs, src, kernelReplaced(ctx, sys, release, build))
}

// settle sets Pending from the sources: Yes when one shows it, else
// Unknown when one could not be read or none applies, else No.
func (r *Reboot) settle(srcs []source) {
	if len(srcs) == 0 {
		r.Err = fmt.Errorf("header: reboot: no source applies: %w", detect.ErrNoSignal)
		return
	}
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

// readBuild reads the running kernel's build. debian reports a build
// Debian made ("#1 SMP PREEMPT_DYNAMIC Debian 6.12.43-1 (2025-08-27)"),
// whose release names no package revision, so a new build can replace
// its file in /boot under the same name; build is then the source
// package's version, or "" with the error in src. A build that names
// no Debian version (Ubuntu's "#14-Ubuntu", Proxmox VE's "PMX 6.17.2-1"),
// whose release carries the revision, is not debian. A build that could
// not be read may be Debian's: debian is true, with the error.
func readBuild(ctx context.Context, sys host.System) (build string, src source, debian bool) {
	data, err := sys.ReadFile(ctx, KernelVersionFile)
	if err != nil {
		return "", source{err: err}, true
	}
	f := strings.Fields(string(data))
	i := slices.Index(f, "Debian")
	switch {
	case i < 0:
		return "", source{}, false
	case i+1 >= len(f) || !isDigit(f[i+1][0]) || strings.ContainsFunc(f[i+1], notVersion):
		return "", source{err: fmt.Errorf("%s: %q: %w", KernelVersionFile, firstLine(data), detect.ErrUnrecognised)}, true
	}
	return f[i+1], source{state: detect.No, evidence: KernelVersionFile + ": Debian " + f[i+1]}, true
}

// notVersion reports a character no dpkg version holds (deb-version(7)).
func notVersion(c rune) bool {
	return !(c < 0x80 && (isDigit(byte(c)) || isAlpha(byte(c)) || strings.ContainsRune(".+~:-", c)))
}

// kernelReplaced asks dpkg for linux-image-<release>'s version and
// compares it with the running build's: another version is Yes, whichever
// is newer, as the build running is not the one installed. The package
// not installed, or dpkg not answering, is Unknown, never No.
func kernelReplaced(ctx context.Context, sys host.System, release, build string) source {
	pkg := KernelPackagePrefix + release
	installed, version, err := detect.DpkgVersion(ctx, sys, pkg)
	switch {
	case err != nil:
		return source{err: err}
	case installed != detect.Yes || version == "":
		return source{err: fmt.Errorf("dpkg: %s not installed: %w", pkg, detect.ErrNoSignal)}
	case version != build:
		return source{state: detect.Yes, evidence: "dpkg: " + pkg + " " + version,
			finding: &engine.Finding{Code: CodeKernelReplaced, Detail: DetailKernelReplaced, Source: "dpkg-query --show " + pkg}}
	}
	return source{state: detect.No, evidence: "dpkg: " + pkg + " " + version}
}

// newerKernel compares the running kernel with the newest vmlinuz-<release>
// in /boot. A name that is no release (isRelease) is skipped. No needs
// the running kernel's own vmlinuz there: without it (/boot's partition
// not mounted, its mount point holding an older kernel; a kernel the
// hypervisor boots) /boot is not shown to be what the bootloader reads,
// and only a newer kernel decides.
func newerKernel(ctx context.Context, sys host.System, running string) source {
	if !isRelease(running) {
		return source{err: fmt.Errorf("%s: %q: no version: %w", KernelReleaseFile, running, detect.ErrUnrecognised)}
	}
	entries, err := sys.ReadDir(ctx, BootDir)
	if err != nil && !missing(err) {
		return source{err: err}
	}
	newest, present := "", false
	for _, e := range entries {
		rel, ok := strings.CutPrefix(e.Name(), "vmlinuz-")
		if !ok || e.IsDir() || !isRelease(rel) {
			continue
		}
		present = present || rel == running
		if newest == "" || compareKernels(rel, newest) > 0 {
			newest = rel
		}
	}
	switch {
	case newest == "":
		return source{err: fmt.Errorf("%s: no vmlinuz-<release>: %w", BootDir, detect.ErrNoSignal)}
	case compareKernels(newest, running) > 0:
		return source{state: detect.Yes, evidence: BootDir + ": newest vmlinuz-" + newest,
			finding: &engine.Finding{Code: CodeNewerKernel, Detail: DetailNewerKernel, Source: BootDir + "/vmlinuz-" + newest}}
	case !present:
		return source{err: fmt.Errorf("%s: no vmlinuz-%s: %w", BootDir, running, detect.ErrNoSignal)}
	}
	return source{state: detect.No, evidence: BootDir + ": newest vmlinuz-" + newest}
}

// modulesMissing: Alpine replaces the kernel's modules when it upgrades
// the kernel, so the running kernel's are gone until the reboot. A
// kernel's own modules are its kernel/ directory (linux
// Documentation/kbuild/modules.rst); a directory of the release without
// it holds others' modules only (updates/, extra/), left behind. No
// kernel's modules at all shows nothing either way.
func modulesMissing(ctx context.Context, sys host.System, running string) source {
	dir := ModulesDir + "/" + running
	has, err := hasModules(ctx, sys, dir)
	switch {
	case err != nil:
		return source{err: err}
	case has:
		return source{state: detect.No, evidence: dir + "/kernel: present"}
	}
	entries, err := sys.ReadDir(ctx, ModulesDir)
	if err != nil && !missing(err) {
		return source{err: err}
	}
	var errs []error
	for _, e := range entries {
		if !e.IsDir() || e.Name() == running {
			continue
		}
		other := ModulesDir + "/" + e.Name()
		has, err := hasModules(ctx, sys, other)
		switch {
		case err != nil:
			errs = append(errs, err)
		case has:
			return source{state: detect.Yes, evidence: dir + "/kernel: absent; " + other + "/kernel: present",
				finding: &engine.Finding{Code: CodeModulesMissing, Detail: DetailModulesMissing, Source: dir}}
		}
	}
	if len(errs) > 0 {
		return source{err: errors.Join(errs...)}
	}
	return source{err: fmt.Errorf("%s: no kernel's modules: %w", ModulesDir, detect.ErrNoSignal)}
}

// hasModules reports whether dir, a release's directory of /lib/modules,
// holds the kernel's own modules: dir/kernel, a directory. dir absent is
// false; dir or dir/kernel not a directory is in no form known.
func hasModules(ctx context.Context, sys host.System, dir string) (bool, error) {
	for _, p := range []string{dir, dir + "/kernel"} {
		fi, err := sys.Stat(ctx, p)
		switch {
		case missing(err):
			return false, nil
		case err != nil:
			return false, err
		case !fi.IsDir():
			return false, fmt.Errorf("%s: not a directory: %w", p, detect.ErrUnrecognised)
		}
	}
	return true, nil
}

// compareKernels compares two kernel releases by their versions, the
// release up to its flavour: the "-"-separated parts before the first
// that starts with a letter ("6.12.43+deb13" of "6.12.43+deb13-amd64",
// "6.17.0-14" of "6.17.0-14-generic", "6.14.8-2" of "6.14.8-2-pve"). Two
// flavours of one version are equal. The versions compare as dpkg compares
// versions (verrevcmp).
func compareKernels(a, b string) int { return verrevcmp(kernelVersion(a), kernelVersion(b)) }

// isRelease reports whether rel is a kernel release: its version starts
// with a digit ("6.12.43+deb13-amd64", not "lts" or "custom").
func isRelease(rel string) bool {
	v := kernelVersion(rel)
	return v != "" && isDigit(v[0])
}

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
