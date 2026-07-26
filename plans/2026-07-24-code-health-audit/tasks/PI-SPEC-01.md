# PI-SPEC-01 — Reconcile Pi credential and agent-state policy

## Dispatch metadata

- Group / priority: Pi lifecycle / P0
- Execution profile: `sol_xhigh`
- Reviewer profile: `sol_xhigh`
- Depends on: `PI-00`
- Work type: kerf/spec security design

## Objective

Reconcile normative Pi requirements with supported Pi 0.80.3 behavior:
`PI_CODING_AGENT_DIR` or `~/.pi/agent`, provider-keyed `auth.json`, models
config env references, and the supported credential kinds/version provenance.
Decide whether OAuth-like persisted credentials deny launch. No production code.

## Exclusive lease

The project-local kerf work plus `specs/pi-harness.md` and directly dependent
Pi spec text. No source/tests or live Pi state mutation.

## Acceptance

PI-042/PI-050 and conformance scenarios name the correct directory/schema,
pin fixture/version provenance, define malformed/unreadable behavior, and decide
raw-key versus env-reference materialization. Review explicitly reconciles
`specs/pi-provider-switch.md`.

## Verification

Kerf/spec validation and independent security/config review.

## Escalate when

Surface credential-kind or compatibility decisions to the operator; do not
delegate policy to Pi.

## Return

Return finalized normative artifacts. **COMMIT EXPLICITLY.**

