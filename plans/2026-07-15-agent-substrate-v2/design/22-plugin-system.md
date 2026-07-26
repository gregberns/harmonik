# 22 — The Plugin System

**Date:** 2026-07-15/16
**Scope:** BRIEF §3.2. The plugin mechanism, the live-reload state machine, the protobuf contract, versioning, the manifest, failure behaviour, and the plugin-library idea.
**Inputs read in full:** `investigate/12-plugins-live-reload.md`, `investigate/14-prior-art.md`. Also read: `BRIEF.md` (full), and the sections of `investigate/11-transport-options.md` and `investigate/13-roster-and-addressbook.md` that constrain this contract.
**Labels used throughout:** **[MEASURED]** = I ran it today and this is the output. **[CLAIMED]** = a doc or maintainer says so; I did not verify. **[DEDUCED]** = my inference from the other two. Measurements marked *(inv-12)* or *(inv-14)* are a sibling's, re-cited not re-run; everything else marked [MEASURED] is mine, from this session, with the code on disk.

---

## 0. The answer, in one page

**Use `hashicorp/go-plugin` v1.8.0.** A plugin is an ordinary Go program compiled to its own binary. The daemon launches it as a child process and talks to it over gRPC — which is protobuf — on a unix socket (a file on disk that two processes on the same machine use to talk; no network involved). **Live reload = kill the child, start the new binary. The daemon never stops.**

I re-derived this from scratch rather than inheriting it, and measured it on both of Greg's stated axes:

| Greg's axis (§3.2) | Result |
|---|---|
| **Live-loadable** (axis 1) | **[MEASURED]** Reload cycle — spawn, handshake, register, first call: **3–4 ms on the dgx**, **7–11 ms on gb-mbp**. The daemon's process never changes. |
| **protobuf interface, defined independently of code** (axis 2) | **[MEASURED]** go-plugin's own internals are three `.proto` services *(inv-12)*. I wrote the full contract as `.proto`, linted it, compiled it, and ran breaking-change detection on it — all with tooling already installed on gb-mbp. |

Four things I measured that change the design, and that were open questions before this session:

1. **`Kill()` does not drain in-flight work.** go-plugin's shutdown path calls gRPC's hard `Stop()`, not `GracefulStop()` — the source carries a `TODO: figure out why GracefullStop doesn't work`. **[MEASURED]** A 3-second call in progress was **cancelled after 504 ms** with `Unavailable ... EOF`. **So the kernel must own the drain.** I built the drain gate (~40 lines) and measured it working: the same in-flight call **completed successfully**.
2. **The macOS 500 ms reload penalty can be moved off the reload path.** **[MEASURED]** First execution of a never-before-seen binary on gb-mbp: **505 ms**. That is macOS validating an unseen binary's code signature. Exec it once at *install* time and throw the result away (**385 ms, paid once, off the hot path**) and the subsequent real reload is **9 ms**. On Linux the penalty does not exist at all: **7 ms** first-exec. This closes inv-12's open question #2.
3. **Forward compatibility — Greg's "(maybe forwards?)" — is real, and I measured it.** A v1 daemon parsed a v2 plugin's manifest, **retained 8 bytes of fields it cannot read**, and forwarded them intact; a v2 reader downstream recovered them exactly. **But** routing the same message through JSON **silently destroyed them, with no error.** Binary protobuf on plugin↔daemon and daemon↔daemon. Always.
4. **The fleet is exactly two platforms.** **[MEASURED]** `gb-mac-mini` is `Darwin arm64`. It was unreachable for both sibling investigations (`Host key verification failed`); it answers to `-o StrictHostKeyChecking=accept-new`. So: `darwin/arm64` ×2, `linux/arm64` ×1 — **two plugin artifacts, not four.** This closes an open question flagged in both inv-12 and inv-14.

**The honest headline: Greg wants BEAM and is not going to get BEAM.** Go has no `code_change/3` and cannot have one. What he actually asked for — *"having to stop the whole system every time is so annoying"* — is fully delivered. What he did not ask for but might imagine comes with it — a plugin keeping its memory across a code swap — does not exist in Go and never will. The price of live reload is that **plugins may hold no in-memory state**. The refund is that he already independently asked for the fix in §3.1: *"a storage mechanism in the daemon. Then the plugins dont make up their own thing."* State lives in the kernel; the plugin is a pure function of it. Vault reached the identical conclusion from the identical direction *(inv-12)*.

**Alternatives, and why they lost** (ranked by how close they came):

- **WASM (wazero)** — genuine runner-up. Faster (6.97 µs/call vs 45 µs) and **one artifact for all platforms**, which is a real win for the plugin library. Lost on: one module instance serves exactly one caller (8 concurrent callers corrupted the guest's allocator), guests can't do their own I/O — and log-tail is a named plugin that wants exactly that — extism's Go SDK untouched 14 months, and an industry record including Vector removing WASM and Envoy calling it experimental after four years *(all inv-12)*. **Not foreclosed:** the same `.proto` can be served by a WASM module later. Traefik runs both. Bank it.
- **Go's stdlib `plugin`** — disqualified by measurement, not taste. Cannot load a second build of the same plugin *at all* (`plugin already loaded`); the workaround leaks ~1 MB per reload forever; **adding one field to a shared struct invalidates every plugin binary** *(inv-12, inv-14)* — which is the exact *opposite* of protobuf's compatibility rules and therefore the opposite of what §3.2 asks for. The Go stdlib docs themselves recommend IPC instead.
- **Yaegi** — the only Go option that reproduces a real BEAM property (two live versions at once, 3.6 ms). Lost because **a plugin goroutine panic hard-kills the daemon** (exit code 2, unfixable — you cannot `recover()` another goroutine's panic), no release since 2024-04-03, generics broken, and Traefik — its own owner — pinned it and built a WASM runtime beside it *(inv-12, inv-14)*. It offers BEAM's code-loading half with **anti**-supervision. Wrong half.
- **Roll your own subprocess + protobuf** — this *is* go-plugin, minus 7,858 lines where nine years of production bugs are already fixed *(inv-12)*. Validated by osquery and Telegraf `execd`, so it stays a real fallback if the gRPC dependency tree ever becomes intolerable. It won't.
- **Compile-time modules (Caddy's model)** — has no live reload by definition. It is the honest baseline: a first-rate Go daemon looked at this exact problem and chose "rebuild the binary." We beat it, but it is not a disgrace.

---

## 1. Words, defined once

Per §8, every term defined at first use. Greg is a strong engineer, not a specialist in this corner.

| Term | Meaning |
|---|---|
| **protobuf** (Protocol Buffers) | A plain-text file format (`.proto`) that describes messages and function signatures independently of any programming language. A code generator turns one `.proto` into Go structs, Python classes, whatever. The wire format is compact binary. This is §3.2's *"define it COMPLETELY independently of any code"*. |
| **gRPC** | A way to call a function in another process using protobuf messages. Works fine over a unix socket. |
| **unix socket** | A file on disk that two processes on the same machine use to talk. No network, no port, no firewall. |
| **RPC** | Remote Procedure Call — calling a function that lives somewhere else. |
| **IDL** | Interface Definition Language — the general name for "a file that defines an interface independent of language." protobuf is one. |
| **buf** | A tool that lints `.proto` files and detects when you have broken a contract. Already installed on gb-mbp. **[MEASURED]** |
| **unary call / streaming call** | Unary = one request, one response. Streaming = one request, a continuing feed of responses (this is the shape of "tail a log"). |
| **drain** | Stop sending new work to something, let its current work finish, *then* stop it. |
| **in-flight** | A call that has been sent but has not yet come back. |
| **BEAM** | The Erlang/Elixir virtual machine. The thing Greg has envy of. |
| **hot code loading** | BEAM's trick: replace code in a running process *while processes keep running and keep their memory*. Go cannot do this. |
| **live reload** (as used here) | The weaker, achievable thing: swap the code, keep the **daemon** running. The plugin's own memory does not survive. |
| **`code_change/3`** | The Erlang callback that hands your old in-memory state to your new code so you can convert it. **Nothing outside BEAM has this.** It is the specific thing Greg cannot have. |
| **crash isolation** | A plugin blowing up doesn't kill the daemon. |
| **manifest** | A small record describing a plugin: its name, version, what it wants. Data, not code. |
| **RSS** | Resident Set Size — how much physical memory a process is actually using. |

One distinction that explains most of the confusion in this space:

- **Reloading *configuration*** — easy. Everyone does it. `SIGHUP`.
- **Reloading *code*** — hard. This document.

When someone says "Caddy has hot reload," they mean the first. Caddy has zero of the second, on purpose.

---

## 2. The mechanism: re-derived, not inherited

The previous round cut go-plugin. Its brief never mentioned live reload or protobuf, so the cut was made on axes that do not exist here *(inv-12, and BRIEF §9 confirms the pattern)*. I did not take the sibling investigations' word for it either. Here is the derivation on Greg's two stated axes, with my own measurements.

### 2.1 Axis 1 — live reload

The requirement, in his words: *"In harmonik having to stop the whole system every time is so annoying."*

Note precisely what that is and is not. It is **"don't make me stop everything."** It is *not* "swap code inside a running process." Those are different asks, and the second one is the one Go cannot do. The first one is fully deliverable.

**[MEASURED — this session]** The full reload cycle (spawn the new binary → handshake → dispense → first successful call), same harness on both platforms:

```
=== reload latency on linux/arm64 (dgx, over Tailscale, real box) ===
  FIRST exec of a never-before-seen binary: 7ms
  warm reload #1: 4ms      warm reload #4: 4ms
  warm reload #2: 4ms      warm reload #5: 4ms
  warm reload #3: 3ms

=== reload latency on darwin/arm64 (gb-mbp) ===
  FIRST exec of a never-before-seen binary: 505ms
  warm reload #1: 11ms     warm reload #4: 8ms
  warm reload #2: 10ms     warm reload #5: 7ms
  warm reload #3: 9ms
```

Code: `/tmp/drainexp/host/main.go`, cross-compiled `CGO_ENABLED=0 GOOS=linux GOARCH=arm64`, copied to `dgx:/tmp/reloadtest/`, run there.

Two findings in that table:

**(a) The 505 ms is macOS-only and it is avoidable.** It reproduces inv-12's 449 ms with an independent harness, and the dgx number proves the diagnosis: it is macOS validating the code signature of a binary it has never seen. **[MEASURED]** It can be pre-paid:

```
  pre-warm bare exec of fresh binary: 385ms     <- paid at INSTALL time, thrown away
  second bare exec (now warm):        9ms
  --- real reload cycle against that same, now-prewarmed binary ---
  FIRST exec of a never-before-seen binary: 9ms  <- was 505ms
```

**Design rule:** after fetching and verifying a plugin binary, **exec it once and discard the result.** The 500 ms leaves the reload path. Reload is then ~9 ms on macOS and ~4 ms on Linux. (Ironically the dgx — the box that is always on and does the heavy work — has the fastest reload of the three.)

**(b) go-plugin's reload is ~1000× faster than the requirement.** Greg's complaint is measured in *"annoying,"* i.e. human seconds. This is 4 ms.

**How the alternatives score on this axis:** stdlib `plugin` = **impossible**, full stop (`plugin already loaded` — it is not slow, it is refused) *(inv-12)*. WASM = ~1–5 ms instantiate but **1.58 s to compile a 7.8 MB protobuf module** *(inv-12)*. Yaegi = 3.6 ms and genuinely two live versions *(inv-12)*. Caddy-style rebuild = the problem, not the fix.

So on axis 1, three options tie at "fast enough" and one is impossible. **Axis 1 does not decide it.** That is worth saying plainly, because it means the usual argument (reload speed) is not the argument.

### 2.2 Axis 2 — protobuf-nativeness

This is the axis that decides.

Greg: *"I say protobuf because we can the define it COMPLETELY independently of any code. Also could be cool because the same interfaces could be available through REST/pubsub/whatever."*

- **go-plugin:** the transport *is* protobuf. Its own internals are three `.proto` services *(inv-12)*. **[MEASURED — this session]** I wrote the entire plugin contract (§4 below) as `.proto`, and `buf lint` + `buf build` + `buf breaking` all work against it with tooling already on the box. The `.proto` is the artifact; the Go is generated from it. **This is not an adaptation. It is the tool's design.**
- **WASM:** protobuf *works* inside a WASM module — inv-12 proved it — but Go 1.26's `//go:wasmexport` allows only scalars and pointers, so **`[]byte` cannot cross the boundary.** You hand-marshal every message through the guest's linear memory (`ptr<<32|len` packing). It works; it is plumbing you own forever. **[CLAIMED — inv-12, verified by them, not by me.]**
- **Yaegi:** the interface is "a Go function value pulled out of an interpreter by name." That is the **most Go-coupled option on the board** — the precise opposite of "independent of any code."
- **stdlib `plugin`:** the interface is Go symbols. Worse: **adding a field to a shared struct invalidates every plugin binary** *(inv-12, inv-14, both measured it independently)* — while adding a field is the canonical **safe** change in protobuf, which I re-measured this session (§5). Choosing stdlib `plugin` means the single most ordinary evolution of the system requires a synchronised rebuild of every plugin on all three boxes. It has *inverted* compatibility properties versus the thing Greg's instinct picked.

**Verdict: go-plugin wins axis 2 outright, and axis 1 is a tie among the survivors. That is the derivation.** It is not "HashiCorp is popular."

### 2.3 The third axis Greg didn't name but is paying for anyway

**Crash isolation.** Separate processes mean separate address spaces, enforced by the CPU's memory-management unit. A plugin panic is an ordinary Go `error` at the call site *(inv-12 measured it; I saw the same error shape in my own runs)*. You cannot get this wrong. Yaegi's option gives you the reverse — a plugin goroutine panic kills the daemon, and that is a Go language rule, not a bug anyone can fix.

This matters more here than it looks. Greg's §4 is *"Part of the problem is that I dont trust my boxes."* A plugin system where a buggy plugin takes down the daemon makes every box less trustworthy, not more. Subprocesses are the only isolation primitive that works without a VM designed for it. Erlang built the VM. Nobody else did.

### 2.4 The cost, stated plainly

- **~17 MB per plugin binary; ~120 MB for the daemon plus all six of §3.2's named plugins** *(inv-12)*. My build: **18.3 MB [MEASURED]**. gb-mac-mini has 37 GiB free. That is 0.3%. **At this scale this is not an objection.**
- **gRPC's dependency tree** — ~44 lines of `go.sum` *(inv-12)*. If you are going protobuf anyway (§3.2 says you are), you were paying most of this regardless.
- **You write a small adapter per interface.** go-plugin's own README calls this *"the most tedious and time consuming step."* It is ~40 lines of mechanical code. Real, but boilerplate, not difficulty.
- **One OS process per plugin per box.** The library supervises them. You choose the restart policy (§7).

---

## 3. Live reload, concretely

This is the section that earns the document, because **the constraint is the design.**

### 3.1 The key structural insight, and it is Greg's own

> §3.1: *"What if the daemon had 'channels', a channel had a name and a type (pubsub, etc), then the daemon would do data transport, while the plugin handled all the logic."*

**Because channels live in the kernel, a plugin reload never touches a channel.** The plugin does not *hold* a subscription — the kernel holds it, on the plugin's behalf, as a fact recorded in the plugin's manifest. When a plugin is swapped, its subscriptions do not go anywhere, because they were never in the plugin.

That falls straight out of Greg's own architectural decision, and it is what makes reload cheap. Compare the alternative he rejected — *"Then the comms system is hard coded into the daemons internals"* — or the other alternative where a plugin owns its own transport connections: in both, swapping the plugin means re-establishing the world.

**So: "what happens to subscriptions during a swap?" — nothing. They are kernel state.** Only *in-flight dispatches* are affected, and that is a bounded, solvable problem. Which brings us to the thing I had to measure.

### 3.2 go-plugin does NOT drain in-flight work. The kernel must. [MEASURED]

This was inv-12's open question #3 (*"Does `plugin.Client.Kill()` drain in-flight gRPC calls, or hard-kill?"*). It is squarely a plugin-system question, so I answered it — first by reading the source, then by running it.

**The source.** `Kill()` is *not* a naive kill — it calls `client.Close()` and waits up to 2 seconds for the process to exit gracefully before force-killing (`client.go:498-572`). That looks reassuring. It is not, and the reason is one layer down. `GRPCClient.Close()` calls `controller.Shutdown()` (`grpc_client.go:105-109`), and here is that handler **in full** (`grpc_controller.go`):

```go
// Shutdown stops the grpc server. It first will attempt a graceful stop, then a
// full stop on the server.
func (s *grpcControllerServer) Shutdown(ctx context.Context, _ *plugin.Empty) (*plugin.Empty, error) {
	resp := &plugin.Empty{}

	// TODO: figure out why GracefullStop doesn't work.
	s.server.Stop()
	return resp, nil
}
```

**The doc comment describes behaviour the code does not have.** It promises a graceful stop first; the code calls `Stop()` immediately, with a TODO admitting `GracefulStop` doesn't work. `GracefulStop()` *does* exist on the type (`grpc_server.go:129`) and is **never called on the shutdown path** — the only `GracefulStop` call in the library is in `grpc_broker.go:398`, an unrelated path. gRPC's `Stop()` cancels every active RPC immediately.

**The measurement.** A plugin with a deliberately slow 3-second call, killed 500 ms in:

```
=== TEST 1: Kill() with an in-flight 3s RPC ===
  in-flight call is running; calling Kill() now...
  Kill() returned after 16ms
  in-flight call returned after 504ms
  in-flight result: val="" err=rpc error: code = Unavailable desc = error reading from server: EOF
  >>> VERDICT: in-flight RPC was CANCELLED, not drained.

=== TEST 2: Kill() when plugin is idle (the normal reload path) ===
  idle Kill() #1: 8ms   #2: 6ms   #3: 6ms
```

**Conclusion: the plugin's work is abandoned mid-flight and the caller gets a transport error.** If the kernel calls `Kill()` naively, every message being handled at that instant is lost or duplicated, and *"all the messages are written down"* (§5) becomes a lie during exactly the operation Greg most wants to be routine.

### 3.3 The drain gate — the ~40 lines the kernel owes. [MEASURED working]

Since go-plugin won't drain, the kernel does. I built it and measured it:

```
=== TEST 4: kernel-side drain gate around the SAME Kill() ===
  reload requested: draining (stop new work, cancel streams, wait for in-flight)...
  drain completed=true in 2.501s
  new call arriving mid-drain: REJECTED cleanly (kernel requeues it)
  in-flight unary result: "slow work completed"          <-- COMPLETED, not lost
  subscription result: rpc error: code = Canceled desc = context canceled
```

The same 3-second call that was destroyed in Test 1 **completed successfully**. The gate:

```go
// The kernel's drain gate. Required because go-plugin's Kill() hard-stops
// the gRPC server (grpc_controller.go: "TODO: figure out why GracefullStop
// doesn't work"). Measured: without this, in-flight calls die; with it, they finish.
type gate struct {
	mu       sync.RWMutex
	draining bool
	inflight sync.WaitGroup
	cancels  map[int]context.CancelFunc // long-lived streams
	next     int
}

// enter is called before dispatching a unary call to the plugin.
// Returns false if a reload is underway: the caller requeues, it does NOT drop.
func (g *gate) enter() bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	if g.draining {
		return false
	}
	g.inflight.Add(1)
	return true
}
func (g *gate) exit() { g.inflight.Done() }

// track registers a stream's cancel func so drain can cut it.
func (g *gate) track(c context.CancelFunc) int { /* ... */ }

// drain: stop new work, cancel streams, wait for unary calls to finish.
func (g *gate) drain(timeout time.Duration) bool {
	g.mu.Lock()
	g.draining = true
	for _, c := range g.cancels {
		c() // subscriptions are CANCELLED, never waited for: they never end
	}
	g.cancels = map[int]context.CancelFunc{}
	g.mu.Unlock()

	done := make(chan struct{})
	go func() { g.inflight.Wait(); close(done) }()
	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false // hung plugin: fall through to Kill() anyway
	}
}
```

Full runnable source: `/tmp/drainexp/host/main.go`.

**The asymmetry that matters, and it is not obvious:**

- **Unary calls are drained** — waited for. They end on their own.
- **Streams are cancelled** — never waited for. **A subscription never ends by itself; a drain that waits for one waits forever.** This is why the gate tracks two different things. Get this wrong and your first reload of the log-tail plugin hangs the daemon.

**A free diagnostic I did not expect.** The two shutdown paths are *distinguishable at the caller* **[MEASURED]**:

| What happened | Error the caller sees |
|---|---|
| Kernel deliberately cut the stream (planned reload) | `code = Canceled desc = context canceled` |
| Plugin vanished (crash, or un-drained kill) | `code = Unavailable desc = error reading from server: EOF` |

`Canceled` is local cancellation; `Unavailable` is transport failure. **So the kernel can tell "I did this on purpose" from "it died" without any bookkeeping** — useful for logging, and for deciding whether to count a stop against the crash-loop budget (§7).

### 3.4 The state machine

```
                    ┌──────────────────────────────────────────────┐
                    │                                              │
  DISCOVERED ──► VERIFIED ──► PREWARMED ──► LAUNCHING ──► HANDSHAKING
   manifest      sha256        exec once      fork/exec     read one line
   on disk       matches       & discard      the binary    from stdout
   (§6)          (§7.1)        (macOS 500ms                 (§4.4)
                               paid HERE)                        │
                                                                 ▼
   RUNNING ◄──── STARTING ◄──── REGISTERED ◄──── DESCRIBING ─────┘
      │          Start()        kernel records     Describe()
      │          plugin gets    manifest; opens    plugin returns
      │          kernel addr    channels; wires    its manifest
      │          + token        interests
      │
      ├── reload requested ──► DRAINING ──► STOPPED ──► (new binary) VERIFIED ──┐
      │                        stop new     Kill()                              │
      │                        cancel streams                                   │
      │                        wait in-flight                                   │
      │                        (§3.3)                                           │
      │                                                                         │
      ├── plugin died ────────► CRASHED ──► BACKOFF ──► LAUNCHING               │
      │                         (Unavailable)  100ms→30s                        │
      │                                                                         │
      ├── 5 crashes in 60s ───► FAILED (terminal; reported to roster;           │
      │                                 daemon stays up; CLI clears it)         │
      │                                                                         │
      └─────────────────────────────────────────────────────────────────────────┘
```

**What the kernel does at each edge that matters:**

| Edge | Kernel behaviour |
|---|---|
| **RUNNING → DRAINING** | Sets `draining`. New dispatches are **rejected, and the message stays in the kernel's channel queue** — not dropped. Cancels every stream. Waits for in-flight unary calls up to a deadline. |
| **DRAINING → STOPPED** | Calls `Drain()` on the plugin **if the manifest declares `CAPABILITY_DRAIN`** (last chance to flush to kernel storage), then `client.Kill()`. |
| **STOPPED → VERIFIED** | sha256 of the new binary against the manifest. **Mismatch = refuse, keep the old plugin stopped, report. Never exec unverified bytes.** (§7.1) |
| **REGISTERED** | The kernel **diffs the new manifest against the old**. Channels that persist are untouched — the queue never drained. New channels are opened; removed channels are closed. **This is why a reload is not a re-subscribe.** |
| **STARTING → RUNNING** | Resume dispatch. Queued messages flow. |
| **CRASHED** | Distinguished from a deliberate stop by `Unavailable` vs `Canceled` (§3.3). Counts against the crash-loop budget. |

**Total observable interruption: the drain time plus ~4–9 ms.** For a well-behaved plugin (returns from `Deliver` in single-digit ms), that is ~10 ms of queueing. Messages are not lost; they wait.

### 3.5 What live reload forbids — the hard rules

**[MEASURED — inv-12]** go-plugin reload, then read back state written before the reload: `state after reload: ""`. Every time. The plugin's map was gone. Same for WASM: `mod.Close()` frees the entire linear memory.

This is not a bug to work around. It is the **defining property of the entire family**, and BEAM avoids it *only* via `code_change/3`, which hands your old state to your new code and asks you to migrate it. **Nothing in Go has an equivalent and nothing in Go can** — there is no mechanism to hand a struct built by one binary's compiler to a different binary's compiler and have it mean the same thing.

**The rule: a plugin must be a pure function of kernel-held state plus its inputs.**

Concretely, a plugin **must not**:

1. **Hold state in a Go variable that matters after a reload.** Kernel storage or nothing. This is the big one.
2. **Hold a long-lived connection to something whose re-establishment is expensive.** It will be dropped on every reload. (A plugin that must do this — e.g. a warm TCP session — is telling you it should be a *daemon* the plugin talks to, not a plugin.)
3. **Do unbounded work inside `Deliver`.** Every kernel→plugin call carries a deadline. Exceed it and you are treated as hung (§7.2).
4. **Address another plugin directly.** Route by channel name. This is Fluent Bit's tag/match model, which inv-14 shows is the only routing model that crosses a machine boundary unchanged — and it is what Greg already picked without knowing it (*"a channel had a name and a type"*).
5. **Write outside its declared storage namespaces, or open a channel not in its manifest.** The kernel enforces both from the manifest, keyed by the token issued at `Start` (§4.3).
6. **Assume the kernel endpoint from `Start` survives a reload.** Re-read it every time.
7. **Block in `Drain` past its deadline.** You will be killed anyway; you will just have made the reload slower.

**How the six named plugins fare under rule 1** — this is the test of whether the rule is livable:

| Plugin | State | Verdict |
|---|---|---|
| **log tail** | File offset | ✅ Kernel storage, keyed by path. Reload → re-read → resume. |
| **comms** | Messages, subscriptions | ✅ Holds **nothing**. §5 already says *"all the messages are written down"*; subscriptions are kernel channel registrations (§3.1). |
| **agent registry** | The roster | ✅ Holds **nothing**. §3.1 already bakes the roster into the kernel. |
| **log archiving** | Write buffer | ⚠️ Must declare `CAPABILITY_DRAIN` and flush, or write through. Livable. |
| **ssh helper** | Connectivity matrix | ✅ Kernel storage (inv-13 §8.3 asks for exactly one Tier-2 slot). |
| **notes** | Notes | ✅ Kernel storage. |

**Every one of them passes, and five of the six hold nothing at all.** That is not luck — it is because Greg's §3.1 storage instinct and his §3.2 reload instinct are the same decision seen from two sides. He wrote the storage line for consistency reasons (*"Then the plugins dont make up their own thing"*). It turns out to be **the precondition that makes reload work.** Vault discovered this in production: *"Plugins don't maintain persistent in-memory state... State management relies on Vault's storage backend"* *(inv-12)*.

**Enforce it on day one, not in month three.** A plugin that stores something important in a Go variable is a bug. If you enforce it at the start, live reload is free forever. If you don't, you find out the day you reload comms and lose the queue.

### 3.6 What live reload costs — the honest bill

- **In-flight work needs the kernel's drain gate.** ~40 lines, measured working (§3.3). Not free, but written once.
- **Versioning stops being optional.** The moment a plugin can be replaced independently, two versions of the interface exist in the world. §5 handles it — but it becomes a discipline with CI teeth, not a nice-to-have.
- **You supervise N+1 processes.** The library does spawn/monitor/reap. You choose the restart policy. **Steal Telegraf's answer — one knob** (`restart_delay = "10s"`) — and do not build a supervision-tree DSL.
- **A `.proto` build step you do not have today.** Smaller than inv-12 thought: **[MEASURED]** `protoc` is genuinely not installed, but **`buf` 1.71.0, `protoc-gen-go` v1.36.11, `protoc-gen-connect-go` and `grpcurl` are all present in `~/go/bin`** — which is simply **not on `PATH`** (`echo $PATH` confirms). `buf` does not need `protoc`. I generated Go from `.proto` this session with zero `protoc`. The only thing I had to install was `protoc-gen-go-grpc`. So the gap is one `go install` and a `PATH` line.
- **The kernel itself is still not live-reloadable, and this is the honest limit of the recommendation.** go-plugin reloads *plugins*. Changing the kernel — channels, roster, storage — still means restarting the daemon, which is the thing Greg actually complained about in harmonik. **The only mitigation is keeping the kernel genuinely small**, which is §3's whole thesis (*"maybe the core is actually really light"*). The plugin system does not make the kernel reloadable; **keeping the kernel tiny does.** go-plugin's `ReattachConfig` is the eventual escape hatch — plugins keep running while the daemon restarts under them *(inv-12, inv-14)*. Don't build for it on day one. Know it exists.

---

## 4. The plugin contract

**Every `.proto` below was written this session, linted with `buf lint` (STANDARD), compiled with `buf build`, and run through `buf breaking`. All pass. [MEASURED]** Source: `/tmp/protoexp/proto/`.

`buf lint` earned its place immediately: my first draft had `rpc Describe(DescribeRequest) returns (Manifest)` and buf rejected it — *"RPC response type "Manifest" should be named "DescribeResponse""*. It is right (a wrapper leaves room to add fields later). **With LLM agents authoring plugins, a mechanical style gate is worth a lot.**

### 4.1 The shared envelope

The kernel reads the header and never parses the payload. This is §3.1's *"the daemon would do data transport, while the plugin handled all the logic"*, expressed as a type.

```proto
syntax = "proto3";

package substrate.type.v1;

option go_package = "github.com/gb/substrate/gen/substrate/type/v1;typev1";

// ChannelType is the kernel's transport menu (BRIEF 3.1).
enum ChannelType {
  CHANNEL_TYPE_UNSPECIFIED = 0;
  CHANNEL_TYPE_PUBSUB = 1;
  CHANNEL_TYPE_POINT_TO_POINT = 2;
  CHANNEL_TYPE_REQUEST_REPLY = 3;
  CHANNEL_TYPE_FANOUT = 4;
  CHANNEL_TYPE_LOOKUP_TABLE = 5;
}

// Envelope is the only message shape the kernel understands.
// The kernel reads the header; the payload is opaque bytes it never parses.
message Envelope {
  string message_id = 1;
  string channel = 2;
  string origin_node = 3;
  int64 sent_unix_nanos = 4;
  map<string, string> tags = 5;
  string content_type = 6;
  bytes payload = 7;
  string reply_to = 8;
  string origin_agent = 9;
}
```

**Why these fields:**

- `origin_node`, `sent_unix_nanos`, `tags` — BRIEF §4: *"the machine, time, etc must ride along because search will need it."* Telegraf solved this a decade ago by making indexed tags a property of **every** message at the kernel level, which is also what makes tag-based routing possible at zero extra cost *(inv-14)*. If tags live in the payload, the kernel cannot route on them and the future search plugin must parse N payload formats.
- `payload` is `bytes`, opaque. **The kernel never unmarshals it.**
- `content_type` lets a plugin say what's in the payload without the kernel caring.
- `reply_to` carries request/reply.
- ⚠️ **`sent_unix_nanos` is metadata, not an ordering key.** Three boxes have three clocks. The salvage investigation's top open question is cross-machine ordering, and its warning is *"never order cross-machine messages by wall clock."* **Do not sort by this field.** It is for humans and for search.

**Deliberate omission:** no `sequence` or `stream_seq`. Assigning one requires a single writer, and the single writer is the hub that §4 forbids. That question is genuinely open and belongs to the kernel design, not here.

### 4.2 What every plugin serves

```proto
syntax = "proto3";

package substrate.plugin.v1;

import "substrate/type/v1/envelope.proto";

option go_package = "github.com/gb/substrate/gen/substrate/plugin/v1;pluginv1";

// Capability is how a plugin declares which OPTIONAL rpcs it implements.
// This is Telegraf's optional-interface trick expressed in protobuf.
enum Capability {
  CAPABILITY_UNSPECIFIED = 0;
  CAPABILITY_DELIVER = 1;
  CAPABILITY_NOTIFY = 2;
  CAPABILITY_DRAIN = 3;
}

enum ResourceKind {
  RESOURCE_KIND_UNSPECIFIED = 0;
  RESOURCE_KIND_ROSTER = 1;
  RESOURCE_KIND_AGENTS = 2;
  RESOURCE_KIND_PLUGIN_MANIFESTS = 3;
}

enum ChannelMode {
  CHANNEL_MODE_UNSPECIFIED = 0;
  CHANNEL_MODE_PUBLISH = 1;
  CHANNEL_MODE_SUBSCRIBE = 2;
  CHANNEL_MODE_SERVE = 3;
}

message ChannelDecl {
  string name = 1;
  substrate.type.v1.ChannelType type = 2;
  ChannelMode mode = 3;
}

// Manifest is what a plugin declares about itself at registration.
message Manifest {
  string name = 1;
  string version = 2;
  int32 api_version = 3;
  repeated ChannelDecl channels = 4;
  repeated string storage_namespaces = 5;
  repeated ResourceKind interests = 6;
  repeated Capability capabilities = 7;
  string description = 8;
}

message DescribeRequest {}

message DescribeResponse {
  Manifest manifest = 1;
}

message StartRequest {
  string kernel_endpoint = 1;
  string plugin_token = 2;
  string node_name = 3;
}

message StartResponse {}

message DeliverRequest {
  substrate.type.v1.Envelope envelope = 1;
}

// DeliverResponse IS the ack. Returning an rpc error is a nack.
message DeliverResponse {
  bytes reply_payload = 1;
  string reply_content_type = 2;
}

message ResourceEvent {
  ResourceKind kind = 1;
  string subject = 2;
  bytes state = 3;
  int64 revision = 4;
}

message NotifyRequest {
  ResourceEvent event = 1;
}

message NotifyResponse {}

message DrainRequest {
  int64 deadline_unix_nanos = 1;
}

message DrainResponse {}

// PluginService is what EVERY plugin serves. The kernel is the client.
service PluginService {
  rpc Describe(DescribeRequest) returns (DescribeResponse);
  rpc Start(StartRequest) returns (StartResponse);
  rpc Deliver(DeliverRequest) returns (DeliverResponse);
  rpc Notify(NotifyRequest) returns (NotifyResponse);
  rpc Drain(DrainRequest) returns (DrainResponse);
}
```

**Five methods, and only two are mandatory.** `Describe` and `Start`. The other three are declared in the manifest's `capabilities` — the kernel only calls what you declared.

**That is the design's answer to Telegraf's "optional interfaces" trick.** Telegraf keeps a 2-method contract while supporting rich plugins by having the kernel type-assert for `Initializer`, `StatefulPlugin`, etc. *(inv-14)*. protobuf has no type assertions — but **the manifest is data, and data can declare what the code implements.** So: `capabilities` *is* the optional-interface mechanism. A notes plugin that only serves request/reply declares `CAPABILITY_DELIVER` and nothing else; the kernel never calls `Notify` or `Drain` on it.

**`DeliverResponse` is the ack.** This is Benthos's per-message `AckFunc` *(inv-14)* translated to protobuf, and the translation is an improvement: in a request/reply world the **RPC's own return is the ack**, so the kernel needs no message-ID bookkeeping and no closure table. Return normally = ack. Return an rpc error = nack. That answers §5's *"we actaully should probably notify the sender that the receiver is not listening... some type of ACK system"* at the plugin boundary, with one mechanism, and it composes with §4's *"durability is a PLUGIN decision"* — what the plugin does before it returns is its own business.

**`Deliver` carries request/reply too.** For a pubsub channel `reply_payload` is empty. For a `CHANNEL_TYPE_REQUEST_REPLY` channel it is the response. One method, both shapes, because the difference is the *channel's* type — which is exactly Greg's model.

### 4.3 The host API — one surface, and this is the point

```proto
syntax = "proto3";

package substrate.kernel.v1;

import "substrate/type/v1/envelope.proto";

option go_package = "github.com/gb/substrate/gen/substrate/kernel/v1;kernelv1";

message PublishRequest { substrate.type.v1.Envelope envelope = 1; }
message PublishResponse {
  int32 delivered_to = 1;
  bool no_listeners = 2;
}

message RequestRequest {
  substrate.type.v1.Envelope envelope = 1;
  int64 timeout_unix_nanos = 2;
}
message RequestResponse {
  substrate.type.v1.Envelope reply = 1;
  bool no_responders = 2;
}

message StorageGetRequest { string namespace = 1; string key = 2; }
message StorageGetResponse { bytes value = 1; int64 revision = 2; bool found = 3; }
message StoragePutRequest {
  string namespace = 1;
  string key = 2;
  bytes value = 3;
  int64 if_revision = 4;
}
message StoragePutResponse { int64 revision = 1; }
message StorageDeleteRequest { string namespace = 1; string key = 2; }
message StorageDeleteResponse {}
message StorageListRequest { string namespace = 1; string key_prefix = 2; }
message StorageListResponse { repeated string keys = 1; }

message RosterGetRequest {}
message RosterNode {
  string name = 1;
  string tailscale_ip = 2;
  string os = 3;
  string arch = 4;
  string liveness = 5;
  string intent = 6;
  int64 last_seen_unix_nanos = 7;
}
message RosterGetResponse { repeated RosterNode nodes = 1; }

// KernelService is the ONE host API. Plugins call it over a unix socket;
// agents call the identical surface over HTTP (ConnectRPC), per BRIEF 3.2.
service KernelService {
  rpc Publish(PublishRequest) returns (PublishResponse);
  rpc Request(RequestRequest) returns (RequestResponse);
  rpc StorageGet(StorageGetRequest) returns (StorageGetResponse);
  rpc StoragePut(StoragePutRequest) returns (StoragePutResponse);
  rpc StorageDelete(StorageDeleteRequest) returns (StorageDeleteResponse);
  rpc StorageList(StorageListRequest) returns (StorageListResponse);
  rpc RosterGet(RosterGetRequest) returns (RosterGetResponse);
}
```

**The design decision here is the one worth arguing about, so I will argue it.**

go-plugin ships a `GRPCBroker` that lets the plugin dial back to the host over an extra multiplexed stream. **I am not using it.** Instead, `StartRequest` hands the plugin `kernel_endpoint` (a unix socket path) and a `plugin_token`, and the plugin dials the kernel's **normal API** like any other client.

Why: **the host API a plugin calls should be the identical surface an agent calls.** BRIEF §3.2: *"the same interfaces could be available through REST/pubsub/whatever."* The transport investigation measured that ConnectRPC serves one `.proto` as plain-JSON-over-HTTP/1.1 (curl-able, no SDK), as binary protobuf, and to a stock gRPC client, all on one port *(inv-11)*. If `KernelService` is that surface, then:

- A plugin publishes with `Publish`.
- An agent publishes with **`curl`**.
- They are the same method, the same schema, the same code path.

That directly serves §4's *"agents dont have to figure that out"* and *"no agent should ever screw around figuring out the network crap."* Using `GRPCBroker` instead would create a **second, private, plugin-only host API** — a drift surface, and exactly the kind of thing that rots. **One surface. Cut the broker.**

The cost: two mechanisms in play (go-plugin gRPC for kernel→plugin lifecycle; ConnectRPC for plugin→kernel). That is a real if small complexity, and I accept it deliberately: go-plugin is managing a *lifecycle* (spawn, handshake, supervise, kill), and the host API is just *the kernel's API*. Those are different jobs and conflating them is what creates the private surface.

`plugin_token` is not a security measure (§4: trusted network). It is how the kernel knows **which** plugin is calling, so it can enforce the manifest: reject a write to an undeclared storage namespace, reject a publish to an undeclared channel. Cheap, and it turns the manifest from documentation into a contract.

**Storage notes:** `if_revision` gives optimistic concurrency for the crash-restart case (a restarted plugin racing its own leftover work). `namespace` is enforced against the manifest — this is §3.1's *"Then the plugins dont make up their own thing"* made mechanical.

**Deliberately absent from the host API:**
- **No `Log` RPC.** go-plugin already streams the plugin's stdout/stderr to the daemon via its internal `grpc_stdio.proto` *(inv-12)*. A plugin calls `fmt.Println`. Free. Don't rebuild it.
- **No `Subscribe` RPC.** Subscriptions are declared in the manifest, not called at runtime. That is what makes §3.4's reload cheap.
- **No SSH, no search, no notes, no comms.** Domain knowledge in the kernel is the failure mode the whole architecture exists to avoid. inv-13's SSH plugin needs *"exactly two named byte-slots and an event feed"* — and if a plugin that fiddly needs no more, the boundary is in the right place.

### 4.4 Registration, end to end

1. Kernel verifies sha256, pre-warms, `exec`s the binary.
2. Plugin prints **one line** on stdout and serves: `1|1|unix|/tmp/plugin2951246566|grpc|` *(inv-12/14 measured the format: `CORE|APIVERSION|network|address|protocol|cert`)*. That is the entire bootstrap.
3. Kernel dials, calls `Describe` → gets the `Manifest`.
4. Kernel **validates the manifest** (§6.2) and records it. Opens declared channels. Wires declared interests to the roster event feed. Issues a `plugin_token` scoped to the declared namespaces.
5. Kernel calls `Start(kernel_endpoint, plugin_token, node_name)`.
6. Plugin dials back, and runs.

Step 2 is osquery's model — *"During an extension's set up it will 'broadcast' all of its registered plugins"* — and step 4 is literally §3.1's *"the plugin would say what resources it was interested in/needed to react to. Maybe it needed changes in the node list."* **A production system arrived at Greg's registration design independently** *(inv-14)*.

---

## 5. Versioning

Greg: *"must be versioned and probably backwards (maybe forwards?) compatible."* All three answers below are measured, against the actual contract in §4.

### 5.1 Two levels, because they catch different failures

**Level 1 — the coarse handshake.** go-plugin's `HandshakeConfig.ProtocolVersion` + `VersionedPlugins map[int]PluginSet` *(inv-12, inv-14)*. The daemon offers a set of API versions; the plugin serves the highest it supports; a mismatch fails **at connect time with a readable error** instead of a weird decode failure three hours later. Protobuf's field rules will never give you this.

**Level 2 — protobuf field rules within a version.** Below.

### 5.2 What buf actually catches — measured on this contract

I ran `buf breaking` against a git baseline of the §4 contract. **[MEASURED — `/tmp/protoexp`, buf 1.71.0]**

| Change | `PACKAGE` | `WIRE_JSON` (buf's *recommended* level) |
|---|---|---|
| Add `maintainer = 9` to `Manifest` | ✅ **pass** (safe) | ✅ pass |
| Add a new `rpc PublishStream` (streaming) | ✅ **pass** (safe) | ✅ pass |
| Delete `rpc Drain` from `PluginService` | ❌ **`Previously present RPC "Drain" on service "PluginService" was deleted.`** | ⚠️ **PASS — not detected** |
| Renumber `Envelope.payload` 7 → 11 | ❌ `Previously present field "7" ... was deleted` | ❌ fail |
| Change `sent_unix_nanos` int64 → string | ❌ `changed type from "int64" to "string"` | ❌ fail |
| Delete `origin_agent` **with `reserved 9`** | ❌ **`Previously present field "9" ... was deleted`** | ✅ pass |

**Use `PACKAGE`, not buf's recommended `WIRE_JSON`.** This independently reproduces inv-14's Experiment 6 on a different schema: **WIRE_JSON silently allows deleting an entire RPC method**, because WIRE_JSON only cares about *encoding* and deleting a method breaks no encoding — callers just get "unimplemented" at runtime, on a box Greg isn't sitting at. For a plugin API **the service surface *is* the contract.**

I pinned the mechanism to buf's own rule table rather than inferring it **[MEASURED — `buf config ls-breaking-rules`]**:

```
RPC_NO_DELETE      FILE, PACKAGE            Checks that rpcs are not deleted from a given service.
FIELD_NO_DELETE    CSR, FILE, PACKAGE       Checks that fields are not deleted from a given message.
```

Neither is in `WIRE_JSON`. That is the whole explanation.

**One nuance inv-14 did not state, and it is a real cost:** at `PACKAGE`, **deleting a field is breaking even when you correctly `reserved` it** (row 5 — I verified the `reserved` really was in the file and that the file still built). `PACKAGE` protects *generated-code* compatibility, not just the wire. **So the contract is append-only: you never remove a field, an RPC, or an enum value. You deprecate.**

That is the right trade here — fields are cheap, and a v1 daemon may run a v2 plugin — but it should be a decision, not a surprise. Take it deliberately.

### 5.3 Is forward compatibility actually achievable? Yes for data. I measured it.

"Forward compatible" = an **old** daemon correctly handles a **new** plugin's messages, including fields it has never heard of.

**[MEASURED — `/tmp/fwdcompat`]** I generated two real schemas — a v1 `Manifest` and a v2 `Manifest` with two extra fields — and ran a v2 message through a v1 reader:

```
v2 plugin sent 29 bytes
old daemon parsed OK: name="comms" version="2.0.0" ns=[comms]
old daemon retained 8 bytes of UNKNOWN fields it cannot read

AFTER a round-trip THROUGH the v1 daemon:
  maintainer = "greg" (want "greg")
  priority   = 7 (want 7)
  >>> FORWARD COMPATIBILITY HOLDS: unknown fields survived a v1 daemon.
```

**Greg's parenthetical gets a measured yes.** The old daemon did not merely tolerate the unknown fields — it **carried them intact** through a full parse/re-serialize cycle, and a v2 reader downstream got them back byte-perfect.

**And the trap, also measured, because it is silent:**

```
--- the trap: same manifest, but the v1 daemon re-serializes via JSON ---
  v1 daemon JSON: {"name":"comms","version":"2.0.0","storageNamespaces":["comms"]}
  maintainer after JSON hop = "", priority = 0
  >>> DATA SILENTLY DESTROYED by the JSON hop.
```

**No error. No warning. The data is just gone.** protobuf's own guidance is *"Use binary; avoid using text formats for data exchange"* *(inv-14)*.

**Rule: plugin↔daemon and daemon↔daemon hops are binary protobuf, always.** JSON at the edge (an agent with `curl`) is fine and is a stated goal — just **never round-trip an envelope through it.** Specifically: a plugin must never `protojson.Marshal` an `Envelope` and pass it on. If a plugin needs to show a human an envelope, that is a terminal operation, not a hop.

### 5.4 So: how does a v1 daemon run a v2 plugin, and vice versa?

| Case | What happens |
|---|---|
| **v1 daemon, v2 plugin** (plugin upgraded first) | Handshake: plugin serves v1 if `VersionedPlugins` includes it → **runs fine**. New fields in its messages are unknown to the daemon and **preserved** (measured, §5.3). If the plugin dropped v1 → clean refusal at connect with a readable error. **No silent corruption either way.** |
| **v2 daemon, v1 plugin** (daemon upgraded first — the common case) | Handshake negotiates v1. The daemon must not *require* fields the v1 plugin never sets. **This is where the discipline lives** (§5.5). |
| **v2 daemon, v2 plugin, v1 messages already in storage** | Unknown-field preservation covers reads. Append-only schema (§5.2) covers the rest. |

**The dangerous case is v2-daemon/v1-plugin, and protobuf will not save you from it.** proto3 has no required fields, and a scalar field that was never set is indistinguishable from one deliberately set to zero. If the v2 daemon adds `int32 priority` and treats `0` as "urgent," every v1 plugin silently becomes urgent.

**The rule that fixes it: use `optional` on any field whose absence must be distinguishable from its zero value.** proto3 `optional` (stable since protobuf 3.15) generates a pointer/`Has` accessor in Go, so "unset" and "zero" are different things. It costs nothing. Use it whenever the answer to "is 0 meaningful?" is yes.

### 5.5 CI enforcement

```yaml
# buf.yaml — the version that matters
version: v2
modules:
  - path: proto
lint:
  use:
    - STANDARD
breaking:
  use:
    - PACKAGE      # NOT WIRE_JSON. Measured: WIRE_JSON misses a deleted RPC.
```

```bash
buf lint proto
buf breaking proto --against '.git#branch=main,subdir=proto'
```

**[MEASURED]** Both commands run green against the §4 contract today, on gb-mbp, with tooling already installed. Generated code is produced by `buf generate` with **no `protoc` anywhere** — I did it this session.

Three levels, covering three distinct failure modes:
1. **Coarse handshake** — catches "this plugin is from a different era," at connect, readably.
2. **protobuf field rules** — free forwards compatibility, *in binary only*.
3. **`buf breaking` at PACKAGE** — mechanically enforces #2 **and** protects the service surface, which #2 alone does not.

---

## 6. The manifest

### 6.1 Two manifests, and conflating them is a bug

This is a distinction the brief does not draw and the design needs:

| | **Runtime manifest** (§4.2 `Manifest`) | **Library manifest** (§7) |
|---|---|---|
| Who writes it | The plugin, in code, returned from `Describe` | The plugin *library*, as a record |
| What it says | What I am and what I want | Where my bytes are and what they hash to |
| Where it lives | In the plugin binary | In kernel storage, replicated across the fleet |
| Contains | channels, namespaces, interests, capabilities | name, version, platform, sha256, url |
| Trust | Asserted, then validated by the kernel | Verified against the bytes before exec |

**The runtime manifest is the plugin describing itself. The library manifest is the fleet describing where to get it.** Keeping them separate is what lets the plugin library ship a *pointer* while the kernel validates a *claim* (§7).

### 6.2 Kernel-side validation — the manifest is a contract, not documentation

The kernel **rejects** a plugin at `REGISTERED` if:

1. `api_version` is not in the daemon's `VersionedPlugins` set.
2. `name` is not a valid dotted namespace (`comms`, `roster.ssh`, `log.tail`). Caddy's namespaced module IDs *(inv-14)* — *"Namespaces are like classes"*.
3. It declares a channel `mode`/`type` combination the kernel doesn't implement.
4. It declares a `storage_namespace` owned by another plugin. **First registration wins; the loser is refused loudly.** (A shared namespace is a request for two plugins to corrupt each other.)
5. It declares `CAPABILITY_DELIVER` but subscribes to no channels, or subscribes to channels without declaring `CAPABILITY_DELIVER` — an incoherent manifest is a bug, and this catches it at load rather than at 3am.
6. `name` collides with a **running** plugin. (Reload is name-preserving by construction: same name, new binary.)

**A rejected manifest is not a daemon failure.** The plugin goes to `FAILED`, it is reported into the roster (*"wanted comms v3, not running"*), and everything else keeps working. §4 forbids one box's problem — let alone one plugin's problem — from becoming the fleet's problem.

### 6.3 Disk layout

```
~/.substrate/
├── config.toml                       # kernel: node name, seeds, tailscale IP
├── storage/                          # kernel-owned. Plugins reach it ONLY via KernelService.
│   └── <namespace>/…                 # namespace == manifest's storage_namespaces entry
├── plugins/
│   ├── available/
│   │   └── comms/
│   │       ├── 1.2.0/
│   │       │   ├── darwin-arm64/comms          # 2 platforms. MEASURED. Not 4.
│   │       │   └── linux-arm64/comms
│   │       └── 1.3.0/…
│   └── enabled/
│       └── comms -> ../available/comms/1.2.0/darwin-arm64/comms   # symlink == "what runs here"
└── library.jsonl                     # replicated manifest records (§7). Data, never bytes.
```

**`enabled/` is a symlink per plugin, and that is the whole "which version runs here" mechanism.** Reload = repoint the symlink, then run the state machine (§3.4). Rollback = repoint it back — and the old version's bytes are still on disk, already verified, **already pre-warmed**, so rollback is ~4–9 ms. That property is worth the directory layout on its own.

---

## 7. Failure

### 7.1 Blast radius

| Failure | Blast radius | Basis |
|---|---|---|
| **Plugin panics** | **The plugin only.** The daemon gets an ordinary Go `error` (`Unavailable`). Other plugins never notice. | [MEASURED — inv-12; I saw the identical error shape this session] |
| **Plugin hangs** | **The plugin, plus whatever queued behind it.** The kernel's per-call deadline fires. | [MEASURED — my drain gate times out and falls through to Kill] |
| **Plugin corrupts its own memory** | **The plugin only.** Separate address space, MMU-enforced. | Architectural |
| **Plugin leaks memory** | ⚠️ **THE BOX.** See below. | [DEDUCED] |
| **Plugin spins the CPU** | ⚠️ **The box, degraded.** The OS scheduler bounds it; other work slows. | [DEDUCED] |
| **Plugin writes garbage to its storage namespace** | **The plugin's own data.** Namespace enforced by token. | Design |
| **Plugin fails to verify (bad sha256)** | **Nothing. It never runs.** | [MEASURED — §7.3] |
| **Daemon dies** | **That box's plugins.** Other boxes unaffected — §4 satisfied. | Architectural |

**The honest one is the leak, and I want to state it loudly rather than bury it: process isolation does not bound memory.** A plugin that leaks will eventually OOM the **box**, and on the dgx that box is also running vLLM. Crash isolation protects the *daemon*; it does not protect the *machine*. This is the one failure mode that escapes the recommendation's central virtue, and nobody in the source material says so.

Mitigation, honestly ranked:
1. **Observe.** The kernel spawned the process; it knows the PID; sampling RSS is trivial. Report it in the roster. **Do this.**
2. **Restart on threshold.** A knob, later, when a real plugin has a real leak. **Don't build it day one.**
3. **Enforce with `RLIMIT_AS`** via `exec.Cmd`'s `SysProcAttr` — go-plugin lets you supply the `exec.Cmd`. Cross-platform semantics differ between Linux and macOS. **[DEDUCED — I did not test this.]** Listed so it is known to exist; not recommended yet.

### 7.2 Hangs

go-plugin gives you nothing here. The kernel must:

- Put a **deadline on every kernel→plugin call.** Default 30 s for `Deliver`/`Notify`; overridable per channel. A plugin that blows it returns `DeadlineExceeded` and the message is **nacked, not lost** (§4.2 — the RPC return is the ack).
- **Count consecutive deadline overruns.** N in a row (suggest 3) → treat as crashed → drain (which will time out) → kill → restart. A hung plugin and a dead plugin need the same response; only the detection differs.

### 7.3 Restart policy — steal Telegraf's one knob

Telegraf's `execd` has exactly one: `restart_delay` (default `10s`), and it has been fine for years *(inv-12)*. **Do not build a supervision-tree DSL.**

```
crash → backoff 100ms, 200ms, 400ms … capped at 30s
      → 5 crashes in 60s → FAILED (terminal)
FAILED → daemon stays up
       → reported into the roster: "comms v1.2.0 FAILED, 5 crashes"
       → cleared by CLI: `substrate plugin restart comms`
```

**Why terminal rather than retry-forever:** a crash-looping plugin retrying forever burns the box and floods the logs, and §4's *"I dont trust my boxes"* means the failure must be *visible*, not *absorbed*. A plugin that is off and reported is a fact Greg can act on. A plugin that restarts every 30 s forever is a mystery.

**The `Canceled` vs `Unavailable` distinction (§3.3) is what makes the crash budget correct** — a deliberate reload must not count as a crash, and the kernel can tell the difference for free.

### 7.4 Can a plugin take the fleet down?

**No, and this is the §4 check.** A plugin is local to its daemon. go-plugin's own README scopes it: *"only designed to work over a local [reliable] network"* — which is a match, not a limitation, because cross-machine traffic goes over channels (kernel), not over go-plugin.

The worst case is: **a comms plugin crash-loops on gb-mbp → gb-mbp cannot send agent messages → dgx and gb-mac-mini are unaffected and keep talking.** That is precisely §4: no single box's death kills the system. A box's *plugin's* death kills strictly less than the box's death, which is already permitted.

**One caveat I will not paper over:** if a plugin is broken *by a manifest that replicated across the fleet* (§7), it can crash-loop on **all three boxes at once**. That is not a hub, but it is a **correlated failure**, and it is the plugin library's specific risk. §8 addresses it directly.

---

## 8. The plugin-library plugin

> §3.2 **[Idea]**: *"the first plugin is a plugin library. A plugin gets added to one node, the plugin gets synced across nodes."*

**Verdict: BUILD IT — with one word changed, and not first.**

**The word: sync the *manifest*, never the *bytes*.**

**The ordering: it cannot be the first plugin. It must be roughly the last.**

Both halves are load-bearing. Taking them in turn.

### 8.1 Why "sync the manifest, not the bytes"

As literally stated — *"the plugin gets synced across nodes"* — it is a well-documented trap. inv-14 found two independent giants that converged on the same fix:

- **Istio/Envoy** deliberately separate code distribution from config distribution: *"istio-agent intercepts the extension config resource update from istiod, reads the remote fetch hint from it, downloads the Wasm module, and rewrites the ECDS configuration with the path of the downloaded Wasm module"* — **the control plane ships a pointer; a local agent fetches the bytes and verifies.** Their hard-won rule: *"It is highly recommended to provide the checksum, since missing checksum will cause the Wasm module to be downloaded repeatedly."*
- **Nomad** converged identically: `go-getter` + `checksum = "md5:..."`, and *"if the checksum is invalid, an error will be returned."*
- **Traefik is the counterexample** that shows the failure mode: remote plugins auto-download and *"the archive hash is **optionally** checked."* Optionally. **[all CLAIMED — inv-14]**

And the decisive local fact, now measured rather than assumed:

**The fleet is heterogeneous. [MEASURED — this session]** `gb-mbp` = `Darwin arm64`, `gb-mac-mini` = `Darwin arm64`, `dgx` = `Linux aarch64`. **A darwin binary synced to the dgx is not a plugin, it is garbage.** Any bytes-sync is platform-keyed from day one, which means the unit of replication is a *record*, not a *blob*.

(The good news from that same measurement: it is **two** artifacts, not four. `gb-mac-mini` was unreachable for both sibling investigations — `Host key verification failed`, which is BRIEF §4's stated problem happening live to the research itself. It answers to `-o StrictHostKeyChecking=accept-new`. So the platform matrix, an open question in inv-12 *and* inv-14, is now closed.)

**The record:**

```jsonc
// library.jsonl — one line per plugin build. Replicated. Small. Ordered. Gossip-friendly.
{
  "name":     "comms",
  "version":  "1.3.0",
  "platform": "linux-arm64",              // REQUIRED. The fleet is 2 platforms. Measured.
  "sha256":   "9f2b...c41e",              // REQUIRED. Not optional. Traefik's mistake.
  "url":      "http://100.115.27.55:7947/plugins/comms/1.3.0/linux-arm64",
  "size":     18261874,
  "added_by": "dgx",
  "added_at": "2026-07-16T00:14:02Z"
}
```

**The flow:** a plugin is built on one box → its **record** is written to kernel storage and replicates like any other resource (`RESOURCE_KIND_PLUGIN_MANIFESTS`, already in the §4.2 contract) → every other box sees the record → each box **fetches its own platform's bytes itself**, over plain HTTP from whichever box has them → **verifies the sha256** → **pre-warms** (§2.1) → repoints the symlink → reloads.

Greg gets his *"add it to one node and it spreads"* experience. There is **no central artifact server and no hub whose loss is fatal** — the origin is just another daemon serving a file. If the origin box is asleep, the fetch fails, and that is a **loud, local, non-fatal** event: the box reports *"comms v1.3.0 wanted, not running"* into the roster and keeps running what it has.

### 8.2 The checksum gate is already written, and I verified it works

`SecureConfig` (`client.go:327-340`) does SHA256 verification of the plugin binary **before exec**. I did not take that on faith. **[MEASURED — `/tmp/drainexp`]**

```
=== TEST 5: SecureConfig SHA256 gate (the plugin-library linchpin) ===
  real sha256 of kv-plugin: 83a15a8067a8b59c
  correct checksum   -> LAUNCHED, got "fast [pid 67961]" err=<nil>
  tampered checksum  -> REFUSED TO LAUNCH: checksums did not match

=== TEST 6: what if the binary changes AFTER the manifest was published? ===
  binary mutated on disk, manifest sha unchanged -> REFUSED TO LAUNCH: checksums did not match
```

Both directions covered: a wrong record and a mutated binary. **The mechanism the plugin library needs most is already in the library we picked, it is ~6 lines to use, and it refuses rather than warns.** That is Istio's rule and Nomad's rule, enforced by default, in the tool we already chose for other reasons.

### 8.3 The ordering problem — "the recovery path is the thing you broke"

This is the sharpest objection to the idea and it deserves a direct answer rather than a mitigation list.

**A self-distributing plugin system has a bootstrap problem and a recovery problem, and they are the same problem.** If the plugin library is a plugin:

- **Bootstrap:** how does the plugin library arrive on a box? Not via itself.
- **Recovery:** if you ship a broken plugin-library plugin, it replicates its own brokenness to all three boxes, and **the tool you would use to fix it is the tool that is broken.** §7.4's correlated-failure caveat, in its worst form.

**The answer, and it is not subtle: the ground truth is the filesystem and a CLI. The plugin library is a convenience layer over it, and never the only path.**

1. **`substrate plugin install <path-to-binary>`** is a **kernel CLI command**, not a plugin. It verifies, pre-warms, writes the record to local storage, repoints the symlink, reloads. **It works with the plugin library dead, absent, or never written.** This is the recovery path, it costs ~50 lines, and it is the *first* thing built — before any plugin at all.
2. **The plugin library plugin only ever does two things:** watch `RESOURCE_KIND_PLUGIN_MANIFESTS` for records this box hasn't got, and call `install` on them. It is a **fetch-and-verify loop, nothing more.** It has no unique power. Everything it does, Greg can do by hand with the CLI in one command.
3. **Therefore its failure mode is: propagation stops.** Every box keeps running what it has. Nothing crashes, nothing is lost, and the fix is a CLI command on one box. **You lose convenience, never operation.**
4. **A plugin may not delete or downgrade another plugin's enabled version without a human.** Replication *adds* records and *offers* versions. Repointing `enabled/` on a box is either a local CLI action or an explicit policy. **Auto-upgrade-everything is exactly how one bad build takes the fleet**, and it is the one power the library must not have. (This is a design judgement, and I hold it firmly: it is the same line inv-13 draws for the SSH plugin — the daemon does the tedious part, the human keeps the decision that can hurt.)

**With those four properties, "the recovery path is the thing you broke" stops being true**: the recovery path is a CLI and a symlink, and neither depends on the plugin library.

### 8.4 So why not first?

Greg's instinct — *"the first plugin is a plugin library"* — has the right shape and the wrong order, and the reason is simple: **you cannot distribute plugins before you can run plugins.** Everything the library needs is downstream of things that must exist first:

- Kernel storage (§4.3) — the records live there.
- The roster (§3.1) — the records replicate on it.
- Resource-interest notification (`RESOURCE_KIND_PLUGIN_MANIFESTS`) — how it learns.
- `SecureConfig` verification + pre-warm + the symlink layout (§6.3).
- **The `install` CLI** — which, once built, makes the library *optional*.

**Build order:**

| Phase | What | Why here |
|---|---|---|
| **1** | Kernel: channels, storage, roster, plugin host, drain gate, `install` CLI | Nothing works without it. `install` is the recovery path — it exists before anything can break. |
| **2** | **One real plugin** (comms — Greg's stated first-class need, §2) | Proves the contract on the plugin that matters most. Two platforms, installed by hand: **two `scp`s.** |
| **3** | 2–3 more plugins (log tail, agent registry) | Now the contract has been evolved once, under `buf breaking`. Versioning is real, not theoretical. |
| **4** | **The plugin library** | Now it automates a chore that is *demonstrably* a chore, against a contract that has already survived change. |

**The test for phase 4 is empirical, and Greg should apply it himself: is `scp`-ing two binaries actually annoying yet?** With 6 plugins × 2 platforms and one operator, it may simply not be. If it is not, the library is a solution looking for a problem — and it is the highest-risk plugin in the set, because it is the only one that can hurt all three boxes at once. Build it when the chore is real. It is an **[Idea]** in the brief, not a requirement, and it should be held to that standard.

---

## 9. What we do NOT build

Explicitly, so it does not creep back in:

| Not built | Why |
|---|---|
| **AutoMTLS between daemon and plugin** | §4: trusted network, three boxes, all his. Encrypting loopback against a threat model of "my own buggy plugin" is absurd. go-plugin has it; leave it off. |
| **Plugin sandboxing / capability ACLs** | Same. The `plugin_token` enforces the *manifest* (namespaces, channels) to catch **bugs**, not attackers. It is a correctness feature, not a security feature. Say so, so nobody mistakes it. |
| **A WASM runtime** | Second place, genuinely. Revisit against the **same `.proto`** if the two-artifact story ever hurts. Traefik runs both. Not now. |
| **Yaegi / any Go interpreter** | A plugin goroutine panic kills the daemon. Unfixable. |
| **Go's stdlib `plugin` package** | Cannot reload at all. |
| **`code_change/3` / state migration across reload** | **Impossible in Go.** Not hard — impossible. Do not design toward it. Put state in the kernel instead. |
| **A supervision-tree DSL** | Telegraf has one knob and it's been fine for years. One knob. |
| **`GRPCBroker` for the host API** | It would create a second, private, plugin-only host API. One surface (§4.3). |
| **A `Log` RPC** | go-plugin already streams stdout/stderr. `fmt.Println` is the API. |
| **A `Subscribe` RPC** | Subscriptions are manifest declarations. That is what makes reload cheap (§3.1). |
| **Plugin-to-plugin addressing** | Route by channel name. Fluent Bit's model — the only one that survives a machine boundary *(inv-14)*. |
| **Kernel live-reload** | Not achievable here. Keep the kernel small instead. `ReattachConfig` exists if it ever becomes urgent. |
| **Byte-sync of plugin binaries** | Sync records; fetch and verify locally (§8). |
| **Auto-upgrade of enabled plugin versions across the fleet** | The one power that could take all three boxes at once (§8.3). |
| **Raft / consensus for the plugin manifest** | §6 disclaims CAP rigor; 2-of-3 quorum means losing two boxes stops it dead *(inv-14)*. Records are add-only and eventually consistent. Write it down, move on. |
| **Overlap detection between plugins/agents** | §6: *"I really dont even care about that."* Not now, not disguised as anything else. |
| **In-plugin routing DSL (Starlark)** | Right tool, wrong phase. Keep it in the back pocket for a user-editable filter language *inside* a plugin *(inv-12)*. |

---

## 10. Open questions

Things I could not determine, stated rather than guessed.

1. **Does "fanout" (§3.1) mean *everyone gets a copy* or *exactly one worker gets it*?** The brief's menu — *"publish/lookup table, point to point, pubsub, fanout"* — lists pubsub and fanout **separately**, which implies fanout means the work-queue sense; but conventionally "fanout" means broadcast. The zeromq investigation flagged this independently. **It does not change this contract** (it is one enum value in `ChannelType`, and the kernel implements the semantics), which is a useful de-risking fact — but it changes the kernel. **Worth 30 seconds of asking Greg.**
2. **Does `RLIMIT_AS` via `exec.Cmd.SysProcAttr` bound a leaking plugin on both darwin and linux?** [DEDUCED that it exists; not tested.] This is the one failure mode that escapes process isolation (§7.1) and the dgx also runs vLLM. Worth one afternoon before a plugin leaks for real.
3. **Does the 2-second graceful window in `Kill()` (`client.go:554-560`) ever matter once the kernel drains first?** My drain gate makes the plugin idle before `Kill()`, so it should always take the fast path — my idle kills were **6–8 ms**, well under 2 s. But I did not test a plugin that **ignores** the shutdown RPC and refuses to exit. That path force-kills after 2 s, which would make a pathological reload ~2 s instead of ~10 ms. Bounded, known, untested.
4. **How many plugins actually need `CAPABILITY_DRAIN`?** I claim five of six named plugins hold no state (§3.5). That is reasoning from their descriptions, not from written plugins. If it turns out most need `Drain`, the "plugins are stateless" rule is weaker than advertised and §3.5 needs revisiting.
5. **What is the right `Deliver` deadline?** I suggest 30 s by analogy, with no measurement behind it. It should be set from a real plugin's real latency distribution, per channel. Nothing here measures a real workload.
6. **Does the pre-warm trick survive a macOS update or a quarantine flag?** **[MEASURED]** it works for a locally-built binary today. A binary **fetched over HTTP by the plugin library** (§8) may carry a quarantine attribute (`com.apple.quarantine`) that Gatekeeper treats differently — possibly worse than 500 ms, possibly a hard refusal. **This is the most important untested thing in §8** and it is cheap to check: fetch a plugin over HTTP on gb-mbp and exec it.
7. **Bidirectional streaming from plugin to kernel.** A log-tail plugin publishing thousands of lines/sec via unary `Publish` calls pays 45 µs each *(inv-12)*. That is fine at this scale, and gRPC streaming would fix it if it ever isn't. I did not design a streaming `Publish`. The `.proto` can add one without breaking anything (adding an RPC is safe at PACKAGE — measured).
8. **Does `buf breaking` at PACKAGE become annoying enough to erode?** The append-only rule (§5.2) is right, but I have not lived with it. If it turns out to block reasonable cleanups, the fallback is PACKAGE for `plugin.proto`/`kernel.proto` (the contract) and WIRE_JSON for internal messages — a split I have not tested.
9. **`gb-mac-mini`'s SSH host key was unknown to this session** until I accepted it with `-o StrictHostKeyChecking=accept-new`. Two prior investigations were blocked by this. It is inv-13's problem, not mine, but it is worth noting that **the address-book problem in §4 blocked the research into it, twice** — which is about as direct a confirmation of the brief's premise as one could ask for.

---

## 11. Dependencies on the kernel design (design/20) — stated to prevent drift

This design assumes the kernel provides, and would need rework if it does not:

1. **Channels are kernel-owned and named.** A plugin's subscription is a kernel-side fact recorded from its manifest — **not** a connection the plugin holds. **This is what makes reload cheap (§3.1) and it is the single largest dependency.**
2. **Kernel storage exists, is namespaced, and enforces namespaces per plugin token.** §3.1 asks for it. §3.5 shows live reload is **impossible to make safe without it.** These are the same requirement.
3. **The kernel queues messages for a plugin that is briefly not dispatching** (bounded; overflow is a nack, not a silent drop — §4 makes durability the plugin's choice, so the kernel must *report* overflow, not *solve* it).
4. **The roster emits change events** the kernel can turn into `Notify` calls (`RESOURCE_KIND_ROSTER`). inv-13 confirms memberlist's `EventDelegate` hands this over directly.
5. **`KernelService` is served over a unix socket for plugins AND over HTTP for agents — the same surface** (§4.3). inv-11 measured ConnectRPC doing exactly this from one `.proto`.
6. **The kernel owns the drain gate** (§3.3) — because go-plugin does not (measured).
7. **Envelope ordering is NOT provided**, and no plugin may assume `sent_unix_nanos` orders anything across boxes.

**Where a sibling conflict exists and I did not resolve it:** inv-11 recommends embedded core NATS (full mesh, JetStream off) for channel transport; inv-10 recommends mangos/NNG. **This design is transport-agnostic and does not vote.** go-plugin is strictly daemon↔plugin, local, over a unix socket; whichever wins governs daemon↔daemon channel data, which this contract touches only through `Envelope` and `KernelService.Publish`. Both candidates satisfy this contract. **That scoping should be checked by whoever reconciles them** — it is inv-14's open question #7 and it is still open.

---

## Sources

**Project files read:**
- `/Users/gb/research/2026-07-15-agent-substrate-v2/BRIEF.md` (in full)
- `/Users/gb/research/2026-07-15-agent-substrate-v2/investigate/12-plugins-live-reload.md` (in full, 733 lines)
- `/Users/gb/research/2026-07-15-agent-substrate-v2/investigate/14-prior-art.md` (in full, 604 lines)
- `/Users/gb/research/2026-07-15-agent-substrate-v2/investigate/11-transport-options.md` (§0–2 headline, §4 RPC layers, §5 middle paths, §6 table, §7 recommendation, §8 open questions)
- `/Users/gb/research/2026-07-15-agent-substrate-v2/investigate/13-roster-and-addressbook.md` (§6 node entry schema, §7 dead-box routing, §8.3 what core must expose, §9–11)

**Library source read (`~/go/pkg/mod/github.com/hashicorp/go-plugin@v1.8.0/`):**
- `client.go:498-572` — `Kill()`: `client.Close()`, 2 s graceful wait on `doneCtx`, then `runner.Kill()`
- `grpc_client.go:105-109` — `GRPCClient.Close()` → `broker.Close()` + `controller.Shutdown()` + `Conn.Close()`
- `grpc_controller.go` (whole file) — **`Shutdown` calls `s.server.Stop()` with `// TODO: figure out why GracefullStop doesn't work`, while its own doc comment claims it stops gracefully first**
- `grpc_server.go:118-135` — `Stop()` vs `GracefulStop()`; `GracefulStop` exists and is unused on the shutdown path
- `grpc_broker.go:394-398` — the library's only `GracefulStop()` call site (unrelated path)

**Experiments written and run this session (code on disk):**
- `/tmp/drainexp/` — go-plugin v1.8.0 + grpc v1.82.1 + protobuf v1.36.11, unary + server-streaming:
  - `host/main.go` — Test 1: `Kill()` with an in-flight 3 s RPC → **cancelled at 504 ms**, `Unavailable ... EOF`. Test 2: idle `Kill()` → **6–8 ms**. Test 3: active subscription during `Kill()` → `Unavailable`, subscriber must re-subscribe. Test 4: **kernel drain gate** → same in-flight call **completed**, streams cut with `Canceled`, mid-drain calls rejected cleanly. Test 5/6: `SecureConfig` sha256 → **refused to launch** on a tampered checksum and on a mutated binary. Reload-latency harness (cross-compiled and run on dgx).
  - `kvplugin/main.go` — plugin with a deliberately slow call and a never-ending stream
  - `prewarm/main.go` — macOS first-exec pre-warm: **385 ms discarded → real reload 9 ms** (was 505 ms)
- `/tmp/protoexp/` — the §4 contract as real `.proto`: `buf lint` (STANDARD) **pass** after fixing buf's `DescribeResponse` finding; `buf build` **pass**; `buf breaking` vs a git baseline at **PACKAGE** and **WIRE_JSON** across six change classes (add field / add streaming RPC / delete RPC / renumber / retype / delete-with-reserved)
- `/tmp/fwdcompat/` — two real generated schemas (v1 and v2 `Manifest`): forward compatibility **holds** in binary (8 unknown bytes preserved through a v1 daemon; v2 reader recovered `maintainer`/`priority`); the **JSON hop silently destroys them**

**Commands run:**
- `go version` → `go1.26.2 darwin/arm64`
- `uname -srm` on all three boxes → gb-mbp `Darwin 25.4.0 arm64`; dgx `Linux 6.17.0-1026-nvidia aarch64`; **gb-mac-mini `Darwin 25.3.0 arm64`** (via `ssh -o StrictHostKeyChecking=accept-new gb@100.120.22.74`) — **closes the open platform question in inv-12 and inv-14**
- `GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build` → static aarch64 ELF, 17.9 MB host / 17.6 MB plugin; `scp` to `dgx:/tmp/reloadtest/`; executed there
- `buf --version` → **1.71.0**; `protoc-gen-go --version` → **v1.36.11**; `go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest` (the one thing missing)
- `buf generate` / `buf lint` / `buf build` / `buf breaking` / `buf config ls-breaking-rules --version v2` → pinned `RPC_NO_DELETE` (FILE, PACKAGE) and `FIELD_NO_DELETE` (CSR, FILE, PACKAGE) — **neither in WIRE_JSON**
- `which buf protoc protoc-gen-go grpcurl` + `ls ~/go/bin` + `echo $PATH` → **buf, grpcurl, protoc-gen-go, protoc-gen-connect-go are all installed but `~/go/bin` is not on `PATH`** — corrects inv-12's "protoc is not installed on gb-mbp" toolchain-gap claim (`protoc` genuinely isn't; it also isn't needed)

**URLs fetched:** none this session. All external claims are re-cited from `investigate/12` and `investigate/14`, which fetched and labelled them, and are marked *(inv-12)* / *(inv-14)* at the point of use.
