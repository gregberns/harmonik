# Agent manifest — design strawman (v0)

**Date:** 2026-07-03. The focused deliverable. A "type" is a folder of static content + a
tie-together manifest; a single command assembles and injects it at launch. Strawman for reaction —
command/field names are illustrative, not final.

## 1. Folder-per-type layout (the "genome")

```
.harmonik/agents/                 # the type registry (git-tracked, immutable master = provenance source)
  crew/
    manifest.yaml                 # tie-together file
    soul.md                       # identity — PROVENANCE MASTER (re-pinned from here, never from handoff)
    operating.md                  # operating instructions (or refs to skills/docs)
  admiral/
    manifest.yaml
    soul.md
    playbook.md                   # (cleaned-up; today's admiral-playbook is an incident-report dump)
  captain/
    manifest.yaml
    soul.md
  watch/
    manifest.yaml
    soul.md
    tools/                        # KNOWN LOCATION for watch's self-built scripts (see §5)
```

**Type vs instance.** A *type* (crew) is the folder. An *instance* (leto) is a running agent of that
type — differs only by launch-time data (name, epic, queue, harness) + its own private handoff. Type
= immutable config; instance = mutable state. `crew` has cardinality 1:n; `admiral`/`captain`/`watch`
are max:1 singletons.

## 2. manifest.yaml (strawman schema)

```yaml
type: crew
cardinality:   { min: 0, max: n }          # admiral/captain/watch: max 1
harness:       claude                       # default actor; overridable per-instance (claude|codex|pi)

identity:
  soul: soul.md                             # THE provenance master — injected + re-pinned every restart
  parent_intent: captain                    # graft parent's 1-line intent (two-levels-up / anti-stall)

context:                                    # the capabilities manifest
  - { ref: operating.md,   as: instruction, presence: injected }
  - { ref: crew-launch,    as: skill,       presence: injected }   # claude=skill; codex/pi=compiled to prose
  - { ref: beads-cli,      as: skill,       presence: retrieved }
  - { ref: harmonik-dispatch, as: skill,    presence: retrieved }
  - { ref: docs/orchestration-protocol-v2.md, as: doc, presence: retrieved }

triggers:                                   # declared, toggleable (default crew: just the queue)
  - { id: queue, source: queue, enabled: true }

handoff:
  channel: private                          # own HANDOFF-<name>.md   (admiral could add a LEDGER later)

keeper:      { thresholds: default }
lifecycle:   { self_restart: true }         # can restart self/peers via a command (replace, not add)
markers:                                    # watch checks these against the EVENT STREAM (not transcripts)
  never_emits: []                           # admiral example: [crew_start, queue_submit:main]
```

Admiral's manifest differs by: `cardinality.max:1`, richer `context`, a **scheduled trigger** (§4),
`never_emits: [crew_start, ...]`, and `parent_intent: operator`.

## 3. The injection mechanism — ONE skill, ONE command (operator's key ask)

Operator constraint: **NOT a pile of skills.** Keep the thing that already works — "session-resume +
agent name" — and make it cascade from a single command.

```
Operator (or keeper on restart):  run the ONE boot skill with just the agent name
  → skill calls:  harmonik agent boot --name leto        # ONE command (name illustrative)
      1. resolve leto → type=crew → read .harmonik/agents/crew/manifest.yaml
      2. emit a STRUCTURED bundle:
           - SOUL (read from soul.md master) + grafted parent intent
           - injected context inlined (operating.md, crew-launch body)
           - RETRIEVED context as pointers ("load beads-cli when you touch br")
           - the handoff file to read (episodic state only)
           - active triggers + this wake's reason
  → skill reads the bundle and proceeds. No multi-skill cascade — code does the assembly.
```

- **The skill is thin; the command does the work.** One memorable verb the operator can remember.
- **Provenance rule falls out:** on keeper `/clear`, the same command re-runs; SOUL always comes
  from `soul.md`, the handoff supplies ONLY episodic state → no Xerox-of-a-Xerox drift.
- Per harness: the boot command's bundle is delivered via the existing seam — claude gets it as the
  paste-seed / pre-exec messages + real skill files symlinked; codex/pi get the same bundle compiled
  into their seed prompt. Same command, three tails.

## 4. Scheduled commands as triggers (the "it is now time to…" idea)

A trigger with `source: cron` that **delivers a comms message carrying a specific instruction to call
a command**. Reuses the comms bus; toggleable; the daemon owns the clock.

```yaml
triggers:
  - id: priorities-report
    source: cron
    every: 6h                 # only while the system is/has been operating (guard on activity)
    enabled: true
    deliver: comms            # message lands in the agent's inbox as a wake
    message: "It is time to summarize what you're doing — publish a priorities update via <command>."
```

Options to pin down later: trigger sources (`cron` / `interval` / `event` / `manual`), the
activity-guard ("only if the fleet has been running"), and delivery (comms message vs direct seed).
The *report target* (where the update goes) is the deferred publish-seam work — for now the message
can just tell the agent to post to comms / write its handoff.

## 5. Self-extending capabilities (watch builds its own tools)

A type may declare a **writable capability location** (e.g. `watch/tools/`) + permission to update
its own `operating.md` to reference what it built. So the watch can write a little script, drop it in
the known dir, and teach itself about it — its capability set grows without a code change. Advanced;
note it as a manifest affordance, design later. (The alignment-watcher-as-configured-type idea lives
here too — it's just another folder under `.harmonik/agents/`.)

## 6. What this delivers vs today
- One folder per type; no mega mission file; a yaml ties content together (operator's A).
- Models crew (1:n, queue-triggered), admiral (singleton, scheduled report), captain (singleton,
  tied to `harmonik start captain`) — all as config, captain kept first-class (operator's B).
- Open-ended types — new folder = new type (operator's C).
- One boot command + one skill; identity re-pinned from the master = the drift fix.
- Markers checked on the event stream by watch (cheap, no transcript crawl).

## 7. Resolved (operator, 2026-07-03) — supersedes conflicting bits above

- **Name resolution:** `--agent` is OPTIONAL. If omitted, use `$HARMONIK_AGENT` (harmonik sets it in
  every agent process). If BOTH are supplied and they **conflict → hard error** (never let one agent
  boot off another's handoff). This is a load-bearing safety check.
- **No side effects.** The boot command ONLY **emits text the agent reads** — no symlinking, no
  seed-writing, no per-harness provisioner. The emitted document works identically for claude/codex/pi.
- **Skills = harmonik-supplied, scoped per agent (NOT global auto-load).** Lay a skill out exactly
  like the current skill spec, but *attach it to harmonik* and inject it ONLY into the agents whose
  manifest requests it. The operator disables the globally-auto-pulled skills (via `.claude/
  settings.json`) so nothing leaks in; every agent then gets exactly its declared skill+tool set.
  In the boot document each skill appears as a **short description + a pointer** to the full
  instruction set (pulled on demand → keeps context lean). This replaces the earlier "symlink real
  skill files / compile-to-prose per harness" idea entirely.
- **Contract strength: light.** The primary output is **Markdown** — it IS the agent's boot prompt,
  well-laid-out. Add `--format json|yaml|toon` for a structured render (so an agent can re-query and
  confirm its instruction set is structured correctly after editing it) and a `--validate` / `--dry-run`
  that checks the agent's folder/manifest is laid out correctly. **The real work is getting the ORDER
  of the instructions right**, not enforcing a rigid schema.
- **Command name: a fresh agent picks** (operator doesn't care). Candidates: `brief` · `orient` ·
  `prime` · `boot` · `prompt` · `directive`. One design agent decides + one-line rationale.
- **`--override` flag:** the captain (or operator) can pass `--override` to bypass the
  `--agent`≠`$HARMONIK_AGENT` conflict error — needed so a captain can TEST an agent's assembled
  metadata after configuring it, without the safety error blocking the test.
- **`--agent` flag naming:** use whatever the existing CLI convention is (match `crew`/`start`).
- **Schema check is its OWN verb/command** (not just a `--validate` flag) — a first-class
  "check this type folder is well-formed" command.
- **Shared skills folder:** a common skills dir shared across agent types; manifests POINT there
  (so a skill used by several types isn't duplicated). Per-agent skills can still live in the type folder.
- **Note-taking via self-message (parked):** idea — an agent messages ITSELF on the comms bus with
  **tags**, giving a searchable/filterable note trail. Parked into `../2026-07-03-fleet-state-and-dashboard-data/`.

### The boot-document ORDER (proposed — most durable first, anchors identity before episode)
1. **Identity / SOUL** — who I am · what I do NOT do · escalate-to · + parent's intent. FIRST, so it
   anchors everything read after it (this is the anti-drift ordering).
2. **This wake's reason** — fresh start vs keeper-restart vs scheduled trigger (so the agent knows
   whether to resume or re-plan — kills the "inferred through a wrong frame" failure).
3. **Operating instructions** — how I work: `operating.md` + the requested skills as short-desc + pointer.
4. **Active triggers** — what wakes me, and how (queue / cron / comms).
5. **Handoff (episodic state)** — LAST: what happened last session, open items. Read in the context
   of identity+role, never the reverse.

## Open questions to resolve next
- **Q1** — manifest field set: is the strawman schema right? What's missing / over-built?
- **Q2** — the boot command's output contract: what EXACTLY does the structured bundle contain, and
  how does each harness consume it? (This is the load-bearing interface.)
- **Q3** — where do type folders live: `.harmonik/agents/<type>/` (proposed) vs under `.claude/`?
  They must be git-tracked and NOT harness-specific.
- **Q4** — instance state: keep `crew.Record` + per-instance handoff as-is, or fold into the new model?
- **Q5** — migration: model `crew` first end-to-end, prove the boot command, then admiral + captain.
