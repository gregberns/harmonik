<!-- PP-TRIAL:v2 2026-05-30 main — CONTROL-POINTS + daemon-hardening thread. NOTE: the main HANDOFF.md is a DIFFERENT concurrent thread (flywheel, awaiting user path-2 decision) — do NOT clobber it. This file = the thread that ran 10 harmonik waves, hardened the daemon (6 bugs fixed), and is mid-build on the control-points subsystem (hk-a8bg.*). -->

ROLE: orchestrator. Delegate. Keep main thread minimal. Route work through `harmonik run` per skill `harmonik-dispatch`.

# Where we are (2026-05-30) — control-points thread

Main clean, `0/0` origin. This thread's work is all merged+pushed; nothing unsaved. **Work is CLEAN but PARTIALLY BLOCKED** by the iter-2 bug below.

The earlier session ran 10 `harmonik run --wave` batches: it (a) hardened the daemon — found & fixed 6 concurrency/UX bugs (`hk-lckbv`, `hk-cw4sx`, `hk-i6hhn`, `hk-w92me`, `hk-dut6b`, `hk-7x7ea`/`hk-poy7k`), and (b) began building the **control-points subsystem** (`hk-a8bg.*`), landing ~18 child beads (gate/hook/guard/budget/policy/freedom-profile/cognition/registry behaviors).

## The one real blocker — iter-2 (shared with flywheel)
`harmonik` review-loop works for beads that pass review on iteration 1 (most), but **any bead drawing REQUEST_CHANGES fails** `no-progress detected at iteration 2`. ROOT CAUSE now FOUND (memory `project-harmonik-reviewloop-iter2-broken`): under tmux the stdout `SessionIDInterceptor` never fires (`handler.go:303` early-return), so iter-1 uses a SYNTHETIC session id and iter-2 does `claude --resume <synthetic>` against a session that never existed. Fix = capture real `session_id` from the hook-relay payload. **Bead `hk-za5mz` (P1) — repair already dispatched 2026-05-30.** Until it lands, expect ~1–2 iter-2 casualties per wave.

## Next step (control-points thread)
Keep dispatching `hk-a8bg.*` child beads via `harmonik run --wave --max-concurrent 4-5` (one bead per subsystem-area to avoid merge collisions; `br ready | grep hk-a8bg`). After `hk-za5mz` lands + rebuild, re-dispatch the two casualties: **`hk-a8bg.26`** (failed reviewer BLOCK — needs rework) and **`hk-a8bg.35`** (iter-2 casualty). `hk-a8bg.5` is refs=5 — verify/close as subsumed.

## CRITICAL coordination (concurrent agent + flywheel)
harmonik allows only ONE daemon + ONE active queue per project. The flywheel thread and another agent share this repo/daemon. **Before dispatching: `pgrep -f "harmonik run"` (must be empty) and check no `.harmonik/queue.json` is active.** Don't `git reset --hard` while any daemon runs. Don't touch the flywheel thread's untracked `internal/supervise/`.

## Files to open first
1. `~/.claude/projects/-Users-gb-github-harmonik/memory/reference_harmonik_wide_waves_disk.md` — operational rules (CPU knee=4-5 wide on 10 cores; `reset --hard origin/main` before every `go install`; `run_stale` triage; disk).
2. `~/.claude/projects/-Users-gb-github-harmonik/memory/project_harmonik_reviewloop_iter2_broken.md` — the iter-2 root cause + fix path.
3. `HANDOFF.md` — the concurrent flywheel thread (read so you don't collide).

## Translations glossary
- **control-points / `hk-a8bg.*`** = the Control-Points spec subsystem (gates/hooks/guards/budgets/policy/registry); epic `hk-a8bg`, many child behaviors.
- **iter-2 / hk-za5mz class** = `claude --resume` resumes a synthetic (never-real) session id → no new work → no-progress fail. Root-caused; fix dispatched.
- **iter-1 APPROVE** = bead passes review first try (works fine today).
- **wave** = one `harmonik run --beads ... --wave` batch.

## Notes
- Pre-screen every bead (`git log --grep "Refs: <id>"` AND check the actual artifact) — many impls land without `Refs:` trailers.
- Daemon must run inside tmux: `tmux new-session -d -s hkwaveN -c <repo> "harmonik run ... --notify-stream 2>&1 | tee /tmp/harmonik-waveN.log; echo HARMONIK_RUN_EXITED_${PIPESTATUS[0]} >> ..."`, then Monitor the tee'd log + `.harmonik/events/events.jsonl`.
