# C2 — Codex adapter (exec launch, capture-id, spawn-per-turn re-task) — change spec

## Requirements (from 03-components.md)
R2.1 `codex exec --json --sandbox workspace-write -a never -C <worktree>`, task via stdin/arg.
R2.2 capture `thread_id` from first `thread.started`. R2.3 `Retask` = `codex exec resume <thread_id>`.
R2.4 completion via process-exit + `turn.completed`/`turn.failed`; `Teardown` no-op. R2.5 ensure
`Refs:<bead>` trailer (instruct+verify, deterministic commit-after-exit fallback). R2.6 `DetectReady`
on `thread.started`/`turn.started`. R2.7 heartbeat/liveness via `Completion()=ProcessExit` bypass (or
mapped emitter).

## Research summary
`codex exec` is a one-shot run-to-exit JSONL surface (`04-research/codex-cli/findings.md`): stdout =
JSONL under `--json` (`thread.started{thread_id}`, `turn.completed{usage}`, `turn.failed`); progress
on stderr. Caller CANNOT mint the session id (NOT_PLANNED #25111) → capture `thread_id`. Resume =
`codex exec resume <id> "<followup>"`. Unattended = `--sandbox workspace-write -a never` (worktree is
the sandbox boundary; `--yolo` unnecessary). codex's commit is a non-deterministic model decision
(`git add -A` footgun #8548) → don't rely on it. This makes the codex adapter **spawn-per-turn**, the
single biggest shape difference from claude — but structurally identical to claude's iter≥2
(`claude --resume`), so the shared loop accommodates it (`04-research/seam-design/findings.md`).

## Approach
Implement `CodexHarness` (satisfies C1 `Harness`, `AgentType()==AgentTypeCodex`):
- **LaunchSpec:** binary `codex` (resolvable path, default `codex`); argv
  `exec --json --sandbox workspace-write -a never -C <worktree>` for the initial turn, and
  `exec resume <thread_id> --json --sandbox workspace-write -a never -C <worktree>` for iter≥2. Task
  text delivered via stdin (`codex exec -` reads stdin) or a positional prompt arg; the seed prompt
  instructs codex to read `.harmonik/agent-task.md`, do the work, and **commit with the
  `Refs:<beadID>` trailer**. env applies the C3 credential strip.
- **Session id (Captured):** run in a substrate session whose stdout is parsed; the adapter reads
  the first `thread.started` line, extracts `thread_id`, and records it as the run's harness session
  id (mapping harmonik run UUID → codex thread_id in run state). `Retask` uses it.
- **Completion (ProcessExit):** the run is done when the substrate session's process exits;
  `turn.completed` (success) / `turn.failed` (failure) refine the outcome. `Teardown` is a no-op.
- **DetectReady:** true on the first `thread.started`/`turn.started` event.
- **Commit-trailer guarantee:** after exit, the shared commit-detection (area F) checks worktree HEAD
  vs parent. If codex committed with the trailer → normal merge. If codex **edited but did not commit
  (or committed without the trailer)** → the adapter performs a deterministic commit-after-exit:
  `git -C <worktree> add -A && git commit -m "<bead title>\n\nRefs: <beadID>"`. If codex made **no
  edits** → noChange path fires (shared, unchanged).
- **Substrate:** run `codex exec` inside a tmux window (reuse the harness-blind substrate verbatim for
  operator inspectability), capturing stdout. (Alternative: plain subprocess — deferred; tmux keeps
  parity. See C1 U1.)

## Files & changes
- **NEW** `internal/daemon/codexlaunchspec.go` — `buildCodexLaunchSpec` (the codex `LaunchSpec`).
- **NEW** `internal/handlercontract/harness_codex.go` — `CodexHarness` impl + JSONL event parser
  (`thread.started`/`turn.*`/`item.*`/`error`).
- **MODIFY** `internal/daemon/` run-state — store the captured `thread_id` keyed by run id.
- **MODIFY** `internal/handlercontract/adapterregistry_hc012.go` — register `CodexHarness` for
  `AgentTypeCodex`.
- **REUSE unchanged:** tmux substrate, worktree mgmt, commit-detection, merge — no edits.

## Acceptance criteria
- AC2.1 Against the **codex twin** (C6), an initial run produces argv
  `codex exec --json --sandbox workspace-write -a never -C <wt>` (asserted) and the adapter captures
  the twin's emitted `thread_id`.
- AC2.2 A second (review) iteration produces `codex exec resume <captured_id> ...` (asserted).
- AC2.3 When the twin commits with `Refs:<bead>`, the shared merge lands it; the run is marked done on
  process exit + `turn.completed`.
- AC2.4 When the twin edits but does NOT commit, the adapter's commit-after-exit creates a commit
  carrying `Refs:<bead>` (asserted by `git log` trailer grep).
- AC2.5 When the twin makes no edits, the noChange path fires and the bead is reopened (shared logic).
- AC2.6 `CodexHarness.Completion()==CompletionProcessExit`, `SessionIDPolicy()==SessionIDCaptured`.

## Verification
```
go test ./internal/handlercontract/... ./internal/daemon/... -run 'Codex'
# scenario (twin substrate): submit a codex-selected bead to a scratch queue; assert
#   .harmonik/events/events.jsonl shows run_started -> (no /quit) -> run_completed with a Refs: commit
```

## Error handling / edge cases
- No `thread.started` line before exit → adapter errors "codex session id not captured"; run fails
  closed (cannot resume without an id).
- `turn.failed`/`error` event → run_failed with the codex error message surfaced.
- `--json`+`--output-schema` silently ignored under MCP servers (#15451) → spec forbids MCP servers in
  the codex launch env for the structured path; document in C6.
- codex exits 0 but made no commit and no edits twice → treated as noChange twice → investigation
  (shared failure-triage rules apply).

## Migration / back-compat
codex is only reached when C4 selection resolves to it; default stays claude. No claude path changes.
