# Go Build Practices (for building harmonik itself)

> **Scope clarification.** These practices govern work on the harmonik codebase itself — the human + agents collaborating to write the Go daemon, handlers, CLI, and tests. They do **NOT** govern the workflow-run commit pattern that harmonik produces at runtime (three-level branching, checkpoint commits with `Harmonik-Run-ID` / `Harmonik-State-ID` / `Harmonik-Transition-ID` / `Harmonik-Bead-ID` / `Harmonik-Schema-Version` trailers). That pattern is specified in components.md §2.1 (checkpoint format) and §5.8 (branching). If a commit lands in this repo without a `Harmonik-Run-ID` trailer, it is a project-level commit and this document applies.

**2026-04-24 user direction:** Until real users adopt the product, harmonik uses **agent reviewers on every commit**. The user reads committed code asynchronously and never gates it.

**Superseded 2026-08-04 — the branch half of that direction is reversed.** The 2026-04-24 direction also said **direct-to-main**, and that is no longer the model. Work now lands on the integration branch, and a human moves the integration branch into `main`. The agent-reviewer-every-commit half stands unchanged. See §"Branch model — land on the integration branch".

## Decisions

1. **Conventional Commits** for message format, with a small fixed type set.
2. **Work lands on the integration branch, never on `main`.** See §"Branch model — land on the integration branch" below for the rule and for where the branch name is recorded.
3. **Agent reviewer on every commit** (required, not optional). Every non-trivial commit carries a `Reviewed-By:` trailer recording the `agent-reviewer` verdict.
4. **One pull request, at the integration→`main` boundary.** Work does not need a pull request to reach the integration branch. The agent reviewer is the gate there. A human opens the single pull request that moves the integration branch into `main`.
5. **Self + agent review** — user reads committed code async, catches what agents miss. Never a merge gate.
6. **Semver `0.y.z`** pre-1.0; breaking changes bump `y`, everything else bumps `z`; 1.0 only after foundation complete + full bootstrap workflow runs end-to-end.
7. **Tag-triggered releases** via `git tag v0.y.z` → GitHub release + `goreleaser` binary matrix.

## Fresh-clone bootstrap

After cloning, run one command to install the pinned dev tools:

```sh
make bootstrap      # == make tools
```

This installs gofumpt, gci, golangci-lint, and govulncheck into `.tools/` (no global GOPATH pollution).

**Git hooks are retired.** lefthook (and its self-re-arming `install`) was removed because it re-wired itself on every commit. Validation — the Tier 1/Tier 2 gates, secret scan, and commit-message trailers — now runs via the agent-driven validation command (`/check`), not a pre-commit / pre-push / commit-msg hook. The underlying scripts (`scripts/validate-commit-msg.sh`, `scripts/secret-scan.sh`) remain callable directly by that flow. **`--no-verify` is moot** (no hook to bypass); the way to comply is to run `/check` (`make fast`, then `make full`) after committing and fix + re-commit if it comes back red.

## Commit conventions

Format: `<type>(<scope>): <subject>` — body optional, trailers optional.

Types (closed set): `feat`, `fix`, `refactor`, `test`, `docs`, `chore`, `spec`, `build`, `perf`.

Scopes (prefer, not required): subsystem ID (`s01`, `s04`) or top-level package (`daemon`, `handler`, `workspace`, `cli`).

Subject: imperative, ≤72 chars, no trailing period. Body wraps at 100.

Required trailers when applicable: `Refs: <bead-id>` or `Refs: <kerf-codename>` when the commit advances a tracked work item; `Co-Authored-By:` for agent-assisted commits; `BREAKING CHANGE: <what>` footer for any incompatible change.

**Additional required trailers (non-trivial commits):** two trailers record the `agent-reviewer` outcome:

- `Reviewed-By: agent-reviewer` (presence-only marker; names the reviewer skill that ran).
- `Review-Verdict: {"verdict": "APPROVE|REQUEST_CHANGES", "flags": [...], "notes": "..."}` — a structured JSON trailer emitted by `agent-reviewer`. JSON schema versioned via `schema_version` field inside the object. `flags[]` is a list of issue tags (e.g., `spec-divergence`, `missing-tests`, `unwanted-abstraction`); `notes` is free text for human consumption.

The JSON trailer is schema-validated by `scripts/validate-commit-msg.sh` (run via the agent-driven `/check` flow, no longer a git hook); an unparseable JSON trailer fails validation. Prevents prompt-injection that would pass a free-text verdict. `schema_version`, `verdict` and `notes` are all required, and `notes` must not be empty — the same three the Go reader in `internal/workspace` requires of a verdict file.

**When no reviewer could be reached.** `AGENTS.md` (the `git commit -F` rule) says a commit whose reviewer could not be reached records that fact and lands anyway, and that such a trailer carries no verdict of `APPROVE`. The shape that records it:

```
Reviewed-By: none — no reviewer was reached for this commit
Review-Verdict: {"schema_version": 1, "verdict": "NOT_REVIEWED", "flags": ["no-reviewer-reached"], "notes": "<what was verified instead, and by whom>"}
```

`NOT_REVIEWED` is a verdict value the validator accepts. Say the absence in words: a `verdict` of `null` is refused, because it cannot be told apart from a trailer that was truncated.

**An approval is held to more than the other verdicts**, so that the honest form is always the cheaper thing to write. For `APPROVE` (and the config-reviewer's `CLEAN`), the validator also requires that `Reviewed-By:` names a reviewer skill this repo has — `agent-reviewer` or `agent-config-reviewer`, with an optional qualifier in parentheses such as `agent-reviewer (codex harness)` — that the value does not say "self", and that the JSON carries the `flags` key. This proves the name is a real reviewer. It cannot prove that reviewer ran, and no shell script reading a commit message ever could.

Trivial commits (typo, whitespace, obvious one-line fix) MAY omit these trailers. `BLOCK` verdicts never land in commits — the agent fixes first.

**`Trivial: true` bypass trailer.** To opt a single commit out of the `Reviewed-By:` / `Review-Verdict:` requirement, add the trailer `Trivial: true` anywhere in the commit message's trailer block (after the blank line separating the body from trailers). `scripts/validate-commit-msg.sh` (run via the agent-driven `/check` flow) detects this trailer and skips the reviewer-trailer check. Use ONLY for: typo fixes, whitespace normalization, obvious one-line corrections, and test-infrastructure trivial changes. The `make full` requirement still applies — `Trivial: true` does not bypass linting or tests, only the agent-reviewer trailer.

A commit message is read later by someone reconstructing why a line exists, usually from `git log` alone. Write for that reader: state the intent, not the diff, which the reader can already see. Four habits defeat that reader, so treat each as a signal to rewrite the subject — emoji, a "WIP" subject on the integration branch, a single-word subject, and a subject that restates the diff.

Examples: `feat(s04): add claude-twin handler adapter` — `fix(workspace): honor run_id in worktree path` — `spec(handler-contract): narrow skill-injection failure to fail-launch`.

## Branch model — land on the integration branch

**This section owns the rule. Other documents point here. They do not restate it.**

**Completed work lands on the integration branch.** It does not land on `main`. This replaces the
earlier direct-to-main model, which made `main` the working branch.

**The repo names the integration branch in `.harmonik/branching.yaml`, key `defaults.lands_on`.**
Read that file to learn the current name. Do not write the branch name into a doc or a script. The
same key is what `harmonik promote`, the daemon, and the smoke check all read, so the file is the one
place the name lives.

**`main` moves only by a deliberate human step.** A person opens a pull request from the integration
branch to `main` and merges it. No agent pushes `main`.

Open that pull request with both flags set:

```sh
harmonik promote --pr --from <integration-branch> --target main
```

**Pass both flags.** `promote --pr` reads its base branch from `defaults.lands_on` and its head
branch from `--from`, whose default is the literal name `integration`. A bare `harmonik promote --pr`
therefore aims at the integration branch, not at `main`.

**Today the daemon still resolves its merge target to `main`.** It does that whenever
`.harmonik/branching.yaml` is absent, and that file is absent in this repo. Create the file and set
`defaults.lands_on` to the integration branch. Add `main` to `defaults.protect_branches` in the same
edit, so the daemon fails closed and refuses to push `main`. Until both keys are set, this section
states a rule that the running system does not yet follow.

- **Commit on a short-lived branch, then merge that branch into the integration branch.** Merge it
  when the review passes. Delete the branch after the merge.
- **Do not commit to `main` directly.** Do not push `main`.
- **Keep a work branch short-lived — one unit of work.** A branch that outlives its unit of work
  drifts from the integration branch, and the merge cost grows with every day it stays open. Two
  patterns have produced that here and are worth stopping on: a `user/<topic>` branch, and an
  `agent/<codename>` branch kept alive across sessions. If a branch needs to live longer, land the
  finished part first.
- **No release branches** pre-1.0. Tags point at `main`, because `main` is the released trunk. If a
  hot-fix on an older release is ever needed post-1.0, spin a `release/0.y` branch at that point — not
  preemptively.
- **The runtime integration branch is a different thing.** harmonik's workflow engine merges
  run-branches at runtime (workspace-model §5.8). That is the product's behavior for a user's repo. It
  is not this repo's branch model. Do not confuse the two.

## Commit standards

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

**Required before every non-trivial commit:** `make full` passes locally (per `quality-checks.md §Two gate targets`) AND `agent-reviewer` ran with a non-`BLOCK` verdict. The verdict is recorded as the `Reviewed-By:` trailer.

## Agent review on every commit

Before an agent commits, it runs:

1. **`make full`** — the merge decision per `quality-checks.md`. Must pass.
2. **`agent-reviewer` skill invocation** — against the agent's own work product (diff from the last `main` tip).

The `agent-reviewer` checks:
- **Spec alignment** — does the diff match `specs/*.md`? Any silent divergence?
- **Idiom compliance** — idiomatic Go, error wrapping at boundaries, context propagation, ZFC tags on new cross-subsystem interfaces.
- **Test adequacy** — per `docs/methodology/TESTING.md` layer expectations for the change scope.
- **Unwanted-abstraction detection** — "did you add an abstraction the user didn't ask for?" (per CLAUDE.md).
- **Bead / codename match** — "does the diff match the bead or kerf codename it claims to implement?"

Reviewer emits a structured JSON verdict in the `Review-Verdict:` trailer. Three `verdict` enum values: `APPROVE`, `REQUEST_CHANGES`, `BLOCK`. `BLOCK` verdicts are never committed (agent fixes before committing). `REQUEST_CHANGES` verdicts may be committed WITH the trailer + a rationale in the commit body; `flags` array names the specific issues. `APPROVE` verdicts commit normally. A fourth value, `NOT_REVIEWED`, is not a reviewer output at all — it is what the author writes when no reviewer could be reached, per §Commit conventions above.

**Trivial commits** (typo, whitespace, one-line obvious fix) MAY skip `agent-reviewer`; they still run `make full`.

**Human review happens asynchronously after commit.** The user reads `git log` + diffs on their own cadence; catches what agents miss (premature abstraction, scope creep, subtle spec-misinterpretation, "technically works but wrong"). Human review is NOT a gate — nothing blocks on it.

## Bug fixes require a reproducing test at the lowest failing layer

**2026-05-21 user direction.** Several runtime bugs in the 2026-05-21 dogfood session (hk-37zy8, hk-yjduq, hk-2hb2y, hk-5s7tg, hk-trjef) passed unit tests and reviewer agents but failed in live runs. Unit-level coverage is insufficient for behavior that only manifests across subsystem boundaries or under real process lifecycle. New rule:

**Trigger.** Any bead labeled `bug`, OR any bead filed in response to a runtime failure observed in dogfooding (regardless of label).

**Requirement.** The fix-commit MUST land alongside a test that reproduces the bug, **at the lowest layer that still fails before the fix.** The test SHOULD be written first and committed failing; the fix flips it green.

**Choosing the layer — this is the part that was missing, and its absence cost 885 bead-named test files.** If a unit test can express the bug, it was a unit-level bug and a unit test is the correct and sufficient answer. If no unit test can express it — because the bug only appears across a subsystem boundary or under real process lifecycle, which is what the 2026-05-21 failures above had in common — then the scenario tier is genuinely correct, **and you must actually run it.** Writing a scenario-*shaped* assertion inside a unit test, or committing a scenario test you never ran, satisfies neither layer and is what this rule previously produced in combination with implementer-protocol F19.

If the scenario tier is the right layer and you cannot run it inside your dispatch budget, that is a signal the change is too big for one dispatch — report it and commit the smaller piece. Do not defer the gate.

**Exemptions.** Trivial typo/docs fixes; fixes whose reproduction requires an irreproducible environment (flaky third-party service, race only observed on a since-retired machine). Name the exemption in the commit body with the phrase `scenario-test exempt: <reason>` as a plain line — **not** under a `## Risk` heading, which `.claude/implementer-protocol.md` forbids.

**Reviewer check.** `agent-reviewer` MUST verify the bug bead has a reproducing test in the diff **at the layer the bug actually occupies**. A unit test in the existing `_test.go` is correct and sufficient when a unit test can express the bug. **Do NOT require the scenario tier merely because the bead is labeled `bug`** — require it only when the diff shows the bug crosses a subsystem boundary or depends on real process lifecycle, and in that case the commit MUST state the scenario test was actually run.

If no reproducing test is present at any layer and no exemption is claimed → `REQUEST_CHANGES` with flag `missing-scenario-test`. If the exemption clause is present but unjustified (e.g. bug is reproducible from the bead description) → `BLOCK` with the same flag.

**A unit test where a unit test suffices is NEVER a `missing-scenario-test` finding.**

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

There is no "merge all PRs" step. Only one pull request reaches `main`, and it is the
integration→`main` pull request. Cut the release tag on `main` after that pull request merges.

### Pre-release checklist

Before cutting a tag:

1. Ensure `main` is green: `make full` passes, CI clean on latest commit.
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
| CI | `make full` on the tagged commit | Exit 0 |
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

- **Merge a work branch into the integration branch.** Do not commit to `main`. See §"Branch model — land on the integration branch".
- **Never rewrite `main` history after a tag.** A tag is a published pointer: the release ledger in
  `internal/release/manifest.go` records the commit hash, the supervisor refuses a binary whose hash
  matches a yanked entry, and both go blind if the commit that hash names is gone.
- **Never `--force-push` `main`.** A force-push discards commits no local clone can recover, and
  branch protection is what stops it. `--force-with-lease` is allowed on a short-lived work branch,
  where the only history at risk is your own and the lease refuses if someone else pushed.
- **`--no-verify` is forbidden, and it stays forbidden now that no hook reads it.** There is nothing left for the flag to skip (see §Fresh-clone bootstrap), so a commit carrying it is not bypassing a check — it is a written statement that the author intended to. That is the reason for the rule: this repo has no human merge gate, so the only thing standing between a bad commit and the integration branch is the author running `/check` and recording an honest verdict. An agent that reaches for `--no-verify` has already decided to route around its own gate, and the next agent has no way to see what was skipped. Red gate, red result, root-cause fix, re-commit.
- **Signed commits (`git commit -S`)** — nice-to-have, not required; revisit when the product gets real users.
- **`.gitignore`** must cover: `/bin/`, `/dist/`, `.harmonik/` (runtime state), `*.test`, `coverage.out`, `.kerf/` (gitignored per CLAUDE.md).

## ⚑ Assumptions worth user's eye

1. **⚑ An integration branch plus agent-reviewer-every-commit is the failure-tolerance model.** The trade-off is speed up front. A bad commit can still reach the integration branch and need a fix-forward. The integration→`main` pull request keeps that bad commit off `main` until a human looks. For agent-coded solo development this is probably the right trade. Revisit when the product has real users or a multi-human team.
2. **⚑ `agent-reviewer` skill is load-bearing and must not rot.** Agent-config Tier 2 cadence explicitly checks its currency. If the reviewer becomes a rubber stamp, the whole model collapses.
3. **⚑ Reviewer verdict as commit trailer.** `Reviewed-By: agent-reviewer` (presence marker) + `Review-Verdict:` (structured JSON) makes review outcome auditable via `git log`. `BLOCK` verdicts by definition never land.
4. **⚑ JSON-structured `agent-reviewer` verdict** (not prose). Prevents prompt-injection; enables audit/metrics. Schema lives in the `agent-reviewer` skill's documentation and is versioned; agents MUST use the current schema.
5. **⚑ No hard LOC cap on commits.** Commits are naturally smaller than PRs; an explicit ceiling isn't needed. Monitor commit size; add a cap if agents produce megacommits.
6. **⚑ `spec:` commit type** — non-standard within Conventional Commits; added because spec work dominates early. Alternative: fold into `docs:` or `chore:`.
7. **⚑ Post-commit CI on the integration branch** — re-runs `make full`. Failures require fix-forward. No gate rejects the push to the integration branch. The integration→`main` pull request is where CI acts as a gate.

## Deferred / follow-up

- **Per-change pull requests and human code review** — restored when the product has real users or multiple human contributors. Today the agent reviewer gates each commit, and one human pull request gates integration→`main`.
- **CI provider choice** — GitHub Actions assumed; not pinned.
- **Coverage thresholds** — per-package minimums to block merge; wait until testing methodology settles.
- **Dependency update policy** — dependabot cadence, pinning vs. floating.
- **Security disclosure process** — not a concern pre-1.0; revisit before public announcement.
- **Release-note automation** — changelog is hand-authored now; consider `git-cliff` once commit-message discipline is proven.
