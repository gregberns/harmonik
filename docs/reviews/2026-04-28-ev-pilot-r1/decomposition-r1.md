# EV Pilot r1 — Decomposition-Quality Review

`reviewer: decomposition-quality` · `pilot: ev-pilot.md v0.1.0` · `spec: event-model.md v0.3.3 (reviewed)` · `discipline: v0.7` · `date: 2026-04-28`

Per pilot-review-protocol §3.2. Sample of 14 beads weighted toward riskier classes; full §5.7 cycle-walk re-check; pilot self-flag spot-check.

---

## 1. Summary of findings

| Severity | Count | Lanes |
|---|---|---|
| BLOCKER | 4 | 4 class, 0 local |
| MAJOR   | 4 | 4 class, 0 local |
| MINOR   | 5 | 2 class, 3 local |

**Headline:** the pilot's bead descriptions are faithful to spec body in every sampled case (zero BLOCKERs on description-vs-spec). The blockers are all in §5.7 cycle-walk: the pilot identified six §2 row predecessor patches but did NOT actually apply them in the row tables, and the walk missed at least one event-row↔§4-req cycle (`ev-002c ↔ ev-events.daemon-degraded`). Sensor-predecessor lists are systematically thin: the v0.7 §2.5 source #1 (conformance-group prose) names contiguous ranges (`EV-001..EV-008`, `EV-015..EV-020`, etc.) and the rule says the sensor blocks-on EACH req in the cited range — the pilot only emits 1-2 predecessors per group.

---

## 2. Per-bead findings (sampled)

### 2.1 §2.3 coalesces (both)

#### `ev-016` (EV-016 + EV-016a coalesce) — clean

- **Description vs spec:** faithful. Captures EV-016 (durability class declaration + per-class fsync) and EV-016a (per-event-fsync; no multi-event atomicity guarantee). Both clauses inseparable since they describe the same Append/Emit code path.
- **Coalesce soundness (§2.3 three-AND test):**
  - (1) Single code path? Yes — both rules constrain the JSONL writer's fsync logic on `Append`.
  - (2) Anchor-and-clarification? Yes — EV-016 declares the rule; EV-016a is an explicit disclaimer ("the bus does NOT guarantee atomicity across multiple boundary events").
  - (3) Splitting reduces to "see anchor"? Yes.
- **Verdict:** sound coalesce. No finding.

#### `ev-023` (EV-023 + EV-023a coalesce) — clean

- **Description vs spec:** faithful. Captures EV-023 three-step detector obligation (post-crash window determination, flag set, git/Beads corroboration) and EV-023a evidence classification (`git-corroborated | beads-corroborated | inconclusive`). Boundary-events-fall-to-inconclusive sub-rule preserved.
- **Coalesce soundness (§2.3):** (1) Yes — same detector classifier function. (2) Yes — EV-023 declares the read protocol; EV-023a refines the evidence-classification step. (3) Yes.
- **Verdict:** sound coalesce. No finding.

### 2.2 Multi-step / F8b cases (both)

#### `ev-011a` — F8b shared-function-body resolution — **borderline**

The pilot's §1 calls out EV-011a's four-mechanism body (shed-by-class, spill files, `bus_overflow` reservation, direct-append fallback) as passing §2.2's three-signal test on surface read but resolving via F8b.

- **§2.2 signal check:**
  - (1) ≥3 steps: technically yes (4 sub-mechanisms), but they are not a NUMBERED step list — they are four behavioral clauses inside one prose paragraph.
  - (2) Independently testable: yes — shed-by-class, spill-file creation, reservation-slot consumption, fallback-to-direct-append are each separately injectable in a test harness.
  - (3) Umbrella loses meaning when stripped: borderline — the umbrella ("non-blocking back-pressure") is itself meaningful as a single contract; an implementer can derive the four mechanisms from the body without enumerated steps.
- **F8b applicability (v0.7 behavioural-spec extension):** the v0.7 example (EM-016's three git ops sitting in one cohesive function body) is the precedent. EV-011a's four mechanisms similarly cohabit one `EnqueueAsync` function body with conditional branches — F8b applies.
- **Verdict:** sound F8b application. No finding.

#### `ev-023` three-step detector — F8b resolution — clean

The three detector steps (post-crash window determination, flag set, corroboration against git/Beads) sit inside one detector classifier function body, exactly the EM-038 six-category-validator shape that F8b cites as a worked example.

- **Verdict:** sound F8b application. No finding.

### 2.3 Sensors (all 6) — systematically under-emitting predecessors

Per discipline §2.5 v0.7, sensor predecessors derive from FOUR §10.2 sources; source #1 (conformance-group prose) when §10.2 names a contiguous range by group label, the sensor blocks-on **EACH** req in the cited range. The pilot under-applies source #1.

#### **F-decomp-EV-1 [BLOCKER, class]** — Sensor predecessor lists omit reqs from cited §10.2 conformance groups

**Spec §10.2 group cites:**

- "EV-001 — EV-008 (envelope and ordering)" — 11 reqs (`EV-001`, `EV-002`, `EV-002a`, `EV-002b`, `EV-002c`, `EV-003`, `EV-004`, `EV-005`, `EV-006`, `EV-007`, `EV-008`).
- "EV-009 — EV-014b (bus and consumer taxonomy)" — 9 reqs (`EV-009`, `EV-010`, `EV-011`, `EV-011a`, `EV-012`, `EV-013`, `EV-014`, `EV-014a`, `EV-014b`).
- "EV-015 — EV-020 (durability)" — 7 reqs (`EV-015`, `EV-016`, `EV-016a`, `EV-017`, `EV-018`, `EV-019`, `EV-019a`, `EV-020`).
- "EV-021 — EV-024 (replay semantics)" — 4 reqs (`EV-021`, `EV-022`, `EV-023`, `EV-024`).
- "EV-035 — EV-036 (redaction)" — 2 reqs.

**Pilot's actual predecessor lists:**

| Sensor | §10.2 group source | Spec range | Pilot predecessors | Missing |
|---|---|---|---|---|
| `ev-inv-001` | EV-021..EV-024 | EV-021, EV-022, EV-023, EV-024 | EV-021, EV-022 | EV-023, EV-024 |
| `ev-inv-002` | EV-015..EV-020 | EV-015, EV-016, EV-016a, EV-017, EV-018, EV-019, EV-019a, EV-020 | EV-014b, EV-014d, EV-018, EV-016, EV-017 | EV-015, EV-019, EV-019a, EV-020 |
| `ev-inv-003` | EV-009..EV-014b | EV-009..EV-014b (9 reqs) | EV-010, EV-014 | EV-009, EV-011, EV-011a, EV-012, EV-013, EV-014a, EV-014b |
| `ev-inv-004` | EV-001..EV-008 | EV-001..EV-008 (11 reqs) | EV-002, EV-002a, EV-002b, EV-002c | EV-001, EV-003, EV-004, EV-005, EV-006, EV-007, EV-008 |
| `ev-inv-005` | only EV-031 (single-req group) | EV-031 | EV-031 | (none — single-req group) |
| `ev-inv-006` | EV-035..EV-036 | EV-035, EV-036 | EV-035, EV-036 | (none — both covered) |

`ev-inv-001`, `ev-inv-002`, `ev-inv-003`, `ev-inv-004` are all MISSING predecessors that the v0.7 §2.5 source #1 mechanical rule requires.

**Severity: BLOCKER.** Under-gating breaks readiness-workflow ordering: the sensor's `br ready` enables prematurely if ANY of the missing predecessors lands but a still-unimplemented req in the same conformance group is unfinished.

**Lane: class.** The discipline §2.5 source #1 rule is mechanical and unambiguous; this is the same rule EM applied with full coverage (per EM r1). The pilot misapplied a clear rule across all 4 ranges — that's an application error in this pilot, but the systematic recurrence (4 of 6 sensors) suggests a discipline-side under-emphasis (the worked example in §2.5 cites only 4 reqs in a 4-req range; a richer worked example showing a sensor with 9 predecessors from a contiguous group would have prevented this). **Recommended discipline patch:** §2.5 source #1 worked example expanded to show a longer contiguous range.

### 2.4 Schema beads (4 sampled)

#### `ev-schema.event` — clean

- **Field list:** 10 fields (event_id, schema_version, type, timestamp_wall, timestamp_mono_nsec, run_id, state_id, source_subsystem, trace_context, payload). Matches §6.1 RECORD declaration exactly.
- **Description vs spec:** faithful. Cites EV-002 (UUIDv7), EV-004 (Go-package-id source_subsystem), §6.3 registry decoding.
- **Verdict:** clean.

#### `ev-schema.subscription` — clean

- **Field list:** 7 fields matching §6.1 RECORD (consumer_id, consumer_class, event_pattern, since, offset_checkpoint_event_id, on_panic, handler).
- **Description vs spec:** faithful. Cites EV-010/011/012 (consumer-class enum), OQ-EV-007 default `recover_and_log`.
- **Verdict:** clean.

#### `ev-schema.event-bus` — clean

- **Method list:** 6 methods (Emit, Subscribe, Seal, ReplayFrom, DeadLetterReplay, Drain). Matches §6.1 INTERFACE.
- **Note** captured `on_tail_truncation` callback per §6.1 trailing prose.
- **Verdict:** clean.

#### `ev-schema.jsonl-line` — primitive-shape, clean

- **Description vs spec:** faithful. Captures all 5 read-recovery rules (torn-tail, mid-file corruption, empty-log, concurrent-tailing, post-fsync tail). Captures dead-letter format and spill-file format.
- **Tag:** `kind:primitive-shape` per F-pilot-AR-3 — appropriate for a constrained file-format declaration.
- **Verdict:** clean.

### 2.5 §8 event-row beads (78-row taxonomy)

Per discipline §2.11(c), large taxonomies → one bead per category. EV's §8 has 78 rows under 8 thematic sub-tables; pilot mints 1 umbrella + 78 children. **§2.11(c) treatment is correct.**

The §4a "canonical edges per row" pattern (each row blocks-on `ev-032`, `ev-034`, `ev-031`, `ev-016`, plus row-specific edges) is mechanical and faithful to discipline.

#### `ev-events.run-failed` — clean

- **Description (per template):** captures durability class F, emitter orchestrator-core, payload fields per §8.1.3 + §6.3 (run_id, terminal_state_id, failure_class, error_category, ended_at, reason).
- **Edges:** `em-015b` (co-owner per §6.5), `em-error.taxonomy` (FailureClass), `forward:hc-error-category` (HC §4.5 ErrorCategory). All three correctly traced.
- **Verdict:** clean.

#### `ev-events.daemon-degraded` (§8.7.5) — **see F-decomp-EV-3 below for cycle**

- Description (per template) and 6-value `reason` enum captured correctly (rto_breach / reconstruction_notify / clock_regression / cat0_post_ready / infrastructure_unavailable / silent_hang_aggregate).
- Row-specific edges `ev-002c`, `forward:rc-012a`, `forward:on-040`, `forward:pl-degraded` traced correctly to inline rationales.
- **Cycle issue separately captured below.**

### 2.6 Random first-class beads (4 sampled)

#### `ev-001` (Envelope) — minor description gap; cycle issue

- **Description vs spec:** faithful — 10 fields enumerated; "exactly one wall-clock read per emission" preserved.
- **Edges:** `ev-schema.event` (forward to schema bead), `ev-002`, `ev-034a`, `ar-001`. Pilot's note correctly reclassifies `[architecture.md §4.5]` cite as supporting (no edge to AR-018), but the actual EDGE TO `ar-018` was AR-018 = "Reconciliation is not a subsystem", not the subsystem-realization rule the pilot's note describes. **Minor reasoning bug** — discussed in F-decomp-EV-2 below; in any case the supporting-cite reclassification is the right outcome.
- **Cycle:** `ev-001 → ev-034a` is wrong-direction per §5.7 #1 — pilot identified the patch ("drop `ev-034a` from `ev-001`'s predecessor list") but the §2 row table was NOT actually patched. The row still shows `ev-034a`. See F-decomp-EV-3.

#### `ev-027` (Amendment protocol) — **wrong AR target**

- **Description vs spec:** faithful — captures both addition and deletion amendments, three artifacts of addition, retired-id-burned rule.
- **Edges:** Pilot emits `ev-027 → ar-019`. **This is wrong:** AR-019 = "Post-MVH process geometry is out of foundation scope". The amendment-protocol procedure is owned by **AR-020** ("Amendment-proposal procedure") at AR §4.6.AR-020. The §4.6 section heading is "Foundation amendment protocol" and AR-019 is the LAST requirement in §4.5 ("Subsystem runtime realization — MVH pin"), not the first in §4.6.

**Severity: MAJOR.** Edge target is the wrong bead; downstream gating mis-orders.

**Lane: local.** Discipline §3.1 step 5 (term-use) and step 3 (section-anchor disambiguation) are clear; the pilot picked the wrong req inside AR §4.6. Any reviewer reading AR §4.6 directly catches this. Patch: change `ev-027 → ar-019` to `ev-027 → ar-020`.

#### **F-decomp-EV-2 [MAJOR, local]** — Wrong AR target on `ev-027`

#### `ev-031` (Tagging obligation) — clean (with caveat already self-flagged)

- **Description vs spec:** faithful — four-axis + mechanism/cognition + durability_class.
- **Edges:** `ar-001` (four-axis classification), `ar-005` (mechanism/cognition tag — pilot's term-use binding to current canonical owner; the spec text "per SC-10" is stale, captured by F-pilot-EV-5).
- **Verdict:** clean. Pilot's F-pilot-EV-5 (local lane, spec-edit TODO) is appropriate.

#### `ev-008` (Partial-order contract) — clean

- **Description vs spec:** faithful, including `trace_context.parent_event_id` and `triggering_event_id on hook_fired` cite. 
- **Edges:** `ev-002`, `ev-002a`, `ev-003`, `ev-schema.trace-context`. The `hook_fired` term-use → edge to `ev-events.hook-fired` per §3.1 step 5 (pilot says "edge to `ev-events.hook-fired`" in notes but doesn't list it in the blocks-edges column). Minor MIN: row's predecessor list omits the explicit `ev-events.hook-fired` it claims in notes.

#### **F-decomp-EV-7 [MINOR, local]** — `ev-008` notes name `ev-events.hook-fired` predecessor that's not in its blocks-edges column

---

## 3. §5.7 cycle-walk re-check (CRITICAL per protocol)

Per protocol step 5, re-check the bidirectional-cycle walk: pilot claims 17 candidate pairs walked, 11 F13 resolutions + 1 F-pilot-AR-10 + 5 no-cycle. EM's r2 missed 8 cycles at load time; spot-check at least 3 F13 resolutions against the source spec.

### 3.1 F13 resolutions spot-checked

#### Walk #1: `ev-001 ↔ ev-034a`

**Spec inline cites:**
- EV-001 body: cites `[architecture.md §4.5]` for source_subsystem field type. Does NOT inline-cite EV-034a.
- EV-034a body: "Each subsystem MUST register its source_subsystem identifier at daemon init...". Does NOT inline-cite EV-001.

**Pilot framing:** "EV-001 declares the envelope (with `source_subsystem` field) and EV-034a registers `source_subsystem` identifiers at startup. Both cite each other implicitly via the registration-of-envelope-field pattern."

**Verdict:** the pilot's "implicit" framing is non-standard. The bidirectional-cite rule (§2.7 / F13) tests INLINE cites, not "implicit" coupling. There is no bidirectional INLINE cite here. The pilot's PATCH (drop `ev-034a` from `ev-001` predecessors) yields the right end state, but for the wrong reason: the correct reason is "EV-001 does not inline-cite EV-034a → no edge in that direction at all" (§3.1 step 1). **The pilot's §2 row table still has `ev-034a` in `ev-001`'s predecessors and was not patched.**

#### Walk #2: `ev-035 ↔ ev-036`

**Spec inline cites:**
- EV-035 body: "the compile-time check (EV-036) is the structural guardrail" → INLINE cite.
- EV-036 body: does NOT inline-cite EV-035.

**Verdict:** ONE-WAY (EV-035 → EV-036), not bidirectional. The pilot lists this as a bidirectional pair and applies F-pilot-AR-10 supporting-cite test to both directions. The end state (no edge between them, both directions) is defensible under supporting-cite (EV-035's main claim is independently testable; EV-036's mention is supplemental). But the pilot's framing as "both citing each other" is wrong per spec body.

#### Walk #5: `ev-014b ↔ ev-018`

**Spec inline cites:**
- EV-014b body: "This obligation pairs with the producer idempotency of EV-018..." → INLINE cite.
- EV-018 body: does NOT inline-cite EV-014b.
- EV-017 body DOES inline-cite EV-014b ("Consumers MUST be coded against tail truncation (EV-014b)").

**Verdict:** the pilot says "EV-018 explicitly cites EV-014b in body" — **factually wrong.** The reverse cite is in EV-017, not EV-018. The actual relationship is one-way (EV-014b → EV-018). The pilot's claim about a bidirectional pair is based on a misreading.

The pilot's PATCH (drop `ev-018` from `ev-014b` predecessors) is defensible under F-pilot-AR-10 supporting-cite (EV-014b's main claim about consumer-side tail-truncation handling is independently testable; EV-018 is mentioned for context). But the §2 row table for `ev-014b` was NOT actually patched — it still shows `ev-018` as a predecessor.

### 3.2 Walks NOT applied to row tables — F-decomp-EV-3 [BLOCKER]

The §5.7 walk identifies SIX patches the pilot claims to apply to §2 rows:

1. (#1) drop `ev-034a` from `ev-001` predecessors.
2. (#5) drop `ev-018` from `ev-014b` predecessors.
3. (#15) drop `ev-events.taxonomy` from `ev-016`, `ev-025`, `ev-026`, `ev-027`, `ev-031`, `ev-033`, `ev-036` predecessors.

**Spot-check the actual §2 row table:**

- `ev-001` row's blocks-edges column: `ev-schema.event, ev-002, ev-034a, ar-001` → **STILL CONTAINS `ev-034a`**. Patch #1 NOT applied.
- `ev-014b` row's blocks-edges column: `ev-018, ev-schema.event-bus` → **STILL CONTAINS `ev-018`**. Patch #5 NOT applied.
- `ev-016` row's blocks-edges column: `ev-015, ev-events.taxonomy` → **STILL CONTAINS `ev-events.taxonomy`**. Patch #15 NOT applied.
- `ev-025` row's blocks-edges column: `ev-events.taxonomy` → **STILL CONTAINS**. Patch #15 NOT applied.
- `ev-026` row's blocks-edges column: `ev-events.taxonomy` → **STILL CONTAINS**. Patch #15 NOT applied.
- `ev-027` row's blocks-edges column: `ev-events.taxonomy, ar-019` → **STILL CONTAINS**. Patch #15 NOT applied.
- `ev-031` row's blocks-edges column: `ev-events.taxonomy, ar-001, ar-005` → **STILL CONTAINS**. Patch #15 NOT applied.
- `ev-033` row's blocks-edges column: `ev-032, ev-events.taxonomy` → **STILL CONTAINS**. Patch #15 NOT applied.
- `ev-036` row's blocks-edges column: `ev-034, ev-events.taxonomy` → **STILL CONTAINS**. Patch #15 NOT applied.

#### **F-decomp-EV-3 [BLOCKER, local]** — §5.7 cycle-walk identifies 9 row patches that were never applied to the §2 row table

**Severity: BLOCKER.** With the patches not applied, `br dep cycles` at load surfaces (at least) the umbrella cycle `ev-events.taxonomy ↔ ev-016` and (separately) `ev-001 ↔ ev-034a` (no — that one's not actually bidirectional, but the un-patched `ev-001 → ev-034a` produces a bidirectional with `ev-034a → ev-004 → ev-001` chain? Need to verify; in any case the umbrella ↔ §4-req cycles WILL fire). Specifically:

- `ev-events.taxonomy → ev-016` (per pilot's umbrella description) AND `ev-016 → ev-events.taxonomy` (per §2 row) → cycle.
- Same for ev-025, ev-026, ev-027, ev-031, ev-033, ev-036 (six umbrella-cycle pairs un-resolved).

**Lane: local.** The walk identified the patches correctly; pilot author simply forgot to apply them in the actual row table. Same kind of bug as EM r2's "8 cycles missed at load." The discipline doesn't need to change; the pilot needs a final pass to align §2 / §3 / §4 / §4a row tables with §5.7's identified patches.

**Recommended action:** patch ev-pilot.md v0.1.0 → v0.1.1 with the nine row-table edits before loading.

### 3.3 Genuinely missed cycle — F-decomp-EV-4 [BLOCKER]

The pilot's §5.7 walk did not analyze every event-row ↔ §4-req pair. Concretely:

#### **`ev-002c ↔ ev-events.daemon-degraded`** — un-walked, genuine cycle

- **EV-002c body:** "If the wall clock is behind the HWM by more than 1 second the daemon MUST emit `daemon_degraded{reason=clock_regression}`..." → term-use of §8.7.5 row (the pilot's §2 row for `ev-002c` correctly lists `ev-events.daemon-degraded` as a predecessor).
- **§8.7.5 row spec body / §6.3 daemon_degraded payload:** the `reason` enum names `clock_regression` carve-out per EV-002c. The §4a row table for `ev-events.daemon-degraded` lists `ev-002c` as a row-specific edge (predecessor).
- **Result:** `ev-002c → ev-events.daemon-degraded` AND `ev-events.daemon-degraded → ev-002c`. **Genuine cycle.** Not in pilot's §5.7 walk list.

**Resolution per F13 slot-rule:** the §8 row IS the slot (the event taxonomy entry); EV-002c is the rule that fires the event. Slot → content normative direction → row blocks-on EV-002c (the rule provides the load-bearing input — clock_regression behavior — to the row); reverse not emitted. **Patch: drop `ev-events.daemon-degraded` from `ev-002c` predecessors.**

**Severity: BLOCKER.** Will fire at `br dep cycles` load.

**Lane: local.** Pilot should have walked all event-row ↔ §4-req bidirectional candidates exhaustively (the walk's stated goal). The discipline rules are clear; pilot just didn't enumerate this pair.

#### **F-decomp-EV-4 [BLOCKER, local]** — Missed bidirectional cycle `ev-002c ↔ ev-events.daemon-degraded` not in §5.7 walk

There may be other un-walked cycles of similar shape (EV-008 → `ev-events.hook-fired`? — no, hook-fired row doesn't reverse-cite EV-008; clean. EV-INV-002 → EV-014d → ...? — non-bidirectional. EV-018 → consumers? — non-bidirectional.). The pilot's claim of "17 candidate pairs walked" is under-walked: the universe of (§4 req × §8 row) bidirectional candidates is larger than 17.

---

## 4. Missing-coalesce smell

Looking at clusters of related reqs that the pilot did NOT coalesce:

- **EV-002 / EV-002a / EV-002b / EV-002c** (UUIDv7 family — 4 reqs). Pilot kept separate. Per §2.3 three-AND test:
  - (1) Single code path? No — different mechanisms (algorithm choice, intra-process counter, subprocess routing, persistent file).
  - (2) Anchor-and-clarification? Partial — EV-002 declares UUIDv7, the others extend.
  - (3) Splitting reduces to "see anchor"? No — each has independent test surface (RFC 9562 method choice; counter tiebreaker test; handler-routing test; HWM persistence test).
  - **Verdict:** correctly NOT coalesced (test 1 fails).

- **EV-014 / EV-014a / EV-014b / EV-014c / EV-014d** (bus mechanics — 5 reqs). Pilot kept separate.
  - Each is a different bus mechanism (cardinality enforcement, dispatch ordering, consumer idempotency, observer goroutine model, replay contract). Different code paths.
  - **Verdict:** correctly NOT coalesced.

- **EV-019 / EV-019a** (panic flush — 2 reqs). Pilot kept separate. EV-019 = MUST log flush; EV-019a = SHOULD bus flush. Different obligation tier (MUST vs SHOULD), arguably same panic recovery handler code path.
  - Per §2.3 test (1), arguably single code path; per (2), anchor-and-extension structure.
  - **Verdict:** borderline. Defensible to keep separate (different MUST/SHOULD tier signals different test approaches).

- **EV-035 / EV-036** (redaction — 2 reqs). Pilot kept separate. EV-035 = runtime redaction registry; EV-036 = compile-time check. Different code paths (runtime vs static).
  - **Verdict:** correctly NOT coalesced.

**No missing-coalesce smell.**

---

## 5. Over-split smell

- Pilot has zero step beads (no §2.2 multi-step splits). EV-011a and EV-023 both correctly resolve to single beads via F8b.
- §6.4 breaking-change classification (9 rows of an evolution table) is correctly inlined in the EV-030 bead description, not split into 9 sub-beads.
- §6.5 co-ownership map is correctly NOT a separate bead (annotation on §8 row beads per §2.11(d)).
- §6.3 per-type payloads correctly absorbed into §8 row beads (per F-pilot-EV-1 — class flag).

**No over-split smell.**

---

## 6. Pilot self-flag accuracy

| Flag | Pilot lane | Reviewer assessment | Notes |
|---|---|---|---|
| F-pilot-EV-1 (§6.3 payload + §8 row absorption) | class | **agree, class** | §2.6 and §2.11(c) intersection is genuinely under-codified; the pilot's policy is sound and prevents 14 redundant `ev-schema.<event-type>-payload` beads. Discipline patch recommended. |
| F-pilot-EV-2 (post-mvh tag for §8.6.14) | class | **agree, class** | The transient-tag analog of `gated-by-spec-edit` and `gated-by-corpus-scale` is the right shape. Discipline §2.8 / §2.11 should grow a `post-mvh` clause. |
| F-pilot-EV-3 (forward cites to non-depends-on specs) | class | **agree, class** (already resolved as Option B carry-forward) | Same posture as F-pilot-EM-2; no new finding. |
| F-pilot-EV-4 (§4.a same-day boundary case) | class | **agree, class** | The grandfather carve-out language is silent on same-day specs. EV's `reviewed` gate landed the same day as AR-053; defensible to extend the carve-out. Discipline patch needed (Option C — "same-day carve-out extension"). |
| F-pilot-EV-5 (stale "SC-10" cite) | local | **agree, local** | Spec-edit TODO; pilot's term-use binding to AR-005 is mechanical and correct. |
| F-pilot-EV-6 (corpus extrapolation update) | class | **agree, class** | Observational; informational discipline note. |
| F-pilot-EV-7 (extend §2.11(d.1) to event-row beads) | class | **agree, class** | The dual-co-owned-event pattern is structurally identical to the trailer-registry row dual-ownership; cross-reference clause warranted. |

All seven self-flags are correctly classified. **No finding.**

---

## 7. Other findings

#### **F-decomp-EV-5 [MINOR, local]** — `ev-001 → ar-018` reasoning bug

The `ev-001` row note says `[architecture.md §4.5]` cite anchors AR-018 ("subsystem-realization rule"). **AR-018 = "Reconciliation is not a subsystem"**, NOT a subsystem-realization rule (subsystem realization is owned by AR-016 / AR-017 in §4.5). The pilot ultimately reclassifies the cite as supporting (no edge), which is the right end state — but the framing is wrong. Pilot's §5.6 supporting-cite analysis correctly says "no edge"; the reasoning to get there, however, is based on a wrong claim about what AR-018 owns.

**Recommended fix:** change the `ev-001` notes column to "AR §4.5 anchors the subsystem-runtime-realization rules (AR-016/AR-017); the source_subsystem field's Go-package-identifier shape is supplemental to EV-001's main envelope claim → supporting cite per F-pilot-AR-10. No edge."

#### **F-decomp-EV-6 [MINOR, local]** — `ev-031 → ar-005` worth verifying

Pilot binds the "per SC-10" stale cite to current AR-005 ("Every evaluation point is tagged mechanism or cognition"). Verified: AR-005 IS the canonical mechanism/cognition rule in current AR taxonomy. Term-use binding is mechanical and correct. **Pilot is right.** This is just a confirmation, not a finding.

#### **F-decomp-EV-8 [MINOR, class]** — Forward cite count discrepancy

The pilot's §5.5 "forward-cite findings" table claims `~32` cite occurrences across 5 specs. Spot-check of the §8.6 (RC-emitted, 14 rows) and §8.7 (PL/ON co-owned, 17 rows) sub-tables suggests the actual cite count is higher: each of those 31 rows has at least one forward cite to RC, ON, or PL, plus several have multiple. Rough re-count: §8.6 = 14 forward cites + §8.7 = 17 forward cites + §8.2 = 9 forward cites (CP) + §8.4 = 3 forward cites (CP) + others ≈ 50+ row-level forward cites alone, before counting §4-req-body cites (~10).

**Severity: MINOR.** Doesn't affect bead structure; affects the F-pilot-EV-3 narrative ("smaller than EM's 50+ cites"). Pilot may want to recount.

**Lane: class.** The discipline doesn't currently mandate exhaustive forward-cite enumeration in pilots; the §5.5 table is informational. Could become normative if cross-pilot edge-coordination becomes a formal step.

---

## 8. Summary table

| Finding ID | Severity | Lane | Description |
|---|---|---|---|
| F-decomp-EV-1 | BLOCKER | class | Sensor predecessor lists omit reqs from cited §10.2 conformance groups (4 of 6 sensors affected) |
| F-decomp-EV-3 | BLOCKER | local | §5.7 cycle-walk identifies 9 row patches that were never applied to §2 row table |
| F-decomp-EV-4 | BLOCKER | local | Missed bidirectional cycle `ev-002c ↔ ev-events.daemon-degraded` |
| F-decomp-EV-2 | MAJOR | local | Wrong AR target on `ev-027`: should be `ar-020` (Amendment-proposal procedure), not `ar-019` (Post-MVH process geometry) |
| F-decomp-EV-5 | MINOR | local | `ev-001` note wrongly identifies AR-018 as "subsystem-realization rule" |
| F-decomp-EV-7 | MINOR | local | `ev-008` notes name `ev-events.hook-fired` predecessor not in blocks-edges column |
| F-decomp-EV-8 | MINOR | class | Forward-cite count in §5.5 likely under-tallied |

**BLOCKERs: 3** (1 class, 2 local) — all must be fixed before load.
**MAJOR: 1** (1 local).
**MINOR: 3** (1 class, 2 local).

(F-decomp-EV-6 is a confirmation, not a finding; not counted.)

The pilot's underlying decomposition is sound — coalesces are correct, multi-step decisions correct, schema beads correct, taxonomy treatment correct. The blockers are all in the cycle-walk's gap between "patches identified" and "patches applied," plus a sensor-predecessor under-application. None of the blockers requires re-thinking the pilot's structure; they require disciplined application of patches the pilot itself has identified.

---

## 9. Recommended actions

**Pilot-patch lane (v0.1.0 → v0.1.1, before load):**

1. Apply the 9 §2 row patches identified in §5.7 (`ev-001`, `ev-014b`, `ev-016`, `ev-025`, `ev-026`, `ev-027`, `ev-031`, `ev-033`, `ev-036`).
2. Drop `ev-events.daemon-degraded` from `ev-002c` predecessors (F-decomp-EV-4).
3. Change `ev-027 → ar-019` to `ev-027 → ar-020` (F-decomp-EV-2).
4. Fix `ev-001` notes column AR-018 description (F-decomp-EV-5).
5. Add `ev-events.hook-fired` to `ev-008` blocks-edges column or remove the claim from notes (F-decomp-EV-7).
6. Optionally re-tally §5.5 forward-cite count (F-decomp-EV-8).

**Discipline-patch lane (v0.7 → v0.8, before next pilot):**

1. F-decomp-EV-1: §2.5 source #1 worked example expanded to a longer contiguous range (~9 reqs) so future pilots don't under-emit.
2. F-pilot-EV-1: §2.6 / §2.11(c) clause stating that when §8 = event-taxonomy AND §6 = envelope+selected-payload-YAML, per-type payloads live with §8 row beads.
3. F-pilot-EV-2: §2.8 / §2.11 grow a `post-mvh` transient-tag rule.
4. F-pilot-EV-4: §3.2 §4.a grandfather carve-out clarified for same-day specs (Option C).
5. F-pilot-EV-7: §2.11(d) gain a (d.2) sub-clause for event-row dual-ownership inheriting (d.1) row-level pattern.

(Patches 2-5 are documentation-tightening; F-decomp-EV-1 patch is the only behavioural change in the discipline.)

---
