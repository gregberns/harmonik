# Research — Assessor handoff schema

## Questions

1. Who creates the assessor mission?
2. Can the assessor consume normal queue work?
3. What makes a handoff valid?
4. Where can the canary evidence contract live?

## Findings

- Section 3.2 assigns mission creation to the admiral. This work adds the
  earlier operator-authorization condition.
- Sections 2 and 5 state that the assessor consumes no normal queue work. Its
  launch queue is a launch detail only.
- Section 4 requires schema version, assessor name, epic, branch, gate,
  evidence sources, report path, and sender. A deploy gate also requires a
  full commit.
- Sections 3.1 and 8 make invalid frontmatter stop the gate before scratch
  startup. The assessor reports the error and idles.
- `report_path` is the durable report destination. The schema has no structured
  artifact, canary-limit, or controlled-load fields. The mission body must
  state them under `## Current State`.

## Patterns to keep

- Tracked missions use schema version 2 and put containment and proof limits in
  their body.
- The assessor uses an isolated scratch daemon and a reasoned verdict. It does
  not pass only because a bead count is low.

## Risks and decisions

- Do not create, reset, or retire the assessor registry during planning.
- The later mission must use the planned mission and report paths only after
  the operator grants authority.
