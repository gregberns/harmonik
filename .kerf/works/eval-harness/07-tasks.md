# Tasks — eval-harness build

All beads: label `codename:eval-harness`, unassigned, type task. **No epic parent** (avoids the
epic-dep-blocks-dispatch footgun). Dep-ordered so the collector + DOT land before acceptance.

## EH1 — results collector (the one genuinely-new Go piece)
Pure post-run collector (O3 lean). Read `<project>/.harmonik/events/events.jsonl`, group by
`run_id`, and for each eval run emit one flat record (schema DESIGN §1.3) to
`.harmonik/eval-results.jsonl`. Join:
- wall_time_s = `run_completed.ended_at − run_started.started_at` (RFC3339, same run_id).
- implement_time_s = `implementer_phase_complete − run_started` (state_* events are NOT emitted —
  DESIGN §1.1 correction; use implementer_phase_complete).
- model + harness from the `harness_selected` event (agent_type, tier) + `harnesses.pi.model` /
  judge model.
- pass + check_kind + commit_sha from the grade node's `outcome_emitted` payload.
- task_id + difficulty from the bead labels (`br show`).
Read-only over the log, off the daemon hot path. Deterministic. This is the only new logic.
**Depends on:** nothing.

## EH2 — eval-bead.dot + grade shell-node + non-gating judge node
Author `eval-bead.dot` at the project dir so `dot:eval-bead` resolves it (DESIGN §1.2). Topology:
`start → implement(agentic, model-under-test) → grade(non-agentic shell: restore committed test from
read-only path [O1], then run the task's deterministic check; exit0→judge, exit≠0→record-fail,
mandatory unconditional fallback LAST) → judge(agentic reviewer, model=claude-opus-4-8, 1-5 rubric
into review.json, unconditional → record-pass, NEVER gate-loops) → record-pass / record-fail
(terminal noop/shell)`. No fix-loop (one-shot capability). Uses only existing node/edge grammar +
the D5 edge dialect (equality/&& only, mandatory last fallback edge). Palette copy may also land at
`.harmonik/workflows/eval-bead.dot`. Wire routing via labels `workflow:dot` + `dot:eval-bead`.
**Depends on:** nothing (parallel with EH1).

## EH3 — config / routing for model-under-test selection
Document + set `harnesses.pi.{provider,model,api_key_env}` for the baseline/ornith model in
`.harmonik/config.yaml` (existing `ResolvePiConfig`; required, zero baked defaults). Define the
label recipe a task bead needs: `workflow:dot dot:eval-bead harness:<pi|claude-code>`. Claude
baseline needs no config. NOTE: pointing Pi at the ornith endpoint (DESIGN O2) is a separate
verify — out of scope here; this task covers the routing seam + baseline config only.
**Depends on:** EH2 (the DOT must exist for the routing labels to mean anything).

## EH4 — aggregation report
`jq` group-by `(model, difficulty)` over `eval-results.jsonl` → pass-rate, median wall_time_s, mean
judge_grade. Shell one-liner / small script acceptable for v1 (optionally `harmonik eval report`).
**Depends on:** EH1 (needs the collector's record format).

## EH5 — acceptance: run the 8 tasks through eval-bead.dot on a baseline model
Run the 8 curated `codename:eval` task beads through `eval-bead.dot` on a baseline model
(`harness:claude-code`), run the collector, and produce `eval-results.jsonl` with 8 records; then
the aggregation report over them. Proves the full chain end-to-end.
**Depends on:** EH1, EH2, EH3, EH4.

## Dep graph
```
EH1 ─┐
EH2 ─┼→ EH3 (dep EH2)
     ├→ EH4 (dep EH1)
     └→ EH5 (dep EH1, EH2, EH3, EH4)
```
