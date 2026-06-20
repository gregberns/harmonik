# 08 — Adversarial Harm Review of Wave-B Fixes (W1, W2, W4, W5/W7)

**Stance:** refute / find harm. Default skepticism; may overrule. Verified against
`internal/keeper/cycle.go`, `awaitack.go`, `scripts/hk-keeper.sh`,
`scripts/captain-tools/captain-launch.sh`, `cmd/harmonik/keeper_cmd.go`.

---

## W2 — wire await-ack into the auto cycle, abort instead of fake-complete

**Verdict: SAFE-TO-IMPLEMENT — but with a SPECIFIED failure behavior. The synthesis
word "abort" is RIGHT only because the existing abort machinery already retries; a
naive abort that re-uses the *handoff-timeout* abort path is the correct move.**

My initial worry was that aborting strands the captain near-overflow with no reset.
**That worry is REFUTED by the code.** The handoff-timeout abort path (`cycle.go:815-863`)
does NOT dead-end the session:
- it sets `lastFireWasAbort=true`, `lastFiredSID=cf.SessionID`, `seenLowPctAfterLastFire=false`
  (`821-825`) and conditionally clears `.managed` (`837-842`);
- Gate-6's **forced-clear exception** (`cycle.go:696-706`) then RE-FIRES the cycle on the
  *same* session every `ForceRetryInterval` (120s, `cycle.go:220-221`) **as long as the agent
  is `aboveForceThreshold`** — which an overflowing captain is, by definition.

So an ACK-timeout abort that funnels into this same path means: "couldn't confirm the REPL is
live → don't `/clear` blind → retry in 120s." The session is NOT stranded; it is *re-attempted
on a bounded interval* until either the ACK lands or the escalation counter
(`MaxHandoffTimeouts`, `848-859`) trips `ForceRestartFn`. **This is strictly better than
fire-and-forget**, which marks `complete` and latches anti-loop suppression on an un-cleared
session (`cycle.go:919-931`) — the silent-overflow hole.

**Correct failure behavior (NORMATIVE for the implementer):**
1. Inject `AckLine(cycleID,"cycle")` *before* `/clear` (mirror `restartnow.go:130`), `AwaitAck`
   it via an injected `PaneCapturer` seam.
2. On `ErrAckTimeout`: **route into the EXISTING handoff-timeout abort branch** (set
   `lastFireWasAbort=true`, the suppression fields, `.managed`-clear, escalation counter) — do
   NOT invent a second abort path, and do NOT mark `complete`. This gives free 120s force-retry
   + `MaxHandoffTimeouts` escalation. Emit `session_keeper_ack_timeout` (already wired in
   `AwaitAck`).
3. On `ctx.Err()` (operator interrupt) — return without abort bookkeeping, same as `AwaitAck`
   already does (`awaitack.go:165-170`).

**Two GUARDS the adversary insists on:**
- **G1 (false-negative ACK / capture-pane timing):** the worry that the paste DID land but the
  read-back missed it. `AwaitAck` already mitigates: it captures **200 lines of scrollback**
  (`awaitack.go:49,215`) and a `captureErrorBudget` of 5 (`awaitack.go:56,139`). The ACK is
  injected by the keeper *itself* immediately before `/clear`, so the token is on-screen within
  one poll. Residual false-negative risk is low. BUT the abort is the SAFE direction on a
  false-negative: you retry, you do NOT `/clear`. The dangerous direction (false-POSITIVE: ACK
  matched but pane dead) is structurally impossible — the agent must be live to echo the token.
  So G1 is satisfied: the failure asymmetry favors abort.
- **G2 (double-fire next tick):** confirmed NOT a risk in the abort case — abort sets
  `lastFireWasAbort=true`, which DISABLES the same-SID below-warn re-arm hatch (`cycle.go:603-604`).
  Re-fire is governed solely by the rate-limited (120s) Gate-6 force path. No tight loop. (This
  is exactly the Bug-3a protection, already landed.) The implementer MUST funnel through that
  path and NOT add an independent re-arm, or G2 reopens.

**Overrule note:** do NOT also gate `/session-resume` behind a second blocking `AwaitAck`
(report 01 §2.3 "optional"). After `/clear` the REPL identity is mid-flux; a post-resume ACK
adds a second 15s blocking wait on the hottest path for marginal value and a new false-negative
surface. Gate `/clear` only. Resume confirmation stays best-effort (`waitForNewSessionID`).

---

## W1 — per-agent keeper `while-true` self-heal shim

**Verdict: NEEDS-GUARD (anti-double-launch + respawn-cmd dedupe).**

Real double-launch risk, two vectors:
1. **`hk-keeper.sh`'s own guard does NOT transfer.** `hk-keeper.sh` keys liveness on
   `pgrep -f "harmonik --project $PROJ"` (`hk-keeper.sh:69`) — a DAEMON-process check, not a
   keeper-session check. The per-agent shim must guard on the *keeper* instead:
   `tmux has-session -t hk-keeper-<name> 2>/dev/null && continue`, OR
   `pgrep -f "harmonik keeper --agent <name>"`. The synthesis text ("mirror hk-keeper.sh")
   is misleading — the shim must replicate the *shape* (while/sleep) but with a DIFFERENT
   liveness predicate. Spell that out or the shim relaunches a live keeper.
2. **Shim ↔ original-launcher collision.** `captain-launch.sh:116` already spawns the keeper
   once. If the shim ALSO spawns it, two keepers race the same pane → double `/clear`-paste
   (the dangerous case). FIX: `captain-launch.sh` must spawn the *shim* in place of the raw
   `harmonik keeper` (report 06 §3's "spawn THAT instead"), NOT in addition. The shim's first
   loop iteration starts the keeper; the launcher no longer starts it directly.
3. **respawn-cmd double-driver.** The captain keeper carries `--respawn-cmd captain-respawn.sh`.
   If a crashed keeper is relaunched by the shim while a *stale* keeper process is still wedged
   (e.g. `^Z`-suspended, not dead — the C10 case in report 07), the `has-session` check passes
   (session exists) so the shim correctly does NOT relaunch. GOOD — but a SUSPENDED keeper means
   the shim's session-existence check sees it alive and never heals it. **This shim does NOT fix
   the `^Z`-suspend class** (report 07 C10); it only fixes the exit/crash class. State that
   limitation; pair with W3's `.keeperalive` heartbeat (a suspended keeper stops writing it) so
   the suspend case is at least ALARMED even if not self-healed.
4. **Operator-intentional-kill resurrection.** A `while-true` shim WILL resurrect a keeper the
   operator deliberately `tmux kill-session`'d (the operator must kill the *shim* window, not the
   inner keeper). Acceptable for captain/crew (you rarely want a watched agent unwatched), but
   the teardown path (crew stop, captain stop) MUST kill the shim window, not just the keeper —
   else stop leaves an immortal relauncher. This is the same orphan-watcher fork risk report 03
   flags for crews.

Not over-engineering — it closes the exact gap that forced today's manual relaunch. Ship with
guards 1+2 mandatory.

---

## W4 — crew keeper watchers, warn-only, NO `--respawn-cmd`

**Verdict: SAFE-TO-IMPLEMENT, warn-only first — AGREE with the report. One clarification.**

- **Warn-only is genuinely safer, and the reason is concrete:** the auto ACT cycle is still
  open-loop (W2 unfixed at W4-time per the wave-B sequence). Arming 5 crews with the open-loop
  `/clear`→`/session-resume` cycle multiplies the fake-complete-on-swallowed-paste exposure 5×
  (report 03 §"caveat 3"). Warn-only crews get the advisory warn inject + can `restart-now`
  themselves; no autonomous destructive `/clear`. Correct sequencing: **W4 warn-only ships
  BEFORE W2; arm crew ACT cycles only AFTER W2 lands.**
- **"Does even warn-only inject text into crew panes?" — YES, it does.** Warn-only still injects
  the advisory warn line into the pane (that is the warn action). So warn-only is NOT
  zero-injection; it is *non-destructive* injection (no `/clear`). That is acceptable — a warn
  line cannot wedge a session — but the report should not imply warn-only is inert. The risk it
  removes is the DESTRUCTIVE `/clear`, not all pane writes.
- **Mandatory guard (already in report 03): `HandleCrewStop` teardown.** A crew keeper that
  outlives its crew + has any respawn-cmd is the fork-bomb signature
  (`reference_keeper_smoke_forkbomb`, 1500+ sessions). Warn-only + no-respawn-cmd defuses the
  fork bomb; teardown defuses the orphan-watcher. Ship BOTH. Idempotency guard (skip if
  `hk-keeper-<crew>` already exists) is required for the same double-launch reason as W1.

---

## W5 / W7 — CLI hardening

**Verdict: W5 abs-normalize SAFE; W5 "require --project" DON'T; W7 SAFE.**

- **W5 `filepath.Abs` for marker verbs (SAFE):** purely makes relative `--project` resolve like
  the watcher's `os.Getwd()`-absolute path. Verified `parseKeeperMarkerArgs` (`keeper_cmd.go:349-357`)
  passes the raw flag string through with NO `Abs`, while enable/doctor DO normalize. Closing that
  asymmetry is a strict bugfix; cannot break the live fleet (an already-absolute `--project` is a
  no-op under `Abs`).
- **W5 / report 04 F1(b) "require --project / refuse to derive from CWD" — DON'T (would break the
  LIVE fleet).** The default `project = os.Getwd()` (`keeper_cmd.go:350-356`) is load-bearing:
  the captain launch and the interactive operator both invoke marker verbs WITHOUT `--project`
  from the repo root. The captain keeper, `set-dispatching`/`clear-dispatching` in the dispatch
  loop, and `restart-now` all rely on the Getwd default. Making `--project` mandatory, or
  hard-failing when CWD lacks `.harmonik/`, is a behavior change that breaks every existing script
  and the captain's own dispatch-guard calls. KEEP the Getwd default; only add the `Abs`
  normalization (safe) and the non-fatal WARN when no live watcher exists for the resolved dir
  (report 04 F5 — advisory only, exit stays 0, breaks nothing).
- **W7 defaults→0, effective-band banner, tighten-only clamp, `default:` verb case (SAFE):**
  these are help-text/banner/error-message changes plus an additive `default:` case for unknown
  subcommands. None changes the firing band for an agent that passes `--warn-abs-tokens/--act-abs-tokens`
  (the captain/crew launchers all pass abs, `captain-launch.sh:116-117`), so the live fleet's
  effective thresholds are unchanged. The `default:` case only catches tokens that currently
  fall through to a confusing error — strictly better. No live-script breakage.

---

## Summary table

| Fix | Verdict | Load-bearing guard |
|---|---|---|
| **W2** | SAFE-TO-IMPLEMENT | Route ACK-timeout into the EXISTING handoff-timeout abort path (free 120s force-retry + escalation); gate `/clear` only, NOT resume; rely on `lastFireWasAbort` to block double-fire |
| **W1** | NEEDS-GUARD | Guard liveness on `hk-keeper-<name>` session (NOT the daemon pgrep); launcher spawns the SHIM not keeper+shim; teardown kills the shim window; does NOT fix `^Z`-suspend (pair with W3) |
| **W4** | SAFE-TO-IMPLEMENT | warn-only + NO respawn-cmd + `HandleCrewStop` teardown + idempotency; arm ACT cycle only AFTER W2; note warn-only STILL injects (non-destructive) text |
| **W5** | SAFE (Abs) / **DON'T (require --project)** | Add `filepath.Abs` + non-fatal warn ONLY; keep `os.Getwd()` default — mandatory `--project` breaks captain/dispatch scripts |
| **W7** | SAFE-TO-IMPLEMENT | Banner/help/`default:`-case only; abs-band agents unaffected |
