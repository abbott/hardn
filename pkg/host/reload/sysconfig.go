package reload

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"github.com/abbott/hardn/pkg/host"
)

// SysconfigSSHD is the environment file Rocky Linux's and AlmaLinux's
// sshd.service reads: its OPTIONS reach sshd's command line (ExecStart=
// /usr/sbin/sshd -D $OPTIONS), which sshd -T over the files does not see.
const SysconfigSSHD = "/etc/sysconfig/sshd"

// ErrSysconfig: /etc/sysconfig/sshd gives sshd arguments, so its effective
// configuration is not what sshd -T reads from the files.
var ErrSysconfig = errors.New("sysconfig sets sshd's arguments")

// CheckSysconfig refuses, with ErrSysconfig, an /etc/sysconfig/sshd whose
// OPTIONS is not empty, as systemd reads an environment file
// (systemd.exec(5), EnvironmentFile=): KEY=VALUE lines, # and ; comments,
// the value's surrounding quotes removed. The stock file sets OPTIONS=""
// (rocky10-test-110). A missing file gives no arguments. The readers that
// run sshd -T for the status rows call it where detect found sshd.service
// (detect.SSH.SSHDUnits), as they call OpenRCFlavour on OpenRC.
func CheckSysconfig(ctx context.Context, sys host.System) error {
	data, err := sys.ReadFile(ctx, SysconfigSSHD)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil
	case err != nil:
		return fmt.Errorf("reload: %w", err)
	}
	for i, l := range strings.Split(string(data), "\n") {
		l = strings.TrimSpace(l)
		if l == "" || l[0] == '#' || l[0] == ';' {
			continue
		}
		k, v, ok := strings.Cut(l, "=")
		if !ok || strings.TrimSpace(k) != "OPTIONS" {
			continue
		}
		v = strings.TrimSpace(v)
		if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
			v = strings.TrimSpace(v[1 : len(v)-1])
		}
		if v != "" {
			return fmt.Errorf("reload: %s line %d: OPTIONS %q: %w", SysconfigSSHD, i+1, v, ErrSysconfig)
		}
	}
	return nil
}
