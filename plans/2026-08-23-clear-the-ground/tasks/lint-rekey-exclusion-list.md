---
id: lint-rekey-exclusion-list
title: A tolerated lint finding should be identified by what it is, not by where the file sits
type: task
priority: 0
labels: [lint, gate, clear-the-ground]
depends_on: []
blocks: [lint-ratchet-mutation-proof, core-cluster-map, core-split-by-cluster, runregistry-extract, harnesspick-extract, spendmeter-extract, handlerpause-extract, cli-extract-logic]
workstream: W1
batch: 1
---

## Problem

`tools/lintreport/allow.txt` holds 575 tolerated findings, each keyed `<path><TAB><linter>`.
(The file is 580 lines; five are the header.)
`scripts/lint-allow-ratchet.sh` refuses any key that was not there before. Moving a file changes its
key, so a pure move — no content change — reads as new debt and the build stops. This is what has
blocked every package extraction in the program.

A rename-aware version of the ratchet was written and reverted on 2026-08-23: an adversarial review
found three working routes to forge a rename and mint a free exemption (a forged R100 rename over
empty blobs, a count-credit swap, and an abandoned-path pre-grant), plus a `LINT_ALLOW_LIST` override
that passed on a shell script.

**That attempt left no trace in this repository** — checked 2026-08-23 evening across the main
checkout and every worktree: one commit has ever touched `scripts/lint-allow-ratchet.sh`, and it is
unrelated. So you cannot read the reverted code or the review that rejected it. Treat the three
forgery routes as a caution you cannot audit, not as a finding you can check. **The operator ruling
below does not depend on them** and is the actual reason this task exists.

**Operator ruling, 2026-08-23: change the rule rather than teach the ratchet to detect renames.**

## Scope

- `tools/lintreport/allow.txt` — the key format.
- `scripts/lint-allow-ratchet.sh` — the `pairs()` function and both comparison windows.
- `scripts/lint-allow.sh` — the judge that reads the same list.
- `scripts/lint-allow-ratchet-test.sh` — the self-test.
- `tools/lintreport/main.go` — `key`, `readFindings`, `readAllow` and `writeAllow`. **This is the
  bulk of the work and an earlier draft of this task left it out.** golangci-lint's JSON gives a
  file and a line and no symbol, so resolving a finding to its enclosing symbol is new `go/ast`
  capability you have to write here.

**Re-key each entry so it names the finding, not the location. The requirement is a property, not a
mechanism: the key must contain neither a file path nor a package name.** A file that moves between
packages with no content change must then produce a byte-identical key, because nothing in the key
changed.

An earlier draft of this task said "package-qualified symbol". That was wrong and it was
self-defeating: a package qualifier *is* location. `internal/daemon.RunRegistry.Get` becomes
`internal/runregistry.RunRegistry.Get` under exactly the cross-package moves the six tasks blocked on
this one exist to perform — a new key, read as new debt, build stopped. Do not use one.

### The design decision this task is really asking you to make

Dropping the package qualifier means bare symbol names can collide (`Run`, `Close`, `String`,
`main`), and one tolerated finding would then grandfather every same-named symbol in the repo. Pick a
scheme and say in the commit body why:

- **A — bare symbol + linter.** Simplest and stays readable; leaves the collision hole open.
- **B — content hash of the finding** (linter + normalized finding text + normalized symbol body, no
  path, no package). Closes the hole and survives symbol renames too; costs readability, which both
  script headers argue is why this list beat a bare count. Mitigation: `pairs()` already strips from
  the first `#`, so a trailing `# internal/daemon/runregistry.go RunRegistry.Get` comment rides along
  without entering the compared set.
- **C — bare symbol + linter, with a uniqueness gate.** `lintreport` refuses to seed or judge while
  two distinct findings collide on one key, forcing just the colliding minority to a disambiguated
  form. Keeps most of the list readable; costs two key shapes in one file.

**Measure before you choose.** A one-off symbol-mode `-write` pass reports the real duplicate-key
count, and the answer may make this decision for you.

### Findings with no enclosing symbol

Not every finding has one. `depguard` (2 entries) fires at import statements; much of `revive` (35)
fires at package or file scope (`package-comments`, `exported-comment`); `gosec` and `errcheck` can
fire inside package-level var initializers. These need a fallback, and the obvious fallback — the
path — reinstates move-sensitivity for that subset. Say what you chose and which entries it covers.

## Done when

1. A real `git mv` of a file carrying a tolerated finding **into a different package**, with no
   content change, leaves the key set byte-identical and the ratchet passes. Proved by a test that
   performs the move. Cross-package is the case that matters — a same-package rename is the easy
   half, and `lint-ratchet-mutation-proof` requires the cross-package case too.
2. The ratchet still fails when a genuinely new tolerated finding is introduced. See
   `lint-ratchet-mutation-proof`.
3. `make fast` is green on the re-keyed list with the same 575 findings tolerated — this task
   changes how they are named, not which ones are tolerated.
4. The 29 entries named as "travelling" by the four extraction tasks (`runregistry-extract` 1,
   `spendmeter-extract` 3, `handlerpause-extract` 11, `harnesspick-extract` 14) survive a
   cross-package move unchanged. Those tasks each assert `allow.txt` gains nothing on landing, and
   that assertion is only true if this task delivers the property above.

## Limits

- **Do not re-attempt rename detection.** No `git diff -M`, no similarity index, no path history.
  That approach is the one that was reverted and its three forgery routes are on record.
- **Do not tolerate anything new.** The count may fall in a later task; it may not rise here.
- Do not remove the `LINT_ALLOW_LIST` override without checking the self-test still has a way to
  point at a scratch list — but do close the hole where it passes on a non-Go file.
