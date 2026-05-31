// router.ts — prepareNextTurn model stratification (CL-070..CL-073)
//
// Tier 0: routine wake → deterministic skip (no LLM)
// Tier 1: normal → Haiku 4.5  (override: FLYWHEEL_MODEL_TIER1)
// Tier 2: triage → Sonnet 4.6 (override: FLYWHEEL_MODEL_TIER2)
// Tier 3: judgment → Sonnet 4.6 default; Opus only if FLYWHEEL_MODEL_TIER3 is set
//
// Routing signal: wakeEvent.cause + digest.exception_flag
// exception_flag is one-shot: prevents runaway Opus chains.

export type WakeCause =
  | "heartbeat"
  | "queue_empty"
  | "run_completed"
  | "run_failed"
  | "bead_failed_twice"
  | "pattern_detected"
  | "escalate_user"
  | "reviewer_verdict"
  | "merge_conflict"
  | "decision_required"
  | string;

export interface WakeEvent {
  cause: WakeCause;
  /** Tier 0–3; if absent the router derives it from cause */
  tier?: 0 | 1 | 2 | 3;
}

export interface Digest {
  /** One-shot flag: set by harness to force a single Opus turn. Cleared after routing. */
  exception_flag?: boolean;
}

export interface RoutingConfig {
  skip?: boolean;
  model?: string;
  thinkingLevel?: "none" | "low" | "high";
  cacheNamespace?: string;
  tier?: 0 | 1 | 2 | 3;
}

// Default model IDs per tier. Operator may override via env vars.
const DEFAULT_TIER1_MODEL = "claude-haiku-4-5-20251001";
const DEFAULT_TIER2_MODEL = "claude-sonnet-4-6-20251022";
// Tier 3 defaults to Sonnet; set FLYWHEEL_MODEL_TIER3 to opt into Opus.
const DEFAULT_TIER3_MODEL = "claude-sonnet-4-6-20251022";

function tierModel(tier: 1 | 2 | 3): string {
  switch (tier) {
    case 1:
      return process.env.FLYWHEEL_MODEL_TIER1 ?? DEFAULT_TIER1_MODEL;
    case 2:
      return process.env.FLYWHEEL_MODEL_TIER2 ?? DEFAULT_TIER2_MODEL;
    case 3:
      return process.env.FLYWHEEL_MODEL_TIER3 ?? DEFAULT_TIER3_MODEL;
  }
}

// Causes that always map to Tier 3 (judgment).
const JUDGMENT_CAUSES = new Set<WakeCause>([
  "bead_failed_twice",
  "pattern_detected",
  "escalate_user",
  "merge_conflict",
  "decision_required",
]);

function deriveTier(cause: WakeCause): 0 | 1 | 2 | 3 {
  if (cause === "heartbeat") return 0;
  if (JUDGMENT_CAUSES.has(cause)) return 3;
  if (cause === "reviewer_verdict") return 2;
  return 1; // queue_empty, run_completed, run_failed, unknown
}

/**
 * Determine the per-turn model config.
 * Mutates digest.exception_flag (clears it after a Tier-3 upgrade).
 */
export function prepareNextTurn(
  digest: Digest,
  wakeEvent: WakeEvent
): RoutingConfig {
  const tier = wakeEvent.tier ?? deriveTier(wakeEvent.cause);

  // Tier 0: deterministic path, no LLM needed.
  if (tier === 0) {
    return { skip: true, tier: 0 };
  }

  // Tier 3: judgment — one-shot (exception_flag cleared after use).
  if (
    tier === 3 ||
    digest.exception_flag ||
    wakeEvent.cause === "bead_failed_twice" ||
    wakeEvent.cause === "pattern_detected" ||
    wakeEvent.cause === "escalate_user"
  ) {
    digest.exception_flag = false; // one-shot reset
    return {
      model: tierModel(3),
      thinkingLevel: "high",
      cacheNamespace: "judgment",
      tier: 3,
    };
  }

  // Tier 1: normal → Haiku (or FLYWHEEL_MODEL_TIER1 override)
  if (tier === 1) {
    return {
      model: tierModel(1),
      thinkingLevel: "none",
      tier: 1,
    };
  }

  // Tier 2 (default): triage / reviewer_verdict / fallthrough → Sonnet (or FLYWHEEL_MODEL_TIER2 override)
  return {
    model: tierModel(2),
    thinkingLevel: "low",
    tier: 2,
  };
}
