// Package characterization records what origin/main does today, as golden files.
//
// The tests drive the real infrastructure.ServiceFactory, application managers,
// domain services and secondary adapters. The only substitutes are the repo's
// own interfaces.MockFileSystem and interfaces.MockCommander, wrapped in
// recorders that log every call in order. Nothing here executes a command, and
// nothing writes outside t.TempDir() except -update rewriting testdata/golden.
//
// The goldens are a baseline, not a specification: origin/main is the behaviour
// baseline (maintainer decision 6), and lines tagged KNOWN-DEFECT name entries
// of the Phase 0 V-list (side-effect inventory, V1-V27) that the refactor is
// expected to fix. A fix flips its golden deliberately, in the same commit,
// with the reason in the review description. A P0.8 hazard gate's fix turns
// the item's KNOWN-DEFECT line into a FIXED line that names the unexported
// constant gating it (flip the constant to restore the old behaviour), and
// the body shows what runs now.
//
// Regenerate after an intended change:
//
//	go test ./pkg/characterization -run . -update
package characterization

import (
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/interfaces"
)

var update = flag.Bool("update", false, "rewrite golden files under testdata/ instead of comparing")

// baselineHeader is the first line of every golden in this package.
const baselineHeader = "# origin/main behaviour (baseline per maintainer decision 6)"

// trace is an ordered log of calls made through the file-system and command seams.
type trace struct {
	entries []traceEntry
}

type traceEntry struct {
	text    string
	content []string // file content lines, for writes
}

func (t *trace) add(format string, args ...any) {
	t.entries = append(t.entries, traceEntry{text: fmt.Sprintf(format, args...)})
}

// addContent attaches file content to the most recent entry, one line per line.
func (t *trace) addContent(data []byte) {
	e := &t.entries[len(t.entries)-1]
	if len(data) == 0 {
		e.content = append(e.content, "(empty)")
		return
	}
	s := string(data)
	trailingNewline := strings.HasSuffix(s, "\n")
	for _, line := range strings.Split(strings.TrimSuffix(s, "\n"), "\n") {
		if line == "" {
			e.content = append(e.content, "|")
		} else {
			e.content = append(e.content, "| "+line)
		}
	}
	if !trailingNewline {
		e.content = append(e.content, "\\ no newline at end of content")
	}
}

func (t *trace) String() string {
	if len(t.entries) == 0 {
		return "    (no calls)\n"
	}
	var b strings.Builder
	for i, e := range t.entries {
		fmt.Fprintf(&b, "%3d %s\n", i+1, e.text)
		for _, c := range e.content {
			b.WriteString("        " + c + "\n")
		}
	}
	return b.String()
}

// recordingFS wraps interfaces.MockFileSystem and logs every call.
type recordingFS struct {
	inner interfaces.FileSystem
	log   *trace
}

var _ interfaces.FileSystem = recordingFS{}

func (f recordingFS) ReadFile(name string) ([]byte, error) {
	data, err := f.inner.ReadFile(name)
	if err != nil {
		f.log.add("read   %s -> error: %v", name, err)
	} else {
		f.log.add("read   %s -> %d bytes", name, len(data))
	}
	return data, err
}

func (f recordingFS) WriteFile(name string, data []byte, perm fs.FileMode) error {
	err := f.inner.WriteFile(name, data, perm)
	f.log.add("write  %s mode=%s -> %s", name, mode(perm), errText(err))
	f.log.addContent(data)
	return err
}

func (f recordingFS) MkdirAll(path string, perm fs.FileMode) error {
	err := f.inner.MkdirAll(path, perm)
	f.log.add("mkdir  %s mode=%s -> %s", path, mode(perm), errText(err))
	return err
}

func (f recordingFS) Stat(name string) (os.FileInfo, error) {
	info, err := f.inner.Stat(name)
	switch {
	case err != nil && os.IsNotExist(err):
		f.log.add("stat   %s -> not-exist", name)
	case err != nil:
		f.log.add("stat   %s -> error: %v", name, err)
	case info.IsDir():
		f.log.add("stat   %s -> dir", name)
	default:
		f.log.add("stat   %s -> file", name)
	}
	return info, err
}

func (f recordingFS) Remove(name string) error {
	err := f.inner.Remove(name)
	f.log.add("remove %s -> %s", name, errText(err))
	return err
}

func (f recordingFS) RemoveAll(path string) error {
	err := f.inner.RemoveAll(path)
	f.log.add("rmall  %s -> %s", path, errText(err))
	return err
}

// recordingCommander wraps interfaces.MockCommander and logs every argv in order.
// afterExec, when set, runs after each command; fixtures use it to model host
// state that changes as a result of a command (for example, a user that exists
// only after adduser has run).
type recordingCommander struct {
	inner     interfaces.Commander
	log       *trace
	afterExec func(argv []string)
}

var _ interfaces.Commander = (*recordingCommander)(nil)

func (c *recordingCommander) Execute(command string, args ...string) ([]byte, error) {
	out, err := c.inner.Execute(command, args...)
	argv := append([]string{command}, args...)
	c.log.add("exec   %s -> %s", formatArgv(argv), errText(err))
	if c.afterExec != nil {
		c.afterExec(argv)
	}
	return out, err
}

func (c *recordingCommander) ExecuteWithInput(input string, command string, args ...string) ([]byte, error) {
	out, err := c.inner.ExecuteWithInput(input, command, args...)
	argv := append([]string{command}, args...)
	c.log.add("exec   %s stdin=%s -> %s", formatArgv(argv), strconv.Quote(input), errText(err))
	if c.afterExec != nil {
		c.afterExec(argv)
	}
	return out, err
}

// formatArgv renders argv with one space between elements; an element is
// quoted (Go syntax) when it is empty or contains anything a shell would split
// or interpret, so element boundaries stay unambiguous.
func formatArgv(argv []string) string {
	parts := make([]string, len(argv))
	for i, a := range argv {
		if a == "" || strings.ContainsAny(a, " \t\n\"'\\|&;<>()$`*?[]#~") {
			parts[i] = strconv.Quote(a)
		} else {
			parts[i] = a
		}
	}
	return strings.Join(parts, " ")
}

func mode(perm fs.FileMode) string {
	return fmt.Sprintf("%04o", uint32(perm.Perm()))
}

func errText(err error) string {
	if err == nil {
		return "ok"
	}
	return "error: " + err.Error()
}

// assertGolden compares got with testdata/golden/<rel>, or rewrites it under -update.
func assertGolden(t *testing.T, rel string, got string) {
	t.Helper()
	path := filepath.Join("testdata", "golden", filepath.FromSlash(rel))
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	wantBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v (run with -update to create it)", path, err)
	}
	want := string(wantBytes)
	if got == want {
		return
	}
	wl, gl := strings.Split(want, "\n"), strings.Split(got, "\n")
	for i := 0; i < len(wl) || i < len(gl); i++ {
		var w, g string
		if i < len(wl) {
			w = wl[i]
		}
		if i < len(gl) {
			g = gl[i]
		}
		if w != g {
			t.Fatalf("golden %s differs at line %d\nwant: %q\ngot:  %q\n(if the change is intended, rerun with -update and explain the diff in the review description)", path, i+1, w, g)
		}
	}
	t.Fatalf("golden %s differs (line endings)", path)
}
