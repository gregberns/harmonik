import { describe, it, expect, beforeEach, afterEach } from "vitest";
import { mkdirSync, rmSync, existsSync } from "node:fs";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { randomUUID } from "node:crypto";

import {
  readWatermark,
  recordReaction,
  advanceWatermark,
  emptyWatermark,
  maxUUIDv7,
  isValidUUIDv7,
  WATERMARK_SCHEMA_VERSION,
  type WatermarkState,
} from "../watermark.js";

function tmpDir(): string {
  const dir = join(tmpdir(), "flywheel-test-" + randomUUID());
  mkdirSync(dir, { recursive: true });
  return dir;
}

// Produce ascending UUIDv7 strings by incrementing the timestamp nibble.
function makeUUIDv7(seq: number): string {
  const ts = String(seq).padStart(12, "0");
  return `${ts.slice(0, 8)}-${ts.slice(8, 12)}-7000-8000-000000000000`;
}

describe("isValidUUIDv7", () => {
  it("accepts valid UUIDv7", () => {
    expect(isValidUUIDv7("00000000-0000-7000-8000-000000000000")).toBe(true);
    expect(isValidUUIDv7("0192a1b2-c3d4-7e5f-8a6b-7c8d9e0f1a2b")).toBe(true);
  });

  it("rejects UUIDv4", () => {
    expect(isValidUUIDv7("550e8400-e29b-41d4-a716-446655440000")).toBe(false);
  });

  it("rejects empty / garbage", () => {
    expect(isValidUUIDv7("")).toBe(false);
    expect(isValidUUIDv7("not-a-uuid")).toBe(false);
  });
});

describe("maxUUIDv7", () => {
  it("returns the larger UUID", () => {
    const a = makeUUIDv7(1);
    const b = makeUUIDv7(2);
    expect(maxUUIDv7(a, b)).toBe(b);
    expect(maxUUIDv7(b, a)).toBe(b);
  });

  it("handles nulls", () => {
    const a = makeUUIDv7(1);
    expect(maxUUIDv7(a, null)).toBe(a);
    expect(maxUUIDv7(null, a)).toBe(a);
    expect(maxUUIDv7(null, null)).toBe(null);
  });
});

describe("readWatermark", () => {
  let dir: string;
  beforeEach(() => { dir = tmpDir(); });
  afterEach(() => { rmSync(dir, { recursive: true, force: true }); });

  it("returns null for missing file", () => {
    expect(readWatermark(join(dir, "missing.json"))).toBeNull();
  });

  it("returns null for corrupt JSON", () => {
    const p = join(dir, "state.json");
    require("node:fs").writeFileSync(p, "{bad json");
    expect(readWatermark(p)).toBeNull();
  });

  it("returns null for invalid UUIDv7 in state", () => {
    const p = join(dir, "state.json");
    const bad = { schema_version: 1, last_processed_event_id: "not-a-uuid", reacted_ledger: {}, updated_at: new Date().toISOString() };
    require("node:fs").writeFileSync(p, JSON.stringify(bad));
    expect(readWatermark(p)).toBeNull();
  });
});

describe("recordReaction + advanceWatermark — CL-053 ordering invariant", () => {
  let dir: string;
  let statePath: string;

  beforeEach(() => {
    dir = tmpDir();
    statePath = join(dir, "state.json");
  });
  afterEach(() => { rmSync(dir, { recursive: true, force: true }); });

  it("step 2 before step 3: ledger precedes watermark", () => {
    const seed = makeUUIDv7(1);
    let s = emptyWatermark(seed);
    const eventId = makeUUIDv7(2);

    // Step 2
    s = recordReaction(statePath, s, eventId, "reaction:test");
    const afterLedger = readWatermark(statePath)!;
    expect(afterLedger.reacted_ledger[eventId]).toBe("reaction:test");
    expect(afterLedger.last_processed_event_id).toBe(seed); // watermark NOT advanced yet

    // Step 3
    s = advanceWatermark(statePath, s, eventId);
    const afterAdvance = readWatermark(statePath)!;
    expect(afterAdvance.last_processed_event_id).toBe(eventId);
  });

  it("watermark never regresses — CL-054 / CL-INV-004", () => {
    const id1 = makeUUIDv7(5);
    const id2 = makeUUIDv7(3); // older
    let s = emptyWatermark(id1);
    s = advanceWatermark(statePath, s, id2); // attempt regression
    expect(s.last_processed_event_id).toBe(id1); // unchanged
    const persisted = readWatermark(statePath);
    // File not written when no change needed (state stays in memory)
    // OR file written with same value — both are compliant
    if (persisted) {
      expect(persisted.last_processed_event_id).toBe(id1);
    }
  });

  it("crash-injection scenario: ledger written but watermark not yet", () => {
    const seed = makeUUIDv7(10);
    let s = emptyWatermark(seed);
    const evId = makeUUIDv7(11);

    // Simulate crash after step 2 (ledger written) but before step 3 (watermark).
    s = recordReaction(statePath, s, evId, "reaction:foo");
    // "crash" — DO NOT call advanceWatermark.
    // On resume, ledger has the entry but watermark is unchanged.
    const resumed = readWatermark(statePath)!;
    expect(resumed.reacted_ledger[evId]).toBe("reaction:foo");
    expect(resumed.last_processed_event_id).toBe(seed);

    // Re-processing: reaction is idempotent (found in ledger → skip effect).
    // Watermark can now be advanced.
    const s2 = advanceWatermark(statePath, resumed, evId);
    expect(s2.last_processed_event_id).toBe(evId);
  });
});

describe("schema version enforcement", () => {
  let dir: string;
  beforeEach(() => { dir = tmpDir(); });
  afterEach(() => { rmSync(dir, { recursive: true, force: true }); });

  it("rejects future schema_version", () => {
    const p = join(dir, "state.json");
    const future = { schema_version: 99, last_processed_event_id: makeUUIDv7(1), reacted_ledger: {}, updated_at: new Date().toISOString() };
    require("node:fs").writeFileSync(p, JSON.stringify(future));
    expect(readWatermark(p)).toBeNull();
  });
});
