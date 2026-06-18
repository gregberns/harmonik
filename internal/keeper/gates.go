package keeper

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// idleMarkerPath returns the path to <projectDir>/.harmonik/keeper/<agent>.idle.
func idleMarkerPath(projectDir, agent string) string {
	return filepath.Join(projectDir, ".harmonik", "keeper", agent+".idle")
}

// dispatchingMarkerPath returns the path to <projectDir>/.harmonik/keeper/<agent>.dispatching.
func dispatchingMarkerPath(projectDir, agent string) string {
	return filepath.Join(projectDir, ".harmonik", "keeper", agent+".dispatching")
}

// precompactMarkerPath returns the path to <projectDir>/.harmonik/keeper/<agent>.precompact.
// This file is written by keeper-precompact-hook.sh when it blocks native
// auto-compaction (exit 2 / decision:block). The keeper watcher detects it and
// runs the intent-preserving cycle, then calls ClearPrecompactTrigger.
func precompactMarkerPath(projectDir, agent string) string {
	return filepath.Join(projectDir, ".harmonik", "keeper", agent+".precompact")
}

// HasPrecompactTrigger reports whether the precompact trigger marker exists for
// the given agent. Returns true when the PreCompact hook has blocked at least
// one compaction and the keeper has not yet consumed the trigger.
// Returns false for any agent name that fails validateAgent (mirroring IsManaged).
func HasPrecompactTrigger(projectDir, agent string) bool {
	if validateAgent(agent) != nil {
		return false // fail-open: traversal name cannot have a valid marker
	}
	_, err := os.Stat(precompactMarkerPath(projectDir, agent))
	return err == nil
}

// ClearPrecompactTrigger removes the precompact trigger marker for the given
// agent. The keeper watcher calls this after deciding what action to take (cycle
// or skip) so the next PreCompact fire gets a clean slate. Idempotent.
func ClearPrecompactTrigger(projectDir, agent string) error {
	if err := validateAgent(agent); err != nil {
		return err
	}
	if err := os.Remove(precompactMarkerPath(projectDir, agent)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("keeper: remove precompact marker: %w", err)
	}
	return nil
}

// restartNowMarkerPath returns the path to <projectDir>/.harmonik/keeper/<agent>.restart-now.
// This file is written by `harmonik keeper restart-now` with JSON content
// {nonce, requested_at, session_id}. The keeper watcher detects it and runs the
// on-demand clear→resume cycle (RunOnDemand), then calls ClearRestartNowTrigger.
func restartNowMarkerPath(projectDir, agent string) string {
	return filepath.Join(projectDir, ".harmonik", "keeper", agent+".restart-now")
}

// RestartNowMarker is the JSON content of the .restart-now marker file written
// by `harmonik keeper restart-now`. It carries the nonce extracted from the
// captain's HANDOFF file, the timestamp of the request, and the session_id at
// the time of the request. The keeper's RunOnDemand freshness gate validates all
// three fields before issuing /clear.
//
// Refs: hk-wjzf, hk-xjlq, ON-059.
type RestartNowMarker struct {
	// Nonce is the KEEPER:cyc token extracted from HANDOFF-<agent>.md by the CLI.
	// RunOnDemand checks that the handoff still contains <!-- KEEPER:<Nonce> -->.
	Nonce string `json:"nonce"`

	// RequestedAt is the time at which `harmonik keeper restart-now` was called.
	// The handoff file mtime must be >= RequestedAt for the freshness gate to pass.
	RequestedAt time.Time `json:"requested_at"`

	// SessionID is the .ctx session_id at the time the restart-now was requested.
	// Must match cf.SessionID in RunOnDemand.
	SessionID string `json:"session_id"`
}

// HasRestartNowTrigger reports whether the restart-now trigger marker exists for
// the given agent. Returns true when `harmonik keeper restart-now` has written
// the marker and the keeper has not yet consumed it.
// Returns false for any agent name that fails validateAgent.
func HasRestartNowTrigger(projectDir, agent string) bool {
	if validateAgent(agent) != nil {
		return false // fail-open: traversal name cannot have a valid marker
	}
	_, err := os.Stat(restartNowMarkerPath(projectDir, agent))
	return err == nil
}

// ClearRestartNowTrigger removes the restart-now trigger marker for the given
// agent. RunOnDemand calls this at entry (consume-once) so no re-fire occurs
// even when a gate blocks the cycle. Idempotent.
func ClearRestartNowTrigger(projectDir, agent string) error {
	if err := validateAgent(agent); err != nil {
		return err
	}
	if err := os.Remove(restartNowMarkerPath(projectDir, agent)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("keeper: remove restart-now marker: %w", err)
	}
	return nil
}

// WriteRestartNowMarker writes m atomically to the .restart-now marker file for
// the given agent, using the korba fsync-marker pattern (temp + fsync + rename)
// from WriteManagedSessionID (hk-b5e2) so no torn/partial JSON is ever
// observable by the watcher's freshness gate.
func WriteRestartNowMarker(projectDir, agent string, m *RestartNowMarker) error {
	if err := validateAgent(agent); err != nil {
		return err
	}
	keeperDir := filepath.Join(projectDir, ".harmonik", "keeper")
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(keeperDir, 0o755); err != nil {
		return fmt.Errorf("keeper: create keeper dir: %w", err)
	}
	path := restartNowMarkerPath(projectDir, agent)
	content, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("keeper: marshal restart-now marker: %w", err)
	}
	content = append(content, '\n')
	// os.CreateTemp gives each concurrent writer a unique temp path so no two
	// concurrent writes can publish each other's partial content. Refs: hk-b5e2.
	//nolint:gosec // G304: keeperDir derived from operator-controlled projectDir; pattern uses validated agent name
	tmp, err := os.CreateTemp(keeperDir, agent+".restart-now.*.tmp")
	if err != nil {
		return fmt.Errorf("keeper: create restart-now marker tmp: %w", err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()        //nolint:errcheck // cleanup before remove
		_ = os.Remove(tmpPath) //nolint:errcheck // best-effort cleanup
		return fmt.Errorf("keeper: write restart-now marker tmp %q: %w", tmpPath, err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()        //nolint:errcheck // cleanup before remove
		_ = os.Remove(tmpPath) //nolint:errcheck // best-effort cleanup
		return fmt.Errorf("keeper: fsync restart-now marker tmp %q: %w", tmpPath, err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath) //nolint:errcheck // best-effort cleanup
		return fmt.Errorf("keeper: close restart-now marker tmp %q: %w", tmpPath, err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath) //nolint:errcheck // best-effort cleanup of tmp
		return fmt.Errorf("keeper: rename restart-now marker %q: %w", path, err)
	}
	return nil
}

// ReadRestartNowMarker reads and parses the .restart-now marker JSON for the
// given agent. Returns an error when the file is absent, unreadable, or
// contains invalid JSON.
func ReadRestartNowMarker(projectDir, agent string) (*RestartNowMarker, error) {
	if err := validateAgent(agent); err != nil {
		return nil, err
	}
	path := restartNowMarkerPath(projectDir, agent)
	//nolint:gosec // G304: path derived from operator-controlled projectDir and agent validated above
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("keeper: read restart-now marker %q: %w", path, err)
	}
	var m RestartNowMarker
	if err := json.Unmarshal(bytes.TrimSpace(data), &m); err != nil {
		return nil, fmt.Errorf("keeper: parse restart-now marker %q: %w", path, err)
	}
	return &m, nil
}

// crispIdleTolerance is the maximum age by which .ctx may postdate .idle and
// still be considered a statusLine poll rather than real tool activity. The
// statusLine hook rewrites .ctx every ~2s, so any .ctx refresh within this
// window is a passive gauge update, not an agent action.
const crispIdleTolerance = 10 * time.Second

// CrispIdle reports whether the agent is at a crisp await-input boundary: the
// .idle marker exists AND either (a) its mtime is newer than .ctx, or (b) .ctx
// is only marginally newer (within crispIdleTolerance). The tolerance covers
// the statusLine hook's ~2s .ctx refresh cadence: a .ctx update within 10s of
// .idle is a passive poll, not agent tool activity.
//
// Returns false when the .idle marker is absent, when the .ctx gauge file
// cannot be stat'd, or when .ctx postdates .idle by more than the tolerance.
func CrispIdle(projectDir, agent string) bool {
	idleStat, err := os.Stat(idleMarkerPath(projectDir, agent))
	if err != nil {
		return false // absent or unreadable
	}
	ctxStat, err := os.Stat(ctxFilePath(projectDir, agent))
	if err != nil {
		return false // no ctx yet — can't confirm ordering
	}
	idleMtime := idleStat.ModTime()
	ctxMtime := ctxStat.ModTime()
	if idleMtime.After(ctxMtime) {
		return true // .idle strictly newer — clean boundary
	}
	// .ctx is marginally newer: treat as a statusLine poll if within tolerance.
	return ctxMtime.Sub(idleMtime) <= crispIdleTolerance
}

// HoldingDispatch reports whether the agent has in-flight queue work that the
// session-keeper cycle must defer around. It checks for the presence of the
// .dispatching marker file.
//
// FAIL-CLOSED: any stat error other than ErrNotExist (e.g. permission denied,
// I/O error) is treated as HoldingDispatch = true so the cycle never clobbers
// an uncertain state.
func HoldingDispatch(projectDir, agent string) bool {
	_, err := os.Stat(dispatchingMarkerPath(projectDir, agent))
	if err == nil {
		return true
	}
	if os.IsNotExist(err) {
		return false
	}
	return true // fail-closed on unexpected error
}

// SetDispatching writes the .dispatching marker for the given agent, recording
// the current timestamp as its content. The orchestrator calls this before
// submitting a batch to the queue so the session-keeper cycle defers.
func SetDispatching(projectDir, agent string) error {
	if err := validateAgent(agent); err != nil {
		return err
	}
	keeperDir := filepath.Join(projectDir, ".harmonik", "keeper")
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(keeperDir, 0o755); err != nil {
		return fmt.Errorf("keeper: create keeper dir: %w", err)
	}
	path := dispatchingMarkerPath(projectDir, agent)
	content := time.Now().UTC().Format(time.RFC3339) + "\n"
	//nolint:gosec // G306: 0600 — keeper-owned file, no world-read needed
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		return fmt.Errorf("keeper: write dispatching marker %q: %w", path, err)
	}
	return nil
}

// ClearDispatching removes the .dispatching marker for the given agent.
// The orchestrator calls this when all in-flight queue work has completed.
// It is idempotent: an already-absent marker is not an error.
func ClearDispatching(projectDir, agent string) error {
	if err := validateAgent(agent); err != nil {
		return err
	}
	if err := os.Remove(dispatchingMarkerPath(projectDir, agent)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("keeper: remove dispatching marker: %w", err)
	}
	return nil
}

// IsSleeping reports whether the session identified by sessionID is currently
// parked by the QuiesceArbiter (M1 / hk-jeby). It checks for the presence of
// .harmonik/.sleeping.<sessionID>, the per-session marker written by the daemon
// when a session is quiesced. When sessionID or projectDir is empty, returns
// false (fail-open: cannot determine state, allow keeper to act).
//
// The keeper watcher (M3 / hk-l3gs) uses this to gate warn pane-injection and
// cycle dispatch so a sleeping session is not woken by its own keeper.
// Refs: hk-l3gs, hk-jeby.
func IsSleeping(projectDir, sessionID string) bool {
	if sessionID == "" || projectDir == "" {
		return false
	}
	path := filepath.Join(projectDir, ".harmonik", ".sleeping."+sessionID)
	_, err := os.Stat(path)
	return err == nil
}
