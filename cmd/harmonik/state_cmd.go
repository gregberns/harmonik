package main

// state_cmd.go — `harmonik state [--json]` CLI command (hk-gv04 P2-a).
//
// Emits a typed StateSnapshot aggregating: supervise status, keeper .ctx/.sid,
// tmux probe, QueueStore.AllQueues, crew.List, RunRegistry.Snapshot, and
// GatherDrainFacts. When the daemon is running the snapshot comes from a
// live socket RPC; when not, it falls back to disk reads (read_quality.unsure).
//
// Spec ref: specs/system-state.md §4 (SS-001..SS-015).
// Bead ref: hk-gv04 (P2-a: harmonik state aggregator command).

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"text/tabwriter"

	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/lifecycle"
)

// runStateSubcommand implements `harmonik state [--json]`.
//
// Exit codes:
//
//	0 — snapshot emitted successfully (even if read_quality.unsure)
//	1 — fatal error (flag parse, marshal failure)
func runStateSubcommand(args []string) int {
	asJSON := false
	for _, a := range args {
		switch a {
		case "--json", "-json":
			asJSON = true
		case "--help", "-h":
			fmt.Fprintf(os.Stderr, "Usage: harmonik state [--json]\n")
			fmt.Fprintf(os.Stderr, "  --json   emit full StateSnapshot as JSON\n")
			return 0
		}
	}

	projectDir, err := resolveProjectDirForState()
	if err != nil {
		fmt.Fprintf(os.Stderr, "harmonik state: project dir: %v\n", err)
		return 1
	}

	ctx := context.Background()
	var snap daemon.StateSnapshot

	if isDaemonUp(projectDir) {
		snap, err = stateViaSocket(ctx, projectDir)
		if err != nil {
			// Non-fatal: fall back to disk.
			fmt.Fprintf(os.Stderr, "harmonik state: socket RPC failed (%v); falling back to disk\n", err)
			snap = daemon.BuildDiskSnapshot(ctx, projectDir)
		}
	} else {
		snap = daemon.BuildDiskSnapshot(ctx, projectDir)
	}

	if asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if encErr := enc.Encode(snap); encErr != nil {
			fmt.Fprintf(os.Stderr, "harmonik state: marshal: %v\n", encErr)
			return 1
		}
		return 0
	}

	if err := printStateHuman(snap); err != nil {
		fmt.Fprintf(os.Stderr, "harmonik state: %v\n", err)
		return 1
	}
	return 0
}

// stateViaSocket sends a "state" RPC to the daemon and returns the decoded snapshot.
func stateViaSocket(ctx context.Context, projectDir string) (daemon.StateSnapshot, error) {
	harmonikDir := filepath.Join(projectDir, ".harmonik")
	payload, err := json.Marshal(map[string]string{"op": "state"})
	if err != nil {
		return daemon.StateSnapshot{}, fmt.Errorf("marshal request: %w", err)
	}

	resp, exitCode := viaSendRequest(ctx, harmonikDir, payload)
	if exitCode == exitViaDaemonDown {
		return daemon.StateSnapshot{}, fmt.Errorf("daemon socket absent")
	}
	if exitCode != 0 {
		return daemon.StateSnapshot{}, fmt.Errorf("socket RPC error (exit %d)", exitCode)
	}
	if !resp.Ok {
		return daemon.StateSnapshot{}, fmt.Errorf("daemon returned error: %s", resp.Error)
	}

	var snap daemon.StateSnapshot
	if err := json.Unmarshal(resp.Result, &snap); err != nil {
		return daemon.StateSnapshot{}, fmt.Errorf("decode snapshot: %w", err)
	}
	return snap, nil
}

// resolveProjectDirForState returns the project root for the current working
// directory, using the same logic as other harmonik subcommands.
func resolveProjectDirForState() (string, error) {
	projectDir := os.Getenv("HK_PROJECT")
	if projectDir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("getwd: %w", err)
		}
		projectDir = findProjectRoot(cwd)
		if projectDir == "" {
			projectDir = cwd
		}
	}
	return projectDir, nil
}

// printStateHuman renders a compact human-readable summary of the snapshot.
// The tabwriter buffers every row until Flush, so a Flush failure means the
// operator saw nothing at all — it is returned rather than dropped.
func printStateHuman(snap daemon.StateSnapshot) error {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)

	daemonStatus := "down"
	if snap.Daemon.Up {
		daemonStatus = fmt.Sprintf("up (pid %d)", snap.Daemon.Pid)
	}
	if err := writeStateRow(w, "daemon\t%s\n", daemonStatus); err != nil {
		return err
	}
	if err := writeStateRow(w, "activity\t%s\n", string(snap.ActivityLabel)); err != nil {
		return err
	}
	if err := writeStateRow(w, "captured_at\t%s\n", snap.CapturedAt); err != nil {
		return err
	}

	if !snap.ReadQuality.Ok {
		if err := writeStateRow(w, "read_quality\tunsure\n"); err != nil {
			return err
		}
		for _, r := range snap.ReadQuality.Reasons {
			if err := writeStateRow(w, "  reason\t%s\n", r); err != nil {
				return err
			}
		}
	}

	if len(snap.Runs) > 0 {
		if err := writeStateRow(w, "\nruns (%d)\t\n", len(snap.Runs)); err != nil {
			return err
		}
		for _, r := range snap.Runs {
			if err := writeStateRow(w, "  %s\tbead=%s queue=%s state=%s\n", r.RunID, r.BeadID, r.QueueName, r.LifecycleState); err != nil {
				return err
			}
		}
	}

	if len(snap.Queues) > 0 {
		if err := writeStateRow(w, "\nqueues (%d)\t\n", len(snap.Queues)); err != nil {
			return err
		}
		for _, q := range snap.Queues {
			eligible := ""
			if q.EligibleNow {
				eligible = " [eligible]"
			}
			if err := writeStateRow(w, "  %s\tstatus=%s items=%d active=%d cap=%d%s\n",
				q.Name, q.Status, q.ItemCount, q.ActiveCount, q.EffectiveWorkerCap, eligible); err != nil {
				return err
			}
		}
	}

	if len(snap.Sessions) > 0 {
		if err := writeStateRow(w, "\nsessions (%d)\t\n", len(snap.Sessions)); err != nil {
			return err
		}
		for _, s := range snap.Sessions {
			status := "alive"
			if !s.Alive {
				status = "dead"
			}
			if s.AtRest {
				status = "sleeping"
			}
			ctx := ""
			if s.Cognition != nil {
				ctx = fmt.Sprintf(" fill=%.1f%%", s.Cognition.Context.FillFrac*100)
			}
			if err := writeStateRow(w, "  %s\t%s%s\n", s.Agent, status, ctx); err != nil {
				return err
			}
		}
	}

	if err := w.Flush(); err != nil {
		return fmt.Errorf("write state summary: %w", err)
	}
	return nil
}

func writeStateRow(w io.Writer, format string, args ...any) error {
	if _, err := fmt.Fprintf(w, format, args...); err != nil {
		return fmt.Errorf("write state summary: %w", err)
	}
	return nil
}

// findProjectRoot walks up from dir looking for a .harmonik directory.
// Returns "" if none found within 10 hops.
func findProjectRoot(dir string) string {
	for i := 0; i < 10; i++ {
		if _, err := os.Stat(filepath.Join(dir, ".harmonik")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

// ensure lifecycle import is used (SocketPath used via isDaemonUp).
var _ = lifecycle.SocketPath
