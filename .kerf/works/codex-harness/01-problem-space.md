# 01 — Problem Space: `codex-harness`

> Plan-jig Pass 1. Source of truth for scope, constraints, and success criteria.
> Problem space derived from the crew mission brief
> (`.harmonik/crew/missions/codexcrew.md`) authored by the captain/operator — this is a
> research+design work, so the "user conversation" is the mission brief itself.

## Summary

harmonik today can drive exactly **one** implementer harness: **Claude Code** — the `claude`
CLI launched in a tmux pane, seeded by bracketed-paste, watched for a git commit, then
quit-on-commit. We want to add **OpenAI codex** (OpenAI's coding-agent CLI) as a **second,
selectable implementer harness**, so that a given bead/run can be executed by *either* Claude
*or* codex. This work designs the **abstraction seam** (a Go `Harness` interface), the **codex
adapter's** behavior, the **auth/billing** posture, and the **integration/selection** model —
and files the implementation beads. **No code is written this session.**

## Goals

1. **Extract the implicit Harness interface** harmonik already assumes for Claude, anchored to
   `file:line`, so we know the exact contract a second harness must satisfy.
2. **Characterize the codex CLI surface** (non-interactive/headless launch, prompt seed, JSON
   output, session/resume, completion signal, whether it self-commits) from *current, cited*
   sources — not from memory.
3. **Define the smallest seam** that supports both harnesses: which call sites become
   harness-dispatched vs stay shared (tmux substrate, worktree mgmt, git commit-detection,
   review-loop control flow are expected shared; launch/seed/re-task/teardown expected to vary).
4. **Settle the auth/billing model** for codex — explicitly which launch mode bills a ChatGPT
   subscription vs the OpenAI API credit pool — and specify the daemon's env-stripping guard so
   codex cannot silently bill the credit pool (mirroring the `ANTHROPIC_API_KEY`-in-`.env`
   credit-burn incident this project already fought).
5. **Design harness selection & migration:** how a run picks its harness (per-bead / per-queue /
   global default with override), the config surface, defaulting (Claude stays default), how DOT
   workflow nodes and the reviewer reference the harness, and N-1 back-compat (existing beads keep
   working untouched).
6. **File the implementation beads** (`codename:codex-harness`, `open`, NOT dispatched) and reach
   kerf `ready` / pass `kerf square`.

## Non-goals (explicitly out of scope this session)

- **Writing or dispatching any implementation code.** Deliverable is kerf artifacts + filed beads.
- **Adding a third+ harness or a general "plugin marketplace."** Design the seam to *not preclude*
  a third harness, but only Claude + codex are in scope.
- **Changing the review-loop's control flow or the workflow-DOT engine** beyond the minimal hooks
  needed to name a harness per node/run.
- **Replacing or re-architecting the tmux substrate, worktree manager, queue, or
  commit-detection.** These are expected to be *shared* and reused unchanged.
- **Per-model routing inside a harness** (e.g. choosing GPT-5 vs another OpenAI model) beyond what
  selecting the codex harness inherently implies — that's a configuration detail, not the seam.
- **Cost/quality benchmarking of codex vs Claude.** Out of scope; this is the integration design.

## Constraints

- **C1 — Live daemon / clean tree.** The harmonik daemon is live during this session. The repo
  working tree MUST stay clean (uncommitted dirt trips the daemon's escape-detector and fails
  in-flight beads). All artifacts go to the gitignored kerf bench
  (`.kerf/works/codex-harness/`); scratch to `.harmonik/` or `/tmp`.
- **C2 — Billing landmine.** harmonik's hard-won posture: the local `claude` CLI bills the **Max
  subscription**; the Agent-SDK/API path bills a separate **credit pool** (the burn this project
  fought). The codex design MUST identify codex's equivalent surfaces and recommend the
  subscription-billed path, with a fail-closed env guard.
- **C3 — Smallest viable seam.** No speculative abstraction layers (project rule: don't add
  abstraction the user hasn't asked for). The `Harness` interface must be the minimum that lets
  Claude and codex coexist; shared infra stays shared.
- **C4 — N-1 back-compat.** Existing beads / queues / workflows with no harness specified MUST
  continue to run on Claude exactly as today. Selection defaults to Claude.
- **C5 — Spec-first project.** harmonik is spec-first; normative specs live in `specs/`. The
  `Harness` contract is itself a normative seam, so a `specs/` artifact (harness contract) is a
  natural output of the change-spec/integration passes.
- **C6 — Evidence discipline.** Dimension-2 facts (codex CLI) must be web-sourced and cited with
  versions/dates; codex has evolved fast. Where codex lacks a Claude primitive (caller-minted
  session id, bracketed-paste re-task), the design must state how the adapter compensates rather
  than assume parity.

## Success criteria (concrete, verifiable)

- **S1.** A document states the implicit Harness interface harmonik assumes today, every claim
  anchored to `file:line`, covering: launch argv + session-id minting, tmux spawn substrate,
  prompt seed + re-task injection, commit/completion detection, worktree isolation, and review-loop
  reuse of the spawn path.
- **S2.** A document states, with cited sources and versions, how to launch codex
  non-interactively: seed mode, JSON output, session/resume (and whether the caller can mint the
  session id), completion signal, and whether codex self-commits — including an explicit list of
  primitives codex *lacks* vs Claude and the adapter's compensation for each.
- **S3.** A `Harness` Go interface is proposed, with the exact existing call sites that become
  harness-dispatched vs remain shared, naming the files that change. The seam is defensibly the
  *smallest* that supports both.
- **S4.** The auth/billing section states plainly, per codex launch mode, whether it bills a
  ChatGPT subscription or the OpenAI API credit pool, recommends the subscription path, and
  specifies the concrete daemon env-stripping/precedence guard that prevents silent credit-pool
  billing.
- **S5.** A selection/integration design specifies where the harness field lives (bead / queue /
  global default + override), the config surface, how DOT nodes and the reviewer reference the
  harness, and a migration story proving N-1 safety (existing beads unchanged → Claude).
- **S6.** Implementation beads exist, labelled `codename:codex-harness`, status `open`, NOT
  dispatched, forming an ordered DAG a future session can execute. `kerf square codex-harness`
  passes and the work is at `ready`.

## Open questions to resolve during research (not blockers)

- **Q1.** Does codex expose a caller-mintable, resumable session id (claude `--session-id` parity),
  or only a captured id? Determines whether re-task (review-loop iteration N>1) targets the *same*
  codex session or a fresh `resume`-by-captured-id launch.
- **Q2.** Does codex `exec` exit on completion (process-exit completion signal) or stay resident
  like the claude TUI? Determines whether the adapter needs the quit-on-commit/watchdog machinery
  at all, or gets a cleaner exit-code completion for free.
- **Q3.** Does codex self-commit or only edit the worktree? If it self-commits, the `Refs:` trailer
  convention and commit-detection may need an adapter-supplied wrapper to enforce the trailer.
- **Q4.** Subscription-billed headless path: does a ChatGPT login (`codex login`) work under
  `codex exec` non-interactively and persist a token file the daemon can rely on? If not, the
  billing recommendation changes.

## Translations glossary (internal → plain English)

- **harness** — the CLI program harmonik launches to do the actual coding work (today: `claude`).
- **seam** — the Go interface boundary where Claude-vs-codex behavior diverges.
- **re-task** — sending a follow-up prompt to an already-running implementer session (used for
  review-loop iteration N>1).
- **quit-on-commit / post-commit watchdog** — daemon machinery that ends the claude session once it
  detects the git commit, with grace + force-kill fallback.
- **N-1 back-compat** — old beads/queues/workflows keep working unchanged after the change ships.
