# operator-nfr.md — extqueue amendment research

All evidence is `specs/operator-nfr.md` file:line + verbatim quote. No commitments to wording — sketches only.

## Findings (file:line + verbatim quotes)

### Q1 — ON-015 (lines 298–302, §4.4)

Verbatim (line 300):
> "The operator's pending-tasks list and harmonik's dispatchable queue are the same store: Beads (SQLite, `Dicklesworthstone/beads_rust`) per [beads-integration.md §4.1]–[beads-integration.md §4.3]. Queue-format compatibility MUST be the union of (a) Beads schema compat (managed upstream) AND (b) harmonik's overlay schema compat: the `Harmonik-Bead-ID` trailers in checkpoint commits per [execution-model.md §4.4], the bead-ID references in events per [event-model.md §6.3], and the session-log bead-ID metadata per [workspace-model.md §4.7]. Both halves MUST be N-1 readable."

**Dispatch-order claim**: sentence 1 only — "The operator's pending-tasks list and harmonik's dispatchable queue are the same store: Beads." This is the sentence that conflates catalog with execution plan and needs replacement.

**Work-catalog claim (keep)**: sentence 2 — overlay-schema compat for trailers, event bead-IDs, session-log metadata — is catalog-facing (bead identity), independent of dispatch ownership.

Proposed amendment sketch: rewrite sentence 1 to "Beads is the catalog of work — the authoritative store for bead identity, status, and `blocks` edges per [beads-integration.md §4.1]–[beads-integration.md §4.3]. The daemon's dispatch order is the execution plan layered on top, owned by [queue-model.md §X] and persisted in `.harmonik/queue.json` (see ON-018)." Sentence 2 unchanged. ON-016 (line 304, startup schema check) now references two version checks (Beads + queue.json).

Heading: recommend leave alone (inbound-citation blast radius); breadcrumb in body.

### Q2 — ON-008 (line 211) + ON-027 step (1) (line 392)

ON-008 itself does **not** contain "stops pulling new tasks" — it cites out to "the full drain sequence of §4.7.ON-027 (all eight steps)." No edit needed in ON-008 for step-(1) phrasing.

ON-027 step (1) verbatim (line 392): "(1) orchestrator stops pulling new tasks from the queue;"

Surgical edit sketch: "(1) the daemon stops advancing the queue: no new dispatches are issued; in-flight runs proceed per step (2); the queue transitions to a `paused-by-drain` sub-state per [queue-model.md §X];". Two changes: "pulling" → "advancing" (no poll under extqueue), and adding the explicit `paused-by-drain` sub-state cue.

Knock-on: §7.2 drain pseudocode (line 733) probably mirrors the step-1 wording; needs a parallel edit. Not read in detail; flag for design pass.

### Q3 — ON-009a (line 226, §4.3)

The word "queue" appears in heading ("Needs-attention queue drain discipline", line 224) and in body. Context anchors the queue to the ledger: "The daemon's ready-work query per [beads-integration.md §4.5] MUST treat `needs-attention`-labeled beads as out-of-scope for automatic claim." This is a Beads-side concept, not the extqueue execution queue. **Confirmed.**

Proposed disambiguation note (append to ON-009a): "Terminology note: 'queue' in this requirement refers to the ledger's needs-attention bead set (a Beads-side concept) — not the daemon execution queue defined in [queue-model.md §X]. The two are layered, not coextensive."

### Q4 — ON-013a (line 273, §4.3)

Verbatim:
> "Every operator-command-dispatch goroutine (the goroutine handling `pause`, `stop`, `upgrade`, `attach`, `enqueue`) MUST install a `defer recover()` barrier."

Current enumeration: 5 commands. Per 02-components.md §4, new JSON-RPC methods are `queue.submit / queue.status / queue.append / queue.dry-run` (v0.1).

Two addition shapes:
- **Itemized**: list each `queue.*` method. Pro: explicit. Con: future `queue.*` methods need ON-013a edits.
- **Categorical**: "every operator-command-dispatch goroutine, including all `queue.*` JSON-RPC methods per [process-lifecycle.md §X PL-003a]". Pro: future-proof. Con: hides v0.1 names.

House style favors itemized; recommend itemized + tail "see [process-lifecycle.md §X] for the canonical method list."

`enqueue` row fate cross-links to Q8.

### Q5 — ON-018 (line 321, §4.5)

Verbatim:
> "Every versioned on-disk or wire artifact declared by foundation specs — event-envelope schema ([event-model.md §6.1]), event payload schemas ([event-model.md §6.3]), checkpoint trailers and sibling files ([execution-model.md §4.4]), queue overlay (§4.4.ON-015), policy schema ([control-points.md §6.3]) — MUST maintain N-1 readability."

Proposed placement: between "queue overlay (§4.4.ON-015)" and "policy schema": "..., queue overlay (§4.4.ON-015), **queue execution plan ([queue-model.md §X], persisted as `.harmonik/queue.json` with `schema_version` field)**, policy schema..." — keeps queue-adjacent artifacts together.

**ON-INV-001 (line 633)** restates the obligation across artifacts; verify in design pass whether it re-enumerates (parallel edit needed) or back-references (no edit needed). Not fully read in this pass — flag.

### Q6 — ON-026 / config inventory

**ID correction**: the reviewer's "ON-026 config inventory" is wrong. ON-026 (line 384, §4.7) is **prompt-injection defense**: "Prompt-injection defense is handler-owned." The actual config-inventory obligation is **ON-004** (line 157, §4.1).

ON-004 verbatim relevant clause (line 159):
> "At minimum the inventory covers the timer-flush cadence ([event-model.md §4.4]), budget warning threshold ([control-points.md §4.5]), drain timeout (§4.7), RTO thresholds (§4.8), **queue-empty re-query cadence ([process-lifecycle.md §4.4])**, Cat 0 pre-check retry cadence ([reconciliation/spec.md §4.3]), per-Cat reconciliation budgets ([reconciliation/spec.md §4.4]), and the `workflow_mode` knob per §4.1.ON-004a."

`queue-empty re-query cadence` is obsolete under extqueue (daemon does not poll; waits for `queue.submit` on socket).

**Precedent for retiring a knob**: searched. The only "retired" precedent in this spec is at **invariant ID** granularity, not knob-line-item granularity:
- ON-INV-002 retired in v0.3 (line 639): "**Retired.** ... This ID is permanently retired; never reuse."
- ON-INV-004 retired in v0.3 (line 653): same pattern.
- §2.1a (line 52) breadcrumb: "Operational-posture assumption (formerly ON-INV-002)."

**No precedent for retiring a single line-item inside an ON's enumeration.** Closest precedent for sub-ID edits is the v0.3.0 changelog B3 citation migration (line 1002).

Two paths:
- **Quiet deletion**: strike the clause from ON-004; record in §A.4 changelog with rationale + explicit "no ON-NNN added or retired." Matches the spec's normal sub-ID amendment style.
- **Explicit retirement note**: add a "Retired in vX.Y:" sub-bullet under ON-004. No precedent; invents an affordance.

Recommend quiet deletion + §A.4 entry. The component-matrix's "removed, not reassigned" framing supports this.

### Q7 — ON-041 (line 526, §4.10)

Verbatim relevant clause:
> "(b) daemon-identification flags on all daemon-communicating commands (stop, pause, attach, status, upgrade) — at minimum `--socket <path>`, `--cwd <path>`, and `--daemon-id <id>`;"

Note: ON-041 (b) enumerates **operator-facing CLI commands**, not JSON-RPC method names. Pre-extqueue, `enqueue` was not in this list. Under extqueue, the new CLI surface is `hk queue submit / status / append / dry-run` (v0.1).

Proposed addition: extend (b) to "(stop, pause, attach, status, upgrade, **queue (subcommands: submit, status, append, dry-run)**)". Daemon-id flags carry over uniformly.

**ON-050 parallel** (line 562, `harmonik attach`): "(d) accept operator commands inline (subset of `pause`, `resume`, `stop`, `enqueue`)" — needs decision per Q8 on `enqueue` retire/alias. Track as a parallel edit, not an independent choice.

### Q8 — `enqueue` retire vs alias

References to `enqueue` in operator-nfr.md:
- **ON-013a (line 273)** — supervision enumeration.
- **ON-050 (line 562)** — `harmonik attach` minimum surface.
- Not in ON-041 (line 526). Component-matrix lists PL-003a + ON-013a + ON-041 + `harmonik attach`; ON-041 reference is for the broader command-set inventory, not for an `enqueue` row.

**Spec-text constraints on retire-vs-alias:**

- **ON-018 (line 321) + ON-019 (line 327)**: N-1 compat + migration-release rule applies to "versioned on-disk or wire artifacts" — CLI command names are **not** in that enumeration. Renaming `enqueue` → `queue-append` is *not* a migration-release-triggering change. Weakly supports retirement (no spec-level cost to dropping).
- **ON-013c (line 279)** operator-command idempotency: an alias would inherit `queue-append`'s idempotency story. Easy if append is idempotent on dup-bead-id; awkward if not. Decided in queue-model.md.
- **ON-INV-006 (line 670)**: no-bypass-control-surface — neither path violates.

**No spec text forces the alias path.** Component-matrix line 67 stated preference: "The retire path is preferred for naming consistency; the alias path keeps backward compat with operator muscle memory." No documented external consumer depends on `enqueue` (project pre-Phase-2 per memory). **Recommend retire.**

## Patterns to adopt

- **Surgical edits + §A.4 changelog entry.** House style for sub-ID changes per v0.4.1 / v0.3.0 entries (lines 1001–1002).
- **ID-FREEZE preservation.** None of these amendments add/retire an ON-NNN ID. Changelog should state this.
- **Cross-spec forward refs as `[queue-model.md §X]`.** Placeholders until queue-model.md lands; matches v0.3.0 EV cross-ref pattern.
- **Inline disambiguation note, not heading rename.** Inbound citations make heading rename expensive.
- **Itemized enumeration over categorical** for ON-013a / ON-018 / ON-041 additions — consistent with existing style.

## Open choice points

1. **Heading of ON-015**: keep verbatim (inbound-cite-safe) vs rewrite. Recommend keep + body amendment.
2. **Step-1 actor ("daemon" vs "orchestrator")**: ON-027 conflates these throughout (e.g., step 7: "orchestrator exits"). Resolving the conflation is bigger than this work; recommend keep "orchestrator" in step-1 parity and flag as separate OQ.
3. **`enqueue` retire vs alias**: no spec constraint forces alias. Recommend retire.
4. **Component-matrix ID confusion**: the reviewer's "ON-026" pointer is wrong — actual target is **ON-004**. Surface this correction in the amendment package.
5. **Quiet deletion vs explicit retirement-line for `queue-empty re-query cadence`**: no precedent for line-item retirement. Recommend quiet deletion + §A.4 changelog.
6. **ON-INV-001 mirror (line 633)**: verify whether it re-enumerates artifacts or back-references ON-018; parallel edit may be needed for queue.json.
7. **§7.2 drain pseudocode (line 733)**: probably mirrors ON-027 step-1 wording; needs parallel edit. Flag for design pass.
8. **ON-050 `enqueue` reference (line 562)**: flows from Q8 decision; track as parallel edit.
