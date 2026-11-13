# The end-of-support notice (`output.NoticeOf`, `detect.Platform.End`)

The real unit of the roadmap's "End-of-support notice", `next/eol-notice`: the notice [gallery-eol.md](gallery-eol.md) mocked, as adopted there (2026-11-13, "Adopt all").
Status: built and tested in process on macOS (`make check`). No hardn binary ran. *Nothing ran on Linux* (§7).

## 1. What it does

From 90 days before the last day of a release's vendor support, and for good after it, by the host's clock:

- **The main menu** draws a warning between the status rows and the divider, after the CVE alert where there is one: "Alpine 3.22 support ends 2027-05-01", or "support ended", then "Upgrade to Alpine 3.24." On an Out of scope release it also says "Out of scope: hardn refuses every change."
- **Each plan** carries one finding, `PLATFORM_SUPPORT_ENDS` or `PLATFORM_SUPPORT_ENDED`: "Alpine 3.22 support ends 2027-05-01: upgrade to Alpine 3.24". A screen's review lists it before the confirmation, and the report and the audit log's line hold it, as they do `PLATFORM_BEST_EFFORT`.
- **The JSON document's `host.platform.supportEnds`** is the date, at any time: `2027-05-01`, a month `2027-05`, or `""`.

It is a notice alone. A release's support level is the matrix's and moves only with a new hardn: the gate (`pkg/engine/plan.go`, `refuseApply`) never reads the clock, so a host whose clock is wrong gets a wrong notice and no wrong refusal. A Best effort or Supported release past its date is changed as before; an Out of scope one is refused as before, on any day.

No notice: more than 90 days before the date; Proxmox VE 9, whose vendor names no date; a release with no matrix row (an Ubuntu interim release, an unlisted version, an Alpine build that is not a stable `x.y.z`); a release hardn could not establish.

## 2. The dates

`detect.Release.Ends` (`pkg/host/detect/platform.go`), the last day of free security updates, the vendor's long-term support included and paid support left out. A day, or a month where the vendor names a month alone (Ubuntu, Proxmox VE), which lasts to its last day and is worded as the month, "May 2027". The vendor pages are in the matrix's comment; gallery-eol.md §3 has what each says.

| Release | Ends | | Release | Ends |
|---|---|---|---|---|
| Debian 13 | 2030-06-30 | | Proxmox VE 9 | none |
| Debian 12 | 2028-06-30 | | Proxmox VE 8 | 2026-08 |
| Ubuntu 26.04 | 2031-05 | | Rocky Linux 10, AlmaLinux 10, RHEL 10 | 2035-05-31 |
| Ubuntu 24.04 | 2029-05 | | Rocky Linux 9, AlmaLinux 9, RHEL 9 | 2032-05-31 |
| Ubuntu 22.04 | 2027-05 | | Alpine 3.22 | 2027-05-01 |
| Alpine 3.24 | 2028-06-01 | | Alpine 3.21 | 2026-11-01 |
| Alpine 3.23 | 2027-11-01 | | Alpine 3.20 | 2026-04-01 |

Read again for this unit on 2026-11-13, from each page's own text: Debian (wiki.debian.org/LTS), Ubuntu, Alpine, Proxmox VE, Rocky Linux and AlmaLinux, all as above. *Unverified here:* RHEL's two dates, which are gallery-eol.md's reading of Red Hat's life-cycle data; Debian 13's LTS architectures, which Debian lists as to be determined; and whether Alpine's "End of support" day is the last supported day or the first unsupported one. hardn treats it as the last.

## 3. The API

- `detect.Release.Ends`, and `detect.Platform.Ends`, which `Classify` copies from the row.
- `detect.Platform.End(today) detect.End`: `State` (`EndNone`, `EndLater`, `EndSoon`, `Ended`), `Date` as the matrix writes it, `Words` as a notice says it; `End.Due()`. `detect.EndNear` is 90. Pure.
- `detect.Platform.Upgrade()`, `detect.Newest(distro)`: the newest Supported row of the distribution. `detect.Distro.Name()`: "Rocky Linux", which the engine's findings now use too.
- `output.NoticeOf(platform, today) *screen.Alert`: the one mapping to the main menu's notice (`screen.EndOfSupport`, `screen.MainMenu.Notice`). The gallery's `eol` mocks draw through it.
- `mainmenu.StatusOptions.Now`, nil for `time.Now`; `cmd/hardn` gives it the clock every engine run takes (`revertNow`).
- `engine.CodeSupportEnds`, `engine.CodeSupportEnded`: the plan's own findings (`Plan.Findings`), from the engine's clock (`Options.Revert.Now`).
- `output.Platform.SupportEnds`, `host.platform.supportEnds` in the document.

## 4. Engineering calls

- **The rule is in `pkg/host/detect`**, beside the matrix, not beside `output.AlertOf` as gallery-eol.md §6 proposed: the engine's finding needs it, and the engine cannot import `pkg/output`.
- **The day is the clock's own calendar date**, in the clock's time zone; days are whole.
- **The JSON holds the date, not "soon" or "ended".** The date is the matrix's and is the same on every run; what the clock makes of it is the plan's finding. A reader counts for itself.
- **Two codes**, so that a script tells "ends" from "ended" without reading the words.
- **Every word is hardn's**: the distribution's name is detect's, and the version and the date are the matrix row's, which the host's version only selects. Nothing the host wrote is drawn.
- **An Alpine build that is not a stable release** (`3.22.0_rc1`) is unlisted and has no date, though its branch has a row.
- **A date in neither form is no date**: nothing is claimed from it. A test holds every row's date.
- **The matrix's notes are unchanged**: Alpine 3.21's and 3.20's repeat the row's date, and the refusal's words quote them. Proxmox VE 8's does not match its date: §5.
- **`play` has no notice**: the gallery's hosts are drawn with no clock. The `eol` listing is where the notice is drawn.
- **Tests that read a status take a fixed day**, so that none changes as the real clock nears a date.
- The goldens that changed: every JSON document's, for `supportEnds`, and the code list's. No drawn golden changed.

## 5. Open: one decision for the maintainer

**Proxmox VE 8's two end dates.** On a Proxmox VE 8 node hardn now gives two:

- the notice and the plan's finding say the month, from the row's date `2026-08`: "Proxmox VE 8 support ended August 2026";
- the refusal quotes the row's note, which this unit left alone: "proxmox 8 is out of scope (end of life since 2026-08-31)" (`pkg/engine/plan.go`, `refuseApply`).

Both are in one row of `pkg/host/detect/platform.go`: `{DistroProxmox, "8", OutOfScope, "end of life since 2026-08-31", "2026-08"}`. The vendor's page says "2026-08" and names no day (gallery-eol.md §3). The day is from the refactor's brief, where it was decided ([detect.md](../safety/detect.md), §2 and §3's sources), so it is not this unit's to change.

- (a) *Recommended:* the note says the month, "end of life since August 2026", and detect.md's two mentions follow. One date on the screen, and the one the vendor gives. It changes the refusal's words on Proxmox VE 8 and nothing else; no test or golden holds the note.
- (b) Leave both. The refusal keeps a day the vendor never named, beside a notice that says the month.
- (c) The row's date becomes the day, `2026-08-31`, and the notice says it. One date, but against decision 6 of gallery-eol.md, adopted: a month alone is said as the month.

It gates nothing: the notice and the gate work the same under each.

## 6. Tested

- `pkg/host/detect`: the 90-day edge, the last day, the day after, a month's last day and the next month, the clock's time zone, no row, no date, a malformed date; every row's date; the newest release; the names.
- `pkg/engine`: the finding at each state and level, with the best-effort one; none without a date; the gate unchanged on every day, an apply past the date included.
- `pkg/output`: `NoticeOf` at each state, level and distribution; `supportEnds`; the two codes registered and documented.
- `pkg/screens/mainmenu`, over hostfake: each of the mocks' states read from `/etc/os-release` and `dpkg-query`, Proxmox VE 8 and 9; and no notice with os-release missing, with no ID or not an os-release at all, a `VERSION_ID` with control characters, `dpkg-query` failing or answering what hardn does not recognise, a pve-manager version that is none. `dpkg-query` missing is a Debian that is not Proxmox VE. Every call only reads.
- `cmd/hardn`: the status is given the clock. The gallery: the `eol` mocks through `NoticeOf`, their goldens unchanged.

## 7. Not run

Nothing ran on Linux, and no hardn binary ran. To see it on a host: `go test ./...`; then `sudo hardn` on an Alpine 3.21 or 3.20 host, or a Proxmox VE 8 node, whose notice shows today; and on a throwaway Alpine 3.22 VM with the clock set after 2027-01-31, the notice, `sudo hardn -n -g` for the finding, and that the change still applies after 2027-05-01.
