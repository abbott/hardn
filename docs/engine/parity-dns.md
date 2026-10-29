# The DNS parity table: origin/main's DNS path against the new one (`TestParity`)

> **Unit:** refactor plan slice 2 (DNS, the pilot), unit (b2), `next/s2-parity`: "Parity table: end state (file contents, …) of legacy vs new path, per fixture distro, with each difference tied to a V-number or an approved proposal. The baseline is the legacy (origin/main) path; the P0.7b v0.3.2 reference shows where origin/main regressed" (plan §2, "Phases 3-5 together", §3.22; [control-dns.md](control-dns.md) §11's (b2) item). Sources the table ties to: V1, V18, V19 ([side-effects.md](../refactor/phase0/side-effects.md) §8); side-effects rows 23 and 29 (§3); decisions 1, 1 (ii), 1 (iii), 2, 4, 5, 6 and 9 of [gallery-dns.md](gallery-dns.md) §4 (adopted 2026-10-22); [P-37](../refactor/proposals.md#p-37) (a); plan §3.19 as amended 2026-10-22; [control-dns.md](control-dns.md) §2.6, §2.8, §8 D1 and decision 11; [screen-dns.md](screen-dns.md) §2.8, §2.9 and §8 decision 12; [dns.md](../safety/dns.md) §2.7, §2.8, §2.12 and §3; interaction.md §10.
> **Status:** built and tested on macOS, in process: origin/main from `pkg/characterization`'s goldens, thirteen of them new in this unit; the new path on the recording fake (`hostfake`) with the resolver model of the DNS screen's tests (written from [dns.md](../safety/dns.md) §3, not captured from a host). Nothing ran on Linux: every host claim is **written, not run (gate 5)**. No legacy code and no existing golden changed. *Since unit (d), `next/s2-replace` (2026-10-23): origin/main's goldens and fixtures are read from a frozen copy, `pkg/screens/dns/testdata/legacy`, as at e666243 (§7); the table, `parity.golden` and §4 are unchanged.*

## 1. Why

Unit (d) replaces the legacy DNS path rather than gating it (the maintainer, 2026-10-22: `HARDN_UI=classic` withdrawn from plan §3.14): menu item 3 and `-g` go to the new control, and `pkg/menu/dns_menu.go` is deleted. This page and its test are the record of what that path did, kept after its code is gone: every difference between its end state and the new path's, on each distribution hardn supports, with the source that explains it.

## 2. Method

- **origin/main, (a):** `DNSManager.ConfigureDNS(cfg.Nameservers, "lan")`, which `-g` (`cmd/hardn/main.go:395-402`), the DNS menu's Configure DNS (`pkg/menu/dns_menu.go:150`) and Run All (`SecurityManager.HardenSystem`, `pkg/application/security_manager.go:68-75`, from `hardn -r` and the menu's Run All) all call. From `pkg/characterization`'s goldens, read in place (*since `next/s2-replace`, from their copy as at e666243 under `pkg/screens/dns/testdata/legacy`, §7*): `ops/dns_{systemd_resolved,resolvconf,direct,empty_nameservers}/{debian,ubuntu,alpine,proxmox}.golden`, the real managers and adapters over the repo's mocks: each command, each write with its mode and content, each `mkdir`, both `cfg.DryRun` sections and `hardn -n -g`'s refusal. The harness forces each strategy by its probes' results, whatever the distribution ships (§3). `ops/dns_direct` also stands for NetworkManager's link (`nm-link`): origin/main's probes take the direct strategy there too; the golden's mock holds no link, so that its write follows the link into NetworkManager's file is *from the code* (`os.WriteFile`; `file_dns_repository.go:184-187`; side-effects.md row 23).
- **origin/main, (b):** the DNS menu's Add, Remove and presets, which no golden pinned. New in this unit: `TestMenuDNSGoldens` (`pkg/characterization/menu_dns_test.go`), thirteen goldens under `menu_dns/`. **The list edit and the message are from the code**, mirrored (`dnsMenuEdit`, from `dns_menu.go:180-342`): the menu cannot run in a test, since `DNSMenu.Show` runs `systemctl` and `resolvectl` and reads `/etc/resolv.conf` for real, and its handlers are unexported. **The save is the real `config.SaveConfig`**, in a scratch working directory (`t.Chdir`), on a configuration `config.LoadConfig` read from a scratch copy of the fixture, as `-f` does, with the umask set to 022. The four `-no-file` goldens start with no hardn.yml loaded: `config.DefaultConfig()`, what `config.LoadConfig` gives when it finds none and n is answered at its question, *from the code* (`pkg/config/config.go:271-287, 362`; the question reads stdin).
- **The new path** (`TestParity`, `pkg/screens/dns/parity_test.go`, through the parity harness, `parity_harness_test.go`, §8; *since `next/s3-shared`, the package `internal/parity`, [screen-kit.md](screen-kit.md)*): (a) the DNS control through the engine, `Plan` then `Apply`, as `-g` is to run it (screen-dns.md §7); its dry run, the plan alone; and the screen's Configure DNS, which must leave the host exactly as the apply does, and its dry run nothing. (b) The screen's items through `Run`, answered as typed, Alpine's udhcpc question answered n (the default): on each distribution's stock install; on resolvconf's link (`@resolvconf`), a host every plan refuses, where only the item's key is typed, since the screen refuses before it asks (screen-dns.md §2.9), the refusal checked by code and by its words; and with no hardn.yml loaded (`@no-file`). **(a) saves no hardn.yml**: its list is the file's own, and the control plans the save only when the list differs from the one the file holds (`pkg/engine/controls/dns/dns.go:278-284`, `savedList`; control-dns.md decision 8); the table's hardn.yml aspect agrees, "not saved", in every (a) cell.
- **The same configuration:** hardn.yml holds the characterization fixture's settings (`pkg/characterization/testdata/fixtures/config/dns-menu.yml`, *since `next/s2-replace` at `pkg/screens/dns/testdata/legacy/fixtures/config/dns-menu.yml`, unchanged, §7*: alice, port 2230, nameservers 192.0.2.53 and 192.0.2.54, four comment lines; `-one` and `-empty` variants), loaded from `/etc/hardn/hardn.yml` (0644) on the new path, and as `-f` on origin/main's; `@no-file`, none on either.
- **The same distributions:** the characterization's cells, Debian 13, Ubuntu 24.04, Alpine 3.23 and Proxmox VE 9 (Debian 13 with `/etc/pve`).
- **The aspects compared:** the refusal; the files written (path, mode); in the files both paths wrote, whatever their paths, the nameservers they set, in order, their search domain lines and **every other line**, so resolved.conf is compared with the drop-in line by line; the mode of a file both wrote at one path; the directories made; the commands that change something; the write (in place or atomic), the backup and the journal; the findings; hardn.yml: where it is saved, its list, **every other setting by value**, as hardn loads the file (the defaults, the file's keys, then `LoadConfig`'s `LogsConfig` line) against the file loaded, the rest of its lines (kept, or written whole and the comments dropped), and its mode; the dry run. An aspect of what was written is compared only when both paths wrote. **Probes**, the commands that only read, are recorded in the golden but not compared: they leave no end state. The backups and the journal are noted, not compared byte for byte.
- **The table is a test:** `dnsTable` (`parity_test.go`) holds every difference, keyed by scenario, distribution and aspect, with its source and one line. `TestParity` fails on a difference not in it (reported UNEXPLAINED), on an entry whose difference no longer occurs, and on an entry tied to a V-number that origin/main's golden for the cell does not name (its KNOWN-DEFECT and FIXED lines). The parsers fail on what they do not compare: a removal, a failed command or write, a failed run in origin/main's trace; a removal, rename, `chown`, append or detached command (a confirm-or-revert timer) on the new path. Checked by mutation (`go test -overlay`, origin/main's goldens copied): an entry removed, a stale entry and an entry tied to V12 each fail; `options rotate` added to origin/main's file on Ubuntu, and `sshPort: 22` and `permitRootLogin: true` in its saved hardn.yml, each fail UNEXPLAINED; a `remove` line in its trace fails the parser.
- **Its limits**, stated: (1) a difference already tied whose values change (`DNSSEC=yes` added to origin/main's resolved.conf, whose other lines already differ) fails only on `parity.golden` and the page, which `make golden-update` accepts: the review of that diff is the guard; (2) a `-run` filter checks only the cells it runs, and the table, the golden and the page are checked, and written under `-update`, only when every cell ran; (3) origin/main's whole saved hardn.yml is in its own golden, not in `parity.golden`; (4) origin/main's end states are its mocks', which record what it wrote, not what a host then held.
- **Its renderings:** [`parity.golden`](../../pkg/screens/dns/testdata/golden/parity.golden), every cell with both end states whole, what agrees and what differs; and the region of §4 below. `make golden-update` rewrites both: the one golden in the repository that writes outside `testdata` ([README](../design-system/README.md), "Running and regenerating"); the makefile's comment on `golden-update` still says `testdata/golden`. Without `-update`, the test fails when either is not what it makes, naming the first line that differs, so the page and the code cannot disagree.

## 3. Host states

What each strategy of origin/main stands for, and the host the new path ran on:

| origin/main's strategy | Its probes (`file_dns_repository.go:38-54`) | The host state | The new path's host |
|---|---|---|---|
| systemd-resolved | `systemctl is-active systemd-resolved` succeeds | `/etc/resolv.conf` linked to resolved's stub, systemd-resolved running | the link to `../run/systemd/resolve/stub-resolv.conf`, resolved answering `resolvectl`, eth0 with its DHCP server 192.0.2.1 and search domain example.net |
| resolvconf | that fails, `which resolvconf` succeeds | resolvconf managing `/etc/resolv.conf` | the link to `../run/resolvconf/resolv.conf` |
| direct | both fail | a regular `/etc/resolv.conf` | Debian 13: dhcpcd's file, dhcpcd installed; Alpine 3.23: udhcpc's file, its stock `udhcpc.conf`, ifupdown-ng running udhcpc for eth0 (no dhcpcd; since `next/s2-udhcpc`, which reports udhcpc only where it is the client: no cell changed); Proxmox VE 9: the installer's file; Ubuntu 24.04 (forced): `search example.net`, `nameserver 192.0.2.1`, resolved stopped |
| direct (`nm-link`) | both fail | `/etc/resolv.conf` linked to NetworkManager's file | the link to `/run/NetworkManager/resolv.conf`, resolved stopped |
| the empty list | the stock probes (`defaultHostProbes`) | each stock install | as above: Ubuntu's stub link, a file elsewhere |

What each distribution's stock install has ([dns.md](../safety/dns.md) §3, with its sources): Debian 12 and 13, a regular file (systemd-resolved optional, not recommended), Debian 12's cloud image a link to resolved's uplink file; Ubuntu 22.04 to 26.04, resolved's stub link; Alpine, the file udhcpc writes; Proxmox VE, the installer's static file; Debian's resolvconf package links the file to `../run/resolvconf/resolv.conf`; NetworkManager keeps its file at `/run/NetworkManager/resolv.conf`, a link none of the four stock installs has. So of origin/main's twenty cells for (a), eight are hosts a stock install has (§4's first table, "real"), eleven are forced, and one cannot exist:

- **systemd-resolved on Alpine is not compared.** Alpine runs OpenRC, with no systemd (`dns.HostOf`: Systemd No, which the test checks); origin/main's harness made `systemctl is-active` succeed there, which no Alpine host does (`systemctl` is absent there: v032-reference.md §4.5; side-effects.md §4.4). On a real Alpine, origin/main takes the direct strategy: the row `direct` on Alpine 3.23.
- **The menu's items (b)** ran on each distribution's stock install, then on resolvconf's link (`@resolvconf`), then with no hardn.yml loaded (`@no-file`): origin/main's are the same on every host, as nothing on that path reads the host, so one golden per item stands for every host.

## 4. The table

<!-- TestParity: begin. Generated by TestParity (pkg/screens/dns); make golden-update rewrites it. Do not edit. -->

**Host states** (§3): what the new path ran on, and whether the distribution's stock install has it (real), the harness forced it, or it cannot exist.

| Scenarios | Debian 13 | Ubuntu 24.04 | Alpine 3.23 | Proxmox VE 9 |
|---|---|---|---|---|
| resolved | resolved's stub (forced) | resolved's stub (real) | resolved's stub (cannot exist) | resolved's stub (forced) |
| resolvconf, add@resolvconf, add-unusable@resolvconf, add-ipv6@resolvconf, remove@resolvconf, remove-last@resolvconf, cloudflare@resolvconf, google@resolvconf, quad9@resolvconf, add-dry-run@resolvconf | resolvconf's link (forced) | resolvconf's link (forced) | resolvconf's link (forced) | resolvconf's link (forced) |
| direct | a file (real) | a file (forced) | a file (real) | a file (real) |
| nm-link | NetworkManager's link (forced) | NetworkManager's link (forced) | NetworkManager's link (forced) | NetworkManager's link (forced) |
| empty, add, add-unusable, add-ipv6, remove, remove-last, cloudflare, google, quad9, add-dry-run, add@no-file, cloudflare@no-file, google@no-file, quad9@no-file | a file (real) | resolved's stub (real) | a file (real) | a file (real) |

**Agreeing and differing aspects** (§4): in how many compared cells each aspect agrees and differs.

| Aspect | Agrees in | Differs in |
|---|---|---|
| refusal | 31 | 76 |
| nameservers | 7 | 0 |
| hardn.yml | 23 | 84 |
| files | 68 | 39 |
| search | 0 | 7 |
| other lines | 3 | 4 |
| directories | 94 | 13 |
| commands | 93 | 14 |
| write | 0 | 27 |
| backup | 0 | 27 |
| journal | 0 | 27 |
| findings | 73 | 34 |
| dry run | 0 | 19 |
| mode | 4 | 0 |
| hardn.yml nameservers | 20 | 0 |
| hardn.yml settings | 20 | 0 |
| hardn.yml mode | 20 | 0 |
| hardn.yml rest | 0 | 20 |

**Differences** (§4): each with its source; every cell, both end states whole, is in [parity.golden](../../pkg/screens/dns/testdata/golden/parity.golden).

| Scenarios | On | Aspect | origin/main | New path | Source |
|---|---|---|---|---|---|
| resolved | Debian 13, Ubuntu 24.04, Proxmox VE 9 | files | `/etc/systemd/resolved.conf 0644` | `/etc/systemd/resolved.conf.d/zz-hardn.conf 0644` | V19: resolved.conf overwritten whole; now a drop-in of hardn's own, resolved.conf untouched |
| resolved | Debian 13, Ubuntu 24.04, Proxmox VE 9 | search | `Domains=lan` | `none set` | V19; decision 2: Domains=lan forced; now none set, the host's search domains kept |
| resolved | Debian 13, Ubuntu 24.04, Proxmox VE 9 | other lines | `[Resolve]` | `# Written by hardn: the system DNS servers for systemd-resolved., # An empty assignment resets the list that earlier files built., [Resolve]` | V19; dns.md §2.7: resolved.conf's section; now the drop-in's, under two lines saying it is hardn's |
| resolved | Debian 13, Ubuntu 24.04, Proxmox VE 9 | directories | `none` | `/etc/systemd/resolved.conf.d 0755, chmod /etc/systemd/resolved.conf.d 0755` | V19; dns.md §2.12: the drop-in's directory, set 0755 whatever the umask |
| resolved | Debian 13, Ubuntu 24.04, Proxmox VE 9 | commands | `systemctl restart systemd-resolved` | `systemctl try-reload-or-restart systemd-resolved.service` | V19; dns.md §2.7, §3: a restart; now a reload on systemd 256 and later, a restart before (Ubuntu 24.04) |
| resolved (Debian 13, Ubuntu 24.04, Proxmox VE 9); direct (all) | as listed | write | `in place` | `atomic` | V18: written in place; now replaced whole by a rename |
| resolved | Debian 13, Ubuntu 24.04, Proxmox VE 9 | backup | `none` | `/etc/systemd/resolved.conf.d/zz-hardn.conf (absent before: the rollback removes it)` | V18: nothing recorded; now the drop-in is recorded absent, so a rollback removes it |
| resolved (Debian 13, Ubuntu 24.04, Proxmox VE 9); direct (all) | as listed | journal | `none` | `journalled first` | V18: no rollback; now journalled first, so a failure rolls it back |
| resolved | Debian 13, Ubuntu 24.04, Proxmox VE 9 | findings | `none` | `DNS_PER_LINK` | P-37 (a); control-dns.md §2.6, §8 D1; decision 5: eth0's own DHCP server, which resolved still queries, reported |
| resolved (Debian 13, Ubuntu 24.04, Proxmox VE 9); direct (all) | as listed | dry run | `the service writes and runs as live (cfg.DryRun unread below the menus); no path reaches it: hardn -n refused (exit status 2)` | `the plan alone: nothing written` | V1: no path reaches the service's dry run (the menu's Configure only prints); a dry run is now the plan alone |
| resolvconf | all | refusal | `none` | `DNS_MANAGED` | P-37 (a); decision 4: resolvconf manages DNS here: reported, not written |
| resolvconf | all | files | `/etc/resolvconf/resolv.conf.d/head 0644` | `none` | P-37 (a); decision 4: resolvconf's head written; now nothing |
| resolvconf | all | directories | `/etc/resolvconf/resolv.conf.d 0755` | `none` | P-37 (a); decision 4: resolvconf's directory made; now nothing |
| resolvconf | all | commands | `resolvconf -u` | `none` | P-37 (a); decision 4: resolvconf -u run; now nothing |
| resolvconf, nm-link | all | dry run | `the service writes and runs as live (cfg.DryRun unread below the menus); no path reaches it: hardn -n refused (exit status 2)` | `refused (DNS_MANAGED): nothing written` | V1: no path reaches the service's dry run (the menu's Configure only prints); a dry run is now the plan alone |
| direct | all | search | `domain lan, search lan` | `search example.net` | V19; decision 2: domain and search lan forced; now the file's search line kept |
| direct | Debian 13 | other lines | `none` | `# Generated by dhcpcd from eth0.dhcp` | V19; dns.md §2.8: the file rewritten whole; now its other lines kept |
| direct | all | backup | `none` | `/etc/resolv.conf` | V18: nothing backed up; now the file is, first |
| direct | Debian 13 | findings | `none` | `DNS_DHCP_CLIENT` | control-dns.md §2.6, §8 D1; decision 5: dhcpcd may rewrite the file on a lease: reported |
| direct | Alpine 3.23 | findings | `none` | `DNS_UDHCPC` | control-dns.md §2.6, §2.8, §8 D1; decisions 5, 6: udhcpc rewrites the file on each lease: reported, not stopped (-g asks nothing; the screen asked, n) |
| direct | Proxmox VE 9 | findings | `none` | `DNS_PROXMOX_NODE` | control-dns.md §2.6, §8 D1; decision 5: Proxmox VE's DNS settings rewrite the file when saved: reported |
| nm-link | all | refusal | `none` | `DNS_MANAGED` | P-37 (a); decision 4: NetworkManager manages DNS here: reported, not written |
| nm-link | all | files | `/etc/resolv.conf 0644` | `none` | V19; side-effects row 23; P-37 (a): written through the link, NetworkManager not detected; now nothing |
| empty | all | refusal | `none` | `DNS_NO_NAMESERVERS` | V19: an empty list applied; now refused before any change |
| empty | Debian 13, Alpine 3.23, Proxmox VE 9 | files | `/etc/resolv.conf 0644` | `none` | V19: DNS= or a file with no nameserver written; now nothing |
| empty | all | dry run | `the service writes and runs as live (cfg.DryRun unread below the menus); no path reaches it: hardn -n refused (exit status 2)` | `refused (DNS_NO_NAMESERVERS): nothing written` | V1: no path reaches the service's dry run (the menu's Configure only prints); a dry run is now the plan alone |
| empty | Ubuntu 24.04 | files | `/etc/systemd/resolved.conf 0644` | `none` | V19: DNS= or a file with no nameserver written; now nothing |
| empty | Ubuntu 24.04 | commands | `systemctl restart systemd-resolved` | `none` | V19: resolved restarted with no servers; now nothing |
| add, remove, cloudflare, google, quad9 | Debian 13, Alpine 3.23, Proxmox VE 9 | files | `none` | `/etc/resolv.conf 0644` | decision 1: hardn.yml alone; now the host's nameservers too, then hardn.yml |
| add, remove, cloudflare, google, quad9 | all | write | `in place` | `atomic` | V18; plan §3.19: ./hardn.yml written in place; now each file replaced whole by a rename |
| add, remove, cloudflare, google, quad9 | Debian 13, Alpine 3.23, Proxmox VE 9 | backup | `none` | `/etc/resolv.conf, /etc/hardn/hardn.yml` | V18: nothing backed up; now the host's file and hardn.yml are, first |
| add, remove, cloudflare, google, quad9 | all | journal | `none` | `journalled first` | V18; decision 1 (iii): no rollback; now journalled, so a failed save rolls the host back too |
| add, add-ipv6, remove, cloudflare, google, quad9, add-dry-run | Debian 13 | findings | `none` | `DNS_DHCP_CLIENT` | decision 5: dhcpcd may rewrite the file on a lease: reported |
| add, remove, cloudflare, google, quad9 | all | hardn.yml | `./hardn.yml, in the working directory` | `/etc/hardn/hardn.yml, the file loaded` | side-effects row 29; plan §3.19; interaction.md §10: ./hardn.yml, last in hardn's search order; now the file loaded |
| add, remove, cloudflare, google, quad9 | all | hardn.yml rest | `48 settings, written whole; the file's 4 comment lines dropped` | `the nameservers lines alone changed; the file's other 18 lines kept (8 settings, 4 comment lines)` | side-effects row 29; plan §3.19: every setting written, comments dropped; now the list's lines alone |
| add, remove, cloudflare, google, quad9 | Ubuntu 24.04 | files | `none` | `/etc/systemd/resolved.conf.d/zz-hardn.conf 0644` | decision 1: hardn.yml alone; now the host's nameservers too, then hardn.yml |
| add, remove, cloudflare, google, quad9 | Ubuntu 24.04 | directories | `none` | `/etc/systemd/resolved.conf.d 0755, chmod /etc/systemd/resolved.conf.d 0755` | decision 1: nothing on the host; now resolved's drop-in directory |
| add, remove, cloudflare, google, quad9 | Ubuntu 24.04 | commands | `none` | `systemctl try-reload-or-restart systemd-resolved.service` | decision 1; dns.md §3: nothing on the host; now resolved reloaded, or restarted on systemd 255 |
| add, remove, cloudflare, google, quad9 | Ubuntu 24.04 | backup | `none` | `/etc/systemd/resolved.conf.d/zz-hardn.conf (absent before: the rollback removes it), /etc/hardn/hardn.yml` | V18: nothing backed up; now hardn.yml is, first, and the drop-in recorded absent |
| add, add-ipv6, remove, cloudflare, google, quad9, add-dry-run | Ubuntu 24.04 | findings | `none` | `DNS_PER_LINK` | P-37 (a); decision 5: eth0's own DHCP server, which resolved still queries, reported |
| add, add-ipv6, remove, cloudflare, google, quad9, add-dry-run | Alpine 3.23 | findings | `none` | `DNS_UDHCPC` | decisions 5, 6: udhcpc rewrites the file on each lease: reported, not stopped (n) |
| add, add-ipv6, remove, cloudflare, google, quad9, add-dry-run | Proxmox VE 9 | findings | `none` | `DNS_PROXMOX_NODE` | decision 5: Proxmox VE's DNS settings rewrite the file when saved: reported |
| add-unusable | all | refusal | `none` | `Invalid IP address format` | decision 9: 999.1.1.1 passed the four-part check; dns.Validate refuses it |
| add-unusable | all | hardn.yml | `./hardn.yml, in the working directory` | `not saved` | decision 9: saved with 999.1.1.1; now nothing saved |
| add-ipv6 | all | refusal | `Invalid IP address format` | `none` | decision 9: the four-part check refused IPv6; dns.Validate takes it |
| add-ipv6 | Debian 13, Alpine 3.23, Proxmox VE 9 | files | `none` | `/etc/resolv.conf 0644` | decisions 9, 1: nothing; now the host's nameservers, then hardn.yml |
| add-ipv6 | all | hardn.yml | `not saved` | `/etc/hardn/hardn.yml, the file loaded` | decision 9: nothing saved; now saved at the file loaded |
| add-ipv6 | Ubuntu 24.04 | files | `none` | `/etc/systemd/resolved.conf.d/zz-hardn.conf 0644` | decisions 9, 1: nothing; now the host's nameservers, then hardn.yml |
| add-ipv6 | Ubuntu 24.04 | directories | `none` | `/etc/systemd/resolved.conf.d 0755, chmod /etc/systemd/resolved.conf.d 0755` | decisions 9, 1: nothing; now resolved's drop-in directory |
| add-ipv6 | Ubuntu 24.04 | commands | `none` | `systemctl try-reload-or-restart systemd-resolved.service` | decisions 9, 1; dns.md §3: nothing; now resolved reloaded, or restarted on systemd 255 |
| remove-last | all | refusal | `none` | `The last nameserver cannot be removed.` | decision 1 (ii): the last nameserver removed; now refused before asking |
| remove-last | all | hardn.yml | `./hardn.yml, in the working directory` | `not saved` | decision 1 (ii); V19: saved with no nameservers, which -g and Run All apply; now nothing saved |
| add-dry-run | all | hardn.yml | `./hardn.yml, in the working directory` | `not saved` | side-effects row 29; screen-dns.md §2.8: saved under a dry run, dryRun: true in it; now a dry run saves nothing |
| add@resolvconf, add-unusable@resolvconf, remove@resolvconf, remove-last@resolvconf, cloudflare@resolvconf, google@resolvconf, quad9@resolvconf, add-dry-run@resolvconf | all | refusal | `none` | `DNS_MANAGED` | P-37 (a); decision 4: resolvconf manages DNS here: every item refused before it asks |
| add@resolvconf, add-unusable@resolvconf, remove@resolvconf, remove-last@resolvconf, cloudflare@resolvconf, google@resolvconf, quad9@resolvconf, add-dry-run@resolvconf | all | hardn.yml | `./hardn.yml, in the working directory` | `not saved` | P-37 (a); decision 4: saved whatever the host; now nothing saved |
| add-ipv6@resolvconf | all | refusal | `Invalid IP address format` | `DNS_MANAGED` | P-37 (a); decision 4: resolvconf manages DNS here: every item refused before it asks |
| add@no-file, cloudflare@no-file, google@no-file, quad9@no-file | all | refusal | `none` | `CONFIG_NOT_LOADED` | control-dns.md decision 11; screen-dns.md §2.9, §8 decision 12: no hardn.yml read: refused before asking, as there is none to save to |
| add@no-file, cloudflare@no-file, google@no-file, quad9@no-file | all | hardn.yml | `./hardn.yml, in the working directory` | `not saved` | control-dns.md decision 11; screen-dns.md §2.9, §8 decision 12: ./hardn.yml saved, the defaults and the list; now nothing saved |

**v0.3.2** (§4): where it differs from origin/main, from [v032-reference.md](../refactor/phase0/v032-reference.md) §4.5, §4.13, §5.1, §6.

| Scenarios | v0.3.2 |
|---|---|
| resolved | the same file and restart, backed up first (V18, V19: regressed); -n -g ran the probes only (V1: regressed; P0.8 refuses -n -g now); menu 3 asked y/n first (regressed: gallery-dns.md §7) |
| resolvconf | the same, the first 2 nameservers only, backed up first (V18, V19: regressed); -n -g ran the probes only (V1: regressed; P0.8 refuses -n -g now); menu 3 asked y/n first (regressed: gallery-dns.md §7) |
| direct | the same 4 lines, the first 2 nameservers only, backed up first (V18, V19: regressed); -n -g ran the probes only (V1: regressed; P0.8 refuses -n -g now); menu 3 asked y/n first (regressed: gallery-dns.md §7) |
| nm-link | the same 4 lines through the link, the first 2 nameservers only, backed up first (V18, V19: regressed); -n -g ran the probes only (V1: regressed; P0.8 refuses -n -g now); menu 3 asked y/n first (regressed: gallery-dns.md §7) |
| empty | resolved: DNS= and a restart; resolvconf and a file: a backup, then a panic; -n -g panicked too. v0.3.2's defect, not carried over (v032-reference.md §4.5, §6 item 3) |
| add, add-unusable, add-ipv6, remove, remove-last, cloudflare, google, quad9, add-dry-run, add@resolvconf, add-unusable@resolvconf, add-ipv6@resolvconf, remove@resolvconf, remove-last@resolvconf, cloudflare@resolvconf, google@resolvconf, quad9@resolvconf, add-dry-run@resolvconf, add@no-file, cloudflare@no-file, google@no-file, quad9@no-file | no such item: v0.3.2 had no DNS screen (gallery-dns.md §7) |

<!-- TestParity: end -->

## 5. What origin/main's path did that the new one does not

From the table, and, where marked, from the code (no golden shows it):

1. **It changed DNS on selection, with no question:** `-g` and the menu's Configure DNS (interaction.md §5, anti-pattern #1 (d)), and Add, Remove and the presets, which saved hardn.yml at once. v0.3.2's menu item 3 asked y/n first, so the menu's question regressed (v032-reference.md §4.5; gallery-dns.md §7). The screen asks y/N before any change (decision 1; no timer, brief 2.4); `-g` is unit (d)'s question (§7).
2. **It forced `lan`**: `domain lan` and `search lan` in a file, `Domains=lan` for resolved (V19). The host's search domains are kept (decision 2).
3. **It overwrote `/etc/systemd/resolved.conf` whole and restarted systemd-resolved** (V19). The new path writes its own drop-in, `zz-hardn.conf`, under two comment lines naming hardn (dns.md §2.7), and reloads where resolved can.
4. **It configured resolvconf** (its `head`, then `resolvconf -u`; side-effects.md row 22). The new path reports resolvconf and changes nothing (P-37 (a), decision 4).
5. **It applied an empty list:** `DNS=` and a restart, or a file with no nameserver (V19). Refused now (`DNS_NO_NAMESERVERS`).
6. **It wrote through a `/etc/resolv.conf` link** with the direct strategy, and never detected NetworkManager (V19; side-effects.md row 23): the row `nm-link`, its write following the link *from the code* (`file_dns_repository.go:184-187`). The new path classifies the link and refuses what it does not manage (`DNS_MANAGED`, P-37 (a)).
7. **It chose the strategy by `which resolvconf`**, a binary on the PATH (V19: NetworkManager not detected; *the probes, recorded in the golden, not compared*). The new path reads what `/etc/resolv.conf` is (dns.md §2.3, brief 2.5).
8. **It wrote every nameserver into a file** (*from the code*, `file_dns_repository.go:179-182`), where glibc and musl read three. The new path refuses a fourth on a file (`DNS_INVALID`; dns.md §2.5: V19's fix, "validate the list"). Not exercised: the fixture lists two.
9. **It wrote in place, backed nothing up and could not roll back** (V18). The new path replaces each file atomically, backs it up first, journals the change, and rolls back a failed save's DNS change too (decision 1 (iii)).
10. **Its Add, Remove and presets changed hardn.yml alone** (decision 1), **to `./hardn.yml`** in the working directory, last in hardn's search order (side-effects.md row 29; interaction.md §10; plan §3.19), **whole**: 48 settings, every default pinned, the file's comments dropped, `dryRun` included (row 29; plan §3.19 as amended 2026-10-22), **and under a dry run** (row 29; screen-dns.md §2.8). The file loaded was left as it was when the working directory was elsewhere, the case the goldens pin. *From the code* (`config.go:470-486`, `os.WriteFile` with a relative name; `FindConfigFile`'s search order ends with `./hardn.yml`): run in the loaded file's own directory (`cd /etc/hardn`, or a `./hardn.yml` that was the file loaded, or `-f hardn.yml`), it overwrote that file, whole, its comments dropped, the file keeping its own mode.
11. **Add took any four dot-separated parts** (999.1.1.1) **and refused IPv6** (decision 9; interaction.md §10).
12. **Remove emptied the list**, which `-g` and Run All then applied (decision 1 (ii); V19).
13. **Add's and Remove's message was wiped at once** by the redraw (W1, W2; *from the code*, `dns_menu.go:162-178`). The screen keeps the result above the screen it draws again (screen-dns.md §2.11).
14. **The menu's Configure DNS, beyond (a)** (*from the code*, `dns_menu.go:132-160`): with no nameserver configured it refused, "No nameservers configured. Please add nameservers first.", as the screen refuses ("Please add nameservers first.", decision 3); under `cfg.DryRun` it printed "[DRY-RUN] Would configure DNS with nameservers: …" and wrote nothing, as the screen's dry run writes nothing: both agree in effect, and the service's own `cfg.DryRun` section, which writes as live, is reached by no path (§4's dry-run rows). On success it printed "DNS configured successfully" and the list, then went back to the main menu after Enter.
15. **`-g` exited 0 after a failure** (V25), logging "Failed to configure DNS: …" or "DNS configured successfully", then "Script completed selected hardening operations." (*from the code*, `cmd/hardn/main.go:395-402`, 418-422).
16. **Its Add, Remove and presets saved hardn.yml whatever the host**: on resolvconf's link (the `@resolvconf` rows), and, *from the code* (nothing on that path reads the host), on NetworkManager's, in a container and with systemd-resolved stopped. The screen refuses every item there before it asks, and saves nothing (P-37 (a), decision 4; screen-dns.md §2.9).
17. **With no hardn.yml loaded** (none found, n answered), **they saved `./hardn.yml` all the same**, the defaults and the list (the `@no-file` rows). The screen refuses Add and the presets before asking, `CONFIG_NOT_LOADED` (control-dns.md decision 11; screen-dns.md §2.9, §8 decision 12); with the defaults' empty list Remove is offered by neither, and Configure DNS refuses on both, "Please add nameservers first." (decision 3).

The new path does what origin/main did not: it reports what may undo the change (decision 5 on the screen, control-dns.md §8 D1 for `-g`'s plan: dhcpcd, udhcpc, Proxmox VE's settings, and on resolved each link's own servers, P-37 (a)); on Alpine the screen can stop udhcpc rewriting the file (decision 6, answered n here).

## 6. UNEXPLAINED differences

**None.** Every difference `TestParity` finds has an entry in `dnsTable` with a source that says it; a difference with no entry fails the test and is marked UNEXPLAINED in the golden and in §4.

## 7. For unit (d)

**What `-g` did on origin/main:** as root (`main.go:143`), after loading hardn.yml (`:151`), with no question and no TTY check, `DNSManager.ConfigureDNS(cfg.Nameservers, "lan")`: §4's (a) rows, the `lan` domain forced, an empty list applied, NetworkManager's link written through; a failure logged, exit status 0 (V25); `hardn -n -g` refused with exit status 2 before anything runs (V1's P0.8 gate, `dryRunFlagApplies`).

**Decided (the maintainer, 2026-10-23: "apply without asking, -n -g shows the plan and writes nothing, and no udhcpc flag (the warning only)"):** what `-g` does on the new control. screen-dns.md §7 says "to the control without the screen (`dnsctl.Controls(cfg)` through the engine, `output.Human`)"; this table compared that, `Plan` then `Apply`.
- **No question:** `-g` plans and applies without asking, as origin/main did; the flag is the request, a DNS change carries no lockout risk, and a failure rolls back. Whether operation flags need `--yes` without a TTY stays plan §3.11's, for every flag at once.
- **A host every plan refuses** (resolvconf, NetworkManager, a container, an empty list): the refusal's report, and a non-zero exit status (V25's fix, decided: Phase 3's exit codes), where origin/main wrote and exited 0.
- **`-n -g` shows the plan and writes nothing**, the new path's dry run, in place of V1's refusal (exit status 2) for this flag.
- **Alpine:** no flag; udhcpc's question (decision 6) is not asked, `StopUDHCPC` is false and the finding is reported, as this table ran it.

**What it may now delete:** `pkg/menu/dns_menu.go`, which has no goldens of its own (`pkg/menu/testdata/golden` holds `main_menu` and `run_all` only); screen-dns.md §7 lists the rest.

**The goldens this test reads stay readable:** `TestParity` reads origin/main's goldens in place, `ops/dns_*` (which stay while the legacy DNS service does, slice 8) and `menu_dns/*` (which describe `dns_menu.go`'s items). Whoever deletes them, or their tests, moves the files unchanged under `pkg/screens/dns/testdata/` and points `legacyGoldens` there, or the record is lost and the test fails.

**Built in `next/s2-replace`** (unit (d), 2026-10-23; [cmd-engine.md](cmd-engine.md)):

- **`-g`, as decided:** `dnsctl.Controls(cfg)` planned and applied through the engine, `output.Human`'s report, no question, after the root check and the configuration's load as before; nothing saved; `StopUDHCPC` false, the finding reported. A refusal is reported with its code and how to go on, nothing written, exit status 1; a failed apply is rolled back, 1; Ctrl-C, SIGTERM or a hangup roll it back and end hardn with 130. `-n -g` is the plan alone (out of `RefuseDryRunOperations`' list): exit status 3 with changes to make, 0 with none, 1 refused (Phase 3's exit codes; docs/reference/output.md §3). Combined with other flags: they run in their order as before, and `-g`'s non-zero status is hardn's once the flags after it have run. The cells of §4 are what `-g` now does on each host, as this table ran it.
- **The record frozen first** (the unit's first commit, before its sources changed): `TestParity` reads origin/main's end state from `pkg/screens/dns/testdata/legacy`, in `pkg/characterization`'s layout (`legacyGoldens`, `fixtures`):
  - `menu_dns/*.golden` (13) and the fixtures they loaded (`fixtures/config/dns-menu.yml`, `-one`, `-empty`), **moved** there unchanged (`git mv`), and `TestMenuDNSGoldens` retired: it mirrored `dns_menu.go`'s handlers, which the unit deleted, so nothing would regenerate them truly.
  - `ops/dns_*/*.golden` (16), **copied** there byte for byte as at e666243, not moved (the orchestrator's decision): `pkg/characterization`'s `TestOperationGoldens` still pins the legacy DNS service, which `hardn -r` and Run All's live run reach until slice 8, and its own goldens had to change where `-g` left it ("Reached by", and the modelled `hardn -n -r` in place of `hardn -n -g`); the copies keep origin/main's record as it was, `-g` and its `-n` refusal included.
  - Nothing writes under `testdata/legacy`: `TestParity`'s `-update` writes `parity.golden` and §4's region only, `pkg/characterization`'s `-update` its own copies. The 32 files `cmp` equal to e666243's; `parity.golden` and §4 are byte for byte as at e666243. The golden's header still says "read in place": it is part of the golden, kept as it was so that the record is provably unchanged.

## 8. For the next slice

Every slice has a unit (b2) (plan §2). The harness is `pkg/screens/dns/parity_harness_test.go`, with nothing of DNS's in it; `parity_test.go` is what DNS supplies.

*Since `next/s3-shared` (slice 3, unit (b); [screen-kit.md](screen-kit.md)): the harness is the package `internal/parity`, moved as it was, with the names below exported (`readLegacy` and `compare` stay its own; `endState` is `EndState`, `newState` `NewState`, `planned` `Planned`, `tie` `Tie`, `runParity` `Run`, `paritySlice` `Slice`, its fields capitalized). A slice now also gives the directories of origin/main's goldens and fixtures (`Slice.Legacy`, `Slice.Fixtures`). The harness links `pkg/ui/uitest`, so the "How" below no longer applies: a slice's tests import `internal/parity` and call `parity.Run` from their `TestParity`. `golden` and `updating` are still copied in `pkg/output`'s and the DNS control's tests.*

- **Reused as it is:** the parser of `pkg/characterization`'s traces (`readLegacy`: exec, mkdir, write with its content, both `cfg.DryRun` sections, a menu item's result, the `-n` refusal, the V-numbers); the end state and its reading from `hostfake`'s calls (`endState`, `newState`, `planned`: the files, directories, commands, backups and journal); the generic aspects (`compare`: refusal, files, mode, directories, commands, write, backup, journal, findings, and hardn.yml's path, setting, other settings by value, rest and mode); the table and its checks (`tie`, `expand`, `checkTable`: unlisted, stale, V-numbers); the driver (`runParity`, `runCell`); the renderings (`renderCells`, `renderPage`) and the golden and page region with `-update`.
- **Supplied by the slice** (`paritySlice`): its scenarios (origin/main's golden, the host state, the fixture, the screen's answers), distributions and table; the golden's header and name, the page and the package; the hardn.yml setting its items save; which host states each stock install has (`shipped`) or cannot have (`exists`); origin/main's commands that only read (`probes`); its run of the new path (`run`: the control's plan and apply, the screen's items, with the new path's commands that only read); and the aspects of the files both paths wrote (`content`: for SSH the effective `sshd -T` values, for UFW the rules).
- **Its legacy goldens, when unit (d) removes their sources:** freeze them first, as DNS's are (§7): under the screen package's `testdata/legacy`, in the characterization's layout, moved where their test goes with the legacy screen, copied byte for byte where it stays because a legacy path still calls the code (Run All's, until slice 8), and `legacyGoldens` pointed there.
- **How:** a `_test.go` file cannot be imported, so the next slice copies the harness into its screen's package, changing its package clause, and that package's tests must link `pkg/ui/uitest` for `make golden-update` to pass `-update`. When a second slice needs it, a shared test-helper package (as `pkg/ui/uitest`) should hold it, with `golden` and `updating`, today copied in `pkg/output/output_test.go` and `pkg/engine/controls/dns/helpers_test.go` as well: non-test Go, which this unit's additions-only scope left out, for the orchestrator to decide.
- **Extending it:** the parsers fail on an operation they do not compare (a removal, a rename, a detached command such as SSH's confirm-or-revert timer); a slice whose goldens hold one adds it to the parser with its aspect, rather than letting it drop. *Since `next/s3-shared`, a golden with no section the parser reads fails too ([screen-kit.md](screen-kit.md) §7).*

## 9. Written, not run (gate 5)

Nothing here ran on Linux or as root. The new path's end states are the fake's, with a model of systemd-resolved; origin/main's are its mocks'. What a host would show is control-dns.md §4's and screen-dns.md §4's: each stock host's `/etc/resolv.conf` or drop-in after `-g` and each item, and whether resolved reloads (Debian 13) or restarts (Ubuntu 24.04, systemd 255; dns.md §3). The mode of origin/main's `./hardn.yml` is what `os.WriteFile` gives under umask 022.

## 10. Files and size

- **Added:** `pkg/characterization/menu_dns_test.go`; `pkg/characterization/testdata/fixtures/config/dns-menu.yml`, `dns-menu-one.yml`, `dns-menu-empty.yml`; `pkg/characterization/testdata/golden/menu_dns/{add,add-unusable,add-ipv6,remove,remove-last,cloudflare,google,quad9,add-dry-run,add-no-file,cloudflare-no-file,google-no-file,quad9-no-file}.golden`; `pkg/screens/dns/parity_harness_test.go`, `parity_test.go`; `pkg/screens/dns/testdata/golden/parity.golden`; this page.
- **Changed:** control-dns.md §11, screen-dns.md §7 and §11, gallery-dns.md §9, and the design-system README's "Running and regenerating". No legacy code, no non-test Go and no existing golden.
- *Since `next/s2-replace`:* `menu_dns_test.go` retired; its goldens and the three fixtures moved to `pkg/screens/dns/testdata/legacy`, and `ops/dns_*` copied there (§7).
- *Since `next/s3-shared`:* `parity_harness_test.go` moved to `internal/parity/parity.go` ([screen-kit.md](screen-kit.md)); `parity.golden` and §4's region unchanged.
- **Size:** about 1,900 lines of test Go (the harness about 1,055, DNS's part about 565, `TestMenuDNSGoldens` about 275), this page about 210 lines; testdata 17 files, about 3,730 lines (the parity golden 2,632). Over the guideline of about 500 changed lines because the table is a test: a parser of origin/main's goldens, an extractor of the new path's end state, the comparison, the table itself (55 entries) and two renderings, plus the new characterization scenario (b) needed; the review fixes added the cells for NetworkManager's link, the items on resolvconf's link and with no hardn.yml, hardn.yml compared by value, the stricter parsers and the harness kept apart for the next slice (§8). No non-test Go; `go.mod` and `go.sum` unchanged.
