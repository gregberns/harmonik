package core

// reviewgateanomaly_hktnmjy.go — event payload for the review_gate_anomaly
// alarm (§8.14).
//
// Spec ref: specs/event-model.md §8.14 (hk-tnmjy).
// Bead ref: hk-tnmjy.

// ReviewGateAnomalyPayload is the typed event payload for the
// review_gate_anomaly event (event-model.md §8.14).
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=idempotent
// Durability class: O (ordinary — observability alarm; the causal sequence is
// reconstructible from bead_closed + reviewer_verdict events in the JSONL log).
//
// Emitted by the daemon's ReviewGateAnomalyWatcher when ConsecutiveCount
// bead_closed events fire with no intervening reviewer_verdict event. The
// watcher resets its counter after emitting the alarm, so subsequent batches
// re-arm independently.
//
// # Payload fields (event-model.md §8.14)
//
//   - consecutive_count — how many consecutive bead_closed fired without a reviewer_verdict
//   - threshold         — the configured N at which the alarm fires (default 3)
//   - bead_ids          — ordered list of the bead IDs that closed without review (up to consecutive_count)
//   - detected_at       — RFC 3339 wall-clock timestamp at alarm emission
type ReviewGateAnomalyPayload struct {
	// ConsecutiveCount is the number of consecutive bead_closed events observed
	// since the last reviewer_verdict (or since daemon startup if no verdict has
	// ever been observed). Required (must be ≥ 1).
	ConsecutiveCount int `json:"consecutive_count"`

	// Threshold is the configured N: the watcher fires when ConsecutiveCount
	// reaches Threshold. Required (must be ≥ 1).
	Threshold int `json:"threshold"`

	// BeadIDs is the ordered list of bead identifiers that closed without a
	// reviewer_verdict since the last reset. The list contains exactly
	// ConsecutiveCount entries (capped at Threshold to bound payload size).
	// Required (non-nil; length = ConsecutiveCount).
	BeadIDs []string `json:"bead_ids"`

	// DetectedAt is the RFC 3339 wall-clock timestamp at alarm emission.
	// Required (non-empty).
	DetectedAt string `json:"detected_at"`
}

// Valid reports whether p is a well-formed ReviewGateAnomalyPayload.
//
// Rules:
//   - ConsecutiveCount must be ≥ 1.
//   - Threshold must be ≥ 1.
//   - BeadIDs must be non-nil and have length = ConsecutiveCount.
//   - DetectedAt must be non-empty.
func (p ReviewGateAnomalyPayload) Valid() bool {
	if p.ConsecutiveCount < 1 {
		return false
	}
	if p.Threshold < 1 {
		return false
	}
	if p.BeadIDs == nil || len(p.BeadIDs) != p.ConsecutiveCount {
		return false
	}
	if p.DetectedAt == "" {
		return false
	}
	return true
}
