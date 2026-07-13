# 1 — Emergent systems and their actual rules

> The agreed reference material: real complex/adaptive systems that stay coherent with no
> central controller, and the *minimal, local, mechanical* rules each one runs on. This is
> the grounding we pull from when we design our own rules. Modeling (how we represent OUR
> system) is deliberately NOT in this doc — that waits for consensus, then becomes doc 2.

## The 5 examples

### 1. Boids / flocking (Reynolds, 1987)
Three rules, each computed only from *nearby* neighbors — no leader, no global view:
- **Separation** — steer to avoid crowding local neighbors
- **Alignment** — steer toward the average heading of local neighbors
- **Cohesion** — steer toward the average position of local neighbors

Coherent flocking emerges from three local vectors.

### 2. Ant colony / stigmergy (Dorigo, Ant Colony Optimization)
- **Deposit** pheromone on the path you took
- **Follow** stronger pheromone, *probabilistically* (exploration survives)
- **Evaporate** — pheromone decays over time

Shortest-path finding with no map and no messaging — coordination happens *through the
environment*. Evaporation is the built-in anti-staleness mechanism: unreinforced paths fade
on their own.

### 3. Slime mold network (Physarum; Tero et al., Science 2010 — reproduced the Tokyo rail map)
- **Reinforce** tubes carrying more flow (thicken them)
- **Decay** tubes carrying less flow (thin, then prune)

An adaptive, self-optimizing, self-pruning network with zero central design.

### 4. Oscillator synchronization (Kuramoto model; fireflies, pacemaker cells)
- Each oscillator **nudges its own phase** slightly toward the phase of those it's coupled to

Weak *local* coupling produces global synchrony — order with no conductor.

### 5. Immune system / clonal selection (Burnet)
- **Select** — cells that bind a real threat proliferate; the rest don't
- **Remember** — successful responders are retained as memory cells
- **Self-tolerance** — cells that attack self are culled

Learns, remembers what generalizes, deletes what's wrong or unused. The natural "metabolism."

*(Honorable mention — Conway's Game of Life: ~3 neighbor-count rules generate unbounded
patterns. Instructive that few rules → much variance, BUT it has no objective and no
constraints, so it is a weak analogy for us — see doc 2 discussion.)*

## The pattern across all five (the laws to obey)

| Law | Present in | Kills, for us |
|---|---|---|
| **Locality** — act only on nearby state, never a global view | all 5 | "1000 agents read one manual" |
| **Coordinate through the medium** — signals live in the environment, not in messages | ants, slime, (boids via neighbors) | brief-injection that "sometimes works" |
| **Reinforce what works** | ants, slime, immune | quality that doesn't compound |
| **Decay by default** — stale fades on its own, no curator | ants, slime, immune | accretion → the stale rulebook |
| **Tiny fixed rule-set** — 3 (boids), 3 (ants), 2 (slime), 1 (Kuramoto) | all 5 | squishy principle sprawl |

The two our first principle-draft was missing entirely: **decay-by-default** (make stale
*fade automatically*, don't make pruning a chore) and **strict locality** (rules act on your
neighbors, not as global statements of value).

## Status
Agreed reference material. The modeling question — how we represent our own system's state,
needs, and how principles act over time — is open and deferred to doc 2.
