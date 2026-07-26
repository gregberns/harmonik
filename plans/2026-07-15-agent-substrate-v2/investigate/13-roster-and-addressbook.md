# 13 — The Machine Roster and the Address Book

**Date:** 2026-07-15
**Scope:** BRIEF §3.1 (machine roster baked into the kernel), §4 (not-centralized; address book; SSH key sync as a plugin), §5 (liveness as a routing input).
**Method:** Everything in the "measured" sections below was produced by running commands on the live fleet, reading the actual source of `hashicorp/memberlist` v0.6.0 on disk, and running a real 3-node gossip cluster with a frozen node. Labels used throughout:
- **[MEASURED]** — I ran it and this is the output.
- **[CLAIMED]** — an upstream project/doc says so; I did not verify it myself.
- **[DEDUCED]** — I reasoned to it from measured facts. Flagged as such because the last round's worst errors were confident deductions.

---

## 0. Verdict up front

**Build the roster on `hashicorp/memberlist` v0.6.0.** It is the gossip membership library that sits under Consul, Nomad, and Serf. It is ~100 lines to integrate, it survives any single box dying, and — the reason that actually decides it — it gets the *sleeping laptop rejoin* right for free, which is the one thing a hand-rolled ping list reliably gets wrong.

**And a finding that matters more than the library choice:** the address-book pain Greg described is **real, worse than he described, and Tailscale is not currently solving it.** On this fleet right now:

- **From `dgx` — the always-on GPU box — `ssh` to any other box fails. All six addressing forms I tried. Zero work.** It has no SSH private key and an empty `known_hosts`.
- From `gb-mbp`, **one** of nine addressing forms works (`ssh 100.115.27.55`).
- `ssh dgx` from the laptop silently connects over the **home LAN**, not Tailscale — and then fails on host-key verification.
- `tailscale ssh gb@dgx` **fails**, because macOS cannot resolve Tailscale's own MagicDNS names.

Tailscale solves the *hard* part (every box can reach every box, encrypted, through NAT — measured). It is not solving the two things that actually stop agents: **name → address**, and **trust anchoring**. Details in §4, which is the most important section of this document.

---

## 1. Terms, defined once

Greg is not deep in this space, so no term below is used before it is defined.

| Term | Plain meaning |
|---|---|
| **Roster** / **membership list** | The list of boxes and whether each is up. The "who's where" thing. |
| **Gossip** | Instead of every box reporting to a central server, each box periodically tells a couple of random other boxes what it knows. Facts spread like rumours. No hub. |
| **SWIM** | The specific gossip algorithm memberlist implements. Stands for *Scalable Weakly-consistent Infection-style process group Membership*. You never need to say the acronym; what matters is the three ideas in it: **probe** (ping a random peer every second), **suspect** (if it does not answer, mark it *maybe dead* and tell others, rather than declaring it dead immediately), and **refute** (a node wrongly declared dead can shout "I'm alive" and win). |
| **Incarnation number** | A counter each box keeps about itself, bumped every time it has to refute a rumour of its own death. Higher number wins. This is the mechanism that makes a rejoining laptop stick instead of being re-killed by stale gossip still in flight. |
| **Anti-entropy / push-pull** | Every so often (30s by default) two boxes open a TCP connection and exchange their *entire* view of the world, not just deltas. It is the backstop that repairs anything gossip dropped. |
| **Consensus** (Raft, ZAB) | The algorithm behind etcd/ZooKeeper/Consul's data store. It makes a majority of nodes agree on an exact ordered history. It needs a **quorum** — more than half the voters alive. This is the thing Greg said he doesn't want ("*I dont want to get hung up on CAP theorem and shit*"). |
| **`known_hosts`** | The file where SSH records "I have seen this host before and its key was X". If the key doesn't match what's recorded *for the exact name or IP you typed*, SSH refuses to connect. |
| **Trust anchor** | Which *string* — `dgx`, or `100.115.27.55`, or `192.168.1.86` — a host key is filed under in `known_hosts`. The same box under a different string is, to SSH, a stranger. This distinction is the whole problem in §4. |
| **MagicDNS** | Tailscale's feature that makes `dgx` resolve to that box's Tailscale IP from anywhere. |
| **BatchMode** | `ssh -o BatchMode=yes`. Disables all interactive prompts. **This is the mode an agent runs in.** A human gets asked "unknown host, continue?" and types yes; an agent gets a hard failure. Every SSH test below uses BatchMode, because that is the honest test. |

---

## 2. Recommendation: `hashicorp/memberlist` v0.6.0

### Maturity — verified, not assumed

| Fact | memberlist | Serf |
|---|---|---|
| Stars | 4,085 | 6,066 |
| Created | 2013-09-09 | 2013-10-01 |
| **Last push** | **2026-07-15 (today)** | 2026-07-07 |
| Archived? | No | No |
| License | MPL-2.0 | MPL-2.0 |
| Latest version | **v0.6.0** | — |

**[MEASURED]** via `https://api.github.com/repos/hashicorp/memberlist` and `.../serf`, and `go get github.com/hashicorp/memberlist@latest` resolving to **v0.6.0**. A 13-year-old library with a commit today is not abandonware. It is the membership layer inside Consul and Nomad **[CLAIMED** — HashiCorp's own description; I did not read Consul's source**]**.

**[MEASURED]** `dgx` has **no Go toolchain installed** (`go: command not found`). `gb-mbp` has go1.26.2. Whatever gets built must ship as a cross-compiled binary, not `go run`. Go cross-compiles to `linux/arm64` trivially, so this is a note, not a problem.

### Why the alternatives lost

**etcd / ZooKeeper / Consul — rejected, and the reason is not "too heavy".**

The real reason is that they are **consensus** systems, and consensus needs a **quorum** (a majority of voters). With 3 boxes you would need to pick the voters. `gb-mbp` is a laptop that sleeps, so it cannot be a voter — a voter that vanishes for 8 hours makes the cluster useless. That leaves `dgx` + `gb-mac-mini` as the two voters, and a 2-voter quorum tolerates **zero** failures: kill either one and the roster goes read-only or dies, and `gb-mbp` becomes a client of a cluster it isn't in.

**That is a hub whose loss is fatal — the exact thing BRIEF §4 forbids, and the exact mistake the last round made with NATS-on-the-DGX.** [DEDUCED, but the quorum arithmetic is not in dispute.] Secondary: they give you a strongly-consistent *key-value store*, which is a much bigger thing than "who's around", and they'd drag CAP-theorem reasoning into a project whose brief explicitly refuses it.

**Serf — the runner-up, and it lost narrowly.** Serf is memberlist plus user-defined events, query/response, and an on-disk snapshot for fast rejoin. Those are genuinely useful. It lost because Serf is shaped as **its own agent + CLI daemon**, and Greg is *writing a daemon*. Adopting Serf means either running a second daemon next to his, or using Serf's library API, which is a heavier wrapper around the same memberlist underneath. Since the kernel already needs its own channel/transport layer (BRIEF §3.1), the extra Serf machinery would be paid for and then bypassed. Its snapshot-for-rejoin feature is worth stealing as an idea (see §5).

**Hand-rolled ping list — the honest answer, because 3 boxes is genuinely small.**

I want to be straight here rather than reflexively reaching for a library. Three boxes, full mesh, is 3 links. "Ping everyone every second, mark them down after N misses" is ~200 lines. **At this scale, a library is not warranted for *scale*. It is warranted for *correctness on rejoin*.**

Specifically, the three things a hand-rolled list gets wrong are exactly the three things `gb-mbp` will do to you every single day:

1. **Incarnation numbers.** Without them, a laptop that wakes up gets re-killed by stale "gb-mbp is dead" rumours still circulating, and flaps. I measured memberlist's refute mechanism firing and fixing precisely this (§5).
2. **Suspicion.** Without it, one dropped UDP packet marks a healthy box dead.
3. **Anti-entropy.** Without it, any missed message is missed forever.

You would write all three, badly, and debug them at 11pm. memberlist has had 13 years of people doing that for you. **Use the library — not because 3 boxes is hard, but because rejoin is.**

---

## 3. Not-centralized (BRIEF §4) — CONFIRMED by experiment

The claim to test: gossip survives any single box dying; a registry-on-one-box does not.

**[MEASURED].** I ran three real memberlist nodes (named `dgx`, `gb-mac-mini`, `gb-mbp`) on ports 7946–7948. Both `gb-mac-mini` and `gb-mbp` bootstrapped by joining through `dgx` — so `dgx` was the **seed**, the single most "central" node in the setup, the one that would be the hub in a hub-and-spoke design.

Then I killed `dgx` with `SIGKILL`:

```
killing dgx pid=58890 at 23:44:12.3
23:44:17.276 [gb-mac-mini] DEAD dgx
23:44:17.277 [gb-mbp]      DEAD dgx
```

Both survivors independently detected it in **~5 seconds**, agreeing within **1 millisecond**. Then, with the seed dead, I updated `gb-mac-mini`'s agent list and checked whether `gb-mbp` still received it:

```
23:44:24.276 [gb-mbp] META-UPDATE from gb-mac-mini -> {"agents":[{"id":"ag-1","doing":"refactoring auth middleware"},...]}
```

**It did.** The two survivors kept gossiping application data to each other with the box they had both bootstrapped through in the ground.

**Verdict: CONFIRMED.** The seed node is only special for the first ~1 second of a node's life. After that there is no hub. This is not a property you have to engineer — it falls out of gossip for free, and it is the single strongest argument for this design over any registry-on-one-box.

---

## 4. The address book — I measured the real pain, and it is worse than described

This is the section that matters most. Greg said an agent must never *"stumble over the wrong machine name/ip/username/inability to connect via ssh"*. I tested whether it currently does.

### 4.1 The headline: the SSH reachability matrix

Every cell below is `ssh -o BatchMode=yes -o StrictHostKeyChecking=yes <target> echo OK` — i.e. **exactly what an agent would run**. **[MEASURED]**, all of it.

**From `gb-mbp` (the laptop) — 1 of 9 works:**

| Target | Result |
|---|---|
| `dgx` | ❌ Host key verification failed |
| `gb-mbp` | ❌ Could not resolve hostname |
| `gb-mac-mini` | ❌ Could not resolve hostname |
| `dgx.tailf4fa3f.ts.net` | ❌ Could not resolve hostname |
| `gb-mbp.tailf4fa3f.ts.net` | ❌ Could not resolve hostname |
| `gb-mac-mini.tailf4fa3f.ts.net` | ❌ Could not resolve hostname |
| **`100.115.27.55`** | ✅ **OK** |
| `100.87.151.114` | ❌ Host key verification failed |
| `100.120.22.74` | ❌ Host key verification failed |

**From `dgx` (the always-on GPU box) — 0 of 6 work:**

| Target | Result |
|---|---|
| `gb-mbp`, `gb-mac-mini` | ❌ Host key verification failed |
| `gb-mbp.tailf4fa3f.ts.net`, `gb-mac-mini.tailf4fa3f.ts.net` | ❌ Host key verification failed |
| `100.87.151.114`, `100.120.22.74` | ❌ Host key verification failed |

**`dgx` cannot SSH to anything.** Root cause, measured: `ls ~/.ssh/id_*` → *"No such file or directory"* (**no private key at all**), and its `known_hosts` is **empty**. Note that `dgx`'s DNS is perfect — it resolved every name correctly. It fails purely on credentials and trust. This is the most consequential single fact in this document: the box you most want to run agents on is the box that cannot initiate a connection to any peer.

**From `gb-mac-mini` — and note it is *inverted* from the laptop:**

| Target | Result |
|---|---|
| **`dgx`** | ✅ **OK** |
| **`100.87.151.114`** | ✅ **OK** |
| `100.115.27.55` | ❌ **Host key verification failed** |
| `gb-mbp`, `dgx.tailf4fa3f.ts.net`, `gb-mbp.tailf4fa3f.ts.net` | ❌ Could not resolve hostname |

Read those two tables together. **`ssh dgx` works from `gb-mac-mini` and fails from `gb-mbp`. `ssh 100.115.27.55` works from `gb-mbp` and fails from `gb-mac-mini`.** Exactly inverted. There is no single command an agent can be told to use that works from more than one box. This is Greg's complaint, reproduced precisely.

### 4.2 Why `ssh dgx` fails from the laptop — it goes over the wrong network

**[MEASURED]** with `ssh -v`:

```
debug1: Connecting to dgx [192.168.1.155] port 22.
debug1: Connection established.
debug1: Server host key: ssh-ed25519 SHA256:gcWBbIsHust4uDK+7DlfVXmxaAlYPwtMh77EGQs/0Zo
No ED25519 host key is known for dgx and you have requested strict checking.
Host key verification failed.
```

`ssh dgx` resolves to **192.168.1.155** — the **home LAN**, handed out by the router at 192.168.1.1 as `dgx.localdomain`. It is not using Tailscale at all. The connection *succeeds*; it is the trust check that fails, because `known_hosts` files the key under the string `100.115.27.55`, not `dgx`.

**Two consequences [DEDUCED, but directly from the above]:** the moment the laptop leaves the house, `ssh dgx` stops resolving entirely and fails a *different* way. And an agent that "fixes" this by accepting the key pins `dgx` to a **DHCP address that moves** — see §4.4.

### 4.3 MagicDNS is enabled and broken on both Macs. This is a real bug.

`tailscale status --json` reports `"MagicDNSEnabled": true`, suffix `tailf4fa3f.ts.net`, and prefs show `"CorpDNS": true`. So it is *on*. But:

**[MEASURED]** — Tailscale's own DNS server has the right answers:
```
$ dig +short @100.100.100.100 dgx.tailf4fa3f.ts.net          -> 100.115.27.55
$ dig +short @100.100.100.100 gb-mac-mini.tailf4fa3f.ts.net  -> 100.120.22.74
```

**[MEASURED]** — but the macOS system resolver (which is what `ssh` actually uses) has none of them:
```
$ dscacheutil -q host -a name dgx.tailf4fa3f.ts.net     -> (empty)
$ dscacheutil -q host -a name gb-mac-mini               -> (empty)
$ dscacheutil -q host -a name dgx                       -> dgx.localdomain / 192.168.1.155
```

**Root cause, measured.** `scutil --dns` on `gb-mbp` shows **no resolver entry mapping `tailf4fa3f.ts.net` → 100.100.100.100**. The only thing Tailscale installed is `/etc/resolver/search.tailscale`, whose entire contents are:

```
# Added by tailscaled
search tailf4fa3f.ts.net localdomain
```

That adds a *search domain* and **no nameserver**. So a lookup for `dgx.tailf4fa3f.ts.net` falls through to resolver #1 — the home router at 192.168.1.1 — which returns NXDOMAIN. And `tailscale dns status` confirms it in its own words:

```
Resolvers (in preference order):
  (no resolvers configured, system default will be used: see 'System DNS configuration' below)
...
System DNS configuration:
  Nameservers:  - 192.168.1.1
  Search domains: - localdomain
```

`gb-mac-mini` has **the identical break** (`/etc/resolver/` contains only `search.tailscale`; all four peer lookups NXDOMAIN). `dgx` (Linux, systemd-resolved) works perfectly — it resolved every bare name and every FQDN correctly.

**The punchline [MEASURED]:** even Tailscale's *own* SSH wrapper fails, because it expands to the name macOS cannot resolve:

```
$ tailscale ssh gb@dgx
ssh: Could not resolve hostname dgx.tailf4fa3f.ts.net.: nodename nor servname provided, or not known
```

### 4.4 Trust anchoring is inconsistent per box, and one anchor is a moving target

**[MEASURED]** — `dgx` currently answers to **six** different address forms:

| Form | Value |
|---|---|
| Tailscale IPv4 | `100.115.27.55` |
| Tailscale IPv6 | `fd7a:115c:a1e0::8539:1b38` |
| LAN (what `ssh dgx` picks) | `192.168.1.155` |
| LAN (second, also live) | `192.168.1.86` |
| mDNS | `dgx.local` → `fe80:11::c93b:...` (link-local IPv6) |
| MagicDNS | `dgx.tailf4fa3f.ts.net` (unresolvable from either Mac) |

**[MEASURED]** — `100.115.27.55`, `192.168.1.155`, and `192.168.1.86` all present the **same** host key `SHA256:gcWBbIsHust4uDK+7DlfVXmxaAlYPwtMh77EGQs/0Zo`. Same box. Same key. But which *string* it's filed under differs per box:

| Box | What its `known_hosts` anchors `dgx` under |
|---|---|
| `gb-mbp` | `100.115.27.55` (ed25519/rsa/ecdsa), `192.168.1.86`, `192.168.1.219` — **and no entry for `gb-mac-mini` at all** |
| `gb-mac-mini` | `dgx.local,192.168.1.86`, `dgx.local`, `gb-mbp`, `100.87.151.114`, `gb-mbp.local`, `192.168.10.2`, `192.168.1.66` — **and no entry for `100.115.27.55`** |
| `dgx` | **empty** |

`gb-mbp` trusts `dgx` under its **Tailscale IP**. `gb-mac-mini` trusts `dgx` under its **LAN name and LAN IP**. `dgx` trusts nobody. Three boxes, three different theories of what `dgx` is called.

**And `192.168.1.86` is stale-ish:** `gb-mbp` has `dgx` anchored at `192.168.1.86`, but the router now hands out `192.168.1.155` for the name `dgx`. Both addresses currently answer with dgx's key (it has two live LAN interfaces), so this hasn't bitten yet — but it is a **DHCP lease**, not a stable identifier. **[DEDUCED]** The day that lease moves, an agent following the LAN path connects to whatever machine inherited `.86` and gets a host-key mismatch — SSH's loudest, scariest error. This is "*stumble over the wrong machine name/ip*" waiting to happen.

### 4.5 Users, OS, and inbound auth — measured

| Box | Username | OS | Tailscale SSH server | `authorized_keys` |
|---|---|---|---|---|
| `gb-mbp` | `gb` | macOS 25.4.0 / Darwin arm64 | `RunSSH: true` | **none** — file does not exist |
| `dgx` | `gb` | Ubuntu 24.04.4 LTS / Linux aarch64 | **`RunSSH: false`** | 3 keys (gb-mbp's, gb-mac-mini's, one more) |
| `gb-mac-mini` | `gb` | macOS 26.3.2 / Darwin arm64 | advertises `sshHostKeys` → on | 3 keys (incl. an iPhone ShellFish RSA key) |

Good news: **the username is `gb` on all three**. That part of the address book is trivially uniform.

Note the asymmetry: `gb-mbp` has **no `authorized_keys` file at all**, yet `ssh 100.87.151.114` from `gb-mac-mini` **succeeds** [MEASURED]. **[DEDUCED]** That inbound connection is being served by **Tailscale SSH** (`RunSSH: true`), which authenticates by tailnet identity and needs no `authorized_keys` — not by the system `sshd` (which is also running and listening on :22). This is a good demonstration of what Tailscale SSH buys you: zero key management. And `dgx`, the box that needs it most, has it **off**.

### 4.6 So — does Tailscale already solve this? Honest answer: **half. And the half it solves is the hard half.**

The brief asked me to answer this honestly and said a "yes" would be a valuable finding, not a failure. It is not a clean yes. Here is the split:

**What Tailscale genuinely solves, and the roster must not reinvent:**
- **Reachability.** Every box reaches every box, encrypted, through NAT, from anywhere. This is the hard distributed-systems problem and it is *done*. Nothing in this design should route packets itself.
- **Stable identity.** [MEASURED] Every node has a permanent ID (`dgx` = `nXBLGEb3iL11CNTRL`) and a stable Tailscale IP that does not move — unlike DHCP.
- **An address feed.** `tailscale status --json` already hands you, per box: hostname, FQDN, both IPs, OS, online flag, and last-seen. That is most of the node entry in §6, for free.
- **Authentication, where enabled.** Tailscale SSH removes `known_hosts` and `authorized_keys` entirely — on the 2 of 3 boxes where it is on.

**What it is not solving today:**
- **Name → address is broken on 2 of 3 boxes** (§4.3), including Tailscale's own tool on its own names.
- **Trust anchoring is per-box and inconsistent** (§4.4). Tailscale has no opinion here unless you use Tailscale SSH, which `dgx` has off.
- **Its liveness is about the *box*, not the *daemon* or the *agents*.** `tailscale status` says the box is up; it cannot say "the daemon is running and has 4 agents, one of which is refactoring auth". BRIEF §3.1 explicitly wants the agent list synced. And its online flag comes from Tailscale's **coordination server** — a third-party central dependency, which is philosophically at odds with §4 and, more practically, goes stale rather than being an app-level truth.
- **Detection speed.** Tailscale's status is a control-plane view; the roster detects a dead peer in ~5s locally with no third party (§5). [DEDUCED — I did not benchmark Tailscale's own offline-detection latency; see open questions.]

**Two fixes worth doing regardless of this project, because they are cheap and remove most of the pain:**
1. **Fix MagicDNS on both Macs.** In the Tailscale admin console, enable **Override Local DNS** (with MagicDNS), or otherwise get a real `/etc/resolver/tailf4fa3f.ts.net` pointing at `100.100.100.100`. That alone turns 6 of the 9 failing rows in §4.1 from "cannot resolve" into working names.
2. **Turn on Tailscale SSH on `dgx`** (`tailscale set --ssh`) and give `dgx` an SSH key. That fixes the 0-of-6 row.

**But this does not make the roster redundant** — and this is the key architectural point:

> **Tailscale is the *network*. The roster is the *directory*.** Tailscale tells you box `dgx` is reachable at `100.115.27.55`. It cannot tell you that agent `pi-refactor-3` lives there, is alive, and is halfway through a refactor. BRIEF §4 says agent identity is a **daemon-defined name** with box/IP as *attributes* — that mapping is precisely what Tailscale has no concept of.

**So the roster should consume `tailscale status --json` as its address source rather than compete with it.** [DEDUCED, but strongly supported: it is the only source on this fleet with stable IDs and stable addresses, and it is already correct on all three boxes even where DNS is broken — the Tailscale IPs were reachable from every box in every test.] This kills the whole `known_hosts`/DNS mess in one move: **the daemon never resolves a name; it looks the peer up in the roster and dials the Tailscale IP it was told.** That is the answer to "*agents dont have to figure that out*".

---

## 5. The sleeping laptop — measured, not guessed

`gb-mbp` sleeps and rejoins. I ran the actual experiment rather than reasoning about it: three real memberlist nodes, then **`SIGSTOP`** on one. `SIGSTOP` freezes a process without killing it — the closest available analogue to a sleeping laptop (the process stops answering; nothing is torn down cleanly).

### 5.1 How fast does a sleeping box get declared dead? ~6 seconds.

**[MEASURED]**, run 2 (a 75-second freeze — 2.5× the 30s `GossipToTheDeadTime`, so the peers stopped gossiping to it entirely):

```
FREEZE_AT = 23:40:44.3
23:40:50.881 [dgx]         LEAVE/DEAD gb-mbp     <- 6.6s after freeze
23:40:50.882 [gb-mac-mini] LEAVE/DEAD gb-mbp     <- 1ms apart
```

**[MEASURED]** the math behind it, read from the source rather than remembered:
- `util.go:70-74`: `suspicionTimeout = SuspicionMult × max(1, log10(N)) × ProbeInterval`
- `config.go` `DefaultLANConfig()`: `SuspicionMult: 4`, `ProbeInterval: 1s`, `ProbeTimeout: 500ms`, `SuspicionMaxTimeoutMult: 6`, `PushPullInterval: 30s`, `GossipInterval: 200ms`, `GossipToTheDeadTime: 30s`, `AwarenessMaxMultiplier: 8`.

For N=3: `log10(3) = 0.477`, but the `max(1.0, ...)` floors it at **1.0** → suspicion timeout = `4 × 1.0 × 1s` = **4 seconds**.

There's a subtlety worth knowing (`state.go:1211-1222`): memberlist normally waits for *k* independent peers to confirm a suspicion before declaring death, where `k = SuspicionMult - 2 = 2`. But it then checks `if n-2 < k { k = 0 }` — with 3 nodes, `n-2 = 1 < 2`, so **k is forced to 0**. And `suspicion.go:73-75`: `if k < 1 { timeout = min }`. **At 3 nodes there is no independent confirmation and the suspect→dead timeout is a flat 4 seconds.** Plus ~1–3s to notice → the measured 5.7–6.6s. Consistent.

### 5.2 What happens on rejoin after a long absence? It just works, in 0.7 seconds.

This is the result that decides the library question. **[MEASURED]**, same run:

```
WAKE_AT = 23:41:59.3          (after 75s frozen, well past GossipToTheDeadTime=30s)
23:41:59.994 [dgx]         JOIN gb-mbp      <- 0.7s after wake
23:41:59.994 [gb-mac-mini] JOIN gb-mbp      <- same instant
```

And the mechanism is visible in the woken node's own log:

```
2026/07/15 23:41:59 [WARN] memberlist: Refuting a suspect message (from: dgx)
```

That is the incarnation counter doing its job: the laptop wakes, hears "you're dead", bumps its incarnation, and its `alive` message beats the stale rumour. **No intervention, no re-Join call, no operator.** An earlier run with a 25s freeze rejoined in 0.44s identically.

**This is why the library wins.** A hand-rolled ping list would have re-killed that node with the stale "gb-mbp is dead" gossip still in flight, and it would flap. I did not have to write, or debug, the thing that prevented that.

**Honest caveat [MEASURED gap]:** I tested 75 seconds, not 8 hours. `SIGSTOP` is not identical to a real macOS sleep — a real sleep also takes the network interface down and the Tailscale IP with it, and after 8 hours the peers will have **reaped** the dead entry from their node maps entirely, not merely marked it dead. **[DEDUCED]** The repair path then is the 30-second push/pull anti-entropy: the woken laptop still has `dgx` in its own list, opens a TCP push/pull, and both sides re-learn each other. That is the mechanism, and it is not obviously time-dependent — but **I have not measured it, and I am flagging it rather than asserting it.**

**Because of that gap, the rejoining node should do the belt-and-braces thing anyway:**

> **On wake, call `Join(seedList)` unconditionally.** It is idempotent, it costs one round-trip against boxes that are already in the roster, and it collapses the entire 8-hour-absence question into the 1-second bootstrap case that is tested by every memberlist user on earth. Wire it to a macOS wake hook, and also just run it on a slow timer (say every 60s) if the member count is < expected. This is cheap insurance against the one case I could not measure.

### 5.3 Asleep vs dead vs slow — the honest answer: **memberlist cannot tell you, and you must add it yourself**

This deserves a straight answer because it is easy to hand-wave.

- **"Slow" is partly handled, for free.** memberlist tracks local "awareness": `AwarenessMaxMultiplier: 8` backs the probe interval off from 1s toward 8s when the local node itself looks unhealthy, so a bogged-down box gets more patient instead of declaring everyone dead. This is real and automatic.
- **"Asleep" vs "dead" — SWIM has no such concept.** Alive, suspect, dead. That's it. A frozen laptop and a dead laptop are byte-for-byte identical from the outside. **No membership library can distinguish them, because the distinction does not exist on the wire — it exists only in *intent*.**

**Therefore the distinction must be announced, not inferred.** I tested the graceful path. **[MEASURED]:**

```
### gb-mbp calls Leave() -- "I am about to sleep" at 23:46:08.3
23:46:08.917 [gb-mac-mini] NotifyLeave gb-mbp   n.State=0
23:46:08.918 [dgx]         NotifyLeave gb-mbp   n.State=0
23:46:09.117 [gb-mbp] Leave() took=347ms err=<nil>
```

**A graceful `Leave()` propagates in ~0.6 seconds versus ~6.6 seconds for a freeze — 10× faster, and with zero false-suspicion in between.**

**But a gotcha worth reporting [MEASURED]:** the `Node` handed to the `NotifyLeave` callback reported `State=0` (Alive) even for a graceful leave — so **you cannot read `n.State` in the callback to tell "left" from "dead"**. The callback fires identically for both. Anyone who assumes otherwise (I would have) writes a bug.

**So the design is:**

> Before sleeping, the daemon sets `intent: "sleeping"` in its node metadata, calls `UpdateNode` (propagates in ~190ms — measured in §6.2), *then* calls `Leave()`. Peers therefore see, in order: "gb-mbp says it's about to sleep", then a clean departure ~0.6s later. A box that dies without that sequence is genuinely **dead** — no warning, ~6s detection, and the roster records exactly that difference.

On macOS this hangs off a sleep/wake hook. The symmetric pair is: **sleep → `UpdateNode(intent=sleeping)` + `Leave()`; wake → `Join(seeds)`.** Two hooks, and the laptop problem is solved properly rather than tolerated.

---

## 6. What's in a node entry — the exact schema

### 6.1 The hard constraint I found by measurement: **512 bytes**

**[MEASURED]** `net.go:83`: `MetaMaxSize = 512 // Maximum size for node meta data`.

I tested what happens when you exceed it. I pushed a 25-agent list (1,563 bytes) into node metadata:

```
23:43:31.395 !! META TRUNCATED: 1563 bytes > limit 512
23:43:31.689 [gb-mbp] UpdateNode(meta=1563B) took=294ms err=<nil>     <-- err is NIL
```

and the receiver got **corrupt, truncated JSON**, cut off mid-token:

```
23:43:31.489 [dgx] META-UPDATE from gb-mbp -> {"agents":[{"id":"ag-00",...},{"id"
```

My delegate defensively truncated. **If it hadn't, `memberlist.go:460` would have `panic("Node meta data provided is longer than the limit")` — and taken the daemon down.** [MEASURED — that line is in the source at both `memberlist.go:460` and `:519`.]

**This is the single most important implementation constraint in this document.** The naive design — "stuff the agent list into node metadata" — either **silently corrupts data** or **crashes the daemon** as soon as a box runs a few agents with real summaries. Greg explicitly wants agent lists *and* summaries synced, and they will not fit in 512 bytes.

### 6.2 Therefore: two tiers, and this is measured, not stylistic

| | **Tier 1 — Node metadata (`Meta`)** | **Tier 2 — Push/pull state (`LocalState`/`MergeRemoteState`)** |
|---|---|---|
| Carries | Identity + addressing = **the address book** | **The agent list + summaries** |
| Size limit | **512 bytes, hard, panics if exceeded** | No 512B cap (TCP stream) |
| Transport | UDP gossip | TCP |
| Speed | **~190ms to all peers [MEASURED]** | every `PushPullInterval` = **30s** |
| Why | Small, bounded, changes rarely, needed for routing | Big, unbounded, changes constantly |

**[MEASURED]** the Tier-1 speed: `gb-mbp` updated its metadata at 23:43:27.3; both `dgx` and `gb-mac-mini` logged the change at **23:43:27.491** — ~190ms, and the two peers agreed to the millisecond.

30 seconds is too slow for a live agent list. **[DEDUCED]** The fix is the pattern Serf already uses: send agent-list *deltas* through memberlist's user-broadcast queue (`GetBroadcasts`/`NotifyMsg` with a `TransmitLimitedQueue`) for ~200ms updates, and let the 30s push/pull act as the anti-entropy backstop that repairs anything dropped. Note the UDP packet budget is `UDPBufferSize: 1400` bytes, so a delta must be one agent's status, not the whole list. I did not measure the broadcast-queue path — flagged as an open question.

### 6.3 The node entry

**Tier 1 — Meta. Must stay under 512 bytes. Budget it deliberately.**

```jsonc
{
  "n":  "dgx",                        // roster name = the key. Stable.
  "ts": "nXBLGEb3iL11CNTRL",          // Tailscale stable node ID (measured, never moves)
  "ip": "100.115.27.55",              // Tailscale IPv4 — THE address. Not DNS. Not LAN.
  "i6": "fd7a:115c:a1e0::8539:1b38",  // Tailscale IPv6
  "u":  "gb",                         // username (measured: 'gb' on all three)
  "os": "linux",                      // linux | darwin
  "ar": "arm64",                      // measured: all three are arm64
  "dp": 7946,                         // daemon port
  "in": "up",                         // intent: up | sleeping | draining  <- §5.3
  "hk": "SHA256:gcWBbIsHu...",        // SSH host key fingerprint  <- §7
  "ep": 3,                            // agent count (the full list is Tier 2)
  "v":  1                             // schema version
}
```

That is ~330 bytes with a real fingerprint. It fits, with headroom, but **it will not survive adding free-text.** Enforce the budget in code with a test that fails at >512, and **never** return oversized metadata from `NodeMeta()` — clamp and log loudly, because the alternative is a panic.

Deliberate omissions, and why:
- **No `known_hosts`-style name list, no LAN IP, no `.local`, no MagicDNS name.** §4.4 showed those are exactly what poisons the address book: six forms, three stale, inconsistently anchored. **The roster carries exactly one address per box — the Tailscale IP — because it is the only one measured stable and reachable from every box.**
- **No `last_seen` in Meta.** Liveness is *local* state (see below), not a gossiped field. Gossiping timestamps across boxes with unsynchronised clocks is how you get a roster that argues with itself.

**Liveness is computed locally, never gossiped:**

```go
type Liveness struct {
    State     string    // "alive" | "suspect" | "dead" | "left"
    LastSeen  time.Time // LOCAL clock, when this box last heard from that peer
    Intent    string    // from Meta: "up" | "sleeping"
}
```

Each daemon derives this from its own memberlist events. Peers do not exchange opinions about third parties' timestamps — memberlist's suspect/refute protocol already handles the disagreement, and it does so correctly (measured: two boxes converged 1ms apart, §3).

**Tier 2 — Agent list. Rides in push/pull, unbounded.**

BRIEF §4: *agent identity is a **daemon-defined name** as the key, with box/IP as attributes.* The schema honours that literally — the agent name is the key, and the box is an attribute hanging off it:

```jsonc
{
  "node": "dgx",
  "rev":  412,                              // monotonic per node; for merge, not for time
  "agents": [
    {
      "name":    "pi-refactor-3",           // <- THE KEY. daemon-defined. globally unique.
      "node":    "dgx",                     // <- box is an ATTRIBUTE of the agent
      "kind":    "claude-code",             // claude-code | codex | pi | opencode
      "pid":     48213,
      "started": "2026-07-15T22:04:11Z",
      "state":   "working",                 // working | idle | blocked | exited
      "summary": "refactoring auth middleware in harmonik",   // Greg's "what the agent is doing"
      "cwd":     "/home/gb/github/harmonik"
    }
  ]
}
```

**Why the agent name is the key and not `box:pid`:** because §5 says the whole point is that an agent on one box can address an agent on another. A key of `box:pid` breaks the moment the process restarts, and it forces every sender to know where the target *lives* — which is the coupling the roster exists to remove. `pi-refactor-3` is a name you can route to; the roster resolves it to a box, and the box to a Tailscale IP. **That is the "domain name" shape Greg asked for.**

**Resolution chain — the thing that makes agents stop screwing around with network crap:**

```
agent name ──roster Tier 2──> node name ──roster Tier 1──> Tailscale IP ──> dial
"pi-refactor-3"               "dgx"                        100.115.27.55
```

No DNS. No `known_hosts`. No `~/.ssh/config`. **Not one of the nine failure modes in §4.1 is on this path.** The daemon dials an IP it was handed by gossip. That is the whole answer to "*an agent should never stumble over the wrong machine name/ip/username*" — you delete the step where it has to *find out*.

---

## 7. The dead box as a routing input (BRIEF §5)

> *"in case one of its agents wants to communicate with an agent on the dead box."*

**The numbers, measured:**

| Event | Latency |
|---|---|
| Box freezes (sleep) → all peers know it's dead | **5.7–6.6 s** |
| Box killed (`SIGKILL`) → all peers know | **~5.0 s** |
| Graceful `Leave()` ("I'm going to sleep") → all peers know | **~0.6 s** |
| Box wakes → all peers know it's back | **~0.7 s** |
| Metadata change (address book) → all peers | **~190 ms** |
| Agent list via push/pull (worst case) | **30 s** (fix with broadcast deltas, §6.2) |
| Peer-to-peer disagreement window | **~1 ms** (measured, repeatedly) |

**So: the worst-case window where a sender thinks a dead box is alive is ~6 seconds.** [MEASURED.] For a fleet with one human operator, that is not a compromise — it is far better than needed.

**What the sender sees — and this is the actual payoff.** Because liveness is *local state on every box*, the send path never touches the network to find out:

```go
// This is a local map lookup. Microseconds. No timeout. No hang.
n, ok := roster.LookupAgent("pi-refactor-3")
if !ok {
    return ErrUnknownAgent{Name: "pi-refactor-3"}
}
switch roster.Liveness(n.Node).State {
case "dead":
    return ErrNodeDown{Node: n.Node, LastSeen: ..., Intent: "none"}      // crashed
case "left":
    return ErrNodeAsleep{Node: n.Node, LastSeen: ..., Intent: "sleeping"} // §5.3
}
return transport.Send(n.Node, msg)
```

**Contrast with today:** an agent trying to reach a sleeping laptop over SSH waits for a TCP timeout — tens of seconds — and then gets `ssh: connect to host ... : Operation timed out`, which is indistinguishable from a typo, a firewall, or a broken key. With the roster it gets, **instantly**, `ErrNodeAsleep{Node: "gb-mbp", LastSeen: 22:04:11, Intent: "sleeping"}`. That is a message a plugin can *act* on — queue it, pick a different agent, tell the human — rather than a timeout it can only log.

**This is what "liveness is a routing input, not a status display" cashes out to:** the roster's job is not to render a dashboard. It is to make `Send()` fail fast, locally, with a reason.

It also directly serves the ACK gap Greg flagged in §5 (*"we actaully should probably notify the sender that the receiver is not listening. I believe we can find that out"*). **He is right that you can find it out — this is how.** The roster answers "is the receiver's *box* up" in microseconds. It does *not* answer "is that agent actually subscribed", which is one level up and belongs to the comms plugin. The roster gives that plugin the half it cannot get for itself.

---

## 8. SSH key sync — a plugin, and I am going to be careful here

BRIEF §4 is explicit: **SSH key sync is a plugin, not core.** But the goal is that the fleet *programmatically verifies every box can connect*. Given §4.1 measured **`dgx` at 0-for-6**, this plugin has real work to do.

The brief also says: *do not be clever with someone's credentials.* So, plainly:

### 8.1 What the plugin must never do

- **Never move a private key between boxes.** Not over gossip, not over the channel layer, not ever. A private key is generated on the box it belongs to and dies there. `gb-mbp`'s `id_ed25519` stays on `gb-mbp`. This is not negotiable and it is not a performance trade-off.
- **Never write to `authorized_keys` without the human saying yes.** Appending a key to `authorized_keys` grants shell access. That is a decision a person makes, not a daemon.
- **Never auto-accept host keys** (`StrictHostKeyChecking=no`, or blindly `accept-new`). That converts SSH's one real defence into decoration. Note this is already happening in the wild on the fleet: **[MEASURED]** `gb-mac-mini`'s `~/.ssh/config.d/sd-vm1` and `sd-t1` both carry `StrictHostKeyChecking no` + `UserKnownHostsFile /dev/null`. Acceptable for throwaway local VMs; **not** a pattern to spread across the fleet.

### 8.2 What the plugin should do — verify and report, then *propose*

The valuable 90% is **diagnosis**, and it needs no credentials at all. §4.1 is exactly the artifact this plugin should produce continuously — and note that I built that table today with nothing but read-only commands.

**Phase 1 — Verify (read-only, always on, zero risk).** Every N minutes, each box attempts `ssh -o BatchMode=yes -o ConnectTimeout=4 <peer-tailscale-ip> true` against every peer in the roster, and publishes the result as a **capability fact** in its own Tier-2 state:

```jsonc
{ "node": "dgx",
  "ssh_out": { "gb-mbp": "no_key", "gb-mac-mini": "no_key" } }
```

with a small closed vocabulary of outcomes — `ok`, `host_key_unknown`, `host_key_mismatch`, `no_key`, `auth_denied`, `unreachable`, `timeout`. Because it gossips, **every box knows the full connectivity matrix**, and any agent can ask "can I get from here to there?" and get a local, instant, honest answer instead of discovering it via a 30-second timeout. This alone would have surfaced `dgx`'s 0-for-6 the day it started, instead of it sitting there silently.

**Phase 2 — Distribute host keys (safe, and this is the big win).** Host-key trust is **not a secret**. A host *public* key is public by construction — I read all of them today with `ssh-keyscan` from an unprivileged shell. So:

- Each daemon puts its own SSH host key fingerprint in its Tier-1 metadata (`hk` in §6.3). It is ~50 bytes and it is authenticated by the fact that it arrived over gossip on a trusted tailnet from the box that owns the name.
- The plugin then writes a **managed, clearly-marked, separate** `known_hosts` file — never touching the human's:

```
# BEGIN managed by roster ssh plugin -- do not edit
100.115.27.55 ssh-ed25519 AAAAC3Nza...
dgx           ssh-ed25519 AAAAC3Nza...
dgx.tailf4fa3f.ts.net ssh-ed25519 AAAAC3Nza...
# END managed by roster ssh plugin
```

  referenced via a generated `~/.ssh/config.d/roster.conf` with `UserKnownHostsFile ~/.ssh/known_hosts.roster ~/.ssh/known_hosts` (the human's file still wins for anything it already knows). **This anchors every box under *every* name it answers to** — killing the §4.4 inconsistency where `gb-mbp` knows `dgx` only as an IP and `gb-mac-mini` knows it only as a LAN name. It fixes 3 of the 9 failures in §4.1 with zero secret handling.

  **The one caveat, stated plainly [DEDUCED]:** this is trust-on-first-gossip. A box that joins the tailnet can assert a host key for its own name. On a 3-box network of machines Greg owns — which BRIEF §4 explicitly scopes as trusted — that is a sound trade. On an untrusted network it would not be, and §6 puts untrusted machines out of scope. If that ever changes, this is the first thing to revisit.

**Phase 3 — Propose authorized_keys changes; never apply them.** When the plugin sees `no_key` for `dgx → gb-mac-mini`, it does **not** fix it. It emits the exact command for the *human* to run, having already done all the discovery:

```
ROSTER: dgx cannot ssh to gb-mac-mini (no_key).
  dgx has no SSH private key at all (~/.ssh/id_* missing).
  To fix, run ON dgx:      ssh-keygen -t ed25519 -C "gb@dgx"
  then approve with:       roster ssh approve dgx -> gb-mac-mini,gb-mbp
  which will append dgx's PUBLIC key to authorized_keys on those boxes.
```

The `approve` step is a human typing a command. The daemon does the tedious part — knowing *who* can't reach *whom* and *why* — and the human keeps the one decision that grants shell access. **[DEDUCED]** design judgement, and I hold it firmly: this is the line where "helpful" becomes "wrote itself a backdoor across three machines".

### 8.3 What core must expose for this plugin to work

This is the part that constrains the kernel, so it's the part that matters for the build:

1. **Read access to the roster**, with change notifications — BRIEF §3.1 already anticipates this: *"Maybe it needed changes in the node list."* The plugin must be able to say "tell me when a node joins/leaves/changes metadata".
2. **A slot in Tier-1 metadata** the plugin can write (the `hk` fingerprint). This is the one kernel concession the plugin needs, it is ~50 bytes, and it must be **byte-budgeted against the 512B cap from §6.1** — which is an argument for the kernel *owning* that budget and rejecting oversize plugin writes rather than letting a plugin panic the daemon.
3. **A slot in Tier-2 state** for the connectivity matrix (`ssh_out`).
4. **Nothing else.** In particular the kernel needs **no** SSH knowledge whatsoever — no key handling, no `known_hosts` awareness, no shelling out. That is the §3.1 "light kernel, heavy plugins" split working exactly as intended: the kernel moves bytes and tracks who's alive; the plugin knows what SSH is.

That last point is worth stating as a check on the design: **the SSH plugin needs exactly two named byte-slots and an event feed.** If a plugin this fiddly needs no more than that, the kernel/plugin boundary is drawn in the right place.

---

## 9. What I'd build, concretely

1. **Roster = memberlist v0.6.0**, `DefaultLANConfig()` with these deltas:
   - `Name` = the roster node name (`dgx`), **not** `os.Hostname()` — note `gb-mac-mini` reports its hostname as `gb-mac-mini.local`, which would silently produce a second, wrong identity. [MEASURED.]
   - `BindAddr`/`AdvertiseAddr` = **the Tailscale IP**, not `0.0.0.0`. The roster lives on the tailnet and nowhere else.
   - `SecretKey` set (memberlist supports symmetric encryption) — cheap, and it means a random device joining the tailnet cannot join the roster.
   - Defaults for all timings. The measured 6s detection is already better than this fleet needs; do not tune what you have not measured a problem with.
2. **Seeds = the two always-on boxes** (`dgx`, `gb-mac-mini`) by Tailscale IP, in a config file. The laptop is never a seed. Note from §3 this only affects the first second of a node's life — there is no hub.
3. **Address source = `tailscale status --json`**, read at startup and on change, to populate Tier-1. Do not resolve names. Do not read `~/.ssh/config`. Do not trust DNS — §4.3 measured why.
4. **Two hooks on the laptop:** sleep → `UpdateNode(intent=sleeping)` + `Leave()`; wake → `Join(seeds)`. Plus a slow `Join(seeds)` retry whenever member count < expected, as insurance against the 8-hour case I could not measure (§5.2).
5. **A hard test that Tier-1 metadata is < 512 bytes**, and a `NodeMeta()` that clamps and logs rather than panics (§6.1).
6. **Agent list in push/pull `LocalState`/`MergeRemoteState`**, with broadcast-queue deltas for latency (§6.2).
7. **Then** the SSH plugin, in the Phase 1 → 2 → 3 order above. Phase 1 is read-only, needs no credentials, and would already tell Greg more about his fleet than he knows today.

**Rough size [DEDUCED]:** the roster kernel is ~300–400 lines of Go around memberlist — the `Delegate` and `EventDelegate` interfaces are 4 and 3 methods respectively, and the working experiment in §3/§5 was ~80 lines. This is a small piece of code. The measurement work in §4 was the hard part, and it is done.

---

## 10. Answers to the brief's questions, in one line each

1. **Membership option?** `hashicorp/memberlist` v0.6.0 — pushed today, MPL-2.0, the library under Consul/Nomad/Serf. Serf lost (it's a whole daemon; Greg is writing one). etcd/ZK/Consul lost (consensus needs a quorum; a 3-box fleet with a sleeping laptop means 2 voters means zero fault tolerance means a fatal hub — violates §4). Hand-rolled lost, **but not on scale** — 3 boxes is genuinely small; it lost because rejoin needs incarnation numbers, suspicion, and anti-entropy, and you'd get them wrong.
2. **Sleeping laptop?** Declared dead in **~6s** [MEASURED]; rejoins **automatically in 0.7s** after a 75s absence via incarnation refute [MEASURED]. memberlist **cannot** distinguish asleep from dead — nothing can, on the wire. Announce it: `intent=sleeping` + `Leave()` before sleep (~0.6s propagation vs ~6s), `Join(seeds)` on wake.
3. **Not-centralized?** **CONFIRMED by experiment** — I killed the seed node and the two survivors kept gossiping agent updates to each other (§3).
4. **Does Tailscale solve the address book?** **It solves the hard half (reachability, stable IDs, stable IPs) and is currently failing the half that stops agents.** MagicDNS is enabled tailnet-wide but not plumbed into either Mac's resolver — `tailscale ssh gb@dgx` fails on Tailscale's own name. `ssh dgx` from the laptop goes over the **home LAN**. `dgx` can SSH **nowhere** (no private key, empty `known_hosts`). Verdict: **use Tailscale as the network and the address *source*; the roster is the *directory*** — it maps daemon-defined agent names to boxes, which Tailscale has no concept of.
5. **Node entry?** Two tiers, forced by a measured **512-byte hard cap** that **panics the daemon** if exceeded: Tier 1 = identity + one address (the Tailscale IP) + intent, ~190ms propagation; Tier 2 = agent list + summaries via push/pull. Agent name is the key; box is an attribute (§6.3).
6. **Dead box?** ~6s worst case, all peers agreeing within ~1ms. The sender gets an **instant local** `ErrNodeDown{node, last_seen, intent}` instead of a 30-second TCP timeout. That is liveness as a routing input.
7. **SSH plugin?** Verify (read-only, free, high value) → distribute **host public keys** via metadata (safe; they're public by construction) → **propose** `authorized_keys` changes for a human to approve. Private keys never move. Core exposes exactly: roster reads + change events, one Tier-1 byte-slot, one Tier-2 slot.

---

## 11. Open questions — things I could not determine, stated rather than guessed

1. **The true 8-hour sleep.** I measured a 75-second `SIGSTOP`, not a real overnight macOS sleep with the network interface torn down and the dead entry fully reaped from peers' node maps. The push/pull anti-entropy path should cover it; **I did not verify this.** The `Join(seeds)`-on-wake recommendation exists specifically because I could not.
2. **The broadcast-queue path for agent-list deltas.** I measured `Meta` (~190ms) and confirmed push/pull is TCP with no 512B cap, but I did **not** measure `GetBroadcasts`/`NotifyMsg` throughput or the real payload ceiling against `UDPBufferSize: 1400`.
3. **Why is MagicDNS not plumbed into the Macs?** I measured *that* it isn't and the exact mechanism (`/etc/resolver/search.tailscale` has a search domain but no nameserver; `tailscale dns status` says "no resolvers configured"). I could not determine **why** — most likely "Override Local DNS" is off in the admin console, but that requires console access I don't have. **This is worth 5 minutes of Greg's time**; it would fix 6 of 9 rows in §4.1 independent of anything else here.
4. **Tailscale's own offline-detection latency.** I saw `tailscale status` correctly report "offline, last seen 38d ago" for dormant boxes, but I did not benchmark how fast its control plane notices a box going down, so my "the roster detects faster" claim is [DEDUCED] from architecture (local probes vs a coordination server), not measured head-to-head.
5. **`dgx`'s two live LAN IPs** (`192.168.1.155` and `192.168.1.86`, both answering with dgx's host key). Probably wired + wireless interfaces. I did not confirm which, and it does not affect the recommendation — the roster uses neither.
6. **memberlist's encryption on a 3-box tailnet** — `SecretKey`/`Keyring` exist and I recommend using them, but I did not test key rotation or the cost of enabling encryption.
7. **The iPhone ShellFish RSA key** in `gb-mac-mini`'s `authorized_keys`, and three extra offline boxes in `tailscale status` (`gb-mac`, `gb-mac-old`, `nvsync-gb-mac-mini`). Out of scope for this task, but the roster's seed list and any SSH plugin need an explicit answer for "boxes on the tailnet that are not fleet members" — the roster should be an **explicit allow-list of the 3 boxes**, not "everything on the tailnet".

---

## Sources

**Files read (local):**
- `/Users/gb/research/2026-07-15-agent-substrate-v2/BRIEF.md` (full)
- `/Users/gb/.ssh/known_hosts`, `/Users/gb/.ssh/id_ed25519.pub` (public key only)
- `/etc/hosts`, `/etc/resolv.conf`, `/etc/resolver/search.tailscale`
- `~/go/pkg/mod/github.com/hashicorp/memberlist@v0.6.0/config.go` (`DefaultLANConfig`, lines ~81–330)
- `.../memberlist@v0.6.0/net.go` line 83 (`MetaMaxSize = 512`)
- `.../memberlist@v0.6.0/memberlist.go` lines 457–462, 516–521 (oversize meta → `panic`)
- `.../memberlist@v0.6.0/util.go` lines 70–75 (`suspicionTimeout`)
- `.../memberlist@v0.6.0/state.go` lines 1205–1232 (suspicion setup, `k = SuspicionMult-2`, `k=0` at n=3)
- `.../memberlist@v0.6.0/suspicion.go` lines 48–78 (`newSuspicion`, `if k < 1 { timeout = min }`)
- `.../memberlist@v0.6.0/delegate.go` line 33 (`LocalState(join bool) []byte`)
- Remote: `dgx:~/.ssh/` listing + `authorized_keys` fingerprints; `gb-mac-mini:~/.ssh/config`, `~/.ssh/config.d/{sd-t1,sd-vm1}`, `known_hosts` host fields, `authorized_keys` fingerprints

**Commands run:**
- `hostname`, `whoami`, `uname -a`, `sw_vers`, `cat /etc/os-release`
- `tailscale status`, `tailscale status --json`, `tailscale version` (1.98.5), `tailscale debug prefs`, `tailscale dns status`
- `scutil --dns`, `dscacheutil -q host -a name <n>`, `host <n>`, `dig +short @100.100.100.100 <n>`, `getent hosts <n>` (on dgx)
- `ssh -o BatchMode=yes -o StrictHostKeyChecking=yes <target> ...` — the full 9×/6×/6× matrix from all three boxes
- `ssh -v dgx true` (resolution trace → 192.168.1.155)
- `ssh-keyscan -t ed25519 <100.115.27.55|192.168.1.155|192.168.1.86|100.120.22.74|100.87.151.114>` piped to `ssh-keygen -lf -`
- `ssh-keygen -F <host> -l`, `ssh-keygen -lf <pubkey>`, `ssh-add -l`
- `ping -c 2 192.168.1.155`; `lsof -nP -iTCP:22 -sTCP:LISTEN`; `netstat -an | grep '\.22 .*LISTEN'`; `launchctl print-disabled system | grep ssh`
- `go version` (gb-mbp: go1.26.2 darwin/arm64; dgx: not installed), `go get github.com/hashicorp/memberlist@latest` → **v0.6.0**

**Experiments I wrote and ran** (source in `/tmp/mlprobe/`, processes cleaned up afterwards):
- `main.go` / `mlprobe` — 3-node memberlist cluster (`dgx`:7946, `gb-mac-mini`:7947, `gb-mbp`:7948 on 127.0.0.1) with join/leave/update event logging. Used for the freeze/wake tests: `SIGSTOP` 75s → dead in 6.6s; `SIGCONT` → rejoin in 0.7s with `[WARN] memberlist: Refuting a suspect message`.
- `mlprobe2` — added live `UpdateNode` metadata changes via `SIGUSR1`/`SIGUSR2`. Used for: meta propagation (~190ms to both peers); the 1,563-byte oversize test (silent truncation to 512B, `err=nil`, corrupt JSON at receiver); and the seed-kill test (`SIGKILL` dgx → survivors kept exchanging agent-list updates).
- `mlprobe3` — graceful `Leave()` test via `SIGUSR1` (~0.6s propagation; `n.State=0` in the `NotifyLeave` callback, i.e. callback does not distinguish left from dead).

**URLs fetched:**
- `https://api.github.com/repos/hashicorp/memberlist` — 4,085 stars, created 2013-09-09, `pushed_at` 2026-07-15, not archived, MPL-2.0
- `https://api.github.com/repos/hashicorp/serf` — 6,066 stars, created 2013-10-01, `pushed_at` 2026-07-07, not archived, MPL-2.0
