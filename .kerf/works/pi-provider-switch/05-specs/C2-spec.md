# C2 — Named-profile config + resolver — Change Spec

**Component:** C2 (detailed). Define reusable named `{provider, model, api_key_env,
api_key_file, base_url, api}` bundles in project config and shape-validate them
without crossing the depguard boundary.

## Requirements (from 03-components.md C2)

1. Config accepts a map of named profiles, each a full tuple; an empty/absent map is
   valid (default path unaffected).
2. Each profile is validated **shape-only** — provider/model matched against
   `piModelShapeRe` `^[A-Za-z0-9._:/-]+$` ≤128; NO enumerated provider allowlist
   (value-opacity invariant PI-052/HC-055a preserved).
3. A profile with `base_url` set but `api` unset resolves `api` consistently with
   `buildPiModelsJSON`'s default (`"openai"`, `pilaunchspec.go:339-341`) — the
   wire-format triple stays internally coherent. **Do NOT bake `api` into the profile
   at resolve time** — leave it empty and let `buildPiModelsJSON` default it (matches
   the top-level block; research Q4).
4. Missing required keys within a profile aggregate into one `PiConfigMissingError`
   (never first-only; `resolve_pi_config.go:109-138`) naming the dotted yaml key
   `harnesses.pi.profiles.<name>.<field>` and pointing at `harmonik pi config
   --example`.
5. A referenced-but-undefined profile name is a fail-loud error — but that check is
   **C3's** concern at claim time. C2 only validates the map at config load; it never
   sees bead labels.

## Research summary (from 04-research/C2)

- Config structs are two-layer in `internal/daemon/projectconfig.go`: raw YAML
  `rawHarnessesPiConfig` (`:789-797`) and typed `PiHarnessConfig` (`:823-854`). The
  passive `fallback` sub-block (`PiFallbackConfig` `:807-814` + `HasFallback bool`
  `:852`) is the structural precedent. A profiles MAP needs no presence bool (nil map
  = absent — simpler than `HasFallback`).
- `resolve_pi_config.go` lives in `cmd/harmonik` (package `main`) specifically because
  it needs `daemon.PiHarnessConfig` and depguard bans `internal/*` importing
  `internal/daemon` (`resolve_pi_config.go:14-17`, imports `internal/daemon` at `:38`
  legally from `cmd/`). **Profile validation MUST live here.**
- Reusable validators already exist: `validatePiModelShape(field, model)`
  (`resolve_pi_config.go:210-224`); the `base_url` shape block (`:171-185`,
  `url.Parse` non-empty Scheme+Host ≤512); the `api_key_file` readable/non-empty +
  `~`-expansion block (`:144-166`, `expandHomePath`). The `base_url` and
  `api_key_file` blocks are inline today — **extract each to a small helper** so the
  top-level block and the per-profile loop share one implementation (avoids drift).
- `api` is NOT validated/normalized at resolve time today ("API needs no validation",
  `:170`); a profile inherits the same launch-time default.
- Missing-key aggregation: `PiConfigMissingError` (`:55-138`) collects every missing
  key. Mirror the fallback rule (`:122-132`): if a profile block is present, its
  provider/model/api_key_env are all required (aggregate, dotted paths).

## Approach

### Config structs (`internal/daemon/projectconfig.go`)

Add a per-profile struct and a `Profiles` map to both the raw and typed layers.

Raw layer (beside `rawHarnessesPiConfig`, `:789-797`):

```go
// rawHarnessesPiProfileConfig is one named profile under harnesses.pi.profiles.
// Full tuple; provider/model/api_key_env required (validated in resolve_pi_config.go).
type rawHarnessesPiProfileConfig struct {
    Provider   string `yaml:"provider"`
    Model      string `yaml:"model"`
    APIKeyEnv  string `yaml:"api_key_env"`
    APIKeyFile string `yaml:"api_key_file"` // OPTIONAL
    BaseURL    string `yaml:"base_url"`     // OPTIONAL
    API        string `yaml:"api"`          // OPTIONAL; defaulted at launch, not here
}
```

Add to `rawHarnessesPiConfig`:
```go
Profiles map[string]rawHarnessesPiProfileConfig `yaml:"profiles"` // OPTIONAL; pi-provider-switch
```

Typed layer (beside `PiHarnessConfig`, `:823-854`):

```go
// PiProfileConfig is one resolved named profile — a full switchable tuple.
// Same shape+opacity discipline as the top-level PiHarnessConfig fields.
type PiProfileConfig struct {
    Provider   string
    Model      string
    APIKeyEnv  string
    APIKeyFile string // expanded from ~ by ResolvePiConfig when set
    BaseURL    string
    API        string
}
```

Add to `PiHarnessConfig`:
```go
// Profiles are named switchable {provider,model,api_key_env,...} bundles,
// selected per-bead by a `profile:<name>` label (pi-provider-switch). Nil/empty
// map = no profiles defined (default path unaffected). Validated by ResolvePiConfig.
Profiles map[string]PiProfileConfig
```

The daemon config-decode path that populates `PiHarnessConfig` from
`rawHarnessesPiConfig` must copy the `Profiles` map (raw→typed) alongside the
existing fields. (Find the existing raw→typed mapper for `PiHarnessConfig` in
`projectconfig.go` and extend it; the six per-field copies already there get a map
copy — `APIKeyFile` expansion is done later in `ResolvePiConfig`, not here.)

### Resolver (`cmd/harmonik/resolve_pi_config.go`)

**Step 1 — extract two helpers** (refactor, behavior-preserving) so top-level and
per-profile validation share code:

- `validatePiBaseURL(field, baseURL string) error` — the `url.Parse` / non-empty
  Scheme+Host / ≤512 logic currently inline at `:171-185`. `field` is the dotted
  path for the error message.
- `resolvePiAPIKeyFile(field, apiKeyFile string) (expanded string, err error)` — the
  `expandHomePath` + `os.ReadFile` + `TrimSpace` non-empty logic at `:144-166`,
  returning the expanded path.

Rewrite the existing top-level `base_url` and `api_key_file` blocks in
`ResolvePiConfig` to call these helpers (byte-equivalent behavior — a C6 concern:
existing `resolve_pi_config_test.go` must stay green).

**Step 2 — per-profile validation loop** in `ResolvePiConfig`, after the top-level
validation completes. For each `name, prof := range cfg.Profiles`:

1. **Missing-key aggregation** (into the SAME `missing []string` slice feeding
   `PiConfigMissingError`, mirroring `:109-138`): if `prof.Provider == ""` append
   `harnesses.pi.profiles.<name>.provider`; same for `model`, `api_key_env`. (Because
   the map key's presence means the profile block is present, all three are required
   — the same rule as the fallback block at `:122-132`.)
2. **Shape validation** (opacity — shape only, NO provider allowlist): call
   `validatePiModelShape("harnesses.pi.profiles.<name>.model", prof.Model)` AND
   `validatePiModelShape("harnesses.pi.profiles.<name>.provider", prof.Provider)`.
   (Provider gets the same regex — shape only. Never enumerate providers.)
3. **base_url**: if `prof.BaseURL != ""`, call
   `validatePiBaseURL("harnesses.pi.profiles.<name>.base_url", prof.BaseURL)`.
4. **api_key_file**: if `prof.APIKeyFile != ""`, call
   `resolvePiAPIKeyFile("harnesses.pi.profiles.<name>.api_key_file", prof.APIKeyFile)`
   and store the expanded path back into the resolved profile.
5. **api**: leave untouched (no validation; defaulted at launch by
   `buildPiModelsJSON`). Requirement 3 — do NOT normalize here.

The resolved profiles map (with expanded `APIKeyFile` paths) is written back onto
the returned `daemon.PiHarnessConfig.Profiles`. All missing keys across the top-level
block AND every profile aggregate into ONE `PiConfigMissingError`.

Ordering note: keep the missing-value gate first (aggregate top-level + all profiles'
missing keys, return before any shape/file/url check) to preserve the "aggregate ALL
missing keys before any other failure" contract at `:135-138`.

## Files & changes

| File | Change |
|------|--------|
| `internal/daemon/projectconfig.go` | Add `rawHarnessesPiProfileConfig` struct + `Profiles` field on `rawHarnessesPiConfig` (`:789-797`); add `PiProfileConfig` struct + `Profiles map[string]PiProfileConfig` on `PiHarnessConfig` (`:823-854`); extend the raw→typed mapper to copy the map. |
| `cmd/harmonik/resolve_pi_config.go` | Extract `validatePiBaseURL` + `resolvePiAPIKeyFile` helpers from the inline blocks (`:144-166`, `:171-185`); rewrite top-level blocks to call them; add the per-profile validation loop (missing-key aggregation + shape + base_url + api_key_file expansion). |

## Acceptance criteria

1. A config with a valid `harnesses.pi.profiles` map (both a cloud openrouter profile
   AND an ornith profile with `base_url` + `api: openai-completions`) resolves without
   error; the returned `PiHarnessConfig.Profiles` carries both, with the ornith
   profile's `api_key_file` (if set) expanded.
2. A profile with a shape-invalid provider or model (e.g. containing a space) is
   rejected with a `PiConfigError` naming `harnesses.pi.profiles.<name>.provider`
   (or `.model`).
3. A profile missing `provider`, `model`, or `api_key_env` aggregates into a single
   `PiConfigMissingError` whose `Missing` slice contains the dotted path(s); when the
   top-level block ALSO has a missing key, both appear in the same error.
4. An absent/empty `profiles:` map resolves exactly as today (default path
   unaffected; `resolve_pi_config_test.go` stays green).
5. A profile with `base_url` set and `api` unset leaves `api == ""` in the resolved
   profile (defaulted later at launch to `"openai"`).
6. `golangci-lint run` (depguard) stays green — no `internal/*` imports
   `internal/daemon`.

## Verification

- `go test ./cmd/harmonik/ -run TestResolvePiConfig` — all cases below pass.
- `golangci-lint run ./...` — depguard clean.
- `go build ./...`.

## Tests to add / update (`cmd/harmonik/resolve_pi_config_test.go`)

Mirror the existing `TestResolvePiConfig_*` table style. New cases:

- `TestResolvePiConfig_ProfileMap_Valid` — two-profile map (openrouter cloud +
  ornith base_url/openai-completions) resolves; assert both profiles present with
  correct fields.
- `TestResolvePiConfig_Profile_OrnithShape` — ornith-shaped profile (base_url + api:
  openai-completions) validates; `api` stays `""` in the resolved profile.
- `TestResolvePiConfig_Profile_InvalidShape` — profile with a space in provider →
  `PiConfigError` naming `harnesses.pi.profiles.<name>.provider`.
- `TestResolvePiConfig_Profile_MissingRequiredKey_Aggregates` — profile missing
  `api_key_env` → `PiConfigMissingError` with dotted path; combined with a top-level
  missing key, both appear.
- `TestResolvePiConfig_Profile_APIKeyFile_Expanded` — profile with `~`-prefixed
  `api_key_file` pointing at a temp file → resolved profile carries the expanded
  absolute path; unreadable/empty file → fail loud.
- `TestResolvePiConfig_AbsentProfiles_DefaultUnchanged` — nil `profiles` map ⇒ result
  byte-identical to the no-profile case (C6 hand-off).

Reuse the existing helper for building a temp `api_key_file` (the pattern already in
`resolve_pi_config_test.go` for the top-level `api_key_file` case).

## Error handling / edge cases

- **Missing required key in a profile:** aggregated into `PiConfigMissingError`,
  dotted path, never first-only, points at `harmonik pi config --example`
  (`:63-92`).
- **Shape-invalid provider/model:** `PiConfigError` (fail loud), dotted path. NO
  provider allowlist — a valid-shaped unknown provider passes (opacity).
- **Unreadable/empty `api_key_file`:** fail loud via `resolvePiAPIKeyFile` (mirrors
  top-level `:144-166`).
- **Unknown profile reference:** NOT C2's concern — C2 validates the config map;
  the label→profile existence check is C3 (fail-loud at claim time).
- **`api` unset with `base_url` set:** legal; defaulted at launch. Do not error.

## Migration / backwards compatibility

Additive: the `Profiles` map field is new; absent map = zero value = today's exact
behavior. The two extract-to-helper refactors are behavior-preserving (guarded by the
existing `resolve_pi_config_test.go` suite staying green — C6).
