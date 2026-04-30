# Git-expert review — workspace-model v0.3.0 (R2)

- **Spec**: `specs/workspace-model.md` @ v0.3.0, status `draft`
- **Reviewer role**: git-specific — challenge every git-related claim
- **Date**: 2026-04-24
- **Prior review**: R1 (not re-read for this pass; this review is fresh against v0.3.0 text)

## 1. Verdict summary

WM v0.3.0 is **mostly right on the coarse shape** (worktree-per-run, `run/<run_id>` task branch, squash-merge, three-level branching), but it is **imprecise or silent on nearly every operational git detail**. The spec reads like it was written by someone who has used `git worktree` but has not fought with it. The gaps that most concern me, in priority order:

1. **WM-003** specifies `git worktree add <path> <branch>` with no `-b` flag — this is an error when `<branch>` does not yet exist, which is the normal case. (§2 below.)
2. **WM-006a** gives a ref-safe transformation that is not ref-safe by `git check-ref-format` rules. It names some forbidden characters but misses several classes (`@{`, control bytes, `.lock` suffix on *any* path component, the sole-`@` form, mixed-case collisions on case-insensitive filesystems). (§3 below.)
3. **WM-019** says "`git merge --squash` followed by a commit" but does not pin committer/author, does not describe the commit message, does not specify the working-tree state (conflict resolution runs in the worktree's index) and does not explain how the trailers are preserved given that squash *discards* the source-side commit metadata. (§4 below.)
4. **§6.1 `parent_commit`** is underspecified: it is *not* the merge-base, it is the branch-point at create-time, but the spec conflates the two concepts and §7.2 passes it as `start_point` to `worktree_add` — which is correct-ish but changes meaning over the run's lifetime (rebasing the integration branch invalidates it). (§5 below.)
5. **Lease-lock vs `.git/worktrees/<name>/gitdir.lock`** is never discussed. The WM lock is a *harmonik* artifact; git has its own worktree-locking mechanism (`git worktree lock`) with its own semantics. The spec needs to state explicitly that these are disjoint. (§7 below.)
6. **No git-version pin**. `git worktree` has had material behavior changes across 2.5 → 2.38 → 2.46; `--reason`, `--lock`, `repair`, `move`, porcelain-v2, and `remove --force` semantics are version-dependent. (§16 below.)
7. **No rerere, no reflog, no packed-refs, no LFS, no submodules, no bare-repo stance**. Some of these are appropriately out-of-scope; the spec should SAY so.

The shape is salvageable; none of the problems below require re-architecting. They do require adding ~8–12 requirement clauses and tightening the prose in ~15 places. The rest of this review walks each issue with line numbers, specific git commands, and proposed text.

---

## 2. Worktree semantics — `git worktree add` invocation

### 2.1 The current text

WM-003 (lines 152–157):

> The workspace manager MUST create a worktree via `git worktree add <path> <branch>` against the backing repository…

§7.2 pseudocode (line 698):

> `git.worktree_add(path, branch, start_point=parent_commit)`

### 2.2 What git actually does

`git worktree add <path> <commit-ish>` has **three** distinct modes determined by what `<commit-ish>` resolves to:

1. `git worktree add <path> <existing-branch>` — checks out the existing branch into the new worktree. **Errors if the branch is already checked out in another worktree** (unless `--force` is passed). Does NOT create a branch.
2. `git worktree add <path> <existing-commit-or-tag>` — creates a *detached HEAD* at that commit. No branch is created.
3. `git worktree add -b <new-branch> <path> [<start-point>]` — creates a *new* branch named `<new-branch>` at `<start-point>` (default HEAD) and checks it out in the worktree. **Errors if the branch already exists** (unless `--force`).

A convenience shortcut exists: `git worktree add <path>` (no `<commit-ish>`) auto-creates a branch named after the last component of `<path>` at HEAD. This is almost never what you want in automation.

### 2.3 The spec's mode is wrong for the normal case

On a fresh run, `run/<run_id>` does not exist yet. `git worktree add <path> run/<run_id>` would fail with:

```
fatal: invalid reference: run/<run_id>
```

(The ref does not resolve.) The correct invocation for WM's normal case is:

```
git worktree add -b run/<run_id> <path> <start-point>
```

where `<start-point>` is the tip of the integration branch at creation time (which is the spec's `parent_commit`).

### 2.4 The spec's mode is right for `resume-here` / recovery paths

After a daemon crash mid-run, the run may resume against an already-existing `run/<run_id>` branch. In that case `git worktree add <path> run/<run_id>` *is* correct — but only if the worktree directory no longer exists on disk, AND the branch is not currently checked out in another (orphan) worktree. WM-034 mints a fresh `run_id` on `reopen-bead`, so the branch never exists from a prior life; but WM-035 (`resume-here`) keeps the same `run_id`, which means a recovered run might re-`worktree add` against an existing branch.

### 2.5 Additional failure modes the spec does not handle

- **Branch checked out in another worktree.** `git worktree add` errors with `fatal: '<branch>' is already checked out at '<other-path>'`. WM-033's orphan-sweep (lines 446–453) only removes stale *lock files*, not stale worktree registrations — so a crashed daemon can leave `run/<run_id>` "checked out" in `.git/worktrees/<stale-name>/` indefinitely. The spec needs `git worktree prune` in the startup sweep, not just lock-file deletion.
- **Path exists but is not a worktree.** If someone `mkdir`-d the path (or a previous `git worktree add` crashed halfway), `git worktree add` errors with `fatal: '<path>' already exists`. WM-033 does not handle this; WM-001's `WorkspaceAlreadyExists` taxonomy (line 775) names the class but does not say it is distinct from the `git worktree prune`-able case.
- **Parent commit is shallow or unreachable.** If `<repo>` is a shallow clone, `git worktree add <path> <deep-commit>` errors. WM-002 insists on a "local clone" but does not say "complete clone." MVH can probably get away with assuming full clones; the spec should say so.
- **`core.worktree` / `HEAD` already points somewhere.** If the main working tree is in a detached-HEAD state, operations are fine; but `git worktree add` refreshes the object DB under `<repo>/.git/worktrees/<name>/` — which names the worktree by its directory's last path segment. Because WM-002 uses `<run_id>` as the directory name, and `run_id` matches `[A-Za-z0-9-]+` (filesystem-safe), the worktree registry name is `<run_id>`. **Good.** This makes `git worktree remove`, `list`, and `repair` discoverable by `run_id` — but the spec does not make the "worktree registry name == `run_id`" identity explicit. This is a bonus invariant worth declaring.

### 2.6 Proposed text

Replace WM-003 with:

> The workspace manager MUST create a worktree via:
>
> ```
> git -C <repo> worktree add \
>     -b run/<run_id> \
>     <path> \
>     <parent_commit>
> ```
>
> where `<path>` is the canonical path of WM-002, `run/<run_id>` is the task-branch name of WM-005, and `<parent_commit>` is the Workspace record's `parent_commit` field. The `-b` flag creates the task branch; the command MUST fail if `run/<run_id>` already exists in the repository (any ref namespace). The workspace manager MUST NOT pass `--force`; the `WorkspaceAlreadyExists` and `WorktreeCreationFailed` classes of §8 cover the failure cases.
>
> Recovery paths (WM-035 `resume-here`) MAY re-enter an existing `run/<run_id>` branch when the worktree directory is absent, by invoking `git worktree add <path> run/<run_id>` (no `-b`). This is an expected mode only inside the reconciliation-driven recovery path; normal `create_workspace` MUST use `-b`.

Add a new WM-003a:

> The worktree registry name under `<repo>/.git/worktrees/` MUST equal `<run_id>`. Since the worktree registry name is derived by git from the final path component of `<path>` (WM-002: `<repo>/.harmonik/worktrees/<run_id>/`), this identity holds by construction. Operations that reference the registry directly (e.g., `git worktree remove`, `git worktree repair`, `git worktree lock`) MUST use `<run_id>` as the key.

### 2.7 Per §7.2 pseudocode (line 698)

Change:

    git.worktree_add(path, branch, start_point=parent_commit)

to:

    git.worktree_add(path, new_branch=branch, start_point=parent_commit)  # uses -b

The distinction between "branch" (existing) and "new_branch" (created with `-b`) is load-bearing and the pseudocode hides it.

---

## 3. Branch-ref encoding — WM-006a

### 3.1 The current text

WM-006a (lines 185–189):

> …characters illegal in git ref names per `git-check-ref-format` (colon `:`, tilde `~`, caret `^`, question mark `?`, asterisk `*`, open-bracket `[`, backslash `\`, whitespace, sequences `..`, leading `/` or `-`, trailing `.lock` or `/`) MUST be replaced with `-` or rejected at workspace-create time…

### 3.2 What `git check-ref-format` actually forbids

Per `git-check-ref-format(1)`, a ref name component is rejected if any of the following hold. I'll mark which ones WM-006a covers:

| Rule | Covered? |
|---|---|
| begins with `.` | **not covered** |
| ends with `/` | covered (trailing `/`) |
| ends with `.lock` | covered — but the rule is "any **slash-separated component** ending in `.lock`," not just the whole ref. `foo.lock/bar` is invalid; WM-006a's text reads as if only the full-ref trailing is checked. |
| contains `..` | covered |
| contains `@{` (ASCII) | **not covered** |
| is the single character `@` | **not covered** |
| contains ASCII control chars (`\000-\037`, `\177`) | WM says "whitespace" — close but not equivalent. Control chars include tab, BEL, etc., which aren't "whitespace" in the usual sense. |
| contains space | covered ("whitespace") |
| contains `:`, `?`, `[`, `\`, `^`, `~`, `*` | covered |
| contains consecutive `/` (i.e. `//`) | **not covered** |
| begins with `/` | covered (leading `/`) |
| contains `//` | **not covered** (distinct from "leading `/`") |
| ends with `.` | **not covered** |
| begins with `-` | WM-006a says "leading `-`" — partially covered but git's rule is per-component? Actually git's rule is only on the whole ref when using `git branch -`; most tools treat `-prefix` as an option flag, which is a separate hazard. The git plumbing accepts `refs/heads/-foo` but the porcelain `git branch -foo` does not. |

The "`.lock` suffix on any component" case is the most likely real bug: a bead ID like `bead/v1.lock/2026` would transform to `bead-v1.lock-2026` — no `.lock` at the *end of the full ref*, but originally it had `.lock` as a component, and after the `-` substitution of `/` characters it no longer does. OK, so the substitution happens to save us here. But an ID like `mybead.lock` would transform to `mybead.lock` (no forbidden char to strip) and then `harmonik/integration/mybead.lock` IS rejected by git because the last component ends in `.lock`.

### 3.3 Case-insensitive filesystems

Ref names on disk live as loose refs at `<repo>/.git/refs/heads/...`. On case-insensitive filesystems (macOS default, Windows), `harmonik/integration/Foo` and `harmonik/integration/foo` collide. Bead IDs that differ only in case will produce ref collisions that neither `check-ref-format` nor WM-006a's transformation catches. WM-006a should either (a) normalize case, or (b) declare case-collision an operator-observable error on first collision.

### 3.4 `.` leading character

A bead ID like `.hidden-task` — the transformation `-` substitution only fires on the listed forbidden characters; `.` is not in the list, so `.hidden-task` passes through unchanged and produces `harmonik/integration/.hidden-task`, which git rejects (component begins with `.`).

### 3.5 Unicode

`git check-ref-format` accepts arbitrary UTF-8 bytes outside the forbidden set. Bead IDs that are valid UTF-8 but contain RTL override characters, zero-width joiners, or combining marks will produce refs that are valid to git but confusing to operators. The spec doesn't declare a posture. Probably fine for MVH; worth a one-line note.

### 3.6 Length

Git refs are limited by filesystem max-path-length, not by git. A very long bead ID produces `<repo>/.git/refs/heads/harmonik/integration/<long-bead-id>` which can exceed filesystem limits (255 bytes per component on ext4, 1024 bytes total on some FS). Not a common case; should be documented as "bead IDs of length N+ SHOULD be rejected at bead-create" in `beads-integration.md`, and WM-006a should cross-reference.

### 3.7 Proposed text

Replace WM-006a with (diff-level changes):

> When substituting `<parent_bead_id>` into a git ref name per WM-006, the workspace manager MUST apply a ref-safe transformation that satisfies ALL rules of `git-check-ref-format(1)`. Specifically, the transformation MUST reject (fail-fast with a `RefSafeSubstitutionFailed` error per §8) any bead ID that, after transformation, would produce a ref name matching any of the following disqualifying patterns:
>
> - Any slash-separated component that begins with `.`, ends with `.`, ends with `.lock`, or is the single character `@`.
> - Contains the byte sequences `..`, `@{`, or `//`.
> - Contains any of `:` `~` `^` `?` `*` `[` `\` or any ASCII control character (`\x00`-`\x1f`, `\x7f`) or space.
> - Begins with `/` or `-`, or ends with `/`.
>
> Default substitution: each disqualifying byte (from the character-class bullet) is replaced with `-`; disqualifying structural patterns (`..`, `@{`, `//`, leading/trailing forms) cause rejection rather than silent repair. The workspace manager MUST validate the post-substitution ref with `git check-ref-format refs/heads/<result>` before using it.
>
> Case-collision policy: two bead IDs whose transformed forms differ only in case MUST produce a `RefSafeSubstitutionFailed` error at workspace-create on the case-insensitive-filesystem platforms (macOS default, Windows); the workspace manager detects the collision by `git show-ref --verify` probing the proposed ref name.

The key addition is "validate with `git check-ref-format`" — this moves the check from prose to the tool that owns the definition. No re-encoding scheme the spec invents will track git upstream; delegate.

---

## 4. Squash-merge semantics

### 4.1 The current text

WM-018 (lines 315–319) — merge is a node in the same worktree.
WM-018a (lines 321–325) — non-agentic or agentic merge node; "the orchestrator executes `git merge --squash` + commit."
WM-019 (lines 327–332) — "The merge MAY be implemented as `git merge --squash` followed by a commit."
WM-020 (lines 334–338) — "Merge is not fast-forward-only."
WM-021 (lines 340–345) — emits `workspace_merge_status` with `merge_commit_hash`.

### 4.2 What `git merge --squash` actually does

`git merge --squash <source>` is a compound operation that does NOT create any commit. It:

1. Performs a three-way merge between HEAD, `<source>`, and their merge-base.
2. Applies the result to the working tree AND the index.
3. Sets `$GIT_DIR/SQUASH_MSG` with a commit message listing the squashed commits.
4. **Does not advance HEAD** and **does not create a merge commit**.

The author must then run `git commit` to produce the squash commit. Important properties:

- The resulting commit has **one parent** (the integration-branch tip), not two. This is by design — squash merges lose multi-parent ancestry.
- `git merge-base <integration-branch> run/<run_id>` after the squash commit will return the *pre-merge* merge-base, because the squash commit has no parent link back to `run/<run_id>`. The task branch is effectively orphaned from the integration branch's history.
- `--ff-only` is irrelevant to `--squash`; they are mutually exclusive modes. WM-020 ("Merge is not fast-forward-only") is factually incoherent under `--squash` — squash merges are never fast-forward.

### 4.3 What WM-020 probably means

I think WM-020 intends to say "we support real three-way merges including conflicting ones," not "we support fast-forward." But under `--squash`, the commit creation is ALWAYS a new commit (not a ff), so the distinction does not apply. WM-020 should be rewritten as:

> The merge operation is always a squash-producing commit; fast-forward is not applicable because `--squash` never advances HEAD as a ff. The merge MUST support conflict detection (non-zero `git merge --squash` exit OR conflict markers in `git status --porcelain`) per WM-018a.

### 4.4 Commit message and authorship

The spec says nothing about:

- Who runs the `git commit` after `git merge --squash`? The non-agentic merge node (WM-018a variant a) — OK, the orchestrator shells out. The agentic merge node (variant b) — the handler process runs `git commit`.
- What commit message? `git commit` on a pending squash will use `$GIT_DIR/SQUASH_MSG` by default (if invoked as `git commit` with no `-m`), which lists the squashed commits' first-lines. This is often fine but verbose; harmonik probably wants a deterministic template.
- Who is the committer? `git commit` uses `user.name` / `user.email` from config, which may be the operator's identity, not harmonik's. The spec should declare: committer is harmonik-bot (configurable), author is the original-implementer's handler identity (for cognition commits) or harmonik-bot (for mechanism commits).
- Trailer preservation: WM-019 says "the squash commit's trailers MUST be the trailers of the final durable checkpoint on the task branch at merge-pending entry." This is a claim about commit-message content. It requires:
    1. Reading the final checkpoint's trailers via `git log -1 --format='%(trailers)' run/<run_id>`.
    2. Injecting them into the squash commit message.
    3. Committing with `git commit -m "<synthesized message>" --trailer ...` OR `git commit -F <message-file>`.

This is non-trivial and the spec is silent on the mechanic. The implementer would have to invent a templating layer.

### 4.5 Index state and conflict detection

WM-018a line 323 says conflict detection is "non-zero exit from `git merge --squash` OR the presence of conflict markers in `git status --porcelain` output." Subtleties:

- `git merge --squash` exits 0 on success (clean squash, index staged) and non-zero on conflict. So "non-zero exit" is a sufficient signal.
- However, `git merge --squash --no-commit` (redundant flag — squash implies no commit) leaves conflicted files in the index with the conflict markers in the worktree. The porcelain output would show `UU` (unmerged, both modified) entries.
- There is an edge case: if `git merge --squash` is invoked with `-X ours` or `-X theirs`, conflicts are silently auto-resolved. The spec does not forbid these strategy options; it should declare a default merge strategy.
- Default merge strategy for a two-branch merge is `recursive` (or `ort` since git 2.33, which is the default since 2.34). The spec should pin the strategy OR declare that the default is used. For reproducibility, I would recommend pinning `-s ort`.

### 4.6 Proposed text

Replace WM-019 / WM-020 with the following block:

> **WM-019 — Squash-merge invocation.** The merge-back operation MUST be executed in the worktree directory with the integration branch checked out. Mechanism-tagged merge node invocation:
>
> ```
> # Preconditions: workspace is in merge-pending; worktree has integration-branch tip checked out.
> git -C <path> fetch origin <integration-branch>   # if remote-tracking; otherwise no-op
> git -C <path> checkout <integration-branch>       # moves worktree HEAD; the task branch is read-only reference
> git -C <path> merge --squash --strategy=ort run/<run_id>
> # Inspect for conflicts: exit != 0 OR `git status --porcelain` shows UU/AA/DD entries
> # If clean:
> git -C <path> commit -F <trailer-preserving-message-file> \
>     --author="<original-implementer or harmonik-bot>" \
>     --trailer "Harmonik-Run-ID=<run_id>" \
>     --trailer "Harmonik-Bead-ID=<bead_id>"   # if present
> ```
>
> The merge strategy MUST be `ort` (git's default since 2.34; explicitly pinned here to avoid drift if the default changes).
>
> **WM-019a — Trailer preservation mechanism.** The squash commit MUST carry the `Harmonik-Run-ID` and (when present) `Harmonik-Bead-ID` trailers. The workspace manager MUST read the trailer values from the final durable checkpoint on the task branch via:
>
> ```
> git -C <path> log -1 --format='%(trailers:key=Harmonik-Run-ID,valueonly=true)' run/<run_id>
> git -C <path> log -1 --format='%(trailers:key=Harmonik-Bead-ID,valueonly=true)' run/<run_id>
> ```
>
> and inject them via `git commit --trailer`. The message BODY is set from `$GIT_DIR/worktrees/<run_id>/SQUASH_MSG` (the auto-generated listing) unless the workflow's merge node provides a template per its LaunchSpec.
>
> **WM-019b — Author and committer identity.** The squash commit's author MUST be the original-implementer's handler identity (for agentic merges per WM-018a variant b) OR harmonik-bot (for non-agentic merges per WM-018a variant a). The committer MUST be harmonik-bot in both cases. The identities are operator-configurable per [operator-nfr.md §4.3]; defaults are declared in a new OQ.

Replace WM-020 with:

> **WM-020 — Conflict detection.** Merge conflicts are detected by:
> (a) non-zero exit status from `git merge --squash`; OR
> (b) presence of any entry matching the porcelain-v1 pattern `UU|AA|DD|AU|UA|DU|UD` in `git status --porcelain` after `git merge --squash` returns.
>
> Squash merges are never fast-forward by construction (they always create a new commit); the prior WM v0.3 text "Merge is not fast-forward-only" is retired as incoherent.

---

## 5. `parent_commit` field

### 5.1 The current text

WM-001 (line 139) — `parent_commit` is "the commit the workspace was branched from."
§6.1 RECORD (line 576) — `parent_commit : CommitSHA` with the comment "commit SHA this worktree was branched from."
§7.2 pseudocode (line 698) — passed as `start_point` to `worktree_add`.

### 5.2 What git thinks

When a worktree is created with `git worktree add -b run/<run_id> <path> <start-point>`, the task branch's first commit has `<start-point>` as its parent. The value is equivalent to:

```
git merge-base <integration-branch-at-create-time> run/<run_id>
```

at the moment of creation — but this equivalence drifts if the integration branch is rebased, force-pushed, or has its history rewritten. In particular:

- `git merge-base --fork-point run/<run_id> <integration-branch>` uses the **reflog** of the integration branch to figure out where the task branch forked. This is not the same as the static `parent_commit` field.
- If the integration branch advances forward (normal case — other runs squash-merge onto it), `git merge-base` still returns the same SHA as `parent_commit` because the task branch did not advance its own merge-base.
- If the integration branch is rebased, the merge-base may no longer exist on the integration branch's current history. `parent_commit` remains a valid git object (reflog keeps it alive for ~90 days by default), but it may be unreachable from any branch.

### 5.3 What the spec should say

`parent_commit` is the branch-point SHA *at workspace-create time*. It is NOT the merge-base, NOT `--fork-point`, and it is stable across the workspace's lifetime (it's a record field, not a computed value).

Its uses:
- Passed as `<start-point>` to `git worktree add -b`.
- Reconciliation can use it for "what was the integration branch at the time this run started."
- The squash-merge does NOT re-consult `parent_commit`; it merges from `run/<run_id>` tip to the current integration-branch tip.

### 5.4 Proposed text

Clarify WM-001's field comment and add a WM-001a or §6.1 sidebar:

> `parent_commit` is the commit SHA at which the task branch was created (the `start-point` argument to `git worktree add -b`). It is RECORDED at create-time and NEVER updated; reconciliation and audit consumers treat it as an immutable witness. It is NOT equivalent to `git merge-base <integration> run/<run_id>` after the integration branch advances, and it is NOT equivalent to `git merge-base --fork-point` (which uses reflog). The squash-merge operation (§4.5) consults the integration-branch tip at merge-time, NOT `parent_commit`.

---

## 6. Transition records (EM-017) — sibling-file atomicity

### 6.1 The user's framing

The review prompt mentions "sibling files `.harmonik/transitions/<tx_id>.json` per EM-019" and asks about commit atomicity.

### 6.2 The spec's position

WM v0.3.0 does not mention `.harmonik/transitions/` at all. It mentions:
- `.harmonik/worktrees/` — worktree root (line 145).
- `.harmonik/sessions/<session_id>/` — session-log directory (line 390).
- `.harmonik/lease.lock` — lease lock (line 640).

Transition records are EM's territory. WM cites [execution-model.md §4.5 EM-023] for checkpoint commits and EM-017 for trailer schema. Whether transition sidecar files live in the worktree tree (and thus get committed) or outside the worktree (and thus do not) is an EM concern.

### 6.3 What I'd want to see from WM

Even if transition records are EM-owned, WM should declare whether the `.harmonik/` subtree is expected to be *inside* git's working tree (and thus tracked) or outside. Currently:

- `.harmonik/worktrees/` is the *parent* of the worktree directories — each worktree is at `<repo>/.harmonik/worktrees/<run_id>/`, which is a separate checkout. The worktrees themselves are NOT inside another worktree's working tree; git's `core.worktree` for each registered worktree points to its own path.
- Inside a given worktree at `<repo>/.harmonik/worktrees/<run_id>/`, the `.harmonik/sessions/` and `.harmonik/lease.lock` paths ARE inside that worktree's working tree. Unless `.gitignore` excludes them (WM-030 explicitly forbids gitignoring `.harmonik/sessions/`), they will appear in `git status` as untracked.
- Whether `.harmonik/lease.lock` is committed is a policy question. WM-030 ("MVH default requires that project `.gitignore` MUST NOT exclude `.harmonik/sessions/`") implies sessions ARE committed as part of the squash. What about `lease.lock`? The spec does not say.

### 6.4 A real bug the spec may have

If `.harmonik/lease.lock` is inside the worktree's working tree (it is — per §6.2 line 640), and the squash-merge commit includes all changes from the task branch, then `lease.lock` gets committed to the task branch on every checkpoint commit that follows a lease event. Since WM-013b says the lease lock is released (file removed) on terminal transitions, and WM-013a says it is created at `leased` entry, the file exists during the worktree's active life. Any checkpoint commit taken during that life would include `lease.lock` as a tracked change.

**This is almost certainly wrong.** The lock file is process-local daemon state and should be gitignored. WM-013a / WM-033's filename discussion does not address this.

### 6.5 Proposed text

Add a new WM-013e:

> `.harmonik/lease.lock` MUST be added to the project's `.gitignore` (or the workspace manager MUST maintain a per-worktree `.git/info/exclude` entry) so that the lease-lock file does not appear in checkpoint commits. Contrast with `.harmonik/sessions/` which WM-030 EXPLICITLY requires to be tracked in the merged tree. The two sibling paths have inverted ignore semantics: `sessions/` is tracked (for audit), `lease.lock` is ignored (process-local state).

And an explicit scope clause: "Transition records (`.harmonik/transitions/<tx_id>.json` per [execution-model.md §EM-019]) are EM-owned; whether they are tracked by git is declared in EM, not here. If EM declares them tracked, WM's §6.1 `Workspace.path` and §6.2 path table apply; if EM declares them gitignored, WM has no opinion." This forces EM to make the call rather than leaving a silent gap.

---

## 7. Lock-file interaction with git's own worktree-lock

### 7.1 Two distinct lock concepts

Git has **two** unrelated locking mechanisms that the spec must distinguish:

1. **`.git/worktrees/<name>/gitdir.lock`** — A transient lock file used by git itself during operations that modify the worktree's gitdir (e.g., `git worktree move`, `git worktree repair`). Short-lived, created by git plumbing, never touched by WM.
2. **`git worktree lock [--reason=<text>] <path>`** — An administrator-set "do not remove" marker stored as `.git/worktrees/<name>/locked`. Prevents `git worktree remove` from deleting the worktree without `--force`. Persistent until `git worktree unlock`.

### 7.2 WM's lease lock is a third thing

`.harmonik/lease.lock` (per WM-013a) is a harmonik-level artifact. It represents "this worktree is leased by run X on daemon PID Y." It is NOT a git lock.

### 7.3 Interactions the spec does not discuss

- If an operator runs `git worktree lock <path>`, subsequent `git worktree remove <path>` fails with `fatal: The worktree '<path>' is locked: <reason>`. WM's failed-run persistence (WM-031) does not require `worktree remove`, but operator cleanup workflows (OQ-WM-010) will need to handle the lock-on-worktree case.
- WM's orphan sweep (WM-033) "MUST NOT delete worktree directories or branches." Good. It only removes stale lock *files*. But the spec does not say whether `git worktree prune` (which removes `.git/worktrees/<name>/` for worktrees whose working directory no longer exists) is invoked. Without `prune`, crashed daemons leave stale worktree registrations that block re-creation if the directory is eventually recreated.
- If WM ever needs to prevent accidental operator deletion of an in-flight worktree, calling `git worktree lock --reason="harmonik: run <run_id> in flight"` would be the clean mechanism. The spec does not adopt this, which is a missed opportunity — the lease-lock file is invisible to `git` tools; `git worktree lock` is visible.

### 7.4 Proposed text

Add WM-013f:

> The workspace manager's lease-lock file (`${workspace_path}/.harmonik/lease.lock`) is DISJOINT from git's own worktree-locking mechanism (`git worktree lock`, producing `.git/worktrees/<run_id>/locked`). The two locks have different meanings and different consumers:
>
> - Harmonik lease-lock: identifies the owning run and daemon PID; consumed by the workspace manager and the reconciliation detector.
> - Git worktree-lock: prevents accidental `git worktree remove --force=false` deletion; consumed by operators using raw git tooling.
>
> The workspace manager SHOULD additionally invoke `git worktree lock --reason="harmonik: run <run_id> in flight; remove only via operator cleanup workflow"` on entry to the `leased` state, and MUST invoke `git worktree unlock` before any operator-initiated cleanup workflow attempts `git worktree remove`. MVH MAY defer the `git worktree lock` call; but if deferred, operators running `git worktree remove <path>` directly on an in-flight worktree will break the run without warning. Tracked as OQ-WM-012.

And add a clause to WM-033:

> The orphan sweep MUST invoke `git worktree prune` against the backing repository after removing stale lease-lock files. `git worktree prune` removes `.git/worktrees/<name>/` entries whose working directory no longer exists on disk; this is the git-side companion to WM's lock-file sweep. Without it, a crashed daemon leaves stale worktree registrations that can block re-creation when `run_id` collides after a filesystem restore.

---

## 8. Orphan-sweep coordination with `git worktree prune`

### 8.1 What `git worktree prune` does

`git worktree prune` enumerates `.git/worktrees/<name>/` entries and removes those whose `gitdir` file points to a nonexistent working-tree path (or whose `gitdir` is empty or unreadable). It does NOT consult filesystem mtimes; it consults the `gitdir` content and `stat()` on the target.

### 8.2 WM-033's definition

WM-033 (lines 446–453) defines orphan sweep as "stale lease-lock files from worktrees whose owning daemon is no longer running." Staleness rule: "lock mtime predates the current daemon's start time" (delegated to PL-006).

### 8.3 Mismatch

The two orphan definitions diverge:

- **`git worktree prune` orphan** = working-tree directory deleted, `.git/worktrees/<name>/` remains.
- **WM-033 orphan** = lease-lock file whose owning PID is gone; working-tree directory still exists.

These are complementary — a well-behaved sweep should do BOTH:

1. `git worktree prune` — clean up stale `.git/worktrees/<name>/` entries.
2. WM-033 — remove stale lease-lock files from extant worktrees.

The spec mentions only (2). It should also invoke (1), or explicitly defer it to the operator.

### 8.4 A related failure mode

If a worktree directory is removed by hand (e.g., `rm -rf`), but `.git/worktrees/<run_id>/` still exists, a subsequent `git worktree add <same-path> run/<run_id>` fails with `fatal: '<run_id>' is a missing but already registered worktree`. The fix is `git worktree prune` OR `git worktree add --force`. Per WM-002's `run_id` uniqueness rule (WM-034 requires fresh `run_id`), re-using a path is forbidden — so this specific case should not arise. But filesystem accidents happen; the spec should document the recovery.

### 8.5 Proposed text

Modify WM-033 to invoke `git worktree prune` explicitly (see §7.4 above), and add WM-033a:

> Operator-initiated cleanup (removal of a failed-run worktree) MUST use:
>
> ```
> git worktree remove <path>           # cleanly removes worktree dir + .git/worktrees/<run_id>/
> git branch -D run/<run_id>           # if branch deletion is desired (MVH preserves; operator-policy)
> ```
>
> NOT `rm -rf <path>`, which leaves a stale `.git/worktrees/<run_id>/` entry that must be swept separately via `git worktree prune`.

---

## 9. Re-run: fresh worktree — `git worktree remove` semantics

### 9.1 The current text

WM-034 (lines 457–464): `reopen-bead` verdict produces a fresh `run_id` and fresh worktree at `<repo>/.harmonik/worktrees/<new_run_id>/`. The prior run's worktree persists. No `git worktree remove` is invoked.

### 9.2 Is that right?

Yes, for MVH. WM-031 explicitly preserves failed-run worktrees. The spec is internally consistent here.

### 9.3 What happens if the operator manually removes the prior worktree

If the operator runs `git worktree remove <old-path>` between failed run and new `reopen-bead` run:

- `<repo>/.harmonik/worktrees/<old_run_id>/` is deleted.
- `.git/worktrees/<old_run_id>/` is deleted.
- `run/<old_run_id>` branch may or may not be deleted (depends on `git worktree remove`'s behavior, which does NOT delete the branch unless `--force` is given and the worktree is the branch's only checkout).

This is fine because the new run uses a different `run_id`. The spec is safe here by construction.

### 9.4 `git worktree lock` interaction

If the prior worktree was locked via `git worktree lock` (WM does not currently do this — see §7), `git worktree remove` fails without `--force`. The spec should document: if WM adopts `git worktree lock` per §7.4, it MUST also invoke `git worktree unlock` before allowing operator cleanup (which is post-MVH per OQ-WM-010).

### 9.5 Proposed text

Nothing required for MVH. If §7's `git worktree lock` adoption is accepted, add to WM-031:

> When the workspace manager has set `git worktree lock` on a worktree per WM-013f, it MUST invoke `git worktree unlock <path>` as part of lease release (WM-013b). Failing to unlock leaves the worktree undeletable by operator cleanup workflows.

---

## 10. Original-implementer trailer walk — WM-022

### 10.1 The current text

WM-022 (lines 350–357):

> …the workspace manager MUST walk the task branch in reverse from tip to the merge-base with the integration branch and select the most recent commit whose `Harmonik-Actor-Role` trailer (per [execution-model.md §4.4]) resolves to an agentic node…

### 10.2 Is the proposed query correct?

Kind of. The walk is conceptually right. The concrete command needs care:

```
git -C <path> log \
    --reverse=false \
    --first-parent \
    --format='%H%n%(trailers:key=Harmonik-Actor-Role,valueonly=true)%n----' \
    <integration-branch>..run/<run_id>
```

Subtleties:

- **`<integration-branch>..run/<run_id>`** is the range "commits on `run/<run_id>` not on `<integration-branch>`" — this is the task-branch-unique commits. This is what you want.
- **`--first-parent`** matters if the task branch has merge commits on it (from sub-workflow work or recovery paths). Without it, the walk visits both sides of any merge, which is probably not what "original implementer" means.
- **`%(trailers:key=...)`** is available since git 2.18. For WM's git-version pin (see §16), 2.23 or later is a safe floor.
- **Merge commits**: trailers on a merge commit (authored by the merging process) are NOT the trailers of the merged content. If the task branch has a merge commit in its history (from, say, an earlier sub-workflow), the walk should probably SKIP merge commits or descend their `--first-parent` side. `--first-parent` handles this by convention (follows the first parent = the task branch's own mainline).

### 10.3 What "most recent commit" means

The walk is "reverse from tip to merge-base." The first commit encountered whose `Harmonik-Actor-Role` resolves to an agentic node is the selected commit. If the merge-node commit itself has a trailer (mechanism tag), it is skipped. Good.

But "resolves to an agentic node" requires a lookup: the trailer gives the role ID, and the role must be looked up in a registry to determine agentic vs mechanism classification. The spec does not name where this registry lives. It cites [architecture.md §4.7 AR-024] for `agent_type` conformance — that's probably the authoritative resolution table. WM-022 should cite it for the resolution mechanism explicitly.

### 10.4 The sidecar-join step

WM-022 says:

> …the sidecar at `.harmonik/sessions/<session_id>/harmonik.meta.json` keyed by that commit's session supplies the `agent_type` and LaunchSpec template.

This is a join from commit → session_id → sidecar. But the commit does not directly carry `session_id`; it carries trailers. The spec is implying a `Harmonik-Session-ID` trailer without declaring it. Either:

- EM-017 declares a `Harmonik-Session-ID` trailer (check EM); OR
- There is an implicit join via some other key.

I don't have EM in front of me; if EM-017 does NOT declare `Harmonik-Session-ID`, this is a latent bug. WM-022 should cite the specific trailer used for the join.

### 10.5 Proposed text

Replace WM-022's mechanical-identification clause with:

> The workspace manager MUST identify the original implementer by the following mechanical procedure:
>
> ```
> # Pseudocode of the git walk (use first-parent to avoid merge-commit descent):
> git -C <path> log \
>     --first-parent \
>     --format='%H%x00%(trailers:key=Harmonik-Actor-Role,valueonly=true)%x00%(trailers:key=Harmonik-Session-ID,valueonly=true)' \
>     <integration-branch>..run/<run_id>
> ```
>
> (a) Iterate results from tip-ward; for each commit, resolve `Harmonik-Actor-Role` against the agent-type conformance table per [architecture.md §4.7 AR-024]. (b) Select the FIRST commit (most recent) whose role is agentic. (c) Read the `Harmonik-Session-ID` trailer from that commit; this is the `session_id` for the sidecar join. (d) Read `.harmonik/sessions/<session_id>/harmonik.meta.json` for `agent_type` and LaunchSpec parameters.
>
> If the walk produces no agentic commit, WM-022a applies (null `implementer_handler_ref`, escalation-only path).

And cross-check that EM-017 declares `Harmonik-Session-ID` as a trailer. If it does not, flag for EM integration.

---

## 11. Conflict resolution mechanism — merge strategy

### 11.1 The current text

WM-020 (lines 334–338): merge is not fast-forward-only.
WM-023 (lines 366–371): unresolvable conflicts escalate.
WM-024 (lines 373–384): re-dispatched implementer gets (task branch tip, integration branch tip, conflict-markered paths, transition history).

### 11.2 What strategy does the resolver use?

The spec does not commit to a strategy. Options:

- **`git merge-tree <merge-base> <branch1> <branch2>`** — produces conflict markers without touching the index. Good for previewing conflicts; not useful for actually resolving because the resolver needs to edit and stage.
- **`git merge --squash --strategy=ort <task-branch>`** followed by resolver editing the worktree and re-staging — this is the approach WM-018a implies. The resolver sees conflict markers in the worktree, edits files, runs `git add <resolved-files>`, then `git commit`.
- **`git merge --no-ff --no-commit <task-branch>`** — produces a merge commit (two parents) rather than a squash. WM-019's "one commit per task" contract forbids this.
- **rerere** — see §12.

### 11.3 What the implementer agent sees

Given the spec's squash-merge posture, the resolver agent arrives in a worktree where:
- `git status` shows `UU` entries for conflicted paths.
- Conflict markers (`<<<<<<<`, `=======`, `>>>>>>>`) are in the files on disk.
- The index has three-way entries (stage 1 = base, stage 2 = ours/integration, stage 3 = theirs/task-branch).
- `git merge --squash` has already been invoked; the agent resolves and `git commit`s.

The spec does not say which side is "ours" and which is "theirs." Convention: if the resolver runs `git merge --squash` from the integration branch, "ours" is the integration side and "theirs" is the task branch. This is almost certainly the intended posture but is not declared.

### 11.4 Index-only vs worktree resolution

The resolver agent's handler could choose to:
- Edit files in the worktree and stage them (`git add`). Standard workflow.
- Use `git checkout --ours <path>` or `git checkout --theirs <path>` to pick a side wholesale. Mechanical resolution.
- Use `git checkout --merge --conflict=diff3 <path>` to re-render conflict markers with base content. Diagnostic aid.

WM-024 lets the handler do any of these via its LaunchSpec. Fine.

### 11.5 Proposed text

Add WM-024a:

> The resolver handler's working state at dispatch MUST be:
>
> - Worktree HEAD at the integration-branch tip (side `--ours`).
> - `git merge --squash --strategy=ort run/<run_id>` has been invoked and returned non-zero.
> - Conflicted paths appear in `git status --porcelain` with the `UU` (or `AA`/`DU`/etc.) prefix.
> - The index has three-way stage entries for each conflict.
> - The LaunchSpec's input payload enumerates the conflicted paths per WM-024.
>
> The handler resolves conflicts by (a) editing files and `git add`-ing them, or (b) using `git checkout --ours <path>` / `git checkout --theirs <path>` for mechanical pick-one-side resolution, or (c) invoking `git merge --abort` followed by a fresh strategy (permitted but MUST be recoverable). Handler completion with `git status --porcelain` showing zero `U?` / `?U` / `UU` / `AA` / `DU` / `UD` / `DD` / `AU` / `UA` entries AND a successful `git commit` on the integration branch counts as resolved; any other terminal state routes to WM-023 escalation.

---

## 12. rerere

### 12.1 Is it mentioned?

No. The words "rerere" and "reuse recorded resolution" do not appear in the spec.

### 12.2 Should it be?

`git rerere` (reuse recorded resolution) records conflict resolutions and auto-applies them when the same conflict recurs. For harmonik's workflow:

- Task branches in flight do not typically re-encounter the same conflict (each `run_id` is fresh).
- BUT, across runs, similar conflicts (e.g., same line in same file, conflicting against repeated concurrent updates on integration) could recur. Rerere would auto-resolve, reducing the need for implementer re-dispatch.
- Rerere state lives in `$GIT_DIR/rr-cache/` — at the backing-repo level, shared across all worktrees of the same repo.

### 12.3 Risks

- Rerere records "ours preferred" resolutions. If multiple runs use the same integration branch with different notions of correctness, rerere could apply a stale resolution silently.
- Rerere is per-*repo*, not per-worktree. Enabling it affects all workflows.
- Harmonik's cognition-tagged conflict resolution (WM-022) is supposed to invoke an implementer agent; rerere would short-circuit this in silent-auto cases. If the implementer agent is the cognitive authority, rerere undermines it.

### 12.4 Recommendation

For MVH, don't enable rerere. Explicit position:

> The workspace manager MUST NOT enable `git rerere` (`rerere.enabled` config). Conflict resolution flows through WM-022 cognition-tagged handler re-dispatch; rerere's silent auto-resolution would bypass the cognitive-authority chain. Post-MVH may revisit if conflict patterns are repetitive enough to justify it; tracked as a potential OQ.

This is a small addition but worth having as an explicit negative requirement so implementers don't casually enable rerere thinking it's a quality-of-life improvement.

---

## 13. Reflog

### 13.1 Does WM rely on it?

Not directly, as far as I can see. The spec does not mention "reflog," "HEAD@{}" notation, or `--walk-reflogs`.

### 13.2 Where it matters implicitly

- **Recovery from operator `git reset`**: If an operator manually resets the task branch, the prior tip is recoverable via `git reflog run/<run_id>`. WM-INV-003 (line 545) forbids rewriting history on a task branch that has emitted `workspace_leased`; the sensor is [execution-model.md §EM-024a] branch-tip monotonicity. Reflog would be the recovery tool if this invariant is violated.
- **`parent_commit` reachability**: If the integration branch is force-pushed, `parent_commit` may be reachable ONLY via the reflog. WM does not rely on this but a downstream consumer might.
- **`git worktree add`'s internal ops** use reflog entries. Not a concern for WM.

### 13.3 Is silence OK?

Yes, for MVH. The spec's posture (git + Beads as state sources, no transactional layer) is reflog-adjacent — in practice, reconciliation on restart walks git refs, and reflog is implicitly the safety net if refs are rewritten.

A single sentence would tighten this:

> WM makes no affirmative guarantees about reflog availability. Operators relying on reflog-based recovery (e.g., recovering from a `git reset` on a task branch) MUST ensure `gc.reflogExpire` and `gc.reflogExpireUnreachable` are set to values compatible with their audit requirements; defaults (90 days / 30 days) are usually adequate for MVH.

Not load-bearing; nice to have.

---

## 14. Bare repo / submodule / LFS posture

### 14.1 Bare repo

WM-002 line 145:

> The daemon operates on a local clone only; workspaces MUST NOT be materialized against a bare remote URL.

Good. This rules out `git worktree add` against a bare repo (which is technically allowed by git but requires more configuration). The spec is explicit.

But: what about a `--bare` clone used as the backing repo? `git worktree add` works against `--bare` clones (and is in fact the idiomatic setup for worktree-heavy workflows). WM-002's "local clone" wording is ambiguous about whether the backing repo MUST be non-bare or MAY be bare.

Proposed clarification:

> The backing repository MAY be a bare clone (`.git` directory only, no working tree) OR a non-bare clone. Harmonik's worktrees are created via `git worktree add` in both cases; the `<repo>` field in the Workspace record is the path to the bare `.git` directory or the non-bare repo's top-level directory as appropriate.

### 14.2 Submodules

Not mentioned. Git worktrees and submodules have known rough edges:

- `git worktree add` does NOT initialize submodules in the new worktree by default. The user must run `git submodule update --init` inside the worktree.
- Submodule state is per-worktree (since git 2.25). Earlier versions share `.git/modules/` with the main worktree, leading to conflicts.
- Harmonik's agentic merges could produce submodule-pointer conflicts that cognition-tagged resolution does not handle well.

Proposed addition:

> Submodule support is OUT OF SCOPE for MVH. A repository containing git submodules MAY be used as the backing repo, but WM does not invoke `git submodule update` in worktrees it creates. Handlers that require submodule initialization MUST do so within their own LaunchSpec. Submodule-pointer conflicts during merge-back are treated as ordinary conflicts per WM-022 and resolved by the implementer handler; WM does not declare a specialized submodule-resolution strategy.

### 14.3 LFS

Not mentioned. Git LFS puts large-object pointers in the tree and fetches content on checkout. Implications:

- `git worktree add` triggers LFS smudge filters, which fetch large objects from the LFS server. This can be slow.
- LFS storage is per-repo; worktrees share the `.git/lfs/objects/` cache.
- On repositories with gigabytes of LFS content, `git worktree add` can take minutes.

Proposed addition:

> Git LFS repositories are SUPPORTED with no WM-specific configuration. `git worktree add` invokes LFS smudge filters by default; operators may disable this with `GIT_LFS_SKIP_SMUDGE=1` for faster worktree creation at the cost of lazy fetch on first access. WM does not declare a posture on LFS performance; the `created → ready` transition may be slow on LFS-heavy repositories.

### 14.4 Proposed text

Consolidate into a new §2.1 scope bullet or a §4.1 sidebar:

> **Backing-repo types.** WM supports (a) standard non-bare clones, (b) bare clones as backing repos, and (c) LFS-enabled repositories. Submodules are out of MVH scope; a backing repo MAY contain submodules but WM does not initialize them in created worktrees (handlers MUST do so if needed).

---

## 15. Packed-refs vs loose refs — scalability

### 15.1 What's at stake

- **Loose refs** live at `.git/refs/heads/<name>`. One file per ref. Fast to read, slow to enumerate when there are thousands.
- **Packed refs** live at `.git/packed-refs`. One file with all refs. Faster enumeration, slower ref update (rewrite).
- `git pack-refs` and `git gc` migrate loose refs to packed.
- `git update-ref` creates loose refs, then `git gc` packs them.

### 15.2 Harmonik's ref volume

Per harmonik's steady-state:
- One `run/<run_id>` branch per run.
- Fresh `run_id` on `reopen-bead` → branches accumulate.
- Integration branches: small number (one per parent-bead context + default `harmonik/integration`).

A long-lived repo could accumulate hundreds or thousands of `run/<run_id>` branches if `WM-031` preservation is honored indefinitely (OQ-WM-008 acknowledges this for disk but not for refs). With thousands of loose refs:
- `git worktree list --porcelain` slows proportionally.
- `git for-each-ref` slows.
- Ref enumeration on daemon startup (WM-013c) slows.

### 15.3 Does WM declare a posture?

No. The spec is silent on ref scaling.

### 15.4 Proposed text

Add to §10.3 (excluded conformance claims) or §A.3 (rationale):

> Ref scalability is not a declared conformance dimension. Operators with long-retention policies (many preserved `run/<run_id>` branches) SHOULD run `git pack-refs --all` periodically, OR configure `gc.auto` to ensure ref packing happens in normal `git gc` cycles. WM makes no claim about performance of `git worktree list` or ref enumeration beyond ~10,000 refs.

---

## 16. Git-version pin

### 16.1 What the spec says

Nothing. No git-version pin anywhere in v0.3.0.

### 16.2 Feature matrix

- `git worktree add` — available since 2.5 (2015).
- `git worktree add -b` — available since 2.5.
- `git worktree list --porcelain` — available since 2.7 (2016).
- `%(trailers:key=X,valueonly=true)` — format since 2.25 (2020); `key=` filter since 2.18 (2018).
- `git check-ref-format` — always available.
- `--strategy=ort` — since 2.33 (2021); default since 2.34.
- `git worktree lock --reason` — since 2.22 (2019).
- `git worktree remove` — since 2.17 (2018).
- `git worktree repair` — since 2.30 (2021).
- `git rerere` — since 2.0 or earlier; not relevant here.

### 16.3 What pin is sensible

Given harmonik's greenfield status and the template-level normativity, I'd pin **git ≥ 2.34** (Nov 2021). This gives:

- `--strategy=ort` default.
- `%(trailers:key=X,valueonly=true)` working.
- `git worktree repair` for recovery.
- `git worktree remove --force`.
- All major ref-format rules as documented.

If MVH must support older distros (RHEL 8 ships with git 2.27; RHEL 9 with 2.39), then ≥ 2.30 is the floor. I'd recommend 2.34 and live with the RHEL-8 issue.

### 16.4 Proposed text

Add to §10.1 (Core MVH conformance) or as a new WM requirement:

> **WM-041 — Minimum git version.** The backing git binary MUST be version 2.34 or later. Harmonik uses the following git features whose availability requires 2.34: `--strategy=ort` default (§4.5.WM-019), `%(trailers:key=X,valueonly=true)` format (§4.6.WM-022), `git worktree repair` (§4.8.WM-033 orphan-sweep recovery), and `git worktree lock --reason` (proposed WM-013f). Operators on older git versions MUST upgrade before enabling harmonik. The workspace manager SHOULD verify the git version at daemon startup and fail-fast if it is below the minimum.

---

## 17. Miscellaneous findings

### 17.1 `git worktree add <path>` creates missing parent directories?

`git worktree add` creates `<path>` but does NOT create missing parents. `<repo>/.harmonik/worktrees/` (WM-002's worktree root) must exist before the first `git worktree add`. §7.2 pseudocode (line 700) calls `mkdir(sessions_dir)` INSIDE the worktree; it does not create the worktree-root parent. This is a minor bootstrap detail the spec should mention:

> On first daemon startup, if `<repo>/.harmonik/worktrees/` does not exist, the workspace manager MUST create it (`mkdir -p`). Subsequent `git worktree add` invocations rely on the parent being present.

### 17.2 `git worktree add` and `.gitignore`

WM-002's worktree root is `<repo>/.harmonik/worktrees/`. This directory is INSIDE the backing repo's working tree. If it is not gitignored, `git status` from the main working tree will show all the sub-worktrees as untracked/mysterious. And committing `.harmonik/worktrees/<run_id>/` into the main branch would be catastrophic (it's a worktree, not tracked content).

**The spec needs to explicitly require `.harmonik/worktrees/` to be gitignored at the repo root.** This is a correctness-critical requirement, not a convention. Currently the spec:

- WM-030 (line 426): ".gitignore MUST NOT exclude `.harmonik/sessions/`" — right policy.
- No mention: `.harmonik/worktrees/` MUST be gitignored.
- No mention: `.harmonik/lease.lock` MUST be gitignored (see §6.4 above).

Proposed additions (consolidating §6.4 and here):

> **WM-041a — Project `.gitignore` requirements.** The backing repository's `.gitignore` at the repository root MUST contain:
>
> - `/.harmonik/worktrees/` — excluded so the main working tree does not see child worktrees as untracked content.
> - `/.harmonik/*.lock` (or `/.harmonik/lease.lock` specifically) — excluded so process-local lock files are not committed.
>
> The `.gitignore` at each WORKTREE's root (inherited or copied) MUST NOT exclude `.harmonik/sessions/` per WM-030; the audit contract requires sessions to be part of the merged tree.
>
> The workspace manager SHOULD verify these `.gitignore` entries at daemon startup and warn via operator-observability if they are missing; it SHOULD NOT auto-edit the project's `.gitignore` (that is a developer-owned file).

### 17.3 `git worktree add`'s `--detach` flag

WM does not use `--detach`. Good — all worktrees are on named branches, which matches the spec's intent. No change needed.

### 17.4 `git worktree add` locks and concurrent operations

`git worktree add` acquires `$GIT_DIR/index.lock` briefly and `$GIT_DIR/worktrees/<name>/gitdir.lock` for the duration of the add. Two concurrent `git worktree add` invocations against the same backing repo can serialize on these locks OR fail with lock errors. If harmonik dispatches many runs simultaneously, worktree creation becomes a serialization point. The spec should acknowledge this:

> The workspace manager's `create_workspace` operation (§7.2) MAY be serialized at the git-index-lock level if many workspaces are created concurrently. `git worktree add` acquires `$GIT_DIR/index.lock` briefly; concurrent invocations retry with exponential backoff per the workspace manager's implementation. This is an implementation concern, not a correctness concern — the spec declares only that `create_workspace` is non-idempotent and deterministic in its outputs (§6.1 axes).

### 17.5 `git worktree` and the main worktree's branch

If the integration branch is `harmonik/integration` and the main working tree (outside `.harmonik/worktrees/`) is ALSO on `harmonik/integration`, then `git worktree add <path> harmonik/integration` would fail (branch checked out elsewhere). WM-018 (merge-back is a node in the same worktree) does NOT create a new worktree for the merge, so the merge happens inside the task-branch worktree. The merge node would `git checkout harmonik/integration` inside the task-branch worktree. Does this conflict with the main worktree being on the same branch? **Yes, under normal git semantics.**

```
fatal: 'harmonik/integration' is already checked out at '<main-worktree-path>'
```

Workarounds:
- Keep the main worktree on a different branch (e.g., `main`).
- Use `git switch --ignore-other-worktrees harmonik/integration` (since 2.23).
- Put the main worktree in detached-HEAD state.

The spec is silent on this. It matters for operators running the daemon against a repo where they also work normally.

Proposed addition:

> **WM-041b — Integration-branch checkout collision with main worktree.** The merge-back node (§4.5.WM-018a) checks out the integration branch inside the task-branch worktree. If the backing repo's main worktree (the non-harmonik working tree) has the integration branch checked out, the `git checkout` will fail per git's "branch is already checked out at <path>" rule. The workspace manager MUST either (a) invoke `git checkout --ignore-other-worktrees <integration-branch>` (since git 2.23), OR (b) require operators to keep the main worktree on a non-integration branch. Default: (a). Tracked as OQ-WM-013 if an operator-facing policy is needed.

---

## 18. Summary of proposed spec edits

In descending priority:

1. **WM-003 — Use `-b`.** The most important fix. Current `git worktree add <path> <branch>` is broken for the normal case. Propose `git worktree add -b run/<run_id> <path> <parent_commit>`. See §2.6.
2. **WM-006a — Delegate ref-name validation to `git check-ref-format`.** The current enumeration is incomplete. Propose invoking `git check-ref-format refs/heads/<result>` after substitution. See §3.7.
3. **WM-019 — Specify committer/author/trailer/strategy.** Current text is ambiguous on who runs `git commit`, with what identity, what message, and what strategy. Propose pinning `--strategy=ort`, declaring committer=harmonik-bot, declaring trailer preservation via `--trailer`. See §4.6.
4. **WM-020 — Rewrite as "conflict detection rule."** The fast-forward framing is incoherent under squash. See §4.6.
5. **New WM-013e — Gitignore `.harmonik/lease.lock`.** Latent correctness bug: lock file would be committed in checkpoint commits. See §6.5.
6. **New WM-041a — Gitignore `.harmonik/worktrees/` at repo root.** Latent catastrophe: committing child worktrees into main branch. See §17.2.
7. **New WM-013f — Declare disjoint from `git worktree lock`; optionally adopt it.** See §7.4.
8. **WM-033 — Invoke `git worktree prune` in orphan sweep.** Missing companion operation. See §7.4, §8.5.
9. **New WM-041 — Pin minimum git version (2.34).** See §16.4.
10. **WM-022 — Pin trailer-walk command to `--first-parent`; cite `Harmonik-Session-ID` trailer.** See §10.5.
11. **New WM-024a — Declare resolver's working-state preconditions.** See §11.5.
12. **Scope clauses — LFS/submodule/bare-repo posture.** See §14.
13. **New WM-041b — Integration-branch checkout collision.** See §17.5.
14. **§10.3 — Note about ref scalability.** See §15.4.
15. **Negative requirement: no rerere.** See §12.4.

Most of these are additive (new requirements) or tightening (more precise prose). None require rearchitecting the spec. Together they move the git-layer claims from "mostly right" to "precisely right."

---

## 19. What the spec got right (credit where due)

- **WM-002 canonical-path convention** — having the worktree-registry name equal `run_id` (by filesystem-segment identity) is a clean invariant; downstream operations via `git worktree list/remove/lock <run_id>` Just Work.
- **WM-004 deterministic `workspace_id`** — no separate store needed; good.
- **WM-005 `run/<run_id>` branch name** — unambiguous, sortable, grep-friendly. No complaints.
- **WM-018 merge-in-same-worktree** — creating a separate worktree just for the merge would be weird; WM correctly avoids it.
- **WM-019 "one commit per task"** — squash-merge posture is the right choice for a workflow system; integration-branch history stays clean.
- **WM-031 preserve failed runs** — aligns with audit + post-mortem requirements; aligns with git's own "branches are cheap" philosophy.
- **WM-INV-003 branch-tip monotonicity** — correctly defers to git's native commit semantics rather than inventing a transactional layer.
- **§6.1 closed `metadata` map** — explicit closed schema is better than the "extensible map" anti-pattern.
- **§6.2 path table** — having a single table of canonical paths is excellent. Just missing the gitignore policy per path (see §17.2).

The spec's architecture is sound. The git-level precision is where it needs tightening.

---

*End of review. 700 lines, within budget. No spec files were modified during this review.*
