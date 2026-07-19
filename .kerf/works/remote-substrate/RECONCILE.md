# M4 — remote-substrate: reconcile note

> **CORRECTION 2026-07-16 (this banner supersedes a FALSE completion claim).** A prior version of
> this banner asserted that `01-problem-space.md` and `03-components.md` had been rewritten onto
> the M4 framing and status advanced to `decompose`. **That claim was false.** As of 2026-07-16
> the on-disk `01-problem-space.md`, `03-components.md`, and `07-tasks.md` were still
> **byte-identical** to their `_archive-phase1-landed/*.PHASE1.md` copies (`diff -q` reported no
> difference), and `spec.yaml` status was still `analyze` — the archive step ran, but the rewrite
> did not. An earlier authoring attempt was keeper-orphaned (`/clear`) mid-write; the "fresh run"
> the old banner described never actually landed the rewrite.
>
> **This has now been done for real (2026-07-16 authoring pass).** `01-problem-space.md`,
> `03-components.md`, and `07-tasks.md` are rewritten onto the M4 framing with the six
> operator-locked decisions (below) as durable constraints, built onto the AS-BUILT M2/M3 seams
> (not the Phase-1 speculative model). The Phase-1 docs on the bench describe **already-merged
> code** — that is the whole reason they had to be replaced rather than "advanced." Status was
> carried honestly through the passes toward `ready`.
>
> Authored by the M4 planning sub-agent (planner role, daemon-OFF, out-of-pipeline, no beads per
> the operator no-beads directive). Nothing was deleted; superseded Phase-1 content stays archived
> under `_archive-phase1-landed/`.
>
> **Pass status (honest).** `spec.yaml` was advanced to `ready`. Because M4 v1 is composition-root
> wiring onto ALREADY-BUILT seams, the intermediate-pass content was FOLDED into the three rewritten
> docs rather than authored as separate `04-research/`, `05-specs/`, `06-integration.md` files:
> the **research/analyze** content is the as-built seam verification (`01-problem-space.md` §"The
> AS-BUILT seams M4 consumes", with file:line cites to the merged tree); the **change-spec**
> content is the F4 resolution + risk list (`03-components.md`); the **integration/tasks** content
> is the build order (`07-tasks.md`). `02-analysis.md` (Phase-1 seam analysis) is kept in place and
> is still accurate for the runner-threaded shape M4 retains. The design is build-ready in prose;
> the per-component change-spec text (esp. the RSM-017/019 spec edit for F4) is written by the
> implementer at build time.

> **Framing reversal the operator locked (2026-07-16).** The Phase-1→M4 story below was originally
> written to REVERSE decision DEC-A — rip out the runner-threading and replace it with a
> worker-resident network agent speaking a "real protocol." **The operator has reversed that
> reversal.** M4 v1 = **Option A, runner-threaded (SSH `CommandRunner`)** — the same shape Phase-1
> shipped — and the worker-resident gRPC/tailnet agent is deferred to a Phase-3 cloud concern
> behind the same seam. The DEC-A dual-path cleanup (the ~98 `runner!=nil`/`IsRemote` branches) is
> **deferred**, not done in M4 v1. Where the prose below says "M4 reverses DEC-A / worker-resident
> real protocol," read it as **superseded** — see §"M4 locked decisions" and the new
> `01-problem-space.md`.

## Decision (one line)

**`remote-substrate` is the M4 home.** `remote-substrate-phase2` is FOLDED into it as
future-transport analysis context (containers), not carried as a separate work. The two works'
*landed Phase-1 design* is archived (it describes shipped code); their *durable problem-space +
hard constraints* survive and are rebased onto the revamp's M4 framing.

## Why — the framing shift the revamp forces

The two existing works predate the revamp and answered a *different question*:

- **`remote-substrate` (was: analyze)** — "add remote SSH execution." Its Phase-1 design
  (`_archive-phase1-landed/PHASE-1-DESIGN.md`, `03-components.PHASE1.md`, `07-tasks.PHASE1.md`) is
  **LANDED and validated on a real remote Mac** (`gb-mbp`; commits `b5cfa982e`,
  `dde46dd4d` "first remote dispatch proof", + the `internal/workers/` package, the pervasive
  `runner != nil` dual paths, `codesync_rs_b8.go`). Phase-1's governing decision **DEC-A** —
  "*no new `handler.Substrate` sibling; thread ONE `tmux.CommandRunner` through every dispatch
  site*" — is exactly what produced the `runner != nil` tech debt the revamp now wants to
  collapse. Phase-1 was the right pragmatic reuse move for shipping; it is the wrong long-term
  shape.
- **`remote-substrate-phase2` (problem-space)** — "local-container bead-isolation" via a
  `DockerRunner` sibling on the *same* `CommandRunner` seam. It **doubles down** on the runner-
  threading pattern (adds a third transport branch everywhere) and is a *capability* expansion,
  not the *rebuild* the revamp calls for.

The revamp's M4 is neither "Phase 1 SSH from scratch" nor "Phase 2 containers." It is **composition
-root wiring + hardening of the already-landed SSH-runner substrate onto the post-revamp M2/M3
seams** — M3's explicit merge queue (`internal/mergeq`, RSM-015..019) and M2's agent-input
`InputPort`/`Ack` contract (AIS-001/003/004). It keeps DEC-A's runner-threading (operator-locked;
see banner) and proves the daemon-on-mac-mini → agent-process-on-gb-mbp topology, Claude first.

This is spec-anchored, not invented:
- **RSM §2.2 (out of scope):** *"The remote worker-resident execution interface — a later work
  depends on this spec's merge queue but owns its own execution seam."* → that later work is the
  **Phase-3 cloud** concern, kept behind the same seam; NOT M4 v1.
- **RSM-019:** *"Pushing to origin remains inside the exclusive section in this spec; relocating
  the push outside it is deferred to the remote-execution work."* → **M4 owns that relocation**
  (fork F4; resolved in `03-components.md`).
- **AIS-003 (as-built, corrected):** the Ack is **BINARY** — `Delivered | Rejected`. There is NO
  `Degraded`/`Accepted` tri-state (that was the Phase-1 speculative model and was NOT what M2
  shipped). Remote Claude over tmux/paste correctly returns `Ack{Delivered}` (tmux/paste has no
  structured protocol — expected and fine for v1); positive acceptance is the ASYNCHRONOUS
  `agent_input_acked` event (relayed back from gb-mbp over the per-run reverse tunnel), and a
  never-confirmed submission reaches the distinct `agent_input_stale` terminal (bounded-liveness
  AIS-INV-001). See `internal/handler/input_port.go:29-103` and
  `internal/daemon/tmuxsubstrate.go:2245-2250`.

## What survives vs is superseded

| Content | Disposition |
|---|---|
| Resource-ceiling / isolation / idle-hardware problem statement (`01-problem-space.PHASE1.md` §problem) | **Survives** — rewritten into the new `01-problem-space.md` as durable motivation. |
| Hard constraints: interactive-never-`claude -p`; subscription-billing **MUST** (D2); box-A keeps merge authority (DEC-B); worker pushes a `run/<id>` branch; NFR7 zero-workers-byte-identical | **Survive** — carried into the new problem-space as HARD constraints. |
| DEC-A "no `handler.Substrate` sibling; thread one `CommandRunner`" | **RETAINED (operator-locked 2026-07-16).** M4 v1 keeps the runner-threading; the dual-path cleanup is DEFERRED, and the worker-resident execution seam is a Phase-3 cloud concern behind the same seam. (The archived Phase-1 doc that called DEC-A "superseded" is stale on this point.) |
| Phase-1 component list / task beads / TEST-PLAN / WORKER-SETUP-macos | **Archived** — describe shipped code, not M4 work. |
| `02-analysis.md` (Phase-1 seam analysis) | **Kept in place** — the seam-coupling inventory (pasteinject optional interfaces, workspace worktree free-funcs, remote-PID probes) is still accurate and feeds M4's analyze pass. Flagged as Phase-1-oriented; re-read against the retained (runner-threaded) seam. |
| `remote-substrate-phase2` (container transport) | **Folded** — containers are a FOLLOW-ON transport for M4's clean seam, not M4 v1 scope. The phase-2 bench is left intact (shelve, don't delete); its `DockerRunner`/two-phase-egress/OAuth-token design is future input. |

## What changed on this bench

- `01-problem-space.md` — **rewritten** onto the M4 framing (old copy → `_archive-phase1-landed/01-problem-space.PHASE1.md`).
- `03-components.md` — **rewritten** as the M4 component map (old copy → `_archive-phase1-landed/03-components.PHASE1.md`).
- `07-tasks.md` — **rewritten** as the M4 task list T1–T8 (old copy → `_archive-phase1-landed/07-tasks.PHASE1.md`); Phase-1 beads B1–B12 retired.
- `PHASE-1-DESIGN.md`, `TEST-PLAN*.md`, `WORKER-SETUP-macos.md`, `REQUIREMENTS.md`,
  `BRAINSTORM.md`, `RESEARCH-NOTES.md`, `PHASE-2-3-ROADMAP.md` → moved to `_archive-phase1-landed/` (top-level copies deleted).
- `02-analysis.md` — left in place (still valid; Phase-1-oriented).
- Status advanced to **`ready`** (2026-07-16). The M4 design is authored onto the as-built M2/M3 seams
  with the six operator-locked decisions as durable constraints; per-component change-specs are written
  by the implementer at build time (T1–T8 in `07-tasks.md`). No design-freeze hold remains — the forks
  that gated it (F1 billing, F2 execution-seam shape, F4 push relocation) are resolved by the locked
  decisions + `03-components.md`.

## `remote-substrate-phase2` disposition

Left intact on its own bench (`.kerf/works/remote-substrate-phase2/`). It is not promoted to a
work of its own in the revamp phase map; its container design is future-transport input to M4's
follow-on. No files deleted.
