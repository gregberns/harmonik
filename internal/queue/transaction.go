package queue

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/google/uuid"
)

// NamespaceOutcome is the durable result taxonomy defined by QM-001.
type NamespaceOutcome string

// Namespace mutation outcomes distinguish rejection, definite absence,
// durable commit, and unresolved parent durability.
const (
	OutcomeRejected            NamespaceOutcome = "rejected"
	OutcomeNotCommitted        NamespaceOutcome = "not_committed"
	OutcomeCommittedDurable    NamespaceOutcome = "committed_durable"
	OutcomeCommitIndeterminate NamespaceOutcome = "commit_indeterminate"
)

// NamespaceResult reports what is known about a namespace mutation. Callers
// must branch on Outcome; Err is diagnostic and never changes its meaning.
type NamespaceResult struct {
	Outcome NamespaceOutcome
	Err     error
}

// Committed reports whether the selected namespace state is parent-synced.
func (r NamespaceResult) Committed() bool { return r.Outcome == OutcomeCommittedDurable }

// OperationKind identifies the queue mutation fixed by a replace intent.
type OperationKind string

// Queue operation kinds cover every QM-001 canonical replacement caller.
const (
	OperationCreate          OperationKind = "create"
	OperationSubmit          OperationKind = "submit"
	OperationAppend          OperationKind = "append"
	OperationReservation     OperationKind = "reservation"
	OperationRunIDPatch      OperationKind = "run-id-patch"
	OperationActivation      OperationKind = "activation"
	OperationAdvance         OperationKind = "advance"
	OperationPause           OperationKind = "pause"
	OperationResume          OperationKind = "resume"
	OperationEagerRefill     OperationKind = "eager-refill"
	OperationBudgetCharge    OperationKind = "budget-charge"
	OperationReviewCharge    OperationKind = "review-charge"
	OperationMaintenance     OperationKind = "maintenance"
	OperationReconciliation  OperationKind = "reconciliation"
	OperationStartup         OperationKind = "startup"
	OperationInline          OperationKind = "inline"
	OperationBootstrap       OperationKind = "bootstrap"
	OperationCrewPlaceholder OperationKind = "crew-placeholder"
	OperationCancellation    OperationKind = "cancellation"
)

// ArchiveHandoff prebinds a cancelled replacement to one exact successor
// archive intent. CQ-02I stores the facts; cancellation callers are downstream.
type ArchiveHandoff struct {
	ArchiveOrigin                     string `json:"archive_origin"`
	ArchiveKind                       string `json:"archive_kind"`
	SourceIdentity                    string `json:"source_identity"`
	SourceCandidateSHA256             string `json:"source_candidate_sha256"`
	DestinationBasename               string `json:"destination_basename"`
	SuccessorArchiveIntentID          string `json:"successor_archive_intent_id"`
	SuccessorArchiveIntentBytesBase64 string `json:"successor_archive_intent_bytes_base64"`
	SuccessorArchiveIntentSHA256      string `json:"successor_archive_intent_sha256"`
}

// ReplaceIntentV1 is the event-free universal replacement record. Completion
// receipt binding is intentionally fixed to null by CQ-02I.
type ReplaceIntentV1 struct {
	SchemaVersion            int             `json:"schema_version"`
	TransactionID            string          `json:"transaction_id"`
	OperationKind            OperationKind   `json:"operation_kind"`
	NormalizedName           string          `json:"normalized_name"`
	QueueID                  string          `json:"queue_id"`
	CanonicalBasename        string          `json:"canonical_basename"`
	PriorState               string          `json:"prior_state"`
	CandidateSHA256          string          `json:"candidate_sha256"`
	CandidateTempBasename    string          `json:"candidate_temp_basename"`
	WakeRequired             bool            `json:"wake_required"`
	CompletionReceiptBinding any             `json:"completion_receipt_binding,omitempty"`
	ArchiveHandoffBinding    *ArchiveHandoff `json:"archive_handoff_binding,omitempty"`
}

// ArchiveIntentV1 is the exact successor record for linked archive handoff.
type ArchiveIntentV1 struct {
	SchemaVersion            int    `json:"schema_version"`
	ArchiveIntentID          string `json:"archive_intent_id"`
	PredecessorTransactionID string `json:"predecessor_transaction_id"`
	ArchiveOrigin            string `json:"archive_origin"`
	ArchiveKind              string `json:"archive_kind"`
	NormalizedName           string `json:"normalized_name"`
	SourceIdentity           string `json:"source_identity"`
	SourceSHA256             string `json:"source_sha256"`
	DestinationBasename      string `json:"destination_basename"`
}

// ArchiveHandoffPlan is the semantic input for a linked cancellation archive.
// IDs and canonical successor bytes are deliberately absent: preparation
// allocates both IDs, builds the successor once, and binds those exact bytes
// into the predecessor.
type ArchiveHandoffPlan struct {
	ArchiveOrigin       string
	ArchiveKind         string
	SourceIdentity      string
	DestinationBasename string
}

// ArchiveRecoveryFacts are namespace facts independently observed by restart
// recovery. Requiring them prevents successor-only recovery from accepting a
// self-consistent but stale or third-destination record.
type ArchiveRecoveryFacts struct {
	NormalizedName      string
	SourceIdentity      string
	SourceSHA256        string
	DestinationBasename string
}

// ReplacementPlan contains exact bytes fixed before namespace I/O.
type ReplacementPlan struct {
	ProjectDir     string
	OperationKind  OperationKind
	NormalizedName string
	QueueID        string
	PriorBytes     []byte
	CandidateBytes []byte
	WakeRequired   bool
	ArchiveHandoff *ArchiveHandoffPlan
}

// ReplacementCommit is returned after executing a replacement plan.
type ReplacementCommit struct {
	NamespaceResult
	Intent ReplaceIntentV1
}

// ReplaceRecoveryAction is the only action permitted by an exact intent
// classifier. It never consults events or process-local generations.
type ReplaceRecoveryAction string

// Replace recovery actions are exhaustive over exact intent-bound facts.
const (
	ReplacePromoteCanonical ReplaceRecoveryAction = "promote_canonical"
	ReplaceRetryRename      ReplaceRecoveryAction = "retry_candidate_rename"
	ReplaceNotCommitted     ReplaceRecoveryAction = "not_committed_cleanup"
	ReplaceRefuse           ReplaceRecoveryAction = "refuse"
)

// LinkedHandoffAction classifies predecessor/successor archive facts.
type LinkedHandoffAction string

// Linked handoff actions are exhaustive over predecessor/successor presence.
const (
	LinkedCreateSuccessor LinkedHandoffAction = "create_successor"
	LinkedContinuePair    LinkedHandoffAction = "continue_exact_pair"
	LinkedContinueArchive LinkedHandoffAction = "continue_successor_only"
	LinkedRefuse          LinkedHandoffAction = "refuse"
)

type namespaceOps struct {
	mkdirAll  func(string, os.FileMode) error
	openFile  func(string, int, os.FileMode) (*os.File, error)
	write     func(io.Writer, []byte) error
	syncFile  func(*os.File) error
	closeFile func(*os.File) error
	rename    func(string, string) error
	link      func(string, string) error
	remove    func(string) error
	readFile  func(string) ([]byte, error)
	openDir   func(string) (*os.File, error)
	syncDir   func(*os.File) error
	closeDir  func(*os.File) error
}

type noReplaceState string

const (
	noReplaceInstalled     noReplaceState = "installed"
	noReplaceNotInstalled  noReplaceState = "not_installed"
	noReplaceRefused       noReplaceState = "refused"
	noReplaceIndeterminate noReplaceState = "indeterminate"
)

type noReplaceResult struct {
	State noReplaceState
	Err   error
}

func osNamespaceOps() namespaceOps {
	return namespaceOps{
		mkdirAll: os.MkdirAll,
		openFile: os.OpenFile,
		write: func(w io.Writer, data []byte) error {
			n, err := w.Write(data)
			if err != nil {
				return err
			}
			if n != len(data) {
				return io.ErrShortWrite
			}
			return nil
		},
		syncFile:  (*os.File).Sync,
		closeFile: (*os.File).Close,
		rename:    os.Rename,
		link:      os.Link,
		remove:    os.Remove,
		readFile:  os.ReadFile,
		openDir:   os.Open,
		syncDir:   (*os.File).Sync,
		closeDir:  (*os.File).Close,
	}
}

// WriteReplacement executes the event-free candidate/intent/canonical
// protocol. On committed success the exact replace intent remains for the
// QueueStore to remove after installing memory and advancing generation.
func WriteReplacement(ctx context.Context, plan ReplacementPlan) ReplacementCommit {
	return writeReplacement(ctx, plan, osNamespaceOps())
}

func writeReplacement(ctx context.Context, plan ReplacementPlan, ops namespaceOps) ReplacementCommit {
	intent, intentBytes, validationErr := prepareReplacement(plan)
	if validationErr != nil {
		return ReplacementCommit{NamespaceResult: NamespaceResult{Outcome: OutcomeRejected, Err: validationErr}}
	}
	if err := ctx.Err(); err != nil {
		return ReplacementCommit{NamespaceResult: NamespaceResult{Outcome: OutcomeRejected, Err: err}}
	}

	qDir := queuesDir(plan.ProjectDir)
	if err := ops.mkdirAll(qDir, 0o700); err != nil {
		return replacementFailure(intent, OutcomeNotCommitted, fmt.Errorf("mkdir queue directory: %w", err))
	}
	candidatePath := filepath.Join(qDir, intent.CandidateTempBasename)
	if err := durableFile(candidatePath, plan.CandidateBytes, ops); err != nil {
		return replacementFailure(intent, OutcomeNotCommitted, fmt.Errorf("candidate: %w", err))
	}

	intentPath := replaceIntentPath(plan.ProjectDir, plan.NormalizedName)
	install := durableNoReplace(intentPath, intentBytes, ops)
	switch install.State {
	case noReplaceNotInstalled:
		cleanupErr := ops.remove(candidatePath)
		return replacementFailure(
			intent,
			OutcomeNotCommitted,
			fmt.Errorf("replace intent: %w", errors.Join(install.Err, cleanupErr)),
		)
	case noReplaceRefused, noReplaceIndeterminate:
		return replacementFailure(
			intent,
			OutcomeCommitIndeterminate,
			fmt.Errorf("replace intent refused: %w", install.Err),
		)
	case noReplaceInstalled:
		// Continue only after the exact predecessor entry is selected.
	default:
		return replacementFailure(intent, OutcomeCommitIndeterminate, errors.New("unknown no-replace result"))
	}
	if err := syncDirectory(qDir, ops); err != nil {
		return replacementFailure(intent, OutcomeCommitIndeterminate, fmt.Errorf("sync replace intent: %w", err))
	}

	canonicalPath := queuePath(plan.ProjectDir, plan.NormalizedName)
	if err := ops.rename(candidatePath, canonicalPath); err != nil {
		return classifyReplacementFailure(plan.ProjectDir, intent, fmt.Errorf("rename candidate: %w", err), ops)
	}
	if err := syncDirectory(qDir, ops); err != nil {
		return replacementFailure(intent, OutcomeCommitIndeterminate, fmt.Errorf("sync canonical: %w", err))
	}
	return ReplacementCommit{
		NamespaceResult: NamespaceResult{Outcome: OutcomeCommittedDurable},
		Intent:          intent,
	}
}

func prepareReplacement(plan ReplacementPlan) (ReplaceIntentV1, []byte, error) {
	if err := validateArchiveOperationCoupling(plan.OperationKind, plan.ArchiveHandoff); err != nil {
		return ReplaceIntentV1{}, nil, err
	}
	transactionID, err := newUUIDv7()
	if err != nil {
		return ReplaceIntentV1{}, nil, fmt.Errorf("transaction id: %w", err)
	}
	var successorID string
	if plan.ArchiveHandoff != nil {
		successorID, err = newUUIDv7()
		if err != nil {
			return ReplaceIntentV1{}, nil, fmt.Errorf("successor archive intent id: %w", err)
		}
	}
	return prepareReplacementWithIDs(plan, transactionID, successorID)
}

// prepareReplacementWithIDs makes the allocation-before-serialization order
// explicit and gives tests a deterministic seam. Production callers use
// prepareReplacement, which allocates canonical UUIDv7 values first.
func prepareReplacementWithIDs(plan ReplacementPlan, transactionID, successorID string) (ReplaceIntentV1, []byte, error) {
	name := NormaliseQueueName(plan.NormalizedName)
	if ok, detail := ValidateQueueName(name); !ok {
		return ReplaceIntentV1{}, nil, fmt.Errorf("invalid normalized name: %s", detail)
	}
	if err := validateUUIDv7(transactionID); err != nil {
		return ReplaceIntentV1{}, nil, fmt.Errorf("transaction id: %w", err)
	}
	if plan.OperationKind == "" {
		return ReplaceIntentV1{}, nil, errors.New("operation kind is required")
	}
	if len(plan.CandidateBytes) > maxQueueFileBytes {
		return ReplaceIntentV1{}, nil, ErrTooLarge
	}
	candidate, err := UnmarshalQueue(plan.CandidateBytes)
	if err != nil {
		return ReplaceIntentV1{}, nil, fmt.Errorf("candidate queue: %w", err)
	}
	if NormaliseQueueName(candidate.Name) != name || candidate.QueueID != plan.QueueID {
		return ReplaceIntentV1{}, nil, errors.New("candidate identity does not match plan")
	}
	if err := validateCancellationCoupling(plan.OperationKind, candidate.Status, plan.ArchiveHandoff); err != nil {
		return ReplaceIntentV1{}, nil, err
	}
	prior := "absent"
	if plan.PriorBytes != nil {
		parsedPrior, parseErr := UnmarshalQueue(plan.PriorBytes)
		if parseErr != nil {
			return ReplaceIntentV1{}, nil, fmt.Errorf("prior queue: %w", parseErr)
		}
		if NormaliseQueueName(parsedPrior.Name) != name {
			return ReplaceIntentV1{}, nil, errors.New("prior name does not match plan")
		}
		prior = digestHex(plan.PriorBytes)
	}
	candidateDigest := digestHex(plan.CandidateBytes)
	candidateBase := fmt.Sprintf("%s.candidate-%s", name, transactionID)
	handoff, err := prepareArchiveHandoff(
		plan.ArchiveHandoff,
		successorID,
		transactionID,
		name,
		candidateDigest,
	)
	if err != nil {
		return ReplaceIntentV1{}, nil, err
	}
	intent := ReplaceIntentV1{
		SchemaVersion:         1,
		TransactionID:         transactionID,
		OperationKind:         plan.OperationKind,
		NormalizedName:        name,
		QueueID:               plan.QueueID,
		CanonicalBasename:     name + ".json",
		PriorState:            prior,
		CandidateSHA256:       candidateDigest,
		CandidateTempBasename: candidateBase,
		WakeRequired:          plan.WakeRequired,
		ArchiveHandoffBinding: handoff,
	}
	if err := validateReplaceIntent(intent); err != nil {
		return ReplaceIntentV1{}, nil, err
	}
	intentBytes, err := json.Marshal(intent)
	if err != nil {
		return ReplaceIntentV1{}, nil, fmt.Errorf("marshal replace intent: %w", err)
	}
	return intent, intentBytes, nil
}

func validateArchiveOperationCoupling(kind OperationKind, handoff *ArchiveHandoffPlan) error {
	if kind == OperationCancellation && handoff == nil {
		return errors.New("cancellation requires archive handoff")
	}
	if kind != OperationCancellation && handoff != nil {
		return errors.New("archive handoff requires cancellation operation")
	}
	return nil
}

func validateCancellationCoupling(
	kind OperationKind,
	status QueueStatus,
	handoff *ArchiveHandoffPlan,
) error {
	cancellation := kind == OperationCancellation
	if cancellation != (status == QueueStatusCancelled) || cancellation != (handoff != nil) {
		return errors.New("cancellation operation, cancelled candidate status, and archive handoff must occur together")
	}
	return nil
}

func prepareArchiveHandoff(
	plan *ArchiveHandoffPlan,
	successorID string,
	transactionID string,
	normalizedName string,
	candidateDigest string,
) (*ArchiveHandoff, error) {
	if plan == nil {
		if successorID != "" {
			return nil, errors.New("successor id without archive handoff")
		}
		return nil, nil
	}
	if err := validateUUIDv7(successorID); err != nil {
		return nil, fmt.Errorf("successor archive intent id: %w", err)
	}
	successor := ArchiveIntentV1{
		SchemaVersion:            1,
		ArchiveIntentID:          successorID,
		PredecessorTransactionID: transactionID,
		ArchiveOrigin:            plan.ArchiveOrigin,
		ArchiveKind:              plan.ArchiveKind,
		NormalizedName:           normalizedName,
		SourceIdentity:           plan.SourceIdentity,
		SourceSHA256:             candidateDigest,
		DestinationBasename:      plan.DestinationBasename,
	}
	facts := ArchiveRecoveryFacts{
		NormalizedName:      normalizedName,
		SourceIdentity:      plan.SourceIdentity,
		SourceSHA256:        candidateDigest,
		DestinationBasename: plan.DestinationBasename,
	}
	if err := validateArchiveIntent(successor, facts); err != nil {
		return nil, fmt.Errorf("archive handoff successor: %w", err)
	}
	successorBytes, err := json.Marshal(successor)
	if err != nil {
		return nil, fmt.Errorf("marshal archive handoff successor: %w", err)
	}
	return &ArchiveHandoff{
		ArchiveOrigin:                     plan.ArchiveOrigin,
		ArchiveKind:                       plan.ArchiveKind,
		SourceIdentity:                    plan.SourceIdentity,
		SourceCandidateSHA256:             candidateDigest,
		DestinationBasename:               plan.DestinationBasename,
		SuccessorArchiveIntentID:          successorID,
		SuccessorArchiveIntentBytesBase64: base64.StdEncoding.EncodeToString(successorBytes),
		SuccessorArchiveIntentSHA256:      digestHex(successorBytes),
	}, nil
}

func validateArchiveHandoff(
	h *ArchiveHandoff,
	candidateDigest string,
	predecessorTransactionID string,
	normalizedName string,
) error {
	if h == nil {
		return nil
	}
	if !archiveHandoffComplete(h) {
		return errors.New("archive handoff is incomplete")
	}
	if h.SourceCandidateSHA256 != candidateDigest {
		return errors.New("archive handoff source digest differs from candidate")
	}
	raw, err := base64.StdEncoding.DecodeString(h.SuccessorArchiveIntentBytesBase64)
	if err != nil || digestHex(raw) != h.SuccessorArchiveIntentSHA256 {
		return errors.New("archive handoff successor bytes/digest mismatch")
	}
	var successor ArchiveIntentV1
	if err := strictJSON(raw, &successor); err != nil {
		return fmt.Errorf("archive handoff successor: %w", err)
	}
	facts := ArchiveRecoveryFacts{
		NormalizedName:      normalizedName,
		SourceIdentity:      h.SourceIdentity,
		SourceSHA256:        h.SourceCandidateSHA256,
		DestinationBasename: h.DestinationBasename,
	}
	if err := validateArchiveIntent(successor, facts); err != nil {
		return fmt.Errorf("archive handoff successor: %w", err)
	}
	canonical, err := json.Marshal(successor)
	if err != nil || !bytes.Equal(canonical, raw) {
		return errors.New("archive handoff successor bytes are not canonical")
	}
	if !archiveSuccessorMatchesHandoff(successor, h, predecessorTransactionID, normalizedName) {
		return errors.New("archive handoff successor identity mismatch")
	}
	return nil
}

func archiveHandoffComplete(h *ArchiveHandoff) bool {
	return h.ArchiveOrigin != "" &&
		h.ArchiveKind != "" &&
		h.SourceIdentity != "" &&
		h.SourceCandidateSHA256 != "" &&
		h.DestinationBasename != "" &&
		h.SuccessorArchiveIntentID != "" &&
		h.SuccessorArchiveIntentBytesBase64 != "" &&
		h.SuccessorArchiveIntentSHA256 != ""
}

func archiveSuccessorMatchesHandoff(
	successor ArchiveIntentV1,
	h *ArchiveHandoff,
	predecessorTransactionID string,
	normalizedName string,
) bool {
	return successor.ArchiveIntentID == h.SuccessorArchiveIntentID &&
		successor.PredecessorTransactionID == predecessorTransactionID &&
		successor.ArchiveOrigin == h.ArchiveOrigin &&
		successor.ArchiveKind == h.ArchiveKind &&
		successor.NormalizedName == normalizedName &&
		successor.SourceIdentity == h.SourceIdentity &&
		successor.SourceSHA256 == h.SourceCandidateSHA256 &&
		successor.DestinationBasename == h.DestinationBasename
}

// ClassifyReplaceIntent strictly decodes canonical predecessor bytes before
// consulting only the exact paths and digests they bind.
func ClassifyReplaceIntent(projectDir string, intentBytes []byte) (ReplaceRecoveryAction, error) {
	intent, err := decodeReplaceIntent(intentBytes)
	if err != nil {
		return ReplaceRefuse, err
	}
	return classifyReplaceIntent(projectDir, intent, osNamespaceOps())
}

func decodeReplaceIntent(intentBytes []byte) (ReplaceIntentV1, error) {
	var intent ReplaceIntentV1
	if err := strictJSON(intentBytes, &intent); err != nil {
		return ReplaceIntentV1{}, fmt.Errorf("replace intent: %w", err)
	}
	if err := validateReplaceIntent(intent); err != nil {
		return ReplaceIntentV1{}, err
	}
	canonical, err := json.Marshal(intent)
	if err != nil || !bytes.Equal(canonical, intentBytes) {
		return ReplaceIntentV1{}, errors.New("replace intent bytes are not canonical")
	}
	return intent, nil
}

func classifyReplaceIntent(projectDir string, intent ReplaceIntentV1, ops namespaceOps) (ReplaceRecoveryAction, error) {
	if err := validateReplaceIntent(intent); err != nil {
		return ReplaceRefuse, err
	}
	qDir := queuesDir(projectDir)
	canonical, canonicalPresent, err := readOptional(filepath.Join(qDir, intent.CanonicalBasename), ops)
	if err != nil {
		return ReplaceRefuse, err
	}
	candidate, candidatePresent, err := readOptional(filepath.Join(qDir, intent.CandidateTempBasename), ops)
	if err != nil {
		return ReplaceRefuse, err
	}
	canonicalDigest := digestHex(canonical)
	candidateDigest := digestHex(candidate)
	switch {
	case canonicalPresent && canonicalDigest == intent.CandidateSHA256 && !candidatePresent:
		return ReplacePromoteCanonical, nil
	case candidatePresent && candidateDigest == intent.CandidateSHA256 &&
		priorMatches(intent.PriorState, canonical, canonicalPresent):
		return ReplaceRetryRename, nil
	case !candidatePresent && priorMatches(intent.PriorState, canonical, canonicalPresent):
		return ReplaceNotCommitted, nil
	default:
		return ReplaceRefuse, errors.New("replace intent facts are corrupt, mismatched, or third-state")
	}
}

func classifyReplacementFailure(projectDir string, intent ReplaceIntentV1, cause error, ops namespaceOps) ReplacementCommit {
	action, err := classifyReplaceIntent(projectDir, intent, ops)
	switch action {
	case ReplacePromoteCanonical:
		return replacementFailure(intent, OutcomeCommitIndeterminate, errors.Join(cause, err))
	case ReplaceRetryRename, ReplaceNotCommitted:
		return replacementFailure(intent, OutcomeNotCommitted, errors.Join(cause, err))
	default:
		return replacementFailure(intent, OutcomeCommitIndeterminate, errors.Join(cause, err))
	}
}

func replacementFailure(intent ReplaceIntentV1, outcome NamespaceOutcome, err error) ReplacementCommit {
	return ReplacementCommit{NamespaceResult: NamespaceResult{Outcome: outcome, Err: err}, Intent: intent}
}

// CleanupReplaceIntent removes the exact resolved intent and makes its absence
// directory-durable. It is valid only after committed canonical durability.
func CleanupReplaceIntent(projectDir, normalizedName string) error {
	ops := osNamespaceOps()
	path := replaceIntentPath(projectDir, NormaliseQueueName(normalizedName))
	if err := ops.remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return syncDirectory(queuesDir(projectDir), ops)
}

// InstallBoundArchiveIntent strictly decodes raw predecessor bytes, then
// installs their bound successor without reserializing caller-owned state.
func InstallBoundArchiveIntent(projectDir string, predecessorBytes []byte) NamespaceResult {
	predecessor, err := decodeReplaceIntent(predecessorBytes)
	if err != nil {
		return NamespaceResult{Outcome: OutcomeRejected, Err: err}
	}
	return installBoundArchiveIntent(projectDir, predecessor, osNamespaceOps())
}

func installBoundArchiveIntent(
	projectDir string,
	predecessor ReplaceIntentV1,
	ops namespaceOps,
) NamespaceResult {
	if err := validateReplaceIntent(predecessor); err != nil {
		return NamespaceResult{Outcome: OutcomeRejected, Err: err}
	}
	h := predecessor.ArchiveHandoffBinding
	if h == nil {
		return NamespaceResult{Outcome: OutcomeRejected, Err: errors.New("predecessor has no archive handoff")}
	}
	data, err := base64.StdEncoding.DecodeString(h.SuccessorArchiveIntentBytesBase64)
	if err != nil {
		return NamespaceResult{Outcome: OutcomeRejected, Err: err}
	}
	qDir := queuesDir(projectDir)
	if err := ops.mkdirAll(qDir, 0o700); err != nil {
		return NamespaceResult{Outcome: OutcomeNotCommitted, Err: err}
	}
	install := durableNoReplace(archiveIntentPath(projectDir, predecessor.NormalizedName), data, ops)
	switch install.State {
	case noReplaceNotInstalled:
		return NamespaceResult{Outcome: OutcomeNotCommitted, Err: install.Err}
	case noReplaceRefused, noReplaceIndeterminate:
		return NamespaceResult{Outcome: OutcomeCommitIndeterminate, Err: install.Err}
	case noReplaceInstalled:
		// Continue to establish parent durability.
	default:
		return NamespaceResult{Outcome: OutcomeCommitIndeterminate, Err: errors.New("unknown no-replace result")}
	}
	if err := syncDirectory(qDir, ops); err != nil {
		return NamespaceResult{Outcome: OutcomeCommitIndeterminate, Err: err}
	}
	return NamespaceResult{Outcome: OutcomeCommittedDurable}
}

// ClassifyLinkedHandoff strictly decodes any raw predecessor and refuses any
// pair not bound byte-for-byte. Empty predecessor bytes select successor-only
// recovery, which additionally requires exact namespace facts.
func ClassifyLinkedHandoff(
	predecessorBytes []byte,
	successorBytes []byte,
	facts ArchiveRecoveryFacts,
) (LinkedHandoffAction, error) {
	if err := validateArchiveRecoveryFacts(facts); err != nil {
		return LinkedRefuse, err
	}
	if len(predecessorBytes) == 0 {
		return classifySuccessorOnly(successorBytes, facts)
	}
	predecessor, err := decodeReplaceIntent(predecessorBytes)
	if err != nil {
		return LinkedRefuse, err
	}
	return classifyLinkedPair(predecessor, successorBytes, facts)
}

func classifySuccessorOnly(
	successorBytes []byte,
	facts ArchiveRecoveryFacts,
) (LinkedHandoffAction, error) {
	if len(successorBytes) == 0 {
		return LinkedRefuse, errors.New("no origin-bearing handoff record")
	}
	var successor ArchiveIntentV1
	if err := strictJSON(successorBytes, &successor); err != nil {
		return LinkedRefuse, fmt.Errorf("invalid successor archive intent: %w", err)
	}
	if err := validateArchiveIntent(successor, facts); err != nil {
		return LinkedRefuse, err
	}
	canonical, err := json.Marshal(successor)
	if err != nil || !bytes.Equal(canonical, successorBytes) {
		return LinkedRefuse, errors.New("successor archive intent bytes are not canonical")
	}
	return LinkedContinueArchive, nil
}

func classifyLinkedPair(
	predecessor ReplaceIntentV1,
	successorBytes []byte,
	facts ArchiveRecoveryFacts,
) (LinkedHandoffAction, error) {
	if err := validateReplaceIntent(predecessor); err != nil {
		return LinkedRefuse, err
	}
	h := predecessor.ArchiveHandoffBinding
	if h == nil {
		return LinkedRefuse, errors.New("predecessor has no archive handoff")
	}
	if predecessor.NormalizedName != facts.NormalizedName ||
		h.SourceIdentity != facts.SourceIdentity ||
		h.SourceCandidateSHA256 != facts.SourceSHA256 ||
		h.DestinationBasename != facts.DestinationBasename {
		return LinkedRefuse, errors.New("predecessor differs from recovery namespace facts")
	}
	if len(successorBytes) == 0 {
		return LinkedCreateSuccessor, nil
	}
	expected, err := base64.StdEncoding.DecodeString(h.SuccessorArchiveIntentBytesBase64)
	if err != nil || !bytes.Equal(expected, successorBytes) ||
		digestHex(successorBytes) != h.SuccessorArchiveIntentSHA256 {
		return LinkedRefuse, errors.New("successor differs from predecessor binding")
	}
	var successor ArchiveIntentV1
	if err := strictJSON(successorBytes, &successor); err != nil {
		return LinkedRefuse, err
	}
	if err := validateArchiveIntent(successor, facts); err != nil {
		return LinkedRefuse, err
	}
	if !archiveSuccessorMatchesHandoff(
		successor,
		h,
		predecessor.TransactionID,
		predecessor.NormalizedName,
	) {
		return LinkedRefuse, errors.New("successor predecessor identity mismatch")
	}
	return LinkedContinuePair, nil
}

func validateReplaceIntent(intent ReplaceIntentV1) error {
	if !validReplaceIntentEnvelope(intent) {
		return errors.New("invalid or unsupported replace intent")
	}
	if (intent.OperationKind == OperationCancellation) != (intent.ArchiveHandoffBinding != nil) {
		return errors.New("invalid cancellation/archive handoff coupling")
	}
	if ok, _ := ValidateQueueName(intent.NormalizedName); !ok ||
		NormaliseQueueName(intent.NormalizedName) != intent.NormalizedName {
		return errors.New("invalid or non-normalized replace intent name")
	}
	if intent.PriorState != "absent" && !validSHA256(intent.PriorState) {
		return errors.New("prior state must be absent or an exact digest")
	}
	return validateArchiveHandoff(
		intent.ArchiveHandoffBinding,
		intent.CandidateSHA256,
		intent.TransactionID,
		intent.NormalizedName,
	)
}

func validReplaceIntentEnvelope(intent ReplaceIntentV1) bool {
	return intent.SchemaVersion == 1 &&
		validateUUIDv7(intent.TransactionID) == nil &&
		validOperationKind(intent.OperationKind) &&
		intent.NormalizedName != "" &&
		validateUUIDv7(intent.QueueID) == nil &&
		intent.CanonicalBasename == intent.NormalizedName+".json" &&
		validSHA256(intent.CandidateSHA256) &&
		intent.CandidateTempBasename == intent.NormalizedName+".candidate-"+intent.TransactionID &&
		intent.CompletionReceiptBinding == nil
}

func validOperationKind(kind OperationKind) bool {
	switch kind {
	case OperationCreate,
		OperationSubmit,
		OperationAppend,
		OperationReservation,
		OperationRunIDPatch,
		OperationActivation,
		OperationAdvance,
		OperationPause,
		OperationResume,
		OperationEagerRefill,
		OperationBudgetCharge,
		OperationReviewCharge,
		OperationMaintenance,
		OperationReconciliation,
		OperationStartup,
		OperationInline,
		OperationBootstrap,
		OperationCrewPlaceholder,
		OperationCancellation:
		return true
	default:
		return false
	}
}

func validateArchiveIntent(intent ArchiveIntentV1, facts ArchiveRecoveryFacts) error {
	if intent.SchemaVersion != 1 ||
		validateUUIDv7(intent.ArchiveIntentID) != nil ||
		validateUUIDv7(intent.PredecessorTransactionID) != nil ||
		!validArchiveOrigin(intent.ArchiveOrigin) ||
		!validArchiveKind(intent.ArchiveKind) ||
		intent.NormalizedName != facts.NormalizedName ||
		intent.SourceIdentity != facts.SourceIdentity ||
		intent.SourceSHA256 != facts.SourceSHA256 ||
		intent.DestinationBasename != facts.DestinationBasename {
		return errors.New("invalid archive intent")
	}
	return validateArchiveRecoveryFacts(facts)
}

func validateArchiveRecoveryFacts(facts ArchiveRecoveryFacts) error {
	if ok, _ := ValidateQueueName(facts.NormalizedName); !ok ||
		NormaliseQueueName(facts.NormalizedName) != facts.NormalizedName {
		return errors.New("invalid archive recovery normalized name")
	}
	if facts.SourceIdentity == "" || !validSHA256(facts.SourceSHA256) {
		return errors.New("invalid archive recovery source facts")
	}
	if facts.DestinationBasename == "" ||
		facts.DestinationBasename == "." ||
		facts.DestinationBasename == ".." ||
		filepath.Base(facts.DestinationBasename) != facts.DestinationBasename {
		return errors.New("invalid archive recovery destination basename")
	}
	return nil
}

func validArchiveOrigin(origin string) bool {
	switch origin {
	case "operator-cancel", "graceful-shutdown", "inline-failure", "recovery-corrupt":
		return true
	default:
		return false
	}
}

func validArchiveKind(kind string) bool {
	switch kind {
	case "cancelled", "failed", "corrupt":
		return true
	default:
		return false
	}
}

func validSHA256(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func newUUIDv7() (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", err
	}
	return id.String(), nil
}

func validateUUIDv7(value string) error {
	id, err := uuid.Parse(value)
	if err != nil || id.String() != value || id.Version() != 7 {
		return errors.New("must be canonical lowercase UUIDv7")
	}
	return nil
}

func replaceIntentPath(projectDir, name string) string {
	return filepath.Join(queuesDir(projectDir), name+".replace-intent")
}

func archiveIntentPath(projectDir, name string) string {
	return filepath.Join(queuesDir(projectDir), name+".archive-intent")
}

func durableFile(path string, data []byte, ops namespaceOps) error {
	f, err := ops.openFile(path, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if err := ops.write(f, data); err != nil {
		return errors.Join(err, ops.closeFile(f), ops.remove(path))
	}
	if err := ops.syncFile(f); err != nil {
		return errors.Join(err, ops.closeFile(f), ops.remove(path))
	}
	if err := ops.closeFile(f); err != nil {
		return errors.Join(err, ops.remove(path))
	}
	return nil
}

func durableNoReplace(path string, data []byte, ops namespaceOps) noReplaceResult {
	existing, present, err := readOptional(path, ops)
	if err != nil {
		return noReplaceResult{State: noReplaceRefused, Err: err}
	}
	if present {
		if bytes.Equal(existing, data) {
			return noReplaceResult{State: noReplaceInstalled}
		}
		return noReplaceResult{State: noReplaceRefused, Err: errors.New("existing record differs")}
	}
	tmp := path + ".tmp-" + uniqueTmpSuffix()
	if err := durableFile(tmp, data, ops); err != nil {
		return noReplaceResult{State: noReplaceNotInstalled, Err: err}
	}
	if err := ops.link(tmp, path); err != nil {
		existing, present, readErr := readOptional(path, ops)
		if readErr == nil && present && bytes.Equal(existing, data) {
			if cleanupErr := ops.remove(tmp); cleanupErr != nil {
				return noReplaceResult{
					State: noReplaceIndeterminate,
					Err:   errors.Join(err, cleanupErr),
				}
			}
			return noReplaceResult{State: noReplaceInstalled}
		}
		if readErr != nil {
			return noReplaceResult{State: noReplaceIndeterminate, Err: errors.Join(err, readErr)}
		}
		if present {
			return noReplaceResult{State: noReplaceRefused, Err: errors.New("conflicting record appeared during install")}
		}
		return noReplaceResult{State: noReplaceNotInstalled, Err: errors.Join(err, ops.remove(tmp))}
	}
	if err := ops.remove(tmp); err != nil {
		return classifyNoReplaceCleanupFailure(path, tmp, data, err, ops)
	}
	return noReplaceResult{State: noReplaceInstalled}
}

func classifyNoReplaceCleanupFailure(
	path, tmp string,
	data []byte,
	cause error,
	ops namespaceOps,
) noReplaceResult {
	target, targetPresent, targetErr := readOptional(path, ops)
	temp, tempPresent, tempErr := readOptional(tmp, ops)
	factsErr := errors.Join(targetErr, tempErr)
	if targetErr == nil && (!targetPresent || !bytes.Equal(target, data)) {
		factsErr = errors.Join(factsErr, errors.New("installed record missing or differs after temp cleanup failure"))
	}
	if tempErr == nil && tempPresent && !bytes.Equal(temp, data) {
		factsErr = errors.Join(factsErr, errors.New("install temp differs after cleanup failure"))
	}
	return noReplaceResult{State: noReplaceIndeterminate, Err: errors.Join(cause, factsErr)}
}

func syncDirectory(path string, ops namespaceOps) error {
	dir, err := ops.openDir(path)
	if err != nil {
		return err
	}
	if err := ops.syncDir(dir); err != nil {
		return errors.Join(err, ops.closeDir(dir))
	}
	// Close after a successful fsync is diagnostic: durability is already known.
	_ = ops.closeDir(dir) //nolint:errcheck // successful directory fsync fixes the outcome
	return nil
}

func readOptional(path string, ops namespaceOps) (data []byte, present bool, err error) {
	data, err = ops.readFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return data, true, nil
}

func priorMatches(prior string, canonical []byte, present bool) bool {
	if prior == "absent" {
		return !present
	}
	return present && digestHex(canonical) == prior
}

func digestHex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func strictJSON(data []byte, dst any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("trailing JSON")
		}
		return fmt.Errorf("trailing JSON: %w", err)
	}
	return nil
}
