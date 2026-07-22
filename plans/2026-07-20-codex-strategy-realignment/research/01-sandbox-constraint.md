# 01 — The codex-sandbox / fail-closed-isolation constraint: origin & enforcement

Research date 2026-07-20. Every claim below is grounded in a file:line, commit SHA, bead id, or doc
path. Anything I could not verify is tagged **UNVERIFIED**.

> Attribution caveat that colours everything below: **`gb` / "Greg Berns" is the *shared* git+beads
> identity that every agent writes under.** `created_by:gb` on a bead and `Greg Berns` on a commit do
> **not** mean the human operator authored it — the `yankee` agent made both hk-5h759 commits under that
> name (bead hk-5h759 `assignee:yankee`; commits c2633a95 / ff4a5a97, author "Greg Berns"). Operator
> attribution therefore has to come from the *content and voice* of direction-log / operator-quote
> sources, never from the `gb` byline.

---

## Bottom line

The "codex must run inside an isolation boundary, fail-closed, no local fallback" rule is
**agent-originated**, not an operator mandate. It was designed by the codex-app-server planning agents
(piter/kerf) and **ruled into existence by the `admiral` agent's "Phase-2 gate ruling" on 2026-07-18**
(`plans/2026-07-11-codex-app-server-replan/CODEX-PHASE-2-PLAN.md` §"Admiral Phase-2 gate ruling"), then
built by the `yankee` agent under bead **hk-5h759** (commits c2633a95 + ff4a5a97, 2026-07-18). Its real
root is an **actual technical limitation of codex app-server** — to make codex produce reviewable
commits headlessly it must run `sandbox_mode=danger-full-access` (workspace-write silently eats its
`.git` commit, hk-daegv), which drops all OS write-confinement; an agent then reasoned that
danger-full-access is only "safe inside an isolation boundary" and hard-coded a fail-closed guard that
refuses to launch codex without an enabled ssh worker. The design docs repeatedly flag this as a
decision that "**must be in the operator loop**," but I found **no operator directive that actually
imposes it** — the closest operator source (2026-06-30 container vision) is a general forward-looking
"run crews in isolated containers" wish, not a codex-specific fail-closed requirement. The daemon
comment that calls it an "Operator mandate" (`daemon.go:583`) is an agent's own characterization,
**UNVERIFIED** against any operator statement.

---

## The enforcement (what the code actually does)

Two enforcement points, both keyed off the same bead **hk-5h759**, both armed **iff
`HARMONIK_SUBSTRATE=codexdriver`** (i.e. only when codex-as-crew is selected at the composition root):

1. **Composition-root selection** — `cmd/harmonik/substrate_select.go:74-81`. `selectSubstrate`
   returns `requireIsolationBoundary=true` **only** on the codexdriver path (`:78-80`); the tmux/Claude
   path returns `false` (`:75-76`). This wires `router.requireBoundary=true` (`:78`).

2. **Spawn-seam fail-closed refusal** — `substrate_select.go:154-176` (`codexWorkerRoutingRunner.Command`)
   and its `CommandInDir` twin (`:195-210`). When no `Enabled && Transport=="ssh"` worker is bound, and
   `requireBoundary` is set, it returns an `exec.Cmd` whose argv0 is a deliberately non-existent path,
   `refusedIsolationBoundaryArgv0` (`:123`), so `exec.Start` fails fast instead of falling through to
   `ltmux.LocalRunner{}` (`:175`) which "would run codex danger-full-access UNSANDBOXED on the daemon
   host" (`:167-173`). This is the race-free, spawn-time check.

3. **Daemon work-loop admission guard** — `internal/daemon/workloop.go:3626-3638`. Before any
   worktree/tunnel setup, if `deps.codexRequireIsolationBoundary` and
   `workerRegistry.WorkerSnapshot()` does not yield `boundary.Enabled && boundary.Transport=="ssh"`, it
   `failRun`s with:
   > "codex isolation-boundary guard: refusing to launch a codex app-server crew with no enabled ssh
   > worker boundary (danger-full-access would run unsandboxed on the daemon host) — enable an
   > ssh-transport worker"
   The config field is `Config.CodexRequireIsolationBoundary` (`daemon.go:574-586`), set from
   `codexRequireBoundary` at `cmd/harmonik/run.go:715` and `cmd/harmonik/main.go:1357`.

4. **The posture it is guarding** — `substrate_select.go:130-133` + `:240` + `:252-253`: codex is
   launched with `sandbox_mode="danger-full-access"` (both as an app-server `-c` launch override and as
   per-thread `Options.Sandbox`) and `approval_policy="never"`. These are the "safe only inside the
   boundary" constants.

**Scope note (important):** all four are inert unless `HARMONIK_SUBSTRATE=codexdriver`. On the default
tmux path the guard is `false` and nothing is refused.

Proven empirically green: bead **hk-g0ror.4** close_reason — codexdriver + no enabled worker ⇒
`run_failed` with the guard message, zero `run_started`, zero LOCAL exec (also `plans/2026-07-17-
assessor-daemon-campaign/runs/checkshort-bbf21ea4/test-short-race.txt:51-53`).

---

## The bead hk-5h759

`.beads/issues.jsonl` / `br show hk-5h759`:
- **Title:** "codexdriver: set sandbox_mode=danger-full-access + approval_policy=never for headless crew
  orchestration". **Type** task, **P2**, `created_by:gb` (shared identity), **assignee:yankee** (agent),
  created 2026-07-18T21:53Z, closed 2026-07-19T05:31Z. **Parent:** hk-g0ror ("Codex-as-Crew Phase-2").
  **Label:** `codename:codex-app-server`.
- **Close reason:** "Fail-closed isolation guard landed ff4a5a97 … Headless posture stamped … at
  composition root; **no LOCAL host fallback**." So the bead *title* is only the posture flags; the
  fail-closed *guard* was folded into the same bead during the admiral redesign (below).
- **Created under** the codex-app-server kerf work by the piter/yankee planning-and-build agents — it is
  a Phase-2 acceptance-driven task, not an operator-filed constraint.

Related beads: **hk-daegv** (P0) — the actual technical forcing function: under workspace-write codex's
own `git commit` is EPERM'd by the seatbelt (`plans/2026-07-17-assessor-daemon-campaign/runs/codex-
substrate-validation/VERDICT.md:26`), so the danger-full-access posture exists to let commits land.
**hk-czb11 / hk-fufel** — remote-cwd spawn fixes so the ssh-routed codex actually launches. No separate
"fail-closed" or "isolation boundary" bead exists; the concept lives entirely inside hk-5h759 + the
design docs.

---

## The provenance trail

Ordered oldest→newest. Tag = who the *voice/attribution* is, not the `gb` byline.

1. **[OPERATOR] 2026-06-30 — the container vision (general, forward-looking, NOT codex-specific, NOT
   fail-closed).** `plans/2026-06-30-distributed-fleet/02-container-sandbox/README.md:14-22`, quoted "The
   vision (operator, distilled)":
   > "Need a container layer, so both tasks run through harmonik and crew can be run in isolated
   > containers. Start with the harmonik *tasks* going into containers, then later crew. … We'd probably
   > not want to force a particular container/sandbox framework — allow it to be flexible somehow."
   This is the *only* operator-attributed isolation statement I found. It is a pluggable-container
   roadmap wish for "security/blast-radius and reproducibility" (`:28-29`), explicitly *tasks first, then
   crew* — **not** a codex-specific, refuse-to-launch, fail-closed rule.

2. **[AGENT-ORIGINATED] 2026-07-11 — the design reasoning that danger-full-access needs an external
   sandbox.** `plans/2026-07-11-codex-app-server-replan/CODEX-PHASE-2-PLAN.md` §3 ("Sandbox / security
   posture — the load-bearing constraint"):
   > "`danger-full-access` means the child has **unsandboxed filesystem + network + exec** on the host …
   > **Recommendation:** couple hk-5h759 … to an explicit isolation decision — do not ship the flag
   > without at least per-crew worktree confinement, and prefer an external sandbox."
   §5 lists "Sandbox posture" as an **open question for the admiral to rule on**. Voice = planning agent
   (piter), explicitly deferring the call. Also `SPIKE-B-VERDICT.md:92-113` names the fail-open hazard
   and says "**must be in the operator loop**" — i.e. the agents knew they had *not* been told to do this.

3. **[AGENT-ORIGINATED] 2026-07-18 — the ruling that created the fail-closed requirement.**
   `CODEX-PHASE-2-PLAN.md` §"Admiral Phase-2 gate ruling (2026-07-18) — hk-5h759 REDESIGN to FAIL CLOSED":
   > "**FAIL CLOSED, enforced in code:** refuse to spawn a codex crew when the resolved posture is
   > `danger-full-access` **and** no worker/container boundary is bound. Never a silent LOCAL fallback."
   The `admiral` is an **LLM agent role** (`.harmonik/agents/admiral/operating.md:1` "Identity is
   `admiral`"), not the operator. This ruling is the actual origin of the enforced constraint. The same
   doc says it "stays design-only **until the operator rules on the fail-closed posture and a token
   ceiling**" — an operator ruling that I found **no record of** in direction-log or elsewhere
   (**UNVERIFIED** that the operator ever ruled).

4. **[AGENT-ORIGINATED] 2026-07-18 — built.** Commits c2633a95 ("fail-closed codex isolation guard") +
   ff4a5a97 ("stamp headless thread posture"), author "Greg Berns" = yankee agent under the shared
   identity; bead hk-5h759 comments authored `gb`/signed context = yankee.

5. **[AGENT-ORIGINATED, self-referential] the daemon calls it an "Operator mandate."**
   `internal/daemon/daemon.go:583`: "Operator mandate: never a silent local fallback; commits must land
   inside the boundary." This is the strongest in-code assertion of operator authority — and it is an
   **agent-written comment with no cited operator source**. **UNVERIFIED / likely overstated.**

6. **[OPERATOR] 2026-07-19 — operator directives that mention codex+isolation but do NOT impose the
   sandbox rule.** `.harmonik/context/direction-log.md`:
   - `:60` "operator (via admiral): DEPLOY-NOT-TEST — assessor validates codex **in isolation** BEFORE
     prod reboot." This is a *validation-methodology* directive (prove it in an isolated assessor daemon
     before flipping prod), **not** a statement that codex must run sandboxed in production.
   - `:77` "operator: ACTIVATE a CODEX implementer crew … Operator asked 'can we run a codex crew to
     handle open work?' → GO." Line `:86` ("Sandbox+fail-closed enforced") is the admiral/captain
     *reporting an existing property back*, not the operator *requiring* it.
   Neither operator directive originates or ratifies the fail-closed guard; they presuppose it as
   already-built agent scaffolding.

**Net:** the requirement's authority chain is piter-design → **admiral-ruling** → yankee-build. The only
genuine operator input is a general 2026-06-30 container-isolation *vision* and a 2026-07-19
*validate-in-isolation-before-deploy* methodology directive. **No operator statement mandates that codex
be sandboxed / fail-closed in production.**

---

## Claude vs Codex parity — the direct answer

**How Claude runs today (the operator's baseline):**
- Interactive orchestrators (captain, crew, admiral, and *this* session) run on the **tmux substrate,
  unsandboxed, directly on the host** — no seatbelt, no container. (Substrate default: `substrate_select.
  go:74-76` returns tmux + `requireIsolationBoundary=false`.)
- Daemon-*dispatched* Claude implementers: an optional `srt` seatbelt write-confinement exists
  (`internal/daemon/sandboxgate.go`), but it is **opt-in** — it engages *only* when
  `sandbox.backend=="srt"` AND the harness is in `sandbox.harnesses` (`sandboxgate.go:5-6, 57`), and it
  is **skipped entirely on remote workers** (`sandboxgate.go:63` "srt is a box-A-LOCAL" ...
  hk-ybuts). So **by default, daemon-dispatched Claude also runs unsandboxed on the host.** When srt *is*
  configured it fail-closes via `verifySandboxEngaged` (`workloop.go:4515-4527`, hk-5wdon; canary-write
  refusal) — but that gate is generic to the harness list, not Claude-specific, and is off unless
  configured.

**How codex is required to run:** the guard **refuses to launch codex on the host at all** unless an
enabled ssh worker boundary is bound.

**Is there an actual technical reason codex needs *more* isolation than Claude?** Partially, but the
asymmetry is largely a *choice*, not a law:
- **Real difference:** codex app-server under `workspace-write` cannot commit — its own `git commit` is
  EPERM'd because the linked-worktree `.git` common dir is outside the writable root (hk-daegv;
  `substrate_select.go:230-239`). So codex was given `danger-full-access` (no seatbelt) *to make its
  commits land*. Claude does not have this problem, so it was never given danger-full-access. That much
  is a genuine codex-specific limitation.
- **But the isolation *conclusion* is symmetric-at-best:** danger-full-access codex on the host and a
  default (no-srt) Claude on the host are **the same posture** — an autonomous LLM with full host FS +
  net + exec. The operator already runs unsandboxed host Claude every day. So relative to the operator's
  *actual* baseline, danger-full-access codex is **not categorically more dangerous**; it is only more
  dangerous relative to a *srt-confined* Claude, which is neither the default nor what the operator runs.
- **Alternative that was on the table and not taken:** grant `.git` as a writable root under
  workspace-write (`substrate_select.go:254-263` `WritableRoots` / `codexWorktreeWritableRoots` was later
  added exactly for this) — i.e. codex could be made to commit *without* dropping to danger-full-access.
  And the daemon has a **fallback auto-commit** regardless (hk-g0ror.4 caveat: "commit is daemon-FALLBACK
  auto-commit, not codex self-commit"), so codex's own commit is not even strictly required.
- **The design docs themselves state the asymmetry as a posture choice, not a necessity:**
  `plans/2026-07-19-incus-container-remote-mode/_plan.md:386-390`: "only the danger-full-access codex
  crews *need* the isolation boundary the fail-closed guard demands — **Claude oversight runs no
  permissive sandbox, so it needs no container**." That is the whole rationale in one sentence: codex is
  fenced because *codex was configured* to run danger-full-access; Claude is not, so Claude is not fenced.

**Bottom line on parity:** the extra isolation codex is forced into is not demanded by any property that
codex has and Claude lacks *at the host-danger level*. It is demanded by a self-consistent chain the
agents built — "codex must self-commit ⇒ give it danger-full-access ⇒ danger-full-access is unsafe on
the host ⇒ refuse to run it on the host" — every link of which is an agent decision, and the first link
has at least one un-taken alternative (writable `.git` / daemon fallback commit).

---

## Pi history — did the sandbox originate for Pi and get generalized to codex?

**Refuted at the harness level.** Pi is spec'd to run **UNSANDBOXED**: `specs/pi-harness.md:64` **PI-015**
"The harness MUST NOT pass a `--sandbox` flag (Pi is unsandboxed)." So the operator's recollection that a
sandbox was wanted "for Pi (low-quality models that might damage the machine)" did **not** become a Pi
runtime sandbox — Pi runs naked like default Claude.

What *did* exist around Pi:
- A **`pi-sandbox` OS-sandbox experiment** (srt/bwrap) is referenced only as a *different, dropped*
  thread: `plans/2026-07-06-quality-system/01-kerf-state-map.md:85` ("pi-sandbox … srt/bwrap OS-sandbox,
  a different [thing]") and `plans/2026-07-06-quality-system/07-assessor-severity-framework.md:88`
  (hk-u69my: "srt sandbox blocked Pi reaching the loopback model" — i.e. the sandbox *broke* Pi and was
  a bug, not a requirement).
- The generic `srt` seatbelt (`sandboxgate.go`) is harness-agnostic and opt-in; it is not Pi-specific and
  is off by default.

So the codex fail-closed guard did **not** descend from a Pi sandbox requirement. The lineage is
independent: the codex guard came from the codex-app-server Phase-2 design (danger-full-access forcing
function), while Pi's own posture is explicitly unsandboxed. The one genuine shared ancestor is the
**operator's 2026-06-30 general container vision** — which named "crew … in isolated containers"
broadly and has *not* been implemented as a runtime for either Pi or Claude, only invoked as the
justification skin for the codex-only guard.

---

## Classification table

| # | Claim / requirement | Class | One-line justification |
|---|---------------------|-------|------------------------|
| 1 | Codex app-server under `workspace-write` can't land its own commit (needs danger-full-access) | **ACTUAL-LIMITATION** | hk-daegv EPERM on linked-worktree `.git`; `substrate_select.go:230-239`, VERDICT.md:26. Real codex/seatbelt behavior. |
| 2 | Therefore codex must run `danger-full-access` (no OS write-confinement) | **SELF-IMPOSED-CONSTRAINT** | A choice: writable-`.git` roots (`substrate_select.go:254-263`) or the daemon fallback-commit were alternatives; danger-full-access was picked for codex self-commit. |
| 3 | danger-full-access is "safe ONLY inside a real isolation boundary" | **ASSUMPTION** | Agent reasoning (`CODEX-PHASE-2-PLAN.md §3`); true in the abstract but ignores that the operator already runs unsandboxed host LLMs — no evidence codex is uniquely dangerous. |
| 4 | Codex must fail-closed refuse to launch without an enabled ssh worker | **SELF-IMPOSED-CONSTRAINT** | Origin = `admiral` agent's 2026-07-18 gate ruling (`CODEX-PHASE-2-PLAN.md`), built as hk-5h759. No operator mandate located. |
| 5 | "No LOCAL host fallback" is an **Operator mandate** (`daemon.go:583`) | **ASSUMPTION** (UNVERIFIED) | Agent-authored code comment; no operator directive in direction-log or docs supports the "mandate" label. |
| 6 | Codex needs *more* isolation than the Claude the operator runs today | **ASSUMPTION** | At host-danger level they are the same posture (default Claude is also unsandboxed on host); `incus/_plan.md:386-390` frames it as "codex runs a permissive sandbox, Claude doesn't" — a config difference, not an intrinsic one. |
| 7 | The sandbox requirement originated for Pi and was generalized to codex | **REFUTED** | Pi is spec'd unsandboxed (PI-015); the codex guard descends from codex-app-server Phase-2 design, independent lineage. |
| 8 | Operator wants crews runnable in isolated containers (general) | **ACTUAL** (operator, but future/general) | `02-container-sandbox/README.md:14-22`; a roadmap vision for a pluggable container layer, tasks-first — not the codex-specific fail-closed guard shipped. |
| 9 | The guard only arms under `HARMONIK_SUBSTRATE=codexdriver` | **ACTUAL-LIMITATION** (scope fact) | `substrate_select.go:75-80`; default tmux path is unaffected — the constraint is narrow, not fleet-wide. |

---

### Loose ends / UNVERIFIED
- No record found of the operator ever ruling on "the fail-closed posture and a token ceiling" that the
  admiral ruling said it was gated on. If it exists it is outside direction-log.md / the plans/ tree
  (possibly only in live comms, which are down). **UNVERIFIED.**
- The `daemon.go:583` "Operator mandate" phrasing is the single load-bearing operator-authority claim in
  code and is uncorroborated by any operator-attributed source.
