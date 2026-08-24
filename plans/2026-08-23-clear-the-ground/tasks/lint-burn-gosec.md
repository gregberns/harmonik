---
id: lint-burn-gosec
title: Fix the 182 security findings the build currently tolerates
type: task
priority: 1
labels: [lint, gate, gosec, clear-the-ground]
depends_on: [lint-rekey-exclusion-list]
blocks: [lint-burn-errcheck, lint-burn-gocritic]
workstream: W1
batch: 3
---

## Problem

`tools/lintreport/allow.txt` holds 956 tolerated findings. **182 of them are `gosec`** — measured
2026-08-24 with `awk -F'\t' '$2=="gosec"' tools/lintreport/allow.txt | wc -l`. These are security
findings the build is told to ignore, which is why the plan names this one first.

Where they sit:

| Package | Entries |
|---|---|
| `internal/daemon` | 109 |
| `cmd/harmonik` | 26 |
| `internal/runloop` | 8 |
| `internal/queue` | 5 |
| `internal/runmerge` | 4 |
| 21 other packages | 3 or fewer each |

**121 of the 182 are in `_test.go` files and 61 are in production code.** The production 61 are the
security surface; the test 121 are mostly file and subprocess handling in fixtures. The densest
single files are `internal/daemon/bandwidthtuner_test.go` (12), `internal/daemon/eagerfill_em063_test.go`
(10), `internal/daemon/quiesce_test.go` (6) and `internal/daemon/pasteinject.go` (6).

Sampled rule codes across `internal/daemon`, `cmd/harmonik`, `internal/core`, `internal/queue`,
`internal/runloop`, `internal/workspace` and `internal/watch`: G304 file inclusion via a variable
(39), G204 subprocess launched with a variable (37), G301 directory permissions (37), G306
`WriteFile` permissions (36), G115 integer overflow on conversion (4), G302 file permissions (3),
G101 hardcoded credentials (2), G104 unhandled error (2).

The permission findings (G301, G302, G306 — 76 of the sample) are the mechanical majority: a mode
constant changes. G204 and G304 need a judgement per site about whether the input is attacker-reachable.

## Scope

- `tools/lintreport/allow.txt` — the 182 lines whose second tab-separated field is `gosec`. The
  third field is a `# <path>:<line> <symbol>` comment that says where each one lives.
- The Go files those comments name.
- Nothing else. This task does not change `scripts/lint-allow.sh`, the ratchet, `tools/lintreport/`,
  or `.golangci.yml`.

Suggested order, because it puts the security value first and the risk last:

1. The 61 production entries, starting with `internal/daemon/pasteinject.go`,
   `internal/daemon/verdictexecutor_rc025a.go` and `internal/runloop/scenariogate.go`.
2. The permission findings in test fixtures — mode constants, no behaviour change.
3. The remaining test-file G204 and G304 entries.

## Done when

1. `awk -F'\t' '$2=="gosec"' tools/lintreport/allow.txt | wc -l` prints `0`.
2. `make lint-allow` exits 0 with the tree in that state — no `gosec` finding falls outside the list,
   because there is no `gosec` entry left in it.
3. `scripts/lint-allow-ratchet.sh` exits 0. Both of its windows must pass: the identity set only
   shrank in the working tree, and it only shrank in the commit.
4. `make fast` is green, and every test that covered an edited file still runs and still passes. A
   finding removed by deleting or skipping the test that reached it is not removed.

## Limits

- **Never widen the allow list.** This is the standing rule of the program. An entry leaves the list
  because the finding is fixed. Do not re-key around it, do not add a `//nolint` directive, and do
  not delete the code's doc comments to satisfy a metric. `.golangci.yml` is not an escape hatch
  either: excluding `gosec` from a path there is the same act one file over.
- **Do not delete or skip a test to remove a finding.** A fixture that reads a file by a computed
  path can take a checked, rooted path. It does not have to stop existing.
- **Do not use `-write`.** `go run ./tools/lintreport -allow ... -write` rebuilds the whole list from
  the current findings, so it silently ADDS anything new alongside the removals. Remove lines by
  hand and prove the removal with `make lint-allow`.
- **Co-tenants re-fingerprint.** The key is a hash of the linter, the normalized message, and the
  formatted enclosing declaration — see `tools/lintreport/main.go` `findingDigest` and `enclosing`.
  Editing a declaration changes the key of EVERY tolerated finding inside it. **43 of these 182
  entries share a declaration with a different linter's entry** (17 with `errcheck`, 8 with
  `gocritic`, 4 with `unparam`, 4 with `gocognit`, 3 with `errorlint`, the rest scattered). When you
  fix one of those, the co-tenant goes red. Fix the co-tenant too and delete both lines. **Never
  paste the new hash into the list** — the count did not rise, but that is still a widening and the
  ratchet is right to refuse it.
- **`lint-burn-gocritic` runs after this task** because those two share 8 declarations, and two lanes
  editing the same declaration at once each re-fingerprint the other's tolerated findings — both
  gates go red with no legal repair. Finish and land this one first.
- **Lesser overlaps have no hard ordering, so check before you edit a shared declaration.** This task
  shares 4 declarations with `lint-burn-unparam` and 3 with `lint-burn-error-returns` (the `errorlint`
  rows). Neither pair is serialized. Before editing a declaration, grep `tools/lintreport/allow.txt`
  for its file and name, and see whether another lane owns a row inside it.
- **Four of those co-tenants are `gocognit` entries.** `gocognit`, `cyclop` and `funlen` are
  deliberately last in this program: they should vanish because a function got smaller, not because
  somebody fixed them in place. If a `gosec` fix in such a declaration would drag you into
  restructuring the function, stop and leave that one row for the run-machine lane. Say which rows
  you left and why.
