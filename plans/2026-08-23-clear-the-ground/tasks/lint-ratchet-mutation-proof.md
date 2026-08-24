---
id: lint-ratchet-mutation-proof
title: Prove the re-keyed ratchet still refuses a genuinely new tolerated finding
type: task
priority: 0
labels: [lint, gate, test-quality, clear-the-ground]
depends_on: [lint-rekey-exclusion-list]
blocks: []
workstream: W1
batch: 1
---

## Problem

Re-keying the exclusion list off file paths (`lint-rekey-exclusion-list`) removes the false positive
that blocked file moves. It could just as easily remove the true positive the ratchet exists for. A
gate that stops firing is indistinguishable from a gate that has nothing to fire about, and this
repo has shipped that failure before: `scripts/secret-scan.sh` carries a header recording that it
failed open because it piped a diff into `grep -q`.

## Scope

`scripts/lint-allow-ratchet-test.sh`, extended with deliberate mutations against a scratch
repository.

Each mutation must be a real edit that produces a real new finding, run through the real gate.

## Done when

Every one of these is proved to FAIL the gate, each as a named test case:

1. A new tolerated finding added to the list directly, uncommitted (working-tree window).
2. The same, committed (HEAD-against-parents window).
3. A swap — one entry deleted and a different one added in the same edit. The set size is unchanged,
   so a count-based check would pass.
4. A file moved AND its content changed so a new finding appears in the moved file. The move must
   not launder the new finding.
5. A finding that moves from one symbol to another within the same file.

And these are proved to PASS:

6. A pure `git mv` between packages, no content change.
7. A file renamed in place, no content change.
8. An entry deleted because the finding was fixed.

## Limits

- No mocking of the gate. Each case runs `scripts/lint-allow-ratchet.sh` as the build runs it.
- A test that only asserts an exit code without asserting the reported entry is not enough — the
  gate must name what it caught, or a later refactor can make it right for the wrong reason.
