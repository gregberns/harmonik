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

## 2. The measured boot budget

| Source | Always / boot | Bytes | Est. tokens |
|---|---|---:|---:|
| **Harness, before harmonik does anything** | | | |
| `CLAUDE.md` to `AGENTS.md` | always | 23,232 | 5,808 |
| `~/.claude/CLAUDE.md` | always | 2,094 | 524 |
| Auto-memory index | always | 1,174 | 294 |
| Project skill descriptions (17) | always | 7,598 | 1,900 |
| Global skill descriptions (4) | always | 1,035 | 259 |
| Built-in skill descriptions (~15) | always | ~7,000 | ~1,750 |
| Base system prompt and tool schemas | always | — | ~4,000 |
| **Captain boot, per `STARTUP.md`** | | | |
| `harmonik agent brief` output | boot | 6,844 | 1,711 |
| `.harmonik/context/project.yaml` | boot | 2,045 | 511 |
| `.harmonik/context/captain-lanes.md` | boot | 3,418 | 855 |
| `.harmonik/context/direction-log.md` | boot | 7,676 | 1,919 |
| `.claude/skills/captain/STARTUP.md` | boot | 22,204 | 5,551 |
| `.claude/skills/captain/SKILL.md` | boot | 21,607 | 5,402 |
| `.claude/skills/orchestrator-rules/SKILL.md` | boot | 5,222 | 1,306 |
| **`scripts/captain-boot-digest.sh` output** | boot | **90,059** | **22,515** |
| **Measured total** | | **194,208** | **48,552** |
| **With estimated harness overhead** | | ~208,000 | **~54,300** |
| *On demand, if pulled* | | *76,687* | *19,172* |

A captain starts at roughly **48.5 K tokens measured, 54 K including harness** — about 27 %
of a 200 K window. Fully loaded, with every on-demand skill pulled, about 73 K.

No tokenizer is installed on this machine. Token figures are bytes divided by four, the
conservative end: these files run 6.3 to 7.6 bytes per word, so a word-based estimate lands
20 to 25 % lower.

### The one number that dominates

**The boot digest is 57 % of the whole boot, and 87 % of the digest is one section.** Its
ready-beads section is 78,019 bytes of the digest's 90,059 — 658 rows, because the script
passes an explicit no-limit flag. Every other section is under 2.2 KB except the kerf map at
7.1 KB.

Capping that section at the top sixty beads removes about **17,500 tokens**, more than the
entire captain skill corpus. It is a one-line change to a shell script and it is not an
instruction problem at all.

### The tax nobody can escape

`AGENTS.md` is 23 KB, and the harness injects it into **every agent in this project** before
a single command runs. The captain's own load map says a captain does not boot-read it.
Nothing on the harmonik side can suppress it. Cutting `AGENTS.md` is a fleet-wide saving
multiplied by every running agent, not a captain-only one.

### Handoffs, not briefs, are what get expensive

A captain brief is only 6,844 bytes because no `HANDOFF-captain.md` exists. Lane briefs
measured: alpha 34,109 B, of which 18,376 is an inlined handoff; bravo 25,636 B; assessor
32,139 B; admiral 17,235 B. A captain handoff written at alpha's size would roughly triple
the captain's brief.

### One more mechanism worth knowing

The captain is launched with a seed message pasted into its pane — an instruction to run
`harmonik agent brief` and begin the loop. So the brief enters context as a command result,
billed as conversation rather than system prompt, and it is **paid again on every keeper
restart**.

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

| Rank | Change | Saving | Cost to make it |
|---|---|---:|---|
| 1 | Cap the digest's ready-beads section | ~17,500 tok | one line of shell |
| 2 | Stop the captain reading `STARTUP.md` and `SKILL.md` at boot — the router flip | ~11,000 tok | the flip nobody made |
| 3 | Cut `AGENTS.md` to a real router | ~4,000 tok, **times every agent** | a careful pass |
| 4 | Delete the 25 duplicated rules | ~3,000–3,750 tok | a careful pass |
| 5 | Fix or delete the stale tier files | ~1,500 tok | judgement, and it fixes wrong instruction too |
| 6 | Drop the mirror banner from shipped skill bodies | ~475 tok | depends on the mirror collapse |

The first two are mechanical and together are worth more than everything the disposition
ledger in Step 2 can plausibly delete. **They should not wait for the ledger.**
