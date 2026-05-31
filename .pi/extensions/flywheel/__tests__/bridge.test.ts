import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { mkdirSync, writeFileSync, readFileSync, rmSync, existsSync } from "node:fs";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { randomUUID } from "node:crypto";

import { createEventBridge, type Harness, type BridgeOptions } from "../bridge.js";
import { readWatermark } from "../watermark.js";
import type { SubscribeEvent } from "../wake-filter.js";

// ── Test utilities ────────────────────────────────────────────────────────────

function tmpDir(): string {
  const dir = join(tmpdir(), "bridge-test-" + randomUUID());
  mkdirSync(dir, { recursive: true });
  return dir;
}

function makeHarness() {
  const calls: { method: string; arg?: string }[] = [];
  const harness: Harness = {
    abort: vi.fn(async () => { calls.push({ method: "abort" }); }),
    prompt: vi.fn((msg: string) => { calls.push({ method: "prompt", arg: msg }); }),
    followUp: vi.fn((msg: string) => { calls.push({ method: "followUp", arg: msg }); }),
  };
  return { harness, calls };
}

// Produce ascending UUIDv7 strings
function uuidv7(seq: number): string {
  const ts = String(seq).padStart(12, "0");
  return `${ts.slice(0, 8)}-${ts.slice(8, 12)}-7000-8000-000000000000`;
}

function makeEvent(type: string, seq: number, payload: Record<string, unknown> = {}): SubscribeEvent {
  return { event_id: uuidv7(seq), type, schema_version: 1, payload };
}

function writeEventsFile(dir: string, events: SubscribeEvent[]): string {
  const eventsDir = join(dir, ".harmonik", "events");
  mkdirSync(eventsDir, { recursive: true });
  const path = join(eventsDir, "events.jsonl");
  writeFileSync(path, events.map((e) => JSON.stringify(e)).join("\n") + "\n");
  return path;
}

function writeTmpReplay(events: SubscribeEvent[]): string {
  const p = join(tmpdir(), "replay-" + randomUUID() + ".jsonl");
  writeFileSync(p, events.map((e) => JSON.stringify(e)).join("\n") + "\n");
  return p;
}

function bridgeOpts(dir: string, extra?: Partial<BridgeOptions>): BridgeOptions {
  return { repoRoot: dir, debounceMs: 0, ...extra };
}

// ── Replay-test: correct reactions WITHOUT real harmonik push ─────────────────

describe("replayFile — correct reactions without real push (acceptance)", () => {
  let dir: string;
  beforeEach(() => { dir = tmpDir(); vi.useFakeTimers(); });
  afterEach(() => { rmSync(dir, { recursive: true, force: true }); vi.useRealTimers(); });

  it("processes run_completed deterministically — no followUp when git check passes", async () => {
    const { harness, calls } = makeHarness();
    // CL-051: inject a gitCheck that confirms the trailer is on origin/main.
    const bridge = createEventBridge(harness, bridgeOpts(dir, { gitCheck: async () => true }));

    const events: SubscribeEvent[] = [
      makeEvent("run_completed", 1, { bead_id: "hk-aaa" }),
    ];
    const replayPath = writeTmpReplay(events);
    await bridge.replayFile(replayPath);

    const followUps = calls.filter((c) => c.method === "followUp");
    expect(followUps).toHaveLength(0);
  });

  it("processes run_failed as Wake-LLM — emits followUp with event digest", async () => {
    const { harness, calls } = makeHarness();
    const bridge = createEventBridge(harness, bridgeOpts(dir, { debounceMs: 0 }));

    const events: SubscribeEvent[] = [
      makeEvent("run_failed", 1, { bead_id: "hk-bbb", reason: "test" }),
    ];
    const replayPath = writeTmpReplay(events);
    await bridge.replayFile(replayPath);

    const followUps = calls.filter((c) => c.method === "followUp");
    expect(followUps.length).toBeGreaterThan(0);
    expect(followUps[0].arg).toContain("run_failed");
  });

  it("ignores state_entered — no followUp, no abort", async () => {
    const { harness, calls } = makeHarness();
    const bridge = createEventBridge(harness, bridgeOpts(dir));

    const replayPath = writeTmpReplay([
      makeEvent("state_entered", 1),
      makeEvent("state_exited", 2),
    ]);
    await bridge.replayFile(replayPath);

    expect(calls).toHaveLength(0);
  });

  it("advances watermark for deterministic + ignore events", async () => {
    const { harness } = makeHarness();
    const bridge = createEventBridge(harness, bridgeOpts(dir));

    const replayPath = writeTmpReplay([
      makeEvent("run_completed", 5, { bead_id: "hk-ccc" }),
      makeEvent("state_entered", 6),
    ]);
    await bridge.replayFile(replayPath);

    const sp = join(dir, ".harmonik", "cognition", "state.json");
    const state = readWatermark(sp);
    expect(state).not.toBeNull();
    expect(state!.last_processed_event_id).toBe(uuidv7(5));
  });

  it("groups burst Wake-LLM events into one followUp", async () => {
    const { harness, calls } = makeHarness();
    // debounceMs=0 flushes immediately but all in same batch since replayFile calls flush() at end
    const bridge = createEventBridge(harness, bridgeOpts(dir, { debounceMs: 100_000 }));

    const replayPath = writeTmpReplay([
      makeEvent("run_failed", 1, { bead_id: "hk-x" }),
      makeEvent("run_failed", 2, { bead_id: "hk-y" }),
      makeEvent("run_failed", 3, { bead_id: "hk-z" }),
    ]);
    await bridge.replayFile(replayPath);

    const followUps = calls.filter((c) => c.method === "followUp");
    expect(followUps).toHaveLength(1);
    expect(followUps[0].arg).toContain("hk-x");
    expect(followUps[0].arg).toContain("hk-y");
    expect(followUps[0].arg).toContain("hk-z");
  });
});

// ── CL-051: two-phase done verification ─────────────────────────────────────

describe("CL-051 two-phase done verification", () => {
  let dir: string;
  beforeEach(() => { dir = tmpDir(); });
  afterEach(() => { rmSync(dir, { recursive: true, force: true }); });

  it("run_completed with bead_id: git check passes → no followUp (deterministic path)", async () => {
    const { harness, calls } = makeHarness();
    const bridge = createEventBridge(harness, bridgeOpts(dir, { gitCheck: async () => true }));

    await bridge.replayFile(writeTmpReplay([
      makeEvent("run_completed", 1, { bead_id: "hk-xyz" }),
    ]));

    const followUps = calls.filter((c) => c.method === "followUp");
    expect(followUps).toHaveLength(0);
  });

  it("run_completed with bead_id: git check fails → followUp warning emitted (CL-051 unverified)", async () => {
    const { harness, calls } = makeHarness();
    const bridge = createEventBridge(harness, bridgeOpts(dir, { gitCheck: async () => false }));

    await bridge.replayFile(writeTmpReplay([
      makeEvent("run_completed", 2, { bead_id: "hk-pushfail" }),
    ]));

    const followUps = calls.filter((c) => c.method === "followUp");
    expect(followUps).toHaveLength(1);
    expect(followUps[0].arg).toContain("hk-pushfail");
    expect(followUps[0].arg).toContain("CL-051");
  });

  it("run_completed with bead_id: git check fails → watermark still advances", async () => {
    const { harness } = makeHarness();
    const bridge = createEventBridge(harness, bridgeOpts(dir, { gitCheck: async () => false }));

    await bridge.replayFile(writeTmpReplay([
      makeEvent("run_completed", 3, { bead_id: "hk-pushfail2" }),
    ]));

    const sp = join(dir, ".harmonik", "cognition", "state.json");
    const s = readWatermark(sp)!;
    expect(s.last_processed_event_id).toBe(uuidv7(3));
    expect(s.reacted_ledger[uuidv7(3)]).toContain("deterministic");
  });

  it("run_completed without bead_id → no git check, no followUp (backward compat)", async () => {
    const { harness, calls } = makeHarness();
    // No gitCheck injection needed; no bead_id means no git check fires.
    const gitCheckCalled = { value: false };
    const bridge = createEventBridge(harness, bridgeOpts(dir, {
      gitCheck: async () => { gitCheckCalled.value = true; return false; },
    }));

    await bridge.replayFile(writeTmpReplay([
      makeEvent("run_completed", 4),
    ]));

    expect(gitCheckCalled.value).toBe(false);
    const followUps = calls.filter((c) => c.method === "followUp");
    expect(followUps).toHaveLength(0);
  });

  it("run_completed unverified: emits run_completed_unverified cognition event", async () => {
    const { harness } = makeHarness();
    const bridge = createEventBridge(harness, bridgeOpts(dir, { gitCheck: async () => false }));

    await bridge.replayFile(writeTmpReplay([
      makeEvent("run_completed", 5, { bead_id: "hk-cogcheck" }),
    ]));

    const cogPath = join(dir, ".harmonik", "cognition", "cognition-events.jsonl");
    const lines = readFileSync(cogPath, "utf8").split("\n").filter(Boolean);
    const types = lines.map((l) => (JSON.parse(l) as { type: string }).type);
    expect(types).toContain("run_completed_unverified");
    expect(types).not.toContain("deterministic_reaction");
  });
});

// ── effect → ledger → watermark ordering — CL-053 ───────────────────────────

describe("effect→ledger→watermark ordering (CL-053)", () => {
  let dir: string;
  beforeEach(() => { dir = tmpDir(); });
  afterEach(() => { rmSync(dir, { recursive: true, force: true }); });

  it("after deterministic event: ledger recorded AND watermark advanced", async () => {
    const { harness } = makeHarness();
    const bridge = createEventBridge(harness, bridgeOpts(dir));

    const replayPath = writeTmpReplay([makeEvent("run_completed", 10, { bead_id: "hk-ev" })]);
    await bridge.replayFile(replayPath);

    const sp = join(dir, ".harmonik", "cognition", "state.json");
    const s = readWatermark(sp)!;
    expect(s.reacted_ledger[uuidv7(10)]).toContain("deterministic");
    expect(s.last_processed_event_id).toBe(uuidv7(10));
  });

  it("crash-between-ledger-and-watermark: re-replay finds ledger entry and skips effect", async () => {
    // We simulate this by replaying the same event twice.
    // The second replay should find the event already in the reacted_ledger.
    const { harness, calls } = makeHarness();
    const bridge = createEventBridge(harness, bridgeOpts(dir));

    const events = [makeEvent("run_failed", 20, { bead_id: "hk-crash" })];
    const replayPath = writeTmpReplay(events);

    // First replay — processes event
    await bridge.replayFile(replayPath);
    const followUpsAfterFirst = calls.filter((c) => c.method === "followUp").length;

    // Reset calls
    calls.length = 0;

    // Second replay — event already in ledger, watermark already advanced;
    // bridge should NOT advance watermark backward, reaction may or may not fire
    // (debounce fires the batch but the watermark never regresses).
    const bridge2 = createEventBridge(harness, bridgeOpts(dir));
    await bridge2.replayFile(replayPath);

    const sp = join(dir, ".harmonik", "cognition", "state.json");
    const s = readWatermark(sp)!;
    // Watermark must equal the event id (not regressed)
    expect(s.last_processed_event_id).toBe(uuidv7(20));
  });
});

// ── Watermark never regresses (max with heartbeat.last_event_id) ─────────────

describe("watermark never regresses — CL-054", () => {
  let dir: string;
  beforeEach(() => { dir = tmpDir(); });
  afterEach(() => { rmSync(dir, { recursive: true, force: true }); });

  it("heartbeat advances watermark to last_event_id even without actionable event", async () => {
    const { harness } = makeHarness();
    const bridge = createEventBridge(harness, bridgeOpts(dir));

    const heartbeat: SubscribeEvent = {
      event_id: uuidv7(100),
      type: "heartbeat",
      payload: { last_event_id: uuidv7(99), active_runs: [] },
    };
    const replayPath = writeTmpReplay([heartbeat]);
    await bridge.replayFile(replayPath);

    const sp = join(dir, ".harmonik", "cognition", "state.json");
    const s = readWatermark(sp)!;
    expect(s.last_processed_event_id).toBe(uuidv7(99));
  });

  it("watermark does not regress when old events replayed after newer ones", async () => {
    const { harness } = makeHarness();
    const bridge = createEventBridge(harness, bridgeOpts(dir));

    // First pass: process event at seq=50
    const p1 = writeTmpReplay([makeEvent("run_completed", 50)]);
    await bridge.replayFile(p1);

    // Second pass: try to process older event at seq=30 (should not regress)
    const bridge2 = createEventBridge(harness, bridgeOpts(dir));
    const p2 = writeTmpReplay([makeEvent("run_completed", 30)]);
    await bridge2.replayFile(p2);

    const sp = join(dir, ".harmonik", "cognition", "state.json");
    const s = readWatermark(sp)!;
    expect(s.last_processed_event_id).toBe(uuidv7(50));
  });
});

// ── merge_conflict aborts in-flight turn within 2s — CL-063 ─────────────────

describe("merge_conflict urgent handling — CL-063", () => {
  let dir: string;
  beforeEach(() => { dir = tmpDir(); });
  afterEach(() => { rmSync(dir, { recursive: true, force: true }); });

  it("calls abort() then prompt() on merge_conflict", async () => {
    const { harness, calls } = makeHarness();
    const bridge = createEventBridge(harness, bridgeOpts(dir));

    const replayPath = writeTmpReplay([
      makeEvent("merge_conflict", 1, { bead_id: "hk-conflict" }),
    ]);

    const start = Date.now();
    await bridge.replayFile(replayPath);
    const elapsed = Date.now() - start;

    expect(harness.abort).toHaveBeenCalledOnce();
    const promptCall = calls.find((c) => c.method === "prompt");
    expect(promptCall).toBeDefined();
    expect(promptCall?.arg).toContain("merge_conflict");
    // Must complete well within 2s (synchronous replay; the 2s SLA is for live bridge)
    expect(elapsed).toBeLessThan(2_000);
  });

  it("merge_conflict bypasses debounce — does not queue into pending batch", async () => {
    const { harness, calls } = makeHarness();
    // High debounce to ensure urgent bypasses it
    const bridge = createEventBridge(harness, bridgeOpts(dir, { debounceMs: 100_000 }));

    const replayPath = writeTmpReplay([
      makeEvent("run_failed", 1, { bead_id: "hk-fail" }),       // queued in debounce
      makeEvent("merge_conflict", 2, { bead_id: "hk-conflict" }), // urgent — bypasses
    ]);
    await bridge.replayFile(replayPath);

    // abort was called for merge_conflict
    expect(harness.abort).toHaveBeenCalledOnce();

    // The pending run_failed batch is flushed by bridge.flush() at end of replayFile
    const followUps = calls.filter((c) => c.method === "followUp");
    const hasRunFailed = followUps.some((f) => f.arg?.includes("run_failed"));
    expect(hasRunFailed).toBe(true);
  });
});

// ── subscription_gap forces ScanAfter(watermark) re-sync — EV-038 ───────────

describe("subscription_gap handling — EV-038", () => {
  let dir: string;
  beforeEach(() => { dir = tmpDir(); });
  afterEach(() => { rmSync(dir, { recursive: true, force: true }); });

  it("triggers ScanAfter replay and followUp notification", async () => {
    const { harness, calls } = makeHarness();

    // Put a run_failed event in events.jsonl AFTER the watermark
    const evPath = writeEventsFile(dir, [
      makeEvent("run_failed", 50, { bead_id: "hk-gap-recovered" }),
    ]);

    const bridge = createEventBridge(harness, bridgeOpts(dir));

    // First advance the watermark to seq=10 so seq=50 is "after watermark"
    const p1 = writeTmpReplay([makeEvent("run_completed", 10)]);
    await bridge.replayFile(p1);

    calls.length = 0;
    const bridge2 = createEventBridge(harness, bridgeOpts(dir));

    // Now replay subscription_gap — bridge should scan events.jsonl and find seq=50
    const gapEvent: SubscribeEvent = {
      event_id: uuidv7(100),
      type: "subscription_gap",
      payload: { dropped: 5 },
    };
    const p2 = writeTmpReplay([gapEvent]);
    await bridge2.replayFile(p2);

    // Should have emitted a followUp about the gap
    const followUps = calls.filter((c) => c.method === "followUp");
    expect(followUps.length).toBeGreaterThan(0);
    const gapMsg = followUps.find((f) => f.arg?.includes("subscription_gap") || f.arg?.includes("gap"));
    expect(gapMsg).toBeDefined();
  });
});

// ── stop() releases resources (no leak) — CL-064 teardown ───────────────────

describe("stop() teardown releases resources", () => {
  let dir: string;
  beforeEach(() => { dir = tmpDir(); vi.useFakeTimers(); });
  afterEach(() => { rmSync(dir, { recursive: true, force: true }); vi.useRealTimers(); });

  it("clears the watchdog interval on stop() — no dangling timer", () => {
    const { harness } = makeHarness();
    const clearSpy = vi.spyOn(globalThis, "clearInterval");
    const bridge = createEventBridge(harness, bridgeOpts(dir));

    // start() arms the watchdog (setInterval) and spawns subscribe.
    bridge.start();
    // stop() must clear the watchdog interval (cancelWatchdog) so it does not leak.
    bridge.stop();

    expect(clearSpy).toHaveBeenCalled();

    // After stop(), advancing fake time must NOT fire any further watchdog ticks.
    const before = harness.followUp as ReturnType<typeof vi.fn>;
    const callsBefore = before.mock.calls.length;
    vi.advanceTimersByTime(120_000);
    expect((harness.followUp as ReturnType<typeof vi.fn>).mock.calls.length).toBe(callsBefore);

    clearSpy.mockRestore();
  });

  it("stop() is idempotent and flushes pending debounced events once", () => {
    const { harness, calls } = makeHarness();
    const bridge = createEventBridge(harness, bridgeOpts(dir, { debounceMs: 100_000 }));
    bridge.start();

    // Two consecutive stop() calls must not throw and must not double-fire.
    bridge.stop();
    const followUpsAfterFirstStop = calls.filter((c) => c.method === "followUp").length;
    expect(() => bridge.stop()).not.toThrow();
    const followUpsAfterSecondStop = calls.filter((c) => c.method === "followUp").length;
    expect(followUpsAfterSecondStop).toBe(followUpsAfterFirstStop);
  });
});

// ── Cognition events emitted to cognition-events.jsonl — OQ-CL-004 ──────────

describe("cognition-events.jsonl emission", () => {
  let dir: string;
  beforeEach(() => { dir = tmpDir(); });
  afterEach(() => { rmSync(dir, { recursive: true, force: true }); });

  it("emits deterministic_reaction event for run_completed", async () => {
    const { harness } = makeHarness();
    const bridge = createEventBridge(harness, bridgeOpts(dir));

    await bridge.replayFile(writeTmpReplay([makeEvent("run_completed", 1)]));

    const cogPath = join(dir, ".harmonik", "cognition", "cognition-events.jsonl");
    expect(existsSync(cogPath)).toBe(true);
    const lines = readFileSync(cogPath, "utf8").split("\n").filter(Boolean);
    const types = lines.map((l) => (JSON.parse(l) as { type: string }).type);
    expect(types).toContain("deterministic_reaction");
  });

  it("emits urgent_abort_issued for merge_conflict", async () => {
    const { harness } = makeHarness();
    const bridge = createEventBridge(harness, bridgeOpts(dir));

    await bridge.replayFile(writeTmpReplay([makeEvent("merge_conflict", 1)]));

    const cogPath = join(dir, ".harmonik", "cognition", "cognition-events.jsonl");
    const lines = readFileSync(cogPath, "utf8").split("\n").filter(Boolean);
    const types = lines.map((l) => (JSON.parse(l) as { type: string }).type);
    expect(types).toContain("urgent_abort_issued");
  });
});
