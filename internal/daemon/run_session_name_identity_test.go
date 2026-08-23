package daemon

import (
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
)

func nameIdentityConcurrentRunIDs(t *testing.T, goroutines, per int) []string {
	t.Helper()

	var mu sync.Mutex
	ids := make([]string, 0, goroutines*per)

	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < per; i++ {
				runUUID, err := uuid.NewV7()
				if err != nil {
					continue
				}
				mu.Lock()
				ids = append(ids, runUUID.String())
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if len(ids) == 0 {
		t.Fatal("nameIdentity: minted no run ids")
	}
	return ids
}

// Two runs dispatched in the same millisecond MUST get two session names.
//
// This is the test that fails when the name carries only the UUIDv7 timestamp.
// At a 12-char prefix a 2000-id burst collapsed to 3 names; at 16 every id kept
// its own. It asserts the property rather than the width, so it still binds if
// the derivation is later replaced by a hash.
func TestRunSessionName_TwoRunsInOneMillisecondGetTwoSessions(t *testing.T) {
	t.Parallel()

	sub := w4cFixtureSubstrate(t, &sessionCollideAdapter{})
	ids := nameIdentityConcurrentRunIDs(t, 8, 250)

	stamps := make(map[string]bool, len(ids))
	for _, id := range ids {
		stamps[strings.ReplaceAll(id, "-", "")[:12]] = true
	}
	if len(stamps) >= len(ids) {
		t.Skipf("nameIdentity: host spread %d ids over %d milliseconds, so no two"+
			" ids share one; this test needs a burst to mean anything", len(ids), len(stamps))
	}

	owner := make(map[string]string, len(ids))
	for _, id := range ids {
		name, err := sub.runSessionName(id)
		if err != nil {
			t.Fatalf("nameIdentity: name the session for %s: %v", id, err)
		}
		if prev, taken := owner[name]; taken {
			t.Fatalf("two runs share one session name: %s and %s both map to %q."+
				" The registry record and the collision branch both read that name as"+
				" one run, so one run's record and one run's agent are attributed to"+
				" the other (%d ids crowded into %d milliseconds)",
				prev, id, name, len(ids), len(stamps))
		}
		owner[name] = id
	}
}

// A run id still names its own session deterministically. Uniqueness is worth
// nothing if the name is not stable, because the registry writes it once at
// launch and every reader recomputes it later.
func TestRunSessionName_TheSameRunAlwaysGetsTheSameSession(t *testing.T) {
	t.Parallel()

	sub := w4cFixtureSubstrate(t, &sessionCollideAdapter{})

	first, err := sub.runSessionName(sessionCollideRunID)
	if err != nil {
		t.Fatalf("nameIdentity: first naming: %v", err)
	}
	second, err := sub.runSessionName(sessionCollideRunID)
	if err != nil {
		t.Fatalf("nameIdentity: second naming: %v", err)
	}
	if first != second {
		t.Fatalf("one run got two session names: %q then %q", first, second)
	}
}
