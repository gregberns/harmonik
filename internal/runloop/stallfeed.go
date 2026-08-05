package runloop

// stallfeed.go — the delivery seam between the daemon's stall detector and the
// per-run dispatch machine.
//
// internal/runexec stepDispatchWorking turns EvNoChangeTimeout and
// EvHeartbeatStale into ActKillAgent. The vocabulary comment says the frozen
// commit watchdog is SHELL-FED rather than reactor-timed, and this is the feed
// it names: the detector runs once for the whole daemon, on its own goroutine,
// and needs to reach ONE run's machine.
//
// The feed is a registry of per-run channels rather than a callback, because
// the machine is single-goroutine-owned. A callback would step it from the
// detector's goroutine while the run's own shell may still be stepping it. A
// channel hands the event to the run's watch goroutine, which is the only
// writer that machine ever has.
//
// Post never blocks. A run whose watch has already ended, or whose buffer is
// full because it has been told to die once already, must not hold up the scan
// that serves every other run.
//
// Bead: hk-hsp9e.

import (
	"sync"

	"github.com/gregberns/harmonik/internal/runexec"
)

// stallFeedDepth is the per-run buffer. Two is the number of stall signatures
// that can fire for one run in a single detector pass (a run can be both silent
// and over its age ceiling), so a full buffer means the run has already been
// told everything a kill needs.
const stallFeedDepth = 2

// StallFeed routes stall events from the daemon's detector to the dispatch
// machine of the run they name. Safe for concurrent use.
type StallFeed struct {
	mu    sync.Mutex
	feeds map[string]chan runexec.Event
}

// NewStallFeed returns an empty feed.
func NewStallFeed() *StallFeed {
	return &StallFeed{feeds: make(map[string]chan runexec.Event)}
}

// Register opens the feed for runID and returns the channel the run's dispatch
// watch reads, plus the release the launch MUST call when the run is done.
//
// Releasing closes the channel, which ends the watch. A second Register for the
// same run replaces the first and releases it, so a re-registration cannot
// strand a reader on a channel nothing posts to.
func (f *StallFeed) Register(runID string) (stalls <-chan runexec.Event, release func()) {
	ch := make(chan runexec.Event, stallFeedDepth)

	f.mu.Lock()
	if prev, ok := f.feeds[runID]; ok {
		close(prev)
	}
	f.feeds[runID] = ch
	f.mu.Unlock()

	var once sync.Once
	return ch, func() {
		once.Do(func() {
			f.mu.Lock()
			defer f.mu.Unlock()
			if cur, ok := f.feeds[runID]; ok && cur == ch {
				delete(f.feeds, runID)
				close(ch)
			}
		})
	}
}

// Post hands ev to runID's dispatch watch. It reports whether the event was
// accepted: false means the run has no open feed, or its buffer is already
// full. Neither is an error — both mean the run has nothing further to learn
// from this event — but the caller may want to say so in a diagnostic.
//
// Post never blocks. The send happens UNDER the lock: the channel is closed
// under the same lock by the release, and a send that read the map and then let
// go would race that close and panic on a run that finished mid-scan.
func (f *StallFeed) Post(runID string, ev runexec.Event) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	ch, ok := f.feeds[runID]
	if !ok {
		return false
	}
	select {
	case ch <- ev:
		return true
	default:
		return false
	}
}
