---
title: Idea Catalog
status: seed
type: index
related: [docs/subsystems/INDEX.md, docs/problems/INDEX.md, docs/goals/INDEX.md]
created: 2026-04-13
updated: 2026-04-13
---

# Idea Catalog

> **As-of April 2026 (Phase-0 framing).** These conceptual anchors are frozen at project inception and predate the comms-bus / Captain-&-Crew / persistent-daemon reality. For current state see [STATUS.md](../../STATUS.md) and [docs/INITIATIVES.md](../INITIATIVES.md).

## Overview
This directory captures brainstorm ideas -- proposed mechanisms, patterns, and approaches that could become part of harmonik's design. Ideas are seeds: some will grow into subsystem designs, others will be absorbed into broader patterns, others will be discarded. Each idea links to the subsystems, problems, and goals it relates to.

Ideas are not commitments. They are named, trackable thoughts.

## Idea Table

| ID | Title | Status | Relates To |
|----|-------|--------|------------|
| I01 | [Hook-Driven Agent Behavior](hook-driven-agent-behavior.md) | seed | S05, S01, P05, P06 |
| I02 | [Deterministic Skeleton, Probabilistic Organs](deterministic-skeleton-probabilistic-organs.md) | seed | G01, ZFC, AlphaGo |
| I03 | [Composable Multi-Workflow Systems](composable-multi-workflow-systems.md) | seed | S01, S05, P06, Kilroy |
| I04 | [AlphaGo Search for Coding](alphago-search-for-coding.md) | seed | G01, AlphaGo |
| I05 | [Model Pyramid Cost Optimization](model-pyramid-cost-optimization.md) | seed | ZFC, Kilroy |
| I06 | [Agent Specialization Through Constraints](agent-specialization-through-constraints.md) | seed | G03, P05 |
| I07 | [Filesystem as Coordination Substrate](filesystem-as-coordination-substrate.md) | seed | P02, P03, G02 |
| I08 | [Fleet Sleep/Wake Research](fleet-sleep-wake-research.md) | research (2026-06) | hk-rl4b |

## Documents
- [I01: Hook-Driven Agent Behavior](hook-driven-agent-behavior.md) -- Hooks enforce behavior and compose workflows
- [I02: Deterministic Skeleton, Probabilistic Organs](deterministic-skeleton-probabilistic-organs.md) -- Deterministic structure, probabilistic intelligence
- [I03: Composable Multi-Workflow Systems](composable-multi-workflow-systems.md) -- Independent workflows composed via events
- [I04: AlphaGo Search for Coding](alphago-search-for-coding.md) -- Tree search over solution spaces with verifiers
- [I05: Model Pyramid Cost Optimization](model-pyramid-cost-optimization.md) -- Route tasks to the smallest capable model
- [I06: Agent Specialization Through Constraints](agent-specialization-through-constraints.md) -- Constrained capabilities make agents more effective
- [I07: Filesystem as Coordination Substrate](filesystem-as-coordination-substrate.md) -- Git-tracked files as the coordination mechanism
- [I08: Fleet Sleep/Wake Research](fleet-sleep-wake-research.md) -- Quiesce token-burning LLM idle-wakeups when work drains; daemon stays up as the cheap wake-trigger (initiative hk-rl4b, 2026-06)
