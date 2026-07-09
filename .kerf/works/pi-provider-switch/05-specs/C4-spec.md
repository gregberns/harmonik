# C4 — PiHarness.LaunchSpec tuple override — Change Spec

**Component:** C4 (mechanical). Give the five sibling fields the same
override-with-`h.*`-fallback shape `model` already has in `LaunchSpec`.

## Requirements (from 03-components.md C4)

1. When a `RunCtx` field is non-empty it is used; when empty the daemon-global `h.*`
   value is used (per field, mirroring `model` at `piharness.go:126-129`).
2. Wire-format coupling honored: when a bead selects a profile, provider + base_url +
   api are overridden together as the profile's unit (they arrive coupled from C3);
   no launch is produced with provider from `rc` but `api` from `h.*`.
3. When `apiKeyEnv` is overridden, the fail-closed allowlist strip (`buildPiEnv`,
   `pilaunchspec.go:391-471`) re-runs keyed on the NEW `apiKeyEnv`.
4. When `baseURL` is overridden on an initial turn, `buildPiModelsJSON` generates the
   models.json for the new endpoint; resume turns reuse the prior session's config.
5. Billing guard (`pilaunchspec.go:281-289`) refuses launch before agent_ready if the
   overridden provider's key is absent/empty.

## Research summary (from 04-research/C4)

- `PiHarness.LaunchSpec` (`piharness.go:125-156`). Only `model` has the override
  shape today: `model := h.model; if rc.Model != "" { model = rc.Model }`
  (`:126-129`, hk-oqlgw). The `piRunCtx` literal (`:130-143`) hard-reads the other
  five from `h.*`: `provider: h.provider` (`:134`), `apiKeyEnv: h.apiKeyEnv`
  (`:136`), `apiKeyFile: h.apiKeyFile` (`:137`), `baseURL: h.baseURL` (`:138`),
  `api: h.api` (`:139`).
- Plumbing below is tuple-complete: `piRunCtx` (`pilaunchspec.go:90-167`) carries
  every field; `buildPiLaunchSpec` validates + builds argv + models.json + env. No
  structural change once the tuple arrives populated.
- `buildPiEnv` is keyed on `apiKeyEnv` — override it and the allowlist strip re-runs
  against the new key. `buildPiModelsJSON` fires when `baseURL != "" &&
  priorSessionID == nil`. Billing guard refuses on absent key. All three adapt with
  no new code, provided `apiKeyEnv`/`baseURL`/`apiKeyFile` are threaded correctly.

## Approach

In `PiHarness.LaunchSpec` (`piharness.go:125-156`), give each of the five sibling
fields the identical `x := h.x; if rc.X != "" { x = rc.X }` shape that `model`
already has, reading the new C1 `RunCtx` fields, then feed the picked values into the
`piRunCtx` literal:

```go
model := h.model
if rc.Model != "" { model = rc.Model }
provider := h.provider
if rc.Provider != "" { provider = rc.Provider }
apiKeyEnv := h.apiKeyEnv
if rc.APIKeyEnv != "" { apiKeyEnv = rc.APIKeyEnv }
apiKeyFile := h.apiKeyFile
if rc.APIKeyFile != "" { apiKeyFile = rc.APIKeyFile }
baseURL := h.baseURL
if rc.BaseURL != "" { baseURL = rc.BaseURL }
api := h.api
if rc.API != "" { api = rc.API }

prc := piRunCtx{
    piBinary:       h.piBinary,
    workspacePath:  rc.WorkspacePath,
    beadID:         rc.BeadID,
    provider:       provider,
    model:          model,
    apiKeyEnv:      apiKeyEnv,
    apiKeyFile:     apiKeyFile,
    baseURL:        baseURL,
    api:            api,
    priorSessionID: rc.PriorSessionID,
    baseEnv:        rc.BaseEnv,
    runID:          rc.RunID,
}
```

(A small `pick(rcVal, hVal string) string` helper is acceptable and reduces
repetition; either form is fine.)

**No change** to `NewPiHarness`, the struct, `newHarnessRegistry`, or anything below
the `piRunCtx` literal. The daemon-global singleton stays the fallback source; the
harness is stateless-per-launch (no per-bead re-registration).

**Coupling invariant (state it, do not enforce mid-launch):** provider + base_url +
api arrive coupled from C3 (they come from one profile). C4 copies them through
together and MUST NOT introduce any per-field default that could split them. Because
C3 delivers the coupled triple, a partial split cannot arise in practice; a test
asserts the invariant.

## Files & changes

| File | Change |
|------|--------|
| `internal/daemon/piharness.go` (`LaunchSpec`, `:125-156`) | Add the five override-with-fallback branches (mirror `model` at `:126-129`); feed picked values into the `piRunCtx` literal instead of `h.*`. |

## Acceptance criteria

1. An `rc` with a populated tuple (provider/apiKeyEnv/baseURL/api/apiKeyFile) wins
   over `h.*`; the emitted argv + models.json + env reflect the `rc` values.
2. An empty `rc` tuple falls back to `h.*` per field (this is also the C6 default
   pin).
3. An ornith `rc` tuple (base_url + `api: openai-completions`) produces the loopback
   `.harmonik/pi-agent/models.json` (baseUrl + api) + correct argv.
4. An overridden `apiKeyEnv` re-runs `buildPiEnv`: only the selected provider's key
   is injected, all siblings emitted as `KEY=`; no sibling key leaks.
5. An overridden provider with a missing key ⇒ billing guard refuses launch before
   agent_ready.

## Verification

- `go test ./internal/daemon/ -run 'TestPiHarness'` — new + existing cases pass.
- `go build ./...`.

## Tests to add / update (`internal/daemon/pilaunchspec_test.go`)

Template: `TestPiHarness_BaseURL_ProductionPath_Present/_Absent/_APIOverride`
(`:703-862`). New cases:

- `TestPiHarness_LaunchSpec_RCTupleOverridesGlobal` — `h.*` = openrouter defaults,
  `rc` tuple = ornith → argv/models.json/env reflect ornith, not the global.
- `TestPiHarness_LaunchSpec_EmptyRCFallsBackToGlobal` — empty `rc` tuple → argv/env
  = `h.*` (shared with C6).
- `TestPiHarness_LaunchSpec_OverriddenAPIKeyEnv_StripsSiblings` — `rc.APIKeyEnv` set
  → only that key injected, siblings `KEY=`.
- `TestPiHarness_LaunchSpec_CoupledTriple_TravelTogether` — asserts provider+base_url+api
  in the emitted spec all come from `rc` (never a provider-from-rc/api-from-h split).

Use the dummy-key-file + injected-`piHome` / `skipBillingGuard` bypass
(`pilaunchspec.go:147-166`) to avoid a live key.

## Error handling / edge cases

- Missing overridden key → billing guard refusal (existing, `:281-289`).
- Resume turn (`priorSessionID != nil`) → no models.json regeneration even if
  `baseURL` overridden (initial-turn-only, `:300`); the resume argv carries neither
  provider nor model — the captured session binds them. C3 fixes the tuple per bead
  (not mid-session), so the resume env matches the initial turn's provider.
- Partial split guarded by the coupling invariant test.

## Migration / backwards compatibility

Empty `rc` tuple ⇒ per-field fallback to `h.*` ⇒ byte-identical to today (the C6
pin). No signature change to `LaunchSpec` (still `(rc RunCtx)`); only its body grows.
