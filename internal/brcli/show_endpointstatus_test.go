package brcli_test

import (
	"context"
	"testing"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
)

// TestShowBeadEndpointStatusInDependencyEdges verifies that the inline "status"
// field on dependency/dependent edges is parsed into EndpointStatus on each
// DependencyEdge per the C1 spec (c1-spec.md §3 D3).
//
// AC-6: closed child -> CoarseStatusClosed
// AC-7: additive/non-breaking; Valid() unchanged
func TestShowBeadEndpointStatusInDependencyEdges(t *testing.T) {
	t.Run("dependency_edge_closed_status", func(t *testing.T) {
		// Edge with status="closed" must populate EndpointStatus = CoarseStatusClosed.
		jsonStr := `[{"id":"hk-c1","title":"T","description":"","status":"open","issue_type":"task","dependencies":[{"id":"hk-child","dependency_type":"parent-child","status":"closed"}],"dependents":[],"parent":""}]`
		path := brcliFixtureMockBinary(t, jsonStr, "", 0)

		adapter, err := brcli.New(path)
		if err != nil {
			t.Fatalf("New: %v", err)
		}

		record, err := adapter.ShowBead(context.Background(), core.BeadID("hk-c1"))
		if err != nil {
			t.Fatalf("ShowBead: unexpected error: %v", err)
		}

		if len(record.Edges) != 1 {
			t.Fatalf("len(Edges) = %d; want 1", len(record.Edges))
		}
		if record.Edges[0].EndpointStatus != core.CoarseStatusClosed {
			t.Errorf("EndpointStatus = %q; want %q (AC-6)", record.Edges[0].EndpointStatus, core.CoarseStatusClosed)
		}
		// AC-7: Valid() must still pass for well-formed edge.
		if !record.Edges[0].Valid() {
			t.Error("edge.Valid() = false after adding EndpointStatus; want true (AC-7)")
		}
	})

	t.Run("dependent_edge_open_status", func(t *testing.T) {
		// Edge in dependents[] with status="open" must populate EndpointStatus = CoarseStatusOpen.
		jsonStr := `[{"id":"hk-c1","title":"T","description":"","status":"open","issue_type":"task","dependencies":[],"dependents":[{"id":"hk-dep","dependency_type":"blocks","status":"open"}],"parent":""}]`
		path := brcliFixtureMockBinary(t, jsonStr, "", 0)

		adapter, err := brcli.New(path)
		if err != nil {
			t.Fatalf("New: %v", err)
		}

		record, err := adapter.ShowBead(context.Background(), core.BeadID("hk-c1"))
		if err != nil {
			t.Fatalf("ShowBead: unexpected error: %v", err)
		}

		if len(record.Edges) != 1 {
			t.Fatalf("len(Edges) = %d; want 1", len(record.Edges))
		}
		if record.Edges[0].EndpointStatus != core.CoarseStatusOpen {
			t.Errorf("EndpointStatus = %q; want %q", record.Edges[0].EndpointStatus, core.CoarseStatusOpen)
		}
	})

	t.Run("empty_status_yields_zero_not_closed", func(t *testing.T) {
		// Edge with absent/empty status field must leave EndpointStatus at its zero
		// value (empty string), which is NOT CoarseStatusClosed.
		jsonStr := `[{"id":"hk-c1","title":"T","description":"","status":"open","issue_type":"task","dependencies":[{"id":"hk-child","dependency_type":"waits-for"}],"dependents":[],"parent":""}]`
		path := brcliFixtureMockBinary(t, jsonStr, "", 0)

		adapter, err := brcli.New(path)
		if err != nil {
			t.Fatalf("New: %v", err)
		}

		record, err := adapter.ShowBead(context.Background(), core.BeadID("hk-c1"))
		if err != nil {
			t.Fatalf("ShowBead: unexpected error: %v", err)
		}

		if len(record.Edges) != 1 {
			t.Fatalf("len(Edges) = %d; want 1", len(record.Edges))
		}
		got := record.Edges[0].EndpointStatus
		if got == core.CoarseStatusClosed {
			t.Errorf("EndpointStatus = %q; want zero (empty) when status absent — must NOT be closed", got)
		}
		if got != "" {
			t.Errorf("EndpointStatus = %q; want empty string (zero) when status field absent", got)
		}
	})

	t.Run("bad_status_returns_error", func(t *testing.T) {
		// Edge with an unrecognized status value must cause ShowBead to return an error.
		jsonStr := `[{"id":"hk-c1","title":"T","description":"","status":"open","issue_type":"task","dependencies":[{"id":"hk-child","dependency_type":"waits-for","status":"BADVALUE"}],"dependents":[],"parent":""}]`
		path := brcliFixtureMockBinary(t, jsonStr, "", 0)

		adapter, err := brcli.New(path)
		if err != nil {
			t.Fatalf("New: %v", err)
		}

		_, err = adapter.ShowBead(context.Background(), core.BeadID("hk-c1"))
		if err == nil {
			t.Fatal("expected error for bad inline edge status, got nil")
		}
	})

	t.Run("bad_dependent_status_returns_error", func(t *testing.T) {
		// Same bad-status check for the dependents[] loop.
		jsonStr := `[{"id":"hk-c1","title":"T","description":"","status":"open","issue_type":"task","dependencies":[],"dependents":[{"id":"hk-dep","dependency_type":"blocks","status":"BADVALUE"}],"parent":""}]`
		path := brcliFixtureMockBinary(t, jsonStr, "", 0)

		adapter, err := brcli.New(path)
		if err != nil {
			t.Fatalf("New: %v", err)
		}

		_, err = adapter.ShowBead(context.Background(), core.BeadID("hk-c1"))
		if err == nil {
			t.Fatal("expected error for bad inline dependent edge status, got nil")
		}
	})

	t.Run("all_status_values_round_trip", func(t *testing.T) {
		cases := []struct {
			name   string
			status string
			want   core.CoarseStatus
		}{
			{"open", "open", core.CoarseStatusOpen},
			{"in_progress", "in_progress", core.CoarseStatusInProgress},
			{"blocked", "blocked", core.CoarseStatusBlocked},
			{"deferred", "deferred", core.CoarseStatusDeferred},
			{"draft", "draft", core.CoarseStatusDraft},
			{"closed", "closed", core.CoarseStatusClosed},
			{"tombstone", "tombstone", core.CoarseStatusTombstone},
			{"pinned", "pinned", core.CoarseStatusPinned},
		}

		for _, tc := range cases {
			tc := tc
			t.Run(tc.name, func(t *testing.T) {
				jsonStr := `[{"id":"hk-c1","title":"T","description":"","status":"open","issue_type":"task","dependencies":[{"id":"hk-child","dependency_type":"waits-for","status":"` + tc.status + `"}],"dependents":[],"parent":""}]`
				path := brcliFixtureMockBinary(t, jsonStr, "", 0)

				adapter, err := brcli.New(path)
				if err != nil {
					t.Fatalf("New: %v", err)
				}

				record, err := adapter.ShowBead(context.Background(), core.BeadID("hk-c1"))
				if err != nil {
					t.Fatalf("ShowBead: unexpected error: %v", err)
				}

				if len(record.Edges) != 1 {
					t.Fatalf("len(Edges) = %d; want 1", len(record.Edges))
				}
				if record.Edges[0].EndpointStatus != tc.want {
					t.Errorf("EndpointStatus = %q; want %q", record.Edges[0].EndpointStatus, tc.want)
				}
			})
		}
	})

	t.Run("existing_fixture_endpoint_statuses", func(t *testing.T) {
		// Verify the existing showBeadFixtureValidJSON fixture (which already has
		// status fields on edges) now populates EndpointStatus correctly.
		id := core.BeadID("hk-872.15")
		jsonStr := showBeadFixtureValidJSON(string(id))
		path := brcliFixtureMockBinary(t, jsonStr, "", 0)

		adapter, err := brcli.New(path)
		if err != nil {
			t.Fatalf("New: %v", err)
		}

		record, err := adapter.ShowBead(context.Background(), id)
		if err != nil {
			t.Fatalf("ShowBead: unexpected error: %v", err)
		}

		// Fixture: hk-872 dep has status="open", hk-872.45 dep has status="closed",
		// hk-872.22 dependent has status="open".
		statusByTarget := make(map[core.BeadID]core.CoarseStatus)
		for _, e := range record.Edges {
			if e.FromBeadID == id {
				statusByTarget[e.ToBeadID] = e.EndpointStatus
			} else {
				statusByTarget[e.FromBeadID] = e.EndpointStatus
			}
		}

		if got := statusByTarget[core.BeadID("hk-872")]; got != core.CoarseStatusOpen {
			t.Errorf("hk-872 EndpointStatus = %q; want %q", got, core.CoarseStatusOpen)
		}
		if got := statusByTarget[core.BeadID("hk-872.45")]; got != core.CoarseStatusClosed {
			t.Errorf("hk-872.45 EndpointStatus = %q; want %q (AC-6)", got, core.CoarseStatusClosed)
		}
		if got := statusByTarget[core.BeadID("hk-872.22")]; got != core.CoarseStatusOpen {
			t.Errorf("hk-872.22 EndpointStatus = %q; want %q", got, core.CoarseStatusOpen)
		}
	})
}
