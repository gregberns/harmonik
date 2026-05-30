// flywheel — harmonik self-managing orchestrator extension.
//
// What this version does:
//   - registers a `note` tool (durable notes → .harmonik/cognition/notes.jsonl)
//   - registers a `reset_context` tool (deferred reset at turn boundary)
//   - injects context-fullness % into every model call (MemGPT 70/90/100 pattern)
//   - on turn_end at >=100% fullness, forces the deferred reset
//   - multi-LLM stratification via prepareNextTurn (router.ts) — CL-070..CL-073
//   - per-day USD budget tracking with 80/90/100% graceful-downgrade + hard halt (budget.ts) — CL-090
//   - reaction-rate circuit breaker (circuit-breaker.ts) — CL-091
//   - starts the harmonik subscribe event bridge on activate (CL-060..CL-064);
//     torn down on session_shutdown so the subscribe child + watchdog interval do not leak
//
// What this version does NOT yet do (intentionally — wired in as harmonik grows):
//   - call `harmonik digest` to build the status sheet (Go subcommand to be built)
//   - manage the loop-singleton lock
//   - render the custom TUI status panel
//
// Design source of truth:
//   /Users/gb/.kerf/projects/gregberns-harmonik/flywheel/04-design/self-managing-architecture.md
// Spec: specs/cognition-loop.md

import { Type } from "typebox";
import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";
import { appendFileSync, mkdirSync } from "node:fs";
import { join } from "node:path";
import { prepareNextTurn, type Digest, type WakeEvent } from "./router.js";
import { BudgetTracker } from "./budget.js";
import { CircuitBreaker } from "./circuit-breaker.js";
import { createEventBridge, type Harness } from "./bridge.js";

const REPO_ROOT = process.cwd();
const NOTE_FILE = join(REPO_ROOT, ".harmonik/cognition/notes.jsonl");
const COGNITION_DIR = join(REPO_ROOT, ".harmonik/cognition");
const EVENTS_FILE = join(REPO_ROOT, ".harmonik/events/events.jsonl");

let pendingReset: { reason: string; instructions?: string } | null = null;
let lastWarnedThreshold = 0; // so we nudge once per crossing, not every turn

// Shared digest state: exception_flag set by harness to trigger a one-shot Opus turn.
const digest: Digest = { exception_flag: false };

// Initialized in activate() so env vars / config are available.
let budgetTracker: BudgetTracker;
let circuitBreaker: CircuitBreaker;

export default function activate(pi: ExtensionAPI) {
  const budgetUsdPerDay = process.env["FLYWHEEL_BUDGET_USD_PER_DAY"]
    ? parseFloat(process.env["FLYWHEEL_BUDGET_USD_PER_DAY"])
    : Infinity;
  const circuitThreshold = process.env["FLYWHEEL_CIRCUIT_THRESHOLD_PER_MIN"]
    ? parseFloat(process.env["FLYWHEEL_CIRCUIT_THRESHOLD_PER_MIN"])
    : 10;

  budgetTracker = new BudgetTracker({ limitUsd: budgetUsdPerDay, eventsFile: EVENTS_FILE });
  circuitBreaker = new CircuitBreaker({ thresholdPerMin: circuitThreshold, eventsFile: EVENTS_FILE });

  // ── prepareNextTurn: model stratification + budget + circuit-breaker ──
  pi.on("prepareNextTurn", async (event, _ctx) => {
    // Circuit breaker check.
    if (circuitBreaker.recordReaction()) {
      return { skip: true };
    }

    // Route based on wake cause from event metadata (fall back to "queue_empty").
    const wakeEvent: WakeEvent = {
      cause: (event as { cause?: string }).cause ?? "queue_empty",
    };
    let routingConfig = prepareNextTurn(digest, wakeEvent);

    if (routingConfig.skip) return { skip: true };

    // Apply budget pressure (may downgrade model or halt).
    const pressured = budgetTracker.applyPressure(routingConfig);
    if (pressured.halt) {
      return { skip: true };
    }
    routingConfig = pressured;

    const result: Record<string, unknown> = {};
    if (routingConfig.model) result["model"] = routingConfig.model;
    if (routingConfig.thinkingLevel != null) result["thinkingLevel"] = routingConfig.thinkingLevel;
    if (routingConfig.cacheNamespace) result["cacheNamespace"] = routingConfig.cacheNamespace;
    return result;
  });

  // ── event bridge ─────────────────────────────────────────────────────
  // CL-062 idle-vs-in-flight delivery: the bridge calls `prompt` when the
  // substrate is idle (a fresh user turn must be started) and `followUp` when
  // a turn is in flight (the message is queued onto the running turn). Pi's
  // sendUserMessage exposes `deliverAs: "steer" | "followUp"`; there is no
  // dedicated "start a fresh idle turn" mode, so idle delivery is a plain
  // sendUserMessage with no deliverAs (queues as the next user prompt), while
  // in-flight delivery uses deliverAs:"followUp". Keeping these two paths
  // distinct preserves the CL-062 idle-vs-in-flight semantic the bridge was
  // designed to honor instead of collapsing both.
  // TODO(pi-api): if Pi later exposes an explicit idle-dispatch mode, route
  // `prompt` to it; revisit if sendUserMessage's idle behaviour changes.
  const harness: Harness = {
    abort: () => (pi as unknown as { abort?: () => void }).abort?.(),
    prompt: (msg) => pi.sendUserMessage?.(msg),
    followUp: (msg) => pi.sendUserMessage?.(msg, { deliverAs: "followUp" }),
  };
  const bridge = createEventBridge(harness, { repoRoot: REPO_ROOT });
  bridge.start();

  // Resource-leak fix: tear the bridge down on session shutdown so the
  // `harmonik subscribe` child process, the watchdog setInterval, and any
  // pending reconnect timer are released. Without this, an extension
  // reload/shutdown leaks the subprocess + interval (reviewer flag: resource-leak).
  pi.on("session_shutdown", async () => {
    bridge.stop();
  });

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

  // ── turn_end: budget tracking + thresholds + deferred reset trigger ──
  pi.on("turn_end", async (event, ctx) => {
    // Record approximate spend for budget tracking.
    const usage = (event as { usage?: { input_tokens?: number; output_tokens?: number; model?: string } }).usage;
    if (usage && budgetTracker) {
      const inputTok = usage.input_tokens ?? 0;
      const outputTok = usage.output_tokens ?? 0;
      const model = usage.model ?? "claude-sonnet-4-6-20251022";
      const turnUsd = estimateTurnCost(model, inputTok, outputTok);
      budgetTracker.recordSpend({ turnUsd, model });
    }

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
      const digestText = buildMinimalDigest();
      await ctx.newSession?.({
        parentSession: ctx.sessionManager?.getSessionFile() ?? undefined,
        withSession: async (rep) => {
          rep.sendUserMessage?.(digestText, { deliverAs: "followUp" });
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

// Approximate USD cost per turn using Anthropic list prices (2026-05-30).
// Input: $/MTok, Output: $/MTok. Uses uncached rates for conservative upper bound.
function estimateTurnCost(model: string, inputTokens: number, outputTokens: number): number {
  let inputRate: number;
  let outputRate: number;
  if (model.startsWith("claude-opus")) {
    inputRate = 5.0 / 1_000_000;
    outputRate = 25.0 / 1_000_000;
  } else if (model.startsWith("claude-haiku")) {
    inputRate = 1.0 / 1_000_000;
    outputRate = 5.0 / 1_000_000;
  } else {
    // Sonnet and unknown models
    inputRate = 3.0 / 1_000_000;
    outputRate = 15.0 / 1_000_000;
  }
  return inputTokens * inputRate + outputTokens * outputRate;
}
