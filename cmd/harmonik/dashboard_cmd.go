package main

// dashboard_cmd.go — `harmonik dashboard [--json]` CLI command (hk-2exz9).
//
// Emits a DashboardSnapshot joining: live StateSnapshot, captain-curated
// dashboard.json, lanes.json, open decisions, active stall signals, and
// windowed session-data.jsonl throughput.
//
// When the daemon is up: snapshot via "dashboard" socket RPC.
// When the daemon is down: no dashboard socket fallback (state only via disk).
//
// Spec ref: plans/2026-07-03-operator-dashboard/DESIGN.md §2.
// Bead ref: hk-2exz9.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

// runDashboardSubcommand implements `harmonik dashboard [--json]`.
//
// Exit codes:
//
//	0 — snapshot emitted successfully
//	1 — fatal error (flag parse, marshal failure, daemon not running)
func runDashboardSubcommand(args []string) int {
	asJSON := false
	for _, a := range args {
		switch a {
		case "--json", "-json":
			asJSON = true
		case "--help", "-h":
			fmt.Fprintf(os.Stderr, "Usage: harmonik dashboard [--json]\n")
			fmt.Fprintf(os.Stderr, "  --json   emit full DashboardSnapshot as JSON\n")
			return 0
		}
	}

	projectDir, err := resolveProjectDirForState()
	if err != nil {
		fmt.Fprintf(os.Stderr, "harmonik dashboard: project dir: %v\n", err)
		return 1
	}

	ctx := context.Background()

	if !isDaemonUp(projectDir) {
		fmt.Fprintf(os.Stderr, "harmonik dashboard: daemon is not running; no dashboard fallback available\n")
		return 1
	}

	snap, err := dashboardViaSocket(ctx, projectDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "harmonik dashboard: socket RPC failed: %v\n", err)
		return 1
	}

	if asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if encErr := enc.Encode(snap); encErr != nil {
			fmt.Fprintf(os.Stderr, "harmonik dashboard: marshal: %v\n", encErr)
			return 1
		}
		return 0
	}

	printDashboardHuman(snap)
	return 0
}

// dashboardViaSocket sends a "dashboard" RPC to the daemon and decodes the snapshot.
func dashboardViaSocket(ctx context.Context, projectDir string) (daemon.DashboardSnapshot, error) {
	harmonikDir := filepath.Join(projectDir, ".harmonik")
	payload, err := json.Marshal(map[string]string{"op": "dashboard"})
	if err != nil {
		return daemon.DashboardSnapshot{}, fmt.Errorf("marshal request: %w", err)
	}

	resp, exitCode := viaSendRequest(ctx, harmonikDir, payload)
	if exitCode == exitViaDaemonDown {
		return daemon.DashboardSnapshot{}, fmt.Errorf("daemon socket absent")
	}
	if exitCode != 0 {
		return daemon.DashboardSnapshot{}, fmt.Errorf("socket RPC error (exit %d)", exitCode)
	}
	if !resp.Ok {
		return daemon.DashboardSnapshot{}, fmt.Errorf("daemon returned error: %s", resp.Error)
	}

	var snap daemon.DashboardSnapshot
	if err := json.Unmarshal(resp.Result, &snap); err != nil {
		return daemon.DashboardSnapshot{}, fmt.Errorf("decode snapshot: %w", err)
	}
	return snap, nil
}

// printDashboardHuman renders a compact operator panel.
func printDashboardHuman(snap daemon.DashboardSnapshot) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	defer w.Flush() //nolint:errcheck

	fmt.Fprintf(w, "dashboard\tcaptured %s\n", snap.CapturedAt)
	fmt.Fprintf(w, "activity\t%s\n", string(snap.State.ActivityLabel))

	// Priorities from captain-curated dashboard.json.
	if snap.Config != nil && len(snap.Config.PrioritiesCurrent) > 0 {
		fmt.Fprintf(w, "\npriorities (current)\t\n")
		for _, p := range snap.Config.PrioritiesCurrent {
			crew := p.Crew
			if crew == "" {
				crew = "-"
			}
			fmt.Fprintf(w, "  #%d %s\tcrew=%s  %s\n", p.Rank, p.Lane, crew, p.Headline)
		}
	}
	if snap.Config != nil && len(snap.Config.PrioritiesFuture) > 0 {
		fmt.Fprintf(w, "\npriorities (on-deck)\t\n")
		for _, p := range snap.Config.PrioritiesFuture {
			fmt.Fprintf(w, "  %s\t%s\n", p.Lane, p.Headline)
		}
	}

	// Crew-lane table with health.
	if len(snap.Lanes) > 0 {
		active := filterLanesByStatus(snap.Lanes, "active")
		if len(active) > 0 {
			fmt.Fprintf(w, "\ncrew↔lane (active)\t\n")
			for _, l := range active {
				crew := l.Crew
				if crew == "" {
					crew = "-"
				}
				health := laneHealth(l, snap)
				fmt.Fprintf(w, "  %s\tcrew=%-12s queue=%-14s %s\n", l.Lane, crew, nvl(l.Queue), health)
			}
		}
	}

	// Expected-vs-actual throughput.
	if snap.Config != nil && len(snap.Config.ThroughputExpected) > 0 {
		fmt.Fprintf(w, "\nthroughput expected\t\n")
		for _, te := range snap.Config.ThroughputExpected {
			actual := throughputActualForLane(te.Lane, snap.Throughput)
			byStr := ""
			if te.By != "" {
				if t, err := time.Parse(time.RFC3339, te.By); err == nil {
					byStr = " by " + t.Format("15:04Z")
				}
			}
			fmt.Fprintf(w, "  %s\texpected=%d%s actual=%s\n",
				te.Lane, te.BeadsExpected, byStr, actual)
		}
	}

	// Active stall signals.
	if len(snap.ActiveStalls) > 0 {
		fmt.Fprintf(w, "\nbottlenecks (%d)\t\n", len(snap.ActiveStalls))
		for _, s := range snap.ActiveStalls {
			fmt.Fprintf(w, "  %s\tbead=%s sig=%s elapsed=%ds\n",
				s.RunID, s.BeadID, s.Signature, s.ElapsedMs/1000)
		}
	}

	// Operator mailbox: decisions raised with topic=operator-mailbox (bead
	// hk-pltjs, pending operator sign-off). The open-item set + its count (the
	// unread count), sorted most-urgent first.
	mailbox := filterDecisionsByTopic(snap.OpenDecisions, core.DecisionTopicOperatorMailbox)
	if len(mailbox) > 0 {
		fmt.Fprintf(w, "\nmailbox (%d unread)\t\n", len(mailbox))
		for _, d := range mailbox {
			from := d.BlockedAgent
			if from == "" {
				from = "unknown"
			}
			q := d.Question
			if len(q) > 60 {
				q = q[:57] + "..."
			}
			urgency := d.Urgency
			if urgency == "" {
				urgency = "-"
			}
			fmt.Fprintf(w, "  %s\t[%s] from=%s  %s\n", d.DecisionID[:8], urgency, from, q)
		}
	}

	// Notes from dashboard.json.
	if snap.Config != nil && snap.Config.Notes != "" {
		fmt.Fprintf(w, "\nnotes\t%s\n", strings.ReplaceAll(snap.Config.Notes, "\n", " "))
	}
}

// filterDecisionsByTopic returns the decisions matching topic, sorted
// most-urgent first (blocker, question, fyi, then unspecified), with
// decision_id as the stable tiebreaker.
func filterDecisionsByTopic(decisions []daemon.DashDecision, topic string) []daemon.DashDecision {
	var out []daemon.DashDecision
	for _, d := range decisions {
		if d.Topic == topic {
			out = append(out, d)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		ri, rj := mailboxUrgencyRank(out[i].Urgency), mailboxUrgencyRank(out[j].Urgency)
		if ri != rj {
			return ri < rj
		}
		return out[i].DecisionID < out[j].DecisionID
	})
	return out
}

// mailboxUrgencyRank orders urgency values for display: blocker first, then
// question, then fyi, then unspecified last.
func mailboxUrgencyRank(u string) int {
	switch core.DecisionUrgency(u) {
	case core.DecisionUrgencyBlocker:
		return 0
	case core.DecisionUrgencyQuestion:
		return 1
	case core.DecisionUrgencyFYI:
		return 2
	default:
		return 3
	}
}

func filterLanesByStatus(lanes []daemon.DashLane, status string) []daemon.DashLane {
	var out []daemon.DashLane
	for _, l := range lanes {
		if l.Status == status {
			out = append(out, l)
		}
	}
	return out
}

func laneHealth(l daemon.DashLane, snap daemon.DashboardSnapshot) string {
	// Check if the crew has a live session.
	if l.Crew == "" {
		return "unstaffed"
	}
	for _, s := range snap.State.Sessions {
		if s.Agent == l.Crew {
			if !s.Alive {
				return "dead"
			}
			if s.AtRest {
				return "sleeping"
			}
			if s.Cognition != nil {
				return fmt.Sprintf("alive fill=%.0f%%", s.Cognition.Context.FillFrac*100)
			}
			return "alive"
		}
	}
	return "absent"
}

func throughputActualForLane(lane string, tp *daemon.DashThroughput) string {
	if tp == nil || !tp.Available {
		return "unavailable"
	}
	for _, lt := range tp.ByLane {
		if lt.Lane == lane {
			return fmt.Sprintf("%d beads", lt.BeadsClosed)
		}
	}
	return "0 beads"
}

func nvl(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
