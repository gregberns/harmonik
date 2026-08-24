---
id: handlerpause-extract
title: Extract handler pause — six files, and one real narrowing that has to happen first
type: task
priority: 1
labels: [daemon, extraction, clear-the-ground]
depends_on: [runregistry-extract, lint-rekey-exclusion-list]
blocks: []
workstream: W2
batch: 3
---

## Problem

Handler pause — pausing a handler or an account on rate limits and budget exhaustion, persisting that
state, and auto-resuming — is six files in `internal/daemon` totalling **1,582 production lines**.
(The review says 2,056; that predates the comment cut.)

It is the largest of the four extractions and the only one with a dependency that does **not**
disappear when the run registry moves.

## Scope

Move to a new package `internal/pause`:

| File | Lines |
|---|---|
| `internal/daemon/handlerpause_9hwbw.go` | 777 |
| `internal/daemon/handlerpause_persist_m0k0a.go` | 319 |
| `internal/daemon/handlerpause_policy_37zy8.go` | 219 |
| `internal/daemon/handlerpause_autoresume_0otqs.go` | 138 |
| `internal/daemon/handlerpause_sigusr1_bdvae.go` | 74 |
| `internal/daemon/workloop_handlerpause_kac8g.go` | 55 |

Ten of the eleven cluster test files are already `package daemon_test` and convert cleanly.

### The one real narrowing

**`dispatchGatesPort` is a daemon-private struct and does not go away after the run registry moves.**
`workloop_handlerpause_kac8g.go` reads two of its fields: `gates.bus` (lines 17, 42) and
`gates.heldEventDedup` (lines 21, 47, 52).

Narrow it before moving the file. Either pass `(bus handlercontract.EventEmitter, dedup map[string]struct{})`,
or define a consumer-owned two-method interface in the new package. **This narrowing is the task's
real work**; the other five files are a straightforward move.

`dispatchports_test.go` is `package daemon`, calls `emitHeldEvent(...)` and mutates
`gates.heldEventDedup` directly (lines 12, 13, 19, 22). It cannot travel — `dispatchGatesPort` belongs
to the daemon. Rewrite it against the narrowed inputs.

The two other outbound dependencies — `RunHandle` (`handlerpause_9hwbw.go:771`,
`handlerpause_policy_37zy8.go:209`) and `RunRegistry` (`handlerpause_policy_37zy8.go:60,212`) — are
gone once `runregistry-extract` lands. Note that `:212` calls `snapshotWithKeys()`, one of the two
symbols that extraction has to export.

### Not in scope

`operatorpause.go` (259 lines) belongs to the later control-plane cut. Do not merge operator pause
into this. Its shim `ExportedNewOperatorPauseController` nevertheless sits in the shared
`export_meters_pause_test.go`, which has to be split three ways.

## Done when

1. `internal/pause` exists with a `depguard` rule denying `internal/daemon`.
2. `dispatchGatesPort` is no longer reached from the moved code.
3. Six production consumer files compile: `bootstate.go`, `daemon.go`, `dispatchports.go`,
   `bootsocket.go`, `quiesce.go`, `scheduler.go`. Note `scheduler.go:539,541,877,879` calls two
   *unexported* moved symbols (`emitHeldEvent`, `pruneHeldDedupOnEpochChange`) — they need exporting
   or the call sites need rethinking.
4. `scripts/runloop-emitter-gate.sh:178` — the hard-coded budget row
   `"internal/daemon/workloop_handlerpause_kac8g.go 3"` is removed **in the same commit as the move**.
   Lines 228–236 are an explicit missing-file guard: leaving the row makes the gate fail.
5. `tools/lintreport/allow.txt` gains nothing. 11 existing entries travel.

## Limits

- **Do not move `workloop_handlerpause_kac8g.go` before the narrowing.** It is the file that makes
  this extraction non-trivial and moving it first drags a daemon-private type out of the daemon.
- Serialized against `spendmeter-extract` only by file conflict — both edit `bootstate.go`,
  `daemon.go` and `export_meters_pause_test.go`. There is no design edge between them.
- Four files the review lists as consumers have **no live references**: `hookrelay_chb025.go`,
  `socket.go`, `socketdispatch.go`, `wiringlog.go`. Only `wiringlog.go:14` mentions the field, in a
  comment. Do not go looking for work there.
