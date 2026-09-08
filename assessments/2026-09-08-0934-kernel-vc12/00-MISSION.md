# Mission

> Written BEFORE anything runs. This file is what you were asked to do, frozen at the start.
> If the scope changes mid-assessment, add a dated note at the bottom — do not edit what is above
> it, or the record stops showing what you actually set out to do.

**Started:** 2026-09-08 09:34 (local)
**Assessor:** implementer/assessor session, task K9 (bead `br-kernel-vc12-0ah.8`)
**Gate:** slice acceptance (VC-12) — the whole kernel-vc12 slice holds or it does not
**Asked by:** operator (via the K9 task brief)

## What is being gated

| Lane / branch | Commit | What is in it |
|---|---|---|
| `work/kernel-vc12` | `2884c808bbb011d8c5b4d10ef84f01f83db2a1ad` | K1-K8: segmented modules, wire contract, SQLite journal, in-memory PUBSUB transport, subprocess plugin host, echo plugin, harmonikd composition root, the K8 drain gate |

**Merge base:** not a merge gate — this grades the slice tip against the VC-12 acceptance bar, not a branch merge.
**Conflicts:** n/a.

The K9 chaos harness itself (`tools/echo/chaos_test.go`, the `test/exploratory/cases/kernel-vc12-chaos.md` cases, this assessment) is the **instrument** and is left uncommitted in the working tree for the orchestrator to review and commit. Every result below is produced by that instrument against the binaries built from commit `2884c808`.

## Scope

In bounds: the VC-12 slice gate — publish >= 100 msg/s at `echo.ping` with sequence-stamped
distinguishable payloads, reload the echo plugin mid-stream at a byte-distinct binary, and assert
(1) journal set-equality (no loss AND no duplication), (2) harmonikd PID unchanged across the reload,
(3) reload latency single-digit ms on this darwin box. Also VC-13 (PUBSUB conformance, shipped types
only) and VC-14 (kernel vocabulary grep). The gate must hold three consecutive runs and be
re-runnable by one command (`make chaos`). `make segments` must stay green (the harness is behind
`//go:build chaos`).

Out of bounds: the four-type channel conformance (a named Slice B gate), REQUEST_REPLY / LOOKUP RPCs,
KV, crash-loop budget/backoff, cross-machine transport. None of these hold this gate.

## Independence

**Carve-out.** This session wrote the K9 chaos harness it is now running. That is inherent to K9 —
the task is "build AND run the acceptance gate." The independence boundary this session honors: the
harness only observes the K1-K8 system through its public surfaces (the KernelService gRPC surface,
the admin reload endpoint, the journal read RPC, and the shipped binaries built from `2884c808`). It
does not reach inside the composition root, and it does not modify any K1-K8 source. The orchestrator
reviews the harness before it is committed. The verdict says on its face that the same session built
and ran the instrument.

## Known-red going in

Nothing known-red. `make segments` was green at `2884c808` before K9 work began (re-confirmed as
part of this gate — see EVIDENCE).

## Decided before start

- The harness runs a **real harmonikd subprocess**, not the in-process `Daemon`, so the "PID
  unchanged across reload" assertion is meaningful (the reload restarts the plugin child, never the
  daemon).
- The "different binary" is produced by building `tools/echo/cmd/echo` twice with distinct linker
  build-ids, so the two binaries are byte-distinct (different sha256) and the host's VERIFIED step
  re-checks a genuinely new binary. Byte-distinctness is asserted at runtime.
- The harness lives in the **`tools/echo` module**, not `kernel/`: the `kernel-vocabulary.sh` check
  (part of `make segments`) bans the whole-words `echo`/`ping` in any `kernel/*.go` file, and the
  harness must name them. Tool-isolation is not violated — the harness imports only `contract/` +
  gRPC + std and reaches harmonikd as a launched subprocess over gRPC/HTTP, never by import.
- "Reload latency single-digit ms" is measured with the target binary already OS-warm (its first-exec
  code-signature cost paid before the timed reload), per the design of record: the pre-warm moves the
  ~500 ms darwin first-exec cost OFF the reload path. This is the same isolation the July 505->9 ms
  measurement used.
