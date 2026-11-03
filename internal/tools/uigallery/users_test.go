package main

import (
	"bytes"
	"context"
	"slices"
	"testing"

	usersctl "github.com/abbott/hardn/pkg/engine/controls/users"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/uitest"
)

// TestPlayUsersGolden plays each flow end to end and pins the transcript.
func TestPlayUsersGolden(t *testing.T) {
	// A pasted key's echo is wider than the screen, so the transcripts
	// answer no to Add SSH key; TestUsersLockoutRisk and
	// TestUsersCreateAlpine paste one.
	create := []string{"2", "carol", "y", "n", "n", "y", "0"}
	tests := []struct {
		name  string
		args  []string
		lines []string
		cells []uitest.Cell
	}{
		{"users-remove-session", []string{"users/hardn"}, []string{"1", "1", "1", "0", "0", "0"}, append(uitest.Core(), asciiColour)},
		{"users-remove-cloud-init", []string{"users/debian13"}, []string{"1", "1", "1", "0", "0", "0"}, nil},
		{"users-remove-group", []string{"users/debian13"}, []string{"1", "2", "1", "0", "0", "0"}, nil},
		{"users-remove-legacy", []string{"users/legacy"}, []string{"1", "1", "1", "0", "0", "0"}, nil},
		{"users-remove-alice", []string{"users/second-admin"}, []string{"1", "2", "1", hostName, "0", "0", "0"}, append(uitest.Core(), asciiColour)},
		{"users-require-no-password", []string{"users/debian13"}, []string{"1", "1", "2", "0", "0", "0"}, nil},
		{"users-require-alice", []string{"users/second-admin"}, []string{"1", "2", "2", hostName, "0", "0", "0"}, nil},
		{"users-stop-asking", []string{"users/sudo-password"}, []string{"1", "1", "2", "y", "0", "0", "0"}, nil},
		{"users-grant-no-sudo", []string{"users/no-sudo"}, []string{"1", "1", "1", "0", "0", "0"}, nil},
		{"users-grant-doas", []string{"users/alpine-no-rule"}, []string{"1", "1", "1", "y", "0", "0", "0"}, nil},
		{"users-not-read", []string{"users/not-read"}, []string{"1", "1", "1", "0", "0", "0"}, nil},
		{"users-add-key", []string{"users/debian13"}, []string{"1", "2", "3", "1", "nope", "0", "0", "0", "0", "0"}, nil},
		{"users-remove-key", []string{"users/debian13"}, []string{"1", "1", "3", "2", "2", hostName, "0", "0", "0", "0"}, nil},
		{"users-last-key", []string{"users/one-key"}, []string{"1", "1", "3", "2", "0", "0", "0", "0"}, nil},
		{"users-create", []string{"users/debian13"}, []string{"2", "George", "george", "carol", "y", "y", "n", "n", "y", "0"}, append(uitest.Core(), asciiColour)},
		{"users-create-alpine", []string{"users/alpine323"}, create, nil},
		{"users-create-pve", []string{"users/pve9"}, []string{"2", "carol", "y", "n", "n", "y", "0"}, nil},
		{"users-create-no-admin", []string{"users/debian13"}, []string{"2", "carol", "n", "n", "y", "0"}, nil},
		{"users-plan-only-create", []string{"-sequence", "dry-run", "users/debian13"}, []string{"2", "carol", "y", "n", "n", "0"}, nil},
		{"users-plan-only-remove", []string{"-sequence", "dry-run", "users/second-admin"}, []string{"1", "2", "1", "0", "0", "0"}, nil},
		{"users-hosts", []string{"users"}, []string{"3", "0", "0"}, nil},
	}
	for _, tt := range tests {
		cells := tt.cells
		if cells == nil {
			cells = uitest.Core()
		}
		uitest.Golden(t, "play/"+tt.name, cells, func(env term.Env) string {
			args := append(lookArgs(t, env), tt.args...)
			code, out, errs, unread := playGallery(tt.lines, args...)
			if code != exitOK || errs != "" || unread != 0 {
				t.Errorf("%s, %v: exit %d, stderr %q, %d lines unread", tt.name, args, code, errs, unread)
			}
			return sessionIDs(t, out)
		})
	}
}

// TestUsersHosts: each host reads with the control (users.Read), with no
// error, the users its note names, and every call a read the model answers.
func TestUsersHosts(t *testing.T) {
	wants := map[string][]string{
		"debian13": {"george", "alice"}, "ubuntu2604": {"george", "alice"}, "alpine323": {"alpine", "george"}, "legacy": {"george"},
		"hardn": {"george"}, "second-admin": {"george", "alice"}, "one-key": {"george", "alice"}, "sudo-password": {"george"},
		"alpine-no-rule": {"george"}, "alpine-locked": {"alpine", "george", "alice"}, "yml-missing": {"george", "alice"},
		"not-read": {"george", "alice"}, "root-login": {"george"}, "debian-nopam": {"george", "alice"}, "alpine-stray": {"alpine", "george"},
		"group-home": {"george", "alice"}, "pve9": nil, "no-sudo": {"george"},
	}
	if len(wants) != len(usrHosts()) {
		t.Fatalf("%d hosts, %d wanted", len(usrHosts()), len(wants))
	}
	ctx := context.Background()
	for _, uh := range usrHosts() {
		h := uh.host()
		sys := usrSystem(h)
		f, err := detect.Detect(ctx, sys)
		if err != nil {
			t.Fatalf("%s: %v", uh.name, err)
		}
		r, err := usersctl.Read(ctx, sys, f, uh.getenv)
		var names []string
		for _, u := range r.Users {
			names = append(names, u.Name)
		}
		if err != nil || !slices.Equal(names, wants[uh.name]) {
			t.Errorf("%s: users %q, %v; want %q", uh.name, names, err, wants[uh.name])
		}
		if got := h.unanswered(); len(got) > 0 {
			t.Errorf("%s: commands the model does not answer: %q", uh.name, got)
		}
	}
}

// TestUsersUsage: play refuses what it does not take, and the listing is
// gone, with status 2 and nothing on stdout; the main menu's 1 leads to
// debian13's.
func TestUsersUsage(t *testing.T) {
	for _, args := range [][]string{
		{usrPrefix},
		{"play", "users/nope"},
		{"play", "-fail", "1", "users/debian13"},
		{"play", "-sequence", "failure", "users/debian13"},
	} {
		var out, errs bytes.Buffer
		if code := run(args, nil, offTerminal, &out, &errs); code != exitUsage || out.Len() > 0 || errs.Len() == 0 {
			t.Errorf("%v: exit %d, stdout %q, stderr %q", args, code, out.String(), errs.String())
		}
	}
	if usrLinks[mainMenuMock+"/1"] != usrMainHost {
		t.Errorf("the main menu's 1 leads to %q", usrLinks[mainMenuMock+"/1"])
	}
}
