package engine

import "github.com/abbott/hardn/pkg/host/detect"

// CodeBestEffort is the finding of a plan made on a best-effort release
// (detect.BestEffort: Ubuntu 22.04, Alpine 3.22 and 3.21): one its vendor
// still supports and hardn changes, but is not tested on. It is stable
// once released; p3-output's registry holds it (P-41, decided by the
// maintainer on 2026-10-19: such releases are changed, with this warning
// in every plan; next/p3-confirm-cleanup).
const CodeBestEffort = "PLATFORM_BEST_EFFORT"

// distroNames are the platforms' names in a finding's words.
var distroNames = map[detect.Distro]string{
	detect.DistroDebian:  "Debian",
	detect.DistroUbuntu:  "Ubuntu",
	detect.DistroAlpine:  "Alpine",
	detect.DistroProxmox: "Proxmox VE",
}

// platformFindings are the plan's findings of the platform p: on a
// best-effort release, CodeBestEffort, whatever the plan holds (P-41: every
// plan made on one); none on a supported release. The support gate is
// unchanged: out-of-scope, unlisted and unestablished releases are refused
// for a plan with changes (refuseApply, ErrUnsupported), and get no such
// finding.
func platformFindings(p detect.Platform) []Finding {
	if p.Support != detect.BestEffort {
		return nil
	}
	name := distroNames[p.Distro]
	if name == "" {
		name = string(p.Distro)
	}
	detail := name + " " + p.Version + " is a best-effort release: its vendor still supports it and hardn changes it, but hardn is not tested on it"
	if p.Note != "" {
		detail += " (" + p.Note + ")"
	}
	return []Finding{{Code: CodeBestEffort, Detail: detail}}
}
