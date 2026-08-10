# Migration Check Specification

## Requirements

Migration checks prove behavior parity and track every compatibility seam to removal.

## Research summary

Existing coverage is broad but does not prove composition-root wiring. Differential and mutation checks address that gap.

## Approach

Compare old and new actions, journals, events, and final state for success, deferral, timeout, recovery, and collision paths. Maintain a temporary source allowlist for legacy seams. Remove an entry only with its migration task.

Each differential test constructs a legacy cycler and a new cycler. Both receive the same fake clock and observations. The oracle compares ordered actions, terminal state, journal phase and reason, emitted event payloads, and returned errors.

Fixtures are nonce-confirmed success, every transient entry gate, handoff timeout, clear-confirm timeout, journal recovery, and transcript collision. The gate table covers managed, crisp idle, dispatch, sleep, hold, operator presence, answer grace, boot grace, anti-loop state, and recent operator activity.

## Files and changes

- Add parity tests under `internal/keeper` and `cmd/harmonik`.
- Add the isolated production-constructor scenario under `cmd/harmonik`.
- Add `internal/keeper/legacy_seams.txt` and `scripts/keeper-seam-ratchet.sh`.
- Remove the list with the final adapter-removal task.

## Error handling and compatibility

A parity difference blocks migration unless the change has a separate behavior specification. One detached-worktree mutation removes the automation-origin check. Another removes one required command dependency. Each mutation must make its scenario fail.

The compatibility list starts with every `CyclerConfig` function field plus `fnPane`, `fnGauge`, `fnHandoff`, and `fnRespawn`. Named tasks remove policy fields, production seams, test seams, broad adapters, and the list.

## Acceptance criteria

- Differential tests cover the five named behavior groups.
- Constructor wiring has an isolated end-to-end test.
- A source check fails for an unlisted compatibility seam.
- Final grep finds no legacy function seam or broad adapter.

## Verification

Run `scripts/keeper-seam-ratchet.sh`. Run `go test ./internal/keeper ./internal/keepertest ./cmd/harmonik` and `go test -tags=integration ./internal/keeper`. Run the detached-worktree mutation commands documented in the scenario tests. Run `make fast` while migrating. Run `make full` before acceptance.
