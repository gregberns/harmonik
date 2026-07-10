# 06 — Task metric fields (Q6)

Bead: hk-eval-prog-task-metric-fields-bpx4n (WS3f).

Per-task `expected_big_o` + `reference_line_budget` labels have been applied to the 14 curated
task beads (8 existing + 6 new, per `05-problem-set-and-tools.md` Part 1). These are the
deterministic feeders `harmonik eval metrics` reads via `evalFetchBeadLabels` /
`evalLabelValue` (`cmd/harmonik/eval_metrics_cmd.go`) for Q4 (efficiency: wrong complexity
class down-scores) and Q5 (over-reach: diff line count vs budget).

`br label add` restricts label values to `[A-Za-z0-9_:-]` (no parens/spaces), so big-O notation
is encoded alnum-safe: `O1` = O(1), `On` = O(n), `OnLogN` = O(n log n), `OVplusE` = O(V+E). The
judge/report layer should treat these as the canonical short forms of the corresponding
mathematical notation.

Line budgets are sized off the actual reference implementations under `evaltasks/` (solution +
test for the 8 simple tasks that commit both; solution-only for the 6 hard tasks whose test is
pre-committed/tamper-proof), with ~15-30% headroom.

| task_id | bead | expected_big_o | reference_line_budget |
|---|---|---|---|
| eval-readme-typo | hk-eval-readme-typo-9aisz | O1 | 20 |
| eval-string-reverse | hk-eval-string-reverse-lvq4x | On | 40 |
| eval-fizzbuzz | hk-eval-fizzbuzz-avjjr | On | 80 |
| eval-parse-int-safe | hk-eval-parse-int-safe-il2im | On | 60 |
| eval-dedupe-stable | hk-eval-dedupe-stable-2uxub | On | 90 |
| eval-lru-cache | hk-eval-lru-cache-kk809 | O1 | 150 |
| eval-json-roundtrip | hk-eval-json-roundtrip-takhz | O1 | 120 |
| eval-topo-sort | hk-eval-topo-sort-idckm | OVplusE | 180 |
| eval-bugfix-rate-limiter | hk-eval-bugfix-rate-limiter-rvk88 | O1 | 25 |
| eval-cli-kv | hk-eval-cli-kv-avdmm | O1-per-op | 170 |
| eval-expr-eval | hk-eval-expr-eval-gvxz6 | On | 220 |
| eval-interval-schedule | hk-eval-interval-schedule-uadn3 | OnLogN | 60 |
| eval-lru-ttl | hk-eval-lru-ttl-tjz7m | O1 | 110 |
| eval-refactor-storage | hk-eval-refactor-storage-28n1l | O1 | 130 |

If beads are ever archived and recreated, re-apply with:

```bash
br label add <bead_id> -l "expected_big_o:<value>"
br label add <bead_id> -l "reference_line_budget:<n>"
```
