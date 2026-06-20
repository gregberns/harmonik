# Root Instruction & State Doc Audit — 2026-06-20

Audit of 18 root-level INSTRUCTION and STATE documents in `/Users/gb/github/harmonik`.
Dates are last-meaningful-update (git `%ci` for tracked files; mtime + content cues for
untracked). Live crews per registry (`.harmonik/crew/*.json`): **chani, irulan, jamis,
logmine, paul**. Today is 2026-06-20.

## Findings table

| File | Last update | Purpose (1-line) | TIER | Staleness (evidence) | Concern-mixing | Duplication | Recommendation |
|---|---|---|---|---|---|---|---|
| **HANDOFF.md** | 2026-06-10 (commit) / mtime newer; body dated "STATE 2026-06-20 keeper campaign COMPLETE" | Per-session authoritative current-state + next-steps for the main/captain session | short-term/operational + behavioral (DIRECTIVES block) | CURRENT (content is 2026-06-20, status CLEAN, no daemon) | YES — jams a 4-rule "ORCHESTRATION DIRECTIVES — DO NOT EDIT" behavioral block on top of ephemeral session state | Directives block duplicates AGENTS.md / orchestrator-rules.md; state overlaps STATUS.md | KEEP (canonical handoff) but SPLIT the durable DIRECTIVES block out into orchestrator-rules.md |
| **HANDOFF-captain.md** | mtime Jun 20 07:22 (untracked, gitignored) | Captain-session handoff w/ keeper nonce, 3-day scale-out posture | short-term/operational + behavioral | CURRENT (dated 2026-06-20, post-incident) | YES — same DIRECTIVES block + posture directives + live state | Heavy overlap w/ HANDOFF.md directives + captain-lanes.md posture | KEEP (gitignored, live captain). Not a repo-cleanup target — it's local-only working state |
| **HANDOFF-chani.md** | mtime Jun 20 10:22 (untracked, gitignored) | Crew handoff for chani (remote-substrate lane) | short-term/operational | CURRENT (chani is a LIVE crew; dated 2026-06-20, BLOCKED status) | Low — single-lane state | Overlaps `.harmonik/crew/missions/chani.md` (the durable mission) | KEEP (live crew, gitignored) — but see narrative: per-crew handoffs belong under `.harmonik/crew/handoffs/`, not repo root |
| **HANDOFF-codexcrew.md** | mtime Jun 9 (untracked, gitignored) | Crew handoff for codexcrew (codex-harness lane) | short-term/operational | **DEAD** — body says "STOOD DOWN CLEAN 2026-06-09"; codexcrew is NOT a live crew; 11 days stale | Low | Codex status also in MEMORY (codex_harness_design) | ARCHIVE/DELETE — dead lane, stood-down, no live crew |
| **HANDOFF-controlpoints.md** | 2026-06-09 (TRACKED — committed!) | Crew handoff for controlpoints lane (concurrency-wedge fix) | short-term/operational | **DEAD** — controlpoints "stood down"; not a live crew; 11 days stale; the wedge fix it describes long-since landed | Low | Concurrency-fix narrative now in postmortems + MEMORY | DELETE from git — the ONLY committed dead per-crew handoff; pollutes repo history for all clones |
| **HANDOFF-flywheel.md** | mtime Jun 9 (untracked, gitignored) | Crew handoff for flywheel lane (releases+CI) | short-term/operational | **DEAD** — "STATUS @ stand-down 2026-06-09"; flywheel not a live crew; 11 days stale | Low | — | ARCHIVE/DELETE — dead lane |
| **HANDOFF-named-queues.md** | mtime Jun 9 (untracked, gitignored) | Crew handoff for named-queues lane (daemon/queues) | short-term/operational | **DEAD** — "STOOD DOWN clean 2026-06-09"; #1 item (deploy hk-togxq) is 11 days old and surely landed; not a live crew | Low | — | ARCHIVE/DELETE — dead lane, stale deploy directive is actively misleading |
| **STATUS.md** | 2026-06-10 | Higher-level structural project-state summary | short-term/operational (+ historical) | STALE — "Last updated 2026-06-10"; active-lanes table predates current chani/paul/etc lanes; 10 days behind reality | YES — current phase + active lanes + historical Phase-0/1 blocks layered together | Overlaps HANDOFF.md (state), docs/INITIATIVES.md (lanes board), ROADMAP.md | MERGE active-lanes into docs/INITIATIVES.md; keep STATUS.md as a thin "what harmonik is + phase" pointer, or ARCHIVE history into a phases doc |
| **TASKS.md** | 2026-06-12 | Phase-boundary task list | medium-term/process | STALE — "As of 2026-05-12 (Phase-1 task list)"; self-admits beads are the live surface; Phase-0 historical bulk | YES — phase boundaries + a frozen Phase-1 ordered list + Phase-0 historical | Beads (`br ready`) is the real task surface; overlaps STATUS.md | ARCHIVE the historical task lists; reduce to a one-liner pointing at `kerf next` / beads, or DELETE |
| **PRIORITIES.md** | mtime Jun 18 (untracked, NOT gitignored — uncommitted new file) | Live priority checklist referenced by HANDOFF-captain.md | long-term/priorities + operational | STALE — "Updated 2026-06-18"; checklist items (keeper wave-1, token wave-2) are this-resume ephemera, not durable priorities | YES — mixes DONE-this-resume operational log with focus-area priorities | Overlaps captain-lanes.md operator_initiatives + HANDOFF-captain.md | KEEP if operator wants a durable priority file, but it's currently a session log — either commit + curate to durable priorities, or fold into captain-lanes.md |
| **ROADMAP.md** | 2026-06-12 (commit) | High-level epic roadmap | long-term/priorities | STALE — "As of 2026-05-15"; rows reference Phase-2 entry / extqueue as in-progress, all long landed | Low (it's purely roadmap) | Self-defers to docs/INITIATIVES.md + STATUS.md for live tracking | KEEP as historical roadmap but mark superseded, or MERGE-INTO docs/INITIATIVES.md |
| **POST_OPERATIONAL_PARALLELISM_ROADMAP.md** | 2026-05-18 | One-time plan for the post-phase-1 concurrency push | reference (point-in-time design) | DEAD/SUPERSEDED — audited against commit `60b6024`; concurrency wedge has since been root-caused + fixed (controlpoints/hk-37giq); 33 days stale | Low (single-purpose) | The concurrency work it plans is done; overlaps postmortems | ARCHIVE into docs/ (historical design), DELETE from root |
| **AGENT_INDEX.md** | 2026-06-12 | Master discovery index for the knowledge base | reference (navigation) | CURRENT — stable index, recently touched | No | — (it's the hub) | KEEP — canonical, required by CLAUDE.md reading order |
| **AGENT_OPERATING_MANUAL.md** | 2026-06-12 | Distilled per-session ops rules for an orchestrator | behavioral-directive / reference | CURRENT — recently updated, template-derived | Some — quick-start rules + reading order + gotchas | Overlaps AGENTS.md / docs/orchestrator-rules.md by design (distillation, links back) | KEEP — explicitly a distillation that links rather than repeats |
| **AGENT_COMMS.md** | mtime Jun 12 (untracked, gitignored) | RETIRED file-outbox message log | reference (retired artifact) | **DEAD** — header: "⛔ RETIRED 2026-06-01 — DO NOT APPEND"; replaced by `harmonik comms` bus per hk-8sm4f | No | Superseded by agent-comms skill + `harmonik comms` | DELETE — confirmed retired artifact, gitignored, audit-trail value only; safe to remove |
| **OPERATING-GUIDE.md** | 2026-06-13 | Day-to-day human runbook (deploy/daemon/etc) | reference (runbook) | CURRENT — recently updated, links to CLI-REFERENCE/CONFIGURATION/QUICKSTART | No | Some overlap w/ harmonik-lifecycle + harmonik-dispatch skills (but human-facing) | KEEP — human-facing runbook, distinct audience |
| **KERF-FEEDBACK.md** | 2026-05-18 | Convention doc for logging kerf beta friction | reference (process convention) | CURRENT — it's a convention pointer to docs/kerf-feedback/YYYY-MM-DD.md, not itself dated content | No | — | KEEP — small, stable convention doc (could live under docs/) |
| **CHANGELOG.md** | 2026-06-12 | Standard Keep-a-Changelog | reference | CURRENT — Unreleased section active, recently updated | No | — | KEEP — standard project artifact |

## Narrative

### HANDOFF proliferation is an anti-pattern (with one real bug)

There are **7 root `HANDOFF-*.md` files**, but only **1** maps to a live concern:

- `HANDOFF.md` — the canonical main/captain handoff. **CURRENT.** Keep.
- `HANDOFF-captain.md`, `HANDOFF-chani.md` — **live** (captain + the chani crew). Both
  gitignored, both dated 2026-06-20. Legitimate working state — but they live at repo root
  by accident of tooling, not design.
- `HANDOFF-codexcrew.md`, `HANDOFF-flywheel.md`, `HANDOFF-named-queues.md` — **DEAD**. All
  three say "STOOD DOWN / STAND-DOWN 2026-06-09," none corresponds to a live crew (codexcrew,
  flywheel, named-queues are not in the registry), and all are 11 days stale. The
  named-queues file's "#1 ON RESUME — DEPLOY hk-togxq" banner is actively misleading: that
  P0 fix is 11 days old and long landed; a resuming agent could be sent chasing a ghost.
- `HANDOFF-controlpoints.md` — **DEAD and uniquely harmful**: it is the **only per-crew
  handoff that is committed to git** (every other is gitignored). controlpoints is a
  stood-down dead lane, yet this stale crew-state ships to every clone of the repo.

The pattern: harmonik's crew/keeper tooling writes per-lane handoffs to **repo root** with a
`HANDOFF-<lane>.md` name. When a lane stands down, nothing reaps the file. Live crews already
have durable mission files under `.harmonik/crew/missions/` and a handoffs dir
(`.harmonik/crew/handoffs/`). **Per-crew handoffs should be written there, not at repo root.**
Repo root should hold exactly **one canonical `HANDOFF.md`** (the main-session handoff).

Recommendations:
1. **DELETE `HANDOFF-controlpoints.md` from git** (tracked dead lane — the priority cleanup).
2. **ARCHIVE/DELETE** the 3 dead gitignored handoffs (codexcrew, flywheel, named-queues).
3. Keep `HANDOFF.md`; keep the 2 live gitignored crew handoffs but **relocate crew handoffs
   to `.harmonik/crew/handoffs/`** going forward so root stops accumulating them.
4. Add `HANDOFF-*.md` (except `HANDOFF.md`) to `.gitignore` so a per-crew file can never be
   committed again.

### Concern-mixing: behavioral directives bolted onto ephemeral state

The highest-friction mixing is the **"ORCHESTRATION DIRECTIVES — DO NOT EDIT"** block
duplicated verbatim at the top of `HANDOFF.md` AND `HANDOFF-captain.md`. These 4 rules are
**durable behavioral contract** (boot via STARTUP.md, never pre-assign, don't race the
daemon, don't self-/quit) — they belong in `docs/orchestrator-rules.md` / the captain skill,
not copy-pasted onto throwaway session state. Same rules, two files, drift risk, and they
contradict the "this file is ephemeral" purpose.

Other mixers:
- **STATUS.md** layers current-phase + active-lanes + frozen Phase-0/1 history. The
  active-lanes table is the live part and is already 10 days stale; it duplicates
  `docs/INITIATIVES.md` (the actual live initiatives board) and `HANDOFF.md`. Reduce
  STATUS.md to "what harmonik is + current phase" and let INITIATIVES.md own lanes.
- **TASKS.md** is a frozen Phase-1 ordered list that self-admits beads are the real surface.
- **PRIORITIES.md** is labelled "priorities" but is actually a per-resume DONE/in-flight
  checklist — operational log masquerading as long-term priorities.

### Tier overlap / duplication clusters

- **Live state**: HANDOFF.md ⟷ STATUS.md ⟷ docs/INITIATIVES.md — three places track
  current lanes/state. Pick INITIATIVES.md as the lane board; HANDOFF.md as session state;
  shrink STATUS.md.
- **Roadmap/priorities**: ROADMAP.md ⟷ POST_OPERATIONAL_PARALLELISM_ROADMAP.md ⟷
  PRIORITIES.md ⟷ captain-lanes.md — all four hold some forward-looking ordering; two are
  stale/superseded.
- **Behavioral**: AGENTS.md/CLAUDE.md ⟷ AGENT_OPERATING_MANUAL.md ⟷ orchestrator-rules.md
  ⟷ the DIRECTIVES blocks. The manual is an intentional distillation (links back, fine); the
  DIRECTIVES blocks are uncontrolled duplication (extract).

### Retired-artifact confirmation

`AGENT_COMMS.md` is confirmed **DEAD** — its own header declares it RETIRED 2026-06-01 (hk-8sm4f),
replaced by the `harmonik comms` bus + agent-comms skill. It is gitignored (local-only). Safe to
DELETE; retain history elsewhere if an audit trail is wanted.

## Cleanup priority (highest-value first)

1. **DELETE `HANDOFF-controlpoints.md` from git** — only committed dead crew-state.
2. **DELETE/ARCHIVE** the 3 dead gitignored handoffs (codexcrew, flywheel, named-queues).
3. **`.gitignore` `HANDOFF-*.md`** (keep `HANDOFF.md`) to prevent recurrence.
4. **DELETE `AGENT_COMMS.md`** — self-declared retired.
5. **ARCHIVE `POST_OPERATIONAL_PARALLELISM_ROADMAP.md`** to docs/ — done/superseded.
6. **Extract the DIRECTIVES block** from HANDOFF.md/HANDOFF-captain.md into orchestrator-rules.md.
7. **Shrink STATUS.md + TASKS.md** to pointers; let INITIATIVES.md + beads own the live surface.
