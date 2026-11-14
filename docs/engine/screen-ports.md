# Listening ports (`screen.FirewallPorts`, `screen.Listeners`)

The real unit of the roadmap's "Listening ports", `next/listening-ports`: the two sections [gallery-ports.md](gallery-ports.md) mocked, as adopted there (2026-11-13, "Adopt all"). Read only: no change is offered, so there is no control step, no journal entry and no new code.
Status: built and tested on macOS in process (`make check`). No hardn binary ran. Nothing ran on Linux: §6 lists what to run there.

## 1. What it does

- **Firewall, section "Ports"**, after Rules (after firewalld's section on Rocky, Alma and RHEL): one row for each listening port and protocol, how it stands against the rules, and the program. A port the rules let in from anywhere with nothing listening is the one warning.
- **System Details, section "Listening Ports"**, after Host and IPs: one row for each listening socket, in port order: port and protocol, the address as `ss` writes it, the program. `hardn system-details` prints it too. As a user other than root the rows have no program and the last line is "Run hardn as root to see every process."
- **No `ss`:** the one row "Listening ports, ss not installed" on both. The Firewall screen's "Install iproute2" is unchanged.
- The Proxmox VE screen has no Ports section. `--output json` does not carry the list (§5).

| Row | Meaning |
| --- | --- |
| `Open` | Listening, and let in from anywhere |
| `Some sources` | Listening, and let in from some sources, or on some addresses or interfaces, only |
| `Blocked` | Listening, and not let in |
| `Local only` | Bound to loopback alone |
| `Open nothing listening` | Let in from anywhere, nothing listening (warning) |
| `Not filtered` | Listening, and no firewall is running |
| `Unknown` | Listening; how the firewall stands to it is not established |

## 2. Reading `ss`

`reload.ReadListening` (`pkg/host/reload/listen.go`), through `host.System`, runs `ss -Htlnp`, then `ss -Hulnp`:

| Flag | Why |
| --- | --- |
| `-t`, `-u` | TCP, UDP. One table a run, not `-tu` together: with one table `ss` prints no Netid column, so the line has the layout `ReadBound` already reads for sshd's sockets |
| `-l` | Listening sockets only: TCP in `LISTEN`, UDP `UNCONN` |
| `-n` | Numbers, no name lookups |
| `-p` | The owning program |
| `-H` | No header line |

Parsing: a line in another state is skipped; a line of the right state whose local address is not read is counted, and both sections then say "Some not read" (and the Firewall section names no port as having nothing listening). The address is `0.0.0.0`, `[v6]`, or `*` (IPv6 `::` that takes IPv4 too); `%<interface>` after it is kept. The program is the first name of `users:(("<name>",pid=...`, read up to `",pid=` so a name holding a space, quote, comma or bracket is read whole; no column, or a cut one, is no program. The name is the host's word: both screens pass it through `logview.Safe`.

## 3. Setting a port against the rules (`firewall.ReadPorts`)

One row for each port and protocol (decision 5). Where a port's sockets stand differently, as IPv4 let in and IPv6 not, the row is the widest: Open, else Unknown, else Some sources, else Blocked. Loopback sockets (and `%lo`) count for nothing; a port on loopback alone is Local only.

**ufw.** Not installed or inactive: Not filtered. State or status not read whole: Unknown. Active: `ufw.Evaluate`, the SSH section's verdict logic, for the socket's address and IP version, and IPv4 too for a `*` socket. Allowed and Limited are Open; Restricted and Excepted (a source, an address, an interface) are Some sources; the rest Blocked. The UDP ports ufw's own `before.rules` accept (68, 546, 5353, 1900) are Some sources. "Nothing listening" rows: each port or range of an allow or limit rule with no source, address or interface that `Evaluate` lets in, with no socket on it; a range is one row (`6000:6007/tcp`); a rule of both protocols is one row with no protocol. With ufw inactive there are none.

**firewalld.** Not installed or stopped: Not filtered. Running: `firewalld.ReadRuntime` reads `firewall-cmd --list-all-zones` and each service's ports (`--permanent --service=<s> --get-ports`) for the active zones and the default zone. A connection arrives in the zone its source is bound to, else its interface's, else the default zone. So: Open where every one of these zones lets the port in; Some sources where only some do; Blocked where none does. A default zone that lists no interface and no source takes the interfaces no zone names, and those are not read: where another zone has an interface and counting the default zone changes the word, the row is Unknown. A port forward (`forward-ports`) makes the port it takes Unknown, and the port it lands on where it stays on this host; a forward not read makes every row Unknown. So does a zone with a rich rule, or anything else not read: rich rules, policies and direct rules are not read.

**Containers.** A port held by `docker-proxy` is Unknown: Docker forwards it ahead of the host's rules.

## 4. Engineering calls

- **System Details is sorted by port**, TCP before UDP, a port's sockets in `ss`'s order, as the adopted mock drew it.
- **The Firewall screen's Read runs `ss -Htlnp` and `ss -Hulnp` once more** than before, for this section.
- **`details.ReadPorts`, `firewall.ReadPorts`, `fwscreen.PortRows`, `sdscreen.PortRows` are exported** so the gallery's `ports` mocks read a simulated host and draw through them. `play` has no ports screen of its own: its Firewall and System Details screens show the sections.
- Goldens changed: every Firewall and System Details screen golden gained its section, and the two parity tables the two `ss` reads. Nothing else.

## 5. Not decided by the page; built the safer way

- **Words for states the mocks did not draw:** "Unknown" (not established), "ss failed", "Some not read".
- **A port whose IP versions differ** shows the widest standing (§3).
- **JSON:** `hardn system-details --output json` has no listening ports; an `ss` failure adds no `DETAILS_UNREADABLE` finding.
- **Proxmox VE:** no section.

## 6. Not run; unverified

Nothing ran on Linux. *Unverified:* that `ss` omits the `users:` column for another user's sockets when not root (read from `ss.c`, not run); firewalld's `--list-all-zones` form with a source-bound zone (the form with rich rules and port forwards is read from upstream `command.py`, `print_zone_policy_info`, not run); that an interface no zone names is absent from the default zone's `interfaces:` line; that a source-bound zone whose target is `default` refuses a port it lacks (true without zone drifting; with `AllowZoneDrifting=yes`, firewalld before 1.0, the connection goes on to the interface's zone and such a port reads Some sources while open); that a firewalld service which includes other services lists their ports under `--get-ports` (if not, such a port reads Blocked); podman's and rootless containers' forwarding (only `docker-proxy` is recognised); firewalld policies and direct rules.

To run, as root, on Debian 13, Ubuntu, Alpine (with and without `iproute2`) and Rocky or Alma:

```
ss -Htlnp; ss -Hulnp                 # the forms the parser reads
sudo hardn                           # 3) Firewall: the Ports section; 10) System Details
hardn system-details                 # as a user: no programs, the last line
ufw allow 8080/tcp                   # a "nothing listening" row; then ufw delete allow 8080/tcp
firewall-cmd --list-all-zones        # Rocky: the form ReadRuntime reads
```
