# Release Protocol — 2026-06-22 Reframe

**Date:** 2026-06-22
**Bead:** hk-6hkh8
**Status:** active

---

## 1. Model

The release model is **dogfooding-first, captain-certifies**. A tag push publishes a pre-release and
stops. The fleet runs it. The captain certifies when confident. There is no automated validation gate
and no auto-yank.

Pipeline:

```
tag push → CREATE (goreleaser → GitHub pre-release) → [dogfooding soak] → captain CERTIFY → stable
```

---

## 2. CERTIFY Readiness

**Readiness is dogfooding-soak confidence, not a CI VALIDATE gate pass.**

The captain triggers `harmonik release certify` when:

- The fleet has run the pre-release build on `main` for a meaningful soak period (typical cadence: ~50 commits of exposure, or several days of fleet operation — captain judgment).
- Ops-monitor signals are healthy: daemon stability, event bus throughput, keeper behavior, no unresolved regressions observed during soak.
- Any issues surfaced during soak have been triaged (either accepted as known/deferred, or fixed and incorporated into the release or a follow-up tag).

The cadence trigger (~50 commits) is a soft guideline. The actual gate is **captain judgment from
ops-monitor + soak observations**, not a commit count or CI pass/fail.

**What does NOT gate CERTIFY:**

- `make release-validate` — this is an optional local sanity check, not on the critical path.
- Any automated CI job — CREATE is the only CI step; CERTIFY is always a deliberate operator act.
- A fixed calendar duration — soak length scales with the weight of changes in the release.

---

## 3. Cadence

Releases follow the natural commit rhythm of the project:

- Main is always a release candidate (no "freeze" period).
- A tag is cut when the captain judges a meaningful milestone has accumulated (~50 commits is a
  guideline, not a rule).
- Pre-release soaks until `harmonik release certify` is issued.
- Multiple pre-releases may coexist; only the most recent one is the active soak candidate.

---

## 4. Failure during soak

If a defect is discovered during dogfooding:

1. The captain assesses severity. Minor issues: note in ops-monitor, continue soak.
2. Blocking defect: operator manually yanks the pre-release (see `specs/release-pipeline.md §7.1`).
3. A fix is merged to `main`; a new tag is cut; the new pre-release enters soak.
4. The yanked release remains in the ledger as an audit trail (`Yanked: true`).

There is no automated yank path. Yank is always a deliberate operator act.

---

## 5. Relationship to supervisor / last-good guard

Supervisor MUST NOT adopt a pre-release binary as the last-good binary (unchanged from prior
model). Once CERTIFY flips `Prerelease: false`, the supervisor may adopt the certified binary.
See `specs/release-pipeline.md §5` for supervisor last-good guard invariants.
