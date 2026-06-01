# Operator Config Inventory

```yaml
---
title: Operator Config Inventory
spec-id: operator-nfr/config-inventory
requirement-prefix: ON-CFG
spec-category: foundation-cross-cutting
status: draft
version: 0.1.1
spec-template-version: 1.1
owner: foundation-author
last-updated: 2026-05-31
satisfies: ON-004 (operator-nfr.md §4.1)
depends-on:
  - operator-nfr
  - event-model
  - control-points
  - process-lifecycle
  - reconciliation/spec
  - cognition-loop
  - credential-isolation
---
```

## 1. Purpose

This document is the normative config inventory obligated by [operator-nfr.md §4.1 ON-004]. It enumerates every operator-configurable knob referenced across foundation specs. For each knob the inventory declares:

- **Precedence layer** — which of the four CP-037 tiers owns the resolved value (runtime override ▷ operator-policy file ▷ workflow definition ▷ default).
- **Default value** — the built-in fallback when no higher-precedence layer supplies a value.
- **Allowed range / enumeration** — the set of legal values.
- **Change-takes-effect semantics** — when a change to this knob becomes observable.

Precedence layer terms follow [control-points.md §4.7 CP-037] (highest first):

1. **Runtime override** — flag or env var supplied at process invocation; beats everything.
2. **Operator-policy file** — persistent operator YAML at the project level; beats workflow definition and default.
3. **Workflow definition** — per-workflow YAML or bead label; beats default only.
4. **Default** — shipped built-in; lowest precedence.

A change to any layer takes effect at the next operator pause per CP-037, except where a knob's own semantics column says "per-invocation" or "per daemon start."

---

## 2. Inventory

### 2.1 Event-bus flush cadence — `timer_flush_cadence`

| Field | Value |
|---|---|
| Spec source | [event-model.md §4.4 EV-016] |
| Knob | Timer-flush interval for `ordinary`-class JSONL events |
| Precedence layer | Operator-policy file ▷ default |
| Default value | **1 second** |
| Allowed range | Positive duration (seconds); 0 disables the timer entirely (only `fsync-boundary` events trigger fsync) |
| Change-takes-effect | Next daemon start (the JSONL writer reads the interval once at startup) |
| Notes | `fsync-boundary`-class events always fsync synchronously per EV-016 — no knob controls that. `lossy-tail-ok`-class events may be flushed only opportunistically. This knob affects only `ordinary`-class events. |

---

### 2.2 Budget warning threshold — `warning_threshold`

| Field | Value |
|---|---|
| Spec source | [control-points.md §4.5 CP-022, CP-025] |
| Knob | Fraction of a Budget's `limit` at which `budget_warning` fires |
| Precedence layer | Per-Budget declaration in operator-policy file ▷ default |
| Default value | **0.8** (80%) |
| Allowed range | Ratio in [0, 1]; value 0 suppresses warning; value 1 fires only at exhaustion |
| Change-takes-effect | Next daemon start (Budgets are registered at policy-load time per CP-022) |
| Notes | Each Budget record in the policy YAML may override this field independently. The `tightest-wins` rule (CP-022) applies before the threshold is checked — the threshold is evaluated against the tightest applicable budget's `limit`. |

---

### 2.3 Drain timeouts — `timeout.step_N` / `drain_timeout_total`

| Field | Value |
|---|---|
| Spec source | [operator-nfr.md §4.7 ON-029] |
| Knob | Per-step drain timeout (`timeout.step_2`, `timeout.step_3`, `timeout.step_3a`, etc.) and optional aggregate `drain_timeout_total` |
| Precedence layer | Operator-policy file ▷ default |
| Default value | step_2: **300 s** · step_3: **60 s** · step_3a: **30 s** · step_4: **10 s** · step_5: **10 s** · step_6: **10 s** · `drain_timeout_total`: **not declared** (operator opt-in; if declared, MUST be ≥ sum of step values; with all defaults the sum is 410 s) |
| Allowed range | Positive integer (seconds) per step; when `drain_timeout_total` is declared it MUST satisfy: `drain_timeout_total ≥ sum(step_2 + step_3 + step_3a + step_4 + step_5 + step_6)` — validated at daemon startup |
| Change-takes-effect | Next daemon start (drain timeouts are read once at startup per ON-029) |
| Notes | Step 1 (`stop_queue_advancement`) is non-blocking and carries no timeout knob. Steps 2 and 3 involve agent subprocesses: on timeout, the daemon sends SIGKILL and synthesizes `agent_warning_silent_hang{reason=drain_forced}` per ON-040 before advancing to the next step. Steps 3a, 4, 5, and 6 involve no external subprocesses: on timeout, the operation is aborted and the step is marked exceeded. All exceeded steps contribute to the `drain-timeout-escalated` exit code (§8 taxonomy) emitted in step 7. The `drain_timeout_total` runtime check fires after all steps complete; a breach is a configuration error (the config validation at startup should prevent it). The §7.2 pseudocode is the normative reference for per-step apportionment. |

---

### 2.4 Restart RTO thresholds — `rto_nominal_p95_seconds` / `rto_ceiling_seconds`

| Field | Value |
|---|---|
| Spec source | [operator-nfr.md §4.8 ON-031, ON-032] |
| Knob | Nominal p95 target and hard ceiling for SIGTERM → `ready` recovery time |
| Precedence layer | Spec-defined (not operator-configurable at v0.1; relaxation via OQ-ON-005b requires spec amendment) |
| Default value (nominal) | **30 seconds** (p95 under the standard fixture per ON-032 criterion 1) |
| Default value (ceiling) | **300 seconds** (hard; non-negotiable per ON-032 criterion 3) |
| Allowed range | Positive integer (seconds); the 300s ceiling MUST NOT be raised without a spec amendment |
| Change-takes-effect | N/A — these are measurement invariants, not runtime parameters. Ceiling breach triggers daemon `degraded` with progress markers and operator notification per ON-032. |
| Notes | Standard fixture: ≤ 500 open beads, ≤ 50 in-flight runs, git-log depth ≤ 10,000 commits, ≤ 100 Cat-3-pending runs, ≤ 10 active investigator workflows. Nominal target MAY be relaxed with documented reason per OQ-ON-005b; the 300s ceiling is non-negotiable. |

---

### 2.5 Queue-empty re-query cadence — **RETIRED**

| Field | Value |
|---|---|
| Spec source | [process-lifecycle.md §4.4 PL-013 (retired)] |
| Status | **RETIRED** in extqueue v0.1 |
| Notes | The Beads-ready-poll dispatch model and its associated `harmonik enqueue` wake channel have been removed. Under [queue-model.md], the daemon's dispatch input is the in-memory queue loaded from `.harmonik/queue.json` or arriving via `queue-submit`. Idle (queue absent, completed, or paused) is simply "no active group is advancing." The daemon MUST NOT exit on queue absence or completion; this is the surviving load-bearing obligation. There is no re-query cadence knob. |

---

### 2.6 Cat 0 pre-check retry cadence — `cat0_retry_cadence_seconds`

| Field | Value |
|---|---|
| Spec source | [reconciliation/spec.md §4.3 RC-012], [process-lifecycle.md §4.2 PL-010] |
| Knob | Interval at which the daemon retries the Cat 0 infrastructure pre-check after a failure |
| Precedence layer | Operator-policy file ▷ default |
| Default value | **10 seconds** (per PL-010 / OQ-PL-002) |
| Allowed range | Positive integer (seconds); lower values increase infrastructure-probe load |
| Change-takes-effect | Next daemon start (read at startup; the retry loop runs until Cat 0 clears) |
| Notes | Cat 0 conditions: `br` CLI missing or timing out beyond 5s, Beads SQLite locked by a non-harmonik process, git index locked, `.harmonik/` unwritable, filesystem full. While Cat 0 persists the daemon emits `infrastructure_unavailable` and holds in `degraded` state; no in-flight run is classified. Post-`ready` Cat 0 failures do NOT re-enter `degraded` per RC-012a — they emit `daemon_degraded{reason=infrastructure_unavailable}` through the health probe only. |

---

### 2.7 Per-category reconciliation wall-clock budgets — `wall_clock_seconds` (per reconciliation YAML)

| Field | Value |
|---|---|
| Spec source | [reconciliation/spec.md §4.4 RC-017], [operator-nfr.md §4.11 ON-047] |
| Knob | `wall_clock_seconds` field in each reconciliation workflow's YAML policy (attached via `budget_ref` per CP-022) |
| Precedence layer | Per-workflow YAML policy (workflow definition) ▷ S01-shipped per-category default ▷ foundation fallback |
| Default value (foundation fallback) | **600 seconds** (10 min) per [operator-nfr.md §4.11 ON-047] |
| Default by category (S01-shipped) | Cat 2 (non-idempotent in-flight): **600 s** · Cat 3 generic (store disagreement): **300 s** · Cat 6a (integrity, LLM-triageable): **900 s** |
| Allowed range | Positive integer (seconds); no enforced maximum at spec level |
| Change-takes-effect | Next reconciliation dispatch (workflows loaded at dispatch time) |
| Notes | Budget exhaustion terminates the investigator subprocess and fires the default `escalate-to-human` verdict per RC-018. Budget-exhausted event carries `run_id`, `workflow_id`, `budget_seconds`, `elapsed_seconds` per [event-model.md §8.6.6]. Operators should size budgets per investigator complexity; Cat 6a's 900s default reflects its need for deeper LLM reasoning. |

---

### 2.8 Workflow mode — `workflow_mode`

| Field | Value |
|---|---|
| Spec source | [operator-nfr.md §4.1 ON-004a], [handler-contract.md §4.2 HC-006] |
| Knob | Mode applied to a dispatched run |
| Precedence layer | Per-task `workflow:<mode>` bead label ▷ per-project policy (reserved; not populated at MVH) ▷ daemon default ▷ built-in fallback |
| Default value | **`single`** (built-in fallback) |
| Allowed enumeration | `{single, review-loop, dot}` |
| Change-takes-effect | Per-task at claim time (resolved mode is sealed into Run record; immutable for run lifetime). Daemon default changes on next daemon start. |
| Notes | The iteration cap for `review-loop` mode is hardcoded at 3 for MVH and is NOT operator-tunable per ON-013d. There MUST NOT be a `harmonik set-mode` command or mid-run mutation surface. Operators change a per-task value by editing the bead's `workflow:<mode>` label via `br update` BEFORE claim. |

---

### 2.9 Credential injection source — `supervise_credential_source`

| Field | Value |
|---|---|
| Spec source | [operator-nfr.md §4.1 ON-004b], [credential-isolation.md §4.4 CI-006] |
| Knob | Source from which `harmonik supervise start` injects the credential into the Pi cognition (holder) process |
| Precedence layer | Explicit operator env export ▷ gitignored repo-root `.env` ▷ fail-closed |
| Default value | Gitignored repo-root `.env` (read only by `supervise start`; never read by the daemon; never unioned into a child env) |
| Allowed values | Explicit `ANTHROPIC_API_KEY` (or equivalent) env export; OR gitignored `.env` at repo root |
| Change-takes-effect | Next `harmonik supervise start` |
| Notes | If no source resolves, `supervise start` MUST fail-closed with an error — the holder process MUST NOT start unauthenticated. The daemon process and every daemon-spawned `claude` child MUST NOT receive the credential per CI-001. |

---

### 2.10 Per-day USD budget cap — `FLYWHEEL_BUDGET_USD_PER_DAY` / `--budget-usd-per-day`

| Field | Value |
|---|---|
| Spec source | [operator-nfr.md §4.1 ON-004c], [cognition-loop.md §4.11 CL-090] |
| Knob | Unified per-day spend cap summing Pi turns AND daemon-spawned `claude` session cost |
| Precedence layer | `--budget-usd-per-day` runtime flag ▷ `FLYWHEEL_BUDGET_USD_PER_DAY` env ▷ finite built-in default |
| Default value | **FINITE** (recommended **20 USD**; MUST NOT be unbounded by default) |
| Allowed range | Positive number (USD); the string `unlimited` or empty value is an explicit opt-out requiring intentional operator action |
| Change-takes-effect | Next daemon/loop start; the cap total resets at local-midnight day-boundary rollover |
| Notes | Mid-day tuning is NOT supported at v0.1. Cap exhaustion fires the budget-exhaustion handler-pause policy per [handler-pause.md §4 HP-012]; the operator clears via `harmonik supervise resume`. The 2026-05-30 API-key burn incident (hk flywheel auto-pull + unbounded cap) is the motivating incident for the mandatory-finite default. |

---

### 2.11 Per-day max-runs ceiling

| Field | Value |
|---|---|
| Spec source | [operator-nfr.md §4.1 ON-004d], [cognition-loop.md §4.11 CL-090a] |
| Knob | Maximum count of daemon `run_started` events since the last day-boundary rollover |
| Precedence layer | Runtime flag ▷ env var ▷ finite built-in default |
| Default value | **FINITE** (exact value is implementation-defined at v0.1; MUST NOT be unbounded) |
| Allowed range | Positive integer |
| Change-takes-effect | Next daemon/loop start; counter resets on the same day-boundary rollover as the USD total |
| Notes | This is the loss-proof backstop alongside the USD cap — it prevents run-count explosion even if per-run cost is negligible. Mid-day tuning is NOT supported at v0.1. Mirrors the precedence model of ON-004c. |

---

### 2.12 Pi judgment-model tiers — `FLYWHEEL_MODEL_TIER1` / `FLYWHEEL_MODEL_TIER2` / `FLYWHEEL_MODEL_TIER3`

| Field | Value |
|---|---|
| Spec source | [operator-nfr.md §4.1 ON-004e], [cognition-loop.md §4.11 CL-090b] |
| Knob | Model IDs the cognition loop uses per tier (tier-1 = fast/cheap; tier-3 = judgment/expensive) |
| Precedence layer | `FLYWHEEL_MODEL_TIER*` env override ▷ built-in default |
| Default values | Tier-1: **claude-haiku-4-5** (or current Haiku equivalent) · Tier-2: **claude-sonnet-4-6** · Tier-3: **claude-sonnet-4-6** (Opus is gated behind explicit operator opt-in — cost-posture default) |
| Allowed range | Any valid Anthropic model ID per tier |
| Change-takes-effect | Next loop start (wired only at the composition root per CL-100) |
| Notes | Operators upgrading tier-3 to Opus must set `FLYWHEEL_MODEL_TIER3=claude-opus-4-8` explicitly. Mid-session tier swap is NOT supported at v0.1. |

---

### 2.13 Daemon `claude` baseline model

| Field | Value |
|---|---|
| Spec source | [operator-nfr.md §4.1 ON-004f] |
| Knob | Default model the daemon's `claude` implementer/reviewer sessions run at |
| Precedence layer | Per-bead `model:` label (workflow definition) ▷ operator-facing daemon-baseline ▷ daemon built-in |
| Default value | **Sonnet/medium** (claude-sonnet-4-6 at v0.1) |
| Allowed range | Any valid model ID |
| Change-takes-effect | Next daemon start (MUST). Hot-reload of the baseline is a SHOULD, not a MUST. |
| Notes | The exact configuration surface (env name vs config field) is implementation-choice; ON-004f binds only the existence of a single operator-facing default, not its name. Per-bead `model:` labels override this at claim time and are sealed into the Run record. |

---

### 2.14 Dry-run / plan-only mode — `--dry-run`

| Field | Value |
|---|---|
| Spec source | [operator-nfr.md §4.1 ON-004g], [cognition-loop.md §4.11 CL-090d] |
| Knob | Daemon plan-only mode — previews intended spawn set without launching any `claude` or emitting spend |
| Precedence layer | Per-invocation flag |
| Default value | **OFF** (live) |
| Allowed values | Present (plan-only) / absent (live) |
| Change-takes-effect | Per invocation |
| Notes | When present, the daemon reports the intended spawn set (per bead: would-launch implementer + reviewer at model X, across M beads) without reading the credential source, spawning any subprocess, or emitting `run_started`. Mirrors the `harmonik queue dry-run` validate-without-execute behavior. |

---

### 2.15 Liveness heartbeat cadence — per-subsystem

| Field | Value |
|---|---|
| Spec source | [operator-nfr.md §4.9 ON-037] |
| Knob | Per-subsystem heartbeat emission interval and missed-heartbeat tolerance |
| Precedence layer | Operator-policy file ▷ per-subsystem built-in default |
| Default value | Per-subsystem (subsystem specs declare their own defaults; ON-037 requires each to declare one) |
| Allowed range | Positive duration (seconds) for interval; positive integer for tolerance (missed-heartbeat count before `degraded`) |
| Change-takes-effect | Next daemon start |
| Notes | Missing heartbeats beyond tolerance trigger `degraded` classification for the subsystem and raise the aggregated harmonik-wide health per ON-037. The aggregated health is exposed via `harmonik status` per ON-036. |

---

### 2.16 Redactor-panic escalation window — `T_redact_fail`

| Field | Value |
|---|---|
| Spec source | [operator-nfr.md §4.7 ON-022] |
| Knob | Window within which repeated redactor panics escalate the daemon to `degraded` |
| Precedence layer | Operator-policy file ▷ default |
| Default value | **60 seconds** |
| Allowed range | Positive integer (seconds) |
| Change-takes-effect | Next daemon start |
| Notes | A single redactor panic aborts the emission and emits `redaction_failed{event_type, run_id?, error_class}` but does NOT immediately escalate. Escalation to `degraded` fires only when the panic count within the rolling `T_redact_fail` window exceeds the threshold. The fail-closed redactor (ON-022) and the compile-time schema check (ON-023) together prevent secrets from reaching durable sinks. |

---

### 2.17 Attach status-snapshot cadence — `T_attach_status`

| Field | Value |
|---|---|
| Spec source | [operator-nfr.md §4.10 ON-050] |
| Knob | Interval at which `harmonik attach` emits a periodic `harmonik status` snapshot to the attached terminal |
| Precedence layer | Operator-policy file ▷ default |
| Default value | **10 seconds** |
| Allowed range | Positive integer (seconds); 0 disables the periodic snapshot |
| Change-takes-effect | Next attach session |
| Notes | The snapshot is the output of `harmonik status`; it does not constitute an additional event-bus emission. It exists so an attached operator can see daemon state without parsing the event stream. |

---

### 2.18 Background reconciliation scan cadence — `reconciliation_scan_cadence`

| Field | Value |
|---|---|
| Spec source | [reconciliation/spec.md §4.3] |
| Knob | Interval for the background divergence-detection scan that runs independently of the startup-reconciliation pass |
| Precedence layer | Operator-policy file (operator YAML) ▷ default |
| Default value | **Hourly** (3600 seconds) |
| Allowed range | Positive integer (seconds); post-MVH cadence tuning tracked in OQ-RC-004 |
| Change-takes-effect | Next daemon start |
| Notes | The startup-reconciliation pass (RC-020a) runs unconditionally at daemon startup before `ready`. This background scan is the ongoing daemon-lifetime complement — it re-classifies any store divergences that arise during normal operation. MVH default is hourly; workloads with high-frequency commits may benefit from a shorter interval. |

---

### 2.19 Resource budget category defaults

| Field | Value |
|---|---|
| Spec source | [operator-nfr.md §4.11 ON-047] |
| Knob | Foundation-level per-category defaults applied when no explicit policy override is present |
| Precedence layer | Per-node or per-role policy override ▷ foundation default (below) |
| Default values | See table |
| Change-takes-effect | Next daemon start (Budgets are registered at policy-load time) |

Default table (from ON-047):

| Category | Default | Scope |
|---|---|---|
| Token budget | **200,000 tokens** | Per-run, any agentic node |
| Wall-clock budget | **30 minutes** | Per-run, any agentic node |
| Iterations budget | **50 iterations** (tool-use cycles) | Per-run, any agentic node |
| Wall-clock budget (reconciliation) | **10 minutes** | Per reconciliation-workflow investigator run |
| Warning threshold | **80%** of budget | All categories |

Overrides are declared in the policy YAML per [control-points.md §4.5 CP-022] and resolve per the CP-037 precedence chain. The foundation defaults are the safe-state floor; operator policy SHOULD tune them per workload.

---

## 3. Ready-detection wait — `T_ready_wait`

| Field | Value |
|---|---|
| Spec source | [process-lifecycle.md §4.2 PL-009b] |
| Knob | Maximum time a CLI client retries the daemon socket probe before declaring the daemon unresponsive |
| Precedence layer | Operator-policy file ▷ default |
| Default value | **60 seconds** (tracked under OQ-PL-002) |
| Allowed range | Positive integer (seconds) |
| Change-takes-effect | Per CLI invocation (clients read this at probe time) |
| Notes | Client uses exponential backoff: initial 100 ms, max 2 s, capped at `T_ready_wait`. The daemon emits `daemon_ready` once startup and reconciliation complete; `T_ready_wait` is the client-side patience bound, not a daemon-side timeout. |

---

## 4. Precedence layer summary

For quick reference, the CP-037 layers in decreasing priority:

| Priority | Layer | Override form |
|---|---|---|
| 1 (highest) | Runtime override | CLI flag or env var at invocation |
| 2 | Operator-policy file | Persistent project-level YAML |
| 3 | Workflow definition | Per-workflow YAML or per-bead label |
| 4 (lowest) | Default | Built-in fallback shipped with harmonik |

A change to any layer takes effect at the next operator pause per CP-037, except:
- Per-invocation knobs (dry-run, T_ready_wait) take effect immediately at invocation.
- `T_attach_status` takes effect at the next attach session.
- Reconciliation workflow budgets take effect at the next dispatch.

---

## 5. Cross-references

- [operator-nfr.md §4.1 ON-004] — obligation that produced this inventory.
- [control-points.md §4.7 CP-037] — precedence-layer contract.
- [control-points.md §4.5 CP-022, CP-025] — Budget primitive and warning threshold.
- [event-model.md §4.4 EV-016] — fsync cadence and durability classes.
- [operator-nfr.md §4.7 ON-027, ON-029] — drain sequence and per-step timeout contract.
- [operator-nfr.md §4.8 ON-031, ON-032] — restart RTO targets and standard fixture.
- [reconciliation/spec.md §4.3 RC-012], [process-lifecycle.md §4.2 PL-010] — Cat 0 pre-check retry.
- [reconciliation/spec.md §4.4 RC-017] — per-category reconciliation budget defaults.
- [operator-nfr.md §4.11 ON-047] — resource budget category defaults.
- [operator-nfr.md §4.1 ON-004a–ON-004g] — workflow mode, credential, spend-governance knobs.
- [operator-nfr.md §4.9 ON-037] — liveness heartbeat cadence obligation.
- [operator-nfr.md §4.7 ON-022] — redactor panic escalation and T_redact_fail.
- [operator-nfr.md §4.10 ON-050] — T_attach_status.
- [process-lifecycle.md §4.4 PL-013 (retired)] — queue-empty re-query cadence (retired).

---

## 6. Open questions

**OQ-ON-001** (from operator-nfr.md): Location of this inventory — sibling file here (`specs/operator-nfr/config-inventory.md`) is the default-if-unresolved per OQ-ON-001. Migration to a top-level spec if the inventory grows beyond ~300 lines or serves multiple non-NFR owners.

**OQ-RC-004**: Post-MVH background reconciliation scan cadence tuning — whether hourly is too coarse for high-commit-rate workloads.

**OQ-ON-005b**: RTO nominal target relaxation vs fixture tightening — whether the 30s p95 nominal is achievable under realistic MVH loads.
