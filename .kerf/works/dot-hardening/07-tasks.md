# 07 — Tasks (implementation breakdown)

> Derived from the drafted specs (`05-spec-drafts/`), `DECISIONS.md`, and the component dependency
> order A→B→C. **Spec-first:** each task lands against a drafted clause; implement the code to match.
> **Beads are OFF this phase** (per `01-problem-space.md` constraints) — these are tracked in
> `plans/2026-07-13-code-revamp/COORD.md`, not live-dispatched. Two operator gates (ISSUES #1, #2)
> block only their own tasks; the spine (WS-A/B/D) is unblocked. Code anchors are from
> `03-research/*/findings.md` (verified file:line).

## Landing order (the spine)

The single renderer (WS-A) unblocks everything: it closes the leak, makes the round-trip forceable,
and emits the manifest the test oracle needs. Then feedback-on-edge (WS-B), then the test harness
(WS-D) can prove the round-trip, then model control (WS-C) lands independently. Bead-override (WS-E)
and exemplar/downstream (WS-F) trail.

**WS-A → {WS-B, WS-C} → WS-D**, with WS-E gated on ISSUES #1 and WS-C's seal gated on WS-C's ladder.

---

## WS-A — Single renderer + declared I/O + leak-as-load-error  (unblocks all)

**T-A1 — Declared `inputs`/`outputs` in the DOT language.**
Add optional `inputs="a, b"` / `outputs="name:type"` node attributes (WG-055/056/058); parse + retain
in the AST; add to the reserved-attr set (WG-031). Types: `string|number|bool|enum(...)`, only
`enum(...)` load-weighted. Distinct namespace from `context_keys`; distinct channel from template params.
- Files: `internal/workflow/dot/ast.go` (Node attrs), `parser.go`, `internal/workflow/loader.go`.
- Spec: WG-055, WG-056, WG-058, WG-031. Dep: none.
- Done: a `.dot` with declared I/O parses; an undeclared node still parses (role-default fallback);
  malformed `enum(...)` = shape error at load.

**T-A2 — The single brief renderer.**
Collapse `buildAgentTaskContent` + `buildReviewTargetContent` into one role-parameterized assembler:
brief = {declared inputs} + {prompt} + {role} + {goal}, framing selected by role. Promote `role` to a
typed Node field (§6.1 Node record). Reviewer framing keeps bead id/title/**body verbatim** + diff
base/head SHAs (H4).
- Files: `internal/workspace/agenttask_chb028.go` (:271 / :577 → one assembler), `internal/daemon/dot_cascade.go`
  (:1257-1286 brief write, :1305-1316 role/goal threading), `claudelaunchspec.go` (:308-311), `harnessregistry.go` (:207-234).
- Spec: EM-069, EM-069-REV. Dep: T-A1.
- Done: implementer and reviewer briefs are produced by the same code path; reviewer brief still contains
  the bead body + SHAs; existing `.dot` files render byte-equivalent via role-defaults.

**T-A3 — Per-node visibility = structural (the leak fix).**
The assembler can reach ONLY a node's declared/default inputs. `goal` becomes a default-visible declared
input subject to visibility (WG-044 amend, WG-057) — no unconditional broadcast (`workloop.go:4070-4081`).
- Files: `internal/daemon/workloop.go` (:4070-4081 goal threading), the assembler from T-A2.
- Spec: WG-057, WG-044 (amended). Dep: T-A2.
- Done: a node that does not declare `X` cannot have `X` in its brief, even if `X` is in `goal`/another node.

**T-A4 — Task/rubric value-source (D-FIX-1).**
Reviewer `rubric` input source = reviewer node `prompt` (move today's hardcoded generic rubric strings,
`agenttask_chb028.go:580-608`, into the standard-bead reviewer node prompt) + optional per-bead `rubric`
field (WS-E). Implementer `task` source = bead task text, excluding both. Implementer never declares `rubric`.
- Files: `internal/workspace/agenttask_chb028.go` (:580-608 rubric strings → reviewer node prompt),
  `internal/daemon/standard-bead.dot` (reviewer node gains the rubric prompt).
- Spec: EM-069-SRC, WG-057. Dep: T-A2. (Per-bead rubric field: WS-E.)
- Done: implementer brief never contains the reviewer rubric; reviewer brief carries the (moved) generic rubric.

**T-A5 — Daemon-emitted typed input manifest (D-FIX-2, the oracle surface).**
The renderer emits, per node dispatch, `{node_id, role, iteration_count, source_keys[]}` (top-level input
names only; `task_context` is one key — P2). Production byproduct, not test-only.
- Files: the assembler (T-A2), an event/record sink in `internal/daemon`.
- Spec: EM-069-MAN. Dep: T-A2.
- Done: every dispatch emits a manifest; `rubric` ∉ implementer `source_keys`.

**T-A6 — Load checks for declared I/O.**
Unbound-required-input; verdict↔edge compatibility (only on edges whose from-node is the verdict producer
— P4); override-names-a-real-node (with WS-E); back-edge cap = reference to WG-028 (no duplicate).
- Files: `internal/workflow/dot/validator.go`, `internal/workflow/loader.go`.
- Spec: WG-060. Dep: T-A1, (T-C-ladder for override check → WS-E).
- Done: an `APPROVE`/`APPROVED` typo on a verdict edge is a load error; an unbound required input is a load error.

## WS-B — Feedback as a value on the back-edge

**T-B1 — Reviewer produces `verdict`+`notes`; renderer binds them into the resume brief.**
The reviewer's verdict (enum) + notes become declared outputs; on REQUEST_CHANGES they bind the
implementer's `feedback` input, rendered by the single renderer identically across tools. Iter-1 unbound
(omitted), bounce-back bound (EM-069-FB/ITER). Iteration = review-loop `iteration_count`.
- Files: `internal/daemon/dot_cascade.go` (verdict read-back :1202, phase select :1288-1303), the assembler.
- Spec: EM-069-FB, EM-069-ITER, WG-056/058. Dep: T-A2.
- Done: on a bounce-back the implementer brief contains the reviewer's verdict+notes, produced by one code path.

**T-B2 — Replace the dot-mode feedback prohibition (EM-056 clause 4).**
Strike the "a dot run MUST NOT produce reviewer-feedback/review-target" clause; the value channel is
canonical. The on-disk `reviewer-feedback.iter-N.md` is retained ONLY as claude's transport (WS-D/HC-072).
Resolves the code/spec contradiction (`dot_cascade.go:875` already writes it, hk-wixms).
- Files: spec text (EM); `internal/daemon/dot_cascade.go` (:875 keep as claude transport), `agentseedprompt.go`, `pasteinject.go`.
- Spec: EM-056 (replaced), EM-015d-RFD/RIA (amended). Dep: T-B1.
- Done: dot-mode feedback flows as a value; no spec/code contradiction remains.

## WS-C — Model control (independent of WS-A/B)

**T-C1 — Ladder flip + pi/codex per-tool fix + alias-catalog lookup.**
Per-node `model=`/`effort=` becomes a soft default BELOW per-bead escalation / per-run force (EM-012b
rewrite). Delete the claude-only guard so the resolved concrete reaches `rc.model` for every tool
(`nodeModelForHarness` `dot_cascade.go:1182`). Add the alias catalog (`.harmonik/config.yaml
models.aliases`, hot-reload, keep-last-good). Three-way behavior: fail-loud on unresolvable explicit
escalation / degrade+warn on default-band miss / keep-last-good on parse fail; pi non-empty floor.
- Files: `internal/daemon/modelpreference.go` (ResolveModelPreference :190), `dot_cascade.go` (:1182, :1351),
  `pilaunchspec.go` (:284), `codexlaunchspec.go` (:207), `internal/daemon/projectconfig.go`.
- Spec: EM-012b (rewritten), HC-073, HC-055a (amended). Dep: none (parallel to WS-A).
- Done: a per-bead escalation beats a node `model=`; a pi/codex node model is honored; an unresolvable
  explicit escalation fails loud, not silently downgrades.

**T-C2 — Per-(node, iteration) concrete seal + replay reads it.**
New sealed Run field `node_model_seal: Map<(node_id, iteration_count), {tool, model, effort}>`, written at
first dispatch of each (node,iteration) after alias→concrete for the node's effective tool (HC-003 selects
tool before the seal write — P3). Replay/resume reads the seal, MUST NOT recompute from the live catalog.
- Files: Run record (§6.1) in `internal/daemon` / core, `modelpreference.go`, `dot_cascade.go` dispatch,
  the resume/replay path (`workloop.go` claim/resume).
- Spec: EM-070, EM-071, EM-055 (amended). Dep: T-C1.
- Done: a rerun reproduces the exact per-node concrete even after a catalog edit; iteration-2 does not
  overwrite iteration-1's record.

## WS-D — Test harness (proves the change)

**T-D1 — Unit-speed capture-recorder layer (matrix + oracle + c074 guard).**
Upgrade one capture stub to record the FULL built launch spec on first dispatch (no subprocess, no git).
Assert: (a) per-handler argv/binary matches the declared tool (c074 guard); (b) reviewer_harness override
routes each role to its declared tool; (c) leak oracle — `rubric` ∉ implementer manifest `source_keys`.
- Files: `internal/daemon/export_test.go` (LaunchSpecBuilder DI :165, capture stubs :877-917), `scenariotest`.
- Spec: HC-074. Dep: T-A5 (manifest), T-A2 (renderer) for the oracle to reach GREEN.
- Done: leak oracle is RED before T-A2/T-A3, GREEN after; c074 guard fails a mislabeled bead.

**T-D2 — One scenario-tagged twin round-trip.**
Keep the REAL launch-spec builder, tee into a recorder, swap only the executable to a handler-faithful
twin, real scratch git. Force REQUEST_CHANGES→resume→APPROVE via a role×iteration twin script; assert the
back-edge fired, the resume seed/brief references the feedback (pi/codex argv-faithful; claude = brief
written), cap-hit → close-needs-attention.
- Files: `internal/daemon/scenariotest`, `cmd/harmonik-twin-{claude,pi,codex}`, `//go:build scenario`.
- Spec: HC-074 (+ honesty limits stated). Dep: T-B1/B2 (feedback), T-A2.
- Done: the deterministic round-trip runs green in-process; the claude-argv limitation is stated, not hidden.

## WS-E — Per-node override in the bead  (GATED on ISSUES #1)

**T-E1 — Structured bead override block + node addressing.**
Implement the operator's chosen serialization (recommend: fenced `harmonik` block in the bead carrying
per-node `{tool, model, effort, locked}` + optional `task`/`rubric`; flat `model:<alias>` stays run-wide).
Bind node-addressed config to the ladder (WS-C) and the rubric field to T-A4.
- Files: bead record (`beadrecord.go:19-28` Labels/Description), `modelpreference.go`, the assembler (rubric field).
- Spec: WG-059, EM-069-SRC (rubric field), ISSUES #1. Dep: ISSUES #1 decision; T-C1; T-A4.
- Done: a bead can escalate one node by id and carry a separate rubric; override-names-a-real-node check fires.

**T-E2 — Force-vs-lock precedence  (GATED on ISSUES #2).**
Implement the chosen force/lock interaction (recommend: `--force-model` overrides `model_locked`; a bead
label respects it).
- Files: `modelpreference.go`, queue-submit force path.
- Spec: EM-012b (force-vs-lock NOTE), ISSUES #2. Dep: ISSUES #2 decision; T-C1.

## WS-F — Exemplar + downstream spec touches

**T-F1 — Standard-bead exemplar gains declared I/O.**
Update `specs/examples/standard-bead.dot` + sidecar + the embedded `internal/daemon/standard-bead.dot` so
the default graph is the leak-proof path (declared inputs, reviewer rubric prompt).
- Spec: D-A9. Dep: T-A1..A4.

**T-F2 — Downstream unchanged-spec touches** (from `06-integration.md`; COHERENT-WITH-FOLLOWUPS).
Mechanical amendments to specs that still assert the old posture:
- **Ladder-flip staleness** (per-node `model=` still called "highest/tier-0"): `workflow-graph.md:94,223,236`,
  `execution-model.md:1177`, `examples/per-node-model-effort.md:30`. (The WG-002 "[remainder unchanged]" leaves
  L94 stale — fix in the WG draft at finalize.)
- **Reviewer-prompt promotion** (still "accepted-but-inert"): `examples/authoring-notes.md:144-145`,
  `examples/README.md:413`, `examples/sentry-triage-faithful.dot:16`, `examples/plan-to-shipped-faithful.dot:26,97`.
- **C1 transport mechanism** — the drafts say claude feedback is "tmux paste-inject", but the M2
  `agent-input-substrate` migration already superseded paste-inject for daemon-run input with the AIS
  structured input port (`execution-model.md:411`, `process-lifecycle.md:772` PL-021d, `agent-input.md:183`
  AIS-011). **Update the mechanism noun to the AIS input port** in HC-072/HC-074 and re-check whether the
  claude delivery is now capturable (this may soften ISSUES #4 — see note there). The file-vs-argv point likely
  survives, but confirm against AIS.
- (soft) `workspace-model.md:933-934` — reference-covered via EM-015d's new carve-out; no edit needed.

**T-F3 — In-draft cross-ref + changelog fixes** (finalize-time, from integration):
- WG cites `§4.3 EM-056` for feedback → should target `EM-069-FB` (§7.5).
- EM cites `WG-042` for `model_locked` → defined in `WG-059`.
- EM cites `WG-014` for the verdict type → defined in `WG-058`.
- HC changelog fragment lacks a version number; `05-changelog.md` "Open reconciliations" should list the T-F2 sites.
- WG front-matter `version:` (0.1.0) vs table (0.3.1→0.4.0) — correct front-matter at finalize.

## Required test beads (spec-jig convention)

- **Scenario-test:** `scenario: execution-model.md — dot REQUEST_CHANGES round-trip forces resume with
  feedback and reaches APPROVE→close` (twin substrate; terminal = `close`, manifest shows `rubric` ∉
  implementer source_keys). = T-D2 + T-D1 oracle.
- **Exploratory-test:** `explore: handler-contract.md — queue submit with a per-node model escalation
  runs the named node on the escalated tool and the seal records the concrete` (observe `node_model_seal`
  + `model_selected` event). = T-C1/T-C2 surface.

## Sequencing summary

1. T-A1 → T-A2 → {T-A3, T-A4, T-A5} → T-A6   (input model; leak closed; manifest live)
2. T-B1 → T-B2                                (feedback on edge; prohibition replaced)
3. T-D1 (oracle RED→GREEN across 1) ; T-D2 (round-trip after 2)
4. T-C1 → T-C2                                (model control; seal; replay) — parallel to 1–2
5. ISSUES #1 → T-E1 ; ISSUES #2 → T-E2        (bead override; gated)
6. T-F1 ; T-F2 (from integration)            (exemplar; downstream)
