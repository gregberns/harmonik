# Cycle Policy Research

## Questions

- Which policy fields does the reactor use?
- Which defaults are library defaults and which are production defaults?
- Which project patterns separate policy from effects?

## Findings

`internal/keeper/step.go` uses thresholds, timer lengths, retry limits, grace windows, and transcript windows. Identity and paths are not policy. The derived `hasRespawn` value is a capability, not policy.

`CyclerConfig.applyDefaults` defines library defaults. `cmd/harmonik/keeper_cmd.go` `buildKeeperConfigs` enables production values such as boot grace and transcript gates. The repository config can override both. Tests must name which policy level they use.

`internal/runloop/ports.go` `RunPorts` is the closest dependency-bundle pattern. `internal/codexinput/reactor.go` keeps pure policy apart from effects.

## Options

- Reuse `CyclerConfig` as policy. This keeps compatibility but preserves mixed concerns.
- Add `CyclePolicy` and `CycleEnv`. This gives clear ownership and supports staged migration.

Use the second option. Keep the legacy config as an adapter until callers migrate.

## Risks

Preserve zero-value sentinels for boot grace and transcript gates. Preserve derived force thresholds and maximum boot grace.
