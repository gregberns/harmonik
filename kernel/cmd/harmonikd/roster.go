package main

import (
	kernelv1 "github.com/gregberns/harmonik/contract/gen/harmonik/kernel/v1"
	"github.com/gregberns/harmonik/kernel/roster"
)

// peerObservation pairs one configured peer's Node identity with this box's
// current observation of it. The composition root builds the slice. In this
// slice a linked in-process peer arrives ALIVE (Probed, zero failures) and any
// other configured peer arrives UNKNOWN (not probed yet) — the roster package
// turns each observation into a liveness verdict; no probe loop exists here.
type peerObservation struct {
	node *kernelv1.Node
	obs  roster.Observation
}

// rosterView is the static roster this box serves over RosterList: itself plus
// the configured peer set, each peer with its last observation. It holds no
// probe loop and no clock — a later slice feeds fresh observations; this carries
// whatever the composition root set.
type rosterView struct {
	self  *kernelv1.Node
	peers []peerObservation
}

// list renders the view into a RosterListResponse, computing each peer's
// liveness with the pure roster functions. Self is always ALIVE: this box is
// the one answering. last_seen stays unset — there is no probe loop in this
// slice to stamp an instant, and the roster functions never invent one.
func (rv *rosterView) list() *kernelv1.RosterListResponse {
	resp := &kernelv1.RosterListResponse{Self: rv.self.GetName()}
	resp.Nodes = append(resp.Nodes, &kernelv1.NodeStatus{
		Node:     rv.self,
		Liveness: &kernelv1.Liveness{State: kernelv1.Liveness_STATE_ALIVE},
	})
	for _, p := range rv.peers {
		v := roster.Evaluate(p.obs)
		resp.Nodes = append(resp.Nodes, &kernelv1.NodeStatus{
			Node:     p.node,
			Liveness: &kernelv1.Liveness{State: v.State, Reason: v.Reason},
		})
	}
	return resp
}
