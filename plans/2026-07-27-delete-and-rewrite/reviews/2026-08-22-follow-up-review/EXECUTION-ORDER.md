# Execution order for Charlie

## Required first assignment

Charlie should begin with **N02 and N03 only**. They are small, independent, and do
not require the unresolved lint policy. They protect the tree from accidental
dispatch activation and make subsequent decomposition measurable.

Stop after both land. Verify the activation guard fails under a temporary producer
call and record the four scoreboard baselines.

## Operator gate

N01 requires an explicit ruling that an unchanged finding may follow a Git rename
without counting as new debt. If that policy is not approved, no file-move task may
start. Do not solve the gate by adding allow entries.

N04 also requires an operator choice about eager refill. It does not block N05, but
it blocks claims that multiple independently planned queues can run safely.

## Dependency order

```text
N02 ── dispatch remains inert
N03 ── scoreboard active

operator lint ruling → N01 → N05 ┬→ N06
                                 ├→ N07
                                 ├→ N08
                                 ├→ N14
                                 └→ N15 preparation

N09 → N10 → N12
N11 is serialized with other work-loop edits
N10 → N13

N05 + two successful leaf landings → N15 execution

N16, N17, N18 may run in a low-conflict quality lane after N03.
```

## Suggested waves

1. **Safety:** N02, N03; decide N01 and N04.
2. **Keystone:** N01, then N05. Stop and run the complete affected gate.
3. **Parallel leaves:** N06, N07, N08 in separate worktrees. Integrate one at a time
   and rebase the remaining moves after each landing.
4. **Ownership:** N09, then N10. Keep N11 under a single work-loop owner.
5. **Boundary:** N12, N13, N14.
6. **Large cut:** N15 only after two smaller moves prove lint, shims, tests, and
   scoreboard accounting.
7. **Quality lane:** N16–N18 when they do not overlap structural owners.

## Stop gates

- **Dispatch:** any production intent creation stops the wave.
- **Rename lint:** any new or changed grandfathered finding stops the move.
- **Package move:** a new package importing `internal/daemon` stops the move.
- **Scoreboard:** unexplained daemon production growth stops integration.
- **Tests:** moved white-box tests must travel or be converted in the same slice;
  do not add compatibility shims without a deletion task in that slice.
- **Pure decision:** do not accept extraction until deliberate mutation proves the
  new table test observes the production call path.
- **Control plane:** do not edit production until the complete 30-file scratch move
  builds and its two named shared symbols have decisions.
- **Crew parallelism:** do not rely on event-stream isolation until queue routing is
  proven end to end.

## Work Charlie must not start

- dispatch producer wiring or replay generalization;
- another implementation of C22;
- scheduler/work-loop package movement;
- terminal-substrate movement before the capability contract;
- bulk detector replacement or repeated-literal waves;
- repo-config `golangci-lint --fix`;
- comment deletion as an independent whole-tree sweep.
