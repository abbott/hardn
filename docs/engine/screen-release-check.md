# The release check (`pkg/version`, `screen.Release`)

The real unit of the roadmap's "Check for new hardn releases" (proposal [P-15](../refactor/proposals.md#p-15)), `next/release-check`: the line [gallery-release-check.md](gallery-release-check.md) mocked, as adopted there (2026-11-13, "Adopt all"), built to its §6.
Status: built and tested in process on macOS (`make check`). No hardn binary ran. *Nothing ran on Linux, and no request was ever made to GitHub: every test gives the check a fake transport and clock (§5).*

## 1. What it does

With hardn.yml's `checkReleases: true`, the main menu says "v0.9.1 available" under the repository link when a newer release is out, and a muted "Release check failed" when the last check got no answer. Up to date, not answered yet, or off (the default), the header is as before. Nothing is asked, nothing waits, and hardn installs nothing.

Since `next/release-notice` (2026-11-14) the notice is under the link, not in its place, and `hardn --version`, verify.sh and install.sh say a newer release too: [gallery-release-notice.md](gallery-release-notice.md) §4.

## 2. The API

- `config.Config.CheckReleases` (`checkReleases`, default false, not written when false): a row under "General" on the Configuration screen and in `hardn config`.
- `version.Checker{On, Version, System, Dir, Latest, Now, Log}` (`pkg/version/release.go`). `Release(ctx)` is what the check knows, from this process's check or the cache, with no request and no wait. `Check(ctx)` makes the request where one is due. `version.ParseTag` is the one test of a tag: `v<major>.<minor>.<patch>`, one to six digits each.
- `github.Client{Version, Transport, Timeout}.Latest(ctx)` (`pkg/version/github`): the request. The only package of hardn that imports `net/http`.
- `revert.ReadStateFile` and `revert.WriteStateFile` (`pkg/host/revert/statefile.go`): a file of hardn's state directory under the journal's trust checks, through `host.System`.
- `output.ReleaseOf(version.Release) screen.Release`: the one mapping to the view; the gallery's `release-check` mocks draw through it and `version.Checker`, each from a cache on an in-memory host.
- `newerRelease` (`cmd/hardn/release.go`): the newer tag the cache holds, for `--version`. It builds a `Checker` with no `Latest`, so it can make no request.
- `mainmenu.Options.Release` (asked on each pass) and `CheckRelease` (started once by `Run`, in one goroutine, cancelled as the menu ends), set by `cmd/hardn`'s `withReleaseCheck`.

## 3. The rules

- **Off means nothing.** `checkReleases` false: `withReleaseCheck` sets neither hook; no request, and the cache is neither read nor written. Only `menuOptions` builds a check that can ask, so no flag and no subcommand makes a request. `--version` reads hardn.yml for this key and then the cache, and writes nothing.
- **The request.** `GET https://api.github.com/repos/abbott/hardn/releases/latest`, with `User-Agent: hardn/<version>` and `Accept: application/vnd.github+json` and nothing else. TLS 1.2 or later; a redirect off `api.github.com` or off HTTPS is refused. 5 seconds for all of it; at most 64 KiB read. Only `tag_name` is read.
- **Once in 24 hours**, from the last attempt, answered or not. The attempt is written to the cache before the request, so whatever ends hardn, no second request follows within the day; where the cache cannot be written, no request is made.
- **The cache** is `/var/lib/hardn/release.json`, 0600, written by rename. Before it is used: the directory and each above it root's and writable by no one else, the file a regular file (no symlink), root's, writable by no one else, at most 4 KiB; then one JSON object with no unknown field, a time not in the future, and a tag that passes `ParseTag`. One that fails counts as no attempt, is replaced by the next check, and nothing of it is drawn.
- **Failure.** No answer in time, a status other than 200, an answer too large or not JSON, or a tag that is no version: the cache records it (`"failed": true`), "Release check failed" until a check answers, and one `WARNING` line in `/var/log/hardn.log` with the reason, through `logview.Safe`.
- **A cancel is no failure.** Leaving the menu while a check is out records nothing new and logs nothing: the menu shows what the last check that ended said, and nothing where none has.
- **A development build never checks**: its own version does not pass `ParseTag`.

## 4. Engineering calls

- **A dry run checks as a live session does** (`-n`, or hardn.yml's `dryRun: true`), as adopted (gallery-release-check.md §6): the check changes nothing on the host but its cache, which it writes. A dry run opens no log (`liveWrites`), so a check that fails there shows "Release check failed" with no line in the log.
- **Leaving the menu while a check is out** leaves the attempt recorded, so no second request follows within 24 hours: keeping to one request a day was put first. Until then the menu shows the last answer or failure, or nothing.
- **While a later check is out, the last answer still shows.** A failed check replaces it: "Release check failed", not a stale "available".
- **No engine control and no output code.** The check changes nothing on the host but its own cache: nothing to plan, review or undo.
- **`pkg/version/checker.go` is removed**, with its tests, its `/tmp` cache, its security-release screen, `HARDN_CLEAR_CACHE` and `updateCheckEnabled`. `pkg/version/service.go` keeps `--version` alone.
- The tag is checked three times: as the answer arrives, as the cache is read, and in `output.ReleaseOf` before it is drawn.

## 5. Tested

No test makes a network request: `github.Client` gets a fake `http.RoundTripper`, `version.Checker` a fake `Latest` and clock, and the host is `hostfake`.

- `pkg/version`: `ParseTag`; each state from the cache; planted caches never drawn (another owner, a symlink, group-writable, a future time, a tag such as `v99; rm`, an unknown field, two values, too large); an unreadable cache; off touches no file and asks nothing; an answer, newer, equal and older; each failure and its log line; once a day; pending, cancelled (nothing shown, or the earlier failure), a second `Check`; a state directory that cannot be trusted asks nothing; a planted link replaced, not written through.
- `pkg/version/github`: the request's method, URL and its two headers alone; each failing status, a redirect elsewhere, an oversized answer, bad JSON; the timeout.
- `pkg/host/revert`: `ReadStateFile` and `WriteStateFile`, each refusal.
- `pkg/screens/mainmenu`: the line in each state; a check that never answers delays no drawing and no key, and ends with the menu or the input.
- `cmd/hardn`: on, off, and a dry run either way (it checks when on); off by default; the Configuration report's row; `--version` (`TestReleaseNotice`).
- The gallery: the five `release-check` mocks through the real check and mapping, goldens unchanged in what they draw.

## 6. Not run, not verified

- Nothing on Linux, and no real request: that GitHub answers this request as the code expects (status 200, a `tag_name`), through a proxy too.
- *Not verified against GitHub's documentation:* that its API refuses a request with no `User-Agent`, and its rate limit for requests with no token. hardn sends a `User-Agent` and asks once a day either way.
- The line on a real terminal.
