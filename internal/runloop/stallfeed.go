package runloop

import (
	"sync"

	"github.com/gregberns/harmonik/internal/runexec"
)

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
