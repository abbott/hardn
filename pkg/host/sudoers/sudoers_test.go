package sudoers_test

import (
	"errors"
	"io/fs"
	"path"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/sudoers"
)

// facts is a host's elevation facts as detect.Detect reports them.
func facts(d detect.Distro, sudo, doas detect.Tri, flavor detect.SudoFlavor) detect.Facts {
	f := detect.Facts{Platform: detect.Platform{Distro: d}}
	f.Elevation.Sudo.Present, f.Elevation.Doas.Present, f.Elevation.SudoFlavor = sudo, doas, flavor
	if sudo == detect.Unknown {
		f.Elevation.Sudo.Err = fs.ErrPermission
	}
	return f
}

var (
	debian  = facts(detect.DistroDebian, detect.Yes, detect.No, detect.SudoOriginal)
	ubuntu  = facts(detect.DistroUbuntu, detect.Yes, detect.No, detect.SudoRS) // 26.04
	alpine  = facts(detect.DistroAlpine, detect.No, detect.Yes, detect.SudoFlavorUnknown)
	proxmox = facts(detect.DistroProxmox, detect.No, detect.No, detect.SudoFlavorUnknown)
)

func TestChoose(t *testing.T) {
	both := facts(detect.DistroAlpine, detect.Yes, detect.Yes, detect.SudoOriginal)
	noFlavor := facts(detect.DistroDebian, detect.Yes, detect.No, detect.SudoFlavorUnknown)
	noFlavor.Elevation.Err = detect.ErrUnrecognised
	for _, tc := range []struct {
		name    string
		f       detect.Facts
		want    sudoers.Tool
		tool    sudoers.Tool
		flavor  detect.SudoFlavor
		wantErr error
	}{
		{"debian sudo", debian, sudoers.ToolUnknown, sudoers.ToolSudo, detect.SudoOriginal, nil},
		{"ubuntu 26.04 sudo-rs", ubuntu, sudoers.ToolUnknown, sudoers.ToolSudo, detect.SudoRS, nil},
		{"alpine doas", alpine, sudoers.ToolUnknown, sudoers.ToolDoas, 0, nil},
		{"alpine both", both, sudoers.ToolUnknown, sudoers.ToolUnknown, 0, sudoers.ErrAmbiguous},
		{"alpine both, sudo named", both, sudoers.ToolSudo, sudoers.ToolSudo, detect.SudoOriginal, nil},
		{"alpine both, doas named", both, sudoers.ToolDoas, sudoers.ToolDoas, 0, nil},
		{"alpine, sudo named but absent", alpine, sudoers.ToolSudo, sudoers.ToolNone, 0, sudoers.ErrNoTool},
		{"proxmox without sudo", proxmox, sudoers.ToolUnknown, sudoers.ToolNone, 0, sudoers.ErrNoTool},
		{"doas outside alpine", facts(detect.DistroDebian, detect.No, detect.Yes, 0), sudoers.ToolUnknown, sudoers.ToolUnknown, 0, sudoers.ErrUnsupported},
		{"doas named outside alpine", debian, sudoers.ToolDoas, sudoers.ToolUnknown, 0, sudoers.ErrUnsupported},
		{"debian sudo and doas", facts(detect.DistroDebian, detect.Yes, detect.Yes, detect.SudoOriginal), sudoers.ToolUnknown, sudoers.ToolSudo, detect.SudoOriginal, nil},
		{"sudo not established", facts(detect.DistroDebian, detect.Unknown, detect.No, 0), sudoers.ToolUnknown, sudoers.ToolUnknown, 0, fs.ErrPermission},
		{"alpine doas not established", facts(detect.DistroAlpine, detect.No, detect.Unknown, 0), sudoers.ToolUnknown, sudoers.ToolUnknown, 0, nil},
		{"flavour not established", noFlavor, sudoers.ToolUnknown, sudoers.ToolSudo, detect.SudoFlavorUnknown, detect.ErrUnrecognised},
		{"none named", debian, sudoers.ToolNone, sudoers.ToolUnknown, 0, fs.ErrInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := sudoers.Choose(tc.f, tc.want)
			if c.Tool != tc.tool || c.Flavor != tc.flavor {
				t.Errorf("Choose = %s/%s, want %s/%s (%v)", c.Tool, c.Flavor, tc.tool, tc.flavor, c.Err)
			}
			switch {
			case tc.wantErr != nil && !errors.Is(c.Err, tc.wantErr):
				t.Errorf("Err = %v, want %v", c.Err, tc.wantErr)
			case tc.tool != sudoers.ToolSudo && tc.tool != sudoers.ToolDoas && c.Err == nil:
				t.Error("Err = nil for a tool that is not usable")
			case tc.tool == sudoers.ToolDoas && c.Err != nil, tc.tool == sudoers.ToolSudo && tc.wantErr == nil && c.Err != nil:
				t.Errorf("Err = %v, want nil", c.Err)
			}
		})
	}
}

func TestCheckUser(t *testing.T) {
	for _, name := range []string{"alice", "bob", "_svc", "svc_backup", "alice.smith", "a-b", "web01$", "a", strings.Repeat("a", 32)} {
		if err := sudoers.CheckUser(sudoers.ToolSudo, name); err != nil {
			t.Errorf("CheckUser(%q) = %v", name, err)
		}
	}
	for _, name := range []string{"", "ALL", "Alice", "-alice", ".alice", "1alice", "alice~", "al ice", "#1000", "%sudo", "+net",
		"alice,bob", "alice=x", "a$b", "alice$$", "élise", strings.Repeat("a", 33), "alice\n"} {
		if err := sudoers.CheckUser(sudoers.ToolSudo, name); !errors.Is(err, sudoers.ErrUserName) {
			t.Errorf("CheckUser(%q) = %v, want ErrUserName", name, err)
		}
	}
	// doas.conf(5)'s keywords are words, not users, in a doas rule.
	for _, kw := range []string{"permit", "deny", "as", "cmd", "args", "nopass", "nolog", "persist", "keepenv", "setenv"} {
		if err := sudoers.CheckUser(sudoers.ToolDoas, kw); !errors.Is(err, sudoers.ErrUserName) {
			t.Errorf("CheckUser(doas, %q) = %v, want ErrUserName", kw, err)
		}
		if err := sudoers.CheckUser(sudoers.ToolSudo, kw); err != nil {
			t.Errorf("CheckUser(sudo, %q) = %v", kw, err)
		}
	}
}

// TestFileNames: names sudo and sudo-rs read (no '.', no trailing '~'),
// one-to-one with user names, under fixed prefixes, so no user's file is
// another user's and no file is the distribution's.
func TestFileNames(t *testing.T) {
	for _, tc := range []struct {
		user, grant, env, doas string
	}{
		{"alice", "hardn-grant-alice", "hardn-env-alice", "hardn-alice.conf"},
		{"alice.smith", "hardn-grant-alice_2esmith", "hardn-env-alice_2esmith", "hardn-alice_2esmith.conf"},
		{"svc_backup", "hardn-grant-svc__backup", "hardn-env-svc__backup", "hardn-svc__backup.conf"},
		{"web01$", "hardn-grant-web01_24", "hardn-env-web01_24", "hardn-web01_24.conf"},
		{"env-alice", "hardn-grant-env-alice", "hardn-env-env-alice", "hardn-env-alice.conf"},
	} {
		s, err := sudoers.FileNames(sudoers.ToolSudo, tc.user)
		if err != nil {
			t.Fatal(err)
		}
		d, err := sudoers.FileNames(sudoers.ToolDoas, tc.user)
		if err != nil {
			t.Fatal(err)
		}
		if s.Grant != sudoers.DropInDir+"/"+tc.grant || s.Env != sudoers.DropInDir+"/"+tc.env || d.Grant != sudoers.DoasDir+"/"+tc.doas || d.Env != "" {
			t.Errorf("FileNames(%q) = %+v, %+v", tc.user, s, d)
		}
	}
	seen := map[string]string{}
	for _, u := range []string{"alice", "alice.smith", "alice_2esmith", "alice_smith", "alice__smith", "alice-smith", "alice_", "_alice",
		"grant-alice", "env-alice", "a.b", "a_2eb", "a__2eb", "x$", "x_24", "x__24"} {
		n, err := sudoers.FileNames(sudoers.ToolSudo, u)
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range []string{n.Grant, n.Env} {
			b := path.Base(p)
			if strings.Contains(b, ".") || strings.HasSuffix(b, "~") {
				t.Errorf("%q: %s is a name sudo skips", u, b)
			}
			if prev, ok := seen[b]; ok {
				t.Errorf("%q and %q share %s", prev, u, b)
			}
			seen[b] = u
		}
	}
	if _, err := sudoers.FileNames(sudoers.ToolNone, "alice"); !errors.Is(err, fs.ErrInvalid) {
		t.Errorf("FileNames(none) = %v, want fs.ErrInvalid", err)
	}
	if _, err := sudoers.FileNames(sudoers.ToolSudo, "Alice"); !errors.Is(err, sudoers.ErrUserName) {
		t.Errorf("FileNames(Alice) = %v, want ErrUserName", err)
	}
}

// TestCodes pins the words scripts and p3-output will match on.
func TestCodes(t *testing.T) {
	got := []string{
		sudoers.ToolUnknown.String(), sudoers.ToolNone.String(), sudoers.ToolSudo.String(), sudoers.ToolDoas.String(),
		sudoers.GrantNone.String(), sudoers.GrantPassword.String(), sudoers.GrantNoPassword.String(), sudoers.Grant(9).String(),
		sudoers.RiskNone.String(), sudoers.RiskLockout.String(),
		sudoers.FindingLater.String(), sudoers.FindingAfterInclude.String(), sudoers.FindingLegacy.String(), sudoers.FindingKind(0).String(),
		sudoers.StageBefore.String(), sudoers.StageCandidate.String(), sudoers.StageAfter.String(), sudoers.StageRestored.String(), sudoers.Stage(0).String(),
	}
	want := []string{
		"unknown", "none", "sudo", "doas",
		"none", "password", "no-password", "unknown",
		"none", "lockout",
		"sudoers-later-file", "sudoers-after-include", "sudoers-legacy-file", "unknown",
		"before", "candidate", "after", "restored", "unknown",
	}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("codes = %q\nwant    %q", got, want)
	}
}
