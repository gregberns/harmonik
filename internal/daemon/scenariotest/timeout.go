package scenariotest

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	tmuxPkg "github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

var daemonLogPatterns = []string{
	"pasteinject quit-on-commit",
	"context cancelled during reviewer wait",
	"non_ff_merge",
}

// MustCompleteWithin runs body in a goroutine. If body returns within deadline
// the function returns normally. If deadline expires before body returns, it
// dumps diagnostics via t.Log and calls t.Fatal.
//
// On timeout the diagnostic dump includes:
//   - last 200 lines of events.jsonl at jsonlPath
//   - tmux windows matching "hk-*" (skipped when adapter is nil)
//   - grep of daemonLog for recovery-path signatures (skipped when daemonLog is "")
//
// Bead: hk-8uy6m.
func MustCompleteWithin(
	t *testing.T,
	jsonlPath string,
	daemonLog string,
	adapter tmuxPkg.Adapter,
	deadline time.Duration,
	body func(),
) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		body()
	}()

	select {
	case <-done:
		return
	case <-time.After(deadline):
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "MustCompleteWithin: timed out after %s\n\n", deadline)

	fmt.Fprintf(&sb, "=== events.jsonl (last 200 lines: %s) ===\n", jsonlPath)
	if tail := tailLines(jsonlPath, 200); tail != "" {
		sb.WriteString(tail)
	} else {
		sb.WriteString("(empty or not found)\n")
	}
	sb.WriteByte('\n')

	if adapter != nil {
		sb.WriteString("=== tmux windows matching hk-* ===\n")
		appendTmuxWindows(&sb, adapter)
		sb.WriteByte('\n')
	}

	if daemonLog != "" {
		fmt.Fprintf(&sb, "=== daemon.log recovery signatures (%s) ===\n", daemonLog)
		appendDaemonLogMatches(&sb, daemonLog)
		sb.WriteByte('\n')
	}

	t.Fatal(sb.String())
}

func tailLines(path string, n int) string {
	//nolint:gosec // G304: path is t.TempDir()-based or test-config; not user input
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			slog.WarnContext(context.Background(), "scenariotest: tailLines: close file", "err", closeErr, "path", path)
		}
	}()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
		if len(lines) > n {
			lines = lines[1:]
		}
	}
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}

func appendTmuxWindows(sb *strings.Builder, adapter tmuxPkg.Adapter) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	sessions, err := adapter.ListSessions(ctx)
	if err != nil {
		fmt.Fprintf(sb, "(ListSessions error: %v)\n", err)
		return
	}
	found := false
	for _, sess := range sessions {
		windows, wErr := adapter.ListWindows(ctx, sess)
		if wErr != nil {
			fmt.Fprintf(sb, "(ListWindows(%q) error: %v)\n", sess, wErr)
			continue
		}
		for _, w := range windows {
			if strings.HasPrefix(w, "hk-") {
				fmt.Fprintf(sb, "  session=%s window=%s\n", sess, w)
				found = true
			}
		}
	}
	if !found {
		sb.WriteString("(no hk-* windows found)\n")
	}
}

func appendDaemonLogMatches(sb *strings.Builder, daemonLog string) {
	//nolint:gosec // G304: path is test-config; not user input
	f, err := os.Open(daemonLog)
	if err != nil {
		fmt.Fprintf(sb, "(cannot open %s: %v)\n", daemonLog, err)
		return
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			slog.WarnContext(context.Background(), "scenariotest: appendDaemonLogMatches: close file", "err", closeErr, "path", daemonLog)
		}
	}()

	scanner := bufio.NewScanner(f)
	found := false
	for scanner.Scan() {
		line := scanner.Text()
		for _, pat := range daemonLogPatterns {
			if strings.Contains(line, pat) {
				fmt.Fprintf(sb, "  [%s] %s\n", pat, line)
				found = true
				break
			}
		}
	}
	if !found {
		sb.WriteString("(no recovery signatures found)\n")
	}
}
