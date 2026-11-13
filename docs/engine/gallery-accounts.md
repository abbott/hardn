# Account Review mock

*Since the real unit (2026-11-13): Account Review is built and is User Management's item 3 ([screen-accounts.md](screen-accounts.md)). The mocks draw through its mapping, four mocks were added for what could not be read or told (`login-not-read`, `no-recent-login`, `not-read`, `not-read-menu`), and `play users` runs the screen. Where this page says nothing is wired or nothing is read, this note is current.*

**Adopted (the maintainer, 2026-11-13): "Adopt all".** Every recommended answer of §4 is adopted, with §5's words and §6's calls. The decisions below stay as the record.

The mock unit of account hygiene: a read-only list, under User Management, of accounts with an empty password, accounts whose password never expires and accounts with no login in 90 days. Each row is the command that fixes it; hardn changes nothing. There is no Figma frame.
Status: built and tested in process (`go test ./pkg/ui/screen/ ./internal/tools/uigallery/`). No hardn binary ran. Nothing ran on Linux. Nothing is wired into hardn: `cmd/hardn`, `pkg/screens`, the engine, the host readers and hardn.yml are untouched, and hardn's User Management screen draws as before (`screen.Users.Review` is nil there).

## 1. What to run

```
go run ./internal/tools/uigallery accounts [-ascii] [mock | all]   # the listing, no input
go run ./internal/tools/uigallery accounts -list                    # the mocks' names
```

The view is `pkg/ui/screen/accounts.go` (`screen.Accounts`, and `screen.Users.Review` for the item), the mocks `internal/tools/uigallery/accounts.go`, the goldens `internal/tools/uigallery/testdata/golden/accounts/`. There is no `play accounts`: the screen has Return alone.

## 2. The mocks

Each mock is a `screen.Accounts` written by hand: no host is read.

| Mock | What it shows |
| --- | --- |
| `menu` | User Management with item 3, "Account Review", "5 accounts to review"; items 1 and 2 unchanged |
| `found` | The screen with something in each section; a long name's row goes under the name |
| `no-login-record` | A host with no login record: the last section says "This host keeps no login record" |
| `clean` | A clean host: "None" in each section |
| `clean-menu` | User Management on the clean host: item 3 says "Nothing to review" |

The screen: the title bar; "Empty password", "Password never expires", "No login in 90 days", each row the account and its command; `0) Return`. An empty password is drawn bad, a stale account a warning, a password that never expires plain.

## 3. Where each fact would come from

Nothing below is read yet.

| Fact | Source |
| --- | --- |
| Empty password | shadow's second field empty, which `pkg/host/accounts` reads already (`PasswordEmpty`); shadow(5): "no passwords are required to authenticate" |
| Never expires | shadow's fifth field (maximum age) empty, for an account with a password (`PasswordSet`). `ParseShadow` checks the field is a number and does not keep it: the real unit adds it. Whether 99999 counts as never is not verified |
| Last login | No one source. Debian 13 dropped `lastlog` and `last` (its release notes, 5.1.9): `lslogins`, from util-linux, tells when an account was last used, and `lastlog2` does only with the lastlog2 and libpam-lastlog2 packages installed. The others are not verified: `/var/log/lastlog` on Debian 12, Ubuntu 24.04 and EL 9; lastlog2 on EL 10 and Ubuntu 26.04; nothing on Alpine |

Where no record is found (Alpine; Debian 13 without lastlog2, if `lslogins` has nothing to read), the section says so and lists no one: hardn never guesses a login from a file's date. The first two sections need shadow alone and are the same on every family.

The commands: `passwd -l NAME` puts "!" before the field; `chage -M 365 NAME` sets a maximum age (chage(1)); `usermod -e 1 NAME` expires the account, which usermod(8) says is what locks more than the password. On Alpine, `chage` and `usermod` come with the shadow package (not verified); `passwd -l` is BusyBox's (not verified).

## 4. Decisions for the maintainer

Drawn: the recommended answer of each.

1. **Where the list shows.** (a) *Recommended, drawn:* a sub-screen, User Management's item 3 "Account Review", whose description counts the accounts. User Management stays as short as it is, and items 1 and 2 keep their keys. (b) Rows on User Management itself, under the users: one screen fewer, but it grows with every account and the review is mixed with what hardn manages.
2. **Whether "Password never expires" is a warning.** (a) *Recommended, drawn:* listed, plain: most hosts' accounts never expire and current guidance (NIST SP 800-63B, not fetched here) does not ask for expiry, so a warning would fire nearly everywhere. (b) A warning, as CIS-style baselines have it. (c) Left out.
3. **How long without a login is stale.** (a) *Recommended, drawn:* 90 days, fixed. (b) 180 days. (c) A hardn.yml key: a key to maintain for a list that only reads.
4. **Which accounts are looked at.** *Gates the reader.* (a) *Recommended:* empty password, every account in shadow; never expires, accounts with a password; no login, accounts hardn's Users check already takes as able to log in, root aside. An account that never logged in is listed ("Never"), so one just created shows until its first login. (b) Every account everywhere: service accounts fill the last two sections.
5. **The commands.** (a) *Recommended, drawn:* `passwd -l` (lock, not set a password), `chage -M 365`, `usermod -e 1` (disable, not delete). (b) Offer `userdel` for a stale account: shorter-lived hosts may want it, but it destroys the home's owner and hardn should not suggest that from a date.
6. **A host with no login record.** (a) *Recommended, drawn:* the section says so and stops. (b) Name the package to install (lastlog2 on Debian 13): helpful, but it starts recording only from then on, and each family's package has to be verified first.
7. **Whether the main menu hears of it.** *Gates the risk score.* (a) *Recommended:* an empty password becomes a finding on the User row in the real unit, as `USERS_UID0` is; the other two stay on this screen. (b) Nothing reaches the main menu. (c) All three do.

## 5. Words

Proposed with §4: "Account Review"; "Empty password"; "Password never expires"; "No login in 90 days"; "None"; "This host keeps no login record"; "Nothing to review", "1 account to review", "5 accounts to review"; "Run passwd -l carol"; "Run chage -M 365 george"; "2026-03-02, run usermod -e 1 alice"; "Never, run usermod -e 1 deploy".

## 6. Made here

- The listing is `uigallery accounts`: `users` is taken by `play users`.
- The value column is 39 cells, so a stale row is the date and the command with no "Last login": the section's title says what the date is. A long name's row goes under the name, whole.
- An account in two sections is counted once in the item's description.
- `screen.Users.Review` is a pointer: nil, as everywhere in hardn today, draws User Management byte for byte as before.

## 7. For the real unit

- A reader for the maximum-age field and for the login record, per family (§3), and the proof of each on a VM.
- Why the accounts are not read (`/etc/tcb`, `accounts.ErrTCB`) has no state here.
- The commands are worded in the view; a family whose command differs needs them passed in.
- Account names from passwd are made safe to show, as the Configuration screen's values are.
