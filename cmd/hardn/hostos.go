package main

import (
	"context"
	"fmt"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
)

// osHost is the host hostOS reads: a seam for the tests.
var osHost = func() host.System { return host.NewReal(host.Options{}) }

// hostOS is the root command's OS check, past the root check and the
// config load (docs/engine/legacy-left.md, decision 4 (a), adopted
// 2026-11-04): pkg/host/detect's os-release and Proxmox VE facts, placed by
// Classify. No os-release, or one naming an OS other than Debian, Ubuntu,
// Alpine, Rocky Linux, AlmaLinux or RHEL (detect.DistroOther), is refused;
// Proxmox VE is Debian's. Rocky Linux, AlmaLinux and RHEL are placed by
// the matrix as the others are (docs/engine/el-changes.md; RHEL, which it
// doesn't name yet, is read only: docs/engine/el-rhel.md). It
// says nothing of an OS it accepts, and clears nothing: the OS is on the
// main menu's context bar.
func hostOS() error {
	ctx := context.Background()
	sys := osHost()
	osr := detect.ReadOSRelease(ctx, sys)
	if !osr.Known() {
		return fmt.Errorf("cannot detect OS type: %w", osr.Err)
	}
	if detect.Classify(osr, detect.DetectProxmox(ctx, sys)).Distro == detect.DistroOther {
		return fmt.Errorf("unsupported OS type detected: %s", osr.ID)
	}
	return nil
}
