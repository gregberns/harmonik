package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"sort"
	"strings"

	supervisecmd "github.com/gregberns/harmonik/cmd/harmonik/supervise"
)

func init() {
	supervisecmd.SkewCheckHook = func(projectDir string) (supervisecmd.AssetSkewVerdict, error) {
		res, err := CheckAssetSkew(
			BuildManifest,
			func() (Lock, error) { return ReadLock(projectDir) },
			func(m Manifest) (map[string]string, error) {
				return buildDiskHashes(projectDir, m)
			},
		)
		if err != nil {
			return supervisecmd.AssetSkewVerdict{}, err
		}
		return supervisecmd.AssetSkewVerdict{
			Skewed:              res.Skewed,
			ChangedCount:        res.ChangedCount,
			ConflictCount:       res.ConflictCount,
			AutoApplyCandidates: res.AutoApplyCandidates,
			NeverSynced:         res.NeverSynced,
			BinaryDigest:        res.BinaryDigest,
			LockDigest:          res.LockDigest,
		}, nil
	}

	supervisecmd.AutoApplyGateHook = daemonDispatchGate

	supervisecmd.AutoApplyHook = func(projectDir string) (int, error) {
		m, err := BuildManifest()
		if err != nil {
			return 0, fmt.Errorf("auto-apply: build manifest: %w", err)
		}
		lock, err := ReadLock(projectDir)
		if err != nil {
			return 0, fmt.Errorf("auto-apply: read lock: %w", err)
		}
		disk, err := buildDiskHashes(projectDir, m)
		if err != nil {
			return 0, fmt.Errorf("auto-apply: hash project files: %w", err)
		}

		fullPlan := Reconcile(m, lock, disk)

		var filtered []ReconcileItem
		for _, item := range fullPlan {
			if item.Class == Managed && item.Action == ActionFastForward {
				filtered = append(filtered, item)
			}
		}
		if len(filtered) == 0 {
			return 0, nil
		}

		outcomes, code := applyPlan(projectDir, m, filtered, io.Discard, io.Discard)
		if code != 0 {
			return 0, fmt.Errorf("auto-apply: apply failed (exit %d)", code)
		}

		newLock := lockFromOutcomes(lock, outcomes)
		if err := WriteLock(projectDir, newLock); err != nil {
			return 0, fmt.Errorf("auto-apply: write lock: %w", err)
		}

		applied := 0
		for _, o := range outcomes {
			if o.written {
				applied++
			}
		}
		return applied, nil
	}
}

// PrintSkewHintIfStale checks whether the project's on-disk managed assets are
// behind the running binary and, when they are, prints a loud, actionable hint
// to stderr telling the operator to run 'harmonik sync-assets'. Best-effort:
// any detection error or zero-change result is silently ignored so a skew check
// never blocks start or init.
//
// Called from 'harmonik start' (before launching) and 'harmonik init' (after
// provisioning). The supervisor→captain comms channel (RunAssetSkewCheck /
// notifyCaptainSkew) is the complementary notify path that reaches a running
// captain; this one reaches the operator at the CLI directly.
//
// Single implementation: delegates to CheckAssetSkew (the pure detection func)
// with the same closure pattern the supervisor wiring uses, so detection logic
// is never duplicated.
func PrintSkewHintIfStale(projectDir string, stderr io.Writer) {
	res, err := CheckAssetSkew(
		BuildManifest,
		func() (Lock, error) { return ReadLock(projectDir) },
		func(m Manifest) (map[string]string, error) {
			return buildDiskHashes(projectDir, m)
		},
	)
	if err != nil || !res.Skewed || res.ChangedCount == 0 {
		return
	}

	switch {
	case res.NeverSynced:
		fmt.Fprintf(stderr, //nolint:errcheck // best-effort operator hint must never block start/init when its writer is unavailable
			"harmonik: WARNING  %d on-disk instruction file(s) have not been synced to this binary version\n",
			res.ChangedCount)
	case res.ConflictCount > 0:
		fmt.Fprintf(stderr, //nolint:errcheck // best-effort operator hint must never block start/init when its writer is unavailable
			"harmonik: WARNING  %d on-disk instruction file(s) are stale vs the running binary (%d conflict(s) require manual review)\n",
			res.ChangedCount, res.ConflictCount)
	default:
		fmt.Fprintf(stderr, //nolint:errcheck // best-effort operator hint must never block start/init when its writer is unavailable
			"harmonik: WARNING  %d on-disk instruction file(s) are stale vs the running binary\n",
			res.ChangedCount)
	}
	fmt.Fprintln(stderr, "harmonik:   → run: harmonik sync-assets --dry-run   (review what would change)") //nolint:errcheck // best-effort operator hint
	fmt.Fprintln(stderr, "harmonik:          harmonik sync-assets --apply      (apply updates)")           //nolint:errcheck // best-effort operator hint
}

// Digest returns a single sha256 (hex) over the lock's sorted "path:sha\n" lines,
// computed the SAME way Manifest.Digest does so the two are directly comparable:
// a project whose lock records exactly the bytes the binary ships produces the same
// digest as BuildManifest().Digest(). Any drift (a newer/older/absent asset in the
// lock) moves it.
//
// A zero-entry lock (never-synced project) digests the empty string — distinct from
// any non-empty manifest digest, so a never-synced project with shipped assets is
// always detected as skewed.
func (l Lock) Digest() string {
	paths := make([]string, 0, len(l.Files))
	for p := range l.Files {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	var b strings.Builder
	for _, p := range paths {
		b.WriteString(p)
		b.WriteByte(':')
		b.WriteString(l.Files[p].Sha256)
		b.WriteByte('\n')
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

// SkewResult is the verdict of a single skew check: whether the project's installed
// assets are behind the running binary, how many files would change, the comparable
// digests, and the subset of changes that are safe to auto-apply (Managed +
// FastForward only). It carries enough to compose a captain notice without re-reading
// the lock or re-walking the plan.
type SkewResult struct {
	// Skewed is true when the binary's embedded asset digest differs from the
	// project's installed (lock) digest — i.e. there is something to sync.
	Skewed bool

	// ChangedCount is the number of reconcile items that would actually change the
	// project (every item except Skip and Leave). This is the "M files have updates"
	// figure in the captain notice.
	ChangedCount int

	// ConflictCount is the number of items that would CONFLICT (project edited a
	// managed file / managed markers missing / content-owned divergence). These are
	// always surfaced for human review and NEVER auto-applied.
	ConflictCount int

	// AutoApplyCandidates is the count of items that are SAFE to auto-apply in a
	// lull: Managed class AND FastForward action only. Conflicts and content-owned
	// changes are deliberately excluded. Used by the (config-gated) auto-apply gate.
	AutoApplyCandidates int

	// BinaryDigest / LockDigest are the comparable fingerprints that produced the
	// verdict (for logging / dedupe — notify once per digest, not every tick).
	BinaryDigest string
	LockDigest   string

	// NeverSynced is true when the project has no (or an empty) assets.lock — it was
	// init'd by an older binary that predates the lock, or never sync'd. Any shipped
	// managed asset then counts as skew.
	NeverSynced bool
}

// CheckAssetSkew compares the running binary's embedded asset manifest against the
// project's installed lock and returns the skew verdict. It performs NO writes and
// hashes on-disk files only to compute the change count (read-only).
//
// Inputs are passed as funcs so the supervisor wiring and the unit tests can inject
// them without a live filesystem:
//
//	buildManifest — typically BuildManifest (the embed-side manifest)
//	readLock      — typically a closure over ReadLock(projectDir)
//	diskHashes    — typically a closure over buildDiskHashes(projectDir, manifest):
//	                embed-path → on-disk sha256 ("" when absent). When nil, the
//	                change count is derived from manifest-vs-lock alone (every
//	                manifest path whose lock sha differs counts as changed), which is
//	                a safe over-estimate used only when disk hashing is unavailable.
//
// On any error from the injected funcs, CheckAssetSkew returns the error and a
// zero-value SkewResult (treat as "could not determine skew" — do not notify).
func CheckAssetSkew(
	buildManifest func() (Manifest, error),
	readLock func() (Lock, error),
	diskHashes func(Manifest) (map[string]string, error),
) (SkewResult, error) {
	m, err := buildManifest()
	if err != nil {
		return SkewResult{}, err
	}
	lock, err := readLock()
	if err != nil {
		return SkewResult{}, err
	}

	res := SkewResult{
		BinaryDigest: m.Digest(),
		LockDigest:   lock.Digest(),
		NeverSynced:  len(lock.Files) == 0,
	}
	res.Skewed = res.BinaryDigest != res.LockDigest

	if !res.Skewed {
		return res, nil
	}

	var disk map[string]string
	if diskHashes != nil {
		disk, err = diskHashes(m)
		if err != nil {
			return SkewResult{}, err
		}
	}

	if disk != nil {
		plan := Reconcile(m, lock, disk)
		for _, it := range plan {
			switch it.Action {
			case ActionSkip, ActionLeave:
			case ActionConflict:
				res.ChangedCount++
				res.ConflictCount++
			default: // Create, FastForward
				res.ChangedCount++
				if it.Class == Managed && it.Action == ActionFastForward {
					res.AutoApplyCandidates++
				}
			}
		}
		return res, nil
	}

	for _, f := range m.Files {
		le, ok := lock.Files[f.Path]
		if !ok || le.Sha256 != f.Sha256 {
			res.ChangedCount++
		}
	}
	return res, nil
}
