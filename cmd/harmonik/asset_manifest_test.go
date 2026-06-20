package main

// asset_manifest_test.go — guards the embedded asset MANIFEST: total coverage of
// the embed FS, correct per-class classification, deterministic digest, and
// hash-matches-content. Extends the embed-in-sync style from
// init_skills_sync_test.go.
//
// Bead ref: hk-532v (asset-manifest).

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"testing"
)

// TestManifestCoversEveryEmbeddedFile asserts the manifest is a TOTAL cover of
// the embed FS: every non-directory file under assets/ appears exactly once, and
// none is left unclassified. A new asset added to the embed without a class
// mapping fails here.
func TestManifestCoversEveryEmbeddedFile(t *testing.T) {
	m, err := BuildManifest()
	if err != nil {
		t.Fatalf("BuildManifest: %v", err)
	}

	if m.FormatVersion != ManifestFormatVersion {
		t.Errorf("FormatVersion = %d, want %d", m.FormatVersion, ManifestFormatVersion)
	}

	manifested := map[string]FileEntry{}
	for _, f := range m.Files {
		if _, dup := manifested[f.Path]; dup {
			t.Errorf("duplicate manifest entry for %s", f.Path)
		}
		manifested[f.Path] = f
	}

	// Walk the embed FS independently and confirm 1:1 coverage.
	walked := map[string]bool{}
	err = fs.WalkDir(initSkillAssets, assetEmbedRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		walked[path] = true
		if _, ok := manifested[path]; !ok {
			t.Errorf("embedded asset %s is MISSING from the manifest", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk embed FS: %v", err)
	}

	for p := range manifested {
		if !walked[p] {
			t.Errorf("manifest lists %s which is not in the embed FS", p)
		}
	}

	if len(walked) == 0 {
		t.Fatal("embed FS walk found no files — embed misconfigured")
	}

	// No asset may be Unclassified: every shipped path must map to a real class
	// so the reconcile engine knows how to sync it.
	for _, f := range m.Files {
		if f.Class == Unclassified {
			t.Errorf("asset %s is Unclassified — add a class mapping in Classify()", f.Path)
		}
	}
}

// TestClassifyRepresentativePaths checks Classify returns the right class for one
// representative path per class.
func TestClassifyRepresentativePaths(t *testing.T) {
	cases := []struct {
		path string
		want AssetClass
	}{
		{"assets/skills/keeper/SKILL.md", Managed},
		{"assets/templates/AGENTS.template.md", ManagedRegion},
		{"assets/context/project.yaml.tmpl", ContentOwned},
		{"assets/scaffolds/STATUS.md", Scaffold},
		{"assets/something/unknown.txt", Unclassified},
		// Pre-stripped (root-relative) form classifies identically.
		{"skills/keeper/SKILL.md", Managed},
	}
	for _, c := range cases {
		if got := Classify(c.path); got != c.want {
			t.Errorf("Classify(%q) = %q, want %q", c.path, got, c.want)
		}
	}
}

// TestManifestDigestDeterministic asserts two independent BuildManifest calls
// produce the same digest (and the same entry list).
func TestManifestDigestDeterministic(t *testing.T) {
	a, err := BuildManifest()
	if err != nil {
		t.Fatalf("BuildManifest a: %v", err)
	}
	b, err := BuildManifest()
	if err != nil {
		t.Fatalf("BuildManifest b: %v", err)
	}

	if a.Digest() != b.Digest() {
		t.Errorf("digest not deterministic:\n  a=%s\n  b=%s", a.Digest(), b.Digest())
	}
	if len(a.Files) != len(b.Files) {
		t.Fatalf("entry count differs: a=%d b=%d", len(a.Files), len(b.Files))
	}
	for i := range a.Files {
		if a.Files[i] != b.Files[i] {
			t.Errorf("entry %d differs: a=%+v b=%+v", i, a.Files[i], b.Files[i])
		}
	}
	if a.Digest() == "" {
		t.Error("digest is empty")
	}
}

// TestManifestHashMatchesContent recomputes the sha256 of a known embedded asset
// and asserts the manifest recorded the same hash.
func TestManifestHashMatchesContent(t *testing.T) {
	const known = "assets/skills/keeper/SKILL.md"

	data, err := initSkillAssets.ReadFile(known)
	if err != nil {
		t.Fatalf("read %s: %v", known, err)
	}
	sum := sha256.Sum256(data)
	want := hex.EncodeToString(sum[:])

	m, err := BuildManifest()
	if err != nil {
		t.Fatalf("BuildManifest: %v", err)
	}

	var found *FileEntry
	for i := range m.Files {
		if m.Files[i].Path == known {
			found = &m.Files[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("known asset %s not in manifest", known)
	}
	if found.Sha256 != want {
		t.Errorf("manifest sha for %s = %s, want recomputed %s", known, found.Sha256, want)
	}
	if found.Class != Managed {
		t.Errorf("known asset %s class = %q, want %q", known, found.Class, Managed)
	}
}
