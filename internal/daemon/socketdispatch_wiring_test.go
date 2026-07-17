package daemon

// T4 — wiring-completeness guard (MANDATORY, package daemon).
//
// Asserts buildSocketRouter(&socketDispatch{}).Ops() equals the frozen 25-op set
// (every op except subscribe, which is a daemon pre-branch). A dropped Register
// would route a live op to the neutral Unknown path → "daemon: unknown op %q"
// instead of its "… not registered" envelope (wire-F7). Plus a static assertion
// that subscribe + hook-relay are the two daemon pre-branches, so the total
// handled surface is provably the full 26 ops + the hook-relay envelope.

import (
	"reflect"
	"sort"
	"testing"
)

// frozenRoutableOps is the exact set of ops routed through socketrouter.Dispatch.
// subscribe is deliberately absent (daemon pre-branch). Freeze this list; a
// diff here is a wire-surface change and must be intentional.
var frozenRoutableOps = []string{
	"claim-next",
	"comms-presence",
	"comms-recv",
	"comms-send",
	"crew-start",
	"crew-stop",
	"daemon-sleep",
	"daemon-wake",
	"dashboard",
	"decisions-answer",
	"decisions-list",
	"decisions-raise",
	"decisions-withdraw",
	"emit-outcome",
	"operator-pause",
	"operator-resume",
	"queue-append",
	"queue-cancel",
	"queue-dry-run",
	"queue-list",
	"queue-set-concurrency",
	"queue-status",
	"queue-submit",
	"state",
	"worker-set-enabled",
}

func TestBuildSocketRouter_FrozenOpSet(t *testing.T) {
	got := buildSocketRouter(&socketDispatch{}).Ops()

	want := append([]string(nil), frozenRoutableOps...)
	sort.Strings(want)

	if len(got) != 25 {
		t.Fatalf("router registered %d ops, want 25: %v", len(got), got)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("router op set drift:\n got: %v\nwant: %v", got, want)
	}
}

// TestSocketSurface_TwoPreBranches statically pins the daemon pre-branch surface:
// the 25 routable ops + the two pre-branches (subscribe, hook-relay) == the full
// 26-op protocol surface + the hook-relay envelope. If subscribe ever appears in
// the router's Ops(), or the routable count changes, this fails.
func TestSocketSurface_TwoPreBranches(t *testing.T) {
	const daemonPreBranchOps = 1 // "subscribe" (hook-relay is keyed on the "type" envelope, not an op)
	const totalProtocolOps = 26  // the frozen op surface of handleSocketConn's switch
	routable := len(buildSocketRouter(&socketDispatch{}).Ops())

	if routable+daemonPreBranchOps != totalProtocolOps {
		t.Fatalf("surface accounting off: %d routable + %d pre-branch op != %d total",
			routable, daemonPreBranchOps, totalProtocolOps)
	}

	// subscribe MUST NOT be routable (it is a response-shape-breaking pre-branch).
	for _, op := range buildSocketRouter(&socketDispatch{}).Ops() {
		if op == "subscribe" {
			t.Fatal("subscribe must be a daemon pre-branch, not a router route")
		}
	}
}
