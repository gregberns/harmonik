// dispatcher.test.ts — acceptance tests for CL-071/072/073 eager pure-code refill.
//
// Tests use fully-injectable deps so no real harmonik/kerf/git processes are spawned.

import { describe, it, expect, beforeEach, afterEach } from "vitest";
import { mkdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { randomUUID } from "node:crypto";

import { createDispatcher, type DispatcherDeps, type ActiveQueueInfo } from "../dispatcher.js";

// ── Fixtures ─────────────────────────────────────────────────────────────────

function tmpDir(): string {
  const dir = join(tmpdir(), "dispatcher-test-" + randomUUID());
  mkdirSync(dir, { recursive: true });
  return dir;
}

function uuidv7(seq: number): string {
  const ts = String(seq).padStart(12, "0");
  return `${ts.slice(0, 8)}-${ts.slice(8, 12)}-7000-8000-000000000000`;
}

// Build a dispatcher with all injectable deps mocked to safe defaults.
function makeDispatcher(
  repoRoot: string,
  deps: Partial<DispatcherDeps>,
) {
  return createDispatcher({ repoRoot, deps });
}

// Build opts for onSlotReleased with captured calls.
function makeSlotOpts(overrides: {
  triggeringEventId?: string;
  beadId?: string;
} = {}) {
  const wakeReasons: string[] = [];
  const cognitionTypes: string[] = [];
  return {
    opts: {
      triggerType: "run_completed" as const,
      triggeringEventId: overrides.triggeringEventId ?? uuidv7(1),
      beadId: overrides.beadId,
      onWakeModel: (reason: string) => { wakeReasons.push(reason); },
      onCognitionEvent: (record: Record<string, unknown>) => {
        if (typeof record.type === "string") cognitionTypes.push(record.type);
      },
    },
    wakeReasons,
    cognitionTypes,
  };
}

// ── Tests ─────────────────────────────────────────────────────────────────────

describe("CL-071: eager refill dispatches via queue append when queue active", () => {
  let dir: string;
  beforeEach(() => { dir = tmpDir(); });
  afterEach(() => { rmSync(dir, { recursive: true, force: true }); });

  it("appends bead to active stream queue when kerf next returns a candidate", async () => {
    const appendedBeads: string[] = [];
    const dispatcher = makeDispatcher(dir, {
      kerfNextBeads: async () => ["hk-aaa"],
      readActiveQueue: () => ({
        queueId: "queue-uuid-1",
        beadsInQueue: new Set<string>(),
        hasStreamGroup: true,
      }),
      gitCheck: async () => false,
      queueAppend: async (_r, _qid, beadId) => { appendedBeads.push(beadId); return true; },
      queueSubmit: async () => ({ queueId: null, ok: false }),
    });

    const { opts } = makeSlotOpts();
    await dispatcher.onSlotReleased(opts);

    expect(appendedBeads).toEqual(["hk-aaa"]);
  });

  it("writes dispatch-log entry with method=queue_append on success", async () => {
    const dispatcher = makeDispatcher(dir, {
      kerfNextBeads: async () => ["hk-bbb"],
      readActiveQueue: () => ({ queueId: "q2", beadsInQueue: new Set<string>(), hasStreamGroup: true }),
      gitCheck: async () => false,
      queueAppend: async () => true,
      queueSubmit: async () => ({ queueId: null, ok: false }),
    });

    const { opts } = makeSlotOpts({ triggeringEventId: uuidv7(10) });
    await dispatcher.onSlotReleased(opts);

    const logPath = join(dir, ".harmonik", "cognition", "dispatch-log.jsonl");
    const content = readFileSync(logPath, "utf8");
    expect(content).toContain('"method":"queue_append"');
    expect(content).toContain('"dispatched_bead":"hk-bbb"');
    expect(content).toContain(`"triggering_event_id":"${uuidv7(10)}"`);
  });

  it("emits eager_refill_dispatched cognition event on success", async () => {
    const dispatcher = makeDispatcher(dir, {
      kerfNextBeads: async () => ["hk-ccc"],
      readActiveQueue: () => ({ queueId: "q3", beadsInQueue: new Set<string>(), hasStreamGroup: true }),
      gitCheck: async () => false,
      queueAppend: async () => true,
      queueSubmit: async () => ({ queueId: null, ok: false }),
    });

    const { opts, cognitionTypes } = makeSlotOpts();
    await dispatcher.onSlotReleased(opts);

    expect(cognitionTypes).toContain("eager_refill_dispatched");
  });
});

describe("CL-071: first-fill dispatches via queue submit when no active queue", () => {
  let dir: string;
  beforeEach(() => { dir = tmpDir(); });
  afterEach(() => { rmSync(dir, { recursive: true, force: true }); });

  it("submits a new queue when no active queue exists", async () => {
    const submittedBeads: string[] = [];
    const dispatcher = makeDispatcher(dir, {
      kerfNextBeads: async () => ["hk-first"],
      readActiveQueue: () => null,
      gitCheck: async () => false,
      queueSubmit: async (_r, beadId) => { submittedBeads.push(beadId); return { queueId: "new-q", ok: true }; },
      queueAppend: async () => false,
    });

    const { opts } = makeSlotOpts();
    await dispatcher.onSlotReleased(opts);

    expect(submittedBeads).toEqual(["hk-first"]);
  });

  it("writes dispatch-log entry with method=queue_submit", async () => {
    const dispatcher = makeDispatcher(dir, {
      kerfNextBeads: async () => ["hk-submit-bead"],
      readActiveQueue: () => null,
      gitCheck: async () => false,
      queueSubmit: async () => ({ queueId: "q-new", ok: true }),
      queueAppend: async () => false,
    });

    const { opts } = makeSlotOpts({ triggeringEventId: uuidv7(20) });
    await dispatcher.onSlotReleased(opts);

    const content = readFileSync(join(dir, ".harmonik", "cognition", "dispatch-log.jsonl"), "utf8");
    expect(content).toContain('"method":"queue_submit"');
    expect(content).toContain('"dispatched_bead":"hk-submit-bead"');
  });
});

describe("CL-073: wake model when kerf next empty", () => {
  let dir: string;
  beforeEach(() => { dir = tmpDir(); });
  afterEach(() => { rmSync(dir, { recursive: true, force: true }); });

  it("wakes model with empty_queue reason when kerf next returns []", async () => {
    const dispatcher = makeDispatcher(dir, {
      kerfNextBeads: async () => [],
      readActiveQueue: () => null,
      gitCheck: async () => false,
      queueSubmit: async () => ({ queueId: null, ok: false }),
      queueAppend: async () => false,
    });

    const { opts, wakeReasons } = makeSlotOpts();
    await dispatcher.onSlotReleased(opts);

    expect(wakeReasons).toContain("empty_queue");
  });

  it("emits eager_refill_kerf_empty cognition event", async () => {
    const dispatcher = makeDispatcher(dir, {
      kerfNextBeads: async () => [],
      readActiveQueue: () => null,
      gitCheck: async () => false,
      queueSubmit: async () => ({ queueId: null, ok: false }),
      queueAppend: async () => false,
    });

    const { opts, cognitionTypes } = makeSlotOpts();
    await dispatcher.onSlotReleased(opts);

    expect(cognitionTypes).toContain("eager_refill_kerf_empty");
  });

  it("wakes model when all candidates are pre-screened out", async () => {
    const dispatcher = makeDispatcher(dir, {
      kerfNextBeads: async () => ["hk-in-q"],
      readActiveQueue: () => ({ queueId: "q1", beadsInQueue: new Set(["hk-in-q"]), hasStreamGroup: true }),
      gitCheck: async () => false,
      queueSubmit: async () => ({ queueId: null, ok: false }),
      queueAppend: async () => false,
    });

    const { opts, wakeReasons } = makeSlotOpts();
    await dispatcher.onSlotReleased(opts);

    expect(wakeReasons).toContain("empty_queue");
  });
});

describe("CL-072 guard #1: already-in-queue skip", () => {
  let dir: string;
  beforeEach(() => { dir = tmpDir(); });
  afterEach(() => { rmSync(dir, { recursive: true, force: true }); });

  it("skips candidate already in queue and falls through to next", async () => {
    const appendedBeads: string[] = [];
    const dispatcher = makeDispatcher(dir, {
      kerfNextBeads: async () => ["hk-inq", "hk-fresh"],
      readActiveQueue: () => ({
        queueId: "q1",
        beadsInQueue: new Set(["hk-inq"]),
        hasStreamGroup: true,
      }),
      gitCheck: async () => false,
      queueAppend: async (_r, _qid, beadId) => { appendedBeads.push(beadId); return true; },
      queueSubmit: async () => ({ queueId: null, ok: false }),
    });

    const { opts } = makeSlotOpts();
    await dispatcher.onSlotReleased(opts);

    expect(appendedBeads).toEqual(["hk-fresh"]);
  });

  it("writes already_in_queue skip to dispatch-log", async () => {
    const dispatcher = makeDispatcher(dir, {
      kerfNextBeads: async () => ["hk-inq"],
      readActiveQueue: () => ({ queueId: "q1", beadsInQueue: new Set(["hk-inq"]), hasStreamGroup: true }),
      gitCheck: async () => false,
      queueAppend: async () => true,
      queueSubmit: async () => ({ queueId: null, ok: false }),
    });

    const { opts } = makeSlotOpts({ triggeringEventId: uuidv7(30) });
    await dispatcher.onSlotReleased(opts);

    const content = readFileSync(join(dir, ".harmonik", "cognition", "dispatch-log.jsonl"), "utf8");
    expect(content).toContain('"skipped_reason":"already_in_queue"');
  });
});

describe("CL-072 guard #2: already-landed skip", () => {
  let dir: string;
  beforeEach(() => { dir = tmpDir(); });
  afterEach(() => { rmSync(dir, { recursive: true, force: true }); });

  it("skips candidate that is already landed on origin/main", async () => {
    const appendedBeads: string[] = [];
    const dispatcher = makeDispatcher(dir, {
      kerfNextBeads: async () => ["hk-landed", "hk-fresh"],
      readActiveQueue: () => ({ queueId: "q1", beadsInQueue: new Set<string>(), hasStreamGroup: true }),
      gitCheck: async (_r, beadId) => beadId === "hk-landed",
      queueAppend: async (_r, _qid, beadId) => { appendedBeads.push(beadId); return true; },
      queueSubmit: async () => ({ queueId: null, ok: false }),
    });

    const { opts } = makeSlotOpts();
    await dispatcher.onSlotReleased(opts);

    expect(appendedBeads).toEqual(["hk-fresh"]);
  });

  it("writes already_landed skip to dispatch-log", async () => {
    const dispatcher = makeDispatcher(dir, {
      kerfNextBeads: async () => ["hk-landed"],
      readActiveQueue: () => ({ queueId: "q1", beadsInQueue: new Set<string>(), hasStreamGroup: true }),
      gitCheck: async () => true,
      queueAppend: async () => true,
      queueSubmit: async () => ({ queueId: null, ok: false }),
    });

    const { opts } = makeSlotOpts();
    await dispatcher.onSlotReleased(opts);

    const content = readFileSync(join(dir, ".harmonik", "cognition", "dispatch-log.jsonl"), "utf8");
    expect(content).toContain('"skipped_reason":"already_landed"');
  });
});

describe("CL-072 guard #3: failed-twice halt", () => {
  let dir: string;
  beforeEach(() => { dir = tmpDir(); });
  afterEach(() => { rmSync(dir, { recursive: true, force: true }); });

  it("halts refill and wakes model when candidate failed twice this session", async () => {
    const dispatcher = makeDispatcher(dir, {
      kerfNextBeads: async () => ["hk-flaky"],
      readActiveQueue: () => null,
      gitCheck: async () => false,
      queueSubmit: async () => ({ queueId: null, ok: false }),
      queueAppend: async () => false,
    });

    dispatcher.recordBeadFailure("hk-flaky");
    dispatcher.recordBeadFailure("hk-flaky");

    const { opts, wakeReasons, cognitionTypes } = makeSlotOpts();
    await dispatcher.onSlotReleased(opts);

    expect(wakeReasons).toContain("failed_twice_halt");
    expect(cognitionTypes).toContain("eager_refill_halt_failed_twice");
  });

  it("does NOT halt when bead has failed only once", async () => {
    const submittedBeads: string[] = [];
    const dispatcher = makeDispatcher(dir, {
      kerfNextBeads: async () => ["hk-once"],
      readActiveQueue: () => null,
      gitCheck: async () => false,
      queueSubmit: async (_r, beadId) => { submittedBeads.push(beadId); return { queueId: "q", ok: true }; },
      queueAppend: async () => false,
    });

    dispatcher.recordBeadFailure("hk-once"); // only once

    const { opts, wakeReasons } = makeSlotOpts();
    await dispatcher.onSlotReleased(opts);

    expect(wakeReasons).toHaveLength(0);
    expect(submittedBeads).toEqual(["hk-once"]);
  });
});

describe("CL-055/056: idempotency guard", () => {
  let dir: string;
  beforeEach(() => { dir = tmpDir(); });
  afterEach(() => { rmSync(dir, { recursive: true, force: true }); });

  it("skips dispatch when same event_id already processed", async () => {
    const appendedBeads: string[] = [];
    const dispatcher = makeDispatcher(dir, {
      kerfNextBeads: async () => ["hk-idem"],
      readActiveQueue: () => ({ queueId: "q1", beadsInQueue: new Set<string>(), hasStreamGroup: true }),
      gitCheck: async () => false,
      queueAppend: async (_r, _qid, beadId) => { appendedBeads.push(beadId); return true; },
      queueSubmit: async () => ({ queueId: null, ok: false }),
    });

    const eid = uuidv7(99);
    const { opts: opts1 } = makeSlotOpts({ triggeringEventId: eid });
    await dispatcher.onSlotReleased(opts1);
    expect(appendedBeads).toEqual(["hk-idem"]);

    // Second call with same event_id — should be skipped.
    const { opts: opts2, cognitionTypes } = makeSlotOpts({ triggeringEventId: eid });
    await dispatcher.onSlotReleased(opts2);
    expect(appendedBeads).toHaveLength(1); // not appended again
    expect(cognitionTypes).toContain("eager_refill_idempotent_skip");
  });
});

describe("bridge integration: dispatcher called from run_completed (deterministic)", () => {
  // These tests verify the bridge→dispatcher wiring via replayFile + injected dispatcher.
  let dir: string;
  beforeEach(() => { dir = tmpDir(); });
  afterEach(() => { rmSync(dir, { recursive: true, force: true }); });

  it("bridge calls dispatcher.onSlotReleased when run_completed passes gitCheck", async () => {
    const { createEventBridge } = await import("../bridge.js");

    const slotReleasedCalls: string[] = [];
    const mockDispatcher = {
      recordBeadFailure: (_beadId: string) => {},
      onSlotReleased: async (opts: { triggeringEventId: string }) => {
        slotReleasedCalls.push(opts.triggeringEventId);
      },
    };

    const harness = {
      abort: async () => {},
      prompt: (_m: string) => {},
      followUp: (_m: string) => {},
    };

    const eid = uuidv7(50);
    const replayPath = join(tmpdir(), `replay-bridge-${randomUUID()}.jsonl`);
    writeFileSync(replayPath, JSON.stringify({
      event_id: eid,
      type: "run_completed",
      schema_version: 1,
      payload: { bead_id: "hk-zzz" },
    }) + "\n");

    const bridge = createEventBridge(harness, {
      repoRoot: dir,
      gitCheck: async () => true,
      dispatcher: mockDispatcher,
      debounceMs: 0,
    });
    await bridge.replayFile(replayPath);

    expect(slotReleasedCalls).toContain(eid);
  });

  it("bridge calls dispatcher.recordBeadFailure + onSlotReleased for run_failed", async () => {
    const { createEventBridge } = await import("../bridge.js");

    const recordedFailures: string[] = [];
    const slotReleasedCalls: string[] = [];
    const mockDispatcher = {
      recordBeadFailure: (beadId: string) => { recordedFailures.push(beadId); },
      onSlotReleased: async (opts: { triggeringEventId: string }) => {
        slotReleasedCalls.push(opts.triggeringEventId);
      },
    };

    const harness = {
      abort: async () => {},
      prompt: (_m: string) => {},
      followUp: (_m: string) => {},
    };

    const eid = uuidv7(60);
    const replayPath = join(tmpdir(), `replay-fail-${randomUUID()}.jsonl`);
    writeFileSync(replayPath, JSON.stringify({
      event_id: eid,
      type: "run_failed",
      schema_version: 1,
      payload: { bead_id: "hk-fail-bead" },
    }) + "\n");

    const bridge = createEventBridge(harness, {
      repoRoot: dir,
      gitCheck: async () => false,
      dispatcher: mockDispatcher,
      debounceMs: 0,
    });
    await bridge.replayFile(replayPath);

    expect(recordedFailures).toContain("hk-fail-bead");
    expect(slotReleasedCalls).toContain(eid);
  });
});

