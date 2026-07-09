# C6 — Backward-compat regression pin — Research findings

Mechanical component. Pin that with no per-bead override, resolution + launch are
byte-identical to today's default openrouter/deepseek path.

## Research questions

1. What is the exact default-path output that must stay byte-identical?
2. Which existing suites must stay green, and where might C1/C4 force mechanical edits?
3. What is the mechanism that guarantees the zero-value path is unchanged?

## Findings

**Q1 — default-path output.** With NO profile/model label against the daemon-global
config, the launch must produce (per 03-components C6 req 1): argv
`--provider openrouter --model deepseek/deepseek-v4-flash`, `api_key_env
OPENROUTER_API_KEY`, no base_url, no models.json, and the env allowlist strip
injecting ONLY `OPENROUTER_API_KEY` (siblings emitted as `KEY=`). The precise
default values come from `.harmonik/config.yaml` (the OpenRouter block; ACTIVE
config is currently ornith/DGX per analysis:78 — the pin should use a fixed
openrouter fixture, not the live config, to stay deterministic). The
zero-value=no-override mechanism is `rc.Model == "" ⇒ h.model`
(piharness.go:127-129); C4 extends the same to the other five fields, so an
unlabeled bead leaves all five `rc.*` empty and every field falls back to `h.*`.

**Q2 — existing suites + mechanical-edit risk.** Green-must-stay-green
(constraint §5): `internal/daemon/pilaunchspec_test.go`,
`cmd/harmonik/resolve_pi_config_test.go`,
`internal/daemon/harnessregistry_pi_hkf8u5j_test.go`,
`internal/daemon/pi_retain_on_failure_hkj6wm7_test.go`, plus the two leak tests
(hk_pkugu, hk_lfrub). C1 adds fields to `RunCtx` (keyed struct literals →
additive, no edit forced). C4 adds override branches inside `LaunchSpec` (behavior
unchanged when rc.* empty → existing assertions hold). The ONLY plausible
mechanical edits are if any test constructs `RunCtx`/`piRunCtx` positionally rather
than by keyed literal — grep confirms keyed literals throughout
(pilaunchspec_test.go, hk_pkugu), so no signature break is expected. If C2 changes
`PiHarnessConfig` by ADDING a `Profiles` map field only, resolve_pi_config_test.go
stays green (absent map is valid).

**Q3 — guarantee mechanism.** The zero-value-is-default discipline
(piharness.go:127-129, generalized by C4) plus additive struct growth (C1/C2). No
existing code path reads the new fields unless the profile resolver (C3) populates
them, and C3 only populates on a `profile:` label. Absent label = identical bytes.

## Pattern to mirror / test to pin
- A golden/byte-comparison test on the default-path launch spec + env: build a
  PiHarness from an openrouter fixture config, LaunchSpec a bead with empty
  RunCtx tuple, assert argv + env + no models.json — exactly the shape of
  TestPiHarness_BaseURL_ProductionPath_Absent (pilaunchspec_test.go:777-813) plus
  an env-strip assertion (only OPENROUTER_API_KEY injected; siblings `KEY=`).
- Confirm the four listed suites + two leak tests pass unmodified (or with only a
  behavior-preserving keyed-literal edit).

## Risks / open decisions
- None blocking. Use a fixed openrouter fixture (not the live ornith config) for
  determinism. This pin is the guard that C3/C4 didn't regress the zero-value path.
