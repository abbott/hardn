# Account Review (`pkg/screens/users`, `screen.Accounts`)

The real unit of account hygiene: the screen [gallery-accounts.md](gallery-accounts.md) mocked, as adopted there (2026-11-13, "Adopt all"), now User Management's item 3. It reads only: no control, step, plan, journal or undo, because hardn changes nothing here.
Status: built and tested in process (`make check`). No hardn binary ran. **Nothing ran on Linux**: every fact about `lslogins`, the login records and each distribution is read from source or not verified (§5).

## 1. What it does

User Management's `3) Account Review` says how many accounts it lists ("5 accounts to review", "Nothing to review"), each counted once. Its screen has three sections, each row an account and the command to run, and `0) Return`:

| Section | Lists | Row |
|---|---|---|
| Empty password | every account whose password field is empty (shadow(5): no password is required); `!`, `*` and `!` before a hash are not empty | "Run passwd -l carol", drawn bad |
| Password never expires | accounts with a password set and no date for it to expire on | "Run chage -M 365 george", plain |
| No login in 90 days | accounts the User row takes as able to log in (uid 1000 or more, a login shell), not expired, whose last login is 90 days old or more, or not on record | "2026-03-02, run usermod -e 1 alice", "Never, run usermod -e 1 deploy", a warning |

A section with no account says "None". What could not be established is said, never assumed:

| The host | Shown |
|---|---|
| neither `/var/log/lastlog` nor `/var/lib/lastlog/lastlog2.db` | the last section says "This host keeps no login record" and lists no one; `lslogins` is not run |
| a record with no login of any account in the last 90 days | the last section says "The login record has no login in 90 days" and lists no one: nothing shows the record is still written, so "Never" would be a guess |
| a record, but `lslogins` missing, failing, its output not in its form, or lacking an account | the last section says "Can't read the login record"; the item says it too where nothing else is listed |
| `/etc/passwd` or `/etc/shadow` not read, malformed, or `/etc/tcb` there | the screen says why and lists nothing; the item says "Can't read the accounts" |

The main menu: an empty password is the finding `USERS_EMPTY_PASSWORD` on the User row, the line "Empty password" under User, 10 points ([risk.md](risk.md) §5.3). The other two sections reach nothing outside this screen (decision 7).

## 2. The API

- `accounts.ShadowEntry.NeverExpires` and `.Expire` (`pkg/host/accounts/shadow.go`): read from the third, fifth and eighth fields as `ParseShadow` checks them. Nothing of the password field is kept.
- `accounts.ReadLogins(ctx, sys) (Logins, error)` (`logins.go`): stats the two record files, then runs `lslogins --export --time-format=iso --output=USER,LAST-LOGIN --wtmp-file=/dev/null` through `host.System`. `ErrNoLoginRecord` (`ACCOUNTS_NO_LOGIN_RECORD`), `ErrLogins` (`ACCOUNTS_LOGINS_MALFORMED`), which names a line and never its content.
- `users.ReadReview(ctx, sys, now) Review` (`pkg/engine/controls/users/review.go`): the three lists as facts, `Err`, `NoRecord`, `NoRecent`, `LoginErr`. `users.StaleDays` is 90.
- `users.CodeEmptyPassword`, made by `Admin.Check` in every state; `risk.KindEmptyPassword`, `risk.PenaltyEmptyPassword`.
- `usrscreen.AccountsView(Review) screen.Accounts` (`pkg/screens/users/rows.go`): the one mapping. The gallery's `accounts` mocks draw through it.
- `screen.Users.Review`, `screen.UsersReviewItem`. `cmd/hardn` is unchanged: it already runs `usrscreen.Run`.

## 3. Engineering calls

- **"Never expires" is what `chage -l` calls never**: no last change, no maximum age, or a maximum of 10000 days or more (shadow's `src/chage.c`), so login.defs' 99999 counts. A last change of 0 (change at next login) does not.
- **wtmp is left out** (`--wtmp-file=/dev/null`): it is rotated, so an account missing from it says nothing. The last login is lastlog2's or `/var/log/lastlog`'s, as lslogins reads them.
- **A record with no recent login calls no one stale.** A `/var/log/lastlog` left behind by an upgrade to a release that no longer writes it would make every account look stale; with no login of anyone in 90 days hardn lists no one and says what it read, "The login record has no login in 90 days" (`Review.NoRecent`), not that the host keeps no record. Not settled by the mock page, neither the call nor its words: reported as a decision.
- **An expired account is not stale**: once `usermod -e 1` has run, the row goes.
- **Stale means 90 days or more**, to the second, by the clock hardn runs with.
- **The cost of an empty password is 10**, under a second root's 15. The page adopted the finding, not its cost: reported as a decision.
- **Names are made safe** (`logview.Safe`) in the mapping, and a name the shell would not take as one word is single-quoted in its command.
- **The commands are the same on every family**, worded in the view.
- **No `play accounts`**: `play users` runs the screen on the simulated hosts, item 3 with it. Those hosts have no login record.
- **`Check` reads `/etc/shadow` on every host** now, for the finding; it read it only with a candidate before.

## 4. Tested

- `pkg/host/accounts`: the ageing fields; `ReadLogins` with each record file, both and neither; `lslogins` missing and failing; a Stat that fails; every malformed output, the error never holding the content.
- `pkg/engine/controls/users`: each list on a host with every kind of account; a clean host; no record; a dead record; each way the record is not read; passwd and shadow missing, unreadable and malformed; `/etc/tcb`; read only; the finding.
- `pkg/engine/risk`, `pkg/output`: the factor, its line, the three codes.
- `pkg/screens/users`: item 3 and its screen live and under plan only, `0`, `q`, nothing written; no record; the record not read; the accounts not read; the mapping, with a control character and names to quote. Every host's golden gained item 3.
- `pkg/ui/screen`, the gallery: the view, eight `accounts` mocks, `play users`.

## 5. Not verified

- Read from util-linux's `login-utils/lslogins.c` (v2.37, v2.41, fetched), not run: the options, the `LAST-LOGIN` column, the order wtmp, lastlog2, lastlog, that an unopened record leaves the field empty. The exact text of `--time-format=iso` (taken as `2026-03-02T10:00:00+00:00`) and of `--export` lines is not verified.
- Not verified at all: which record each distribution writes (Debian 12 and 13, Ubuntu 24.04 and 26.04, EL 9 and 10, Alpine, Proxmox); whether each ships `lslogins`, and one new enough for `--wtmp-file`; whether Debian 13's is built with lastlog2; whether sshd without PAM writes either record; `passwd -l`, `chage` and `usermod` on Alpine.
- Not run: the hardn binary, anything on Linux, arm64.
