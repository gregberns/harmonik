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

---

## Folded in from lane bravo's parallel pass (2026-08-03)

Lane bravo ran the same pass on branch `work/queue-dogfood-readiness` before any
lane contract named that branch. The two passes reached the same shape. Bravo's
text is kept below because it names evidence, measurements and review records
this document does not. The plan of record stays T1..T12 plus T5a in
`07-tasks.md`. Where the two disagree on behaviour, the section above wins.

### Assessor handoff schema research findings

#### Questions

1. Is readiness a new gate type or a constrained deploy gate?
2. How do assessor reply routing and activation authority differ?
3. Which inputs must be machine-readable and durable?

#### Findings

`specs/assessor-handoff-schema.md` allows only `merge` and `deploy`. Only
`deploy` requires a full commit. `spawned_by` is the current comms target and
gate holder. The readiness decision belongs to the operator. The schema
validates frontmatter only. `report_path` is the sole artifact reference, yet
`.harmonik/reports/` is ignored. `assessor/operating.md` parses only branch,
epic ID, and gate.

#### Patterns and risks

A branch is context, while a full commit pins the candidate. Body prose cannot
prevent an assessor from accepting a broader canary. A free-text readiness
convention would bypass schema validation. A report only in ignored storage is
not durable evidence.

#### Design constraints

- Select a new readiness type or define one exact deploy encoding.
- Keep `spawned_by` for reply routing and add a distinct operator decision
  owner.
- Require the candidate commit, its reachability from branch, a machine-readable
  canary profile, and an artifact map.
- The profile includes one identified item and stream group, no append, local
  active repository, concurrency one, no remote, Pi, cross-repo, or wave, and
  the repeat-safety limit.
- Store report and evidence in tracked or declared durable storage. A new
  required field needs a schema version change and matching author, validator,
  and parser changes.
