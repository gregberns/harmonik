# Operator NFR — Change Design (C7)

Scope: operator visibility into the `workflow_mode` setting and into review-loop cycle progression; the `needs-attention` queue as operator-drained-only; explicit non-control surface (mode is not runtime-tunable); extension to the config inventory obligation.

## 1. Current state

- `operator-nfr.md §4.1 ON-001..ON-004` (lines ~137–159) defines the exit-code taxonomy and three companion obligations (`ON-002` exit-code taxonomy, `ON-003` startup failure-mode catalog, `ON-004` config inventory). `ON-004` (line ~155) enumerates the minimum knobs the inventory MUST cover: timer-flush cadence, budget warning threshold, drain timeout, RTO thresholds, queue-empty re-query cadence, Cat 0 retry cadence, per-Cat reconciliation budgets. **No mention of `workflow_mode`** today.
- `§4.3 ON-007..ON-013c` (lines ~184–253) defines operator-control semantics. `ON-009` (line ~197) — only `stop --immediate` aborts in-flight runs; `ON-011` (line ~209) — the operator-control state machine (`running`, `pausing`, `paused`, `resuming`, `stopped`, `upgrading`).
- `§4.9 ON-034..ON-035` (lines ~430+) defines the observability envelope — every subsystem emits typed events; structured logs are mandated.
- No `needs-attention` queue concept exists in the spec today.

## 2. Target state

### (a) Extend `ON-004` config inventory to include `workflow_mode`

Amend `ON-004` (or add `ON-004a`). The config inventory MUST list the `workflow_mode` knob with its **four-tier resolution-precedence layers** named explicitly:

1. Per-task `workflow:<mode>` label on the bead (per `beads-integration.md §4.3 BI-009a`).
2. Per-project policy (reserved tier).
3. Daemon default per `process-lifecycle.md §4.1 PL-004a`.
4. Built-in fallback `single`.

Default value: `single` (built-in). Allowed enumeration: `{single, review-loop, dot}`. Change-takes-effect semantics: per-task at claim time; daemon default on next daemon start. The inventory entry MUST also state that `workflow_mode` is **NOT** runtime-tunable per `ON-XXX` below.

### (b) Declare `workflow_mode` as a non-control surface

Add a new requirement, provisionally `ON-013d — Workflow mode is not an operator-control surface`. The daemon's `workflow_mode` is observable via `harmonik status` (the daemon's default + the per-run resolved value on any in-flight run) but is NOT mutable via any operator command. There is no `harmonik set-mode` and no `pause-then-set-mode` workflow. Operators who wish to change the default restart the daemon with a different config; operators who wish to change a per-task value edit the bead's `workflow:` label (via `br update`) **before** the bead is claimed. Once claimed, the resolved mode is sealed into the Run record per `execution-model.md §4.3 EM-012` and is immutable for the run's lifetime.

This pre-empts a foreseeable misreading: that mode is a runtime tunable. The locked decision (per `02-components.md §C7` constraints and the kerf's locked-decisions list) is the opposite.

### (c) Observability of review-loop cycles via existing JSONL path

Amend `§4.9` (or add `ON-035a`). Operator visibility into review-loop cycles MUST be supplied by the existing JSONL event-consumption path (`harmonik status`, `harmonik logs`, `jq`). The six new event types per `event-model.md §8.1.a*` provide the cycle's observability surface. No new command surface (no `harmonik review-status`). The operator's diagnostic recipe for a stuck cycle is: `jq` for `run_id` against `events.jsonl`, filter to `reviewer_verdict` + `iteration_cap_hit` + `no_progress_detected` + `review_loop_cycle_complete`.

### (d) `needs-attention` queue is operator-drained-only

Add a new requirement, provisionally `ON-009a — Needs-attention queue drain discipline`. Beads closed under any of the review-loop termination reasons that constitute non-success (`iteration_cap_hit`, `BLOCK` verdict, `no_progress_detected`) MUST be marked with a `needs-attention` label (encoding TBD by spec-draft; see Open Decisions) and MUST NOT be automatically re-dispatched. There is NO auto-retry. The daemon's ready-work query (per `beads-integration.md §4.5 BI-013`) MUST treat `needs-attention`-labeled beads as out-of-scope for automatic claim. Operators drain the queue manually: triage, then either re-open by removing the label (which restores claimability on next ready-work scan) or close as wontfix. This rule is normative; phantom auto-retry logic in implementations is a structural violation.

### (e) Exit-code taxonomy for review-loop termination paths

Amend `ON-002`. The exit-code taxonomy (§8 of operator-nfr) does NOT need a new top-level category for `iteration-cap-exceeded` / `blocked-verdict` / `no-progress` — these are run-level terminations, not daemon-level exits. The operator's observable signal is the bead's `needs-attention` label + the corresponding `review_loop_cycle_complete` event payload's `completion_reason` field (per event-model design `§8.1.a6`). State this explicitly (or implicitly via non-action) so spec-draft does not over-engineer the exit-code surface.

### (f) Pause / drain interaction with in-flight review-loop cycles

Amend `ON-008` (or add a clarifying sentence). An operator `pause` issued while a review-loop cycle is mid-iteration MUST be honored at the next durable checkpoint per existing pause semantics. The pause boundary for a review-loop run is at the end of any iteration (after `reviewer_verdict` emission, before the next implementer launch) — this is the run's natural between-checkpoint boundary, consistent with `ON-008`'s between-task invariant. `stop --immediate` aborts mid-iteration per `ON-009`; the run is left in the standard canceled-and-reconciled state. ON-008's between-task invariant is amended to include intra-run iteration boundaries (between `reviewer_verdict` emission and next `implementer_resumed`) as legitimate pause checkpoints when the run is in `review-loop` mode.

## 3. Rationale

- Satisfies `02-components.md §C7` requirements: extend `ON-004` config inventory; operator visibility via JSONL only (no new command); mode is non-tunable at runtime; `needs-attention` queue is operator-drained.
- Locked decision: `needs-attention` queue is operator-drained-only, no auto-retry. `ON-009a` is the load-bearing statement.
- Locked decision: cap=3 hardcoded, not operator-tunable. `ON-013d` and the `ON-004` inventory entry reinforce.
- The observation that no new exit-code category is needed prevents spec-draft from inventing one. Review-loop terminations are run-level events, not daemon-level exits.

## 4. Requirements traceability

| Req (02-components.md C7) | Target-state element |
|---|---|
| `ON-004` config inventory lists `workflow_mode` default + precedence layers | §2 (a) |
| Operator visibility into review-loop via existing JSONL path; no new command | §2 (c) |
| Mode is NOT operator-controllable at runtime | §2 (b) |
| `needs-attention` queue is operator-drained, no auto-retry | §2 (d) |
| `ON-002` exit-code taxonomy — fold into existing `needs-attention` close path (no new category) | §2 (e) |
| Iteration cap (3) is NOT operator-tunable | §2 (a) inventory entry, §2 (b) |
| Pause / drain integrates with review-loop iteration boundaries | §2 (f) |

## 5. Open decisions remaining for spec-draft pass

- **`needs-attention` encoding.** Bead label (`needs-attention`), Beads status (`blocked`?), or both. Recommend label (`needs-attention`) for consistency with existing `workflow:<mode>` label encoding and to avoid mis-using Beads's write-subset enum. Spec-draft confirms and coordinates with `beads-integration.md §4.4 BI-010a` table.
- **`harmonik status` output extension.** Whether `harmonik status` adds a `--review-loop` subcommand or simply surfaces review-loop info inline when a run is in that mode. Recommend inline (no subcommand); spec-draft picks.
