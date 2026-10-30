# System Details and Logs in the gallery, before any build (slice 1, unit (a): `system-details`, `logs`, `play system-details`, `play logs`)

> **Unit:** refactor plan slice 1, unit (a), `next/s1-gallery`: "Gallery mock and full sequence, reviewed first" (plan §2, "Phases 3–5 together", §3.22). Slice table row 1: "System Details + Logs (read-only) | No frame. System Details inferred from origin/main (not in v0.3.2); 2025 hex-lineage sections are reference. Logs from v0.3.2 + origin/main | No-op system-details flags; Logs dump → `$PAGER` or tail; broken log parser | §3.11 (system-details flags)". Decisions cited, not reopened: P-03 (decided 2026-10-23: one "Plan only" mode set at start); the main menu's one form at every height (the maintainer, 2026-10-23; DEV-052); Backup's decided heading and prompt (gallery-backup.md §6). Proposals shown: [P-17](../refactor/proposals.md#p-17) and [P-18](../refactor/proposals.md#p-18), both decided (§5). Proposals these mocks take a side in, both still proposed: [P-28](../refactor/proposals.md#p-28) with [P-09](../refactor/proposals.md#p-09), which the design-system guide says these two screens wait on (README §9.12, "The other screens"): the mocks draw P-28 (a), a sub-menu with no pause (§5, Made here). §3.11's change for the five flags alone is approved, narrowed (decision 1); the rest of §3.11 is not. V-numbers: none holds any of the slice's three defects; nearest are V25, V26 and side-effects rows 1 and 31 (§7).
> **Status:** built and tested on macOS, in process. No hardn binary ran. Read-only checks of the hosts' tools ran on the three VMs as a user (`_support/hardn-audit-2026-10-16/vm-runs/s1/`, `vm-runs/s1/` below; `lslogins`, `sshd -T` and sshd's logs alone also through sudo or doas); the rest is read. The mocks read simulated hosts; the characterization runs pkg/menu's two screens and `hardncmd.SystemDetailsCmd` in child test processes, with stand-ins on `PATH` for what `pkg/system` runs itself. The eleven decisions in §5 are adopted; every word new to origin/main is decided with them (§6). Nothing is wired into `cmd/hardn`, `pkg/menu`, `pkg/system`, `pkg/cmd`, `pkg/logging`, `pkg/engine`, `pkg/host`, `pkg/screens` or `pkg/ui`. `make check` passes on this Mac.

## 1. Review description

1. **Purpose and scope.** What origin/main's System Details screen, `hardn system-details`, its Logs screen and `-p` do (§2); the slice's three defects confirmed, and others found (§7); the two screens proposed on the design system, drawn from what real code and the gallery's stand-ins read of simulated hosts (§3); eleven decisions (§5). In scope: characterization tests (additions only), the gallery's `system-details` and `logs` modes (`systemdetails.go`, `logs.go`, their tests, the dispatch in `main.go` and `play.go`), their goldens, this page, a bullet and a note in the design-system README and a line in each of P-17's, P-18's and P-28's entries.
2. **Behaviour changes:** none to hardn. No component, layout, engine, host, config, logging or menu code changed. In the playable main menu, 9 and 10 now lead to the mocks (`sdLinks`, `logsLinks`) where they said the gallery had no screen.
3. **Tests added.** `pkg/characterization/menu_system_details_test.go`: `TestLegacyMenuSystemDetails` (21 cases), `TestLegacySystemDetailsFlags`. `menu_logs_test.go`: `TestLegacyMenuLogs` (10), `TestLegacyLogsParser`. `internal/tools/uigallery/systemdetails_test.go`: `TestSystemDetailsGolden` (29 listing mocks), `TestPlaySystemDetailsGolden` (13 transcripts), `TestSDHosts`, `TestSDLastLogin`, `TestSDLsloginsCall`, `TestSDReadsAddrs`, `TestSDBlankCPU`, `TestSDNotRootNamesTheRules`, `TestSDUnreadableKeepsTheRest`, `TestSDZFSDatasetIsRead`, `TestSDJSONIsTheDocument`, `TestSystemDetailsUsage`, `TestSystemDetailsHeights`, `TestSystemDetailsWritesNothing`. `logs_test.go`: `TestLogsGolden` (24), `TestPlayLogsGolden` (14), `TestLogHosts`, `TestSafeLine`, `TestLogRowsKeepTheCharacters`, `TestNotesOpenWithTheirSource`, `TestParseLogLine`, `TestPagerFor`, `TestLogsUsage`, `TestLogsHeights`, `TestLogsWriteNothing`. `TestPlayTouchesNothing` takes the new files as it comes. `host_info_test.go` changed a comment alone: it said System Details had no golden.
4. **Golden diffs.** Added only: `pkg/characterization/testdata/golden/menu/system_details_*.golden` (22), `logs_*.golden` (11); `internal/tools/uigallery/testdata/golden/system-details/*.golden` (29), `logs/*.golden` (24), `play/system-details*.golden` (13), `play/logs*.golden` (14). No existing golden changed (`git diff --name-only --diff-filter=MDR 962d800 HEAD -- '*testdata*'` is empty).
5. **UI.** No component changed, but the mocks need two things `pkg/ui` lacks, each a deviation for unit (c) to log: a section that holds code, as the log's lines are captured output (`logScreen`, `codeRows`; the README's "Code" rule, §9.1), and an uncapped form off a terminal (§5, Made here). `report-wide` would add a third, not offered. Freeze screenshots deferred (design-system README §9.16).
6. **Inferred screens.** Every System Details mock's note opens "Inferred from origin/main (unreleased).", P-17's label: there is no Figma frame, and v0.3.2 had no such screen ([phase0-findings.md](../refactor/phase0-findings.md):548, :697). Every Logs mock's note opens "Inferred from v0.3.2 and origin/main.": no frame either, its item and words v0.3.2's and origin/main's (§2.11). The `origin*` mocks are origin/main's own words and say so instead (`TestNotesOpenWithTheirSource`).
7. **New dependencies:** none.
8. **Per-distro notes:** the hosts' tools ran read-only on Debian 13, Ubuntu 26.04 and Alpine 3.23, amd64 (`vm-runs/s1/`): decision 3 holds the login tools, decision 7 `less`, §8 the rest. The kernels drawn are those of earlier VM runs ([vmtest.md](../safety/vmtest.md) §8.5, §8.6). **Not run:** `zpool` and `zfs` (no ZFS VM), Proxmox VE 9 (no VM), arm64, a tighter umask, a pager under hardn, Alpine's less package, a write through BusyBox's `s`, `lslogins` on a usual host, through a real rotation or as root for more than one account, and the new Go tests on Linux. As root only `lslogins` for one account, `sshd -T`, `journalctl` and `grep` of `/var/log/messages` ran. The new characterization tests ran on macOS as a user other than root, under umask 022, 027 and 077; on Linux, untested, `TestLegacySystemDetailsFlags` would read the real host, read only.
9. **`git status --short --ignored`** before the docs commit: the docs paths alone.

## 2. What origin/main does

Each claim is **run** (a golden of `TestLegacyMenuSystemDetails`, `TestLegacySystemDetailsFlags`, `TestLegacyMenuLogs` or `TestLegacyLogsParser`, `menu/<case>.golden`) or **read** (the code cited). Line numbers are the trunk's (962d800). The two screens' files and `pkg/system` are origin/main's (9d46866) on the same lines, but for the pauses' "Press Enter" (DEV-050) and gopsutil v4's imports; `pkg/logging/logging.go` and `pkg/utils/utils.go` have moved on the trunk: **`LogFileOnly` and `revertFileLog` (e264b8bb, `next/p3-confirm-cleanup`) and `pkg/host/runlog.go` (6a04b467) are the trunk's and not on origin/main**, so the "run <n>:" lines in these goldens are not lines origin/main writes, as §2.7 and §2.10 mark; origin/main's own are those of `LogInfo`, `LogWarning`, `LogError`, `LogSuccess` and `LogInstall`; on origin/main the main menu's items are at `main_menu.go:460-461`, `InitLogging` at `cmd/hardn/main.go:56` and `-p` at `:398-399`. What has no seam is masked whole in the screen goldens, each saying so: the OS, Hostname, User and Load lines, the addresses and Domain. The render goldens hold those lines on synthetic values.

### 2.1 Reaching them (run: `system_details_flags`, `logs_print_flag`, `logs_print_flag_missing`; pkg/menu `main_menu_layout_test.go:171-172`; cmd/hardn `TestDryRunAloneIsNotRefused`)

- **Main menu 9**, "System Details" "View system information", and **10**, "Logs" "View log file" (`pkg/menu/main_menu.go:768-769`): both behind the root check (`cmd/hardn/main.go:148-152`), so they run as root (read). Under plan only both open as live (read).
- **`hardn system-details`** (`cmd/hardn/main.go:84`, `pkg/cmd/system_details_cmd.go`) has its own `RunE` and **no root check**: the check is on the root command's `Run` (read). It prints the report with no prompts (`system_details_cmd.go:124`, read). What it reads as another user is not recorded. `main()` runs first (`cmd/hardn/main.go:57-62`), so as another user it says it cannot open the log or write the example configuration (read).
- **`-p`/`--print-logs`** (`cmd/hardn/main.go:98`, `:416-418`) sits on the root command's `Run`, after the root check, the config load (`:155`) and `detectOS` (`:184-188`): a file it asks for as 0644 (unless a tighter umask applied) hardn refuses to print to another user, and a missing hardn.yml gets its offer first (read; `-n -p` reaching the root check is `TestDryRunAloneIsNotRefused`).

### 2.2 System Details, the report (run: `system_details_open`, `_render`, `_render_zfs`, `_refresh`, `_return`, `_invalid`)

- Cleared on every draw (`system_details_menu.go:38`), the title twice, a box fixed at 50 columns (`pkg/system/display.go:32`). A longer line runs past the box, unpadded, uncut, 81 columns in `_render`. No headings: blank lines alone part the groups, in this order: OS and kernel; host, IPs, client IP, DNS, user; non-system users; CPU, load with 20-wide bars; volume and bar; memory and bar; last login and uptime.
- `DisplayMachineStatus` prints as it builds and returns nothing a caller could place or test (`_render`'s note).
- Items: "1) Refresh Information" "Reload system status from system", "2) Export Information" "Export system status to file", "0) Return"; "❯ Enter selection [0-2 or q]:". 0 and q return (`_return`). An invalid key: "Invalid option. Please try again.", a pause, the screen again (`_invalid`).
- Each option but 0 and q draws the screen again by calling itself (`:102`, `:115`, `:128`), a call deeper each time. Each draw reads everything again: `GetHostInfo` four times, so `uname -r` and `/proc/meminfo` four times (`_open`'s counts).

### 2.3 When a collector fails (run: `system_details_error`, `system_details_error_export`)

- `GenerateSystemStatus` stops at the first error and returns nil (`pkg/system/details.go:91-117`), so what was read is lost: for want of memory, the OS, network, users and CPU. The golden fails the memory collector, `/proc/meminfo` unreadable and `free` not on `PATH`; the network, user, disk and login collectors can fail too (`collectors.go:99`, `:451`, `:500`; `user_info.go:24`).
- The error prints twice, above a box "System Details Error" and in it. The box says "Please check system permissions and try again." whatever the error (`system_details_menu.go:43-44`, `:59-62`). Item 2 is not offered, yet 2 is still taken: "Cannot export: No system information available" (`_error_export`).

### 2.4 Export (run: `system_details_export_default`, `_named`, `_overwrite`, `_fails`, `_dry_run`, `_zfs`)

- "Enter filename to export system status (default: system_status.txt):". The name is relative to the working directory, no prompt says which, and the message repeats it as typed (`system_details_menu.go:137-139`, `:231`; `_default`). Mode 0644, holding users and addresses (`pkg/utils/utils.go:139`). Side-effects row 31.
- A named export makes its directories, 0755 (`_named`). A file of that name is overwritten with no question and no backup (`_overwrite`). A failed write prints "Failed to write file: failed to create directory reports: mkdir reports: not a directory" and the screen goes on, the name not asked again (`_fails`). Under dry run: "[DRY-RUN] Would write system status to system_status.txt", nothing written (`_dry_run`).
- The export is its own text, built apart from the screen (`system_details_menu.go:142-222`; `display.go:42-125`): it has headings, the root partition and the free sizes; the screen has the bars (`_default`). On ZFS it says "ZFS Filesystem: zroot/ROOT/os" (`_zfs`).

### 2.5 Rows that read wrong (run: `system_details_open`, `_no_lastlog`, `_login_hostname`, `_never_logged_in`, `_no_lscpu`, `_zfs_dataset`, `_zfs_other_dataset`, `_zfs_all_healthy`, `_render_zfs`)

- **CPU Freq** reads "0.00 GHz" where the host-info service gave a model and `nproc` answered (`collectors.go:199-205`, `:231`, `:246-252`; `_open`), the usual case [I, not run on a host].
- **Memory** counts page cache as used: used is total less MemFree (`collectors.go:329`), so 3.89/7.78 GiB where MemAvailable is 5.83 GiB (`_open`). **Disk** divides by 1024³ and says "GB" where memory, the same division, says "GiB" (`collectors.go:430-432`, `:370`; `_open`). **IP addresses** are IPv4 alone (`pkg/adapter/secondary/os_host_info_repository.go:161`, `collectors.go:186`; read).
- **Last Login** is `user.Current()`'s (`collectors.go:498`), so root's under sudo (read), from `lastlog -u`. It reads "Oct 29 08:12:44 +0000" and "From: 192.0.2.10" (`_open`), or "Never logged in" (`_never_logged_in`), or blank with no `lastlog`, the error dropped (`collectors.go:505-507`; `_no_lastlog`), and blank when From is a host name with no dot, taken for the date's first word (`lastlog_command_adapter.go:91-96`, `:111-122`; `_login_hostname`). So on stock Debian 13 and Ubuntu 26.04 the row is blank: neither has `lastlog` (run: `vm-runs/s1/01-tools.log`; Debian 13's [release notes](https://www.debian.org/releases/trixie/release-notes/issues.en.html), 5.1.9).
- **Hypervisor** reads "Bare Metal" and sockets read 1 with no `lscpu` (`collectors.go:273-275`, `:314-316`; `_no_lscpu`): not known reads as fact. Stock Alpine 3.23 has no `lscpu` (run: `vm-runs/s1/01-tools.log`).
- **ZFS:** `zfs` is asked about the hard-coded `zroot/ROOT/os` (`details.go:85`; `_zfs_dataset`). With another dataset the errors are dropped: "Volume: 0.00/0.00 GB [0.00%]" with "ZFS Health: HEALTH O.K." (`_zfs_other_dataset`). "Volume" is then used/available, not used/total as without ZFS (`display.go:99-100`, `:104-105`; `_render_zfs`). **The branch may never run:** the check looks for "is healthy" (`collectors.go:386`), and `zpool status -x` with no pool named says "all pools are healthy" [I, zpool(8), not run on a ZFS host], which does not hold that text; `_zfs_all_healthy` records that such a host reads as no ZFS.

### 2.6 The five flags (run: `system_details_flags`)

`-E/--export`, `-S/--section`, `-O/--format`, `--json`, `--yaml` (`system_details_cmd.go:38-42`) are accepted and change nothing, the code using them commented out (`:95-121`). The golden holds the syntax tree: `--json` and `--yaml` are read only to set `outputFormat` (`:66-69`), which nothing reads, and the others are only declared and bound. It holds eleven command lines against the same run with no flags: same output and status, no file written, `-O nonsense` no error. **On this Mac every run ends at `DetectOS`**, no `/etc/os-release`, so the comparison reaches that far and the syntax tree shows the rest; the golden says so. The output starts with the clear-screen sequence even with stdout a pipe (`osdetect.go:50`; `utils.go:24-31`).

### 2.7 Logs, the screen (run: `logs_open`, `logs_large`, `logs_empty`, `logs_missing`, `logs_unreadable`, `logs_escape`)

- Cleared; the green `~~~` line, "View Logs"; "- Log file: <path>"; "Log Contents:", a rule; "# Contents of <path>:"; the whole file; a blank line more; "- Press Enter to return to the main menu...". One prompt; no refresh, search or follow (`pkg/menu/logs_menu.go:32-63`; `pkg/adapter/secondary/file_logs_repository.go:81-96`; `_open`).
- **The whole file at once:** 20,000 of 20,000 lines printed, no pager, no count, `$PAGER` not run though set, read whole into memory (`_large`).
- An empty log: the heading and no words (`_empty`); start-up has written to the file by the time the screen can read it (`cmd/hardn/main.go:57`, `:155`, `:185`; `pkg/config/config.go:246`; `pkg/osdetect/osdetect.go:93-95`; read), so this needs hardn's open to have failed or the file truncated while hardn runs. A missing log: "Error displaying logs: failed to access log file <path>: stat <path>: no such file or directory", the path named twice more (`_missing`). At the default path start-up has already made the file (`cmd/hardn/main.go:57`; `pkg/logging/logging.go:38`), so missing needs `logFile` elsewhere or that open failing (read). Unreadable as root needs a read that fails as root [I, not run]; the golden uses mode 0000 as another user (`_unreadable`).
- **A terminal escape reaches the terminal raw** (`file_logs_repository.go:96`; `logging.go:146`; `_escape`): origin/main's defect, on any bytes the file holds. Where such bytes come from is **the trunk's**: its run log holds commands' output (`pkg/host/runlog.go:58-62`), which `pkg/logging` does not clean (`logging.go:131-135`), so on the trunk a command's output can move an admin's terminal when the log is read. On origin/main a narrow search found no line logging a command's output (`git grep -nE 'Log(Info|Error|Warning)\(.*[oO]utput' origin/main -- pkg cmd`; [unverified] as complete).

### 2.8 `logFile`, and the file hardn writes (run: `logs_other_path`, `logs_other_path_stale`)

`main()` always opens `/var/log/hardn.log` (`cmd/hardn/main.go:57`). The screen and `-p` read `cfg.LogFile` (`pkg/infrastructure/service_factory.go:219-222`; `main.go:417`). With `logFile` elsewhere a line just written is not shown and the screen says the log is not there (`_other_path`), or another file's old lines show as hardn's (`_other_path_stale`). This is P-18's [V] claim. `Check the log file at <cfg.LogFile>` is also what hardn tells the user after Run All, naming a file it did not write (`cmd/hardn/main.go:269`, `pkg/menu/run_all_menu.go:278`; read).

### 2.9 `-p` (run, of `logging.PrintLogs` alone: `logs_print_flag`, `logs_print_flag_missing`; the rest read, not run)

- **The whole stream, to a terminal or not:** before the log, on stdout, the config line, `[INFO] Using configuration from: <path>` or the no-config lines (`pkg/config/config.go:246`, `:252`, `:279`; the root command's `Run`, `cmd/hardn/main.go:155`); the clear sequence and the green `~~~` line, even in a pipe (`detectOS`, `main.go:185`; `pkg/osdetect/osdetect.go:50`; `pkg/utils/utils.go:24-37`), the same clear-in-a-pipe as `hardn system-details` (§2.6, §7 row 6); `[SUCCESS] <os> detected` (`osdetect.go:93-95`). Then what `_print_flag` holds: "# Contents of <path>:", a blank line, the file, a blank line more (`logging.go:145-146`). The same on origin/main and in v0.3.2 (`DetectOS` at `main.go:166`, `:156`). `SetSilentMode` is on no part of this path (`pkg/cmd/system_details_cmd.go:50`, `:56` and `os_user_repository.go:22` alone). The config and OS lines also append to the log (`logging.go:99-100`), so each `-p` grows it. `logs_print_flag` calls `PrintLogs` alone, as its NOTE says, so it could not show the rest; no VM run holds a `-p` transcript.
- On a read failure, the logger's "[ERROR] Failed to read log file <path>: open <path>: no such file or directory"; **that line is also written into hardn's log** (`logging.go:141`, `:78`), and `PrintLogs` returns nothing, so the caller goes on (`main.go:417`) and exits 0 (read, not run).
- Three copies of the print: `logging.PrintLogs` (`logging.go:138`), `FileLogsRepository.PrintLogs` (`file_logs_repository.go:81`), which the screen uses, and `utils.PrintLogs` (`pkg/utils/utils.go:117`), which nothing calls (`logs_parser`, last block).

### 2.10 The parser (run: `logs_parser`)

- hardn's lines are `DATE TIME LEVEL: message`, `log.LstdFlags` (`logging.go:46`), levels INFO, WARNING, ERROR, SUCCESS, INSTALLED. origin/main's lines come through the first five. **The trunk's alone:** `LogFileOnly`, and the run log, which has no writer of its own: `revertFileLog` sends each line to `LogFileOnly` (`cmd/hardn/confirm.go:158-163`), so its lines are `INFO: run <n>: $ argv`, `INFO: run <n>: | output`, `INFO: run <n>: exit <code>`.
- `GetLogs` expects `TIME LEVEL: MESSAGE` (`file_logs_repository.go:51-57`): Time holds the date, Level the clock, Message the level and text. This holds on origin/main's lines and the trunk's alike. **No entry has a level.** A second line of a message is an entry if it has three words (Time "with", Level "a") and dropped if fewer (`:53`). A line over 64 KiB ends the read with no error, losing it and all after, as `scanner.Err` is not checked (`:47-70`).
- **No live path calls it:** go/parser finds the two forwarding methods alone (`pkg/application/log_manager.go`, `pkg/domain/service/logs_service.go`), and `LogsManager.GetLogs` has no caller. Only `logs_service_test.go` exercises it, on a mock.

### 2.11 v0.3.2 and the references (read)

- **v0.3.2:** no System Details. Main menu item 11, "Logs" "View log file" (`git show v0.3.2:pkg/menu/menu.go`, :184), `viewLogsMenu` (:699): "# View Logs", `logging.PrintLogs(cfg.LogFile)`, a pause. `-p` as today (`v0.3.2:cmd/hardn/main.go`, :240-241), "# Contents of" included (`v0.3.2:pkg/logging/logging.go`, :94), and `InitLogging` hard-coded at :55. **So `-p` and its form are released, and `logFile` has never set where hardn writes.**
- **2025 hex lineage**, ideas only: `refs/heads/hex-migration:cmd/hardn/systeminfo_demo/systeminfo_screen.go` has "System Information", "Hardware", "Root Storage", "Filesystems"; `stash@{7}` has "System Information", "Hardware Information", "Storage Information", "Additional Filesystems" (`stash@{8}` the same file). Neither has users, addresses or a last login. P-17's seven are nearer origin/main's groups, named.

## 3. The mocks

### 3.1 What to run

From the repository root, with `GOTOOLCHAIN=local GOWORK=off` (add `-profile plain`, `-ascii`, `-width 60`, `-height 40` or `-theme light`, before the names):

```text
go run ./internal/tools/uigallery system-details                 # every mock under its note
go run ./internal/tools/uigallery system-details screen-debian13 origin   # some; -list names them
go run ./internal/tools/uigallery logs                           # every mock under its note
go run ./internal/tools/uigallery play                           # the main menu: 9 and 10 lead to them
go run ./internal/tools/uigallery play system-details            # the hosts' menu
go run ./internal/tools/uigallery play system-details/debian13   # the screen on one host
go run ./internal/tools/uigallery play logs/long                 # likewise
go run ./internal/tools/uigallery play -sequence dry-run logs/lines   # plan only
```

`play` refuses `-fail` on them (`TestSystemDetailsUsage`, `TestLogsUsage`). **The sequence is the reading:** opening, each state, refreshing, leaving and the non-interactive forms. There is no plan, no y/N and no journal; `TestSystemDetailsWritesNothing` and `TestLogsWriteNothing` play every host live and under plan only, every item chosen: nothing written, no command but reads, no pager, no engine, no hold. The listing mocks draw the core cells and `truecolor-dark-ascii-80`; those marked 64+ or 80+ leave out the 60-column cell, and `report-wide` has its own 132-column cell.

The System Details screen on `debian13`, plain, at 80 columns (`screen-debian13`), recommended options:

```text
# System Details ###############################################

# OS and Kernel ------------------------------------------------

   -  OS                   Debian 13 (Trixie)
   -  Kernel               6.12.113+deb13-cloud-amd64
   -  Uptime               3 days, 5 hours, 12 minutes

# Host and IPs -------------------------------------------------

   -  Hostname             web-01
   -  eth0                 192.0.2.20/24

# Users --------------------------------------------------------

   -  alice                sudo

# CPU and Load -------------------------------------------------

   -  Model                QEMU Virtual CPU version 2.5+
   -  CPUs                 4, 1 socket
   -  Load (1, 5, 15 min)  0.12, 0.08, 0.03

# Disk ---------------------------------------------------------

   -  /                    /dev/vda1 ext4
   -  Used                 4.2 of 38.6 GiB (11%)

# Memory -------------------------------------------------------

   -  Used                 1.2 of 3.8 GiB (30%)

# Last Login ---------------------------------------------------

   -  root                 None recorded
   -  alice                Oct 20 11:42 from 192.0.2.10

  Select an option

   1) Refresh              Read the host again

   0) Return

  > Enter selection [0-1 or q]:
```

The Logs screen on `long`, plain, at 80×24 (`screen-long`), recommended options:

```text
# Logs #########################################################

# File ---------------------------------------------------------

   -  Path                 /var/log/hardn.log
   -  Size                 49 KiB

# Last 3 of 1204 lines -----------------------------------------

      2026/10/20 12:00:05 ERROR: Failed to configure firewall:
      | exit status 1
      2026/10/20 12:05:11 INFO: Using configuration from:
      | /etc/hardn/hardn.yml
      2026/10/20 12:05:11 SUCCESS: debian trixie detected

  Select an option

   1) Open                 The whole log in less
   2) Refresh              Read the log again

   0) Return

  > Enter selection [0-2 or q]:
```

**System Details hosts** (`sdHosts`; each `screen-<host>`). Each draws the recommended options unless it names another.

| Host | What it is | What it shows |
|---|---|---|
| `debian13` | Debian 13, kernel 6.12.113+deb13-cloud-amd64, sudo (vmtest.md §8.5), `lslogins` in util-linux | The report, Refresh and Return, 44 lines; "root  None recorded", "alice  Oct 20 11:42 from 192.0.2.10" |
| `ubuntu2604` | Ubuntu 26.04, kernel 7.0.0-38-generic, sudo-rs 0.2.13 (vmtest.md §8.5), `lslogins` in util-linux-extra | The same |
| `alpine323` | Alpine 3.23.6, kernel 6.18.53-0-virt, doas (vmtest.md §8.6); no `lslogins`; `ip` in the VM's own form (`vm-runs/s1/01-tools.log`) | Users says doas; "Logins  Not known: lslogins isn't installed"; 43 lines |
| `pve9` | Proxmox VE 9.0.10, kernel 6.17.2-1-pve (the model's; no VM); no user but root; `lslogins` by inference from Debian 13, its base [I] | Users "None but root"; dataset `rpool/ROOT/pve-1` read from `/proc/self/mounts` [I for the name]; "Pool rpool" ONLINE; the 40-cell CPU name cut at its row; "root  Oct 20 11:42 from 192.0.2.10" |
| `zfs-degraded` | As `pve9`, the pool DEGRADED | "Pool rpool" DEGRADED in the warning's state |
| `unreadable` | As `debian13`, `/proc/meminfo` unreadable | "Can't read /proc/meminfo" under Memory, the other six sections kept |
| `no-logins` | As `debian13`, `lslogins` answering blanks for both accounts | Both "None recorded" |
| `login-console` | As `debian13`, alice at a console: a line with no host [I: the VMs ran it with a host] | "alice  Oct 20 11:42 on tty1" |
| `many` | As `debian13`, 14 users, 9 addresses, IPv4 and IPv6 ([I] for `inet6`'s form: no VM has a global one), two of the users having logged in | 78 lines |
| `report-sections` | Decision 4 (b), not recommended | A line per section as items 1 to 7, 8 Refresh, 16 lines |
| `report-pager` | Decision 4 (c), not recommended | A "Summary" section of seven rows, 1 Open "The whole report in less", 2 Refresh, 20 lines |
| `export-command` | Decision 5 (b), not recommended | Item 2, Export "Say how to save it" |
| `export-file` | Decision 5 (c), not recommended | Item 2, Export "Write it to a file" |

| System Details listing mock | What it shows |
|---|---|
| `report-sections-open` | Item 5, Disk, its rows under the prompt, the screen drawn again |
| `report-pager-open` | Item 1, Open: the note and the command, as no golden can run a pager |
| `report-wide` | Not offered (decision 4): two 64-cell columns at 132 columns, past the content cap and so a new layout; below 132, the report whole |
| `export-command-open` | "To save it, run: hardn system-details > report.txt" |
| `export-file-open` | "Path to write"; "Use a full path, such as /root/system-details.txt."; "Written to /root/system-details.txt, 0600." The gallery writes nothing |
| `plan-only` | The same screen: other sub-menus show the mode nowhere (§5, Made here) |
| `plan-only-indicator` | Not proposed: "Mode Plan only (-n)" beside the heading |
| `cli-terminal` | `hardn system-details` to a terminal: title and sections, no menu, no prompt |
| `cli-pipe` (64+) | The same off a terminal: plain mode, whatever the surrounding cell |
| `cli-pipe-cut` (64+) | The same on `pve9`: the CPU's name cut in the pipe (§5, Made here) |
| `cli-not-root` | As alice off sudo: `main()`'s two lines first, then the report, the row "Not root: /etc/sudoers.d not read" (`/etc/doas.conf` on a doas host) naming what unit (b)'s reader skips; the stand-in reads groups alone, root or not. Last Login reads the same as a user (run: `vm-runs/s1/02-login.log`) |
| `cli-json` (64+), `cli-json-unreadable` (64+) | Depends on decision 1: `pkg/output`'s nine fields from the real `output.Build`, and `details`; a section not read is null and one finding `DETAILS_UNREADABLE` (proposed), exit 0; `details` carries the logins; as a user other than root, `main()`'s lines come first until unit (d) moves them |
| `cli-flags` | The five flags: today, proposed if §3.11's row is approved, and until it is decided |
| `origin` | origin/main's report in its words on the design system: box, clear, blank-line grouping and bars left out; Last Login blank, as `lastlog` is not on the `debian13` host (`system_details_no_lastlog`) |
| `origin-error` | The error twice, above the box and in it (the word "Error:" before each title is the component's), the box's words, the report gone, Export not offered |

| System Details play golden | Keys | What it shows |
|---|---|---|
| `play/system-details` | `play`: 9, 0, 0 | From the main menu and back |
| `play/system-details-refresh` | `system-details/debian13`: 1, 0 | Refresh: uptime and load moved (the fake's); also under LANG=C |
| `play/system-details-plan-only` | `-sequence dry-run`: 9, 1, 0, 0 | Plan only: the same screen, Refresh the same |
| `play/system-details-sudo-rs` | `system-details/ubuntu2604`: 0 | Opening and leaving |
| `play/system-details-alpine` | `system-details/alpine323`: 1, 0 | Refresh on Alpine's forms |
| `play/system-details-zfs` | `system-details/pve9`: 1, 0 | Refresh on ZFS |
| `play/system-details-unreadable` | `system-details/unreadable`: 1, q | The unread section, refreshed, q |
| `play/system-details-sections` | `system-details/report-sections`: 5, 8, 0 | An item opened, Refresh |
| `play/system-details-pager` | `system-details/report-pager`: 1, 0 | Open: the note |
| `play/system-details-export-command` | `system-details/export-command`: 2, 0 | The words |
| `play/system-details-export-file` | `system-details/export-file`: 2, 0 | The question, the refusal, the file; nothing written |
| `play/system-details-export-file-plan-only` | `-sequence dry-run system-details/export-file`: 2, 0 | "Not available in plan only." |
| `play/system-details-hosts` | `system-details`: 6, 0, 0 | The thirteen hosts' menu, `unreadable`, back |

**Logs hosts** (`logHosts`; each `screen-<host>`).

| Host | What it is | What it shows |
|---|---|---|
| `lines` | Debian 13, less installed, 4 lines | Path and Size rows, "Last 3 of 4 lines" at 80×24, "All 4 lines" at 80×40, as three wrap; WARNING and ERROR in the warning's colour; 1 Open, 2 Refresh |
| `long` | 1204 lines, nothing rotating them, ending in this start's own lines | "Last 3 of 1204 lines" at 80×24, room for 6 rows, 5 drawn; "Last 17" at 80×40, 22 rows, a wrapped line taking two |
| `missing` | No file | "No log yet"; Refresh alone. Nearly unreachable today, `main()` making the file at every start (`cmd/hardn/main.go:57`); `next/p3-startup` moves that write (`_support/hardn-refactor-plan.md:86`) |
| `unreadable` | A directory at the path | "Can't read it: is a directory"; Refresh alone |
| `empty` | An empty file | "Empty"; Refresh alone. Nearly unreachable today: start-up has written to the file by the time the screen can read it (`cmd/hardn/main.go:57`, `:155`, `:185`), so only if hardn's open failed, the file was truncated while hardn runs, or under `next/p3-startup` |
| `control` | Colour, title and bell, a bidi override, a non-UTF-8 byte, typographic characters | `^[`, `^G`, `<U+202E>`, `<0xe9>`; `don’t “quote” ≥ 5 → … x` as it is in both glyph sets |
| `elsewhere` | hardn.yml's `logFile: /srv/log/hardn.log` | Reads `/var/log/hardn.log`, and says "hardn.yml's logFile, /srv/log/hardn.log, isn't used" |
| `alpine323` | BusyBox's less through a link (`vm-runs/s1/01-tools.log`) | No Open item (decision 7 (a)), the last lines alone |
| `no-pager` | No less | No Open item, the last lines alone |
| `read-pager` | Decision 8 (c), not recommended | Main menu 10 runs the pager at once and returns there; the golden shows the note and command |
| `read-pager-none` | The same with no less | Falls back to the tail screen, P-18's "otherwise the last N lines" |

| Logs listing mock | What it shows |
|---|---|
| `open` | Item 1, Open: the note and command, the screen drawn again |
| `pager-commands` | What each pager option would run: a root login with `PAGER=most`, sudo, sudo-rs [I] and doas dropping it, BusyBox's less, Alpine's less package [I, not run], sudo keeping it, no less |
| `cli-terminal-tail` | `hardn -p` to a terminal: no menu or prompt, room left for the shell's prompt |
| `cli-terminal-pager` | The same under decision 8 (c): note and command |
| `cli-pipe` (80+), `cli-pipe-control` (80+) | `hardn -p` off a terminal: every line as written, no heading, safe too; exit 0. `cat` keeps the bytes. Needs unit (d)'s move (decision 10) |
| `cli-missing`, `cli-unreadable`, `cli-unreadable-not-root` | stderr "hardn: no log yet: /var/log/hardn.log", exit 0; "hardn: can't read /var/log/hardn.log: is a directory", exit 1; under decision 9 (a), where the log is tighter than 0644, "...: permission denied" and "Run it with sudo.", exit 1 |
| `parser` | A real line as `GetLogs` reads it and as `parseLogLine` does |
| `origin` (80+), `origin-p` (80+), `origin-elsewhere` | origin/main's screen; `-p`'s whole stream as read, the config line, the clear sequence as text, the `~~~` line, the OS line and the log; and the error with `logFile` where nothing writes, in their words |

| Logs play golden | Keys | What it shows |
|---|---|---|
| `play/logs` | `play`: 10, 0, 0 | From the main menu and back |
| `play/logs-refresh` | `logs/lines`: 2, 0 | Refresh: the log gains a line (the fake's); also under LANG=C |
| `play/logs-plan-only` | `-sequence dry-run`: 10, 2, 0, 0 | Plan only: the same screen |
| `play/logs-open` | `logs/long`: 1, 0 | Open: the note and command |
| `play/logs-missing`, `-unreadable`, `-empty` | `logs/<host>`: 1, 0; 1, q; 0 | Each state, Refresh where it has one |
| `play/logs-control`, `-elsewhere` | `logs/<host>`: 0 | Opening and leaving |
| `play/logs-alpine` | `logs/alpine323`: 1, 0 | BusyBox's less: 1 is Refresh here |
| `play/logs-no-pager` | `logs/no-pager`: 1, 0 | 1 is Refresh here |
| `play/logs-read-pager` | `logs/read-pager`: no input | The note: nothing to answer |
| `play/logs-read-pager-none` | `logs/read-pager-none`: 0 | The fallback |
| `play/logs-hosts` | `logs`: 6, 0, 0 | The eleven hosts' menu, `control`, back |

## 4. Heights (run: `TestSystemDetailsHeights`, `TestLogsHeights`, Unicode and ASCII, at 80×24 and 80×40)

`TestSystemDetailsHeights` pins each System Details count. `TestLogsHeights` pins that every Logs screen ends with its prompt within the rows, and `long`'s "Last 3" and "Last 17"; the other Logs counts are counted from one listing run at each height.

| Screen | Lines to the prompt | 80×24 | 80×40 |
|---|---|---|---|
| origin/main's System Details (counted in `system_details_open`) | 50, of which the report 41; the screen clears first, so its top is gone | past | past |
| Report whole (`debian13`, `ubuntu2604`, `unreadable`, `no-logins`, `login-console`) | 44 | past: 20 scroll off | past: 4 scroll off |
| Report whole on `alpine323`, one Last Login row | 43 | past: 19 scroll off | past: 3 scroll off |
| Report whole with a Pool row and root alone (`pve9`, `zfs-degraded`) | 44 | past: 20 scroll off | past: 4 scroll off |
| Report whole with an Export item (`export-command`, `export-file`) | 45 | past: 21 scroll off | past: 5 scroll off |
| `many`: over `debian13`, 13 more Last Login rows, 13 more Users rows and 8 more addresses | 78 | past: 54 scroll off | past: 38 scroll off |
| `report-sections` | 16 | fits | fits |
| `report-pager` | 20 | fits | fits |
| `report-wide`, at 132 columns (counted from one `-width 132` run, no test pins it) | 30 | past | fits |
| origin/main's Logs (counted in `logs_open`) | 13 plus the file's lines, 1217 on the `long` host's log | past | past |
| Logs `missing`, `unreadable`, `empty` | 14 | fits | fits |
| Logs `lines`, 4 lines | at most the terminal | 23, "Last 3" | 25, all |
| Logs `long` | as many as the terminal holds: room for 6 rows, 5 drawn, or 22 | 23, "Last 3" | 40, "Last 17" |
| Logs `control`, 6 lines, one wrapping | at most the terminal | 24, "Last 5" | 25, all |
| Logs `elsewhere`, `alpine323`, `no-pager`, `read-pager-none`, 10 lines | at most the terminal | 24 | 34, 32, 32, 32 |

Each System Details form is the same number of lines at both heights: one form at every height, its top scrolling off, as the main menu's (DEV-052). A sub-menu has no compact form, and none is proposed. At 60 columns rows and items stack, a line each more (the goldens' `truecolor-dark-unicode-60` cells). Only `report-sections` and `report-pager` fit 80×24 (decision 4).

## 5. Decisions for the maintainer

In dependency order: a decision whose answer changes another decision, or another unit, comes first and says what it gates. Each stands on its own: what it is, each option and what it changes, the recommended one first and why, then the question. Later units: (b) builds the readers of the host and the log, (c) the two screens on the design system, and (d) moves main menu 9 and 10, `hardn system-details` and `-p` onto them and deletes the old code (§9). A name such as `screen-debian13` is a mock: `go run ./internal/tools/uigallery system-details screen-debian13` draws it, `logs screen-long` for a Logs mock, and §3 says what each shows. The mocks draw each recommended option unless a host names another. **Settled, so not asked:** the mode beside a sub-menu's heading (the layout has no such field, and no sub-menu shows it: below, Made here); that nothing here plans or journals (the plan's row: read only).

**Adopted (the maintainer, 2026-10-30):** "Adopt all eleven", the button for adopting all eleven recommended options and starting unit (b). Every recommended answer below is adopted, and the Made here calls stand as stated. What follows from them:

- §3.11's `system-details` change alone is approved, narrowed: `--json` and `-O json` become `--output json` with a warning, `-O text` warns, and `--yaml`, `-O yaml`, `-S` and `-E` are refused, exit 2. The rest of §3.11 stays unapproved (1).
- The report is P-17's seven sections, with the rows `screen-debian13` draws (2), whole, its top scrolling off a short terminal (4), with no Export item, `hardn system-details > file` saving it (5).
- Last Login is root's and each listed user's newest login with a terminal, from `lslogins`; a blank reads "None recorded" only where a source can be read and holds records (3).
- The screen and `-p` read `/var/log/hardn.log` always, and `logFile` is retired with a warning where it names another path (6).
- GNU `less` alone runs as root, `LESSSECURE=1`, `$PAGER` not read, with no Open item on BusyBox's. This accepts an interactive root program on this screen, asked for by a key (7).
- Logs opens on the last lines as written, this start's own among them (8).
- `-p` alone needs no root (9), and takes the forms and exit statuses of 10 (a).
- §6's words are decided, main menu 9 and 10 unchanged (11).
- The mocks of the options not adopted go with unit (b).

### System Details

**1. The five flags of `hardn system-details`, and the one row of the plan's §3.11 that covers them.** *Gates* decision 5's `--output json` line, the JSON form, unit (b)'s `details` field in `pkg/output` and unit (d)'s work on those flags; *blocks* those two pieces of work; the rest of unit (b) and unit (c) go ahead.

`hardn system-details` takes `-E/--export`, `-S/--section`, `-O/--format`, `--json` and `--yaml` (`pkg/cmd/system_details_cmd.go:38-42`). They were meant to export, filter and format; the code that used them is commented out (`:95-121`), so each is accepted and does nothing: `-O nonsense` is no error and `-E out.txt` writes no file (`pkg/characterization/testdata/golden/menu/system_details_flags.golden`). The plan's slice row names this as a defect to fix and §3.11 as what it waits on. §3.11 is one row of the plan's table of surface changes (`_support/hardn-refactor-plan.md:857`), holding eight changes, and of this it says "`system-details -E/-S/-O/--json/--yaml` → `--output json` + warning". It is not approved: `cmd/hardn/output.go:4`, "§3.11 is not approved, so no --output, --yes or --confirm-timeout flag". So no `--output` exists anywhere, and no JSON reaches the command line (`docs/reference/output.md`, its Status). None of the five flags is in a release: v0.3.2 has no `system-details` ([interaction.md](../refactor/phase0/interaction.md):38). As a user other than root, which this command allows, `main()` prints two unrelated lines to stdout first (`pkg/logging/logging.go:40`, `cmd/hardn/main.go:62`; read, not run), so JSON on stdout would not parse until unit (d) moves them to stderr, which it does under (a) and (b) (`cli-not-root`).

- (a) **Approve this one change of §3.11 now, narrowed from its row,** the other seven left unapproved. `--output json` then exists on `hardn system-details` alone, printing the document `cli-json` draws. Two forms become it with a warning: `--json` and `-O json`. One warns: `-O text`. Three are refused, exit 2: `--yaml` and `-O yaml`; `-S`, as nothing filters; `-E`, saying "use > file", as a write as root is what decision 5 (a) avoids (`cli-flags`). This departs from the row, which sends all five to `--output json` with a warning, because those three never worked and no release holds them, and turning them into JSON would again accept a flag and do something else.
- (b) **The row as written** (not mocked): every form of the five becomes `--output json` with a warning. Literal to the plan and to the brief's rule, "add the new form and deprecate the old one with a warning" (`_support/hardn-refactor-brief-handoff.md` §4, :200). But `--yaml` then prints JSON, `-S disk` the whole document and `-E out.txt` no file, each saying so only in the warning.
- (c) **Not now.** The five stay parsed and ignored until §3.11 is decided whole (`cli-flags`, "Until 3.11 is decided"). Slice 1 ends with this defect open, P-17's "Exports go through `--output json`" waits, and unit (b) builds no JSON.
- (d) **Remove the five now, with no `--output`** (not mocked). A flag given is cobra's unknown-flag error. It ends the false acceptance and adds no public contract, but there is no machine-readable form.

*Recommended:* (a), as it closes the slice's listed defect, `--output json` first meets a command that changes nothing, and the document already exists and is tested. By the brief's rule (a) falls short on three flags and (d) on all five; both rest on no released user holding them, the row's own note saying these changes are "all unreleased since v0.3.2". If you hold to the rule whole, take (b).

**Question: approve §3.11's `system-details` change alone now, narrowed as `cli-flags` draws it?**

**2. Which sections and rows the System Details report shows.** *Gates* decisions 3, 4 and 11, the JSON form's `details` and unit (b)'s reader; *blocks* unit (b).

System Details has no Figma frame and is not in v0.3.2, so its content is yours to set. P-17 ([proposals.md](../refactor/proposals.md#p-17)) proposes "OS and kernel, host and IPs, users, CPU and load, disk, memory, last login". origin/main's report has the same groups with no headings (`pkg/system/display.go:43-129`; `pkg/characterization/testdata/golden/menu/system_details_open.golden`).

- (a) **P-17's seven, as drawn** (`screen-debian13`). Compared with origin/main, **left out:** Domain, Client IP, DNS IP n, Hypervisor, CPU Freq, the bar graphs (no design-system component draws one), and the User row. **Added:** IPv6 addresses ([I] for `inet6`'s form: no VM has a global one). **Changed:** memory by MemAvailable, disk in GiB, used always of total, the pool's health as `zpool` says it, "CPUs" for what origin/main's Cores row counts. Each row reads a fact or says it could not. Whether Last Login stays is decision 3.
- (b) **The seven and origin/main's rows** (`origin` draws them in its words): at least five more lines. Hypervisor and CPU Freq need their readers fixed first, as they read "Bare Metal" with no `lscpu` and "0.00 GHz" on the usual host (`pkg/characterization/testdata/golden/menu/system_details_no_lscpu.golden`, `system_details_open.golden`).
- (c) **Fewer sections** (not mocked), say without Users and Last Login: closer to a screen, but it loses the two that bear most on who can reach the host.

*Recommended:* (a), as Hypervisor and CPU Freq read wrong today, DNS has main menu 3's own screen, and Domain and Client IP say little about how the host is hardened.

**Question: adopt P-17's sections and the rows `screen-debian13` draws, leaving Last Login to decision 3?**

**3. What the Last Login section shows, and where it reads from.** *Depends on* 2 (a) or (b); *gates* decision 4's counts, the JSON form's `logins` and unit (b)'s login reader; *blocks* that reader alone.

P-17 ([proposals.md](../refactor/proposals.md#p-17)) lists "last login"; origin/main reads one from `lastlog -u` for the current user (`pkg/system/collectors.go:498`), so root's under sudo. The tools it assumed are gone. What ran on the VMs (logs in `_support/hardn-audit-2026-10-16/vm-runs/s1/`):

- **Debian 13 and Ubuntu 26.04:**
  - No `last`, `lastb`, `lastlog` or `wtmpdb` (`01-tools.log`).
  - `lslogins` is there, util-linux 2.41.5 and 2.41.3. `lslogins --time-format=iso --noheadings --raw --output=USER,LAST-LOGIN,LAST-TTY,LAST-HOSTNAME -l root,alice` answers a line per name it knows: `alice 2026-10-20T11:42:05+00:00 pts/0 192.0.2.10`, `root   ` (the name and three spaces) for no login, and nothing for a name it does not know, exit 0 (`02-login.log`, `08-lslogins.log`).
  - The same as a user and as root where its files are readable (`02-login.log`).
  - Newest only, so in a login session a person's own row is that session (`02-login.log`).
  - Only logins with a terminal: `ssh host cmd` moved nothing (`02-login.log`), though sshd's journal holds it (`08-lslogins.log`).
  - sshd prints its own `Last login: <the session before> from <addr>` at an interactive login (`07-banner.log`), so the login before this one already reaches the person.
  - A rotation does not lose the answer: with `--wtmp-file=/dev/null` the same login came from its other records (`08-lslogins.log`). The order is wtmp, lastlog2 where built with it, then `/var/log/lastlog` (read: `login-utils/lslogins.c`, v2.41).
  - With its sources missing, empty or unreadable it answers blanks, exit 0 (`09-lslogins-blank.log`): a blank is not proof of no login.
  - About 0.9 s a call on these VMs' 34 to 36 MB wtmp, for one name or two, as a user or root; 0.005 s with `--wtmp-file=/dev/null` (`08-lslogins.log`, `09-lslogins-blank.log`). These VMs' wtmp gains a `LOGIN` record every 10 seconds; a usual host's time was not measured.
- **Alpine 3.23:** no `lslogins` or `lastlog`, and BusyBox's `last -n 1 <user>` prints its usage, exit 1, `last` alone its header (`01-tools.log`). `/var/log/wtmp` and `/run/utmp` stay at 0 bytes after a terminal login (`08-lslogins.log`). sshd's own `Accepted` lines are in `/var/log/messages`, 0640 root wheel (`09-lslogins-blank.log`). `sshd -T` shows no printlastlog line, and at an interactive login no `Last login:` line and no prompt matched in 6 s (`07-banner.log`).
- **Proxmox VE 9:** taken to be as Debian 13, its base [I, no VM].

The options:

- (a) **Root's and each listed user's newest login with a terminal, from `lslogins`** (`screen-debian13`). One call. The Users section lists UID 1000 and up, so other accounts below 1000 have no row. What it adds over sshd's own line is the other accounts, root's most: a terminal login you did not expect. Your own row mostly says now. A login with no terminal does not show (`ssh host cmd` ran; scp and sftp [I]), so "None recorded" says what it says and is not proof. Unit (b) reads "Not known" on a blank where its sources cannot be read or hold nothing, as on Alpine with `lslogins` added [I]. As root it takes the password lock for each account: `lckpwdf()` before `getspnam` (read: `lslogins.c`, v2.41; up to 15 s if another tool holds it, read from glibc, not run; `/etc/.pwd.lock` already exists on both VMs, `08-lslogins.log`). On Alpine, one row, "Logins  Not known: lslogins isn't installed" (`screen-alpine323`). Counts: 44 lines on Debian 13 with one user; 13 more rows with 14 users (`screen-many`'s 78 also holds 13 more Users rows and 8 more addresses).
- (b) **sshd's own log** (not mocked). Every ssh login, with a terminal or not, on all three VMs: `Accepted publickey for <name> from <addr> port <n> ssh2`, in the journal on Debian and Ubuntu (`journalctl -g 'Accepted ' _COMM=sshd-session`, `08-lslogins.log`) and in `/var/log/messages` on Alpine (`09-lslogins-blank.log`). As root only; log lines to parse, in two places; gone when the log rotates; no console logins; its time not measured. The only option that shows logins with no terminal.
- (c) **Only whoever ran hardn, their newest login, from `lslogins`** (not mocked). One row, 43 lines, but it needs `SUDO_USER`, `DOAS_USER` or `USER`, and in an ssh session it mostly says now, which adds nothing.
- (d) **Whoever ran hardn, the login before this session, by hardn reading `/var/log/wtmp` itself** (not mocked). A binary reader: parsed here on amd64, 384-byte records (`05-wtmp-*.tail`); arm64, a released target (`.slsa-goreleaser/linux-arm64.yml`), has another layout [I: glibc's headers, not run]. Rotation empties the file (Ubuntu monthly, keeping 1; `06-wtmp-source-ubuntu26-vm.log`). Debian's sshd also writes `wtmp.db`, so the format is moving [I]. And sshd already prints this on both.
- (e) **Have hardn's install add wtmpdb and libpam-wtmpdb** (not mocked). A package and a PAM change on the host, the install unit's and not this read-only slice's.
- (f) **Drop the section,** leaving six (not mocked; 39 lines on Debian 13, by count, which fit 80×40). The smallest reader. sshd's line still gives the person their previous login on Debian and Ubuntu; nothing then says who else logged in.

*Recommended:* (a), as it alone tells the person something sshd's line does not with no package and no log parsing. Its limit is that it sees only logins with a terminal. Choose (b) if those without one must show, (f) if a partial answer is not worth a line per account.

**Question: show root's and each listed user's newest login with a terminal, from `lslogins`?**

**4. How the report fits an 80×24 terminal.** *Depends on* 2 and 3; *gates* unit (c)'s layout, and if (c) is chosen decision 7 covers the report too; *blocks* unit (c).

With decision 3 (a)'s rows the report is 44 lines on Debian 13 and Ubuntu 26.04, 43 on Alpine, 44 on a pool with root alone and 78 on the `many` host (`TestSystemDetailsHeights`). On Debian 13 it runs past 80×24 by 20 lines and past 80×40 by 4; under 3 (f) it is 39 lines, which fit 80×40 and not 80×24. origin/main's runs past both too, 50 lines, after clearing the screen (`pkg/characterization/testdata/golden/menu/system_details_open.golden`). The maintainer withdrew the main menu's compact form on 2026-10-23, so the main menu's top scrolls off a short terminal, though it fits 80×40 (35 lines over a host's eight rows; [`docs/design-system/README.md`](../design-system/README.md) §9.12).

- (a) **The report whole**, its top scrolling off (`screen-debian13`): no extra keystroke, and the same form as `hardn system-details`. At 80×24 the first 20 lines need the terminal's scrollback: all of OS and Kernel, Host and IPs and Users, and CPU and Load's heading and Model, so the view opens at "CPUs" under no heading, the prompt always last. 21 of the 44 lines are the seven headings and their gaps.
- (b) **A line per section as items** (`screen-report-sections`, 16 lines; `report-sections-open`): fits, but a key to see any detail, and items for what is not an action.
- (c) **A summary and a pager** (`screen-report-pager`, 20 lines; `report-pager-open`): fits, but needs an interactive child as root (decision 7) just to read a report.
- (d) **The same rows under fewer headings** (not mocked): up to 21 lines fewer, 23 lines on Debian 13 with no heading at all, which fits 80×24, but rows such as "Used" then need their section's name to mean anything, and P-17's sections go.

Two columns from 132 columns up are drawn (`report-wide`, 30 lines) but not offered: past the 64-cell cap, a new layout, and at 80 columns it is (a).

*Recommended:* (a), as it has one form on and off the screen and adds no key. It takes the main menu's precedent further than that case went, as this one runs past 80×40 too.

**Question: draw the report whole, its top scrolling off a short terminal?**

**5. Whether the screen has an Export item.** *Depends on* 1; *gates* unit (c)'s items; *blocks* unit (c).

origin/main's screen has "2) Export Information" (`pkg/menu/system_details_menu.go:70-80`). It asks a file name, default `system_status.txt`, and writes it **as root, relative to the working directory, mode 0644, over any file of that name, holding users and addresses** (`system_details_menu.go:137-139`, `:231`; `pkg/utils/utils.go:139`; `pkg/characterization/testdata/golden/menu/system_details_export_default.golden`, `system_details_export_overwrite.golden`; [side-effects.md](../refactor/phase0/side-effects.md)'s row 31, the same write). P-17 says "Exports go through `--output json`".

- (a) **No Export item** (`screen-debian13`). `hardn system-details > file` saves the report, and `--output json` too if decision 1 (a) or (b) is taken. As a user other than root the file also holds `main()`'s two lines until unit (d) moves them to stderr (`cli-not-root`). Run through sudo or doas it does not.
- (b) **An item that says how** (`screen-export-command`, `export-command-open`): "To save it, run: hardn system-details > report.txt". Writes nothing, and teaches the form; one item more, for one line of words.
- (c) **An item that writes** (`screen-export-file`, `export-file-open`): a full path only, the file 0600. This is a change on the host as root, **outside the slice's agreed read-only row unless you change that scope**: atomic write, path checks and plan only's refusal ("Not available in plan only.") come with it.

*Recommended:* (a), as the shell already does this with the user's own rights, and the slice's row says read only.

**Question: drop Export, leaving `hardn system-details > file` to save the report?**

### Logs

**6. What hardn.yml's `logFile` key means, now that hardn writes a fixed path.** *Gates* decisions 8 and 10, and unit (b)'s reader; *blocks* unit (b)'s reader of the log, and unit (d).

`logFile` looks like the log's path: `hardn.yml.example:7`, `logFile: "/var/log/hardn.log"     # Log file path`. It has never set where hardn writes, in v0.3.2 either (`v0.3.2:cmd/hardn/main.go`, :55): `main()` opens `/var/log/hardn.log` before flags are parsed (`cmd/hardn/main.go:57`), and `pkg/output/audit.go:22` records "The logFile key is only read". What reads it: the Logs screen and `-p`, so with it set elsewhere a line just written is not shown, or another file's old lines show as hardn's (`pkg/characterization/testdata/golden/menu/logs_other_path.golden`, `logs_other_path_stale.golden`); and the "Check the log file at" line after Run All (`cmd/hardn/main.go:269`, `pkg/menu/run_all_menu.go:278`).

- (a) **Read the file hardn writes; retire the key by a warning.** The screen and `-p` read `/var/log/hardn.log`. A hardn.yml that sets `logFile` elsewhere is still accepted, and the screen says so: "hardn.yml's logFile, /srv/log/hardn.log, isn't used" (`screen-elsewhere`). Unit (d) adds the warning when hardn.yml loads, as for `permitRootLogin` (`cmd/hardn/output.go:87`; not mocked), **only where the key names another path**, so hardn's own example file, which names `/var/log/hardn.log` (`hardn.yml.example:7`, `pkg/config/embed.go:19`), draws none. It fixes both "Check the log file" lines, and drops the key from `hardn.yml.example`, `pkg/config/embed.go:19` and `docs/configuration.md:131`.
- (b) **Honour the key for writing.** `main()` would have to know it before opening the log, but the configuration loads after the root check (`cmd/hardn/main.go:155`), so the later unit that moves hardn's start-up writes out of `main()` comes first, and that itself waits for the install-script unit (`_support/hardn-refactor-plan.md:86`): slice 1 would wait too. A hardn.yml, which may be `./hardn.yml` (`pkg/config/config.go:210`), would name a path hardn creates and appends to as root. The run log's path and `docs/reference/output.md` §4 change with it.

Leaving the key read as today keeps P-18's defect, so it is not offered.

*Recommended:* (a), as P-18 proposes "Read the file hardn actually writes", the key has never done what its comment says, and the warning meets the brief's rule for a config key.

**Question: read `/var/log/hardn.log` always, and retire `logFile` with a warning where it names another path?**

**7. Whether a pager runs as root, and which.** *Gates* decision 8: under (e) the screen has no Open item and 8 (c) cannot be chosen; decision 10, whose terminal form under 8 (c) is the pager; and unit (b)'s interactive seam, which is new. *Depends on* decision 4 only if 4 (c) is chosen. *Blocks* decision 8 and that seam, and accepts a risk on the user's behalf.

A pager is an interactive program with shell escapes. By then hardn is root (`cmd/hardn/main.go:148`), and hardn can already write `/etc/sudoers.d`, so whoever reaches the screen is already root in effect [I]. What a pager adds is an **unlogged interactive shell**, rather than new privilege [I]. Run as a user, not as root (`_support/hardn-audit-2026-10-16/vm-runs/s1/`): GNU `less` 668 on Debian 13 and Ubuntu 26.04, without `LESSSECURE`, ran a shell on `!echo X` (`03-less-*.log`), and `E` and `:e` prompt Examine; with `LESSSECURE=1`, `!`, `|`, `E`, `:e` and `v` each say "Command not available" (`04-less2-*.log`); `s` says "Input is not a pipe" on a file, with or without it (hardn passes a file; piped input not run). **Alpine's is another program,** `/usr/bin/less -> /bin/busybox` (`01-tools.log`), and `LESSSECURE=1` changes nothing there: `s` prompts "Log file", `E` and `:e` prompt "Examine", and `!`, `|` and `v` do nothing (`04-less2-alpine323-vm.log`). No path was typed at `s`, so the write did not run; `E` did try a name that does not exist, answering "Cannot read this file" (`03-less-alpine323-vm.log`). That `s` writes any path as root and `E` and `:e` open any readable file is read from BusyBox's [`miscutils/less.c`](https://github.com/mirror/busybox/blob/master/miscutils/less.c), master (the VM runs v1.37.0, not compared): `:1554-1566`, `:1742`; `:1212`, `:1746`, `:1288`. It reads `LESS` alone from the environment (`:1836`). `host.Real`'s environment is exactly `PATH` and `LC_ALL=C` (`pkg/host/real.go:48`), so nothing is inherited unless a new seam passes it, and `Run` pipes output and gives no terminal (`pkg/host/real.go:87`, `:137`), which is more than P-18's "Effort: S" suggests [I]. Under sudo `env_reset` would drop `PAGER` [I; HARDN_CONFIG's loss was seen, [vmtest.md](../safety/vmtest.md) §4.9; not run for `PAGER`]; sudo-rs takes `env_keep` from a drop-in as sudo does ([vmtest.md](../safety/vmtest.md):315), so the drop and a kept `PAGER` apply to it the same way [I; not run for `PAGER`]; under doas only what `setenv` or `keepenv` keeps [I; [sudoers.md](../safety/sudoers.md):102]; a root login keeps root's own. A rule or `sudo -E` keeping `PAGER` makes hardn run whatever it names as root. `pager-commands` tables each case. No golden runs a pager.

- (a) **GNU `less` alone, `LESSSECURE=1`, `$PAGER` not read; no Open item on BusyBox's** (`pager-commands`, "pager-less"; `screen-alpine323`). Someone who prefers another pager uses `cat /var/log/hardn.log | theirs`, as themselves. On Alpine, the screen is the last lines alone unless the `less` package is installed, when Open is there as elsewhere ("doas, the less package" [I, not run]).
- (b) **The same, with Alpine requiring the `less` package** (not mocked): Open everywhere, escapes off everywhere [I for that package, not run], but one package more on Alpine, which is the install unit's to add, not this read-only slice's.
- (c) **The same, accepting BusyBox's `less` as it is** (`pager-commands`, "pager-env", the doas row, unsecured): Open everywhere with no package, and no shell escape on the VM, but as root on Alpine the person at the screen can overwrite any file with what they are reading (read; the write not run).
- (d) **P-18 as written, `$PAGER` then `less`** (`pager-commands`, "pager-env"). The user's own tool, but hardn then runs as root whatever the variable names, and that program's own escapes are outside `LESSSECURE` [I].
- (e) **No pager at all** (`screen-no-pager` on every host). No interactive child and no new seam, the smallest unit (b). The screen is the last lines alone, and the whole log is `less /var/log/hardn.log` in the user's own shell. P-18's pager is not adopted.

*Recommended:* (a), as it keeps the whole log one key away where the secure mode exists, and takes no program name from the environment into a root process. Choose (e) if an interactive root child on this screen is more than you want to accept.

**Question: accept GNU `less` alone as root, escapes off, `$PAGER` not read, with no Open item on BusyBox's?**

**8. How the Logs screen opens: the last lines, or the pager at once.** *Depends on* 6 and 7; *gates* decision 10 and unit (c)'s screen; *blocks* unit (c).

origin/main prints the whole file at once: 20,000 of 20,000 lines in `pkg/characterization/testdata/golden/menu/logs_large.golden`, `$PAGER` not run. P-18 proposes "Open the log in `$PAGER`/`less` when available; otherwise show the last N lines", which the brief also says (`_support/hardn-refactor-brief-handoff.md`:421, the Bubbles `viewport` row). The log is never rotated (`pkg/output/audit.go:22`). Whatever the form, every start of hardn writes to the log before the screen can read it: "INFO: Using configuration from: /etc/hardn/hardn.yml" and "SUCCESS: debian trixie detected" with a hardn.yml, three lines with none (`pkg/config/config.go:246`, `:252`, `:279`; `pkg/osdetect/osdetect.go:93-95`; held in `_support/hardn-audit-2026-10-16/vm-runs/s3/57-log-ubuntu26-vm.log:116-117`, `s3/97-log-alpine323-vm.log:7-8`, `s2/58-alpine-example.log`; [menu-live.md](menu-live.md):180). The tail always ends with this session's own lines.

- (a) **A screen of the last lines as written, as many as the terminal holds** (`screen-lines`, `screen-long`, `open`). That is room for 6 rows at 80×24, 5 drawn on `screen-long`, and 22 at 80×40, 5 rows of the 24 going to the File section, and a line over 58 cells wrapping onto two, as many real lines do (116 of the 250 in `s3/57-log-ubuntu26-vm.log`): on `screen-long`, "Last 3" at 80×24, of which two are this start's, and "Last 17" at 80×40. After two starts in a row, 80×24 holds start-up lines alone. An item, Open, reads the whole log where decision 7 gives one, and Refresh reads again. No child runs unless asked, no line is hidden, and the missing, empty and unreadable states each have words (`screen-missing`, `screen-empty`, `screen-unreadable`). It differs from P-18, whose pager comes first.
- (b) **The same, leaving out this start's own lines** (not mocked): unit (b) notes where the log ended before this start wrote [unverified how, as `main()` opens it first], so the rows show what came before. The rows say more, but a log view hides lines the file holds, and an earlier start's lines still fill them.
- (c) **P-18 as written:** main menu 10 runs the pager at once and returns there; the tail screen only with no pager (`screen-read-pager`, `screen-read-pager-none`, `cli-terminal-pager`). One key to the whole log, with search. But the screen differs by host, and a root pager runs on every 10. Not with decision 7 (e).

*Recommended:* (a), on the verified points that no process runs as root unless asked for and that nothing the file holds is hidden. That most visits want the last lines is [I], and does not rank it; at 80×24 they are few, and Open or a taller terminal is what shows more.

**Question: open Logs on the last lines as written, this start's own among them?**

**9. Whether `-p` needs root.** *Gates* decision 10 and where unit (d) puts `-p`; *blocks* those alone. The main menu's root check is not in question.

The two screens are reached from the main menu, behind the root check (`cmd/hardn/main.go:148-152`), and stay so. `hardn system-details` has no root check and can read most of the report as any user (`cli-not-root`: `/etc/sudoers.d` is `drwxr-x--- root root` on the three VMs, so a user cannot even list it, and Alpine's `/etc/doas.conf` is 0640 root; `_support/hardn-audit-2026-10-16/vm-runs/s1/01-tools.log`). `-p` sits after the root check, the config load and `detectOS` (`:155`, `:185`), though the log is opened asking 0644 (`pkg/logging/logging.go:38`) and is `-rw-r--r-- root root` on all three VMs (`01-tools.log`; a tighter umask not run). So there the check protects nothing: any user can `cat` it, and `-p` under another user fails with "This script needs to be run as root." (`TestDryRunAloneIsNotRefused` holds `-n -p` reaching it.)

- (a) **`-p` alone needs no root**, and `hardn system-details` stays as it is. Unit (d) moves `-p` ahead of the root check, where no other operation flag is given, so it no longer gets hardn.yml's offer either; `-n -p`'s row in that test changes on purpose. Where the log is tighter than 0644 it says "hardn: can't read <path>: permission denied" and "Run it with sudo." (`cli-unreadable-not-root`). Until unit (d) moves them to stderr, `main()` still prints its two unrelated lines to a user other than root first (`cmd/hardn/main.go:57-62`).
- (b) **Leave both as they are:** no change to `cmd/hardn`'s order, and a reading flag that refuses what `cat` allows.
- (c) **Root for both:** one rule, "hardn needs root", at the cost of the report as another user. It is unreleased, so no release breaks.

*Recommended:* (a), as a check that guards nothing on a usual host costs the user a sudo for each read.

**Question: let `hardn -p` run without root when it is the only operation?**

**10. What `hardn -p` prints.** *Depends on* 6, 7, 8 and 9; *gates* unit (d)'s change to `-p`; *blocks* that alone.

`-p`/`--print-logs` is a released flag (v0.3.2, help "Print logs"), so this changes a surface users have. Today, to a terminal or not, its stdout is (read, not run; §2.9): the config line ("[INFO] Using configuration from: <path>" or the no-config lines), the clear sequence and the `~~~` line even in a pipe, "[SUCCESS] <os> detected", then "# Contents of <path>:", the file whole and raw, and a blank line (`origin-p`; `pkg/config/config.go:246`, `pkg/osdetect/osdetect.go:50`, `:93-95`, `pkg/logging/logging.go:138-146`; `pkg/characterization/testdata/golden/menu/logs_print_flag.golden` holds the last part alone). So a script today already has more than a heading line to skip, and a clear sequence in what it saves. Those config and OS lines also go into the log on each `-p`. It reads `cfg.LogFile`. On a read failure it logs "[ERROR] Failed to read log file ...", which also writes a line into the log, and exits 0 (`pkg/characterization/testdata/golden/menu/logs_print_flag_missing.golden`; `cmd/hardn/main.go:417`, exit read, not run). That is the class of defect V25 names, a command exiting 0 after a failure, whose fix is decided as Phase 3's exit codes.

- (a) **As drawn.** To a terminal: the file's rows and the last lines the terminal holds, no menu, no prompt, room for the shell (`cli-terminal-tail`); under 8 (c), the pager (`cli-terminal-pager`). Off a terminal: every line as written, no heading, control characters made safe, as a pipe may end at a terminal, with `cat` for the bytes (`cli-pipe`, `cli-pipe-control`). No log: stderr "hardn: no log yet: <path>", exit 0 (`cli-missing`). Unreadable: stderr "hardn: can't read <path>: <reason>", exit 1 (`cli-unreadable`, `cli-unreadable-not-root`). Nothing is written to the log. **This needs unit (d) to run `-p` ahead of the config load and `detectOS`, or with a silent console and no clear** (decision 9 (a) does the first where `-p` is the only operation flag); under 9 (b) or (c), the silent console. With another operation flag `-p` stays after it (below, Made here), so that operation's lines stay on stdout. A script that skips today's lines breaks.
- (b) **Every line always, to a terminal too** (the lines of `cli-pipe`; the terminal form not mocked), with the same move: one rule and "print" means print, but 20,000 lines to a terminal stays, which is the slice's defect on the flag's path.
- (c) **Today's form,** reading the file hardn writes, exit 1 on a failed read: no surface change but the exit, but the raw escapes and the clear in a pipe stay (`pkg/characterization/testdata/golden/menu/logs_escape.golden`).

*Recommended:* (a), as a pipe gets every line and nothing else, a terminal gets what fits, and raw bytes never reach a terminal by way of hardn.

**Question: adopt `-p`'s forms and exit statuses as option (a) says?**

**11. Words: the two screens', and main menu items 9 and 10.** *Last:* depends on 1 to 10; *blocks* unit (c)'s words.

§6 lists every word. Main menu 9 is "System Details" "View system information" and 10 is "Logs" "View log file" in origin/main, in hardn today (`pkg/menu/main_menu.go:768-769`), in the gallery's fixture (`pkg/ui/screen/fixture.go:71-72`) and in the brief's main-menu sketch (`_support/hardn-refactor-brief-handoff.md`); the proposal on the main menu's words, P-07, proposes nothing for them. Both stay true under every option above.

- (a) **§6 as drawn, main menu 9 and 10 unchanged.** The Logs screen's title becomes "Logs" (origin/main's "View Logs"), the main menu's label.
- (b) **§6 with your changes:** name the row, and the page and mocks follow.

*Recommended:* (a), as the words are the shortest in the user's terms, and nothing about items 9 and 10 needs them changed.

**Question: adopt §6's words, leaving main menu 9 and 10 as they are?**

### Made here (engineering, stated; say if you'd rather otherwise)

- **No pause, a sub-menu:** both screens end at a menu with no "Press Enter", which is [P-28](../refactor/proposals.md#p-28) (a) with [P-09](../refactor/proposals.md#p-09) dropping the pause here, both still proposed; the design-system guide lists these two as waiting on that choice ([`docs/design-system/README.md`](../design-system/README.md) §9.12). SSH_LOGIN-SSH_SETTINGS_DISPLAY's own choice stays open. Refresh is what makes a menu worth having here: with none, the prompt would read "[0 or q]". Under P-28 (b) they would need a fourth layout, a display ending in a pause.
- **Refresh stays** on both screens: "1) Refresh" reads again and draws again, in place of origin/main's recursion, and 0 then 9 would be more keys.
- **A value too long for its row:** off a terminal nothing is cut; on a terminal the row cuts, as every screen's does (`KeyValue`'s doc, `pkg/ui/component/keyvalue.go:104`). A report saved to a file must hold what it read: `hardn system-details > file` holds each value whole. Today nothing in `pkg/ui` lays out uncapped, so the mocks still draw the cut on `pve9`, a 40-cell CPU name in a 37-cell column (`screen-pve9`, `cli-pipe-cut`), and unit (c) adds a new path through the components for the plain, off-terminal form, a deviation to log. origin/main never cuts (`pkg/characterization/testdata/golden/menu/system_details_render.golden`). Alternatives: a value too long goes under its key whole everywhere, which changes a component every screen uses; or cut everywhere, which leaves only `--output json` with the whole value.
- **Plan only:** both screens open and read as live, Refresh too, since nothing is written. The mode is not shown beside the heading: `SubMenu.Heading` is "with no mode beside it" (`pkg/ui/screen/sub.go:58`), only the main menu renders it (`pkg/ui/screen/main.go:194`), and no other sub-menu does (`play/backup-plan-only`, `play/environment-main-plan-only`). `plan-only-indicator` shows the alternative, not proposed.
- **A section that cannot be read** says which and what, and the rest stays (`screen-unreadable`, `TestSDUnreadableKeepsTheRest`), where origin/main loses the report.
- **ZFS:** the dataset is what `/proc/self/mounts` has on `/`, never one of hardn's; health is `zpool list -H -o health <pool>`; total is used plus available (`TestSDZFSDatasetIsRead`). Every form [unverified], no ZFS VM. Not ONLINE draws in the warning's state.
- **Figures:** memory by MemAvailable; both sizes in GiB, as the division is 1024³. "CPUs" counts `/proc/cpuinfo`'s `processor` lines, which are logical CPUs, not cores.
- **Lines of the log are code, as captured output is** ([`docs/design-system/README.md`](../design-system/README.md) §9.1, "Code"; `logRows`, `codeRows`, `TestLogRowsKeepTheCharacters`): no bullet, from the label column, typographic characters kept in both glyph sets, never cut, a line too long breaking after the wrap glyph. The row components would map or strip them (`pkg/ui/component/ctx.go:284`), and a section's rows are a `BulletList` alone, so the gallery draws them in that place (`logScreen`), and unit (c) needs a section that holds code, a deviation to log. What a terminal would act on is made safe, not stripped (`safeLine`, `screen-control`, `TestSafeLine`), showing what a line held. WARNING and ERROR take the warning's colour, a past error not being a state; the rest take the output's, muted.
- **The parser:** `parseLogLine` is the fix, used only for that colour (`TestParseLogLine`). `GetLogs`, its wrappers and `utils.PrintLogs` have no live caller and go with unit (d).
- **`hardn system-details` off a terminal** is plain mode, no colour, no clear-screen sequence, no prompt (`cli-pipe`).
- **The JSON form, if decision 1 (a) or (b):** on `pkg/output`'s document, not beside it: `--output json` is one flag with one contract (`docs/reference/output.md`), mode `check` already means read and change nothing, and `SchemaVersion`'s comment lets a field be added without a new version (`pkg/output/wire.go:5-10`). So the nine fields and `details`, on this form alone: `pkg/output/output_test.go:98-104` holds exactly nine for every other document, and the audit log's lines are the same document (`TestSDJSONIsTheDocument`). `DETAILS_UNREADABLE` is proposed, not registered.
- **`-p` with another operation flag** prints where it does today, after the others (`cmd/hardn/main.go:416`).
- **Slice units:** the plan's (b2) stays, in its smallest form (§9).

**Also, not mocked:** follow and search on the screen (`less` has search under 7 (a)); rotation of the log, which `next/p7-install` owns (`docs/reference/output.md` §4).

## 6. Words

origin/main's where it has them; every other word is **decided** with decision 11 (§5, adopted 2026-10-30), unless its row says otherwise.

| Where | Words | Source |
|---|---|---|
| Main menu 9, 10 | "System Details" "View system information"; "Logs" "View log file" | origin/main's, unchanged |
| Title bars | "System Details"; "Logs" | the main menu's labels; origin/main's first, its second "View Logs" |
| Menu heading, prompt | "Select an option"; "Enter selection" | **decided**, Backup's; origin/main's |
| System Details sections | "OS and Kernel", "Host and IPs", "Users", "CPU and Load", "Disk", "Memory", "Last Login" | P-17's, capitalised |
| Rows | "OS", "Kernel", "Uptime", "Hostname", `<interface>`, "Model", "CPUs" ("4, 1 socket", "48, 2 sockets", "4"), "Load (1, 5, 15 min)", "/" ("/dev/vda1 ext4"), "Used" ("4.2 of 38.6 GiB (11%)"), "Pool rpool" (ONLINE, DEGRADED), `<user>` (sudo, doas, "No sudo", "No doas"), `<account>` ("Oct 20 11:42 from 192.0.2.10", "Oct 20 11:42 on tty1", "Oct 20 2025 from 192.0.2.10", "None recorded", "Not known"), "Logins" | proposed |
| States | "No addresses", "None but root", "None recorded", "Not known", "Not known: lslogins isn't installed", "Can't read <source>", "Not root: <file> not read" | proposed |
| Items | "Refresh" "Read the host again"; "Return" | proposed; origin/main's "Refresh Information" "Reload system status from system" shortened; its "Return" |
| Decision 4 (b), (c) | "Summary"; "Open" "The whole report in less"; a section's one line: "Debian 13 (Trixie)", "web-01, 192.0.2.20/24 (+8)", "alice", "14 users", "None but root", "4 CPUs, load 0.12", "11% of 38.6 GiB", "alice, Oct 20 11:42", "None recorded", "Not known"; "Unknown" for a section not read | not recommended |
| Decision 5 (b), (c) | "Export" "Say how to save it"; "To save it, run: hardn system-details > report.txt"; "Export" "Write it to a file"; "Path to write"; "Use a full path, such as /root/system-details.txt."; "Written to /root/system-details.txt, 0600." | not recommended |
| Plan only, under 5 (c) | "Not available in plan only." | **decided** ([dryrun-replace.md](dryrun-replace.md)) |
| Logs sections | "File"; "Last N of M lines", "All N lines", "1 line" | proposed |
| Logs rows | "Path", "Size" ("49 KiB", "<n> bytes", "<n> MiB"), "No log yet", "Empty", "Can't read it: <reason>", "hardn.yml's logFile, <path>, isn't used" | proposed |
| Logs items | "Open" "The whole log in less"; "Refresh" "Read the log again" | proposed |
| `-p`'s stderr | "hardn: no log yet: <path>"; "hardn: can't read <path>: <reason>"; "Run it with <tool>." | proposed |
| Flags, under 1 (a) | "Today", "All five", "Parsed and ignored"; "Proposed, if 3.11 is approved", "--output json, with a warning", "Accepted, with a warning", "Refused, exit 2", "Refused: use > file"; "Until 3.11 is decided", "Parsed and ignored, as today" | the mock's summary; unit (d) writes the warnings' words, proposed |
| The JSON form | `details`, `logins`, `unknown`; the code `DETAILS_UNREADABLE`; its detail "<section> not read" | proposed |
| The gallery's, dev-only | "System Details hosts", "Logs hosts", "Select a host", "Enter your choice", each host's label, about and note; "The gallery runs no pager. hardn would run ..."; `pager-commands`' and `parser`'s headings and rows | the gallery's |

## 7. Defects

**No V-number holds any of these but where the table names one.** The "[V]" in P-17 and P-18 means verified, not a V-number. [side-effects.md](../refactor/phase0/side-effects.md)'s table, [p0-hazards.md](../refactor/p0-hazards.md) and the plan were searched.

The slice's three:

| Listed | Verdict | V-number | Evidence |
|---|---|---|---|
| No-op `system-details` flags | **Confirmed.** All five accepted, none acts. **Not corrected:** waits on decision 1 | none (interaction.md:79; §3.11) | run: `system_details_flags` (the syntax tree on this Mac; see §2.6) |
| Logs dump | **Confirmed.** The whole file at once, no pager, tail or count. **Corrected in the mocks** (`screen-long`, `read-pager`), `$PAGER` not read under 7 (a) | none (P-18 [V]; interaction.md:180) | run: `logs_open`, `logs_large`, `logs_print_flag` |
| Broken log parser | **Confirmed, and dead:** no entry has a level, a second line becomes an entry, a line over 64 KiB ends the read silently; no live path calls it. **Corrected in the mocks** (`parseLogLine`), for colour alone | none | run: `logs_parser`; `parser`, `TestParseLogLine` |

Others found:

| # | Defect | V-number | Evidence |
|---|---|---|---|
| 4 | Box fixed at 50 columns, a longer line past it | none (P-17 [V]) | run: `system_details_render`; `pkg/system/display.go:32` |
| 5 | The renderer does I/O; nothing can place or test it | none (P-17) | run: `system_details_render`; `boxes.go:584` |
| 6 | The screen clears itself each draw, the title twice; `hardn system-details` and `-p` clear even in a pipe | none (P-17) | run: `system_details_open`, `system_details_flags`; `-p` read, not run: `cmd/hardn/main.go:185`; `osdetect.go:50` |
| 7 | One collector's error loses the report; printed twice; "check permissions" whatever it was | none | run: `system_details_error`; `details.go:91-117` |
| 8 | Hard-coded ZFS dataset: another dataset reads 0.00/0.00 and "HEALTH O.K." | none (P-17 [V]) | run: `system_details_zfs_dataset`, `_zfs_other_dataset`; `details.go:85` |
| 9 | The ZFS branch may never run: "is healthy" is not in "all pools are healthy" | none | run: `system_details_zfs_all_healthy`; zpool's words [I, not run on a ZFS host] |
| 10 | ZFS "Volume" is used/available, beside used/total without ZFS | none | run: `system_details_render_zfs`, `_zfs_dataset` |
| 11 | "CPU Freq: 0.00 GHz" | none | run: `system_details_open`; on a host [I] |
| 12 | Memory counts page cache as used | none | run: `system_details_open`; `collectors.go:329` |
| 13 | Disk says GB dividing by 1024³; memory says GiB | none | run: `system_details_open`; `collectors.go:430-432`, `:370` |
| 14 | Last Login is root's under sudo; blank with no `lastlog`, which stock Debian 13 and Ubuntu 26.04 have not, or a dotless From | none | read: `collectors.go:498`; run: `system_details_no_lastlog`, `_login_hostname`, `vm-runs/s1/01-tools.log` |
| 15 | No `lscpu`: "Bare Metal", 1 socket: unknown reads as fact | none | run: `system_details_no_lscpu` |
| 16 | IPv4 alone | none | read: `os_host_info_repository.go:161`, `collectors.go:186` |
| 17 | Export relative to the working directory, as root, 0644, holding users and addresses | none (side-effects row 31; P-17 [V]) | run: `system_details_export_default`, `_named`; `utils.go:139` |
| 18 | Export overwrites with no question or backup; a failure is not asked again | none | run: `system_details_export_overwrite`, `_fails` |
| 19 | The screen and the export are built apart, and differ | none | run: `system_details_export_default` |
| 20 | Each draw reads everything again, four `GetHostInfo`; each option recurses | none | run: `system_details_open`, `_refresh`; `system_details_menu.go:102`, `:115`, `:128` |
| 21 | `logFile` diverges from the file hardn writes: a line just written not shown; another file's lines shown as hardn's; "Check the log file at" names a file not written | none (P-18 [V]; phase0-findings.md:428, :650 **V**) | run: `logs_other_path`, `_stale`; read: `cmd/hardn/main.go:57`, `:269`, `pkg/menu/run_all_menu.go:278` |
| 22 | A terminal escape in a line reaches the terminal raw, as root. The printing is origin/main's; that commands' output puts such bytes in the log is the trunk's | none | run: `logs_escape`; `file_logs_repository.go:96`; the trunk's: `pkg/host/runlog.go:58-62`, `logging.go:131-135` |
| 23 | `-p` exits 0 on a read failure | V25's class; no V-number names `-p` | run: `logs_print_flag_missing`; exit read: `cmd/hardn/main.go:417` |
| 24 | A failed read writes an ERROR line into the log | none | run: `logs_print_flag_missing`; `logging.go:141`, `:78` |
| 25 | `-p` needs root and `DetectOS` for a file asked for as 0644, and prints their lines, the clear sequence among them, before the log | none | read: `cmd/hardn/main.go:148-152`, `:155`, `:184-188`; `TestDryRunAloneIsNotRefused` |
| 26 | Empty log: no words, though it needs hardn's open to have failed or a truncation, as start-up has written by then. Missing log: Stat's words, the path three times | none | run: `logs_empty`, `logs_missing` |
| 27 | Three copies of the print, one with no caller | none | run: `logs_parser`, last block; `pkg/utils/utils.go:117` |
| 28 | `main()` opens the log, as a user other than root prints two unrelated lines on stdout, before `hardn system-details` starts, so `> file` holds them | V26; side-effects row 1 | read: `cmd/hardn/main.go:57-62`, `logging.go:38-40`; `cli-not-root` |
| 29 | The log is 0644; **on the trunk** it holds commands' output (origin/main's log has no run log), where the audit log is 0600 for holding plans. Noted: left as `docs/reference/output.md` §4 has it, not this slice's to change | none | read: `logging.go:38`, `runlog.go:58-62`, `pkg/output/audit.go:30-33` |

4 to 27 go with the legacy code in unit (d). The mocks correct them, but 9, which they avoid, as the stand-in does not ask `zpool status -x`, and 25, which is decision 9. 28's stdout is unit (d)'s, as §9 says, and the write's move `next/p3-startup`'s. **Not corrected by this slice:** 29.

## 8. What the mocks take from real code, and what not

**Real:** the host's facts, `detect.Detect`; OS, hostname and uptime, the header probe (`pkg/engine/header`'s `Read`, `output.HeaderViewOf`, `output.FormatUptime`); users, `accounts.Read`; the JSON document's nine fields, `output.Build`; the layout, `screen.SubMenu`, and every component; `host.TailLines` to split the file (`pkg/host/runlog.go:112`); `host.SearchPath` to find `less`.

**Not, the gallery's own, each a stand-in for unit (b)'s work** (all gallery-local, nothing added to live code):

| Stand-in | File | What unit (b) builds instead |
|---|---|---|
| `readDetails`: `readAddrs` (`ip -o addr show scope global`), `readUsers` (`accounts.Read`, UID ≥ 1000 not nobody, group membership), `readCPU` (`/proc/cpuinfo`, `/proc/loadavg`), `readDisk` (`/proc/self/mounts`, `df -kP /`, `zpool list`, `zfs get`), `readMemory` (`/proc/meminfo`), `readLogins`, `parseLogin` (`lslogins`) | `systemdetails.go:556-824` | A section-by-section reader that keeps going on error, on the forms the VMs printed (`vm-runs/s1/01-tools.log`, `02-login.log`; `zpool` and `zfs` not run, and no VM printed `/proc/loadavg`, `inet6` [I] or `df`'s header), on a host with **no run log**, as `statusHost` is (`cmd/hardn/status.go:29`, against `engineHost`'s and `confirmHost`'s `RunLog: revertFileLog{}`, `engine.go:41`, `confirm.go:38`), with a test that a draw adds no log line, as `TestSystemDetailsWritesNothing` runs on a fake with no such log. Users read as the users check reads them (`pkg/engine/controls`), as groups miss sudoers rules such as cloud-init's. Decision 3's source, in one call, as its cost is wtmp's per call, its time on a usual host still to measure: a failed or unparsed answer never reads as no login, and an account with no line reads "Not known" (`TestSDLastLogin`). A blank is "None recorded" only where a source can be read and holds records, else "Not known", which the stand-in does not check. As root it takes the password lock for each account. With no `zpool` or `zfs` on a ZFS root, as in an LXC guest [I, not run; no ZFS VM], fall back to `df`, the pool's row saying it is not known, where the stand-in loses the section ([unverified]). Where `/proc/cpuinfo` has no `model name` or `physical id`, as on arm64, a released target (`.slsa-goreleaser/linux-arm64.yml`) [I, not run], Model says "Not known" and no socket count is made up (`TestSDBlankCPU`); "CPUs" are its `processor` lines, logical CPUs |
| `detailsModel`, `withDetails`, `ipLine`, `alpineIPLine`, `cpuinfo` | `systemdetails.go:225-314`, `:333-356` | Nothing: what the fake answers |
| `detailsDocument`, `detailsJSON` | `systemdetails.go:1381-1431` | Under decision 1 (a), the `details` field in `pkg/output`, and `--output` on the command |
| `report`, `offTerminalCtx` | `systemdetails.go:1082-1094` | The uncapped plain form (§5, Made here), in `pkg/ui` |
| `readLog` | `logs.go:283-310` | A reader of the end of the file: `host.System`'s `ReadFile` reads it whole and the log is not rotated |
| `safeLine` | `logs.go:329-351` | A sanitiser in `pkg/ui` or beside the screen |
| `parseLogLine` | `logs.go:365-377` | The same, if a level is still wanted for colour |
| `logRows`, `codeRows`, `logScreen` | `logs.go:442-491`, `:556-577` | Unit (c): a section that holds code, in `pkg/ui`, a deviation to log |
| `pagerFor`, `pagerChoice`, `pagerTable`, the fake's `less` | `logs.go:183-188`, `:239-256`, `:382-436`, `:774-796` | Under decision 7 (a) to (d), a seam that runs an interactive child with a terminal and a chosen environment, which `host.System` lacks, and that leaves the prompt reader consistent after: `commandEnv` is exactly `PATH` and `LC_ALL=C` (`pkg/host/real.go:48`), so it must also pass `TERM` from hardn's own environment and a locale for non-ASCII, and tell GNU's less by a positive check, `less --version` having printed `less 668` on GNU's and refused on BusyBox's (`vm-runs/s1/01-tools.log`), failing closed; the stand-in goes by the link, and where it cannot tell chooses none; the choice |
| `sdHosts`' and `logHosts`' treatments (`report`, `export`, `read`) | `systemdetails.go:179-214`, `logs.go:192-218` | Nothing: retired with the mocks |
| The words of §6 | both | Unit (c) |

**Live helpers each later unit needs** (not added here):

- **(b):** the two readers above, on `host.System`, with no run log; a tail read; the interactive seam (decision 7), with `TERM` and a locale; `pkg/output`'s `details` and its code (decision 1); what has not run: `less` under hardn and as root, Alpine's less package, `lslogins`' time on a usual host, a real rotation, `lslogins` as root for more than one account, `zpool` and `zfs`, a tighter umask and the arm64 `/proc/cpuinfo` where one can be run.
- **(c):** `pkg/screens/systemdetails` and `pkg/screens/logs` on `screenkit`, with no engine; `hardn system-details`' and `-p`'s forms rendered off a terminal, nothing cut (§5, Made here); a section that holds code.
- **(d):** see §9.

## 9. Not in this unit

- **(b) The readers,** with their own goldens, as §8; no control on the engine and no `Check`, as neither screen has a main-menu row or a change. Decisions 1, 2, 3, 6 and 7 shape it. A ZFS root with no `zpool` is named in §8.
- **(b2) Parity:** the plan's every-slice unit, smallest here: origin/main's goldens (`menu/system_details_*`, `menu/logs_*`) against the new forms by row, each difference tied to a decision or defect of this page. `internal/parity` compares end states (`internal/parity/parity.go:1-8`), and under decision 5 (a) there is only "nothing written" to hold, against origin/main's export, which is decision 5's difference. It needs a reader for these goldens' layout.
- **(c) The screens** on the design system, retiring these mocks, with the off-terminal forms.
- **(d) The legacy path replaced:** main menu 9 and 10 to the new screens; `hardn system-details` and its flags (decision 1); `main()`'s two prints to stdout moved to stderr (`pkg/logging/logging.go:40`, `cmd/hardn/main.go:62`), so that as a user other than root `> file` and `--output json` hold only the report, which does not wait for the start-up unit's move of the log's write; `-p` (decisions 9 and 10), ahead of the config load and `detectOS` or on a silent console, so that its pipe form holds the log alone; the `logFile` warning, the "Check the log file" lines and the example file (decision 6); then, after (b2), `pkg/menu/system_details_menu.go`, `pkg/menu/logs_menu.go`, `pkg/system`, the log manager, service, model, port and repository, the lastlog adapter and `utils.PrintLogs` deleted, each with its grep (`HostInfoManager` also serves the main menu, `pkg/menu/main_menu.go`, so what stays is that grep's), the goldens frozen as the parity record.
- **On a Linux VM:** the new characterization and gallery tests, as they have run on macOS alone. Proxmox VE 9 and a ZFS host have no VM, so that release, `zpool` and `zfs` stay [unverified] without one.
- **Rotation of the log,** `next/p7-install`'s. **The write's move,** `next/p3-startup`'s (`_support/hardn-refactor-plan.md:86`).

## 10. Size

**This unit.** Non-test Go: 2,576 lines added, 9 removed (`systemdetails.go` 1,598, 257 of them whole-line comments; `logs.go` 895, 181 comments; `play.go` 45, `main.go` 38). Tests: 2,040 lines (`menu_system_details_test.go` 710, `menu_logs_test.go` 513, `systemdetails_test.go` 494, `logs_test.go` 319, `host_info_test.go` 4). Testdata: 113 new golden files (33 characterization, 53 listing, 27 play), 20,400 lines; none changed. This page. Over the guideline of about 500 changed lines, as earlier units (a) were: two screens, the options of decisions 4, 5, 7 and 8 drawn.

**Against Backup** (lines added, a proxy, not time or cost; Backup's as gallery-environment.md §10 measured them). Backup's unit (a): 1,268 non-test, 1,186 test, 7,105 golden lines in 45 files. So this unit is about **2.03** of Backup's non-test lines, **1.72** of its tests, **2.87** of its golden lines. Backup's slice came to 4,253 non-test lines: (a) 1,268, the shared extraction 322, the control 776, parity 431, the screen 1,304, the replacement 152.

**The earlier estimate of about 0.55 for the slice cannot be restated on this proxy:** unit (a) alone is already 0.61 of Backup's whole slice, most of it mocks that retire. For units (b) to (d), measured only in what exists:

- No shared extraction: `screenkit` and `internal/parity` exist (Backup's 322 lines).
- No control on the engine, no plan, no flow: Backup's control unit and most of its screen's flow have no counterpart.
- The stand-ins themselves: readers 431 lines (`systemdetails.go:552-824` 273, `logs.go:279-436` 158), rows and screens 421 (`systemdetails.go:845-1078` 234, `logs.go:438-624` 187), together 852, about 0.29 of Backup's 2,985. Not a forecast: no tests, VM fixes, command wiring or `details` field are in it, and it holds the pager choice that decision 7 (e) would drop.
- Unmeasured, and not small: the interactive seam (decision 7), the uncapped form and a section holding code (§5, Made here), a tail read, whatever login source decision 3 chooses.
- The legacy to delete is larger than Backup's: up to 1,888 lines in 15 files (`pkg/system` and its login package 1,112, `system_details_menu.go` 245, `system_details_cmd.go` 127, `lastlog_command_adapter.go` 129, the Logs layers 275), less what its grep keeps, against Backup's 763 in six.

So units (b) to (d) look smaller than Backup's, with decisions 1, 3 and 7 moving them most.
