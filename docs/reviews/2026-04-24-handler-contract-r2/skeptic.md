# Round 2 Skeptic Review — handler-contract.md v0.2

## Verdict

Round 1 extracted real work. Silent-hang's false-positive problem is
solved structurally — by introducing a heartbeat obligation that makes
T mechanical, not by tuning T. The skill-provisioning split (HC-048
structural / HC-048a transient-with-backoff, bounded by distinct
`provisioning_timeout`) is the right shape. The in-process-fake
carve-out names the unit-test path the locked decision never intended
to forbid. Emitter identity is reworded but, per Challenge C, still
phrased descriptively rather than normatively.

Recommendation: **proceed with revisions focused on the five load-
bearing challenges below**. The heartbeat cadence math and the Unix-
socket pin are defensible but under-justified; the 5+2 taxonomy
reshuffles Round 1's labeling concern without revisiting whether
ProtocolMismatch belongs to Structural's routing regime at all.

## Integration-fix audit

- **C1 watcher/adapter split rationale →** §A.3 paragraph present but
  still does not name the two alternatives by shape (S04-owns-watcher
  vs daemon-owns-everything). Minor this round; reopenable.
- **C2 skill transient path → HC-048a + `provisioning_timeout`.** Genuine
  fix; parameters (1s/16s/4 attempts/60s) are implementable as stated.
- **C3 six-sentinel taxonomy → 5+2 restructure.** Partial — Challenge A.
- **C4 silent-hang T → HC-026a heartbeat + T=600s.** Genuine in shape;
  Challenge B on the cadence math.
- **C5 twin-vs-in-process → HC-035 carve-out.** Genuine fix; tightly
  scoped to per-adapter / per-watcher / per-classifier unit tests.
- **R1 emitter-identity →** HC-007/HC-008/HC-010/§6.4 reworded; see
  Challenge C (descriptive, not normative, at the place it matters).
- **Unix-socket pin (HC-007 + HC-007a + HC-044).** Challenge D.

Net: ~75% real resolution. Two remaining items are structural (taxonomy
honesty, emitter-identity normativity); one is a defensibility gap
(heartbeat cadence math).

## Challenges

### Challenge A — The 5+2 sub-sentinel restructure reshuffles Round 1's concern without resolving it

HC-020 declares five primary classes plus two structural sub-sentinels.
§8.6/§8.7 publish `agent_failed` with `class="structural"` and a
`sub_reason`. §A.3 says the five primary classes "map one-to-one to
routing regimes" in execution-model §8.

The R1 critic's concrete challenge: ProtocolMismatch may not belong to
Structural's routing regime at all. A re-plan against the same pinned
handler binary cannot resolve "no mutually supported wire-protocol
version" — the fix is a different binary or a config change. That is
deterministic-failure shape (§8.3), not structural-replan shape. v0.2
relabels the taxonomy without revisiting the routing assignment. If
ProtocolMismatch routes like Structural (re-plan), the orchestrator
spins re-plans that cannot possibly succeed until budget exhaustion.

Two defensible shapes:

- **(i)** Keep ProtocolMismatch under Structural, but name in §A.3 the
  concrete re-plan that resolves it (swap `handler_ref`, change pinned
  commit via config). If no such re-plan exists, the parent is wrong.
- **(ii)** Move ProtocolMismatch to wrap `ErrDeterministic`. Version
  mismatch against a pinned commit IS the "confirmed-bug or impossible-
  condition determined from structured fields" of §8.3: version lists
  are structured; no-common-version is the impossible condition.

**Load-bearing because**: the routing assignment drives retry spin. A
mis-classified terminal condition under a re-plan regime generates
"why did we burn budget on sixteen retries" bug reports. The label
shuffle leaves what the orchestrator actually does unchanged.

### Challenge B — T=600s + heartbeat-at-T/2 leaves a 300s detection window that is still arbitrary

HC-026a pins heartbeat cadence at ≤T/2; §7.1 sets T=600s. Practical
detection window is 300s — max time a stuck process (no heartbeats)
runs before `warning`. Soft-terminate at 2×T=1200s; hard-terminate at
4×T=2400s — 40 minutes from last progress.

R1 called T=120s arbitrary. v0.2's answer: with heartbeats, T becomes
mechanical. But the 300s detection window IS load-bearing and IS still
arbitrary. A stuck handler (infinite loop, mutex deadlock, blocked
syscall) consumes up to 300s wall-clock plus whatever tokens it keeps
burning before `warning`. A handler-implementation bug that suppresses
heartbeats runs 40 minutes before SIGKILL. §A.3 addresses the false-
positive story but not the false-negative cost; S07 scenario drift-
detection now picks up 40-minute stalls where v0.1 got 8.

Three defensible shapes:

- **(i)** Decouple cadence and escalation. Fix heartbeat cadence at a
  concrete small number (30s or 60s); let T govern escalation only.
  T/2 couples two unrelated quantities.
- **(ii)** Reduce T to ~180s now that heartbeats make false-positives
  near-impossible; detection window becomes 90s, upper bound drops
  proportionally.
- **(iii)** Keep T=600s but state the false-negative cost in §A.3 and
  name what bounds a stuck handler's damage. Without that, control-
  points §6.9 budget enforcement is the only thing protecting the
  operator from runaway handlers.

**Load-bearing because**: silent-hang protects the operator from
handlers that CANNOT self-report. T/2=300s coupling gives a suppressed-
heartbeat bug 40 minutes to misbehave — a big envelope for a condition
the spec frames as "process is actually stuck."

### Challenge C — Emitter-identity rewording is descriptive, not normative, at the place it matters

R1 flagged v0.1's "handler publishes to bus" as wrong; the watcher
publishes. v0.2 reworks HC-007 para 2, §6.4 preamble, HC-008, HC-010 to
say "handler emits on stream; watcher translates to bus event."

The MUST in HC-007 is about emission on the stream. The bus obligation
reads "the watcher … is the authoritative publisher of handler-
lifecycle events." That is descriptive voice. No MUST forbids a
non-watcher component from publishing directly. For a subprocess this
is physically impossible — but in-process `Handler` fakes per HC-035
carve-out are legal, and the carve-out excuses them from the wire
protocol without binding them to emitter-identity discipline. A fake
publishing directly to the bus bypasses the redaction middleware
(§4.7 installs on the producer path); HC-INV-003 (no secrets in event
log) silently degrades for fake-emitted events.

HC-INV-001 (one watcher per session) and HC-INV-004 (ordering) both
describe watcher behavior. Neither asserts the watcher is the SOLE
publisher. The invariant R1 was after is missing.

Fix shape: add HC-INV-006 — "For every handler-lifecycle event type in
§6.4, the watcher MUST be the sole publisher to the in-process event
bus. No other component, including in-process fakes per §4.8, MAY
publish a handler-lifecycle event directly."

**Load-bearing because**: redaction middleware on the producer path is
the enforcement point for HC-INV-003. Any bus-publisher that is not the
watcher is an unredacted channel by construction.

### Challenge D — Unix-socket pin is silent on Windows and remote-execution futures

HC-007 pins Unix domain sockets at `.harmonik/daemon.sock`; HC-007a
pins NDJSON framing; HC-044 consolidates onto that single socket. §2.2
names "cloud execution shapes" and "remote-container shapes" as
post-MVH. HC-052 pins that shape evolution re-implements the adapter,
not the watcher. HC-053 pins the cross-subsystem surface as stable
across shape evolution.

Cloud-execution and remote-container shapes cannot use a local
filesystem socket — the subprocess is on another host. Windows has
AF_UNIX in Win10+ but path and permission semantics differ. HC-053's
"stable across shape evolution" is broken as written: a remote shape
must replace the transport, not just the adapter.

R1 pinned Unix sockets as the MVH transport answer — first-plausible,
defensible on macOS/Linux. But the spec silently forecloses Windows
and post-MVH remote shapes without naming either as a limitation.

Two defensible shapes:

- **(i)** Demote HC-007 to "MVH transport is Unix socket at
  `.harmonik/daemon.sock`; post-MVH shapes MAY substitute a transport
  that preserves NDJSON framing, bidirectional flow, and authenticated-
  connection semantics." Add an OQ for the post-MVH transport
  abstraction.
- **(ii)** Keep the pin; add §2.2 bullet: "Windows and remote/cloud
  shapes are out of scope at MVH; adding them is a breaking change to
  §4.2 requiring foundation amendment per §6.3." Makes the foreclosure
  explicit.

**Load-bearing because**: HC-053's stability guarantee is false as
written. A reader following the architectural-seam argument plans a
cloud-execution shape assuming only the adapter changes, then
discovers §4.2 blocks them.

### Challenge E — NDJSON framing chosen over length-prefixed without naming the failure modes

HC-007a mandates NDJSON: JSON object per line, `\n`-terminated, no
embedded unescaped newlines. Rationale is unstated — presumably
human-readability and jq/grep-friendliness. The alternative never
considered on record is length-prefixed framing (4-byte big-endian
length + JSON payload, no newline rules).

NDJSON has three failure shapes length-prefixed avoids:

- **Embedded-newline bugs.** A handler using `MarshalIndent` or
  hand-assembled JSON produces newlines inside objects. Failure mode
  is silent corruption: message splits, reader sees two invalid half-
  messages. Length-prefixed cannot split content-dependently.
- **Large-message stalling.** A 1 MiB message requires the reader to
  buffer fully before parsing; no early-reject. Length-prefixed reads
  length, decides, then reads content.
- **Protocol fuzz / DoS.** A buggy or hostile handler emits a 256 MiB
  line with no terminator; the line-reader loops to OOM. HC-007a
  states no max-line-length cap.

R1 pinned NDJSON. §A.3 is silent on framing choice.

**Load-bearing because**: a `MarshalIndent` bug (common) silently
corrupts under NDJSON; under length-prefixed, the same bug surfaces
immediately as a frame mismatch. Spec should either (a) add a max-
line-length bound and reject-on-embedded-newline rule to HC-007a, or
(b) document in §A.3 why human-readability outweighs these failures.

## Hidden assumptions v0.2 introduces (not flagged in r1)

1. **HC-026a `phase` enum is pinned at five values with no evolution
   rule.** A sixth phase (e.g., `compacting` for context-window
   compaction) requires amending HC-026a. Declare the enum extensible
   ("handlers MAY declare additional phases in their subsystem
   envelope") or name it a stable contract the way the sentinel set is.

2. **HC-008a `T_shutdown = 10s` vs HC-026a T/2 = 300s cadence.** A
   handler in `shutting_down` phase never emits a `shutting_down`
   heartbeat at T/2 during the 10s window — the phase is pinned in the
   enum but unobservable in practice. Either drop `shutting_down` from
   the HC-026a enum or raise `T_shutdown` enough for at least one
   heartbeat.

3. **HC-044 "filesystem-permission authenticity" without permission
   mode.** `0600` vs `0660` vs `0700` have different threat models.
   Operator-nfr §7.2 may own the number; the pointer from HC-044 is
   absent.

4. **HC-004 concurrent-launch MUST is on caller-side code.** "If a
   second `Launch` arrives while the first's handshake is executing,
   the second call MUST block on the handshake outcome" — phrased as
   a handler obligation, but callers (S01 dispatch path) are the
   audience. Disambiguate.

## Affirmations (decisions that survive both rounds)

1. **HC-048a transient-provisioning split** — adapter-per-agent-type
   classification, bounded backoff (1s/16s/4/60s), distinct
   `provisioning_timeout` field. Cleanest response to R1's concrete
   challenge; parameters are implementable as stated.
2. **HC-035 in-process-fake carve-out** — tightly scoped to three unit-
   test cases; preserves the locked decision without over-forbidding.
3. **HC-026a heartbeat obligation** — moves silent-hang from tunable-
   threshold to mechanical. Challenge B targets cadence math, not shape.
4. **HC-INV-005 no-launch-without-verified-binary-path** — closes the
   hole r1 implementer flagged; makes HC-042+HC-043 observable.
5. **§8 as detection-only; routing delegated to execution-model §8** —
   correct spec-boundary discipline. Challenge A is about taxonomy
   structure, not the ownership split.
