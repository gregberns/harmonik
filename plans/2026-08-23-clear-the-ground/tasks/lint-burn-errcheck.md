---
id: lint-burn-errcheck
title: Check the 162 error returns the build currently lets fall on the floor
type: task
priority: 1
labels: [lint, gate, errcheck, clear-the-ground]
depends_on: [lint-rekey-exclusion-list, lint-burn-gosec]
blocks: []
workstream: W1
batch: 3
---

## Problem

`tools/lintreport/allow.txt` holds 956 tolerated findings. **162 of them are `errcheck`** — measured
2026-08-24 with `awk -F'\t' '$2=="errcheck"' tools/lintreport/allow.txt | wc -l`. It is the second
largest non-complexity group in the list.

Where they sit:

| Package | Entries |
|---|---|
| `internal/daemon` | 121 |
| `cmd/harmonik` | 21 |
| `evaltasks` | 13 |
| `tools` | 7 |

**142 of the 162 are in `_test.go` files.** The 20 production entries are all in build-support code,
not in the product: `evaltasks/eval-cli-kv/kvcli.go` (10), `tools/testreport/main.go` (6), and four
others. Measured 2026-08-24: `internal/daemon` and `cmd/harmonik` contribute **zero** production
`errcheck` entries. So this group is a test-hygiene and tooling problem, and it carries less risk
than its size suggests.

The densest files are `internal/daemon/subscribe_test.go` (18),
`internal/daemon/notifystream_test.go` (14), `internal/daemon/commsrecvhandler_nnwaa_test.go` (12),
`evaltasks/eval-cli-kv/kvcli.go` (10) and `internal/daemon/agent_message_test.go` (8).

Sampled callees over `internal/daemon` and `cmd/harmonik`: `daemon.ExportedRunWorkLoop` (25),
`json.Marshal` (23), `io.ReadAll` (10), `os.RemoveAll` (8), `uuid.NewV7` (7), `conn.Write` and
`conn.SetReadDeadline` (8 together), and a long tail of one-offs. The `ExportedRunWorkLoop` cluster
is one shape repeated 25 times: a test starts the work loop in a goroutine and drops its error.

## Scope

- `tools/lintreport/allow.txt` — the 162 lines whose second tab-separated field is `errcheck`.
- The Go files their location comments name.
- Nothing else. No change to `scripts/lint-allow.sh`, the ratchet, `tools/lintreport/`, or
  `.golangci.yml`.

Suggested order, largest repeated shape first:

1. The 25 `ExportedRunWorkLoop` goroutine launches. One decision, applied 25 times.
2. The 20 production entries in `evaltasks/` and `tools/`.
3. `internal/daemon/subscribe_test.go`, `notifystream_test.go` and `commsrecvhandler_nnwaa_test.go`
   — 44 entries between them.
4. The remainder.

## Done when

1. `awk -F'\t' '$2=="errcheck"' tools/lintreport/allow.txt | wc -l` prints `0`.
2. `make lint-allow` exits 0 with the tree in that state.
3. `scripts/lint-allow-ratchet.sh` exits 0 in both windows — working tree against HEAD, and HEAD
   against its parents.
4. `make fast` is green, and every test that covered an edited file still runs and still passes.

## Limits

- **Never widen the allow list.** An entry leaves because the finding is fixed. No re-keying around
  it, no `//nolint`, no `errcheck` exclusion added to `.golangci.yml`, and no deleting doc comments
  to satisfy a metric.
- **`_ = f()` is a repair only where the error genuinely cannot be acted on, and it needs a reason
  next to it.** In a test the usual right answer is `require.NoError`, because an ignored error in a
  fixture is how a test comes to pass for the wrong reason. Prefer asserting to discarding, and say
  in the commit body where you chose to discard and why.
- **Do not delete or skip a test to remove a finding.**
- **Do not use `-write`.** `go run ./tools/lintreport -allow ... -write` rebuilds the list from the
  current findings and silently adds anything new. Delete lines by hand.
- **Co-tenants re-fingerprint.** The key is a hash of the linter, the normalized message and the
  formatted enclosing declaration (`tools/lintreport/main.go` `findingDigest`, `enclosing`). Editing
  a declaration changes the key of every tolerated finding inside it. **45 of these 162 entries share
  a declaration with a different linter's entry** — 17 with `gosec`, 4 with `noctx`, 4 with
  `gocritic`, 2 with `unconvert`, the rest scattered. Fix the co-tenant in the same landing and
  delete both lines. Never paste the replacement hash into the list.
- **This task runs after `lint-burn-gosec`** because those two share 17 declarations and two lanes
  editing them at once will each turn the other's gate red.
- **Lesser overlaps have no hard ordering, so check before you edit a shared declaration.** This task
  shares 4 declarations with `lint-burn-gocritic` and 4 with `lint-burn-context-plumbing` (the
  `noctx` rows). Neither pair is serialized, so either task can run beside you. Before editing a
  declaration, grep
  `tools/lintreport/allow.txt` for its file and name, and see whether another lane owns a row inside
  it. The `gocritic` four are `internal/daemon/socket_operatorpause_ry8q1_test.go` `socketOpSend`,
  `internal/daemon/t2_scenarios_test.go` `TestT2_RunFailedEventContainsExitCode`,
  `internal/daemon/t3_exploratory_test.go` `TestT3_StaleWorktreeOrphanSweep`, and
  `tools/testreport/main.go` `run`.
- **Leave `gocognit`, `cyclop` and `funlen` entries alone.** They are last in this program on purpose.
