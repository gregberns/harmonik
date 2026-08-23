package keeper

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gregberns/harmonik/internal/core"
)

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

func transcriptDirFor(projectDir string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	munged := strings.NewReplacer("/", "-", ".", "-").Replace(projectDir)
	return filepath.Join(home, ".claude", "projects", munged)
}

const deriveContextTailBytes = 512 * 1024

func deriveContextTokens(ctx context.Context, transcriptDir, sessionID string) (int64, bool) {
	if transcriptDir == "" || sessionID == "" {
		return 0, false
	}
	path := filepath.Join(transcriptDir, sessionID+".jsonl")
	//nolint:gosec // G304: transcriptDir derived from operator-controlled projectDir; sessionID is a latched UUID
	f, err := os.Open(path)
	if err != nil {
		return 0, false
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			slog.WarnContext(ctx, "keeper: close transcript while deriving context", "err", closeErr, "path", path)
		}
	}()

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
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
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
		return tokens, found
	}
	return tokens, found
}

func (w *Watcher) deriveCachedTokens(ctx context.Context, transcriptDir, sid string, now time.Time) (int64, bool) {
	if w.deriveCacheSID == sid && now.Before(w.deriveCacheExpiry) {
		return w.deriveCacheTokens, true
	}
	tokens, ok := deriveContextTokens(ctx, transcriptDir, sid)
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
	if err := os.MkdirAll(keeperDir, core.HarmonikDirMode); err != nil {
		return fmt.Errorf("keeper: create keeper dir for heartbeat: %w", err)
	}
	raw, err := json.Marshal(cf)
	if err != nil {
		return fmt.Errorf("keeper: marshal heartbeat ctx: %w", err)
	}
	raw = append(raw, '\n')
	tmp, err := os.CreateTemp(keeperDir, agent+".ctx.*.tmp")
	if err != nil {
		return fmt.Errorf("keeper: create heartbeat ctx tmp: %w", err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(raw); err != nil {
		err = errors.Join(err, tmp.Close())
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

func heartbeatSessionID(managedSID string, last *CtxFile) string {
	if managedSID != "" {
		return managedSID
	}
	return last.SessionID
}

func (w *Watcher) heartbeatDue(ctx context.Context, age time.Duration) bool {
	if !w.cfg.HeartbeatEnabled || w.cfg.TmuxTarget == "" {
		return false
	}
	if age < w.cfg.HeartbeatThreshold {
		return false
	}
	return !w.cfg.IsPaneIdleFn(ctx, w.cfg.TmuxTarget)
}

func (w *Watcher) maybeHeartbeat(ctx context.Context, last *CtxFile, age time.Duration) (wrote bool) {
	if !w.heartbeatDue(ctx, age) {
		return false // agent has exited or the heartbeat is not due
	}

	managedSID, err := w.cfg.ReadManagedSessionFn(w.cfg.ProjectDir, w.cfg.AgentName)
	if err != nil {
		managedSID = "" // fall back to the last gauge session_id
	}
	sid := heartbeatSessionID(managedSID, last)
	if managedSID == "" {
		if liveSID, _, sidErr := w.cfg.ReadSidFn(w.cfg.ProjectDir, w.cfg.AgentName); sidErr == nil && isPrimarySID(liveSID) {
			sid = liveSID
		}
	}
	if sid != w.heartbeatLastSID {
		w.heartbeatMissCount = 0
		w.heartbeatLastSID = sid
	}

	transcriptDir := w.cfg.TranscriptDir
	if transcriptDir == "" {
		transcriptDir = transcriptDirFor(w.cfg.ProjectDir)
	}

	now := w.cfg.Clock.Now()
	fresh := CtxFile{
		Pct:        last.Pct,
		Tokens:     last.Tokens,
		WindowSize: last.WindowSize,
		SessionID:  sid,
		Ts:         now.UTC().Format(time.RFC3339),
	}
	derivedTokens, derivedOk := w.deriveCachedTokens(ctx, transcriptDir, sid, now)
	if derivedOk {
		w.heartbeatMissCount = 0
		fresh.Tokens = derivedTokens
		if fresh.WindowSize > 0 {
			fresh.Pct = float64(derivedTokens) / float64(fresh.WindowSize) * 100.0
		}
	} else {
		w.heartbeatMissCount++
		maxMisses := w.cfg.HeartbeatMaxMisses
		if w.heartbeatMissCount > maxMisses {
			if w.heartbeatMissCount == maxMisses+1 {
				slog.WarnContext(ctx, "keeper: heartbeat derive-miss budget exceeded, suppressing carry-forward write",
					"agent", w.cfg.AgentName, "miss_count", w.heartbeatMissCount)
			}
			return false
		}
	}

	if err := WriteCtxFile(w.cfg.ProjectDir, w.cfg.AgentName, &fresh); err != nil {
		slog.WarnContext(ctx, "keeper: heartbeat write ctx failed", "agent", w.cfg.AgentName, "err", err)
		return false
	}
	slog.DebugContext(ctx, "keeper: heartbeat refreshed gauge on live pane",
		"agent", w.cfg.AgentName, "age", age, "tokens", fresh.Tokens, "session_id", sid)
	return true
}
