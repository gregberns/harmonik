// dispatcher.ts — CL-071/072/073 eager pure-code refill.
//
// On any slot-releasing event (run_completed/run_failed/run_canceled):
//   1. kerf next --format=json --only=bead  (CL-071 §1)
//   2. Pre-screen guards CL-072             (already-in-queue, already-landed, failed-twice)
//   3. Dispatch via harmonik queue append (refill) or submit (first-fill)
//   4. If kerf next empty → wake model (CL-073)
//
// Mechanism-tagged (CL-INV-001 / CL-013): no LLM calls.
// All judgment paths (empty queue, failed-twice halt) delegate back via onWakeModel.
//
// Spec: specs/cognition-loop.md §4.9 CL-070..073.

import { spawn } from "node:child_process";
import {
  readFileSync,
  appendFileSync,
  writeFileSync,
  mkdirSync,
  unlinkSync,
} from "node:fs";
import { join } from "node:path";
import { randomUUID } from "node:crypto";
import { tmpdir } from "node:os";

// ── Types ─────────────────────────────────────────────────────────────────────

export type SlotReleaseTrigger = "run_completed" | "run_failed" | "run_canceled";

export interface ActiveQueueInfo {
  queueId: string;
  /** All bead_ids present in the queue, regardless of item status. */
  beadsInQueue: Set<string>;
  /** True when group 0 is kind=stream (append target). */
  hasStreamGroup: boolean;
}

/**
 * Injectable dependencies. All have production defaults in createDispatcher().
 * Override in tests to avoid spawning real processes.
 */
export interface DispatcherDeps {
  /**
   * Runs kerf next and returns {beads, raw}.
   * beads: ordered list of bead IDs from `kerf next --format=json --only=bead`.
   * raw: raw JSON output string; passed to onWakeModel so the caller can build a
   * CL-073 turn with context (spec §4.9 CL-073: turn MUST include kerf next output).
   */
  kerfNextBeads: (repoRoot: string) => Promise<{ beads: string[]; raw: string }>;
  readActiveQueue: (repoRoot: string) => ActiveQueueInfo | null;
  gitCheck: (repoRoot: string, beadId: string) => Promise<boolean>;
  /** First-fill: submit a new stream queue. Returns {queueId, ok}. */
  queueSubmit: (repoRoot: string, beadId: string) => Promise<{ queueId: string | null; ok: boolean }>;
  /** Refill: append bead to stream group 0 of an existing queue. */
  queueAppend: (repoRoot: string, queueId: string, beadId: string) => Promise<boolean>;
}

export interface DispatcherOptions {
  repoRoot: string;
  /** Partial dep overrides; unset fields use production defaults. */
  deps?: Partial<DispatcherDeps>;
  harmonikBin?: string;
  kerfBin?: string;
}

/** Context passed to onWakeModel so the caller can build a CL-073 turn with kerf output. */
export interface WakeModelContext {
  /** Raw kerf next output (JSON string), if kerf ran successfully. */
  kerfNextRaw?: string;
}

export interface SlotReleasedOpts {
  triggerType: SlotReleaseTrigger;
  /** UUIDv7 event_id from the triggering event (idempotency key). */
  triggeringEventId: string;
  /** bead_id from event payload, when present. */
  beadId?: string;
  /**
   * Called when harness decides the model should be woken (CL-073).
   * context.kerfNextRaw carries the raw kerf next output for inclusion in
   * the turn so the model has context to decide what to do next.
   */
  onWakeModel: (reason: string, context: WakeModelContext) => void;
  /** Pipe cognition events back to the caller (bridge emits them to cognition-events.jsonl). */
  onCognitionEvent: (record: Record<string, unknown>) => void;
}

export interface Dispatcher {
  /**
   * CL-071: Eager pure-code refill on slot release.
   * Fire-and-forget from bridge.ts; errors logged via onCognitionEvent.
   */
  onSlotReleased(opts: SlotReleasedOpts): Promise<void>;

  /** CL-072 guard #3: record a bead failure so the session fail count is tracked. */
  recordBeadFailure(beadId: string): void;
}

// ── Helpers ───────────────────────────────────────────────────────────────────

function spawnCapture(
  cmd: string,
  args: string[],
  spawnOpts?: { cwd?: string },
): Promise<{ stdout: string; code: number }> {
  return new Promise((resolve) => {
    const proc = spawn(cmd, args, { stdio: ["ignore", "pipe", "pipe"], ...spawnOpts });
    let stdout = "";
    proc.stdout?.on("data", (chunk: Buffer) => { stdout += chunk.toString(); });
    proc.on("exit", (code) => resolve({ stdout, code: code ?? 1 }));
    proc.on("error", () => resolve({ stdout: "", code: 1 }));
  });
}

// ── Production dep factory functions ─────────────────────────────────────────

function makeKerfNextBeads(kerfBin: string): DispatcherDeps["kerfNextBeads"] {
  return async (repoRoot: string): Promise<{ beads: string[]; raw: string }> => {
    const { stdout, code } = await spawnCapture(
      kerfBin,
      ["next", "--format=json", "--only=bead"],
      { cwd: repoRoot },
    );
    if (code !== 0 || !stdout.trim()) return { beads: [], raw: stdout };
    try {
      type KerfItem = { kind?: string; bead_id?: string; id?: string };
      const parsed = JSON.parse(stdout) as { items?: KerfItem[] } | KerfItem[];
      const items: KerfItem[] = Array.isArray(parsed) ? parsed : ((parsed as { items?: KerfItem[] }).items ?? []);
      const beads = items
        .filter((it) => !it.kind || it.kind === "bead")
        .map((it) => it.bead_id ?? it.id ?? "")
        .filter(Boolean);
      return { beads, raw: stdout };
    } catch {
      return { beads: [], raw: stdout };
    }
  };
}

function makeReadActiveQueue(queueName: string = "main"): DispatcherDeps["readActiveQueue"] {
  return (repoRoot: string): ActiveQueueInfo | null => {
    const queuePath = join(repoRoot, ".harmonik", "queues", `${queueName}.json`);
    let raw: string;
    try {
      raw = readFileSync(queuePath, "utf8");
    } catch {
      return null;
    }
    try {
      const q = JSON.parse(raw) as {
        queue_id?: string;
        status?: string;
        groups?: Array<{ kind?: string; items?: Array<{ bead_id?: string }> }>;
      };
      if (!q.queue_id) return null;
      if (q.status === "completed" || q.status === "cancelled") return null;
      const beadsInQueue = new Set<string>();
      let hasStreamGroup = false;
      for (const g of q.groups ?? []) {
        if (g.kind === "stream") hasStreamGroup = true;
        for (const item of g.items ?? []) {
          if (item.bead_id) beadsInQueue.add(item.bead_id);
        }
      }
      return { queueId: q.queue_id, beadsInQueue, hasStreamGroup };
    } catch {
      return null;
    }
  };
}

function defaultGitCheck(repoRoot: string, beadId: string): Promise<boolean> {
  return new Promise((resolve) => {
    const proc = spawn(
      "git",
      ["-C", repoRoot, "log", "origin/main", `--grep=Refs: ${beadId}`, "--max-count=1", "--oneline"],
      { stdio: ["ignore", "pipe", "ignore"] },
    );
    let out = "";
    proc.stdout?.on("data", (chunk: Buffer) => { out += chunk.toString(); });
    proc.on("exit", () => resolve(out.trim().length > 0));
    proc.on("error", () => resolve(false));
  });
}

export function makeQueueSubmit(harmonikBin: string, queueName?: string): DispatcherDeps["queueSubmit"] {
  return async (
    repoRoot: string,
    beadId: string,
  ): Promise<{ queueId: string | null; ok: boolean }> => {
    const queueDoc = JSON.stringify({
      schema_version: 1,
      groups: [{ kind: "stream", items: [{ bead_id: beadId }] }],
    });
    const tmpFile = join(tmpdir(), `dispatch-${randomUUID()}.json`);
    try {
      writeFileSync(tmpFile, queueDoc, "utf8");
      const args = ["queue", "submit", "--json"];
      if (queueName) args.push("--queue", queueName);
      args.push(tmpFile);
      const { stdout, code } = await spawnCapture(harmonikBin, args, { cwd: repoRoot });
      if (code !== 0) return { queueId: null, ok: false };
      try {
        const parsed = JSON.parse(stdout.trim()) as { queue_id?: string };
        const queueId = parsed.queue_id ?? null;
        return { queueId, ok: queueId != null };
      } catch {
        return { queueId: null, ok: false };
      }
    } catch {
      return { queueId: null, ok: false };
    } finally {
      try { unlinkSync(tmpFile); } catch { /* ignore */ }
    }
  };
}

function makeQueueAppend(harmonikBin: string, queueName?: string): DispatcherDeps["queueAppend"] {
  return async (
    repoRoot: string,
    queueId: string,
    beadId: string,
  ): Promise<boolean> => {
    const args = ["queue", "append"];
    if (queueName) {
      args.push("--queue", queueName);
    } else {
      args.push("--queue-id", queueId);
    }
    args.push("0", beadId);
    const { code } = await spawnCapture(harmonikBin, args, { cwd: repoRoot });
    return code === 0;
  };
}

// ── Goal-keeper fire-and-forget ───────────────────────────────────────────────

/**
 * fireGoalKeeper spawns `harmonik goal-keeper --project <repoRoot>` as a
 * detached fire-and-forget process (flywheel V6 idle-triggered realign,
 * hk-owz1). Errors are non-fatal — if the goal-keeper fails the captain is
 * woken anyway with whatever goal-state exists (possibly stale).
 *
 * Called on every empty-queue wake so the captain always sees the latest
 * operator directives before deciding what to do next.
 */
function fireGoalKeeper(
  harmonikBin: string,
  repoRoot: string,
  onCognitionEvent: (record: Record<string, unknown>) => void,
): void {
  const proc = spawn(harmonikBin, ["goal-keeper", "--project", repoRoot], {
    stdio: "ignore",
    detached: true,
  });
  proc.unref();
  onCognitionEvent({ type: "goal_keeper_fired", repoRoot });
}

// ── createDispatcher ──────────────────────────────────────────────────────────

export function createDispatcher(opts: DispatcherOptions): Dispatcher {
  const {
    repoRoot,
    harmonikBin = "harmonik",
    kerfBin = "kerf",
    deps = {},
  } = opts;

  const kerfNextBeads = deps.kerfNextBeads ?? makeKerfNextBeads(kerfBin);
  const readActiveQueue = deps.readActiveQueue ?? makeReadActiveQueue();
  const gitCheck = deps.gitCheck ?? defaultGitCheck;
  const queueSubmit = deps.queueSubmit ?? makeQueueSubmit(harmonikBin);
  const queueAppend = deps.queueAppend ?? makeQueueAppend(harmonikBin);

  // CL-072 guard #3: per-session fail counts indexed by bead_id.
  const failCounts = new Map<string, number>();

  function appendDispatchLog(entry: Record<string, unknown>): void {
    const logPath = join(repoRoot, ".harmonik", "cognition", "dispatch-log.jsonl");
    mkdirSync(join(repoRoot, ".harmonik", "cognition"), { recursive: true });
    appendFileSync(logPath, JSON.stringify({ ts: Date.now(), ...entry }) + "\n");
  }

  // CL-055/056: idempotency check — skip if this event_id was already dispatched.
  function alreadyProcessed(triggeringEventId: string): boolean {
    const logPath = join(repoRoot, ".harmonik", "cognition", "dispatch-log.jsonl");
    try {
      const content = readFileSync(logPath, "utf8");
      return content.includes(`"triggering_event_id":"${triggeringEventId}"`);
    } catch {
      return false;
    }
  }

  return {
    recordBeadFailure(beadId: string): void {
      failCounts.set(beadId, (failCounts.get(beadId) ?? 0) + 1);
    },

    async onSlotReleased({
      triggeringEventId,
      onWakeModel,
      onCognitionEvent,
    }: SlotReleasedOpts): Promise<void> {
      // CL-055: idempotency guard.
      if (alreadyProcessed(triggeringEventId)) {
        onCognitionEvent({ type: "eager_refill_idempotent_skip", triggering_event_id: triggeringEventId });
        return;
      }

      // 1. kerf next --format=json --only=bead (CL-071 §1)
      let candidates: string[];
      let kerfNextRaw = "";
      try {
        const result = await kerfNextBeads(repoRoot);
        candidates = result.beads;
        kerfNextRaw = result.raw;
      } catch (err) {
        onCognitionEvent({ type: "eager_refill_kerf_error", error: String(err), triggering_event_id: triggeringEventId });
        onWakeModel("kerf_error", {});
        return;
      }

      if (candidates.length === 0) {
        // CL-073: empty queue → wake model with kerf output as context.
        appendDispatchLog({ triggering_event_id: triggeringEventId, result: "empty_queue" });
        onCognitionEvent({ type: "eager_refill_kerf_empty", triggering_event_id: triggeringEventId });
        // Idle-triggered goal-keeper realign (flywheel V6, hk-owz1): when the
        // queue is empty and the captain is about to be woken to assess the
        // situation, refresh the goal-state first so the captain sees the most
        // recent operator directives. Fire-and-forget; failures are non-fatal.
        fireGoalKeeper(harmonikBin, repoRoot, onCognitionEvent);
        onWakeModel("empty_queue", { kerfNextRaw });
        return;
      }

      // 2. Read active queue for guard #1 (already-in-queue check).
      const activeQueue = readActiveQueue(repoRoot);

      // 3. Apply CL-072 pre-screen guards in rank order.
      let dispatchBeadId: string | null = null;
      for (const cand of candidates) {
        // Guard 1: already in queue.json
        if (activeQueue?.beadsInQueue.has(cand)) {
          appendDispatchLog({
            triggering_event_id: triggeringEventId,
            candidate_bead: cand,
            skipped_reason: "already_in_queue",
            picked_instead: null,
          });
          onCognitionEvent({ type: "eager_refill_skipped", candidate_bead: cand, skipped_reason: "already_in_queue", triggering_event_id: triggeringEventId });
          continue;
        }

        // Guard 2: already landed on origin/main
        let landed = false;
        try {
          landed = await gitCheck(repoRoot, cand);
        } catch { /* treat as not landed */ }
        if (landed) {
          appendDispatchLog({
            triggering_event_id: triggeringEventId,
            candidate_bead: cand,
            skipped_reason: "already_landed",
            picked_instead: null,
          });
          onCognitionEvent({ type: "eager_refill_skipped", candidate_bead: cand, skipped_reason: "already_landed", triggering_event_id: triggeringEventId });
          // CL-072 guard #2: emit deferred close-stale-bead intent so the model
          // can close the orphaned open bead at the next judgment turn.
          onCognitionEvent({ type: "deferred_close_stale_bead_intent", bead_id: cand, triggering_event_id: triggeringEventId });
          continue;
        }

        // Guard 3: failed twice this session → HALT refill, wake model (CL-072 §3).
        if ((failCounts.get(cand) ?? 0) >= 2) {
          appendDispatchLog({
            triggering_event_id: triggeringEventId,
            candidate_bead: cand,
            skipped_reason: "failed_twice",
            picked_instead: null,
          });
          onCognitionEvent({ type: "eager_refill_halt_failed_twice", bead_id: cand, triggering_event_id: triggeringEventId });
          onWakeModel("failed_twice_halt", { kerfNextRaw });
          return;
        }

        // Guard 4: conflict with in-flight (best-effort v0.1 — always pass)

        dispatchBeadId = cand;
        break;
      }

      if (dispatchBeadId === null) {
        // All candidates screened out.
        appendDispatchLog({ triggering_event_id: triggeringEventId, result: "all_candidates_screened" });
        onCognitionEvent({ type: "eager_refill_all_screened", triggering_event_id: triggeringEventId });
        onWakeModel("empty_queue", { kerfNextRaw });
        return;
      }

      // 4. Dispatch (CL-071 §3).
      const dispatchKey = `dispatch_intent:${triggeringEventId}:${dispatchBeadId}`;

      if (activeQueue?.hasStreamGroup) {
        // Refill: append to existing stream group (group 0).
        let ok = false;
        try {
          ok = await queueAppend(repoRoot, activeQueue.queueId, dispatchBeadId);
        } catch { /* ok stays false */ }

        if (ok) {
          appendDispatchLog({
            triggering_event_id: triggeringEventId,
            dispatched_bead: dispatchBeadId,
            dispatch_key: dispatchKey,
            method: "queue_append",
            queue_id: activeQueue.queueId,
          });
          onCognitionEvent({ type: "eager_refill_dispatched", bead_id: dispatchBeadId, method: "append", queue_id: activeQueue.queueId, triggering_event_id: triggeringEventId });
        } else {
          appendDispatchLog({
            triggering_event_id: triggeringEventId,
            candidate_bead: dispatchBeadId,
            skipped_reason: "queue_append_failed",
            picked_instead: null,
          });
          onCognitionEvent({ type: "eager_refill_dispatch_failed", method: "append", bead_id: dispatchBeadId, triggering_event_id: triggeringEventId });
        }
      } else {
        // First fill: submit a new stream queue (submit-as-start, CL-071 §Dispatch surface).
        let submitResult: { queueId: string | null; ok: boolean } = { queueId: null, ok: false };
        try {
          submitResult = await queueSubmit(repoRoot, dispatchBeadId);
        } catch { /* submitResult stays {ok:false} */ }

        if (submitResult.ok && submitResult.queueId) {
          appendDispatchLog({
            triggering_event_id: triggeringEventId,
            dispatched_bead: dispatchBeadId,
            dispatch_key: dispatchKey,
            method: "queue_submit",
            queue_id: submitResult.queueId,
          });
          onCognitionEvent({ type: "eager_refill_dispatched", bead_id: dispatchBeadId, method: "submit", queue_id: submitResult.queueId, triggering_event_id: triggeringEventId });
        } else {
          appendDispatchLog({
            triggering_event_id: triggeringEventId,
            candidate_bead: dispatchBeadId,
            skipped_reason: "queue_submit_failed",
            picked_instead: null,
          });
          onCognitionEvent({ type: "eager_refill_dispatch_failed", method: "submit", bead_id: dispatchBeadId, triggering_event_id: triggeringEventId });
        }
      }
    },
  };
}
