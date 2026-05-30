import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import {
  checkWatchdog,
  armWatchdog,
  createWatchdogState,
  type WatchdogState,
  type WatchdogFire,
} from "../watchdog.js";

describe("checkWatchdog — CL-064", () => {
  it("returns empty when no conditions met", () => {
    const state = createWatchdogState();
    const fires = checkWatchdog(state);
    expect(fires).toHaveLength(0);
  });

  it("daemon_down when heartbeat age > 3×interval", () => {
    const state: WatchdogState = {
      lastEventAt: null,
      lastHeartbeatAt: Date.now() - 181_000, // 181s ago, threshold 180s
      activeRuns: [],
    };
    const fires = checkWatchdog(state, { heartbeatIntervalMs: 60_000, heartbeatMultiplier: 3 });
    expect(fires.some((f) => f.kind === "daemon_down")).toBe(true);
  });

  it("no daemon_down when heartbeat is fresh", () => {
    const state: WatchdogState = {
      lastEventAt: null,
      lastHeartbeatAt: Date.now() - 60_000,
      activeRuns: [],
    };
    const fires = checkWatchdog(state, { heartbeatIntervalMs: 60_000, heartbeatMultiplier: 3 });
    expect(fires.some((f) => f.kind === "daemon_down")).toBe(false);
  });

  it("quiet when lastEventAt > 5min AND active_runs non-empty", () => {
    const state: WatchdogState = {
      lastEventAt: Date.now() - 301_000, // > 300s
      lastHeartbeatAt: Date.now() - 30_000, // fresh heartbeat
      activeRuns: [{ bead_id: "hk-abc", age_seconds: 100 }],
    };
    const fires = checkWatchdog(state, { quietThresholdMs: 300_000 });
    expect(fires.some((f) => f.kind === "quiet")).toBe(true);
  });

  it("no quiet when active_runs empty", () => {
    const state: WatchdogState = {
      lastEventAt: Date.now() - 400_000,
      lastHeartbeatAt: Date.now() - 30_000,
      activeRuns: [],
    };
    const fires = checkWatchdog(state, { quietThresholdMs: 300_000 });
    expect(fires.some((f) => f.kind === "quiet")).toBe(false);
  });

  it("run_stall when any run exceeds threshold", () => {
    const state: WatchdogState = {
      lastEventAt: Date.now() - 1_000,
      lastHeartbeatAt: Date.now() - 30_000,
      activeRuns: [
        { bead_id: "hk-abc", age_seconds: 700 },
        { bead_id: "hk-def", age_seconds: 100 },
      ],
    };
    const fires = checkWatchdog(state, { runStallThresholdS: 600 });
    const stall = fires.find((f) => f.kind === "run_stall");
    expect(stall).toBeDefined();
    expect(stall?.stalled_ids).toEqual(["hk-abc"]);
  });

  it("daemon_down suppresses quiet check", () => {
    const state: WatchdogState = {
      lastEventAt: Date.now() - 400_000,
      lastHeartbeatAt: Date.now() - 181_000, // daemon down
      activeRuns: [{ bead_id: "hk-abc", age_seconds: 100 }],
    };
    const fires = checkWatchdog(state, {
      quietThresholdMs: 300_000,
      heartbeatIntervalMs: 60_000,
      heartbeatMultiplier: 3,
    });
    // daemon_down should fire; quiet should NOT (suppressed when daemon down)
    expect(fires.some((f) => f.kind === "daemon_down")).toBe(true);
    expect(fires.some((f) => f.kind === "quiet")).toBe(false);
  });
});

describe("armWatchdog", () => {
  beforeEach(() => { vi.useFakeTimers(); });
  afterEach(() => { vi.useRealTimers(); });

  it("calls onFire when a condition is met on tick", () => {
    const fires: WatchdogFire[][] = [];
    const state: WatchdogState = {
      lastEventAt: null,
      lastHeartbeatAt: Date.now() - 200_000, // daemon down
      activeRuns: [],
    };
    const cancel = armWatchdog(() => state, (f) => fires.push(f), {
      heartbeatIntervalMs: 60_000,
      heartbeatMultiplier: 3,
    }, 1_000);

    vi.advanceTimersByTime(1_000);
    expect(fires.length).toBeGreaterThan(0);
    cancel();
  });

  it("cancel stops ticking", () => {
    const fires: WatchdogFire[][] = [];
    const state: WatchdogState = {
      lastEventAt: null,
      lastHeartbeatAt: Date.now() - 200_000,
      activeRuns: [],
    };
    const cancel = armWatchdog(() => state, (f) => fires.push(f), {}, 1_000);
    cancel();
    vi.advanceTimersByTime(5_000);
    expect(fires).toHaveLength(0);
  });
});
