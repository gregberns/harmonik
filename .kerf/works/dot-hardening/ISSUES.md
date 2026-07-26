# ISSUES — for the operator to decide at the end

> Significant issues surfaced while carrying the DOT redesign through to tasks. Each is stated as
> one decision, with options, each option's consequence, and a recommendation. Nothing here blocks
> the spec drafts from being written against the recommendation — but each is reversible and yours.
> Cross-ref `DECISIONS.md` (the items marked [OPERATOR]) and the adversary reviews in the spike.

---

## Issue 1 — How the per-node model/effort/tool override is serialized in the bead

You sketched `implement@model=claude/opus;review@model=…` and said it was throwaway — "there are
much better serialization options." The design needs a concrete one. The override must: name a node
id (must line up with the graph), carry `{tool, model, effort, locked}` per node, live in the bead,
and survive `br` round-trips.

- **Option A — a fenced structured block in the bead body** (a `harmonik` YAML/TOML block parsed
  from the bead description). *Consequence:* proper structured data — clean for per-node bundles
  (`implement: {model: opus}`, `review: {model: sonnet, effort: high}`, `qa: {tool: codex}`), room to
  grow, no string-munging. Cost: a new parse surface on the bead body; one more thing that can be
  malformed (needs a clear load error).
- **Option B — node-addressed labels** (`model@implement=opus`, `effort@review=high`). *Consequence:*
  reuses the existing label scan, minimal new code. Cost: it is essentially the `@` string you
  rejected, just as labels; label grammar grows a node-address dimension and conflict rules; awkward
  for bundling three properties on one node.
- **Option C — keep only the flat run-wide `model:<alias>` label; no per-node override in the bead.**
  *Consequence:* simplest; but drops the per-node-in-the-bead capability you asked for — you'd have to
  edit the `.dot` to vary a single node, which is the pain that started this.

**Recommendation: A** (fenced structured block), with the flat `model:<alias>` label retained for the
simple run-wide case. It's the "proper serialization" you asked for and bundles per-node config
cleanly. The bare label stays the easy path.

**Extra weight for Option A (surfaced by the design review):** the same structured block is the clean
home for an optional per-bead **`rubric`** field kept *separate from the `task`* — which is what makes
the leak fix structural (see the leak-boundary note below). One structured surface answers both the
model override AND the task/rubric split. That tips the recommendation further toward A.

---

## Issue 2 — Does a per-run force override a node marked "keep cheap" (locked)?

A node can be marked `locked` so escalation skips it (e.g. a trivial formatting node that must stay
on the cheap model even for a hard bead). There are two escalation sources: a per-bead label and a
per-run `--force-model`. The question is only about the interaction with the lock.

- **Option A — force overrides the lock; a per-bead label respects it.** *Consequence:* the operator
  doing a one-shot `--force-model` gets exactly what they typed (a force is a deliberate override); the
  author's lock still protects the node from routine per-bead escalation. Two different intents, two
  different answers.
- **Option B — lock always wins (even over force).** *Consequence:* the lock is absolute; an operator
  cannot force a locked node without editing the graph. Surprising for a top-tier "force."
- **Option C — force and label both override the lock.** *Consequence:* the lock only sets the default,
  never resists escalation — which makes "locked" barely different from an unlocked node default.

**Recommendation: A.** Force = operator override (wins); label = routine escalation (respects lock).

---

## Issue 3 — Ladder-flip migration risk (mostly mitigated; confirm)

Flipping the priority so a per-bead escalation beats the graph's node default changes how a bead
carrying a model label resolves. Under the chosen design the **bare `model:opus` label stays run-wide**
(your decision this session), so the common case is unchanged. The only behavior change is: a `.dot`
node `model=sonnet` no longer overrides a per-bead `model:opus` — the bead now wins (which is the
whole point of the flip).

- **Consequence if any deployed bead relied on a node `model=` winning over its label:** that bead
  would now escalate where it previously did not. Beads are OFF this phase, so the live blast radius is
  almost certainly nil.
- **Decision:** confirm there is no deployed bead + graph pair that deliberately relies on the old
  "node default wins over the label" behavior. If unsure, a one-line grep of `.dot` `model=` against
  any `model:`-labelled bead before landing settles it.

**Recommendation:** proceed with the flip; treat this as a pre-land grep check, not a blocker.

---

## Issue 4 — Accepted limitation: claude's resume feedback can't be argv-asserted in the test harness

The forceable round-trip harness records the real per-tool argv and asserts it. For pi and codex the
resume feedback rides in the argv (a positional seed), so the harness proves it faithfully. **For
claude, the resume feedback is delivered by pasting into the terminal pane (tmux), not via argv** — the
recorder cannot see it, and the twin's substrate isn't tmux-backed, so that delivery path is a no-op
under the harness. So the claude row proves "the daemon assembled and wrote the right brief," and
claude's actual delivery stays proven by its existing unit tests + live runs.

- **Option A — accept it.** *Consequence:* the harness honestly proves daemon-side brief construction
  for all three tools and argv-faithful delivery for pi/codex; claude delivery keeps its current
  (unit + live) proof. No extra work. The spec states the split plainly.
- **Option B — build a paste-inject capture seam** so claude's delivery is also asserted in-harness.
  *Consequence:* full symmetry, but new test infrastructure for the one path that is hardest to fake,
  for marginal added confidence over the existing unit tests.

**Recommendation: A** (accept, state plainly). Revisit B only if claude's resume delivery regresses
again.

**Update from the integration pass — this may already be softer than stated.** The design carried the
"claude delivers by tmux paste-inject" assumption from the research. But the M2 `agent-input-substrate`
work already superseded paste-inject for daemon-run input with the **AIS structured input port**
(`process-lifecycle.md` PL-021d, `agent-input.md` AIS-011). If claude's resume feedback now goes through
the AIS port rather than a tmux paste, it may be **capturable in the harness after all** — which would
make Option B cheap or unnecessary and could close this limitation entirely. Action folded into task
T-F2: re-check claude's actual delivery path against AIS before writing the final HC honesty caveat. Not
a decision you need to make now — flagging that the limitation's severity is now uncertain (possibly nil).

---

## Leak-boundary note — resolved in design, worth your awareness

The design review flagged that "the leak is structurally unexpressible" needs a precise boundary, and
the fix is now specified (`DECISIONS.md` D-FIX-1). What the system guarantees: **one role's
separately-sourced instructions cannot reach another role** — the reviewer's rubric (the reviewer node's
prompt + an optional per-bead `rubric` field) is a distinct source that the implementer never declares,
so it cannot appear in the implementer's brief. What it does NOT guarantee: it cannot stop an author who
writes review criteria into the *task text itself* — that is telling the implementer directly, not a
cross-role leak (putting the answer in the question). Today's generic reviewer checklist (hardcoded in
Go) moves to the reviewer node's prompt, so the reviewer keeps its checklist. No decision needed — noted
so the guarantee isn't mis-read as "the system prevents all over-sharing."

## Not an issue — resolved this session (recorded so it isn't re-opened)

- **Escalation scope** (was MODEL.md's one open decision): per-node addressing by node id is primary;
  a bare/flat form means run-wide. You answered this directly.
- **Per-node config in a bead is NOT version pinning.** Version pinning = hardcoding a tool/model
  version in the codebase (the br mistake). A model/tool alias in a bead is per-task config and is
  fine.
