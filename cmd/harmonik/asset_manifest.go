package main

// asset_manifest.go — build-time/runtime MANIFEST of every embedded instruction
// asset (embed path → sha256 + sync CLASS), plus the shared TYPES the upcoming
// reconcile engine (hk-gh1m) and `sync-assets` command (hk-i7i3) consume.
//
// This is the FOUNDATION layer of the asset-sync design
// (plans/2026-06-20-doc-instruction-audit/10-asset-sync.md). It deliberately does
// NOT implement the per-project lock file, the 3-way reconcile diff, or the
// command — only the manifest + class model. The reconcile engine reads
// BuildManifest() (embed-side hashes) and compares against the on-disk lock.
//
// Asset source: the //go:embed assets FS declared in init_skill_assets.go
// (var initSkillAssets). Layout recap:
//
//	assets/skills/<name>/...            — product-owned fleet skills      → Managed
//	assets/templates/AGENTS.template.md — marker-delimited router         → ManagedRegion
//	assets/context/*.tmpl               — project-owned scaffold bodies    → ContentOwned
//	assets/scaffolds/*.md               — create-once stub files          → Scaffold
//
// Bead ref: hk-532v (asset-manifest).

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"sort"
	"strings"
)

// ManifestFormatVersion is the schema version of the manifest structure itself.
// Bump it when FileEntry / Manifest gain or change fields so consumers
// (reconcile engine, lock reader) can detect and migrate older formats.
const ManifestFormatVersion = 1

// assetEmbedRoot is the directory the assets are embedded under (see the
// //go:embed assets directive in init_skill_assets.go). Embed paths are always
// rooted here, e.g. "assets/skills/keeper/SKILL.md".
const assetEmbedRoot = "assets"

// AssetClass identifies how an embedded instruction asset is reconciled into a
// project during init / sync-assets. The class drives the per-file sync policy
// in the 3-way reconcile (overwrite vs. region-merge vs. never-overwrite).
type AssetClass string

const (
	// Managed assets are product-owned: the skills under assets/skills/*.
	// Projects should not edit them; on update they are overwritten from the
	// embed FS (local drift is reported, not silently clobbered).
	Managed AssetClass = "managed"

	// ManagedRegion assets are shared: the AGENTS router template under
	// assets/templates/. Only marker-delimited regions update; project deltas
	// outside the markers are preserved.
	ManagedRegion AssetClass = "managed-region"

	// ContentOwned assets are project-owned: the context scaffolds under
	// assets/context/*.tmpl. The body is never overwritten — sync may only
	// create-if-missing or refresh the self-describing header region.
	ContentOwned AssetClass = "content-owned"

	// Scaffold assets are create-once stub files under assets/scaffolds/* (e.g.
	// AGENT_INDEX/STATUS/TASKS). They are written when missing and otherwise
	// left to the project; they carry no managed body to keep in sync.
	Scaffold AssetClass = "scaffold"

	// Unclassified is the fallback for any embedded path that matches no known
	// class prefix. The manifest still records it (so coverage is total), but a
	// reconcile engine should treat it conservatively (create-if-missing only).
	Unclassified AssetClass = "unclassified"
)

// Classify maps an embedded asset path (e.g. "assets/skills/keeper/SKILL.md") to
// its AssetClass. The path is matched by its leading directory segment under the
// embed root; classification is purely structural so it stays in lockstep with
// the asset layout documented in init_skill_assets.go.
func Classify(path string) AssetClass {
	// Normalize: strip the embed root prefix so both "assets/skills/..." and a
	// pre-stripped "skills/..." classify identically.
	rel := strings.TrimPrefix(path, assetEmbedRoot+"/")

	switch {
	case strings.HasPrefix(rel, "skills/"):
		return Managed
	case strings.HasPrefix(rel, "templates/"):
		return ManagedRegion
	case strings.HasPrefix(rel, "context/"):
		return ContentOwned
	case strings.HasPrefix(rel, "scaffolds/"):
		return Scaffold
	default:
		return Unclassified
	}
}

// FileEntry is a single embedded asset's manifest record: its embed path, the
// sha256 of its content (hex), and its sync class.
type FileEntry struct {
	Path   string
	Sha256 string
	Class  AssetClass
}

// Manifest is the deterministic, sorted set of every embedded instruction asset
// together with its content hash and sync class. It is the embed-side input to
// the 3-way reconcile: reconcile compares Manifest hashes (what the binary ships)
// against the per-project assets.lock (what was last installed) and the on-disk
// files (what's there now).
type Manifest struct {
	FormatVersion int
	Files         []FileEntry
}

// BuildManifest walks the embedded asset FS, sha256-hashes each file, classifies
// it, and returns a manifest sorted by path for deterministic output. Every file
// reachable under the embed root is included so the manifest is a total cover of
// the shipped assets.
func BuildManifest() (Manifest, error) {
	m := Manifest{FormatVersion: ManifestFormatVersion}

	err := fs.WalkDir(initSkillAssets, assetEmbedRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		data, readErr := initSkillAssets.ReadFile(path)
		if readErr != nil {
			return fmt.Errorf("read embedded asset %s: %w", path, readErr)
		}
		sum := sha256.Sum256(data)
		m.Files = append(m.Files, FileEntry{
			Path:   path,
			Sha256: hex.EncodeToString(sum[:]),
			Class:  Classify(path),
		})
		return nil
	})
	if err != nil {
		return Manifest{}, err
	}

	sort.Slice(m.Files, func(i, j int) bool {
		return m.Files[i].Path < m.Files[j].Path
	})
	return m, nil
}

// Digest returns a single sha256 (hex) over the manifest's sorted "path:sha\n"
// lines. It is a stable fingerprint of the whole embedded asset set: two binaries
// with byte-identical assets produce the same digest, and any asset change moves
// it. The supervisor's version-skew check (hk, doc 10 §Daemon-safety) can compare
// this against the project's recorded digest to detect "assets N behind".
func (m Manifest) Digest() string {
	var b strings.Builder
	for _, f := range m.Files {
		b.WriteString(f.Path)
		b.WriteByte(':')
		b.WriteString(f.Sha256)
		b.WriteByte('\n')
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}
