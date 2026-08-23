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
	"strings"
	"time"

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
	OperationFailedRecovery  OperationKind = "failed-recovery"
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
	OperationCompletion      OperationKind = "completion"
)

// CompletionReceiptBinding fixes the exact immutable completion receipt before
// the completed queue candidate reaches namespace I/O.
type CompletionReceiptBinding struct {
	ReceiptID            string `json:"receipt_id"`
	Basename             string `json:"basename"`
	SchemaVersion        int    `json:"schema_version"`
	CanonicalBytesBase64 string `json:"canonical_bytes_base64"`
	SHA256               string `json:"sha256"`
}

// CompletionReceipt is the immutable final-success authority from QM-005.
type CompletionReceipt struct {
	SchemaVersion        int    `json:"schema_version"`
	QueueID              string `json:"queue_id"`
	ReceiptID            string `json:"receipt_id"`
	TransactionID        string `json:"transaction_id"`
	NormalizedName       string `json:"normalized_name"`
	FinalGroupIndex      int    `json:"final_group_index"`
	FinalStatus          string `json:"final_status"`
	SuccessCount         int    `json:"success_count"`
	FailCount            int    `json:"fail_count"`
	CompletedAt          string `json:"completed_at"`
	CompletedQueueSHA256 string `json:"completed_queue_sha256"`
}

// CompletionReleaseMarker is the immutable retention anchor from QM-006.
type CompletionReleaseMarker struct {
	RecordType           string `json:"record_type"`
	SchemaVersion        int    `json:"schema_version"`
	QueueID              string `json:"queue_id"`
	ReceiptID            string `json:"receipt_id"`
	TransactionID        string `json:"transaction_id"`
	ReceiptSHA256        string `json:"receipt_sha256"`
	CompletedQueueSHA256 string `json:"completed_queue_sha256"`
	ReleasedAt           string `json:"released_at"`
	GCNotBefore          string `json:"gc_not_before"`
}

// CompletionPlan contains the exact completed candidate and receipt binding.
// It is a value-only result. Callers execute it through the queue transaction
// owner in a later step.
type CompletionPlan struct {
	TransactionID  string
	Candidate      Queue
	CandidateBytes []byte
	Receipt        CompletionReceipt
	ReceiptBytes   []byte
	Binding        *CompletionReceiptBinding
	MarkerInputs   CompletionReleaseMarkerInputs
}

// CompletionReleaseMarkerInputs are the facts fixed before release. The later
// release step supplies only its trusted timestamp.
type CompletionReleaseMarkerInputs struct {
	QueueID              string
	ReceiptID            string
	TransactionID        string
	ReceiptSHA256        string
	CompletedQueueSHA256 string
}

// CompletionReceiptBasename returns the exact flat-root receipt name.
func CompletionReceiptBasename(queueID, receiptID string) (string, error) {
	if validateUUIDv7(queueID) != nil || validateUUIDv7(receiptID) != nil {
		return "", errors.New("invalid completion receipt identity")
	}
	return queueID + "--" + receiptID + ".json", nil
}

// CompletionReleaseMarkerBasename returns the exact marker name for a receipt.
func CompletionReleaseMarkerBasename(queueID, receiptID string) (string, error) {
	basename, err := CompletionReceiptBasename(queueID, receiptID)
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(basename, ".json") + ".release-v1.json", nil
}

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

// ReplaceIntentV1 is the event-free universal replacement record.
type ReplaceIntentV1 struct {
	SchemaVersion                int                           `json:"schema_version"`
	TransactionID                string                        `json:"transaction_id"`
	OperationKind                OperationKind                 `json:"operation_kind"`
	NormalizedName               string                        `json:"normalized_name"`
	QueueID                      string                        `json:"queue_id"`
	CanonicalBasename            string                        `json:"canonical_basename"`
	PriorState                   string                        `json:"prior_state"`
	CandidateSHA256              string                        `json:"candidate_sha256"`
	CandidateTempBasename        string                        `json:"candidate_temp_basename"`
	WakeRequired                 bool                          `json:"wake_required"`
	CompletionReceiptBinding     *CompletionReceiptBinding     `json:"completion_receipt_binding,omitempty"`
	FailedRecoveryReceiptBinding *FailedRecoveryReceiptBinding `json:"failed_recovery_receipt_binding,omitempty"`
	ArchiveHandoffBinding        *ArchiveHandoff               `json:"archive_handoff_binding,omitempty"`
}

// FailedRecoveryReceiptBinding fixes the exact durable receipt that proves a
// failed queue recovery. The replace intent owns this binding until both the
// recovered queue and receipt are durable.
//
// Spec ref: specs/queue-model.md QM-058a.
type FailedRecoveryReceiptBinding struct {
	ReceiptID            string `json:"receipt_id"`
	TransactionID        string `json:"transaction_id"`
	Basename             string `json:"basename"`
	SchemaVersion        int    `json:"schema_version"`
	CanonicalBytesBase64 string `json:"canonical_bytes_base64"`
	SHA256               string `json:"sha256"`
}

// FailedRecoveryReceipt is the immutable record for one failed queue recovery.
// Its bytes are bound into the same replace intent as the recovered queue.
type FailedRecoveryReceipt struct {
	SchemaVersion        int                  `json:"schema_version"`
	RecordType           string               `json:"record_type"`
	QueueID              string               `json:"queue_id"`
	ReceiptID            string               `json:"receipt_id"`
	TransactionID        string               `json:"transaction_id"`
	NormalizedName       string               `json:"normalized_name"`
	PriorQueueSHA256     string               `json:"prior_queue_sha256"`
	RecoveredQueueSHA256 string               `json:"recovered_queue_sha256"`
	RecoveredItems       []FailedRecoveryItem `json:"recovered_items"`
	RecoveredAt          string               `json:"recovered_at"`
}

// FailedRecoveryItem names one failed item that recovery re-armed.
type FailedRecoveryItem struct {
	BeadID       string  `json:"bead_id"`
	RetiredRunID *string `json:"retired_run_id"`
}

// FailedRecoveryPlan contains the exact candidate and receipt binding that a
// QueueStore writes for one paused-by-failure queue.
type FailedRecoveryPlan struct {
	TransactionID string
	Candidate     Queue
	Receipt       FailedRecoveryReceipt
	Binding       *FailedRecoveryReceiptBinding
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
	ProjectDir                   string
	TransactionID                string
	OperationKind                OperationKind
	NormalizedName               string
	QueueID                      string
	PriorBytes                   []byte
	CandidateBytes               []byte
	WakeRequired                 bool
	ArchiveHandoff               *ArchiveHandoffPlan
	FailedRecoveryReceiptBinding *FailedRecoveryReceiptBinding
	CompletionReceiptBinding     *CompletionReceiptBinding
}

// ReplacementCommit is returned after executing a replacement plan.
type ReplacementCommit struct {
	NamespaceResult
	Intent ReplaceIntentV1
	Phase  CompletionPhase
}

// CompletionPhase identifies the last completed QM-053 boundary.
type CompletionPhase string

// CompletionPhase values identify the last completed QM-053 boundary.
const (
	CompletionPhaseRejected             CompletionPhase = "rejected"
	CompletionPhaseNotCommitted         CompletionPhase = "not_committed"
	CompletionPhaseCommitIndeterminate  CompletionPhase = "commit_indeterminate"
	CompletionPhaseCanonicalCommitted   CompletionPhase = "canonical_committed"
	CompletionPhaseReceiptDurable       CompletionPhase = "receipt_durable"
	CompletionPhaseObservationAttempted CompletionPhase = "observation_attempted"
	CompletionPhaseCleaned              CompletionPhase = "cleaned"
	CompletionPhaseOwnershipReleased    CompletionPhase = "ownership_released"
	CompletionPhaseMarkerDurable        CompletionPhase = "marker_durable"
	CompletionPhaseMarkerFailed         CompletionPhase = "marker_failed"
)

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
	readDir   func(string) ([]os.DirEntry, error)
	lstat     func(string) (os.FileInfo, error)
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
		readDir:   os.ReadDir,
		lstat:     os.Lstat,
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
		return ReplacementCommit{NamespaceResult: NamespaceResult{Outcome: OutcomeRejected, Err: validationErr}, Phase: CompletionPhaseRejected}
	}
	if err := ctx.Err(); err != nil {
		return ReplacementCommit{NamespaceResult: NamespaceResult{Outcome: OutcomeRejected, Err: err}, Phase: CompletionPhaseRejected}
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
	case noReplaceRefused:
		cleanupErr := ops.remove(candidatePath)
		return replacementFailure(
			intent,
			OutcomeCommitIndeterminate,
			fmt.Errorf("replace intent refused: %w", errors.Join(install.Err, cleanupErr)),
		)
	case noReplaceIndeterminate:
		return replacementFailure(
			intent,
			OutcomeCommitIndeterminate,
			fmt.Errorf("replace intent indeterminate: %w", install.Err),
		)
	case noReplaceInstalled:
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
	if intent.FailedRecoveryReceiptBinding != nil {
		if err := writeFailedRecoveryReceipt(
			plan.ProjectDir,
			intent.QueueID,
			intent.TransactionID,
			intent.FailedRecoveryReceiptBinding,
			ops,
		); err != nil {
			return replacementFailure(intent, OutcomeCommitIndeterminate, fmt.Errorf("failed recovery receipt: %w", err))
		}
	}
	phase := CompletionPhase("")
	if intent.CompletionReceiptBinding != nil {
		phase = CompletionPhaseCanonicalCommitted
		if err := writeCompletionReceipt(
			plan.ProjectDir,
			intent.QueueID,
			intent.TransactionID,
			intent.CompletionReceiptBinding,
			ops,
		); err != nil {
			return ReplacementCommit{
				NamespaceResult: NamespaceResult{Outcome: OutcomeCommitIndeterminate, Err: fmt.Errorf("completion receipt: %w", err)},
				Intent:          intent,
				Phase:           phase,
			}
		}
		phase = CompletionPhaseReceiptDurable
	}
	return ReplacementCommit{
		NamespaceResult: NamespaceResult{Outcome: OutcomeCommittedDurable},
		Intent:          intent,
		Phase:           phase,
	}
}

func prepareReplacement(plan ReplacementPlan) (ReplaceIntentV1, []byte, error) {
	if err := validateArchiveOperationCoupling(plan.OperationKind, plan.ArchiveHandoff); err != nil {
		return ReplaceIntentV1{}, nil, err
	}
	transactionID := plan.TransactionID
	if transactionID == "" {
		var err error
		transactionID, err = newUUIDv7()
		if err != nil {
			return ReplaceIntentV1{}, nil, fmt.Errorf("transaction id: %w", err)
		}
	}
	plan.TransactionID = transactionID
	var successorID string
	if plan.ArchiveHandoff != nil {
		var err error
		successorID, err = newUUIDv7()
		if err != nil {
			return ReplaceIntentV1{}, nil, fmt.Errorf("successor archive intent id: %w", err)
		}
	}
	return prepareReplacementWithIDs(plan, transactionID, successorID)
}

func prepareReplacementWithIDs(plan ReplacementPlan, transactionID, successorID string) (ReplaceIntentV1, []byte, error) {
	plan.TransactionID = transactionID
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
		if err := validateFailedRecoveryPlan(
			plan,
			parsedPrior,
			candidate,
			prior,
			digestHex(plan.CandidateBytes),
		); err != nil {
			return ReplaceIntentV1{}, nil, err
		}
	} else if err := validateFailedRecoveryPlan(
		plan,
		Queue{},
		candidate,
		prior,
		digestHex(plan.CandidateBytes),
	); err != nil {
		return ReplaceIntentV1{}, nil, err
	}
	candidateDigest := digestHex(plan.CandidateBytes)
	if err := validateCompletionPlan(plan, candidate, candidateDigest); err != nil {
		return ReplaceIntentV1{}, nil, err
	}
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
		SchemaVersion:                1,
		TransactionID:                transactionID,
		OperationKind:                plan.OperationKind,
		NormalizedName:               name,
		QueueID:                      plan.QueueID,
		CanonicalBasename:            name + ".json",
		PriorState:                   prior,
		CandidateSHA256:              candidateDigest,
		CandidateTempBasename:        candidateBase,
		WakeRequired:                 plan.WakeRequired,
		FailedRecoveryReceiptBinding: plan.FailedRecoveryReceiptBinding,
		CompletionReceiptBinding:     plan.CompletionReceiptBinding,
		ArchiveHandoffBinding:        handoff,
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

func validateFailedRecoveryPlan(
	plan ReplacementPlan,
	prior, candidate Queue,
	priorSHA256, candidateSHA256 string,
) error {
	binding := plan.FailedRecoveryReceiptBinding
	if plan.OperationKind != OperationFailedRecovery {
		if binding != nil {
			return errors.New("failed recovery receipt binding requires failed-recovery operation")
		}
		return nil
	}
	if binding == nil {
		return errors.New("failed-recovery operation requires receipt binding")
	}
	if prior.Status != QueueStatusPausedByFailure || candidate.Status != QueueStatusActive {
		return errors.New("failed-recovery requires paused-by-failure prior and active candidate")
	}
	if candidate.FailedRecoveryReceiptID == nil || *candidate.FailedRecoveryReceiptID != binding.ReceiptID {
		return errors.New("failed-recovery candidate does not bind receipt ID")
	}
	return validateFailedRecoveryReceiptBinding(
		binding,
		candidate.QueueID,
		plan.TransactionID,
		plan.NormalizedName,
		priorSHA256,
		candidateSHA256,
	)
}

func validateCompletionPlan(plan ReplacementPlan, candidate Queue, candidateSHA256 string) error {
	binding := plan.CompletionReceiptBinding
	if plan.OperationKind != OperationCompletion {
		if binding != nil {
			return errors.New("completion receipt binding requires completion operation")
		}
		return nil
	}
	if binding == nil {
		return errors.New("completion operation requires receipt binding")
	}
	if candidate.Status != QueueStatusCompleted {
		return errors.New("completion candidate must have completed status")
	}
	if len(candidate.Groups) == 0 {
		return errors.New("completion candidate requires a final group")
	}
	if err := validateCompletionReceiptBinding(
		binding,
		candidate.QueueID,
		plan.TransactionID,
		plan.NormalizedName,
		candidateSHA256,
	); err != nil {
		return err
	}
	receipt, err := decodeBoundCompletionReceipt(binding)
	if err != nil {
		return err
	}
	finalGroup := candidate.Groups[len(candidate.Groups)-1]
	if finalGroup.GroupIndex != receipt.FinalGroupIndex ||
		len(finalGroup.Items) != receipt.SuccessCount ||
		finalGroup.CompletedAt == nil ||
		finalGroup.CompletedAt.UTC().Truncate(time.Millisecond).Format("2006-01-02T15:04:05.000Z") != receipt.CompletedAt {
		return errors.New("completion receipt final group facts do not match candidate")
	}
	for _, item := range finalGroup.Items {
		if item.Status != ItemStatusCompleted {
			return errors.New("completion receipt candidate contains non-completed final item")
		}
	}
	return nil
}

func validateCompletionReceiptBinding(
	binding *CompletionReceiptBinding,
	queueID, transactionID, normalizedName, completedQueueSHA256 string,
) error {
	if binding == nil || binding.SchemaVersion != 1 ||
		validateUUIDv7(binding.ReceiptID) != nil ||
		binding.Basename != queueID+"--"+binding.ReceiptID+".json" ||
		!validSHA256(binding.SHA256) ||
		!validSHA256(completedQueueSHA256) {
		return errors.New("invalid completion receipt binding")
	}
	receipt, err := decodeBoundCompletionReceipt(binding)
	if err != nil {
		return err
	}
	if receipt.SchemaVersion != binding.SchemaVersion ||
		receipt.QueueID != queueID ||
		receipt.ReceiptID != binding.ReceiptID ||
		receipt.TransactionID != transactionID ||
		receipt.NormalizedName != normalizedName ||
		receipt.CompletedQueueSHA256 != completedQueueSHA256 {
		return errors.New("completion receipt does not match binding")
	}
	return nil
}

func decodeBoundCompletionReceipt(binding *CompletionReceiptBinding) (CompletionReceipt, error) {
	data, err := base64.StdEncoding.DecodeString(binding.CanonicalBytesBase64)
	if err != nil || len(data) == 0 || digestHex(data) != binding.SHA256 {
		return CompletionReceipt{}, errors.New("invalid completion receipt bytes")
	}
	return DecodeCompletionReceipt(data)
}

// DecodeCompletionReceipt accepts only canonical receipt v1 bytes.
func DecodeCompletionReceipt(data []byte) (CompletionReceipt, error) {
	var receipt CompletionReceipt
	if err := strictJSON(data, &receipt); err != nil {
		return CompletionReceipt{}, fmt.Errorf("completion receipt: %w", err)
	}
	canonical, err := json.Marshal(receipt)
	if err != nil || !bytes.Equal(canonical, data) {
		return CompletionReceipt{}, errors.New("completion receipt bytes are not canonical")
	}
	if receipt.SchemaVersion != 1 ||
		validateUUIDv7(receipt.QueueID) != nil ||
		validateUUIDv7(receipt.ReceiptID) != nil ||
		validateUUIDv7(receipt.TransactionID) != nil ||
		receipt.NormalizedName == "" ||
		NormaliseQueueName(receipt.NormalizedName) != receipt.NormalizedName ||
		receipt.FinalGroupIndex < 0 ||
		receipt.FinalStatus != string(GroupStatusCompleteSuccess) ||
		receipt.SuccessCount < 0 || receipt.FailCount != 0 ||
		!validRecoveryTimestamp(receipt.CompletedAt) ||
		!validSHA256(receipt.CompletedQueueSHA256) {
		return CompletionReceipt{}, errors.New("invalid or unsupported completion receipt")
	}
	if ok, _ := ValidateQueueName(receipt.NormalizedName); !ok {
		return CompletionReceipt{}, errors.New("invalid completion receipt queue name")
	}
	return receipt, nil
}

func validateCompletionReleaseMarker(marker CompletionReleaseMarker) error {
	if marker.RecordType != "completion-release" || marker.SchemaVersion != 1 ||
		validateUUIDv7(marker.QueueID) != nil ||
		validateUUIDv7(marker.ReceiptID) != nil ||
		validateUUIDv7(marker.TransactionID) != nil ||
		!validSHA256(marker.ReceiptSHA256) ||
		!validSHA256(marker.CompletedQueueSHA256) {
		return errors.New("invalid completion release marker")
	}
	releasedAt, releasedOK := parseRecoveryTimestamp(marker.ReleasedAt)
	gcNotBefore, gcOK := parseRecoveryTimestamp(marker.GCNotBefore)
	if !releasedOK || !gcOK {
		return errors.New("invalid completion release marker")
	}
	want := releasedAt.Add(720 * time.Hour)
	if want.Before(releasedAt) || !gcNotBefore.Equal(want) {
		return errors.New("invalid completion release retention interval")
	}
	return nil
}

// DecodeCompletionReleaseMarker accepts only canonical marker v1 bytes.
func DecodeCompletionReleaseMarker(data []byte) (CompletionReleaseMarker, error) {
	var marker CompletionReleaseMarker
	if err := strictJSON(data, &marker); err != nil {
		return CompletionReleaseMarker{}, fmt.Errorf("completion release marker: %w", err)
	}
	canonical, err := json.Marshal(marker)
	if err != nil || !bytes.Equal(canonical, data) {
		return CompletionReleaseMarker{}, errors.New("completion release marker bytes are not canonical")
	}
	if err := validateCompletionReleaseMarker(marker); err != nil {
		return CompletionReleaseMarker{}, err
	}
	return marker, nil
}

// PrepareCompletion builds exact completed queue and receipt bytes from
// supplied identities and time. It performs no clock, UUID, or filesystem I/O.
func PrepareCompletion(
	prior Queue,
	transactionID, receiptID string,
	completedAt time.Time,
) (CompletionPlan, error) {
	if err := validateUUIDv7(transactionID); err != nil {
		return CompletionPlan{}, fmt.Errorf("completion transaction id: %w", err)
	}
	if err := validateUUIDv7(receiptID); err != nil {
		return CompletionPlan{}, fmt.Errorf("completion receipt id: %w", err)
	}
	if len(prior.Groups) == 0 {
		return CompletionPlan{}, errors.New("completion requires at least one group")
	}
	candidate := *CloneQueue(&prior)
	candidate.Name = NormaliseQueueName(candidate.Name)
	if err := CompleteQueue(&candidate); err != nil {
		return CompletionPlan{}, fmt.Errorf("complete queue: %w", err)
	}
	completedAt = completedAt.UTC().Truncate(time.Millisecond)
	finalGroup := &candidate.Groups[len(candidate.Groups)-1]
	if finalGroup.CompletedAt == nil || !finalGroup.CompletedAt.UTC().Truncate(time.Millisecond).Equal(completedAt) {
		return CompletionPlan{}, errors.New("completion time does not match final group")
	}
	for _, item := range finalGroup.Items {
		if item.Status != ItemStatusCompleted {
			return CompletionPlan{}, errors.New("completion final group contains non-completed item")
		}
	}
	finalGroup.CompletedAt = &completedAt
	candidateBytes, err := json.Marshal(candidate)
	if err != nil {
		return CompletionPlan{}, fmt.Errorf("marshal completed queue: %w", err)
	}
	receipt := CompletionReceipt{
		SchemaVersion:        1,
		QueueID:              candidate.QueueID,
		ReceiptID:            receiptID,
		TransactionID:        transactionID,
		NormalizedName:       candidate.Name,
		FinalGroupIndex:      finalGroup.GroupIndex,
		FinalStatus:          string(GroupStatusCompleteSuccess),
		SuccessCount:         len(finalGroup.Items),
		FailCount:            0,
		CompletedAt:          completedAt.Format("2006-01-02T15:04:05.000Z"),
		CompletedQueueSHA256: digestHex(candidateBytes),
	}
	receiptBytes, err := json.Marshal(receipt)
	if err != nil {
		return CompletionPlan{}, fmt.Errorf("marshal completion receipt: %w", err)
	}
	binding := &CompletionReceiptBinding{
		ReceiptID:            receiptID,
		Basename:             candidate.QueueID + "--" + receiptID + ".json",
		SchemaVersion:        1,
		CanonicalBytesBase64: base64.StdEncoding.EncodeToString(receiptBytes),
		SHA256:               digestHex(receiptBytes),
	}
	if err := validateCompletionReceiptBinding(
		binding,
		candidate.QueueID,
		transactionID,
		candidate.Name,
		digestHex(candidateBytes),
	); err != nil {
		return CompletionPlan{}, err
	}
	return CompletionPlan{
		TransactionID:  transactionID,
		Candidate:      candidate,
		CandidateBytes: append([]byte(nil), candidateBytes...),
		Receipt:        receipt,
		ReceiptBytes:   append([]byte(nil), receiptBytes...),
		Binding:        binding,
		MarkerInputs: CompletionReleaseMarkerInputs{
			QueueID:              candidate.QueueID,
			ReceiptID:            receiptID,
			TransactionID:        transactionID,
			ReceiptSHA256:        binding.SHA256,
			CompletedQueueSHA256: receipt.CompletedQueueSHA256,
		},
	}, nil
}

func validateFailedRecoveryReceiptBinding(
	binding *FailedRecoveryReceiptBinding,
	queueID string,
	transactionID string,
	normalizedName string,
	priorSHA256 string,
	recoveredSHA256 string,
) error {
	if binding == nil || binding.SchemaVersion != 1 ||
		validateUUIDv7(binding.ReceiptID) != nil ||
		validateUUIDv7(binding.TransactionID) != nil ||
		binding.TransactionID != transactionID ||
		binding.Basename != queueID+"--"+binding.ReceiptID+".json" ||
		!validSHA256(binding.SHA256) ||
		!validSHA256(priorSHA256) ||
		!validSHA256(recoveredSHA256) {
		return errors.New("invalid failed recovery receipt binding")
	}
	data, err := base64.StdEncoding.DecodeString(binding.CanonicalBytesBase64)
	if err != nil || len(data) == 0 || digestHex(data) != binding.SHA256 {
		return errors.New("invalid failed recovery receipt bytes")
	}
	var receipt FailedRecoveryReceipt
	if err := strictJSON(data, &receipt); err != nil ||
		receipt.SchemaVersion != binding.SchemaVersion ||
		receipt.RecordType != "failed-recovery" ||
		receipt.QueueID != queueID ||
		receipt.ReceiptID != binding.ReceiptID ||
		receipt.TransactionID != transactionID ||
		receipt.NormalizedName != normalizedName ||
		receipt.PriorQueueSHA256 != priorSHA256 ||
		receipt.RecoveredQueueSHA256 != recoveredSHA256 ||
		receipt.RecoveredItems == nil ||
		!validRecoveryTimestamp(receipt.RecoveredAt) ||
		!validFailedRecoveryItems(receipt.RecoveredItems) {
		return errors.New("failed recovery receipt does not match binding")
	}
	return nil
}

func validRecoveryTimestamp(value string) bool {
	_, ok := parseRecoveryTimestamp(value)
	return ok
}

func parseRecoveryTimestamp(value string) (time.Time, bool) {
	parsed, err := time.Parse("2006-01-02T15:04:05.000Z", value)
	if err != nil || parsed.UTC().Format("2006-01-02T15:04:05.000Z") != value {
		return time.Time{}, false
	}
	return parsed, true
}

func validFailedRecoveryItems(items []FailedRecoveryItem) bool {
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		if item.BeadID == "" {
			return false
		}
		if _, ok := seen[item.BeadID]; ok {
			return false
		}
		seen[item.BeadID] = struct{}{}
		if item.RetiredRunID != nil {
			id, err := uuid.Parse(*item.RetiredRunID)
			if err != nil || id.String() != *item.RetiredRunID {
				return false
			}
		}
	}
	return true
}

// PrepareFailedRecovery creates the candidate queue and immutable receipt for
// one paused-by-failure queue. The caller must pass its exact snapshot to the
// QueueStore transaction that writes this plan.
func PrepareFailedRecovery(prior Queue, recoveredAt time.Time) (FailedRecoveryPlan, error) {
	if prior.Status != QueueStatusPausedByFailure {
		return FailedRecoveryPlan{}, errors.New("failed recovery requires paused-by-failure queue")
	}
	priorBytes, err := json.Marshal(prior)
	if err != nil {
		return FailedRecoveryPlan{}, fmt.Errorf("marshal failed recovery prior: %w", err)
	}
	candidate := *CloneQueue(&prior)
	recoveredItems := failedRecoveryItems(candidate)
	if _, ok := ResumeFromFailure(&candidate); !ok {
		return FailedRecoveryPlan{}, errors.New("resume paused-by-failure queue")
	}
	receiptID, err := newUUIDv7()
	if err != nil {
		return FailedRecoveryPlan{}, fmt.Errorf("failed recovery receipt id: %w", err)
	}
	transactionID, err := newUUIDv7()
	if err != nil {
		return FailedRecoveryPlan{}, fmt.Errorf("failed recovery transaction id: %w", err)
	}
	candidate.FailedRecoveryReceiptID = &receiptID
	candidate.Name = NormaliseQueueName(candidate.Name)
	candidateBytes, err := json.Marshal(candidate)
	if err != nil {
		return FailedRecoveryPlan{}, fmt.Errorf("marshal recovered queue: %w", err)
	}
	recoveredAt = recoveredAt.UTC().Truncate(time.Millisecond)
	receipt := FailedRecoveryReceipt{
		SchemaVersion:        1,
		RecordType:           "failed-recovery",
		QueueID:              candidate.QueueID,
		ReceiptID:            receiptID,
		TransactionID:        transactionID,
		NormalizedName:       candidate.Name,
		PriorQueueSHA256:     digestHex(priorBytes),
		RecoveredQueueSHA256: digestHex(candidateBytes),
		RecoveredItems:       recoveredItems,
		RecoveredAt:          recoveredAt.Format("2006-01-02T15:04:05.000Z"),
	}
	receiptBytes, err := json.Marshal(receipt)
	if err != nil {
		return FailedRecoveryPlan{}, fmt.Errorf("marshal failed recovery receipt: %w", err)
	}
	binding := &FailedRecoveryReceiptBinding{
		ReceiptID:            receiptID,
		TransactionID:        transactionID,
		Basename:             candidate.QueueID + "--" + receiptID + ".json",
		SchemaVersion:        receipt.SchemaVersion,
		CanonicalBytesBase64: base64.StdEncoding.EncodeToString(receiptBytes),
		SHA256:               digestHex(receiptBytes),
	}
	if err := validateFailedRecoveryReceiptBinding(
		binding,
		candidate.QueueID,
		transactionID,
		candidate.Name,
		digestHex(priorBytes),
		digestHex(candidateBytes),
	); err != nil {
		return FailedRecoveryPlan{}, err
	}
	return FailedRecoveryPlan{
		TransactionID: transactionID,
		Candidate:     candidate,
		Receipt:       receipt,
		Binding:       binding,
	}, nil
}

func failedRecoveryItems(q Queue) []FailedRecoveryItem {
	items := make([]FailedRecoveryItem, 0)
	for _, group := range q.Groups {
		for _, item := range group.Items {
			if item.Status != ItemStatusFailed {
				continue
			}
			entry := FailedRecoveryItem{BeadID: string(item.BeadID)}
			if item.RunID != nil {
				runID := *item.RunID
				entry.RetiredRunID = &runID
			}
			items = append(items, entry)
		}
	}
	return items
}

// ReadFailedRecoveryReceipt reads the immutable receipt named by a recovered
// queue. It verifies the receipt identity and the recovered queue digest.
func ReadFailedRecoveryReceipt(projectDir string, recovered Queue) (FailedRecoveryReceipt, []byte, error) {
	if recovered.FailedRecoveryReceiptID == nil {
		return FailedRecoveryReceipt{}, nil, errors.New("recovered queue has no failed recovery receipt ID")
	}
	name := NormaliseQueueName(recovered.Name)
	recovered.Name = name
	recoveredBytes, err := json.Marshal(recovered)
	if err != nil {
		return FailedRecoveryReceipt{}, nil, fmt.Errorf("marshal recovered queue: %w", err)
	}
	path := filepath.Join(
		failedRecoveryReceiptsDir(projectDir),
		recovered.QueueID+"--"+*recovered.FailedRecoveryReceiptID+".json",
	)
	data, err := os.ReadFile(path)
	if err != nil {
		return FailedRecoveryReceipt{}, nil, err
	}
	var receipt FailedRecoveryReceipt
	if err := strictJSON(data, &receipt); err != nil {
		return FailedRecoveryReceipt{}, nil, fmt.Errorf("decode failed recovery receipt: %w", err)
	}
	binding := &FailedRecoveryReceiptBinding{
		ReceiptID:            receipt.ReceiptID,
		TransactionID:        receipt.TransactionID,
		Basename:             filepath.Base(path),
		SchemaVersion:        receipt.SchemaVersion,
		CanonicalBytesBase64: base64.StdEncoding.EncodeToString(data),
		SHA256:               digestHex(data),
	}
	if err := validateFailedRecoveryReceiptBinding(
		binding,
		recovered.QueueID,
		receipt.TransactionID,
		name,
		receipt.PriorQueueSHA256,
		digestHex(recoveredBytes),
	); err != nil {
		return FailedRecoveryReceipt{}, nil, err
	}
	if receipt.ReceiptID != *recovered.FailedRecoveryReceiptID {
		return FailedRecoveryReceipt{}, nil, errors.New("failed recovery receipt ID does not match queue")
	}
	return receipt, data, nil
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
	if intent.FailedRecoveryReceiptBinding != nil {
		return classifyFailedRecoveryIntent(projectDir, intent, ops)
	}
	if intent.CompletionReceiptBinding != nil {
		return classifyCompletionIntent(projectDir, intent, ops)
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

func classifyCompletionIntent(
	projectDir string,
	intent ReplaceIntentV1,
	ops namespaceOps,
) (ReplaceRecoveryAction, error) {
	facts, err := loadCompletionRecoveryFacts(projectDir, intent, ops)
	if err != nil {
		return ReplaceRefuse, err
	}
	switch {
	case facts.canPromoteCanonical(intent):
		return ReplacePromoteCanonical, nil
	case facts.canFinishCleanup():
		return ReplacePromoteCanonical, nil
	case facts.canRetryRename(intent):
		return ReplaceRetryRename, nil
	case facts.canRollBack(intent):
		return ReplaceNotCommitted, nil
	default:
		return ReplaceRefuse, errors.New("completion intent facts are corrupt, mismatched, or third-state")
	}
}

type completionRecoveryFacts struct {
	canonical        []byte
	canonicalPresent bool
	candidate        []byte
	candidatePresent bool
	receiptPresent   bool
	receiptExact     bool
}

func loadCompletionRecoveryFacts(
	projectDir string,
	intent ReplaceIntentV1,
	ops namespaceOps,
) (completionRecoveryFacts, error) {
	qDir := queuesDir(projectDir)
	canonical, canonicalPresent, err := readOptional(filepath.Join(qDir, intent.CanonicalBasename), ops)
	if err != nil {
		return completionRecoveryFacts{}, err
	}
	candidate, candidatePresent, err := readOptional(filepath.Join(qDir, intent.CandidateTempBasename), ops)
	if err != nil {
		return completionRecoveryFacts{}, err
	}
	binding := intent.CompletionReceiptBinding
	receipt, receiptPresent, err := readOptional(filepath.Join(completionReceiptsDir(projectDir), binding.Basename), ops)
	if err != nil {
		return completionRecoveryFacts{}, err
	}
	expectedReceipt, err := base64.StdEncoding.DecodeString(binding.CanonicalBytesBase64)
	if err != nil {
		return completionRecoveryFacts{}, err
	}
	return completionRecoveryFacts{
		canonical:        canonical,
		canonicalPresent: canonicalPresent,
		candidate:        candidate,
		candidatePresent: candidatePresent,
		receiptPresent:   receiptPresent,
		receiptExact:     receiptPresent && bytes.Equal(receipt, expectedReceipt),
	}, nil
}

func (f completionRecoveryFacts) canPromoteCanonical(intent ReplaceIntentV1) bool {
	return f.canonicalPresent && digestHex(f.canonical) == intent.CandidateSHA256 && !f.candidatePresent &&
		(!f.receiptPresent || f.receiptExact)
}

func (f completionRecoveryFacts) canFinishCleanup() bool {
	return !f.canonicalPresent && !f.candidatePresent && f.receiptExact
}

func (f completionRecoveryFacts) canRetryRename(intent ReplaceIntentV1) bool {
	return f.candidatePresent && digestHex(f.candidate) == intent.CandidateSHA256 &&
		priorMatches(intent.PriorState, f.canonical, f.canonicalPresent) && !f.receiptPresent
}

func (f completionRecoveryFacts) canRollBack(intent ReplaceIntentV1) bool {
	return !f.candidatePresent &&
		priorMatches(intent.PriorState, f.canonical, f.canonicalPresent) && !f.receiptPresent
}

func classifyFailedRecoveryIntent(
	projectDir string,
	intent ReplaceIntentV1,
	ops namespaceOps,
) (ReplaceRecoveryAction, error) {
	qDir := queuesDir(projectDir)
	canonical, canonicalPresent, err := readOptional(filepath.Join(qDir, intent.CanonicalBasename), ops)
	if err != nil {
		return ReplaceRefuse, err
	}
	candidate, candidatePresent, err := readOptional(filepath.Join(qDir, intent.CandidateTempBasename), ops)
	if err != nil {
		return ReplaceRefuse, err
	}
	receiptPath := filepath.Join(failedRecoveryReceiptsDir(projectDir), intent.FailedRecoveryReceiptBinding.Basename)
	receipt, receiptPresent, err := readOptional(receiptPath, ops)
	if err != nil {
		return ReplaceRefuse, err
	}
	expectedReceipt, err := base64.StdEncoding.DecodeString(intent.FailedRecoveryReceiptBinding.CanonicalBytesBase64)
	if err != nil {
		return ReplaceRefuse, err
	}
	switch {
	case canonicalPresent && digestHex(canonical) == intent.CandidateSHA256 && !candidatePresent &&
		(!receiptPresent || bytes.Equal(receipt, expectedReceipt)):
		return ReplacePromoteCanonical, nil
	case priorMatches(intent.PriorState, canonical, canonicalPresent) &&
		(!candidatePresent || digestHex(candidate) == intent.CandidateSHA256) && !receiptPresent:
		return ReplaceNotCommitted, nil
	default:
		return ReplaceRefuse, errors.New("failed recovery intent facts are corrupt, mismatched, or third-state")
	}
}

func recoverFailedReplaceIntent(
	projectDir string,
	intent ReplaceIntentV1,
	intentBytes []byte,
	ops namespaceOps,
) (ReplaceRecoveryAction, error) {
	durableIntent, present, err := readOptional(replaceIntentPath(projectDir, intent.NormalizedName), ops)
	if err != nil || !present || !bytes.Equal(durableIntent, intentBytes) {
		return ReplaceRefuse, errors.New("durable failed recovery intent differs")
	}
	action, err := classifyFailedRecoveryIntent(projectDir, intent, ops)
	if err != nil {
		return action, err
	}
	switch action {
	case ReplacePromoteCanonical:
		if err := writeFailedRecoveryReceipt(
			projectDir,
			intent.QueueID,
			intent.TransactionID,
			intent.FailedRecoveryReceiptBinding,
			ops,
		); err != nil {
			return ReplaceRefuse, fmt.Errorf("write failed recovery receipt: %w", err)
		}
		if err := cleanupReplaceIntent(projectDir, intent.NormalizedName, ops); err != nil {
			return ReplaceRefuse, fmt.Errorf("cleanup failed recovery intent: %w", err)
		}
		return ReplacePromoteCanonical, nil
	case ReplaceNotCommitted:
		candidatePath := filepath.Join(queuesDir(projectDir), intent.CandidateTempBasename)
		if err := ops.remove(candidatePath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return ReplaceRefuse, fmt.Errorf("remove failed recovery candidate: %w", err)
		}
		if err := syncDirectory(queuesDir(projectDir), ops); err != nil {
			return ReplaceRefuse, fmt.Errorf("sync failed recovery candidate removal: %w", err)
		}
		if err := cleanupReplaceIntent(projectDir, intent.NormalizedName, ops); err != nil {
			return ReplaceRefuse, fmt.Errorf("cleanup failed recovery intent: %w", err)
		}
		return ReplaceNotCommitted, nil
	default:
		return ReplaceRefuse, errors.New("unsupported failed recovery action")
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
	phase := CompletionPhase("")
	if intent.OperationKind == OperationCompletion {
		switch outcome {
		case OutcomeRejected:
			phase = CompletionPhaseRejected
		case OutcomeCommitIndeterminate:
			phase = CompletionPhaseCommitIndeterminate
		default:
			phase = CompletionPhaseNotCommitted
		}
	}
	return ReplacementCommit{NamespaceResult: NamespaceResult{Outcome: outcome, Err: err}, Intent: intent, Phase: phase}
}

// CleanupReplaceIntent removes the exact resolved intent and makes its absence
// directory-durable. It is valid only after committed canonical durability.
func CleanupReplaceIntent(projectDir, normalizedName string) error {
	return cleanupReplaceIntent(projectDir, normalizedName, osNamespaceOps())
}

func cleanupReplaceIntent(projectDir, normalizedName string, ops namespaceOps) error {
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
	if err := validateFailedRecoveryIntentCoupling(intent); err != nil {
		return err
	}
	if err := validateCompletionIntentCoupling(intent); err != nil {
		return err
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

func validateFailedRecoveryIntentCoupling(intent ReplaceIntentV1) error {
	binding := intent.FailedRecoveryReceiptBinding
	if intent.OperationKind != OperationFailedRecovery {
		if binding != nil {
			return errors.New("failed recovery receipt binding requires failed-recovery operation")
		}
		return nil
	}
	if intent.CompletionReceiptBinding != nil || intent.ArchiveHandoffBinding != nil {
		return errors.New("failed-recovery intent has incompatible binding")
	}
	return validateFailedRecoveryReceiptBinding(
		binding,
		intent.QueueID,
		intent.TransactionID,
		intent.NormalizedName,
		intent.PriorState,
		intent.CandidateSHA256,
	)
}

func validateCompletionIntentCoupling(intent ReplaceIntentV1) error {
	if intent.OperationKind != OperationCompletion {
		if intent.CompletionReceiptBinding != nil {
			return errors.New("completion receipt binding requires completion operation")
		}
		return nil
	}
	if intent.FailedRecoveryReceiptBinding != nil || intent.ArchiveHandoffBinding != nil {
		return errors.New("completion intent has incompatible binding")
	}
	return validateCompletionReceiptBinding(
		intent.CompletionReceiptBinding,
		intent.QueueID,
		intent.TransactionID,
		intent.NormalizedName,
		intent.CandidateSHA256,
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
		intent.CandidateTempBasename == intent.NormalizedName+".candidate-"+intent.TransactionID
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
		OperationFailedRecovery,
		OperationEagerRefill,
		OperationBudgetCharge,
		OperationReviewCharge,
		OperationMaintenance,
		OperationReconciliation,
		OperationStartup,
		OperationInline,
		OperationBootstrap,
		OperationCrewPlaceholder,
		OperationCancellation,
		OperationCompletion:
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
	return filepath.Join(queuesDir(projectDir), name+replaceIntentSuffix)
}

func failedRecoveryReceiptsDir(projectDir string) string {
	return filepath.Join(queuesDir(projectDir), ".failed-recovery-receipts")
}

func completionReceiptsDir(projectDir string) string {
	return filepath.Join(queuesDir(projectDir), ".completion-receipts")
}

func writeCompletionReceipt(
	projectDir, queueID, transactionID string,
	binding *CompletionReceiptBinding,
	ops namespaceOps,
) error {
	data, err := base64.StdEncoding.DecodeString(binding.CanonicalBytesBase64)
	if err != nil {
		return err
	}
	receipt, err := DecodeCompletionReceipt(data)
	if err != nil {
		return err
	}
	if err := validateCompletionReceiptBinding(
		binding,
		queueID,
		transactionID,
		receipt.NormalizedName,
		receipt.CompletedQueueSHA256,
	); err != nil {
		return err
	}
	root := completionReceiptsDir(projectDir)
	if err := ops.mkdirAll(root, 0o700); err != nil {
		return err
	}
	info, err := ops.lstat(root)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("completion receipt root is not a real directory")
	}
	if err := syncDirectory(queuesDir(projectDir), ops); err != nil {
		return err
	}
	install := durableNoReplace(filepath.Join(root, binding.Basename), data, ops)
	if install.State != noReplaceInstalled {
		if install.Err != nil {
			return install.Err
		}
		return errors.New("completion receipt was not installed")
	}
	return syncDirectory(root, ops)
}

// CleanupCompletedCanonical removes only the exact completed queue bound by a
// durable receipt. A newer same-name queue or changed bytes are preserved.
func CleanupCompletedCanonical(
	projectDir, normalizedName, queueID, completedQueueSHA256 string,
) error {
	return cleanupCompletedCanonical(projectDir, normalizedName, queueID, completedQueueSHA256, osNamespaceOps())
}

func cleanupCompletedCanonical(
	projectDir, normalizedName, queueID, completedQueueSHA256 string,
	ops namespaceOps,
) error {
	path := queuePath(projectDir, NormaliseQueueName(normalizedName))
	data, err := ops.readFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return syncDirectory(queuesDir(projectDir), ops)
	}
	if err != nil {
		return err
	}
	q, err := UnmarshalQueue(data)
	if err != nil {
		return fmt.Errorf("completed canonical: %w", err)
	}
	if q.QueueID != queueID {
		return errors.New("completed canonical belongs to a different queue")
	}
	if digestHex(data) != completedQueueSHA256 {
		return errors.New("completed canonical digest differs from receipt")
	}
	if err := ops.remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return syncDirectory(queuesDir(projectDir), ops)
}

func writeFailedRecoveryReceipt(
	projectDir string,
	queueID string,
	transactionID string,
	binding *FailedRecoveryReceiptBinding,
	ops namespaceOps,
) error {
	data, err := base64.StdEncoding.DecodeString(binding.CanonicalBytesBase64)
	if err != nil {
		return err
	}
	var receipt FailedRecoveryReceipt
	if err := strictJSON(data, &receipt); err != nil {
		return err
	}
	if err := validateFailedRecoveryReceiptBinding(
		binding,
		queueID,
		transactionID,
		receipt.NormalizedName,
		receipt.PriorQueueSHA256,
		receipt.RecoveredQueueSHA256,
	); err != nil {
		return err
	}
	root := failedRecoveryReceiptsDir(projectDir)
	if err := ops.mkdirAll(root, 0o700); err != nil {
		return err
	}
	if err := syncDirectory(queuesDir(projectDir), ops); err != nil {
		return err
	}
	install := durableNoReplace(filepath.Join(root, binding.Basename), data, ops)
	if install.State != noReplaceInstalled {
		return install.Err
	}
	return syncDirectory(root, ops)
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
