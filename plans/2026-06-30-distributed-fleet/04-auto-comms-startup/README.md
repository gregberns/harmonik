# Idea 4 — Auto-listen-to-comms on startup

**Scoping stub.** Date 2026-06-30. Small but high-leverage; sequenced right after idea 1 because it
removes boot friction that gets worse as node/model diversity grows.

---

## The vision (operator, distilled)

> "With Pi, can we force the agent to listen to comms on startup? Right now the crew need to set up
> the comms connection — it would be nice to not have that. We should also see if this could be done
> with Claude Code too — maybe there's a way."

The ask: make "subscribe to the comms bus" a **boot property of the agent process**, not a thing the
agent's prompt/skill has to manually wire up each launch.

---

## Important nuance: a normative version already exists — but it's *prompt-driven*, not *enforced*

Auto-subscribe-on-boot is **already a landed, normative behavior for Claude crews** — but it lives
in the *agent's instructions*, not the *harness*:

- `docs/plans/captain/SPEC.md:69` — a spawned crew "auto-subscribes to its comms inbox ... with no
  human steps."
- `docs/plans/captain/05-specs/c3-spec.md` **AC-1** — "On boot the crew runs `harmonik comms join`
  ... and `harmonik comms recv --follow --json`, maintains a `seen` set keyed on `event_id`."
- Encoded in the **`crew-launch` skill** boot sequence: "join comms, mirror assignee, subscribe
  inbox."

**The gap the operator is pointing at:** this is the *agent choosing to run those commands because
its prompt told it to.* It can be skipped, mis-ordered, or lost on a context-clear, and it depends
on the agent's cooperation. The operator wants it to be **forced** — wired by the launcher/harness
so the connection exists regardless of what the model does. That's a real, different thing:
**declarative/enforced subscription vs. prompt-driven subscription.**

This also connects to known wake gaps (MEMORY: "idle crews don't wake on a bare comms send";
"comms --wake can't reach captain") — a transcript already shows a crew hand-rolling *a background
watcher that exits on a new message to re-invoke itself.* That workaround is exactly what should be
a harness primitive instead of a per-agent improvisation.

## The two surfaces

1. **Pi.** Greenfield — we control the launch. The shim (idea 3) can `harmonik comms join` +
   spawn the `recv --follow` watcher as part of *process startup*, before the model gets control.
   Cleanest place to make subscription a true boot property.
2. **Claude Code.** Harder — we don't own the process internals, but we have launch-time seams:
   the bracketed-paste seed, `--remote-control`, session hooks/`settings.json`, and the
   keeper's resume cycle. Question for the sketch: can a **hook** (SessionStart / on-resume) own the
   subscription so it survives `/clear` without relying on the prompt? The update-config / hooks
   surface is the likely lever.

## Open questions (for the full sketch)

- Can subscription be a **harness/launcher responsibility** (a sidecar `recv --follow` process the
  launcher owns) rather than an in-session command, for both Pi and Claude Code?
- For Claude Code: does a SessionStart hook reliably re-establish the watcher across the keeper's
  `/clear` → `/session-resume` cycle (where `--session-id` is known to flip)?
- Should the "background watcher that re-invokes the agent on a new message" pattern become a
  first-class `harmonik comms watch` primitive instead of a per-agent improvisation?
- Does enforcing subscription at the harness make the prompt-driven `crew-launch` step redundant, or
  do they belong as belt-and-suspenders?

**Status: stub — flesh out after idea 1; it's a prerequisite-enabler for idea 3 (Pi crew).**
