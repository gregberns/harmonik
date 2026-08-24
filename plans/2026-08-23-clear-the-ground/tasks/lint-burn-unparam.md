---
id: lint-burn-unparam
title: Remove the 28 parameters and results unparam says nothing varies or reads
type: task
priority: 2
labels: [lint, gate, unparam, clear-the-ground]
depends_on: [lint-rekey-exclusion-list]
blocks: []
workstream: W1
batch: 3
---

## Problem

`tools/lintreport/allow.txt` holds 956 tolerated findings. **28 of them are `unparam`** — measured
2026-08-24 with `awk -F'\t' '$2=="unparam"' tools/lintreport/allow.txt | wc -l`.

Where they sit:

| Package | Entries |
|---|---|
| `internal/daemon` | 15 |
| `cmd/harmonik` | 4 |
| `internal/core` | 3 |
| `internal/apptap` | 2 |
| `internal/watch` | 2 |
| `internal/brcli` | 1 |
| `internal/hookrelay` | 1 |

**21 are in `_test.go` files and 7 in production code.** They are scattered — no file holds more
than two.

Sampled messages over `internal/daemon` and `cmd/harmonik`: ``captainTmuxSessionName - result 1
(error) is always nil``, ``makeDoctorCfg - result cleanup is never used``, ``smokeCheckCommitOnBranch
- branch always receives "main"``, ``writeTestJSONL - result 0 (string) is never used``,
``squashLanding - repoRoot is unused``.

Three shapes: a returned `error` that is always `nil`, a parameter that every caller passes the same
value for, and a result nobody reads. All three are the signature lying about the function, and this
program's frame is subtraction — a parameter with one possible value is a parameter to delete.

## Scope

- `tools/lintreport/allow.txt` — the 28 lines whose second tab-separated field is `unparam`.
- The Go files their location comments name.
- Nothing else.

Suggested order: the 7 production entries first (the signature is public-facing inside the module and
the change is worth more there), then the 21 test-file ones.

## Done when

1. `awk -F'\t' '$2=="unparam"' tools/lintreport/allow.txt | wc -l` prints `0`.
2. `make lint-allow` exits 0 with the tree in that state.
3. `scripts/lint-allow-ratchet.sh` exits 0 in both windows.
4. `make fast` is green, and every test that covered an edited file still runs and still passes.

## Limits

- **Never widen the allow list.** No `//nolint`, no re-keying, no new `.golangci.yml` exclusion, no
  deleting doc comments to satisfy a metric.
- **An always-nil error is sometimes correct to keep, and then you say why.** A function that
  implements an interface, or one whose whole point is that a future implementation can fail, has a
  reason for the result. That reason goes in the commit body and the signature stays — but then the
  finding stays too, and you have not finished. Prefer removing it; escalate the ones you genuinely
  cannot.
- **Do not satisfy the linter by adding a caller that passes a different value.** Manufacturing
  variety to hide a fixed parameter is the same disease as manufacturing a reference to hide dead code.
- **Do not delete or skip a test to remove a finding.**
- **Do not use `-write`.** It rebuilds the list from current findings and silently adds new ones.
- **Co-tenants re-fingerprint.** The key hashes the linter, the message and the formatted enclosing
  declaration (`tools/lintreport/main.go` `findingDigest`, `enclosing`). **6 of these 28 share a
  declaration with another linter's entry**, 4 of them with `gosec`. Fix the co-tenant in the same
  landing and delete both lines; never paste a replacement hash in.
- **The `gosec` overlap has no hard ordering, so check before you edit a shared declaration.** This
  task shares 4 declarations with `lint-burn-gosec` and the pair is not serialized — `lint-burn-gosec`
  blocks `lint-burn-errcheck` and `lint-burn-gocritic`, not this task, so it can run beside you.
  Before editing a declaration, grep `tools/lintreport/allow.txt` for its file and name, and see
  whether another lane owns a row inside it. The four are
  `internal/daemon/bandwidthtuner_test.go` `writeTestJSONL`, `internal/daemon/branching.go`
  `squashLanding`, `internal/daemon/mergetomain_hkftyvo_test.go` `mergeToMainFixtureHeadSHA`, and
  `internal/daemon/sub_workflow_runner_hkoe6_test.go` `swWriteDotFile`.
- **Leave `gocognit`, `cyclop` and `funlen` entries alone.** They are last in this program on purpose.
