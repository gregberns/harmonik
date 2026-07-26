# 15 — Salvage: what survives the reframe from the previous research round

**Date:** 2026-07-15
**Scope:** `/Users/gb/research/2026-07-15-agent-comms-substrate/` (~11,954 lines across 17 docs), plus its live spike in `/tmp/natspike`.
**Task:** separate the gold from the wreckage so nobody else has to read that folder.
**Answering BRIEF §7 open question 4:** *"Is anything in the previous research recoverable?"*

**Labels used throughout:**
- **[MEASURED]** — I ran a command or a test today, 2026-07-15, and the output is quoted.
- **[CLAIMED]** — the prior round asserted it; I did not re-verify.
- **[DEDUCED]** — reasoning, mine or theirs, not a measurement.

**Jargon defined on first use.** A few terms you will need:
- **NATS** — a message-passing server written in Go. It can be *embedded as a library* inside your own program, so there is no second process to install.
- **Core NATS** — the plain, fast, fire-and-forget part: if nobody is listening, the message is dropped.
- **JetStream** — NATS's optional durability layer: it writes messages to disk so a recipient who was asleep can pick them up later.
- **Raft / quorum** — an algorithm where a *majority* of nodes must agree before a write counts. With 3 boxes, 2 must be alive.
- **Route / cluster** — how NATS servers link as equal peers.
- **Leafnode** — a different NATS link type, designed for edge devices that connect *up* to a more central server.

---

## 0. The verdict, in one paragraph

**Recoverable: yes, and substantially — but almost none of it is the part the prior round thought was the answer.** The salvage splits cleanly along a line the prior round itself named but could not act on: *"measured claims survived; deduced claims got cut"* (`00-README.md:55`). Its **measurements are excellent and hold today** — I re-ran the entire NATS spike and all 7 original tests pass unchanged; I re-verified 8 harmonik line counts to the line; I re-confirmed all three claimed harmonik bugs at HEAD and **root-caused one of them further than the prior round did**. Its **reverse-engineering of harmonik's comms code is the single most valuable artifact in the folder** and is framing-independent — take it wholesale. But its **architecture is void**: it chose a hub on the DGX, which BRIEF §4 disqualifies. And the decisive new fact is that **its chosen topology cannot be un-hubbed** — I tried, and NATS refuses: leafnode links structurally cannot form a mesh (`Loop detected for leafnode account="$G"` → `Protocol Violation`) [MEASURED]. So the prior round's answer is not "right architecture, wrong box"; it is **unfixable under this brief**. The good news is that the same spike, re-run with JetStream turned off, shows **core NATS in a full mesh satisfies BRIEF §4 exactly** — any box may die, survivors keep talking, dead boxes rejoin themselves — and it fails *only* on durability, which **BRIEF §4 already assigns to plugins**. The requirements and the measurements converge.

---

## 1. THE SALVAGE MANIFEST

Bucket key: **A** = survives, measured fact · **B** = survives, framing-independent reasoning · **C** = void, depends on the dead framing · **D** = contested, verdict flips or reopens.

| # | Claim from the prior round | Bucket | Evidence (verified today unless noted) | What to do with it |
|---|---|---|---|---|
| 1 | NATS embeds as a Go library; 2 direct + 11 indirect deps, 15MB binary | **A** | `/tmp/natspike/go.mod` read today; nats-server v2.14.3 + nats.go v1.52.0; `nats-server` v2.14.3 confirmed current release (2026-06-29) via GitHub API [MEASURED] | Keep. The dependency-budget risk is genuinely retired. |
| 2 | Isolated JetStream **cluster** node cannot create a durable consumer | **A** (fact) / **D** (its use) | Re-ran `TestPartitionPinnedLocalStream`: `[!!] local durable consumer while isolated FAILED -> context deadline exceeded` [MEASURED] | **The fact is solid. The conclusion drawn from it is void — see §3.1.** |
| 3 | Isolated leafnode *can* create streams + durable consumers | **A** | Re-ran `TestLeafNodeIndependentDomains`: all 5 assertions pass [MEASURED] | Keep the fact. Its architecture is void (row 8). |
| 4 | Offline delivery works: publish while asleep, drain on wake | **A** | `TestCrossDomainOfflineDelivery` passes: `beta woke on a THIRD box and drained the message sent while it was asleep` [MEASURED] | Keep. This is real and it is what BRIEF §5 asks for. Note it required JetStream. |
| 5 | KV presence keys self-expire with no reaper code | **A** | `TestKVPerKeyTTL`: `t=3.5s EXPIRED by itself -> nats: key not found` [MEASURED] | Keep. Bucket-level TTL verified; per-key TTL still unverified (their own caveat, `20:105`). |
| 6 | 1MB payload cliff is real; Object Store chunks past it | **A** | `raw 8MiB publish -> err=nats: maximum payload exceeded`; `PUT 8MiB ok in 12.9ms: chunks=64` [MEASURED] | Keep as a constraint. Object Store itself: see row 26. |
| 7 | Cross-node core NATS delivery is sub-millisecond | **A** | `CORE CROSS-NODE DELIVERY OK in 250.5µs` … `397µs` across runs [MEASURED] | Keep, with a caveat: this is loopback in-process, **not** over Tailscale. Real fleet RTT is 4–7ms (row 12). |
| 8 | **Topology: leafnodes, hub-and-spoke, DGX as hub** | **C — VOID** | Violates BRIEF §4 ("no single box's death may kill the system"). Their own §4.1 admits: *"this reintroduces a SPOF"* (`20:283`) | **Delete.** And it cannot be repaired — see §3.2, the loop-detection finding. |
| 9 | harmonik line counts: busimpl 1516, jsonlwriter 413, commscursor 323, injector 233, comms.go 1998, subscribe 683, crew/registry 268 | **A** | `wc -l` at HEAD `0553d4b6` — **all exact** [MEASURED] | Keep. Cite with confidence. |
| 10 | pasteinject.go is 2633 lines | **A (rotted, trivially)** | Actually **2632** [MEASURED] | Keep; fix the off-by-one. |
| 11 | Three harmonik bugs at HEAD | **A — all 3 confirmed, one improved** | See §4. All three reproduce today; bug #1 now has a **root cause** the prior round never found [MEASURED] | Keep, and report upstream. Independent of this project. |
| 12 | Fleet: dgx `100.115.27.55`, mini `100.120.22.74`, mbp `100.87.151.114`; direct LAN paths; 8 tailnet nodes but only 3 daemon candidates | **A** | `tailscale status` + `tailscale ping`: `pong from dgx … via 192.168.1.86 in 4ms`, `gb-mac-mini … 7ms`; exactly 5 stale/mobile nodes [MEASURED] | Keep. Latency drifted (they said 6/15ms; now 4/7ms) — same order, fine. |
| 13 | Transcript corpus ~261MB / 789 JSONL / 203 project dirs | **A (grown)** | `266M`, **799** files, 203 dirs [MEASURED] | Keep. It is a live corpus; growth is expected and the number is directionally right. |
| 14 | `gh`/`git`/`rg` installed; `cass`/`syncthing`/`opencode` not | **A** | All 7 confirmed today [MEASURED] | Keep. `cass` still not installed, so every `cass` argument in that folder is still unresolvable. |
| 15 | `~/.claude/CLAUDE.md` does not exist despite being referenced | **A** | `ls` → `No such file or directory` [MEASURED] | Keep as a live papercut. Unrelated to the substrate. |
| 16 | Go stdlib `plugin` package disqualifies itself for live reload | **A** | `go doc plugin` → *"A plugin is only initialized once, and cannot be closed"*, verbatim [MEASURED] | Keep. This is decisive for BRIEF §7 Q2 and it is upstream-documented. |
| 17 | `go-zeromq/zmq4` (pure-Go ZeroMQ) is abandoned | **A** | GitHub API: last commit **2024-06-18**, `pushed_at` 2024-06-18, 392 stars, 31 open issues, not archived [MEASURED] | Keep. Now ~25 months stale. **This is the load-bearing fact for BRIEF §7 Q1** — see §3.4. |
| 18 | ZeroMQ rejected because it has **no durability** | **C — VOID** | BRIEF §4: *"Durability is a PLUGIN decision, not a kernel guarantee."* | **Delete this reason.** It was disqualifying under the old brief; under this one it is a non-issue. ZeroMQ still loses, for a different reason (§3.4). |
| 19 | ZeroMQ rejected because Zyre discovery needs UDP broadcast, which Tailscale lacks | **C — mostly void** | BRIEF §3.1 wants a **baked-in machine roster** (a config file / explicit list), not auto-discovery | **Mostly delete.** We were never going to auto-discover 3 known boxes. The Tailscale-no-broadcast fact itself is **A** and worth keeping (kills mDNS/Zyre generally). |
| 20 | Presence from **transcript growth**, not agent self-report | **B** | Rests on a measured harmonik production bug (row 23), not on the anecdote | **Keep.** Framing-independent and correct. |
| 21 | **Two-clock model**: lease clock (is registration valid?) ≠ activity clock (is it working?) | **B** | `22:17`; rests on the measured OFFLINE bug | **Keep.** This is real engineering. Explicitly named in the task as surviving; I agree. |
| 22 | **"The daemon must not judge"** — publish facts, never scores | **B** | Independently required by BRIEF §2 (*"The system's job is to carry the message. It never judges"*) and §6 | **Keep.** Greg reached this himself. Strongly reinforced, not weakened. |
| 23 | Harmonik presence is outbound-only, so an actively-*receiving* agent reads OFFLINE | **A/B** | Prior round verified in crew `stilgar`; `internal/presence/presence.go` `EffectiveLastSeen = max(beat, last send)` [CLAIMED — I did not re-run the daemon] | Keep. This is the empirical root of rows 20–21. |
| 24 | **Host-owned storage** so plugins don't invent their own state files | **B — reinforced** | `24:48`, `24:75`. **Greg independently reached the same conclusion**: BRIEF §3.1 *"One thing that might be useful is to have a storage mechanism in the daemon. Then the plugins dont make up their own thing."* | **Keep and promote.** Two independent derivations. This is the strongest B-bucket item. |
| 25 | **Cut the plugin system entirely** (`29` O1) | **C — VOID** | BRIEF §3.2 wants plugins, live-loadable, protobuf-defined; §3 says plugins are where the value is | **Delete the cut.** See §3.3 — this is more nuanced than "the critique was wrong". |
| 26 | **Cut live-loading** (`29` O2: *"the user said 'desirable,' not 'required'"*) | **C — VOID** | BRIEF §3.2 quotes Greg: *"In harmonik having to stop the whole system every time is so annoying. Wish we had Erlangs Beam"* | **Delete the cut.** The premise was misquoted requirements. |
| 27 | **Cut request/reply** (`29` A4: *"its one cited use case is a RecordList"*) | **C — VOID** | BRIEF §3.1: *"the internet is based on that - probably a good idea, lol"* | **Delete the cut.** And it works: `cross-node REQUEST/REPLY in 408µs` [MEASURED]. |
| 28 | **Reject the machine roster** (*"zookeeper-like is a shape, not a request"*) | **C — VOID** | BRIEF §3.1: *"The machine roster probably should be baked in."* | **Delete.** Greg wants it in the kernel. Their sentence is a direct contradiction of a stated requirement. |
| 29 | Everything about **overlap detection** (`acs who --touching`, `focus`, `focus_paths`, `related`, the A6 amendment) | **C — VOID** | BRIEF §6: *"That is not my instinct… I really dont even care about that."* | **Delete aggressively.** This shaped a large fraction of `22`, `23`, and `30` §5.5. Note the irony: `29`'s "sharpest finding" (M3, `30:10.1` A6) is an *improvement to a feature that should not exist*. |
| 30 | **"Don't build this — use git + an afternoon of bash"** (Hour Zero / Hour One) and the whole of `31-roadmap.md` | **C — VOID** | This is the conclusion the broken brief was engineered to produce (BRIEF §9). It answers the two dead anecdotes, not BRIEF §2's *"the fleet has no shared memory and no shared address book"* | **Delete the conclusion and the roadmap.** See §3.5 for the one piece worth keeping. |
| 31 | Object Store / blob plane cut entirely (`30` A3) | **C — void reasoning, possibly right answer** | Cut because *"the critic traced every claimed consumer and found zero survivors"* — but the consumer list came from the dead framing | Re-derive. BRIEF §3.2 names *log tail, log archiving* as plugins; they may want bulk. Row 6's measurements stand either way. |
| 32 | `acs search` is ~20 lines of `rg`; RAG ≠ search | **B** | `rg` present [MEASURED]; BRIEF §4: *"We handle search later - we can solve search once we have a data backbone"* | **Keep the distinction** (it is a genuine category error worth not repeating). **Drop the urgency** — Greg explicitly defers search and calls it a consumer. |
| 33 | Metadata (machine, time) must ride with messages because search needs it | **B** | BRIEF §4 says exactly this independently | Keep. |
| 34 | `WallTime` is display-only; never order cross-machine messages by wall clock | **B** | 3 unsynchronized boxes is a physical fact, not a framing artifact | **Keep.** Load-bearing for any design here. |
| 35 | UUIDv7 ordering breaks across 3 boxes (harmonik relies on it in 4 places) | **B** | `extract/10:257`, `:470`; cites `jsonlwriter.go:337`, `busimpl.go:386`/`:1417`, `commscursor.go:248` | **Keep — this is one of the best findings in the folder.** But their *fix* (a single writer = the hub) is void with the hub. Re-derive: see §3.6. |
| 36 | Their fix for ordering: hub-assigned `stream_seq` = one writer | **D** | Verified working (`stream_seq=1`, then `2`) [MEASURED] — but requires the hub | **Reopened.** Without a hub there is no single writer. §3.6. |
| 37 | Durability class must live on the plugin manifest, never a daemon-side map | **B** | Rests on a measured harmonik bug: `fsyncBoundaryEventTypes` hand-maintained map drifted, *silently downgrading durability*. Confirmed present at `busimpl.go:142` [MEASURED] | **Keep.** Strong, framing-independent structural lesson. |
| 38 | Node identity must be a stable opaque id, never an IP/hostname | **B** | Boxes are dual-homed; `tailscale status` shows 8 nodes / 3 candidates [MEASURED] | **Keep — and it matches BRIEF §4**: *"a **daemon-defined name** is the key… box name, IP… hang off that entry."* Independent agreement. |
| 39 | Discovery is a config file, not a protocol, at n=3 | **B** | Tailscale carries no multicast/broadcast [CLAIMED, well-sourced]; 3 known boxes | **Keep.** Aligns with BRIEF §3.1's roster. |
| 40 | The DGX NIC has a TSO/GSO/GRO offload bug; fix unapplied | **A** [CLAIMED-today] | Prior round verified unapplied; I did not re-check (needs sudo on dgx) | Keep as an open item; it gates bulk transfer over the wired LAN. |
| 41 | Two comms systems forever (harmonik + new) is an accepted permanent tax | **D** | Reasonable, but was reasoned against the old scope | Re-derive against BRIEF. Not urgent. |
| 42 | `hashicorp/go-plugin` is alive and current | **A** | v1.8.0 (2026-04-29), pushed 2026-07-06, 6,042 stars [MEASURED] | Keep. Relevant to §3.3. |

---

## 2. THE HARMONIK REVERSE-ENGINEERING — take this wholesale

**This is the most valuable thing in the folder and it does not care about framing.** `extract/10-harmonik-comms-today.md` (545 lines) is a direct source-reading study of harmonik's comms. I re-verified its structural claims at HEAD `0553d4b6` and **every line count was exact**. A re-implementation wants all of the following.

### 2.1 The tmux injector's settle + retry — and why it exists

Harmonik has **two** implementations of "poke a message into a running agent's terminal", and they have diverged.

**The good one — `internal/keeper/injector.go:131` `InjectText`** [MEASURED, read today]:
```
load-buffer → paste-buffer → SETTLE (submitSettle) → send-keys Enter → 2 bounded retries (submitRetryDelay)
```
Its own comment explains the race it fixes:
> *"Settle so the REPL finishes ingesting the pasted text before the submit Enter; otherwise the first Enter races ahead and is dropped (hk-89g)."*

And the retry rationale, which is a nice piece of reasoning:
> *"Bounded retries defend against a dropped first keypress. Failures here are non-fatal — the line is already submitted by the first Enter on the happy path, and a redundant Enter is a harmless empty line."*

It has a **test seam** (`tmuxRunFn` package var) so the sequence can be tested against a fake. It also has `SendEscapeKey` (`:184`) to preempt partial input on a busy pane, and `SetTmuxEnv` (`:224`) which uses `tmux setenv -t` (deliberately not `-g`) so a resumed session inherits env.

**Why the constants exist** — `internal/daemon/pasteinject.go:129` [MEASURED, verbatim]:
> *"on a REMOTE SSH worker under concurrent cold-boots, ~1/3 of runs hang because the seed paste is silently lost. The seed is delivered via `tmux load-buffer` + `tmux paste-buffer` (bracketed paste); tmux returns exit 0 once it has handed the buffer to the pane, **NOT once claude's React/ink TUI has rendered it**. When the TUI reaches input-ready later than the blind 750ms splash wait (common under load), the paste lands on a not-ready TUI and is discarded — claude idles at an empty prompt, never emits agent_ready, and the run burns the full 30-min timeout before failing."*

The fix was to **capture the pane and grep for a marker string** to confirm the paste actually rendered, bounded to `pasteVerifyAttempts = 3` (`:161`) with backoff. `resumeSubmitRetries = 2` (`:114`). `implementerReseedGrace = 75s` (`:753`).

**The lesson, which is the asset:** *tmux exit 0 means "handed to the pane", not "the TUI accepted it."* There is no positive acknowledgement primitive. This is empirical knowledge bought with real failures — 2,632 lines of scar tissue. **Take the knowledge; leave the code** (it is soaked in harmonik's bead/worktree vocabulary).

### 2.2 The `SubscribeHub` back-pressure contract — steal this verbatim

`internal/daemon/subscribe.go:34-42` [MEASURED, verbatim at HEAD]:
> *"The handler performs a non-blocking send into a 256-slot buffered channel. If the channel is full (slow client), the OLDEST queued event is discarded and a drop counter is incremented. On the next successful send to the socket a `subscription_gap` line is emitted carrying the accumulated drop count, then the counter resets. **The bus's emission goroutine is NEVER blocked by a slow subscriber.**"*

Three properties worth naming, because they are the whole contract:
1. **The producer is never blocked by a slow consumer.** No backpressure propagates upstream.
2. **Drops are counted, not hidden.** The subscriber is *told* it missed N messages (`subscription_gap`).
3. **Drop-oldest, not drop-newest.** A slow tailer gets recent data, not stale data.

Plus a server-side heartbeat (clamped `[10,600]`s, default 60) so a quiet stream still wakes the client, and `subscription_gap`/`heartbeat` are **connection-only** — never written to the log. That last detail matters: telemetry about the *transport* must not pollute the *data*.

This is exactly the right contract for a log-tail plugin. Port it.

### 2.3 `jsonlwriter.go` — the best single file, and the durability plugin's answer

`internal/eventbus/jsonlwriter.go`, **413 lines confirmed** [MEASURED]. Prior round's assessment (`extract/10:245`): *"the single best extract-as-is candidate in the repo… zero harmonik semantics."*

The mechanism:
- Opened `O_CREATE|O_WRONLY|O_APPEND` — the kernel positions every write at EOF, so concurrent appends don't interleave.
- A **batching drainer goroutine**: `Append` enqueues onto a 128-slot channel and blocks on a per-call result channel. The drainer dequeues one, non-blocking-drains the rest, **concatenates all lines into ONE `write`**, and issues **one `fsync` if any request in the batch wanted sync**.
- The reasoning (`:42-69`): a mutex held across fsync made P99 latency O(N × fsync); batching makes it O(1 × fsync) per burst, because *"fsync is a barrier over all preceding writes to the fd, per POSIX."*

**Why this matters more now than it did then:** BRIEF §4 says durability is a plugin decision, and §3.1 says the daemon should provide storage so plugins don't invent their own. This file is that storage, already written and already hard-won. See §3.6.

**Known wart to fix on port:** `ScanAfter` (`:312`) — which all the comms read paths use — has **no torn-tail detection**, while `replayAndDetectTrunc` (`busimpl.go:1390`) does. Two readers, two behaviours. A torn tail (a partial last line after a crash) should be a *signal*, not a silently skipped malformed line.

### 2.4 `commscursor.go` — the sleeper hit

`internal/daemon/commscursor.go`, **323 lines confirmed** [MEASURED]. `Advance` already solves the cross-*process* cursor race:
1. Validates the name (rejects `/`, `\`, `\0`, `.`, `..`) so it cannot escape the dir.
2. Takes a **cross-process advisory `flock(LOCK_EX|LOCK_NB)`** on a sidecar lock file, bounded-retry every 25ms up to 10s, then errors rather than hanging.
3. **Re-reads the persisted cursor under the lock** and refuses to write anything not strictly greater — *"the cursor can only ever move forward."*
4. Writes temp → `fsync` → `rename`.
5. A **corrupt** stored cursor is treated as "no usable floor" so a well-formed advance can recover rather than wedging forever.

The lock dir is a *sibling* (`s.dir + ".locks"`), not a child, to preserve "one cursor file per agent, nothing else." Nice discipline.

### 2.5 The message-injection path, end to end

Three distinct delivery paths, and only the third is real injection:
- **Path A — pull.** `comms recv`: socket op → daemon scans the log from the agent's cursor → returns a batch → advances the cursor. **The agent must ask.**
- **Path B — long-poll stream.** `comms recv --follow`: `SubscribeHub` fans events into a per-connection NDJSON stream. **Only wakes an agent that is already blocked reading.** A Claude agent that finished its turn is reading nothing.
- **Path C — tmux paste.** The only thing that makes an *idle* agent act. §2.1.

**The single best design decision in harmonik's comms** (`extract/10:338`): `--wake` injects **only** a fixed nudge string — `"You have a new comms message. Please check your inbox."` — **never the message body**. Delivery and notification are fully decoupled: the log stays the source of truth, the pane poke is only a doorbell. **Preserve this split regardless of transport.**

**Addressing is by naming convention with fallbacks** (`comms.go:403`), and it has been bug-fixed twice in ways worth knowing: the captain has no `crew-` prefix and no registry record (so a third candidate pattern was added), and the project hash **must** be computed on the `EvalSymlinks`-resolved path or the wake silently targets a pane that never existed.

### 2.6 Other framing-independent extraction notes

- **`events.jsonl` is not a log *of* the system, it **is** the system.** Presence, inbox, history, and audit are all *projections* over it. No database, no queue, no mailbox table — so there is almost no state to corrupt, and broadcast is free. `extract/10:66-68` verified there is no mailbox implementation at all: the word "inbox" appears only in prose.
- **`recv` is an O(entire log) linear rescan from the cursor, re-opening the file every call** (`jsonlwriter.go:314`). Invisible for one project; **this is the scaling wall for a central multi-box archive**, and it arrives well before search does.
- **`Seal()` + startup-only `Subscribe`** (`busimpl.go:1004`) is fundamentally incompatible with live-loading plugins — and harmonik already routes around it with the `SubscribeHub`-registered-before-Seal hack. **Directly relevant to BRIEF §3.2.** Dynamic subscription must be first-class.
- **`from` is not authenticated** (`agentcommspayloads_djqc9.go:67`) — an explicit non-goal. Fine on one trusted box, and **still fine** under BRIEF §4's trusted-network scope. Do not let anyone re-open this as a requirement.
- **`transcript_path` already arrives in every hook payload** (`hookrelay.go:45`) and nothing tails it. This is the cheap route to presence-from-transcript-growth (row 20).
- **The hook-relay connection regime is a good spec to steal verbatim** (`hookrelay.go:11`): one-shot NDJSON, dial timeout ≤5s, message ≤1MiB, ack-line read with 5s deadline, then close.
- **Five near-identical emit functions** (~500 of busimpl's 1,516 lines) collapse to one on extraction. `EmitTyped` already subsumes the others.

---

## 3. THE CONTESTED BUCKET — where the verdict actually flips

### 3.1 The cluster experiment: what does it prove *now*?

**The experiment is real and it reproduces today** [MEASURED, `TestPartitionPinnedLocalStream`]:
```
R1 stream placed on: leader="gb-mbp" (want gb-mbp)
AFTER PARTITION: A.routes=0 JSCurrent=true
[OK] PINNED R1 publish while isolated SUCCEEDED -> local durability survives total peer loss
[!!] local durable consumer while isolated FAILED -> context deadline exceeded
```

The prior round's chain was: *creating a durable consumer is what an agent does when it registers → so a clustered design hangs agent startup on an isolated laptop → therefore use leafnodes → leafnodes need a hub → the hub is the DGX.*

**The first link is sound. The last link is now disqualified.** So the honest answer to "what does this experiment tell us now?":

> **It kills clustered JetStream. It does not kill NATS.** The experiment is a finding about **Raft quorum**, not about NATS. Everything that failed — durable consumer create, stream create, R3 publish — failed because it needed a *majority of 3 boxes to agree*, and an isolated laptop has no majority. Every one of those operations lives in **JetStream**, the optional durability layer. **Core NATS never asks anyone's permission.**

And the prior round's own test already contained the evidence, unremarked [MEASURED, `TestPartitionedNodeLocalFunction`]:
```
AFTER PARTITION: A.routes=0
[OK] CORE NATS local delivery still works isolated: "local agent chat"
```

So the question the prior round never asked is: **does core-NATS-full-mesh, with no JetStream at all and durability in plugins (exactly what BRIEF §4 specifies), escape the finding?** I wrote the test. **It does.**

### 3.2 The decisive new experiment — I re-ran the spike and extended it

I added `/tmp/natspike/mesh_test.go`, `meshleaf_test.go`, `failover_test.go`, `meshdebug_test.go`. **All 7 original tests still pass unchanged** (`ok natspike 22.6s`). Here are the new results.

**(a) Core NATS full mesh, JetStream OFF, satisfies BRIEF §4 exactly** [MEASURED]:
```
full mesh formed: a.routes=8 b.routes=8 c.routes=8
[OK] DGX (the proposed HUB) is DEAD -- gb-mbp <-> gb-mac-mini still talk. NO HUB.
[OK] DGX rejoined the mesh automatically and delivery resumed (no operator action)
[OK] laptop asleep -- dgx <-> mini unaffected (contrast: JS cluster wedges)
```
Any box may die; the survivors keep talking; the dead box rejoins itself with zero operator action. **This is precisely "no single box's death may kill the system."**

**(b) A node cold-starts alone, with both peers dead, in 28ms** [MEASURED]:
```
[OK] node booted ALONE in 28.9ms with 2 dead peers configured; routes=0
[OK] isolated cold-start serves LOCAL agents immediately: "alone but working"
```
This directly repairs a constraint the prior round hit and documented (`20:117`): a JetStream **cluster** node *refuses to start* without routes — `Can't start JetStream: JetStream cluster requires configured routes`. **With JetStream off, that constraint vanishes.** The laptop boots at the coffee shop and works.

**(c) Request/reply works cross-node and fails fast when nobody is home** [MEASURED]:
```
[OK] cross-node REQUEST/REPLY in 408µs: "results for: eventbus"
[MEASURED] request to a subject with NO responder -> err=nats: no responders available for request after 310µs
```
BRIEF §3.1 wants request/reply and gives request/reply-shaped examples (*"a search tool on the network"*). It works, it is fast, and — importantly — **`no responders available` is a real, immediate, negative acknowledgement.** That is a direct, free answer to **BRIEF §5's known gap**: *"we actaully should probably notify the sender that the receiver is not listening. I believe we can find that out."* **You can. It costs 310 microseconds and zero lines of code.**

> **An honesty note on my own test.** My first run showed a request *succeeding* after I killed the responder's box (`err=<nil>`), which would have been nonsense. I chased it instead of reporting it. Cause: NATS clusters advertise peer URLs to clients, so the responder's *client* silently reconnected to the surviving node and kept answering — `after DGX death, responder client is now connected to: "nats://127.0.0.1:19422" (reconnects=1)` [MEASURED]. **A test artifact, not a finding** (in the real fleet a plugin dies with its box). Pinned properly, a genuinely absent responder fails in **395µs**. Recording this because the prior round's sin was confident deduction.

**(d) THE BIG ONE — the prior round's topology cannot be un-hubbed.** I tried to build the obvious repair: keep leafnodes (which passed the isolation test) but wire all three boxes to each other so no box is special. **NATS refuses** [MEASURED]:
```
[ERR] Loop detected for leafnode account="$G". Delaying attempt to reconnect for 30s
[INF] Leafnode connection closed: Protocol Violation - Remote: minix
ONE-WAY (A solicits B):     a.leafs=1 b.leafs=1     <- works
C solicits A and B:         c.leafs=0 a.leafs=0 b.leafs=0  <- the whole mesh collapses
```
Note the second line: attempting the mesh didn't just fail to add links, it **tore down the working one**. Leafnodes are structurally a **tree** — an edge-to-core hierarchy — and the server actively enforces acyclicity as a protocol violation.

**This is the single most important salvage finding.** It means the prior round's architecture is not "right idea, wrong box." **Leafnode topology *requires* a hub. It is unfixable under BRIEF §4.** Moving the hub to gb-mac-mini (their open question #2's suggested escape) doesn't help — it's still a hub.

*(Caveat, stated honestly: I tested the default single-account setup, which is what the prior round's design used. Whether a multi-account NATS configuration relaxes leaf loop detection is untested — see open questions.)*

**(e) The honest cost of core-only, measured rather than assumed** [MEASURED]:
```
[MEASURED] publish to an ABSENT subscriber: returned nil error (fire-and-forget, silently dropped)
[MEASURED][EXPECTED] beta woke up and got NOTHING -> nats: timeout
```
Core NATS alone **does not** satisfy BRIEF §5 (*"all the messages are written down… If they are polling, then they read their messages the next time they come in"*). A message to a sleeping agent evaporates, and `Publish` returns `nil` — **it doesn't even tell you.**

**But that is exactly the gap BRIEF §4 hands to plugins:** *"Durability is a PLUGIN decision, not a kernel guarantee: 'How about: the plugin defines that! Then we can have multiple options.'"*

**The convergence is the point.** Three independent lines land in the same place:

| Line of evidence | Lands on |
|---|---|
| BRIEF §4: no hub allowed | JetStream-clustered ✗, leafnode-hub ✗ → **core NATS mesh** |
| BRIEF §4: durability is a plugin decision | kernel must **not** own durability → **core NATS mesh** |
| BRIEF §3.1: daemon provides storage so plugins don't invent their own | plugin durability needs a **host-provided local store** |
| The measurements (a)–(e) | core mesh does transport perfectly, durability not at all |
| harmonik's `jsonlwriter.go` (413 lines, "best file in the repo") | **is** a local durable append-only store, already written |

**Verdict for the next stage:** the surviving NATS shape is **core NATS, full mesh of routes, JetStream OFF, durability owned by a plugin over host-provided local storage.** This is not a compromise — it is the only shape that satisfies the brief, and every measurement supports it. *(This does not by itself settle NATS-vs-alternatives; it settles which NATS.)*

### 3.3 go-plugin: the cut was half-right, and the half it got wrong is the half Greg wants

The task framing says go-plugin was *"cut without considering live reload or protobuf-nativeness."* **The record is more interesting than that**, and the distinction matters:

- **`design/24` DID consider them, at length, and chose go-plugin *because* of them.** It has a whole section (`24:44`, *"The honest cost of live-loading, stated up front"*) that costs out live reload item by item. Its tiebreak argument is genuinely good and survives: *"with go-plugin, items 1–3 are already required by crash-restart, which you must support regardless (a plugin will segfault). Once crash-restart is correct, live-reload is `Kill()` + the same restart path plus an admission check. **Live-reload is ~a day of work on top of crash-restart; it is not a separate feature.**"*
- **`design/29-critique.md` (O1/O2) cut it**, and `30-architecture.md` §10.1 (A1/A2) capitulated.

So who was right? **Split the decision in two, because the critique conflated a mechanism with its ceremony:**

| What was cut | Verdict | Why |
|---|---|---|
| go-plugin itself (subprocess + gRPC) | **REINSTATE** — bucket C on the cut | Cut on grounds of "n=3, one operator, one language" — but that argues against *ceremony*, not against *process isolation*. And the stdlib alternative **disqualifies itself**: *"A plugin is only initialized once, and cannot be closed"* [MEASURED]. If Greg wants live reload in Go, subprocess is the mainstream answer. |
| **protobuf** | **REINSTATE** — bucket C on the cut | The critique cut protobuf as over-engineering. **BRIEF §3.2 requires it**, and Greg's reason is architectural, not cosmetic: *"we can define it COMPLETELY independently of any code. Also could be cool because the same interfaces could be available through REST/pubsub/whatever."* go-plugin is **protobuf-native** — this is a *fit*, not a coincidence. |
| **live-loading** | **REINSTATE** — bucket C on the cut | Cut because *"the user said 'desirable,' not 'required'"* (`29` O2). Under this brief he said *"having to stop the whole system every time is so annoying."* The cut's premise is simply false now. |
| AutoMTLS between a daemon and its own child on loopback | **CUT STANDS** — bucket B | The critique's mockery is correct **and framing-independent**: *"design 24's own threat model is 'my own buggy plugin' — AutoMTLS does not defend against your own bug."* **BRIEF §4 independently says trusted network.** Kill it. |
| The 12-state lifecycle machine, per-plugin ACLs, circuit breakers, QUARANTINED, sha256 admission, generational sealing | **CUT MOSTLY STANDS** — bucket B | For 3 boxes, one operator, first-party plugins. BRIEF §6 explicitly rejects HA/consensus rigor. Keep the **manifest** (row 37 — it is load-bearing) and drop the ceremony. |
| *"`exec.Command` is the plugin ABI for Rust"* | **CUT STANDS, and it's a good line** | Language independence didn't need gRPC. Still true. |

**Net verdict: reinstate the mechanism (subprocess + protobuf + live reload), keep the critique's cut of the ceremony (mTLS, state machine, ACLs, breakers).** The critique was right that ~half of `design/24` was fat. It was wrong about which half. **That's the most useful sentence in this section:** an adversarial reviewer optimizing against a brief that omitted three of Greg's stated requirements will reliably cut exactly those three requirements — and it did (plugins, live reload, request/reply).

One genuinely strong argument from `24:36` that **survives and is worth carrying forward**: with subprocess plugins, "no cognition in the daemon" becomes a *structural* property rather than a coding rule — a plugin that shells out to an LLM is a different process **by definition**. Given BRIEF §2's *"The system's job is to carry the message. It never judges"*, that is an enforced invariant instead of a wish. **That argument got stronger under the new brief, not weaker.**

### 3.4 ZeroMQ (BRIEF §7, Q1) — the rejection survives, but only one of its three pillars does

Greg asked twice and leans toward it. The prior round rejected it on three grounds. Under the reframe:

| Pillar | Status |
|---|---|
| 1. **The Go story is bad** | **SURVIVES — and it is now the whole argument.** [MEASURED today] `go-zeromq/zmq4` (the pure-Go port): last commit **2024-06-18**, ~25 months stale, 392 stars, 31 open issues. |
| 2. **No discovery; Zyre needs UDP broadcast, dead on Tailscale** | **MOSTLY VOID.** BRIEF §3.1 wants a **baked-in roster** — an explicit list. We were never auto-discovering 3 known boxes. |
| 3. **No persistence / no delivery guarantees** | **VOID.** This was the killer under the old brief. BRIEF §4: *"Durability is a PLUGIN decision, not a kernel guarantee."* ZeroMQ not having durability is now **aligned with the requirement**, not against it. |

**So the honest position: ZeroMQ's rejection now rests almost entirely on the health of its Go bindings.** That is a much narrower argument than the prior round made — and it is worth stating plainly to Greg, because pillar 3 was the one that sounded most damning and it is the one that evaporated.

**I checked the alternative the prior round never assessed.** It only evaluated the pure-Go port. The mainstream binding is **`pebbe/zmq4`**, which uses **cgo** (Go calling C code) [MEASURED]: 1,259 stars, last commit **2025-07-06** (~12 months), recent commits are README-only — maintenance mode, not dead. Upstream `zeromq/libzmq` C++ is genuinely alive (10,936 stars, pushed 2026-07-04). But cgo costs:
- **`libzmq` is not installed on this box** [MEASURED] — it becomes a C dependency to install and version-match on all 3 boxes (2 macOS + 1 ARM Linux).
- It **kills Go's static-binary property** and complicates cross-compilation — directly against BRIEF §4's *"I'd kinda like to stick with go."*

**Verdict: the rejection holds, for a better and narrower reason than was given.** ZeroMQ is an excellent library for *building* a broker; Greg doesn't want to build a broker, he wants to build plugins. Its "brokerless, no daemon" selling point is **already delivered by embedded NATS** — which is itself brokerless in the sense that matters here: the daemon *is* the server, there is no separate process to install (`main.go` runs it with `DontListen: true`, zero sockets, verified). And critically, **the not-centralized property Greg wants from ZeroMQ is available from core NATS in a full mesh** (§3.2a) — which was the real reason he leaned away from NATS. **That premise is now measured false.** That is the single most useful thing to tell him about Q1.

*(Not fully settled here: this compares ZeroMQ to NATS only. Whether some third option beats both is out of this doc's scope.)*

### 3.5 The "don't build this, use git" conclusion

**VOID as a conclusion.** It was reverse-engineered from a brief built to elicit it (BRIEF §9). It answers "how do I find a coding-principles doc" and "two agents collided once" — not BRIEF §2's *"the fleet has no shared memory and no shared address book"*, and not §3's kernel-plus-plugins vision. The entire `31-roadmap.md` (359 lines) is downstream of it: **discard**.

**One piece survives, and it's a measurement worth keeping** (`design/26`): the prior round *corrected an earlier extract* by measuring. `extract/14` claimed "5+ divergent AGENTS.md"; `design/26` showed the three gitnaut checkouts hold a **byte-identical** `AGENTS.md` because they are three clones of one remote — *"There is no drift where git is present. The drift is exactly and only where git is absent."* [CLAIMED — I did not re-verify the md5s]. That is a good epistemic datapoint about the *corpus*, and it is a fair warning that some sync problems are already solved. It is not an argument against a substrate.

**Also worth noting as a caution, not a conclusion:** their honest observation that a 1ms doorbell which hangs ~1/3 of the time is not obviously better than a 60s poll that always works. That tension is real (§2.1's measurements are the proof) and the next stage should be aware of it — **but it argues about the *doorbell mechanism*, not about whether to build a substrate.**

### 3.6 Ordering without a hub — genuinely reopened

`extract/10:257` identified the deepest technical issue, and it holds: **UUIDv7 sorts by wall clock**, harmonik relies on `bytes.Compare` over event ids in **4 load-bearing places**, and *"across 3 daemons on 3 boxes there is no such guarantee — two events minted on skewed clocks will sort by skew, not causality."*

The prior round solved it with **a single writer** — the hub's stream leader assigning `stream_seq` (verified working, `stream_seq=1` then `2` [MEASURED]). **With the hub gone, that solution goes with it.** This is genuinely reopened and it is the sharpest open technical question the salvage hands forward.

The extract's own menu (`extract/10:470`) had three options; option (c) is dead, so:
- **(a) One log per node + per-node cursors.** No merged total order — each node's log is totally ordered by its own single writer, and cross-node is explicitly concurrent. This is the natural fit for a not-centralized design and for per-node `jsonlwriter` durability.
- **(b) A logical clock** (Lamport/vector). Real causality, more machinery.

I lean strongly to **(a)** — it composes with everything else that survived (per-node local durability, no hub, BRIEF §6's "no CAP rigor") — but I am labelling this **[DEDUCED]**, not measured, and flagging it as the top open question. What *does* survive as a hard rule, framing-independently: **never order cross-machine messages by wall clock; `message id` is an identity/dedupe key, not an ordering key** (rows 34–35). And `in_reply_to`/`thread_id` survive as the **only** expressible happens-before once id-ordering and wall-clock-ordering are both banned — the critique tried to cut them (`29` O8) and `30` §10.3 R3 correctly rebutted it. That rebuttal stands.

---

## 4. THE THREE HARMONIK BUGS — all confirmed at HEAD, and one is worse than reported

These are independent of this project and worth reporting upstream. HEAD `0553d4b6`.

**Bug 1 — `DeriveCIaudeTranscriptPath` produces paths that do not exist.** [MEASURED — and I found the root cause the prior round missed]

The prior round said it *"silently produces non-existent paths"* and located it at `:703`; it is actually at `internal/handler/claudehandler_chb006_024.go:696` (minor drift). **I compiled the function verbatim and ran it against the real corpus. 0 of 3 derived directories exist. 3 of 3 corrected ones do:**

```
workspace: /Users/gb/github
  derived dir: /Users/gb/.claude/projects/Users-gb-github     EXISTS? false
  actual dir:  /Users/gb/.claude/projects/-Users-gb-github    EXISTS? true
```

**Two independent defects, neither previously identified:**
1. **`strings.TrimPrefix(slug, "-")` strips a leading hyphen that Claude Code actually keeps.** Every one of the 203 real project dirs begins with `-` [MEASURED]. The line is not just unnecessary, it is exactly backwards.
2. **Dots are never replaced.** Claude maps `.` → `-`; the function only maps `/` and space. So `/Users/gb/harmonik-worker/repo/.harmonik/worktrees/…` needs `…repo--harmonik-worktrees…` (double hyphen) and the function emits `…repo-.harmonik-worktrees…`. **A second, independent reason the same path fails.**

**Why it survived:** its two tests assert only `HasSuffix(path, sessID+".jsonl")` and `Contains(path, "projects")` [MEASURED, `claudehandler_chb006_024_test.go:1211-1230`]. **Neither test ever touches the filesystem**, so a function whose entire job is to produce a real path is tested without checking that any path is real. The prior round flagged the weak test; I'm adding that the fix is one line (`os.Stat`) and it would have caught both defects.

**Bug 2 — `commsInjectTmuxPane` lacks the settle+retry its own comment claims.** [MEASURED — confirmed verbatim]

`cmd/harmonik/comms.go:448` fires `load-buffer` → `paste-buffer` → `send-keys Enter` with **zero delay and zero retries**, inheriting exactly the hk-89g race that `keeper.InjectText` was written to fix. **And the comment directly above it says** (`comms.go:440`):
> *"commsInjectTmuxPane delivers text into a tmux pane via the bracketed-paste mechanism (tmux load-buffer → paste-buffer → send-keys Enter), **the same approach used by keeper.InjectText**."*

It is **not** the same approach. `keeper.InjectText` has a settle and 2 bounded retries; this has neither. The comment asserts a parity that does not exist — which is worse than the bug, because it tells the next reader not to look. **The prior round's characterization is exactly right and I confirm it to the character.**

**Bug 3 — presence is outbound-only, so an actively-receiving agent reads OFFLINE.** [CLAIMED by the prior round; verified by them in crew `stilgar`, not re-run by me]. This is the empirical foundation of the two-clock model (row 21), which is why that model is bucket B and not bucket C.

---

## 5. WHAT TO DO WITH THE FOLDER

| Action | Files |
|---|---|
| **Read for mechanism, ignore all conclusions** | `extract/10-harmonik-comms-today.md` — **the one genuinely valuable doc.** §2 above distils it; the original is worth reading for the port. |
| **Mine for measurements only** | `design/20` §2 (the spike), `design/25` (corpus measurements), `design/26` §1 (on-disk ground truth), `extract/15` §2.5 (ZeroMQ facts) |
| **Keep the reasoning, drop the topology** | `design/22` (two-clock model, presence-from-observation), `extract/13` (the "daemon must not judge" rules) |
| **Read as a cautionary tale** | `design/29-critique.md` — well-argued and **wrong where it matters**, because it optimized against a brief missing 3 of Greg's requirements. Its ceremony cuts are right; its mechanism cuts are void. |
| **Discard entirely** | `31-roadmap.md`, `00-README.md`, `30-architecture.md` §10.2, everything about overlap detection / `focus` / `related` |
| **The live spike — keep and extend** | `/tmp/natspike` — **all 7 original tests pass today**; I added 4 files with the reframe experiments. This is a working NATS testbed. |

**Spike inventory** (`/tmp/natspike`, module `natspike`, go 1.26.2, all passing today):

| File | Origin | What it establishes |
|---|---|---|
| `cluster_test.go`, `crossdomain_test.go`, `kv_test.go`, `leaf_test.go`, `partition_test.go`, `partition2_test.go`, `main.go` | prior round | Original 7 tests — **all pass unchanged** |
| `mesh_test.go` | **new** | Core-mesh no-hub, cold-start alone, request/reply, the honest cost of fire-and-forget |
| `meshleaf_test.go` | **new** | Leafnode mesh is impossible (skips with the measured reason) |
| `meshdebug_test.go` | **new** | Isolates the `Loop detected` protocol violation |
| `failover_test.go` | **new** | Proves the request-after-death anomaly was client failover, not durability |

---

## 6. OPEN QUESTIONS I COULD NOT CLOSE

1. **Ordering without a single writer** (§3.6). The prior round's answer died with the hub. Per-node logs + per-node cursors is my [DEDUCED] lean; it is not measured. **This is the top open technical question in the salvage.**
2. **Does a multi-account NATS config relax leafnode loop detection?** I measured the default single-account case (what their design used) and it is a hard protocol violation. If accounts change this, the leafnode option partially reopens — though it would still be a tree, so I doubt it rescues BRIEF §4.
3. **Does core NATS route-mesh survive a real Tailscale partition** (not `Shutdown()` on loopback)? My tests kill processes; a network partition with half-open TCP connections may behave differently, and the laptop-sleeps case is exactly that. **Cheap to test on the real fleet; worth doing.**
4. **What does the durability plugin actually store, and where?** `jsonlwriter.go` is the obvious answer (413 lines, already written, already correct) but I did not design the plugin.
5. **Bug 3 (presence outbound-only)** — I did not re-run harmonik's daemon to reconfirm it.
6. **The DGX `ethtool` offload fix** — still needs sudo on the dgx; I did not re-check (row 40).
7. **`cass` is still not installed** [MEASURED], so every `cass`-dependent argument in that folder remains unresolvable.
8. **Sub-64KiB vs bulk** — Object Store was cut (row 31) using the dead framing's consumer list. Log-tail/archive plugins may want it; needs re-derivation.

---

## Sources

**Prior research folder — files read** (all under `/Users/gb/research/2026-07-15-agent-comms-substrate/`):
`00-README.md` (full, 117 lines) · `30-architecture.md` (§10 in full + greps) · `31-roadmap.md` (via README/architecture summaries + greps) · `design/20-transport-layer.md` (lines 1–627 of 820) · `design/21-daemon-mesh-membership.md` (greps) · `design/22-agent-registry-presence.md` (greps) · `design/23-comms-extraction.md` (greps) · `design/24-plugin-system.md` (greps) · `design/25-log-capture-and-archive.md` (via README/architecture) · `design/26-cross-project-knowledge-sync.md` (head, 30 lines) · `design/29-critique.md` (greps) · `extract/10-harmonik-comms-today.md` (full, 545 lines) · `extract/11`, `extract/12`, `extract/13`, `extract/14` (via citations in the above) · `extract/15-external-tech-survey.md` (greps)

**Authoritative brief:** `/Users/gb/research/2026-07-15-agent-substrate-v2/BRIEF.md` (full)

**Harmonik source read directly** (`/Users/gb/harmonik-worker/repo`, HEAD `0553d4b6`, `.harmonik/worktrees/**` ignored):
`internal/keeper/injector.go` (125–185) · `cmd/harmonik/comms.go` (418–470) · `internal/daemon/pasteinject.go` (126–151) · `internal/daemon/subscribe.go` (30–45) · `internal/handler/claudehandler_chb006_024.go` (680–730) · `internal/handler/claudehandler_chb006_024_test.go` (1206–1232) · `internal/eventbus/busimpl.go` (greps) · `internal/eventbus/jsonlwriter.go` · `internal/daemon/commscursor.go`

**Commands run today (2026-07-15), all on `gb-mbp`:**
- `git log -1` → `0553d4b6 Sat Jul 11 08:21:41 2026`
- `wc -l` over 8 harmonik files (all counts confirmed; pasteinject 2632 vs 2633 claimed)
- `go version` → `go1.26.2 darwin/arm64`
- `cd /tmp/natspike && go test ./... -v` → **all 7 original tests PASS** (`ok natspike 22.643s`)
- `go test -v -run TestCoreMesh` → 4 new tests PASS
- `go test -v -run TestFullMeshLeaf` / `TestWhyLeafMeshFails` → `Loop detected for leafnode account="$G"` → `Protocol Violation`
- `go test -v -run TestClientAutoFailover` → `reconnects=1`, explaining the anomaly
- `go doc plugin` → *"A plugin is only initialized once, and cannot be closed"*
- `go run /tmp/slugcheck/main.go` → 0/3 derived transcript dirs exist; 3/3 corrected ones do
- `tailscale status`, `tailscale ping -c 2 100.115.27.55` (4ms), `… 100.120.22.74` (7ms)
- `du -sh ~/.claude/projects` → `266M`; `find … -name '*.jsonl' | wc -l` → `799`; `ls ~/.claude/projects | wc -l` → `203`
- `ls ~/.claude/projects | grep -c '\.'` → `0` (no real project dir contains a dot)
- `command -v` for `gh`,`git`,`rg`,`cass`,`syncthing`,`opencode` · `ls ~/.claude/CLAUDE.md` → missing
- `ls /opt/homebrew/lib/libzmq*` → not installed

**GitHub API (via `gh api`), queried today:**
- `repos/go-zeromq/zmq4` → last commit `2024-06-18T07:04:44Z`, 392 stars, 31 open issues, not archived
- `repos/pebbe/zmq4` → 1,259 stars, pushed `2025-07-06T13:32:46Z`, not archived
- `repos/zeromq/libzmq` → 10,936 stars, pushed `2026-07-04T16:32:21Z`
- `repos/hashicorp/go-plugin` → v1.8.0 (`2026-04-29`), pushed `2026-07-06`, 6,042 stars
- `repos/nats-io/nats-server/releases/latest` → `v2.14.3` (`2026-06-29`)

**Files I created (experiment inputs, not reports):**
`/tmp/natspike/mesh_test.go` · `/tmp/natspike/meshleaf_test.go` · `/tmp/natspike/meshdebug_test.go` · `/tmp/natspike/failover_test.go` · `/tmp/slugcheck/main.go`
