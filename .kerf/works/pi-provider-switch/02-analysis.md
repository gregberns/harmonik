# Pass 2: Analyze — `pi-provider-switch`

Factual map of the territory. Locked scope (2026-07-08 re-scope): thread the full
`{provider, model, api_key_env, api_key_file, base_url, api}` tuple **per-bead**
through the same `RunCtx` seam `rc.Model` already uses, with BOTH OpenRouter AND
DGX/ornith reaching the model with real `tool_calls` end-to-end. Backward compat:
no override ⇒ today's `openrouter`/`deepseek-v4-flash` behavior byte-identical.

Every claim below points to a file:line read during this pass.

---

## The seam, end to end (how `rc.Model` reaches Pi today)

This is the exact path the tuple must ride. Model already rides it; provider and
the four sibling fields do NOT.

1. **Claim-time resolution.** `dispatchAndRun` (workloop.go) resolves the model
   once at claim time:
   - `resolveHarnessAgentTypeQuiet(...)` computes the harness up front
     (`workloop.go:3077-3082`) so the model default matches the real harness —
     this is the hk-pkugu (`codename:pi-model-leak`) fix. **Landmine:** the
     `agentType` passed to `ResolveModelPreference` MUST equal the harness that
     launch actually selects, or the claude tier-3 default (`sonnet`) seals into
     `rc.model` for a pi run (`workloop.go:3066-3076`). Any per-bead provider
     threading must resolve the provider tuple with the SAME harness-aware
     discipline — do not re-introduce a claude-shaped default leak.
   - `resolvedModel, resolvedEffort := ResolveModelPreference(...)`
     (`workloop.go:3083-3090`); `sdModel = resolvedModel` (`workloop.go:3091`).
2. **Model preference walk** — `ResolveModelPreference` (modelpreference.go:190),
   four-tier: tier-1 per-bead `model:<alias>` label (`labelPrefixModel = "model:"`,
   modelpreference.go:166, collected modelpreference.go:212-233) → tier-2 project
   config `LookupAgent` → tier-2.5 env `HARMONIK_CLAUDE_MODEL` → tier-3
   `defaultModelEntries` (claude-code=`sonnet`, modelpreference.go:156-159) → tier-4
   empty. **model VALUE is never validated here** (opacity; modelpreference.go:220-224).
3. **Into `claudeRunCtx`.** `resolvedModel` is assigned to the run context field
   `model:` at `workloop.go:4052`. `claudeRunCtx` is defined at
   claudelaunchspec.go:50 (the daemon-internal run-context struct; the
   canonical launch builder input).
4. **Builder routing.** `routedLaunchSpecBuilder` (harnessregistry.go:134) →
   `resolveHarness(...)` (harnessregistry.go:143) → `reg.ForAgent(agentType)`
   (harnessregistry.go:145). Claude delegates to `buildClaudeLaunchSpec`
   (harnessregistry.go:156-158); non-claude (codex/pi) goes through
   `buildCodexRoutedLaunchSpec` (harnessregistry.go:162, 200).
5. **`claudeRunCtx` → `handlercontract.RunCtx`.** `buildCodexRoutedLaunchSpec`
   copies fields into the public `RunCtx` literal (harnessregistry.go:240-264);
   `Model: rc.model` at harnessregistry.go:258. **This literal is the ONLY place
   `claudeRunCtx` is projected onto `RunCtx` for the pi path** — any new
   per-bead field must be added both to `claudeRunCtx` and to this literal.
6. **Harness LaunchSpec.** `PiHarness.LaunchSpec(rc RunCtx)` (piharness.go:125):
   `model := h.model; if rc.Model != "" { model = rc.Model }` (piharness.go:126-129,
   `hk-oqlgw`/`hk-`per-run override). It builds `piRunCtx` (piharness.go:130-143);
   **`provider`, `apiKeyEnv`, `apiKeyFile`, `baseURL`, `api` are ALL taken from
   `h.*`** (piharness.go:134-139), never from `rc`. This is the exact gap.
7. **Argv/env.** `buildPiLaunchSpec(prc)` (pilaunchspec.go:203) emits argv and env.

**`RunCtx` public struct** (handlercontract/harness.go:77-163): has
`Model string` (line 141) and `Effort string` (line 143). It has NO provider,
api_key_env, api_key_file, base_url, or api fields today. Extending it is the
crux of the change (a public contract — see Constraints).

---

## Affected areas (files)

| Area | File | Role in the change |
|------|------|--------------------|
| Public run-context contract | `internal/handlercontract/harness.go:77-163` | Add per-bead provider tuple fields alongside `Model` (line 141) |
| Pi harness struct + LaunchSpec | `internal/daemon/piharness.go:48-101, 125-156` | Currently reads provider tuple from `h.*`; must honor `rc.*` override with `h.*` fallback (mirror the `rc.Model` pattern at :126-129) |
| Pi launch spec builder | `internal/daemon/pilaunchspec.go:90-167, 203-327, 338-368` | `piRunCtx` already carries every field; already validates + builds argv/models.json/env. No structural change needed if the tuple arrives populated |
| Model-preference walk | `internal/daemon/modelpreference.go:190-256` | Template for a per-bead label-based resolver; may host a parallel `provider:`/profile resolver |
| Claim-time resolution | `internal/daemon/workloop.go:3063-3091, 4052` | Where `sdModel`/`resolvedModel` are resolved and placed into `claudeRunCtx`; a provider-tuple resolver would sit here with the same harness-aware discipline |
| Daemon-internal run ctx | `internal/daemon/claudelaunchspec.go:50` (`claudeRunCtx`) | Add tuple fields; populate at workloop.go and project at harnessregistry.go:240-264 |
| Builder → RunCtx projection | `internal/daemon/harnessregistry.go:240-264` | Copy new `claudeRunCtx` tuple fields into the `RunCtx` literal |
| Harness registry / config seam | `internal/daemon/harnessregistry.go:47-68` | `newHarnessRegistry` builds ONE `PiHarness` from daemon-global `piCfg`; the singleton stays the fallback source |
| Config resolver | `cmd/harmonik/resolve_pi_config.go:108-224` | Shape-only validation; a named-profile mechanism would validate a profiles map here |
| Config structs | `internal/daemon/projectconfig.go:779-854` | `rawHarnessesPiConfig` / `PiHarnessConfig`; a named-profile registry would add a `profiles:` map here |
| Live config | `.harmonik/config.yaml:171-195` | Currently ACTIVE = ornith/DGX (loopback tunnel); OpenRouter block commented above |
| effectiveModel helper | `internal/daemon/harnessregistry.go:70-86` | model-only today; a "which provider" analog may be wanted for `model_selected`/observability |
| Tests | `internal/daemon/pilaunchspec_test.go`, `cmd/harmonik/resolve_pi_config_test.go`, `internal/daemon/harnessregistry_pi_hkf8u5j_test.go`, `internal/daemon/hk_lfrub_dot_node_model_leak_test.go`, `internal/daemon/hk_pkugu_pi_launch_e2e_test.go` | Regression pins + new per-bead coverage |

---

## Area detail

### 1. `handlercontract.RunCtx` (public contract)

- Struct at harness.go:77-163. `Model string` (141) is the precedent: "resolved
  model alias; empty ⇒ no flag (tool default)." `PriorSessionID *string` (162)
  is the resume seam. `BaseEnv []string` (107) is the credential-strip input.
- It is a **published cross-package contract** (`internal/handlercontract`) consumed
  by every harness (`Harness` interface, harness.go:173). Adding fields is
  additive/back-compat as long as the zero value = "no override" (same discipline
  as `Model`'s "empty ⇒ default").

### 2. `PiHarness` + `LaunchSpec` (the gap)

- Struct fields piharness.go:48-80: `provider`, `model`, `apiKeyEnv`, `apiKeyFile`,
  `baseURL`, `api` — all daemon-global (set once by `NewPiHarness`).
- `NewPiHarness(piBinary, provider, model, apiKeyEnv, apiKeyFile, baseURL, api)`
  (piharness.go:91-101).
- `LaunchSpec` (piharness.go:125-156): **only `model` has the override-with-fallback
  shape** (`:126-129`). The other five fields hard-read `h.*` (`:134-139`). The
  minimal change is to give each field the same `if rc.X != "" { use rc.X }` shape,
  reading new `RunCtx` fields.
- **Coupling caution:** `provider`, `base_url`, and `api` must travel together
  (wire-format coupling — see Constraints). A partial override (e.g. provider set,
  api left as global) would produce a wrong-protocol launch. The override must be
  all-or-nothing per profile, or explicitly field-merged with care.

### 3. `buildPiLaunchSpec` / `piRunCtx` (already tuple-complete)

- `piRunCtx` (pilaunchspec.go:90-167) ALREADY carries every field per-launch:
  `provider` (104), `model` (108), `apiKeyEnv` (113), `apiKeyFile` (117),
  `baseURL` (123), `api` (130). **So the plumbing below the harness needs no new
  fields** — the tuple just has to arrive populated from `LaunchSpec`.
- Validation gates (pilaunchspec.go:204-239): `workspacePath`, `beadID`,
  `apiKeyEnv` required; on initial turn (`priorSessionID == nil`) `provider` and
  `model` required. Fail-closed with operator-facing "run `harmonik pi config
  --example`" errors.
- Argv (pilaunchspec.go:246-269): initial =
  `pi --mode json --provider <prov> --model <prov/id> "<seed>"`; resume =
  `pi --mode json --session <id> "<seed>"`. **Resume argv carries NEITHER provider
  NOR model** — the session already binds them (see turn-boundary constraint).
- `models.json` (pilaunchspec.go:300-317, `buildPiModelsJSON` :338-368): generated
  ONLY when `baseURL != "" && priorSessionID == nil` (initial turn only). Writes
  `<workspacePath>/.harmonik/pi-agent/models.json`, injects `PI_CODING_AGENT_DIR`
  into env only. `api` defaults to `"openai"` when empty (:339-341). `modelID` =
  substring after last `/` (:342-345).
- Billing guard (pilaunchspec.go:281-289, `runPiBillingGuard`): fail-closed
  pre-flight; absent/empty key ⇒ launch refused before agent_ready.

### 4. Credential handling — `buildPiEnv` (fail-closed allowlist strip)

- `buildPiEnv(baseEnv, apiKeyFile, apiKeyEnv)` (pilaunchspec.go:391-471).
- `piProviderCredentialKeys` table (pilaunchspec.go:58-74) + `*_API_KEY` suffix
  pattern (`isPiAPIKeyPattern`, :478-480) = allowlist strip: every provider key
  EXCEPT the selected `apiKeyEnv` is emitted as `KEY=` (empty override), then only
  the selected provider's key is injected (`:459-460`). The `KEY=` empty-override
  (not omission) is load-bearing against tmux's additive `-e` (`:448-453`).
- `resolvePiAPIKeyValue(apiKeyFile, apiKeyEnv)` (pilaunchspec.go:183-193): the ONE
  shared key resolver (file-first, env fallback) feeding BOTH `buildPiEnv` and the
  billing guard so they never disagree.
- PATH guarantee (pilaunchspec.go:442-446) — the `hk-6atjk`/`codename:pi-model-leak`
  fix: exec substrate fully replaces child env; without a PATH the pi shebang can't
  find node (exit 127). **A per-bead provider switch that changes `apiKeyEnv` must
  re-run this whole strip against the NEW selected key** — the strip is keyed on
  `apiKeyEnv`, so it already adapts if `apiKeyEnv` is threaded through correctly.

### 5. Config resolver + structs

- `ResolvePiConfig` (resolve_pi_config.go:108-204): aggregates ALL missing required
  keys into one `PiConfigMissingError` (:109-138); validates `api_key_file`
  readable+non-empty and expands `~` (:144-166); `base_url` shape (scheme/host,
  ≤512, :171-185); model **shape only** via `piModelShapeRe` `^[A-Za-z0-9._:/-]+$`
  ≤128 (:46, `validatePiModelShape` :210-224). Called from `cmd/harmonik/main.go:1081`
  (CLI validation) and threaded to the registry via `cfg.ProjectCfg.Harnesses.Pi`.
- Lives in `cmd/harmonik` (NOT `internal/daemon`) because depguard bans internal
  packages importing `internal/daemon` (resolve_pi_config.go:14-17). **A
  named-profile registry validated by the resolver must respect this boundary.**
- `PiHarnessConfig` (projectconfig.go:823-854) + raw `rawHarnessesPiConfig`
  (:789-797). `fallback` sub-block (`PiFallbackConfig` :807-814, `HasFallback`
  :853) is **passive** (V1 no auto-failover, PI-072) — a possible home for named
  profiles, or a new `profiles:` map alongside it.

### 6. Registry wiring (the singleton)

- `newHarnessRegistry(piCfg)` (harnessregistry.go:47-68) registers ONE `PiHarness`
  built from daemon-global `piCfg` (:55-63). Called at `workloop.go:1078` with
  `cfg.ProjectCfg.Harnesses.Pi`. This singleton stays the **fallback** source for
  the tuple; per-bead override rides `RunCtx` above it. No per-bead re-registration
  is needed (and would be wrong — the harness is stateless-per-launch by design).

---

## Existing constraints (must preserve)

1. **Value-opacity invariant (PI-052 / HC-055a).** Validate shape, never value:
   model regex `^[A-Za-z0-9._:/-]+$` ≤128 (resolve_pi_config.go:46, 210-224);
   `ResolveModelPreference` does not validate model value (modelpreference.go:220-224).
   A per-bead provider/model must NOT introduce an enumerated provider allowlist —
   Pi's full provider set must stay selectable.
2. **Fail-closed credential stripping (PI-021/PI-040/PI-050).** `buildPiEnv`
   allowlist-strips all sibling keys and injects only the selected key
   (pilaunchspec.go:391-471); billing guard refuses launch on absent/empty key
   (pilaunchspec.go:281-289). A provider switch must resolve a complete valid
   credential for the CHOSEN provider before launch and leak no sibling key.
3. **Wire-format coupling.** `provider` + `base_url` + `api` travel together
   (`.harmonik/config.yaml:189-195`: ornith needs `api: openai-completions` +
   loopback `base_url`; cloud openrouter needs NEITHER). `buildPiModelsJSON`
   defaults `api="openai"` (pilaunchspec.go:339-341). A partial override splits the
   protocol from the endpoint → wrong-wire-format failure.
4. **`models.json` initial-turn-only.** Generated only when
   `baseURL != "" && priorSessionID == nil` (pilaunchspec.go:300). Resume turns
   reuse the prior session's config; the resume argv carries no provider/model
   (pilaunchspec.go:256-261). **A per-bead switch changes provider PER BEAD, not
   mid-session** — this is naturally compatible (a bead's provider is fixed for its
   whole review loop), but any resume must reuse the SAME tuple the initial turn
   used, or the captured session binds to a different endpoint than the resume env.
5. **Backward-compat regression pins.** No override ⇒ byte-identical
   `openrouter`/`deepseek-v4-flash`/`OPENROUTER_API_KEY`. The zero-value-is-default
   discipline (like `rc.Model == "" ⇒ h.model`, piharness.go:127-129) is the
   mechanism. Existing tests that must stay green:
   `pilaunchspec_test.go`, `resolve_pi_config_test.go`,
   `harnessregistry_pi_hkf8u5j_test.go`, `pi_retain_on_failure_hkj6wm7_test.go`.
6. **hk-pkugu / pi-model-leak discipline.** The harness agent-type used to resolve
   defaults MUST match the launch harness (workloop.go:3066-3082). A provider-tuple
   resolver must inherit this — resolve against the resolved pi harness, not a
   claude-shaped default, or a claude tier-3 value leaks into the pi tuple.
7. **Public-contract additivity.** `RunCtx` (handlercontract) is cross-package;
   new fields must be additive with zero-value = no-override.
8. **depguard.** Config-resolution logic that needs `daemon.PiHarnessConfig` must
   live in `cmd/harmonik`, not `internal/*` (resolve_pi_config.go:14-17).

---

## Conventions to follow

- **Override-with-fallback pattern:** `x := h.x; if rc.X != "" { x = rc.X }`
  (piharness.go:126-129) — the exact shape to replicate for the five sibling fields.
- **Fail-loud config errors** name the dotted yaml key + point at
  `harmonik pi config --example` (pilaunchspec.go:213-238, resolve_pi_config.go:63-92).
- **Aggregated missing-key errors** (never first-only): `PiConfigMissingError`
  (resolve_pi_config.go:109-138).
- **Env injection only, never argv** for secrets and dir hints
  (`PI_CODING_AGENT_DIR` at pilaunchspec.go:316; api-key never `--api-key`,
  pilaunchspec.go:252-253).
- **Test naming:** `TestBuild<Thing>_<Scenario>` (pilaunchspec_test.go:33, 174, 351).
  Env-shape tests set `skipBillingGuard`/inject `piHome` to avoid needing a real key
  (piRunCtx fields pilaunchspec.go:147-166). Exported test seams:
  `ExportedNewPiHarness`, `ExportedRoutedLaunchSpecBuilder`, `ExportedClaudeRunCtx`
  (export_test.go). E2E launch pattern in `hk_pkugu_pi_launch_e2e_test.go` (real
  `routedLaunchSpecBuilder`) and per-node model-leak coverage in
  `hk_lfrub_dot_node_model_leak_test.go` (already constructs a per-run
  ornith/openai-completions PiHarness — a template for the two-provider e2e).

---

## Relevant recent git history (affected files)

- `9dc09433` fix(hk-6atjk): guarantee PATH in Pi exec child env (`pi-model-leak`) — buildPiEnv PATH fallback.
- `0af960c0` fix(pi-harness): `effectiveModel()` honors `rc.model` override for Pi.
- `60387c50` feat(pi-harness): **honor per-run `rc.Model` override like claude harness** (`hk-oqlgw`) — the direct precedent this work generalizes from model-only to the full tuple.
- `c10c193b` feat(pilot): **base_url/api passthrough** for local OpenAI-compatible endpoints (`hk-z13jz`) — added `baseURL`/`api` fields + `buildPiModelsJSON`.
- `c69dba6f` feat(pilot/hk-xmfoi): `api_key_file` file-first secret injection.
- `cbf764cb` / `a66bdf0c` feat(pilot/hk-f8u5j): wire `ResolvePiConfig` → `NewPiHarness` (config→harness seam).
- `116d126c` / `df039e0e` feat(pilot/hk-l1bkp): `pibillingguard.go` fail-closed guard.
- `4cb48a6e` feat(events): `model_selected` event at launch resolution.
- **hk-pkugu** (`codename:pi-model-leak`, the workloop.go:3066-3082 harness-aware
  resolution) — not on the pi files' log but the load-bearing constraint for the
  claim-time resolver; see `hk_pkugu_pi_launch_e2e_test.go`.

MEMORY corroboration (external context, not code): in-daemon Pi→ornith was a stack
of bugs (srt sandbox egress, DOT model-pin, empty PATH); the ACTIVE
`.harmonik/config.yaml` uses OPTION A (loopback SSH tunnel `base_url:
http://127.0.0.1:8551/v1`, `api: openai-completions`) because srt blocks the LAN IP.
The two-provider e2e (OpenRouter cloud + ornith/DGX loopback) must exercise BOTH
wire formats — bare `openai` (no base_url) AND `openai-completions` (base_url) — the
exact split `buildPiModelsJSON` and the argv path already encode.

---

## Summary of the change surface

The plumbing BELOW the harness (`piRunCtx`, `buildPiLaunchSpec`, `buildPiEnv`,
`buildPiModelsJSON`, the resolver, the config structs) is **already tuple-complete**
— every field exists and is exercised. The gap is a **three-hop threading**:
(1) add the five sibling fields to `handlercontract.RunCtx` (mirroring `Model`);
(2) resolve the per-bead tuple at claim time in workloop.go with hk-pkugu
harness-aware discipline and carry it in `claudeRunCtx`, projecting it in the
`RunCtx` literal at harnessregistry.go:240-264; (3) in `PiHarness.LaunchSpec`,
give the five fields the same override-with-`h.*`-fallback shape `model` already
has. The open mechanism choice (Analyze-level, not grain-level) is
**named-profile registry** (select a `{...}` bundle by name on the bead — a config
`profiles:` map validated in `resolve_pi_config.go`) vs **raw per-field bead
overrides**; the former better preserves the wire-format coupling (provider+base_url+api
travel as one unit) and the fail-closed credential invariant.
