# Evidence

> Append-only, written as you go.

## The tree this was run from

| | |
|---|---|
| Checkout | `/Users/gb/github/harmonik-kernel-vc12` |
| Branch | `work/kernel-vc12` |
| Pinned revision (slice tip under audit) | `2884c808bbb011d8c5b4d10ef84f01f83db2a1ad` |
| Working-tree additions (the K9 instrument) | `tools/echo/chaos_test.go`, `test/exploratory/cases/kernel-vc12-chaos.md`, `assessments/2026-09-08-0934-kernel-vc12/`, `Makefile` (chaos-target comment only) — all UNCOMMITTED, left for the orchestrator to review and commit |

Every binary graded below is built from `2884c808`. The K9 harness is the instrument, not the thing
graded; it observes the slice only through the shipped gRPC / admin / journal surfaces.

## Runs

Exit codes read from inside each log.

| # | When | Command | Revision | Exit | Log | Result |
|---|---|---|---|---|---|---|
| 1 | 09:38 | `go test -tags chaos -run TestVC12ChaosReloadUnderLoad -v` | 2884c808 + harness | 1 | (console) | zero-loss PASS, latency FAIL (51 ms) — first probe |
| 2 | 09:43 | `make chaos` | 2884c808 + harness | 2 | `logs/chaos-run-1.log` | contract ok, kernel suite ok, tools/echo FAIL (latency 63 ms only) |
| 3 | 09:43 | `make segments` | 2884c808 + harness | 0 | `logs/segments.log` | GREEN — build+vet+test+lint each module, proto-regen, import-closure, tool-isolation, kernel-vocabulary all ok |
| 4 | 09:44 | `make chaos` (run 2) | 2884c808 + harness | 2 | `logs/chaos-run-2.log` | same shape: zero-loss holds, latency 60.9 ms |
| 5 | 09:44 | `make chaos` (run 3) | 2884c808 + harness | 2 | `logs/chaos-run-3.log` | same shape: zero-loss holds, latency 70.1 ms |
| 6 | 09:45 | `go test -tags chaos -run TestVC12... -v -count=1` x3 | 2884c808 + harness | 1 each | (console, saved to scratchpad) | per-subtest evidence, three consecutive runs — see table below |
| 7 | 09:44 | `go test -tags chaos -run 'TestVC13\|TestVC14' -v` | 2884c808 + harness | 0 | (console) | VC-13 PASS, VC-14 PASS |

### VC-12 three-run detail (run 6)

| Run | msg/s | reload latency | no-loss | no-duplication | set-equality | pid-unchanged | load-rate | latency<10ms |
|---|---|---|---|---|---|---|---|---|
| 1 | 187 | 59.4 ms | PASS | PASS | PASS | PASS | PASS | FAIL |
| 2 | 187 | 60.0 ms | PASS | PASS | PASS | PASS | PASS | FAIL |
| 3 | 187 | 68.5 ms | PASS | PASS | PASS | PASS | PASS | FAIL |

Set-equality = journal held exactly 500 records each run, one per sent sequence number, zero missing
and zero extra. `harmonikd` PID stable and alive across the reload each run (the reload swapped the
plugin child only).

## Live legs — what actually went through the process

| | |
|---|---|
| Work driven through the loop | 500 sequence-stamped publishes/run to `echo.ping` against a live `harmonikd` subprocess, with a mid-stream `plugin reload` to a byte-distinct echo binary (buildid `chaos-variant-a` vs `-b`; sha256 `68bb60...` vs `f324ba...`) |
| Reached a terminal state? | Yes — every run drained the journal to the full sent set and returned |
| Did the work actually land? | Yes — read back through the real `KernelService.JournalRead` on the `seen` journal, byte-for-byte |
| Cases re-run from `test/exploratory/cases/` | KV-001..KV-004 (this gate authored them) |
| New cases written this gate | KV-001 (VC-12 zero-loss, protocol), KV-002 (VC-13 PUBSUB, probe), KV-003 (VC-14 vocabulary, probe), KV-004 (reload latency, protocol) |

### Reload-cost breakdown (standalone measurement on this box, 18.6 MB echo binary)

Measured directly to attribute the ~60 ms reload:

| Component | Cost | Note |
|---|---|---|
| Cold first-exec of a never-run binary | ~330 ms | darwin code-signature validation; the cost pre-warm exists to remove |
| Warm re-exec of the same binary | ~11 ms | one fork/exec/exit of the 18.6 MB binary — already over single-digit ms alone |
| sha256 of the binary (Go crypto/sha256) | ~12 ms | the VERIFIED step, run on every reload |
| Full mid-stream reload (harness end-to-end) | 59-70 ms | sha256 ~12 + pre-warm exec ~11 + real launch exec+handshake ~25 + drain |

## Conditions

| | Value |
|---|---|
| `df -g /` | 611 GiB free (3% used) — well clear of any low-disk dispatch pause |
| Sub-agents active in this tree | none |
| Box | darwin arm64 (Apple silicon), the box VC-12's ms bar names |

## NOT RUN

- Full four-type channel conformance (POINT_TO_POINT / REQUEST_REPLY / LOOKUP) — a named Slice B
  gate, out of scope for slice A per the task plan.
- `make full` — this is a slice/segment gate, not a whole-repo merge gate; `make segments` is the
  hygiene bar for the segment modules and it is green (run 3).
- A cold-binary reload inside the harness — the harness warms binary B first (the design's
  install-time pre-warm model). The ~330 ms cold cost is recorded from the standalone measurement
  above rather than folded into the gate, so the gate isolates the kernel swap.

## Reproduce it

    cd /Users/gb/github/harmonik-kernel-vc12
    make chaos            # the whole tier; tools/echo carries VC-12/13/14
    make segments         # confirm the normal build stays green (harness is behind //go:build chaos)
