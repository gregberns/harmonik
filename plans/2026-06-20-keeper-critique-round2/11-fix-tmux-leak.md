# 11 — Fix the `*-flywheel` tmux-session leak (hk-0ouc)

**Bead:** hk-0ouc. **Status:** FIXED + verified.
**Subject:** the `harmonik-<hash>-flywheel` tmux sessions that leak when the
keeper integration suite runs on a tmux-equipped host.

---

## Root cause — NOT the keeper suite

The baseline (`15-reliability-baseline.md` §5) attributed the 2 leaked
`*-flywheel` sessions to the keeper integration Twin suite. That attribution is
**wrong** — it was cross-package contamination on the shared tmux server.

Empirical findings (poller capturing every `*-flywheel` session + its pane
`start_command` during isolated runs):

1. The keeper Twin suite (`go test -tags integration ./internal/keeper/... -run
   Twin`) creates and reaps ONLY its own `hksav-twin-*` / `twe2e*` sessions, by
   EXACT name, in `t.Cleanup`. It creates **zero** `*-flywheel` sessions. The
   keeper test binary is `keeper.test`; no keeper test calls
   `FlywheelSessionName`, `supervise start`, or `tmux new-session … -flywheel`.

2. The leaked `*-flywheel` panes all run
   `harmonik.test supervise _shim /tmp/hkt-…` — i.e. they come from the
   **`cmd/harmonik` package test binary** (`harmonik.test`), not keeper. The
   `<hash>` in each leaked name is `FlywheelSessionName(t.TempDir())`.

3. The actual leakers are the `TestSupervise_Start*` tests in
   `cmd/harmonik/supervise_integration_hkqx702_test.go`. Two of them call
   `supervisecmd.RunStart(--project <tmpdir> --command …)`:
   - `TestSupervise_StartCommandFlagSetsConfigCommand`
   - `TestSupervise_StartDoubleDashCommand`

   These tests were written assuming *"no tmux in CI"* (see their inline
   comments). On a dev/fleet box **with** tmux, `RunStart` passes the tmux
   pre-flight and actually creates `harmonik-<hash>-flywheel` (running
   `supervise _shim`, `remain-on-exit on`). After the `_shim` process exits the
   empty session lingers, and the test had **no teardown** for it — one leaked
   flywheel session per call.

The baseline saw "2 leaked per keeper run" because the live fleet was running
`cmd/harmonik` integration tests **concurrently** on the same tmux server; the
counts intermingled. (`supervise_reap_hkizs8s_test.go` already reaps its own
flywheel session — that file was never the problem.)

## Fix — reap at the source, exact-name

`cmd/harmonik/supervise_reap_hkizs8s_test.go`: added a shared helper

```go
func cleanupFlywheelSession(t *testing.T, dir string) {
    sessionName := supervisecmd.FlywheelSessionName(dir)
    t.Cleanup(func() {
        _ = exec.Command("tmux", "kill-session", "-t", "="+sessionName).Run()
    })
}
```

It kills, by **exact name** (`=` anchor, no glob, no list-and-kill), the
deterministic `FlywheelSessionName(dir)` for that test's unique `t.TempDir()` /
`socketSafeTempDir` path — so it can only ever touch that test's own session,
never a real `harmonik-<realhash>-flywheel`, a `*-default` spawn target, or a
captain/crew session. Killing an absent session is a no-op.

`cmd/harmonik/supervise_integration_hkqx702_test.go`: registered
`cleanupFlywheelSession(t, dir)` in the two leaking `RunStart`-with-`--command`
tests, plus the lock-held `StartHoldsLockDuringSessionCreation` test (a
defensive no-op there — it exits 25 before the tmux step).

No production code changed — the leak was purely a test-teardown gap.

## Verification (single runs, NO loops)

The shared tmux server had concurrent fleet test runs leaking flywheel sessions
throughout, so verification used a poller to **attribute** each `*-flywheel`
session to its creating process and computed *new survivors = (flywheel present
after) − (flywheel present before)* for MY runs only.

| Check | Result |
|---|---|
| Fixed `cmd/harmonik` `TestSupervise_Start*` run, NEW flywheel survivors attributable to it | **0** (was 2–5 before the fix) |
| Same, second run | **0** new survivors |
| Keeper Twin suite — its own `hksav-twin*`/`twe2e*` sessions left behind | **NONE** (always reaped) |
| Keeper Twin suite — flywheel sessions it created | **0** (3 survivors during the run all traced to `harmonik.test`, the concurrent fleet, not `keeper.test`) |
| `go test ./internal/keeper/...` (default) | **PASS** (2.7 s) |
| `go test ./cmd/harmonik/` (incl. edited tests) | **PASS** (23.5 s) |
| `go build ./...` / `go vet ./cmd/harmonik/...` | **clean** |

tmux session counts (this worktree's verification, attributing concurrent-fleet
noise out): the leftover test-flywheel sessions (14, all `harmonik.test
supervise _shim`, none the live `a3dc45482890` project) were swept by exact
name → tmux back to baseline **9**.

**Before fix:** each `TestSupervise_Start*` run leaked ≥2 `*-flywheel` sessions.
**After fix:** 0.

## Note for the round-2 re-run

`15-reliability-baseline.md` §5 should be corrected: the keeper integration Twin
suite does NOT leak `*-flywheel` sessions. The leak was always the
`cmd/harmonik` `TestSupervise_Start*` tests, surfaced only on tmux-equipped
hosts and conflated with the keeper run because both ran concurrently on the one
tmux server. The fork-bomb session-name *family* is shared, but the keeper suite
is not a member.
