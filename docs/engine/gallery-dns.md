# DNS in the gallery, before any wiring (slice 2, unit (a): `-screens`, `play`)

> **Unit:** refactor plan slice 2 (DNS, the pilot of the full flow), unit (a), `next/s2-gallery`: "Gallery mock and full sequence, reviewed first" (plan §2, "Phases 3-5 together", §3.22, approved 2026-10-21; brief Phase 5, "gallery first"). Slice table row 2: "Apply without confirmation; empty list; overwrite; symlink writes; presets don't apply; wiped results", decision "`lan` default". V-numbers: V18, V19; W1, W2 ([side-effects.md](../refactor/phase0/side-effects.md) rows 21-23 and 29, [interaction.md](../refactor/phase0/interaction.md) §1, §5). Proposals shown: [P-27](../refactor/proposals.md#p-27) (b), [P-29](../refactor/proposals.md#p-29) (a), [P-30](../refactor/proposals.md#p-30) (b), [P-37](../refactor/proposals.md#p-37) (a); [P-01](../refactor/proposals.md#p-01) stays out (§9).
> **Status:** built and tested on macOS, in process: the mocks' goldens on the core set, the playable mode's three DNS transcripts, and five tests that hold every plan, finding, refusal, check and failure the mocks show to what `pkg/host/dns`, `steps.DNS`, the engine and `pkg/output`'s codes make of the same hosts on hostfake. No real terminal and no host were used; nothing here runs on Linux, and nothing needs to: the gallery runs no command. **Every word new to origin/main is proposed** (§5), and every behaviour below is a proposal for the maintainer's review: nothing is wired into `cmd/hardn` or the live menu.

## 1. Review description (brief §6, the refactor plan's template)

1. **Purpose and scope.** origin/main's DNS screen ([interaction.md](../refactor/phase0/interaction.md) §4.3, `pkg/menu/dns_menu.go`), on the sub-menu layout as `sub-menu/03-dns` was (DEV-058), revised and drawn on seven hosts, and its six items as workflows through the canonical flow: each change planned, reviewed and confirmed y/N, with no timer (brief 2.4: DNS has no lockout risk), then back to the DNS screen with the result above it. Twelve sequences (§3), for the maintainer's review before unit (b) puts DNS on the engine. In scope: `pkg/ui/screen` (`dns.go`, the DNS mocks; `Mock.Refusals`), the gallery (`play.go`'s links and refusals; `dns_test.go`), one `pkg/ui/component` change (DEV-060), the goldens and the docs.
2. **Behaviour changes:** none to hardn. `cmd/hardn` links neither the gallery nor `pkg/ui/screen`'s mocks; `pkg/host`, `pkg/engine`, `pkg/output`, `pkg/config` and the legacy packages are untouched. The one shared change is `KeyValue`'s (a bullet item with `ValueWarn` draws in `status.warn`, DEV-060), which no screen of hardn draws yet. The behaviours the mocks propose are tied to their defects in §6.
3. **Tests added.** In `internal/tools/uigallery/dns_test.go`, each over the hosts built on hostfake and read by `dns.Read` itself:
   - `TestDNSScreensAreReal`: each DNS screen's rows are what `dns.Read` establishes (the manager, the nameservers and search domains in use, Unknown where `pkg/host/dns` reads none), hardn.yml's list, and item 3 exactly when there is one to remove.
   - `TestDNSRefusalsAreReal`: the NetworkManager host refuses every item where `dns.PlanDNS` refuses with `ErrManaged` (`DNS_MANAGED`), the empty list Configure DNS where it refuses with `ErrNoNameservers` (`DNS_NO_NAMESERVERS`), and no other host refuses anything.
   - `TestDNSPlansAreReal`: for all fourteen DNS sequences, the review's nameservers (Validate's canonical list) and search domains, the change through `steps.DNS` (title, target, commands, the diff with a created file against `/dev/null`, stage files, risk plain), no change where `PlanDNS` plans none, the save of hardn.yml exactly when the list changes, and a warning line per finding of `dns.Plan`, each a code `pkg/output`'s registry holds (`DNS_PER_LINK` and its kin).
   - `TestDNSCheckIsValidate`: Add nameserver's check against `dns.Validate` on twenty entries: every kind it refuses, and some it takes, word for word.
   - `TestDNSFailureIsTheEngines`: the real engine on the Ubuntu host, whose systemd-resolved reloads with no effect, through `output.FlowEngine`: the mock's failure reports the same steps and the same `flow.Failure`, and `dns.Apply` leaves no drop-in.

   Also `TestPlayGolden`'s three DNS transcripts, `TestPlayReaches` (refusals; the other hosts' DNS screens as starts), and `TestBlockStyles`' warning bullet item. `TestMocks`, `TestOutputRules` and the gallery's `TestScreenSmoke` take the new mocks as they come.
4. **Golden diffs.** Changed: `pkg/ui/screen/testdata/golden/sub-menu/03-dns.golden`, the DNS screen revised (a "Search Domains" row; hardn.yml's list `192.0.2.54`, `2001:db8::54`, so that 192.0.2.53 can be added). Added: `sub-menu/03-dns-{alpine324,configured,empty,networkmanager,pve9,ubuntu2604}` (the other hosts), the 32 step files of the ten DNS workflows (`workflow-step/dns-*/<sequence>/<n>-<step>`, §3), `internal/tools/uigallery/testdata/golden/play/{dns,dns-networkmanager,dns-empty}` (from the DNS screen and back), and `pkg/ui/component/testdata/golden/keyvalue/item-warn` (DEV-060, the full matrix). No other golden changed.
5. **UI.** freeze screenshots are deferred, as for every unit (design-system README §9.16). New deviation: DEV-060. Proposals: those above; this page lists each decision with its alternatives (§4).
6. **Inferred screens.** Every DNS mock is labelled "inferred from origin/main" (`sub-menu/03-dns` adds "the Figma export holds a screenshot of its legacy screen, images/03-dns-legacy-tui.png"); each carries the origin/main-vs-v0.3.2 delta in its source note (§7).
7. **New dependencies:** none (`go.mod` and `go.sum` unchanged).
8. **Per-distro notes:** written, not run (gate 5). The mocks run on fixture plans; the tests read hostfake hosts; nothing ran on Debian, Ubuntu, Alpine or Proxmox VE. What `pkg/host/dns` itself has not run is [dns.md](../safety/dns.md) §4's.
9. **`git status --short --ignored`** before the commit: the paths of this unit only, none ignored (§10).

## 2. The DNS screen

origin/main's screen on the sub-menu layout: the title bar "DNS Configuration"; the section "Current DNS Configuration" with three rows, "DNS Implementation" (what `dns.Read` found manages `/etc/resolv.conf`), "Nameservers" (in use) and, new, "Search Domains" (in use, shown because hardn keeps them: decision 2); the section "Configured Nameservers", hardn.yml's list, or with none P-30 (b)'s bullet item, origin/main's "No nameservers configured" (decision 3); origin/main's items and descriptions, item 3 only when the list is not empty, and with none neither shown nor taken (the hint says `[0-2, 4-6 or q]`, where origin/main hid it and still took it); "0) Return"; origin/main's prompt, "Enter selection" (P-29 (a)). Plain, at 80 columns, the main host:

```text
# DNS Configuration ############################################

# Current DNS Configuration ------------------------------------

   -  DNS Implementation   direct (/etc/resolv.conf)
   -  Nameservers          192.0.2.1
   -  Search Domains       example.net

# Configured Nameservers ---------------------------------------

   -  Nameserver 1         192.0.2.54
   -  Nameserver 2         2001:db8::54
```

| Host | Screen | What `dns.Read` finds | The rows it shows | Items that lead somewhere |
|---|---|---|---|---|
| Debian 13, the main menu's 3 | `sub-menu/03-dns` | a file dhcpcd wrote; dhcpcd installed | direct (/etc/resolv.conf); 192.0.2.1; example.net | all six (§3) |
| Ubuntu 26.04 | `sub-menu/03-dns-ubuntu2604` | systemd-resolved's stub link; no global servers, eth0's 192.0.2.1 | systemd-resolved; 192.0.2.1 (eth0); None | 1 |
| Alpine 3.24 | `sub-menu/03-dns-alpine324` | udhcpc's file; stock udhcpc.conf (udhcpc rewrites the file) | direct (/etc/resolv.conf); 192.0.2.1; example.net | 1 |
| Proxmox VE 9 | `sub-menu/03-dns-pve9` | the installer's file | Proxmox VE (/etc/resolv.conf); 192.0.2.1; example.net | 1 |
| Debian 13 with NetworkManager | `sub-menu/03-dns-networkmanager` | the link to `/run/NetworkManager/resolv.conf`; nothing else read | ⭘ NetworkManager, reported only; ? Unknown; ? Unknown | all six refuse (decision 4) |
| Debian 13, no list | `sub-menu/03-dns-empty` | as the main host; hardn.yml lists none | as the main host; "- No nameservers configured" | 1 refuses; 3 not shown |
| Debian 13, configured | `sub-menu/03-dns-configured` | the file lists 192.0.2.54, 2001:db8::54 already | direct (/etc/resolv.conf); 192.0.2.54, 2001:db8::54; example.net | 1 |

To see them, from the repository root with `GOTOOLCHAIN=local GOWORK=off` (add `-profile plain`, `-width 60` or `-theme light` as wanted):

```text
go run ./internal/tools/uigallery -screens sub-menu/03-dns sub-menu/03-dns-ubuntu2604 sub-menu/03-dns-alpine324 sub-menu/03-dns-pve9 sub-menu/03-dns-networkmanager sub-menu/03-dns-empty sub-menu/03-dns-configured
go run ./internal/tools/uigallery play                                       # then 3: the main host's DNS screen
go run ./internal/tools/uigallery play sub-menu/03-dns-<host>                # any other host's
```

## 3. The sequences

Each from the DNS screen through to the result and back (in play), and step by step (`-screens`, the goldens), on the core set. A workflow's title bar is its item's label; the flow under it is the canonical one: the form, if any; the review, the section "DNS Configuration" with the nameservers the host will use, the search domains it keeps, then each finding of the plan as a warning line, and, when hardn.yml's list changes, that saving it loses its comments; the plan, the DNS change ("Set the nameservers", its file and diff, the reload where there is one), then the save of `/etc/hardn/hardn.yml`, after it; the question, y/N; the steps; the result. Then `nav.Run` draws the DNS screen again under it.

| # | Sequence | Mock / sequence (steps) | Listing | Play (keys typed) |
|---|---|---|---|---|
| 1 | Ubuntu 26.04, systemd-resolved: Configure DNS; the drop-in created, the reload; the per-link finding | `dns-configure-dns-ubuntu2604/normal` (`1-confirm`, `2-applied`) | `-screens workflow-step/dns-configure-dns-ubuntu2604/normal` | `play sub-menu/03-dns-ubuntu2604`: 1, y |
| 2 | Debian 13, a file: Use Cloudflare DNS; resolv.conf's diff, the DHCP client finding, the save listed | `dns-use-cloudflare-dns/normal` (`1-confirm`, `2-applied`) | `-screens workflow-step/dns-use-cloudflare-dns/normal` | `play`: 3, 4, y |
| 3 | Add nameserver: 999.1.1.1 refused, 192.0.2.54 refused as a duplicate, 192.0.2.53; plan; y | `dns-add-nameserver/normal` (`1-nameserver` to `3-nameserver`, `4-confirm`, `5-applied`) | `-screens workflow-step/dns-add-nameserver` | `play`: 3, 2, 999.1.1.1, 192.0.2.54, 192.0.2.53, y (golden `play/dns`, then 0, 0) |
| 4 | Remove nameserver: a choice of the two; plan; y | `dns-remove-nameserver/normal` (`1-nameserver`, `2-confirm`, `3-applied`) | `-screens workflow-step/dns-remove-nameserver` | `play`: 3, 3, 2, y |
| 5 | Alpine 3.24: udhcpc's finding; the RESOLV_CONF question answered y, and answered n | `dns-configure-dns-alpine324/normal` and `/kept` (`1-udhcpc`, `2-confirm`, `3-applied`) | `-screens workflow-step/dns-configure-dns-alpine324` | `play sub-menu/03-dns-alpine324`: 1, y, y (or 1, n, y) |
| 6 | Proxmox VE 9: its file and the node finding | `dns-configure-dns-pve9/normal` (`1-confirm`, `2-applied`) | `-screens workflow-step/dns-configure-dns-pve9` | `play sub-menu/03-dns-pve9`: 1, y |
| 7 | Dry run: the plan with the dry-run footer; nothing saved | `dns-use-cloudflare-dns/dry-run` (`1-planned`) | `-screens workflow-step/dns-use-cloudflare-dns/dry-run` | `play -sequence dry-run`: 3, 4 |
| 8 | Cancel: n at the y/N | `dns-configure-dns/cancelled` (`1-confirm`, `2-cancelled`) | `-screens workflow-step/dns-configure-dns/cancelled` | `play`: 3, 1, n |
| 9 | Nothing to change: "Nothing needs changing." (P-27 (b)) | `dns-configure-dns-configured/unchanged` (`1-unchanged`) | `-screens workflow-step/dns-configure-dns-configured` | `play sub-menu/03-dns-configured`: 1 |
| 10 | Empty list: P-30 (b)'s item; Configure DNS refused; 3 not an option | `sub-menu/03-dns-empty` | `-screens sub-menu/03-dns-empty` | `play sub-menu/03-dns-empty`: 3, 1 (golden `play/dns-empty`) |
| 11 | NetworkManager, reported only: the refusal | `sub-menu/03-dns-networkmanager` | `-screens sub-menu/03-dns-networkmanager` | `play sub-menu/03-dns-networkmanager`: 1, 4 (golden `play/dns-networkmanager`) |
| 12 | Failure: the reload has no effect (`DNS_NOT_EFFECTIVE`), the change undone; the report as the engine gives it | `dns-configure-dns-ubuntu2604/failure` (`1-confirm`, `2-failed`) | `-screens workflow-step/dns-configure-dns-ubuntu2604/failure` | `play -sequence failure sub-menu/03-dns-ubuntu2604`: 1, y |

No sequence was merged with another. Four were added: Alpine's `kept`, which shows udhcpc's finding as a warning line (`dns.PlanDNS` drops it once the question is answered y), and the `normal` sequences of Configure DNS, Use Google DNS and Use Quad9 DNS on the main host, so that every item of its screen plays. `-fail N` fails step N of any DNS plan, as of every mock's.

The failure, as the engine reports it (sequence 12, step 2, plain):

```text
  [!] Failed: Set the nameservers (DNS_NOT_EFFECTIVE)

# Operation Failed
# Set the nameservers: dns: reload-resolved: global nameservers
# [], want ["192.0.2.54" "2001:db8::54"]: the step ran, but the
# host does not show its effect (DNS_NOT_EFFECTIVE)

  [i] Info: Check the log file at /var/log/hardn.log for details
```

`dns.Apply` did undo the change (the drop-in is gone, resolved is back on its servers), but the report names nothing rolled back: §8, item 3.

## 4. Decisions for the maintainer

Each is proposed; the mock shows the recommendation.

1. **Edits go through the plan.** Add nameserver, Remove nameserver and the presets plan the list they make, the host's change and the save of hardn.yml, which runs only after the change applies; n changes and saves nothing. *Recommended:* this, which fixes "presets don't apply" and gives each change a review. *Alternatives:* (a) origin/main's: Add, Remove and the presets change hardn.yml alone (now at its loaded path, §3.19), and only Configure DNS applies, through the plan; (b) as recommended, but saving hardn.yml before the apply, so that a failed apply still records the list; (c) a question after a successful apply, "Save to hardn.yml?", so that a host can be changed without changing the configuration.
2. **The search domain** (the plan's "`lan` default"). *Recommended:* hardn keeps the host's search domains (`pkg/host/dns`'s default, `ReplaceSearch` false), the screen shows them, and no configuration key is added: origin/main forced `domain lan`/`search lan` on every apply (V19). *Alternatives:* (a) a "Search domain" field, default "lan", for parity with origin/main: a new configuration key (a surface change, plan §3.11's kind), and on Proxmox VE one domain only (`dns.Validate`); (b) show nothing about search domains, as origin/main did. Not mocked.
3. **P-30, the empty list.** *Recommended:* (b), mocked: a bullet item of the section, origin/main's words, "- No nameservers configured"; no layout change. The built bullet item draws its dash in `text.muted` and its words in `text.primary`, as Figma's checklist does; words in `text.muted` too would need a `KeyValue` state of their own (not built). *Alternatives:* P-30 (a), a notice block (a warning `Message`) in the sub-menu layout; (c) a status row. Configure DNS then refuses with origin/main's "Please add nameservers first.", its first sentence, which the item already says, left out.
4. **P-37, hosts hardn only reports** (resolvconf, NetworkManager, another link, a container). *Recommended:* (a), mocked for NetworkManager: the manager named, "reported only", the nameservers and search domains Unknown (`pkg/host/dns` reads nothing more there), and every item refused before it asks anything, with a warning naming the manager and the tool to use by hand. *Alternatives:* P-37 (b), writers for resolvconf and NetworkManager; (c) on systemd-resolved, a step that also stops per-link servers. The other managers' rows would read "resolvconf, reported only" (origin/main's word), "another program, reported only" (a link hardn does not know) and, in a container, the refusal of `dns.HostOf` (`DNS_CONTAINER`); none is mocked.
5. **The findings before the confirmation.** *Recommended:* in the review, as the maintainer's warning lines of 2026-10-21 (`- ` in `status.warn`, DEV-060), each in a few words: the review is built before the plan, from `dns.State.Findings()`, which is pure, so no flow change is needed. *Alternatives:* (a) under the change they are about, in the plan (a `PlanView` change: lines under a change, as under a status row), closest to the decided "lines under their rows"; (b) between the plan and the question (a `pkg/ui/flow` change); (c) as now: after the result, as `output.AfterFlow`'s warning messages, with dns's whole detail and code; which today shows nothing for DNS (§8, item 1). Words of each line: §5.
6. **Alpine's RESOLV_CONF question.** *Recommended:* asked in the form of every change on a host where udhcpc rewrites the file, "Stop udhcpc rewriting /etc/resolv.conf?", y/N, default N, as `dns.Config.UDHCPCNoResolv` is an explicit step: y adds `RESOLV_CONF="no"` to `/etc/udhcpc/udhcpc.conf` in the same change, and the finding goes; n keeps it, and the review warns. *Alternatives:* (a) never ask: the warning alone, and the next lease undoes the change; (b) a configuration key (a surface change); (c) ask only on Configure DNS.
7. **The presets** stay origin/main's, IPv4 only (1.1.1.1, 1.0.0.1; 8.8.8.8, 8.8.4.4; 9.9.9.9, 149.112.112.112), replacing the configured list. *Alternative:* each provider's IPv6 servers too; a file takes three nameservers at most (`dns.Validate`, glibc and musl), so two and two would be refused on Debian, Alpine and Proxmox VE: one of each, or IPv6 on systemd-resolved alone.
8. **P-01, a DNS row on the main menu.** Not mocked: the main menu's rows are the status model's. *Recommended* for its unit: a row from `dns.State` (the file's nameservers, or resolved's global ones), Configured when they are hardn.yml's, with the reported-only managers Not Applicable.

Also shown, and still proposed: P-27 (b)'s "Nothing needs changing."; P-29 (a)'s prompt, origin/main's "Enter selection"; the built patterns of P-23, P-24 and P-42 (the steps' and messages' words and glyphs). No item is marked (P-22): a refused item refuses when chosen.

## 5. Words

origin/main's where it has them; **every other word is proposed**.

| Where | Words | Source |
|---|---|---|
| Title bar, sections | "DNS Configuration", "Current DNS Configuration", "Configured Nameservers" | origin/main (headings without their colons, DEV-058) |
| Rows | "DNS Implementation", "Nameservers", "Nameserver 1"...; "systemd-resolved", "direct (/etc/resolv.conf)" | origin/main |
| Rows | "Search Domains", "None"; "Proxmox VE (/etc/resolv.conf)"; "NetworkManager, reported only"; "192.0.2.1 (eth0)" (a link's server and its link) | **proposed** |
| Empty list | "No nameservers configured" | origin/main (P-30 (b)) |
| Items, descriptions, "Return", "Enter selection" | as origin/main | origin/main (P-29 (a)) |
| Title bars of the workflows | the item's label: "Configure DNS", "Add nameserver", ... | origin/main's labels, **proposed** as titles |
| Add's field | "Enter nameserver IP address" | origin/main |
| Remove's field | "Select a nameserver to remove" | **proposed** (after origin/main's "Select a key to remove") |
| Alpine's question | "Stop udhcpc rewriting /etc/resolv.conf?" | **proposed** |
| Review | "DNS Configuration" with "Nameservers", "Search Domains" | origin/main's words, **proposed** here |
| Warning lines | "dhcpcd may rewrite /etc/resolv.conf on a lease" (`dns-dhcp-client`), "eth0 still uses its own nameserver 192.0.2.1" (`dns-per-link`), "udhcpc rewrites /etc/resolv.conf on each lease" (`dns-udhcpc`), "Proxmox VE's DNS settings rewrite /etc/resolv.conf" (`dns-proxmox-node`), "Saving hardn.yml loses its comments" (§3.19) | **proposed**; `pkg/host/dns`'s detail of each finding is a sentence of 100 to 160 characters, `pkg/output`'s registry line a shorter one ("a DHCP client that may rewrite /etc/resolv.conf is installed") |
| The plan's changes | "Set the nameservers", "Save the configuration" | **proposed** (the DNS control's summary, unit (b)) |
| Questions | "Configure DNS?", "Add nameserver 192.0.2.53?", "Remove nameserver 2001:db8::54?", "Use Cloudflare DNS?" | origin/main's labels, **proposed** as questions, as Figma's "Create user 'george'?" |
| Add's refusals | `dns: nameserver "999.1.1.1": not an IPv4 or IPv6 address: invalid DNS configuration`, `dns: nameserver 192.0.2.54 given twice: invalid DNS configuration` | `dns.Validate`'s, word for word; a screen's own words would be **proposed** (§8) |
| Refusals | "Please add nameservers first."; "NetworkManager manages DNS here. Set the nameservers with nmcli." | origin/main's; **proposed** |
| The failure | "Set the nameservers: " + dns.Apply's error + " (DNS_NOT_EFFECTIVE)" | `pkg/output`'s ErrorReport, `pkg/host/dns`'s error, `pkg/output`'s code |
| Flow | "Nothing needs changing.", "No actual changes were made to your system.", "Operation Successful", "Operation Cancelled", "Operation Failed", "Done", "Failed" | P-27 (b); origin/main; Figma; P-23, P-24, P-42 |

## 6. Defects fixed, as the mocks show them

| Defect | Reference | In the mocks |
|---|---|---|
| Configure DNS applies on selection, with no confirmation | [interaction.md](../refactor/phase0/interaction.md) §5, anti-pattern #1 (d) | the plan, the review and a y/N question before anything changes (§3) |
| The presets set hardn.yml only; nothing applies | interaction.md §4.3 ("config only"); the plan's slice row 2 | each preset plans, applies, then saves (sequence 2) |
| Add's and Remove's results wiped by the redraw | W1, W2 (interaction.md §5, anti-pattern #2) | the result stays above the DNS screen (`play/dns`) |
| An invalid entry restarts the screen; only "4 dot-separated parts" checked, so 999.1.1.1 passes and IPv6 is refused | interaction.md §5, anti-pattern #4, and §10 | the field asked again under `dns.Validate`'s refusal; 2001:db8::54 configured (sequence 3) |
| Item 3 hidden yet selectable; the hint hides the gap ("0-6") | interaction.md §5, anti-patterns #5 and #6, §1 item 4 | `[0-2, 4-6 or q]`, and 3 refused (`play/dns-empty`) |
| An empty list accepted (`DNS=`, a file with no nameserver) | V19 | Configure DNS refused (`DNS_NO_NAMESERVERS`); Remove keeps one |
| `resolved.conf` overwritten, systemd-resolved restarted | V19; side-effects row 21 | the drop-in `zz-hardn.conf`, a new file, and `try-reload-or-restart` (sequence 1) |
| `lan` forced | V19 | the host's search domains kept and shown (decision 2) |
| The direct path writes through the `/etc/resolv.conf` symlink; NetworkManager not detected | V19; side-effects rows 22, 23 | the link classified, never written through; NetworkManager reported (sequence 11) |
| hardn.yml saved to `./hardn.yml`, the whole file, with no warning | side-effects row 29 | the loaded path, listed in the plan, after the apply, its lost comments said (§3.19) |
| No backup, no rollback, non-atomic | V18 | `dns.Apply`'s, not visible in a mock but in the failure's undo (sequence 12) |
| "Press any key to continue" after each action | interaction.md §5, anti-pattern #3 | none: the menu again under the result (P-09) |

## 7. Deltas from origin/main and v0.3.2 (template item 6)

- **v0.3.2** has no DNS screen: menu item 3 warns and asks "Do you want to continue? (y/n)", then applies the configured nameservers (`pkg/menu/menu.go:519-541`), by one of three strategies, `domain lan`/`search lan` forced, the first two nameservers on a file, with backups, and panics on an empty list ([v032-reference.md](../refactor/phase0/v032-reference.md) §4.5, [side-effects.md](../refactor/phase0/side-effects.md) §6).
- **origin/main** adds the screen: the nameservers in use and those configured, six items; Configure DNS applies at once, with no backup and with an empty list; Add and Remove change `./hardn.yml` only and redraw over their message; the presets change `./hardn.yml` only. The regression from v0.3.2: the confirmation and the backups gone.
- **The mocks** keep origin/main's screen and words, add the search domains, and put every change through the plan, the review and a y/N question, on the manager `pkg/host/dns` finds, which restores v0.3.2's confirmation and backups.

## 8. What the mocks take from real code, and what not

**Real** (held by the five tests of §1, item 3): what each screen shows of its host (`dns.Read`); each plan's change, its target, diff, commands, stage and risk (`dns.PlanDNS`, `steps.DNS`), the diff of a created file taken as unit (b) will take it; which findings each plan has, and their codes in `pkg/output`'s registry; each refusal's error and code (`dns.PlanDNS`, `output.CodeOf`); Add's refusals, word for word (`dns.Validate`); the failure, its steps and its report (the engine on hostfake, through `output.FlowEngine`).

**Not from real code** (the mocks' own, each a proposal or a placeholder until unit (b) or (c)):
- The words of §5 marked proposed: the warning lines say each finding in a few words where `pkg/host/dns`'s detail is a long sentence; the change's title is the summary the DNS control will give `steps.DNS`; the refusals' words.
- **The save of hardn.yml** is a change of the mock's plan: no control saves the configuration yet (`pkg/config.Save` exists, plan §3.19); its diff is not shown, since the saved file is the whole configuration, canonical, its comments gone.
- **Add's check** is a copy of `dns.Validate`'s nameserver rules, for one more entry on a file (`addCheck`), because `pkg/ui` cannot import `pkg/host`; `TestDNSCheckIsValidate` holds it to `dns.Validate` on twenty entries. The real screen calls `dns.Validate`. So is the plan of an entry play's user types other than the sequence's: built from the host's file as `dns` writes it, held by the test for the sequence's.
- **The screens do not change** after a change: the gallery has no DNS model of a host (§9).
- **The review is built** from the fixture; on the real screen it would come from `dns.State.Findings()` before the plan, as decision 5 notes.

**Recorded by probe** (`go test -overlay` in `pkg/host/dns`, the tests' resolved model; inputs to unit (b)):
1. **`steps.DNS` does not copy `dns.Plan.Findings` onto `engine.Change.Findings`.** On Ubuntu 26.04 with a DHCP link, `PlanDNS` gives one finding (`dns-per-link`), the change none, and `output.AfterFlow` then writes nothing: the real flow shows no DNS finding anywhere today.
2. **A created file's diff is taken against "", not `/dev/null`.** For the drop-in, `steps.DNS`'s diff starts `--- /etc/systemd/resolved.conf.d/zz-hardn.conf` where `engine.FileDiff(path, nil, ...)` gives `--- /dev/null`, so `--output json` and any reader of the diff see an empty file edited, not a file created. `PlanView` draws both alike (it leaves out the pair when one names the target), so the screen does not show it; `TestDNSPlansAreReal` allows only that difference.
3. **The failure names nothing rolled back.** When the reload has no effect, `dns.Apply` undoes its own steps (the drop-in removed, the old servers checked), `steps.DNS` then drops its journal step (`unitRan`), and the engine's rollback has nothing to undo, so it reports no change rolled back: the user reads "Operation Failed" and never that the change was undone. Proposed for unit (b): report the change rolled back when the unit's own undo finished (`dns.Result.Restored`), as the engine does for a journalled step. The report's words are `pkg/host/dns`'s error, with its Go quoting (`[]`, `["192.0.2.54" ...]`), for the same unit to word.

## 9. Not in this unit

Later units, as the plan orders them:
- **The gallery's DNS host model and play-engine DNS scenarios** with real reads (`dns.Read` on the gallery's simulated hosts, the screen refreshed after a change).
- **The DNS engine control** (unit (b)): `Check` from `dns.Read` and `State.Findings()`, `Plan` from `PlanDNS` through `steps.DNS`, the three inputs of §8, the save of hardn.yml as a change, and the refusals as `steps.Refusal`s.
- **The parity table** (unit (b2)): origin/main's end state against the new path's, per distribution.
- **The screen on the design system** (unit (c)) and the legacy path gated (unit (d)); `pkg/menu/dns_menu.go` stays until then.
- **The main menu's DNS status row** (P-01, decision 8) and **the compact main menu's height** with one row more (plan §3.15, DEV-052).
- **The findings before the confirmation in the real flow** (`pkg/output` or `pkg/ui/flow`): the mocks show the proposed place (decision 5).

## 10. Size

About 560 lines of non-test Go added and 60 removed: `pkg/ui/screen/dns.go` 496 (98 of them whole-line comments: the hosts' data, the workflows' table and the realness note), the rest in `mock.go`, `frames.go` (the old DNS fixture removed), `doc.go`, `play.go` and `keyvalue.go`; about 560 lines of tests (`dns_test.go` 524), 190 of this page and about 20 changed in the design-system README and the deviations log. That is over the guideline of about 500 changed lines, for two reasons: the mocks are seven hosts and ten workflows, mostly data, and the realness checks run the primitive, the engine and the registry over the same seven hosts, which no package could share with them; the split would have been the screens first, then the workflows. Testdata: 43 golden files (one changed). `git status --short --ignored` before the commit listed only this unit's paths, modified or untracked, and nothing ignored.
