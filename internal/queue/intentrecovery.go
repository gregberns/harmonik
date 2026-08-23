package queue

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ErrArchiveHandoffIntentNotRecoverable is reported for a leftover intent that
// carries an archive-handoff binding.
//
// Such an intent is not a crash artifact. A cancellation transaction leaves it
// on disk on purpose, because the linked predecessor/successor continuation
// reads it back later. Resolving it here would destroy that linkage, so the
// sweep refuses and says so.
//
// The continuation that consumes these intents — ClassifyLinkedHandoff and
// InstallBoundArchiveIntent — has no production caller today, so an intent
// reported with this error stays on disk and keeps its queue wedged. That is a
// separate unwired half and it is deliberately out of this sweep's scope. The
// error exists so the condition is loud in the daemon log rather than silent.
var ErrArchiveHandoffIntentNotRecoverable = errors.New(
	"queue: leftover archive-handoff intent needs the linked-handoff continuation, which is not wired",
)

const replaceIntentSuffix = ".replace-intent"

// ReplaceIntentRecovery reports what RecoverReplaceIntents did with one
// leftover intent.
type ReplaceIntentRecovery struct {
	// NormalizedName is the queue the intent belongs to. It is taken from the
	// filename when the intent bytes cannot be decoded.
	NormalizedName string

	// Action is the recovery action that was carried out. ReplaceRefuse means
	// nothing on disk was changed and Err says why.
	Action ReplaceRecoveryAction

	// Completion is set only for a completion-bound intent. It reports the
	// reloaded durable phase and whether C11 must install a release marker.
	Completion *CompletionIntentRecovery

	// Err is non-nil when the intent could not be resolved. Plain replacements
	// preserve their input facts. Completion recovery can stop after a later
	// durable phase, which Completion reports for the next startup attempt.
	Err error
}

// CompletionIntentRecovery reports the durable C09 result for one final
// completion transaction.
type CompletionIntentRecovery struct {
	Phase                CompletionPhase
	ReleaseMarkerPending bool
}

// Resolved reports whether the intent was finished or rolled back. A false
// result means the intent is still on disk and its queue is still wedged.
func (r ReplaceIntentRecovery) Resolved() bool {
	return r.Err == nil && r.Action != ReplaceRefuse
}

// RecoverReplaceIntents finishes or rolls back every durable replace intent
// left in the queues directory by a crash.
//
// It MUST run before the QueueStore installs a queue, so that what the store
// installs is the resolved queue rather than half of a transaction.
//
// Be precise about what that does NOT say. It is not "before anything reads a
// queue file". The daemon's boot reconcile reads every queue file earlier still,
// to build bead provenance for the orphan sweep, and that read happens before
// this sweep runs. In the retry-rename case those two see different bytes: the
// provenance pass sees the prior state and this sweep then rolls the file
// forward. Closing that window means calling this earlier in the daemon's boot
// sequence, which is a change in a package this one does not own.
//
// The returned slice holds one entry per unresolved-or-resolved intent found, in
// directory order, and is empty on a clean boot. The error return covers only a
// failure to read the queues directory at all; a per-intent failure is reported
// in that intent's entry and never stops the sweep, because one corrupt intent
// must not keep every other queue from recovering.
func RecoverReplaceIntents(projectDir string) ([]ReplaceIntentRecovery, error) {
	return recoverReplaceIntents(projectDir, osNamespaceOps())
}

func recoverReplaceIntents(projectDir string, ops namespaceOps) ([]ReplaceIntentRecovery, error) {
	qDir := queuesDir(projectDir)
	entries, err := ops.readDir(qDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("queue: RecoverReplaceIntents: readdir %q: %w", qDir, err)
	}

	var recoveries []ReplaceIntentRecovery
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), replaceIntentSuffix) {
			continue
		}
		if recovery, found := recoverOneIntentFile(projectDir, entry.Name(), ops); found {
			recoveries = append(recoveries, recovery)
		}
	}
	return recoveries, nil
}

func recoverOneIntentFile(projectDir, basename string, ops namespaceOps) (ReplaceIntentRecovery, bool) {
	nameFromFile := strings.TrimSuffix(basename, replaceIntentSuffix)
	refuse := func(err error) (ReplaceIntentRecovery, bool) {
		return ReplaceIntentRecovery{NormalizedName: nameFromFile, Action: ReplaceRefuse, Err: err}, true
	}

	intentBytes, present, err := readOptional(filepath.Join(queuesDir(projectDir), basename), ops)
	if err != nil {
		return refuse(fmt.Errorf("read replace intent: %w", err))
	}
	if !present {
		return ReplaceIntentRecovery{}, false
	}

	intent, err := decodeReplaceIntent(intentBytes)
	if err != nil {
		return refuse(fmt.Errorf("decode replace intent: %w", err))
	}
	if intent.NormalizedName != nameFromFile {
		return refuse(fmt.Errorf(
			"replace intent names queue %q but is filed under %q", intent.NormalizedName, nameFromFile,
		))
	}
	if intent.ArchiveHandoffBinding != nil {
		return refuse(ErrArchiveHandoffIntentNotRecoverable)
	}

	if intent.FailedRecoveryReceiptBinding != nil {
		action, recoverErr := recoverFailedReplaceIntent(projectDir, intent, intentBytes, ops)
		return ReplaceIntentRecovery{NormalizedName: intent.NormalizedName, Action: action, Err: recoverErr}, true
	}
	if intent.CompletionReceiptBinding != nil {
		action, phase, recoverErr := recoverCompletionReplaceIntent(projectDir, intent, intentBytes, ops)
		return ReplaceIntentRecovery{
			NormalizedName: intent.NormalizedName,
			Action:         action,
			Completion: &CompletionIntentRecovery{
				Phase: phase,
				ReleaseMarkerPending: recoverErr == nil &&
					(action == ReplacePromoteCanonical || action == ReplaceRetryRename),
			},
			Err: recoverErr,
		}, true
	}
	action, recoverErr := recoverReplaceIntent(projectDir, intent, intentBytes, ops)
	return ReplaceIntentRecovery{NormalizedName: intent.NormalizedName, Action: action, Err: recoverErr}, true
}

func completionRecoveryPhase(projectDir string, intent ReplaceIntentV1, ops namespaceOps) CompletionPhase {
	facts, err := loadCompletionRecoveryFacts(projectDir, intent, ops)
	if err != nil {
		return CompletionPhaseCommitIndeterminate
	}
	canonicalCandidate := facts.canonicalPresent && digestHex(facts.canonical) == intent.CandidateSHA256
	if facts.receiptExact && (canonicalCandidate || (!facts.canonicalPresent && !facts.candidatePresent)) {
		return CompletionPhaseReceiptDurable
	}
	if canonicalCandidate && !facts.receiptPresent {
		return CompletionPhaseCanonicalCommitted
	}
	if !facts.receiptPresent && priorMatches(intent.PriorState, facts.canonical, facts.canonicalPresent) {
		return CompletionPhaseNotCommitted
	}
	return CompletionPhaseCommitIndeterminate
}

func recoverCompletionReplaceIntent(
	projectDir string,
	intent ReplaceIntentV1,
	intentBytes []byte,
	ops namespaceOps,
) (ReplaceRecoveryAction, CompletionPhase, error) {
	durableIntent, present, err := readOptional(replaceIntentPath(projectDir, intent.NormalizedName), ops)
	if err != nil {
		return ReplaceRefuse, CompletionPhaseCommitIndeterminate, fmt.Errorf("read durable completion intent: %w", err)
	}
	if !present || !bytes.Equal(durableIntent, intentBytes) {
		return ReplaceRefuse, CompletionPhaseRejected, errors.New("durable completion intent differs")
	}
	action, err := classifyCompletionIntent(projectDir, intent, ops)
	if err != nil {
		return action, completionRecoveryPhase(projectDir, intent, ops), err
	}
	switch action {
	case ReplaceRetryRename:
		if err := retryCompletionRename(projectDir, intent, ops); err != nil {
			return ReplaceRefuse, CompletionPhaseCommitIndeterminate, err
		}
		fallthrough
	case ReplacePromoteCanonical:
		phase, finishErr := finishRecoveredCompletion(projectDir, intent, ops)
		if finishErr != nil {
			return ReplaceRefuse, phase, finishErr
		}
		return action, phase, nil
	case ReplaceNotCommitted:
		if err := discardCandidate(projectDir, intent, ops); err != nil {
			return ReplaceRefuse, CompletionPhaseNotCommitted, err
		}
		return ReplaceNotCommitted, CompletionPhaseNotCommitted, nil
	default:
		return ReplaceRefuse, CompletionPhaseRejected, errors.New("unsupported completion recovery action")
	}
}

func retryCompletionRename(projectDir string, intent ReplaceIntentV1, ops namespaceOps) error {
	qDir := queuesDir(projectDir)
	if err := ops.rename(
		filepath.Join(qDir, intent.CandidateTempBasename),
		filepath.Join(qDir, intent.CanonicalBasename),
	); err != nil {
		return fmt.Errorf("retry completion rename: %w", err)
	}
	if err := syncDirectory(qDir, ops); err != nil {
		return fmt.Errorf("sync completion canonical: %w", err)
	}
	return nil
}

func finishRecoveredCompletion(
	projectDir string,
	intent ReplaceIntentV1,
	ops namespaceOps,
) (CompletionPhase, error) {
	receiptBytes, err := base64.StdEncoding.DecodeString(intent.CompletionReceiptBinding.CanonicalBytesBase64)
	if err != nil {
		return CompletionPhaseCanonicalCommitted, fmt.Errorf("decode completion receipt binding: %w", err)
	}
	receipt, err := DecodeCompletionReceipt(receiptBytes)
	if err != nil {
		return CompletionPhaseCanonicalCommitted, fmt.Errorf("decode completion receipt: %w", err)
	}
	if err := writeCompletionReceipt(
		projectDir, intent.QueueID, intent.TransactionID, intent.CompletionReceiptBinding, ops,
	); err != nil {
		return CompletionPhaseCanonicalCommitted, fmt.Errorf("write completion receipt: %w", err)
	}
	if err := cleanupCompletedCanonical(
		projectDir, intent.NormalizedName, intent.QueueID, receipt.CompletedQueueSHA256, ops,
	); err != nil {
		return CompletionPhaseReceiptDurable, fmt.Errorf("cleanup completed canonical: %w", err)
	}
	if err := cleanupReplaceIntent(projectDir, intent.NormalizedName, ops); err != nil {
		return CompletionPhaseReceiptDurable, fmt.Errorf("cleanup completion intent: %w", err)
	}
	return CompletionPhaseCleaned, nil
}

func recoverReplaceIntent(
	projectDir string,
	intent ReplaceIntentV1,
	intentBytes []byte,
	ops namespaceOps,
) (ReplaceRecoveryAction, error) {
	durableIntent, present, err := readOptional(replaceIntentPath(projectDir, intent.NormalizedName), ops)
	if err != nil {
		return ReplaceRefuse, fmt.Errorf("re-read durable replace intent: %w", err)
	}
	if !present {
		return ReplaceRefuse, errors.New("durable replace intent is gone")
	}
	if !bytes.Equal(durableIntent, intentBytes) {
		return ReplaceRefuse, errors.New("durable replace intent differs")
	}
	action, err := classifyReplaceIntent(projectDir, intent, ops)
	if err != nil {
		return action, err
	}
	switch action {
	case ReplacePromoteCanonical:
		if err := cleanupReplaceIntent(projectDir, intent.NormalizedName, ops); err != nil {
			return ReplaceRefuse, fmt.Errorf("cleanup replace intent: %w", err)
		}
		return ReplacePromoteCanonical, nil
	case ReplaceRetryRename:
		if err := retryCandidateRename(projectDir, intent, ops); err != nil {
			return ReplaceRefuse, err
		}
		return ReplaceRetryRename, nil
	case ReplaceNotCommitted:
		if err := discardCandidate(projectDir, intent, ops); err != nil {
			return ReplaceRefuse, err
		}
		return ReplaceNotCommitted, nil
	default:
		return ReplaceRefuse, errors.New("unsupported replace recovery action")
	}
}

func retryCandidateRename(projectDir string, intent ReplaceIntentV1, ops namespaceOps) error {
	qDir := queuesDir(projectDir)
	candidatePath := filepath.Join(qDir, intent.CandidateTempBasename)
	canonicalPath := filepath.Join(qDir, intent.CanonicalBasename)
	if err := ops.rename(candidatePath, canonicalPath); err != nil {
		return fmt.Errorf("retry candidate rename: %w", err)
	}
	if err := syncDirectory(qDir, ops); err != nil {
		return fmt.Errorf("sync canonical after retry rename: %w", err)
	}
	if err := cleanupReplaceIntent(projectDir, intent.NormalizedName, ops); err != nil {
		return fmt.Errorf("cleanup replace intent: %w", err)
	}
	return nil
}

func discardCandidate(projectDir string, intent ReplaceIntentV1, ops namespaceOps) error {
	qDir := queuesDir(projectDir)
	candidatePath := filepath.Join(qDir, intent.CandidateTempBasename)
	if err := ops.remove(candidatePath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove replace candidate: %w", err)
	}
	if err := syncDirectory(qDir, ops); err != nil {
		return fmt.Errorf("sync replace candidate removal: %w", err)
	}
	if err := cleanupReplaceIntent(projectDir, intent.NormalizedName, ops); err != nil {
		return fmt.Errorf("cleanup replace intent: %w", err)
	}
	return nil
}
