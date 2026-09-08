package keeper

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gregberns/harmonik/internal/keeper/panehost/tmuxhost"
)

// HarmonikSessionName is a back-compat wrapper over tmuxhost.HarmonikSessionName
// (KH-1: the tmux naming convention moved to panehost/tmuxhost so a herdr
// adapter can share it; this thin forward keeps existing callers/tests
// compiling unchanged). See tmuxhost.HarmonikSessionName for the full doc.
func HarmonikSessionName(projectDir, agentName string) string {
	return tmuxhost.HarmonikSessionName(projectDir, agentName)
}

// HarmonikCrewSessionName is a back-compat wrapper over
// tmuxhost.HarmonikCrewSessionName. See tmuxhost.HarmonikCrewSessionName for
// the full doc.
func HarmonikCrewSessionName(projectDir, agentName string) string {
	return tmuxhost.HarmonikCrewSessionName(projectDir, agentName)
}

// SplitTmuxTarget is a back-compat wrapper over tmuxhost.SplitTmuxTarget.
func SplitTmuxTarget(value string) (session, window string) {
	return tmuxhost.SplitTmuxTarget(value)
}

// ResolveTmuxTarget is a back-compat wrapper over tmuxhost.ResolveTmuxTarget.
// See tmuxhost.ResolveTmuxTarget for the full doc.
func ResolveTmuxTarget(projectDir, agentName, explicit string, sessionExistsFn func(string) bool) string {
	return tmuxhost.ResolveTmuxTarget(projectDir, agentName, explicit, sessionExistsFn)
}

// OperatorAttached is a back-compat wrapper over tmuxhost.OperatorAttached.
// See tmuxhost.OperatorAttached for the full doc.
func OperatorAttached(target string) bool {
	return tmuxhost.OperatorAttached(target)
}

const recentTranscriptTailBytes = 256 * 1024

func recentTranscriptTurn(transcriptDir, sessionID, role string) (time.Time, bool) {
	if transcriptDir == "" || sessionID == "" {
		return time.Time{}, false
	}
	path := filepath.Join(transcriptDir, sessionID+".jsonl")
	//nolint:gosec // G304: transcriptDir derived from operator-controlled projectDir; sessionID is a latched UUID
	f, err := os.Open(path)
	if err != nil {
		return time.Time{}, false
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			slog.WarnContext(context.Background(), "keeper: close transcript while finding recent turn", "err", closeErr, "path", path)
		}
	}()

	partialStart := false
	size, seekErr := f.Seek(0, io.SeekEnd)
	if seekErr == nil && size > recentTranscriptTailBytes {
		if _, err2 := f.Seek(size-recentTranscriptTailBytes, io.SeekStart); err2 == nil {
			partialStart = true
		}
	}
	if !partialStart {
		if _, err2 := f.Seek(0, io.SeekStart); err2 != nil {
			return time.Time{}, false
		}
	}

	type transcriptEntry struct {
		Type      string          `json:"type"`
		Timestamp string          `json:"timestamp"`
		Message   json.RawMessage `json:"message"`
	}
	type transcriptMessage struct {
		Content json.RawMessage `json:"content"`
	}

	var (
		lastTs time.Time
		found  bool
	)
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	if partialStart {
		sc.Scan() // discard the partial first line
	}
	for sc.Scan() {
		raw := sc.Bytes()
		if len(raw) == 0 {
			continue
		}
		var e transcriptEntry
		if err := json.Unmarshal(raw, &e); err != nil || e.Type != role || e.Timestamp == "" {
			continue
		}
		var msg transcriptMessage
		if err := json.Unmarshal(e.Message, &msg); err != nil {
			continue
		}
		if !isRealTranscriptTurn(role, msg.Content) {
			continue
		}
		ts, parseErr := time.Parse(time.RFC3339Nano, e.Timestamp)
		if parseErr != nil {
			ts, parseErr = time.Parse(time.RFC3339, e.Timestamp)
			if parseErr != nil {
				continue
			}
		}
		lastTs = ts
		found = true
	}
	if scanErr := sc.Err(); scanErr != nil {
		slog.WarnContext(context.Background(), "keeper: recentTranscriptTurn: transcript scan truncated (over-long line)",
			"path", path, "role", role, "err", scanErr)
	}
	return lastTs, found
}

func isRealTranscriptTurn(role string, content json.RawMessage) bool {
	if len(content) == 0 {
		return role == "user"
	}
	if content[0] == '"' {
		var text string
		if json.Unmarshal(content, &text) != nil {
			return role == "user"
		}
		return role != "user" || isOperatorText(text)
	}
	if content[0] != '[' {
		return role == "user" // unknown format → conservative
	}
	type contentItem struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	var items []contentItem
	if err := json.Unmarshal(content, &items); err != nil {
		return role == "user"
	}
	switch role {
	case "user":
		for _, it := range items {
			if it.Type == "text" && isOperatorText(it.Text) {
				return true
			}
			if it.Type != "text" && it.Type != "tool_result" {
				return true // image or other inbound operator content
			}
		}
		return false
	case "assistant":
		for _, it := range items {
			if it.Type == "text" {
				return true // real text response to operator
			}
		}
		return false
	default:
		return false
	}
}

func isOperatorText(text string) bool {
	text = strings.TrimSpace(text)
	if strings.HasPrefix(text, AutomationEnvelopePrefix) {
		return false
	}
	return !strings.HasPrefix(text, "<command-name>/session-handoff</command-name>") &&
		!strings.HasPrefix(text, "/session-handoff ")
}
