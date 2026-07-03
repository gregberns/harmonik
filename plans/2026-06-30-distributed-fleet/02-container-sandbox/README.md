# Idea 2 — Container / sandbox layer + the build-cache problem

**Scoping stub.** Date 2026-06-30. To be fleshed out after idea 1. This file frames the problem and
captures what already exists so the real sketch doesn't re-derive it.

---

## The vision (operator, distilled)

> "Need a container layer, so both tasks run through harmonik and crew can be run in isolated
> containers. Start with the harmonik *tasks* going into containers, then later crew. We've got
> remotes working/almost working so harmonik sends off to run on another machine — but this is a
> little different. We'd probably not want to force a particular container/sandbox framework — allow
> it to be flexible somehow."
>
> "Tied to the container issue — if we run in isolated containers, languages like Go, Rust, Haskell,
> OCaml all download source and compile (heavy build). If we only move source / the working tree
> over, then pulling libs, compiling, and the other steps take way too long. We need the isolated
> sandboxes to work as fast (or nearly as fast) as on the main machine."

Two distinct asks: **(a) a pluggable isolation layer** (tasks first, then crew), and **(b) the
warm-build problem** — a fresh sandbox must not pay the full from-scratch compile cost.

---

## Why this is different from "run on another machine" (idea 1)

Remote substrate moves work to a *different host*. Containers isolate work on *the same (or a
remote) host* for **security/blast-radius and reproducibility**, not for compute distribution. The
two compose: a node from idea 1 could run its per-bead work inside a container. But the *isolation
boundary* is a separate axis from the *node boundary*, and the build-cache problem is sharpest
*inside* the isolation boundary.

---

## What already exists (don't re-derive)

- **Sandbox is already a spec invariant — but a confinement contract, not a runtime.** `specs/
  operator-nfr.md` **ON-024** (command-execution sandbox: agent execution + skill provisioning must
  stay inside `workspace_path`) and **ON-025** (network egress whitelist). This is a
  filesystem/egress *boundary*, not a container framework. A container layer would be one concrete
  *enforcement* of ON-024.
- **`specs/workspace-model.md:198` deliberately excludes provisioning layers from MVH:** "No
  provisioning layer (adze, devbox, container build) participates in MVH worktree creation; the
  worktree is a plain subfolder." So containers were *considered and left out* of the minimal model
  — adding them is a deliberate expansion, with a clear seam to expand at.
- **Containers already exist as a *test* substrate, not a production sandbox.** Test-pyramid L3
  (`hk-yflqo`, merged `4bdf7e93`) runs scenarios in `alpine:latest` via a `DockerExecRunner`; skips
  when Docker is absent. OrbStack is up so these run live. This proves harmonik *can* drive a
  container runner — it's a starting point for a production one.
- **`specs/credential-isolation.md`** — the credential sole-holder/scrub contract across spawn
  boundaries. Directly relevant: a sandboxed crew that holds a model key (e.g. Pi's
  `OPENROUTER_API_KEY`) must not leak it across the boundary.
- **The build-cache pain is documented only as incidents, never as a design.** The go-build
  cache-reaper TOCTOU bug (reaper wiped the shared stdlib cache mid-build fleetwide; `hk-y3frr`) and
  the **warm-worktree affinity** idea (route a bead to the box whose worktree/object cache is hot —
  prior-art doc Family A: *"Worktree warmth ... is the affinity gold"*). **There is no plan
  reconciling per-sandbox ephemerality with build-cache reuse** — that's the genuine design gap.

---

## The two real design problems (for the full sketch)

1. **Pluggable isolation, framework-agnostic.** Define an interface (à la the existing `Runner`
   abstraction the remote/Docker test paths already use) so the sandbox backend is config —
   Docker / OrbStack / Lima / Apple `container` / Firejail / nsjail / plain-namespace — without
   harmonik hard-coding one. The `DockerExecRunner` from L3 is the shape to generalize.
2. **Warm builds in cold sandboxes — the hard part.** Candidate strategies to evaluate:
   - **Mounted/shared cache volumes** — bind the host's `$GOMODCACHE` / `$GOCACHE`, cargo registry +
     target-dir, cabal/stack store, opam switch into the container read-mostly. Fast, but the
     cache-reaper TOCTOU lesson screams: *shared mutable cache across concurrent builds is a
     footgun.* Needs per-language locking or copy-on-write.
   - **Warm base images** — bake toolchain + pre-fetched deps into a layer; rebuild on dep drift.
   - **Content-addressed / remote build cache** — sccache / Go's `GOCACHEPROG`, a shared cache
     server. Heaviest, most robust, language-by-language.
   - **CoW snapshots** — clone a warm worktree/overlay per task (OrbStack/Lima/zfs/APFS clones).
     Closest to "as fast as the main machine"; ties isolation backend to a filesystem feature.
   - **Warm-affinity routing (reuse idea 1)** — don't fully isolate the build; route to a node/
     sandbox whose cache is already hot. The prior-art affinity idea, applied to sandboxes.

   The likely answer is **per-language**, since each toolchain's cache model differs — Go's is
   benign-ish to share, Rust's `target/` is not, Haskell/OCaml have switch/store semantics.

---

## Candidate backends to evaluate (operator-flagged 2026-06-30)

Raw notes for the pluggable-isolation matrix — to be evaluated properly in the full sketch. The
operator's steer: **we probably want *process* isolation, not full composable-computer / VM
isolation.** That biases us toward lightweight OS-level sandboxing over heavyweight
sandbox-as-a-service.

- **Daytona** (https://www.daytona.io/docs/) — "secure and elastic infrastructure for running
  AI-generated code ... full composable computers — sandboxes — with complete isolation, a dedicated
  kernel, filesystem, network stack, and allocated vCPU/RAM/disk." Has come up before. *Concern:*
  this is closer to a **micro-VM / full-computer** model than process isolation — likely heavier than
  what we want, and it's a hosted-infra product, which cuts against "don't force a particular
  framework." Worth understanding whether you *run processes* in it or ship it whole workloads.
- **Singularity / Apptainer** — HPC-origin container runtime, **rootless, single-file image (SIF)**,
  designed to run *processes* (not daemons) with the host user's identity. Much closer to the
  process-isolation model; strong for reproducible toolchains. Evaluate for the build-cache angle
  (SIF + bind-mounted caches).
- **Modal** (serverless) — serverless container backend; spin functions/containers on demand.
  Elastic and cache-aware, but hosted + function-shaped; same "hosted infra + framework lock-in"
  caution as Daytona. Interesting for *bursty* compute, less so for a persistent crew box.

**Read for the matrix:** heaviness (process vs. container vs. micro-VM vs. hosted), local-vs-hosted,
how each handles the warm-build/cache problem, and whether each fits behind a single pluggable
`Runner`-style seam without leaking its model into harmonik. Lean toward the lightest thing that
satisfies ON-024/ON-025 confinement.

## Open questions (for the full sketch)

- Tasks-first then crew: what's the minimal task-in-container path that reuses the L3
  `DockerExecRunner`?
- How does the sandbox boundary compose with idea-1 nodes (sandbox-on-node) and idea-3 Pi crew?
- Does ON-024/ON-025 already give us the contract a container backend must satisfy, or does the spec
  need extension?
- Per-language cache matrix: which strategy per toolchain, and who owns cache invalidation safely
  (post-TOCTOU)?

**Status: stub — flesh out after idea 1 is decided.**
