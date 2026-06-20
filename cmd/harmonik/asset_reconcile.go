package main

// asset_reconcile.go — the per-project LOCK file (.harmonik/assets.lock) plus the
// PURE 3-way reconcile PLANNER that drives `harmonik sync-assets` (and the empty-
// project case used by `init`).
//
// This is the CORE logic of the asset-sync update path
// (plans/2026-06-20-doc-instruction-audit/10-asset-sync.md §"The 3-way reconcile").
// It builds on the manifest + class model in asset_manifest.go (hk-532v): it
// reuses AssetClass / FileEntry / Manifest and produces a typed PLAN. It does NOT
// perform any file writes, region-merges, or scaffold-once mechanics — that is the
// executor's job (hk-i7i3) — nor the command / supervisor wiring (hk-yqx9).
//
// Three inputs, one comparison per path:
//
//	embed_sha = Manifest[path].Sha256        # what the binary ships now
//	lock_sha  = Lock.Files[path].Sha256      # what we last installed
//	disk_sha  = diskHashes[path]             # what's there now ("" = absent)
//
// Reconcile returns one ReconcileItem per path it considered (union of manifest
// paths and disk paths), each carrying the file's AssetClass so the executor can
// apply the class policy (Managed=overwrite/.harmonik-new, ManagedRegion=region-
// merge, ContentOwned=create-or-header-only, Scaffold=create-once).
//
// Bead ref: hk-gh1m (assets.lock + 3-way reconcile engine).

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// LockFormatVersion is the schema version of the assets.lock structure. Bump it
// when LockEntry / Lock gain or change fields so ReadLock can detect/migrate
// older on-disk locks. Starts at 1.
const LockFormatVersion = 1

// lockRelPath is the project-relative location of the lock file.
const lockRelPath = ".harmonik/assets.lock"

// LockEntry records, for one asset path, the sha256 (hex) that was installed at
// the last init/sync. The Path is kept on the entry (in addition to the map key)
// so a serialized entry is self-describing.
type LockEntry struct {
	Path   string `json:"path"`
	Sha256 string `json:"sha256"`
}

// Lock is the per-project record of "what we installed and at what hash" — the
// middle leg of the 3-way reconcile. It is keyed by asset path (the same embed
// path the manifest uses). A zero-value Lock (FormatVersion 0, nil/empty Files)
// represents a never-synced project and reconciles every embedded asset as new.
type Lock struct {
	FormatVersion int                  `json:"format_version"`
	Files         map[string]LockEntry `json:"files"`
}

// LockFromManifest builds the Lock that should be stamped after an apply: every
// manifest entry recorded at the hash the binary just shipped. Call this after a
// successful sync-assets apply and WriteLock the result.
func LockFromManifest(m Manifest) Lock {
	l := Lock{
		FormatVersion: LockFormatVersion,
		Files:         make(map[string]LockEntry, len(m.Files)),
	}
	for _, f := range m.Files {
		l.Files[f.Path] = LockEntry{Path: f.Path, Sha256: f.Sha256}
	}
	return l
}

// lockJSON is the on-disk wire shape: a sorted slice (not a map) so the JSON is
// byte-deterministic regardless of Go's map-iteration order.
type lockJSON struct {
	FormatVersion int         `json:"format_version"`
	Files         []LockEntry `json:"files"`
}

// WriteLock writes the lock to <dir>/.harmonik/assets.lock as deterministic,
// sorted JSON (creating the .harmonik directory if needed). Two locks with the
// same entries always serialize to byte-identical output.
func WriteLock(dir string, l Lock) error {
	paths := make([]string, 0, len(l.Files))
	for p := range l.Files {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	out := lockJSON{
		FormatVersion: l.FormatVersion,
		Files:         make([]LockEntry, 0, len(paths)),
	}
	if out.FormatVersion == 0 {
		// Never write a 0/unknown version to disk; stamp current.
		out.FormatVersion = LockFormatVersion
	}
	for _, p := range paths {
		e := l.Files[p]
		// Keep the entry's Path in lockstep with the key.
		e.Path = p
		out.Files = append(out.Files, e)
	}

	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal lock: %w", err)
	}
	data = append(data, '\n')

	full := filepath.Join(dir, lockRelPath)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return fmt.Errorf("mkdir for lock: %w", err)
	}
	if err := os.WriteFile(full, data, 0o644); err != nil {
		return fmt.Errorf("write lock %s: %w", full, err)
	}
	return nil
}

// ReadLock reads <dir>/.harmonik/assets.lock. When the file is ABSENT it returns
// an empty (zero-entry) Lock and a nil error — a never-synced project is not an
// error condition; reconcile treats every embedded asset as new. Any other I/O or
// parse failure is returned.
func ReadLock(dir string) (Lock, error) {
	full := filepath.Join(dir, lockRelPath)
	data, err := os.ReadFile(full)
	if err != nil {
		if os.IsNotExist(err) {
			return Lock{FormatVersion: LockFormatVersion, Files: map[string]LockEntry{}}, nil
		}
		return Lock{}, fmt.Errorf("read lock %s: %w", full, err)
	}

	var wire lockJSON
	if err := json.Unmarshal(data, &wire); err != nil {
		return Lock{}, fmt.Errorf("parse lock %s: %w", full, err)
	}

	l := Lock{
		FormatVersion: wire.FormatVersion,
		Files:         make(map[string]LockEntry, len(wire.Files)),
	}
	for _, e := range wire.Files {
		l.Files[e.Path] = LockEntry{Path: e.Path, Sha256: e.Sha256}
	}
	return l, nil
}

// Action is the typed verdict the reconcile planner reaches for one path. The
// executor (hk-i7i3) consumes Action together with Class to decide the concrete
// file operation; the planner itself performs no I/O.
type Action string

const (
	// ActionSkip — nothing to do: the embed already matches the lock (file is
	// current), or disk already matches the embed (lock merely stale).
	ActionSkip Action = "skip"

	// ActionCreate — write the file fresh: it is in the manifest but absent on
	// disk (and not yet recorded current), so there is no local content to merge.
	ActionCreate Action = "create"

	// ActionFastForward — the embed advanced past the lock AND disk still equals
	// the lock, i.e. no local edits: it is safe to update from embed.
	ActionFastForward Action = "fast-forward"

	// ActionConflict — the embed advanced AND disk diverged from BOTH the lock and
	// the embed: the project edited a managed file. The executor preserves local
	// content (Managed → write .harmonik-new; region/content → merge the managed
	// region only) and reports.
	ActionConflict Action = "conflict"

	// ActionLeave — the path exists on disk but is NOT in the embed manifest:
	// project-authored, left untouched.
	ActionLeave Action = "leave"
)

// ReconcileItem is the planner's per-path verdict. Class is always populated
// (from the manifest for embed paths, or Classify(path) for disk-only paths) so
// the executor can apply the class policy. The three *Sha fields are the inputs
// that produced Action; Reason is a short human-readable explanation.
type ReconcileItem struct {
	Path     string
	Class    AssetClass
	Action   Action
	EmbedSha string
	LockSha  string
	DiskSha  string
	Reason   string
}

// Reconcile is the PURE 3-way reconcile planner. It compares, per path, the embed
// manifest, the project lock, and the on-disk hashes, and emits one ReconcileItem
// per path it considered. diskHashes maps embed-path → sha256 of the project's
// current on-disk file ("" when the file is absent).
//
// The matrix (doc 10 §"The 3-way reconcile"), refined for the missing/stale-lock
// edge cases:
//
//	embed == lock:
//	    disk present            → Skip   (already current)
//	    disk absent             → Create (restore a deleted current file)
//	embed != lock:
//	    disk == embed           → Skip   (already matches embed; lock is stale — re-stamp)
//	    disk == lock            → FastForward (no local edits; safe update)
//	    disk absent             → Create (no local content to merge)
//	    else (disk != both)     → Conflict   (project edited a managed file)
//	path in embed, not in lock:
//	    disk absent             → Create
//	    disk == embed           → Skip   (independently identical; re-stamp)
//	    else                    → Conflict
//	path on disk, not in embed  → Leave  (project-authored)
//
// Output is sorted by path for deterministic plans.
func Reconcile(m Manifest, lock Lock, diskHashes map[string]string) []ReconcileItem {
	// Index embed entries by path.
	embed := make(map[string]FileEntry, len(m.Files))
	for _, f := range m.Files {
		embed[f.Path] = f
	}

	// Collect the union of all paths we must consider: every embed path plus
	// every disk path (the latter surfaces project-authored Leave items).
	considered := make(map[string]struct{}, len(embed)+len(diskHashes))
	for p := range embed {
		considered[p] = struct{}{}
	}
	for p := range diskHashes {
		considered[p] = struct{}{}
	}

	paths := make([]string, 0, len(considered))
	for p := range considered {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	items := make([]ReconcileItem, 0, len(paths))
	for _, p := range paths {
		ef, inEmbed := embed[p]
		le, inLock := lock.Files[p]
		diskSha := diskHashes[p] // "" if absent

		item := ReconcileItem{
			Path:     p,
			EmbedSha: ef.Sha256,
			LockSha:  le.Sha256,
			DiskSha:  diskSha,
		}

		switch {
		case !inEmbed:
			// On disk (or lock) but not shipped by the binary → project-authored.
			item.Class = Classify(p)
			item.Action = ActionLeave
			item.Reason = "path not in embed manifest; project-authored, left untouched"

		case !inLock:
			// New asset: shipped but never installed here.
			item.Class = ef.Class
			switch {
			case diskSha == "":
				item.Action = ActionCreate
				item.Reason = "new embedded asset, absent on disk; create"
			case diskSha == ef.Sha256:
				item.Action = ActionSkip
				item.Reason = "new embedded asset but disk already matches embed; skip (lock should be re-stamped)"
			default:
				item.Action = ActionConflict
				item.Reason = "new embedded asset but disk differs from embed; conflict"
			}

		default:
			// In both embed and lock: the canonical 3-way matrix.
			item.Class = ef.Class
			switch {
			case ef.Sha256 == le.Sha256:
				// Embed already matches the lock: file is current.
				if diskSha == "" {
					item.Action = ActionCreate
					item.Reason = "embed==lock but disk missing; restore current file"
				} else {
					item.Action = ActionSkip
					item.Reason = "embed==lock; already current"
				}
			case diskSha == ef.Sha256:
				// Embed advanced but disk already equals the new embed: lock stale.
				item.Action = ActionSkip
				item.Reason = "disk already matches embed; skip (lock should be re-stamped)"
			case diskSha == le.Sha256:
				// Disk still at the locked version: no local edits → safe update.
				item.Action = ActionFastForward
				item.Reason = "embed!=lock and disk==lock; no local edits, safe to update"
			case diskSha == "":
				// File deleted locally: nothing to merge, write fresh embed.
				item.Action = ActionCreate
				item.Reason = "embed!=lock and disk missing; create from embed"
			default:
				// Disk diverged from both lock and embed: local edits to a managed file.
				item.Action = ActionConflict
				item.Reason = "embed!=lock and disk differs from both; project edited a managed file"
			}
		}

		items = append(items, item)
	}

	return items
}
