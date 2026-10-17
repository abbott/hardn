# System: the host seam (`pkg/host`)

> **Unit:** refactor plan Phase 2, `next/p2-system` **[plan adjustment: System v0 moved from P3 into P2]**.
> **Status:** built and tested on macOS with the recording fake and on real files in temporary directories only. Everything that needs a Linux host is listed in §4 as "written, not run (gate 5)".

`host.System` is the one seam between hardn and the machine it hardens. Every Phase 2 primitive (atomic write, sshd validation, sudoers, UFW, reload, revert, Proxmox, Alpine, DNS) and the Phase 3 engine (`Change.Apply(ctx, sys)`, brief Phase 3 sketch) reach the host through it. `host.Real` is the machine; `hostfake.System` is a recording, in-memory host for tests. The legacy `interfaces.Commander` and `interfaces.FileSystem` are adapters over a `System`, so legacy and new code share one execution path.

## 1. Why

The Phase 0 audit found every hardening mutation going through `interfaces.Commander`/`FileSystem` with no context, no stderr, no exit code, non-atomic symlink-following writes and no record of what ran ([phase0-findings.md §6](../refactor/phase0-findings.md#6-architecture-map-originmain-the-worktree-base); V18, V20, V21 in [side-effects.md](../refactor/phase0/side-effects.md)). The primitives that keep a user from being locked out need a seam that reports exit codes faithfully, never involves a shell, never follows a symlink it was not asked to, and logs every command.

## 2. Guarantees

### Commands (`Run`, `RunInput`)

1. **One argv, never a shell.** `name` and `args` go to `exec.CommandContext` as separate argv elements; spaces, quotes, `;`, `$(...)` and globs carry no meaning. Go's os/exec never invokes a shell and expands nothing ([pkg.go.dev/os/exec](https://pkg.go.dev/os/exec), package overview). Tested through the exec seam with a command file literally named `a b;c` and metacharacter arguments; nothing executes.
2. **Fixed resolution.** A name without a slash is looked up only in `host.SearchPath` = `/usr/sbin:/usr/bin:/sbin:/bin`, in that order; the first regular file with an execute bit wins. An absolute path is used as is; a relative path (`./x`, `a/b`) is refused. Otherwise `ErrNotFound`, and nothing runs. `/usr/local` is left out on purpose: hardn runs packaged tools only.
3. **Pinned environment.** The command's whole environment is `PATH=/usr/sbin:/usr/bin:/sbin:/bin` and `LC_ALL=C`. Nothing is inherited from whoever started hardn (no `IFS`, `ENV`, `BASH_ENV`, `LD_PRELOAD`, `PERL5LIB` or locale), and output is in the C locale, so parsers see one language. The working directory is `/`.
4. **A non-zero exit is an error.** It returns `*host.ExitError` (exit code, argv, output tail; `Unwrap` gives the `*exec.ExitError`), so a failed check such as `sshd -t` exiting 255 cannot read as a pass. The `Result` is filled in on every return: `Stdout`, `Stderr`, `Combined` (both, in write order), `ExitCode` (-1 when it did not start or was ended by a signal) and `Tail` (the last `Options.TailLines` lines of `Combined`, default 20).
5. **Context.** A done context before the start runs nothing; during the run, `exec.CommandContext` kills the process ([pkg.go.dev/os/exec#CommandContext](https://pkg.go.dev/os/exec#CommandContext)) and `Run` returns an error wrapping `ctx.Err()`. `WaitDelay` is 10 s, so once the command has exited or been killed, a daemon it started that still holds the output pipes cannot hang `Run` ([pkg.go.dev/os/exec#Cmd](https://pkg.go.dev/os/exec#Cmd), field `WaitDelay`); exit 0 with pipes held past it is still success, with the later output lost.
6. **Every run is logged.** `host.RunLog` writes one entry per command to the `io.Writer` given at construction; every line starts with `run <n>: `, so overlapping runs stay apart:

   ```
   run 3: $ ufw allow 2230/tcp
   run 3: | Rule added
   run 3: exit 0
   ```

   Stdin is recorded by length only (`stdin: 42 bytes (not logged)`), never its content: it can hold a key or a password. Logging is **best effort**: a failing writer never stops a command, because a rollback must still run on a full disk; `Real.LogErr` returns the first write error. The log's path and rotation are `p3-output`'s business: until then the legacy path discards it.

### Files and accounts

7. **`WriteFileAtomic` refuses a symlink.** A symlink at the target, dangling or not, is refused with `ErrSymlink` and left untouched with its target; so is a directory (`EISDIR`). It writes a new file in the target's directory (`os.CreateTemp`), sets mode and owner, and renames it over the target, so readers see old or new content, never a mix, and the new file has exactly the given mode and owner. `rename(2)` overwrites a symlink at the new path itself rather than writing through it ([rename(2)](https://man7.org/linux/man-pages/man2/rename.2.html)), so a link planted after the check is not followed either. **This is the naive version** (`TODO(next/p2-atomic)`): no fsync of the file or the directory, no backups, no manifest; `p2-atomic` replaces it. Parent directories are followed; whether to refuse a symlinked parent is `p2-atomic`'s call.
8. **`Chown` never follows a symlink.** It refuses one with `ErrSymlink`, and changes the path itself with `lchown(2)`, which never dereferences a symlink ([chown(2)](https://man7.org/linux/man-pages/man2/chown.2.html)). V21 is root chowning through a user-controlled `~/.ssh`.
9. **Reads follow symlinks; `Lstat` does not.** `ReadFile`, `Stat`, `ReadDir` (sorted by name) are the os package's; `Remove`, `RemoveAll` and `Rename` act on a symlink itself.
10. **No in-place write in `System`.** `os.WriteFile` semantics (truncate, follow the symlink, keep the mode) exist only as `LegacySystem.WriteFileInPlace`, for the legacy `interfaces.FileSystem` adapter; new code takes a `System` and cannot reach it.
11. **Accounts.** `LookupUser` and `LookupGroup` use os/user and return `ErrUnknownUser` / `ErrUnknownGroup`. Release builds are `CGO_ENABLED=0` (`.slsa-goreleaser/*.yml`), so os/user uses its pure-Go implementation, which reads `/etc/passwd` and `/etc/group` itself ([pkg.go.dev/os/user](https://pkg.go.dev/os/user)): accounts that only NSS knows (LDAP, sssd) are not found.
12. **Service control is a `Run` call** (`systemctl reload ssh`, `rc-service sshd reload`). There is no systemd API here. Which unit, which verb and which init system is `p2-detect`'s and `p2-reload`'s: the service is `ssh` on Debian, Ubuntu and Proxmox and `sshd` under OpenRC on Alpine, and the `sshd` unit alias is missing on fresh Ubuntu 24.04 ([phase0-findings.md §10](../refactor/phase0-findings.md#10-verify-list-brief-9), "Service names / init"; plan `p2-reload`).

### The fake (`pkg/host/hostfake`)

13. **Scripted commands, in the mocks' style.** `Respond(Response{Stdout, Stderr, ExitCode, Err}, name, args...)` scripts one argv; several responses for one argv are used in order and the last repeats. A non-zero `ExitCode` gives `*host.ExitError`; `Err` is returned verbatim, as `MockCommander` returns its errors. Unscripted commands go to `Handler` if set (it runs without the fake's lock, so it can model a command's effect on the fake), else succeed with no output, as `MockCommander`'s do.
14. **An ordered record** of every call (`Calls`, `Runs`; `Call.String` for failure messages), stdin and written data included, and the same run log as `Real`.
15. **An in-memory file system with modes, owners and symlinks**, seeded with `AddFile`, `AddDir`, `AddSymlink` and inspected with `Entry` (neither recorded). `host.Owner(fi)` reads the owner from either System's `fs.FileInfo`. A contract test runs the same 13 cases against `host.Real` in `t.TempDir()` and against the fake: write in place through a symlink, atomic write refusing a symlink, rename over a symlink, remove and remove-all on links and non-empty directories, stat versus lstat, sorted directory listings, mkdir over a file, chown refusing a link. The fake has no umask, no hard links and no permission checks (it acts as root), resolves `..` in a link target lexically, follows at most 40 links (`ELOOP`, Linux's limit), and refuses relative paths (`fs.ErrInvalid`).

## 3. Platform facts relied on

| Fact | Source | Used for |
|---|---|---|
| Proxmox's root `authorized_keys` is a symlink into `/etc/pve/priv` (cluster-wide) | [phase0-findings.md §10](../refactor/phase0-findings.md#10-verify-list-brief-9), "Proxmox join / detection" **[V]**, primary source pve-docs; plan R15 | §2.7: never write through or rename over a symlink |
| Debian/Ubuntu/PVE service `ssh`, Alpine OpenRC `sshd`; no `sshd` alias on fresh Ubuntu 24.04 | phase0-findings.md §10, "Service names / init" **[V]** | §2.12: service control stays a `Run` call chosen by later units |
| `pveversion` is `/usr/bin/pveversion` | phase0-findings.md §10, "Proxmox join / detection" **[V]** | inside `SearchPath` |
| The other commands hardn runs (`sshd`, `ufw`, `visudo`, `systemctl`, `rc-service`, `apk`, `adduser`, `usermod`, `su`, `sudo`/`doas`, ...) are all installed in `/usr/sbin`, `/usr/bin`, `/sbin` or `/bin` on every supported release, Alpine's busybox applets included | **[unverified]**: no primary source collected; checked on the VMs (§4) | §2.2: nothing outside `SearchPath` |
| Busybox applets dispatch on `argv[0]` | **[unverified]** | `argv[0]` is the name as given, as `exec.Command` sets it |
| Go facts: os/exec never invokes a shell; `CommandContext` kills on cancel; `WaitDelay`; os/user's pure-Go implementation without cgo; `CreateTemp` creates a new file | [pkg.go.dev/os/exec](https://pkg.go.dev/os/exec), [pkg.go.dev/os/user](https://pkg.go.dev/os/user), [pkg.go.dev/os#CreateTemp](https://pkg.go.dev/os#CreateTemp) | §2.1, 2.5, 2.7, 2.11 |
| `rename(2)` replaces a symlink at the new path; `lchown(2)` does not dereference | [rename(2)](https://man7.org/linux/man-pages/man2/rename.2.html), [chown(2)](https://man7.org/linux/man-pages/man2/chown.2.html) | §2.7, 2.8 |

## 4. Written, not run (gate 5)

Nothing here has run on Linux. The tests run on macOS as an unprivileged user: commands only through the exec seam (no process is started), files only inside `t.TempDir()`, chown only to the test's own uid. On the Debian 13, Ubuntu 26.04 and Alpine 3.24 VMs (`next/p2-vmtest`), and by hand on Proxmox VE 9:

- every command the legacy code and the Phase 2 units run resolves from `SearchPath` (§3, unverified row), including Alpine's busybox applets and Ubuntu 26.04's `sudo` (sudo-rs);
- the legacy operations behave the same with the pinned environment and `/` as working directory (§5): `adduser`, `usermod`, `su - alice -c ...`, `sh -c "yes | ufw enable"`, `visudo -c -f`, `systemctl`, `rc-service`, `resolvconf -u`;
- `WriteFileAtomic` and `Chown` as root, with real owners, and `WriteFileAtomic` refusing `/root/.ssh/authorized_keys` on Proxmox;
- `LookupUser`/`LookupGroup` in the static (`CGO_ENABLED=0`) release build;
- `WaitDelay` with `rc-service sshd restart` and `systemctl restart`, which start daemons;
- the run log through `p3-output`'s writer.

## 5. The legacy adapters and the one execution path

`interfaces.SystemCommander{Sys}` and `interfaces.SystemFileSystem{Sys}` implement the legacy interfaces over a `System`/`LegacySystem`. `SystemCommander` returns `Result.Combined`, as `CombinedOutput` did, and unwraps `*host.ExitError` to the `*exec.ExitError` behind it, so legacy callers still see `exit status 1`. `SystemFileSystem.WriteFile` keeps `OSFileSystem`'s semantics: create the parent (0755), then write in place, through a symlink.

`interfaces.OSCommander` and `OSFileSystem`, which `NewProvider` and `osdetect` construct, now delegate to one shared `host.Real` behind `const legacyThroughHost = true` in `pkg/interfaces/system_adapter.go`. Their old bodies are kept: **flip it to `false` to restore the pre-P2 calls.** With it on, legacy production commands change in these ways, all intended and all **written, not run** (§4):

- resolved from `SearchPath` only (was: hardn's `PATH`; with sudo, its `secure_path`). Of the commands the legacy adapters run, only `uv`, which the Python package path installs with `pip3 install uv`, is likely to land outside it, in `/usr/local/bin` **[unverified]**; that path is unreachable on origin/main ([phase0-findings.md §6](../refactor/phase0-findings.md#6-architecture-map-originmain-the-worktree-base), "Dead or unreachable");
- the environment is exactly `PATH` and `LC_ALL=C` (was: inherited whole), and the working directory `/`;
- a command not found reports `host: "x": not in /usr/sbin:/usr/bin:/sbin:/bin: command not found` (was: os/exec's "executable file not found in $PATH");
- after exit, at most 10 s of waiting for held pipes (was: unbounded).

File operations make the same os calls as before.

**Goldens.** The P0.7 characterization goldens pass unchanged (no `-update`). They record the seam itself, so they also run a second time through the adapters: `TestOperationGoldensThroughSystem` and `TestHostInfoGoldenThroughSystem` wrap `SystemFileSystem`/`SystemCommander` over a `hostfake.System` scripted from the same mock maps and compare byte for byte with the same golden files (compare-only; `-update` regenerates from the mocks). A deliberately broken adapter fails them. The environment and resolution changes above sit below the seam, in `host.Real`, so no golden can show them; §4 covers them.

## 6. API

```go
package host

const SearchPath = "/usr/sbin:/usr/bin:/sbin:/bin"
const DefaultTailLines = 20

type System interface {
	Run(ctx context.Context, name string, args ...string) (Result, error)
	RunInput(ctx context.Context, stdin []byte, name string, args ...string) (Result, error)
	ReadFile(ctx context.Context, name string) ([]byte, error)
	Stat(ctx context.Context, name string) (fs.FileInfo, error)
	Lstat(ctx context.Context, name string) (fs.FileInfo, error)
	ReadDir(ctx context.Context, name string) ([]fs.DirEntry, error)
	MkdirAll(ctx context.Context, name string, perm fs.FileMode) error
	Remove(ctx context.Context, name string) error
	RemoveAll(ctx context.Context, name string) error
	Rename(ctx context.Context, oldname, newname string) error
	Chown(ctx context.Context, name string, uid, gid int) error // -1 unchanged; refuses symlinks
	WriteFileAtomic(ctx context.Context, name string, data []byte, perm fs.FileMode, uid, gid int) error
	LookupUser(ctx context.Context, name string) (User, error)
	LookupGroup(ctx context.Context, name string) (Group, error)
}

type LegacySystem interface {
	System
	WriteFileInPlace(ctx context.Context, name string, data []byte, perm fs.FileMode) error
}

type Result struct { Argv []string; Stdout, Stderr, Combined []byte; ExitCode int; Tail []string }
type ExitError struct { Argv []string; ExitCode int; Tail []string; Err error } // Unwrap() error
type User struct { Name string; UID, GID int; HomeDir string }
type Group struct { Name string; GID int }
type Ownership struct { UID, GID int } // the fake's fs.FileInfo.Sys()
type Options struct { RunLog io.Writer; TailLines int }

var ErrNotFound, ErrSymlink, ErrUnknownUser, ErrUnknownGroup error

func NewReal(Options) *Real            // implements LegacySystem; LogErr() error
func NewRunLog(io.Writer) *RunLog      // Begin(argv, stdinLen) int; End(id, Result, error); Err() error
func Owner(fs.FileInfo) (uid, gid int, ok bool)
func FormatArgv([]string) string
func TailLines(out []byte, n int) []string

package hostfake

func New(host.Options) *System         // implements host.LegacySystem
type Response struct { Stdout, Stderr string; ExitCode int; Err error }
type Call struct { Op string; Argv []string; Stdin []byte; ExitCode int; Path, To string; Data []byte; Mode fs.FileMode; UID, GID int; Err error }
type Entry struct { Mode fs.FileMode; Data []byte; Target string; UID, GID int }
// (*System) Respond, Handler, AddFile, AddDir, AddSymlink, AddUser, AddGroup, Entry, Calls, Runs

package interfaces

type SystemCommander struct{ Sys host.System }
type SystemFileSystem struct{ Sys host.LegacySystem }
```

**Import rules** (depguard, `.golangci.yml`): `pkg/host/**` keeps to the standard library (rule `host-stdlib`), so nothing it links runs a command or touches a file behind the seam; `hostfake` is for tests and `internal/tools` only (rule `test-only`).

## 7. Not in this unit

- **The remaining direct call sites.** 53 lines calling `exec.Command`/`exec.LookPath` in 11 non-test files outside `pkg/interfaces` and `pkg/host` bypass the seam (pkg/security, pkg/system, pkg/updates, pkg/menu, pkg/config, pkg/utils, pkg/adapter/secondary's lastlog adapter), as do their direct `os` file calls. Remote PR #2's idea, injecting the Commander into pkg/system, pkg/security and pkg/updates, is the checklist for moving them (plan §6); it is Phase 3's, area by area.
- **Legacy `sh -c` calls** (`yes | ufw enable`, `chown -R`) are explicit shells in legacy code, not the seam's; they go with their slices.
- **Durable atomic writes and backups** (`p2-atomic`), detection (`p2-detect`), the run log's path (`p3-output`).
