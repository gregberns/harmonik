//go:build scenario

package sentinel_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/sentinel"
)

type bt6RecordingStarter struct {
	calls []json.RawMessage
}

func (s *bt6RecordingStarter) HandleCrewStart(_ context.Context, payload json.RawMessage) (json.RawMessage, error) {
	s.calls = append(s.calls, payload)
	return json.RawMessage(`{"session_id":"bt6-adversary-session","name":"sentinel-adversary"}`), nil
}

func bt6AdversaryProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	missionPath := filepath.Join(dir, sentinel.DefaultAdversaryMissionRelPath)
	if err := os.MkdirAll(filepath.Dir(missionPath), 0o755); err != nil {
		t.Fatalf("bt6AdversaryProject: mkdir: %v", err)
	}
	mission := strings.Join([]string{
		"# sentinel-adversary — independent governor-trip adjudicator",
		"",
		"> You are a FRESH-CONTEXT adversary. You are NOT the captain.",
		"> Read the evidence as a foreign auditor who has never seen this project before.",
		"",
		"Review the captain's recent comms and commits as FOREIGN ARTIFACTS:",
		"    harmonik comms log --from captain --since 60m",
		"    git log origin/main --since=60m --oneline",
		"",
		"Your ONLY power is to write ONE decision_required exception:",
		"    harmonik sentinel emit-trip --project \"$(pwd)\" --bead \"<id1>,<id2>\"",
		"Exit immediately. Do NOT act as the captain or dispatch beads yourself.",
		"",
	}, "\n")
	if err := os.WriteFile(missionPath, []byte(mission), 0o644); err != nil {
		t.Fatalf("bt6AdversaryProject: write mission: %v", err)
	}
	return dir
}

func bt6ParsePayload(t *testing.T, raw json.RawMessage) (name, queue, missionPath string) {
	t.Helper()
	var m map[string]string
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("bt6ParsePayload: %v", err)
	}
	return m["name"], m["queue"], m["mission_path"]
}

// TestScenario_BT6_AdversaryFreshContext_NoCaptainTranscript asserts the §2.3
// fresh-context property: the spawned adversary carries NO captain transcript.
// Its launch context is exactly (a) the foreign-auditor mission file and (b) a
// queue SEPARATE from the captain/main queue — no captain-conversation channel
// is threaded into the spawn. The mission file on disk is the real foreign-auditor
// contract (reviews comms/commits as foreign artifacts).
func TestScenario_BT6_AdversaryFreshContext_NoCaptainTranscript(t *testing.T) {
	t.Parallel()

	projectDir := bt6AdversaryProject(t)
	starter := &bt6RecordingStarter{}
	online := map[string]struct{}{} // adversary not online → a spawn will occur

	spawned, err := sentinel.SpawnAdversary(context.Background(),
		sentinel.AdversaryInput{ProjectDir: projectDir},
		starter,
		online,
	)
	if err != nil {
		t.Fatalf("SpawnAdversary: %v", err)
	}
	if !spawned {
		t.Fatal("expected the adversary to spawn (none online); got spawned=false")
	}
	if len(starter.calls) != 1 {
		t.Fatalf("expected exactly 1 spawn; got %d", len(starter.calls))
	}

	name, queue, missionPath := bt6ParsePayload(t, starter.calls[0])

	if queue != sentinel.AdversaryQueueName {
		t.Errorf("adversary queue = %q, want the separate %q queue (fresh-context isolation)",
			queue, sentinel.AdversaryQueueName)
	}
	if queue == "main" || queue == "captain" {
		t.Errorf("adversary bound to the captain/main queue %q — its context is NOT fresh", queue)
	}
	if name != sentinel.AdversaryCrewName {
		t.Errorf("adversary crew name = %q, want %q", name, sentinel.AdversaryCrewName)
	}

	wantMission := filepath.Join(projectDir, sentinel.DefaultAdversaryMissionRelPath)
	if missionPath != wantMission {
		t.Fatalf("mission_path = %q, want %q", missionPath, wantMission)
	}
	body, readErr := os.ReadFile(missionPath)
	if readErr != nil {
		t.Fatalf("read adversary mission: %v", readErr)
	}
	text := string(body)

	for _, marker := range []string{"FRESH-CONTEXT", "NOT the captain", "foreign"} {
		if !strings.Contains(text, marker) {
			t.Errorf("adversary mission missing fresh-context marker %q; the spawned session "+
				"is not provably foreign-context", marker)
		}
	}
	if !strings.Contains(text, "comms log --from captain") && !strings.Contains(text, "git log") {
		t.Error("adversary mission does not review captain comms/commits as foreign artifacts")
	}
	var payloadKeys map[string]json.RawMessage
	if err := json.Unmarshal(starter.calls[0], &payloadKeys); err != nil {
		t.Fatalf("unmarshal payload keys: %v", err)
	}
	for k := range payloadKeys {
		switch k {
		case "name", "queue", "mission_path":
		default:
			t.Errorf("spawn payload carries unexpected channel %q — fresh-context spawn "+
				"must not thread captain context", k)
		}
	}
}

// TestScenario_BT6_AdversaryExceptionBound_AtMostOnePerOnlineWindow asserts the
// §2.1 exception bound: the adversary's only power is to write ONE exception, and
// the governor structurally cannot stack adjudicators. We drive SEVERAL
// consecutive governor trips (as the workloop would on consecutive ACTIVE ticks)
// against the REAL SpawnAdversary while the adversary is online after the first
// spawn; the overlap-skip must hold the total spawned adversary count to AT MOST
// ONE across the window — hence at most one exception-write session.
func TestScenario_BT6_AdversaryExceptionBound_AtMostOnePerOnlineWindow(t *testing.T) {
	t.Parallel()

	projectDir := bt6AdversaryProject(t)
	starter := &bt6RecordingStarter{}

	online := map[string]struct{}{}

	const trips = 5
	spawnedCount := 0
	for i := 0; i < trips; i++ {
		spawned, err := sentinel.SpawnAdversary(context.Background(),
			sentinel.AdversaryInput{ProjectDir: projectDir},
			starter,
			online,
		)
		if err != nil {
			t.Fatalf("SpawnAdversary tick %d: %v", i, err)
		}
		if spawned {
			spawnedCount++
			online[sentinel.AdversaryCrewName] = struct{}{}
		}
	}

	if spawnedCount > 1 {
		t.Errorf("adversary spawned %d times across %d consecutive governor trips; want <= 1 "+
			"(overlap-skip must bound exception-write sessions per online window)", spawnedCount, trips)
	}
	if len(starter.calls) != spawnedCount {
		t.Errorf("HandleCrewStart called %d times but spawnedCount=%d; overlap-skip must NOT "+
			"invoke the starter on a skipped tick", len(starter.calls), spawnedCount)
	}
	if spawnedCount != 1 {
		t.Errorf("spawnedCount = %d; want exactly 1 (first trip spawns, rest overlap-skip)", spawnedCount)
	}
}
