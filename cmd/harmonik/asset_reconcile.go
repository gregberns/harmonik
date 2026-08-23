package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/gregberns/harmonik/internal/core"
)

// LockFormatVersion is the schema version of the assets.lock structure. Bump it
// when LockEntry / Lock gain or change fields so ReadLock can detect/migrate
// older on-disk locks. Starts at 1.
const LockFormatVersion = 1

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
		out.FormatVersion = LockFormatVersion
	}
	for _, p := range paths {
		e := l.Files[p]
		e.Path = p
		out.Files = append(out.Files, e)
	}

	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal lock: %w", err)
	}
	data = append(data, '\n')

	full := filepath.Join(dir, lockRelPath)
	if err := os.MkdirAll(filepath.Dir(full), core.HarmonikDirMode); err != nil {
		return fmt.Errorf("mkdir for lock: %w", err)
	}
	if err := os.WriteFile(full, data, 0o600); err != nil {
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
	embed := make(map[string]FileEntry, len(m.Files))
	for _, f := range m.Files {
		embed[f.Path] = f
	}

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
			item.Class = Classify(p)
			item.Action = ActionLeave
			item.Reason = "path not in embed manifest; project-authored, left untouched"

		case !inLock:
			item.Class = ef.Class
			switch diskSha {
			case "":
				item.Action = ActionCreate
				item.Reason = "new embedded asset, absent on disk; create"
			case ef.Sha256:
				item.Action = ActionSkip
				item.Reason = "new embedded asset but disk already matches embed; skip (lock should be re-stamped)"
			default:
				item.Action = ActionConflict
				item.Reason = "new embedded asset but disk differs from embed; conflict"
			}

		default:
			item.Class = ef.Class
			switch {
			case ef.Sha256 == le.Sha256:
				if diskSha == "" {
					item.Action = ActionCreate
					item.Reason = "embed==lock but disk missing; restore current file"
				} else {
					item.Action = ActionSkip
					item.Reason = "embed==lock; already current"
				}
			case diskSha == ef.Sha256:
				item.Action = ActionSkip
				item.Reason = "disk already matches embed; skip (lock should be re-stamped)"
			case diskSha == le.Sha256:
				item.Action = ActionFastForward
				item.Reason = "embed!=lock and disk==lock; no local edits, safe to update"
			case diskSha == "":
				item.Action = ActionCreate
				item.Reason = "embed!=lock and disk missing; create from embed"
			default:
				item.Action = ActionConflict
				item.Reason = "embed!=lock and disk differs from both; project edited a managed file"
			}
		}

		items = append(items, item)
	}

	return items
}
