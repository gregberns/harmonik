# Assessor handoff schema research findings

## Questions

1. Is readiness a new gate type or a constrained deploy gate?
2. How do assessor reply routing and activation authority differ?
3. Which inputs must be machine-readable and durable?

## Findings

`specs/assessor-handoff-schema.md` allows only `merge` and `deploy`. Only
`deploy` requires a full commit. `spawned_by` is the current comms target and
gate holder. The readiness decision belongs to the operator. The schema
validates frontmatter only. `report_path` is the sole artifact reference, yet
`.harmonik/reports/` is ignored. `assessor/operating.md` parses only branch,
epic ID, and gate.

## Patterns and risks

A branch is context, while a full commit pins the candidate. Body prose cannot
prevent an assessor from accepting a broader canary. A free-text readiness
convention would bypass schema validation. A report only in ignored storage is
not durable evidence.

## Design constraints

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
