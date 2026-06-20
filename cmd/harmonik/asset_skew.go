package main

// asset_skew.go — supervisor-side VERSION-SKEW DETECTION for the asset-sync update
// path (plans/2026-06-20-doc-instruction-audit/10-asset-sync.md §"Daemon-safety" +
// §"Trigger model"). This is the DETECTION + decision layer; the notify + supervisor
// wiring lives in cmd/harmonik/supervise/assetskew.go.
//
// The problem: `init` writes the embedded assets into a project ONCE. After a newer
// harmonik is `go install`ed, the project's instruction files are frozen at the
// version that ran init. The supervisor is the natural home to NOTICE this: it boots
// alongside the daemon and can compare the RUNNING binary's embedded asset bundle
// against what the project last installed (.harmonik/assets.lock), then nudge the
// captain to run `harmonik sync-assets`.
//
// SAFETY: this layer NEVER writes project files. It computes two comparable digests
// (binary-manifest vs. project-lock) and, on skew, the COUNT of files that would
// change. Notify-not-clobber. The optional auto-apply gate is config-driven and
// deferred to the supervisor wiring; see SkewResult.AutoApplyCandidates.
//
// Bead ref: hk-yqx9 (supervisor version-skew detection + notify).

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"sort"
	"strings"

	supervisecmd "github.com/gregberns/harmonik/cmd/harmonik/supervise"
)

// init installs the supervisor's skew-check hook. The supervisor (supervisecmd)
// is imported BY main and cannot import main back, so we bridge the detection logic
// (which needs the embedded manifest + reconcile planner, both here in main) via this
// registration. supervisecmd.RunAssetSkewCheck calls SkewCheckHook at boot.
func init() {
	supervisecmd.SkewCheckHook = func(projectDir string) (supervisecmd.AssetSkewVerdict, error) {
		res, err := CheckAssetSkew(
			BuildManifest,
			func() (Lock, error) { return ReadLock(projectDir) },
			func(m Manifest) (map[string]string, error) {
				return buildDiskHashes(projectDir, m, io.Discard)
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

	// No skew → nothing more to compute. (A never-synced project with NO shipped
	// assets would also land here with Skewed=false, which is correct.)
	if !res.Skewed {
		return res, nil
	}

	// Compute the precise change set via the real reconcile planner when disk hashes
	// are available; otherwise fall back to a manifest-vs-lock over-estimate.
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
				// No project change.
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

	// Disk-hash unavailable: over-estimate from manifest vs lock. Every manifest path
	// whose lock sha differs (or is absent) is counted as a change; FastForward /
	// conflict cannot be distinguished here so none are flagged auto-apply-safe.
	for _, f := range m.Files {
		le, ok := lock.Files[f.Path]
		if !ok || le.Sha256 != f.Sha256 {
			res.ChangedCount++
		}
	}
	return res, nil
}
