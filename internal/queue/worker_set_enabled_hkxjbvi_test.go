package queue_test

// worker_set_enabled_hkxjbvi_test.go — unit tests for HandleWorkerSetEnabled,
// the live `harmonik worker enable/disable` toggle (hk-xjbvi).
//
// Coverage:
//   - EnableOK:        wired toggle, valid name → echoes {name, enabled:true}
//   - DisableOK:       wired toggle → echoes {name, enabled:false}
//   - UnknownName:     toggle returns an error → invalid_worker RPCError
//   - NotWired:        no toggle func → -32099 (no worker registry)
//   - MissingName:     empty name → invalid_worker before reaching the toggle

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/gregberns/harmonik/internal/queue"
)

// workerToggleFixture builds a HandlerAdapter with an optional worker-toggle
// func. When toggle is nil the adapter is left unwired (the no-registry case).
func workerToggleFixture(toggle func(name string, enabled bool) (string, error)) *queue.HandlerAdapter {
	a := queue.NewHandlerAdapter(nil, "", nil, nil)
	if toggle != nil {
		a.SetWorkerToggleFunc(toggle)
	}
	return a
}

func TestHandleWorkerSetEnabled_EnableOK(t *testing.T) {
	t.Parallel()

	var gotName string
	var gotEnabled bool
	a := workerToggleFixture(func(name string, enabled bool) (string, error) {
		gotName, gotEnabled = name, enabled
		return name, nil
	})

	params := mustMarshal(t, map[string]any{"name": "gb-mbp", "enabled": true})
	res, rpcErr := a.HandleWorkerSetEnabled(context.Background(), params)
	if rpcErr != nil {
		t.Fatalf("HandleWorkerSetEnabled: unexpected RPCError %+v", rpcErr)
	}
	if gotName != "gb-mbp" || gotEnabled != true {
		t.Fatalf("toggle called with (%q,%v), want (gb-mbp,true)", gotName, gotEnabled)
	}
	var resp struct {
		Name    string `json:"name"`
		Enabled bool   `json:"enabled"`
	}
	if err := json.Unmarshal(res, &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Name != "gb-mbp" || !resp.Enabled {
		t.Fatalf("response = %+v, want {gb-mbp true}", resp)
	}
}

func TestHandleWorkerSetEnabled_DisableOK(t *testing.T) {
	t.Parallel()

	a := workerToggleFixture(func(name string, enabled bool) (string, error) {
		return name, nil
	})
	params := mustMarshal(t, map[string]any{"name": "gb-mbp", "enabled": false})
	res, rpcErr := a.HandleWorkerSetEnabled(context.Background(), params)
	if rpcErr != nil {
		t.Fatalf("HandleWorkerSetEnabled: unexpected RPCError %+v", rpcErr)
	}
	var resp struct {
		Name    string `json:"name"`
		Enabled bool   `json:"enabled"`
	}
	if err := json.Unmarshal(res, &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Name != "gb-mbp" || resp.Enabled {
		t.Fatalf("response = %+v, want {gb-mbp false}", resp)
	}
}

func TestHandleWorkerSetEnabled_UnknownName(t *testing.T) {
	t.Parallel()

	a := workerToggleFixture(func(name string, enabled bool) (string, error) {
		return "", fmt.Errorf("no such worker %q (configured worker is %q)", name, "gb-mbp")
	})
	params := mustMarshal(t, map[string]any{"name": "ghost", "enabled": true})
	_, rpcErr := a.HandleWorkerSetEnabled(context.Background(), params)
	if rpcErr == nil {
		t.Fatal("HandleWorkerSetEnabled(unknown name): expected an RPCError, got nil")
	}
	if rpcErr.Message != "invalid_worker" {
		t.Fatalf("RPCError.Message = %q, want %q", rpcErr.Message, "invalid_worker")
	}
}

func TestHandleWorkerSetEnabled_NotWired(t *testing.T) {
	t.Parallel()

	a := workerToggleFixture(nil) // no toggle func → no worker registry
	params := mustMarshal(t, map[string]any{"name": "gb-mbp", "enabled": true})
	_, rpcErr := a.HandleWorkerSetEnabled(context.Background(), params)
	if rpcErr == nil {
		t.Fatal("HandleWorkerSetEnabled(not wired): expected an RPCError, got nil")
	}
	if rpcErr.Code != -32099 {
		t.Fatalf("RPCError.Code = %d, want -32099 (no worker registry wired)", rpcErr.Code)
	}
}

func TestHandleWorkerSetEnabled_MissingName(t *testing.T) {
	t.Parallel()

	called := false
	a := workerToggleFixture(func(name string, enabled bool) (string, error) {
		called = true
		return name, nil
	})
	params := mustMarshal(t, map[string]any{"name": "", "enabled": true})
	_, rpcErr := a.HandleWorkerSetEnabled(context.Background(), params)
	if rpcErr == nil {
		t.Fatal("HandleWorkerSetEnabled(empty name): expected an RPCError, got nil")
	}
	if rpcErr.Message != "invalid_worker" {
		t.Fatalf("RPCError.Message = %q, want %q", rpcErr.Message, "invalid_worker")
	}
	if called {
		t.Fatal("toggle func must NOT be called for an empty worker name")
	}
}
