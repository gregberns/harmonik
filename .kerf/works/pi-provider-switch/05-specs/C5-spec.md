# C5 — Two-provider e2e harness corpus — Change Spec

**Component:** C5 (detailed). Prove BOTH OpenRouter (cloud, bare `openai`, no
base_url) AND DGX/ornith (loopback, `openai-completions` + base_url) reach the
launch with the correct per-bead tuple, and re-affirm the two prior leak fixes.
Three new hermetic Go scenarios in `internal/daemon`.

## Requirements (from 03-components.md C5)

Three scenarios:
1. **pi-toolcalls-per-provider** — an OpenRouter-profile bead and an ornith-profile
   bead, dispatched together, each produce the correct argv AND models.json for THEIR
   wire format (OpenRouter: bare `openai`, no base_url, no models.json; ornith:
   `openai-completions` + loopback base_url + generated models.json).
2. **pi-no-tier3-leak** (hk-pkugu) — a pi-resolved bead with NO profile/model label
   does NOT seal a claude tier-3 `sonnet` default; the harness-global pi
   model/provider is used. Includes a DOT-path variant (locked C3-Q5 decision) AND a
   REQUIRED unknown-profile refuse-to-launch sub-case (C3 fail-loud e2e — the workloop
   builds no LaunchSpec and reopens the bead; review finding #1).
3. **pi-dgx-reasoning** (hk-4ir08) — an ornith/DGX bead reaches the reasoning model
   over the loopback openai-completions endpoint (hermetic: assert launch spec +
   models.json).

## Research summary (from 04-research/C5)

- **Hermetic boundary (LOCKED).** There is NO in-test fake HTTP provider anywhere in
  the pi tests. The existing corpus proves BOTH wire formats **at the launch-spec /
  models.json / env layer** without network: `hk_pkugu_pi_launch_e2e_test.go` drives
  the REAL claim-time seam (`ExportedResolveHarnessAgentTypeQuiet` →
  `ExportedResolveModelPreference` → `ExportedRoutedLaunchSpecBuilder` →
  `buildCodexRoutedLaunchSpec` → `PiHarness.LaunchSpec` → `buildPiLaunchSpec`),
  asserting on **argv** (`hkpkuguE2EArgFlagValue`, `:160-167`) and the **generated
  models.json** (`:170-183`), plus an adversarial counterfactual (`:185-202`); it
  never contacts a model. `hk_lfrub_dot_node_model_leak_test.go` builds a per-run
  ornith/`openai-completions` PiHarness via `ExportedNewPiHarness("pi",
  "ornith-provider","ornith","PI_KEY","","","openai-completions")` (`:56`).
- **The three scenarios are HERMETIC Go tests in `internal/daemon` (package
  `daemon_test`)** at the launch-spec/models.json/model_selected layer, auto-picked
  by the affected-package scenario gate (`scripts/scenario-gate.sh`; internal/daemon
  is always affected). NO YAML scenario authoring (no fake provider to talk to).
- **Billing bypass (hermetic, no live key):** (a) write a dummy key FILE, set it as
  `APIKeyFile` so the PI-040 guard sees a non-empty key (`hkpkuguE2EKeyFile`,
  `:68-78`); (b) `t.Setenv("HOME", t.TempDir())` so the PI-042 `~/.pi/auth.json`
  check is a no-op (`:109`). Lower-level builder tests can use
  `skipBillingGuard`/injected `piHome` (`pilaunchspec.go:147-166`).
- **The live `tool_calls` round-trip on the DGX tunnel is a SEPARATE operator
  canary** (the DoD proof), NOT part of the hermetic gate. State this explicitly so
  "e2e" is not over-claimed: the corpus proves everything up to the launch spec the
  real turn consumes; the model round-trip needs the loopback tunnel
  (`http://127.0.0.1:8551/v1`; srt blocks the LAN IP → OPTION A loopback only).
- Export seams: `ExportedNewPiHarness`, `ExportedNewHarnessRegistryWithPi(piCfg)`,
  `ExportedRoutedLaunchSpecBuilder`, `ExportedResolveHarnessAgentTypeQuiet`,
  `ExportedResolveModelPreference`, `ExportedNodeModelForHarness`,
  `ExportedEffectiveModel`, `ExportedClaudeRunCtx` (`export_test.go`). C3 adds a
  `resolvePiProfile` — export it as `ExportedResolvePiProfile` for these tests.
- Each new test file needs a UNIQUE helper prefix (implementer-protocol §), e.g.
  `hkppsToolcalls`, `hkppsNoLeak`, `hkppsDgx`.

## Approach

All three scenarios drive the REAL routed builder through
`ExportedRoutedLaunchSpecBuilder` (as `hk_pkugu` does), with the C3 profile
resolution in the loop, and assert on argv + generated models.json + injected env.
The billing guard is bypassed hermetically (dummy key file + `t.Setenv("HOME",
t.TempDir())`). No network, no live model.

### Scenario 1 — `pi_toolcalls_per_provider_test.go`

Build a `PiHarnessConfig` with TWO profiles:
- `openrouter-cloud`: `{provider: openrouter, model: openrouter/<id>, api_key_env:
  OPENROUTER_API_KEY}` — NO base_url.
- `ornith-dgx`: `{provider: <p>, model: <p>/<id>, api_key_env: PI_KEY, api_key_file:
  <dummy>, base_url: http://127.0.0.1:8551/v1, api: openai-completions}`.

Drive TWO beads through the real seam in the same test (proving concurrent
A→OpenRouter / B→ornith per-bead selection, re-scope #1):

- **OpenRouter bead** (label `profile:openrouter-cloud`): assert argv contains
  `--provider openrouter --model openrouter/<id>`, NO base_url, and NO models.json
  written — `os.Stat(<ws>/.harmonik/pi-agent/models.json)` errors (template:
  `TestPiHarness_BaseURL_ProductionPath_Absent`, `pilaunchspec_test.go:777-813`).
- **ornith bead** (label `profile:ornith-dgx`): assert argv `--provider <p> --model
  <p>/<id>`, AND the generated `<ws>/.harmonik/pi-agent/models.json` contains the
  loopback `baseUrl` + `api: openai-completions` (template:
  `TestPiHarness_BaseURL_ProductionPath_Present` `:703-763` and `_APIOverride`
  `:817-862`).

This is the two-provider "both work completely" success criterion made executable at
the hermetic layer.

### Scenario 2 — `pi_no_tier3_leak_test.go` (extends hk-pkugu)

Two sub-cases:
- **No-label pi bead:** a pi-resolved bead with NO `profile:`/`model:` label MUST NOT
  seal claude `sonnet`; the harness-global pi model/provider is used. Assert argv
  `--model` = the harness-global pi model (NOT `sonnet`), plus the adversarial
  counterfactual (re-drive with the claude-leaked model to prove the assertion is not
  vacuous — copy the `hk_pkugu` counterfactual pattern `:185-202`).
- **DOT-path variant (LOCKED C3-Q5):** drive the same no-label pi bead through
  `driveDotWorkflow` (or the exported DOT cascade seam) with a per-node `model=`
  attribute, and assert the provider tuple rides the cascade UNCLOBBERED — the
  per-node DOT `model=` pin is dropped for the pi family
  (`nodeModelForHarness`/`ExportedNodeModelForHarness`, `dot_cascade.go:1274-1281`)
  and the provider/base_url/api tuple is unchanged across node re-launches. This
  proves the tuple survives the cascade (constraint C3 Q5).

- **Unknown-profile refuse-to-launch (REQUIRED — C3 fail-loud e2e, integration-review
  finding #1):** drive a pi-resolved bead carrying `profile:does-not-exist` (a name
  absent from the config `Profiles` map) through the real claim-time seam and assert
  the WORKLOOP refuses to launch — NO LaunchSpec is built and the bead is routed to
  the `brAdapter.ReopenBead` refuse-before-launch path (assert the reopen call fires
  with the unknown profile named in the reason; assert no argv/launch-spec is
  produced). This test asserts the end-to-end workloop behavior, NOT merely that
  `resolvePiProfile` returns an error (that is C3's unit test
  `TestResolvePiProfile_UnknownProfile_FailLoud`). Suggested name
  `TestPi_UnknownProfile_WorkloopRefusesLaunch` (prefix `hkppsNoLaunch`). Use a fake
  or capturing `brAdapter` to observe the `ReopenBead` call without a live br.

Guards C3 requirement 2 (harness gate / no claude tier-3 leak into the pi tuple) and
C3 requirement 5 (unknown profile → fail loud, does NOT launch).

### Scenario 3 — `pi_dgx_reasoning_test.go` (hk-4ir08)

An `ornith-dgx` reasoning-model profile bead: hermetically assert the loopback launch
spec + generated models.json for the reasoning model (argv `--provider`/`--model`,
models.json `baseUrl` + `api: openai-completions`). Add a comment documenting that
the actual reasoning + `tool_calls` round-trip is a live-tunnel **operator canary**,
not part of this hermetic test — cite the DoD proof separately.

## Files & changes

| File | Change |
|------|--------|
| `internal/daemon/pi_toolcalls_per_provider_test.go` (new) | Scenario 1, helper prefix `hkppsToolcalls`. |
| `internal/daemon/pi_no_tier3_leak_test.go` (new) OR extend `hk_pkugu_pi_launch_e2e_test.go` | Scenario 2 (no-label + DOT variant), helper prefix `hkppsNoLeak`. |
| `internal/daemon/pi_dgx_reasoning_test.go` (new) | Scenario 3, helper prefix `hkppsDgx`. |
| `internal/daemon/export_test.go` | Add `ExportedResolvePiProfile` (and, if needed, an `ExportedNewHarnessRegistryWithPiProfiles` builder) seam. |

## Acceptance criteria

1. Scenario 1: both beads, driven through the real routed builder in one test, emit
   argv + models.json matching THEIR wire format (openrouter: no models.json; ornith:
   loopback models.json with `api: openai-completions`).
2. Scenario 2: the no-label pi bead's argv `--model` is the harness-global pi model,
   NOT `sonnet`; the adversarial counterfactual fails when fed the leaked model; the
   DOT-path variant shows the provider tuple unchanged across node re-launches; AND
   the unknown-profile sub-case proves the workloop builds NO LaunchSpec and routes
   the bead to `brAdapter.ReopenBead` (C3 fail-loud e2e, review finding #1).
3. Scenario 3: the ornith reasoning bead emits the loopback launch spec + models.json;
   a code comment records the live-tunnel operator canary as the separate DoD proof.
4. All three run hermetically (no network, no live key) via the dummy-key-file +
   `t.Setenv("HOME", t.TempDir())` bypass.
5. `scripts/scenario-gate.sh` picks them up automatically (internal/daemon affected);
   no YAML scenario is added.

## Verification

- `go test ./internal/daemon/ -run 'PerProvider|NoTier3Leak|DgxReasoning'` — green.
- `go test -tags=scenario ./internal/daemon/...` — green (scenario-tagged subset, if
  the tests carry the `//go:build scenario` tag; otherwise plain package tests suffice
  as they are in the always-affected `internal/daemon` package).
- `scripts/scenario-gate.sh` on the change → runs the new tests.
- **Operator canary (separate, NOT CI):** with the DGX loopback tunnel up, submit an
  ornith-profile bead and one openrouter-profile bead and confirm each drives a real
  `tool_calls` turn end-to-end. This is the DoD proof, recorded on the scenario-test
  bead, not gated by CI.

## Test beads to file (record IDs back into this spec after `br create`)

- `br create "scenario: pi-provider-switch — two-provider per-bead launch (openrouter + ornith)" --type task --label scenario-test`
  — names the harmonik dispatch seam under test (routed launch-spec builder), the
  bead lifecycle state (claim-time resolution → launch spec), and the observable
  terminal condition (argv + `.harmonik/pi-agent/models.json` content per provider).
- `br create "explore: pi-provider-switch — operator canary: real tool_calls per provider over DGX tunnel" --type task --label exploratory-test`
  — names the operator-facing surface (submit a `profile:`-labeled bead), the command
  (`harmonik queue submit`), and the expected side-effect (a real `tool_calls` turn
  on each of OpenRouter and the ornith loopback).

(IDs: _to be filled after `br create` — this spec does not create beads._)

## Error handling / edge cases

- **No live key in CI:** covered by the dummy-key-file + `HOME` temp-dir bypass.
- **Loopback unavailable in CI:** the hermetic tests never contact it; only the
  operator canary does.
- **Over-claiming "e2e":** explicitly bounded — hermetic = up to launch spec; the
  model round-trip is the operator canary.

## Migration / backwards compatibility

Test-only additions; no product behavior change. New export seam
(`ExportedResolvePiProfile`) is test-only (`export_test.go`).
