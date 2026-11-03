package secondary

import (
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/domain/model"
	"github.com/abbott/hardn/pkg/interfaces"
)

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
