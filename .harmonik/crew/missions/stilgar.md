<!-- Mission handoff — locked 6-field schema -->
```yaml
schema_version: 1
crew_name: stilgar
queue: stilgar-q
epic_id: hk-9jdid
goal: "Land the eval-metrics WS1 plumbing: make the run log carry a trustworthy per-run model + emit a general post-run session-data record. Two ready children under epic hk-9jdid, dispatch serially (B1 then B2) — they share the metrics surface."
captain_name: captain
```

## Lane: eval-metrics WS1 (codename:eval-program)

You own epic **hk-9jdid** on queue **stilgar-q**. Two ready beads, dispatch **serially** (B2 builds on B1's model resolution):

- **B1 — hk-eval-prog-model-on-log-bh2o7 (P1):** the run log's `harness_selected` event (harnessresolve.go:120) carries agent_type+tier but NOT the model string. Add a `model` field to that payload OR emit a new `model_selected{run_id,model,harness}` event at launch resolution (Claude path dot_cascade.go:1229; Pi/codex launch-spec build). Gates a trustworthy cross-model record. Spec: plans/2026-07-03-eval-program/01-run-matrix-and-metrics.md Part 1.3b.
- **B2 — hk-eval-prog-sessiondata-hook-vmxrk (P1):** fire `sessiondata.Collect(runID,beadID,harness,model,project)` from `emitDone` (workloop.go:2810), after emitRunCompleted, in a goroutine, best-effort, off the hot path. Append ONE record per run to `<project>/.harmonik/session-data.jsonl` (schema in doc 01 Part 2.2). Refactor `internal/usage` (RunRecord+join+price) into a harness-general sessiondata collector; `harmonik usage` becomes a VIEW over the same jsonl. Spec: doc 01 Part 2.

## Discipline
- **Dispatch B1 first; hold B2 until B1 lands** — both touch the run-log/metrics surface; serial avoids a merge conflict.
- Standard queue submit to **stilgar-q** (never `main`). Daemon owns terminal transitions — do NOT pre-set in_progress or close on merge.
- **Triage your own failures** (Opus lane): on a run_failed, reproduce + diagnose before re-dispatch; escalate to captain only if a root cause is refuted ≥2× or a wedge survives ≥2 fix attempts (major-issue fan-out trigger).
- Post progress to **both** `comms send --to captain --topic status` AND `br` comments: on every bead close + a ≤10-min timer while dispatching (≤15-min when idle/draining) + boot and drain bookends.
- This is a FRESH manifest-path boot on the new daemon (81434151) — admiral is observing the agent-manifest rollout; boot cleanly via your soul.md/operating.md type folder.
