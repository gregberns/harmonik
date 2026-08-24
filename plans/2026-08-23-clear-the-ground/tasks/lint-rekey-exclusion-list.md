---
id: lint-rekey-exclusion-list
title: A tolerated lint finding should be identified by what it is, not by where the file sits
type: task
priority: 0
labels: [lint, gate, clear-the-ground]
depends_on: []
blocks: [lint-ratchet-mutation-proof, core-cluster-map, runregistry-extract, harnesspick-extract, spendmeter-extract, handlerpause-extract]
workstream: W1
batch: 1
---

## Problem

`tools/lintreport/allow.txt` holds 580 tolerated findings, each keyed `<path> <linter>`.
`scripts/lint-allow-ratchet.sh` refuses any key that was not there before. Moving a file changes its
key, so a pure move — no content change — reads as new debt and the build stops. This is what has
blocked every package extraction in the program.

A rename-aware version of the ratchet was written and reverted on 2026-08-23: an adversarial review
found three working routes to forge a rename and mint a free exemption (a forged R100 rename over
empty blobs, a count-credit swap, and an abandoned-path pre-grant), plus a `LINT_ALLOW_LIST` override
that passed on a shell script.

**Operator ruling, 2026-08-23: change the rule rather than teach the ratchet to detect renames.**

## Scope

- `tools/lintreport/allow.txt` — the key format.
- `scripts/lint-allow-ratchet.sh` — the `pairs()` function and both comparison windows.
- `scripts/lint-allow.sh` — the judge that reads the same list.
- `scripts/lint-allow-ratchet-test.sh` — the self-test.

Re-key each entry so it names the finding, not the location: the linter plus the enclosing symbol
(package-qualified function, method or type). A file that moves between packages with no content
change must produce a byte-identical key.

## Done when

1. A real `git mv` of any file carrying a tolerated finding, with no content change, leaves the key
   set byte-identical and the ratchet passes. Proved by a test that performs the move.
2. The ratchet still fails when a genuinely new tolerated finding is introduced. See
   `lint-ratchet-mutation-proof`.
3. `make fast` is green on the re-keyed list with the same 580 findings tolerated — this task
   changes how they are named, not which ones are tolerated.

## Limits

- **Do not re-attempt rename detection.** No `git diff -M`, no similarity index, no path history.
  That approach is the one that was reverted and its three forgery routes are on record.
- **Do not tolerate anything new.** The count may fall in a later task; it may not rise here.
- Do not remove the `LINT_ALLOW_LIST` override without checking the self-test still has a way to
  point at a scratch list — but do close the hole where it passes on a non-Go file.
