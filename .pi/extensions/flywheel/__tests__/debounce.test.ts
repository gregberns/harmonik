import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { createDebouncer, DEBOUNCE_MS } from "../debounce.js";
import type { SubscribeEvent } from "../wake-filter.js";

function evt(type: string, seq = 0): SubscribeEvent {
  return { event_id: `00000000-0000-7000-8000-${String(seq).padStart(12, "0")}`, type };
}

describe("createDebouncer — CL-062 burst debounce", () => {
  beforeEach(() => { vi.useFakeTimers(); });
  afterEach(() => { vi.useRealTimers(); });

  it("collects events within the window and flushes after debounce interval", () => {
    const flushed: SubscribeEvent[][] = [];
    const d = createDebouncer((batch) => flushed.push(batch), 400);

    d.add(evt("run_failed", 1));
    d.add(evt("run_failed", 2));
    expect(flushed).toHaveLength(0); // not yet

    vi.advanceTimersByTime(400);
    expect(flushed).toHaveLength(1);
    expect(flushed[0]).toHaveLength(2);
  });

  it("resets the timer on each add (debounce behaviour)", () => {
    const flushed: SubscribeEvent[][] = [];
    const d = createDebouncer((batch) => flushed.push(batch), 400);

    d.add(evt("run_failed", 1));
    vi.advanceTimersByTime(300);
    d.add(evt("run_failed", 2)); // resets timer
    vi.advanceTimersByTime(300); // only 300ms since last add — no flush yet
    expect(flushed).toHaveLength(0);

    vi.advanceTimersByTime(100); // now 400ms since last add
    expect(flushed).toHaveLength(1);
    expect(flushed[0]).toHaveLength(2);
  });

  it("flush() drains pending events immediately", () => {
    const flushed: SubscribeEvent[][] = [];
    const d = createDebouncer((batch) => flushed.push(batch), 400);

    d.add(evt("run_failed", 1));
    d.flush();
    expect(flushed).toHaveLength(1);
    expect(flushed[0]).toHaveLength(1);

    // Timer is cancelled — no double-flush
    vi.advanceTimersByTime(400);
    expect(flushed).toHaveLength(1);
  });

  it("clear() discards pending events without calling onFlush", () => {
    const flushed: SubscribeEvent[][] = [];
    const d = createDebouncer((batch) => flushed.push(batch), 400);

    d.add(evt("run_failed", 1));
    d.clear();
    vi.advanceTimersByTime(400);
    expect(flushed).toHaveLength(0);
  });

  it("flush() on empty pending does nothing", () => {
    const flushed: SubscribeEvent[][] = [];
    const d = createDebouncer((batch) => flushed.push(batch), 400);
    d.flush();
    expect(flushed).toHaveLength(0);
  });

  it("DEBOUNCE_MS constant is 400", () => {
    expect(DEBOUNCE_MS).toBe(400);
  });
});
