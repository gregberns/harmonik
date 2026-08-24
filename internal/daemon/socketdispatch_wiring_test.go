package daemon

import (
	"reflect"
	"sort"
	"testing"
)

var frozenRoutableOps = []string{
	"claim-next",
	"comms-presence",
	"comms-recv",
	"comms-send",
	"confirm_verdict",
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
	"queue-drop",
	"queue-dry-run",
	"queue-list",
	"queue-recover",
	"queue-set-concurrency",
	"queue-status",
	"queue-submit",
	"session-start-ack",
	"state",
	"veto_verdict",
	"worker-set-enabled",
}

func TestBuildSocketRouter_FrozenOpSet(t *testing.T) {
	got := buildSocketRouter(&socketDispatch{}).Ops()

	want := append([]string(nil), frozenRoutableOps...)
	sort.Strings(want)

	if len(got) != 30 {
		t.Fatalf("router registered %d ops, want 30: %v", len(got), got)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("router op set drift:\n got: %v\nwant: %v", got, want)
	}
}

// TestSocketSurface_TwoPreBranches statically pins the daemon pre-branch surface:
// the 30 routable ops + the two pre-branches (subscribe, hook-relay) == the full
// 31-op protocol surface + the hook-relay envelope. If subscribe ever appears in
// the router's Ops(), or the routable count changes, this fails.
func TestSocketSurface_TwoPreBranches(t *testing.T) {
	const daemonPreBranchOps = 1 // "subscribe" (hook-relay is keyed on the "type" envelope, not an op)
	const totalProtocolOps = 31  // the frozen op surface of handleSocketConn's switch
	routable := len(buildSocketRouter(&socketDispatch{}).Ops())

	if routable+daemonPreBranchOps != totalProtocolOps {
		t.Fatalf("surface accounting off: %d routable + %d pre-branch op != %d total",
			routable, daemonPreBranchOps, totalProtocolOps)
	}

	for _, op := range buildSocketRouter(&socketDispatch{}).Ops() {
		if op == "subscribe" {
			t.Fatal("subscribe must be a daemon pre-branch, not a router route")
		}
	}
}
