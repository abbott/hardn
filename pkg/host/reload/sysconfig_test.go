package reload_test

import (
	"context"
	"errors"
	"io/fs"
	"testing"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/reload"
)

// TestCheckSysconfig: /etc/sysconfig/sshd's OPTIONS reaches sshd's command
// line on Rocky Linux and AlmaLinux, so anything in it refuses the read;
// the stock file (rocky10-test-110's, OPTIONS="") and none do not.
func TestCheckSysconfig(t *testing.T) {
	stock := "# Configuration file for the sshd service.\n\n#SSH_RSA_BITS=3072\n#SSH_ECDSA_BITS=256\nOPTIONS=\"\"\n"
	for _, c := range []struct {
		name string
		data *string
		want error
	}{
		{"none", nil, nil},
		{"stock", &stock, nil},
		{"single quotes, empty", ptr("OPTIONS=''\n"), nil},
		{"quoted blanks", ptr("OPTIONS = \"  \"\n"), nil},
		{"commented", ptr("#OPTIONS=\"-p 2222\"\n; OPTIONS=-p2222\n"), nil},
		{"another variable", ptr("SSH_RSA_BITS=4096\n"), nil},
		{"an option", ptr("OPTIONS=\"-o PasswordAuthentication=yes\"\n"), reload.ErrSysconfig},
		{"unquoted", ptr("OPTIONS=-p2222\n"), reload.ErrSysconfig},
		{"set, then emptied", ptr("OPTIONS=\"-u0\"\nOPTIONS=\"\"\n"), reload.ErrSysconfig},
		{"indented", ptr("  OPTIONS='-f /etc/ssh/other_config'\n"), reload.ErrSysconfig},
		{"spaced around =", ptr("OPTIONS = -p 2222\n"), reload.ErrSysconfig},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := hostfake.New(host.Options{})
			if c.data != nil {
				f.AddFile(reload.SysconfigSSHD, []byte(*c.data), 0o644, 0, 0)
			}
			if err := reload.CheckSysconfig(context.Background(), f); !errors.Is(err, c.want) || (c.want == nil) != (err == nil) {
				t.Errorf("err %v, want %v", err, c.want)
			}
			if len(f.Runs()) != 0 {
				t.Errorf("ran %q", f.Runs())
			}
		})
	}
	f := deniedSys{hostfake.New(host.Options{})}
	if err := reload.CheckSysconfig(context.Background(), f); !errors.Is(err, fs.ErrPermission) || errors.Is(err, reload.ErrSysconfig) {
		t.Errorf("unreadable: err %v, want a permission error", err)
	}
}

func ptr(s string) *string { return &s }

// deniedSys fails every read with a permission error.
type deniedSys struct{ *hostfake.System }

func (deniedSys) ReadFile(context.Context, string) ([]byte, error) { return nil, fs.ErrPermission }
