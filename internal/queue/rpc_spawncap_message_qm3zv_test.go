package queue_test

import (
	"context"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/queue"
)

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

	params, err := json.Marshal(map[string]any{"n": 8})
	if err != nil {
		t.Fatalf("marshal set-concurrency request: %v", err)
	}
	_, rpcErr := adapter.HandleQueueSetConcurrency(context.Background(), params)
	msg := refusalMessage(t, rpcErr, "a substrate with no live resize accepted a request that oversubscribes its fixed cap")
	assertMessageNames(t, msg, "cap in force in slots", 12)
	assertMessageNames(t, msg, "safe max_concurrent", 6)
}
