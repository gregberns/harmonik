package run

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
)

// DispatchAmbiguousError means exact bytes are visible but parent durability
// could not be proved.
type DispatchAmbiguousError struct{ Err error }

func (e *DispatchAmbiguousError) Error() string {
	return "run: dispatch record durability is ambiguous: " + e.Err.Error()
}
func (e *DispatchAmbiguousError) Unwrap() error { return e.Err }

// DispatchConflictError means durable bytes do not match the requested fact.
type DispatchConflictError struct{ Detail string }

func (e *DispatchConflictError) Error() string { return "run: dispatch record conflict: " + e.Detail }

// RegistrySnapshot partitions legacy session facts from universal records.
type RegistrySnapshot struct {
	Dispatch []DispatchRecord
	Legacy   []Record
}

type dispatchRegistryOps struct {
	createTemp func(string, string) (*os.File, error)
	link       func(string, string) error
	rename     func(string, string) error
	remove     func(string) error
	syncDir    func(string) error
}

func osDispatchRegistryOps() dispatchRegistryOps {
	return dispatchRegistryOps{
		createTemp: os.CreateTemp,
		link:       os.Link,
		rename:     os.Rename,
		remove:     os.Remove,
		syncDir:    syncRunDir,
	}
}

// CreateDispatchRecord installs one base record without replacement.
func CreateDispatchRecord(projectDir string, record DispatchRecord) error {
	return createDispatchRecord(projectDir, record, osDispatchRegistryOps())
}

func createDispatchRecord(projectDir string, record DispatchRecord, ops dispatchRegistryOps) error {
	runNamespaceMu.Lock()
	defer runNamespaceMu.Unlock()
	if record.Location != nil || record.SessionName != "" {
		return errors.New("run: base dispatch record cannot contain placement or session")
	}
	data, err := dispatchRecordBytes(record)
	if err != nil {
		return err
	}
	dir, err := ensureDispatchRunsRoot(projectDir, ops)
	if err != nil {
		return err
	}
	path := recordPath(projectDir, record.RunID.String())
	tmp, err := ops.createTemp(dir, ".dispatch-run-create-*")
	if err != nil {
		return fmt.Errorf("run: create dispatch temp: %w", err)
	}
	tmpPath := tmp.Name()
	if err := writeDispatchTemp(tmp, data); err != nil {
		return errors.Join(err, removeRunTemp(tmpPath, ops))
	}
	if err := ops.link(tmpPath, path); err != nil {
		return convergeDispatchLinkError(dir, path, tmpPath, data, err, ops)
	}
	cleanupErr := removeRunTemp(tmpPath, ops)
	if syncErr := ops.syncDir(dir); syncErr != nil {
		current, readErr := readRegularRunFile(path)
		if readErr == nil && !bytes.Equal(current, data) {
			readErr = errors.New("reloaded base record differs")
		}
		return errors.Join(&DispatchAmbiguousError{Err: errors.Join(syncErr, readErr)}, cleanupErr)
	}
	return cleanupErr
}

func convergeDispatchLinkError(dir, path, tmpPath string, data []byte, operationErr error, ops dispatchRegistryOps) error {
	cleanupErr := removeRunTemp(tmpPath, ops)
	current, readErr := readRegularRunFile(path)
	if readErr == nil && bytes.Equal(current, data) {
		return errors.Join(convergedRunSync(dir, ops), cleanupErr)
	}
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return errors.Join(&DispatchAmbiguousError{Err: errors.Join(operationErr, readErr)}, cleanupErr)
	}
	if errors.Is(readErr, os.ErrNotExist) {
		return errors.Join(fmt.Errorf("run: install dispatch record: %w", operationErr), cleanupErr)
	}
	return errors.Join(&DispatchConflictError{Detail: "base path is not the requested record"}, operationErr, cleanupErr)
}

// AdvanceDispatchRecord replaces one exact record with its next bound shape.
func AdvanceDispatchRecord(projectDir string, prior, next DispatchRecord) error {
	return advanceDispatchRecord(projectDir, prior, next, osDispatchRegistryOps())
}

func advanceDispatchRecord(projectDir string, prior, next DispatchRecord, ops dispatchRegistryOps) error {
	runNamespaceMu.Lock()
	defer runNamespaceMu.Unlock()
	if err := validDispatchAdvance(prior, next); err != nil {
		return err
	}
	priorBytes, err := dispatchRecordBytes(prior)
	if err != nil {
		return err
	}
	nextBytes, err := dispatchRecordBytes(next)
	if err != nil {
		return err
	}
	dir, err := checkedRunsRoot(projectDir)
	if err != nil {
		return err
	}
	path := recordPath(projectDir, prior.RunID.String())
	alreadyAdvanced, err := checkDispatchPredecessor(path, priorBytes, nextBytes)
	if err != nil {
		return err
	}
	if alreadyAdvanced {
		return convergedRunSync(dir, ops)
	}
	tmp, err := ops.createTemp(dir, ".dispatch-run-advance-*")
	if err != nil {
		return fmt.Errorf("run: create advance temp: %w", err)
	}
	tmpPath := tmp.Name()
	if err := writeDispatchTemp(tmp, nextBytes); err != nil {
		return errors.Join(err, removeRunTemp(tmpPath, ops))
	}
	current, err := readRegularRunFile(path)
	if err != nil {
		return errors.Join(err, removeRunTemp(tmpPath, ops))
	}
	if !bytes.Equal(current, priorBytes) {
		return errors.Join(&DispatchConflictError{Detail: "predecessor changed before replace"}, removeRunTemp(tmpPath, ops))
	}
	if err := ops.rename(tmpPath, path); err != nil {
		return convergeDispatchRenameError(dir, path, tmpPath, priorBytes, nextBytes, err, ops)
	}
	if syncErr := ops.syncDir(dir); syncErr != nil {
		current, readErr := readRegularRunFile(path)
		if readErr == nil && !bytes.Equal(current, nextBytes) {
			readErr = errors.New("reloaded advanced record differs")
		}
		return &DispatchAmbiguousError{Err: errors.Join(syncErr, readErr)}
	}
	return nil
}

func checkDispatchPredecessor(path string, priorBytes, nextBytes []byte) (bool, error) {
	current, err := readRegularRunFile(path)
	if err != nil {
		return false, err
	}
	if bytes.Equal(current, priorBytes) {
		return false, nil
	}
	if bytes.Equal(current, nextBytes) {
		return true, nil
	}
	return false, &DispatchConflictError{Detail: "predecessor bytes changed"}
}

func convergeDispatchRenameError(dir, path, tmpPath string, priorBytes, nextBytes []byte, operationErr error, ops dispatchRegistryOps) error {
	cleanupErr := removeRunTemp(tmpPath, ops)
	current, readErr := readRegularRunFile(path)
	if readErr == nil && bytes.Equal(current, nextBytes) {
		return errors.Join(convergedRunSync(dir, ops), cleanupErr)
	}
	if readErr == nil && bytes.Equal(current, priorBytes) {
		return errors.Join(operationErr, cleanupErr)
	}
	if readErr == nil {
		return errors.Join(&DispatchConflictError{Detail: "third record appeared during replace"}, operationErr, cleanupErr)
	}
	return errors.Join(&DispatchAmbiguousError{Err: errors.Join(operationErr, readErr)}, cleanupErr)
}

// RemoveDispatchRecord removes one exact universal record.
func RemoveDispatchRecord(projectDir string, record DispatchRecord) error {
	return removeDispatchRecord(projectDir, record, osDispatchRegistryOps())
}

func removeDispatchRecord(projectDir string, record DispatchRecord, ops dispatchRegistryOps) error {
	runNamespaceMu.Lock()
	defer runNamespaceMu.Unlock()
	expected, err := dispatchRecordBytes(record)
	if err != nil {
		return err
	}
	dir, err := checkedRunsRoot(projectDir)
	if err != nil {
		return err
	}
	path := recordPath(projectDir, record.RunID.String())
	current, err := readRegularRunFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return convergedRunSync(dir, ops)
	}
	if err != nil {
		return err
	}
	if !bytes.Equal(current, expected) {
		return &DispatchConflictError{Detail: "record changed before remove"}
	}
	if err := ops.remove(path); err != nil {
		reloaded, readErr := readRegularRunFile(path)
		if errors.Is(readErr, os.ErrNotExist) {
			return convergedRunSync(dir, ops)
		}
		if readErr == nil && bytes.Equal(reloaded, expected) {
			return err
		}
		if readErr == nil {
			return errors.Join(&DispatchConflictError{Detail: "third record appeared during remove"}, err)
		}
		return &DispatchAmbiguousError{Err: errors.Join(err, readErr)}
	}
	if syncErr := ops.syncDir(dir); syncErr != nil {
		_, readErr := readRegularRunFile(path)
		if errors.Is(readErr, os.ErrNotExist) {
			readErr = nil
		}
		return &DispatchAmbiguousError{Err: errors.Join(syncErr, readErr)}
	}
	return nil
}

// ScanRegistry fails closed and partitions every record-shaped entry.
func ScanRegistry(projectDir string) (RegistrySnapshot, error) {
	runNamespaceMu.Lock()
	defer runNamespaceMu.Unlock()
	dir, err := checkedRunsRoot(projectDir)
	if errors.Is(err, os.ErrNotExist) {
		return RegistrySnapshot{}, nil
	}
	if err != nil {
		return RegistrySnapshot{}, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return RegistrySnapshot{}, err
	}
	var result RegistrySnapshot
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		runID, parseErr := canonicalRegistryBasename(entry.Name())
		if parseErr != nil {
			return RegistrySnapshot{}, parseErr
		}
		data, readErr := readRegularRunFile(filepath.Join(dir, entry.Name()))
		if readErr != nil {
			return RegistrySnapshot{}, readErr
		}
		var version struct {
			SchemaVersion int `json:"schema_version"`
		}
		if err := json.Unmarshal(data, &version); err != nil {
			return RegistrySnapshot{}, fmt.Errorf("run: read schema: %w", err)
		}
		switch version.SchemaVersion {
		case 1:
			legacy, decodeErr := decodeLegacyRecord(data, runID)
			if decodeErr != nil {
				return RegistrySnapshot{}, decodeErr
			}
			result.Legacy = append(result.Legacy, legacy)
		case dispatchRecordSchemaVersion:
			dispatchRecord, decodeErr := decodeDispatchRecord(data, runID)
			if decodeErr != nil {
				return RegistrySnapshot{}, decodeErr
			}
			result.Dispatch = append(result.Dispatch, dispatchRecord)
		default:
			return RegistrySnapshot{}, fmt.Errorf("run: unsupported record schema_version %d", version.SchemaVersion)
		}
	}
	return result, nil
}

func canonicalRegistryBasename(name string) (core.RunID, error) {
	if filepath.Ext(name) != ".json" {
		return core.RunID{}, fmt.Errorf("run: invalid record basename %q", name)
	}
	raw := name[:len(name)-len(".json")]
	id, err := uuid.Parse(raw)
	if err != nil || id == uuid.Nil || id.Version() == 0 || id.String() != raw {
		return core.RunID{}, fmt.Errorf("run: invalid record basename %q", name)
	}
	return core.RunID(id), nil
}

func validDispatchAdvance(prior, next DispatchRecord) error {
	if err := prior.Validate(); err != nil {
		return err
	}
	if err := next.Validate(); err != nil {
		return err
	}
	priorBase, nextBase := prior, next
	priorBase.Location, nextBase.Location = nil, nil
	priorBase.SessionName, nextBase.SessionName = "", ""
	if priorBase != nextBase {
		return &DispatchConflictError{Detail: "base identity changed"}
	}
	if prior.Location == nil && next.Location != nil && next.SessionName == "" {
		return nil
	}
	if prior.Location != nil && next.Location != nil && *prior.Location == *next.Location && prior.SessionName == "" && next.SessionName != "" {
		return nil
	}
	return &DispatchConflictError{Detail: "record did not advance by one binding"}
}

func dispatchRecordBytes(record DispatchRecord) ([]byte, error) { return json.Marshal(record) }

func decodeDispatchRecord(data []byte, runID core.RunID) (DispatchRecord, error) {
	var record DispatchRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return DispatchRecord{}, err
	}
	canonical, err := json.Marshal(record)
	if err != nil || !bytes.Equal(canonical, data) || record.RunID != runID {
		return DispatchRecord{}, &DispatchConflictError{Detail: "dispatch bytes or path identity are not canonical"}
	}
	return record, nil
}

func decodeLegacyRecord(data []byte, runID core.RunID) (Record, error) {
	var record Record
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&record); err != nil {
		return Record{}, err
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return Record{}, errors.New("run: legacy record must contain one JSON value")
	}
	if record.SchemaVersion != 1 || record.RunID != runID.String() || record.BeadID == "" || record.SessionName == "" || record.StartedAt.IsZero() {
		return Record{}, &DispatchConflictError{Detail: "legacy record identity is invalid"}
	}
	canonical, err := json.MarshalIndent(record, "", "  ")
	if err != nil || !bytes.Equal(canonical, data) {
		return Record{}, &DispatchConflictError{Detail: "legacy record bytes are not canonical writer output"}
	}
	return record, nil
}

func ensureDispatchRunsRoot(projectDir string, ops dispatchRegistryOps) (string, error) {
	harmonikRoot := filepath.Join(projectDir, ".harmonik")
	if err := ensureRealRunDir(harmonikRoot); err != nil {
		return "", err
	}
	if err := ops.syncDir(projectDir); err != nil {
		return "", &DispatchAmbiguousError{Err: err}
	}
	dir := runsDir(projectDir)
	if err := ensureRealRunDir(dir); err != nil {
		return "", err
	}
	if err := ops.syncDir(harmonikRoot); err != nil {
		return "", &DispatchAmbiguousError{Err: err}
	}
	return dir, nil
}

func checkedRunsRoot(projectDir string) (string, error) {
	for _, path := range []string{filepath.Join(projectDir, ".harmonik"), runsDir(projectDir)} {
		info, err := os.Lstat(path)
		if err != nil {
			return "", fmt.Errorf("run: inspect registry root: %w", err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return "", errors.New("run: registry root is not a real directory")
		}
	}
	return runsDir(projectDir), nil
}

func ensureRealRunDir(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return os.Mkdir(path, core.HarmonikDirMode)
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errors.New("run: registry namespace component is not a real directory")
	}
	return nil
}

func readRegularRunFile(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("run: inspect record: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, errors.New("run: record is not a regular file")
	}
	data, err := os.ReadFile(path) //nolint:gosec // validated UUIDv7 path below the checked project registry root.
	return data, err
}

func canonicalRunBasename(name string) (core.RunID, error) {
	raw := name[:len(name)-len(".json")]
	id, err := uuid.Parse(raw)
	if err != nil || id.Version() != 7 || id.String() != raw {
		return core.RunID{}, fmt.Errorf("run: invalid record basename %q", name)
	}
	return core.RunID(id), nil
}

func writeDispatchTemp(file *os.File, data []byte) error {
	_, writeErr := io.Copy(file, bytes.NewReader(data))
	syncErr := file.Sync()
	closeErr := file.Close()
	return errors.Join(writeErr, syncErr, closeErr)
}

func syncRunDir(path string) error {
	dir, err := os.Open(path) //nolint:gosec // internal project namespace path.
	if err != nil {
		return err
	}
	return errors.Join(dir.Sync(), dir.Close())
}

func convergedRunSync(dir string, ops dispatchRegistryOps) error {
	if err := ops.syncDir(dir); err != nil {
		return &DispatchAmbiguousError{Err: err}
	}
	return nil
}

func removeRunTemp(path string, ops dispatchRegistryOps) error {
	err := ops.remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
