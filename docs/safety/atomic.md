# Atomic writes and backups (`pkg/host`, `pkg/host/backup`)

> **Unit:** refactor plan Phase 2, `next/p2-atomic` (brief 2.1; decision §3.16, approved 2026-10-17). V-numbers: V18 (non-atomic writes, no backups, no rollback) and V21 (root writes through a user-controlled `~/.ssh`), both assigned to this unit ([side-effects.md §8](../refactor/phase0/side-effects.md)).
> **Status:** built and tested on macOS as an unprivileged user, on real files inside `t.TempDir()` and against the recording fake. Nothing has run on Linux; §4 lists what is "written, not run (gate 5)". Tests that set another user's ownership skip unless run as root.

This unit makes `host.Real.WriteFileAtomic` durable, closes V21 for the operations that change files, and adds the backup store a rollback restores from: `<backupPath>/<run-id>/` with a manifest. It supersedes [system.md](system.md) §2 items 7 and 8 and its V21 paragraph, which describe the naive write this unit replaces (that document is `next/p2-system`'s and was not edited here; `next/p2-vmtest` has since brought it up to date).

## 1. Why

On origin/main every write is `os.WriteFile`: in place, following symlinks, with no backup and no rollback; v0.3.2 kept backups by base name only and ignored their errors (V18; [phase0-findings.md §6](../refactor/phase0-findings.md#6-architecture-map-originmain-the-worktree-base), "Backups"). Root writes `authorized_keys` through a `~/.ssh` the user controls (V21). Brief 2.1 asks for a same-directory temporary file, fsync, rename and directory fsync, a backup before every replacement, and failing closed. Never locking the user out (brief §3 principle 1) needs both: a write that cannot leave a half-written `sshd_config`, and a copy of what was there so the change can be undone.

## 2. Guarantees

### Durable atomic write (`Real.WriteFileAtomic`)

1. **Steps, in order** (plan `p2-atomic`; brief 2.1). Everything after the walk works relative to the open directory of the target:
   1. the guarded walk to the target's directory (below);
   2. **check:** the target is absent or a regular file; a symlink is refused with `ErrSymlink` (Proxmox's `/root/.ssh/authorized_keys`, plan R15), a directory with `EISDIR`, anything else with `fs.ErrInvalid`;
   3. **create** `.<name>.hardn-<random>` beside it with `O_CREAT|O_EXCL|O_NOFOLLOW` and mode `0600`, so it never exists with looser permissions and never follows a planted link ("When these two flags are specified, symbolic links are not followed", [open(2)](https://man7.org/linux/man-pages/man2/open.2.html), `O_EXCL`);
   4. **write** the data; **chown** (skipped when uid and gid are both -1), **then chmod**, because `chown(2)` clears setuid and setgid ([chown(2)](https://man7.org/linux/man-pages/man2/chown.2.html), NOTES); **fsync**; **close**;
   5. **re-check** that the target is still not a symlink, then **rename** the temporary file over it: readers see the old file or the new, never a mix ([rename(2)](https://man7.org/linux/man-pages/man2/rename.2.html));
   6. **fsync the directory**, since fsync of a file "does not necessarily ensure that the entry in the directory containing the file has also reached disk" ([fsync(2)](https://man7.org/linux/man-pages/man2/fsync.2.html)).

   The brief and the plan put chmod/chown before fsync; this unit follows them (the owner and mode are then part of what fsync makes durable). `TestRealWriteFileAtomicStepOrder` pins the order.
2. **Fails closed.** An error in any step up to and including the rename leaves the target as it was (or absent) and removes the temporary file; `TestRealWriteFileAtomicFailsClosed` injects a failure at each of the eleven steps through a seam (`Real.fail`), for a replaced and a created file, on real files. The one step after the rename is the directory fsync: if it fails, the new file is in place and may not survive a crash, and the error wraps `host.ErrNotDurable`. The caller treats that as a failed step and rolls back from the backup. A crash (power loss) before the rename can leave a temporary file. Its name ends in `.hardn-<random>`, so sshd's `Include /etc/ssh/sshd_config.d/*.conf` ([phase0-findings.md §10](../refactor/phase0-findings.md#10-verify-list-brief-9), "sshd `Include` per distro") never reads it, and sudo and sudo-rs skip it because it contains a dot (brief §2.2; [phase0-findings.md §12](../refactor/phase0-findings.md#12-proposed-adjustments-to-the-brief), row 25).
3. **Never writes through or over a symlink at the target.** The check and the re-check refuse one. Between the re-check and `rename(2)` a link could still be planted; `rename(2)` would then replace the link itself, never its target, and only a user who can write that directory can plant one. **Writing through a link is explicit, and only from a trusted directory:** `host.WriteFileThroughSymlink(ctx, sys, name, data, perm, uid, gid)` resolves the link with `ResolveSymlink` (item 6) and calls `WriteFileAtomic` on the file it points to; the link stays as it is. A link in a directory another user can change is refused with `ErrSymlinkParent`, so alice's `~alice/.ssh/authorized_keys → /etc/shadow` is never written through. This is the helper for p2-pve's cluster-wide keys file (`/root/.ssh` is root's, `0700`, so trusted); `backup` resolves symlink entries the same way. Resolving a link with the unguarded `Readlink` and writing to its target is the V21 hole again: do not. `ErrSymlinkParent` also matches `ErrSymlink`, so a caller that picks the write-through path on `ErrSymlink` must test `ErrSymlinkParent` first (the helper refuses it anyway).
4. **Keeping mode and owner.** `host.WriteFileAtomicKeep(ctx, sys, name, data, perm, uid, gid)` uses an existing regular file's permission bits and owner, and `perm`, `uid`, `gid` only for a new file. It never carries over setuid, setgid or sticky bits. It reads them with `Lstat` just before the write, so it is for files in root-controlled directories (`hardn.yml` at its loaded path, §3.19; the main `sshd_config`); a file in a user's home gets explicit values. `WriteFileAtomic` itself always sets exactly the mode and owner it is given; -1 for uid or gid leaves the process's.

### Guarded operations (V21)

5. `WriteFileAtomic`, `Chown`, `Chmod`, `Remove` and the new `ReadFileNoFollow` and `ResolveSymlink` reach the target's directory by a **guarded walk**: from `/`, one component at a time, with `openat(O_NOFOLLOW|O_DIRECTORY)`, so a directory the walk has opened cannot be swapped for a link under it. **A symlink on the way is followed only when the directory holding it is owned by root (or by the user hardn runs as) and is writable by neither its group nor others.** Any other link is refused with `host.ErrSymlinkParent` (which also matches `ErrSymlink`). So when alice replaces `~alice/.ssh` with a link to `/root/.ssh`, writing, chowning, chmodding, removing or reading `~alice/.ssh/authorized_keys` is refused, and root's keys stay as they were; a root-owned link such as `/var/run → /run` is followed. The kernel resolves `..` and relative link targets against the directory reached so far; at most 40 links are followed ([path_resolution(7)](https://man7.org/linux/man-pages/man7/path_resolution.7.html)), then `ELOOP`. The final component is acted on with no-follow calls: `unlinkat` with and without `AT_REMOVEDIR` in `os.Remove`'s order ([unlink(2)](https://man7.org/linux/man-pages/man2/unlink.2.html)), `readlinkat` for `ResolveSymlink`, and an `O_NOFOLLOW|O_NONBLOCK` open for `Chmod`, `Chown` and `ReadFileNoFollow`, after an `lstat` that refuses FIFOs and devices without opening them; `Chmod` and `Chown` then change the file through that descriptor (`fchmod`, `fchown`), so they change regular files and directories only (`fs.ErrInvalid` otherwise) and, unprivileged, only files the process can read. Guarded operations take absolute paths only (`fs.ErrInvalid` otherwise), as the fake always did. Tested on real files (`TestRealGuardedWalk`: a world-writable directory, and through a seam, a directory owned by another user) and as a contract case against both `host.Real` and `hostfake`.
6. **`ResolveSymlink(ctx, name)`** returns a link's target as stored and `resolved`, the absolute path it points to. The directory holding the link must itself be trusted (the same rule), or `ErrSymlinkParent`. The walk knows the path of the directory it reached with no symlink in it, and a relative target is joined to that, so `../target` means what it means to the kernel even when the link's path goes through `alias → real/sub`: ".." in a directory is its parent ([path_resolution(7)](https://man7.org/linux/man-pages/man7/path_resolution.7.html)), and in a path with no symlink the parent is the previous component. `host.JoinLinkTarget` does the join, and refuses a target with `..` after another component (`x/../y`) with `fs.ErrInvalid`, because `x` may be a link, which only a lookup could tell. Not a symlink: `EINVAL`, as `readlink(2)`. Tested on real files and against the fake (contract case "resolve symlink").
7. **`ReadFileNoFollow`** returns a regular file's content and its `fs.FileInfo` from one open descriptor, so content, mode and owner describe the same file. Backups read through it: with `ReadFile`, a user swapping their file for a link to `/etc/shadow` between the check and the read would get the shadow file's content written back into their own file at rollback.
8. **Hard links.** A name another user hard-linked to a root file is that file: `chmod` or `chown` of it changes the root file. `Chmod` and `Chown` therefore refuse a regular file with more than one link (`st_nlink > 1`, from the open descriptor) in a directory that is not trusted, with `host.ErrHardLink`; in a trusted directory, or with one link, the file is changed (`TestRealChangeRefusesHardLinks`). Brief 2.3 step 1 (`authorized_keys` `0600`, owned by the user) is exactly a root chmod and chown in a user's directory. Linux's `fs.protected_hardlinks` stops unprivileged users from making such links; whether every supported release enables it by default is **[unverified]**, and nothing here relies on it. `WriteFileAtomic` renames a new file over the name and `Remove` unlinks the name, so neither changes a linked file.
9. **Not guarded:** `MkdirAll`, `RemoveAll`, `Rename`, `WriteFileInPlace` and the reads (`ReadFile`, `Stat`, `Lstat`, `ReadDir`, `Readlink`) still follow every link in the parent directories (`ReadFile` must: `/etc/os-release` is a link). None of them may be used on a path under a directory another user controls, and `Readlink` must not be used to find a file to write (item 3).

### Backups (`pkg/host/backup`, §3.16)

10. **Layout.** `backup.Open(sys, settings, runID)` starts a run; nothing is created until the first backup is taken. The run directory is `<backupPath>/<run-id>/`, mode `0700`, and must not exist yet (`ErrRunExists`). **Nothing is created before the check of item 14 passes** on the deepest directory of its path that exists; the missing directories are then created one at a time, `0700`, each checked in turn, and if a check or a creation fails, the ones this call created are removed, so a later `Backup` of the run tries again rather than finding `ErrRunExists`. The existence check before the creation is not an exclusive create, and need not be: once that directory has passed the check, only root or hardn's user can create a name in it. It holds one copy per backup, `NNNN-<base name>` with mode `0600` (numbered, so two files with one base name never collide, unlike v0.3.2's base-name-only copies), and `manifest.json` (`0600`). `NewRunID(t)` gives `20261017T143005Z-1a2b3c4d`: UTC, sortable, with a random suffix. An id is 1 to 64 letters, digits, `-` or `_`, starting with a letter or digit, so it is one path component.
11. **The configuration keys are honoured.** `config.(*Config).BackupSettings()` maps `backupPath` and `enableBackups` (defaults `/var/backups/hardn`, `true`). `backupPath` must be absolute: an empty or relative one is refused with `ErrSettings` at `Open` rather than guessed (a decision of this unit: a relative path would land wherever hardn was started). Each `Backup(ctx, path, need)` says whether the change's rollback reads it:
   - `RollbackRequired`: always taken. With `enableBackups: false` the outcome is `TakenAnyway`, and the entry records `"takenAnyway": true` with `"backupsEnabled": false` at the top, so both the result and the manifest say so (§3.16);
   - `Optional`: `Taken` when backups are enabled, `Skipped` (nothing recorded, nothing created) when not.
12. **What is recorded** (format: [`pkg/host/backup/testdata/manifest.json`](../../pkg/host/backup/testdata/manifest.json), compared byte for byte by `TestBackupRestoreRoundTrip`): `schemaVersion` (1), `runId`, `created`, `backupsEnabled`, and per entry its `seq` (1, 2, … in the order taken: the entry's name for a rollback journal, and the `NNNN` of its copy), the original `path`, its `kind` and, when there was content, a `file` object with the copy's name (`backup`), `mode` (octal string, with setuid/setgid/sticky), `uid`, `gid`, `size` and `sha256`:
    - `file`: a regular file, read with `ReadFileNoFollow`;
    - `absent`: nothing was there; a rollback removes the file the change created;
    - `symlink`: the link's `symlinkTarget` as stored, `symlinkResolved`, the absolute path it pointed to (`ResolveSymlink`, item 6), and the content of the regular file there (none when it points to nothing). This is Proxmox's `/root/.ssh/authorized_keys` → `/etc/pve/priv/authorized_keys` ([phase0-findings.md §10](../refactor/phase0-findings.md#10-verify-list-brief-9), "Proxmox join / detection" **[V]**).

    `Backup` sees the path only through guarded operations, as the change and the restore do: a symlink in a directory another user can change, at the path or on the way to it, is refused with `host.ErrSymlinkParent`, and nothing is recorded (`TestBackupRefusesUntrustedSymlinks`: alice's `~/.ssh/authorized_keys → etc/shadow`, a link to nothing, and `~bob/.ssh` a link to a directory holding such a link). A directory, any other file type, or a link to one is refused with `ErrNotRegular`; a link whose target has `..` after another component with `fs.ErrInvalid`; a link to a link with `ErrSymlink`.

    The golden file is not rewritten by `make golden-update` (that target is pkg/ui's): after an intended format change, run `go test ./pkg/host/backup -run TestBackupRestoreRoundTrip -update` and review the diff.
13. **Order and failure.** The copy is written (atomically, fsynced) before the manifest, and the manifest is rewritten atomically after every backup, so the manifest on disk always lists every copy a crash left. **Any error from `Backup` means no backup was recorded, and the change must not go ahead** (`TestBackupWriteFailures` fails the copy and then the manifest).
14. **The backup directory must be root-controlled.** The run directory and every directory above it, after links, must be owned by root (or hardn's user) and writable by neither group nor others, or the backup is refused with `ErrUnsafeDir`, which names the directory. A rollback writes the copies back as root, into `/etc/sudoers.d` among others, so a copy another user could edit would be a privilege escalation. `Load` and `Restore` check the same before reading a manifest. **A directory with the sticky bit is no exception** (a decision of this unit): a `backupPath` under `/tmp` is refused, as plan §3 row 3.12 takes root's writes out of `/tmp` for the update checker. The real-file tests run in `t.TempDir()`, which is under `/tmp` on Linux, so they set a seam (`trustedTop`) that makes their temporary directory the top of the check; `TestBackupRefuses` pins the sticky refusal.

### Restore (`backup.Load`, `backup.Restore`)

15. `Load(ctx, sys, dir)` reads `dir/manifest.json` and refuses (`ErrManifest`) another `schemaVersion`, a `seq` that is missing or not increasing, a relative or unclean path or `symlinkResolved`, an unknown kind, inconsistent fields, a copy name that is not a plain file name in the run directory, and a malformed mode, size or sha256. Unknown JSON fields are ignored, so fields can be added without a new schema version.
16. `Restore(ctx, sys, m)` puts the entries back **last taken first**, so a path backed up twice in one run ends as it was before its first change. Per entry:
    - `file`: the copy's size and SHA-256 are checked first (`ErrCorrupt`, and the path is left alone); then it is written back with `WriteFileAtomic` with the recorded mode and owner;
    - `absent`: a regular file at the path is removed; anything else there is `ErrChanged`;
    - `symlink`: the link is resolved again with `ResolveSymlink`. It must be in a directory that is still trusted (`host.ErrSymlinkParent` otherwise, and nothing is written), still be a link to the same target, pointing to the same `symlinkResolved` (`ErrChanged` otherwise; restore never recreates or replaces a link); then the file it points to is restored as a `file` or `absent` entry (`TestBackupRefusesUntrustedSymlinks`, `TestBackupSymlinkResolution`, `TestBackupDanglingSymlink`).

    It is **idempotent**: an entry already as backed up (same content, mode and owner) is `Unchanged`, and a second restore changes nothing. It goes on past a failed entry and returns every failure joined, with a per-entry `Restored{Seq, Path, Action, Err}`; `Action` is zero for a failed entry, except `Rewritten` with an `Err` wrapping `host.ErrNotDurable` when the copy is back but its directory fsync failed (`TestRestoreFailures`). A manifest built by hand rather than loaded gets the same checks: the run directory's (`ErrUnsafeDir`) and each entry's (`ErrManifest`). It reads, writes and removes only through guarded operations: `ReadFileNoFollow`, `WriteFileAtomic` and `Remove`, which removes the name and never what a link there points to. A restore after Ctrl-C must run under `context.WithoutCancel(ctx)` with its own timeout: every `host.System` method refuses a done context ([system.md](system.md) §2.5).

## 3. Platform and library facts relied on

| Fact | Source | Used for |
|---|---|---|
| `rename(2)` replaces the new path atomically, and replaces a symlink there rather than following it | [rename(2)](https://man7.org/linux/man-pages/man2/rename.2.html) | §2.1, §2.3 |
| fsync of a file does not make its directory entry durable; the directory needs its own fsync | [fsync(2)](https://man7.org/linux/man-pages/man2/fsync.2.html) | §2.1 step 6 |
| `O_CREAT\|O_EXCL` fails on an existing path and does not follow a symlink there; `O_NOFOLLOW` covers the last component only | [open(2)](https://man7.org/linux/man-pages/man2/open.2.html) | §2.1 step 3, §2.5 |
| `chown(2)` clears setuid and setgid; `fchown` changes the file an open descriptor refers to | [chown(2)](https://man7.org/linux/man-pages/man2/chown.2.html) | §2.1 step 4, §2.5, §2.8 |
| `unlinkat(AT_REMOVEDIR)` is `rmdir(2)` | [unlink(2)](https://man7.org/linux/man-pages/man2/unlink.2.html) | §2.5 `Remove` |
| At most 40 symlinks per lookup; ".." in a directory is its parent, resolved during the walk | [path_resolution(7)](https://man7.org/linux/man-pages/man7/path_resolution.7.html) | §2.5, §2.6 |
| A symlink opened with `O_DIRECTORY\|O_NOFOLLOW` fails with `ENOTDIR` on Linux and `ELOOP` elsewhere, so the walk asks `lstat` what it met | Go 1.27.2 `src/os/root_unix.go` (`rootOpenDir` treats both); the Linux-specific errno is **[unverified]** here | §2.5 |
| The sshd drop-in Include is `sshd_config.d/*.conf`; sudo and sudo-rs skip `sudoers.d` names with a `.` | [phase0-findings.md §10](../refactor/phase0-findings.md#10-verify-list-brief-9), §12 row 25; brief §2.2 | §2.2 leftover temporary files |
| Whether `fs.protected_hardlinks` is on by default on each supported release | **[unverified]**; not relied on | §2.8 |
| Whether a leftover `.name.hardn-<random>` is read in `resolved.conf.d`, `doas.d` or by Alpine's `run-parts` for `/etc/periodic/daily` | **[unverified]** | §2.2 |
| Proxmox's root `authorized_keys` is a symlink into `/etc/pve/priv`; never rename over it | [phase0-findings.md §10](../refactor/phase0-findings.md#10-verify-list-brief-9) **[V]**; plan R15, `p2-pve` | §2.1 step 2, §2.12 |
| `crypto/rand.Read` "never returns an error" | Go 1.27.2 `src/crypto/rand/rand.go` | `NewRunID` |
| The standard library's `syscall` has no `Openat`, `Fstatat`, `Renameat` or `Readlinkat` on macOS; `golang.org/x/sys/unix` v0.49.0 has them on Linux and macOS | Go 1.27.2 `src/syscall`; x/sys v0.49.0 `unix/zsyscall_{linux,darwin}_*.go` | §5 dependency note |

## 4. Written, not run (gate 5)

Nothing here has run on Linux or as root. On the Debian 13, Ubuntu 26.04 and Alpine 3.24 VMs (`next/p2-vmtest`) and by hand on Proxmox VE 9:

- every `WriteFileAtomic` step on ext4 as root, with real owners (`authorized_keys` as alice), a setuid mode kept through chown-then-chmod, and the directory fsync succeeding (no `EINVAL`);
- the guarded walk on Linux: `ENOTDIR` for a link met with `O_DIRECTORY|O_NOFOLLOW` (§3, unverified row), `/var/run → /run` followed, `~alice/.ssh → /root/.ssh` refused as root; as root, `Backup` refusing alice's planted `~/.ssh/authorized_keys` link, and `Chmod`/`Chown` refusing her hard link to a root file, whatever `fs.protected_hardlinks` is;
- `TestRealGuardedWalk`, `TestBackupRefuses` and the other real-file tests as root, with `t.TempDir()` under Linux's `/tmp` (here, `pkg/host/backup` and `hostfake` pass with `TMPDIR=/tmp`, macOS's root-owned sticky `/private/tmp`);
- `TestRealWriteFileAtomicOwner` and `TestRestoreOwner`, which skip unless root, and `Chown` as root through its descriptor, on a file and on a directory;
- Proxmox: `WriteFileAtomic` refusing `/root/.ssh/authorized_keys`; `ResolveSymlink` accepting it (`/root/.ssh` root, `0700`); a backup of it recording the link and the `/etc/pve/priv` file; a restore and `WriteFileThroughSymlink` writing back through the link. **Whether pmxcfs supports `O_CREAT|O_EXCL`, `fchown`, `fchmod`, `renameat` within `/etc/pve/priv` and an fsync of that directory is [unverified]**; if not, `p2-pve` needs another write path for that file;
- backups as root under `/var/backups/hardn` on each distribution (missing directories are created `0700`, which would include `/var/backups` itself where it is absent) and a restore from a `systemd-run` transient unit and an OpenRC detached process (`p2-revert`);
- that sshd and sudo ignore a leftover temporary file (§2.2), and the unverified directories in §3;
- crash safety under real power loss: here it is simulated by injected failures only. The run directory's own entry in `backupPath` is not fsynced (`System` has no directory sync); on ext4 the fsync of the first copy is expected to commit it too **[unverified]**.

## 5. Changes to the seam (`pkg/host`, `hostfake`)

- `System` gains `ReadFileNoFollow` and `ResolveSymlink`. `Real` and the fake implement them; the fake records them as `read-nofollow` and `resolve-symlink`.
- `WriteFileAtomic`, `Chown`, `Chmod`, `Remove` are guarded in `Real` and in the fake (the fake acts as root, so only root-owned directories are trusted there). They now refuse relative paths in `Real` too, as the fake already did. `Chown` now changes regular files and directories only, through a descriptor, as `Chmod` does (`fs.ErrInvalid` for anything else, in both), and both refuse a hard-linked file in a directory that is not trusted (`Real` only: the fake has no hard links).
- New errors: `ErrSymlinkParent` (wraps `ErrSymlink`), `ErrHardLink`, `ErrNotDurable`. New helpers: `WriteFileAtomicKeep`, `WriteFileThroughSymlink`, `JoinLinkTarget`.
- **Dependency:** `golang.org/x/sys` was already a direct requirement (v0.49.0; `go.mod` and `go.sum` unchanged). The depguard rule `host-stdlib` now also allows `golang.org/x/sys/unix` under `pkg/host`, and `detect-through-host` denies `golang.org/x/sys` to `pkg/host/detect`, which still reaches the host only through `host.System`.
- **Legacy gate (P-32):** `legacyThroughHost` stays `false`. If it is flipped, `SystemFileSystem.Remove` takes the guarded `Real.Remove`: a relative path, or one through a link in a directory another user can change, is then refused where `os.Remove` accepted it. This adds to the delta list in [system.md](system.md) §5.

## 6. API

```go
package host

var ErrSymlinkParent error // a link on the way (or at the path, ResolveSymlink) in an untrusted directory; also matches ErrSymlink
var ErrHardLink error      // Chmod, Chown: a file with other hard links in an untrusted directory
var ErrNotDurable error    // replaced, but the directory fsync failed

// in System:
ReadFileNoFollow(ctx context.Context, name string) ([]byte, fs.FileInfo, error)
ResolveSymlink(ctx context.Context, name string) (target, resolved string, err error)

func WriteFileAtomicKeep(ctx context.Context, sys System, name string, data []byte, perm fs.FileMode, uid, gid int) error
func WriteFileThroughSymlink(ctx context.Context, sys System, name string, data []byte, perm fs.FileMode, uid, gid int) error
func JoinLinkTarget(dir, target string) (string, error)

package backup

const DefaultPath = "/var/backups/hardn"
const ManifestName = "manifest.json"
const SchemaVersion = 1
var ErrSettings, ErrRunExists, ErrUnsafeDir, ErrNotRegular, ErrManifest, ErrCorrupt, ErrChanged error

type Settings struct { Path string; Enabled bool } // backupPath, enableBackups
type Need uint8    // Optional, RollbackRequired
type Outcome uint8 // Skipped, Taken, TakenAnyway; String()
type Kind string   // KindFile "file", KindAbsent "absent", KindSymlink "symlink"
type Perm uint32   // octal mode with setuid/setgid/sticky; PermOf(fs.FileMode), FileMode(), text "0644"
type Content struct { Backup string; Mode Perm; UID, GID int; Size int64; SHA256 string }
type Entry struct { Seq int; Path string; Kind Kind; SymlinkTarget, SymlinkResolved string; File *Content; RollbackRequired, TakenAnyway bool }
type Manifest struct { SchemaVersion int; RunID string; Created time.Time; BackupsEnabled bool; Entries []Entry; Dir string /* not stored */ }
type Result struct { Outcome Outcome; Entry Entry }
type Action uint8 // Unchanged, Rewritten, Removed; String()
type Restored struct { Seq int; Path string; Action Action; Err error }

func NewRunID(t time.Time) string
func Open(sys host.System, s Settings, runID string) (*Run, error) // no I/O
func (r *Run) Backup(ctx context.Context, path string, need Need) (Result, error)
func (r *Run) Dir() string
func (r *Run) Manifest() Manifest
func Load(ctx context.Context, sys host.System, dir string) (Manifest, error)
func Restore(ctx context.Context, sys host.System, m Manifest) ([]Restored, error)
// next/s3-control (control-backup.md §2.12 to §2.14): ErrWrite; type DirError struct{ Dir string; Err error }
//   (ErrUnsafeDir or ENOTDIR, the directory named); type WriteError struct{ Path, BackupPath string; Err error };
//   func Runs(ctx context.Context, sys host.System, dir string) ([]Manifest, error)

package config

func (c *Config) BackupSettings() backup.Settings
```

## 7. Not in this unit; inputs to later units

- **`p3-engine`:** call `Backup` before each file change, with `RollbackRequired` for every change whose `Rollback` restores a file; stop on any `Backup` error; treat `ErrNotDurable` as a failed step.
- **`p2-revert`:** the journal records the run directory (`Manifest.Dir`) and the `Seq` of the entries that belong to the pending change (filter `Entries` by it before `Restore`; `Restored.Seq` names each result); `Load` it in the rollback process, run `Restore` under `context.WithoutCancel`. A run directory without a manifest means no backup was recorded. A `Rewritten` result with an `ErrNotDurable` error is restored but not synced.
- **`p2-pve`:** edit the cluster-wide keys file with `host.WriteFileThroughSymlink`, never `WriteFileAtomic` on the link nor `Readlink` then a write; the pmxcfs items in §4 decide whether that works.
- **§3.19 config save:** the one save function writes the loaded path with `WriteFileAtomicKeep`; the function itself, and replacing the 17 legacy `SaveConfig` call sites, are the slices'.
- **Not here:** guarding `MkdirAll`, `RemoveAll` and `Rename`; removing leftover temporary files after a crash; retention or pruning of old run directories (a policy choice for the maintainer); directories a change creates, and the mode and owner of existing directories a change sets (brief 2.3 step 1: `~/.ssh` `0700`, home directories that are group- or world-writable): `Backup` refuses directories (`ErrNotRegular`), so these are the engine's inverse operations, recorded in p2-revert's journal, not file backups; the legacy `utils.BackupFile` and `BackupManager` (untouched here; deleted in `next/s3-replace`, slice 3 unit (d)); the other half of V21, legacy `sh -c "chown -R …"` with an interpolated user name (the user-management slice).
