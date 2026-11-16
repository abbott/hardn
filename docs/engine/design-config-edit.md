# Editing hardn.yml from the menu, and two first-run questions: design

Status: **adopted 2026-11-15.** The maintainer adopted every recommended answer of §4 ("adopt all"), with one addition, decision 13. §4 is kept as the record.

## 1. What is there

- **Configuration is read-only.** Main menu 8 opens a menu: 1) hardn.yml, 2) DNS Settings, 3) NTP Settings, 4) Syslog Settings (`pkg/ui/screen/configgroup.go:13-64`). Item 1 shows the file's path and a row per key; its menu has Return alone (`pkg/ui/screen/config.go:48-62`, `:162-171`).
- **Four screens save their own keys** through `steps.PlanSave` (`pkg/engine/steps/save.go:102-151`): only the edited lines change, the diff is shown, then asked; the file is backed up. A file that is a link, changed since read, or not editable in place is refused.

| keys | saved by |
|---|---|
| `ufwAppProfiles`, `ufwTrustedInterfaces` | Firewall (`pkg/screens/firewall/actions.go:270`, `:383`) |
| `nameservers` | DNS (`pkg/screens/dns/actions.go:136`) |
| `enableUnattendedUpgrades`, off only: no screen saves `true` | Auto Updates (`pkg/screens/updates/actions.go:33-37`) |
| `enableBackups`, `backupPath` | Backup (`pkg/screens/backup/actions.go:106`, `:129`) |
| `checkReleases` | the startup question only (`cmd/hardn/releaseask.go:28-36`) |

- **No screen saves the rest:** `username`, `adminNoPassword`, `sshKeys`, `sshPort`, `disableRootSSH`, `sshConfigFile`, `enableUfwSshPolicy`, `configureDns`, `syslogServers`, `ntpServers`, `kernel`, `dryRun`, `confirmTimeout` (`pkg/config/config.go:33-113`).
- **Already decided** (`docs/engine/gallery-config.md` §3, decision 5): the editing unit covers only keys no other screen saves; `adminNoPassword` stays a hand edit. `syslogServers`, `ntpServers`, `kernel` and `checkReleases` came later and are in no list.
- **A save names its keys**, so `-n`'s dry run and `-u`'s username, held in memory, are never written (`docs/engine/config-edit.md` decision 12).
- **A dry run stops every change at its plan, saves too** (`pkg/screens/screenkit/screenkit.go:86-87`, `:214`).
- **With no hardn.yml read, every save is refused**: `CONFIG_NOT_LOADED`, "create /etc/hardn/hardn.yml, then try again" (`save.go:107-110`).
- **When a saved key acts** (`pkg/engine/runall/runall.go:233-310`):

| key | acts |
|---|---|
| `disableRootSSH`, `enableUfwSshPolicy`, `configureDns`, `kernel` | at Run All only |
| `username`, `sshKeys` | Run All; the Users screen's defaults (`pkg/screens/users/actions.go:366-375`) |
| `sshPort` | nowhere on the host: Run All compares it with sshd's ports (`runall.go:270-274`); the SSH screen's port field starts from it (`pkg/screens/ssh/actions.go:198`) |
| `ntpServers`, `syslogServers` | Run All; "Set servers" in NTP Settings and Syslog Settings |
| `confirmTimeout` | same session (`cmd/hardn/engine.go:287-290`) |
| `checkReleases` | at the next start: the main menu sets the check up once (`cmd/hardn/hooks.go:36`, `cmd/hardn/release.go:71-81`) |
| `dryRun` | at the next start (`cmd/hardn/main.go:268`); in memory it is the session's mode, not the file's value (`pkg/config/dryrun.go`) |

- **First run.** With no hardn.yml, on a terminal, hardn asks "create one?", then five questions: username, SSH port, automatic updates, an SSH key, the release check (`pkg/config/config.go:399`, `:434-497`). No answer is checked or asked again: port 99999 is saved (`config.go:453-461`). The whole file is then written (`config.go:485`).
- **Checks that exist:** `NTPServers.Resolve` (1 to 4, IPv4 or a host name, no IPv6, none twice; `pkg/config/timesync.go:29-53`); `SyslogServers.Resolve` (1 to 4; `server` alone gives tcp, port 514; `pkg/config/logforward.go:59-143`). Nothing parses "host:port".
- **DNS is different from NTP and syslog.** It needs two keys: Run All sets `nameservers` only where `configureDns: true` (`runall.go:280-284`). Its screen already saves the key and has Cloudflare, Google and Quad9 (`docs/usage.md:27`). hardn refuses to set DNS on every EL distribution, in a container, and under NetworkManager, resolvconf or cloud-init (`pkg/engine/controls/dns/dns.go:359-362`, `:510-515`). None of DNS, NTP, syslog counts in the risk level (`docs/engine/risk.md:6`, `:66`).

## 2. What is asked for

The maintainer, 2026-11-15:

1. The roadmap's "Edit hardn.yml from the menu" (`docs/roadmap.md:20`).
2. First run also asks for an NTP server address and a syslog server address.
3. Evaluate asking about DNS too. He left it out: "not particularly related or impactful to hardening".

## 3. Mocks

Proposed words throughout. The hardn.yml screen, its rows as today, with one item; then the keys it edits:

```
  Select an option

   1) Edit a setting       Change a key in hardn.yml

   0) Return

    > Option [0-1 or q]: 1

# Edit a setting ###############################################

   1) username             george
   2) sshKeys              1 key
   3) sshPort              2222
   4) disableRootSSH       true
   5) enableUfwSshPolicy   true
   6) configureDns         true
   7) syslogServers        None
   8) ntpServers           None
   9) kernel               false
  10) confirmTimeout       3m
  11) checkReleases        false
  12) enableUnattendedUpgrades  false

   0) Return

    > Option [0-12 or q]: 3
```

One value: checked, the edited lines shown, then asked:

```
# sshPort ######################################################

  Enter SSH port [2222]: 99999
  (x) Not a port between 1 and 65535
  Enter SSH port [2222]: 2022

   -  Save the configuration
      /etc/hardn/hardn.yml
      @@ -3,3 +3,3 @@
       username: george
      -sshPort: 2222
      +sshPort: 2022
       disableRootSSH: true

  Save sshPort? [y/N, 0 or q]: y

  [+] Save the configuration  Done

# Operation Successful

  Backup of /etc/hardn/hardn.yml kept in
  /var/backups/hardn/20261115T120000Z-1a2b3c4d

  sshd's port is not changed. Change it under SSH (2).
```

A list (a syslog server asks its protocol and port after the address):

```
# ntpServers ###################################################

   -  Server               time.example.net

   1) Add server
   2) Remove server

   0) Return

    > Option [0-2 or q]: 1

  Enter NTP server: 2001:db8::1
  (x) An IPv6 address, which hardn does not set yet
  Enter NTP server: 192.0.2.10

   -  Save the configuration
      /etc/hardn/hardn.yml
      @@ -12,2 +12,3 @@
       ntpServers:
         - time.example.net
      +  - 192.0.2.10

  Save ntpServers? [y/N, 0 or q]: y

  [+] Save the configuration  Done

  Set them under NTP Settings, or with Run All.
```

A refused save (the words are `save.go`'s):

```
# Warning
# /etc/hardn/hardn.yml changed since it was read. Start again,
# so that the file is read as it is now.

  Nothing was saved.
```

First run, with a refused answer:

```
No configuration file found. Would you like to create one? [Y/n]: y
Enter default username (optional, press Enter to skip):
Enter SSH port [22]:
Enable automatic security updates? [y/N]:
Add SSH public key (optional, press Enter to skip):
Enter NTP server (optional, press Enter to skip): 2001:db8::1
An IPv6 address, which hardn does not set yet.
Enter NTP server (optional, press Enter to skip): time.example.net
Enter syslog server (optional, press Enter to skip): 192.0.2.20:1514
Check for new hardn releases, once a day? [y/N]:
Created configuration file at /etc/hardn/hardn.yml
A complete example configuration with all options is available at /etc/hardn/hardn.yml.example
Run All sets the NTP and syslog servers.
```

## 4. Decisions for the maintainer

Editing (1 to 7), then first run (8 to 12). Gates come first in each part.

1. **Which keys Configuration edits. GATES 2 to 7.**
   - (a) *Recommended:* the keys no screen saves, plus `checkReleases`: `username`, `sshKeys`, `sshPort`, `disableRootSSH`, `enableUfwSshPolicy`, `configureDns`, `kernel`, `confirmTimeout`, `checkReleases`, and `enableUnattendedUpgrades` (Auto Updates saves only its `false`); and `ntpServers`, `syslogServers` by decision 2. Keys another screen saves keep that screen as their one editor, and its checks (`backupPath`'s: `pkg/engine/controls/backup/backup.go:213-235`).
   - (b) Every key here. Two editors for seven keys; this one would have to reuse each screen's checks.
2. **Where `ntpServers` and `syslogServers` are edited. GATES 4's list forms and 9's closing line.** Their screens apply them but say "No servers in hardn.yml" (`pkg/ui/screen/timesync.go:49`, `logforward.go:52`).
   - (a) *Recommended:* here, in this unit. The list editor is built once, for `sshKeys` too. After a save, one line says where to set them.
   - (b) Add and Remove items in NTP Settings and Syslog Settings, as DNS has. The better home, but two more screens change, each with its goldens. A later unit could move them.
3. **Keys that stay read-only.**
   - (a) *Recommended:* `adminNoPassword` (decided: a deliberate opt-in); `dryRun` (in memory it is the session's mode, so saving it from the menu would switch the running session; and under a dry run no save is written, so it could never be turned off here; a hand edit); `sshConfigFile` (this revises `gallery-config.md` decision 5, which listed it for editing: it only finds v0.3.x's old file for Migrate, `pkg/host/sshd/migrate/migrate.go:139-142`; nothing checks it at load); `logFile`, `permitRootLogin` and the 33 retired keys (nothing uses them, `pkg/config/retired.go:44-60`). `sshPort` is editable, with the line in §3: saving it never changes sshd.
   - (b) `sshPort` read-only too. No one can make hardn.yml disagree with sshd from the menu, but the SSH screen does not save it either, so it stays a hand edit.
4. **How a value is entered and checked.** (a) *Recommended:* one item, "Edit a setting", then the keys numbered (§3). Each answer is checked before the diff, and a refused one is asked again.

   | type | keys | entry | check |
   |---|---|---|---|
   | yes/no | `disableRootSSH`, `enableUfwSshPolicy`, `configureDns`, `kernel`, `checkReleases`, `enableUnattendedUpgrades` | nothing typed: the diff, then "Save kernel: true?" | none |
   | number | `sshPort` | one field | 1 to 65535 |
   | text | `username`, `confirmTimeout` | one field; empty removes the value | the form of a user name alone (`pkg/engine/controls/users/create.go:64-66`), never "already exists": the usual value is a user Run All made; `revert.ParseTimeout` and the 60 s floor (`pkg/host/revert/revert.go:68`, `:226`) |
   | list | `sshKeys`, `ntpServers` | Add or Remove, one entry a save | `authkeys.ParsePublic` (`pkg/host/authkeys/authkeys.go:158`); `NTPServers.Resolve` on the whole list |
   | syslog servers | `syslogServers` | Add asks the address, protocol [tcp], port [514 or 6514], and the CA file for tls; Remove by number | `SyslogServers.Resolve` on the whole list |

   A key in `sshKeys` is listed by type, comment and fingerprint, never whole. `username` starts from the file's value, not `-u`'s.
   (b) One form of every key at once. One save, but one bad field holds up the rest, and lists do not fit a form.
5. **Save only, or also apply.**
   - (a) *Recommended:* save only. Then one line says where it acts (§1's table): "Run All applies it." for `disableRootSSH`, `enableUfwSshPolicy`, `configureDns`, `kernel`, `username`, `sshKeys`; the two server lists name their screen; `sshPort` says sshd is unchanged; `checkReleases` "From the next start."; `confirmTimeout` acts at once and says nothing. The save is made from the file's values, so `-u`'s name and `-n`'s mode are never written.
   - (b) Offer to apply after the save. Each key needs its control's plan here; it is Run All, one row at a time, and those screens exist.
6. **In a dry run.** `-n`, or the file's `dryRun: true`.
   - (a) *Recommended:* plan only, like every save: the diff is shown and nothing is written.
   - (b) Saves are written in a dry run. hardn.yml is not the host, but every other screen's save stops at its plan.
7. **No hardn.yml read.**
   - (a) *Recommended:* no item; the screen says "Start hardn again to create one." The first-run code writes the whole file outside the safe save, so run from the menu it would write under `-n`, save `-u`'s name, and ignore the path `-f` names. Creating from the menu is its own unit.
   - (b) An item "Create hardn.yml" that runs the first-run questions. Needs those three faults solved first.
   - (c) Write the defaults with no questions, then edit. `checkReleases: false` is written unasked.
8. **DNS at first run. GATES 12.**
   - For: a host with a wrong resolver can't reach an NTP or syslog server given by name, or its updates; one more optional line.
   - Against: (1) it is two keys, and `configureDns: true` makes Run All replace the host's nameservers (`dns.go:1-3`); a wrong but well-formed address then breaks names, updates and both servers above. (2) hardn refuses to set DNS on every EL distribution, in containers, and under NetworkManager, resolvconf and cloud-init, so on many hosts the answer could never be applied. (3) The full check needs the host's DNS manager (`pkg/host/dns/config.go:51-75`), not known at first run. (4) DNS Settings already asks, saves, and has three presets. NTP and syslog have no screen that saves. (5) Its weight in the risk level is none. That is true of NTP and syslog too, so it is the least of the five.
   - (a) *Recommended:* don't ask. The host's DNS works as it is at first run; changing it is a choice for DNS Settings, where the host is read first.
   - (b) Ask, and write `nameservers` and `configureDns: true`. (c) Ask, and write `nameservers` alone: Run All ignores it; only "Configure DNS" in DNS Settings uses it.
9. **Whether first-run answers are applied. GATES 10 and 11's words.**
   - (a) *Recommended:* they only fill hardn.yml. Run All then lists NTP and Syslog rows (`runall.go:179-195`), and each screen's "Set servers" sets them. The last line says so (§3), only where one was given.
   - (b) Apply at once. First run would change the host before the menu has shown anything, and may install rsyslog (`pkg/engine/controls/logforward/plan.go:159`).
10. **The NTP question.**
    - (a) *Recommended:* "Enter NTP server (optional, press Enter to skip): ". One address; Enter skips; checked with `NTPServers.Resolve`; refused in one line and asked again. More servers are a menu edit (decision 2).
    - (b) One to four, separated by spaces or commas. A typed comma for a dot (`time.example,net`) would then be saved as two servers, unnoticed.
11. **The syslog question.**
    - (a) *Recommended:* "Enter syslog server (optional, press Enter to skip): ". One server, `host` or `host:port`; tcp; port 514 unless given. Checked with `SyslogServers.Resolve`. Needs a small new parser: the whole answer is tried as an address first, so an IPv6 address gets the IPv6 refusal, then the last colon is the port's. udp and tls are a menu edit (decision 2).
    - (b) The host alone: no parser, always 514.
    - Either way, where the host's logger is busybox syslogd (Alpine's default) tcp is refused when the servers are set (`plan.go:185-194`), and the Syslog Settings screen says so then. First run does not read the host, so it can't say it.
12. **Order and length.** Seven questions after "create one?" (eight with DNS).
    - (a) *Recommended:* flat, in §3's order: NTP and syslog after the SSH key, the release check last. Both new questions skip with Enter, so a gate saves no one a keystroke.
    - (b) "Set NTP and syslog servers now? [y/N]" first. One Enter instead of two for those who skip; one question more for those who don't.

13. **A syslog server the host's logger can't use.** *Added by the maintainer, 2026-11-15:* "we should disclose this within the '8) Configuration' menu". Where the host's logger is busybox syslogd and hardn.yml lists a tcp or tls syslog server, Configuration says so: a line under `syslogServers` on the hardn.yml screen, and the same line in its editor after a save, in the words Syslog Settings already uses for that refusal, with where to change it. Read here as the hardn.yml screen and the editor; the logger is read as Syslog Settings reads it. Where the logger can't be read, nothing is said.

## 5. Not in this unit

- Checking the four older first-run answers (username, port, key). Port 99999 is still saved.
- Editing keys another screen saves; `adminNoPassword`; `sshConfigFile`; `dryRun`.
- Creating hardn.yml from the menu (decision 7).
- Applying a key from this screen (decision 5).
- The SSH screen saving `sshPort` when it changes sshd's port.
- `hardn config` (the printed view) is unchanged.

## 6. Checks when built

- Each key in decision 1: saved, only its lines change, comments kept, the screen redrawn from the saved file.
- `config.Edit` on `syslogServers`: add, remove, the last one removed. No test covers it today (`pkg/config/edit_test.go:102-112` tests `ufwAppProfiles` only).
- A key the file does not hold, set and set back to its default.
- Under `-u alice`, an edit of another key leaves `username` as the file had it; under `-n`, nothing is written.
- Each refusal: the file a link, changed since read, a `<<` merge, no hardn.yml.
- A bad port, timeout, key, NTP server and syslog block: refused under the field, asked again, nothing saved.
- First run: Enter at both new questions writes neither key; a bad answer is asked again; end of input ends the series; `host:port` gives that port; the release question is still last. `pkg/config/releaseask_test.go:52` and the answer strings in `pkg/config/input_test.go` change.
- Docs: `docs/configuration.md:26`, `:271`, `:287`, `:322`; `docs/install.md:26`; `docs/engine/design-release-ask.md:10`; `docs/engine/screen-config.md` ("editing is a later unit", "kernelSettings"); `docs/roadmap.md:20` removed.
- `make check` on Linux, both arches, and the first-run series on a real terminal.

## 7. Built

2026-11-15, `next/config-edit`. First run (decisions 8 to 12): `pkg/config/config.go`, `firstrun_test.go`. Editing (1 to 7, 13): `pkg/screens/config/edit.go`, `pkg/ui/screen/configedit.go`, `cmd/hardn/readers.go`. Every save is `steps.PlanSave` of the one key, through `screenkit.Session.Change`, as Backup's and DNS's. Choices made where this page is silent, or where the kit draws otherwise than §3:

**Editing**

1. **A refused save is the flow's report**, as on every screen: "Operation Failed", the refusal and its code, "To go on: ...". §3's "Warning ... Nothing was saved." is not drawn.
2. **The prompts carry the kit's hint:** "Enter SSH port [2222] [0 or q]:". A form of several fields numbers them ("1. Enter syslog server").
3. **`enableUnattendedUpgrades` keeps its value on its line** ("12) enableUnattendedUpgrades  false"), as §3 draws it: `component.Menu` would otherwise stack every item.
4. **A list's rows sit under a section,** "Servers" or "Keys": the kit draws no rows without one. Remove shows only where the list has an entry.
5. **Field words.** "Enter username (Enter for none)", "Enter confirm timeout (Enter for 3m)", "Enter SSH public key", "Enter NTP server", "Enter syslog server", "Enter protocol [tcp]", "Enter port [514]" (6514 for tls), "Enter CA file", "Select a key to remove", "Select a server to remove".
6. **Refusals.** §3's for a port and IPv6; "Not an IPv4 address or a host name"; "Not a time of 1m or more, such as 90s or 5m"; "Not an SSH public key hardn takes"; "This key is in sshKeys already" (a warning); "Not udp, tcp or tls"; the user-name form's words from User Management; the rest are `Resolve`'s own, without the number of the entry typed ("The same address and port as server 1", "TLS needs caFile"); a fault of an entry the file already holds keeps its number ("Server 1: tls needs caFile").
7. **TLS with an IPv4 address is refused at the protocol field,** not at the CA file, so that the field asked again is the one to change.
8. **A syslog block holds only what differs from the defaults:** no `protocol: tcp`, no `port: 514` (or 6514 under tls).
9. **An empty answer writes `username: ""` and `confirmTimeout: ""`,** which load as not set and the default; the key's line stays. The last entry removed leaves `[]`.
10. **A value typed as it already is plans nothing:** "Nothing needs changing."
11. **After a save:** decision 5's lines. `enableUnattendedUpgrades`, which decision 5 does not list, says "Run All applies it.". The SSH line is "sshd's port is not changed. Change it under SSH (2)."
12. **`-u`.** Saving `username` writes the name typed and the file's name becomes it (`config.Save` naming `username` now updates `FileUsername()`); the session keeps `-u`'s name. Where `-u` gave the file's own name, the session takes the name saved.
13. **The diff shows an added SSH key whole,** as it shows every line it writes. The list and the menu never do.
14. **Decision 13's line:** "busybox syslogd is UDP only. Under Edit a setting, remove the server and add it with udp." The editor has no change of a server's protocol, and refuses the same address and port twice, so the line says remove, then add. "busybox syslogd is UDP only" is Syslog Settings' (`logFwdDescUDP`). It is a warning row under `syslogServers` on the hardn.yml screen and under the list in its editor, and a Warning after a save of `syslogServers`. The host is read (`logfwd.Read`) only where the list is valid and has a TCP or TLS server. Nothing is said where the logger's options can't be read. `hardn config` does not say it: it reads no host.
15. **With no hardn.yml** the line shows for every such case, a file `-f` or `HARDN_CONFIG` names and is not there too: the next start offers to create one then as well.
16. **Configuration's item 1** reads "View or edit the file hardn reads".
17. **`users.CheckNameForm`** is new, the form check `CheckName` began with, so the editor never asks whether the user exists.
18. **The gallery's play guard** (`TestPlayTouchesNothing`) now lets play name `config.ResolveHost`, which reads nothing.

**2026-11-16, `next/syslog-protocol`** (bug "The syslog server is TCP, with no easy way to UDP"):

- **Change protocol**, a third item under `syslogServers`, only with a server: the server by number, "Enter protocol [<current>]", the port ("Enter port [514]", 6514 for tls) only where the protocol changed and the port was the old default or unset, and for tls the CA file. An explicit other port is kept. The whole list is checked by `SyslogServers.Resolve`; the block is changed in place through `PlanSave`, never removed and added. `config.Edit` now edits a list item whose fields alone change in place, so the server's comments stay; a removed field's line goes with its comment. The same protocol: "Nothing needs changing."
- **Choice 14's line** is now "busybox syslogd is UDP only. Change protocol under Edit a setting."
- **First run** asks "Syslog protocol (udp, tcp, tls) [tcp]: " after an accepted syslog server. Enter is tcp, not written. tls asks "Enter CA file: "; tls without one, or that `Resolve` refuses, gets one line and the protocol again. A bad answer: "Not udp, tcp or tls." The end of the input keeps tcp.

**First run:** see the release's notes and `docs/configuration.md`; its choices are in the unit's report (three fixed refusal lines, the closing line for one server, `host:0` refused, the answer written as typed).

## 8. Not verified

- Nothing ran on Linux, on either arch, and no hardn binary ran: `make check` on macOS only.
- The editor on a real terminal, and on a real Alpine host for decision 13's line: tested on `hostfake` and the gallery's simulated hosts only.
- The first-run series on a real terminal.
- A save under a pending `hardn confirm`, and one interrupted by a signal: the engine's own tests cover them, none here.
- `disableRootSSH` edited in a file that still has the old `disableRoot`: not tested.
