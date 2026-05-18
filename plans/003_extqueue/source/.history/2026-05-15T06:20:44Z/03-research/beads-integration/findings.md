# beads-integration.md — extqueue research findings

Scope: evidence-gathering for the spec edit defined in `02-components.md §3`.
All line refs are against `/Users/gb/github/harmonik/specs/beads-integration.md`
at HEAD (v0.4.1).

## Questions

1. Where does the spec say (or imply) the daemon polls `br ready`?
2. Does the intent log cover queue submissions, or only ledger writes?
3. What pre-claim validation does the daemon already perform (hk-p4xbw)?
4. Where does ON-015 ("Beads is the queue") get instantiated in BI?
5. Where are `blocks`-edge semantics described and how does `br ready` honour them?
6. Are there `br ready` consumers other than the daemon work loop?

## Findings (file:line)

### Q1 — `br ready` as daemon input

- beads-integration.md:122 (BI-007 last paragraph): "The dispatch loop
  (BI-013 ready-work) MUST exclude `draft`-status beads, which is the
  native semantic of `br ready`."
- beads-integration.md:253–257 (BI-013, header `### 4.5` at line 251):
  "The daemon MUST be able to query the set of beads whose dependencies
  are satisfied and whose status is `open` via `br ready` (or its
  equivalent command). The ready-work query result is the input to the
  daemon's dispatch loop."
- beads-integration.md:262–266 (BI-013a): "The `br`-CLI adapter's
  ready-work query (BI-013) MUST exclude beads carrying a
  `needs-attention` label from the dispatchable set… The exclusion MUST
  be applied at adapter read time so the daemon's dispatch loop never
  observes a `needs-attention`-labeled bead as ready."
- beads-integration.md:383 (BI-024a): version handshake "completes BEFORE
  the step 6 `br ready` query so that step 6 does not run against an
  incompatible `br` version".
- beads-integration.md:404 (BI-025c): "The 5s read timeout aligns with
  PL-005 step 6's `br ready` 5s constraint."
- Cross-spec: process-lifecycle.md:238 PL-005 step 6 — "Query Beads via
  `br ready` for dispatchable beads…" This is the canonical
  daemon-startup hook into `br ready`.
- §4.9 BI-027/BI-028 (lines 437–447) do NOT reference `br ready`; they
  scope the agent-facing skill surface, not the daemon's input.

Net: §4.5 BI-013 is the load-bearing prose; BI-013a, BI-024a, BI-025c
are satellite refs anchoring cadence/ordering.

### Q2 — Intent log scope

- §4.10 header (line 449): "`br`-adapter idempotency — terminal-transition
  writes." Scope is explicitly terminal transitions only.
- BI-029 (line 455): idempotency key is `<run_id>:<transition_id>:<op>`
  with `op ∈ {claim, close, reopen}` — three values, no submit/append.
- BI-030 (line 461): intent file is written "Before invoking `br` for a
  terminal-transition write per BI-029."
- BI-INV-001 (line 522): "No harmonik code path MAY write to Beads at any
  run transition other than (i) the three terminal transitions… or (ii)
  the reconciliation-driven writes in BI-010b."

Conclusion: the intent log covers ONLY ledger writes (claim/close/reopen).
Queue submission is NOT a Beads write; it is a daemon-internal state
mutation. BI-030 MUST NOT be repurposed for queue durability. Queue
persistence is owned by queue-model.md (`.harmonik/queue.json` per
02-components.md §1). The two on-disk contracts sit side-by-side in
`.harmonik/` and are independent.

### Q3 — Pre-claim validation surface today

The spec is silent on the ShowBead guard — no BI-* requirement names it.
The guard lives in code only:

- internal/daemon/workloop.go:227–243 (doc block): "ShowBead — pre-claim
  status guard (hk-p4xbw)": "ShowBead is called between Ready and
  ClaimBead to confirm the bead is still 'open' before dispatching."
- internal/daemon/workloop.go:439–469 (implementation): ShowBead sits
  between Ready() (line 392) and ClaimBead (line 471+). On non-open
  status the loop emits a stderr `bead_claim_skipped` line and
  continues.
- internal/daemon/t5_realdb_concurrent_test.go:121–257 — real-DB
  concurrent-claim exclusion test asserting exactly one `run_started`
  event across two competing loops.

Submit-time validation extends naturally: ShowBead (existence + coarse
status) is the existing primitive.

### Q4 — Where ON-015 lands in BI

- operator-nfr.md:298 ON-015 cites BI §4.1–§4.3 (daemon scope, Beads as
  external authority, Beads-managed data).
- BI itself does NOT name "queue" anywhere. There is no reverse-cite
  from BI to ON-015; the framing is one-way (ON-015 overlays on top of
  BI surfaces).
- The only implicit echoes: §4.4 BI-010a status-mapping table (lines
  200–214) and §4.5 BI-013 (line 253) together constitute the
  "dispatchable queue" surface ON-015 talks about.

Implication: the BI edit per 02-components.md §3 has nothing to delete
re ON-015. The reconciliation is one-way — ON-015 prose changes in
operator-nfr.md (02-components.md §6), and BI's §4.5 BI-013 gets a
demotion-style amendment so the ON-015 cite no longer carries
dispatch-input semantics.

### Q5 — `blocks` edge semantics

- beads-integration.md:110–112 BI-006: "Beads MUST be the source of
  truth for typed dependency edges between beads. The supported edge
  kinds MUST include `parent-child`, `blocks`, `conditional-blocks`,
  and `waits-for`. Harmonik consumes these edges read-only per §4.5."
- §6.1 EdgeKind enum, lines 596–600: includes `blocks` and
  `conditional-blocks`.
- §4.5 BI-013 (line 253): the ready-work filter — "beads whose
  dependencies are satisfied and whose status is `open`" — natively
  excludes blocked beads. The filter lives in `br ready`; the adapter
  does not re-implement it.
- beads-integration.md:240 BI-011: invokes Beads's `blocked_issues_cache`
  as the reason intra-run writes are forbidden — that cache IS what
  `br ready` reads against.
- §4.5 BI-014 (lines 271–273): typed-edge query lets the daemon read
  blockers directly. This is the surface the post-extqueue in-group
  ledger-dep check uses.

Post-extqueue invariant: `br ready`'s `blocks`-respecting filter is no
longer consumed at top of dispatch loop, but the same semantics are
re-realised at queue-dispatch time inside a group via BI-014 (read the
bead's blockers, defer if any are open). execution-model.md per
02-components.md §2 declares the new `queue_item_deferred_for_ledger_dep`
event.

### Q6 — Other `br ready` consumers

Spec corpus:

- beads-integration.md:122, 253, 383, 404 — enumerated above.
- process-lifecycle.md:238 — PL-005 step 6 (daemon-startup dispatch
  hook).
- No other normative spec references.

Code:

- internal/brcli/ready.go:1–131 — adapter primitive `Ready()` plus
  `ErrBrReadyFailed`. Library-level, no policy.
- internal/brcli/timeout.go:22, 50 — applies to `br ready`.
- internal/daemon/workloop.go:392 — sole production caller of
  `brAdapter.Ready()`.
- internal/lifecycle/readystate_pl009.go:26 — comment-only reference;
  no call site.

Operator agents and the orchestrator will keep using `br ready` via
the Beads-CLI skill (§4.9 BI-027); skill surface needs no change.
internal/brcli/ready.go SHOULD remain (orchestrator-side `hk` CLI
may shell into it via the same adapter), but the daemon work loop's
call at workloop.go:392 is removed by the execution-model change.

## Patterns to adopt

- **Demote BI-013, retain the prose.** Amend "input to the daemon's
  dispatch loop" to "input to the orchestrator's planning surface; the
  daemon no longer calls this query as part of dispatch." Add a
  back-reference to the new queue-model.md submission contract.
  BI-013a's `needs-attention` exclusion stays correct as
  orchestrator-side guidance.
- **New §4.5a or §4.4a "Validation contract on submit".** Per
  02-components.md §3 this is a small section: existence (via ShowBead),
  status ≠ closed, not already `in_progress` from external claim, and
  informational `blocks`-edge inspection that surfaces a
  `parallelism_narrowed` notice. The existing ShowBead pre-claim guard
  pattern (workloop.go:439) is the implementation primitive — submit-time
  validation just calls ShowBead per item up-front and aggregates
  failures.
- **Adapter scope unchanged.** §4.8 / §4.8a / §4.10 (adapter contract,
  CLI surface, idempotency) need no edit. Submit-time ShowBead reuses
  the existing `BrError` taxonomy and the 5s read timeout; no new
  primitive.
- **Intent log untouched.** Per Q2, BI-029/BI-030 do not cover queue
  state. Queue persistence is queue-model.md's domain via its own
  atomic temp-rename pattern; both artifacts coexist in `.harmonik/`.

## Risks / conflicts

- **BI-024a / BI-025c cite a `br ready` cadence by line.** Lines 383
  and 404 anchor timing relative to the daemon's `br ready` call. When
  the daemon stops calling `br ready` at startup, these prose fragments
  need re-anchoring — either to the orchestrator-side caller (overreach
  for BI) or to "any `br ready` invocation" (cleaner). PL-005 step 6
  (process-lifecycle.md:238) is the cite to coordinate on; the PL edit
  per 02-components.md §4 drives the BI fix-up.
- **BI-007 line 122's "dispatch loop (BI-013 ready-work)" wording** is
  obsoleted by the demotion. Either re-anchor to "the orchestrator's
  queue-planning surface" or drop the parenthetical.
- **OQ-BI-009 `br audit-log` idempotency-key query** (line 808) is
  orthogonal and unchanged.
- **No ON-015 reverse-cite to delete.** As Q4 found, BI never mentions
  "queue"; the reconciliation lives entirely on the ON side. Confirm
  during edit that no BI prose inherits "queue" semantically.
- **BI-013a `needs-attention` exclusion**: under extqueue, the
  orchestrator is the consumer of `br ready`. If the orchestrator is
  human-facing tooling rather than an automated agent, the
  exclusion-at-read-time guarantee weakens; flag in the demoted BI-013a
  but treat as design-pass-resolvable, not blocking.
