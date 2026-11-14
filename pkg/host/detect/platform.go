package detect

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// Distro is a platform hardn tells apart.
type Distro string

const (
	DistroUnknown Distro = ""        // not established
	DistroDebian  Distro = "debian"  // os-release ID
	DistroUbuntu  Distro = "ubuntu"  // os-release ID
	DistroAlpine  Distro = "alpine"  // os-release ID
	DistroProxmox Distro = "proxmox" // Proxmox VE: pve-manager installed; os-release says debian
	// Rocky Linux and AlmaLinux, Enterprise Linux (EL) rebuilds: 10 and 9
	// are supported for the changes docs/engine/el-changes.md lists; hardn
	// reads the rest (docs/engine/el-readonly.md).
	DistroRocky Distro = "rocky"     // os-release ID
	DistroAlma  Distro = "almalinux" // os-release ID
	// Red Hat Enterprise Linux, which Rocky Linux and AlmaLinux rebuild:
	// 10 and 9 supported for the same changes (docs/engine/el-rhel.md).
	DistroRHEL Distro = "rhel" // os-release ID
	// CentOS Stream, which RHEL's next minor release is built from, and
	// Oracle Linux, a RHEL rebuild: 10 and 9 supported for the same changes
	// (docs/engine/design-centos-oracle.md). CentOS Linux 8 and 7 say
	// "centos" too, and have no row.
	DistroCentOS Distro = "centos" // os-release ID
	DistroOracle Distro = "ol"     // os-release ID
	DistroOther  Distro = "other"  // an os-release ID hardn does not know
)

// EL reports Rocky Linux, AlmaLinux, RHEL, CentOS Stream or Oracle Linux:
// dnf, SELinux, firewalld or none, NetworkManager, sshd.service.
func (d Distro) EL() bool {
	switch d {
	case DistroRocky, DistroAlma, DistroRHEL, DistroCentOS, DistroOracle:
		return true
	}
	return false
}

// Support is a platform's place in the support matrix.
type Support uint8

const (
	SupportUnknown Support = iota // the platform or its version is not established
	Supported                     // supported: hardn changes it (which versions a VM run covered: the README's support table)
	BestEffort                    // still supported by its vendor; no promises
	OutOfScope                    // named out of scope: Ubuntu interim releases, Proxmox VE 8, Alpine 3.21 and 3.20
	Unlisted                      // a platform or version the matrix does not name
)

func (s Support) String() string {
	switch s {
	case Supported:
		return "supported"
	case BestEffort:
		return "best effort"
	case OutOfScope:
		return "out of scope"
	case Unlisted:
		return "unlisted"
	}
	return "unknown"
}

// Release is one row of the support matrix.
type Release struct {
	Distro Distro
	// Version is VERSION_ID for Debian and Ubuntu, VERSION_ID's major.minor
	// for Alpine, VERSION_ID's major for Rocky Linux, AlmaLinux, RHEL,
	// CentOS Stream and Oracle Linux (their minor releases are updates of
	// one major, which the vendor supports as a whole), and pve-manager's
	// major version for Proxmox VE.
	Version string
	Support Support
	Note    string
	// Ends is the last day of the vendor's free security updates, its
	// long-term support included and paid support left out
	// (docs/engine/gallery-eol.md, decision 1): a day, "2027-05-01", or,
	// where the vendor names a month alone, the month, "2027-05", which
	// lasts to its last day. "" where the vendor names none (Proxmox VE 9).
	// Platform.End reads it.
	Ends string
}

// matrix is brief §2's decision "support what each vendor still supports;
// test the newest two releases of each" (2026-10-16). Ubuntu interim releases
// are out of scope by rule, not by row (ubuntuInterim). Alpine 3.21 is out
// of scope since its vendor's support ended, as Proxmox VE 8 is (Phase 7
// survey decision 5, adopted 2026-11-04), and so is Alpine 3.20, whose
// support ended first, given a row so that its refusal says why (the
// maintainer, 2026-11-08: "Out of scope + fixes"). Supported says hardn
// changes a release, not that a VM run covered it.
//
// Each row's Ends is its vendor's date (each page read 2026-11-13;
// docs/engine/gallery-eol.md §3):
//   - Debian: the end of LTS, https://wiki.debian.org/LTS
//   - Ubuntu: the end of standard security maintenance, a month,
//     https://ubuntu.com/about/release-cycle
//   - Alpine: "End of support", https://alpinelinux.org/releases/
//   - Proxmox VE: "Proxmox EOL", a month, "tba" for 9,
//     https://pve.proxmox.com/wiki/FAQ
//   - Rocky Linux: security support, https://docs.rockylinux.org/releases/
//   - AlmaLinux: security support, https://wiki.almalinux.org/release-notes/
//   - RHEL: the end of maintenance support,
//     https://access.redhat.com/support/policy/updates/errata
//
// CentOS Stream's and Oracle Linux's (each read 2026-11-14):
//   - CentOS Stream 9: "End of Life", https://www.centos.org/cl-vs-cs/
//   - CentOS Stream 10: CentOS names the year, 2030, and ties the day to
//     the end of RHEL 10's full support, which Red Hat ends on 2030-05-31
//     (https://blog.centos.org/2024/12/introducing-centos-stream-10/ and
//     RHEL's page above)
//   - Oracle Linux: the end of Premier Support, a month,
//     https://www.oracle.com/a/ocom/docs/elsp-lifetime-069338.pdf
//
// A distribution's supported rows are newest first (Newest).
var matrix = []Release{
	{DistroDebian, "13", Supported, "", "2030-06-30"},
	{DistroDebian, "12", Supported, "", "2028-06-30"},
	{DistroUbuntu, "26.04", Supported, "", "2031-05"},
	{DistroUbuntu, "24.04", Supported, "", "2029-05"},
	{DistroAlpine, "3.24", Supported, "", "2028-06-01"},
	{DistroAlpine, "3.23", Supported, "", "2027-11-01"},
	{DistroProxmox, "9", Supported, "", ""},
	// Rocky Linux and AlmaLinux: every change but DNS (el-changes.md,
	// el-ports.md, el-firewalld.md, el-updates.md). VM runs covered Rocky
	// Linux 10 alone.
	{DistroRocky, "10", Supported, "", "2035-05-31"},
	{DistroRocky, "9", Supported, "", "2032-05-31"},
	{DistroAlma, "10", Supported, "", "2035-05-31"},
	{DistroAlma, "9", Supported, "", "2032-05-31"},
	// RHEL: as Rocky Linux and AlmaLinux; the host kit ran on RHEL 10.3 and
	// 9.9 (el-rhel.md §7).
	{DistroRHEL, "10", Supported, "", "2035-05-31"},
	{DistroRHEL, "9", Supported, "", "2032-05-31"},
	// CentOS Stream and Oracle Linux: as Rocky Linux, AlmaLinux and RHEL;
	// the host kit ran on CentOS Stream 10 and 9 and on Oracle Linux 10.1
	// and 9.8 (design-centos-oracle.md).
	{DistroCentOS, "10", Supported, "", "2030-05-31"},
	{DistroCentOS, "9", Supported, "", "2027-05-31"},
	{DistroOracle, "10", Supported, "", "2035-06"},
	{DistroOracle, "9", Supported, "", "2032-06"},
	{DistroUbuntu, "22.04", BestEffort, "", "2027-05"},
	{DistroAlpine, "3.22", BestEffort, "", "2027-05-01"},
	{DistroAlpine, "3.21", OutOfScope, "vendor support ended 2026-11-01", "2026-11-01"},
	{DistroAlpine, "3.20", OutOfScope, "vendor support ended 2026-04-01", "2026-04-01"},
	{DistroProxmox, "8", OutOfScope, "end of life since 2026-08-31", "2026-08"},
}

// Matrix returns the support matrix, a copy.
func Matrix() []Release { return slices.Clone(matrix) }

// Platform is the host's platform and its support level.
type Platform struct {
	Distro Distro
	// Like is, for DistroOther, the first ID_LIKE entry hardn knows (debian,
	// ubuntu or alpine): what a derivative is built from, never a claim
	// that it is supported.
	Like    Distro
	Version string // the matrix key (Release.Version); "" when not established
	Support Support
	Note    string
	// Ends is the matrix row's (Release.Ends); "" for a release with no
	// row, or whose vendor names no date.
	Ends string
	Err  error // why Distro or Support is Unknown
}

// Classify places a host in the support matrix from its os-release and its
// Proxmox VE facts. Proxmox VE keeps Debian's os-release, so a Debian ID with
// Proxmox not established is not established either. Classify is pure.
func Classify(osr OSRelease, pve Proxmox) Platform {
	if !osr.Known() {
		return Platform{Err: errOr(osr.Err, fmt.Errorf("detect: platform: os-release has no ID: %w", ErrNoSignal))}
	}
	switch {
	case pve.Installed == Yes:
		p := Platform{Distro: DistroProxmox, Version: pveMajor(pve.Version)}
		switch {
		case pve.Version == "":
			p.Err = fmt.Errorf("detect: platform: Proxmox VE version not established (%s): %w", pve.Source, errOr(pve.Err, ErrNoSignal))
		case p.Version == "":
			p.Err = fmt.Errorf("detect: platform: Proxmox VE version %q: %w", pve.Version, ErrUnrecognised)
		default:
			return lookup(p)
		}
		return p
	case pve.Installed == Unknown && osr.ID == string(DistroDebian):
		return Platform{Err: fmt.Errorf("detect: platform: Debian or Proxmox VE: %w", errOr(pve.Err, ErrNoSignal))}
	}
	switch d := Distro(osr.ID); d {
	case DistroDebian, DistroUbuntu:
		return lookup(Platform{Distro: d, Version: osr.VersionID})
	case DistroAlpine:
		p := lookup(Platform{Distro: d, Version: majorMinor(osr.VersionID)})
		if p.Version != "" && !alpineStable(osr.VersionID) {
			p.Support, p.Note, p.Ends = Unlisted, "VERSION_ID "+osr.VersionID+" is not a stable release's x.y.z", ""
		}
		return p
	case DistroRocky, DistroAlma, DistroRHEL, DistroOracle:
		return lookup(Platform{Distro: d, Version: elMajor(osr.VersionID)})
	case DistroCentOS:
		return lookup(Platform{Distro: d, Version: centosMajor(osr.VersionID)})
	}
	p := Platform{Distro: DistroOther, Support: Unlisted}
	for _, id := range osr.IDLike {
		if d := Distro(id); d == DistroDebian || d == DistroUbuntu || d == DistroAlpine {
			p.Like = d
			break
		}
	}
	return p
}

func lookup(p Platform) Platform {
	for _, r := range matrix {
		if r.Distro == p.Distro && r.Version == p.Version {
			p.Support, p.Note, p.Ends = r.Support, r.Note, r.Ends
			return p
		}
	}
	p.Support = Unlisted
	if p.Distro == DistroUbuntu && ubuntuInterim(p.Version) {
		p.Support = OutOfScope
	}
	return p
}

// ubuntuInterim reports a YY.MM release that is not an LTS: Ubuntu's LTS
// releases are every two years, in April of even years (22.04, 24.04, 26.04).
func ubuntuInterim(v string) bool {
	yy, mm, ok := strings.Cut(v, ".")
	y, yerr := strconv.Atoi(yy)
	_, merr := strconv.Atoi(mm)
	if !ok || yerr != nil || merr != nil || len(yy) != 2 || len(mm) != 2 {
		return false
	}
	return mm != "04" || y%2 != 0
}

// majorMinor is "3.24" for Alpine's "3.24.2" (and "3.25.0_alpha…" on edge).
func majorMinor(v string) string {
	parts := strings.SplitN(v, ".", 3)
	if len(parts) < 2 || !digits(parts[0]) || !digits(parts[1]) {
		return ""
	}
	return parts[0] + "." + parts[1]
}

// alpineStable reports a VERSION_ID of three numeric components, the form a
// stable branch's alpine-base writes ("3.24.2"). A suffixed one, as edge's
// "3.25.0_alpha20261001" or a release candidate's, is not a release the
// matrix names even when its major.minor is a row.
func alpineStable(v string) bool {
	parts := strings.Split(v, ".")
	return len(parts) == 3 && digits(parts[0]) && digits(parts[1]) && digits(parts[2])
}

// elMajor is "10" for Rocky Linux's and AlmaLinux's "10.2", RHEL's "10.3"
// and Oracle Linux's "10.1": a VERSION_ID of two numeric components, as
// they write it. Anything else is "".
func elMajor(v string) string {
	major, minor, ok := strings.Cut(v, ".")
	if !ok || !digits(major) || !digits(minor) {
		return ""
	}
	return major
}

// centosMajor is "10" for CentOS Stream's "10", whose VERSION_ID is the
// major alone; a major.minor is read as elMajor reads it.
func centosMajor(v string) string {
	if digits(v) {
		return v
	}
	return elMajor(v)
}

// pveMajor is "9" for pve-manager's "9.2.22".
func pveMajor(v string) string {
	if major, _, _ := strings.Cut(v, "."); digits(major) {
		return major
	}
	return ""
}

func digits(s string) bool {
	_, err := strconv.ParseUint(s, 10, 32)
	return err == nil
}

func errOr(err, fallback error) error {
	if err != nil {
		return err
	}
	return fallback
}
