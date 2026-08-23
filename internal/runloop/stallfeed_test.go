package runloop

import (
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/runexec"
)

func TestStallFeed_APostReachesOnlyTheRunItNames(t *testing.T) {
	f := NewStallFeed()
	a, releaseA := f.Register("run-a")
	defer releaseA()
	b, releaseB := f.Register("run-b")
	defer releaseB()

	if !f.Post("run-a", runexec.Event{Kind: runexec.EvHeartbeatStale}) {
		t.Fatalf("Post to an open feed was refused")
	}

	select {
	case ev := <-a:
		if ev.Kind != runexec.EvHeartbeatStale {
			t.Errorf("run-a received %q, want heartbeat_stale", ev.Kind)
		}
	default:
		t.Errorf("run-a received nothing")
	}
	select {
	case ev := <-b:
		t.Errorf("run-b received %q for a stall posted to run-a", ev.Kind)
	default:
	}
}

func TestStallFeed_PostNeverBlocks(t *testing.T) {
	f := NewStallFeed()
	_, release := f.Register("run-a")
	defer release()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < stallFeedDepth+3; i++ {
			f.Post("run-a", runexec.Event{Kind: runexec.EvNoChangeTimeout})
		}
		f.Post("run-nobody", runexec.Event{Kind: runexec.EvNoChangeTimeout})
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("Post blocked; one wedged run would stop the detector for the whole fleet")
	}
}

func TestStallFeed_APostRacingTheReleaseDoesNotPanic(t *testing.T) {
	f := NewStallFeed()
	var wg sync.WaitGroup
	for i := 0; i < 200; i++ {
		_, release := f.Register("run-a")
		wg.Add(2)
		go func() { defer wg.Done(); f.Post("run-a", runexec.Event{Kind: runexec.EvNoChangeTimeout}) }()
		go func() { defer wg.Done(); release() }()
	}
	wg.Wait()
}

func TestStallFeed_AReleasedFeedEndsItsReader(t *testing.T) {
	f := NewStallFeed()
	ch, release := f.Register("run-a")
	release()

	select {
	case _, ok := <-ch:
		if ok {
			t.Errorf("a released feed delivered an event")
		}
	case <-time.After(time.Second):
		t.Errorf("a released feed left its reader parked for ever, so the run's stall watch never ends")
	}

	release()
}
