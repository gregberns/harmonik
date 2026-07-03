# Cross-Model Eval Program — Run Matrix & Session-Data Metrics

**Date:** 2026-07-03. **Status:** design-only (no production code, no beads created by this doc).
**Scope:** two designs — (1) the **run matrix** that routes the SAME problem set through N
`(harness, model)` combos through harmonik's DOT workflow; (2) an **always-on session-data /
token extraction** step the PRODUCT runs at the END of every DOT for ANY harness, emitting a
normalized per-run record. Companion to `plans/2026-07-02-eval-harness/DESIGN.md` (the
single-model grading harness + curated task suite + `eval-bead.dot`). This doc adds the
cross-model dimension and the general session-data feature; it does NOT restate the task suite.

Combos in scope: **Claude+Sonnet, Claude+Opus, Codex+its model(s), Pi+MiniMax
(OpenRouter open-weight), Pi+ornith (DGX-local, `dgx.local:8551`, OpenAI-completions API)**.
Compare per-STEP wall-time, tokens in/out, and a quality signal (deterministic pass + LLM-judge).

---

## Part 0 — The routing seams as they ACTUALLY are today (studied first, cited)

`resolveHarness` (`internal/daemon/harnessresolve.go:53`) is a 4-tier precedence walk:

- **Tier 1 — per-bead `harness:<agent-type>` label** (`harnessresolve.go:61-87`). Exactly one
  `harness:` label whose value is a valid `core.AgentType` wins; **zero → fall through; two+ →
  conflict, treated as absent** (`:82-86`). This is the ONLY per-bead harness selector.
- **Tier 2 — per-queue harness default: field EXISTS but dispatch-wiring is a STUB.** The queue
  DOES persist a `DefaultHarness core.AgentType` (`internal/queue/types.go:310`, validated at
  submit via `NormaliseDefaultHarness` `queue/rpc.go:848`), BUT the production dispatch site passes
  `core.AgentType("")` for the queue tier — the field's wiring into the `resolveHarness` call is
  tracked as hk-xhawy and NOT landed (`workloop.go:3318`; `queue/types.go:324-326`). So at
  dispatch `queueDefault` is always empty today. **A queue cannot yet route by harness** — and note
  it carries only a harness override, NOT a model override.
- **Tier 3 — DOT-node harness attribute: STUB.** `nodeDefault` always empty; hk-u67of "not yet
  landed" (`harnessresolve.go:99-107`). **A DOT node cannot carry a harness today.** (Reviewer
  nodes DO pin a harness, but via a separate `pinnedHarnessLaunchSpecBuilder` that BYPASSES
  `resolveHarness` — `dot_cascade.go:1293` — not through the tier-3 path.)
- **Tier 4 — global `Config.DefaultHarness`**, else built-in `claude-code`
  (`harnessresolve.go:109-117`).

Every resolution emits `harness_selected{bead_id, agent_type, tier}` — **but NOT the model
string** (`emitHarnessSelected`, `harnessresolve.go:120-…`). The model is nowhere on this event.

**Model selection differs by harness — this is the crux of the matrix:**
- **Claude — per-DOT-node.** `node.Model` (the DOT `model="…"` attr) overrides the run-level
  model (`dot_cascade.go:1229-1231`) and is passed as `--model <model>` at
  `claudelaunchspec.go:408`. So **Sonnet vs Opus is a DOT-node attribute** — two Claude combos
  differ ONLY by the `model=` attr, no config change, and can run concurrently under one daemon.
- **Pi — global config only.** Model comes from `harnesses.pi.model` (required, zero baked
  default — `cmd/harmonik/resolve_pi_config.go:110-119`); passed as `--provider <prov> --model
  <prov/id>` argv (`internal/daemon/pilaunchspec.go:257-262`). There is **no per-node / per-bead
  Pi model override** — it's daemon-global config.
- **Codex — NOT harmonik-controlled at all.** harmonik sets only `CODEX_HOME` in the child env
  (`codexlaunchspec.go:248`); the codex model is governed by the codex CLI's own
  `$CODEX_HOME/config.toml` + ChatGPT login. There is NO codex `model` field in harmonik config
  (`harnesses.codex.*` only has `stale_wal_max_bytes`). **To vary the codex model you swap
  `$CODEX_HOME` / its config.toml** — an out-of-band, non-harmonik step.

**ornith (DGX) plumbing — LANDED, correcting the older DESIGN.md.** Pi now has a `base_url`
field (hk-z13jz): when `harnesses.pi.base_url` is set on the initial turn, the daemon generates a
`models.json` under the run worktree (`.harmonik/pi-agent/models.json`) pointing Pi at a local
OpenAI-compatible endpoint (`pilaunchspec.go:281-307`, `buildPiModelsJSON` `:328-356`). The
`harnesses.pi.api` field selects the API dialect (**`openai-completions`** for ornith; defaults
to `"openai"` when empty — `pilaunchspec.go:329-331`). Example in the config validator:
`http://dgx.local:8551/v1` (`resolve_pi_config.go:182`). So **Pi→ornith is pure config**:
`base_url` + `api: openai-completions` + `provider`/`model`/`api_key_env`.

**The single-bead Pi routing gotcha (LOAD-BEARING for the matrix; operator-tracked as hk-ytzj2):**
a queue's `default_harness=pi` does **NOT** reliably route beads to Pi — a bead that carries ANY
pre-existing label runs on `claude-code` (`harness_selected agent_type=claude-code`) despite the
queue default. Root cause (confirmed in code): Tier 2's dispatch-wiring is unlanded (hk-xhawy), so
the queue `DefaultHarness` never reaches `resolveHarness` — it always sees an empty `queueDefault`
and falls to Tier-4 claude-code. (Note: a repo grep did NOT find a bead literally id'd `hk-ytzj2`
in-tree; the code-level guardrail for the related "a coarse per-bead `harness:` label silently
overrides an intended node harness pin" hazard is at `dot_cascade.go:1293` — reviewer nodes use
`pinnedHarnessLaunchSpecBuilder` to bypass `resolveHarness` for exactly that reason.)
**Consequence for the matrix: every Pi (and codex) bead MUST carry an explicit `harness:pi` /
`harness:codex` LABEL** — never rely on a queue/config default for Pi work, or it silently runs on
Claude and the whole eval row is wrong.

---

## Part 1 — The run matrix (routing the SAME problem N ways)

### 1.1 Chosen routing strategy: re-submit the same task bead once per combo

Given the seams above, the cleanest routing is **N sibling beads per task — one per combo — that
share the same curated task body (same solution-file spec + same committed test), each carrying
the combo's routing labels.** Rationale, driven by what actually works today:

- Per-queue and per-DOT-node harness are **stubs** → we cannot express "combo" as a queue or as a
  node attribute for Pi/codex. The only working per-bead harness selector is the `harness:` label.
- Pi/codex model is **daemon-global config**, so two different Pi models (MiniMax vs ornith)
  **cannot run concurrently** under one daemon — they need distinct `harnesses.pi.*` config. This
  forces **sequential config-swap passes** for the two Pi combos (and, if codex has model variants,
  for those too). Claude combos, by contrast, differ by DOT `model=` attr and run concurrently.
- Re-submitting the same task body N times (vs one bead run N times) keeps each run's `run_id`,
  `harness_selected`, and grade cleanly separable in `events.jsonl` — the join key stays 1:1.

**Bead shape per combo:** clone the curated task into a per-combo bead
`eval-<task>-<combo>` with labels `workflow:dot` + `dot:eval-bead` (+ Claude combos add the
`model=` on the DOT's implement node — see 1.3) + `harness:<agent-type>`. Body/spec/test are
identical across the N siblings so only the routing varies.

### 1.2 Combo → exact config/label routing table

| combo | harness label | model source (where the model string lives) | concurrency | config required |
|---|---|---|---|---|
| **Claude / Sonnet** | `harness:claude-code` (or omit → Tier-4 default) | DOT implement-node `model="claude-sonnet-4-…"` attr (`dot_cascade.go:1230`, `claudelaunchspec.go:408`) | concurrent with Opus | none (per-node attr) |
| **Claude / Opus** | `harness:claude-code` | DOT implement-node `model="claude-opus-4-8"` attr | concurrent with Sonnet | none (per-node attr) |
| **Codex / \<model\>** | `harness:codex` | **NOT harmonik config** — codex CLI's own `$CODEX_HOME/config.toml` + ChatGPT login; harmonik sets only `CODEX_HOME` (`codexlaunchspec.go:248`) | own daemon config; sequential if multiple codex models | swap `$CODEX_HOME` / its config.toml out-of-band |
| **Pi / OpenRouter-MiniMax** | `harness:pi` | `harnesses.pi.model: openrouter/minimax/<id>`, `provider: openrouter`, `api_key_env: OPENROUTER_API_KEY`, **no** `base_url` (`resolve_pi_config.go:110-119`; `pilaunchspec.go:257-262`) | daemon-global; **sequential** vs ornith | swap `harnesses.pi.*` |
| **Pi / ornith (DGX)** | `harness:pi` | `harnesses.pi.model: <ornith-model-id>`, `provider: <name>`, `base_url: http://dgx.local:8551/v1`, `api: openai-completions`, `api_key_env: <key>` (`pilaunchspec.go:281-307`, `buildPiModelsJSON`) | daemon-global; **sequential** vs MiniMax | swap `harnesses.pi.*` |

**Execution plan for one problem set across all combos:**
1. **Pass A (concurrent):** submit the Claude/Sonnet + Claude/Opus sibling beads together
   (distinct `model=` on the implement node via two DOT variants, or one DOT parameterized by a
   per-node model — Claude only). Both run under one daemon config.
2. **Pass B (Pi/MiniMax):** set `harnesses.pi.{provider:openrouter, model:openrouter/minimax/…,
   api_key_env}`, restart daemon (Pi config is read at launch), submit the `harness:pi` sibling
   beads. **Every Pi bead carries `harness:pi` explicitly (hk-ytzj2).**
3. **Pass C (Pi/ornith):** swap `harnesses.pi.{provider, model, base_url:http://dgx.local:8551/v1,
   api:openai-completions, api_key_env}`, restart daemon, re-submit the `harness:pi` siblings.
4. **Pass D (Codex):** set `harnesses.codex.*` model, submit `harness:codex` siblings.

The Claude passes need no restart; the Pi/codex passes each need a config swap + daemon restart
because model is global. This asymmetry is inherent to the current seams — a future landing of
Tier-2 (per-queue harness+model, hk-4x3rg) or Tier-3 (per-node harness, hk-u67of) would let Pi
combos run concurrently via distinct queues/nodes and collapse passes B–D into one; call that out
as the matrix's forward-looking simplification, not a v1 requirement.

### 1.3 Model on the record (the `harness_selected` gap)

`harness_selected` carries `agent_type` + tier but **not model** (Part 0). Two options:
- **(a) minimal, no daemon change** — the collector derives model per row from: Claude → the DOT
  node `model=` attr (or run-level default); Pi/codex → the active `harnesses.<h>.model` config at
  run time. Fragile across config-swap passes (must snapshot config per pass).
- **(b) small daemon change (RECOMMENDED)** — add a `model` field to the `harness_selected`
  payload (`emitHarnessSelected`, `harnessresolve.go:120`) OR emit a new `model_selected` event
  once the launch model is resolved (Claude at `dot_cascade.go:1229`, Pi/codex at launch-spec
  build). Then the model is IN the log, keyed on `run_id`, and the record needs no config snapshot.
  This is the single most valuable small change for a trustworthy cross-model record.

---

## Part 2 — Always-on session-data / token extraction (general product feature)

**Operator ask:** the PRODUCT should ALWAYS extract session data at the END of a DOT, for ANY
harness — not eval-only. Design: a normalized per-run record + a daemon post-run hook.

### 2.1 What exists to build on (cited)

- **`internal/usage/usage.go` already does the hard join for Claude.** It reads Claude-Code
  session transcripts (`~/.claude/projects/…/*.jsonl`, matched on `gitBranch="run/<run_id>"`,
  `usage.go:3-6`) against `events.jsonl` on `run_id` and produces per-run **`RunRecord`**
  (`usage.go:119-134`) with `TokenUsage{Input, Output, CacheCreation, CacheRead}` (`:70-77`),
  `CostUSD` via a per-model price table (`priceFor`/`computeCost`, `:52,97-103`), `DominantModel`,
  `StartedAt`/`EndedAt`, `Success`. Invoked TODAY only as the post-hoc `harmonik usage` CLI over
  the whole log (`cmd/harmonik/usage_cmd.go:156 usage.RunAnalysis`) — **not per-run, not hooked
  into the daemon, and Claude-transcript-only.** This is the record schema + join logic to reuse.
- **Per-harness token sources differ:**
  - **Claude** — transcript `message.usage` fields, already parsed (`readTranscript`,
    `internal/usage/usage.go:468-473`): `input_tokens`, `output_tokens`,
    `cache_creation_input_tokens`, `cache_read_input_tokens` → `TokenUsage{Input,Output,
    CacheCreation,CacheRead}`. The transcript path is NOT computed from session-id — it comes from
    the **`session_log_location` event's `log_path`** field (`usage.go:385-389`, payload
    `SessionLogLocationPayload.LogPath` at `agentevents_hqwn59.go:663`), resolved to the on-disk
    jsonl by `resolveTranscriptPath` (`usage.go:495`).
  - **Pi** — `internal/daemon/pijsonlparser.go` parses Pi's `{"type":"agent_end","messages":[…]}`
    stream (`:12,57`). It extracts turn/message artifacts; **token/usage extraction is NOT
    currently pulled** — the parser would need a usage-field pass (Pi's agent_end/message frames
    carry usage; add extraction there, `pijsonlparser.go`).
  - **Codex** — `internal/daemon/codexjsonlparser.go` sees `turn.completed` frames that DO carry a
    `usage:{…}` object (`:26`), but the harness **explicitly ignores token counts today**
    (`:30` — "codex emits many item.* / token-count events the harness does not act on"). Extend
    the parser to capture `usage` off `turn.completed`.
- **The daemon post-run choke-point** is `emitDone` inside `beadRunOne`
  (`internal/daemon/workloop.go:2810-2822`) — the single closure that emits `run_completed` /
  `run_failed` for EVERY run and every workflow mode (single / review-loop / DOT). `emitDone` calls
  `emitRunCompleted` (`workloop.go:5266`), which already writes `started_at`/`ended_at`. **This is
  the one place a general "extract session data at end of run" hook belongs.**
- Timing: `wall_time_s = run_completed.ended_at − run_started.started_at` (same `run_id`);
  per-node implement time via `implementer_phase_complete − run_started` (the typed
  `state_entered`/`state_exited` node-bracket events are DEFINED but NOT emitted in production —
  confirmed in the companion DESIGN.md §1.1). `node_dispatch_requested`/`node_dispatch_decided`
  (`dot_cascade.go`) give coarse per-node timestamps for a per-STEP breakdown.

### 2.2 The normalized per-run record schema (harness-general extension of `usage.RunRecord`)

```json
{
  "schema_version": 1,
  "run_id":        "019f…",
  "bead_id":       "hk-…",
  "queue_id":      "eval-pi",
  "harness":       "pi",                         // claude-code | codex | pi  (from harness_selected)
  "model":         "openrouter/minimax/…",       // launch model (see Part 1.3 — from log if 2b lands)
  "success":       true,
  "started_at":    "2026-07-03T21:14:03Z",
  "ended_at":      "2026-07-03T21:17:38Z",
  "wall_time_s":   215.0,                         // ended_at − started_at
  "nodes": [                                      // per-STEP breakdown (the operator's per-step ask)
    {"node_id":"implement","kind":"agentic","wall_time_s":191.2,
     "tokens":{"input":42100,"output":8800,"cache_creation":1200,"cache_read":301000}},
    {"node_id":"grade","kind":"shell","wall_time_s":3.1,"tokens":null},
    {"node_id":"judge","kind":"agentic","model":"claude-opus-4-8","wall_time_s":20.7,
     "tokens":{"input":6100,"output":900,"cache_creation":0,"cache_read":0}}
  ],
  "tokens_total": {"input":48200,"output":9700,"cache_creation":1200,"cache_read":301000},
  "cost_usd":     0.41,                           // null when no price table for the model (Pi/ornith)
  "turn_count":   14,
  "commit_sha":   "abcd123"
}
```

Notes: `tokens_total` reuses `usage.TokenUsage` verbatim (four categories). `cost_usd` is
derivable only where a price table exists (`priceFor`, `usage.go:52`) — Claude yes; OpenRouter has
published prices (add a table); ornith (local) has no marginal token cost → `null` is correct.
Per-node `tokens` requires attributing transcript turns to nodes — for Claude the transcript turns
fall between the node's dispatch/complete events; for codex/pi the single agentic node gets the
whole `usage`. The **eval** record (companion DESIGN §1.3: `task_id`, `difficulty`, `pass`,
`check_kind`, `judge_grade`) is a THIN eval-only superset layered on top of this general record —
eval adds the grading fields; the general product record carries model/harness/timing/tokens/cost.

### 2.3 The extraction hook — where it plugs in

**Recommended: a daemon post-run hook fired from `emitDone`, NOT a new DOT terminal node.**

- **Why a hook, not a terminal DOT node:** the operator wants this for ANY harness and ANY
  workflow (single, review-loop, DOT) — a terminal DOT node would only fire for DOT-mode runs and
  would need every DOT to include it. `emitDone` (`workloop.go:2810`) fires for **all** modes on
  **every** terminal transition — one seam, universal coverage.
- **Hook design:** after `emitRunCompleted` returns inside `emitDone`, invoke a
  `sessiondata.Collect(runID, beadID, harness, model, project)` that: (1) resolves the harness's
  session log via the **`session_log_location` events for this `run_id`** — each carries
  `{agent_type, node_id, log_path, log_format}` (`SessionLogLocationPayload`,
  `agentevents_hqwn59.go:663`), which is the harness-general index into every node's log (Claude
  transcript, codex jsonl, pi jsonl) and simultaneously gives the per-node breakdown key,
  (2) parses tokens via the harness-appropriate parser (reuse `usage` for Claude; extend
  `codexjsonlparser`/`pijsonlparser` for the others), (3) joins timing + node breakdown from
  `events.jsonl` by `run_id`, (4) appends ONE record line to
  `<project>/.harmonik/session-data.jsonl`. Read-only over logs, off the hot path (fire in a
  goroutine after the terminal event so it never blocks run completion), best-effort (a parse
  failure logs + emits an empty-token record, never fails the run).
- **Refactor `internal/usage` into the shared core:** lift its join + `RunRecord` + price logic
  into a harness-general `sessiondata` collector; `harmonik usage` (the existing CLI report) then
  becomes an aggregation VIEW over the same `session-data.jsonl` the hook writes, instead of
  re-deriving from raw logs each invocation.
- **Eval reuses this, doesn't duplicate it:** the eval-harness collector (bead EH1,
  `hk-eval-harness-collector-uavgd`, which today reads `events.jsonl` by `run_id`) becomes a
  **consumer** of `session-data.jsonl` — it joins the general per-run record with the grade/judge
  fields to produce `eval-results.jsonl`. EH1's schema (companion DESIGN §1.3) is the general
  record + eval columns. This keeps ONE token-extraction path for the whole product.

---

## Part 3 — Beads to create

1. **`model_selected` on the log (Part 1.3b)** — add a `model` field to `harness_selected`
   (`harnessresolve.go:120`) OR emit a `model_selected{run_id, model, harness}` event at launch
   resolution (Claude `dot_cascade.go:1229`; Pi/codex launch-spec). *P1 — gates a trustworthy
   cross-model record.* (bug/task)
2. **General `sessiondata.Collect` post-run hook** — fire from `emitDone`
   (`workloop.go:2810`); refactor `internal/usage` join+`RunRecord`+price into a harness-general
   collector; write `<project>/.harmonik/session-data.jsonl`; goroutine, best-effort, off hot path.
   *P1 — the operator's "always extract at end of DOT" feature.* (feature/epic)
3. **Codex token extraction** — extend `codexjsonlparser.go` to capture `usage` off
   `turn.completed` (`:26,30`). *P2.* (task)
4. **Pi token extraction** — extend `pijsonlparser.go` to pull usage from Pi `agent_end`/message
   frames. *P2.* (task)
5. **Per-node token attribution** — split transcript token usage across DOT nodes using
   `node_dispatch_requested`/`node_dispatch_decided` + `implementer_phase_complete` timestamps for
   the per-STEP `nodes[]` array. *P2.* (task)
6. **OpenRouter / open-weight price table** — extend `priceFor` (`usage.go:52`) with OpenRouter
   MiniMax pricing; ornith → `cost_usd: null` (local, no marginal cost). *P3.* (task)
7. **Cross-model run-matrix runbook + N-sibling submitter** — a script/skill that clones a curated
   task into N per-combo sibling beads with the correct `harness:`/`model=`/config-pass labels per
   the Part-1.2 table, and drives passes A–D (Claude concurrent; Pi/codex config-swap + restart).
   *P2.* (task)
8. **EH1 becomes a consumer of `session-data.jsonl`** — re-point
   `hk-eval-harness-collector-uavgd` to join the general record with grade/judge fields instead of
   re-deriving timing/model from `events.jsonl`. *P2 — depends on #2.* (task; update existing bead)

Forward-looking (out of v1 scope, note in the matrix): landing Tier-2 per-queue harness+model
(hk-4x3rg) or Tier-3 per-node harness (hk-u67of) would let the Pi/codex combos run concurrently
via distinct queues/nodes and collapse passes B–D into one — the matrix's future simplification.
