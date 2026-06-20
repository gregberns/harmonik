# Crew Mission-Handoff Schema

```yaml
---
title: Crew Mission-Handoff Schema
spec-id: crew-handoff-schema
status: draft
spec-shape: contract
spec-category: foundation-cross-cutting
version: 1.0.0
spec-template-version: 1.1
owner: captain-plan-author
last-updated: 2026-06-09
depends-on:
  - beads-integration
  - queue-model
  - event-model
---
```

## 1. Purpose and scope

This document is the **byte-for-byte contract** for the mission-handoff file that
flows between three components of the Captain & Crew system:

- **C4/captain** — *writes* the handoff file for each crew it launches.
- **C2 (`harmonik crew start`)** — *reads* the path (passed as `--mission`) and
  delivers the file to the crew via the paste seed:
  `"Please read <handoff-path> and run /session-resume on it, then begin your
  operating loop."`
- **C3/crew** — *resumes into* the handoff (via `/session-resume`), *parses* the
  YAML frontmatter to derive its operating parameters, and *re-hydrates* from it
  on keeper restart.

All three components MUST agree on this format, field names, charsets, and
required-field rules. Any breaking change requires a `schema_version` bump and
updates to all three readers/writers in a single atomic commit.

**This document does NOT own:** the crew-launch skill (`.claude/skills/crew-launch/SKILL.md`),
the captain operating context (`c4-spec.md`), C2's registry format
(`.harmonik/crew/<name>.json`), or the named-queue model (`specs/queue-model.md`).

---

## 2. On-disk format

The handoff is a **Markdown file with a YAML frontmatter header** followed by a
human-readable body.

```markdown
---
schema_version: 1
crew_name: alpha
queue: alpha-q
epic_id: hk-tigaf
goal: "Ship named-queues"
captain_name: captain
---

# Mission: <goal>

You are crew member **<crew_name>**, owning epic **<epic_id>** on queue
**<queue>**. Report status to **<captain_name>**.

<Optional free-text context the captain wants to convey: priorities, caveats,
links to design docs, etc. This section is NOT part of the machine contract —
it is human-readable guidance for the crew agent.>
```

### 2.1 Frontmatter parsing rules

- The frontmatter MUST begin on line 1 with exactly `---` (no leading whitespace,
  no trailing spaces).
- The frontmatter MUST be closed with a `---` line before any Markdown body.
- All six fields listed in §3 MUST be present. A handoff missing any required
  field is **invalid** — the crew MUST NOT parse partial frontmatter and dispatch.
- Field values are parsed as YAML. `goal` SHOULD be quoted (double-quotes) if it
  contains special YAML characters.
- The YAML parser MUST treat the body (below the second `---`) as Markdown prose,
  not YAML.

### 2.2 Path convention

Handoff files live at:

```
.harmonik/crew/missions/<crew_name>.md
```

`<crew_name>` is the same identifier as the `crew_name` field in the frontmatter.
The `.harmonik/` tree is gitignored — handoff files do NOT appear in `git status`
and are NOT committed to the repo.

**C4 (captain) is responsible for creating this file before calling
`harmonik crew start`.** C2 does not create or validate the handoff — it passes
the path as-is to the crew's paste seed.

---

## 3. Field contract

Six fields are **required**. One field is **optional** (additive, no schema_version
bump — per §8 additive-change rule).

| Field | Type | Charset / constraints | Required | Meaning |
|---|---|---|---|---|
| `schema_version` | integer | must equal `1` | yes | Schema version. Bump on any breaking field change. C2 and C4 MUST validate this equals 1 before acting. |
| `crew_name` | string | `[a-z0-9-]`, 1–64 chars | yes | The crew's stable identity. Equals its `$HARMONIK_AGENT` env var, its comms identity (`--from`/`--to`), and the registry `Name` field in `.harmonik/crew/<name>.json` (C2 §3.3). |
| `queue` | string | `[a-z0-9-]`, 1–64 chars | yes | The crew's named queue. Equals the registry `Queue` field and the on-disk file `.harmonik/queues/<queue>.json`. The crew MUST submit beads only to this queue, NEVER to `main`. |
| `epic_id` | string | opaque bead id (e.g. `hk-XXXXX`) | yes | The assigned epic. The parent bead whose ready children the crew dispatches. |
| `goal` | string | single line, no newlines | yes | Plain-English mission statement. Used in `/session-resume` framing and in crew status updates. |
| `captain_name` | string | `[a-z0-9-]`, 1–64 chars | yes | The captain's comms identity. The crew sends `--to <captain_name> --topic status` progress updates here. |
| `model` | string | `opus` \| `sonnet` \| `haiku` | **optional** | Claude model the crew should run on. When present, C2 (daemon) injects `--model <value>` on the crew's launch argv. Absent = no `--model` flag is added, so the crew inherits the launcher's configured default model (harmonik does not pin one). Decision rule: **sonnet** for lane-drain crews with file-disjoint clean beads (add mission clause "escalate to captain on ANY run_failed, do NOT self-classify"); **opus** for design / test / investigation. |

### 3.1 Charset enforcement

The charset `[a-z0-9-]` means: lowercase ASCII letters, ASCII digits, and hyphens
only. No uppercase, no underscores, no dots, no spaces. This matches the naming
rules for comms identities and named queues.

`goal` has no charset restriction beyond "single line" (no `\n` characters). It
SHOULD be human-readable plain English (no shell metacharacters).

---

## 4. Durable assignment mirror (REQUIRED)

The handoff file is *convenient* but is NOT the durable source of truth for the
crew-to-epic assignment. The handoff can be lost or stale across a keeper restart.

**On every epic adoption** — at initial boot AND on every `topic == assign` comms
re-task — the crew MUST mirror the assignment to beads:

```bash
br update <epic_id> --assignee <crew_name>
```

This is a **metadata-only write** (permitted by beads-cli write discipline — it is
NOT a terminal transition and does NOT touch bead status).

**Why this mirror is load-bearing (not just a restart convenience):** The captain
attributes a completed epic to its owning crew by reading `br show <epic_id>
--format json` and checking the `assignee` field. If the mirror is missing, the
captain reads a stale or empty assignee and mis-attributes the completion. The
mirror MUST run on EVERY epic adoption, not only on first boot.

**`--assignee` is the locked field.** C3, C4, and the crew MUST all use
`--assignee` for this mirror (not `--owner` or a custom field). If the installed
`br` does not support `--assignee`, fall back to:

```bash
br update <epic_id> --add-label crew:<crew_name>
```

(same metadata-write class, permitted). The re-hydrate read then checks for the
`crew:<crew_name>` label instead of the assignee field. This fallback is a
MUST-document impl-time branch — do not silently drop the mirror.

### 4.1 Re-hydration on keeper restart

On restart, the crew re-derives its operating parameters from the **union** of:

1. The handoff frontmatter at `.harmonik/crew/missions/<crew_name>.md`, AND
2. `br show <epic_id> --format json` → `assignee == crew_name` (or the label fallback).

If both sources agree, proceed normally. If they disagree, **prefer beads** —
beads is the durable roster (§A6 of the Captain & Crew analysis). If neither
source is available, post a `--topic error` message to the captain and idle on the
comms inbox.

---

## 5. Why `session_id` is NOT in the handoff

C2 mints and owns the `session_id` (stored in the crew registry
`.harmonik/crew/<name>.json`). The handoff is **captain-authored** and re-used
verbatim across keeper restarts. Embedding the `session_id` in the handoff would:

1. Make C4 responsible for a value C2 owns and that rotates on `--fork-session`.
2. Make the handoff stale after every restart (defeating its purpose as a durable
   re-hydration source).

C2 captures the `session_id` from `claude --remote-control` at launch time and
writes it to the registry. C4 receives the `session_id` as a return value from
`harmonik crew start` (informational only — C4 does not need to persist it).

---

## 6. Worked example

See `.harmonik/crew/missions/example-handoff.md` for a concrete filled-in example
(the `alpha` / `alpha-q` / `hk-tigaf` case) intended as a copy-paste template for
C2's smoke and C4's author.

The example frontmatter (with optional `model:` field):

```markdown
---
schema_version: 1
crew_name: alpha
queue: alpha-q
epic_id: hk-tigaf
goal: "Ship named-queues: multi-queue generalization of harmonik's single queue"
captain_name: captain
model: sonnet
---
```

The `model:` field is optional. Omit it to inherit the launcher's configured
default model (harmonik adds no `--model` flag). Include it when the captain has
decided this lane uses a specific model (e.g. Sonnet for lane-drain, file-disjoint beads).

This maps to:
- Crew `alpha` on queue `alpha-q`
- Working epic `hk-tigaf` (the named-queues epic)
- Status reports go to the `captain` comms identity

---

## 7. Invalid handoff handling

A handoff file is **invalid** if any of the following are true:

- The file does not exist at the seeded path.
- The frontmatter cannot be parsed as valid YAML.
- Any of the six required fields is absent.
- `schema_version != 1`.
- `crew_name`, `queue`, or `captain_name` contains characters outside `[a-z0-9-]`
  or has length 0 or > 64.
- `goal` contains a newline character.

On encountering an invalid handoff, the crew MUST:
1. Do NOT dispatch any beads.
2. Post `harmonik comms send --to <captain_name or broadcast> --topic error --
   "crew <crew_name>: invalid handoff at <path>: <reason>"`.
3. Idle on the comms inbox, waiting for the captain to re-seed a corrected handoff.

---

## 8. Schema evolution

`schema_version: 1` is the initial version. The version field enables forward
compatibility:

- **Additive changes** (new optional fields): MAY be introduced without a version
  bump if C2/C4 treat unknown fields as no-ops and the six required fields remain
  unchanged.
- **Breaking changes** (rename/remove a required field, change a charset): MUST
  bump `schema_version` to 2 (or higher) AND update C2, C3, and C4 in the same
  commit. A crew parsing `schema_version: 2` against this document MUST treat the
  handoff as invalid and post a `--topic error`.
- **Version negotiation**: not supported in v1 — the three components are
  updated together.
