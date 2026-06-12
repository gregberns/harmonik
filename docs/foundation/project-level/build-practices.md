# Go Build Practices (for building harmonik itself)

> **Scope clarification.** These practices govern work on the harmonik codebase itself — the human + agents collaborating to write the Go daemon, handlers, CLI, and tests. They do **NOT** govern the workflow-run commit pattern that harmonik produces at runtime (three-level branching, checkpoint commits with `Harmonik-Run-ID` / `Harmonik-State-ID` / `Harmonik-Transition-ID` / `Harmonik-Bead-ID` / `Harmonik-Schema-Version` trailers). That pattern is specified in components.md §2.1 (checkpoint format) and §5.8 (branching). If a commit lands in this repo without a `Harmonik-Run-ID` trailer, it is a project-level commit and this document applies.

**2026-04-24 user direction:** MVH and post-MVH until real users adopt the product, harmonik uses **direct-to-main** development with **agent reviewers on every commit**. No pull requests. User reads committed code asynchronously, never gates it. This doc is revised accordingly; the PR-based shape it previously described is gone.

## Decisions

1. **Conventional Commits** for message format, with a small fixed type set.
2. **Direct commits to `main`.** `main` is the working branch. Ephemeral `agent/<codename>` branches allowed only for work spanning sessions that needs to park; squash-merge back to main same session on resume.
3. **Agent reviewer on every commit** (required, not optional). Every non-trivial commit carries a `Reviewed-By:` trailer recording the `agent-reviewer` verdict.
4. **No pull requests.** PR-based workflow returns when real users adopt the product or multiple human contributors join.
5. **Self + agent review** — user reads committed code async, catches what agents miss. Never a merge gate.
6. **Semver `0.y.z`** pre-1.0; breaking changes bump `y`, everything else bumps `z`; 1.0 only after foundation complete + full bootstrap workflow runs end-to-end.
7. **Tag-triggered releases** via `git tag v0.y.z` → GitHub release + `goreleaser` binary matrix.

## Commit conventions

Format: `<type>(<scope>): <subject>` — body optional, trailers optional.

Types (closed set): `feat`, `fix`, `refactor`, `test`, `docs`, `chore`, `spec`, `build`, `perf`.

Scopes (prefer, not required): subsystem ID (`s01`, `s04`) or top-level package (`daemon`, `handler`, `workspace`, `cli`).

Subject: imperative, ≤72 chars, no trailing period. Body wraps at 100.

Required trailers when applicable: `Refs: <bead-id>` or `Refs: <kerf-codename>` when the commit advances a tracked work item; `Co-Authored-By:` for agent-assisted commits; `BREAKING CHANGE: <what>` footer for any incompatible change.

**Additional required trailers (non-trivial commits):** two trailers record the `agent-reviewer` outcome:

- `Reviewed-By: agent-reviewer` (presence-only marker; names the reviewer skill that ran).
- `Review-Verdict: {"verdict": "APPROVE|REQUEST_CHANGES", "flags": [...], "notes": "..."}` — a structured JSON trailer emitted by `agent-reviewer`. JSON schema versioned via `schema_version` field inside the object. `flags[]` is a list of issue tags (e.g., `spec-divergence`, `missing-tests`, `unwanted-abstraction`); `notes` is free text for human consumption.

The JSON trailer is schema-validated in the pre-commit hook; an unparseable JSON trailer blocks the commit. Prevents prompt-injection that would pass a free-text verdict.

Trivial commits (typo, whitespace, obvious one-line fix) MAY omit these trailers. `BLOCK` verdicts never land in commits — the agent fixes first.

**`Trivial: true` bypass trailer.** To opt a single commit out of the `Reviewed-By:` / `Review-Verdict:` requirement, add the trailer `Trivial: true` anywhere in the commit message's trailer block (after the blank line separating the body from trailers). The pre-commit hook (`scripts/validate-commit-msg.sh`) detects this trailer and skips the reviewer-trailer check. Use ONLY for: typo fixes, whitespace normalization, obvious one-line corrections, and test-infrastructure trivial changes. The `make check-full` requirement still applies — `Trivial: true` does not bypass linting or tests, only the agent-reviewer trailer.

Forbidden: emoji, "WIP" subjects on main, single-word subjects, messages that describe the diff instead of the intent.

Examples: `feat(s04): add claude-twin handler adapter` — `fix(workspace): honor run_id in worktree path` — `spec(handler-contract): narrow skill-injection failure to fail-launch`.

## Branch model — direct-to-main

**`main` is the working branch.** Agents commit directly. The previous trunk-based + feature-branch shape is gone for MVH.

- **Ephemeral `agent/<codename>` branches allowed ONLY** for parked work that spans sessions and cannot cleanly land on main mid-way. Squash-merged to main on resume within the same session.
- **No `user/<topic>` or long-lived `agent/<codename>` branches.** Direct-to-main is the norm; branching is the rare exception for session-straddling parks.
- **No release branches** pre-1.0. Tags point at main. If a hot-fix on an older release is ever needed post-1.0, spin a `release/0.y` branch at that point — not preemptively.
- **Integration branch (`harmonik/integration`) is runtime-only** — it is how harmonik's workflow engine merges run-branches at runtime (workspace-model §5.8). It is NOT a branch for building harmonik itself. Do not confuse the two.

## Commit standards for direct-to-main

**Size.** Target roughly one logical change per commit. No hard LOC cap: commits are naturally smaller units of work than PRs, so the 1,000-LOC PR ceiling does not translate. Monitor commit size in practice; add a cap if agents produce megacommits.

**Commit body template** (agents fill this verbatim for non-trivial commits — same information previously required in PR bodies, now in the commit body):
```
## Why
<1–3 sentences: the problem this solves, linking to the kerf work or bead>

## What
<bulleted list of concrete changes, grouped by package>

## Spec alignment
<which specs/*.md files this commit implements or updates; "N/A" if tooling-only>

## Test plan
<checklist of layers exercised: unit / integration / scenario / twin / manual>

## Risk
<what could break; who/what this blocks on>
```

**Required before every non-trivial commit:** `make check-full` passes locally (per `quality-checks.md §Three-tier identical gauntlet`) AND `agent-reviewer` ran with a non-`BLOCK` verdict. The verdict is recorded as the `Reviewed-By:` trailer.

## Agent review on every commit

Before an agent commits, it runs:

1. **`make check-full`** — Tier 3 gauntlet per `quality-checks.md`. Must pass.
2. **`agent-reviewer` skill invocation** — against the agent's own work product (diff from the last `main` tip).

The `agent-reviewer` checks:
- **Spec alignment** — does the diff match `specs/*.md`? Any silent divergence?
- **Idiom compliance** — idiomatic Go, error wrapping at boundaries, context propagation, ZFC tags on new cross-subsystem interfaces.
- **Test adequacy** — per `docs/methodology/TESTING.md` layer expectations for the change scope.
- **Unwanted-abstraction detection** — "did you add an abstraction the user didn't ask for?" (per CLAUDE.md).
- **Bead / codename match** — "does the diff match the bead or kerf codename it claims to implement?"

Reviewer emits a structured JSON verdict in the `Review-Verdict:` trailer. Three `verdict` enum values: `APPROVE`, `REQUEST_CHANGES`, `BLOCK`. `BLOCK` verdicts are never committed (agent fixes before committing). `REQUEST_CHANGES` verdicts may be committed WITH the trailer + a rationale in the commit body; `flags` array names the specific issues. `APPROVE` verdicts commit normally.

**Trivial commits** (typo, whitespace, one-line obvious fix) MAY skip `agent-reviewer`; they still run `make check-full`.

**Human review happens asynchronously after commit.** The user reads `git log` + diffs on their own cadence; catches what agents miss (premature abstraction, scope creep, subtle spec-misinterpretation, "technically works but wrong"). Human review is NOT a gate — nothing blocks on it.

## Bug fixes require a reproducing scenario test

**2026-05-21 user direction.** Several runtime bugs in the 2026-05-21 dogfood session (hk-37zy8, hk-yjduq, hk-2hb2y, hk-5s7tg, hk-trjef) passed unit tests and reviewer agents but failed in live runs. Unit-level coverage is insufficient for behavior that only manifests across subsystem boundaries or under real process lifecycle. New rule:

**Trigger.** Any bead labeled `bug`, OR any bead filed in response to a runtime failure observed in dogfooding (regardless of label).

**Requirement.** The fix-commit MUST land alongside a scenario test (per `docs/methodology/TESTING.md` scenario tier) that reproduces the bug. The scenario test SHOULD be written first and committed in a failing state; the fix flips it green. Landing both in a single commit is acceptable when the bisect-history value is low — document the choice in the commit body under `## Test plan`.

**Exemptions.** Trivial typo/docs fixes; fixes whose reproduction requires an irreproducible environment (flaky third-party service, race only observed on a since-retired machine). Exemption MUST be named explicitly in the commit body under `## Risk` with the phrase `scenario-test exempt: <reason>`.

**Reviewer check.** `agent-reviewer` MUST verify the bug bead has a corresponding scenario test in the diff (new file under `tests/scenarios/` or new `Test*` function in an existing scenario file). If absent and no exemption is claimed → `REQUEST_CHANGES` with flag `missing-scenario-test`. If the exemption clause is present but unjustified (e.g. bug is reproducible from the bead description) → `BLOCK` with the same flag.

Cross-refs: `docs/methodology/TESTING.md` (scenario-tier definition); `.claude/skills/agent-reviewer/SKILL.md §Flag vocabulary` (flag registration); `CLAUDE.md §Daily loop` (dogfood is the bug-discovery channel).

## Versioning

**Semver, pre-1.0.** Current line is `0.y.z`.
- `z` bumps for any additive or bug-fix release.
- `y` bumps on any breaking change (wire format, CLI flag removal, spec-level contract change).
- `0` → `1.0.0` only when: foundation spec stable ≥30 days, bootstrap workflow runs end-to-end in scenario tests, N-1 compat contract honored for ≥2 prior releases.
- No CalVer; date lives in the release notes, not the version string.

## Release process

> **Normative spec:** `specs/release-pipeline.md`. This section is the practitioner reference; the spec wins on any conflict.
>
> **Current state (as of v0.1.0, 2026-06-10):** goreleaser CI workflow is not yet wired. v0.1.0 was cut by hand (see manual escape hatch below and `docs/known-workarounds.md §Manual release escape hatch`). Gaps are tracked in `specs/release-pipeline.md §9` and the `codename:release-pipeline` bead lane.

The pipeline has four stages triggered by pushing a signed semver tag to `main`:

```
CREATE → VALIDATE → CERTIFY → [ROLLBACK if needed]
```

No "merge all PRs" step — `main` is the working branch and is always the release candidate.

### Pre-release checklist

Before cutting a tag:

1. Ensure `main` is green: `make check-full` passes, CI clean on latest commit.
2. Update `CHANGELOG.md` (keep-a-changelog format; sections: Added / Changed / Deprecated / Fixed / Removed / Security / Spec).
3. Verify `internal/release/manifest.go` `BeadsVersion` matches the `br --version` of the tested environment.

### Stage 1 — CREATE

```bash
git tag -s v0.y.z -m "v0.y.z"
git push origin v0.y.z
```

CI runs `goreleaser release --clean` using `.goreleaser.yaml` at the repo root. goreleaser:

- Builds the binary matrix: `linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`.
- Injects ldflags: `-X main.commitHash=$(git rev-parse HEAD) -X main.version=$(git describe --tags --exact-match)`.
- Produces `harmonik_<os>_<arch>` binaries + `checksums.txt` (SHA-256 per binary).
- Creates a GitHub **pre-release** with all artifacts attached.
- Mirrors the `CHANGELOG.md` entry for this version into the GitHub release body.
- Writes a ledger entry to `internal/release/manifest.go` with `Prerelease: true`.

`harmonik --version` output after this stage: `harmonik v0.y.z (commit: <sha>)`.

### Stage 2 — VALIDATE

All three gates run in parallel; any failure yanks the pre-release automatically:

| Gate | Command | Pass criterion |
|------|---------|----------------|
| CI Tier 2 | `make check-full` on the tagged commit | Exit 0 |
| Scenario tests | `go test -tags=scenario ./tests/scenarios/...` | Exit 0, zero failures |
| `--version` smoke | Download published binary, run `harmonik --version` | Matches `harmonik v0.y.z (commit: <sha>)` |

On failure: CI deletes the GitHub pre-release and discards the pending ledger entry. A new tag must be pushed to re-run.

### Stage 3 — CERTIFY

When all VALIDATE gates pass, CI:

1. Updates the ledger entry: `Prerelease: false`, `CertifiedAt` = current RFC3339 timestamp.
2. Flips the GitHub release to non-pre-release via the GitHub API.
3. Commits `internal/release/manifest.go` to `main`: `chore(release): certify v0.y.z\n\nRefs: hk-brc3z\nTrivial: true`.

The release is now the current stable version. The supervisor may adopt this binary as the last-good binary.

### Stage 4 — ROLLBACK

A certified release may be yanked by a human operator (not automatic). Procedure:

1. Set `Yanked: true`, `YankedReason: "<reason>"` in `internal/release/manifest.go`.
2. Commit and push to `main`.
3. Mark the GitHub release as pre-release again (or delete it).

The supervisor script (`scripts/hk-keeper.sh`) refuses to launch a binary whose commit hash matches a yanked ledger entry, and falls back to the last-good binary on crash within 30 s of start. Full protocol: `specs/release-pipeline.md §7`.

### Manual escape hatch (pre-goreleaser CI)

Until `.goreleaser.yaml` and the CI workflow exist, cut releases manually. See `docs/known-workarounds.md §Manual release escape hatch` for the procedure and what VALIDATE / CERTIFY steps must be performed by hand.

No binary signing pre-1.0. Distribution is GitHub releases only until a user asks for Homebrew / apt / etc.

## Git hygiene

- **Direct commits to `main`.** No rebase-feature-branch flow; there are no feature branches to rebase.
- **Never rewrite main history** after a tag.
- **Never `--force-push` main.** `--force-with-lease` allowed on ephemeral `agent/<codename>` park-branches only.
- **`--no-verify` forbidden.** Pre-commit hook failures require fixing the underlying issue, not bypassing the hook.
- **Signed commits (`git commit -S`)** — nice-to-have, not required pre-MVH; revisit when the product gets real users.
- **`.gitignore`** must cover: `/bin/`, `/dist/`, `.harmonik/` (runtime state), `*.test`, `coverage.out`, `.kerf/` (gitignored per CLAUDE.md).

## ⚑ Assumptions worth user's eye

1. **⚑ Direct-to-main with pre-commit + agent-reviewer-every-commit is the failure-tolerance model.** Trade-off: speed up front, accept possibility that a bad commit reaches main and needs fix-forward. For agent-coded solo dev: probably the right trade. Revisit when the product has real users or a multi-human team.
2. **⚑ `agent-reviewer` skill is load-bearing and must not rot.** Agent-config Tier 2 cadence explicitly checks its currency. If the reviewer becomes a rubber stamp, the whole model collapses.
3. **⚑ Reviewer verdict as commit trailer.** `Reviewed-By: agent-reviewer` (presence marker) + `Review-Verdict:` (structured JSON) makes review outcome auditable via `git log`. `BLOCK` verdicts by definition never land.
4. **⚑ JSON-structured `agent-reviewer` verdict** (not prose). Prevents prompt-injection; enables audit/metrics. Schema lives in the `agent-reviewer` skill's documentation and is versioned; agents MUST use the current schema.
5. **⚑ No hard LOC cap on commits.** Commits are naturally smaller than PRs; an explicit ceiling isn't needed. Monitor commit size; add a cap if agents produce megacommits.
6. **⚑ `spec:` commit type** — non-standard within Conventional Commits; added because spec work dominates early. Alternative: fold into `docs:` or `chore:`.
7. **⚑ Post-commit CI on main** — re-runs `make check-full`. Failures require fix-forward; no gate that rejects the push. Flag for user: is a remote CI set up (GitHub Actions assumed but not confirmed)?

## Deferred / follow-up

- **PR-based workflow, code review, branch protection** — restored when the product has real users or multiple human contributors. Currently direct-to-main with agent review.
- **CI provider choice** — GitHub Actions assumed; not pinned.
- **Coverage thresholds** — per-package minimums to block merge; wait until testing methodology settles.
- **Dependency update policy** — dependabot cadence, pinning vs. floating.
- **Security disclosure process** — not a concern pre-1.0; revisit before public announcement.
- **Release-note automation** — changelog is hand-authored now; consider `git-cliff` once commit-message discipline is proven.
