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

## Done when

The ruling is recorded here with a date, and `harnesspick-extract`'s scope is amended to match.

## Limits

- **Do not extract around the policy without reversing it.** A gate comment that describes a world
  that no longer exists is how this repo accumulated the stale guidance the program is clearing out.
- Do not delete the freeze gate to make the conflict go away.
