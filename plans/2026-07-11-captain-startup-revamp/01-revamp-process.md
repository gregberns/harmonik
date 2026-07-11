# Revamp Execution Process — Staged, Live-Fleet-Safe

Constraint: a live captain + 5 crews are running, keeper restarts fire unpredictably, and any
of them may boot mid-change. Governing rule for every stage: **fix the destination before
removing the old path.** An agent that boots mid-stage must land on EITHER a complete old path
or a complete new path — never a half of each.

Sequencing at a glance:

```
Stage 0  Make the manifest model correct        (new path becomes safe)
Stage 1  Compact operational STATE              (helps BOTH paths; pure deletion)
Stage 2  Flip the routers to `agent brief`      (old path stops being prescribed)
Stage 3  Shrink the contracts                   (old docs become stubs/references)
Stage 4  Bead-surface + code fixes              (independent lane, own beads)
Stage 5  Anti-rot enforcement + final teardown
```

Each stage ends with a **gate**: named checks that must pass before the next stage starts.
Stages 0–3 are docs/config only — no daemon restart, no binary deploy (renderer fixes in
Stage 0 are the one exception and follow the pre-deploy e2e gate + docs/daemon-redeploy.md).

Coordination note (all stages): announce each stage over comms to captain + crews before
editing files they boot from; prefer editing between keeper restarts (check `keeper doctor` /
gauge, don't edit a role's boot file while that role is actively re-booting). Nothing here
requires stopping the fleet.

---

## Stage 0 — Make the manifest model safe to converge on

The new model has five defects (SYNTHESIS §2). Until fixed, pointing agents at it trades
verbose-correct for lean-wrong.

Steps, in order:

1. **Resolve the two live contradictions as deliberate decisions (operator/admiral sign-off,
   one message):**
   - (a) Watcher set: confirm the two-watcher rule (`comms recv --follow` +
     `subscribe --types epic_completed`; run_stale/heartbeat FORBIDDEN — operator-flagged
     context burn 2026-06-11) still stands → fix `.harmonik/agents/captain/operating.md`
     Active-loop item 3. If the widening was intentional, fix STARTUP/SKILL instead — but do
     not leave both live.
   - (b) Escalation ladder: confirm captain→admiral (→operator) is intentional → all docs say
     admiral; SKILL.md's `--to operator` awaits get rewritten in Stage 3.
2. **Rewrite captain/operating.md** (draft: `drafts/agents/captain/operating.md`): two-watcher
   fix; wake step 3 shrinks to "project.yaml → lanes.json/captain-lanes (compacted) →
   direction-log" and DROPS the HANDOFF-captain.md read (brief embeds it); zombie-reconcile =
   `harmonik crew stop <name>`; add the `--wake keeper-restart` LEAN variant (from STARTUP
   L550–586); add identity-collision guard + direction-log LAPSE rule (single-copy rules from
   STARTUP); drop the "Skills I use" list (manifest generates it).
3. **Kill `_skills/` drift:** make `.harmonik/agents/_skills/` a generated mirror of
   `.claude/skills/` (script + check), or repoint manifest refs at `.claude/skills/` paths.
   Immediately re-sync agent-comms (backfill the presence-refresh ≤90s block into
   `.claude/skills/agent-comms` — it exists ONLY in manifest Bounds today) and give
   crew-launch a real body (the Stage-3 ~150-line reference; interim: repoint at the .claude
   file).
4. **File renderer beads** (code, daemon binary — dispatch via normal queue, deploy per
   docs/daemon-redeploy.md + pre-deploy e2e gate): (a) parse frontmatter `description` for
   injected-skill short-desc; (b) render `as: doc, presence: retrieved` refs with paths;
   (c) stamp the brief's Handoff section header "CLAIM, not ground truth — `harmonik digest`
   overrides". Docs must not depend on (a)/(b) landing — until they do, operating.md carries
   explicit paths for its retrieved docs (orchestrator-rules, orchestration-protocol-v2.md).

**GATE 0:** `harmonik agent brief --agent captain --wake fresh|keeper-restart` and
`--agent <each live crew>` render clean: correct watcher set, no HANDOFF-captain read, no
`---` skill descs (or explicit paths present), retrieved docs discoverable, LEAN variant
present. Diff `_skills/` vs `.claude/skills/` = empty. Read the full output as if booting:
every named file exists and every named command runs.

## Stage 1 — Compact operational state (safe immediately, biggest win)

Pure deletion of superseded state. Helps agents on BOTH boot paths; zero ordering risk —
git is the archive. Do this the same day; it removes ~35k tokens from every captain boot.

1. **captain-lanes.md** (draft: `drafts/context/captain-lanes.md`): delete L36–604; reconcile
   the kept block against admiral-initiatives 04:26Z (flagship DONE, deployed 59089968 —
   currently contradicts "REDEPLOY HELD"); pi forensics → one pointer at bead hk-y20d2; new
   header contract: exactly ONE current block, ≤60 lines, replace-in-place (DELETE superseded
   content on every update), `updated:` stamp, lanes.json is authoritative and is updated
   FIRST, prose second.
2. **direction-log.md** (draft: `drafts/context/direction-log.md`): strike the 3 expired
   entries (LAPSE rule); newest entry to TOP; fold the three overlapping 07-11 entries into
   ≤2; 3–5 lines each. Result ~25 lines.
3. **admiral-initiatives.md**: keep L1–49 as the whole file; move the program narrative
   (L53–169) into plans/2026-07-06-quality-system/ leaving one-line rows; delete audit markers
   (L212–266) and stale TOP/ACTIVE+ON-DECK+GATED tables (L170–210).
4. **Mission files**: purge kynes.md L27–56 (dead GATE-0 in goal) + L61–97 (Prior blocks);
   sweep the other 5 missions for the same disease. Discipline going forward (written into
   captain SKILL.md §mission in Stage 3): goal is REWRITTEN on re-task, exactly one
   `## Current State` block, superseded content deleted.
5. **Freshness tiebreak**: every state doc gets `updated:`; context/CLAUDE.md gains one line:
   newest stamp wins on conflict; `harmonik digest` beats all documents.
6. Delete-or-fill roadmap.md; drop it from read lists until real.

Announce over comms to the live captain: "tier-2 compacted; re-read on next wake; digest
remains ground truth." A mid-edit reader sees a shorter-but-true file — no breakage window.

**GATE 1:** captain-lanes ≤60 lines with ONE current-truth block; direction-log ≤60,
newest-first, zero expired entries; admiral-initiatives ≤~50; no mission file contains
"SUPERSEDED"; captain-lanes and admiral-initiatives agree on flagship/redeploy status. Then
watch the next real captain keeper-restart: it must reconcile cleanly against the compacted
tier (no re-derive thrash, no resurrected dead directives).

## Stage 2 — Flip the routers

Only now — the new path is correct (Stage 0) and the state it reads is sane (Stage 1).
AGENTS.md is auto-loaded by every session, so this single edit re-routes all future boots.
The old docs still exist untouched, so an agent mid-boot on the old path is unaffected.

1. **AGENTS.md** (draft: `drafts/AGENTS.md`): replace "Per-role load map" + "Start here" boot
   prose (L13–34) with ONE boot section ("run `harmonik agent brief`; its output IS your
   complete boot context; do not read STARTUP.md or the doc chain") + a separate Launching
   paragraph (start verbs, D2 rule — launching ≠ booting) + the bead line ("daemon-dispatched
   beads: agent-task.md IS your complete boot context; this map does not apply") + skill-tree
   canonicity (.claude/skills canonical; _skills generated). Delete L30 reading-order and the
   L66 "Don't skip the reading order" bullet (replace: "Exploring the KB, not booting?
   AGENT_INDEX.md is the map"). Fix §Workflow Pattern: `br update --status=in_progress` /
   `br close` marked "solo operator only — daemon-dispatched agents NEVER" (this poisons
   every bead worktree today).
2. **AGENT_INDEX.md**: strip the L7 boot-order banner; reframe as on-demand KB map; fix the
   duplicated Reviews section.
3. **STATUS.md**: gut to decisions-in-force + frozen-spec-ID rule + pointers
   (`harmonik digest`, ROADMAP, project.yaml) — or retire, folding decisions into
   project.yaml/AGENT_INDEX.
4. **HANDOFF.md**: 5-line tombstone → HANDOFF-<agent>.md + the brief; declare a home/retention
   rule for the 27 root HANDOFF-*.md files (e.g. .harmonik/handoffs/).
5. **Same commit**: banner-stamp STARTUP.md line 1 and crew-launch SKILL.md line 1 —
   "SUPERSEDED as a boot path — run `harmonik agent brief`. Retained temporarily for
   procedure reference (moving in Stage 3)." A stray reader bounces immediately.
6. **Propagated copies**: update cmd/harmonik/assets/templates/AGENTS.template.md and
   cmd/harmonik/assets/skills/captain/* in the same change, or `harmonik init` re-deploys the
   old world into new projects.

**GATE 2 (cutover verification):** grep AGENTS.md/AGENT_INDEX/STATUS for the old ritual = zero
hits; `harmonik agent brief` referenced in AGENTS.md. **Canary boot:** launch a scratch crew
(`harmonik start crew --name canary-boot ...`) on a trivial epic; verify from its pane that it
runs the brief, never opens STARTUP.md/AGENT_INDEX/STATUS, joins comms, mirrors assignee, and
reaches its queue in <5 min with boot context ~≤3k tokens. Then the next natural captain
keeper-restart is the captain canary — same checks. Rollback = revert the router commit.

## Stage 3 — Shrink the contracts

Routers no longer send anyone here, so these edits are low-risk; single-copy rules were
already re-homed in Stages 0–1 (verify against SYNTHESIS §7 checklist before deleting).

Order within stage (dependency: don't delete a rule before its new home exists):

1. **captain SKILL.md → ~200-line mechanics reference** (draft: `drafts/captain/SKILL.md`):
   the six sections (spawn/native grammar + exit codes; mission schema; mail/re-task;
   attribution; error-edge table absorbing STARTUP's crew-classification table, §4.3 liveness
   sweep, exit-17, healthy-fleet checklist, pane-nudge, 3 surviving anti-patterns; restart
   continuity). Delete §0.5, autonomy ×4, keeper bands, §11, tombstones, provenance tags,
   19-line frontmatter. Escalation target per Stage-0 decision.
2. **STARTUP.md → ≤20-line stub**: "Boot = `harmonik agent brief --wake <reason>`" + pointers
   (SKILL.md mechanics, keeper skill, docs/daemon-redeploy.md, specs/park-resume-protocol.md).
   Every unique rule must already be findable at its new home — diff the keep-list from
   SYNTHESIS §6 against the new homes before cutting. SHUTDOWN.md cross-refs re-pointed.
3. **crew-launch SKILL.md → ~150-line retrieved reference** (park/wake discrimination,
   invalid-handoff fallback, restart await-ack, failure classification). Boot/loop sections
   deleted (operating.md owns them); WAKE procedure says "run
   `harmonik agent brief --wake trigger:<id>`"; HK_PROJECT python snippet replaced with a
   native resolver; "§How you were launched" moved to captain SKILL.md.
4. **orchestrator-rules → ~150 lines**: frontmatter re-wired to manifest-retrieved; Monitor
   pattern/daily-loop CLI/stream-wave → one-line pointers to harmonik-dispatch; phase state
   (DOT endgame, kerf-beta, phase2 label) → project.yaml/ROADMAP; STARTUP pointers re-homed;
   HARD RULE demotion to ~5; §Autonomy untouched.
5. **Supporting skills**: agent-reviewer fixes (scope header self-review vs daemon-reviewer,
   five→eight, `incomplete-coverage` flag, commit-msg hook, schema out of frontmatter);
   beads-cli slim (write discipline + read surface); harmonik-dispatch de-stale (drop
   self-cancelling caveats, incident refs, manual tmux/$HARMONIK_PROJECT recipe; mark audience
   = orchestrators/crews).

**GATE 3:** run the SYNTHESIS §7 single-copy checklist — every rule resolvable from its new
home via brief → operating.md → named skill/spec (grep each). Cold-boot one crew + observe one
captain keeper-restart on the shrunk corpus; measure boot tokens (target: captain ≤~6k incl.
digest; crew ≤~3k). Independent review of the STARTUP/SKILL diffs (review-before-merge gate)
explicitly checking for lost rules, not style.

## Stage 4 — Bead-surface code fixes (parallel lane, own beads)

Independent of Stages 1–3; needs daemon redeploy so it follows the pre-deploy e2e gate.

1. Extract ONE shared reviewer-constraint renderer; fix agenttask_chb028.go L356–363 and
   L570–576 to mandate `harmonik write-review-verdict` (hk-9w79a) — two of three injection
   points still teach the banned hand-write today.
2. Add the explicit statement (AGENTS.md bead line, Stage 2, + agent-task.md header): beads
   boot from agent-task.md alone; no `agent brief`, no reading order.
3. (Stretch) bead-scoped worktree instruction filter — today every bead pays ~8–12k of
   orchestrator-audience CLAUDE.md + 15 skill descriptions.

**GATE 4:** canary bead through implement→review→merge; reviewer verdict lands via
write-review-verdict; no bead-issued terminal br transitions in events.

## Stage 5 — Bound the rot permanently

1. **Mechanical check** (script, runnable by agent-config-reviewer and the admiral audit —
   context/CLAUDE.md L47–55 already assigns ownership; this gives it a checkable definition
   of done). Fails on: captain-lanes/direction-log over line caps; any past `expires:`;
   >1 CURRENT TRUTH heading in captain-lanes; "SUPERSEDED" in any mission file;
   captain-lanes vs admiral-initiatives freshness disagreement; `_skills/` vs
   `.claude/skills/` drift; forbidden strings in routers (STARTUP.md-first, the reading-order
   ritual). Wire into lefthook or the admiral audit loop.
2. **Write discipline is replace-in-place everywhere**: captain-lanes, direction-log (cap
   enforced), mission goal/Current State, admiral-initiatives. Git is the archive; prose
   "SUPERSEDED" markers are banned.
3. **Teardown**: after 1 week of clean boots, delete the STARTUP stub's transitional banner
   language, scripts/captain-boot-digest.sh + crew-boot-digest.sh (if `harmonik digest` /
   the brief cover them), and regenerate cmd/harmonik/assets copies.

**GATE 5:** rot-check green in CI/audit for a week; one full keeper-restart cycle per role
with no old-path reads (verifiable from pane transcripts); operator sign-off.

---

## Failure/rollback notes

- Every stage is a small commit set; rollback = revert that stage's commits. Stage 2 (router
  flip) is the only user-visible cutover and reverts cleanly in one commit.
- If a live agent boots mid-Stage-2 and wedges: its pane transcript shows which path it took;
  nudge with "run `harmonik agent brief --wake fresh`" — the brief is self-contained either way.
- Do not run Stage 3 deletions and Stage 0/1 re-homes in the same commit — the re-home must be
  merged and verified before its source is cut (lost-rule risk is the whole game).
