package queue_test

// rpc_spawncap_message_qm3zv_test.go — a refused `queue set-concurrency` must
// name the ceiling and the value in force IN THE MESSAGE.
//
// WHY THE MESSAGE AND NOT THE DETAIL. The daemon copies an RPCError onto the
// socket as SocketResponse{Error: Message, ErrorCode: Code} and drops Detail,
// and the CLI prints "error: <Message> (code <n>)". The numbers were computed
// correctly and put in Detail, so they existed everywhere except where the
// operator reads. The whole refusal reached the terminal as:
//
//	error: spawn_cap_exceeded (code -32099)
//
// An operator who hit the cap with one extra digit learned nothing from that
// and had to guess against a live daemon.
//
// Bead ref: hk-qm3zv.

import (
	"context"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/queue"
)

// refusalMessage returns the refusal text, and fails the test when the request
// was accepted or when the message lost its typed reason code.
func refusalMessage(t *testing.T, rpcErr *queue.RPCError, acceptedMsg string) string {
	t.Helper()
	if rpcErr == nil {
		t.Fatal(acceptedMsg)
		return ""
	}
	if !strings.HasPrefix(rpcErr.Message, "spawn_cap_exceeded") {
		t.Fatalf("refusal message is %q, want it to start with the spawn_cap_exceeded reason code", rpcErr.Message)
	}
	return rpcErr.Message
}

// assertMessageNames fails when the refusal message leaves out a number the
// operator needs to pick the next value. The match is on a whole number, so a
// 4 hiding inside a 64 does not count as naming the 4.
func assertMessageNames(t *testing.T, msg, label string, n int) {
	t.Helper()
	whole := regexp.MustCompile(`\b` + strconv.Itoa(n) + `\b`)
	if !whole.MatchString(msg) {
		t.Errorf("the refusal does not name the %s (%d), so the operator cannot tell what to type next.\nmessage: %s", label, n, msg)
	}
}

// TestSpawnCapRefusalMessageNamesTheNumbers_HostBound covers the typo case the
// bead was filed for: a raise past what the host can serve.
func TestSpawnCapRefusalMessageNamesTheNumbers_HostBound(t *testing.T) {
	f := newCapFixture(4, 64) // 4 slots in force, this host serves 64

	_, rpcErr := f.setConcurrency(t, 999999)
	msg := refusalMessage(t, rpcErr, "set-concurrency 999999 was accepted against a host bound of 64 slots")
	assertMessageNames(t, msg, "host bound in slots", 64)
	assertMessageNames(t, msg, "cap in force in slots", 4)
	assertMessageNames(t, msg, "highest max_concurrent this host accepts", 32)
}

// TestSpawnCapRefusalMessageNamesTheNumbers_FixedCapSubstrate covers the older
// substrate that cannot resize at all. It refuses on a different branch, so it
// needs its own check that the numbers reach the operator.
func TestSpawnCapRefusalMessageNamesTheNumbers_FixedCapSubstrate(t *testing.T) {
	adapter := queue.NewHandlerAdapter(nil, "", nil, nil)
	concurrency := 6
	adapter.SetConcurrencyFuncs(
		func() int { return concurrency },
		func(n int) (int, error) { old := concurrency; concurrency = n; return old, nil },
	)
	adapter.SetSpawnCapFunc(func() int { return 12 })
	// No SetSpawnCapSetFunc and no bounds — this substrate cannot resize.

	params, err := json.Marshal(map[string]any{"n": 8})
	if err != nil {
		t.Fatalf("marshal set-concurrency request: %v", err)
	}
	_, rpcErr := adapter.HandleQueueSetConcurrency(context.Background(), params)
	msg := refusalMessage(t, rpcErr, "a substrate with no live resize accepted a request that oversubscribes its fixed cap")
	assertMessageNames(t, msg, "cap in force in slots", 12)
	assertMessageNames(t, msg, "safe max_concurrent", 6)
}
