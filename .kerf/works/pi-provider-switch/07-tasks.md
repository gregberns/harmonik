# 07 — Tasks — `pi-provider-switch`

**Status:** kerf pass 7 (tasks). Produced from SPEC.md (post-integration-review, with
the 3 SHOULD-FIX clarifications applied), 06-integration.md, and 05-specs/C1–C6.
Parent epic: **hk-m6uu2** (`codename:pi-provider-switch`). Label every task bead
`codename:pi-provider-switch`, `--parent hk-m6uu2`, `--priority=1`.

**Load-bearing prereq (NOT a task in this list — a manual gate):** **hk-pkugu**
(`resolveHarnessAgentTypeQuiet` at `workloop.go:3077-3082`) is IN_PROGRESS and must be
merged before T-C3 starts. Without it a claude tier-3 `sonnet` default leaks into the
pi tuple exactly as the model leak did. T-C3 has a hard dependency on hk-pkugu.

**Ownership (locked, SPEC §2):**
- **chani** — C1, C2, C3, C4, C6, and the **C5 corpus DESIGN/contract** (scenario
  definitions, hermetic-vs-live boundary, export seams, per-scenario assertions).
- **stilgar** — the **C5 GATE-WIRING** (scenario fixtures, §10.1 conformance
  registration, assertion wiring into the deterministic gates, `scenario-gate.sh`
  pickup). stilgar wires against the contract chani designs.

---

## Dependency graph

```
        hk-pkugu (prereq, IN_PROGRESS — merge before T-C3)
              │
   ┌──────────┴───────────┐
 T-C1                    T-C2            (T-C1 ∥ T-C2 — independent)
   │  \                    │
   │   \___________        │
   │               \       │
 T-C4 (needs C1)   T-C3 (needs C1 + C2 + hk-pkugu)
   │                   │
   └────────┬──────────┘
            │
   ┌────────┴─────────────────────┐
 T-C5-design (chani)            T-C6 (chani)     (both need {C3,C4})
   │
 T-C5-wiring (stilgar; needs T-C5-design)
   │
   ├── T-SCENARIO  (scenario-test bead; needs T-C5-wiring + T-C6)
   └── T-EXPLORE   (exploratory-test/operator-canary; needs T-C5-wiring)
```

**Parallelization:** T-C1 ∥ T-C2 at the start. T-C4 unblocks as soon as T-C1 lands
(parallel to T-C2/T-C3). T-C3 is the join of C1+C2+hk-pkugu. T-C5-design and T-C6 both
open once {C3,C4} land; T-C5-wiring follows T-C5-design; the two test beads gate the
epic close.

**DAG check:** acyclic. Matches SPEC §4 build order (`C1→C3`, `C2→C3`, `C1→C4`,
`{C3,C4}→C5`, `{C3,C4}→C6`).

---

## Tasks

### T-C1 — RunCtx tuple contract (C1) — chani
- **What to build:** Add five `string` fields (`Provider`, `APIKeyEnv`, `APIKeyFile`,
  `BaseURL`, `API`) to `handlercontract.RunCtx`, each with an "empty ⇒ no override
  (harness-global default)" doc comment. Zero-value must leave existing harness
  behavior unchanged; no new import to the leaf package.
- **Spec ref:** SPEC §5; C1 spec.
- **Files:** `internal/handlercontract/harness.go` (after `Effort`, ~:143);
  new test `internal/handlercontract/harness_runctx_tuple_test.go`.
- **Acceptance:** `go build ./...` compiles; `TestRunCtx_ProviderTupleFields_Exist`
  (keyed-literal, all five sentinels, readback) passes; `go list -deps
  ./internal/handlercontract` shows no new dependency.
- **Deps:** none. (Blocks T-C3, T-C4.)

### T-C2 — Named-profile config + resolver (C2) — chani
- **What to build:** Add `rawHarnessesPiProfileConfig` + `Profiles` map on the raw
  and typed (`PiProfileConfig` + `PiHarnessConfig.Profiles`) config layers; extend the
  raw→typed mapper to copy the map. In `ResolvePiConfig`: extract `validatePiBaseURL`
  and `resolvePiAPIKeyFile` helpers (rewrite the top-level blocks to call them), then a
  per-profile validation loop — missing-key aggregation into the SAME
  `PiConfigMissingError` (dotted paths `harnesses.pi.profiles.<name>.<field>`),
  shape-only validation (NO allowlist), base_url + api_key_file (expanded) validation,
  `api` left empty. Missing-value gate stays FIRST.
- **Spec ref:** SPEC §6; C2 spec. Cross-cutting invariant §11.1, §11.3.
- **Files:** `internal/daemon/projectconfig.go` (structs + mapper, ~:789-854);
  `cmd/harmonik/resolve_pi_config.go` (helpers + loop, ~:109-185);
  `cmd/harmonik/resolve_pi_config_test.go` (new `TestResolvePiConfig_Profile*` cases).
- **Acceptance:** two-profile map (openrouter cloud + ornith base_url/openai-completions)
  resolves with both present and api_key_file expanded; shape-invalid provider/model
  rejected with dotted-path `PiConfigError`; per-profile + top-level missing keys
  aggregate into ONE error; absent map resolves exactly as today; base_url-set/api-unset
  leaves `api == ""`; depguard (`golangci-lint`) stays green.
- **Deps:** none (may run parallel to T-C1). (Blocks T-C3.)

### T-C4 — PiHarness.LaunchSpec tuple override (C4) — chani
- **What to build:** In `PiHarness.LaunchSpec`, give each of the five tuple fields the
  same `x := h.x; if rc.X != "" { x = rc.X }` override shape `model` already has, and
  feed the picked values (not `h.*`) into the `piRunCtx` literal. No change to
  `NewPiHarness`, the struct, `newHarnessRegistry`, or anything below the `piRunCtx`
  literal (already tuple-complete). Honor wire-format coupling (no provider-from-rc /
  api-from-h split); overridden `apiKeyEnv` re-runs the fail-closed `buildPiEnv` strip;
  overridden `baseURL` regenerates models.json on an initial turn only; billing guard
  refuses on absent/empty key.
- **Spec ref:** SPEC §8; C4 spec. Invariant §11.2, §11.4, §11.5.
- **Files:** `internal/daemon/piharness.go` (`LaunchSpec`, ~:125-156);
  `internal/daemon/pilaunchspec_test.go` (new `TestPiHarness_LaunchSpec_*` cases).
- **Acceptance:** rc tuple wins over `h.*`; empty rc falls back per field; ornith rc
  (base_url + openai-completions) produces loopback models.json + correct argv;
  overridden apiKeyEnv strips siblings and injects only the selected key; missing key ⇒
  refused. Tests: `_RCTupleOverridesGlobal`, `_EmptyRCFallsBackToGlobal` (shared w/ C6),
  `_OverriddenAPIKeyEnv_StripsSiblings`, `_CoupledTriple_TravelTogether`.
- **Deps:** T-C1. (Parallel to T-C2/T-C3.)

### T-C3 — Claim-time per-bead profile resolver (C3, the crux) — chani
- **What to build:** Add `labelPrefixProfile = "profile:"` + `resolvePiProfile`
  (collect→count, mirroring `resolveModelField`) with:
  - **Harness gate** keyed on the concrete constant `agentType == core.AgentTypePi`
    (NOT a "pi family" test — SHOULD-FIX #2): non-pi ⇒ zero tuple, no error.
  - Exactly-one `profile:` label ⇒ look up `piCfg.Profiles[name]`; **unknown name ⇒
    fail loud** via the `brAdapter.ReopenBead` refuse-before-launch seam (the same one
    `CrossRepoUnsafeError` / `StartFromRefError` use at `workloop.go:~3110/~3140`):
    stderr log + `ReopenBead(profErr.Error())` + early `return`, so the bead is
    reopened and NO LaunchSpec is built (SHOULD-FIX #1). `>1` ⇒ `emitBeadLabelConflict`,
    treat as absent. `0` ⇒ zero tuple.
  - Model coalescing via the single `hasSingleModelLabel(beadLabels) bool` helper —
    non-zero profile AND no tier-1 `model:` label ⇒ `resolvedModel = profile.Model`.
    Do NOT return a coalesce flag from `resolvePiProfile` (SHOULD-FIX #3).
  - Seat the five tuple fields into `claudeRunCtx` (`claudelaunchspec.go:21-50`) and
    project them onto the `RunCtx` literal (`harnessregistry.go:240-264`) — BOTH edits
    required.
- **Spec ref:** SPEC §7 (all sub-sections); C3 spec. Locked decisions §3.2, §3.3.
- **Files:** `internal/daemon/modelpreference.go` (or new `pi_profile_resolve.go`);
  `internal/daemon/workloop.go` (~:3082-3091, 4052); `internal/daemon/claudelaunchspec.go`
  (~:21-50); `internal/daemon/harnessregistry.go` (~:240-264); resolver unit tests in
  `internal/daemon`.
- **Acceptance:** SPEC §7 acceptance (1)-(7): pi bead + defined profile → full tuple
  end-to-end; pi bead no label → five empty; claude-resolved bead + profile label →
  zero tuple, no leak; `profile:X`+`model:Y` → X triple/creds + model Y; undefined
  profile → fail loud via ReopenBead (no LaunchSpec); `>1` labels → conflict; tuple
  survives DOT cascade. Tests: `TestResolvePiProfile_LabeledBead_ResolvesTuple`,
  `_UnlabeledBead_ZeroTuple`, `_ClaudeHarness_ZeroTuple`, `_UnknownProfile_FailLoud`,
  `_MultipleLabels_Conflict`, `_ModelLabelOverridesProfileModelOnly`.
- **Deps:** T-C1, T-C2, **hk-pkugu (merged)**. (Blocks T-C5-design, T-C6.)

### T-C5-design — Two-provider e2e corpus DESIGN/contract (C5, chani half) — chani
- **What to build:** Design the three hermetic Go scenarios in `internal/daemon`
  (`daemon_test`) at the launch-spec / models.json / env layer driving the REAL seam
  via `ExportedRoutedLaunchSpecBuilder`; define the hermetic-vs-live boundary (dummy
  key FILE + `t.Setenv("HOME", t.TempDir())`), the export seam
  `ExportedResolvePiProfile` (+ optional `ExportedNewHarnessRegistryWithPiProfiles`),
  the unique helper prefixes (`hkppsToolcalls`/`hkppsNoLeak`/`hkppsDgx`/`hkppsNoLaunch`),
  and the exact assertions each scenario/sub-case must make — including the REQUIRED
  unknown-profile **refuse-to-launch** e2e sub-case (SHOULD-FIX #1: assert the workloop
  builds NO LaunchSpec and routes the bead to `brAdapter.ReopenBead`, via a capturing
  brAdapter). Note in-corpus that "concurrent A→OpenRouter/B→ornith" is established by
  construction (stateless-per-launch, no shared mutable state) + the live canary, not a
  parallel-goroutine test.
- **Spec ref:** SPEC §9; C5 spec (scenarios 1-3 + unknown-profile sub-case + export
  seam). Re-scope directives #1, #2.
- **Files (design + export seam, chani):** `internal/daemon/export_test.go`
  (`ExportedResolvePiProfile`); the scenario contract documented for stilgar to wire.
- **Acceptance:** the scenario contract + export seam exist and compile; each scenario's
  assertions and the hermetic bypass are unambiguously specified for the gate-wiring
  task; unknown-profile refuse-to-launch sub-case is specified.
- **Deps:** T-C3, T-C4. (Blocks T-C5-wiring.)

### T-C5-wiring — C5 corpus GATE-WIRING (C5, stilgar half) — stilgar (coordinated w/ chani)
- **What to build:** Author the three scenario test files against chani's contract
  (`pi_toolcalls_per_provider_test.go`, `pi_no_tier3_leak_test.go` incl. the DOT-path
  variant AND the unknown-profile refuse-to-launch sub-case, `pi_dgx_reasoning_test.go`);
  wire the assertions (argv, generated models.json, injected env, ReopenBead capture),
  register in the §10.1 conformance registry, and confirm `scripts/scenario-gate.sh`
  picks them up (internal/daemon always affected; NO YAML scenario). Adversarial
  counterfactual for scenario 2 (copy the hk_pkugu pattern).
- **Spec ref:** SPEC §9 (gate-wiring); C5 spec Files & Acceptance.
- **Files:** `internal/daemon/pi_toolcalls_per_provider_test.go` (new),
  `internal/daemon/pi_no_tier3_leak_test.go` (new or extend
  `hk_pkugu_pi_launch_e2e_test.go`), `internal/daemon/pi_dgx_reasoning_test.go` (new);
  §10.1 conformance registration; `scripts/scenario-gate.sh` pickup verified.
- **Acceptance:** C5 acceptance (1)-(5): both beads emit per-wire-format argv+models.json;
  no-label pi argv `--model` = harness-global pi model (NOT `sonnet`) w/ counterfactual;
  DOT variant tuple unchanged; unknown-profile sub-case shows no LaunchSpec + ReopenBead;
  ornith reasoning bead emits loopback spec+models.json w/ operator-canary comment; all
  hermetic; `scenario-gate.sh` runs them.
- **Deps:** T-C5-design. (Blocks T-SCENARIO, T-EXPLORE.)
- **Owner note:** stilgar-owned gate-wiring; coordinate with chani on the contract.

### T-C6 — Backward-compat regression pin (C6) — chani
- **What to build:** Pure test — a `TestPiHarness_DefaultPath_ByteIdentical` golden
  from a FIXED openrouter fixture (provider `openrouter`, model
  `deepseek/deepseek-v4-flash`, api_key_env `OPENROUTER_API_KEY`, no base_url/api) with
  an EMPTY RunCtx tuple: argv `--provider openrouter --model deepseek/deepseek-v4-flash`,
  NO models.json, env strip injects ONLY `OPENROUTER_API_KEY` (siblings `KEY=`). Confirm
  the green-must-stay-green suites pass unmodified (or only behavior-preserving
  keyed-literal edits).
- **Spec ref:** SPEC §10; C6 spec. Success criterion #3, #6.
- **Files:** `internal/daemon/pilaunchspec_test.go` (or new
  `pi_default_path_golden_test.go`); verify `cmd/harmonik/resolve_pi_config_test.go`,
  `harnessregistry_pi_hkf8u5j_test.go`, `pi_retain_on_failure_hkj6wm7_test.go`,
  `hk_pkugu`, `hk_lfrub` suites stay green.
- **Acceptance:** `TestPiHarness_DefaultPath_ByteIdentical` passes; env strip injects
  only `OPENROUTER_API_KEY`; all listed suites pass.
- **Deps:** T-C3, T-C4.

### T-SCENARIO — scenario-test bead (jig-required) — stilgar (gate) / chani (design)
- **What:** `scenario: pi-provider-switch — two-provider per-bead launch (openrouter +
  ornith)`. The end-to-end workflow proof: the routed launch-spec builder resolves each
  bead's tuple at claim time and emits per-provider argv + `.harmonik/pi-agent/models.json`.
  The live-DGX operator canary result (real `tool_calls` per provider over the loopback
  tunnel) is recorded here as the DoD proof. **Neither the plan (hk-m6uu2) nor the impl
  beads may close until this bead closes.**
- **Deps:** T-C5-wiring, T-C6.

### T-EXPLORE — exploratory-test bead (jig-required) — chani (operator canary)
- **What:** `explore: pi-provider-switch — operator canary: real tool_calls per provider
  over DGX tunnel`. Operator-facing CLI surface: `harmonik queue submit` a
  `profile:openrouter-cloud` bead and a `profile:ornith-dgx` bead with the DGX loopback
  tunnel (`http://127.0.0.1:8551/v1`) up; confirm each drives a real `tool_calls` turn
  end-to-end. NOT gated by CI; recorded on T-SCENARIO.
- **Deps:** T-C5-wiring.

---

## Spec-coverage check (every SPEC section → task)

| SPEC section | Task |
|---|---|
| §5 C1 RunCtx tuple | T-C1 |
| §6 C2 config + resolver | T-C2 |
| §7 C3 claim-time resolver (7.1–7.6) | T-C3 |
| §8 C4 LaunchSpec override | T-C4 |
| §9 C5 e2e corpus | T-C5-design (chani) + T-C5-wiring (stilgar) |
| §10 C6 regression pin | T-C6 |
| §11 cross-cutting invariants | enforced across T-C2 (§11.1,3), T-C3 (§11.4), T-C4 (§11.2,4,5) |
| §12 traceability | satisfied by the task↔component mapping above |
| §3 locked decisions | carried verbatim in T-C2/T-C3/T-C4 acceptance |
| jig: scenario-test bead | T-SCENARIO |
| jig: exploratory-test bead | T-EXPLORE |

Every SPEC section and both jig-required test beads have a home. The two test beads are
dependents of the core implementation tasks and gate the epic close.

---

## Ready-to-run `br create` invocations (DO NOT RUN — author only)

> All: `--priority=1 --label codename:pi-provider-switch --parent hk-m6uu2`. Dependency
> wiring (`br dep add <bead> <depends-on>`) noted per bead; run after the IDs exist.

```bash
# T-C1 — chani
br create --title="pi-provider-switch C1: add 5-field Pi provider tuple to handlercontract.RunCtx" \
  --type=task --priority=1 --parent hk-m6uu2 --label codename:pi-provider-switch \
  --assignee chani
# deps: none

# T-C2 — chani
br create --title="pi-provider-switch C2: named-profile config map + ResolvePiConfig per-profile validation" \
  --type=task --priority=1 --parent hk-m6uu2 --label codename:pi-provider-switch \
  --assignee chani
# deps: none

# T-C4 — chani
br create --title="pi-provider-switch C4: PiHarness.LaunchSpec per-field rc.* tuple override + coupling" \
  --type=task --priority=1 --parent hk-m6uu2 --label codename:pi-provider-switch \
  --assignee chani
# deps: T-C1

# T-C3 — chani (the crux)
br create --title="pi-provider-switch C3: claim-time resolvePiProfile (core.AgentTypePi gate, fail-loud ReopenBead, hasSingleModelLabel coalesce)" \
  --type=task --priority=1 --parent hk-m6uu2 --label codename:pi-provider-switch \
  --assignee chani
# deps: T-C1, T-C2, hk-pkugu (must be merged first — load-bearing prereq)

# T-C5-design — chani (corpus contract + export seam)
br create --title="pi-provider-switch C5-design: two-provider hermetic corpus contract + ExportedResolvePiProfile seam (chani)" \
  --type=task --priority=1 --parent hk-m6uu2 --label codename:pi-provider-switch \
  --assignee chani
# deps: T-C3, T-C4

# T-C5-wiring — stilgar (gate-wiring)
br create --title="pi-provider-switch C5-wiring: author 3 scenario test files + unknown-profile refuse-to-launch + scenario-gate registration (stilgar)" \
  --type=task --priority=1 --parent hk-m6uu2 --label codename:pi-provider-switch \
  --assignee stilgar
# deps: T-C5-design  (STILGAR-OWNED gate-wiring; coordinate with chani on the contract)

# T-C6 — chani
br create --title="pi-provider-switch C6: TestPiHarness_DefaultPath_ByteIdentical golden + green-must-stay-green suite verification" \
  --type=task --priority=1 --parent hk-m6uu2 --label codename:pi-provider-switch \
  --assignee chani
# deps: T-C3, T-C4

# T-SCENARIO — scenario-test bead (jig-required)
br create --title="scenario: pi-provider-switch — two-provider per-bead launch (openrouter + ornith)" \
  --type=task --priority=1 --parent hk-m6uu2 --label codename:pi-provider-switch \
  --label scenario-test --assignee stilgar
# deps: T-C5-wiring, T-C6   (gates epic close)

# T-EXPLORE — exploratory-test bead (jig-required; operator canary)
br create --title="explore: pi-provider-switch — operator canary: real tool_calls per provider over DGX tunnel" \
  --type=task --priority=1 --parent hk-m6uu2 --label codename:pi-provider-switch \
  --label exploratory-test --assignee chani
# deps: T-C5-wiring
```

**Dependency-wiring commands (run after IDs exist):**
```bash
# <C3>  depends on <C1> <C2> hk-pkugu
# <C4>  depends on <C1>
# <C5d> depends on <C3> <C4>
# <C5w> depends on <C5d>
# <C6>  depends on <C3> <C4>
# <SCN> depends on <C5w> <C6>
# <EXP> depends on <C5w>
# e.g.: br dep add <C3-id> <C1-id>; br dep add <C3-id> <C2-id>; br dep add <C3-id> hk-pkugu
```

---

## Proposed beads summary (title · owner · deps)

| # | Title (short) | Owner | Depends on |
|---|---|---|---|
| T-C1 | C1 RunCtx tuple | chani | — |
| T-C2 | C2 profile config + resolver | chani | — |
| T-C4 | C4 LaunchSpec override | chani | C1 |
| T-C3 | C3 claim-time resolver (crux) | chani | C1, C2, **hk-pkugu** |
| T-C5-design | C5 corpus contract + export seam | chani | C3, C4 |
| T-C5-wiring | C5 gate-wiring (test files + registration) | **stilgar** | C5-design |
| T-C6 | C6 byte-identical regression pin | chani | C3, C4 |
| T-SCENARIO | scenario-test (two-provider launch) | stilgar | C5-wiring, C6 |
| T-EXPLORE | exploratory-test (operator canary) | chani | C5-wiring |
</content>
</invoke>
