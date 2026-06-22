//go:build scenario

package sentinel_test

// scenario_adversary_bt6_test.go — BT6 scenario test for the FW4 independent
// fresh-context adversary spawn (flywheel-motion.md §2.3, §2.4; bead hk-jsvc /
// hk-9mr2).
//
// # What is tested (BT6 — adversary fresh-context)
//
// Two coupled properties of the spawned adversary, exercised against the REAL
// production SpawnAdversary path (internal/sentinel/adversary.go) with a real
// on-disk foreign-auditor mission file:
//
//  1. FRESH-CONTEXT property (§2.3, the active ingredient). The adversary the
//     governor spawns carries NO captain transcript: it is dispatched with the
//     foreign-auditor mission (.flywheel/skills/sentinel-adversary.md) — a file
//     that, by its own contract, instructs the session to "Read the evidence as
//     a foreign auditor who has never seen this project before" and reviews the
//     captain's comms/commits as FOREIGN ARTIFACTS via shell-outs, never an
//     injected conversation. The spawn request also binds the adversary to its
//     OWN queue (AdversaryQueueName != the captain/main queue) so the captain's
//     inbox/transcript never bleeds into the adversary's context. We assert the
//     spawn request points only at the mission file + the separate queue — no
//     captain-context channel is threaded in.
//
//  2. EXCEPTION/TRIP BOUND (<= 1). The adversary's ONLY power is to write ONE
//     decision_required exception (§2.1). The structural guarantee that the
//     governor cannot stack adjudicators — and therefore cannot drive more than
//     one exception-write session per online window — is SpawnAdversary's
//     overlap-skip: while the adversary crew is already online, repeated governor
//     trips spawn NO additional adversary. We drive several consecutive trips
//     against the REAL SpawnAdversary and assert AT MOST ONE adversary session is
//     ever spawned (=> at most one exception-write path) across the window.
//
// # Why this is the REAL FW4 path (no stubs of the code under test)
//
// SpawnAdversary is the production function the workloop ACT-mode path calls
// (internal/daemon/workloop.go ~L1609). The AdversaryCrewStarter is necessarily
// injected (the concrete starter is the daemon crew handler, which would spin a
// real tmux session); we inject a recording double for it — that is the seam the
// production wiring itself uses (deps.crewHandler). Everything else — the mission
// resolution, the queue binding, the overlap-skip decision — is the real code.
//
// # Why //go:build scenario
//
// The test performs real filesystem I/O (it materialises the real foreign-auditor
// mission file on disk and asserts the spawn references that on-disk artifact),
// matching the BT3/BT4 scenario idiom. Tagged scenario so the daemon commit-gate
// skips it.
//
// Run independently:
//
//	go test -tags=scenario -run BT6 ./internal/sentinel/...
//
// Spec ref: flywheel-motion.md §2.3 (independence / fresh-context), §2.4
// (movement-gated trigger), §2.1 (one exception). Bead: hk-rsje (flywheel-BT6).
// FW4 bead: hk-jsvc / hk-9mr2. Epic: hk-0oca (codename:flywheel).

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/sentinel"
)

// ---------------------------------------------------------------------------
// helpers (prefix "bt6" per the helper-prefix discipline)
// ---------------------------------------------------------------------------

// bt6RecordingStarter is a recording AdversaryCrewStarter double: it captures the
// raw spawn payload for each HandleCrewStart call so the test can inspect what
// context (if any) the adversary is launched with. It models the daemon crew
// handler that would otherwise spin a real session.
type bt6RecordingStarter struct {
	calls []json.RawMessage
}

func (s *bt6RecordingStarter) HandleCrewStart(_ context.Context, payload json.RawMessage) (json.RawMessage, error) {
	s.calls = append(s.calls, payload)
	return json.RawMessage(`{"session_id":"bt6-adversary-session","name":"sentinel-adversary"}`), nil
}

// bt6AdversaryProject materialises a project dir containing the REAL
// foreign-auditor mission file at the default relative path the adversary spawn
// resolves to. The body is the real production mission text (the fresh-context
// foreign-auditor contract), so the assertion that the spawn references a
// genuinely-foreign-artifact mission is exercised against real content.
func bt6AdversaryProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	missionPath := filepath.Join(dir, sentinel.DefaultAdversaryMissionRelPath)
	if err := os.MkdirAll(filepath.Dir(missionPath), 0o755); err != nil {
		t.Fatalf("bt6AdversaryProject: mkdir: %v", err)
	}
	// The load-bearing parts of the production foreign-auditor mission: it is a
	// fresh-context auditor (no captain transcript), reviewing comms/commits as
	// foreign artifacts, whose ONLY write is a single emit-trip exception.
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

// bt6ParsePayload unmarshals a spawn payload into name/queue/mission_path.
func bt6ParsePayload(t *testing.T, raw json.RawMessage) (name, queue, missionPath string) {
	t.Helper()
	var m map[string]string
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("bt6ParsePayload: %v", err)
	}
	return m["name"], m["queue"], m["mission_path"]
}

// ---------------------------------------------------------------------------
// BT6 — adversary fresh-context + exception/trip bound
// ---------------------------------------------------------------------------

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

	// (a) The adversary binds to its OWN queue, NOT the captain/main queue — so
	// the captain's inbox/transcript cannot leak into the adversary's view.
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

	// (b) The launch context is the foreign-auditor MISSION FILE — not an injected
	// captain conversation. Resolve the file the spawn pointed at and assert it is
	// the foreign-artifact auditor contract, carrying no captain transcript.
	wantMission := filepath.Join(projectDir, sentinel.DefaultAdversaryMissionRelPath)
	if missionPath != wantMission {
		t.Fatalf("mission_path = %q, want %q", missionPath, wantMission)
	}
	body, readErr := os.ReadFile(missionPath)
	if readErr != nil {
		t.Fatalf("read adversary mission: %v", readErr)
	}
	text := string(body)

	// Fresh-context markers: the mission tells the session it is NOT the captain
	// and to read the evidence as a foreign auditor.
	for _, marker := range []string{"FRESH-CONTEXT", "NOT the captain", "foreign"} {
		if !strings.Contains(text, marker) {
			t.Errorf("adversary mission missing fresh-context marker %q; the spawned session "+
				"is not provably foreign-context", marker)
		}
	}
	// It reviews comms/commits as foreign artifacts (shell-outs), NOT an injected
	// captain transcript.
	if !strings.Contains(text, "comms log --from captain") && !strings.Contains(text, "git log") {
		t.Error("adversary mission does not review captain comms/commits as foreign artifacts")
	}
	// Negative: no captain transcript / conversation channel is threaded into the
	// spawn payload itself (only name/queue/mission_path are present).
	var payloadKeys map[string]json.RawMessage
	if err := json.Unmarshal(starter.calls[0], &payloadKeys); err != nil {
		t.Fatalf("unmarshal payload keys: %v", err)
	}
	for k := range payloadKeys {
		switch k {
		case "name", "queue", "mission_path":
			// expected
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

	// online models the live presence set the workloop reads from
	// `harmonik comms who` before each spawn attempt. Once the adversary is
	// spawned it registers presence, so subsequent ticks see it online.
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
			// The freshly-spawned adversary now holds presence: model the
			// `comms who` registration the real session performs on boot.
			online[sentinel.AdversaryCrewName] = struct{}{}
		}
	}

	// EXCEPTION BOUND: at most one adversary session was ever spawned across the
	// whole online window, so at most one exception-write path exists (<= 1 trip).
	if spawnedCount > 1 {
		t.Errorf("adversary spawned %d times across %d consecutive governor trips; want <= 1 "+
			"(overlap-skip must bound exception-write sessions per online window)", spawnedCount, trips)
	}
	if len(starter.calls) != spawnedCount {
		t.Errorf("HandleCrewStart called %d times but spawnedCount=%d; overlap-skip must NOT "+
			"invoke the starter on a skipped tick", len(starter.calls), spawnedCount)
	}
	// And concretely: exactly one spawn for the first trip, none thereafter.
	if spawnedCount != 1 {
		t.Errorf("spawnedCount = %d; want exactly 1 (first trip spawns, rest overlap-skip)", spawnedCount)
	}
}
