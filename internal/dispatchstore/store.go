// Package dispatchstore persists dispatch intents without owning replay policy.
package dispatchstore

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/dispatch"
)

const intentSuffix = ".json"

// Store owns the durable dispatch-intent namespace for one project.
type Store struct {
	projectDir string
	ops        storeOps
}

type storeOps struct {
	createTemp func(string, string) (*os.File, error)
	link       func(string, string) error
	rename     func(string, string) error
	remove     func(string) error
	syncDir    func(string) error
}

var namespaceMu sync.Mutex

// AmbiguousError reports that exact bytes are visible but parent durability
// could not be proved. A caller must leave the fact for startup repair.
type AmbiguousError struct{ Err error }

func (e *AmbiguousError) Error() string {
	return "dispatchstore: namespace durability is ambiguous: " + e.Err.Error()
}
func (e *AmbiguousError) Unwrap() error { return e.Err }

// New returns a store rooted below the supplied project directory.
func New(projectDir string) *Store {
	return &Store{projectDir: projectDir, ops: storeOps{
		createTemp: os.CreateTemp,
		link:       os.Link,
		rename:     os.Rename,
		remove:     os.Remove,
		syncDir:    syncDirectory,
	}}
}

// Create installs a prepared intent without replacing an existing fact.
func (s *Store) Create(intent dispatch.Intent) error {
	namespaceMu.Lock()
	defer namespaceMu.Unlock()
	if intent.Phase != dispatch.PhasePrepared {
		return errors.New("dispatchstore: create requires prepared phase")
	}
	data, err := canonicalBytes(intent)
	if err != nil {
		return err
	}
	root, err := s.ensureRoot()
	if err != nil {
		return err
	}
	path := filepath.Join(root, basename(intent.Binding.RunID))
	tmp, err := s.ops.createTemp(root, ".dispatch-create-*")
	if err != nil {
		return fmt.Errorf("dispatchstore: create intent temp: %w", err)
	}
	tmpPath := tmp.Name()
	if err := writeSyncClose(tmp, data); err != nil {
		return errors.Join(fmt.Errorf("dispatchstore: write intent temp: %w", err), s.removeIfPresent(tmpPath))
	}
	if err := s.ops.link(tmpPath, path); err != nil {
		cleanupErr := s.removeIfPresent(tmpPath)
		current, readErr := readExactIntent(path)
		if readErr == nil && bytes.Equal(current, data) {
			return errors.Join(s.convergedSync(root), cleanupErr)
		}
		if errors.Is(err, os.ErrExist) {
			return errors.Join(errors.New("dispatchstore: conflicting intent already exists"), readErr, cleanupErr)
		}
		return errors.Join(fmt.Errorf("dispatchstore: install intent: %w", err), readErr, cleanupErr)
	}
	cleanupErr := s.removeIfPresent(tmpPath)
	if syncErr := s.ops.syncDir(root); syncErr != nil {
		current, readErr := readExactIntent(path)
		if readErr == nil && !bytes.Equal(current, data) {
			readErr = errors.New("reloaded intent does not match prepared bytes")
		}
		return errors.Join(&AmbiguousError{Err: errors.Join(syncErr, readErr)}, cleanupErr)
	}
	return cleanupErr
}

// Advance replaces exact predecessor bytes with the next intent phase.
func (s *Store) Advance(prior, next dispatch.Intent) error {
	namespaceMu.Lock()
	defer namespaceMu.Unlock()
	encoded, err := prepareAdvance(prior, next)
	if err != nil {
		return err
	}
	priorBytes, nextBytes := encoded.prior, encoded.next
	root, err := s.checkedRoot()
	if err != nil {
		return err
	}
	path := filepath.Join(root, basename(prior.Binding.RunID))
	current, err := readExactIntent(path)
	if err != nil {
		return err
	}
	if !bytes.Equal(current, priorBytes) {
		if bytes.Equal(current, nextBytes) {
			return s.convergedSync(root)
		}
		return errors.New("dispatchstore: predecessor bytes changed")
	}
	tmp, err := s.ops.createTemp(root, ".dispatch-advance-*")
	if err != nil {
		return fmt.Errorf("dispatchstore: create advance temp: %w", err)
	}
	tmpPath := tmp.Name()
	if err := writeSyncClose(tmp, nextBytes); err != nil {
		return errors.Join(fmt.Errorf("dispatchstore: write advance temp: %w", err), s.removeIfPresent(tmpPath))
	}
	current, err = readExactIntent(path)
	if err != nil {
		return errors.Join(err, s.removeIfPresent(tmpPath))
	}
	if !bytes.Equal(current, priorBytes) {
		return errors.Join(errors.New("dispatchstore: predecessor changed before replace"), s.removeIfPresent(tmpPath))
	}
	if err := s.ops.rename(tmpPath, path); err != nil {
		return s.convergeReplaceError(root, path, tmpPath, nextBytes, err)
	}
	if syncErr := s.ops.syncDir(root); syncErr != nil {
		current, readErr := readExactIntent(path)
		if readErr == nil && !bytes.Equal(current, nextBytes) {
			readErr = errors.New("reloaded intent does not match replacement")
		}
		return &AmbiguousError{Err: errors.Join(syncErr, readErr)}
	}
	return nil
}

// Load reads and validates the exact intent for a run.
func (s *Store) Load(runID core.RunID) (dispatch.Intent, error) {
	namespaceMu.Lock()
	defer namespaceMu.Unlock()
	root, err := s.checkedRoot()
	if err != nil {
		return dispatch.Intent{}, err
	}
	data, err := readExactIntent(filepath.Join(root, basename(runID)))
	if err != nil {
		return dispatch.Intent{}, err
	}
	return decodeCanonical(data, runID)
}

// List reads every intent-shaped entry and fails closed on any invalid fact.
func (s *Store) List() ([]dispatch.Intent, error) {
	namespaceMu.Lock()
	defer namespaceMu.Unlock()
	root, err := s.checkedRoot()
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("dispatchstore: list root: %w", err)
	}
	intents := make([]dispatch.Intent, 0, len(entries))
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), intentSuffix) {
			continue
		}
		runID, parseErr := parseBasename(entry.Name())
		if parseErr != nil {
			return nil, parseErr
		}
		data, readErr := readExactIntent(filepath.Join(root, entry.Name()))
		if readErr != nil {
			return nil, readErr
		}
		intent, decodeErr := decodeCanonical(data, runID)
		if decodeErr != nil {
			return nil, decodeErr
		}
		intents = append(intents, intent)
	}
	return intents, nil
}

// Remove deletes one exact terminal intent and syncs the root.
func (s *Store) Remove(intent dispatch.Intent) error {
	namespaceMu.Lock()
	defer namespaceMu.Unlock()
	if intent.Phase != dispatch.PhaseHandoffDurable {
		return errors.New("dispatchstore: remove requires handoff_durable phase")
	}
	expected, err := canonicalBytes(intent)
	if err != nil {
		return err
	}
	root, err := s.checkedRoot()
	if err != nil {
		return err
	}
	path := filepath.Join(root, basename(intent.Binding.RunID))
	current, err := readExactIntent(path)
	if errors.Is(err, os.ErrNotExist) {
		return s.convergedSync(root)
	}
	if err != nil {
		return err
	}
	if !bytes.Equal(current, expected) {
		return errors.New("dispatchstore: intent changed before remove")
	}
	if err := s.ops.remove(path); err != nil {
		_, statErr := os.Lstat(path)
		if errors.Is(statErr, os.ErrNotExist) {
			return s.convergedSync(root)
		}
		return errors.Join(fmt.Errorf("dispatchstore: remove intent: %w", err), statErr)
	}
	if syncErr := s.ops.syncDir(root); syncErr != nil {
		_, statErr := os.Lstat(path)
		if !errors.Is(statErr, os.ErrNotExist) {
			return &AmbiguousError{Err: errors.Join(syncErr, statErr)}
		}
		return &AmbiguousError{Err: syncErr}
	}
	return nil
}

func (s *Store) root() string { return filepath.Join(s.projectDir, ".harmonik", "dispatch-intents") }

func (s *Store) ensureRoot() (string, error) {
	harmonikRoot := filepath.Join(s.projectDir, ".harmonik")
	if err := ensureRealDirectory(harmonikRoot); err != nil {
		return "", err
	}
	if err := s.ops.syncDir(s.projectDir); err != nil {
		return "", &AmbiguousError{Err: fmt.Errorf("sync .harmonik root: %w", err)}
	}
	root := s.root()
	if err := ensureRealDirectory(root); err != nil {
		return "", err
	}
	if err := s.ops.syncDir(harmonikRoot); err != nil {
		return "", &AmbiguousError{Err: fmt.Errorf("sync intent root: %w", err)}
	}
	return root, nil
}

func (s *Store) checkedRoot() (string, error) {
	harmonikRoot := filepath.Join(s.projectDir, ".harmonik")
	if err := checkRealDirectory(harmonikRoot); err != nil {
		return "", err
	}
	root := s.root()
	if err := checkRealDirectory(root); err != nil {
		return "", err
	}
	return root, nil
}

func checkRealDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("dispatchstore: inspect root: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errors.New("dispatchstore: intent root is not a real directory")
	}
	return nil
}

func ensureRealDirectory(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.Mkdir(path, core.HarmonikDirMode); err != nil {
			return fmt.Errorf("dispatchstore: create directory: %w", err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("dispatchstore: inspect directory: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errors.New("dispatchstore: namespace component is not a real directory")
	}
	return nil
}

func readExactIntent(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("dispatchstore: inspect intent: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, errors.New("dispatchstore: intent is not a regular file")
	}
	data, err := os.ReadFile(path) //nolint:gosec // path is rooted in the project and uses a validated UUIDv7 basename.
	if err != nil {
		return nil, fmt.Errorf("dispatchstore: read intent: %w", err)
	}
	return data, nil
}

func decodeCanonical(data []byte, runID core.RunID) (dispatch.Intent, error) {
	var intent dispatch.Intent
	if err := json.Unmarshal(data, &intent); err != nil {
		return dispatch.Intent{}, fmt.Errorf("dispatchstore: decode intent: %w", err)
	}
	canonical, err := json.Marshal(intent)
	if err != nil {
		return dispatch.Intent{}, fmt.Errorf("dispatchstore: remarshal intent: %w", err)
	}
	if !bytes.Equal(data, canonical) {
		return dispatch.Intent{}, errors.New("dispatchstore: intent bytes are not canonical")
	}
	if intent.Binding.RunID != runID {
		return dispatch.Intent{}, errors.New("dispatchstore: path identity does not match intent")
	}
	return intent, nil
}

func canonicalBytes(intent dispatch.Intent) ([]byte, error) {
	data, err := json.Marshal(intent)
	if err != nil {
		return nil, fmt.Errorf("dispatchstore: encode intent: %w", err)
	}
	return data, nil
}

func basename(runID core.RunID) string { return runID.String() + intentSuffix }

func parseBasename(name string) (core.RunID, error) {
	raw := strings.TrimSuffix(name, intentSuffix)
	id, err := uuid.Parse(raw)
	if err != nil || id.Version() != 7 || id.String() != raw {
		return core.RunID{}, fmt.Errorf("dispatchstore: invalid intent basename %q", name)
	}
	return core.RunID(id), nil
}

func nextPhase(prior, next dispatch.Phase) bool {
	return prior == dispatch.PhasePrepared && next == dispatch.PhaseClaimDurable ||
		prior == dispatch.PhaseClaimDurable && next == dispatch.PhaseRunDurable ||
		prior == dispatch.PhaseRunDurable && next == dispatch.PhaseHandoffDurable
}

func writeSyncClose(file *os.File, data []byte) error {
	_, writeErr := io.Copy(file, bytes.NewReader(data))
	syncErr := file.Sync()
	closeErr := file.Close()
	return errors.Join(writeErr, syncErr, closeErr)
}

func syncDirectory(path string) error {
	dir, err := os.Open(path) //nolint:gosec // path is an internal namespace directory, not operator input.
	if err != nil {
		return err
	}
	return errors.Join(dir.Sync(), dir.Close())
}

func (s *Store) removeIfPresent(path string) error {
	err := s.ops.remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (s *Store) convergeReplaceError(root, path, tmpPath string, nextBytes []byte, operationErr error) error {
	cleanupErr := s.removeIfPresent(tmpPath)
	current, readErr := readExactIntent(path)
	if readErr == nil && bytes.Equal(current, nextBytes) {
		return errors.Join(s.convergedSync(root), cleanupErr)
	}
	return errors.Join(fmt.Errorf("dispatchstore: replace intent: %w", operationErr), readErr, cleanupErr)
}

type advanceBytes struct {
	prior []byte
	next  []byte
}

func prepareAdvance(prior, next dispatch.Intent) (advanceBytes, error) {
	priorBytes, err := canonicalBytes(prior)
	if err != nil {
		return advanceBytes{}, fmt.Errorf("dispatchstore: prior: %w", err)
	}
	nextBytes, err := canonicalBytes(next)
	if err != nil {
		return advanceBytes{}, fmt.Errorf("dispatchstore: next: %w", err)
	}
	if prior.Binding != next.Binding || !nextPhase(prior.Phase, next.Phase) {
		return advanceBytes{}, errors.New("dispatchstore: advance requires the same binding and next phase")
	}
	if prior.Run != nil && (next.Run == nil || *prior.Run != *next.Run) {
		return advanceBytes{}, errors.New("dispatchstore: advance cannot change a durable run binding")
	}
	return advanceBytes{prior: priorBytes, next: nextBytes}, nil
}

func (s *Store) convergedSync(root string) error {
	if err := s.ops.syncDir(root); err != nil {
		return &AmbiguousError{Err: err}
	}
	return nil
}
