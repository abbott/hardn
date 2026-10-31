# The parity table of System Details and Logs: origin/main's two read-only screens and `-p` against the readers (`TestParity`)

> **Unit:** refactor plan slice 1 (System Details + Logs), unit (b2), `next/s1-parity`, from `next/s1-readers` at 41ef415e: "Parity table: end state … of legacy vs new path, per fixture distro, with each difference tied to a V-number or an approved proposal" (plan §2, "Phases 3-5 together", §3.22); [gallery-system-details.md](gallery-system-details.md) §9: "origin/main's goldens (`menu/system_details_*`, `menu/logs_*`) against the new forms by row, each difference tied to a decision or defect of this page". Sources the table ties to: decisions 2, 3, 5, 6, 7, 8 and 10 of gallery-system-details.md §5 and its Made here calls, its §6 words, its §7 defects and the slice's two listed there (adopted 2026-10-30); [readers-system-details.md](readers-system-details.md) §9's words (adopted 2026-10-30); [P-18](../refactor/proposals.md#p-18), decided; a `KNOWN-DEFECT` line that cell's golden holds. No V-number: no golden names one for what is compared (§2).
> **Status:** built and tested on macOS, in process: origin/main from `pkg/characterization`'s goldens, read in place; the new path on the recording fake (`hostfake`). Nothing ran on Linux or as root here: a claim about a host is **written, not run (gate 5)**, or cites `_support/hardn-audit-2026-10-16/vm-runs/s1/` (§9). DNS's, Backup's and Environment's `TestParity`, their `parity.golden` and the regions of their pages are byte for byte as at 41ef415e, `-update` included (run). **No difference is UNEXPLAINED** (§6). Exit statuses and `-p`'s stderr are unit (d)'s: no aspect here compares them.
> *Since `next/s1-screen` (unit (c); [screen-system-details.md](screen-system-details.md)):* **`TestParity` drives the screens.** Each cell's new side runs the screen's `Run` on hostfake, typed to leave at once or as the row needs, or `logs.Print` for `-p`, and a row is what it drew, beside the fact where origin/main's form holds figures the words round (§2, §7). The table gained what stands around the rows and three rows: 32 rows, 34 cells, 412 aspects agree and 586 differ. Every difference has a source; none is pending. Before this, a row was the reader's fact (git: `next/s1-parity`).

## 1. Why

Unit (d) replaces origin/main's two screens, `hardn system-details` and `-p`, and deletes `pkg/menu/system_details_menu.go`, `pkg/menu/logs_menu.go`, `pkg/system` and the log's layers. This page and its test are the record of what they showed and wrote, kept after their code is gone: every row and every state, against the fact that replaces it, with the source that explains the difference. §2 names the lines it knows and does not compare.

## 2. Method

- **origin/main:** the 33 goldens `menu/system_details_*` (22) and `menu/logs_*` (11), read in place by a reader of their layouts (`readGolden`, `legacy_read_test.go`, through `Slice.Reader`). It sits beside the test because every line it knows is these two screens'. Any section, call or line it does not know fails the test, so that no golden reads as a run that did nothing:
  - **`readReport`** (`TestLegacyMenuSystemDetails`): each row of the first draw, by its label, and a later draw must print the same rows; the lost report, the error printed twice; an export's message; "What the host-info service asked of the mocks" and "The tools the screen ran itself", which must only read, each held to a list, and are recorded with their counts, not compared; the working directory before and afterwards, whose change is what the run wrote. The two renderer goldens have no trailer.
  - **`readLog`** (`TestLegacyMenuLogs`): the file read, what was shown or said, "The repository's calls" (`stat` and `read` alone, on `logFile`, the section once on the screen and not at all under `-p`), and what hardn's log holds afterwards, less the lines it held and the start-up's line. A golden without its `$PAGER` line, which says the pager did not run, fails.
  - **`readParser`** (`TestLegacyLogsParser`): for each of its two blocks the lines written and `GetLogs`' entries, each entry held to its line's split and an unknown field failing.
- **Lines the reader reads and compares since unit (c).** Each is drawn, not a fact of the report, and is its own aspect:
  - `title`: "System Details", "System Details Error", "View Logs", which decision 11 makes "Logs";
  - `sections`: on Logs the `~~~` line, "Log Contents:" and its rule, "# Contents of"; on System Details none, against P-17's seven;
  - `file row`: "Log file:", held to name `logFile`, against "Path";
  - `pause`: "Press Enter to return to the main menu", "Press Enter to continue...", against a sub-menu's menu (Made here, P-28);
  - `items`, `menu heading`, `prompt`: item 2 not offered after an error among them; on Logs none, against the menu;
  - `failure words`: "Failed to retrieve system status information." and "Please check system permissions and try again.", defect 7's, against "Can't read <source>";
  - `export prompt`, "Enter filename": no item, decision 5 (a); `key refused`, "Invalid option. Please try again.", nav's the same words;
  - `draws`, and `reads a draw`, from the mocks' counts: defect 20.
- **A line the reader knows and does not compare:** the blank lines around `-p`'s log, unit (d)'s.
- **Masked rows.** The screen goldens mask what has no seam, as their "Masked" line says: the OS, Hostname, User and Load lines, the addresses, and Domain dropped. A masked row is **recorded on both sides and not compared** (`Row.Masked`), and the reader fails if a row that line names is not masked, if one it does not name is, or if one is where the line is absent. The two renderer goldens hold those rows on synthetic values, and are where they are compared (`sd-render`, `sd-render-zfs`).
- **The new path** (`TestParity`, `pkg/screens/systemdetails/parity_test.go`, `drive_test.go`, through `internal/parity`), on hostfake:
  - **System Details:** the screen's `Run` on `detect.Detect`'s facts through `details.Bounded`, as readers-system-details.md item 23 has `hardn system-details`' path, typed the cell's keys, plain at 80×24. `readDrawn` reads each line of the screen, failing on a line or a fixed row it does not know; a row of the table is that line, under the label of the row it replaces (readers-system-details.md §7's map), `none` where nothing replaces it. Every draw must be the first. `Print` off a terminal, on a twin host, must draw the same rows, each value whole (`holdWhole`).
  - **Logs:** the screen's `Run` with the cell's `logFile`, at 80×552, so that every line `logview.Read` keeps is compared, and `holdFitted` holds the same screen at 80×24, its header and lines the end of those. `logs.Print` for `-p` off a terminal. In every cell `Each`'s last lines are held equal to those shown. Where the screen offers Open, a twin host takes the key, and the pager's row is the command Open ran, nothing else run or written.
  - **The parser:** `logview.Read` on the golden's written lines, each `Line.Text` against the line written and each `Line.Level` against what a logger's line looks like, and on its long line.
- **Values.** Where origin/main's form holds figures the words round (`Cores`, `Volume`, `Memory`) the aspect stays the fact in that form, read on a twin host, so that the same numbers agree, and a wrong reading fails as UNEXPLAINED; the row drawn is its own aspect (`CPUs`, `Disk Used`, `Memory Used`), held to draw that fact by a sum of the test's own (`holdDrawn`). `Volume`'s unit is a row of its own, `Volume unit`, as GB against GiB is defect 13 and the figures on a plain root are the same. On a ZFS root `Volume` differs by defects 8 to 10, so a wrong figure there is limit 1's. Every other row is the screen's words: "Debian 13 (Trixie)", "No sudo", "None recorded", "Not known: lslogins isn't installed", "No log yet", "Can't read it: permission denied", "All 5 lines". `Uptime` agrees, drawn.
- **The same start.** `holdStart` holds the host's `/etc/os-release`, `/etc/passwd`, `/etc/group`, `/proc/uptime` and `/proc/meminfo` to the sizes the golden's trailer records (their bytes are `runSystemDetailsChild`'s). On Logs, the `size` row agrees with the golden's `read` where the file is the same one, and the lines agree. **Where a fact cannot be the same:**
  - `/proc/cpuinfo`: the golden's holds one `processor` line (75 bytes), its `nproc` and `lscpu` stand-ins say 2 and 1, and origin/main's row takes those. The new path reads this file alone, so the host's says 2 and 1.
  - What origin/main has no file for, and the new path reads: `/proc/sys/kernel/hostname`, `/proc/loadavg`, `/proc/self/mounts`, `/proc/self/status`, `/etc/sudoers` (with `%sudo`, so that the golden's group means what it says, and without it in `sd-group-no-rule`) or `/etc/doas.conf`, `/var/log/wtmp`, and the forms of `ip`, `df -kP /` (the figures of the golden's `df -k`), `lslogins`, `zpool list` and `zfs get` (the figures of the `zfs_dataset` stand-in).
  - `sd-zfs-*`: the golden's `zpool` stand-in answers words to reach origin/main's branch; the new path's says ONLINE. Its form is [unverified], no ZFS VM.
  - `logs-unreadable`: mode 0000 as a user in the golden; the new path's read is refused the same way (`refusingScan`). hardn as root meets it only as the golden's NOTE says [I].
- **What is written: nothing.** In every cell `parity.NewState` reads hostfake's record, and a write, an append or `run-interactive` fails the test ("a change the harness does not compare"); `recordOf` holds every call to the reads and says which, in the cell's "new path:" line; a command not in `readCommands` differs in `commands` (mutation 24). The commands the readers and `detect` run are recorded as probes, not compared. **One read a draw** (`holdOneRead`): each draw reads `/proc/meminfo` or the log once, and runs `ip`, `df`, `lslogins` and `less --version` at most once.
- **The harness's end state fits as it is:** an export's file is `files` (path and mode), its directory `directories`, each against none. What was missing is rows: `EndState.Rows` compares each as an aspect of its name, a row one side lacks being `none`. `refusal`, `commands`, `findings` and `hardn.yml` agree in every cell. Backups, the journal, checks and temp files are empty on both sides, and not aspects.
- **One slice, one package, one page.** `pkg/screens/systemdetails` holds both screens' rows: the plan has one slice, its page and golden are one, and the harness's region is one per page. A directory holding only tests, as `pkg/screens/environment` was. `details` would collide with `pkg/engine/details` on import; `systemdetails` does not, and it is the package gallery-system-details.md §8 names for unit (c).
- **Hosts: 34 cells of 32 rows.** The legacy goldens are host-independent: their host is stand-ins and mocks. The new path differs by host in the elevation tool, the login tool and `less`, which the second cells show (`Scenario.Only`), and in the OS row, masked in every screen run, so that no cell shows it:
  - **Debian 13 (sudo, `lslogins`, GNU `less`):** every row, its start forced. 31 of the 33 goldens are rows, `system_details_open` twice (`sd-open`, `sd-group-no-rule`).
  - **Alpine 3.23:** `sd-open` (doas; no `lslogins`, so `LoginsNoTool`; `ip`'s `noprefixroute` form, whose address reads the same) and `logs-open` (BusyBox's `less`, so no pager). Run: `vm-runs/s1/01-tools.log`, for those forms.
  - **Not cells:** Ubuntu 26.04 reads as Debian 13 here: sudo-rs is `visudo -c` and "sudo" to the report, and `lslogins` answers (run: `vm-runs/s1/15-readers-ubuntu26-vm-root.log`); GNU `less` 668 (run: `vm-runs/s1/01-tools.log`:88). Proxmox VE 9's differences are the OS row, which names Proxmox VE (read: `header.OSOf`), and is masked in every screen run; a root-only `Users`, which no golden's start holds; and a ZFS root, which `sd-zfs-other` runs on Debian 13 with `rpool/ROOT/pve-1` [I for each; no VM].
- **Rows:** 21 of System Details (`sd-*`) and 11 of Logs (`logs-*`). **Not rows** (`TestParityGoldens` holds each of the 33 a row or a not-row, and each named there):
  - `menu/system_details_flags`: the five flags, in a layout of its own: decision 1 (a), and unit (d) builds `--output json` and the warnings. Nothing here has the flags.
  - `menu/system_details_error_export`: `sd-error`'s start and a key not offered, nothing written; its words go with Export (decision 5 (a)). Its reads are `sd-error`'s twice over (held: `notRowDraws`).
  - *Since `next/s1-screen`,* `menu/system_details_invalid`, `_return` and `_refresh` are rows (`sd-invalid`, `sd-return`, `sd-refresh`), with defect 20's recursion gone: an invalid key is refused under the prompt, no draw; Refresh draws again from one read.
- **Commands and reads that only read** are recorded, not compared. Defect 20 shows in each screen run's probes, and since `next/s1-screen` in `reads a draw`: 4 for each draw, 3 in `sd-error`; the two renderer rows have no calls. Against that, the new path's 1 is held (`holdOneRead`): this table's probes name the new path's commands, and its reads by operation alone.
- **The table is a test:** `table()` (`parity_test.go`) holds every difference, keyed by scenario, cell and aspect, with its source and one line. `TestParity` fails on a difference not in it (UNEXPLAINED), on an entry whose difference no longer occurs, on an entry listed twice, and on a V-number the cell's golden does not name. `logs_print_flag_missing` alone names one, V25's class, for the exit status, which unit (d) compares. `TestTiesCiteTheirGoldens` fails where an entry cites a `KNOWN-DEFECT` line by a phrase that cell's golden does not hold. An entry with no source would be `Tie.Pending`, carried and marked UNEXPLAINED (§6); there is none.
- **Mutation-checked** (run at this unit's tip, each on a copy outside the repository):
  1. `Kernel`'s entry removed: UNEXPLAINED;
  2. an entry for `Processor`, which agrees: stale;
  3. `Memory` tied to V20: "does not name";
  4. `Kernel` listed twice: "twice";
  5. `Memory`'s entries made pending: only `parity.golden` and the page fail (limit 1);
  6. a row the reader does not know in `system_details_open`: the reader fails;
  7. a `write` among the mocks' calls: the reader fails;
  8. `useradd bob` among the tools: "only read";
  9. a masked row unmasked: "the OS row";
  10. a later draw whose rows differ: "differ from the first's";
  11. a `write` among the repository's calls: the reader fails;
  12. the `$PAGER` line saying it ran: the reader fails;
  13. `logs_empty` with neither its heading nor a failure: "neither";
  14. an export's file gone from "afterwards": the reader fails;
  15. a not-row left out: `TestParityGoldens` fails;
  16. a golden added, neither: `TestParityGoldens` fails;
  17. a `KNOWN-DEFECT` phrase no golden holds: `TestTiesCiteTheirGoldens` fails;
  18. `/proc/meminfo` another size: "not what the golden reads";
  19. `Memory.Used` wrong: UNEXPLAINED, `Memory`, in the render rows, where it agrees;
  20. `Safe` not applied: `raw escape` stale;
  21. `logview.Read` appending to the log: "a change the harness does not compare";
  22. `details.Read` writing a file: "written, then gone";
  23. the pager run: "a change the harness does not compare";
  24. `details.Read` running `useradd`: UNEXPLAINED, `commands`;
  25. `less --version` not GNU's on Debian 13: only `parity.golden` and the page fail (limit 1);
  26. a heading row on the new path's side: UNEXPLAINED, `-p heading`;
  27. `details.read` keeping only memory where memory is unread: "not kept";
  28. `ParseLine` returning `Entry{}` for a line not the logger's: "loses part";
  29. `ParseLine` returning `Entry{Level: m[2]}`: "loses part";
  30. `s.Used` doubled in `df`'s reader: UNEXPLAINED, `Volume`, on a plain root;
  31. `sd-group-no-rule`'s start left with `%sudo`: its `Non-System Users` entry stale;
  32. `useradd bob` among the mocks' commands: "only read";
  33. `Processor` masked: "the Processor row masked";
  34. a second `Processor` row on the new path's side: "twice";
  35. an `Only` naming no distribution: "no distribution of this slice";
  36. a `logs_parser` entry changed: "split"; given a new field: "unknown field";
  37. a `read` of another file: "another file"; `logs_missing` with no calls, and `logs_open` with them twice: "sections of the repository's calls";
  38. a later draw whose Load 15m bar differs: "differ from the first's";
  39. `system_details_error_export` with a file added: "it wrote"; `_refresh` with `useradd bob`: "only read"; `_return` with another count: "asks";
  40. a `Volume` line of another form: "Volume line";
  41. `details.Read` taking a lock: "a call that does not only read".

  *Since `next/s1-screen`,* each on the screens, with `go test -overlay` (run): mutations 1, 2, 4, 19 to 24, 30 and 41 again, each fails `TestParity`; 28 and 29 now as `ParseLine` answering none and the date, "the level"; and new:

  42. `items`' entry removed, Logs' `pause` entry removed: UNEXPLAINED;
  43. a not-row listed again: `TestParityGoldens`, "one or the other";
  44. a draw writing, on either screen: "written, then gone";
  45. two `details.Read` a draw, two `logview.Read` a draw: "read 2 times in 1 draws";
  46. Open at each draw: "less ran 2 times";
  47. Used and CPUs drawn wrong: "does not draw";
  48. the Model row dropped: UNEXPLAINED, `Processor`;
  49. a row in a fixed section no reader knows: "a row this reader does not know";
  50. the first lines fitted, not the last: "not the end of";
  51. an Export item back: `parity.golden` and the page (limit 1), and `TestScreenHosts`, `TestNavigation`;
  52. `Print` cutting off a terminal: "Print says"; a section left out of `Print`: "Print off a terminal";
  53. a line with no level emptied: "loses part"; a parser row made origin/main's: "no longer differs".

  `TestDrawnReaderRefuses` holds the reader of the screens as `TestReaderRefuses` holds origin/main's: nothing drawn, a line before the title, a line or prompt or pause it does not know, an escape in plain mode, a row before a section; the latter gained an item it does not know, a renderer with a menu, no read of `/proc/meminfo`, reads a draw does not divide, a title on `-p`, none on the screen.
- **Its limits:** (1) a difference already tied whose values change fails only on `parity.golden` and the page, which `make golden-update` accepts: the review of that diff is the guard; (2) a `-run` filter checks only the cells it runs, and the table, the golden and the page only when every cell ran; (3) origin/main's rows are its run's in a child process, with stand-ins on `PATH` and mocks, as the test's user, on macOS, and its masked rows hold nothing but the render rows' synthetic values; (4) the new path's forms are the readers' tests', typed from the VMs' logs, ZFS's [unverified]; (5) *since `next/s1-screen`, each row is drawn, in plain mode at 80 columns: colour, Unicode glyphs and 60 columns are the screens' own tests' and the gallery's*; (6) `logs_large`'s golden keeps 6 of its 20,000 lines, so `lines` compares its last three, with the count and the size.
- **Its renderings:** [`parity.golden`](../../pkg/screens/systemdetails/testdata/golden/parity.golden), every cell with what agrees and what differs; and the region of §4. `make golden-update` rewrites both; without `-update` the test fails when either is not what it makes.

## 3. Host states

Every cell's host is forced: the golden's synthetic start with what only the new path reads (§2), so §4 marks them "forced". `details.Read` on the real hosts, as the user and as root, is `vm-runs/s1/15-readers-*.log` (run, read only; compared by hand with `16-truth-*.log`).

| Scenario | origin/main's golden | Start | The new path |
|---|---|---|---|
| `sd-open` | `system_details_open` | the golden's, `SSH_CLIENT` set | `details.Read` |
| `sd-invalid`, `sd-return`, `sd-refresh` | `_invalid`, `_return`, `_refresh` | 7, then q; 0; 1, then q | the screen, typed the same but the pause's Enter |
| `sd-group-no-rule` | `system_details_open` | the golden's, with no rule naming the sudo group | `details.Read` |
| `sd-render`, `sd-render-zfs` | `system_details_render`, `_render_zfs` | the renderer's synthetic report, in files | `details.Read`; each row compared |
| `sd-error` | `system_details_error` | `/proc/meminfo` unreadable | `details.Read`; `report` alone compared, naming the rows kept, each held equal to the same host's with memory read (`holdKept`) |
| `sd-no-lastlog`, `sd-never`, `sd-login-hostname` | `_no_lastlog`, `_never_logged_in`, `_login_hostname` | `lslogins` as usual, a blank for alice, or a host with no dot | `details.Read` |
| `sd-no-lscpu` | `_no_lscpu` | as `sd-open`: the new path asks no `lscpu` | `details.Read` |
| `sd-zfs-all-healthy`, `sd-zfs-dataset`, `sd-zfs-other` | `_zfs_all_healthy`, `_zfs_dataset`, `_zfs_other_dataset` | a ZFS root | `details.Read` |
| `sd-export`, `-named`, `-overwrite`, `-fails`, `-dry-run`, `-zfs` | `_export_*` | item 2 on origin/main; on the new path no item (decision 5 (a)) | `details.Read`; nothing written |
| `logs-open`, `-large`, `-empty`, `-missing`, `-unreadable`, `-escape` | `logs_open`, `_large`, `_empty`, `_missing`, `_unreadable`, `_escape` | the golden's log | `logview.Read`, `Pager` where there are lines |
| `logs-other-path`, `-stale` | `logs_other_path`, `_stale` | `logFile` `/srv/log/custom.log`; `/var/log/hardn.log` holds the start-up's line | `logview.Read`, `Elsewhere`, `Pager` |
| `logs-p`, `logs-p-missing` | `logs_print_flag`, `_missing` | as the screen's | `logview.Each`, `Elsewhere` |
| `logs-parser` | `logs_parser` | the lines hardn writes | `logview.Read`, its lines and each line's level |

*Since `next/s1-screen`,* the new path of each is the screen's `Run` or `logs.Print` over the reader named (§2): each cell's "new path:" line in `parity.golden` says what was typed.

## 4. The table

<!-- TestParity: begin. Generated by TestParity (pkg/screens/systemdetails); make golden-update rewrites it. Do not edit. -->

**Host states** (§3): what the new path ran on, and whether the distribution's stock install has it (real), the harness forced it, or it cannot exist.

| Scenarios | Debian 13 | Alpine 3.23 |
|---|---|---|
| sd-open, logs-open | the golden's start (forced) | the golden's start (forced) |
| sd-invalid | a key no item has (forced) | not run |
| sd-return | 0, back (forced) | not run |
| sd-refresh | 1, Refresh (forced) | not run |
| sd-group-no-rule | alice in the sudo group, no rule naming it (forced) | not run |
| sd-render | the renderer's synthetic report (forced) | not run |
| sd-render-zfs | the renderer's synthetic report, a ZFS root (forced) | not run |
| sd-error | /proc/meminfo unreadable (forced) | not run |
| sd-no-lastlog | no lastlog, lslogins as on stock Debian 13 (forced) | not run |
| sd-never | alice has no login (forced) | not run |
| sd-login-hostname | alice's login from a name with no dot (forced) | not run |
| sd-no-lscpu | no lscpu (forced) | not run |
| sd-zfs-all-healthy | a ZFS root, zpool healthy (forced) | not run |
| sd-zfs-dataset | a ZFS root, the default dataset (forced) | not run |
| sd-zfs-other | a ZFS root, another dataset (forced) | not run |
| sd-export | item 2, the default name (forced) | not run |
| sd-export-named | item 2, a name in a new directory (forced) | not run |
| sd-export-overwrite | item 2, a file of that name there (forced) | not run |
| sd-export-fails | item 2, a name whose directory is a file (forced) | not run |
| sd-export-dry-run | item 2, in dry run (forced) | not run |
| sd-export-zfs | item 2, a ZFS root (forced) | not run |
| logs-large | a log of 20000 lines (forced) | not run |
| logs-empty | an empty log (forced) | not run |
| logs-missing | no log (forced) | not run |
| logs-unreadable | the read refused (forced) | not run |
| logs-escape | a line holding an escape (forced) | not run |
| logs-other-path | logFile elsewhere, nothing there (forced) | not run |
| logs-other-path-stale | logFile elsewhere, an old log there (forced) | not run |
| logs-p | -p, off a terminal (forced) | not run |
| logs-p-missing | -p, logFile elsewhere, nothing there (forced) | not run |
| logs-parser | the lines hardn writes (forced) | not run |

**Agreeing and differing aspects** (§4): in how many compared cells each aspect agrees and differs.

| Aspect | Agrees in | Differs in |
|---|---|---|
| refusal | 34 | 0 |
| files | 30 | 4 |
| directories | 33 | 1 |
| commands | 34 | 0 |
| findings | 34 | 0 |
| hardn.yml | 34 | 0 |
| report | 20 | 2 |
| Non-System Users | 17 | 4 |
| Processor | 19 | 2 |
| Cores | 21 | 0 |
| Volume | 16 | 5 |
| Uptime | 21 | 0 |
| title | 21 | 10 |
| menu heading | 20 | 9 |
| draws | 13 | 7 |
| Kernel | 0 | 21 |
| Client IP | 0 | 21 |
| DNS IP 1 | 0 | 21 |
| DNS IP 2 | 0 | 21 |
| Hypervisor | 0 | 21 |
| CPU Freq | 0 | 21 |
| Volume unit | 0 | 21 |
| Disk Usage | 0 | 21 |
| Memory | 2 | 19 |
| Usage | 0 | 21 |
| Last Login | 0 | 21 |
| From | 0 | 20 |
| items | 0 | 29 |
| prompt | 1 | 28 |
| reads a draw | 0 | 20 |
| CPUs | 0 | 21 |
| / | 0 | 21 |
| Disk Used | 0 | 21 |
| Memory Used | 0 | 21 |
| sections | 0 | 31 |
| key refused | 1 | 6 |
| pause | 0 | 16 |
| Hostname | 2 | 0 |
| Load 1m | 2 | 0 |
| Load 5m | 2 | 0 |
| Load 15m | 2 | 0 |
| OS | 0 | 2 |
| Domain | 0 | 2 |
| IP Addresses | 0 | 2 |
| User | 0 | 2 |
| Load bars | 0 | 2 |
| ZFS Health | 0 | 5 |
| failure words | 0 | 1 |
| export | 0 | 6 |
| export prompt | 0 | 6 |
| file read | 8 | 3 |
| state | 6 | 5 |
| lines | 4 | 4 |
| raw escape | 7 | 1 |
| size | 4 | 3 |
| lines shown | 1 | 7 |
| pager | 0 | 11 |
| file row | 0 | 9 |
| log written | 3 | 1 |
| logFile elsewhere | 0 | 3 |
| -p heading | 0 | 1 |
| parser lines kept | 0 | 1 |
| parser levels | 0 | 1 |
| parser long line | 0 | 1 |

**Differences** (§4): each with its source; every cell, both end states whole, is in [parity.golden](../../pkg/screens/systemdetails/testdata/golden/parity.golden).

| Scenarios | On | Aspect | origin/main | New path | Source |
|---|---|---|---|---|---|
| sd-open, sd-invalid, sd-return, sd-refresh, sd-group-no-rule, sd-render, sd-render-zfs, sd-no-lastlog, sd-never, sd-login-hostname, sd-no-lscpu, sd-zfs-all-healthy, sd-zfs-dataset, sd-zfs-other, sd-export, sd-export-named, sd-export-overwrite, sd-export-fails, sd-export-dry-run, sd-export-zfs | every cell of each | Kernel | `Linux 6.12.0-example-amd64` | `6.12.0-example-amd64` | decision 2 (a): Linux and the release; now the release, as screen-debian13's row |
| sd-open, sd-group-no-rule, sd-render, sd-render-zfs | every cell of each | Client IP | `192.0.2.10` | `none` | decision 2 (a), left out: SSH_CLIENT's address; no row |
| sd-open, sd-invalid, sd-return, sd-refresh, sd-group-no-rule, sd-render, sd-render-zfs, sd-no-lastlog, sd-never, sd-login-hostname, sd-no-lscpu, sd-zfs-all-healthy, sd-zfs-dataset, sd-zfs-other, sd-export, sd-export-named, sd-export-overwrite, sd-export-fails, sd-export-dry-run, sd-export-zfs | every cell of each | DNS IP 1 | `192.0.2.53` | `none` | decision 2 (a), left out: DNS has main menu 3's screen; no row |
| sd-open, sd-invalid, sd-return, sd-refresh, sd-group-no-rule, sd-render, sd-render-zfs, sd-no-lastlog, sd-never, sd-login-hostname, sd-no-lscpu, sd-zfs-all-healthy, sd-zfs-dataset, sd-zfs-other, sd-export, sd-export-named, sd-export-overwrite, sd-export-fails, sd-export-dry-run, sd-export-zfs | every cell of each | DNS IP 2 | `192.0.2.54` | `none` | decision 2 (a), left out: DNS has main menu 3's screen; no row |
| sd-open, sd-invalid, sd-return, sd-refresh, sd-group-no-rule, sd-no-lastlog, sd-never, sd-login-hostname, sd-zfs-all-healthy, sd-zfs-dataset, sd-zfs-other, sd-export, sd-export-named, sd-export-overwrite, sd-export-fails, sd-export-dry-run, sd-export-zfs | every cell of each | Hypervisor | `KVM` | `none` | decision 2 (a), left out: lscpu's vendor, or the report's; no row |
| sd-open, sd-invalid, sd-return, sd-refresh, sd-group-no-rule, sd-no-lastlog, sd-never, sd-login-hostname, sd-no-lscpu, sd-zfs-all-healthy, sd-zfs-dataset, sd-zfs-other, sd-export, sd-export-named, sd-export-overwrite, sd-export-fails, sd-export-dry-run, sd-export-zfs | every cell of each | CPU Freq | `0.00 GHz` | `none` | decision 2 (a), left out; defect 11: 0.00 GHz on the usual host; no row |
| sd-open, sd-invalid, sd-return, sd-refresh, sd-group-no-rule, sd-render, sd-render-zfs, sd-no-lastlog, sd-never, sd-login-hostname, sd-no-lscpu, sd-zfs-all-healthy, sd-zfs-dataset, sd-zfs-other, sd-export, sd-export-named, sd-export-overwrite, sd-export-fails, sd-export-dry-run, sd-export-zfs | every cell of each | Volume unit | `GB` | `GiB` | defect 13: bytes / 1024^3 said as GB; now GiB |
| sd-open, sd-invalid, sd-return, sd-refresh, sd-group-no-rule, sd-no-lastlog, sd-never, sd-login-hostname, sd-no-lscpu, sd-zfs-all-healthy, sd-export, sd-export-named, sd-export-overwrite, sd-export-fails, sd-export-dry-run | every cell of each | Disk Usage | `███░░░░░░░░░░░░░░░░░` | `none` | decision 2 (a), left out: a 20-wide bar; no row |
| sd-open, sd-invalid, sd-return, sd-refresh, sd-group-no-rule, sd-no-lastlog, sd-never, sd-login-hostname, sd-no-lscpu, sd-zfs-all-healthy, sd-zfs-dataset, sd-zfs-other, sd-export, sd-export-named, sd-export-overwrite, sd-export-fails, sd-export-dry-run, sd-export-zfs | every cell of each | Memory | `3.89/7.78 GiB [50.00%]` | `1.94/7.78 GiB [25.00%]` | defect 12: MemFree alone counted as free, so page cache as used; now by MemAvailable |
| sd-open, sd-invalid, sd-return, sd-refresh, sd-group-no-rule, sd-render, sd-render-zfs, sd-no-lastlog, sd-never, sd-login-hostname, sd-no-lscpu, sd-zfs-all-healthy, sd-zfs-dataset, sd-zfs-other, sd-export, sd-export-named, sd-export-overwrite, sd-export-fails, sd-export-dry-run, sd-export-zfs | every cell of each | Usage | `██████████░░░░░░░░░░` | `none` | decision 2 (a), left out: a 20-wide bar; no row |
| sd-open, sd-invalid, sd-return, sd-refresh, sd-group-no-rule, sd-no-lscpu, sd-zfs-all-healthy, sd-zfs-dataset, sd-zfs-other, sd-export, sd-export-named, sd-export-overwrite, sd-export-fails, sd-export-dry-run, sd-export-zfs | Debian 13 | Last Login | `Oct 29 08:12:44 +0000` | `root None recorded, alice Oct 29 08:12 from 192.0.2.10` | decision 3 (a): whoever ran it, by lastlog; now root and each listed user, by lslogins |
| sd-open, sd-invalid, sd-return, sd-refresh, sd-group-no-rule, sd-render, sd-render-zfs, sd-no-lscpu, sd-zfs-all-healthy, sd-zfs-dataset, sd-zfs-other, sd-export, sd-export-named, sd-export-overwrite, sd-export-fails, sd-export-dry-run, sd-export-zfs | Debian 13 | From | `192.0.2.10` | `alice 192.0.2.10` | decision 3 (a): one address; now each account's |
| sd-open, sd-invalid, sd-return, sd-refresh, sd-group-no-rule, sd-no-lastlog, sd-never, sd-login-hostname, sd-no-lscpu, sd-zfs-all-healthy, sd-zfs-dataset, sd-zfs-other, sd-export, sd-export-named, sd-export-overwrite, sd-export-fails, sd-export-dry-run, sd-export-zfs | every cell of each | items | `1) Refresh Information: Reload system status from system; 2) Export Information: Export system status to file; 0) Return` | `1) Refresh: Read the host again; 0) Return` | decision 5 (a); gallery-system-details.md §6, Items: Refresh Information and Export Information; now Refresh alone, in fewer words, no Export item |
| sd-open, sd-invalid, sd-return, sd-refresh, sd-group-no-rule, sd-no-lastlog, sd-never, sd-login-hostname, sd-no-lscpu, sd-zfs-all-healthy, sd-zfs-dataset, sd-zfs-other, sd-export, sd-export-named, sd-export-overwrite, sd-export-fails, sd-export-dry-run, sd-export-zfs | every cell of each | prompt | `Enter selection [0-2 or q]` | `Enter selection [0-1 or q]` | decision 5 (a): 0-2, with Export; now 0-1, no Export item |
| sd-open, sd-invalid, sd-return, sd-refresh, sd-group-no-rule, sd-no-lastlog, sd-never, sd-login-hostname, sd-no-lscpu, sd-zfs-all-healthy, sd-zfs-dataset, sd-zfs-other, sd-export, sd-export-named, sd-export-overwrite, sd-export-fails, sd-export-dry-run, sd-export-zfs | every cell of each | reads a draw | `4` | `1` | defect 20: each of the host-info service's files read four times a draw, three where memory fails; now one read |
| sd-open, sd-invalid, sd-return, sd-refresh, sd-group-no-rule, sd-no-lastlog, sd-never, sd-login-hostname, sd-no-lscpu, sd-zfs-all-healthy, sd-zfs-dataset, sd-zfs-other, sd-export, sd-export-named, sd-export-overwrite, sd-export-fails, sd-export-dry-run, sd-export-zfs | every cell of each | CPUs | `none` | `2, 1 socket` | decision 2 (a); gallery-system-details.md §6, CPUs: none, Cores in its own words; now the row's, Cores the fact it draws |
| sd-open, sd-invalid, sd-return, sd-refresh, sd-group-no-rule, sd-render, sd-no-lastlog, sd-never, sd-login-hostname, sd-no-lscpu, sd-export, sd-export-named, sd-export-overwrite, sd-export-fails, sd-export-dry-run | every cell of each | / | `none` | `/dev/vda1 ext4` | decision 2 (a); gallery-system-details.md §6, /: no row; now the root's source and type |
| sd-open, sd-invalid, sd-return, sd-refresh, sd-group-no-rule, sd-no-lastlog, sd-never, sd-login-hostname, sd-no-lscpu, sd-export, sd-export-named, sd-export-overwrite, sd-export-fails, sd-export-dry-run | every cell of each | Disk Used | `none` | `6.0 of 39.3 GiB (15%)` | decision 2 (a); gallery-system-details.md §6, Used: none, Volume in its own words; now the row's, Volume the fact it draws |
| sd-open, sd-invalid, sd-return, sd-refresh, sd-group-no-rule, sd-no-lastlog, sd-never, sd-login-hostname, sd-no-lscpu, sd-zfs-all-healthy, sd-zfs-dataset, sd-zfs-other, sd-export, sd-export-named, sd-export-overwrite, sd-export-fails, sd-export-dry-run, sd-export-zfs | every cell of each | Memory Used | `none` | `1.9 of 7.8 GiB (25%)` | decision 2 (a); gallery-system-details.md §6, Used: none, Memory in its own words; now the row's, Memory the fact it draws |
| sd-open, sd-invalid, sd-return, sd-refresh, sd-group-no-rule, sd-render, sd-render-zfs, sd-error, sd-no-lastlog, sd-never, sd-login-hostname, sd-no-lscpu, sd-zfs-all-healthy, sd-zfs-dataset, sd-zfs-other, sd-export, sd-export-named, sd-export-overwrite, sd-export-fails, sd-export-dry-run, sd-export-zfs | every cell of each | sections | `none` | `OS and Kernel, Host and IPs, Users, CPU and Load, Disk, Memory, Last Login` | decision 2 (a); gallery-system-details.md §6, System Details sections: no headings, blank lines alone part the report; now P-17's seven |
| sd-open | Alpine 3.23 | report | `whole` | `kept OS, Kernel, Hostname, IP Addresses, Non-System Users, Processor, Cores, Load 1m, Load 5m, Load 15m, /, Volume, Memory, Uptime; Last Login: Not known: lslogins isn't installed` | decision 3 (a); gallery-system-details.md §6, Not known: lslogins isn't installed: whole, by the golden's stand-ins; now Last Login says it: no lslogins on this host |
| sd-open | Alpine 3.23 | Non-System Users | `alice (sudo)` | `alice (doas)` | decision 2 (a); gallery-system-details.md §6, <user>: the sudo group, on a doas host; now what doas grants |
| sd-open | Alpine 3.23 | Last Login | `Oct 29 08:12:44 +0000` | `Logins Not known: lslogins isn't installed` | decision 3 (a); gallery-system-details.md §6, Not known: lslogins isn't installed: lastlog's stand-in; now the row says it: no lslogins on this host |
| sd-open | Alpine 3.23 | From | `192.0.2.10` | `none` | decision 3 (a): lastlog's stand-in; now none: no lslogins on this host |
| sd-invalid, sd-return, sd-refresh, sd-no-lastlog, sd-never, sd-login-hostname, sd-no-lscpu, sd-zfs-all-healthy, sd-zfs-dataset, sd-zfs-other, sd-export, sd-export-named, sd-export-overwrite, sd-export-fails, sd-export-dry-run, sd-export-zfs | Debian 13 | Client IP | `Not connected` | `none` | decision 2 (a), left out: SSH_CLIENT's address; no row |
| sd-invalid | Debian 13 | draws | `2` | `1` | defect 20: an invalid key reads and draws again; now refused under the prompt and asked again |
| sd-invalid | Debian 13 | pause | `Press Enter to continue...` | `none` | gallery-system-details.md §5, Made here, No pause: a pause before the draw; now the prompt asks again |
| sd-group-no-rule | Debian 13 | Non-System Users | `alice (sudo)` | `alice (No sudo)` | readers-system-details.md item 16 (adopted 2026-10-30); gallery-system-details.md §6, <user>: the sudo group alone marked alice; now the rules alone: a group no rule names grants nothing |
| sd-render, sd-render-zfs | Debian 13 | OS | `Debian 13 Trixie` | `Debian 13 (Trixie)` | decision 2 (a): the report's own string; now the row's words, as the header's bar names it |
| sd-render, sd-render-zfs | Debian 13 | Domain | `example.internal` | `none` | decision 2 (a), left out: the machine's domain; no row |
| sd-render, sd-render-zfs | Debian 13 | IP Addresses | `192.0.2.20, 198.51.100.20` | `eth0 192.0.2.20/24, eth0 198.51.100.20/24` | decision 2 (a); gallery-system-details.md §6, <interface>: IPv4 addresses alone; now each interface and address with its prefix (the report's fixture held none) |
| sd-render, sd-render-zfs | Debian 13 | User | `root` | `none` | decision 2 (a), left out: the current user; no row |
| sd-render, sd-render-zfs | Debian 13 | Non-System Users | `alice (sudo), bob` | `alice (sudo), bob (No sudo)` | decision 2 (a); gallery-system-details.md §6, <user>: a user no group marks has no word; now No sudo |
| sd-render, sd-render-zfs | Debian 13 | Processor | `Example Corporation Example Processor 9000 64-Core Processor @ 2.00GHz` | `Example Corporation Example Proces...` | gallery-system-details.md §5, Made here, A value too long for its row: whole, past a 50-wide box (defect 4); now cut on a terminal, whole off one, which Print holds |
| sd-render, sd-render-zfs | Debian 13 | Hypervisor | `Bare Metal` | `none` | decision 2 (a), left out: lscpu's vendor, or the report's; no row |
| sd-render, sd-render-zfs | Debian 13 | CPU Freq | `2.00 GHz` | `none` | decision 2 (a), left out: the report's figure; no row |
| sd-render | Debian 13 | Disk Usage | `█████░░░░░░░░░░░░░░░` | `none` | decision 2 (a), left out: a 20-wide bar; no row |
| sd-render, sd-render-zfs | Debian 13 | Last Login | `Oct 29 08:12:44 +0000` | `root None recorded, alice Oct 29 08:12 from 192.0.2.10, bob None recorded` | decision 3 (a): one login, the report's; now root and each listed user |
| sd-render, sd-render-zfs | Debian 13 | Load bars | `1m █░░░░░░░░░░░░░░░░░░░, 5m ░░░░░░░░░░░░░░░░░░░░, 15m ░░░░░░░░░░░░░░░░░░░░` | `none` | decision 2 (a), left out: 20-wide bars; the figures alone |
| sd-render, sd-render-zfs | Debian 13 | CPUs | `none` | `64, 2 sockets` | decision 2 (a); gallery-system-details.md §6, CPUs: none, Cores in its own words; now the row's, Cores the fact it draws |
| sd-render | Debian 13 | Disk Used | `none` | `20.0 of 80.0 GiB (25%)` | decision 2 (a); gallery-system-details.md §6, Used: none, Volume in its own words; now the row's, Volume the fact it draws |
| sd-render, sd-render-zfs | Debian 13 | Memory Used | `none` | `31.2 of 62.5 GiB (50%)` | decision 2 (a); gallery-system-details.md §6, Used: none, Memory in its own words; now the row's, Memory the fact it draws |
| sd-render-zfs, sd-zfs-dataset, sd-export-zfs | Debian 13 | Volume | `12.00/88.00 [12.00%]` | `12.00/100.00 [12.00%]` | defect 10: used/available; now used of total |
| sd-render-zfs, sd-zfs-dataset, sd-export-zfs | Debian 13 | Disk Usage | `██░░░░░░░░░░░░░░░░░░` | `none` | decision 2 (a), left out: a 20-wide bar; no row |
| sd-render-zfs, sd-zfs-dataset, sd-export-zfs | Debian 13 | ZFS Health | `HEALTH O.K.` | `zroot ONLINE` | decision 2 (a); gallery-system-details.md §6, Pool: words of its own; now the pool's state as zpool says it |
| sd-render-zfs, sd-zfs-all-healthy, sd-zfs-dataset, sd-export-zfs | Debian 13 | / | `none` | `zroot/ROOT/os zfs` | decision 2 (a); gallery-system-details.md §6, /: no row; now the root's source and type |
| sd-render-zfs, sd-zfs-all-healthy, sd-zfs-dataset, sd-zfs-other, sd-export-zfs | Debian 13 | Disk Used | `none` | `12.0 of 100.0 GiB (12%)` | decision 2 (a); gallery-system-details.md §6, Used: none, Volume in its own words; now the row's, Volume the fact it draws |
| sd-error | Debian 13 | report | `lost: failed to collect memory info: failed to get memory info: exec: "free": executable file not found in $PATH` | `kept OS, Kernel, Hostname, IP Addresses, Non-System Users, Processor, Cores, Load 1m, Load 5m, Load 15m, /, Volume, Last Login, From, Uptime; Memory: Can't read /proc/meminfo` | defect 7; KNOWN-DEFECT (One collector's error loses the report): the first failed collector loses the report; now the section says what it could not read, and the rest is kept |
| sd-error | Debian 13 | title | `System Details Error` | `System Details` | defect 7: a title of its own for the failure; now the report's |
| sd-error | Debian 13 | items | `1) Refresh Information: Reload system status from system; 0) Return` | `1) Refresh: Read the host again; 0) Return` | gallery-system-details.md §6, Items: Refresh Information, in its words; now Refresh, as on every draw |
| sd-error | Debian 13 | failure words | `Failed to retrieve system status information.; Please check system permissions and try again.` | `Can't read /proc/meminfo` | defect 7; gallery-system-details.md §6, Can't read <source>: "check permissions" whatever it was; now what it could not read |
| sd-error | Debian 13 | reads a draw | `3` | `1` | defect 20: each of the host-info service's files read four times a draw, three where memory fails; now one read |
| sd-no-lastlog | Debian 13 | Last Login | `blank` | `root None recorded, alice Oct 29 08:12 from 192.0.2.10` | decision 3 (a); defect 14; KNOWN-DEFECT (left blank): blank; now each login by lslogins |
| sd-no-lastlog | Debian 13 | From | `none` | `alice 192.0.2.10` | decision 3 (a); defect 14; KNOWN-DEFECT (left blank): no row with the blank; now each account's, by lslogins |
| sd-never | Debian 13 | Last Login | `Never logged in` | `root None recorded, alice None recorded` | decision 3 (a); gallery-system-details.md §6, None recorded: Never logged in, for whoever ran it; now each account's row, a blank guarded |
| sd-login-hostname | Debian 13 | Last Login | `blank` | `root None recorded, alice Oct 29 08:12 from laptop` | decision 3 (a); defect 14; KNOWN-DEFECT (left blank): blank; now each login by lslogins |
| sd-login-hostname | Debian 13 | From | `none` | `alice laptop` | decision 3 (a); defect 14; KNOWN-DEFECT (left blank): no row with the blank; now each account's, a name with no dot read |
| sd-no-lscpu | Debian 13 | Hypervisor | `Bare Metal` | `none` | decision 2 (a), left out; defect 15; KNOWN-DEFECT (Bare Metal): not known read as Bare Metal; no row |
| sd-zfs-all-healthy | Debian 13 | Volume | `6.00/39.34 [15.25%]` | `12.00/100.00 [12.00%]` | defect 9; KNOWN-DEFECT (a healthy ZFS host reads as no ZFS): df's figures, ZFS not seen; now the dataset's |
| sd-zfs-all-healthy | Debian 13 | ZFS Health | `none` | `zroot ONLINE` | defect 9: no row, ZFS not seen; now the mount's pool, as zpool says it |
| sd-zfs-other | Debian 13 | Volume | `0.00/0.00 [0.00%]` | `12.00/100.00 [12.00%]` | defect 8; KNOWN-DEFECT (hard-coded default): 0.00/0.00, the hard-coded dataset not there; now the mount's |
| sd-zfs-other | Debian 13 | Disk Usage | `░░░░░░░░░░░░░░░░░░░░` | `none` | decision 2 (a), left out: a 20-wide bar; no row |
| sd-zfs-other | Debian 13 | ZFS Health | `HEALTH O.K.` | `rpool ONLINE` | defect 8; decision 2 (a): HEALTH O.K. with the dataset not there; now the mount's pool, as zpool says it |
| sd-zfs-other | Debian 13 | / | `none` | `rpool/ROOT/pve-1 zfs` | decision 2 (a); gallery-system-details.md §6, /: no row; now the root's source and type |
| sd-export, sd-export-zfs | Debian 13 | files | `system_status.txt 0644` | `none` | decision 5 (a); defect 17: the export, relative to the working directory, 0644; now no item writes, and hardn system-details > file saves |
| sd-export, sd-export-overwrite, sd-export-zfs | Debian 13 | export | `Machine status exported to system_status.txt` | `none` | decision 5 (a): the export's message; no item |
| sd-export, sd-export-named, sd-export-overwrite, sd-export-fails, sd-export-dry-run, sd-export-zfs | Debian 13 | draws | `2` | `1` | decision 5 (a): drawn again after the export; now the key refused and asked again |
| sd-export, sd-export-named, sd-export-overwrite, sd-export-fails, sd-export-dry-run, sd-export-zfs | Debian 13 | export prompt | `Enter filename to export system status (default: system_status.txt):` | `none` | decision 5 (a): the file name asked; no item |
| sd-export, sd-export-named, sd-export-overwrite, sd-export-fails, sd-export-dry-run, sd-export-zfs | Debian 13 | pause | `Press Enter to continue...` | `none` | decision 5 (a): a pause after the export; no item |
| sd-export, sd-export-named, sd-export-overwrite, sd-export-fails, sd-export-dry-run, sd-export-zfs | Debian 13 | key refused | `none` | `Invalid option. Please try again.` | decision 5 (a): item 2 offered; now no item 2, and the key refused in origin/main's words |
| sd-export-named | Debian 13 | files | `reports/web-01.txt 0644` | `none` | decision 5 (a); defect 17: the export under a directory it made; now no item writes |
| sd-export-named | Debian 13 | directories | `reports 0755` | `none` | decision 5 (a); defect 17: the export's directory, 0755; now no item writes |
| sd-export-named | Debian 13 | export | `Machine status exported to reports/web-01.txt` | `none` | decision 5 (a): the export's message; no item |
| sd-export-overwrite | Debian 13 | files | `system_status.txt 0644` | `none` | decision 5 (a); defect 18; KNOWN-DEFECT (overwritten with no question): alice's file overwritten; now no item writes, and hardn system-details > file saves |
| sd-export-fails | Debian 13 | export | `Failed to write file: failed to create directory reports: mkdir reports: not a directory` | `none` | decision 5 (a); defect 18: the failure said, the name not asked again; no item |
| sd-export-dry-run | Debian 13 | export | `[DRY-RUN] Would write system status to system_status.txt` | `none` | decision 5 (a): dry run's line; no item |
| logs-open | all | lines shown | `all 5` | `All 5 lines` | decision 8 (a); the slice's Logs dump: the whole file at once, no count; now the last lines, at most logview.MaxLast, with the total |
| logs-open, logs-large, logs-escape, logs-other-path, logs-other-path-stale | Debian 13 | pager | `$PAGER less set, not run` | `less -- /var/log/hardn.log; LESSSECURE=1 LESSHISTFILE=- TERM=xterm-256color LC_ALL=C.UTF-8` | decision 7 (a); P-18: $PAGER set, and not run; now GNU less alone by this command, $PAGER not read, run only by the screen's key |
| logs-open, logs-large, logs-empty, logs-missing, logs-unreadable, logs-escape, logs-other-path, logs-other-path-stale | every cell of each | title | `View Logs` | `Logs` | decision 11; gallery-system-details.md §6, Title bars: View Logs; now the main menu's label |
| logs-open | all | sections | `~~~ line, Log Contents:, its rule, # Contents of /var/log/hardn.log:` | `File, All 5 lines` | gallery-system-details.md §6, Logs sections: a ~~~ line, Log Contents: and its rule, and # Contents of where lines print; now File, and the last lines' header |
| logs-open, logs-large, logs-empty, logs-missing, logs-unreadable, logs-escape | every cell of each | file row | `Log file: /var/log/hardn.log` | `Path /var/log/hardn.log` | gallery-system-details.md §6, Logs rows: Log file:; now Path |
| logs-open, logs-large, logs-empty, logs-missing, logs-unreadable, logs-escape, logs-other-path, logs-other-path-stale | every cell of each | pause | `Press Enter to return to the main menu...` | `none` | gallery-system-details.md §5, Made here, No pause: a pause; now a sub-menu's menu |
| logs-open | Debian 13 | items | `none` | `1) Open: The whole log in less; 2) Refresh: Read the log again; 0) Return` | gallery-system-details.md §5, Made here, Refresh stays; decision 7 (a); gallery-system-details.md §6, Logs items: no items, a pause; now Open where logview.Pager says, and Refresh |
| logs-open, logs-large, logs-empty, logs-missing, logs-unreadable, logs-escape, logs-other-path, logs-other-path-stale | every cell of each | menu heading | `none` | `Select an option` | gallery-system-details.md §5, Made here, No pause: no menu; now Backup's heading |
| logs-open, logs-large, logs-escape, logs-other-path, logs-other-path-stale | Debian 13 | prompt | `none` | `Enter selection [0-2 or q]` | gallery-system-details.md §5, Made here, No pause: no menu; now Backup's prompt |
| logs-open | Alpine 3.23 | pager | `$PAGER less set, not run` | `none` | decision 7 (a): $PAGER set, and not run; now none: BusyBox's less refuses less --version |
| logs-open | Alpine 3.23 | items | `none` | `1) Refresh: Read the log again; 0) Return` | gallery-system-details.md §5, Made here, Refresh stays; decision 7 (a); gallery-system-details.md §6, Logs items: no items, a pause; now Open where logview.Pager says, and Refresh |
| logs-open (Alpine 3.23); logs-empty (Debian 13); logs-missing (Debian 13); logs-unreadable (Debian 13) | as listed | prompt | `none` | `Enter selection [0-1 or q]` | gallery-system-details.md §5, Made here, No pause: no menu; now Backup's prompt |
| logs-large | Debian 13 | lines shown | `all 20000` | `Last 256 of 20000 lines` | decision 8 (a); the slice's Logs dump: the whole file at once, no count; now the last lines, at most logview.MaxLast, with the total |
| logs-large | Debian 13 | sections | `~~~ line, Log Contents:, its rule, # Contents of /var/log/hardn.log:` | `File, Last 256 of 20000 lines` | gallery-system-details.md §6, Logs sections: a ~~~ line, Log Contents: and its rule, and # Contents of where lines print; now File, and the last lines' header |
| logs-large | Debian 13 | size | `868894 bytes` | `848 KiB` | gallery-system-details.md §6, Logs rows, Size: bytes; now the row's words, the same size |
| logs-large, logs-escape, logs-other-path, logs-other-path-stale | Debian 13 | items | `none` | `1) Open: The whole log in less; 2) Refresh: Read the log again; 0) Return` | gallery-system-details.md §5, Made here, Refresh stays; decision 7 (a); gallery-system-details.md §6, Logs items: no items, a pause; now Open, as logview.Pager says, and Refresh |
| logs-empty | Debian 13 | state | `no words` | `Empty` | defect 26; gallery-system-details.md §6, Empty: no words; now the row's |
| logs-empty, logs-missing, logs-unreadable | Debian 13 | pager | `$PAGER less set, not run` | `none` | decision 7 (a); decision 8 (a), its screen-empty, screen-missing and screen-unreadable: $PAGER set, and not run; now none: no Open item where there are no lines |
| logs-empty | Debian 13 | sections | `~~~ line, Log Contents:, its rule, # Contents of /var/log/hardn.log:` | `File` | gallery-system-details.md §6, Logs sections: a ~~~ line, Log Contents: and its rule, and # Contents of where lines print; now File, and the last lines' header |
| logs-empty, logs-missing, logs-unreadable | Debian 13 | items | `none` | `1) Refresh: Read the log again; 0) Return` | gallery-system-details.md §5, Made here, Refresh stays; decision 8 (a); gallery-system-details.md §6, Logs items: no items, a pause; now Refresh alone: nothing to open |
| logs-missing | Debian 13 | state | `Error displaying logs: failed to access log file /var/log/hardn.log: stat /var/log/hardn.log: no such file or directory` | `No log yet` | defect 26; gallery-system-details.md §6, No log yet: Stat's words, the path named three times; now the row's |
| logs-missing, logs-unreadable | Debian 13 | sections | `~~~ line, Log Contents:, its rule` | `File` | gallery-system-details.md §6, Logs sections: a ~~~ line, Log Contents: and its rule, and # Contents of where lines print; now File, and the last lines' header |
| logs-unreadable | Debian 13 | state | `Error displaying logs: failed to read log file /var/log/hardn.log: open /var/log/hardn.log: permission denied` | `Can't read it: permission denied` | gallery-system-details.md §6, Can't read it: <reason>: os's words and the path; now the row's, and the reason |
| logs-escape, logs-other-path-stale | Debian 13 | lines shown | `all 1` | `1 line` | decision 8 (a); the slice's Logs dump: the whole file at once, no count; now the last lines, at most logview.MaxLast, with the total |
| logs-escape | Debian 13 | lines | `2026/10/29 08:12:44 INFO: run 1: \| \x1b]0;a title a command set\a\x1b[31mred` | `2026/10/29 08:12:44 INFO: run 1: \| ^[]0;a title a command set^G^[[31mred` | defect 22; KNOWN-DEFECT (bytes go to the terminal as they are): the line's escape as it is; now logview.Safe's ^[ and ^G |
| logs-escape | Debian 13 | raw escape | `raw` | `none` | defect 22; KNOWN-DEFECT (bytes go to the terminal as they are): reaches the terminal raw; now made safe |
| logs-escape | Debian 13 | sections | `~~~ line, Log Contents:, its rule, # Contents of /var/log/hardn.log:` | `File, 1 line` | gallery-system-details.md §6, Logs sections: a ~~~ line, Log Contents: and its rule, and # Contents of where lines print; now File, and the last lines' header |
| logs-other-path, logs-other-path-stale, logs-p-missing | Debian 13 | file read | `/srv/log/custom.log` | `/var/log/hardn.log` | decision 6 (a); defect 21; P-18: cfg.LogFile, which hardn has never written; now the file hardn writes |
| logs-other-path | Debian 13 | state | `Error displaying logs: failed to access log file /srv/log/custom.log: stat /srv/log/custom.log: no such file or directory` | `lines` | decision 6 (a); defect 21; KNOWN-DEFECT (says the log is not there): the log said not there; now the file hardn writes, with its line |
| logs-other-path | Debian 13 | sections | `~~~ line, Log Contents:, its rule` | `File, 1 line` | gallery-system-details.md §6, Logs sections: a ~~~ line, Log Contents: and its rule, and # Contents of where lines print; now File, and the last lines' header |
| logs-other-path, logs-other-path-stale | Debian 13 | file row | `Log file: /srv/log/custom.log` | `Path /var/log/hardn.log` | decision 6 (a); gallery-system-details.md §6, Logs rows: Log file:, the other path; now Path, the file hardn writes |
| logs-other-path | Debian 13 | lines shown | `none` | `1 line` | decision 6 (a); decision 8 (a): none, the log said not there; now the last lines, with the total |
| logs-other-path, logs-p-missing | Debian 13 | lines | `none` | `2026/10/29 08:12:44 INFO: Line written at start-up` | decision 6 (a); defect 21: the line just written not shown; now shown |
| logs-other-path | Debian 13 | size | `none` | `51 bytes` | decision 6 (a): the other file's size, or no read; now the size of the file hardn writes |
| logs-other-path, logs-other-path-stale | Debian 13 | logFile elsewhere | `none` | `hardn.yml's logFile, /srv/log/custom.log, isn't used` | decision 6 (a); gallery-system-details.md §6, hardn.yml's logFile: nothing says the two part; now the row says so |
| logs-other-path-stale | Debian 13 | lines | `2025/01/02 03:04:05 INFO: An old line` | `2026/10/29 08:12:44 INFO: Line written at start-up` | decision 6 (a); defect 21; KNOWN-DEFECT (the other file's lines as hardn's): another file's old line shown as hardn's; now the line just written |
| logs-other-path-stale | Debian 13 | sections | `~~~ line, Log Contents:, its rule, # Contents of /srv/log/custom.log:` | `File, 1 line` | gallery-system-details.md §6, Logs sections: a ~~~ line, Log Contents: and its rule, and # Contents of where lines print; now File, and the last lines' header |
| logs-other-path-stale | Debian 13 | size | `38 bytes` | `51 bytes` | decision 6 (a): the other file's size, or no read; now the size of the file hardn writes |
| logs-p, logs-p-missing | Debian 13 | pager | `$PAGER less set, not run` | `none` | decision 7 (a); decision 10 (a): $PAGER set, and not run; now none: every line off a terminal, no pager |
| logs-p | Debian 13 | -p heading | `# Contents of /var/log/hardn.log:` | `none` | decision 10 (a): a heading naming the path; now the lines alone, off a terminal |
| logs-p-missing | Debian 13 | state | `[ERROR] Failed to read log file /srv/log/custom.log: open /srv/log/custom.log: no such file or directory` | `lines` | decision 6 (a); defect 21: -p's failure on the other path; now the file hardn writes, with its line |
| logs-p-missing | Debian 13 | log written | `<date> <time> ERROR: Failed to read log file /srv/log/custom.log: open /srv/log/custom.log: no such file or directory` | `none` | defect 24; KNOWN-DEFECT (writes an ERROR line to the log); decision 10 (a): a failed read written into the log; now nothing written |
| logs-p-missing | Debian 13 | lines shown | `none` | `all 1` | decision 6 (a); decision 10 (a): none, -p's failure; now every line, off a terminal |
| logs-p-missing | Debian 13 | logFile elsewhere | `none` | `/srv/log/custom.log` | decision 6 (a); gallery-system-details.md §6, hardn.yml's logFile: nothing says the two part; now logview.Elsewhere's path, for unit (d)'s warning, as -p's forms hold the log alone |
| logs-parser | Debian 13 | parser lines kept | `11 of 14 lines` | `14 of 14 lines` | the slice's Broken log parser; KNOWN-DEFECT (dropped if it has fewer); its Made here, The parser: a second line taken as an entry, or dropped; now each line whole, no level |
| logs-parser | Debian 13 | parser levels | `<time> 10, a 1` | `INFO 6, WARNING 1, ERROR 1, SUCCESS 1, INSTALLED 1, none 4` | the slice's Broken log parser; KNOWN-DEFECT (No entry has a level); its Made here, The parser: Level holds the clock, no entry has a level; now logging's level, for the colour alone |
| logs-parser | Debian 13 | parser long line | `1 of 3 lines, no error` | `3 of 3 lines, no error, the long one cut` | the slice's Broken log parser; KNOWN-DEFECT (ends the read with no error); readers §9, <cut>: the read ends at the long line, the rest lost; now the line cut, the read going on |

**v0.3.2** (§4): where it differs from origin/main, from [v032-reference.md](../refactor/phase0/v032-reference.md) (no row for these two screens) and gallery-system-details.md §2.11.

| Scenarios | v0.3.2 |
|---|---|
| sd-open, sd-invalid, sd-return, sd-refresh, sd-group-no-rule, sd-render, sd-render-zfs, sd-error, sd-no-lastlog, sd-never, sd-login-hostname, sd-no-lscpu, sd-zfs-all-healthy, sd-zfs-dataset, sd-zfs-other, sd-export, sd-export-named, sd-export-overwrite, sd-export-fails, sd-export-dry-run, sd-export-zfs | no such screen: v0.3.2 had no System Details (gallery-system-details.md §2.11; read) |
| logs-open, logs-large, logs-empty, logs-missing, logs-unreadable, logs-escape, logs-other-path, logs-other-path-stale, logs-p, logs-p-missing | as here: the whole file after # Contents of, from cfg.LogFile, with the log's path hard-coded (gallery-system-details.md §2.11; read) |
| logs-parser | no such parser: v0.3.2's log is pkg/logging alone (run: git ls-tree v0.3.2) |

<!-- TestParity: end -->

## 5. What origin/main's path did that the new one does not

From the table, and, where marked, from the code:

1. **It wrote an export as root** to a name relative to the working directory, 0644, holding users and addresses, over any file of that name, making its directories (`sd-export*`; defects 17, 18; side-effects row 31). Now no item writes; `hardn system-details > file` saves (decision 5 (a)).
2. **One collector's error lost the report**, printed twice, "check permissions" whatever it was (`sd-error`, defect 7). Now a section says what it could not read, and the rest is kept.
3. **It showed rows the report no longer has:** Domain, Client IP, DNS IP n, the User row, Hypervisor, CPU Freq, the three bars (decision 2 (a)). "Bare Metal" with no `lscpu` (defect 15) and "0.00 GHz" (defect 11) go with their rows.
4. **It read figures wrong:** MemFree alone as free (defect 12), bytes / 1024³ as "GB" (defect 13), on ZFS used/available as used/total (defect 10), a hard-coded dataset (defect 8) and a health check that may never run (defect 9). `sd-zfs-*`'s forms are [unverified].
5. **Last Login was one account's,** root's under sudo (read: `collectors.go:498`), from `lastlog`, blank with none, or with a From that has no dot (defect 14). Now root's and each listed user's, from `lslogins`, a blank guarded; on Alpine `LoginsNoTool`.
6. **A user's mark was "sudo" whatever the tool:** the sudo or wheel group, a file named for the user in `/etc/sudoers.d`, or the name anywhere in `/etc/sudoers` (read: `os_user_repository.go:322-362`). Now the rules, by `users.Elevation`, so `sd-open` on Alpine says doas, and a group no rule names counts for nothing (`sd-group-no-rule`, readers-system-details.md item 16, adopted 2026-10-30); the rows agree where the group and the rule do. A file named for the user and the name in `/etc/sudoers` are read, not run: the golden's mocks answer at the group.
7. **It printed the whole log at once,** read whole into memory, 20,000 of 20,000 lines (`logs-large`). Now the last lines, at most 256, with the total.
8. **It read `cfg.LogFile`,** which hardn has never written (defect 21, P-18): a line just written not shown, another file's old line shown as hardn's. Now `/var/log/hardn.log`, and `Elsewhere` gives the path for the row and unit (d)'s warning.
9. **A line's escape reached the terminal raw** (`logs-escape`, defect 22). Now `Safe`, on and off the screen.
10. **A log's state had no words of its own:** none when empty, Stat's words when missing, the path three times (defect 26). Now "No log yet", "Empty" and "Can't read it: <reason>".
11. **`-p` printed a heading and, on a failed read, wrote an ERROR line into the log** (`logs-p`, `logs-p-missing`; defect 24), and exits 0 (defect 23; read, unit (d)'s). Now every line alone, off a terminal, and nothing written. Every `-p` cell is off a terminal, so `lines shown` agreeing at `all 5` on `logs-p` says nothing of a terminal (§8). The rest of `-p`'s stream (the config line, the clear, the OS line; defect 25) is in no golden: read, gallery-system-details.md §2.9.
12. **`$PAGER` was set and never run.** Now GNU `less` alone, by a fixed command, with no key pressed nothing runs (decision 7 (a)); on BusyBox's, no pager.
13. **`GetLogs` read a format hardn does not write,** no entry with a level, a second line an entry or dropped, a line over 64 KiB ending the read (`logs-parser`). It had no live caller; `ParseLine` is for a line's colour alone, and since `next/s1-screen` reads the level and nothing else (readers-system-details.md §12 decision 50).
14. **Each draw read everything again,** four `GetHostInfo`s, and each option recursed (defect 20; `reads a draw`; `draws`; `sd-invalid`, `sd-refresh`). Now one read a draw, in `nav.Prompter.Run`'s loop.

15. **It drew and built rows in ways no row holds,** which go with unit (c) or (d) and are not in the table: a box fixed at 50 columns, a longer line past it (defect 4); a renderer that does I/O, which nothing can place or test (defect 5); a clear on each draw, the title twice, and a clear in a pipe (defect 6); three copies of the print, one with no caller (defect 27).
16. **The export was its own text** (defect 19): headings, Root Partition, Root Free, Memory Free, DNS Servers and, on ZFS, "ZFS Filesystem: zroot/ROOT/os", found nowhere else. Those lines are recorded in `parity.golden`, not compared: under decision 5 (a) there is no export.
17. **Outside these rows,** both read: "Check the log file at" after Run All names a file not written (defect 21's third clause), and `main()`'s two lines as a user other than root spoil `> file` (defect 28). Both are unit (d)'s. The log's 0644 (defect 29) is not this slice's to correct (gallery-system-details.md §7).

The new path does what origin/main did not: it adds IPv6 ([I] for `inet6`'s form; defect 16), the root's source and type, and each listed user's login.

## 6. UNEXPLAINED differences

**None.** Every difference has a source this table may cite, or a `KNOWN-DEFECT` line its golden holds: *since `next/s1-screen`, the new differences the screens bring too, each tied to a decision, a Made here call or a row of gallery-system-details.md §6, none pending.* The one this unit carried pending (`Tie.Pending`) is tied since the maintainer approved its source, 2026-10-30, answering "Should a user's sudo mark come from the rules alone?" with "Adopt, start (c)". It was:

- **`Non-System Users`, in `sd-group-no-rule`, on Debian 13 (1 cell):** origin/main says `alice (sudo)` because alice is in the sudo group; the new path says `alice (No sudo)` where no rule names that group, as it reads the rules alone (`users.Elevation`; run: `TestUsers`, `TestElevation`). No source this table could then cite said it: no V-number; no decision of gallery-system-details.md §5; its `cli-not-root` row, adopted 2026-10-30, is about rules a user cannot read, not rules read that name no group; and `users.Elevation` came into its §8 with `next/s1-readers`, after adoption. **Tie, approved 2026-10-30:** readers-system-details.md item 16, "a group a rule does not name does not". Debian 13's and Ubuntu 26.04's VMs hold `%sudo ALL=(ALL:ALL) ALL` (run: `vm-runs/s1/16-truth-*.log`), and there the rows agree: they differ only where that rule has gone.

## 7. For unit (c)

- **Keep the test, and drive the screens.** `TestParity` lives in `pkg/screens/systemdetails`, the package unit (c) fills; depguard's `screens-through-host` covers its non-test files alone. Logs' rows may import `pkg/screens/logs` here, or move to it with a Slice of their own and a page region of their own, which the harness has no marker for yet; one page is smaller.
- **What the screens must draw and leave equal to these facts:** each row of `factRows`, by gallery-system-details.md §6's words and readers-system-details.md §7's map: nothing written, nothing run but `readCommands` (Open's `less` alone, by its key); the `report` row's state, a section's rows kept and "Can't read <source>" where it says so; the `Lines`, `NoLog`, `Empty` and `Unreadable` states in their words; the last lines fitted from `Reading.Last`'s end, with "Last N of M lines".
- **Report values through `logview.Safe`,** on and off the terminal (readers-system-details.md §12 decision 32): `clean` leaves a format character such as U+202E. No aspect holds this until the screens exist.
- **What the table gains then:**
  - each cell's rows read from the screen's own output, not `factRows`: a drawn row beside its fact, so that "Debian 13 (Trixie)", "4.2 of 38.6 GiB (11%)", "No sudo" and "None recorded" are compared, and `Cores` and `Uptime` against the words §6 decides;
  - the items: Refresh on both, Open where `Pager` says, no Export;
  - the lines §2 lists as not compared, the title "Logs" and no pause on Logs among them;
  - `system_details_invalid`, `_return`, `_refresh` as rows, with defect 20's recursion gone;
  - the driver held to one read a draw, as `recordOf` holds it to reads.
  The table's differences stay but for the values the words change: a screen that draws otherwise shows as UNEXPLAINED or a failed golden.

*Built in `next/s1-screen` ([screen-system-details.md](screen-system-details.md) §8, decisions 10 to 13):* each point above, as §2 has it. Existing values changed, by the screen's words alone: `OS` on the renderer rows; `Last Login` in every cell; `report` on Alpine and in `sd-error`; `state` where the log is missing or unreadable; `lines shown` where lines show; `logFile elsewhere` on the screen's two cells. Newly differing: `Processor` on the renderer rows, cut at its row on a terminal (Made here; whole off one, `holdWhole`), and `size` on `logs-large`, "848 KiB" (§6). Every other existing value is as it was. `pkg/screens/logs` is imported here; Logs' rows stayed, one page being smaller. `internal/parity` did not change.

## 8. For unit (d)

**What origin/main's path did:** main menu 9 and 10, `hardn system-details`, `-p`: §4's rows and §5.

**The record to freeze first,** before the legacy code goes, as parity-environment.md §8 did: `TestParity` reads `pkg/characterization/testdata/golden/menu/system_details_*.golden` (22) and `menu/logs_*.golden` (11) in place, and `TestParityGoldens` lists them. Move them unchanged (`git mv`) under `pkg/screens/systemdetails/testdata/legacy/menu/`, and point `legacyDir` there, with these tests retired in the same commit:

- `TestLegacyMenuSystemDetails`, `TestLegacySystemDetailsFlags` (`menu_system_details_test.go`, with its stand-ins' `init`, which `TestLegacyMenuLogs`' `less` also uses: move what it needs or retire them together);
- `TestLegacyMenuLogs`, `TestLegacyLogsParser` (`menu_logs_test.go`).

Then prove it: each file `cmp` equal to its old path at the freeze's parent; `go test -run TestParity -update` leaves `git status` clean. Add a `TestLegacyRecord`, as Environment's: each file's SHA-256, no other file there.

**What may then go** (gallery-system-details.md §9; written, not run; each with its `git grep`): `pkg/menu/system_details_menu.go`; `pkg/menu/logs_menu.go`; `pkg/system`; the log manager, service, model, port and repository (`GetLogs`, `PrintLogs`); the lastlog adapter; `utils.PrintLogs`; `HostInfoManager` stays where the main menu uses it. Then main menu 9 and 10 open the screens, `hardn system-details` takes decision 1 (a)'s flags, `-p` decisions 9 and 10 (a).

**What unit (d) must add, as no aspect here compares it:** exit statuses and `-p`'s stderr ("hardn: no log yet: <path>", "hardn: can't read <path>: <reason>", "Run it with sudo."); `system_details_flags`' eleven command lines against decision 1 (a); `-p` ahead of the config load and `detectOS`, so that its pipe form holds the log alone (`logs-p`'s `-p heading` row holds `PrintLogs` alone: gallery-system-details.md §2.9); `-p` to a terminal, decision 10 (a)'s file rows and last lines (`cli-terminal-tail`), with its blank-line form; "Check the log file" and `main()`'s two lines (§5 item 17).

## 9. Written, not run (gate 5)

Nothing here ran on Linux or as root. origin/main's rows are its run's on macOS, as the test's user, with stand-ins and mocks; the new path's are hostfake's. **Run elsewhere, not by this unit, read only:** both readers on the three VMs, as the user and as root, compared by hand with what each host says (`vm-runs/s1/15-readers-*.log`, `16-truth-*.log`; readers-system-details.md §4). **Not run anywhere:** hardn itself; `less` under hardn; `zpool` and `zfs`; Proxmox VE 9; origin/main's screens on a host (`system_details_flags`' NOTE says so of its one case).

## 10. Files and size

- **Added:** `pkg/screens/systemdetails/parity_test.go`, `legacy_read_test.go`; `pkg/screens/systemdetails/testdata/golden/parity.golden`; this page.
- **Changed:** `internal/parity` (`Slice.Reader`, `LegacyRead`, `EndState.Rows`, `Row`, `Scenario.Only`, `scan-lines` among `NewState`'s reads, `whereRows`, `rowsProblem`, `onlyProblem`, and their tests); [screen-kit.md](screen-kit.md) §2 and §7, [gallery-system-details.md](gallery-system-details.md) §9, [readers-system-details.md](readers-system-details.md) §13, the design-system README's "Running and regenerating" and the makefile's comment on `golden-update`. [parity-environment.md](parity-environment.md) has no section for the next slice, so it is not touched. No non-test Go outside `internal/parity`; no legacy code, no reader, no existing golden or testdata file.
- *Since `next/s1-screen`:* `drive_test.go` added, 482 lines; `parity_test.go` and `legacy_read_test.go` 537 added and 258 removed; `parity.golden` 2,420 lines; the table 125 entries, 586 differences in 34 cells; `internal/parity` unchanged.
- **Size** (`git diff --numstat 41ef415e` and `wc -l`, run): `internal/parity` 183 lines added and 16 removed, its tests 115 and 0, under the guideline of about 300 for the harness. The slice's part is larger than Environment's because its 33 goldens are in four layouts, and two screens' rows. This slice's part: `parity_test.go` 1,089 lines of test Go, its table 83 entries, 309 differences in 31 cells; the reader and its refusals, `legacy_read_test.go`, 823, beside the test as every line it knows is these screens'; `parity.golden` 1,705 lines; this page about 360 lines. Environment's unit (b2) for comparison (parity-environment.md §10): the harness 585 and 54, its part 887 lines, 69 entries.
