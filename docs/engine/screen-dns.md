# The DNS screen on the real control (`pkg/screens/dns`, `screen.DNS`)

> **Unit:** refactor plan slice 2 (DNS, the pilot of the full flow), unit (c), `next/s2-screen`: "Screen on the design system and prompt kit" (plan §2, "Phases 3-5 together", §3.22, approved 2026-10-21), on `next/s2-flow-review` (the findings before the confirmation, its first part). It builds the screen the maintainer reviewed as the gallery's mock and adopted on 2026-10-22 ([gallery-dns.md](gallery-dns.md) §4, decisions 1 to 9), drawn from the host through the DNS control ([control-dns.md](control-dns.md)) and acting through the engine and the flow. Decisions implemented: 1 (every change through the plan; (i) origin/main's descriptions kept, (ii) the last nameserver not removed, (iii) a failed save rolling the change back), 2 (the host's search domains shown and kept), 3 (P-30 (b), the empty list), 4 (P-37 (a), hosts hardn only reports), 5 (the findings before the confirmation, `next/s2-flow-review`), 6 (Alpine's question on the form), 7 (the presets), 8 (the DNS row's design, for P-01's unit: not drawn here), 9 (Add's inline errors); plan §3.19 as amended (the save edits hardn.yml's list alone, at its loaded path). V-numbers: V18, V19; W1, W2 (the result kept above the screen drawn again); side-effects rows 21 to 23 and 29 ([gallery-dns.md](gallery-dns.md) §6).
> **Status:** built and tested on macOS, in process: the screen end to end through its entry point on the recording fake (`hostfake`), scripted answers, every item, refusal, failure and redraw; and in the gallery's playable mode on its simulated hosts, the transcripts pinned on the core set. No real terminal and no host were used; nothing has run on Linux: every host claim is **written, not run (gate 5)** (§4). Nothing is wired into `cmd/hardn` or the live main menu (unit (d)); the legacy DNS screen (`pkg/menu/dns_menu.go`) and its characterization goldens are untouched. **Every word new to origin/main is proposed** (§5), the adopted mock's where it has them.

## 1. Why

origin/main's DNS screen applies Configure DNS on selection, with no confirmation and no backup; its Add, Remove and presets change `./hardn.yml` alone and wipe their message by redrawing ([gallery-dns.md](gallery-dns.md) §6, §7). Unit (a) drew the replacement on fixture data, which the maintainer adopted; unit (b) built the control and the save; `next/s2-flow-review` listed a plan's findings before its confirmation. This unit draws the adopted screen from the host itself and runs each item through the engine, so that one DNS screen exists, on the real code, ready for unit (d) to call from hardn's menu.

## 2. Guarantees

### Where the screen lives

1. **The drawing in `pkg/ui`, the reading and the running outside it.** `pkg/ui/screen` holds the screen's view-model, `screen.DNS` (what manages `/etc/resolv.conf`, the nameservers and search domains in use, hardn.yml's list), which draws origin/main's screen on the sub-menu layout (`SubMenu`), its workflows' review (`Review`) and title bar (`Step`), and the presets (`DNSPresets`). `pkg/screens/dns`, new, reads the host and hardn.yml, fills the view, binds the items, runs each change's form and flow on `output.FlowEngine` with the DNS control, and draws the screen again. **One entry point**, `Run`, which unit (d) calls (§6, §7).
2. **Through `host.System` only.** `pkg/screens/dns` reaches the host only through the `host.System` it is given, the DNS control, `pkg/host/dns` and the engine it is given, and draws only through `pkg/ui` and `pkg/output`: depguard rule `screens-dns-through-host` (the end of the rules, naming this unit) denies `os`, `io/ioutil`, `golang.org/x/sys`, `syscall`, `net` (but `net/netip`) and `plugin` to its non-test files. **`pkg/ui` reads nothing of the host**: rule `ui-renders-only`, new, denies `pkg/host`, `pkg/engine`, `pkg/config`, `pkg/output` and `pkg/screens` to `pkg/ui`'s non-test files, which none imported. Both probed in a scratch copy outside the repository: a file of `pkg/screens/dns` importing `os`, and one of `pkg/ui/screen` importing `pkg/host`, each fail lint under its rule. It never calls `engine.New` (§8, decision 2), so the gallery's `TestPlayTouchesNothing` walks it unchanged but for naming it among the packages it must reach.

### The screen (decisions 2, 3, 4, 8)

3. **Each round reads the host and draws the screen** (`nav.Prompter.Run`'s draw): `dns.Read` and the control's `Check`, against hardn.yml as the screen holds it, on the facts given. Drawing reads only (`TestScreen`: no write, no engine, no run).
4. **The title bar and sections as the adopted mock**, origin/main's words: "DNS Configuration"; "Current DNS Configuration" with three rows; "Configured Nameservers", hardn.yml's list as written there, "Nameserver 1" and on, or with none P-30 (b)'s muted item, "No nameservers configured" (`ValueNone`, DEV-061).
5. **"DNS Implementation"** is what `dns.Read` found manages `/etc/resolv.conf`: "direct (/etc/resolv.conf)" (a file hardn writes), "Proxmox VE (/etc/resolv.conf)", "systemd-resolved"; a manager hardn only reports as its name and "reported only" in `status.warn` (P-37 (a)): "NetworkManager, reported only", "resolvconf, reported only", "another program, reported only" (a link hardn does not know, or a file a running resolved reads as another's), and in a container "container, reported only" (`dns.ErrContainer`); Unknown where it is not established (the family not established, a family hardn does not configure, `/etc/resolv.conf` unreadable or neither a file nor a link).
6. **"Nameservers" and "Search Domains" in use, only where they were read**: a file's lines (a file or Proxmox VE's); on systemd-resolved while it runs, its global list, then each delegate's and each link's own, each with its name as the DNS findings give it, "192.0.2.1 (eth0)", "192.0.2.9 (delegate corp)"; the route-only domains ("~corp.example", "~.") left out of the search domains, as they route queries and search nothing. "None" when read and empty. **Unknown otherwise, never a guess**: a manager hardn only reports, a container, a fact not established (`dns.State.Err`, or the control's `Check` Unknown), and systemd-resolved stopped, whose link then points at a stub nothing answers (`TestScreen`, `TestReadResolved`).
7. **The six items and Return**, origin/main's labels and descriptions (decision 1 (i): Add's and Remove's understate the action; the alternatives stay proposed, not built); **item 3 only when hardn.yml lists a nameserver**, so that with none it is neither shown nor taken: the hint is `[0-2, 4-6 or q]`, and 3 is refused as any option the menu does not show; origin/main's prompt, "Enter selection" (P-29 (a)). No status row and no main-menu row: decision 8 is the main menu's (P-01).

### The items (decision 1, (i) to (iii); decisions 6, 7, 9)

8. **Every change through the plan**, under its item's title bar: the form, if any; the review, "DNS Configuration" with the nameservers the host will use (`dns.Validate`'s form of the list) and the search domains it keeps, then each finding of the plan as a warning line (`next/s2-flow-review`); the plan, the DNS control's change, then hardn.yml's save after it when its list changes (`dns.nameservers/save`, the list alone, edited in place); the question, y/N, as no DNS change locks anyone out (brief 2.4); the steps; the result. **n changes and saves nothing; a dry run stops at the plan and saves nothing; 0 at a form's first field goes back to the screen, planning nothing** (`TestNothingWritten`). The control is built when the flow plans, once the form is answered, from a copy of hardn.yml with the list the item asks for (`FlowEngine` reads its controls and configuration then), so the answer shown is the one applied: the engine plans again under its run lock with the same control and configuration.
   - **Configure DNS** plans hardn.yml's list, which it never saves (it is the file's). With none configured it is **refused before any plan** in decision 3's words, "Please add nameservers first." (origin/main's second sentence, the first said by the muted item; §11).
   - **Add nameserver** asks origin/main's "Enter nameserver IP address" and checks each entry as `dns.Validate` would for the list with it added, on the host's manager, each refusal an inline result under the prompt (⨳, `(x)` in ASCII; DEV-062), the field asked again: "Nameserver cannot be empty", "Invalid IP address format", "Nameserver <ip> is already configured" (a warning, `nav.Warn`, as origin/main warns of it), all origin/main's; "Not a usable nameserver address" for what `dns.Validate` refuses that origin/main took (a zone, IPv4-mapped, unspecified, multicast, 0.0.0.0/8, the broadcast address, IPv6 link-local, systemd-resolved's own stub addresses on resolved); and "/etc/resolv.conf takes 3 nameservers at most" for a fourth on a file (the control's `DNS_INVALID` words). The entry's own kind is said before the count. A list configured that `dns.Validate` refuses already (an entry no address, by hand) takes any usable entry: the plan then refuses the list, naming the entry at fault, with how to correct it. The question names the address as written to the host, "Add nameserver 2001:db8::53?" for "2001:DB8::53" (`TestAdd`, `TestAddCheckIsValidate`: the check refuses exactly what `dns.Validate` refuses after the list, on a file, Proxmox VE and systemd-resolved, with none, two and three configured, each kind in its words).
   - **Remove nameserver** asks "Select a nameserver to remove", a choice of the list as written; with one configured it is **refused before asking**, "The last nameserver cannot be removed." (decision 1 (ii): the plan would refuse an empty list); the question names the entry, "Remove nameserver 2001:db8::54?" (`TestRemove`).
   - **The presets** (decision 7): Cloudflare 1.1.1.1, 1.0.0.1; Google 8.8.8.8, 8.8.4.4; Quad9 9.9.9.9, 149.112.112.112, origin/main's, IPv4 only, each replacing the list; the question is the item's label, "Use Cloudflare DNS?" (`TestPresets`).
   - **Alpine** (decision 6): where `dns.Read` finds udhcpc rewriting `/etc/resolv.conf` (Alpine, a file, `UDHCPC.Rewrites` Yes), every change's form asks last "Stop udhcpc rewriting /etc/resolv.conf?", y/N, default N; y is the control's `StopUDHCPC`, which adds `RESOLV_CONF="no"` to `udhcpc.conf` in the same change, titled for both; n keeps it, and the review warns (`TestConfigure`).
9. **Refused before asking anything** (decision 4, P-37 (a)): on a host every plan refuses whatever its list, every item writes a Warning callout and the screen is drawn again: what stops it and how to go on, the control's own report (`steps.Refusal`'s Why, then its Fix as a sentence): "NetworkManager manages DNS here. Set the nameservers with nmcli.", resolvconf's, another program's link, a plain file beside a running resolved, a container ("This is a container: its manager provides /etc/resolv.conf. Set the nameservers in the container's settings on its host."), a family hardn does not configure, and a fact not established ("The host's DNS settings could not all be read. See the log for what failed, then try again."). Which hosts those are is the control's new `HostRefusal` (§6), held to the plan's refusals word for word (`TestHostRefusal`); no engine is made, nothing is planned or written (`TestRefusedBeforeAsking`). **The plan's other refusals** (systemd-resolved stopped, a later drop-in, hardn's drop-in path blocked, an invalid list, hardn.yml changed since it was read, the save's) come from the plan, after any form, as the flow's report with its code and "To go on: …" (`TestRefusedByThePlan`).
10. **A failure is the engine's**, rolled back with its report (decision 1 (iii); control-dns.md D3): a reload of systemd-resolved with no effect, "Reloading systemd-resolved had no effect", the change "Rolled back", "Run resolvectl dns to see the servers in use." (`TestFailedReload`); a save refused after the change applied, "Saving /etc/hardn/hardn.yml failed", the change rolled back, so the host and hardn.yml agree (`TestFailedSave`). No confirm-or-revert timer is armed (brief 2.4).

### After a change (W1, W2)

11. **The result stays on the screen, and the screen is drawn again under it from a fresh reading** of the host and of hardn.yml: after Add on Debian 13 the nameservers in use are the three applied and the list has three; after Configure DNS on Ubuntu 26.04, resolved's global servers then eth0's; after a failure, the host as it was. What the flow does not draw, `output.AfterFlow` (a refused plan's findings; no DNS change has a countdown), is written under the flow first.
12. **The configuration adopts hardn.yml as saved.** Each change plans a copy of the configuration with the list it asks for, which the save writes and records (`config.Save`: `LoadedFrom` the bytes written). Once the change has applied, the screen reads hardn.yml at its path (not through a link) and, when the file holds the bytes the copy saved and not the ones the configuration records, the configuration becomes the copy: its list, its recorded bytes, every other setting as it was in memory (the save wrote the list alone, `dns.Setting`, so a flag's value in memory is never saved). **So the next change's save is not refused `CONFIG_CHANGED`** (control-dns.md §11's open item): two presets in a row both save (`TestPresets`, `play/dns-presets`). A save the engine rolled back, a dry run, a decline and a failure adopt nothing (`TestFailedSave`, `TestNothingWritten`). Mutation-checked: with the adoption removed, `TestAdd`, `TestRemove` and `TestPresets` fail.
13. **Return goes back** to the caller (`Run` returns nil); **q quits** from the screen and from any prompt of a change (`nav.ErrQuit`); the end of the input and Ctrl-C abort (`prompt.ErrAborted`) (`TestNavigation`). `Options.Ran` is given what each planned change did (`output.Run`), for the audit log (§7).

## 3. Facts relied on

| Fact | Source | Used for |
|---|---|---|
| What manages `/etc/resolv.conf` on each platform, systemd-resolved's links and delegates, `resolvectl dns` and `domain`'s forms, route-only domains written `~`, glibc's and musl's three nameservers, udhcpc's rewrite | [dns.md](../safety/dns.md) §3 (each with its source); control-dns.md §3 | §2.5, §2.6, §2.8 |
| `dns.PlanDNS` refuses the manager and the host's unread facts before it validates a list | `pkg/host/dns` `PlanDNS` (code) | `HostRefusal` (§2.9) |
| `config.Save` records the bytes it wrote; the save is the plan's last change, so a later failure rolls it back from its backup | `pkg/config` `Save`; [steps.md](steps.md) `PlanSave`; control-dns.md §2.9 | §2.12 |
| The layouts' content stops at 64 columns, and a row's value is cut to one line with the ellipsis; at 60 columns a row stacks its value under its key | design-system README §9.3; `component.KeyValue` | §11, a long row cut |

## 4. Written, not run (gate 5)

Nothing here has run on Linux or as root, and nothing calls `Run` yet. On the VMs of [vmtest.md](../safety/vmtest.md) once unit (d) wires it (Debian 13, Ubuntu 26.04, Alpine 3.24; Proxmox VE 9 by hand): the screen on each stock host against `resolvectl dns`, `resolvectl domain` and `/etc/resolv.conf`; each item applied, the screen drawn again showing the host as changed and `/etc/hardn/hardn.yml` saved at its loaded path, its other lines and comments kept; two presets in a row; Alpine's question answered y, then a DHCP renewal; a save made to fail (`chattr +i`); a NetworkManager desktop and an LXC container refusing every item; and name resolution working after each. control-dns.md §4's runs cover the control's own claims.

## 5. Words

origin/main's where it has them; the adopted mock's where it has them ([gallery-dns.md](gallery-dns.md) §5); **every other word is proposed**.

| Where | Words | Source |
|---|---|---|
| Title bar, sections, rows, items, descriptions, "Return", "Enter selection", "No nameservers configured" (muted) | as the adopted mock | origin/main; P-30 (b), P-29 (a) |
| "DNS Implementation" | "direct (/etc/resolv.conf)", "systemd-resolved"; "Proxmox VE (/etc/resolv.conf)", "NetworkManager, reported only" | origin/main; the adopted mock |
| "DNS Implementation", not mocked | "resolvconf, reported only", "another program, reported only" | gallery-dns.md decision 4's text, **proposed** |
| "DNS Implementation", new | "container, reported only" (a container, the status row's "container") | **proposed** |
| "Nameservers", "Search Domains" in use | "192.0.2.1 (eth0)", "example.net (eth0)", "None"; Unknown | the adopted mock |
| A delegate's server, new | "192.0.2.9 (delegate corp)" (as the findings name it) | **proposed** |
| Add's field and errors | "Enter nameserver IP address"; "Nameserver cannot be empty", "Invalid IP address format", "Nameserver 192.0.2.54 is already configured" (a warning); "Not a usable nameserver address" | origin/main; decision 9 (adopted) |
| Add's error for a fourth on a file, new | "/etc/resolv.conf takes 3 nameservers at most" | the control's `DNS_INVALID` Why, **proposed** there |
| Remove's field | "Select a nameserver to remove" | the mock, **proposed** |
| Alpine's question | "Stop udhcpc rewriting /etc/resolv.conf?" | the mock, **proposed** |
| Questions | "Configure DNS?", "Add nameserver 192.0.2.53?", "Remove nameserver 2001:db8::54?", "Use Cloudflare DNS?" | the mock, **proposed** |
| Refusals before asking | "Please add nameservers first."; "The last nameserver cannot be removed."; each host refusal as the control's Why, then its Fix as a sentence, "NetworkManager manages DNS here. Set the nameservers with nmcli." | origin/main (decision 3); the mock, **proposed**; control-dns.md §6, **proposed** |
| The review, the plan, the steps, the failures | "DNS Configuration", "Nameservers", "Search Domains"; the findings; "Set the nameservers", "Save the configuration"; "Reloading systemd-resolved had no effect", "Saving /etc/hardn/hardn.yml failed" | the mock; control-dns.md §5, §7 (**proposed** there) |
| The gallery's, dev-only | "DNS hosts", "Select a host", each host's label, about and note | the gallery's, **proposed** |

## 6. API

```go
package dns // pkg/screens/dns (new)

type Options struct {
	Prompter  nav.Prompter                                   // the one line reader, the writer, the render context (its term.Env)
	System    host.System                                    // the host
	Facts     detect.Facts                                   // detect's
	Config    *config.Config                                 // hardn.yml as loaded; adopts the file as saved (§2.12). Required
	NewEngine func(progress func(engine.Event)) *engine.Engine // one per change, progress its Options.Progress. Required
	DryRun    bool
	LogPath   string                                         // a failure's report names it
	Now       func() time.Time                               // AfterFlow's clock; nil is time.Now
	Ran       func(output.Run)                               // what each planned change did, for the audit log
}

// Run shows the DNS screen and runs its items until 0 (nil) or q (nav.ErrQuit);
// prompt.ErrAborted on the end of the input or Ctrl-C; fs.ErrInvalid, writing
// nothing, without System, Config or NewEngine.
func Run(ctx context.Context, o Options) error

package screen // pkg/ui/screen, added: the DNS screen's view-model (the mocks retired)
type DNS struct { Manager DNSManager; InUse, Search []DNSEntry; Read bool; Configured []string }
func (DNS) SubMenu() SubMenu; func (DNS) Review(nameservers []string) flow.Review; func (DNS) Step(label string) WorkflowStep
type DNSManager uint8 // DNSManagerUnknown, DNSFile, DNSProxmox, DNSResolved, DNSNetworkManager, DNSResolvconf, DNSOther, DNSContainer
type DNSEntry struct{ Value, Link string }; func (DNSEntry) String() string
const DNSConfigure, DNSAdd, DNSRemove = "1", "2", "3"
type DNSPreset struct { Key, Label string; Nameservers []string }; func DNSPresets() []DNSPreset

package dns // pkg/engine/controls/dns, added
// HostRefusal is the refusal, with its report, every plan on the host makes
// whatever its list; nil when there is none. Pure.
func HostRefusal(s hostdns.State, readErr error) error
```

No new error sentinel and no new code: every refusal and failure the screen shows is the control's, the engine's or the save's, already in `pkg/output`'s registry; `Run`'s misuse error wraps `fs.ErrInvalid`. No change to `pkg/engine`, `pkg/host`, `pkg/output`, `pkg/config`, `pkg/ui/flow`, `pkg/ui/nav` or the components.

## 7. What unit (d) must do

Unit (d) replaces the legacy DNS path (the maintainer, 2026-10-22: `HARDN_UI=classic` withdrawn from plan §3.14; menu item 3 and `-g` to the new control; the legacy screen deleted after (b2)). For the screen:

- **Call `Run` from the main menu's item 3** in place of `menu.NewDNSMenu(...).Show()` (`pkg/menu`), with what `cmd/hardn` has: `Prompter` on the one line reader every prompt shares (`prompt.Stdin()`), the writer and render context of stdout's `term.Env` (`streamEnv(os.Stdout)`); `System`, the host it builds (`host.NewReal` with its run log); `Facts`, `detect.Detect` once; `Config`, the configuration it loaded (`config.LoadConfig`, which records its path and bytes; the screen edits a copy and adopts it, so the menu sees the list as saved when the screen returns); `NewEngine`, a function that copies its `engine.Options` (its `Revert` options, `Session` from `revert.ReadSession`, `Backups` from `backupPath` and `enableBackups`, `PID`, a nil `Hold` for the engine's own) and sets `Progress` to the one given, then calls `engine.New`; `DryRun`, the dry-run mode (plan §3.19: the flag wins, otherwise the configuration); `LogPath`, the legacy log (`output.LegacyLogPath`).
- **Map what `Run` returns**: nil back to the main menu; `nav.ErrQuit` quits hardn, as every menu's q; `prompt.ErrAborted` exits 130 (`prompt.ExitCode`).
- **The audit log** (`Ran`): fill each `output.Run`'s `HardnVersion`, `Hostname` and `Facts`, then `output.Append(ctx, sys, output.AuditPath, output.NewRecord(run, now))`, if the audit log is wired by then.
- **`-g`** (`--configure-dns`) goes to the control without the screen (`dnsctl.Controls(cfg)` through the engine, `output.Human`), not to `Run`.
- **Delete** `pkg/menu/dns_menu.go` and its goldens after (b2), as the plan says; Run All keeps the legacy DNS service until slice 8.

## 8. Decisions of this unit

None is the maintainer's; each is for review.

1. **`pkg/screens/dns`**, one package per area's screen, beside `pkg/engine/controls/<area>`: not `pkg/ui`, which draws and must not reach the host (§2.2), nor `pkg/output`, the output contract of every area. The glue is not a main package either: unit (d) calls it from `pkg/menu` or `cmd/hardn`, and the gallery plays it.
2. **The engine comes from the caller, built by `NewEngine(progress)`**, one per change, rather than `Run` taking `engine.Options` and calling `engine.New`: the flow's adapter needs the engine built with its `Progress`, and the gallery's guard allows `engine.New` outside the gallery only in `pkg/engine/confirm`, with every engine of play given the gallery's hold; with the constructor the gallery builds it so, and the guard is unchanged. cmd/hardn's constructor is three lines (§7).
3. **Every host-level refusal before asking, not only P-37's managers**: a container, a family hardn does not configure and a fact not established refuse every plan whatever the list, so asking Add's address first would ask for an answer refused anyway. The control gives them (`HostRefusal`, new, pure, held to the plan's words by `TestHostRefusal`), so the screen copies no words. systemd-resolved stopped, a later drop-in and a blocked drop-in path stay the plan's refusals, after any form: `PlanDNS` makes them only after it has validated a list. *Alternative:* the reported-only managers alone, the task's minimum, and the others refused by the plan after Add's question.
4. **The configuration adopts a copy only when the file holds the copy's saved bytes** (§2.12), read back after the apply, rather than on the flow's outcome alone: a save that ran and was rolled back, or any other path that leaves the file as it was, adopts nothing.
5. **Unknown rather than "None" while systemd-resolved is stopped**, and for every manager hardn only reports (§2.6): the nameservers in use are not read there.
6. **The route-only domains are left out** of "Search Domains" (§2.6): they route queries to a link and are no search domain.
7. **Add's check judges the entry first, then the count** (§2.8), and **a list already invalid takes any usable entry**, its fault the plan's report to name. The check calls `dns.Validate` itself, with the host's manager, where the mock copied its rules.
8. **`output.AfterFlow` inside the screen, `Ran` for the caller**: the screen writes what the flow does not draw before drawing itself again; the audit record, which needs the host's facts and hardn's version, is the caller's.
9. **The DNS mocks are retired, not kept as a design record** ([gallery-dns.md](gallery-dns.md) §11): the real screen draws the same eight hosts in play, with every sequence the mocks had a transcript of; kept, their fixture plans and their copy of `dns.Validate` would drift from the real code, held only by five tests of their own. git keeps them (`next/s2-flow-review`, f0111c4), and gallery-dns.md records them. `screen.Mock.Refusals`, which only they used, is kept for the next slices' mocks.
10. **The DNS scenarios of `play engine` stay** ([gallery-engine.md](gallery-engine.md) §8): they run the control with no screen, as the CLI and Run All will, and they are not a DNS screen.
11. **The gallery plays the screen with a note** above it, its simulated host named (`dns/<host>`), also from the main menu's 3, whose mock shows Figma's Debian 12 host while the screen reads web-01's Debian 13.

## 9. Tests

- `pkg/screens/dns` (through `Run`, on `hostfake` with a model of systemd-resolved after the control's tests'): `TestScreen` (every host's rows, the list or the empty item, item 3 shown and taken only with a list, drawing reads only), `TestReadResolved` (links, a delegate, route-only domains; the global list after Configure DNS), `TestConfigure` (Debian 13, Ubuntu 26.04, Proxmox VE 9, Alpine 3.24 answered y and n: the file or drop-in, no save, the finding before the question, udhcpc's question and its effect, the screen drawn again under the result), `TestAdd` (each inline error in order, the question's address, the host, hardn.yml and the configuration adopted, the redraw), `TestRemove`, `TestPresets` (each preset; two in a row both saved), `TestNothingWritten` (n, a dry run, a dry run of Add, 0 at Add's field and Remove's choice: nothing written, nothing adopted), `TestRefusedBeforeAsking` (NetworkManager, resolvconf, a container, a fact not established: every item, the control's words, no engine; no list; one), `TestRefusedByThePlan`, `TestFailedSave`, `TestFailedReload`, `TestNavigation`, `TestRunNeeds`, `TestAddCheckIsValidate`.
- `pkg/ui/screen`: `TestDNSScreen` (rows and items for each kind of reading), `TestDNSWords` (origin/main's items and presets drawn, `DNSPresets` a copy, `Step`, `Review`).
- `pkg/engine/controls/dns`: `TestHostRefusal` (each of the control's refusals: the plan's report exactly for the host-level codes, nil for the others; nil on every host that plans).
- The gallery: `TestPlayDNS` (every item and failure on its host: the transcript in order, the host left, the DNS row after, what changed call by call, the hold taken once by each failure), `TestDNSScreenHosts` (each host detected with every fact, the screen drawn reading only), `TestPlayDNSUsage` (a host that is not one, `-fail`, a sequence other than normal or dry-run; the main menu's 3 and the hosts' menu), `TestPlayReaches` (the DNS link), `TestPlayTouchesNothing` (walks `pkg/screens/dns`), `TestPlayGolden`.

## 10. Goldens

Changed, each now hardn's own screen on the real engine where it was the mock on fixture plans: `internal/tools/uigallery/testdata/golden/play/dns.golden` (main menu → 3: the host's note, web-01's Debian 13 read, Add's errors, the real plan with hardn.yml's diff, the screen drawn again showing the three nameservers applied and listed, which the mock could not), `dns-networkmanager.golden`, `dns-empty.golden` and `dns-one.golden` (started as `play dns/<host>`, their note; the refusals as before). Added: `play/dns-ubuntu2604`, `dns-alpine324`, `dns-pve9`, `dns-configured`, `dns-remove`, `dns-presets`, `dns-dry-run`, `dns-failed-reload`, `dns-failed-save` (the screen per host and each item, refusal, failure and redraw; core set). Removed, with the retired mocks: `pkg/ui/screen/testdata/golden/sub-menu/03-dns.golden` and `03-dns-{alpine324,configured,empty,networkmanager,one,pve9,ubuntu2604}.golden` (8), and the 34 step files of `workflow-step/dns-*/` (10 workflows). No other golden changed: the engine scenarios', the status hosts', the components', the flow's, `pkg/output`'s, the control's, and every legacy and characterization golden are as they were.

## 11. Not in this unit; inputs to later units; open items

- **Unit (d)**: §7. **The parity table (b2)**, **P-01's DNS row** and **the compact main menu's height**: their units.
- **For the maintainer:**
  - **A long row is cut.** The layouts stop at 64 columns, and a row's value is one line, cut with the ellipsis: after Configure DNS on Ubuntu 26.04 the screen reads "192.0.2.54, 2001:db8::54, 192.0.2...." at 80 columns, eth0's own server cut off (`play/dns-ubuntu2604`; the finding above the question says it whole); at 60 columns the row stacks and shows it whole. A row per link, or a value that wraps, would be a component change.
  - **The empty list's words.** Built in decision 3's adopted words, "Please add nameservers first.", origin/main's second sentence alone, as the mock drew it; this unit's task quoted origin/main's two sentences, "No nameservers configured. Please add nameservers first.". Say if the first sentence should come back.
  - **A host that needs no change lists no finding** (`play/dns-configured`): the control's findings ride on its change ([flow-review.md](flow-review.md) §4), where the mock listed dhcpcd's.
  - **Add's and Remove's descriptions** understate the action (decision 1 (i)): kept, as adopted.
  - **A reload that exits non-zero** (systemd 252/255's restart leaving resolved failed) is still reported as the raw error chain ("…: exit status 1 (COMMAND_FAILED)"), not in words like D3's, as every control's failed command is ([gallery-engine.md](gallery-engine.md) §8.9): the control's or the engine's to word; the screen shows what the flow's report gives it.
- **Two reads per drawing**: `dns.Read` for the rows and the control's `Check` for its state, each reading the host; cheap, but a host changed between them could show a manager from one and Unknown rows from the other, which says Unknown, not a guess.

## 12. Size

About 945 lines of non-test Go added and 555 removed (`git diff --numstat next/s2-flow-review`): `pkg/screens/dns` 486 (about 150 of them comments), `pkg/ui/screen/dns.go` 157 for the mocks' 515, the gallery's `dnsscreen.go` 213, `play.go` 27 and `main.go`'s doc 12, the control's `HostRefusal` 23, `pkg/ui/screen`'s other files 25; about 1,285 lines of tests added and 570 removed (the mocks' realness tests, the gallery's `dns_test.go`, 551). Over the guideline of about 500 changed lines because the unit is a screen, its glue and its gallery play together, each needing the others to be tested end to end, and because retiring the mocks removes their code and tests in the same unit; net of the removals, about 390 lines of non-test Go. The split would have been the glue with its tests first, then the gallery and the mocks' retirement. Testdata: 55 golden files, 3,820 lines added and 2,306 removed (9 play goldens added, 4 changed, 42 screen goldens removed). `go.mod` and `go.sum` unchanged. `git status --short --ignored` before each commit listed only this unit's paths, none ignored.
