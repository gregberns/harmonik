# 05 — Change Spec: cm-context-inject (Route 1)

Normative spec for the build. Sections marked **MUST** are contract; **SHOULD**
are recommended defaults a reviewer may adjust.

## S1 — Engine: per-bead env for non-agentic tool nodes

**File:** `internal/daemon/dot_cascade.go`, shell tool-node branch in
`driveDotWorkflow` (env construction ~line 436, before the `dispatchDotToolNode`
call).

**MUST:** layer the following env vars onto the env passed to
`dispatchDotToolNode`, sourced from the in-scope `beadID`, `beadTitle`,
`beadDescription`:
- `HK_BEAD_ID` = string(beadID)
- `HK_BEAD_TITLE` = beadTitle
- `HK_BEAD_DESCRIPTION` = beadDescription

**MUST:** preserve existing behavior — keep `HK_GATE_BASE_SHA` layering; the new
vars are additive. Build the slice without mutating `deps.handlerEnv` (copy, as
the existing `HK_GATE_BASE_SHA` branch does).

**MUST:** apply to BOTH local and remote (login-shell `export`) tool-node paths —
satisfied automatically because both consume the single `env` argument.

**MUST:** set empty vars (not unset) when a field is empty, so `tool_command`
references are deterministic.

**MUST NOT:** add any credential/secret env var. Only the three bead fields.

**Non-goal:** no `tool_command` string interpolation; no structured output
capture (that is Route 2).

## S2 — Example opt-in workflow

**File (new):** `specs/examples/cm-context-bead.dot` (a copy of the standard bead
graph with the cm step added; do not modify `standard-bead.dot`).

**MUST** include a non-agentic shell node between `start` and the implementer:
```dot
load_context [
  type="non-agentic",
  handler_ref="shell",
  tool_command="<C3 command>",
  timeout="20"
];
start -> load_context;
load_context -> implement;
```
**MUST** give the `implement` node a `role` that directs the agent to consume the
lessons file, e.g.:
`role="If .harmonik/cm-lessons.md exists, read it first — it lists relevant lessons from past sessions. Then implement the bead's change; commit required."`

**MUST** remain opt-in: `standard-bead.dot` and all existing graphs are
unchanged, so workflows that don't reference this file behave identically
(success criterion 3).

## S3 — cm output shaping (C3)

**SHOULD** the `tool_command` be (single line in the DOT):
```sh
mkdir -p .harmonik && cm context "$HK_BEAD_TITLE
$HK_BEAD_DESCRIPTION" --json --limit 5 2>/dev/null \
| python3 -c 'import sys,json; d=json.load(sys.stdin).get("data",{}); \
b=d.get("relevantBullets",[]); a=d.get("antiPatterns",[]); \
print("\n".join(["# Relevant prior lessons\n"] \
+ ["- [%s] %s"%(x.get("type","rule"),x["content"]) for x in b] \
+ ["- [anti-pattern] %s"%x["content"] for x in a])) if (b or a) else None' \
> .harmonik/cm-lessons.md 2>/dev/null || true
```
**MUST:** be best-effort — the whole command ends in `|| true` so a missing/erroring
`cm`, missing `python3`, or empty result yields exit 0 (decision B). On empty
result the file is absent or empty; the implement node's `role` is conditional
("if exists").

**MUST:** pass only `$HK_BEAD_TITLE` / `$HK_BEAD_DESCRIPTION` to `cm` — no secrets.

**SHOULD:** `--limit 5`, `timeout="20"`. A reviewer may tune.

**Host assumption (document in C5):** `python3` and `cm` on the daemon's PATH.
Both absent → best-effort no-op, dispatch unaffected.

## S4 — Tests (`internal/daemon`, table/golden style)

**MUST** add:
1. **env-visible:** drive a graph with a shell node whose `tool_command` writes
   `$HK_BEAD_ID`/`$HK_BEAD_TITLE` to a worktree file; assert the file equals the
   dispatched bead's id/title.
2. **best-effort:** a shell node running a failing/missing command wrapped in
   `|| true` yields `OutcomeStatusSuccess`, the cascade advances to `implement`,
   and worker launch proceeds.
3. **off-path-unchanged:** a graph without the cm node produces a byte-identical
   `agent-task.md` seed vs the current baseline (golden compare).
4. **example/e2e (SHOULD):** run the `cm-context-bead.dot` flow with a fake `cm`
   on PATH that emits known JSON; assert `.harmonik/cm-lessons.md` contains the
   formatted lessons and the implement node's seed references it.

## S5 — Docs

**MUST** add a short note (new file under `docs/`, e.g.
`docs/components/.../tool-node-bead-env.md` or an addition surfaced in the example)
covering: the `HK_BEAD_*` env vars now available to shell tool nodes; how to
opt a workflow in (copy the `load_context` node + implement `role` line); the
`cm`/`python3` PATH requirement; pointer to `docs/ideas/node-context-passing.md`
as the durable Route 2 successor. Create new files only (daemon live).

## Success criteria mapping

- SC1 (node can call cm for the bead) → S1.
- SC2 (lessons file produced + agent reads it) → S2 + S3.
- SC3 (off-path byte-identical) → S2 opt-in + S4.3 golden.
- SC4 (cm absent/error → dispatch still succeeds) → S3 best-effort + S4.2.
- SC5 (harness parity) → S2/S3 worktree-file channel is harness-agnostic.

## Out of scope (restated)

Route 2 structured context passing; crew-launch injection; cm write-back; making
cm a hard dependency.
