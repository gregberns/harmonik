---
id: lint-burn-exhaustive
title: Close the 7 switches that do not handle every case of the type they switch on
type: task
priority: 2
labels: [lint, gate, exhaustive, clear-the-ground]
depends_on: [lint-rekey-exclusion-list]
blocks: []
workstream: W1
batch: 3
---

## Problem

`tools/lintreport/allow.txt` holds 956 tolerated findings. **7 of them are `exhaustive`** — measured
2026-08-24 with `awk -F'\t' '$2=="exhaustive"' tools/lintreport/allow.txt | wc -l`.

All seven, with the declaration each sits in:

| Entry | Kind |
|---|---|
| `internal/daemon/stategather.go` `LiveStateBuilder.buildQueues` | production |
| `internal/daemon/statedisk.go` `diskQueues` | production |
| `internal/daemon/eagerfill_em063.go` `buildInQueueSet` | production |
| `internal/core/cpregistry_hka8bg2.go` `MapRegistry.Register` | production |
| `internal/daemon/sub_workflow_runner_hkoe6_test.go` `TestSubWorkflowRunner_EventsCarryParentRunID` | test |
| `internal/daemon/dot_postexit_fixture_test.go` `runDotFixtureBead` | test |
| `internal/core/trailers_test.go` `TestEM017_RequiredSetConformance` | test |

Sampled messages: `missing cases in switch of type queue.ItemStatus:
queue.ItemStatusDeferredForLedgerDep`, `missing cases in switch of type queue.QueueStatus:
queue.QueueStatusActive, queue.QueueStatusCompleted, ...`, `missing cases in switch of type
core.Kind: core.KindGate, core.KindHook`, `missing cases in switch of type
core.TrailerRequirement: core.TrailerKnownExtension`.

**Small, but this is the group with the sharpest trap in it**, so it is written as its own task rather
than folded in with the other short ones. Every `exhaustive` message names the specific missing cases.
The allow-list key is a hash of the linter, the normalized message and the enclosing declaration, so
**the message itself is part of the key**. Add one member to `queue.ItemStatus` or `core.Kind` and
every message over that type changes, which re-fingerprints every `exhaustive` entry that mentions
it — including entries in files nobody touched. The gate goes red on the next person to add an event
type, and the ten-second repair that suggests itself is a new allow-list line.

## Scope

- `tools/lintreport/allow.txt` — the 7 lines whose second tab-separated field is `exhaustive`.
- The Go files and switches their location comments name.
- Nothing else.

## Done when

1. `awk -F'\t' '$2=="exhaustive"' tools/lintreport/allow.txt | wc -l` prints `0`.
2. `make lint-allow` exits 0 with the tree in that state.
3. `scripts/lint-allow-ratchet.sh` exits 0 in both windows.
4. `make fast` is green, and every test that covered an edited file still runs and still passes.
5. **Adding a member to one of the switched enums no longer produces an `exhaustive` finding in these
   files.** Prove it: add a throwaway member to `queue.ItemStatus`, confirm the gate is still green
   for these seven declarations, and revert. That is the acceptance test — after this task the list
   holds nothing that a new event type can re-fingerprint.

## Limits

- **Never widen the allow list.** No `//nolint`, no re-keying, no new `.golangci.yml` exclusion, no
  deleting doc comments to satisfy a metric.
- **Adding an event type re-fingerprints every `exhaustive` finding over that type, and the repair
  for that is a `default:` clause — never a new allow-list entry.** This is the standing rule the
  task exists to make permanent. A switch that ends in `default:` cannot go red when the enum grows.
- **Enumerate the cases, or add a `default:`. Do not pick by convenience.** Where each case really
  needs its own answer, write them out and let a future member break the build on purpose — but then
  the switch must not be one an added event type can reach silently. Where the switch has one right
  answer for anything unrecognised, `default:` is correct and is what makes this list stable. Say per
  switch which you chose and why.
- **Do not make the switch exhaustive by removing an enum member.** Deleting `KindGate` to satisfy a
  switch is a product change disguised as a lint fix.
- **Do not delete or skip a test to remove a finding.**
- **Do not use `-write`.** `go run ./tools/lintreport -allow ... -write` rebuilds the list from the
  current findings and silently adds anything new.
- **Co-tenants re-fingerprint.** The key hashes the linter, the message and the formatted enclosing
  declaration (`tools/lintreport/main.go` `findingDigest`, `enclosing`). Only **1 of these 7** shares
  a declaration with another linter (a `gocritic` entry). Fix it in the same landing and delete both
  lines; never paste a replacement hash in.
