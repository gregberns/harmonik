# 04 — Prior planning on agent identity, role drift & role-definition structure

**Investigated 2026-07-03.** Scope: admiral-framework, admiral retros/playbooks, crew-arrangement
review, clear-communication-pattern, distributed-fleet, "decouple-role-from-name" park.

## Headline findings (the two focus questions)

**(a) Prior thinking on admiral↔captain drift after ~6h:** NO prior plan framed the failure as
*time-based role-identity degradation*. Closest: `admiral-framework/` diagnoses a **frame that
self-reinstantiates verbatim through every keeper `/clear`** — a momentary mis-classification
hardening into a structural config. Most relevant mechanism for "roles forget their job across
resumes," but never generalized to admiral/captain overlap or a 6h horizon. The admiral retro shows
the boundary IS soft (admiral repeatedly did captain-adjacent narration) — framed as value/waste,
not identity drift.

**(b) Prior "decouple role from name" / pluggable roles:** Exists ONLY as a **PARKED admiral item**,
never designed. Survives as one reference: `plans/2026-07-03-operator-dashboard/DESIGN.md` cites
`HANDOFF-admiral.md` PARKED item #3 (role-decoupled-from-agent) — but reads it narrowly as "a clean
crew↔lane map to render," NOT pluggable/renameable role definitions. **C6 is genuinely greenfield.**
Nearest adjacent art: distributed-fleet's Hermes study — **profile-as-forked-identity** (own
memory/skills/keys/persona via `HERMES_HOME`), the nearest existing "SOUL"-style concept.

## File-by-file

### `plans/2026-06-25-admiral-framework/`
- **Problem:** fleet drained #1 program then sat idle ~2h emitting ~26 msgs confirming idle. Root
  cause: admiral+captain mis-classified "resume a KNOWN parked ranked lane" as "rank a brand-new
  initiative," and the hold posture **re-instantiated verbatim through every keeper `/clear`**.
- **Key insight (Part 0): "More principle-text will NOT work."** The self-check mechanisms all
  fired and returned the WRONG answer — they're **self-scored judgment questions answered through
  the agent's current (wrong) frame.** Fix must remove judgment from the trigger.
- **Proposed:** a DETERMINISTIC, agent-EXTERNAL trigger (bash `ops-monitor-check.sh` computes a fact
  and PUSHES a lane-named `[IMMEDIATE]` wake); a first-class `lanes.json` lane-index; the
  SELF-AUTHORIZATION principle (a lane in any durable doc / ever-ranked is KNOWN & self-resumable;
  only never-recorded initiatives are operator-gated). Canonical home = orchestrator-rules
  §Autonomy, stated ONCE + one-line pointers per role (don't copy verbatim into 6 files).
- **Landed:** plan NOT applied (operator-gated), BUT the SELF-AUTHORIZATION principle + PARKED-is-a-
  fact framing DID land into `captain/STARTUP.md`, admiral-playbook, admiral-initiatives. PLAN-v2
  flagged a **per-role boot-token measurement / de-dup scope** (relevant to C5) — scope only, not built.

### `plans/2026-06-22-admiral-retro.md`
- ~11h admiral session; several audits were **pure narration of the captain's wins = near-no-op** —
  empirical evidence the admiral↔captain boundary is soft; mid/late session the admiral collapsed to
  "confirmation + operator-relay." Also ran to ~457k tokens keeper-less (long-session degradation
  data point, but token-cap not role-identity).

### `plans/2026-06-22-admiral-suggestions-central.md`
- Operator-review artifact; 11 process improvements codified into `admiral-playbook.md`. Oversight-
  quality tuning, no role-identity/decouple content.

### `plans/2026-06-22-crew-arrangement-review.md`
- **Problem:** "fill every lane" over-provisions always-on Opus orchestrators (~96% spend = 24/7
  cache-read). Rec **E** = replace *"one crew per lane"* with *"one crew per JUDGMENT-requiring
  lane"* — early argument that role/session existence should be scoped to **function, not name/slot.**

### `plans/2026-06-20-clear-communication-pattern/`
- Making guidance survive `/clear` via an embed mechanism — the *continuity* substrate the README
  distinguishes from *identity*. Prior art on injection/embedding (C4), not identity itself.

### `.harmonik/crew/admiral-playbook.md` + `admiral-initiatives.md`
- **Playbook** = the durable "operating instructions" layer for the admiral (README's C2 middle
  layer, ALREADY existing per-role): 11 evidence-grounded rules; points to orchestrator-rules
  §Autonomy rather than re-deriving — the canonical-home pattern to reuse.
- **Initiatives registry** = admiral-owned "what/what's-next" map with status vocab
  (ACTIVE/ON-DECK/PARKED/GATED/DONE) where **PARKED = a fact (zero ready beads), decoupled from
  GATED.** These two files are a working example of the mission-vs-operating-instructions split.

### `plans/2026-06-30-distributed-fleet/`
- **Hermes study** = load-bearing prior art: **profile = fully-forked identity** (own state,
  memory, skills, keys, persona via `HERMES_HOME`) — the "SOUL.md" analog. Contrast: harmonik crews
  *share* the repo/beads DB, differ only by *mission + queue*. Recommends **"harness = pluggable
  seam"** — maps onto C6.
- **PLANNING-SUMMARY** names 3 Hermes concepts to adopt: workspace-kinds, typed block reasons,
  profile-as-forked-identity. `04-auto-comms-startup/` = forced comms-subscribe as a harness-
  enforced boot property (not a skippable prompt step) — directly relevant to C4 injection
  ("make it a boot property the model can't lose on `/clear`").

## Net for the current plan
1. The **self-reinstantiating-frame-through-`/clear`** mechanism is the strongest prior explanation
   for "roles forget their job across resumes" — reuse for C7.
2. The **canonical-home + one-line-pointer** pattern is the established idiom for durable per-role
   identity; C1/C2 should extend it, not reinvent.
3. **Pluggable roles / decouple-role-from-name = genuinely unbuilt** (only a parked one-liner + Hermes
   adjacent art). C6 is greenfield.
4. **admiral-playbook + admiral-initiatives already instantiate the 3-layer split for one role** —
   use as the reference schema for C2/C3.
5. **Open threads to chase:** PLAN-v2's per-role boot-token measurement (C5); whether admiral-
   framework's deterministic-trigger/lanes.json machinery was ever built.
