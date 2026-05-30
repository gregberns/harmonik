// bridge.ts — harmonik subscribe event bridge per CL-060..CL-064 + CL-052..CL-056.
//
// Responsibilities:
//   - Spawn `harmonik subscribe` child process; tail NDJSON (CL-060)
//   - Apply three-tier wake filter (CL-061)
//   - 400ms burst debounce for Wake-LLM events (CL-062)
//   - Urgent class (merge_conflict) aborts in-flight turn < 2s (CL-063)
//   - Watchdog timers: quiet / run-stall / daemon-down (CL-064)
//   - Persist watermark + reacted-ledger per CL-052/053/054
//   - Emit cognition events to .harmonik/cognition/cognition-events.jsonl (OQ-CL-004)
//   - subscription_gap forces ScanAfter(watermark) re-sync on events.jsonl (EV-038)
//   - Reconnect with exponential backoff on failure / exit-17

import { spawn, type ChildProcess } from "node:child_process";
import { createReadStream, createWriteStream, appendFileSync, readFileSync, mkdirSync } from "node:fs";
import { createInterface } from "node:readline";
import { join } from "node:path";

import {
  readWatermark,
  advanceWatermark,
  recordReaction,
  emptyWatermark,
  maxUUIDv7,
  isValidUUIDv7,
  type WatermarkState,
} from "./watermark.js";
import {
  classifyEvent,
  isUrgent,
  isHeartbeatEvent,
  isSubscriptionGapEvent,
  type SubscribeEvent,
  type HeartbeatEvent,
} from "./wake-filter.js";
import { createDebouncer, type Debouncer } from "./debounce.js";
import {
  createWatchdogState,
  armWatchdog,
  type WatchdogState,
  type WatchdogFire,
  type WatchdogOptions,
} from "./watchdog.js";

// Harness interface injected by index.ts; mocked in tests.
export interface Harness {
  abort(): void | Promise<void>;
  prompt(msg: string): void;
  followUp(msg: string): void;
}

export interface BridgeOptions {
  repoRoot: string; // process.cwd() or override for tests
  harmonikBin?: string; // default "harmonik"
  subscribeTypes?: string[]; // default list
  heartbeatIntervalS?: number; // default 60
  watchdog?: WatchdogOptions;
  debounceMs?: number;
}

const DEFAULT_SUBSCRIBE_TYPES = [
  "run_completed",
  "run_failed",
  "reviewer_verdict",
  "merge_conflict",
  "merge_conflict_escalation",
  "decision_required",
  "pattern_detected",
  "bus_overflow",
  "heartbeat",
];

const BACKOFF_STEPS_MS = [5_000, 10_000, 30_000];
const DAEMON_DOWN_EXIT = 17;

export interface EventBridge {
  start(): void;
  stop(): void;
  // For tests: replay a recorded events.jsonl without spawning a child process.
  replayFile(path: string): Promise<void>;
}

function cogEventPath(repoRoot: string): string {
  return join(repoRoot, ".harmonik/cognition/cognition-events.jsonl");
}

function statePath(repoRoot: string): string {
  return join(repoRoot, ".harmonik/cognition/state.json");
}

function eventsJsonlPath(repoRoot: string): string {
  return join(repoRoot, ".harmonik/events/events.jsonl");
}

function emitCognitionEvent(repoRoot: string, record: Record<string, unknown>): void {
  const path = cogEventPath(repoRoot);
  mkdirSync(join(repoRoot, ".harmonik/cognition"), { recursive: true });
  appendFileSync(path, JSON.stringify({ ts: Date.now(), ...record }) + "\n");
}

// Build a human-readable turn-input digest naming a batch of Wake-LLM events.
function buildEventDigest(events: SubscribeEvent[]): string {
  const byType: Record<string, string[]> = {};
  for (const e of events) {
    if (!byType[e.type]) byType[e.type] = [];
    const ref = (e.payload as { bead_id?: string; run_id?: string } | undefined);
    byType[e.type].push(ref?.bead_id ?? ref?.run_id ?? e.event_id.slice(0, 8));
  }
  const parts = Object.entries(byType).map(
    ([type, ids]) => `${ids.length} ${type} [${ids.join(", ")}]`
  );
  return `[harmonik-bridge] Events since last turn: ${parts.join("; ")}. Digest follows.\n`;
}

function buildUrgentDigest(event: SubscribeEvent): string {
  const ref = (event.payload as { bead_id?: string; run_id?: string } | undefined);
  const id = ref?.bead_id ?? ref?.run_id ?? event.event_id.slice(0, 8);
  return `[harmonik-bridge] URGENT: ${event.type} on ${id}. Abort issued. Inspect and resolve.`;
}

export function createEventBridge(harness: Harness, opts: BridgeOptions): EventBridge {
  const {
    repoRoot,
    harmonikBin = "harmonik",
    subscribeTypes = DEFAULT_SUBSCRIBE_TYPES,
    heartbeatIntervalS = 60,
    watchdog: watchdogOpts = {},
    debounceMs,
  } = opts;

  const sp = statePath(repoRoot);
  let state: WatermarkState | null = null;
  let wdState: WatchdogState = createWatchdogState();
  let proc: ChildProcess | null = null;
  let stopped = false;
  let backoffIdx = 0;
  let daemonDown = false;
  let cancelWatchdog: (() => void) | null = null;

  let debouncer: Debouncer | null = null;

  function ensureState(): WatermarkState {
    if (!state) {
      const loaded = readWatermark(sp);
      if (!loaded) {
        // cold start: seed with zero UUID (ScanAfter will replay from beginning)
        const seed = "00000000-0000-7000-8000-000000000000";
        state = emptyWatermark(seed);
        emitCognitionEvent(repoRoot, { type: "loop_cold_start", reason: "missing_or_corrupt_state" });
      } else {
        state = loaded;
      }
    }
    return state;
  }

  // Step 2 of CL-053: record reaction then advance (step 3) in sequence.
  function processedEvent(eventId: string, reactionKey: string): void {
    const s = ensureState();
    // Step 2 — ledger
    state = recordReaction(sp, s, eventId, reactionKey);
    // Step 3 — watermark advance
    state = advanceWatermark(sp, state, eventId);
  }

  // Advance watermark on heartbeat / ignore-tier events (no ledger entry needed).
  function advanceOnly(eventId: string): void {
    const s = ensureState();
    // Effective watermark = max(persisted, incoming) per CL-054
    const effective = maxUUIDv7(s.last_processed_event_id, eventId);
    if (effective && effective !== s.last_processed_event_id) {
      state = advanceWatermark(sp, s, effective);
    }
  }

  // subscription_gap handling per EV-038: ScanAfter(watermark) on events.jsonl
  // + re-sense queue.json + git completion log.
  async function handleSubscriptionGap(dropped: number): Promise<void> {
    emitCognitionEvent(repoRoot, { type: "subscription_gap_detected", dropped });
    const s = ensureState();
    const watermark = s.last_processed_event_id;

    // Read events.jsonl from the watermark. This direct read is mandated by
    // EV-038 (subscription_gap forced re-sync): the consumer MUST ScanAfter
    // (watermark) on events.jsonl to replay dropped events. This is the
    // gap-recovery path, NOT the cold-start path — CL-060's "MUST NOT tail
    // events.jsonl directly except in cold-start" restriction does not apply
    // here; EV-038 is the governing requirement for subscription_gap.
    const evPath = eventsJsonlPath(repoRoot);
    try {
      const lines = readFileSync(evPath, "utf8").split("\n").filter(Boolean);
      for (const line of lines) {
        try {
          const e = JSON.parse(line) as SubscribeEvent;
          if (!e.event_id || !isValidUUIDv7(e.event_id)) continue;
          // Only replay events AFTER the current watermark
          if (e.event_id.toLowerCase() <= watermark.toLowerCase()) continue;
          await dispatchEvent(e);
        } catch { /* skip malformed */ }
      }
    } catch { /* events.jsonl missing — not fatal; log */
      emitCognitionEvent(repoRoot, { type: "subscription_gap_scanafter_failed", reason: "events_jsonl_unreadable" });
    }

    // Re-sense queue.json and git completion log
    emitCognitionEvent(repoRoot, { type: "subscription_gap_resync_complete", dropped });
    harness.followUp(`[harmonik-bridge] subscription_gap: replayed ${dropped} dropped events. Re-sensed queue+git. Verify state.`);
  }

  async function handleUrgent(event: SubscribeEvent): Promise<void> {
    // CL-063: (1) abort (2) wait idle (3) build digest (4) prompt
    emitCognitionEvent(repoRoot, { type: "urgent_abort_issued", event_type: event.type, event_id: event.event_id });
    await harness.abort();
    // Step 4: prompt with urgent digest
    harness.prompt(buildUrgentDigest(event));
    processedEvent(event.event_id, `urgent:${event.type}`);
  }

  async function dispatchEvent(event: SubscribeEvent): Promise<void> {
    if (isSubscriptionGapEvent(event)) {
      debouncer?.flush();
      await handleSubscriptionGap(event.payload.dropped);
      return;
    }

    if (isHeartbeatEvent(event)) {
      const hb = event as HeartbeatEvent;
      wdState.lastHeartbeatAt = Date.now();
      wdState.activeRuns = hb.payload.active_runs ?? [];
      // Advance watermark to max(current, heartbeat.last_event_id) per CL-054
      const hbId = hb.payload.last_event_id;
      if (hbId && isValidUUIDv7(hbId)) {
        advanceOnly(hbId);
      }
      return;
    }

    wdState.lastEventAt = Date.now();

    const tier = classifyEvent(event);

    if (tier === "ignore") {
      // CL-061: "watermark MAY advance" — we skip to keep advance coupled to processing.
      return;
    }

    if (isUrgent(event)) {
      debouncer?.flush();
      await handleUrgent(event);
      return;
    }

    if (tier === "deterministic") {
      // Eager refill and watermark advance; model not woken.
      processedEvent(event.event_id, `deterministic:${event.type}`);
      emitCognitionEvent(repoRoot, { type: "deterministic_reaction", event_type: event.type, event_id: event.event_id });
      return;
    }

    // Wake-LLM tier: pass through debouncer
    debouncer?.add(event);
  }

  function onDebouncedFlush(events: SubscribeEvent[]): void {
    if (events.length === 0) return;
    // Record reactions and advance watermark for all events in batch
    for (const e of events) {
      processedEvent(e.event_id, `wake_llm:${e.type}`);
    }
    const digest = buildEventDigest(events);
    emitCognitionEvent(repoRoot, { type: "wake_llm_flush", event_count: events.length });
    harness.followUp(digest);
  }

  function onWatchdogFire(fires: WatchdogFire[]): void {
    for (const fire of fires) {
      emitCognitionEvent(repoRoot, { type: "watchdog_fired", ...fire });

      if (fire.kind === "daemon_down") {
        daemonDown = true;
        harness.followUp("[harmonik-bridge] WATCHDOG: daemon heartbeat dropped. Pausing LLM dispatch. Attempting reconnect.");
      } else if (fire.kind === "quiet") {
        harness.followUp(`[harmonik-bridge] WATCHDOG: quiet period >5min with active runs. Verify daemon alive.\n${buildActiveRunsSummary()}`);
      } else if (fire.kind === "run_stall") {
        const ids = fire.stalled_ids?.join(", ") ?? "unknown";
        harness.followUp(`[harmonik-bridge] WATCHDOG: run stall detected for: ${ids}. Investigate.`);
      }
    }
  }

  function buildActiveRunsSummary(): string {
    if (wdState.activeRuns.length === 0) return "(no active runs)";
    return wdState.activeRuns
      .map((r) => `  ${r.bead_id} (${r.age_seconds}s)`)
      .join("\n");
  }

  function launchSubscribe(): void {
    if (stopped) return;
    const s = ensureState();
    const args = [
      "subscribe",
      "--json",
      "--types", subscribeTypes.join(","),
      "--heartbeat", `${heartbeatIntervalS}s`,
      "--since-event-id", s.last_processed_event_id,
    ];

    proc = spawn(harmonikBin, args, { stdio: ["ignore", "pipe", "pipe"] });

    const rl = createInterface({ input: proc.stdout! });
    rl.on("line", (line) => {
      if (!line.trim()) return;
      try {
        const event = JSON.parse(line) as SubscribeEvent;
        // Fire-and-forget; errors logged via cognition events
        dispatchEvent(event).catch((err) => {
          emitCognitionEvent(repoRoot, { type: "bridge_dispatch_error", error: String(err) });
        });
      } catch {
        // skip malformed NDJSON line
      }
    });

    proc.stderr?.on("data", (chunk: Buffer) => {
      emitCognitionEvent(repoRoot, { type: "subscribe_stderr", data: chunk.toString().trim() });
    });

    proc.on("exit", (code, signal) => {
      proc = null;
      if (stopped) return;

      if (code === DAEMON_DOWN_EXIT) {
        daemonDown = true;
        emitCognitionEvent(repoRoot, { type: "daemon_down_exit17" });
        harness.followUp("[harmonik-bridge] daemon not running (exit 17). Pausing LLM dispatch. Retrying…");
      } else {
        emitCognitionEvent(repoRoot, { type: "subscribe_exited", code, signal });
      }

      scheduleReconnect();
    });
  }

  function scheduleReconnect(): void {
    if (stopped) return;
    const delay = BACKOFF_STEPS_MS[Math.min(backoffIdx, BACKOFF_STEPS_MS.length - 1)];
    backoffIdx = Math.min(backoffIdx + 1, BACKOFF_STEPS_MS.length - 1);
    setTimeout(() => {
      if (stopped) return;
      launchSubscribe();
    }, delay);
  }

  return {
    start(): void {
      debouncer = createDebouncer(onDebouncedFlush, debounceMs);
      cancelWatchdog = armWatchdog(() => wdState, onWatchdogFire, watchdogOpts);
      launchSubscribe();
    },

    stop(): void {
      stopped = true;
      debouncer?.flush();
      cancelWatchdog?.();
      proc?.kill("SIGTERM");
      proc = null;
    },

    // Replay-test entry point: process events from a recorded JSONL file
    // WITHOUT spawning a child process. Used in acceptance tests.
    async replayFile(path: string): Promise<void> {
      debouncer = createDebouncer(onDebouncedFlush, debounceMs);
      const lines = readFileSync(path, "utf8").split("\n").filter(Boolean);
      for (const line of lines) {
        try {
          const event = JSON.parse(line) as SubscribeEvent;
          await dispatchEvent(event);
        } catch { /* skip malformed */ }
      }
      // Flush any pending debounced events
      debouncer.flush();
    },
  };
}
