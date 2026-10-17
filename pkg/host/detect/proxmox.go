package detect

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/abbott/hardn/pkg/host"
)

// Proxmox VE's signals.
const (
	PVEVersionBin = "/usr/bin/pveversion" // shipped by pve-manager; never run here (Perl and APT)
	PVEDir        = "/etc/pve"            // pmxcfs's mount point
	SelfMounts    = "/proc/self/mounts"
)

// pveShowFormat is dpkg-query's --showformat: the status ("want flag
// state"), a tab, the version. The backslash escapes are dpkg-query's own;
// no shell is involved.
const pveShowFormat = `${Status}\t${Version}\n`

// Proxmox is what the host says about Proxmox VE.
type Proxmox struct {
	// Installed is Yes when dpkg has pve-manager installed or
	// /usr/bin/pveversion exists, No when neither, Unknown otherwise.
	Installed Tri
	Version   string // pve-manager's version, from dpkg; "" when not established
	Source    string // the signal that decided Installed
	// ClusterFS is whether pmxcfs is mounted at /etc/pve, read from
	// /proc/self/mounts. A separate fact from Installed: /etc/pve's
	// contents (cluster.fw, priv/authorized_keys) exist only while it is.
	ClusterFS     Tri
	ClusterFSType string // the file system type mounted at /etc/pve, if any
	// EtcPVE is whether /etc/pve exists: a hint only, since pmxcfs leaves
	// the empty directory behind when it stops.
	EtcPVE Tri
	Err    error
}

// DetectProxmox asks dpkg about pve-manager, looks for pveversion, and reads
// the mount table for pmxcfs.
func DetectProxmox(ctx context.Context, sys host.System) Proxmox {
	var p Proxmox
	var errs [4]error
	dpkg, version, dpkgErr := pveManager(ctx, sys)
	fi, bin, binErr := stat(ctx, sys, PVEVersionBin)
	if bin == Yes && !fi.Mode().IsRegular() {
		bin = No
	}
	switch {
	case dpkg == Yes:
		p.Installed, p.Version, p.Source = Yes, version, "dpkg-query: pve-manager installed"
	case bin == Yes:
		p.Installed, p.Source = Yes, PVEVersionBin
	case dpkg == No && bin == No:
		p.Installed, p.Source = No, "dpkg-query and "+PVEVersionBin
	default:
		errs[0], errs[1] = dpkgErr, binErr
	}
	p.ClusterFS, p.ClusterFSType, errs[2] = pmxcfs(ctx, sys)
	p.EtcPVE, errs[3] = isDir(ctx, sys, PVEDir)
	if err := errors.Join(errs[:]...); err != nil {
		p.Err = fmt.Errorf("detect: proxmox: %w", err)
	}
	return p
}

// pveManager asks dpkg for pve-manager's status. A host without dpkg-query
// cannot have a dpkg package installed; exit 1 is dpkg-query's "no packages
// found" (dpkg-query(1), EXIT STATUS).
func pveManager(ctx context.Context, sys host.System) (Tri, string, error) {
	res, err := sys.Run(ctx, "dpkg-query", "--show", "--showformat="+pveShowFormat, "pve-manager")
	var xe *host.ExitError
	switch {
	case errors.Is(err, host.ErrNotFound):
		return No, "", nil
	case errors.As(err, &xe) && xe.ExitCode == 1:
		return No, "", nil
	case err != nil:
		return Unknown, "", err
	}
	status, version, _ := strings.Cut(firstLine(res.Stdout), "\t")
	f := strings.Fields(status)
	if len(f) == 3 {
		switch f[2] {
		case "installed":
			return Yes, version, nil
		case "not-installed", "config-files":
			return No, "", nil
		}
	}
	return Unknown, "", fmt.Errorf("dpkg-query: pve-manager status %q: %w", status, ErrUnrecognised)
}

// pmxcfs reads the mount table for a FUSE file system at /etc/pve, the last
// mount there being the one in effect. pmxcfs is FUSE; the exact type string
// it shows is not verified, so a FUSE type ("fuse", "fuse.<subtype>") is Yes
// and any other type at /etc/pve is Unknown.
func pmxcfs(ctx context.Context, sys host.System) (Tri, string, error) {
	data, err := sys.ReadFile(ctx, SelfMounts)
	if err != nil {
		return Unknown, "", err
	}
	fstype := ""
	for _, m := range ParseMounts(data) {
		if m.Dir == PVEDir {
			fstype = m.Type
		}
	}
	switch {
	case fstype == "":
		return No, "", nil
	case fstype == "fuse" || strings.HasPrefix(fstype, "fuse."):
		return Yes, fstype, nil
	}
	return Unknown, fstype, fmt.Errorf("%s mounted with type %q: %w", PVEDir, fstype, ErrUnrecognised)
}

// Mount is one line of /proc/<pid>/mounts.
type Mount struct {
	Source, Dir, Type, Options string
}

// ParseMounts parses /proc/<pid>/mounts, whose format is fstab(5)'s: fields
// separated by spaces, with space, tab, newline, backslash and # in a field
// written as an octal escape (\040). Lines with fewer than four fields are
// dropped.
func ParseMounts(data []byte) []Mount {
	var ms []Mount
	for _, line := range strings.Split(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) < 4 {
			continue
		}
		ms = append(ms, Mount{unescapeOctal(f[0]), unescapeOctal(f[1]), unescapeOctal(f[2]), unescapeOctal(f[3])})
	}
	return ms
}

func unescapeOctal(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+4 <= len(s) {
			if n, err := strconv.ParseUint(s[i+1:i+4], 8, 8); err == nil {
				b.WriteByte(byte(n))
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
