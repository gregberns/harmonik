# Pass-4 Change-Design Review — phase-3-dot

**Reviewed:** 2026-05-23
**Round:** 1
**Verdict:** REQUEST_CHANGES

The five component designs are substantively strong, internally well-reasoned, and respect the seven landed D-decisions almost everywhere. They fail at the seams: terminal-node-ID spelling differs between C1 and C5; the D5 dialect grammar is violated by C5's iteration-count edge condition; and C2's handler_ref obligations contradict EM-007 in a way that mis-cites the existing spec rather than honestly surfacing the amendment as a pass-5 follow-up. None of these are deep design errors — every finding is a fixable, localized text-level inconsistency. But each is load-bearing for pass-5 (the prose writer would propagate the contradiction), so they get caught here.

## Per-component scorecard

| Component | Current state | Target state | Rationale | Traceability | Notes |
|-----------|---------------|--------------|-----------|--------------|-------|
| C1 — workflow-graph.md (new) | ✓ | ✓ | ✓ | ✓ | Strong; commits D9–D12 cleanly; §5 open items honestly surfaced. |
| C2 — execution-model.md §dot | ✓ | ✓ | ✓ | ✓ | Strong on binding-doc framing; weak on handler_ref / EM-007 reconciliation (F3). |
| C3 — handler-contract.md §Outcome | ✓ | ✓ | ✓ | ✓ | Correctly catches the EM-046c→EM-042a brief error (OQ-C3-3). Per-node-type emission table well-shaped. |
| C4 — control-points.md (D3 design, landed) | ✓ | ✓ | ✓ | ✓ | Pre-existing; the older C4 file remains the authority where newer designs cross-reference it. |
| C5 — specs/examples/ | ✓ | partial | ✓ | partial | review-loop.dot example violates D5 (F1) and uses non-canonical terminal-ID spelling (F2). |

## Findings

### F1 — C5 review-loop.dot edge condition uses `<` operator, which D5 does not admit (BLOCKER) — C5

`C5-examples-design.md §2.3` specifies the retry edge as:

> `reviewer -> implementer` with `condition="outcome.preferred_label == 'changes_requested' && context.iteration_count < 3"`

`D5-edge-condition-dialect.md §"Decision"` defines the grammar as `comparison := lhs " == " literal | lhs " != " literal`, and §"Open questions deferred" #3 explicitly says "Numeric comparison (`<`, `>`) ... Additive amendment when needed. Not v1." A canonical example that exercises an operator outside the v1 dialect contradicts the landed D5 decision.

C5 is the loser against D5. Fix: either (a) restructure the review-loop to use equality on `context.iteration_count == "3"` (treating iteration_count as a string the daemon increments through `"1" → "2" → "3"`) plus the unconditional-fallback edge to `close-needs-attention` for the cap-hit case, or (b) admit `<` to D5 as a one-paragraph dialect extension (would require a new D-decision and re-review). Option (a) is mechanically cleaner and matches D5's spirit; option (b) is honest about how a real iteration-cap is expressed.

Note that the cap-hit scenario in C5 §3.5 scenario 4 *already* relies on the unconditional-edge fallback (D-edge-cascade-invariant) — so the simplest fix is to drop the `< 3` clause from the conditional edge entirely. The conditional becomes `condition="outcome.preferred_label == 'changes_requested'"`, the unconditional fallback handles cap-hit, and the EM-015e iteration-cap is enforced by the *daemon* (not by edge syntax) via the existing review-loop cap discipline. This is also more honest about what enforces the cap: it's not the workflow author writing `< 3`, it's the daemon's EM-015e mode logic.

### F2 — C5 uses `close_needs_attention` (underscore); C1 + EM-015d use `close-needs-attention` (hyphen) (BLOCKER) — C5 vs. C1

`C5-examples-design.md §2.3` uses `close_needs_attention` consistently — node ID, edge targets, and scenario references. `C1-workflow-graph-design.md §4.5 WG-T03` reserves `close-needs-attention` (hyphen); EM-015d (specs/execution-model.md:278) uses `close-needs-attention` (hyphen). C5 also lists "close_needs_attention" in §3.5 scenarios and §5 cross-refs.

C5 is the loser — C1 WG-T03 is the authority for the reserved-ID spelling, and EM-015d (the older landed spec) already uses hyphen. Fix: replace all `close_needs_attention` with `close-needs-attention` throughout C5 §2.3, §3.5, and any other site. DOT permits hyphens in identifiers when quoted (`"close-needs-attention"`); pass-5 prose handles the quoting. If a DOT-syntax constraint forces underscore (worth a parser-survey check), C1 WG-T03 must be amended to match — but in either case the two designs must agree, and EM-015d's existing reservation is the strongest anchor for the hyphen form.

### F3 — C2 says non-agentic MUST carry `handler_ref` "per existing EM-007"; EM-007 says the opposite (MAJOR) — C2 vs. EM-007

`C2-execution-model-dot-design.md §7.5.3 item 7` reads:

> `non-agentic` nodes MUST carry `handler_ref` (per existing EM-007)

The C2 §7.5.4 dispatch table row for `non-agentic` likewise reads "Invoke the handler referenced by `handler_ref`." But specs/execution-model.md:146 EM-007 currently states: "Non-agentic, gate, and control-point nodes MUST NOT declare `handler_ref`." C2 cites EM-007 as already permitting the requirement, which is wrong.

C2 already correctly surfaces the analogous problem for `gate` carrying `handler_ref` (§7.5.3 item 7 + §6 trade-offs + §7 open-follow-up #1 — "Pass-5 must amend EM-007"). The same amendment must extend to non-agentic, OR the design must take a different position on how non-agentic nodes dispatch. Fix options:

1. **Extend the EM-007 amendment** to permit `handler_ref` on `agentic | non-agentic | gate` (i.e., everything except `sub-workflow`). The non-agentic dispatch surface has been implicit in handler-contract.md HC-008 / the LaunchSpec table all along; this is the honest reconciliation.
2. **Reframe non-agentic dispatch** through a different ref (`tool_ref` or similar). Larger change, probably gold-plating, and D17 (tool-node handler contract) is already an open item.

Lean: option 1. The fix is one sentence in C2 §7.5.3 item 7 ("per pass-5's amendment of EM-007 — see §7 follow-up #1, extended to cover non-agentic") and one column in §7.5.4. Also remove the "per existing EM-007" parenthetical — it propagates a stale claim into pass-5.

### F4 — D-attractor-adoption commits lowercase status enum; C1 §4.1 WG-N06 quotes uppercase (MINOR) — C1

`C1-workflow-graph-design.md §4.1 WG-N06`: "drawn from EM-005 closed status enum `{SUCCESS, FAIL, RETRY, PARTIAL_SUCCESS}`."

`D-attractor-adoption.md §"Items adopted verbatim"` item 5: "Status comparisons are case-sensitive against the lowercase enum `{success, fail, retry, partial_success}`. ... Pass-5 spec-draft normalizes to lowercase."

The existing execution-model.md spec uses uppercase. D-attractor-adoption explicitly says pass-5 normalizes to lowercase. C1 quotes the uppercase form, which will be wrong by the time pass-5 spec-text lands. Fix: either (a) quote both forms with a "pass-5 normalizes per D-attractor-adoption" footnote, or (b) cite the lowercase form prospectively and let pass-5 carry the consistency. Severity MINOR because it's a known-pending normalization, but the design should flag it rather than silently restate the pre-normalization form.

C3 §2.1 also implicitly assumes the existing-spec form (refers to "FAIL outcomes" with uppercase styling) — same MINOR finding applies. C5 §2.3/§3.5 talk about routing semantics in mixed case without quoting status enum literals, so C5 isn't affected here.

### F5 — C1 §4.5 WG-T04 cites EM-015c for terminal-node detection; check spec ID (MINOR) — C1

`C1 §4.5 WG-T04`: "terminal-node detection — cites EM-015c (terminal-state detection rule) verbatim."

Quick grep of the spec for EM-015c — the spec uses EM-015a / EM-015b / EM-015c / EM-015d / EM-015e in §4.3. EM-015c handles terminal-state detection (per the existing spec structure). The cite appears correct, but pass-5 should verify EM-015c is indeed the terminal-state-detection rule (vs. one of the adjacent sub-clauses). Severity MINOR (likely fine; worth a one-line confirmation in pass-5 to avoid propagating a wrong ID).

### F6 — C5 §6 OQ-2 admits the iteration-cap encoding question but treats it as pass-5-deferrable; F1 makes it pass-4-blocking (NIT) — C5

This finding is a meta-observation: C5 §6 OQ-2 ("Iteration-cap encoding ... Whether C1 introduces a graph-level retry-cap attribute ... is a pass-5 question. Until then, the condition expresses the cap inline.") *acknowledges* the issue but defers it. Per F1, the deferral is wrong — the inline expression violates D5 *now*, not at pass-5. The fix is mechanical (drop the `< 3`); the design's own meta-note about iteration-cap encoding should be updated to reflect that it has been moved to the unconditional-fallback edge.

### F7 — C5 cross-ref to "D4 row 5" via `context.last_verdict` lacks D8 registration discipline (NIT) — C5

`C5 §2.3` lists `context.iteration_count`, `context.last_verdict` as example LHS keys per "D4 row 5." D4 row 5 admits `context.<key>` only against the workflow's D8-registered context-key list. EM-015d already reserves these four context keys for `review-loop` mode; for the `dot`-mode review-loop example, C5 should declare them in the workflow's context-key registry (per D8 / per C1's WG-S05 reserved-attributes posture). Currently C5 silently assumes they're registered. Severity NIT — easy to address in pass-5 by adding a `context_keys=...` graph attribute or a sibling `.context-keys.yaml`. Surface here so pass-5 doesn't drop it.

### F8 — C2 §7.5.3 item 2 vs. C1 §4.6 WG-S05 unknown-attribute policy (NIT) — C2 vs. C1 (no contradiction, but a discoverability gap)

C1 WG-S05 commits a MIXED policy (Option C): strict for the `type` enum + reserved attributes + §8 failure-class RHS literals; permissive for non-reserved attributes (warning + retained in AST).

C2 §7.5.3 item 2 says: "Unknown type is a validation failure (refuse-to-run, not warn-and-continue — closes OQ-Q3)."

These agree on the `type` enum (strict). C2 does NOT restate the permissive-side of D9 for non-reserved attributes — a reader of §7.5.3 in isolation could conclude "all unknown attributes refuse-to-run." Fix: C2 §7.5.3 should add a one-sentence cross-ref to C1 §4.6 WG-S05 ("Non-`type` unknown attributes are warning-only per C1 WG-S05; only `type`-enum violations refuse-to-run."). Severity NIT — discoverability hygiene, not a contradiction.

## Approved positions (no rework needed)

- **D9 — unknown-attribute policy** (mixed Option C): committed by C1 §4.6 WG-S05; C2 §7.5.3 item 2 enforces the strict half. (See F8 for a cross-ref hygiene NIT.)
- **D10 — `schema_version` placement** (graph-level only): committed by C1 §4.6 WG-S02; C5 §2.2 README requirement #3 anchors the discipline by example.
- **D11 — in-repo `.dot` paths** (`specs/examples/` only at v1; `.harmonik/workflows/` deferred): committed by C1 §4.7 WG-R01-R02 and C5 §3.1 in unison.
- **D12 — terminal-node differentiation** (distinct terminal IDs, not `terminal_kind`): committed by C1 §4.5 WG-T02-T03. C5 §2.3 follows the distinct-ID approach. (F2 is a spelling fix, not a position fix.)
- **D7 — `kind = gate_decision` payload**: committed by C3 §2.3 + §3.3. Position internally consistent with EM-005a's extension protocol and D2's "kind for failure was wrong, kind for gate is right" distinction. C2 §7.5.4's gate row uses the conditional ("`kind = gate_decision` per D7, pending") correctly.
- **D8 — per-workflow registered context-key list** (lean): committed by C3 §3.4. Both warn-vs-reject (OQ-C3-2) and the registry's physical location remain pass-5 picks. (F7 surfaces a C5 gap consequent on D8.)
- **D-attractor-adoption**: C1/C2/C3/C5 all reflect adopt-verbatim + named-divergences. (See F4 for the lowercase normalization MINOR.)
- **D-edge-cascade-invariant**: C1 §4.2 WG-E03 and C2 §7.5.2 restate the invariant; C5 §3.5 scenario 4 ships the fallback test. Excellent traceability.
- **D-verdict-surfacing**: C1 §4.5 WG-T01 + C3 §2.1 agentic row + C5 §2.3 reviewer edges all align on `preferred_label` as the verdict carrier. No invented `verdict` field anywhere.
- **D1 — `failure_class` as LHS**: row 2 of C1 §4.3 WG-C02 + C2 §7.5.2 failure-class clarifying clause + C3 §2.1 handler-hint rule.
- **D2 — `failure_class` top-level field, daemon back-fill**: C1 §4.4 WG-F03 + C3 §2.2 EM-005 v2 bump + C2 §7.5.2 back-fill clause.
- **D3 — control-point not a node-type, 4-type catalog**: C1 §4.1, C2 §7.5.3 item 2 + §7.5.4 4-row table, C3 §2.1 4-row table, C5 §5 ("review node is type `agentic`, NOT `gate`"). All four downstream designs agree.
- **D4 — closed LHS whitelist (5 identifiers)**: C1 §4.3 WG-C02 transcribes the whitelist. C2 §7.5.3 item 3 enforces it statically. C5 §2.3 uses only whitelisted LHS — modulo the `<` operator in F1 (D5 violation, not D4).
- **D5 — restricted equality + `&&` dialect**: C1 §4.3 WG-C01 transcribes the grammar. C2 §7.5.2 + §7.5.3 honor it. (F1 catches C5 violating it.)
- **review-loop.dot alone; bead-process.dot deferred**: C5 §3.2 + rationale. Aligns with research recommendation and D17 dependency chain.
- **Brief's stale "EM-046c" reference**: C3 §1.2 and §7 OQ-C3-3 correctly catch and route to EM-042a + control-points.md §6.2.

## Contradiction sweep

| Cross-component check | Result |
|---|---|
| Edge-condition LHS — C1 §4.3 vs. C2 §7.5.3 vs. C5 §2.3 | Agree on the 5-LHS whitelist. F1 is a *dialect* (D5) violation, not a whitelist (D4) violation. |
| `failure_class` flow — C2 §7.5.2 back-fill ↔ C3 §2.1 handler-hint rule ↔ C1 §4.4 WG-F03 | Agree. Daemon-authoritative, handler-as-hint, daemon back-fills before cascade. Logging rule open per D2 follow-up #1. |
| Terminal-node IDs — C1 §4.5 WG-T03 ↔ C5 §2.3 | **DISAGREE on spelling.** F2. C1 + EM-015d use `close-needs-attention` (hyphen); C5 uses `close_needs_attention` (underscore). |
| Reviewer node type — D3 §"Implications for C5" ↔ C5 §2.3 ↔ C3 §2.1 | Agree: reviewer is `agentic`, NOT `gate`. |
| `handler_ref` obligations — C1 §4.1 ↔ C2 §7.5.3-4 ↔ C3 §2.1 ↔ EM-007 | **C2 contradicts EM-007 on non-agentic.** F3. The `gate` contradiction is correctly surfaced as pass-5 follow-up; the non-agentic contradiction is silently asserted as "per existing EM-007," which it isn't. C1 and C3 don't take a position. |
| `kind = gate_decision` — C3 §2.3 ↔ C2 §7.5.4 gate row ↔ C1 §6 open items | Agree. C2 uses conditional ("per D7, pending") correctly. C1 §6 lists D7 as pending. |
| Schema-version placement — C1 §4.6 WG-S01-S04 ↔ C2 §7.5.3 item 1 ↔ C5 §2.2 | Agree: graph-level only, N-1 readability, additive bumps. |
| Sub-workflow propagation — C1 §4.1 WG-N05 ↔ C2 §7.5.2 clarifying clause ↔ C3 §2.1 sub-workflow row | Agree: verbatim propagation per EM-036a; review-loop excluded as sub-workflow per EM-015d. |
| `specs/examples/` path — C1 §4.7 WG-R01 ↔ C5 §2.1 ↔ §3.1 | Agree (D11). |
| Status-string case — D-attractor §item 5 lowercase ↔ C1 §4.1 WG-N06 uppercase | **MINOR mismatch.** F4. Both pre-normalization (existing spec) and post-normalization (D-attractor commitment) coexist; C1 should flag prospectively. |
| `preferred_label` verdict vocabulary — D-verdict-surfacing §"Open follow-ups" #1 ↔ C5 §2.3 strings | Agree (C5 uses the documented lean `{approved, changes_requested, blocked}`; pass-5 picks finals). |

## Recommended action

**REQUEST_CHANGES.** Two BLOCKER findings (F1, F2) sit entirely inside C5 and are mechanical to fix. One MAJOR finding (F3) sits in C2 §7.5.3 / §7.5.4 and is also mechanical (one parenthetical to delete, one open-follow-up to extend). The MINOR + NIT items can land alongside the fix or be deferred to pass-5 prose-writing.

Dispatch one fix-up sub-agent (or a `harmonik run` slot) per component touched:

1. **C5 fix-up bead.** Rewrite C5 §2.3 + §3.5 to (a) replace `close_needs_attention` → `close-needs-attention` throughout, (b) drop the `< 3` clause from the retry edge and lean on the unconditional fallback for cap-hit (per F1's option a), (c) add a one-sentence C5 §6 OQ-2 update reflecting that the iteration cap is daemon-enforced via EM-015e rather than edge-expression-enforced. Optional: add F7's context-key registration sentence.

2. **C2 fix-up bead.** Amend C2 §7.5.3 item 7 to remove "per existing EM-007" from the non-agentic clause and add "per pass-5's amendment of EM-007 — see §7 follow-up #1, extended to cover non-agentic." Update §7.5.4 dispatch table row for non-agentic to cite the amendment. Extend §7 open-follow-up #1 from "gate" to "gate + non-agentic." Optional: add F8's one-sentence cross-ref to C1 §4.6 WG-S05.

3. **C1 fix-up bead (small).** Update §4.1 WG-N06 to flag the lowercase-normalization per D-attractor-adoption (F4) — one sentence, no normative change. Optional: confirm EM-015c is the right ID for terminal-state-detection (F5).

After the three fix-ups land, re-review can be a focused diff-only pass (≤30 min), and the work advances to pass-5 spec-draft.

**Finding counts:** 2 BLOCKER, 1 MAJOR, 2 MINOR, 3 NIT.

**Top 3 load-bearing findings:** F1 (D5 dialect violation in C5), F2 (terminal-ID spelling mismatch C1↔C5), F3 (C2 mis-cites EM-007 for non-agentic).

**Not ready to advance to spec-draft until F1+F2+F3 are addressed.**

## Round 2 — diff verification

**Reviewed:** 2026-05-23
**Scope:** diff-only re-read of C5-examples-design.md and C2-execution-model-dot-design.md against the three round-1 load-bearing findings.

### F1 — CLOSED

C5 §2.3 retry-edge bullet (line 73) now reads `condition="outcome.preferred_label == 'changes_requested'"` with the explicit annotation: *"NOTE: no inline `iteration_count` bound — the D5 v1 dialect is equality + `&&` only (no `<`/`>`); cap-hit is handled by the unconditional fallback edge below + daemon-enforced EM-015e iteration-cap."* Reinforced in §2.3 closing paragraph (line 77): *"The cap-hit is enforced daemon-side (EM-015e), NOT by an inline `<` condition (out of D5 v1 dialect)."* §3.5 scenarios 2+4 (lines 134, 136) updated accordingly ("iteration cap enforced by daemon EM-015e, not by inline edge condition"). §6 OQ-2 (line 173) rewritten: *"review-loop.dot does NOT encode the cap as an inline edge condition ... cap is enforced exclusively by (a) EM-015e on the daemon side and (b) the D-edge-cascade-invariant unconditional fallback edge."* No `<`/`>` operator survives in C5. D5 dialect cleanly respected.

### F2 — CLOSED

All occurrences in C5 are now `close-needs-attention` (hyphen). Verified at lines 68 (node introduction), 74, 75 (edge targets), 77 (fallback-prose), 134, 136, 137 (scenarios). Zero remaining occurrences of `close_needs_attention` (underscore). Spelling now matches C1 WG-T03 + EM-015d.

### F3 — CLOSED

Three sites patched cleanly:
- C2 §7.5.3 item 7 (line 81): the "(per existing EM-007)" parenthetical is gone. Replaced with: *"current EM-007 prose lists `handler_ref` as `agentic`-only and lists `non-agentic` as MUST-NOT-carry-`handler_ref`. Both the `gate` and `non-agentic` requirements above are pass-5 coordinated amendments to EM-007 (single table row + one sentence each — see §7 follow-up #1)."*
- C2 §7.5.4 dispatch-table prose (line 98): *"this collapsing is itself part of the pass-5 EM-007 amendment, since current EM-007 prose says non-agentic MUST NOT carry `handler_ref`."*
- C2 §6 trade-offs (line 212): bullet now reads *"**`gate` AND `non-agentic` MUST carry `handler_ref`** contradicts the current EM-007 prose ... pass-5 amends EM-007 to cover BOTH cases."*
- C2 §7 follow-up #1 (line 220): scope extended — *"Pass-5 must amend EM-007 to permit `handler_ref` on BOTH `gate` AND `non-agentic` nodes."*

The non-agentic amendment is now surfaced honestly as pass-5 work rather than mis-cited as already-in-EM-007.

### New contradictions introduced

None. Spot-checked cross-component seams again:
- Terminal-node ID spelling: C1 WG-T03 ↔ C5 §2.3 — both `close-needs-attention`.
- D5 dialect: C1 §4.3 WG-C01 ↔ C5 §2.3 — both equality+`&&` only.
- EM-007 amendment scope: C2 §7.5.3 item 7 + §7.5.4 + §6 + §7 follow-up #1 — all four sites consistent on "gate AND non-agentic" as pass-5 scope.

### Round-1 minor / nit status

- F4 (lowercase status enum in C1 §4.1 WG-N06), F5 (EM-015c terminal-state ID confirmation), F6 (C5 §6 OQ-2 meta-note — incidentally addressed by F1 patch; the OQ-2 prose now correctly reflects daemon-side enforcement), F7 (C5 context-key registration discipline), F8 (C2 §7.5.3 cross-ref to C1 WG-S05 permissive policy): not blocking. F6 is incidentally CLOSED by the F1 rewrite. F4, F5, F7, F8 remain as recommended pass-5 polish or follow-up beads.

### Final verdict

**APPROVE.** All three load-bearing findings (F1 BLOCKER, F2 BLOCKER, F3 MAJOR) are closed. No new contradictions. Ready to advance to pass-5 spec-draft. Remaining MINOR/NIT items (F4/F5/F7/F8) can land as pass-5 prose polish or follow-up beads — they do not gate the pass.
