# ROADMAP — building the agent substrate

**Date:** 2026-07-16
**Companion to:** `ARCHITECTURE.md` (which is the design; this is the order).
**Names used throughout:** **`fleetd`** = the daemon, one per box, contains the kernel. **`fleetctl`** = the command-line tool. **Plugin** = a separate ordinary program `fleetd` launches as a child.

---

## 0. The shape of this plan

Two rules govern the order, and both come from BRIEF §9's post-mortem of the last round:

1. **Prove the risky claim before building on it.** The kernel/plugin split is the whole bet. If it doesn't survive first contact, everything downstream is wasted. So the walking skeleton exists to *attack* the split, not to demo it.
2. **Every milestone ends in a demo Greg can watch, not a document he has to trust.** Where a milestone can't be demoed, it isn't a milestone.

**Everything in Milestone 0 is measurement. No production code is written until it passes.**

---

## 1. Milestone 0 — the overnight sleep test

> **This is the only thing on the critical path before code. Half a day of work, one overnight wait. Do not skip it.**

**Why it's first.** Three of the five subsystem designs independently flagged the same #1 risk, and **none of them closed it**: *nobody has tested a real macOS sleep.* One design built a blackhole proxy, one froze an HTTP handler, one used `SIGSTOP`. **None of those is a sleeping laptop**, where `utun0` goes down and the Tailscale IP is withdrawn. BRIEF §1 says the laptop sleeping is the fleet's **normal state**, not an edge case.

**Goal:** learn what actually happens to a NATS route and a TCP probe when a real lid closes and reopens.

**Deliverable:** a ~150-line throwaway harness — an embedded core NATS mesh (`JetStream: false`, `Cluster.PingInterval = 2s`) plus a 1s TCP probe loop — running on gb-mbp, dgx, and gb-mac-mini. Logs timestamped to disk on the two always-on boxes. Then `pmset sleepnow` on the laptop, wait overnight, open the lid.

**The demo:** a table with four numbers, read off the dgx's log the next morning.

| Question | Why it changes the design |
|---|---|
| How long until dgx's NATS declares the route dead? | Validates or moves `Cluster.PingInterval=2s`. Modelled: 5.0s. |
| How long until the probe loop says `DEAD`? | Validates or moves the 6-probe threshold. Modelled: ~6.5s. |
| On wake, does macOS send RSTs (fast detect) or leave peers hanging? | Unknown. Decides whether wake needs an explicit rejoin. |
| Does the mesh re-form by itself, with no restart and no `Join()`? | **If no: the whole "laptop is a peer" premise needs rework.** |

**Kill criteria:**
- **If the mesh does not re-form on wake without a restart** → stop. Do not build the transport as designed. Re-open the mangos-vs-NATS decision, because "survives a sleeping peer" is a requirement, not a preference.
- **If route-death detection is >30s even at `PingInterval=2s`** → the `Interest` signal is useless on this fleet. It stays in the API as `INTEREST_UNKNOWN`-by-default, and the comms plugin's end-to-end ACK becomes the *only* mechanism (which the architecture already says it is — but this would remove the cheap rungs 2 and 3 of the ladder).

**Cost if we skip it:** we discover this after the comms plugin is built on top of it, which is exactly how the last round wasted its work.

---

## 2. The walking skeleton

> **The smallest thing that proves the kernel/plugin split works end to end across two boxes — and can be shown to be *false* if it doesn't.**

### 2.1 Definition

**Two boxes: gb-mbp (laptop) and dgx.** Not three — the third box adds no new failure mode at this stage and costs a `scp`.

**`fleetd` contains exactly this and nothing else:**

| Component | Scope for the skeleton | Deferred |
|---|---|---|
| **Transport** | Embedded core NATS, `JetStream: false`, full mesh from a config file, `Cluster.PingInterval=2s`, bound to the Tailscale IP | — |
| **Kernel API** | **4 of the 19 RPCs**: `Publish`, `Subscribe`, `JournalAppend`, `Info`. ConnectRPC over `net/http`, loopback. | the other 15 |
| **Storage** | SQLite (`modernc.org/sqlite`), journal only, `sync` honoured | KV, Watch, Truncate |
| **Roster** | Config file + 1s probe loop + `ALIVE`/`DEAD` | `SUSPECT`, intent, watch stream |
| **Plugin host** | go-plugin: verify sha256 → pre-warm → exec → `Describe` → validate manifest → `Start`; **plus the drain gate** | crash budget, backoff, library |

**One plugin: `echo`.** It is deliberately trivial and deliberately domain-shaped. Its manifest declares one channel (`echo.ping`, PUBSUB) and one interest. On each message it calls `JournalAppend(journal="seen", records=[payload], sync=true)`. **That is the entire plugin.**

**Line budget: ~1,200 lines of `fleetd`, ~80 lines of `echo`.** If `fleetd` is over ~2,000, the kernel is already growing domain knowledge and the skeleton has failed its own test.

### 2.2 The demo — four commands, and one of them must fail

Run in front of Greg. Takes about three minutes.

```bash
# (1) THE SPLIT WORKS ACROSS MACHINES
#     On gb-mbp — an agent with no SDK, no library, no client code:
curl -X POST http://127.0.0.1:7947/fleet.kernel.v1.KernelService/Publish \
  -d '{"channel":"echo.ping","payload":"aGVsbG8gZnJvbSB0aGUgbGFwdG9w"}'
#     On dgx:
fleetctl journal read seen
#     -> "hello from the laptop"
#
#     Bytes crossed a machine. The kernel never knew what they were.
#     PROOF, not assertion: `grep -riE 'echo|ping' internal/kernel/` -> 0 hits.

# (2) LIVE RELOAD, UNDER LOAD — the thing harmonik can't do
#     On gb-mbp, publish 100 msg/sec continuously. On dgx, mid-stream:
fleetctl plugin reload echo          # <- points at a NEW echo binary
#     -> reload completes in single-digit ms
#     -> `fleetctl journal read seen | wc -l` == exactly what was sent. ZERO loss.
#     -> fleetd's PID never changed.
#
#     THIS IS THE MONEY DEMO. It is BRIEF §3.2 in one command.

# (3) NO BOX IS IN CHARGE
kill fleetd on gb-mbp
#     -> dgx keeps serving its own agents. Publishes locally. Journals. Fine.
#     -> restart it: mesh re-forms with no election, no seed, no waiting.
#     Then the sharper version:
start a THIRD fleetd on gb-mac-mini with BOTH peers dead
#     -> it boots in ~28ms and serves its local agents immediately.

# (4) THE FAILURE THAT MUST HAPPEN — and this is the honest one
#     Sleep the dgx. On gb-mbp:
curl ... Publish -d '{"channel":"echo.ping","payload":"..."}'
#     -> returns 200. message_id assigned. AND THE MESSAGE IS GONE FOREVER.
#
#     This is at-most-once, working as designed. It is NOT a bug.
#     It is the exact reason the comms plugin (M4) is not optional polish.
#     Greg must SEE this, in the skeleton, before anyone builds on top of it.
```

### 2.3 What the skeleton proves — and what it deliberately does not

**Proves:**
1. **The kernel/plugin line is real** — mechanically, by grep, not by intention.
2. **The line survives a machine boundary** — the plugin on dgx never knew the sender was remote.
3. **Live reload is real and lossless under load** — the drain gate works or it doesn't.
4. **No box is in charge** — verified by killing boxes, including all-but-one.
5. **The whole toolchain is real** — proto → generated Go → ConnectRPC → cross-compiled to the dgx with cgo off. *(Already verified: the reconciled proto lints, builds, generates, and cross-compiles today.)*

**Deliberately does not prove:** durability (M4), name resolution (M3), liveness subtlety (M2), or that any of this is *useful* (M4). **The skeleton is a structural test, not a product.**

### 2.4 Kill criteria for the skeleton

- **`fleetd` exceeds ~2,000 lines** → the kernel is absorbing domain knowledge. Stop and re-cut the boundary.
- **The boundary test or the dependency-allowlist test needs an exception to pass** → the boundary is wrong. **An exception is a design failure, not a build failure.**
- **Reload demo (2) loses a single message under a 100/sec load** → the drain gate is wrong. Fix before anything else; this is the one property everything else assumes.
- **`echo` needs *any* kernel RPC that isn't in the 19** → the API is incomplete and M4 will be worse. Find out now.

---

## 3. Milestones

Each: **goal / deliverable / demo.** Ordered by dependency, not by appetite.

### M1 — Walking skeleton
**Goal:** prove the split. **Deliverable:** §2. **Demo:** §2.2, four commands, including the one that must fail.

---

### M2 — The roster and the address book
**Goal:** BRIEF §3.1's *"that one 'thing' that keeps track of 'whos where'"*, baked in.

**Deliverable:** the full probe loop (~400 lines): `ALIVE`/`SUSPECT`/`DEAD`/`UNKNOWN` as pure functions of a local counter; `boot_id` for restart detection; `RosterList` + `RosterWatch`; the sleep-announce hook (`INTENT_SLEEPING`); one address per box (the Tailscale IP), never DNS. **Not memberlist** — see `ARCHITECTURE.md` §9 row 1.

**Demo:**
```
fleetctl roster
  NODE          ADDRESS          STATE    REASON            LAST SEEN
  gb-mbp        100.87.151.114   ALIVE    -                 0.3s
  dgx           100.115.27.55    ALIVE    -                 0.6s
  gb-mac-mini   100.120.22.74    DEAD     ANNOUNCED_SLEEP   3h12m
```
Then: close the laptop lid → dgx shows `DEAD(ANNOUNCED_SLEEP)` in **~0.6s**. Kill `fleetd` with `-9` (no hook) → `DEAD(PROBE_TIMEOUT)` in **~6.5s**. Reopen → `ALIVE` in **≤1s**, with no `Join()`, no re-registration, no operator action.

**Kill criterion:** if M0's overnight numbers contradict the 1s/6-probe thresholds, retune here **before** M4 depends on them.

---

### M3 — LOOKUP: the one replicated thing
**Goal:** the shared address book (BRIEF §2's *"a 'shared' address book"*).

**Deliverable:** `LookupPut`/`LookupGet`/`LookupList` (~150–250 lines). **Single-writer-per-key by construction**: a `LookupPut` stamps `writer_node = me`; another node writing the same key name creates a *separate* entry. No conflict, so no resolver, so **no CAP argument exists**. `revision` monotonic per writer, compared only within one writer.

> ⚠️ **This is the riskiest code in the project — the only component with no upstream to lean on**, and everything routes through it. Budget accordingly. Also: **this is where `ARCHITECTURE.md` §11's open question 2 must be resolved** (persist the revision counter; force a fresh incarnation on disk loss) — resolve it *in code*, here, not in a comment. **Reuse M2's `boot_id`**: the roster already mints a fresh `boot_id` per daemon start, which is exactly the incarnation signal LOOKUP needs. Make the writer identity `(writer_node, boot_id, revision)`; a disk wipe yields a new `boot_id`, so peers stop trusting the old counter automatically. ~10 lines, not a new subsystem.

**Demo:**
```
# On dgx:          fleetctl lookup put registry.agents dgx/refactor-3 '{"pid":4412}'
# On gb-mbp:       fleetctl lookup get registry.agents dgx/refactor-3
#   -> value + writer_node=dgx, ~4ms after the write.

# Then the demo that matters — the name clash:
# On gb-mbp:       fleetctl lookup put registry.agents dgx/refactor-3 '{"pid":9}'
# On gb-mac-mini:  fleetctl lookup get registry.agents dgx/refactor-3
#   -> TWO entries. writer_node=dgx AND writer_node=gb-mbp.
#   -> The kernel picks NEITHER, and says so.
#   That is BRIEF §2's "it never judges", visible in a terminal.
```

---

### M4 — The comms plugin ⭐ **← harmonik migrates here**
**Goal:** BRIEF §2's first-class need — *"I need them to send messages back and forth"* — and BRIEF §5's model and its known flaw.

**Deliverable:** the comms plugin. Owns durability, retry, de-dup, the outbox/inbox, the end-to-end ACK. Uses `JournalAppend(sync=true)` + `Publish` + `JournalRead(after_seq)` + `KVPut` (cursors) + `LookupGet` + `RosterWatch`. **Store-and-forward:** sender writes its own disk, then transmits; receiver writes its own disk, then ACKs.

**This is the milestone that makes BRIEF §5 true, and it is where the skeleton's demo-(4) message-loss stops happening.**

**What to port from harmonik — specifically:**

| Port | Into | Why |
|---|---|---|
| `jsonlwriter.go` (413 lines, *"best file in the repo"*) — the batching drainer | **the kernel's journal** | One write + one fsync per burst: P99 becomes O(1×fsync), not O(N×fsync). The naive fsync-per-append in the prototypes will not hold at logtail volumes. |
| `injector.go`'s settle+2-retries sequence | comms | **Not `comms.go:448`**, which lacks both while claiming parity. |
| `SubscribeHub`'s drop-oldest + count back-pressure contract | the kernel | Producer is **never** blocked by a slow consumer; drops are **counted and reported**, never hidden. Harmonik converged on this independently. |
| `--wake` injecting only a fixed nudge, never the body | comms | The log is truth; the pane poke is a doorbell. Harmonik's best decision. Keep it verbatim. |

**What to delete, not port:**
- **`commscursor.go` (323 lines) — deleted entirely** by `KVPut(if_revision=…)`. The daemon becomes the single writer; the file-based cursor bug class goes away.
- **The `fsyncBoundaryEventTypes` map — deleted.** It drifted and **silently downgraded durability**. `sync` is a per-call argument now, never a daemon-side policy map.
- **`bytes.Compare` over UUIDv7 ids in 4 load-bearing places — deleted.** It sorts by clock skew, not causality. There is no fleet-wide order and no API for one.
- **The vendor path deriver — deleted.** It produces 0-of-3 correct paths today. Discover by watching directories; never derive.

**Demo — the money demo of the whole project:**
```
Agent on gb-mbp -> "dgx/refactor-3", while the dgx is ASLEEP:
  QUEUED — dgx is asleep (announced 3h12m ago). Your message is on disk in
  the outbox and will deliver when dgx wakes. Nothing was lost.
     ^ returned in MICROSECONDS. No TCP timeout. No hang. BRIEF §5, answered.

Open the dgx's lid. Within ~1s, unprompted, the outbox drains.
The agent on the dgx gets its message. The sender gets:
  DELIVERED, but receiver stale — 47 unread, last read 3h12m ago.
     ^ Rung 4. Actionable. All mechanical facts, no judgement.

Then, mid-conversation:  fleetctl plugin reload comms
     -> the conversation does not notice. fleetd's PID never changed.
```

**Kill criteria:**
- **comms needs a kernel RPC that isn't in the 19** → the boundary is wrong. Stop and re-cut it. *This is the real test of the whole architecture, and M4 is where it is applied.*
- **comms needs to hold in-memory state across a reload** → the "plugins are stateless" rule is broken, and live reload is not safe. Four independent derivations say it shouldn't; find out here.
- **The migrated comms is slower or less reliable than harmonik's today** → we have gone backwards. Fix or stop.

---

### M5 — The registry plugin
**Goal:** BRIEF §3.2's *"The idea of an agent list could probably also be a plugin"* — proving the kernel needs zero agent knowledge.

**Deliverable:** agent names as `<node>/<local>` (hierarchical, so clashes are *deleted*, not resolved). Registration is the **union of three mechanical routes** (Claude hooks, directory discovery, launcher-supplied `tmux_target`) and **never self-declared**. Names keyed on `(node, tmux_target)`, **not** `session_id`, so they survive `/clear`. Presence from transcript growth. Summary from the **vendor's own** `ai-title` line (measured in 55/60 recent transcripts) with a **mandatory `source` field so nobody can later slip cognition in.**

**Demo:** `fleetctl agents` from gb-mbp lists agents on all three boxes with what each is doing — **and `grep -riE 'agent|tmux|claude' internal/kernel/` returns 0 hits.** That grep is the demo. The rest is output.

---

### M6 — logtail + archive 🔍 **← search attaches here**
**Goal:** BRIEF §2's *"how could all learning of all agents be centralized and searchable?"* — the **backbone**, not search itself.

**Deliverable:** logtail (discovers paths by **watching directories, never deriving** — the two vendors' slug rules are incompatible and undocumented); lines >256KB ship as pointer+sha256; the archive on the dgx (3.2TB free, verified) which **PULLS, cursor-driven, and never receives.**

> **That pull property is the entire reason the dgx is not the hub BRIEF §9 warns about.** If the dgx dies, nothing stops — every box keeps its own complete log. §4 forbids a box whose death **stops** the system, not one whose death degrades a **consumer**.

**Demo:** `fleetctl logs tail --node dgx` from the laptop. Then kill the dgx: **every box keeps logging, locally, uninterrupted.** Restart it: the archive catches up from its cursor, with no gap and no coordination.

**Where search attaches, and its contract:** search is a **consumer** (BRIEF §4: *"We handle search later - we can solve search once we have a data backbone"*). It reads per-node journals. It needs nothing new from the kernel — `origin_node` and `origin_time` already ride on every envelope, kernel-stamped. **Its contract, which must be written down before anyone builds it:**
- `origin_seq` is the ordering key **and it is per-node.**
- `origin_time` is **display only.** Sorting a cross-machine result set by it produces a **plausible, wrong story** on three unsynchronised clocks.
- **The kernel must never gain a search-shaped field.**

---

### M7 — The ssh plugin
**Goal:** BRIEF §4's *"agents dont have to figure that out"* / *"no agent should ever screw around figuring out the network crap."*

**Phase 0, and do this first because it is five minutes and deletes most of the work:** run `tailscale set --ssh` on the dgx + grant it in the ACL. **A human decision in an admin console.** Keep plain SSH as fallback (nobody verified Tailscale SSH survives a control-plane outage).

**Deliverable:** continuous read-only `Verify` (zero credentials) producing a live connectivity matrix; host **public** keys distributed via one LOOKUP channel (single-writer: each box publishes only its own); `authorized_keys` changes **proposed** for a human to run.

**Demo:**
```
fleetctl ssh matrix
            -> gb-mbp   -> dgx      -> gb-mac-mini
  gb-mbp       -         OK 12s      OK 14s
  dgx          FAIL 9s   -           OK 11s        <- found BEFORE an agent hit it
  gb-mac-mini  OK 13s    OK 12s      -
```
**The whole point is the `FAIL`:** the fleet knew before any agent tried. Three separate research agents hit `Host key verification failed` on `gb-mac-mini` *during this very investigation* — **this milestone is the fix for a problem that has already bitten us three times.**

**Hard lines:** private keys never move; the daemon never edits `authorized_keys`; `approve` is always a human typing a command.

---

### M8 — The plugin library — **only if the chore is real**
**Goal:** BRIEF §3.2's *"[Idea] A plugin gets added to one node, the plugin gets synced across nodes."*

**Deliverable:** sync the **manifest**, never the bytes (name/version/platform/sha256/url). Each box fetches its own platform's bytes and **verifies the checksum before exec**. It may **add** records and **offer** versions; it may **never** repoint another box's `enabled/` symlink without a human.

**The gate, and Greg applies it himself:** **is `scp`-ing two binaries actually annoying yet?** With 4 plugins × 2 platforms and one operator, **it may simply not be.** It is an `[Idea]` in the brief, not a requirement, and it should be held to that standard.

**Kill criteria — this is the plugin most likely to be a mistake:**
- **If the answer to the gate is "no, `scp` is fine"** → **don't build it.** It is the only plugin that can break all three boxes at once.
- **If `ARCHITECTURE.md` §11 open question 4 fails** (macOS quarantines HTTP-fetched binaries and refuses to exec them) → the fetch-and-verify story does not work on 2 of 3 boxes. Stop.

**Prerequisite already built:** `fleetctl plugin install <path>` (~50 lines, in M1) makes the library **optional forever** — which is what defuses *"the recovery path is the thing you broke."*

---

## 4. Timeline

| | Milestone | Rough size | Unlocks |
|---|---|---|---|
| **M0** | Overnight sleep test | ½ day + one night | **Everything. Gate.** |
| **M1** | Walking skeleton | ~1,300 lines | The bet is proven or dead |
| **M2** | Roster | ~400 lines | Routing, dead-box knowledge |
| **M3** | LOOKUP | ~250 lines ⚠️ riskiest | Name resolution |
| **M4** | **comms** ⭐ | plugin | **BRIEF §2's first-class need. harmonik migrates.** |
| **M5** | registry | plugin | Names → agents |
| **M6** | logtail + archive 🔍 | plugin | **The backbone. Search attaches.** |
| **M7** | ssh | plugin | *"agents dont have to figure that out"* |
| **M8** | plugin library | plugin | **Only if the chore is real** |

**M0→M4 is the spine.** Everything after M4 is a plugin against an unchanged kernel — **and if it isn't, the boundary was wrong and M4's kill criteria should have caught it.**

---

## 5. Project-wide kill criteria

> **When to stop, decided now, while nobody is invested.** BRIEF §9's lesson is that a bad premise survives because nobody wrote down what would falsify it.

**Stop the project if:**

1. **M0 shows the mesh does not survive a real sleep** and no tuning fixes it. The laptop is a peer; if peers can't sleep, the architecture is wrong for this fleet.
2. **The kernel passes ~3,000 lines** (advisory tripwire). BRIEF §3's whole thesis is *"maybe the core is actually really light."* **A heavy kernel is a failed kernel**, and it also can't be live-reloaded — which is the thing Greg actually complained about.
3. **Two consecutive plugins need new kernel RPCs.** One is a miss. Two is a boundary in the wrong place, and every future plugin will need one too.
4. **The boundary test needs a permanent exception.** **An exception is a design failure, not a build failure.** *(The test earned this: it rejected its own author's draft twice before a line of kernel code existed.)*
5. **After M4, harmonik's comms is still better.** Then this is architecture for its own sake. Ship harmonik and stop.

**Do NOT stop for:**
- **Messages lost to at-most-once in the skeleton.** That is the design working. It is what M4 fixes.
- **Per-observer liveness disagreement.** gb-mbp saying dgx is down while gb-mac-mini says it's up is the **correct answer** (BRIEF §5 wants *"can I reach B"*, not a fleet consensus), not a bug to resolve.
- **The kernel not being live-reloadable.** Known, stated, unfixable in Go. The mitigation is keeping it small — which is kill criterion 2, already watched.
- **`fleetctl lookup get` returning two entries for one name.** That is *"it never judges"* working.

---

## 6. Decisions this roadmap assumes

If any of these are overturned, re-plan rather than patch:

1. **Embedded core NATS, `JetStream: false`.** Against Greg's stated lean — argued in `ARCHITECTURE.md` §6, hedged behind a 6-method interface in one file. **If Greg rejects it, the API does not change and M1 costs one extra day.** That is the entire blast radius, which is why recommending against his lean is defensible.
2. **Hand-rolled roster, not memberlist.** Verified in memberlist's own source: at n=3, `k=0` — the feature you import it for does not engage.
3. **go-plugin, subprocess + gRPC.** Reload = kill the child, spawn the new binary, 4–9ms.
4. **Plugins hold no in-memory state.** Four independent derivations. **Enforce day one** — retrofitting it costs the comms queue.
5. **`bool sync` and `max_payload_bytes` are in the API before v1.** Verified additive and free.
6. **`STATE_LEFT` is gone before v1.** Verified genuinely breaking — **this one cannot be deferred**, which is precisely why it is called out here.

---

## 7. Amendments (post-critique)

The adversarial critique (`design/29-critique.md`) touched this roadmap in two places; the fuller record is in `ARCHITECTURE.md` §13.

- **M3 now reuses M2's `boot_id` as LOOKUP's incarnation counter** (above). The critic noticed that the roster already mints the exact signal the kernel doc called a missing subsystem; the disk-wipe hole closes with ~10 lines wired into code M2 is already writing, not a new build item. The sizing of M3 (~250 lines, riskiest) is unchanged.
- **No milestone was added, removed, or re-ordered.** The critic's structural resolutions (one kernel proto, pull not push, one failure detector, four channel types, at-most-once) were already the plan this roadmap was built on — it post-dates the reconciliation, not the raw subsystem docs the critic reviewed. Its one non-negotiable finding (a fabricated Greg quote) lived in `ARCHITECTURE.md` §6, not here; it is struck there.

---

## Sources

- `/Users/gb/research/2026-07-15-agent-substrate-v2/BRIEF.md` — the requirements and quotes this plan is ordered against.
- `/Users/gb/research/2026-07-15-agent-substrate-v2/ARCHITECTURE.md` — the design; §9 (conflicts), §10 (not built), §11 (open questions) are referenced directly.
- `design/25-reconciled-proto/fleet/kernel/v1/{kernel,plugin}.proto` — the interface M1 builds against. Verified this session: lint-clean, build-clean, boundary-clean, generates Go+ConnectRPC, cross-compiles `CGO_ENABLED=0` to `linux/arm64`.
- Subsystem designs `design/20`–`24` — measurements re-cited at the point of use; the milestone sizes and demos derive from their prototypes.
- Harmonik migration specifics (`jsonlwriter.go`, `injector.go`, `comms.go:448`, `commscursor.go`, `fsyncBoundaryEventTypes`, `SubscribeHub`, the UUIDv7 `bytes.Compare` sites, the vendor path deriver) are from `investigate/15-salvage.md` and `design/24-validation-plugins.md`, which read that code. **This roadmap did not re-read harmonik**; before M4 begins, those specific line references should be re-confirmed against the current repo.
