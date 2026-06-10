# A2 — Captain & Crew Retrospective: Friction Patterns
*Analysis date: 2026-06-10. Sources: chani/duncan/liet/stilgar transcripts, comms log --since 48h, crew memory files.*

---

## Pattern 1: Crew-Start Mission Seed Not Auto-Submitted

**Root cause:** `harmonik crew start` pastes the mission seed into the crew pane but does NOT send `Enter`. The crew sits idle at its prompt until the captain manually sends `tmux send-keys <pane> Enter`. Filed as `hk-jzpqo`. Observed on every fresh crew boot 2026-06-09/10.

**Cost per incident:** ~1–5 min of idle crew time; captain must manually nudge each pane after start; multiplied by 4 crews × N restarts = recurring overhead.

**Fix:**
- PROCESS: Captain's boot script adds `tmux send-keys <pane> '' Enter` immediately after `harmonik crew start`.
- HARMONIK-EMBED: `crew start` should emit `Enter` after pasting the seed. One-liner fix in the pane-seeding code path.

**Tag: HARMONIK-EMBED**
**Proposed bead:** `fix(crew): crew start auto-submits mission seed (hk-jzpqo)`

---

## Pattern 2: Crews Do Not Wake on `comms send` — No Push Channel

**Root cause:** `harmonik comms send` is durable but PULL-only. An idle crew's `recv --follow` Monitor feeds the pane's view but does NOT inject a new agent turn. A crew at rest only wakes on operator keystrokes or a live `tmux send-keys` nudge. `recv --follow` also does NOT advance the durable cursor — a re-follow replays the entire backlog. Observed: duncan + liet both silently ignored re-task `comms send` messages; captain had to `tmux send-keys` each.

**Cost per incident:** Crews park for extended periods after a `comms send` they never see; captain burns turns diagnosing "why isn't the crew acting?" Liet sat unresponsive until a manual pane nudge + eventual context-clear restart.

**Fix:**
- PROCESS: Harden each crew mission with `comms recv --from captain --follow` as a LIVE Monitor, AND include a `tmux send-keys` step in captain's toolbox for fleet-wide wakeup.
- HARMONIK-EMBED: `comms send` should optionally inject a tmux pane nudge via the session handle stored in the crew registry (`harmonik crew list` already has `handle`). A `--wake` flag or auto-wake for directed messages. Filed design at comms log 2026-06-09T12:59:46Z (SessionStart hook → background watcher injects via `tmux send-keys`).

**Tag: HARMONIK-EMBED + PROCESS**
**Proposed bead:** `feat(comms): crew auto-wake on directed send via pane injection`

---

## Pattern 3: OAuth `workflow` Scope Block Stalls Entire Lane

**Root cause:** The daemon's git OAuth token lacks the `workflow` scope. ANY bead that creates or edits `.github/workflows/*.yml` merges successfully but push-rejects with `refusing to allow an OAuth App to create or update workflow`. First hit by chani (hk-jdesv, release.yml), then liet (hk-jzepv, CI un-mask), then stilgar (hk-4mten). Additionally, implementers of adjacent beads organically create workflow files when not explicitly told not to — chani's `validate` bead (hk-o4j13) over-scoped and bundled a workflow file, hitting the same block.

**Cost per incident:** Multiple beads parked across 3 crew lanes; captain + operator surfaced it; cross-crew broadcast required; validate bead wasted a dispatch slot and required a re-scoped re-dispatch.

**Fix:**
- PROCESS: (a) Pre-flight token scope check (`gh auth status --show-token` for `workflow` scope) before starting any crew session touching CI/release. (b) Bead descriptions for non-workflow beads near CI must include explicit "do NOT create .github/workflows/* files."
- HARMONIK-EMBED: `harmonik queue dry-run` / `submit` could detect workflow-file-touching beads and warn about the scope requirement; or `harmonik crew start` could check token scopes at boot.

**Tag: PROCESS + HARMONIK-EMBED**
**Proposed bead:** `feat(preflight): detect workflow-scope gap at queue dry-run time`

---

## Pattern 4: Same-Package Parallel Beads — Test-Helper Redeclaration Collisions

**Root cause:** Multiple beads dispatched concurrently into the same Go package each invent package-level test helpers with generic names (`drawNonNilUUID`, etc.). Each bead's worktree builds green in isolation, but after sequential merges the combined package has redeclared identifiers. Per-bead gates (`go build ./... && go vet ./...` in the worktree) do NOT compile the merged tree, so the collision lands invisibly. Filed as `hk-ycp62`. Observed in `internal/queue` (hk-wifef + hk-9ztth) this session; previously in `internal/core`.

**Cost per incident:** Post-merge build breakage on main requiring a follow-up dedup pass; coordination overhead (duncan ↔ stilgar spent multiple comms rounds agreeing on `_wifef` / `_hk9ztth` namespacing).

**Fix:**
- PROCESS: (a) Pre-dispatch check — are ≥2 beads targeting the same package? If yes, enforce bead-id namespacing in their descriptions. (b) The second-landing crew runs `go build && go vet ./pkg/...` post-merge as a standing duty (crews now self-enforce this; it worked this session).
- HARMONIK-EMBED: Post-merge gate should run `go build ./... && go vet ./...` on the merged tree (not just the worktree). Filed as `hk-ycp62`.

**Tag: HARMONIK-EMBED**
**Proposed bead:** `fix(gate): post-merge gate runs go build+vet on merged main tree (hk-ycp62)`

---

## Pattern 5: CWD Drift — Worktree-Agent Sub-Shell Mutates Shared Main Tree

**Root cause:** `isolation: "worktree"` Agent sub-shells reset CWD to the shared checkout (`/Users/gb/github/harmonik`) BETWEEN Bash calls — not to the worktree. An agent doing `git -C <worktree>` in one call and `git reset --hard` in the next (assuming CWD persisted) runs the reset on shared main. Observed 2026-06-10 (liet pg0w5 reviewer: `git reset --hard` on main reverted an uncommitted `.beads/issues.jsonl`). Also: crew main-thread CWD drifts into `.claude/worktrees/agent-<id>/` after finishing a worktree Agent, causing `br` false `ISSUE_NOT_FOUND` and `harmonik` exit-17 with a wrong socket path.

**Cost per incident:** Reverted ledger changes, near-false-alarm escalations to captain, time lost diagnosing symptoms vs root cause.

**Fix:**
- PROCESS: Every worktree-agent prompt must include: "Your CWD may reset to the shared checkout between Bash calls; use `git -C <abs-worktree-path>` on EVERY git command; NEVER reset/revert/commit against `/Users/gb/github/harmonik`." Captain verifies `git status` on main after each git-mutating agent completes.
- HARMONIK-EMBED: The agent framework could pin `$GIT_DIR` and `$GIT_WORK_TREE` env vars for worktree-isolated agents, preventing accidental main-tree mutations regardless of CWD.

**Tag: PROCESS + HARMONIK-EMBED**
**Proposed bead:** `fix(worktree-agent): pin GIT_DIR+GIT_WORK_TREE env vars in worktree isolation mode`

---

## Pattern 6: Staged Debris in Main Working Tree from Daemon Merge-Overlap

**Root cause:** After two beads touching the same package land sequentially (e.g. hk-fkpb7 → hk-9ztth, both in `internal/queue`), the daemon's merge machinery leaves **staged but uncommitted changes** in the main working tree — specifically stale reversions of the second bead's already-landed code. This looks like a daemon wedge (captain initially diagnosed a merge wedge) but is actually inert debris. The daemon merges off worktree branches, not the main tree, so the debris doesn't affect live merges, but it confuses status checks, triggers false alarms, and would regress code if accidentally committed.

**Cost per incident:** Multi-message captain ↔ stilgar investigation cycle; false "merge wedge" hypothesis; cleanup must wait for a "true lull" (no merges in flight) to safely `git checkout -- .`.

**Fix:**
- PROCESS: After any same-package sequential merge, captain checks `git status` and files a "clear debris at next lull" note rather than treating it as an emergency.
- HARMONIK-EMBED: The daemon's merge path should `git checkout -- .` (or `git restore --staged .`) on the main working tree after each merge, ensuring the shared tree is always clean between merges.

**Tag: HARMONIK-EMBED**
**Proposed bead:** `fix(daemon): clear main working-tree staged debris after each merge`

---

## Pattern 7: Pre-Assigned Bead `--assignee <crew>` Wedges Daemon Dispatch

**Root cause:** A crew that runs `br create --assignee <crew>` or `br update <id> --assignee <crew>` on a task bead makes it undispatchable: the daemon claims via `br claim`, which rejects any pre-assigned bead with `SchemaMismatch (exit 4): "claim: issue already assigned"`. The bead exhausts 3 attempts (max_attempts_exceeded) with `run_id:null` — looks like queue-poison but isn't. Only the epic assignee is needed (for run attribution). Filed as daemon bug `hk-amed0` (P1, now landed as `3a98091c`).

**Cost per incident:** ~5 failed dispatches + investigator dispatch before root-cause; chani burned significant context on this before the pattern was documented.

**Fix:**
- PROCESS: Crew-launch SKILL.md gap: add explicit "do NOT set --assignee on task beads; assign only the epic." Now documented in memory (`reference_crew_beads_no_preassign`).
- HARMONIK-EMBED: `br claim` should treat "already assigned to the current daemon actor" as idempotent success. `hk-amed0` now landed — this is CLOSED for newly deployed daemons.

**Tag: PROCESS (doc gap) — HARMONIK-EMBED now LANDED**

---

## Pattern 8: Scenario-Test Beads Time Out on 30-Min Daemon Budget

**Root cause:** Scenario tests boot real daemons (a single test can `wait` 45s+). An implementer iterating + running the gate within a daemon-managed worktree exhausts the 30-min `commitPollTimeout` before committing → `no_commit`. Additionally, the per-bead commit-gate and review-loop skip `//go:build scenario` tests entirely (the default `go test ./...` omits the tag), so a broken scenario test merges green. Filed as `hk-i2ie5`.

**Cost per incident:** Every scenario-test bead requires the worktree-agent bypass (no daemon path), adding coordination overhead (author → independent reviewer → captain cherry-pick). Approximately 30% more orchestration steps per scenario bead.

**Fix:**
- PROCESS: Author scenario-test beads exclusively via `isolation: worktree` sub-agent + targeted gate `go test -tags=scenario -count=1 -run '<funcs>'` + independent reviewer + captain cherry-pick. Documented in memory (`reference_scenario_test_authoring`).
- HARMONIK-EMBED: (a) Raise or make configurable the commit-poll timeout for beads labeled `scenario`. (b) Add a post-merge `go test -tags=scenario -run <changed funcs>` gate. Filed as `hk-i2ie5`.

**Tag: PROCESS + HARMONIK-EMBED**
**Proposed bead:** `feat(gate): configurable commit-poll timeout + scenario tag post-merge gate`

---

## Summary Table

| # | Pattern | Tag | Proposed Bead |
|---|---------|-----|---------------|
| 1 | Crew start seed not auto-submitted | HARMONIK-EMBED | fix crew start auto-submit (hk-jzpqo) |
| 2 | Crews don't wake on comms send | HARMONIK-EMBED + PROCESS | feat comms crew auto-wake via pane injection |
| 3 | OAuth workflow scope stalls lane | PROCESS + HARMONIK-EMBED | feat preflight workflow-scope detection |
| 4 | Same-package test-helper collision | HARMONIK-EMBED | fix gate post-merge go build (hk-ycp62) |
| 5 | CWD drift → shared main mutation | PROCESS + HARMONIK-EMBED | fix worktree-agent pin GIT_DIR env vars |
| 6 | Staged debris from merge overlap | HARMONIK-EMBED | fix daemon clean main tree after merge |
| 7 | Pre-assigned bead wedges dispatch | PROCESS (now landed hk-amed0) | — |
| 8 | Scenario-test 30-min timeout | PROCESS + HARMONIK-EMBED | feat gate scenario timeout configurable |
