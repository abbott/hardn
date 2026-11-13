# SSH Keys mock

*Since the screen unit (2026-11-13): the screen is built and is SSH Login's item 6 ([screen-keys.md](screen-keys.md)). The mocks draw through its mapping (`sshkeys.View`), `play keys` runs it, the reader reports DSA and small RSA keys, the files are sshd's `AuthorizedKeysFile`, and two mocks were added, `not-keys` and `not-read`. Where this page says nothing is wired, or names `keysView`, this note is current.*

The mock unit of the roadmap's "SSH key review", `next/key-review`: a read-only list of every SSH key sshd reads, by account, under SSH Login. **Adopted (the maintainer, 2026-11-13): "Adopt all".** Every recommended answer of §3 is adopted, with the Made here calls as stated and §4's words.
Status: built and tested in process (`go test ./pkg/ui/screen/ ./internal/tools/uigallery/`). No hardn binary ran. Nothing ran on Linux. Nothing is wired into hardn: `cmd/hardn`, `pkg/screens`, the engine and hardn.yml are untouched, and play has no keys screen.

## 1. What to run

```
go run ./internal/tools/uigallery keys [-ascii] [mock | all]   # the listing, no input
go run ./internal/tools/uigallery keys -list                    # the mocks' names
```

The view is `pkg/ui/screen/keys.go` (`screen.SSHKeys`), the mocks `internal/tools/uigallery/keys.go`, the goldens `internal/tools/uigallery/testdata/golden/keys/`. SSH Login's renders are unchanged: `screen.SSH` gained `Keys`, which nothing but the `login` mock sets.

## 2. The mocks

Each mock is a host's accounts and their `~/.ssh/authorized_keys`, read by the real reader (`authkeys.Read`) from a fake host. What sshd asks of a key (`RequiredRSASize`, `PubkeyAcceptedAlgorithms`) is the mock's own word: no `sshd -T` is modelled.

| Mock | What it shows |
| --- | --- |
| `clean` | Three keys in two accounts: "To review: None" |
| `weak` | A key root and george both hold; an RSA 1024 key and a DSA key, weak; an RSA 2048 key, not marked |
| `refused` | sshd asks `RequiredRSASize 3072` and takes no ECDSA: two keys that can't log in, neither weak |
| `many` | 23 keys in six accounts, folded: the keys to review, and a count for the rest |
| `many-all` | The same host after 1, All keys |
| `unreadable` | alice's file can't be read: a "Can't read" row with its path, and "Not read" in the summary |
| `comments` | Untrusted comments: an escape sequence with a right-to-left override, 120 letters, none |
| `login` | SSH Login on the `weak` host with the item that opens the screen |

The screen: the title bar; "Summary" (Keys, then Weak, Shared, Refused by sshd and Not read where there are any, or "To review: None"); one section per account, each row a key's type and size, its comment and what marks it, the keys to review first; `0) Return`.

## 3. Decisions (adopted 2026-11-13: each (a))

Drawn: the recommended answer of each.

1. **Where the list lives.** *Gates the screen unit.* (a) *Recommended, drawn (`login`):* a screen of its own, "SSH Keys", opened from SSH Login's new item 6, "SSH keys", whose description counts them ("6 keys, 4 to review"). Items 4 and 5 keep their keys. (b) Under User Management, beside each user's keys: the keys are there already, but a key shared between accounts is only seen across them. (c) A main-menu item: the menu grows to eleven.
2. **What "weak" means.** *Gates what users are told to replace.* (a) *Recommended, drawn:* a DSA key, or an RSA key under 2048 bits. ED25519, ECDSA and the FIDO types are never weak. (b) RSA under 3072: also marks every RSA 2048 key, which many hosts still hold; `weak`'s `george@desk-1` would be marked. (c) RSA under this host's `RequiredRSASize` alone: then "weak" only says what sshd refuses already (3).
3. **Keys sshd refuses.** A key under the host's `RequiredRSASize`, or of a type its `PubkeyAcceptedAlgorithms` leave out, can't log in. (a) *Recommended, drawn (`refused`):* listed and marked "Refused by sshd", not a warning: it is dead, not a risk. (b) Left out: the list is then what can log in, and a dead key is never found.
4. **A key several accounts hold.** (a) *Recommended, drawn:* marked on each copy, a warning: "Shared with root", or "Shared with 3 accounts" for more; the summary counts the key once. Same key is same type and blob; the comment is not compared. (b) Shown, no warning: one admin's key on root and their own account is common.
5. **Many keys.** (a) *Recommended, drawn (`many`, `many-all`):* over 10 keys the screen opens folded: each account's keys to review, "N more keys", and the accounts with none as one row each under "Nothing to review"; item 1, "All keys", lists every key and "Keys to review" folds them again. (b) Always every key: `many-all` is over 50 lines, and the summary scrolls off a 24-row terminal.
6. **Which accounts.** (a) *Recommended, drawn:* every account in passwd with a key or a file that can't be read, system and locked accounts too: a key on one is what a review is for. Accounts with no key are not listed. (b) Login accounts only.
7. **The fingerprint.** (a) *Recommended, drawn:* not shown: 50 columns, and the comment names the key. A key with none says "No comment". (b) A second line per key: twice the height, and the only way to tell two "No comment" keys apart.

## 4. Words

Proposed: "SSH Keys"; "SSH keys" (the item), "6 keys, 4 to review"; "Summary"; "Keys", "3 in 2 accounts"; "To review", "None"; "Weak"; "Shared", "Shared with george", "Shared with 3 accounts"; "Refused by sshd"; "Not read", "1 file"; "Can't read"; "No comment"; "1 more key"; "Nothing to review"; "All keys"; "Keys to review", "Fold the rest". Types as `ssh-keygen -l` words them: "RSA 4096", "ED25519", "ECDSA 384", "ED25519-SK", "ECDSA-SK", "DSA".

## 5. Made here

- **Comments are untrusted.** A key's comment is whatever its owner wrote. The mapping passes every comment, account name and path through `logview.Safe` before the view sees it: a control character is spelled out (`^[`), a bidi override or other invisible character is its code point (`<U+202E>`), a byte that is not UTF-8 is `<0xe9>`. The row then cuts a long comment with the ellipsis; a marked key keeps its mark (`KeepNote`), the comment going under the type where both don't fit. `comments` and `TestKeysCommentsSafe` pin it.
- A section per account, its name the header: the layout's section has one badge, so a badge per account under one header can't be drawn without a component change.
- A weak or shared key takes the warning marker; a refused one keeps the bullet; a file that can't be read is the bad marker. Without colour, warning and bad share `[ ]`, so the words say which.
- "Keys" counts a key once per account that holds it; "Shared" counts a shared key once.
- A key twice in one account is listed once. Options (`from=`, `command=`) are read past and not shown.
- The summary is the first section, not status rows: status rows are the main menu's controls.
- The fold is by count (10), not by the terminal's height, so a render does not depend on the window.

## 6. For the screen unit

- `keysView` in `internal/tools/uigallery/keys.go` is the stand-in mapping, from `authkeys.File`s and sshd's two settings to `screen.SSHKeys`. It moves into `pkg/screens`.
- **authkeys drops what the review most wants.** `authkeys.ParseLine` returns `ErrNotKey` for `ssh-dss` (not in `kinds`) and for an RSA key under 1024 bits (`MinRSABits`), so `authkeys.Read` leaves both out. The mock finds a DSA line by its first word; an RSA key under 1024 bits is not shown at all. The reader should report such lines. A DSA line with options before its type is missed here.
- The mock reads `.ssh/authorized_keys` alone. The screen needs sshd's `AuthorizedKeysFile` per account (`sshd.Effective`), `RequiredRSASize` and `PubkeyAcceptedAlgorithms` from `sshd -T`, as the SSH Login control reads them; with `sshd -T` unread, "Refused by sshd" can't be said.
- A file "can't be read" in the mock is a directory in its place, the one read error the fake host gives. On a host it is whatever `ReadFile` fails with other than not being there.
- StrictModes (`authkeys.CheckStrict`) is not shown: a file sshd ignores still lists its keys here.
- Not mocked: a key in two of one account's files, `AuthorizedKeysCommand`, certificates (`cert-authority`), a key's options, removing a key from here (User Management's).
- The roadmap's entry is not in this worktree: `docs/roadmap.md` is untouched here.
- Nothing ran on Linux.
