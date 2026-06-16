package sentinel_test

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/gregberns/harmonik/internal/sentinel"
)

// stubCrewStarter is a test double for AdversaryCrewStarter that records calls
// and optionally returns a configured error.
type stubCrewStarter struct {
	calls []json.RawMessage
	err   error
}

func (s *stubCrewStarter) HandleCrewStart(_ context.Context, payload json.RawMessage) (json.RawMessage, error) {
	s.calls = append(s.calls, payload)
	if s.err != nil {
		return nil, s.err
	}
	return json.RawMessage(`{"session_id":"test-session","name":"sentinel-adversary"}`), nil
}

// parseCrewStartPayload unmarshals a HandleCrewStart payload into a map.
func parseCrewStartPayload(t *testing.T, raw json.RawMessage) map[string]string {
	t.Helper()
	var m map[string]string
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("parse crew-start payload: %v", err)
	}
	return m
}

// TestSpawnAdversary_SpawnsWhenCrewOffline verifies that SpawnAdversary calls
// HandleCrewStart with the correct crew name, queue, and mission path when the
// adversary crew is not yet online.
func TestSpawnAdversary_SpawnsWhenCrewOffline(t *testing.T) {
	t.Parallel()
	projectDir := t.TempDir()

	stub := &stubCrewStarter{}
	online := map[string]struct{}{} // adversary not yet online

	spawned, err := sentinel.SpawnAdversary(context.Background(),
		sentinel.AdversaryInput{ProjectDir: projectDir},
		stub,
		online,
	)
	if err != nil {
		t.Fatalf("SpawnAdversary: %v", err)
	}
	if !spawned {
		t.Error("expected spawned=true; got false")
	}
	if len(stub.calls) != 1 {
		t.Fatalf("expected 1 HandleCrewStart call; got %d", len(stub.calls))
	}

	req := parseCrewStartPayload(t, stub.calls[0])
	if req["name"] != sentinel.AdversaryCrewName {
		t.Errorf("name: got %q, want %q", req["name"], sentinel.AdversaryCrewName)
	}
	if req["queue"] != sentinel.AdversaryQueueName {
		t.Errorf("queue: got %q, want %q", req["queue"], sentinel.AdversaryQueueName)
	}

	// mission_path must default to <projectDir>/.flywheel/skills/sentinel-adversary.md
	wantMission := filepath.Join(projectDir, sentinel.DefaultAdversaryMissionRelPath)
	if req["mission_path"] != wantMission {
		t.Errorf("mission_path: got %q, want %q", req["mission_path"], wantMission)
	}
}

// TestSpawnAdversary_SkipsWhenCrewOnline verifies the overlap-skip policy: when
// the adversary crew is already online, SpawnAdversary returns (false, nil) and
// does NOT call HandleCrewStart.
func TestSpawnAdversary_SkipsWhenCrewOnline(t *testing.T) {
	t.Parallel()
	projectDir := t.TempDir()

	stub := &stubCrewStarter{}
	online := map[string]struct{}{
		sentinel.AdversaryCrewName: {}, // adversary already online
	}

	spawned, err := sentinel.SpawnAdversary(context.Background(),
		sentinel.AdversaryInput{ProjectDir: projectDir},
		stub,
		online,
	)
	if err != nil {
		t.Fatalf("SpawnAdversary: %v", err)
	}
	if spawned {
		t.Error("expected spawned=false when crew already online; got true")
	}
	if len(stub.calls) != 0 {
		t.Errorf("expected 0 HandleCrewStart calls on overlap-skip; got %d", len(stub.calls))
	}
}

// TestSpawnAdversary_ReturnsErrorOnCrewStartFailure verifies that a
// HandleCrewStart error propagates as (false, err).
func TestSpawnAdversary_ReturnsErrorOnCrewStartFailure(t *testing.T) {
	t.Parallel()
	projectDir := t.TempDir()

	wantErr := fmt.Errorf("tmux spawn failed")
	stub := &stubCrewStarter{err: wantErr}
	online := map[string]struct{}{} // adversary not online

	spawned, err := sentinel.SpawnAdversary(context.Background(),
		sentinel.AdversaryInput{ProjectDir: projectDir},
		stub,
		online,
	)
	if err == nil {
		t.Fatal("expected non-nil error; got nil")
	}
	if spawned {
		t.Error("expected spawned=false on error; got true")
	}
}

// TestSpawnAdversary_CustomMissionPath verifies that an explicit MissionPath
// overrides the default.
func TestSpawnAdversary_CustomMissionPath(t *testing.T) {
	t.Parallel()
	projectDir := t.TempDir()

	stub := &stubCrewStarter{}
	online := map[string]struct{}{}
	customMission := "/tmp/custom-mission.md"

	_, err := sentinel.SpawnAdversary(context.Background(),
		sentinel.AdversaryInput{
			ProjectDir:  projectDir,
			MissionPath: customMission,
		},
		stub,
		online,
	)
	if err != nil {
		t.Fatalf("SpawnAdversary: %v", err)
	}
	if len(stub.calls) != 1 {
		t.Fatalf("expected 1 call; got %d", len(stub.calls))
	}
	req := parseCrewStartPayload(t, stub.calls[0])
	if req["mission_path"] != customMission {
		t.Errorf("mission_path: got %q, want %q", req["mission_path"], customMission)
	}
}

// TestSpawnAdversary_OtherCrewOnlineDoesNotBlock verifies that OTHER crews being
// online does not block the adversary spawn — only the adversary crew itself triggers
// the skip.
func TestSpawnAdversary_OtherCrewOnlineDoesNotBlock(t *testing.T) {
	t.Parallel()
	projectDir := t.TempDir()

	stub := &stubCrewStarter{}
	online := map[string]struct{}{
		"some-other-crew": {}, // different crew; must not trigger skip
	}

	spawned, err := sentinel.SpawnAdversary(context.Background(),
		sentinel.AdversaryInput{ProjectDir: projectDir},
		stub,
		online,
	)
	if err != nil {
		t.Fatalf("SpawnAdversary: %v", err)
	}
	if !spawned {
		t.Error("expected spawned=true when only different crews are online")
	}
	if len(stub.calls) != 1 {
		t.Errorf("expected 1 HandleCrewStart call; got %d", len(stub.calls))
	}
}
