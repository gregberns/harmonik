# Keeper threshold misfire — why wrap-up messaging fires "~30k below" the configured 200k warn

READ-ONLY investigation. Captain keeper: `harmonik keeper --agent captain --warn-abs-tokens 200000 --act-abs-tokens 215000` (pid 59748, confirmed live). Operator observed a handoff/wrap-up nudge at "~170–190k" while the configured warn is 200k.

## TL;DR

There is **no early-firing bug in the gate**. The effective warn threshold is exactly **200,000 tokens** (the pct-ceil cannot pull it earlier on a 1M window — `min(200000, 0.70×1,000,000)=200000`). What the operator saw is a **text artifact**: the soft `[KEEPER HINT]` and the hard `[KEEPER WARN]` fire on the **same single 200k crossing**, and the HINT's body contains a **hard-coded literal "~190K"** that does not reflect the live gauge value. So the operator read "~190K" off the message and concluded the keeper fired at 190k — but the keeper actually fired at ~200k+. The HINT is harmless (a nudge, no forced clear).

## 1. HINT and WARN are NOT separate thresholds — they share the 200k crossing

Both strings are emitted from the **same `if warnArmed && !warnFired` block** at the warn crossing in `internal/keeper/watcher.go:1251`:

- `internal/keeper/watcher.go:1257` — `w.emitWarn(...)` (the `[KEEPER WARN]` path; injected text selected later by `selectWarnText`, `watcher.go:763`).
- `internal/keeper/watcher.go:1269-1275` — the one-time `[KEEPER HINT]` injection, gated only by `!hintSentThisSession`, fired **inside the same crossing block**. There is no second/lower threshold.

The hint text is a **hard-coded literal** — note the "~190K" is a fixed string, not an interpolated live value:

> `internal/keeper/watcher.go:1390`
> `const keeperHintText = "[KEEPER HINT] Context is at ~190K tokens. Consider wrapping up the current task and preparing a handoff soon."`

So whenever the warn fires (at the **200k** gate), the agent receives a message that *claims* "~190K" regardless of the real count. That 10–20k mismatch between the literal text and the true crossing point is the entire "~170–190k early fire" the operator perceived. The `[KEEPER WARN]` advisory text (`injector.go:14`, `wrapUpWarningText`) and the actionable form (`injector.go:39 ActionableWarnText`, which DOES interpolate live `tokens/1000`) are the harder messages — both ride the same 200k crossing.

## 2. The gate fires at exactly 200,000 tokens — NOT a percentage of a smaller window

`belowWarnThreshold` (`internal/keeper/watcher.go:706-712`, byte-identical to the cycler's `cycle.go:357`):

```go
if cf.Tokens > 0 && cf.WindowSize > 0 {
    return cf.Tokens < minAbsOrPctCeil(c.WarnAbsTokens, c.WarnPctCeil, cf.WindowSize)
}
return cf.Pct < c.WarnPct
```

`minAbsOrPctCeil` (`internal/keeper/thresholds.go:181-189`) returns `min(abs, pctCeil×window)` — it can only **tighten** (move earlier), never loosen. With the live captain values:
- `WarnAbsTokens = 200000`, `WarnPctCeil = 0.70` (default, `thresholds.go:43`), `WindowSize = 1000000` (from the gauge).
- pct-based = `0.70 × 1,000,000 = 700,000`, which is **not** `< 200,000`, so the function returns **200,000**.

The captain gauge has `tokens>0 && window>0`, so the **abs-token branch is taken** — the coarse `pct` (an integer, here 20–21) is irrelevant to the decision. The recent warn events confirm this: the most recent `session_keeper_warn` records `pct:20, warn_pct:80` (`.harmonik/events/events.jsonl`), i.e. the warn fired with pct(20) far below warn_pct(80) — only possible because the **token gate**, not the pct gate, fired. There is no stale-200k-window percentage trap; the F45 fabricated-window bug that once fired at ~140k is already fixed (`watcher.go:700-705`, hk-jgzg). Live flags carry **no `--warn-pct` override**, so nothing pulls the gate earlier.

## 3. Gauge accuracy — the gauge is faithful; it is NOT under/over-counting

The statusline writes `tokens` from `.context_window.total_input_tokens` (`scripts/keeper-statusline.sh:83`) and `window_size` from `.context_window_size` (line 86; inferred to 1,000,000 for `[1m]` models, lines 92-108). The keeper's own independent heartbeat derivation (`internal/keeper/heartbeat.go:64-118 deriveContextTokens`) computes context occupancy as:

> `sum = input_tokens + cache_read_input_tokens + cache_creation_input_tokens` (heartbeat.go:115)

i.e. **the total tokens fed into the most recent turn** — the correct proxy for live window occupancy (cache reads occupy the window). The statusline's `total_input_tokens` is Claude Code's equivalent of the same quantity. Live captain gauge: `{"pct":21,"tokens":211811,"window_size":1000000,...}` and the operator's "~201k" both sit right around the 200k gate — **the gauge is accurate**, not inflated. A 211,811 reading legitimately crosses 200,000. So the *gate* fired correctly at ~200k; only the *hint text* misrepresents it as "~190K".

(One minor coarseness: the gauge's `pct` is an integer percent of the 1M window — `211811 → pct 21`, `~200k → pct 20`. This rounding affects display only, not the abs-token decision.)

## 4. Is the early message harmful? — No, the HINT is advisory; only the act path (215k) forces a cycle

- `[KEEPER HINT]` (`watcher.go:1269`) is a **one-time nudge**, injected once per session, latched by `hintSentThisSession`. It does **not** drive `/session-handoff` or `/clear`. Harmless.
- `[KEEPER WARN]` advisory (`injector.go:14 wrapUpWarningText`) explicitly says "Keep working" — advisory only, no exit instruction. The actionable variant (`injector.go:39`) *instructs* a self-restart but only when CrispIdle + self-service-eligible + operator-not-attached (`watcher.go:731 actionableWarnEligible`, `:763 selectWarnText`); it tells the agent to act **only at a clean stop**.
- The **forced** clear/handoff cycle is the **act** path at `--act-abs-tokens 215000` (the Cycler's `actEffectiveTokens`), 15k above warn — not the warn/hint. So a warn/hint at ~200k does **not** prematurely clear the captain.

Net: the captain was nudged at the correct ~200k point but the nudge's wording understated the count by ~10k, and no premature handoff was forced.

## Root cause

The "~170–190k early fire" is a **reporting artifact, not a threshold bug**: the soft `[KEEPER HINT]` rides the same 200k warn crossing as the hard `[KEEPER WARN]`, and its text hard-codes the literal "~190K" (`internal/keeper/watcher.go:1390`) instead of interpolating the live token count. The operator read "~190K" off the message body. The actual gate (`watcher.go:706` / `thresholds.go:181`) fires at exactly 200,000 tokens on the 1M window, the gauge value (~211k) is accurate, and no premature clear occurs (forced cycle is the separate 215k act gate).

**Fix direction (not applied — investigation is read-only):** make `keeperHintText` interpolate the live `cf.Tokens` and the configured warn band (as `ActionableWarnText` already does at `injector.go:39`), so the hint says the real crossing value instead of a stale literal "~190K". Optionally fold the hint into the warn text to avoid two messages on one crossing.

## File:line citations
- `internal/keeper/watcher.go:1251-1276` — single crossing block emits BOTH warn and hint.
- `internal/keeper/watcher.go:1390` — `keeperHintText` hard-codes "~190K".
- `internal/keeper/watcher.go:706-712` — `belowWarnThreshold` abs-token gate (window>0 branch).
- `internal/keeper/thresholds.go:181-189` — `minAbsOrPctCeil` (tighten-only; 200000 wins on 1M window).
- `internal/keeper/thresholds.go:35-43` — defaults: warn 200_000, act 215_000, warnPctCeil 0.70.
- `scripts/keeper-statusline.sh:83,86` — gauge tokens = `total_input_tokens`, window = `context_window_size`.
- `internal/keeper/heartbeat.go:115` — independent occupancy = input + cache_read + cache_creation.
- `internal/keeper/injector.go:14,39` — advisory `wrapUpWarningText` ("Keep working") vs actionable `ActionableWarnText` (interpolates live tokens).
- `.harmonik/keeper/captain.ctx` — live: tokens 211811, window 1000000, pct 21.
- `.harmonik/events/events.jsonl` — latest captain warn: pct 20 / warn_pct 80 (token gate fired, not pct).
