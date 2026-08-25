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

1. `awk -F'\t' '$2=="exhaustive"' tools/lintreport/allow.txt | wc -l` prints `0`. This group really
   can reach `0`: its one co-tenant is a plain `gocritic` row, not a complexity suppression, so no
   Limit below forbids any of the seven. `awk … | wc -l` exits 0 whatever it counts, so the printed
   number is the verdict and the exit status says nothing.
2. `make lint-allow` exits 0 with the tree in that state.
3. `scripts/lint-allow-ratchet.sh` exits 0 in both windows.
4. `make fast` is green, and every test that covered an edited file still runs and still passes.
5. **Each of the seven switches now answers every case, and none of them was silenced.** Two checks,
   neither of which changes a shared type:
   - `grep -rn --include='*.go' -e 'nolint:[a-z, ]*exhaustive' \
     -e 'exhaustive:ignore' -e 'exhaustive:enforce' .` prints nothing. The tree holds no such
     directive today. A directive is the ten-second repair this task exists to prevent, and it is one
     way item 1 can read `0` while the switch still ignores a case. **Keep the whole command on one
     logical line — that is what the `\` is for — and put the path operand last, after the options.**
     A `grep` with no path reads standard input and hangs; a path before the options works only
     because most greps permute, and BSD grep under `POSIXLY_CORRECT=1` instead exits 1 and prints
     nothing. Every one of those failures prints nothing, and "prints nothing" is the pass condition
     here, so a broken command and a clean tree look identical.
   - The commit body lists all seven declarations, one line each, and says which repair each one got:
     a `default:` clause, or the full case list written out. Say it per switch, in the order of the
     table above, so a reviewer can check the list against the table without reading the diff.
     **For a `default:`, say what that branch does with a member it does not recognise.** "It has a
     `default:`" is not an answer. `.golangci.yml` sets `default-signifies-exhaustive: true`, so an
     empty `default:` or one holding only a comment silences the linter completely: item 1 reads `0`,
     the grep prints nothing, and the switch still ignores every case. That is the same ten-second
     repair in a different spelling, and no command in this list catches it — only your sentence does.
     A `default:` switch cannot go red when the enum grows. A written-out list can, and Limits below
     allow that on purpose — where you chose it, say what a future member is meant to do.

   **Do not add a member to `queue.ItemStatus`, `core.Kind` or any other enum to test this**, even
   with the intent to revert. A new member makes `exhaustive` findings in switches this task never
   touched and that are not on the allow list, so `make lint-allow` goes red for reasons that are not
   yours and the result tells you nothing about these seven. No command reports the gate verdict for
   one declaration, which is why the check above is a grep plus a written report.

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
