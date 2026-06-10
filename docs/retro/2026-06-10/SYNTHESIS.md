# Captain & Crew Retrospective — Consolidated Synthesis
Date: 2026-06-10 | Sources: A1–A6 (captain retro, crew retro, tooling inventory, roadmap, process formalization, session-keeper brief)

---

## (a) Executive Summary

The Captain & Crew system (15/15 beads, `57c6fd94`) is functionally complete and deployed with 4 live crews. The fleet ran its first real multi-session sprint this session and surfaced a clear cluster of friction:

1. **Dispatch reliability** — three root causes produced silent failures: pre-assigned beads (hk-amed0, now fixed), seed-not-submitted Enter hang (hk-76n5g, now fixed), and DOT-mode reviewer-stall (still unresolved, currently worked around by `--workflow-mode review-loop`).
2. **Observability gaps** — crews can't distinguish "bead progressing slowly" from "bead wedged" without pane inspection; `run_stale` false-positives burned multiple diagnostic cycles. Run events don't carry crew attribution, forcing br round-trips.
3. **Merge hygiene** — sequential same-package merges leave staged debris in the main tree; post-merge gate runs on worktree (clean) not merged main (may break).
4. **Ad-hoc tooling proliferation** — 15 manual scripts/SOPs have partial or zero harmonik-verb coverage; the highest-priority gaps are: orphan reap (P1), `harmonik promote` banked-commit deploy (P1), credit-burn guard in `supervise start` (P1), already-landed dry-run check (P1, hk-lhv8i).
5. **Docs and handoff staleness** — AGENT_INDEX.md and STATUS.md predate the crew system (last updated 2026-05-12); no captain SHUTDOWN.md exists; session-handoff skill is fleet-unaware.
6. **Session-keeper** — fully implemented, zero operator action blocks it. All 4 `.managed` markers exist; only hook stanzas need wiring via `keeper enable`.
7. **Near-term roadmap** — flush in-flight lanes (Codex Harness T12 keystone, Validation Net 3 infra beads, Release Pipeline gated on OAuth scope, Flywheel smoke hk-m8zqv), then advance standard-bead-dot (Phase-3 DOT grammar). Logmine recurring pipeline and Flywheel 4h smoke require operator ranking/consent.

---

## (b) "Embed into harmonik" Improvements (Deduplicated)

Cross-references: A1 = captain retro, A2 = crew retro, A3 = tooling inventory.

### B1. Auto-submit seed Enter on crew-boot and review-loop iter-N
`harmonik crew start` pastes mission seed but does not send Enter; same gap in review-loop iter-N re-seeds. Both panes sit idle until manual `tmux send-keys Enter`. Filed as hk-76n5g (LANDED `1ed53a6d`); verify the fix covers both paths.
- **Proposed bead title:** `fix(crew): verify hk-76n5g covers crew-boot AND all review-loop iter-N seeds` | type: chore | P1

### B2. Clear staged debris from main tree after daemon bead merge
Sequential merges of beads touching the same package leave staged-but-uncommitted deletions in the main working tree. Not tripping the escape-detector but would regress code if accidentally committed. Daemon merge path should `git restore --staged .` after each ff-merge.
- **Proposed bead title:** `fix(daemon): restore staged index on main tree after each bead merge` | type: bug | P1
- Sources: A1 pattern 2, A2 pattern 6

### B3. Periodic `bead_progressing` heartbeat event
Crews can't distinguish a progressing bead from a wedged one using event stream alone. `run_stale` at `launch_initiated` is a false-positive for a live-but-slow bead. Daemon should emit a `bead_progressing` event every ~5 min for any run with an active claude pane.
- **Proposed bead title:** `feat(daemon): emit bead_progressing heartbeat event for active runs` | type: feature | P1
- Sources: A1 pattern 7, A3 pattern 15 (indirect)

### B4. Denormalize epic.assignee into run events
`run_failed`, `run_stale`, `run_completed`, `run_started` events carry no crew attribution. Every event requires a `br show <bead> → parent_id → br show <epic>` round-trip. Daemon knows the bead's epic at dispatch time; include `crew_assignee` in the event payload.
- **Proposed bead title:** `feat(daemon): include epic assignee in run_failed/stale/completed event payloads` | type: feature | P2
- Source: A1 pattern 8

### B5. Comms `--wake` flag — pane injection on directed send
`harmonik comms send` is pull-only; idle crews never wake from a `send` without manual `tmux send-keys`. Scripts/hk-wake.sh exists but requires per-crew manual launch. `comms send --to <crew>` should optionally inject a tmux nudge via the crew registry's stored session handle.
- **Proposed bead title:** `feat(comms): --wake flag injects pane nudge on directed comms send` | type: feature | P2
- Sources: A1 implicit, A2 pattern 2, A3 pattern 5

### B6. Post-merge gate runs go build+vet on merged main tree (not worktree)
Same-package concurrent beads build green in isolation but collide post-merge (redeclared test helpers). Per-worktree gate misses cross-bead symbol collisions. Post-merge gate must compile the merged tree. Filed as hk-ycp62.
- **Proposed bead title:** `fix(gate): post-merge go build+vet against merged main tree (hk-ycp62)` | type: bug | P1
- Source: A2 pattern 4

### B7. `reviewer_submitted` event + workflow_mode in queue status
DOT-mode reviewer stalls silently (reviewer launched but never emits verdict). No event marks "reviewer is actively working vs spawned-and-gone". `queue status` also does not surface `--workflow-mode`. Both needed to detect stalls without pane inspection.
- **Proposed bead title:** `feat(daemon): emit reviewer_submitted event; expose workflow_mode in queue status` | type: feature | P2
- Source: A1 pattern 4

### B8. `harmonik promote` — banked-commit cherry-pick with build gate
The bypass-SOP (worktree-author → reviewer → merge-tree gate → cherry-pick to main) is executed 5+ times/session manually. No harmonik verb. Proposed: `harmonik promote <sha> [--verify-build]` runs merged-tree gate, cherry-picks to main, pushes, emits `bypass_sop_land` event.
- **Proposed bead title:** `feat(harmonik): promote command for daemon-independent banked-commit deploy` | type: feature | P1
- Source: A3 pattern 4 (also partially hk-gax8v — check overlap before filing)

### B9. `harmonik supervise start` — always strip ANTHROPIC_API_KEY (credit-burn guard)
`supervise start` does not strip API creds; spawned claude sessions bill the credit pool instead of subscription. `hk-keeper.sh` and the ephemeral `/tmp/hk-daemon-supervise.sh` both strip them. Fold into `supervise start` as unconditional default; emit `credit_burn_risk` warning event if key is detected at boot.
- **Proposed bead title:** `fix(supervise): strip ANTHROPIC_API_KEY at daemon boot by default` | type: bug | P1
- Source: A3 pattern 11

### B10. `harmonik queue dry-run` — already-landed git-log check (hk-lhv8i)
`dry-run` validates ledger deps but does not check `git log --grep "Refs: <id>"`. Stale-open beads whose work already landed waste dispatch slots. Filed as hk-lhv8i.
- **Proposed bead title:** `feat(queue): dry-run reports already-landed beads via Refs: git-log check (hk-lhv8i)` | type: feature | P1
- Source: A3 pattern 9

### B11. `harmonik supervise reap` — boot-time + on-demand orphan tmux reap
Daemon leaks orphaned `harmonik-<hash>-flywheel` sessions; 128 observed. These can exhaust `tmux new-window` spawn slots. Boot reconciler should reap sessions with `pane_dead=1` predating the live daemon start.
- **Proposed bead title:** `feat(supervise): reap-orphans verb for boot-time + on-demand tmux orphan cleanup` | type: feature | P1
- Source: A3 pattern 2

### B12. `harmonik crew start --keeper` — auto-wire keeper on crew launch
`harmonik keeper enable` ceremony must be run manually per crew. `crew start` should accept `--keeper [--warn-pct 80 --act-pct 90]` to wire stanzas and start the watcher automatically, removing per-crew setup toil.
- **Proposed bead title:** `feat(crew): --keeper flag on crew start auto-wires session-keeper` | type: feature | P2
- Source: A3 pattern 13, A6

### B13. Configurable commit-poll timeout for `scenario`-labeled beads
Scenario tests boot real daemons (45s+ per test), exhausting the 30-min `commitPollTimeout` before commit. Gate also skips `//go:build scenario` tests. Two fixes: raise/make-configurable the timeout for `scenario` beads; add a `go test -tags=scenario` post-merge gate for changed functions. Filed as hk-i2ie5.
- **Proposed bead title:** `feat(daemon): configurable commit-poll timeout + scenario post-merge gate (hk-i2ie5)` | type: feature | P2
- Source: A2 pattern 8, A3 pattern 6

### B14. OAuth `workflow`-scope preflight warning at dry-run time
Beads touching `.github/workflows/` push-reject with `workflow scope` error. The scope gap is detectable at queue-submit time by scanning bead descriptions or prior-run events. Emit a warning at `dry-run` before wasting a dispatch slot.
- **Proposed bead title:** `feat(preflight): warn at dry-run if bead may touch workflow files and token lacks scope` | type: feature | P3
- Source: A2 pattern 3

### B15. Pin GIT_DIR+GIT_WORK_TREE in worktree-isolated agents
Worktree-isolated sub-agents have CWD reset to shared checkout between Bash calls. `git reset --hard` in one call can silently run against main. Framework should pin `GIT_DIR` and `GIT_WORK_TREE` env vars for worktree-isolated agents.
- **Proposed bead title:** `fix(worktree-agent): pin GIT_DIR/GIT_WORK_TREE env vars in worktree isolation` | type: bug | P2
- Source: A2 pattern 5

---

## (c) Process Formalization (from A5)

### Planning
- Add "kerf vs direct-bead decision rule" to CLAUDE.md: cross-cutting/spec → kerf; scoped ≤5 beads known design → direct; typo/one-liner → direct; contrib-open tooling → direct with `contrib-open` label.
- Add "kerf graduation gate" (kerf square passes + spec in specs/ + codename: labels + scenario bead exists) before dispatching from any kerf work.
- Interim `kerf next` priority workaround: `--only=bead`; explicit `--work <codename>` for cross-work ranking. File meta-bead tracking upstream kerf priority fixes KF-026-01/02.

### Documentation
- AGENT_INDEX.md is broken for any agent bootstrapping today — last updated 2026-05-12, predates crew system, named queues, validation-net, codex-harness.
- STATUS.md: separate live state from pre-2026-06 historical noise; archive to `docs/historical/`.
- Add `docs/concepts/captain-and-crew.md` (conceptual overview; STARTUP.md is ops runbook, not this).
- Add doc ownership rules to `docs/methodology/METHODOLOGY.md` (which layer owns what, update triggers).
- MEMORY.md at 29.9KB vs 24.4KB limit — enforce ≤200-char index entries + topic file discipline.

### Handoff
- No captain SHUTDOWN.md exists (STARTUP.md has no symmetrical shutdown counterpart).
- session-handoff SKILL.md is fleet-unaware — produces incomplete handoffs for multi-crew sessions.
- Add tiered handoff model: (1) Captain HANDOFF.md ≤50 lines fleet-level; (2) crew mission file per-lane; (3) crew progress doc for crew-side durable findings.
- Crew context is lost on stop+restart — mission files are overwritten; captain is sole continuity bearer today.

---

## (d) Next-Phase Roadmap (from A4)

### Flush in-flight lanes (no operator decision needed)
- **Codex Harness** (~10 beads): T12 (CodexHarness registration in DOT cascade) is keystone; T13–T18 blocked behind it. Duncan crew owns.
- **Validation Net** (3 beads: hk-tijaj merge-conflict-skip scenario P1, hk-d5twq hook-bridge stub, hk-i0hor test-harness).
- **Flywheel smoke** (hk-m8zqv: 4h unattended run + CL conformance scenarios).
- **Spec drift** (17 open `kind:spec-drift` beads, all P2, independently dispatchable — batch submit).

### OAuth scope unblock (operator action required)
- Release Pipeline 4/8 beads (hk-jdesv, hk-o4j13, hk-ya51z, hk-vem4j) blocked on `workflow` scope for daemon/captain tokens.

### Standard-bead-dot (Phase-3 DOT grammar — KNOWN, no new design)
- Finish `standard-bead-dot` spec-draft; wire beads; dispatch (~8–15 beads). T12 (Codex Harness) is the prerequisite consumer. This is the most structurally significant next step in the north-star trajectory.

### Needs operator ranking/consent
- **Logmine recurring pipeline** (hk-mhmaw P1): kerf work still at research pass; operator decides whether to advance now or after CI/infra lanes stabilize.
- **Flywheel 4h smoke + keeper opt-in for captain** (hk-m8zqv + `.managed` creation): machinery ready; requires informed consent (a `/clear` on a live captain session is irreversible).

---

## (e) Session-Keeper Enablement Plan (from A6)

**Status: zero code work needed.** All 3 implementation beads (hk-kzqml, hk-rc51s, hk-dopv3) are CLOSED. All 4 crew `.managed` markers already exist. The only gap: zero hook stanzas are wired in either settings.json.

**Recommended rollout (Phase-1 validation first):**
1. Wire one crew (chani): `harmonik keeper enable chani --project /Users/gb/github/harmonik --scripts-dir /Users/gb/github/harmonik/scripts --tmux "harmonik-a3dc45482890-default:hk-crew-chani"`
2. Run `harmonik keeper doctor chani` — must pass all checks (binary, statusLine, Stop hook, PreCompact).
3. Confirm gauge writes: `cat .harmonik/keeper/chani.ctx` — should show `{"pct":..., "session_id":..., "ts":...}`.
4. Add `set-dispatching` / `clear-dispatching` calls around queue submits in chani's mission script (otherwise mid-dispatch /clear risk).
5. Start keeper watcher: `harmonik keeper --agent chani --tmux <handle> --warn-pct 80 --act-pct 90`. Because `.managed` exists, this is immediately Phase-2-live.
6. Wire remaining crews (duncan, liet, stilgar) after chani validates.

**Warn-only option:** rename `chani.managed` → `chani.managed.disabled` before starting the watcher to run warn-only (80% warning, no cycle).

**Key risk:** `.managed` markers already exist for all 4 crews — Phase-2 (destructive /clear cycle) activates the moment any keeper watcher starts with hooks wired. Confirm this is intended before wiring hooks for all 4 simultaneously.

---

## Bead-Proposal Table (Prioritized & Deduplicated)

Existing bead IDs mentioned in the reports are marked. Aim: 12–18 genuinely-new proposals.

### P1 — Critical / Unblocks Daily Loop

| Proposed Title | Type | Pri | Rationale | Label | Existing? |
|---|---|---|---|---|---|
| `fix(daemon): restore staged index on main tree after each bead merge` | bug | P1 | Staged debris from sequential same-package merges would regress code if accidentally committed; daemon should auto-clean | `kind:bug` `queue` | NEW |
| `feat(daemon): emit bead_progressing heartbeat every 5min for active runs` | feature | P1 | Eliminates run_stale false-positive wedge alarms; crews need a positive "still working" signal | `kind:feature` `observability` | NEW |
| `fix(gate): post-merge go build+vet against merged main tree` | bug | P1 | Same-package concurrent beads collide post-merge; per-worktree gate misses symbol redeclarations | `kind:bug` `gate` | hk-ycp62 (filed) |
| `fix(supervise): strip ANTHROPIC_API_KEY at daemon boot by default` | bug | P1 | API creds in daemon env → spawned claude bills credit pool; hk-keeper.sh strips them but supervise start does not | `kind:bug` `credit-burn` | NEW |
| `feat(queue): dry-run reports already-landed beads via Refs: git-log` | feature | P1 | Stale-open beads waste dispatch slots; dry-run should prescreen git history | `kind:feature` `queue` | hk-lhv8i (filed) |
| `feat(supervise): reap-orphans for boot-time + on-demand tmux orphan cleanup` | feature | P1 | 128 orphaned flywheel sessions can exhaust tmux spawn slots | `kind:feature` `daemon` | NEW (extends hk-5pg37) |
| `feat(harmonik): promote command — banked-commit cherry-pick with build gate` | feature | P1 | Bypass-SOP executed 5+ times/session manually; no harmonik verb | `kind:feature` `deploy` | check hk-gax8v overlap |
| `chore: verify hk-76n5g covers crew-boot AND all review-loop iter-N seeds` | chore | P1 | hk-76n5g LANDED but may only cover review-loop path, not crew-boot path | `kind:chore` `crew` | hk-76n5g EXISTS (verify scope) |

### P2 — High Value / Crew UX

| Proposed Title | Type | Pri | Rationale | Label | Existing? |
|---|---|---|---|---|---|
| `feat(daemon): include epic assignee in run event payloads` | feature | P2 | Every run_failed/stale requires a 2-hop br round-trip; denormalize at emit time | `kind:feature` `observability` | NEW |
| `feat(comms): --wake flag injects pane nudge on directed send` | feature | P2 | Idle crews ignore comms send without a tmux nudge; hk-wake.sh is manual per-crew | `kind:feature` `comms` | NEW |
| `feat(daemon): emit reviewer_submitted event; expose workflow_mode in queue status` | feature | P2 | DOT-mode stalls are invisible without this; queue status omits live workflow_mode | `kind:feature` `reviewer` | NEW |
| `fix(worktree-agent): pin GIT_DIR/GIT_WORK_TREE env vars in isolation mode` | bug | P2 | CWD resets between Bash calls can run git reset --hard against shared main | `kind:bug` `worktree` | NEW |
| `feat(crew): --keeper flag on crew start auto-wires session-keeper` | feature | P2 | Manual keeper enable ceremony per crew is error-prone; should be one flag at boot | `kind:feature` `crew` `keeper` | NEW |
| `feat(daemon): configurable commit-poll timeout + scenario post-merge gate` | feature | P2 | 30-min cap kills scenario beads before commit; scenario tag also skipped by default gate | `kind:feature` `gate` `scenario` | hk-i2ie5 (filed) |
| `docs: update AGENT_INDEX.md to reflect 2026-06 reality` | docs | P2 | Stale since 2026-05-12; crew system, named queues, validation-net entirely absent | `kind:docs` | NEW |
| `docs: create .claude/skills/captain/SHUTDOWN.md (fleet handoff runbook)` | docs | P2 | STARTUP.md exists; no symmetrical shutdown procedure; handoffs are ad-hoc | `kind:docs` `crew` | NEW |
| `chore: update session-handoff SKILL.md with crew-fleet awareness` | chore | P2 | Produces incomplete handoffs for multi-crew sessions; misses lane state | `kind:chore` `crew` | NEW |

### P3 — Ergonomics / Nice-to-Have

| Proposed Title | Type | Pri | Rationale | Label | Existing? |
|---|---|---|---|---|---|
| `feat(preflight): warn at dry-run if bead may need workflow OAuth scope` | feature | P3 | Prevents silent push-reject on workflow-touching beads; detectable at submit time | `kind:feature` `preflight` | NEW |
| `chore: add kerf-vs-direct-bead decision rule + graduation gate to CLAUDE.md` | chore | P3 | Agents must guess when to create a kerf work; decision rule reduces friction | `kind:chore` `docs` | NEW |

**Skipping (already exists / landed):** hk-svieq (non-ff retry, landed b4858a3c), hk-amed0 (pre-assign claim, landed 3a98091c), hk-76n5g (seed Enter, landed 1ed53a6d — verify scope only), hk-ycp62 (filed), hk-i2ie5 (filed), hk-lhv8i (filed), hk-ekap1 (session-keeper, code complete — operator action only).
