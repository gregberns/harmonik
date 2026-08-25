---
id: lint-burn-gocritic
title: Clear the 82 gocritic findings, half of which are unnamed results and unexplained nolints
type: task
priority: 2
labels: [lint, gate, gocritic, clear-the-ground]
depends_on: [lint-rekey-exclusion-list, lint-burn-gosec]
blocks: []
workstream: W1
batch: 3
---

## Problem

`tools/lintreport/allow.txt` holds 956 tolerated findings. **82 of them are `gocritic`** — measured
2026-08-24 with `awk -F'\t' '$2=="gocritic"' tools/lintreport/allow.txt | wc -l`.

Where they sit:

| Package | Entries |
|---|---|
| `internal/daemon` | 51 |
| `cmd/harmonik` | 17 |
| `internal/core` | 7 |
| 7 other packages | 1 each |

**47 are in production code and 35 in `_test.go` files** — the only group in this workstream where
production outweighs tests. No single file holds more than four; the densest are
`internal/daemon/t3_exploratory_test.go` (4), `internal/daemon/scheduler.go` (3),
`internal/daemon/pasteinject.go` (3), `internal/daemon/orphansweep.go` (3) and
`cmd/harmonik/keeper_enable_doctor_cmd.go` (3).

Sampled checks over `internal/daemon`, `cmd/harmonik`, `internal/core`, `internal/queue`,
`internal/runloop` and `internal/workspace`: `unnamedResult` (16), `whyNoLint` (12), `ifElseChain`
(11), `paramTypeCombine` (11), `stringXbytes` (5), `unnecessaryBlock` (5), `builtinShadow` (4),
`emptyStringTest` (4), `importShadow` (3), and a tail of `filepathJoin`, `nestingReduce`,
`appendAssign` and `regexpSimplify`.

**`whyNoLint` is worth reading before you start.** Twelve tolerated findings say a `//nolint`
directive in this tree carries no explanation. Those twelve are a map of where the codebase already
silences a linter without saying why, and this program cares about that more than about the other
seventy.

## Scope

- `tools/lintreport/allow.txt` — the 82 lines whose second tab-separated field is `gocritic`.
- The Go files their location comments name.
- Nothing else.

Suggested order:

1. The `whyNoLint` entries — add the explanation the directive is missing, or delete the directive
   if the finding under it is already gone.
2. `unnamedResult` and `paramTypeCombine` — signature-level, mechanical, no behaviour change.
3. `ifElseChain`, `unnecessaryBlock`, `emptyStringTest`, `stringXbytes`, `unconvert`-adjacent rewrites.
4. `builtinShadow` and `importShadow` — renames, so check the blast radius first.

## Done when

1. `awk -F'\t' '$2=="gocritic"' tools/lintreport/allow.txt | wc -l` prints `0`.
2. `make lint-allow` exits 0 with the tree in that state.
3. `scripts/lint-allow-ratchet.sh` exits 0 in both windows.
4. `make fast` is green, and every test that covered an edited file still runs and still passes.

## Limits

- **Never widen the allow list.** No re-keying around an entry, no `//nolint`, no new `.golangci.yml`
  exclusion, no deleting doc comments to satisfy a metric.
- **A `whyNoLint` finding is not repaired by deleting the explanation requirement.** Write the reason
  or remove the directive. If the reason turns out to be "this is real debt we are not fixing today",
  say that in the directive and file it.
- **Do not use `-write`.** It rebuilds the list from current findings and silently adds new ones.
- **Co-tenants re-fingerprint.** The key hashes the linter, the message and the formatted enclosing
  declaration (`tools/lintreport/main.go` `findingDigest`, `enclosing`). **21 of these 82 entries
  share a declaration with a different linter's entry** — 8 with `gosec`, 4 with `errcheck`, 4 with
  `gocognit`, 2 with `contextcheck`, the rest scattered. Fix the co-tenant in the same landing and
  delete both lines; never paste a replacement hash in.
- **This task runs after `lint-burn-gosec`** because those two share 8 declarations, and two lanes
  editing the same declaration at once each re-fingerprint the other's tolerated findings — both
  gates go red with no legal repair.
- **Lesser overlaps have no hard ordering, so check before you edit a shared declaration.** This task
  shares 4 declarations with `lint-burn-errcheck`; elsewhere in the set, `lint-burn-gosec` shares 4
  with `lint-burn-unparam` and 3 with `lint-burn-error-returns`, and `lint-burn-errcheck` shares 4
  with `lint-burn-context-plumbing` (the `noctx` rows). None of those pairs is serialized. Before
  editing a declaration, grep `tools/lintreport/allow.txt` for its file and name, and see whether
  another lane owns a row inside it.
- **Five of the co-tenants are complexity suppressions.** `gocognit`, `cyclop` and `funlen` are last
  in this program on purpose. All five are `gocognit`: three in `cmd/harmonik/keeper_enable_doctor_cmd.go`
  (`runKeeperEnable`, and two in `runKeeperDoctor`), one in `cmd/harmonik/supervise/pause.go`
  `sendOperatorOp`, one in `internal/daemon/orphansweep.go` `RunOrphanSweep`. If a `gocritic` fix in
  one of those declarations would drag you into restructuring the function, leave that row and say
  which one.
- **Two of these `gocritic` rows are in `internal/daemon/workloop.go`, which the run-machine lane
  holds** — `productionWorktreeFactory:589` and `resolveHEAD:650`. Do not open that file in this
  task. Leave those two rows and name them in the commit body.
