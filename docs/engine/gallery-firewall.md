# Firewall in the gallery, before any build (slice 4, unit (a): `firewall`, `play firewall`)

> **Unit:** refactor plan slice 4, unit (a), `next/s4-gallery`: a proposal for the maintainer's review, nothing of it wired into hardn. No change under `pkg/` or `cmd/`.
> **Status:** built and tested on macOS, in process; no hardn binary ran. One existing golden changed: `play/user-management` recorded main menu 4's "no screen for Firewall" line, so it now takes 7, Run All, for that line. The ten decisions of §5 are adopted (the maintainer, 2026-11-01), with the Made here calls as stated and the words of §6.

## 1. What to run

```sh
go run ./internal/tools/uigallery firewall                 # every mock, no input; -list names them
go run ./internal/tools/uigallery play firewall            # the menu of hosts
go run ./internal/tools/uigallery play firewall/ubuntu2604 # one host; -sequence dry-run is plan only
go run ./internal/tools/uigallery play                     # main menu 4 leads to firewall/debian13
```

## 2. What origin/main does

Golden names are `pkg/characterization/testdata/golden/menu/firewall_<name>.golden`. The trunk differs from origin/main in these files by three things: "Press Enter" pauses (DEV-050), add and remove refused under plan only (slice 2b), ufw changes refused on Proxmox (V10).

- **Reached by.** Main menu 4 (read main_menu.go:748, :798). `hardn -w` runs the same sequence without the screen and without profiles (run ops/configure_secure_firewall/debian.golden; read cmd/hardn/main.go:417-418).
- **Shows.** UFW Installed, UFW Status, SSH Port, a rules line, configured profiles, a menu: 1 and 0 with ufw missing, else 1, 2, 3, 0 (run open_not_installed, open_inactive, open_profiles). SSH Port is `cfg.SshPort`, warned at 22, "good security" otherwise (run open_port_22_warns, open_port_2230_while_sshd_says_22).
- **Rules list.** Needs "deny (incoming)" and "allow (outgoing)" in the text of `ufw status`; the repository never asks for verbose (read ufw_firewall_repository.go:55, :93-94). Plain status with rules reads "No firewall rules configured" (run open_active_rules_never_listed). Verbose's text, fed by the mock, lists them (run open_active_rules_listed_if_status_is_verbose); so does a rule whose comment holds both phrases (run open_active_rule_listed_by_its_comment). That plain `ufw status` lacks the phrases is from ufw's source, not run on a host.
- **Failure part way.** Allow rule fails after disable and reset: ufw off, default deny set, and, on the fixture alone, no rules; the screen says only "failed to add rule allow 2230/tcp" (run item2_fails_part_way).
- **Proxmox.** Open screen says nothing about ufw there and offers Enable and Configure (run proxmox_open_says_nothing). 1 and 2 print "UFW is not used on Proxmox VE hosts: pve-firewall manages the packet filter, ..." and run no change (run proxmox_item1_refused, proxmox_item2_refused).
- **hardn.yml fields.** Written back on every save (run profiles_add). Read by the screen or `-w`: none. `ufwDefaultIncomingPolicy`, `ufwDefaultOutgoingPolicy`: read by nothing; deny and allow are constants (read firewall_manager.go:66-69, config.go:85-86). `ufwAllowedPorts`: Run All only (read run_all_menu.go:195); the screen and `-w` pass `[]int{}` (read firewall_menu.go:227, :263; main.go:418). `enableUfwSshPolicy`: Run All and `-r` only (read run_all_menu.go:194, main.go:273).

**Items.**
- 1, ufw missing: "isn't yet implemented"; dry run says it would install (run item1_not_installed_stub, item1_not_installed_dry_run). 1, inactive: no confirmation; default deny incoming, default allow outgoing, `ufw disable`, `ufw reset`, profiles, allow SSH port, `yes | ufw enable` (run item1_inactive_enable_runs_configure). Dry run prints one line (run item1_inactive_dry_run). 1, active: asks; y prints the stub, n cancels; ufw stays active (run item1_active_yes_stub, item1_active_no, item1_active_yes_dry_run).
- 2, "Configure firewall": the same sequence, no confirmation (run item2_allows_configured_port_not_sshds, item2_active_rules_are_reset). The goldens' fixture takes `ufw reset` as done; without `--force` the real one probably aborts on stdin=/dev/null and may be a no-op ([I], [side-effects.md](../refactor/phase0/side-effects.md) V9), so "rules reset" is the fixture's outcome, not a host's. Profiles: writes /etc/ufw/applications.d/hardn, one `ufw allow from any to any app <name>` each, before the SSH rule (run item2_profiles). Dry run (run firewall_configure_dry_run). ufw missing: hidden, still runs, "UFW firewall is not installed" (run item2_not_installed_hidden_still_runs); 3 likewise (run profiles_not_installed_hidden_still_runs).
- 3, sub-screen: add (four answers), remove, apply (run profiles_none, profiles_two, profiles_add, profiles_remove_yes, profiles_remove_no). Add and remove save ./hardn.yml and run no ufw command. Apply live is a stub; dry run names a file (run profiles_apply_stub, profiles_apply_dry_run). Add and remove under plan only: refused (run profiles_add_plan_only_refused, profiles_remove_plan_only_refused). Invalid key redraws; 0 and q return to the main menu (run invalid_key, return_0, quit_q).

### Defects
1. **Disable/reset sequence (slice):** `ufw disable` and `ufw reset` precede the allow rule, and a failure leaves ufw off; whether the reset takes effect is V9's correction above. V9 (run item2_active_rules_are_reset, item2_fails_part_way).
2. **Configured vs effective port (slice):** only `cfg.SshPort` is allowed; sshd's file says 22 and is not read. V9, V12, and V8's order (run item2_allows_configured_port_not_sshds; read side-effects.md:273-277).
3. **Enable without confirm (slice):** item 1 runs item 2's whole sequence on one key under the words "Start UFW" (run item1_inactive_enable_runs_configure).
4. **Stubs (slice):** Install, Disable and Apply print "isn't yet implemented"; `DisableFirewall` exists, uncalled (run item1_not_installed_stub, item1_active_yes_stub, profiles_apply_stub; read firewall_manager.go:96).
5. **Proxmox (slice):** no word on the open screen; the disable that is the way out is the stub. V10 (run proxmox_open_says_nothing, proxmox_active_disable_stub).
6. Rules never listed on plain `ufw status` output (run open_active_rules_never_listed). 7. A failing `ufw status` reads Inactive; the "Error getting firewall status" branch cannot show (run open_status_fails_reads_inactive; read ufw_firewall_repository.go:98). 8. Sub-screen messages are cleared at once, with no pause (run profiles_add, profiles_hidden_items_none_to_act_on, others so marked; read firewall_menu.go:373, :386, :398). 9. Each item 1 adds a "Press Enter to return to the main menu" pause on leaving (run item1_active_no; read firewall_menu.go:241, :304). 10. Dry-run lines claim what live does not do: install, write the profile file (run item1_not_installed_dry_run, profiles_apply_dry_run). 11. Port check is a slash; refusal comes after four answers and drops them (run profiles_add_port_check_is_a_slash, profiles_add_bad_port_after_all_answers). 12. Save goes to ./hardn.yml, not the file loaded (run profiles_add; read firewall_menu.go:497). Removing a profile leaves its ufw rule and file (read firewall_menu.go:541-551). 13. `-w` failure is logged and exit status stays 0: V25 (read main.go:418-419). The ops golden's "cmd/hardn/main.go:386-392" is stale; the call is at :417-418.

## 3. The mocks

Every host's rows come from real readers, never hardn.yml's `sshPort`: the Firewall row from `firewall.Incoming.Check` through the main menu's own mapping; ufw from `ufw.Read`, `ufw.Evaluate`; pve-firewall from `pve.ReadFirewall`; the listeners from `sshd.ReadEffective`, `reload.ReadBound`, on every host. Enable is the real `steps.PlanUFWEnable` on the real engine: lockout risk, so the hostname is typed and confirm-or-revert armed. Under plan only each item opens as live and stops at its plan, profiles too.

| Host | What it is | Firewall row | Item 1 does |
|---|---|---|---|
| `debian13`, `alpine324` | no ufw; Alpine's fake has `ss` | Not Configured, ufw not installed | Install (`apt-get`; `apk add ufw`) |
| `ubuntu2604` | Ubuntu 26.04 as shipped, socket-activated | Not Configured, inactive | Enable: `allow 22/tcp`, `--force enable` |
| `active22`, `hardened`, `profiles` | active on 22; sshd on 2230, ufw allowing 2230; active on 22, two profiles in hardn.yml | Configured, deny policy | Disable |
| `allow-policy` | active, default allow, no rule | Partial, allow policy | Set deny policy: `allow 22/tcp`, `default deny incoming` |
| `lockout` | sshd on 2230, ufw allowing 443 | Partial, ssh blocked | Let SSH through: `allow 2230/tcp` |
| `lockout-at-start` | stopped, ENABLED=yes, saved rules refuse SSH | Not Configured, enabled, not running | Refuses (`ErrEnabledMismatch`) |
| `allowlist` | SSH from 192.0.2.0/24 and 2001:db8::/32 | Partial, ssh allowlist | Disable |
| `blocklist` | inactive, a saved deny before the allow | Not Configured, inactive | Refuses (`ErrBlocks`) |
| `edit-2230` | Port 2230 not reloaded, sshd on 22, inactive | Not Configured, inactive | Refuses (decision 2) |
| `moved-2230` | active on 22, sshd -T says 22, 2230 bound | Unknown, ssh config not in effect | Refuses (decision 2) |
| `denied` | active, `deny 22/tcp` | Partial, ssh blocked | Refuses: nothing it can plan |
| `alpine-runlevel`, `unit-off` | active, in no runlevel; `ufw.service` disabled | Configured, deny policy | Start ufw at boot: `rc-update add ufw default`; `systemctl enable ufw.service` |
| `pve9`, `pve9-drop` | Proxmox VE 9: pve-firewall disabled; DROP, management IP set | Not Configured, disabled; Configured, drop policy | Return alone |

Item 2 is Application Profiles wherever ufw is installed; item 3, Disable, on any active ufw whose item 1 is not. The listing also draws the profiles' sub-screen with two and with none, `no-install` (decision 1's alternative), `refusals` (all of §6's), `pve-states` (stopped, sshd elsewhere, a blacklist, ufw left active), and `stale-listener`. `lockout`, plain and ASCII, 31 lines drawn, here its blank lines and the ufw and Rules sections left out:

```
# UFW Firewall Configuration ###################################
  [ ] Firewall             Partial ssh blocked
# SSH ----------------------------------------------------------
  [ ] [::]:2230            Refused
  [ ] 0.0.0.0:2230         Refused
  Select an option
   1) Let SSH through      Add its rules; other rules stay
                           [ ] Lockout risk
   2) Application Profiles Add, remove, apply
   3) Disable firewall     Stop ufw (not recommended)
   0) Return
  > Enter selection [0-3 or q]:
```

## 4. Heights

The same lines at 80x24 and 80x40, Unicode and ASCII; the prompt is last. Every menu screen fits 80x40; those with ufw installed overrun 80x24 by 5 to 10 lines, so the title and the Firewall row scroll off (decision 9). Enable's plan step fits neither: about 55 lines from its title to the hostname prompt (`play firewall-enable`), mostly the diff of "Install hardn's boot unit", the engine's for any lockout-risk plan (P-34), which pushes the review of Status, policy and At boot off both.

Fit 80x24: the profiles' sub-screen 16, Proxmox VE 17, no ufw 20. Over it: most hosts with ufw 29; `edit-2230`, `allow-policy`, `alpine-runlevel`, `denied`, `unit-off` 30; `lockout`, `moved-2230`, `allowlist` 31; `blocklist` 33; `profiles` 34.

## 5. Decisions for the maintainer

**Adopted (the maintainer, 2026-11-01):** "Adopt all ten." Every recommended answer below is adopted, and every Made here call as stated. What follows from them:

- Install is restored through the plan, apt or apk, never on Proxmox VE; unit (b) builds the package primitive, which slice 7 can share (1, [P-16](../refactor/proposals.md#p-16)).
- Enable refuses whenever a configured listener is not bound, with a session or from a console, so the real plan gains a read of `ss` (2).
- The profiles' sub-screen stays: Add, Remove and Apply through the flow (3).
- `hardn -w` on a terminal asks the typed hostname and arms the timer, and refuses off a terminal; `runControls` gains that question in unit (d), and slice 6's `-d` takes the same answer (4).
- No rule is saved while ufw stays inactive, and Apply refuses there; a saved blocklist refuses Enable; P-33 (2) stays open for slice 6 (5, [P-33](../refactor/proposals.md#p-33)).
- `ufw disable` is a plain y/N change that warns, on every active ufw (6).
- Proxmox VE gets the read-only screen, titled "Firewall Configuration", and main menu 4 reads "View pve-firewall" there. A node where ufw was left active gets a warning row read from `ufw.conf`, with no `ufw` call: the adoption allows that read under brief 2.5. P-20 stays out of this slice (7).
- `ufwDefaultIncomingPolicy` and `ufwDefaultOutgoingPolicy` warn on load, the file still accepted; `ufwAllowedPorts` waits for slice 8 (8).
- Full sections at every height; the title and the Firewall row scroll off at 24 rows (9).
- §6's words are decided (10), "No firewall rules configured" under "Rules" with them ([P-30](../refactor/proposals.md#p-30) (b)).
- The mocks of options not adopted, `no-install` and `stale-listener`'s last section, go when the real screen replaces the mocks.

The slice's defects (§2), and where each is answered:

| Defect | Answer |
|---|---|
| 1. Disable/reset sequence | Made here: origin/main's item 2, "Configure firewall", goes and Profiles moves from 3 to 2. No plan but Disable's runs `ufw disable`; none runs `ufw reset`. So Enable keeps saved rules that origin/main's sequence meant to clear |
| 2. Configured vs effective port | Rows from `sshd -T` and `ss`; decision 2 |
| 3. Enable without confirm; 4. Stubs | Through the flow, the hostname typed, confirm-or-revert armed; Install (decision 1), Disable (decision 6), Apply (decision 3) |
| 5. Proxmox | The read-only screen; ufw left active there is decision 7's second question |

Drawn: the screen on every host, each change through the canonical flow, decision 1's and 2's alternatives. **Not mocked:** 2(c); 3(b), 3(c); 6(b), 6(c); 7(b), 7(c) and 7's ufw row; main menu 4's "View pve-firewall" (7(a); that menu's 4 leads to `firewall/debian13`, whatever the host); 8(a)'s warning; 9(b); a failed install or stand-in apply, a rollback of a stand-in, the confirmation from a new session, `hardn -w`.

**1. A host with no ufw (P-16, the slice's open question). GATES the items, unit (b)'s scope, and a package primitive.** A stock Debian 13 and Alpine have no ufw, so the Firewall row says "ufw not installed" and hardn has no way to change that. On origin/main "Install UFW" is a stub that says it isn't implemented.
- (a) **Recommended:** restore the install through the plan: `apt-get install -y ufw` or `apk add ufw`, plain risk, never on Proxmox VE. Otherwise the row can never reach Configured from hardn. It needs a package primitive, which slice 7 may share: `pkg/host/alpine` has apk's, nothing has apt's. On Alpine it costs more than is drawn: `apk add ufw` needs the community repository [I], unchecked by the plan; a stock Alpine has no `ss` and no `iproute2` (alpine323-vm.log, 2026-11-02), so Enable refuses until `iproute2` is there; Install does not bring it, as adopted. The `alpine324` fake has `ss`, so `play firewall-install-alpine` draws the path without that refusal.
- (b) No install: item 1 goes and the ufw section says what to run (mock `no-install`). Smaller, but the first thing a stock host needs is left to the user.

**2. Enable while sshd's configuration is not in effect. GATES unit (b)'s Enable plan, and whether `pkg/` adds a read of `ss` to it.** The lockout this slice exists to prevent. The real `PlanUFWEnable` reads `sshd -T` alone: on `edit-2230`, from a console, it allows 2230 and starts ufw, refusing the 22 sshd accepts on (mock `stale-listener`). On `moved-2230` it is the other way round, and `ss -Htln` names no program, so the mock cannot tell 2230 is sshd's and draws no row for it.
- (a) **Recommended, as drawn:** refuse Enable whenever a configured listener is not bound, with a session or from a console; the user reloads sshd first. Covers both directions; costs one reload.
- (b) Plan over the row's candidates, the configured set and what is bound on 22 (`stale-listener`'s last section). Enable goes ahead on `edit-2230`, but `moved-2230`'s 2230 is no candidate, so from a console it is still refused. (c) Read `ss -Htlnp` to name sshd's sockets and plan over them: both directions, a new read for the row too.

**3. Application profiles. GATES unit (b)'s control beyond Enable. The slice's largest scope call.** origin/main's item 3 opens a sub-screen: Add and Remove save hardn.yml, Apply is a stub. `docs/configuration.md` documents the feature and its file, `/etc/ufw/applications.d/hardn`.
- (a) **Recommended:** keep the sub-screen, as drawn, now item 2: Add, Remove and Apply through the flow, so the documented feature works. Apply refuses on an inactive ufw (decision 5).
- (b) `hardn.yml`'s key alone: no sub-screen, Apply run with Enable; profiles are edited by hand, and Enable opens more than SSH. (c) Retire it: the documentation goes, the key warns; the smallest slice, and its users lose it.

**4. `hardn -w`, a lockout-risk change once migrated. GATES unit (d).** Today it runs origin/main's `ConfigureSecureFirewall` (`cmd/hardn/main.go:417`), V9's disable, and reset where that takes effect. The engine allows the plan from a flag once the session is established and hardn's path known (`cmd/hardn/engine.go`, `engineOptions`): nothing below `runControls` stops it, and only its `refuse` hook does.
- (a) **Recommended:** on a terminal ask the typed hostname and arm the timer; off a terminal refuse. A `-w` that can only refuse does nothing. `runControls` has no question today, so this is new. Slice 6's `-d` meets the same question: decide them alike.
- (b) Refuse always and name main menu 4, as `-e` does (`cmd/hardn/environment.go`, `refuseLockoutRisk`): no new path, and `-w` becomes a pointer. (c) Retire it: scripts that call it fail.

**5. P-33 (1) and (3), what they mean here.** [P-33](../refactor/proposals.md#p-33) asks which rules hardn adds on its own. (2), a moved port's source restriction, belongs to slice 6.
- (1), rules saved while ufw stays inactive. (a) saves them, as `PlanFor` does now, so a later `ufw enable` keeps SSH open. **Recommended (b)**, as `steps` already plans (`UFWKeep`, `NoSaveWhileInactive`): save none, report what a later start would block. The mock follows (b): Enable starts ufw, and Apply, which would otherwise save, refuses on an inactive ufw (mock `refusals`). Under (a) Apply saves there, and slice 6's plan changes. Gates that refusal and slice 6's plan.
- (3), a saved blocklist: **recommended (a)**, refuse Enable, as `blocklist` draws. (b), allowing it when the session's address is outside every excepted source, needs a new input and one client's word for everyone. Gates only the refusal's words.

**6. Disable. GATES that item alone.** It is a stub on origin/main, offered on every active ufw: it warns, asks, then says it isn't implemented.
- (a) **Recommended, as drawn:** implement `ufw disable` as a plain y/N change that warns first, so an active ufw always has a way off, a console user behind a refused SSH included: item 1 once ufw is active and set, else item 3 beside the repair. It cannot lock anyone out, so no hostname and no timer. Rules stay saved. It never resets (§3.17).
- (b) Only when active and set: one line shorter, and a broken ufw has no way off from hardn. (c) Drop the item: the user always runs `ufw disable`.

**7. Proxmox VE. GATES the screen there, and main menu 4's text.** Brief 2.5: report only. P-20, managing pve-firewall, stays out of this slice.
- (a) **Recommended:** the read-only screen drawn: pve-firewall's service, enabled, default incoming policy, how far SSH and 8006 are let through, what the reading found, why hardn does not change it, Return alone; its title "Firewall Configuration", as origin/main's says UFW; and main menu 4's "Configure UFW rules" there "View pve-firewall". No ufw call of any kind, which a test holds.
- (b) Keep origin/main's title and item text: both say UFW on a host that must not run it. (c) Leave item 4 out there: the row stays, with nowhere to see why.
- **And a node where origin/main left ufw active (V10):** this screen does not show it and offers no way out, though ufw filters there and defect 5's way out was a Disable (mock `pve-states`, last). **Recommended:** a warning row read from `ufw.conf`, no `ufw` call, saying to run `ufw disable`; hardn changes nothing, but brief 2.5 would have to allow that read. Otherwise it stays unseen, as `check-firewall.md` lists it.

**8. `hardn.yml`'s `ufwDefaultIncomingPolicy`, `ufwDefaultOutgoingPolicy`, `ufwAllowedPorts`. GATES a warning on load.** Grep shows the first two read by nothing. `ufwAllowedPorts` is read by Run All alone (`pkg/menu/run_all_menu.go:195`). `docs/configuration.md:222` says the policies are always deny and allow. Nothing on this screen reads any of them.
- (a) **Recommended:** warn as `logFile` does, the file still accepted, for the two policies; leave `ufwAllowedPorts` to Run All's slice. A setting that does nothing should say so.
- (b) Honour them: a host-wide policy from a file, a new way to refuse SSH. (c) Remove them: a file that sets them still loads, silently ignored.

**9. 80x24. GATES the sections of the real screen.** Every host with ufw takes 29 to 34 lines, so at 24 rows the title and the Firewall row, the screen's verdict, scroll off (§4).
- (a) **Recommended, as drawn:** full sections at every height, the menu and prompt always on screen, as you chose for the main menu ("a two-column menu is not a desired method", gallery-dryrun.md §4).
- (b) Rules behind an item: 25 lines on most hosts, so still over; it fits only if the ufw section shrinks too.

**10. The words**, §6: each proposed. Gates unit (b)'s strings.

**Made here**, say if you would rather otherwise:
- Outgoing policy left alone, not set to allow: `PlanUFWEnable` passes none, and origin/main's `ufw default allow outgoing` restates ufw's default.
- A service that does not start at boot is a step of Enable, a unit (b) item: `rc-update add ufw default` on Alpine, as `ufw enable` adds no runlevel; `systemctl enable ufw.service` on systemd. `ufw enable` does not touch `ufw.service`, which starts `ufw-init` and is `WantedBy=multi-user.target` (deb13-vm.log, ubuntu26-vm.log, 2026-11-02), so a disabled unit does not start it at boot; no VM has rebooted to show that.
- "Active and set" counts an allowlist and an active blocklist, every listener let through, so item 1 is Disable there. Where a rule or IPV6=no still refuses a listener, nothing can be planned and item 1 refuses, saying what to do.
- With ufw inactive the status prints no rule, so Rules reads its files and says "Saved"; each IP version's rule keeps its row, as ufw prints it. Nothing is marked good while ufw filters nothing: policy and saved allows read plain. Under plan only, Add and Remove stop at their plan, where origin/main refuses them (`docs/configuration.md:140` then changes).

## 6. Words

Decided with decision 10 (2026-11-01). Each is new unless marked origin/main's. Its "consider changing" and "good security" are gone: this screen reads what sshd does, not which port is better.

| Where | Words |
|---|---|
| Titles | "UFW Firewall Configuration" and "Manage UFW Application Profiles" (origin/main's); "Firewall Configuration" on Proxmox VE; "Install UFW", "Enable Firewall", "Set Deny Policy", "Let SSH Through", "Start at Boot", "Disable Firewall", "Add Profile", "Remove Profile", "Apply Profiles" |
| Sections | "ufw", "SSH", "Rules", "Application Profiles" (origin/main's), "pve-firewall" |
| ufw rows | "Installed" Yes/No, "Status" Active/Inactive, "Incoming policy" Deny/Reject/Allow, "At boot" Yes/No/"In no runlevel"/"Service disabled", "IPv6" "Off" |
| SSH rows | "Allowed", "Rate limited", "Some sources", "Blocked for some", "Refused", "No IPv6", "Not filtered", "Refused at start"; beside them "Not bound", "Not configured"; "Bound sockets" "ss not installed"/"ss failed"/"None on SSH's ports" |
| Rules, profiles | "Allow", "Limit", "Deny", "Reject", "Saved"; "No firewall rules configured" (origin/main's); "Not applied", "Applied"; "No application profiles configured" (origin/main's) |
| Proxmox VE | "Service" Running/Stopped, "Enabled", "SSH (22)", "Web UI (8006)", "Management network", "From anywhere", "sshd on 2230"; "Rules" "One may block", "Blacklist" "Has entries", "proxmox-firewall" "Not read"; "hardn reads pve-firewall and doesn't change it: a wrong rule can cut off the web UI and the cluster." |
| Item 1, by what its plan does | "Install UFW" (origin/main's) "Install the ufw package"; ufw not running: "Enable firewall" (origin/main's) "Let SSH through, then start ufw"; running: "Set deny policy" "Let SSH through, deny by default"; "Let SSH through" "Add its rules; other rules stay" or "Add its rules, start at boot"; "Start ufw at boot" "Add it to the boot services"; refused: "Repair firewall" "Refused here: select to see why"; "Disable firewall" (origin/main's) "Stop ufw (not recommended)" |
| Other items | "Application Profiles" "Add, remove, apply"; "Add profile", "Remove profile", "Apply profiles" |
| Questions, notices | "Install ufw?", "Enable the firewall?", "Set the deny policy?", "Let SSH through?", "Start ufw at boot?", "Disable the firewall?", "Add <name>?", "Remove <name>?", "Apply the profiles?"; "This stops the firewall and leaves every port open."; "This takes the profile out of hardn.yml. A rule already in ufw stays."; Add's "Give the profile a title.", "Give the profile a description.", "Give each port, 1 to 65535, with its protocol, such as 80/tcp." |
| Refusals | "hardn can't tell which ports SSH uses: ss isn't installed. Install iproute2, then try again."; "hardn can't tell which ports SSH uses: ss failed. Check that it runs, then try again."; "sshd isn't listening where its config says, so hardn can't tell which ports SSH uses. Reload sshd or restart ssh.socket, then try again."; "hardn can't read ufw's state. Check ufw status verbose, then try again."; "ufw is set to start at boot but isn't running. Check that its rules let SSH through, then run ufw enable."; "ufw is running but isn't set to start at boot. Run ufw enable."; "A saved ufw rule would block SSH on port 22, for some addresses or all. Remove it with ufw delete, then try again."; "ufw's IPv6 is off in /etc/default/ufw, so it would refuse SSH on port 22. Set IPV6=yes, then try again."; "ufw refuses SSH on [::]:22, and hardn doesn't change a rule it finds. Fix it with ufw allow or ufw delete, then try again."; "hardn can't tell if ufw starts at boot. Check its service, then try again."; "hardn can't tell if this host runs Proxmox VE, where it leaves the firewall alone."; "ufw isn't running, so hardn saves no rule for it. Enable the firewall, then apply." |

## 7. For unit (b)

> **Since `next/s4-control`:** the stand-ins below are gone. The gallery runs on `firewall.Read` and `firewall.Incoming` ([control-firewall.md](control-firewall.md)); `no-install` and `stale-listener`'s last section went with them. The screen's words, rows and layout stay in the gallery until unit (c).

| Stand-in, labelled in `firewall.go` | What it must become |
|---|---|
| `fwControl`, wrapping `firewall.Incoming`; `fwSockets`, a third copy of `sockets.go`'s reading | `Incoming.Plan`, replacing `engine.ErrNotMigrated`; that reading exported, shared with the row |
| `fwEnable`'s refusals: ss missing or failing, configuration not in effect | refusals in the real plan (decision 2) |
| `fwUnit`, `fwCommand` (install, disable, `rc-update`, `systemctl enable`), `fwApply` (the profile file, `ufw allow from any to any app <name>`), Add and Remove (`steps.PlanSave` on `ufwAppProfiles`) | real reads and plans with their undos, the control's own; the package primitive (decision 1) |

Findings:
- **`PlanUFWEnable` can let a stale listener be refused**, and reads no `ss`: decision 2. Pinned as it stands by `TestFirewallStaleListener`; without `ss` the row says Unknown while the plan goes ahead.
- **Nothing to plan under a row that says refused.** `deny 22/tcp`, an allowlist of one family, or IPV6=no gives "ssh blocked", and `ufw.PlanFor` plans nothing: "Nothing needs changing" on the real flow. The mock refuses first, naming the cause (`TestFirewallNothingRefused`).
- **The row says "deny policy" on a host that may lose ufw at reboot** (`alpine-runlevel`, `unit-off`): `Incoming.Check` reads neither (check-firewall.md §6). `detect.ServiceRunlevels` and `SystemctlIsEnabled` are there; the At boot row reads them.
- **Profiles:** origin/main joins a profile's ports by a comma (`ufw_firewall_repository.go:255`), which ufw-framework(8)'s `|` form does not read [I], read, not run. Remove leaves a rule behind, whose profile leaves the file at the next Apply; whether ufw minds is not established. Enable's own undo is `ufw disable`, in the journal (`ufw.PlanFor`, §3.17's exception), never among a plan's commands.

## 8. Not run

No hardn binary, no VM, no ufw, apt, apk or pve-firewall. ufw answers from a model (`firewallhost.go`, sources there), not captured from a host; what ufw does with a multi-port or an empty profile, and its "Rule deleted", are from memory of ufw, marked there. Whether a stock Debian 13 lacks ufw, and how its and Alpine's package leave it, are [unverified]. `make check` passed at the unit's end.
