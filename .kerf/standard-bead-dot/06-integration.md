# Integration Review — standard-bead-dot (Pass 6)

Epic hk-o7j. Change: tier-4 built-in default `workflow_mode` flips `single` → `dot`
(embedded `standard-bead.dot`) with an `EM-012a-FLOOR` review-loop floor, plus a new
`sub-workflow-dispatch.md` spec (SW-001..SW-010, SW-INV-001/002).

Bench drafts under review (reviewer-APPROVED at Pass 5): `05-spec-drafts/execution-model.md`
(UPDATE), `05-spec-drafts/workflow-graph.md` (UPDATE), `05-spec-drafts/sub-workflow-dispatch.md`
(NEW). All edits below are bench-only; no `specs/` or code mutations.

## 1. Contradiction sweep (the core of this pass)

Swept EVERY live spec under `specs/` (changed and unchanged) for statements the default
flip could contradict: `default`, `single`, `workflow_mode`, `WorkflowMode`, `review-loop`,
`dispatched`, `standard-bead`, `sub-workflow`, `EM-012a`, `tier-4`, `built-in fallback`,
`review_bypassed`, `no-review`, `unreviewed`. Fan-out: 4 parallel read-only sub-agents over
the full spec set. Each NEEDS-DRAFT-UPDATE hit below was re-verified directly against the live
file (line/req-ID/text).

A NEEDS-DRAFT-UPDATE hit asserts or implies "tier-4 default = `single`" in a spec **not**
covered by the three bench drafts, so the flip leaves it stale. The three bench drafts already
sync their own files (the EM bench draft fixed EM-012 line 229 + §6.1 line ~1206; the live
`specs/execution-model.md` copies of those are superseded by the bench UPDATE, NOT separate
contradictions).

### NEEDS-DRAFT-UPDATE (4 specs — recommend new draft-updates; author DEFERRED to operator)

| Spec | Req-ID | Line | Conflicting text (verbatim) | Why it conflicts |
|---|---|---|---|---|
| `process-lifecycle.md` | **PL-004a** | 221 | "When the field is absent, the daemon's default workflow mode MUST be `single`." (+ tier-list "built-in fallback (`single`)" same req; + PL-005 step-0 line ~236 "absence of the field defaults the cached value to `single`") | PL-004a is the tier-3 source EM-012a cites by name. It normatively pins the daemon-absent default to `single`. Must flip to `dot` and carry the `EM-012a-FLOOR` review-loop floor. **Highest-priority — load-bearing tier-3 anchor.** |
| `operator-nfr.md` | **ON-004a** | 168, 173 | "**Default value:** `single` (built-in fallback)." and tier-4 line "Built-in fallback `single`." | The operator config inventory normatively declares the built-in fallback as `single`. Must flip both lines to `dot` (+ note the review-loop floor). |
| `beads-integration.md` | **BI-009a** | 156 | "...its absence defers to lower-precedence tiers (per-project → daemon-level per [process-lifecycle.md §4.1 PL-004a] → built-in fallback `single`)." | The `workflow:<mode>` label req describes the resolution chain's tail as `single`. Inherits the PL-004a contradiction; the "built-in fallback `single`" clause must become `dot`. |
| `handler-contract.md` | **HC-006** | 154 | "**`workflow_mode`** (enum `{single, review-loop, dot}`, optional). Present iff the daemon resolved a non-`default` mode for the dispatched run; otherwise omitted." | "non-default" is now AMBIGUOUS: with `dot` the default, a `single` run is now non-default and would (per this rule) be surfaced, while a `dot` run would be omitted — the inverse of the prior intent. Recommend rewording to name the field-presence rule against an explicit value rather than "default". **Minor — observational field only.** |

### BENIGN / already-consistent (notable hits, no update)

- `execution-model.md` live §4.3 EM-012 (line ~229) and §6.1 Run RECORD (line ~1206) "defaults
  to `single`" — these are the live copies the **EM bench draft already supersedes** (restatement-sync
  recorded in 05-changelog). Not separate contradictions.
- `queue-model.md` — ZERO relevant hits. Queue is mode-agnostic; it does not assign or normalize
  `workflow_mode`. (The historical "queue submit landed beads unreviewed" bug was a CODE defect in
  `beadsToQueueDoc`, not spec text — nothing in queue-model.md encodes a single-default.)
- `workflow-graph.md` (live), `architecture.md` — mode-agnostic; no "single is default" assertion.
- `handler-contract.md` HC-005a, HC-006a, HC-058, HC-061, HC-062 — describe single/sub-workflow
  *mechanics*, mode-conditional, not default-asserting. HC-058/061/062 are the anchors the new SW
  spec binds; CONSISTENT (no contradiction).
- `event-model.md` §8.1 `WorkflowMode` payload-field rule, and §8.1.9/§8.1.10 `sub_workflow_entered`
  / `sub_workflow_exited` — exist with the exact fields the SW spec cites; mode-agnostic. CONSISTENT.
- ZERO relevant hits: `control-points.md`, `release-pipeline.md`, `cognition-loop.md`,
  `claude-launchspec.md`, `scenario-harness.md`, `reconciliation/spec.md`. BENIGN-only:
  `claude-hook-bridge.md` (`HARMONIK_WORKFLOW_MODE` "only set when non-default" — same wording
  watch-item as HC-006 but env-var observational), `harness-contract.md`, `workspace-model.md`.

**Recommendation:** file the 4 NEEDS-DRAFT-UPDATE items as draft-update beads in the tasks pass
(PL-004a + ON-004a + BI-009a are the same single-word "single→dot" flip + floor-note; HC-006 is a
small reword). The HC-006 "non-default" ambiguity and the parallel `HARMONIK_WORKFLOW_MODE` wording
should be fixed together. Author DEFERRED per pass instructions — operator to decide.

## 2. Reviewer nits applied (3 — all bench-only)

1. **EM-012a tier-4 cross-ref tightened** (`execution-model.md` §4.3, line 242). The canonical-exemplar
   pointer now names `[workflow-graph.md §17 WG-047..WG-052 — canonical exemplar: standard-bead.dot]`
   as the normative topology contract, retaining `§12 WG-036` as a secondary catalog cross-ref. DONE.
2. **SW-009 harmonized** (`sub-workflow-dispatch.md` §2.1, in-scope list). One-liner changed from
   "The **DOT-mode-only** constraint on `sub-workflow` nodes (SW-009)." to
   "The **graph-driven-mode** constraint... valid only under `dot` (and the `single` carve-out),
   never `review-loop` (SW-009)." — now agrees with the SW-009 title and §4.6 body, both of which
   admit the `single` carve-out. DONE.
3. **WG-006 stale anchor fixed** (`workflow-graph.md` §4, WG-006 lines 127, 129). The three
   `execution-model.md §4.10 EM-034*` anchors retargeted to **§4.8** — confirmed as the live home
   of the EM-034 family (bench EM draft `### 4.8 Sub-workflow composition` line 647; the §4.10 cascade
   at line 737 homes EM-041/EM-046, not EM-034). Same-file same-blast-radius repair also corrected an
   in-WG-006 content swap: the text said "namespacing on expansion is per EM-034b" but namespacing is
   **EM-034a** (EM-034b is acyclicity); now reads "namespacing... per EM-034a; acyclicity per EM-034b".
   DONE. `05-changelog.md` reconciliation note updated to mark all three resolved.

## 3. Cross-reference verification (the 3 drafts as a set)

All forward-refs among the three bench drafts now resolve (all three files exist on the bench):

- **sub-workflow-dispatch.md → execution-model.md (bench):** EM-005, EM-007, EM-012a, EM-015d,
  EM-034/034a/034b/034c, EM-036, EM-036a, EM-038, EM-041, EM-041a, EM-046a, §7.5/§7.5.2/§7.5.3/§7.5.5
  — ALL present in the bench EM draft (grep-confirmed, §4.8 family + §7.5 binding).
- **sub-workflow-dispatch.md → workflow-graph.md (bench):** WG-001, WG-006, WG-029, WG-031 — ALL present.
- **sub-workflow-dispatch.md → handler-contract.md (LIVE):** HC-058, HC-061, HC-062 — ALL present.
- **sub-workflow-dispatch.md → event-model.md (LIVE):** §8.1.9 `sub_workflow_entered`, §8.1.10
  `sub_workflow_exited` — present with `terminal_outcome_status` field SW-005 relies on.
- **execution-model.md (bench) → workflow-graph.md (bench):** new EM-012a §17 WG-047..WG-052 pointer
  (nit 1) resolves — §17 + all six req-IDs exist in the WG bench draft. EM also cites WG-036 (§12), present.
- **execution-model.md (bench) → sub-workflow-dispatch.md:** the NORMATIVE CROSS-REFERENCE note
  (line 250) links `[sub-workflow-dispatch.md]` — now on the bench, resolves.
- **workflow-graph.md (bench) → execution-model.md:** §17 WG-051 cites EM-012a tier-4 + EM-055; both present.

**CROSSREFS_OK: yes.** Two cosmetic stale qualifiers remain in the WG bench draft (§14 line ~650 and
the §16/§18 changelog rows call `sub-workflow-dispatch.md` "not yet on disk — forward cross-ref,
unverified" / "planned companion spec"). The links themselves resolve; only the parenthetical
"unverified/planned" wording is stale now that the SW draft exists. Recommend a one-word finalize-time
scrub of those qualifiers (NOT a blocker; recorded as OPEN item O-3).

## 4. Terminology consistency (across the 3 drafts)

**TERMINOLOGY_OK: yes.** Same concept = same name across the set:
- **Namespacing:** SW-002 prose `<parentNodeID>/<subNodeID>` + the `A/B/C` left-to-right composed
  example matches EM-034a's `<parent_node_id>/<sub_node_id>` + identical `A/B/C` example. The
  camelCase (SW prose variable) vs snake_case (EM record-field) is a notation style difference, not a
  concept divergence; the binding is explicit (SW-002 "binds EM-034a"). No change needed.
- **In-place / single run identity:** SW-001 + SW-INV-001 ("exactly one RunID, the parent run_id; no
  child RunID") matches EM-034's single-run-expansion framing. Consistent.
- **Terminal outcome / verbatim:** SW-006 + SW-INV-002 ("byte-equal... no rewrite/synthesis/aggregation")
  matches EM-036a ("inherits the expanded terminal outcome mechanically; MUST NOT declare its own
  Outcome shape"). Consistent.
- **Failure class:** SW-003/004/010 use `structural` for expansion-time reject, consistent with
  EM-046a (`no_outgoing_edge_matches` → `structural`). (Reviewer APPROVED `structural` as the closest
  published class — see O-2.)
- **Mode constraint:** SW-009/SW-010 "dot (+ single carve-out), never review-loop" matches EM-015d
  carve-out + EM-034 "review-loop is NOT a sub-workflow". After nit 2, the SW §2.1 one-liner, SW-009
  title, and SW-009 body all agree.

## 5. Open items for the tasks pass

- **O-1 (draft-updates):** Author + file the 4 NEEDS-DRAFT-UPDATE items from §1 — PL-004a,
  ON-004a, BI-009a (the same `single`→`dot` + review-floor-note flip) and HC-006 (reword the
  "non-default" presence rule; pair with `claude-hook-bridge.md` `HARMONIK_WORKFLOW_MODE` wording).
  These are spec-text follow-ups the finalize must NOT silently skip — they keep the cross-spec
  resolution chain (per-task → per-project → daemon → built-in) internally consistent after the flip.
- **O-2 (failure-class confirm):** SW-003/004/010 use `structural`; reviewer APPROVED as closest
  published anchor. The impl/tasks pass should confirm against the landed failure-class enum and flag
  if a dedicated class is wanted (carried from 05-changelog reconciliation bullet 3).
- **O-3 (cosmetic):** Scrub the "not yet on disk / planned companion spec / unverified" qualifiers
  on the `sub-workflow-dispatch.md` cross-refs in the WG bench draft §14 + changelog rows at finalize.
- **O-4 (test obligations):** The SW §6 Conformance lists 6 impl-test obligations and WG-052 cites the
  golden test `internal/workflow/scenario_standard_bead_hkp0kum_test.go`; the keystone impl bead
  (replace the `dot_cascade.go` `core.NodeTypeSubWorkflow` out-of-scope stub) must satisfy them.

## Final assessment

The corpus is coherent after this change. The dot-default flip is fully wired inside the three bench
drafts (EM tier-4 + floor, WG §17 canonical exemplar, SW dispatch contract), and the only outward
ripple is the four stale "single is the default" assertions in PL-004a / ON-004a / BI-009a / HC-006 —
all narrow, mechanical, and recommended (not authored) here per the pass scope. Cross-refs resolve as
a set and terminology is consistent. No locked decision is reopened. Ready to advance to the tasks pass
once O-1 is dispositioned.
