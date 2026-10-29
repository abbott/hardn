package characterization

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/adapter/secondary"
	"github.com/abbott/hardn/pkg/application"
	"github.com/abbott/hardn/pkg/domain/service"
	"github.com/abbott/hardn/pkg/interfaces"
)

// The Environment area below its screen, refactor plan slice 9's
// evidence (docs/engine/gallery-environment.md §2): what item 1 of the
// Environment screen (main menu 8) does as root, hardn -e and hardn
// setup-sudo-env, all three one call, SetupSudoPreservation
// (TestLegacyEnvironmentKeep). The goldens are
// testdata/golden/environment/keep_*. The ops goldens of
// setup_sudo_preservation (testdata/golden/ops/setup_sudo_preservation)
// already hold the one start they record, alice's grant there, and are
// not recorded again here. The file system helpers here (rootedFS,
// envSeed, snapshot) serve the screen's test too
// (menu_environment_test.go).

// envTmp stands for the case's temp dir in the goldens: the root, /, of
// the file system the repository is given.
const envTmp = "$TMP"

// The sudoers lines of the cases: alice's grant as ConfigureSudo writes it
// (ops/configure_sudo), and the line SetupSudoPreservation adds
// (file_environment_repository.go:69).
const (
	grantLine = "alice ALL=(ALL) NOPASSWD: ALL\n"
	keepLine  = "Defaults:alice env_keep += \"HARDN_CONFIG\"\n"
)

// envSeed is one thing a case's root holds before it runs: a directory, a
// file and its content, or a symlink, with the mode it is given (the
// umask aside). Parents are seeded first, each explicitly.
type envSeed struct {
	path    string // hardn's path, absolute
	dir     bool
	content string
	link    string // a symlink's target, relative, so it resolves inside the root
	mode    fs.FileMode
}

// hostDirs are what every case's root holds: /etc, /etc/sudoers.d as
// Debian's sudo package leaves it, and /tmp, anyone's to write in.
func hostDirs() []envSeed {
	return []envSeed{
		{path: "/etc", dir: true, mode: 0o755},
		{path: "/etc/sudoers.d", dir: true, mode: 0o750},
		{path: "/tmp", dir: true, mode: 0o777 | fs.ModeSticky},
	}
}

// withFile is hostDirs and a file at path.
func withFile(path, content string, mode fs.FileMode) []envSeed {
	return append(hostDirs(), envSeed{path: path, content: content, mode: mode})
}

// seedRoot builds seeds under root, each mode set past the umask.
func seedRoot(t *testing.T, root string, seeds []envSeed) {
	t.Helper()
	for _, s := range seeds {
		p := filepath.Join(root, filepath.FromSlash(s.path))
		var err error
		switch {
		case s.dir:
			if err = os.Mkdir(p, 0o700); err == nil {
				err = os.Chmod(p, s.mode)
			}
		case s.link != "":
			err = os.Symlink(s.link, p)
		default:
			if err = os.WriteFile(p, []byte(s.content), 0o600); err == nil {
				err = os.Chmod(p, s.mode)
			}
		}
		if err != nil {
			t.Fatal(err)
		}
	}
}

// rootedFS is the real file system under root, a temp dir standing for
// /: each of hardn's absolute paths is used at root+path, through
// interfaces.OSFileSystem, the file system infrastructure.ServiceFactory
// gives the repository (service_factory.go:221, the provider's). An error
// names hardn's path, root cut from it; a path that would leave root is
// refused.
type rootedFS struct{ root string }

var _ interfaces.FileSystem = rootedFS{}

func (r rootedFS) at(name string) (string, error) {
	p := filepath.Join(r.root, filepath.FromSlash(name))
	if p != r.root && !strings.HasPrefix(p, r.root+string(filepath.Separator)) {
		return "", fmt.Errorf("characterization: %s is outside the temp root", name)
	}
	return p, nil
}

// hardnPath gives err's path as hardn named it.
func (r rootedFS) hardnPath(err error) error {
	pe, ok := err.(*fs.PathError)
	if !ok {
		return err
	}
	cp := *pe
	cp.Path = "/" + strings.TrimPrefix(strings.TrimPrefix(filepath.ToSlash(pe.Path), filepath.ToSlash(r.root)), "/")
	return &cp
}

func (r rootedFS) ReadFile(name string) ([]byte, error) {
	p, err := r.at(name)
	if err != nil {
		return nil, err
	}
	data, err := interfaces.OSFileSystem{}.ReadFile(p)
	return data, r.hardnPath(err)
}

func (r rootedFS) WriteFile(name string, data []byte, perm fs.FileMode) error {
	p, err := r.at(name)
	if err != nil {
		return err
	}
	return r.hardnPath(interfaces.OSFileSystem{}.WriteFile(p, data, perm))
}

func (r rootedFS) MkdirAll(path string, perm fs.FileMode) error {
	p, err := r.at(path)
	if err != nil {
		return err
	}
	return r.hardnPath(interfaces.OSFileSystem{}.MkdirAll(p, perm))
}

func (r rootedFS) Stat(name string) (os.FileInfo, error) {
	p, err := r.at(name)
	if err != nil {
		return nil, err
	}
	fi, err := interfaces.OSFileSystem{}.Stat(p)
	return fi, r.hardnPath(err)
}

func (r rootedFS) Remove(name string) error {
	p, err := r.at(name)
	if err != nil {
		return err
	}
	return r.hardnPath(interfaces.OSFileSystem{}.Remove(p))
}

func (r rootedFS) RemoveAll(path string) error {
	p, err := r.at(path)
	if err != nil {
		return err
	}
	return r.hardnPath(interfaces.OSFileSystem{}.RemoveAll(p))
}

// fsEntry is one path under a root: its kind, mode, and a file's content
// or a symlink's target.
type fsEntry struct {
	kind, content, link string
	mode                uint32
}

// snapshot lists what is under root, by hardn's path, symlinks not
// followed.
func snapshot(t *testing.T, root string) map[string]fsEntry {
	t.Helper()
	out := map[string]fsEntry{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || p == root {
			return err
		}
		fi, err := os.Lstat(p)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		mode := uint32(fi.Mode().Perm())
		if fi.Mode()&fs.ModeSticky != 0 {
			mode |= 0o1000
		}
		e := fsEntry{kind: "file", mode: mode}
		switch {
		case fi.IsDir():
			e.kind = "dir"
		case fi.Mode()&fs.ModeSymlink != 0:
			e.kind = "symlink"
			if e.link, err = os.Readlink(p); err != nil {
				return err
			}
		default:
			data, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			e.content = string(data)
		}
		out["/"+filepath.ToSlash(rel)] = e
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// describe says what an entry is: a directory, a symlink and its target,
// or a file, with its mode unless it is new (a new entry's mode is the
// umask's; the mode asked for is in the calls).
func describe(e fsEntry, withMode bool) string {
	var s string
	switch e.kind {
	case "dir":
		s = "a directory"
	case "symlink":
		s = "a symlink to " + e.link
	default:
		s = fmt.Sprintf("a file, %d bytes", len(e.content))
	}
	if withMode && e.kind != "symlink" {
		s += fmt.Sprintf(", mode %04o", e.mode)
	}
	return s
}

// rootChanges says how root changed from before to after: each path new,
// changed or gone, a file's content as it now is, then the paths
// unchanged; or "unchanged".
func rootChanges(before, after map[string]fsEntry) string {
	paths := map[string]bool{}
	for p := range before {
		paths[p] = true
	}
	for p := range after {
		paths[p] = true
	}
	sorted := make([]string, 0, len(paths))
	for p := range paths {
		sorted = append(sorted, p)
	}
	sort.Strings(sorted)
	var b strings.Builder
	var same []string
	for _, p := range sorted {
		was, had := before[p]
		now, has := after[p]
		switch {
		case had && has && was == now:
			same = append(same, p)
			continue
		case !had:
			fmt.Fprintf(&b, "%s: new, %s\n", p, describe(now, false))
		case !has:
			fmt.Fprintf(&b, "%s: gone (was %s)\n", p, describe(was, true))
			continue
		default:
			fmt.Fprintf(&b, "%s: changed, %s (was %s)\n", p, describe(now, true), describe(was, true))
		}
		if now.kind == "file" {
			b.WriteString(contentLines(now.content))
		}
	}
	if b.Len() == 0 {
		return "unchanged\n"
	}
	if len(same) > 0 {
		fmt.Fprintf(&b, "unchanged: %s\n", strings.Join(same, " "))
	}
	return b.String()
}

// seedLines lists a root's start, by hardn's path, each file's content
// under it.
func seedLines(seeds []envSeed) string {
	var b strings.Builder
	for _, s := range seeds {
		e := fsEntry{kind: "file", content: s.content, link: s.link, mode: uint32(s.mode.Perm())}
		if s.mode&fs.ModeSticky != 0 {
			e.mode |= 0o1000
		}
		switch {
		case s.dir:
			e.kind = "dir"
		case s.link != "":
			e.kind = "symlink"
		}
		fmt.Fprintf(&b, "%s: %s\n", s.path, describe(e, true))
		if e.kind == "file" {
			b.WriteString(contentLines(s.content))
		}
	}
	return b.String()
}

// contentLines is a file's content as the traces show it.
func contentLines(content string) string {
	tr := &trace{}
	tr.add("")
	tr.addContent([]byte(content))
	var b strings.Builder
	for _, c := range tr.entries[0].content {
		b.WriteString("        " + c + "\n")
	}
	return b.String()
}

// envVars are the variables a case runs with, "" for unset: the user the
// repository names (SUDO_USER, else USER: file_environment_repository.go
// :135-138) and HARDN_CONFIG, which the screen shows (environment_settings
// _menu.go:37).
type envVars struct {
	sudoUser, user, hardnConfig string
}

func (v envVars) words() string {
	val := func(s string) string {
		if s == "" {
			return "unset"
		}
		return s
	}
	return fmt.Sprintf("SUDO_USER %s, USER %s, HARDN_CONFIG %s, TMPDIR unset (os.TempDir() is /tmp)", val(v.sudoUser), val(v.user), val(v.hardnConfig))
}

// setenv sets v in this process for the test, each "" unset, and TMPDIR
// unset.
func (v envVars) setenv(t *testing.T) {
	t.Helper()
	for k, val := range map[string]string{"SUDO_USER": v.sudoUser, "USER": v.user, "HARDN_CONFIG": v.hardnConfig, "TMPDIR": ""} {
		t.Setenv(k, val)
		if val == "" {
			if err := os.Unsetenv(k); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// keepCase is one start of SetupSudoPreservation: the variables, the
// root's start and what it shows, the commands that fail.
type keepCase struct {
	name, start string
	vars        envVars
	seeds       []envSeed
	// fail answers each named command with an error, as the mock
	// commander does (CommandErrors).
	fail           map[string]error
	notes, defects []string
}

// keepCases are SetupSudoPreservation's starts but the one the ops goldens
// record (alice's grant there, ops/setup_sudo_preservation): a user with
// no file yet; the line there already, and commented out; visudo
// refusing; the temp file's name taken by a directory and by a symlink;
// no /etc/sudoers.d; root as the user; no user; a user name with a dot.
func keepCases() []keepCase {
	const (
		tmpName  = "The temp file is a fixed name, os.TempDir()/hardn_sudoers_temp, written by os.WriteFile as root (file_environment_repository.go:72-75), so anyone who can write to /tmp can take the name first."
		atomic   = "V18: the sudoers file is written in place by os.WriteFile, no rename, nothing backed up first (file_environment_repository.go:96; interfaces/os_filesystem.go:22-33)."
		shared   = "V16: the file is /etc/sudoers.d/<user>, the one ConfigureSudo writes a grant to when that user is the one hardn manages (os_user_repository.go:232-263; side-effects.md A13): with the grant there the line is appended to it (ops/setup_sudo_preservation/*.golden), and the next grant overwrites the file, the line lost (ops/configure_sudo/*.golden)."
		noBackup = "Nothing is backed up or rolled back; a failure after the temp write leaves the temp file removed and the sudoers file as it was (file_environment_repository.go:79-98)."
	)
	sudoUser := envVars{sudoUser: "alice", user: "root"}
	return []keepCase{
		{name: "keep_new", start: "alice has no file in /etc/sudoers.d", vars: sudoUser, seeds: hostDirs(),
			defects: []string{"V20: " + tmpName, atomic, shared},
			notes:   []string{"visudo checks the temp file alone (-c -f), then the file is written from the same bytes: the only sudoers write origin/main checks (side-effects.md row 18)."}},
		{name: "keep_already", start: "alice's file holds her grant and the line", vars: sudoUser, seeds: withFile("/etc/sudoers.d/alice", grantLine+keepLine, 0o440),
			notes: []string{"The line found, nothing is written and the call succeeds (file_environment_repository.go:59-62)."}},
		{name: "keep_commented", start: "alice's file holds her grant and the line commented out", vars: sudoUser, seeds: withFile("/etc/sudoers.d/alice", grantLine+"# "+keepLine, 0o440),
			defects: []string{"A commented-out line counts as kept: the check is strings.Contains(content, `env_keep += \"HARDN_CONFIG\"`) (file_environment_repository.go:60), so nothing is written and the call succeeds, while sudo reads the line as a comment [I, sudoers(5)] and does not keep the variable."}},
		{name: "keep_visudo_fails", start: "alice's file holds a grant visudo cannot parse (a parenthesis missing)", vars: sudoUser,
			seeds: withFile("/etc/sudoers.d/alice", "alice ALL=(ALL NOPASSWD: ALL\n", 0o440),
			fail:  map[string]error{"visudo -c -f /tmp/hardn_sudoers_temp": errors.New("exit status 1")},
			notes: []string{"fixture: visudo answers \"exit status 1\", as it does for a file it cannot parse [I]; the repository passes no output on (file_environment_repository.go:79-86).", noBackup}},
		{name: "keep_tmp_dir", start: "another user's directory at /tmp/hardn_sudoers_temp", vars: sudoUser,
			seeds:   append(hostDirs(), envSeed{path: "/tmp/hardn_sudoers_temp", dir: true, mode: 0o755}),
			defects: []string{"V20: " + tmpName + " Here a directory of that name makes the first write fail: any local user can stop hardn -e, setup-sudo-env and item 1 this way."}},
		{name: "keep_tmp_symlink", start: "another user's symlink at /tmp/hardn_sudoers_temp, to /etc/hosts", vars: sudoUser,
			seeds: append(hostDirs(),
				envSeed{path: "/etc/hosts", content: "127.0.0.1 localhost\n", mode: 0o644},
				envSeed{path: "/tmp/hardn_sudoers_temp", link: "../etc/hosts"}),
			defects: []string{"V20: " + tmpName + " os.WriteFile follows a symlink of that name: /etc/hosts is overwritten with the sudoers lines, visudo is run on the link (here the mock, answering success), and the remove takes the link alone, so /etc/hosts stays overwritten. On Linux, fs.protected_symlinks, which Debian and Ubuntu turn on [I], refuses root this open of another user's link in a sticky world-writable directory [I], so there the write fails instead [I, not run as root on Linux]."},
			notes:   []string{"The link is relative, ../etc/hosts, so it resolves inside the temp root; one planted on a host would name /etc/hosts."}},
		{name: "keep_no_sudoers_d", start: "no /etc/sudoers.d (a host without sudo, such as Alpine with doas only [I])", vars: sudoUser,
			seeds: []envSeed{{path: "/etc", dir: true, mode: 0o755}, {path: "/tmp", dir: true, mode: 0o777 | fs.ModeSticky}},
			notes: []string{"The repository refuses before it writes (file_environment_repository.go:39-43); it never looks for doas."}},
		{name: "keep_root", start: "alice's file holds her grant", vars: envVars{user: "root"}, seeds: withFile("/etc/sudoers.d/alice", grantLine, 0o440),
			defects: []string{"With SUDO_USER unset the user is USER (file_environment_repository.go:135-138), root under doas, which names its caller in DOAS_USER [I, doas(1)], or at a root login: the line is written for root, to /etc/sudoers.d/root, and alice's sudo still drops the variable. Under doas no sudoers line keeps it at all [I].", "V20: " + tmpName}},
		{name: "keep_no_user", start: "alice's file holds her grant", vars: envVars{}, seeds: withFile("/etc/sudoers.d/alice", grantLine, 0o440),
			defects: []string{"With neither SUDO_USER nor USER set the call succeeds with nothing read or written (environment_service.go:45-47), and each caller says it is done (below)."}},
		{name: "keep_dotted", start: "alice.smith has no file in /etc/sudoers.d", vars: envVars{sudoUser: "alice.smith", user: "root"}, seeds: hostDirs(),
			defects: []string{"V16: the file name is the raw user name (file_environment_repository.go:46): /etc/sudoers.d/alice.smith, a name sudo skips because it holds a dot [I, sudoers(5) \"#includedir\"], so the line is written and never read.", "V20: " + tmpName}},
	}
}

// TestLegacyEnvironmentKeep records SetupSudoPreservation, in this
// process, as the Environment screen's item 1 calls it as root
// (MenuManager.SetupSudoPreservation, environment_settings_menu.go:90),
// on each start of keepCases. The repository is built as
// infrastructure.ServiceFactory.CreateEnvironmentManager builds it
// (service_factory.go:219-228), over the real file system under the
// case's temp dir, which stands for / ($TMP in the goldens), each call
// recorded with hardn's own path; commands go to the mock commander,
// recorded, every one answering success unless the case fails it.
// hardn -e and setup-sudo-env call the same function through
// EnvironmentManager (cmd/hardn/main.go:423, :470). What each caller
// prints for the result is read from the code, not run: the screen's
// root branch cannot run here (the test is not root; the screen checks
// os.Geteuid, environment_settings_menu.go:82), and cmd/hardn is not
// importable. The test runs as a user other than root, so a write to a
// seeded file it cannot write would fail where root's would not: the
// cases write only to files that are new or the test's to write.
func TestLegacyEnvironmentKeep(t *testing.T) {
	for _, tc := range keepCases() {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			seedRoot(t, root, tc.seeds)
			tc.vars.setenv(t)
			before := snapshot(t, root)

			log := &trace{}
			mc := interfaces.NewMockCommander()
			for argv, err := range tc.fail {
				mc.CommandErrors[argv] = err
			}
			repo := secondary.NewFileEnvironmentRepository(recordingFS{inner: rootedFS{root: root}, log: log}, &recordingCommander{inner: mc, log: log})
			env := application.NewEnvironmentManager(service.NewEnvironmentServiceImpl(repo))
			mm := application.NewMenuManager(nil, nil, nil, nil, nil, nil, env, nil, nil)
			callErr := mm.SetupSudoPreservation()

			var b strings.Builder
			fmt.Fprintf(&b, backupHeader, "TestLegacyEnvironmentKeep")
			b.WriteString("# Operation: MenuManager.SetupSudoPreservation, item 1 of the Environment screen as root (environment_settings_menu.go:90); hardn -e and setup-sudo-env call EnvironmentManager.SetupSudoPreservation, the same function (cmd/hardn/main.go:423, :470)\n")
			fmt.Fprintf(&b, "# Environment: %s\n", tc.vars.words())
			fmt.Fprintf(&b, "# Start: %s\n", tc.start)
			for _, d := range tc.defects {
				fmt.Fprintf(&b, "# KNOWN-DEFECT: %s\n", d)
			}
			for _, n := range tc.notes {
				fmt.Fprintf(&b, "# NOTE: %s\n", n)
			}
			b.WriteString("# The file system is the real one under the case's temp dir, which stands for /; each call is recorded with hardn's path. Commands go to the mock commander, recorded.\n")
			b.WriteString("\n== / before\n")
			b.WriteString(seedLines(tc.seeds))
			if len(tc.fail) > 0 {
				b.WriteString("commands that fail (every other command succeeds with empty output):\n")
				for _, argv := range sortedKeys(tc.fail) {
					fmt.Fprintf(&b, "  %s -> fixture: %v\n", argv, tc.fail[argv])
				}
			}
			fmt.Fprintf(&b, "\n== result\nSetupSudoPreservation: %s\n", errText(callErr))
			b.WriteString(callerWords(callErr))
			fmt.Fprintf(&b, "\n== calls\n%s", log)
			fmt.Fprintf(&b, "\n== / afterwards\n%s", rootChanges(before, snapshot(t, root)))

			got := strings.ReplaceAll(b.String(), root, envTmp)
			if resolved, err := filepath.EvalSymlinks(root); err == nil && strings.Contains(got, resolved) {
				t.Fatalf("golden would contain the temp dir %s", resolved)
			}
			assertGolden(t, "environment/"+tc.name+".golden", got)
		})
	}
}

// callerWords is what each caller prints for err, read from the code.
func callerWords(err error) string {
	if err == nil {
		return "screen, item 1 as root (read, not run: environment_settings_menu.go:93-94): \"Successfully configured sudo to preserve HARDN_CONFIG\"\n" +
			"hardn -e and setup-sudo-env (read, not run: cmd/hardn/main.go:427, :474): \"[SUCCESS] Sudo environment configured to preserve HARDN_CONFIG\", exit status 0\n"
	}
	return fmt.Sprintf("screen, item 1 as root (read, not run: environment_settings_menu.go:91-92): \"Failed to configure sudo: %v\"\n", err) +
		fmt.Sprintf("hardn -e and setup-sudo-env (read, not run: cmd/hardn/main.go:423-425, :470-472): \"[ERROR] Failed to configure sudoers: %v\", exit status 1\n", err)
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
