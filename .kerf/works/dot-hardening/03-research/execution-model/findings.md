# 03 — Research: `specs/execution-model.md` (Component B, reqs B1–B7)

> Grounds the EM change in the current spec + code with exact anchors. Does NOT design new text.
> All EM citations are `specs/execution-model.md` line numbers at HEAD (file = 1909 lines).
> All code citations are `file:line` against `/Users/gb/github/harmonik`.

## 0. Anchor map (verify-first — section : EM line : what it says today)

| Concern | EM anchor | Line |
|---|---|---|
| `## 7.5` dot-mode BINDING DOCUMENT header | §7.5 | 1562 |
| Input contract (ingest→validate→return) | §7.5.1 EM-055 | 1570–1597 |
| **`goal` → agentic brief** (step 6) | §7.5.1 EM-055 step 6 | 1581 |
| **B↔E brief-composition contract** (goal=ExtraContext, prompt=Body, bead body=Body) | §7.5.1 | 1584–1590 |
| Assembly order note — cites `buildAgentTaskContent` | §7.5.1 | 1590 |
| Resume re-executes steps 1–4, reparse, no serialized parse-tree | §7.5.1 | 1592 |
| Dispatch equivalence (§7.4/§7.3 unchanged) | §7.5.2 EM-056 | 1601–1612 |
| **"A `dot` run MUST NOT produce" reviewer-feedback + review-target** | §7.5.2 EM-056 clause 4 | **1610** |
| Validator obligations (7 static checks) | §7.5.3 EM-057 | 1616–1632 |
| Node-type dispatch table | §7.5.4 EM-058 | 1638–1653 |
| Non-committing agentic sub-note (implementer clean-exit → SUCCESS) | §7.5.4 | 1653 |
| Review-loop lifecycle (hardcoded 2-node) | §4.3 EM-015d | 349–418 |
| **reviewer-feedback file writer** (`reviewer-feedback.iter-<N-1>.md`, review-loop only) | EM-015d-RFD | 364–383 |
| **review-target writer** (`review-target.md`, review-loop only; sole reviewer context) | EM-015d-RIA | 386–413 |
| implementer commit obligation is review-loop-scoped, NOT dot | EM-015d | 416 |
| Model/effort resolution precedence (6-tier) | §4.3 EM-012b | 255–308 |
| **Per-node override tier-0** (node `model=`/`effort=` beats run default) | EM-012b-NODE | 295 |
| Precedence summary block (tier 0 highest) | EM-012b | 297–306 |
| Deterministic replay contract (git trail; NOT event tail) | EM-032 | 637–641 |
| Run record: `model_preference`, `context`, `template_params` | §6.1 RECORD Run | 1214–1229 |
| Node record: `model`,`effort`,`prompt`,`non_committing`; `role` ABSENT | §6.1 RECORD Node | 1162–1182 |
| Workflow.goal field | §6.1 RECORD Workflow | 1158 |

Note: the phrase the brief asked to locate ("MUST NOT produce" / "review-loop-mode") lives at **line 1610
(EM-056 §7.5.2 clause 4)**, not near 353–390. Lines 353–413 are where EM-015d-RFD/RIA *define* those
artifacts as review-loop-only; 1610 is the dot-mode *prohibition*. Both are load-bearing for B3.

ID high-water: EM uses §-numbered clauses; dot-mode IDs run EM-055..EM-059, EM-068. New IDs continue from EM-069.

---

## B1 — Single brief renderer

- **(a) Current:** two role-specific builders — `buildAgentTaskContent` (agenttask_chb028.go:271, via `WriteAgentTask`:205)
  and `buildReviewTargetContent` (:577, via `WriteReviewTarget`:558). Spec only ever describes the *implementer*
  brief assembly: EM-055 step 6 (1581) + the B↔E contract (1584–1590) — goal→ExtraContext, prompt/bead-body→Body.
  There is **no EM clause describing how a reviewer brief is assembled in dot mode** (EM-056 clause 4 forbids the
  only writer that exists). So the spec has a one-sided renderer contract.
- **(b) Insert/modify:** amend the B↔E contract (1584–1590) into a role-general "single renderer" clause: brief =
  {declared inputs} + {prompt} + {role} + {goal}, role framing selected by role. New EM-069-class clause under §7.5.1.
  The 1590 sentence pinning assembly to `buildAgentTaskContent` must be generalized (it names one builder).
- **(c) Notes:** the renderer must subsume `buildReviewTargetContent` — today's separate reviewer path (1610 forbids
  it, code writes it anyway; see B3). Declared-inputs vocabulary is owned by Component A (WG); EM consumes it.
- **(d) OQ:** does §6.1 Node need a `role` field? It is threaded today only via `node.Role` (dot_cascade.go:1305–1316,
  prepended to ExtraContext) and is **absent from the §6.1 Node record (1162–1182)**. A role-selected renderer likely
  needs `role` promoted to a typed Node field.
- **(e) Hazard:** EM-055 step 6 + B↔E contract are the CURRENT normative brief contract; rewriting them risks
  desyncing the leak-oracle (Component C asserts on a typed input manifest, not markdown). Keep the manifest surface in mind.

## B2 — Reviewer brief keeps task context

- **(a) Current:** `buildReviewTargetContent` renders `## Bead` id/title/**BODY** (agenttask_chb028.go:610–613:
  `id:`, `title:`, then `p.BeadBody` verbatim) + `## Diff range` base/head SHAs (:618–620). Payload is populated in
  dot mode with `BeadTitle`/`BeadBody`/`BaseSHA`/`HeadSHA` (dot_cascade.go:1274–1282). So the reviewer today DOES know
  the change it judges. But **the spec forbids this artifact for dot** (EM-056 clause 4, 1610).
- **(b) Insert/modify:** the new renderer's reviewer role-framing MUST require task context (bead id/title/body ref +
  diff base/head SHAs) alongside the rubric. New sub-clause of the B1 renderer clause, §7.5.1.
- **(c) Notes:** git diff (base/head SHAs) stays the code channel — per MODEL.md it is NOT a variable/edge payload.
- **(d) OQ:** does the reviewer get the bead BODY verbatim (today) or a body *reference* (problem-space B2 says
  "id/title/body reference")? Verbatim body is what avoids under-contexting; a bare reference may regress.
- **(e) Hazard:** the single-renderer change must NOT reduce the reviewer to rubric-only — the current code proves the
  reviewer needs the body to judge (:610–613). A naive "declared inputs only" reviewer that omits the bead body
  under-contexts the reviewer.

## B3 — Feedback as a produced value on the back-edge (reconcile EM §7.5)

- **(a) Current — code/spec DISAGREE (load-bearing):**
  - Spec: EM-056 §7.5.2 **clause 4, line 1610** — "A `dot` run MUST NOT produce" `reviewer-feedback.iter-<N>.md`
    (EM-015d-RFD) or `review-target.md` (EM-015d-RIA); "their absence on a `dot` run is not an authoring error."
  - Code: dot_cascade.go **DOES write both** in dot mode — `WriteReviewerFeedback` at dot_cascade.go:875 (tagged
    `hk-wixms`, carrying verdict notes on the back-edge) and `WriteReviewTargetVia` at :1274. Directly violates 1610.
- **(b) Insert/modify:** **replace** EM-056 clause 4 (1610) with the value-on-back-edge channel: reviewer produces
  `verdict`+`notes`; on REQUEST_CHANGES they bind the implementer's declared feedback input, rendered by the single
  renderer (B1). This is the reconciliation the whole component exists for. Also touch EM-015d-RFD/RIA (364–413) so
  the review-loop-only framing is not read as forbidding the dot channel.
- **(c) Notes:** delivery is currently harness-divergent (claude paste-inject via pasteinject.go + rewritten
  agent-task.md; pi/codex via `implementerResumeSeedPrompt`, agentseedprompt.go:44). The value-on-edge collapses the
  WHAT; the HOW stays per-tool transport (Component C).
- **(d) OQ:** is the dot feedback channel still a file on disk (reusing today's writer) or purely an in-record value
  the renderer reads? MODEL.md §"mechanism" says the file only existed because there was no input channel — implies retire.
- **(e) Hazard:** must not leave EM-056 clause 4 and the new channel both live — that re-creates the exact
  code/spec contradiction. The reconciliation is a *replacement*, not an addition.

## B4 — Iteration-1 vs resume binding

- **(a) Current:** phase selection at **dot_cascade.go:1288–1303** — reviewer always fresh; implementer
  `iterationCount <= 1` → `ImplementerInitial`, else `ImplementerResume` resuming the prior claude session
  (`*claudeSessionID`). The prior-iteration section is present only on resume (agenttask_chb028.go phase-specific
  block). No EM clause states "feedback input unbound on iter 1 → section omitted."
- **(b) Insert/modify:** new sub-clause of the B1/B3 renderer clause: feedback input unbound at iteration 1 (section
  omitted), bound on bounce-back (rendered). §7.5.1.
- **(c) Notes:** this is what makes a deterministic round-trip *forceable* once the leak (B1) is gone.
- **(d) OQ:** "iteration" here is the review-loop `iteration_count` (§6.1 context, 1225). Is the same counter the seal
  key for B6 (see hazard H1)? Must be defined once.
- **(e) Hazard:** iteration is a `context` value (1225) not a first-class dispatch key — B6's `(node_id, iteration)`
  seal must pin exactly which counter it reads.

## B5 — Model-resolution ladder flip

- **(a) Current:** EM-012b-NODE (295) + precedence summary (297–306) put the **per-node attr at tier 0 — ABOVE** the
  per-bead label (tier 1). Verified in code: `nodeModelForHarness` (dot_cascade.go:1182) returns `node.Model`, layered
  over `resolvedModel` at :1351 — so `.dot model=sonnet` overwrites a per-bead `model:opus` (ADV-B "gets right",
  code-confirmed). The operator wants the flip: **per-bead escalation ABOVE node default.**
- **(b) Insert/modify:** rewrite EM-012b-NODE (295) and the precedence block (297–306): node `model=`/`effort=` becomes
  a **soft/overridable default** sitting BELOW a per-bead escalation / per-run force, with a per-node lock for
  keep-cheap nodes. This inverts the current tier-0 statement.
- **(c) Notes:** Component A (WG) owns the override-surface serialization + the lock attr; EM owns the precedence order.
  Depends on A5.
- **(d) OQ:** does a per-run `--force-model` respect a per-node lock? (ADV-B S1 — unspecified; a precedence spec must
  answer.) Escalation scope: whole-run vs per-role (MODEL.md marks this OPEN).
- **(e) Hazard:** flipping silently changes deployed beads carrying `model:opus` (ADV-B S2) and risks silent cost
  overrun on cheap implementer nodes (ADV-B S1). The precedence rewrite must state the new order unambiguously.

## B6 — Per-(node, iteration) concrete-selection seal

- **(a) Current:** §6.1 Run seals only `model_preference: ModelPreference` at claim (1219) — a run-level `(model,effort)`
  pair. `ResolveModelPreference` runs once at claim (workloop.go:3274) and, for tier 1, returns the **alias STRING**
  (`strings.TrimPrefix(..., "model:")`, modelpreference.go:220–223) — NOT a concrete. **No per-(node,iteration)
  concrete field exists** anywhere in §6.1. EM-012b-NODE's replay claim (295: "a replay re-derives the same per-node
  (model,effort)") rests on inputs being immutable — which a hot-reloadable alias catalog (Component C) breaks.
- **(b) Insert/modify:** NEW EM clause (EM-069-class) under §7.5 + a NEW §6.1 Run field (e.g. `node_concrete_seal:
  Map<(node_id, iteration), (tool, model, effort)>`). At each agentic dispatch, after alias→concrete resolution for
  the node's effective tool, record the concrete keyed by `(node_id, iteration)`.
- **(c) Notes:** the concrete needs the node's *effective tool* — a dispatch-time fact (mixed-harness runs:
  `strong.claude ≠ strong.pi`, ADV-B F1 hole 3). Claim-time alias seal has zero determinism value.
- **(d) OQ:** where does the seal live — Run.context, a new Run field, or an event replayed from the git/event trail
  (EM-032 says replay reads the git checkpoint trail, 639)? The seal's durable home must be decided.
- **(e) Hazard (H1, load-bearing):** the seal MUST be keyed by `(node_id, iteration)`, NOT `node_id` alone. The
  REQUEST_CHANGES back-edge re-dispatches the SAME implementer node at iteration 2,3 (dot_cascade.go:1288–1303) — an
  iteration-blind key overwrites iteration 1's concrete, and replaying iteration 1 binds iteration 2's model (ADV-B F1
  hole 2). Composite key is non-negotiable.

## B7 — Replay reads the seal

- **(a) Current:** resume RE-PARSES the graph and re-enters dispatch fresh — EM-055 "Resume semantics" (1592: "MUST
  re-execute steps 1–4… MUST NOT trust a serialized prior parse tree"). There is **no "read the sealed concrete first"
  branch** — a resumed/replayed run re-enters the model-resolution path and recomputes from the live catalog/labels/
  config (ADV-B F1 hole 1). EM-032 (637) is the general replay contract but says nothing about model concretes.
- **(b) Insert/modify:** NEW EM clause pairing with B6: on replay/resume, dispatch MUST read the sealed concrete for
  `(node_id, iteration)` and MUST NOT recompute from the live catalog. Amend EM-055 resume semantics (1592) to add the
  seal-read branch, and cross-ref EM-012b-NODE (295).
- **(c) Notes:** catalog is an input to *first* resolution only; after that the run is frozen. This is the operator's
  confirmation mechanism ("did it use what I configured?").
- **(d) OQ:** interaction with EM-055's reparse-on-restart (1592) — the graph reparses but the concrete must come from
  the seal, not the reparsed node attrs. Two sources of truth must be ordered.
- **(e) Hazard (H2, load-bearing):** without an explicit seal-read branch, the determinism guarantee is prose only.
  The hot-reloadable alias catalog (Component C) opens a window the old immutable-graph-text model never had: a daemon
  restart mid-run + a catalog edit → one logical run mixes two catalog versions across its nodes (ADV-B F1 "the race").
  Also needs keep-last-good on catalog parse failure (ADV-B S3) — a Component-C detail, but the seal-read is what
  isolates already-sealed runs from a reload.

---

## Cross-cutting load-bearing hazards (code-verified against HEAD)

- **H1 (B6):** composite `(node_id, iteration)` seal key — back-edge re-dispatch confirmed at dot_cascade.go:1288–1303.
- **H2 (B7):** replay must read the seal before re-resolving — resume reparse confirmed at EM-055:1592; claim-time seal
  is only an alias string (modelpreference.go:220–223), concrete needs dispatch-time effective tool.
- **H3 (B3):** EM-056 clause 4 (1610) forbids reviewer-feedback + review-target in dot; dot_cascade.go writes both
  (:875 WriteReviewerFeedback `hk-wixms`, :1274 WriteReviewTargetVia). Reconciliation = the fix; must REPLACE 1610, not add beside it.
- **H4 (B2):** review-target renders bead id/title/BODY today (agenttask_chb028.go:610–613) + SHAs (:618–620). The new
  renderer MUST preserve task context for the reviewer; do not reduce to rubric-only.
- **H5 (B5):** ladder flip silently changes deployed `model:opus` beads (run-wide → implementer-only) and risks silent
  cost overrun (ADV-B S1/S2); explicit escalation that can't resolve must fail loud, not degrade (ADV-B F2).

## Open questions for change-design

1. **B1:** Does §6.1 Node gain a typed `role` field (today only `node.Role` threaded via ExtraContext, absent from the
   1162–1182 record), and does the renderer clause replace or generalize the B↔E contract (1584–1590)?
2. **B2:** Reviewer gets bead BODY verbatim (today) or a body reference? Verbatim avoids under-contexting.
3. **B3:** Is the dot feedback channel a disk file (reuse today's writer) or an in-record value the renderer reads —
   and is EM-056 clause 4 (1610) deleted or rewritten?
4. **B4/B6:** Which counter is "iteration" — the review-loop `iteration_count` (§6.1 context, 1225) — and is it the
   same key the seal uses? Define once.
5. **B5:** Does a per-run force override a per-node lock? Escalation scope whole-run vs per-role? (both OPEN in MODEL.md/ADV-B).
6. **B6/B7:** Durable home of the concrete seal — new §6.1 Run field, Run.context, or an event replayed from the git
   checkpoint trail (EM-032, 639)? And how does the seal-read branch order against EM-055's mandatory reparse (1592)?
7. **Scope:** does `standard-bead.dot` (embedded default, §7.5.1) need declared I/O so the default is the leak-proof
   path — an exemplar touch, not a fourth EM clause? (02-components §"Possible additional touch".)
