# Integration — Queue dogfood readiness

## Cross-reference checks

Checked the drafted identifiers against the source corpus and the affected
specification set. `QM-058`, `QM-059`, `EM-053a`, `PL-032`, `PL-033`, `ON-052`,
`WM-041`, and `EV-051` are new identifiers in the drafts. Existing links in
each complete draft are preserved from its source document. The affected-spec
names occur across the current corpus, so the amendment set does not orphan a
referenced specification.

## Consistency checks

- Queue-model owns failed recovery and its receipt.
- Execution-model owns the durable terminal-recovery record.
- Run-state-machine owns the in-process terminal spine.
- Operator NFR and process lifecycle own ordering and command behavior.
- Workspace-model owns retained recovery evidence and adoption.
- Event-model owns observation after persistence.
- The assessor schema and runbook own the restricted audit input.

The selected terminology is consistent: *terminal-recovery record* means the
durable post-commit record, *recovery receipt* means the durable failed-queue
result, and *readiness gate* means the separate assessor audit. Neither record
is an event-log substitute.

## Resolved apparent contradictions

The prior `queue resume` behavior remains drain resume. PL-032 adds a distinct
failed-recovery surface. Existing reopen behavior remains a new run and
worktree. WM-041 applies only to the retained terminal ladder. The general
scratch harness remains broad; its new readiness procedure is narrow.

## Changelog and corpus assessment

`05-changelog.md` accounts for all nine drafts. The source-spec list was
checked for every affected name and the draft changes introduce no removed
target. The full amendment set is coherent and ready for task decomposition.
