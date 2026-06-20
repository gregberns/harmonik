# Fresh-Context Review — Bug 3 fix (hk-vpnp), commit `e2109a2f`

**Reviewer:** independent fresh-context agent · **Date:** 2026-06-20
**Branch:** `worktree-agent-a9acc91670372a2f7` · **Target:** `main` (HEAD `95135107`)
**Scope:** automatic ACT cycle loop + handoff truncation (`internal/keeper/cycle.go`
+ new `internal/keeper/act_loop_hkvpnp_test.go`). `89852bb3`/`4128d760` already
fixed the MANUAL `restart-now` path; this targets `MaybeRun`/`runCycle`.

## VERDICT: APPROVE

---

## 1. Reproduce-confirmation (tests are non-vacuous)

In the worktree, `go test ./internal/keeper/ -run ActLoop -v`:

- **Post-fix (HEAD `e2109a2f`):** BOTH PASS.
- **Pre-fix (reverted ONLY the cycle.go hunks via `git show e2109a2f -- cycle.go | git apply -R`):**
  BOTH FAIL with the exact bug signatures:
  - `3b: handoff truncated to 0 lines on aborted cycle; prior content lost`
  - `3a: cycle re-fired 3 /session-handoff nonces on an un-cleared session (loop); want 1`
- Restored cleanly (`git checkout -- cycle.go`; worktree clean at `e2109a2f`).

The tests genuinely gate the two defects. They use the existing injectable seams
(`InjectFn` spy + real on-disk `ReadHandoff`/`TruncateHandoffFn` for 3b;
oscillating gauge + distinct-nonce `CycleIDGen` for 3a). `TmuxTarget:"fake-pane"`
means NO real tmux is touched — pure in-memory.

## 2. Build / vet / suites

- `go build ./...` — OK
- `go vet ./internal/keeper/...` — OK
- `go test ./internal/keeper/... -count=1` — green ×3 (single runs; 2.4–2.7s each)
- `go test ./cmd/harmonik/ -run Keeper -count=1` — PASS
- `go test ./cmd/harmonik/ -count=1` — PASS (23.6s)

## 3. Adversarial edge-case analysis

### 3(a) — could `handoffHasStaleNonce` ever FAIL to truncate when it should (leak a stale nonce into a real cycle)?

**No.** The DEFECT-2 hazard is a *prior* cycle's nonce pre-satisfying the step-3
poll. `handoffHasStaleNonce` returns `false` (→ skip truncate) only when:
(a) file absent/unreadable — then the poll reads the same absent file, nothing to
pre-satisfy; or (b) the content contains NO `<!-- KEEPER:` prefix at all — by
definition no stale nonce. The defensive `isOnlyNonce` branch returns true only
if every marker equals the current nonce.

Verified the uniqueness premise that the whole design rests on:
`newCycleIDGen` (cycle.go:347) = `cyc-<process-start-timestamp>-<atomic seq>`.
Within a process every nonce is unique; across restarts the timestamp prefix
differs. So the CURRENT nonce can never already be on disk — only a prior one can.
Therefore any leftover keeper marker that isn't the current nonce is correctly
treated as stale and truncated. `isOnlyNonce` parses each marker up to `-->` and
compares against the full `nonceMarker(cycleID)` string (`<!-- KEEPER:cyc-... -->`),
an exact match — sound. **No path leaks a real stale nonce.** Net effect vs. the
old unconditional truncate is strictly narrower destruction with identical poll
safety.

### 3(b) — could `lastFireWasAbort` wedge the keeper into never re-firing a legitimately-needed cycle (deadlock the ACT path)?

**No deadlock for new sessions, and the residual same-SID tradeoff is strictly
safer than the bug it replaces.**

State after an abort: `lastFiredSID=SID_old`, `lastFireWasAbort=true`.

- **Same un-cleared SID (the loop case):** the same-SID escape hatch
  (cycle.go:603, and the `RunForPrecompact` mirror at :1084) is now gated by
  `!lastFireWasAbort`, so a below-warn gauge dip no longer re-arms. Re-fire on the
  same SID is governed by Gate-6 force-retry (rate-limited, above-force-threshold
  only). This is the intended fix — a below-warn reading on a session that was
  NEVER /cleared is gauge noise, not proof of a clear.
- **New session legitimately appears:** the DIFFERENT-SID arming
  (`seenLowPctAfterLastFire = true`, cycle.go:587) is **NOT** gated by
  `lastFireWasAbort`, so a new SID still arms normally and Gate-6's different-SID
  branch lets the next cycle fire. A completed cycle then resets
  `lastFireWasAbort=false` (cycle.go:931). No stale-true leak across sessions.
- **Escalation still works:** the abort path still increments
  `consecutiveHandoffTimeouts` and calls `ForceRestartFn` after
  `MaxHandoffTimeouts` (cycle.go:768-782) — a permanently-frozen pane is still
  hard-restarted; the fix does not touch that.

**Residual (acceptable):** on the SAME un-cleared SID that genuinely drops below
warn after an abort (e.g. the operator manually `/clear`s) AND stays below the
hard force threshold, the cycle won't re-fire until a new SID is observed. This is
a deliberate, documented narrowing. It cannot strand context-overflow because
above the force threshold Gate-6 force-retry still fires; and a real manual
`/clear` mints a new SID anyway, which re-arms via the different-SID path. The
old behavior in this exact slot was the destructive loop, so this is a net safety
gain, not a regression.

## 4. "Pre-existing flake" finding

The implementer reported one transient first-run FAIL in the watcher/respawn
timing tests, attributed to CPU contention, not recurring. **I could not
reproduce any failure** across 3 single keeper runs + the full `cmd/harmonik`
run. The new code is pure synchronous in-memory logic (one bool field + two string
helpers); it adds no goroutines, timers, or tmux interaction. There is no
plausible mechanism by which it could introduce a watcher/respawn timing flake.
Host load was elevated (load avg 4.7 at session start; a live fleet with crews +
flywheel running), consistent with the contention explanation. **Not introduced by
this change.**

## 5. tmux leak check

`tmux ls | wc -l` went 9 → 13 over the review, but ALL new sessions are
`harmonik-*-flywheel` created 08:56–08:57 by the LIVE daemon during normal fleet
operation — not test artifacts (keeper tests never spawn tmux). No `*-keeper`,
`*-test`, or `*-smoke` sessions appeared. **No leak from this change.**

## 6. Merge cleanliness

`git merge-tree` against current `main` shows no conflict markers; the six touched
hunks in cycle.go apply cleanly. The fix builds atop the `89852bb3` worktree but
the touched regions are independent of main's recent commits. Safe to merge.

## 7. Quality notes (non-blocking)

- Comments are thorough and reference the precise bead/defect lineage. No new
  abstraction layers, no signature changes — in-spirit with the surgical-fix
  directive from the recovery README §6.4.
- `act_loop_hkvpnp_test.go:198` ends with `var _ = core.EventTypeSessionKeeperCycleAborted`
  to silence an otherwise-unused `core` import. Minor; the import could simply be
  dropped, but it is harmless and self-documenting. Not worth a change request.

**APPROVE — merge.**
