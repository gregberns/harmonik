# Doc & Instruction Audit — 2026-06-20

> ## ✅ STATUS: COMPLETE (2026-06-20)
> The audit's recommendations were executed. Summary:
> - **Track 1 cleanup** — `3b25168c`: untracked 2 dead committed files (HANDOFF-controlpoints.md, ops-monitor.md mission), archived 14 dead/stale missions + 3 dead handoffs + retired AGENT_COMMS.md to `.harmonik/_archive/`, gitignored `*.env.txt`.
> - **Phase A — instruction consolidation (the three-kinds model)** — `334bb759`: NEW `orchestrator-rules` skill (single standing-rules contract), AGENTS.md→router, `.harmonik/context/` tiers with self-describing headers, retired AOM/TASKS/PRIORITIES, slimmed STATUS/AGENT_INDEX. `511d3348`: `harmonik init` now scaffolds the new model.
> - **Phase B — asset-sync update path** (epic hk-vk7b, CLOSED) — `7b0da354` manifest+classes, `f7c286ac` reconcile engine, `91ff5e0f` **`harmonik sync-assets`** command, `38f23f21` supervisor skew-notify. Each bead implement→review→commit; the reconcile matrix and sync writes got adversarial review (caught + fixed real data-loss bugs).
> - **Deferred:** `hk-fozq` (P3) supervisor auto-apply execution; **Track 1b** bulk gitignored cruft (~3.2 GB — binaries, .beads.bak, worktree/run-context prune) pending a fleet-down window.
> - Detail docs 07/09/10 (load-discipline, reusable layout, asset-sync) are the as-built design; the issue tables below are the original findings, now addressed.

Audit of root files, `.harmonik/` instruction/state artifacts, crew missions, and gitignored cruft.
Goal: separate **operational/session state** (volatile, hour-to-hour) from **behavioral directives** (stable, how-to-act) from **long-term priorities** (roadmap/goals) — and clean up the accumulated cruft.

## Detail reports
- [01-root-instruction-docs.md](01-root-instruction-docs.md) — 18 root instruction/state docs
- [02-product-docs.md](02-product-docs.md) — 8 product/user docs (README, QUICKSTART, CLI-REFERENCE…)
- [03-harmonik-context.md](03-harmonik-context.md) — `.harmonik/context/` (project.yaml concern-mixing)
- [04-crew-missions.md](04-crew-missions.md) — 20 mission files vs 5 live crews
- [05-gitignored-cruft.md](05-gitignored-cruft.md) — ~3.2 GB reclaimable + security flag
- [06-instruction-architecture.md](06-instruction-architecture.md) — tier map, overlaps, gaps, target arch

---

## The core problem (one sentence)

Stable behavioral rules are pinned inside the most-volatile files, the daily-loop / dispatch discipline is restated 4× with no declared precedence, and "progress" lives in 4 disagreeing trackers — so a booting captain gets scattered, stale answers to both "what do I do this hour?" and "what are we building this month?"

The **skills tree is already the correct model** (it externalizes all volatile state to tier-1/2/3 context files). The fix is to copy that pattern across the root doc tree.

---

## Convergent findings (every report agreed)

1. **Behavioral directives rot inside volatile files.** Stable rules are pinned in the 3 most-ephemeral surfaces:
   - `HANDOFF.md`'s `ORCHESTRATION DIRECTIVES — DO NOT EDIT` header (durable contract on an ephemeral file)
   - `PRIORITIES.md`'s "Standing rules" (and the file is **untracked** in git)
   - `project.yaml`'s time-boxed "3-day scale-out" directives — in a file whose own header says "weeks cadence, no ephemeral state", with no eviction owner.
2. **The daily loop / dispatch discipline is stated FOUR times** — AGENTS.md, AGENT_OPERATING_MANUAL.md, orchestrator-rules.md, harmonik-dispatch skill — with no precedence and already-drifted details (concurrency numbers, wave caveat).
3. **Four overlapping progress trackers, none authoritative** — ROADMAP.md, STATUS.md "Active lanes", docs/INITIATIVES.md, AGENT_INDEX.md "Major Features Landed" — all with different dates. No clean home for **medium-term epics-in-progress**.
4. **`project.yaml` blends four cadences in one file** despite a `.yaml` extension that machine-reads nothing — it's prose-in-YAML-clothing. The priority ranking is duplicated **three times** across project.yaml + captain-lanes.md.
5. **Proliferation of stale per-entity artifacts** — 7 root HANDOFF-*.md (3 dead lanes), 20 crew missions (9 dead / 5 stale), all because nothing prunes on stand-down.

---

## Proposed target architecture

Every file is **EITHER stable-behavioral OR volatile-state, never both**, with one owner + one cadence.

| Tier | Cadence | Home | Owns | Written by |
|------|---------|------|------|------------|
| **Long-term / strategic** | weeks–months | `ROADMAP.md` | objectives, phases, "we are HERE" progress marker | operator + captain at milestones |
| **Medium-term / process** | days–week | `docs/INITIATIVES.md` | live epics/lanes in progress + per-initiative how-we-process | captain |
| **Short-term / operational** | session–hour | `HANDOFF.md` | session state, known-tasks-now, blockers | each session at handoff |
| **Operational lane state** | hour | `.harmonik/context/captain-lanes.md` | current lane→crew assignment + ONE merged priority ranking + time-boxed campaign posture | captain at boot/shutdown |
| **Behavioral — routing** | rare | `AGENTS.md` (==CLAUDE.md) | pure router; **declares precedence**; project-specific deltas | deliberate edit |
| **Behavioral — standing rules** | rare | `docs/orchestrator-rules.md` | THE single standing-rules file: dispatch discipline, bead lifecycle, monitor pattern, CWD | deliberate edit |
| **Behavioral — role contracts** | rare | `.claude/skills/*` | captain / crew / keeper / dispatch / lifecycle operating contracts | deliberate edit |
| **Behavioral — guardrails** | rare | AGENTS.md or orchestrator-rules.md | `forbidden_actions`, `locked_decisions` | deliberate edit |
| **Normative** | rare | `specs/` | the spec (always right) | kerf finalize |
| **Config (machine-read)** | rare | `.harmonik/context/project.yaml` | shrinks to ~10 lines: phase + locked-decisions pointer | rare |
| **Reference** | n/a | `docs/known-workarounds.md` | harness quirks, gotchas | as discovered |

**Collapses:** 4 rule-sources → `orchestrator-rules.md`. 4 trackers → `ROADMAP.md` (long) + `docs/INITIATIVES.md` (medium). `HANDOFF.md` owns short-term only.
**Retire:** `AGENT_OPERATING_MANUAL.md` (gotchas → known-workarounds), `TASKS.md` (→ historical/archive), `PRIORITIES.md` (content → HANDOFF, git-tracked).

---

## Prioritized cleanup backlog

### P0 — Security / safety (do first, some need operator)
- **`edited.env.txt`** holds a plaintext `ANTHROPIC_API_KEY`, ignored only via local `.git/info/exclude` (NOT committed `.gitignore`). → **operator rotates the key**, then delete the file, add `*.env.txt` to `.gitignore`. (detail 05)
- Verify the key was never committed: `git log --all -- edited.env.txt`.

### P1 — Dead-artifact deletion (safe, recoverable via git/regeneration)
- Delete `AGENT_COMMS.md` (self-declares RETIRED 2026-06-01, hk-8sm4f). (01)
- Delete/relocate dead HANDOFFs: `HANDOFF-codexcrew.md`, `HANDOFF-flywheel.md`, `HANDOFF-named-queues.md` (stood down 2026-06-09). **`HANDOFF-controlpoints.md` is the priority** — it's the only per-crew handoff *committed to git* and ships a dead lane to every clone. (01)
- `.gitignore HANDOFF-*.md` (keep `HANDOFF.md`); relocate live crew handoffs to `.harmonik/crew/handoffs/`. (01)
- Prune 9 DEAD + 5 STALE crew missions; rename `example-handoff.md` → `_TEMPLATE.md`; move smoke artifacts to `missions/_smoke/`. (04)
- Root build binaries (`daemon.test`, `hooksystem.test`, `harmonik`, `harmonik-twin-claude` ~46 MB), `.beads.bak.*` (75 MB), `beads-intents.bak`, `.claire/`, `.claude-pid`, `.DS_Store` — all ignored & regenerable. (05)

### P2 — Retention policies (unbounded growth — needs a rule, not a one-time rm)
- `.beads/.br_history-archive/` — **1.3 GB / 4,281 files**, one snapshot per sync, never pruned.
- `.claude/worktrees/` — **1.2 GB / 22 dirs** + 15 stale git-registered-but-missing entries (`git worktree prune`).
- `.harmonik/events/events.jsonl` — 15 MB / 39k lines, never rotated.
- `.harmonik/run-context/` — 140 dirs; needs age-based prune. **Do NOT touch `.harmonik/worktrees/` — those are LIVE daemon runs.** (05)

### P3 — Instruction restructure (the real work — NEW NORMATIVE CONTRACT, needs operator sign-off)
- Split `project.yaml` per detail 03 (→ ~10 lines; directives→skill/AGENTS, campaign+ranking→captain-lanes, salvage narrative→HANDOFF).
- Make `AGENTS.md` a router that declares precedence; collapse the 4 dispatch-discipline restatements into `orchestrator-rules.md`.
- Collapse 4 progress trackers into ROADMAP (long) + INITIATIVES (medium); stand up `docs/INITIATIVES.md` as the medium-term epic home if not already.
- Define the **mission lifecycle rule**: a mission `.md` is LIVE iff a same-named `<crew>.json` exists; auto-archive orphans on `crew stop` + startup reconcile. (04)
- Regenerate `CLI-REFERENCE.md` (missing verbs: captain, schedule, decisions, sleep, wake, goal-keeper, project-hash, sentinel); fix OVERVIEW/CONCEPTS "in flight" claims for shipped commands. (02)
- (Optional) Move product docs (README excepted) to `docs/product/` to separate the product family from the agent-operating family. (02)

---

## Reclaimable space
~3.2 GB total: ~125 MB immediate safe-delete, ~3 GB behind P2 retention policies. (detail 05)
