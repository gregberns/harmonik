# SPEC — `pi-provider-switch`

**Status:** integration (kerf pass 6). This is the single self-contained normative
document an implementing agent reads first. It is assembled faithfully from the six
component change specs (C1–C6) plus the integration analysis; it adds no new
requirements and changes no locked decision.

---

## 1. Summary & scope

Pi is harmonik's alternative agentic harness. Its
`{provider, model, api_key_env, api_key_file, base_url, api}` tuple is already
parameterized through the `harnesses.pi` block in `.harmonik/config.yaml`, but that
switchability is **daemon-global and static** — one provider for the whole daemon,
change requires a config edit plus a daemon restart.

**This work makes Pi's provider AND model switchable per-bead**, threading the full
tuple through the same `RunCtx` seam `rc.Model` already rides, so a single fleet can
route bead A to OpenRouter and bead B to DGX/ornith concurrently — with NO Go source
change and no restart-per-switch. BOTH providers must work COMPLETELY (reach the
model, drive real `tool_calls` end-to-end): named explicitly, OpenRouter AND
DGX/ornith. With no override supplied, resolution yields today's
`openrouter`/`deepseek-v4-flash`/`OPENROUTER_API_KEY` behavior byte-for-byte.

### Non-goals
- No automatic cost/latency/quality-based provider routing or failover (the passive
  `fallback` block stays passive).
- No change to the Pi binary or Pi's own provider/model catalog (value-opacity
  preserved).
- No new provider integrations/wire-formats beyond what Pi + the existing `api`
  field handle (openai, openai-completions/ornith).
- No touching the Claude Code harness's `ResolveModelPreference`.
- No reworking credential storage beyond the existing `api_key_env` / `api_key_file`.

---

## 2. Ownership split (load-bearing)

- **chani** owns the provider-side capability — **C1–C4** — **plus the C5 corpus
  DESIGN/contract** (scenario definitions, the hermetic-vs-live boundary, export
  seams, the assertions each scenario must make). C6 rides with chani's work.
- **stilgar** owns the **C5 corpus GATE-WIRING**: scenario fixtures, §10.1
  conformance registration, assertion wiring into the deterministic gates, and the
  `scripts/scenario-gate.sh` pickup. stilgar wires against the contract chani
  designs; chani does not wire the gate.

---

## 3. Locked decisions (carry verbatim — do NOT reopen)

1. **Named-profile registry.** A config `harnesses.pi.profiles:` map of named
   `{provider, model, api_key_env, api_key_file, base_url, api}` bundles, selected
   per-bead by a `profile:<name>` bead label (mirroring the existing `model:` label
   path at `modelpreference.go:166`). Chosen over raw per-field bead overrides
   because the wire-format triple (provider+base_url+api) and the fail-closed
   credential invariant must travel as one atomic unit; raw per-field overrides
   fragment both.
2. **`model:` + `profile:` precedence — option (b): `model:` overrides ONLY the
   profile's model field.** When a bead carries BOTH a `profile:<name>` and a
   `model:<alias>` label:
   - `{provider, base_url, api}` PLUS credentials (`api_key_env`, `api_key_file`)
     come **atomically from the profile** and are NEVER split.
   - the `model:` label overrides ONLY the resolved model string (model is
     orthogonal to the coupled triple, so overriding it alone cannot produce a
     wrong-wire-format launch).
3. **C3 resolver runs AFTER `resolveHarnessAgentTypeQuiet`, keyed off
   `resolvedAgentType`.** The per-bead profile resolver resolves a pi tuple ONLY
   when `resolvedAgentType == core.AgentTypePi` (the single concrete pi harness
   constant — there is no "family" of pi types); for a claude/codex-resolved bead the
   tuple stays empty. Prerequisite: hk-pkugu claim-time harness-type resolution must
   be in place, or a claude tier-3 `sonnet` default leaks into the pi tuple exactly
   as the model leak did.
4. **Backward-compat byte-identical default.** No override ⇒
   `--provider openrouter --model deepseek/deepseek-v4-flash`,
   `api_key_env OPENROUTER_API_KEY`, no base_url, no models.json — byte-identical to
   pre-change, with the existing required-field / fail-closed refusal semantics
   unchanged.
5. **Value-opacity (PI-052/HC-055a) — NO provider allowlist.** Provider/model are
   validated shape-only (`piModelShapeRe` `^[A-Za-z0-9._:/-]+$`, ≤128); the switch
   never introduces an enumerated provider allowlist that would reject a valid Pi
   provider.

---

## 4. Architecture — the single data-flow seam

The provider tuple travels from config to the launched process through exactly one
path; each component owns one hop. The harness is stateless-per-launch (the
daemon-global singleton stays the fallback source; no per-bead re-registration).

```
config profiles: map            bead profile:<name> label
  (C2, validated in                (selects a profile)
   resolve_pi_config.go)                 |
        |                                  v
        +---------→ [C3 resolvePiProfile] runs AFTER resolveHarnessAgentTypeQuiet
                     (hk-pkugu), keyed off resolvedAgentType (pi family only)
                          |  writes tuple into claudeRunCtx (claudelaunchspec.go:50)
                          v
              harnessregistry.go:240-264  — projects claudeRunCtx → RunCtx literal
                          |  (C1 fields on handlercontract.RunCtx; zero-value = no-override)
                          v
              PiHarness.LaunchSpec (C4)  — rc.X override else h.X fallback, per field
                          |  (tuple arrives populated & coupled from C3)
                          v
              piRunCtx → buildPiLaunchSpec / buildPiEnv / buildPiModelsJSON
                          |  (ALREADY tuple-complete — no change below this line)
                          v
                     pi --mode json --provider .. --model ..  (+ models.json if base_url)
```

### Build order (dependency DAG: `C1→C3`, `C2→C3`, `C1→C4`, `{C3,C4}→C5`, `{C3,C4}→C6`)
0. **Prereq:** verify hk-pkugu (`resolveHarnessAgentTypeQuiet`,
   `workloop.go:3077-3082`) is in place.
1. **C1** (struct contract) — before C3/C4 use it.
2. **C2** (config + validation) — before C3 resolves profiles. May run parallel to C1.
3. **C4** (LaunchSpec override) — after C1; parallel to C2/C3.
4. **C3** (claim-time resolver) — after C1+C2, with hk-pkugu in place.
5. **C5 + C6** (proof) — after {C3, C4}.

---

## 5. C1 — RunCtx tuple contract

**File:** `internal/handlercontract/harness.go` (the `RunCtx` struct, after the
`Effort` field at line 143).

Add exactly five `string` fields, named to match the `PiHarness` struct field
spelling exported to Go-public form (so C4 reads as a straight copy), each with an
"empty ⇒ no override (harness-global default)" doc comment:

```go
// Provider is the per-bead Pi provider override (pi-provider-switch).
// Empty ⇒ no override (harness-global default from PiHarness.provider).
Provider string

// APIKeyEnv is the per-bead Pi credential env-var-name override.
// Empty ⇒ no override (harness-global default). Travels coupled with Provider.
APIKeyEnv string

// APIKeyFile is the per-bead Pi credential file-path override.
// Empty ⇒ no override (harness-global default).
APIKeyFile string

// BaseURL is the per-bead Pi endpoint override for local OpenAI-compatible
// endpoints. Empty ⇒ no override. Part of the coupled wire-format triple
// {Provider, BaseURL, API}.
BaseURL string

// API is the per-bead Pi wire-format override (e.g. "openai-completions").
// Empty ⇒ no override. Part of the coupled wire-format triple.
API string
```

**Requirements.** (1) Five new string fields, each documenting "empty ⇒ no
override". (2) Zero-value of every new field leaves existing harness behavior
unchanged. (3) No new import to `internal/handlercontract` (it stays a leaf).

**Acceptance.** `go build ./...` compiles; a struct-shape assertion test confirms
the five fields exist and are `string`; `go list -deps ./internal/handlercontract`
shows no new dependency.

**Test.** `internal/handlercontract/harness_runctx_tuple_test.go` —
`TestRunCtx_ProviderTupleFields_Exist`: keyed-literal RunCtx with all five set to
sentinels, assert readback. Compile-and-shape pin only.

**Migration.** Fully backward compatible — all construction sites use keyed struct
literals; new fields default to `""`.

---

## 6. C2 — Named-profile config + resolver

**Files:** `internal/daemon/projectconfig.go` (config structs);
`cmd/harmonik/resolve_pi_config.go` (validation — MUST live here because depguard
bans `internal/*` importing `internal/daemon`, and the resolver already lives in
`cmd/harmonik` for exactly this reason).

### Config structs (`internal/daemon/projectconfig.go`)
Raw layer (beside `rawHarnessesPiConfig`, `:789-797`):
```go
type rawHarnessesPiProfileConfig struct {
    Provider   string `yaml:"provider"`
    Model      string `yaml:"model"`
    APIKeyEnv  string `yaml:"api_key_env"`
    APIKeyFile string `yaml:"api_key_file"` // OPTIONAL
    BaseURL    string `yaml:"base_url"`     // OPTIONAL
    API        string `yaml:"api"`          // OPTIONAL; defaulted at launch, not here
}
```
Add `Profiles map[string]rawHarnessesPiProfileConfig \`yaml:"profiles"\`` to
`rawHarnessesPiConfig`.

Typed layer (beside `PiHarnessConfig`, `:823-854`):
```go
type PiProfileConfig struct {
    Provider   string
    Model      string
    APIKeyEnv  string
    APIKeyFile string // expanded from ~ by ResolvePiConfig when set
    BaseURL    string
    API        string
}
```
Add `Profiles map[string]PiProfileConfig` to `PiHarnessConfig`. Extend the raw→typed
mapper to copy the map (APIKeyFile expansion happens later in `ResolvePiConfig`, not
here).

### Resolver (`cmd/harmonik/resolve_pi_config.go`)
**Step 1 — extract two behavior-preserving helpers** so top-level and per-profile
validation share one implementation (avoids drift; guarded by
`resolve_pi_config_test.go` staying green — C6):
- `validatePiBaseURL(field, baseURL string) error` — the `url.Parse` / non-empty
  Scheme+Host / ≤512 logic inline at `:171-185`.
- `resolvePiAPIKeyFile(field, apiKeyFile string) (expanded string, err error)` — the
  `expandHomePath` + `os.ReadFile` + `TrimSpace` non-empty logic at `:144-166`.
Rewrite the existing top-level `base_url` and `api_key_file` blocks to call these.

**Step 2 — per-profile validation loop** in `ResolvePiConfig`, after top-level
validation, for each `name, prof := range cfg.Profiles`:
1. **Missing-key aggregation** into the SAME `missing []string` slice feeding
   `PiConfigMissingError` (mirror `:109-138`; because the map key's presence means
   the profile is present, provider/model/api_key_env are all required — same rule
   as the fallback block at `:122-132`): append
   `harnesses.pi.profiles.<name>.provider` / `.model` / `.api_key_env` as absent.
2. **Shape validation** (opacity — shape only, NO allowlist): call
   `validatePiModelShape("harnesses.pi.profiles.<name>.model", prof.Model)` AND
   `validatePiModelShape("harnesses.pi.profiles.<name>.provider", prof.Provider)`.
3. **base_url:** if set, `validatePiBaseURL("harnesses.pi.profiles.<name>.base_url",
   prof.BaseURL)`.
4. **api_key_file:** if set,
   `resolvePiAPIKeyFile("harnesses.pi.profiles.<name>.api_key_file", ...)` and store
   the expanded path back into the resolved profile.
5. **api:** leave untouched — do NOT normalize/bake at resolve time; leave empty and
   let `buildPiModelsJSON` default it to `"openai"` (matches the top-level block).

Keep the missing-value gate FIRST (aggregate top-level + all profiles' missing keys,
return before any shape/file/url check) to preserve the "aggregate ALL missing keys
before any other failure" contract at `:135-138`. Write the resolved profiles map
(expanded APIKeyFile paths) back onto `daemon.PiHarnessConfig.Profiles`.

**Requirements.** (1) Config accepts a map of named profiles; empty/absent map valid.
(2) Shape-only validation; NO provider allowlist (opacity). (3) `base_url` set but
`api` unset ⇒ `api` stays `""`, defaulted later at launch. (4) Missing required keys
aggregate into ONE `PiConfigMissingError` with dotted paths, pointing at
`harmonik pi config --example`. (5) Unknown-profile-reference is C3's concern (fail
loud at claim time), NOT C2 — C2 only validates the config map.

**Acceptance.** Two-profile map (openrouter cloud + ornith base_url/openai-completions)
resolves with both present and ornith's api_key_file expanded; shape-invalid
provider/model rejected with dotted-path `PiConfigError`; missing per-profile key
aggregates (combined with a top-level missing key, both appear); absent map resolves
exactly as today; `base_url`-set/`api`-unset leaves `api == ""`; `golangci-lint`
depguard stays green.

**Tests** (`cmd/harmonik/resolve_pi_config_test.go`, mirror `TestResolvePiConfig_*`):
`_ProfileMap_Valid`, `_Profile_OrnithShape`, `_Profile_InvalidShape`,
`_Profile_MissingRequiredKey_Aggregates`, `_Profile_APIKeyFile_Expanded`,
`_AbsentProfiles_DefaultUnchanged`.

---

## 7. C3 — Claim-time per-bead profile resolver (the crux)

**Files:** `internal/daemon/modelpreference.go` (or a new sibling
`pi_profile_resolve.go`); `internal/daemon/workloop.go` (`:3082-3091, 4052`);
`internal/daemon/claudelaunchspec.go` (`:21-50`);
`internal/daemon/harnessregistry.go` (`:240-264`).

### 7.1 Label constant + resolver
Add beside `labelPrefixModel` (`modelpreference.go:166`):
```go
// labelPrefixProfile is the label prefix for per-bead Pi provider-profile
// selection (pi-provider-switch). E.g. `profile:ornith-dgx`.
const labelPrefixProfile = "profile:"
```
Add `resolvePiProfile` mirroring `resolveModelField`'s collect→count pattern
(simpler — no tier cascade). Signature:
```go
func resolvePiProfile(
    ctx context.Context,
    beadLabels []string,
    agentType core.AgentType,
    piCfg PiHarnessConfig,
    bus handlercontract.EventEmitter,
    beadID string,
) (PiProfileConfig, error)
```
Logic:
1. **Harness gate (hk-pkugu, load-bearing).** If `agentType != core.AgentTypePi`,
   return the zero `PiProfileConfig` immediately — no lookup, no error (quiet,
   non-fatal; optionally emit an observability event but do NOT fail). This is the
   no-leak guard. The predicate is the concrete `core.AgentTypePi` constant (used at
   `harnessregistry.go:64`, `workloop.go:4094/4837`, `dot_cascade.go`), NOT a "family"
   test.
2. **Collect** all labels with `labelPrefixProfile` (mirror `:212-218`).
3. **Count:**
   - `== 1`: `name := strings.TrimPrefix(...)`. Look up `piCfg.Profiles[name]`.
     **Existence check (fail-loud):** absent name → return an error naming the
     unknown profile and the bead (the C2→C3 contract). Otherwise return the found
     `PiProfileConfig`. NAME value never re-validated (opacity).
   - `> 1`: conflict — `emitBeadLabelConflict` (reuse existing helper), treat as
     absent, return zero tuple (mirror `:225-232`).
   - `== 0`: absent, zero tuple, no event (mirror `:233`).

### 7.2 `model:` vs `profile:` precedence (LOCKED — encode exactly)
Option (b): `model:` overrides ONLY the profile's model field. After both
`resolvePiProfile` and `ResolveModelPreference` have run:
- `claudeRunCtx.provider   = profile.Provider`
- `claudeRunCtx.apiKeyEnv  = profile.APIKeyEnv`
- `claudeRunCtx.apiKeyFile = profile.APIKeyFile`
- `claudeRunCtx.baseURL    = profile.BaseURL`
- `claudeRunCtx.api        = profile.API`
- `claudeRunCtx.model`: precedence when a profile is present —
  (1) tier-1 `model:<alias>` label if exactly one present (overrides);
  (2) else `profile.Model`;
  (3) no profile → existing tier-2/2.5/3/4 `ResolveModelPreference` walk, unchanged.

The wire-format triple + credentials come atomically from the profile and are NEVER
split. When NO profile is present, model resolution is byte-identical to today (C6).

### 7.3 Claim-time wiring (`workloop.go:3082-3091, 4052`)
After `resolvedAgentType` (`:3082`) and `ResolveModelPreference` (`:3090`):
```go
resolvedProfile, profErr := resolvePiProfile(
    ctx, beadRecord.Labels, resolvedAgentType,
    deps.projectCfg.Harnesses.Pi, deps.bus, string(beadID),
)
if profErr != nil {
    // fail-loud: unknown profile reference. Route through the SAME claim-time
    // refuse-before-launch seam that CrossRepoUnsafeError / StartFromRefError use:
    //   deps.brAdapter.ReopenBead(ctx, deps.intentLogDir, deps.brTimeoutCfg,
    //       runID, reopenTID, beadID, profErr.Error())
    //   return
    // (workloop.go: see the CrossRepoUnsafeError block at ~:3110 and the
    // resolveParentCommit StartFromRefError block at ~:3140 for the exact shape:
    // stderr log + ReopenBead with the error as reason + early return.) The bead is
    // reopened (NOT left in_progress, NOT launched on an empty/wrong tuple). The
    // implementer MUST cite this seam. The C5 corpus adds an end-to-end test
    // asserting the WORKLOOP refuses to launch (no LaunchSpec built) for an
    // unknown-profile bead — not merely that the resolver returns an error.
}
```
Model coalescing (LOCKED precedence — encode via the single `hasSingleModelLabel`
approach; do NOT have `resolvePiProfile` return a coalesce flag, which would couple
the profile resolver to a `model:`-label concern it does not own): if
`resolvedProfile` is non-zero (`resolvedProfile != (PiProfileConfig{})`) AND the
tier-1 `model:` label was absent, set `resolvedModel = resolvedProfile.Model` before
it is seated. Detect "tier-1 model label absent" via a small
`hasSingleModelLabel(beadRecord.Labels) bool` helper (mirrors `resolveModelField`'s
exactly-one test; false for both 0 and >1 labels ⇒ coalesce to `profile.Model`). The
locked observable precedence is unchanged: `model:` overrides ONLY the profile's model
field; the wire-format triple + credentials stay atomic from the profile.

At `workloop.go:4052-4053`, seat the five tuple fields into `claudeRunCtx` beside
`model`/`effort`:
```go
model:      resolvedModel,   // now possibly profile.Model (see precedence)
effort:     resolvedEffort,
provider:   resolvedProfile.Provider,
apiKeyEnv:  resolvedProfile.APIKeyEnv,
apiKeyFile: resolvedProfile.APIKeyFile,
baseURL:    resolvedProfile.BaseURL,
api:        resolvedProfile.API,
```

### 7.4 `claudeRunCtx` struct (`claudelaunchspec.go:21-50`)
Add five fields beside `model`/`effort` (doc: "per-bead Pi provider tuple; empty ⇒
harness-global default (C4 fallback)"):
```go
provider   string
apiKeyEnv  string
apiKeyFile string
baseURL    string
api        string
```

### 7.5 Projection onto `RunCtx` (`harnessregistry.go:240-264`)
Add to the `handlercontract.RunCtx{...}` literal beside `Model: rc.model` (`:258`):
```go
Provider:   rc.provider,
APIKeyEnv:  rc.apiKeyEnv,
APIKeyFile: rc.apiKeyFile,
BaseURL:    rc.baseURL,
API:        rc.api,
```
BOTH the struct edit (7.4) and this projection edit are required — missing either =
the tuple never reaches `LaunchSpec`.

### 7.6 DOT-path survival (verify, likely no code change)
The DOT cascade (`dot_cascade.go:1274-1281`) re-resolves `node.Model`/`effort` per
node but does NOT touch the provider tuple; it rides `claudeRunCtx` unchanged. No DOT
code change is expected, but the C5 no-leak scenario MUST include a DOT-path variant
(locked C3-Q5) proving the tuple survives unclobbered. If the variant reveals a
reset, add a guard mirroring `nodeModelForHarness`.

**Acceptance.** (1) pi bead + defined `profile:ornith-dgx` → RunCtx carries the full
tuple end-to-end into LaunchSpec. (2) pi bead, no label → five fields empty (C4
fallback). (3) claude-resolved bead + `profile:` label → zero tuple, no error, no
claude tier-3 leak (hk-pkugu; C5 scenario 2). (4) `profile:X` + `model:Y` → X's
triple+creds AND model Y (triple stays atomic). (5) undefined profile → fail loud at
claim time via the `ReopenBead` refuse-before-launch seam; a C5 end-to-end test
asserts the workloop builds NO LaunchSpec for the unknown-profile bead (not merely
that the resolver errors). (6) `>1` `profile:` labels → `bead_label_conflict`, treated as absent.
(7) tuple survives DOT cascade (C5 DOT variant).

**Tests** (`internal/daemon`): `TestResolvePiProfile_LabeledBead_ResolvesTuple`,
`_UnlabeledBead_ZeroTuple`, `_ClaudeHarness_ZeroTuple`, `_UnknownProfile_FailLoud`,
`_MultipleLabels_Conflict`, `_ModelLabelOverridesProfileModelOnly`. Reuse the
`emitBeadLabelConflict` bus-capture pattern from the modelpreference tests.

---

## 8. C4 — PiHarness.LaunchSpec tuple override

**File:** `internal/daemon/piharness.go` (`LaunchSpec`, `:125-156`).

Today only `model` has the override shape (`model := h.model; if rc.Model != "" {
model = rc.Model }`, `:126-129`); the other five hard-read `h.*` at `:134-139`. Give
each the identical `x := h.x; if rc.X != "" { x = rc.X }` shape reading the new C1
`RunCtx` fields, then feed the picked values into the `piRunCtx` literal:
```go
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
// ...feed provider/apiKeyEnv/apiKeyFile/baseURL/api into the piRunCtx literal
// instead of h.*
```
(A small `pick(rcVal, hVal string) string` helper is acceptable.) **No change** to
`NewPiHarness`, the struct, `newHarnessRegistry`, or anything below the `piRunCtx`
literal — the daemon-global singleton stays the fallback source; the plumbing below
(`piRunCtx`, `buildPiLaunchSpec`, `buildPiEnv`, `buildPiModelsJSON`) is already
tuple-complete.

**Coupling invariant (state, do not enforce mid-launch):** provider + base_url + api
arrive coupled from C3 (one profile); C4 copies them through together and introduces
NO per-field default that could split them. A test asserts the invariant.

**Requirements.** (1) Non-empty `rc` field used; empty ⇒ `h.*`, per field. (2)
Wire-format coupling honored (no provider-from-rc / api-from-h split). (3) Overridden
`apiKeyEnv` re-runs the `buildPiEnv` fail-closed strip keyed on the NEW key — only
that provider's key injected, siblings `KEY=`. (4) Overridden `baseURL` on an initial
turn ⇒ `buildPiModelsJSON` generates models.json for the new endpoint; resume turns
reuse prior session's config (initial-turn-only, `:300`). (5) Billing guard
(`:281-289`) refuses launch before agent_ready if the overridden provider's key is
absent/empty.

**Acceptance.** rc tuple wins over `h.*`; empty rc falls back per field; ornith rc
(base_url + openai-completions) produces the loopback models.json + correct argv;
overridden apiKeyEnv strips siblings and injects only the selected key; missing key ⇒
refused.

**Tests** (`internal/daemon/pilaunchspec_test.go`, template
`TestPiHarness_BaseURL_ProductionPath_*`):
`_LaunchSpec_RCTupleOverridesGlobal`, `_LaunchSpec_EmptyRCFallsBackToGlobal`
(shared with C6), `_LaunchSpec_OverriddenAPIKeyEnv_StripsSiblings`,
`_LaunchSpec_CoupledTriple_TravelTogether`. Use the dummy-key-file + injected
`piHome` / `skipBillingGuard` bypass to avoid a live key.

**Edge:** resume turn (`priorSessionID != nil`) → no models.json regeneration even
if `baseURL` overridden; the captured session binds provider/model; C3 fixes the
tuple per-bead (not mid-session) so resume env matches the initial turn's provider.

---

## 9. C5 — Two-provider e2e corpus

Three new HERMETIC Go scenarios in `internal/daemon` (package `daemon_test`) at the
launch-spec / models.json / model_selected layer. There is NO in-test fake HTTP
provider; the corpus proves BOTH wire formats at the **argv / generated models.json /
injected env** layer without network, driving the REAL claim-time seam via
`ExportedRoutedLaunchSpecBuilder` (as `hk_pkugu_pi_launch_e2e_test.go` does).

**Billing bypass (hermetic, no live key):** dummy key FILE set as `APIKeyFile` (so
the PI-040 guard sees a non-empty key) + `t.Setenv("HOME", t.TempDir())` (so the
PI-042 `~/.pi/auth.json` check is a no-op). Each new test file needs a UNIQUE helper
prefix (`hkppsToolcalls`, `hkppsNoLeak`, `hkppsDgx`).

### Scenario 1 — `pi_toolcalls_per_provider_test.go` (prefix `hkppsToolcalls`)
Build a `PiHarnessConfig` with TWO profiles: `openrouter-cloud`
`{provider: openrouter, model: openrouter/<id>, api_key_env: OPENROUTER_API_KEY}` (NO
base_url); `ornith-dgx` `{provider:<p>, model:<p>/<id>, api_key_env: PI_KEY,
api_key_file:<dummy>, base_url: http://127.0.0.1:8551/v1, api: openai-completions}`.
Drive TWO beads through the real seam in ONE test (proving concurrent
A→OpenRouter / B→ornith, re-scope #1):
- **OpenRouter bead** (`profile:openrouter-cloud`): argv contains
  `--provider openrouter --model openrouter/<id>`, NO base_url, and NO models.json
  written (`os.Stat(<ws>/.harmonik/pi-agent/models.json)` errors).
- **ornith bead** (`profile:ornith-dgx`): argv `--provider <p> --model <p>/<id>` AND
  the generated `<ws>/.harmonik/pi-agent/models.json` contains the loopback `baseUrl`
  + `api: openai-completions`.

### Scenario 2 — `pi_no_tier3_leak_test.go` (prefix `hkppsNoLeak`; extends hk-pkugu)
- **No-label pi bead:** argv `--model` = the harness-global pi model (NOT `sonnet`),
  plus the adversarial counterfactual (re-drive with the claude-leaked model to prove
  the assertion is not vacuous; copy the `hk_pkugu` counterfactual pattern).
- **DOT-path variant (LOCKED C3-Q5):** drive the same no-label pi bead through
  `driveDotWorkflow` (or the exported DOT cascade seam) with a per-node `model=`
  attribute; assert the provider tuple rides the cascade UNCLOBBERED (the per-node
  DOT `model=` pin is dropped for the pi family; provider/base_url/api unchanged
  across node re-launches).

### Scenario 3 — `pi_dgx_reasoning_test.go` (prefix `hkppsDgx`; hk-4ir08)
An `ornith-dgx` reasoning-model profile bead: hermetically assert the loopback launch
spec + generated models.json (argv `--provider`/`--model`, models.json `baseUrl` +
`api: openai-completions`). Add a comment documenting that the actual reasoning +
`tool_calls` round-trip is a live-tunnel **operator canary**, not part of this
hermetic test.

### Export seam
`internal/daemon/export_test.go` — add `ExportedResolvePiProfile` (and, if needed, an
`ExportedNewHarnessRegistryWithPiProfiles` builder).

**Acceptance.** Scenario 1: both beads in one test emit argv + models.json matching
THEIR wire format. Scenario 2: no-label pi argv `--model` = harness-global pi model,
NOT `sonnet`; counterfactual fails on the leaked model; DOT variant shows tuple
unchanged. Scenario 3: ornith reasoning bead emits loopback spec + models.json; code
comment records the live-tunnel operator canary as the separate DoD proof. All three
hermetic (dummy-key-file + `HOME` temp-dir). `scripts/scenario-gate.sh` picks them up
automatically (internal/daemon always affected); NO YAML scenario added.

**Live-DGX operator canary (separate, NOT CI — DoD proof):** with the DGX loopback
tunnel up (`http://127.0.0.1:8551/v1`; srt blocks the LAN IP → loopback only), submit
an ornith-profile bead and an openrouter-profile bead and confirm each drives a real
`tool_calls` turn end-to-end. Recorded on the scenario-test bead. This bounds "e2e"
honestly: the hermetic corpus proves everything up to the launch spec the real turn
consumes; the model round-trip is the operator canary.

**Test beads to file** (record IDs back into this spec after `br create`; this spec
does not create beads): a `scenario-test`-labeled bead for the two-provider per-bead
launch, and an `exploratory-test`-labeled bead for the operator canary.

**Ownership:** chani designs this corpus (scenarios, hermetic-vs-live boundary,
export seams, assertions); **stilgar wires the gate** (fixtures, §10.1 conformance
registration, assertion wiring, `scenario-gate.sh` pickup).

---

## 10. C6 — Backward-compat regression pin

**File:** `internal/daemon/pilaunchspec_test.go` (or new
`pi_default_path_golden_test.go`). No product code changes — pure regression
protection proving C1–C4 did not disturb the zero-value path.

Build a `PiHarness` from a FIXED openrouter fixture (provider `openrouter`, model
`deepseek/deepseek-v4-flash`, api_key_env `OPENROUTER_API_KEY`, no base_url, no api —
NOT the live `.harmonik/config.yaml`, which is currently ornith/DGX, for
determinism), call `LaunchSpec` with an EMPTY `RunCtx` tuple (all five new fields
`""`), and assert `TestPiHarness_DefaultPath_ByteIdentical`:
- argv contains `--provider openrouter --model deepseek/deepseek-v4-flash`;
- NO base_url-driven models.json (`os.Stat(<ws>/.harmonik/pi-agent/models.json)`
  errors);
- the env strip injects ONLY `OPENROUTER_API_KEY` and emits every sibling provider
  key as `KEY=` empty-override.

**Green-must-stay-green suites** (pass unmodified, or only a behavior-preserving
keyed-literal edit if C1/C4 forces one): `internal/daemon/pilaunchspec_test.go`,
`cmd/harmonik/resolve_pi_config_test.go`,
`internal/daemon/harnessregistry_pi_hkf8u5j_test.go`,
`internal/daemon/pi_retain_on_failure_hkj6wm7_test.go`, plus the two leak tests
(`hk_pkugu`, `hk_lfrub`).

**Acceptance.** `TestPiHarness_DefaultPath_ByteIdentical` passes (golden argv + env +
no models.json); env strip injects only `OPENROUTER_API_KEY` (siblings `KEY=`); all
listed suites pass.

**Test beads to file** (after `br create`): a `scenario-test`-labeled default-path
byte-identical bead, and an `exploratory-test`-labeled unlabeled-bead bead.

---

## 11. Cross-cutting invariants (must hold across the whole seam)

1. **Error propagation — dotted paths.** C2 aggregates ALL missing required keys
   (top-level + every profile) into ONE `PiConfigMissingError`, each naming
   `harnesses.pi.profiles.<name>.<field>`, pointing at `harmonik pi config
   --example`; never first-only; missing-value gate first. C3's unknown-profile
   reference is a separate fail-loud claim-time error naming profile + bead.
2. **Fail-closed credential strip.** `buildPiEnv` injects only the selected
   `api_key_env`'s key, siblings `KEY=`; re-runs on an overridden `apiKeyEnv`; no
   sibling key leaks; billing guard refuses on absent/empty key.
3. **Value-opacity — NO provider allowlist.** Shape-only (`piModelShapeRe`, ≤128);
   NAME/model VALUE never value-validated at the claim hop (existence-only).
4. **Wire-format triple coupling.** `{provider, base_url, api}` + creds travel as one
   atomic unit from the profile; `model:` overrides only the model field; C4 never
   splits the triple.
5. **models.json initial-turn-only.** Generated only when `priorSessionID == nil`;
   the tuple is fixed per-bead (not mid-session); resume env matches the initial
   turn; the DOT cascade never touches the tuple.

---

## 12. Success-criteria traceability (every 01-problem-space criterion → component → change-spec section)

| 01-problem-space success criterion | Component(s) | Change-spec section |
|---|---|---|
| **#1** operator points Pi at a different provider+model by config/flag, NO Go source change; launched run demonstrably uses new `--provider`/`--model` | C2 (profile registry), C3 (per-bead resolve), C4 (LaunchSpec override), C1 (seam) | §6, §7, §8, §5 |
| **#2** switching provider carries correct `api_key_env`/`api_key_file`/`base_url`/`api`; missing-key run refused before launch; no other provider's key reaches child | C2 (atomic profile), C4 (fail-closed strip re-run, billing guard) | §6, §8; invariant §11.2/§11.4 |
| **#3** no override ⇒ exactly today's `openrouter`/`deepseek-v4-flash`/`OPENROUTER_API_KEY` (regression test pins it) | C6, C4 (zero-value fallback), C1 (zero-value invariant) | §10, §8, §5 |
| **#4** grain finer than daemon-global; `{provider,model,...}` tuple threads through `RunCtx` so concurrent runs aren't forced onto one provider (grain = per-bead) | C1 (RunCtx tuple), C3 (per-bead claim-time resolve) | §5, §7 |
| **#5** wire-format correctness exercised e2e for two providers with different `api` (native openai AND openai-completions/base_url), each producing valid models.json/argv + successful initial turn | C5 (scenario 1 hermetic + operator canary), C4 (models.json generation) | §9, §8 |
| **#6** all existing Pi harness tests still pass + new coverage for the switch path | C6 (green-must-stay-green suites), C5 (new coverage) | §10, §9 |

### Re-scope directives → components
| Re-scope directive (2026-07-08) | Component(s) | Section |
|---|---|---|
| **#1** per-bead selection; concurrent A→OpenRouter, B→ornith | C3, C5 scenario 1 | §7, §9 |
| **#2** BOTH OpenRouter AND DGX/ornith land completely (real `tool_calls`), not just plan | C5 (all three hermetic scenarios + the live-DGX operator canary DoD proof) | §9 |

### Goals (01-problem-space §Goals) → components
| Goal | Component(s) |
|---|---|
| G1 provider switchable like model, no source change | C1, C2, C3, C4 |
| G2 select provider+model tuple together (coherent unit) | C2 (bundle), C3, C4 (coupling) |
| G3 preserve DeepSeek/OpenRouter default | C6 |
| G4 grain finer than daemon-global (per-bead via RunCtx) | C1, C3 |

---

## 13. Rejected alternative (recorded for completeness)

**Raw per-field bead overrides** (provider / base_url / api / api_key_env each as its
own bead label, no profile indirection). Rejected: (1) fragments the wire-format
triple — a bead could set `provider` and forget `api`/`base_url` → wrong-protocol
launch; (2) fragments the fail-closed credential invariant — api_key_env
independently settable → mismatched key/provider pair; (3) multiplies per-bead label
surface and pushes validation onto the claim path. The named-profile registry makes
provider+base_url+api atomic by construction, binds credential to provider as one
validated unit, and validates once at config load (respecting depguard) while the
bead carries a single opaque name.
