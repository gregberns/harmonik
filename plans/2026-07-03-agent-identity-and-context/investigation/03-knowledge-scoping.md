# 03 — Knowledge Scoping Investigation

## 1. AGENTS.md (== CLAUDE.md symlink) — section inventory & role relevance

`AGENTS.md` is 179 lines / ~12.5 KB. It self-describes as a **ROUTER**, not a state file (line 11):
"it points you at the right contract; it does not restate one." Structurally split into a managed
router block (lines 7–48) and unmanaged legacy prose (lines 50–179).

| Section (lines) | Content | Who actually needs it |
|---|---|---|
| Precedence (9–11) | orchestrator-rules skill > AGENTS.md > per-domain skills; state in `.harmonik/context/` + HANDOFF | ALL (short, correct as shared) |
| **Per-role load map (13–26)** | The explicit per-role scoping table (see §2) | ALL — this is the routing core |
| Start here (28–34) | AGENT_INDEX→STATUS→captain-lanes→HANDOFF order; launch verbs; D2 positional-XOR-flags rule | admiral/captain (launch); implementer-orchestrator (reading order). **Crew/queue-workers do NOT need launch-verb detail.** |
| Standing rules → orchestrator-rules (36–47) | Router bullets: daily loop, monitoring, CWD, comms, lifecycle, daemon-redeploy, keeper | admiral/captain + implementer-orchestrator. **Over-shared to crew.** |
| Planning with kerf (50–52) | kerf spec-first, codename labels | captain/admiral + planners |
| Key conventions (54–60) | specs/ normative, kerf bench paths, KB, 10 locked decisions, codename labels | Mixed: everyone (specs) vs planner-only (bench paths) |
| Don't (62–66) | don't reopen decisions, don't add abstraction, don't skip reading order | ALL (short) |
| **Beads Workflow Integration (72–129)** | kerf-vs-bv, `br` commands, workflow pattern, session protocol | Largely superseded by **beads-cli** + **harmonik-dispatch** skills. ~58 lines inline. |
| **UBS Quick Reference (131–179)** | Ultimate Bug Scanner install + full command/severity reference | Only agents that COMMIT code. ~49 lines, largest single block. Captain/watch/keeper/queue-workers never run `ubs`. |

### Content ALL agents see but only SOME need (the over-share)

- **UBS block (131–179, ~49 lines)** — pre-commit bug scanner. Irrelevant to captain, watch,
  keeper, daemon queue-workers. Matches operator's "queue worker doesn't need X" example.
- **Beads Workflow Integration (72–129, ~58 lines)** — full `br`/kerf command dump duplicated by
  `beads-cli` + `harmonik-dispatch` + kerf skills. Router (lines 40, 44) already points there; this
  legacy block restates the detail, violating the file's own "does not restate a contract" rule.
- **Launch verbs / stack management (30–34)** — `harmonik start captain|crew`, D2 flag rules.
  Crews and queue-workers do not launch the fleet.
- **kerf planning + bench-path conventions (50–57)** — planner/captain detail.

Together roughly **lines 50–179 (~130 of 179) are legacy implementer-and-committer detail** the
managed router block (7–48) already delegates to skills. That tail is the bulk of "too large."

## 2. Existing per-role scoping (the "Per-role load map", AGENTS.md 13–26)

Scoping ALREADY exists and is explicit. Each role "loads only its slice":

- **Captain — cold boot** (→ `captain/STARTUP.md`): identity guard → tier-3 `project.yaml` →
  tier-2 `captain-lanes.md` → `captain/SKILL.md` + `orchestrator-rules` + HANDOFF → boot digest.
  **Explicitly does NOT boot-read `AGENT_INDEX.md`, `STATUS.md`, product/docs KB, full skill bodies** (line 23).
- **Captain — keeper-restart (LEAN):** re-drain comms → tier-3/2 + one digest → trust cache → re-arm.
- **Crew — minimal load** (→ `crew-launch/SKILL.md`): mission file + `crew-launch` + `agent-comms`
  + `beads-cli` + `harmonik-dispatch`. **Explicitly does NOT load ROADMAP, captain-lanes,
  project.yaml, orchestrator-rules, STATUS, HANDOFF, KB** (line 25) — scoped to ONE epic + ONE queue.
- **Implementer-orchestrator:** `AGENT_INDEX → STATUS → HANDOFF` + `orchestrator-rules` + `harmonik-dispatch`.

### STARTUP.md load steps (captain-specific, deepest scoping)

`captain/STARTUP.md` (891 lines) is itself heavily scoped and repeatedly tells the captain to read LESS:
- Step 1 (107–138): **"DO NOT full-read AGENT_INDEX.md / STATUS.md / TASKS.md at boot (M5/hk-039z
  — context economy)"** (130) — the general CLAUDE.md reading order is for the implementer-orchestrator, NOT the captain.
- "SLIM COLD-BOOT" (116): don't eager-load full `agent-comms`/`harmonik-dispatch` bodies — boot-critical content is already in `orchestrator-rules`.
- Keeper: a ~15-line cheatsheet (406–430) replaces the full 484-line keeper SKILL.md.
- LEAN resume (530–567): keeper-restart trusts cached tier-2/3; digest is the single verify pass.

## 3. AGENT_INDEX.md and STATUS.md — KB entry structure & size

- **AGENT_INDEX.md** — 183 lines / ~16 KB. Pure master map / two-hop index (Problems P01–07, Goals
  G01–07, Concepts, Components, Subsystems S01–09, Ideas, Specs, etc). Entry = **ID | name(link) |
  summary** tables. Routing index, not content.
- **STATUS.md** — 112 lines / ~9 KB. Phase + locked-decisions + burst log + spec-corpus table +
  "where to start next." Entry = dated bulletins newest-first.
- Both are implementer-orchestrator boot reads, and both are **explicitly excluded from captain's
  and crew's boot** (AGENTS.md 23, 25; STARTUP 130) — the sharpest existing example of correct scoping.

## 4. Assessment: "everyone reads AGENTS.md + role loads its slice via skills"?

**Partially — and that's exactly where the over-sharing lives.**

The *intended* model (line 11 + Per-role load map + STARTUP Step 1): **AGENTS.md is a thin router;
each role loads only its slice via its skill.** The managed block (7–48) honors this — short,
delegates to `orchestrator-rules`, `crew-launch`, `captain`, `beads-cli`, `harmonik-dispatch`,
`keeper`, `harmonik-lifecycle`.

The over-sharing is the **legacy unmanaged tail (lines 50–179, ~72% of the file)**, which every
agent that reads AGENTS.md sees in full:
1. **UBS reference (49 lines)** — code-committing implementers only.
2. **Beads Workflow Integration (58 lines)** — duplicates skills the router already points to.
3. **Launch/stack-startup + kerf bench conventions** — captain/admiral/planner detail leaking in.

**Net:** the router design is sound; the per-role load map + STARTUP already enforce real scoping
(captain & crew each skip the KB, STATUS, most skill bodies). The fix the operator reaches for is to
**finish the router migration** — move the UBS block and Beads Workflow block out of AGENTS.md into
the roles/skills that need them (UBS → a commit/implementer skill; Beads → already in `beads-cli`),
leaving AGENTS.md as the ~48-line managed router it claims to be. The operator's three examples all
map onto content still sitting in that shared legacy tail rather than in a role-scoped skill.
