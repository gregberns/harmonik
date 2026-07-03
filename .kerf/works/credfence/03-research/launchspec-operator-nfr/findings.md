# Research — `claude-launchspec.md` + `operator-nfr.md` — Components C2 (note), C3 (note), C6 (knobs)

> Pass 3 (`research`) of the `credfence` spec work. Covers the additive launch-spec note recording the deny-list scrub as part of CHB-006 env assembly (C2), the operator-nfr notes for the credential-injection source (C3) and the model-tier / daemon-baseline knobs (C6). Grounded in incident assessment §"Genuinely lost" #4/#6 and the live specs (verified 2026-05-31). Planning artifact; does not modify `specs/`.

## Research questions

- **RQ1.** What does `claude-launchspec.md` §4 say about `baseEnv` / env assembly today, and where does the deny-list-scrub note slot in?
- **RQ2.** What does `operator-nfr.md §4.3` cover, and is there an existing config-inventory obligation the credential source + model knobs slot into?
- **RQ3.** What are the current hardcoded model IDs (Pi tiers + daemon claude baseline), and what env knobs exist vs are missing?
- **RQ4.** Does adding `FLYWHEEL_MODEL_TIER1/2/3` + a daemon-baseline knob break any existing contract?

## Findings

### F1 — `claude-launchspec.md §4` already documents `baseEnv` and CHB-006 env assembly; the scrub note is purely additive (RQ1)

- `claude-launchspec.md:108` (§4 field table): "`baseEnv` | Environment inherited from daemon `Config.HandlerEnv`. MUST already include `HARMONIK_PROJECT_HASH` per PL-006a. CHB-006 vars are appended (or overwrite) by `ClaudeEnvVars`. | nil slice is valid; results in an env built solely from CHB-006 vars." — This is the exact line the scrub note attaches to. It already names `ClaudeEnvVars` and the "appended (or overwrite)" assembly step.
- `claude-launchspec.md:54` and `:276` already reference a **"Forbidden-flag deny-list"** (`claude-hook-bridge.md §4.2 CHB-007`) — so the launch-spec spec ALREADY has a deny-list concept (for CLI flags). The credential **env** deny-list is a parallel, sibling concept; the note should be careful to distinguish "forbidden FLAGS (CHB-007)" from "scrubbed ENV keys (credfence deny-list)" to avoid conflation. **Flag for design: name the new env deny-list distinctly from the existing CHB-007 forbidden-flag deny-list.**
- The change is a single ADDITIVE note at the §4 `baseEnv` row / the env-assembly step (around the CHB-006 "5. Build ClaudeEnvConfig and call ClaudeEnvVars" step documented in `internal/daemon/claudelaunchspec.go:209`): "env assembly removes the credential deny-list keys (`credential-isolation.md`) from the constructed child env, symmetric with the existing `HARMONIK_SECRET_*` strip." No existing requirement changes; the launch-spec stays consistent with the new `credential-isolation.md` contract.

### F2 — `operator-nfr.md §4.3` is the operator surface; ON-004 config-inventory is the home for BOTH the credential source AND the model knobs (RQ2) — KEY FINDING

`operator-nfr.md` §4.1/§4.3 carry two obligations that directly absorb C3 and C6:
- `operator-nfr.md:157` **ON-004 — Config inventory obligation:** "The spec-draft pass MUST produce a normative config inventory enumerating every operator-configurable knob referenced across foundation specs. For each knob, the inventory MUST specify: the precedence layer (runtime override / operator-policy file / workflow definition / default, per `control-points.md §4.7 CP-037`), the default value, the allowed range or enumeration, and the change-takes-effect semantics." — Every credfence knob (credential source, `FLYWHEEL_BUDGET_USD_PER_DAY`, max-runs, `FLYWHEEL_MODEL_TIER1/2/3`, daemon claude baseline) is an ON-004 inventory entry. C3 and C6 should write these as inventory rows, not invent a new config doc.
- `operator-nfr.md §4.3` (the operator-control surface): defines `harmonik supervise` semantics (pause/upgrade complete in-flight runs per the between-task invariant, §4.3 glossary), and §9 cross-refs already list `budget-paused`/`circuit-tripped` pause-reasons supervise SHOULD surface. So §4.3 is where "`supervise start` injects `ANTHROPIC_API_KEY` from a non-committed scoped source" (C3) and "the `budget-paused` reason surfaced to the operator" (C5 echo) belong as additive notes.
- `operator-nfr.md:174` ON-004a shows the inventory pattern (per-knob fields: precedence, default, change-takes-effect) — credfence's knobs follow this exact shape.

**Conclusion:** C3 and C6 require NO new spec surface in operator-nfr — they are additive ON-004 inventory entries + a §4.3 note. This matches the decomposition's "additive" framing and is the lowest-friction landing.

### F3 — Model IDs hardcoded in `router.ts`; daemon claude baseline is Go/config-only; no `FLYWHEEL_MODEL_TIER*` env knob exists (RQ3)

- `.pi/extensions/flywheel/router.ts:84` (Tier 3 judgment): hardcoded `model: "claude-opus-4-7-20260219"` (Opus). Tier 1 -> `claude-haiku-4-5-20251001`; Tier 2 -> `claude-sonnet-4-6-20251022`. There is **no env override** — the IDs are literals in `prepareNextTurn`. The assessment (§"Genuinely lost" #6) flags this: "Pi judgment model hardcoded to opus + no runtime override; no `FLYWHEEL_MODEL_TIER*` env knob symmetric with the budget knob."
- The Pi tier vocabulary already exists (tiers 1/2/3 in `router.ts`), so `FLYWHEEL_MODEL_TIER1/2/3` env overrides map cleanly onto the existing tier structure. **Design: default tier-3 (judgment) to Sonnet, gate Opus behind explicit opt-in** (hk-rljho) — i.e. flip the `router.ts:84` literal to read `process.env["FLYWHEEL_MODEL_TIER3"] ?? "claude-sonnet-..."`.
- The daemon's claude baseline (the model implementer/reviewer claude runs at) is set Go-side / per-project config with no hot-reload (assessment #6). C6's "single operator-facing default for the daemon's claude baseline" (hk-c5oxy) is a NEW env/config knob symmetric with the Pi knob. **Flag for design: locate the daemon claude-model config site; expose ONE operator default (env or config), hot-reload is a SHOULD not a MUST (assessment says "ideally hot-reload").**

### F4 — Adding the model knobs is additive + consistent with the budget-knob pattern (RQ4)

- `FLYWHEEL_BUDGET_USD_PER_DAY` and `FLYWHEEL_CIRCUIT_THRESHOLD_PER_MIN` already establish the `FLYWHEEL_*` env-knob convention in `index.ts:62-66`. `FLYWHEEL_MODEL_TIER1/2/3` is the same pattern — no new mechanism, just three more env reads in `activate()` threaded into `router.ts`.
- No existing spec asserts the model IDs are fixed; `cognition-loop.md` CL-100 (composition root) already says the root "may wire ... budget tracker" etc. — model selection is a composition-root concern, so an env override there is idiomatic. **No contract break.**
- The default-flip (Opus -> Sonnet for judgment) is a deliberate cost-posture change, like the budget default-flip — operators who want Opus opt in. Changelog notes it as intentional.

## Patterns to follow

- **Additive notes only.** `claude-launchspec.md` gets ONE env-assembly scrub note at the §4 `baseEnv` row; `operator-nfr.md` gets ON-004 inventory rows + a §4.3 note. No new sections, no rewrites.
- **Reuse ON-004 config-inventory** as the single home for ALL credfence knobs (credential source, budget cap, max-runs, model tiers, daemon baseline) — F2.
- **Reuse the `FLYWHEEL_*` env-knob convention** (F4) for the model tiers.
- **Distinguish the new credential ENV deny-list from the existing CHB-007 forbidden-FLAG deny-list** (F1) to avoid conflation.
- **Default cheap** (Sonnet judgment, Opus opt-in) symmetric with the finite-budget default-flip.

## Risks / conflicts

- **R1 (naming conflation, F1).** `claude-launchspec.md` already has a "deny-list" (forbidden CLI flags, CHB-007). The new credential env deny-list must be named distinctly (e.g. "credential env deny-list" vs "forbidden-flag deny-list") in the launch-spec note, or a reader conflates two different deny-lists.
- **R2 (daemon-baseline config site unknown, F3).** The daemon claude-model baseline config site was not pinned in this pass (the knob is Go/per-project, not a single obvious literal like `router.ts:84`). Design must locate it before specifying the C6 daemon-baseline knob. **Carry to design.**
- **R3 (hot-reload scope).** Assessment says "ideally hot-reload" for the daemon baseline. Hot-reload of a daemon config is a larger mechanism (config-watch + safe mid-run application). Keep hot-reload a SHOULD; the MUST is a single operator-facing default knob. Avoid scope-creep into a config hot-reload subsystem (problem-space §3 "no new infrastructure").
- **R4 (no conflict).** Both specs' changes are additive; no existing requirement is reversed. Backward-compatible.

## Open questions carried to design

- NEW (R2): locate the daemon claude-baseline config site (C6) before specifying the knob.
- NEW (R1): name the credential env deny-list distinctly from CHB-007's forbidden-flag deny-list in the launch-spec note.
- (R3): keep daemon-baseline hot-reload a SHOULD, not a MUST — confirm in design.
