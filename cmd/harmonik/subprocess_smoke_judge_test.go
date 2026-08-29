package main

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"testing"
)

//go:embed testdata/subprocess-smoke/*.events.jsonl
var subprocessSmokeFixtures embed.FS

type subprocessSmokeRun struct {
	RunID    string
	Nodes    map[string]bool
	Terminal string
	Success  bool
	Summary  string
}

func scanSubprocessSmokeEvents(jsonl []byte, beadID string) (subprocessSmokeRun, error) {
	run := subprocessSmokeRun{Nodes: make(map[string]bool)}
	lines := bytes.Split(jsonl, []byte("\n"))
	for index, line := range lines {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var event struct {
			Type    string `json:"type"`
			Payload struct {
				BeadID  string `json:"bead_id"`
				RunID   string `json:"run_id"`
				NodeID  string `json:"node_id"`
				Success bool   `json:"success"`
				Summary string `json:"summary"`
			} `json:"payload"`
		}
		if err := json.Unmarshal(line, &event); err != nil {
			if index == len(lines)-1 {
				continue
			}
			return subprocessSmokeRun{}, fmt.Errorf("decode event line %d: %w", index+1, err)
		}
		if run.RunID == "" {
			if event.Type == "run_started" && event.Payload.BeadID == beadID {
				run.RunID = event.Payload.RunID
			}
			continue
		}
		if event.Payload.RunID != run.RunID {
			continue
		}
		switch event.Type {
		case "node_dispatch_requested":
			run.Nodes[event.Payload.NodeID] = true
		case "run_completed", "run_failed":
			run.Terminal = event.Type
			run.Success = event.Payload.Success
			run.Summary = event.Payload.Summary
		}
	}
	return run, nil
}

// TestScanSubprocessSmokeEvents checks the judgment used by the live subprocess
// test. crashed-run.events.jsonl was captured on 2026-08-24 from commit
// 9f6723bac8e037a0843051de5170f6447b55cac2.
func TestScanSubprocessSmokeEvents(t *testing.T) {
	tests := []struct {
		name         string
		fixture      string
		beadID       string
		wantTerminal string
		wantSuccess  bool
		wantNode     string
	}{
		{name: "crashed run", fixture: "crashed-run.events.jsonl", beadID: "sub-iio", wantTerminal: "run_failed", wantNode: "implement"},
		{name: "completed run", fixture: "completed-run.events.jsonl", wantTerminal: "run_completed", wantSuccess: true, wantNode: "implement"},
		{name: "summary quotes completed", fixture: "summary-quotes-completed.events.jsonl", wantTerminal: "run_failed"},
		{name: "other run terminal", fixture: "other-run-terminal.events.jsonl", wantTerminal: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.beadID == "" {
				test.beadID = "sub-fixture"
			}
			path := "testdata/subprocess-smoke/" + test.fixture
			data, err := subprocessSmokeFixtures.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			run, err := scanSubprocessSmokeEvents(data, test.beadID)
			if err != nil {
				t.Fatalf("scan %s: %v", path, err)
			}
			if run.Terminal != test.wantTerminal {
				t.Errorf("Terminal = %q, want %q", run.Terminal, test.wantTerminal)
			}
			if run.Success != test.wantSuccess {
				t.Errorf("Success = %t, want %t", run.Success, test.wantSuccess)
			}
			if test.wantNode != "" && !run.Nodes[test.wantNode] {
				t.Errorf("Nodes[%q] = false, want true", test.wantNode)
			}
		})
	}
}

func TestScanSubprocessSmokeEvents_RejectsCorruptCompleteLine(t *testing.T) {
	jsonl := []byte("{\"type\":\"run_started\",\"payload\":{\"bead_id\":\"sub-fixture\",\"run_id\":\"run-1\"}}\nnot-json\n{}\n")
	if _, err := scanSubprocessSmokeEvents(jsonl, "sub-fixture"); err == nil {
		t.Fatal("scan corrupt complete line returned nil error")
	}
}

func TestScanSubprocessSmokeEvents_AllowsPartialFinalLine(t *testing.T) {
	jsonl := []byte("{\"type\":\"run_started\",\"payload\":{\"bead_id\":\"sub-fixture\",\"run_id\":\"run-1\"}}\n{\"type\":")
	run, err := scanSubprocessSmokeEvents(jsonl, "sub-fixture")
	if err != nil {
		t.Fatalf("scan partial final line: %v", err)
	}
	if run.RunID != "run-1" {
		t.Errorf("RunID = %q, want run-1", run.RunID)
	}
}
