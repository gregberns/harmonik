# Research — Beads Encoding for `workflow_mode`

**Research question:** What mechanism does Beads (`br` CLI) provide for storing per-task `workflow_mode`?

**Bottom line:** Use a label with the `workflow:` prefix (e.g., `workflow:ralph`, `workflow:dot`, `workflow:single`). This matches an existing harmonik convention and requires no schema changes.

## What Beads exposes

**Bead schema (observed via `br show --format json`):**
- `id`, `title`, `description`, `status`, `priority`, `issue_type`
- `created_at`, `created_by`, `updated_at`
- `source_repo`, `compaction_level`, `original_size`
- `labels` (array)

**Create/update flags:**
- `--labels` (comma-separated, repeatable)
- `--priority`, `--type`, `--description`, `--body`, `--assignee`, `--owner`, `--due`, `--estimate`, `--external-ref`

**Not available:** custom fields, extensible key-value metadata, a generic `metadata` blob. The schema is fixed.

## Existing harmonik convention

Harmonik already uses `prefix:value` label syntax throughout the bead corpus:
- `spec:architecture`, `spec:beads-integration` (spec-parent linkage)
- `kind:spec-parent`, `tag:mechanism` (categorization)
- `scope:bootstrap`, `area:daemon` (role/area)
- `parallelism-prep`, `exploratory-finding`, `tester-T6` (phase/stage)

Labels are multi-value and queryable via `br search --labels <value>`.

## Recommendation: `workflow:<mode>` label

| Option | Verdict |
|---|---|
| (a) Label prefix `workflow:ralph` etc. | **Recommended.** Matches existing idiom, multi-value capable, native CLI support, no schema changes, queryable. |
| (b) Dedicated metadata field | **Not available.** Beads has no such mechanism. |
| (c) Body-embedded marker (HTML comment) | **Rejected.** Fragile, unparseable in JSON queries, doesn't survive bead-body rewrites. |

Operational shape:

```
br create --title "..." --labels "workflow:ralph,area:daemon"
br update <id> --add-label "workflow:ralph"        # set
br search --labels "workflow:ralph"                # query
```

## Implications for spec drafts

- **C5 (beads-integration spec):** Defines `workflow:<mode>` label syntax. Allowed values: `single`, `ralph`, `dot`. Multiple `workflow:` labels on one bead is undefined (last-wins or error — design pass picks; recommend "error at adapter").
- **C2 (execution-model spec):** When daemon reads the bead in its claim path, it scans labels for the `workflow:` prefix, extracts the mode, and seeds it into the Run record. Resolution precedence remains (task → project → daemon → default).
- **Reader path:** Beads adapter (BI-004) returns labels as part of the ready-work response payload (already does, per JSON schema). No new adapter surface needed.
- **Writer discipline:** Per BI-004 write-discipline, agents MUST NOT modify a bead's `workflow:` label. Only operators set it at creation/triage time, or the daemon sets it where a workflow's design intent so dictates (e.g., reconciliation workflows might set themselves to a fixed mode).

## Open design question carried forward

- **Multi-`workflow:` label collision.** Recommend: adapter validates at read time; bead with two conflicting `workflow:` labels is a hard error logged to JSONL as an O-class `bead_label_conflict` event, daemon falls back to project default. Design pass C5-D should decide.
