---
id: lint-burn-prealloc-unconvert
title: Clear the 17 pre-allocation and redundant-conversion findings
type: chore
priority: 2
labels: [lint, gate, prealloc, unconvert, clear-the-ground]
depends_on: [lint-rekey-exclusion-list]
blocks: []
workstream: W1
batch: 3
---

## Problem

`tools/lintreport/allow.txt` holds 956 tolerated findings. **17 of them are these two linters** —
measured 2026-08-24: `prealloc` 11, `unconvert` 6. They are one task because each alone is too small
to dispatch and both are local, mechanical edits inside a single statement.

`prealloc` — 11 entries. Message: ``Consider pre-allocating `x` ``.

| Package | Entries |
|---|---|
| `internal/daemon` | 5 |
| `cmd/harmonik`, `internal/brcli`, `internal/codexreactor`, `internal/core`, `internal/workflow`, `tools` | 1 each |

**6 production, 5 test.** Sampled slice names: `qItems`, `contentLines`, `messages`, `out`,
`stalePaths`, `sessions`, `entries`.

`unconvert` — 6 entries, message ``unnecessary conversion``: `internal/daemon` 5,
`internal/core` 1.

Neither is a correctness defect. They are here because the allow list is meant to reach zero and
these are the cheapest rows in it.

## Scope

- `tools/lintreport/allow.txt` — the 17 lines whose second tab-separated field is `prealloc` or
  `unconvert`.
- The Go files their location comments name.
- Nothing else.

## Done when

1. `awk -F'\t' '$2=="prealloc" || $2=="unconvert"' tools/lintreport/allow.txt | wc -l` prints `0`.
2. `make lint-allow` exits 0 with the tree in that state.
3. `scripts/lint-allow-ratchet.sh` exits 0 in both windows.
4. `make fast` is green, and every test that covered an edited file still runs and still passes.

## Limits

- **Never widen the allow list.** No `//nolint`, no re-keying, no new `.golangci.yml` exclusion, no
  deleting doc comments to satisfy a metric.
- **`prealloc` is advice, not law, and a wrong pre-allocation is worse than none.** `make([]T, 0, n)`
  is right when `n` is known and the loop appends at most `n` times. Where the count is not known,
  the honest answer is to restate the loop so it is, or to report the row as one that should not be
  fixed — with the reason. Do not write `make([]T, n)` where the code appends: that ships `n` zero
  values ahead of the real ones, and it is a real bug the linter did not ask for.
- **Do not delete or skip a test to remove a finding.**
- **Do not use `-write`.** It rebuilds the list from current findings and silently adds new ones.
- **Co-tenants re-fingerprint.** The key hashes the linter, the message and the formatted enclosing
  declaration (`tools/lintreport/main.go` `findingDigest`, `enclosing`). **3 of the 6 `unconvert`
  entries share a declaration with another linter's entry**, one of them with a `funlen` suppression.
  `prealloc` has none. Fix a plain co-tenant in the same landing and delete both lines; never paste
  a replacement hash in.
- **Leave `gocognit`, `cyclop` and `funlen` entries alone.** They are last in this program on purpose.
