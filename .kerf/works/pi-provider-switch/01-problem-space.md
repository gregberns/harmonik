# Pass 1: Problem Space — `pi-provider-switch`

## Summary

Pi is harmonik's alternative agentic harness (an alternative to the Claude Code
harness). Its model, provider, credential, endpoint, and wire-format are already
parameterized through the `harnesses.pi` block in `.harmonik/config.yaml` and
resolved by `ResolvePiConfig` — the harness is NOT hardcoded to DeepSeek at the
Go-literal level (the only `deepseek/deepseek-v4-flash` string lives in the
config file, not in source). The real, felt limitation is that this
switchability is **daemon-global and static**: the provider, credential, base
URL, and wire-format are fixed for the whole daemon at boot, so changing which
model/provider Pi uses requires a config edit plus a daemon restart, and every
concurrent Pi run shares one provider. This matters now because the account is
near its weekly Claude usage cap, work is being offloaded to Pi, and the ability
to steer Pi's model/provider by cost, availability, and quality (including feeding
the research crews' model-selection findings) is strategically important. This
work makes Pi's model AND provider switchable at a finer grain than "edit config,
restart daemon," so the fleet can retarget Pi without a restart and (stretch)
select provider per queue/bead/run.

## Current state (grounded in code)

- **Config surface already exists.** `harnesses.pi` accepts `provider`, `model`,
  `api_key_env`, `api_key_file` (optional), `base_url` (optional), `api`
  (optional wire-format), and an optional passive `fallback` sub-block.
  - Raw config struct + fallback: `internal/daemon/projectconfig.go:767-802`.
  - Validation/resolution (required = provider/model/api_key_env; model VALUE is
    never validated — full provider/model range is selectable per the
    value-opacity invariant PI-052/HC-055a): `cmd/harmonik/resolve_pi_config.go:108-179`.
  - Live config values (the DeepSeek default): `.harmonik/config.yaml:165-181`
    (`provider: openrouter`, `model: deepseek/deepseek-v4-flash`,
    `api_key_env: OPENROUTER_API_KEY`); the commented ornith/DGX block
    (`api: openai-completions`, `base_url`) sits directly below it.
- **Launch plumbing.** `NewPiHarness(piBinary, provider, model, apiKeyEnv,
  apiKeyFile, baseURL, api)` at `internal/daemon/piharness.go:91` carries the
  resolved config into the harness; `buildPiLaunchSpec` /
  `internal/daemon/pilaunchspec.go:104-346` emits
  `pi --mode json --provider <prov> --model <prov/id>`, generates a `models.json`
  for a locally-hosted OpenAI-compatible endpoint when `base_url` is set
  (`buildPiModelsJSON`, `pilaunchspec.go:338-366`), and does allowlist credential
  stripping keeping only the selected provider's key (`buildPiEnv`,
  `pilaunchspec.go:391-459`; provider-key table `pilaunchspec.go:58-73`).
- **Per-run MODEL override already exists, but not per-run PROVIDER.**
  `PiHarness.LaunchSpec` honors `rc.Model` to let concurrent Pi runs target
  different models (`internal/daemon/piharness.go:120-129`, hk-oqlgw) — but
  `provider`, `apiKeyEnv`, `apiKeyFile`, `baseURL`, and `api` are still taken
  from the harness-global `h.*` fields (`piharness.go:134-139`). So today you can
  vary the model per run, but the provider/credential/endpoint are fixed
  daemon-wide.
- **The `fallback` block is passive.** V1 has no automatic failover — it exists
  "for operator convenience" only (`projectconfig.go:794-802`).

## Goals

1. Make Pi's **provider** switchable the way the model already is — without a
   source-code change, and (target) without a full daemon restart / not fixed to
   one provider for all concurrent runs.
2. Let the operator/fleet **select provider + model together** (a coherent
   `{provider, model, api_key_env, base_url, api}` tuple) by config or flag,
   because model IDs are provider-namespaced and credentials/wire-format are
   provider-specific.
3. Preserve today's DeepSeek-on-OpenRouter behavior as the default when no
   override is supplied (backward compatibility).
4. Provide a switch grain finer than daemon-global — at minimum a way to change
   the active provider that the research crews' model-selection work can drive.

## Non-goals

- Not building automatic cost/latency/quality-based provider **routing or
  failover** (the passive `fallback` block stays passive unless this work
  explicitly scopes activating it; default assumption: out of scope for V1).
- Not changing the Pi binary itself or Pi's own provider/model catalog — harmonik
  only selects among what Pi already supports (value-opacity invariant preserved).
- Not adding new provider integrations/wire-formats beyond what Pi + the existing
  `api` field already handle (openai, openai-completions/ornith).
- Not touching the Claude Code harness's model/effort resolution
  (`ResolveModelPreference`) — that is a separate, already-existing four-tier path.
- Not reworking credential storage/secret management beyond the existing
  `api_key_env` / `api_key_file` mechanism.

## Constraints (technical)

- **Wire-format differences are real and provider-specific.** ornith needs
  `api: "openai-completions"`, whereas cloud providers use bare `"openai"` /
  the native provider path with no `base_url`
  (`.harmonik/config.yaml:175-181`; `buildPiModelsJSON` default
  `pilaunchspec.go:338-340`). A provider switch must carry the correct `api`
  and `base_url` together, or Pi will talk the wrong protocol.
- **Credential handling is per-provider and fail-closed.** Each provider needs
  its own `api_key_env` (and optional `api_key_file`); `buildPiEnv` strips ALL
  other provider keys and injects only the selected one, and launch is refused if
  the selected key is absent/empty (`pilaunchspec.go:273-286`, `391-459`). Any
  switch mechanism must resolve to a complete, valid credential for the chosen
  provider before launch, and must not leak sibling-provider keys into the child.
- **`base_url` models.json is generated only on the initial turn**
  (`priorSessionID == nil`, `pilaunchspec.go:300`). A switch that changes
  provider mid-session must respect turn boundaries (resume turns reuse the prior
  session's models.json).
- **Config is decoded at daemon boot** and the remote worker registry is
  boot-only, so a config-only mechanism inherits "restart to take effect." A
  finer-grained (per-run/per-queue) switch must thread the tuple through the same
  `RunCtx` seam that `rc.Model` already uses, extending it to provider/credential/
  endpoint fields.
- **Backward compatibility:** absent any override, resolution MUST yield today's
  `openrouter` + `deepseek/deepseek-v4-flash` behavior byte-for-byte, and the
  existing required-field / fail-closed refusal semantics must not regress.
- **Value-opacity invariant (PI-052/HC-055a):** harmonik validates the *shape* of
  provider/model, never the *value* — the switch must keep Pi's full provider/
  model range selectable and must not introduce an enumerated provider allowlist
  that would reject a valid Pi provider.

## Success criteria (concrete, verifiable)

1. An operator can point Pi at a different provider+model (e.g. openrouter→ornith,
   or a different OpenRouter model) by supplying a config/flag value — **with no
   Go source change** — and a launched Pi run demonstrably uses the new
   `--provider`/`--model` (verifiable in the launch spec / retained pi-agent
   artifacts).
2. Switching provider automatically carries the correct `api_key_env`,
   `api_key_file`, `base_url`, and `api` for that provider; a run started against
   a provider whose credential is missing is **refused before launch** (existing
   fail-closed behavior preserved), and no other provider's key reaches the child.
3. With no override supplied, resolution produces exactly today's
   `openrouter` / `deepseek/deepseek-v4-flash` / `OPENROUTER_API_KEY` behavior
   (a regression test pins this).
4. The switch grain is finer than daemon-global: at minimum the active provider
   can be changed for subsequent runs without editing source, and the selected
   `{provider, model, ...}` tuple threads through the `RunCtx` seam so concurrent
   runs are not forced onto one provider (the exact grain — per-run / per-queue /
   per-bead — is fixed in the Analyze pass).
5. Wire-format correctness is exercised end-to-end for at least two providers with
   different `api` values (e.g. a native openai provider AND an
   `openai-completions`/`base_url` provider), each producing a valid `models.json`
   / argv and a successful initial turn.
6. All existing Pi harness tests (`pilaunchspec_test.go`,
   `pi_retain_on_failure_hkj6wm7_test.go`, `resolve_pi_config` tests) still pass,
   plus new coverage for the switch path.

## Open questions for the Analyze pass

- **Switch grain:** daemon-global-hot-reload vs per-queue vs per-bead/per-run
  tuple override — which does the fleet actually need to offload Claude and to let
  the model-selection crews steer Pi? (`rc.Model` per-run already exists; extend
  to the full tuple, or add a named-profile registry?)
- **Named provider profiles:** define reusable `{provider, model, api_key_env,
  api_key_file, base_url, api}` bundles in config and select one by name — vs
  passing raw fields at the switch point.
- **Restart-free activation:** is hot-reload of `harnesses.pi` in scope, or is
  per-run tuple threading (no restart needed by construction) the answer?
- **Fallback activation:** stays passive (non-goal) or gets promoted to real
  availability failover as part of "provider by availability"?
