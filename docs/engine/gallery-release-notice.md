# The release notice

`next/release-notice`: the "new release available" notice of the release check ([screen-release-check.md](screen-release-check.md)) in more places:

- on the main menu, under the repository link and not in its place;
- on `hardn --version`, on its Version line;
- after verify.sh's last line, and in install.sh when it installs a named version.

Adopted 2026-11-14, in the maintainer's words: "adopt all, but include the following revisions/additions", with four revised mocks (§4). Built (§5). The `--help` notice was then dropped (§4, R2).
Status: built; `make check` passes; run on two hosts (§6).

## 1. What to run

```
go run ./internal/tools/uigallery release-notice [-ascii] [-width 60] [mock | all]
go run ./internal/tools/uigallery release-notice -list
```

The mocks are `internal/tools/uigallery/releasenotice.go`; the goldens `internal/tools/uigallery/testdata/golden/release-notice/`. The `release-check` goldens `newer` and `failed` changed with the menu.

## 2. The mocks

Each is hardn v0.4.0 with v0.4.1 out, unless it says otherwise.

| Mock | What it shows | Drawn through |
| --- | --- | --- |
| `menu-newer` | The main menu, "v0.4.1 available" under the link | hardn's check and the real menu |
| `menu-failed` | The same, a muted "Release check failed" | the same |
| `header` | The header alone, for both | the same |
| `version-newer` | `hardn --version` as root: "Version:    0.4.0 (v0.4.1 available)" | the real `PrintVersionInfo` and check |
| `version-none` | `hardn --version` with nothing newer | the same |
| `version-not-root` | `hardn --version` as a user who cannot read the cache: nothing said | the same, on a host that refuses the cache |
| `verify` | The last line of verify.sh, then the notice | fixed text |
| `install` | The end of install.sh for `HARDN_VERSION=0.4.0`, with the notice | fixed text |

Fixed text is the output's last lines off a terminal, then the notice, "hardn v0.4.1 available (https://github.com/abbott/hardn)". The gallery cannot call a shell script: `scripts/install_test.sh` holds the real ones to these words. The lines above are left out, some being wider than 80 columns.

At 80 columns the notice is at the link's column. Below 64 the link is already under the version, and the notice goes under the link: `-width 60`.

```
hardn  v0.4.0              https://github.com/abbott/hardn
                           v0.4.1 available
```

## 3. The decisions, as they were put

Every recommended (a) is adopted, but for what §4 revises: 5 and 7 change where the words go.

1. **`--version`: show it?** *Gates 2, 3 and 5.* (a) *Recommended, drawn:* yes, from the cache alone. `--version` never makes a request and never waits. (b) No: the menu alone says it, and 2, 3 and 5 fall away.
2. **Who can read the cache.** `/var/lib/hardn` is root's, 0700, and holds the rollback journal; `release.json` is 0600. (a) *Recommended, drawn:* root alone. `sudo hardn --version` shows the line; `hardn --version` as a user shows nothing. Nothing on disk changes. (b) `release.json` readable by all: the directory would have to let every user in, beside the journal. The file holds the time of the last check, whether it answered or failed, and the tag: nothing about the host. (c) A second copy all can read, outside `/var/lib/hardn`: one more file and directory for hardn to write and to check. Root alone writes it, in a root-owned directory that group and others cannot write, and it is read with the cache's checks (`revert.ReadStateFile`, `decodeCache`); a copy a user could write would let that user announce a fake release, as the old `/tmp` cache did (V26).
3. **Does `checkReleases` gate `--version`?** Today `--version` answers before hardn.yml is read. (a) *Recommended:* yes: read hardn.yml there for this key alone, as `-n` reads it (`config.LoadExisting`: writes nothing, offers nothing), with `-f` and `HARDN_CONFIG`. A file missing or not valid means no line and no message. The cost: `--version` reads one more file. (b) No: show it whenever the cache holds a newer tag. hardn.yml is not read, but the line stays after `checkReleases` is turned off, and "off, the cache is not read" stops being true.
4. **`--help`.** (a) *Recommended:* not at all. The help is cobra's fixed text, the same for every command and user; it would start reading files, and whoever reads the help has not asked about versions. `--version` and the menu say it. (b) Its last line, as the `help` mock (since removed): the same rules as `--version` (1 to 3, 5).
5. **stdout or stderr, on `--version`.** Nothing parses the output today: install.sh and verify.sh never run hardn, and `scripts/install_test.sh` does not either. `cmd/hardn`'s `TestVersionFlag` pins the five lines, which stay as they are with no newer release. .github/SUPPORT.md, .github/SECURITY.md and the bug report template (.github/ISSUE_TEMPLATE/bug_report.yml) ask a reporter to paste it. (a) *Recommended, drawn:* stdout, the last line: it is part of the answer, and a pasted report shows it. (b) stderr: `hardn --version > file` holds today's five lines always, and a pasted report may lose the line.
6. **verify.sh and install.sh.** *Gates 8.* (a) *Recommended, drawn:* verify.sh, and install.sh given `HARDN_VERSION`, ask GitHub's API for the latest tag once, after everything else has passed, and add the line when it is newer. Not gated by `checkReleases`: the scripts read no hardn.yml and already download from GitHub. No answer in 5 seconds, a rate limit or a tag that is not `v<major>.<minor>.<patch>`: nothing is said and the exit status does not change. install.sh with no version asks nothing more: it installs the latest. In install.sh the line goes above "Run hardn --help or sudo hardn", which `scripts/install_test.sh` holds to be the last. (b) verify.sh alone. (c) Neither: no new request; whoever names a version chose it.
7. **The words.** A cached tag that is not newer than the running hardn shows nothing anywhere, as on the menu today (hardn upgraded since the last check). The cache is as old as the last time the main menu was opened: nothing else makes the check, so on a host run by flags alone the tag may be weeks old. (a) *Recommended, drawn:* "v0.4.1 available" everywhere, on `--version` after a blank line. It stays true when a later release is also out. None says how to update. (b) Also how: install.sh's command is 78 characters, too long for the menu's line; on `--version` and the scripts it would be a second line, "Update: curl -fsSL ... | sudo sh". (c) `--version` says "Latest:     v0.4.1", in the shape of its other lines: false while the cache is old and a later release is out, so it needs a limit on the cache's age.
8. **The README's "No telemetry" sentence.** It says hardn makes no network request unless `checkReleases` is on. (a) *Recommended:* leave it. Options 1 to 5 add no request, so it stays true; 6 (a) adds one by the scripts, not by hardn, to the host they already download from, and docs/verify.md and the README's install steps would say so when it is built. (b) Add to the sentence that install.sh and verify.sh ask GitHub for the latest tag.
9. **The menu: under the link, or in its place.** Gates nothing. (a) *Recommended, drawn:* under the link, for "v0.4.1 available" and "Release check failed" alike. The link stays; the menu is one row taller while a notice shows (the fixture's goes from 35 lines to 36 at 80 columns). This reverses decision 1 of [gallery-release-check.md](gallery-release-check.md). (b) As today: in the link's place, no added row. (c) Only the newer release moves under the link; a failed check stays in its place: two forms to keep.

## 4. Adopted, with revisions

The maintainer's revised mocks win over §3 where they differ.

- **R1, `hardn --version`** (decisions 1 to 3, 5 and 7). No added last line: the notice is on the Version line, and a Repo line is added, newer release or not. On stdout.

  ```
  hardn - Linux hardening tool
  Version:    0.4.0 (v0.4.1 available)
  Build Date: 2026-11-04T12:00:00Z
  Git Commit: 0123456789abcdef0123456789abcdef01234567
  Repo:       https://github.com/abbott/hardn
  License:    GNU AGPL v3, no warranty
  ```

  "Version:    0.4.0" alone with nothing newer, a cache that cannot be read (not root), `checkReleases` off, or hardn.yml missing or not valid. From the cache alone, never a request.
- **R2, `hardn --help`: dropped.** The orchestrator read decision 4 as reversed from the maintainer's revised `help` mock, which kept the notice, and it was built: the notice after the root help's last line. The maintainer then dropped it (2026-11-14), in his words: "Drop --help notice". Decision 4 (a) stands: the help is cobra's fixed text and reads no file.
- **R3, verify.sh.** After "Verified ./hardn-linux-amd64 (hardn v0.4.0)", a blank line, then the same line.
- **R4, install.sh with `HARDN_VERSION`.** After "Installed hardn v0.4.0 as /usr/local/bin/hardn", a blank line, the same line, a blank line, then "Run hardn --help or sudo hardn", still the last.
- **Unchanged:** 6 (a), the scripts' rules; 8 (a), the README's sentence stays, and docs/verify.md, docs/install.md and the README's install note say the scripts ask; 9 (a), the menu, whose words stay "v0.4.1 available".

## 5. Built

- **The menu** (9 (a)): `screen.Release` sets `component.Header.NoticeBelow` for both notices (`pkg/ui/screen/release.go`). The header is one line taller while one shows.
- **`--version`** (R1): `version.Service.Newer` and the Repo line (`pkg/version/service.go`); `version.RepoURL` is the one link, the menu's too.
- **`--version`** asks `newerRelease` (`cmd/hardn/release.go`): hardn.yml through `config.LoadExistingQuick` (`-f`, `HARDN_CONFIG`, the search path; writes nothing, says nothing; a FIFO, a device or a file over 1 MiB is refused without waiting on it), then `version.Checker.Release` with no `Latest`, so the cache's checks and tag comparison are the menu's. A development build says nothing.
- **verify.sh and install.sh** (R3, R4): `release_notice`, the same in both. One `curl` to `https://api.github.com/repos/abbott/hardn/releases/latest`, HTTPS and TLS 1.2 or later, `--max-time 5`, after everything else passed; the tag `v<major>.<minor>.<patch>`, each part one to six digits, compared by number in POSIX sh (`later`, `num`). Anything else says nothing, and the exit status is unchanged: an interrupt during the request ends with status 0. install.sh with no `HARDN_VERSION` makes no second request.

Tested, with no network:

- `cmd/hardn` `TestReleaseNotice`: `--version`, `-v` and `-f … --version`, each for a newer, same and older release, a failed check, no cache, a cache not valid or another user's, `checkReleases` off, hardn.yml missing or not valid, not root, and a development build; no request and no write in any. `TestReleaseNoticeHostileConfig`: a hardn.yml that is a FIFO, `/dev/zero` or over 1 MiB, nothing said and no wait. `TestVersionFlag`: the Repo line. `TestHelpNoReleaseNotice`: `--help`, `-h`, `help` and `config --help` are the same with a newer release in the cache, and read neither hardn.yml nor the cache.
- `pkg/version` `TestPrintVersionInfo`; `pkg/ui/screen` `TestRelease`; `pkg/screens/mainmenu` `TestReleaseLine`; `pkg/ui/component`'s header table.
- `scripts/install_test.sh`, under sh and dash, for both scripts against a stub `curl`: newer; newer and older by number, not by letter (0.9.0 and 0.10.0); same; older; tags that are no version (a suffix, none, two, shell text, no `v`, seven digits, two parts); an answer that is no JSON; a failed request; a timeout; the request last, once, with `--max-time 5`; none without `HARDN_VERSION`; none after a refusal; INT, TERM or HUP during the request, status 0.
- The gallery's goldens.

## 6. On two hosts

2026-11-14: centos10-test-120 (CentOS Stream 10) and alpine324-test-116 (Alpine 3.24, a container). A build of this branch as 0.8.0, v0.9.0 being the latest release; the cache written by hand. Both hosts left as they started. The run was before the `--help` notice was dropped: it showed there then, and the help has not been run on a host since.

| Case | Result |
|---|---|
| root, cache v0.9.0, `checkReleases: true` (both hosts) | `Version:    0.8.0 (v0.9.0 available)` |
| no cache; cache v0.8.0; `checkReleases: false`; no hardn.yml (CentOS) | `Version:    0.8.0` |
| the user nobody (both hosts) | `Version:    0.8.0` |
| hardn.yml a FIFO (CentOS) | answers at once, no notice |
| the cache after all of it (CentOS) | not changed |
| install.sh, `HARDN_VERSION=0.8.0` (CentOS) | the notice above "Run hardn --help or sudo hardn"; status 0 |
| install.sh, `HARDN_VERSION=0.9.0` (CentOS) | no notice; status 0 |
| verify.sh's notice function alone, real request (CentOS, bash as sh) | 0.8.0: the notice; 0.9.0, 0.10.0 and a repository that is not there: nothing, status 0 |

## 7. Not verified

- verify.sh from start to end: it needs gh signed in, which no test host has. Its notice function ran alone.
- The scripts' request under busybox sh: the Alpine host has no curl.
- GitHub's rate limit for a request with no token. A rate limit is expected to be an HTTP error, which `curl -f` fails on.
- The real menu's height on a host: the menu was not opened there.
