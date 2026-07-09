---
schema_version: 1
crew_name: leto
queue: leto-q
epic_id: hk-ag97p
captain_name: captain
model: opus
---

# Crew mission — leto — Pi/ornith + sandbox CLOSE-OUT chain (P0, captain-owned)

> RE-TASKED 2026-07-03 (~17:20Z) from pi-sandbox BUILD (landed: 3/7, acceptance hk-i0377
> committed c09c4c03) to the **CLOSE-OUT LANE**. The operator pushed close-out ownership
> onto the captain ("push it all onto the captain"); leto is the executing crew. Deliverable
> = a clean e2e SANDBOXED ornith run. Model = Opus (diagnostic gate work).

## On boot / re-task
1. `harmonik comms join` + confirm identity = leto.
2. `br update hk-1hgjr --assignee leto` (mirror for attribution; re-mirror on each step's bead).
3. Post a boot/re-task status to captain (`--topic status`).
4. Arm `harmonik comms recv --agent leto --follow --json`.

## HARD rule — fix OUT-OF-DAEMON, not through the pipeline
The keystone bug is IN the review pipeline. Do NOT route these fixes through the daemon's
DOT review — it would fail its own review on the very bug it repairs (the self-fix
bootstrap trap). For each code fix: isolated worktree off origin/main → build + test
(`go build ./...` + `go test` the touched packages, `-race` where relevant) → INDEPENDENT
review (≥2 diverse review sub-agents, isolation:worktree so they never mutate main) →
fast-forward land on main → push. Report the SHA to captain; the captain closes the bead
(NEVER `br close` yourself — daemon/captain owns terminal transitions).

Work the chain IN ORDER. Report to captain (topic status) after EACH step.

## Step 1 (KEYSTONE) — hk-1hgjr: reviewer local review_correctness ErrMalformed
Local twin of the ALREADY-FIXED remote bug hk-qts7r (commit 9860e8a2 — gate the kill on a
valid-complete verdict; mirror that pattern). Symptom: `review verdict ErrMalformed`
(schema_version missing / reviewer produced no verdict) + agent_ready_stall, failing runs
POST-commit. This is what cascade-failed the eval queue (watch escalation 17:13Z). Reproduce
first, then fix, then prove a local review run reaches a valid verdict. UNBLOCKS the e2e.

## Step 2 — hk-r4p0l: srt sandbox is a no-op for pi
Run agent-type tags `claude-code`, so `sandbox.harnesses:[pi]` never matches → no srt-wrap.
Make the sandbox ACTUALLY engage for pi runs and TEST it (a pi run is demonstrably
srt-wrapped). Sandbox pkg is yours — file-disjoint from gurney.

## Step 3 — land the api code-fix
In-flight api fix on branch `worktree-agent-a6e56ba9b90b2c320` (Pi vs ornith needs
`api:"openai-completions"`, NOT `"openai"` — wrong value = 4.5s exit0 no-commit fast-fail;
related: hk-j6wm7 artifact-retention, hk-u69my intermittent fast-fail). Locate the branch,
verify, build+test, review, ff-land. If the branch is gone, re-derive from the bead(s) and land.

## Step 4 — clean e2e SANDBOXED ornith run
With 1-3 landed: run ONE bead end-to-end Pi → ornith (dgx.local:8551/v1, model `ornith`,
api:openai-completions, dummy key) INSIDE the srt sandbox. Prove: sandbox engaged, commit
landed, review reached a valid verdict, no ErrMalformed. Report run_id + evidence. This is
the close-out deliverable.

## Discipline
- Do NOT touch gurney's lane (eval-program WS1/WS4). File-disjoint.
- Progress feed: `--topic status` to captain on boot, each step done, any blocker, ≤15-min idle tick.

## translations
hk-1hgjr = "reviewer local ErrMalformed bug — the e2e keystone" · hk-r4p0l = "srt sandbox
is a no-op for pi runs" · srt = "@anthropic-ai/sandbox-runtime argv-wrapper sandbox" ·
ornith = "local DGX model at dgx.local:8551, OpenAI-compat, api:openai-completions".
