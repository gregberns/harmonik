# 05 — Test QA Critique (Round 2, post-fix tree)

**Lens:** test quality on the CURRENT tree. HEAD = `b43638ca` (await-ack), one
commit past the `1ccc2b90` the brief named. Out of scope (covered by others):
impure paste/respawn paths (hk-zole), `*-flywheel` integration leak (hk-0ouc).

---

## 1. Suite status — GREEN, deterministic, race-clean, no leak

```
go test ./internal/keeper/...   → ok, 76.3% of statements (stable across 3 fresh runs)
go test -race ./internal/keeper → ok, 3.9s, no data race
go test ./cmd/harmonik/...      → ok 34.1% ; .../digest 28.3% ; .../supervise 29.7%
```
- Coverage rose 74.4% → **76.3%** since round 1 (the new `restartnow.go` +
  `awaitack.go` brought their own tests).
- **tmux guard:** 9 → 9 across every default-tag run; the `-race` run dipped to
  9 (fleet churn), never above baseline. **No leak from the default suite.** (The
  round-1 `*-flywheel` leak is integration-tag only — out of scope, hk-0ouc.)
- The one-off **77.1%** I saw on the very first cached run was a cross-package
  caching artifact, not a real reading. Uncached is deterministically 76.3%.

### Flakiness — NOT reproduced
Round 1 flagged a "watcher/respawn timing flake." Across 3 fresh single runs +
one `-race` run, the suite was green every time. The timing-sensitive tests
(`ActLoop_HKVPNP_*`, `AwaitAck_*`, reactive harness) all completed in <0.1s with
no variance. The await-ack/never-appears tests use an **injected `fakeClock`**
(`awaitack_test.go:46-60,124-161`) rather than wall-clock sleeps, which removes
the obvious flake vector. **Verdict: no flake characterized on this tree** — if
the round-1 flake lived in a respawn path, it is either fixed or only reachable
under integration tags (out of scope). I did not run integration tags (fork-bomb
caution + hk-0ouc ownership).

---

## 2. "Certifies-the-bug" audit — the round-1 smell is FIXED

Round 1's headline finding (04-testability §4) was that
`keeper_restart_now_hk4zy9_test.go` **certified the no-op**: it asserted "marker
written → exit 0 → marker re-read" and never checked a keeper was alive, so it
green-lit the silent-no-op bug.

**That test was rewritten and now asserts the OPPOSITE contract**
(`cmd/harmonik/keeper_restart_now_hk4zy9_test.go:50-64`):
```
TestRunKeeperRestartNow_NoPane_FailsLoudly:
  // No .sid/.ctx and no live tmux session → must fail (exit 1), NOT exit 0.
  // This is the regression guard for the silent no-op.
  if code := runKeeperRestartNow(...) == 0 { t.Fatalf("silent no-op regression") }
```
The whole marker → `RunOnDemand` state machine the old test exercised is gone;
`restartnow.go` is now a 181-line direct synchronous path. **No
certifies-the-bug test survives.** I also checked the two new test files the
brief named:

- **`restartnow_test.go`** — non-vacuous. `TestRestartNow_HappyPath` asserts the
  exact ordered sequence `[ack, /clear, /session-resume <handoff>]`
  (`:94-107`); the four refusal tests each assert **both** a non-nil error AND
  `len(rec.calls)==0` — i.e. they prove the dangerous `/clear` is NOT injected on
  unverified-SID / missing-handoff / stale-handoff (`:135-137,152-154,172-174`).
  That `calls==0` assertion is the anti-vacuity guard; without it a test could
  pass while still pasting `/clear`.
- **`awaitack_test.go`** — strong. Covers ack-immediate, ack-on-3rd-poll,
  never-appears (asserts `ErrAckTimeout` + exactly-one
  `session_keeper_ack_timeout` event + payload fields + `timeout_seconds==0.5`),
  **wrong-nonce discrimination** (a stale prior-cycle ACK must NOT match —
  `:166-189`), capturer-error budget exhaustion (`cap.count()==captureErrorBudget`),
  no-tmux-target, and context-cancel-emits-no-event. This is the
  closed-loop the L-manual path lacked in round 1.

---

## 3. Bug 3 (`hk-vpnp`) coverage — adequate and non-vacuous

`act_loop_hkvpnp_test.go` drives `MaybeRun` through **real on-disk handoff I/O**
(default `TruncateHandoffFn`/`ReadHandoff`/`AppendHandoffFn` — `:48-60`), not a
mock, so it tests the actual seam that misfired:

- **3b (truncation):** seeds a real non-empty handoff, lets the nonce-poll time
  out, then asserts the file still has content (`:100-107`). This exercises the
  real fix at `cycle.go:788-790` — truncate ONLY when
  `handoffHasStaleNonce(...)` is true; a genuine handoff with no keeper nonce is
  preserved.
- **3a (re-fire loop):** ticks `95→50→95→50→95` on the SAME un-cleared SID
  (mimicking the live `-000001/-000002` signature) and asserts ≤1
  `/session-handoff` injection (`:181-196`). This exercises the
  `lastFireWasAbort` escape-hatch suppression at `cycle.go:603-605,823-825`.

Both tests assert the *bug signature directly* (file emptied / second nonce
fired) rather than an internal flag, so they would catch a regression that
re-introduced the behavior. **Non-vacuous.**

---

## 4. Gaps (severity-ordered) — what the merged tests miss

### G1 (MED-HIGH) — CLI "fails loudly" tests pass via the TRIVIAL gate, not the real one
`TestRunKeeperRestartNow_NoPane_FailsLoudly` (`hk4zy9_test.go:53-64`) and the
await-ack `no-pane-timeout` case (`keeper_await_ack_hkuldg_test.go:27-31`) both
rely on `ResolveTmuxTarget` returning `""` for a never-seen agent, so they trip
the **empty-target guard** — the FIRST gate in `RestartNow`/`AwaitAck` — and
exit non-zero/3. They never reach the unverified-SID / poll-then-timeout logic
*with a real pane present*. So "fails loudly when the keeper is dead but the pane
exists" — the operator's actual round-1 pain (writes succeed, nothing happens) —
is **still not asserted at the CLI layer**. The package-level
`TestRestartNow_UnverifiedSID_Refuses` covers the SID gate with a
non-empty target, but the CLI handler's wiring (resolve→call→exit-map) only ever
sees the empty-target path. Add a CLI case with `TmuxTarget` resolvable (or
inject a non-empty target + a fake injector) so the SID/handoff refusal exit
codes are exercised through `runKeeperRestartNow` itself.

### G2 (HIGH, but = brief #1/#2, not a *test* gap to fix in isolation)
`AwaitAck`, `RestartNow`, `Ping` are called **only** by their CLI subcommands
(`keeper_cmd.go:440,494,567`); `grep` confirms **zero non-test callers inside
`runCycle`/`MaybeRun`/`RunForPrecompact`**. So the automatic L-auto cycle is
still open-loop — it pastes `/clear`+`/session-resume` and marks the journal
`complete` with no ACK read-back. There is correspondingly **no test of an
ack-confirmed automatic cycle**, because the production code has no such path
yet. This is a code-integration gap (hk-uldg / brief #1), and a test for it
can't exist until the wiring does. Flag, don't author a test against absent code.

### G3 (MED) — no deterministic equivalent for the precompact end-to-end
`RunForPrecompact` gate logic is well covered (`precompact_test.go`:
not-managed / empty-sid / holding-dispatch / anti-loop / happy), but like
L-auto its actual paste delivery is only exercised by the dark, cooperative-twin
integration test. Same shape as round 1; the merge did not change it.

### G4 (LOW) — still no bash-hook test
`keeper-statusline.sh` (jq path, `[1m]`-window inference) has **zero** direct
tests; only the integration twin runs the script. Unchanged from round 1 §4.

---

## 5. Bottom line
The single highest-value round-1 "certifies-the-bug" defect is **resolved** — the
restart-now CLI test now guards against the silent no-op instead of certifying
it, and the new `restartnow`/`awaitack`/`hkvpnp` tests are well-constructed and
non-vacuous (they assert the bug signature, not an internal flag). The suite is
green, race-clean, and leak-free with no flake reproducible on the default tier.
**Top gap to fix now: G1** — make the CLI fail-loudly tests exercise the
unverified-SID / dead-keeper path with a *real* pane present, so they cover the
operator's actual pain instead of passing on the trivial empty-target guard. G2
(wire await-ack into the auto cycle) is the larger reliability hole but is a
code change owned by the brief, not a test-only fix.
