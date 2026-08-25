---
id: lint-burn-unused
title: Delete the 65 unused symbols the build is told to ignore
type: chore
priority: 1
labels: [lint, gate, unused, deletion, clear-the-ground]
depends_on: [lint-rekey-exclusion-list]
blocks: []
workstream: W1
batch: 3
---

## Problem

`tools/lintreport/allow.txt` holds 956 tolerated findings. **65 of them are `unused`** — measured
2026-08-24 with `awk -F'\t' '$2=="unused"' tools/lintreport/allow.txt | wc -l`. Each one names a
type, function, method or field that nothing calls, and the build has been told not to mind.

Where they sit:

| Package | Entries |
|---|---|
| `internal/daemon` | 32 |
| `internal/core` | 10 |
| `internal/lifecycle` | 7 |
| `internal/crewrun` | 6 |
| `internal/runmerge` | 5 |
| 5 other packages | 1 each |

**37 are in `_test.go` files and 28 in production code.** The clusters are whole abandoned units
rather than stray symbols, which is what makes this the cheapest real subtraction in the workstream:

- `internal/crewrun/idlereap.go` — 6 entries: the type `CrewIdleReaper` and five of its methods
  (`scan`, `checkCrew`, `crewIsPersistent`, `clearCandidate`, `reap`). The exported constructor and
  `StartWatcher` beside them are not flagged because `unused` does not report exported identifiers.
  Whether anything calls them is the first thing to check — if nothing does, the file goes.
- `internal/core/budgetcounterstate_hka8bg25.go` — 5 entries and every declaration in the file: the
  unexported type `budgetCounterState`, `newBudgetCounterState`, and the methods `Accrue`,
  `CheckDispatch` and `RehydrateAccrual`. Unexported and unconstructed, so nothing outside can reach it.
- `internal/runmerge/fixture_test.go` — 5; `internal/lifecycle/clifixture_test.go` — 5;
  `internal/daemon/workloop_fidelity_fixture_wl01_test.go` — 4; `internal/daemon/pasteinject.go` — 4.

Standing rule 3 of this program applies directly: **deletion needs no permission.** A thing with no
caller and no reference is removed, and that is not a decision that needs a gate.

## Scope

- `tools/lintreport/allow.txt` — the 65 lines whose second tab-separated field is `unused`.
- The Go files their location comments name.
- Nothing else.

Suggested order: whole-unit deletions first (`internal/crewrun/idlereap.go`,
`internal/core/budgetcounterstate_hka8bg25.go`), then the test fixtures, then the scattered singles.

## Done when

1. `awk -F'\t' '$2=="unused" && $3 !~ /internal\/daemon\/workloop\.go/' tools/lintreport/allow.txt |
   wc -l` prints `0`. The unfiltered count prints `1`, not `0`. One row stays:
   `internal/daemon/workloop.go` `emitImplPresence`, because the run-machine lane holds that file and
   Limits below tell you not to open it. Read the survivor rather than trust the number —
   `awk -F'\t' '$2=="unused"' tools/lintreport/allow.txt` prints one line, and its comment names
   `workloop.go` and `emitImplPresence`. Any other survivor is a row you missed.
2. `make lint-allow` exits 0 with the tree in that state.
3. `scripts/lint-allow-ratchet.sh` exits 0 in both windows.
4. `make fast` is green. No test loses coverage of a behaviour: a symbol that only tests used is
   dead only if the test that used it is also dead — check, and say which is which.

`awk … | wc -l` exits 0 whatever it counts, so the printed number is the verdict and the exit status
says nothing. The `$3` filter reads the location comment, which the allow list calls an aid, so look
at the row it keeps.

## Limits

- **Never widen the allow list.** No `//nolint`, no re-keying, no `.golangci.yml` exclusion.
- **Do not keep a symbol alive by adding a reference to it.** A call from a test written to satisfy
  the linter is worse than the dead code, because now the dead code looks live.
- **Check for a real caller before deleting, twice.** `unused` does not see reflection, build tags,
  `go:generate`, or a name reached from a string. Grep the whole tree including `scripts/` and
  `.harmonik/` before removing an exported symbol. If a symbol is exported and the package is a
  library boundary, say in the commit body why nothing outside can want it.
- **Deleting the code is the repair. Deleting the doc comment is not.** Do not strip comments to move
  any metric.
- **Do not use `-write`.** It rebuilds the list from current findings and silently adds new ones.
- **Co-tenants re-fingerprint.** The key hashes the linter, the message and the formatted enclosing
  declaration (`tools/lintreport/main.go` `findingDigest`, `enclosing`). Only **1 of these 65**
  shares a declaration with another linter, so this group is safe to run beside the others. Deleting
  a whole file removes its co-tenants outright, which is the good case — delete their allow-list
  lines too.
- **`internal/daemon/workloop.go` holds one of these rows.** The run-machine lane holds that file.
  Leave it, and name it in the commit body.
