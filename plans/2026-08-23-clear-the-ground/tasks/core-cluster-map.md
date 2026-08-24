---
id: core-cluster-map
title: Name the packages hiding inside internal/core's 450 flat files before moving anything
type: task
priority: 1
labels: [core, architecture, clear-the-ground]
depends_on: [lint-rekey-exclusion-list]
blocks: [core-split-by-cluster]
workstream: W3
batch: 4
---

## Problem

`internal/core` is 30,208 production lines across **449 files in one flat directory** (239 production,
210 test). It fails differently from `internal/daemon`: the files are small — 126 lines on average —
and the problem is that there is no structure at all. It is the repo's second-largest package and its
most comment-dense (20,529 comment lines, 25.4%).

A flat package of 449 files has no import boundary, so nothing prevents any part of it depending on
any other part. That is the property that has to change, and it cannot be changed by moving files
until someone says what the packages are.

## Scope

Read-only. The deliverable is a map, not a move.

Produce, for `internal/core`:

1. The proposed package boundaries, each with the files that belong to it and a one-line statement of
   what the package is for.
2. The dependency graph between the proposed packages, and any cycle in it. A cycle is a finding —
   report it, do not resolve it by merging the two packages back together without saying so.
3. The symbols referenced from outside `internal/core`, grouped by which proposed package would own
   them. This is the public surface each split has to preserve.
4. The files that fit no cluster. These are usually the interesting ones.

Some clusters are already visible in the file names — event types, daemon events, agent events and
reconciliation events each have their own files. Start there and expect it to be incomplete.

## Done when

The map exists as a document under `plans/2026-08-23-clear-the-ground/`, every one of the 239
production files is assigned to exactly one proposed package or explicitly listed as unassigned, and
the cycles are named.

## Limits

- **Move nothing.** This task produces a document. `core-split-by-cluster` does the moving.
- Do not propose a package with one file in it unless you say why it is alone.
- Do not resolve a dependency cycle by inventing an interface. Report it; the resolution is a
  decision with consequences and it belongs in the split task where a reviewer can see it.
