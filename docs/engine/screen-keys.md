# The SSH Keys screen (`pkg/screens/sshkeys`, `screen.SSHKeys`)

The screen unit of the roadmap's "SSH key review", `next/key-review`: the screen [gallery-keys.md](gallery-keys.md) mocked, as adopted there (2026-11-13, "Adopt all"), now SSH Login's item 6. It only reads.
Status: built and tested in process (`make check`). No hardn binary ran. **Nothing ran on Linux** (§5).

## 1. What it does

SSH Login `6) SSH keys`, its description the count ("6 keys, 4 to review", "3 keys", "2 keys, 1 file not read", or "Can't read"; only a key is counted as one to review), opens "SSH Keys": a Summary, then a section per account with a key, each row a key's type and size, its comment and what marks it. `0` returns to SSH Login, `q` quits. It plans nothing, writes nothing and opens under plan only as live.

- **Files:** sshd -T's `AuthorizedKeysFile`, each entry expanded for each account of `/etc/passwd` (`%h`, `%u`, `%%`, a relative path under the home), `none` skipped.
- **Weak** (a warning): DSA, or RSA under 2048 bits. No other type.
- **Shared** (a warning): the same type and blob in another account. "Shared with root", or "Shared with 3 accounts". The summary counts the key once.
- **Refused by sshd** (no warning): a type `PubkeyAcceptedAlgorithms` leaves out, or an RSA key under `RequiredRSASize`.
- **Can't read** (bad): a file there that could not be read, with its path. A file that is not a regular one (a FIFO, a device, a directory), is over 1 MiB or has a line over 64 KiB, or is behind a symlink in a directory someone other than root can write, is not read (§3).
- **Not a key** (a warning): a file's lines that are neither a key, blank nor a `#` comment, counted, with its path. The lines are not shown.
- **Folded:** over 10 keys, each account shows its keys to review and "N more keys", the accounts with none one row each under "Nothing to review"; item 1, "All keys", lists every key, then "Keys to review" folds them.
- **Not read:** with sshd -T or `/etc/passwd` not read, the summary's "Keys" says "Can't read" and nothing is listed. No file is assumed.
- Accounts: every passwd account with a key, an unread file or lines that are no key; system and locked accounts too. No fingerprint; a key with no comment says "No comment".

## 2. The API

- `sshkeys.Run(ctx, Options{Prompter, System, Settings})`: draws until `0` (nil) or `q` (`nav.ErrQuit`), reading the host again on each draw.
- `sshkeys.Read(ctx, sys, settings) Reading` and `sshkeys.View(Reading) screen.SSHKeys`: the one reading and the one mapping. SSH Login's item and the gallery's mocks go through both.
- `ssh.ReadKeySettings(ctx, sys, facts)` and `ssh.KeySettingsOf(sshd.Effective)` (`pkg/engine/controls/ssh`): `AuthorizedKeysFile`, `PubkeyAcceptedAlgorithms` and `RequiredRSASize` from sshd -T, read as `ReadKeyFiles` reads it.
- `authkeys.ReadAccounts(ctx, sys, entries)`: every passwd account and its files, each read guarded (§3), not through `authkeys.Read`. `authkeys.Review(File)`: the file's keys, DSA (`authkeys.DSA`) and RSA under 1024 bits among them, and how many lines are no key. `ParseLine` and `Read` are unchanged: they still leave both out, as no way to log in.
- `screen.SSH.Keys` and `screen.SSHKeysEntry` ("6"): SSH Login's item, absent where `Keys` is "".
- No engine control, step or journal entry, and no new output code: the screen makes no change.

## 3. Engineering calls

- **Match blocks are not read**, as `ReadKeyFiles` (User Management) does not: an `AuthorizedKeysFile`, `RequiredRSASize` or `PubkeyAcceptedAlgorithms` a `Match` block sets for a user is not seen, and the screen does not say so. Open (the maintainer's call, [gallery-keys.md](gallery-keys.md) did not settle it).
- **Refused is said only from what sshd -T printed.** No `pubkeyacceptedalgorithms` line: no type is refused. No `requiredrsasize` line (before OpenSSH 9.1): the size is 1024, OpenSSH's own floor (`authkeys.MinRSABits`). A `requiredrsasize` that is no number is settings not read.
- **"Not a key" is new here**: the task asked that a file that cannot be parsed be shown as such; the words are proposed.
- **An account whose home in passwd is not an absolute path** has its relative entries shown as "Can't read", the path as expanded; nothing is read for them.
- **Untrusted text:** every account name, comment and path goes through `logview.Safe` in `View`. A key's options are read past and never shown; a line that is no key is counted, never shown.
- **The key files are read guarded**, as any account can put anything where its file is and SSH Login reads them on each draw: `host.System`'s `ScanLines` (a regular file only, opened without blocking, no further than its size), at most `authkeys.MaxFile` (1 MiB), and a symlink only through `ResolveSymlink`, from a directory that is root's and no one else writes (Proxmox VE's `/root/.ssh/authorized_keys`). So the screen reads less than sshd: **a key file that is a symlink inside the account's own `~/.ssh` is "Can't read"**, though sshd takes it. User Management and the lockout checks still read through `authkeys.Read` (plain, through any symlink): not changed here.
- **SSH Login reads the keys on each draw**, from the sshd -T it just read, for the item's count. The SSH Keys screen reads sshd -T again itself.
- Accounts only NSS knows (LDAP, sssd) are not listed: passwd is the file. `AuthorizedKeysCommand`, `TrustedUserCAKeys` and StrictModes are not read. A `cert-authority` line is listed as a key of its type: that it is a CA's, like every option, is not shown.
- An RSA key over 16384 bits, or a key whose blob is not in its type's form, is "Not a key".

## 4. Tested

- `pkg/host/authkeys`: `Review` (order, each key once, DSA, RSA 768, options before the type, lines that are no key, bytes that are no text, a file not there, a file not read); `ReadAccounts` (tokens, `none`, a system account, an unreadable file, a home that is no path, a name twice, no passwd, a malformed passwd, a context that ended; a symlink in root's directory followed and one in the account's refused, a file over 1 MiB, a line over 64 KiB, a link to itself; a FIFO on the test machine's file system, not opened); `ParseLine` and `ParsePublic` still refuse DSA and small RSA.
- `pkg/engine/controls/ssh`: `ReadKeySettings` on Debian and Alpine, no algorithms line, no `authorizedkeysfile` line, a size that is no number or negative, no sshd, a configuration sshd refuses, no sshd_config.
- `pkg/screens/sshkeys`: clean, the weak rule at its edges, shared, refused, several files, unreadable, not keys, not read, untrusted text, folded and unfolded, a fresh reading on each draw, invalid options; nothing written in any.
- `pkg/screens/ssh`: item 6 on three hosts, live and plan only, `0` and `q`. `pkg/ui/screen`: the view.
- The gallery: the `keys` listing and `play keys`, `play keys/<mock>`, and `play ssh/<host>` item 6, all through `sshkeys.Read` and `sshkeys.View`.

## 5. Not run

Nothing ran on Linux. To prove there, as root on a throwaway host of each distribution:

- `go test ./...`.
- `sshd -T | grep -E '^(authorizedkeysfile|pubkeyacceptedalgorithms|requiredrsasize) '`: the three lines are printed in the form read here (*unverified on a host*: the keywords' lower-case form and the comma list are as hardn's other sshd readers take them).
- `sudo hardn`, 2, 6 on a host with: a key in root's and a user's file; an RSA 1024 key and an `ssh-dss` line; a key file `chmod 000` on a root-squashed mount or replaced by a directory (root reads a 000 file); a file with a junk line; a key file replaced by a FIFO (`mkfifo`), and by a symlink to `/dev/zero`: item 2 still draws at once, each "Can't read". Then `0`, and `q`.
- The same under `sudo hardn -n`.
- On Alpine with doas, and on a host with `AuthorizedKeysFile /etc/ssh/keys/%u`.
