# XT (Exploratory Adversarial Break-Test) — baseline pin 599f80ab

Delta under test: a0591ba3..599f80ab (keeper T7/T8, gauge-drop re-inject gating,
in-cycle TOCTOU re-check, restart-now nonce; NEW codexdriver resident-session;
daemon workloop dispatch; claude-launch CLAUDE_CONFIG_DIR isolation; sessioncapture
scrub auth-family concat).

Method: adversarial probes from angles the epic's own tests did NOT cover. New
`_test.go` files written into a throwaway worktree (`/tmp/h-assessor/xt-599f80ab`,
since removed) and run under `-race`; keeper/codexwire angles analyzed by cited
source reading. All file:line cites are against the 599f80ab tree.

**xt_verdict: XT-CONCERNS** — no P0 (no crash / wedge / delta-introduced secret
leak). Two P2 findings (one delta-attributable keeper gating hole; one PRE-EXISTING
scrub value-concat leak) + one P3 residual TOCTOU gap. The NEW codexdriver
concurrency (highest bug risk) survived every attack cleanly.

---

## Angle 1 — codexdriver bounded FIFO input queue (inputqueue.go)  → SURVIVED

Adversarial input: drainer pinned (one turn held in flight), 200 concurrent
producers flood a cap=4 queue; then release and audit for loss/dup; separately 50
trials of 64 producers racing `Close()` (send-on-closed panic hunt); resident
Enqueue racing Close.

Observed (`go test -run XT -race`, new file
`internal/codexdriver/inputqueue_xt_adversarial_test.go`):
```
flood: accepted=4 full=196 (cap=200)   → accepted == cap EXACTLY; 196 got ErrQueueFull
--- PASS: TestXT_FloodFarPastCapacity
--- PASS: TestXT_ProducersRacingClose
--- PASS: TestXT_ResidentEnqueueRacingClose
```
Bounded (never grew past cap), every accepted item resolved exactly once (no
loss/dup/wedge), no send-on-closed panic. The RLock-across-send (inputqueue.go:95)
vs WLock-before-`close(items)` (inputqueue.go:131-138) design holds under -race.
**PASS.**

## Angle 2 — resident-session watchdog + owner/queue (resident.go)  → SURVIVED

Adversarial input: a ResidentSession whose spawn ALWAYS fails (empty
`SubstrateSpawn`), Supervise started; probe for (a) hot-loop (no backoff sleep),
(b) goroutine leak after Close, (c) Close wedge; plus Supervise-after-Close and
idempotent Supervise.

Observed (new file `internal/codexdriver/watchdog_xt_adversarial_test.go`):
```
--- PASS: TestXT_WatchdogFailingSpawnNoLeakNoHotLoop (0.40s)
--- PASS: TestXT_SuperviseAfterCloseNoOp
```
`superviseLoop` backs off on repeated spawn failure (backoffSleep, resident.go:203)
and `Close`→`close(closeCh)` wakes it promptly (Close returned <3s); no goroutine
survived Close; Supervise after Close no-ops (closed guard, resident.go:148).
**PASS.**

## Angle 3 — codexwire thread/resume handshake  → SURVIVED

Adversarial input: malformed / wrong-JSON-type (string-vs-number, H11 class) /
null / missing / out-of-order / duplicate resume payloads.

Observed (source analysis, `internal/codexdriver/session.go:942-965`,
`internal/codexwire/codexwire.go`): thread id decodes into a typed `string` field;
a wrong JSON type yields `*json.UnmarshalTypeError` → caught at session.go:959 →
graceful `EventTypeError` "missing thread id". Missing/empty/null all hit the
`ID == ""` guard → typed error event. Error-bearing response handled first
(session.go:950-952). Correlation is by JSON-RPC id, not arrival order → out-of-order
safe; duplicate deletes the pending entry then drops the second (session.go:916-921).
No bare type assertions on decoded interface{} (only two comma-ok guarded). One
non-crash nuance: a non-conforming app-server echoing the *envelope* id as a JSON
string would drop-as-uncorrelated → timeout-bounded handshake stall (caller ctx
timeout), NOT a panic/wedge. **PASS.**

## Angle 4 — keeper gauge-drop defensive re-inject gating (hk-u7j83)  → FINDING (P2)

The KEY adversarial check. Suppression predicate (verbatim, `step.go:431`):
```go
gaugeDropped := ev.CF != nil && cfg.belowActThreshold(ev.CF)
if !gaugeDropped { /* fire defensive re-inject */ }
```
`ev.CF` is the gauge sample read fresh at settle-expiry (`shell.go:399`
`ReadGauge()`), `belowActThreshold` = tokens/pct below act (`cycle.go:454`). No
captured-earlier TOCTOU — the read is file-fresh.

**Hole:** the gate uses the raw gauge value as its ONLY proxy for "the /clear
landed", with no corroborating signal (no session_id delta, no pane busy/idle
check). The only positive confirmation a /clear landed is `EvSessionChanged` with a
NEW session_id (`step.go:379`, `shell.go:406`). These are decoupled. In the genuine
busy-pane case hk-vdqe2 defends (the /clear keystroke was dropped by a busy pane),
the gauge can still drop below act with the SAME session_id via:
- **Auto-compaction racing the un-landed clear (strongest):** Claude Code
  auto-compacts in place at high context, tokens fall below act, session_id
  unchanged. Settle expiry reads below-act → `gaugeDropped=true` → the NEEDED
  defensive re-inject is suppressed. No new SID ever surfaces (compaction ≠ new
  session) → the cycle exhausts ClearConfirmRetries → `stepClearUnconfirmed`
  (step.go:963) briefs into the STILL-BUSY, un-cleared pane — the exact regression
  hk-vdqe2 exists to prevent.
- **Statusline-cadence stale-low gauge:** the `.ctx` file only rewrites on
  statusline render; a busy pane mid-generation may hold a stale below-act value →
  file-fresh but value-stale suppression on an un-landed clear.

Sharp asymmetry: in the LEGITIMATE self-restart case the new SID eventually confirms
regardless, so suppression only avoids a brief pile-on; in the compaction/stale case
the new SID NEVER surfaces, so suppression PERMANENTLY loses the re-inject.

Severity **P2** — no crash; degrades to pre-hk-vdqe2 behavior (brief into un-cleared
pane); trigger is narrow (compaction/stale racing an un-landed clear) but is
delta-attributable (this commit's new gating narrows the defensive re-inject's
coverage). Analytically derived (not reproduced by a written test — keeper is a
complex state machine); cited lines verified present. Recommend the epic add a
corroborating signal (same-SID ⇒ don't treat a below-act drop as clear-landed) or a
compaction guard.

## Angle 5 — keeper T8 in-cycle operator-attached TOCTOU re-check (6zbg1)  → FINDING (P3)

The T8 re-sample (`shell.go:323`, `pollAwaitingHandoff`) correctly closes the large
AwaitingHandoff window (≤300s): while attached it holds the nonce-confirm and skips
the handoff-timeout freshness recovery, so neither edge reaches /clear.

**Residual gap:** the re-check lives ONLY in `pollAwaitingHandoff`. Once the nonce
is confirmed (only possible while DETACHED), the cycle enters AwaitModelDone. On
`EvModelDone`, `stepAwaitModelDone` → `stepEnterClearing` injects the /clear
(`step.go:358-361`) with NO live operator re-check. An operator who RE-attaches and
starts typing during the AwaitModelDone wait can be clobbered: `pollAwaitModelDone`
fires `EvModelDone` off a stale `.idle` marker mtime ≥ nonce (`shell.go:363`)
regardless of the operator's new in-flight turn → /clear. Same destructive harm
class as T8, in a smaller (model-done-bounded) window. The commit message's "gates
BOTH edges that reach /clear" is true only within AwaitingHandoff. Severity **P3**
(narrow window; requires re-attach+type in the AwaitModelDone gap).

## Angle 6 — claude-launch CLAUDE_CONFIG_DIR isolation (claudeconfigdir_hk8juwz.go)  → SURVIVED

Adversarial check: does the isolated config leak into the real ~/.claude.json? All
`os.WriteFile` targets are `destCfg` = `<workspace>/.harmonik/claude-config/.claude.json`
(seed copy at scrub.go:161/182; trust upsert `ensureWorktreeTrustAt(trustKeyPath,
destCfg)` writes the EXPLICIT isolated path, claudetrust_wm040b.go:161). The
operator's real ~/.claude.json is READ-ONLY (seed source, os.ReadFile). Shared
global trust writer untouched. Prepare-failure propagates without exec'ing claude
(documented fatal posture). Minor note (not a defect): the unreadable-source
fallback writes a minimal best-effort config rather than fail-closed — a documented
RISK (firstStartTime alone may not dismiss the modal), but it never leaks and never
execs un-isolated. **PASS.**

## Angle 7 — sessioncapture scrub value-pattern concat (hk-a5f1k area)  → FINDING (P2, PRE-EXISTING — NOT a delta regression)

The DELTA's actual change (hk-a5f1k, `keyIsSensitive` auth-family compact-substring
matching) is SOUND: a sensitive-keyed value redacts its WHOLE body regardless of
value-pattern gaps (test `TestScrubLine_XT_SensitiveKeyCoversWholeValue` PASS; the
existing hka5f1k key tests pass).

BUT the adjacent value-pattern layer (`valuePatterns` sk/pk/ghp/AKIA regexes +
`ReplaceAll`, scrub.go:22-32,130-132) leaks the tail of a SECOND secret when two
secrets are concatenated with NO delimiter in a NON-sensitive-keyed value or bare
output text. Reproduced (new file
`internal/sessioncapture/scrub_xt_adversarial_test.go`):
```
{"blob":"ghp_AAAAAAAAAAAAAAAAAAAAsk-BBBBBBBBBBBBBBBBBB"}
  → {"blob":"<redacted>-BBBBBBBBBBBBBBBBBB"}     ← sk key body BBBB… LEAKS
log line: sk-AAAA…sk-BBBB… end
  → log line: <redacted>-BBBBBBBBBBBBBBBB end     ← second sk body LEAKS
```
Root cause: the greedy `[A-Za-z0-9]+` in the ghp/sk pattern swallows the second
key's `sk` prefix (alnum) up to the `-`, so after replacement `-BBBB…` no longer
matches the `sk-` anchor. `ReplaceAll` continues after the match, leaving the tail
exposed. The value-pattern layer is precisely the defense for raw NDJSON output
where field structure isn't parsed (scrub.go:16-21), so the bare/non-sensitive-key
case is in-scope for it.

Severity **P2** — it is a real secret-tail leak, but (a) PRE-EXISTING: the sk/pk
value pattern + ReplaceAll are present at the pre-delta base a0591ba3 (verified via
`git show a0591ba3:internal/sessioncapture/scrub.go`), so it is NOT introduced by
this branch's delta; and (b) requires the unusual no-delimiter concatenation of two
secrets. Flagged as an adjacent hardening item, not a gate on this delta.

---

## Findings roll-up

| # | Angle | Verdict | Severity | Delta-attributable? |
|---|-------|---------|----------|---------------------|
| 1 | bounded FIFO inputqueue | SURVIVED | — | — |
| 2 | resident watchdog + owner/queue | SURVIVED | — | — |
| 3 | codexwire resume handshake | SURVIVED | — | — |
| 4 | keeper gauge-drop re-inject (hk-u7j83) | **FINDING** | P2 | YES |
| 5 | keeper T8 TOCTOU re-check (6zbg1) | **FINDING** | P3 | YES (residual gap) |
| 6 | CLAUDE_CONFIG_DIR isolation | SURVIVED | — | — |
| 7 | scrub value-concat leak | **FINDING** | P2 | NO (pre-existing) |

No P0 (no panic / wedge / hang / delta-introduced secret leak). The new codexdriver
concurrency — the highest-risk surface — survived flood, close-races, watchdog
failure-spin, and malformed-handshake attacks cleanly under -race.
