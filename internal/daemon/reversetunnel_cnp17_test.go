package daemon

// reversetunnel_cnp17_test.go — regression tests for hk-cnp17: at max_slots>1 the
// per-run reverse tunnel collapsed onto the worker's shared SSH ControlMaster, so
// the agent_ready forward was not durably established and every concurrent run died
// at agent_ready_timeout. The fix pins each tunnel onto its own dedicated,
// non-multiplexed, kept-warm ssh connection, and de-conflicts the worker-side hint
// port across concurrent runs.

import (
	"strings"
	"testing"
)

// TestReverseTunnel_CNP17_MultiplexingOptOut asserts buildReverseTunnelArgs forces
// the per-run tunnel off the worker's shared ControlMaster and keeps the dedicated
// link warm, WITHOUT losing the existing reverse forward / fail-fast semantics.
func TestReverseTunnel_CNP17_MultiplexingOptOut(t *testing.T) {
	t.Parallel()

	const (
		port  = 40404
		dsock = "/proj/.harmonik/daemon.sock"
		host  = "worker-mac-2"
	)
	// Include worker opts to prove the forced flags survive alongside them.
	got := buildReverseTunnelArgs(port, dsock, host, []string{"-p", "2222"})
	joined := strings.Join(got, " ")

	// The hk-cnp17 multiplexing opt-outs + keepalives.
	for _, want := range []string{
		"-o ControlMaster=no",
		"-o ControlPath=none",
		"-o ServerAliveInterval=15",
		"-o ServerAliveCountMax=4",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("argv missing %q (hk-cnp17):\n%s", want, joined)
		}
	}

	// Pre-existing semantics must be preserved.
	if !strings.Contains(joined, "-R 127.0.0.1:40404:"+dsock) {
		t.Errorf("argv missing reverse forward -R 127.0.0.1:40404:%s:\n%s", dsock, joined)
	}
	if !strings.Contains(joined, "-o ExitOnForwardFailure=yes") {
		t.Errorf("argv missing ExitOnForwardFailure=yes:\n%s", joined)
	}

	// The forced opt-outs must come BEFORE the worker opts (ssh: first value wins),
	// so a worker ControlMaster/ControlPath in opts cannot re-enable multiplexing.
	cmIdx := strings.Index(joined, "ControlMaster=no")
	optIdx := strings.Index(joined, "-p 2222")
	if cmIdx < 0 || optIdx < 0 || cmIdx > optIdx {
		t.Errorf("forced multiplexing opt-outs must precede worker opts:\n%s", joined)
	}
}

// TestReverseTunnel_CNP17_SequentialAllocDistinct asserts the reserved-port set
// hands out DISTINCT ports even for rapid SEQUENTIAL allocations. Without the set,
// a tight Listen(:0)/Close loop frequently re-receives the just-freed port from the
// kernel, so two runs whose alloc/close interleave could collide. Releasing returns
// the port to the pool.
func TestReverseTunnel_CNP17_SequentialAllocDistinct(t *testing.T) {
	// Not parallel: exercises the package-global reservedTunnelPorts set.
	const n = 25
	got := make([]int, 0, n)
	seen := make(map[int]bool, n)
	for i := 0; i < n; i++ {
		p, err := allocateReverseTunnelPort()
		if err != nil {
			t.Fatalf("allocateReverseTunnelPort #%d: %v", i, err)
		}
		if seen[p] {
			t.Fatalf("duplicate port %d on sequential allocation #%d (reserved-set not holding)", p, i)
		}
		seen[p] = true
		got = append(got, p)
	}
	// Release all reservations so the test leaves no global state behind.
	for _, p := range got {
		releaseReverseTunnelPort(p)
	}

	// After release, a fresh allocation may legitimately reuse a freed port — prove
	// release actually frees by confirming we can re-allocate up to n more without
	// running out (the set is no longer holding the originals).
	for i := 0; i < n; i++ {
		p, err := allocateReverseTunnelPort()
		if err != nil {
			t.Fatalf("post-release allocateReverseTunnelPort #%d: %v", i, err)
		}
		releaseReverseTunnelPort(p)
	}
}
