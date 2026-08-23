package tunnel

import (
	"strings"
	"testing"
)

// TestReverseTunnel_CNP17_MultiplexingOptOut asserts BuildArgs forces
// the per-run tunnel off the worker's shared ControlMaster and keeps the dedicated
// link warm, WITHOUT losing the existing reverse forward / fail-fast semantics.
func TestReverseTunnel_CNP17_MultiplexingOptOut(t *testing.T) {
	t.Parallel()

	const (
		port  = 40404
		dsock = "/proj/.harmonik/daemon.sock"
		host  = "worker-mac-2"
	)
	got := BuildArgs(port, dsock, host, []string{"-p", "2222"})
	joined := strings.Join(got, " ")

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

	if !strings.Contains(joined, "-R 127.0.0.1:40404:"+dsock) {
		t.Errorf("argv missing reverse forward -R 127.0.0.1:40404:%s:\n%s", dsock, joined)
	}
	if !strings.Contains(joined, "-o ExitOnForwardFailure=yes") {
		t.Errorf("argv missing ExitOnForwardFailure=yes:\n%s", joined)
	}

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
	const n = 25
	got := make([]int, 0, n)
	seen := make(map[int]bool, n)
	for i := 0; i < n; i++ {
		p, err := AllocatePort()
		if err != nil {
			t.Fatalf("AllocatePort #%d: %v", i, err)
		}
		if seen[p] {
			t.Fatalf("duplicate port %d on sequential allocation #%d (reserved-set not holding)", p, i)
		}
		seen[p] = true
		got = append(got, p)
	}
	for _, p := range got {
		ReleasePort(p)
	}

	for i := 0; i < n; i++ {
		p, err := AllocatePort()
		if err != nil {
			t.Fatalf("post-release AllocatePort #%d: %v", i, err)
		}
		ReleasePort(p)
	}
}
