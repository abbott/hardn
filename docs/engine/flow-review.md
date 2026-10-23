# A plan's findings before its confirmation (`pkg/ui/flow`, `pkg/output`)

> **Unit:** refactor plan slice 2 (DNS, the pilot of the full flow), unit (c), its first part, `next/s2-flow-review`, on `next/s2-config-edit`. **Slice 2's decision 5, adopted by the maintainer on 2026-10-22:** the findings before the confirmation, "in the review, as the maintainer's warning lines of 2026-10-21 (`- ` in `status.warn`, DEV-060), each in a few words" ([gallery-dns.md](gallery-dns.md) §4). It builds the slot [confirm-cleanup.md](confirm-cleanup.md) §2.12 and §7 and [gallery-engine.md](gallery-engine.md) §7 left for P-41's best-effort warning, for every flow. New deviation: DEV-063; DEV-048 and DEV-060 amended. No V-number.
> **Status:** built and tested on macOS, in process: the flow on fixture engines at every cell of `uitest.Full()`, the flow adapter on the real engine over the recording fake (`hostfake`), the gallery's scenarios on its simulated hosts. No real terminal and no host were used; nothing here runs a command. Nothing is wired into `cmd/hardn` or the live main menu. **No word is new** (§5).

## 1. Why

Until this unit `pkg/ui/flow`'s Plan step had no place for a finding, so `output.AfterFlow` printed a plan's findings after the result, as a Warning callout with each code and source: "dhcpcd may rewrite /etc/resolv.conf on a lease (DNS_DHCP_CLIENT, /usr/sbin/dhcpcd)" under "Operation Successful", and under "Operation Cancelled" after a decline. A warning read after the change is applied cannot change the user's answer. The DNS mocks drew the decided place, the review's last lines, from data of their own.

## 2. Guarantees

### The flow (`pkg/ui/flow`)

1. **`flow.Plan.Findings`**, new: what the plan found that stops nothing, each a `flow.Finding{Text, Code, Severity}`, in the order found. `Severity` is `SeverityWarn` (the zero value, so a finding is never drawn as less than a warning by default) or `SeverityNote`; any other value draws as a warning.
2. **Listed in the review, before the plan and its question.** Each finding is one more row of the review's section, a `KeyValue` bullet item: a warning's bullet and words in `KeyValue.ValueWarn` (`status.warn`, DEV-060), a note's words muted (`ValueNone`, DEV-061), wrapped under itself at col 6. A review with no rows, or no review, has no section; the findings are then a `BulletList` of their own in its place, after the notice when there is one, set apart by `SectionGap` (DEV-063). With no findings nothing changes: every flow without findings draws as before (`flow/*` goldens unchanged).
3. **The Engine plans before the review is drawn**, so that the review, its findings with it, is one block in one write (§9.10's rule). What the user sees is in the same order as before: review, plan, question. A context done while planning now ends the flow with nothing written, where the review was written before it.
4. **Every path that reaches the plan lists them**: applied, declined (`n`, `0`), a dry run (the plan is its output), a failure at a step, a typed confirmation, and a plan with no changes, before "Nothing needs changing." (P-41's warning is on every plan, one with nothing to change included). **A plan that cannot be made lists none** (`Engine.Plan`'s error is shown as the `ErrorReport`, as before).
5. **No code on the line** (decision 1, §3).

### The adapter and the report after a flow (`pkg/output`)

6. **`FlowEngine.Plan` hands the flow the plan's findings** (`reviewFindings`): the document's (`planFindings`, factored out of `Build`, its order unchanged: the plan's own, P-41's, then each change's in the plan's order), each once (a finding two changes carry is one line), each `SeverityWarn`, as the human report words every finding, its line the finding's `Detail`, its code kept in `Finding.Code`.
7. **`AfterFlow` does not repeat them.** `Run.Reviewed`, new, says the flow listed the plan's findings; `FlowEngine.Run` sets it when `Plan` handed over a plan (no error, no refusal) and the flow reached a result, or ended at the confirmation (`q`, the end of the input, Ctrl-C), which comes after them. `AfterFlow` then leaves out every finding of the plan, and reports only what the flow did not list: after an apply, a decline, a dry run or a failure, nothing (the engine's `Result` carries no finding of its own; the failure's next step, D3's "Run resolvectl dns to see the servers in use.", is the flow's `ErrorReport`'s, as before). The countdown is unchanged.
8. **A refusal at the plan**: the flow shows the `ErrorReport`, lists no finding, and `AfterFlow` reports the plan's findings after it, as warnings with their codes and sources, as before (decision 4, §3). **A flow that ended before listing them** (a context done as it planned, a write that failed, a typed confirmation the flow cannot ask) is taken not to have listed them, so `AfterFlow` reports them: a finding is shown twice at worst, never missed.
9. **The human report says a finding once** (`findingsBlock`): a finding two changes carry, with the same detail, code and source, is one line, in `Human` and `AfterFlow` alike.
10. **The JSON document is unchanged**: `findings[]` holds every finding as before, one per change that carries it, with its control and change; `Reviewed` changes nothing in it (`TestAfterFlow` compares the two documents). `Human`, for a run no flow drew, is unchanged too: it has no confirmation to list them before, so its findings stay after the plan or the steps, with their codes (decision 6).

### The DNS mocks (`pkg/ui/screen`)

11. **The mocks' findings are their fixture plans'** (`flow.Plan.Findings`, with the control's codes), no longer rows of their review, so the flow's own slot draws them: every mock golden is byte for byte as before (`workflow-step/dns-*`, `play/dns*`), which is the check that the real flow draws the decided look. `TestDNSPlansAreReal` holds each sequence's findings, text and code, to what `output.FlowEngine` hands the flow on the gallery's resolver model, and the real ones to the plan's changes' findings.

## 3. Decisions of this unit

None is the maintainer's; each is for review.

1. **The code is not on the line.** README §9.15: a line says what happens in the fewest words, in the user's terms, "and leaves the exact lines to what shows them in full (the plan's diff, the journal, `--output json`)"; decision 5 says "each in a few words", and the decided mock shows none. The code stays in the JSON document (`findings[].code`) and the audit log, and on the human report of a run no flow drew; `flow.Finding.Code` carries it for a caller. *Alternative:* the code as a faint aside after the words, as a failed step's code is in its row (`StepProgress`'s detail, `Step.Detail` in `text.faint`), which needs a bullet item with an aside (a `KeyValue` change) and puts hardn's identifier on the line before the question.
2. **Plan, then the review.** The review is drawn once the plan is made, so that it is one block, its findings its last rows, as the mock draws it; drawing the review first and the findings under it after planning would split one block into two writes. A slow plan now shows nothing until it is made, where it showed the review first.
3. **With no review, no heading.** The engine's scenarios have no review, so their findings are a list of their own above the plan, with no section header: a header would be words no decision has, and the lines read as warnings by their colour, as DEV-060 records, and by their words without it.
4. **A refusal at the plan keeps the findings after its report**, with their codes: the flow has no plan to show, `Engine.Plan`'s error being its result, and a finding listed above an "Operation Failed" that refuses the plan would read as the refusal's cause. *Alternative:* the flow lists a refused plan's findings above the `ErrorReport` too (an `Engine` contract change).
5. **Every finding is a warning** (`SeverityWarn`), as `pkg/output` words every finding today; `SeverityNote` exists so that a lesser finding draws as the muted dash line decided on 2026-10-21 ("`text.muted` otherwise", DEV-061), and no finding is one yet. Which findings are less than a warning is the finding's owner's (its control's or the engine's), when one is.
6. **`Human` unchanged.** It renders a run no flow drew (a dry run or an apply without a terminal, once §3.11 wires them), where there is no confirmation to list the findings before; its plan is a log as much as a review, so its findings stay with their codes and sources after it.
7. **Each finding once, by its words and code** (`flow.Finding`'s fields): two findings with one detail and two codes would be two lines; none does today.

## 4. Differences from the mocks, recorded

- **A host that needs no change** (`dns-configure-dns-configured/unchanged`): the mock lists the host's dhcpcd warning above "Nothing needs changing.", as decision 5 reads the findings, from `dns.State.Findings()`; the real flow lists none there, because the DNS control's findings ride on its change (`steps.DNS`) and a plan with no change has none (the engine has no place for a control's findings without a change). The mock is left as decided, and `TestDNSPlansAreReal` pins the difference: for that sequence the real plan carries none and the mock the host's. Input to the control's owner (§8).
- **The engine's scenarios** have no review (no section "DNS Configuration"), so their findings stand alone above the plan (decision 3), where the DNS screen's will close its review.

## 5. Words

No word is new: each line is the finding's own detail, the DNS control's (`steps.DNSFinding`, gallery-dns.md §5, proposed there) or the engine's (P-41's). One is longer than "a few words" and is proposed shorter, **not built** (the engine's detail is the JSON document's too, and P-41's unit's):

| Where | Words now | Proposed, not built |
|---|---|---|
| P-41's best-effort warning, a review line since this unit (three lines at 80 columns) | "Ubuntu 22.04 is a best-effort release: its vendor still supports it and hardn changes it, but hardn is not tested on it" | **proposed:** "Ubuntu 22.04 is best effort: hardn is not tested on it" |

The flow's tests use a note of their own, "eth0 keeps its search domain example.net" (`TestFindings`, `flow/findings`): fixture words, no finding of hardn's.

## 6. Goldens

Added: `pkg/ui/flow/testdata/golden/flow/findings.golden` (the DNS mocks' Use Cloudflare DNS as a flow of its own: the review, a warning and a note under its rows, the plan, y, applied; core set). Changed, each because its findings moved from after the result to above the plan, one line each without code, and are not repeated after it:
- `internal/tools/uigallery/testdata/golden/play/engine/best-effort-ubuntu2204.golden`: P-41's warning above the boot unit's change, no longer a Warning callout under the dry-run footer.
- `…/play/engine/dns-cloudflare-debian13.golden`, `dns-dry-run-debian13.golden`, `dns-failed-save-debian13.golden`: dhcpcd's warning above "Set the nameservers" (in the dry run above the plan and its footer; in the failed save no longer under the `ErrorReport`).
- `…/play/engine/dns-configure-ubuntu2604.golden`, `dns-configure-ubuntu2404.golden`, `dns-failed-reload-ubuntu2604.golden`: eth0's server (`DNS_PER_LINK`) above the plan (in the failed reload no longer under the report and its next step).
- `…/play/engine/dns-configure-pve9.golden`: the node's warning (`DNS_PROXMOX_NODE`) above the plan.
- `pkg/output/testdata/golden/human/after-flow.golden`: the countdown alone; the port plan's `SSHD_OVERRIDDEN` warning, which the flow listed (`Reviewed`), is gone.

Unchanged: every other golden, the DNS mocks' (§2.11), the components', `flow/*` without findings, `pkg/output`'s `Human` and JSON ones, and the engine scenarios without findings (`dns-udhcpc-alpine324` answered y has none; the SSH, ufw and sudoers scenarios' Warning callouts are the countdown's).

## 7. Tests

- `pkg/ui/flow`: `TestFindings`, at every cell of `uitest.Full()` (every profile, both backgrounds, both glyph sets, every width): each finding once, in order after the review's rows and before the plan, the question and the result, never after; the warning drawn as `KeyValue` draws a warning bullet item and the note as a muted one; no code shown. `TestFindingsPaths`, plain: applied, declined, a dry run, a failure at the step, nothing to change, no review, a review with a notice and no rows, a plan that cannot be made (none listed). `TestGolden`'s `findings`. `TestReview` takes no findings.
- `pkg/output`: `TestFlowEngineFindings`, the real engine over the fake, a control whose two changes carry one finding and the first another: the flow handed each once, in order, a warning with its code; listed before the question and not after the flow when applied, declined, a dry run, failed, `q` or the end of the input at the confirmation; a plan refused by a second control: none listed, both after the report, once each, with their codes; a flow done as it planned (`context.Canceled`): reported after; the document's three findings whichever. `TestAfterFlow`: the golden with `Reviewed`; the finding after the countdown without; the documents equal.
- The gallery: `TestPlayEngineDNS` and `TestPlayEngineDNSKeepsUDHCPC` (each finding's line before the plan; no DNS finding's code and no line twice in any transcript), `TestPlayEngine`'s `best-effort-ubuntu2204` (P-41's line before the plan; once; its code nowhere), `TestDNSPlansAreReal` (§2.11), `TestPlayEngineGolden`.
- Probed with `go test -overlay` (not committed): the flow listing no findings, and listing a warning as a plain row, each fails `TestFindings`.

## 8. Not in this unit; inputs to later units

- **The rest of unit (c)**: the DNS screen on the real control, Alpine's question on the form, Add's inline errors, the presets, the screen drawn again after a change.
- **For the control's owner** (the DNS control, or the engine): a control's findings on a plan with no change (§4), if the maintainer wants decision 5's reading on the real flow.
- **For P-41's words** (§5): the shorter line, if the maintainer wants it, changes the engine's detail, the JSON document's and `confirm-cleanup.md`'s and `TestBestEffort*`'s.
- **A finding less than a warning**: none yet; its owner sets `SeverityNote` through the adapter, which today words every finding a warning (a severity on `engine.Finding` would be an engine change).
- **Written, not run (gate 5):** nothing here touches a host; what the DNS findings say of each host is control-dns.md §4's.

## 9. Size

About 220 lines of non-test Go added and 85 removed (`pkg/ui/flow` 105, `pkg/output` 90, the DNS mocks and the gallery's comment 30), about 335 lines of tests added and 40 removed, against the guideline of about 500 changed lines: within it for the code, over it with the tests (about 680 in all), which run the findings through every cell of the full matrix and every path of the flow, the real engine and the gallery's scenarios. Testdata: one golden added (72 lines), nine changed (55 lines added, 114 removed). This page, and about 40 lines across the design-system README, the deviations log, output.md, the reference, gallery-engine.md, gallery-dns.md, confirm-cleanup.md and control-dns.md.
