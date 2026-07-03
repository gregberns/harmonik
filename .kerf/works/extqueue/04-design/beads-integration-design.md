# beads-integration.md — Change Design

## Current state

`specs/beads-integration.md` v0.4.1 frames `br ready` as the daemon's dispatch input and anchors satellite cadence/exclusion requirements off that role. Specifically:

- **BI-013 (line 253–257), §4.5 Ready-work query.** "The daemon MUST be able to query the set of beads whose dependencies are satisfied and whose status is `open` via `br ready` … The ready-work query result is the input to the daemon's dispatch loop." Second paragraph requires the response payload to surface labels for per-bead workflow-mode extraction.
- **BI-013a (line 262–268).** The `br`-CLI adapter's ready-work query MUST exclude beads carrying a `needs-attention` label "at adapter read time so the daemon's dispatch loop never observes a `needs-attention`-labeled bead as ready."
- **BI-024a (line 383).** The `br --version` handshake MUST complete "BEFORE the step 6 `br ready` query so that step 6 does not run against an incompatible `br` version".
- **BI-025c (line 404).** "The 5s read timeout aligns with PL-005 step 6's `br ready` 5s constraint."
- **BI-007 (line 122).** "The dispatch loop (BI-013 ready-work) MUST exclude `draft`-status beads…" — the parenthetical anchors the `draft` exclusion to the daemon's dispatch loop.
- **§4.10 BI-029/BI-030 (line 449–461+).** Intent-log scope is explicitly `<run_id>:<transition_id>:<op>` with `op ∈ {claim, close, reopen}` — terminal transitions only. BI-INV-001 forbids any other write path.
- **ShowBead pre-claim guard.** No BI-* anchor today. Lives in `internal/daemon/workloop.go:439–469` (hk-p4xbw): a status re-read between `Ready()` and `ClaimBead` that emits `bead_claim_skipped` if a competing claim already advanced the bead.

## Target state

Edits are surgical: demote BI-013's daemon-input claim, re-anchor the satellite cites away from "daemon polls `br ready`", and introduce a new validation-surface subsection covering the submit-time read contract. No new write surface; no change to §4.10 intent-log scope.

### Amendment A — BI-013 demotion (line 253–260)

After:

> The orchestrator agent uses `br ready` (or its equivalent command) to discover the set of beads whose dependencies are satisfied and whose status is `open`. The query's result is an input to the orchestrator's queue-planning surface per `[queue-model.md §1 — Scope and terms]`. The daemon MUST NOT consume `br ready` as a dispatch input; the daemon's dispatch input is the submitted queue per `[queue-model.md §8 QM submit]`.
>
> When the orchestrator (or any agent) invokes `br ready`, the response payload MUST surface each bead's labels array — including any `workflow:<mode>` label per BI-009a — so the orchestrator's planning surface can extract and apply the per-task workflow-mode override at submit time. This is a read-path observation that exploits the existing `br ... --format json` label exposure (per BI-025b); no new query surface is introduced.

The second paragraph's label-exposure clause stays — the workflow-mode label still needs to ride the read surface, but the consumer is now the orchestrator-at-submit-time, not the daemon-at-dispatch-time. The daemon re-reads the bead at submit-validation time per Amendment E below; that read also surfaces labels.

### Amendment B — BI-013a needs-attention exclusion (line 262–268)

The exclusion still applies, but its location moves. Under extqueue, the `needs-attention` label gates whether a bead may be RE-DISPATCHED after a `review-loop` iteration-cap hit or operator drain. With the daemon out of selection, the exclusion is now a submit-validation rule, not a read-time filter on `br ready`.

After:

> Beads carrying a `needs-attention` label MUST NOT be accepted into a submitted queue. The submit-time validation contract of §4.5a (BI-013b) MUST reject any `bead_id` whose live record carries the label, returning a typed validation failure per `[queue-model.md §6 QM-021]`. The `br`-CLI adapter's `br ready` output remains a faithful ledger view — it MAY include `needs-attention` beads if Beads itself returns them — and downstream consumers (orchestrator agents, operator tooling) are responsible for filtering them per their own policy.
>
> The label is set by the daemon when a `review-loop` run hits the iteration cap per `[execution-model.md §4.3]` (and by analogous operator-drain semantics per `[operator-nfr.md §4.3]`); its presence asserts that operator triage is required before re-dispatch. An operator who clears the label restores the bead to the submittable set on the next queue-submit.

This preserves the load-bearing safety invariant ("a `needs-attention` bead never silently re-runs") without keeping the daemon dependent on adapter-side filtering. The previous "exclusion at adapter read time" guarantee is replaced by an equivalent submit-time rejection.

### Amendment C — BI-024a re-anchor (line 383)

After:

> The adapter MUST invoke `br --version` at daemon startup (during PL-005 step 4, the Cat 0 prerequisite pre-check where Beads/`br` availability is verified). The handshake MUST complete BEFORE the daemon accepts its first `queue.submit` RPC per `[process-lifecycle.md §4.4 PL-003a]`, so that submit-time validation (§4.5a BI-013b) does not run against an incompatible `br` version. (Remainder of the requirement is unchanged.)

The check still gates first ledger I/O; the dependent event is now first queue-submit rather than first ready-poll.

### Amendment D — BI-025c re-anchor (line 404)

After:

> Every adapter invocation of `br` MUST be bounded by a subprocess wall-clock timeout: 5 s for read commands (default; operator-tunable per `[operator-nfr.md §4.9]`), 10 s for write commands (default; operator-tunable). Timeout expiry MUST classify as `BrUnavailable`. The 5 s read-command default applies to all adapter reads, including the submit-time `br show` per §4.5a BI-013b.

The dropped "aligns with PL-005 step 6's `br ready` 5s constraint" sentence is replaced with a positive statement about the read-timeout default's scope. The PL-005 step 6 prose itself is retired per `02-components.md §4`.

### Amendment E — New §4.5a "Submit-time validation read surface"

Add after the existing §4.5 block (after BI-016/BI-014a, before §4.6 "Bead-ID propagation").

> ### 4.5a Submit-time validation read surface
>
> Under extqueue, the daemon's dispatch input is the externally-submitted queue. Before a queue-submit (or queue-append, or queue-dry-run) is accepted, the daemon MUST validate every referenced bead against the live ledger. This section names the daemon's read surface for that validation; the rules themselves (existence, status, no-double-dispatch, no-duplicate, append-target validity, parallelism-narrowed surfacing) live in `[queue-model.md §6 QM-020..026]` and are NOT restated here.
>
> #### BI-013b — Submit-time bead read uses `br show`
>
> For every `bead_id` named in a `queue.submit` / `queue.append` / `queue.dry-run` request, the adapter MUST invoke `br show <bead_id> --format json` (per BI-025b) and parse the result. The read is subject to the BI-025c 5 s read-timeout discipline and the BI-025a `BrError` taxonomy. The parsed record supplies the inputs the validation rules of `[queue-model.md §6 QM-020 (existence)]`, `[queue-model.md §6 QM-021 (status)]`, and `[queue-model.md §6 QM-022 (no-double-dispatch)]` consume. The label set on the returned record is the input the validation rule of Amendment B (BI-013a, `needs-attention` rejection) consumes.
>
> The validation MUST NOT mutate Beads. Submit-time reads are read-only; BI-INV-001 continues to apply.
>
> #### BI-013c — Pre-claim status re-read
>
> Between the dispatcher's selection of a queue item and the `claim` write to Beads, the daemon MUST re-read the bead's status via `br show <bead_id>` and confirm `status = open`. If the re-read returns a non-open status, the daemon MUST skip the claim, emit `bead_claim_skipped{bead_id, observed_status, reason="status_changed_between_select_and_claim"}` per `[event-model.md §8.8]`, and return the item to its group with status `deferred-for-ledger-dep` (when the change indicates an external claim by another daemon process — disallowed per BI-009 and §4.8a BI-025e operator note — or a status flip to `closed`/`tombstone`/`deferred`).
>
> This requirement names the ShowBead pre-claim guard that lived as implementation-only in `internal/daemon/workloop.go:439–469` (hk-p4xbw). Under extqueue, the guard becomes load-bearing for the queue's exactly-once dispatch guarantee and warrants a normative anchor.

### Amendment F — BI-007 line 122 prose cleanup

After:

> `draft` specifically is the harmonik-side readiness mechanism for loaded but not-yet-dispatchable beads (per the decompose-to-tasks discipline). Submit-time validation per §4.5a BI-013b MUST reject any `bead_id` whose live status is `draft`, mapping the rejection through `[queue-model.md §6 QM-021 (status)]`. `br ready`'s native exclusion of `draft`-status beads remains available to orchestrator agents using the Beads-CLI skill per §4.9 BI-027.

The "dispatch loop (BI-013 ready-work)" parenthetical is gone; the `draft` exclusion is restated as a submit-time validation duty plus an orchestrator-side affordance.

### Amendment G — §4.10 intent-log scope clarification

Add at the end of the §4.10 header informative block (after the existing `.harmonik/beads-intents/` ownership note, before BI-029):

> INFORMATIVE — Intent-log scope is unchanged under extqueue. The intent-log discipline of BI-029/BI-030 covers ONLY terminal-transition writes to Beads (`op ∈ {claim, close, reopen}`). Queue submissions, appends, removes, pauses, and resumes are daemon-internal state mutations that are NOT Beads writes; they are persisted under the separate `.harmonik/queue.json` discipline of `[queue-model.md §3 QM-001..003]`. The two on-disk contracts coexist in `.harmonik/` and are independent. BI-INV-001 ("no intra-run writes") continues to apply to Beads writes only; it does NOT constrain queue mutations.

This is purely defensive: it pre-empts the confusion of "a queue submission is a state-mutating operation; does it need an intent log?" by stating "no, the intent log's scope is unchanged."

## Rationale

| Amendment | Driver |
|---|---|
| A — BI-013 demotion | D1/D6 (problem-space): daemon ceases selection; queue is the input per `[queue-model.md §8 submit]`. Research Q1, Q6: workloop.go:392 is the sole production caller; demotion is exact. |
| B — BI-013a relocation | D4 (honor-ledger surface): the safety promise (never silently re-dispatch a `needs-attention` bead) is preserved by rejecting at submit time. Research Risks bullet: the previous read-time exclusion weakens when the orchestrator is human-facing; submit-side enforcement is the durable place for it. |
| C — BI-024a re-anchor | D6 minimal-impact edit: the handshake's job (don't operate against an incompatible `br`) is preserved; only its dependent event changes. Research Q1/Risks. |
| D — BI-025c re-anchor | Same as C. The timeout default is independent of PL-005's old step-6 prose. |
| E — §4.5a + BI-013b/c | D4 (validation surfaces, not silently rejects) + research Q3: ShowBead is already the implementation primitive; lifting it to a named requirement closes the spec gap on submit-time validation reads. Research Patterns bullet 2 ("New §4.5a or §4.4a Validation contract on submit"). |
| F — BI-007 prose cleanup | Research Risks bullet 2: the parenthetical "(BI-013 ready-work)" is obsoleted by Amendment A. |
| G — §4.10 scope clarification | Research Q2: intent log covers `op ∈ {claim, close, reopen}` only; queue mutations are NOT Beads writes. Stating this explicitly costs nothing and prevents the natural misreading. |

## Requirements traceability

Mapping `02-components.md §3` requirements to amendments and the resulting BI-NNN anchors.

| 02-components.md §3 requirement | Amendment | BI anchor after edit |
|---|---|---|
| `br ready` no longer a daemon input; remains an orchestrator tool | A | BI-013 (demoted) |
| Daemon read surface = `br show` (existence + status) | E | BI-013b |
| Validation rules cite queue-model, not restated in BI | E | BI-013b (cross-ref `[queue-model.md §6 QM-020..022]`) |
| Daemon write surface unchanged (claim / close / reopen) | (no-op) | BI-010, BI-010a, BI-012 unchanged |
| `needs-attention` exclusion preserved, relocated to submit-time | B | BI-013a (reframed) |
| `draft` exclusion preserved, relocated to submit-time | F | BI-007 (prose cleanup, cites BI-013b) |
| `br --version` handshake still gates first ledger I/O | C | BI-024a |
| Read-timeout discipline unchanged | D | BI-025c |
| Intent log scope unchanged — terminal transitions only | G | §4.10 informative + BI-029/BI-030 unchanged |
| Pre-claim status re-read (formerly hk-p4xbw, implementation-only) | E | BI-013c |
| `blocks`-edge semantics still authoritative for in-group dispatch | (no-op) | BI-006, BI-014 unchanged (consumed by execution-model.md per `02-components.md §2`) |

## ShowBead pre-claim guard — decision: SPEC IT

Research flagged: should the ShowBead pre-claim guard (workloop.go:439–469, hk-p4xbw) be lifted to a BI-NNN requirement, or remain implementation-only?

**Decision: spec it as BI-013c (Amendment E).**

Reasoning. Two questions: (1) does BI already anchor code-only invariants? (2) does the guard's role change under extqueue?

For (1): BI does anchor implementation-shaped invariants where they carry cross-spec or cross-process safety meaning — BI-009 (atomic-claim semantics, a Beads-side property the daemon relies on), BI-INV-001 (no intra-run writes, a code-discipline rule), BI-025e (no adapter-side serialization, a code-discipline rule). The bar isn't "is it implementation-only?" but "does breaking this silently violate another spec'd contract?"

For (2): yes. Under extqueue, the dispatcher selects from the queue and claims. Between selection and claim, the bead's status can change (concurrent daemon claim — supposedly disallowed per BI-009 / BI-025e operator note — or a reconciliation Cat 3c close, or an operator close-via-`br`). If the dispatcher skips the re-read, two failure modes appear: (i) the daemon attempts to claim a `closed` bead (BI-009 atomic claim returns `BrConflict`, but the queue's accounting now shows a phantom dispatch); (ii) the daemon emits `run_started` for a bead that is no longer `open`, breaking the BI-010a status-mapping table's `open → in_progress` precondition.

The guard's role is now a load-bearing public contract: it preserves the queue's exactly-once dispatch guarantee and the BI-010a transition table. That is exactly the bar BI-009 and BI-025e clear. Spec it.

The cost is small (two paragraphs in §4.5a), the asymmetric risk of silent regression is large (queue accounting bug surfaces as Cat 3 reconciliation flag, not as a clean failure), and it lets the kerf-tasks pass schedule a sensor against BI-013c per AR-042.
