package alpine_test

import (
	"context"
	"io/fs"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/alpine"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
)

// TestCompareVersion: apk's order of versions, each pair both ways; every
// branch's first fixed release equal to itself; a version not in apk's
// grammar an error.
func TestCompareVersion(t *testing.T) {
	for _, tc := range []struct{ a, b string }{ // a < b
		{"10.0_p1-r10", "10.0_p1-r11"},
		{"9.9_p2-r1", "10.0_p1-r1"},
		{"9.9", "10.0"},
		{"10.3_p1-r1", "10.3_p1-r2"},
		{"10.3_p1-r2", "10.6_p2-r0"},
		{"10.2_p1-r0", "10.2_p1-r1"},
		{"10.6_p1-r5", "10.6_p2-r0"},
		{"10.6_p2", "10.6_p2-r0"},    // no revision before -r0
		{"10.6", "10.6_p1"},          // a post-release suffix after none
		{"10.6_rc1", "10.6"},         // a pre-release suffix before none
		{"10.6_alpha1", "10.6_beta"}, // alpha < beta < pre < rc
		{"10.6_beta2", "10.6_pre1"},
		{"10.6_pre3", "10.6_rc1"},
		{"10.6_cvs", "10.6_svn"}, // cvs < svn < git < hg < p
		{"10.6_svn", "10.6_git"},
		{"10.6_git", "10.6_hg"},
		{"10.6_hg", "10.6_p"},
		{"10.6_p1", "10.6_p2"},
		{"10.6_p9", "10.6_p10"},
		{"10.6", "10.6.1"},    // an extra component
		{"10.6_p5", "10.6.1"}, // a component before a suffix
		{"10.6", "10.6a"},     // a letter after the components
		{"10.6a", "10.6.1"},   // an extra component before a letter
		{"10.6a", "10.6b"},
	} {
		if c, err := alpine.CompareVersion(tc.a, tc.b); c != -1 || err != nil {
			t.Errorf("CompareVersion(%s, %s) = %d, %v; want -1", tc.a, tc.b, c, err)
		}
		if c, err := alpine.CompareVersion(tc.b, tc.a); c != 1 || err != nil {
			t.Errorf("CompareVersion(%s, %s) = %d, %v; want 1", tc.b, tc.a, c, err)
		}
	}
	for _, v := range []string{"10.3_p1-r2", "10.2_p1-r1", "10.0_p1-r11", "9.9_p2-r1", "10.6_p2-r0"} {
		if c, err := alpine.CompareVersion(v, v); c != 0 || err != nil {
			t.Errorf("CompareVersion(%s, %s) = %d, %v; want 0", v, v, c, err)
		}
	}
	for _, v := range []string{"", "10", "10.6_p2-r", "10.6_x1", "10.6_p2~abc", "v10.6", "10.6-r1-r2", "10..6", "10.6p2", "10.6_p1a", "1.05"} {
		if v == "10" { // one component is apk's grammar
			if _, err := alpine.CompareVersion(v, "9"); err != nil {
				t.Errorf("CompareVersion(10, 9): %v", err)
			}
			continue
		}
		if _, err := alpine.CompareVersion(v, "10.6_p2-r0"); err == nil {
			t.Errorf("CompareVersion(%q): no error", v)
		}
		if _, err := alpine.CompareVersion("10.6_p2-r0", v); err == nil {
			t.Errorf("CompareVersion(_, %q): no error", v)
		}
	}
}

// TestParseInstalled: each record's P: and V:, nothing else; a record
// with no name or no version, or a name twice, an error naming the line.
func TestParseInstalled(t *testing.T) {
	db := "C:Q1abc=\nP:musl\nV:1.2.5-r10\nA:x86_64\n\nC:Q1def=\nP:openssh-server\nV:10.3_p1-r2\nA:x86_64\nF:usr/sbin\nR:sshd\n\nP:last\nV:1.0-r0"
	got, err := alpine.ParseInstalled([]byte(db))
	if err != nil || len(got) != 3 || got["musl"] != "1.2.5-r10" || got["openssh-server"] != "10.3_p1-r2" || got["last"] != "1.0-r0" {
		t.Errorf("ParseInstalled = %v, %v", got, err)
	}
	if got, err := alpine.ParseInstalled(nil); err != nil || len(got) != 0 {
		t.Errorf("empty: %v, %v", got, err)
	}
	for _, tc := range []struct{ db, line string }{
		{"P:musl\nV:1\n\nP:openssh-server\nA:x86_64\n", "line 4"},
		{"V:1\n", "line 1"},
		{"P:a\nV:1\n\n\nP:a\nV:2\n", "line 5"},
	} {
		if _, err := alpine.ParseInstalled([]byte(tc.db)); err == nil || !strings.Contains(err.Error(), tc.line) {
			t.Errorf("ParseInstalled(%q) = %v, want an error at %s", tc.db, err, tc.line)
		}
	}
}

// TestFixedVersion: each branch's first fixed release; edge's for a
// branch not listed and for a VERSION_ID that is not x.y.z.
func TestFixedVersion(t *testing.T) {
	for id, want := range map[string]string{
		"3.24.2": "10.3_p1-r2", "3.24.0": "10.3_p1-r2", "3.23.6": "10.2_p1-r1", "3.22.1": "10.0_p1-r11", "3.21.4": "9.9_p2-r1",
		"3.25.0_alpha20261001": "10.6_p2-r0", "3.20.3": "10.6_p2-r0", "3.24": "10.6_p2-r0", "": "10.6_p2-r0",
	} {
		if got := alpine.FixedVersion(id); got != want {
			t.Errorf("FixedVersion(%q) = %s, want %s", id, got, want)
		}
	}
}

// TestOpenSSHFix: fixed, unfixed, the wanted version itself; a package of
// another release than the branch's (edge's on 3.24) fixed only from
// upstream's fix; Unknown with why for a database missing or not read, no
// entry, a version not compared; reads only.
func TestOpenSSHFix(t *testing.T) {
	ctx := context.Background()
	rec := func(v string) string {
		return "P:openssh-server\nV:" + v + "\n\nP:openssh-server-pam\nV:10.3_p1-r1\n\n"
	}
	for _, tc := range []struct {
		name, db, pkg, id string
		unreadable        bool
		fixed             detect.Tri
		installed, why    string
		wanted            string // "": the branch's, FixedVersion's
	}{
		{name: "3.24 fixed", db: rec("10.3_p1-r2"), pkg: alpine.OpenSSHServer, id: "3.24.2", fixed: detect.Yes, installed: "10.3_p1-r2"},
		{name: "3.24 later", db: rec("10.3_p1-r3"), pkg: alpine.OpenSSHServer, id: "3.24.2", fixed: detect.Yes, installed: "10.3_p1-r3"},
		{name: "3.24 unfixed", db: rec("10.3_p1-r1"), pkg: alpine.OpenSSHServer, id: "3.24.2", fixed: detect.No, installed: "10.3_p1-r1"},
		{name: "3.24 -pam unfixed", db: rec("10.3_p1-r2"), pkg: alpine.OpenSSHServerPAM, id: "3.24.2", fixed: detect.No, installed: "10.3_p1-r1"},
		{name: "3.22 r10", db: rec("10.0_p1-r10"), pkg: alpine.OpenSSHServer, id: "3.22.1", fixed: detect.No, installed: "10.0_p1-r10"},
		{name: "3.22 r11", db: rec("10.0_p1-r11"), pkg: alpine.OpenSSHServer, id: "3.22.1", fixed: detect.Yes, installed: "10.0_p1-r11"},
		{name: "edge before 10.6_p2", db: rec("10.6_p1-r3"), pkg: alpine.OpenSSHServer, id: "3.25.0_alpha20261101", fixed: detect.No, installed: "10.6_p1-r3"},
		{name: "3.24, edge's 10.6_p1 pinned", db: rec("10.6_p1-r0"), pkg: alpine.OpenSSHServer, id: "3.24.2", fixed: detect.No, installed: "10.6_p1-r0", wanted: "10.6_p2-r0"},
		{name: "3.24, edge's 10.6_p2", db: rec("10.6_p2-r0"), pkg: alpine.OpenSSHServer, id: "3.24.2", fixed: detect.Yes, installed: "10.6_p2-r0", wanted: "10.6_p2-r0"},
		{name: "missing", pkg: alpine.OpenSSHServer, id: "3.24.2", why: "/lib/apk/db/installed is missing"},
		{name: "unreadable", db: rec("10.3_p1-r2"), unreadable: true, pkg: alpine.OpenSSHServer, id: "3.24.2", why: "open /lib/apk/db/installed: permission denied"},
		{name: "no entry", db: rec("10.3_p1-r2"), pkg: alpine.OpenSSHServerKrb5, id: "3.24.2", why: "openssh-server-krb5 isn't in /lib/apk/db/installed"},
		{name: "not compared", db: rec("10.3_p1-r2~git1"), pkg: alpine.OpenSSHServer, id: "3.24.2", installed: "10.3_p1-r2~git1", why: "version 10.3_p1-r2~git1 isn't one hardn compares"},
		{name: "a record without V:", db: "P:openssh-server\n\n", pkg: alpine.OpenSSHServer, id: "3.24.2", why: "/lib/apk/db/installed: the record at line 1 has no P: or no V:"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := hostfake.New(host.Options{})
			if tc.db != "" {
				h.AddFile(alpine.InstalledDB, []byte(tc.db), 0o644, 0, 0)
			}
			var sys host.System = h
			if tc.unreadable {
				sys = denied{h}
			}
			got := alpine.OpenSSHFix(ctx, sys, tc.pkg, tc.id)
			want := tc.wanted
			if want == "" {
				want = alpine.FixedVersion(tc.id)
			}
			if got.Fixed != tc.fixed || got.Installed != tc.installed || got.Why != tc.why || got.Package != tc.pkg || got.Wanted != want {
				t.Errorf("OpenSSHFix = %+v", got)
			}
			for _, c := range h.Calls() {
				if c.Op != "read" {
					t.Errorf("a call that is not a read: %v", c)
				}
			}
		})
	}
}

// denied refuses every read, as a run without root may find a file.
type denied struct{ *hostfake.System }

func (denied) ReadFile(_ context.Context, name string) ([]byte, error) {
	return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrPermission}
}
