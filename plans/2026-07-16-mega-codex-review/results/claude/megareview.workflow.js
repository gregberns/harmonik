export const meta = {
  name: 'mega-review-claude-lane',
  description: 'Claude lane of the mega code-review: fan out per review-unit, write findings to disk, condense with multiple agents, synthesize FINDINGS + NITS',
  phases: [
    { title: 'Review', detail: 'one agent per review-unit, writes raw/<RU>.json' },
    { title: 'Condense', detail: 'multi-lens cross-read + completeness critic' },
    { title: 'Synthesize', detail: 'FINDINGS.md (major-first) + NITS.md' },
  ],
}

const ROOT = '/Users/gb/github/harmonik'
const RAW = `${ROOT}/plans/2026-07-16-mega-codex-review/results/claude/raw`
const COND = `${ROOT}/plans/2026-07-16-mega-codex-review/results/claude/condensed`
const SCHEMA = `${ROOT}/plans/2026-07-16-mega-codex-review/results/claude/SCHEMA.md`

// Review units. scope = concrete files/dirs the agent reads itself. focus = the lens.
const UNITS = [
  { id: 'RU-01a', scope: 'internal/daemon/workloop.go (lines 1-4100 only)', focus: 'god-function; concurrency, goroutine leaks, error swallowing, dead branches, unmaintainable structure' },
  { id: 'RU-01b', scope: 'internal/daemon/workloop.go (lines 4100-end) + runbridge.go, dispatchsegment.go, stategather.go, runshell.go, eagerfill_em063.go (all in internal/daemon/)', focus: 'shell-event->machine-event->br close/reopen mapping; fabricated done-status; runshell fireOnCancel; concurrency' },
  { id: 'RU-02', scope: 'internal/daemon/{dot_cascade.go,reviewloop.go,dot_gate.go,verdictexecutor_rc025a.go,sub_workflow_runner.go}', focus: 'DOT cascade + review/gate loop; state correctness, verdict handling, deadlock/livelock, error paths' },
  { id: 'RU-03', scope: 'internal/runexec/, internal/mergeq/, internal/run/, internal/runexectest/', focus: 'run-state-machine seam (M3); state transitions, merge queue correctness, race conditions' },
  { id: 'RU-04', scope: 'internal/workspace/{remotematerialize.go,createworktree.go,reviewverdict.go,autostatusmarker.go,diffhash.go}, internal/workers/, and grep for runner==nil / runner!=nil dual-path sites across internal/', focus: 'remote/SSH substrate; transport failure modes, dual-path (runner nil) drift, resource lifecycle' },
  { id: 'RU-04b', scope: 'internal/workspace/ EXCLUDING the 5 files in RU-04 — i.e. worktree lifecycle, merge dispatch, conflict resolution+escalation, leaselock.go, wipcapture, interruptstate, crashevidence, gitignorehygiene, discoverworktrees', focus: 'git-concurrency + resource lifecycle + terminal git decisions + cross-process lease lock; data-integrity' },
  { id: 'RU-05a', scope: 'internal/daemon/{tmuxsubstrate.go,pasteinject.go}', focus: 'tmux input channel (old, still live); paste injection correctness, argv/exec, races' },
  { id: 'RU-05b', scope: 'internal/lifecycle/tmux/, internal/keeper/tmuxresolve.go', focus: 'tmux lifecycle adapter; argv/exec correctness, resource leaks, error handling' },
  { id: 'RU-06', scope: 'internal/daemon/{daemon.go,projectconfig.go,socket.go} + internal/daemon/bootconfig/ + internal/daemon/router/ (the post-giant-retirement carve)', focus: 'god-struct + composition root (85-field workLoopDeps); over-abstraction, coupling, architecture rot' },
  { id: 'RU-07', scope: 'internal/codexdriver/, internal/codexinput/, internal/codexreactor/, internal/codexwire/, internal/codexdigitaltwin/, internal/codextest/', focus: 'codex substrate vertical (new M2 code); protocol correctness, wire parsing, reactor state' },
  { id: 'RU-08', scope: 'internal/substrate/, internal/handler/, internal/handlercontract/', focus: 'substrate/Handler contract seam; interface correctness, contract adherence, over-abstraction' },
  { id: 'RU-09', scope: 'internal/queue/', focus: 'queue subsystem; rpc.go two-writer path, HandlerAdapter, lost-update race, concurrency' },
  { id: 'RU-10', scope: 'internal/core/ event registry surface (eventreg, pertypecompat table, eventtype.go)', focus: 'event registry; dead code (pertypecompat), decode/validate with zero live consumers, correctness' },
  { id: 'RU-10b', scope: 'internal/core/ type & payload definitions (SKIM — classify, not line-audit)', focus: 'type/payload defs; classify sloppiness, dead types, drift — do not exhaustively line-audit 26k LOC' },
  { id: 'RU-11', scope: 'internal/eventbus/ (busimpl.go, jsonlwriter.go)', focus: 'event bus; delivery guarantees, jsonl writer durability/races, error handling' },
  { id: 'RU-12', scope: 'internal/lifecycle/ startup/orphansweep/stalewatch/draindetect/reconcile (EXCLUDING the reconcile-close seam in RU-12x)', focus: 'lifecycle sweeps + reconcile; sweep correctness, stale detection, races' },
  { id: 'RU-12x', scope: 'internal/lifecycle reconcile-close path (the noChange-subsumption close) + internal/brcli/{terminaltransition_bi010.go,intentlogwrite.go}', focus: 'fabricated-done-status close seam; can a close path fabricate done-status? data-integrity' },
  { id: 'RU-13', scope: 'internal/keeper/ (watcher.go, step.go, cycle.go) + internal/keepertwin/ (NOT twinparity)', focus: 'keeper watcher/step/cycle; restart timing, context-fill logic, races' },
  { id: 'RU-14', scope: 'internal/hook/, internal/hooksystem/, internal/hookrelay/, internal/policy/, internal/orchestrator/', focus: 'hook system (M5 seams); correctness read (coverage already strong — do NOT ask for more tests, DO find bugs)' },
  { id: 'RU-15', scope: 'internal/workflow/, internal/workflowvalidator/, internal/goalstate/', focus: 'workflow graph engine + DOT; OVER-ABSTRACTION is the prime suspect, plus validator correctness' },
  { id: 'RU-16a', scope: 'cmd/harmonik/{comms.go,main.go,run.go,run_via_daemon.go,harness.go,subscribe.go,handler.go,graph.go,state_cmd.go,substrate_select.go,smoke.go,decisions.go,decisions_k4.go,confirm_verdict.go,veto_verdict.go,write_review_verdict_cmd.go,greenlight_cmd.go,goalkeeper_cmd.go}', focus: 'CLI comms/core + gate/verdict control surface; correctness, sloppy CLI plumbing' },
  { id: 'RU-16b', scope: 'cmd/harmonik/ keeper/init/asset/crew/captain/start/digest/schedule/sleepwake/sentinel/ops_monitor/dashboard/reconcile/migrate/branch_reap/release/usage/watcherreap/project_hash verbs + eval_cmd.go,eval_metrics_cmd.go,eval_report_cmd.go,eval_guardrails_lygpp.go', focus: 'CLI lifecycle + eval-harness verbs; correctness, drift, sloppy plumbing' },
  { id: 'RU-17', scope: 'cmd/harmonik/{supervise_cmd.go,workers_boot.go,promote_cmd.go,remote_control_prefix_cmd.go}', focus: 'supervise + daemon lifecycle CLI; process management, revival correctness' },
  { id: 'RU-18', scope: 'internal/brcli/ (EXCLUDING terminaltransition_bi010.go/intentlogwrite.go which are in RU-12x)', focus: 'beads adapter; subprocess handling, parsing, error paths' },
  { id: 'RU-19', scope: 'internal/keepertest/, internal/codexdigitaltwin/ twin harnesses (SKIM — classify)', focus: 'twin harnesses; classify test-theater vs real, do not line-audit' },
  { id: 'RU-20', scope: 'internal/operatornfr/ (CLASSIFY keep-vs-delete, not line-audit)', focus: 'test theater; is the green real or fake? classify each file keep/delete' },
  { id: 'RU-21', scope: 'internal/specaudit/ (CLASSIFY; check the 3 untagged stragglers vs //go:build specaudit)', focus: 'test theater classify; find untagged stragglers, is it real?' },
  { id: 'RU-22', scope: 'internal/scenario/ — FIRST run: go list / grep what test-scenario and check-full pull in (Makefile/scripts), then classify prune-vs-keep. DO NOT recommend deleting anything the live gate compiles', focus: 'scenario harness; real vs structural-corpus theater, gated prune list' },
  { id: 'RU-23', scope: 'internal/{agentmanifest,apptap,branching,cognition,crew,dashboard,digest,presence,release,replay,schedule,sentinel,sessiondata,structuredlog,usage,watch,testhelpers,t5probe,t6probe,scratchpad,sessioncapture}/', focus: 'supporting grab-bag; scan for correctness bugs and sloppy/unmaintainable code across these packages' },
  { id: 'RU-24', scope: 'cmd/harmonik/assets/ shell scripts (*.sh) + *.tmpl templates ONLY (skip the 15 skill .md files — deferred)', focus: 'executable scaffold assets; a script bug is a real runtime bug — quoting, error handling, idempotency' },
  { id: 'RU-25', scope: 'specs/ — walk every normative MUST/SHALL, then grep internal/ + cmd/ to confirm a live implementation exists', focus: 'spec-vs-code drift; find orphaned specs (spec with no impl) and normative clauses never built' },
]

const reviewPrompt = (u) => `You are a senior Go reviewer on the harmonik mega code-review (Claude correctness+quality lane).

REVIEW UNIT: ${u.id}
SCOPE (read these files YOURSELF from ${ROOT}): ${u.scope}
FOCUS LENS: ${u.focus}

Read the finding schema at ${SCHEMA} and follow it EXACTLY.

Your job: uncover issues — do NOT fix anything, do NOT edit any file. Hunt for:
- correctness bugs (nil-deref, error swallowing, wrong logic, races, goroutine/resource leaks, deadlocks)
- architecture rot: over-abstraction, god-functions/structs, tight coupling, unmaintainable/sloppy/garbage code
- spec drift, dead code, test-theater / fake-anchored coverage
Include bad code EVEN IF it is unchanged vs main. Wide scope. Be specific: cite file:line, describe the concrete failure scenario.
Put pure style/naming/preference issues at severity "nit" (they will be filed separately, not dropped).

If you cannot read the scope (missing dir, etc.), write status="failed" with a note — never emit empty findings as if you reviewed. A file-absent/empty review is NOT a clean review.

WRITE your result as a single JSON file to: ${RAW}/${u.id}.json (schema in SCHEMA.md). Create it with the Write tool.
Then return ONLY: "${u.id}: <N> findings (crit=<c> high=<h>), status=<reviewed|partial|failed>". Nothing else.`

phase('Review')
const reviewResults = await parallel(UNITS.map((u) => () =>
  agent(reviewPrompt(u), { label: `review:${u.id}`, phase: 'Review' })
))
const manifest = UNITS.map((u, i) => `${u.id}: ${reviewResults[i] || 'AGENT-DIED'}`).join('\n')
log('Review phase done:\n' + manifest)

// Condense: three lenses cross-read ALL raw findings + a completeness critic. Each writes to disk.
phase('Condense')
const LENSES = [
  { key: 'correctness', prompt: 'correctness bugs, concurrency/resource hazards, data-integrity — the ones that can actually break in production' },
  { key: 'architecture', prompt: 'architecture rot, over-abstraction, god-functions/structs, coupling, unmaintainable/sloppy/garbage code, maintainability debt' },
  { key: 'coverage', prompt: 'test-theater, fake-anchored coverage, coverage gaps, dead code, spec drift' },
]
const condensePrompt = (l) => `You are condensing the raw findings of the harmonik mega code-review.

Read EVERY JSON file in ${RAW}/ (each is one review unit's findings). Also note which units have status="failed" or "partial" — those are gaps that must be surfaced, not hidden.

Your lens: ${l.prompt}

Produce a condensed, DEDUPED, ranked list for YOUR lens only. Group duplicates, never drop a distinct issue. For each: severity, file:line, one-line title, 1-2 sentence why-it-matters + failure scenario, and which RU it came from. Rank most-severe first. Separate anything that is really just a nit into a "Nits" section at the bottom.
Also list any RU that reported status failed/partial (coverage gap).

Do NOT edit any source file. WRITE your output as markdown to ${COND}/${l.key}.md with the Write tool. Return only: "${l.key}: <N> real issues, <M> nits, <K> failed/partial units".`

const criticPrompt = `You are the COMPLETENESS CRITIC for the harmonik mega code-review.
Read every raw file in ${RAW}/ AND the three condensed files in ${COND}/ (correctness.md, architecture.md, coverage.md).
Find what the condensers DROPPED or under-ranked: a real finding in a raw file that no condensed file carried, any RU with status failed/partial not surfaced, any high/critical buried as low. Also flag whole subsystems that got thin/suspicious coverage (very few findings from a large scope may mean a shallow review).
Do NOT edit source. WRITE ${COND}/critic.md with a "Recovered/under-ranked findings" list and a "Coverage-confidence" note per RU. Return only: "critic: <N> recovered, <G> thin-coverage units".`

await parallel([
  ...LENSES.map((l) => () => agent(condensePrompt(l), { label: `condense:${l.key}`, phase: 'Condense' })),
])
// critic runs after the three condensed files exist
await agent(criticPrompt, { label: 'condense:critic', phase: 'Condense' })

// Synthesize: one agent merges condensed + critic into the two deliverables.
phase('Synthesize')
const synthPrompt = `You are writing the two final deliverables of the harmonik mega code-review (Claude lane).
Read ${COND}/correctness.md, ${COND}/architecture.md, ${COND}/coverage.md, ${COND}/critic.md.

Write TWO files (Write tool, do NOT edit source):
1. ${COND}/../FINDINGS.md — the real issues, ranked. Structure: (a) Executive summary (5-8 bullets: the worst problems + overall code-health read), (b) Critical/High findings grouped by subsystem, each with file:line + why-it-matters + failure scenario + fix-direction, (c) Medium/Low findings by subsystem, (d) Coverage-confidence table: per-RU whether the review was deep/thin/failed. Fold in the critic's recovered findings. Major/architectural problems FIRST.
2. ${COND}/../NITS.md — all nit-severity items, grouped by file. Kept, not dropped, but out of the main report.

These are for the operator + will seed beads. Be concrete and honest — thin coverage must be labeled thin, not passed off as clean. Return only a 3-line summary: total critical, total high, total nits, and the single worst problem found.`
const summary = await agent(synthPrompt, { label: 'synthesize', phase: 'Synthesize' })

log('DONE. ' + summary)
return { manifest, summary }
