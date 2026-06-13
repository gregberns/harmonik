# Release Pipeline Specification

```yaml
---
title: Release Pipeline Specification
spec-id: release-pipeline
requirement-prefix: RP
status: draft
spec-category: foundation-cross-cutting
spec-shape: requirements-first
version: 0.1.0
spec-template-version: 1.1
owner: foundation-author
last-updated: 2026-06-10
depends-on:
  - architecture
  - operator-nfr
---
```

> **Status:** normative — 2026-06-10
> **Bead:** hk-81h4d (spec anchor)
> **Epic:** hk-brc3z (label `codename:release-pipeline`)
>
> This document is the normative contract for harmonik's 4-stage release pipeline.
> Implementing beads (2–8 of the release-pipeline epic) MUST be written against this spec.
> If code contradicts this spec, the spec wins; open a bead to reconcile.

---

## 1. Overview

The release pipeline has four stages that execute in sequence after a signed semver tag is pushed to `main`:

```
CREATE → VALIDATE → CERTIFY → [ROLLBACK if needed]
```

- **CREATE** — goreleaser builds the binary matrix and creates a GitHub pre-release.
- **VALIDATE** — automated gates (CI Tier 2, scenario tests, `--version` smoke) must all pass; on failure the release is yanked before users see it.
- **CERTIFY** — a release-ledger entry is written flipping `prerelease: false`; the release becomes the current stable version.
- **ROLLBACK** — if a certified release is later yanked, the supervisor restores the last-good binary automatically.

The pipeline is tag-triggered. No manual "merge all PRs" step exists (main is always the candidate). A release MUST NOT become stable until CERTIFY completes.

---

## 2. Semver Rules

### 2.1 Version line

harmonik is pre-1.0. The current version line is `0.y.z`.

| Component | Bump rule |
|-----------|-----------|
| `z` | additive change, bug fix, documentation-only, spec-only release |
| `y` | any breaking change: wire-format change, CLI flag removal, spec-level contract change (e.g. new required event field), incompatible beads-integration change |
| `0 → 1` | foundation spec stable ≥30 days, bootstrap workflow runs end-to-end in scenario tests, N-1 compat contract honored for ≥2 prior releases |

No CalVer. Dates belong in release notes, not the version string.

### 2.2 Tag format

Tags MUST match `v[0-9]+\.[0-9]+\.[0-9]+` (e.g. `v0.2.0`). Signed tags are required:

```bash
git tag -s v0.y.z -m "v0.y.z"
git push origin v0.y.z
```

Pre-release tags (alpha/beta/rc) are NOT supported by this pipeline in the current iteration. The `prerelease` field in the ledger covers this semantically.

### 2.3 Binary version stamp (ldflags)

The binary self-reports its version via two linker-injected variables in `cmd/harmonik/version.go`:

| Variable | ldflags key | Default | Description |
|----------|-------------|---------|-------------|
| `commitHash` | `main.commitHash` | `"unknown"` | Full 40-char git SHA |
| `version` | `main.version` | `"dev"` | Semver string from the tag (`v0.y.z`) |

goreleaser MUST inject both. The `daemon_started` event currently carries only `binary_commit_hash` (refs: `specs/event-model.md §8.7.1`). Adding the `version` field to the `daemon_started` payload requires amending event-model.md §8.7.1; this is tracked as a gap in §9.

`harmonik --version` output format (normative):

```
harmonik v0.y.z (commit: <sha>)
```

Any other format is a spec violation.

### 2.4 Pre-release semantics

A binary is in **pre-release** state from CREATE through VALIDATE. During pre-release:
- The GitHub release is marked as a pre-release (`prerelease: true` in the GitHub API).
- The ledger entry exists with `prerelease: true`.
- Supervisor MUST NOT adopt a pre-release binary as the last-good binary (see §5).

A binary becomes **stable** when CERTIFY flips `prerelease: false` in both the ledger and the GitHub release.

---

## 3. Stage 1 — CREATE

**Trigger:** push of a signed tag matching `v[0-9]+\.[0-9]+\.[0-9]+` to `main`.

**Steps:**

1. CI verifies the tag signer is in the trusted-committer list (GPG or GitHub vigilant mode).
2. CI runs `goreleaser release --clean` using `.goreleaser.yaml` at repo root.
3. goreleaser builds the binary matrix: `linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`.
4. goreleaser injects ldflags: `-X main.commitHash=$(git rev-parse HEAD) -X main.version=$(git describe --tags --exact-match)`.
5. goreleaser produces `harmonik_<os>_<arch>` binaries plus a `checksums.txt` (SHA-256 of each binary).
6. goreleaser creates a GitHub pre-release (`prerelease: true`) with all binaries and `checksums.txt` attached.
7. `CHANGELOG.md` entry for this version is mirrored into the GitHub release body.
8. A ledger entry is written to `internal/release/manifest.go` with `Prerelease: true` (see §4).

**Failure contract:** if any goreleaser step fails, the GitHub release MUST NOT be created (or MUST be immediately deleted if partially created). No ledger entry is written.

---

## 4. Release Ledger

### 4.1 Location

The release ledger lives in `internal/release/manifest.go`. This is the existing home (BI-024, `BeadsVersion` pin). The ledger schema is added alongside the existing constant.

### 4.2 Schema

```go
// ReleaseEntry records a single harmonik release in the ledger.
type ReleaseEntry struct {
    // Semver is the release version string, e.g. "v0.2.0".
    Semver string

    // CommitHash is the full 40-character git SHA of the tagged commit.
    CommitHash string

    // Tag is the git tag name, e.g. "v0.2.0".
    Tag string

    // Prerelease is true from CREATE through VALIDATE. CERTIFY flips it false.
    Prerelease bool

    // CertifiedAt is the RFC3339 timestamp when CERTIFY ran. Zero value means
    // not yet certified.
    CertifiedAt string // zero value: ""

    // Yanked is true if this release was withdrawn after certification.
    Yanked bool

    // YankedReason is a human-readable explanation of why the release was yanked.
    // MUST be non-empty whenever Yanked is true.
    YankedReason string

    // Artifacts holds per-binary checksums produced by goreleaser.
    Artifacts []ArtifactEntry
}

// ArtifactEntry records one binary artifact in the release.
type ArtifactEntry struct {
    // Name is the artifact filename, e.g. "harmonik_linux_amd64".
    Name string

    // OS is the GOOS value, e.g. "linux".
    OS string

    // Arch is the GOARCH value, e.g. "amd64".
    Arch string

    // SHA256 is the lowercase hex SHA-256 checksum of the artifact binary.
    SHA256 string
}
```

### 4.3 Ledger invariants

1. **Append-only.** Entries are never deleted from the ledger. Yanked releases remain with `Yanked: true`.
2. **At most one current stable.** At any point, at most one entry may have `Prerelease: false` AND `Yanked: false`. This is the "current stable" release.
3. **CommitHash immutable.** Once an entry is written, `CommitHash` and `Semver` MUST NOT change.
4. **CertifiedAt monotone.** Entries are ordered by `CertifiedAt`. A new entry MUST have a `CertifiedAt` strictly greater than all prior certified entries.

### 4.4 Ledger storage

For pre-1.0, the ledger is a Go slice literal in `internal/release/manifest.go`, updated by the CI CERTIFY step via a code-generation script. The ledger is checked into source control; the CERTIFY commit records the certification event durably.

---

## 5. Stage 2 — VALIDATE

**Purpose:** prevent a defective binary from becoming stable. All gates MUST pass; any failure triggers an automatic yank (the pre-release is deleted from GitHub and the pending ledger entry is discarded).

**Gates (all required, in parallel where possible):**

| Gate | What it runs | Pass criterion |
|------|--------------|----------------|
| CI Tier 2 | `make check-full` (unit + integration) on the tagged commit | Exit 0 |
| Scenario tests | Full scenario suite (`go test -tags=scenario ./tests/scenarios/...`) | Exit 0, zero failures |
| `--version` smoke | Download the published binary, run `harmonik --version`, parse output | Matches `harmonik v<semver> (commit: <sha>)` with the correct semver and commit hash |

**Failure contract:**

- If any gate fails, CI MUST: (a) delete the GitHub pre-release, (b) NOT write or retain the ledger entry, (c) push a `release_validate_failed` event to `.harmonik/events/events.jsonl` with fields `{semver, tag, failed_gate, reason}`.
- A failed validation MUST NOT auto-retry. A new tag must be pushed to re-run the pipeline.

**Timing:** VALIDATE runs immediately after CREATE completes, without human intervention.

---

## 6. Stage 3 — CERTIFY

**Trigger:** all VALIDATE gates pass.

**Steps:**

1. CI updates the ledger entry for this semver: set `Prerelease: false`, set `CertifiedAt` to the current RFC3339 timestamp.
2. CI updates the GitHub release: flip `prerelease: false` via the GitHub API.
3. CI commits the updated `internal/release/manifest.go` to `main` with message: `chore(release): certify v0.y.z\n\nRefs: hk-brc3z\nTrivial: true`.
4. CI pushes a `release_certified` event to `.harmonik/events/events.jsonl` with fields `{semver, tag, commit_hash, certified_at}`.

**Post-CERTIFY state:**

- `Prerelease: false`, `Yanked: false` → current stable release.
- The supervisor may now adopt this binary as the last-good binary (see §5.2 of this document).
- Any prior certified release remains in the ledger as historical record (not yanked unless separately yanked).

**CERTIFY is idempotent:** running CERTIFY twice on the same semver MUST be a no-op (check `CertifiedAt != ""`  before writing).

---

## 7. Stage 4 — ROLLBACK

### 7.1 Yank

A certified release may be yanked by a human operator. Yank is NOT an automatic pipeline step; it requires an explicit action.

**Yank procedure:**

```bash
# Via a future `harmonik release yank` command (hk-brc3z implementing beads).
# Until that command exists, yank manually:
#   1. Set Yanked: true, YankedReason: "<reason>" in internal/release/manifest.go
#   2. Commit, push to main.
#   3. Mark the GitHub release as pre-release again (or delete it).
```

**Yank contract:**

- `Yanked: true` MUST be accompanied by a non-empty `YankedReason`.
- The supervisor MUST refuse to launch a yanked binary (see §7.2).
- A yanked entry is never deleted; it remains as an audit trail.

### 7.2 Supervisor last-good guard

The per-project supervisor — `harmonik supervise` (the in-binary supervisor-of-record per
[process-lifecycle.md §4.6 PL-019]; the `/tmp/hk-daemon-supervise.sh` artifact is retired from
the supported surface), or a de-hardcoded out-of-band shell fallback (`scripts/hk-supervise.sh`;
no hardcoded PROJECT or BIN; resolves PROJECT from `$HK_PROJECT` / argument / `git rev-parse`,
BIN from `command -v harmonik` with `$HOME/go/bin/harmonik` fallback, failing loudly if neither)
— MUST implement the last-good-binary protocol:

1. **On binary adoption:** before adopting a newly-installed binary as the last-good binary, the
   supervisor MUST query the release ledger. If the binary's `commitHash` matches an entry with
   `Yanked: true`, the supervisor MUST refuse adoption and log `refused_yank: <semver> <reason>`.
2. **Last-good tracking:** the supervisor persists the path to the last known-good binary in the
   per-project state file `<projectDir>/.harmonik/state/last-good-binary` (this replaces the
   former machine-global `/tmp/hk-last-good-binary` pre-1.0 path; the per-project path is the
   standing location for all releases, realizing the prior post-1.0 target). The last-good binary
   is updated only when a new binary is adopted successfully (daemon started and ran for ≥30s
   without crash). An absent state file on first read is a fresh start (no migration from the old
   `/tmp` path).
3. **On crash-restart:** if the current binary crashes within 30s of start, the supervisor falls
   back to the last-good binary. If the last-good binary is the same as the current binary (first
   install or unknown regression), the supervisor backs off exponentially and alerts the operator
   (via stderr) rather than spinning.
4. **Refuse-to-start for yanked binaries:** if the operator manually installs a binary whose
   commit hash appears in a yanked ledger entry, `harmonik` (the binary itself) MUST check the
   embedded ledger on startup and exit with code `9` (yanked-binary) and message `FATAL: this
   binary (v<semver>, <sha>) has been yanked: <reason>`. This self-check is belt-and-suspenders
   over the supervisor guard.

### 7.3 State machine

```
                  ┌──────────────────────────────────┐
                  │                                  │
    tag push   ┌──▼──────┐  VALIDATE pass  ┌────────┴─────┐
    ──────────►│ PRE-    │────────────────►│  CERTIFIED   │
               │ RELEASE │                 │ (stable)     │
               └──┬──────┘                └────────┬─────┘
                  │                                │
                  │ VALIDATE fail                  │ yank
                  ▼                                ▼
               [DISCARDED]                    ┌───────────┐
                                              │  YANKED   │
                                              │ (audit    │
                                              │  trail)   │
                                              └───────────┘
```

State transitions:

| From | To | Trigger | Ledger change |
|------|----|---------|---------------|
| (none) | PRE-RELEASE | CREATE completes | Entry written with `Prerelease: true` |
| PRE-RELEASE | CERTIFIED | All VALIDATE gates pass | `Prerelease: false`, `CertifiedAt` set |
| PRE-RELEASE | DISCARDED | Any VALIDATE gate fails | Entry NOT written (or discarded if written) |
| CERTIFIED | YANKED | Operator yank action | `Yanked: true`, `YankedReason` set |
| YANKED | CERTIFIED | (not supported — yank is irreversible) | — |

---

## 8. Events

The pipeline emits typed events to `.harmonik/events/events.jsonl`. All release-pipeline events share a common envelope:

```json
{
  "event_type": "<see below>",
  "timestamp": "<RFC3339>",
  "semver": "v0.y.z",
  "tag": "v0.y.z",
  "commit_hash": "<40-char sha>"
}
```

| `event_type` | Stage | Additional fields |
|---|---|---|
| `release_created` | CREATE | `artifacts: [{name, sha256}]` |
| `release_validate_started` | VALIDATE | `gates: ["ci_tier2", "scenario", "version_smoke"]` |
| `release_validate_gate_passed` | VALIDATE | `gate: <name>` |
| `release_validate_gate_failed` | VALIDATE | `gate: <name>`, `reason: <string>` |
| `release_validate_failed` | VALIDATE | `failed_gate: <name>`, `reason: <string>` |
| `release_certified` | CERTIFY | `certified_at: <RFC3339>` |
| `release_yanked` | ROLLBACK | `yanked_reason: <string>` |
| `release_refused_yank` | ROLLBACK | `reason: <string>` (supervisor refused to launch yanked binary) |

---

## 9. Gaps and deferred work

The following are known gaps as of 2026-06-10 (true state: v0.1.0 cut by hand, no goreleaser, no ledger, no rollback):

| Gap | Bead | Priority |
|-----|------|----------|
| `.goreleaser.yaml` does not exist | hk-brc3z lane | P1 |
| `cmd/harmonik/version.go` has `commitHash` only; `version` variable missing | hk-brc3z lane | P1 |
| `internal/release/manifest.go` has `BeadsVersion` only; `ReleaseEntry` schema not yet implemented | hk-brc3z lane | P1 |
| Supervisor has no last-good guard; relaunches `$BIN` blindly | hk-brc3z lane | P1 |
| No `CHANGELOG.md` | hk-brc3z lane | P2 |
| No CI workflow for tag-triggered release | hk-brc3z lane | P1 |
| `harmonik release yank` command not yet implemented | hk-brc3z lane | P2 |
| `daemon_started` event payload lacks `version` field; `specs/event-model.md §8.7.1` must be amended | hk-brc3z lane | P2 |

These gaps are the implementation surface for beads 2–8 of the release-pipeline epic. This spec is the anchor; each implementing bead MUST cite this file in its `## Spec alignment` commit-body section.

---

## 10. References

- `docs/foundation/project-level/build-practices.md §Versioning` — semver rules source
- `docs/foundation/project-level/build-practices.md §Release process` — intended pipeline (pre-spec)
- `specs/event-model.md §8.7.1` — `daemon_started` event payload (commitHash field)
- `internal/release/manifest.go` — BeadsVersion pin (BI-024), future ledger home
- `cmd/harmonik/version.go` — commitHash ldflags injection (hk-mz0x4)
- `specs/beads-integration.md §4.8` — BeadsVersion compat window (BI-024/BI-026)
