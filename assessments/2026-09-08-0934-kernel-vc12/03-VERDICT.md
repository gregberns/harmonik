# Verdict

> Written last. A reasoned judgement, not a tally.

## VC-12 HOLDS on its load-bearing guarantee (zero loss, zero duplication under reload). It DOES NOT meet the single-digit-ms reload criterion. The latency bar goes to the operator.

The reason the kernel-vc12 slice exists is a plugin reload that loses nothing. That holds, cleanly,
three consecutive runs: 500 sequence-stamped payloads published at ~185 msg/s to `echo.ping` against
a live `harmonikd`, the echo plugin reloaded mid-stream to a byte-distinct binary, and the journal
afterwards holding the sent set **exactly** — 500 records, every sequence number once, none missing,
none extra. The `harmonikd` process never restarts across the reload; only the plugin child is
swapped. This is the guarantee the drain gate (K8) and the kernel-held subscription (K4) were built
to make, and it is real.

One VC-12 criterion does not hold: the reload does not complete in single-digit milliseconds. It
takes 59-70 ms against the real 18.6 MB echo plugin binary. This is not a zero-loss failure and it
does not put a single message at risk — a reload is a short dispatch pause during which publishes
queue in the kernel and are delivered after. It is a performance bar that was set from a measurement
on an unrepresentative binary: `design/22` measured a trivial test plugin at 7-11 ms, and a real
gRPC plugin binary on this box cannot be sha256-verified (~12 ms), pre-warm-exec'd (~11 ms) and
re-launched with a go-plugin handshake (~25 ms) in single digits. See findings 1 and 2.

I am not relaxing the bar to make the gate green — that would be a green nobody could trust. The
harness asserts single-digit ms as a hard subtest, it fails loudly, and the miss is put to the
operator with the full cost breakdown and two levers. Recalibrating a stated acceptance bar is the
operator's call; the evidence to have that conversation is here.

## Commits graded

| What | Commit |
|---|---|
| Slice tip under audit (K1-K8) | `2884c808bbb011d8c5b4d10ef84f01f83db2a1ad` |
| K9 instrument (uncommitted, not graded — reviewed separately by the orchestrator) | working tree: `tools/echo/chaos_test.go`, `test/exploratory/cases/kernel-vc12-chaos.md`, Makefile chaos comment |

## Not graded

- Full four-type channel conformance — a named Slice B gate, out of scope for slice A.
- The K9 harness itself is the instrument, not the graded artifact; the orchestrator reviews it
  before committing. This gate's independence carve-out (same session built and ran the instrument)
  is stated on the face of `00-MISSION.md` and here.

## What the criteria found

| Criterion | Result | Weight |
|---|---|---|
| No loss | HOLDS 3/3 | load-bearing — the slice's reason to exist |
| No duplication | HOLDS 3/3 | load-bearing |
| Set-equality (exact count) | HOLDS 3/3 | load-bearing |
| harmonikd PID unchanged | HOLDS 3/3 | load-bearing |
| Load rate >= 100 msg/s | HOLDS 3/3 (~185) | supporting |
| Reload single-digit ms | FAILS 3/3 (59-70 ms) | finding to the operator, not a zero-loss failure |
| VC-13 PUBSUB conformance | HOLDS | in scope |
| VC-14 kernel vocabulary | HOLDS | in scope |
| `make segments` stays green | HOLDS (exit 0) | hygiene — the harness is invisible to the normal build |

## Residual risk

- The reload latency was measured with binary B **warmed** first (the design's install-time pre-warm
  model). Because K5 pre-warms inside `host.Launch` (finding 2), a real operator reload of a
  *freshly built, never-executed* plugin would pay the ~330 ms cold code-signature cost on the
  reload path — 5x worse than measured, and still zero-loss. The gate does not exercise that path;
  finding 2 records it.
- The reload-window kernel queue is unbounded in slice A (fine at this volume; bounded by VC-11 in
  the comms slice). A pathological producer during a long drain could grow it without limit.
- Zero-loss is proven at 500 messages and one reload per run. It is not proven at logtail volumes or
  across repeated back-to-back reloads; those belong to later slices.

## For the operator

One decision: **the VC-12 "single-digit ms reload" bar.** The zero-loss slice gate holds. The
latency bar, as literally stated, does not, and the evidence says it cannot for a real plugin binary
on this box. Choose one:

1. **Re-calibrate the bar** to a realistic figure for a real plugin binary (e.g. "reload dispatch
   pause < 100 ms with zero loss"), and let Slice B start. The zero-loss guarantee — the thing that
   actually matters — is met.
2. **Direct a K5/K8 change** to move pre-warm to install time (finding 2), which removes the
   redundant per-reload exec and, more importantly, removes the ~330 ms cold-start cliff from the
   reload path. Even then, single digits are out of reach here (sha256 + one exec + handshake
   ~35 ms), so this improves honesty and worst-case latency but does not by itself pass a
   single-digit bar.

My recommendation: option 1 to unblock Slice B, with option 2 filed as a real K5 defect (finding 2)
to fix on its own timeline — the cold-start-on-reload behaviour is worth fixing regardless of the
bar.
