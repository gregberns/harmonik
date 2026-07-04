# SPEC — Agent manifest & boot-context injection

Authoritative spec. An implementer follows THIS. Scope: agent identity + declarative per-agent config
+ a single emit-only boot command. The publish/fleet-state/dashboard work is DEFERRED (see §Out of
scope) — do NOT spec or build it here.

## 1. Folder layout (the type registry)

```
.harmonik/agents/                  # git-tracked, immutable identity master (provenance source, I1)
  _skills/                         # SHARED skills dir; manifests point here for skills used by >1 type
    <skill-name>/SKILL.md          # laid out per the existing skill spec, attached to harmonik
  crew/
    manifest.yaml
    soul.md                        # identity master (re-pinned verbatim every restart)
    operating.md                   # how-I-work (loop + skill pointers)
  admiral/
    manifest.yaml
    soul.md
    operating.md
  captain/
    manifest.yaml
    soul.md
    operating.md
  watch/
    manifest.yaml
    soul.md
    operating.md
    tools/                         # (affordance only, deferred) writable self-built scripts
```

- A **type** = a folder. An **instance** (e.g. crew `leto`) = a running agent of that type, differing
  only by launch data (name, epic, queue, harness) + its own private handoff. Type = immutable config;
  instance = mutable state.
- **Open-ended vocabulary:** a new type is a new folder. No enum, no Go change.
- Type folders are git-tracked and harness-agnostic (NOT under `.claude/`).
- Per-type skills MAY live in the type folder; skills shared by >1 type live in `_skills/` and are
  referenced by bare name (stored once; see §6 resolution rule).

### Authoring limits (from AUTHORING-GUIDE)
- `soul.md` <= 25 lines: **I am** (1 sentence) / **I do** (2-4 bullets) / **I do NOT** (2-4 bullets,
  the anti-drift boundary) / **I escalate to**. No prose, no history. The parent-intent line is NOT
  authored here — `brief` grafts it at emit time from the parent's `soul.md` (see §4).
- `operating.md` <= 45 lines: On-wake ritual / Loop (<=6 steps) / Skills-I-use (name + one-line "when",
  never inline bodies) / Bounds (2-3 don'ts). Reference skills by name; never copy their content.

## 2. manifest.yaml schema

```yaml
type: crew                          # == folder name
cardinality: { min: 0, max: n }     # admiral/captain/watch: { min: 0, max: 1 } (singleton)
harness: claude                     # default actor; overridable per-instance (claude|codex|pi)

identity:
  soul: soul.md                     # THE provenance master; injected + re-pinned every restart (I1)
  parent_intent: captain            # role whose 1-line intent is grafted (crew->captain->admiral->operator)

context:                            # capabilities manifest
  - { ref: operating.md,      as: instruction, presence: injected }
  - { ref: crew-launch,       as: skill,       presence: injected }   # bare ref → _skills/ then type folder
  - { ref: beads-cli,         as: skill,       presence: retrieved }  # pointer only, pulled on demand
  - { ref: docs/orchestration-protocol-v2.md, as: doc, presence: retrieved }  # path sep → literal

triggers:                           # declared, toggleable; daemon owns the loop + supplies wake-reason
  - { id: queue, source: queue, enabled: true }

handoff:
  channel: private                  # own HANDOFF-<name>.md (episodic state only)

keeper:    { thresholds: default }
lifecycle: { self_restart: true }   # restart = replace, not add (a lifecycle transition)
tools_dir: null                     # (INERT affordance) declared for forward-compat; NOT acted upon
                                    #   in this work — self-built tools are DEFERRED (see §8)
markers:                            # declarative only; watch checks vs EVENT STREAM (not transcripts)
  never_emits: []                   # e.g. admiral: [crew_start, queue_submit:main]
```

### Field meaning
- **type** — folder name; the type identifier.
- **cardinality** — min/max live instances. `max:1` forbids a second instance by declaration.
- **harness** — default actor; per-instance override allowed.
- **identity.soul** — path to the provenance master read on every boot.
- **identity.parent_intent** — the parent role whose one-line intent `brief` grafts into the SOUL
  section AT EMIT TIME (two-levels-up anti-stall lever). This field is the SINGLE source of the parent
  line: `brief` reads the named parent type's `soul.md` and grafts its intent — child souls do NOT
  hand-copy it. The reserved terminal `operator` (the human; no type folder) roots the chain
  (crew→captain→admiral→operator).
- **context[]** — `{ ref, as, presence }`. `as` in {instruction, skill, doc}. `presence` in
  {injected, retrieved, embodied}: **injected** = inlined/short-desc into the boot doc; **retrieved**
  = pointer only, pulled on demand (keeps context lean); **embodied** = realized as a tool/wrapper
  (declared, not built by this work). `ref` is resolved per §6: a bare name resolves against `_skills/`
  then the type folder; a path-bearing ref is literal.
- **triggers[]** — `{ id, source, enabled, ... }`. `source` in {queue, cron, interval, event, comms,
  manual, operator}. `cron`/`interval` carry `every:` + `deliver: comms` + `message:` (see §5).
- **handoff.channel** — where episodic state lives (private HANDOFF-<name>.md now; LEDGER deferred).
- **keeper / lifecycle / markers** — as above; markers are declarative-only in this work.
- **tools_dir** — EXPLICITLY INERT. A declared affordance for a future self-extending-tools capability
  (an agent writing its own scripts); `check` accepts it but nothing in this work acts on it (see §8).

## 3. Boot command contract

Command group: `harmonik agent`. Two verbs.

### `harmonik agent brief` — emit the boot document
- **Purpose:** resolve an agent -> its type -> read the manifest -> **emit the ordered boot document**.
- **Side effects: NONE.** Emits text only. No symlink, no seed-write, no per-harness provisioner. The
  emitted document works identically for claude/codex/pi (I2).
- **Parent-intent graft (I1):** `brief` reads the parent type's `soul.md` (named in
  `identity.parent_intent`), takes its one-line **I am** intent, and grafts it into boot-doc section 1.
  The `operator` terminal has no folder → a fixed operator-intent line is grafted. The child's own
  `soul.md` carries NO parent-intent line (the manifest field is the single source).
- **Name resolution (load-bearing):**
  - `--agent <name>` is OPTIONAL.
  - If omitted, use `$HARMONIK_AGENT` (harmonik sets it in every agent process).
  - If BOTH `--agent` and `$HARMONIK_AGENT` are supplied and they **conflict -> HARD ERROR** (never
    let one agent boot off another's identity/handoff). Load-bearing safety check.
  - `--override` bypasses the conflict error (captain/operator testing another agent's assembled
    metadata after configuring it).
  - `--agent` name resolves: instance name -> its type folder (crew `leto` -> `crew/`); a bare type
    name resolves to itself.
- **`--wake <fresh|keeper-restart|trigger:<id>>`** (default `fresh`) — the wake-reason feeding boot-doc
  section 2 (fresh start vs keeper-restart vs a fired trigger). The launcher/keeper passes it on restart
  (keeper restart path — bead hk-cxyn7 / T8); absent → `fresh`.
- **`--format markdown|json|yaml|toon`** (default `markdown`). Markdown IS the boot prompt. json/yaml/
  toon render the same content structured, so an agent can re-query and confirm its instruction set is
  structured correctly after editing.
- Match existing CLI conventions for flag naming (mirror `crew`/`start`).

### `harmonik agent check` — schema/layout validation (first-class verb)
- **Purpose:** validate a type folder is well-formed: manifest fields present + typed correctly,
  referenced files exist, `context[].ref` resolve (incl. `_skills/`), boot-doc order buildable.
- Its OWN verb (per MANIFEST §7), not merely a flag. A `--validate`/`--dry-run` flag on `brief` MAY
  exist as a thin alias.
- **C-C parent-intent check:** `identity.parent_intent` MUST name an existing type whose `soul.md` is
  readable, OR the reserved terminal `operator`. A dangling parent (no such type folder) is a defect.
- **C-C trigger-source check:** each `triggers[].source` MUST be in the allowed set
  {queue, cron, interval, event, comms, manual, operator}.
- Exit 0 + "ok" when well-formed; non-zero naming the specific defect otherwise.

## 4. Boot-document ORDER (the load-bearing interface)

`brief` emits sections in EXACTLY this order (MANIFEST §7 — "the real work is getting the order
right"). Ordering is the anti-drift mechanism: identity anchors before episode.

1. **Identity / SOUL** — who I am, what I do NOT do, escalate-to, + parent's intent. FIRST. Content is
   byte-identical to `soul.md` (the provenance master); `brief` then GRAFTS the parent's one-line
   intent — read at emit time from the PARENT type's `soul.md` (the role named in
   `identity.parent_intent`, taking that role's **I am** line) — appended as the "Parent intent" line.
   Child souls never hand-copy it. For the `operator` terminal (no type folder) `brief` grafts a fixed
   operator-intent line (the chain root).
2. **This wake's reason** — fresh start vs keeper-restart vs scheduled trigger (so the agent knows to
   resume vs re-plan).
3. **Operating instructions** — `operating.md` (injected) + each requested skill as a **short
   description + a pointer** to its full instruction set (pulled on demand). Never inline skill bodies.
4. **Active triggers** — what wakes me and how (queue / cron / comms); only `enabled: true` triggers.
5. **Handoff (episodic state)** — LAST. What happened last session, open items. Episodic only; carries
   NO identity re-statement.

**Provenance rule (I1):** SOUL always comes from `soul.md`; the handoff supplies ONLY episodic state.
They are never fused. On keeper `/clear` the same command re-runs -> no Xerox-of-a-Xerox drift.

## 5. Triggers

`triggers[]` is a declared, toggleable subscription table. The daemon owns the loop/clock and hands
the agent a wake-reason (feeds boot-doc section 2).

- crew: `{ id: queue, source: queue, enabled: true }`.
- A **scheduled command** trigger delivers a comms message carrying an instruction to call a command:

```yaml
triggers:
  - id: priorities-report
    source: cron
    every: 6h
    enabled: true
    deliver: comms
    message: "It is time to summarize what you're doing - post a priorities update."
```

This work DECLARES the schema + emits declared triggers in the boot doc. Daemon-side delivery of new
cron/interval sources is a buildable follow-on. The report **target** (fleet-state) is deferred — for
now the message just tells the agent to post to comms / write its handoff.

## 6. Skills model
- Skills are **harmonik-supplied and per-agent-scoped**, NOT global auto-load. Global skill autoload
  is disabled via `.claude/settings.json` so nothing leaks in.
- Each skill is laid out per the existing skill spec, attached to harmonik, and injected ONLY into
  agents whose manifest `context[]` requests it.
- Shared skills (>1 type) live in `.harmonik/agents/_skills/`; manifests point there (stored once).
  Per-agent skills may live in the type folder.
- **Skill-ref resolution (one rule):** a bare `ref` (e.g. `crew-launch`) resolves against
  `.harmonik/agents/_skills/` FIRST, then the type's own folder; a `ref` containing a path separator
  (e.g. `docs/orchestration-protocol-v2.md`) is taken literally (relative to repo root). `check` (C-D)
  validates every `ref` resolves under this rule.
- In the boot document a skill appears as **short-desc + pointer**, pulled on demand (context stays
  lean). This replaces the earlier "symlink real skill files / compile-to-prose per harness" idea.

## 7. Command name
Use **`brief`** (`harmonik agent brief`). Rationale: it produces the agent's *briefing* — the emitted
boot document — and reads naturally as a verb without colliding with existing "boot" terminology
(boot assets, boot digest, boot skill). Schema-check verb: **`check`** (`harmonik agent check`).

## 8. Out of scope (DEFERRED — do not build here)
- Publish seam / fleet-state store / `admiral-initiatives.md` as a generated view / operator dashboard
  data -> `plans/2026-07-03-fleet-state-and-dashboard-data/`.
- Memory-horizon LEDGER as a published store (handoff stays a private markdown file for now).
- Self-message tagged note-taking (parked).
- Embodied guardrails / rules-as-code (deprioritized, DIRECTION §D); `markers` stay declarative-only.
- Self-extending capabilities (watch writing its own `tools/`): manifest affordance only, designed later.
- Per-harness provisioner (symlink/compile-to-prose): explicitly deleted — emit-only markdown (I2).

## DECISIONS-NEEDED
1. **Shared-skills dir location.** SPEC assumes `.harmonik/agents/_skills/`.
   - Option A: `.harmonik/agents/_skills/` (co-located with types; one tree to git-track). Consequence:
     keeps everything agent-related under one root; needs a naming rule so `_skills` never collides
     with a type named `_skills` (leading-underscore reserved).
   - Option B: reuse the existing `.claude/skills/` tree and point manifests at it. Consequence: no
     duplication of today's skills, but couples the "harness-agnostic" registry to a claude-named
     path (violates the git-tracked-not-harness-specific intent) and complicates disabling autoload.
   - Recommend A. Confirm the reserved-prefix rule.
2. **Wake-reason source for boot-doc section 2.** (RESOLVED — Option A: the `--wake` flag, see §3.)
   How does `brief` learn fresh vs keeper-restart vs scheduled-trigger?
   - Option A: a `--wake <fresh|restart|trigger:<id>>` flag the caller (launcher/keeper/daemon) sets.
     Consequence: explicit, testable; each caller must pass it.
   - Option B: an env var (e.g. `HARMONIK_WAKE_REASON`) the daemon sets alongside `HARMONIK_AGENT`.
     Consequence: agent doesn't need a flag; another env contract to maintain.
   - Recommend A (flag), with default `fresh` when absent.
3. **Trigger activity-guard semantics.** MANIFEST §4 wants cron triggers to fire "only while the fleet
   has been operating." What's the guard?
   - Option A: daemon fires only if there was fleet activity within a window (config'd). Consequence:
     no wakes on an idle/paused fleet; needs an activity signal + threshold.
   - Option B: fire unconditionally on the schedule; agent decides if there's anything to report.
     Consequence: simpler, but re-introduces the "26 idle-confirmation messages" failure class.
   - Recommend A. Needs the activity signal + window defined (defer to daemon follow-on if trigger
     delivery itself is deferred).
4. **Instance -> type resolution source.** `brief --agent leto` must map instance `leto` -> type
   `crew`. Where does that mapping live?
   - Option A: extend `crew.Record` (`registry.go:38`) with a `type` field. Consequence: one
     authoritative record per instance; migration for existing records (default `crew`).
   - Option B: a separate instances index file under `.harmonik/agents/`. Consequence: new file to
     keep in sync with the crew registry (two sources of instance truth).
   - Recommend A (extend `crew.Record`), defaulting legacy records to `type: crew`.
