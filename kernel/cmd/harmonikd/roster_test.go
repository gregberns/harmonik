package main

import (
	"context"
	"testing"

	kernelv1 "github.com/gregberns/harmonik/contract/gen/harmonik/kernel/v1"
	"github.com/gregberns/harmonik/kernel/roster"
)

// RosterList touches neither the transport nor the journal, so these tests
// construct a kernelServer with both nil and exercise the roster path alone.

func TestRosterListSelfOnly(t *testing.T) {
	k := newKernelServer("box-1", nil, nil)

	resp, err := k.RosterList(context.Background(), &kernelv1.RosterListRequest{})
	if err != nil {
		t.Fatalf("RosterList: %v", err)
	}
	if resp.GetSelf() != "box-1" {
		t.Errorf("Self = %q, want box-1", resp.GetSelf())
	}
	if len(resp.GetNodes()) != 1 {
		t.Fatalf("nodes = %d, want 1 (self only)", len(resp.GetNodes()))
	}
	self := resp.GetNodes()[0]
	if self.GetNode().GetName() != "box-1" {
		t.Errorf("self node name = %q, want box-1", self.GetNode().GetName())
	}
	if self.GetLiveness().GetState() != kernelv1.Liveness_STATE_ALIVE {
		t.Errorf("self state = %v, want ALIVE", self.GetLiveness().GetState())
	}
}

// TestRosterListTwoNode is the acceptance case: a two-node harness shows both
// boxes and self. The linked peer arrives ALIVE (Probed, zero failures); a
// second, never-probed configured peer arrives UNKNOWN.
func TestRosterListTwoNode(t *testing.T) {
	k := newKernelServer("box-1", nil, nil)
	k.setRoster(&rosterView{
		self: &kernelv1.Node{Name: "box-1"},
		peers: []peerObservation{
			{
				node: &kernelv1.Node{Name: "box-2"},
				obs:  roster.Observation{Probed: true, ConsecutiveFailures: 0},
			},
			{
				node: &kernelv1.Node{Name: "box-3"},
				obs:  roster.Observation{Probed: false},
			},
		},
	})

	resp, err := k.RosterList(context.Background(), &kernelv1.RosterListRequest{})
	if err != nil {
		t.Fatalf("RosterList: %v", err)
	}
	if resp.GetSelf() != "box-1" {
		t.Errorf("Self = %q, want box-1", resp.GetSelf())
	}

	states := map[string]kernelv1.Liveness_State{}
	for _, ns := range resp.GetNodes() {
		states[ns.GetNode().GetName()] = ns.GetLiveness().GetState()
	}
	if len(states) != 3 {
		t.Fatalf("nodes = %d, want 3 (self + two peers)", len(states))
	}
	if states["box-1"] != kernelv1.Liveness_STATE_ALIVE {
		t.Errorf("box-1 (self) state = %v, want ALIVE", states["box-1"])
	}
	if states["box-2"] != kernelv1.Liveness_STATE_ALIVE {
		t.Errorf("box-2 (linked peer) state = %v, want ALIVE", states["box-2"])
	}
	if states["box-3"] != kernelv1.Liveness_STATE_UNKNOWN {
		t.Errorf("box-3 (unprobed peer) state = %v, want UNKNOWN", states["box-3"])
	}
}
