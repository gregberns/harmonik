# Possibility space — what this system could be

**Date:** 2026-07-03. Synthesis of four independent divergent lenses (`visions/01-04`): actor/trigger,
harness-portable provisioning, memory-horizons/homeostasis, cross-domain metaphors. They were told
NOT to converge; they converged anyway on a small set of ideas — that convergence is the signal.

## The reframe all four reached
**An agent is not a role-bundle. It is a *character* (role) cast on an *actor* (harness), defined by
a declared config entry over several ORTHOGONAL axes.** "Role" stops being a noun you assemble by
copy-paste and becomes a point in configuration space. Renaming captain→mayor, swapping claude→pi,
muting a timer, giving admiral a private memory — each moves ONE axis and leaves the rest untouched.

## Two deep invariants (independently found by ≥3 lenses)

### I1 — The provenance rule (the drift fix)
**Identity must be re-expressed on every restart from an EXTERNAL, IMMUTABLE master — never copied
from the outgoing session's own handoff.** Today KEEPER-IDENTITY re-pins identity *from the HANDOFF*
— the setpoint is downstream of the thing that drifts, so a session re-seeds itself from its own
drifted state and propagates the drift forever (Xerox-of-a-Xerox / corrupted epigenetic mark /
self-copy corruption — lenses 1,2,3,4 all named it). Fix is a **provenance rule on the injection
seam**, not better prose (which fact-5 says fails): SOUL is re-read from the role registry
(`roles.yaml`, the "genome"); HANDOFF supplies *only* ephemeral state. Fuse them and you build a
drift amplifier.

### I2 — The portability rule (the harness fix)
**A role declares WHAT it needs (harness-agnostic); a per-harness Provisioner decides HOW to make it
present.** Skills (`.claude/skills`) are *one delivery mechanism*, not the contract — codex/pi have
only a seed-prompt + one auto-read ambient file. So an "instruction" a role requests becomes a skill
on claude and *compiled-down seed prose* on codex/pi. The role author never sees the difference; the
same character runs on any of the three actors.

## The axes (union of the four lenses) — a role config entry declares each

| Axis | What it declares | Presence contract / notes |
|---|---|---|
| **1. SOUL / identity** | who I am · my job · **what I do NOT do** · who I escalate to · **+ parent's intent (1 line)** | INJECTED, re-pinned from immutable master (I1). Parent-intent graft = the anti-stall / two-levels-up lever. |
| **2. Capabilities** | instructions · knowledge · tools · guardrails it needs | each tagged **injected / retrieved / embodied**; realized per-harness by a Provisioner (I2). Guardrails = embodied (a `br` wrapper), NOT prose a `/clear` drops. |
| **3. Triggers** | what wakes it: comms · bus event · cron/timer · queue · agent-signal · operator · lifecycle | a declared **toggleable subscription table** (`enabled: true/false`); daemon owns the loop + hands the agent a **wake-reason**; delivery differs only at the harness tail. |
| **4. Lifecycle / keeper** | spawn/sleep/wake/restart/retire; keeper thresholds; hold/release | keeper is *one* lifecycle actuator; policy is per-role, not global. |
| **5. Cardinality** | min/max live instances + overflow behavior | `max:1` makes a 2nd admiral impossible-by-declaration (kills duplicate-escalation / fork-bomb classes). |
| **6. Memory horizons** | which streams it reads/writes: **PIN** (eternal identity) · **EPISODE** (this session) · **LEDGER** (multi-day role cohesion) · **FLEET** (shared bus) | admiral's multi-day view = LEDGER (`admiral-initiatives.md` already is one); restart cadence decoupled from LEDGER write cadence → no churn-drift. Per-agent handoff differences fall out of this, no special-casing. |
| **7. Markers** | deterministic "does-NOT-do" behaviors | checked EXTERNALLY by the **watch** session (immune patrol) against emitted commands — drift caught by a different agent than the one drifting, not by self-introspection. |

## What this makes possible that today's system can't
- **Add / rename roles as config** (mayor, governor, a new "librarian") — no Go edits, no mission
  copy-paste. `start` resolves *any* role via the registry; captain loses its hardcoded specialness
  (kept as a back-compat verb only).
- **Same role on any harness** — run the admiral SOUL on claude today, pi tomorrow; A/B a codex crew
  vs a claude crew under identical identity.
- **Mute/throttle a trigger without redeploy** — "only wake admiral on operator msg for 6h" = flip
  two `enabled` flags (generalizes the bespoke wake-economy cutover).
- **Structural anti-drift** — identity can't erode (I1) and role-bleed is caught by the watch patrol
  (axis 7), instead of surfacing to the operator hours later.
- **Right-sized memory per role** — admiral gets multi-day cohesion; crew stays lean; nobody carries
  what they don't need.

## Constraints today's design treats as fixed that we'd DELETE
1. Operating-instructions = `.claude/skills` (claude-only). → capabilities manifest + Provisioner.
2. HANDOFF carries identity across restarts. → provenance rule; identity from the genome only.
3. One handoff file per session. → memory horizons (PIN/EPISODE/LEDGER/FLEET).
4. Captain is the hardcoded root every role is relative to. → every role is peer config; intent is the invariant, not captain.
5. Triggers are implicit Go wiring. → declared, greppable, toggleable subscription table.
6. Drift is self-detected. → external marker check by the watch.

## Open decisions for the operator (pick where to go deep)
- **A — The registry unit.** One `roles.yaml` genome (types) + instances, extending `crew.Record`? Or
  keep per-mission files and add the missing axes to their front-matter? (types vs instances: is
  "admiral" a type with one instance, or just an instance?)
- **B — How far to push harness-portability now.** Build the Provisioner seam for all 3 harnesses, or
  prove I1 (provenance/anti-drift) on claude first and generalize later?
- **C — Cardinality / how many types.** Do we want a *fixed small vocabulary* of types (operator's
  "maybe constrain the number"), or open-ended? What's the seed set (captain, admiral, crew, watch, +?)
- **D — Where guardrails become embodied.** Which "does-NOT-do" rules graduate from prose to a tool
  wrapper / allowlist now (e.g. terminal-transition writes, cross-role verbs)?
