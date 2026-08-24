---
id: lint-burn-forbidigo
title: Replace the 40 panics that forbidigo says should be returned errors
type: task
priority: 2
labels: [lint, gate, forbidigo, clear-the-ground]
depends_on: [lint-rekey-exclusion-list]
blocks: []
workstream: W1
batch: 3
---

## Problem

`tools/lintreport/allow.txt` holds 956 tolerated findings. **40 of them are `forbidigo`** — measured
2026-08-24 with `awk -F'\t' '$2=="forbidigo"' tools/lintreport/allow.txt | wc -l`.

Every sampled one is the same message: ``use of `panic` forbidden because "return an error; panics
only in main/init"``. So this group is a single rule broken 40 times, and the repair shape is the
same each time — return an error, or move the panic into a place the rule allows.

Where they sit:

| Package | Entries |
|---|---|
| `internal/daemon` | 10 |
| `internal/handlercontract` | 9 |
| `internal/core` | 6 |
| `internal/workers` | 5 |
| `internal/eventbus` | 2 |
| `internal/workspace` | 2 |
| `scripts` | 2 |
| 4 other packages | 1 each |

**26 are in production code and 14 in `_test.go` files.** The tightest clusters are
`internal/daemon/detectorbarrier_rc020b_test.go` (5),
`internal/handlercontract/harnessregistry.go` (4) and
`internal/handlercontract/adapterregistry_hc012.go` (4) — the two registry files are one design
choice made twice: a registration path that panics on a bad registration instead of refusing it.

That registry pair is the one place here where the repair is a real signature change and not a
line edit. It is also the one worth doing: a constructor that can refuse an invalid value is what
`PRINCIPLES.md` asks for.

## Scope

- `tools/lintreport/allow.txt` — the 40 lines whose second tab-separated field is `forbidigo`.
- The Go files their location comments name.
- Nothing else.

Suggested order:

1. The 14 test-file panics. In a test the repair is usually `t.Fatalf`, which is strictly better
   because it names the test that failed.
2. `internal/handlercontract/harnessregistry.go` and `adapterregistry_hc012.go` — 8 rows, one design
   change: registration returns an error.
3. `internal/core`, `internal/workers`, `internal/daemon` and the singles.

## Done when

1. `awk -F'\t' '$2=="forbidigo"' tools/lintreport/allow.txt | wc -l` prints `0`.
2. `make lint-allow` exits 0 with the tree in that state.
3. `scripts/lint-allow-ratchet.sh` exits 0 in both windows.
4. `make fast` is green, and every test that covered an edited file still runs and still passes.
5. Where a panic became a returned error, a caller now handles it and a test proves the error path
   is reachable. A returned error nobody checks is not an improvement over a panic.

## Limits

- **Never widen the allow list.** No `//nolint`, no re-keying, no deleting doc comments to satisfy a
  metric, and do not add a path to the `forbidigo` exclusions in `.golangci.yml` — that is the same
  act one file over.
- **Do not turn a panic into a silent no-op.** If the condition really is impossible, the repair is
  to make it unrepresentable, not to drop the check. If it is possible, it returns an error.
- **Do not delete or skip a test to remove a finding.**
- **Do not use `-write`.** It rebuilds the list from current findings and silently adds new ones.
- **Co-tenants re-fingerprint.** The key hashes the linter, the message and the formatted enclosing
  declaration (`tools/lintreport/main.go` `findingDigest`, `enclosing`). Only **3 of these 40** share
  a declaration with another linter, and **2 of those 3 share it with a complexity suppression** —
  leave the complexity rows alone and name them in the commit body.
