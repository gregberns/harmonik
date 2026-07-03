# 03 — Decompose: components (Route 1)

Route 1 is small and decomposes into one engine capability plus authoring +
verification artifacts. Single-area change (`internal/daemon` + `specs/examples`).

## C1 — Per-bead env for non-agentic tool nodes  (the only engine change)

Expose the current bead's identity to shell tool nodes so `tool_command` can
reference it. In `driveDotWorkflow` (`dot_cascade.go`), where `gateEnv` is built
(~436), layer additional env from the in-scope `beadID`/`beadTitle`/
`beadDescription` and pass to `dispatchDotToolNode`.

- Env keys (proposed): `HK_BEAD_ID`, `HK_BEAD_TITLE`, `HK_BEAD_DESCRIPTION`.
  Mirrors the `HK_GATE_BASE_SHA` precedent (same seam, same prefix convention).
- Additive: existing shell nodes (commit_gate) gain ignored vars; no behavior
  change for them.
- Must survive the remote login-shell `export K=V` path (it does, by
  construction — values are exported before the command).
- Edge cases: title/description may be empty (set empty var, not unset); may
  contain newlines/quotes (env values are safe; the burden is on the
  `tool_command` author to quote references).

## C2 — Opt-in example workflow with the cm step

A new example DOT (e.g. `specs/examples/cm-context-bead.dot`, or an opt-in
variant) demonstrating the pattern: a `load_context` non-agentic shell node
between `start` and `implement` that runs `cm context` and writes
`.harmonik/cm-lessons.md`, plus an `implement` node whose `role` directs the
agent to read that file first. This is the canonical, copyable opt-in.

## C3 — cm output shaping convention

Decide and document how `cm context --json` becomes the lessons file: simplest is
to write the relevant bullets as markdown. Options (design-pass pick):
(a) shape inside the `tool_command` (jq/python one-liner json→markdown), or
(b) write raw `--json` and let the agent parse. Recommendation: (a) a tiny
formatter so the agent sees clean markdown; keep it dependency-light.
Best-effort wrapper (`… || true`) is part of this convention.

## C4 — Tests

Per analysis Area E:
- env-reaches-command (HK_BEAD_* visible to `tool_command`).
- best-effort (non-zero / missing cm under `|| true` → SUCCESS, cascade advances,
  worker launches).
- off-path-unchanged (graph without the cm node → byte-identical agent-task.md).
- example/e2e (opt-in workflow produces `.harmonik/cm-lessons.md`; cm faked on
  PATH).

## C5 — Docs / deployment note

Short note: the daemon's PATH must include `cm` (~/.local/bin) for the node to
resolve it; missing cm is non-fatal (`|| true`). How to opt a workflow in (copy
the `load_context` node + implementer role line). Points to the Route 2 idea doc
(`docs/ideas/node-context-passing.md`) as the durable successor.

## Dependency order

C1 (engine) → C2/C3 (authoring, depend on C1's env) → C4 (tests, depend on
C1+C2) → C5 (docs, last). C1 is the only piece that gates the others.
