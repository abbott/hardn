# DNS on the engine: the DNS control (`pkg/engine/controls/dns`)

> **Unit:** refactor plan slice 2 (DNS, the pilot of the full flow), unit (b), `next/s2-control`: "Control on the engine, with its own new goldens" (plan §2, "Phases 3-5 together", §3.22, approved 2026-10-21). It builds on unit (a) ([gallery-dns.md](gallery-dns.md), whose nine decisions the maintainer adopted on 2026-10-22) and on P-42's row grammar (`next/s2-row-grammar`). Decisions implemented: §3.19 (approved 2026-10-17: save to the loaded path, one atomic save function, warn that comments are lost); slice 2's decisions 1 (edits go through the plan; 1 (i) to (iii)), 2 (the host's search domains kept), 5 (the findings in a few words), 6 (Alpine's question, no configuration key) and 8 (the DNS row's design, for P-01's unit); [P-37](../refactor/proposals.md#p-37) (a) (hosts hardn only reports); P-27 (b) ("Nothing needs changing."). V-numbers: V18 (no backup, no rollback, non-atomic) and V19 (`resolved.conf` overwritten, an empty list applied, `lan` forced, NetworkManager not detected, the symlink written through), now on the engine's path; side-effects row 29 (`./hardn.yml` saved whole, without warning). It fixes the four defects unit (a) recorded (gallery-dns.md §8, items 1 to 4; §8 here).
> **Status:** built and tested on macOS against the recording fake (`hostfake`) only, with a model of systemd-resolved written from [dns.md](../safety/dns.md) §3, not captured from a host. Nothing has run on Linux: every host claim is "written, not run (gate 5)" (§4). Nothing is wired into `cmd/hardn` or the live main menu (unit (c), and P-01's unit), and the legacy DNS path (`pkg/menu/dns_menu.go`, its characterization goldens) is untouched. **Every word below is proposed** (§5 to §7), the adopted mock's where it has them.

The DNS control is the first control that both checks and plans. `Check` gives the status model its DNS row: whether the host uses `hardn.yml`'s nameservers. `Plan` gives the engine the change that makes it so, with the host's search domains kept, and, when the list it applies is not the one `hardn.yml` holds, the save of `hardn.yml` after it. Every refusal comes before any change, with a report. A failure is undone, with no confirm-or-revert timer (brief 2.4), and reported as undone.

## 1. Why

origin/main's DNS screen applies on selection, with no confirmation and no backup; its Add, Remove and presets change `./hardn.yml` alone, whole, without a word on its comments; an empty list is applied; `lan` is forced; `resolved.conf` is overwritten and the symlink written through (gallery-dns.md §6 and §7, V18, V19). Unit (a) drew the replacement and the maintainer adopted it. Phase 2 built the primitive (`pkg/host/dns`, [dns.md](../safety/dns.md)) and Phase 3 its change kind (`steps.DNS`), which unit (a) found to drop the plan's findings, show a created file as edited, and report a failure that `dns.Apply` had undone as never undone (§8). This unit puts DNS on the engine, fixes those, and gives every later slice the save of `hardn.yml` as a change kind of its own.

## 2. Guarantees

### The row (`Check`)

1. **One control, `dns.nameservers`, titled "DNS"** (`Nameservers`; `Controls(cfg)`). **It carries `hardn.yml` as loaded** (`Nameservers.Config`), since the engine's `Check` takes no configuration (engine.md §2.1): `Check` compares the host with that list. No engine API changes for it (§10, decision 1).
2. **`Applies` is true on every host.** A container and the managers hardn only reports are Not Applicable through `Check` (decision 8) and refused by `Plan` with a report (P-37 (a)), because a control that does not apply plans nothing, and an empty plan reads "Nothing needs changing." on a host whose nameservers are not `hardn.yml`'s (§10, decision 3). **A departure from [checks.md](checks.md) §1.6** ("NotApplicable only through `Applies`"), as [check-apparmor.md](check-apparmor.md) §2.2 departs from it: which manager owns `/etc/resolv.conf` is a reading, not one of detect's facts.
3. **What is read:** `dns.Read` (reads only, dns.md §2.2), its manager and what it holds: for a file (`file`, `proxmox`), its `nameserver` lines in order; for systemd-resolved, its global servers (`resolvectl dns`'s `Global:`), and each link's servers and search domains, the latter since this unit (D4, §8).
4. **The line**, decided in this order (§5 lists every one):
   - a container (`dns.ErrContainer`): Not Applicable, "container";
   - a manager hardn only reports (`resolvconf`, `networkmanager`, `foreign`): Not Applicable, naming it;
   - anything `dns.Read` could not establish (`ErrUnknown`, `ErrUnsupported`, the manager, or any fact in `State.Err`): Unknown, "can't read", with the error, whose code is the read's own (`DNS_UNKNOWN`, `COMMAND_FAILED`, `PERMISSION_DENIED`, ...); never a pass;
   - `hardn.yml` lists none: Not Configured, "none in hardn.yml";
   - `hardn.yml`'s list is one `dns.Validate` refuses for the manager (an entry that is no address, a duplicate, four for a file, resolved's own stub): Not Configured, "not valid in hardn.yml";
   - `/etc/resolv.conf` is resolved's link and resolved does not run: Not Configured, "resolved not running";
   - **the nameservers in use are `hardn.yml`'s, in order, as `dns.Validate` writes them** (`2001:DB8:0::54` is `2001:db8::54`): Configured, the list ("192.0.2.54, 2001:db8::54");
   - otherwise Not Configured, "not hardn.yml's".
5. **In order** (§10, decision 4): resolvers query the servers in the order listed (resolv.conf(5) **[unverified]**, not re-read for this unit), and `dns.PlanDNS` changes the host when only the order differs, so Configured is "the plan's DNS change is empty", save for a file whose nameserver lines are scattered (the plan gathers them; the row reads Configured) and a resolved whose global servers come from a file other than hardn's drop-in (the plan writes the drop-in; the row reads Configured). Per-link servers do not change the line: they are a finding (decision 8: "the file's nameservers, or resolved's global ones").
6. **Findings explain the line; none sets it**: the host's (`dns.State.Findings`), coded and worded by `steps.DNSFinding` (§7): `DNS_PER_LINK`, `DNS_DHCP_CLIENT`, `DNS_UDHCPC`, `DNS_PROXMOX_NODE`, each in `pkg/output`'s registry already.
7. **Evidence**, one line per source: `/etc/resolv.conf -> ../run/systemd/resolve/stub-resolv.conf` (a link), `/etc/resolv.conf: absent`, or a reported manager's first line, quoted; for a file `/etc/resolv.conf: nameservers 192.0.2.1` and `/etc/resolv.conf: search example.net`; for resolved `systemctl is-active systemd-resolved.service: active` (or `not active`), `resolvectl dns: Global: ...`, each link with servers (`resolvectl dns: link 2 (eth0): 192.0.2.1`), `resolvectl domain: Global: ...` and each link with domains (`resolvectl domain: link 2 (eth0): example.net`); and `hardn.yml`'s list (`/etc/hardn/hardn.yml: nameservers 192.0.2.54, 2001:db8::54`).
   Tested: `TestCheck` (every line on every host, an error exactly for Unknown, the findings' codes registered, nothing written), `TestEvidence`, `TestCheckGolden`.

### The plan (`Plan`)

8. **The change that brings the host to `in.Config`'s list**: `dns.Read`, then `dns.PlanDNS` with `Nameservers` `in.Config`'s, `ReplaceSearch` false (the host's search domains kept: decision 2, no `lan`, no configuration key) and `UDHCPCNoResolv` the control's `StopUDHCPC` (decision 6: the answer to "Stop udhcpc rewriting /etc/resolv.conf?", asked on Alpine only, with no configuration key; y adds `RESOLV_CONF="no"` to `udhcpc.conf` in the same change and the `dns-udhcpc` finding goes; refused off Alpine), then `steps.DNS` (`dns.nameservers/set`; `StageFiles`, plain: brief 2.4). None when the host uses the list already: a configured host plans nothing, and a plan applied then planned again is empty (idempotency: `TestApply`, every platform). Titled "Set the nameservers", or "Set nameservers; stop udhcpc rewriting" with udhcpc stopped; summarised "Set the nameservers to 192.0.2.54, 2001:db8::54" (and ", and stop udhcpc rewriting /etc/resolv.conf"). `Plan` reads `in.Config`, the engine's; the caller gives `Engine.Plan` the same `hardn.yml`, or a copy with the list edited (Add nameserver, a preset). The engine plans again under the run lock with the same control values and configuration, so the answer and the list shown are the ones applied.
9. **The save of `hardn.yml`** (decision 1, §3.19; the change kind is `steps.PlanSave`, [steps.md](steps.md) §2.24): when `in.Config`'s list is not the one the file at `in.Config.Path()` holds, read now and compared as written, the plan adds `dns.nameservers/save` after the change (`After`; the save alone when the host uses the list already), which `config.Save`s `in.Config` to that path, atomically (`host.WriteFileAtomicKeep`), never to `./hardn.yml`. The engine backs the file up and journals it before the save, so the rollback restores the previous bytes; **a save that fails after the change applied rolls the change back too** (decision 1 (iii): the engine rolls back every change started), so the host and `hardn.yml` agree; **n at the confirmation changes and saves nothing** (the plan wrote nothing: `TestDeclined`). Its finding, when the file has comments: "Saving hardn.yml loses its comments" (`CONFIG_COMMENTS_LOST`, the mock's words). Refused before anything changes: a configuration loaded from no file (`CONFIG_NOT_LOADED`), and a file whose bytes are not the ones it was read from (`CONFIG_CHANGED`: saving the whole configuration would undo an edit made since; §10, decision 6). "Configure DNS", whose list is `hardn.yml`'s, never rewrites `hardn.yml`.
10. **Every refusal before any change, with a report** (`steps.Refusal`: its `Why` and `Fix`, which `pkg/output` shows as the error and "To go on: …"; §6 lists each): `pkg/host/dns`'s `ErrContainer`, `ErrUnsupported`, `ErrManaged` (for each reported manager, what manages DNS and how to change it by hand: P-37 (a)), `ErrNotRunning`, `ErrOverridden` (naming the drop-in), `ErrDropInBlocked`, `ErrNoNameservers` ("No nameservers configured", "add a nameserver first"), `ErrNotApplicable`, `Validate`'s `ErrInvalid` (naming the entry and why) and `ErrUnknown`; then the save's. Each keeps its registered code (the `Refusal` wraps the primitive's error, which the registry matches; the control's own `Code` field is that code, pinned by the tests). The plan is the dry run: `Engine.Plan` refuses, nothing is written but the plan's run directory. Tested: `TestRefusals` (each, its code, its words, nothing written), `TestRefusalGoldens`.
11. **The words of the plan** are the adopted mock's (§7): the steps' titles, the findings in a few words (D1), a created file shown created (D2), and a failure that the change undid reported undone, in a few words, with its next step (D3; §8).

### Through `host.System`

12. **Reads and plans only through `host.System`**, `pkg/host/dns`, `pkg/config` and `pkg/engine/steps`, and renders nothing. depguard rule `dns-control-through-host` (the last of the rules, naming this unit) denies `os`, `io/ioutil`, `golang.org/x/sys`, `syscall`, `net` (but `net/netip`), `plugin`, `pkg/ui` and `pkg/output` to the package's non-test files, as `controls-through-host` already does for every control. Probed in a scratch copy outside the repository: a file importing `os` and `pkg/output` fails lint, under `controls-through-host`, and under `dns-control-through-host` once that rule's glob is narrowed to leave the directory out. The gallery's `TestPlayTouchesNothing` allows `pkg/config`'s `Read`, `Save` and `HasComments`, which `steps.PlanSave` names: they reach the host only through the `host.System` they are given, or not at all.

## 3. Facts relied on

| Fact | Source | Used for |
|---|---|---|
| What manages `/etc/resolv.conf` on each platform, how systemd-resolved collects `DNS=` across drop-ins, `try-reload-or-restart` reloading on systemd 256 and later and restarting before, `resolvectl dns` and `domain`'s forms, glibc's and musl's three nameservers, udhcpc's rewrite, Proxmox VE's node settings | [dns.md](../safety/dns.md) §3 (each with its source) | the control's whole reading and plan, which are `pkg/host/dns`'s |
| `resolvectl domain` with no argument prints each link's domains on the `Link <i> (<name>):` lines | systemd v257 `src/resolve/resolvectl.c` (`verb_domain`, `status_all`), via [dns.md](../safety/dns.md) §3 | D4: `Link.Domains` |
| The resolver queries the servers in the order listed | resolv.conf(5) **[unverified]** (not re-read for this unit) | §2.5, in order |
| `revert.Rollback` restores every backup, then runs the undo steps (a unit's plan step among them) | [revert.md](../safety/revert.md) §2.14 | §2.9: a failed save restores `hardn.yml` and rolls the DNS change back |
| yaml.v3 keeps comments on the nodes it decodes (head, line and foot), and a file of comments alone decodes to no node | **observed** by a probe of this unit (`go test -overlay` in `pkg/config`), and `TestHasComments` | `config.HasComments`: its line scan for a file with no node |
| `config.Save` writes the configuration's keys, canonical, through `host.WriteFileAtomicKeep`, keeping the file's mode and owner, and drops its comments | `pkg/config` (§3.19's save, `TestSave`) | §2.9 |

## 4. Written, not run (gate 5)

Nothing here has run on Linux or as root, and nothing calls the control yet. On the VMs of [vmtest.md](../safety/vmtest.md) (Debian 13, Ubuntu 26.04, Alpine 3.24), by hand on Proxmox VE 9, and on Ubuntu 24.04 or a Debian 12 cloud image where one is made, through a dev-only driver or unit (c)'s screen:

- each stock host's DNS line, against what `resolvectl dns` and `/etc/resolv.conf` show; the per-link finding on a DHCP host; the line on a NetworkManager desktop and in an LXC container on Proxmox VE;
- "Configure DNS" on Ubuntu 26.04 (the drop-in created, a reload), Ubuntu 24.04 or Debian 12 (a restart), Debian 13 (resolv.conf rewritten, its comments and options kept, then whether dhcpcd rewrites it on a lease), Alpine 3.24 (with and without udhcpc stopped, then a DHCP renewal), Proxmox VE 9 (the GUI's System > DNS after it); each planned again and empty; name resolution working through each;
- a preset and Add nameserver on Debian 13: `/etc/hardn/hardn.yml` saved at its loaded path, its mode and owner kept, its comments gone and the warning shown; a save made to fail (`chattr +i` on the file) after the change applied: `/etc/resolv.conf` and `hardn.yml` both as they were;
- a reload made to have no effect on Ubuntu 26.04 (a `DNS=` line resolved rejects, dns.md §4): the drop-in gone, resolved back on its servers, the report "Reloading systemd-resolved had no effect", the change "Rolled back", and `resolvectl dns` as the report advises;
- `hardn.yml` edited in another terminal between loading and saving: refused, `CONFIG_CHANGED`;
- every refusal of §6 on a host that has it (resolvconf on Debian, NetworkManager on a desktop, a later drop-in, the masked drop-in, resolved stopped).

## 5. The status lines

Every line the DNS row can show, in Figma's grammar (marker, label, the state's word, a faint detail), the details **proposed** (decision 8 is the row's design; its words are this unit's). At 64 columns a detail has 26 cells after "Configured" and 22 after "Not Configured" or "Not Applicable" (design-system README §9.3); a longer list is cut with the ellipsis.

| Host | State | Detail | Findings |
|---|---|---|---|
| the nameservers in use are `hardn.yml`'s, in order | Configured | the list: `192.0.2.54, 2001:db8::54` | the host's, as below |
| other nameservers, or the same in another order | Not Configured | `not hardn.yml's` | the host's |
| `hardn.yml` lists none | Not Configured | `none in hardn.yml` | the host's |
| `hardn.yml`'s list is one `dns.Validate` refuses here | Not Configured | `not valid in hardn.yml` | the host's |
| `/etc/resolv.conf` is systemd-resolved's link, and it does not run | Not Configured | `resolved not running` | none |
| NetworkManager's link or file | Not Applicable | `NetworkManager` | none |
| resolvconf's link or file | Not Applicable | `resolvconf` | none |
| a link hardn does not know, or a plain file a running resolved reads as another's | Not Applicable | `another program` | none |
| a container (detect's `Container.Detected` Yes) | Not Applicable | `container` | none |
| anything not established (the container, the manager, a file or command read) | Unknown | `can't read` | none; the error, with its code |

The findings of a host (`steps.DNSFinding`, proposed): `eth0 still uses its own nameserver 192.0.2.1` (`DNS_PER_LINK`, source `resolvectl dns`; "nameservers" for more than one, "delegate <id>" for a delegate), `dhcpcd may rewrite /etc/resolv.conf on a lease` (`DNS_DHCP_CLIENT`, the client's path), `udhcpc rewrites /etc/resolv.conf on each lease` (`DNS_UDHCPC`; "may rewrite" when whether it does is not established), `a udhcpc hook may rewrite /etc/resolv.conf` (`DNS_UDHCPC`, the hook's path), `Saving Proxmox VE's DNS settings rewrites /etc/resolv.conf` (`DNS_PROXMOX_NODE`). The gallery's words, decision 5, save the last two, new.

## 6. The refusals

Each is a `steps.Refusal`; `pkg/output` shows the Why with the code, then "To go on: " and the Fix. All **proposed**; "No nameservers configured" and "add a nameserver first" are origin/main's "No nameservers configured. Please add nameservers first." cut in two, and NetworkManager's the mock's (decision 4).

| Code | When | Why | Fix |
|---|---|---|---|
| `DNS_MANAGED` | NetworkManager | NetworkManager manages DNS here | set the nameservers with nmcli |
| `DNS_MANAGED` | resolvconf | resolvconf manages DNS here | set the nameservers through resolvconf |
| `DNS_MANAGED` | a link hardn does not know | /etc/resolv.conf links to `<target>`, which another program writes | set the nameservers with that program |
| `DNS_MANAGED` | a plain file beside a running resolved | systemd-resolved runs, and /etc/resolv.conf is not its link | link /etc/resolv.conf to /run/systemd/resolve/stub-resolv.conf, or stop systemd-resolved |
| `DNS_CONTAINER` | a container | This is a container: its manager provides /etc/resolv.conf | set the nameservers in the container's settings on its host |
| `DNS_UNSUPPORTED` | not of the four families | This host is not Debian, Ubuntu, Alpine or Proxmox VE | set the nameservers by hand |
| `DNS_UNKNOWN` | a fact not established | The host's DNS settings could not all be read | see the log for what failed, then try again |
| `DNS_RESOLVED_NOT_RUNNING` | resolved's link, resolved stopped | systemd-resolved is not running | start it with systemctl start systemd-resolved, then try again |
| `DNS_OVERRIDDEN` | a drop-in after hardn's sets `DNS=` | `<path>` sets the nameservers after hardn's drop-in | remove DNS= from it, then try again |
| `DNS_DROPIN_BLOCKED` | a mask or a directory at hardn's drop-in | /etc/systemd/resolved.conf.d/zz-hardn.conf is not a file | remove it, then try again |
| `DNS_NO_NAMESERVERS` | `hardn.yml` lists none | No nameservers configured | add a nameserver first |
| `DNS_INVALID` | an entry `dns.Validate` refuses | Nameserver `<entry>` is not an IP address / is listed twice / is not a usable address; or /etc/resolv.conf takes 3 nameservers at most | correct the nameservers in `<hardn.yml's path>` |
| `DNS_OPTION_NOT_APPLICABLE` | udhcpc's question answered y off Alpine | Only Alpine's udhcpc can be stopped rewriting /etc/resolv.conf | leave udhcpc as it is |
| `CONFIG_NOT_LOADED` | the save, with no `hardn.yml` read | No hardn.yml was read, so there is none to save to | create /etc/hardn/hardn.yml, then try again |
| `CONFIG_CHANGED` | the save, with `hardn.yml` changed since it was read | `<path>` changed since hardn read it | run hardn again, so that it reads the file as it is now |

`CONFIG_CHANGED` is new (`steps.ErrConfigChanged`, appended to the registry); the others were registered by `next/p3-output` and Phase 2.

## 7. The words of the plan and its result

All **proposed**, the mock's where it has them (gallery-dns.md §5).

| Where | Words | Source |
|---|---|---|
| The change's title | "Set the nameservers"; "Set nameservers; stop udhcpc rewriting" (Alpine, udhcpc stopped) | the mock |
| The change's summary | "Set the nameservers to 192.0.2.54, 2001:db8::54"; with ", and stop udhcpc rewriting /etc/resolv.conf" | this unit |
| The save's title, summary | "Save the configuration"; "Save the configuration to /etc/hardn/hardn.yml" | the mock; this unit |
| The save's finding | "Saving hardn.yml loses its comments" (`CONFIG_COMMENTS_LOST`, new) | the mock |
| The reload without effect | "Reloading systemd-resolved had no effect"; next step "Run resolvectl dns to see the servers in use."; the change "Rolled back" | the mock's `failure-proposed` |
| The save failing | "Saving /etc/hardn/hardn.yml failed" | this unit |
| A step's detail in the result | "dns: applied"; "saved /etc/hardn/hardn.yml" | this unit (the JSON document's `detail`) |

The goldens (`pkg/engine/controls/dns/testdata/golden/human/`, the core cells, and `testdata/json/`), from the real engine on `hostfake`, show each as `pkg/output` renders it: `check` (seven lines), `plan-ubuntu2604` (the drop-in against `/dev/null`, its reload, the per-link finding), `applied-ubuntu2604`, `plan-debian13-cloudflare` and `applied-debian13-cloudflare` (a preset: resolv.conf's diff, the DHCP client finding, the save and its lost comments), `plan-alpine324-udhcpc` (both files in one change, no udhcpc finding), `plan-pve9` (its file, its search domain kept, the node finding), `plan-configured` ("Nothing needs changing."), `refused-networkmanager` and `refused-empty`, `failed-reload` (D3, the mock's `failure-proposed` but for the log line, which the flow adds) and `failed-save` (both rolled back). The plan's id, random in its last part, is fixed in them (`stable`, as `pkg/output`'s real-engine goldens do).

## 8. The defects fixed (gallery-dns.md §8)

Each was verified by a probe first (`go test -overlay`, a test file in `pkg/engine/steps` run against the code before this unit, `/tmp`, not committed), which printed, on the Ubuntu 26.04 host of the gallery with eth0's DHCP server and domain:

```text
D4: Resolved.Domains=[] State.Search=[] Links=[link 2 (eth0)]
D1: plan findings 1, change findings 0
D2: diff head "--- /etc/systemd/resolved.conf.d/zz-hardn.conf"
D3: events [running failed]; rolledBack=true; failed change "dns" step "apply"; err=dns: reload-resolved: global nameservers [], want ["192.0.2.54" "2001:db8::54"]: the step ran, but the host does not show its effect
D3: drop-in after: lstat /etc/systemd/resolved.conf.d/zz-hardn.conf: no such file or directory
```

| | Defect | Fix | Evidence now |
|---|---|---|---|
| D1 | `steps.DNS` dropped `dns.Plan.Findings`: no DNS finding reached the plan, the document or the flow | `steps.DNS` carries them, coded through `steps.FindingCode("DNS", kind)` and worded by `steps.DNSFinding` (`steps/findings.go`), the registry's four DNS codes reused | `TestDNSChange`, `TestPlans`, the plan goldens' warnings and the JSON's `findings[]` |
| D2 | a created file's diff was taken against `""`, so the new drop-in read as an empty file edited | the diff of a step whose path the plan read absent is taken against `/dev/null` (`engine.FileDiff(path, nil, ...)`) | `TestDNSChange`, `plan-ubuntu2604.json` (`--- /dev/null`); the gallery's `TestDNSPlansAreReal` no longer allows the difference |
| D3 | when the reload had no effect, `dns.Apply` undid its steps, `steps.DNS` dropped its journal step, and the engine reported nothing rolled back, with `dns`'s raw error as the report | `engine.Ran.Undone` (new): `steps.DNS` sets it when `dns.Apply` failed having undone its steps (`Result.Restored`); the engine then sends `EventRolledBack` for the failed change, first, once its rollback has finished, and sets `ApplyError.Undone`; `pkg/output` names the change rolled back (the report, and `result.rollback.changeUndone`, new); and `steps.Failure` (new) words the failure, "Reloading systemd-resolved had no effect", with its next step, which `pkg/output` shows in place of the error, the code on the step's line, in `Human` and through `FlowEngine` alike | `TestFailedReload`, `TestUndoneByItself`, `failed-reload` (human and JSON); the gallery's `TestDNSFailureIsTheEngines` now holds the mock's `failure-proposed` sequence to the engine, step for step and word for word, and checks that `failure` (the report before this unit) no longer is the engine's |
| D4 | `dns.Read` dropped each link's search domains (`resolvectl domain`) | read into `Link.Domains`, matched by index and name, a link with domains and no servers added; never written | `TestReadLinkDomains` (`pkg/host/dns`); the row's evidence |

Also: [dns.md](../safety/dns.md) §5's API now lists `Plan.Check` and `Unit`, with `Link.Domains` and `Finding.Link` (the per-link finding's link, which `DNSFinding` words).

Gallery-dns.md §8 left one choice to this unit: whether the rolled-back report goes with the failed line or the report alone names it. **Both, as the mock draws it**: the step's line keeps "Failed DNS_NOT_EFFECTIVE" where nothing animates (the goldens, a pipe), and the report names the change "Rolled back"; on a terminal that animates the steps, `pkg/ui/flow` redraws the row as "Rolled back" when the event arrives, and the report names it too.

## 9. API

```go
package dns // pkg/engine/controls/dns (new)

const ID = "dns.nameservers"
const ChangeSet, ChangeSave = ID + "/set", ID + "/save"
const TitleSet, TitleSetStop = "Set the nameservers", "Set nameservers; stop udhcpc rewriting"
const DetailNone, DetailNotHardn, DetailInvalid, DetailNotRunning, DetailContainer, DetailNetworkManager,
	DetailResolvconf, DetailOther, DetailCantRead = ... // §5
func Controls(cfg *config.Config) []engine.Control
type Nameservers struct { Config *config.Config; StopUDHCPC bool } // an engine.Control

package steps   // added: type Failure struct { Text, Next string; Err error }; func DNSFinding(dns.State, dns.Finding) engine.Finding
                //   func PlanSave(ctx, sys, id string, cfg *config.Config, after ...string) (engine.Change, error)
                //   const CodeCommentsLost, CodeConfigChanged, ResourceConfig; var ErrConfigChanged ([steps.md](steps.md) §6)
package engine  // added: Ran.Undone bool; ApplyError.Undone bool (engine.md §6)
package config  // added: func Read(ctx, sys host.System, path string) (*Config, error); func (*Config) LoadedFrom(data []byte) bool;
                //   func HasComments(data []byte) bool; Save records what it wrote (LoadedFrom)
package dns     // pkg/host/dns, added: Link.Domains []string; Finding.Link *Link (dns.md §5)
package output  // added: Rollback.ChangeUndone bool (result.rollback.changeUndone); codes CONFIG_CHANGED, CONFIG_COMMENTS_LOST
```

**The engine's API change is two fields**, both for D3 (§8): `Ran.Undone`, which a change kind sets, and `ApplyError.Undone`, which the engine sets. Nothing else in `pkg/engine` changed: neither `Control.Check` nor `Input` (§10, decisions 1 and 2).

## 10. Decisions of this unit

1. **How `Check` gets `hardn.yml`'s list:** the control's value carries it (`Nameservers.Config`), built with `Controls(cfg)` from the configuration the caller loaded. *Alternatives:* `Control.Check` taking `Input` (every control changes); an optional interface with a configuration and a new `Engine.Check` parameter (an engine API change for one control). The value is zero engine change, and the status model builds its rows from the loaded configuration anyway.
2. **How Alpine's answer reaches `Plan`: a field of the control (`StopUDHCPC`), not an `Input` field** (the task proposed "the smallest Input addition"). The engine builds `Input` from `Engine.Plan`'s own parameters and `Options`, so any `Input` field needs a new `Engine.Plan` parameter or an engine-wide option for one control's per-plan answer; the control value already travels with the plan to the plan made again under the run lock (`Plan.controls`), so the answer the user gave is the one applied, with no engine change. Say if an `Input` field is wanted instead.
3. **`Applies` always true** (§2.2): Not Applicable through `Check`, refused through `Plan`, for a container and the managers hardn only reports.
4. **In order** (§2.5).
5. **Unknown for any fact not established**, even with the nameservers read: `dns.State.Err` does not say which fact failed, and `PlanDNS` refuses the same state.
6. **A `hardn.yml` changed since it was read is refused, not saved over** (`CONFIG_CHANGED`, `config.Config.LoadedFrom`): the save writes the whole configuration, so saving over another edit would undo it while the plan said "Save the configuration". The loader records the bytes it read (`LoadConfig` and `config.Read`), and `Save` the bytes it wrote. A configuration created by `CreateDefaultConfig` records the file as written.
7. **An invalid list is Not Configured, "not valid in hardn.yml"**, not Unknown: the host is established not to use a list that cannot be used, and `Plan` refuses it with the entry named.
8. **The save only when the list differs from the file's, compared as written**: "Configure DNS" never rewrites `hardn.yml`; a host already as asked with another list in `hardn.yml` gets the save alone.
9. **The words of a step's failure** are a type of `pkg/engine/steps` (`Failure`), which any change kind can return, and `pkg/output` shows alone, without the change's summary or the code, which the step's line gives (the mock's `failure-proposed`); the JSON document's `message` keeps the whole error. Only DNS's reload without effect and the save are worded; other DNS failures keep `dns`'s error.

## 11. Not in this unit; inputs to later units

- **Unit (b2), the parity table:** origin/main's end state against this path's, per distribution, from the legacy characterization goldens `pkg/characterization/testdata/golden/ops/dns_direct`, `dns_empty_nameservers`, `dns_resolvconf` and `dns_systemd_resolved` (`{alpine,debian,proxmox,ubuntu}.golden` each): `resolved.conf` overwritten and `Domains=lan` against hardn's drop-in and the domains kept (V19), the empty list written against `DNS_NO_NAMESERVERS`, resolvconf's `head` written against `DNS_MANAGED` (P-37 (a)), the direct strategy through the symlink against the manager classified, no backup against the journal and `dns.Apply`'s backups (V18), each difference tied to its V-number or decision.
- **Unit (c), the screen:** `Nameservers` with the form's answer, built after the form (the flow's `FlowEngine` takes its `Controls` before it runs, so the screen's adapter sets them once the answer is in); the findings before the confirmation (decision 5: the review's warning lines from `dns.State.Findings`, filtered as `PlanDNS` filters them, and `CONFIG_COMMENTS_LOST` when the list changes); Add's inline errors through `dns.Validate` (decision 9); the presets (decision 7); the screen refreshed after a change, and the edited configuration adopted once saved (its `LoadedFrom` is the saved bytes); the DNS screen's Search Domains row from `Link.Domains`.
- **The main menu's DNS row (P-01, decision 8)** and the compact main menu's height: `dns.Controls(cfg)` among the status model's controls.
- **Unit (d):** the legacy path gated behind `HARDN_UI=classic`.
- **Run All (slice 8):** one save of `hardn.yml` per plan (two are `ErrConflict`), so its controls' edits go through one save.
- **The VM run** (§4).

## 12. Size and review

- **Code:** about 785 lines of non-test Go added and 30 removed (`git diff --numstat`): the control 380 (80 of them whole-line comments), `pkg/config/file.go` 97, `steps/save.go` 84, `steps.DNS` and its findings about 115, the engine 27, `pkg/output` 45, `pkg/host/dns` 33. **Tests:** about 1,280 lines (the control's `dns_test.go`, `apply_test.go`, `helpers_test.go`, with a model of systemd-resolved; `TestReadLinkDomains`, `TestDNSChange`, `TestPlanSave`, `TestUndoneByItself`, `TestRead`, `TestHasComments`). **Testdata:** 19 new golden files (12 human, 7 JSON) and four regenerated in `pkg/output` (`codes.golden` and `sentinels.golden` with the two codes and the sentinel; `json/rolled-back.json` and `json/unjournalled.json` with `"changeUndone": false`). Over the guideline of about 500 changed lines: the unit is a control that both checks and plans, a change kind every slice reuses, four defects across four packages, and a test per status line, refusal and platform; the split would have been the defects and the save first, then the control.
- **Golden diffs:** only those above; no legacy or characterization golden changed. The gallery's `TestDNSFailureIsTheEngines` and `TestDNSPlansAreReal` are held to the engine as it is now (§8); its goldens are unchanged.
- **`go.mod` and `go.sum`** unchanged. **`git status --short --ignored`** before each commit listed only this unit's paths.
