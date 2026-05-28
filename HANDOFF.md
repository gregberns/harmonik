<!-- PP-TRIAL:v2 2026-05-27 main — v68. DOT subsystem made functional end-to-end; daemon liveness bug fixed. Clean, all pushed. -->

Roadmap: [ROADMAP.md](ROADMAP.md). Cross-project rules: `~/.claude/CLAUDE.md`. Orchestrator rules: [docs/orchestrator-rules.md](docs/orchestrator-rules.md). Known workarounds: [docs/known-workarounds.md](docs/known-workarounds.md).

ROLE: You are the orchestrator. Delegate substantively. Keep the main thread minimal.

# Where we are (v68, 2026-05-27)

**Main at `05a0a0b`, clean, all pushed.** 22 commits this session. Work is **clean — not blocked**.

## What this session did (two big things)

1. **DOT workflow-mode is now functional end-to-end** for the review-loop topology. Previously `--workflow-mode=dot` loaded+validated the graph then fell through to single-mode (ran only the first node). Now the daemon **walks the graph** (`internal/daemon/dot_cascade.go`, `driveDotWorkflow`): start → implementer → reviewer → APPROVE/close or REQUEST_CHANGES/loop. Proven by `internal/daemon/workloop_dot_mode_e2e_hklphyf_test.go`. agentic + non-agentic nodes work; **gate + sub-workflow node execution are honest deferrals** (see below).

2. **Fixed the daemon `no_commit` false-kill bug (hk-tgqy5)** — this was the root of the dispatch friction. tmux exec's `sh` into `claude`, so during a think-phase the pane PID *is* claude with no children; the old liveness probe (`pgrep -P`, direct children only) saw none and the watchdog killed healthy agents mid-work (they committed 2–6 min *after* being declared failed). Fix: `hasChildProcess` now also matches when the pane PID itself is an agent command. `harmonik run` should be reliable again — prefer it over sub-agent dispatch going forward.

## Gate decision — RESOLVED (don't relitigate)

Cross-spec contradiction (EM-005b said gate deny→FAIL, CP-058 said deny→SUCCESS) is settled: **CP-058 wins** — an evaluated gate deny/allow/escalate is `status=SUCCESS`, cascade routes on `outcome.preferred_label`; `FAIL` only when the gate *cannot evaluate*. Confirmed by the Attractor spec + kilroy/fabro/agate impls (user said "do whatever those say"). Fixed in EM-005b, HC-058/HC-060, `gate_dispatch.go`, tests. Reviewer APPROVED.

## Next step (what the user wants)

**Run the first live DOT test:** `harmonik run --workflow-mode dot --workflow-ref specs/examples/review-loop.dot --beads <bead>` — drives a real bead through the DOT review-loop with a real claude agent (not a stub). User leaning toward a **throwaway/sacrificial bead** for the first run to validate the mechanism cleanly before pointing at real work. Arm a Monitor on `.harmonik/events/events.jsonl` for `node_dispatch_requested|node_dispatch_decided|run_completed|run_failed` to watch the graph walk live. Rebuild first: `go install ./cmd/harmonik`.

## Open beads filed this session (none blocking the live test)

- **hk-karlz** — build daemon-side gate evaluator (no GateEvalFunc provider / ControlPoint-registry loading exists; gate *nodes* error until this lands). The real remaining gap for full gate support.
- **hk-9dnak follow-up (hk-1xsyu)** — daemon E2E for REQUEST_CHANGES back-edge + cap-hit (only APPROVE path is E2E-tested).
- **hk-kxygy** — unify the two DOT parsers (internal/workflow/dot vs internal/workflowvalidator disagree on review-loop.dot dialect); blocks hk-geype.
- **hk-yn29b** — EV-029 compat test is test-isolation-flaky: `go test ./internal/core/` is RED as a full package (~108 sub-failures), passes with `-run EV029`. Global event-registry pollution. Pre-existing.
- **hk-o4vjp** — 3 daemon tests in `run_w3cp1_boiwe_hiqrl_test.go` RED (exit-0 handlers + no-commit guard). Pre-existing.
- **hk-vhped** (P3) — pane-liveness: derive agent names from HandlerBinary vs hardcoded claude/node.
- **hk-uidls** (P3) — CP-056 loader returns ErrWorkflowLoad, spec says ErrDeterministic.
- Sub-workflow node cascade dispatch — still out of scope (no dedicated bead yet; file one when picking it up).

## Files to open first

1. `internal/daemon/dot_cascade.go` — the cascade driver (walk + back-edge + cap; gate/sub-workflow deferral comments).
2. `internal/daemon/workloop_dot_mode_e2e_hklphyf_test.go` — how to drive DOT mode through the daemon in a test.
3. `internal/handler/gate_dispatch.go` — gate semantics (CP-058 model).
4. `specs/examples/review-loop.dot` — the canonical fixture the live test uses.

## Caveats

- `go test ./internal/core/` and full `./internal/daemon/` are RED on main from PRE-EXISTING bugs (hk-yn29b, hk-o4vjp, a StaleWatcher hang). Use `-run` filters; don't be alarmed by the full-package red.
- Two DOT parsers exist and disagree (hk-kxygy) — the daemon path uses `internal/workflow/dot` via `workflow.LoadDotWorkflow`; the CLI `graph validate` uses `internal/workflowvalidator`.

## Translations glossary

- **DOT mode** — workflows defined as Graphviz `.dot` graphs; daemon walks node→edge→node via the cascade engine.
- **cascade driver** — `driveDotWorkflow`; the daemon loop that walks the graph (this session's keystone).
- **gate node** — a policy/decision node returning allow/deny/escalate; deny=SUCCESS per CP-058.
- **hk-tgqy5** — daemon false `no_commit` kill bug (FIXED this session); **hk-9dnak** — cascade driver wiring (DONE); **hk-karlz** — daemon gate-evaluator (NOT built); **hk-lt0w7** — gate deny OQ (RESOLVED).
- **EM-005b / HC-058 / CP-058** — execution-model / handler-contract / control-points spec requirements governing gate-decision outcome status.
- **no_commit** — daemon failure class: implementer exited without advancing HEAD (was firing falsely; now fixed).

## No hard blockers. Next action: rebuild, then live DOT run on a throwaway bead.
