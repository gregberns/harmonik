package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/gregberns/harmonik/internal/core"
)

const decisionAckSchemaVersion = 1

type decisionAckSubjectKind string

const (
	decisionAckSubjectKindBead  decisionAckSubjectKind = "bead"
	decisionAckSubjectKindQueue decisionAckSubjectKind = "queue"
)

type decisionAckStatus string

const (
	decisionAckStatusPending      decisionAckStatus = "pending"
	decisionAckStatusAcknowledged decisionAckStatus = "acknowledged"
)

type decisionAckRecord struct {
	SchemaVersion int                    `json:"schema_version"`
	AckToken      string                 `json:"ack_token"`
	Status        decisionAckStatus      `json:"status"`
	SubjectKind   decisionAckSubjectKind `json:"subject_kind"`
	SubjectID     string                 `json:"subject_id"`
	Reason        string                 `json:"reason,omitempty"`
	EmittedAt     string                 `json:"emitted_at,omitempty"`
}

const sentinelSubjectIDACT = "sentinel"

func decisionAcksDir(projectDir string) string {
	return filepath.Join(projectDir, ".harmonik", "decision_acks")
}

// DecisionBlocker holds the set of subjects (beads or queues) that are
// currently blocked by an unacknowledged decision_required event.
//
// It is populated at startup by LoadDecisionAckState and checked at every
// workloop dispatch attempt (EV-043).  Thread-safe.
type DecisionBlocker struct {
	mu sync.RWMutex

	// blockedBeads maps BeadID → set of pending ack tokens.
	// A bead is blocked while its set is non-empty.
	blockedBeads map[core.BeadID]map[string]struct{}

	// blockedQueues maps queue subject ID → set of pending ack tokens.
	// A queue is blocked while its set is non-empty.
	blockedQueues map[string]map[string]struct{}
}

// NewDecisionBlocker returns an empty, ready-to-use DecisionBlocker.
func NewDecisionBlocker() *DecisionBlocker {
	return &DecisionBlocker{
		blockedBeads:  make(map[core.BeadID]map[string]struct{}),
		blockedQueues: make(map[string]map[string]struct{}),
	}
}

// AddBeadBlock records a pending ack token for beadID.
// Idempotent: calling multiple times with the same (beadID, token) is safe.
func (b *DecisionBlocker) AddBeadBlock(beadID core.BeadID, ackToken string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.blockedBeads[beadID]; !ok {
		b.blockedBeads[beadID] = make(map[string]struct{})
	}
	b.blockedBeads[beadID][ackToken] = struct{}{}
}

// AddQueueBlock records a pending ack token for the given queue subject ID.
// Idempotent.
func (b *DecisionBlocker) AddQueueBlock(subjectID string, ackToken string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.blockedQueues[subjectID]; !ok {
		b.blockedQueues[subjectID] = make(map[string]struct{})
	}
	b.blockedQueues[subjectID][ackToken] = struct{}{}
}

// IsBeadBlocked reports whether beadID has at least one unacknowledged
// decision_required pending (EV-043).
func (b *DecisionBlocker) IsBeadBlocked(beadID core.BeadID) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.blockedBeads[beadID]) > 0
}

// IsQueueBlocked reports whether the given queue subject ID has at least one
// unacknowledged decision_required pending.
func (b *DecisionBlocker) IsQueueBlocked(subjectID string) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.blockedQueues[subjectID]) > 0
}

// Acknowledge removes ackToken from the pending set for its subject.
// If the set becomes empty the subject is unblocked and future dispatch
// attempts will proceed normally.
//
// It is safe to call Acknowledge for a token that was never registered (no-op).
func (b *DecisionBlocker) Acknowledge(kind decisionAckSubjectKind, subjectID string, ackToken string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	switch kind {
	case decisionAckSubjectKindBead:
		beadID := core.BeadID(subjectID)
		tokens := b.blockedBeads[beadID]
		delete(tokens, ackToken)
		if len(tokens) == 0 {
			delete(b.blockedBeads, beadID)
		}
	case decisionAckSubjectKindQueue:
		tokens := b.blockedQueues[subjectID]
		delete(tokens, ackToken)
		if len(tokens) == 0 {
			delete(b.blockedQueues, subjectID)
		}
	}
}

// PendingBeadTokens returns the set of pending ack tokens for beadID.
// Returns nil if the bead is unblocked.
// The returned map is a snapshot copy — safe to use outside the lock.
func (b *DecisionBlocker) PendingBeadTokens(beadID core.BeadID) map[string]struct{} {
	b.mu.RLock()
	defer b.mu.RUnlock()
	src := b.blockedBeads[beadID]
	if len(src) == 0 {
		return nil
	}
	cp := make(map[string]struct{}, len(src))
	for k := range src {
		cp[k] = struct{}{}
	}
	return cp
}

// LoadDecisionAckState scans <projectDir>/.harmonik/decision_acks/ for
// ack-state files with status=pending and seeds blocker with the corresponding
// dispatch-blocking entries.
//
// This MUST be called at daemon startup BEFORE the workloop begins dispatching
// (EV-043a).
//
// Behaviour:
//   - Directory absent → no-op (no decisions have ever been required).
//   - File unreadable or unparseable → logged to stderr; skipped (non-fatal).
//   - schema_version > decisionAckSchemaVersion → logged; skipped (non-fatal;
//     a newer agent wrote the file; we can't safely interpret it).
//   - status == "pending" → AddBeadBlock or AddQueueBlock called on blocker.
//   - status == "acknowledged" → skipped (already resolved).
//
// Spec ref: specs/event-model.md §4.12 EV-043a.
// Bead ref: hk-pbmsq.
func LoadDecisionAckState(_ context.Context, projectDir string, blocker *DecisionBlocker) error {
	acksDir := decisionAcksDir(projectDir)

	entries, err := os.ReadDir(acksDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("LoadDecisionAckState: read dir %s: %w", acksDir, err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		filePath := filepath.Join(acksDir, entry.Name())
		if loadErr := loadOneAckFile(filePath, blocker); loadErr != nil {
			fmt.Fprintf(os.Stderr,
				"daemon: LoadDecisionAckState: skip %s: %v\n", filePath, loadErr)
		}
	}
	return nil
}

func loadOneAckFile(path string, blocker *DecisionBlocker) error {
	data, err := os.ReadFile(path) //nolint:gosec // G304: operator-controlled project dir
	if err != nil {
		return fmt.Errorf("read: %w", err)
	}
	var rec decisionAckRecord
	if jsonErr := json.Unmarshal(data, &rec); jsonErr != nil {
		return fmt.Errorf("parse: %w", jsonErr)
	}
	if rec.SchemaVersion > decisionAckSchemaVersion {
		return fmt.Errorf("schema_version %d > supported %d", rec.SchemaVersion, decisionAckSchemaVersion)
	}
	if rec.Status != decisionAckStatusPending {
		return nil
	}
	if rec.AckToken == "" {
		return fmt.Errorf("ack_token is empty")
	}
	if rec.SubjectID == "" {
		return fmt.Errorf("subject_id is empty")
	}
	switch rec.SubjectKind {
	case decisionAckSubjectKindBead:
		blocker.AddBeadBlock(core.BeadID(rec.SubjectID), rec.AckToken)
	case decisionAckSubjectKindQueue:
		blocker.AddQueueBlock(rec.SubjectID, rec.AckToken)
	default:
		return fmt.Errorf("unknown subject_kind %q", rec.SubjectKind)
	}
	return nil
}
