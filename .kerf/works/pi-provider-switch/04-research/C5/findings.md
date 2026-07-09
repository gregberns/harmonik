# C5 — Two-provider e2e harness corpus — Research findings

Prove BOTH OpenRouter (cloud, bare `openai`, no base_url) AND DGX/ornith (loopback,
`openai-completions` + base_url) reach the launch with correct per-bead tuple, and
re-affirm the two prior leak fixes. Three new scenarios.

## Research questions

1. How do the existing pi e2e tests stand up a fake/loopback provider and what do
   they assert (launch spec / model / models.json / tool_calls)?
2. What export seams and billing-guard bypasses let a test run without a live key?
3. Is an ornith/DGX loopback exercisable IN-TEST, or does a real tool_calls turn
   need the live tunnel?
4. How does the scenario gate hook these tests (YAML scenario vs Go test)?
5. What is needed to template the three new scenarios?

## Findings

**Q1 — existing e2e template + what they assert.** Two Go tests in
`internal/daemon` (package `daemon_test`) are the templates:
- `hk_pkugu_pi_launch_e2e_test.go` drives the REAL claim-time seam verbatim
  (`ExportedResolveHarnessAgentTypeQuiet` → `ExportedResolveModelPreference` →
  `ExportedRoutedLaunchSpecBuilder` → buildCodexRoutedLaunchSpec →
  PiHarness.LaunchSpec → buildPiLaunchSpec). It asserts on **argv** (`--model` value
  via `hkpkuguE2EArgFlagValue`, lines 160-167) and on the **generated models.json**
  content (reads `<ws>/.harmonik/pi-agent/models.json`, lines 170-183). It also runs
  an **adversarial counterfactual** (lines 185-202): re-drives the SAME path with
  the claude-leaked model to prove the assertions aren't vacuous. **It never
  contacts a model** — no tool_calls; it stops at the launch spec.
- `hk_lfrub_dot_node_model_leak_test.go` builds a per-run ornith/`openai-completions`
  PiHarness (`ExportedNewPiHarness("pi","ornith-provider","ornith","PI_KEY","","",
  "openai-completions")`, line 56) and asserts via `ExportedNodeModelForHarness` +
  `ExportedEffectiveModel` that a claude per-node pin is dropped for a pi harness
  (routes to configured "ornith"). Pure resolution assertion, no launch, no model.

**Q2 — export seams + billing bypass.** Seams in export_test.go:
`ExportedNewPiHarness`, `ExportedNewHarnessRegistryWithPi(piCfg)`,
`ExportedRoutedLaunchSpecBuilder`, `ExportedResolveHarnessAgentTypeQuiet`,
`ExportedResolveModelPreference`, `ExportedNodeModelForHarness`,
`ExportedEffectiveModel`, `ExportedClaudeRunCtx`. Billing guard is defeated
hermetically WITHOUT a live key by: (a) writing a dummy key FILE and setting it as
`APIKeyFile` (hkpkuguE2EKeyFile, lines 68-78 → the resolved key is non-empty so the
PI-040 guard passes); (b) `t.Setenv("HOME", t.TempDir())` so the PI-042 on-disk
`~/.pi/auth.json` check is a no-op (line 109). pilaunchspec_test.go additionally
has a `skipBillingGuard`/injected `piHome` piRunCtx path (pilaunchspec.go:147-166)
for the lower-level builder tests.

**Q3 — loopback exercisable in-test? NO real turn without the tunnel.** The
existing corpus proves BOTH wire formats **at the launch-spec layer** without any
network: the ornith path is asserted purely by the generated models.json
(`baseUrl`, `api: openai-completions`) and argv; the cloud path by the ABSENCE of
models.json + the argv. There is **no in-test fake HTTP provider** anywhere in the
pi tests — a genuine `tool_calls` round-trip requires the live DGX loopback tunnel
(`http://127.0.0.1:8551/v1`, per MEMORY: srt blocks the LAN IP, so OPTION A
loopback is the only working path). **Recommendation:** template the three
scenarios at the launch-spec + env + models.json assertion layer (exactly what
pkugu/lfrub do) — "asserts the launch spec + env that a real turn consumes"
(03-components C5 req 1's stated fallback). A live `tool_calls` turn is an
operator-run canary against the tunnel, NOT a CI-hermetic test. Flag this: the
"reaches the model with real tool_calls end-to-end" success criterion is provable
hermetically only up to the launch spec; the actual model round-trip is a manual
canary (matches how the pi flip was proven per MEMORY — single-mode canary is the
discriminator).

**Q4 — scenario gate wiring.** `scripts/scenario-gate.sh` is an
affected-package-scoped, FAIL-OPEN commit gate: it computes changed Go packages vs
the merge base and runs `go test ./pkg/...` then re-runs the scenario-tagged subset
under `-tags=scenario` (header lines 1-45). It mirrors
`internal/daemon/scenariogate.go`. The `scenarios/` dir holds YAML
(regression/smoke/_workflows) driven by `test/scenario/scenarios_test.go` under
`//go:build scenario` — but **there are NO pi YAML scenarios today**; all pi e2e
coverage is plain Go tests in `internal/daemon`. **The three new scenarios should
be Go tests in `internal/daemon`** (package `daemon_test`), following pkugu/lfrub.
They are picked up automatically by the affected-package gate (internal/daemon is
always affected by this change) — no YAML/scenario-gate authoring needed. A
YAML-level scenario would require a fake provider the harness can actually talk to,
which does not exist (Q3).

**Q5 — what to template for the three scenarios.**
1. **pi-toolcalls-per-provider** — two beads dispatched via the real routed builder:
   - OpenRouter bead: profile with provider `openrouter`, no base_url →
     assert argv `--provider openrouter --model openrouter/<id>`, NO base_url,
     and NO models.json written (assert `os.Stat(.../models.json)` errors, per
     TestPiHarness_BaseURL_ProductionPath_Absent, pilaunchspec_test.go:777-813).
   - ornith bead: profile with base_url + `api: openai-completions` →
     assert argv + generated `.harmonik/pi-agent/models.json` with `baseUrl` +
     `api: openai-completions` (per TestPiHarness_BaseURL_ProductionPath_Present
     :703-763 and _APIOverride :817-862). Both driven through
     `ExportedRoutedLaunchSpecBuilder` with the C3 profile resolution in the loop,
     proving concurrent A→OpenRouter / B→ornith per-bead selection (re-scope #1).
2. **pi-no-tier3-leak** (hk-pkugu) — a pi-resolved bead with NO profile/model label
   MUST NOT seal claude `sonnet`; extend the existing pkugu test to also cover a
   profile-labeled bead and (per C3 Q5) a DOT-path variant.
3. **pi-dgx-reasoning** (hk-4ir08) — an ornith/DGX profile bead: hermetically,
   assert the loopback launch spec + models.json for the reasoning model; the
   actual reasoning+tool_calls round-trip is a live-tunnel operator canary (Q3).

## Risks / open decisions for change spec
- **Hermetic vs live boundary (Q3)** — the corpus proves launch-spec/models.json/env
  hermetically; the real model round-trip needs the DGX tunnel and is an operator
  canary, not CI. State this explicitly so "e2e" isn't over-claimed. Flag to operator.
- **No fake HTTP provider exists** — building one is out of scope; don't invent it.
- Helper-prefix discipline (implementer-protocol §): each new test file needs a
  unique helper prefix (e.g. `hkNNNNN...`), like `hkpkuguE2E` / `hklfrub`.
- Reuse the dummy-key-file + `t.Setenv("HOME", t.TempDir())` billing bypass verbatim.
