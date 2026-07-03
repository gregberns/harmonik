# Decompose-pass review — codex-harness

Reviewer: independent sub-agent (round 1). **Verdict: REVISE → resolved in-place (no DAG change).**

## Form checks — all PASS
Goal→component coverage (G1–G6 all mapped), traceability, concrete/testable requirements, clean
boundaries, 6 components (in range), valid DAG, interfaces identified (one partial — fixed below).

## Findings + resolutions

### [BLOCKING] B1 — spawn-per-turn breaks the shared heartbeat/budget contract
`agent_heartbeat` is **not** git-derived and **not** harness-agnostic: it is emitted by a timer loop
**inside the claude handler** (`RunHeartbeatLoop`, CHB-019, `claudehandler_chb006_024.go:588-617`,
every 300s "while Claude alive"). The shared watchdogs `launchHeartbeatTimeout` (180s) /
`heartbeatStalenessThreshold` (8m) consume `agent_ready`/`agent_heartbeat` and will KILL a run that
emits no heartbeat. A `codex exec` running 12 min with zero heartbeats would be staleness-killed
mid-run. My earlier "watchdog timers are shared/harness-agnostic" claim (02-analysis, seam-design
Part E) was **wrong about the emitter**.
**Resolution:** added **R2.7** (codex adapter emits its own heartbeat mapped from codex `item.*`/
`turn.*` JSONL progress, OR declares no-liveness-polling so the stale-kill path is bypassed) and
corrected `seam-design/findings.md`.

### [BLOCKING] B2 — completion-signal source must be a first-class harness property
The interface assumed a live-session model (`Teardown`, `DetectReady(events)`). codex is
exit-on-completion. Without modeling this, codex specifics leak into shared `sess.Wait` consumers.
**Resolution:** added `Completion() CompletionMode {EventStreamThenQuit | ProcessExit}` to **R1.1**;
the shared workloop branches on it deterministically instead of the codex adapter faking heartbeats
to satisfy claude-shaped timers.

### [MINOR] M1 — C3 billing assert has a timing gap
`codex login status` assert is correct, but running it **post-spawn** means a turn may have already
billed the API pool. **Resolution:** **R3.3** now asserts ChatGPT-plan **pre-flight (before the
first task turn)** at adapter init; the `#2000` auto-generated-org-key audit promoted to an explicit
**C6 pre-production checklist** item (strip + forced_login do NOT defend a subscription-login that
silently routes to an org key).

### [MINOR] M2 — name the C1 refactor boundary
`buildClaudeLaunchSpec` mixes argv/env/file-materialization/pre-exec. **Resolution:** **R1.3** now
explicitly requires the split land **shared scaffolding** (worktree-trust, agent-task write) OUTSIDE
the per-harness `LaunchSpec`, so codex doesn't re-implement or skip it.

### [MINOR] M3 — codex reviewer verdict path unverified
Whether codex reliably writes `.harmonik/review.json` on instruction is untested. **Resolution:**
tagged as a **C6 MUST-TEST** (R6.5), not an assumption in R5.3.

### [MINOR] M4 — commit-trailer enforcement underspecified
codex's commit is a non-deterministic model decision (`git add -A` footgun, #8548); "instruct +
verify" may loop-fail. **Resolution:** **R2.5** now requires a deterministic **commit-after-exit
fallback** (the research's preferred option) when codex edits but doesn't commit with the trailer.

## Outcome
All fixes land inside existing components; DAG unchanged. No unresolved findings. Advancing.
