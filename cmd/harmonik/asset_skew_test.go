package main

import "testing"

// fakeManifest builds a Manifest from a path→sha map for test injection.
func fakeManifest(entries map[string]struct {
	sha   string
	class AssetClass
}) Manifest {
	m := Manifest{FormatVersion: ManifestFormatVersion}
	for p, e := range entries {
		m.Files = append(m.Files, FileEntry{Path: p, Sha256: e.sha, Class: e.class})
	}
	// BuildManifest sorts; Digest is order-independent of the slice anyway, but keep
	// it deterministic.
	return m
}

func lockFromPairs(pairs map[string]string) Lock {
	l := Lock{FormatVersion: LockFormatVersion, Files: map[string]LockEntry{}}
	for p, s := range pairs {
		l.Files[p] = LockEntry{Path: p, Sha256: s}
	}
	return l
}

// TestLockDigestMatchesManifestDigest — a lock recording exactly the manifest's
// path:sha set produces the SAME digest as the manifest, so they are comparable.
func TestLockDigestMatchesManifestDigest(t *testing.T) {
	m := fakeManifest(map[string]struct {
		sha   string
		class AssetClass
	}{
		"assets/skills/keeper/SKILL.md":       {"aaa", Managed},
		"assets/templates/AGENTS.template.md": {"bbb", ManagedRegion},
	})
	lock := lockFromPairs(map[string]string{
		"assets/skills/keeper/SKILL.md":       "aaa",
		"assets/templates/AGENTS.template.md": "bbb",
	})
	if m.Digest() != lock.Digest() {
		t.Fatalf("digests should match: manifest=%s lock=%s", m.Digest(), lock.Digest())
	}
}

// TestSkewEqualNoSkew — lock equals manifest → not skewed, zero change count.
func TestSkewEqualNoSkew(t *testing.T) {
	m := fakeManifest(map[string]struct {
		sha   string
		class AssetClass
	}{
		"assets/skills/keeper/SKILL.md": {"aaa", Managed},
	})
	lock := lockFromPairs(map[string]string{"assets/skills/keeper/SKILL.md": "aaa"})
	disk := map[string]string{"assets/skills/keeper/SKILL.md": "aaa"}

	res, err := CheckAssetSkew(
		func() (Manifest, error) { return m, nil },
		func() (Lock, error) { return lock, nil },
		func(Manifest) (map[string]string, error) { return disk, nil },
	)
	if err != nil {
		t.Fatal(err)
	}
	if res.Skewed {
		t.Fatalf("expected no skew, got Skewed=true")
	}
	if res.ChangedCount != 0 {
		t.Fatalf("expected 0 changes, got %d", res.ChangedCount)
	}
}

// TestSkewLockBehindCountsChanges — manifest advanced past the lock; disk still at
// the locked sha (no local edits) → skew with the right change count and a
// FastForward Managed item flagged auto-apply-safe.
func TestSkewLockBehindCountsChanges(t *testing.T) {
	m := fakeManifest(map[string]struct {
		sha   string
		class AssetClass
	}{
		"assets/skills/keeper/SKILL.md":  {"NEW1", Managed},
		"assets/skills/captain/SKILL.md": {"NEW2", Managed},
		"assets/context/project.yaml.tmpl": {"NEW3", ContentOwned},
	})
	// Lock + disk both still at the OLD shas for all three → 3 fast-forwards.
	lock := lockFromPairs(map[string]string{
		"assets/skills/keeper/SKILL.md":     "OLD1",
		"assets/skills/captain/SKILL.md":    "OLD2",
		"assets/context/project.yaml.tmpl":  "OLD3",
	})
	disk := map[string]string{
		"assets/skills/keeper/SKILL.md":    "OLD1",
		"assets/skills/captain/SKILL.md":   "OLD2",
		"assets/context/project.yaml.tmpl": "OLD3",
	}

	res, err := CheckAssetSkew(
		func() (Manifest, error) { return m, nil },
		func() (Lock, error) { return lock, nil },
		func(Manifest) (map[string]string, error) { return disk, nil },
	)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Skewed {
		t.Fatal("expected skew")
	}
	if res.ChangedCount != 3 {
		t.Fatalf("expected 3 changed, got %d", res.ChangedCount)
	}
	// Two Managed fast-forwards are auto-apply-safe; the ContentOwned one is not.
	if res.AutoApplyCandidates != 2 {
		t.Fatalf("expected 2 auto-apply candidates (Managed FF only), got %d", res.AutoApplyCandidates)
	}
	if res.ConflictCount != 0 {
		t.Fatalf("expected 0 conflicts, got %d", res.ConflictCount)
	}
}

// TestSkewConflictCounted — manifest advanced AND disk diverged from both lock and
// embed → a Conflict, counted as changed but NOT auto-apply-safe.
func TestSkewConflictCounted(t *testing.T) {
	m := fakeManifest(map[string]struct {
		sha   string
		class AssetClass
	}{
		"assets/skills/keeper/SKILL.md": {"NEW", Managed},
	})
	lock := lockFromPairs(map[string]string{"assets/skills/keeper/SKILL.md": "OLD"})
	disk := map[string]string{"assets/skills/keeper/SKILL.md": "LOCALEDIT"} // diverged

	res, err := CheckAssetSkew(
		func() (Manifest, error) { return m, nil },
		func() (Lock, error) { return lock, nil },
		func(Manifest) (map[string]string, error) { return disk, nil },
	)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Skewed {
		t.Fatal("expected skew")
	}
	if res.ChangedCount != 1 || res.ConflictCount != 1 {
		t.Fatalf("expected 1 changed/1 conflict, got changed=%d conflict=%d", res.ChangedCount, res.ConflictCount)
	}
	if res.AutoApplyCandidates != 0 {
		t.Fatalf("conflicts must never be auto-apply candidates, got %d", res.AutoApplyCandidates)
	}
}

// TestSkewAbsentLockNeverSynced — an empty lock (never synced) is skewed against any
// shipped asset and flagged NeverSynced.
func TestSkewAbsentLockNeverSynced(t *testing.T) {
	m := fakeManifest(map[string]struct {
		sha   string
		class AssetClass
	}{
		"assets/skills/keeper/SKILL.md": {"aaa", Managed},
	})
	emptyLock := Lock{FormatVersion: LockFormatVersion, Files: map[string]LockEntry{}}
	disk := map[string]string{"assets/skills/keeper/SKILL.md": ""} // absent on disk too

	res, err := CheckAssetSkew(
		func() (Manifest, error) { return m, nil },
		func() (Lock, error) { return emptyLock, nil },
		func(Manifest) (map[string]string, error) { return disk, nil },
	)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Skewed {
		t.Fatal("never-synced project with shipped assets must be skewed")
	}
	if !res.NeverSynced {
		t.Fatal("expected NeverSynced=true for empty lock")
	}
	if res.ChangedCount != 1 {
		t.Fatalf("expected 1 change (a Create), got %d", res.ChangedCount)
	}
}

// TestSkewNilDiskHashesOverEstimate — when disk hashing is unavailable, skew is
// derived from manifest-vs-lock alone (over-estimate); still detects skew + count.
func TestSkewNilDiskHashesOverEstimate(t *testing.T) {
	m := fakeManifest(map[string]struct {
		sha   string
		class AssetClass
	}{
		"assets/skills/keeper/SKILL.md":  {"NEW", Managed},
		"assets/skills/captain/SKILL.md": {"SAME", Managed},
	})
	lock := lockFromPairs(map[string]string{
		"assets/skills/keeper/SKILL.md":  "OLD",  // differs → counted
		"assets/skills/captain/SKILL.md": "SAME", // same → not counted
	})

	res, err := CheckAssetSkew(
		func() (Manifest, error) { return m, nil },
		func() (Lock, error) { return lock, nil },
		nil, // no disk hasher
	)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Skewed {
		t.Fatal("expected skew")
	}
	if res.ChangedCount != 1 {
		t.Fatalf("expected 1 changed (over-estimate), got %d", res.ChangedCount)
	}
	if res.AutoApplyCandidates != 0 {
		t.Fatalf("over-estimate path cannot flag auto-apply, got %d", res.AutoApplyCandidates)
	}
}
