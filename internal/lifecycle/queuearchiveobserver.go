package lifecycle

// queuearchiveobserver.go — boot-time OBSERVATION of accumulated failed-queue
// archive files. It counts them, measures them, and reports. It removes
// nothing.
//
// Tags: mechanism
// (ZFC per specs/architecture.md §4.2 AR-005/AR-006. Every step here is file
// I/O and arithmetic over a fixed layout: list, stat, sum, sort, compare
// against an operator-set integer. There is no semantic judgment and no LLM.
// AR-INV-001 holds: no daemon-side requirement in this file is
// cognition-tagged.)
//
// # Why this observes instead of deletes
//
// This used to be SweepQueueArchives. It kept "the newest 5 per category" and
// deleted the rest, where 5 was a compiled-in number and "category" was
// inferred by chopping the last hyphenated field off a filename. It also
// looked in the wrong directory, so it never actually deleted anything — the
// bug and the design flaw hid each other for the life of the function.
//
// Fixing only the directory would have turned a silently-broken deleter into a
// working deleter, which is the worse outcome. Deciding which record of a
// failed run is still worth keeping is a judgment about the value of evidence.
// It depends on whether anyone has read the archive yet, whether the failure
// is still being investigated, and whether the queue has since been re-run.
// A count of five knows none of that.
//
// plans/2026-07-27-delete-and-rewrite/CHARTER.md §5 names this exact shape as
// a recurring failure: "a mechanical rule that infers intent from a coarse
// signal, then acts on it", and records that the movement governor scored "no
// commits" as "stalled" and would have killed the daemon 4,520 times in five
// weeks. Its conclusion — "the honest answer is always agent judgment or an
// operator-set number, never an inference" — is why this function now stops at
// the report.
//
// # The delegation path for the judgment part
//
// The judgment ("which of these archives may go") is NOT performed here and is
// NOT cognition-tagged daemon-side. It leaves the daemon as data:
// [ObserveQueueArchives] returns a [QueueArchiveReport], RunOrphanSweep folds
// its counts into the daemon_orphan_sweep_completed event, and the event
// reaches the captain through the boot digest's recent_events. The captain (or
// the operator) decides and acts with an explicit command. The daemon's only
// job is to make sure the pile is visible.
//
// # The operator-set number
//
// [EnvQueueArchiveKeepCount] lets an operator state how many archives per
// queue they want to keep. There is NO default. When the variable is unset the
// report says RetentionConfigured=false and OverRetention=0, and it says so
// rather than quietly applying a number nobody chose. When it IS set, the
// report names the archives that exceed it — and still deletes none of them,
// because the removal is the operator's act, not the daemon's.
//
// Supersedes the Gap-4 archive sweep (bead ref hk-pycay).

import (
	"fmt"
	"log"
	"os"
	"sort"
	"strconv"
	"time"

	"github.com/gregberns/harmonik/internal/queue"
)

// EnvQueueArchiveKeepCount is the environment variable through which an
// operator states how many failed-queue archives to keep per queue.
//
// It has NO default. Unset means "no retention policy has been chosen", which
// the report states plainly. A value that does not parse as a positive integer
// is treated as unset and is reported as a parse error, not silently ignored.
const EnvQueueArchiveKeepCount = "HARMONIK_QUEUE_ARCHIVE_KEEP_COUNT"

// QueueArchive is one failed-queue archive file found on disk.
type QueueArchive struct {
	// Path is the absolute path to the archive file.
	Path string

	// QueueName is the queue the archive came from, per
	// [queue.FailedArchiveQueueName]. Empty when the name does not parse.
	QueueName string

	// SizeBytes is the archive's size on disk.
	SizeBytes int64

	// ModTime is the archive's last-modified time.
	ModTime time.Time

	// Age is ModTime measured against ObserveQueueArchivesConfig.Now.
	// Negative when the file is newer than the injected clock.
	Age time.Duration
}

// ObserveQueueArchivesConfig carries the inputs to [ObserveQueueArchives].
type ObserveQueueArchivesConfig struct {
	// Now is the reference time for age. Required — a zero Now is a
	// programming error and yields an error rather than a silent time.Now()
	// call, per PRINCIPLES.md (time is injected, not read).
	Now time.Time

	// KeepPerQueue is the operator's retention number, if they set one in
	// code rather than in the environment. Nil → read [EnvQueueArchiveKeepCount].
	// Non-nil takes precedence over the environment.
	KeepPerQueue *int

	// Getenv reads the environment. Nil → os.Getenv.
	Getenv func(string) string

	// Logger receives diagnostic messages. Nil → silent.
	Logger *log.Logger
}

// QueueArchiveReport is what the observer found. It is a description of the
// disk, plus a comparison against an operator-set number when one exists. It
// carries no recommendation and no action.
type QueueArchiveReport struct {
	// Archives is every archive found, oldest ModTime first, ties broken by
	// path so the order is stable.
	Archives []QueueArchive

	// Count is len(Archives), repeated so a caller that only wants the number
	// does not have to keep the slice.
	Count int

	// TotalBytes is the sum of every archive's size.
	TotalBytes int64

	// OldestAge and NewestAge are the age of the first and last entries in
	// Archives. Both are zero when Count is 0.
	OldestAge time.Duration
	NewestAge time.Duration

	// QueueNames is the set of queue names that produced archives, sorted.
	QueueNames []string

	// RetentionConfigured reports whether an operator set a retention number.
	// FALSE is the honest default: no number has been chosen, so nothing is
	// over any limit.
	RetentionConfigured bool

	// RetentionKeep is the operator's number when RetentionConfigured is true,
	// and 0 otherwise. It is never a value this code invented.
	RetentionKeep int

	// OverRetentionPaths lists the archives that exceed RetentionKeep for
	// their queue, oldest first. Always empty when RetentionConfigured is
	// false. These are CANDIDATES for an operator or agent to remove. Nothing
	// in the daemon removes them.
	OverRetentionPaths []string

	// OverRetention is len(OverRetentionPaths).
	OverRetention int
}

// ObserveQueueArchives lists every failed-queue archive under projectDir,
// measures each one, and returns the result. It performs no removal and takes
// no other action. A caller that wants archives removed must remove them
// itself, having decided which ones.
//
// The archive layout comes from [queue.ListFailedArchives] — the observer does
// not know where archives live, the package that writes them does.
//
// A missing .harmonik/queues/ directory returns a zero report and a nil error:
// no archives is a valid state.
//
// Per-file stat failures are logged and the file is skipped, because one
// unreadable archive must not hide the other sixty-seven. The returned error is
// non-nil only when the listing itself failed or when Now is zero.
func ObserveQueueArchives(projectDir string, cfg ObserveQueueArchivesConfig) (QueueArchiveReport, error) {
	if cfg.Now.IsZero() {
		return QueueArchiveReport{}, fmt.Errorf("lifecycle: ObserveQueueArchives: Now is required (time is injected, not read)")
	}

	paths, err := queue.ListFailedArchives(projectDir)
	if err != nil {
		return QueueArchiveReport{}, fmt.Errorf("lifecycle: ObserveQueueArchives: %w", err)
	}

	report := QueueArchiveReport{}
	seenQueues := make(map[string]struct{})

	for _, path := range paths {
		info, statErr := os.Stat(path)
		if statErr != nil {
			orphanLog(cfg.Logger, "ObserveQueueArchives: stat %q: %v", path, statErr)
			continue
		}
		name := queue.FailedArchiveQueueName(path)
		report.Archives = append(report.Archives, QueueArchive{
			Path:      path,
			QueueName: name,
			SizeBytes: info.Size(),
			ModTime:   info.ModTime(),
			Age:       cfg.Now.Sub(info.ModTime()),
		})
		report.TotalBytes += info.Size()
		if name != "" {
			seenQueues[name] = struct{}{}
		}
	}

	sort.Slice(report.Archives, func(i, j int) bool {
		a, b := report.Archives[i], report.Archives[j]
		if !a.ModTime.Equal(b.ModTime) {
			return a.ModTime.Before(b.ModTime)
		}
		return a.Path < b.Path
	})

	report.Count = len(report.Archives)
	if report.Count > 0 {
		report.OldestAge = report.Archives[0].Age
		report.NewestAge = report.Archives[report.Count-1].Age
	}
	report.QueueNames = make([]string, 0, len(seenQueues))
	for name := range seenQueues {
		report.QueueNames = append(report.QueueNames, name)
	}
	sort.Strings(report.QueueNames)

	keep, configured := resolveKeepPerQueue(cfg)
	report.RetentionConfigured = configured
	if configured {
		report.RetentionKeep = keep
		report.OverRetentionPaths = overRetentionPaths(report.Archives, keep)
		report.OverRetention = len(report.OverRetentionPaths)
	}

	orphanLog(cfg.Logger,
		"ObserveQueueArchives: %d archive(s), %d byte(s), %d queue(s), retention_configured=%t, over_retention=%d, removed=0",
		report.Count, report.TotalBytes, len(report.QueueNames), report.RetentionConfigured, report.OverRetention)

	return report, nil
}

// resolveKeepPerQueue returns the operator's retention number and whether one
// was set at all. It invents no value: an absent, unparseable, or non-positive
// setting yields (0, false).
func resolveKeepPerQueue(cfg ObserveQueueArchivesConfig) (int, bool) {
	if cfg.KeepPerQueue != nil {
		if *cfg.KeepPerQueue > 0 {
			return *cfg.KeepPerQueue, true
		}
		return 0, false
	}
	getenv := cfg.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	raw := getenv(EnvQueueArchiveKeepCount)
	if raw == "" {
		return 0, false
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		orphanLog(cfg.Logger,
			"ObserveQueueArchives: %s=%q is not a positive integer; treating retention as unset",
			EnvQueueArchiveKeepCount, raw)
		return 0, false
	}
	return n, true
}

// overRetentionPaths returns, per queue name, the archives beyond the newest
// keep. archives MUST already be sorted oldest first. The result keeps that
// order, so the oldest candidate is first.
//
// This is a comparison against a number the operator chose. It is not a
// recommendation and no caller in the daemon acts on it.
func overRetentionPaths(archives []QueueArchive, keep int) []string {
	perQueue := make(map[string]int)
	for _, a := range archives {
		perQueue[a.QueueName]++
	}
	// remaining[q] counts how many of q's archives are still ahead of the
	// cursor. The first (count - keep) of each queue are over retention.
	overCount := make(map[string]int, len(perQueue))
	for name, total := range perQueue {
		if total > keep {
			overCount[name] = total - keep
		}
	}
	var out []string
	for _, a := range archives {
		if overCount[a.QueueName] > 0 {
			out = append(out, a.Path)
			overCount[a.QueueName]--
		}
	}
	return out
}
