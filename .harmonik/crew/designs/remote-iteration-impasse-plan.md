# Remote-worker iteration impasse — plan + post-mortem (2026-06-25)

> Operator hit a wall: remote-worker validation felt "silly," iteration "way too slow," lots of
> go-cache noise, captain frozen, gurney blocked. Admiral fanned out 5 agents to break the impasse.
> This is the durable synthesis (the conversation itself is not persisted).

## The reframe
The remote bugs were REAL and distinct (not incompetence). The tax was debugging second-long plumbing
bugs through our SLOWEST loop (30–90 min full daemon review-cycle), with all-or-nothing remote routing
(can't land local while remote enabled → disable+restart churn) and restarts that kill in-flight local work.
Fix the loop, not just the bugs.

## The plan — do now / build next / durable / skip

**① DO NOW — zero new code, ~200–600× faster loop.** A deterministic full-pipeline mode ALREADY EXISTS:
the **twin scenario-harness**. `make build-twin-generic` (5s once) + a scenario test boots the REAL daemon
through route→launch→implement→gate→review→merge against a stubbed agent in **~6s** (measured;
`go test -tags=scenario -run TestScenario_HappyPath_N1 ./internal/daemon/`). No LLM, no API key.
Combine with a **scratch clone**: `git clone` to a different path → distinct project-hash → fully isolated
socket + tmux namespace; `harmonik --project <scratch>` runs STANDALONE (no supervisor/revive) so a crew can
`pkill` + rebuild it in seconds without touching the fleet daemon (bootstrap trap avoided structurally).
GAP TO FILL: add ONE twin-backed *remote* smoke scenario routing through the SSH runner to a localhost
"remote" worker, asserting `run_started.worker_name` — turns a 30-min agent_ready_timeout into ~5–90s.
**Why this matters:** ssh-localhost scenario tests give FALSE-GREEN today (box A and the "worker" share one
FS + tmux server), so all ~9 separate-machine gaps were invisible to fast tests — findable only by slow live
runs. A true fast separate-machine reproducer is the #1 missing capability.

**② BUILD NEXT — small, durable: per-queue local/remote routing.** Add `LocalOnly`/`WorkerTarget` to queue
config; gate the one `SelectWorker()` call (`workloop.go:2813-2814`) on it. The queue name + queueStore already
reach that call site, and `DefaultHarness` (`queue/types.go:332`, resolved in `harnessresolve.go`) is the exact
field-pattern to copy → SMALL effort, additive, backward-compatible. Permanently kills gurney's "can't land a
local fix while remote enabled." hk-xjbvi (live on/off toggle) is COMPLEMENTARY, not a substitute (still global).

**③ DURABLE ROOT FIX — bigger, sequence last: make bead-runs survive a daemon restart.** Crews already survive
restarts (independent tmux sessions, `crewstart.go:274-283`); bead-runs don't (windows inside the daemon, killed
on pkill — `workloop.go:1362-1392`). Extend the crew pattern to runs → restart-disruption pain gone at the root.

**④ SKIP — two daemons on the SAME repo.** Blocked (pidfile flock singleton, exit 5) + unsafe (mutual tmux/worktree
destruction, beads WAL contention). The two-daemon *goal* is met by the separate-clone in ① (fleet-portability,
landed 2026-06-13, explicitly sanctions one-daemon-per-project).

## Post-mortem corrections (IMPORTANT — two beads need rework)
- **hk-t1t00 premise is WRONG in mechanism.** Source: the scenario gate computes its affected-set from
  `headSHA..HEAD` (the run's own parent), NOT `merge-base(HEAD, origin/main)` (`scenariogate.go:325-333`). And
  **`HK_GATE_BASE_SHA` does NOT exist in the Go source** — it's only an operator memory note. The stale-origin/main
  pain is real but not via the stated merge-base story → rewrite the bead before building.
- **hk-f3u6o is CONFIRMED in source.** `ReadReviewVerdict(workspacePath)` (`workspace/reviewverdict.go:93`) is a bare
  local `os.ReadFile`, no `…Via(runner)` variant; the detection path next to it IS runner-aware → genuine asymmetry
  defect. Fix = runner-routed read variant. LANDING LANDMINE: this fix's own remote review fails verdict-absent, so
  it must land with the worker DISABLED (run+review locally) — exactly what ② or the ① scratch-loop enables.
- Root class of all ~14-16 remote bugs: "no single seam guaranteed all I/O for a remote run goes to the worker."
  One bug wearing many hats, not 16 unrelated. Phase-1 e2e WAS proven green on the real box (hk-620j).
- "Is it us?" → a hard distributed-systems problem done reasonably well. The genuine do-better items are 3:
  (1) build a fast separate-machine reproducer (= ①), (2) read events.jsonl typed events NOT daemon stderr,
  (3) always re-validate the whole fix stack on current main before declaring "still broken" (stale-base trap).

## Recommended first moves (for operator / captain)
1. **Revive captain + watch** (frozen ~2.5h from the wake-economy watch-stall). gurney's 2 fixes are bankable.
2. Stand up a **remote-hardening crew on a scratch clone** with authority to build/restart ONLY the scratch daemon;
   first task = add the remote twin smoke scenario + land hk-f3u6o (+ rewrite hk-t1t00) LOCALLY.
3. Queue the **per-queue-routing field** (②) as the durable unblock.

## Side-findings worth a bead each
- Smoke tier is currently RED on `TestScenario_ConcurrentMultiQueue_N2_HappyPath` (beads not closing within 2s —
  real concurrent-dispatch flake, unrelated to remote).
- Pipeline-stage timeouts: commit_gate 900s is a DOT-node attribute (`standard-bead.dot` / `workflow.dot`), only
  `--agent-ready-timeout` is flag-tunable; the rest are hardcoded `var`s (test-overridable). No config.yaml stage-timeout keys.
- Repo-root `workflow.dot` is a 3-reviewer triple-review graph; a minimal one-reviewer loop needs `--workflow-ref <single-reviewer.dot>`.
