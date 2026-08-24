---
id: runregistry-extract
title: Move the run registry out of the daemon — the keystone extraction
type: task
priority: 1
labels: [daemon, extraction, clear-the-ground]
depends_on: [lint-rekey-exclusion-list]
blocks: [spendmeter-extract, handlerpause-extract]
workstream: W2
batch: 3
---

## Problem

`internal/daemon/runregistry.go` (371 lines) holds the in-memory registry of live runs. It is the
keystone extraction: two of the other three daemon extractions depend on symbols it owns, and
`internal/runloop` already declares the consumer-side ports for it (`RunHandlePort`,
`RunRegistryPort` in `internal/runloop/ports.go`), so the boundary is already designed.

Confirmed against source 2026-08-23: the file is still `package daemon` and `internal/runregistry`
does not exist.

**This extraction has zero outbound dependencies.** The moved code references *nothing* that stays in
`internal/daemon` — verified by identifier intersection against every package-level daemon
declaration. The only daemon names in the file are in doc comments.

## Scope

Move to a new package `internal/runregistry`:

- `internal/daemon/runregistry.go` (371 lines, production)
- `internal/daemon/runregistry_test.go` (251 lines, already `package daemon_test` — converts cleanly)

Symbols: `RunHandle`, `RunRegistry`, `NewRunRegistry`, and their methods.

**Two narrow API changes are required, and only two:**

1. **Export `snapshotWithKeys()`.** Three callers: `handlerpause_policy_37zy8.go:212`,
   `stategather.go:130`, `stalewatch.go:625`.
2. **Add a setter for the unexported `aborted` field.** It is written directly at
   `stalewatch.go:701`, `:919` and `:981` (`handle.aborted.Store(true)`). The `Aborted()` getter
   already exists; a `MarkAborted()` does not.

**`export_runregistry_test.go` (94 lines) must be SPLIT, not moved.** These shims go to the new
package: `ExportedNewRunRegistry`, `ExportedRunHandleIsAborted`, `ExportedMarkRunAborted`,
`ExportedRunRegistryRegister`. These are unrelated event-tap shims and stay in daemon:
`ExportedPerRunEventTap`, `noopExportedEmitter`, `ExportedNewPerRunEventTap`,
`ExportedNewCapturedSpawnProof`.

**Not part of this extraction, despite the names:** `run_registry_has_no_writer_test.go`,
`bootreconcile_unreadable_registry_test.go`, `orphansweep_unreadable_registry_test.go`,
`universal_run_registry_readers_test.go`. Those concern the on-disk universal run registry
(`internal/run`), which is a different thing.

## Done when

1. `internal/runregistry` exists and `go list -deps ./internal/runregistry` contains no
   `internal/daemon`.
2. All 19 daemon production files that name `RunRegistry`/`RunHandle`/`NewRunRegistry` compile
   against the new package.
3. The 26 daemon test files that name those symbols pass unmodified in behaviour — 15 white-box files
   need only an import and qualifier change; 11 are already external.
4. `tools/lintreport/allow.txt` gains nothing. One existing entry
   (`internal/daemon/runregistry_test.go gocritic`) travels with the file.
5. A `depguard` rule denying `internal/daemon` is added for the new package. Copy the shape used by
   `crew`, `gitprobe`, `projectconfig`, `runloop` or `harness/shared` in `.golangci.yml`.

## Limits

- **Only the two API changes named above.** If a third looks necessary, that is a finding worth
  reporting, not a licence to widen the surface.
- Do not move the `internal/run` on-disk registry. Different subsystem, same word.
- No freeze gate needs editing for this one. `scripts/runloop-freeze-gate.sh` fences
  `RunRegistryPort`/`RunHandlePort` — the runloop-side interfaces — not `RunRegistry`/`RunHandle`.
- **`bootstate.go` lines 112–150 are a collision hotspot.** Three extractions edit that 40-line
  window. This one goes first; the others rebase onto it.
