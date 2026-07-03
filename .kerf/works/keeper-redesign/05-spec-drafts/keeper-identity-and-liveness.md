# Keeper Identity & Liveness — Normative Spec (DRAFT)

> Status: DRAFT (kerf work `keeper-redesign`, spec jig).
> On `kerf finalize`, this is copied to `specs/keeper-identity-and-liveness.md`.
> NORMATIVE. The spec is always right; code is updated to match it.

This spec governs how the harmonik **keeper** (the per-session context-fill watcher)
establishes the identity of the session it watches, how it keeps the occupancy gauge live,
and how it gates restart on operator activity. It does **not** redesign the inject step
(handoff → /clear → /session-resume mechanics), which is sound.

The keyword **MUST** / **MUST NOT** / **SHOULD** carry their RFC-2119 force.

---

## 1. Invariant: SINGLE-WRITER

There MUST be exactly **ONE** writer of session identity (`.managed`) and exactly **ONE**
writer of the occupancy gauge (`.ctx`).

- **I1.1** `WriteManagedSessionFn` MUST be called **exactly once per keeper process**, at
  boot, to persist the launch-passed session_id into `.managed`. It MUST NOT be called again
  for the life of the keeper.
- **I1.2** There MUST be **no auto-clear** of `.managed`. The watcher MUST NOT erase or
  rewrite `.managed` in response to a gauge mismatch, a "foreign session", a "stale binding",
  a UUID-version mismatch, or any other runtime observation.
- **I1.3** There MUST be **no flap/latch recovery loop**: no rapid-clear counter, no
  auto-clear cooldown, no latch-suppression latch, no suppress-state file, no self-recovery
  by counting "stable SID" ticks.
- **I1.4** The gauge writer is the status-line / hook path; the watcher MUST be a gauge
  *reader* only. The watcher MUST NOT mint, normalize, or rewrite the session_id it reads.

**Verification.** A unit test drives a full warn→act→clear→resume cycle through a
test-double `WriteManagedSessionFn` and asserts the call count is exactly **1**. A grep gate
asserts the deleted call sites (§4) no longer exist.

---

## 2. Invariant: AUTHORITATIVE-IDENTITY

The keeper's session identity MUST be **authoritative**, supplied at launch — never inferred.

- **I2.1** The keeper MUST accept its session identity from `harmonik keeper --session-id
  <uuid4>`. The value MUST be a lowercase **UUIDv4**.
- **I2.2** At boot, the keeper MUST write that launch SID into `.managed` (the single
  `WriteManagedSessionFn` call of §I1.1) and thereafter MUST read identity FROM `.managed`.
- **I2.3** The keeper MUST NOT scrape, heuristically derive, or "self-heal" the session_id
  from: the gauge file (`.ctx`), a transcript filename, a conversation/transcript-dir UUID,
  or any `tmux`/process inspection. All such derivation paths MUST be removed.
- **I2.4** There MUST be no UUID-**version** branching in identity binding (no special-casing
  UUIDv7 daemon-implementer SIDs, no uppercase-vs-lowercase guard). The launch SID is a
  lowercase UUIDv4 by contract; if a non-conforming SID is passed at launch, the keeper MUST
  fail-closed at boot with a clear error rather than carrying a runtime guard.
- **I2.5** The launch SID-mint mechanism is `SID="$(uuidgen)"` in
  `scripts/captain-tools/captain-launch.sh` — the **repo source-of-truth**. The SID MUST be
  threaded from that mint point into both the Claude session launch and the
  `harmonik keeper --session-id $SID` launch. It MUST NEVER be re-derived from any copy of
  the value downstream.
- **I2.6** Crews thread the SID identically: `crewstart.go resolveSessionID` mints/propagates
  the UUIDv4 and threads it into the keeper launch. Hooks resolve the agent name from the
  unified `HARMONIK_AGENT` env var (NOT `HARMONIK_KEEPER_AGENT`).

**Verification.** `harmonik keeper doctor --agent <name>` reports
`BoundSessionID == <launch UUIDv4>` and `.managed` holds that lowercase UUIDv4. A negative
test confirms a transcript-filename UUID is NEVER written to `.managed`.

---

## 3. Invariant: GAUGE-LIVENESS (with EXPLICIT DELETION TARGET)

The gauge MUST have a liveness guarantee so the warn/act triggers cannot die on a live pane;
and the heuristic identity machinery MUST be DELETED, with identity-machinery LOC going DOWN.

### 3.1 Gauge liveness

- **I3.1** When `.ctx` is **stale or absent** but the watched pane is **alive**, the keeper
  MUST derive occupancy from the transcript (`DeriveOccupancyFromTranscript`) and refresh
  `.ctx` — writing `.ctx` **only when stale/absent** (never clobbering a fresh gauge written
  by the status-line path). (bead hk-81wk)
- **I3.2** A live pane whose gauge is dead MUST remain recoverable via a **gated,
  fail-closed** force-restart path (`maybeLivePaneRecover` → gated `ForceRestart`), so
  recovery does not depend on the gauge being alive. (beads hk-75mr, NEW-forcerestart)

### 3.2 EXPLICIT DELETION TARGET — net LOC for identity machinery MUST go DOWN

A change that ADDS a layer without DELETING the heuristic machinery has **NOT** broken the
fix-of-fix pattern and is **NON-CONFORMING**. The following MUST be DELETED. Net
identity-machinery LOC MUST be **negative**.

**Deletion checklist** (anchored to current `main`):

| # | Artifact | Location | What it is |
|---|----------|----------|------------|
| D1 | The session_id binding heuristic block | `internal/keeper/watcher.go:664-888` (~150 LOC) | latch / auto-clear / flap-cooldown / suppress / self-recovery / UUIDv7-guard / uppercase-guard |
| D2 | Stale-UUIDv7 self-heal branch | `watcher.go:682-689` | clears `.managed`, calls `WriteManagedSessionFn(... "")` |
| D3 | Transient-UUIDv7 skip-and-retain branch | `watcher.go:698-707` | counter-reset + retain-last-gauge |
| D4 | Foreign-session auto-clear + flap cooldown | `watcher.go:709-761` | `consecutiveForeignTicks`, auto-clear, rapid-clear flap counter |
| D5 | Latch-suppression + self-recovery branch | `watcher.go:762-825` | `latchSuppressed`, `suppressedStableSID`, `consecutiveStableSuppressedTicks` |
| D6 | UUIDv7-reject + uppercase-reject latch guards | `watcher.go:832-856` | `isUUIDv7` / `isUppercaseUUID` latch rejections |
| D7 | Flap-cooldown-cleared-on-stable-bind branch | `watcher.go:877-888` | resets all flap state on stable bind |
| D8 | `isUppercaseUUID` helper | `watcher.go:470` | uppercase-UUID heuristic |
| D9 | `isUUIDv7` / `IsUUIDv7` helper (identity use) | `keeper.go:148-153` | UUID-version heuristic (remove identity-binding uses) |
| D10 | Suppress-state persistence helpers | `keeper.go` (`WriteSuppressState` / `ClearSuppressState` / `ReadSuppressState`) | flap-suppress marker file I/O |
| D11 | `keeper rebind` CLI command | `cmd/harmonik/keeper_cmd.go:315-391` (incl. `WriteManagedSessionFn` call at :388) | manual-rebind escape hatch — no longer needed once identity is authoritative |

**Named config knobs to DELETE** (the heuristic-machinery knobs; `WatcherConfig` in
`internal/keeper/watcher.go`):

| # | Knob | Purpose (removed) |
|---|------|-------------------|
| K1 | `StaleBindingThreshold` | consecutive-foreign-ticks before auto-clear |
| K2 | `AutoClearCooldown` | rapid-repeat auto-clear window |
| K3 | `AutoClearMaxAttempts` | flap-cooldown trip point |
| K4 | `SuppressRecoverThreshold` | stable-SID ticks for self-recovery |
| K5 | `WriteSuppressFn` | persist latch suppression |
| K6 | `ClearSuppressFn` | clear suppression marker |
| K7 | `ReadSuppressFn` | restore suppression on boot |

(K1–K7 are the ~6+ knobs the bead map references. `WriteManagedSessionFn` and
`ReadManagedSessionFn` are RETAINED — they are the single-writer/reader of §1; their
auto-clear *call sites* D2/D4 are what get deleted.)

**Verification.** `git diff --stat` over `internal/keeper/` + `cmd/harmonik/keeper_cmd.go`
shows net-negative LOC for identity machinery. A grep gate (in the bead spec) asserts each
deleted identifier (`latchSuppressed`, `consecutiveRapidClears`, `lastAutoClearAt`,
`consecutiveForeignTicks`, `suppressedStableSID`, `consecutiveStableSuppressedTicks`,
`StaleBindingThreshold`, `AutoClearCooldown`, `AutoClearMaxAttempts`,
`SuppressRecoverThreshold`, `WriteSuppressFn`, `ClearSuppressFn`, `ReadSuppressFn`,
`isUppercaseUUID`, and the `keeper rebind` command) returns **0 hits** outside test
fixtures.

---

## 4. Invariant: DEFAULTS-PIN (operator HARD-NO)

The threshold constants MUST NOT move.

- **I4.1** **Act** = absolute **300_000** tokens / pct ceil **0.85**.
- **I4.2** **Warn** = absolute **270_000** tokens / pct ceil **0.70**.
- **I4.3** **Force** = Act absolute **+ 40_000** (default **340_000**) / Act pct ceil **+ 0.10**
  (default **0.95**).
- **I4.4** The keeper window-size default and percent-gate semantics MUST NOT change. On a
  **1M** context window the absolute-token gate fires first and `--act-pct` is **INERT**; the
  **27% warn** the operator observes on the status line is therefore **CORRECT-BY-DESIGN**.
- **I4.5** Band-widening the warn/act band to "fix" the 27% display is a **BLOCKING FAILURE**.
  The warn-nag is a SIGNAL, not a bug.
- **I4.6** Current threshold definitions live in `internal/keeper/cycle.go` (`applyDefaults`,
  ~lines 172-203: `ActAbsTokens`, `ActPctCeil`, `WarnAbsTokens`, `WarnPctCeil`,
  `ForceActAbsTokens`, `ForceActPctCeil`) and are duplicated for the warn gate in
  `internal/keeper/watcher.go` (`WarnAbsTokens` / `WarnPctCeil`, ~lines 156/167). The P3
  cleanup (hk-bpkv) MAY consolidate these to ONE source — but **only** if the consolidated
  VALUES are byte-for-byte identical. No value change is permitted under any bead.

**Verification.** A **defaults-PIN test** asserts each constant equals the value above. The
test is RED if any constant moves. It is a standing invariant asserted by EVERY bead in this
work (the consolidation bead included).

---

## 5. Validation tiers (normative)

Every bead MUST be validated at these tiers, **RED-THEN-GREEN** (the first worktree commit is
a test that FAILS on today's `main`):

- **T1 — package-keeper unit tier**: `go test ./internal/keeper/...`. Fast, no tmux, no
  daemon. Hosts the single-writer call-count assertion, the authoritative-bind negative test,
  and the defaults-PIN constant assertions.
- **T2 — `//go:build integration` twin-pane tier**: `go test -tags=integration
  ./internal/keeper/...`. Drives a **real throwaway tmux pane** named `hksav-twin-…`. **NO
  daemon. NO 30-minute scenario budget.** Hosts the gauge-liveness-through-idle test, the
  operator-activity-recency test, and the extended
  `cycle_twin_e2e_integration_test.go` acceptance gate.
- **T3 — manual LIVE-SOAK by an INDEPENDENT verifier** (NOT the implementer): reproduce
  **stale-gauge-on-live-agent**; confirm the gauge writer keeps `.ctx` **fresh through a
  >5-minute idle window** AND **across a `/clear`** (capture `.ctx` occupancy before+after a
  clear and assert equal). **A green suite alone is INSUFFICIENT** for this subsystem — the
  fix-of-fix history is exactly the class of bug a unit/integration suite missed.

A bead is DONE only when T1+T2 are GREEN for its scope AND (for the validation-gate bead
hk-nlio) the operator-real-env acceptance test flips RED→GREEN. The LIVE-SOAK is required
before declaring the whole work square.

---

## 6. State-transition reference (post-refactor)

The watcher tick reduces to a small, branch-free identity path:

1. **boot**: read `--session-id` (lowercase UUIDv4); fail-closed if malformed; write
   `.managed` ONCE.
2. **tick**: read `.managed` (authoritative). Read `.ctx`.
3. if `.ctx` is **fresh** → evaluate warn/act/force gates against the pinned thresholds.
4. if `.ctx` is **stale/absent** AND pane **alive** → `DeriveOccupancyFromTranscript`, write
   `.ctx` (only because stale/absent), evaluate gates.
5. if pane **alive** but recovery still blocked → `maybeLivePaneRecover` → gated, fail-closed
   `ForceRestart` (behind `--force-restart`).
6. **operator gate**: suppress inject ONLY when activity-recency shows the operator typed
   recently — NOT merely because a client is attached.

There is no foreign-session branch, no auto-clear, no latch-suppress, no flap cooldown, no
UUID-version guard. Identity is a constant for the keeper's lifetime.
