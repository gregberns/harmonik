# validation-net — Tasks

13 beads, label `codename:validation-net`. **No hard `br` deps** (an open-dependency dep insta-fails daemon
dispatch — `reference_beads_epic_dep_blocks_dispatch`); sequencing is enforced by the dispatch order below.

| VN | bead | what | pri | comp | twin? | dispatch |
|----|------|------|-----|------|-------|----------|
| 1  | **hk-944c2** | `RunConcurrentMerge(t,N,twin)` reusable fixture | P1 | C2 | twin | **first wave** (enabler) |
| 2  | **hk-he18w** | watchdog-engaging twin scenario (heartbeat-then-delay) | P1 | C2 | twin | **first wave** (enabler) |
| 3  | hk-353t4 | `make test-scenario` + determinism-recipe doc | P2 | C2 | — | first wave |
| 4  | **hk-ukhzu** | **FLAGSHIP** N≥3 concurrent dispatch all-reach-merge, -race | P0 | C1 | twin | after VN1+VN2 merge |
| 5  | hk-40c3y | concurrent launch-liveness + spawn-semaphore no-leak | P0 | C1 | twin | after VN1 merge |
| 6  | hk-tijaj | multi-bead conflict-skip + N-completion serialization | P1 | C1 | twin | after VN1 merge |
| 7  | hk-i8g59 | failing commit_gate terminates via traversal cap (hk-pj4b6 guard) | P1 | C3 | twin | parallel |
| 8  | hk-v5dyg | RED scenario test BLOCKS a merge (gate efficacy) | P2 | C3 | twin | parallel |
| 9  | hk-l11dr | stand up a CI lane (none exists today) | P1 | C4 | — | parallel |
| 10 | hk-6hzci | restore 21 `-short`-quarantined E2E tests in a non-blocking lane | P1 | C4 | — | after VN9 |
| 11 | hk-s2psr | flake policy (hk-6ra3p + hk-3hf9n) | P2 | C4 | — | parallel |
| 12 | hk-pg0w5 | comms at-least-once redelivery + dedupe-on-event_id | P2 | C5 | twin | parallel (stretch) |
| 13 | hk-nhmbk | review-loop verdict-absent salvage | P2 | C5 | twin | parallel (stretch) |

## Dispatch sequence (for the captain agent)

1. **Wave 1 (enablers + independents):** VN1 `hk-944c2`, VN2 `hk-he18w`, VN3 `hk-353t4`, VN7 `hk-i8g59`,
   VN9 `hk-l11dr`, VN11 `hk-s2psr`. VN1+VN2 are the fixture+twin the flagship needs — dispatch first; **wait for
   their merge** before Wave 2 (per `reference_harmonik_stream_concurrent_dispatch` — appended-stream items branch
   off the same base, so build-on-merged work must wait).
2. **Wave 2 (the flagship + concurrency coverage, after VN1+VN2 land):** VN4 `hk-ukhzu` (FLAGSHIP), VN5 `hk-40c3y`,
   VN6 `hk-tijaj`.
3. **Wave 3 (CI restoration, after VN9 lands):** VN10 `hk-6hzci`.
4. **Stretch, any time:** VN8 `hk-v5dyg`, VN12 `hk-pg0w5`, VN13 `hk-nhmbk`.

## Authoring caveat (every scenario bead)

Scenario tests carry `//go:build scenario`; the per-bead commit_gate **SKIPS** them and times out the daemon's
30-min commit budget on real-daemon boot. Author via a **worktree sub-agent (no cap) + targeted fast gate +
cherry-pick**, and **re-run by hand** under the supervisor (`make test-scenario` once VN3 lands). See
`reference_scenario_test_authoring`. Infra/Makefile/CI beads (VN3/VN9/VN10) touch `internal/daemon` or build files
and are gated on the commit_gate no-escape fix (`hk-pj4b6`, named-queues' lane) before they can LAND via the daemon.

## Acceptance for the work

The flagship (VN4) is the keystone: **reverting `53ead2aa` (the tapCh fan-out) must make VN4 FAIL.** That is the
regression guard the 2-week incident lacked.
