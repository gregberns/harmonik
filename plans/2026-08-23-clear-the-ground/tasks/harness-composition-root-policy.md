---
id: harness-composition-root-policy
title: Extracting harness selection reverses a policy the freeze gate states in writing
type: task
priority: 1
labels: [daemon, extraction, needs-operator, clear-the-ground]
depends_on: []
blocks: [harnesspick-extract]
workstream: W2
batch: 3
status: NOT READY — needs a ruling before harnesspick-extract can start
---

## Problem

`harnesspick-extract` moves harness selection out of `internal/daemon`. Two files in that cluster are
currently in the daemon **on purpose**, and the reason is written into a gate:

`scripts/harnesspi-freeze-gate.sh` lines 25–33 say `pi_profile_resolve.go` is a deliberate exception
that stays in `internal/daemon` — *"CLAIM-TIME daemon wiring, not harness impl… Moving it would drag
the whole projectconfig type-family out of the daemon"* — and excludes it by exact name at line 57.
Lines 41–42 say *"`newHarnessRegistry` stays in internal/daemon on purpose: the daemon is the
composition root."* `Makefile:406` repeats the note.

The gate will not go red on the move, because it only scans `internal/daemon`. So the extraction can
proceed silently against a stated policy, which is the worst of the available outcomes.

## The decision

Either:

- **The composition-root policy still holds** → `harnesspick-extract` must exclude
  `pi_profile_resolve.go` and `newHarnessRegistry`, and its scope shrinks accordingly. The extraction
  is still worth doing for the other four files.
- **The policy is superseded** → it is reversed explicitly: the gate comments and `Makefile:406` are
  rewritten in the same commit that moves the files, stating what changed and when.

The claim to test before deciding: does moving `pi_profile_resolve.go` actually drag the
`projectconfig` type family out of the daemon? That was true when the note was written. The survey
found the cluster's import closure is `projectconfig` among others, so the note may still hold — but
"may" is not a decision.

## Evidence, gathered 2026-08-23 evening

The claim was tested. **It was true when it was written and it stopped being true two days later.**

- The gate was written 2026-07-22. On that day `PiHarnessConfig` and `PiProfileConfig` were declared
  in `internal/daemon/projectconfig.go`, and `pi_profile_resolve.go` did not import
  `internal/projectconfig` at all, because the types were package-local. Moving the file then really
  would have forced the type family out of the daemon.
- On 2026-07-24 the commit "extract projectconfig to a leaf package" moved those types to
  `internal/projectconfig` and rewrote this file's import block to match. **The gate comment has
  described a world that does not exist for 30 days.** Nothing has touched the gate script since
  2026-07-22.

What a move would actually cost today:

- **Nothing has to move with it.** The file's import closure is `projectconfig`, `core`,
  `handlercontract` and `handlercontract/lifecycle` — four leaf packages, none of which import
  `internal/daemon`. No cycle is possible.
- Three daemon-private symbols become exported: `resolvePiProfile`, `emitProviderSelected`,
  `hasSingleModelLabel`. All three are already counted among the extraction's inbound symbols.
- One new import edge, `internal/harnesspick` to `internal/projectconfig`. That is an ordinary
  downward edge with direct precedent — `internal/runloop` has the same one and `.golangci.yml`
  grants it explicitly.

**The exclusion also buys nothing it claims to buy.** `internal/daemon/modelpreference.go` is in the
same move cluster and carries the same `projectconfig` import. Leaving `pi_profile_resolve.go`
behind would not keep that edge inside the daemon.

**The gate cannot catch the move.** Confirmed by reading and by running it against a throwaway copy
with all five files moved out: it reports OK and exits 0, because every check scans `internal/daemon`
or `internal/harness` and a move out leaves no matches anywhere.

### The two halves of the policy are not alike

- **The `projectconfig` half is dead** and provably so. It rests on a fact a named commit deleted.
- **The `newHarnessRegistry` half is real wiring, not inertia.** It constructs the three concrete
  harnesses and binds each to its agent type, once, at work-loop boot. It is also named as the
  composition root in three harness package docs and in two foundation documents, so reversing it is
  a project-level architectural decision and not a gate-comment fix.
- A trap in the current framing: `harnessregistry.go` is 270 lines and `newHarnessRegistry` is 22 of
  them. The other 248 are per-run launch-spec logic that asserts on the concrete harness types, so
  whichever package owns them must import all three harness implementations. That, not the 22-line
  constructor, is what would relocate the composition root.

## Recommended ruling, for the operator to accept or reject

**Supersede the `projectconfig` half; keep the `newHarnessRegistry` half.**

`pi_profile_resolve.go` moves. Rewrite the gate header block and the `Makefile` comment in the same
commit, naming the 2026-07-24 extraction as what changed, and delete the now-dead
`! -name 'pi_profile_resolve.go'` filter rather than leaving it as a hole no stated reason covers.
`newHarnessRegistry` stays in `internal/daemon`, and the question of moving the harness composition
root is deferred out of this ground-clearing program.

Net scope change: four files move as planned, plus `pi_profile_resolve.go`. `newHarnessRegistry`
stays.

## Done when

The ruling is recorded here with a date, and `harnesspick-extract`'s scope is amended to match.

## Limits

- **Do not extract around the policy without reversing it.** A gate comment that describes a world
  that no longer exists is how this repo accumulated the stale guidance the program is clearing out.
- Do not delete the freeze gate to make the conflict go away.
