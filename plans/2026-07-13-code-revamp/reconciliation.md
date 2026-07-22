# Code-Revamp — Kerf Reconciliation (rebase-vs-supersede)

> Advisory audit, 2026-07-13. Read-only pass over the existing kerf works that overlap the
> post-P1 revamp phases (M2/M3/M4/M5 + Tracks B/C), per ROADMAP §"Kerf reconciliation checklist".
> **No kerf work or bead was modified.** Recommendations only. Daemon intentionally OFF.
>
> Evidence sources: `kerf show <codename>` (pass status + bead counts), the bench artifacts under
> `~/.kerf/projects/gregberns-harmonik/<codename>/`, and in-tree greps cited inline.

## 1. Decision table

| Work | Pass / status | Beads | Maps-to phase | Overlap | RECOMMENDATION | One-line rationale |
|---|---|---|---|---|---|---|
| `remote-substrate` | analyze | 0 attached (P1 **code landed**) | **M4** | full | **REBASE onto revamp framing** | The remote channel itself; SSHRunner + `runner!=nil` dual-paths already in-tree are the exact M4 collapse target. |
| `remote-substrate-phase2` | problem-space | 0 attached (full P2 plan written) | **M4** (partial) | partial | **SUPERSEDE-and-fold-into M4** + **NEEDS-OPERATOR-CALL** on seam + container scope | Multi-worker/transport work folds into M4; its DEC-A (reject `handler.Substrate`) conflicts with the revamp; container/egress is out-of-M4 scope. |
| `subsystem-proofs` | problem-space (stale) | 12 / 12 closed | ROADMAP says M5 — **MISMATCH** | tangential | **KEEP-as-is (independent, DONE)** | Naming collision: it is per-subsystem *test lanes* (DOT/promote-reconcile/br-adapter), not the god-package breakup. M5 is net-new. |
| `quality-system` | tasks (stale) | 15 / 15 closed | acceptance oracle (not Track C) | full (oracle) | **KEEP-as-is (independent, DONE)** | `core-loop-proof` matrix harness IS the census Acceptance Oracle — the DOGFOOD gate. Already built; do not rebuild. |
| `validation-net` | ready | 1 labeled / closed (SPEC lists 13 VN beads) | protective net (ROADMAP "turn on the net") | full (scenario tier) | **REBASE onto revamp framing** | The scenario/integration safety net the M-phases lean on; content is exactly right, just predates the phase names. |
| `testing-strategy-uplift` | integration (stalled) | 0 beads | ROADMAP says M1 — **mis-mapped** | partial | **SUPERSEDE** | Stalled 2026-05-20 umbrella; already superseded by `validation-net`. Carve: coverage-gate+lint → Track C, taxonomy → M1. |

Net for the two genuinely new codenames: **M3 `run-state-machine`** and **M2 `agent-input-substrate`**
are confirmed net-new (§4). Both currently exist as `spec.yaml`-only stubs (0 beads, no problem-space).

## 2. Carry-forward / next-action detail (REBASE & SUPERSEDE works)

### `remote-substrate` — REBASE onto M4
Phase-1 code **shipped**: `grep` finds the `SSHRunner`/`CommandRunner` seam in `internal/workspace/`
and `internal/lifecycle/tmux`, and the `runner != nil` dual-paths in `internal/workspace/remotematerialize.go`
and `worktreepath.go` — i.e. the precise thing M4 exists to collapse. **Carries forward:** the seam
itself, `WORKER-SETUP-macos.md`, the auth/billing constraint analysis (OAuth-token vs API-credit,
the make-or-break for the whole branch), and `PHASE-1-DESIGN.md`. **Stale:** the "add remote placement
capability" framing predates the revamp's "rebuild the ack-free channel behind the *proven* substrate
seam + remove the embedded flock" framing, and M3's merge-queue is now a **hard dependency** (ROADMAP
phase map). **Next kerf action:** re-open `remote-substrate` at problem-space and rebase it as the M4
problem-space — start from the landed seam, add the M3-merge-queue dependency, and carry the two census
orphans the ROADMAP homes here (STEP-0c honest-probe `createworktree.go` guard; instrument the
event-dark `internal/workspace` *before* rebuild).

### `remote-substrate-phase2` — SUPERSEDE-and-fold-into M4 (partial)
A complete, adversarially-reviewed Phase-2 plan (13 components D1–D13). **Carries forward into M4:** the
multi-worker registry + scheduler (D6–D8, `workers.yaml` v2, lift `ErrTooManyWorkers`), the
`IsTransportFailure` transport-neutral classifier (D2), and the Linux-SSH worker (D10). **Stale / in
conflict:** it explicitly **rejected `handler.Substrate`** (its DEC-A) and locked `tmux.CommandRunner`
as the single seam — the revamp M2/M4 build *on* `handler.Substrate`, so that decision reverses (operator
call §3). **Out of M4 scope:** container bead-isolation + two-phase egress (G1/G3, D3/D4/D9) are a separate
hardening concern, not the "rebuild the channel" work. **Next kerf action:** fold the multi-worker +
transport-classifier components into the rebased M4; split container-isolation/egress into its own parked
work pending the operator scope call; do not keep `remote-substrate-phase2` as a live standalone.

### `validation-net` — REBASE onto revamp framing
Status `ready` with an assembled SPEC. Its SPEC already declares **"Supersedes `testing-strategy-uplift`"**.
**Carries forward essentially whole:** the `RunConcurrentMerge(t,N,twin)` fixture, the watchdog-engaging
twin, the flagship N≥3 concurrent-dispatch guard (acceptance: reverting `53ead2aa` makes it FAIL),
commit_gate liveness/efficacy, the CI **run lane** where none exists, and restoration of the 21
`-short`-quarantined E2E tests. This is precisely the ROADMAP's "turn on the protective net" step —
complementary to Track C's mechanical linters and to `quality-system`'s oracle, not a duplicate of either.
**Next kerf action:** re-home `validation-net` as the revamp's protective-net workstream (runs in parallel
from day one, like Track C); **verify its bead attachment** — only `hk-d5twq` carries the
`codename:validation-net` label though the SPEC references 13 VN beads (VN1–VN13); the label set needs
reconciling or the VN IDs re-verified before it is treated as scoped-and-tracked.

### `testing-strategy-uplift` — SUPERSEDE
Stalled at `integration` since 2026-05-20 with **0 beads** (never decomposed). It is an 8-track umbrella
that has since been carved by two successors: `validation-net` (scenario/integration net — and it names
this supersession explicitly) and `quality-system` (the oracle). **Harvest, don't resurrect:** goal 1
(the missing `scripts/coverage-gate.sh` + populated `coverage.baseline`) and goal 5 (the disabled/stale
golangci-lint rule audit) are **Track C** enforcement items; its documented 5-layer taxonomy (unit/
integration/scenario/crash/property) is the reference for **M1**'s L0–L3 test-theater classification.
**Next kerf action:** mark `testing-strategy-uplift` superseded; lift goals 1 + 5 into the Track C
checklist; cite its taxonomy from the M1 scope note. Do not re-open it as a work.

## 3. Operator decisions required (with recommended defaults)

These are the ~3 genuine rebase-vs-supersede calls the handoff flagged. Each has a default so the operator
can confirm rather than deliberate.

1. **Remote seam conflict — reverse `remote-substrate-phase2` DEC-A?** Phase-2 locked
   `tmux.CommandRunner` as the sole seam and *rejected* `handler.Substrate`; the revamp M2/M4 are built
   behind `handler.Substrate`. **Recommended default: adopt the revamp's `handler.Substrate` seam; demote
   `CommandRunner` to the transport-adapter layer beneath it.** (Confirm before M4 problem-space is written.)

2. **Container-isolation + egress — in M4 or its own parked work?** Phase-2's headline (G1/G3: per-bead
   Linux container + two-phase egress) is orthogonal to "rebuild the ack-free remote channel." **Recommended
   default: split it OUT of M4 into a separate later hardening work; M4 stays the channel rebuild + dual-path
   collapse + multi-worker scheduling.**

3. **Formally supersede `testing-strategy-uplift`, and confirm the census-PLAN relationship (PLAN §8.6).**
   **Recommended default: supersede `testing-strategy-uplift` (validation-net already declares it) and carve
   as in §2; keep the census PLAN as the diagnosis and the revamp PLAN as the how (not a hard supersede of
   the census).**

Two lower-stakes ROADMAP corrections (findings, not calls — no operator action needed, just fix the doc):
`subsystem-proofs → M5` should be **dropped** (it is done test-lanes, not the god-package breakup — M5 is
net-new); and `quality-system` should be tagged as **the acceptance oracle / DOGFOOD gate**, not a Track C
enforcement source (Track C — linters/coverage/depguard — has no existing-work owner and is direct work).

## 4. No-overlap confirmation for M3 and M2

**M3 `run-state-machine` — CONFIRMED net-new.** No existing work extracts `beadRunOne` or splits
`mergeMu` into an explicit merge queue. Two *partial adjacencies* to cross-reference in its problem-space
(so it does not reinvent them): (a) **`stall-sentinel`** (status `tasks`) — deterministic stall/wedge
detection + tiered escalation; its motivating "silent run hang" and "review-loop wedge, no `run_completed`"
classes are the operational outside of M3's SR9-analog **bounded-liveness invariant** — reuse its stall
signatures, don't redefine them. (b) **`reap`** (status `ready`) — remediating orphan sweep + boot-reconcile
of `dispatched` queue items whose run did not survive; that is the recovery side of the run lifecycle M3
owns. Non-overlaps ruled out: `named-queues` (12/12 closed) is *work* queues, not the *merge* queue;
`bead-ledger-worktree-merge` is the `.beads/issues.jsonl` ledger merge, a different "merge" than mergeMu.

**M2 `agent-input-substrate` — CONFIRMED net-new.** No existing work rebuilds the agent-input channel
behind a real protocol driver. Cross-reference in its problem-space: (a) **P1 `session-restart-substrate`**
— the *proven* `handler.Substrate` seam M2 builds behind, and the keeper (a paste-inject consumer) must be
coordinated with M2's deletion of `pasteinject`/`tmuxsubstrate`; (b) **`remote-substrate-phase2` DEC-A**,
which rejected `handler.Substrate` — M2 reverses it (operator call §3.1). `handler-pause` (17/17 closed)
and `keeper-redesign` (42/42 closed) touch the same substrate but own pause-policy and the restart cycle
respectively, not the input protocol — tangential.
