---
id: lint-burn-error-returns
title: Repair the 22 places an error is swallowed, unwrapped, or marshalled unchecked
type: task
priority: 2
labels: [lint, gate, nilerr, errorlint, errchkjson, clear-the-ground]
depends_on: [lint-rekey-exclusion-list]
blocks: []
workstream: W1
batch: 3
---

## Problem

`tools/lintreport/allow.txt` holds 956 tolerated findings. **22 of them are the three
error-correctness linters** — measured 2026-08-24: `nilerr` 16, `errorlint` 4, `errchkjson` 2. They
are one task because they are one subject: an error that exists and does not travel.

`nilerr` — 16 entries. The message is ``error is not nil (line N) but it returns nil``: a function
looked at a real failure and reported success.

| Package | Entries |
|---|---|
| `internal/daemon` | 14 |
| `internal/handler` | 1 |
| `internal/scenario` | 1 |

**12 are production and 4 are tests.** The densest single site is
`internal/daemon/notifystream.go` (4). The rest are one apiece across `walcheckpoint.go`,
`spendmeter_hkk3f8g.go`, `reviewgateanomaly_hktnmjy.go`, `quiesce.go`,
`perqueuespendmeter_tigaf11.go`, `bandwidthtuner.go`, `gate_dispatch.go` and
`postsuiteleaksensor.go`.

`errorlint` — 4 entries, all `internal/daemon`: ``non-wrapping format verb for fmt.Errorf. Use `%w`
to format errors``. In `pasteinject.go` `statTaskFileVia` and three test factories.

`errchkjson` — 2 entries, both in `cmd/harmonik-twin-session/main.go` `marshalStatusJSON`.

`nilerr` is the one that costs real debugging time: twelve production paths that report success on a
failure. Several of them are in the daemon's spend, quiesce and checkpoint code, where reporting
success on a failed write is how state goes quietly wrong.

## Scope

- `tools/lintreport/allow.txt` — the 22 lines whose second tab-separated field is `nilerr`,
  `errorlint` or `errchkjson`.
- The Go files their location comments name.
- Nothing else.

Suggested order:

1. The 12 production `nilerr` entries, `internal/daemon/notifystream.go` first.
2. The 4 `errorlint` entries — `%v` becomes `%w`, mechanical.
3. `errchkjson` in `cmd/harmonik-twin-session/main.go`.
4. The 4 test-file `nilerr` entries.

## Done when

1. `awk -F'\t' '$2=="nilerr" || $2=="errorlint" || $2=="errchkjson"' tools/lintreport/allow.txt |
   wc -l` prints `0`.
2. `make lint-allow` exits 0 with the tree in that state.
3. `scripts/lint-allow-ratchet.sh` exits 0 in both windows.
4. `make fast` is green, and every test that covered an edited file still runs and still passes.
5. Each production `nilerr` site now has a test that reaches the failure and asserts the caller sees
   it. Turning `return nil` into `return err` and never proving a caller notices leaves the same
   defect one frame up.

## Limits

- **Never widen the allow list.** No `//nolint`, no re-keying, no new `.golangci.yml` exclusion, no
  deleting doc comments to satisfy a metric.
- **Do not repair `nilerr` by deleting the error check.** Removing the `if err != nil` block makes
  the finding go away and makes the bug permanent. The error is either handled or returned.
- **A deliberate swallow is allowed, and it must say so in code.** Some of these sixteen may be
  correct — a best-effort cleanup, a probe whose failure means "absent". Where that is true, restate
  the code so the intent is visible (assign to `_` with a comment saying why, or return a sentinel),
  and name each such site in the commit body so a reviewer can disagree with a stated claim.
- **Do not delete or skip a test to remove a finding.**
- **Do not use `-write`.** It rebuilds the list from current findings and silently adds new ones.
- **Co-tenants re-fingerprint.** The key hashes the linter, the message and the formatted enclosing
  declaration (`tools/lintreport/main.go` `findingDigest`, `enclosing`). **2 of the 16 `nilerr` and
  3 of the 4 `errorlint` entries share a declaration with another linter's entry**, mostly `gosec`.
  Fix the co-tenant in the same landing and delete both lines; never paste a replacement hash in.
- **The `gosec` overlap has no hard ordering, so check before you edit a shared declaration.** This
  task shares 3 declarations with `lint-burn-gosec` — all three are the `errorlint` rows — and the
  pair is not serialized. `lint-burn-gosec` blocks `lint-burn-errcheck` and `lint-burn-gocritic`, not
  this task, so it can run beside you. Before editing a declaration, grep
  `tools/lintreport/allow.txt` for its file and name, and see whether another lane owns a row inside
  it. The three are `internal/daemon/mergetomain_hkftyvo_test.go` `mergeToMainCommittingFactory`,
  `internal/daemon/mergetomain_stripruncontext_hk4je_test.go` `stripRunCtxWorktreeFactory`, and
  `internal/daemon/workloop_precommit_factory_test.go` `workloopFixturePreCommitWorktreeFactory` —
  the last of which carries a `gocritic` row as well, so `lint-burn-gocritic` is a third lane in it.
- **Leave `gocognit`, `cyclop` and `funlen` entries alone.**
