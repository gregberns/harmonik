# Token-Usage Audit Tooling + Model-Tiering — facts & options

**Date:** 2026-06-22 · **Status:** research complete, decisions pending operator
**Method:** 6-agent fan-out (prior-art · ccusage raw data · harmonik event data · model-tiering · join/rollup design · productization). This doc is **facts + possibilities for you to decide on** — nothing here is committed.

---

## TL;DR (the four findings that matter)

1. **The join is EASIER than we feared — two independent keys already exist.** A run's exact tokens/cost/model are recoverable today with *no new instrumentation* for daemon worktree runs.
2. **Harmonik already records the attribution dimensions** (which bead / crew / queue / run) — **the only missing piece is the token *numbers* themselves.** Harmonik captures *zero* real tokens today (its "budget" meter is an output-**bytes** proxy, not tokens).
3. **The biggest spend is currently invisible.** ~96% of fleet spend is **cache-read on 24/7 uptime** — i.e. the long-lived Opus captain + crew orchestrator sessions. Those sessions emit **no events at all**, so every per-bead rollup misses them.
4. **Model-tiering: most of the fleet is already Sonnet.** The bursty daemon work (DOT implementer + 3 reviewers), ops-monitor, and ctx-watchdog are Sonnet (or Haiku-able). The **only** large Opus consumers are the **captain** (must stay Opus) and the **24/7 crew orchestrators** — and there's already a knob to downgrade the latter. The lever is *uptime/restart-cadence + which crews run Opus*, not the daemon.

---

## 1. What we already know (prior audits — don't re-derive)

Three audits + two landed initiatives in the last week:

- **2026-06-17 fleet audit (`hk-bsdr`, codename:tokenaudit):** ~**$1,775 over 3 days**; **~95.9% of all tokens are cache-read** (output 0.65%). Smoking gun: **one 56h crew session = $245 = 14% of the 3-day spend** (944 turns × ~300K cache-read/turn). *Caveat: the canonical report `docs/retro/2026-06-17/token-burn-analysis.md` was never committed to git — the "96% cache-read" figure traces to that one uncommitted analysis, preserved in `docs/plans/leanfleet/design.md`.*
- **2026-06-16 diagnosis:** ~7.0B tokens / ~$5,637 over 10 days; cost = **fleet width × 24/7 uptime**, chiefly live captain/crew sessions. Keeper-restart-loop suspicion was **refuted** with event evidence.
- **2026-06-22 12h retro:** per-workflow Opus spend $3–$56 each (largest $55.88 / 56M cache-read); clean exact-cost extraction still **pending**.
- **Landed mitigations:** `leanfleet` (restart-earlier keeper bands, boot-digest, **Sonnet ops-monitor**, **per-crew `--model`**), `captain-economy` (boot 81k→~55-60k, ops-monitor absorbs the 12m checks), **per-queue spend caps + a global DaemonSpendMeter**.
- **Recommended-but-not-done:** fleet **sleep-wake POLICY** (mechanism `hk-rl4b` is built, the captain "when to sleep" policy is deferred), idle restart-to-small (`hk-ee81`), the daemon-native **300k force-cut governor** (P2, needs operator decision), event-log noise (~70% is keeper/governor heartbeat — `hk-qshh8`).

**Implication:** the efficiency frontier is **not** the daemon runs (already Sonnet, bursty, cheap) — it's the **always-on Opus orchestrators' cache-read burn**. Every prior audit says the same thing.

---

## 2. The two data sources (facts)

### Side A — Claude-Code usage (the token numbers live here)
- **Raw files:** `~/.claude/projects/<slug>/<sessionId>.jsonl`, one per session, **filename = sessionId**. Per **assistant turn**: `message.model`, `timestamp`, `cwd`, `gitBranch`, and full `message.usage` (`input`, `output`, `cache_creation`, `cache_read`, + ephemeral 1h/5m split, `service_tier`). **Model and all four token types are here, per turn.** Cost is *not* stored — it's computed from a pricing table.
- **`ccusage` (`npx ccusage@latest`)** reads exactly these files. `daily|weekly|session|blocks`, all with `--json` and `--since/--until` (date-granular). `session --json` → per-session, per-model breakdown + cost; `session -i <id> --json` → per-request entries. **`session.period` ≡ sessionId** (matched 504/504 in test).
- **Limits:** **per-machine** (remote workers' transcripts live on the worker box); retention ~30 days (Claude Code `cleanupPeriodDays`); date-granular windows.

### Side B — Harmonik event data (the activity dimensions live here)
- **`.harmonik/events/events.jsonl`**, append-only NDJSON, **no query API** (only the live `harmonik subscribe` socket). Envelope key = **`run_id`** (universal).
- **Terminal events (`run_completed`/`run_failed`) already carry:** `run_id`, `bead_id`, `owning_epic_id`, **`owning_epic_assignee` (crew name)**, `queue_id`, `queue_group_index`, `workflow_mode`. So **rollup by bead / epic / crew / queue / lane is already keyable.**
- **`claude_session_id`** is **minted by harmonik** (`--session-id <uuid>`, CHB-023) and persisted to `Run.context` + emitted on `transition_event`/`launch_initiated`/`handler_capabilities` (per `run_id`). 94% of harmonik session-ids resolve to a live Claude transcript.
- **Two hard gaps:** (a) **`model` is on NO event** — resolved into argv at launch and lost; (b) **no token counts anywhere** — `budget_accrual` exists but its `CostBasis="output_bytes"` is a byte proxy; `input/output/cache_read` are reserved label strings with **no emitter** (claude runs interactively in tmux → no stream-json → the daemon never sees per-turn usage).

---

## 3. THE JOIN (the central finding)

**Two independent join keys — bead-level attribution works *today* for daemon runs:**

1. **`gitBranch = run/<run_id>`** — every daemon **worktree** session's transcript stamps its branch as `run/<run_id>` (34/40 sampled). → **direct `run_id` join, side-A-only, needs zero harmonik plumbing.** Recover model + tokens straight from the transcript. *Primary key for worktree runs.*
2. **`claude_session_id`** — `events.jsonl` (per `run_id`) → ccusage `session.period` = transcript filename. Direct equality join. *Fallback / cross-check.*

**Where the join breaks — and the fallbacks:**

| Case | Why it breaks | Fallback |
|---|---|---|
| **Long-lived captain/crew orchestrators** (the big spend) | Not worktree runs (cwd=repo root, branch=`main`); one session spans many beads; **emit no events**. | Cannot bead-attribute. Bucket as **role-level burn** via a session→role registry (see §6 gap). This is the *intended* "idle-burn" bucket. |
| **`/clear` re-mints session_id** | One logical session → N transcript files. | Per-file sum over the window by role — a non-issue for *role* rollups. |
| **Multiple boxes (remote workers)** | Worker transcripts live on the worker. | Per-box collection (rsync/ship), keyed by `worker_name` already on the run-started payload. |

**Productive-vs-idle split falls out for free:** anything that joins to a `run_id` = productive bead work; everything else in a known role session = idle/overhead. That directly answers "how much are we burning on always-on orchestrators vs. actual work."

---

## 4. The rollup an audit agent wants (design)

Compute `$ = Σ(token_type × model price)`, then group. Worth producing:
- **Cost per bead** `{bead → tokens, $, cache-read%, model(s), runs, wall-time}` — the headline.
- **By model** — Opus/Sonnet/Haiku split + share-of-spend (verifies "DOT is on Sonnet").
- **By role/lane** — captain vs each crew vs keeper vs per-queue.
- **Productive vs idle burn** — bead-attributed vs role-level (the orchestrator-uptime question).
- **By token type** — cache-read% front-and-center (it's ~96% of volume, ~10× cheaper/token, but the biggest aggregate line).
- **By day/hour** across the window; **per-box**; a one-screen cross-cut (total $, top-5 beads, Opus%, idle%, cache-read%).

**Interface shape (target):**
```
harmonik usage --since 24h --group-by bead,model,lane,day --format json   # machine
harmonik usage --since 24h --format summary                                # one-screen human
```
JSON for agents (pipe to `jq`); a `warnings[]` array naming runs that couldn't be joined so coverage gaps are visible, not silently undercounted.

---

## 5. Model-tiering: where the Opus spend actually is

| Role | Today | Downgrade candidacy | Knob |
|---|---|---|---|
| **captain** | **Opus** | Must stay Opus (planning/judgment/fan-out synthesis). **No model knob exists for the captain** today. | — (would need one) |
| **crew orchestrators** (paul/stilgar/leto/…) | **Opus** | **Sonnet for clean lane-drain crews**; Opus only for design/test/root-cause crews. **The real addressable lever.** | ✅ mission front-matter `model:` (per-crew `--model`, `hk-9j3z`) |
| **DOT implementer + 3 reviewers** (per bead) | **Sonnet** | Already Sonnet (scale-out "DOT=sonnet"). Per-node `node.Model` knob exists if a node should drop to Haiku. | per-DOT-node model |
| **ops-monitor** | **Sonnet** | Already offloaded off the Opus captain (leanfleet LF-B). | crew model |
| **ctx-watchdog** | Sonnet | **Clean Haiku candidate** (mechanical gauge-poll + restart). | launch `--model` |
| **admiral** | operator-set | Intermittent hourly judgment → stays strong model **if used**; not a 24/7 burner. | `crew start --model` |
| **keeper / supervisor / reconcile / ops-monitor checks** | Go/bash or Sonnet | Mostly **pure-Go/bash = zero tokens**. | n/a |
| **flywheel governor** | Haiku/Sonnet (Pi `router.ts`) | Tier1=Haiku, Tier2/3=Sonnet; live but **observe-only, ~zero tokens**. | `.pi/extensions/flywheel/router.ts` |

**Conclusion:** the daemon is already cheap. The money is **(a) the captain (uptime-bound, can't downgrade) and (b) the 24/7 crew orchestrators (downgradable for clean lanes via the existing knob)**. So the highest-value efficiency moves are **uptime/cadence levers** — restart-earlier (landed), **fleet sleep-wake policy (built, not enabled)**, fewer-Opus-crews-by-default — **more than model swaps**, because cache-read on always-on sessions dominates. A cheap immediate win: default clean lane-drain crews to Sonnet via mission front-matter; reserve Opus for design/test/investigation crews (the per-crew knob already supports this).

---

## 6. Productization — making this first-class (options + phasing)

**Where it lives:** new leaf package `internal/usage` + a `cmd/harmonik/usage_cmd.go` verb (the existing `cmd/harmonik/usage.go` is just `--help` text — name is free for a real impl). Pure file-reader (events.jsonl + transcripts) so it works **even when the daemon is down**; depguard-clean (not in eventbus/handler).

**The instrumentation gaps to close (in priority order):**
1. **Emit `model` on `run_completed`/`run_failed`** — small daemon edit, highest payoff (makes side B self-sufficient; today model is recoverable only via the side-A transcript).
2. **Capture real tokens at run-end** — the pragmatic path (no change to the interactive tmux launch): **at `run_completed`, read claude's own transcript** (`~/.claude/.../<claude_session_id>.jsonl`) and emit a **`token_usage` event** `{run_id, session_id, bead_id, role, model, input/output/cache_read, ended_at}`. A spec home already exists: **`operator-nfr.md` ON-049** (the `(run_id, role, node_id, category, amount)` attribution 5-tuple) + ON-041a/046 `budget_summary` — **normative but unbuilt**.
3. **Register long-lived orchestrator sessions** — emit `session_registered{role, session_id, lane}` from the captain/crew launcher so the **biggest spend bucket** (always-on Opus sessions) is attributable instead of an unattributable blob.
4. Record `/clear` re-mints; resolve `queue_id`→name; multi-box transcript collection (via `worker_name`).

**A `token-audit` skill** (mirroring `agent-comms`/`beads-cli` under `.claude/skills/`) declares the command surface + rollup contract + healthy-bands, so **any** audit agent (here or in another deployment) runs the same checks — turning these one-off audits into a repeatable, portable capability. The skill is the portable contract; `harmonik usage` is its implementation.

**Phasing (each phase ships value; sizes are rough):**
- **Phase 0 (thin spike, ~1 bead):** a join script (or `harmonik usage` v0) = transcripts/ccusage joined on `run/<run_id>` branch + `claude_session_id`; per-session + per-bead + per-model rollup, JSON+summary. **No source changes.** Proves the join, gives an exact 24h cost picture immediately.
- **Phase 1 (~2-3 beads):** the `token-audit` skill + `internal/usage` projection over events.jsonl (by bead/epic/crew/queue using existing terminal keys).
- **Phase 2 (~3-4 beads):** the real instrumentation — `model` on terminal events (#1), `token_usage` via run-end transcript read (#2), orchestrator session registration (#3). Now attribution is built-in + box-agnostic; ccusage becomes optional cross-check.
- **Phase 3 (optional):** wire `budget_accrual` to real tokens + surface `budget_summary` on `harmonik list`/`status` — closes the ON-049/041a/046 spec gap.

---

## 7. Decisions for the operator

1. **Model-tiering now (cheap, no build):** default clean lane-drain crews to **Sonnet** via mission front-matter, reserve Opus for design/test/investigation crews? Drop **ctx-watchdog to Haiku**? (The captain stays Opus regardless.)
2. **Uptime levers (bigger savings than model swaps):** enable the built-but-dormant **fleet sleep-wake policy** when unattended? Adopt the daemon-native **300k force-cut governor**?
3. **Build scope:** approve **Phase 0** (the thin join spike) to get an exact, repeatable cost picture immediately — then decide on Phases 1-2 based on what it shows?
4. **Productization depth:** is the **`harmonik usage` verb + `token-audit` skill** the target end-state (portable to other deployments), or is a one-off script enough for now?

Recommended first step (cheapest, highest-information): **Phase 0 + the immediate Sonnet-crew-default**, then re-audit with the new tool to measure the delta.

---

## Appendix — key source pointers (verified)
- Join keys: side-A `~/.claude/projects/<slug>/<sessionId>.jsonl` (`gitBranch=run/<run_id>`, `message.model`, `message.usage`); side-B `.harmonik/events/events.jsonl` (`run_id`, `bead_id`, `owning_epic_assignee`, `queue_id`, `claude_session_id` via `transition_event`).
- claude_session_id mint/persist: `internal/handler/claudehandler_chb006_024.go` (`MintClaudeSessionID`), `internal/daemon/sessioncontext_chb023.go` (persist), `claudelaunchspec.go:405` (`--session-id`).
- Token gap: `internal/handlercontract/watcher_hc011.go:533` (`CostUnits = BytesEmitted`), `costbasis.go` ("token-count not carried in progress-stream"), `internal/daemon/spendmeter_hkk3f8g.go`.
- Model resolution (not logged): `claudelaunchspec.go:407` (`--model`), `core.Node.Model`, mission front-matter `model:` (`crewstart.go:482`), per-queue/config defaults.
- Spec home: `specs/operator-nfr.md` ON-049 / ON-041a / ON-046.
- CLI add point: `cmd/harmonik/main.go` arg switch; new `cmd/harmonik/usage_cmd.go` + `internal/usage`.
- Prior art: `docs/plans/leanfleet/design.md`, `plans/2026-06-20-captain-economy/`, bead `hk-bsdr`, memory `reference_token_burn_diagnosis_ccusage`.
