// flywheel — v0 scaffold of the harmonik self-managing orchestrator extension.
//
// What this version does:
//   - registers a `note` tool (durable notes → .harmonik/cognition/notes.jsonl)
//   - registers a `reset_context` tool (deferred reset at turn boundary)
//   - injects context-fullness % into every model call (MemGPT 70/90/100 pattern)
//   - on turn_end at >=100% fullness, forces the deferred reset
//
// What this version does NOT yet do (intentionally — wired in as harmonik grows):
//   - call `harmonik digest` to build the status sheet (Go subcommand to be built)
//   - subscribe to `harmonik subscribe` for event ingestion
//   - implement multi-LLM stratification via prepareNextTurn
//   - manage the loop-singleton lock
//   - render the custom TUI status panel
//   - per-event-class wake filtering / budget kill-switch
//
// Design source of truth:
//   /Users/gb/.kerf/projects/gregberns-harmonik/flywheel/04-design/self-managing-architecture.md

import { Type } from "typebox";
import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";
import { appendFileSync, mkdirSync } from "node:fs";
import { join } from "node:path";

const REPO_ROOT = process.cwd();
const NOTE_FILE = join(REPO_ROOT, ".harmonik/cognition/notes.jsonl");
const COGNITION_DIR = join(REPO_ROOT, ".harmonik/cognition");

let pendingReset: { reason: string; instructions?: string } | null = null;
let lastWarnedThreshold = 0; // so we nudge once per crossing, not every turn

export default function activate(pi: ExtensionAPI) {
  // ── note ─────────────────────────────────────────────────────────────
  pi.registerTool({
    name: "note",
    label: "Note",
    description:
      "Record a durable cognition note (decision/hypothesis/warning/defer). Survives compaction and context reset.",
    promptSnippet:
      "note(kind, refs, text) — record a durable decision/hypothesis/warning/defer",
    promptGuidelines: [
      "Call `note` whenever you make a non-trivial decision, form a hypothesis worth checking later, hit a warning, or defer a sub-task.",
      "`refs` should cite bead IDs (hk-XXX), file paths, or commit SHAs.",
    ],
    parameters: Type.Object({
      kind: Type.Union([
        Type.Literal("decision"),
        Type.Literal("hypothesis"),
        Type.Literal("warning"),
        Type.Literal("defer"),
      ]),
      refs: Type.Array(Type.String()),
      text: Type.String({ description: "Single paragraph, no markdown headers." }),
    }),
    async execute(toolCallId, params, _signal, _onUpdate, ctx) {
      const row = {
        ts: Date.now(),
        toolCallId,
        ...params,
        session: ctx.sessionManager.getSessionFile(),
      };
      mkdirSync(COGNITION_DIR, { recursive: true });
      appendFileSync(NOTE_FILE, JSON.stringify(row) + "\n");
      ctx.appendEntry?.("harmonik.note", row); // mirror into live session journal
      return {
        content: [{ type: "text", text: `noted (${params.kind})` }],
        details: row,
      };
    },
  });

  // ── reset_context (deferred) ─────────────────────────────────────────
  pi.registerTool({
    name: "reset_context",
    label: "Reset Context",
    description:
      "Request a deferred context reset. Fires at the next turn boundary; does NOT abort the current turn.",
    promptSnippet:
      "reset_context(reason) — request a deferred save+reseed at the next turn boundary",
    promptGuidelines: [
      "Call this when context-fullness is high and you've recorded the open decisions/hypotheses you need to carry forward via `note`.",
    ],
    parameters: Type.Object({
      reason: Type.String(),
      instructions: Type.Optional(Type.String()),
    }),
    async execute(_id, params) {
      pendingReset = params;
      return {
        content: [
          { type: "text", text: "reset queued; will fire at the next turn boundary" },
        ],
        details: params,
      };
    },
  });

  // ── context: per-model-call fullness injection (read-only) ──────────
  pi.on("context", async (event, ctx) => {
    const u = ctx.getContextUsage?.();
    if (!u || u.percent == null) return;
    const pct = Math.round(u.percent);
    const line =
      pct >= 100
        ? `[context ${pct}% — CRITICAL: harness will force-save next turn]`
        : pct >= 90
          ? `[context ${pct}% — call \`reset_context\` THIS turn]`
          : pct >= 70
            ? `[context ${pct}% — save important notes via \`note\`; call \`reset_context\` soon]`
            : `[context ${pct}%]`;
    return {
      messages: [
        ...event.messages,
        {
          role: "user",
          content: [{ type: "text", text: line }],
          timestamp: Date.now(),
          metadata: { ephemeral: true },
        },
      ],
    };
  });

  // ── turn_end: thresholds + deferred reset trigger ───────────────────
  pi.on("turn_end", async (_event, ctx) => {
    const u = ctx.getContextUsage?.();
    const pct = u?.percent ?? 0;

    // Forced floor: 100% → queue a save+reseed regardless of agent action.
    if (pct >= 100 && !pendingReset) {
      pendingReset = { reason: "100%-floor (auto)" };
    }

    // Single-shot 90% nudge per crossing.
    if (pct >= 90 && lastWarnedThreshold < 90) {
      lastWarnedThreshold = 90;
      pi.sendUserMessage?.(
        "Context is at 90%. Save any open notes via `note`, then call `reset_context`.",
        { deliverAs: "followUp" }
      );
    } else if (pct < 70) {
      lastWarnedThreshold = 0; // re-arm after a reset drops us back down
    }

    // Deferred reset dispatch.
    if (pendingReset) {
      const reason = pendingReset.reason;
      pendingReset = null;
      pi.sendUserMessage?.(
        `/reset --reason="${reason.replace(/"/g, '\\"')}"`,
        { deliverAs: "followUp" }
      );
    }
  });

  // ── /reset: hard reseed via newSession (lives on command context) ───
  pi.registerCommand?.("reset", {
    description: "Hard reset: open notes survive; reseed a new session from the digest.",
    handler: async (_args, ctx) => {
      // v0: minimal digest — the production version will call `harmonik digest`.
      const digest = buildMinimalDigest();
      await ctx.newSession?.({
        parentSession: ctx.sessionManager?.getSessionFile() ?? undefined,
        withSession: async (rep) => {
          rep.sendUserMessage?.(digest, { deliverAs: "followUp" });
        },
      });
    },
  });
}

function buildMinimalDigest(): string {
  // TODO(v1): shell out to `harmonik digest --json` and render.
  return [
    "## Resumed from prior session",
    "(v0 scaffold — `harmonik digest` not yet implemented; read your open notes via the filesystem at `.harmonik/cognition/notes.jsonl`.)",
    "",
    "Continue from: review open notes, then resume the active priority work.",
  ].join("\n");
}
