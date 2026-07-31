package cli

// quarantine_render_hkujanf_test.go — the quarantine marker on the queue read
// commands.
//
// A quarantined queue keeps its persisted status and its item counts, so every
// column of a `queue list` row reads like a healthy queue. Before this marker a
// permanently dead queue and a slow one were the same picture, and the one
// daemon event that said otherwise had usually scrolled away.
//
// Spec ref: specs/queue-model.md §3.1 QM-001.
// Bead ref: hk-ujanf.

import (
	"encoding/json"
	"strings"
	"testing"
)

// The marker must appear for the shut queue and for nothing else. A listing
// that marks every row is the same blind spot in the other direction.
func TestRenderQueueListText_MarksTheQuarantinedQueueOnly(t *testing.T) {
	t.Parallel()

	result := json.RawMessage(`{"queues":[
		{"name":"main","queue_id":"0197-main","status":"active","pending_items":2,"workers":0,"completed_items":0,"failed_items":0,
		 "quarantine_reason":"rename queue file: no space left on device"},
		{"name":"paul","queue_id":"0197-paul","status":"active","pending_items":1,"workers":1,"completed_items":3,"failed_items":0}
	]}`)

	var out strings.Builder
	if got := renderQueueListText(result, &out); got != exitSuccess {
		t.Fatalf("renderQueueListText = %d; want %d", got, exitSuccess)
	}
	text := out.String()

	if strings.Count(text, "QUARANTINED") != 1 {
		t.Fatalf("QUARANTINED appears %d time(s); want exactly one:\n%s", strings.Count(text, "QUARANTINED"), text)
	}
	if !strings.Contains(text, "no space left on device") {
		t.Errorf("output omits the cause; an operator cannot tell a full disk from a replaced queue file:\n%s", text)
	}

	// The marker must attach to the shut queue, not to the healthy one that
	// happens to follow it.
	mainRow := strings.Index(text, "main")
	paulRow := strings.Index(text, "paul")
	marker := strings.Index(text, "QUARANTINED")
	if marker < mainRow || marker > paulRow {
		t.Errorf("the marker is not between the main row and the paul row; it reads as the wrong queue:\n%s", text)
	}
}

// A healthy listing must stay quiet. A marker that fires on a healthy queue
// trains the operator to skip it.
func TestRenderQueueListText_LeavesHealthyQueuesUnmarked(t *testing.T) {
	t.Parallel()

	result := json.RawMessage(`{"queues":[
		{"name":"main","queue_id":"0197-main","status":"active","pending_items":2,"workers":0,"completed_items":0,"failed_items":0}
	]}`)

	var out strings.Builder
	if got := renderQueueListText(result, &out); got != exitSuccess {
		t.Fatalf("renderQueueListText = %d; want %d", got, exitSuccess)
	}
	if text := out.String(); strings.Contains(text, "QUARANTINED") || strings.Contains(text, "!!") {
		t.Errorf("a healthy queue is marked:\n%s", text)
	}
}

// `queue status` had the same blind spot: it prints the persisted queue status,
// which a quarantine does not change.
func TestRenderQueueStatusText_MarksTheQuarantinedQueue(t *testing.T) {
	t.Parallel()

	result := json.RawMessage(`{"queue":{"name":"main","queue_id":"0197-main","status":"active","groups":[]},
		"quarantine_reason":"rename queue file: no space left on device"}`)

	var out strings.Builder
	if got := renderQueueStatusText(result, &out); got != exitSuccess {
		t.Fatalf("renderQueueStatusText = %d; want %d", got, exitSuccess)
	}
	text := out.String()
	if !strings.Contains(text, "QUARANTINED") {
		t.Fatalf("output has no marker; the queue reads as active:\n%s", text)
	}
	if !strings.Contains(text, "no space left on device") {
		t.Errorf("output omits the cause:\n%s", text)
	}
}

// The condition does not clear by retrying. Text that sends the operator back
// to the same wall is worse than no text, so the block says so and names the
// repair instead.
func TestPrintQuarantineBlock_TellsTheOperatorNotToRetry(t *testing.T) {
	t.Parallel()

	var out strings.Builder
	p := newPrinter(&out)
	printQuarantineBlock(p, "main", "rename queue file: no space left on device")
	text := out.String()

	for _, want := range []string{
		"retrying does not clear this",
		"restart the daemon",
		".harmonik/queues",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("block omits %q:\n%s", want, text)
		}
	}
}

// An empty reason means the queue is healthy, so the block prints nothing. Both
// renderers call it unconditionally and rely on that.
func TestPrintQuarantineBlock_EmptyReasonPrintsNothing(t *testing.T) {
	t.Parallel()

	var out strings.Builder
	printQuarantineBlock(newPrinter(&out), "main", "")
	if out.Len() != 0 {
		t.Errorf("printed %q for a healthy queue; want nothing", out.String())
	}
}
