package reload

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/sshd"
)

// The sshd binaries Alpine's init script picks between (aports
// main/openssh/sshd.initd, update_command), and the settings file OpenRC
// reads before it.
const (
	BinarySSHD  = "/usr/sbin/sshd"
	BinaryPAM   = "/usr/sbin/sshd.pam"
	BinaryKrb5  = "/usr/sbin/sshd.krb5"
	OpenRCConfd = "/etc/conf.d/sshd"
)

// ErrConfd: /etc/conf.d/sshd names a binary, a configuration file or sshd
// arguments other than the init script's defaults, which hardn does not
// model.
var ErrConfd = errors.New("conf.d sets the sshd binary, configuration file or arguments")

// confdVars change the binary, the configuration file or the arguments the
// init script uses (sshd.initd: command, cfgfile, and command_args, which
// defaults to SSHD_OPTS and reaches both "$command" -t and the daemon, so a
// -f or -o there changes what sshd reads).
var confdVars = []string{"SSHD_BINARY", "SSHD_CONFIG", "SSHD_CONFDIR", "cfgfile", "SSHD_OPTS", "command_args"}

// OpenRCFlavour returns the sshd binary Alpine's init script runs for the
// installed files, for sshd.Validate's and sshd.ReadEffective's Binary: as
// update_command picks it, sshd.krb5 when it is executable and its own -G
// prints KerberosAuthentication or GSSAPIAuthentication yes, else sshd.pam
// when executable with UsePAM yes, else sshd. Where the script ignores a
// failing -G, OpenRCFlavour returns the error. It refuses, with ErrConfd, an
// /etc/conf.d/sshd that sets the binary, the configuration file or the
// arguments.
func OpenRCFlavour(ctx context.Context, sys host.System) (string, error) {
	switch data, err := sys.ReadFile(ctx, OpenRCConfd); {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return "", fmt.Errorf("reload: %w", err)
	default:
		for i, l := range strings.Split(string(data), "\n") {
			l = strings.TrimSpace(l)
			for _, v := range confdVars {
				if l != "" && l[0] != '#' && strings.Contains(l, v) {
					return "", fmt.Errorf("reload: %s line %d: %s: %w", OpenRCConfd, i+1, v, ErrConfd)
				}
			}
		}
	}
	for _, f := range []struct {
		bin   string
		lines []string
	}{
		{BinaryKrb5, []string{"kerberosauthentication yes", "gssapiauthentication yes"}},
		{BinaryPAM, []string{"usepam yes"}},
	} {
		fi, err := sys.Stat(ctx, f.bin)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			continue
		case err != nil:
			return "", fmt.Errorf("reload: %w", err)
		case fi.IsDir() || fi.Mode().Perm()&0o111 == 0:
			continue
		}
		res, err := sys.Run(ctx, f.bin, "-f", sshd.MainConfig, "-G")
		if err != nil {
			return "", fmt.Errorf("reload: %s -f %s -G: %w", f.bin, sshd.MainConfig, err)
		}
		for _, l := range strings.Split(string(res.Stdout), "\n") {
			for _, w := range f.lines {
				if strings.EqualFold(strings.TrimSuffix(l, "\r"), w) {
					return f.bin, nil
				}
			}
		}
	}
	return BinarySSHD, nil
}
