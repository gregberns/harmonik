<!-- DRAFT — additive companion to specs/crew-handoff-schema.md (2026-07-11
     captain-startup revamp, cutover Step 1.1 / 02-cutover-and-open-questions.md §2.6
     [B] "`## Current State` field contract ... currently has NO home — captain and
     crew can disagree on the block's shape"). Do NOT deploy as live.
     Landing notes:
       1. This file is ONLY the new "## 9. `## Current State` body-block contract"
          section below — land it by APPENDING to the live specs/crew-handoff-schema.md
          (after its existing §8 "Schema evolution"), not by replacing the whole file.
          Everything above the "---" divider in this draft is landing metadata, not
          spec body.
       2. Strip this HTML comment at landing.
       3. This is an ADDITIVE landing (cutover Step 1.1): the `## Current State` block
          already exists informally in live mission files (e.g.
          `.harmonik/crew/missions/admiral.md`, `commodore.md` — `queue_id`, `in_flight`,
          `next_action`) and in crew-launch/SKILL.md's and operating.md's prose
          references to it. This section is the first NORMATIVE field contract; it adds
          three fields (`monitor`, `blockers`, `translations`) that today's mission
          files omit, plus the "absent section" fallback rule. No existing reader
          breaks — an absent or partial block already falls back to re-derive today,
          this section just makes that fallback explicit and required.
       4. Once landed, crew-launch/SKILL.md draft L62-64 and L163-164's pointers to
          "field contract: specs/crew-handoff-schema.md" resolve to a real section.
-->

---

## 9. `## Current State` body-block contract

The mission-file body (below the frontmatter, §2) carries **exactly one**
`## Current State` heading. Unlike the six frontmatter fields (§3), which are
**captain-authored** and change only on re-task, this block is **crew-owned** — the
crew REPLACES it in place on every status change (boot, bead close, timer tick,
drain, park). It is never appended to or stacked; superseded content is not
retained in the file (`br comments list <epic_id>` and `comms log --from <crew_name>
--topic status` are the durable history a captain re-derives from if needed — see
`missions/kynes.md` purge note for the anti-pattern this forbids).

### 9.1 Field contract

Six fields, all **optional individually** (a crew fills in what it knows; an unknown
field is written as `(none)` or `(unknown)` rather than omitted, so the block's shape
stays predictable):

| Field | Meaning |
|---|---|
| `queue_id` | The daemon-minted `queue_id` from the crew's last `harmonik queue submit`, if any is currently live. `(none)` if nothing has been submitted since the last drain. |
| `in_flight` | The bead ID(s) currently dispatched and not yet terminal. `(none)` if the queue is empty. |
| `monitor` | Whether a Monitor (`harmonik subscribe`) is currently armed for this crew's queue, and on what event types. e.g. `armed: run_completed,run_failed,run_stale,heartbeat` or `not armed — re-arm on next dispatch`. |
| `next_action` | The single next thing the crew will do on resume — one line, plain English. This is the field a keeper-restart or captain re-task reads first. |
| `blockers` | Anything preventing forward progress right now: a dependency bead, a paused queue, an awaited captain decision. `(none)` if unblocked. |
| `translations` | Plain-English glosses for any bead IDs, codenames, or internal identifiers used elsewhere in this block or the goal — same purpose as the "Say the thing, not the pointer" principle: a private tracking ID is never the sole handle for a thing in a block a human or a fresh context may read. Omit this field entirely (not `(none)`) when nothing in the block needs translating. |

### 9.2 Worked example

```markdown
## Current State
queue_id: 4a9f2b31-...
in_flight: hk-8xspi, hk-qw63o
monitor: armed: run_completed,run_failed,run_stale,heartbeat
next_action: awaiting hk-8xspi/hk-qw63o completion; submit next batch from kerf next on drain.
blockers: (none)
translations: hk-8xspi = comms recv-cursor fix; hk-qw63o = idle-follow presence-beat fix.
```

### 9.3 Absent-section rule

If the mission file has NO `## Current State` section at all (e.g. a freshly
captain-authored handoff before the crew's first status write), the crew MUST treat
**all six fields as unknown** and re-derive rather than assume any default:

- `queue_id` / `in_flight` → `harmonik queue status` / `br ready --parent <epic_id>
  --limit 0` against beads with `assignee == $HARMONIK_AGENT` (or the `crew:<crew_name>`
  label fallback, §4).
- `monitor` → assume NOT armed; arm one before dispatching.
- `next_action` → derive from the frontmatter `goal` + live bead state, not from any
  memory of a prior session.
- `blockers` → re-check live queue/bead state; do not assume the last-known blocker
  still holds (it may have cleared).
- `translations` → nothing to translate yet; populate on the first write.

A partially-present block (some fields absent, others present) is treated field-by-field:
present fields are read as a CLAIM (verify against live state before acting on
anything stakes-bearing, per the digest-overrides-claims principle); only the
literally-absent fields are unknown-and-re-derive.
