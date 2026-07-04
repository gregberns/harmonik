# Vision 2 — The Provisioning Model: agents declare capabilities, adapters provision them

## (a) Real per-harness injection surface (from launchspec + adapter code)
Lifecycle adapters (`adapter_*.go`) only handle ready/rate-limit/exit — **prompt injection lives in
`internal/daemon/*launchspec.go`**.
- **claude** (`claudelaunchspec.go`): a *headless REPL*. Three seams: (1) **ambient filesystem** —
  `CLAUDE.md` + `.claude/skills/` auto-discovered by the REPL, harmonik injects nothing; (2)
  **pre-exec messages** — 4 ordered NDJSON messages via `PreExecMessages(...)`, where `skills=nil`
  today; (3) CLI flags `--model`/`--effort`/skip-perms. Long-lived; `/clear` wipes conversation but re-reads `CLAUDE.md`.
- **codex** (`codexlaunchspec.go`): a *one-shot* `codex exec --json -C <wt> <seedPrompt>`. The
  **only** per-run injection is the seed-prompt string. Ambient behavior from `AGENTS.md` (codex's
  own auto-read convention). No pre-exec channel, no skills, no `--effort`. Continuity via `exec resume`.
- **pi** (`pilaunchspec.go`): also *one-shot* `pi --mode json --provider <p> --model <p/id>
  <seedPrompt>`. Seed-prompt is the injection; ambient behavior from git-tracked **`.pi/extensions/`**
  (the flywheel fork-bomb lives here — auto-loaded, unskippable = the EMBODIED failure mode).

**Asymmetry that kills "skills = operating instructions":** only claude has skills. codex/pi have
exactly one injection lever (a prompt string) + one ambient file read by their own convention. Any
portable role model must treat "a skill" as **one realization**, not the unit.

## (b) The capability manifest (harness-agnostic)
A role declares a flat list of **capability requests**, never a mechanism. Each = `{kind, ref, presence, harness_hint?}`:
```yaml
role: reviewer
requires:
  - {kind: identity,     ref: souls/reviewer.md,       presence: injected}
  - {kind: instruction,  ref: skills/agent-reviewer,   presence: injected}
  - {kind: instruction,  ref: skills/beads-cli,        presence: retrieved}
  - {kind: knowledge,    ref: docs/build-practices.md, presence: retrieved}
  - {kind: tool,         ref: br,                       presence: embodied, scope: read-only}
  - {kind: guardrail,    ref: no-terminal-transitions,  presence: embodied}
```
`kind` = what it *is* (identity/instruction/knowledge/tool/guardrail). `presence` = the
**injected/retrieved/embodied** contract. `ref` = content address into a single store, resolved
identically for every harness. The manifest is the *only* thing a role author writes — pure
declaration, zero harness knowledge.

## (c) One role → three realizations, via a per-harness Provisioner
A `Provisioner` interface (sibling to `Adapter`, one impl per harness) consumes the manifest and
emits that harness's launch reality. Same `reviewer` manifest:

| request | claude | codex | pi |
|---|---|---|---|
| `identity` (injected) | prepend to pre-exec `skills_provisioned` + pin into paste-seed | concatenate into seed-prompt | concatenate into seed-prompt |
| `instruction agent-reviewer` (injected) | symlink into `.claude/skills/` | **render skill body → seed prose** | render → seed prose |
| `instruction beads-cli` (retrieved) | on disk; seed says "load when you touch br" | one-line pointer in `.harmonik/agent-task.md` | pointer |
| `knowledge` (retrieved) | on-disk, referenced | pointer | pointer |
| `tool br read-only` (embodied) | `--allowedTools`/MCP scope | wrapper `br` on PATH rejecting writes | same wrapper |
| `guardrail` (embodied) | allowlist denies write | wrapper enforces | wrapper enforces |

The Provisioner is where "skills are claude-only" stops leaking: on codex/pi an `instruction` is
**compiled down to seed prose**; on claude it stays a skill. Role author never sees the difference.
Closes SYNTHESIS fact #4 — the manifest is the structured per-role payload the daemon lacks (reads
only `model:` today).

## (d) The injected / retrieved / embodied split, applied
- **INJECTED** = pushed at boot **and re-pushed on every resume/`/clear`**; survives reset; costs
  permanent context weight. → **IDENTITY lives here, always** (fact #5: drift is a wrong frame that
  self-reinstantiates through `/clear`; fix is durable external re-injection). KEEPER-IDENTITY is the prototype — generalize to all roles/harnesses.
- **RETRIEVED** = on disk, pulled on demand; zero boot cost; guaranteed *reachable*, not *present*.
  → **REFERENCE KNOWLEDGE + most operating-instructions.** Honors the crew-context-token mandate (keep `.ctx` low) without starving the role.
- **EMBODIED** = baked into tooling/guardrails the model **cannot skip or forget** (allowlist, `br`
  wrapper refusing terminal transitions, sandbox). → **Non-negotiable guardrails** ("never pre-set
  in_progress" belongs in the wrapper, not prose a `/clear` drops). `.pi/extensions` fork-bomb is the
  *anti*-pattern: embodied-by-accident, unskippable → must be *declared*, not inherited from a git dir.

**Load-bearing rule:** identity=injected, operating-instructions=mostly retrieved, reference=retrieved, guardrails=embodied.

## (e) Constraint to DELETE
**"Operating-instructions are `.claude/skills/` — the ambient-filesystem convention IS the
injection mechanism."** Today `CLAUDE.md`/`AGENTS.md`/`.pi/extensions/` each carry role behavior by
each harness's auto-read convention → behavior is claude-shaped AND invisibly global (every agent on
the box inherits the same `AGENTS.md`; the fork-bomb rides in on `.pi/extensions`). Make those files
**generated artifacts** the Provisioner writes per-run from the manifest (or a scoped overlay), not
authored surfaces inherited wholesale. Then "which skills a role loads" → "which capabilities a role
*requests*," and the same role runs identically on all three harnesses.
