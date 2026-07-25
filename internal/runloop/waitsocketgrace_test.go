package runloop

import (
	"encoding/json"
	"testing"
)

func TestParseOutcomePayload(t *testing.T) {
	t.Parallel()

	outcome := parseOutcomePayload(json.RawMessage(`{"kind":"WORK_COMPLETE"}`))
	if outcome == nil || outcome.Kind != "WORK_COMPLETE" {
		t.Fatalf("parseOutcomePayload() = %#v, want WORK_COMPLETE", outcome)
	}
}

func TestParseOutcomePayloadRejectsInvalidJSON(t *testing.T) {
	t.Parallel()

	if outcome := parseOutcomePayload(json.RawMessage(`{`)); outcome != nil {
		t.Fatalf("parseOutcomePayload(invalid) = %#v, want nil", outcome)
	}
}
