# I2 — Captain Instruction-Set Conflicts & Contradictions

Audit of the harmonik CAPTAIN's instruction surface for conflicts an LLM captain
would actually hit. Sources audited:

- `.claude/skills/captain/STARTUP.md` (boot runbook)
- `.claude/skills/captain/SKILL.md` (operating context)
- `.claude/skills/captain/SHUTDOWN.md` (wind-down runbook)
- `docs/orchestrator-rules.md` (permanent orchestrator directives)
- `CLAUDE.md` / `AGENTS.md` (project instructions)
- `~/.claude/CLAUDE.md` (cross-project working style)
- composed skills: `agent-comms`, `beads-cli`, `harmonik-dispatch`, `keeper`
- captain-relevant memory files under
  `~/.claude/projects/-Users-gb-github-harmonik/memory/`

Each item: the tension in one sentence, both citations, and the resolution. Items
marked **RESOLVED-BY-SUPERSEDES** are not real defects (a SUPERSEDES note already
reconciles them) but are listed because the captain reading the docs linearly will
still hit the contradiction before reaching the note — i.e. they are *latent*
confusions worth flattening.

---

## CATEGORY 1 — DIRECT CONTRADICTIONS

### Conflict 1 — Keeper band values: THREE different numbers across live docs (HIGH)

Three captain-facing docs cite three different warn/act thresholds for arming the
captain keeper, and none matches the operator's current directive.

- `STARTUP.md:335-340` (Step 6 keeper arming) and `:329-330` → `--warn-pct 30 --act-pct 35`.
- `SKILL.md:222` (§A lane snapshot) → "relaunch the watcher with `--warn-pct 30 --act-pct 35`".
- `keeper/SKILL.md:412,436` (§KNOWN DRIFT + Quick reference) → `--warn-pct 25 --act-pct 30`.
- `reference_captain_keeper_restart_gap.md:10` → the live launch used `--warn-pct 25 --act-pct 30`.
- **Operator's CURRENT directive** (`feedback_keeper_band_no_retune.md:16`, 2026-06-17):
  restart EARLIER, **warn=200K / act=215K absolute tokens** (≈20% of 1M).

**Tension:** A captain hand-relaunching its keeper has four candidate values and
will pick the wrong one; worse, all the pct flags are **inert on a 1M window** —
`keeper/SKILL.md:96-99,238-239` states `--warn-pct`/`--act-pct` "have no effect and
the keeper will emit a warning if they are passed explicitly." So every doc that
says "relaunch with `--warn-pct 30 --act-pct 35`" is instructing the captain to pass
flags that DO NOTHING and trigger a warning, while the real lever
(`--warn-abs-tokens`/`--act-abs-tokens` or the config.yaml `keeper:` block) goes
unmentioned in STARTUP/SKILL.

**Resolution:** The operator directive wins: warn≈200K / act≈215K **absolute
tokens**. Replace every `--warn-pct N --act-pct N` instruction in STARTUP.md,
SKILL.md §A, and keeper/SKILL.md with `--warn-abs-tokens 200000 --act-abs-tokens
215000` (or point at the config.yaml `keeper:` block as the single source). Delete
the pct flags from captain arming entirely — they are inert and misleading on the
1M window the captain runs on.

### Conflict 2 — "fill every non-conflicting lane" vs "keep the fleet LEAN while operator away" (HIGH)

The single most damaging behavioral contradiction.

- **Fill-every-lane (HARD, repeated ~8×):** `SKILL.md:48,79,108-110` ("Fill every
  non-conflicting free slot. Keep the fleet moving; do NOT park it 'in case.'");
  `SKILL.md:148-150` (§0.2 — "Holding/idling while ready work exists in the feed" is
  a HARD anti-pattern); `STARTUP.md:236,398,408-414` (a tick that finds ready work +
  a free slot and does not staff it is a "FAILED tick / MISSED STAFFING FAILURE").
- **Lean-while-away (operator-directed):** `feedback_captain_lean_while_operator_away.md`
  — "when operator AWAY and only low-pri work remains, keep the fleet LEAN, do NOT
  auto-expand crews… the generic §0 fill-every-lane rule YIELDS to the hk-rl4b
  token-burn directive." Confirmed live in `captain-lanes.md:6,12` ("LEAN park,
  operator away" — 0 crews, logmine stood down to honor token-burn).

**Tension:** SKILL.md/STARTUP.md make idling-with-ready-work a defined FAILURE; the
operator's token-burn directive makes auto-expanding the fleet while away a failure.
A captain that boots to a quiet bus with P2/P3 ready work has two HARD rules firing
in opposite directions and **nothing in STARTUP.md or SKILL.md mentions the lean
exception** — it lives only in memory.

**Resolution:** The lean directive is the more-specific, operator-directed override
and wins *while operator is away with only low-pri work*. This MUST be written into
SKILL.md §0 and the STARTUP.md "FAILED-tick definition" as an explicit carve-out:
"While the operator is away and the only unstaffed ready work is P2/P3, keeping the
fleet lean is correct — do NOT auto-expand; surface a scale-up recommendation for
operator return. A genuinely P0/P1 clean ready bead still gets staffed." Without
this, the captain either burns tokens (obeys fill-every-lane) or looks broken
(obeys lean but every health tick self-reports FAILED).

### Conflict 3 — On-WARN: "keep working / restart-now" vs "NEVER self-terminate" framing collision (MEDIUM)

- `STARTUP.md:345`, `SKILL.md:668`, `SHUTDOWN.md:378`, `keeper/SKILL.md:353` all
  quote the captain warn text: *"…you have ample buffer remaining. Keep working. At
  a clean checkpoint only: …run: harmonik keeper restart-now…"*
- But `reference_captain_keeper_restart_gap.md:12` documents that the SHARED warn
  injector text historically ended "…then run `/quit`" (`injector.go:14-16`), and
  the captain obeying `/quit` is exactly the dead-captain bug. The docs now
  repeatedly say "NEVER exit or terminate your own session on a warn"
  (`STARTUP.md:369`, `SKILL.md:690`, `SHUTDOWN.md:377`).

**Tension:** The captain-specific warn text and the "never self-quit" rule are
consistent with each other, but they depend on the keeper actually injecting the
*captain-specific* text rather than the *shared* `/quit` text. `keeper/SKILL.md`
config block shows `on_demand_warn_text` defaults to `""` = compiled default —
whether the deployed compiled default is the safe captain text or the fatal `/quit`
text is unverifiable from the docs.

**Resolution:** Not a doc-vs-doc contradiction per se, but a latent footgun: add to
STARTUP.md Step 6 a one-line verify ("confirm the captain keeper injects the
captain-specific restart-now text, NOT the shared `/quit` advisory — run `keeper
doctor` / inspect `on_demand_warn_text`"). The "never self-quit" rule must be read
as overriding ANY injected `/quit` instruction.

### Conflict 4 — restart-now band "UNCHANGED" vs operator directive to LOWER the band (MEDIUM, RESOLVED-BY-SUPERSEDES)

- `STARTUP.md:348-349`, `SKILL.md:725-727`, `SHUTDOWN.md:384-385`,
  `keeper/SKILL.md:148-151,336-338` all repeat: "**The keeper band is UNCHANGED.**
  The operator HARD-NO on widening the band stands."
- `feedback_keeper_band_no_retune.md:16` (2026-06-17 UPDATE): operator EXPLICITLY
  DIRECTED LOWERING the band (300K/350K → 200K/215K) to restart earlier; "a future
  captain must NOT re-apply the old 'no band-retune' lock to refuse this LOWERING."

**Tension:** A captain reads "band is UNCHANGED / HARD-NO on retune" four times and
will refuse the operator's LOWERING directive, mistaking direction (the HARD-NO is
on WIDENING only). The memory note resolves it, but the four skill docs never carry
the directional caveat.

**Resolution:** RESOLVED-BY-SUPERSEDES in memory, but the skills are STALE. Reword
every "band is UNCHANGED" line to "**`restart-now` does not widen the band**;
LOWERING the band to restart earlier is operator-directed and good — the HARD-NO is
on WIDENING only." Ties directly into Conflict 1's stale pct numbers.

---

## CATEGORY 2 — AMBIGUOUS TRIGGERS / TWO RULES BOTH FIRE

### Conflict 5 — "Captain NEVER spawns sub-agents" vs "run a 3-agent consensus before surface-and-await" (HIGH)

- **No-sub-agent rule:** `STARTUP.md:449-456` (Anti-pattern C: "The captain NEVER
  spawns its own implementer Agent sub-agents… do not spin up ≥10 parallel Agent
  sub-agents"); `SKILL.md:582-585` (concurrency guard: "LIGHT orchestrator… do NOT
  spin up ≥10 parallel Agent-tool sub-agents"); `harmonik-dispatch/SKILL.md:88-98`
  (HARD RULE: daemon + ≥10 sub-agents = 56-min rate-limit stall; cap total claude
  sessions ≤5).
- **Consensus-gate rule:** `SKILL.md:113-138` (§0.1 — "Before you stop and ask the
  operator any GENUINELY-NEW-judgment question, you MUST first run a **3-agent
  consensus**… Spawn 3 independent sub-agents in parallel, READ-ONLY").
- **Fan-out:** `major-issue-fanout` (referenced `SKILL.md:135`) fans out **10–15
  agents** + ≥2 verifiers.

**Tension:** The consensus gate is MANDATORY before surface-and-await, but it spawns
3 sub-agents; the fan-out spawns 10–15. The no-sub-agent anti-pattern and the
≤5-total-claude-sessions rate-limit rule both bite at exactly the moment the captain
is also coordinating crews + daemon beads. STARTUP.md Anti-pattern C *does* carve out
"read-only PLANNING/RESEARCH/triage sub-agents," and the consensus agents are
read-only — but the captain has to reconcile "NEVER spawns sub-agents" (stated
absolutely, twice) against "MUST run 3-agent consensus" and the hard ≤5 cap.

**Resolution:** They reconcile but the reconciliation is implicit. Make it explicit:
add to SKILL.md §0.1 and STARTUP.md Anti-pattern C a cross-reference — "the
consensus gate's 3 read-only agents and the fan-out's verifiers are the SANCTIONED
exception to the no-sub-agent rule; count them against the
`harmonik-dispatch` ≤5-concurrent-claude cap (pause queue submits while a fan-out
runs)." Otherwise a careful captain refuses to run the mandatory consensus gate, or
trips the rate-limit stall.

### Conflict 6 — `br close` forbidden (daemon-owned) vs SHUTDOWN bypass-SOP requires `br close` (MEDIUM)

- **Forbidden:** `beads-cli/SKILL.md:28-46,216` ("Agents MUST NOT issue
  terminal-transition `br` writes… MUST NOT call `br close`"); `SKILL.md:546-549`
  ("Permitted `br` writes = comments + the EPIC `--assignee` mirror ONLY… MUST NOT
  issue terminal-transition writes (`br claim`/`br close`/`br reopen`)").
- **Required:** `SHUTDOWN.md:100-101` (bypass-SOP step 8: "`br close <bead_id>
  --reason 'Manually deployed…'`"); `SHUTDOWN.md:408-410` ("`br close` on
  manually-deployed beads is the one exception, sanctioned by the bypass-SOP").
- **Live caution:** `captain-lanes.md:25` warns hk-gu3v/hk-nlio are STRANDED and
  "do NOT raw `br close` — would reverse the locked beads-own-transitions decision."

**Tension:** SHUTDOWN.md sanctions a single `br close` exception for manually
cherry-picked beads; SKILL.md and beads-cli state the prohibition absolutely; and a
live note says raw `br close` reverses a LOCKED decision (a §8 surface-and-await
trigger). A captain doing a lull-deploy must `br close` per SHUTDOWN but is told by
SKILL/memory that doing so is forbidden / a locked-decision reversal.

**Resolution:** The bypass-SOP exception is real but DANGEROUS because promote
cherry-picks lack the `Harmonik-Bead-ID` merge trailer reconcile keys on (memory:
`promote_salvage_strands_bead_inprogress`). Reconcile: SKILL.md §8 should name the
ONE sanctioned exception ("the bypass-SOP `br close` after a *verified* manual
cherry-pick to main, per SHUTDOWN.md Step 2") AND carry the caveat that for promote
cherry-picks lacking the merge trailer, you do NOT raw-close — you let `harmonik
reconcile` close it (or land the hk-53p3 fix). Currently the exception and its
exception-to-the-exception live in three different files.

### Conflict 7 — Stream vs wave concurrency: contradictory defaults for "concurrent dispatch" (MEDIUM)

- `orchestrator-rules.md:22-23` (USE --wave): "`--max-concurrent N` only works
  reliably with `--wave`… Stream-mode enforces head-of-line blocking — only ONE item
  dispatches at a time."
- `harmonik-dispatch/SKILL.md:66-67`: "Use `kind: 'stream'` groups for the daily
  loop… Use `kind: 'wave'` only when you need true concurrent dispatch."
- `reference_captain_dispatch_serial_not_concurrent.md:12`: "Concurrent dispatch of
  REAL beads WEDGES; only SERIAL works… Dispatch captain/daemon beads ONE AT A TIME,
  or `--max-concurrent 1`."

**Tension:** orchestrator-rules + dispatch skill push the captain toward `--wave`
for concurrency, while hard-won operational memory says concurrent REAL-bead dispatch
WEDGES (`launch_stall_detected`) and only serial works. A captain optimizing for
throughput per the skills will reintroduce the wedge.

**Resolution:** This is partly a freshness question (the wedge bead hk-3j50y may be
fixed), but the docs and memory point opposite ways with no cross-reference. SKILL/
dispatch should note: "if concurrent dispatch wedges at `launch_stall_detected`, fall
back to `--max-concurrent 1` (serial) per the known concurrent-dispatch-wedge class;
verify hk-h8u7p / hk-3j50y status before going wide." Crews own dispatch, so this
mostly bites crews — but the captain's lull-deploy and canary probes hit it too.

---

## CATEGORY 3 — STALE INSTRUCTIONS / RETIRED MECHANISMS

### Conflict 8 — Quality-check greps a field that does NOT exist → permanent false all-clear (HIGH)

- `STARTUP.md:296-301` (Step 5d-c) and `STARTUP.md:398` (the `/loop 12m` health-tick
  template, item 7) instruct: grep `run_started` events for `workflow_mode ==
  "single"` to detect review-bypass dispatches.
- `reference_captain_quality_check_broken.md`: "`workflow_mode` does NOT exist on
  `run_started`… the check ALWAYS returns null = a permanent FALSE 'no single-mode'
  all-clear. A genuine review-gate bypass would go UNDETECTED." Correct method =
  match `reviewer_verdict` events by `run_id`. Also: run_started fields nest under
  `.payload`, so even the top-level `.bead_id`/`.workflow_mode` jq is wrong.

**Tension:** A load-bearing safety check (catch silent review-bypass) is wired to a
non-existent field and silently always passes. The captain's STARTUP verification
AND its recurring health tick both contain the broken grep. This is the
"self-defeating safety check" class — worse than a missing check because it reports
GREEN.

**Resolution:** Replace both occurrences (Step 5d-c and the `/loop 12m` item 7) with
the `reviewer_verdict`-per-`run_id` method: for each `run_completed .payload.run_id`,
confirm a matching `reviewer_verdict` exists; a completed run with no verdict ran
review-bypassed. STALE — fix the skill, do not wait for the tracked bead.

### Conflict 9 — `subscribe --types …,run_stale,heartbeat` armed in §0.5 vs STARTUP forbids the heartbeat subscribe (MEDIUM)

- `SKILL.md:184-185` (§0.5 Step 5): "Arm watchers — `comms recv --follow` … and
  `subscribe --types epic_completed,run_failed,run_stale,heartbeat`."
- `STARTUP.md:382-387,396-399`: "**The captain watches HEALTH + LANES + DECISIONS —
  never RUNS.** A prior captain armed `subscribe --types …,run_stale,heartbeat
  --heartbeat 60s`; that … re-invoked the captain every minute, training it to react
  to individual runs and burning the context the captain role exists to protect…
  **Do NOT re-create that. Arm EXACTLY the two watchers below**" — i.e.
  `comms recv --follow` + a `/loop 12m` tick, explicitly NOT a heartbeat subscribe.
- `SKILL.md:426-428` (§5) also says run `harmonik subscribe --types epic_completed`
  "for the life of the session."

**Tension:** SKILL.md §0.5 tells the captain to arm a `run_stale,heartbeat`
subscribe; STARTUP.md (the authoritative boot runbook) explicitly forbids exactly
that subscribe as the documented context-burn failure mode and says arm "EXACTLY two
watchers." §5 adds a third "for the life of the session" `epic_completed` subscribe.

**Resolution:** STARTUP.md wins (it post-dates and names the operator-flagged
failure). Fix SKILL.md §0.5 Step 5 to match: arm `comms recv --follow` + the
`/loop 12m` health tick ONLY; drop `run_stale,heartbeat` from the captain's standing
subscription. Clarify §5's `epic_completed` subscribe is folded into the comms-recv
feed / tick, not a separate always-on heartbeat stream.

### Conflict 10 — keeper/SKILL.md cites `--warn-pct 25 --act-pct 30` AND flags the same pct as inert (LOW, self-inconsistent within one file)

- `keeper/SKILL.md:412` (§KNOWN DRIFT): "the captain skill (§A lane snapshot)
  instructs relaunching the watcher with `--warn-pct 25 --act-pct 30`."
- `keeper/SKILL.md:436` (Quick reference): same `--warn-pct 25 --act-pct 30`.
- `keeper/SKILL.md:96-99,238-239`: pct flags are inert on 1M models and emit a
  warning if passed.

Note the §A lane snapshot it cites actually says **30/35** (`SKILL.md:222`), not
25/30 — so even the cross-reference is stale. Folds into Conflict 1.

**Resolution:** Same as Conflict 1 — purge pct flags from all keeper-arming guidance.

### Conflict 11 — §A lane snapshot bead IDs / "fleet wound down" baked into the skill (LOW)

- `SKILL.md:201-266` (§A) carries a point-in-time lane table, "fleet wound down,"
  named crews (gurney/chani/liet/duncan/stilgar), and a "Prioritized NEXT work" list
  with specific bead IDs, despite the header at `:206-211,212-222` saying "re-derive
  live every boot." `captain-lanes.md` (the tier-2 file the captain reads at Step 0b)
  carries a *different, newer* lane state (0-crew lean park, logmine, paul/daemon-
  reliability).

**Tension:** Two snapshots-of-record (SKILL.md §A and captain-lanes.md) disagree, and
SHUTDOWN.md Step 5a tells the captain to update SKILL.md §A — but STARTUP.md Step 0b
tells it to read captain-lanes.md. Maintaining the same lane state in two files
guarantees drift; the bead IDs in §A ("standard-bead-dot," hk-h8u7p, the 2026-06-10
retro beads) are months stale.

**Resolution:** Pick ONE lane source of record. captain-lanes.md is the tier-2
per-session file and should win; reduce SKILL.md §A to the *model* ("one lane = one
epic = one crew") + the assignment rule, and delete the point-in-time table and bead
list (or replace with "see `.harmonik/context/captain-lanes.md`"). Update SHUTDOWN.md
Step 5a to write captain-lanes.md, not SKILL.md §A.

---

## CATEGORY 4 — IDENTITY / ROLE CONFUSION

### Conflict 12 — "Your comms identity is `captain`" (hardcoded) vs "identity = THIS session's lane, NOT always captain" (HIGH)

- `STARTUP.md:37` ("Your comms identity is **`captain`**. Pass `--from captain` on
  every send"); `SKILL.md:37,272` ("You run under `$HARMONIK_AGENT` (e.g.
  `captain`)… your `--from` on every comms op"); SHUTDOWN.md uses `--from captain`
  throughout (`:86,120,308`).
- `reference_my_comms_identity.md`: "my identity is whatever lane THIS session is
  assigned… NOT always `captain`… an uncommissioned `--from captain` freezes the
  fleet (two-captains collision)." `SKILL.md:659` itself echoes this: "`<your-lane>`
  is THIS session's comms identity, NOT a hardcoded `captain` — an uncommissioned
  `--from captain` freezes the fleet."

**Tension:** STARTUP.md Step 0 and most of SKILL/SHUTDOWN hardcode `--from captain`,
but the memory note and SKILL.md §10's own keeper-alert example warn that asserting
`--from captain` from a session NOT commissioned as the captain re-creates a
fleet-freezing two-captains collision. The captain skill is loaded by a session that
*is* captain — but the same session can be re-resumed under a different lane, and
STARTUP.md Step 0's `echo "agent=$HARMONIK_AGENT"` check is the only guard.

**Resolution:** The skill is internally inconsistent (hardcoded `captain` vs §10's
`<your-lane>`). Reconcile by making Step 0 normative: "`--from` = `$HARMONIK_AGENT`,
verified at boot; only assert `--from captain` if `$HARMONIK_AGENT == captain` AND
no other live `captain` is in `comms who`. If this session was resumed under a
different lane, you are NOT the captain — do not run this skill's captain ops." This
is the one identity guard and it must apply to ALL the `--from captain` literals, not
just §10.

### Conflict 13 — A crew/agent cannot wake an idle captain, but the captain is told to surface "dual-channel" as if reachable (MEDIUM)

- `SKILL.md:589-615` (§10 dual-surface) + `STARTUP.md:238` instruct surfacing to
  the operator via `comms send --to operator` + status line, and the captain expects
  `comms recv --follow` to deliver operator direction.
- `reference_comms_wake_captain_pane_mismatch.md`: `comms send --to captain --wake`
  FAILS — it derives `harmonik-<hash>-crew-captain` but the captain session is just
  `captain`. So a stalled/idle captain with no armed `--follow` cannot be roused by
  any sanctioned mechanism; escalation must go to the human operator.

**Tension:** Not a direct contradiction, but a reliability gap the instructions don't
flag: the captain is told the comms bus is its operator channel, yet a captain that
lets its `comms recv --follow` lapse (e.g. after a keeper `/clear`, which STARTUP.md
:376-377 says must be re-armed) becomes unreachable, and nothing can wake it. The
PARK procedure (`STARTUP.md:522-555`) deliberately drops `--follow` and relies on a
daemon pane-nudge — but `--wake` to `captain` is exactly the mechanism shown to fail.

**Resolution:** Add a captain reliability note: after every `/clear`/resume and after
PARK, re-arming `comms recv --follow` is LOAD-BEARING because `--wake` cannot reach
the captain pane (pane-name mismatch). The PARK wake relies on the daemon's direct
pane-Enter nudge (a different path than `--wake`); confirm that path targets session
`captain` not `crew-captain`. File the captain-pane `--wake` gap as a bead.

---

## CATEGORY 5 — SELF-DEFEATING FOR CONTEXT ECONOMY

### Conflict 14 — "re-ground via STARTUP.md every resume / do NOT trust handoff" vs the boot is heavy AND restart-earlier wants cheap resume (HIGH)

- `STARTUP.md:374-377`, `SKILL.md:191,704-706`, `SHUTDOWN.md:25`, `keeper/SKILL.md:363`
  all insist: on resume, "Re-ground via STARTUP.md (Steps 2–6) — do NOT trust the
  handoff's live-state claims" and "re-derive live state."
- STARTUP.md Steps 2–6 are a LARGE boot: ~8 ground-truth commands, a live-state
  table, a lane plan, per-lane 5a–5d verification, watcher arming. STARTUP.md offers
  a one-call digest (`captain-boot-digest.sh`, `:103-112`) to compress it.
- `feedback_keeper_band_no_retune.md:16`: restart-EARLIER (warn=200K) is operator-
  directed, but "Net-positive depends on cheap resume (hk-n3w1 TA2 boot-digest)."

**Tension:** The operator wants the captain to restart EARLIER and more often (lower
band) to cut idle token burn — but every restart triggers a full STARTUP.md
re-grounding, which is itself context-expensive. The two pull against each other:
more-frequent restarts × heavy boot = the re-narration burn the lower band was meant
to avoid. STARTUP.md Steps 0a/0b (`:46-73`) explicitly exist to "skip re-deriving
state from scratch," yet Step 2 then re-derives anyway and the resume rule says "do
NOT trust" the cached tier-2/tier-3 state.

**Resolution:** This is the deepest economy tension and the likely root of perceived
unreliability. Reconcile by leaning fully on the boot digest: on a *keeper-restart
resume* (as opposed to a cold boot), the captain should run the ONE
`captain-boot-digest.sh` call + read tier-2/tier-3, and treat Steps 2–6's individual
commands as "only if the digest flags a discrepancy" (STARTUP.md :108-112 already
says this — but the resume instructions at :374-377 contradict it with "do NOT trust
the handoff, re-derive"). Make explicit: handoff INTENT is trusted; tier-2/tier-3
cached state is trusted *as input*; the digest is the single verification pass; full
Step-2 re-derivation is only on a cold boot or a flagged discrepancy.

### Conflict 15 — TERSE-ACK "do not re-narrate" vs the many "surface dual-channel / report state" duties (LOW)

- `STARTUP.md:355-357`, `SKILL.md:675-680`, `keeper/SKILL.md`: on WARN, "ack with
  ONE terse line… DO NOT re-summarize or re-narrate current state… `/clear` is the
  reset."
- vs. the pervasive surface-dual-channel duties (`SKILL.md:455-462`, `STARTUP.md:238`,
  §10) and the FAILED-tick reporting (`STARTUP.md:408-414`).

**Tension:** Minor and reconcilable — the terse-ack rule is scoped to the WARN
moment, the surfacing duties to events/ticks. But a captain under context pressure
(exactly when WARN fires) faces "report state dual-channel" duties and "do NOT
re-narrate state" simultaneously and may freeze or over-report.

**Resolution:** Clarify scope: the terse-ack/no-re-narration rule applies ONLY to the
keeper WARN response; normal event-surfacing (epic_completed, errors) remains
one-line-per-event. Already mostly clear; add an explicit "this does not suppress
event surfacing" clause to §10.

---

## SUMMARY TABLE

| # | Category | Severity | One-line |
|---|---|---|---|
| 1 | Direct | HIGH | keeper band: 30/35 vs 25/30 vs 200K/215K; pct flags inert on 1M |
| 2 | Direct | HIGH | fill-every-lane vs lean-while-away (lean lives only in memory) |
| 3 | Direct | MED | warn "keep working/restart-now" vs latent shared `/quit` text |
| 4 | Direct | MED (superseded) | "band UNCHANGED" vs operator-directed LOWERing |
| 5 | Ambiguous | HIGH | "never spawn sub-agents" vs mandatory 3-agent consensus + fan-out |
| 6 | Ambiguous | MED | `br close` forbidden vs bypass-SOP requires it vs locked-decision |
| 7 | Ambiguous | MED | use `--wave` for concurrency vs "concurrent REAL beads wedge, go serial" |
| 8 | Stale | HIGH | quality-check greps non-existent `workflow_mode` → false all-clear |
| 9 | Stale | MED | §0.5 arms `run_stale,heartbeat` subscribe that STARTUP forbids |
| 10 | Stale | LOW | keeper skill cites inert pct flags + wrong cross-ref value |
| 11 | Stale | LOW | §A lane snapshot duplicates+contradicts captain-lanes.md |
| 12 | Identity | HIGH | hardcoded `--from captain` vs "identity = this lane, not always captain" |
| 13 | Identity | MED | captain unreachable by `--wake`, yet treated as comms-reachable |
| 14 | Economy | HIGH | restart-earlier vs heavy full-STARTUP re-grounding every resume |
| 15 | Economy | LOW | terse-ack/no-re-narrate vs surface-dual-channel duties (scope) |
