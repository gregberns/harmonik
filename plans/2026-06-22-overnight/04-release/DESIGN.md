# C4 — Cut a known-good release — DESIGN + VERDICT

**Date:** 2026-06-22 (overnight)
**Author:** Phase-1 design agent (cluster C4)
**Status:** DESIGN-ONLY. No tag pushed, no release created. Recommendation below.

---

## TL;DR / VERDICT

**CAN-CUT-TONIGHT — for a lightweight annotated git tag on the known-good commit.**

The full goreleaser CI pipeline is built but is gated on a *signed* tag + GitHub Actions secrets, and it would publish a 16-binary GitHub release with N-1 compat implications — that part is **NEEDS-MORNING-SIGN-OFF** (it needs the operator's GPG signing key and a deliberate version-line decision). But a *plain annotated tag* on the proven commit is cheap, low-risk, fully reversible, and is exactly what "mark the build we know works" means. Recommend cutting `v0.1.1` as an annotated tag tonight; defer pushing it to origin (which fires the publish pipeline) to the morning.

- **Known-good SHA:** `79a3b0ce4a6f094fafd2b5a1507f7888fcbec0fd` (origin/main HEAD)
- **Does it build:** YES — `go build ./...` exits 0; versioned `--version` smoke prints the spec-correct format.
- **Can cut tonight:** YES for a local/lightweight annotated tag (reversible with `git tag -d`); NO for the full signed-tag-push → goreleaser GitHub release without operator sign-off.

---

## 1. Current release mechanism — what exists vs what's missing

The release pipeline epic **hk-brc3z** ("release-pipeline lane — create/validate/certify/rollback") is **fully landed** — all 8 child beads closed and on `main` (verified by crew chani 2026-06-16; epic itself is the only thing left OPEN, awaiting a terminal close). What exists is a real, spec-backed, tag-triggered pipeline.

### What EXISTS (file-grounded)

| Artifact | Path | What it does |
|----------|------|--------------|
| Normative spec | `specs/release-pipeline.md` | 4-stage contract: CREATE → VALIDATE → CERTIFY → ROLLBACK. Semver rules (§2), tag format `v[0-9]+\.[0-9]+\.[0-9]+` (§2.2), ldflags stamp contract (§2.3). |
| Version stamp | `cmd/harmonik/version.go:34,44` | `var commitHash = "unknown"` and `var version = "dev"` — ldflags injection targets. Runtime VCS fallback via `runtime/debug.ReadBuildInfo()`. |
| `--version` printer | `cmd/harmonik/main.go:138-139` | Prints `harmonik %s (commit: %s)` — matches spec §2.3 format exactly. |
| goreleaser config | `.goreleaser.yaml` | v2; builds 4 binaries × 4 platforms (linux/darwin × amd64/arm64) = 16 artifacts; injects `-X main.commitHash={{.Commit}} -X main.version={{.Version}}`; `checksums.txt`; `release.prerelease: auto`. |
| CI workflow | `.github/workflows/release.yml` | Trigger: push of a tag matching `v[0-9]+.[0-9]+.[0-9]+`. Job `create` = goreleaser; job `validate` = `make release-validate`, and **deletes the pre-release on gate failure** (`gh release delete "$TAG" --yes --cleanup-tag`). |
| VALIDATE gate | `Makefile:249-256` (`release-validate`) | `build-all` → `make lint` → `go test -short -race` → scenario suite (`-tags=scenario`) → `/tmp/harmonik --version` smoke. |
| Release CLI | `cmd/harmonik/release_cmd.go` | `harmonik release ledger | certify <semver> | yank <semver> --reason | rollback [--bin]`. Operates on a JSON ledger at `<project>/.harmonik/release-ledger.json` (no daemon needed). |
| Ledger / last-good / rollback | `internal/release/{ledger,ledger_file,lastgood,manifest}.go` | `ReleaseEntry` schema, certify/yank state machine, last-good-binary pin at `<project>/.harmonik/state/last-good-binary`, rollback restore. |
| Changelog | `CHANGELOG.md` | As-built pipeline documented. |
| Prior tags | `git tag` | `v0.1.0` (annotated, "first pre-release", 2026-06-09, signer Greg Berns) and `daemon-20260610-01` (an ad-hoc deploy marker). |

### What's MISSING / gating the *full* publish

1. **Signed tags + GPG key.** Spec §2.2 and §3 step 1 require a *signed* tag (`git tag -s`) and CI verifies the signer is in the trusted-committer list. The existing `v0.1.0` is annotated but the spec wants signed for the real pipeline. Signing needs the operator's GPG key — not something to do unattended.
2. **GitHub Actions secrets / token scope.** The pipeline runs in CI on `secrets.GITHUB_TOKEN`. Recall from the epic history: the daemon's OAuth token lacked the `workflow` scope and blocked `.github/workflows/` edits — pushing a tag that triggers a 16-binary publish is an operator-owned, network-visible action.
3. **CERTIFY is CI-driven.** Per spec §6, CERTIFY runs *in CI* after VALIDATE passes (flips `prerelease:false`, commits the ledger). Locally, `harmonik release certify <semver>` exists but the canonical certify path is the workflow.
4. **Version-line decision.** Spec §2.1: `z` bump = additive/bugfix, `y` bump = any breaking change. Picking the number is a human call (is anything since v0.1.0 a wire/CLI/contract break?). For a pure bugfix-and-reliability release, `v0.1.1` is the conservative, defensible pick — but it's the operator's to confirm.

---

## 2. How harmonik is built + deployed TODAY (and what a "release" adds)

**Today's deploy is NOT a release.** Per the harmonik-lifecycle skill and the supervisor: deploy = `go install ./cmd/harmonik` (+ twins) → `pkill harmonik` → the supervisor (`harmonik supervise`) auto-revives the daemon from the newly-installed binary after a 30s–1m socket-bind delay. The running commit is observable via the `daemon_started` event's `binary_commit_hash`. Tags like `daemon-20260610-01` are informal deploy markers, not pipeline releases.

**What a "release" adds on top of `go install`:**

- A **durable, named, signed marker** (`v0.1.1`) pinning the exact known-good SHA — so "the build we know works" has a stable handle instead of a 40-char SHA.
- **Versioned binary artifacts** (goreleaser → 16 cross-platform binaries + `checksums.txt` attached to a GitHub release) so any box can fetch the exact binary, not rebuild from source.
- A **last-good / rollback contract**: the supervisor pins the certified binary and can restore it; `harmonik release rollback` is the operator escape hatch (`internal/release/lastgood.go`, `release_cmd.go:312`).
- A **certified-vs-prerelease state** in the ledger so the supervisor refuses yanked binaries (spec §7.2, self-check exit code 9).

For tonight's intent — "mark a build we trust" — the **marker** is the load-bearing part. The artifact-publish + certify are nice-to-have and can follow in the morning.

---

## 3. The KNOWN-GOOD commit + evidence

**Recommended SHA: `79a3b0ce4a6f094fafd2b5a1507f7888fcbec0fd`** (current origin/main HEAD; `79a3b0ce`).

Evidence it's good:

1. **Builds clean.** `go build ./...` → exit 0 (run this session).
2. **Version smoke passes, spec-correct format.** Built with the release ldflags:
   ```
   harmonik v0.1.0-test (commit: 79a3b0ce4a6f094fafd2b5a1507f7888fcbec0fd)
   ```
   This is exactly the spec §2.3 format `harmonik v<semver> (commit: <sha>)`.
3. **Proven-in-prod fixes are ancestors of HEAD** (`git merge-base --is-ancestor`, both confirmed):
   - `1c84fd1f` — `fix(daemon): deliver actionable feedback on DOT implementer-resume back-edges` (hk-wixms). The brief notes this is PROVEN in prod (3 beads landed clean).
   - `f6b76f59` — `fix(supervise): detect keeper-loop liveness when supervisor.pid absent` (hk-yrnui), the supervisor cry-wolf fix.
4. **No newer commits.** Local HEAD == origin/main HEAD == `79a3b0ce`; nothing unpushed.

> NOTE on the alternative — tagging `1c84fd1f` directly. The brief calls `1c84fd1f` the proven commit, but six clean commits sit on top of it (rc-prefix migrate, keeper-doctor wiring, format, remote-substrate retry, regression tests). All are low-risk fixes/tests already on origin/main. Tagging HEAD (`79a3b0ce`) captures the proven substrate **plus** those fixes and is the simpler story ("tag what's on main"). If the operator wants a strictly-minimal proven cut, `1c84fd1f` is the floor. Recommendation: tag HEAD.

### Test caveat (do not block on this)

The full `go test ./...` was NOT run to completion this session (the heavy scenario/daemon suite is long and the daemon may be live). The release VALIDATE gate covers it (`make release-validate` runs `go test -short -race` + the scenario suite). One **known-flaky** test to expect if you run locally: `TestWorkflowModeDot_ValidRefPassesFlagParse` in `cmd/harmonik` fails with "not a terminal" in a non-TTY shell — pre-existing/environmental, not a regression (recorded in project memory). For a CAN-CUT-TONIGHT *annotated tag* this is moot (a local tag runs no gate); for the full publish, CI runs the gate in its own environment.

---

## 4. What "CERTIFY" means here

From `specs/release-pipeline.md`:

- **VALIDATE (§5)** is the acceptance/smoke gate: CI Tier-2 (`make check-full` per spec; the as-built `make release-validate` does lint + `go test -short -race` + scenario suite), the scenario suite, and the `--version` smoke (downloaded binary must print the correct semver+sha). Any failure auto-yanks the pre-release.
- **CERTIFY (§6)** is the promotion step that runs *after* VALIDATE passes: flip `Prerelease:false` + stamp `CertifiedAt` in the ledger, flip the GitHub release `prerelease:false`, commit the ledger to main, emit a `release_certified` event. It is idempotent (no-op if already certified).

So "certify" = "VALIDATE gate passed, this is now the stable release." Locally that is `harmonik release certify v0.1.1`; canonically it is the CI job. **A release MUST NOT be treated as stable until CERTIFY completes** (spec §1). For tonight, an annotated tag is *not* a certified release — it's a marker. That's fine and is the safe scope.

---

## 5. Cut-the-release procedure

### 5A. TONIGHT (recommended) — lightweight/annotated tag, local-only, reversible

This marks the known-good build without firing the publish pipeline (push deferred). Fully reversible.

```bash
cd /Users/gb/github/harmonik

# 1. Confirm clean known-good state (no unpushed commits, builds).
git fetch origin && git log --oneline -1 origin/main      # expect 79a3b0ce
go build ./...                                             # expect exit 0

# 2. Create an ANNOTATED tag on the known-good SHA (NOT pushed yet).
git tag -a v0.1.1 79a3b0ce4a6f094fafd2b5a1507f7888fcbec0fd \
  -m "v0.1.1 — known-good: wixms DOT-cascade fix (1c84fd1f, proven) + supervisor cry-wolf fix (f6b76f59)"

# 3. Verify it.
git tag -l v0.1.1
git show v0.1.1 --stat | head -20
```

**Rollback (if anything looks wrong):**
```bash
git tag -d v0.1.1            # delete the local tag — instant, no trace
```

This is the CAN-CUT-TONIGHT scope: a local annotated tag is a pure metadata add, costs nothing, and `git tag -d` fully undoes it.

### 5B. MORNING (operator sign-off) — signed tag → publish + certify

When the operator is back to sign and decide the version line:

```bash
# Re-cut as a SIGNED tag (spec §2.2 requires signed for the pipeline).
git tag -d v0.1.1                                          # remove the unsigned one
git tag -s v0.1.1 79a3b0ce -m "v0.1.1 — <release notes>"   # needs operator GPG key

# Push the tag → fires .github/workflows/release.yml (CREATE → VALIDATE).
git push origin v0.1.1

# CI runs goreleaser (16 binaries + checksums.txt) → pre-release,
# then `make release-validate`; on failure CI auto-deletes the pre-release.
# After VALIDATE passes, CERTIFY (CI or locally):
harmonik release certify v0.1.1
harmonik release ledger                                    # confirm "stable"
```

**Rollback of a published release:**
```bash
git push --delete origin v0.1.1      # remove remote tag
gh release delete v0.1.1 --yes       # remove GitHub release + artifacts
harmonik release yank v0.1.1 --reason "<why>"   # mark yanked in ledger (audit trail)
# supervisor then refuses the yanked binary; `harmonik release rollback` restores last-good.
```

### Tag scheme

Use **semver `vMAJOR.MINOR.PATCH`**, NOT CalVer — this is mandated by spec §2.1 ("No CalVer. Dates belong in release notes"). harmonik is pre-1.0 (`0.y.z`). Since v0.1.0 → HEAD is bugfix/reliability/test work (no wire-format, CLI-removal, or contract break identified), the conservative bump is the patch: **`v0.1.1`**. If the operator judges any change since v0.1.0 a breaking contract change, it's `v0.2.0` — that's the one judgment call to confirm at sign-off.

---

## 6. VERDICT (detailed)

| Scope | Verdict | Why |
|-------|---------|-----|
| **Local annotated tag `v0.1.1` on `79a3b0ce`** | **CAN-CUT-TONIGHT** | Build green, fixes proven, version smoke spec-correct. Pure metadata, reversible via `git tag -d`. Commands in §5A. |
| **Push tag → goreleaser GitHub release (16 binaries) + CERTIFY** | **NEEDS-MORNING-SIGN-OFF** | Needs (a) operator GPG key for the *signed* tag the pipeline requires, (b) confirmation of the version line (`v0.1.1` vs `v0.2.0`), (c) a deliberate, network-visible publish that's harder to fully unwind. Not an unattended move. Commands staged in §5B. |

**Recommended overnight action:** create the local annotated `v0.1.1` tag on `79a3b0ce` per §5A (or just leave a note and let the operator run it — it's one command). Do **not** push it. Leave §5B for the morning so the operator signs the tag and confirms the version bump.

---

## Appendix — commands run this session (evidence)

- `git log --oneline -20`, `git tag` → HEAD `79a3b0ce`; tags `v0.1.0`, `daemon-20260610-01`.
- `go build ./...` → exit 0.
- `go build -ldflags "-X main.commitHash=$(git rev-parse HEAD) -X main.version=v0.1.0-test" -o /tmp/harmonik-vtest ./cmd/harmonik && /tmp/harmonik-vtest --version`
  → `harmonik v0.1.0-test (commit: 79a3b0ce4a6f094fafd2b5a1507f7888fcbec0fd)` (spec §2.3 format ✓).
- `git merge-base --is-ancestor 1c84fd1f HEAD` → ancestor ✓ (wixms DOT fix).
- `git merge-base --is-ancestor f6b76f59 HEAD` → ancestor ✓ (supervisor cry-wolf fix).
- `which goreleaser` → `/Users/gb/go/bin/goreleaser` (installed).
- `br show hk-brc3z` → all 8 children closed; epic OPEN (awaiting terminal close).
