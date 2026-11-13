# End-of-support notice mock

*Since the real unit (2026-11-13, [screen-eol.md](screen-eol.md)): the notice is built. The dates are in the support matrix, the main menu fills the notice from the host's clock, and the mocks draw through that mapping (`output.NoticeOf`). Where this page says nothing is wired, or names `eolEnds` and `eolNotice`, this note is current.*

The mock unit of the roadmap's "End-of-support notice", `next/eol-notice`: a main-menu notice for a release whose vendor support ends soon or has ended, drawn as the CVE alert is. **Adopted (the maintainer, 2026-11-13): "Adopt all".** Every recommended answer of §4 is adopted, with §5's calls as stated. §4 is kept as the record.
Status: built and tested on macOS in process (`go test ./pkg/ui/screen/ ./internal/tools/uigallery/`). No hardn binary ran. Nothing ran on Linux. Nothing is wired into hardn: `cmd/hardn`, the engine, `pkg/output`, `pkg/host/detect` and hardn.yml are untouched, and no host or clock is read.

## 1. What to run

```
go run ./internal/tools/uigallery eol [-ascii] [mock | all]   # the listing, no input
go run ./internal/tools/uigallery eol -list                    # the mocks' names
```

The words are `pkg/ui/screen/eol.go` (`screen.EndOfSupport`), drawn by the main menu as a second callout (`screen.MainMenu.Notice`, `pkg/ui/screen/main.go`). The mocks, the dates and the 90-day rule are `internal/tools/uigallery/eol.go`; the goldens `internal/tools/uigallery/testdata/golden/eol/`. `play` has no eol screen.

## 2. The mocks

Each mock is the main menu's fixture on a release and a day, both the mock's own. The release is classified by the real `detect.Classify`; the status rows and the items are the fixture's, not the release's.

| Mock | Release, level | Day | What it shows |
| --- | --- | --- | --- |
| `soon` | Alpine 3.22, Best effort | 2027-03-15 | 47 days before the date: "support ends" |
| `soon-month` | Ubuntu 22.04, Best effort | 2027-04-01 | A vendor that names a month alone: "May 2027" |
| `ended` | Alpine 3.22, Best effort | 2027-05-10 | Past the date in a hardn that still calls it Best effort: "support ended"; hardn still changes it |
| `out-of-scope` | Alpine 3.21, Out of scope | 2026-11-13 | "support ended", and why a change is refused |
| `supported` | Debian 12, Supported | 2028-05-01 | 60 days before its LTS ends: the same notice at every level |
| `with-cve` | Alpine 3.22, Best effort | 2027-03-15 | The CVE alert and the notice: two callouts |

Plain, at 80 columns, between the status rows and the divider:

```
   # Warning: Alpine 3.22 support ends 2027-05-01
   # Upgrade to Alpine 3.24.

   # Warning: Alpine 3.22 support ended 2027-05-01
   # Upgrade to Alpine 3.24.

   # Warning: Alpine 3.21 support ended 2026-11-01
   # Out of scope: hardn refuses every change.
   # Upgrade to Alpine 3.24.
```

`with-cve`:

```
   # Warning: CVE-2026-123158
   # Upgrade openssh.
   # Select option: 2) SSH Login

   # Warning: Alpine 3.22 support ends 2027-05-01
   # Upgrade to Alpine 3.24.
```

## 3. The dates

One row for each of `pkg/host/detect/platform.go`'s matrix rows. The date proposed is the last day of free security updates, the vendor's long-term support included and paid support left out (§4, decision 1). Every page was fetched on 2026-11-13.

| Release | hardn's level | Date proposed | What the vendor's page says | Vendor page |
| --- | --- | --- | --- | --- |
| Debian 13 | Supported | 2030-06-30 | End of life 2028-08-09, end of LTS 2030-06-30 | [debian.org/releases](https://www.debian.org/releases/), [wiki.debian.org/LTS](https://wiki.debian.org/LTS) |
| Debian 12 | Supported | 2028-06-30 | End of life 2026-07-11, end of LTS 2028-06-30 | the same two |
| Ubuntu 26.04 | Supported | May 2031 | End of standard security maintenance May 2031; ESM to May 2036 | [ubuntu.com/about/release-cycle](https://ubuntu.com/about/release-cycle) |
| Ubuntu 24.04 | Supported | May 2029 | May 2029; ESM to May 2034 | the same |
| Ubuntu 22.04 | Best effort | May 2027 | May 2027; ESM to May 2032 | the same |
| Alpine 3.24 | Supported | 2028-06-01 | End of support 2028-06-01 | [alpinelinux.org/releases](https://alpinelinux.org/releases/) |
| Alpine 3.23 | Supported | 2027-11-01 | 2027-11-01 | the same |
| Alpine 3.22 | Best effort | 2027-05-01 | 2027-05-01 | the same |
| Alpine 3.21 | Out of scope | 2026-11-01 | 2026-11-01 | the same |
| Alpine 3.20 | Out of scope | 2026-04-01 | 2026-04-01 | the same |
| Proxmox VE 9 | Supported | **unconfirmed: none** | Proxmox EOL "tba" | [pve.proxmox.com/wiki/FAQ](https://pve.proxmox.com/wiki/FAQ) |
| Proxmox VE 8 | Out of scope | August 2026 | Proxmox EOL 2026-08 | the same |
| Rocky Linux 10 | Supported | 2035-05-31 | General support to 2030-05-31, security support through 2035-05-31 | [docs.rockylinux.org/releases](https://docs.rockylinux.org/releases/) |
| Rocky Linux 9 | Supported | 2032-05-31 | 2027-05-31, 2032-05-31 | the same |
| AlmaLinux 10 | Supported | 2035-05-31 | Active support until 2030-05-31, security support until 2035-05-31 | [wiki.almalinux.org/release-notes](https://wiki.almalinux.org/release-notes/) |
| AlmaLinux 9 | Supported | 2032-05-31 | 2027-05-31, 2032-05-31 | the same |
| RHEL 10 | Supported | 2035-05-31 | Maintenance support ends 2035-05-31; the paid ELS add-on to 2039-05-31 | [Red Hat's life-cycle data](https://access.redhat.com/product-life-cycles/api/v1/products?name=Red%20Hat%20Enterprise%20Linux), the data of the [life-cycle page](https://access.redhat.com/support/policy/updates/errata) |
| RHEL 9 | Supported | 2032-05-31 | Maintenance support ends 2032-05-31; ELS to 2036-05-31 | the same |

Unconfirmed, or not exact:

- **Proxmox VE 9** has no date: the vendor's is "tba". The mock gives it no notice (decision 8).
- **Ubuntu** names a month, never a day. The table holds the month; the mock counts from its last day (decision 6).
- **Proxmox VE 8**: the vendor says "2026-08". The matrix's note says 2026-08-31, a day the page does not name.
- **Debian 12's LTS** covers i386, amd64, armhf, arm64 and ppc64el. **Debian 13's** architectures are "to be determined", so 2030-06-30 for arm64 is not confirmed.
- **Red Hat** calls its end-of-phase dates approximate and subject to change.
- The RHEL and Debian rows were read from the pages' own text. The others were read through a fetch tool that summarises the page: check a date at its link before it goes into the matrix.

## 4. Decisions for the maintainer

Drawn: the recommended answer of each.

1. **Which date counts.** The date the notice names and counts from. (a) *Recommended, drawn:* the end of free security updates: Debian's LTS and Ubuntu's five years included, Ubuntu's ESM and RHEL's ELS left out. §3's dates. (b) The end of the vendor's standard support. Debian 12's passed on 2026-07-11, so a Supported release would say "ended" today while Debian still fixes it.
2. **Whether the date changes what hardn does.** (a) *Recommended, drawn:* no, a notice alone. A release's level moves only with a new hardn, so a wrong clock cannot cause a wrong refusal: the `ended` mock is Best effort and hardn still changes it. (b) Past the date, hardn refuses changes as on an Out of scope release. An old hardn then stops by itself, and a host whose clock is wrong is refused.
3. **How early the notice starts.** (a) *Recommended, drawn:* 90 days before. (b) 30 days: less time to plan an upgrade. (c) 180 days: on Alpine, whose releases last two years, a quarter of a release's life.
4. **The CVE alert and the notice together.** The menu holds one alert today. (a) *Recommended, drawn (`with-cve`):* two callouts, the CVE's first, a blank line between: five lines and two gaps more than the menu alone. (b) One at a time, the CVE's first: the notice is hidden for as long as sshd lacks the fix. (c) One callout with both: its route, "Select option: 2) SSH Login", would then stand under the notice too.
5. **The release to upgrade to.** (a) *Recommended, drawn:* the newest release of the distribution that hardn supports: "Upgrade to Alpine 3.24.", "Upgrade to Ubuntu 26.04." It names where to end up, and a vendor's upgrade may pass through a release between. The newest release itself gets no such line. (b) The next release up ("Ubuntu 24.04"): the step the vendor's tool takes, but it may be near its own date. (c) None.
6. **A vendor that names a month alone** (Ubuntu, Proxmox VE 8). (a) *Recommended, drawn (`soon-month`):* the notice says the month, "May 2027", and counts from its last day. (b) A day hardn picks, shown as a day: a date the vendor never gave.
7. **Which levels get the notice.** (a) *Recommended, drawn (`supported`, `out-of-scope`):* every release with a date, at every level. On an Out of scope release it is where the menu says why changes are refused. (b) Best effort and Out of scope alone: a Supported release near its date, as Debian 12 in 2028, says nothing until a new hardn moves it.
8. **Proxmox VE 9, whose vendor names no date.** (a) *Recommended, drawn:* no date and no notice until the vendor names one. (b) Debian 13's date. Proxmox VE 8 ended a month after Debian 12's standard support, not its LTS, so this would be 2028, a guess.
9. **The words.** (a) *Recommended, drawn:* "\<release> support ends \<date>", "\<release> support ended \<date>", "Out of scope: hardn refuses every change." and "Upgrade to \<release>.", each under the callout's "Warning:". The notice stays for good once the date has passed. (b) Other words, or days left ("in 47 days").

Gated by these: the real unit, which puts the dates in the matrix and fills the notice from the host's clock, and the same fact in `--status`, the JSON output and each plan.

## 5. Made here

- The notice is the CVE alert's callout: a warning, its bar at column 3, with no route, as no menu item upgrades a release.
- An ended release is a warning too, not an error: a callout's "Error:" says something failed.
- The date is the last day of support: "ended" starts the day after, and a month's from the first of the next.
- "Near" is 90 days or fewer to the date, counted in whole days.
- "Out of scope: hardn refuses every change." is drawn where the engine's gate refuses (`pkg/engine/plan.go`, `refuseApply`): any level but Supported and Best effort. Its words are the README's.
- A release with no matrix row has no date and no notice: an Ubuntu interim release, or an unlisted version.
- The status rows and the menu items under each mock are the fixture's (Debian's AppArmor row on Alpine): only the bar and the callouts are the mock's.
- `screen.Alert` has a new body line, `Why`, before `Fix`, and `screen.MainMenu` a second callout, `Notice`. With neither set, every render is as it was: no other golden changed.

## 6. For the real unit

- `eolEnds` and `eolNotice` in `internal/tools/uigallery/eol.go` are the stand-ins: the dates belong in `detect.Release`, and the rule beside `output.AlertOf`. `TestEOLEnds` holds the stand-in to the matrix's rows.
- The matrix's notes ("vendor support ended 2026-11-01") repeat a date the row would then carry.
- Alpine's page says "End of support" and a date. Whether that day is the last supported day or the first unsupported one was not established; the mock treats it as the last.
- The clock is the host's: the unit needs it passed in, and tests with a fixed day.
- The engine's refusal for an Out of scope release (`ErrUnsupported`) has its own words; the notice does not replace it.
- Nothing ran on Linux.
