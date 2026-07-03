# Research/Design — Pi (TS) extension internals + install + memory tooling

> Component: `pi-extension-internals-install-memory`. Round-3. Source: sub-agent (opus) deep-read of TS Pi (`@earendil-works/pi-coding-agent`), 2026-05-30. **Authoritative install + concrete extension code.**

## TL;DR
- **Install:** `npm install -g --ignore-scripts @earendil-works/pi-coding-agent` → CLI binary `pi`, Node ≥22.19. For SDK-only embedding: depend on `@earendil-works/pi-coding-agent` + `@earendil-works/pi-agent-core` + `typebox` in your extension's `package.json`.
- **Path layout:** project-local `./.pi/extensions/` AND global `~/.pi/agent/extensions/` — both auto-discovered (`*.ts` file, `*/index.ts` dir, OR `*/package.json` with `pi.extensions` field). Loader: `packages/coding-agent/src/core/extensions/loader.ts:573-579`; constants in `src/config.ts:461,485-491`. **CONFIG_DIR_NAME=`.pi`.**
- **Deferred reset confirmed:** agents do NOT call `ctx.compact()` mid-think. Canonical reseed = `ctx.newSession({parentSession, setup, withSession})` on `ExtensionCommandContext`. Trigger pattern: tool sets a flag → `turn_end` reads it → fires via `sendUserMessage("/reset", {deliverAs:"followUp"})` → command handler calls `newSession`. Reference: `examples/extensions/handoff.ts:175-184`.

## A — Authoring + install

**A1. Discovery (authoritative, from loader.ts:550-580).** Walks two roots: (1) `path.join(cwd, CONFIG_DIR_NAME, "extensions")` = `./.pi/extensions/`; (2) `path.join(agentDir, "extensions")` = `~/.pi/agent/extensions/` (override via `PI_CODING_AGENT_DIR`). Plus `extensions: [...]` in `~/.pi/agent/settings.json` resolved relative to cwd. **Layout:** `~/.pi/agent/` = global config (`settings.json`, `auth.json`, `models.json`, `extensions/`, `tools/`, `skills/`, `prompts/`, `themes/`, `sessions/`, `bin/`, `pi-debug.log`). `./.pi/extensions/` + `./.pi/skills/` = project-local auto-discovery; the rest is global.

**A2. Extension shapes** (loaded via `jiti` — no build step, three accepted forms per docs/extensions.md §Extension Styles):
1. Single file: `./.pi/extensions/my-ext.ts`
2. Dir w/ `index.ts`: `./.pi/extensions/my-ext/index.ts`
3. Dir w/ `package.json` carrying a `pi` manifest:
```json
{ "name": "my-ext", "dependencies": {...},
  "pi": { "extensions": ["./src/index.ts"] } }
```
Run `npm install` in that dir; entry imports from local `node_modules/`. Entry point = **default export of a factory** `(pi: ExtensionAPI) => void | Promise<void>`. Returning a Promise → pi awaits before `session_start` + provider-registration flush.

**A3. Install (authoritative commands):**
```bash
# CLI + SDK (required for both running and embedding):
npm install -g --ignore-scripts @earendil-works/pi-coding-agent     # bin: pi
# Per-extension deps (in ./.pi/extensions/<name>/package.json):
npm install typebox @earendil-works/pi-coding-agent @earendil-works/pi-agent-core
```
Node `>=22.19.0` (engines in `packages/agent/package.json`). CLI bin = `pi`.

**A4. Project init.** No `pi init`. Minimal scaffold = drop `./.pi/extensions/my-ext.ts` exporting a default factory. For deps-bearing extensions, add `package.json` with `pi.extensions` field per A2 + `npm install` in that subdir.

**A5. Reference samples** (all under `packages/coding-agent/examples/extensions/`):
- **`trigger-compact.ts`** (50 LOC) — threshold-driven `ctx.compact()` at `turn_end`. **Closest analog to MemGPT 70/90/100; we extend it.**
- **`handoff.ts`** (191 LOC) — `/handoff` command builds focused prompt via side LLM call, calls `ctx.newSession({parentSession, withSession})`. **The deferred-reset reference.**
- `todo.ts` — stateful tool, rebuilds state from `sessionManager.getBranch()` on `session_start` and `session_tree`.
- `custom-compaction.ts` — `session_before_compact` handler that replaces the default summarizer entirely.

**A6. Dev loop.** Loaded via jiti (TS direct, no build). Quick tests: `pi -e ./path/to/ext.ts`. Auto-discovered locations: `/reload` re-imports without restart. `pi.registerTool()` post-startup takes effect immediately w/o `/reload`.

## B — Concrete TS code

**B1. `note` tool** (registers via `ToolDefinition`, ref: `src/core/extensions/types.ts:426-473`):
```typescript
import { Type } from "typebox";
import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";
import { appendFileSync, mkdirSync } from "node:fs";
import { join } from "node:path";

const NOTE_FILE = join(process.cwd(), ".harmonik/cognition/notes.jsonl");

export default function(pi: ExtensionAPI) {
  pi.registerTool({
    name: "note",
    label: "Note",
    description: "Record a durable cognition note (decision/hypothesis/warning/defer) that survives compaction and session reset.",
    promptSnippet: "note(kind, refs, text) — record a durable decision/hypothesis/warning/defer",
    promptGuidelines: [
      "Call `note` whenever you make a non-trivial decision, form a hypothesis worth checking later, hit a warning, or defer a sub-task.",
      "`refs` should cite bead IDs (hk-XXX), file paths, or commit SHAs."
    ],
    parameters: Type.Object({
      kind: Type.Union([Type.Literal("decision"), Type.Literal("hypothesis"),
                        Type.Literal("warning"),  Type.Literal("defer")]),
      refs: Type.Array(Type.String()),
      text: Type.String({ description: "Single paragraph, no markdown headers." }),
    }),
    async execute(toolCallId, params, _signal, _onUpdate, ctx) {
      const row = { ts: Date.now(), toolCallId, ...params,
                    session: ctx.sessionManager.getSessionFile() };
      mkdirSync(join(process.cwd(), ".harmonik/cognition"), { recursive: true });
      appendFileSync(NOTE_FILE, JSON.stringify(row) + "\n");
      ctx.appendEntry("harmonik.note", row);   // also surface in live session journal
      return { content: [{type:"text", text:`noted (${params.kind})`}], details: row };
    },
  });
}
```

**B2. `reset_context` tool — deferred via flag + `turn_end`** (mid-turn `ctx.compact()` aborts; we hop through a command):
```typescript
let pendingReset: { reason: string; instructions?: string } | null = null;

pi.registerTool({
  name: "reset_context",
  label: "Reset Context",
  description: "Request a deferred context reset. Fires at the next turn boundary; does not abort the current turn.",
  promptSnippet: "reset_context(reason) — request a deferred save+reseed at the next turn boundary",
  parameters: Type.Object({
    reason: Type.String(),
    instructions: Type.Optional(Type.String()),
  }),
  async execute(_id, params) {
    pendingReset = params;
    return { content: [{type:"text", text:"reset queued; will fire at turn boundary"}], details: params };
  },
});

pi.on("turn_end", async (_event, ctx) => {
  if (!pendingReset) return;
  const { reason, instructions } = pendingReset; pendingReset = null;
  pi.sendUserMessage(`/reset --reason="${reason.replace(/"/g,'\\"')}"`, { deliverAs: "followUp" });
});

pi.registerCommand("reset", {
  description: "Hard reset: save digest + open notes, reseed new session.",
  handler: async (args, ctx) => {       // ExtensionCommandContext
    const digest = buildDigest(ctx);     // see B5
    await ctx.newSession({
      parentSession: ctx.sessionManager.getSessionFile() ?? undefined,
      withSession: async (rep) => {
        rep.sendUserMessage(digest, { deliverAs: "followUp" }); // headless reseed
      },
    });
  },
});
```
The split is required: `turn_end` runs in `ExtensionContext` (no `newSession`); `newSession` is on `ExtensionCommandContext`. We hop via `sendUserMessage` → `/reset` command. Same trick `examples/extensions/reload-runtime.ts` uses.

**B3. MemGPT 70/90/100 thresholds.** `context` event runs before each LLM call AND returns a replacement messages array (read-only injection):
```typescript
pi.on("context", async (event, ctx) => {
  const u = ctx.getContextUsage();
  if (!u || u.percent == null) return;                  // null right after compact
  const pct = Math.round(u.percent);
  const line = pct >= 100
    ? `[context ${pct}% — CRITICAL: harness will force-save next turn]`
    : pct >= 90
      ? `[context ${pct}% — call \`reset_context\` THIS turn]`
      : pct >= 70
        ? `[context ${pct}% — save important notes via \`note\`; call \`reset_context\` soon]`
        : `[context ${pct}%]`;
  // Inject AFTER cache_control marker as ephemeral user-role block:
  return { messages: [...event.messages, {
    role: "user", content: [{type:"text", text:line}], timestamp: Date.now(),
    metadata: { ephemeral: true },
  }] };
});

pi.on("turn_end", async (_e, ctx) => {
  const u = ctx.getContextUsage(); if (!u?.percent) return;
  if (u.percent >= 100) {                                // FORCE the floor
    pendingReset = { reason: "100%-floor" };
    pi.sendUserMessage("/reset --reason=auto-100", { deliverAs: "followUp" });
  } else if (u.percent >= 90) {                          // single nudge per crossing
    pi.sendUserMessage("Context is at 90%. Call `reset_context` now.", { deliverAs: "followUp" });
  }
});
```
70% line goes in EVERY model call via `context`; 90% = single nudge per crossing; 100% bypasses the agent entirely.

**B4. Persistence — recommend (b) separate disk file.** `ctx.appendEntry()` writes to the current session journal (survives compaction within the same session, but a `/reset` starts a fresh journal). For flywheel: notes MUST survive a complete process restart, `/reset`, and `/new`. → Write to `.harmonik/cognition/notes.jsonl` (durable source-of-truth) + mirror via `appendEntry` so the live model sees its own note this turn. The digest reads from the jsonl on reseed.

**B5. Digest on reseed (the prompt the model actually sees):**
```
SYSTEM PROMPT (stable, above cache_control marker — reused across reseeds)
  Pi base system prompt + harmonik extension promptGuidelines + tool list
  [cache_control breakpoint]
USER MESSAGE 0 (the digest — first message in new session)
  ## Resumed from prior session <id> @ <ts>
  ## Decisions made (kind=decision)
    - <ts>  <text>  refs: <hk-IDs/files>
  ## Open hypotheses (kind=hypothesis, unresolved)
  ## Warnings still active (kind=warning, not cleared)
  ## Deferred work (kind=defer)
  ## Current bead queue (from .harmonik/queue.json)
  ## Git head: <sha>  branch: <name>  status: clean|dirty
  ## Continue from: <one-paragraph "what I was about to do next">
```
`buildDigest(ctx)` reads `.harmonik/cognition/notes.jsonl` (filtering kind/active) + `.harmonik/queue.json` + `git log -1`/`status`. Digest as first user message → cache breakpoint sits between the stable system block and the digest → every subsequent turn re-reads cached system+tools, pays only for digest + conversation tail.

## Key file references (TS Pi)
`packages/coding-agent/src/core/extensions/loader.ts:550-599` (discovery); `src/config.ts:455-535` (CONFIG_DIR_NAME, getAgentDir); `src/core/extensions/types.ts:281-326,426-473,605-660,1135-1195` (ContextUsage, ToolDefinition, ContextEvent, appendEntry, getContextUsage); `examples/extensions/trigger-compact.ts`, `handoff.ts`, `custom-compaction.ts`, `reload-runtime.ts`; `docs/extensions.md`.
