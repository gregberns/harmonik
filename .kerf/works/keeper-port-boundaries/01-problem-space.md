# Keeper Port Boundaries

## Summary

The keeper has a pure cycle reactor, but its construction still uses two dependency systems. `CyclerConfig` holds both policy values and function-based effect seams. Named ports then adapt those function seams back into interfaces. Tests often build partial configurations with different defaults from production. This work will make construction clear and make integration tests use production policy by default.

## Goals

- Separate cycle policy from runtime dependencies.
- Give each port one clear source of change.
- Provide one production constructor.
- Provide one production-parity scenario builder for tests.
- Migrate in small steps without behavior changes.
- Reduce the chance that an integration test disables a production gate by accident.

## Non-goals

- Do not rewrite the cycle state machine.
- Do not change keeper thresholds or timeout policy.
- Do not split the package into new Go packages.
- Do not rewrite the full watcher loop in this work.
- Do not change the keeper command surface.

## Constraints

- Preserve current production behavior and on-disk formats.
- Keep existing constructors during a short migration when compatibility needs them.
- Use small consumer-owned interfaces.
- Avoid a new abstraction unless it removes an existing duplicate seam.
- Keep tests deterministic with the existing fake clock.

## Success criteria

- Production constructs the cycler through one dependency object.
- The cycle core no longer depends on function fields as a second injection system.
- Transcript and pane observation do not belong to the gauge file port.
- New integration scenarios start with production policy.
- A test must opt out when it disables an active production gate.
- Existing keeper tests pass without changed runtime behavior.
