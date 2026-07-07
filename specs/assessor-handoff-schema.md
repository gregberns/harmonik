# Assessor Mission-Handoff Schema

```yaml
---
title: Assessor Mission-Handoff Schema
spec-id: assessor-handoff-schema
status: draft
spec-shape: contract
spec-category: foundation-cross-cutting
version: 1.0.0
spec-template-version: 1.1
owner: admiral-plan-author
last-updated: 2026-07-06
depends-on:
  - crew-handoff-schema
  - beads-integration
  - queue-model
  - event-model
---
```

## 1. Purpose and scope

This document is the **byte-for-byte contract** for the mission-handoff file that
spawns an **assessor** — the merge/deploy-gate verifier in the quality-system
initiative. It flows between three components:

- **Admiral** — *writes* the handoff file for each per-epic gate it opens. (The
  admiral, not the captain, spawns the assessor: gate authority is admiral-owned
  per `07-assessor-severity-framework.md` §6 and the assessor `soul.md` role
  split.)
- **`harmonik crew start assessor`** (C2 / daemon) — *reads* the path (passed as
  `--mission`) and delivers the file to the assessor via the paste seed. Note the
  assessor is spawned through the `crew start` verb with `HARMONIK_AGENT=assessor`
  (`crew.ResolveType("assessor")` resolves the bare type folder), NOT through
  `harmonik start`, which only knows `captain|crew`
  (`08-assessor-wireup-plan.md` headline).
- **Assessor** — *resumes into* the handoff, *parses* the YAML frontmatter to
  derive its gate parameters (`.harmonik/agents/assessor/operating.md` step 1
  parses `{branch, epic_id, gate}`), and *re-hydrates* from it on keeper restart.

All three MUST agree on this format, field names, charsets, and required-field
rules. Any breaking change requires a `schema_version` bump and updates to all
three readers/writers in a single atomic commit.

**This document does NOT own:** the assessor manifest
(`.harmonik/agents/assessor/soul.md` / `operating.md` / `manifest.yaml`), the
severity→P-level→found-by mapping (`07-assessor-severity-framework.md`), the
remediation disposition labels (`09-remediation-loop-design.md`), the block query
mechanics (owned by `operating.md` §Merge-gate step 6), or the crew registry
format (`.harmonik/crew/<name>.json`).

---

## 2. Why this is a SEPARATE schema from the crew handoff

The assessor is **not a crew** for handoff purposes, even though it is launched
through the `crew start` verb. The crew schema (`specs/crew-handoff-schema.md` v1)
mandates exactly six fields — `schema_version, crew_name, queue, epic_id, goal,
captain_name` — with **no `branch`, no `gate`, no `commit`**. But the assessor's
load-bearing inputs are precisely `branch` (the branch-under-test), `gate` (which
of the two gates to run), and — for the deploy gate — `commit` (the GATE-0 target
SHA). Reusing the crew schema would leave every one of these in the unparseable
free-text body, exactly the gap `08-assessor-wireup-plan.md` §Gap A calls out.

The two schemas also differ in **role wiring**: a crew reports to a `captain_name`
and drains a named `queue`; an assessor reports to the **admiral** over
`--topic gate` and submits to **no queue at all** (it files findings as beads and
self-terminates — `operating.md` §Bounds: "Never … submit to any queue"). Folding
gate fields into the crew schema would also force every crew reader to carry
assessor-only fields it never uses. Keeping them separate lets each schema stay a
tight contract for exactly one role. This schema deliberately **mirrors the crew
schema's STRUCTURE** (purpose, on-disk format, field-contract table, parsing
rules, invalid-handling, evolution) so both are read and maintained the same way.

---

## 3. On-disk format

The handoff is a **Markdown file with a YAML frontmatter header** followed by a
human-readable body.

```markdown
---
schema_version: 1
assessor_name: assessor-core-loop-proof
epic_id: hk-xxxxx
branch: integration/core-loop-proof
gate: merge
found_by_sources: [assessor, admiral, fast-follow]
report_path: .harmonik/reports/core-loop-proof-gate.md
spawned_by: admiral
---

# Gate: <gate> — <branch>

You are the **assessor** for epic **<epic_id>** on branch **<branch>**. Run the
**<gate>** gate on an isolated scratch clone, file findings as scoped
`found-by:assessor` beads, post `<PASS|BLOCK>` to **<spawned_by>** over
`--topic gate`, and self-terminate.

## Current State

<Free-text context the admiral wants to convey: the acceptance behavior the epic
claims, the known-RED corpus cells this gate must exercise, deploy preconditions
(for a deploy gate), links to the epic's design docs. This section is NOT part of
the machine contract — it is human-readable guidance.>
```

A **deploy-gate** handoff additionally sets `gate: deploy` and MUST carry a
`commit`:

```markdown
---
schema_version: 1
assessor_name: assessor-core-loop-proof-deploy
epic_id: hk-xxxxx
branch: integration/core-loop-proof
gate: deploy
commit: 632406ce9b1d4f0a...
found_by_sources: [assessor, admiral, fast-follow]
report_path: .harmonik/reports/core-loop-proof-deploy.md
spawned_by: admiral
---
```

### 3.1 Frontmatter parsing rules

- The frontmatter MUST begin on line 1 with exactly `---` (no leading whitespace,
  no trailing spaces) and MUST be closed with a `---` line before any body.
- All required fields in §4 MUST be present. A handoff missing any required field
  — or missing `commit` when `gate == deploy` — is **invalid**; the assessor MUST
  NOT run the gate on partial frontmatter (mirrors `operating.md` step 1:
  "Missing/invalid → do NOT run the gate; post `--topic error` to the admiral and
  idle").
- Field values are parsed as YAML. `found_by_sources` is a YAML sequence.
  `report_path` SHOULD be quoted if it contains special YAML characters.
- The parser MUST treat the body (below the second `---`) as Markdown prose, not
  YAML. The `## Current State` heading the assessor reads is body prose, not a
  frontmatter field.

### 3.2 Path convention

Handoff files live at:

```
.harmonik/crew/missions/<assessor_name>.md
```

(the same missions tree crews use — `<assessor_name>` is the frontmatter
`assessor_name`). The `.harmonik/` tree is gitignored: handoff files do NOT appear
in `git status` and are NOT committed.

**The admiral is responsible for creating this file before calling
`harmonik crew start assessor --mission <path>`.** C2 does not create or validate
the handoff — it passes the path as-is to the paste seed.

---

## 4. Field contract

Eight fields; seven are unconditionally **required**, `commit` is **required iff
`gate == deploy`**.

| Field | Type | Charset / constraints | Required | Meaning |
|---|---|---|---|---|
| `schema_version` | integer | must equal `1` | yes | Schema version. Bump on any breaking field change. C2 and the assessor MUST validate this equals 1 before acting. |
| `assessor_name` | string | `[a-z0-9-]`, 1–64 chars | yes | The assessor instance's stable identity. Convention `assessor-<epic>` (or `assessor-<epic>-deploy`). Equals its comms `--from`/`--to` identity and the mission filename stem. NOTE: the bare launch resolves `HARMONIK_AGENT=assessor` regardless (`08` "Optional CODE changes"); `assessor_name` is the human/handoff-level instance label, not the agent-type folder. |
| `epic_id` | string | opaque bead id (e.g. `hk-XXXXX`) | yes | The epic under gate. Doubles as the **scope label** the assessor attaches to every finding and the block query filters on (`--label <epic_id>` — `08` Gap B), because beads have no branch field. |
| `branch` | string | git ref, e.g. `integration/<epic>` | yes | The branch-under-test. The scratch clone/daemon (`scripts/scratch-daemon.sh`) is stood up on this ref. NEVER `cd`-ed into — operated via `git -C`. |
| `gate` | string | `merge` \| `deploy` | yes | Which gate to run. `merge` → `operating.md` §Merge-gate (LT+XT+CR, deterministic found-by block). `deploy` → §Deploy-gate (GATE-0 e2e on `commit` + 24h-reliability preconditions). |
| `commit` | string | full git SHA | **iff `gate == deploy`** | The GATE-0 target commit the deploy e2e reproduces the changed behavior on. MUST be present when `gate == deploy`; MUST be omitted (or ignored) when `gate == merge` (a merge gate tests the branch tip, not a pinned commit). |
| `found_by_sources` | sequence of string | each `[a-z0-9-]` | yes | The known `found-by:` sources the block query unions over via `--label-any` (`07` §3 wire-up gotcha: `found-by:*` does NOT glob — enumeration is load-bearing). Current set: `assessor, admiral, fast-follow`. Keeping this in the handoff makes the enumerated set explicit per gate; a missed source is a silent false-PASS. |
| `report_path` | string | repo-relative path | yes | Where the assessor writes the deploy-readiness report (`operating.md` §Verdict step 1). Referenced in the `--topic gate` verdict message. Convention `.harmonik/reports/<epic>-gate.md`. |
| `spawned_by` | string | `[a-z0-9-]`, 1–64 chars | yes | The comms identity that opened the gate and holds the merge/deploy decision — normally `admiral`. The assessor posts its boot status, `--topic error` (on invalid handoff), and `--topic gate` verdict here. |

### 4.1 Charset enforcement

`[a-z0-9-]` = lowercase ASCII letters, digits, and hyphens only — no uppercase,
underscores, dots, or spaces. Applies to `assessor_name`, each element of
`found_by_sources`, and `spawned_by`, matching comms-identity and label naming
rules. `commit` is a hex SHA (`[0-9a-f]`). `branch` and `report_path` may contain
`/` and `.` as ordinary path/ref characters.

---

## 5. Why `queue` and `captain_name` are NOT in the handoff

The assessor **submits to no queue** — it files findings as beads and
self-terminates (`operating.md` §Bounds). There is nothing for a `queue` field to
name, unlike a crew's mandatory named queue. Likewise there is no `captain_name`:
the assessor reports to the **admiral** (`spawned_by`), not a captain — the
captain never gates. Omitting both keeps the contract honest about the assessor's
actual wiring rather than inheriting crew fields it would leave dead.

The `--queue` the launch command passes (`--queue assessor-<epic>-q`,
`08` runbook step 2) is a C2 launch-mechanics argument, not a handoff field: the
assessor never dispatches from it. It exists only because `crew start` requires a
queue argument; it is not part of this contract.

---

## 6. Durable epic-scope mirror (recommended)

Unlike a crew, the assessor does **not** own an epic long-term, so the crew
schema's `--assignee` mirror (`crew-handoff-schema.md` §4) does not apply — the
assessor must never take ownership of the epic it grades (independence is
load-bearing; `operating.md` §Bounds). The durable link that matters here is the
**`<epic_id>` scope label on each finding bead**: because beads carry no branch
field, `--label <epic_id>` is what makes the block set per-branch (`08` Gap B).
The handoff's `epic_id` is therefore the single source for that scope label, and
the assessor re-derives it from the frontmatter on every keeper restart. If the
frontmatter is unavailable on restart, the assessor posts `--topic error` to
`spawned_by` and idles (it does NOT guess the scope, which would corrupt the
block set).

---

## 7. Worked example

```markdown
---
schema_version: 1
assessor_name: assessor-core-loop-proof
epic_id: hk-9x1qz
branch: integration/core-loop-proof
gate: merge
found_by_sources: [assessor, admiral, fast-follow]
report_path: .harmonik/reports/core-loop-proof-gate.md
spawned_by: admiral
---

# Gate: merge — integration/core-loop-proof

You are the **assessor** for epic **hk-9x1qz** on branch
**integration/core-loop-proof**. Run the merge gate (LT + XT + CR) on an isolated
scratch clone, file each confirmed defect as a scoped `found-by:assessor` bead
(`--label hk-9x1qz` + the `07`/`09` severity + disposition labels), post
`<PASS|BLOCK>` to admiral over `--topic gate`, and self-terminate.

## Current State

The epic's acceptance behavior: per-bead/DOT integration-branch targeting drives a
harness assertion (T10 `hk-xke2i`) that REDs today. Confirm it flips green.
Known-RED corpus cells to exercise: ... Deploy preconditions: N/A (merge gate).
```

This maps to: assessor instance `assessor-core-loop-proof`, gating epic
`hk-9x1qz` on `integration/core-loop-proof`, running the **merge** gate, unioning
findings over `found-by:{assessor,admiral,fast-follow}`, writing its report to
`.harmonik/reports/core-loop-proof-gate.md`, and reporting to `admiral`.

Launch:

```
harmonik crew start assessor \
  --queue assessor-core-loop-proof-q \
  --mission .harmonik/crew/missions/assessor-core-loop-proof.md
```

---

## 8. Invalid handoff handling

A handoff is **invalid** if any of the following hold:

- The file does not exist at the seeded path.
- The frontmatter cannot be parsed as valid YAML.
- Any unconditionally-required field (§4) is absent.
- `gate == deploy` but `commit` is absent (or malformed / not a hex SHA).
- `gate` is neither `merge` nor `deploy`.
- `schema_version != 1`.
- `assessor_name`, any `found_by_sources` element, or `spawned_by` contains
  characters outside `[a-z0-9-]`, or has length 0 or > 64.
- `found_by_sources` is empty (an empty union → the block query matches nothing →
  silent false-PASS; this is the `07` §3 wire-up gotcha and MUST be rejected).

On an invalid handoff the assessor MUST:

1. Do NOT stand up the scratch daemon and do NOT run the gate.
2. Post `harmonik comms send --from <assessor_name> --to <spawned_by> --topic
   error -- "assessor <assessor_name>: invalid handoff at <path>: <reason>"`.
3. Idle on the comms inbox, waiting for the admiral to re-seed a corrected
   handoff. (Mirrors `operating.md` step 1.)

---

## 9. Schema evolution

`schema_version: 1` is the initial version.

- **Additive changes** (new optional fields): MAY be introduced without a version
  bump if C2 and the assessor treat unknown fields as no-ops and the required-field
  set is unchanged.
- **Breaking changes** (rename/remove a required field, change a charset, change
  the `commit`-required-iff-`deploy` rule): MUST bump `schema_version` to 2+ AND
  update the admiral author, C2, and the assessor in the same commit. An assessor
  parsing an unrecognized `schema_version` MUST treat the handoff as invalid and
  post `--topic error`.
- **Version negotiation** is not supported in v1 — the components are updated
  together.
```
