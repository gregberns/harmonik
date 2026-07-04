# Vision 1 — The Agent as a Declared Actor

## (a) Core reframe
Today a "role" is a **noun** — a bundle of prose (skill + mission + JSON) assembled by copy-paste;
the drift problem is really *identity leakage across restarts*. Reframe every agent as an **actor**:
a small, addressable process defined not by what it *is* but by **what it reacts to**. An actor =
`(triggers → behavior → lifecycle → cardinality)` — a point in config space, not a class in an
inheritance tree. **Triggers are first-class declared data, not implicit wiring baked into Go**:
"what wakes admiral" should be a line you can read, add, and *toggle off* without touching the
harness — meaning the same whether the body is a claude REPL, a codex run, or a pi run.

## (b) The orthogonal axes (six, independently configurable)
1. **Identity (SOUL)** — durable "who am I / what I never do / who I escalate to." Harness-agnostic prose, re-pinned every wake.
2. **Behavior binding** — which operating-instruction set + which harness (`claude`|`codex`|`pi`). Body swappable under a fixed identity.
3. **Trigger set** — declared list of activation sources, each individually named + toggleable.
4. **Lifecycle policy** — spawn/sleep/wake/restart(keeper)/retire, and *who* owns each transition. Keeper = one lifecycle actuator among several.
5. **Cardinality & backpressure** — min/max live instances; overflow behavior (queue/drop/coalesce/reject-escalate).
6. **Handoff channel** — which state file(s) this actor reads/writes on wake. A *named channel reference*, not a hardcoded `HANDOFF-<name>.md`.

Win: rename captain→mayor = axis 1; swap claude→pi = axis 2; mute admiral's timer = axis 3 — each a one-line edit leaving the others untouched.

## (c) Triggers as declared, disableable inputs
Model triggers as a **subscription table**, not code. The *daemon* owns the trigger loop and hands
the actor a **wake reason**; the three harnesses differ only in the last inch (delivery).

```yaml
# roles/admiral.yaml  (extends crew.Record + mission schema)
identity:   souls/admiral.md
harness:    claude
handoff:    channels/admiral       # private channel; captain uses channels/fleet
cardinality: { min: 0, max: 1, on_overflow: reject-escalate }
triggers:
  - id: operator-msg     source: comms     filter: {to: admiral}                enabled: true
  - id: planning-timer   source: cron      filter: {every: 6h}                  enabled: false  # muted, one flag
  - id: epic-drained     source: bus       filter: {event: epic_completed}      enabled: true
  - id: captain-signal   source: agent     filter: {from: captain, kind: needs-ranking} enabled: true
  - id: keeper-restart   source: lifecycle filter: {event: context_full}        enabled: true   # wake ≠ new task
```

- **Sources unify across harnesses.** `comms`/`bus`/`cron`/`queue`/`agent`/`lifecycle`/`operator`
  all resolve to *a wake with a reason*. Delivery differs only at the tail: claude = bracketed-paste
  seed into the tmux REPL (existing `pasteCrewMission` seam); codex/pi = wake reason becomes the
  run's injected preamble. Same table, three tails.
- **Disabling a trigger = `enabled: false`** — declared, greppable, reversible, auditable. No trigger
  wired implicitly in Go → no "where do I turn off the health tick?" hunt (a real past incident).
- **The wake reason is re-injected alongside the SOUL every wake** — a keeper `/clear` wake carries
  `reason: keeper-restart` (resume, don't re-plan); a `planning-timer` wake carries `reason:
  cron/6h` (do plan). Directly attacks drift: the actor never *infers* why it woke through an
  already-wrong frame — the frame arrives externally with the wake.

## (d) Newly possible
1. **Trigger-level throttling/muting without redeploy** — "only wake admiral on operator msg for 6h"
   = flip two `enabled` flags. Generalizes the bespoke wake-economy cutover into declared per-actor policy.
2. **Harness A/B under a stable identity** — run the same admiral SOUL on claude today, pi tomorrow;
   split crew cardinality (one codex, one pi) and compare, zero identity churn.
3. **Cardinality caps as declared invariants** — `max:1` + `reject-escalate` makes a second admiral
   structurally impossible; duplicate-escalation / fork-bomb classes become impossible-by-declaration.
   Actors get a coalescing mailbox → a burst of `epic_completed` collapses into one wake.

## (e) Constraint to DELETE
**"Operating instructions must live in a claude-only skill (`.claude/skills`)."** Skills are a
*delivery mechanism*, not the contract. Promote operating-instructions to a **harness-agnostic
content reference** the daemon resolves + injects through whichever tail the harness uses. Skills
become *one renderer* for the claude tail; codex/pi get the same content as a preamble. Removing this
also lets us **delete the two hardcoded roles in `start.go`** — `start` resolves *any* actor name
through the registry, captain included (kept as a back-compat verb, not a special code path).
