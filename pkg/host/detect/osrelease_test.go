package detect_test

import (
	"context"
	"errors"
	"io/fs"
	"reflect"
	"testing"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
)

func TestParseOSRelease(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want map[string]string
	}{
		{"unquoted and double-quoted", "ID=debian\nVERSION_ID=\"13\"\n", map[string]string{"ID": "debian", "VERSION_ID": "13"}},
		{"single quotes keep backslashes", `NAME='a\b "c"'`, map[string]string{"NAME": `a\b "c"`}},
		{"double-quote escapes", `NAME="a \"b\" \$HOME \\ \x"`, map[string]string{"NAME": `a "b" $HOME \ \x`}},
		{"unquoted escape", `NAME=a\ b`, map[string]string{"NAME": "a b"}},
		{"no expansion", `NAME="$ID"` + "\nID=x", map[string]string{"NAME": "$ID", "ID": "x"}},
		{"comments and blank lines", "# ID=no\n\n   \n  # x\nID=yes\n", map[string]string{"ID": "yes"}},
		{"surrounding white space", "  ID=debian  \r\n", map[string]string{"ID": "debian"}},
		{"later repeat wins", "ID=a\nID=b\n", map[string]string{"ID": "b"}},
		{"empty value", "VARIANT=\n", map[string]string{"VARIANT": ""}},
		{"value holding =", `HOME_URL="https://example.net/?a=b"`, map[string]string{"HOME_URL": "https://example.net/?a=b"}},
		{"unterminated quote dropped", "ID=\"debian\nVERSION_ID=13", map[string]string{"VERSION_ID": "13"}},
		{"second word dropped", "ID=debian ubuntu\nVERSION_ID=13", map[string]string{"VERSION_ID": "13"}},
		{"bad keys dropped", "1ID=x\nI-D=x\n=x\nexport ID=x\nnoequals\nID_2=ok", map[string]string{"ID_2": "ok"}},
		{"trailing backslash dropped", `ID=debian\`, map[string]string{}},
	}
	for _, tt := range tests {
		if got := detect.ParseOSRelease([]byte(tt.in)); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("%s: ParseOSRelease(%q) = %q, want %q", tt.name, tt.in, got, tt.want)
		}
	}
}

func TestReadOSRelease(t *testing.T) {
	ctx := context.Background()
	ubuntu := osRelease(t, "ubuntu-26.04")

	t.Run("etc link to usr/lib", func(t *testing.T) {
		got := detect.ReadOSRelease(ctx, newFake(t, "ubuntu-26.04"))
		want := detect.OSRelease{Path: detect.EtcOSRelease, ID: "ubuntu", IDLike: []string{"debian"}, VersionID: "26.04",
			VersionCodename: "resolute", PrettyName: "Ubuntu 26.04.1 LTS"}
		if !reflect.DeepEqual(got, want) || !got.Known() {
			t.Errorf("ReadOSRelease = %+v, want %+v", got, want)
		}
	})
	t.Run("usr/lib only", func(t *testing.T) {
		f := newFake(t, "")
		f.AddFile(detect.UsrOSRelease, ubuntu, 0o644, 0, 0)
		if got := detect.ReadOSRelease(ctx, f); got.Path != detect.UsrOSRelease || got.ID != "ubuntu" || !got.Known() {
			t.Errorf("ReadOSRelease = %+v, want %s read", got, detect.UsrOSRelease)
		}
	})
	t.Run("dangling etc link falls back", func(t *testing.T) {
		f := newFake(t, "")
		f.AddSymlink(detect.EtcOSRelease, "/nowhere")
		f.AddFile(detect.UsrOSRelease, ubuntu, 0o644, 0, 0)
		if got := detect.ReadOSRelease(ctx, f); got.Path != detect.UsrOSRelease || !got.Known() {
			t.Errorf("ReadOSRelease = %+v, want %s read", got, detect.UsrOSRelease)
		}
	})

	unknown := []struct {
		name string
		sys  host.System
		want error
	}{
		{"neither file", newFake(t, ""), fs.ErrNotExist},
		{"etc unreadable is not passed over", func() host.System {
			f := newFake(t, "")
			f.AddFile(detect.EtcOSRelease, []byte("ID=x\n"), 0o600, 0, 0)
			f.AddFile(detect.UsrOSRelease, ubuntu, 0o644, 0, 0)
			return deny(f, detect.EtcOSRelease)
		}(), fs.ErrPermission},
		{"no ID", func() host.System {
			f := newFake(t, "")
			f.AddFile(detect.EtcOSRelease, []byte("NAME=Linux\nVERSION_ID=1\n"), 0o644, 0, 0)
			return f
		}(), detect.ErrUnrecognised},
	}
	for _, tt := range unknown {
		got := detect.ReadOSRelease(ctx, tt.sys)
		if got.Known() || !errors.Is(got.Err, tt.want) {
			t.Errorf("%s: ReadOSRelease = %+v, want unknown with %v", tt.name, got, tt.want)
		}
	}
}
