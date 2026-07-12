package daemon

// crewstart_hkp006e_test.go — regression test for hk-p006e.
//
// Bug: the .managed marker was created in step 6a (after SpawnCrewSession), so
// the in-session keeper window saw an absent marker at startup and exited as a
// no-op. The keeper process calls keeper.IsManaged immediately on boot; if the
// marker is absent it prints "not opted-in (.managed marker missing); no-op"
// and exits, leaving ops-monitor to fire keeper-missing on every check cycle.
//
// Fix: the marker is now created in step 3.5 (before SpawnCrewSession) so the
// keeper process always finds it present when its startup check fires.
//
// This test verifies the ordering by inspecting whether the .managed file
// exists at the moment SpawnCrewSession is invoked.

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/gregberns/harmonik/internal/handler"
)

// managedCheckSubstrate implements handler.Substrate + crewSessionSpawner and
// records whether the .managed marker existed at the moment SpawnCrewSession
// was invoked. The projectDir field must be set before the first call.
type managedCheckSubstrate struct {
	projectDir    string
	markerExisted bool
	spawnCalled   bool
}

func (s *managedCheckSubstrate) SpawnWindow(_ context.Context, _ handler.SubstrateSpawn) (handler.SubstrateSession, error) {
	return &fakeSession{handle: "fallback"}, nil
}

func (s *managedCheckSubstrate) SpawnCrewSession(_ context.Context, crewName string, _ handler.SubstrateSpawn) (handler.SubstrateSession, error) {
	s.spawnCalled = true
	markerPath := filepath.Join(s.projectDir, ".harmonik", "keeper", crewName+".managed")
	_, err := os.Stat(markerPath)
	s.markerExisted = (err == nil)
	return &fakeSession{handle: "hk-crew-" + crewName + ":agent"}, nil
}

// TestCrewStart_ManagedMarkerBeforeSpawn_hkp006e verifies that the .managed
// marker exists at the moment SpawnCrewSession is called. This is the
// regression guard for hk-p006e: if the marker is created after the spawn the
// keeper watcher sees an absent marker on boot and exits as a no-op, causing
// ops-monitor to fire keeper-missing on every check cycle.
func TestCrewStart_ManagedMarkerBeforeSpawn_hkp006e(t *testing.T) {
	sub := &managedCheckSubstrate{}
	h, dir := newTestCrewHandler(t, sub, nil)
	sub.projectDir = dir

	mustCrewStart(t, h, CrewStartRequest{
		Name:  "jessica",
		Queue: "crew-jessica",
	})

	if !sub.spawnCalled {
		t.Fatal("SpawnCrewSession was not called — test infrastructure problem")
	}
	if !sub.markerExisted {
		t.Error("hk-p006e: .managed marker was absent when SpawnCrewSession was called; " +
			"the in-session keeper watcher would exit as a no-op (keeper.IsManaged returned false). " +
			"The marker must be created before the session is spawned.")
	}
}
