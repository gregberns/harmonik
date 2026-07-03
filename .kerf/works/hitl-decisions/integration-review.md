# hitl-decisions — Integration Pass Review (independent)

**Reviewer:** independent (did not author these artifacts) · **Date:** 2026-06-13
**Artifacts reviewed:** `SPEC.md`, `06-integration.md`, `05-specs/hitl-decisions-spec.md`, `03-components.md`, `01-problem-space.md`

---

## VERDICT: APPROVE

The work is internally consistent and faithfully assembled. Every success criterion (S1–S8) traces to a component and a change-spec section. Interfaces agree on `decision_id`, payload field names, error handling, and the §9 locked policy across all artifacts. The integration build order respects all stated data dependencies. SPEC.md is a faithful assembly — it introduces no new requirement and changes no decision (the one apparent addition, the SPEC.md §8.1 "Coverage gap" note, is a verbatim restatement of an item already present in the change-spec's source chain and the integration plan, not a new requirement).

**No P1 or P2 findings.** Three P3 (nice-to-have / clarity) findings are listed below; none block tasks-pass advance.

---

## S1–S8 Traceability table

| S | Success criterion (01-problem-space) | Component(s) (03-components) | Change-spec section(s) | Status |
|----|--------------------------------------|------------------------------|------------------------|--------|
| **S1** | Emit `decision_needed` (q+options+context+blocked) then stall cleanly | K1 (event), K2 (raise/block) | §1.1, §2 (raise), §4, §8 S1 | ✓ traced |
| **S2** | One command shows ALL open decisions from ≥2 agents | K4 (operator `list`), K3 (projection) | §2 (list), §3, §8 S2 | ✓ traced |
| **S3** | `answer` routes `decision_resolved`, unblocks originating agent | K4 (answer), K2 (wake) | §1.2, §2 (answer), §4, §8 S3 | ✓ traced |
| **S4** | Exactly-once even on re-delivered resolve (dedupe on `event_id`) | K2/K3 (dedupe) | §3, §6 N2, §8 S4 | ✓ traced |
| **S5** | Open set identical after daemon/session restart | K3 (projection replay) | §3, §8 S5 | ✓ traced |
| **S6** | No new always-on service (pure projection) | K3 | §3, §6 (C3), §8 S6 | ✓ traced |
| **S7** | (a) orphan reap, (b) restart re-establishes wait | K5 (reaper), K6 (keeper seam), K2 (re-wait) | §4, §5, §6 N9, §8 S7 | ✓ traced |
| **S8** | Idempotent resolution (twice / unknown id = no-op) | K4 (answer no-op), N3 | §5, §6 N3, §8 S8 | ✓ traced |

**No success criterion is without a home.** Components K1–K7 all map to a change-spec §7 row and a contract paragraph in `03-components.md`. K7 is correctly fenced as v1-second / out-of-band and is not on the S1–S8 critical path.

---

## Findings

### Finding 1 — P3 — S5/S7 not gated by the two validation beads (already disclosed, no fix required)
- **Issue:** The two gate beads `hk-rz4` (scenario) and `hk-1vl` (exploratory) cover S1,S2,S3,S4,S6,S8 but NOT S5 (restart durability) or the S7 *emit/re-wait* path (only the display-side orphaned-pending flag is asserted).
- **Ref:** `SPEC.md` §8.1 "Coverage gap" note; `06-integration.md` §5.2; `05-specs/hitl-decisions-spec.md` §8.1.
- **Assessment:** This is **already flagged in all three artifacts** with a concrete remediation (K3 impl bead carries an S5 restart scenario test; K5 impl bead carries an S7a+S7b reap/re-wait scenario test, minted at the tasks pass). The gap is disclosed, owned, and scheduled — not a hidden hole. No change needed at the integration pass; verify at tasks-pass that the K3 and K5 beads actually carry these scenario tests.
- **Fix:** None for integration. At tasks pass: confirm the K3 bead text includes the S5 restart scenario and the K5 bead text includes the S7a/S7b reap+re-wait scenario.

### Finding 2 — P3 — `decisions withdraw` appears in K4 contract but is an agent-side verb everywhere else
- **Issue:** `03-components.md` line 10 lists `withdraw` under **K4 (Operator surface)**: "`list` … `answer` … `show <decision_id>`, `withdraw`." But in `SPEC.md`/§2 and the change-spec, `withdraw` is exclusively an **agent-side** verb (`harmonik decisions withdraw <id> [--reason self_obsoleted]` — the agent cancels *its own* decision; `by` = agent name). The operator's withdraw path is the keeper-emitted `orphaned` withdrawal (K5), not an operator CLI verb.
- **Ref:** `03-components.md:10` (K4 row) vs `SPEC.md` §2 "Agent side" + §1.3 (`by` = agent name | "keeper").
- **Assessment:** Cosmetic placement drift in the *upstream* components table; the normative change-spec and the assembled SPEC.md are correct and consistent (withdraw = agent-self or keeper-orphan, never an operator verb). Since SPEC.md is the implementer entry point and it is correct, this does not mislead the implementer. Faithful-assembly is intact (SPEC.md did not propagate the error).
- **Fix:** Optionally tidy `03-components.md:10` to move `withdraw` out of the K4 operator row (it is a K2 agent verb + K5 keeper emission). Non-blocking.

### Finding 3 — P3 — `decisions show` daemon-op routing stated two ways (both reconcilable)
- **Issue:** `SPEC.md` §2 says `show` → `decisions-list` ("`show` = `list` filtered to one `decision_id`, **client-side**"), i.e. `show` reuses the `decisions-list` daemon op and filters in the client. The §7 K4 row says "`list`/`show`→`decisions-list`" (consistent). No contradiction, but the parenthetical "client-side" filtering vs the verb→op map ("`show`→`decisions-list`") could read as ambiguous about *where* the single-id filter happens.
- **Ref:** `SPEC.md` §2 verb-map + §7 K4 row; identical text in change-spec §2/§7.
- **Assessment:** Reconcilable on a careful read — `show` calls the `decisions-list` op (server returns the full open set) then filters to one `decision_id` client-side. Consistent across SPEC.md and change-spec. Minor clarity only.
- **Fix:** Optionally state once explicitly: "`show <id>` issues the `decisions-list` op and filters to `<id>` in the client." Non-blocking.

---

## Detailed criterion checks

### 2. Interface consistency — PASS
- **`decision_id` semantics uniform.** `decision_id` = the `decision_needed` event's own `event_id` (UUIDv7); the two terminals carry it as `payload.decision_id`. Stated identically in `SPEC.md` §1, change-spec §1, and the K3 fold keys (§3: add keyed on `event_id`, remove keyed on `payload.decision_id`). The fold's asymmetric key (add=`event_id`, remove=`payload.decision_id`) is internally correct precisely *because* `decision_id == decision_needed.event_id`. Consistent.
- **Payload field names match across §1, §3, and CLI verbs.** `decision_needed`: `question`, `options[]`, `context_link`, `blocked_agent`, `value_requested`. `decision_resolved`: `decision_id`, `chosen_option`, `value`, `resolver`. `decision_withdrawn`: `decision_id`, `reason`, `by`. CLI flags map cleanly: `raise --question/--option/--context/--from`; `answer <id> <option> --value --resolver`; `withdraw <id> --reason`. `list` renders `question · options · blocked_agent · context_link · decision_id`. No field-name divergence found.
- **Error handling uniform.** Exit-17-on-absent-socket is stated for *all* daemon-bound verbs in `SPEC.md` §2, change-spec §2, and `06-integration.md` §3/§4(d). No-op-on-unknown/terminal-id (N3) is stated for `answer`/`withdraw` in §2, §5, §6 N3, and §4(d). Uniform.
- **`wait` has NO daemon op** — stated consistently in §2 verb-map, §7 K2 row, and `06-integration.md` §2.3 (client-side subscribe stream reusing the existing subscribe op). Consistent.

### 3. No contradictions — PASS
- **Build order respects dependencies.** `06-integration.md` §2 order (K1 → K3 → {K2,K4} → {K5,K6}; K7 out-of-band) matches the dependency DAG embedded in the change-spec §3/§4/§5 (projection folds K1 types; CLI emits K1 + reads K3; reaper/keeper read K3). The SPEC.md "Integration / build order" section reproduces the same DAG and numbered order verbatim. No edge is violated: K5 depends on K3 + keeper-tick (both available by step 5); K2's N8 re-projection depends on K3 (built step 2, before K2 at step 3). Consistent.
- **§9 locked policy stated consistently everywhere.** Single-human answerer + first-writer-wins-on-`decision_id` + multi-human-deferred(NG1) appears identically in: `SPEC.md` §9-resolution + N3; change-spec §6 N3 + §9 policy flag; `06-integration.md` §1.1 + §4(c); `01-problem-space.md` C7 + D-context. The "first `decision_resolved` wins, later = no-op no-second-wake" phrasing is uniform. No drift.

### 4. Integration concerns addressed — PASS
- **Init/build order:** documented in `06-integration.md` §2 with per-component dependency edges and rationale; mirrored in SPEC.md.
- **Shared state:** `events.jsonl` (single source of truth, no in-memory aggregator), daemon socket (shared transport, exit-17 failure mode), and `fsyncBoundaryEventTypes` map (shared cross-cutting config, N1) all explicitly documented in `06-integration.md` §3 and SPEC.md §9 cross-cutting.
- **Cross-component error handling:** N8 arm-then-check (K2↔K4 joint invariant) documented as a normative cross-component contract in `06-integration.md` §4(b), change-spec §4, SPEC.md §4. N3 first-writer-wins across K2/K4/K5 documented in `06-integration.md` §4(c) with the explicit per-component obligations (K2 dedupe+no-op, K4 answer no-op, K5 reap-race no-op). All present.

### 5. Faithful assembly — PASS
- SPEC.md §§1–9 are a consolidation of the change-spec §§1–9 with no new normative requirement and no changed decision. Spot-checks: §1 event schemas byte-identical in intent and field set; §4 blocked-wait + N8 ordering identical; §6 N1–N9 identical; §8 S1–S8 identical (SPEC.md adds prose gloss like "*(raise emits + agent blocks cleanly.)*" — restatement, not new requirement).
- The one item in SPEC.md not in the *narrowest* reading of the change-spec body — the §8 "Coverage gap (independent-reviewer-flagged)" callout — is **not** an assembly violation: the identical coverage-gap content is present in the change-spec source chain via `06-integration.md` §5.2 and is reproduced (not invented) in SPEC.md. It restates a known test-coverage gap; it imposes no new build requirement on the implementer (the remediation is deferred to the tasks pass per the same note). Faithful.
- D1–D4 and the §9 policy in SPEC.md match `01-problem-space.md` and the change-spec exactly.

---

## Summary

APPROVE. Full traceability S1→component→spec-section with no orphaned criterion; interfaces agree on `decision_id`, payload fields, exit-17, and no-op-on-unknown; build order honors every dependency; §9 single-human/first-writer-wins is uniform; SPEC.md is a faithful assembly adding no requirement. Zero P1/P2. Three P3 clarity items (disclosed S5/S7 gate gap with scheduled remediation; `withdraw` mis-placed in the K4 row of the upstream components table only; `show` client-side-filter phrasing) — none block the tasks pass.
