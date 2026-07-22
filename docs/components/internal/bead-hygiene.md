# Bead-Filing Hygiene Protocol

> **Purpose.** Every bead filed by any agent (assessor, crew, captain, admiral, reviewer,
> solo orchestrator) must be *consistent*, *correctly set up*, and *aligned with a project
> goal* the moment it lands. This doc is the normative filing contract: a priority rubric, a
> required-label set, a title convention, a goal-alignment check, and a pre-`br create`
> checklist. It is checkable — an auditor can hold a bead against it and get a yes/no.
>
> **Scope.** Filing metadata only (priority, labels, type, title, description, goal link).
> It does NOT touch the daemon's write-discipline contract — terminal status transitions
> (`claim`/`close`/`reopen`) remain daemon-owned per
> [`beads-cli` skill](../../../.claude/skills/beads-cli/SKILL.md) and
> `specs/beads-integration.md §4.4`. Re-labeling and re-prioritizing via
> `br update --add-label` / `--remove-label` / `--priority` are **non-terminal** and are
> the permitted way to bring an existing bead into compliance.

---

## 1. Priority rubric (P0–P4)

Assign priority by **blast radius × how much it blocks forward motion**, not by how annoying
it is. One axis: "if we never fixed this, what breaks and how widely?"

| P | Bar | Examples |
|---|---|---|
| **P0** | **Fleet-down / data-loss / security, active now.** Everything else stops until this is fixed. | Disk fills and wedges every crew; ledger corruption; a live credential leak. |
| **P1** | **Critical-path blocker.** A *proven* bug that wedges a run / crew / lane, blocks a NAMED initiative or a release gate, or is a correctness bug with fleet-wide blast radius. Also: a **required pre-deploy E2E test** for a shipped fleet-impacting fix (per the PRE-DEPLOY E2E TEST GATE). | Worker-slot leak that permanently stalls a group (`hk-3hozm`); orphan-sweep double-dispatch after SIGKILL (`hk-nddg1`); keeper watcher dies with no auto-revive (`hk-220lv`); E2E backfill for a fleet-wedge fix (`hk-98at0`). |
| **P2** | **Significant bug, bounded blast radius or has a workaround.** Degrades reliability/correctness of ONE subsystem; does not wedge the fleet. Release-gating campaign findings that aren't fleet-down. Dormant/unwired feature behind a spec. | Unbounded in-memory buffer growth (`hk-hi53s`); false-green auto-close on SSH drop (`hk-cjqyn`); runtime policy loading unwired (`hk-1dgy0`). |
| **P3** | **Localized / latent / hygiene.** Real but low blast radius: pre-existing/latent defects, test-fragility/flakes, spec-drift lint, non-blocking reviewer follow-ups, corpus/schema drift. | Wall-clock test flake (`hk-3dn16`); pre-existing corpus lint fail (`hk-uhxwd`); supervise-stop edge case, reviewer-flagged non-blocking (`hk-goi0a`). |
| **P4** | **Backlog / nice-to-have.** No user-visible or fleet impact; deferrable indefinitely. | Cosmetic cleanups; speculative refactors nobody is blocked on. |

**Bug-severity tie-breakers** (bump toward the higher P):
- Reproduced/subagent-confirmed > theorized. A latent-but-unreachable defect drops one level (tag `known-issue`).
- Fleet-wide or cross-crew blast radius > single-subsystem.
- Silent-corruption / false-green > loud-crash (a false pass is worse than an honest failure).
- On the critical path of a NAMED initiative or a release gate > backlog-only.

---

## 2. Required labels

A bead's `issue_type` is intrinsic (`bug|task|feature|epic|chore|docs|question`) — not a
label. On top of type, apply labels from the taxonomy below. **Reconcile to this taxonomy;
do not invent a new label without adding it here first.**

### 2.1 Mandatory-when-applicable

| Class | Form | When REQUIRED |
|---|---|---|
| **Provenance** | `found-by:<agent>` | A reviewer/assessor/watcher filed it as a *finding* (not the owner picking up planned work). `found-by:assessor` is in use; `found-by:<agent>` generalizes the same pattern. |
| **Kerf codename** | `codename:<work>` | The bead belongs to a kerf work. `<work>` MUST exactly match a real work in `kerf map` (e.g. `codename:2026-07-14-agent-input-substrate`, NOT a bare `codename:agent-input-substrate` that matches no work). Program-plan labels that are established but not kerf works (e.g. `codename:code-revamp` for the freeze-and-carve program) are grandfathered. |
| **Campaign** | `assessor-campaign-<sha>` | Filed by an assessor campaign; `<sha>` is the pinned tree the campaign ran against. Pairs with `found-by:assessor`. |
| **Disposition** | `remediation:blocking` \| `known-issue` | On any `found-by:assessor` bead: exactly ONE. `remediation:blocking` = must be fixed for the campaign's gate. `known-issue` = latent / pre-existing / lower-weighted / accepted-for-now. Every campaign bead carries one or the other — never neither, never both. |

### 2.2 Functional / topical (bare — no prefix)

Observed in the open corpus (tally as of 2026-07-18). Use the existing tag; add a new one
here only when a genuinely new area recurs:

- **Initiative/area:** `fleet-reliability` (fleet-wide infra/wedge defects — disk, ledger, dispatch), `keeper-reliability` (keeper-specific subset), `keeper`, `comms`, `spec-drift`
- **Test kind:** `scenario-test`, `exploratory-test`

Bare functional labels tag *area or initiative*. A pure ranked-backlog bug with no area
affiliation MAY carry no functional label — that is allowed, provided §4 goal-alignment holds
(it points at the ranked backlog).

---

## 3. Title convention

From the existing corpus, a good title is: **`<subsystem>: <symptom> (<qualifier>)`**.

- **Subsystem prefix**, lowercase, colon-separated: `workloop:`, `bootreconcile:`,
  `tmuxsubstrate remote runWait:`, `queue append:`. Lets a reader route the bead at a glance.
- **Symptom, not fix.** State what is wrong and its consequence, not the patch.
  Good: "remote worker slot leak — refuse-before-launch early returns bypass ReleaseSlot
  defer". Bad: "add ReleaseSlot to the refuse path".
- **Consequence after an em-dash / arrow** when it clarifies blast radius:
  "… -> exitCodeClean(0), SSH drop mid-run = false-green auto-close".
- **Qualifier in parens** for latency/status: `(LATENT/pre-existing)`, `(twin)`, `(needs separate Enter)`.
- **Provenance prefix `[found-by:assessor]`** may lead the title on assessor findings (mirrors the label; it is a convention, not a substitute for the label).
- **Never make an opaque ID the handle.** `hk-…`, a SHA, or a codename may appear as context, but the title must still say *what the thing is* on its own.

Every bead MUST have a non-empty `description` that a reader who didn't file it can act on
(repro, file:line, root cause, or the residual work). A title-only bead (`description: null`)
is a filing defect — complete it, don't ship it.

---

## 4. Goal-alignment check

A filed bead must be traceable to one of:

1. A **named initiative** in `.harmonik/crew/admiral-initiatives.md` (Codex-as-crew, Fleet
   comms/keeper reliability, freeze-and-carve, the assessor release-gate campaign) — via a
   `codename:` / area label or the campaign label.
2. The **ranked backlog** — `kerf next` ranks it; it needs no initiative, but it must be a
   real, actionable unit (not a vague "look into X").

A bead that maps to **neither** is an **orphan** — flag it: either attach the right
initiative/area label, re-scope it into an actionable backlog item, or (if it's noise) it
shouldn't have been filed. "Belongs to the ranked backlog" is a valid answer; "belongs to
nothing" is not.

---

## 5. Pre-`br create` checklist

Run this before every `br create` (and it is the exact set an auditor re-checks):

1. **Type** — correct `--type` (`bug` for a defect, `task` for planned work, `chore` for
   maintenance, `feature`/`epic`/`docs`/`question` as apt).
2. **Priority** — set per the §1 rubric (blast radius × blocks-motion), not by gut annoyance.
3. **Title** — `subsystem: symptom (qualifier)`; symptom-not-fix; self-describing (no bare ID as the handle).
4. **Description** — non-empty and actionable (repro / file:line / root cause / residual).
5. **Provenance** — `found-by:<agent>` if a reviewer/assessor/watcher filed it as a finding.
6. **Codename** — `codename:<work>` if it belongs to a kerf work, and `<work>` matches `kerf map` exactly.
7. **Campaign + disposition** — assessor findings: `assessor-campaign-<sha>` + exactly one of `remediation:blocking` / `known-issue`.
8. **Area label** — a bare functional tag from §2.2 if it has an area; reconcile to the taxonomy (don't invent).
9. **Goal link** — can you name the initiative or say "ranked backlog"? If neither, it's an orphan — fix before filing.

---

## 6. Sources

- `.claude/skills/beads-cli/SKILL.md` — the CLI surface + daemon write-discipline (this doc is the *filing* layer on top; it never overrides write-discipline).
- `AGENTS.md` / `CLAUDE.md` §"Beads Workflow Integration" — priority meanings, types, the `codename:<name>` convention.
- `.harmonik/crew/admiral-initiatives.md` — the named-initiative registry the §4 alignment check points at.
- `specs/beads-integration.md §4.4` — terminal-transition ownership (out of scope here, referenced for the boundary).
</content>
</invoke>
