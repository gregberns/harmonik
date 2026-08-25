---
id: lint-burn-last-three-linters
title: The last six rows — three banned imports in core, two test assertions, one naked return
type: task
priority: 3
labels: [lint, gate, depguard, testifylint, nakedret, clear-the-ground]
depends_on: [lint-rekey-exclusion-list]
blocks: []
workstream: W1
batch: 3
---

## Problem

`tools/lintreport/allow.txt` holds 956 tolerated findings. **6 of them belong to three linters that
have almost no entries each** — measured 2026-08-24: `depguard` 3, `testifylint` 2, `nakedret` 1.
They are collected here because none is worth dispatching alone, and because two of the three are
coupled to work another lane owns. All six, in full:

**`depguard` — 3, all in `internal/core`.** The component matrix in `.golangci.yml` forbids these
imports from the `core` list:

| Site | Import |
|---|---|
| `internal/core/policydocument.go` | `gopkg.in/yaml.v3` |
| `internal/core/policyexprevaluator.go` | `github.com/expr-lang/expr` |
| `internal/core/policyexprevaluator.go` | `github.com/expr-lang/expr/vm` |

This is not a lint fix. It is an architectural finding: the policy code inside `internal/core`
reaches for a YAML parser and a general expression evaluator, and the matrix says the core package
set may not. **The real repair is to move those two files out of `internal/core`, which is exactly
what the core split does.** Coordinate with that lane, or the move happens twice.

**`testifylint` — 2, both in `internal/runloop/scenariogate_test.go`** (`exitErrorWithCode` and
`killedExitError`). Message: `error-is-as: use require.ErrorAs`. Mechanical.

**`nakedret` — 1, in `internal/daemon/workloop.go` `beadRunOne`.** Message: `naked return in func
beadRunOne with 500 lines of code`. **That function is the run machine's, and `beadrunone-extract-phases` (W2) owns it.** The finding is also a symptom rather than a cause: the naked return is only a
problem because the function is 500 lines, and the run-machine work makes it small.

## Scope

- `tools/lintreport/allow.txt` — the 6 lines whose second tab-separated field is `depguard`,
  `testifylint` or `nakedret`.
- `internal/runloop/scenariogate_test.go`, `internal/core/policydocument.go`,
  `internal/core/policyexprevaluator.go`.
- **Not** `internal/daemon/workloop.go`. See Limits.
- Nothing else.

Order: `testifylint` first — it is two lines and costs nothing. Then decide the `depguard` question
with whoever owns the core split. `nakedret` is handed over, not done here.

## Done when

1. `awk -F'\t' '$2=="testifylint"' tools/lintreport/allow.txt | wc -l` prints `0`.
2. `awk -F'\t' '$2=="depguard"' tools/lintreport/allow.txt | wc -l` prints `0`, **and
   `.golangci.yml`'s `core` deny list is unchanged** — proved by `git diff .golangci.yml` being
   empty. The entries leave because the imports left `internal/core`, not because the rule was
   relaxed.
3. The single `nakedret` row is either gone, or it is named in the commit body as handed to the
   run-machine lane with the reason. Either is an acceptable finish for this task; leaving it
   unmentioned is not.
4. `make lint-allow` exits 0 with the tree in that state.
5. `scripts/lint-allow-ratchet.sh` exits 0 in both windows.
6. `make fast` is green, and every test that covered an edited file still runs and still passes.

## Limits

- **Never widen the allow list.** No `//nolint`, no re-keying, no deleting doc comments to satisfy a
  metric.
- **Do not amend the `depguard` component matrix in `.golangci.yml` to make these three go away.**
  Adding `yaml.v3` or `expr-lang/expr` to the `core` allow list is the same act as widening the lint
  allow list, one file over, and the matrix is enforced configuration. If the matrix is genuinely
  wrong, that is an operator call and it is not made inside this task — report it.
- **Do not open `internal/daemon/workloop.go`.** `beadrunone-extract-phases` owns that function, and
  a second lane editing the file re-fingerprints the entries the first lane is working through. Hand
  the `nakedret` row to that task.
- **Do not delete or skip a test to remove a finding.**
- **Do not use `-write`.** `go run ./tools/lintreport -allow ... -write` rebuilds the list from the
  current findings and silently adds anything new alongside the removals.
- **Co-tenants re-fingerprint.** The key hashes the linter, the message and the formatted enclosing
  declaration (`tools/lintreport/main.go` `findingDigest`, `enclosing`). The two `testifylint` rows
  sit in a file that also holds five `gosec` entries, and one of them shares a declaration. Fix the
  co-tenant in the same landing and delete both lines; never paste a replacement hash in.
