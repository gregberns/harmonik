# C4 — PiHarness.LaunchSpec tuple override — Research findings

Mechanical component. Give the five sibling fields the same
override-with-`h.*`-fallback shape `model` already has in `LaunchSpec`.

## Research questions

1. What is the exact override-with-fallback shape to mirror, and which fields lack it?
2. Does the plumbing below `LaunchSpec` need any change once the tuple arrives?
3. Does the fail-closed credential strip and models.json generation adapt
   automatically when `apiKeyEnv`/`baseURL` are overridden?

## Findings

**Q1 — the shape + the gap.** `PiHarness.LaunchSpec` (piharness.go:125-156). Only
`model` has the override shape today: `model := h.model; if rc.Model != "" { model
= rc.Model }` (piharness.go:126-129, hk-oqlgw). The `piRunCtx` literal
(piharness.go:130-143) then hard-reads the other five from `h.*`:
`provider: h.provider` (:134), `apiKeyEnv: h.apiKeyEnv` (:136),
`apiKeyFile: h.apiKeyFile` (:137), `baseURL: h.baseURL` (:138), `api: h.api`
(:139). **Give each the identical `x := h.x; if rc.X != "" { x = rc.X }` shape**
reading the new C1 RunCtx fields (conventions §, 02-analysis:219). Struct +
`NewPiHarness` (piharness.go:48-101) stay the daemon-global fallback source; the
singleton from `newHarnessRegistry(piCfg)` (harnessregistry.go:47-68) is unchanged
— no per-bead re-registration (harness is stateless-per-launch).

**Q2 — plumbing below is tuple-complete.** `piRunCtx` (pilaunchspec.go:90-167)
already carries every field and `buildPiLaunchSpec` already validates + builds argv
+ models.json + env from them (analysis §3). No structural change once the tuple
arrives populated. Wire-format coupling is honored automatically because C3
delivers provider+base_url+api coupled as one profile unit — C4 just copies them
through together; it must NOT introduce any per-field default that could split them.

**Q3 — strip + models.json adapt automatically.** `buildPiEnv`
(pilaunchspec.go:391-471) is keyed on `apiKeyEnv` — override it and the allowlist
strip re-runs against the NEW key, emitting `KEY=` empty-overrides for all siblings
and injecting only the selected provider's key (constraint §2). `buildPiModelsJSON`
(pilaunchspec.go:338-368) fires when `baseURL != "" && priorSessionID == nil`
(:300) — an overridden base_url generates the new endpoint's models.json on the
initial turn; resume turns reuse the prior session (models.json initial-turn-only,
constraint §4). Billing guard (pilaunchspec.go:281-289) refuses launch pre-agent_ready
if the overridden provider's key is absent. All three adapt with no new code,
provided `apiKeyEnv`/`baseURL`/`apiKeyFile` are threaded correctly.

## Pattern to mirror / test to pin
- Mirror piharness.go:126-129 five more times (or refactor to a small per-field
  helper `pick(rcVal, hVal)`); resulting `piRunCtx` fields read the picked values.
- Tests (pilaunchspec_test.go style, e.g. TestPiHarness_BaseURL_ProductionPath_*
  at :703-813 are the template): rc-provided tuple wins over `h.*`; empty rc falls
  back to `h.*` (this is also the C6 default-path pin); ornith tuple
  (base_url + `openai-completions`) produces the loopback models.json + correct
  argv; overridden `apiKeyEnv` strips siblings and injects only the selected key;
  missing key ⇒ refused.

## Risks / open decisions
- **Partial-override guard:** if the change spec keeps per-field RunCtx fields
  (rather than an atomic profile struct on RunCtx), C4 could in principle receive
  provider from rc but api from h.* — the wrong-wire-format hazard (constraint §3).
  Since C3 delivers the coupled triple together from one profile, this cannot arise
  in practice, but the C4 spec should state the invariant (provider+base_url+api are
  set together or not at all) and a test could assert it.
- No decision needed beyond mirroring the existing shape.
