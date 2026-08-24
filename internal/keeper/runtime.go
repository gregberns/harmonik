package keeper

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/gregberns/harmonik/internal/core"
)

// RuntimeRecord identifies the exact keeper process that owns an agent.
// The lock proves liveness. This record makes that live process auditable.
type RuntimeRecord struct {
	PID              int       `json:"pid"`
	Executable       string    `json:"executable"`
	ExecutableSHA256 string    `json:"executable_sha256"`
	Commit           string    `json:"commit,omitempty"`
	TmuxTarget       string    `json:"tmux_target"`
	ConfigSHA256     string    `json:"config_sha256"`
	StartedAt        time.Time `json:"started_at"`
}

func runtimeRecordPath(projectDir, agent string) string {
	return filepath.Join(projectDir, ".harmonik", "keeper", agent+".runtime.json")
}

// FileSHA256 returns the lowercase SHA-256 digest of a file.
func FileSHA256(path string) (string, error) {
	f, err := os.Open(path) //nolint:gosec // caller chooses the executable path
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// WriteRuntimeRecord atomically records the live keeper identity.
func WriteRuntimeRecord(projectDir, agent string, record RuntimeRecord) error {
	if err := validateAgent(agent); err != nil {
		return err
	}
	dir := filepath.Join(projectDir, ".harmonik", "keeper")
	if err := os.MkdirAll(dir, core.HarmonikDirMode); err != nil {
		return fmt.Errorf("keeper: create runtime record dir: %w", err)
	}
	raw, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return fmt.Errorf("keeper: marshal runtime record: %w", err)
	}
	raw = append(raw, '\n')
	tmp, err := os.CreateTemp(dir, ".runtime-*")
	if err != nil {
		return fmt.Errorf("keeper: create runtime record: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, runtimeRecordPath(projectDir, agent)); err != nil {
		return fmt.Errorf("keeper: install runtime record: %w", err)
	}
	return nil
}

// ReadRuntimeRecord reads the last keeper runtime identity.
func ReadRuntimeRecord(projectDir, agent string) (RuntimeRecord, error) {
	if err := validateAgent(agent); err != nil {
		return RuntimeRecord{}, err
	}
	raw, err := os.ReadFile(runtimeRecordPath(projectDir, agent)) //nolint:gosec // validated agent
	if err != nil {
		return RuntimeRecord{}, err
	}
	var record RuntimeRecord
	if err := json.Unmarshal(raw, &record); err != nil {
		return RuntimeRecord{}, fmt.Errorf("keeper: decode runtime record: %w", err)
	}
	return record, nil
}

// RemoveRuntimeRecord removes only the record owned by pid.
func RemoveRuntimeRecord(projectDir, agent string, pid int) error {
	record, err := ReadRuntimeRecord(projectDir, agent)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if record.PID != pid {
		return nil
	}
	return os.Remove(runtimeRecordPath(projectDir, agent))
}
