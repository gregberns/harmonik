package daemon

// agent_message_test.go — shared table test for MatchAgentMessage (N1).
//
// Spec: agent-comms §8 N1 requires that the live offer path and the durable
// replay path return IDENTICAL verdicts for the same (payload, to, from, topic)
// inputs. This test drives all cases through:
//   (a) MatchAgentMessage directly (the shared predicate),
//   (b) subscriptionStream.offer (live path), and
//   (c) HandleSubscribe JSONL replay (durable replay path),
// asserting that all three agree on every row.

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gregberns/harmonik/internal/core"
)

// TestMatchAgentMessage_SharedTable is the N1 shared table test. It runs the
// same addressing-filter cases through the predicate directly, the live offer
// path, and the replay path, asserting identical verdicts.
func TestMatchAgentMessage_SharedTable(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		payload AgentMessagePayload
		// Subscribe-side filter values (empty = wildcard).
		to    string
		from  string
		topic string
		want  bool
	}{
		{
			name:    "directed-to-me",
			payload: AgentMessagePayload{To: "alice", From: "bob", Body: "hi"},
			to:      "alice",
			want:    true,
		},
		{
			name:    "directed-to-other",
			payload: AgentMessagePayload{To: "carol", From: "bob", Body: "hi"},
			to:      "alice",
			want:    false,
		},
		{
			name:    "broadcast-star-matches-specific-to-filter",
			payload: AgentMessagePayload{To: "*", From: "bob", Body: "broadcast"},
			to:      "alice",
			want:    true,
		},
		{
			name:    "broadcast-star-matches-empty-to-filter",
			payload: AgentMessagePayload{To: "*", From: "bob", Body: "broadcast"},
			to:      "",
			want:    true,
		},
		{
			name:    "from-filter-match",
			payload: AgentMessagePayload{To: "alice", From: "bob", Body: "hi"},
			to:      "alice",
			from:    "bob",
			want:    true,
		},
		{
			name:    "from-filter-no-match",
			payload: AgentMessagePayload{To: "alice", From: "carol", Body: "hi"},
			to:      "alice",
			from:    "bob",
			want:    false,
		},
		{
			name:    "topic-filter-match",
			payload: AgentMessagePayload{To: "alice", From: "bob", Topic: "status", Body: "hi"},
			to:      "alice",
			topic:   "status",
			want:    true,
		},
		{
			name:    "topic-filter-no-match",
			payload: AgentMessagePayload{To: "alice", From: "bob", Topic: "other", Body: "hi"},
			to:      "alice",
			topic:   "status",
			want:    false,
		},
		{
			name:    "wildcard-all-empty-filters",
			payload: AgentMessagePayload{To: "anyone", From: "sender", Body: "hi"},
			to:      "",
			from:    "",
			topic:   "",
			want:    true,
		},
		{
			name:    "combined-all-match",
			payload: AgentMessagePayload{To: "alice", From: "bob", Topic: "status", Body: "hi"},
			to:      "alice",
			from:    "bob",
			topic:   "status",
			want:    true,
		},
		{
			name:    "combined-topic-no-match",
			payload: AgentMessagePayload{To: "alice", From: "bob", Topic: "other", Body: "hi"},
			to:      "alice",
			from:    "bob",
			topic:   "status",
			want:    false,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			payloadBytes, marshalErr := json.Marshal(tc.payload)
			if marshalErr != nil {
				t.Fatalf("marshal payload: %v", marshalErr)
			}

			// (a) Predicate directly.
			gotPredicate := MatchAgentMessage(tc.payload, tc.to, tc.from, tc.topic)
			if gotPredicate != tc.want {
				t.Errorf("MatchAgentMessage: got %v, want %v", gotPredicate, tc.want)
			}

			// (b) Live path: subscriptionStream.offer.
			// wildcard=true passes the type filter; only the addressing filter is
			// under test here.
			var gotLive bool
			{
				evtID, _ := uuid.NewV7()
				evt := core.Event{
					EventID:       core.EventID(evtID),
					SchemaVersion: 1,
					Type:          "agent_message",
					TimestampWall: time.Now(),
					Payload:       json.RawMessage(payloadBytes),
				}
				s := &subscriptionStream{
					ch:       make(chan core.Event, 2),
					wildcard: true,
					to:       tc.to,
					from:     tc.from,
					topic:    tc.topic,
				}
				s.offer(evt)
				gotLive = len(s.ch) > 0
			}
			if gotLive != tc.want {
				t.Errorf("live path (offer): got %v, want %v", gotLive, tc.want)
			}

			// (c) Replay path: HandleSubscribe with a JSONL file containing the
			// test event. cursorID is used as since_event_id so only the test
			// event is in the replay window.
			var gotReplay bool
			{
				dir := t.TempDir()
				jsonlPath := filepath.Join(dir, "events.jsonl")

				cursorID, uuidErr := uuid.NewV7()
				if uuidErr != nil {
					t.Fatalf("cursor uuid: %v", uuidErr)
				}
				time.Sleep(2 * time.Millisecond) // ensure UUIDv7 ordering
				evtID, uuidErr2 := uuid.NewV7()
				if uuidErr2 != nil {
					t.Fatalf("event uuid: %v", uuidErr2)
				}
				testEvt := core.Event{
					EventID:       core.EventID(evtID),
					SchemaVersion: 1,
					Type:          "agent_message",
					TimestampWall: time.Now(),
					Payload:       json.RawMessage(payloadBytes),
				}

				f, createErr := os.Create(jsonlPath)
				if createErr != nil {
					t.Fatalf("create jsonl: %v", createErr)
				}
				if encErr := json.NewEncoder(f).Encode(testEvt); encErr != nil {
					_ = f.Close()
					t.Fatalf("encode test event: %v", encErr)
				}
				_ = f.Close()

				hub := NewSubscribeHub(SubscribeHubConfig{
					Bus:             nil,
					EventsJSONLPath: jsonlPath,
				})

				srv, cli := net.Pipe()
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)

				handlerDone := make(chan struct{})
				go func() {
					defer close(handlerDone)
					hub.HandleSubscribe(ctx, srv, SubscribeRequest{
						SinceEventID:     uuid.UUID(cursorID).String(),
						HeartbeatSeconds: 600,
						To:               tc.to,
						From:             tc.from,
						Topic:            tc.topic,
					})
				}()

				// Read one line from replay. A 500ms deadline is generous for
				// JSONL replay (in-memory scan) and lets "should not arrive"
				// cases time out cleanly.
				rdr := bufio.NewReader(cli)
				_ = cli.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
				line, _ := rdr.ReadBytes('\n')

				_ = cli.Close()
				cancel()
				<-handlerDone

				if len(line) > 0 {
					var gotEvt core.Event
					if json.Unmarshal(line, &gotEvt) == nil && gotEvt.EventID == testEvt.EventID {
						gotReplay = true
					}
				}
			}
			if gotReplay != tc.want {
				t.Errorf("replay path: got %v, want %v", gotReplay, tc.want)
			}

			// Key assertion: both paths must agree (identical verdicts).
			if gotLive != gotReplay {
				t.Errorf("path divergence: live=%v replay=%v — paths MUST return identical verdicts for the same input",
					gotLive, gotReplay)
			}
		})
	}
}

// TestMatchAgentMessage_NonAgentMessageBypass verifies that the addressing
// filter does not apply to non-agent_message events. Both the live offer path
// and the replay path must deliver non-comms events unaffected by to/from/topic
// filters (§3: "non-comms events bypass addressing").
func TestMatchAgentMessage_NonAgentMessageBypass(t *testing.T) {
	t.Parallel()

	// Live path: a run_completed event should pass through even with a
	// restrictive to-filter, because the addressing block is guarded by
	// evt.Type == "agent_message".
	t.Run("live-path-bypass", func(t *testing.T) {
		t.Parallel()
		evtID, _ := uuid.NewV7()
		evt := core.Event{
			EventID:       core.EventID(evtID),
			SchemaVersion: 1,
			Type:          "run_completed",
			TimestampWall: time.Now(),
			Payload:       json.RawMessage(`{}`),
		}
		s := &subscriptionStream{
			ch:       make(chan core.Event, 2),
			wildcard: true,
			to:       "alice", // restrictive filter — must NOT affect run_completed
			from:     "bob",
			topic:    "status",
		}
		s.offer(evt)
		if got := len(s.ch); got != 1 {
			t.Errorf("non-agent_message bypass: got %d events in channel, want 1 (addressing filter must not apply)", got)
		}
	})

	// Replay path: a run_completed event in JSONL should be delivered even when
	// to/from/topic filters are set.
	t.Run("replay-path-bypass", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		jsonlPath := filepath.Join(dir, "events.jsonl")

		cursorID, _ := uuid.NewV7()
		time.Sleep(2 * time.Millisecond)
		evtID, _ := uuid.NewV7()
		testEvt := core.Event{
			EventID:       core.EventID(evtID),
			SchemaVersion: 1,
			Type:          "run_completed",
			TimestampWall: time.Now(),
			Payload:       json.RawMessage(`{}`),
		}

		f, _ := os.Create(jsonlPath)
		_ = json.NewEncoder(f).Encode(testEvt)
		_ = f.Close()

		hub := NewSubscribeHub(SubscribeHubConfig{
			Bus:             nil,
			EventsJSONLPath: jsonlPath,
		})

		srv, cli := net.Pipe()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)

		done := make(chan struct{})
		go func() {
			defer close(done)
			hub.HandleSubscribe(ctx, srv, SubscribeRequest{
				SinceEventID:     uuid.UUID(cursorID).String(),
				HeartbeatSeconds: 600,
				To:               "alice", // restrictive — must not block run_completed
				From:             "bob",
				Topic:            "status",
			})
		}()

		rdr := bufio.NewReader(cli)
		_ = cli.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		line, _ := rdr.ReadBytes('\n')

		_ = cli.Close()
		cancel()
		<-done

		if len(line) == 0 {
			t.Fatalf("replay bypass: no event received; run_completed must not be filtered by addressing predicate")
		}
		var gotEvt core.Event
		if err := json.Unmarshal(line, &gotEvt); err != nil {
			t.Fatalf("decode replayed event: %v", err)
		}
		if gotEvt.EventID != testEvt.EventID {
			t.Errorf("event_id mismatch: got %v, want %v", gotEvt.EventID, testEvt.EventID)
		}
	})
}
