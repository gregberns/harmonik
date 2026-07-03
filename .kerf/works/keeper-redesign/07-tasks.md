# keeper-redesign — Tasks

> **OPERATOR DIRECTIVE (hard rule).** EVERY task entry below carries FOUR explicit parts:
> **(1) Code change**, **(2) Deployment step**, **(3) Test step**, **(4) Validation step**.
> A task that lists only a code change is INCOMPLETE and MUST NOT be marked done.
> Deploy/test/validate mechanics are spelled out once in `00-deploy-test-validate.md`; each
> task names the bead-specific specifics.

## Dependency / ordering summary

```
hk-baf4 (twin harness flags) ──┐  (standalone prereq, no prod change)
hk-psds (CLI flag-parity)     ──┤  (independent)
hk-p9kw (HARMONIK_AGENT env)  ──┘  (independent)

hk-7rmv (P0-B: authoritative UUIDv4 + DELETE 664-888)   ← MUST LAND FIRST (the unblocker)
   ├─→ hk-81wk (gauge-liveness: DeriveOccupancyFromTranscript)
   ├─→ hk-0t5s (OperatorAttached → activity-recency)
   └─→ hk-75mr (P0-A: gauge-independent live-pane recovery)   (also needs hk-0t5s)
            └─→ NEW-forcerestart (wire ForceRestartFn behind --force-restart)  (needs hk-7rmv + hk-75mr + hk-0t5s)

hk-nlio (VALIDATION GATE) ← RED on main now; GREEN only after ALL P0/P1 land
hk-bpkv (P3 cleanup: consolidate threshold constants, defaults-PIN enforced) ← any time; no value change
```

**Land order:** hk-7rmv first → then hk-81wk / hk-0t5s (parallel) → hk-75mr → NEW-forcerestart
→ hk-nlio flips green. Prereqs hk-baf4 / hk-psds / hk-p9kw can land in parallel up front.
hk-bpkv is opportunistic.

---

## hk-7rmv — P0-B: deterministic UUIDv4 session-id binding + DELETE watcher.go:664-888 (MUST LAND FIRST)

The unblocker. Replaces the heuristic identity state machine with authoritative launch-SID
binding and DELETES the heuristic block (net LOC down — this is the "break the pattern" proof).

1. **Code change.**
   - Add `--session-id <uuid4>` to the `harmonik keeper` watcher command; validate it is a
     lowercase UUIDv4, fail-closed at boot otherwise.
   - At boot, call `WriteManagedSessionFn` ONCE to write the launch SID into `.managed`;
     thereafter read identity from `.managed` only.
   - DELETE the block `internal/keeper/watcher.go:664-888` and its helpers/knobs per the
     deletion checklist (D1–D11, K1–K7) in `05-spec-drafts/keeper-identity-and-liveness.md §3.2`.
     Remove `isUppercaseUUID`, the identity uses of `isUUIDv7`, `Write/Clear/ReadSuppressState`,
     and the auto-clear `WriteManagedSessionFn(...,"")` call sites (watcher.go:685, :725).
   - Thread the SID: `scripts/captain-tools/captain-launch.sh` (`SID="$(uuidgen)"` → keeper
     `--session-id $SID`); `crewstart.go resolveSessionID` for crews.
2. **Deployment step.** `go install ./cmd/harmonik`; relaunch keeper-watched sessions
   (captain via `captain-launch.sh`, crews via crew start) so the new `--session-id` flag is
   passed. No daemon restart. Confirm with `harmonik keeper doctor --agent <name>`:
   `BoundSessionID == minted UUIDv4`, `.managed` holds that lowercase UUIDv4.
3. **Test step.** RED-THEN-GREEN. Unit (T1): assert `WriteManagedSessionFn` called exactly
   once over a full cycle; negative test that a transcript-filename UUID is NEVER written to
   `.managed`; grep gate that the deleted identifiers (§3.2 verification list) return 0 hits.
   Integration (T2, `-tags=integration`): twin-pane bind from a passed `--session-id`.
4. **Validation step.** Net-LOC check: `git diff --stat internal/keeper/ cmd/harmonik/keeper_cmd.go`
   confirms identity-machinery LOC is NEGATIVE and the rebind CLI is gone. **Phase-0 empirical
   gate (BEFORE merging the deletion):** prove `--session-id` survives `/clear` (capture `.ctx`
   before+after, assert equal); if false, STOP and surface to captain.

---

## hk-baf4 — twin test-harness flags `--emit-na` / `--suppress-statusline-after` (standalone prereq)

Adds twin-harness affordances to reproduce gauge-NA and gauge-goes-stale-while-pane-alive in
the integration tier. Single definition, NO production behavior change.

1. **Code change.** Add `--emit-na` (twin emits a gauge `NA` sample) and
   `--suppress-statusline-after <dur>` (twin stops writing `.ctx` after a delay) to the twin
   test harness only. Single flag definition; no prod code path touched.
2. **Deployment step.** Build-only (test harness): `go build -tags=integration ./internal/keeper/...`.
   Nothing to relaunch — harness is test-scoped.
3. **Test step.** Integration (T2): a smoke test invokes the twin with each new flag and
   asserts the harness emits NA / stops statusline writes as specified.
4. **Validation step.** Confirm the flags are referenced by hk-81wk's stale-gauge test and
   hk-nlio's acceptance gate (they are the reproduction levers). No prod-behavior diff in
   `git diff` outside the harness file.

---

## hk-psds — keeper CLI parser flag-parity (`--agent` flag vs positional; leading-dash reject)

Removes the silent-misparse class where `enable`/`doctor`/`set-dispatching`/`clear-dispatching`
disagree on `--agent`-as-flag vs positional.

1. **Code change.** Normalize all keeper subcommands to accept the agent name via a single
   parsing convention (`--agent <name>` flag), reject a leading-dash value as the positional
   to avoid silent misparse. (Note: `rebind` is REMOVED by hk-7rmv, so it drops from this set.)
2. **Deployment step.** `go install ./cmd/harmonik`. Relaunch any session/launcher that calls
   these subcommands with the old positional form so they use the unified flag.
3. **Test step.** Unit (T1): table test per subcommand asserting `--agent foo` and the
   rejected-leading-dash case parse identically across subcommands; a misparse case is RED on
   main, GREEN after.
4. **Validation step.** `harmonik keeper doctor --agent <name>` and `... set-dispatching
   --agent <name>` resolve the SAME agent; run each subcommand live and confirm no
   silent-empty-agent path.

---

## hk-p9kw — keeper hooks env-var unification onto `HARMONIK_AGENT`

Crew gauge + idle/precompact markers de-correlate because hooks read `HARMONIK_KEEPER_AGENT`
while the rest reads `HARMONIK_AGENT`. Unify onto `HARMONIK_AGENT`.

1. **Code change.** In `scripts/keeper-stop-hook.sh` and `scripts/keeper-precompact-hook.sh`,
   read `HARMONIK_AGENT` (retire `HARMONIK_KEEPER_AGENT`). Update any Go code / `enable` wiring
   that exports the legacy var.
2. **Deployment step.** `go install ./cmd/harmonik`; re-run `harmonik keeper enable --agent
   <name>` for each watched session so the hook scripts and settings.json export the unified
   var; relaunch the sessions.
3. **Test step.** Unit (T1): the existing `keeper_enable_doctor_cmd_test.go` env assertions are
   updated to `HARMONIK_AGENT`; a test that a crew's idle/precompact marker correlates to its
   gauge agent is RED on main, GREEN after.
4. **Validation step.** Live: a crew keeper emits a gauge and an idle marker under the SAME
   agent name; `harmonik keeper doctor --agent <crew>` shows no `no_gauge` de-correlation.

---

## hk-81wk — gauge-liveness: `DeriveOccupancyFromTranscript`, write `.ctx` only when stale/absent (depends hk-7rmv)

Kills the ~3006 `no_gauge:stale` events: when the gauge is stale/absent but the pane is alive,
derive occupancy from the transcript and refresh `.ctx`.

1. **Code change.** Implement `DeriveOccupancyFromTranscript` (parse the live transcript to an
   occupancy estimate). In the watcher tick, when `.ctx` is stale/absent AND the pane is alive,
   write `.ctx` from the derived occupancy — **only when stale/absent** (never clobber a fresh
   status-line gauge). Single gauge-writer invariant preserved.
2. **Deployment step.** `go install ./cmd/harmonik`; relaunch keeper-watched sessions. Confirm
   via `harmonik subscribe --types session_keeper_*` that `no_gauge:stale` stops on a live pane.
3. **Test step.** RED-THEN-GREEN. Integration (T2, uses hk-baf4 `--suppress-statusline-after`):
   suppress the twin's statusline writes, assert the keeper refreshes `.ctx` from the transcript
   and the warn/act gates still evaluate. Unit (T1): `DeriveOccupancyFromTranscript` returns a
   plausible occupancy for a fixture transcript.
4. **Validation step.** LIVE-SOAK (independent verifier): reproduce stale-gauge-on-live-agent;
   confirm `.ctx` mtime stays current through a **>5-min idle window**. Defaults-PIN test green.

---

## hk-0t5s — `OperatorAttached` → activity-recency (idle human vs remote-control; widen to 3 sites) (depends hk-7rmv)

Kills the ~3956 captain false-suppressions: replace the raw attached-count check with an
activity-recency signal that distinguishes an idle/remote/monitor client from active typing.

1. **Code change.** Replace `OperatorAttached` (raw `tmux list-clients` attached-count,
   `tmuxresolve.go:101-114`) with an activity-recency signal (e.g. last keystroke / client
   activity within a recency window). Apply it at all **3** suppression sites so the gate is
   consistent.
2. **Deployment step.** `go install ./cmd/harmonik`; relaunch keeper-watched sessions
   (captain especially, under the iOS/remote workflow).
3. **Test step.** RED-THEN-GREEN. Integration (T2): with an idle / detached-feeling client
   attached, assert the keeper does NOT suppress inject (RED on main); with simulated recent
   activity, assert it DOES suppress. Unit (T1): recency predicate truth table.
4. **Validation step.** Live event-trace over a warn→act cycle with a remote-control client
   attached: `harmonik subscribe --types session_keeper_*` shows NO operator-attached
   false-suppress; the captain is no longer parked warn-only.

---

## hk-75mr — P0-A: gauge-independent live-pane recovery (`maybeLivePaneRecover` → gated `ForceRestart`, fail-closed) (depends hk-7rmv + hk-0t5s)

A live pane whose gauge is dead must still be recoverable WITHOUT a live gauge.

1. **Code change.** Implement `maybeLivePaneRecover`: when the pane is alive but the gauge is
   dead beyond a bound and recovery is otherwise blocked, route to a **gated** `ForceRestart`
   that respects the activity-recency operator gate (hk-0t5s) and is **fail-closed** (no
   restart unless all preconditions hold). Do NOT auto-restart on ambiguity.
2. **Deployment step.** `go install ./cmd/harmonik`; relaunch keeper-watched sessions.
3. **Test step.** RED-THEN-GREEN. Integration (T2, uses hk-baf4 `--emit-na`): kill the twin's
   gauge while the pane stays alive; assert `maybeLivePaneRecover` fires the gated restart
   exactly when preconditions hold and NOT when the operator is recently active.
4. **Validation step.** LIVE-SOAK: reproduce dead-gauge-live-pane; confirm recovery occurs
   without depending on the gauge and without a false restart during operator activity.

---

## NEW-forcerestart — wire dormant `ForceRestartFn` behind `--force-restart`, fail-closed (TO BE FILED; depends hk-7rmv + hk-75mr + hk-0t5s)

> **Bead not yet created.** File with:
> `br create --title="KEEPER: wire dormant ForceRestartFn behind --force-restart (fail-closed)" --type=task --priority=1`
> then `br label add <id> codename:keeper-redesign` and
> `br dep add <id> hk-7rmv; br dep add <id> hk-75mr; br dep add <id> hk-0t5s`
> (attach via codename label, not an epic dep — see memory "Epic dep blocks dispatch").

1. **Code change.** Expose the currently-dormant `ForceRestartFn` behind a `--force-restart`
   flag (default OFF). When enabled, it is the executor `maybeLivePaneRecover` (hk-75mr) calls.
   Fail-closed: never restart unless `--force-restart` is set AND all hk-75mr preconditions +
   the hk-0t5s operator gate pass.
2. **Deployment step.** `go install ./cmd/harmonik`; relaunch keeper-watched sessions with
   `--force-restart` where the operator wants gauge-independent recovery armed.
3. **Test step.** RED-THEN-GREEN. Integration (T2): with `--force-restart` OFF, assert NO
   restart even on dead-gauge-live-pane; with it ON and preconditions met, assert restart
   fires. Unit (T1): flag default is OFF; fail-closed predicate truth table.
4. **Validation step.** Live: with the flag armed, a dead-gauge-live-pane recovers; with it
   unset, the keeper degrades to warn-only (no surprise restarts).

---

## hk-nlio — VALIDATION GATE: extend `cycle_twin_e2e_integration_test.go` to operator real-env (RED on main, GREEN after all P0/P1)

The headline acceptance gate. RED on today's `main`; GREEN ONLY after all P0/P1 land.

1. **Code change.** Extend `internal/keeper/cycle_twin_e2e_integration_test.go` to assert the
   full **handoff → confirm-nonce → /clear → SID-flip → /session-resume** cycle completes and
   rebinds `.managed`, under the operator's real-env conditions: high context **>=300k on a 1M
   window** + **gauge-stale-while-pane-alive** + **idle / remote-control client attached**.
   Use hk-baf4's `--emit-na` / `--suppress-statusline-after` to stage the conditions.
2. **Deployment step.** Test-scoped; build with `go build -tags=integration ./internal/keeper/...`.
   No relaunch. (The gate runs in the opus-triple-review `commit_gate` before reviewers.)
3. **Test step.** Run `go test -tags=integration ./internal/keeper/ -run CycleTwinE2E`. Assert
   it is **RED on `main`** today (capture the failure) and **GREEN** only once all P0/P1 beads
   are merged.
4. **Validation step.** Confirm the gate is DETERMINISTIC (no daemon, no 30-min budget, real
   `hksav-twin-` pane). Pair with the manual LIVE-SOAK (T3): a green suite alone is INSUFFICIENT.

---

## hk-bpkv — P3 cleanup: consolidate duplicated threshold CONSTANTS to one source (NO value change; defaults-PIN enforced)

Opportunistic. The band-retune premise is RETRACTED; this is consolidation ONLY.

1. **Code change.** Consolidate the threshold constants duplicated between
   `internal/keeper/watcher.go` (`WarnAbsTokens`/`WarnPctCeil`) and `internal/keeper/cycle.go`
   (`applyDefaults`) to ONE source. VALUES MUST be byte-for-byte identical — Act 300k/0.85,
   Warn 270k/0.70, Force +40k/0.95. NO band change.
2. **Deployment step.** `go install ./cmd/harmonik`; relaunch keeper-watched sessions (no
   behavior change expected).
3. **Test step.** The defaults-PIN test (T1) asserts every constant unchanged — RED if any
   value moves. Unit test that watcher and cycle now resolve from the SAME constant.
4. **Validation step.** Live: the status-line still shows the same warn/act behavior
   (27% warn on 1M window unchanged); `git diff` shows no numeric literal changed.
