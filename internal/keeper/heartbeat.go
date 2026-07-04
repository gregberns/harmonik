package keeper

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
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

// deriveContextTailBytes is the tail window read by deriveContextTokens.
// The last usage-bearing assistant turn is always near EOF (the transcript is
// append-only), so scanning only the last 512 KB reduces scan cost from
// O(filesize) to O(1) for sessions with large transcripts. For files smaller
// than this window the scan is equivalent to a full read. Refs: hk-div6c.
const deriveContextTailBytes = 512 * 1024

// deriveContextTokens scans the Claude Code transcript JSONL for sessionID under
// transcriptDir and returns the effective context-token count of the most recent
// assistant turn that carries a usage block: input_tokens + cache_read_input_tokens +
// cache_creation_input_tokens + output_tokens. Including output_tokens makes the
// heartbeat gauge match what /context reports — the model's output from this turn
// will appear as input to the next turn, so "input + output" is the correct
// post-turn context occupancy. Returns (0, false) when the transcript is absent,
// unreadable, or carries no usage — callers then carry the last-good reading
// forward, so a derivation miss never breaks gauge liveness.
//
// Scan is bounded to the tail window (deriveContextTailBytes) because the last
// usage turn is always near EOF; scanning the full file on every heartbeat tick
// caused sustained 20-47% CPU on long captain sessions. Refs: hk-div6c.
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

	// Seek to the tail window so the scan is O(deriveContextTailBytes) not O(filesize).
	partialStart := false
	size, seekErr := f.Seek(0, io.SeekEnd)
	if seekErr == nil {
		if size > deriveContextTailBytes {
			if _, err2 := f.Seek(size-deriveContextTailBytes, io.SeekStart); err2 == nil {
				partialStart = true
			}
		}
		if !partialStart {
			if _, err2 := f.Seek(0, io.SeekStart); err2 != nil {
				return 0, false
			}
		}
	} else {
		if _, err2 := f.Seek(0, io.SeekStart); err2 != nil {
			return 0, false
		}
	}

	var (
		tokens int64
		found  bool
	)
	sc := bufio.NewScanner(f)
	// Transcript lines embed tool results and can far exceed the 64KB default;
	// allow up to 16MB per line so a long line is not silently truncated.
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	// When seeked into the middle of the file, the first read may be a partial
	// line; discard it so we only parse complete JSON objects.
	if partialStart {
		sc.Scan()
	}
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

// deriveCachedTokens returns the derive result for (transcriptDir, sid), using
// the Watcher's in-memory cache when the session and TTL match. Only successful
// derives are cached; misses always call deriveContextTokens directly so the
// miss-budget counter (heartbeatMissCount) increments correctly per tick.
// Single-threaded: only the Run goroutine calls this via maybeHeartbeat.
// Refs: hk-div6c.
func (w *Watcher) deriveCachedTokens(transcriptDir, sid string, now time.Time) (int64, bool) {
	if w.deriveCacheSID == sid && now.Before(w.deriveCacheExpiry) {
		return w.deriveCacheTokens, true
	}
	tokens, ok := deriveContextTokens(transcriptDir, sid)
	if ok {
		w.deriveCacheSID = sid
		w.deriveCacheTokens = tokens
		w.deriveCacheExpiry = now.Add(w.cfg.DeriveCacheTTL)
	}
	return tokens, ok
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
// Preference: the latched managed session, falling back to the last gauge value.
func heartbeatSessionID(managedSID string, last *CtxFile) string {
	if managedSID != "" {
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
	// When .managed is empty (e.g., after a ClearSettle-timeout cycle clears the
	// binding), fall back to the authoritative .sid channel for the current session
	// id. Without this, derive targets the previous session's JSONL and exhausts
	// MaxHeartbeatMisses, suppressing writes and causing gauge-death on a live
	// agent (K1 of hk-4xni9 — leto gauge stale 23h post-ClearSettle-timeout).
	if managedSID == "" {
		if liveSID, _, sidErr := w.cfg.ReadSidFn(w.cfg.ProjectDir, w.cfg.AgentName); sidErr == nil && isPrimarySID(liveSID) {
			sid = liveSID
		}
	}
	// Reset the miss budget when the derive target changes (new session detected).
	// This unblocks a heartbeat that exhausted its budget against a prior
	// session's transcript and allows the new session a fresh derive window.
	// Refs: hk-4xni9 K1.
	if sid != w.heartbeatLastSID {
		w.heartbeatMissCount = 0
		w.heartbeatLastSID = sid
	}

	transcriptDir := w.cfg.TranscriptDir
	if transcriptDir == "" {
		transcriptDir = transcriptDirFor(w.cfg.ProjectDir)
	}

	now := time.Now()
	fresh := CtxFile{
		Pct:        last.Pct,
		Tokens:     last.Tokens,
		WindowSize: last.WindowSize,
		SessionID:  sid,
		Ts:         now.UTC().Format(time.RFC3339),
	}
	// Use cached token count when available (same session, within TTL) to avoid
	// O(filesize) JSONL re-scans on consecutive heartbeat ticks. Misses bypass
	// the cache so the miss-budget counter increments correctly per tick.
	// Refs: hk-div6c.
	derivedTokens, derivedOk := w.deriveCachedTokens(transcriptDir, sid, now)
	if derivedOk {
		w.heartbeatMissCount = 0
		fresh.Tokens = derivedTokens
		// Recompute pct only when the window size is authoritative (written by the
		// statusline script). When WindowSize==0 the statusline hasn't confirmed the
		// window yet; substituting FallbackWindowSize (200k default) overestimates pct
		// for large-context sessions (e.g. 210k/200k=105%) which causes
		// belowWarnThreshold to return false and fires session_keeper_warn below the
		// configured warn_pct. Carry last.Pct forward instead. Refs: hk-eovln.
		if fresh.WindowSize > 0 {
			fresh.Pct = float64(derivedTokens) / float64(fresh.WindowSize) * 100.0
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
