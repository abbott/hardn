# The Firewall parity table: origin/main's Firewall screen and `hardn -w` against the Firewall control (`TestParity`)

> **Unit:** refactor plan slice 4, unit (b2), `next/s4-parity`. **Status:** built and tested on macOS against `hostfake`; nothing ran on a VM or through the binary (§9). The legacy path stays until unit (d). No production path changed.

## 1. Why

Unit (d) deletes `pkg/menu/firewall_menu.go`, the UFW repository and `-w`'s path. This table holds, before that, what origin/main does against what the control does, per fixture distribution, each difference tied to a V-number or an adopted decision, so that nothing is lost by accident. Where no source says a difference, it stays UNEXPLAINED (§6), and unit (d) waits on it.

## 2. Method

- **origin/main, read in place** from `pkg/characterization/testdata/golden`, by a reader beside the test (`legacy_read_test.go`, `Slice.Reader`), which fails on any header, section, fixture, call, command, `-n` trace or trailer line it does not compare, and on a section left out (`TestReaderRefuses`):
  - `menu/firewall_*.golden` (49): the `Start:` line (any other header line it knows by its prefix and does not read; the harness takes V-numbers from `KNOWN-DEFECT` and `FIXED`), the screen, the repository's calls, ufw as the fixture ends, the files after, `./hardn.yml` as written. Of the screen it takes the result lines alone (a `✗`, the stub's line, `[DRY-RUN]`, a y/n question); drawn rows and words are unit (c)'s, not compared. The calls are replayed from `Start:` and held to the fixture's end, and the files after to the writes.
  - `ops/configure_secure_firewall/` (4), `hardn -w`, its dry-run section and `hardn -n` refusal too. Its start holds a profile, and origin/main's `-w` passes none (`cmd/hardn/main.go:418`, read), nor does the new one: no `-w` applies a profile.
  - `ops/configure_secure_firewall_profiles/` (4), menu item 2 with a profile, which no flag reaches (its header): the row `item2-profile`, Enable then Apply. Its dry-run section, which the menu skips (its `NOTE`), is not compared; `dry-configure` is that item's dry run.
  - Both ops layouts' fixture ufw answers everything, its start unsaid: the new path's host holds ufw installed and inactive.
- **The origin/main column is the goldens', which are origin/main plus the trunk's gates** (gallery-firewall.md §2; each golden's header): add and remove refused under plan only (slice 2b; origin/main saved `./hardn.yml` under dry run); ufw changes refused on Proxmox VE (P0.8; on origin/main ufw ran, V10); `hardn -n` with an operation flag refused (P0.8; on origin/main it ran live, V1); the pauses (DEV-050). §4's cells show the gate; their why-lines say what origin/main did.
- **The fixture's ufw takes `ufw reset` as done**, which probably aborts on a real host (V9's correction, [I]). "Rules after" is the fixture's outcome, not a host's. `ufw disable`, and a failure after it, stand either way.
- **The new path**, as unit (c) is to run it (`parity_test.go`, `host_test.go`): `firewall.Read` for what the screen would offer or refuse; then the action's `Incoming` through `screenkit.Session.Change` on `hostfake`, answered y/N where plain and with the hostname typed where lockout-risk, the timer checked armed. Refused: said before asking, nothing planned; where no item leads to the action (Proxmox VE's rows, `no-ufw-item2`) the cell says so, the control's plan and no screen path (§7). Under plan only: stops at the plan. The driver is this test's until unit (c).
- **`-w`'s new form:** `Incoming{ActionEnable}` through the engine, as `runControls` builds it, in every cell equal to the screen's Enable (checked). **Decision 4's question comes in unit (d):** on a terminal the hostname typed, off a terminal a refusal. The rows say the timer is armed and the question is to come; neither is claimed run.
- **The same start:** each cell's host is held to its golden's (`Cell.LegacyStart`): ufw's state, `cfg.SshPort`, the profiles, from a loaded `hardn.yml` (`testdata/fixtures`). The golden does not say where sshd is, so the row does: on `cfg.SshPort`, 2230, but `configure`, whose golden seeds `Port 22` and whose cell note says so.
- **Rows and cells:** 33 rows, 42 cells, every menu golden on Debian 13 (Proxmox VE's on Proxmox VE 9). Install alone also runs on the others, as its plan differs by host (`apt-get`, `apk add`, refused). `w` and `item2-profile` run on all four. Alpine's stock shape has its own: `install-stock` (no ufw in apk's index), `w-stock` (no `ss`). Enable differs on Alpine (`rc-update add ufw default`, `/etc/init.d/hardn-boot`). Disable and the saves are not run off Debian 13.
- **Not rows** (`TestParityGoldens` holds each of the 57 goldens a row or a not-row, `notRows` with why): each ran no change.
  - Unit (c)'s, drawn: `firewall_open_active_no_rule`, `_inactive`, `_not_installed`, `_profiles`, `_active_rules_never_listed` and its two `_listed_*` sides (defect 6; P-30 (b)), `firewall_profiles_none`, `_two`.
  - Unit (c)'s, a key: `firewall_invalid_key`, `_return_0`, `_quit_q`, `firewall_profiles_invalid`, `_quit_q`, `_hidden_items_none_to_act_on`.
  - `firewall_profiles_not_installed_hidden_still_runs`: opened and left; whether the sub-screen opens with no ufw is unit (c)'s. `firewall_profiles_remove_bad_number`: nothing saved; Remove's Select refuses at the field.
  - `firewall_open_status_fails_reads_inactive`, `firewall_open_port_22_warns`, `firewall_open_port_2230_while_sshd_says_22`: drawn, but what they show differs (§5, `TestOpenDifferences`).
- **Aspects compared:** the refusal (none, the code and "before asking", `failed:`, origin/main's words); `commands`, the ufw commands in order; what they leave, `ufw after`, `rules after`; `SSH rule added`, by which port, and `rule comment`, `how ufw starts`, none for a command that failed; `at boot`; `packages`; `files`, path and mode, `profile file` content, `write`, in place or atomic; `hardn.yml`, which file, the key, the other settings, the rest; `asks`, none, y/N, or the hostname typed and the timer; `plan names` under plan only; `backup`, `journal`, `findings`; `dry run` for `-w`.
- **Recorded, not compared:** commands that only read (`probes:`); the revert timer's and the boot unit's commands, each by its whole form (`timerForm`), which `asks` says. Any other command fails the test. **Not compared:**
  - drawn rows and words (unit (c)); the pauses and defects 8 and 9 (unit (c));
  - exit statuses (unit (d), V25); a file's owner, not in the goldens;
  - `./hardn.yml`'s mode, not in the goldens: 0644 on origin/main (`config.go:491`, read); the `0600` of `add` and `remove` is this test's `AddFile`;
  - the default policy: no golden says what a start held. origin/main's commands set deny incoming and allow outgoing whatever the host holds; Enable plans deny incoming and leaves outgoing (Made here), so a host whose outgoing policy is not allow keeps it (§5). The control's tests hold `allow-policy`.
- **The table is a test:** `fwTable` keys every difference by scenario, cell and aspect, with its source and one line. `TestParity` fails on an unlisted difference (UNEXPLAINED), a stale entry and a V-number the cell's golden does not name.
- **Mutation-checked** (run at this unit's tip, each through `go test -overlay`, nothing in the repository changed; goldens from a changed copy):
  1. `disable`'s `commands` entry removed: UNEXPLAINED;
  2. an entry for `disable-no`'s `commands` added: stale;
  3. `enable`'s `commands` tied to V18: "does not name";
  4. the pending `rule comment` entry removed: UNEXPLAINED, on each of its cells;
  5. a not-row left out: `TestParityGoldens` fails;
  6. a not-row misnamed: "not there";
  7. a row's golden also a not-row: "one or the other";
  8. a `chmod` call in `firewall_item2_profiles`: the reader fails;
  9. `cfg.SshPort` 2231 in `firewall_item1_inactive_enable_runs_configure`'s `Start:`: the test fails on the start;
  10. V9 taken from `firewall_item2_active_rules_are_reset`: "does not name";
  11. the profile file's mode 0640 in `firewall_item2_profiles`: UNEXPLAINED, `mode`;
  12. `-w` planning Disable: "the screen's Enable left … -w left";
  13. the pending `how ufw starts` entry made explained, and the control's Proxmox VE gate dropped: only `parity.golden` and the page fail (limit 1; `ufw.Read` refuses Proxmox VE again below it, so no ufw call);
  14. `sh -c "ufw reset"` and `rc-service ufw stop` at the top of `ufw.Execute`: `unexpected command`, each;
  15. the fake finding no profile file: Apply's allow exits 1, and `profiles` fails.
- **Its limits:** (1) a difference already tied whose values change fails only on `parity.golden` and the page, which `make golden-update` accepts: the review of that diff is the guard; (2) the driver is this test's, not a screen, until unit (c); (3) ufw's answers are a model, in `plan_test.go`'s shape, not captured.
- **Size:** `internal/parity` 14 lines added, 7 removed (5 of them its package comment), its tests 19 and 4; `pkg/screens/firewall` 1,892 lines of test (`parity_test.go` 1,002, above Environment's 899, its reader 534 and its host 356 where Environment's are the harness's and a sudoers file).

## 3. Host states

Every host is the fixture's start forced on `hostfake`, none read from a stock install. Facts are the control's tests': Debian 13 (`sshd.service`), Ubuntu 26.04 (`ssh.socket`, `[::]` dual-stack; its ops golden's host line says 24.04, which origin/main's path never reads), Alpine 3.23 (OpenRC, ufw in no runlevel, with `iproute2` but in `w-stock`), Proxmox VE 9. A session is alice's, to the port sshd is on. Each state's words are below; `parity.golden` has each cell's.

## 4. The table

The region below is generated.

<!-- TestParity: begin. Generated by TestParity (pkg/screens/firewall); make golden-update rewrites it. Do not edit. -->

**Host states** (§3): what the new path ran on, and whether the distribution's stock install has it (real), the harness forced it, or it cannot exist.

| Scenarios | Debian 13 | Ubuntu 26.04 | Alpine 3.23 | Proxmox VE 9 |
|---|---|---|---|---|
| enable | inactive (forced) | not run | not run | not run |
| configure | sshd on 22 (forced) | not run | not run | not run |
| active-rules | active, a rule (forced) | not run | not run | not run |
| fails | allow fails (forced) | not run | not run | not run |
| profiles, add-case, remove, remove-no, apply | two profiles (forced) | not run | not run | not run |
| no-ufw-item2 | no ufw (forced) | not run | not run | not run |
| install | no ufw (forced) | no ufw (forced) | no ufw (forced) | no ufw (forced) |
| install-stock | not run | not run | index empty (forced) | not run |
| disable, disable-no | active (forced) | not run | not run | not run |
| pve-open, pve-item1, pve-item2 | not run | not run | not run | ufw inactive (forced) |
| pve-active | not run | not run | not run | ufw active (forced) |
| add, add-bad-port, add-slash, add-no-name, add-no-ports | no profiles (forced) | not run | not run | not run |
| dry-configure, dry-enable, dry-install, dry-disable, dry-apply, dry-add, dry-remove | plan only (forced) | not run | not run | not run |
| w | inactive (forced) | inactive (forced) | inactive (forced) | inactive (forced) |
| item2-profile | a profile (forced) | a profile (forced) | a profile (forced) | a profile (forced) |
| w-stock | not run | not run | no ss (forced) | not run |

**Agreeing and differing aspects** (§4): in how many compared cells each aspect agrees and differs.

| Aspect | Agrees in | Differs in |
|---|---|---|
| refusal | 19 | 23 |
| profile file | 10 | 1 |
| directories | 42 | 0 |
| backup | 12 | 7 |
| findings | 42 | 0 |
| hardn.yml | 38 | 4 |
| ufw after | 36 | 6 |
| rules after | 38 | 4 |
| SSH rule added | 40 | 2 |
| at boot | 40 | 2 |
| packages | 39 | 3 |
| files | 31 | 11 |
| commands | 26 | 16 |
| journal | 1 | 18 |
| asks | 24 | 18 |
| rule comment | 0 | 11 |
| how ufw starts | 0 | 11 |
| mode | 4 | 0 |
| write | 0 | 6 |
| items offered | 0 | 1 |
| ufw left active | 0 | 1 |
| hardn.yml ufwAppProfiles | 2 | 0 |
| hardn.yml settings | 2 | 0 |
| hardn.yml rest | 0 | 2 |
| plan names | 0 | 7 |
| dry run | 0 | 5 |

**Differences** (§4): each with its source; every cell, both end states whole, is in [parity.golden](../../pkg/screens/firewall/testdata/golden/parity.golden).

| Scenarios | On | Aspect | origin/main | New path | Source |
|---|---|---|---|---|---|
| enable, configure, active-rules, fails | Debian 13 | files | `none` | `/etc/systemd/system/hardn-boot.service 0644` | defect 3's answer (§5): through the flow, the hostname typed, confirm-or-revert armed: hardn's boot unit, the engine's for any lockout-risk plan; it stays after a rollback, until a boot finds nothing journalled |
| enable | Debian 13 | commands | `ufw default deny incoming, ufw default allow outgoing, ufw disable, ufw reset, ufw allow 2230/tcp comment "SSH access", sh -c "yes \| ufw enable"` | `ufw allow 2230/tcp, ufw --force enable` | V9; defect 1; Made here: default deny, default allow outgoing, disable and reset gone; the allow, then the start (the comment and the start's form are rows of their own) |
| enable (Debian 13); configure (Debian 13); active-rules (Debian 13); fails (Debian 13); profiles (Debian 13); item2-profile (Debian 13, Ubuntu 26.04, Alpine 3.23) | as listed | journal | `none` | `journalled first` | defect 3's answer (§5): through the flow, the hostname typed, confirm-or-revert armed: no rollback; now journalled first |
| enable (Debian 13); configure (Debian 13); active-rules (Debian 13); fails (Debian 13); profiles (Debian 13); item2-profile (Debian 13, Ubuntu 26.04, Alpine 3.23) | as listed | asks | `nothing` | `the hostname typed; the timer armed` | defect 3's answer (§5): through the flow, the hostname typed, confirm-or-revert armed: one key, no question; now the hostname typed, the timer armed |
| enable (Debian 13); configure (Debian 13); active-rules (Debian 13); profiles (Debian 13); w (Debian 13, Ubuntu 26.04, Alpine 3.23); item2-profile (Debian 13, Ubuntu 26.04, Alpine 3.23) | as listed | rule comment | `SSH access` | `none` | UNEXPLAINED (pending; proposed, not approved: gallery-firewall.md §3 (Enable's commands, the real plan's); docs/safety/ufw.md §2 item 7): the SSH rule's comment, SSH access, is gone: ufw status no longer names it |
| enable (Debian 13); configure (Debian 13); profiles (Debian 13); w (Debian 13, Ubuntu 26.04, Alpine 3.23); item2-profile (Debian 13, Ubuntu 26.04, Alpine 3.23) | as listed | how ufw starts | `sh -c "yes \| ufw enable"` | `ufw --force enable` | UNEXPLAINED (pending; proposed, not approved: docs/safety/ufw.md (enable prompts under SSH unless --force)): sh -c "yes | ufw enable"; now ufw --force enable |
| configure | Debian 13 | commands | `ufw default deny incoming, ufw default allow outgoing, ufw disable, ufw reset, ufw allow 2230/tcp comment "SSH access", sh -c "yes \| ufw enable"` | `ufw allow 22/tcp, ufw --force enable` | V9; defect 1; Made here; defect 2; decision 2: as enable; and sshd's port, 22, from sshd -T and ss, not cfg.SshPort's 2230 |
| configure | Debian 13 | rules after | `2230/tcp` | `22/tcp` | V9; defect 2; decision 2: cfg.SshPort's 2230, while sshd is on 22; now 22 |
| configure | Debian 13 | SSH rule added | `2230/tcp` | `22/tcp` | V9; defect 2; decision 2: the configured port; now what sshd -T and ss say |
| active-rules | Debian 13 | commands | `ufw default deny incoming, ufw default allow outgoing, ufw disable, ufw reset, ufw allow 2230/tcp comment "SSH access", sh -c "yes \| ufw enable"` | `ufw allow 2230/tcp` | V9; defect 1; Made here: the sequence gone: ufw stays active, its rule kept, and only 2230's added |
| active-rules | Debian 13 | rules after | `2230/tcp` | `the start's 2 lines, 2230/tcp` | V9; defect 1; Made here: reset cleared the rule on 22, on the fixture (V9's correction: the reset probably aborts on a host [I]); now it stays |
| active-rules | Debian 13 | how ufw starts | `sh -c "yes \| ufw enable"` | `none` | V9; defect 1; Made here: disable, then a start; now ufw stays active, no start |
| fails | Debian 13 | refusal | `failed: exit status 1` | `failed: COMMAND_FAILED` | V9; defect 1; Made here: the command's exit status, with ufw left off; now its code, rolled back |
| fails | Debian 13 | commands | `ufw default deny incoming, ufw default allow outgoing, ufw disable, ufw reset, ufw allow 2230/tcp comment "SSH access"` | `ufw allow 2230/tcp` | V9; defect 1; Made here: the sequence gone: only the allow runs, and fails |
| fails | Debian 13 | ufw after | `inactive` | `active` | V9; defect 1; Made here: disable ran before the allow failed, and nothing restored it; now ufw stays active |
| fails | Debian 13 | rules after | `none` | `the start's 2 lines` | V9; defect 1; Made here: as active-rules, and the allow failed; now the start's rule stays |
| profiles (Debian 13); item2-profile (Debian 13, Ubuntu 26.04) | as listed | files | `/etc/ufw/applications.d/hardn 0644` | `/etc/systemd/system/hardn-boot.service 0644, /etc/ufw/applications.d/hardn 0644` | defect 3's answer (§5): through the flow, the hostname typed, confirm-or-revert armed; decision 3 (a): the boot unit beside the profile file, written by Apply |
| profiles | Debian 13 | profile file | `[WebServer] / title=Web Server / description=Public web / ports=80/tcp,443/tcp / [Metrics] / title=Metrics / description=Node exporter / ports=9100/tcp` | `[WebServer] / title=Web Server / description=Public web / ports=80/tcp\|443/tcp / [Metrics] / title=Metrics / description=Node exporter / ports=9100/tcp` | UNEXPLAINED (pending; proposed, not approved: gallery-firewall.md §7 findings; ufw-framework(8) [I]): ports=80/tcp,443/tcp, which ufw-framework(8)'s | form does not read [I]; now ports=80/tcp|443/tcp |
| profiles | Debian 13 | commands | `ufw default deny incoming, ufw default allow outgoing, ufw disable, ufw reset, ufw allow from any to any app WebServer, ufw allow from any to any app Metrics, ufw allow 2230/tcp comment "SSH access", sh -c "yes \| ufw enable"` | `ufw allow 2230/tcp, ufw --force enable, ufw allow from any to any app WebServer, ufw allow from any to any app Metrics` | V9; defect 1; Made here; decision 3 (a): as enable; and the profiles' rules at Apply, after the start, not before it |
| profiles | Debian 13 | write | `in place` | `atomic` | decision 3 (a): the profile file in place; now atomic, through the plan |
| profiles | Debian 13 | backup | `none` | `/etc/ufw/applications.d/hardn (absent before: the rollback removes it)` | decision 3 (a): nothing backed up; now the profile file recorded absent, so a rollback removes it |
| no-ufw-item2 | Debian 13 | refusal | `failed: UFW firewall is not installed` | `UFW_NOT_INSTALLED, no item: the control's plan` | Made here (origin/main's item 2 goes): hidden item 2 still ran and failed; it is gone, and Enable's plan, as -w's, refuses |
| install | Debian 13, Ubuntu 26.04, Alpine 3.23 | refusal | `stub: isn't yet implemented` | `none` | defect 4; decision 1; P-16: the stub; now it installs |
| install | Debian 13, Ubuntu 26.04 | commands | `none` | `apt-get install -y ufw` | defect 4; decision 1; P-16: the stub ran nothing; now the tool's install |
| install | Debian 13, Ubuntu 26.04, Alpine 3.23 | journal | `none` | `journalled first` | decision 1; P-16; control-firewall.md: Install's undo is journalled before it runs; engineering call, not adopted by the maintainer: the stub; now journalled first, its undo the tool's |
| install | Debian 13, Ubuntu 26.04, Alpine 3.23 | ufw after | `not installed` | `inactive` | defect 4; decision 1; P-16: the stub left it not installed; now installed, inactive |
| install | Debian 13, Ubuntu 26.04, Alpine 3.23 | packages | `none` | `ufw` | defect 4; decision 1; P-16: the stub installed nothing; now ufw |
| install | Debian 13, Ubuntu 26.04, Alpine 3.23 | asks | `nothing` | `y/N` | decision 1: the stub; now plain, y/N |
| install | Alpine 3.23 | commands | `none` | `apk add ufw` | defect 4; decision 1; P-16: the stub ran nothing; now the tool's install |
| install | Proxmox VE 9 | refusal | `stub: isn't yet implemented` | `UFW_NOT_APPLICABLE, no item: the control's plan` | defect 4; decision 1; P-16: the stub, on any host; now no item on Proxmox VE, and the control's plan refuses |
| install-stock | Alpine 3.23 | refusal | `stub: isn't yet implemented` | `PACKAGE_NOT_IN_REPOS, before asking` | defect 4; decision 1; control-firewall.md: Read also plans Install, so its refusal is said before asking; engineering call, not adopted by the maintainer: the stub; now apk's index has no ufw, refused before asking |
| disable | Debian 13 | refusal | `stub: isn't yet implemented` | `none` | defect 4; decision 6: the stub; now ufw disable |
| disable | Debian 13 | commands | `none` | `ufw disable` | defect 4; decision 6: the stub ran nothing; now ufw disable |
| disable | Debian 13 | journal | `none` | `journalled first` | decision 6: the stub; now journalled first, undone by ufw --force enable |
| disable | Debian 13 | ufw after | `active` | `inactive` | defect 4; decision 6: the stub left ufw active; now inactive, its rules saved |
| pve-open | Proxmox VE 9 | items offered | `Enable firewall, Configure firewall, Manage application profiles` | `Return alone: pve-firewall read, nothing changed` | V10; defect 5; decision 7: Enable, Configure and Profiles offered, no word of Proxmox VE; now read-only |
| pve-item1, pve-item2 | Proxmox VE 9 | refusal | `failed: UFW is not used on Proxmox VE hosts: pve-firewall manages the packet filter, so hardn neither configures nor enables UFW` | `UFW_NOT_APPLICABLE, no item: the control's plan` | decision 7; control-firewall.md: code UFW_NOT_APPLICABLE; engineering call, not adopted by the maintainer: on origin/main ufw ran here (the golden's NOTE, V10); the trunk's P0.8 gate refuses after the key; now no item leads here, the screen read-only, and the control's plan refuses |
| pve-active | Proxmox VE 9 | refusal | `stub: isn't yet implemented` | `UFW_NOT_APPLICABLE, no item: the control's plan` | V10; defect 5; decision 7: the stub, the way out on the screen; now no item leads here, the control's plan refuses, and the warning row says to run ufw disable |
| pve-active | Proxmox VE 9 | asks | `y/N` | `nothing` | V10; decision 7: asked, then the stub; now no item, nothing asked |
| pve-active | Proxmox VE 9 | ufw left active | `not said; item 1 is the stub` | `a warning row, from ufw.conf; no ufw call` | V10; defect 5; decision 7: not said; now a warning row from ufw.conf, no ufw call |
| add, remove | Debian 13 | write | `in place` | `atomic` | defect 12; decision 3 (a): hardn.yml in place; now atomic, through the plan |
| add, remove | Debian 13 | backup | `none` | `/etc/hardn/hardn.yml` | defect 12; decision 3 (a): nothing backed up; now hardn.yml first |
| add, remove | Debian 13 | journal | `none` | `journalled first` | decision 3 (a): no rollback; now journalled first |
| add, remove | Debian 13 | hardn.yml | `./hardn.yml, in the working directory` | `/etc/hardn/hardn.yml, the file loaded` | defect 12: ./hardn.yml in the working directory; now the file loaded |
| add, remove | Debian 13 | hardn.yml rest | `47 settings, written whole; no file was loaded` | `the ufwAppProfiles lines alone changed; the file's other 2 lines kept (3 settings, 0 comment lines)` | defect 12; decision 3 (a): the whole configuration written; now the key's lines alone (steps.PlanSave) |
| add | Debian 13 | asks | `nothing` | `y/N` | decision 3 (a): saved on the fourth answer; now through the flow, y/N |
| add-bad-port | Debian 13 | refusal | `refused: Invalid port format '8080'. Must include protocol (e.g., '80/tcp')` | `UFW_BAD_PROFILE, before asking` | defect 11; decision 3 (a); §6's words: refused after four answers, dropping them; the plan's code now (Add's form refuses at the field: unit (c)) |
| add-slash | Debian 13 | refusal | `none` | `UFW_BAD_PROFILE, before asking` | defect 11; §6's words: saved: its check was a slash; now refused, 1 to 65535 with a protocol |
| add-slash | Debian 13 | hardn.yml | `./hardn.yml, in the working directory` | `not saved` | defect 11; defect 12: saved with a port that is none; now refused, nothing saved |
| add-no-name | Debian 13 | refusal | `refused: Profile name cannot be empty` | `UFW_BAD_PROFILE, before asking` | decision 3 (a): refused in its words; the plan's code now (Add's form refuses at the field: unit (c)) |
| add-no-ports | Debian 13 | refusal | `refused: Ports cannot be empty` | `UFW_BAD_PROFILE, before asking` | decision 3 (a): refused in its words; the plan's code now (Add's form refuses at the field: unit (c)) |
| add-case | Debian 13 | refusal | `refused: A profile with this name already exists` | `none` | UNEXPLAINED (pending; proposed, not approved: control-firewall.md: a profile name used twice is refused; engineering call, not adopted by the maintainer): webserver beside WebServer refused, ignoring case (firewall_menu.go:450, strings.EqualFold); now only the same case is the same name, and it saves |
| add-case | Debian 13 | backup | `none` | `/etc/hardn/hardn.yml` | UNEXPLAINED (pending; proposed, not approved: control-firewall.md: a profile name used twice is refused; engineering call, not adopted by the maintainer): not saved; now hardn.yml backed up |
| add-case | Debian 13 | journal | `none` | `journalled first` | UNEXPLAINED (pending; proposed, not approved: control-firewall.md: a profile name used twice is refused; engineering call, not adopted by the maintainer): not saved; now journalled |
| add-case | Debian 13 | hardn.yml | `not saved` | `/etc/hardn/hardn.yml, the file loaded` | UNEXPLAINED (pending; proposed, not approved: control-firewall.md: a profile name used twice is refused; engineering call, not adopted by the maintainer): not saved; now saved |
| add-case | Debian 13 | asks | `nothing` | `y/N` | UNEXPLAINED (pending; proposed, not approved: control-firewall.md: a profile name used twice is refused; engineering call, not adopted by the maintainer): refused at the name; now asked y/N |
| apply | Debian 13 | refusal | `stub: isn't yet implemented` | `UFW_INACTIVE, before asking` | defect 4; decision 3 (a); decision 5; P-33 (1) (b): the stub; now no rule saved while ufw stays inactive |
| dry-configure | Debian 13 | plan names | `Would configure UFW with default policies and SSH rules; SSH port: 2230/tcp` | `ufw allow 2230/tcp; ufw --force enable` | defect 3's answer (§5): through the flow, the hostname typed, confirm-or-revert armed: words naming no command; now Enable's plan |
| dry-enable | Debian 13 | plan names | `Would enable UFW` | `ufw allow 2230/tcp; ufw --force enable` | defect 3's answer (§5): through the flow, the hostname typed, confirm-or-revert armed: words naming no command (the golden's KNOWN-DEFECT); now Enable's plan |
| dry-install | Debian 13 | plan names | `Would install UFW package` | `apt-get install -y ufw` | defect 10; decision 1; P-16: words for what live does not do; now the plan |
| dry-disable | Debian 13 | asks | `y/N` | `nothing` | defect 4's answer (§5): Disable through the flow: asked, then said what it would do; now stops at its plan, unasked |
| dry-disable | Debian 13 | plan names | `Would disable UFW` | `ufw disable` | defect 4's answer (§5); decision 6: words for the stub; now the plan |
| dry-apply | Debian 13 | refusal | `none` | `UFW_INACTIVE, before asking` | defect 10; decision 5; P-33 (1) (b): said it would write the file, which live does not; now refused on an inactive ufw |
| dry-apply | Debian 13 | plan names | `Would write profiles to /etc/ufw/applications.d/hardn; Profile: WebServer (80/tcp, 443/tcp); Profile: Metrics (9100/tcp)` | `none` | defect 10; decision 5: words for what live does not do; now refused, no plan |
| dry-add, dry-remove | Debian 13 | refusal | `refused: Not available in plan only.` | `none` | Made here (under plan only, Add and Remove stop at their plan): the trunk refuses since slice 2b, where origin/main saved ./hardn.yml under dry run (the golden's NOTE); now the plan, nothing saved |
| dry-add, dry-remove | Debian 13 | plan names | `none` | `/etc/hardn/hardn.yml` | Made here (under plan only, Add and Remove stop at their plan): refused, no plan; now the plan, its target the file loaded |
| w | Debian 13, Ubuntu 26.04 | files | `none` | `/etc/systemd/system/hardn-boot.service 0644` | decision 4: hardn's boot unit, the engine's for any lockout-risk plan: the timer unit (d)'s question arms |
| w | Debian 13, Ubuntu 26.04 | commands | `ufw default deny incoming, ufw default allow outgoing, ufw disable, ufw reset, ufw allow 2230/tcp comment "SSH access", sh -c "yes \| ufw enable"` | `ufw allow 2230/tcp, ufw --force enable` | V9; defect 1; Made here: as enable, on -w |
| w | Debian 13, Ubuntu 26.04, Alpine 3.23 | journal | `none` | `journalled first` | decision 4: no rollback; now journalled first |
| w | Debian 13, Ubuntu 26.04, Alpine 3.23 | asks | `nothing` | `the timer armed; the hostname typed in unit (d) (decision 4)` | decision 4: no question; the plan arms the timer, and unit (d) adds the hostname typed, off a terminal a refusal |
| w | Debian 13, Ubuntu 26.04, Alpine 3.23 | dry run | `the service writes and runs as live (cfg.DryRun unread below the menus); no path reaches it: hardn -n refused (exit status 2)` | `the plan alone: nothing written` | UNEXPLAINED (pending; proposed, not approved: V1; docs: -n and -e's plan (gallery-environment.md Made here)): the trunk's P0.8 gate refuses hardn -n -w, exit status 2, as V1's fix; runControls' plan alone once unit (d) moves it there |
| w | Alpine 3.23 | files | `none` | `/etc/init.d/hardn-boot 0755` | decision 4: hardn's boot unit, the engine's for any lockout-risk plan: the timer unit (d)'s question arms |
| w | Alpine 3.23 | commands | `ufw default deny incoming, ufw default allow outgoing, ufw disable, ufw reset, ufw allow 2230/tcp comment "SSH access", sh -c "yes \| ufw enable"` | `rc-update add ufw default, ufw allow 2230/tcp, ufw --force enable` | V9; defect 1; Made here; Made here (a service that does not start at boot is a step of Enable); control-firewall.md: the boot change goes before ufw's; engineering call, not adopted by the maintainer: as enable, on -w; and rc-update add ufw default first, as ufw enable adds no runlevel |
| w, item2-profile | Alpine 3.23 | at boot | `not changed` | `rc-update add ufw default` | Made here (a service that does not start at boot is a step of Enable): ufw enable adds no runlevel; now rc-update add ufw default |
| w | Proxmox VE 9 | refusal | `failed: UFW is not used on Proxmox VE hosts: pve-firewall manages the packet filter, so hardn neither configures nor enables UFW` | `UFW_NOT_APPLICABLE, before asking` | V10; decision 7; control-firewall.md: code UFW_NOT_APPLICABLE; engineering call, not adopted by the maintainer: on origin/main ufw ran (V10); the trunk's P0.8 gate refuses; now the plan refuses |
| w | Proxmox VE 9 | dry run | `the service writes and runs as live (cfg.DryRun unread below the menus); no path reaches it: hardn -n refused (exit status 2)` | `refused (UFW_NOT_APPLICABLE): nothing written` | UNEXPLAINED (pending; proposed, not approved: V1; docs: -n and -e's plan (gallery-environment.md Made here)): the trunk's P0.8 gate refuses hardn -n -w, exit status 2, as V1's fix; the plan's refusal once unit (d) moves it there |
| item2-profile | Debian 13, Ubuntu 26.04 | commands | `ufw default deny incoming, ufw default allow outgoing, ufw disable, ufw reset, ufw allow from any to any app ExampleHTTPS, ufw allow 2230/tcp comment "SSH access", sh -c "yes \| ufw enable"` | `ufw allow 2230/tcp, ufw --force enable, ufw allow from any to any app ExampleHTTPS` | V9; defect 1; Made here; decision 3 (a): as profiles, with its one profile |
| item2-profile | Debian 13, Ubuntu 26.04, Alpine 3.23 | write | `in place` | `atomic` | V18; decision 3 (a): the profile file in place; now atomic, through the plan |
| item2-profile | Debian 13, Ubuntu 26.04, Alpine 3.23 | backup | `none` | `/etc/ufw/applications.d/hardn (absent before: the rollback removes it)` | V18; decision 3 (a): nothing backed up; now the profile file recorded absent, so a rollback removes it |
| item2-profile | Alpine 3.23 | files | `/etc/ufw/applications.d/hardn 0644` | `/etc/init.d/hardn-boot 0755, /etc/ufw/applications.d/hardn 0644` | defect 3's answer (§5): through the flow, the hostname typed, confirm-or-revert armed; decision 3 (a): the boot unit beside the profile file, written by Apply |
| item2-profile | Alpine 3.23 | commands | `ufw default deny incoming, ufw default allow outgoing, ufw disable, ufw reset, ufw allow from any to any app ExampleHTTPS, ufw allow 2230/tcp comment "SSH access", sh -c "yes \| ufw enable"` | `rc-update add ufw default, ufw allow 2230/tcp, ufw --force enable, ufw allow from any to any app ExampleHTTPS` | V9; defect 1; Made here; decision 3 (a); Made here (a service that does not start at boot is a step of Enable); control-firewall.md: the boot change goes before ufw's; engineering call, not adopted by the maintainer: as profiles; and rc-update add ufw default first, as ufw enable adds no runlevel |
| item2-profile | Proxmox VE 9 | refusal | `failed: UFW is not used on Proxmox VE hosts: pve-firewall manages the packet filter, so hardn neither configures nor enables UFW` | `UFW_NOT_APPLICABLE, no item: the control's plan` | V10; decision 7; control-firewall.md: code UFW_NOT_APPLICABLE; engineering call, not adopted by the maintainer: on origin/main ufw ran (V10); the trunk's P0.8 gate refuses; now no item leads here, and the control's plan refuses |
| w-stock | Alpine 3.23 | refusal | `none` | `FIREWALL_SS_NOT_INSTALLED, before asking` | V9; decision 1; decision 2; §6's words: no ss: Enable refuses until iproute2 is there, as Install does not bring it |
| w-stock | Alpine 3.23 | commands | `ufw default deny incoming, ufw default allow outgoing, ufw disable, ufw reset, ufw allow 2230/tcp comment "SSH access", sh -c "yes \| ufw enable"` | `none` | V9; decision 1; decision 2: no ss, so Enable refuses and nothing runs |
| w-stock | Alpine 3.23 | ufw after | `active` | `inactive` | V9; decision 1; decision 2: refused: ufw stays as it was |
| w-stock | Alpine 3.23 | rules after | `2230/tcp` | `none` | V9; decision 1; decision 2: refused: no rule added |
| w-stock | Alpine 3.23 | SSH rule added | `2230/tcp` | `none` | V9; decision 1; decision 2: refused: no rule added |
| w-stock | Alpine 3.23 | rule comment | `SSH access` | `none` | V9; decision 1; decision 2: refused: no rule |
| w-stock | Alpine 3.23 | how ufw starts | `sh -c "yes \| ufw enable"` | `none` | V9; decision 1; decision 2: refused: no start |
| w-stock | Alpine 3.23 | dry run | `the service writes and runs as live (cfg.DryRun unread below the menus); no path reaches it: hardn -n refused (exit status 2)` | `refused (FIREWALL_SS_NOT_INSTALLED): nothing written` | UNEXPLAINED (pending; proposed, not approved: V1; docs: -n and -e's plan (gallery-environment.md Made here)): the trunk's P0.8 gate refuses hardn -n -w, exit status 2, as V1's fix; the plan's refusal, no ss, once unit (d) moves it there |

**v0.3.2** (§4): where it differs from origin/main, from [v032-reference.md](../refactor/phase0/v032-reference.md) §4.4.

| Scenarios | v0.3.2 |
|---|---|
| enable, configure, active-rules, profiles, w-stock | no ufw disable, no reset, existing rules kept; ufw allow <port>/tcp comment SSH, yes | ufw enable; rc-update add ufw default, rc-service ufw start on Alpine; no status check |
| fails | no ufw disable, no reset, existing rules kept; a failed SSH allow only logged, and ufw enable still runs; no status check |
| no-ufw-item2, install, install-stock | apt-get install -y ufw, or apk add ufw, inside configure, where ufw is not on PATH |
| disable, disable-no, add, add-bad-port, add-slash, add-no-name, add-no-ports, add-case, remove, remove-no, apply, dry-configure, dry-enable, dry-install, dry-disable, dry-apply, dry-add, dry-remove | not compared: §4.4 records v0.3.2's configure alone |
| pve-open, pve-item1, pve-item2, pve-active | configure ran as on Debian, no Proxmox VE gating (V10); §4.4 records no other item |
| w | no ufw disable, no reset, existing rules kept; ufw allow <port>/tcp comment SSH, yes | ufw enable; rc-update add ufw default, rc-service ufw start on Alpine; no status check; -w also wrote a host's profiles and allowed them, which origin/main's -w, passing none, does not; Proxmox VE as Debian, no gating (V10) |
| item2-profile | no ufw disable, no reset, existing rules kept; ufw allow <port>/tcp comment SSH, yes | ufw enable; rc-update add ufw default, rc-service ufw start on Alpine; no status check; the profile file backed up, written 0644, an allow per profile; Proxmox VE as Debian, no gating (V10) |

<!-- TestParity: end -->

## 5. What origin/main's path did that the new one does not

Each is intended unless §6 lists it.

| What goes | Where | Source |
|---|---|---|
| `ufw disable`, `ufw reset` before the allow: saved rules stay | `enable`, `configure`, `active-rules`, `fails`, `w*`, `item2-profile` | V9; defect 1; Made here |
| `ufw default allow outgoing`, whatever the host holds: an outgoing policy that is not allow now stays, which no cell's start holds | `commands` of the same rows | Made here |
| Item 2, "Configure firewall", the same sequence | `configure`, `profiles`, `item2-profile`, `no-ufw-item2` | Made here |
| Allowing `cfg.SshPort` regardless of sshd | `configure` | V9; defect 2; decision 2 |
| Applying profiles in Enable's sequence, items 1 and 2; now Apply alone. `-w` applies none, on either path | `profiles`, `item2-profile` | decision 3 (a) |
| Saving to `./hardn.yml` in the working directory, the whole configuration | `add`, `remove` | defect 12 |
| A port checked by its slash, and saved | `add-slash` | defect 11; §6 |
| Saving `./hardn.yml` under plan only, origin/main's (the goldens' `NOTE`), and the trunk's refusal since slice 2b: each now stops at its plan | `dry-add`, `dry-remove` | Made here |
| **Drawn only, named here:** a failing `ufw status` reading Inactive, the "Error getting firewall status" branch unreachable; now ufw's state is not established and Enable refuses, `UFW_STATE_UNKNOWN` | `firewall_open_status_fails_reads_inactive` | defect 7; §6's words |
| **Drawn only, named here:** SSH Port from `cfg.SshPort`, warned at 22, "good security" otherwise; now what `sshd -T` and `ss` read | `firewall_open_port_22_warns`, `_2230_while_sshd_says_22` | defect 2; §6 |
| **UNEXPLAINED:** the SSH rule's comment; `yes \| ufw enable`; a comma in a profile's ports; a name refused in another case; `hardn -n -w` refused | §6 | none |

Removing a profile still leaves its rule, on both (§6's notice); the rule goes with neither.

## 6. UNEXPLAINED differences

No source the table may cite says these. Each is carried as `Tie.Pending`, marked in the golden and in §4, its tie proposed and not approved. **Each blocks unit (d) until answered.**

| Difference | Cells | Evidence | Proposed |
|---|---|---|---|
| The SSH rule's comment, `SSH access`, goes (`rule comment`) | `enable`, `configure`, `active-rules`, `profiles`, `w`, `item2-profile` | `firewall_manager.go:47` (a golden's note, read); `steps.PlanUFWEnable` passes none (run) | gallery-firewall.md §3, Enable's commands; docs/safety/ufw.md §2 item 7 |
| `sh -c "yes \| ufw enable"` becomes `ufw --force enable` (`how ufw starts`) | `enable`, `configure`, `profiles`, `w`, `item2-profile` | docs/safety/ufw.md §3 says `enable` prompts under SSH unless `--force` (read) | that section |
| A profile's ports join by `\|`, not a comma (`profile file`) | `profiles` | `ports=80/tcp,443/tcp` against `ports=80/tcp\|443/tcp` (run); whether ufw reads the comma is [I] | gallery-firewall.md §7's finding; ufw-framework(8) [I] |
| A name held in another case: `webserver` beside `WebServer` is refused on origin/main (`strings.EqualFold`, `firewall_menu.go:450`, read), saved here (`profilesOf` compares exactly; the gallery's form too) | `add-case`: `refusal`, `hardn.yml`, `backup`, `journal`, `asks` | `firewall_profiles_add_duplicate_name` against this run | control-firewall.md's "a profile name used twice is refused", read as exact; or make `profilesOf` and the form ignore case |
| `hardn -n -w` ran live on origin/main (V1) and is refused with exit status 2 by the trunk's P0.8 gate; `runControls` shows the plan alone, or its refusal. If the refusal stays, every cell changes | `w` on all four, `w-stock` | the ops goldens; `cmd/hardn/dns.go`'s `runControls` (read); decision 4 says nothing of `-n` | as `-e` (gallery-environment.md's Made here) |

## 7. For unit (c)

**What the screen must leave equal:** every cell's end state, through `firewall.Read`, then each item's `Session.Change`. Point `screenRun` at the screen's `Run`, typing what it types, and keep the table. `Via` changes, and the cells below; no other cell should. Hold in particular:

- Enable through the flow, the hostname typed; Disable and Install y/N. Disable beside Enable where item 1 is not it: `disable`'s start, active with no rule on sshd's port, has `First` Enable and `DisableBeside`.
- Apply refused by planning `ActionApplyProfiles` first (`apply`, `dry-apply`): `Reading` does not hold it.
- Under plan only, every item stops at its plan; Add and Remove too.
- Proxmox VE: Return alone, no ufw call, the warning row where `PVEUFWEnabled` is Yes.

**Cells that change, as no key leads to the action:** `pve-item1`, `pve-item2`, `pve-active`, `install` and `item2-profile` on Proxmox VE (Return alone), and `no-ufw-item2` (item 2 gone). Each `refusal` here, "no item: the control's plan", and `pve-active`'s `asks` are not a screen path (`onPVE`, `hidden` in `screenRun`): at unit (c) each row becomes what its key does, the plan's refusal held by the control's tests, and its tie's why-line changes with it. `add-bad-port`, `add-no-name`, `add-no-ports` and `add-slash` change too, below.

**What the table gains:** the pauses (defects 8 and 9); the drawn rows and words of §2's not-rows, in the screen's own golden; Add's form refusing at the field in `add-bad-port`, `add-no-name`, `add-no-ports` and `add-slash`, which plan here; `firewall_profiles_remove_bad_number` as a row through its Select; what opens with no ufw (`firewall_profiles_not_installed_hidden_still_runs`). Item 2 hidden and still running is gone with Made here's item 2: a test should hold that key.

## 8. For unit (d)

**Freeze first,** before `pkg/menu/firewall_menu.go`, the UFW repository and `-w`'s path go, as parity-environment.md §8 did: `git mv` the record under `pkg/screens/firewall/testdata/legacy`, the same paths under it, and point `legacyDir` there:

- `menu/firewall_*` (48) with `TestLegacyMenuFirewall`, retired in that commit; `menu/firewall_configure_dry_run` with its case of `TestLegacyMenuDryRun`, which stays for the others;
- `ops/configure_secure_firewall*` (8) with their `TestOperationGoldens` cases (`operations_test.go:661`, `:679`);
- a `TestLegacyRecord` holding each file's bytes, as Environment's. Nothing regenerates them.

**Then:** `-w` on `runControls` with decision 4's question; `TestParity`'s `flagRun` is the end state to keep, and its `asks` row gains the hostname typed; `-n -w` as §6 is answered; exit statuses, not compared here, including V25 (defect 13); decision 8's warning on load, unbuilt (`ufwDefaultIncomingPolicy` and `ufwDefaultOutgoingPolicy` in `pkg/config`, control-firewall.md). **Other callers of the sequence, not rows here (read):** `pkg/application/security_manager.go:58`, Run All's and `-r`'s, with `ufwAllowedPorts`; the repository cannot go while it stands, and it is not this table's to explain.

## 9. Written, not run (gate 5)

No hardn binary, VM, ufw, apt, apk or pve-firewall ran. ufw and the package tools answer from the model in `host_test.go`, the shape of `plan_test.go`'s; `apk add ufw`'s success is pkg/host/alpine's tests' form [I]. `make check` passed at the unit's end. `Slice.Reader`'s `Dry` and `CLI`, and `start-detached` read as a command, are the harness's only growth; no other slice's table, golden or page changed (run: `make golden-update`, `git status`).
