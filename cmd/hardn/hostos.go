package main

import (
	"context"
	"fmt"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/osdetect"
)

// osHost is the host hostOS reads: a seam for the tests.
var osHost = func() host.System { return host.NewReal(host.Options{}) }

// hostOS is the root command's OS check, past the root check and the
// config load (docs/engine/legacy-left.md, decision 4 (a), adopted
// 2026-11-04): pkg/host/detect's os-release and Proxmox VE facts, placed by
// Classify. No os-release, or one naming an OS other than Debian, Ubuntu
// or Alpine (detect.DistroOther), is refused, as osdetect.DetectOS refused
// them, in its words; Proxmox VE is Debian's. It says nothing of an OS it
// accepts, and clears nothing: the OS is on the main menu's context bar.
// The OSInfo is the legacy factories' (main.go), until they go.
func hostOS() (*osdetect.OSInfo, error) {
	ctx := context.Background()
	sys := osHost()
	osr := detect.ReadOSRelease(ctx, sys)
	if !osr.Known() {
		return nil, fmt.Errorf("cannot detect OS type: %w", osr.Err)
	}
	pve := detect.DetectProxmox(ctx, sys)
	if detect.Classify(osr, pve).Distro == detect.DistroOther {
		return nil, fmt.Errorf("unsupported OS type detected: %s", osr.ID)
	}
	info := &osdetect.OSInfo{OsType: osr.ID, OsVersion: osr.VersionID, OsCodename: osr.VersionCodename, IsProxmox: pve.Installed == detect.Yes}
	if osr.ID == string(detect.DistroAlpine) {
		info.OsCodename = osr.VersionID // as osdetect set it
	}
	return info, nil
}
