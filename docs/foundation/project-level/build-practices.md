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

## Versioning

**Semver, pre-1.0.** Current line is `0.y.z`.
- `z` bumps for any additive or bug-fix release.
- `y` bumps on any breaking change (wire format, CLI flag removal, spec-level contract change).
- `0` → `1.0.0` only when: foundation spec stable ≥30 days, bootstrap workflow runs end-to-end in scenario tests, N-1 compat contract honored for ≥2 prior releases.
- No CalVer; date lives in the release notes, not the version string.

## Release process

Triggered by pushing a signed tag `v0.y.z` to main. No "merge all PRs" step — main is the working branch and is always the release candidate.

1. Ensure `main` is green (latest `make check-full` pass; CI clean on latest commit).
2. Update `CHANGELOG.md` (keep-a-changelog format; sections: Added / Changed / Deprecated / Fixed / Removed / Security / Spec).
3. Tag: `git tag -s v0.y.z -m "v0.y.z"` and push.
4. CI runs `goreleaser release --clean`, building `linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64` binaries for every harmonik binary (daemon, handlers compiled-in, CLI); publishes GitHub release with checksums.
5. Changelog entry mirrored into the GitHub release body.

No binary signing pre-1.0 (parallel to MVH commit-hash decision in operator-nfr §7.2). Distribution is GitHub releases only until a user asks for Homebrew / apt / etc.

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
