package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/dashboard"
)

const defaultDashboardUnlockDuration = 1 * time.Hour

func runDashboardSubcommand(args []string) int {
	asJSON := false
	doUnlock := false
	doLock := false
	until := defaultDashboardUnlockDuration
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch a {
		case "--json", "-json":
			asJSON = true
		case "--unlock":
			doUnlock = true
		case "--lock":
			doLock = true
		case "--until":
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "harmonik dashboard: --until requires a duration argument (e.g. --until 1h)\n")
				return 1
			}
			i++
			d, parseErr := time.ParseDuration(args[i])
			if parseErr != nil {
				fmt.Fprintf(os.Stderr, "harmonik dashboard: --until %q: %v\n", args[i], parseErr)
				return 1
			}
			until = d
		case "--help", "-h":
			fmt.Fprintf(os.Stderr, "Usage: harmonik dashboard [--json] [--unlock [--until DURATION]] [--lock]\n")
			fmt.Fprintf(os.Stderr, "  --json    emit full DashboardSnapshot as JSON\n")
			fmt.Fprintf(os.Stderr, "  --unlock  bypass the staleness forcing gate for DURATION (default %s)\n", defaultDashboardUnlockDuration)
			fmt.Fprintf(os.Stderr, "  --until   duration for --unlock (e.g. 1h, 30m)\n")
			fmt.Fprintf(os.Stderr, "  --lock    clear an active --unlock override immediately\n")
			return 0
		}
	}

	projectDir, err := resolveProjectDirForState()
	if err != nil {
		fmt.Fprintf(os.Stderr, "harmonik dashboard: project dir: %v\n", err)
		return 1
	}

	if doUnlock {
		return runDashboardUnlock(projectDir, until)
	}
	if doLock {
		return runDashboardLock(projectDir)
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

	if err := printDashboardHuman(snap); err != nil {
		return 1
	}
	return 0
}

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

func printDashboardHuman(snap daemon.DashboardSnapshot) error {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)

	if err := writeDashboardRow(w, "dashboard\tcaptured %s\n", snap.CapturedAt); err != nil {
		return err
	}
	if err := writeDashboardRow(w, "activity\t%s\n", string(snap.State.ActivityLabel)); err != nil {
		return err
	}

	if snap.Config != nil && len(snap.Config.PrioritiesCurrent) > 0 {
		if err := writeDashboardRow(w, "\npriorities (current)\t\n"); err != nil {
			return err
		}
		for _, p := range snap.Config.PrioritiesCurrent {
			crew := p.Crew
			if crew == "" {
				crew = "-"
			}
			if err := writeDashboardRow(w, "  #%d %s\tcrew=%s  %s\n", p.Rank, p.Lane, crew, p.Headline); err != nil {
				return err
			}
		}
	}
	if snap.Config != nil && len(snap.Config.PrioritiesFuture) > 0 {
		if err := writeDashboardRow(w, "\npriorities (on-deck)\t\n"); err != nil {
			return err
		}
		for _, p := range snap.Config.PrioritiesFuture {
			if err := writeDashboardRow(w, "  %s\t%s\n", p.Lane, p.Headline); err != nil {
				return err
			}
		}
	}

	if len(snap.Lanes) > 0 {
		active := filterLanesByStatus(snap.Lanes, "active")
		if len(active) > 0 {
			if err := writeDashboardRow(w, "\ncrew↔lane (active)\t\n"); err != nil {
				return err
			}
			for _, l := range active {
				crew := l.Crew
				if crew == "" {
					crew = "-"
				}
				health := laneHealth(l, snap)
				if err := writeDashboardRow(w, "  %s\tcrew=%-12s queue=%-14s %s\n", l.Lane, crew, nvl(l.Queue), health); err != nil {
					return err
				}
			}
		}
	}

	if snap.Config != nil && len(snap.Config.ThroughputExpected) > 0 {
		if err := writeDashboardRow(w, "\nthroughput expected\t\n"); err != nil {
			return err
		}
		for _, te := range snap.Config.ThroughputExpected {
			actual := throughputActualForLane(te.Lane, snap.Throughput)
			byStr := ""
			if te.By != "" {
				if t, err := time.Parse(time.RFC3339, te.By); err == nil {
					byStr = " by " + t.Format("15:04Z")
				}
			}
			if err := writeDashboardRow(w, "  %s\texpected=%d%s actual=%s\n",
				te.Lane, te.BeadsExpected, byStr, actual); err != nil {
				return err
			}
		}
	}

	if len(snap.ActiveStalls) > 0 {
		if err := writeDashboardRow(w, "\nbottlenecks (%d)\t\n", len(snap.ActiveStalls)); err != nil {
			return err
		}
		for _, s := range snap.ActiveStalls {
			if err := writeDashboardRow(w, "  %s\tbead=%s sig=%s elapsed=%ds\n",
				s.RunID, s.BeadID, s.Signature, s.ElapsedMs/1000); err != nil {
				return err
			}
		}
	}

	mailbox := filterDecisionsByTopic(snap.OpenDecisions, core.DecisionTopicOperatorMailbox)
	if len(mailbox) > 0 {
		if err := writeDashboardRow(w, "\nmailbox (%d unread)\t\n", len(mailbox)); err != nil {
			return err
		}
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
			if err := writeDashboardRow(w, "  %s\t[%s] from=%s  %s\n", d.DecisionID[:8], urgency, from, q); err != nil {
				return err
			}
		}
	}

	if snap.Config != nil && snap.Config.Notes != "" {
		if err := writeDashboardRow(w, "\nnotes\t%s\n", strings.ReplaceAll(snap.Config.Notes, "\n", " ")); err != nil {
			return err
		}
	}
	if err := w.Flush(); err != nil {
		return fmt.Errorf("write dashboard summary: %w", err)
	}
	return nil
}

func writeDashboardRow(w io.Writer, format string, args ...any) error {
	if _, err := fmt.Fprintf(w, format, args...); err != nil {
		return fmt.Errorf("write dashboard summary: %w", err)
	}
	return nil
}

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

func runDashboardUnlock(projectDir string, until time.Duration) int {
	expiry := time.Now().Add(until)
	if err := dashboard.WriteUnlock(projectDir, expiry, "operator"); err != nil {
		fmt.Fprintf(os.Stderr, "harmonik dashboard: --unlock: %v\n", err)
		return 1
	}
	if _, writeErr := fmt.Fprintf(os.Stdout, "dashboard gate unlocked until %s\n", expiry.Format(time.RFC3339)); writeErr != nil {
		return 1
	}
	return 0
}

func runDashboardLock(projectDir string) int {
	if err := dashboard.ClearUnlock(projectDir); err != nil {
		fmt.Fprintf(os.Stderr, "harmonik dashboard: --lock: %v\n", err)
		return 1
	}
	if _, writeErr := fmt.Fprintf(os.Stdout, "dashboard gate re-armed (unlock override cleared)"); writeErr != nil {
		return 1
	}
	return 0
}
