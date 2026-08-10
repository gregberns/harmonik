# Test Scenario Research

## Questions

- Where do current tests drift from production?
- Which builder pattern fits the repository?
- Which effects must scenarios record?

## Findings

The keeper has 73 config literals and 65 test calls to `NewCycler`. Existing builders copy selected defaults. They omit active production gates.

Library defaults, command defaults, and repository-effective values are distinct. The scenario builder should use compiled command production policy. Command tests should cover repository overlays.

`internal/keepertest/l2_integration_test.go` already provides virtual timer scheduling. The new builder can extend this pattern with narrow recording ports and one fake clock.

The builder should require a reason for methods that disable active gates. State changes that exercise a gate do not need a reason.

## Required records

Scenarios record ordered pane effects, context writes, handoff operations, journal writes, events, respawn calls, and timers. Transcript entries include role, origin, time, and visible-text status.

## Risks

A builder based only on `applyDefaults` must not call itself production parity. It would disable boot grace and transcript gates.
