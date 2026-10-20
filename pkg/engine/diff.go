package engine

import "github.com/abbott/hardn/pkg/diff"

// devNull names the missing side of a file created or removed, as diff -u
// and git diff do.
const devNull = "/dev/null"

// FileDiff is a file change's Diff: pkg/diff's unified diff of the file's
// content before and after, under its path; nil is a file that does not
// exist. "" when nothing changes. A file created or removed empty shows its
// headers alone, so the plan still shows it.
func FileDiff(path string, before, after *string) string {
	if before == nil && after == nil {
		return ""
	}
	oldName, newName, a, b := path, path, "", ""
	if before == nil {
		oldName = devNull
	} else {
		a = *before
	}
	if after == nil {
		newName = devNull
	} else {
		b = *after
	}
	d := diff.Unified(oldName, newName, a, b)
	if d == "" && (before == nil) != (after == nil) {
		d = "--- " + oldName + "\n+++ " + newName + "\n"
	}
	return d
}

// CommandDiff is a command change's Diff: what reading the state shows
// before and after the command (ufw status, a service's state), as a
// unified diff under "<what> (before)" and "<what> (after)"; "" when they
// are the same.
func CommandDiff(what, before, after string) string {
	return diff.Unified(what+" (before)", what+" (after)", before, after)
}
