# Startup-Doc Revamp — Synthesis of 8 Fresh-Context Audits

Date: 2026-07-11. Scope: everything any harmonik agent loads at boot — admiral, captain, crew,
watch, and daemon-dispatched implementer/reviewer beads.

## 1. Headline diagnosis

Two boot models are live at once, and **the retired one owns the entry point.**

- **NEW (shipped, working):** `harmonik agent brief --wake fresh|keeper-restart|trigger:<id>`
  emits the complete ordered boot document (identity/soul → wake reason → operating+skills →
  triggers → embedded handoff) from `.harmonik/agents/{admiral,captain,crew,watch,assessor}/`.
  ~1.5–2.5k tokens per role. Verified working for all four live roles.
- **OLD (rotted, still routed to):** role STARTUP runbooks + the AGENT_INDEX → STATUS →
  captain-lanes → HANDOFF reading ritual. `harmonik agent brief` appears **nowhere** in
  AGENTS.md, AGENT_INDEX.md, STATUS.md, or HANDOFF.md (grep-confirmed zero hits). AGENTS.md
  L34 still says "read `.claude/skills/captain/STARTUP.md` FIRST" and states the 4-doc reading
  order twice (L30, and as a hard "Don't skip" rule at L66).

An agent following the docs literally cannot discover the one-line boot. The operator's
observed failure — an agent reading the 910-line STARTUP.md plus the 604-line append-only
captain-lanes.md and never running the boot command — is the *prescribed* path, not a fluke.

**Cost:** old captain boot chain ≈ 1,700+ lines / 25–50k tokens (STARTUP 910 + captain-lanes
604 + SKILL.md §0.5 dependency + stale entry docs), of which ~4–5k tokens is genuinely live
content. The brief is ~116 lines. That is a ~10–20× per-boot overpayment, paid again on every
keeper restart.

## 2. The new model is not yet safe to converge on — 5 defects

Cutover order matters: these must be fixed **before** the old docs are cut, or we swap a
verbose-but-correct boot for a lean-but-wrong one.

1. **Watcher-set contradiction (behavioral, operator-locked).**
   `.harmonik/agents/captain/operating.md` Active-loop item 3 arms
   `subscribe --types epic_completed,run_failed,run_stale,heartbeat` — exactly the run-level
   telemetry STARTUP.md L588–609 forbids ("Arm EXACTLY the two watchers"; operator-flagged
   2026-06-11 context burn; also SKILL.md L221–230/L446–452). The two-watcher rule
   (`comms recv --follow` + `subscribe --types epic_completed`) wins; fix the manifest.
2. **Old model embedded inside the new.** captain/operating.md wake step 3 mandates the
   tier-file ritual including the 604-line captain-lanes.md and a `HANDOFF-captain.md` read the
   brief already embeds. Until captain-lanes is compacted, every "new-model" boot still pays
   the old tax.
3. **Renderer defects.** (a) Injected-skill short-desc is taken from the file's first line, so
   frontmatter'd skills render as `**agent-comms:** ---`. (b) `as: doc, presence: retrieved`
   refs (orchestrator-rules, orchestration-protocol-v2.md, kerf.md) are silently dropped from
   the rendered brief — booting agents cannot find their declared standing-rules doc.
4. **`_skills/` tree drift.** `.harmonik/agents/_skills/` is a hand-copied twin of
   `.claude/skills/`: crew-launch is a 1-line stub while the manifest calls it the
   "authoritative boot sequence" (empty authority); agent-comms is already 6 lines behind
   (missing the presence-refresh CRITICAL block). No sync mechanism → drift is structural.
5. **Smaller reconciles:** manifest zombie-reconcile says `br update --status open` where the
   proven procedure is `harmonik crew stop <name>`; escalation target differs (manifest:
   admiral; SKILL/STARTUP: operator — the admiral layer is newer, confirm admiral-first is
   intentional and make everything match); handoff section needs a "CLAIM, not ground truth —
   `harmonik digest` overrides" banner (current embedded captain handoff still claims the
   reaper P0 is unfixed though 95701ee9 shipped it).

## 3. Operational-state rot (append-only disease)

Same failure at three scales — supersession expressed as prose annotation instead of deletion,
with git already keeping history:

- **captain-lanes.md (604 lines, ~38k tokens):** lines 36–604 (~94%) are TWENTY-TWO stacked
  CURRENT TRUTH / directive blocks, each explicitly marked SUPERSEDED / LIFTED / STALE /
  expired. Line 62 declares "everything below this block is HISTORY" — 540 lines follow. Even
  the live top block (L12–34) is contradicted by the fresher admiral-initiatives (01:54Z
  "REDEPLOY IS DEAD/HELD" vs 04:26Z "flagship DONE, deployed + prod-canary-green on 59089968").
  No retention cap exists at all.
- **direction-log.md (125 lines):** violates its own header three ways — over its ~60-line cap;
  NOT newest-first (newest 07-11 01:54Z entry is at the BOTTOM, under three expired entries, so
  the "newest RETURN-PATH is ground truth" marker sits next to the wrong entry); entries run
  12–16 lines vs the declared 3–5. Three entries are expired and per the LAPSE rule should be
  struck.
- **admiral-initiatives.md (266 lines):** healthy through L49; L170–210 are tables its own
  header calls "STALE historical context"; L212–266 are six append-only audit-marker journals
  violating its own "one line per initiative" charter.
- **Crew scale — missions/kynes.md:** three stacked `## Prior … SUPERSEDED` Current-State
  blocks (L61–97) plus a dead GATE-0 re-task directive appended *inside* `goal` (L27–56).
  Mission goal/Current-State must be overwrite-only.
- **Entry docs:** STATUS.md frozen at 2026-06-20, telling booters to start work (hk-538l) that
  closed three weeks ago; generic HANDOFF.md body is a dead 2026-06-21 snapshot no manifest
  agent reads (they read `HANDOFF-<agent>.md`; 27 such files sit unindexed at repo root).

**Root cause of the rot:** the anti-rot rules exist (context/CLAUDE.md L47–55 assigns the
admiral audit ownership of striking expired entries; direction-log declares its cap) but
nothing mechanical enforces them. Enforcement gap, not rule gap — the fix is a checkable gate,
not another rule.

## 4. Duplication map

| Fact/rule | Copies today | Surviving owner |
|---|---|---|
| Autonomy boundary (KNOWN vs brand-new, surface-and-await) | orchestrator-rules §Autonomy, captain SKILL.md ×5 (desc, §0, R-C4.6, §0.2, §8), STARTUP ×5, manifest soul+bounds | **orchestrator-rules §Autonomy** (declared canonical); manifest Bounds carries the one-line pointer |
| Boot sequence | STARTUP.md, SKILL.md §0.5, AGENTS.md load map ×2, AGENT_INDEX L7, STATUS L107–112, crew-launch L68–173, manifest brief | **`harmonik agent brief`** (everything else points) |
| 5-lane operator priority order | captain-lanes L22–34, direction-log L14–30, lanes.json notes, admiral-initiatives L25–48 | **lanes.json** (machine state) + **admiral-initiatives** (big-rocks registry); prose files point. This is the "4th priority list" context/CLAUDE.md L57 forbids |
| Keeper band values (200000/215000) | STARTUP ×4, SKILL.md ×2 | **launcher defaults / config.yaml** (pointer only; standing no-hardcoded-thresholds rule) |
| Monitor pattern / stream-vs-wave / daily-loop CLI | orchestrator-rules L37+L48–50+L194–205, harmonik-dispatch, AGENTS.md | **harmonik-dispatch** (AGENTS.md-declared owner); orchestrator-rules keeps the ≥75% RULE + one-line pointer |
| Reviewer output contract | agent-task.md builder, review-target.md builder, pasteinject seed | **one shared Go renderer**; seed text (hk-9w79a, `write-review-verdict`) is newest and wins — the two builders still teach the BANNED hand-write-review.json-via-mv |
| Bead lifecycle prohibition | beads-cli, agent-task.md ×2, project CLAUDE.md (INVERTED — teaches `br update --status=in_progress`/`br close` to every bead) | **agent-task.md inline** (what beads reliably read) + beads-cli; CLAUDE.md workflow section rewritten "solo operator only" |
| Skill bodies | `.claude/skills/` vs `.harmonik/agents/_skills/` (agent-comms drifted, crew-launch stub) | **`.claude/skills/`** canonical; `_skills/` becomes generated mirror w/ drift check |
| Park/wake protocol | STARTUP L865–910, crew-launch L254–306, specs/park-resume-protocol.md | **the spec**; skills keep thin digest + pointer |
| Pi stdin forensics | captain-lanes L13–17, direction-log L110–125, bead hk-y20d2, comms, memory | **the bead** |
| Presence-refresh ≤90s rule | manifest Bounds ONLY (missing from .claude agent-comms and SKILL.md) | backfill into agent-comms skill — the one rule that must be COPIED before deletion |

## 5. Target information architecture

Three layers, one command:

- **BOOT (injected):** `harmonik agent brief` output only. Soul = identity + escalation.
  Operating = wake/loop/bounds. Embedded handoff = last session's CLAIM (digest overrides).
- **CONTRACTS (retrieved on demand):** orchestrator-rules (behavior canon, ~150 lines),
  role SKILL.md (mechanics reference, ~150–200 lines), detail-owner skills
  (harmonik-dispatch, agent-comms, beads-cli, keeper, harmonik-lifecycle), specs/.
- **STATE (read as data, bounded, replace-in-place):** tier-3 project.yaml (locked, healthy),
  tier-2 lanes.json (authoritative registry) + captain-lanes.md (≤60-line snapshot) +
  direction-log.md (≤60 lines, newest-first) + admiral-initiatives.md (≤50 lines),
  tier-1 = the brief's embedded handoff. Ground truth is always `harmonik digest`, which
  overrides every document claim.

Per-role boot loads:

| Role | Boot = | Then reads (data) | Pulls on demand | Never loads |
|---|---|---|---|---|
| **Admiral** | `harmonik agent brief` | admiral-initiatives.md, `harmonik digest` | orchestrator-rules, kerf.md, harmonik-lifecycle | STARTUP.md, captain-lanes history |
| **Captain** | `harmonik agent brief --wake <reason>` | project.yaml → lanes.json/captain-lanes (compacted) → direction-log; ONE `harmonik digest` (ground truth, overrides all claims incl. embedded handoff) | orchestrator-rules, captain SKILL.md (mechanics), keeper, harmonik-dispatch, docs/daemon-redeploy.md | AGENT_INDEX, STATUS, docs/ KB, 910-line runbook |
| **Crew** | `harmonik agent brief` | its mission file (frontmatter + ONE Current State block) | crew-launch reference (~150 lines: park/wake, invalid handoff, await-ack), agent-comms, beads-cli, harmonik-dispatch | ALL fleet-level state (ROADMAP, lanes, project.yaml, orchestrator-rules, STATUS, HANDOFF) |
| **Watch** | `harmonik agent brief` | — (bus-driven) | watch skill | fleet docs |
| **Bead (impl/reviewer)** | daemon-injected **agent-task.md** (+ seed paste / review-target.md) — the bead-tier brief; beads do NOT run `agent brief` and do NOT follow AGENTS.md boot routing (state this explicitly) | — | beads-cli, agent-reviewer | orchestrator CLAUDE.md workflow sections, fleet skills, reading-order ritual |

Wake variants replace the lifecycle soup: `--wake fresh` = cold boot; `--wake keeper-restart` =
the LEAN resume (re-drain comms → tier reads + ONE digest → trust cached tier state → re-arm
watchers, folded from STARTUP L550–586); `--wake trigger:<id>` = park-exit.

**Freshness tiebreak:** every state doc carries `updated:`; newest stamp wins on conflict;
`harmonik digest` beats all documents. (Today the tier order puts the STALEST truth first.)

## 6. Keep / Cut / Move

Legend: KEEP = survives in place · MOVE = survives elsewhere (target noted) · CUT = delete.

### captain/STARTUP.md (910 → ~20-line stub)
| Content | Verdict |
|---|---|
| Identity/two-captains collision guard (L47–57) | MOVE → manifest operating.md |
| Direction-log LAPSE-to-autonomous rule (L64–113) | MOVE → operating.md wake step (nowhere else) |
| Single-pass digest rule (L159–174) | MOVE → operating.md, as `harmonik digest` (native verb) |
| Daemon-down exit-17 recovery (L204–217) | MOVE → captain SKILL.md edge table / docs/daemon-redeploy.md |
| Crew classification table + collision guard (L221–253) | MOVE → captain SKILL.md |
| Staffing discipline: lazy-boot/PARKED gate, model tiering, 5a–5d order, 2-min stagger, re-task-not-restart (L286–345) | MOVE → captain SKILL.md |
| Two-watcher rule + run-telemetry prohibition (L588–609) | MOVE → operating.md (and FIX the manifest that contradicts it) |
| §4.3 liveness sweep, wedge shapes, clear-and-retype (L665–699) | MOVE → captain SKILL.md (unique, incident-proven) |
| LEAN keeper-restart resume (L550–586) | MOVE → operating.md `--wake keeper-restart` variant |
| Keeper cheatsheet essentials, ~10 lines (L430–454) | MOVE → operating.md bounds + keeper skill pointer |
| Healthy-fleet definition + comm −23 one-liner (L828–861) | MOVE → captain SKILL.md, compressed ~15 lines |
| Park/wake (L865–910) | CUT → pointer to specs/park-resume-protocol.md §3.3/§4.1 |
| Idle-crew pane-nudge (L747–750) | MOVE → captain SKILL.md |
| Origin war-story (L22–26); all M#/ES#/hk-#/Lever# audit tags; retired-tool eulogies (L451–453, L479, L484); script-missing fallback (L172–174); 5d(c) review-bypass jq loop (L360–380, ops-monitor owns it); M11 warn-text check (L522–533, keeper doctor owns it); SLIM-boot fences (L124–148); hardcoded 200k/215k ×4; autonomy restated ×5; verbatim WARN text + restart-now duplication | CUT |
| 8 anti-patterns | 3 survive (worktree≠crew; never serialize fleet behind one bead; stale presence≠live crew) MOVE → captain SKILL.md; rest CUT (owned elsewhere) |

### captain/SKILL.md (842 → ~200-line mechanics reference)
| Content | Verdict |
|---|---|
| §2 spawn mechanics + exit-code table (L309–353) | KEEP, rewritten to native `harmonik start crew --name/--queue/--mission` (D2 grammar) |
| §3 mission-handoff 6-field schema + no-session_id (L356–408) | KEEP (only captain-side authoring home; spec: specs/crew-handoff-schema.md) |
| §4 mail/re-task `--topic assign`, re-task ≠ re-spawn (L412–433) | KEEP |
| §5 Gap-1 `--assignee` attribution mirror (L454–475) | KEEP |
| §8 guardrails: never pre-assign, epic-only assignee, br-close exception + promote exception, read-only reviewers (L560–591) | KEEP (also §8's four-case surface-and-await list survives as the ONE autonomy statement here) |
| §9 attribution-first + edge table (L599–615) | KEEP |
| §10 re-arm recv after /clear; restart-now; await-ack; never self-terminate; terse-ack (L660–763) | KEEP, condensed |
| ON-060 RE-CLASSIFY rule (L771–786) | KEEP (or MOVE → orchestrator-rules §Autonomy) |
| §0.5 boot sequence mandating STARTUP.md (L199–243) | CUT — the competing boot model |
| 19-line frontmatter description | CUT → one sentence |
| §0 + R-C4.6 + §0.2 autonomy restatements (L56–197) | CUT (orchestrator-rules + manifest own it) |
| `--to operator` escalation throughout | FIX → admiral per manifest (confirm intent) |
| Hardcoded keeper band ×2 (L262–264, L793–794) | CUT |
| §11 future-judgment-layer (L804–821); tombstones (L730–733, L759–763); provenance tags; §0.1 consensus gate (L151–176, → 3 lines in operating.md if kept); stream-vs-wave note (L623–629, harmonik-dispatch owns); plans/-era sources list | CUT |

### orchestrator-rules/SKILL.md (249 → ~150 lines)
| Content | Verdict |
|---|---|
| Role split, dispatch HARD RULEs, kerf-first, pre-screen/pre-flight, bead lifecycle, batch-failure protocol, §Autonomy (VERBATIM — the canonical home), pre-deploy e2e gate (4 steps), review gate, never-cd-into-worktree, run_stale discriminator, planning-artifact placement table, provenance section | KEEP |
| Frontmatter "STARTUP Step 1.3 / session-resume" wiring | FIX → "retrieved doc per .harmonik/agents/*/manifest.yaml" |
| L219 pointer into STARTUP §4.3; L151 "STARTUP already says this" | FIX → point at surviving homes / state rule directly |
| L60 DOT-endgame, L62 kerf-beta status, L58 phase2 label | MOVE → project.yaml/ROADMAP (violates its own no-state charter) |
| §Monitor pattern + daily-loop CLI + stream/wave detail | CUT → one-line pointers to harmonik-dispatch (declared owner) |
| E2e-gate incident narrative; L157 memory-footnote; L44 run --beads mechanics | CUT |
| ~12 HARD RULE tags | DEMOTE all but ~5 genuinely inviolable |

### crew-launch/SKILL.md (609 → ~150-line retrieved reference)
| Content | Verdict |
|---|---|
| Park/wake + park-exit vs disconnect discrimination (L254–306); invalid-handoff fallback (L576–592); restart await-ack (L494–516); two-failures-then-escalate | KEEP (the retrieved detail) |
| Boot sequence + operating loop + MUST/MUST-NOTs (L68–173, 310–397, 534–573) | CUT — `.harmonik/agents/crew/operating.md` is the surviving (better, 10× shorter) statement |
| ≤90s presence-refresh rule | BACKFILL from manifest Bounds into agent-comms skill before any deletion |
| "§How you were launched" D2/D3 (L42–64) | MOVE → captain SKILL.md / CLI help |
| HK_PROJECT python3 STATUS_TARGET snippet (L411–420) | CUT → native harmonik resolver (HK_PROJECT is RETIRED; py3-in-shell landmine) |
| crew-boot-digest.sh ritual (L75–84) | CUT → fold into `--wake keeper-restart` output or demote to troubleshooting |
| sources/References planner archaeology | CUT |

### State tier (.harmonik/context/ + admiral-initiatives)
| Content | Verdict |
|---|---|
| project.yaml (28 lines) | KEEP; fix "STARTUP Step 0a" refs → manifest |
| context/CLAUDE.md meta-contract | KEEP; extend retention rules to captain-lanes; fix boot refs |
| lanes.json | KEEP; PROMOTE to authoritative lane registry (update FIRST, prose second) |
| captain-lanes.md L12–34 | KEEP, reconciled against admiral-initiatives 04:26Z; pi forensics → bead pointer |
| captain-lanes.md L36–604 | CUT (git is the archive) + new ≤60-line replace-in-place header contract |
| direction-log header contract + 3 unexpired 07-11 entries | KEEP, deduped, newest-first, 3–5 lines each (~25 lines total) |
| direction-log expired entries (L64–108) | CUT per its own LAPSE rule |
| admiral-initiatives L1–49 (E2E gate, charter, ★★ priority table, deprioritized) | KEEP — becomes the whole file |
| admiral-initiatives L53–169 program narrative | MOVE → plans/2026-07-06-quality-system/ |
| admiral-initiatives L170–266 stale tables + audit markers | CUT (audits report over comms/handoff) |
| roadmap.md (never-filled template) | CUT (or fill; drop from any read list until real) |
| missions/kynes.md L27–56 dead GATE-0 in goal + L61–97 Prior blocks | CUT; missions become overwrite-only (goal rewritten on re-task; exactly one Current State block) |

### Entry docs
| Content | Verdict |
|---|---|
| AGENTS.md Precedence, launch verbs + D2 rule, Key conventions, Don't 1–2, skill/doc pointers, beads/kerf sections | KEEP |
| AGENTS.md Per-role load map (L13–26), "read STARTUP.md FIRST" (L34), reading order (L30, L66) | CUT → ONE boot section: "Booting as a manifest agent? Run `harmonik agent brief`. Its output IS your complete boot context." + explicit "beads: agent-task.md IS your boot; ignore this map" |
| AGENTS.md/CLAUDE.md §Workflow Pattern `br update --status=in_progress`/`br close` | FIX → "daemon-dispatched agents: NEVER; solo operator only" (currently poisons every bead) |
| AGENT_INDEX.md KB map + skills directory | KEEP as on-demand map; CUT L7 boot-order banner; fix dup Reviews section |
| STATUS.md decisions-in-force + frozen-spec-ID rule | MOVE → project.yaml / AGENT_INDEX; rest of STATUS.md CUT (stale, self-contradicting) — retire or gut to pointers |
| HANDOFF.md body | CUT → 5-line tombstone pointing at HANDOFF-<agent>.md + the brief; add retention/home rule for the 27 root HANDOFF-*.md files |

### Manifest tree + bead surface
| Content | Verdict |
|---|---|
| Brief section order, soul.md files, boot/SKILL.md, operating.md structure, persistent:true flags, --override guard, injected/retrieved distinction, crew scoping line, digest-overrides rule | KEEP (the target model) |
| captain/operating.md 4-type subscribe; `br update --status open` zombie reconcile; HANDOFF-captain.md read in step 3 | FIX (see §2) |
| soul "I do NOT" items duplicated in Bounds; operating.md "Skills I use" list (manifest generates one) | CUT duplicate leg |
| `_skills/` hand copies | FIX → generated mirror of .claude/skills + drift check (re-sync agent-comms, real crew-launch body first) |
| agenttask_chb028.go L356–363 + L570–576 hand-write-review.json instructions | FIX (code) → `write-review-verdict` mandate via ONE shared renderer for all three injection points |
| agent-reviewer skill: "five checks"→eight, missing `incomplete-coverage` flag, pre-commit→commit-msg hook, schema-in-frontmatter, missing self-review-vs-daemon-reviewer scope header | FIX |
| beads-cli jq examples + version trivia; harmonik-dispatch self-cancelling caveats, incident refs, manual tmux/$HARMONIK_PROJECT recipe | CUT |

## 7. What must be preserved verbatim (loss risks)

The compaction's failure mode is deleting a hard-won rule that exists in only one place. The
single-copy rules to carry forward explicitly: direction-log LAPSE-to-autonomous; two-watcher
prohibition; §4.3 wedge shapes + clear-and-retype recovery; Gap-1 assignee mirror;
never-pre-assign; the br-close exception + promote exception; presence-refresh ≤90s (exists
ONLY in manifest Bounds today); park-exit vs disconnect discrimination; invalid-handoff
fallback; never-self-terminate-on-WARN; `br ready --limit 0`; digest-overrides-handoff;
reviewer `write-review-verdict` mandate; daemon-owns-terminal-transitions.
