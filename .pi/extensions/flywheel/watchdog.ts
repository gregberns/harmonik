// watchdog.ts — watchdog timers per CL-064.
//
// Three conditions checked on every outer-loop wake:
//   quiet       — lastEventAt > 5 min AND active_runs non-empty
//   run_stall   — any active_run.age_seconds > 600
//   daemon_down — now - lastHeartbeatAt > 3 × heartbeat_interval (default 180s)

export type WatchdogKind = "quiet" | "run_stall" | "daemon_down";

export interface WatchdogFire {
  kind: WatchdogKind;
  stalled_ids?: string[]; // bead_ids for run_stall
  last_event_age_ms?: number; // for quiet
  last_heartbeat_age_ms?: number; // for daemon_down
}

export interface WatchdogOptions {
  quietThresholdMs?: number; // default 5 * 60_000
  runStallThresholdS?: number; // default 600
  heartbeatIntervalMs?: number; // default 60_000
  heartbeatMultiplier?: number; // default 3  → 180s
}

export interface ActiveRun {
  bead_id: string;
  age_seconds: number;
}

export interface WatchdogState {
  lastEventAt: number | null; // Date.now() of last non-heartbeat event
  lastHeartbeatAt: number | null; // Date.now() of last heartbeat
  activeRuns: ActiveRun[];
}

const DEFAULTS: Required<WatchdogOptions> = {
  quietThresholdMs: 5 * 60_000,
  runStallThresholdS: 600,
  heartbeatIntervalMs: 60_000,
  heartbeatMultiplier: 3,
};

export function createWatchdogState(): WatchdogState {
  return { lastEventAt: null, lastHeartbeatAt: null, activeRuns: [] };
}

export function checkWatchdog(
  state: WatchdogState,
  opts: WatchdogOptions = {}
): WatchdogFire[] {
  const o = { ...DEFAULTS, ...opts };
  const now = Date.now();
  const fires: WatchdogFire[] = [];

  // daemon_down — checked first; if daemon is down, other checks are moot
  const heartbeatThreshold = o.heartbeatIntervalMs * o.heartbeatMultiplier;
  if (state.lastHeartbeatAt !== null) {
    const age = now - state.lastHeartbeatAt;
    if (age > heartbeatThreshold) {
      fires.push({ kind: "daemon_down", last_heartbeat_age_ms: age });
    }
  }

  // quiet — only when daemon is believed alive (no daemon_down fired)
  if (fires.length === 0 && state.lastEventAt !== null && state.activeRuns.length > 0) {
    const age = now - state.lastEventAt;
    if (age > o.quietThresholdMs) {
      fires.push({ kind: "quiet", last_event_age_ms: age });
    }
  }

  // run_stall — independent of quiet
  const stalled = state.activeRuns
    .filter((r) => r.age_seconds > o.runStallThresholdS)
    .map((r) => r.bead_id);
  if (stalled.length > 0) {
    fires.push({ kind: "run_stall", stalled_ids: stalled });
  }

  return fires;
}

// Arm a repeating watchdog tick. Caller provides the current state getter and
// an onFire callback. Returns a cancel function.
export function armWatchdog(
  getState: () => WatchdogState,
  onFire: (fires: WatchdogFire[]) => void,
  opts: WatchdogOptions = {},
  tickMs = 30_000
): () => void {
  const timer = setInterval(() => {
    const fires = checkWatchdog(getState(), opts);
    if (fires.length > 0) onFire(fires);
  }, tickMs);
  return () => clearInterval(timer);
}
