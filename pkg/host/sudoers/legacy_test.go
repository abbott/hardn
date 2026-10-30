package sudoers_test

import (
	"context"
	"errors"
	"testing"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/sudoers"
)

// origin/main's /etc/sudoers.d/alice, as its characterization goldens
// hold it (pkg/characterization/testdata/golden, the Environment ones
// frozen in pkg/screens/environment/testdata/legacy since
// next/s9-replace): its grant (ops/configure_sudo), its env_keep line
// alone (environment/keep_new), both (ops/setup_sudo_preservation/debian),
// and the line commented out (environment/keep_commented).
const (
	legacyGrant     = "alice ALL=(ALL) NOPASSWD: ALL\n"
	legacyLine      = "Defaults:alice env_keep += \"HARDN_CONFIG\"\n"
	legacyCommented = legacyGrant + "# " + legacyLine
)

// TestReadLegacy: origin/main's /etc/sudoers.d/<user> keeps HARDN_CONFIG
// by its own line, as origin/main wrote it, alone: not commented out
// (which origin/main counted), not indented, not continued from the line
// before, not another list or another user's. A file sudo skips (a '.' in
// the name), a missing one and doas read as none; a symlink is refused.
// Every call is a read, and the file is left as it was.
func TestReadLegacy(t *testing.T) {
	const alice = sudoers.DropInDir + "/alice"
	for _, tc := range []struct {
		name  string
		c     sudoers.Choice
		user  string
		setup func(*hostfake.System)
		want  sudoers.Legacy
		err   error
	}{
		{"the grant and the line", sudoChoice, "alice", file(alice, legacyGrant+legacyLine), sudoers.Legacy{Path: alice, Keeps: true}, nil},
		{"the line alone", sudoChoice, "alice", file(alice, legacyLine), sudoers.Legacy{Path: alice, Keeps: true}, nil},
		{"sudo-rs", rsChoice, "alice", file(alice, legacyGrant+legacyLine), sudoers.Legacy{Path: alice, Keeps: true}, nil},
		{"no final newline", sudoChoice, "alice", file(alice, legacyGrant+"Defaults:alice env_keep += \"HARDN_CONFIG\""), sudoers.Legacy{Path: alice, Keeps: true}, nil},
		{"commented out", sudoChoice, "alice", file(alice, legacyCommented), sudoers.Legacy{Path: alice}, nil},
		{"the grant alone", sudoChoice, "alice", file(alice, legacyGrant), sudoers.Legacy{Path: alice}, nil},
		{"indented", sudoChoice, "alice", file(alice, legacyGrant+"  "+legacyLine), sudoers.Legacy{Path: alice}, nil},
		{"continued from the line before", sudoChoice, "alice", file(alice, "alice ALL=(ALL) NOPASSWD: ALL \\\n"+legacyLine), sudoers.Legacy{Path: alice}, nil},
		{"another list", sudoChoice, "alice", file(alice, "Defaults:alice env_keep += \"HARDN_CONFIG EDITOR\"\n"), sudoers.Legacy{Path: alice}, nil},
		{"bob's line", sudoChoice, "alice", file(alice, "Defaults:bob env_keep += \"HARDN_CONFIG\"\n"), sudoers.Legacy{Path: alice}, nil},
		{"no file", sudoChoice, "alice", func(*hostfake.System) {}, sudoers.Legacy{}, nil},
		{"a name sudo skips", sudoChoice, "alice.smith", file(sudoers.DropInDir+"/alice.smith", "Defaults:alice.smith env_keep += \"HARDN_CONFIG\"\n"), sudoers.Legacy{}, nil},
		{"doas", doasChoice, "alice", func(*hostfake.System) {}, sudoers.Legacy{}, nil},
		{"a symlink", sudoChoice, "alice", func(h *hostfake.System) {
			h.AddFile("/etc/hardn-alice", []byte(legacyLine), 0o440, 0, 0)
			h.AddSymlink(alice, "/etc/hardn-alice")
		}, sudoers.Legacy{}, host.ErrSymlink},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := hostFor(t, tc.c)
			tc.setup(h)
			st, err := sudoers.Read(context.Background(), h, tc.c, []string{tc.user})
			if err != nil {
				t.Fatal(err)
			}
			before, _ := h.Entry(alice)
			n := len(h.Calls())
			got, err := sudoers.ReadLegacy(context.Background(), h, st, tc.user)
			if got != tc.want || !errors.Is(err, tc.err) || (err == nil) != (tc.err == nil) {
				t.Errorf("ReadLegacy = %+v, %v; want %+v, %v", got, err, tc.want, tc.err)
			}
			for _, c := range h.Calls()[n:] {
				if c.Op != "read-nofollow" || c.Path != alice {
					t.Errorf("ReadLegacy made %s", c)
				}
			}
			if after, _ := h.Entry(alice); string(after.Data) != string(before.Data) || after.Mode != before.Mode {
				t.Errorf("%s changed", alice)
			}
		})
	}
	if _, err := sudoers.ReadLegacy(context.Background(), sudoHost(t), sudoers.State{Choice: sudoChoice}, "Alice"); !errors.Is(err, sudoers.ErrUserName) {
		t.Errorf("ReadLegacy(Alice) = %v, want ErrUserName", err)
	}
}

// file adds name to a host, 0440 root:root, holding data.
func file(name, data string) func(*hostfake.System) {
	return func(h *hostfake.System) { h.AddFile(name, []byte(data), 0o440, 0, 0) }
}
