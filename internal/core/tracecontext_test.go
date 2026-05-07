package core

import (
	"testing"

	"github.com/google/uuid"
)

// validTraceContext returns a fully-populated TraceContext with all optional
// fields set to non-zero values, for use as a baseline in rejection tests.
func validTraceContext(t *testing.T) TraceContext {
	t.Helper()

	traceID := "trace-abc123"
	parentID := uuid.Must(uuid.NewV7())
	rootID := uuid.Must(uuid.NewV7())

	return TraceContext{
		TraceID:       &traceID,
		ParentEventID: &parentID,
		RootEventID:   &rootID,
	}
}

func TestTraceContextValid_AllNil(t *testing.T) {
	t.Parallel()

	tc := TraceContext{}
	if !tc.Valid() {
		t.Error("Valid() = false for zero-value TraceContext (all nil), want true")
	}
}

func TestTraceContextValid_AllFieldsSet(t *testing.T) {
	t.Parallel()

	tc := validTraceContext(t)
	if !tc.Valid() {
		t.Error("Valid() = false for fully-populated TraceContext, want true")
	}
}

func TestTraceContextValid_OnlyTraceID(t *testing.T) {
	t.Parallel()

	traceID := "external-correlation-id"
	tc := TraceContext{
		TraceID: &traceID,
	}
	if !tc.Valid() {
		t.Error("Valid() = false when only TraceID is set, want true")
	}
}

func TestTraceContextValid_EmptyTraceID(t *testing.T) {
	t.Parallel()

	tc := validTraceContext(t)
	empty := ""
	tc.TraceID = &empty
	if tc.Valid() {
		t.Error("Valid() = true when TraceID pointer is non-nil but value is empty string, want false")
	}
}

func TestTraceContextValid_NilParentEventID(t *testing.T) {
	t.Parallel()

	tc := validTraceContext(t)
	nilUUID := uuid.Nil
	tc.ParentEventID = &nilUUID
	if tc.Valid() {
		t.Error("Valid() = true when ParentEventID pointer is non-nil but value is uuid.Nil, want false")
	}
}

func TestTraceContextValid_NilRootEventID(t *testing.T) {
	t.Parallel()

	tc := validTraceContext(t)
	nilUUID := uuid.Nil
	tc.RootEventID = &nilUUID
	if tc.Valid() {
		t.Error("Valid() = true when RootEventID pointer is non-nil but value is uuid.Nil, want false")
	}
}
