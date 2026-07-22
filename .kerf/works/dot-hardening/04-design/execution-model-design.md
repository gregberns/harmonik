# 04 — Change Design: `specs/execution-model.md` (Component B, reqs B1–B7)

> One step before normative text. For each requirement: (i) the exact clause that is
> **NEW / AMENDED / REPLACED** + id + line, (ii) the normative posture (1–3 sentences),
> (iii) back-compat impact, (iv) cross-file dependency. All line numbers are EM at HEAD
> (`specs/execution-model.md`, 1909 lines). Decision ids (D-Bn) per `DECISIONS.md`.
>
> **ID allocation.** Dot-mode clauses today run EM-055..EM-059 + EM-068; bookkeeping handles
> EM-060/EM-061; §4.11/§4.13 use EM-062..EM-067. New dot-mode ids continue from **EM-069**.
> Allocated here: **EM-069** (single renderer, §7.5.1) with lettered sub-clauses
> EM-069-REV / EM-069-FB / EM-069-ITER; **EM-070** (per-(node,iteration) seal, §7.5.1 + §6.1);
> **EM-071** (replay reads the seal, §7.5.1). The ladder flip is an in-place **rewrite** of the
> existing EM-012b-NODE (no new id).

---

## B1 — Single brief renderer (D-B1) — **NEW EM-069 + §6.1 Node.role; REPLACES the B↔E contract**

- **(i) Clauses.**
  - **NEW EM-069** "Single node-brief renderer" under §7.5.1, placed immediately after EM-055 (insert at L1597, before §7.5.2). It **REPLACES** the *B↔E brief-composition contract* prose (L1584–1590) — that block is struck and folded into EM-069. **AMENDS** EM-055 step 6 (L1581) and the §6.1 `Workflow.goal` record note (L1158): `goal` stops being an unconditional ExtraContext broadcast and becomes a *default-visible declared input* the renderer reads (tracks D-A3).
  - **NEW §6.1 Node record field `role : Role | None`** (insert at L1181, after `non_committing`), a typed field promoted from today's `node.Role` thread-through (`dot_cascade.go:1305–1316`, ExtraContext-only; absent from the L1162–1182 record). `Role` is the renderer's framing selector; MVH values `{implementer, reviewer}`, open-set forward-compatible (parallels `agent_type`, L1166).
- **(ii) Posture.** One renderer assembles ANY agentic node's brief from exactly `{node's declared inputs} + {node.prompt} + {node.role} + {run goal, if the node's declared/default input set includes it}`; the role selects framing (worktree discipline, reviewer read-only + verdict-emission instruction), not which builder runs. The renderer is the sole producer of the brief for every node and every tool — it subsumes both `buildAgentTaskContent` and `buildReviewTargetContent`. Because the brief carries only declared inputs, a node that does not declare the review rubric cannot render it: the leak is structurally unexpressible, not merely discouraged.
- **(iii) Back-compat.** Strict-additive. `role` absent → renderer infers framing from `agent_type` exactly as today; un-migrated `.dot` graphs render byte-identically because their default input set (D-A2, WG) reproduces the current `Body`+`ExtraContext` shape. The `goal` reframing is behavior-preserving for graphs that inherit the default set (goal is default-visible), and *closes* the re-leak vector for graphs that exclude it.
- **(iv) Cross-file.** **WG owns** the declared-input/output vocabulary and the default-input set by role (A1/A2/A3) — EM *consumes* the resolved input list, it does not define the graph syntax. **HC owns** the transport of the finished brief (C1 transport-only adapter); EM produces one representation, HC picks the envelope. **Manifest coupling (D-C5):** the renderer MUST emit the daemon-side typed `role→source-keys` manifest that HC's leak oracle asserts on — keep that emit surface in EM-069 so it is a production byproduct, not test-only.

## B2 — Reviewer brief keeps task context (D-B2, hazard H4) — **NEW sub-clause EM-069-REV**

- **(i) Clause.** **NEW EM-069-REV**, a sub-clause of EM-069 (§7.5.1). Amends nothing standalone; it constrains the reviewer role-framing of the single renderer.
- **(ii) Posture.** When `role = reviewer`, the renderer's default input set MUST include the bead **id, title, and body verbatim** (no truncation — the current `buildReviewTargetContent` behavior, `agenttask_chb028.go:610–613`) plus the diff **base/head SHAs** (`:618–620`), alongside the rubric. The reviewer MUST NOT be reduced to rubric-only: it needs the task the diff is meant to satisfy to judge the diff. The verbatim body (not a bare reference) is normative — a reference regresses into under-contexting (resolves the OQ-B2 body-vs-reference question in favor of verbatim).
- **(iii) Back-compat.** Behavior-preserving — the reviewer already receives exactly this content today via `review-target.md` (`## Bead` + `## Diff range`). The change is *where* it is specified (the renderer's reviewer framing) not *what* is delivered.
- **(iv) Cross-file.** Git diff stays the **code channel** (base/head SHAs → worktree), NOT a variable/edge payload (MODEL.md "what deliberately stays out of variables"). WG's reviewer default-input set (D-A2) MUST enumerate `{task_context(id/title/body), diff SHAs, rubric}`; EM's EM-069-REV binds the renderer to honor it.

## B3 — Feedback as a produced value on the back-edge (D-B3, hazard H3) — **REPLACES EM-056 clause 4; AMENDS EM-015d-RFD/RIA**

> This is the reconciliation the whole component exists for. Stated explicitly so the reviewer
> sees the contradiction resolved, not duplicated: **one REPLACE, two AMENDs, zero additions
> that leave the old prohibition standing.**

- **(i) Clauses.**
  - **REPLACED — EM-056 §7.5.2 clause 4 (L1610).** The current text — *"A `dot` run MUST NOT produce `reviewer-feedback.iter-<N>.md` / `review-target.md`; their absence is not an authoring error"* — is **struck in full** and replaced by a pointer to the new value channel: *"Reviewer→implementer feedback in a `dot` run is a produced value on the back-edge per EM-069-FB; the on-disk `reviewer-feedback.iter-<N>.md` file, when written, is claude's transport detail per §7.5.1.EM-069-FB and [handler-contract.md] C1, not a review-loop artifact."* This directly resolves the code/spec contradiction: `dot_cascade.go:875` (`WriteReviewerFeedback`, `hk-wixms`) and `:1274` (`WriteReviewTargetVia`) already write both files in dot mode, violating L1610 as written.
  - **NEW — EM-069-FB**, a sub-clause of EM-069 (§7.5.1): the value-on-back-edge channel proper.
  - **AMENDED — EM-015d-RFD (L364–383) and EM-015d-RIA (L386–413):** add one scoping sentence to each stating that the *"review-loop-mode-only"* framing of these on-disk artifacts governs the **review-loop driver**, and does NOT forbid a `dot` run from writing a `reviewer-feedback.iter-<N>.md` file **as claude transport** for the EM-069-FB value channel. No mechanism inside RFD/RIA changes.
- **(ii) Posture.** On a `REQUEST_CHANGES` back-edge the reviewer's produced `verdict`+`notes` values (typed per D-A4: `verdict:enum(APPROVE,REQUEST_CHANGES,BLOCK)`, `notes:string`) bind the implementer's declared `feedback` input and are rendered by the single renderer (EM-069) into the resume brief — identically across claude/pi/codex. The value is canonical; the WHAT lives in the Run record. The **file is retained ONLY as claude's transport** (claude receives the resume brief by paste-inject referencing a worktree file, not argv — `pasteinject.go`, `agenttask_chb028.go` resume block); pi/codex carry the same rendered value in the positional seed argv (`agentseedprompt.go:44`). The prohibition and the channel MUST NOT both be live — that would re-create the exact contradiction (hazard H3).
- **(iii) Back-compat.** Reconciling — it makes the spec match shipped code (`hk-wixms`). No deployed `dot` graph breaks; runs that already write the file keep writing it, now spec-sanctioned as transport. review-loop mode (EM-015d) is untouched: its RFD/RIA delivery over the AIS input port (per the 2026-07-14 amendment) is unchanged.
- **(iv) Cross-file.** WG owns the edge that carries the value (A2 producer→consumer on the back-edge; A4 verdict enum). HC owns the per-tool delivery envelope of the rendered brief (C1). EM owns only: the value is canonical, the file is transport, the prohibition is gone.

## B4 — Iteration-1 vs resume binding (D-B4) — **NEW sub-clause EM-069-ITER**

- **(i) Clause.** **NEW EM-069-ITER**, a sub-clause of EM-069/EM-069-FB (§7.5.1).
- **(ii) Posture.** "Iteration" is the review-loop `iteration_count` (§6.1 `Run.context`, L1225) — the SAME counter in the HC-004 launch-idempotency tuple `(run_id,node_id,phase,iteration_count)` and the SAME counter the seal keys on (B6). Defined once here. At `iteration_count = 1` the implementer's `feedback` input is **unbound → its brief section is omitted**; on a bounce-back (`iteration_count ≥ 2`) it is **bound → rendered**. This is what makes a deterministic REQUEST_CHANGES→resume→APPROVE round-trip *forceable* once the leak (B1) is gone.
- **(iii) Back-compat.** Behavior-preserving — mirrors today's phase selection (`dot_cascade.go:1288–1303`: `iterationCount ≤ 1` → `ImplementerInitial`, else `ImplementerResume`); the resume-only feedback section already appears only on iteration ≥ 2. The clause makes an implicit code behavior normative.
- **(iv) Cross-file.** None new. Pins the shared counter definition that WG's default-input resolution (feedback optional-when-unbound) and HC's per-tool resume transport both read.

## B5 — Model-resolution ladder flip (D-B5, hazard H5) — **REWRITES EM-012b-NODE + precedence block**

- **(i) Clauses.**
  - **REWRITTEN — EM-012b-NODE (L295).** Today it declares the per-node attr as **tier 0, ABOVE the per-bead label** ("the node's attribute value takes precedence over the run-level `ModelPreference`"; verified in code — `nodeModelForHarness` at `dot_cascade.go:1182` returns `node.Model`, layered over `resolvedModel` at `:1351`, so `.dot model=sonnet` overwrites a per-bead `model:opus`). The rewrite **inverts** this: node `model=`/`effort=` becomes a **soft default that sits BELOW a per-bead escalation and a per-run force**, overridable unless the node carries a `model_locked` opt-out.
  - **REWRITTEN — precedence summary block (L297–306).** New order, highest→lowest, stated unambiguously:
    ```
    tier 0   per-run force            --force-model / --force-effort (operator, one-shot)   [ISSUES #2 — force-vs-lock]
    tier 1   per-bead escalation      model:<alias> / effort:<lvl> label (run-wide; @node scoping D-B6)
    tier 2   per-node attr            model="…" / effort="…"  (UNLESS model_locked)
    tier 3   per-project config       .harmonik/config.yaml
    tier 3.5 operator env var         HARMONIK_CLAUDE_MODEL / HARMONIK_CLAUDE_EFFORT
    tier 4   per-agent-type compiled default
    tier 5   built-in fallback (empty)
    ```
- **(ii) Posture.** Task-specificity beats file-position: a per-bead "use Opus" escalation MUST beat the workflow file's stage-typical `model=` default. The node default is a floor authors set for the common case; `model_locked` is the author asserting "cheap even for hard beads," which the escalation band (tier 1) respects but the operator force (tier 0) MAY override.
- **(iii) Back-compat.** **NOT behavior-preserving for a node that carried `model=` intending to win over a label** — this is the flip. Blast radius is mitigated by D-B6 (a **bare** `model:opus` label STAYS run-wide, so most deployed labels are unaffected) and by beads being OFF this phase (blast radius likely nil). Flagged to **ISSUES #3** (ladder-flip migration) — confirm no deployed bead relies on a node `model=` winning over a label. An explicit escalation that cannot resolve MUST **fail loud, not silently degrade** (hazard H5 / ADV-B F2) — but that fail-loud lives HC-side (C2), EM only sets the precedence order.
- **(iv) Cross-file.** **WG owns** the override-surface serialization and the `model_locked` marker (A5; serialization is **[OPERATOR] → ISSUES #1**, recommend a fenced `harmonik` YAML block in the bead body carrying per-node `{tool,model,effort,locked}`). **HC owns** the resolution of an alias→concrete for every tool and the fail-loud-on-explicit-miss rule (C2/D-C3). **EM owns** ONLY the tier order. **Escalation scope (D-B6) RESOLVED:** per-node addressing by node id is primary; a bare/flat label is run-wide.
  - **[OPERATOR] — do NOT decide here.** Whether tier-0 force overrides a `model_locked` node → **ISSUES #2** (D-B7; recommendation recorded: force overrides, per-bead label respects the lock). The precedence block leaves tier 0's interaction with lock as a pointer to ISSUES #2, not a decided clause.

## B6 — Per-(node, iteration) concrete-selection seal (D-B8, hazard H1) — **NEW EM-070 + §6.1 Run.node_model_seal**

- **(i) Clauses.**
  - **NEW §6.1 Run record field** (insert at L1229, after `template_params`):
    ```
    node_model_seal : Map<(node_id, iteration_count), {tool, model, effort}> | None
                      -- per-(node,iteration) concrete model seal; written at first dispatch
                         of each (node,iteration) after alias→concrete resolution for the node's
                         effective tool; None until first agentic dispatch; frozen per key once written
    ```
  - **NEW EM-070** "Per-(node, iteration) concrete-selection seal" under §7.5.1 (insert after EM-069).
- **(ii) Posture.** At the **first dispatch** of each `(node_id, iteration_count)`, after the node's alias→concrete resolution completes for that node's **effective tool** (a dispatch-time fact — `strong.claude ≠ strong.pi` in mixed-harness runs, so a claim-time seal of an opaque alias string per `modelpreference.go:220–223` has zero determinism value), the daemon MUST write the resolved `{tool, model, effort}` into `node_model_seal` keyed by the **composite** `(node_id, iteration_count)`. The composite key is **load-bearing (hazard H1):** the REQUEST_CHANGES back-edge re-dispatches the SAME implementer node at iteration 2,3 (`dot_cascade.go:1288–1303`); an iteration-blind `node_id`-only key would overwrite iteration 1's concrete, and replaying iteration 1 would then bind iteration 2's model. Each key is written once and frozen; a re-dispatch of an already-sealed key MUST read (B7), never re-write.
- **(iii) Back-compat.** Additive — new nullable Run field, `None` for every existing run and for any run before its first agentic dispatch. `model_preference` (L1219, the claim-time run-level pair) is unchanged and remains the run-level default; the seal is the per-dispatch record layered on top. No existing replay path regresses (they simply had no seal to read; see B7's amendment for the new read branch).
- **(iv) Cross-file.** **HC owns** the alias→concrete resolution that produces the sealed value (C2: the resolved per-tool concrete reaches `rc.model` for EVERY tool — delete the claude-only guard `nodeModelForHarness` `dot_cascade.go:1182`) and the catalog (`.harmonik/config.yaml models.aliases`, hot-reloadable, keep-last-good on parse failure). **EM owns** the seal's shape, key, write-timing, and durable home (the Run record). EM-032 (L637) deterministic-replay contract is the general umbrella; EM-070 is the model-concrete specialization it never covered.

## B7 — Replay reads the seal (D-B9, hazard H2) — **AMENDS EM-055 resume semantics; NEW EM-071**

- **(i) Clauses.**
  - **AMENDED — EM-055 "Resume semantics" (L1592).** Today: resume MUST re-execute steps 1–4 (reparse the `.dot`), MUST NOT trust a serialized parse tree. Add a **seal-read branch:** the graph still reparses as today (unchanged), BUT at dispatch of any `(node_id, iteration_count)` whose key is present in `node_model_seal`, the daemon MUST read the sealed concrete and MUST NOT recompute model/effort from the live catalog/labels/config. Order-of-truth stated explicitly: the reparsed node attrs feed FIRST resolution only; for an already-sealed key the seal wins.
  - **NEW EM-071** "Replay reads the seal" under §7.5.1, cross-referencing EM-070, EM-055, and EM-012b-NODE (L295).
- **(ii) Posture.** A replay/resume MUST read the sealed concrete for `(node_id, iteration_count)` and MUST NOT re-resolve from the live catalog (hazard H2). Without this branch the determinism guarantee is prose only: the hot-reloadable alias catalog (HC/C2) opens a window the old immutable-graph-text model never had — a daemon restart mid-run + a catalog edit → one logical run mixing two catalog versions across its nodes (ADV-B F1 "the race"). The catalog is an input to *first* resolution only; after that the run is frozen. This is the operator's replay/rerun confirmation ("did it use what I configured?").
- **(iii) Back-compat.** Additive branch — runs with `node_model_seal = None` (all pre-change runs) fall through to the existing re-resolution path unchanged; only sealed keys take the new short-circuit. The mandatory reparse (L1592) is preserved intact; the seal-read layers over it without removing it.
- **(iv) Cross-file.** **HC owns** keep-last-good on catalog parse failure (ADV-B S3) so a mid-edit reload never adopts a broken catalog; **EM owns** the isolation — the seal-read is what protects an already-sealed run from ANY reload. WG unaffected.

---

## Requirement → clause summary

| Req | Decision | Clause action | Id | EM line |
|---|---|---|---|---|
| B1 | D-B1 | NEW renderer; REPLACES B↔E contract; +§6.1 `Node.role`; AMENDS EM-055 step 6 + goal note | EM-069 | 1584–1590 / 1581 / 1158 / 1181 |
| B2 | D-B2 | NEW sub-clause (reviewer framing) | EM-069-REV | 1610 (was) → 7.5.1 |
| B3 | D-B3 | **REPLACES** EM-056 cl.4; NEW value channel; AMENDS EM-015d-RFD/RIA | EM-069-FB | **1610** / 364–413 |
| B4 | D-B4 | NEW sub-clause (iter binding) | EM-069-ITER | 7.5.1 (counter def L1225) |
| B5 | D-B5 | **REWRITES** node tier + precedence block | EM-012b-NODE | 295 / 297–306 |
| B6 | D-B8 | NEW seal clause + §6.1 Run field | EM-070 | 7.5.1 / 1229 |
| B7 | D-B9 | AMENDS EM-055 resume; NEW replay clause | EM-071 | 1592 / 7.5.1 |

## Deferred — do NOT decide in this component (pointers only)

- **ISSUES #1 — override serialization (D-A6, WG concern).** Fenced `harmonik` YAML block in the bead body vs node-addressed labels. EM's precedence block references it as an input; the block's *parse home* and *shape* are WG/bead-integration + operator. Recommend the block.
- **ISSUES #2 — force-vs-lock (D-B7, [OPERATOR]).** Does tier-0 `--force-model` override a `model_locked` node? Recommendation recorded (force overrides; per-bead label respects lock); EM-012b-NODE's precedence block leaves this as a pointer, not a decided clause.
- **ISSUES #3 — ladder-flip migration (hazard H5).** Confirm no deployed bead relies on a node `model=` beating a label; blast radius likely nil (beads OFF this phase, bare label stays run-wide per D-B6).

## Under-specified (flag for Spec-Draft)

- **`Role` enum openness.** MVH `{implementer, reviewer}` with open-set forward-compat (parallels `agent_type`) — but the renderer's framing table must enumerate the reviewer read-only + verdict-emission instructions concretely. Decide in Spec-Draft whether an unknown `role` fails load or falls back to `agent_type`-inferred framing (recommend the latter, matching WG-003's open-set posture — but note ADV-B S2 warns an unvalidated open-set target silently no-ops).
