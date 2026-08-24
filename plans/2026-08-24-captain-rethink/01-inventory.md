# Step 1 — Inventory, measurement, and the record

Everything below is measured, not estimated, except where a row says otherwise. Nothing was
edited to produce it.

---

## 1. How text reaches a captain — three mechanisms, and only three

| # | Mechanism | What it injects | Controlled by |
|---|---|---|---|
| M1 | Claude Code auto-load | The project `CLAUDE.md`, a symlink to `AGENTS.md` (23,232 B), plus the global `~/.claude/CLAUDE.md` (2,094 B). A folder-scoped `AGENTS.md` inside `.harmonik/context/` (3,697 B) loads only when the session touches files there. | the harness, not harmonik |
| M2 | `harmonik agent brief` | Full text of `roles/captain/soul.md` and `operating.md`, full text of a `HANDOFF-captain.md` if one exists, and **one line per context entry** — a name, a description, an absolute path. Never a body. | `.harmonik/agents/captain/manifest.yaml` |
| M3 | The captain reading files, because `STARTUP.md` tells it to | The three tier files, `STARTUP.md`, `SKILL.md`, `orchestrator-rules/SKILL.md`, and the boot-digest output | `STARTUP.md` |

**Hooks inject no text.** The project hooks run `harmonik hook-relay`, which emits a bus
event and returns no additional context. The global hooks only write a keeper session id.

**Skill bodies are never injected — only their frontmatter descriptions.** Confirmed by
comparing this session's skill listing against the files: the captain entry is byte-identical
to the description block in `SKILL.md` frontmatter, and the 21,607-byte body is absent.
Supporting files such as `STARTUP.md`, `SHUTDOWN.md`, and `REFERENCE.md` are not registered
at all.

**This is the most important structural fact in the inventory.** The cost is not the file.
The cost is the instruction to open the file. Deleting a "read this at boot" line saves
exactly what deleting the file saves, and costs nothing in lost knowledge.

**A related trap: `presence: injected` in a manifest does not inject anything.** The brief
emits one pointer line per context entry whatever the presence value says. The manifest's own
comment admits this. Budget a manifest entry at about 250 bytes, not at the size of its
target.

---

## 2. The boot budget — measured live, and the first attempt was wrong by half

**A captain starts at roughly 110,000 tokens before the Step 1a cut, and roughly 74,000
after it.** Not the 48,500 this section first reported.

The first figure summed file sizes from disk and divided bytes by four. Both halves were
wrong, and they compounded.

### What the disk sum could not see: a 39,000-token floor

Claude Code writes a transcript per session under `~/.claude/projects/`, and the first
assistant message of a session carries its usage accounting. Summing `input_tokens`,
`cache_creation_input_tokens` and `cache_read_input_tokens` on that first turn gives the
whole context before the agent has read anything. Every live crew was keeper-restarted on
2026-08-24, so all three are fresh boots:

| Agent | First-turn total |
|---|---:|
| charlie | 37,924 |
| charlie (earlier restart) | 39,005 |
| alpha | 39,111 |
| bravo (in a worktree) | 35,382 |

Each session's first user message is a single 94-character command, so that is essentially
pure overhead. **The floor is about 39,000 tokens at the repo root and 35,400 in a
worktree, and it is identical for a captain, a crew, and an oversight session** — same
binary, same repo, same auto-injected files.

This accounting is the one the keeper already uses: `internal/keeper/gauge.go` reads a
per-agent context file written from Claude Code's own total. Cross-checked at a single
moment — the gauge said 79,221 and the transcript sum said 79,079.

Where the floor goes, from a live `/context` in this repo:

| Category | Tokens |
|---|---:|
| System tools | 11,800 |
| System tools, deferred | 9,100 |
| Base system prompt | 3,600 |
| Connected tool servers | 320 |
| Custom agent definitions | 270 |
| **Harness subtotal** | **~25,100** |
| `AGENTS.md` (8.8 K) + global instructions + memory index | 8,900 |
| Skill descriptions, 38 of them | 5,000 |
| **Auto-injected project text** | **~13,900** |

**The harness was estimated at 4,000 and is really about 25,100 — a 21,100-token miss, and
21,000 of it is tool schemas.** The base system prompt really is small. Connected tool
servers are negligible.

### The second error: this repo does not tokenize at four bytes

No tokenizer is installed here, so the ratio was measured against the live tokenizer by
injecting known-size payloads and reading the usage delta:

| Text | Bytes | Tokens | Bytes/token |
|---|---:|---:|---:|
| `harmonik agent brief` output | 15,391 | 6,154 | 2.50 |
| Boot-digest bead rows | 20,000 | 8,140 | 2.46 |
| `captain/STARTUP.md` + `SKILL.md` prose | 20,000 | 7,729 | 2.59 |
| A whole crew boot, including tool envelopes | 50,383 | 23,871 | 2.11 |

**About 2.5, not 4.** Path-heavy, identifier-heavy markdown tokenizes far worse than plain
prose, which is why `AGENTS.md` costs 8,800 tokens rather than the 5,800 first claimed.

### The captain, rebuilt

| Item | Tokens |
|---|---:|
| Harness floor, measured | 39,000 |
| `harmonik agent brief` | 2,757 |
| The three context tier files | 3,059 |
| `.claude/skills/captain/STARTUP.md` | 8,729 |
| `.claude/skills/captain/SKILL.md` | 8,323 |
| `orchestrator-rules/SKILL.md` | 2,016 |
| Boot digest, **before** Step 1a | 36,848 |
| The captain's own boot thinking and tool calls | 8,000–19,000 |
| **Before Step 1a** | **~110,000** |
| **After Step 1a** | **~74,000** |

A crew, measured directly rather than derived: charlie went 37,924 → 69,018 over a
two-and-a-half-minute boot; alpha 39,111 → about 60,042.

### The three numbers that should govern the rest of this plan

**1. Step 1a was worth about 35,400 tokens, not the 21,300 first reported.** The bead
listing and kerf map were 85 KB of digest at 2.5 bytes per token, not at 4.

**2. The harness floor is larger than the entire captain instruction corpus.** 39,000
tokens arrive before any harmonik file is read, and `STARTUP.md` + `SKILL.md` +
`orchestrator-rules/SKILL.md` together are about 19,000. Nothing in Steps 2 or 3 can touch
the floor. Two of its parts are reachable, though, and neither is an instruction-cutting
problem: 21,000 tokens of tool schemas, and the 8,800 that `AGENTS.md` costs every agent.

**3. So the honest ceiling for the corpus cut is about 14,000 to 19,000 tokens.** Deleting
the whole captain corpus saves 19,000. Cutting it to four pages of roughly 12 KB saves
about 14,000. That is worth doing and it is not where the remaining bulk is. **Say so
rather than letting the plan imply otherwise.**

## 2b. Formatting — the path hypothesis, tested and mostly refuted

The idea was that path-heavy markdown tokenizes badly, so presenting paths as a tree with
a shared prefix factored out would be cheaper. It was measured against the live tokenizer,
same 53 path-and-description pairs in nine presentations. The control — an empty payload —
came out at exactly 0 tokens on six of six runs, and every repeated payload reproduced to
the token, so these are point measurements of a deterministic tokenizer rather than samples.

**Paths are not what makes this corpus dense.** The whole 72 KB contains only 62 mentions of
a path with two or more slashes, worth about 1,100 tokens — four per cent. Only 16 of those
are repeat mentions, so factoring shared prefixes has about 200 tokens to play for. A tree
beat the same list unfactored by 70 tokens on 53 entries: **1.3 tokens per entry.** The
indentation itself is free. It is not a lever.

**What is actually dense is code-formatted text.** The 228 backticked spans in `AGENTS.md`
are 16.9 % of its bytes and 22 % of its tokens, and cost **exactly twice** what the same
byte count of prose costs. Commands, flags, identifiers, error codes — not directory names.

**A backtick costs 0.95 tokens**, confirmed three independent ways. The corpus holds 1,306
of them, so about 1,245 tokens, 4.6 %. Bold markers cost 1.0 token each, 416 in total.

**Total available from every formatting change combined: about 1,660 tokens of 27,270 —
6.1 %.** Not worth a blanket rule against the readability cost.

**The one change worth making** is narrower: stop backticking things that are not code. A
large share of those 228 spans are bare skill names and plain nouns — assessor, captain,
lane, main, queue, boot, crew. Each pair costs about two tokens and buys no disambiguation.
That is a few hundred tokens at no cost to a reader, where stripping ticks from genuine
paths would save 1,245 and hurt.

### One measured fact that changes the governance step

**Bytes are the wrong unit for a size budget.** An em dash costs two extra bytes and
**zero** extra tokens — `runbook — boot order` and `runbook - boot order` measure the same.
A backtick costs one byte and one token. So a byte budget punishes punctuation that is free
and ignores markup that is not. **The size check in Step 8 should count tokens or lines, not
bytes**, or it will push authors toward exactly the wrong edits.

Two other small findings: a table costs 3.8 % more than the same content as bullets, and a
bead identifier costs about seven tokens more than an English handle of the same length —
but only six such mentions exist corpus-wide, so keep discouraging them for readability
rather than for budget.

---

## 3. Duplication — 25 rules stated in two or more places

Within the roughly 66 KB the captain deliberately loads, restated rules account for an
estimated **12 to 15 KB of second and third copies**. `STARTUP.md`, `SKILL.md`, and
`SHUTDOWN.md` restate each other on: name collisions, the daemon-down exit code, the
ops-monitor's queue blindness, wedge recovery, the two-watcher rule, assignee mirroring, what
parked means, priority, and leaving the harness unset.

Each of those files carries an explicit line saying it does not hold a second copy. The
disclaimers are accurate about *ownership* and wrong about *text*.

1. **Never change directory into a worktree; work from the repo root** — restated in
   `orchestrator-rules/SKILL.md`, `captain/STARTUP.md`, `roles/captain/operating.md`,
   `crew-launch/SKILL.md`; pointed at from `orchestrator-rules/REFERENCE.md`, `AGENTS.md`,
   `AGENT_INDEX.md`. Seven places.
2. **Deduplicate every delivered message on its event id** — `agent-comms/SKILL.md` (the
   normative home), `captain/SKILL.md` **three separate times**, `captain/STARTUP.md`,
   `crew-launch/SKILL.md`, `watch/SKILL.md`.
3. **Exit code 17 means the daemon is down; local reads still work** — `captain/STARTUP.md`
   twice, `captain/SKILL.md` twice, `captain/SHUTDOWN.md`, and the boot-digest script.
4. **A queue paused by failure needs recover, not resume** — six places: three captain files,
   the dispatch skill, the orchestrator reference, the crew skill.
5. **The ops-monitor's paused-queue check is unreliable, so sweep the queue list yourself** —
   the *same multi-sentence explanation* in `STARTUP.md`, `SKILL.md`, and `SHUTDOWN.md`. The
   longest triplicated idea in the surface.
6. **A lane recorded in any durable document is known and self-authorized** — nine places:
   both orchestrator-rules files, both captain files, the folder-scoped `AGENTS.md`, a JSON
   documentation field, the captain's soul file, both admiral files.
7. **Priority is stated intent first, then the ledger** — ten places, despite one declaring
   itself canonical and another declaring it carries no second copy.
8. **Parked is a fact about the backlog, not a hold** — six places, twice within `STARTUP.md`.
9. **Assign the epic, never a dispatchable child bead** — five places.
10. **The daemon owns terminal transitions for what you submitted; you close by hand what you
    worked by hand** — five places.
11. **A crew orchestrator cannot run on Codex; leave the harness unset** — three places,
    naming the same symbol and quoting the same error string.
12. **How to tell a wedged crew from a slow one, and how to re-drive its pane** — four places.
13. **Exactly two watchers; no run-level subscribe** — three places.
14. **A name or queue collision means the lane is already staffed** — four places, twice
    within `SKILL.md`.
15. **Attribute completion from the epic's assignee field, not from the crew list** — five
    places.
16. **The escalation chain is captain, then admiral, then operator** — three places.
17. **A handoff is a claim; live state wins** — six places, one of which is a Go constant
    printed into every brief.
18. **The boot read order for the three tier files** — eight places: the runbook, the
    folder-scoped `AGENTS.md`, the root `AGENTS.md`, and a tier header inside each of four
    context files.
19. **The captain does not boot-read the index, the status file, the principles, or the
    charter** — five places, three of them inside `AGENTS.md` alone.
20. **The "this file is generated output, mirror it" banner** — verbatim in 13 skill files
    under `.claude/skills/` and 13 more under the embed source. About 8 KB of pure banner
    across the skill set, of which about 1.9 KB lands in captain boot context.
21. **Keeper warning handling: acknowledge in one line, keep working, restart at a clean
    point** — five places.
22. **Daemon restart and redeploy is self-authorized** — four places.
23. **Write the mission file before starting the crew** — five places, one with a full example.
24. **A free slot beside ready work is a defect** — four places.
25. **Presence timeout is about two minutes, so a drop from the roster is staleness, not
    death** — five places.

Two more: a zombie-detection shell one-liner appears verbatim in both `STARTUP.md` and
`SHUTDOWN.md`, and the rule against hand-searching the event log by run identifier is in both
`orchestrator-rules/SKILL.md` and `major-issue-fanout/SKILL.md`, each presented as that
file's most important rule.

---

## 4. The three copies of a shipped skill

All comparable files are **byte-identical today**. The embed source under
`cmd/harmonik/assets/skills/` and the working copy under `.claude/skills/` agree on all
thirteen files. The third location, `.harmonik/agents/_skills/`, holds only five files, and
the four it shares agree with both other copies.

**The documented hazard does not currently bite the captain.** Reference resolution
short-circuits on any reference containing a path separator, and the captain manifest names
its captain documents by path. The hazard bites bare-name references only — comms, dispatch,
and beads.

**One file is unguarded.** `.harmonik/agents/_skills/boot/SKILL.md` exists in exactly one
copy, has no embed source and no working copy, and is the only skill body a manifest-launched
captain receives for free. Nothing verifies it.

A fourth home for load-map prose exists: `AGENTS.template.md` under the embed assets,
13,924 B, differing substantially from the live 23,232-byte `AGENTS.md`. Template versus
instance, so the difference is expected — but it is another place the same rules are written.

---

## 5. Instruction that is loaded and wrong

Size is not the only problem. Two of the three tier files the captain reads as ground truth
at boot are stale:

- `captain-lanes.md` announces itself as current truth as of 2026-07-22 and names five crews
  that no longer exist.
- Every entry in `direction-log.md` is past its own stated expiry.
- `lanes.json` is empty and says the fleet is torn down, contradicting both.

`.harmonik/context/captain-monitor.md` is orphaned instruction text — a scheduling prompt
naming crews that are gone, in the folder the captain reads, referenced by no boot path.

One pointer is a trap: `admiral-initiatives.md` is 76,158 bytes and is named as a durable
source of known lanes. A captain that follows that pointer and reads the file has spent about
19 K tokens.

---

## 6. `.memory/` — 660 KB, and nothing reads it

238 markdown files, 1.2 MB on disk, 660,652 bytes of prose. **Auto-injected total: zero.** No
skill, manifest, hook, or settings file references the folder; it is gitignored and
machine-local; the skills auto-load directory list is empty.

It reaches context only when an agent opens it deliberately, and exactly two files invite
that: the orchestrator reference names one memory file in order to *correct* it, and
`project.yaml` cites another by filename in a comment. A captain that follows either pointer
opens an unindexed 660 KB corpus.

Not a context cost today. Worth an operator decision on its own.

---

## 7. What five previous efforts found

| Date | Effort | Landed? |
|---|---|---|
| 2026-06-16 | Boot audit — batch the boot round-trips, cut the runbook to a checklist | Batching landed; **the length cut was explicitly deferred** |
| 2026-06-20 | Captain economy — five investigators, two adversarial reviewers, twelve issues | Four beads merged |
| 2026-06-20 | Doc-instruction audit — three-kinds load model, `AGENTS.md` as a router, tiered context | Shipped |
| 2026-06-23 | Wake economy — reframe from bytes read to wakes taken; a cheaper watch tier | Partly landed |
| 2026-07-11 | Startup revamp — `harmonik agent brief` as the one generated boot document | **Drafted only. The router flip never happened.** |
| 2026-08-23 | The cut — straight deletion across the whole shipped corpus | Landed |

### The growth curve

Combined bytes of `STARTUP.md` and `SKILL.md`:

```
2026-06-09    19,484   captain skill authored
2026-06-10    47,549   +27,131   boot runbook plus two "load-bearing lessons"
2026-06-20    80,038    +5,793   "lean boot + de-conflict"  <- a REDUCTION commit that ADDED 5.8 KB
2026-06-25    98,647    +5,884   captain-as-engine
2026-06-30   103,015    +3,959   crew process-liveness sweep
2026-08-12   120,744   +13,707   "rules become principles"
2026-08-23    41,959   -78,785   THE CUT
2026-08-24    43,811    +1,852   one stopped-queue incident, one day later
```

### Four accretion mechanisms, each visible in a commit

1. **A consolidation pass grows the file it was meant to slim.** The June de-duplication
   commit moved duplicated text *into* the captain files and net-added 5,793 bytes.
2. **Converting rules to principles is itself the largest single addition.** Plus 13,707
   bytes, and the commit body says why: rules "kept their force and gained their reason." A
   rule with its reasoning attached is three to five times the length of the rule.
3. **An incident adds a row plus its full mechanism.** The only post-cut growth is exactly
   this shape: about 1,100 bytes of explanation for one stopped queue.
4. **The mirror multiplies it by three.** That 1,100-byte addition became 2,807 bytes across
   the captain files. In one day the corpus recovered 3.5 % of what the cut removed. At that
   rate it is back to pre-cut size in about a month.

### The diagnosis of record

From the July synthesis: the anti-rot rules already exist, but nothing mechanical enforces
them. **"Enforcement gap, not rule gap — the fix is a checkable gate, not another rule."**

And the reason nobody deletes, also named there: "The compaction's failure mode is deleting a
hard-won rule that exists in only one place."

### Twelve operator rulings already made

`plans/2026-07-11-captain-startup-revamp/03-operator-decisions.md` holds them, under a
governing instruction from the operator: **"This is where we need PRINCIPLES NOT RULES"** —
any closed category list or checklist is a smell to fix.

The load-bearing ones: escalation is a principle and not a closed list; the captain talks to
the admiral rather than the operator; a crew-start collision fails fast and loud with no
auto-rename; all four relocated startup rules stay; an idle slot beside ready work is a
defect; the captain listens for exactly two event kinds at boot; nine rules keep their hard
tag; two soften to plain rules with the behaviour kept; the throwaway canary is a
recommendation; friction-gets-priority keeps its behaviour.

One is still open in the operator's own words — whether an always-on watch earns its keep. It
stays open here.

---

## 8. Where the leverage is, in order

| Rank | Change | Saving | Cost to make it | State |
|---|---|---:|---|---|
| 1 | Take work discovery out of the boot digest | **~35,400 tok** | one section of shell | **DONE** |
| 2 | Trim the tool schemas the harness injects | up to ~21,000 tok | not an instruction problem at all — unexamined | open |
| 3 | Stop the captain reading `STARTUP.md` and `SKILL.md` at boot — the router flip | ~17,000 tok | the flip nobody made | Step 4 |
| 4 | Cut `AGENTS.md` to a real router | ~6,000 tok, **times every agent** | a careful pass | Step 5 |
| 5 | Delete the 25 duplicated rules | ~5,000–6,000 tok | a careful pass | Step 2 |
| 6 | Fix or delete the stale tier files | ~3,200 tok | judgement, and it fixes wrong instruction too | **DONE** |

Item 2 is the surprise and nobody has looked at it. 21,000 tokens of tool-schema text
arrives in every agent in this project, which is more than the whole captain corpus, and it
is a harness-configuration question rather than a writing one. It is out of scope for this
plan as written. **It should not stay out of scope for long.**
