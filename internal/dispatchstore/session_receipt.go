package dispatchstore

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/dispatch"
)

const (
	sessionReceiptRoot = "dispatch-session-starts"
	receiptTempPrefix  = ".tmp-"
)

// InstallSessionStartReceipt installs one exact receipt without replacement.
func (s *Store) InstallSessionStartReceipt(receipt dispatch.SessionStartReceipt) error {
	namespaceMu.Lock()
	defer namespaceMu.Unlock()

	data, err := canonicalReceiptBytes(receipt)
	if err != nil {
		return err
	}
	root, err := s.ensureReceiptRoot()
	if err != nil {
		return err
	}
	path := filepath.Join(root, receiptBasename(receipt.Binding.RunID))
	tempID, err := s.ops.newUUID()
	if err != nil {
		return fmt.Errorf("dispatchstore: mint receipt temporary identity: %w", err)
	}
	if tempID.Version() != 7 {
		return errors.New("dispatchstore: receipt temporary identity must be UUIDv7")
	}
	tempPath := filepath.Join(root, receiptTempPrefix+receipt.Binding.RunID.String()+"-"+tempID.String())
	temp, err := s.ops.openFile(tempPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("dispatchstore: create receipt temp: %w", err)
	}
	if err := writeSyncClose(temp, data); err != nil {
		return errors.Join(fmt.Errorf("dispatchstore: write receipt temp: %w", err), s.removeIfPresent(tempPath))
	}
	if err := s.ops.link(tempPath, path); err != nil {
		cleanupErr := s.removeIfPresent(tempPath)
		current, readErr := readExactRegular(path, "session receipt")
		if readErr == nil && bytes.Equal(current, data) {
			return errors.Join(s.convergedSync(root), cleanupErr)
		}
		if errors.Is(err, os.ErrExist) {
			return errors.Join(errors.New("dispatchstore: conflicting session receipt already exists"), readErr, cleanupErr)
		}
		return errors.Join(fmt.Errorf("dispatchstore: install session receipt: %w", err), readErr, cleanupErr)
	}
	cleanupErr := s.removeIfPresent(tempPath)
	if syncErr := s.ops.syncDir(root); syncErr != nil {
		current, readErr := readExactRegular(path, "session receipt")
		if readErr == nil && !bytes.Equal(current, data) {
			readErr = errors.New("reloaded session receipt does not match installed bytes")
		}
		return errors.Join(&AmbiguousError{Err: errors.Join(syncErr, readErr)}, cleanupErr)
	}
	return cleanupErr
}

// LoadSessionStartReceipt reads the exact receipt for one run.
func (s *Store) LoadSessionStartReceipt(runID core.RunID) (dispatch.SessionStartReceipt, error) {
	namespaceMu.Lock()
	defer namespaceMu.Unlock()
	root, err := s.checkedReceiptRoot()
	if err != nil {
		return dispatch.SessionStartReceipt{}, err
	}
	data, err := readExactRegular(filepath.Join(root, receiptBasename(runID)), "session receipt")
	if err != nil {
		return dispatch.SessionStartReceipt{}, err
	}
	return decodeCanonicalReceipt(data, runID)
}

// ListSessionStartReceipts returns all receipts and fails closed on every entry.
func (s *Store) ListSessionStartReceipts() ([]dispatch.SessionStartReceipt, error) {
	namespaceMu.Lock()
	defer namespaceMu.Unlock()
	root, err := s.checkedReceiptRoot()
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("dispatchstore: list receipt root: %w", err)
	}
	receipts := make([]dispatch.SessionStartReceipt, 0, len(entries))
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), receiptTempPrefix) {
			if cleanupErr := s.cleanupConvergedReceiptTemp(root, entry.Name()); cleanupErr != nil {
				return nil, cleanupErr
			}
			continue
		}
		if !strings.HasSuffix(entry.Name(), intentSuffix) {
			return nil, fmt.Errorf("dispatchstore: unsupported session receipt entry %q", entry.Name())
		}
		runID, parseErr := parseBasename(entry.Name())
		if parseErr != nil {
			return nil, parseErr
		}
		data, readErr := readExactRegular(filepath.Join(root, entry.Name()), "session receipt")
		if readErr != nil {
			return nil, readErr
		}
		receipt, decodeErr := decodeCanonicalReceipt(data, runID)
		if decodeErr != nil {
			return nil, decodeErr
		}
		receipts = append(receipts, receipt)
	}
	return receipts, nil
}

func (s *Store) cleanupConvergedReceiptTemp(root, name string) error {
	runID, err := parseReceiptTempBasename(name)
	if err != nil {
		return err
	}
	tempPath := filepath.Join(root, name)
	tempBytes, err := readExactRegular(tempPath, "session receipt temporary entry")
	if err != nil {
		return err
	}
	canonicalBytes, err := readExactRegular(filepath.Join(root, receiptBasename(runID)), "session receipt")
	if err != nil || !bytes.Equal(tempBytes, canonicalBytes) {
		return errors.Join(fmt.Errorf("dispatchstore: unresolved session receipt temporary entry %q", name), err)
	}
	tempReceipt, tempDecodeErr := decodeCanonicalReceipt(tempBytes, runID)
	canonicalReceipt, canonicalDecodeErr := decodeCanonicalReceipt(canonicalBytes, runID)
	if tempDecodeErr != nil || canonicalDecodeErr != nil || tempReceipt != canonicalReceipt {
		return errors.Join(
			fmt.Errorf("dispatchstore: unresolved session receipt temporary entry %q", name),
			tempDecodeErr,
			canonicalDecodeErr,
		)
	}
	if err := s.ops.remove(tempPath); err != nil {
		_, statErr := os.Lstat(tempPath)
		if !errors.Is(statErr, os.ErrNotExist) {
			return errors.Join(fmt.Errorf("dispatchstore: remove converged receipt temp: %w", err), statErr)
		}
	}
	return s.convergedSync(root)
}

func (s *Store) receiptRoot() string {
	return filepath.Join(s.projectDir, ".harmonik", sessionReceiptRoot)
}

func (s *Store) ensureReceiptRoot() (string, error) {
	harmonikRoot := filepath.Join(s.projectDir, ".harmonik")
	if err := ensureRealDirectory(harmonikRoot); err != nil {
		return "", err
	}
	if err := s.ops.syncDir(s.projectDir); err != nil {
		return "", &AmbiguousError{Err: fmt.Errorf("sync .harmonik root: %w", err)}
	}
	root := s.receiptRoot()
	if err := ensureRealDirectory(root); err != nil {
		return "", err
	}
	if err := s.ops.syncDir(harmonikRoot); err != nil {
		return "", &AmbiguousError{Err: fmt.Errorf("sync session receipt root: %w", err)}
	}
	return root, nil
}

func (s *Store) checkedReceiptRoot() (string, error) {
	if err := checkRealDirectory(filepath.Join(s.projectDir, ".harmonik")); err != nil {
		return "", err
	}
	root := s.receiptRoot()
	if err := checkRealDirectory(root); err != nil {
		return "", err
	}
	return root, nil
}

func canonicalReceiptBytes(receipt dispatch.SessionStartReceipt) ([]byte, error) {
	data, err := json.Marshal(receipt)
	if err != nil {
		return nil, fmt.Errorf("dispatchstore: encode session receipt: %w", err)
	}
	return data, nil
}

func decodeCanonicalReceipt(data []byte, runID core.RunID) (dispatch.SessionStartReceipt, error) {
	var receipt dispatch.SessionStartReceipt
	if err := json.Unmarshal(data, &receipt); err != nil {
		return dispatch.SessionStartReceipt{}, fmt.Errorf("dispatchstore: decode session receipt: %w", err)
	}
	canonical, err := json.Marshal(receipt)
	if err != nil {
		return dispatch.SessionStartReceipt{}, fmt.Errorf("dispatchstore: remarshal session receipt: %w", err)
	}
	if !bytes.Equal(data, canonical) {
		return dispatch.SessionStartReceipt{}, errors.New("dispatchstore: session receipt bytes are not canonical")
	}
	if receipt.Binding.RunID != runID {
		return dispatch.SessionStartReceipt{}, errors.New("dispatchstore: path identity does not match session receipt")
	}
	return receipt, nil
}

func readExactRegular(path, kind string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("dispatchstore: inspect %s: %w", kind, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("dispatchstore: %s is not a regular file", kind)
	}
	data, err := os.ReadFile(path) //nolint:gosec // validated entry below a checked project namespace.
	if err != nil {
		return nil, fmt.Errorf("dispatchstore: read %s: %w", kind, err)
	}
	return data, nil
}

func receiptBasename(runID core.RunID) string { return runID.String() + intentSuffix }

func parseReceiptTempBasename(name string) (core.RunID, error) {
	const uuidTextLength = 36
	if len(name) != len(receiptTempPrefix)+uuidTextLength+1+uuidTextLength ||
		name[len(receiptTempPrefix)+uuidTextLength] != '-' {
		return core.RunID{}, fmt.Errorf("dispatchstore: invalid session receipt temporary basename %q", name)
	}
	runRaw := name[len(receiptTempPrefix) : len(receiptTempPrefix)+uuidTextLength]
	tempRaw := name[len(receiptTempPrefix)+uuidTextLength+1:]
	runID, runErr := uuid.Parse(runRaw)
	tempID, tempErr := uuid.Parse(tempRaw)
	if runErr != nil || runID.Version() != 7 || runID.String() != runRaw ||
		tempErr != nil || tempID.Version() != 7 || tempID.String() != tempRaw {
		return core.RunID{}, fmt.Errorf("dispatchstore: invalid session receipt temporary basename %q", name)
	}
	return core.RunID(runID), nil
}
