export const meta = {
  name: 'captain-startup-revamp-2',
  description: 'Stage-2: reframe captain/crew docs to PRINCIPLES-not-rules, bake operator decisions, apply verifier fixes, draft missing companion docs, re-verify (drafts only)',
  phases: [
    { title: 'Reframe', detail: 'lead the operating docs with principles; bake decisions; apply [A] fixes', model: 'fable' },
    { title: 'Companions', detail: 'draft the ~12 missing companion docs', model: 'fable' },
    { title: 'Verify', detail: 'adversarial: principles achieved, decisions honored, nothing load-bearing dropped', model: 'fable' },
    { title: 'Consolidate', detail: 'refresh the cutover doc with the new state', model: 'fable' },
  ],
}

const PLAN = 'plans/2026-07-11-captain-startup-revamp'

const PREAMBLE = [
  'CONTEXT — you have FRESH eyes, no prior conversation. The harmonik project runs a fleet of',
  'long-lived LLM sessions (ADMIRAL oversight, CAPTAIN fleet-orchestrator, CREWS one-per-epic, plus',
  'daemon-dispatched implementer/reviewer beads). A Stage-1 revamp already produced draft docs under',
  PLAN + '/drafts/ that reconcile all boot docs to the MANIFEST-BOOT model (the single boot command is',
  '"harmonik agent brief", flags --wake fresh|keeper-restart|trigger:<id>; its output IS the complete',
  'boot context; the old STARTUP.md + reading-order ritual dies).',
  '',
  'GOVERNING FRAME FOR THIS STAGE — READ ' + PLAN + '/03-operator-decisions.md FIRST AND OBEY IT.',
  'The operator\'s #1 steer: **PRINCIPLES, NOT RULES.** The operating docs must give agents PRINCIPLES',
  'they reason from — not a checklist they obey. The Stage-1 drafts are still condensed rule-piles;',
  'your job is to RE-APPROACH them so each doc LEADS with the principle and demotes specific rules to',
  'illustrations/guardrails underneath. Key applied principles from the decisions file:',
  ' - ESCALATION IS JUDGMENT, not a category filter: agents decide and verify their own work, and',
  '   raise to a human only what a reasonable operator would genuinely want a say in (stakes +',
  '   reversibility, judged each time). Keep the "verify via a few independent agents then act"',
  '   consensus model; do NOT switch to blocking escalate-first. STOP over-raising trivia.',
  ' - The captain communicates to the ADMIRAL; the admiral surfaces pending decisions to the operator',
  '   WHEN the operator is present (decisions must not just sit unraised).',
  ' - FAIL FAST AND LOUD (e.g. crew-start name collision => stop loudly, never auto-rename/double-staff).',
  ' - ANTI-IDLE stays strong: agents take initiative and GO; an idle slot with ready work is a defect.',
  ' - Boot listens for only TWO event kinds (epic-completed + urgent), not per-task chatter.',
  ' - HARD-tag split per the decisions file (stream-not-waves + scratch-lane stay inviolable;',
  '   throwaway-canary becomes an explained recommendation; friction keeps its dogfooding behavior).',
  'CWD is the repo root (/Users/gb/github/harmonik). Read live docs + the Stage-1 drafts +',
  PLAN + '/00-SYNTHESIS.md + ' + PLAN + '/02-cutover-and-open-questions.md as needed.',
].join('\n')

// Existing Stage-1 drafts to RE-APPROACH (principles) + apply their 02-flagged [A] fixes.
const REFRAME = [
  { key: 'captain-operating', draft: 'drafts/agents/captain/operating.md', live: '.harmonik/agents/captain/operating.md',
    note: 'Captain manifest operating section. LEAD WITH PRINCIPLES. Bake: escalation-as-judgment, two-event boot, captain->admiral, LEAN keeper-restart resume, single "harmonik digest" ground-truth pass. Apply 02 section 2.1 [A] fixes: one-line bead write-discipline rule (daemon owns terminal transitions) + list ALL FOUR retrieved refs by path; restore the stranded-in_progress remediation verb (br update <id> --status open); cite M3/hk-039z instead of the confabulated "operator-locked 2026-06-11"; add the ops-monitor-owns-crew-liveness backstop clause.' },
  { key: 'captain-skill', draft: 'drafts/captain/SKILL.md', live: '.claude/skills/captain/SKILL.md',
    note: 'Captain mechanics. Principle-led. Apply 02 section 2.3 [A] fixes: RESTORE crew-start no-auto-retry (fail loud, do not double-staff); daemon-down escalation goes to the OPERATOR (comms is a daemon RPC, admiral is unreachable when daemon down); split the escalation-target list so it matches the manifest (operator for locked-decision reversal + destructive ops; admiral for new-initiative ranking); restore the dual-surface status convention + the read-progress surfaces (comms log --from <crew>, br comments) incl. the "comms log does not advance the recv cursor" nuance; add the light-orchestrator concurrency guard; keep one line of WHY on the stable-session-id launcher requirement.' },
  { key: 'crew', draft: 'drafts/crew-launch/SKILL.md', live: '.claude/skills/crew-launch/SKILL.md',
    note: 'Crew boot reference AND write the re-homed Bounds into drafts/agents/crew/operating.md. Principle-led. Apply 02 section 2.6 re-homes so no crew guardrail vanishes: false-drain guard (empty br ready != drained), no spin-poll br ready more than every 10 min, do NOT self-unblock beads, MUST NOT spawn Agent-tool sub-agents for epic work (use the daemon queue); restore the "br comments add TEXT is positional, there is NO --body flag" landmine and the "--heartbeat 60s" subscribe note; name the wake trigger id (harmonik agent brief --wake trigger:queue); ensure $STATUS_TARGET resolution has a home; flip the presence injected->retrieved story consistently.' },
  { key: 'orchestrator-rules', draft: 'drafts/orchestrator-rules/SKILL.md', live: '.claude/skills/orchestrator-rules/SKILL.md',
    note: 'THE standing-principles doc — principles-not-rules matters MOST here. Rewrite so each section states the PRINCIPLE first, with the specific rule as an illustration/guardrail beneath. Apply the HARD-tag split from 03-operator-decisions (keep inviolable: queue-is-default, the 3 exceptions, daemon-owns-terminal-transitions, review-every-batch, no-cd-worktree, pre-deploy-e2e-gate, major-issue-fanout, STREAM-NOT-WAVES, scratch-lane; demote to plain rule: submit-as-one-group, run_stale!=wedge; recast throwaway-canary as an explained recommendation; keep friction behavior). Apply 02 section 2.7 fixes: give friction-P1 + Monitor-pattern real homes (restore the full content here if the destination skill lacks it), restore the STREAM-NOT-WAVES two-step per-completion procedure, restore the pre-deploy-gate closing anti-loophole sentence + the do-not-amend-global-memory marker, fix the dangling section pointers.' },
  { key: 'captain-lanes', draft: 'drafts/context/captain-lanes.md', live: '.harmonik/context/captain-lanes.md',
    note: 'Tier-2 state TEMPLATE (one current-truth block, <=60 lines, replace-in-place, history banned). Apply 02 section 2.5 [A] fixes: add COMMIT-TIER-2-IMMEDIATELY to the CONTRACT block; rehome the git-add-SPECIFIC-PATH-only rule (never git add -A) into orchestrator-rules or operating.md; restore the .claire/worktrees placeholder-purge half of the fail-closed hooks gate; carry the hook-mitigation recovery pointers; give the always-on-watch existence a durable home BUT per operator decision Q7 this is DEFERRED to research — instead add a one-line note that watch/watchdog/flywheel utility is under investigation (do NOT cement a "watch must always exist" rule); stamp expires:+owner on any dated item; sync the example content to current ground truth (pi flagship CLOSED, kynes on follow-ups; add dehardcode deprioritized).' },
  { key: 'sync', draft: 'drafts/agents/_skills/SYNC.md', live: 'NEW',
    note: 'Sync-script design note. Apply 02 section 2.8 fixes: R7 regex must flag ANY STARTUP.md mention in the routers (not just "STARTUP.md FIRST"); add a mirror-set completeness check derived from .harmonik/agents/*/manifest.yaml at runtime; replace the --apply mtime heuristic with a pure-git blob comparison; fix the stale snapshot count and the manifest.go citation.' },
]

// Missing companion docs (clustered). Draft into drafts/<path>.
const COMPANIONS = [
  { key: 'status-index-handoff',
    note: 'Draft the top-level entry companions that must land WITH the AGENTS.md router rewrite. Write: drafts/STATUS.md (slim it toward phase + locked-decisions but PRESERVE the "#decisions-locked-in-2026-04-19" anchor and the locked-decision content), drafts/AGENT_INDEX.md (keep it a load-on-demand map, drop any boot-ritual framing), drafts/HANDOFF.md (a lean this-session template consistent with the manifest brief carrying handoff), and drafts/AGENTS.template.md (the cmd/harmonik/assets/templates version mirroring the amended AGENTS.md). Read the live versions first.' },
  { key: 'project-yaml-context',
    note: 'Write drafts/context/project.yaml (add the friction-P1 guardrail that orchestrator-rules now delegates here, plus phase/beta status; FIX its header line that still says "Captain reads on every boot (STARTUP.md Step 0a)") and drafts/context/CLAUDE.md (update the retention/boot-order lines so it no longer teaches the old reading order and no longer claims captain-lanes holds the operator priority order). Read the live versions first.' },
  { key: 'dispatch-schema',
    note: 'Write drafts/harmonik-dispatch/SKILL.md additions (the Monitor pattern detail orchestrator-rules now points here for: filter-by-event-TYPE, run_completed-keyed-by-run_id rationale, the tail -F events.jsonl fallback, re-arm-on-timeout; AND the STREAM-NOT-WAVES two-step) and drafts/specs/crew-handoff-schema.md (the "## Current State" field contract: queue_id, in_flight, monitor, next_action, blockers, translations + "absent section => re-derive"). Read the live harmonik-dispatch skill + any existing crew-handoff spec first.' },
  { key: 'tier2-state-cleanup',
    note: 'Draft the tier-2 state cleanups: drafts/context/direction-log.md (compacted to the cap, newest-first, expired entries struck), drafts/crew/admiral-initiatives.md (trimmed to the current big-rocks snapshot, stale 06-25 rows removed), drafts/HANDOFF-captain.md (a tombstone/redirect so no stray reader treats it as a competing tier-1 — the manifest brief carries the handoff), and a short drafts/SHUTDOWN.md note confirming the end-of-session tier-2 discipline (update captain-lanes + direction-log BEFORE the handoff) and re-pointing away from STARTUP.md. Read the live versions first.' },
]

const VERDICT = {
  type: 'object',
  properties: {
    doc: { type: 'string' },
    principles_not_rules: { type: 'string', enum: ['yes', 'partial', 'no'], description: 'does the doc now LEAD with principles rather than read as a rule-checklist' },
    decisions_honored: { type: 'string', enum: ['yes', 'partial', 'no'] },
    dropped_load_bearing: { type: 'array', items: { type: 'string' } },
    remaining_issues: { type: 'array', items: { type: 'string' } },
    verdict: { type: 'string', enum: ['ship', 'fix-first'] },
  },
  required: ['doc', 'principles_not_rules', 'decisions_honored', 'dropped_load_bearing', 'verdict'],
}

// ---- Phase 1: reframe + fix the existing drafts (parallel) ----
phase('Reframe')
const reframed = (await parallel(REFRAME.map((t) => () =>
  agent(PREAMBLE + '\n\nRE-APPROACH this doc so it LEADS WITH PRINCIPLES, then apply its fixes.\n' +
    'Live: ' + t.live + '\nDraft to overwrite (in place): ' + PLAN + '/' + t.draft + '\n' +
    'What to do: ' + t.note + '\n\n' +
    'Read the live doc, the current Stage-1 draft, 03-operator-decisions.md, and 00-SYNTHESIS.md. Then ' +
    'rewrite the draft: principle-first structure, decisions baked in, every flagged [A] fix applied, ' +
    'every load-bearing rule preserved (as a guardrail under its principle), cruft/duplication cut, ' +
    'aligned to the manifest-boot model. Keep the DRAFT banner HTML comment at the very top. Return a ' +
    'one-paragraph changelog: how you re-framed to principles, what you fixed, what you cut, any risk.',
    { label: 'reframe:' + t.key, phase: 'Reframe', model: 'fable', effort: 'high' })
))).filter(Boolean)
log('Reframe: ' + reframed.length + '/' + REFRAME.length + ' drafts revised')

// ---- Phase 2: draft missing companions (parallel) ----
phase('Companions')
const companions = (await parallel(COMPANIONS.map((c) => () =>
  agent(PREAMBLE + '\n\nDRAFT missing companion docs into ' + PLAN + '/drafts/. Do NOT touch live files.\n' +
    'Task: ' + c.note + '\n\n' +
    'Read the live versions first, plus 00-SYNTHESIS.md (target architecture) and 03-operator-decisions.md. ' +
    'Write each draft with a top DRAFT-banner HTML comment naming the live doc it revises. Preserve every ' +
    'load-bearing rule/anchor; align to the manifest-boot model and the principles frame. Return a ' +
    'one-paragraph changelog listing the files you wrote and the key choices.',
    { label: 'companion:' + c.key, phase: 'Companions', model: 'fable', effort: 'high' })
))).filter(Boolean)
log('Companions: ' + companions.length + '/' + COMPANIONS.length + ' clusters drafted')

// ---- Phase 3: adversarial verify the reframed drafts ----
phase('Verify')
const verdicts = (await parallel(REFRAME.map((t) => () =>
  agent(PREAMBLE + '\n\nADVERSARIALLY verify a reframed draft — default to skepticism.\n' +
    'Live: ' + t.live + '\nDraft: ' + PLAN + '/' + t.draft + '\n\n' +
    'Read BOTH + 03-operator-decisions.md. Judge: (1) does the draft now LEAD WITH PRINCIPLES rather than ' +
    'read as a rule-checklist; (2) are the operator decisions honored (escalation-as-judgment not a ' +
    'category filter, captain->admiral, fail-loud collisions, anti-idle, two-event boot, the HARD-tag ' +
    'split); (3) did it DROP any load-bearing rule/guardrail/safety gate present in the live doc. Return ' +
    'the verdict object.',
    { label: 'verify:' + t.key, phase: 'Verify', model: 'fable', effort: 'high', schema: VERDICT })
))).filter(Boolean)
const fixFirst = verdicts.filter((v) => v && v.verdict === 'fix-first')
log('Verify: ' + verdicts.length + ' checked, ' + fixFirst.length + ' still fix-first')

// ---- Phase 4: consolidate into an updated cutover status ----
phase('Consolidate')
await agent(PREAMBLE + '\n\nWrite ' + PLAN + '/04-stage2-status.md summarizing this Stage-2 pass. Include: ' +
  '(1) which drafts were reframed-to-principles + fixed and which companions were drafted (paths); ' +
  '(2) the verify results — principles_not_rules + decisions_honored + any dropped load-bearing per doc, ' +
  'and which are ship vs fix-first; (3) what still remains before cutover (open items, the code beads ' +
  'from 02 which are OUT of scope here, the deferred watch/watchdog/flywheel research); (4) a short ' +
  'READY-FOR-CUTOVER assessment pointing back at the 02 staged checklist.\n\n' +
  'VERIFY RESULTS (JSON):\n' + JSON.stringify(verdicts) + '\n\n' +
  'REFRAME CHANGELOGS:\n' + JSON.stringify(reframed) + '\n\n' +
  'COMPANION CHANGELOGS:\n' + JSON.stringify(companions),
  { label: 'consolidate', phase: 'Consolidate', model: 'fable', effort: 'high' })

return {
  plan_dir: PLAN,
  reframed: reframed.length,
  companions: companions.length,
  verified: verdicts.length,
  fix_first: fixFirst.map((v) => v.doc),
}