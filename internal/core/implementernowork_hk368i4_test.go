package core

import (
	"encoding/json"
	"testing"
)

// implementernowork_hk368i4_test.go — ImplementerNoWorkSuspectedPayload (hk-368i4).

func TestImplementerNoWorkSuspectedPayload_Valid_hk368i4(t *testing.T) {
	t.Parallel()

	base := ImplementerNoWorkSuspectedPayload{
		RunID:           "01hwxyz-run",
		BeadID:          "hk-368i4",
		DurationSeconds: 4.0,
		FloorSeconds:    10.0,
	}

	if !base.Valid() {
		t.Errorf("well-formed payload reported invalid: %+v", base)
	}

	cases := []struct {
		name  string
		mutue func(*ImplementerNoWorkSuspectedPayload)
	}{
		{"missing run_id", func(p *ImplementerNoWorkSuspectedPayload) { p.RunID = "" }},
		{"missing bead_id", func(p *ImplementerNoWorkSuspectedPayload) { p.BeadID = "" }},
		{"zero floor", func(p *ImplementerNoWorkSuspectedPayload) { p.FloorSeconds = 0 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := base
			tc.mutue(&p)
			if p.Valid() {
				t.Errorf("payload with %s reported valid: %+v", tc.name, p)
			}
		})
	}

	// A zero duration stays VALID: the detector never fires on an unmeasured
	// duration, but a payload carrying one is still well-formed and must not be
	// dropped on replay.
	zeroDur := base
	zeroDur.DurationSeconds = 0
	if !zeroDur.Valid() {
		t.Errorf("payload with zero duration reported invalid: %+v", zeroDur)
	}
}

func TestImplementerNoWorkSuspectedPayload_JSONRoundTrip_hk368i4(t *testing.T) {
	t.Parallel()

	want := ImplementerNoWorkSuspectedPayload{
		RunID:           "01hwxyz-run",
		BeadID:          "hk-368i4",
		DurationSeconds: 3.27,
		FloorSeconds:    10,
	}
	b, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got ImplementerNoWorkSuspectedPayload
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got != want {
		t.Errorf("round-trip mismatch: got %+v, want %+v", got, want)
	}
}
