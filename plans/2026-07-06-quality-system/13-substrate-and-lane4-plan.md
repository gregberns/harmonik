# Scratch-substrate + Lane-4 lightweight-subsystem batch — kerf-ready lane designs

*Planning subagent → admiral/captain · quality-system initiative · 2026-07-06 · PLANNING ONLY (no kerf
works / beads / branches created here). Purpose: keep the overnight max-parallelism pipeline full — with
gb-mbp remote re-enabled (up to ~10 daemon slots), stage two more buildable lanes so the captain can staff a
crew the moment slots free.*

> **Verdict up front.** BOTH lanes are **buildable NOW, in parallel, with NO dependency on alia's coreloop
> assertion library (hk-1yxhh / Phase-1 T2)** — because neither drives the live daemon spawn→verdict loop
> the assertion library abstracts. They are **infra + pure-unit** surfaces.
> - **`codename:scratch-substrate`** — Layer-0 Docker container + disk-pressure DIAL. Greenfield infra
>   (a Dockerfile + a disk-dial launcher), reusing `scratch-daemon.sh` guard rails. **Buildable now.**
> - **`codename:subsystem-proofs`** — the Lane-4 lightweight batch (DOT verdict-parsing, promote/reconcile on
>   temp git, br-adapter on temp `.beads/`). Every seam is already pure or projectDir-injectable; no Docker,
>   no twin, no coreloop. **Buildable now, and file-disjoint** from the dispatch (alia), keeper, and comms
>   lanes.
> Hand the captain **`codename:subsystem-proofs` first** (pure Go/shell, codex-path, zero infra risk, fills
> the most slots fastest with disjoint sub-beads); **`codename:scratch-substrate` second** (needs Docker on
> the build host — one prerequisite check gates it).

---

## Grounding — what already exists (confirmed in-repo)

| Piece | Location | Status |
|---|---|---|
| Throwaway isolated daemon (guard_path, assert_not_supervised, `init/build/up/down/cycle/batch/feedback`) | `scripts/scratch-daemon.sh` | **Exists**, ~80% of the substrate primitive |
| Scratch smoke rig | `scripts/smoke-scratch.sh` | Exists |
| **No Dockerfile anywhere** | — | **Greenfield** (this lane's core deliverable) |
| review.json → verdict parser (pure, `ErrMalformed` salvage) | `internal/workspace/reviewverdict.go` — `parseReviewVerdict([]byte,string)`, `retryVerdictReadOnMalformed`, `ReadReviewVerdict*` | Exists; **pure**, well-tested |
| DOT-graph parse + validate (pure) | `internal/workflowvalidator/dotparser.go` `parseDOT`, `validator.go` `PreRunValidator.Validate(dotSrc)` | Exists; **pure over injected interfaces** |
| promote (push + PR mode) | `cmd/harmonik/promote_cmd.go` — `runPromotePush` (L265), `runPromotePR` (L444); `--project` = git `-C` seam | Exists; **temp-git-repo test fixture already proven** (`promote_cmd_hkpk3p1_test.go`) |
| reconcile (close merged in_progress) | `cmd/harmonik/reconcile.go` `runReconcileSubcommandIO`; scanner `internal/lifecycle/orphansweepbeads.go` `GitMergeCommitScanner.HasMergeCommitForBead` | Exists; scanner **untested against a real repo** (only faked) |
| br adapter (read + terminal-transition writes) | `internal/brcli/adapter.go` `NewForProject(brPath, projectDir)`; `terminaltransition_bi010.go` `ClaimBead/CloseBead/ReopenBead/ResetBead` | Exists; **`NewForProject` is the temp-`.beads/` seam**; real-`br`-into-`t.TempDir()` pattern already used in `internal/daemon` (`t5_realdb_concurrent_test.go`) |
| Stranded in_progress claim-flood auto-reset (hk-l2xd1) | `internal/daemon/workloop.go` (calls `strandedInProgressResetter.ResetBead`), delegates to `brcli.ResetBead` | Exists; daemon-side, tested with a fake resetter |

---

# LANE A — `codename:scratch-substrate` (Layer-0 Docker + disk-pressure dial)

**Epic branch.** `epic/scratch-substrate` (aligns with `03`/`06` chunk-3 naming). **Bead label:**
`codename:scratch-substrate`. **Size:** M, splittable. **Path:** all NEW files under `deploy/testbed/` +
`scripts/`; no edits to production daemon source.

## What it provides
A **disposable, disk-dialable containerized substrate** the daemon runs *inside*, so the disk-pressure
incident class becomes a reproducible green/red scenario instead of a production-only surprise:
- **Clean reset** — an identical throwaway image per scenario run (kills today's cross-test contamination
  under fleet load; valuable even before any twin).
- **Disk DIAL** — a tmpfs/quota-sized rootfs (or build-cache mount) small enough to force `ENOSPC`, which
  drives the **C5 disk cache-wipe → reactive-reap → clean-retry** path (hk-5uezz `go clean -cache` mid-build
  wipes the SHARED cache; hk-44ab2 merge-gate cold-cache retry). The dial proves recovery is *reactive +
  scoped*, not a hard `merge_build_failed`.

## Greenfield vs reused
- **Greenfield:** the `Dockerfile` (disposable image: Go toolchain + `br` + `git` + the harmonik binary) and
  a `scripts/substrate-up.sh` launcher that (a) builds/runs the image, (b) sizes the disk dial, (c) invokes
  `scratch-daemon.sh` *inside* the container, (d) tears down + resets. Plus one ported disk-cache-wipe
  scenario fixture at repo-root `scenarios/<group>/<name>.yaml`, registered in
  `internal/scenario/conformancecorpus_test.go`.
- **Reused, do NOT rebuild:** `scripts/scratch-daemon.sh` in full — its `guard_path` (scratch≠fleet) and
  `assert_not_supervised` rails run *inside* the container so the production/fleet daemon is never touched;
  its `batch`/`feedback` verbs give structured pass/fail out of the box.

## Platform scope — do NOT overclaim fidelity
The image is **Linux**; production runs on the local Mac mini (**darwin**). The harness already carries a
`networksandbox_darwin.go` vs `_linux.go` split. The **disk dial (shared go-build-cache wipe → reactive
reap) is platform-portable**, so this lane validates the **disk-dial MECHANISM on Linux** and explicitly does
**not** claim darwin fidelity. CPU / network / clock dials are **stubbed extension points only** (Phase-3
`adversarial-corpus` builds them out) — do not build them here.

## Parallel-vs-gated verdict
**BUILDABLE NOW, in parallel — infra, no dep on alia's assertion library.** The container + disk-dial +
clean-reset are pure environment plumbing; the disk-cache-wipe scenario asserts the daemon's *own* recovery
(reap + retry → not a hard fail) via `scratch-daemon.sh batch`'s existing pass/fail summary and the event
stream — it does NOT consume the coreloop assertion package. **One prerequisite gate:** Docker (or an OCI
runtime) must be available on the chosen build host. If the assigned crew's box lacks it, route this lane to
a host that has it, or stage it behind that check — otherwise fully parallel with everything.

> Note the `06` framing said chunk-3 "depends on chunk 1." That is a *sequencing convenience* (so there is a
> loop to run inside the container), **not** an assertion-library coupling: the disk-dial recovery scenario
> asserts fail-safe recovery from the event stream, which is stable today. The container/dial/clean-reset
> **build** does not touch the coreloop package at all and can proceed now; only if the crew wants to run a
> *twin* inside the container would it wait on Phase-2 chunk-2. Keep the disk-dial scenario as the acceptance
> target and this lane is unblocked.

## Tranched kerf tasks
1. **A1 — Dockerfile (disposable image).** `deploy/testbed/Dockerfile`: pinned Go toolchain, `git`, real
   `br` on PATH, build+embed the harmonik binary from the mounted checkout. Non-root, reproducible, no fleet
   state baked in. *Done:* `docker build` yields an image that runs `harmonik --help` and `br --version`.
2. **A2 — `scripts/substrate-up.sh` launcher (clean reset).** Build/run the image, bind-mount a scratch
   clone, invoke `scratch-daemon.sh init/up` *inside* the container, expose `down`/`reset` that guarantees an
   identical image per run. Reuse `scratch-daemon.sh` guard rails; never target the fleet daemon. *Done:*
   two back-to-back runs from clean reset show **zero cross-test contamination**.
3. **A3 — disk DIAL.** Size the container rootfs / build-cache mount (tmpfs quota) to force `ENOSPC` on
   demand; expose a `--disk=<size>` knob. *Done:* the dial provably triggers cache-wipe pressure.
4. **A4 — disk-cache-wipe scenario port (C5).** Fixture at repo-root `scenarios/<group>/disk-cache-wipe.yaml`
   asserting the reactive-reap + clean-retry recovery (hk-5uezz / hk-44ab2) → run completes, NOT
   `merge_build_failed` hard fail; register in `internal/scenario/conformancecorpus_test.go`. *Done:* scenario
   runs green on the substrate with **zero Claude tokens**, repeatably across a clean reset.
5. **A5 — stub CPU/net/clock dials as documented extension points.** Named hooks + a comment pointing to
   Phase-3; do **not** implement. *Done:* extension points documented, not built.

Assessor gate fires at the `epic/scratch-substrate → main` boundary (one human PR).

---

# LANE B — `codename:subsystem-proofs` (Lane-4 lightweight-subsystem batch)

**Epic branch.** `epic/subsystem-proofs`. **Bead label:** `codename:subsystem-proofs`. **Size:** M,
splittable into three file-disjoint sub-batches (one per subsystem). **Path:** all Go test files beside their
targets — no Docker, no twin, no coreloop, no scratch-daemon.

**File-disjointness (confirmed):** targets are `internal/workspace`, `internal/workflowvalidator`,
`cmd/harmonik/{promote_cmd,reconcile}`, `internal/lifecycle/orphansweepbeads`, and `internal/brcli` — **none
overlap** the dispatch lane (alia: `internal/daemon` loop + greenfield `internal/coreloop`), the keeper lane
(`internal/keeper`), or the comms lane (`internal/commscursor` / `commsrecvhandler`). Three crews could run
these three sub-batches concurrently with zero merge contention.

## B1 — DOT verdict-parsing + DOT-graph validation
- **What to test.** (a) review.json → verdict: schema-version / verdict-enum / flags / non-empty-notes
  validation, and the **`ErrMalformed` mid-write salvage** (hk-vv10r ssh-cat partial write → bounded-backoff
  retry, not crash / not silent no-commit). (b) DOT-graph: malformed-DOT rejection, node-attr checks, ref
  resolution, reachability, cycle bounds.
- **Cheapest faithful harness.** **Pure in-process unit tests** — `parseReviewVerdict([]byte, target)` and
  `PreRunValidator.Validate(dotSrc)` take bytes/strings and return a verdict/error; `retryVerdictReadOnMalformed`
  takes an injected `verdictRead` func (fake a partial-then-complete write). **No files, no daemon, no
  fixtures beyond byte literals.** Extends the existing `reviewverdict_*_test.go` /
  `malformed_dot_corpus_test.go` beds; this lane's job is to turn scattered cases into a **repeatable
  acceptance + corpus-regression suite** for the whole class.
- **Acceptance scenarios from the corpus (C6 boundary/wire-format).** Malformed review.json mid-write →
  salvage-not-crash (hk-vv10r); every verdict enum round-trips; a truncated JSON prefix at N byte
  boundaries never yields a false APPROVE; malformed DOT corpus rows each reject with the right code.
- **Buildable-now verdict.** **YES — pure unit, zero infra, zero token, zero coreloop dep.** The single
  easiest slot-filler; hand it first inside this lane.

## B2 — promote / reconcile on a temp git repo
- **What to test.** (a) **promote push-mode** (`runPromotePush`): cherry-pick SHAs onto `origin/<target>`,
  `Harmonik-Bead-ID` trailer stamping (explicit / auto-detect / none), the go-build/vet gate, and the
  **non-ff rebase-retry** race path (promote non-ff race, from memory bank). (b) **reconcile**
  (`GitMergeCommitScanner.HasMergeCommitForBead`): a commit bearing the bead trailer on the target branch is
  detected and the bead closed; a bead with NO merge commit is left open (guards the false-close class —
  hk-vbv3b/whru3 rebase-dropped-commits reopen family).
- **Cheapest faithful harness.** **A throwaway git repo — NO Docker, NO twin.** The fixture already exists:
  `promote_cmd_hkpk3p1_test.go`'s `setupPromoteRepo` builds a bare `origin` + local clone with a `go.mod` and
  drives real `git` via `--project <tempRepo>`. Reuse it verbatim. For reconcile, drive the *scanner*
  directly against a temp repo with a trailer-bearing commit (the scanner is currently only faked — this
  lane adds its first real-repo coverage); the full `reconcile` subcommand additionally needs `br` on PATH,
  so fold the temp-`.beads/` rig from B3 for the end-to-end close.
- **Acceptance scenarios from the corpus.** promote non-ff race → rebase-retry succeeds, no lost commit;
  bead-ID trailer present on the promoted commit; reconcile closes ONLY beads with a real merge commit
  (rebase-dropped-commit bead stays open, not false-closed).
- **Buildable-now verdict.** **YES — temp git repo, fixture pattern already proven.** No coreloop, no Docker.
  PR-mode (`runPromotePR`) needs `gh` — either stub `gh` on PATH or scope this sub-batch to push-mode +
  the PR-mode arg-construction unit test (leave live-`gh` out).

## B3 — br-adapter on a temp `.beads/`
- **What to test.** The `brcli.Adapter` contract against a **real `br` binary** on a throwaway store:
  read ops (`Ready`, `ShowBead`, `ListInFlightBeads`, `ListBeadsByStatus`) and the **terminal-transition
  writes** (`ClaimBead` open→in_progress, `CloseBead` in_progress→closed, `ReopenBead`, `ResetBead`) with
  their BI-030 intent-log fsync + bounded retry + serialization under `terminalMu`. Prove the
  **daemon-owns-terminal-transitions** policy (beads-owned sentinel) and the **stranded-in_progress reset**
  primitive (`ResetBead`, the hk-l2xd1 claim-flood auto-reset building block — the daemon-loop wiring stays
  with alia's lane; this lane proves the adapter primitive it calls).
- **Cheapest faithful harness.** **`br init` into a `t.TempDir()` + `NewForProject(brPath, tmpDir)`** — no
  Docker, no daemon. The seam and pattern already exist (`internal/daemon/t5_realdb_concurrent_test.go`:
  `LookPath("br")` + `t.Skip` if absent, `br init` in a tempdir, seed a bead). Today `internal/brcli` tests
  are almost all mock-shell-script binaries; this lane adds the **real-`br`-against-temp-`.beads/`** acceptance
  layer directly in `internal/brcli` (guarded by the `LookPath("br")` skip so CI without `br` still passes).
- **Acceptance scenarios from the corpus (C7 field-fidelity adjacency + BI policy).** Concurrent
  `CloseBead` on the same bead serializes (no double-close); a claim on an already-in_progress bead is
  safe; `ResetBead` returns a stranded in_progress bead to open (hk-l2xd1 primitive); an agent-issued
  terminal write is refused by the beads-owned sentinel.
- **Buildable-now verdict.** **YES — temp `.beads/` + real `br`, guarded by a PATH skip.** No coreloop, no
  Docker. Note: `queue-submit field-fidelity` (the *daemon-side* rpc-rebuild drop, hk-u6zp/y3o51) is the
  substrate-bound S1/S8 overlap — it is **NOT in this lane**; it stays gated on alia + the substrate. This
  lane is strictly the adapter-on-temp-store surface.

## Lane-B tranche summary
- **B1 (DOT verdict-parsing)** — pure unit, hand FIRST.
- **B2 (promote/reconcile on temp git)** — temp git repo, proven fixture.
- **B3 (br-adapter on temp `.beads/`)** — `br init` in tempdir, PATH-skip guarded.
- All three **file-disjoint** from each other and from dispatch/keeper/comms → up to 3 concurrent crews, or
  one crew serially draining B1→B2→B3. Assessor gate at `epic/subsystem-proofs → main`.

---

## Hand-off to the captain (staffing order)

1. **First:** `codename:subsystem-proofs` — pure Go/shell, codex-path (Claude cap ~98%, pi blocked),
   zero infra risk, three disjoint sub-batches that fill the most free slots immediately with no
   cross-lane merge contention. Branch `epic/subsystem-proofs`.
2. **Second:** `codename:scratch-substrate` — buildable now too, but gated on **Docker present on the build
   host**; route to a Docker-capable box or stage behind that one check. Branch `epic/scratch-substrate`.
3. **Neither waits on alia** (hk-1yxhh coreloop assertion library). The only truly-gated work remains the
   substrate-BOUND acceptance rows (S4 egress, S5 agent_ready/tunnel, S6 loop-emergent, S8 queue-submit
   field-fidelity) — do NOT pull those forward; they stay on the Phase-2/3 gate per `10`/`06`.
4. Every epic → main is one human PR after the assessor `found-by:*` gate PASSes.
