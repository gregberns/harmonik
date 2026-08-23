package pi_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/gregberns/harmonik/internal/harness/pi"
)

func piStreamFixture(t *testing.T, deltaCount int) (stream []byte, reasoningText string) {
	t.Helper()

	var out bytes.Buffer
	var reasoning strings.Builder

	writeLine := func(v any) {
		t.Helper()
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("build fixture line: %v", err)
		}
		out.Write(b)
		out.WriteByte('\n')
	}

	accumulated := func() map[string]any {
		return map[string]any{
			"role": "assistant",
			"content": []any{
				map[string]any{"type": "thinking", "thinking": reasoning.String(), "thinkingSignature": "reasoning"},
			},
			"model":      "nemotron",
			"provider":   "ornith",
			"stopReason": "stop",
			"usage":      map[string]any{"input": 0, "output": 0, "totalTokens": 0},
		}
	}

	writeLine(map[string]any{"type": "session", "version": 3, "id": "01a0061c-75e9-70c7-94d8-f043f7df2a57", "cwd": "/tmp/wt"})
	writeLine(map[string]any{"type": "agent_start"})
	writeLine(map[string]any{"type": "turn_start"})
	writeLine(map[string]any{"type": "message_start", "message": accumulated()})

	writeLine(map[string]any{
		"type": "message_update",
		"assistantMessageEvent": map[string]any{
			"type": "thinking_start", "contentIndex": 0, "partial": accumulated(),
		},
		"message": accumulated(),
	})

	for i := range deltaCount {
		delta := fmt.Sprintf("step %04d: the model keeps talking and every line repeats it all. ", i)
		reasoning.WriteString(delta)
		writeLine(map[string]any{
			"type": "message_update",
			"assistantMessageEvent": map[string]any{
				"type": "thinking_delta", "contentIndex": 0, "delta": delta, "partial": accumulated(),
			},
			"message": accumulated(),
		})
	}

	writeLine(map[string]any{
		"type": "message_update",
		"assistantMessageEvent": map[string]any{
			"type": "thinking_end", "contentIndex": 0, "content": reasoning.String(), "partial": accumulated(),
		},
		"message": accumulated(),
	})
	writeLine(map[string]any{
		"type": "message_update",
		"assistantMessageEvent": map[string]any{
			"type": "toolcall_end", "contentIndex": 1, "partial": accumulated(),
			"toolCall": map[string]any{"toolCallId": "call_1", "toolName": "bash", "args": map[string]any{"command": "go test ./..."}},
		},
		"message": accumulated(),
	})
	writeLine(map[string]any{"type": "tool_execution_start", "toolCallId": "call_1", "toolName": "bash", "args": map[string]any{"command": "go test ./..."}})
	writeLine(map[string]any{"type": "tool_execution_end", "toolCallId": "call_1", "toolName": "bash", "isError": true, "result": "FAIL: the reason this run died"})
	writeLine(map[string]any{"type": "message_end", "message": accumulated()})
	writeLine(map[string]any{"type": "agent_end", "willRetry": false, "messages": []any{accumulated()}})

	return out.Bytes(), reasoning.String()
}

func piPersist(t *testing.T, stream []byte) []byte {
	t.Helper()

	var sink bytes.Buffer
	w := pi.NewStdoutLogWriter(&sink)
	const chunk = 97
	for off := 0; off < len(stream); off += chunk {
		end := min(off+chunk, len(stream))
		n, err := w.Write(stream[off:end])
		if err != nil {
			t.Fatalf("write chunk at %d: %v", off, err)
		}
		if n != end-off {
			t.Fatalf("write reported %d bytes for a %d-byte chunk.\n"+
				"io.TeeReader treats a short write as an error and fails the read that produced "+
				"the bytes, so the agent's own stdout would break.", n, end-off)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	return sink.Bytes()
}

// TestStdoutLogWriter_ThePersistedLogGrowsWithTheModelsOutputNotItsSquare is the
// test that hk-k4jrh was written for.
//
// Double the model's output and the persisted log must roughly double. It must
// NOT quadruple, which is what a verbatim tee does and what filled the disk.
//
// The test proves the fixture reproduces the defect before it judges the writer.
// Without that check a fixture whose lines did not grow would pass this test
// while defending nothing at all.
func TestStdoutLogWriter_ThePersistedLogGrowsWithTheModelsOutputNotItsSquare(t *testing.T) {
	t.Parallel()

	const deltas = 400

	small, _ := piStreamFixture(t, deltas)
	large, _ := piStreamFixture(t, 2*deltas)

	rawRatio := float64(len(large)) / float64(len(small))
	if rawRatio < 3.0 {
		t.Fatalf("the raw stream grew by %.2fx when the model's output doubled, want >= 3x.\n"+
			"This fixture is supposed to reproduce the quadratic growth (%d -> %d bytes). If it does "+
			"not, the assertion below is measuring nothing.", rawRatio, len(small), len(large))
	}

	smallLog := len(piPersist(t, small))
	largeLog := len(piPersist(t, large))

	logRatio := float64(largeLog) / float64(smallLog)
	if logRatio > 2.5 {
		t.Errorf("the persisted log grew by %.2fx when the model's output doubled, want ~2x (<= 2.5x).\n"+
			"  %d deltas -> %d bytes\n"+
			"  %d deltas -> %d bytes\n"+
			"Growth that tracks the square of the turn is the defect: one 8.5-minute run wrote "+
			"197 MB for 45 KB of output, and under 10 GiB free the daemon pauses dispatch silently.",
			logRatio, deltas, smallLog, 2*deltas, largeLog)
	}

	if largeLog*10 > len(large) {
		t.Errorf("the persisted log is %d bytes for a %d-byte stream — less than a 10x saving.\n"+
			"The accumulated message snapshots are the whole cost; dropping them shrank the measured "+
			"run by 237x. A saving this small means they are still being written.", largeLog, len(large))
	}
}

// TestStdoutLogWriter_APostMortemCanStillReadTheReasoningAndTheToolCalls holds
// the shrink to the thing it is allowed to cost.
//
// A smaller log that has lost the failure reason is worse than the big one. So:
// the deltas must still concatenate to every character the model reasoned, the
// tool call and its result must survive, and the final message must survive
// whole.
func TestStdoutLogWriter_APostMortemCanStillReadTheReasoningAndTheToolCalls(t *testing.T) {
	t.Parallel()

	stream, reasoning := piStreamFixture(t, 50)
	persisted := piPersist(t, stream)

	var (
		fromDeltas   strings.Builder
		endedContent string
		toolCallSeen bool
		toolResult   string
		finalMessage string
	)
	for _, line := range bytes.Split(bytes.TrimRight(persisted, "\n"), []byte("\n")) {
		var event struct {
			Type                  string `json:"type"`
			AssistantMessageEvent struct {
				Type     string          `json:"type"`
				Delta    string          `json:"delta"`
				Content  string          `json:"content"`
				ToolCall json.RawMessage `json:"toolCall"`
				Partial  json.RawMessage `json:"partial"`
			} `json:"assistantMessageEvent"`
			Message json.RawMessage `json:"message"`
			Result  string          `json:"result"`
		}
		if err := json.Unmarshal(line, &event); err != nil {
			t.Fatalf("the persisted log is not valid NDJSON: %v\nline: %.200s", err, line)
		}
		switch event.Type {
		case "message_update":
			if len(event.AssistantMessageEvent.Partial) > 0 {
				t.Fatalf("a persisted message_update still carries assistantMessageEvent.partial.\n" +
					"That field holds the whole message so far, which is what made the file quadratic.")
			}
			if len(event.Message) > 0 {
				t.Fatalf("a persisted message_update still carries a top-level \"message\".\n" +
					"It holds the same accumulated snapshot as partial. Dropping only one of the two " +
					"halves the constant and leaves the growth quadratic.")
			}
			fromDeltas.WriteString(event.AssistantMessageEvent.Delta)
			if event.AssistantMessageEvent.Type == "thinking_end" {
				endedContent = event.AssistantMessageEvent.Content
			}
			if len(event.AssistantMessageEvent.ToolCall) > 0 {
				toolCallSeen = bytes.Contains(event.AssistantMessageEvent.ToolCall, []byte("go test ./..."))
			}
		case "tool_execution_end":
			toolResult = event.Result
		case "message_end":
			finalMessage = string(event.Message)
		}
	}

	if fromDeltas.String() != reasoning {
		t.Errorf("the deltas in the persisted log do not reconstruct the model's reasoning: "+
			"got %d characters, want %d.\n"+
			"The deltas are the ONLY complete record of the reasoning once the accumulated snapshots "+
			"are dropped. If they do not add up, the log has lost the failure reason.",
			fromDeltas.Len(), len(reasoning))
	}
	if endedContent != reasoning {
		t.Errorf("thinking_end no longer carries the completed block: got %d characters, want %d.\n"+
			"It is the cross-check on the deltas and a separate field from partial, so it must survive.",
			len(endedContent), len(reasoning))
	}
	if !toolCallSeen {
		t.Error("the persisted log lost the tool call.\n" +
			"toolcall_end carries the call in its own \"toolCall\" field. A post-mortem that cannot " +
			"see what the agent ran cannot explain what it did.")
	}
	if toolResult != "FAIL: the reason this run died" {
		t.Errorf("the persisted log lost the tool result: %q.\n"+
			"tool_execution_end is not a message_update and must pass through untouched.", toolResult)
	}
	if !strings.Contains(finalMessage, reasoning) {
		t.Error("the persisted log lost the final assistant message.\n" +
			"message_end carries it in full and is not rewritten, so the whole turn survives once " +
			"even if the deltas are read by nobody.")
	}
}

// TestStdoutLogWriter_EverythingThatIsNotAMessageUpdatePassesThroughByteForByte
// pins the blast radius of the rewrite.
//
// The writer is allowed to touch exactly one event type. A line it cannot parse,
// a line of another type, and a line that merely quotes the discriminator inside
// a tool result all have to land on disk unchanged — the malformed one most of
// all, because a truncated or garbled line is often the whole reason a run is
// being read after the fact.
func TestStdoutLogWriter_EverythingThatIsNotAMessageUpdatePassesThroughByteForByte(t *testing.T) {
	t.Parallel()

	lines := []string{
		`{"type":"session","version":3,"id":"01a0061c","cwd":"/tmp/wt"}`,
		`{"type":"agent_end","willRetry":true,"messages":[{"role":"assistant","usage":{"input_tokens":3}}]}`,
		`{"type":"tool_execution_end","toolCallId":"c1","result":"the log said \"type\":\"message_update\" here"}`,
		`{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":"done"}]}}`,
		// The one that decides whether the writer keys on the EVENT TYPE or merely
		// on the presence of the fields it strips. This line carries a top-level
		// "message", an "assistantMessageEvent" and a "partial" inside it, and its
		// type is not message_update. Pi does not emit this shape today. That is
		// the point: the writer's contract is "message_update and nothing else",
		// and a writer that strips by field name instead would rewrite whatever
		// the next Pi release adds.
		`{"type":"message_end","assistantMessageEvent":{"type":"message_update","partial":{"role":"assistant"}},"message":{"role":"assistant"}}`,
		`this line is not JSON at all`,
		`{"type":"message_update","assistantMessageEvent":{"type":"thinking_delta","delta":"x"}}`,
		`{"type":"message_update","truncated mid line`,
	}
	stream := []byte(strings.Join(lines, "\n") + "\n")

	got := string(piPersist(t, stream))
	if got != string(stream) {
		t.Errorf("lines that carry no accumulated snapshot were rewritten.\n got: %q\nwant: %q\n"+
			"A capture is read when a run has already failed. Rewriting a line the writer does not "+
			"fully understand is how the evidence stops being evidence.", got, stream)
	}
}

// TestStdoutLogWriter_ATurnThatEndsWithoutANewlineStillLandsOnDisk defends the
// last line of a killed run.
//
// Pi's process exit is unreliable and the daemon kills the session on agent_end,
// so a capture very often ends mid-line. That last fragment is the closest thing
// to a cause of death the log has, and buffering it without a flush would drop
// exactly it.
func TestStdoutLogWriter_ATurnThatEndsWithoutANewlineStillLandsOnDisk(t *testing.T) {
	t.Parallel()

	stream := []byte(`{"type":"session","id":"01a0061c"}` + "\n" + `{"type":"tool_execution_end","result":"killed mid`)

	got := string(piPersist(t, stream))
	if got != string(stream) {
		t.Errorf("the trailing fragment was dropped.\n got: %q\nwant: %q\n"+
			"A killed Pi run ends mid-line, and that fragment is the last thing it said.", got, stream)
	}
}

// TestStdoutLogWriter_ARewrittenLineCarriesTheModelsCharactersUnchanged is the
// difference between a log you can grep and a log you cannot.
//
// The writer decodes a message_update and re-encodes it. Go's default JSON
// encoder escapes `<`, `>` and `&` to `\u003c`, `\u003e` and `\u0026`, so a
// rewritten line silently stops saying what the model said. Measured on the
// real capture: 122 of the 4,139 delta lines and 169 characters, mostly shell
// process substitution. A run reasoning about Go channels, HTML or XML is far
// worse.
//
// The cost is not cosmetic. Pass-through lines keep the raw bytes and rewritten
// lines would not, so the file disagrees with itself, and a person searching it
// for `<-ch` finds half the evidence and concludes the other half is absent.
func TestStdoutLogWriter_ARewrittenLineCarriesTheModelsCharactersUnchanged(t *testing.T) {
	t.Parallel()

	const delta = `select { case v := <-ch: if a < b && c > d { emit("<tag>&amp;") } }`

	line := `{"type":"message_update","assistantMessageEvent":{"type":"thinking_delta","contentIndex":0,` +
		`"delta":` + piRawJSONString(t, delta) + `,"partial":{"role":"assistant"}},"message":{"role":"assistant"}}`
	if !strings.Contains(line, "<-ch") {
		t.Fatalf("the fixture does not contain the raw characters under test: %s", line)
	}

	persisted := string(piPersist(t, []byte(line+"\n")))

	for _, escape := range []string{`\u003c`, `\u003e`, `\u0026`} {
		if strings.Contains(persisted, escape) {
			t.Errorf("the persisted line escaped a character the model wrote (%s).\n%s\n"+
				"Pass-through lines keep the raw byte, so the file now disagrees with itself and a "+
				"grep for the model's own text finds only half of it.", escape, persisted)
		}
	}
	if !strings.Contains(persisted, "<-ch") {
		t.Errorf("the persisted line no longer contains %q.\n%s", "<-ch", persisted)
	}

	var got struct {
		AssistantMessageEvent struct {
			Delta string `json:"delta"`
		} `json:"assistantMessageEvent"`
	}
	if uErr := json.Unmarshal([]byte(persisted), &got); uErr != nil {
		t.Fatalf("the persisted line is not valid JSON: %v", uErr)
	}
	if got.AssistantMessageEvent.Delta != delta {
		t.Errorf("the delta round-tripped to something else.\n got: %q\nwant: %q",
			got.AssistantMessageEvent.Delta, delta)
	}
}

// TestStdoutLogWriter_ARewrittenLineDropsTheSnapshotsAndKeepsEveryOtherField
// pins the surviving field set.
//
// Every other test here reads the fields it happens to care about, so a writer
// that quietly dropped `contentIndex` — the field that says WHICH content block
// a delta belongs to, and therefore the field that makes two interleaved blocks
// reconstructable — would keep the whole suite green. This one names the exact
// set, so removing a field is a failure and adding one is a decision somebody
// has to make on purpose.
func TestStdoutLogWriter_ARewrittenLineDropsTheSnapshotsAndKeepsEveryOtherField(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		line      string
		wantTop   []string
		wantInner []string
	}{
		{
			name:      "a delta keeps its content index and its delta",
			line:      `{"type":"message_update","assistantMessageEvent":{"type":"thinking_delta","contentIndex":2,"delta":"hi","partial":{"role":"assistant"}},"message":{"role":"assistant"}}`,
			wantTop:   []string{"assistantMessageEvent", "type"},
			wantInner: []string{"contentIndex", "delta", "type"},
		},
		{
			name:      "a completed block keeps its content",
			line:      `{"type":"message_update","assistantMessageEvent":{"type":"thinking_end","contentIndex":0,"content":"all of it","partial":{"role":"assistant"}},"message":{"role":"assistant"}}`,
			wantTop:   []string{"assistantMessageEvent", "type"},
			wantInner: []string{"content", "contentIndex", "type"},
		},
		{
			name:      "a finished tool call keeps the call",
			line:      `{"type":"message_update","assistantMessageEvent":{"type":"toolcall_end","contentIndex":1,"toolCall":{"toolName":"bash"},"partial":{"role":"assistant"}},"message":{"role":"assistant"}}`,
			wantTop:   []string{"assistantMessageEvent", "type"},
			wantInner: []string{"contentIndex", "toolCall", "type"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			persisted := piPersist(t, []byte(tc.line+"\n"))

			var top map[string]json.RawMessage
			if err := json.Unmarshal(bytes.TrimSpace(persisted), &top); err != nil {
				t.Fatalf("the persisted line is not valid JSON: %v\n%s", err, persisted)
			}
			if got := piSortedKeys(top); !slices.Equal(got, tc.wantTop) {
				t.Errorf("top-level fields are %v, want %v.\n"+
					"The writer removes the two accumulated snapshots and nothing else.", got, tc.wantTop)
			}
			var inner map[string]json.RawMessage
			if err := json.Unmarshal(top["assistantMessageEvent"], &inner); err != nil {
				t.Fatalf("assistantMessageEvent is not an object: %v", err)
			}
			if got := piSortedKeys(inner); !slices.Equal(got, tc.wantInner) {
				t.Errorf("assistantMessageEvent fields are %v, want %v.\n"+
					"contentIndex says WHICH block a delta belongs to. Without it two interleaved "+
					"blocks cannot be told apart, and no other test here would notice it going missing.",
					got, tc.wantInner)
			}
		})
	}
}

func piSortedKeys(m map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

type piLockedSink struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *piLockedSink) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *piLockedSink) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// TestStdoutLogWriter_CloseIsSafeWhileTheWatcherIsStillDraining defends the
// writer against the shape of its own caller.
//
// Close runs from runAgentLaunch's defer, on the dispatch goroutine. Write runs
// from the SpawnWatcher's read goroutine. Three documented paths return with the
// watcher STILL DRAINING and say so in their own comments — the kill-watcher
// reap grace in internal/runloop/waitsocketgrace.go and both "watcher.Done()
// reap timed out after Kill — continuing" sites in internal/daemon/agentlaunch.go
// — and callers then read launch.Watcher.Err() afterwards, so the watcher
// provably outlives the function.
//
// The *os.File this writer replaced synchronises Close against Write internally.
// A bytes.Buffer does not, so the unguarded version can panic inside the buffer
// on the daemon's dispatch goroutine — on exactly the cancelled, stalled and
// killed runs this log exists to explain.
func TestStdoutLogWriter_CloseIsSafeWhileTheWatcherIsStillDraining(t *testing.T) {
	t.Parallel()

	sink := &piLockedSink{}
	w := pi.NewStdoutLogWriter(sink)

	const line = `{"type":"message_update","assistantMessageEvent":{"type":"thinking_delta","contentIndex":0,"delta":"still draining","partial":{"role":"assistant"}},"message":{"role":"assistant"}}` + "\n"

	drained := make(chan struct{})
	go func() {
		defer close(drained)
		for range 3000 {
			if _, err := w.Write([]byte(line)); err != nil {
				t.Errorf("the watcher's write failed: %v", err)
				return
			}
		}
	}()

	if err := w.Close(); err != nil {
		t.Errorf("close during the drain: %v", err)
	}
	<-drained
	if err := w.Close(); err != nil {
		t.Errorf("close after the drain: %v", err)
	}

	if got := sink.String(); !strings.Contains(got, "still draining") {
		t.Error("the drain wrote nothing, so this test raced nothing.\n" +
			"A writer that dropped every line would pass a race check for free.")
	}
}

func piRawJSONString(t *testing.T, s string) string {
	t.Helper()
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(s); err != nil {
		t.Fatalf("quote the fixture delta: %v", err)
	}
	return strings.TrimRight(b.String(), "\n")
}
