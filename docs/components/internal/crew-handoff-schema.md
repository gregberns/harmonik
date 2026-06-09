# Crew Mission-Handoff Schema (component doc)

> **Normative reference:** `specs/crew-handoff-schema.md` — that is the
> byte-for-byte contract C2, C3, and C4 MUST agree on. This component doc is the
> knowledge-base entry linking the schema into the broader component map.

## Purpose

The mission-handoff file is the durable cross-component contract shared by three
components of the Captain & Crew system:

- **C4 / captain** — *writes* the handoff file for each crew it launches.
- **C2 (`harmonik crew start`)** — *reads* the `--mission <path>` and delivers
  the file to the crew via the paste seed.
- **C3 / crew** — *resumes into* the handoff (via `/session-resume`) and
  *re-hydrates* from its YAML frontmatter on keeper restart.

## Format summary

A Markdown file with a YAML frontmatter header. Path:
`.harmonik/crew/missions/<crew_name>.md` (gitignored).

Six required fields (see `specs/crew-handoff-schema.md §3` for full charset rules):

| Field | Meaning |
|---|---|
| `schema_version` | Must equal `1`. |
| `crew_name` | Crew's stable comms identity (`$HARMONIK_AGENT`). |
| `queue` | Crew's named queue. NEVER `main`. |
| `epic_id` | Assigned epic (parent bead). |
| `goal` | One-line plain-English mission statement. |
| `captain_name` | Captain's comms name for status reports. |

## Durable assignment mirror

On every epic adoption the crew mirrors:

```bash
br update <epic_id> --assignee <crew_name>
```

This is load-bearing for `epic_completed` attribution (the captain reads the
`assignee` field, not a registry entry). Must run at boot AND on every comms
re-task. See `specs/crew-handoff-schema.md §4`.

## Example

See `.harmonik/crew/missions/example-handoff.md` for a filled-in copy-paste
template (the `alpha` / `alpha-q` / `hk-tigaf` case).

## Related

- `specs/crew-handoff-schema.md` — normative spec (field contract, charset rules,
  error handling, schema evolution).
- `.claude/skills/crew-launch/SKILL.md` — the crew boot context that consumes
  this schema.
- `docs/plans/captain/05-specs/c3-spec.md` — C3 design: §3.1 handoff schema
  rationale.
