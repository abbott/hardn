# Release check mock

*Since the real unit (2026-11-13): the check is built ([screen-release-check.md](screen-release-check.md)). The mocks draw through hardn's own check and mapping. Where this page says nothing is wired or built, or that `pkg/version/checker.go` stays, this note is current.*

The mock unit of the roadmap's "Check for new hardn releases" (proposal [P-15](../refactor/proposals.md#p-15)), `next/release-check`. It is the main menu's one line alone. **Adopted (the maintainer, 2026-11-13): "Adopt all".** Every recommended answer of §3 is adopted, with the Made here calls as stated, §4's words and §6's design.
Status: built and tested on macOS in process (`go test ./pkg/ui/... ./internal/tools/uigallery/`). No hardn binary ran. Nothing ran on Linux. Nothing is wired into hardn: no request, no cache, no hardn.yml key; `cmd/hardn`, `pkg/version` and `pkg/screens` are untouched, and `pkg/version/checker.go` stays disabled.

## 1. What to run

```
go run ./internal/tools/uigallery release-check [-ascii] [mock | all]   # the listing, no input
go run ./internal/tools/uigallery release-check -list                    # the mocks' names
```

The view is `pkg/ui/screen/release.go` (`screen.Release`, a field of `screen.MainMenu`), drawn by `component.Header`'s new `Notice`; the mocks `internal/tools/uigallery/release.go`; the goldens `internal/tools/uigallery/testdata/golden/release-check/`. With no `Release` set, every existing render is byte-identical: no other golden changed.

## 2. The mocks

Each mock is the main-menu fixture (hardn v0.4.0) with one state of the check.

| Mock | State | The first line |
| --- | --- | --- |
| `newer` | on, v0.4.1 is out | `hardn  v0.4.0              v0.4.1 available` |
| `current` | on, this is the latest | as today: the repository link |
| `pending` | on, no answer yet (the first start after turning it on) | as today |
| `failed` | on, the last check got no answer | `hardn  v0.4.0              Release check failed`, muted |
| `off` | off, the default | as today |

The notice takes the repository link's place, so the menu is no line taller, nothing is asked and nothing waits. Below 64 columns it goes under the version, as the link does.

## 3. Decisions for the maintainer

Drawn: every recommendation below that the main menu shows (1 to 4).

1. **Where the line goes.** (a) *Recommended, drawn:* in the header, in the repository link's place. No added row; the link is gone while the notice shows. *Reversed 2026-11-14: the notice is now under the link ([gallery-release-notice.md](gallery-release-notice.md), decision 9).* (b) In the link's place, with the link on a second line, as origin/main's checker drew it: one row more whenever a notice shows. (c) A warning callout above the divider, as the CVE alert: four rows more, and it reads as a problem with the host.
2. **The words for a newer release.** (a) *Recommended, drawn:* "v0.4.1 available". (b) "v0.4.1 available: run install.sh", which says how, and is 34 cells where (a) is 16.
3. **Up to date.** (a) *Recommended, drawn:* nothing is shown. (b) A muted "Up to date" in the link's place: the link would then never show while the check is on.
4. **A check that failed.** (a) *Recommended, drawn:* a muted "Release check failed" in the link's place, until a check answers. Without it, a host that cannot reach GitHub looks up to date for ever. (b) Nothing on the menu; only the log says it. A check that has not run yet shows nothing either way.
5. **The key's name.** *Gates the real unit.* (a) *Recommended:* `checkReleases: false`, a bool beside `dryRun` under "General" on the Configuration screen. (b) `enableReleaseCheck`, after `enableBackups`. P-15 also allows a flag; recommended: no flag, the key alone, so one place says whether hardn makes a request.
6. **The README's sentence.** *Gates the real unit's release.* Today: "**No telemetry.** `hardn` never phones home, and needs no account or cloud service." (a) *Recommended:* "**No telemetry.** `hardn` makes no network request unless you turn on `checkReleases`, which asks GitHub for the latest release's tag once a day and sends nothing about the host. It needs no account or cloud service." (b) Keep today's sentence and describe the check only in docs/configuration.md.
7. **What the check tells apart.** (a) *Recommended:* a newer release, nothing else: the release's name and body are never read, so no release can call itself a security release and change the menu (origin/main's did). (b) A second wording for a release that fixes a security problem, which needs a signal hardn can trust; none exists today.

## 4. Words

To decide with §3: "v0.4.1 available"; "Release check failed".

## 5. Made here

- `component.Header` has a `Notice` (and `NoticeMuted`) drawn where the URL is, by the URL's own rule for moving down. A newer release is in the primary text colour, a failed check muted: no new theme style.
- `screen.Release` holds the state (`ReleaseOff`, `ReleaseCurrent`, `ReleasePending`, `ReleaseNewer`, `ReleaseFailed`) and the newer tag; its zero value is off. `ReleaseNewer` with no tag draws nothing.
- The compact main menu (gated off, `CompactMainMenu`) has no header, and so no notice.
- The listing is `release-check`; there is no `play` entry, as the line takes no input.
- No component fixture state was added for the notice: the header's table test and the screen's hold it.

## 6. For the real unit

None of this is built. It is the design the mock assumes.

**The key.** `checkReleases`, default `false` (decision 5). Off, hardn makes no request and reads and writes no cache, as today. hardn.yml.example and docs/configuration.md name it; the Configuration screen lists it.

**When it runs.** Only at the interactive main menu, as root, with the key on: never for `hardn system-details`, `-p`, a run with a flag, or plan only's own sake (plan only changes nothing about it: the check changes nothing on the host but its cache). At most one request in 24 hours, counted from the last attempt, answered or not. It runs beside the menu, never before it: the menu draws at once from the cache, and a new answer shows the next time the menu draws. One check per start, in one goroutine (origin/main raced two).

**The request.** `GET https://api.github.com/repos/abbott/hardn/releases/latest`, the URL install.sh reads. HTTPS only, TLS 1.2 or later, no redirect to another host followed, no proxy setting beyond Go's default from the environment. Headers: `User-Agent: hardn/<version>` and `Accept: application/vnd.github+json`; nothing else. No token, no cookie, no query string, no body: nothing about the host, its distribution, its hostname or its configuration is sent. GitHub sees the host's address and hardn's version, which is what the README's new sentence must not hide (decision 6).

**Timeouts.** 5 seconds for the whole request, connection included; the response read to at most 64 KiB. Leaving the menu, or hardn, cancels it.

**The answer.** Only `tag_name` is read. It must be `v<major>.<minor>.<patch>`, digits alone, as install.sh's `check_version` asks; anything else is a failed check. It is compared by number with this build's version; a build whose own version is not of that form (a development build) never checks. Name, body and URL are not read (decision 7), and no URL from the answer is ever shown: the notice names the tag alone.

**The cache.** `/var/lib/hardn/release.json`, in hardn's state directory (`revert.DefaultDir`, root's, 0700), mode 0600, owner root. It holds the time of the last attempt, whether it answered, and the tag. It is written to a temporary file in the same directory and renamed. Before it is read, the same checks as the journal's (`pkg/host/revert/store.go`): the directory and the file are root's, not writable by group or other, the file a regular file and not a symlink, at most 4 KiB. Then its content: the JSON decodes with no unknown field, the tag is of the form above, and the time is not in the future. A cache that fails any check is not used, counts as no attempt, and is replaced by the next check; nothing from it reaches the screen. `/tmp` is never used, and origin/main's `/tmp/hardn-version-cache.json` is never read.

**What failure shows.** No answer in time, a status other than 200, or an answer that does not pass: the cache records a failed attempt, the menu says "Release check failed" (decision 4), and the log has one line with the reason. hardn retries after 24 hours. Nothing else changes: no prompt, no exit, no change to the risk level or the status rows. A tag older than this build, or equal, is "up to date".

**Updating.** The notice only tells. Installing stays install.sh's, which checks the release against `SHA256SUMS`.

**The old checker.** `pkg/version/checker.go` and its `updateCheckEnabled` gate are replaced by the real unit, not flipped: its `/tmp` cache, its security-release screen and its `HARDN_CLEAR_CACHE` have no place in this design.

**Tests the real unit owes.** With the key off, no request and no file access (the checker's `doRequest` seam shows how). A planted cache (wrong owner, a symlink, group-writable, a future time, a tag such as `v99; rm`, an unknown field) is never drawn. A slow or failing server never delays the menu.

**Not verified here.** That GitHub's API refuses a request with no `User-Agent`, and its rate limit for requests with no token, were not checked against GitHub's documentation; the design sends a `User-Agent` either way and makes one request a day.
