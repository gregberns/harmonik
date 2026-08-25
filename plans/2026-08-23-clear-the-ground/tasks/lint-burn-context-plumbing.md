---
id: lint-burn-context-plumbing
title: Pass the context the daemon already has — 32 findings across three context linters
type: task
priority: 2
labels: [lint, gate, contextcheck, noctx, containedctx, clear-the-ground]
depends_on: [lint-rekey-exclusion-list]
blocks: []
workstream: W1
batch: 3
---

## Problem

`tools/lintreport/allow.txt` holds 956 tolerated findings. **32 of them are the three context
linters** — measured 2026-08-24: `contextcheck` 23, `noctx` 7, `containedctx` 2. They are one task
because they are one defect seen from three angles: a `context.Context` exists at the call site and
does not reach the work.

`contextcheck` — 23 entries, **every one in production code, none in tests**:

| Package | Entries |
|---|---|
| `internal/daemon` | 18 |
| `internal/workspace` | 3 |
| `internal/codexdriver` | 1 |
| `internal/scenario` | 1 |

Densest: `internal/daemon/workloop.go` (3), `internal/daemon/scheduletick.go` (3),
`internal/daemon/subscribe.go` (2), `internal/daemon/quiesce.go` (2), `internal/daemon/daemon.go` (2),
`internal/workspace/remotematerialize.go` (2). Sampled messages are call chains:
``Function `LoadCached->Load->parse` should pass the context parameter``,
``Function `acquirePidfile->AcquirePidfile` should pass the context parameter``.

`noctx` — 7 entries, 5 in tests: `cmd/harmonik` 5, `internal/daemon` 1, `internal/projectconfig` 1.
Messages: `net.Listen must not be called. use (*net.ListenConfig).Listen`, and
`os/exec.Command must not be called. use os/exec.CommandContext`.

`containedctx` — 2 entries, both `internal/daemon`: `daemon.go` `Config` and
`dot_postexit_fixture_test.go` `dotFixtureOpts`. A struct field holding a context.

This group matters out of proportion to its size: a subprocess started without a context is a
subprocess cancellation cannot reach, and this daemon's job is starting and stopping processes.

## Scope

- `tools/lintreport/allow.txt` — the 32 lines whose second tab-separated field is `contextcheck`,
  `noctx` or `containedctx`.
- The Go files their location comments name.
- Nothing else.

Suggested order:

1. The 7 `noctx` entries. Mechanical: `exec.Command` becomes `exec.CommandContext`, `net.Listen`
   becomes `(*net.ListenConfig).Listen`. The context is already in scope at all seven sites.
2. The 2 `containedctx` entries.
3. `contextcheck`, which is the real work, package by package: `internal/workspace` (3),
   `internal/codexdriver` and `internal/scenario` (1 each), then `internal/daemon`.

## Done when

1. The rows this task is allowed to fix are gone, and the rows it is not allowed to fix are still
   there. The unfiltered count does not reach `0`, and chasing `0` is how this task gets stuck.
   Two commands, both filtered on the location comment:
   - `awk -F'\t' '($2=="contextcheck" || $2=="noctx" || $2=="containedctx") &&
     $3 ~ /internal\/daemon\/workloop\.go/' tools/lintreport/allow.txt | wc -l` prints `3`, unchanged.
     Those three rows are `beadRunOne` twice and `productionWorktreeFactory`. The run-machine lane
     owns that file. Hand them over; do not count them as yours.
   - `awk -F'\t' '($2=="contextcheck" || $2=="noctx" || $2=="containedctx") &&
     $3 !~ /internal\/daemon\/workloop\.go/' tools/lintreport/allow.txt | wc -l` prints the rows you did
     not fix, out of 29. `0` is the best case. It is above `0` if you left a row on a declaration
     that also carries a `gocognit` or `cyclop` suppression, which Limits let you do rather than
     restructure the function. There is a second cause: Limits also name four `noctx` declarations
     shared with `lint-burn-errcheck`, and that pair is not serialized, so if that lane lands first a
     row re-keys and one more can survive. Nine rows carry a complexity co-tenant today, on seven
     declarations:
     `internal/daemon/commsrecvhandler_nnwaa.go` `commsSendHandlerImpl.HandleCommsRecv`,
     `internal/daemon/crewstart.go` `crewHandlerImpl.HandleCrewStart`,
     `internal/daemon/daemon.go` `startWithHooks` (2 rows),
     `internal/daemon/orphansweep.go` `RunOrphanSweep`,
     `internal/daemon/pasteinject.go` `pasteInjectQuitOnCommit`,
     `internal/daemon/subscribe.go` `SubscribeHub.HandleSubscribe` (2 rows), and
     `internal/projectconfig/projectconfig.go` `parseKeeperBlock`. So this count can land anywhere
     from `0` to `9`. Name every row you leave in the commit body, by file and symbol, with the
     reason. The count must equal the number of rows you named, and every row you did not name must
     be gone.
2. `make lint-allow` exits 0 with the tree in that state.
3. `scripts/lint-allow-ratchet.sh` exits 0 in both windows.
4. `make fast` is green, and every test that covered an edited file still runs and still passes.
5. Where a subprocess or a listener gained a context, cancelling that context stops it, and a test
   proves it. Plumbing a context that nothing honours is a signature change, not a fix.

`awk … | wc -l` exits 0 whatever it counts, so the printed number is the verdict and the exit status
says nothing. The `$3` filter reads the location comment, which the allow list calls an aid, so look
at the rows it keeps.

## Limits

- **Never widen the allow list.** No `//nolint`, no re-keying, no new `.golangci.yml` exclusion, no
  deleting doc comments to satisfy a metric.
- **Do not use `context.TODO()` or `context.Background()` to satisfy a linter.** A fresh background
  context passes `contextcheck` and cancels nothing, which is the failure the rule exists to catch.
  If no context is available at a site, the repair is to add the parameter to the caller.
- **Do not delete or skip a test to remove a finding.**
- **Do not use `-write`.** It rebuilds the list from current findings and silently adds new ones.
- **Co-tenants re-fingerprint, and this task has the worst exposure in the workstream.** The key
  hashes the linter, the message and the formatted enclosing declaration
  (`tools/lintreport/main.go` `findingDigest`, `enclosing`). **11 of the 23 `contextcheck` entries
  and 5 of the 7 `noctx` entries share a declaration with another linter's entry, and 8 of the
  `contextcheck` ones share it with a `gocognit` or `cyclop` suppression.** Fix a plain co-tenant in
  the same landing and delete both lines. Never paste a replacement hash in.
- **The `errcheck` overlap has no hard ordering, so check before you edit a shared declaration.**
  This task shares 4 declarations with `lint-burn-errcheck` — all four are `noctx` rows — and the
  pair is not serialized. Before editing a declaration, grep `tools/lintreport/allow.txt` for its
  file and name, and see whether another lane owns a row inside it. The four are
  `cmd/harmonik/confirm_verdict_test.go` `startFakeVerdictDaemon`,
  `cmd/harmonik/sleepwake_cmd_coverage_test.go` `TestSendSleepWakeRequest`, and
  `cmd/harmonik/supervise/coverage_drain_test.go` `TestSendOperatorOp_Acked` and
  `TestSendOperatorOp_DaemonError`. `startFakeVerdictDaemon` carries a `gosec` row too, so
  `lint-burn-gosec` is a third lane in that one.
- **Leave the complexity suppressions alone.** `gocognit`, `cyclop` and `funlen` are last in this
  program on purpose: they should vanish because a function got smaller. If threading a context into
  such a declaration would drag you into restructuring it, leave that row and say which one.
- **Three `contextcheck` rows are in `internal/daemon/workloop.go`, which the run-machine lane holds.**
  Do not open that file in this task. Report those three rows as handed over.
