# Pass 3: Decompose — `pi-provider-switch`

Locked scope (2026-07-08 re-scope): thread the full
`{provider, model, api_key_env, api_key_file, base_url, api}` tuple **per-bead**
through the same `RunCtx` seam `rc.Model` already rides, with BOTH OpenRouter AND
DGX/ornith reaching the model and driving real `tool_calls` end-to-end. No
override ⇒ today's `openrouter`/`deepseek-v4-flash` behavior byte-identical.

**Mechanism (decided — see Rejected Alternatives):** a config `profiles:` map of
named `{provider, model, api_key_env, api_key_file, base_url, api}` bundles,
selected per-bead by a bead label (mirroring the existing `model:` label path at
modelpreference.go:166), validated in `cmd/harmonik/resolve_pi_config.go` to
respect the depguard boundary. Chosen because the wire-format triple
(provider+base_url+api) and the fail-closed credential invariant must travel as
one atomic unit; raw per-field bead overrides fragment both.

The plumbing BELOW `PiHarness.LaunchSpec` (`piRunCtx`, `buildPiLaunchSpec`,
`buildPiEnv`, `buildPiModelsJSON`) is **already tuple-complete** — every field
exists and is exercised (analysis §3). The net change is a **three-hop threading**
plus the profile registry that feeds it and the test corpus that proves both
providers.

---

## Component overview

| # | Component | One-line responsibility |
|---|-----------|-------------------------|
| C1 | RunCtx tuple contract | Add the five sibling provider fields to the public `handlercontract.RunCtx` alongside `Model`, additive with zero-value = no-override |
| C2 | Named-profile config + resolver | `profiles:` map in config structs, shape-validated in `resolve_pi_config.go` respecting depguard |
| C3 | Claim-time per-bead profile resolver | Resolve the bead's profile label → tuple at claim time in `workloop.go` with hk-pkugu harness-aware discipline; carry in `claudeRunCtx`; project at the `RunCtx` literal |
| C4 | `PiHarness.LaunchSpec` tuple override | Give the five sibling fields the same override-with-`h.*`-fallback shape `model` already has |
| C5 | Two-provider e2e harness corpus | 3 scenario tests: tool-calls-per-provider (OpenRouter AND ornith), no-tier3-leak, dgx-reasoning |
| C6 | Backward-compat regression pin | No override ⇒ byte-identical openrouter/deepseek-v4-flash launch spec + env |

**Ordering constraint (load-bearing):** hk-pkugu — claim-time harness-type
resolution (`resolveHarnessAgentTypeQuiet`, workloop.go:3077-3082) — is the
prerequisite for C3. The per-bead provider-tuple resolver MUST inherit that
discipline: resolve the profile against the *already-resolved pi harness*, not a
claude-shaped default. If C3 resolves before/independent of the harness type, a
claude tier-3 default (`sonnet`) leaks into the pi tuple exactly as the model leak
did (workloop.go:3066-3076; constraint §6 in 02-analysis). C3 depends on C1+C2;
C4 depends on C1; C5/C6 depend on C1–C4.

Dependency DAG: `C1 → C3`, `C2 → C3`, `C1 → C4`, `{C3,C4} → C5`,
`{C3,C4} → C6`. No cycles.

---

## C1 — RunCtx tuple contract

**Responsibility.** Extend the published cross-package run-context so a per-bead
provider tuple can ride the same seam as `rc.Model`.

**Files/seams.**
- `internal/handlercontract/harness.go:77-163` — the `RunCtx` struct. Add
  `Provider`, `APIKeyEnv`, `APIKeyFile`, `BaseURL`, `API` (all `string`)
  alongside `Model string` (line 141) / `Effort string` (line 143). Precedent
  doc-comment discipline: "empty ⇒ no override (harness-global default)."
- Consumed by every `Harness` implementation (harness.go:173) — additive only.

**Requirements.**
1. Five new string fields exist on `RunCtx`; each documents "empty ⇒ no override".
2. Zero-value of every new field leaves existing harness behavior unchanged (no
   consumer is forced to read them).
3. No new import or dependency is added to `internal/handlercontract` (it stays a
   leaf contract package).

**Dependencies.** None (foundational).

**Verification.** Package compiles; every existing `RunCtx` construction site
still builds with the new fields defaulted. A struct-shape assertion test that the
five fields exist and are `string`.

---

## C2 — Named-profile config + resolver

**Responsibility.** Define reusable named `{provider, model, api_key_env,
api_key_file, base_url, api}` bundles in project config and shape-validate them
without crossing the depguard boundary.

**Files/seams.**
- `internal/daemon/projectconfig.go:779-854` — add a `profiles:` map to
  `rawHarnessesPiConfig` / `PiHarnessConfig` (a `map[string]PiProfileConfig`
  alongside the passive `fallback` sub-block at :807-814).
- `cmd/harmonik/resolve_pi_config.go:108-224` — profile validation lives here
  (depguard bans `internal/*` importing `internal/daemon`; the resolver already
  lives in `cmd/harmonik` for exactly this reason, resolve_pi_config.go:14-17).
  Reuse `validatePiModelShape` (:210-224), the `base_url` shape check (:171-185),
  and `api_key_file` readable/non-empty + `~`-expansion (:144-166) per profile.

**Requirements.**
1. Config accepts a map of named profiles, each a full tuple; an empty/absent map
   is valid (default path unaffected).
2. Each profile is validated **shape-only** — provider/model matched against
   `piModelShapeRe` `^[A-Za-z0-9._:/-]+$` ≤128; NO enumerated provider allowlist
   (value-opacity invariant PI-052/HC-055a preserved).
3. A profile with `base_url` set but `api` unset resolves `api` consistently with
   `buildPiModelsJSON`'s default (`"openai"`, pilaunchspec.go:339-341) — the
   wire-format triple stays internally coherent.
4. Missing required keys within a profile aggregate into one
   `PiConfigMissingError` (never first-only; resolve_pi_config.go:109-138) naming
   the dotted yaml key and pointing at `harmonik pi config --example`.
5. A referenced-but-undefined profile name is a fail-loud error at resolution, not
   a silent fallthrough.

**Dependencies.** None structurally, but co-designed with C3 (C3 consumes the
validated profile map).

**Verification.** `resolve_pi_config_test.go` cases: valid profile map resolves;
ornith-shaped profile (base_url + api: openai-completions) validates; shape-invalid
provider/model rejected; missing per-profile required key aggregates; unknown
profile reference errors. Depguard lint stays green.

---

## C3 — Claim-time per-bead profile resolver

**Responsibility.** At claim time, read the bead's profile-selecting label,
resolve it to a tuple against the resolved pi harness, and thread it into the
run context — inheriting the hk-pkugu harness-aware discipline so no claude
default leaks.

**Files/seams.**
- `internal/daemon/workloop.go:3063-3091, 4052` — where `resolveHarnessAgentTypeQuiet`
  (3077-3082) computes the harness, `ResolveModelPreference` (3083-3090) resolves
  the model, and `resolvedModel` lands in `claudeRunCtx.model` (4052). The
  provider-tuple resolver sits **here, after** the harness type is known.
- `internal/daemon/modelpreference.go:166, 190-256` — template: the tier-1 per-bead
  `model:<alias>` label collector (labelPrefixModel = "model:", :212-233). A
  parallel `profile:<name>` (or reused label convention) resolver mirrors this.
- `internal/daemon/claudelaunchspec.go:50` (`claudeRunCtx`) — add the five tuple
  fields; populate from the resolved profile.
- `internal/daemon/harnessregistry.go:240-264` — the ONLY place `claudeRunCtx` is
  projected onto the public `RunCtx` literal for the pi path (`Model: rc.model`
  at :258). Copy the five new fields here too.

**Requirements.**
1. A bead carrying the profile-selecting label resolves to that profile's full
   tuple in `RunCtx`; a bead with no such label leaves all five fields empty
   (⇒ harness-global fallback, C4).
2. The resolver runs **only after** `resolveHarnessAgentTypeQuiet` and keys off
   the resolved pi harness type — it MUST NOT produce a tuple for a claude-resolved
   bead, and MUST NOT inherit a claude tier-3 default (hk-pkugu discipline;
   constraint §6). Verifiable by a no-leak test (C5 scenario 2).
3. The tuple is resolved atomically per bead: provider+base_url+api arrive together
   (no partial split that would cross wire formats).
4. `label VALUE` (profile name / model) is never value-validated at this hop
   (opacity; matches modelpreference.go:220-224) — shape/existence only, existence
   checked against C2's validated map.
5. The five fields added to `claudeRunCtx` are copied into the `RunCtx` literal at
   harnessregistry.go:240-264 (both edits required, per analysis §5).

**Dependencies.** C1 (RunCtx fields), C2 (profile map to resolve against).
**Prerequisite:** hk-pkugu claim-time harness-type resolution must be in place.

**Verification.** Unit test on the resolver: labeled bead → correct tuple;
unlabeled bead → empty tuple; claude-resolved bead → no pi tuple / no default leak.
Integration: the `RunCtx` literal carries the tuple end-to-end into `LaunchSpec`.

---

## C4 — `PiHarness.LaunchSpec` tuple override

**Responsibility.** Honor the per-bead tuple from `RunCtx`, falling back to the
daemon-global `h.*` fields — closing the exact gap where five fields hard-read
`h.*`.

**Files/seams.**
- `internal/daemon/piharness.go:125-156` — `LaunchSpec`. Today only `model` has
  the override-with-fallback shape (`model := h.model; if rc.Model != "" { model =
  rc.Model }`, :126-129). `provider`, `apiKeyEnv`, `apiKeyFile`, `baseURL`, `api`
  hard-read `h.*` at :134-139. Give each the same
  `x := h.x; if rc.X != "" { x = rc.X }` shape (conventions §, 02-analysis:219).
- `internal/daemon/piharness.go:48-101` — struct + `NewPiHarness`; the singleton
  built by `newHarnessRegistry(piCfg)` (harnessregistry.go:47-68) stays the
  fallback source. No per-bead re-registration (harness is stateless-per-launch).
- Below this line, `piRunCtx` (pilaunchspec.go:90-167) already carries every field
  — no structural change once the tuple arrives populated.

**Requirements.**
1. When a `RunCtx` field is non-empty it is used; when empty the daemon-global
   `h.*` value is used (per field, mirroring `model` at :126-129).
2. **Wire-format coupling honored:** when a bead selects a profile, provider +
   base_url + api are overridden together as the profile's unit (they arrive
   coupled from C3); no launch is produced with provider from `rc` but `api` from
   `h.*` (constraint §3 — a partial split → wrong-protocol launch).
3. When `apiKeyEnv` is overridden, the fail-closed allowlist strip
   (`buildPiEnv`, pilaunchspec.go:391-471) re-runs keyed on the NEW `apiKeyEnv`,
   injecting only that provider's key and emitting `KEY=` empty-overrides for all
   siblings — no sibling-provider key leaks to the child (constraint §2).
4. When `baseURL` is overridden on an initial turn, `buildPiModelsJSON` generates
   the models.json for the new endpoint; resume turns reuse the prior session's
   config unchanged (models.json initial-turn-only, constraint §4).
5. Billing guard (pilaunchspec.go:281-289) refuses launch before agent_ready if
   the overridden provider's key is absent/empty.

**Dependencies.** C1 (RunCtx fields to read).

**Verification.** `pilaunchspec_test.go`-style cases: rc-provided tuple wins over
`h.*`; empty rc falls back to `h.*`; ornith tuple (base_url + openai-completions)
produces the loopback models.json + correct argv; overridden apiKeyEnv strips the
sibling key and injects only the selected one; missing key ⇒ refused.

---

## C5 — Two-provider e2e harness corpus

**Responsibility.** Prove BOTH OpenRouter (cloud, bare `openai`, no base_url) AND
DGX/ornith (loopback, `openai-completions` + base_url) reach the model with real
`tool_calls` per-bead, and that the per-bead resolver re-affirms the two prior
leak fixes.

**Files/seams (templates).**
- `internal/daemon/hk_lfrub_dot_node_model_leak_test.go` — already constructs a
  per-run ornith/openai-completions `PiHarness`; the template for the two-provider
  build (02-analysis:234-235).
- `internal/daemon/hk_pkugu_pi_launch_e2e_test.go` — the real-`routedLaunchSpecBuilder`
  e2e launch pattern.
- Export seams `ExportedNewPiHarness`, `ExportedRoutedLaunchSpecBuilder`,
  `ExportedClaudeRunCtx` (export_test.go); `skipBillingGuard`/injected `piHome`
  to avoid needing a live key (piRunCtx fields pilaunchspec.go:147-166).

**Requirements (three scenarios).**
1. **pi-toolcalls-per-provider** — a bead labeled with an OpenRouter profile and a
   bead labeled with an ornith profile, dispatched together, each produce the
   correct argv AND models.json for THEIR wire format:
   - OpenRouter bead: `--provider openrouter --model openrouter/<id>`, NO
     base_url, NO models.json (bare `openai` path).
   - ornith bead: `--provider <p> --model <p>/<id>` + generated
     `.harmonik/pi-agent/models.json` with `api: openai-completions` + loopback
     base_url. (Exercises the exact split `buildPiModelsJSON`/argv encode.)
   Each drives a real `tool_calls` turn end-to-end (or, where a live key is
   unavailable in CI, asserts the launch spec + env that a real turn consumes).
2. **pi-no-tier3-leak** (hk-pkugu / `codename:pi-model-leak`) — a pi-resolved bead
   with NO profile/model label does NOT seal a claude tier-3 `sonnet` default into
   the tuple; the harness-global pi model/provider is used. Guards C3 requirement 2.
3. **pi-dgx-reasoning** (hk-4ir08) — an ornith/DGX bead reaches the reasoning
   model and returns a well-formed reasoning + tool_calls turn over the loopback
   openai-completions endpoint.

**Dependencies.** C3, C4 (the threading under test).

**Verification.** The three tests are the verification; they run in the
`internal/daemon` suite and gate the pass. Scenario 1 is the two-provider "both
work completely" success criterion made executable.

---

## C6 — Backward-compat regression pin

**Responsibility.** Pin that with no per-bead override, resolution + launch are
byte-identical to today's default.

**Files/seams.**
- `internal/daemon/pilaunchspec_test.go`, `resolve_pi_config_test.go`,
  `harnessregistry_pi_hkf8u5j_test.go`, `pi_retain_on_failure_hkj6wm7_test.go` —
  the existing green-must-stay-green suite (constraint §5).

**Requirements.**
1. A bead with NO profile/model label, against the daemon-global config, produces
   `--provider openrouter --model deepseek/deepseek-v4-flash`, `api_key_env
   OPENROUTER_API_KEY`, no base_url, no models.json — byte-identical to pre-change.
2. The env allowlist strip output for the default path is unchanged (only
   `OPENROUTER_API_KEY` injected; siblings emitted as `KEY=`).
3. All four listed existing suites pass unmodified except where a signature change
   from C1/C4 requires a mechanical, behavior-preserving edit.

**Dependencies.** C3, C4 (proves they didn't regress the zero-value path).

**Verification.** A golden/byte-comparison test on the default-path launch spec +
env; the four existing suites green.

---

## Interface summary (data flow & contracts)

```
config profiles: map            bead profile label
   (C2, validated in               (selects a profile)
    resolve_pi_config.go)                |
        |                                 v
        +---------→ [C3 claim-time resolver] ──(after harness-type resolve, hk-pkugu)
                          |  writes tuple into claudeRunCtx (claudelaunchspec.go:50)
                          v
              harnessregistry.go:240-264  ── projects claudeRunCtx → RunCtx literal
                          |  (C1 fields on handlercontract.RunCtx)
                          v
              PiHarness.LaunchSpec (C4)  ── rc.X override else h.X fallback
                          |  (tuple arrives populated)
                          v
              piRunCtx → buildPiLaunchSpec / buildPiEnv / buildPiModelsJSON
                          |  (already tuple-complete — no change)
                          v
                     pi --mode json --provider .. --model ..  (+models.json if base_url)
```

**Contracts at the boundaries.**
- **C2 → C3:** the validated profile map; profile-name existence is C2's guarantee,
  C3 fails loud on an unknown reference.
- **C1:** the additive `RunCtx` tuple; zero-value = no-override is the invariant
  every downstream reader relies on (C4's fallback, C6's default path).
- **C3 → C4:** the tuple arrives coupled (provider+base_url+api atomic) so C4 never
  splits the wire format.
- **hk-pkugu:** the harness-type-first ordering is the contract C3 inherits from
  the existing claim-time model resolver — violating it re-opens the model leak.

---

## Goal → component traceability

| 01-problem-space goal | Component(s) |
|---|---|
| G1 provider switchable like model, no source change | C1, C2, C3, C4 |
| G2 select provider+model tuple together (coherent unit) | C2 (profile bundle), C3, C4 (coupling) |
| G3 preserve DeepSeek/OpenRouter default | C6 |
| G4 grain finer than daemon-global (per-bead via RunCtx) | C1, C3 |
| Success #2 (carry api_key/base_url/api; fail-closed; no key leak) | C2, C4 |
| Success #5 (two wire formats e2e) | C5 |
| Success #6 (existing tests pass + new coverage) | C5, C6 |
| Re-scope #1 (per-bead, concurrent A→OpenRouter B→ornith) | C3, C5 scenario 1 |
| Re-scope #2 (BOTH providers land, not plan) | C5 (all three scenarios) |

---

## Rejected alternative

**Raw per-field bead overrides** (put provider / base_url / api / api_key_env each
as its own bead label, no profile indirection). Rejected because:
1. It fragments the wire-format triple — a bead could set `provider` and forget
   `api`/`base_url`, producing a wrong-protocol launch (constraint §3). The profile
   makes provider+base_url+api atomic by construction.
2. It fragments the fail-closed credential invariant — the api_key_env would be
   independently settable from the provider, inviting a mismatched key/provider
   pair. A profile binds credential to provider as one validated unit (constraint §2).
3. It multiplies per-bead label surface and pushes validation onto the claim path;
   the profile registry validates once at config load in `resolve_pi_config.go`
   (respecting depguard) and the bead carries a single opaque name.

Noted for completeness; the named-profile registry is the chosen mechanism.
