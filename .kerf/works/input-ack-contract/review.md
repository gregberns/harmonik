# Independent review — input-ack-contract

Reviewer: independent `agent-reviewer` pass over the working diff, the three
pinned specs, the task card, and the evidence file. Not the author.

## Round 1 — REQUEST_CHANGES

```json
{
  "schema_version": 1,
  "verdict": "REQUEST_CHANGES",
  "flags": ["spec-divergence"],
  "notes": "The drift correction itself is correct, strictly in-lease, and well-evidenced: RSM-027 is amended in place with no live Accepted/Degraded, the four routes are total, the input_seq rule matches card item 5, all five owner-spec restatements are corrected with their front-stop composition rule preserved, and the AIS/HC diffs touch nothing outside the leased prose + metadata + one revision row each. One material gap: the new RSM-024 bullet asserts, citing AIS-INV-001, that Ack{Delivered} does NOT resolve the seed - but AIS-INV-001 and HC-INV-008 both name Ack{Delivered} as a terminal that discharges the bounded window, and RSM-027 simultaneously forbids a second timer. As written, the post-Delivered pending state has no owner-spec timer bounding it."
}
```

### Findings and disposition

**F1 — RSM-024 cited AIS-INV-001 as authority for a reading its headline denies.**
Real. Both invariants' headline terminal sets read as `Ack{Delivered}`
discharging the window, while their bodies say the window bounds the wait for
the hook signal. FIXED IN LEASE two ways:

- RSM-024's resume-seed bullet now cites **AIS-003 + AIS-004 + AIS-INV-001** as
  the composite authority, matching how the independent
  `reviewloop-decoupling` draft cited the same reading.
- RSM-027's seam-ownership bullet now names the run-level backstop explicitly:
  the ALREADY-composed RSM-024 timer stack (ready sub-bound, `post_ready_hang`,
  absolute commit-watchdog ceiling) routes a never-terminating `Delivered` to
  RSM-025. RSM-INV-001 is therefore bounded without a second input timer. The
  reviewer's "no timer bounds the pending state" reading missed that RSM-024 is
  a composed stack, not a single input timer.

The residual owner-spec tension (AIS-INV-001 / HC-INV-008 headline vs. body) is
PRE-EXISTING, landed in `045e937b5`, and cannot be fixed inside this lease —
fixing it edits timer semantics, a card "Escalate when" condition. It is now
recorded under `escalations.surfaced_owner_spec_tension` in the evidence file
with a follow-up.

**F2 — evidence said `escalations: none`.** Correct criticism. Replaced with a
structured `surfaced_owner_spec_tension` block naming the exact contradiction,
why it was not fixed here, and what was mitigated in lease.

**F3 (advisory) — version collision with an unlanded draft.** Confirmed:
`.kerf/works/reviewloop-decoupling/05-spec-drafts/run-state-machine.md` is a
committed, kerf-`ready`, never-landed full RSM draft that also claims `0.2.1`,
also rewrites RSM-027, and adds its own `0.2.1` revision row dated 2026-07-24.
Out of lease. Recorded under `collisions_noticed` in the evidence and reported
to the coordinator.

**F4 (advisory) — `input_seq` named without its owner.** Fixed: RSM-027's
correlation bullet now cites `[agent-input.md] AIS-003b` for the sequence id and
`[event-model.md §6.3]` for the serialized field name, keeping RSM clear of
card item 4's "does not redefine the event payload".

## Reviewer checks that passed unchanged

- RSM-027 amended in place, not renumbered; no live `Accepted` / `Degraded` /
  three-valued vocabulary (only the explicit negative sentence and the revision
  row).
- The four routes are total and correct, with RSM-INV-002 cited.
- All five clauses of card item 5's `input_seq` consumption rule are present.
- All five owner-spec restatements corrected with each one's watchdog rule
  preserved verbatim in substance.
- Lease respected: only the three specs modified; no Go, tests, `_registry.yaml`,
  `TASK-INDEX.yaml`, or other task cards touched; `.kerf/project-identifier`
  correctly reverted.
- Version bumps and revision rows correct on all three specs; RSM's new §14 is
  acceptable (spec had none; §13 was terminal; header discloses the table starts
  at 0.2.1; matches the convention in 19 other specs).
- Evidence blob IDs, chronology commits, and both grep claims reproduce exactly;
  the Go claim is true — `internal/handler/input_port.go` already ships the
  binary `DeliveryOutcome`, so production code agrees with the corrected specs.

## Round 2 — APPROVE

```json
{
  "schema_version": 1,
  "verdict": "APPROVE",
  "flags": ["non-go-bead-idiom-na"],
  "notes": "Round-2 re-review. All four Round-1 findings resolved in lease. F1: RSM-024's resume-seed bullet now cites AIS-003 + AIS-004 + AIS-INV-001 as composite authority instead of leaning on AIS-INV-001 alone, and RSM-027 names the run-level backstop (the already-composed RSM-024 ready sub-bound / post_ready_hang / commit-watchdog ceiling routing to RSM-025) so RSM-INV-001 stays bounded with no second input timer. My Round-1 'no timer bounds the pending state' was too strong at the run level -- the ready sub-bound is an independent backstop; the narrower true concern was the input sub-bound's authority, which is now correctly attributed. The residual AIS-INV-001 / HC-INV-008 headline-vs-body tension is verified pre-existing in 045e937b5 and correctly left unfixed: resolving it edits timer semantics, a card 'Escalate when' condition and outside the write lease. F2/F3/F4 fixed. Spec-only bead; Go idiom, production wire-up, and scenario-test checks are N/A."
}
```

Re-verified rather than taken on trust:

- `git show 045e937b5:specs/agent-input.md` contains BOTH the AIS-INV-001
  headline terminal set and AIS-003's "acceptance is confirmed asynchronously"
  sentence — the tension predates this diff.
- RSM-024 sub-bound 2 (resume-to-ready ≤ the effective agent-ready timeout)
  bounds the run independently of the input seam, so a never-arriving async
  terminal cannot wedge RSM-INV-001. The new backstop clause correctly EXCLUDES
  sub-bound 1 from the list, which makes it defense-in-depth rather than
  circular.
- `AIS-003b` resolves to AIS-003's own clause (b), used in the §6.2 `Ack` schema
  comment; `input_seq` is registered in `event-model.md` §6.3 / §8.21.
- Beads `hk-7rlmq` and `hk-ek5gp` exist in the canonical ledger with accurate
  bodies. (`br` must be run from the repo root — the worktree has its own
  `.beads/` and returns ISSUE_NOT_FOUND.)
- Only `specs/run-state-machine.md` changed since Round 1; the agent-input and
  handler-contract blobs are byte-identical to the Round-1 pass.
- Both grep claims in the evidence still reproduce exactly.

Round-2 cosmetic nits, both fixed before commit: two 99-char lines in the
reflowed RSM-027 region rewrapped, and the RSM §14 revision row extended to
mention the run-level backstop sentence and the AIS-003b / event-model §6.3
attribution.
