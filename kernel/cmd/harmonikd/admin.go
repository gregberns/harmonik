package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/gregberns/harmonik/kernel/host"
)

// reloadRequest is the admin surface's body for a plugin reload. Path and
// SHA256 are optional: an empty body reloads the currently registered spec
// unchanged (a bare process restart); supplying both points the reload at a
// different binary, which is how a live "point at a new binary" reload is
// driven.
type reloadRequest struct {
	Namespace string `json:"namespace"`
	Path      string `json:"path,omitempty"`
	SHA256    string `json:"sha256,omitempty"`
}

// adminMux is the operator-facing surface this slice does not put on
// KernelService: plugin reload is a host-lifecycle action, not one of the
// contract's 19 kernel RPCs, so it gets its own small local endpoint instead
// of stretching the wire contract to carry it.
func (d *Daemon) adminMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/plugin/reload", d.handleReload)
	return mux
}

func (d *Daemon) handleReload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "harmonikd: reload requires POST", http.StatusMethodNotAllowed)
		return
	}

	var req reloadRequest
	if r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, fmt.Sprintf("harmonikd: decode reload body: %v", err), http.StatusBadRequest)
			return
		}
	}

	current := d.plugin.namespace()
	if req.Namespace != "" && req.Namespace != current {
		http.Error(w, fmt.Sprintf("harmonikd: no registered plugin named %q (registered: %q)", req.Namespace, current), http.StatusNotFound)
		return
	}

	spec := d.plugin.currentSpec()
	if req.Path != "" || req.SHA256 != "" {
		if req.Path == "" || req.SHA256 == "" {
			http.Error(w, "harmonikd: reload needs both path and sha256, or neither", http.StatusBadRequest)
			return
		}
		spec = host.LaunchSpec{
			Path:           req.Path,
			SHA256:         req.SHA256,
			Node:           spec.Node,
			KernelEndpoint: spec.KernelEndpoint,
			CallerID:       spec.CallerID,
			APIVersion:     spec.APIVersion,
		}
	}

	if err := d.plugin.reload(r.Context(), spec); err != nil {
		http.Error(w, fmt.Sprintf("harmonikd: %v", err), http.StatusInternalServerError)
		return
	}
	d.kernel.setNamespace(d.plugin.namespace())

	w.WriteHeader(http.StatusOK)
}

// requestReload is the client-side call the "plugin reload" verb makes.
func requestReload(ctx context.Context, adminAddr string, req reloadRequest) error {
	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("harmonikd: encode reload request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+adminAddr+"/plugin/reload", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("harmonikd: build reload request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("harmonikd: reload: %w", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			slog.ErrorContext(ctx, "harmonikd: close reload response body", "error", closeErr)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("harmonikd: reload: %w: status %s", errReloadFailed, resp.Status)
	}
	return nil
}

var errReloadFailed = errors.New("reload request failed")
