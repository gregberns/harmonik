# Change Design — A2: `specs/cognition-loop.md`

**Area:** A2 — Pi dispatch surface (CL-070..073) + two-phase-done (CL-051) + pause-verb naming (CL-080)
**Maps to:** G2 (S5/S6/S10 curated dispatch), G4 (S8 two-phase-done), G3 (S9 verb naming), G5 (stale-doc).
**Authoring order:** third (depends on A4 stream-append contract + A3 pause-verb name).
**Research:** `03-research/cognition-loop/findings.md`.

---

## 0. One-line summary — the single most important reframing

**The spec text for CL-051, CL-070..073, and CL-080 is ALREADY FULL NORMATIVE TEXT (research F1).** The assessment's "spec-only, no harness code" framing is half-wrong: the *harness code* is missing, but the *spec is already written*. So A2 is **mostly an annotation + harness-obligation-tightening pass, plus a small two-phase-done clarification** — NOT a re-authoring pass. The bulk of the work the attached beads describe (hk-3ix6o, hk-dg42b) is **implementation against an already-written spec**, surfaced in 07-tasks. Resist re-drafting working normative CL text or renumbering CL IDs (research R1).

## 1. Current state — what the spec says now

All re-verified against the live spec (219 lines) and harness on 2026-05-31:

- **CL-051 two-phase done (`:118-122`).** *"Bead `hk-XYZ` is DONE only when BOTH: (1) `run_completed{success}` … observed in `events.jsonl`; (2) `git log origin/main --grep "Refs: hk-XYZ" --max-count=1` non-empty."* Condition 1 only → in-flight, re-poll. Condition 2 only → emit `loop_observed_phantom_done{bead_id}` + route to Tier-2 reconciliation (RC §4.4); MUST NOT act directly. Glossary (`:68`) and `defer` auto-resolution (`:108`) already reference it.
- **CL-070 (`:152`).** WIP-limited pull; ceiling = `max_concurrent` = refill target.
- **CL-071 (`:153`).** *"Eager pure-code refill on slot release … (1) `kerf next --format=json --only=bead`; (2) for each candidate in rank order, apply CL-072 guards until non-skipped found; (3) submit via `harmonik queue append`; (4) if `kerf next` empty → wake the model … Eager refill MUST NOT consult the model."* — the concrete CLI calls are **already named**.
- **CL-072 (`:154`).** Four pre-screen guards applied in order, skip on hit: (1) already-in-queue; (2) already-landed (`git log origin/main --grep`); (3) failed-twice-this-session → HALT + wake; (4) conflicts-with-in-flight (best-effort). Every skip logged to `dispatch-log.jsonl`.
- **CL-073 (`:160`).** Wake-LLM only at empty-queue boundary; speculative bead generation in eager-refill FORBIDDEN.
- **CL-080 (`:163`).** *"Lifecycle via `harmonik supervise`. Commands flow through `harmonik supervise start/stop/status/pause/resume` per PL §4.9 + ON §4.3. Loop MUST honor daemon `paused`/`pausing` states by not initiating new dispatch."* — canonical verb form is already `harmonik supervise pause/resume`.
- **CL-013 / CL-INV-001 (`:88`, `:177`).** Mechanism/cognition split is normative and byte-clean; `:153` + `:160` + `:199` keep the line clean for eager-refill.
- **CL-055 idempotency table (`:127-134`).** Dispatch-on-empty-queue keyed by `dispatch_intent:<event_id>:<bead_id>` checked against dispatch-log AND queue.json AND `git log --grep`.
- **Execution-model counterparts (research F5).** EM-062 (`:945`, daemon eager-refill: deficit-based `kerf next` ×OVERFETCH_FACTOR → `queue_append`, fires after terminal-event processing), EM-063 (`:970`, two-phase pre-screen), EM-064/065 (`:990`, `:1004`, read-order authority chain + "submit/append targets the active stream group; no double-queue") are the **daemon-side** mirror of CL-070..073; the two layers cross-reference cleanly and must not be re-specified (N4).

**What is NOT done (code, not spec):**

1. **CL-071/CL-072 are zero-percent implemented in the harness (research F2).** A grep of `.pi/extensions/flywheel/*.ts` finds only `harmonik subscribe` and `harmonik digest` (both read-only) — **no `queue submit`, no `queue append`, no `kerf next`, no `git log origin/main`.** This is the S5/S10 gap.
2. **CL-051 is a *definition*, not a *named harness obligation*, and `bridge.ts` does not implement it (research F2).** `bridge.ts:253-258`: the deterministic-tier branch calls `processedEvent(...)` + `emitCognitionEvent(...)` and returns with **no git-trailer check** — a `run_completed` marks the reaction processed without verifying Condition 2. A push-failed run looks done (S8 hole). The spec reads as a definition the harness can ignore; it does not name a producer-with-a-testable-done-condition the implementer is bound to.
3. **CL-080's verb is spec-named + code-promised but unwired (research F4).** `supervise_cmd.go` exposes no `pause`/`resume`; `budget.ts:8` / `circuit-breaker.ts:5` tell the operator to run `harmonik supervise resume` (a command that does not exist today — wired by A3).
4. **`buildMinimalDigest` is a stub (`index.ts:343`, research F2).** Post-recycle digest is placeholder text, not a `harmonik digest --json` call (hk-ytj2r). Relevant to A2 only as post-reset blindness (G2 support), not the dispatch contract.

## 2. Target state — what the spec should say after the change

A2 makes **four narrow spec edits** (no CL renumbering, no new CL mechanism — research R1/F5). The heavy lifting is implementation, surfaced in 07-tasks.

### T1 — Promote CL-051 from a *definition* to a *named harness obligation with a testable done-condition* (G4 / S8)

Amend CL-051 (in place, same ID) to add an explicit **obligation clause** binding the harness:

- The harness (`bridge.ts` / cognition loop) **MUST NOT mark a bead DONE, auto-resolve a `defer` note, or advance a bead past in-flight, on the strength of a `run_completed{success}` event alone.** It MUST additionally confirm Condition 2 (`git log origin/main --grep "Refs: <bead-id>" --max-count=1` non-empty) before treating the bead as done.
- Restate the two single-condition windows as harness **routing obligations** (not just definitions): event-only → treat in-flight, re-poll (do not advance); trailer-only → emit `loop_observed_phantom_done{bead_id}` + route to Tier-2 reconciliation (RC §4.4); MUST NOT act directly (preserves CL-050 / N3 — loop routes, never acts).
- The two-phase check is **idempotency-keyed** per CL-055 (`dispatch_intent`-class is for dispatch; the done-check reuses the reacted-ledger keyed by the triggering `run_completed.event_id`) so it is effectively-once across crashes (C2).
- Cite EM-052/EM-053 as the daemon-side reason Condition 1 can fire without Condition 2 (push may have failed in the terminal multi-step window) — so the converse window is a real, expected state, not a defect.

The **spec edit is small**; the *implementation* (replace the `bridge.ts:253-258` unconditional `processedEvent` with the two-phase gate; add the converse-window routing) is hk-3ix6o, a 07-tasks item. (research R2 — non-trivial, crosses into RC §4.4 territory but stays loop-side / routes-only.)

### T2 — Annotate CL-070..073 with the concrete, already-named Pi drive surface + cite the EM-062..065 daemon mirror (G2 / S5/S6/S10)

CL-071 already names `kerf next --format=json --only=bead` and `harmonik queue append`. A2's edit is to **tighten and cross-link**, not rewrite:

- Add an explicit statement that the harness's curated-dispatch path targets the active **stream** group (per queue-model.md §2.4 / QM-040, A4 T1) — wave groups reject the append CL-071 requires. The **first fill** (when no queue is active) uses `harmonik queue submit` to create the stream group; **refill on slot release** uses `harmonik queue append` against that active stream group (this completes S6 — submit-as-start, no separate start verb, per A4 T3 / N4).
- Add a cross-reference from each CL-07x to its EM-06x daemon-side mirror (CL-071↔EM-062, CL-072↔EM-063, the read-order/no-double-queue discipline↔EM-064/EM-065) so the harness/daemon split stays legible and A2 introduces **no new queue surface** (research F5; queue-model §6 `:212` already confirms "no new queue-method surface required").
- Reaffirm the mechanism/cognition line at the concrete call-site (CL-013/CL-INV-001, C1): `kerf next` / guards / `queue submit`/`append` live in **mechanism** code and MUST NOT round-trip the model; only the empty-queue wake (CL-073) is cognition. The eager-refill submit/append is idempotency-keyed `dispatch_intent:<event_id>:<bead_id>` per CL-055 (C2).
- Resolve **OQ-CL-001** in spec text: when `kerf next` and the in-memory queue.json view disagree, **queue.json wins; kerf is advisory** (consistent with EM-064 tier-1). (research F5; the working answer is already recorded — promote it from OQ to normative note or leave OQ resolved with a confirming sentence.)

The **spec edit is annotation**; the *implementation* (the harness actually shelling `kerf next` + `queue submit`/`append` with the guards, in mechanism code) is hk-dg42b, a 07-tasks item.

### T3 — Confirm CL-080's canonical verb form + flag the dangling code comments (G3 / G5)

- Confirm CL-080 as the **single canonical** verb form: `harmonik supervise pause/resume` (OQ1 resolved here AND in A3; the two must agree). The *producer + agent-callable obligation* is owned by A3 (operator-nfr ON-014a/ON-014b); CL-080 only **references** the verb name and the loop's obligation to honor `paused`/`pausing` by not dispatching.
- Add an informative note that the `budget.ts:8` / `circuit-breaker.ts:5` comments referencing `harmonik supervise resume` are **correct in intent** — they describe the verb A3 wires; their current dangling status (the verb does not exist yet) is a code-state gap, not a spec error. The *comment corrections* are a 07-tasks item tied to hk-5bw7a (kept separate from the spec edit per research R4).

### T4 — Conformance scenarios (SC2, SC4) in §7 acceptance-scenario house style

Add two acceptance scenarios to §7 (prose form matching the existing `:194-199` list):

- **SC2 (CL-071).** *"On `run_completed`/`run_failed`/`run_canceled` with ≥1 free slot and a non-empty `kerf next`, the harness appends the next ranked, non-pre-screened-out bead to the active stream group via `harmonik queue append` WITHOUT waking the model; the model is woken for queue composition only when `kerf next` is empty (or yields only pre-screened-out candidates)."*
- **SC4 (CL-051).** *"A `run_completed{success}` event arrives for `hk-XYZ` but `git log origin/main --grep "Refs: hk-XYZ"` is empty: the harness MUST NOT mark the bead done, MUST treat it as in-flight, and MUST re-poll; conversely a `Refs:` trailer present on origin/main with no terminal event MUST emit `loop_observed_phantom_done` and route to Tier-2, never act directly."*

### T5 — `buildMinimalDigest` stub (G2 support, tasked not spec'd)

CL-030/CL-031 already require the digest be produced by `harmonik digest`. The stub (`index.ts:343`) violates that on the recycle path. **No spec change needed** — the obligation already exists; the fix (shell `harmonik digest --json` on recycle) is hk-ytj2r, a 07-tasks item. A2 may add a one-line note that the recycle-path digest MUST come from the same `harmonik digest` producer (CL-030) — i.e. the stub is non-conforming today.

## 3. Rationale

- **Annotate, don't re-author (research F1/R1 — the load-bearing reframing).** CL-051/070..073/080 are already normative. Re-drafting them risks renumbering working CL IDs (the changelog discipline forbids it) and inflating a code-conformance job into a spec-authoring job. A2's spec deltas are: T1 (one obligation clause on CL-051), T2 (annotation + cross-links on CL-070..073), T3 (one confirmation + one informative note on CL-080), T4 (two scenarios). Everything else is 07-tasks implementation.
- **CL-051 must bind the harness, not just define done (research F2).** The current text reads as a definition `bridge.ts` ignores; promoting it to an explicit harness obligation with a testable done-condition (T1) is exactly what closes S8 — and gives hk-3ix6o a spec line to test against.
- **The harness/daemon mirror must stay legible (research F5).** EM-062..065 own the daemon-side refill; CL-070..073 own the harness/loop side. Cross-linking them (T2) prevents the duplication the assessment's snapshot implied and forecloses A2 inventing a new queue surface (N4).
- **CL-080 wins on the verb name, A3 owns the producer (research F4).** Keeping the verb name in CL-080 but the producer + agent-callable obligation in A3 avoids a parallel surface (C4/N3) and keeps each spec owning its half.

## 4. Decisions recorded

- **OQ1 (verb name) → `harmonik supervise pause/resume`** (agrees with A3). CL-080 already says this; T3 confirms.
- **OQ-CL-001 (kerf vs queue.json authority) → queue.json wins, kerf advisory** (EM-064 tier-1). T2 promotes this to a normative confirming sentence.
- **OQ4 (dependency ordering) → rank-order stream append** (rely on `kerf next` rank + stream append's order-preservation + QM-031 cross-group sequencing; harness does not build explicit multi-group queues at v0.1). Consistent with A4 §4.

## 5. Requirements traceability

| Goal / SC | Target state | CL requirement touched |
|---|---|---|
| G4 / S8 / SC4 — two-phase-done harness obligation | T1 | **CL-051 amended** (obligation clause; same ID) |
| G2 / S5/S6/S10 / SC2 — concrete Pi dispatch surface | T2 | **CL-070..073 annotated** (stream target, EM-06x cross-links, OQ-CL-001 resolved; same IDs) |
| G3 / S9 — pause-verb naming | T3 | **CL-080 confirmed** (canonical form; producer deferred to A3; same ID) |
| G2 — post-reset digest | T5 | CL-030/031 (already-binding; stub fix is 07-tasks hk-ytj2r) |
| G5 — stale-doc fix | T3 note | 07-tasks (hk-5bw7a) |
| SC2, SC4 | T4 | §7 acceptance scenarios |

Every 02-components.md §A2 requirement is addressed:
- "CL-070..073 name the concrete mechanism path + stay byte-clean" → T2.
- "eager-refill submit idempotency-keyed per CL-055" → T2 (C2).
- "CL-051 stated as a harness obligation with a definite done-condition + two-window routing" → T1.
- "CL-080 reconciled to one canonical form; dangling comments noted" → T3.
No target lacks a driver; no CL ID is renumbered or retired (research R1 discipline honored).

## 6. Constraints honored

- **C1 (mechanism/cognition byte-clean).** T2 reaffirms the line at the concrete call-site; `kerf next`/guards/`queue submit`/`append` are mechanism, only the empty-queue wake is cognition (CL-INV-001).
- **C2 (loop is a second consumer; idempotency-keyed).** T1 and T2 each name the CL-055 idempotency key; the loop routes to RC but never acts (CL-050 / N3).
- **N3 (no run-state-ownership change).** T1's converse-window handling routes to Tier-2; the loop does not issue trailer commits or take reconciliation locks.
- **N4 (no new queue surface / no new start verb).** T2 cross-links the *existing* EM-062..065 + queue-model methods; first-fill uses `queue submit` (existing), refill uses `queue append` (existing).

## 7. Dependencies

- **A4** (queue-model): T2 references the stream-append contract (QM-040) and submit-as-start (A4 T3).
- **A3** (operator-nfr): T3 references ON-014a/ON-014b for the producer behind the CL-080 verb name. If A3's final IDs shift, T3's citation updates.
- **A2's two-phase-done sub-part (T1) is independent** of A1/A4 and may be drafted in parallel (02-components.md §Authoring order).

## 8. Cross-spec coordination (for the Integration pass)

- CL-051's Tier-2 routing target `reconciliation/spec.md §4.4` must still resolve (research R2) — verify at Integration.
- The EM-062..065 cross-links must resolve to the current execution-model line ranges (§4.13/§4.14) — verify at Integration.
- `queue-model.md §6` already states "no new queue-method surface required" (`:212`) — A2's annotations must keep that true.
