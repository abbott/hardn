package detect

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/abbott/hardn/pkg/host"
)

// Variant is a custom distribution that keeps Debian's os-release, so its
// Distro is DistroDebian (docs/engine/design-custom-distros.md).
type Variant string

const (
	VariantNone    Variant = ""        // plain Debian, or not Debian
	VariantTrueNAS Variant = "truenas" // TrueNAS (SCALE, Community Edition): hardn refuses every change
	VariantRaspiOS Variant = "raspios" // Raspberry Pi OS, 64-bit; the 32-bit image says ID=raspbian
)

// Name is v's name in the user's words; "" for none.
func (v Variant) Name() string {
	switch v {
	case VariantTrueNAS:
		return "TrueNAS"
	case VariantRaspiOS:
		return "Raspberry Pi OS"
	}
	return ""
}

// The custom distributions' signals, read and never run. TrueNAS takes the
// execute bit off apt and dpkg, so no package tool is asked there.
const (
	TrueNASDebug   = "/usr/local/bin/freenas-debug"
	TrueNASRootfs  = "/usr/local/libexec/disable-rootfs-protection"
	KernelRelease  = "/proc/sys/kernel/osrelease" // uname -r's release
	TrueNASKernel  = "+truenas"                   // the end of TrueNAS's kernel release
	RpiIssue       = "/etc/rpi-issue"             // written by pi-gen and rpi-image-gen
	RpiArchive     = "archive.raspberrypi.com"
	RpiSourcesList = "/etc/apt/sources.list.d/raspi.list"
	RpiSources     = "/etc/apt/sources.list.d/raspi.sources"
	RpiSysModsPkg  = "raspberrypi-sys-mods"
)

// Custom is what the host says about TrueNAS and Raspberry Pi OS. Detect
// reads it on a host whose os-release says debian and that is not Proxmox
// VE; elsewhere it is the zero value.
type Custom struct {
	// TrueNAS is Yes when any of its signals is there, No when none is,
	// Unknown when one could not be read and none is there.
	TrueNAS Tri
	// RaspiOS is read as TrueNAS is, where TrueNAS is No; Unknown, with no
	// error, where TrueNAS is not No.
	RaspiOS Tri
	Source  string // the signal that said Yes; "" for none
	Err     error  // why TrueNAS or RaspiOS is Unknown
}

// Variant is the custom distribution c establishes: TrueNAS, Raspberry Pi
// OS, or none, with false where TrueNAS is Unknown. Raspberry Pi OS
// Unknown is none: it only adds warnings, so it refuses nothing.
func (c Custom) Variant() (Variant, bool) {
	switch {
	case c.TrueNAS == Yes:
		return VariantTrueNAS, true
	case c.TrueNAS == Unknown:
		return VariantNone, false
	case c.RaspiOS == Yes:
		return VariantRaspiOS, true
	}
	return VariantNone, true
}

// signal is one check: Yes, No, or Unknown with the error.
type signal struct {
	name  string
	check func(context.Context, host.System) (Tri, error)
}

// DetectCustom reads TrueNAS's signals, then Raspberry Pi OS's, each in
// order, stopping at the first Yes. TrueNAS: freenas-debug,
// disable-rootfs-protection, a kernel release ending "+truenas" (only where
// container is No: a container shares the host's kernel, so a Debian
// container on TrueNAS is not TrueNAS). Raspberry Pi OS: /etc/rpi-issue, an
// APT source on archive.raspberrypi.com (raspi.list or raspi.sources),
// dpkg's raspberrypi-sys-mods.
func DetectCustom(ctx context.Context, sys host.System, container Tri) Custom {
	var c Custom
	var errs []error
	truenas := []signal{
		{TrueNASDebug, regular(TrueNASDebug)},
		{TrueNASRootfs, regular(TrueNASRootfs)},
	}
	if container == No {
		truenas = append(truenas, signal{KernelRelease, truenasKernel})
	}
	c.TrueNAS, c.Source, errs = anyOf(ctx, sys, truenas, errs)
	if c.TrueNAS == No {
		c.RaspiOS, c.Source, errs = anyOf(ctx, sys, []signal{
			{RpiIssue, regular(RpiIssue)},
			{RpiSourcesList, aptSource(RpiSourcesList)},
			{RpiSources, aptSource(RpiSources)},
			{"dpkg-query: " + RpiSysModsPkg + " installed", func(ctx context.Context, sys host.System) (Tri, error) {
				return DpkgInstalled(ctx, sys, RpiSysModsPkg)
			}},
		}, errs)
	}
	if err := errors.Join(errs...); err != nil {
		c.Err = fmt.Errorf("detect: custom: %w", err)
	}
	return c
}

// anyOf runs the checks in turn: Yes at the first Yes, with its name; No
// when every one is No; Unknown otherwise, the errors added to errs.
func anyOf(ctx context.Context, sys host.System, checks []signal, errs []error) (Tri, string, []error) {
	t := No
	var mine []error
	for _, s := range checks {
		switch got, err := s.check(ctx, sys); got {
		case Yes:
			return Yes, s.name, errs
		case Unknown:
			t = Unknown
			mine = append(mine, err)
		}
	}
	return t, "", append(errs, mine...)
}

// regular checks for a regular file at name.
func regular(name string) func(context.Context, host.System) (Tri, error) {
	return func(ctx context.Context, sys host.System) (Tri, error) {
		fi, t, err := stat(ctx, sys, name)
		if t == Yes && !fi.Mode().IsRegular() {
			return No, nil
		}
		return t, err
	}
}

// truenasKernel reads the kernel release for TrueNAS's suffix.
func truenasKernel(ctx context.Context, sys host.System) (Tri, error) {
	data, err := sys.ReadFile(ctx, KernelRelease)
	switch {
	case err == nil:
		return known(strings.HasSuffix(firstLine(data), TrueNASKernel)), nil
	case missing(err):
		return No, nil
	}
	return Unknown, err
}

// aptSource reads the APT source file name for the Pi's archive, on a
// line that is not a comment.
func aptSource(name string) func(context.Context, host.System) (Tri, error) {
	return func(ctx context.Context, sys host.System) (Tri, error) {
		data, err := sys.ReadFile(ctx, name)
		switch {
		case missing(err):
			return No, nil
		case err != nil:
			return Unknown, err
		}
		for _, line := range strings.Split(string(data), "\n") {
			if line = strings.TrimSpace(line); !strings.HasPrefix(line, "#") && strings.Contains(line, RpiArchive) {
				return Yes, nil
			}
		}
		return No, nil
	}
}

func known(b bool) Tri {
	if b {
		return Yes
	}
	return No
}
