# `hardn check`: design

Status: **adopted 2026-11-13**, and built (§6). The maintainer adopted every recommended answer of §4, in these words: "Adopt all". So each decision's (a) stands, and §4 is kept as the record. Since 2026-11-14 there is one more fact, `logs.servers`, the syslog servers the host forwards to ([design-syslog-servers.md](design-syslog-servers.md) §2.7, §6). Three baselines were worked through against the code (2026-11-13); this page compares them.

## 1. The command

`sudo hardn check` reads the host, changes nothing, makes no network request, and exits non-zero when the host no longer matches a baseline. It is for cron and CI.

## 2. Three baselines

| | A: hardn.yml | B: a saved baseline | C: the status rows |
|---|---|---|---|
| Drift is | what Run All would change now | a difference from a state saved earlier | a status row not Configured |
| A port opened with `ufw allow` | missed | **caught** | missed |
| Password login turned back on | missed (no key for it) | **caught** | caught only if it leaves Configured |
| An admin or SSH key added | missed | **caught** | missed |
| A host hardened from the menu | nothing to check: the menu doesn't write Run All's switches | checked | checked |
| A host hardn never changed | passes | "no baseline" | fails, as a drifted one does |
| Stores something | no | one root-owned file | no |
| Work | small | medium | small |

A and C both miss the feature's own example, a port opened. A also has nothing to compare on a host set up from the menu, and passes silently when hardn.yml itself is weakened. C fails every host still on port 22 and can't tell "never hardened" from "drifted": it is a posture check, which the main menu and the Markdown report already give.

**Recommended: B.** It is the only one that answers "has this host changed since I last looked".

## 3. B in outline

- `sudo hardn check --accept` reads the host and saves the baseline: `/var/lib/hardn/baseline.json`, root's, 0600, written by rename, read back through the checks the journal's files already get (owner, mode, no symlink, strict parse).
- `sudo hardn check` reads the host the same way and compares. Each difference is one line: what it was, what it is.
- What is read, all through readers hardn already has:
  - **Users:** who can become root, accounts with uid 0, each admin's SSH keys (type and fingerprint), accounts with an empty password.
  - **SSH:** root login, password login, the port and listeners, allowed users, as sshd reads them (`sshd -T`).
  - **Firewall:** on or off, the incoming policy, at boot, and every rule (ufw's, or firewalld's zones and what each lets in).
  - **DNS:** the nameservers in use.
  - **Auto updates:** on or off, as its screen reads it.
- Not read: AppArmor, sysctl, packages, rules written straight into nftables, a cloud provider's firewall.
- Exit statuses are the existing ones: 0 no difference; 3 a difference (`changes-needed`); 1 an error, not root, or no baseline; 2 a usage error; 130 interrupted.
- It never calls the planner, takes no lock and opens no log, so "changes nothing" holds: `check` writes no file at all, and `--accept` writes the baseline alone.

Known false alarms, to be said in the docs: DHCP rewriting the nameservers; a deliberate change, until it is accepted; IPv6 switched on, which gives each ufw rule a twin.

## 4. Decisions for the maintainer

Adopted 2026-11-13, each as recommended: "Adopt all".

1. **The baseline.** (a) *Recommended:* B, a saved baseline. (b) A, hardn.yml: small, but misses an opened port and menu-hardened hosts. (c) C, the status rows: small, but a posture check under another name. Gates every decision below.
2. **When the baseline is saved.** (a) *Recommended:* only when you run `hardn check --accept`. After hardn applies a change, its result says "Run hardn check --accept to keep this as the baseline". Nothing in apply or rollback changes. (b) Also saved by hardn after each change it applies: no reminder to act on, but it silently accepts any hand change made before it, and it touches apply and rollback, the riskiest code. Gates the size of the build.
3. **No baseline yet.** `hardn check` on a host where `--accept` never ran. (a) *Recommended:* exit 1, "No baseline: run hardn check --accept", so a cron job can't pass with nothing checked. (b) Exit 0 with a notice.
4. **A change in the safer direction**, such as the firewall turned on, or root login from key-only to off. (a) *Recommended:* a difference like any other: the host isn't as you left it. (b) Not counted, which needs a rule for "safer" for every fact.
5. **A new listening port.** A program starts listening on a port the firewall already lets in. No rule changed, but the port is newly reachable. (a) *Recommended:* counted, for ports the firewall lets in from outside; loopback ports never. Needs `ss`; without it the baseline says so and ports aren't compared. (b) Not counted: rules only.
6. **How much of Users.** (a) *Recommended:* host-wide: every account that can become root, with its keys. A new admin made with `useradd` is the case that matters. (b) Only the users hardn made or changed.
7. **The audit log.** (a) *Recommended:* `--accept` writes one record (who, when); a plain `check` writes none, since a five-minute cron job would add 105,000 lines a year to a file nothing rotates. (b) Every check writes one. (c) Neither.
8. **`--output json`.** (a) *Recommended:* yes: CI needs it. It makes the list of differences a public format, as `system-details`'s is. (b) Text only for now.
9. **Root.** (a) *Recommended:* required, exit 1 without it: the baseline and half the readers are root's. (b) Allowed, with what can't be read shown as Unknown, which would read as a difference.

## 5. Not verified

- That every reader gives the same answer under cron as on a terminal: some readers take the session, which cron doesn't have. To be tested on a VM under real cron before anything else.
- What each release's default logger, time service and DHCP client do to the files compared.

## 6. Built

`next/drift-check`, 2026-11-13. Tested on the recording fake alone: nothing here has run on a VM, and §5 is still open.

**Where it is.**

| Part | Code |
|---|---|
| The facts, the reading, the comparison, the baseline's file | `pkg/engine/drift` (`Specs`, `Read`, `Compare`, `Save`, `Load`, `Kept`) |
| The document, the text report, the codes, the reminder | `pkg/output/check.go`, `codes.go`, `human.go` (`baselineBlock`) |
| The command | `cmd/hardn/check.go` |

**The baseline.** `/var/lib/hardn/baseline.json`: `format` (1), `hardnVersion`, `saved` (UTC), `user` (who ran `--accept`) and `facts`, the sixteen facts in a fixed order. It is written and read through the journal's own state-file functions (`revert.WriteStateFile`, `revert.ReadStateFile`): the directory and every one above it root's and writable by no one else; the file a regular file, not a symlink, root's, 0600, at most 4 MiB; written to a temporary file and renamed. Then it is decoded strictly: one JSON value, no unknown field, every fact once and in order, each in its form. A baseline that fails a check is not compared: `BASELINE_UNSAFE`, `BASELINE_INVALID`. One of another `format` is `BASELINE_OTHER_FORMAT`, told before anything else in it is read. None is `BASELINE_MISSING`. Each is exit status 1.

**The facts, and the reader each comes from.** No parser was added.

| Facts | Reader |
|---|---|
| `users.admins`, `users.uid0` | `accounts.Read`, and the User check's rules (`users.ReadElevation`, `GrantsOf`), over every account of `/etc/passwd` |
| `users.keys` | `authkeys.ReadAccounts` and `authkeys.Review`, the SSH keys screen's, over sshd -T's `AuthorizedKeysFile`; kept for admins and uid-0 accounts |
| `users.emptyPasswords` | `accounts.ReadShadow`, `PasswordOf`, as Account Review |
| `ssh.rootLogin`, `ssh.passwordLogin`, `ssh.ports`, `ssh.listeners`, `ssh.allowUsers` | the SSH checks' sshd -T (`ssh.ReadEffective`, newly exported); password login by SSH Login's rule (`sshchange.Reading.PasswordsOn`) |
| `firewall.state`, `firewall.incoming`, `firewall.boot`, `firewall.rules` | `ufw.Read` and `ufw.ReadBootUnit`; on Rocky, Alma and RHEL `firewalld.Read` and `firewalld.ReadRuntime`; on Proxmox VE `pve.ReadFirewall`, state and policy alone |
| `firewall.ports` | `firewall.ReadPorts`, the Firewall screen's Ports section |
| `dns.nameservers` | `dns.Read`, and the DNS check's "in use" (`InUse`, newly exported) |
| `updates.auto` | the Auto Updates check (`updates.Auto.Check`) |

The Firewall screen's own `firewall.Read` is not called: it plans item 1. The facts are composed from the readers under it. No reader takes the session.

**Not established.** A fact that cannot be read is saved and compared as not established, with why. Established on one side alone, it is a difference; on neither, none, whatever each side says of why. An item whose own state is not established says so in its text (`carol (not established)`), so it is never taken as absent.

**Listening ports** (decision 5). An item is a port that listens off loopback and that the firewall lets in, from anywhere or from some sources, or does not filter. A port on loopback alone, a blocked one, and a rule with nothing listening are never items. Without `ss` the fact is marked not compared, on either side, and the result says so (`CHECK_NOT_COMPARED`), with why: the host has no `ss`, or the baseline was saved without it. Where `ss` failed or printed lines hardn can't read, the fact is not established.

**The comparison** is pure: the same two readings give the same list, whatever order their facts or a set's items are in. Sets (admins, uid-0 accounts, keys, empty passwords, firewall rules, listening ports) are compared item by item; the rest are one value. Nameservers keep their order, which resolvers use. So do ufw's rules, since ufw applies the first that matches: the baseline keeps them in ufw's order, and the same rules in another order are one difference, "Firewall rule order".

**Writes.** `hardn check` writes nothing: no file, no lock, no log, no audit record, and it shows no revert notice, since acknowledging one writes. `--accept` writes the baseline and one audit record, whatever its outcome. Neither loads hardn.yml nor builds the engine.

**The reminder** (decision 2). Reports of an applied change come from two functions, `output.Human` (the flags) and `output.AfterFlow` (the screens and the typed-hostname flow); both end with `baselineBlock`. The run carries whether the host has a baseline (`Run.Baseline`, one `Lstat`, looked at only once a change applied). Nothing in apply or rollback changed.

**Limits.**

- `Match` blocks in sshd's files are not read: the facts are sshd -T's global values.
- firewalld's rich rules are not read: a zone with one has an item saying so, and a change inside it is not seen. firewalld stopped leaves the policy and rules not established.
- On Proxmox VE only pve-firewall's state and policy are compared; its rules and the listening ports are not established there.
- A ufw rule's comment is no part of its item.
- The baseline does not name its host: a file copied from another host is compared as this host's.
