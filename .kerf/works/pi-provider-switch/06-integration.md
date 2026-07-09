# Pass 6: Integration — `pi-provider-switch`

How the six components (C1–C6) connect into one working capability: make Pi's
provider AND model switchable **per-bead** through the same `RunCtx` seam
`rc.Model` already rides, with BOTH OpenRouter (cloud, bare `openai`) AND
DGX/ornith (loopback, `openai-completions` + base_url) reaching the model and
driving real `tool_calls` end-to-end. No override ⇒ today's
`openrouter`/`deepseek-v4-flash` behavior byte-identical.

This document is the connective tissue between the component change specs; the
single self-contained normative document an implementer reads first is `SPEC.md`
in this same directory.

---

## Ownership split (record in both docs — load-bearing)

- **chani** owns the provider-side capability: **C1–C4** (RunCtx tuple contract,
  named-profile config + resolver, claim-time per-bead resolver, PiHarness
  LaunchSpec override) **plus the C5 corpus DESIGN/contract** — the scenario
  definitions, the hermetic-vs-live boundary, the export seams
  (`ExportedResolvePiProfile` etc.), and the assertions each scenario must make.
- **stilgar** owns the **C5 corpus GATE-WIRING**: the scenario fixtures, the
  §10.1 conformance registration, the assertion wiring into the deterministic
  gates, and the `scripts/scenario-gate.sh` pickup. stilgar wires against the
  contract chani designs; chani does not wire the gate.
- **C6** (backward-compat regression pin) rides with the chani provider-side work
  — it is the golden pin proving C1–C4 did not disturb the zero-value default
  path.

This split is reflected in the Integration testing strategy section below.

---

## Integration ORDER (build sequence)

The dependency DAG is `C1 → C3`, `C2 → C3`, `C1 → C4`, `{C3,C4} → C5`,
`{C3,C4} → C6` (no cycles). The load-bearing build order:

0. **PREREQUISITE — hk-pkugu claim-time harness-type resolution must already be in
   place** (`resolveHarnessAgentTypeQuiet`, `workloop.go:3077-3082`). This is NOT a
   component of this work; it is an inherited invariant. If it is not present,
   C3 cannot key its resolver off `resolvedAgentType` and the claude tier-3
   `sonnet` default leaks into the pi tuple exactly as the model leak did
   (workloop.go:3066-3076; constraint §6 of 02-analysis). Verify it before C3.

1. **C1 — RunCtx tuple contract FIRST.** The five sibling fields (`Provider`,
   `APIKeyEnv`, `APIKeyFile`, `BaseURL`, `API`) must exist on the public
   `handlercontract.RunCtx` before C3 can project onto them and before C4 can read
   them. Pure additive struct growth; no behavior. This is the struct contract
   that C3 and C4 both depend on.

2. **C2 — Named-profile config + resolver.** The `profiles:` map on the config
   structs plus its shape-validation in `cmd/harmonik/resolve_pi_config.go`. C2
   produces the validated profile map that C3 resolves bead labels against. C2 can
   proceed in parallel with C1 (no structural dependency) but MUST land before C3
   (C3 consumes the validated map). The two extract-to-helper refactors
   (`validatePiBaseURL`, `resolvePiAPIKeyFile`) are behavior-preserving and guarded
   by the existing `resolve_pi_config_test.go` staying green.

3. **C4 — PiHarness.LaunchSpec tuple override** (depends on C1 only). Give the five
   sibling fields the same override-with-`h.*`-fallback shape `model` already has.
   Can land right after C1, in parallel with C2/C3, because it only reads the C1
   fields; it does not care how they got populated.

4. **C3 — Claim-time per-bead profile resolver** (depends on C1 + C2, prerequisite
   hk-pkugu). The crux. Adds `labelPrefixProfile = "profile:"`, the
   `resolvePiProfile` collect→count resolver keyed off `resolvedAgentType`, the
   model:+profile: precedence coalescing, the `claudeRunCtx` fields, and the
   projection onto the `RunCtx` literal at `harnessregistry.go:240-264`. Must land
   AFTER C1 (fields to project onto), C2 (map to resolve against), and with
   hk-pkugu in place (harness gate).

5. **C5 — Two-provider e2e corpus** and **C6 — Backward-compat regression pin**
   (both depend on {C3, C4}). C5 proves both providers thread correctly per-bead;
   C6 proves the zero-value path is byte-identical. Land last — they are the proof
   the threading works and did not regress the default.

**Why order is load-bearing, three specific prereqs:**
- **hk-pkugu before C3** — or the model leak re-opens (a claude tier-3 `sonnet`
  seals into the pi tuple).
- **C1 (struct contract) before C3/C4 use it** — C3 projects onto the fields, C4
  reads them; both fail to compile without C1.
- **C2 (config) before C3 resolves profiles** — C3's existence check keys off C2's
  validated `PiHarnessConfig.Profiles` map.

---

## Shared state / resources — the single data-flow seam

The whole capability is one linear data-flow seam: the provider tuple travels from
config to the launched process through exactly one path, and every component owns
one hop of it. There is NO second path and no shared mutable state — the harness is
stateless-per-launch (the daemon-global singleton stays the fallback source; no
per-bead re-registration).

```
config profiles: map            bead profile:<name> label
  (C2, validated in                (selects a profile)
   resolve_pi_config.go)                 |
        |                                  v
        +---------→ [C3 claim-time resolvePiProfile]
                     runs AFTER resolveHarnessAgentTypeQuiet (hk-pkugu),
                     keyed off resolvedAgentType (pi family only)
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

**The RunCtx tuple is the single seam.** The five sibling fields on
`handlercontract.RunCtx` (C1) are the one contract every hop agrees on:

- **C2** produces the validated `PiHarnessConfig.Profiles` map (the tuple values,
  at config-load time).
- **C3** resolves a bead's label to one profile and seats the tuple into
  `claudeRunCtx`, then the projection at `harnessregistry.go:240-264` copies it onto
  the public `RunCtx` literal.
- **C4** reads the `RunCtx` tuple in `PiHarness.LaunchSpec`, overriding `h.*`
  per-field when non-empty.
- Below `LaunchSpec`, `piRunCtx` / `buildPiLaunchSpec` / `buildPiEnv` /
  `buildPiModelsJSON` are already tuple-complete — they need no change once the
  populated tuple arrives.

**The zero-value invariant is the shared contract.** Every downstream reader relies
on "empty ⇒ no override (harness-global default)". C4's fallback and C6's default
path both depend on it. Any component that violated zero-value=no-override would
break the backward-compat guarantee.

---

## Cross-cutting concerns

These invariants span multiple components and must hold across the whole seam, not
just within one component.

### 1. Error propagation — `PiConfigMissingError` with dotted paths
Config-load validation (C2) aggregates ALL missing required keys — across the
top-level block AND every profile — into ONE `PiConfigMissingError`, each naming
the dotted yaml key (`harnesses.pi.profiles.<name>.<field>`) and pointing at
`harmonik pi config --example`. Never first-only. The missing-value gate stays
first (aggregate before any shape/url/file check). At claim time (C3), an
unknown-profile reference is a separate fail-loud error naming the profile and the
bead — routed through the existing claim-time error path so the bead does not
silently launch on an empty/wrong tuple.

### 2. Fail-closed credential strip
`buildPiEnv` (pilaunchspec.go:391-471) strips ALL non-selected provider keys and
injects only the selected `api_key_env`'s key, emitting `KEY=` empty-overrides for
every sibling. When C4 overrides `apiKeyEnv`, this strip re-runs keyed on the NEW
key — no sibling-provider key leaks to the child. The billing guard
(pilaunchspec.go:281-289) refuses launch before agent_ready if the overridden
provider's key is absent/empty. This invariant is unchanged in mechanism; C4 just
feeds it the per-bead `apiKeyEnv` instead of only `h.apiKeyEnv`.

### 3. Value-opacity (PI-052/HC-055a) — NO provider allowlist
Provider and model are validated **shape-only** (`piModelShapeRe`
`^[A-Za-z0-9._:/-]+$`, ≤128) at C2; profile NAME and model VALUE are never
value-validated at the C3 claim hop (existence-only against C2's map). No component
introduces an enumerated provider allowlist that would reject a valid Pi provider.
Pi's full provider/model range stays selectable.

### 4. Wire-format triple coupling
`{provider, base_url, api}` plus credentials (`api_key_env`, `api_key_file`) travel
as ONE atomic unit from the profile. This is why the mechanism is a named-profile
registry, not raw per-field bead overrides (which would fragment the triple and
invite a wrong-protocol launch or a mismatched key/provider pair). C3 delivers the
triple coupled; C4 copies it through together and introduces no per-field default
that could split it; a C4 test asserts the coupling invariant. The `model:` label,
when combined with a profile, overrides ONLY the model field — model is orthogonal
to the coupled triple, so overriding it alone cannot produce a wrong-wire-format
launch.

### 5. models.json initial-turn-only
`buildPiModelsJSON` generates the loopback models.json only on the initial turn
(`priorSessionID == nil`, pilaunchspec.go:300). A profile switch fixes the tuple
per-bead (not mid-session); resume turns reuse the prior session's config unchanged,
so the resume env matches the initial turn's provider. C3 sets the tuple once at
claim time; the DOT per-node cascade re-resolves only `model=`/`effort=`, never the
provider tuple, so the tuple rides `claudeRunCtx` unclobbered through node
re-launches (verified by the C5 DOT-path variant; no DOT code change expected).

---

## Integration testing strategy

Two distinct proof layers, with a clean ownership boundary between them:

### A. Hermetic corpus at the launch-spec / model_selected layer (C5 + C6)
There is NO in-test fake HTTP provider anywhere in the pi tests. The corpus proves
BOTH wire formats at the **argv / generated models.json / injected env** layer
without network, driving the REAL claim-time seam via
`ExportedRoutedLaunchSpecBuilder` (as `hk_pkugu_pi_launch_e2e_test.go` does). Three
C5 scenarios:
1. **pi-toolcalls-per-provider** — an OpenRouter-profile bead and an ornith-profile
   bead dispatched together, each emitting the correct argv + models.json for THEIR
   wire format (openrouter: no models.json; ornith: loopback models.json with
   `api: openai-completions`). This is the concurrent A→OpenRouter / B→ornith
   per-bead re-scope made executable.
2. **pi-no-tier3-leak** (hk-pkugu) — a no-label pi bead's argv `--model` is the
   harness-global pi model, NOT `sonnet`; includes an adversarial counterfactual and
   a DOT-path variant proving the tuple survives the cascade.
3. **pi-dgx-reasoning** (hk-4ir08) — an ornith reasoning bead emits the loopback
   launch spec + models.json.

C6 adds the golden byte-identical default-path pin. Billing guard bypassed
hermetically (dummy key FILE as `APIKeyFile` + `t.Setenv("HOME", t.TempDir())`); no
live key, no network.

**Ownership within C5:** chani designs the corpus (scenario definitions, the
hermetic-vs-live boundary, the assertions, the `ExportedResolvePiProfile` export
seam). **stilgar wires the gate** — the scenario fixtures, §10.1 conformance
registration, assertion wiring into the deterministic gates, and the
`scripts/scenario-gate.sh` pickup (internal/daemon is always-affected, so the tests
are auto-picked; no YAML scenario is authored). This is the chani/stilgar seam:
chani hands stilgar a wired-against-able contract; stilgar makes it a gate.

### B. Live-DGX operator canary (DoD proof, separate, NOT CI)
The live `tool_calls` round-trip is a SEPARATE operator canary — the Definition-of-
Done proof, not part of the hermetic gate. With the DGX loopback tunnel up
(`http://127.0.0.1:8551/v1`; srt blocks the LAN IP → loopback only), submit an
ornith-profile bead and an openrouter-profile bead and confirm each drives a real
`tool_calls` turn end-to-end. Recorded on the scenario-test bead, not gated by CI.
This is where "e2e" is honestly bounded: the hermetic corpus proves everything up to
the launch spec the real turn consumes; the model round-trip is the operator canary.

---

## Contracts at the boundaries (summary)

- **C2 → C3:** the validated profile map; profile-name existence is C2's guarantee at
  config load, C3 fails loud on an unknown reference at claim time.
- **C1:** the additive `RunCtx` tuple; zero-value = no-override is the invariant every
  downstream reader relies on (C4's fallback, C6's default path).
- **C3 → C4:** the tuple arrives coupled (provider+base_url+api atomic) so C4 never
  splits the wire format.
- **hk-pkugu (inherited):** the harness-type-first ordering is the contract C3
  inherits from the existing claim-time model resolver — violating it re-opens the
  model leak.
- **chani → stilgar (C5):** chani's corpus DESIGN/contract + export seams; stilgar's
  gate-wiring against it.
