package sshd

import (
	"context"
	"errors"
	"io/fs"
	"strings"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/alpine"
	"github.com/abbott/hardn/pkg/host/detect"
)

// NoSSHD reports whether err, a read of sshd (ReadEffective), establishes
// that no sshd can run here: its binary not found (host.ErrNotFound), or
// no MainConfig, which sshd does not start without (sshd -t -f on a missing
// file: "No such file or directory", exit 1, on the Alpine 3.23 VM,
// 2026-11-07).
func NoSSHD(err error) bool {
	var pe *fs.PathError
	return errors.Is(err, host.ErrNotFound) || errors.As(err, &pe) && pe.Path == MainConfig && errors.Is(err, fs.ErrNotExist)
}

// serverBinaries are the SSH servers' binaries NoServer looks for in
// serverDirs: OpenSSH's sshd, Alpine's builds with PAM (reload.BinaryPAM,
// reload.BinaryKrb5), dropbear and tinysshd. Debian's packages install
// /usr/sbin/dropbear (dropbear-bin) and /usr/sbin/tinysshd (tinysshd),
// Alpine's under /usr too (their file lists and APKBUILDs, read
// 2026-11-07).
var serverBinaries = []string{"sshd", "sshd.pam", "sshd.krb5", "dropbear", "tinysshd"}

// serverDirs are where NoServer looks for serverBinaries: host.SearchPath,
// where the packages put them, and /usr/local's, where a server built from
// source goes by default.
const serverDirs = host.SearchPath + ":/usr/local/sbin:/usr/local/bin"

// dpkgServers are the SSH server packages NoServer asks dpkg for:
// OpenSSH's, dropbear's (dropbear holds its service, dropbear-bin the
// binary) and tinysshd. A package this list misses is still caught by its
// binary.
var dpkgServers = []string{"openssh-server", "dropbear", "dropbear-bin", "tinysshd"}

// apkServers are the SSH server packages NoServer looks for in Alpine's
// installed database: openssh-server's three builds, dropbear and tinyssh.
var apkServers = []string{alpine.OpenSSHServer, alpine.OpenSSHServerPAM, alpine.OpenSSHServerKrb5, "dropbear", "tinyssh"}

// NoServer reports whether the host is established to have no SSH server
// at all, with what it read: none of serverBinaries in serverDirs (each
// detect.FindToolIn No), and the package database read and holding none of
// the server packages: dpkg's on Debian, Ubuntu and Proxmox VE, dpkg-query
// itself found first (DpkgInstalled reads a missing dpkg-query as no
// package), Alpine's installed database. A binary or a package found, a
// read that fails, a status dpkg does not say plainly, and any other
// distribution are false. The SSH rows (Not Applicable, "no SSH server"),
// the Firewall row and the firewall's Enable read it, each only after a
// read of sshd that NoSSHD says failed for want of sshd. It changes nothing.
func NoServer(ctx context.Context, sys host.System, f detect.Facts) ([]string, bool) {
	for _, b := range serverBinaries {
		if detect.FindToolIn(ctx, sys, serverDirs, b).Present != detect.No {
			return nil, false
		}
	}
	ev := []string{"none of " + strings.Join(serverBinaries, ", ") + " in " + serverDirs}
	switch f.Platform.Distro {
	case detect.DistroDebian, detect.DistroUbuntu, detect.DistroProxmox:
		if detect.FindTool(ctx, sys, "dpkg-query").Present != detect.Yes {
			return nil, false
		}
		for _, p := range dpkgServers {
			if t, err := detect.DpkgInstalled(ctx, sys, p); t != detect.No || err != nil {
				return nil, false
			}
		}
		ev = append(ev, "dpkg-query: none of "+strings.Join(dpkgServers, ", ")+" installed")
	case detect.DistroAlpine:
		pkgs, err := alpine.ReadInstalled(ctx, sys)
		if err != nil {
			return nil, false
		}
		for _, p := range apkServers {
			if _, ok := pkgs[p]; ok {
				return nil, false
			}
		}
		ev = append(ev, alpine.InstalledDB+": none of "+strings.Join(apkServers, ", "))
	default:
		return nil, false
	}
	return ev, true
}
