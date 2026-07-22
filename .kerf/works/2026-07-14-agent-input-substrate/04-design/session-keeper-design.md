# 04-design / session-keeper-design — SK-002 keeper carve-out reconciliation (C6 / A11)

> Pass 4 (Change Design). Area: amendments to the EXISTING spec `specs/session-keeper.md`,
> specifically **SK-002** (`PanePort`, the tmux write boundary). Subordinate to
> `04-design/00-decisions.md` — this file EXECUTES **D6** (keeper carve-out + deferred
> migration); it does NOT re-decide carve-out-vs-migrate. Grounds:
> `03-research/seam-contract/findings.md` Q4 + Risk #1; `02-components.md` C6 (A11).
> All SK line numbers verified against `specs/session-keeper.md` on
> `phase1-session-restart-substrate`, 2026-07-14.

## Current state

`specs/session-keeper.md` (SK, `last-updated: 2026-07-13` — ONE day before this M2 work) is
freshly normative. **SK-002** (heading `:70`, prose `:72`, interface block `:74-81`) requires:

- `PanePort.Inject` MUST follow the `tmux load-buffer` + `paste-buffer` write discipline of
  **[process-lifecycle.md §4.7 PL-021d]**; the bare `send-keys` form is FORBIDDEN.
- `PanePort.Capture` is keeper-scoped and MUST NOT be extended into the daemon's
  process-spawn path (PL-021b §5).

§9.1 "Depends on" (`:479-480`) cites PL-021d as the write discipline `PanePort.Inject`
follows, and PL-021b as the process-spawn seam Capture stays clear of. The production impl is
`keeper.InjectText` (`internal/keeper/injector.go:133`), which shells out to tmux **directly**
via `tmuxRunFn`/`exec.Command` (own buffer `hk-keeper-inject`, `:152`), NOT through
`tmux.OSAdapter`. Keeper is **off-daemon**, depguard-barred from importing the daemon
(`.golangci.yml:142`), addresses arbitrary interactive panes it did NOT spawn by tmux target
string, and sends slash-commands (`/session-handoff`, `/clear`, `/session-resume`,
`harmonik agent brief --wake keeper-restart`) plus warn/ACK nudges. It holds no
`SubstrateSession` handle.

The collision (seam-contract Risk #1 / A11): C3/C6 retire PL-021d on the daemon RUN input
path. If "retire PL-021d" is read as "delete the `load-buffer`/`paste-buffer`/`send-keys`
verbs wholesale," SK-002 is left citing a deleted requirement — a spec lie — and keeper's live
restart cycle loses its write path.

## Target state

Per **D6** (carve-out now, migration deferred), the minimal correct change is a **cross-
reference amendment to SK-002 plus one new deferred-migration requirement** — NOT a re-draft.
Concretely:

1. **Amend SK-002 prose** (`:72`) — append one carve-out sentence so SK-002 does not contradict
   the PL-021d demotion. Added text (verbatim intent):

   > NOTE (M2 agent-input-substrate carve-out): [process-lifecycle.md §4.7 PL-021d] is
   > DEMOTED for the daemon RUN input path (superseded by [agent-input.md] AIS for daemon-
   > spawned agent runs), but is PRESERVED for the keeper. `PanePort.Inject`'s conformance to
   > the load-buffer + paste-buffer discipline is UNCHANGED by that demotion: the keeper is
   > off-daemon, holds no `SubstrateSession` handle, and drives interactive panes it did not
   > spawn, so it is explicitly EXCLUDED from the C6 deletion boundary that retires the daemon
   > run input stack. The `load-buffer`/`paste-buffer`/`send-keys` tmux verbs keeper depends on
   > MUST survive that deletion.

2. **Amend §9.1 "Depends on"** (`:479`) — the PL-021d bullet gains a clause noting the
   requirement survives ONLY as the keeper-preserved discipline (demoted, not deleted, on the
   daemon path), so a future PL reader does not treat the demotion as removing keeper's basis.

3. **Add one new deferred requirement — SK-021** (§4.10, a new sub-section after SK-020) — the
   migration path, stated as deferred (mirrors the §11 deferred-item register):

   > #### SK-021 — Deferred: keeper input MAY migrate to a session-id-keyed input port
   >
   > The keeper's paste path (SK-002) is a deliberate carve-out during M2. A future phase MAY
   > migrate it OFF direct tmux paste onto the agent-input substrate — but ONLY via a
   > **session-id-keyed input port in a leaf package keeper is permitted to import**
   > (keeper has no `SubstrateSession` handle and is depguard-barred from the daemon), OR via a
   > daemon RPC. Until such a port exists, SK-002's PL-021d carve-out is normative and keeper
   > stays OUT of the C6 deletion boundary. This is a deferred item, NOT an open decision.
   >
   > Tags: mechanism

   SK-021 is warranted as its OWN requirement (not just a §11 note) because it carries a
   normative MUST-precondition ("only via a leaf port / RPC") that gates a future teardown —
   §11's register is for non-normative deferrals, and it is added there too as a pointer.

A new SK requirement number **is** warranted (SK-021), because the migration precondition is a
normative constraint on future work, not merely a cross-ref. Everything else is a note/clause
on SK-002 + §9.1 — no SK-002 re-draft, no interface-block (`:74-81`) change.

## Rationale

- **Tie to D6.** D6 resolves the direction as carve-out-now: PL-021d DEMOTED (daemon run path)
  but PRESERVED for keeper + the interactive-session-nudge use. This design writes exactly that
  into SK-002 so the demotion recorded in `process-lifecycle-design.md` / `agent-input-design.md`
  and SK-002 are mutually consistent in the same landing motion (D10 registry-lint style: the
  demotion and the carve-out note land together, or SK-002 dangles a demoted cross-ref).
- **Tie to the A11 hazard.** Deleting the input path before keeper migrates breaks the restart
  cycle (02-components C6; seam-contract Risk #1). The carve-out note makes the C6 deletion
  boundary EXCLUDE keeper's verbs textually, so C6's tmux-verb teardown cannot regress SK-015 /
  SK-INV-005 bounded-liveness (the keeper's own escape hatch).
- **Architectural distance.** Immediate migration is awkward, not merely deferred for
  convenience: a daemon-side `SubstrateSession`-keyed input driver cannot serve keeper, which
  is off-daemon, session-id-addressed, and drives panes it never spawned (Q4). Forcing
  migration now would require inventing the session-id-keyed input contract as a sizeable added
  M2 task AND re-drafting SK-002 in the same motion (D6 PLANNER-RECONCILE) — carve-out is the
  safe, unblocking move.

## Requirements traceability

| Driver | SK change |
|---|---|
| **A11 hazard** (delete input path before keeper migrates → breaks restart cycle) | SK-002 carve-out note: keeper EXCLUDED from C6 boundary; keeper's tmux verbs MUST survive |
| **SC4 (deletion scope)** — C6 deletes the daemon RUN input stack only | SK-002 note + SK-021: deletion boundary scoped away from keeper's PL-021d verbs |
| **D6** (carve-out now, migrate deferred) | SK-002 prose note (PL-021d demoted-but-preserved) + §9.1 clause + new SK-021 (deferred migration precondition) |
| **PL-021d demotion** (process-lifecycle-design) | §9.1 "Depends on" clause: PL-021d survives as keeper-preserved discipline |

## PLANNER-RECONCILE (carried inline from D6)

TASKS.md M2-3/M2-6 state the C6 dependency as "**keeper migrated to the M2-2 structured
driver**" before deletion — i.e. the ROADMAP's stated intent is MIGRATE. But SK-002 (freshly
normative 2026-07-13) requires keeper's `PanePort.Inject` to follow PL-021d paste, and keeper's
architecture (off-daemon, session-id-addressed, no `SubstrateSession` handle, sends slash-
commands to interactive panes it did not spawn) makes full migration to the daemon-side
`SubstrateSession`-keyed driver architecturally awkward. **This design resolves the A11
deletion hazard via carve-out + narrowed C6 scope** (safe, unblocks M2) and defers migrate-vs-
carve-out as the planner's call. If the planner INSISTS on migration, SK-021 becomes an active
sizeable task (define the session-id-keyed input contract as a leaf package keeper may import)
AND SK-002 must be re-drafted in the same motion — not the SK-002-note-only change specced
here. **Confirm carve-out before C6 lands.**

## Non-goals

- Does NOT touch the SK-002 interface block (`:74-81`), any other SK requirement, or keeper's
  Step reactor / invariants.
- Does NOT define the session-id-keyed input port (SK-021 is a deferred precondition, not a
  design of that port).
- Does NOT re-decide D6's direction, or reinterpret PL-021b's Capture prohibition.
