package queue

// intentrecovery.go — the startup half of the durable replace transaction.
//
// WriteReplacement installs a durable <name>.replace-intent before it renames
// the candidate over the canonical queue file, and the QueueStore removes that
// intent after the rename lands. A crash between those two steps leaves the
// intent on disk.
//
// Nothing used to read it back. The consequence was not a slow leak: an intent
// left behind refuses the next install of a different intent for the same queue
// (durableNoReplace returns noReplaceRefused), the QueueStore reads that refusal
// as an indeterminate commit and quarantines the queue, and the operator's own
// `queue recover` path installs an intent too, so it is refused by the same
// leftover file. One crash wedged the queue until somebody deleted the file by
// hand.
//
// RecoverReplaceIntents is the missing read-back. It runs once at startup and
// finishes or rolls back each leftover intent against the facts the intent
// itself pins by digest. Read its doc comment for exactly how early "at
// startup" is, because it is not as early as it sounds.
//
// Spec ref: specs/queue-model.md §3.1 QM-001 — the atomic-write sequence.
// Spec ref: specs/process-lifecycle.md §4.2 PL-005 step 8a — startup queue load.

import (
	"bytes"
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

// replaceIntentSuffix is the filename suffix of a durable replace intent inside
// the queues directory.
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

	// Err is non-nil when the intent could not be resolved. The intent file is
	// left exactly as it was found, so the next boot sees the same facts.
	Err error
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

// recoverOneIntentFile resolves a single intent file named by its basename.
// Every failure path returns a refusal that names the queue, so a caller can
// report the wedge even when the bytes are too corrupt to say which queue the
// intent meant.
//
// The bool is false when the file is gone by the time it is read, which is the
// one case with nothing to report: no intent, no wedge, and no action taken.
// Reporting that as a successful recovery would put a line in the daemon log
// claiming a crash was cleaned up when none was.
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
	action, recoverErr := recoverReplaceIntent(projectDir, intent, intentBytes, ops)
	return ReplaceIntentRecovery{NormalizedName: intent.NormalizedName, Action: action, Err: recoverErr}, true
}

// recoverReplaceIntent completes a plain replace transaction after a restart.
// It is the sibling of recoverFailedReplaceIntent for intents that carry no
// receipt binding, and it acts on the same rule: only the exact durable bytes
// for this queue are accepted, and the action comes from classification against
// the on-disk facts rather than from anything the caller believes.
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
		// The rename already landed before the crash. The canonical file is
		// already the candidate's bytes, so the only unfinished step is
		// dropping the intent.
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

// retryCandidateRename finishes a transaction whose candidate was durable but
// whose rename had not happened when the process died.
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

// discardCandidate rolls a transaction back to its prior state. The canonical
// file still holds the prior bytes, so removing the candidate and the intent
// restores the queue exactly as it stood before the transaction began.
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
