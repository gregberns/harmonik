# SESSION — pi-provider-switch

Full spec pipeline authored in one session (crew chani, 2026-07-08), passes
`problem-space → analyze → decompose → research → change-spec → integration → tasks → ready`.

## Outcome
- **SPEC.md** is the normative reference: named-profile registry making Pi's
  `{provider, model, api_key_env, api_key_file, base_url, api}` tuple selectable
  **per-bead** via a `profile:<name>` label, with **both** OpenRouter AND
  DGX/ornith working end-to-end.
- Independent review of the assembled spec: **READY-TO-ADVANCE, 0 blockers**
  (3 should-fixes folded in — see `integration-review.md`).

## Locked decisions
1. Named-profile registry (`harnesses.pi.profiles:` map), per-bead `profile:` label.
2. `model:`+`profile:` precedence: `model:` overrides ONLY the profile's model
   field; the wire-format triple `{provider, base_url, api}` + creds stay atomic.
3. C3 claim-time resolver runs AFTER `resolveHarnessAgentTypeQuiet`, gated on
   `core.AgentTypePi` — inherits hk-pkugu discipline or the tier-3 model leak reopens.
4. Backward-compat: no override ⇒ byte-identical `openrouter`/`deepseek-v4-flash`.
5. Value-opacity preserved (shape-only validation, no provider allowlist).

## Ownership split (confirmed with stilgar)
- **chani** — provider-side capability: C1 (RunCtx tuple), C2 (profile config +
  resolver), C3 (claim-time resolver, crux), C4 (LaunchSpec override), C6
  (backward-compat pin) + C5 corpus DESIGN/contract.
- **stilgar** — C5 gate-WIRING: scenario fixtures, §10.1 conformance registration,
  assertion wiring, deterministic gates.

## Build order (load-bearing prereqs)
`hk-pkugu (merge first) → C3`; `C1 → C3/C4`; `C2 → C3`; `C3+C4 → C5-design/C6`;
`C5-design → C5-wiring → scenario`.

## Testing
- **Hermetic corpus** (CI, scenario-gate): launch-spec/models.json/model_selected
  layer — no live provider; billing guard bypassed via dummy key + `t.Setenv HOME`.
- **Operator canary** (DoD proof): real `tool_calls` round-trip on the DGX tunnel,
  kept SEPARATE from the hermetic gate.

Epic: **hk-m6uu2** (STEP 1 = hk-fdbhf green-in-daemon). Beads in `07-tasks.md`.
