# Startup-doc revamp — cutover plan & open questions

Stage-4 consolidation (2026-07-11). Inputs: eight drafts under `drafts/`, eight independent
verify passes (fresh-eyes, one per draft), `00-SYNTHESIS.md` keep/cut/move tables,
`01-revamp-process.md` stage plan. This file is the single place to read before touching
anything live.

**Headline:** every draft verified as a net improvement and consistent with the manifest-boot
model (`harmonik agent brief` is the one boot command; the STARTUP.md + reading-order ritual
dies). **All eight came back FIX-FIRST — none ships as-is.** Every fix is small (1–5 lines of
doc text) except two that are *code/sequencing* gates: `harmonik digest` parity and the brief
renderer's retrieved-refs gap. Two drafts (`AGENTS.md`, `captain/STARTUP.md`) had their fixes
applied by the verifiers and those amended versions are now persisted to disk (2026-07-11,
this consolidation) — their remaining items are companion/sequencing work, not text edits.

---

## 1. Inventory — what the revamp produced

All paths under `/Users/gb/github/harmonik/plans/2026-07-11-captain-startup-revamp/`.

| Draft | Replaces (live) | What it is |
|---|---|---|
| `drafts/AGENTS.md` | `AGENTS.md` (= `CLAUDE.md` symlink) | Router rewrite: one `## Booting` section (manifest agents → `agent brief`; beads → `agent-task.md`; solo → `/session-resume`); per-role load map + reading-order ritual deleted. **Verifier-amended on disk.** |
| `drafts/agents/captain/operating.md` | `.harmonik/agents/captain/operating.md` | Captain manifest operating section (~50 lines): identity/collision guard, wake steps, two-watcher rule (run-telemetry subscribe FORBIDDEN), direction-log LAPSE rule, LEAN keeper-restart, `harmonik digest` as the single ground-truth pass. |
| `drafts/captain/SKILL.md` | `.claude/skills/captain/SKILL.md` (842→241 lines) | Captain mechanics only; absorbs STARTUP.md's operational content (classification, staffing 5a–5d, liveness sweep, healthy-fleet, anti-patterns); boot + autonomy delegated out. |
| `drafts/captain/STARTUP.md` | `.claude/skills/captain/STARTUP.md` (910→~55 lines) | Tombstone stub: SUPERSEDED banner + pointer list. Header comment IS the apply-gate (5 preconditions). **Verifier-amended on disk.** |
| `drafts/context/captain-lanes.md` | `.harmonik/context/captain-lanes.md` (605→45 lines) | Tier-2 rewrite: ONE CURRENT TRUTH block, ≤60-line cap, replace-in-place, lanes.json authoritative, history banned (git is the archive). |
| `drafts/crew-launch/SKILL.md` | `.claude/skills/crew-launch/SKILL.md` (609→141 lines) | Crew reference flipped to retrieved-not-injected; boot/loop delegated to `.harmonik/agents/crew/operating.md`; keeps park/wake, restart verification, failure classification. |
| `drafts/orchestrator-rules/SKILL.md` | `.claude/skills/orchestrator-rules/SKILL.md` (249→203 lines) | Standing-rules condensation; header re-anchored to the manifests; §Autonomy kept verbatim; state (phase/beta/friction) evicted to their owners. |
| `drafts/agents/_skills/SYNC.md` | NEW — `.harmonik/agents/_skills/SYNC.md` | Design note + spec for `scripts/agents-skills-sync.sh` (check/apply/rot/all): kills the two-skill-tree drift, defines the 7 rot checks (R1–R7), names 3 renderer/reviewer code beads. |

Process docs: `00-SYNTHESIS.md` (audit + keep/cut/move), `01-revamp-process.md` (stages/gates),
this file (`02-cutover-and-open-questions.md`).

---

## 2. Verify results — SHIP vs FIX-FIRST

All eight verdicts: **FIX-FIRST**. Grouped by what actually blocks: (A) text edits still owed
on the draft, (B) companion content that must exist somewhere else first, (C) code gates.

### 2.1 `drafts/agents/captain/operating.md` — FIX-FIRST (text edits)

- **[A] Bead write discipline lost with no carrier.** Live doc carried "beads-cli — br read
  surface + write discipline (no terminal transitions)"; draft cuts the whole Skills section,
  and its Retrieved-docs list names only the two `as: doc` refs. Given the draft's own stated
  renderer gap (retrieved refs not printed in the brief), a booted captain gets NO statement
  and NO pointer to daemon-owns-terminal-transitions (the rule whose violation caused the
  in_progress claim-livelock, hk-l2xd1). **Fix:** add the one-line rule to Bounds AND list all
  FOUR manifest retrieved refs (add `beads-cli` + `harmonik-dispatch` skills) in
  Retrieved-docs.
- **[A] Stranded-bead remediation verb dropped:** wake step 5 lists "stranded in_progress
  beads" with no action. Restore `br update <id> --status open` (this reconcile is the
  captain's sanctioned exception).
- **[A] Confabulated provenance:** "(operator-locked, 2026-06-11)" on the run-level-telemetry
  ban exists in no repo doc. Cite **M3/hk-039z** (captain SKILL.md L221–229/L448–450; bead
  closed 2026-06-20) instead.
- **[A, one clause] Silent crew death:** with run_failed unsubscribed, failure detection =
  crews self-reporting + ops-monitor IMMEDIATEs; neither fires for a crew that dies silently.
  Add one clause: "ops-monitor owns crew liveness (WE4/§5); its stale-latest.json tripwire is
  the backstop" — which requires the tripwire to survive (see 2.4 item 4).
- **[B] Companion:** retire/redirect the root `HANDOFF-captain.md` so a stray reader doesn't
  treat it as a competing tier-1 (draft correctly reads the brief-embedded handoff instead).

Verified good (do not re-litigate): two-watcher set + telemetry ban match canon; zombie fix =
`harmonik crew stop <name>` (live doc's `br update` for zombies was wrong); `harmonik digest`
verb exists; lanes.json/direction-log paths + LAPSE rule verbatim-match the source.

### 2.2 `drafts/AGENTS.md` — FIX-FIRST → text fixes DONE; companions remain

The three verified defects are **already applied on disk** (this consolidation): (1) solo /
implementer-orchestrator boot route restored as one Booting bullet (`/session-resume` reads
HANDOFF.md + orchestrator-rules + harmonik-dispatch — no doc-chain ritual); (2) global-skills
resolution guard folded into Precedence ("do NOT resolve skill names against
`~/.claude/skills/`"); (3) captain-tiers sentence now says tiers are "surfaced through
`harmonik agent brief`, never read as a boot chain."

- **[B] Must land WITH companions** (banner says so; none are drafted yet): `AGENT_INDEX.md`,
  `STATUS.md` (preserve the `#decisions-locked-in-2026-04-19` anchor!), `HANDOFF.md`,
  `cmd/harmonik/assets/templates/AGENTS.template.md`.
- **[B] Must land with (or after) the STARTUP tombstone** — "Do not read STARTUP.md" +
  a live 910-line STARTUP.md is a contradiction only the tombstone resolves.

Verified good: intentional removals (per-role load map, Start-here ritual, read-STARTUP-FIRST,
crew do-NOT-load list) are correct — restoring any would re-create the two-competing-models
problem. `agent brief` flags, five manifest types, Launching/D2, kerf/bead conventions all
check out. The SOLO-OPERATOR-ONLY annotations on `br update/close` are a strengthening — keep.

### 2.3 `drafts/captain/SKILL.md` — FIX-FIRST (text edits + one decision)

- **[A] REVERSED guardrail — crew-start collision auto-retry.** Live §8 (+ exit table + §9):
  on non-17 `crew start` failure, surface the error, do NOT auto-retry under a different
  name/queue. Synthesis marked it KEEP; draft §1 exit table instead says "pick a distinct free
  name/queue and re-launch on your own authority." A collision usually means the lane is
  ALREADY staffed — auto-relaunch double-staffs the epic, violating the draft's own
  one-lane-one-epic-one-crew invariant. **Fix:** restore no-auto-retry, or (operator call, see
  §4 Q4) record the autonomy lift WITH a mandatory diagnose-first step ("check whether the
  colliding crew already owns this epic").
- **[A] Incoherent escalation:** §6 "can't re-arm (daemon down) → escalate to the admiral" —
  comms IS a daemon RPC (exit 17 when down); the admiral is unreachable by exactly that
  mechanism. Restore the live rule: daemon-down escalation goes to the **human operator**.
- **[A] Escalation-target split:** header says "escalation target throughout is the admiral,"
  but operating.md Bounds requires **operator** approval for locked-decision reversal and
  destructive repo ops (admiral only for new-initiative ranking). Split the four-case list to
  match the manifest.
- **[A/B] Gap-3 dual-surface status convention has NO surviving home** — no draft states the
  captain's routine completion/status reporting duty (both channels: status line + `comms send
  --topic status`, + the no-join `comms log` fallback). Needs one paragraph in operating.md's
  active loop or draft §5. Target (operator vs admiral vs watch) is an operator call — §4 Q3.
- **[A] Restore §6 read-progress surfaces:** `comms log --from <crew> --topic status --since
  30m` + `br comments list <epic_id>`, incl. the nuance that `comms log` does NOT advance the
  recv cursor (using recv eats the captain's own inbox) and that reading triggers zero failure
  action.
- **[A, minor] (a)** light-orchestrator concurrency guard (no ~10+ parallel Agent-tool
  sub-agents while the daemon dispatches) — add here or to orchestrator-rules; **(b)** verify
  `docs/daemon-redeploy.md` owns the LULL-DEPLOY "ff-after-push for the non-ff race" nuance
  before trusting the pointer; **(c)** keep one line of WHY on the launcher requirement (a
  bare `claude --remote-control captain` can't be keeper-cycled); **(d)** add the
  TIER/LOADED-BY/OWNER header the sibling orchestrator-rules draft carries.
- **[C→cutover] §0.1 consensus-first gate cut** without exercising the "3 lines in
  operating.md if kept" option — behavioral change from adopt-don't-block(+operator redline)
  to blocking escalate-to-admiral. Operator call — §4 Q2.

Verified good: everything else verified RELOCATED not lost (boot→operating.md,
autonomy→orchestrator-rules, keeper band→keeper skill); Gap-1 attribution, mission schema,
guardrail set, restart continuity, absorbed STARTUP content all present; `--queue` default,
D3 semantics, zombie one-liner fact-checked against source.

### 2.4 `drafts/captain/STARTUP.md` (tombstone) — FIX-FIRST as a *standalone apply*; text is done

The amended stub (on disk) is correct; the verdict is about **sequencing**. It is safe ONLY
after its header preconditions hold:

1. **[B] operating.md draft lands FIRST** — the LIVE operating.md still arms the forbidden
   4-type run-telemetry subscribe and lacks the identity guard / LAPSE rule / LEAN resume.
   Tombstone-before-operating.md boots the captain into the banned pattern.
2. **[B] captain SKILL.md draft lands** (classification, staffing, liveness sweep,
   healthy-fleet, anti-patterns).
3. **[C] DIGEST PARITY** — `harmonik digest` today emits only
   queue/commits/in-progress-beads/notes/events and its br_ready collector errors (exit 2).
   It must gain: comms who, crew list, tmux fleet, PAUSED-QUEUE sweep (regex must cover
   `paused|complete-with-failures` — draft SKILL.md's healthy-fleet #6 currently checks only
   "paused-by-failure": fix that too), ready beads, kerf next/map — BEFORE
   `scripts/captain-boot-digest.sh` is called retired. Until then zombie + paused-queue
   detection have no boot home. Daemon-binary work → bead, pre-deploy e2e gate applies.
4. **[B + operator] Four rules currently dispositioned NOWHERE** (§4 Q1): ≤5-min
   REFRESH-AND-STAFF backlog pull + "missed staffing failure" condition; CE4 ops-monitor
   checks map incl. the **stale-latest.json ⇒ monitor-down tripwire** (this tripwire is what
   makes BOTH "ops-monitor owns review-bypass" and "keeper doctor owns M11" cuts safe — as
   drafted, nothing detects a dead ops-monitor); keystone-gated bead marking (dep-on-open-epic
   ⇒ silent group_failure — mark BLOCKED, don't dispatch); goal-state.json once-per-restart
   re-ground + §4.4 idle realign (previously operator-locked).
5. **[B] Confirm SHUTDOWN.md carries the tier-2 end-of-session discipline** (update
   captain-lanes + direction-log BEFORE writing the handoff); re-point SHUTDOWN.md and
   `cmd/harmonik/assets/skills/captain/` in the same change.
6. **[B] Mixed-era contradiction:** live STARTUP says "lanes.json is NOT a boot-read"; draft
   operating.md makes it the authoritative registry (deliberate flip per SYNTHESIS §5/§6).
   Fine — but both docs must flip in the same landing so no captain reads both eras.

### 2.5 `drafts/context/captain-lanes.md` — FIX-FIRST (contract lines + one sync)

- **[A] Add COMMIT-TIER-2-IMMEDIATELY to the CONTRACT block** ("update ⇒ `git add <this
  file>` + commit in the same action"). MORE critical under replace-in-place: a daemon
  merge-checkout clobbering an uncommitted rewrite destroys the ONLY truth block (already
  happened once — a8d4591b reset the tree).
- **[A/B] Rehome the git-add-SPECIFIC-PATH-only rule** (never `git add -A` for captain doc
  commits — a blanket add once committed a daemon-reverted source tree, dc316cd6). Only
  durable home today is the file being rewritten; give it to orchestrator-rules §CWD/commit
  discipline or operating.md Bounds.
- **[A] Restore the `.claire/worktrees/agent-*` placeholder-purge half** of the fail-closed
  hooks gate (draft kept only the lint-debt half; flipping fail-closed with the 4 stale
  placeholders failing `make check` reproduces the 07-11 fleet-wide merge outage).
- **[A] Carry the hook-mitigation recovery pointers:** backup at
  `.harmonik/context/hook-mitigation-backup/` (+ `lefthook install` reinstall), and the
  **orphaned `git stash`** (settings.json drift) that stilgar must reconcile — a live hazard
  (stash-stack shifts while the daemon runs) that currently loses its only owner. §4 Q6.
- **[A/B] Watch session durable record:** the always-on watch's existence otherwise lives
  only in a direction-log entry that LAPSES 2026-07-13 — the exact condition that produced
  the prior 43h unnoticed watch outage. Give it a lanes.json row. §4 Q7.
- **[B, same deploy action] lanes.json sync:** lanes.json (01:48Z) still says kynes owns
  GATE-0 / epic hk-hcrvb active, while the draft (04:30Z, verified true via `br show`) says
  epic CLOSED, kynes on hk-cdpxu. The draft's own contract says lanes.json-first — deploying
  without the same-action lanes.json update ships a conflict where the declared
  source-of-truth is the stale side. Also add the `dehardcode` deprioritized entry.
- **[A, small] stamp `expires:` + owner on the "keeper-missing: hawat/piter/stilgar" dated
  item (context/CLAUDE.md forced-write requires both), or move it to a lanes.json note; credit
  **hk-j0p1r (PR #30)** alongside hk-y20d2 for the stdin fix; note the 60-line-cap
  "enforced by rot-check" claim is aspirational until the sync script lands (GATE 5).
- **[B] Companion edits the banner promises don't exist yet:** direction-log.md compaction,
  admiral-initiatives.md trim, missions/kynes.md purge, **context/CLAUDE.md** retention-line
  updates (live context/CLAUDE.md still teaches the old boot order and says this file holds
  the operator priority order — direct contradiction until edited).

### 2.6 `drafts/crew-launch/SKILL.md` — FIX-FIRST (re-homes must land before the flip)

The draft's premise (retrieved, not injected) contradicts the live crew manifest
(`presence: injected`) and operating.md L22 ("authoritative boot sequence"). The flip is
right, but if it lands before these re-homes, five guardrails vanish from every crew's loaded
context:

- **[B → `.harmonik/agents/crew/operating.md` Bounds]** (1) FALSE-DRAIN guard: empty
  `br ready` ≠ drained — also check in-progress beads + epic-blocked beads + paused/failed
  queues before posting drain; (2) do NOT spin-poll `br ready` more than every 10 min;
  (3) do NOT try to unblock beads yourself (captain judgment); (4) MUST NOT spawn Agent-tool
  sub-agents for epic work — use the daemon queue (harmonik-dispatch is only
  presence:retrieved for crews, so this needs an injected home).
- **[B → `specs/crew-handoff-schema.md`]** The `## Current State` field contract (queue_id,
  in_flight, monitor, next_action, blockers, translations + "absent section ⇒ all tier-1
  fields unknown, re-derive") currently has NO home — captain and crew can disagree on the
  block's shape.
- **[A]** Restore two landmine notes: `br comments add` TEXT is positional / there is NO
  `--body` flag (agents guessed `--body` and silently failed the mandatory feed); and
  `--heartbeat 60s` on the subscribe monitor (liveness between events; the 10-min timer and
  stream-death detection lean on it).
- **[A]** Name the wake trigger id: `harmonik agent brief --wake trigger:queue` (the crew
  manifest defines exactly one trigger).
- **[A/B]** `$STATUS_TARGET` resolution (3 lines) must move into operating.md or be injected
  by the brief — a crew that never pulls the retrieved reference otherwise posts status with
  an unset target.
- **[B]** Flip `crew/manifest.yaml` `presence: injected → retrieved` AND fix operating.md
  L22's "authoritative boot sequence" wording in the same landing.
- **[A, verify-deliberate]** crew:`<name>` label fallback dropped (fine iff `br --assignee`
  is guaranteed); "prefer beads over handoff on disagreement" softened; "unknown message →
  log and no-op" dropped — confirm each is intended.

Verified good: all eight one-copy rules confirmed present in crew operating.md at the cited
lines; agent-comms already carries the ≤90s presence-refresh + wake-idle-peer (the Stage-0
backfill LANDED — the remaining gap is only the `_skills` mirror, closed by the first sync
`--apply`); captain draft owns D2/D3 + mission-overwrite discipline.

### 2.7 `drafts/orchestrator-rules/SKILL.md` — FIX-FIRST (MOVE-without-destination ×3)

- **[B] friction-P1 rule:** draft claims it lives in `.harmonik/context/project.yaml` — it
  does NOT, and no project.yaml draft exists. Land the guardrail in project.yaml in the same
  change (with PHASE-3-DOT + kerf-beta status if keeping their MOVE story, incl. the
  `docs/kerf-beta-feedback.md` logging instruction) or restore the lines here.
- **[B] Monitor pattern:** draft points at harmonik-dispatch as canonical owner, but the live
  harmonik-dispatch skill does NOT contain filter-by-event-TYPE (+ the run_completed-keyed-by-
  run_id rationale), the `tail -F .harmonik/events/events.jsonl` fallback, the
  no-daemon.log/no-per-run-file note, or the re-arm-on-Monitor-timeout note. Patch
  harmonik-dispatch in the same change or keep the full block here. Also restore the
  STREAM-NOT-WAVES two-step per-completion procedure (merge returner → spawn exactly one
  replacement or note draining) — currently lost everywhere.
- **[A] HARD-RULE vocabulary:** draft demoted 12 HARD RULE tags to 5; draft AGENTS.md still
  advertises "the HARD-RULE exceptions." Minimum: restore the tag on THE THREE EXCEPTIONS and
  HARMONIK-IS-DEFAULT-DISPATCHER, or change AGENTS.md's wording. Decide tag-set deliberately.
- **[A]** Restore the pre-deploy gate's closing sentence ("the missing thing is the harness —
  build it (codename:daemon-testbed), do not test in prod") — the anti-loophole + epic
  pointer; keep a one-line "this section overrides the `feedback_captain_lean_while_operator_away`
  memory; do NOT amend ~/.claude/CLAUDE.md" marker; fix the "§edge-table" pointer to the
  draft captain SKILL.md's real heading "## 5. Errors & edges."
- **[A, deploy-mechanical]** HTML comment sits ABOVE the YAML frontmatter — strip at deploy
  or frontmatter won't parse. (Same pattern in captain/SKILL.md and crew-launch drafts —
  make "strip the DRAFT banner" a cutover step for ALL drafts.)
- **[B, dangling]** live project.yaml header still says "Captain reads on every boot
  (STARTUP.md Step 0a)" — update in the project.yaml companion edit.

### 2.8 `drafts/agents/_skills/SYNC.md` — FIX-FIRST (spec bugs; nothing dropped)

Zero load-bearing drops (new file); all major claims fact-checked TRUE (ResolveRef
`_skills`-first shadowing; agent-comms mirror 6 lines behind; crew-launch mirror = 1-line
stub; mirror set complete vs today's manifests; brief renderer drops `as: doc` refs; the two
agenttask reviewer blocks still teach the banned hand-write+`mv`). Fixes:

- **[A] R7 regex can't match its own motivating case:** `STARTUP\.md FIRST` misses the live
  backtick-quoted "read `` `.claude/skills/captain/STARTUP.md` `` FIRST". Flag ANY STARTUP.md
  mention in the routers instead. And add R7 to the "will FAIL against today's files" list
  (it fires on the live reading-order lines; clears at Stage 4, not GATE 1).
- **[A] Mirror-set completeness check missing:** derive expected bare refs from
  `.harmonik/agents/*/manifest.yaml` at runtime; FAIL on (a) a manifest bare ref not in the
  set, (b) a `_skills/` subdir outside the set. Without it a future bare ref (e.g. `keeper`)
  drifts invisibly — a blind spot in an anti-drift tool.
- **[A] `--apply` hand-edit guard:** replace the mtime heuristic (unreliable after
  checkout/clone) with a pure-git blob comparison.
- **[A, nits]** stale snapshot count (19 CURRENT TRUTH blocks today, not 22); cite
  `ResolveRef` + SPEC §6 instead of `manifest.go L289`.

---

## 3. Cutover checklist — ordered, with a captain + 5 crews live

Ground rules for the whole cutover: **(i)** live agents keep running on old docs until their
next keeper restart — docs are read at boot, so the doc SET must be boot-coherent at every
commit, never "half old model, half new"; **(ii)** re-home BEFORE cut (Stage-3 rule) — a rule
leaves its old home only in the commit where its new home lands; **(iii)** every captain-doc
commit uses `git add <specific paths>` (never `-A`) and commits immediately; **(iv)** strip
the `<!-- DRAFT ... -->` banners at landing (three drafts have them ABOVE frontmatter);
**(v)** daemon-binary changes (digest parity, renderer beads) go through the normal queue +
pre-deploy e2e gate + `docs/daemon-redeploy.md` — never doc-style edits.

### Step 0 — decisions + draft fixes (no live changes)

- [ ] 0.1 Get operator answers to §4 (Q1–Q4 gate later steps).
- [ ] 0.2 Apply the §2 [A] text fixes to each draft in `drafts/` (operating.md ×4,
      captain/SKILL.md ×6, captain-lanes ×5, crew-launch ×4, orchestrator-rules ×4, SYNC ×4).
      AGENTS.md and STARTUP.md are already amended on disk.
- [ ] 0.3 Draft the missing companions (currently NOT drafted): `AGENT_INDEX.md`, `STATUS.md`
      (preserve `#decisions-locked-in-2026-04-19`), `HANDOFF.md`, `AGENTS.template.md`,
      `project.yaml` (friction-P1 + phase/beta + fix its "Step 0a" header), harmonik-dispatch
      additions (Monitor detail + stream two-step), `specs/crew-handoff-schema.md` Current
      State fields, crew `operating.md` Bounds additions, direction-log compaction,
      admiral-initiatives trim, missions/kynes.md purge, `context/CLAUDE.md` retention lines,
      SHUTDOWN.md check/re-point, `HANDOFF-captain.md` tombstone.
- [ ] 0.4 File the code beads (dispatch via normal queue; independent of doc landings):
      **(a)** brief renderer — render `as: doc/retrieved` refs with paths + parse frontmatter
      `description:`; **(b)** brief handoff header stamped "CLAIM — `harmonik digest`
      overrides"; **(c)** shared `renderReviewerConstraint()` so agenttask_chb028.go
      (L356–363, L570–576) stops teaching the banned hand-write-review.json (pasteinject.go
      already mandates `write-review-verdict`); **(d)** `harmonik digest` parity (§2.4 item 3
      section list + fix the br_ready collector exit 2); **(e)** `scripts/agents-skills-sync.sh`
      per the fixed SYNC.md spec.

### Step 1 — additive landings (zero behavior change for running agents)

Safe any time; nothing reads these at boot yet, or they only ADD rules to already-loaded docs.

- [ ] 1.1 Land project.yaml guardrails, harmonik-dispatch additions,
      crew-handoff-schema fields, crew operating.md Bounds additions, agent-comms untouched
      (already canonical). These are the destinations that make later cuts legal.
- [ ] 1.2 Land `_skills/SYNC.md` + the sync script; run first `--apply` (closes the
      agent-comms mirror lag; copies the current 609-line crew-launch into the 1-line stub —
      verbose-but-correct interim). Wire the DRIFT check (not `--rot`) into lefthook,
      path-gated. `--rot` stays audit-only until GATE 1.
- [ ] 1.3 Verify: `agents-skills-sync.sh` exits 0; a crew booting NOW gets identical content
      to before (mirror == canonical).

### Step 2 — captain-side swap (one tight landing; do during a quiet window)

- [ ] 2.1 `keeper hold` on the captain (suspend the ACT/restart cutoff so a keeper cycle
      can't fire mid-swap and boot into a half-landed doc set).
- [ ] 2.2 Single commit: fixed `agents/captain/operating.md` + fixed `captain/SKILL.md` +
      fixed `orchestrator-rules/SKILL.md` + `_skills` mirror re-sync + re-pointed SHUTDOWN.md
      + `cmd/harmonik/assets/skills/captain/` refs. (operating.md must never land AFTER the
      tombstone; landing all captain docs together avoids mixed-era reads.)
- [ ] 2.3 Same action, second commit (tier-2 state, specific paths only): rewritten
      `captain-lanes.md` + synced `lanes.json` (pi lane → follow-ups; dehardcode entry; watch
      row per Q7) + direction-log compaction + admiral-initiatives trim + context/CLAUDE.md
      retention lines + missions/kynes.md purge. Commit immediately.
- [ ] 2.4 Verify BEFORE releasing: `harmonik agent brief --wake fresh` (render only, from a
      scratch pane, NOT a real captain boot) shows the new operating section, the two-watcher
      rule, all four retrieved refs by explicit path, and the embedded handoff labeled CLAIM.
- [ ] 2.5 `keeper release`; at the captain's next natural keeper restart it boots the new
      model. Optionally force one deliberate keeper cycle now and watch the boot: expect
      brief + single `harmonik digest` pass, NO STARTUP.md read, NO captain-lanes chain-read,
      context burn far below the old ~900+600-line ritual.

### Step 3 — crew-side flip

- [ ] 3.1 Single commit: fixed `crew-launch/SKILL.md` (banner stripped) + crew
      `manifest.yaml` presence flip injected→retrieved + operating.md L22 wording fix +
      `_skills` mirror re-sync. Prereq: Step 1.1 Bounds additions already live.
- [ ] 3.2 Do NOT force-restart the five crews (hawat, kynes, piter, stilgar, yueh). Changes
      take effect per-crew at each one's next keeper restart / park-wake.
- [ ] 3.3 Canary ONE crew: pick the next crew due a keeper cycle (or nudge one idle crew),
      watch it boot via `harmonik agent brief`; verify it joins comms, mirrors assignee,
      resolves `$STATUS_TARGET`, posts the boot bookend, and does NOT read the old 609-line
      skill. Only then let the rest cycle naturally.

### Step 4 — router + tombstone (the point of no return for the old model)

Gate: Step 2 landed AND digest-parity bead (0.4d) deployed per `docs/daemon-redeploy.md`
(captain self-authorizes the restart per standing rules). The tombstone's header precondition
list is self-checking — walk it.

- [ ] 4.1 Single commit: amended `AGENTS.md` + companions (`AGENT_INDEX.md`, `STATUS.md`,
      `HANDOFF.md`, `AGENTS.template.md`) + `captain/STARTUP.md` tombstone (banner comment
      retained — it documents the precondition that was met) + `HANDOFF-captain.md`
      retire/redirect.
- [ ] 4.2 Retire `scripts/captain-boot-digest.sh` (only now — digest parity proven: run both
      side-by-side once and diff section coverage).
- [ ] 4.3 Verify: grep the routers for old-boot strings (R7 semantics) = clean; a fresh
      `harmonik start captain` in a scratch project boots entirely from the brief.

### Step 5 — enforcement + soak

- [ ] 5.1 After GATE 1 (caps hold: captain-lanes ≤60 lines / 1 block, direction-log ≤60/10),
      wire `--rot` into the admiral audit (`--all` → DRIFT_MAJOR mapping); lefthook only when
      R1–R7 are all green in CI-mode.
- [ ] 5.2 Soak checks across the next few days: every keeper restart of captain/crews boots
      via brief (grep session logs for STARTUP.md reads = zero); ops-monitor paused-queue
      flag doesn't false-positive on kynes-q (post-flagship pause expectation was dropped from
      captain-lanes — if it fires, that's the Q5 residue); renderer beads (0.4a/b) landing
      lets operating.md's explicit-path workaround be removed later (file a follow-up bead,
      don't block cutover on it).
- [x] 5.3 Reconcile the orphaned `git stash` (Q6) — triage completed 2026-07-11 (hk-j5yer.15):
      zero stashes found; all were cleared by 2026-07-03 recover/plan-folders commits. Q6 resolved.

Rollback at any step: every landing is a small commit set on specific paths; `git revert` the
landing commit(s) and re-run the sync script. Old docs remain in git history (the archive) —
no content is destroyed at any step.

---

## 4. OPEN QUESTIONS — operator decisions needed

1. **The four undispositioned STARTUP rules (§2.4 item 4)** — for each, name the home or
   approve the cut: (a) ≤5-min REFRESH-AND-STAFF backlog pull + missed-staffing-failure
   condition (suggested: orchestrator-rules §ANTI-IDLE); (b) CE4 ops-monitor checks map + the
   stale-latest.json monitor-down tripwire (suggested: captain SKILL.md §5 — note the
   tripwire is what makes two other approved cuts safe); (c) keystone-gated bead marking
   (suggested: captain SKILL.md §Staffing); (d) goal-state.json re-ground + §4.4 idle realign
   — this one was previously operator-locked, so cutting it needs your explicit sign-off.
2. **Consensus-first gate (§0.1):** the draft replaces "3-agent consensus → ADOPT with
   redline window, don't block (operator redline always wins)" with a BLOCKING
   escalate-to-admiral. Intended behavior change, or restore adopt-don't-block as 3 lines in
   operating.md?
3. **Routine status reporting target:** the live dual-surface convention reported to the
   OPERATOR; the manifest model suggests the admiral; watch also exists. Who receives the
   captain's routine epic-completion/status posts (and does the dual-channel + `comms log`
   no-join fallback survive as-is)?
4. **Crew-start collision:** restore the live no-auto-retry guardrail, or ratify the draft's
   autonomy lift (self-serve rename + relaunch) with a mandatory diagnose-first step?
5. **kynes-q paused state:** is kynes-q still deliberately paused post-flagship? If yes it
   needs a one-line expectation somewhere durable, or ops-monitor's paused-queue flag will
   read as a finding every tick.
6. **Orphaned `git stash`** (settings.json drift, stilgar named): confirm stilgar reconciles
   it, and when (needs a daemon-quiet window).
7. **Watch durability:** approve a lanes.json row (or equivalent durable line) stating an
   always-on watch session must exist — the current record LAPSES 2026-07-13, recreating the
   conditions of the 43h unnoticed outage.
8. **Stage-0 sign-offs the operating.md draft header already requests:** the two-watcher set
   (epic_completed + IMMEDIATE only; run_failed arrives via crews/watch) and
   escalation-to-admiral as the default — confirm both.
9. **HARD-RULE tag set:** the condensation keeps 5 of 12 tags. Confirm the demotion list, or
   name any rule that must keep its inviolable marking (minimum fix regardless: three
   exceptions + default-dispatcher, to match AGENTS.md's wording).

---

## 5. Deliberately NOT touched (and why)

- **Every live file.** All output is under `plans/2026-07-11-captain-startup-revamp/drafts/`;
  zero live docs, manifests, skills, or context files were modified. The running captain + 5
  crews are unaffected until Step 1.
- **Companion docs named-but-not-drafted** (AGENT_INDEX.md, STATUS.md gutting, HANDOFF.md,
  AGENTS.template.md, project.yaml, harmonik-dispatch additions, SHUTDOWN.md, direction-log
  compaction, admiral-initiatives trim, missions/kynes.md purge, context/CLAUDE.md lines,
  HANDOFF-captain.md redirect) — scoped to the drafting stage's eight targets; they are
  cutover work (Step 0.3), and several depend on §4 answers.
- **Daemon/renderer code** — the brief renderer gaps (retrieved refs not printed, frontmatter
  desc bug, handoff CLAIM framing), `harmonik digest` parity, and the reviewer-constraint
  unification are daemon-binary work: normal queue beads + pre-deploy e2e gate +
  `docs/daemon-redeploy.md`, never hand-edits. Docs were written to NOT depend on them
  (operating.md carries explicit paths as the interim workaround).
- **Admiral / watch / assessor operating docs and manifests** — verified to exist and were
  read for consistency, but the revamp scope was the captain+crew boot path (where the rot
  and the operator's symptom live). Their docs get the same treatment as a follow-up if
  wanted.
- **The keeper skill and keeper band values** — verified as the live home for the band
  (200k/215k + no-WIDEN); no retune, per the standing no-band-retune rule.
- **The bead (implementer/reviewer) boot path** — `agent-task.md` + seed is already
  manifest-free and correct; the draft AGENTS.md only states it.
- **`scripts/captain-boot-digest.sh`** — NOT deleted or deprecated in any draft; retirement
  is gated on digest parity (Step 4.2).
- **Memory files** — the `feedback_captain_lean_while_operator_away` conflict is handled by
  an in-doc override marker (§2.7), not by editing memory.
- **kerf docs, the Beads-workflow / UBS blocks in AGENTS.md, `specs/`** — carried verbatim
  (only strengthened: SOLO-OPERATOR-ONLY annotations); out of scope for boot-model work.
- **Locked decisions** — none reopened; the STATUS.md anchor for the ten locked decisions is
  explicitly preserved through the gutting (Step 0.3 requirement).
