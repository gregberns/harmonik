# Captain boot-process audit — 2026-06-16

Operator asked: the captain boot takes several minutes; make it effective + fast. Three parallel read-only auditors (latency / coverage-gap / drift). Findings synthesized below as **actionable STARTUP.md / SKILL.md edits** (propose-then-review — NOT yet applied). Anchors are `.claude/skills/captain/STARTUP.md` unless noted.

## A. LATENCY — the boot is round-trip-bound, not CPU-bound

1. **Batch Step 2 (a–f) into ONE shell call.** All six ground-truth probes are independent read-only — collapse the six fenced blocks into a single `bash` with labeled sections. **Biggest win (~5 round-trips).**
2. **Drop `subscribe --heartbeat 1s | head -1` (Step 2e)** — it waits for a daemon tick / may hang on buffering. `queue status --json` active-group already tells you if a bead is dispatching.
3. **One-shot the crew pane captures** (Step 3/5d): `for w in $(tmux list-windows -a -F '#{window_name}' | grep hk-crew-); do echo "== $w =="; tmux capture-pane -p -t "$w" | tail -25; done` — one call, not five. And only capture-pane the crews that FAIL the `comm -23` presence filter.
4. **Defer the dead-session reap** to AFTER fleet-verify (one batched `kill-session` loop) — not interleaved with ground-truth.
5. **Drop inline `sleep`-gated re-checks** — re-checks belong on the `/loop 12m` tick; the only legit bounded-wait is Step 5d crew-online (poll `comms who`, not fixed sleep).
6. **Overlap Step 4 planning reads with the Step 5 crew-online wait** (`kerf next`/`br ready`/status-doc are independent of waiting for `comms join`).
- Net: items 1+3 collapse ~10 serial tool calls into 2 batched shells — the bulk of the "several minutes." Coverage unchanged.

## B. COVERAGE GAPS — false-green risks (each confirmed live this session)

1. **Paused/failed-queue sweep is MISSING.** Boot never inspects per-queue status → a `paused-by-failure` queue is invisible. ADD to Step 2: `harmonik queue list --json | jq -r '.queues[]|select(.status|test("paused|complete-with-failures"))|"\(.name)\t\(.status)"'`. **Live now: main + remote-substrate + ~14 crew queues are paused-by-failure.** HEALTHY criterion #6 ("daemon up = queue status ≠ 17") is a FALSE-GREEN — `queue status` returns 0 on a paused main queue. Up ≠ dispatching.
2. **Dispatch quality-bar / workflow sanity is MISSING** — nothing verifies a crew ran the INTENDED workflow. `run_started`/`reviewer_launched` carry `workflow_mode` (`dot` vs `single`). ADD (post-spawn + 12m tick): check keeper beads emit `dot` + a non-empty `reviewer_verdict`. **This is exactly the gap that let the workflow_ref Opus-bar bypass land undetected (hk-u6zp).**
3. **Leaked dead-session sweep is MISSING** (the 42 `*-flywheel` leak). ADD: list `*-flywheel` older than the daemon's `ps -o lstart`; reap WINDOW/PID only; NEVER kill `*-default`.
4. **Presence-stale vs dead is conflated** (Step 3). `comms who` ages at ~120s; this session's 5 live crews would've been wrongly stopped. The `comm -23` one-liner is a CANDIDATE filter ONLY — a record absent from `comms who` is DEAD only if capture-pane shows no activity AND `comms log --from <crew> --since 5m` is empty. State this; never let the one-liner drive `crew stop`.

## C. DRIFT / REDUNDANCY / LENGTH

1. **FUNCTIONAL BUG — STARTUP.md:319** `comms recv --follow --from captain` → `--from` is a SENDER filter, so the captain watches only messages IT sent (empty inbox). FIX: `comms recv --follow --agent captain`. (SKILL.md:499 already correct.) Also STARTUP:37-38 "pass `--from captain` on every comms op" is false for recv/who/log.
2. **Stale keeper band** — STARTUP:262/265 + SKILL:221 say `--warn-pct 25 --act-pct 30`, but `~/.claude/captain-tools/captain-launch.sh` defaults are **30/35**. Single-source: cite captain-launch.sh, don't hardcode.
3. **keeper-doctor misparse** — `harmonik keeper doctor --agent X` treats `--agent` as the agent NAME; positional `keeper doctor X` works. (STARTUP/SKILL doctor refs are actually SAFE — all positional — but the captain hit the broken form live; fix bead = hk-psds.)
4. **STARTUP.md is ~446 lines (~3× a "run every boot" checklist).** Recommend: make STARTUP the PURE ordered checklist (Steps 0–6, ~150 lines, command + one-line why); move Anti-patterns A–G, the HEALTHY-FLEET definition, and the full On-WARN/restart-now block (≈duplicated in SKILL §10) OUT to SKILL.md / an appendix, leaving one-line pointers. ~35 lines of On-WARN + the boot sequence are stated TWICE (STARTUP ↔ SKILL) and will drift.
5. **`comms send --wake` exists** (verified) — replaces the hand-rolled `tmux send-keys` idle-crew nudge at STARTUP:350.
6. `crew list --json` / `comms who --json` are NDJSON not arrays — flag the jq gotcha near those lines (`br --json` IS an array, so `br ready | jq '.[]'` is fine).

## Recommended next step
A focused editor applies B1+B2 (paused-queue + workflow-mode checks — highest safety value), C1 (the recv `--from`→`--agent` functional bug), and C2 (band 25/30→30/35) to STARTUP.md + SKILL.md, then a reviewer pass. The length/consolidation refactor (C4) is a larger, separate edit. Full agent transcripts were ephemeral (/tmp tasks a24a6135 / ad423dbc / add203b2).
