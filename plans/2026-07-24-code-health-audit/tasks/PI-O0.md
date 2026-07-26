# PI-O0 — Capture redacted Pi failure-event fixtures

## Dispatch metadata

- Group / priority: Pi resilience / P1
- Execution profile: `pi_ralph`
- Reviewer profile: `sol_xhigh`
- Depends on: `PI-SPEC-01`
- Work type: deferred characterization

## Objective

Capture/version/redact real supported Pi 429/404/auto-retry/error NDJSON shapes.
Do not infer a classifier from undocumented events.

## Exclusive lease

Versioned test fixtures and evidence only; no production code or secrets.

## Acceptance

Fixtures name Pi version/source, contain no credential/prompt content, and
distinguish confirmed from absent signals.

## Escalate when

If safe capture is unavailable, leave PI-071 unconfirmed.

## Return

Commit accepted redacted fixtures explicitly.

