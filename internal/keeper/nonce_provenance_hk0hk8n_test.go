package keeper

// nonce_provenance_hk0hk8n_test.go — T6 join-key verification
// (hk-keeper-delivery-nonce-provenance-0hk8n, SK-031).
//
// Proves the provenance channel's core invariant WITHOUT the (deferred, post-T3)
// render-into-slot step: the auto-cycle KEEPER:<cycle_id> marker, the value a
// leader-defer message would render into `restart-now --nonce <cycle_id>`, and
// the restart-now emitted event's nonce are ONE identical cyc-<ts>-<seq> value,
// so a query of events.jsonl by that value joins the self-restart to its
// originating cycle. No shared runtime state — the value travels only as text
// (the handoff marker → the restart-now command string).

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
)

// TestNonceProvenance_MarkerAndRestartNowEventShareJoinKey is the T6 acceptance
// (minus the deferred render): the KEEPER:<id> marker cycle_id, the restart-now
// --nonce, and the emitted event's nonce are one identical value that joins the
// restart-now event to its cycle in events.jsonl.
func TestNonceProvenance_MarkerAndRestartNowEventShareJoinKey(t *testing.T) {
	dir := t.TempDir()
	agent := "captain"
	writeSidAndCtx(t, dir, agent, goodSID)
	requested := time.Now()

	// The keeper minted this cycle_id at cycle entry and embedded it in the
	// handoff's KEEPER marker (nonceMarker is the production builder).
	const cycleID = "cyc-20260718T000000-000042"
	handoffPath := filepath.Join(dir, "HANDOFF-"+agent+".md")
	content := "# handoff\n\n" + nonceMarker(cycleID) + "\n\nsome handoff body.\n"
	if err := os.WriteFile(handoffPath, []byte(content), 0o644); err != nil { //nolint:gosec // G306: test fixture under t.TempDir()
		t.Fatal(err)
	}
	fresh := requested.Add(time.Second)
	if err := os.Chtimes(handoffPath, fresh, fresh); err != nil {
		t.Fatal(err)
	}

	// (1) SURFACE: the cycle_id is recoverable from the marker as text (the join
	// key a leader-defer message would render into `restart-now --nonce <id>`).
	got, ok := CycleIDFromNonceMarker(content)
	if !ok || got != cycleID {
		t.Fatalf("CycleIDFromNonceMarker = (%q, %v); want (%q, true)", got, ok, cycleID)
	}

	// (2) RECORD: restart-now --nonce <the SAME cycle_id> emits a durable event
	// carrying it. A real FileEmitter so the event lands in events.jsonl — the
	// surface the join query runs against. A daemon-initialized project already
	// has .harmonik/events/; create it here to mirror that (the FileEmitter
	// O_CREATEs the file, not its parent dir).
	if err := os.MkdirAll(filepath.Join(dir, ".harmonik", "events"), 0o755); err != nil {
		t.Fatal(err)
	}
	rec := &recordingInjector{}
	err := RestartNow(context.Background(), RestartNowConfig{
		ProjectDir:  dir,
		AgentName:   agent,
		TmuxTarget:  "sess:0",
		Inject:      rec.inject,
		RequestedAt: requested,
		Emitter:     NewFileEmitter(dir),
	}, cycleID)
	if err != nil {
		t.Fatalf("RestartNow: %v", err)
	}

	// (3) JOIN: a query of events.jsonl by that nonce finds the restart-now event.
	evPath := filepath.Join(dir, ".harmonik", core.EventsJSONLPath)
	data, rerr := os.ReadFile(evPath) //nolint:gosec // path is the test temp dir
	if rerr != nil {
		t.Fatalf("read events.jsonl at %s: %v", evPath, rerr)
	}
	joined := false
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var ev core.Event
		if json.Unmarshal([]byte(line), &ev) != nil {
			continue
		}
		if ev.Type != string(core.EventTypeSessionKeeperRestartNow) {
			continue
		}
		var p core.SessionKeeperRestartNowPayload
		if json.Unmarshal(ev.Payload, &p) != nil {
			continue
		}
		if p.Nonce == cycleID {
			joined = true
			break
		}
	}
	if !joined {
		t.Fatalf("no session_keeper_restart_now event in events.jsonl carries nonce=%q — "+
			"the provenance join (marker cycle_id → restart-now event nonce) is broken", cycleID)
	}

	// The three views of the join key are ONE identical value (marker == nonce ==
	// event nonce), with no shared runtime state — only the marker text carried it.
}

// TestCycleIDFromNonceMarker_Malformed pins the extractor's negative cases so a
// missing/malformed marker never yields a bogus join key.
func TestCycleIDFromNonceMarker_Malformed(t *testing.T) {
	cases := []struct {
		name    string
		content string
		wantOK  bool
		wantID  string
	}{
		{"well-formed", "x " + nonceMarker("cyc-1-1") + " y", true, "cyc-1-1"},
		{"no-marker", "no keeper marker here", false, ""},
		{"unclosed", "<!-- KEEPER:cyc-2-2 (never closed", false, ""},
		{"empty-id", "<!-- KEEPER: -->", false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id, ok := CycleIDFromNonceMarker(tc.content)
			if ok != tc.wantOK || id != tc.wantID {
				t.Errorf("CycleIDFromNonceMarker(%q) = (%q, %v); want (%q, %v)",
					tc.content, id, ok, tc.wantID, tc.wantOK)
			}
		})
	}
}
