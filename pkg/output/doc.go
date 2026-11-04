// Package output is hardn's output contract (refactor plan P3,
// next/p3-output; brief Phase 3: the JSON contract, the exit codes, the
// audit log): what a run of the plan/apply engine reports, to a script and
// to a person.
//
//   - Document and its parts are the wire types of the JSON document's nine
//     top-level fields (schemaVersion, hardnVersion, host, mode, result,
//     changes, status, findings, errors), version SchemaVersion, shared with
//     the audit log's Record. Build maps a Run (engine.Report, engine.Plan,
//     engine.Result, detect.Facts) to one; the engine's types are not the
//     wire format (docs/engine/engine.md §7).
//   - The code registry (codes.go, one file): a stable SCREAMING_SNAKE code
//     for every error, finding and boot outcome hardn reports. CodeOf
//     matches an error by errors.Is and errors.As, the more specific entry
//     first.
//   - ExitCode is the run's exit status, 0, 1, 2, 3 or 130, one function
//     over its results and errors.
//   - The renderers: WriteJSON writes exactly one document; Human renders
//     the same run through pkg/ui's components only; Emit routes them, the
//     JSON alone on stdout, nothing for a person beside it. FlowEngine is
//     pkg/ui/flow's Engine over the engine, which hands the flow the
//     plan's findings to list before its confirmation, and AfterFlow what
//     the flow does not draw (a change's confirm countdown, the findings
//     it did not list).
//   - The audit log: Append writes one Record per run, a JSON line, to
//     AuditPath through host.System's AppendFile.
//   - Confirmation is the non-interactive policy: never prompt without a
//     terminal; apply without asking only on an explicit yes. -g does not
//     consult it: it applies without asking, as the maintainer decided
//     (2026-10-23), as the legacy flags do.
//
// cmd/hardn renders hardn confirm's clean-up and -g (slice 2, unit (d):
// Human, ExitCode) through it, the DNS screen's changes through FlowEngine,
// and hardn system-details --output json (slice 1, unit (d): Build,
// WriteJSON); nothing else: the rest of refactor plan §3.11 (--output json
// on any other command) is not approved. Since next/p3-startup it appends
// each engine run, a flag's or a screen's change, to the audit log
// (Append, NewRecord). docs/reference/output.md is the public contract;
// docs/engine/output.md says what this package guarantees.
//
// It reaches the host only through host.System (depguard rule
// output-through-host) and renders only through pkg/ui.
package output
