// wake-filter.ts — three-tier static wake classifier per CL-061.
//
// Tier 1: Ignore    — log+discard; watermark MAY advance; no model wake.
// Tier 2: Deterministic — pure-code handling (refill, heartbeat advance); no model wake.
// Tier 3: Wake-LLM  — requires model judgment.
//
// New / unknown event types default to Wake-LLM (fail-towards-judgment per CL-061).

export type WakeTier = "ignore" | "deterministic" | "wake_llm";

export interface SubscribeEvent {
  event_id: string;
  type: string;
  schema_version?: number;
  run_id?: string;
  payload?: Record<string, unknown>;
  [key: string]: unknown;
}

// Heartbeat event shape from harmonik subscribe (--heartbeat N)
export interface HeartbeatEvent extends SubscribeEvent {
  type: "heartbeat";
  payload: {
    last_event_id: string;
    active_runs: Array<{ bead_id: string; age_seconds: number }>;
  };
}

// Subscription-gap meta-event (emitted by subscribe stream itself on overflow)
export interface SubscriptionGapEvent extends SubscribeEvent {
  type: "subscription_gap";
  payload: { dropped: number };
}

export function isHeartbeatEvent(e: SubscribeEvent): e is HeartbeatEvent {
  return e.type === "heartbeat";
}

export function isSubscriptionGapEvent(e: SubscribeEvent): e is SubscriptionGapEvent {
  return e.type === "subscription_gap";
}

// Static tier table — exhaustive for known types.
// CL-061 small-payload discriminators:
//   run_completed → Deterministic (success is the happy path; non-success would be run_failed)
//   reviewer_verdict → Wake-LLM only when verdict ∈ {REQUEST_CHANGES, BLOCK}; APPROVE → Deterministic
const TIER_TABLE: Record<string, WakeTier | ((e: SubscribeEvent) => WakeTier)> = {
  // Tier 1 — Ignore
  state_entered: "ignore",
  state_exited: "ignore",
  node_dispatch_requested: "ignore",

  // Tier 2 — Deterministic
  run_completed: "deterministic",
  heartbeat: "deterministic",
  run_started: "deterministic",
  queue_submitted: "deterministic",
  queue_group_started: "deterministic",
  queue_group_completed: "deterministic",
  queue_paused: "deterministic",
  queue_appended: "deterministic",
  outcome_emitted: "deterministic",

  // reviewer_verdict: discriminate on verdict field
  reviewer_verdict: (e: SubscribeEvent): WakeTier => {
    const verdict = (e.payload as { verdict?: string } | undefined)?.verdict;
    return verdict === "REQUEST_CHANGES" || verdict === "BLOCK" ? "wake_llm" : "deterministic";
  },

  // Tier 3 — Wake-LLM
  run_failed: "wake_llm",
  merge_conflict: "wake_llm",
  merge_conflict_escalation: "wake_llm",
  decision_required: "wake_llm",
  pattern_detected: "wake_llm",
  bus_overflow: "wake_llm",
  no_progress_detected: "wake_llm",
  iteration_cap_hit: "wake_llm",
};

// Urgent class: MAY abort the in-flight turn (CL-063).
const URGENT_TYPES = new Set(["merge_conflict"]);

export function classifyEvent(e: SubscribeEvent): WakeTier {
  // subscription_gap is handled specially in bridge.ts — treat as deterministic
  // here so the bridge's special case always fires first.
  if (e.type === "subscription_gap") return "deterministic";

  const entry = TIER_TABLE[e.type];
  if (entry === undefined) return "wake_llm"; // unknown → fail-towards-judgment
  if (typeof entry === "function") return entry(e);
  return entry;
}

export function isUrgent(e: SubscribeEvent): boolean {
  return URGENT_TYPES.has(e.type);
}
