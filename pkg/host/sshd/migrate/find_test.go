package migrate

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/host/sshd"
)

// TestFind recognises each legacy file by the lines hardn wrote, on hosts
// with every combination the side-effect inventory lists (side-effects.md
// §10), and leaves what hardn did not write, or sshd does not read.
func TestFind(t *testing.T) {
	manage, listen := fixture(t, "v032-manage.conf"), fixture(t, "v032-listen.conf")
	hardnR, hardnC := fixture(t, "main-hardn-r.conf"), fixture(t, "main-hardn-c.conf")
	tests := []struct {
		name       string
		files      map[string]string
		links      map[string]string
		configFile string
		want       []string // kind:path:generation[:ports]
		left       []string // path: a word of why
	}{
		{name: "stock host", files: map[string]string{sshd.MainConfig: stockMain}},
		{name: "v0.3.2 host", configFile: ManageConf,
			files: map[string]string{sshd.MainConfig: stockMain, ManageConf: manage, ListenConf: listen},
			want:  []string{"manage.conf:" + ManageConf + ":hardn v0.3.x", "listen.conf:" + ListenConf + ":hardn v0.3.x:[2230]"}},
		{name: "v0.3.2 upgraded to an unreleased build", configFile: HardnConf,
			files: map[string]string{sshd.MainConfig: stockMain, ManageConf: manage, HardnConf: hardnR, ListenConf: listen},
			want: []string{"manage.conf:" + ManageConf + ":hardn v0.3.x", "hardn.conf:" + HardnConf + ":an unreleased hardn build",
				"listen.conf:" + ListenConf + ":hardn v0.3.x:[2230]"}},
		{name: "unreleased build, -c", files: map[string]string{sshd.MainConfig: stockMain, HardnConf: hardnC},
			want: []string{"hardn.conf:" + HardnConf + ":an unreleased hardn build"}},
		{name: "custom sshConfigFile drop-in", configFile: sshd.DropInDir + "/10-site-hardn.conf",
			files: map[string]string{sshd.MainConfig: stockMain, sshd.DropInDir + "/10-site-hardn.conf": manage},
			want:  []string{"sshConfigFile:" + sshd.DropInDir + "/10-site-hardn.conf:hardn v0.3.x"}},
		{name: "custom sshConfigFile sshd does not read", configFile: "/etc/ssh/hardn_sshd.conf",
			files: map[string]string{sshd.MainConfig: stockMain, "/etc/ssh/hardn_sshd.conf": manage},
			left:  []string{"/etc/ssh/hardn_sshd.conf: does not read"}},
		{name: "custom sshConfigFile not absolute", configFile: "sshd_config.d/x.conf",
			files: map[string]string{sshd.MainConfig: stockMain}, left: []string{"sshd_config.d/x.conf: absolute"}},
		{name: "custom sshConfigFile is sshd_config", configFile: sshd.MainConfig,
			files: map[string]string{sshd.MainConfig: manage}, left: []string{sshd.MainConfig + ": over sshd_config"}},
		{name: "files at the legacy paths that hardn did not write",
			files: map[string]string{sshd.MainConfig: stockMain, ManageConf: "PermitRootLogin no\n", ListenConf: "[Socket]\nListenStream=\nListenStream=0.0.0.0:2230\n"},
			left:  []string{ManageConf + ": not hardn's", ListenConf + ": not the form"}},
		{name: "listen.conf out of range", files: map[string]string{sshd.MainConfig: stockMain, ListenConf: "[Socket]\nListenStream=\nListenStream=70000\n"},
			left: []string{ListenConf + ": not the form"}},
		{name: "masked listen.conf", files: map[string]string{sshd.MainConfig: stockMain}, links: map[string]string{ListenConf: "/dev/null"},
			left: []string{ListenConf + ": symlink"}},
		{name: "listen.conf a directory", files: map[string]string{sshd.MainConfig: stockMain, ListenConf + "/x": ""},
			left: []string{ListenConf + ": not a regular file"}},
		{name: "Alpine, written by v0.3.x", files: map[string]string{sshd.MainConfig: fixture(t, "v032-alpine-sshd_config")},
			want: []string{"generated sshd_config:" + sshd.MainConfig + ":hardn v0.3.x"}},
		{name: "Alpine, written by an unreleased build", files: map[string]string{sshd.MainConfig: fixture(t, "main-alpine-sshd_config")},
			want: []string{"generated sshd_config:" + sshd.MainConfig + ":an unreleased hardn build"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHost(t, tt.files)
			for p, target := range tt.links {
				h.AddSymlink(p, target)
			}
			f, err := Find(context.Background(), h, FindRequest{ConfigFile: tt.configFile})
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, a := range f.Artifacts {
				s := a.Kind.String() + ":" + a.Path + ":" + a.Gen.String()
				if a.Ports != nil {
					s += fmt.Sprint(":", a.Ports)
				}
				got = append(got, s)
				if want := tt.files[a.Path]; string(a.Data) != want {
					t.Errorf("%s: Data is not the file's content", a.Path)
				}
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("artifacts:\n got %q\nwant %q", got, tt.want)
			}
			if len(f.Left) != len(tt.left) {
				t.Fatalf("Left = %+v, want %q", f.Left, tt.left)
			}
			for i, l := range f.Left {
				p, word, _ := strings.Cut(tt.left[i], ": ")
				if l.Path != p || !strings.Contains(l.Why, word) {
					t.Errorf("Left[%d] = %+v, want %s and a why with %q", i, l, p, word)
				}
			}
			for _, c := range h.Calls() {
				if !readOnly(c) {
					t.Errorf("Find made %s; it only reads", c)
				}
			}
		})
	}
}

func TestFindErrors(t *testing.T) {
	h := newHost(t, nil)
	if _, err := Find(context.Background(), h, FindRequest{}); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Find with no sshd_config = %v, want fs.ErrNotExist", err)
	}
	errIO := errors.New("injected: input/output error")
	for _, f := range []func(string, string) error{
		failOn("lstat", ManageConf, errIO),
		failOn("lstat", ListenConf, errIO),
		failOn("read-nofollow", ManageConf, errIO),
		failOn("readdir", sshd.DropInDir, errIO),
	} {
		h := newHost(t, map[string]string{sshd.MainConfig: stockMain, ManageConf: fixture(t, "v032-manage.conf"), ListenConf: fixture(t, "v032-listen.conf")})
		h.fail = f
		if _, err := Find(context.Background(), h, FindRequest{}); !errors.Is(err, errIO) {
			t.Errorf("Find with a failing host call = %v, want the injected error", err)
		}
	}
	h = newHost(t, map[string]string{sshd.MainConfig: stockMain})
	h.AddDir(sshd.DropInDir+"/50-dir.conf", 0o755, 0, 0)
	if _, err := Find(context.Background(), h, FindRequest{}); !errors.Is(err, sshd.ErrNotRegular) {
		t.Errorf("Find with a directory named *.conf = %v, want sshd.ErrNotRegular", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Find(ctx, h, FindRequest{}); !errors.Is(err, context.Canceled) {
		t.Errorf("Find with a done context = %v", err)
	}
}
