// router.ts — prepareNextTurn model stratification (CL-070..CL-073)
//
// Tier 0: routine wake → deterministic skip (no LLM)
// Tier 1: normal → Haiku 4.5
// Tier 2: triage → Sonnet 4.6 (default)
// Tier 3: judgment → Opus 4.7 (one-shot via exception_flag)
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

  // Tier 3: judgment — Opus once (exception_flag clears the one-shot).
  if (
    tier === 3 ||
    digest.exception_flag ||
    wakeEvent.cause === "bead_failed_twice" ||
    wakeEvent.cause === "pattern_detected" ||
    wakeEvent.cause === "escalate_user"
  ) {
    digest.exception_flag = false; // one-shot reset
    return {
      model: "claude-opus-4-7-20260219",
      thinkingLevel: "high",
      cacheNamespace: "judgment",
      tier: 3,
    };
  }

  // Tier 1: normal → Haiku
  if (tier === 1) {
    return {
      model: "claude-haiku-4-5-20251001",
      thinkingLevel: "none",
      tier: 1,
    };
  }

  // Tier 2 (default): triage / reviewer_verdict / fallthrough → Sonnet
  return {
    model: "claude-sonnet-4-6-20251022",
    thinkingLevel: "low",
    tier: 2,
  };
}
