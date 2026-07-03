# Spec-Draft Changelog — `pilot` (Pi-driven dispatch & control plane)

One row per drafted spec file. All four are **modified** existing specs (no new spec files, no removed specs — per 02-components.md "All requirements land as amendments/annotations to A1–A4"). Every change is additive: new requirement IDs are appended (none renumbered or retired), and all other edits are informative annotations or in-place clarifications.

| Target spec file | Status | New IDs | Change design |
|---|---|---|---|
| `specs/operator-nfr.md` | modified | ON-056, ON-057 | `04-design/operator-nfr-design.md` (A3) |
| `specs/queue-model.md` | modified | — (annotations only) | `04-design/queue-model-design.md` (A4) |
| `specs/cognition-loop.md` | modified | — (CL-051/071/080/030 amended in place) | `04-design/cognition-loop-design.md` (A2) |
| `specs/execution-model.md` | modified | EM-066, EM-067 | `04-design/execution-model-design.md` (A1) |

---

## `specs/operator-nfr.md` (A3) — v0.4.3 → v0.4.4

**Motivated by:** `04-design/operator-nfr-design.md` (T1, T2, T4). Closes G3/S9 (the producer-side half of the agent-callable pause).

- **ON-056 (new) — Agent-callable pause/resume command verb.** Exposes `pause`/`resume` over the PL-003a Unix-socket JSON-RPC transport co-located with the queue methods; canonical CLI form `harmonik supervise pause/resume`; explicit agent-callable obligation (the cognition loop per CL-080 MAY issue them without human intervention; no human-only gate). Drives the existing §7.1 `running → pausing → paused` / `paused → resuming → running` transitions with `pause_reason=operator`, inheriting ON-027 drain, ON-008 gate, ON-013 emission, ON-013c idempotency, ON-030a marker, and ON-010 reconciliation carve-out unchanged.
- **ON-057 (new) — The verb is the production `operator_pause_status` producer.** Emits the existing ON-013 `operator_pause_status`/`operator_resuming` events through the existing transitions; no new event type, no new state. The emitted `operator_pause_status` is named the **single source of pause truth** observed by both the queue consumer (QM-054) and the execution-model br-ready fallback gate (EM-067).
- **§7.1 state-machine table:** the `running | pause` and `paused | resume` rows annotated to reference ON-056 (operator-or-agent origin).
- **Conformance obligation** added for ON-056/ON-057 (pause→no-dispatch→drain→resume, no human action).
- **Front matter:** version 0.4.3 → 0.4.4; last-updated → 2026-05-31.

**Design coverage:** T1→ON-056; T2→ON-057; T4→conformance obligation. T3 (reconcile `commandcodes.go`/`budget.ts`/`circuit-breaker.ts` comments) is a 07-tasks item (hk-5bw7a), not spec text, per the design.

## `specs/queue-model.md` (A4) — v0.1.3 → v0.1.4

**Motivated by:** `04-design/queue-model-design.md` (T1, T2, T3). Annotation-only confirmation pass — no new requirement IDs, no new methods, no consumer-semantics change (constraint C4/R1).

- **§2.4 GroupKind (informative note):** Pi-driven curated dispatch uses a `stream` group (the only appendable kind); `harmonik run --beads`'s `wave` default is correct for closed batches and MUST NOT be changed to obtain appendability; cites EM-NOTE-STREAM-CONCURRENCY (stream is concurrency-safe) and EM-NOTE-WAKE (append wakes at sub-poll-interval latency). [T1]
- **§8.5 QM-054 (informative note):** confirms the `operator_pause_status` driving the `active → paused-by-drain` transition is produced in production by the operator-nfr pause/resume verb (ON-056/ON-057), with no consumer-semantics change, and is the single source of pause truth shared with EM-067. [T2]
- **§8.1 QM-050 (informative note):** confirms `queue-submit` returning `status: active` IS the queue's "start" semantics; no separate `start` method exists or is added. [T3]
- **§A.4 Changelog** entry added (v0.1.4); **front matter** version 0.1.2 → 0.1.4 (catching up the front matter to the §A.4-tracked 0.1.3 baseline), last-updated → 2026-05-31.

**Design coverage:** T1→§2.4 note; T2→§8.5 note; T3→§8.1 note. No requirement ID added (A4 is confirmation/annotation per N4).

## `specs/cognition-loop.md` (A2) — v0.1.1 → v0.1.2

**Motivated by:** `04-design/cognition-loop-design.md` (T1, T2, T3, T4, T5). The load-bearing reframing (research F1): the CL text already existed and is normative; A2 amends in place — **no CL ID renumbered or retired** (research R1).

- **CL-051 amended (T1, G4/S8):** added the explicit harness obligation (MUST NOT mark done / auto-resolve `defer` / advance past in-flight on Condition 1 alone; a deterministic-tier completion does not by itself mark done); restated the two single-condition windows as harness routing obligations; named the CL-055 idempotency key; corrected the daemon-window citation to EM-052/EM-053.
- **CL-071 annotated (T2, G2/S5/S6/S10):** concrete Pi dispatch surface — stream-group target (QM-040), first-fill via `harmonik queue submit` (submit-as-start, no separate start verb), refill via `harmonik queue append`; daemon-side mirror cross-links (EM-062/EM-063, EM-064/EM-065); reaffirmed the mechanism tag at the call-site (CL-013/CL-INV-001) and the CL-055 dispatch-intent key; resolved OQ-CL-001 (queue.json authoritative, `kerf next` advisory).
- **CL-080 confirmed (T3, G3/G5):** `harmonik supervise pause/resume` is the single canonical verb form; producer + agent-callable obligation deferred to ON-056/ON-057; added the loop-may-self-issue clause and an informative note that the `budget.ts`/`circuit-breaker.ts` comments are correct-in-intent.
- **CL-030 amended (T5, G2):** recycle-path seed digest MUST come from `harmonik digest` (substrate placeholder is non-conforming).
- **§7 acceptance scenarios (T4):** added scenario 6 (CL-071 eager-append-without-wake / SC2) and scenario 7 (CL-051 two-phase routing / SC4).
- **Front matter:** version 0.1.1 → 0.1.2; last-updated → 2026-05-31.

**Design coverage:** T1→CL-051; T2→CL-071; T3→CL-080; T4→§7 scenarios; T5→CL-030. The implementation work (hk-3ix6o two-phase-done in `bridge.ts`; hk-dg42b Pi-side curated dispatch; hk-ytj2r `buildMinimalDigest` stub; hk-5bw7a comment corrections) is surfaced in 07-tasks against this already-normative spec text.

## `specs/execution-model.md` (A1) — v0.8.0 → v0.8.1

**Motivated by:** `04-design/execution-model-design.md` (T1, T2, T3). Closes G1/S2 (the incident root mechanism) and the G3 br-ready pause gate.

- **EM-066 (new, §4.11) — No-auto-pull (queue-only) daemon topology.** Startup-sealed boolean `--no-auto-pull` (alias `--queue-only`); when set, the daemon MUST NOT fall back to `br ready` and a bare boot with no submitted queue dispatches zero runs (no `run_started`, no agent spawn, no credit); when unset, the historical `br ready` fallback is retained as an opt-in. Default is topology-scoped (ON for the supervised topology, OFF for the historical single-daemon topology). Reconciles the long-standing spec-vs-code contradiction (§7.4/§10.1 already forbade the fallback; the live binary still did it).
- **EM-067 (new, §4.11) — Operator-pause binding + defense-in-depth gate on the `br ready` fallback path.** Establishes that the §7.4 loop-top `should_pause_between_runs()` check (the ON-008 between-task drain gate) is the PRIMARY operator-pause gate covering ALL dispatch paths including the fallback — so EM-067 is NOT a new primary gate. Its load-bearing content is the **binding of the fallback path to the single source of pause truth** from ON-056/ON-057 (`operator_pause_status`, the same signal driving QM-054), guaranteeing the fallback and active-queue branches honor ONE pause concept. The inline branch gate is a belt-and-suspenders assertion (reachable under operator pause only if `should_pause_between_runs()` was scoped narrower than the full ON-008 state; vacuous in a conforming implementation). The EM-067 coherence resolution (loop-top vs. inline relationship) is recorded in 06-integration.md.
- **§7.4 main-loop pseudocode reconciled:** the `queue IS None` branch is now a flag-gated two-way branch (idle-wait under `--no-auto-pull`; operator-pause-gated `br ready` fallback otherwise).
- **§10.1 Core MVH conformance:** refined (fallback non-conforming under `--no-auto-pull`; conforming opt-in otherwise) and extended to require EM-066/EM-067. **§10.2:** added test obligations for EM-066/EM-067 (quiet-daemon zero-dispatch, historical-topology fallback, sealing, pause-gate).
- **Front matter:** version 0.5.0 (stale) → 0.8.1 (catching up to the §12-tracked 0.8.0 baseline), last-updated → 2026-05-31.

**Design coverage:** T1→EM-066 + §7.4 reconcile; T2→EM-067 + §7.4 reconcile; T3→§10.1/§10.2.

---

## Net new requirement IDs across the bundle

| Spec | New IDs | Renumbered/retired |
|---|---|---|
| `operator-nfr.md` | ON-056, ON-057 | none |
| `execution-model.md` | EM-066, EM-067 | none |
| `cognition-loop.md` | none (in-place amendments to CL-051/071/080/030 + §7 scenarios) | none |
| `queue-model.md` | none (informative annotations) | none |

**Total:** 4 new requirement IDs (ON-056, ON-057, EM-066, EM-067). No prior IDs renumbered or retired in any spec.

## Cross-reference integrity (verified)

The producer→consumer→gate→obligation chain is bidirectionally consistent across the four drafts:

- **ON-056/ON-057** (operator-nfr producer) ← referenced by → **QM-054** (queue consumer), **EM-067** (fallback gate), **CL-080** (loop obligation).
- **EM-067** (fallback gate) ← referenced by → **QM-054**, **ON-057**.
- **CL-071** (loop dispatch surface) → **QM-040** (stream append target), **EM-062/063/064/065** (daemon-side mirror).
- **CL-051** (two-phase done) → **EM-052/EM-053** (terminal-multi-step window), **reconciliation/spec.md §4.4** (Tier-2 routing — to be re-verified at Integration).

## Bead → spec-area coverage (per 02-components.md)

| Bead | Spec change (this pass) | Implementation (07-tasks) |
|---|---|---|
| hk-ry8q1 (P0) — agent-callable pause/resume + producer; gate br-ready | ON-056, ON-057 (A3); EM-067 (A1); QM-054 confirmation (A4) | wire the verb + producer + br-ready gate |
| hk-dg42b (P2) — Pi-side curated dispatch via `queue submit/append` | CL-071 annotation (A2); QM §2.4 stream note (A4) | implement the harness dispatch path |
| hk-3ix6o (P1) — CL-051 two-phase-done in `bridge.ts` | CL-051 amendment (A2) | replace `bridge.ts:253-258` with the two-phase gate |
| hk-ytj2r (P2) — replace `buildMinimalDigest` stub | CL-030 amendment (A2) | shell `harmonik digest --json` on recycle |
| hk-5bw7a (P3) — stale-doc correction | CL-080 note (A2); ON-056 canonical form (A3) | fix `commandcodes.go`/`budget.ts`/`circuit-breaker.ts` comments |

No attached bead is orphaned; every bead maps to a spec change in this pass and an implementation task for 07-tasks.

## Validation / acceptance test beads

Per the spec-draft pass requirement (scenario-test + exploratory-test bead per substantially-changed spec area). Each new EM/ON requirement and each amended CL requirement has its conformance obligation drafted inline (execution-model §10.2 EM-066/EM-067; operator-nfr ON-056/ON-057 conformance; cognition-loop §7 scenarios 6–7) to anchor those test beads.

Filed (all labeled `codename:pilot`):

| Bead | Type | Spec area |
|---|---|---|
| hk-h5lv2 | scenario-test | execution-model — quiet daemon (EM-066) + pause-gated fallback (EM-067) |
| hk-ynjnf | exploratory-test | execution-model — `--no-auto-pull`/`--queue-only` flag surface, zero-dispatch quiet boot |
| hk-95a2r | scenario-test | operator-nfr — agent-issued `harmonik supervise pause`/`resume` end-to-end (ON-056/ON-057) |
| hk-rnlxh | exploratory-test | operator-nfr — human + agent both invoke `supervise pause/resume`, agent-callable no-human-gate |
| hk-iht2w | scenario-test | cognition-loop — CL-051 two-phase-done (event-only re-poll; trailer-only → Tier-2) |
| hk-va7z2 | exploratory-test | cognition-loop — CL-071/CL-073 eager-refill via `kerf next` + `queue append` without waking the model |
