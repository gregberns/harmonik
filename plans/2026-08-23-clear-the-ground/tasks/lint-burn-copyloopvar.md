---
id: lint-burn-copyloopvar
title: Delete the 16 loop-variable copies Go 1.22 made unnecessary
type: chore
priority: 1
labels: [lint, gate, copyloopvar, clear-the-ground]
depends_on: [lint-rekey-exclusion-list]
blocks: []
workstream: W1
batch: 3
---

## Problem

`tools/lintreport/allow.txt` holds 956 tolerated findings. **16 of them are `copyloopvar`** —
measured 2026-08-24 with `awk -F'\t' '$2=="copyloopvar"' tools/lintreport/allow.txt | wc -l`.

**All 16 are in `internal/daemon`, all 16 are in `_test.go` files, and all 16 carry the same
message**: ``The copy of the 'for' variable "x" can be deleted (Go 1.22+)``. Each is one line —
`tc := tc` at the top of a subtest loop — that stopped being needed when the language changed.

The files:

| File | Entries |
|---|---|
| `internal/daemon/moderesolve_single_override_dot_default_hkgwy_test.go` | 3 |
| `internal/daemon/socket_queue_test.go` | 2 |
| `internal/daemon/harnessresolve_test.go` | 2 |
| 9 further `internal/daemon` test files | 1 each |

**This is the smallest, most mechanical and most self-contained group in the workstream, and that is
why it should run first.** Nobody has yet removed an entry from the re-keyed allow list and watched
the gate stay green. This task proves that loop end to end — remove entries, run `make lint-allow`,
run the ratchet — on sixteen rows where a mistake costs nothing and the repair is deleting a line.
Everything else in W1.3 assumes that loop works.

## Scope

- `tools/lintreport/allow.txt` — the 16 lines whose second tab-separated field is `copyloopvar`.
- The 12 `internal/daemon` test files their location comments name.
- Nothing else.

## Done when

1. `awk -F'\t' '$2=="copyloopvar"' tools/lintreport/allow.txt | wc -l` prints `0`.
2. `make lint-allow` exits 0 with the tree in that state.
3. `scripts/lint-allow-ratchet.sh` exits 0 in both windows — working tree against HEAD, and HEAD
   against its parents.
4. `make fast` is green and every affected test still runs. In particular the parallel subtests in
   these files still pass with `-race`: the redundant copy is the thing that used to make them safe,
   so removing it is only correct if the loop variable is per-iteration, which Go 1.22 guarantees and
   a `-race` run confirms.

## Limits

- **Never widen the allow list.** No `//nolint`, no re-keying, no new `.golangci.yml` exclusion, no
  deleting doc comments to satisfy a metric.
- **Delete the copy; do not rename around it.** Renaming the shadow to something else silences the
  linter and leaves the redundant line.
- **Do not delete or skip a test to remove a finding.**
- **Do not use `-write`.** `go run ./tools/lintreport -allow ... -write` rebuilds the list from the
  current findings and silently ADDS anything new alongside the removals. Delete lines by hand.
- **Co-tenants re-fingerprint.** The key hashes the linter, the message and the formatted enclosing
  declaration (`tools/lintreport/main.go` `findingDigest`, `enclosing`). Only **2 of these 16** share
  a declaration with another linter (one `gosec`, one `errcheck`). Fix that co-tenant in the same
  landing and delete both lines; never paste a replacement hash in.
- **Report what you learned about the removal loop.** This task is also the shakedown for the other
  twelve. If `make lint-allow` or the ratchet behaves differently from the description above, that
  is the most valuable thing this task produces — say so.
