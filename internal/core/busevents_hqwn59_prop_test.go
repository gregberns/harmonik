package core

// busevents_hqwn59_prop_test.go — property tests for the Valid() methods
// declared in busevents_hqwn59.go.
//
// Naming: TestProp_* per testing.md §Decisions #10.
// Approach: rapid generator builds a valid payload, flips exactly one required
// field to its zero/invalid value, asserts Valid()==false; all-valid -> true.
//
// Refs: hk-qgzso (property-test coverage uplift for hk-j3hrn core uplift).

import (
	"testing"

	"github.com/google/uuid"
	"pgregory.net/rapid"
)

// ============================================================
// MetricPayload
// ============================================================

func TestProp_MetricPayload_AllValidAccepted(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := MetricPayload{
			Name:  MetricName(drawNonEmptyString(rt, "name")),
			Value: rapid.Float64().Draw(rt, "value"),
		}
		if !p.Valid() {
			rt.Error("Valid() == false for well-formed MetricPayload")
		}
	})
}

func TestProp_MetricPayload_EmptyNameRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := MetricPayload{
			Name:  "",
			Value: rapid.Float64().Draw(rt, "value"),
		}
		if p.Valid() {
			rt.Error("Valid() should be false with empty Name")
		}
	})
}

// ============================================================
// ConsumerFailedPayload
// ============================================================

func TestProp_ConsumerFailedPayload_AllValidAccepted(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := ConsumerFailedPayload{
			ConsumerName:  drawNonEmptyString(rt, "consumer_name"),
			EventType:     EventTypeRunStarted,
			EventID:       EventID(drawNonNilUUID(rt, "event_id")),
			ErrorCategory: rapid.SampledFrom(allErrorCategories).Draw(rt, "error_category"),
			FailedAt:      drawNonEmptyString(rt, "failed_at"),
		}
		if !p.Valid() {
			rt.Error("Valid() == false for well-formed ConsumerFailedPayload")
		}
	})
}

func TestProp_ConsumerFailedPayload_EmptyConsumerNameRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := ConsumerFailedPayload{
			ConsumerName:  "",
			EventType:     EventTypeRunStarted,
			EventID:       EventID(drawNonNilUUID(rt, "event_id")),
			ErrorCategory: rapid.SampledFrom(allErrorCategories).Draw(rt, "error_category"),
			FailedAt:      drawNonEmptyString(rt, "failed_at"),
		}
		if p.Valid() {
			rt.Error("Valid() should be false with empty ConsumerName")
		}
	})
}

func TestProp_ConsumerFailedPayload_EmptyEventTypeRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := ConsumerFailedPayload{
			ConsumerName:  drawNonEmptyString(rt, "consumer_name"),
			EventType:     "",
			EventID:       EventID(drawNonNilUUID(rt, "event_id")),
			ErrorCategory: rapid.SampledFrom(allErrorCategories).Draw(rt, "error_category"),
			FailedAt:      drawNonEmptyString(rt, "failed_at"),
		}
		if p.Valid() {
			rt.Error("Valid() should be false with empty EventType")
		}
	})
}

func TestProp_ConsumerFailedPayload_NilEventIDRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := ConsumerFailedPayload{
			ConsumerName:  drawNonEmptyString(rt, "consumer_name"),
			EventType:     EventTypeRunStarted,
			EventID:       EventID(uuid.Nil),
			ErrorCategory: rapid.SampledFrom(allErrorCategories).Draw(rt, "error_category"),
			FailedAt:      drawNonEmptyString(rt, "failed_at"),
		}
		if p.Valid() {
			rt.Error("Valid() should be false with nil EventID")
		}
	})
}

func TestProp_ConsumerFailedPayload_InvalidErrorCategoryRejected(t *testing.T) {
	known := make(map[string]bool)
	for _, v := range allErrorCategories {
		known[string(v)] = true
	}
	rapid.Check(t, func(rt *rapid.T) {
		s := rapid.StringN(1, 32, -1).Draw(rt, "err_cat_str")
		if known[s] {
			rt.Skip("known constant")
		}
		p := ConsumerFailedPayload{
			ConsumerName:  drawNonEmptyString(rt, "consumer_name"),
			EventType:     EventTypeRunStarted,
			EventID:       EventID(drawNonNilUUID(rt, "event_id")),
			ErrorCategory: ErrorCategory(s),
			FailedAt:      drawNonEmptyString(rt, "failed_at"),
		}
		if p.Valid() {
			rt.Errorf("Valid() should be false for unknown error category %q", s)
		}
	})
}

func TestProp_ConsumerFailedPayload_EmptyFailedAtRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := ConsumerFailedPayload{
			ConsumerName:  drawNonEmptyString(rt, "consumer_name"),
			EventType:     EventTypeRunStarted,
			EventID:       EventID(drawNonNilUUID(rt, "event_id")),
			ErrorCategory: rapid.SampledFrom(allErrorCategories).Draw(rt, "error_category"),
			FailedAt:      "",
		}
		if p.Valid() {
			rt.Error("Valid() should be false with empty FailedAt")
		}
	})
}

// ============================================================
// DeadLetterEnqueuedPayload
// ============================================================

func TestProp_DeadLetterEnqueuedPayload_AllValidAccepted(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := DeadLetterEnqueuedPayload{
			ConsumerName:     drawNonEmptyString(rt, "consumer_name"),
			EventType:        EventTypeRunStarted,
			OriginalEventID:  EventID(drawNonNilUUID(rt, "original_event_id")),
			RetriesAttempted: rapid.IntRange(0, 10).Draw(rt, "retries"),
			EnqueuedAt:       drawNonEmptyString(rt, "enqueued_at"),
		}
		if !p.Valid() {
			rt.Error("Valid() == false for well-formed DeadLetterEnqueuedPayload")
		}
	})
}

func TestProp_DeadLetterEnqueuedPayload_EmptyConsumerNameRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := DeadLetterEnqueuedPayload{
			ConsumerName:     "",
			EventType:        EventTypeRunStarted,
			OriginalEventID:  EventID(drawNonNilUUID(rt, "original_event_id")),
			RetriesAttempted: 0,
			EnqueuedAt:       drawNonEmptyString(rt, "enqueued_at"),
		}
		if p.Valid() {
			rt.Error("Valid() should be false with empty ConsumerName")
		}
	})
}

func TestProp_DeadLetterEnqueuedPayload_EmptyEventTypeRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := DeadLetterEnqueuedPayload{
			ConsumerName:     drawNonEmptyString(rt, "consumer_name"),
			EventType:        "",
			OriginalEventID:  EventID(drawNonNilUUID(rt, "original_event_id")),
			RetriesAttempted: 0,
			EnqueuedAt:       drawNonEmptyString(rt, "enqueued_at"),
		}
		if p.Valid() {
			rt.Error("Valid() should be false with empty EventType")
		}
	})
}

func TestProp_DeadLetterEnqueuedPayload_NilOriginalEventIDRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := DeadLetterEnqueuedPayload{
			ConsumerName:     drawNonEmptyString(rt, "consumer_name"),
			EventType:        EventTypeRunStarted,
			OriginalEventID:  EventID(uuid.Nil),
			RetriesAttempted: 0,
			EnqueuedAt:       drawNonEmptyString(rt, "enqueued_at"),
		}
		if p.Valid() {
			rt.Error("Valid() should be false with nil OriginalEventID")
		}
	})
}

func TestProp_DeadLetterEnqueuedPayload_NegativeRetriesAttemptedRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := DeadLetterEnqueuedPayload{
			ConsumerName:     drawNonEmptyString(rt, "consumer_name"),
			EventType:        EventTypeRunStarted,
			OriginalEventID:  EventID(drawNonNilUUID(rt, "original_event_id")),
			RetriesAttempted: rapid.IntRange(-100, -1).Draw(rt, "retries"),
			EnqueuedAt:       drawNonEmptyString(rt, "enqueued_at"),
		}
		if p.Valid() {
			rt.Errorf("Valid() should be false for RetriesAttempted=%d (must be >= 0)", p.RetriesAttempted)
		}
	})
}

func TestProp_DeadLetterEnqueuedPayload_EmptyEnqueuedAtRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := DeadLetterEnqueuedPayload{
			ConsumerName:     drawNonEmptyString(rt, "consumer_name"),
			EventType:        EventTypeRunStarted,
			OriginalEventID:  EventID(drawNonNilUUID(rt, "original_event_id")),
			RetriesAttempted: 0,
			EnqueuedAt:       "",
		}
		if p.Valid() {
			rt.Error("Valid() should be false with empty EnqueuedAt")
		}
	})
}

// ============================================================
// BusOverflowPayload
// ============================================================

func TestProp_BusOverflowPayload_AllValidAccepted(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := BusOverflowPayload{
			ConsumerName: drawNonEmptyString(rt, "consumer_name"),
			EventType:    EventTypeRunStarted,
			EventID:      EventID(drawNonNilUUID(rt, "event_id")),
			QueueDepth:   rapid.IntRange(0, 10000).Draw(rt, "queue_depth"),
			ShedAt:       drawNonEmptyString(rt, "shed_at"),
			ShedPolicy:   rapid.SampledFrom(allShedPolicies).Draw(rt, "shed_policy"),
		}
		if !p.Valid() {
			rt.Error("Valid() == false for well-formed BusOverflowPayload")
		}
	})
}

func TestProp_BusOverflowPayload_EmptyConsumerNameRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := BusOverflowPayload{
			ConsumerName: "",
			EventType:    EventTypeRunStarted,
			EventID:      EventID(drawNonNilUUID(rt, "event_id")),
			QueueDepth:   0,
			ShedAt:       drawNonEmptyString(rt, "shed_at"),
			ShedPolicy:   ShedPolicyFsyncSpilled,
		}
		if p.Valid() {
			rt.Error("Valid() should be false with empty ConsumerName")
		}
	})
}

func TestProp_BusOverflowPayload_EmptyEventTypeRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := BusOverflowPayload{
			ConsumerName: drawNonEmptyString(rt, "consumer_name"),
			EventType:    "",
			EventID:      EventID(drawNonNilUUID(rt, "event_id")),
			QueueDepth:   0,
			ShedAt:       drawNonEmptyString(rt, "shed_at"),
			ShedPolicy:   ShedPolicyFsyncSpilled,
		}
		if p.Valid() {
			rt.Error("Valid() should be false with empty EventType")
		}
	})
}

func TestProp_BusOverflowPayload_NilEventIDRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := BusOverflowPayload{
			ConsumerName: drawNonEmptyString(rt, "consumer_name"),
			EventType:    EventTypeRunStarted,
			EventID:      EventID(uuid.Nil),
			QueueDepth:   0,
			ShedAt:       drawNonEmptyString(rt, "shed_at"),
			ShedPolicy:   ShedPolicyFsyncSpilled,
		}
		if p.Valid() {
			rt.Error("Valid() should be false with nil EventID")
		}
	})
}

func TestProp_BusOverflowPayload_NegativeQueueDepthRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := BusOverflowPayload{
			ConsumerName: drawNonEmptyString(rt, "consumer_name"),
			EventType:    EventTypeRunStarted,
			EventID:      EventID(drawNonNilUUID(rt, "event_id")),
			QueueDepth:   rapid.IntRange(-10000, -1).Draw(rt, "queue_depth"),
			ShedAt:       drawNonEmptyString(rt, "shed_at"),
			ShedPolicy:   ShedPolicyFsyncSpilled,
		}
		if p.Valid() {
			rt.Errorf("Valid() should be false for QueueDepth=%d (must be >= 0)", p.QueueDepth)
		}
	})
}

func TestProp_BusOverflowPayload_EmptyShedAtRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := BusOverflowPayload{
			ConsumerName: drawNonEmptyString(rt, "consumer_name"),
			EventType:    EventTypeRunStarted,
			EventID:      EventID(drawNonNilUUID(rt, "event_id")),
			QueueDepth:   0,
			ShedAt:       "",
			ShedPolicy:   ShedPolicyFsyncSpilled,
		}
		if p.Valid() {
			rt.Error("Valid() should be false with empty ShedAt")
		}
	})
}

func TestProp_BusOverflowPayload_InvalidShedPolicyRejected(t *testing.T) {
	known := make(map[string]bool)
	for _, v := range allShedPolicies {
		known[string(v)] = true
	}
	rapid.Check(t, func(rt *rapid.T) {
		s := rapid.StringN(1, 32, -1).Draw(rt, "policy_str")
		if known[s] {
			rt.Skip("known constant")
		}
		p := BusOverflowPayload{
			ConsumerName: drawNonEmptyString(rt, "consumer_name"),
			EventType:    EventTypeRunStarted,
			EventID:      EventID(drawNonNilUUID(rt, "event_id")),
			QueueDepth:   0,
			ShedAt:       drawNonEmptyString(rt, "shed_at"),
			ShedPolicy:   ShedPolicy(s),
		}
		if p.Valid() {
			rt.Errorf("Valid() should be false for unknown shed policy %q", s)
		}
	})
}

// ============================================================
// RedactionFailedPayload
// ============================================================

func TestProp_RedactionFailedPayload_AllValidAccepted(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := RedactionFailedPayload{
			EventType:  EventTypeRunStarted,
			ErrorClass: rapid.SampledFrom(allErrorCategories).Draw(rt, "error_class"),
			FailedAt:   drawNonEmptyString(rt, "failed_at"),
		}
		if !p.Valid() {
			rt.Error("Valid() == false for well-formed RedactionFailedPayload")
		}
	})
}

func TestProp_RedactionFailedPayload_EmptyEventTypeRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := RedactionFailedPayload{
			EventType:  "",
			ErrorClass: rapid.SampledFrom(allErrorCategories).Draw(rt, "error_class"),
			FailedAt:   drawNonEmptyString(rt, "failed_at"),
		}
		if p.Valid() {
			rt.Error("Valid() should be false with empty EventType")
		}
	})
}

func TestProp_RedactionFailedPayload_NilRunIDRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		nilID := RunID(uuid.Nil)
		p := RedactionFailedPayload{
			EventType:  EventTypeRunStarted,
			RunID:      &nilID,
			ErrorClass: rapid.SampledFrom(allErrorCategories).Draw(rt, "error_class"),
			FailedAt:   drawNonEmptyString(rt, "failed_at"),
		}
		if p.Valid() {
			rt.Error("Valid() should be false when RunID points to uuid.Nil")
		}
	})
}

func TestProp_RedactionFailedPayload_InvalidErrorClassRejected(t *testing.T) {
	known := make(map[string]bool)
	for _, v := range allErrorCategories {
		known[string(v)] = true
	}
	rapid.Check(t, func(rt *rapid.T) {
		s := rapid.StringN(1, 32, -1).Draw(rt, "err_class_str")
		if known[s] {
			rt.Skip("known constant")
		}
		p := RedactionFailedPayload{
			EventType:  EventTypeRunStarted,
			ErrorClass: ErrorCategory(s),
			FailedAt:   drawNonEmptyString(rt, "failed_at"),
		}
		if p.Valid() {
			rt.Errorf("Valid() should be false for unknown error class %q", s)
		}
	})
}

func TestProp_RedactionFailedPayload_EmptyFailedAtRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := RedactionFailedPayload{
			EventType:  EventTypeRunStarted,
			ErrorClass: rapid.SampledFrom(allErrorCategories).Draw(rt, "error_class"),
			FailedAt:   "",
		}
		if p.Valid() {
			rt.Error("Valid() should be false with empty FailedAt")
		}
	})
}
