# SSH keys: reading a user's keys and planning one in `authorized_keys` (`pkg/host/authkeys`)

> **Unit:** refactor plan slice 5, `next/s5-control`, unit (b) ([gallery-users.md](../engine/gallery-users.md) §5 Made here, §7 items 4 and 5).
> **Status:** built and tested on macOS against `hostfake` only. **Not run: no key has been written or removed on a VM.** It reads and plans; the write is the users control's.

## 1. What it reads
- **The files sshd reads for a user:** `sshd.Effective.AuthorizedKeysFile`, sshd -T's `authorizedkeysfile` fields as printed. On the VMs (read 2026-11-03): Debian 13 and Ubuntu 26.04 `.ssh/authorized_keys .ssh/authorized_keys2`, Alpine 3.23 `.ssh/authorized_keys`. `Path` expands `%h`, `%u` and `%%` (sshd_config(5) TOKENS) and puts a relative entry under the home, cleaned (`path.Clean`: a home `/home/x/` plans `/home/x/.ssh/authorized_keys` both ways). `none` is skipped. **Match blocks are not read**: a block that sets the files for one user is not seen.
- `Read` reads each file through a symlink, as sshd does, in order: not there is `Data` nil, any other failure `Err`. `Keys` is their keys, each once, so a key in both `authorized_keys` and `authorized_keys2` counts once.
- **A key** is parsed with no `x/crypto` (`ParseLine`, `ParsePublic`): a type OpenSSH 9 and 10 take (ed25519, RSA, the three ECDSA curves, both `sk-` types; no DSA), a base64 blob whose first string is that same type, then the comment. `ParseLine` reads past the options before the type, commas and spaces inside double quotes among them (sshd(8) AUTHORIZED_KEYS FILE FORMAT); blank and `#` lines are `ErrNotKey`. A pasted key (`ParsePublic`) is one line with no options, its surrounding space and a CR trimmed. The fingerprint is ssh-keygen -l's (`SHA256:` and unpadded base64 of the blob's SHA-256), tested against ssh-keygen's output for three synthetic keys made on macOS. The key material inside the blob is not checked.

## 2. What it plans
- **One file:** `Planned`, `~/.ssh/authorized_keys`. Where the files sshd reads leave it out (`Plannable`: `.ssh/authorized_keys` or `%h/.ssh/authorized_keys`), both plans are `ErrNotPlanned`; so are they where no file read is the planned one (`%h` with no home), never an empty plan.
- `PlanAdd`: the key's line appended, every other line kept byte for byte, a missing final newline added. A key already in any file sshd reads is an empty `Plan`. Where `~/.ssh` is absent, `MakeDir` is `install -d -m 700 -o <uid> -g <gid> <dir>` with numeric ids: GNU install (Debian 13), uutils (Ubuntu 26.04) and BusyBox (Alpine 3.23) all list `-d -m -o -g` (`01-inspect-b-*.log`).
- `PlanRemove`: every line of `~/.ssh/authorized_keys` holding the key (by fingerprint, with or without options) dropped, the rest byte for byte. A key also in another file sshd reads refuses, naming that file (`fs.ErrExist`): removing it here would leave it working. Not found: `fs.ErrNotExist`.
- A file that can't be read stops either plan. Plans make only reads (`TestPlansOnlyRead`).

## 3. StrictModes
All three VMs print `strictmodes yes`, read as `sshd.Effective.StrictModes` (Unknown, with no finding, where sshd -T prints no line). `CheckStrict` is the rule sshd's StrictModes applies (sshd_config(5)): the home is the user's or root's, `~/.ssh` the user's or absent, and neither writable by group or others. Both are read with `Lstat`, so a symlink at either refuses (a symlinked home refused too: what sshd does with one is **[I]**, not read). The error is `ErrStrictModes`, its message naming the path and why ("/home/alice is writable by its group (0775)").

## 4. API (only through `host.System`: depguard `authkeys-through-host`, which also denies `golang.org/x/crypto`)
```go
type Key struct { Type, Blob, Comment, Options, Line, Fingerprint string }
var ErrNotKey, ErrStrictModes, ErrNotPlanned error
func ParseLine(line string) (Key, error)
func ParsePublic(s string) (Key, error)
func (k Key) Same(o Key) bool
func Path(entry string, u accounts.User) string
type File struct { Path string; Data *string; Keys []Key; Err error }
func Read(ctx context.Context, sys host.System, entries []string, u accounts.User) []File
func Keys(files []File) []Key
const Planned = ".ssh/authorized_keys"
func Plannable(entries []string) bool
func CheckStrict(ctx context.Context, sys host.System, u accounts.User) error
type Plan struct { Path string; Before *string; After string; MakeDir []string }
func (p Plan) Empty() bool
func PlanAdd(ctx context.Context, sys host.System, u accounts.User, entries []string, k Key) (Plan, error)
func PlanRemove(ctx context.Context, sys host.System, u accounts.User, entries []string, fingerprint string) (Plan, error)
```
