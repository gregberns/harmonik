// flywheel — harmonik self-managing orchestrator extension.
//
// What this version does:
//   - registers a `note` tool (durable notes → .harmonik/cognition/notes.jsonl)
//   - registers a `reset_context` tool (deferred reset at turn boundary)
//   - registers a `read_skill` tool (lazy-fetch fat-skills from .flywheel/skills/)
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
//     NOTE: tui-panel.ts already calls harmonik digest --json for widget rendering (CL-082)
//   - manage the loop-singleton lock
//
// Design source of truth:
//   /Users/gb/.kerf/projects/gregberns-harmonik/flywheel/04-design/self-managing-architecture.md
// Spec: specs/cognition-loop.md

import { Type } from "typebox";
import type { ExtensionAPI, AgentToolResult } from "@earendil-works/pi-coding-agent";
import { appendFileSync, mkdirSync, readFileSync } from "node:fs";
import { execFile } from "node:child_process";
import { promisify } from "node:util";
import { createHash } from "node:crypto";
import { join } from "node:path";
import { prepareNextTurn, type Digest, type WakeEvent } from "./router.js";
import { BudgetTracker } from "./budget.js";
import { CircuitBreaker } from "./circuit-breaker.js";
import { createEventBridge, type Harness } from "./bridge.js";
import { createDigestPanel, type DigestPanel, type DigestJSON } from "./tui-panel.js";
import { createDispatcher } from "./dispatcher.js";

const execFileAsync = promisify(execFile);

const REPO_ROOT = process.cwd();
const NOTE_FILE = join(REPO_ROOT, ".harmonik/cognition/notes.jsonl");
const COGNITION_DIR = join(REPO_ROOT, ".harmonik/cognition");
const EVENTS_FILE = join(REPO_ROOT, ".harmonik/events/events.jsonl");
const SKILLS_DIR = join(REPO_ROOT, ".flywheel/skills");

const SKILL_NAMES = [
  "triage-failure",
  "investigate-run",
  "compose-batch",
  "escalate",
  "reconcile-state",
] as const;
type SkillName = typeof SKILL_NAMES[number];

let pendingReset: { reason: string; instructions?: string } | null = null;
let lastWarnedThreshold = 0; // so we nudge once per crossing, not every turn

// Shared digest state: exception_flag set by harness to trigger a one-shot Opus turn.
const digest: Digest = { exception_flag: false };

// Initialized in activate() so env vars / config are available.
let budgetTracker: BudgetTracker;
let circuitBreaker: CircuitBreaker;
let digestPanel: DigestPanel;

export default function activate(pi: ExtensionAPI) {
  const _budgetEnv = process.env["FLYWHEEL_BUDGET_USD_PER_DAY"];
  // Default 20 USD/day to keep the downgrade+halt ladder active out of the box.
  // Set FLYWHEEL_BUDGET_USD_PER_DAY=unlimited to opt out of the cap explicitly.
  const budgetUsdPerDay =
    _budgetEnv === "unlimited" ? Infinity
    : _budgetEnv ? parseFloat(_budgetEnv)
    : 20;
  const circuitThreshold = process.env["FLYWHEEL_CIRCUIT_THRESHOLD_PER_MIN"]
    ? parseFloat(process.env["FLYWHEEL_CIRCUIT_THRESHOLD_PER_MIN"])
    : 10;

  budgetTracker = new BudgetTracker({ limitUsd: budgetUsdPerDay, eventsFile: EVENTS_FILE });
  circuitBreaker = new CircuitBreaker({ thresholdPerMin: circuitThreshold, eventsFile: EVENTS_FILE });
  digestPanel = createDigestPanel(REPO_ROOT);

  // ── prepareNextTurn: model stratification + budget + circuit-breaker ──
  // prepareNextTurn is a Pi lifecycle hook not yet typed in the published ExtensionAPI overloads.
  (pi as { on(event: string, handler: (event: unknown, ctx: unknown) => Promise<unknown>): void }).on(
  "prepareNextTurn", async (event, _ctx) => {
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
    followUp: (msg) => {
      pi.sendUserMessage?.(msg, { deliverAs: "followUp" });
      // Refresh the TUI panel so the new event lands within 1s.
      digestPanel.refresh();
    },
  };
  const dispatcher = createDispatcher({ repoRoot: REPO_ROOT });
  const bridge = createEventBridge(harness, { repoRoot: REPO_ROOT, dispatcher });
  bridge.start();

  // ── TUI digest panel (CL-082) ─────────────────────────────────────────
  // Start on first session_start; ctx.ui may be absent in non-interactive
  // (RPC/print) modes — guard with hasUI.
  pi.on("session_start", async (_event, ctx) => {
    if (ctx.hasUI) {
      digestPanel.start(ctx.ui);
    }
  });

  // Resource-leak fix: tear the bridge and panel down on session shutdown.
  pi.on("session_shutdown", async () => {
    bridge.stop();
    digestPanel.stop();
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
      pi.appendEntry("harmonik.note", row); // mirror into live session journal
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

  // ── read_skill (lazy fat-skill fetch) ───────────────────────────────
  // Reads a skill markdown file from .flywheel/skills/<name>.md and returns
  // its content + a SHA-256 content hash so the caller can verify identity
  // across fetches. Skills live-reload: each read_skill() call re-reads disk,
  // so an operator edit takes effect on the next invocation (no process restart).
  // The sha pins the skill body in the conversation — same sha across turns in
  // the same cycle means the cached tool_result can be reused.
  pi.registerTool({
    name: "read_skill",
    label: "Read Skill",
    description:
      "Fetch a fat-skill procedure from .flywheel/skills/<name>.md. " +
      "Call this before making a judgment-class decision of the named class. " +
      "Returns {content, sha} where sha is a SHA-256 of the file content (first 12 hex chars).",
    promptSnippet:
      "read_skill(name) — fetch procedure before deciding; do not improvise the named class",
    promptGuidelines: [
      "Call read_skill BEFORE making any judgment-class decision covered by the skill index.",
      "Skill names: triage-failure, investigate-run, compose-batch, escalate, reconcile-state.",
      "The returned sha identifies the skill version; include it in any decision note that cites the skill.",
    ],
    parameters: Type.Object({
      name: Type.Union(
        SKILL_NAMES.map((n) => Type.Literal(n)) as [
          ReturnType<typeof Type.Literal>,
          ...ReturnType<typeof Type.Literal>[],
        ]
      ),
    }),
    async execute(_id, params): Promise<AgentToolResult<{ name: SkillName; error?: string; sha?: string; path?: string }>> {
      const skillName = params.name as SkillName;
      const skillPath = join(SKILLS_DIR, `${skillName}.md`);
      let content: string;
      try {
        content = readFileSync(skillPath, "utf8");
      } catch (err) {
        const msg = err instanceof Error ? err.message : String(err);
        return {
          content: [{ type: "text" as const, text: `error: skill not found at ${skillPath}: ${msg}` }],
          details: { name: skillName, error: msg, sha: undefined, path: undefined },
        };
      }
      const sha = createHash("sha256").update(content).digest("hex").slice(0, 12);
      return {
        content: [{ type: "text" as const, text: content }],
        details: { name: skillName, sha, path: skillPath, error: undefined },
      };
    },
  });

  // ── context: per-model-call fullness injection (read-only) ──────────
  pi.on("context", async (event, ctx) => {
    const u = ctx.getContextUsage?.();
    if (!u || u.percent == null) return;
    const pct = Math.round(u.percent);
    digestPanel.setContextFullness(pct);
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
      const digestText = await buildDigest();
      await ctx.newSession?.({
        parentSession: ctx.sessionManager?.getSessionFile() ?? undefined,
        withSession: async (rep) => {
          rep.sendUserMessage?.(digestText, { deliverAs: "followUp" });
        },
      });
    },
  });
}

async function buildDigest(): Promise<string> {
  try {
    const { stdout } = await execFileAsync(
      "harmonik",
      ["digest", "--json", "--project", REPO_ROOT],
      { timeout: 10_000 }
    );
    const d = JSON.parse(stdout.trim()) as DigestJSON;
    return renderDigestAsText(d);
  } catch {
    return [
      "## Resumed from prior session",
      "(`harmonik digest` unavailable — read open notes at `.harmonik/cognition/notes.jsonl`.)",
      "",
      "Continue from: review open notes, then resume the active priority work.",
    ].join("\n");
  }
}

function renderDigestAsText(d: DigestJSON): string {
  const lines: string[] = [];
  lines.push("## Harmonik digest (session resume)");
  lines.push("");

  // Queue status
  const q = d.queue;
  if (!q.present) {
    lines.push("**Queue:** no active queue");
  } else {
    lines.push(
      `**Queue:** ${q.active_run_count} active run(s), ${q.pending_count} pending`
    );
    for (const r of q.active_runs ?? []) {
      const runId = r.run_id ? r.run_id.slice(0, 8) : "—";
      lines.push(`  - ${r.bead_id}  run=${runId}  ${r.status}`);
    }
    const omitted = d.truncated?.active_runs_omitted ?? 0;
    if (omitted > 0) lines.push(`  - [+${omitted} more omitted]`);
  }

  // In-progress beads
  const inProg = d.in_progress_beads ?? [];
  if (inProg.length > 0) {
    lines.push("");
    lines.push(`**In-progress beads (${inProg.length}):**`);
    for (const b of inProg) {
      lines.push(`  - ${b.bead_id}  ${b.title}`);
    }
  }

  // Ready beads
  const ready = d.ready_beads ?? [];
  if (ready.length > 0) {
    lines.push("");
    lines.push(`**Ready beads (${ready.length}):**`);
    for (const b of ready.slice(0, 10)) {
      lines.push(`  - ${b.bead_id}  ${b.title}`);
    }
    if (ready.length > 10) lines.push(`  - [+${ready.length - 10} more]`);
  }

  // Open notes
  const notes = d.open_notes ?? [];
  if (notes.length > 0) {
    lines.push("");
    lines.push(`**Open notes (${notes.length}):**`);
    for (const n of notes.slice(0, 10)) {
      lines.push(`  - [${n.kind}] ${n.text}`);
    }
    const notesOmitted = d.truncated?.open_notes_omitted ?? 0;
    if (notesOmitted > 0) lines.push(`  - [+${notesOmitted} more]`);
  }

  // Recent commits
  const commits = d.recent_commits ?? [];
  if (commits.length > 0) {
    lines.push("");
    lines.push("**Recent commits:**");
    for (const c of commits.slice(0, 5)) {
      lines.push(`  - ${c.hash.slice(0, 8)}  ${c.subject}`);
    }
  }

  // Warnings
  if (d.errors && d.errors.length > 0) {
    lines.push("");
    for (const e of d.errors) lines.push(`> WARN: ${e}`);
  }

  lines.push("");
  lines.push(
    "Continue from: review in-progress/ready beads above, then resume the active priority work."
  );

  return lines.join("\n");
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
