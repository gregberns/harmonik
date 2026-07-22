# 01 — Problem Space: no-auto-dispatch

> **STATUS: BACK-FILLED from authoritative source material (planning agent, 2026-07-21).**
> Sources: operator directive in `plans/2026-07-21-platform-architecture/DECISIONS.md` §"Daemon
> auto-dispatch on boot — REMOVE IT ENTIRELY" (lines 154–161); the fully-specified epic bead tree
> `hk-04q2j` + children `.1`–`.5`; and the current normative spec text in
> `specs/execution-model.md §4.11 EM-066/EM-067`, `§7.4`, `§10.1`.
> Epic: `hk-04q2j` (`codename:no-auto-dispatch`, `daemon`). Not a conversation-derived draft —
> the operator decision is already LOCKED; this pass records it.

## Summary (one paragraph)

Booting the daemon used to auto-run the bead backlog: with no operator-submitted queue, the daemon
work loop polled `br ready` and dispatched the first ready bead on its own. That path was already
default-OFF behind a startup-sealed `noAutoPull` flag (queue-only is the default per the `hk-8vy18`
spec flip), but the **code still exists and can be re-enabled with `--auto-pull`**. The operator's
decision (2026-07-21) is to **delete the code path entirely, not merely default it off**: the daemon
is a dumb execution substrate and **only agents decide what runs through it** — never an out-of-date
set of beads the daemon arbitrarily pulls in on boot. This work removes the `br ready` fallback
dispatch block from the work loop, tears out the `noAutoPull`/`NoAutoPull` config-flag-status
plumbing that gated it, migrates every test that leaned on `br ready` as a convenience dispatch
driver onto the queue-submit surface, and retires the now-vestigial machinery (restart-backoff, the
`--auto-pull`/`--no-auto-pull` flags). Because harmonik is spec-first, it also retires the spec
clauses (`execution-model.md` EM-066/EM-067 fallback branch, the §7.4 pseudocode fallback arm, the
§10.1 conformance opt-in) that still sanction the fallback as a legal opt-in.

## Goals (what is true about the system after this change)

1. **No self-start on boot.** A freshly-booted daemon with no operator/agent-submitted queue
   dispatches **zero** runs, spawns no agent subprocess, and claims no bead — permanently, with no
   flag that re-enables auto-pull. It sits idle on the queue-submit wake channel.
2. **The `br ready` fallback dispatch code is gone**, not disabled. `deps.brAdapter.Ready(...)` is
   no longer a dispatch source anywhere in the work loop.
3. **The `noAutoPull`/`NoAutoPull` flag surface is gone** across cmd + daemon + core + scenario
   harnesses (config field, CLI flag, status-payload field, scenario setters).
4. **The test suite is green on the queue surface.** Tests that used `br ready` as a shorthand to
   drive a dispatch now route a submitted queue; the daemon's "queue-only" behavior is what they
   assert.
5. **The spec no longer sanctions the fallback.** `execution-model.md` describes queue-only as the
   *only* topology, not a default with a legal opt-in.

## Non-goals (explicitly out of scope)

- **NOT touching the queue-pull dispatch path.** Crews' actively-submitted named-queue work
  (`workloop.go` ~2119–2723) is PRESERVED — that is exactly the "agents decide what runs" surface.
- **NOT touching boot-time restore of AGENT-submitted queues.** `LoadQueueAtStartup`
  (`lifecycle/startup_pl005_qm002.go`) restores queues that agents previously submitted; that is
  agent intent, not daemon self-start — PRESERVED.
- **NOT touching the sentinel-governor `Ready()` calls** (`workloop.go:1919, 1970`) — those are
  observe-only movement/opportunity reads, they do not dispatch — PRESERVED.
- **NOT re-litigating the queue-only default** (settled by `hk-8vy18`); this goes *further* than
  default-off to full removal.
- **NOT a behavior change for the operator-pause / handler-pause gates** other than deleting their
  now-dead `br ready` copies.

## Constraints

- **Spec-first.** The spec is normative; deleting the code obliges a coordinated amendment of
  `execution-model.md` EM-066/EM-067 + §7.4 + §10.1 so code and spec do not diverge.
- **Big test-migration blast radius.** Many `workloop`/scenario tests use `br ready` as a
  convenience dispatch driver (they stub a ledger whose `Ready()` returns ≥1 bead and pass **no**
  QueueStore). These must migrate to a synthetic/submitted queue or they break when the fallback is
  deleted. Step 1 lands a test-helper shim FIRST to keep the suite green through the deletion.
- **Preserve queue-pull, queue-restore, and sentinel Ready() reads** (see non-goals).
- **Sequenced, not big-bang.** Land the test shim (Step 1) before the primary deletion (Step 2) so
  the suite never goes red.

## Success criteria (concrete, verifiable)

1. Quiet-daemon test: boot with no queue → zero `run_started` over a bounded window; daemon idle on
   the submit-wake channel. (This becomes the *only* boot behavior — no `--auto-pull` variant.)
2. `grep -r noAutoPull\|NoAutoPull` across `cmd/`, `internal/daemon/`, `internal/core/`,
   `internal/lifecycle/`, scenario harnesses returns nothing (or only accepted-but-ignored no-op
   flag stubs, if the operator elects back-compat — see Open decisions).
3. `deps.brAdapter.Ready` has no remaining *dispatch* caller in `workloop.go`; only the
   observe-only sentinel reads at ~1919/1970 remain.
4. Full daemon + scenario test suite passes with the fallback deleted.
5. `execution-model.md` no longer describes a legal `br ready` fallback opt-in.

## Preliminary spec/code areas affected

- `internal/daemon/workloop.go` (primary — the fallback block + `noAutoPull` field).
- `cmd/harmonik/{main.go,usage.go}`, `internal/lifecycle` (supervise/shim, bootstate), the daemon
  `Config`, `internal/core/daemonevents_hqwn59.go` status payload, `scenario/orchdrive.go` +
  `daemon/scenariotest/`.
- `internal/daemon/export_test.go` + the feature tests pinned on the removed fallback.
- `internal/daemon/restartbackoff.go` (vestigial).
- **Spec:** `specs/execution-model.md` §4.11 (EM-066/EM-067), §7.4 pseudocode, §10.1 conformance.

## Open decisions (need a human) — carried into every downstream pass

- **D1 — Retire EM-066/EM-067, or keep as historical?** The spec currently makes queue-only a
  *default* with the `br ready` fallback a *conforming opt-in* (EM-066/EM-067). Full removal means
  queue-only is the *only* topology. Retire the fallback clauses vs. keep them marked "removed at
  vX." (Operator's spec-shape call.)
- **D2 — Keep `--auto-pull`/`--no-auto-pull` as accepted-but-ignored no-ops for back-compat, or
  delete the flags?** (From bead `hk-04q2j.3`/`.5` — operator's call.)
