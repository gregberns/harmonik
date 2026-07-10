# pi-provider-switch — two-provider per-bead launch: scenario-test DoD

Bead: `hk-m6uu2.8` (T-SCENARIO, jig-required scenario-test bead for the
`pi-provider-switch` epic, `hk-m6uu2`). Per the task decomposition
(`07-tasks.md` T-SCENARIO), neither the plan bead nor its implementation
beads may close until this bead closes. This doc is the DoD record.

## What this proves

The routed launch-spec builder resolves each bead's `profile:` tuple at
claim time and emits per-provider `argv` + `.harmonik/pi-agent/models.json`
— for both an OpenRouter-profile bead and an ornith/DGX-profile bead,
dispatched through the same harness registry, with no cross-provider
leakage and no claude-tier-3 model leak into the pi family.

## Hermetic scenario corpus (authored under C5-wiring, hk-m6uu2.6)

All five scenarios below live in `internal/daemon` (package `daemon_test` /
`daemon`), require no network, and are picked up automatically by
`scripts/scenario-gate.sh` — `internal/daemon` is always in the affected-package
set for any bead touching this epic, so no `//go:build scenario` tag is needed.

| Test | File | Proves |
|---|---|---|
| `TestPiToolcallsPerProvider_TwoBeadsSameRegistry` | `pi_toolcalls_per_provider_test.go` | An OpenRouter-profile bead and an ornith/DGX-profile bead, dispatched through the same registry, each produce argv + models.json matching their own wire format. |
| `TestPiNoTier3Leak_NoLabelBead_UsesHarnessGlobalModel` | `pi_no_tier3_leak_test.go` | A no-label pi bead never seals the claude tier-3 default. |
| `TestPiNoTier3Leak_DotPathVariant_ProviderTupleUnclobbered` | `pi_no_tier3_leak_test.go` | The per-node claude `model=` pin is dropped for the pi family, so the provider tuple survives DOT node re-launch unclobbered (locked C3-Q5). |
| `TestPi_UnknownProfile_WorkloopRefusesLaunch` | `pi_unknown_profile_refuse_test.go` | A `profile:does-not-exist` label refuses launch via `brAdapter.ReopenBead` before any launch spec is built (fail-loud, C3). |
| `TestPiDgxReasoning_LoopbackLaunchSpecAndModelsJSON` | `pi_dgx_reasoning_test.go` | An ornith/DGX reasoning-model bead hermetically reaches the loopback launch spec + models.json. |

Plus the C6 regression pin, re-verified here as part of the same gate:

| Test | File | Proves |
|---|---|---|
| `TestPiHarness_DefaultPath_ByteIdentical` | `pilaunchspec_test.go` | A bead with no `profile:`/provider labels produces a byte-identical default-path argv + env to pre-epic behavior. |

## Verification

```
go test -run 'TestPiToolcallsPerProvider_TwoBeadsSameRegistry|TestPiNoTier3Leak_NoLabelBead_UsesHarnessGlobalModel|TestPiNoTier3Leak_DotPathVariant_ProviderTupleUnclobbered|TestPi_UnknownProfile_WorkloopRefusesLaunch|TestPiDgxReasoning_LoopbackLaunchSpecAndModelsJSON|TestPiHarness_DefaultPath_ByteIdentical' -v ./internal/daemon/...
```

All six PASS (re-run 2026-07-08 against this worktree's HEAD).

## Live-DGX operator canary — NOT included here

The bead description names "the live-DGX operator canary result (real
`tool_calls` per provider over the loopback tunnel)" as DoD proof to record
on this bead. That canary requires an operator-attended run against a real
DGX loopback tunnel (`http://127.0.0.1:8551/v1`) and real provider
credentials — it cannot be executed hermetically from a worktree agent
session, and no such run has been performed yet. It is tracked separately
as `T-EXPLORE` (`hk-m6uu2.7`, not yet dispatched): `explore:
pi-provider-switch — operator canary: real tool_calls per provider over DGX
tunnel`. Recording an untested canary result here would be a fabrication;
this section exists so that gap is explicit rather than silently assumed
closed.

## hk-4ir08 resolution note

`hk-4ir08` originally read the ornith/DGX reasoning-model path as a stalled
0-byte hang (content:null, no tool_calls) — a protocol/capability gap. A
later in-daemon canary (run `019f4365-e7cb`, 2026-07-08) proved the opposite:
Pi over DGX/ornith completes end-to-end through the daemon — real `write`
tool_calls, a real commit — it is just slow. `agent_ready` legitimately takes
~20 min on the reasoning path (reasoning latency ahead of the first
tool_call), which is well inside the 30-min never-spawned reaper but past the
3-min `agent_ready_stall_detected` default, so every healthy run on this
profile tripped a spurious stall alarm that read as a hang.

The residual fix (this bead) adds a per-bead
`agent_ready_stall_threshold=<seconds>` label override (mirroring the
existing `stale_after=` / `never_spawned_timeout=` overrides) in
`internal/daemon/stalewatch.go` — beads dispatched against a reasoning-model
pi profile (e.g. `profile:ornith-dgx-reasoning`) can widen the detection
window to match the profile's real latency instead of alarming on every
healthy run. No model swap or protocol adaptation is required; options
(a)/(b)/(c) from the original bead description are moot. The live-DGX
operator canary above remains the separate, not-yet-run DoD proof for the
wider epic.
