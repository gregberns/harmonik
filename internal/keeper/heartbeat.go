package keeper

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// heartbeat.go — keeper-side gauge liveness (hk-81wk).
//
// MaxHeartbeatMisses is the number of consecutive ticks on which
// deriveContextTokens may return false before the heartbeat stops writing the
// gauge file. At the default 10 s tick cadence, 12 misses ≈ 2 minutes — roughly
// one Staleness window. After the budget is exceeded the heartbeat suppresses
// WriteCtxFile so the gauge ages to genuine staleness and the existing
// no_gauge:stale path fires loudly, restoring the safety signal that carry-forward
// writes were silently suppressing (hk-lal8).
//
// This constant does NOT change any warn/act/force_act/window threshold values.
// Alias of the exported DefaultMaxHeartbeatMisses (thresholds.go single source). hk-gwz6.
const MaxHeartbeatMisses = DefaultMaxHeartbeatMisses

//
// PROBLEM. The .ctx gauge's sole writer is scripts/keeper-statusline.sh, which
// runs ONLY on a Claude Code UI repaint and SKIPS the write whenever the pane
// reports an absent/NA percentage (right after /clear, or when a busy/idle
// session stops repainting). Nothing else writes .ctx. So on a perfectly LIVE
// agent the gauge can age past Staleness(120s); the watcher then takes the
// stale branch and `continue`s BEFORE any cycle / CrispIdle / act evaluation.
// BOTH keeper triggers read this same feed, so one stale gauge kills both —
// this was the dominant failure mass (≈2699 no_gauge:stale events).
//
// FIX. Give the keeper its OWN gauge source, independent of statusLine repaint.
// On each tick, once the gauge is aging toward Staleness while the tmux pane is
// still alive (the agent process has NOT exited), the watcher re-writes .ctx
// with a fresh timestamp — deriving a current token count from the session
// transcript JSONL when it can, otherwise carrying the last-good reading
// forward. The session_id written is the latched managed UUIDv4 (falling back
// to the last gauge value), so a transient daemon-UUIDv7 / uppercase poisoning
// is corrected rather than propagated. The pane-alive gate is what preserves
// the respawn path: when the agent genuinely exits the pane goes idle, the
// heartbeat stops, the gauge goes stale, and maybeRespawn fires as before.

// transcriptDirFor returns the Claude Code transcript projects directory for the
// given project root: ~/.claude/projects/<munged-project-path>. Claude Code
// sanitises the absolute project path by replacing '/' and '.' with '-' (e.g.
// /Users/gb/github/harmonik -> -Users-gb-github-harmonik). Returns "" when the
// home directory cannot be resolved.
func transcriptDirFor(projectDir string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	munged := strings.NewReplacer("/", "-", ".", "-").Replace(projectDir)
	return filepath.Join(home, ".claude", "projects", munged)
}

// deriveContextTokens scans the Claude Code transcript JSONL for sessionID under
// transcriptDir and returns the effective context-token count of the most recent
// assistant turn that carries a usage block: input_tokens + cache_read_input_tokens +
// cache_creation_input_tokens + output_tokens. Including output_tokens makes the
// heartbeat gauge match what /context reports — the model's output from this turn
// will appear as input to the next turn, so "input + output" is the correct
// post-turn context occupancy. Returns (0, false) when the transcript is absent,
// unreadable, or carries no usage — callers then carry the last-good reading
// forward, so a derivation miss never breaks gauge liveness.
func deriveContextTokens(transcriptDir, sessionID string) (int64, bool) {
	if transcriptDir == "" || sessionID == "" {
		return 0, false
	}
	path := filepath.Join(transcriptDir, sessionID+".jsonl")
	//nolint:gosec // G304: transcriptDir derived from operator-controlled projectDir; sessionID is a latched UUID
	f, err := os.Open(path)
	if err != nil {
		return 0, false
	}
	defer func() { _ = f.Close() }()

	type usage struct {
		InputTokens         int64 `json:"input_tokens"`
		CacheReadTokens     int64 `json:"cache_read_input_tokens"`
		CacheCreationTokens int64 `json:"cache_creation_input_tokens"`
		OutputTokens        int64 `json:"output_tokens"`
	}
	type line struct {
		Message struct {
			Usage *usage `json:"usage"`
		} `json:"message"`
	}

	var (
		tokens int64
		found  bool
	)
	sc := bufio.NewScanner(f)
	// Transcript lines embed tool results and can far exceed the 64KB default;
	// allow up to 16MB per line so a long line is not silently truncated.
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		raw := sc.Bytes()
		if len(raw) == 0 {
			continue
		}
		var l line
		if err := json.Unmarshal(raw, &l); err != nil {
			continue // skip malformed / non-JSON lines
		}
		if l.Message.Usage == nil {
			continue
		}
		u := l.Message.Usage
		sum := u.InputTokens + u.CacheReadTokens + u.CacheCreationTokens + u.OutputTokens
		if sum > 0 {
			tokens = sum // keep the LAST usage-bearing turn
			found = true
		}
	}
	if err := sc.Err(); err != nil {
		// A scan error after some lines still yields the last-good sum we saw.
		return tokens, found
	}
	return tokens, found
}

// WriteCtxFile atomically writes the gauge file for the given agent (tmp-write +
// rename), mirroring the contract of scripts/keeper-statusline.sh. Used by the
// keeper-side heartbeat to keep the gauge live without a statusLine repaint.
func WriteCtxFile(projectDir, agent string, cf *CtxFile) error {
	if err := validateAgent(agent); err != nil {
		return err
	}
	path := ctxFilePath(projectDir, agent)
	keeperDir := filepath.Dir(path)
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(keeperDir, 0o755); err != nil {
		return fmt.Errorf("keeper: create keeper dir for heartbeat: %w", err)
	}
	raw, err := json.Marshal(cf)
	if err != nil {
		return fmt.Errorf("keeper: marshal heartbeat ctx: %w", err)
	}
	raw = append(raw, '\n')
	//nolint:gosec // G304: keeperDir derived from operator-controlled projectDir; pattern uses validated agent name
	tmp, err := os.CreateTemp(keeperDir, agent+".ctx.*.tmp")
	if err != nil {
		return fmt.Errorf("keeper: create heartbeat ctx tmp: %w", err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()        //nolint:errcheck // cleanup before remove
		_ = os.Remove(tmpPath) //nolint:errcheck // best-effort cleanup
		return fmt.Errorf("keeper: write heartbeat ctx tmp %q: %w", tmpPath, err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath) //nolint:errcheck // best-effort cleanup
		return fmt.Errorf("keeper: close heartbeat ctx tmp %q: %w", tmpPath, err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath) //nolint:errcheck // best-effort cleanup
		return fmt.Errorf("keeper: rename heartbeat ctx %q: %w", path, err)
	}
	return nil
}

// heartbeatSessionID picks the session_id the heartbeat should stamp into .ctx.
// Preference: the latched managed UUIDv4 (corrects a transient UUIDv7/uppercase
// poisoning in the gauge), falling back to the last gauge value. An uppercase or
// UUIDv7 managed value is rejected in favour of the last gauge value so the
// heartbeat never propagates a known-bad form.
func heartbeatSessionID(managedSID string, last *CtxFile) string {
	if managedSID != "" && !isUUIDv7(managedSID) && !isUppercaseUUID(managedSID) {
		return managedSID
	}
	return last.SessionID
}

// maybeHeartbeat keeps the gauge live on an alive agent. It is a no-op unless the
// heartbeat is enabled, a tmux target is known, the gauge has aged past
// HeartbeatThreshold, and the pane is NOT idle (the agent process is still
// running). When it fires it writes a fresh .ctx — token count re-derived from
// the transcript when available, otherwise the last-good reading carried forward
// — stamped with the managed session_id and a fresh timestamp.
//
// The pane-alive gate is load-bearing: when the agent genuinely exits the pane
// goes idle, the heartbeat stops, and the gauge is allowed to go stale so the
// respawn path (maybeRespawn) can fire. The heartbeat ONLY suppresses the false
// no_gauge:stale on a LIVE agent.
func (w *Watcher) maybeHeartbeat(ctx context.Context, last *CtxFile, age time.Duration) {
	if !w.cfg.HeartbeatEnabled || w.cfg.TmuxTarget == "" {
		return
	}
	if age < w.cfg.HeartbeatThreshold {
		return
	}
	if w.cfg.IsPaneIdleFn(ctx, w.cfg.TmuxTarget) {
		return // agent has exited — let the gauge go stale so respawn can fire
	}

	managedSID, err := w.cfg.ReadManagedSessionFn(w.cfg.ProjectDir, w.cfg.AgentName)
	if err != nil {
		managedSID = "" // fall back to the last gauge session_id
	}
	sid := heartbeatSessionID(managedSID, last)

	transcriptDir := w.cfg.TranscriptDir
	if transcriptDir == "" {
		transcriptDir = transcriptDirFor(w.cfg.ProjectDir)
	}

	fresh := CtxFile{
		Pct:        last.Pct,
		Tokens:     last.Tokens,
		WindowSize: last.WindowSize,
		SessionID:  sid,
		Ts:         time.Now().UTC().Format(time.RFC3339),
	}
	if tokens, ok := deriveContextTokens(transcriptDir, sid); ok {
		w.heartbeatMissCount = 0
		fresh.Tokens = tokens
		// Recompute pct from the fresh token count when a window size is known so
		// the gauge tracks live growth, not just the last repaint's percentage.
		windowSize := fresh.WindowSize
		if windowSize == 0 {
			windowSize = w.cfg.FallbackWindowSize
		}
		if windowSize > 0 {
			fresh.Pct = float64(tokens) / float64(windowSize) * 100.0
		}
	} else {
		w.heartbeatMissCount++
		maxMisses := w.cfg.HeartbeatMaxMisses
		if w.heartbeatMissCount > maxMisses {
			// Derive-miss budget exceeded: suppress the carry-forward write so the
			// gauge ages to genuine staleness. The existing no_gauge:stale path then
			// fires loudly, restoring the safety signal (hk-lal8).
			if w.heartbeatMissCount == maxMisses+1 {
				slog.WarnContext(ctx, "keeper: heartbeat derive-miss budget exceeded, suppressing carry-forward write",
					"agent", w.cfg.AgentName, "miss_count", w.heartbeatMissCount)
			}
			return
		}
	}

	if err := WriteCtxFile(w.cfg.ProjectDir, w.cfg.AgentName, &fresh); err != nil {
		slog.WarnContext(ctx, "keeper: heartbeat write ctx failed", "agent", w.cfg.AgentName, "err", err)
		return
	}
	slog.DebugContext(ctx, "keeper: heartbeat refreshed gauge on live pane",
		"agent", w.cfg.AgentName, "age", age, "tokens", fresh.Tokens, "session_id", sid)
}
