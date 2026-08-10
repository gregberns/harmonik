package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRenderQueueStatusTextShowsReceiptBackedCompletion(t *testing.T) {
	result := json.RawMessage(`{"queue":null,"completed":true,"final_status":"complete-success","final_group_index":0,"success_count":1,"fail_count":0,"completed_at":"2026-08-10T18:12:13.456Z","completion_receipt_id":"0197c452-0000-7000-8000-000000000003","active_runs":[]}`)
	var out strings.Builder
	if exit := renderQueueStatusText(result, &out); exit != exitSuccess {
		t.Fatalf("exit = %d", exit)
	}
	for _, want := range []string{
		"queue:    completed",
		"receipt:  0197c452-0000-7000-8000-000000000003",
		"final:    complete-success  group=0  success=1  failed=0",
		"completed_at: 2026-08-10T18:12:13.456Z",
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("output %q does not contain %q", out.String(), want)
		}
	}
}
