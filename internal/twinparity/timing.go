package twinparity

import (
	"testing"
	"time"
)

// TimingEdge names a causal pair (From → To) whose elapsed latency is compared
// between twin and real streams.
type TimingEdge struct {
	From, To string
}

// AssertTimingWithinTolerance asserts that, for each edge, the twin's From→To
// latency and the real stream's From→To latency differ by no more than tol. An
// edge whose endpoints are absent in either stream fails. Latencies are derived
// from the unexported per-event elapsed offsets (envelope timestamps relative
// to each stream's first record).
func AssertTimingWithinTolerance(t testing.TB, twin, realStream Stream, edges []TimingEdge, tol time.Duration) {
	t.Helper()
	for _, edge := range edges {
		twinDelta, twinOK := edgeDelta(twin, edge)
		realDelta, realOK := edgeDelta(realStream, edge)
		if !twinOK {
			t.Errorf("twinparity timing: edge %s→%s: endpoints absent in TWIN stream", edge.From, edge.To)
			continue
		}
		if !realOK {
			t.Errorf("twinparity timing: edge %s→%s: endpoints absent in REAL stream", edge.From, edge.To)
			continue
		}
		diff := twinDelta - realDelta
		if diff < 0 {
			diff = -diff
		}
		if diff > tol {
			t.Errorf("twinparity timing: edge %s→%s: |Δreal(%s) − Δtwin(%s)| = %s > tol %s",
				edge.From, edge.To, realDelta, twinDelta, diff, tol)
		}
	}
}

// edgeDelta returns the elapsed-time difference between the first occurrence of
// edge.To and the first occurrence of edge.From in the stream. ok=false when
// either endpoint is missing.
func edgeDelta(s Stream, edge TimingEdge) (time.Duration, bool) {
	from, fromOK := firstElapsed(s, edge.From)
	to, toOK := firstElapsed(s, edge.To)
	if !fromOK || !toOK {
		return 0, false
	}
	return to - from, true
}

// firstElapsed returns the elapsed offset of the first event of the given kind.
func firstElapsed(s Stream, kind string) (time.Duration, bool) {
	for _, ev := range s.Events {
		if ev.Kind == kind {
			return ev.elapsed, true
		}
	}
	return 0, false
}
