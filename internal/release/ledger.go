// Package release — ledger transition logic.
//
// This file contains pure functions that implement ledger state transitions
// (certify, yank) and ledger invariant checks. No file I/O here; see
// ledger_file.go for JSON persistence.
//
// Spec ref: specs/release-pipeline.md §4.3 (invariants), §6 (CERTIFY), §7 (ROLLBACK).
package release

import "errors"

// Sentinel errors for ledger transition violations.
var (
	// ErrSemverNotFound is returned when an operation targets a semver that
	// does not exist in the ledger.
	ErrSemverNotFound = errors.New("release: semver not found in ledger")

	// ErrAlreadyCertified is returned when Certify is called on an entry that
	// has already been certified (CertifiedAt non-empty).
	// Spec ref: specs/release-pipeline.md §6 — "CERTIFY is idempotent".
	ErrAlreadyCertified = errors.New("release: entry is already certified")

	// ErrYankIsIrreversible is returned when Certify is called on a yanked entry.
	// Spec ref: specs/release-pipeline.md §7.3 — "YANKED → CERTIFIED: not supported".
	ErrYankIsIrreversible = errors.New("release: yanked release cannot be re-certified (yank is irreversible)")

	// ErrYankReasonEmpty is returned when Yank is called without a reason.
	// Spec ref: specs/release-pipeline.md §7.1 — "Yanked: true MUST be accompanied by a non-empty YankedReason".
	ErrYankReasonEmpty = errors.New("release: yank reason must be non-empty")

	// ErrNotCertified is returned when Yank is called on an entry that has not
	// yet been certified. Pre-release entries are discarded, not yanked.
	ErrNotCertified = errors.New("release: only certified entries can be yanked")

	// ErrAlreadyYanked is returned when Yank is called on an entry that is
	// already yanked.
	ErrAlreadyYanked = errors.New("release: entry is already yanked")
)

// RecordCreate appends a CREATE-stage pre-release entry when semver is absent.
// If the semver is already present, the input ledger is returned unchanged so
// rerunning the tag workflow cannot roll a certified release back to pre-release.
func RecordCreate(entries []ReleaseEntry, entry ReleaseEntry) []ReleaseEntry {
	if indexBySemver(entries, entry.Semver) >= 0 {
		result := make([]ReleaseEntry, len(entries))
		copy(result, entries)
		return result
	}
	entry.Prerelease = true
	entry.CertifiedAt = ""
	entry.Yanked = false
	entry.YankedReason = ""

	result := make([]ReleaseEntry, 0, len(entries)+1)
	result = append(result, entries...)
	result = append(result, entry)
	return result
}

// Certify flips the prerelease flag to false and stamps the certified_at
// timestamp on the entry matching semver. Returns a new slice (does not mutate
// the input) and an error if the transition is invalid.
//
// Error cases:
//   - ErrSemverNotFound   — semver not in ledger
//   - ErrAlreadyCertified — entry already has CertifiedAt set (double-certify)
//   - ErrYankIsIrreversible — entry is yanked; yank cannot be reversed
//
// Spec ref: specs/release-pipeline.md §6.
func Certify(entries []ReleaseEntry, semver, certifiedAt string) ([]ReleaseEntry, error) {
	idx := indexBySemver(entries, semver)
	if idx < 0 {
		return nil, ErrSemverNotFound
	}
	e := entries[idx]
	if e.Yanked {
		return nil, ErrYankIsIrreversible
	}
	if e.CertifiedAt != "" {
		return nil, ErrAlreadyCertified
	}

	result := make([]ReleaseEntry, len(entries))
	copy(result, entries)
	result[idx].Prerelease = false
	result[idx].CertifiedAt = certifiedAt
	return result, nil
}

// Yank marks the entry for semver as yanked with the supplied reason. Returns a
// new slice and an error if the transition is invalid.
//
// Error cases:
//   - ErrSemverNotFound  — semver not in ledger
//   - ErrYankReasonEmpty — reason is empty
//   - ErrNotCertified    — entry is still in pre-release state
//   - ErrAlreadyYanked   — entry is already yanked
//
// Spec ref: specs/release-pipeline.md §7.1.
func Yank(entries []ReleaseEntry, semver, reason string) ([]ReleaseEntry, error) {
	if reason == "" {
		return nil, ErrYankReasonEmpty
	}
	idx := indexBySemver(entries, semver)
	if idx < 0 {
		return nil, ErrSemverNotFound
	}
	e := entries[idx]
	if e.Prerelease || e.CertifiedAt == "" {
		return nil, ErrNotCertified
	}
	if e.Yanked {
		return nil, ErrAlreadyYanked
	}

	result := make([]ReleaseEntry, len(entries))
	copy(result, entries)
	result[idx].Yanked = true
	result[idx].YankedReason = reason
	return result, nil
}

// CurrentStable returns a pointer to the single entry with Prerelease=false and
// Yanked=false, or nil if no such entry exists.
//
// Spec ref: specs/release-pipeline.md §4.3 invariant 2 — "at most one current stable".
func CurrentStable(entries []ReleaseEntry) *ReleaseEntry {
	for i := range entries {
		if !entries[i].Prerelease && !entries[i].Yanked && entries[i].CertifiedAt != "" {
			e := entries[i]
			return &e
		}
	}
	return nil
}

// indexBySemver returns the index of the first entry matching semver, or -1.
func indexBySemver(entries []ReleaseEntry, semver string) int {
	for i := range entries {
		if entries[i].Semver == semver {
			return i
		}
	}
	return -1
}
