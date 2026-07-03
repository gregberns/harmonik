# Transcript Retrospective Tool + Multi-Agent Retro Process — PLAN

Status: PLAN ONLY (no code built/executed). Author: planning sub-agent. Date: 2026-06-25.

## Why this exists (triggering incident)

Overnight Jun 24→25 the fleet finished the "remote test-pyramid" program. Afterward BOTH the
admiral (fleet-objective oversight) and the captain (execution coordinator) went IDLE waiting for
an operator decision on the next major thrust, instead of using their standing authority to keep
work flowing. The operator calls this a PROCESS FAILURE: the admiral had the authority, the
standing "keep moving" directive, and aligned goals (the flywheel positive/negative-reinforcement
machinery exists precisely to keep work flowing) — yet it escalated a decision menu and held.

This plan delivers (1) a REUSABLE transcript-extraction tool the platform can run on any incident,
and (2) a repeatable multi-agent retro process that answers, for THIS incident:

- **Q1.** When was the remote test-pyramid work actually finished?
- **Q2.** Between work-finish and "now" (~2026-06-25 09:30 local / 16:30Z), how many inter-agent
  messages, watchdog ticks, and wakeups happened?
- **Q3.** Why did the admiral NOT take a decision to move forward, given it had the authority?
- **Q4 (audit).** Which captain/admiral directives OVERLAP / CONFLICT / are UNCLEAR / CONTRADICT,
  such that "escalate-and-hold" won over "decide-and-move"?

Investigation window: **2026-06-24 21:00 local (UTC-7) → 2026-06-25 09:30 local** =
**2026-06-25T04:00:00Z → 2026-06-25T16:30:00Z**.

---

## 0. Ground-truth confirmed by real-file inspection

### 0.1 Claude Code transcript JSONL schema (verified)

Files: `/Users/gb/.claude/projects/-Users-gb-github-harmonik/<session-id>.jsonl`, one JSON object
per line. 411 files total; ~18 modified inside the window.

Top-level record `.type` values observed: `mode`, `file-history-snapshot`, `user`, `assistant`,
`system`, `attachment`, `queue-operation`, `last-prompt`, `ai-title`.

Key fields the extractor reads:

- Every record: `.type`, `.sessionId`, `.timestamp` (ISO-8601 **UTC, `Z`-suffixed**, e.g.
  `2026-06-25T15:13:51.894Z`).
- `user` record: `.message.role == "user"`, `.message.content` is EITHER a string (the seed /
  slash-command / operator turn) OR an array of blocks; array blocks are `{type:"text"}` or
  `{type:"tool_result", ...}` (tool output coming back to the model).
- `assistant` record: `.message.role=="assistant"`, `.message.model` (e.g. `claude-opus-4-8`),
  `.message.content` is an array of blocks with `.type` in `{thinking, text, tool_use}`.
  - `tool_use` block: `.name` (observed: `Bash`, `Read`, `Agent`, `ScheduleWakeup`), `.input`.
    - `Bash` → `.input.command`, `.input.description`.
    - `ScheduleWakeup` → `.input.delaySeconds`, `.input.reason`, `.input.prompt`.
    - `Agent` → `.input.subagent_type`, `.input.description`, `.input.prompt`.
- `system` record: `.subtype` in `{local_command, stop_hook_summary, turn_duration, ...}` —
  mostly noise; `turn_duration` carries `.durationMs`, `.messageCount`.
- `queue-operation` record: `.operation` (Claude Code's local prompt queue; usually noise).

**Watchdog/wakeup signatures inside the transcript** (these are how the admiral got re-woken):
- A scheduled wake arrives as a `user` string turn whose text contains
  `Admiral hourly progress-watchdog tick` (the `ScheduleWakeup.prompt` echoed back in).
- A monitor/comms event arrives as a `user` string turn wrapped in
  `<task-notification>…<summary>Monitor event: "inbound comms addressed to admiral"…</task-notification>`.
- Operator turns are `user` string turns that are neither of the above and not a `<command-*>`
  slash wrapper.

### 0.2 Agent-identity resolution (verified — DO NOT trust the live crew registry)

Session IDs flip on every keeper `/clear`, so ONE agent spans MANY files across the window. Resolve
identity from FILE CONTENT, by counting these markers and taking the **dominant** one (a watch
session legitimately contains a few `--from gurney` lines because it relayed others' comms — so
first-match is wrong; use the modal `--from`/`--agent` value):

| Marker (regex)                         | Example (real counts in sampled files)                  |
|----------------------------------------|---------------------------------------------------------|
| `You are \`<agent>\``                  | `You are \`admiral\`` ×9, `You are \`captain\`` ×22      |
| `HARMONIK_AGENT=<agent>`               | `HARMONIK_AGENT=admiral` in admiral session             |
| `--from <agent>` (comms send)          | `--from watch` ×47, `--from captain` ×32, `--from ctx-watchdog` ×300 |
| `--agent <agent>` (comms recv)         | `--agent admiral` ×68                                    |
| `/session-resume … HANDOFF-<role>.md`  | `HANDOFF-admiral.md` ×53, `HANDOFF-captain.md` ×51       |
| `missions/<agent>.md`                  | `missions/watch.md` ×175                                 |

Heuristic: score each candidate agent = weighted sum of markers; pick max. Weight the explicit
identity markers (`You are`, `HANDOFF-<role>`, `HARMONIK_AGENT`, mission path) higher than relayed
comms (`--from`/`--agent`), since the latter appear for OTHER agents too. Emit a confidence value
and the marker breakdown into the metadata header so a human can sanity-check a low-confidence call.

### 0.3 Cross-reference ground truth (verified present)

- `.harmonik/events/events.jsonl` (~52 MB). Typed events; each has `.type`, `.timestamp_wall`
  (ISO-8601 with offset, e.g. `2026-06-25T16:37:28.441Z` or `-07:00` form), `.event_id`,
  `.source_subsystem`, `.payload`. Relevant types in-window: `agent_message` (the bus — has
  `.payload.from` / `.payload.to`), `epic_completed`, `agent_heartbeat`, `governor_signal`,
  `node_dispatch_*`, `reviewer_verdict`, `agent_presence`.
  - In-window `agent_message` counts by sender (already measured): watch 36, captain 32,
    ctx-watchdog 28, gurney 23, admiral 17.
  - `epic_completed` events exist (37 total); the test-pyramid finish is identifiable here +
    by `remote-test` / `test-pyramid` payload substrings (10/9/3/7 hits across spellings).
- `harmonik comms log --json` — same `agent_message` bus, but resolves the LIVE bus (use as the
  authoritative message ledger; cross-check counts against events.jsonl).

These two corroborate transcript timestamps and answer Q2's message/tick/wake counts
deterministically (don't infer counts from prose).

### 0.4 Instruction sources for the audit (verified present, with line counts)

`.claude/skills/captain/STARTUP.md` (754L), `.claude/skills/captain/SKILL.md` (790L),
`.claude/skills/orchestrator-rules/SKILL.md` (184L), `.harmonik/crew/missions/admiral.md` (144L),
`.harmonik/crew/admiral-playbook.md` (152L), `.harmonik/crew/admiral-initiatives.md` (53L),
`.harmonik/context/captain-lanes.md` (204L), plus the global `~/.claude/CLAUDE.md` "Keep moving"
rules, project `AGENTS.md`/`CLAUDE.md`, and the flywheel directives (grep `flywheel` hits in
`.harmonik/config.yaml`, `.harmonik/crew/admiral-playbook.md`, `.harmonik/crew/admiral-initiatives.md`,
`.harmonik/context/captain-lanes.md`, `.harmonik/cognition/ctx-watchdog-prompt.txt`).

---

## 1. THE EXTRACTOR SCRIPT (reusable platform tool)

### 1.1 Name, language, location

- **Language: Python 3** (stdlib only — `json`, `argparse`, `datetime`, `pathlib`, `re`,
  `glob`). Justification: the work is per-line JSON parsing + multi-marker identity scoring +
  timezone math + Markdown rendering + truncation logic. `bash+jq` can do the JSON slicing but the
  identity-scoring heuristic, the local↔UTC dual-stamping, snippet trimming, and the
  cross-session chaining are control-flow-heavy and become unreadable in jq. Python keeps it one
  testable file with zero non-stdlib deps (matches harmonik's "no surprise deps" posture).
- **Location now:** `scripts/transcript-extract.py` (a platform utility, sibling to other
  `scripts/` tooling). **Later:** promote to a `harmonik transcript extract` subcommand once the
  shape stabilizes (the JSONL path, events.jsonl path, and comms log are all already harmonik
  concepts) — noted as a follow-up, not built here.
- The script writes only into the caller-supplied `--out-dir` (default
  `plans/2026-06-25-transcript-retro-tool/out/`); it never mutates transcripts or repo state.

### 1.2 Inputs (three selection modes)

```
transcript-extract.py \
  [--session <id> ...]                 # explicit session id(s); repeatable
  [--agent <name> --since <ts> --until <ts>]   # agent-name + window
  [--since <ts> --until <ts>]          # raw window, ALL agents
  [--projects-dir <path>]              # default: /Users/gb/.claude/projects/-Users-gb-github-harmonik
  [--events <path>]                    # default: .harmonik/events/events.jsonl
  [--comms-log <cmd|file>]             # default: `harmonik comms log --json`
  [--out-dir <path>]                   # default: plans/.../out
  [--local-offset -7]                  # window/display offset; default -7
  [--redact]                           # scrub secrets/tokens from snippets
  [--max-tool-output 800]              # truncate tool_result/Bash output to N chars
```

`--since/--until` accept local (`2026-06-24 21:00`) or UTC (`2026-06-25T04:00:00Z`); normalize both
to UTC internally using `--local-offset`.

### 1.3 Session→file discovery + per-agent chain building

1. **Candidate files:** `glob(<projects-dir>/*.jsonl)`. Filter by window using each file's
   first/last record `.timestamp` (a file is in-window if its `[first,last]` interval intersects
   `[since,until]`). Modes:
   - `--session`: take exactly those files.
   - `--agent`: keep in-window files whose dominant identity (§0.2) == the agent.
   - raw window: keep all in-window files.
2. **Identity resolution** per file (§0.2): emit `agent`, `confidence`, `marker_breakdown`.
3. **Chain building** (handle `/clear` flips): for each agent, order its files by first-record
   `.timestamp`. Link adjacent sessions via the `/session-resume … HANDOFF-<role>.md` seed at the
   TOP of file N+1 and the `session-handoff` write near the BOTTOM of file N. Record
   `cleared_from` (prev session id) and `cleared_to` (next session id) so the chain is navigable.
   This produces, per agent, an ordered list of `(session_id, file, [start,end])`.

### 1.4 Output: ONE Markdown file per session

Filename: `<out-dir>/<local-start>__<agent>__<session8>.md` (e.g.
`2026-06-25T0413-0700__admiral__f49808b2.md`) so a directory listing sorts chronologically.

**(a) METADATA HEADER** (YAML-ish front block):
```
agent:            admiral            (confidence 0.94)
session_id:       f49808b2-cf07-...
file:             /Users/gb/.claude/projects/.../f49808b2-....jsonl
timeframe_local:  2026-06-24 21:13 → 2026-06-25 02:37  (UTC-7)
timeframe_utc:    2026-06-25T04:13Z → 2026-06-25T09:37Z
turns:            user=28  assistant=50  (tool_use=20)
model:            claude-opus-4-8
keeper_chain:     cleared_from=<prev8 or "cold-boot">  cleared_to=<next8 or "live">
marker_breakdown: You-are=9 HANDOFF-admiral=53 --agent admiral=68 --from admiral=36
```

**(b) DE-NOISED CHRONOLOGICAL DIGEST** — one line per notable event, each stamped
`[LOCAL | UTC]`, in file order:

- **User turns** classified into: `OPERATOR` (free text), `WAKE` (matches
  `*progress-watchdog tick*` / `ScheduleWakeup` echo), `MONITOR` (the `<task-notification>` /
  `Monitor event:` wrapper), `RESUME` (`/session-resume` seed), `TOOL_RESULT` (collapsed —
  shown only if it carries an error or is referenced by a kept tool_use).
- **Assistant decision text** — the `text` blocks (what the agent SAID it would do); thinking
  blocks summarized to first sentence only (flag `--include-thinking` to keep full).
- **Notable tool calls only:**
  - `Bash` where the command matches `harmonik (comms|queue|subscribe)`, `br `, `ScheduleWakeup`,
    `git ` (commit/push/merge), or `kerf ` → show trimmed command + trimmed output.
  - `comms send`/`comms recv` → show `--from`/`--to`/`--topic` + body snippet.
  - `ScheduleWakeup` → show `delaySeconds` + first line of `reason`.
  - `Agent`/Task spawns → show `subagent_type` + `description`.
  - `br`/`queue` ops → show the verb + ids.
- **Collapse/suppress:** `agent_heartbeat`/`governor_signal`-style noise, `turn_duration`,
  `stop_hook_summary`, repeated identical comms-poll Bash (collapse runs of N identical polls into
  `(×N polls, no new mail)`), and any tool output > `--max-tool-output` (truncate with `… [+K
  chars]`).
- Each digest line: `[2026-06-24 21:14 | 04:14Z]  WAKE  hourly watchdog tick — "check operator's
  next-thrust answer …"`.

### 1.5 Extra features to include (called out, in priority order)

1. **Cross-session MANIFEST / index** (`<out-dir>/INDEX.md`): table of every session doc — agent,
   window (local+UTC), turn counts, keeper-chain links, file link. Plus a per-agent chain diagram
   (cold-boot → s1 → s2 → live).
2. **Event-log cross-link column:** for each kept comms `send`/`recv` digest line, look up the
   matching `agent_message` in events.jsonl by (from,to,~ts±2s) and annotate `✓bus` (corroborated)
   or `⚠no-bus` (transcript claims a send the bus didn't record — a real failure signature).
3. **`--redact`:** scrub obvious secrets (`sk-…`, `ghp_…`, `AWS…`, bearer tokens, `HK_*` values)
   from snippets before writing — so retro docs are shareable.
4. **Token/size column** (optional): approximate per-turn token estimate (chars/4) so the analysis
   stage can see where context was spent.
5. **`--manifest-only`:** discovery + identity + chain build WITHOUT rendering digests (fast survey
   of "which sessions are in this window and who owns them").

---

## 2. EXTRACTION-AGENT STAGE (one agent per session doc)

For EACH generated `<session>.md`, spawn one sub-agent (the doc is small + de-noised, so this is
cheap and parallel). Each emits a structured per-session summary to
`out/summaries/<agent>__<session8>.summary.md`:

```
session: <agent> <session8>  [local window | utc window]
role_in_incident: <e.g. admiral oversight / captain coord / watch triage / ctx-watchdog>
key_events:                     # each: [LOCAL|UTC]  one-line what-happened  + raw snippet ref
decisions_made:                 # explicit decisions THIS session took (or explicitly deferred)
stall_points:                   # PRECISE moments the agent could have acted but held/escalated,
                                #   with the timestamp + the exact deferral text snippet
work_finish_signals:           # any mention/evidence the test-pyramid finished + timestamp
counts_seen:                    # wakes, monitor events, comms in/out as seen in THIS doc
open_threads:                   # anything handed to the next session via keeper chain
```

Prompt contract per extraction agent: "Read ONLY this one doc. Do not speculate beyond it. Quote
timestamps verbatim. Flag every point where the agent deferred a decision it had authority to make,
with the snippet."

## 3. ANALYSIS STAGE (cross-session synthesis)

One synthesis agent reads ALL `out/summaries/*.md` + queries events.jsonl/comms log directly, and
writes `out/ANALYSIS.md`:

- **Merged timeline** (single chronological table across all agents, local+UTC), built from the
  per-session `key_events` + `stall_points`, deduped/corroborated against events.jsonl.
- **Direct answers to Q1–Q3:**
  - **Q1 (work finished when):** the `epic_completed` event for the test-pyramid in events.jsonl
    (authoritative `timestamp_wall`), cross-checked against the captain's "DONE+verified" comms
    posts. Report the wall time in local+UTC.
  - **Q2 (messages/ticks/wakes between finish and 16:30Z):** deterministic counts —
    `agent_message` count from events.jsonl/comms log split by sender (already measured: watch 36,
    captain 32, ctx-watchdog 28, gurney 23, admiral 17 over the full window; re-scope to
    [finish, 16:30Z]); watchdog ticks = count of `*progress-watchdog tick*` WAKE turns in the
    admiral chain; wakeups = count of `ScheduleWakeup` + monitor-notification user turns across
    admiral+captain. Present a small table with exact numbers, not prose.
  - **Q3 (why no decision):** synthesize the stall_points — the chain of "I'll wait for the
    operator's next-thrust answer" deferrals in admiral + captain, with timestamps, showing the
    decision was reachable (authority present, flywheel available, work queue drained) yet held.
- **Corroboration notes:** any transcript claim NOT backed by the event bus (`⚠no-bus`).

## 4. INSTRUCTION-AUDIT STAGE (directive-conflict panel)

A small panel (3 agents, distinct lenses; see §5) reads the §0.4 instruction set and produces
`out/INSTRUCTION-AUDIT.md`: a **RANKED list of concrete directive conflicts**, each with
`file:line` refs + a proposed reconciliation. The panel MUST specifically resolve the central
tension:

- **"Hold" pull** — keep-fleet-lean-while-operator-away; don't unpark initiatives on admiral
  authority; "the major thrust is the operator's call"; SURFACE-AND-AWAIT for a genuinely new
  initiative not in the known ranked feed. (Sources to cite with line refs: captain SKILL.md
  surface-and-await clause; admiral mission/playbook "operator owns the thrust"; captain-lanes
  dated "lean while away" directives; the "feedback_captain_lean_while_operator_away" posture.)
- **"Move" pull** — admiral has authority + duty to keep work moving; the flywheel
  positive/negative machinery exists to keep work flowing; global `~/.claude/CLAUDE.md` "Keep
  moving / don't stop to ask should-I-continue / end on the next action"; orchestrator-rules
  autonomy/flow boundaries; captain "fill every non-conflicting free slot."

Output schema per conflict:
```
RANK <n>  [SEVERITY: blocking|major|minor]
title:        <one line>
pull-A (hold): <quote> — <file>:<line>
pull-B (move): <quote> — <file>:<line>
why-it-fired:  <how this pair produced escalate-and-hold in THIS incident>
reconciliation:<concrete proposed edit: which doc gets the tie-breaker rule, exact wording>
```

The reconciliation should converge on a single **tie-breaker rule** (candidate, for the panel to
refine, not pre-decided here): *"When the ready queue is drained AND a parked/flywheel lane exists
that advances a LOCKED objective, the admiral advances it on its own authority and NOTIFIES the
operator; SURFACE-AND-AWAIT is reserved for ranking a brand-new initiative not already in the known
feed or reversing a locked decision."* The panel decides the exact home + wording.

## 5. ORCHESTRATION SHAPE & RUN RECIPE

Artifacts all land under `plans/2026-06-25-transcript-retro-tool/out/`:
- `out/sessions/*.md` (extractor output), `out/INDEX.md`
- `out/summaries/*.summary.md` (stage-2)
- `out/ANALYSIS.md` (stage-3)
- `out/INSTRUCTION-AUDIT.md` (stage-4)
- `out/RETRO.md` (final merge — Q1–Q4 answered + ranked fixes + recommended directive edits)

**Agent count & topology (pipeline with a parallel fan in the middle):**

1. **Build the extractor** — 1 agent writes `scripts/transcript-extract.py` + a tiny unit test on
   2 sample files. (Serial; gates everything.)
2. **Run extractor** on the window → `out/sessions/*.md` + `INDEX.md`. (1 agent / direct run.)
3. **Extraction fan-out** — N agents in PARALLEL, one per session doc (expect ~10–18 docs; cap at
   ~10 concurrent, all `run_in_background=true`). → `out/summaries/`.
4. **Analysis synthesis** — 1 agent (waits for stage-3) → `out/ANALYSIS.md`.
5. **Instruction-audit panel** — 3 agents in PARALLEL, distinct lenses: (a) admiral-authority
   lens, (b) captain-autonomy lens, (c) global "keep moving" vs project "lean while away" lens;
   then 1 reconciler merges their findings → `out/INSTRUCTION-AUDIT.md`. (Can run concurrently with
   stage-3/4 — it reads instruction files, not transcripts.)
6. **Final merge** — 1 reviewer agent reads ANALYSIS + INSTRUCTION-AUDIT, fresh-context, writes
   `out/RETRO.md` (Q1–Q4 + ranked reconciliations) and sanity-checks the counts against
   events.jsonl one more time (review gate).

End-to-end run recipe (admiral hands stages to sub-agents):
```
# stage 1 (serial)
agent: "build scripts/transcript-extract.py per PLAN §1; add a 2-file unit test; do not run on the full set yet"
# stage 2
run: scripts/transcript-extract.py --since 2026-06-25T04:00:00Z --until 2026-06-25T16:30:00Z \
       --local-offset -7 --out-dir plans/2026-06-25-transcript-retro-tool/out/sessions
# stage 3 (parallel, background) — one agent per out/sessions/*.md → out/summaries/
# stage 4 (parallel) — 3 audit-lens agents + 1 reconciler → out/INSTRUCTION-AUDIT.md  (independent of 3)
# stage 5 — 1 synthesis agent → out/ANALYSIS.md  (after 3)
# stage 6 — 1 reviewer → out/RETRO.md  (after 4 & 5)
```

---

## Key design decisions

1. **Python stdlib, single file in `scripts/`**, promotable to a `harmonik transcript extract`
   subcommand later — chosen over bash+jq because identity-scoring + dual-TZ stamping +
   chain-linking are control-flow-heavy.
2. **Identity by DOMINANT marker, with confidence**, not first-match — a session legitimately
   relays other agents' comms (`--from gurney` inside a watch session), so modal scoring + a
   confidence value in the header is required to survive `/clear` session flips.
3. **events.jsonl + comms log are the authoritative counters** for Q2; the transcript supplies the
   narrative/stall-points; the tool cross-links the two (`✓bus`/`⚠no-bus`) to catch claimed-but-
   unrecorded actions.
4. **Pipeline with two parallel fans** (per-session extraction; 3-lens audit panel) — extraction
   and audit don't depend on each other; only final merge joins them. A fresh-context reviewer
   does the final merge (review gate).
5. **The audit must land on ONE tie-breaker rule** resolving hold-vs-move, with a concrete home +
   wording, not just a list of tensions — that is the actual process fix.

## Open questions that genuinely need the operator

1. **Authority tie-breaker — final wording & home.** The audit will PROPOSE the "drained-queue +
   parked-flywheel-lane → advance-and-notify; surface only for brand-new-initiative/locked-reversal"
   rule. Reversing the current "operator owns the major thrust" posture is a locked-decision-class
   change — do you want the panel to (A) propose the edit for your approval, or (B) apply it
   directly to the admiral mission/playbook once the panel agrees? (Recommend A.)
2. **Scope of "next thrust" the admiral may self-authorize.** Should admiral autonomy cover ONLY
   resuming already-parked/known lanes (e.g. flywheel, token-optimization), or also GREENLIGHTING a
   net-new program (e.g. the Pi gateway) when the queue is empty? This shapes how aggressive the
   tie-breaker is and is the one product-shaping call I can't make for you.
3. **Where the tool ultimately lives.** Ship as `scripts/transcript-extract.py` now (my default),
   or hold for the `harmonik transcript extract` subcommand? I'll proceed with the script unless you
   want it as a first-class subcommand from the start.
