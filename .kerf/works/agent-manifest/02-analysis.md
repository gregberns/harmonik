# 02 — Analysis

## The reframe
**An agent is not a role-bundle. It is a *character* (role) cast on an *actor* (harness), declared as
a config entry over orthogonal axes.** "Role" stops being a noun you assemble by copy-paste and
becomes a point in configuration space. Renaming captain→mayor, swapping claude→pi, muting a timer —
each moves ONE axis and leaves the rest untouched.

Confirmed shape (DIRECTION §A): a **folder per agent type**, holding static content files (SOUL,
operating-instructions) + a **tie-together `manifest.yaml`** declaring *what + where + how-present*.
The system parses the yaml and pulls in only what each launch needs. This IS the capabilities manifest.

The orthogonal axes a manifest declares (POSSIBILITY-SPACE): **identity/SOUL, capabilities,
triggers, lifecycle/keeper, cardinality, memory-horizons, markers.** This work builds the axes that
serve identity + boot injection now; memory-horizons/markers/publish are deferred (see §out-of-scope).

## The two invariants

### I1 — The provenance rule (the drift fix)
**Identity must be re-expressed on every restart from an EXTERNAL, IMMUTABLE master — never copied
from the outgoing session's own handoff.** Today the keeper fuses the two (`cycle.go` appends the
identity block *into* the HANDOFF the resume reads), so a session re-seeds from its own drifted state
and propagates drift forever. Fix is a rule on the **injection seam**, not better prose (which prior
art proves fails — admiral-framework Part 0: "more principle-text will NOT work"):

> The boot command re-reads SOUL from the type folder's `soul.md` master. HANDOFF supplies ONLY
> ephemeral episodic state. They are never fused.

This falls out naturally: on keeper `/clear`, the same boot command re-runs; SOUL always comes from
`soul.md`; the handoff carries only "what happened last session." No Xerox-of-a-Xerox.

### I2 — Harness-portable, emit-markdown-only
**The boot command emits harness-agnostic Markdown text the agent reads — it decides WHAT context is
present, not HOW a harness realizes it.** Operator-confirmed (MANIFEST §7): **no side effects** — no
symlinking, no seed-writing, no per-harness provisioner. The emitted document is the agent's boot
prompt, well-laid-out, and works identically for claude/codex/pi. Skills appear in the document as a
**short description + a pointer** to the full instruction set (pulled on demand → context stays lean),
NOT as inlined bodies and NOT as claude-only skill files. This supersedes the earlier
"symlink real skill files / compile-to-prose per harness" idea entirely.

## Keyed to the confirmed decisions
- **DIRECTION §A** → C-A (type folder + manifest schema).
- **DIRECTION §B** (model today's reality: crew first, then admiral, then wire captain) → C-G/C-H +
  the build order; anti-drift via the provenance rule (I1), not live monitoring.
- **DIRECTION §C** (open-ended vocabulary) → new folder = new type, no enum, no code change.
- **MANIFEST §7** (name-resolution, no side effects, scoped harmonik-supplied skills, light contract,
  format flags, override flag, schema-check as its own verb, shared skills folder, the boot-doc ORDER)
  → C-B, C-C, C-D, C-F.
- **DIRECTION §D** (embodied guardrails deprioritized; alignment-watcher as a configured type) →
  markers stay declarative-only; watch is just another folder (C-G).

## Out of scope (deferred)
Publish seam / fleet-state store / dashboard data / self-message note-taking / memory-horizon LEDGER
store → `plans/2026-07-03-fleet-state-and-dashboard-data/`. Self-extending capabilities
(watch writes its own `tools/`) is noted as a manifest affordance only, designed later. Embodied
guardrails (rules-as-code) are deprioritized per DIRECTION §D.
