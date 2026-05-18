# operator-nfr — Change Design

Scope: surgical amendments to `specs/operator-nfr.md` reconciling the extqueue execution-queue concept with ON's existing requirements. All edits are sub-ID clause-level rewrites or insertions; ID-FREEZE preserved (no ON-NNN added or retired). Sister design doc: `04-design/queue-model-design.md` (the new spec these clauses reference).

## Current state

Verbatim quotes of each affected clause (per `03-research/operator-nfr/findings.md`):

**ON-004 (line 159, §4.1) — config inventory:**
> "At minimum the inventory covers the timer-flush cadence ([event-model.md §4.4]), budget warning threshold ([control-points.md §4.5]), drain timeout (§4.7), RTO thresholds (§4.8), queue-empty re-query cadence ([process-lifecycle.md §4.4]), Cat 0 pre-check retry cadence ([reconciliation/spec.md §4.3]), per-Cat reconciliation budgets ([reconciliation/spec.md §4.4]), and the `workflow_mode` knob per §4.1.ON-004a."

**ON-009a (line 224 heading, line 226 body, §4.3) — needs-attention queue:** Heading "Needs-attention queue drain discipline"; body refers to the bead-label needs-attention set and the ledger's ready-work query. Conflates with the new execution-queue concept.

**ON-013a (line 273, §4.3) — per-command supervision:**
> "Every operator-command-dispatch goroutine (the goroutine handling `pause`, `stop`, `upgrade`, `attach`, `enqueue`) MUST install a `defer recover()` barrier."

**ON-015 (line 300, §4.4) — queue-format compatibility:**
> "The operator's pending-tasks list and harmonik's dispatchable queue are the same store: Beads (SQLite, `Dicklesworthstone/beads_rust`) per [beads-integration.md §4.1]–[beads-integration.md §4.3]. Queue-format compatibility MUST be the union of (a) Beads schema compat (managed upstream) AND (b) harmonik's overlay schema compat: the `Harmonik-Bead-ID` trailers in checkpoint commits per [execution-model.md §4.4], the bead-ID references in events per [event-model.md §6.3], and the session-log bead-ID metadata per [workspace-model.md §4.7]. Both halves MUST be N-1 readable."

**ON-018 (line 321, §4.5) — N-1 compat enumeration:**
> "Every versioned on-disk or wire artifact declared by foundation specs — event-envelope schema ([event-model.md §6.1]), event payload schemas ([event-model.md §6.3]), checkpoint trailers and sibling files ([execution-model.md §4.4]), queue overlay (§4.4.ON-015), policy schema ([control-points.md §6.3]) — MUST maintain N-1 readability."

**ON-027 step (1) (line 392, §4.7) — drain step 1:**
> "(1) orchestrator stops pulling new tasks from the queue;"

**ON-041 step (b) (line 526, §4.10) — daemon-communicating commands:**
> "(b) daemon-identification flags on all daemon-communicating commands (stop, pause, attach, status, upgrade) — at minimum `--socket <path>`, `--cwd <path>`, and `--daemon-id <id>`;"

**ON-050 step (d) (line 562, §4.10) — `harmonik attach`:**
> "(d) accept operator commands inline (subset of `pause`, `resume`, `stop`, `enqueue`);"

**ON-INV-001 Sensor (line 635, §5):**
> "Corpus-wide compat-matrix test harness: for every artifact declared by foundation specs (event envelope, event payload schemas per [event-model.md §4.7], checkpoint trailer per [execution-model.md §4.4], queue overlay, policy schema per [control-points.md §6.3]), produce writer output at version N and parse at a reader pinned to N-1; failure of ANY pair flips the invariant."

**§7.2 drain pseudocode (line 738):**
> "    stop_dispatch_loop()                                 -- step 1"

## Target state

Copy-paste-ready replacement text for each amendment. Quoted blocks are the new clause as it should appear in the spec.

### ON-004 — config inventory (line 159)

Delete the clause `queue-empty re-query cadence ([process-lifecycle.md §4.4])` (quiet deletion; the daemon no longer polls under extqueue). The replacement sentence reads:

> "At minimum the inventory covers the timer-flush cadence ([event-model.md §4.4]), budget warning threshold ([control-points.md §4.5]), drain timeout (§4.7), RTO thresholds (§4.8), Cat 0 pre-check retry cadence ([reconciliation/spec.md §4.3]), per-Cat reconciliation budgets ([reconciliation/spec.md §4.4]), and the `workflow_mode` knob per §4.1.ON-004a."

No surrounding-paragraph or §A.3 rationale change. The deletion is recorded only in §A.4 changelog.

### ON-009a — disambiguation note (append after current body, before `Tags:`)

Append the following paragraph as the final body paragraph of ON-009a (between the existing "structural violation of this requirement." sentence and the `Tags: mechanism` line):

> "**Terminology note.** The "queue" in this requirement is the *needs-attention bead set* — a Beads-side concept defined by the `needs-attention` label per [beads-integration.md §4.3]. It is NOT the daemon's execution queue defined in [queue-model.md §1] and persisted at `.harmonik/queue.json`. The two are layered: the needs-attention set governs which beads an orchestrator MAY enqueue into the execution queue; the execution queue governs which queued beads the daemon dispatches. Operator drain actions in this requirement (label removal, `wontfix` closure) mutate the bead set, not the execution queue."

Heading unchanged (inbound-cite-safe per research §Q3).

### ON-013a — extend the enumeration

Replace the parenthetical command list in the first sentence:

> "Every operator-command-dispatch goroutine (the goroutine handling `pause`, `stop`, `upgrade`, `attach`, and the `queue-submit` / `queue-append` / `queue-status` / `queue-dry-run` JSON-RPC methods per [process-lifecycle.md §4.1 PL-003a]) MUST install a `defer recover()` barrier."

Rationale for the change: itemized style matches house convention (research §Q4); `enqueue` is removed from the enumeration per the retire decision; v0.1 queue-method names listed explicitly with a trailing forward-ref to PL for the canonical method list. The remainder of ON-013a (panic-event emission, state-machine revert, `degraded` escalation) is unchanged.

### ON-015 — sentence-1 rewrite

Replace the first sentence only. Sentence 2 ("Queue-format compatibility MUST be the union of …") and the rest of the paragraph are unchanged.

New sentence 1:

> "Beads is the catalog of work — the authoritative store for bead identity, status, and `blocks` edges per [beads-integration.md §4.1]–[beads-integration.md §4.3]. The daemon's execution queue (dispatch order and group structure) is the execution plan layered on top, owned by [queue-model.md §2] and persisted in `.harmonik/queue.json` per ON-018."

Heading unchanged per research §Q1 (inbound-cite-safe; ON-016 still references ON-015 for the schema-check pair).

### ON-018 — extend the artifact enumeration

Insert the new artifact between `queue overlay (§4.4.ON-015)` and `policy schema`. Replacement of the enumeration sentence:

> "Every versioned on-disk or wire artifact declared by foundation specs — event-envelope schema ([event-model.md §6.1]), event payload schemas ([event-model.md §6.3]), checkpoint trailers and sibling files ([execution-model.md §4.4]), queue overlay (§4.4.ON-015), queue execution plan ([queue-model.md §3], persisted as `.harmonik/queue.json` with a `schema_version` field), policy schema ([control-points.md §6.3]) — MUST maintain N-1 readability."

Remainder of ON-018 ("A reader pinned to version N-1 MUST…") unchanged.

### ON-027 step (1) — drain-step rewording

Replace step (1) inline:

> "(1) the daemon stops advancing the queue: no new dispatches are issued from the active group, and pending groups do not advance; in-flight runs proceed per step (2); the queue's status field transitions to `paused-by-drain` per [queue-model.md §5];"

Steps (2) through (7) and the closing "In the pause/upgrade path…" prose are unchanged. The "eight-step" framing (with step 3a) is preserved; the change is purely the wording of step (1).

### ON-041 step (b) — add `queue` to daemon-communicating commands

Replace step (b):

> "(b) daemon-identification flags on all daemon-communicating commands (`stop`, `pause`, `attach`, `status`, `upgrade`, and `queue` with its subcommands `submit`, `status`, `append`, `dry-run`) — at minimum `--socket <path>`, `--cwd <path>`, and `--daemon-id <id>`;"

The `queue status` subcommand shares its identifier with the top-level `status` command; both honor the daemon-id flags uniformly. No knock-on change to ON-041 (a), (c), or to the `harmonik list` column set.

### ON-050 step (d) — retire `enqueue` from the attach inline-command subset

Replace step (d):

> "(d) accept operator commands inline (subset of `pause`, `resume`, `stop`);"

Rationale: `enqueue` is retired per the cross-cutting decision. Append, the operation `enqueue` formerly served, is now reached through `queue append` — a daemon-communicating command per ON-041(b), not an attach inline shorthand. The attach inline subset is therefore narrowed; the operator who wants to append from an attach session does so by issuing the regular `harmonik queue append` command (the attach session_id is still carried on emissions per ON-039). No spec text constrains keeping `enqueue` as an alias (research §Q8); `enqueue` is removed cleanly with no replacement at this surface.

### ON-INV-001 Sensor — parallel artifact enumeration

ON-INV-001's Sensor clause re-enumerates the artifact set (research §Q5 confirmed). Replace the sensor parenthetical:

> "**Sensor.** Corpus-wide compat-matrix test harness: for every artifact declared by foundation specs (event envelope, event payload schemas per [event-model.md §4.7], checkpoint trailer per [execution-model.md §4.4], queue overlay, queue execution plan per [queue-model.md §3] (`.harmonik/queue.json`), policy schema per [control-points.md §6.3]), produce writer output at version N and parse at a reader pinned to N-1; failure of ANY pair flips the invariant. Sensor runs corpus-level per [architecture.md §4.1] AR-004."

The invariant body sentence ("Every versioned on-disk or wire artifact declared by foundation specs MUST hold the N-1 readability property…") is unchanged; only the Sensor enumeration is amended to match ON-018.

### §7.2 drain pseudocode — parallel step-1 wording

The pseudocode at line 738 names step 1 as `stop_dispatch_loop()`. Replace the line and its comment to mirror the ON-027 rewording:

> "    stop_queue_advancement()                             -- step 1: no new dispatches; queue → `paused-by-drain` per [queue-model.md §5]"

No other pseudocode lines change. The narrative under the code block ("Every branch corresponds to a normative requirement…") is unchanged because step 1 still maps to ON-027.

## Changelog entry — §A.4

Append the following row to the changelog table:

> | 2026-05-14 | 0.4.2 | foundation-author | extqueue reconciliation pass. Surgical amendments aligning ON with the new `specs/queue-model.md` (extqueue work). **ON-004** — quietly deleted the `queue-empty re-query cadence ([process-lifecycle.md §4.4])` line-item from the config inventory; the daemon no longer polls under extqueue (orchestrator submits via `queue-submit` over the daemon socket). No knob is renamed or relocated; the slot is removed, not reassigned. Precedent: invariant-level retirement exists (ON-INV-002/-004 retired v0.3) but no precedent for line-item retirement; quiet deletion + this changelog entry chosen over an explicit "Retired in v0.4.2" sub-bullet to avoid inventing an affordance. **ON-009a** — appended a disambiguation note distinguishing the needs-attention bead set (Beads-side, this requirement) from the daemon execution queue ([queue-model.md §1], persisted as `.harmonik/queue.json`); heading unchanged for inbound-cite safety. **ON-013a** — replaced the operator-command enumeration's `enqueue` entry with the explicit v0.1 `queue-*` JSON-RPC methods (`queue-submit`, `queue-append`, `queue-status`, `queue-dry-run`) plus a forward-ref to [process-lifecycle.md §4.1 PL-003a] for the canonical method list. **ON-015** — rewrote sentence 1 only: "Beads is the catalog of work … the daemon's execution queue is the execution plan layered on top, owned by [queue-model.md §2] and persisted in `.harmonik/queue.json` per ON-018." Sentence 2 (overlay-schema compat for trailers/event bead-IDs/session-log metadata) and the rest of the paragraph unchanged. Heading unchanged. **ON-018** — extended the N-1 artifact enumeration with `queue execution plan ([queue-model.md §3], persisted as .harmonik/queue.json with a schema_version field)`, placed between `queue overlay (§4.4.ON-015)` and `policy schema`. **ON-027 step (1)** — reworded from "orchestrator stops pulling new tasks from the queue" to "the daemon stops advancing the queue: no new dispatches are issued …; the queue's status field transitions to `paused-by-drain` per [queue-model.md §5]". Steps (2)–(7) and (3a) unchanged. **§7.2 drain pseudocode** — parallel edit: renamed `stop_dispatch_loop()` to `stop_queue_advancement()` with updated comment mirroring ON-027 step (1). **ON-041 step (b)** — added `queue` (with subcommands `submit`, `status`, `append`, `dry-run`) to the daemon-communicating-commands list; daemon-id flags carry uniformly. **ON-050 step (d)** — removed `enqueue` from the `harmonik attach` inline-command subset; the subset is now `{pause, resume, stop}`. `enqueue` is retired with no alias (no spec text requires backward compat on CLI command names). **ON-INV-001 Sensor** — parallel artifact-enumeration edit to match ON-018: added `queue execution plan per [queue-model.md §3] (.harmonik/queue.json)`. Invariant body unchanged. **ID-FREEZE preserved.** No ON-NNN added or retired by this revision. No invariants added or retired. No §8 exit codes touched (new `queue_validation_failed` failure modes live in queue-model.md's JSON-RPC error space, not in ON §8 exit-code taxonomy). Cross-spec coordination requests: `specs/queue-model.md` is a NEW spec (drafted in the extqueue kerf work) and is a prerequisite for these citations to resolve; process-lifecycle.md is amended in the same work to declare PL-003a's queue-method extension. Status remains `reviewed`. |

## Rationale

- **ON-004 deletion** — D6 (v0.1 minimal): no daemon polling means no re-query cadence. The knob is dead code in the config inventory.
- **ON-009a disambiguation** — D1 / cross-cutting decision: two distinct "queues" exist (bead set vs execution plan). Inline note keeps the heading (and its inbound citations) stable while preventing confusion.
- **ON-013a enumeration** — D5 (Unix-socket transport, JSON-RPC methods are the supervised surface). Explicit method names match house style; forward-ref to PL keeps single-source-of-truth for the method list.
- **ON-015 sentence-1 rewrite** — D1, D2: the framing "Beads is the queue" is structurally wrong under extqueue. The new sentence separates catalog (Beads) from execution plan (queue), matching the components.md §6 reframing. Sentence 2 (overlay-schema fields) is independent of dispatch ownership and stays.
- **ON-018 enumeration extension** — D5 (persisted queue is an N-1-governed on-disk artifact). queue.json carries `schema_version: 1` per queue-model §2 RECORD Queue.
- **ON-027 step (1) rewording** — D2 + cross-cutting decision: under extqueue the daemon advances the queue rather than polling, and the queue itself carries a `paused-by-drain` status that operators can observe. The new wording names both the action (no new dispatches) and the observable state (queue status flip).
- **§7.2 pseudocode** — Parallel edit to keep the pseudocode and ON-027 step (1) wording aligned (Q2 confirmed: prior wording `stop_dispatch_loop()` is the step-1 mirror, so it must move with the requirement).
- **ON-041 (b) extension** — D5: `queue` is daemon-communicating; therefore covered by the `--socket / --cwd / --daemon-id` discipline.
- **ON-050 (d) `enqueue` retire** — Cross-cutting decision (Q8): no spec text forces an alias; `queue append` is the post-retire surface; attach inline-command subset narrows accordingly.
- **ON-INV-001 Sensor parallel edit** — Q5 confirmed re-enumeration: matches ON-018 enumeration extension to keep the corpus-wide compat-matrix harness covering every artifact named in ON-018.

## Requirements traceability

Mapping `02-components.md §6` requirements to the ON-* amendment that lands them.

| 02-components.md §6 requirement | ON amendment |
|---|---|
| ON-015 reframe (catalog vs execution plan) | ON-015 sentence-1 rewrite |
| ON-008 / ON-027 drain step (1) re-anchoring | ON-027 step (1) reword + §7.2 pseudocode parallel edit |
| ON-009a disambiguation (Beads-side queue vs execution queue) | ON-009a appended note |
| ON-013a panic-barrier coverage of `queue.*` methods | ON-013a enumeration replacement |
| ON-018 N-1 enumeration adds `.harmonik/queue.json` | ON-018 insertion + ON-INV-001 Sensor parallel |
| ON-026 / config inventory retire `queue-empty re-query cadence` | ON-004 quiet deletion (research §Q6: target is ON-004, not ON-026) |
| ON-041 daemon-communicating command-set extension | ON-041 step (b) extension |
| `enqueue` retire vs alias (parallel edit to ON-050) | ON-050 step (d) retire-without-alias |

Coverage: every §6 requirement has at least one ON amendment; no amendment exists without a §6 driver. ON-008's body cites out to ON-027's eight-step sequence (research §Q2), so ON-008 needs no direct edit; the ON-027 amendment is what changes ON-008's observable behavior.

## Open choice points carried forward — resolutions

Per research §"Open choice points":

1. **ON-015 heading**: keep verbatim (inbound-cite safe). Body amended. **Adopted as-recommended.**
2. **ON-027 step-1 actor ("daemon" vs "orchestrator")**: research recommended keeping "orchestrator" for parity with later steps (e.g., step 7 "orchestrator exits"). The cross-cutting decision OVERRIDES this: step (1) names "the daemon" (matches D5 — the daemon owns the queue). Step 7's "orchestrator exits" wording is not edited here (out of scope; flagged as separate OQ).
3. **`enqueue` retire vs alias**: retire. No spec constraint forces alias. **Adopted as-recommended.**
4. **Component-matrix ON-026 vs ON-004 confusion**: the actual target is ON-004 (per research §Q6). The amendment package edits ON-004. The component-matrix typo is corrected by reference in the §A.4 changelog (research §Q6 finding cited).
5. **Quiet deletion vs explicit-retirement-line for the cadence knob**: quiet deletion + §A.4 entry. No precedent for line-item retirement. **Adopted as-recommended.**
6. **ON-INV-001 mirror (line 633/635)**: parallel edit IS needed; the Sensor clause re-enumerates rather than back-referencing ON-018. Target-state text supplied above.
7. **§7.2 drain pseudocode (line 738)**: parallel edit IS needed; `stop_dispatch_loop()` is the step-1 mirror line. Target-state text supplied above.
8. **ON-050 `enqueue` reference (line 562)**: retire (flows from Q8); target-state text supplied above.

All choice points resolved in-band; nothing carried forward to the spec-draft pass.
