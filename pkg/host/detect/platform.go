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
	// Rocky Linux and AlmaLinux, Enterprise Linux (EL) rebuilds. The matrix
	// has no row for either yet, so both are Unlisted: hardn reads them and
	// refuses every change (docs/engine/el-readonly.md).
	DistroRocky Distro = "rocky"     // os-release ID
	DistroAlma  Distro = "almalinux" // os-release ID
	DistroOther Distro = "other"     // an os-release ID hardn does not know
)

// EL reports Rocky Linux or AlmaLinux: dnf, SELinux, firewalld or none,
// NetworkManager, sshd.service.
func (d Distro) EL() bool { return d == DistroRocky || d == DistroAlma }

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
	// for Alpine, VERSION_ID's major for Rocky Linux and AlmaLinux (their
	// minor releases are updates of one major, which the vendor supports
	// as a whole), and pve-manager's major version for Proxmox VE.
	Version string
	Support Support
	Note    string
}

// matrix is brief §2's decision "support what each vendor still supports;
// test the newest two releases of each" (2026-10-16). Ubuntu interim releases
// are out of scope by rule, not by row (ubuntuInterim). Alpine 3.21 is out
// of scope since its vendor's support ended, as Proxmox VE 8 is (Phase 7
// survey decision 5, adopted 2026-11-04), and so is Alpine 3.20, whose
// support ended first, given a row so that its refusal says why (the
// maintainer, 2026-11-08: "Out of scope + fixes"). Supported says hardn
// changes a release, not that a VM run covered it.
var matrix = []Release{
	{DistroDebian, "13", Supported, ""},
	{DistroDebian, "12", Supported, ""},
	{DistroUbuntu, "26.04", Supported, ""},
	{DistroUbuntu, "24.04", Supported, ""},
	{DistroAlpine, "3.24", Supported, ""},
	{DistroAlpine, "3.23", Supported, ""},
	{DistroProxmox, "9", Supported, ""},
	{DistroUbuntu, "22.04", BestEffort, ""},
	{DistroAlpine, "3.22", BestEffort, ""},
	{DistroAlpine, "3.21", OutOfScope, "vendor support ended 2026-11-01"},
	{DistroAlpine, "3.20", OutOfScope, "vendor support ended 2026-04-01"},
	{DistroProxmox, "8", OutOfScope, "end of life since 2026-08-31"},
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
	Err     error // why Distro or Support is Unknown
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
			p.Support, p.Note = Unlisted, "VERSION_ID "+osr.VersionID+" is not a stable release's x.y.z"
		}
		return p
	case DistroRocky, DistroAlma:
		return lookup(Platform{Distro: d, Version: elMajor(osr.VersionID)})
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
			p.Support, p.Note = r.Support, r.Note
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

// elMajor is "10" for Rocky Linux's and AlmaLinux's "10.2": a VERSION_ID of
// two numeric components, as both write it. Anything else is "".
func elMajor(v string) string {
	major, minor, ok := strings.Cut(v, ".")
	if !ok || !digits(major) || !digits(minor) {
		return ""
	}
	return major
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
