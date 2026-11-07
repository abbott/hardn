package main

import (
	"slices"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/host/alpine"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/sshd"
)

// alpineUsersHost is the User Management host as Alpine 3.24.2 where
// sshd checks passwords without PAM (UsePAM no, as openssh-server-pam's
// build reads it), password authentication on, and openssh-server at
// version in apk's installed database ("" none): sudo stays, so that -c's
// grant is planned as on Debian.
func alpineUsersHost(t *testing.T, version string) *usrTestHost {
	t.Helper()
	h, _ := newUsersHost(t)
	h.facts.OS = detect.OSRelease{ID: "alpine", VersionID: "3.24.2"}
	h.facts.Platform = detect.Platform{Distro: detect.DistroAlpine, Version: "3.24", Support: detect.Supported}
	h.AddFile(sshd.MainConfig, []byte(sshd.IncludeLine+"\n\nUsePAM no\n"), 0o644, 0, 0)
	h.AddFile("/usr/sbin/useradd", []byte("ELF"), 0o755, 0, 0) // shadow's, which Create runs
	if version != "" {
		h.AddFile(alpine.InstalledDB, []byte("P:openssh-server\nV:"+version+"\n\n"), 0o644, 0, 0)
	}
	inner := h.Handler
	h.Handler = func(argv []string, in []byte) hostfake.Response {
		r := inner(argv, in)
		if (argv[0] == "sshd" || argv[0] == "/usr/sbin/sshd") && slices.Contains(argv, "-T") {
			r.Stdout = strings.Replace(r.Stdout, "usepam yes\n", "usepam no\n", 1)
		}
		return r
	}
	return h
}

// TestCreateUserFlagBypass: -c on Alpine with openssh-server lacking
// CVE-2026-123158's fix is the control's refusal, its code and words,
// before anything is written: no useradd, 1, under -n too. With the fix,
// it applies as on Debian, the field "*"; with the fix not established, it
// applies with the plan's SSH_FIX_UNKNOWN said.
func TestCreateUserFlagBypass(t *testing.T) {
	for _, tc := range []struct {
		name, version string
		args          []string
		code          int
		said          []string
		applied       bool
	}{
		{"unfixed", "10.3_p1-r1", []string{"-c", "-u", "carol"}, 1,
			[]string{"sshd would let anyone log in as carol with a long password, since carol has no password (CVE-2026-123158)", "USERS_PASSWORD_BYPASS",
				"To go on: run apk -U upgrade, then rc-service sshd restart, or turn password login off in SSH Login."}, false},
		{"unfixed, -n", "10.3_p1-r1", []string{"-n", "-c", "-u", "carol"}, 1, []string{"USERS_PASSWORD_BYPASS"}, false},
		{"fixed", "10.3_p1-r2", []string{"--yes", "-c", "-u", "carol"}, 0, []string{"Create user carol", "Let carol use sudo", "Operation Successful"}, true},
		{"not established", "", []string{"--yes", "-c", "-u", "carol"}, 0,
			[]string{"Create user carol", "Operation Successful", "can't tell if openssh-server has the fix for CVE-2026-123158: /lib/apk/db/installed is missing", "SSH_FIX_UNKNOWN"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := alpineUsersHost(t, tc.version)
			cfg := withKeys(t, h)
			r, s := runC(t, h, cfg, nil, tc.args...)
			report := stripANSI(s.out.String())
			if got := map[bool]int{true: r.code}[r.ended]; got != tc.code {
				t.Fatalf("exit status %d (ended %v), want %d; report:\n%s\nstderr:\n%s", r.code, r.ended, tc.code, report, r.stderr)
			}
			if w := inOrder(report, tc.said...); w != "" {
				t.Errorf("the report lacks %q after the text before it:\n%s", w, report)
			}
			ran := h.commands("useradd")
			switch {
			case !tc.applied && (len(h.writes()) > 0 || len(ran) > 0):
				t.Errorf("wrote %q, ran %q", h.writes(), ran)
			case tc.applied && !slices.Equal(ran, []string{"useradd --home-dir /home/carol --create-home --shell /bin/ash --password \"*\" carol"}):
				t.Errorf("ran %q", ran)
			}
		})
	}
}
