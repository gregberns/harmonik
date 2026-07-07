# quality-system / `core-loop-proof` — Analysis + Technical Design

Combined codebase map (analyze pass) and technical approach (the "design" the operator asked for), scoped
to the `core-loop-proof` chunk ONLY. Source of truth for what "correct" looks like, which chunk 2
(`scripted-twin`) later imitates.

## 1. What already exists — reuse, do NOT rebuild

| Piece | Path | What it gives us |
|---|---|---|
| **Scratch-daemon harness** | `scripts/scratch-daemon.sh` (+ `docs/scratch-daemon-runbook.md`) | Fully-isolated 2nd daemon keyed off scratch path (own socket/tmux/pid/binary/beads). `init`/`cycle`/`up`/`down`/`status` + **`batch`** (submit beads/queue-file → structured pass/fail) + **`feedback`** (scratch failures → deduped MAIN-repo beads). Guards: `guard_path`, `assert_not_supervised`, never `pkill harmonik`. **This is the substrate for the whole chunk.** |
| **Env dials** | `SCRATCH_MAX_CONCURRENT`, `SCRATCH_WORKFLOW_MODE`, `SCRATCH_DAEMON_FLAGS` | Configure the scratch daemon per matrix cell (concurrency, workflow-mode, extra daemon flags). |
| **Event stream** | `harmonik subscribe --json` (`cmd/harmonik/subscribe.go`), `events.jsonl` | Typed events — the ONLY correctness source (memory: worker activity is in events, not stderr). |
| **remote-test-pyramid runner-seam + twin foundation** | kerf work `remote-test-pyramid` (10/10 done) | The runner abstraction and twin scaffolding the testbed assumes — reuse its seam for the local-vs-remote parity assertions. |
| **`internal/schedule`** | `internal/schedule/{clock,store,types}.go` | Deterministic clock/store primitives if the harness needs time control (kept minimal here). |
| **orphaned `harmonik smoke`** | `scripts/smoke-scratch.sh`, `scripts/scratch-daemon-smoke.sh` | Prior smoke intent to fold the acceptance run into. |

## 2. Affected seams the harness asserts against (factual map)

- **Dispatch / worker-select / model-selection** — `internal/daemon/workloop.go`. Emits `model_selected`,
  `model_class`, `model_tier`, `worker_id/host/name`. This is where the pi-model-leak class (C4) lives:
  the assertion for gap 1 reads `model_selected` per run and compares to the configured model for the
  bead's harness family.
- **Sandbox gate / provider comms** — `internal/daemon/sandboxgate.go` (+ `sandboxgate_hkr4p0l_test.go`).
  The srt-on-remote misapplication (C2) and sandbox-blocks-provider (C5) bugs live here. Gap 2/3 assert
  `tool_call` events fire and a real content change lands; a `content:null`/no-`tool_call` response
  surfaces an explicit failure event, not a silent no-commit.
- **Remote runner / reverse tunnel** — `internal/daemon/reversetunnel.go`, `reviewloop.go` (tcp:// path).
  Gap 2 runs the SAME bead through the remote runner and asserts the event sequence matches the local
  cell (same seam, no local-only wiring skipped).
- **DOT reviewer verdict feedback** — `internal/daemon/reviewloop.go`. Emits `review_*` events + verdict.
  Every cell asserts a verdict is produced and fed back and the bead reaches a terminal transition
  (`run_completed`/`merged` or a bounded safe-fail), never an unbounded hang.
- **queue-submit field fidelity** — `cmd/harmonik/run_via_daemon.go` / queue-submit rpc rebuild (C7).
  Gap 4 submits a fully-specified item and asserts the dispatched worker carried every field.
- **agent_ready startup gates** — `EventTypeAgentReady` / `EventTypeAgentReadyTimeout` /
  `EventTypeAgentReadyStallDetected`. Gap 5 (flag-gated Claude) asserts a real git-worktree launch reaches
  `AgentReady` past the folder-trust/permissions/onboarding modals (PR-19 class C8).

## 3. Technical approach — the matrix acceptance harness

**Shape.** A thin **assertion library + matrix runner** layered on `scratch-daemon.sh batch`. The runner:

1. `scratch-daemon.sh init` + `cycle` a clean isolated daemon from the crew's worktree build.
2. For each matrix cell `{harness ∈ claude, codex, pi} × {substrate ∈ local, remote(tcp://)}`:
   - Compose a **known seed bead** (fixed title/body/label, configured model + harness pinned) and submit
     via `batch`, with per-cell env dials (`SCRATCH_WORKFLOW_MODE`, remote flags via `SCRATCH_DAEMON_FLAGS`).
   - Tail `harmonik subscribe --json` for that run_id into a per-cell event capture.
   - Run the **assertion library** over the captured events (the 5 gap checks below).
   - Green → record pass; red → `scratch-daemon.sh feedback` files a deduped MAIN-repo bead.
3. Print a per-cell green/red grid + overall exit code. Reproduce across a clean reset (step 1 re-run).

**Cell economics / gating.**
- pi + codex × {local, remote} = 4 cells run by default (token-cheap).
- claude × {local, remote} = 2 cells behind `--enable-claude` (opt-in, minimal) — token crunch.
- Remote cells require a reachable tcp:// worker; if none, **skip loudly** (distinct SKIP state, not green).

**The 5 gap assertions (event-stream, not stdout):**

| Gap | Assertion (from events) | Key events |
|---|---|---|
| 1 model-per-family | `model_selected` for the run == configured model for the bead's harness family; a `model=` node-pin for one family does NOT appear on another family's run | `model_selected`, `model_class`, `model_tier` |
| 2 remote==local | For the same seed bead, the remote-cell event sequence is equivalent to the local-cell sequence (same seam; terminal outcome matches) | full run_* sequence, `worker_host` |
| 3 provider-thru-sandbox | ≥1 `tool_call` fired AND worktree HEAD advanced with real content; a `content:null`/no-`tool_call` provider reply → explicit failure event (not silent no-commit) | `tool_call`, `merge_commit_hash`, run failure event |
| 4 field-fidelity | Submitted `{workflow_ref, workflow_mode, model, harness}` all present on the dispatched run; no hardcoded review-loop override | dispatch/run_started fields |
| 5 claude agent_ready | Real git-worktree claude launch emits `AgentReady` (no `AgentReadyTimeout`/`StallDetected`) past startup modals | `EventTypeAgentReady`, `...Timeout`, `...StallDetected` |

**Assertion-library boundary (load-bearing for chunk 2).** The library is a standalone module (jq-based or
a small Go test binary) that takes a captured event stream + expected-cell spec and returns a typed
pass/fail per gap. Chunk 2's scripted twin must be able to satisfy this exact library — so the "correct"
contract is codified HERE, in the assertion specs, not in prose.

**Reuse discipline.** No new isolation machinery — `scratch-daemon.sh` owns isolation. No new event types
— assert on existing ones (add one only if a gap is genuinely unobservable, and flag it). No Docker (that
is chunk 3). The remote seam is exercised through the remote-test-pyramid runner, not a new abstraction.

## 4. Constraints carried from problem-space

Token-gated Claude rows · build-in-own-worktree on `epic/core-loop-proof` · 24h rule (build against current
live binary) · never touch the fleet daemon · remote row skips loudly when no tcp:// worker · all checks
from the event stream.

## 5. Recent git activity (relevant)

The pi-model-leak fixes (hk-6atjk PATH, hk-lfrub DOT model-pin scoping, hk-pkugu launch-model e2e) landed
this week (`daemon-20260705-*`) — those are the exact regressions gap 1/3 must lock down. The srt-on-remote
fix (hk-ybuts / 37eca951) is the gap-2 regression. All are unproven out-of-band today.
