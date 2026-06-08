<!-- PP-TRIAL:v2 2026-06-08 main @ 2051f722, in-sync origin, CLEAN. controlpoints lane. The `captain` (Captain & Crew) kerf work is DESIGN COMPLETE — ready/SQUARE, 15 beads filed (codename:captain), plan published to docs/plans/captain/. NOT yet dispatched — operator wants implementation to start in a deliberate next step. Daemon UP (pid 96288, -c6, review-loop) but IDLE. Do NOT clobber HANDOFF.md / HANDOFF-named-queues.md / HANDOFF-flywheel.md (other lanes). -->

ROLE: orchestrator. Next phase = **implement the `captain` plan by dispatching its beads through the daemon queue** (harmonik-dispatch skill). This is execution, not design.

# Where we are — `captain` plan is READY TO IMPLEMENT (not started)
The Captain & Crew design is fully through kerf (`kerf square captain` = SQUARE) and all 15 implementation beads are filed, committed, and pushed. **Nothing has been dispatched.** Implementation begins when the operator says go.

**Captain & Crew** = a long-lived "captain" orchestrator that spawns + coordinates long-lived "crew" agents (each owns one epic + its own named queue), wired by `harmonik comms` + a new `epic_completed` event. In scope = the mechanical wiring; OUT of scope = the captain's judgment (ranking / failed-crew handling — a future layer; for now the captain surfaces-and-awaits the operator).

## The plan lives in TWO places
- **Published (in git, what implementers read):** `docs/plans/captain/` — `SPEC.md` (read first), `05-specs/c1..c4-spec.md` (the per-component change-specs implementers follow verbatim), `06-integration.md` (build order, data flow, resolved gaps, test strategy), `07-tasks.md`, `SESSION.md`. Published here because the kerf bench is gitignored → daemon worktrees can't see it.
- **Bench (gitignored, full process record):** `.kerf/works/captain/` (adds `01-04`, the review files). Authoritative for kerf; not visible to worktrees.

## The 15 beads (label `codename:captain`)
| T | bead | what | deps |
|---|------|------|------|
| T1 | hk-xdxws | C1: surface child status on `br show` dependency edges | — |
| T2 | hk-w6y70 | C1: emit `epic_completed` on last-child close (at-most-once) | T1 |
| T3 | hk-o50hy | C1: boot-seed the `epic_completed` guard from event log | T2 |
| T4 | hk-tfxjp | scenario: C1 emit + at-most-once + boot-seed | T3 |
| T5 | hk-i1ue4 | C2: `internal/crew` registry package + depguard entry | — |
| T6 | hk-kbqto | C2: `buildCrewLaunchSpec` argv builder | — |
| T7 | hk-5tg5o | C2: daemon `crew-start/stop` handler + socket + keeper-attach | T5,T6 |
| T8 | hk-yj2j6 | C2: `harmonik crew` CLI (start/stop/list) | T7 |
| T9 | hk-4z0gp | C2: unit tests (registry, launch-spec, handler) | T7 |
| T10 | hk-rbpss | smoke: C2 crew start/stop on real `claude --remote-control` | T8 |
| T11 | hk-zblnu | C3: mission-handoff schema doc (→ `specs/crew-handoff-schema.md`) + example | T2 |
| T12 | hk-cvg1j | C3: crew-launch skill (boot/dispatch/progress feed) | T11 |
| T13 | hk-bejpi | C4: captain skill (mechanics-only loop) | T12,T8,T3 |
| T14 | hk-zi4ej | scenario: captain — end-to-end captain+crew | T4,T9,T10,T13 |
| T15 | hk-495is | explore: captain — operator CLI surface | T8,T13 |

**Build order:** C1 (T1→T2→T3/T4) ∥ C2 (T5/T6→T7→T8/T9/T10) run concurrently from the start → C3 (T11→T12) → C4 (T13) → the two gating test beads T14/T15. DAG verified acyclic.

## How to start implementing (when operator says go)
1. Rebuild + confirm daemon: `go install ./cmd/harmonik`; `harmonik queue status` (daemon is already up at -c6, idle). Restart only if you rebuilt.
2. **Dispatch the roots first** — submit T1+T2 and T5+T6 to the daemon queue (`harmonik queue submit`, stream group; see harmonik-dispatch skill). These have no deps and unblock the rest.
3. Arm a Monitor: `harmonik subscribe --types run_completed,run_failed,run_stale,reviewer_verdict,heartbeat --json`.
4. As roots land, submit the next layer (T3/T4, T7→T8/T9). Then C3 (T11→T12), then C4 (T13), then the test beads.
5. The daemon merges to main one-at-a-time; review-loop is on.

## Caveats (READ before dispatching)
- **T3 (hk-o50hy) and T7 (hk-5tg5o) BOTH edit `internal/daemon/daemon.go`.** They're logically parallel but contend at merge — the one-at-a-time merge handles it, but don't expect a conflict-free simultaneous pair; land one then rebase the other. Everything else is conflict-free.
- **T11 (hk-zblnu) creates `specs/crew-handoff-schema.md`** — operator RESOLVED the doc home to `specs/` (spec-first; C2's Go must conform). It does NOT exist yet.
- **T2 (hk-w6y70)** is the slice's ONLY `specs/` touch beyond T11 — an additive `specs/event-model.md §8` row (EV-029 N-1-safe).
- **Nothing closes (plan or impl bead) until T14 AND T15 close** (the two test beads gate the whole plan). Scenario/smoke beads (T4, T10, T14) are `//go:build scenario` / manual — the daemon gate SKIPS scenario tests, so RUN THEM YOURSELF under the supervisor (`reference_scenario_test_authoring`, `reference_harmonik_daemon_session_nesting`).
- **Pin (claude v2.1.168):** crew launch = INTERACTIVE `claude --remote-control "<name>" --session-id <uuid>` (caller-minted id, bracketed-paste seed, `--resume` re-task). NOT server-mode `claude remote-control`; NOT the Agent-SDK sessions API (bills the API pool, not the Max subscription). Flags confirmed in `claude --help`.

# Translations glossary
- **captain / crew** — captain = top orchestrator that hands epics to long-lived "crew" agents; each crew owns one epic + its own named queue.
- **C1–C4** — the four components: C1 `epic_completed` event (Go), C2 `harmonik crew start/stop` + registry (Go), C3 crew-launch skill + mission-handoff schema (instructions), C4 captain skill (instructions).
- **T1–T15 / hk-…** — the 15 implementation beads above.
- **epic_completed** — new daemon event fired when an epic's last child bead closes; the captain's structural completion trigger.
- **`--assignee` mirror** — the crew writes `br update <epic> --assignee <crew>` on every adopt; the captain attributes `epic_completed` via `br show <epic> --assignee` (NOT the registry's spawn-time field).
- **harmonik-dispatch / comms / subscribe** — the daily-loop dispatch skill, the inter-agent message bus, and the daemon event stream.

# No hard blockers. Plan is implement-ready; do NOT dispatch until the operator says go. Daemon healthy + idle.
