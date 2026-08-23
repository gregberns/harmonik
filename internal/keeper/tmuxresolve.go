package keeper

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// HarmonikSessionName returns the conventional tmux session name for a
// harmonik-managed agent: "harmonik-<hash12>-<agentName>", where hash12 is
// the first 12 hexadecimal characters of SHA-256(realpath(projectDir)).
//
// This mirrors lifecycle.TmuxSessionName but avoids importing the lifecycle
// package (depguard: keeper MUST only import $gostd, core, eventbus, and
// self per hk-ekap1 / hk-fzzc6).
//
// Spec ref: process-lifecycle.md §4.2 PL-006a — "harmonik-<project_hash>-<session_name>".
func HarmonikSessionName(projectDir, agentName string) string {
	resolved, err := filepath.EvalSymlinks(projectDir)
	if err != nil {
		resolved = projectDir
	}
	sum := sha256.Sum256([]byte(resolved))
	hash12 := fmt.Sprintf("%x", sum[:6])
	return "harmonik-" + hash12 + "-" + agentName
}

// HarmonikCrewSessionName returns the conventional tmux session name for a
// harmonik-managed CREW agent: "harmonik-<hash12>-crew-<agentName>".
//
// The lifecycle layer spawns crew sessions with a "crew-" infix
// (lifecycle.TmuxSessionName(hash, "crew-"+name)), so restart-now / ping must
// also try this form when the bare convention misses. Mirror:
// commsWakePaneCandidates in cmd/harmonik/comms.go (hk-y7v8/CE5), which already
// handles the crew-vs-captain naming asymmetry by trying both forms in order.
// B4 / hk-pp1in: the bare-only probe caused no_tmux_target for crew agents
// (e.g. admiral running in "harmonik-<hash>-crew-admiral").
func HarmonikCrewSessionName(projectDir, agentName string) string {
	resolved, err := filepath.EvalSymlinks(projectDir)
	if err != nil {
		resolved = projectDir
	}
	sum := sha256.Sum256([]byte(resolved))
	hash12 := fmt.Sprintf("%x", sum[:6])
	return "harmonik-" + hash12 + "-crew-" + agentName
}

const windowAgent = "agent"

// SplitTmuxTarget splits a --tmux value into its session and window components.
//
//   - "session:window" → (session, window) — the keeper targets the named
//     window's active pane (e.g. "harmonik-<hash>-captain:agent").
//   - "session"        → (session, "")     — no window; legacy session-active-pane
//     behavior, for back-compat with a half-migrated fleet.
//
// Only the FIRST colon separates session from window; any remaining colons stay
// in the window component, so a full tmux "session:window.pane" form round-trips
// (window = "window.pane"). An empty input yields ("", "").
func SplitTmuxTarget(value string) (session, window string) {
	if value == "" {
		return "", ""
	}
	if i := strings.IndexByte(value, ':'); i >= 0 {
		return value[:i], value[i+1:]
	}
	return value, ""
}

// ResolveTmuxTarget determines the effective tmux target for a keeper session.
//
// The returned target is what ALL keeper tmux operations use — keystroke
// injection (send-keys / paste-buffer), context-gauge liveness probes
// (capture-pane / display-message), and operator-attach detection
// (list-clients). tmux resolves a "session:window" target to that window's
// ACTIVE pane, which is exactly what the keeper needs so that a keeper running
// in its own sibling "keeper" window measures and injects into the AGENT
// window's pane, never itself (CONTRACT.md §Keeper inject-target contract).
//
// Priority:
//  1. explicit — if non-empty, returned as-is (caller-supplied --tmux flag).
//     A "session:window" value (e.g. "harmonik-<hash>-captain:agent") therefore
//     targets the named window's active pane; a bare "session" value keeps the
//     legacy session-active-pane behavior. The split rule lives in
//     SplitTmuxTarget; tmux itself honours the "session:window" target form, so
//     the explicit value passes through verbatim.
//  2. bare convention — derives "harmonik-<hash12>-<agentName>", verifies the
//     SESSION exists in tmux, and (when live) returns "<session>:agent" so the
//     gauge / inject path targets the AGENT window's pane. This is the canonical
//     form for the captain and non-crew agents.
//  3. crew convention — derives "harmonik-<hash12>-crew-<agentName>" and checks
//     that session. Crew agents (admiral, any named crew) are spawned with this
//     "crew-" infix by the lifecycle layer. B4 / hk-pp1in: the bare-only probe
//     caused restart-now to abort no_tmux_target for crew agents even when a
//     healthy watcher was bound to their crew-named pane. Mirrors the dual-probe
//     in commsWakePaneCandidates (cmd/harmonik/comms.go, hk-y7v8/CE5).
//  4. "" — no usable target; caller proceeds without tmux injection.
//
// sessionExistsFn may be nil, in which case a real tmux has-session check is
// performed. Inject a stub for unit tests.
func ResolveTmuxTarget(projectDir, agentName, explicit string, sessionExistsFn func(string) bool) string {
	if explicit != "" {
		return explicit
	}
	if agentName == "" || projectDir == "" {
		return ""
	}
	if sessionExistsFn == nil {
		sessionExistsFn = tmuxSessionLive
	}
	session := HarmonikSessionName(projectDir, agentName)
	if sessionExistsFn(session) {
		return session + ":" + windowAgent
	}
	crewSession := HarmonikCrewSessionName(projectDir, agentName)
	if sessionExistsFn(crewSession) {
		return crewSession + ":" + windowAgent
	}
	return ""
}

func tmuxSessionLive(sessionName string) bool {
	// context.Background() is appropriate: this is a synchronous, sub-second
	// liveness probe with no caller-supplied cancellation context (the public
	// ResolveTmuxTarget signature does not thread one through).
	//nolint:gosec // G204: sessionName is derived from projectDir (filepath-resolved) + validated agentName
	cmd := exec.CommandContext(context.Background(), "tmux", "has-session", "-t", "="+sessionName)
	return cmd.Run() == nil
}

const operatorActiveWindow = 5 * time.Minute

// OperatorAttached reports whether a human operator is ACTIVELY attached to the
// tmux session that owns target — i.e. some client has had keyboard activity
// within operatorActiveWindow. It runs
// `tmux list-clients -t <target> -F '#{client_activity}'` and reports true when
// any client's last-activity timestamp is recent.
//
// This is the production default for CyclerConfig.OperatorAttachedFn (hk-6qf):
// when an operator is actively typing into the pane, the keeper's reset-cycle
// injection would race the operator's keystrokes and could clobber an in-flight
// turn — so the cycle suppresses injection and falls back to warn-only.
//
// The previous probe counted ANY attached client, which permanently pinned the
// captain pane to warn-only under the operator's iOS / `claude --remote-control`
// workflow: a passive terminal stays attached while the operator drives via the
// remote-control channel, so a bare attach was an over-suppression (hk-0t5s,
// ~2265 false operator_attached suppressions). `#{client_activity}` advances
// only on keystrokes through that tmux client — not on pane output — so a
// remote-control / idle attach is reliably distinguishable from a live typist.
//
// A non-zero exit (the session does not exist, or no tmux server is running) is
// treated as NOT attached — fail-open — so a transient tmux error never
// permanently suppresses the reset cycle that protects against context
// exhaustion.
//
// target accepts any tmux target form (session name, "session:window.pane", or
// a "%pane_id"); tmux resolves it to the owning session for client listing.
func OperatorAttached(target string) bool {
	if target == "" {
		return false
	}
	cmd := exec.CommandContext(context.Background(), "tmux", "list-clients", "-t", target, "-F", "#{client_activity}")
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return operatorActiveSince(string(out), time.Now(), operatorActiveWindow)
}

func operatorActiveSince(listClientsOutput string, now time.Time, window time.Duration) bool {
	for _, line := range strings.Split(listClientsOutput, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		secs, err := strconv.ParseInt(line, 10, 64)
		if err != nil {
			continue
		}
		if now.Sub(time.Unix(secs, 0)) <= window {
			return true
		}
	}
	return false
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
