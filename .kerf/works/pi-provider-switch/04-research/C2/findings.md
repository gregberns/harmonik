# C2 — Named-profile config + resolver — Research findings

Add a `profiles:` map of full `{provider, model, api_key_env, api_key_file,
base_url, api}` bundles to the pi config structs, shape-validated in
`cmd/harmonik/resolve_pi_config.go` respecting the depguard boundary.

## Research questions

1. Where exactly do the pi config structs live and how is the raw→typed split done?
2. Does the depguard boundary force profile validation into `cmd/harmonik`, and how
   is existing shape validation structured (value-opacity, no allowlist)?
3. What reusable validators already exist to apply per-profile?
4. How is the wire-format triple (provider+base_url+api) kept coherent today, and
   what must a profile do for the `api` default?
5. How are missing-key errors aggregated, and how should an unknown-profile
   reference fail?

## Findings

**Q1 — config structs.** Two layers in `internal/daemon/projectconfig.go`:
- raw YAML: `rawHarnessesPiConfig` (projectconfig.go:789-797) with
  `provider/model/api_key_env/api_key_file/base_url/api` + `fallback`
  (`rawHarnessesPiFallbackConfig` :779-783). Parent `rawHarnessesConfig`
  (:800-802) has only `Pi`.
- typed: `PiHarnessConfig` (projectconfig.go:823-854) with the same fields plus
  `Fallback PiFallbackConfig` (:807-814) and a `HasFallback bool` presence flag
  (:852-853). The `fallback` sub-block is PASSIVE (V1 no auto-failover, PI-072) —
  it is the structural precedent for a new nested map.
  **Add `Profiles map[string]PiProfileConfig` to both raw and typed** (a new
  `PiProfileConfig`/`rawHarnessesPiProfileConfig` carrying the full six-field
  tuple). An absent/empty map must be valid (default path unaffected).
  Note the `HasFallback` pattern: presence is tracked with an explicit bool
  because a zero struct is indistinguishable from "absent"; a profiles MAP needs
  no such flag (nil map = absent), which is simpler.

**Q2 — depguard boundary + validation shape.** `resolve_pi_config.go` explicitly
lives in `cmd/harmonik` (package `main`) BECAUSE it needs `daemon.PiHarnessConfig`
and depguard bans `internal/*` importing `internal/daemon`
(resolve_pi_config.go:14-17; it imports `internal/daemon` at :38, legal from
cmd/). **Profile validation MUST live here**, mirroring `ResolvePiConfig`
(resolve_pi_config.go:108-204). Value-opacity is enforced by
`validatePiModelShape` (resolve_pi_config.go:210-224): regex
`piModelShapeRe = ^[A-Za-z0-9._:/-]+$` (:46), ≤128 chars — SHAPE ONLY, never a
curated provider/model enum (PI-052/HC-055a; comment :206-209). No allowlist
anywhere. A profile's provider/model get the same shape check.

**Q3 — reusable validators.** All three per-field validators already exist and
should be looped over each profile:
- `validatePiModelShape(field, model)` (resolve_pi_config.go:210-224) — model shape.
- `base_url` shape: inline block resolve_pi_config.go:171-185 (`url.Parse`, non-empty
  Scheme+Host, ≤512 chars). Not yet a named helper — the change spec may extract it
  to a helper so profiles and the top-level block share one implementation.
- `api_key_file` readable+non-empty + `~`-expansion: resolve_pi_config.go:144-166
  (`expandHomePath` :228+, `os.ReadFile`, TrimSpace non-empty). Also inline today.
  **Recommendation for change spec:** extract the base_url and api_key_file blocks
  into small helpers so ResolvePiConfig and the per-profile loop call the same code
  (avoids drift between top-level and profile validation).

**Q4 — wire-format triple + api default.** The `api` field defaults to `"openai"`
at LAUNCH time inside `buildPiModelsJSON` when empty and base_url is set
(pilaunchspec.go:339-341, per constraint §3). `ResolvePiConfig` today does NOT
validate/normalize `api` ("API needs no validation", resolve_pi_config.go:170).
A profile with `base_url` set but `api` unset therefore resolves consistently by
inheriting the same launch-time default — the change spec should NOT bake `api`
into the profile at resolve time (that would diverge from the default path); leave
it empty and let buildPiModelsJSON default it, exactly as the top-level block does.

**Q5 — missing-key aggregation + unknown reference.** `PiConfigMissingError`
(resolve_pi_config.go:55-138) collects EVERY missing required key (never
first-only, :109-138), names the dotted yaml path (e.g. `harnesses.pi.provider`),
and points at `harmonik pi config --example`. Per-profile required keys should
aggregate into the same error with dotted paths like
`harnesses.pi.profiles.<name>.provider`. There is no `HasFallback`-style
"all-or-nothing" needed for profiles unless a profile is partially specified —
mirror the fallback rule (:122-132): if a profile block is present, its
provider/model/api_key_env are all required. An UNKNOWN profile reference is NOT a
concern of this component (C2 validates the map at config load); the fail-loud
unknown-reference check happens at C3 claim time against this validated map.

## depguard confirmation
- `resolve_pi_config.go` imports `internal/daemon` (:38) and is in `cmd/harmonik`
  → allowed. Any profile validation needing `daemon.PiProfileConfig` must stay
  here, NOT in `internal/daemon`. This is the same reason `ResolvePiConfig` lives
  here (mirrors resolve_keeper_config.go).

## Risks / open decisions for change spec
- **Extract-vs-inline** the base_url / api_key_file validators (recommended:
  extract, so profile + top-level share one implementation).
- **Where the profile map lives in YAML:** `harnesses.pi.profiles.<name>` is the
  natural path (sibling of `fallback`). Confirm no collision with the passive
  `fallback` block.
- **api_key_env required per profile?** Yes — the fail-closed credential invariant
  (constraint §2) means every profile must bind its own key env; treat it as a
  required per-profile key in the aggregated missing-key check.
- Value-opacity MUST be preserved: no provider enum, shape-only. Non-negotiable.
