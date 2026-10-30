package sudoers

import (
	"context"
	"slices"
	"strings"

	"github.com/abbott/hardn/pkg/host"
)

// legacyVar is the variable origin/main's env_keep line keeps
// (file_environment_repository.go:69).
const legacyVar = "HARDN_CONFIG"

// Legacy is origin/main's /etc/sudoers.d/<user> as ReadLegacy found it:
// the file of V16, which held the user's grant (ConfigureSudo) and the
// env_keep line setup-sudo-env appended (sudoers-legacy-file).
type Legacy struct {
	// Path is the file, when sudo reads it: an entry of State.Entries
	// named as the user. "" otherwise, on doas too.
	Path string
	// Keeps: Path holds origin/main's env_keep line for the user as
	// origin/main wrote it, `Defaults:<user> env_keep += "HARDN_CONFIG"`,
	// a line of its own: not commented out, nothing before or after it
	// on the line, and not continued from the line before (one ending in
	// '\'). So origin/main's line counts and nothing else does: a line
	// commented out, which origin/main counted (gallery-environment.md §7
	// item 4), or the variable kept in another form, which is another
	// file's rule (sudoers.md §6).
	Keeps bool
}

// ReadLegacy reads origin/main's /etc/sudoers.d/<user> in st's drop-in
// directory: whether sudo reads it, and whether it holds origin/main's
// env_keep line (Legacy.Keeps), decision 3 (a) of slice 9: the screen
// says the variable is kept by that file. It reads that one file, with
// ReadFileNoFollow (a symlink there is host.ErrSymlink), and changes
// nothing: the file is slice 5's to migrate. A Legacy with no Path, and
// no error, when st is not sudo's or sudo reads no file of that name (a
// name with a '.', which sudo skips). Errors: ErrUserName, and the host's.
func ReadLegacy(ctx context.Context, sys host.System, st State, user string) (Legacy, error) {
	if err := CheckUser(ToolSudo, user); err != nil {
		return Legacy{}, err
	}
	if st.Choice.Tool != ToolSudo || !slices.Contains(st.Entries, user) {
		return Legacy{}, nil
	}
	f, err := readFile(ctx, sys, DropInDir+"/"+user)
	if err != nil || !f.Exists {
		return Legacy{}, err
	}
	return Legacy{Path: f.Path, Keeps: keepsLine(f.Data, envLine(user, []string{legacyVar}))}, nil
}

// keepsLine reports whether data has line as a line of its own, not
// continued from the line before.
func keepsLine(data []byte, line string) bool {
	lines := strings.Split(string(data), "\n")
	for i, l := range lines {
		if l == line && (i == 0 || !strings.HasSuffix(strings.TrimSpace(lines[i-1]), `\`)) {
			return true
		}
	}
	return false
}
