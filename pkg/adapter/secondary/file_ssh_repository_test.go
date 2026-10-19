package secondary

import (
	"errors"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/domain/model"
	"github.com/abbott/hardn/pkg/interfaces"
)

// disableRootCase runs FileSSHRepository.DisableRootSSH on the repo's mock
// file system and commander: nothing is read from or run on this host.
type disableRootCase struct {
	name     string
	osType   string
	seed     map[string]string // path -> content
	wantErr  string            // "" for success
	wantFile string            // the file expected to be written, or ""
	want     string            // its content
	restart  bool              // whether sshd is restarted
}

func TestDisableRootSSHInPlace(t *testing.T) {
	if stubDisableRoot {
		t.Fatal("stubDisableRoot is set: disable-root rebuilds the SSH config from a stub (V4)")
	}
	const dropIn = "/etc/ssh/sshd_config.d/hardn.conf"
	const main = "/etc/ssh/sshd_config"
	for _, tc := range []disableRootCase{
		{
			name: "debian edits hardn's drop-in, not sshd_config", osType: "debian",
			seed: map[string]string{
				main:   "Include /etc/ssh/sshd_config.d/*.conf\nPermitRootLogin yes\n",
				dropIn: "Port 2230\nListenAddress 192.0.2.10\nPermitRootLogin yes\nAllowUsers root alice\nAuthorizedKeysFile .ssh/authorized_keys .ssh_%u/authorized_keys\n",
			},
			wantFile: dropIn,
			want:     "Port 2230\nListenAddress 192.0.2.10\nPermitRootLogin no\nAllowUsers alice\nAuthorizedKeysFile .ssh/authorized_keys .ssh_%u/authorized_keys\n",
			restart:  true,
		},
		{
			name: "ubuntu creates the drop-in when there is none", osType: "ubuntu",
			seed:     map[string]string{main: "Include /etc/ssh/sshd_config.d/*.conf\nPort 2230\n"},
			wantFile: dropIn, want: "PermitRootLogin no\n", restart: true,
		},
		{
			name: "alpine edits sshd_config in place; sftp survives", osType: "alpine",
			seed:     map[string]string{main: "Port 2230\nAllowUsers root alice\nPermitRootLogin yes\nSubsystem sftp internal-sftp\nMatch User bob\n\tForceCommand internal-sftp\n"},
			wantFile: main,
			want:     "Port 2230\nAllowUsers alice\nPermitRootLogin no\nSubsystem sftp internal-sftp\nMatch User bob\n\tForceCommand internal-sftp\n",
			restart:  true,
		},
		{
			name: "alpine without sshd_config is an error", osType: "alpine",
			wantErr: "SSH config file /etc/ssh/sshd_config not found",
		},
		{
			name: "already disabled: nothing written, no restart", osType: "debian",
			seed: map[string]string{dropIn: "Port 2230\nPermitRootLogin no\nAllowUsers alice\n"},
		},
		{
			name: "AllowUsers root only is refused", osType: "debian",
			seed:    map[string]string{dropIn: "PermitRootLogin yes\nAllowUsers root\n"},
			wantErr: "refusing to disable root SSH login in " + dropIn + ": AllowUsers allows only root",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fs := interfaces.NewMockFileSystem()
			for p, c := range tc.seed {
				fs.Files[p] = []byte(c)
			}
			mc := interfaces.NewMockCommander()
			repo := NewFileSSHRepository(fs, mc, tc.osType, nil)

			err := repo.DisableRootSSH()
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err %v, want %q", err, tc.wantErr)
				}
			} else if err != nil {
				t.Fatal(err)
			}

			for p, c := range tc.seed {
				if p == tc.wantFile {
					continue
				}
				if got := string(fs.Files[p]); got != c {
					t.Errorf("%s changed:\n%s", p, got)
				}
			}
			if tc.wantFile != "" {
				if got := string(fs.Files[tc.wantFile]); got != tc.want {
					t.Errorf("%s:\n%s\nwant:\n%s", tc.wantFile, got, tc.want)
				}
			}
			wantFiles := map[string]bool{}
			for p := range tc.seed {
				wantFiles[p] = true
			}
			if tc.wantFile != "" {
				wantFiles[tc.wantFile] = true
			}
			if len(fs.Files) != len(wantFiles) {
				t.Errorf("files %v, want %v", keys(fs.Files), wantFiles)
			}
			commands := withoutProxmoxProbe(mc.ExecutedCommands)
			restarted := len(commands) > 0
			if restarted != tc.restart {
				t.Errorf("commands %v, want a restart: %v", commands, tc.restart)
			}
			if tc.restart {
				want := "systemctl restart ssh"
				if tc.osType == "alpine" {
					want = "rc-service sshd restart"
				}
				if strings.Join(commands, "; ") != want {
					t.Errorf("commands %v, want %q", commands, want)
				}
			}
		})
	}
}

// TestDisableRootSSHReadError checks that a file that cannot be read is
// an error, with nothing written.
func TestDisableRootSSHReadError(t *testing.T) {
	fs := interfaces.NewMockFileSystem()
	const dropIn = "/etc/ssh/sshd_config.d/hardn.conf"
	fs.Files[dropIn] = []byte("PermitRootLogin yes\n")
	fs.ReadFileError[dropIn] = errors.New("fixture: permission denied")
	mc := interfaces.NewMockCommander()
	err := NewFileSSHRepository(fs, mc, "debian", nil).DisableRootSSH()
	if err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("err %v", err)
	}
	if string(fs.Files[dropIn]) != "PermitRootLogin yes\n" || len(withoutProxmoxProbe(mc.ExecutedCommands)) != 0 {
		t.Errorf("file %q, commands %v", fs.Files[dropIn], mc.ExecutedCommands)
	}
}

// withoutProxmoxProbe drops the read-only pve-manager query (ProxmoxHost).
func withoutProxmoxProbe(commands []string) []string {
	var out []string
	for _, c := range commands {
		if !strings.HasPrefix(c, "dpkg-query ") {
			out = append(out, c)
		}
	}
	return out
}

func keys(m map[string][]byte) []string {
	var k []string
	for p := range m {
		k = append(k, p)
	}
	return k
}

// TestSaveSSHConfigAuthorizedKeysFile checks that SaveSSHConfig lists
// every key path on one AuthorizedKeysFile line: sshd uses the first such
// line it reads, so a second line would be ignored.
func TestSaveSSHConfigAuthorizedKeysFile(t *testing.T) {
	if authorizedKeysFileLinePerPath {
		t.Fatal("authorizedKeysFileLinePerPath is set: a second key path goes on a line sshd ignores (V24)")
	}
	for _, tc := range []struct {
		paths []string
		want  string
	}{
		{nil, "AuthorizedKeysFile .ssh/authorized_keys\n"},
		{[]string{".ssh_%u"}, "AuthorizedKeysFile .ssh_%u\n"},
		{[]string{".ssh/authorized_keys", ".ssh_%u/authorized_keys"}, "AuthorizedKeysFile .ssh/authorized_keys .ssh_%u/authorized_keys\n"},
	} {
		fs := interfaces.NewMockFileSystem()
		repo := NewFileSSHRepository(fs, interfaces.NewMockCommander(), "debian", nil)
		if err := repo.SaveSSHConfig(model.SSHConfig{Port: 2230, PermitRootLogin: false, KeyPaths: tc.paths}); err != nil {
			t.Fatal(err)
		}
		conf := string(fs.Files["/etc/ssh/sshd_config.d/hardn.conf"])
		if strings.Count(conf, "AuthorizedKeysFile") != 1 || !strings.HasSuffix(conf, tc.want) {
			t.Errorf("%q: hardn.conf ends\n%s\nwant one line %q", tc.paths, conf, tc.want)
		}
	}
}
