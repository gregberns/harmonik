# Change Design — `specs/operator-nfr.md` (C3 injection source + C6 model knobs + C7 dry-run; ON-004 inventory)

> Pass 4 (`change-design`) of the `credfence` spec work. Covers the operator-surface additions: the `supervise start` credential-injection source note (C3), the model-tier / daemon-baseline env knobs (C6), and the `--dry-run` flag (C7) — all landing as ON-004 config-inventory entries plus a §4.3 note. NORMATIVE output is `05-spec-drafts/operator-nfr.md`. Grounded in `03-research/launchspec-operator-nfr/findings.md §F2-F4` and `03-research/tooling-secret-dryrun/findings.md §F3/F5`, `02-components.md §2`, and the live spec (`specs/operator-nfr.md:157-175, 201-213`, re-verified 2026-05-31).

## Current state

- **ON-004 (line 159):** "The spec-draft pass MUST produce a normative config inventory enumerating every operator-configurable knob ... For each knob, the inventory MUST specify: the precedence layer (... per `control-points.md §4.7` CP-037), the default value, the allowed range or enumeration, and the change-takes-effect semantics ... At minimum the inventory covers the timer-flush cadence, budget warning threshold (`control-points.md §4.5`), drain timeout, RTO thresholds, Cat 0 retry cadence, per-Cat reconciliation budgets, and the `workflow_mode` knob." — the config-inventory obligation exists; the "at minimum" list does NOT yet include the credential source, the per-day budget cap, max-runs, the model tiers, the daemon baseline, or the dry-run flag.
- **ON-004a (line 165):** the worked example showing the per-knob inventory pattern (precedence / default / allowed values / change-takes-effect / runtime-tunability).
- **§4.3 (line 201):** operator-control semantics; defines `harmonik supervise`, the between-task invariant, pause-reason surfacing. §9 cross-refs already list `budget-paused` / `circuit-tripped` as pause-reasons supervise SHOULD surface.
- No `FLYWHEEL_MODEL_TIER*` knob exists; `router.ts:84` hardcodes `claude-opus-4-7-20260219` for tier-3 judgment, no env override (launchspec research F3). The daemon `claude` baseline is Go/per-project-config only, no single operator default (research R2 — config site not yet pinned). `supervise start` injects no credential.

## Target state

All additions are additive ON-004 inventory entries (the lowest-friction landing per research F2) plus one §4.3 note. NO new section, NO rewrite.

**Extend ON-004's "at minimum" list** to include the credfence knobs, each then specified as a full inventory entry per the ON-004a pattern:

- **ON-004b — credential injection source (C3).** Knob: the `supervise start` credential source. Precedence: explicit env export (`ANTHROPIC_API_KEY` in the operator shell) > gitignored `.env` read only by `supervise start` > error (no source → fail-closed). Default: gitignored `.env` at repo root (the sanctioned source, `credential-isolation.md` CI-006). Allowed values: a path-resolvable env file or an exported var. Change-takes-effect: next `supervise start`. Runtime-tunable: NO (boot-time). Cross-ref `credential-isolation.md` CI-005/CI-006.
- **ON-004c — per-day budget cap (C4 echo).** Knob: `FLYWHEEL_BUDGET_USD_PER_DAY` / `--budget-usd-per-day`. Precedence: runtime flag > env > finite default. Default: FINITE (recommended 20 USD; `cognition-loop.md` CL-090). Allowed values: positive number or `unlimited`/empty for explicit opt-out. Change-takes-effect: next daemon/loop start (or next day rollover for the reset). Cross-ref CL-090.
- **ON-004d — max-runs ceiling (C4 echo).** Knob: per-day max-runs. Precedence/shape mirror ON-004c. Default: a FINITE count (`cognition-loop.md` CL-090a). Cross-ref CL-090a.
- **ON-004e — Pi model tiers (C6).** Knob: `FLYWHEEL_MODEL_TIER1/2/3`. Precedence: env override > extension default. Default: tier-1 Haiku, tier-2 Sonnet, **tier-3 (judgment) Sonnet** (Opus gated behind explicit opt-in — the cost-posture default-flip, research F3, hk-rljho). Allowed values: any valid Anthropic model ID. Change-takes-effect: next loop start (composition root, `cognition-loop.md` CL-100). Cross-ref CL-090b.
- **ON-004f — daemon `claude` baseline (C6).** Knob: a single operator-facing default for the model the daemon's implementer/reviewer `claude` runs at (hk-c5oxy). Precedence: per-bead `model:` label (existing) > operator daemon-baseline default > built-in default. Default: the current built-in (Sonnet/medium). Allowed values: any valid model ID. Change-takes-effect: next daemon start (hot-reload is a SHOULD, NOT a MUST — research R3; avoid a config-watch subsystem). **Design note (research R2):** the daemon-baseline config site was not pinned in research; the spec text states the knob as an operator default and DEFERS the exact config wiring to implementation (the spec-level obligation is "one operator-facing default exists," not the literal env name) — flag for the spec-draft to keep this knob's normative text at the obligation level, not bound to an unverified literal.
- **ON-004g — dry-run flag (C7).** Knob: `--dry-run`/plan-only daemon mode (hk-cebjc). Precedence: runtime flag. Default: OFF (live). Behavior: previews intended spawns ("N implementers + N reviewers across M beads") WITHOUT launching `claude`, reading the credential source, or emitting spend (mirrors `harmonik queue dry-run` + orphan-sweep report-vs-act, research F3). Change-takes-effect: per invocation. Cross-ref `cognition-loop.md` CL-090d.

**§4.3 note.** Add: "`supervise start` injects the credential from a non-committed scoped source per `credential-isolation.md` CI-006; the `budget-paused` pause-reason (`cognition-loop.md` CL-090) is surfaced to the operator per §9, cleared via the existing handler-resume / `supervise resume` surface." (The pause-reason surfacing is already implied by §9; this note makes the budget-exhaustion path operator-visible.)

**Revision history:** add a 0.x row noting the credfence ON-004 entries.

## Rationale

- **ON-004 is the single home for ALL credfence knobs (research F2 KEY FINDING).** The config-inventory obligation already requires per-knob precedence/default/change-semantics; every credfence knob slots in as an inventory entry rather than a new config doc — the lowest-friction, no-new-surface landing matching the "additive" framing.
- **Default cheap, symmetric with the budget default-flip (research F3/F4).** Tier-3 judgment defaults to Sonnet (Opus opt-in); the budget defaults finite. Both are deliberate cost-posture changes — operators opt into the expensive path. Changelog flags both as intentional.
- **Hot-reload stays a SHOULD (research R3).** "Ideally hot-reload" for the daemon baseline must not creep into a config-watch subsystem (problem-space §3, no new infrastructure). The MUST is a single operator-facing default knob.
- **Daemon-baseline knob at obligation level (research R2).** The config site was not pinned in research; binding the spec to an unverified literal would risk drift. The spec states the obligation ("one operator default exists") and defers the wiring to implementation.
- These entries are the operator-nfr-side realization of `credential-isolation.md` Appendix A item (b) (injection source) plus the C6/C7 knobs cross-referenced from `cognition-loop.md`.

## Requirements traceability

| 02-components requirement | Goal (01 §2) | Target |
|---|---|---|
| C3 supervise-start injection source (SC3) | G3 | ON-004b + §4.3 note |
| C4 finite budget cap surfaced as operator knob (SC5) | G4/G5 | ON-004c |
| C4 max-runs surfaced as operator knob (SC4) | G4 | ON-004d |
| C6 Pi model-tier knobs (SC7) | G7 | ON-004e |
| C6 daemon baseline default (SC7) | G7 | ON-004f |
| C7 dry-run flag (SC8) | G8 | ON-004g |
| budget-paused operator-visibility (SC6 echo) | G6 | §4.3 note |

Every C3/C6/C7-in-operator-nfr requirement has a target; no target lacks a backing requirement. The normative budget/dry-run obligations live in `cognition-loop.md` (CL-090/090a/090d); operator-nfr carries the operator-facing KNOB form per ON-004. No contradiction with other areas.
