---
schema_version: 1
crew_name: gurney
queue: gurney-cmd
epic_id: hk-b89kk
captain_name: captain
model: sonnet
---

# Crew mission — gurney (cmd/harmonik tools lane)

> NOTE: this mission file was REPURPOSED 2026-06-24 from the old rc-prefix tail
> lane to the cmd-tools lane (captain directive: fill the 4-instance capacity with
> file-disjoint work). The old rc-prefix scope is recoverable from `codename:rc-prefix`.

## goal

cmd/harmonik tools lane: ship the `harmonik usage` verb + fix the
`Manifest.Digest` map-order flaky test.

## On boot
1. `harmonik comms join` + confirm identity = gurney.
2. `br update hk-b89kk --assignee gurney` (mirror for attribution — load-bearing).
3. Post a boot status to captain (`--topic status`) + a journal comment.
4. Arm `harmonik comms recv --agent gurney --follow --json`.

## Dispatch scope = exactly TWO standalone beads (no epic tree)

Submit these to `gurney-cmd`. They touch DIFFERENT files under `cmd/harmonik/` so
they may run concurrently (the daemon merges each off origin/main):

1. **hk-b89kk** (P1, feature) — promote the `usage_join.py` spike to a real
   `harmonik usage` verb: join transcripts × events on `run/<run_id>`, roll up by
   session/bead/model. New file `cmd/harmonik/usage_cmd.go` (+ wiring in main).
2. **hk-z8fp** (P2, bug) — `Manifest.Digest()` iterates `m.Files` UNSORTED while
   `Lock.Digest()` sorts paths → flaky `TestLockDigestMatchesManifestDigest`
   (`cmd/harmonik/asset_skew_test.go`, fails ~15-20%). Fix: sort Files in
   `Manifest.Digest()` to match `Lock.Digest()`; correct the wrong
   "order-independent" comment.

## queue
Use your OWN named queue `gurney-cmd` for every `harmonik queue submit`. NEVER the
main queue, NEVER another crew's queue. Daemon global cap is max-concurrent 4 with
several lanes active — your beads may queue behind running work; that is fine.

## review + test discipline
Every bead dispatches through the daemon's DOT (sonnet triple-review) graph — do
NOT override to single/no-review mode. Reviewed AND tested before it counts landed.

## boundaries (file-disjoint lane)
- This lane is file-disjoint from **paul** (`internal/daemon/*`) and **jamis**
  (`specs/` scenario). You touch ONLY `cmd/harmonik/*`. If a fix would pull you
  into `internal/daemon/*`, STOP and post `--topic status` to captain — do not
  edit paul's files.
- Embedded-asset gotcha: if you ever edit `.claude/skills/*` or
  `scripts/captain-tools/*`, the embedded copy under `cmd/harmonik/assets/...`
  must be re-synced or `TestSkillAssetsEmbedInSync` goes RED — neither of your two
  beads should touch those; flag captain if one seems to.

## failure handling (Sonnet lane — escalate, do not self-classify)
On ANY `run_failed` / `run_stale` past launch+30min / unexpected wedge: post to
the captain over comms (`--topic status` or `--topic error`) and HOLD — do NOT
self-classify or re-dispatch beyond the standard once-rule. The captain owns
failure triage for this lane.

## progress feed (mandatory)
Post `--topic status` to captain AND a `br comment` on each bead close, plus a
timer tick (≤10 min while dispatching, ≤15 min idle/draining) and boot/drain
bookends.

## What you MUST NOT do
- Do NOT `br close` any bead — the daemon closes beads when their work merges.
- Do NOT submit to `main`. Use `--queue gurney-cmd`.
- Do NOT spawn Agent-tool sub-agents for implementation — the daemon queue is the
  dispatch mechanism.

## When the lane drains
Both beads landed → post a completion status and idle on your comms inbox for the
next assignment. Do NOT self-terminate.
