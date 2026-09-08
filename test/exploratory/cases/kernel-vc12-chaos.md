# Kernel VC-12 chaos cases

The live cases behind the kernel-vc12 slice gate (task K9). They are driven by the standing chaos
harness at `tools/echo/chaos_test.go`, behind `//go:build chaos`, run by one command: `make chaos`.
Unlike the CLI cases in this library, these need three built binaries (one `harmonikd`, two
byte-distinct `echo` plugins) and a live daemon; the harness builds and launches all of them, so a
reader runs one command and reads the verdict.

The slice question is narrow and load-bearing: **does a plugin reload mid-stream lose or duplicate a
message?** Everything else here supports answering that honestly.

---

## KV-001 — a plugin reload under load loses or duplicates a message

Class:       protocol
Exercises:   the K8 drain gate + the K4 kernel-held subscription, under the K7 composition root
Bead:        br-kernel-vc12-0ah.8 (K9)
Status:      HOLDS at 2884c808bbb011d8c5b4d10ef84f01f83db2a1ad (verified 2026-09-08, three runs)

Preconditions: a Go toolchain and the workspace `go.work`. No daemon needs to be running; the
harness launches its own `harmonikd` subprocess on two ephemeral loopback ports.

Steps:

    make chaos
    # or, to watch one VC-12 run on its own:
    cd tools/echo && go test -tags chaos -run TestVC12ChaosReloadUnderLoad -v -count=1 ./.

Expect: the load generator publishes 500 sequence-stamped payloads to `echo.ping` at >= 100 msg/s;
a third of the way through, the echo plugin is reloaded to a byte-distinct binary; afterwards the
`seen` journal holds the sent set **exactly** — every sequence number once, none missing, none
extra. Subtests `no-loss`, `no-duplication`, `set-equality`, `daemon-pid-unchanged`, `load-rate`
all pass.

Failure signature: a `HARD STOP` line naming the exact lost or duplicated sequence numbers. Loss
means a publish that landed mid-reload was dropped instead of held in the kernel queue; duplication
means an in-flight delivery was re-dispatched after the reload instead of drained to completion. A
changed `harmonikd` PID means the reload restarted the daemon instead of only the plugin child.

---

## KV-002 — PUBSUB does not round-trip (VC-13, shipped types only)

Class:       probe
Exercises:   the one channel type this slice carries traffic for — CHANNEL_TYPE_PUBSUB
Bead:        br-kernel-vc12-0ah.8 (K9)
Status:      HOLDS at 2884c808 (verified 2026-09-08)

Preconditions: as KV-001.

Steps:

    cd tools/echo && go test -tags chaos -run TestVC13PubsubConformance -v -count=1 ./.

Expect: a single payload carrying a non-ASCII byte published to `echo.ping` lands in the `seen`
journal byte-for-byte — proving publish -> subscribe -> deliver -> journal conforms and that the
transport carries opaque bytes it never parses. Full four-type channel conformance is a Slice B
gate, out of scope here.

Failure signature: the journal holds zero records (the subscription never received it) or a record
whose bytes differ from what was published (the transport parsed or mangled the payload).

---

## KV-003 — a domain noun leaked into the kernel (VC-14)

Class:       probe
Exercises:   the kernel-names-no-domain-noun rule
Bead:        br-kernel-vc12-0ah.8 (K9)
Status:      HOLDS at 2884c808 (verified 2026-09-08)

Preconditions: none — a pure source grep, no daemon.

Steps:

    scripts/segments/kernel-vocabulary.sh
    # also run inside the chaos tier:
    cd tools/echo && go test -tags chaos -run TestVC14KernelVocabulary -v -count=1 ./.

Expect: `kernel-vocabulary: ok — no domain noun in kernel/ Go source`. The substrate carries opaque
bytes and names no tool word (bead/run/session/agent/echo/ping/tmux/claude) as a whole word in any
`kernel/*.go`.

Failure signature: `kernel-vocabulary: FAIL` with the offending file and line. This is why the chaos
harness lives in `tools/echo`, not `kernel/`: it must say `echo` and `ping`, and a tool may name
itself.

---

## KV-004 — the reload does not complete in single-digit ms

Class:       protocol
Exercises:   the reload latency budget of VC-12 on a realistic plugin binary
Bead:        br-kernel-vc12-0ah.8 (K9) — finding raised for the operator
Status:      DOES NOT HOLD at 2884c808 (measured 59-70 ms, three runs, 2026-09-08)

Preconditions: as KV-001.

Steps:

    cd tools/echo && go test -tags chaos -run TestVC12ChaosReloadUnderLoad -v -count=1 ./.
    # read the "reload latency (mid-stream ...)" log line

Expect (per the VC-12 bar): the mid-stream reload completes in single-digit milliseconds on this
darwin box.

Failure signature: the `reload-latency-single-digit-ms` subtest fails with a measured latency of
~60 ms. The cost breakdown on this box (18.6 MB gRPC plugin binary): sha256 verify ~12 ms +
a pre-warm exec inside `host.Launch` ~11 ms + the real launch exec and go-plugin handshake ~25 ms.
The design's 7-11 ms figure (`plans/2026-07-15-agent-substrate-v2/design/22-plugin-system.md`) was
measured on a trivial test plugin, not a real plugin binary. Two levers, both for the operator:
(1) re-calibrate the latency bar to a realistic value for a real plugin binary while keeping the
zero-loss guarantee; (2) move the pre-warm to install time (the design's own rule) so the reload
path stops paying a second exec — and note that on a *never-warmed* fresh binary the current
in-`Launch` pre-warm re-exposes the full ~330 ms cold code-signature cost on the reload path, which
pre-warm exists to remove. Even with lever (2), single digits are out of reach for an 18.6 MB binary
here (sha256 + one exec + handshake ~35 ms).
