package queue_test

// rpc_template_param_validation_test.go — WG-045 ingestion-boundary validation.
//
// HandleQueueSubmit MUST reject malformed template-param keys and control-char /
// over-length values BEFORE persist, so a poison value never reaches the
// substitution path. This is the primary fail-fast chokepoint (the substitution
// backstop in internal/workflow covers the daemon-down local-persist path).

import (
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/queue"
)

func submitReqWithParams(beadID core.BeadID, key, val string) queue.QueueSubmitRequest {
	return queue.QueueSubmitRequest{
		SchemaVersion: 1,
		Groups: []queue.Group{
			{
				GroupIndex: 0,
				Kind:       queue.GroupKindStream,
				Status:     queue.GroupStatusPending,
				Items: []queue.Item{
					{
						BeadID:         beadID,
						Status:         queue.ItemStatusPending,
						TemplateParams: map[string]string{key: val},
					},
				},
				CreatedAt: time.Now().UTC(),
			},
		},
	}
}

func TestHandleQueueSubmit_RejectsControlCharParamValue(t *testing.T) {
	const bead = "hk-rpcparam1"
	projectDir := rpcFixtureTempProjectDir(t)
	ledger := rpcFixtureOpenLedger(bead)

	req := submitReqWithParams(bead, "SID", "x\ny") // embedded newline = injection primitive
	_, _, _, rpcErr := queue.HandleQueueSubmit(t.Context(), req, ledger, projectDir, 1)
	if rpcErr == nil {
		t.Fatal("expected RPCError for control-char param value, got nil")
	}
	if rpcErr.Code != -32602 || rpcErr.Message != "invalid_template_param" {
		t.Fatalf("RPCError = {code=%d msg=%q}, want {code=-32602 msg=invalid_template_param}", rpcErr.Code, rpcErr.Message)
	}
}

func TestHandleQueueSubmit_RejectsBadParamKey(t *testing.T) {
	const bead = "hk-rpcparam2"
	projectDir := rpcFixtureTempProjectDir(t)
	ledger := rpcFixtureOpenLedger(bead)

	req := submitReqWithParams(bead, "path", "ok") // lowercase key violates grammar
	_, _, _, rpcErr := queue.HandleQueueSubmit(t.Context(), req, ledger, projectDir, 1)
	if rpcErr == nil {
		t.Fatal("expected RPCError for malformed param key, got nil")
	}
	if rpcErr.Message != "invalid_template_param" {
		t.Fatalf("RPCError.Message = %q, want invalid_template_param", rpcErr.Message)
	}
}

func TestHandleQueueSubmit_RejectsOverLengthParamValue(t *testing.T) {
	const bead = "hk-rpcparam3"
	projectDir := rpcFixtureTempProjectDir(t)
	ledger := rpcFixtureOpenLedger(bead)

	req := submitReqWithParams(bead, "SID", strings.Repeat("v", 9000)) // > 8192-byte cap
	_, _, _, rpcErr := queue.HandleQueueSubmit(t.Context(), req, ledger, projectDir, 1)
	if rpcErr == nil {
		t.Fatal("expected RPCError for over-length param value, got nil")
	}
	if rpcErr.Message != "invalid_template_param" {
		t.Fatalf("RPCError.Message = %q, want invalid_template_param", rpcErr.Message)
	}
}

func TestHandleQueueSubmit_AcceptsValidShellMetacharParamValue(t *testing.T) {
	const bead = "hk-rpcparam4"
	projectDir := rpcFixtureTempProjectDir(t)
	ledger := rpcFixtureOpenLedger(bead)

	// Shell metacharacters in a value are NOT rejected here — neutralising them is
	// the substitution shell-quoting close's job. Validation only blocks control
	// chars / bad keys / over-length, so this must be accepted.
	req := submitReqWithParams(bead, "SID", "x; touch /tmp/pwned #")
	_, q, _, rpcErr := queue.HandleQueueSubmit(t.Context(), req, ledger, projectDir, 1)
	if rpcErr != nil {
		t.Fatalf("unexpected RPCError for valid (metachar) value: %v", rpcErr)
	}
	if q == nil {
		t.Fatal("expected a non-nil queue on accept")
	}
}
