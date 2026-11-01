# Firewall in the gallery, before any build (slice 4, unit (a): `firewall`, `play firewall`)

> **Unit:** refactor plan slice 4, unit (a), `next/s4-gallery`: a proposal for the maintainer's review, nothing of it wired into hardn. No change under `pkg/` or `cmd/`.
> **Status:** built and tested on macOS, in process; no hardn binary ran. One existing golden changed: `play/user-management` recorded main menu 4's "no screen for Firewall" line, so it now takes 7, Run All, for that line.

## 1. What to run

```sh
go run ./internal/tools/uigallery firewall                 # every mock, no input; -list names them
go run ./internal/tools/uigallery play firewall            # the menu of hosts
go run ./internal/tools/uigallery play firewall/ubuntu2604 # one host; -sequence dry-run is plan only
go run ./internal/tools/uigallery play                     # main menu 4 leads to firewall/debian13
```

## 2. What origin/main does

## What origin/main does

Golden names are `pkg/characterization/testdata/golden/menu/firewall_<name>.golden`. The trunk differs from origin/main in these files by three things: "Press Enter" pauses (DEV-050), add and remove refused under plan only (slice 2b), ufw changes refused on Proxmox (V10).

**Reached by.** Main menu 4 (read main_menu.go:748, :798). `hardn -w` runs the same sequence without the screen and without profiles (run ops/configure_secure_firewall/debian.golden; read cmd/hardn/main.go:417-418).

**Shows.** UFW Installed, UFW Status, SSH Port, a rules line, configured profiles, a menu: 1 and 0 with ufw missing, else 1, 2, 3, 0 (run open_not_installed, open_inactive, open_profiles). SSH Port is `cfg.SshPort`, warned at 22, "good security" otherwise (run open_port_22_warns, open_port_2230_while_sshd_says_22).

**Rules list.** Needs "deny (incoming)" and "allow (outgoing)" in the text of `ufw status`; the repository never asks for verbose (read ufw_firewall_repository.go:55, :93-94). Plain status with rules reads "No firewall rules configured" (run open_active_rules_never_listed). Verbose's text, fed by the mock, lists them (run open_active_rules_listed_if_status_is_verbose); so does a rule whose comment holds both phrases (run open_active_rule_listed_by_its_comment). That plain `ufw status` lacks the phrases is from ufw's source, not run on a host.

**Items.**
- 1, ufw missing: "isn't yet implemented"; dry run says it would install (run item1_not_installed_stub, item1_not_installed_dry_run).
- 1, inactive: no confirmation; default deny incoming, default allow outgoing, `ufw disable`, `ufw reset`, profiles, allow SSH port, `yes | ufw enable` (run item1_inactive_enable_runs_configure). Dry run prints one line (run item1_inactive_dry_run).
- 1, active: asks; y prints the stub, n cancels; ufw stays active (run item1_active_yes_stub, item1_active_no, item1_active_yes_dry_run).
- 2: the same sequence, no confirmation (run item2_allows_configured_port_not_sshds, item2_active_rules_are_reset). Profiles: writes /etc/ufw/applications.d/hardn, one `ufw allow from any to any app <name>` each, before the SSH rule (run item2_profiles). Dry run (run firewall_configure_dry_run).
- 2, ufw missing: hidden, still runs, "UFW firewall is not installed" (run item2_not_installed_hidden_still_runs). 3 likewise (run profiles_not_installed_hidden_still_runs).
- 3, sub-screen: add (four answers), remove, apply (run profiles_none, profiles_two, profiles_add, profiles_remove_yes, profiles_remove_no). Add and remove save ./hardn.yml and run no ufw command. Apply live is a stub; dry run names a file (run profiles_apply_stub, profiles_apply_dry_run). Add and remove under plan only: refused (run profiles_add_plan_only_refused, profiles_remove_plan_only_refused).
- Invalid key redraws; 0 and q return to the main menu (run invalid_key, return_0, quit_q).

**Failure part way.** Allow rule fails after disable and reset: ufw off, no rules, default deny set; the screen says only "failed to add rule allow 2230/tcp" (run item2_fails_part_way, on a fixture that takes `ufw reset` as done).

**Proxmox.** Open screen says nothing about ufw there and offers Enable and Configure (run proxmox_open_says_nothing). 1 and 2 print "UFW is not used on Proxmox VE hosts: pve-firewall manages the packet filter, ..." and run no change (run proxmox_item1_refused, proxmox_item2_refused).

**hardn.yml fields.** Written back on every save (run profiles_add). Read by the screen or `-w`: none.
- `ufwDefaultIncomingPolicy`, `ufwDefaultOutgoingPolicy`: read by nothing; deny and allow are constants (read firewall_manager.go:66-69, config.go:85-86).
- `ufwAllowedPorts`: Run All only (read run_all_menu.go:195); the screen and `-w` pass `[]int{}` (read firewall_menu.go:227, :263; main.go:418).
- `enableUfwSshPolicy`: Run All and `-r` only (read run_all_menu.go:194, main.go:273).

### Defects
1. **Disable/reset sequence (slice):** `ufw disable` and `ufw reset` precede the allow rule, and a failure leaves ufw off. V9 (run item2_active_rules_are_reset, item2_fails_part_way).
2. **Configured vs effective port (slice):** only `cfg.SshPort` is allowed; sshd's file says 22 and is not read. V9, V12, and V8's order (run item2_allows_configured_port_not_sshds; read side-effects.md:273-277).
3. **Enable without confirm (slice):** item 1 runs item 2's whole sequence on one key under the words "Start UFW" (run item1_inactive_enable_runs_configure).
4. **Stubs (slice):** Install, Disable and Apply print "isn't yet implemented"; `DisableFirewall` exists, uncalled (run item1_not_installed_stub, item1_active_yes_stub, profiles_apply_stub; read firewall_manager.go:96).
5. **Proxmox (slice):** no word on the open screen; the disable that is the way out is the stub. V10 (run proxmox_open_says_nothing, proxmox_active_disable_stub).
6. Rules never listed on plain `ufw status` output (run open_active_rules_never_listed).
7. A failing `ufw status` reads Inactive; the "Error getting firewall status" branch cannot show (run open_status_fails_reads_inactive; read ufw_firewall_repository.go:98).
8. Sub-screen messages are cleared at once, with no pause (run profiles_add, profiles_hidden_items_none_to_act_on, others so marked; read firewall_menu.go:373, :386, :398).
9. Each item 1 adds a "Press Enter to return to the main menu" pause on leaving (run item1_active_no; read firewall_menu.go:241, :304).
10. Dry-run lines claim what live does not do: install, write the profile file (run item1_not_installed_dry_run, profiles_apply_dry_run).
11. Port check is a slash; refusal comes after four answers and drops them (run profiles_add_port_check_is_a_slash, profiles_add_bad_port_after_all_answers).
12. Save goes to ./hardn.yml, not the file loaded (run profiles_add; read firewall_menu.go:497). Removing a profile leaves its ufw rule and file (read firewall_menu.go:541-551).
13. `-w` failure is logged and exit status stays 0: V25 (read main.go:418-419). The ops golden's "cmd/hardn/main.go:386-392" is stale; the call is at :417-418.

## 3. The mocks

Every host's rows come from real readers, never hardn.yml's `sshPort`: the Firewall row from `firewall.Incoming.Check` through the main menu's own mapping; ufw from `ufw.Read`, `ufw.Evaluate`; pve-firewall from `pve.ReadFirewall`; the listeners from `sshd.ReadEffective`, `reload.ReadBound`. Enable is the real `steps.PlanUFWEnable` on the real engine: lockout risk, so the hostname is typed and confirm-or-revert armed. Under plan only each item opens as live and stops at its plan, profiles too.

| Host | What it is | Firewall row | Item 1 does |
|---|---|---|---|
| `debian13` | Debian 13, no ufw | Not Configured, ufw not installed | Install |
| `ubuntu2604` | Ubuntu 26.04 as shipped, socket-activated | Not Configured, inactive | Enable: `allow 22/tcp`, `--force enable` |
| `active22` | Debian 13, active on 22 | Configured, deny policy | Disable |
| `hardened` | sshd on 2230, ufw allowing 2230 | Configured, deny policy | Disable |
| `allow-policy` | active, default allow, no rule | Partial, allow policy | Enable: `allow 22/tcp`, `default deny incoming` |
| `lockout` | sshd on 2230, ufw allowing 443 | Partial, ssh blocked | Enable: `allow 2230/tcp` |
| `lockout-at-start` | stopped, ENABLED=yes, saved rules refuse SSH | Not Configured, enabled, not running | Refuses (`ErrEnabledMismatch`) |
| `allowlist` | SSH from 192.0.2.0/24 and 2001:db8::/32 | Partial, ssh allowlist | Disable |
| `blocklist` | inactive, a saved deny before the allow | Not Configured, inactive | Refuses (`ErrBlocks`) |
| `edit-2230` | Port 2230 not reloaded, sshd on 22 | Not Configured, inactive | Enable: see §7's first finding |
| `alpine324` | Alpine 3.24, no ufw | Not Configured, ufw not installed | Install (`apk add ufw`) |
| `alpine-runlevel` | active, in no runlevel | Configured, deny policy | Enable: `rc-update add ufw default` |
| `pve9` | Proxmox VE 9, pve-firewall disabled | Not Configured, disabled | Return alone |
| `pve9-drop` | Proxmox VE 9, DROP, management IP set | Configured, drop policy | Return alone |
| `profiles` | active on 22, two profiles in hardn.yml | Configured, deny policy | Disable |

The listing also draws the profiles' sub-screen with two and with none, `no-install` (decision 1's alternative), `refusals` (no ss, ufw's state not read, ENABLED and the chains disagreeing, a saved blocklist, a saved deny, Proxmox VE not established), and `stale-listener`. `lockout`, plain and ASCII, its blank lines left out:

```
# UFW Firewall Configuration ###################################
  [ ] Firewall             Partial ssh blocked
# ufw ----------------------------------------------------------
  [+] Installed            Yes
  [+] Status               Active
  [+] Incoming policy      Deny
  [+] At boot              Yes
# SSH ----------------------------------------------------------
  [ ] [::]:2230            Refused
  [ ] 0.0.0.0:2230         Refused
# Rules --------------------------------------------------------
  [+] 443/tcp              Allow
  [+] 443/tcp (v6)         Allow
  Select an option
   1) Enable firewall      Let SSH through, deny the rest
                           [ ] Lockout risk
   2) Application Profiles Add, remove, apply
   0) Return
  > Enter selection [0-2 or q]:
```

## 4. Heights

The same lines at 80x24 and 80x40, Unicode and ASCII; the prompt is last. Every screen fits 80x40. **Those with ufw installed overrun 80x24** by 5 to 10 lines, so the title and the Firewall row scroll off.

| Screen | Lines | At 80x24 |
|---|---|---|
| Proxmox VE, the profiles' sub-screen 16; no ufw 20 | 16, 20 | fits |
| Most hosts with ufw 29; `lockout` 30; `allowlist`, `edit-2230` 31; `blocklist` 33; `profiles` 34 | 29 to 34 | over by 5 to 10 |

## 5. Decisions for the maintainer

Drawn: the screen on every host, each change through the canonical flow. Not mocked: a failed install or stand-in apply, a rollback of a stand-in, the confirmation from a new session, `hardn -w`.

**1. A host with no ufw (P-16, the slice's open question). GATES the items, unit (b)'s scope, and a package primitive.** A stock Debian 13 and Alpine have no ufw, so the Firewall row says "ufw not installed" and hardn has no way to change that. On origin/main "Install UFW" is a stub that says it isn't implemented.
- (a) **Recommended:** restore the install through the plan: `apt-get install -y ufw` or `apk add ufw`, plain risk, never on Proxmox VE. Otherwise the row can never reach Configured from hardn. It needs a package primitive, which slice 7 may share: `pkg/host/alpine` has apk's, nothing has apt's.
- (b) No install: item 1 goes and the ufw section says what to run (mock `no-install`). Smaller, but the first thing a stock host needs is left to the user.

**2. Disable.** It is a stub on origin/main: it warns, asks, then says it isn't implemented.
- (a) **Recommended:** implement `ufw disable` as a plain y/N change that warns first, so the screen goes both ways as Backup and Environment do. It cannot lock anyone out, so no hostname and no timer. Rules stay saved. It never resets (§3.17).
- (b) Drop the item: an active firewall has no way off from hardn, and the user runs `ufw disable`. Gates that item alone.

**3. Application profiles. The slice's largest scope call.** origin/main's item 3 opens a sub-screen: Add and Remove save hardn.yml, Apply is a stub. `docs/configuration.md` documents the feature and its file, `/etc/ufw/applications.d/hardn`. Gates unit (b)'s control beyond Enable.
- (a) **Recommended:** keep the sub-screen, as drawn: Add, Remove and Apply through the flow, so the documented feature works.
- (b) `hardn.yml`'s key alone: no sub-screen, Apply run with Enable; profiles are edited by hand, and Enable opens more than SSH. (c) Retire it: the documentation goes, the key warns; the smallest slice, and its users lose it.

**4. `hardn -w`, a lockout-risk change once migrated.** Today it runs origin/main's `ConfigureSecureFirewall` (`cmd/hardn/main.go:417`), V9's disable and reset. The engine allows the plan from a flag once the session is established and hardn's path known (`cmd/hardn/engine.go`, `engineOptions`): nothing below `runControls` stops it, and only its `refuse` hook does. Gates unit (d).
- (a) **Recommended:** on a terminal ask the typed hostname and arm the timer; off a terminal refuse. A `-w` that can only refuse does nothing. `runControls` has no question today, so this is new. Slice 6's `-d` meets the same question: decide them alike.
- (b) Refuse always and name main menu 4, as `-e` does (`cmd/hardn/environment.go`, `refuseLockoutRisk`): no new path, and `-w` becomes a pointer. (c) Retire it: scripts that call it fail.

**5. P-33 (1) and (3), what they mean here.** [P-33](../refactor/proposals.md#p-33) asks which rules hardn adds on its own. (2), a moved port's source restriction, belongs to slice 6.
- (1), rules saved while ufw stays inactive: this screen never does; Enable starts ufw. **Recommended (b)**, as `steps` already plans (`UFWKeep`, `NoSaveWhileInactive`): save none, report what a later start would block. (a) would change slice 6's plan, not this screen.
- (3), a saved blocklist: **recommended (a)**, refuse Enable, as `blocklist` draws. (b), allowing it when the session's address is outside every excepted source, needs a new input and one client's word for everyone. Gates only the refusal's words.

**6. `hardn.yml`'s `ufwDefaultIncomingPolicy`, `ufwDefaultOutgoingPolicy`, `ufwAllowedPorts`.** Grep shows the first two read by nothing. `ufwAllowedPorts` is read by Run All alone (`pkg/menu/run_all_menu.go:195`). `docs/configuration.md:222` says the policies are always deny and allow. Nothing on this screen reads any of them.
- (a) **Recommended:** warn as `logFile` does, the file still accepted, for the two policies; leave `ufwAllowedPorts` to Run All's slice. A setting that does nothing should say so.
- (b) Honour them: a host-wide policy from a file, a new way to refuse SSH. (c) Remove them: a file that sets them still loads, silently ignored.

**7. Proxmox VE.** Brief 2.5: report only. P-20, managing pve-firewall, stays out of this slice.
- (a) **Recommended:** the read-only screen drawn: enabled, default incoming policy, how far SSH and 8006 are let through, why hardn does not change it, Return alone; its title "Firewall Configuration", as origin/main's says UFW; and main menu 4's "Configure UFW rules" there "View pve-firewall". No ufw call of any kind, which a test holds.
- (b) Keep origin/main's title and item text: both say UFW on a host that must not run it. (c) Leave item 4 out there: the row stays, with nowhere to see why.

**8. The words**, section 6: each proposed. Gates unit (b)'s strings.

**Made here**, say if you would rather otherwise:
- Outgoing policy left alone, not set to allow: `PlanUFWEnable` passes none, and origin/main's `ufw default allow outgoing` restates ufw's default.
- `rc-update add ufw default` is a step of Enable on Alpine, a unit (b) item, as `ufw enable` adds no runlevel. An active Alpine in none draws Enable, with this step alone, plain risk. "Active and set" counts an allowlist and an active blocklist: Enable would plan nothing there.
- With ufw inactive the status prints no rule, so Rules reads its files and says "Saved"; each IP version's rule keeps its row, as ufw prints it.
- Under plan only, Add and Remove stop at their plan, where origin/main refuses them (`docs/configuration.md:140` then changes).

## 6. Words

Proposed, unless marked origin/main's. Its "consider changing" and "good security" are gone: this screen reads what sshd does, not which port is better.

| Where | Words |
|---|---|
| Titles | "UFW Firewall Configuration" and "Manage UFW Application Profiles" (origin/main's); "Firewall Configuration" on Proxmox VE; "Install UFW", "Enable Firewall", "Disable Firewall", "Add Profile", "Remove Profile", "Apply Profiles" |
| Sections | "ufw", "SSH", "Rules", "Application Profiles" (origin/main's), "pve-firewall" |
| ufw rows | "Installed" Yes/No, "Status" Active/Inactive, "Incoming policy" Deny/Reject/Allow, "At boot" Yes/No/"In no runlevel" |
| SSH rows | "Allowed", "Rate limited", "Some sources", "Blocked for some", "Refused", "No IPv6", "Not filtered", "Refused at start"; beside them "Not bound", "Not configured"; "Bound sockets" "ss not installed" |
| Rules, profiles | "Allow", "Limit", "Deny", "Reject", "Saved"; "No firewall rules configured" (origin/main's); "Not applied", "Applied"; "No application profiles configured" (origin/main's) |
| Proxmox VE | "Enabled", "SSH (22)", "Web UI (8006)", "Management network", "From anywhere"; "hardn reads pve-firewall and doesn't change it: a wrong rule can cut off the web UI and the cluster." |
| Items | "Install UFW" (origin/main's) "Install the ufw package"; "Enable firewall" (origin/main's) "Let SSH through, deny the rest"; "Disable firewall" (origin/main's) "Stop ufw (not recommended)"; "Application Profiles" "Add, remove, apply"; "Add profile", "Remove profile", "Apply profiles" |
| Questions, notices | "Install ufw?", "Enable the firewall?", "Disable the firewall?", "Add <name>?", "Remove <name>?", "Apply the profiles?"; "This stops the firewall and leaves every port open."; "This takes the profile out of hardn.yml. A rule already in ufw stays." |
| Refusals | "hardn can't tell which ports SSH uses: ss isn't installed. Install iproute2, then try again."; "hardn can't read ufw's state. Check ufw status verbose, then try again."; "ufw is set to start at boot but isn't running. Check that its rules let SSH through, then run ufw enable."; "ufw is running but isn't set to start at boot. Run ufw enable."; "A saved ufw rule would block SSH on port 22, for some addresses or all. Remove it with ufw delete, then try again."; "hardn can't tell if this host runs Proxmox VE, where it leaves the firewall alone." |

## 7. For unit (b)

| Stand-in, labelled in `firewall.go` | What it must become |
|---|---|
| `fwControl`, wrapping `firewall.Incoming` | `Incoming.Plan`, replacing `engine.ErrNotMigrated` |
| `fwSockets`, a third copy of `sockets.go`'s reading | that reading exported, shared with the row |
| `fwEnable`'s refusal with no ss | a refusal in the real plan (first finding) |
| `fwCommand` (install, disable, `rc-update`), `fwApply` (the profile file, `ufw allow from any to any app <name>`), Add and Remove (`steps.PlanSave` on `ufwAppProfiles`) | real plans with their undos, the control's own; the package primitive (decision 1) |

Findings:
- **`PlanUFWEnable` can let a stale listener be refused.** It reads `sshd -T` alone (`st.before.Listeners`). On `edit-2230`, Port 2230 not reloaded and sshd accepting on 22, it plans `ufw allow 2230/tcp` and `ufw --force enable`. 22 is let through only as the applying session's port (`Keep`). From a console, or with no session established, **22 is refused once ufw starts**, the brief's own lockout, while the row's reading has 22 among what must pass (mock `stale-listener`, `TestFirewallStaleListener`). It should plan over the row's candidates: the configured set and what is bound.
- **`PlanUFWEnable` reads no `ss`.** Without it the row says Unknown, "ss not installed", while the plan goes ahead. The refusal here is the gallery's. Should it stay, a stock Alpine, said to ship no `ss` ([unverified], reload.md §3), needs `iproute2` before Enable, which Install may want to bring.
- **An explicit deny on SSH plans nothing.** Under an active ufw, `deny 22/tcp` gives "ssh blocked" and item 1 Enable, and `ufw.PlanFor` plans nothing: "Nothing needs changing" under a row that says otherwise (`TestFirewallDeniedPlansNothing`). The screen needs words for it.
- **The row says "deny policy" on an Alpine that loses ufw at reboot** (`alpine-runlevel`): `Incoming.Check` reads no runlevel (check-firewall.md §6). `detect.ServiceRunlevels` is there to read it. On systemd whether `ufw.service` is enabled is still unread.
- **Profiles:** origin/main joins a profile's ports by a comma (`ufw_firewall_repository.go:255`), which ufw-framework(8)'s `|` form does not read [I], read, not run. Remove leaves a rule behind, whose profile leaves the file at the next Apply; whether ufw minds is not established.
- Enable's own undo is `ufw disable`, in the journal (`ufw.PlanFor`, §3.17's exception), never among a plan's commands. `apk add ufw` needs the community repository enabled [I]; the plan does not check it.

## 8. Not run

No hardn binary, no VM, no ufw, apt, apk or pve-firewall. ufw answers from a model (`firewallhost.go`, sources there), not captured from a host. Whether a stock Debian 13 lacks ufw, and how its and Alpine's package leave it, are [unverified]. `make check` was left to the unit's end; `go test ./internal/tools/uigallery/` passed.
