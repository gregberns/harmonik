---
id: core-split-by-cluster
title: Move internal/core into real packages, one package per landing
type: task
priority: 1
labels: [core, architecture, clear-the-ground]
depends_on: [core-cluster-map, lint-rekey-exclusion-list]
blocks: []
workstream: W3
batch: 4
---

## Problem

`internal/core`'s 449 flat files have no internal import boundary. `core-cluster-map` says what the
packages should be; this task creates them.

## Scope

`internal/core`, moved per the map.

**One package per landing.** Not one commit for the whole split. Each landing: create the package,
move its files and their tests, delete any shim it needed, run the full gate, land. Then the next.

## Done when

For each package created:

1. Its files and their tests have moved together. A test that was white-box and had to become
   external is converted **in the same landing**, not left behind.
2. No shim survives the landing that created it. If a landing needs a temporary shim, deleting it is
   part of that landing.
3. No new package imports `internal/daemon`.
4. `tools/lintreport/allow.txt` gains nothing. After `lint-rekey-exclusion-list` a pure move changes
   no key, so any new entry here means content changed during a move — which is the thing not to do.
5. The dependency direction matches the map. A cycle that the map predicted must be resolved
   explicitly in the landing that hits it, with the resolution stated in the commit body.

## Limits

- **Do not combine a move with a change.** A landing either moves code or changes it, never both. This
  is what makes the diff reviewable and what keeps the lint list honest.
- Do not start until the map exists. The previous programs' repeated failure was moving files before
  agreeing what the packages were.
- Comment density in `internal/core` is 25.4% and much of it is doc comments on exported event types,
  which is legitimate. **Do not cut comments during a move.** That is `core-comment-review`.
