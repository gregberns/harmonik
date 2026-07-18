---
schema_version: 1
crew_name: alpha
queue: alpha-q
epic_id: hk-ky7ye
captain_name: captain
model: opus
goal: "Pre-baseline gate blockers, in order: hk-okzy1 (fixed — verify+harden+close) then hk-ky7ye (Tier0 supervisor auto-revive)."
---

# Mission: alpha — pre-baseline gate blockers

You are crew **alpha** (NATO naming; worker crews are Alpha, Bravo, Charlie, …). You own
queue **alpha-q** and report status to **captain**. Both beads below GATE the assessor
baseline pass — they are the top priority. Work them **in order**, one at a time.

## On boot
0. `harmonik agent brief` — pull current operating context.
1. `harmonik comms join --name alpha` + confirm identity = alpha.
2. Arm `harmonik comms recv --agent alpha --follow --json`.
3. `br update hk-ky7ye --assignee alpha` (re-affirm the mirror on adopt — load-bearing for attribution).
4. Post a boot status to captain (`--topic status`) + a journal comment on the active bead.

## Queue (work in this exact order)

### 1. hk-okzy1 — P1 hook_fired-not-emitted — ALREADY FIXED by captain; your job is VERIFY + HARDEN + CLOSE
**Do NOT re-investigate or re-implement.** The captain already root-caused and landed the fix.
- **Root cause (settled):** the `7bee7b89` attribution was DISPROVED (reverting all three of its
  source files still fails). Git-bisect pinned the real culprit to **`18a2d221`** ("H6/H7/H8/H13
  daemon-concurrency, wave2b"). Its H7 fix set a permanent `drainSealed` flag on `eventbus`
  `Drain()`; once sealed, `addGlobalDrainer()` returned false and **re-entrant** emit-delivery
  goroutines ran UNTRACKED. `hook_fired`/`hook_failed` are emitted re-entrantly from inside the
  `agent_started` handler during drain (part of the cascade `Drain` must flush), so `Drain`
  returned before delivering them → wildcard observers deterministically missed them. The emit
  itself always succeeded (`Valid()==true`, nil error) — it was a drain-cascade bug, not an emit bug.
- **Fix (landed):** commit **`86ff7565`** — `internal/eventbus/busimpl.go` replaces the global
  `sync.WaitGroup` + seal with an `inflight` counter + `sync.Cond` under `drainMu`; `Drain` loops
  `for inflight>0 { cond.Wait() }` so mid-drain cascade emits are tracked and flushed, while H7's
  Add-concurrent-with-Wait crash is structurally impossible. Independently reviewed = APPROVE (7/7
  properties), `CP016/017/042` RED→GREEN, eventbus `-race ×20` green.
- **Your tasks:** (a) re-verify green: `go test ./internal/hooksystem/ -run 'CP016|CP017|CP042' -count=1`
  and `go test ./internal/eventbus/... -race -count=1`. (b) **HARDEN:** add the recommended
  dedicated eventbus-level unit test asserting "a re-entrant Emit issued from within a handler
  during Drain is waited on and delivered to observers" (the reviewer flagged this as the missing
  own-tier regression test). Implement → independent review (spawn a reviewer; captain gates) →
  commit (explicit paths only). (c) Close hk-okzy1 once the hardening test lands green.
- **KNOWN follow-up (file a P2 bead, do NOT fix here):** the per-run `DrainRun`/`addRunDrainer`/
  `runSealed` path shares the analogous orphan-on-seal bug for re-entrant `EmitWithRunID` during
  `DrainRun`. Out of scope for hk-okzy1; file it so it's tracked.

### 2. hk-ky7ye — Tier0 supervisor-watchdog auto-revive (restart --watch-restart) — REAL WORK, start cold
- **Symptom:** a standalone daemon's in-daemon supervisor-watchdog finds no supervisor, runs
  `harmonik supervise restart --watch-restart --project <scratch>` 3× (30s pidfile-wait each),
  each fails to write `cognition/supervisor.pid`, hits `max_revives=3`, logs `ERROR "revival cap
  reached — giving up"`, and the watchdog thread exits → daemon survives but is permanently
  unsupervised. This is the failure that took down the live fleet daemon (pid 22202) this session.
- **Load-bearing contrast:** `harmonik supervise start` SUCCEEDS in ~2s (pidfile written, status
  running). So `supervise` works; only the watchdog's `restart --watch-restart` revive path is broken.
  Chase the delta between the working `start` path and the broken `restart --watch-restart` path
  (pidfile handshake / working-dir / project-flag / wait-loop). Look under `cmd/harmonik` supervise
  subcommands + the in-daemon supervisor-watchdog that invokes it.
- Trigger scope: daemon launched WITHOUT a pre-existing supervisor (standalone). Reproduce, root-cause,
  fix → independent review (captain gates) → commit (explicit paths). This is gnarly process-lifecycle
  work: Opus default; reach for Fable only if it turns into hard concurrency/parity.

## Operating loop
Follow the crew-launch skill (`.claude/skills/crew-launch/SKILL.md`): drain **alpha-q** (never main),
one bead at a time, in the order above. Design → implement → **independent review** (spawn a reviewer;
the captain gates the merge) → commit (explicit-paths only; NEVER `git add -A`/`.`, bare `git commit`,
`git reset`, or `commit --amend` — the shared-index race). Progress feed per contract: `comms --topic
status` AND `br` comments — on bead-close + ≤10min while dispatching / ≤15min idle-drain, + boot/drain
bookends. Escalate genuine blockers to captain; do NOT reverse a locked decision or run a destructive op.

## Keeper restart
Re-read this file, re-join comms as `alpha`, re-arm the recv monitor, re-affirm `--assignee` on the
active bead. Committed work is not lost; resume at the next unfinished task above.
