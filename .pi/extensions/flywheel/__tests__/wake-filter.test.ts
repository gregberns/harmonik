import { describe, it, expect } from "vitest";
import { classifyEvent, isUrgent, type SubscribeEvent } from "../wake-filter.js";

function evt(type: string, payload: Record<string, unknown> = {}): SubscribeEvent {
  return { event_id: "00000000-0000-7000-8000-000000000001", type, payload };
}

describe("classifyEvent — CL-061 three-tier table", () => {
  it("Tier 1 Ignore: state_entered, state_exited, node_dispatch_requested", () => {
    expect(classifyEvent(evt("state_entered"))).toBe("ignore");
    expect(classifyEvent(evt("state_exited"))).toBe("ignore");
    expect(classifyEvent(evt("node_dispatch_requested"))).toBe("ignore");
  });

  it("Tier 2 Deterministic: run_completed, heartbeat, run_started", () => {
    expect(classifyEvent(evt("run_completed"))).toBe("deterministic");
    expect(classifyEvent(evt("heartbeat"))).toBe("deterministic");
    expect(classifyEvent(evt("run_started"))).toBe("deterministic");
  });

  it("Tier 2 Deterministic: reviewer_verdict APPROVE", () => {
    expect(classifyEvent(evt("reviewer_verdict", { verdict: "APPROVE" }))).toBe("deterministic");
  });

  it("Tier 3 Wake-LLM: reviewer_verdict REQUEST_CHANGES / BLOCK", () => {
    expect(classifyEvent(evt("reviewer_verdict", { verdict: "REQUEST_CHANGES" }))).toBe("wake_llm");
    expect(classifyEvent(evt("reviewer_verdict", { verdict: "BLOCK" }))).toBe("wake_llm");
  });

  it("Tier 3 Wake-LLM: run_failed, merge_conflict, decision_required, bus_overflow", () => {
    expect(classifyEvent(evt("run_failed"))).toBe("wake_llm");
    expect(classifyEvent(evt("merge_conflict"))).toBe("wake_llm");
    expect(classifyEvent(evt("decision_required"))).toBe("wake_llm");
    expect(classifyEvent(evt("bus_overflow"))).toBe("wake_llm");
    expect(classifyEvent(evt("pattern_detected"))).toBe("wake_llm");
  });

  it("unknown event types default to Wake-LLM (fail-towards-judgment)", () => {
    expect(classifyEvent(evt("totally_new_event_type"))).toBe("wake_llm");
    expect(classifyEvent(evt("some_future_event"))).toBe("wake_llm");
  });

  it("subscription_gap is treated as deterministic (handled separately in bridge)", () => {
    expect(classifyEvent(evt("subscription_gap", { dropped: 5 }))).toBe("deterministic");
  });
});

describe("isUrgent — CL-063 urgent class", () => {
  it("merge_conflict is urgent", () => {
    expect(isUrgent(evt("merge_conflict"))).toBe(true);
  });

  it("run_failed is NOT urgent", () => {
    expect(isUrgent(evt("run_failed"))).toBe(false);
  });

  it("reviewer_verdict BLOCK is NOT urgent (judgment, not abort-class)", () => {
    expect(isUrgent(evt("reviewer_verdict", { verdict: "BLOCK" }))).toBe(false);
  });

  it("unknown type is NOT urgent by default", () => {
    expect(isUrgent(evt("some_new_type"))).toBe(false);
  });
});
