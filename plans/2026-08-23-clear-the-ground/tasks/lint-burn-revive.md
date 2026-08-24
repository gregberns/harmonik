---
id: lint-burn-revive
title: Give the 67 revive findings the doc comments and the Go names they ask for
type: task
priority: 2
labels: [lint, gate, revive, clear-the-ground]
depends_on: [lint-rekey-exclusion-list]
blocks: []
workstream: W1
batch: 3
---

## Problem

`tools/lintreport/allow.txt` holds 956 tolerated findings. **67 of them are `revive`** — measured
2026-08-24 with `awk -F'\t' '$2=="revive"' tools/lintreport/allow.txt | wc -l`.

Where they sit:

| Package | Entries |
|---|---|
| `internal/queue` | 18 |
| `internal/daemon` | 12 |
| `internal/runloop` | 11 |
| `internal/scenario` | 7 |
| `internal/core` | 6 |
| `evaltasks` | 4 |
| 7 other packages | 1–3 each |

**60 are in production code and 7 in `_test.go` files** — the most production-weighted group in this
workstream. It also has the tightest single cluster in the whole allow list:
**`internal/queue/types.go` alone holds 15 of the 67.** After that,
`internal/daemon/runfailed_bead_reset_s20z_test.go` (5), `internal/runloop/runshell.go` (4) and
`internal/core/agentevents_hqwn59.go` (4).

Sampled rules: `exported` — an exported type, method or const with no doc comment — dominates at 45
of 52 sampled; then `var-naming` (6, underscores in Go names) and `if-return` (1).

So this group is mostly **writing the doc comment an exported symbol is missing**, and it is the one
group here where the repair ADDS prose rather than removing it. `internal/queue/types.go` is a single
sitting for a third of the task.

## Scope

- `tools/lintreport/allow.txt` — the 67 lines whose second tab-separated field is `revive`.
- The Go files their location comments name, starting with `internal/queue/types.go`.
- Nothing else.

Suggested order: `internal/queue/types.go` first (15 rows, one file, one shape), then the other
`exported` findings package by package, then the six `var-naming` renames last because a rename has
callers.

## Done when

1. `awk -F'\t' '$2=="revive"' tools/lintreport/allow.txt | wc -l` prints `0`.
2. `make lint-allow` exits 0 with the tree in that state.
3. `scripts/lint-allow-ratchet.sh` exits 0 in both windows.
4. `make fast` is green, and every test that covered an edited file still runs and still passes.

## Limits

- **Never widen the allow list.** No re-keying around an entry, no `//nolint`, no new `.golangci.yml`
  exclusion.
- **Do not silence `exported` by unexporting a symbol that has callers outside its package.** That is
  an API change wearing a lint fix as a disguise. Unexporting is fine only where the package is the
  sole caller, and then say so in the commit body.
- **Write the doc comment the symbol deserves, not the shortest string that satisfies the rule.**
  `// Foo is a Foo.` passes the linter and is worse than nothing. If you cannot say what a symbol is
  for, that is a finding about the symbol, not about the comment — report it.
- **Do not delete doc comments anywhere in this task.** The comment cut is over; this program's
  standing rule is that a metric is never satisfied by removing prose.
- **Do not use `-write`.** It rebuilds the list from current findings and silently adds new ones.
- **Co-tenants re-fingerprint.** The key hashes the linter, the message and the formatted enclosing
  declaration (`tools/lintreport/main.go` `findingDigest`, `enclosing`). Only **1 of these 67**
  shares a declaration with another linter, so this group is unusually safe to run in parallel with
  the others. Even so: if a gate goes red on an identity you did not touch, that is the co-tenant
  effect — fix that finding too and delete its line rather than pasting the new hash in.
