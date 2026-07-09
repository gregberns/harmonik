# C6 — Backward-compat regression pin — Change Spec

**Component:** C6 (mechanical). Pin that with no per-bead override, resolution +
launch are byte-identical to today's default openrouter/deepseek path.

## Requirements (from 03-components.md C6)

1. A bead with NO profile/model label, against the daemon-global config, produces
   `--provider openrouter --model deepseek/deepseek-v4-flash`, `api_key_env
   OPENROUTER_API_KEY`, no base_url, no models.json — byte-identical to pre-change.
2. The env allowlist strip output for the default path is unchanged (only
   `OPENROUTER_API_KEY` injected; siblings emitted as `KEY=`).
3. All four listed existing suites pass unmodified except where a signature change
   from C1/C4 requires a mechanical, behavior-preserving edit.

## Research summary (from 04-research/C6)

- Default-path output (per 03-components C6 req 1): argv `--provider openrouter
  --model deepseek/deepseek-v4-flash`, `api_key_env OPENROUTER_API_KEY`, no base_url,
  no models.json, env strip injecting ONLY `OPENROUTER_API_KEY` (siblings `KEY=`).
  Use a FIXED openrouter fixture config, NOT the live `.harmonik/config.yaml` (which
  is currently ornith/DGX per analysis:78) — for determinism.
- The zero-value=no-override mechanism is `rc.Model == "" ⇒ h.model`
  (`piharness.go:127-129`); C4 extends the same to the other five fields, so an
  unlabeled bead leaves all five `rc.*` empty and every field falls back to `h.*`.
- Green-must-stay-green suites (constraint §5): `internal/daemon/pilaunchspec_test.go`,
  `cmd/harmonik/resolve_pi_config_test.go`,
  `internal/daemon/harnessregistry_pi_hkf8u5j_test.go`,
  `internal/daemon/pi_retain_on_failure_hkj6wm7_test.go`, plus the two leak tests
  (`hk_pkugu`, `hk_lfrub`). C1 adds keyed-literal struct fields (additive, no edit
  forced). C4 adds override branches (behavior unchanged when `rc.*` empty). C2 adds
  a `Profiles` map (absent map valid). The ONLY plausible mechanical edit is a
  positional `RunCtx`/`piRunCtx` literal — grep confirms keyed literals throughout,
  so no signature break is expected.

## Approach

Add ONE golden/byte-comparison test pinning the default path, and confirm the
existing suites stay green. No product code changes in C6 — it is pure regression
protection proving C1–C4 did not disturb the zero-value path.

Build a `PiHarness` from a FIXED openrouter fixture (provider `openrouter`, model
`deepseek/deepseek-v4-flash`, api_key_env `OPENROUTER_API_KEY`, no base_url, no api),
call `LaunchSpec` with an **empty** `RunCtx` tuple (all five new fields `""`), and
assert:
- argv contains `--provider openrouter --model deepseek/deepseek-v4-flash`;
- NO `--base_url`-driven models.json — `os.Stat(<ws>/.harmonik/pi-agent/models.json)`
  errors (template: `TestPiHarness_BaseURL_ProductionPath_Absent`,
  `pilaunchspec_test.go:777-813`);
- the env strip injects ONLY `OPENROUTER_API_KEY` and emits every sibling provider
  key as `KEY=` empty-override (template: the `buildPiEnv` assertions in
  `pilaunchspec_test.go`).

## Files & changes

| File | Change |
|------|--------|
| `internal/daemon/pilaunchspec_test.go` (or new `pi_default_path_golden_test.go`) | Add `TestPiHarness_DefaultPath_ByteIdentical` (golden argv + env + no-models.json). |
| Existing suites | No edits expected. If C1/C4 forces a keyed-literal field addition anywhere, apply only a behavior-preserving edit. |

## Acceptance criteria

1. `TestPiHarness_DefaultPath_ByteIdentical` passes: empty-`rc` launch against the
   openrouter fixture yields the exact argv + env + no models.json documented above.
2. The env strip output injects only `OPENROUTER_API_KEY`; all sibling provider keys
   present as `KEY=`.
3. `internal/daemon/pilaunchspec_test.go`, `cmd/harmonik/resolve_pi_config_test.go`,
   `internal/daemon/harnessregistry_pi_hkf8u5j_test.go`,
   `internal/daemon/pi_retain_on_failure_hkj6wm7_test.go`, `hk_pkugu`, `hk_lfrub`
   all pass — unmodified, or with only a behavior-preserving keyed-literal edit.

## Verification

- `go test ./internal/daemon/... ./cmd/harmonik/...` — all green.
- `go test ./internal/daemon/ -run TestPiHarness_DefaultPath_ByteIdentical` — green.

## Test beads to file (record IDs back into this spec after `br create`)

- `br create "scenario: pi-provider-switch — default path byte-identical (openrouter/deepseek)" --type task --label scenario-test`
  — names the seam (`PiHarness.LaunchSpec` with empty RunCtx), the lifecycle state
  (default-path launch spec), and the observable terminal condition (golden argv +
  env + absence of `.harmonik/pi-agent/models.json`).
- `br create "explore: pi-provider-switch — unlabeled bead still launches openrouter/deepseek" --type task --label exploratory-test`
  — names the operator surface (submit a bead with NO `profile:` label) and the
  expected side-effect (argv `--provider openrouter --model deepseek/deepseek-v4-flash`).

(IDs: _to be filled after `br create` — this spec does not create beads._)

## Error handling / edge cases

None new. C6 is the guard that C3/C4 did not regress the zero-value path.

## Migration / backwards compatibility

C6 IS the backward-compatibility assertion. The zero-value-is-default discipline
(`piharness.go:127-129`, generalized by C4) plus additive struct growth (C1/C2)
guarantee identical bytes when no `profile:`/`model:` label is present.
