# 15 — Diagnosis: gauge-stale / fresh-but-wrong gauge (C2 false-negative)

**Scope:** READ-ONLY diagnosis. No code edited. Verified against live code + runtime
state on 2026-06-20. Anchors: `internal/keeper/gauge.go`, `internal/keeper/watcher.go`,
`internal/keeper/heartbeat.go`, `internal/keeper/sessionid.go`,
`scripts/keeper-statusline.sh`, `.harmonik/events/events.jsonl`, `.harmonik/keeper/`.

---

## TL;DR

There are **two distinct defects** under the "gauge-stale" umbrella, and the round-2 reports
conflate them:

1. **`no_gauge:stale` churn (benign-ish, mostly fixed):** the gauge ages past Staleness on a
   live pane. The keeper-side heartbeat (`maybeHeartbeat`, hk-81wk) was built to fix exactly
   this and largely does. The 165 "recent window" events the reports cite are **historical /
   pre-heartbeat tail**; the *live* dominant reason is now `foreign_session`, not `stale`
   (235 vs 145 in the last 400; 541 foreign_session events are ALL captain).

2. **The real top-priority bug — a genuine FALSE-NEGATIVE: a fresh-by-mtime `.ctx` that
   carries a STALE/WRONG token count, which passes the freshness gate so an oversize agent is
   never flagged.** This is REAL, and the *heartbeat itself can manufacture it*. This is the
   item worth a P1 bead.

---

## 1. How the freshness check works (and what "stale" means)

`watcher.go` tick, in order:

1. `ReadCtxFile` (gauge.go:34) reads `.ctx`, returns `(*CtxFile, modTime, err)`. `modTime` is
   the **file mtime**, NOT the `ts` field inside the JSON.
2. `maybeHeartbeat(ctx, ctxFile, time.Since(modTime))` (watcher.go:648) — runs FIRST, can
   rewrite `.ctx` with a fresh mtime.
3. **Stale gate (watcher.go:651):** `if time.Since(modTime) >= w.cfg.Staleness { …; continue }`.
   Staleness default 120s. "Stale" = **file mtime older than 120s**. Pure mtime; the gate
   never inspects token count, `ts`, or session_id.
4. **Foreign-session gate (watcher.go:681-712):** only reached when fresh. Compares
   `ctxFile.SessionID` (already overlaid with `.sid` by `ReadCtxFile`, gauge.go:59) against
   `.managed`. Mismatch → adopt (if `.sid==gauge.sid`) or reject as `foreign_session` +
   `continue`.
5. Only AFTER both gates pass do the warn/act/cycle evaluations run on `ctxFile.Tokens`.

So **freshness is mtime-only**. Token-count and session-id correctness are checked *after*,
but the warn/act gate then **trusts `ctxFile.Tokens` unconditionally**.

---

## 2. The false-negative — fresh mtime + wrong token count

Two independent paths produce a fresh-by-mtime gauge whose **token count is stale/wrong**, and
the warn/act gate (`belowWarnThreshold` → `minAbsOrPctCeil(abs, pctCeil, window)` on
`ctxFile.Tokens`) trusts it, so an oversize agent never crosses the gate. This is exactly the
round-1 "captain at 313k while gauge showed 20-27%" symptom.

### Path A — the heartbeat carries a STALE token reading forward (the worst one)

`maybeHeartbeat` (heartbeat.go:178) fires on a live pane and writes a fresh `.ctx`:

```go
fresh := CtxFile{ Pct: last.Pct, Tokens: last.Tokens, ... Ts: now }   // carries last-good
if tokens, ok := deriveContextTokens(transcriptDir, sid); ok {
    fresh.Tokens = tokens; ... recompute pct ...
}
WriteCtxFile(... &fresh ...)   // fresh MTIME, possibly STALE tokens
```

`deriveContextTokens` returns `(0,false)` when the transcript file is **absent or unreadable**
— e.g. `sid` resolves to the wrong UUID (see Path B), the transcript dir munge is wrong, or
the file simply hasn't been flushed. On `false`, the heartbeat **carries `last.Tokens`
forward verbatim** but stamps a **fresh mtime**. So:

- statusline stops repainting (busy pane) at, say, the last good reading 180k tokens;
- pane keeps growing to 230k+ (genuinely oversize, should ACT at 215k);
- heartbeat fires every tick, transcript-derive MISSES, re-stamps `.ctx` = `{tokens:180000,
  ts:now}` with a **fresh mtime**;
- stale gate passes (fresh mtime), foreign gate passes, warn/act gate reads 180k < 215k →
  **never fires.** The agent overflows the pane while the gauge looks healthy and fresh.

The heartbeat was designed to suppress `no_gauge:stale` — but in doing so it **defeats the one
safety the stale-continue provided**: when the gauge was stale, the watcher at least did NOT
falsely conclude "this agent is fine." Now it keeps a stale *value* alive under a fresh mtime,
converting a loud `no_gauge:stale` into a silent false-negative. **This is the C2 the
adversary flagged, and the heartbeat is complicit.**

### Path B — wrong session_id feeds a wrong/empty transcript derive

`heartbeatSessionID` (heartbeat.go:160) prefers `.managed`. If `.managed` is bound to an OLD
session after a `/clear` (the adopt path at watcher.go:693 only runs when the gauge has
ALREADY repainted with the new `.sid`; until the first post-clear repaint, `.managed` is
stale), the heartbeat derives tokens from the **old session's transcript** — which is frozen
at its last value or returns `false`. Same outcome as Path A: fresh mtime, wrong tokens.

Note the foreign-session gate runs at watcher.go:681 — but the heartbeat at :648 runs
**BEFORE** it and has already written the wrong-sid gauge. The foreign gate then either rejects
(churn) or adopts, but the damage (a fresh-mtime file with a wrong token count) is done a tick
earlier and is what the warn/act gate would read once adopted.

### Why the warn/act gate is the structural hole

`belowWarnThreshold`/`belowActThreshold` (via `minAbsOrPctCeil` in thresholds.go:80) consume
`ctxFile.Tokens` with **no freshness or provenance check on the value itself**. The freshness
gate guards the *file mtime*; nothing guards the *staleness of the number inside it*. The
heartbeat breaks the implicit invariant "fresh mtime ⇒ fresh number."

---

## 3. The 165 `no_gauge:stale` — benign churn or blind-spot?

Full-history breakdown (`.harmonik/events/events.jsonl`):

| agent | reason | count |
|---|---|---|
| captain | stale | 2287 |
| captain | foreign_session | 541 |
| gurney/paul/liet | stale | ~550 combined |
| keeper-dogfood | stale | 66 |

In the **recent** window the live dominant reason flipped to **`foreign_session`** (235 of
last 400; 48 of last 50), all captain. That is a *second* live defect: the captain repeatedly
sees `gauge.SessionID != .managed` and neither adopts nor latches cleanly — a 2-minute-cadence
re-emit loop (one per tick) from 06-16 through 06-18. It is mostly **noise** (the captain pane
is fine; the live `captain.ctx`/`.sid`/`.managed` now all agree on `1274a140…`), but it proves
the identity overlay flaps under concurrent same-agent sessions. It is NOT the false-negative,
but it shares the root (multi-writer `.ctx` + the heartbeat/identity interaction).

**Verdict:** the `stale` mass is largely the **pre-heartbeat tail** (heartbeat hk-81wk
suppresses new ones on live panes). The residual live churn is `foreign_session`. The actual
blind-spot worth fixing is §2, which the stale-count does not surface because the heartbeat
*converts* would-be stale events into silent fresh-but-wrong gauges.

---

## 4. `.ctx.tmp` write-race / atomicity

Both writers ARE atomic (write-tmp-then-`rename`):
- statusline (`keeper-statusline.sh:60,121-122`): `TMP_FILE="${CTX_FILE}.tmp.$$"` → `mv`.
- heartbeat (`heartbeat.go:134-151`): `os.CreateTemp(... agent+".ctx.*.tmp")` → `os.Rename`.

`rename(2)` on the same filesystem is atomic, so **no torn/partial read is possible** — a
reader sees either the old or the new whole file. The two leftover zero-byte
`captain.ctx.tmp.16966` (Jun19) / `captain.ctx.tmp.2512` (Jun16) are **crash residue** (the
shell `mv` never ran because the process died between `printf >TMP` and `mv`, or the write was
empty due to a `set -e` abort mid-pipe). They are **harmless leftovers**, not a live race —
they are NOT named `<agent>.ctx`, so no reader ever picks them up. Report 07's "torn read"
worry is unfounded; the only real issue is **cleanup hygiene** (tmp files orphan on crash).

**Conclusion: atomicity is fine. The false-negative is a VALUE-staleness bug, not a
write-race.**

---

## 5. Root cause (one sentence)

The freshness gate guards only the gauge file's **mtime**, while the keeper-side heartbeat
keeps that mtime fresh by re-stamping the file even when it can only carry the **last-good (or
wrong-session) token count** forward — so a genuinely oversize, busy-pane agent presents a
fresh-looking gauge with a stale number and **never crosses the warn/act threshold**.

---

## 6. Concrete minimal fix proposal

**Primary fix — bound the staleness of the *value*, not just the file
(`internal/keeper/heartbeat.go`, `maybeHeartbeat`):**

When `deriveContextTokens` returns `false` (cannot get a CURRENT reading), the heartbeat must
**NOT carry the last-good token count forward indefinitely under a fresh mtime.** Two
acceptable minimal forms:

- **(a) Stop re-stamping after a derive-miss budget.** Track how long the heartbeat has been
  unable to derive a fresh count for this session; once that exceeds a "value-staleness"
  bound (e.g. `2 * Staleness`, ~4min), **stop writing the heartbeat** and let the gauge go
  genuinely stale → the existing `no_gauge:stale` path fires (loud), restoring the safety the
  heartbeat removed. ~10 lines: add a `lastDeriveOK time.Time` to the watcher and gate the
  carry-forward write on it.

- **(b) Mark carried-forward readings as untrusted.** Add a `derived bool` (or `stale_value`)
  field to `CtxFile`; when the heartbeat carries `last.Tokens` forward without a fresh derive,
  set it false. Then in `belowWarnThreshold`/`belowActThreshold` (or at the watcher gate),
  treat an untrusted reading conservatively — e.g. when the value is untrusted AND its age
  exceeds a bound, fall through to the stale/no_gauge path rather than trusting the number.

(a) is the smaller, lower-risk change and preserves the existing event shape — **recommended.**

**Secondary fix — guard the heartbeat session_id against a stale `.managed`
(`heartbeat.go:160`, `heartbeatSessionID`):** prefer the **live `.sid`** (single-writer,
SessionStart-hook-owned) over `.managed` when `.sid` `isPrimarySID`, since `.managed` can lag a
post-`/clear` session by up to one repaint. This makes `deriveContextTokens` read the CURRENT
session's transcript, closing Path B. ~3 lines (read `.sid`, prefer it when primary).

**Cleanup (rides with either) — sweep orphan `.ctx.tmp.*`:** on watcher boot, remove
`<agent>.ctx.tmp.*` older than a few minutes (heartbeat.go or boot path). Cosmetic; addresses
the residue in `.harmonik/keeper/`.

**Do NOT** widen the warn/act band (operator HARD-NO, `feedback_keeper_band_no_retune`) — the
fix is value-staleness detection, not threshold loosening.

---

## 7. Why this is higher-priority than W7

W7 is CLI ergonomics. This is the **exact failure the keeper exists to prevent**: an oversize
agent silently overflowing because the gauge lied. W6 (hoist precompact past the stale
continue) does NOT help — it acts on a fresh gauge, and a fresh-but-wrong gauge under-reports
to precompact too. The heartbeat (hk-81wk) traded a loud false-positive (`no_gauge:stale`) for
a silent false-negative; this bead closes that trade.
