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

// Why a fact is Unknown, as detect.Detect would report it.
var (
	errSudo = errors.New("stat /usr/bin/sudo: permission denied")
	errDoas = errors.New("stat /usr/bin/doas: permission denied")
	errOS   = errors.New("os-release sets no ID")
)

// facts is a host's elevation facts as detect.Detect reports them.
func facts(d detect.Distro, sudo, doas detect.Tri, flavor detect.SudoFlavor) detect.Facts {
	f := detect.Facts{Platform: detect.Platform{Distro: d}}
	if d == detect.DistroUnknown {
		f.Platform.Err = errOS
	}
	f.Elevation.Sudo.Present, f.Elevation.Doas.Present, f.Elevation.SudoFlavor = sudo, doas, flavor
	if sudo == detect.Unknown {
		f.Elevation.Sudo.Err = errSudo
	}
	if doas == detect.Unknown {
		f.Elevation.Doas.Err = errDoas
	}
	return f
}

var (
	debian  = facts(detect.DistroDebian, detect.Yes, detect.No, detect.SudoOriginal)
	ubuntu  = facts(detect.DistroUbuntu, detect.Yes, detect.No, detect.SudoRS) // 26.04
	alpine  = facts(detect.DistroAlpine, detect.No, detect.Yes, detect.SudoFlavorUnknown)
	proxmox = facts(detect.DistroProxmox, detect.No, detect.No, detect.SudoFlavorUnknown)
)

// TestChoose: Err is set exactly when the tool is not usable, and a fact
// that is not established (a tool's presence, or, where it decides, the
// distribution) gives ToolUnknown, never ToolNone or a guess.
func TestChoose(t *testing.T) {
	const (
		yes, no, unk = detect.Yes, detect.No, detect.Unknown
		orig         = detect.SudoOriginal
		none         = detect.SudoFlavorUnknown
	)
	var (
		nodistro = detect.DistroUnknown
		deb      = detect.DistroDebian
		alp      = detect.DistroAlpine
	)
	noFlavor := facts(deb, yes, no, none)
	noFlavor.Elevation.Err = detect.ErrUnrecognised
	noCause := facts(alp, no, unk, none)
	noCause.Elevation.Doas.Err = nil
	for _, tc := range []struct {
		name      string
		f         detect.Facts
		want      sudoers.Tool
		tool      sudoers.Tool
		flavor    detect.SudoFlavor
		wantErr   error // in Err, for a tool that is not usable
		flavorErr error
	}{
		{"debian sudo", debian, sudoers.ToolUnknown, sudoers.ToolSudo, orig, nil, nil},
		{"ubuntu 26.04 sudo-rs", ubuntu, sudoers.ToolUnknown, sudoers.ToolSudo, detect.SudoRS, nil, nil},
		{"alpine doas", alpine, sudoers.ToolUnknown, sudoers.ToolDoas, none, nil, nil},
		{"alpine both", facts(alp, yes, yes, orig), sudoers.ToolUnknown, sudoers.ToolUnknown, none, sudoers.ErrAmbiguous, nil},
		{"alpine both, sudo named", facts(alp, yes, yes, orig), sudoers.ToolSudo, sudoers.ToolSudo, orig, nil, nil},
		{"alpine both, doas named", facts(alp, yes, yes, orig), sudoers.ToolDoas, sudoers.ToolDoas, none, nil, nil},
		{"alpine, sudo named but absent", alpine, sudoers.ToolSudo, sudoers.ToolNone, none, sudoers.ErrNoTool, nil},
		{"alpine sudo only", facts(alp, yes, no, orig), sudoers.ToolUnknown, sudoers.ToolSudo, orig, nil, nil},
		{"alpine neither", facts(alp, no, no, none), sudoers.ToolUnknown, sudoers.ToolNone, none, sudoers.ErrNoTool, nil},
		{"alpine doas not established", facts(alp, no, unk, none), sudoers.ToolUnknown, sudoers.ToolUnknown, none, errDoas, nil},
		{"proxmox without sudo", proxmox, sudoers.ToolUnknown, sudoers.ToolNone, none, sudoers.ErrNoTool, nil},
		{"doas outside alpine", facts(deb, no, yes, none), sudoers.ToolUnknown, sudoers.ToolUnknown, none, sudoers.ErrUnsupported, nil},
		{"doas named outside alpine", debian, sudoers.ToolDoas, sudoers.ToolUnknown, none, sudoers.ErrUnsupported, nil},
		{"debian sudo and doas", facts(deb, yes, yes, orig), sudoers.ToolUnknown, sudoers.ToolSudo, orig, nil, nil},
		{"debian sudo, doas not established", facts(deb, yes, unk, orig), sudoers.ToolUnknown, sudoers.ToolSudo, orig, nil, nil},
		{"sudo not established", facts(deb, unk, no, none), sudoers.ToolUnknown, sudoers.ToolUnknown, none, errSudo, nil},
		{"sudo named, not established", facts(deb, unk, no, none), sudoers.ToolSudo, sudoers.ToolUnknown, none, errSudo, nil},
		{"doas not established, no cause given", noCause, sudoers.ToolUnknown, sudoers.ToolUnknown, none, detect.ErrNoSignal, nil},
		// Not ToolNone: doas, if installed, would be ErrUnsupported.
		{"debian no sudo, doas not established", facts(deb, no, unk, none), sudoers.ToolUnknown, sudoers.ToolUnknown, none, errDoas, nil},
		// Where the distribution decides (doas present or not established),
		// an Unknown distribution is Unknown, not "not Alpine".
		{"distribution not established, both", facts(nodistro, yes, yes, orig), sudoers.ToolUnknown, sudoers.ToolUnknown, none, errOS, nil},
		{"distribution not established, doas only", facts(nodistro, no, yes, none), sudoers.ToolUnknown, sudoers.ToolUnknown, none, errOS, nil},
		{"distribution not established, doas not established", facts(nodistro, no, unk, none), sudoers.ToolUnknown, sudoers.ToolUnknown, none, errOS, nil},
		{"distribution not established, doas named", facts(nodistro, no, yes, none), sudoers.ToolDoas, sudoers.ToolUnknown, none, errOS, nil},
		{"distribution not established, sudo only", facts(nodistro, yes, no, orig), sudoers.ToolUnknown, sudoers.ToolSudo, orig, nil, nil},
		{"distribution not established, sudo named", facts(nodistro, yes, yes, orig), sudoers.ToolSudo, sudoers.ToolSudo, orig, nil, nil},
		{"distribution not established, neither", facts(nodistro, no, no, none), sudoers.ToolUnknown, sudoers.ToolNone, none, sudoers.ErrNoTool, nil},
		{"flavour not established", noFlavor, sudoers.ToolUnknown, sudoers.ToolSudo, none, nil, detect.ErrUnrecognised},
		{"none named", debian, sudoers.ToolNone, sudoers.ToolUnknown, none, fs.ErrInvalid, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := sudoers.Choose(tc.f, tc.want)
			if c.Tool != tc.tool || c.Flavor != tc.flavor {
				t.Errorf("Choose = %s/%s, want %s/%s (%v)", c.Tool, c.Flavor, tc.tool, tc.flavor, c.Err)
			}
			usable := tc.tool == sudoers.ToolSudo || tc.tool == sudoers.ToolDoas
			switch {
			case usable && c.Err != nil:
				t.Errorf("Err = %v for a usable tool", c.Err)
			case !usable && (c.Err == nil || !errors.Is(c.Err, tc.wantErr)):
				t.Errorf("Err = %v, want %v", c.Err, tc.wantErr)
			}
			if !errors.Is(c.FlavorErr, tc.flavorErr) { // errors.Is(nil, nil) holds
				t.Errorf("FlavorErr = %v, want %v", c.FlavorErr, tc.flavorErr)
			}
		})
	}
}

func TestCheckUser(t *testing.T) {
	for _, name := range []string{"alice", "bob", "_alice", "bob_smith", "alice.smith", "a-b", "bob$", "a", strings.Repeat("a", 32)} {
		if err := sudoers.CheckUser(sudoers.ToolSudo, name); err != nil {
			t.Errorf("CheckUser(%q) = %v", name, err)
		}
	}
	for _, name := range []string{"", "ALL", "Alice", "-alice", ".alice", "1alice", "alice~", "al ice", "#1000", "%sudo", "+net",
		"alice,bob", "alice=x", "a$b", "alice$$", "alicé", strings.Repeat("a", 33), "alice\n"} {
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
		{"bob_smith", "hardn-grant-bob__smith", "hardn-env-bob__smith", "hardn-bob__smith.conf"},
		{"bob$", "hardn-grant-bob_24", "hardn-env-bob_24", "hardn-bob_24.conf"},
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
