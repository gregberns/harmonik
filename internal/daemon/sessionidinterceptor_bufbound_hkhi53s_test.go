package daemon

// sessionidinterceptor_bufbound_hkhi53s_test.go — regression for hk-hi53s.
//
// SessionIDInterceptor.Read appends every read to s.buf and calls checkBuffer.
// Once the session id is captured (firedOnce && capsSeen) checkBuffer used to
// short-circuit WITHOUT draining s.buf, so every subsequent byte of the session
// (all agent_output_chunk output — many MB on a long run) accumulated in memory
// until session end. The fix drops the accumulator in the terminal state; the
// buffer must stay at ~one read's worth, not grow with total post-caps bytes.

import (
	"bytes"
	"encoding/json"
	"io"
	"testing"

	"github.com/gregberns/harmonik/internal/handlercontract"
)

func TestSessionIDInterceptor_BufferBoundedAfterFire_hkhi53s(t *testing.T) {
	t.Parallel()

	capsMsg := handlercontract.HandlerCapabilitiesMsg{
		Type:              handlercontract.ProgressMsgTypeHandlerCapabilities,
		SupportedVersions: []int{1},
		ClaudeSessionID:   "session-bufbound-hkhi53s",
	}
	capsData, _ := json.Marshal(capsMsg)
	capsLine := append(capsData, '\n')

	// A large volume of post-caps NDJSON — stand-in for a long run's output.
	const postCapsBytes = 512 * 1024
	var post bytes.Buffer
	for post.Len() < postCapsBytes {
		post.WriteString(`{"type":"agent_output_chunk","text":"xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"}` + "\n")
	}
	totalPost := post.Len()

	stream := io.MultiReader(bytes.NewReader(capsLine), bytes.NewReader(post.Bytes()))

	fired := 0
	ic := newSessionIDInterceptor(stream, func(string) { fired++ })

	// Drain to EOF with a modest read buffer so many Read calls occur.
	buf := make([]byte, 4096)
	for {
		_, err := ic.Read(buf)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Read: %v", err)
		}
	}

	if fired != 1 {
		t.Fatalf("callback fired %d times, want 1", fired)
	}

	ic.mu.Lock()
	remaining := ic.buf.Len()
	ic.mu.Unlock()

	// After the fire the accumulator must be effectively empty — certainly not
	// holding the whole post-caps stream. Bound generously at one read's worth.
	if remaining > len(buf) {
		t.Fatalf("buffer retained %d bytes after fire (post-caps stream was %d bytes); want <= %d — unbounded growth regression",
			remaining, totalPost, len(buf))
	}
}
