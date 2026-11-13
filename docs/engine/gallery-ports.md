# Listening ports mock

*Since the real unit (2026-11-13): the sections are built ([screen-ports.md](screen-ports.md)). The Firewall screen and System Details read `ss` and draw them, the mocks' two sections are read from a simulated host by the same readers and drawn through the screens' mappings, and `play`'s Firewall and System Details have them. Where this page says nothing is wired, no host is read, or a row is the mock's own word, this note is current. Two rows differ from §2: on the ufw mock `5353/udp` is "Some sources" (ufw's own rules let mDNS in), and on the firewalld mock 9090 is let in by a second zone, `internal`.*

**Adopted (the maintainer, 2026-11-13): "Adopt all".** Every recommended answer of §3 is adopted, with the Made here calls as stated and §4's words.

The mock unit of the roadmap's "Listening ports", `next/listening-ports`. hardn shows which ports the firewall lets in, not which ones something listens on. This mock draws both together on the Firewall screen, and the listening ports alone on System Details.
Status: built and tested on macOS in process (`go test ./pkg/ui/screen/ ./internal/tools/uigallery/`). No hardn binary ran. Nothing ran on Linux. Nothing is wired into hardn: no host is read, no `ss` runs, and `cmd/hardn`, `pkg/screens`, the engine, hardn.yml and `play` are untouched. Every screen hardn draws today is byte-identical: the new sections show only where they are given rows, and only the mocks give any.

## 1. What to run

```
go run ./internal/tools/uigallery ports [-ascii] [mock | all]   # the listing, no input
go run ./internal/tools/uigallery ports -list                    # the mocks' names
```

The view-models are `pkg/ui/screen/ports.go` (`screen.FirewallPorts`, `screen.Listeners`), whose rows go into the new `Ports` field of `screen.Firewall`, `screen.Firewalld` and `screen.SystemDetails`. The mocks are `internal/tools/uigallery/ports.go`, the goldens `internal/tools/uigallery/testdata/golden/ports/`.

## 2. The mocks

One host, `web-01`: sshd on 22 and nginx on 80 and 443, on every address; avahi-daemon on 5353/udp; PostgreSQL on 5432, loopback alone; prometheus on 9090; node_exporter on 9100. The firewall lets in 22, 80, 443 and 8080, which nothing listens on. Every row is the mock's own word.

| Mock | What it shows |
| --- | --- |
| `ufw` | The Firewall screen, ufw active and denying by default: the Ports section after Rules, with the four states |
| `ufw-inactive` | ufw installed and stopped: every port but the loopback one "Not filtered"; a saved rule's unused port has no row |
| `ufw-no-ss` | No `ss`: the section is the one row "Listening ports, ss not installed"; item 1 is today's "Install iproute2" |
| `firewalld` | The Firewall screen on a firewalld host: the same section after firewalld's, and 9090 let in from one network, "Some sources" |
| `firewalld-no-ss` | The firewalld host with no `ss` |
| `details` | System Details as root: the Listening Ports section after Host and IPs, a row per socket |
| `details-not-root` | `hardn system-details` as a user: no process names, and a last line that says why |
| `details-no-ss` | System Details with no `ss`: the one row |

The Firewall screen's row is the port and protocol, how it stands, and the process:

| Row | Meaning |
| --- | --- |
| `22/tcp  Open sshd` | Listening, and let in from anywhere |
| `9090/tcp  Some sources prometheus` | Listening, and let in from some sources only |
| `9100/tcp  Blocked node_exporter` | Listening, and not let in |
| `5432/tcp  Local only postgres` | Bound to loopback alone: not reachable from outside, whatever the rules |
| `8080/tcp  Open nothing listening` | Let in, with nothing listening; the one warning |
| `22/tcp  Not filtered sshd` | Listening, and the firewall is not running |

System Details' row is the port and protocol, the address as `ss` writes it, and the process: `22/tcp  [::] sshd`.

## 3. Decisions for the maintainer

Drawn: every recommendation below.

1. **Where the comparison shows on the Firewall screen.** (a) *Recommended, drawn:* a "Ports" section on the screen itself, after Rules (after firewalld's section on a firewalld host), as the SSH section already sets sshd's sockets against the rules. Nothing to open, no item renumbered. Cost: the screen grows by a row per port, and a host that runs many services pushes the menu down. (b) A sub-screen behind a new item ("Listening Ports"): the main screen stays as it is, but the item needs a key, which moves "Disable firewall" from 4 to 5 or puts the new item after it.
2. **The words for how a port stands.** (a) *Recommended, drawn:* "Open", "Some sources", "Blocked", "Local only", "Open nothing listening", and "Not filtered" with the firewall stopped. "Some sources" and "Not filtered" are the SSH section's own. (b) The SSH section's words throughout ("Allowed", "Refused"): one vocabulary, but there they are green and red, and here they are neither.
3. **Which rows are warnings.** (a) *Recommended, drawn:* only a port let in with nothing listening, an opening nothing uses. Every other row is plain: hardn cannot tell whether a port is meant to be open, so an open port is not green and a blocked one is not red. (b) Also warn on every port open to anywhere but SSH's. (c) No warning: a list only.
4. **Whether SSH's port is in the Ports section.** The SSH section above already says whether sshd's sockets get through. (a) *Recommended, drawn:* yes, so the section is every port. (b) Left out, so nothing is said twice.
5. **One row per port, or per socket, on the Firewall screen.** `ss` lists IPv4 and IPv6 apart. (a) *Recommended, drawn:* one row per port and protocol, which halves the section. A port the rules treat differently per family then needs a word that says so, which the real unit must settle. (b) A row per socket, as the SSH section and System Details have.
6. **Where the list sits on System Details.** (a) *Recommended, drawn:* "Listening Ports" after "Host and IPs", the network facts together. (b) Last, after "Last Login", nearest the menu on a short terminal.
7. **System Details as a user.** `hardn system-details` runs without root, and `ss` then names only that user's processes (ss's behaviour, not run here). (a) *Recommended, drawn:* list the ports with no process, and end with "Run hardn as root to see every process." (b) "Not known" on every row.
8. **No `ss`.** (a) *Recommended, drawn:* the one row "Listening ports, ss not installed" on both screens, and no new item: the Firewall screen already offers "Install iproute2" where Enable needs it. (b) An item that installs it for this section alone, on System Details too.
9. **The process on the Firewall screen.** (a) *Recommended, drawn:* shown after the state, and dropped where the row has no room. (b) Left to System Details, so the Firewall row is port and state only.

## 4. Words

Proposed, decided with §3: "Ports"; "Listening Ports"; "Open"; "Some sources"; "Blocked"; "Local only"; "Not filtered"; "nothing listening"; "Listening ports"; "ss not installed"; "Nothing is listening"; "Run hardn as root to see every process."

## 5. Made here

- The sections are fields of the existing views (`Firewall.Ports`, `Firewalld.Ports`, `SystemDetails.Ports`), each a list of rows as the other sections are, left out when empty. `pkg/ui/screen`'s tests hold that a view with no ports has today's sections.
- Rows are drawn in the order given; the mocks give port order.
- The process is the row's `Extra`: it follows the value where there is room and is dropped where there is not, so the state is never cut for it. At 60 columns the list stacks, as every section does there.
- With `ss` and no listening port, the section is the muted line "Nothing is listening". No mock draws it; `pkg/ui/screen`'s tests hold it.
- With the firewall stopped, a rule's port with nothing listening has no row: the rule is saved, not in effect.
- The Firewall mocks' other sections (ufw, SSH, Rules, firewalld) and System Details' other sections are written by hand in today's words, not read from a simulated host.
- The Proxmox VE screen has no Ports section.
- A listing only: `play` has no ports screen, and the main menu's Firewall and System Details lead where they did.

## 6. For the real unit

*Each is answered in [screen-ports.md](screen-ports.md) §3 and §5.*

- Reading: `ss -tulnp`, parsed in `pkg/host`, bounded, every string made safe to show. hardn already runs `ss` for sshd's sockets; whether that reader can serve this one was not checked here.
- Setting a socket against ufw's rules: the verdict logic the SSH section uses (`ufw.Verdict`, `Reading.Coverage`) covers SSH's ports. Whether it holds for any port, for UDP, for port ranges, for application profiles, for rules on one interface and for trusted interfaces was not checked here.
- firewalld: a zone lets in services and ports, and a service names its ports in its own file; rich rules and sources limit them. What hardn reads of firewalld today is the default and active zones. The comparison needs the zone's services resolved to ports.
- A port let in by a rule with nothing listening needs the rule's ports enumerated; a range (`6000:6007/tcp`) is one row or many.
- A socket bound to one address (`192.0.2.20:9090`), to an interface (`%eth0`) or to a wildcard that takes IPv4 too (`*:9100`) each needs its rule matched by family and address.
- Containers: a port published by a container runtime may be forwarded before the host's rules see it, so "Blocked" could be false there. Not examined.
- Proxmox VE: whether its read-only screen gets the section.
- JSON: whether `hardn system-details`'s JSON form carries the list.
- Nothing ran on Linux.
