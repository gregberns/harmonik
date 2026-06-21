# Lens 3 — Cognition-observability fields in the system-state snapshot

**Bead:** hk-jay1 (context-into-state) · **Design pass:** hk-9fvk (`specs/system-state.md`) · **Codename:** `fleet-state`
**Scope:** the P2-c slice — replace a constantly-polling "checker agent" by capturing context-size + derived cognition signals INTO the system-state snapshot, so agents (and the existing ~30-min ctx-watchdog) READ signals instead of eyeballing gauges.
**Locked framing:** session-level gauges FIRST; per-subagent context is a follow-on stretch. The schema leaves a clean slot for it but does not plumb it (decision #3, scaffold).

---

## 1. How context size is gauged TODAY (file:line)

The raw signal already exists per session — the cognition fields **read existing readers**, they do not invent a new measurement.

**The gauge file (`.ctx`) — the single per-session context reading:**
- `internal/keeper/gauge.go:18-24` — `CtxFile{ Pct, Tokens, WindowSize, SessionID, Ts }`, written as one JSON line to `.harmonik/keeper/<agent>.ctx`. `Ts` is RFC-3339 — **this is the staleness clock the cognition fields reuse.**
- Writer: `scripts/keeper-statusline.sh` — runs ONLY on a Claude Code UI repaint; pulls `.context_window.used_percentage` / `.total_input_tokens` / `.context_window_size`. **Skips the write entirely** when the pane reports `NA` (right after `/clear`, or when a session stops repainting). So `.ctx.Ts` ages on a perfectly-live agent.
- Reader: `internal/keeper/gauge.go:34-63` `ReadCtxFile` → returns `(*CtxFile, modTime, err)`. `Tokens`/`WindowSize` default 0 on older Claude Code; logic falls back to `Pct`-based gating when either is 0.

**The keeper-side heartbeat (the gauge-liveness backstop) — derives tokens independently of repaint:**
- `internal/keeper/heartbeat.go:70-125` `deriveContextTokens(transcriptDir, sessionID)` — scans `~/.claude/projects/<munged>/<sessionID>.jsonl`, sums `input_tokens + cache_read_input_tokens + cache_creation_input_tokens` of the **last usage-bearing assistant turn**. This is the best proxy for current context occupancy and is the canonical "what is this session's context size RIGHT NOW" computation.
- `heartbeat.go:190-252` `maybeHeartbeat` re-writes `.ctx` with a fresh `Ts` + re-derived `Tokens` when the gauge ages while the pane is alive (`IsPaneIdleFn` gate). Token miss budget = `HeartbeatMaxMisses` (`thresholds.go:152`, default 12).

**Thresholds — already config-sourced, zero-hardcoded path exists:**
- `internal/keeper/thresholds.go` — compiled DEFAULT band (warn 200K / act 215K / force_act 240K; pct ceils 0.70/0.85; hard ceiling 280K at `:95`). These are **defaults only**.
- `internal/daemon/projectconfig.go:426-477` — `KeeperConfig` is the parsed `.harmonik/config.yaml` `keeper:` block: `WarnAbsTokens / ActAbsTokens / ForceActAbsTokens / HardCeilingAbsTokens / WarnPctCeil / ActPctCeil / Staleness / …`. **Zero = not configured ⇒ defer to compiled default.**
- `cmd/harmonik/resolve_keeper_config.go` — `ResolveKeeperConfig` = the SINGLE precedence resolver: **FLAG > CONFIG > DEFAULT**, fail-loud on bad values. **This is the "no hardcoded thresholds" mechanism the keeper-config-redesign direction wants, and every cognition signal's threshold MUST route through the same resolved struct — never a literal in the snapshot builder.**

### Known gauge drift the schema MUST survive (load-bearing)
1. **Crew gauge unwired on the live deployment** — `keeper doctor` reports the gauge not wired for crews; a crew's `.ctx` may be permanently absent or stale even though the session is live. → cognition fields per session must carry an explicit **`source`** + **`stale`** discriminator and degrade to a transcript-derive or capture-pane fallback, never silently read 0 as "small."
2. **`session_id` flips on `/clear`** — the launch `--session-id` goes dead after the first keeper `/clear`; identity must be re-resolved from the `.sid` channel (`internal/keeper/sessionid.go:30-46` `ReadSessionIDFile`, primary-SID gate `isPrimarySID`/`isUUIDv4`). `ReadCtxFile` already OVERRIDES `cf.SessionID` with the `.sid` value when primary (`gauge.go:59-61`). → the cognition snapshot keys context on the **agent name** (the stable `.ctx`/`.sid` filename key), NOT the volatile session UUID; the UUID is carried as a *resolved-at-read* attribute, not the join key.

---

## 2. Per-session cognition field schema

One `cognition` object per session in the system-state snapshot. **Keyed by agent name** (stable across `/clear`), session UUID carried as an attribute. Every threshold is a *reference to the resolved keeper config*, not a value baked into the snapshot.

```jsonc
// system-state snapshot → sessions[<agent>].cognition
{
  "agent":        "paul",                  // STABLE join key (the .ctx/.sid filename), survives /clear
  "session_id":   "a1b2…-4…-…",            // resolved-at-read from .sid (gauge.go:59); may be "" → see source
  "context": {
    "tokens":       182304,                // CtxFile.Tokens, or heartbeat deriveContextTokens() fallback
    "window_size":  1000000,               // CtxFile.WindowSize; 0 ⇒ FallbackWindowSize from resolved cfg
    "fill_frac":    0.182,                  // tokens / effective window; the model-agnostic primary fill
    "source":       "gauge",               // gauge | heartbeat_derive | capture_pane | absent  (drift discriminator)
    "gauge_ts":     "2026-06-20T19:30:00Z",// CtxFile.Ts — the staleness clock
    "read_ts":      "2026-06-20T19:31:05Z",// when THIS snapshot read it
    "age_seconds":  65                      // read_ts - gauge_ts; feeds signal (b)
  },
  "signals": {
    "too_big":         { "tripped": false, "threshold_ref": "keeper.warn_abs_tokens", "threshold": 200000, "value": 182304 },
    "stale_stuck":     { "tripped": false, "threshold_ref": "keeper.staleness",       "threshold_s": 120, "age_s": 65 },
    "loop_detected":   { "tripped": false, "source": "haiku", "checked_ts": "…", "confidence": 0.0, "note": "" }
  },
  "subagents": null   // STRETCH SLOT — see §5; null/absent in v1, never plumbed
}
```

**Field provenance (all real):**
- `tokens` / `window_size` / `gauge_ts` ← `gauge.go:18-24` `CtxFile`; on `source:absent|stale` fall back to `heartbeat.go:70` `deriveContextTokens` then to a `capture-pane` tail (the watchdog already does this — prompt step 2).
- `fill_frac` = `tokens / window` where `window = WindowSize>0 ? WindowSize : cfg.FallbackWindowSize` (mirrors `heartbeat.go:225-229`). **`fill_frac` is the cross-model-safe primary** because the abs band only wins on a 1M window and the pct ceil wins on a 200K window (`thresholds.go:18-20`).
- `source` discriminator is the answer to drift #1: `gauge` (fresh `.ctx`), `heartbeat_derive` (transcript JSONL), `capture_pane` (last-resort tail), `absent` (no reading — crew-unwired case). A snapshot consumer treats `absent` as **unknown, not zero**.

---

## 3. The three derived signals — input · threshold-source · compute-timing

### (a) `too_big` — context over the band
- **Input:** `context.tokens` (or `fill_frac` × window).
- **Threshold-source:** the resolved keeper band via `ResolveKeeperConfig` (`resolve_keeper_config.go`) — `warn_abs_tokens` / `act_abs_tokens` capped by `min(abs, pct_ceil × window)` (`thresholds.go:181` `minAbsOrPctCeil`, `:200` `EffectiveBandTokens`). **No literal in the builder** — the signal records `threshold_ref` ("keeper.warn_abs_tokens") + the resolved numeric so the consumer can see *which* config knob fired.
- **Compute-timing:** cheap, deterministic, computed **every snapshot build** (`harmonik state`). It is a comparison, not an LLM call.
- **Tiers:** report the band level reached (`warn` / `act` / `force_act` / `hard_ceiling`) rather than a bare bool, reusing the existing four-level band so the watchdog can choose nudge vs restart vs alarm — and matching the existing `HardCeilingMode = off|alarm|restart` config (`projectconfig.go:444`).

### (b) `stale_stuck` — context not changing
- **Input:** `context.age_seconds` (`read_ts − gauge_ts`) AND a token-delta-vs-prior-snapshot (the snapshot builder keeps the prior reading; `tokens` unchanged across N builds while the pane is non-idle ⇒ stuck).
- **Threshold-source:** `keeper.staleness` (resolved; default `DefaultStaleness = 120s`, `thresholds.go:133`). Two sub-conditions, both config-sourced:
  - gauge-aging: `age_s > staleness` (the existing `no_gauge:stale` clock).
  - token-flat: `tokens` delta == 0 across ≥ `stuck_min_intervals` snapshots while `source != absent` and pane alive (`IsPaneIdleFn`). `stuck_min_intervals` is a NEW config knob (defaults from the resolved struct, not a literal).
- **Compute-timing:** every snapshot build; needs the prior snapshot's `tokens` (the builder already holds a per-agent last-good, mirroring the heartbeat carry-forward at `heartbeat.go:212`).
- **Drift note:** distinguish *gauge stale* (drift #1 — a measurement gap, `source:absent/stale`) from *session stuck* (real — token-flat on a fresh gauge). Only the latter trips `stuck`; a stale gauge trips a separate read-quality flag so the watchdog does not restart a healthy-but-unmeasured crew.

### (c) `loop_detected` — repeating-pattern on recent messages (cheap Haiku pass)
- **Input:** the **last K assistant/tool turns** of the session transcript JSONL (the SAME file `deriveContextTokens` already opens — `~/.claude/projects/<munged>/<sessionID>.jsonl`, `heartbeat.go:74`). K is a config knob (e.g. last 12 turns), trimmed to a token budget so the Haiku call is cheap.
- **What the Haiku pass decides:** "is this session doing the same thing over and over" (re-running the same command, re-reading the same file, an ACT-loop, a reviewer/implementer ping-pong) — a single cheap classification returning `{tripped, confidence, note}`. This is the ONE cognition (LLM) step; everything else is deterministic Gather (ZFC-clean: detection-as-signal, not detection-as-action — §5).
- **Threshold-source:** `confidence ≥ keeper.loop_confidence_min` (NEW resolved config knob). No hardcoded cutoff.
- **Compute-timing — where it runs / how it is stored (the crux):**
  - It does **NOT** run on every `harmonik state` build (that would re-introduce a constant LLM poll — the exact thing decision #3 kills).
  - It runs **on-demand, event/cadence-triggered**: the existing ~30-min ctx-watchdog tick invokes it (or `harmonik state --refresh-loop-check`) only for sessions whose deterministic signals already look suspicious — `too_big.tripped` OR `stale_stuck` token-flat. **Deterministic signals gate the expensive Haiku pass**, so cost scales with suspicion, not with fleet size.
  - **Storage:** the Haiku verdict is written back INTO state as `signals.loop_detected` with `checked_ts` (its own staleness clock — a verdict older than the cadence is treated as unknown, re-checked on next gate). Natural home: a sibling of the gauge under `.harmonik/keeper/<agent>.loop.json` (single-writer, same pattern as `.sid`/`.ctx`), folded into the snapshot by the `harmonik state` aggregator. This makes the loop verdict a cheap *read* for every subsequent consumer — the whole point of context-into-state.

**Substrate note:** the unwired sentinel governor (`internal/sentinel/governor.go` + `adversary.go`, 0 callers — synthesis §8 fork 3) is the natural Go-governor → fresh-context-LLM-adversary pattern for the Haiku pass. The signal design above does not depend on reviving it; it just names it as the ready substrate.

---

## 4. Watchdog reads state instead of eyeballing (integration)

**Today (eyeball):** `scripts/ctx-watchdog-launch.sh` seeds a Sonnet `/loop 30m` whose prompt (`.harmonik/cognition/ctx-watchdog-prompt.txt`) hand-reads each `<agent>.ctx`, compares `tokens>=300000`, falls back to `capture-pane | tail -5`, and restarts. The 300K cap is a **literal baked into the prompt** — drift from the keeper band (215K/240K) and from config.

**After (read state):** the tick becomes:
1. `harmonik state --json` → the snapshot with `sessions[*].cognition.signals` already computed (deterministic ones for free; loop verdicts present if a prior gate fired).
2. For each session, read `signals.too_big.tripped` (+ its band level) and `signals.stale_stuck.tripped` — **no per-agent `.ctx` parsing, no hardcoded 300K**; the threshold is whatever `ResolveKeeperConfig` resolved, surfaced in `threshold_ref`/`threshold`.
3. For any session whose deterministic signal is suspicious but `loop_detected` is unknown/stale, the tick triggers the on-demand Haiku pass (§3c) and re-reads.
4. Act per `HardCeilingMode` semantics (alarm vs restart) — the **observe** stays here; the **act** (crew stop/start) is the watchdog's existing job, unchanged.

This collapses the watchdog prompt from "read N gauge files + a literal cap" to "read one snapshot + honor its signals," and removes the 300K-vs-config drift. The watchdog keeps its sleep-guard (`.sleeping.<sid>` skip — already in the launch/respawn scripts) because the snapshot OBSERVES; it does not wake parked sessions.

---

## 5. Stretch slot — per-subagent context (named, NOT plumbed)

- **Field:** `sessions[<agent>].cognition.subagents` — an array of `{ subagent_id, tokens, fill_frac, source, gauge_ts }`, same shape as the session `context` block, one entry per in-flight Task/sub-agent.
- **v1 status:** **`null` / absent.** No reader, no writer, no signal derivation. The schema reserves the name and shape so a later slice can populate it without a snapshot-format break.
- **Why it is a clean future slot:** a sub-agent has no `.ctx` of its own today (only top-level sessions get a keeper gauge), so its context would have to come from the parent transcript's nested usage blocks — that plumbing is explicitly deferred. The three session-level signals (§3) generalize per-subagent unchanged once the readings exist; nothing in the v1 signal logic assumes session-only.

---

## 6. Out of scope (operator-declared — state OBSERVES, never ACTS)

Per scaffold §"Out of scope": the cognition fields make these **visible** but do not remediate them.
- A captain that has lost its way after many `/clear`s (token-burn from a confused captain) — `too_big`/`loop_detected` will SURFACE it; auto-remediation is NOT in this initiative.
- A crew stuck at ~200K with a forever-wake monitor — `stale_stuck`/`loop_detected` SURFACE it; no auto-fix here.
- The signals are inputs to the captain's (LLM) judgment and to the existing watchdog's existing restart authority — they do not add a new autonomous actor. This keeps P2-c ZFC-clean: **Gather (the readings) + a single cheap Decide (the Haiku loop classification) folded into state**, never a Go heuristic that parks/kills on its own.

---

## Evidence map
- Gauge file + reader: `internal/keeper/gauge.go:18-63`; writer `scripts/keeper-statusline.sh`.
- Token derivation + heartbeat: `internal/keeper/heartbeat.go:70-125` (`deriveContextTokens`), `:190-252` (`maybeHeartbeat`).
- Thresholds (defaults) + band formula: `internal/keeper/thresholds.go:23-98` (band), `:181` (`minAbsOrPctCeil`), `:200` (`EffectiveBandTokens`), `:133` (`DefaultStaleness`).
- Config block (threshold SOURCE): `internal/daemon/projectconfig.go:426-477` (`KeeperConfig`).
- Precedence resolver (no-hardcoded path): `cmd/harmonik/resolve_keeper_config.go` (`ResolveKeeperConfig`, FLAG>CONFIG>DEFAULT, fail-loud).
- Identity / SID-flip robustness: `internal/keeper/sessionid.go:30-46` (`ReadSessionIDFile`), `gauge.go:59-61` (`.sid` override).
- Watchdog (eyeball today): `scripts/ctx-watchdog-launch.sh` + `.harmonik/cognition/ctx-watchdog-prompt.txt` (300K literal cap).
- Loop-pass substrate (unwired): `internal/sentinel/governor.go` + `adversary.go`.
