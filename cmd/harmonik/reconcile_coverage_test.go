package main

// reconcile_coverage_test.go — behavior tests for the pure-logic slices of
// `harmonik reconcile`: the usage renderer, the flag/arg validation exit-code
// truth table (paths that return before any `br` / git shell-out), and the
// filterBeadsByRunID queue-scoping helper driven against a temp project.
//
// Paths that require a live `br` binary or git-log scan are intentionally NOT
// exercised here — see the report for the skipped-with-reason list.

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/queue"
)

func TestReconcileUsage_RendersFlagsAndExitCodes(t *testing.T) {
	var buf bytes.Buffer
	if err := reconcileUsage(&buf); err != nil {
		t.Fatalf("reconcileUsage: %v", err)
	}
	out := buf.String()
	for _, want := range []string{
		"harmonik reconcile",
		"--project DIR",
		"--target-branch BRANCH",
		"--run RUN_ID",
		"EXIT CODES",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("usage missing %q; full output:\n%s", want, out)
		}
	}
}

// TestRunReconcileSubcommandIO_ParseExitCodes covers the argument/validation
// truth table for every path that returns before the `br` lookup or git scan.
func TestRunReconcileSubcommandIO_ParseExitCodes(t *testing.T) {
	// A directory guaranteed not to exist, to drive the os.Stat failure branch.
	missingDir := filepath.Join(t.TempDir(), "does-not-exist")

	tests := []struct {
		name string
		args []string
		want int
	}{
		{"help long", []string{"--help"}, 0},
		{"help short", []string{"-h"}, 0},
		{"unknown flag", []string{"--bogus"}, 1},
		{"unexpected positional", []string{"stray-arg"}, 1},
		{"nonexistent project", []string{"--project", missingDir}, 1},
		{"nonexistent project equals-form", []string{"--project=" + missingDir}, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var stdout bytes.Buffer
			if got := runReconcileSubcommandIO(tc.args, &stdout); got != tc.want {
				t.Errorf("runReconcileSubcommandIO(%v) = %d, want %d", tc.args, got, tc.want)
			}
			if tc.want == 0 && !strings.Contains(stdout.String(), "harmonik reconcile") {
				t.Errorf("help path should write usage to the stdout writer; got:\n%s", stdout.String())
			}
		})
	}
}

// seedRunIDQueue writes a minimal main.json queue with the given (beadID→runID)
// item pairs so filterBeadsByRunID has a real ledger to scope against.
func seedRunIDQueue(t *testing.T, projectDir string, items map[string]string) {
	t.Helper()
	qDir := filepath.Join(projectDir, ".harmonik", "queues")
	if err := os.MkdirAll(qDir, 0o750); err != nil {
		t.Fatalf("mkdir queues: %v", err)
	}
	var qItems []queue.Item
	for beadID, runID := range items {
		rid := runID
		qItems = append(qItems, queue.Item{BeadID: core.BeadID(beadID), RunID: &rid})
	}
	q := queue.Queue{
		SchemaVersion: 1,
		Name:          queue.QueueNameMain,
		Groups:        []queue.Group{{GroupIndex: 0, Items: qItems}},
	}
	data, err := json.Marshal(q)
	if err != nil {
		t.Fatalf("marshal queue: %v", err)
	}
	if err := os.WriteFile(filepath.Join(qDir, "main.json"), data, 0o600); err != nil {
		t.Fatalf("write main.json: %v", err)
	}
}

func TestFilterBeadsByRunID(t *testing.T) {
	ctx := context.Background()
	beads := []core.BeadRecord{
		{BeadID: "hk-aaa"},
		{BeadID: "hk-bbb"},
		{BeadID: "hk-ccc"},
	}

	t.Run("queue absent fails open to full slice", func(t *testing.T) {
		dir := t.TempDir() // no .harmonik/queues/main.json
		got := filterBeadsByRunID(ctx, dir, "run-x", beads)
		if len(got) != len(beads) {
			t.Errorf("fail-open expected all %d beads, got %d", len(beads), len(got))
		}
	})

	t.Run("run_id matches a subset", func(t *testing.T) {
		dir := t.TempDir()
		seedRunIDQueue(t, dir, map[string]string{"hk-bbb": "run-match", "hk-ccc": "run-other"})
		got := filterBeadsByRunID(ctx, dir, "run-match", beads)
		if len(got) != 1 || got[0].BeadID != "hk-bbb" {
			t.Errorf("expected only hk-bbb, got %+v", got)
		}
	})

	t.Run("run_id present in queue but bead not in_progress set", func(t *testing.T) {
		dir := t.TempDir()
		// run-match maps to hk-zzz, which is NOT in the supplied in-progress beads.
		seedRunIDQueue(t, dir, map[string]string{"hk-zzz": "run-match"})
		got := filterBeadsByRunID(ctx, dir, "run-match", beads)
		if len(got) != 0 {
			t.Errorf("expected no intersection, got %+v", got)
		}
	})

	t.Run("run_id not found in queue returns nil", func(t *testing.T) {
		dir := t.TempDir()
		seedRunIDQueue(t, dir, map[string]string{"hk-bbb": "run-other"})
		got := filterBeadsByRunID(ctx, dir, "run-absent", beads)
		if got != nil {
			t.Errorf("expected nil for unmatched run_id, got %+v", got)
		}
	})
}
