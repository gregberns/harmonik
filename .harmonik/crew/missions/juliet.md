---
schema_version: 1
crew_name: juliet
queue: juliet-q
epic_id: hk-04q2j
goal: "Remove the daemon's boot-time auto-drain of the bead backlog (delete the code path, not just default it off)"
captain_name: captain
---

# Mission: Remove daemon boot-time auto-drain (delete the code)

You are crew member **juliet**, owning epic **hk-04q2j** on queue **juliet-q**. Report status to **captain**.

## Why
Operator (`plans/2026-07-21-platform-architecture/DECISIONS.md` §Clarifications): the daemon must NOT self-start work on boot; only AGENTS (crews submitting to their own named queues) decide what runs. An out-of-date backlog getting arbitrarily pulled in on boot is a coupling being eliminated. The mechanism is already default-OFF (`noAutoPull`), but the operator wants the **code path DELETED**, not merely defaulted off.

## What to delete (the auto-drain = the `br ready` fallback dispatch path)
- **`internal/daemon/workloop.go`** — the body of `if queueItemIndex < 0 { … }` (~lines 2725–2833): the `br ready` poll (`deps.brAdapter.Ready(ctx)`), pick-first-ready, dispatch, the `noAutoPull` gate, the br-ready copies of the operator/handler-pause gates, and the `readyPathAttempts` map. Replace `queueItemIndex < 0` with idle-and-continue (sleep on `deps.submitWakeC`, exactly as the `noAutoPull` branch does today). Delete the `noAutoPull` field (~712–717) + assignment (~1183). Fix the godoc (~79–84).
- Config/flag/status plumbing (Step 3 bead): `cmd/harmonik/main.go` `autoPullFlag`, `usage.go`, `supervise/shim.go` (drop `--no-auto-pull`), `daemon.go Config.NoAutoPull`, `bootstate.go`, `core/daemonevents_hqwn59.go` status payload, scenario setters.

## What to PRESERVE (do NOT touch)
- The **queue-pull dispatch path** (`workloop.go` ~2119–2723) — how crews' actively-submitted named-queue work runs.
- Boot-time restore of **agent-submitted** queues (`internal/lifecycle/startup_pl005_qm002.go` `LoadQueueAtStartup`) — a crew's own submitted work surviving a restart is intended.
- The **sentinel-governor** `Ready()` calls (`workloop.go:1919,1970`) — observe-only, they emit `governor_signal`, they do NOT dispatch.
- The hk-kac8g handler-pause gate's **queue-path** copy (~2320–2331) and `workloop_handlerpause_kac8g.go` helpers.

## The big work: test migration (Step 1 first)
Many `internal/daemon` tests use the br-ready path as a convenience dispatch driver (stub ledger `Ready()` returns a bead, no `QueueStore`). Once the path is gone they dispatch nothing. **Land the export-helper first** (hk-04q2j.1): make the test export helper synthesize a single-item queue from `BrAdapter` when no `QueueStore` is provided, so legacy tests route through the queue path. Then delete (Step 2), then remove plumbing (Step 3), then delete/rewrite feature-specific tests (Step 4).

## Beads (under hk-04q2j)
hk-04q2j.1 export-helper → hk-04q2j.2 delete br-ready block → hk-04q2j.3 remove config plumbing → hk-04q2j.4 test migration → hk-04q2j.5 optional restartbackoff/flag cleanup.

## Shared-file watchpoint
Sibling crew **india** (epic hk-tckw3, codex-first) edits **different regions** of `internal/daemon/workloop.go` (the codex isolation guard ~3626, not the br-ready block) and other daemon files. Rebase onto the target branch before finalizing any daemon-file bead so the daemon merge stays clean; if a real conflict hits workloop.go or a shared test helper, post `--topic status` to captain to serialize.

## Model
Opus (daemon-code deletion + large test-migration blast radius = failure triage). Verify with `go build ./... && go vet ./...` green and `ubs` clean on changed files, plus a live check that a booted daemon dispatches NOTHING until a queue is submitted.
