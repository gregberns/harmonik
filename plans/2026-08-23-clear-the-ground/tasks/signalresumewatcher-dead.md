---
id: signalresumewatcher-dead
title: The SIGUSR1 resume watcher has no production caller — decide whether it moves or goes
type: task
priority: 2
labels: [daemon, dead-code, needs-operator, clear-the-ground]
depends_on: []
blocks: []
workstream: W2
batch: 3
status: NOT READY — needs a ruling on whether the feature is wanted
---

## Problem

`SignalResumeWatcher` and `NewSignalResumeWatcher` in
`internal/daemon/handlerpause_sigusr1_bdvae.go` (74 lines) have **zero production consumers**.
Nothing wires the SIGUSR1 resume watcher. Its only reachers are its own two test files:
`handlerpause_sigusr1_bdvae_test.go` and `handlerpause_sigusr1_bdvae_export_test.go`.

Found while mapping the handler-pause extraction boundary, 2026-08-23.

This is the exact shape the charter warns about: roughly a fifth to a quarter of the system is built
but not wired, and building a feature to a quarter finished and calling it done is a named recurring
failure.

## The decision

- **Wanted** → it moves with `handlerpause-extract` and gets wired, which is a separate piece of work
  with its own reason to exist.
- **Not wanted** → the file and both its tests are deleted, and the extraction gets 74 lines smaller.

The question is whether an operator ever wants to resume a paused handler by sending SIGUSR1 to the
daemon, given that `harmonik handler` already exists as a command surface for inspecting and resuming
paused handlers.

## Done when

The ruling is recorded here with a date, and `handlerpause-extract`'s file list is amended.

## Limits

- **Do not delete it on the strength of "no caller" alone.** Unwired is not the same as unwanted, and
  this repo has code that is deliberately preserved while inert — the crash-safe dispatch subsystem is
  the standing example.
- Do not wire it to justify keeping it. If it is wanted, wiring it is its own task with its own
  acceptance test.
