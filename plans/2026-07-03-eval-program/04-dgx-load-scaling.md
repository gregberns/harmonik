# DGX Load-Scaling — finding ornith's concurrency ceiling and the DGX's max queue slots

**Date:** 2026-07-03. **Status:** design-only (no production code, no beads created yet). Read-only study.

**Goal:** discover how many concurrent harmonik agent-runs the DGX-hosted **ornith** model
(`Ornith-1.0-35B`, vLLM/OpenAI-compat at `dgx.local:8551/v1`, dummy key OK — verified in
`.harmonik/context/direction-log.md:17-18`) can serve before latency/errors/VRAM degrade, and from
that derive the **recommended max queue-slot count** to pin to the DGX worker. Sibling to
`plans/2026-07-02-eval-harness/DESIGN.md` (the eval DOT + per-run record) — this doc reuses that
run/record substrate and adds a **concurrency ramp** on top of it.

---

## 1. How harmonik controls concurrency (studied first, with citations)

There are **three stacked ceilings**, and the DGX load test drives the third by varying the second:

1. **Global daemon ceiling** — `--max-concurrent` (durable via `daemon.max_concurrent` in
   `.harmonik/config.yaml`, per MEMORY). The daemon-wide in-flight-run cap; echoed as `MaxConcurrent`
   in the queue snapshot (`internal/queue/types.go:513-517`). Threaded into RPC as
   `globalMaxConcurrent` and stamped via `SetGlobalMaxConcurrent` (`internal/queue/rpc.go:786-793`).
   The substrate `SpawnCap` is derived from it (`SpawnCap/2`, each bead = 2 sessions —
   `internal/queue/types.go:580-584`). **This is the hard fleet ceiling — set it ≥ the highest ramp
   level so it never becomes the binding constraint during the test.**
2. **Per-queue slots** — `Queue.Workers` (`internal/queue/types.go:273-287`, QM-066): "the dispatcher
   admits at most `Workers` in-flight runs for this queue at any instant, independent of (and never
   exceeding) the global `--max-concurrent`." Absent → defaults to the global cap
   (`DefaultWorkers`, `internal/queue/rpc.go:829-838`). **This IS a real per-queue in-flight ceiling**,
   enforced in the dispatch tick by `selectNextQueue` (`internal/daemon/workloop.go:1232-1251`), which
   only admits a queue whose `runRegistry.LenForQueue(name)` is below its `effectiveQueueWorkers`
   (`workloop.go:1222-1230`). **This is the knob we ramp:** submit the eval batch to a dedicated
   `dgx-load` queue and step its `Workers` value 1 → 2 → 4 → 8 → 16. The daemon-wide ceiling is
   live-adjustable via `harmonik queue set-concurrency <n>`
   (`internal/queue/cli/setconcurrency.go:29` → `internal/queue/rpc.go:1112` → `concurrencyCtrl.Set`,
   `internal/daemon/concurrencycontroller.go:46`); per-queue `Workers` is set at submit
   (`queue submit --workers N`).
3. **Remote-worker slots** — the DGX runs ornith; whether *runs execute on the DGX box* is a separate
   routing gate. `Queue.WorkerTarget` (`internal/queue/types.go:344-352`) pins every bead from a queue
   to a named worker; `Queue.LocalOnly` (`:334-342`) forces local. The worker itself is declared in
   `.harmonik/workers.yaml` (`internal/workers/workers.go:32,37-53`) with fields
   `name/host/max_slots/enabled`. `Worker.MaxSlots` (`workers.go:43`) is that worker's own slot cap,
   enforced atomically by `SelectWorkerByName` / `SelectWorker` against `inFlight`
   (`internal/workers/registry.go:47,62-83`; `ReleaseSlot` at `:87-93`). **NOTE — two different DGX
   roles:** (a) ornith is a *model endpoint* Pi calls over HTTP; here concurrency = concurrent HTTP
   inference requests, and the harmonik knob is the **queue `Workers`** (per §2, all runs stay on box A,
   only the model traffic goes to the DGX). (b) If instead we make the DGX a remote *execution* worker
   (agents run *on* the DGX), then `Worker.MaxSlots` is the analogous cap. **The operator's question is
   (a)** — "how many queue slots point at the DGX model" — so the ramp variable is `Queue.Workers` on a
   queue whose runs use `harness:pi` pointed at ornith, and `Worker.MaxSlots` is the derived
   *deliverable* if/when the DGX is later registered as its own worker.

**Pointing runs at ornith (no new code):** `harnesses.pi.{provider,model,api_key_env,base_url}` in
`.harmonik/config.yaml`. `base_url` is **now implemented** (hk-z13jz —
`internal/daemon/projectconfig.go:784`, `PiHarnessConfig`; validation example literally cites
`http://dgx.local:8551/v1` at `cmd/harmonik/resolve_pi_config.go:171-184`), wired to a generated
per-run `models.json` (`internal/daemon/pilaunchspec.go:281-306`, `api:"openai"` default). So the
eval-harness doc's O2 "Pi has no base_url" is **stale** — set `harnesses.pi.base_url:
http://dgx.local:8551/v1`, `model: ornith`, dummy key. Every eval bead carries
`workflow:dot dot:eval-bead harness:pi`. So N concurrent runs against ornith = N =
`dgx-load` queue's `Workers`, all sharing the one ornith endpoint.

---

## 2. The concurrency RAMP methodology

**Fixed batch, rising concurrency.** Use a fixed, repeatable task set — the 12 curated eval beads
from `plans/2026-07-02-eval-harness/DESIGN.md` §2 (self-contained, re-runnable, deterministic check)
— as the constant load unit. Re-submit that *same* batch at each concurrency level so throughput
comparisons are apples-to-apples. Because eval tasks create-a-new-file + carry their own test, the
batch is safe to run repeatedly.

**Levels:** run the batch at `Workers` = **1, 2, 4, 8, 16** (extend to 24/32 only if 16 is still
clean). Between levels: drain the queue fully (`queue` empty), let the DGX settle ~30s, poll a
baseline GPU/VRAM reading, then submit the next level.

**Per-level procedure (one level = one clean measurement point):**
1. Set the level: submit the batch to `dgx-load` with `--workers N` (or `queue set-concurrency N` on
   an active queue). Confirm global `max_concurrent ≥ N` first so §1-ceiling-1 isn't the binder.
2. Start on-box GPU polling in the background (§3) at ~1s cadence for the whole level.
3. Let all 12 (× repeats, see below) drain.
4. Snapshot the metrics (§3), tear down polling, record one row.

**Repeats for statistical stability:** run the batch **≥3× per level** (36 runs/level) so per-request
latency/throughput medians aren't dominated by one slow task. The batch mixes trivial→harder, so also
bucket latency by task difficulty — a knee may appear on hard/long-context tasks first.

---

## 3. Metrics and where each comes from (client + endpoint + on-box, cross-checked)

The operator's caveat — **on-box perf tools may misreport on this DGX** — is load-bearing: treat
`nvidia-smi` as *corroborating*, and make the **client-side** and **vLLM `/metrics`** numbers the
primary signal (they measure what the workload actually experienced).

| metric | source (primary) | how |
|---|---|---|
| **request/run latency** | harmonik event log (PRIMARY) | `run_completed.ended_at − run_started.started_at` joined on `run_id`; per-node via `implementer_phase_complete` vs `run_started` — exactly the wall-time seam in `plans/2026-07-02-eval-harness/DESIGN.md:78-93`. Bucket by difficulty. |
| **tokens/sec throughput** | vLLM `/metrics` (PRIMARY) + client | scrape `dgx.local:8551/metrics` (Prometheus text; vLLM exposes `vllm:generation_tokens_total`, `vllm:prompt_tokens_total`, `vllm:num_requests_running`, `vllm:num_requests_waiting`, `vllm:time_per_output_token_seconds`, `vllm:e2e_request_latency_seconds`). Δtokens/Δt = aggregate throughput; `num_requests_waiting > 0` sustained = the endpoint is queueing = past saturation. Cross-check against client-side (batch total output tokens ÷ wall-time). |
| **error / timeout rate** | harmonik event log (PRIMARY) | count `run_failed` (eventtype) and DOT-node timeout/transient outcomes per level; a rising `failure_class='transient'` or HTTP-5xx/429 from ornith = overload. vLLM `/metrics` `vllm:request_success_total` vs failures corroborates. |
| **GPU util / VRAM** | `nvidia-smi` (CORROBORATING, may misreport) | poll `nvidia-smi --query-gpu=utilization.gpu,memory.used,memory.total,power.draw --format=csv,noheader,nounits -l 1` (assumed; **reconcile with monitoring recon doc `03` when it lands** — it owns the authoritative DGX poll commands + whether nvidia-smi reports correctly here). VRAM near `memory.total` = OOM risk. If nvidia-smi is unreliable, `/metrics` `vllm:gpu_cache_usage_perc` (KV-cache occupancy) is the trustworthy VRAM-pressure proxy. |

Record per level: median & p95 run-latency (overall + per difficulty), aggregate tokens/sec,
error/timeout count, peak VRAM %, peak KV-cache %, mean `num_requests_waiting`.

---

## 4. Stop condition (declare the ceiling reached when ANY fires)

Ramp until the **first** of:
- **Latency knee** — median run-latency at level N ≥ ~1.5× the level-1 baseline, OR throughput
  (tokens/sec) stops rising (or falls) as N doubles — i.e. added concurrency no longer buys work
  (the endpoint is time-slicing, `num_requests_waiting` climbing).
- **Error onset** — any non-zero timeout / HTTP-5xx / 429 rate that is *attributable to load* (absent
  at lower N), sustained across the repeats (not a one-off flake).
- **VRAM / OOM** — `vllm:gpu_cache_usage_perc` → ~1.0 (KV-cache full → requests preempted/rejected), or
  nvidia-smi `memory.used` within a safety margin of `memory.total`, or vLLM logs preemption.

The **last clean level** (the highest N *before* the one that tripped a stop condition) is the DGX's
serving ceiling for this workload.

---

## 5. Deriving the recommended max QUEUE SLOTS

- **Recommended `Workers` for the DGX queue = the highest clean level** (last N before the knee/error/
  VRAM onset). If the knee appears *between* powers of two, binary-search that gap (e.g. clean at 8,
  tripped at 16 → probe 12) for a tighter number.
- Apply a **safety headroom**: pin the production queue at `min(clean_ceiling, ⌊0.8 × clean_ceiling⌋)`
  rounded down, so normal jitter doesn't ride the knee. State both the measured ceiling and the
  padded recommendation.
- **Where the number lands:** the per-queue `Queue.Workers` value (`types.go:287`) for the queue that
  targets ornith — set at `queue submit --workers <N>` or live via `queue set-concurrency <N>`. Ensure
  global `daemon.max_concurrent` ≥ that value (else §1-ceiling-1 clips it —
  `types.go:275-276`). If the DGX is later registered as a remote *execution* worker, the same number
  becomes its `workers.yaml max_slots` (`workers.go:43`, enforced at `registry.go:47`).
- **Caveat to record with the number:** it is workload-specific (ornith-35B, this task mix, this
  context length). Longer-context or heavier tasks lower it; note the batch's token profile alongside
  the recommendation so a future re-derive is comparable.

---

## 6. Beads to create (codename:eval-load)

1. **EL1 — DGX-load queue + config.** Define the `dgx-load` named queue and the `harness:pi`→ornith
   config used for the ramp; confirm `daemon.max_concurrent` ≥ 16 for the test window. (config/DOT only.)
2. **EL2 — GPU/`/metrics` poller.** A small read-only collector that scrapes `dgx.local:8551/metrics`
   (Prometheus text) at ~1s + runs the `nvidia-smi` poll, writing a timestamped JSONL per level.
   BLOCKED-BY monitoring recon doc `03` (authoritative poll commands / nvidia-smi reliability).
3. **EL3 — ramp driver + per-level teardown.** Script that, per level, sets `Workers`=N, submits the
   fixed 12-bead batch ×3, waits for drain, snapshots, steps N. Reuses the eval collector for
   run-latency from events.jsonl.
4. **EL4 — aggregation + report.** `jq`/collector join: per level → median/p95 latency (overall +
   per difficulty), tokens/sec, error rate, peak VRAM%/KV%; emit the ramp table + the derived
   max-slots recommendation (§5) with headroom.
5. **EL5 — stop-condition + recommendation write-up.** Apply §4 thresholds to EL4's table, pick the
   clean ceiling, compute the padded `Workers` recommendation, and record it (with the workload
   caveat) back into the DGX queue config + this plan dir.

Sequence: EL1 ‖ (EL2 after doc 03) → EL3 → EL4 → EL5. All ride existing seams (named queues, per-queue
`Workers`, Pi→ornith config, events.jsonl); the only new code is the read-only poller (EL2) and the
ramp/aggregation scripts (EL3/EL4) — none on the daemon hot path.
