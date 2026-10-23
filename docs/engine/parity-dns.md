# The DNS parity table: origin/main's DNS path against the new one (`TestParity`)

> **Unit:** refactor plan slice 2 (DNS, the pilot), unit (b2), `next/s2-parity`: "Parity table: end state (file contents, …) of legacy vs new path, per fixture distro, with each difference tied to a V-number or an approved proposal. The baseline is the legacy (origin/main) path; the P0.7b v0.3.2 reference shows where origin/main regressed" (plan §2, "Phases 3-5 together", §3.22; [control-dns.md](control-dns.md) §11's (b2) item). Sources the table ties to: V1, V18, V19 ([side-effects.md](../refactor/phase0/side-effects.md) §8); decisions 1, 1 (ii), 1 (iii), 2, 4, 5, 6 and 9 of [gallery-dns.md](gallery-dns.md) §4 (adopted 2026-10-22); [P-37](../refactor/proposals.md#p-37) (a); plan §3.19 as amended 2026-10-22; side-effects row 29; [screen-dns.md](screen-dns.md) §2.8.
> **Status:** built and tested on macOS, in process: origin/main from `pkg/characterization`'s goldens, nine of them new here; the new path on the recording fake (`hostfake`) with the resolver model of the DNS screen's tests (written from [dns.md](../safety/dns.md) §3, not captured from a host). Nothing ran on Linux: every host claim is **written, not run (gate 5)**. No legacy code and no existing golden changed.

## 1. Why

Unit (d) replaces the legacy DNS path rather than gating it (the maintainer, 2026-10-22: `HARDN_UI=classic` withdrawn from plan §3.14): menu item 3 and `-g` go to the new control, and `pkg/menu/dns_menu.go` is deleted. This page and its test are the record of what that path did, kept after its code is gone: every difference between its end state and the new path's, on each distribution hardn supports, with the source that explains it.

## 2. Method

- **origin/main, (a):** `DNSManager.ConfigureDNS(cfg.Nameservers, "lan")`, which `-g` (`cmd/hardn/main.go:395-402`), the DNS menu's Configure DNS (`pkg/menu/dns_menu.go:150`) and Run All (`SecurityManager.HardenSystem`, `pkg/application/security_manager.go:68-75`, from `hardn -r` and the menu's Run All) all call. From `pkg/characterization`'s goldens, read in place: `ops/dns_{systemd_resolved,resolvconf,direct,empty_nameservers}/{debian,ubuntu,alpine,proxmox}.golden`, the real managers and adapters over the repo's mocks: each command, each write with its mode and content, each `mkdir`, both `cfg.DryRun` sections and `hardn -n -g`'s refusal. The harness forces each strategy by its probes' results, whatever the distribution ships (§3).
- **origin/main, (b):** the DNS menu's Add, Remove and presets, which no golden pinned. New in this unit: `TestMenuDNSGoldens` (`pkg/characterization/menu_dns_test.go`), nine goldens under `menu_dns/`. **The list edit and the message are from the code**, mirrored (`dnsMenuEdit`, from `dns_menu.go:180-342`): the menu cannot run in a test, since `DNSMenu.Show` runs `systemctl` and `resolvectl` and reads `/etc/resolv.conf` for real, and its handlers are unexported. **The save is the real `config.SaveConfig`**, in a scratch working directory (`t.Chdir`), on a configuration `config.LoadConfig` read from a scratch copy of the fixture, as `-f` does, with the umask set to 022.
- **The new path** (`TestParity`, `pkg/screens/dns/parity_test.go` with `parity_state_test.go` and `parity_render_test.go`): (a) the DNS control through the engine, `Plan` then `Apply`, as `-g` is to run it (screen-dns.md §7); its dry run, the plan alone; and the screen's Configure DNS, which must leave the host exactly as the apply does, and its dry run nothing. (b) The screen's items through `Run`, answered as typed, Alpine's udhcpc question answered n (the default). **(a) saves no hardn.yml**: its list is the file's own, and the control plans the save only when the list differs from the one the file holds (`pkg/engine/controls/dns/dns.go:278-284`, `savedList`; control-dns.md decision 8); the table's hardn.yml aspect agrees, "not saved", in every (a) cell.
- **The same configuration:** hardn.yml holds the characterization fixture's settings (`pkg/characterization/testdata/fixtures/config/dns-menu.yml`: alice, port 2230, nameservers 192.0.2.53 and 192.0.2.54, four comment lines; `-one` and `-empty` variants), loaded from `/etc/hardn/hardn.yml` (0644) on the new path, and as `-f` on origin/main's.
- **The same distributions:** the characterization's cells, Debian 13, Ubuntu 24.04, Alpine 3.23 and Proxmox VE 9 (Debian 13 with `/etc/pve`).
- **The aspects compared:** the refusal; the files written (path, mode); the nameservers they set, in order; their search domain lines; the other lines and the mode of a file both wrote; the directories made; the commands that change something; the write (in place or atomic), the backup and the journal; the findings; hardn.yml (where it is saved, its list, the rest of it, its mode); the dry run. An aspect of what was written is compared only when both paths wrote. **Probes**, the commands that only read, are recorded in the golden but not compared: they leave no end state. The backups and the journal are noted, not compared byte for byte.
- **The table is a test:** `parityTable` (`parity_test.go`) holds every difference, keyed by scenario, distribution and aspect, with its source and one line. `TestParity` fails on a difference not in it (reported UNEXPLAINED), on an entry whose difference no longer occurs, and on an entry tied to a V-number that origin/main's golden for the cell does not name (its KNOWN-DEFECT and FIXED lines). Checked by mutation (`go test -overlay`): the table without one entry, with one stale entry, and with one entry tied to V12 each fail.
- **Its renderings:** [`parity.golden`](../../pkg/screens/dns/testdata/golden/parity.golden), every cell with both end states whole, what agrees and what differs; and the region of §4 below. `make golden-update` rewrites both; without it, the test fails when either is not what it makes, so the page and the code cannot disagree.

## 3. Host states

What each strategy of origin/main stands for, and the host the new path ran on:

| origin/main's strategy | Its probes (`file_dns_repository.go:38-54`) | The host state | The new path's host |
|---|---|---|---|
| systemd-resolved | `systemctl is-active systemd-resolved` succeeds | `/etc/resolv.conf` linked to resolved's stub, systemd-resolved running | the link to `../run/systemd/resolve/stub-resolv.conf`, resolved answering `resolvectl`, eth0 with its DHCP server 192.0.2.1 and search domain example.net |
| resolvconf | that fails, `which resolvconf` succeeds | resolvconf managing `/etc/resolv.conf` | the link to `../run/resolvconf/resolv.conf` |
| direct | both fail | a regular `/etc/resolv.conf` | Debian 13: dhcpcd's file, dhcpcd installed; Alpine 3.23: udhcpc's file, its stock `udhcpc.conf`; Proxmox VE 9: the installer's file; Ubuntu 24.04 (forced): `search example.net`, `nameserver 192.0.2.1`, resolved stopped |
| the empty list | the stock probes (`defaultHostProbes`) | each stock install | as above: Ubuntu's stub link, a file elsewhere |

What each distribution's stock install has ([dns.md](../safety/dns.md) §3, with its sources): Debian 12 and 13, a regular file (systemd-resolved optional, not recommended), Debian 12's cloud image a link to resolved's uplink file; Ubuntu 22.04 to 26.04, resolved's stub link; Alpine, the file udhcpc writes; Proxmox VE, the installer's static file; Debian's resolvconf package links the file to `../run/resolvconf/resolv.conf`. So of origin/main's sixteen cells for (a), eight are hosts a stock install has (§4's first table, "real"), seven are forced, and one cannot exist:

- **systemd-resolved on Alpine is not compared.** Alpine runs OpenRC, with no systemd (`dns.HostOf`: Systemd No, which the test checks); origin/main's harness made `systemctl is-active` succeed there, which no Alpine host does (`systemctl` is absent there: v032-reference.md §4.5; side-effects.md §4.4). On a real Alpine, origin/main takes the direct strategy: the row `direct` on Alpine 3.23.
- **The direct strategy on a link** (NetworkManager's, connman's) is not a cell: the characterization's mock file system holds no `/etc/resolv.conf`. There origin/main's probes pick direct and write through the link (V19; side-effects.md row 23); the new path refuses (`DNS_MANAGED`, P-37 (a)). §5 lists it, from the code.
- The menu's items (b) ran on each distribution's stock install: origin/main's are the same on every distribution, as nothing on that path reads the host.

## 4. The table

<!-- TestParity: begin. Generated by TestParity (pkg/screens/dns); make golden-update rewrites it. Do not edit. -->

**Host states** (§3): what the new path ran on, and whether the distribution's stock install has it.

| Scenario | Debian 13 | Ubuntu 24.04 | Alpine 3.23 | Proxmox VE 9 |
|---|---|---|---|---|
| resolved | resolved's stub (forced) | resolved's stub (real) | resolved's stub (cannot exist) | resolved's stub (forced) |
| resolvconf | resolvconf's link (forced) | resolvconf's link (forced) | resolvconf's link (forced) | resolvconf's link (forced) |
| direct | a file (real) | a file (forced) | a file (real) | a file (real) |
| empty | a file (real) | resolved's stub (real) | a file (real) | a file (real) |

The menu's items ran on each distribution's stock install.

**Agreeing and differing aspects** (§4): in how many compared cells each aspect agrees and differs.

| Aspect | Agrees in | Differs in |
|---|---|---|
| refusal | 31 | 20 |
| nameservers | 7 | 0 |
| hardn.yml | 15 | 36 |
| files | 16 | 35 |
| search | 0 | 7 |
| directories | 38 | 13 |
| commands | 37 | 14 |
| write | 0 | 27 |
| backup | 0 | 27 |
| journal | 0 | 27 |
| findings | 17 | 34 |
| dry run | 0 | 15 |
| mode | 4 | 0 |
| other lines | 3 | 1 |
| hardn.yml list | 20 | 0 |
| hardn.yml mode | 20 | 0 |
| hardn.yml rest | 0 | 20 |

**Differences** (§4): each with its source; every cell, both end states whole, is in [parity.golden](../../pkg/screens/dns/testdata/golden/parity.golden).

| Scenarios | On | Aspect | origin/main | New path | Source |
|---|---|---|---|---|---|
| resolved | debian, ubuntu, proxmox | files | `/etc/systemd/resolved.conf 0644` | `/etc/systemd/resolved.conf.d/zz-hardn.conf 0644` | V19: resolved.conf overwritten whole; now a drop-in of hardn's own, resolved.conf untouched |
| resolved | debian, ubuntu, proxmox | search | `Domains=lan` | `none set` | V19; decision 2: Domains=lan forced; now none set, the host's search domains kept |
| resolved | debian, ubuntu, proxmox | directories | `none` | `/etc/systemd/resolved.conf.d 0755, chmod /etc/systemd/resolved.conf.d 0755` | V19: the drop-in's directory, set 0755 whatever the umask (dns.md §2.12) |
| resolved | debian, ubuntu, proxmox | commands | `systemctl restart systemd-resolved` | `systemctl try-reload-or-restart systemd-resolved.service` | V19: a restart; now a reload where resolved has one (systemd 256 and later: Debian 13), a restart before (Ubuntu 24.04, systemd 255; dns.md §3) |
| resolved (debian, ubuntu, proxmox); direct (all) | as listed | write | `in place` | `atomic` | V18: written in place; now replaced whole by a rename |
| resolved | debian, ubuntu, proxmox | backup | `none` | `/etc/systemd/resolved.conf.d/zz-hardn.conf (absent before: the rollback removes it)` | V18: nothing recorded; now the drop-in is recorded absent, so a rollback removes it |
| resolved (debian, ubuntu, proxmox); direct (all) | as listed | journal | `none` | `journalled first` | V18: no rollback; now journalled first, so a failure rolls it back |
| resolved (debian, ubuntu, proxmox); add (ubuntu); add-ipv6 (ubuntu); remove (ubuntu); cloudflare (ubuntu); google (ubuntu); quad9 (ubuntu); add-dry-run (ubuntu) | as listed | findings | `none` | `DNS_PER_LINK` | P-37 (a); decision 5: eth0's own DHCP server, which resolved still queries, reported |
| resolved (debian, ubuntu, proxmox); direct (all) | as listed | dry run | `writes and runs as the live run (no layer below the menus reads cfg.DryRun)` | `the plan alone: nothing written` | V1: below the menus cfg.DryRun is ignored (A1), and hardn -n -g is refused (exit 2); a dry run is now the plan alone |
| resolvconf | all | refusal | `none` | `DNS_MANAGED` | P-37 (a); decision 4: resolvconf manages DNS here: reported, not written |
| resolvconf | all | files | `/etc/resolvconf/resolv.conf.d/head 0644` | `none` | P-37 (a); decision 4: resolvconf's head written; now nothing |
| resolvconf | all | directories | `/etc/resolvconf/resolv.conf.d 0755` | `none` | P-37 (a); decision 4: resolvconf's directory made; now nothing |
| resolvconf | all | commands | `resolvconf -u` | `none` | P-37 (a); decision 4: resolvconf -u run; now nothing |
| resolvconf | all | dry run | `writes and runs as the live run (no layer below the menus reads cfg.DryRun)` | `refused (DNS_MANAGED): nothing written` | V1: below the menus cfg.DryRun is ignored (A1), and hardn -n -g is refused (exit 2); a dry run is now the plan alone |
| direct | all | search | `domain lan, search lan` | `search example.net` | V19; decision 2: domain and search lan forced; now the file's search line kept |
| direct | debian | other lines | `none` | `# Generated by dhcpcd from eth0.dhcp` | V19: the file rewritten whole; now its other lines kept (dns.md §2.8) |
| direct | all | backup | `none` | `/etc/resolv.conf` | V18: nothing backed up; now the file is, first |
| direct, add, add-ipv6, remove, cloudflare, google, quad9, add-dry-run | debian | findings | `none` | `DNS_DHCP_CLIENT` | decision 5: dhcpcd may rewrite the file on a lease: reported |
| direct, add, add-ipv6, remove, cloudflare, google, quad9, add-dry-run | alpine | findings | `none` | `DNS_UDHCPC` | decision 5; decision 6: udhcpc rewrites the file on each lease (not stopped): reported |
| direct, add, add-ipv6, remove, cloudflare, google, quad9, add-dry-run | proxmox | findings | `none` | `DNS_PROXMOX_NODE` | decision 5: Proxmox VE's DNS settings rewrite the file when saved: reported |
| empty | all | refusal | `none` | `DNS_NO_NAMESERVERS` | V19: an empty list applied; now refused before any change |
| empty | debian, alpine, proxmox | files | `/etc/resolv.conf 0644` | `none` | V19: DNS= or a file with no nameserver written; now nothing |
| empty | all | dry run | `writes and runs as the live run (no layer below the menus reads cfg.DryRun)` | `refused (DNS_NO_NAMESERVERS): nothing written` | V1: below the menus cfg.DryRun is ignored (A1), and hardn -n -g is refused (exit 2); a dry run is now the plan alone |
| empty | ubuntu | files | `/etc/systemd/resolved.conf 0644` | `none` | V19: DNS= or a file with no nameserver written; now nothing |
| empty | ubuntu | commands | `systemctl restart systemd-resolved` | `none` | V19: resolved restarted with no servers; now nothing |
| add, remove, cloudflare, google, quad9 | debian, alpine, proxmox | files | `none` | `/etc/resolv.conf 0644` | decision 1: hardn.yml alone; now the host's nameservers too, then hardn.yml |
| add, remove, cloudflare, google, quad9 | all | write | `in place` | `atomic` | V18; plan §3.19: ./hardn.yml written in place; now each file replaced whole by a rename |
| add, remove, cloudflare, google, quad9 | debian, alpine, proxmox | backup | `none` | `/etc/resolv.conf, /etc/hardn/hardn.yml` | V18: nothing backed up; now the host's file and hardn.yml are, first |
| add, remove, cloudflare, google, quad9 | all | journal | `none` | `journalled first` | V18: no rollback; now journalled, so a failed save rolls the host back too (decision 1 (iii)) |
| add, remove, cloudflare, google, quad9 | all | hardn.yml | `./hardn.yml, in the working directory` | `/etc/hardn/hardn.yml, the file loaded` | side-effects row 29; plan §3.19: ./hardn.yml, last in hardn's search order (interaction.md §10); now the file loaded |
| add, remove, cloudflare, google, quad9 | all | hardn.yml rest | `48 settings; the file's 4 comment lines dropped` | `8 settings; the file's 4 comment lines kept` | side-effects row 29; plan §3.19: every setting written, comments dropped; now the list's lines alone |
| add, remove, cloudflare, google, quad9 | ubuntu | files | `none` | `/etc/systemd/resolved.conf.d/zz-hardn.conf 0644` | decision 1: hardn.yml alone; now the host's nameservers too, then hardn.yml |
| add, remove, cloudflare, google, quad9 | ubuntu | directories | `none` | `/etc/systemd/resolved.conf.d 0755, chmod /etc/systemd/resolved.conf.d 0755` | decision 1: nothing on the host; now resolved's drop-in directory |
| add, remove, cloudflare, google, quad9 | ubuntu | commands | `none` | `systemctl try-reload-or-restart systemd-resolved.service` | decision 1: nothing on the host; now resolved reloaded, or restarted on systemd 255 (dns.md §3) |
| add, remove, cloudflare, google, quad9 | ubuntu | backup | `none` | `/etc/systemd/resolved.conf.d/zz-hardn.conf (absent before: the rollback removes it), /etc/hardn/hardn.yml` | V18: nothing backed up; now hardn.yml is, first, and the drop-in recorded absent |
| add-unusable | all | refusal | `none` | `Invalid IP address format` | decision 9: 999.1.1.1 passed the four-part check; dns.Validate refuses it |
| add-unusable | all | hardn.yml | `./hardn.yml, in the working directory` | `not saved` | decision 9: saved with 999.1.1.1; now nothing saved |
| add-ipv6 | all | refusal | `Invalid IP address format` | `none` | decision 9: the four-part check refused IPv6; dns.Validate takes it |
| add-ipv6 | debian, alpine, proxmox | files | `none` | `/etc/resolv.conf 0644` | decision 9; decision 1: nothing; now the host's nameservers, then hardn.yml |
| add-ipv6 | all | hardn.yml | `not saved` | `/etc/hardn/hardn.yml, the file loaded` | decision 9: nothing saved; now saved at the file loaded |
| add-ipv6 | ubuntu | files | `none` | `/etc/systemd/resolved.conf.d/zz-hardn.conf 0644` | decision 9; decision 1: nothing; now the host's nameservers, then hardn.yml |
| add-ipv6 | ubuntu | directories | `none` | `/etc/systemd/resolved.conf.d 0755, chmod /etc/systemd/resolved.conf.d 0755` | decision 9; decision 1: nothing; now resolved's drop-in directory |
| add-ipv6 | ubuntu | commands | `none` | `systemctl try-reload-or-restart systemd-resolved.service` | decision 9; decision 1: nothing; now resolved reloaded, or restarted on systemd 255 (dns.md §3) |
| remove-last | all | refusal | `none` | `The last nameserver cannot be removed.` | decision 1 (ii): the last nameserver removed; now refused before asking |
| remove-last | all | hardn.yml | `./hardn.yml, in the working directory` | `not saved` | decision 1 (ii); V19: saved with no nameservers, which -g and Run All apply; now nothing saved |
| add-dry-run | all | hardn.yml | `./hardn.yml, in the working directory` | `not saved` | side-effects row 29; screen-dns.md §2.8: saved under a dry run, dryRun: true in it; now a dry run saves nothing |

**v0.3.2** (§4): where it differs from origin/main, from [v032-reference.md](../refactor/phase0/v032-reference.md) §4.5, §5.1, §6.

| Scenarios | v0.3.2 |
|---|---|
| resolved | the same file, but backed up first: the backups regressed (V18, V19) |
| resolvconf | the same, the first 2 nameservers only, backed up first: the backups regressed (V18, V19) |
| direct | the same 4 lines, the first 2 nameservers only, backed up first: the backups regressed (V18, V19) |
| empty | resolved: DNS= and a restart; a file: a backup, then a panic. v0.3.2's own defect, not carried over (§6 item 3) |
| add, add-unusable, add-ipv6, remove, remove-last, cloudflare, google, quad9, add-dry-run | no such item: v0.3.2 had no DNS screen |

<!-- TestParity: end -->

## 5. What origin/main's path did that the new one does not

From the table, and, where marked, from the code (no golden shows it):

1. **It changed DNS on selection, with no question:** `-g` and the menu's Configure DNS (interaction.md §5, anti-pattern #1 (d)), and Add, Remove and the presets, which saved hardn.yml at once. The screen asks y/N before any change (decision 1; no timer, brief 2.4); `-g` is unit (d)'s question (§7).
2. **It forced `lan`**: `domain lan` and `search lan` in a file, `Domains=lan` for resolved (V19). The host's search domains are kept (decision 2).
3. **It overwrote `/etc/systemd/resolved.conf` whole and restarted systemd-resolved** (V19). The new path writes its own drop-in, `zz-hardn.conf`, and reloads where resolved can (dns.md §2.7).
4. **It configured resolvconf** (its `head`, then `resolvconf -u`; side-effects.md row 22). The new path reports resolvconf and changes nothing (P-37 (a), decision 4).
5. **It applied an empty list:** `DNS=` and a restart, or a file with no nameserver (V19). Refused now (`DNS_NO_NAMESERVERS`).
6. **It wrote through a `/etc/resolv.conf` link** with the direct strategy, and never detected NetworkManager (V19; *from the code*, `file_dns_repository.go:184-187`, and side-effects.md row 23). The new path classifies the link and refuses what it does not manage (P-37 (a)).
7. **It chose the strategy by `which resolvconf`**, a binary on the PATH (V19: NetworkManager not detected; *the probes, recorded in the golden, not compared*). The new path reads what `/etc/resolv.conf` is (dns.md §2.3, brief 2.5).
8. **It wrote every nameserver into a file** (*from the code*, `file_dns_repository.go:179-182`), where glibc and musl read three. The new path refuses a fourth on a file (`DNS_INVALID`; dns.md §2.5: V19's fix, "validate the list"). Not exercised: the fixture lists two.
9. **It wrote in place, backed nothing up and could not roll back** (V18). The new path replaces each file atomically, backs it up first, journals the change, and rolls back a failed save's DNS change too (decision 1 (iii)).
10. **Its Add, Remove and presets changed hardn.yml alone** (decision 1), **to `./hardn.yml`** in the working directory, last in hardn's search order (side-effects.md row 29; interaction.md §10; plan §3.19), **whole**: 48 settings, every default pinned, the file's comments dropped, `dryRun` included (row 29; plan §3.19 as amended 2026-10-22), **and under a dry run** (row 29; screen-dns.md §2.8). The file loaded was left as it was.
11. **Add took any four dot-separated parts** (999.1.1.1) **and refused IPv6** (decision 9; interaction.md §10).
12. **Remove emptied the list**, which `-g` and Run All then applied (decision 1 (ii); V19).
13. **Add's and Remove's message was wiped at once** by the redraw (W1, W2; *from the code*, `dns_menu.go:162-178`). The screen keeps the result above the screen it draws again (screen-dns.md §2.11).
14. **The menu's Configure DNS, beyond (a)** (*from the code*, `dns_menu.go:132-160`): with no nameserver configured it refused, "No nameservers configured. Please add nameservers first.", as the screen refuses ("Please add nameservers first.", decision 3); under `cfg.DryRun` it printed "[DRY-RUN] Would configure DNS with nameservers: …" and wrote nothing, as the screen's dry run writes nothing: both agree in effect. On success it printed "DNS configured successfully" and the list, then went back to the main menu after Enter.
15. **`-g` exited 0 after a failure** (V25), logging "Failed to configure DNS: …" or "DNS configured successfully", then "Script completed selected hardening operations." (*from the code*, `cmd/hardn/main.go:395-402`, 418-422).

The new path does what origin/main did not: it reports what may undo the change (decision 5: dhcpcd, udhcpc, Proxmox VE's settings, and on resolved each link's own servers, P-37 (a)); on Alpine it can stop udhcpc rewriting the file (decision 6, answered n here).

## 6. UNEXPLAINED differences

**None.** Every difference `TestParity` finds has an entry in `parityTable` with a source that says it; a difference with no entry fails the test and is marked UNEXPLAINED in the golden and in §4.

## 7. For unit (d)

**What `-g` did on origin/main:** as root (`main.go:143`), after loading hardn.yml (`:151`), with no question and no TTY check, `DNSManager.ConfigureDNS(cfg.Nameservers, "lan")`: §4's (a) rows, the `lan` domain forced, an empty list applied; a failure logged, exit status 0 (V25); `hardn -n -g` refused with exit status 2 before anything runs (V1's P0.8 gate, `dryRunFlagApplies`).

**Open, for unit (d) to settle (not decided here):** what `-g` does on the new control. screen-dns.md §7 says "to the control without the screen (`dnsctl.Controls(cfg)` through the engine, `output.Human`)"; this table compared that, `Plan` then `Apply`. Still open:
- whether `-g` asks before applying (a y/N on a TTY; without one, plan §3.11's proposed "operation flags require `--yes` without a TTY", which `next/p3-output` left unbuilt until §3.11 is approved), or applies as origin/main did;
- on a host every plan refuses (resolvconf, NetworkManager, a container, an empty list): the report and the exit status (origin/main wrote, and exited 0);
- under `-n`: refused as today (V1's gate), or the plan, which is the new path's dry run;
- on Alpine, udhcpc's question (decision 6) has no form under `-g`: not asked (`StopUDHCPC` false, the finding reported, as this table ran it), or a flag (a surface change, plan §3.11's kind).

**What it may now delete:** `pkg/menu/dns_menu.go`, which has no goldens of its own (`pkg/menu/testdata/golden` holds `main_menu` and `run_all` only); screen-dns.md §7 lists the rest.

**The goldens this test reads stay readable:** `TestParity` reads origin/main's goldens in place, `ops/dns_*` (which stay while the legacy DNS service does, slice 8) and `menu_dns/*` (which describe `dns_menu.go`'s items). Whoever deletes them, or their tests, moves the files unchanged under `pkg/screens/dns/testdata/` and points `legacyGoldens` there, or the record is lost and the test fails.

## 8. Written, not run (gate 5)

Nothing here ran on Linux or as root. The new path's end states are the fake's, with a model of systemd-resolved; origin/main's are its mocks'. What a host would show is control-dns.md §4's and screen-dns.md §4's: each stock host's `/etc/resolv.conf` or drop-in after `-g` and each item, and whether resolved reloads (Debian 13) or restarts (Ubuntu 24.04, systemd 255; dns.md §3). The mode of origin/main's `./hardn.yml` is what `os.WriteFile` gives under umask 022.

## 9. Files and size

- **Added:** `pkg/characterization/menu_dns_test.go`; `pkg/characterization/testdata/fixtures/config/dns-menu.yml`, `dns-menu-one.yml`, `dns-menu-empty.yml`; `pkg/characterization/testdata/golden/menu_dns/{add,add-unusable,add-ipv6,remove,remove-last,cloudflare,google,quad9,add-dry-run}.golden`; `pkg/screens/dns/parity_test.go`, `parity_state_test.go`, `parity_render_test.go`; `pkg/screens/dns/testdata/golden/parity.golden`; this page.
- **Changed:** control-dns.md §11 and screen-dns.md §7. No legacy code, no non-test Go and no existing golden.
- **Size:** about 1,530 lines of test Go (`TestParity`'s three files about 1,290, `TestMenuDNSGoldens` about 245), this page about 185 lines; testdata 13 files, about 2,300 lines (the parity golden 1,584). Over the guideline of about 500 changed lines because the table is a test: a parser of origin/main's goldens, an extractor of the new path's end state, the comparison, the table itself (about 50 entries) and two renderings, plus the new characterization scenario (b) needed. No non-test Go; `go.mod` and `go.sum` unchanged.
