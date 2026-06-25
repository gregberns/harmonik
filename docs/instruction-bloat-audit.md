# Instruction-Bloat Audit

**Bead:** hk-s2xo3  
**Date:** 2026-06-25  
**Scope:** Agent-instruction surface — boot-token measurement, duplicate/near-duplicate contract detection, trigger-less principle-text inventory, and a ranked cut-list.  
**Method (token estimation):** `wc -c <file> / 4` (chars/4). This under-counts code blocks and over-counts prose slightly; treat values as ±15%.  
**Read-only:** no live contract was modified.

---

## 1. Boot Tokens Per Role

### 1.1 Captain (cold boot)

Boot files per AGENTS.md §"Per-role load map" + STARTUP.md Steps 0a/0b/0c/1. Files load in order shown.

| # | File | Chars | ~Tokens | Notes |
|---|------|------:|-------:|-------|
| 0a | `.harmonik/context/project.yaml` | 1,605 | 401 | tier-3; phase + locked decisions |
| 0b | `.harmonik/context/captain-lanes.md` | 27,284 | 6,821 | tier-2; heavily annotated with stale history |
| 0c | `.harmonik/context/direction-log.md` | 2,796 | 699 | tier-2; sequencing intent |
| 1a | `.claude/skills/captain/SKILL.md` | 45,693 | 11,423 | per-crew mechanics |
| 1b | `.claude/skills/captain/STARTUP.md` | 47,070 | 11,768 | boot checklist |
| 1c | `.claude/skills/beads-cli/SKILL.md` | 7,624 | 1,906 | loaded alongside SKILL.md |
| 1d | `.claude/skills/orchestrator-rules/SKILL.md` | 19,584 | 4,896 | standing-rules contract |
| 1e | `HANDOFF.md` | absent | 0 | (variable; typical ~1,250–3,750 tokens when present) |
| — | `AGENTS.md` (always injected by harness) | 10,380 | 2,595 | project CLAUDE.md |

**Captain cold boot total (excluding HANDOFF): ~40,509 tokens**  
With a typical HANDOFF (~8k chars): **~42,509 tokens**

Not loaded at cold boot per STARTUP.md §1 "SLIM COLD-BOOT": `agent-comms/SKILL.md` (2,730 tokens) and `harmonik-dispatch/SKILL.md` (2,303 tokens) are deferred to first use of each surface.

### 1.2 Captain (keeper-restart resume — LEAN)

Per STARTUP.md "On resume after a restart-now cycle": tier-3 + tier-2 (Steps 0a/0b/0c) + `scripts/captain-boot-digest.sh` (runtime script, no skill text). Does NOT re-run STARTUP.md §1-5 in full.

| File | ~Tokens |
|------|-------:|
| project.yaml | 401 |
| captain-lanes.md | 6,821 |
| direction-log.md | 699 |
| HANDOFF.md (prior session) | ~2,500 variable |
| AGENTS.md (injected) | 2,595 |

**Resume total: ~13,016 tokens** (the remainder re-hydrates from cached context)

### 1.3 Crew (cold boot)

Per AGENTS.md §"Per-role load map": mission file + crew-launch + agent-comms + beads-cli + harmonik-dispatch. Mission files range from 772–3,569 tokens; admiral.md is an outlier at 3,569 tokens.

| File | Chars | ~Tokens |
|------|------:|-------:|
| `.harmonik/crew/missions/<crew>.md` (typical: gurney) | 4,090 | 1,023 |
| `.claude/skills/crew-launch/SKILL.md` | 23,830 | 5,958 |
| `.claude/skills/agent-comms/SKILL.md` | 10,920 | 2,730 |
| `.claude/skills/beads-cli/SKILL.md` | 7,624 | 1,906 |
| `.claude/skills/harmonik-dispatch/SKILL.md` | 9,210 | 2,303 |
| `AGENTS.md` (injected) | 10,380 | 2,595 |

**Crew cold boot total: ~16,515 tokens** (with typical mission file)  
Admiral mission (14,276 chars = 3,569 tokens) pushes admiral boot to ~19,061 tokens.

### 1.4 Implementer-Orchestrator (session-resume)

Per AGENTS.md §"Per-role load map": AGENT_INDEX + STATUS + HANDOFF + orchestrator-rules + harmonik-dispatch.

| File | Chars | ~Tokens |
|------|------:|-------:|
| `AGENT_INDEX.md` | 15,757 | 3,939 |
| `STATUS.md` | 9,246 | 2,312 |
| `HANDOFF.md` | absent | 0 (variable) |
| `.claude/skills/orchestrator-rules/SKILL.md` | 19,584 | 4,896 |
| `.claude/skills/harmonik-dispatch/SKILL.md` | 9,210 | 2,303 |
| `AGENTS.md` (injected) | 10,380 | 2,595 |

**Implementer-orchestrator total (without HANDOFF): ~16,045 tokens**  
With typical HANDOFF (~8k chars): **~18,045 tokens**

### 1.5 Summary

| Role | Boot tokens (no HANDOFF) | With HANDOFF |
|------|------------------------:|-------------:|
| Captain — cold boot | ~40,509 | ~42,509 |
| Captain — keeper-restart resume | ~13,016 | ~15,516 |
| Crew — cold boot (typical mission) | ~15,492 | N/A (mission IS the handoff) |
| Implementer-orchestrator | ~16,045 | ~18,045 |

Captain cold boot is **2.5× crew boot**, driven mainly by STARTUP.md (11,768 tokens) + SKILL.md (11,423 tokens) comprising 57% of captain's total.

---

## 2. Duplicate / Near-Duplicate Contracts

File abbreviations used below:  
- **OR** = `.claude/skills/orchestrator-rules/SKILL.md`  
- **CS** = `.claude/skills/captain/SKILL.md`  
- **ST** = `.claude/skills/captain/STARTUP.md`  
- **CL** = `.claude/skills/crew-launch/SKILL.md`  
- **AC** = `.claude/skills/agent-comms/SKILL.md`  
- **BC** = `.claude/skills/beads-cli/SKILL.md`  
- **HD** = `.claude/skills/harmonik-dispatch/SKILL.md`  
- **AG** = `AGENTS.md`

### D1 — Keeper WARN on-WARN 4-step procedure (verbatim)

The "restart-now is REQUIRED at the next clean checkpoint" 4-step block appears **twice**, nearly word-for-word:
- ST §"When you receive a WARN — restart-now is REQUIRED..."  (steps 1–4, ~900 chars)
- CS §10 "On a WARN, restart-now is REQUIRED at the next clean checkpoint:" (steps 1–4, ~800 chars)

Both files also carry the `TERSE-ACK / NO-RE-NARRATION rule (HARD — hk-4zy9, ON-059)` verbatim:  
- ST §TERSE-ACK  
- CS §10 TERSE-ACK  

Total duplicated text: ~1,700 chars (~425 tokens).

### D2 — KNOWN-vs-brand-new lane autonomy definition (5 locations)

OR §Autonomy explicitly declares: *"The CANONICAL home of the KNOWN-vs-brand-new definition (stated ONCE here; every role file... carries only a one-line POINTER back to this section)."*  Despite this declaration, the full or near-full definition appears in:
- OR §Autonomy: canonical home
- CS §0: full re-statement including durable-doc list and "only a never-before-recorded initiative is the operator's to rank"
- CS §0 (again) R-C4.6 NORMATIVE block: another full statement
- CS §8 case 1: "A lane is brand-NEW only by that test — NOT because it is parked..."
- CS §A "PARKED is a fact; GATED is a named live gate": repeats the distinction
- ST §4: re-states in "Organize the KNOWN open backlog" section
- `.harmonik/context/AGENTS.md` §"KNOWN vs brand-new": full re-statement

Total across non-canonical files: ~3,500 chars (~875 tokens) that should be one-line pointers.

### D3 — `--assignee` mirror on every epic adoption (Gap 1)

The "mirror --assignee on EVERY adoption, boot AND re-task" rule appears in:
- CL §Step 4: full 200-word explanation (canonical crew-side home)
- CL §MUST: "Run br update <epic_id> --assignee <crew_name> on EVERY epic adoption..."
- CS §5: "Attribute the owning crew — via the durable beads mirror (Gap 1)"
- ST §5b: "`br update <epic_id> --assignee <crew>`"
- CL frontmatter description: "The Gap-1 --assignee-on-every-adopt rule is present"

The full rationale (logmine F13, ≥4 exchanges observed) is repeated in both CL §Step 4 and CS §5. ~600 chars duplicated (~150 tokens).

### D4 — Daemon terminal transitions / beads write discipline (5 locations)

The rule "agents MUST NOT issue terminal-transition br writes" appears as:
- BC §Write discipline: canonical, full explanation
- OR §Bead lifecycle discipline: "THE DAEMON OWNS TERMINAL TRANSITIONS (HARD RULE)... Do NOT `br update --status=in_progress`"
- CL §MUST NOT: "`br close`, `br claim`, or `br reopen` any bead (daemon-only terminal writes)"
- CS §8: "NEVER pre-assign a dispatchable bead... `br close`—the ONE sanctioned exception (M7/hk-039z)"
- AG §Decisions: "beads owns terminal bead transitions; harmonik daemon does NOT call br close directly"

The BC skill is the declared authority. OR's bead-lifecycle section and CL's MUST-NOT are acceptable summary pointers. CS §8's long sanctioned-exception discussion (~700 chars) is the highest-value addition but is partially duplicated from BC. ~400 chars duplicated (~100 tokens).

### D5 — Event_id deduplication (N3 rule) in 4 locations

- AC §Delivery guarantee: canonical, full explanation with pseudocode
- CL §Subscribe: "dedupe on event_id... N3"
- CL §MUST: "Dedupe all comms messages on event_id (agent-comms N3, NORMATIVE)"
- CS §4: "Dedupe anything you RECEIVE on event_id (agent-comms N3)"

CL and CS appropriately reference N3; the full pseudocode explanation exists only in AC. These are short (~50 chars each) and function as acceptable one-line pointers.

### D6 — `harmonik subscribe` monitor command (4 locations)

Full `harmonik subscribe --types run_completed,run_failed,run_stale,heartbeat --heartbeat 60s --json` command block appears in:
- OR §Monitor pattern: canonical with explanation
- HD §Arm a Monitor: same flags, same code block
- CL §Operating loop §3: same command
- AC §Monitoring daemon run events: a 250-char cross-reference block

Duplicated text: ~900 chars across 3 extra copies (~225 tokens).

### D7 — `br ready --limit 0` full explanation (3 locations)

The "ALWAYS pass --limit 0; bare br ready silently caps at 20" rule with its two-sentence rationale appears in:
- BC §Check available work: canonical
- CL §Operating loop §1: "RULE — `br ready` = dispatchable-now..." (~250-char block)
- OR §Review and quality gates: "TRUST `br ready` BUT VERIFY... `br ready --limit 0`"

~500 chars duplicated (~125 tokens).

### D8 — Sub-agent dispatch 3 exceptions (2 full copies)

- OR §Dispatch discipline: "THE THREE EXCEPTIONS (HARD RULE). (a)/(b)/(c)"
- HD §When to NOT route through the daemon: same 3 cases with nearly identical wording

~300 chars duplicated (~75 tokens).

### D9 — `comms join` / `comms leave` at boot/shutdown (3 locations)

- AC §join/leave: canonical
- CS §1: "Announce presence at start; leave at clean shutdown: `harmonik comms join`"
- CL §Boot sequence §3 and §Clean shutdown: `comms join` + `comms leave`

Short (one-liners each). Acceptable as reminders, but each adds ~100 chars.

### D10 — `harmonik start captain` launch command (3 locations)

- ST §Step 6 "Keeper arming": full launch explanation with both windows (canonical)
- CS §10: "You MUST be launched via `harmonik start captain`... NO env var, NO script path"
- AG §Start here: "Use the native umbrella verb — `harmonik start captain`"

~800 chars of additional context in CS §10 partially repeats ST §6. ~400 chars duplicated (~100 tokens).

### D11 — `run_stale` ≠ wedge signal (2 locations)

- OR §Run liveness: full rule with the 30-minute ceiling and the jq command
- ST §6 "SLOW-RECOVERY vs GENUINE-WEDGE guard": same principle with slightly different wording

~600 chars of overlap (~150 tokens).

### D12 — Crew MUST NOT submit to main queue (2 locations within CL)

- CL §Operating loop §2: "HARD RULE: the crew MUST NOT submit to the `main` queue"
- CL §MUST NOT: "Submit to the `main` queue (HARD RULE...)"

Same rule stated twice within a single file. ~200 chars.

---

## 3. Trigger-Less Principle Text

These passages state principles without a named trigger condition (no "when X, do Y" or "fires on Y event"):

### T1 — OR §Identity ("ROLE. You are the orchestrator. Delegate substantively.")

> "ROLE. You are the orchestrator. Delegate substantively. Keep the main thread minimal — it exists to dispatch, not to implement or investigate. The main-thread context window is precious; protect it."

No trigger. "Delegate substantively" and "keep main thread minimal" are aspirational but don't say when inline work is acceptable vs. delegation. ~180 chars.

### T2 — OR §Priority ("PHASE-3 DOT IS THE NEAR-TERM ENDGAME.")

> "PHASE-3 DOT IS THE NEAR-TERM ENDGAME. DOT-defined bead-process workflow is the planned replacement for `--review-loop`."

No trigger — doesn't say when to use DOT vs. review-loop, or what action fires. ~130 chars.

### T3 — OR §Priority ("KERF IS IN BETA.")

> "KERF IS IN BETA. Use `kerf next` as the primary dispatch surface but expect friction... Log issues to `docs/kerf-beta-feedback.md`."

"Log issues" has no trigger (which issues? what frequency?). ~200 chars.

### T4 — HD §75% criterion

> "Each session ends with a tally: substantive commits this session, of which N landed via the daemon queue... Target: N/total ≥ 0.75. Sessions that miss the target log a one-line reason in `/session-handoff`."

No trigger for what changes when the target is missed — no corrective action is specified. Reads as aspirational metric. ~280 chars.

### T5 — CS §11 "Where the future judgment layer plugs in"

Entire section (~600 chars) describes a future design, not current behavior. Contains no current trigger conditions. Pure forward-looking narrative.

### T6 — OR §Operational rules ("NO CI. Do not propose GitHub Actions.")

> "NO CI. Do not propose GitHub Actions."

No trigger. When would an agent be tempted to propose CI? Not stated. ~60 chars.

### T7 — HD §References block

> "AGENTS.md §'Daily loop (canonical)' + §'Submitting work'... HANDOFF.md — the current orchestration directive..."

A 4-line reference list with no actionable trigger. Agents don't "do something" when they read a references block. ~300 chars.

### T8 — AG §Key conventions (non-trigger statements)

Several sentences in AGENTS.md §Key conventions:
- "Specs live in `specs/` at the repo root. These are normative: the spec is always right, and code is expected to match it."
- "Knowledge base docs (`docs/`) capture problems, goals, concepts..."

These are definitional facts, not triggers. No condition activates them. ~400 chars.

### T9 — AG §Don't block

> "Don't reopen locked-in decisions without explicit user request."  
> "Don't add abstraction layers the user hasn't asked for."  
> "Don't skip the AGENT_INDEX → STATUS → captain-lanes → HANDOFF reading order when picking up the project."

The third item has an implicit trigger (session start) but no explicit "fires when" clause per the naming convention. The first two have a trigger buried in "without explicit user request" but the positive condition for "when locked decisions may be revisited" is not named. ~200 chars.

### T10 — OR §Dispatch shape ("DISPATCH SHAPE. Implementers: model=sonnet...")

> "DISPATCH SHAPE. Implementers: `model=sonnet`, `effort=high`, `isolation=worktree`, `run_in_background=true`. Reviewers: `model=sonnet`, `effort=high`..."

No trigger for when to deviate from these defaults. The guidance is correct but doesn't say "fire when dispatching an implementer sub-agent." ~200 chars.

### T11 — ST §Anti-patterns A through G

Seven anti-patterns (~2,200 chars total) describe negative behaviors. Each names a forbidden action and occasionally its consequence, but most lack an explicit "fires when you notice X" trigger. Examples:
- "A. A daemon worktree executing a bead is NOT a crew working." (When do you check for this? The answer is implied but not stated.)
- "B. Never park on a single bead while lanes sit idle." (When is "parking on a single bead" diagnosed?)

These are valuable guardrails but are written as anti-patterns rather than triggers.

### T12 — captain-lanes.md stale LIVE FLEET STATE snapshot (2026-06-20 ~22:30)

> "⚠️ STALE (2026-06-20) — SUPERSEDED. As of 2026-06-24: daemon UP & healthy..."

This block (and the L1–L14 dispatch table, the "re-verified 2026-06-20" dispatch-ready table, and the "resolved since last reassessment" block) carry zero trigger logic. They are historical operational snapshots that read but never fire. Combined: ~8,000 chars (~2,000 tokens).

---

## 4. Ranked Cut-List

Ranking criterion: estimated token savings × safety (low-risk = safe to remove without altering behavior; moderate-risk = requires verifying no single surviving copy is missing). Savings are estimated chars/4.

| # | What to cut | Where | Est. tokens saved | Risk |
|---|---|---|---|---|
| **1** | Stale 2026-06-20 operational blocks in `captain-lanes.md`: the L1–L14 dispatch table, "LIVE FLEET STATE snapshot 2026-06-20 ~22:30" block (explicitly labeled STALE), "Dispatch-ready lanes (re-verified 2026-06-20)" sub-table, and "Resolved since last reassessment (do NOT re-open)" table | `captain-lanes.md` | **~2,000** | **LOW** — blocks are self-labeled STALE and superseded by the "⭐ CURRENT TRUTH" block above them |
| **2** | KNOWN-vs-brand-new definition re-statements in `captain/SKILL.md`: the three re-statements in §0 autonomous list, the R-C4.6 NORMATIVE block, §8 case-1 paragraph, and §A "PARKED is a fact" block — replace each with a one-line pointer to OR §Autonomy | `captain/SKILL.md` §0, §8, §A | **~875** | **LOW** — OR is the self-declared canonical; CS already says "Canonical: orchestrator-rules §Autonomy" in two of these spots |
| **3** | Keeper WARN 4-step procedure duplication between STARTUP.md and SKILL.md. Keep canonical in STARTUP.md §"When you receive a WARN"; replace CS §10 block with "On a WARN, run the STARTUP.md §On-WARN procedure." Also keep the terse-ack rule in one place only | `captain/SKILL.md` §10 (~800 chars) | **~425** | **LOW** — identical text; STARTUP is the checklist, SKILL is the contract. Cross-reference is already a pattern in the file |
| **4** | `AGENT_INDEX.md` §Problems, §Goals, §Concepts, §Ideas, §Foundation alignment, §Reviews tables (historical and largely static content not needed at implementer-orchestrator boot) | `AGENT_INDEX.md` | **~2,000** | **LOW** — these sections route to `docs/` sub-trees; agents can read on demand. The Skills and Operational Protocols sections are the boot-critical content |
| **5** | `harmonik subscribe` command block (4 copies). Keep in OR §Monitor pattern (canonical). Replace in HD, CL, and AC with a one-line pointer ("See OR §Monitor pattern for the subscribe command") | `harmonik-dispatch/SKILL.md`, `crew-launch/SKILL.md`, `agent-comms/SKILL.md` | **~225** | **LOW** — OR is canonical; command signature is stable |
| **6** | `STATUS.md` spec corpus inventory table (10-row table with versions and req-ID counts, §"Spec corpus inventory — current state (2026-04-25)") | `STATUS.md` | **~500** | **LOW** — historical, frozen ("ALL spec IDs ARE PERMANENTLY FROZEN"), never needed at runtime. Move to ROADMAP.md or docs/historical/ |
| **7** | `br ready --limit 0` full explanation block in `crew-launch/SKILL.md` §Operating loop §1 and `orchestrator-rules/SKILL.md` §Review and quality gates — replace with "See BC §Check available work (always `--limit 0`)" | `crew-launch/SKILL.md`, `orchestrator-rules/SKILL.md` | **~125** | **LOW** — BC is canonical |
| **8** | Sub-agent dispatch 3 exceptions duplicated in `harmonik-dispatch/SKILL.md` §When to NOT route through the daemon — OR is canonical; HD can reduce to "see OR §Dispatch discipline §THE THREE EXCEPTIONS" | `harmonik-dispatch/SKILL.md` | **~75** | **LOW** — direct dedup of OR; no change to behavior |
| **9** | `captain/SKILL.md` §11 "Where the future judgment layer plugs in" — pure forward-looking narrative about a feature not yet built | `captain/SKILL.md` §11 (~600 chars) | **~150** | **LOW** — no current behavioral content; future design belongs in docs/plans |
| **10** | `agent-comms/SKILL.md` §Monitoring daemon run events — 250-char cross-reference block that repeats OR §Monitor pattern and HD §Arm a Monitor; the two-sentence summary plus a pointer suffices | `agent-comms/SKILL.md` | **~63** | **LOW** — pure cross-reference section |
| **11** | `harmonik-dispatch/SKILL.md` §75% criterion and §References — the 75% tally has no enforcement mechanism and the references section names files agents already know to read | `harmonik-dispatch/SKILL.md` | **~125** | **LOW** — metric without correction; move tally note to docs/ |
| **12** | `captain/STARTUP.md` §Anti-patterns A–G — seven anti-patterns (~2,200 chars) are valuable but partly duplicate §0.2 "Forbidden wishy-washy failures" in CS and the Autonomy section in OR. Could be condensed from 7 paragraphs to a table (name + one-line signature) pointing to the canonical location where each is addressed | `captain/STARTUP.md` §Anti-patterns | **~550** | **MODERATE** — behavioral guardrails; condensation risks losing edge-case detail. Requires careful compression |
| **13** | `captain/SKILL.md` §0.5 "Boot Sequence" — CS §0.5 repeats the 6-step checklist already in STARTUP.md in summary form. The summary adds some value as a quick-reference but ~700 chars of it paraphrase STARTUP in detail | `captain/SKILL.md` §0.5 | **~350** | **MODERATE** — some readers use SKILL.md as standalone reference; reducing to a pointer changes the read order |
| **14** | `captain-lanes.md` operator directives with `expires:~2026-06-22` — four dated directives from 2026-06-19 (ONE-AT-A-TIME retired, scale-out, DOT triple-review mandate, captain orchestrates, ≤300k token watchdog) are past their expiry. Per the expiry mechanism, they should lapse to the standing autonomous posture | `captain-lanes.md` §Active operator directives | **~500** | **LOW** — expired; the `.harmonik/context/AGENTS.md` forced-read rule requires the admiral audit to flag and strike these |

### Cumulative savings estimate

| Risk tier | Items | Est. tokens saved |
|---|---|---|
| LOW (items 1–11) | 11 | ~6,563 |
| MODERATE (items 12–13) | 2 | ~900 |
| LOW — expiry-cleanup (item 14) | 1 | ~500 |
| **Total** | **14** | **~7,963** |

~8k tokens represents roughly a 19% reduction in captain cold boot context (excluding HANDOFF) if all LOW-risk items are cut. The single largest saving is the stale captain-lanes.md operational history (item 1, ~2,000 tokens) combined with the AGENT_INDEX.md static tables (item 4, ~2,000 tokens). These four cuts alone (items 1–4) yield ~5,300 tokens with no behavioral risk.
