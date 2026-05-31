import { describe, it, expect, beforeEach, afterEach } from "vitest";
import { prepareNextTurn, type Digest, type WakeEvent } from "../router.js";

function wake(cause: string, tier?: 0 | 1 | 2 | 3): WakeEvent {
  return tier !== undefined ? { cause, tier } : { cause };
}

describe("prepareNextTurn — tier routing", () => {
  it("Tier 0: heartbeat → skip", () => {
    const cfg = prepareNextTurn({}, wake("heartbeat"));
    expect(cfg.skip).toBe(true);
    expect(cfg.tier).toBe(0);
  });

  it("Tier 1: run_completed → Haiku", () => {
    const cfg = prepareNextTurn({}, wake("run_completed"));
    expect(cfg.tier).toBe(1);
    expect(cfg.model).toContain("haiku");
    expect(cfg.thinkingLevel).toBe("none");
  });

  it("Tier 2: reviewer_verdict → Sonnet", () => {
    const cfg = prepareNextTurn({}, wake("reviewer_verdict"));
    expect(cfg.tier).toBe(2);
    expect(cfg.model).toContain("sonnet");
    expect(cfg.thinkingLevel).toBe("low");
  });

  it("Tier 3: bead_failed_twice → Sonnet by default (not Opus)", () => {
    const cfg = prepareNextTurn({}, wake("bead_failed_twice"));
    expect(cfg.tier).toBe(3);
    expect(cfg.model).toContain("sonnet");
    expect(cfg.thinkingLevel).toBe("high");
    expect(cfg.cacheNamespace).toBe("judgment");
  });

  it("Tier 3: exception_flag → Sonnet by default, flag cleared", () => {
    const digest: Digest = { exception_flag: true };
    const cfg = prepareNextTurn(digest, wake("run_completed"));
    expect(cfg.tier).toBe(3);
    expect(cfg.model).toContain("sonnet");
    expect(digest.exception_flag).toBe(false);
  });

  it("Tier 3: explicit tier=3 override → judgment path", () => {
    const cfg = prepareNextTurn({}, wake("queue_empty", 3));
    expect(cfg.tier).toBe(3);
    expect(cfg.cacheNamespace).toBe("judgment");
  });
});

describe("prepareNextTurn — FLYWHEEL_MODEL_TIER env overrides", () => {
  const saved: Record<string, string | undefined> = {};

  beforeEach(() => {
    saved.TIER1 = process.env.FLYWHEEL_MODEL_TIER1;
    saved.TIER2 = process.env.FLYWHEEL_MODEL_TIER2;
    saved.TIER3 = process.env.FLYWHEEL_MODEL_TIER3;
  });

  afterEach(() => {
    if (saved.TIER1 === undefined) delete process.env.FLYWHEEL_MODEL_TIER1;
    else process.env.FLYWHEEL_MODEL_TIER1 = saved.TIER1;
    if (saved.TIER2 === undefined) delete process.env.FLYWHEEL_MODEL_TIER2;
    else process.env.FLYWHEEL_MODEL_TIER2 = saved.TIER2;
    if (saved.TIER3 === undefined) delete process.env.FLYWHEEL_MODEL_TIER3;
    else process.env.FLYWHEEL_MODEL_TIER3 = saved.TIER3;
  });

  it("FLYWHEEL_MODEL_TIER1 overrides Haiku", () => {
    process.env.FLYWHEEL_MODEL_TIER1 = "custom-model-tier1";
    const cfg = prepareNextTurn({}, wake("run_completed"));
    expect(cfg.model).toBe("custom-model-tier1");
  });

  it("FLYWHEEL_MODEL_TIER2 overrides Sonnet", () => {
    process.env.FLYWHEEL_MODEL_TIER2 = "custom-model-tier2";
    const cfg = prepareNextTurn({}, wake("reviewer_verdict"));
    expect(cfg.model).toBe("custom-model-tier2");
  });

  it("FLYWHEEL_MODEL_TIER3 opts into Opus (or any model)", () => {
    process.env.FLYWHEEL_MODEL_TIER3 = "claude-opus-4-7-20260219";
    const cfg = prepareNextTurn({}, wake("bead_failed_twice"));
    expect(cfg.tier).toBe(3);
    expect(cfg.model).toBe("claude-opus-4-7-20260219");
  });

  it("no FLYWHEEL_MODEL_TIER3 → Sonnet, not Opus", () => {
    delete process.env.FLYWHEEL_MODEL_TIER3;
    const cfg = prepareNextTurn({}, wake("decision_required"));
    expect(cfg.tier).toBe(3);
    expect(cfg.model).not.toContain("opus");
  });
});
