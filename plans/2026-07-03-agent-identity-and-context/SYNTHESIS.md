# Synthesis — what exists today, and the design direction

**Date:** 2026-07-03 · inputs: `investigation/01-05`. Status: for operator review.

## The five load-bearing facts

1. **Identity is scattered across 3 unrelated places, no single source of truth.**
   A role lives partly in a *skill* (`.claude/skills/<role>/`), partly in a per-instance *mission
   file* (`.harmonik/crew/missions/<name>.md`), partly in *registry JSON* (session-state only). The
   sharpest symptom: `captain_name` in every mission's front-matter is read by **zero Go code** — a
   pure LLM-facing convention. "captain" is baked into every crew by copy-paste, not by a model.

2. **Only captain + crew are first-class in code.** `cmd/harmonik/start.go` hardcodes exactly two
   roles ("roles are: captain, crew"). `"admiral"` appears in **0** non-test Go files — it's just a
   mission file launched as a crew. So renaming/adding roles today = editing Go + copy-pasting
   mission boilerplate. **C6 (pluggability) is greenfield.**

3. **The operator's 3-layer split already exists — for ONE role, ad hoc.** The admiral has:
   `admiral.md` mission (≈ identity), `admiral-playbook.md` (≈ operating-instructions),
   `admiral-initiatives.md` + JSON (≈ state). That trio is a working proof of the mission /
   operating-instructions / handoff split — but it's convention, unenforced, and unique to admiral.
   **The design job is to formalize that trio as the standard role shape and give every role one.**

4. **Injection is asymmetric and thin.** Crew launch delivers a bracketed-paste seed —
   *"read HANDOFF-<name>.md, run /session-resume, begin your loop"* (`crewstart.go:447`,
   `pasteCrewMission`). **Captain gets NO seed** — bare REPL, must self-find AGENTS.md/STARTUP.md.
   The daemon reads exactly **one** structured field from a mission today: `model:`. Everything else
   is prose the model parses. So the injection seam exists (the paste-seed) but carries almost no
   structured per-role data.

5. **Drift root cause is known and is NOT a prose problem.** `admiral-framework` (2026-06-25)
   diagnosed the failure as a **wrong frame that self-reinstantiates verbatim through every keeper
   `/clear`** — self-check questions fire but are answered through the already-wrong frame. Fix =
   re-inject a durable, external identity each resume; do NOT add more principle-text. This is the
   direct lever for C7 (admiral↔captain 6h overlap).

## Existing primitives to build ON (don't reinvent)
- `crew.Record` (`internal/crew/registry.go`) — atomic file-backed per-crew CRUD. Add a `role` field.
- `crew-handoff-schema.md` — a specified 6+1-field mission front-matter. Extend it.
- `harnesses.pi.*` in config.yaml — the precedent for **central-YAML per-agent-type injection**.
- `watch.*_target` — precedent for **per-role routing** config.
- The paste-seed (`pasteCrewMission`) + KEEPER-IDENTITY block (`cycle.go:456`, re-pinned to HANDOFF
  on every restart) — the two existing **injection points** to layer onto.
- The **canonical-home + one-line-pointer** idiom (orchestrator-rules §Autonomy) — how to state a
  rule once and point every role at it, instead of copy-paste.

## Design direction (the spine)

**A "role" becomes a declared thing with three separable layers, assembled at launch/resume:**

- **Layer 1 — SOUL / mission (durable identity):** who am I, what's my job, what I do NOT do, who I
  escalate to. Per-role, versioned, checked in. (Generalizes the scattered skill-§0 + mission
  front-matter + the "admiral directs · captain drives · crew executes" split.)
- **Layer 2 — operating instructions (durable how):** the skills + knowledge THIS role loads —
  nothing more. (Generalizes admiral-playbook + the per-role load map; the fix for C5 = move the
  UBS + Beads blocks out of shared AGENTS.md into the roles that need them.)
- **Layer 3 — handoff (ephemeral state):** what happened last session, open items. (Already exists:
  HANDOFF-<name>.md + session-handoff/resume.)

**Assembly:** a per-role config entry (extending crew.Record + mission schema, possibly a
`roles.yaml` registry) tells the launcher, for any role by name: which SOUL, which operating-
instruction set (skill list), and which handoff file — then the launcher injects all three via the
existing paste-seed (and captain gets a seed too, closing the asymmetry). Layer 1 is re-pinned on
every keeper `/clear` the way KEEPER-IDENTITY already is — that's the anti-drift mechanism.

**Pluggability falls out for free:** if role = a named config entry (SOUL + skills + handoff),
then mayor/governor/new-role = add an entry. captain stays the one hardcoded `start` verb
(back-compat), but even it resolves its SOUL/skills through the same registry.

## Open questions for the operator (see plan README §"decisions")
- Is a **`roles.yaml` registry** the right home, or extend per-mission front-matter + crew.Record?
- How hard is **captain's** first-class status — keep the `start captain` verb but route its identity
  through the registry, or leave captain fully special?
- Do we want **profile-as-forked-identity** (Hermes-style own skills/keys dir) or just declarative
  role config over the shared repo? (Credential isolation is a separate want — idea-2/idea-3.)
- Layer-2 scoping: prune AGENTS.md now (move UBS/Beads blocks out) as an independent quick win, or
  fold it into the bigger role-registry work?
