# Findings

> Written as findings appear.

## Confirmed

| # | Finding | Issue | Severity | Branch-introduced? | Disposition | Evidence |
|---|---|---|---|---|---|---|
| 1 | Mid-stream `plugin reload` does not complete in single-digit ms: measured 59-70 ms over three runs against the real 18.6 MB echo plugin binary. The VC-12 bar's "single-digit ms" came from `design/22` measuring a trivial test plugin, not a real gRPC plugin binary. | none — raised here for the operator | P2 | inherited (bar + K5/K8 design, not the K9 harness) | to the operator — NOT a zero-loss failure | EVIDENCE run 6 + reload-cost breakdown |
| 2 | `host.Launch` runs `prewarm` (a discard exec) on EVERY launch, including reload — not once at install as `design/22 §2.1` specifies. On a warm binary this adds a redundant ~11 ms per reload; on a never-warmed fresh binary it re-exposes the full ~330 ms cold code-signature cost ON the reload path, which pre-warm exists to remove. The K9 harness only avoids this by warming binary B before the timed reload. | none — raised here | P2 | inherited (K5) | to the operator — diagnosis for lever (2) of finding 1 | EVIDENCE reload-cost breakdown (cold ~330 ms vs warm ~11 ms) |

Both findings are **inherited** — they live in the VC-12 latency bar and the K5/K8 reload path, not
in the K9 harness this gate added. Neither is a loss-of-message defect. The load-bearing VC-12
invariant — zero loss, zero duplication under reload — holds.

## Investigated and dismissed

| Looked like | What it actually was | How that was established |
|---|---|---|
| The reload might drop mid-stream publishes | It does not — the kernel-held subscription buffers them and the drain gate holds new dispatch until the new process is up | set-equality PASS three runs, 500/500 exact each time |
| A byte-identical "reload" (no real swap) would falsely pass | Ruled out — the two echo binaries are asserted byte-distinct (sha256 `68bb60...` vs `f324ba...`) and the host's VERIFIED step re-hashes the new binary | harness Fatals if the two sha256s are equal; reload points at binary B's path+sha |
| PID "unchanged" might be a vacuous check (in-process) | Ruled out — `harmonikd` runs as a real subprocess; the check is that the daemon process never exited while the plugin child was replaced | EVIDENCE: real subprocess, `syscall.Kill(pid,0)` alive after reload |

## Claimed-done, reconciled

| Claim | Confirmed by | Holds? |
|---|---|---|
| K9: standing chaos harness under `//go:build chaos`, run by `make chaos` only | `tools/echo/chaos_test.go` + Makefile `chaos` target; invisible to `make segments` (run 3 green) | Yes |
| >= 100 msg/s, sequence-stamped distinguishable payloads | 182-187 msg/s measured, payloads `vc12-chaos-%06d` | Yes |
| Mid-stream reload to a different binary | byte-distinct binary B, reload fired at 1/3 of the load | Yes |
| Journal count == messages sent exactly (set-equality) | 500/500 exact, no loss, no duplication, three runs | Yes |
| `harmonikd` PID unchanged | stable + alive across reload, three runs | Yes |
| Reload latency single-digit ms | 59-70 ms measured | **No** — finding 1 |
| VC-13 PUBSUB conformance (shipped types) | byte-for-byte round trip PASS | Yes |
| VC-14 kernel vocabulary grep | clean, PASS | Yes |
| Three consecutive runs, one command | `make chaos` x3 + VC-12 `-count=1` x3 | Yes for the gate mechanics; the latency criterion fails all three |
