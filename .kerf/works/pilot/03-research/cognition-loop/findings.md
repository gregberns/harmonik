# Research — A2: `specs/cognition-loop.md` (Pi dispatch surface CL-070..073 + two-phase-done CL-051 + pause-verb CL-080)

**Component requirement (from 02-components.md):** realize CL-070..073 (curated dispatch) and CL-051 (two-phase-done) as a concrete Pi drive surface that names the CLI calls; reconcile CL-080's pause-verb naming. Maps to G2 (S5/S6/S10), G4 (S8), G3 (S9 naming), G5 (stale-doc).

All anchors re-verified against the live tree on 2026-05-31.

## Research Questions

1. Do CL-070..073 and CL-051 already exist as normative spec text, or are they spec-only stubs the assessment thinks need writing?
2. What does the Pi harness (`.pi/extensions/flywheel/*.ts`) actually call today, and where is the two-phase-done check missing in code?
3. Is the CL-013 mechanism/cognition line already drawn for eager-refill, and does the harness honor it?
4. What does CL-080 name the pause verb as, and what do the code comments (`budget.ts`, `circuit-breaker.ts`) reference that does not exist?
5. How does execution-model §4.13/§4.14 (EM-062..065) relate to CL-070..073 — duplication or layered ownership?

## Findings

### F1 — CL-070..073 and CL-051 are ALREADY FULL NORMATIVE TEXT in the spec; the gap is code, not spec text (this is the single most important finding for A2)

The assessment's S5/S8/S10 "spec-only, no harness code" framing is half right: the *harness code* is missing, but the *spec text is already drafted and normative*. Verified:

- **CL-051 two-phase done** (`cognition-loop.md:118`): *"Bead `hk-XYZ` is DONE only when BOTH:"* (`:118-122`) — Condition 1 = `run_completed{success}`, Condition 2 = `Refs: <bead-id>` trailer on `origin/main`; *"Condition 2 only (trailer without terminal event) = converse window. Loop MUST emit `loop_observed_phantom_done{bead_id}` warning and route to Tier-2 reconciliation (RC §4.4); MUST NOT act directly."* The glossary (`:68`) and `defer` auto-resolution (`:108`) already reference it. **The spec is complete; bridge.ts does not implement it.**
- **CL-070** (`:152`): WIP-limited pull, ceiling = `max_concurrent`.
- **CL-071** (`:153`): *"Eager pure-code refill on slot release ... (1) `kerf next --format=json --only=bead`; (2) for each candidate in rank order, apply CL-072 guards until non-skipped found; (3) submit via `harmonik queue append`; (4) if `kerf next` empty -> wake the model ... Eager refill MUST NOT consult the model."* — the **concrete CLI calls are already named in the spec.**
- **CL-072** (`:154`): pre-screen guards, applied in order, skip on hit.
- **CL-073** (`:160`): wake-LLM only at empty-queue boundary.

**Implication:** A2 is largely a **code-conformance + annotation** job, not a spec-authoring job. The spec already names `kerf next --format=json --only=bead`, `harmonik queue append`, the guard order, and the wake boundary. What A2 must add/clarify in the *spec* is narrow: (a) make CL-051 an explicit *harness obligation with a definite done-condition* the implementer can test against (it reads as a definition today, not an obligation on a named producer), and (b) ensure CL-070..073 cross-link cleanly to EM-062..065 (see F5). The bulk of the work the attached beads describe (hk-3ix6o, hk-dg42b) is **implementation against an already-written spec**, surfaced in 07-tasks, not new normative text.

### F2 — The Pi harness shells out to only `subscribe` + `digest` (both read-only); the two-phase-done check is absent in `bridge.ts`

Confirmed against live `.pi/extensions/flywheel/`:
- A grep of the harness for harmonik subcommands finds only `harmonik subscribe` and `harmonik digest`. **No `queue submit`, no `queue append`, no `kerf next`, no `git log origin/main` call.** This is the S5/S10 zero-percent-Pi-driven gap, exactly as the assessment states.
- **`bridge.ts` deterministic-done path (assessment anchor `:253-258`, verified):** `if (tier === "deterministic") { processedEvent(event.event_id, ...); emitCognitionEvent(...); return; }` — it advances the watermark and emits a reaction event with **no git-trailer check**. So a `run_completed` (or any deterministic-tier event) marks the reaction processed without verifying CL-051 Condition 2. This is the literal S8 verification hole: a push-failed run looks done.
- **`index.ts:343` `buildMinimalDigest`** is a `TODO(v1)` stub returning placeholder text instead of shelling to `harmonik digest --json` (assessment anchor, verified) — the hk-ytj2r gap; relevant to A2 only as post-recycle digest blindness (G2 support), not the dispatch contract itself.

### F3 — The CL-013 mechanism/cognition line is ALREADY drawn for eager-refill, byte-clean, and the spec is explicit that refill MUST NOT consult the model

`cognition-loop.md:88` (CL-013) and `:177` (CL-INV-001) make the split normative and "byte-clean"; `:153` (CL-071) states *"Eager refill MUST NOT consult the model"* and `:160` (CL-073) puts model-wake exactly at the empty-queue boundary; `:199` restates the invariant ("Empty-queue boundary wakes the model exactly once per boundary; subsequent slot-free events refill via deterministic path without waking"). So constraint C1 (keep the line byte-clean) is **already satisfied in the spec** — A2 must not blur it when annotating the concrete call-site, and the harness implementation (hk-dg42b) must place `kerf next`/`queue append` in mechanism code with no model round-trip. There is no spec conflict here; the risk is purely an implementation-discipline one carried into 07-tasks.

### F4 — CL-080 names the verb `harmonik supervise pause/resume`; the code comments reference the same shape but no such verb is wired; CL-090 also points operators at `harmonik supervise resume`

- **CL-080** (`:163`): *"Lifecycle via `harmonik supervise`. Commands flow through `harmonik supervise start/stop/status/pause/resume` per PL §4.9 + ON §4.3. Loop MUST honor daemon `paused`/`pausing` states by not initiating new dispatch."* — so the spec's canonical form is **`harmonik supervise pause/resume`** (resolving OQ1 in favor of CL-080's existing wording; the assessment's `harmonik pause/resume` is the non-canonical alias).
- **CL-090** (`:169`): *"Reset NOT automatic; operator MUST `harmonik supervise resume`."* — reinforces the `supervise resume` form.
- **Code reality:** `supervise_cmd.go` ships only `start, stop, status, attach, restart, logs, _shim` (verified; default-case error string literally lists `start, stop, status, attach, restart, logs` — **no pause/resume**). The `budget.ts:8` / `circuit-breaker.ts:5` comments tell the operator to run `harmonik supervise resume`, a command that does not exist. So the verb is **spec-named (CL-080), code-promised (comments), but unwired.**

**Implication for A2:** the naming reconciliation is trivial in the spec (CL-080 already wins; OQ1 resolved to `supervise pause/resume`). A2's spec change is to (a) confirm CL-080 as the single canonical form, (b) note the stale `budget.ts`/`circuit-breaker.ts` comments as correct-in-intent-but-currently-dangling (they describe a verb the `pilot`/`reap` work will wire), and (c) the *verb + producer* itself is owned by A3 (operator-nfr); A2 only references the name.

### F5 — execution-model §4.13/§4.14 (EM-062..065) and CL-070..073 are LAYERED, not duplicated — and the layering is already coherent (must not break it)

The 2026-05-30 EM spec-bundle (changelog `:1741`) landed the **daemon-side** refill/guard contract; CL-070..073 are the **harness/loop-side** of the same dance. They cross-reference cleanly:
- **EM-062** (`:945`) — daemon eager-refill trigger/compute: `available = max_concurrent - in_flight_count`, deficit-based pull from `kerf next` x2 OVERFETCH_FACTOR, `queue_append` survivors, fires AFTER terminal-event processing. CL-071 is the loop-side mirror naming the same `kerf next`/`queue append` calls.
- **EM-063** (`:990`) — two-phase pre-screen (queue.json Phase 1, `git log origin/main` Phase 2); CL-072's guards are the loop-side pre-screen.
- **EM-064/065** (`:990,:1004`) — read-order authority chain (queue.json -> origin/main -> Beads -> events.jsonl) + "submit/append targets the active stream group; no double-queue."
- **OQ-CL-001** (`:202`) already flags the one genuine ambiguity: when `kerf next` and the in-memory queue.json view disagree, *"queue.json wins; kerf advisory"* — which is consistent with EM-064 tier-1 (queue.json first).

**Implication:** A2 must **not re-specify** the refill mechanism (EM owns the daemon side, N4 forbids new queue primitives). A2's CL edits must *cite* EM-062..065 as the daemon-side counterpart and keep the harness-side CL-070..073 as the orchestrator obligation. The `queue-model.md §6` cross-ref (`:212` — *"Consumes `queue-submit` and `queue-append` per CL-071; no new queue-method surface required"*) confirms A2 introduces **no new queue surface** — it wires Pi to call the existing one.

## Patterns to Follow

- **Annotate, don't re-author.** CL-051/070..073 are already normative; A2 converts CL-051 from a *definition* into an explicit *harness obligation with a testable done-condition* and tightens cross-links to EM-062..065. Resist rewriting working normative text (project spec-first + "don't add abstraction" directives).
- **Cite the daemon-side counterpart.** Every CL-07x edit references its EM-06x mirror so the harness/daemon split stays legible (avoids the duplication the assessment's snapshot implied).
- **CL-080 wins on the verb name** (OQ1 -> `harmonik supervise pause/resume`); defer the verb's *producer* definition to A3.
- **Keep the mechanism/cognition line byte-clean (C1, CL-INV-001).** The concrete `kerf next`/`queue append` call-site lives in mechanism; only the empty-queue wake is cognition.
- **Conformance scenarios** follow cognition-loop's §"acceptance/invariants" prose form (`:199` style): SC2 ("slot frees -> harness appends next ranked non-skipped bead via `queue append` without waking the model") and SC4 ("completion event arrives but trailer absent on origin/main -> bead NOT marked done") attach to CL-071 and CL-051 respectively.

## Risks / Conflicts

- **R1 (assessment-snapshot drift).** The assessment doc (2026-05-30) and the decomposition speak of CL-070..073 as "spec-only with no harness code" as if the spec needed writing. The spec is in fact already written and normative (post-bundle hk-j7o3i). Change-design must treat A2 as **mostly an implementation-tasking + annotation** pass, or it will redundantly re-draft normative text and risk renumbering working CL IDs. **This reframing should be surfaced to the change-design author explicitly.**
- **R2 (two-phase-done routing edge).** CL-051's two single-condition windows are asymmetric: event-only -> in-flight, re-poll; trailer-only -> `loop_observed_phantom_done` -> Tier-2 (RC §4.4). The harness must NOT collapse them. `bridge.ts`'s current single-`processedEvent` path has no notion of the converse window; the implementation (hk-3ix6o) is non-trivial and crosses into reconciliation territory (RC §4.4) — but A2 stays loop-side and only *routes* to RC, never acts (CL-050, N3). Confirm the `reconciliation/spec.md §4.4` target still resolves before finalizing the cross-ref.
- **R3 (idempotency key, C2).** CL-055 (`:127`) per-reaction-class idempotency requires the eager-refill submit and the two-phase-done check each be keyed (`dispatch_intent:<event_id>:<bead_id>`) for effectively-once across crashes. The spec names the discipline; the harness watermark (`watermark.ts`) exists. Risk is implementation-side (carried to 07-tasks), not a spec gap.
- **R4 (stale-comment correction scope).** Fixing `budget.ts`/`circuit-breaker.ts` comments and the stale-doc bead (hk-5bw7a) touches code/docs outside the spec. A2's *spec* change is only the CL-080 confirmation; the comment fixes belong in 07-tasks tied to hk-5bw7a. Keep the spec edit and the code-comment edit in separate tasks.
- **R5 (no blocker).** OQ-CL-001 (kerf-vs-queue.json authority) is already answered (queue.json wins, EM-064 tier-1); no open question blocks the A2 change design.
