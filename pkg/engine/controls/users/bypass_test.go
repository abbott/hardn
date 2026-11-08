package users_test

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/engine/controls/ssh"
	"github.com/abbott/hardn/pkg/engine/controls/users"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/alpine"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/output"
)

// sshdAlpine is Alpine's /usr/sbin/sshd -T over its stock files: built
// without PAM (no usepam line), password and keyboard-interactive
// authentication on, OpenSSH's defaults, as the 3.23.6 VM printed them
// (check-ssh.md §8); sshdAlpineOff the same with PasswordAuthentication
// no. Shaped like OpenSSH's, not read from a binary.
const (
	sshdAlpine    = sshdBase + "permitrootlogin prohibit-password\npubkeyauthentication yes\npasswordauthentication yes\nkbdinteractiveauthentication yes\n" + keyFiles
	sshdAlpineOff = sshdBase + "permitrootlogin prohibit-password\npubkeyauthentication yes\npasswordauthentication no\nkbdinteractiveauthentication yes\n" + keyFiles
)

// alpineCreate is Alpine 3.24.2 with sshd answering out over the file
// sshdConfig and openssh-server at version in apk's installed database
// ("" none).
func alpineCreate(out, sshdConfig, version string) site {
	s := site{plat: with(alpine324(), func(f *detect.Facts) { f.OS = detect.OSRelease{ID: "alpine", VersionID: "3.24.2"} }), sshd: out,
		files: map[string]string{"/etc/ssh/sshd_config": sshdConfig}}
	if version != "" {
		s.files[alpine.InstalledDB] = "P:musl\nV:1.2.5-r10\n\nP:openssh-server\nV:" + version + "\n\n"
	}
	return s
}

// noOpenSSH is s with an installed database holding no openssh package.
func noOpenSSH(s site) site {
	s.files[alpine.InstalledDB] = "P:musl\nV:1.2.5-r10\n\n"
	return s
}

// TestCreateBypass: Create on Alpine, where the new user's field is "*"
// (sshd's PAM not on), refused before anything is written while
// openssh-server lacks CVE-2026-123158's fix and password authentication
// is on, in the proposed words, and CheckNew the same (the User
// Management screen's); planned as before with the fix, with password
// authentication off, or with sshd's PAM on; planned with a finding
// (SSH_FIX_UNKNOWN) beside USERS_NO_KEY where the fix is not established;
// planned with no such finding where no sshd can run.
func TestCreateBypass(t *testing.T) {
	const words = "sshd would let anyone log in as carol with a long password, since carol has no password (CVE-2026-123158). " +
		"Upgrade openssh in SSH Login (5), or turn password login off there (2)."
	n := users.New{Name: "carol"}
	t.Run("refused", func(t *testing.T) {
		s := alpineCreate(sshdAlpine, "# stock\n", "10.3_p1-r1")
		_, p, sys, _ := planOn(t, s, users.Admin{Action: users.ActionCreate, New: n})
		refused(t, p, "USERS_PASSWORD_BYPASS", words)
		r, err := users.Read(context.Background(), sys, s.plat.facts, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := r.CheckNew(context.Background(), sys, n); output.CodeOf(err) != "USERS_PASSWORD_BYPASS" {
			t.Errorf("CheckNew: %v", err)
		}
	})
	t.Run("refused through a Match block", func(t *testing.T) {
		s := alpineCreate(sshdAlpineOff, "PasswordAuthentication no\nMatch User alice\n\tPasswordAuthentication yes\n", "10.3_p1-r1")
		_, p, _, _ := planOn(t, s, users.Admin{Action: users.ActionCreate, New: n})
		refused(t, p, "USERS_PASSWORD_BYPASS", words)
	})
	for _, tc := range []struct {
		name  string
		s     site
		codes []string
		why   string
	}{
		{"fixed", alpineCreate(sshdAlpine, "# stock\n", "10.3_p1-r2"), []string{users.CodeNoKey}, ""},
		{"password authentication off", alpineCreate(sshdAlpineOff, "PasswordAuthentication no\n", "10.3_p1-r1"), []string{users.CodeNoKey}, ""},
		{"sshd's PAM on", alpineCreate(sshdBase+"usepam yes\n"+strings.TrimPrefix(sshdAlpine, sshdBase), "UsePAM yes\n", "10.3_p1-r1"), []string{users.CodeNoKey}, ""},
		{"no installed database", alpineCreate(sshdAlpine, "# stock\n", ""), []string{users.CodeNoKey, ssh.CodeFixUnknown},
			"can't tell if openssh-server has the fix for CVE-2026-123158: /lib/apk/db/installed is missing"},
		{"password authentication off, no database", alpineCreate(sshdAlpineOff, "PasswordAuthentication no\n", ""), []string{users.CodeNoKey}, ""},
		// No sshd can run, as in the Alpine 3.23 LXC of 2026-11-07 (no
		// openssh at all): nothing to warn of.
		{"no sshd, no sshd_config", noOpenSSH(alpineCreate("", "", "")), []string{users.CodeNoKey}, ""},
		{"sshd not installed", func() site {
			s := noOpenSSH(alpineCreate("", "# stock\n", ""))
			s.failT = &hostfake.Response{Err: fmt.Errorf("host: run /usr/sbin/sshd: %w", host.ErrNotFound)}
			return s
		}(), []string{users.CodeNoKey}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, p, _, _ := planOn(t, tc.s, users.Admin{Action: users.ActionCreate, New: n})
			if p.Err() != nil || len(p.Changes) != 1 {
				t.Fatalf("%v, %+v", p.Err(), p.Changes)
			}
			var codes []string
			for _, f := range p.Changes[0].Findings {
				codes = append(codes, f.Code)
				if f.Code == ssh.CodeFixUnknown && f.Detail != tc.why {
					t.Errorf("finding %q, want %q", f.Detail, tc.why)
				}
			}
			if !slices.Equal(codes, tc.codes) {
				t.Errorf("findings %+v, want %v", p.Changes[0].Findings, tc.codes)
			}
			star := !strings.Contains(tc.name, "PAM on")
			if cmd := p.Changes[0].Commands[0]; !strings.HasPrefix(cmd, "useradd --home-dir /home/carol --create-home --shell /bin/ash") || strings.Contains(cmd, `--password "*"`) != star {
				t.Errorf("%s", cmd)
			}
		})
	}
	// The plan's finding is what the engine's plan carries for the
	// report: on the change, as USERS_NO_KEY's.
	_, p, _, _ := planOn(t, alpineCreate(sshdAlpine, "# stock\n", ""), users.Admin{Action: users.ActionCreate, New: n})
	if fs := p.Changes[0].Findings; len(fs) != 2 || fs[1].Source != alpine.InstalledDB {
		t.Errorf("findings %+v", fs)
	}
}
