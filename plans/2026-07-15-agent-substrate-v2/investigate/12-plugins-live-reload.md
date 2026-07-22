# Plugin systems and live reload in Go — the option space

**Investigation date:** 2026-07-15
**Brief:** `/Users/gb/research/2026-07-15-agent-substrate-v2/BRIEF.md` §3.2, §7.2
**All benchmarks:** measured on `gb-mbp`, darwin/arm64, Go 1.26.2, this session. Every number below labelled **[MEASURED]** came out of a program I wrote and ran today; the code is in `/tmp/pluginexp`, `/tmp/gpexp`, `/tmp/wzexp`, `/tmp/pbwasm`, `/tmp/yaexp`, `/tmp/starexp` and is reproduced inline where it matters.

---

## 0. The answer in one paragraph

**Use `hashicorp/go-plugin`.** Each plugin is its own ordinary Go program, compiled to its own binary. The daemon launches it as a child process and talks to it over gRPC — which is protobuf, over a unix socket. "Live reload" means: kill the child, start the new binary, the daemon never stops. I measured that at **12 ms** when the binary is already on disk and warm, ~450 ms the first time a freshly-built binary is executed on macOS. A plugin that panics does not take the daemon down — I crashed one on purpose and the daemon kept running and reloaded it. The interface is protobuf-defined in a `.proto` file, completely independent of any code, exactly as §3.2 asks — and go-plugin's own internals are already three protobuf services, so this is not a bolt-on. HashiCorp Vault does precisely this in production today: `POST /sys/plugins/reload/backend` kills the running plugin processes and starts the new ones without restarting Vault. The honest cost: **it is not Erlang's BEAM and nothing in Go is.** You get BEAM's *supervision and isolation* half. You do not get BEAM's *hot code loading* half — no `code_change`, no two live versions, no state carried across the swap. Everything in a reloaded plugin's memory is gone. The way to buy that back is the thing you already asked for in §3.1: *"a storage mechanism in the daemon. Then the plugins dont make up their own thing."* Plugin state lives in the kernel; the plugin is a pure function of it. That is exactly what Vault does, and it is not a coincidence.

---

## 1. Vocabulary (defining everything, per §8)

You said you're not deep in this space, so here's every term this document uses, up front.

| Term | What it means |
|---|---|
| **protobuf** (Protocol Buffers) | A file format for describing messages and function signatures — `.proto` files. A code generator turns one `.proto` into Go structs, Python classes, Rust structs, etc. The wire format is compact binary. This is the "define it COMPLETELY independently of any code" property you want in §3.2. |
| **gRPC** | Google's RPC (remote procedure call) system built on protobuf. You write `service KV { rpc Get(GetRequest) returns (GetResponse); }` in a `.proto`, and gRPC gives you a client and a server. It runs over HTTP/2 — which works fine over a **unix socket**, a file on disk two processes on the same machine use to talk. No network involved. |
| **IDL** (Interface Definition Language) | The general name for "a file that defines an interface independent of language". protobuf is an IDL. Thrift is another. |
| **shared library / .so** | A chunk of compiled machine code loaded into a running program's memory at runtime. `dlopen()` is the C function that does it. This is what Go's `plugin` package uses. |
| **address space** | The memory a process can see. Two threads in one process share one address space — a bad pointer in one corrupts the other. Two *processes* have separate address spaces — one can't touch the other's memory. This distinction is the whole ballgame for crash isolation. |
| **WASM / WebAssembly** | A portable binary instruction format. A `.wasm` file is compiled code that runs inside a sandbox — it can only touch its own memory and can only call functions the host explicitly hands it. Originally for browsers, now used server-side. |
| **wazero** | A WebAssembly runtime written in pure Go. No cgo, no C dependency. You embed it as a library. |
| **extism** | A framework on top of a WASM runtime (it uses wazero in Go) that adds a plugin convention: named functions that take bytes and return bytes, plus "host functions". |
| **cgo** | Go's mechanism for calling C code. It makes builds slower, breaks easy cross-compilation, and is required by Go's `plugin` package. |
| **BEAM** | The Erlang/Elixir virtual machine. The thing you have envy of. |
| **hot code loading** | Replacing code in a running process without stopping it, *while processes keep running and keep their state*. This is BEAM's trick. |
| **live reload** (as used here) | The weaker, achievable thing: swap the code, keep the *daemon* running. The plugin's own state may or may not survive. Almost every Go option gives you this; none give you the strong version. |
| **crash isolation** | A plugin blowing up doesn't kill the daemon. |
| **`rg`** | (You asked before.) `ripgrep`, a fast `grep`. Not used in this document. |

**One distinction to hold onto, because it explains most of the confusion in this space:**

- **Reloading *configuration*** — easy, everyone does it, `SIGHUP` or an API call.
- **Reloading *code*** — hard, and the subject of this document.

Caddy, for example, does the first perfectly and the second not at all. People often say "Caddy has hot reload" and mean the first.

---

## 2. The BEAM comparison, honestly

### 2.1 What Erlang actually gives you

From the official Erlang docs (https://www.erlang.org/doc/system/code_loading.html):

> "The code of a module can exist in two variants in a system: *current* and *old*."

> "If a third instance of the module is loaded, the code server removes (purges) the old code and any processes lingering in it are terminated."

> "Fully qualified function calls always refer to current code. Old code can still be evaluated because of processes lingering in the old code."

Unpacked, the four properties that make BEAM feel magic:

1. **Per-module versioning.** The unit of replacement is one module, not the program. You reload `comms.erl` and nothing else in the system notices.
2. **Two live versions at once.** Old code keeps running for processes already inside it. New calls go to the new code. A long-running loop finishes its current iteration in the old version and steps into the new one when it makes a fully-qualified call `m:loop()`. **Nothing is interrupted mid-flight.**
3. **`code_change/3` — state migration.** From https://www.erlang.org/doc/system/release_handling.html: the release handler "causes behaviour processes to call the callback function `code_change/3`". This is the callback where you transform your old in-memory state into the new version's shape. *This is the part nobody outside Erlang has.* It is why BEAM can hot-reload a stateful server: it hands you your old state and asks you to convert it.
4. **Supervision trees.** Erlang processes are not OS processes — they're VM-scheduled green threads, each with its own private heap, each isolated from the others by the VM. A crash kills exactly one, and a supervisor restarts it per a declared strategy. This is a *language-runtime* guarantee, not an OS one, which is why it costs ~300 bytes per process instead of ~16 MB.

And the honest fine print on the Erlang side, which BEAM fans skip: the OTP release-handling doc's own advice is

> "It is thus recommended that code is changed in as small steps as possible, and always kept backwards compatible."

Real hot upgrades in Erlang require `.appup` and `.relup` files, an embedded-mode boot with `heart` monitoring, and hand-written `code_change/3` for every stateful process. **Most production Elixir shops do not use hot code loading.** They do rolling restarts, like everyone else. BEAM's hot loading is real and it is genuinely better than anything else — and it is also a specialist tool that costs real discipline. Worth knowing before importing the envy wholesale.

### 2.2 Why Go cannot do this — specifically

Not hand-waving. Four concrete reasons, in order of how fatal they are.

**(a) The runtime has no concept of removing code.** Go's runtime tracks loaded code in a linked list of `moduledata` structs. `/opt/homebrew/Cellar/go/1.26.2/libexec/src/runtime/symtab.go:544-558` — `modulesinit()` walks `firstmoduledata.next` and *appends* to the active module list. There is no corresponding remove. There is no `dlclose` anywhere in the plugin path — I grepped: the only `dlclose` in the entire Go source tree is in `runtime/cgo/gcc_android.c:61`, unrelated. **[MEASURED]** Loading is a one-way door by construction.

**(b) The runtime actively forbids loading a second version of the same module.** `/opt/homebrew/Cellar/go/1.26.2/libexec/src/runtime/plugin.go:33-36`:

```go
for _, pmd := range activeModules() {
    if pmd.pluginpath == md.pluginpath {
        md.bad = true
        return "", nil, nil, "plugin already loaded"
    }
```

This is the exact opposite of BEAM's "two variants, current and old". Go's runtime refuses on principle. **[MEASURED]** — I hit this error in a real program, see §4.

**(c) One address space, one garbage collector, no process isolation.** Erlang's per-process heap means the VM can kill one process and reclaim its memory precisely. Go has one shared heap and one GC for the whole program. If plugin code is in your address space, its pointers are your pointers. A plugin's bad write corrupts your daemon's memory. A plugin's panic on a goroutine you don't own kills your daemon — there is no `recover()` from another goroutine's panic. **[MEASURED]** in §7.1, where I did exactly this and the host exited with code 2. And even if Go added `plugin.Close()`, it would have to prove no goroutine, no pointer, no interface table, no `defer` frame, and no in-flight GC scan anywhere in the process still references the unloaded code. That proof is not tractable in a language with unrestricted pointers and no ownership tracking. It's why golang/go#20461 ("plugin: add support for closing plugins") has been open since **2017-05-22** with **70 comments** and milestone **"Unplanned"** **[MEASURED via GitHub API, 2026-07-15]**. Nine years. This is not going to be fixed.

**(d) Static linking and whole-program compilation.** Go's compiler and linker assume one build unit. Types are identified by pointers to type descriptors baked in at link time. Two independently-compiled copies of `pluginexp/shared.Msg` are *different types* to the runtime even if the source is identical — which is why Go's runtime hashes every shared package and refuses a mismatch (`/opt/homebrew/Cellar/go/1.26.2/libexec/src/runtime/plugin.go:54-59`). BEAM doesn't have this problem because it's dynamically typed and everything is a tagged term.

### 2.3 Which option gets closest, and the honest gap

Here is the uncomfortable finding, and it's the most interesting thing in this document.

**Two different Go options each get half of BEAM, and they're different halves. Nothing gets both.**

| BEAM property | Best Go option for it | Verdict |
|---|---|---|
| Per-module versioning | **Yaegi** (Go interpreter) | ✅ Real. Reload one interpreted package. |
| Two live versions at once | **Yaegi** | ✅ **Real — I verified it.** [MEASURED, §7] |
| `code_change/3` state migration | *nothing* | ❌ Nobody has this. |
| Supervision + crash isolation | **go-plugin** (OS processes) | ✅ Real. [MEASURED, §5] |
| Cheap isolated units (~300 bytes) | *nothing* | ❌ OS processes cost ~16 MB. [MEASURED] |

Yaegi gives you BEAM's code-loading half and **none** of the isolation half — a plugin panic on its own goroutine hard-kills the daemon [MEASURED, §7.1]. go-plugin gives you BEAM's isolation-and-supervision half and **none** of the code-loading half — every reload starts from zero state.

**So the honest gap is: you must choose which half of BEAM you want, and you cannot have `code_change` at all.**

The verdict is to take the **isolation half** (go-plugin), because:

1. The isolation half is the half that fails *safely*. If Yaegi's half fails, your daemon dies. If go-plugin's half fails, you lose in-memory state you shouldn't have had anyway.
2. The missing piece — state surviving a reload — has a design workaround that BEAM's missing piece doesn't. **Put the state in the kernel.** Brief §3.1 already asks for this: *"One thing that might be useful is to have a storage mechanism in the daemon. Then the plugins dont make up their own thing."* If the plugin is stateless and the kernel owns storage, "state lost on reload" stops being a loss. There's nothing to lose.
3. Yaegi's half is, in 2026, not actually deliverable. See §7.

**The reframe worth internalizing:** BEAM's magic is *hot code loading + supervision*. Go can give you *stateless plugins + supervision*, which reaches the same operational outcome — never stop the daemon — by a different route: instead of migrating state through a code swap, don't have state to migrate. This is exactly the trade Vault made, and Vault reloads plugins in production all day.

---

## 3. The candidates

Five families:

1. **Go's stdlib `plugin` package** — `.so` files loaded into the daemon's memory.
2. **Subprocess + IDL RPC** — plugin is its own program, talk over a socket. `hashicorp/go-plugin` is the framework; you can also roll your own.
3. **WASM** — plugin is a `.wasm` module running sandboxed inside the daemon.
4. **Embedded interpreters** — plugin is source code the daemon interprets. Yaegi (Go), Starlark, Lua.
5. **Compile-time modules** — plugin is a Go package linked into the daemon at build time. (Caddy's model. Included for completeness; it has no live reload by definition, so it's out.)

---

## 4. Go's stdlib `plugin` package — dead on arrival

### 4.1 How it works

`go build -buildmode=plugin` produces a `.so`. `plugin.Open(path)` `dlopen()`s it and `plugin.Lookup(name)` `dlsym()`s a symbol out. `/opt/homebrew/Cellar/go/1.26.2/libexec/src/plugin/plugin_dlopen.go:18-23`:

```c
static uintptr_t pluginOpen(const char* path, char** err) {
    void* h = dlopen(path, RTLD_NOW|RTLD_GLOBAL);
```

Note `RTLD_GLOBAL` and no matching `dlclose`. The plugin's symbols go into the global namespace and stay forever.

### 4.2 The documented warning, verified in Go 1.26.2

`go doc plugin` on this machine, **Go 1.26.2** **[MEASURED]**:

> "When a plugin is first opened, the init functions of all packages not already part of the program are called. The main function is not run. **A plugin is only initialized once, and cannot be closed.**"

The platform list in current docs is **"Linux, FreeBSD, and macOS"** — and the build tag adds a condition the prose doesn't mention. `/opt/homebrew/Cellar/go/1.26.2/libexec/src/plugin/plugin_dlopen.go:5`:

```go
//go:build (linux && cgo) || (darwin && cgo) || (freebsd && cgo)
```

**cgo is mandatory.** No Windows (golang/go#19282, open since 2017-02-24, 93 comments, milestone "Unplanned" **[MEASURED via GitHub API]**).

The doc's own conclusion, verbatim:

> "For these reasons, many users decide that traditional interprocess communication (IPC) mechanisms such as sockets, pipes, remote procedure call (RPC), shared memory mappings, or file system operations may be more suitable despite the performance overheads."

The Go team is telling you to use go-plugin's model. In the stdlib docs. For the plugin package.

### 4.3 Is it viable for live reload at all? I tested. No.

**Experiment 1 — load two builds of the same plugin.** `/tmp/pluginexp`. Built `greeter_v1.so`, changed the source, built `greeter_v2.so`, opened both in one process:

```
Opening greeter_v1.so
[plugin] init ran, version = V1
  -> hello from V1 (call #1)
Opening greeter_v2.so
  OPEN ERROR: plugin.Open("greeter_v2"): plugin already loaded
```

**[MEASURED]** Different file, different bytes (verified by sha1), same import path → refused. This is the `runtime/plugin.go:33-36` check. **You cannot load a new build of a plugin into a running Go process. Full stop.** That single result ends the stdlib plugin as a live-reload mechanism.

**Experiment 2 — the workaround, and its price.** You *can* force a unique module identity per build:

```bash
go build -buildmode=plugin -gcflags="-p=gen7" -ldflags="-pluginpath=gen7" -o gen7.so ./greeter
```

**[MEASURED]** This works — v2 loads alongside v1. (Note: `-ldflags=-pluginpath` alone is *not* enough; the compiler bakes the old path into symbol names and `dlsym` fails. You need both flags. That took me three tries to discover, which tells you how well-trodden this path is.)

Then I built 20 uniquely-named versions and loaded them one after another in a single process, sampling real RSS from `ps`:

```
start:           RSS= 9504 KB
after  1 loads:  RSS=10528 KB
after  5 loads:  RSS=14320 KB
after 10 loads:  RSS=18944 KB
after 20 loads:  RSS=29968 KB
```

**[MEASURED] ~1 MB leaked per reload, monotonic, never reclaimed — for a hello-world plugin with one function.** No `dlclose`, no module removal, and every old version's init-time allocations are pinned by the module list forever. A real plugin (protobuf + your deps) is a 4–18 MB `.so`; scale accordingly. Reload a plugin 100 times during a day of development and your daemon has leaked hundreds of megabytes of dead code it can never free.

**Experiment 3 — the lockstep problem.** Built a plugin against a shared package, then changed **one string literal inside one function body** in that shared package and rebuilt *only the daemon*:

```
=== matched build ===
host sees shared-v1
-> plugin sees shared-v1

=== now change ONLY the shared package and rebuild ONLY the host ===
host sees shared-v2
OPEN ERROR: plugin.Open("dep_v1"): plugin was built with a different version of package pluginexp/shared
```

**[MEASURED]** Not an API change. Not a signature change. A string literal. Every previously-built plugin is now invalid. This is `runtime/plugin.go:54-59` comparing link-time and run-time package hashes. In practice: **the daemon and every plugin must be built together, from the same tree, by the same toolchain, at the same moment.** Which means there is no such thing as an independently-shipped plugin — which means there is no plugin system, just a slow way to link.

**Experiment 4 — the one that kills §3.2's plugin-library idea outright.** Your `[Idea]` is *"A plugin gets added to one node, the plugin gets synced across nodes."* Your fleet is `gb-mbp` (darwin/arm64) and `dgx` (linux/aarch64 — **[MEASURED]** `uname -srm` over ssh returned `Linux 6.17.0-1026-nvidia aarch64`) plus `gb-mac-mini`. So syncing a plugin means building for two platforms. From the mac:

```
$ GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -buildmode=plugin -o x.so ./greeter
-buildmode=plugin requires external (cgo) linking, but cgo is not enabled
```

**[MEASURED]** You cannot cross-compile a Go plugin without setting up a full cgo cross-toolchain (Linux headers, a cross linker, the works) on every box that builds plugins. Compare go-plugin, same machine, same moment:

```
$ time GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -o kv-plugin-linux-arm64 ./kvplugin
1.836 total
$ file kv-plugin-linux-arm64
ELF 64-bit LSB executable, ARM aarch64, statically linked
```

**[MEASURED] 1.8 seconds, no toolchain, static binary, done.**

**Verdict: the stdlib `plugin` package is unusable here.** Cannot reload the same plugin at all; the workaround leaks ~1 MB per reload; requires lockstep rebuild of the entire world for a one-character change; requires cgo; cannot cross-compile; no crash isolation (it's your address space); Go-only forever. It fails every requirement in §3.2 simultaneously. **The Go team's own docs recommend against it.**

---

## 5. hashicorp/go-plugin — the recommendation

**Latest: v1.8.0, released 2026-04-29. Repo last pushed 2026-07-06 (9 days ago). 6,042 stars. License MPL-2.0.** **[MEASURED via proxy.golang.org and GitHub API, 2026-07-15]**. Note the copyright header now reads `Copyright IBM Corp. 2016, 2025` — IBM acquired HashiCorp; the library stayed MPL-2.0 and stayed maintained.

> **The prior research round cut this tool. Its brief never mentioned live reload or protobuf, so the cut was made on grounds that don't apply here. I re-derived it from scratch and reached the opposite conclusion. Below is why, with measurements.**

### 5.1 The shape match

Read this against §3.2 side by side:

| You wrote | go-plugin |
|---|---|
| *"probably protobuf"* | gRPC transport = protobuf on the wire. Interface lives in a `.proto`. |
| *"define it COMPLETELY independently of any code"* | The `.proto` file **is** the artifact. Code is generated from it, on both sides, in any language. |
| *"the same interfaces could be available through REST/pubsub/whatever"* | Same `.proto` → grpc-gateway gives you REST; the message types serialize anywhere. The IDL outlives the transport. |
| *"must be versioned"* | `VersionedPlugins map[int]PluginSet` — the daemon offers a set of versions, the plugin picks the highest it supports. |
| *"backwards (maybe forwards?) compatible"* | protobuf's field-number rules give you both directions for free — unknown fields are preserved, not errors. |
| *"live-loadable"* | Kill the child, start the new binary. Daemon never stops. **[MEASURED: 12 ms]** |
| *"registration process with the daemon, and the plugin would say what resources it was interested in"* | The handshake + `Dispense` model is literally this. |

**This is not a bolt-on.** go-plugin's own internals are protobuf services — I found three `.proto` files in its `internal/plugin/` directory **[MEASURED]**:

```
./internal/plugin/grpc_stdio.proto        service GRPCStdio      (streams plugin stdout/stderr back to the daemon)
./internal/plugin/grpc_controller.proto   service GRPCController (orderly shutdown)
./internal/plugin/grpc_broker.proto       service GRPCBroker     (opens extra bidirectional connections)
```

It is protobuf all the way down.

### 5.2 How the handshake actually works (it's simpler than you'd guess)

The plugin process prints **one line on stdout** and then serves. From `client.go:836-900`:

```
CORE_PROTOCOL_VERSION | API_VERSION | network | address | protocol | server_cert
```
e.g. `1|3|unix|/tmp/plugin2951246566|grpc|`

The daemon reads that line, checks the core version, negotiates the API version against its `VersionedPlugins` map, dials the address, and it's connected. That's the entire bootstrap. Everything else is layered on top.

### 5.3 Measurements

All from `/tmp/gpexp`, a real go-plugin gRPC key/value plugin I built today.

**Live reload — the headline number:**
```
--- live reload x5 (host process stays up the whole time) ---
reload 1: 14.609333ms
reload 2: 13.297333ms
reload 3: 12.367916ms
reload 4: 12.013375ms
reload 5: 12.106416ms
```
**[MEASURED] ~12 ms per reload** (kill + spawn + handshake + dispense), warm binary.

**Live reload of *actually new code*, with the daemon untouched.** I started the daemon, had a shell script rebuild the plugin binary underneath it mid-run, then reloaded:

```
1. running OLD plugin binary: world [served by plugin V1 pid 53957]
2. (test harness swaps kv-plugin binary for a NEW build now)
[harness] swapped binary at 23:35:11
3. after reload (449.255625ms): world [served by plugin V2-HOTLOADED pid 54016]   <-- NEW CODE, host never restarted
```
**[MEASURED]** The 449 ms is macOS's first-execution cost for a never-before-seen binary (code-signature validation + page-in). On Linux this won't apply; on macOS it's a one-time cost per new build, and 12 ms thereafter. Either way it is *milliseconds*, and the daemon's PID never changed.

**Crash isolation — I made the plugin panic on purpose:**
```
4. asking plugin to panic...
   host survived. error surfaced as: rpc error: code = Unavailable desc = error reading from server: EOF
5. host still alive; recovering by reloading...
   recovered [served by plugin V2-HOTLOADED pid 54022]
DONE - host process never restarted.
```
**[MEASURED]** A plugin panic is an ordinary Go `error` at the call site. The daemon logs it and relaunches. This is the OS doing the isolation, which is why it's airtight — separate address spaces, enforced by the MMU.

**Latency and throughput over the unix socket:**
```
gRPC round-trip: 2000 calls in 90.877542ms = 45.4 us/call (22008 calls/sec)
  payload      64 B: 39.0 us/call,   2 MB/s
  payload    4096 B: 42.7 us/call,  96 MB/s
  payload   65536 B: 166.1 us/call, 395 MB/s
  payload 1048576 B: 1638.0 us/call, 640 MB/s
```
**[MEASURED] 45 µs per call, 22,000 calls/sec, 640 MB/s at 1 MB payloads.** Sanity check against your actual workload: log-tail, comms, notes, machine roster, ssh-helper. These are dozens-to-thousands of messages per second at most, with one human operator and tens of agents (§6: *"Scale beyond ~3 boxes and tens of agents"*). **45 µs is roughly 1,000× more headroom than you need.** For a log-firehose plugin, use gRPC streaming (one call, many messages) and the per-call cost vanishes into the 640 MB/s number.

**Memory cost — 6 plugins at once:**
```
host RSS alone: 16176 KB
host RSS with 6 plugins connected: 20240 KB
6 plugin processes total RSS: 102144 KB (16.6 MB each avg)
GRAND TOTAL for daemon + 6 plugins: 119.5 MB
```
**[MEASURED] ~17 MB per plugin process, ~120 MB for the daemon plus all six plugins from §3.2** (comms, agent registry, log tail, log archiving, ssh helper, notes). Your mac-mini has 37 GiB free (§1). **That is 0.3% of it.** This is the objection people raise against subprocess plugins, and at your scale it is not an objection.

Plugin binary size: **17.7 MB** (Go static binary with grpc + protobuf). Cross-compiled to linux/arm64: **17.1 MB, 1.8 s** **[MEASURED]**.

### 5.4 Who actually runs this in production, and what they actually do

**Vault — this is the decisive precedent, because Vault does *exactly the thing you're asking for*.**

`POST /sys/plugins/reload/backend` (https://developer.hashicorp.com/vault/api-docs/system/plugins-reload-backend):

> "All instances of the plugin will be killed, and any newly pinned version of the plugin will be started in their place."

And from the plugin architecture doc (https://developer.hashicorp.com/vault/docs/plugins/plugin-architecture):

> "If Vault terminates a plugin process out-of-band, Vault lazily reloads the process when it receives a client request that requires the plugin."

> "Plugin processes continue to run after spawning until Vault explicitly terminates them."

The upgrade workflow in production: register the new binary + SHA256 in the catalog, call reload, old process dies, new process starts, Vault never restarted. **This is your BEAM-lite, it exists, it's shipping, and thousands of enterprises run it.**

And the crucial corollary, from the same doc: **"Plugins don't maintain persistent in-memory state... State management relies on Vault's storage backend."** Vault made plugins stateless *because* it wanted reload. You independently arrived at the same primitive in §3.1 (kernel-owned storage). The two decisions lock together.

Also note Vault's stance on the alternative, from go-plugin's README:

> "we use this plugin system in Vault where dynamic library loading is not acceptable for security reasons."

**Terraform** — every provider (`aws`, `google`, …) is a go-plugin subprocess over gRPC. Terraform Core starts and stops provider processes per graph walk. Thousands of third-party providers, written by people who never see Terraform's source, versioned independently. This is proof the *ecosystem* property works: **a protobuf interface is a real contract that strangers can build against.** That's your §3.2 plugin-library idea, validated at scale.

**Nomad, Boundary, Waypoint, Packer** — same library. README: *"used on millions of machines across many different projects and has proven to be battle hardened and ready for production use."*

**The one caveat, stated plainly**, from the README:

> "While the plugin system is over RPC, it is currently only designed to work over a local [reliable] network. Plugins over a real network are not supported and will lead to unexpected behavior."

Doesn't matter — plugins are local to their daemon by design (§3.1: the daemon does transport, cross-machine traffic goes over your channels, not over go-plugin).

### 5.5 Dev experience

You write an ordinary Go program with a `main()`. You `go build` it. You run it under a debugger, `fmt.Println` in it, `pprof` it — all normal, because it *is* a normal program. go-plugin pipes its stdout/stderr back to the daemon automatically (`grpc_stdio.proto`). There's a `plugin.TestPluginRPCConn` helper for in-process unit tests. There's `ReattachConfig` so you can run the plugin under a debugger by hand and have the daemon attach to it instead of spawning it — which is how Terraform provider authors debug.

**This is the best dev experience of any option here, by a wide margin, and it's not close.** Compare: the WASM option means debugging via wasm stack traces; the Yaegi option means debugging an interpreter's idea of your program.

The cost: **you write the client/server shim per interface.** README: *"In practice, step 2 is the most tedious and time consuming step."* Honest. It's ~40 lines of mechanical adapter code per plugin interface (see `/tmp/gpexp/shared/grpc.go`, copied from their example). Real, but it's boilerplate, not difficulty.

### 5.6 Dependency footprint — the one real downside

`go get github.com/hashicorp/go-plugin@v1.8.0` pulls **[MEASURED]**: grpc v1.61.0, protobuf v1.36.6, yamux, hclog, protoreflect, go-spew, uuid, fatih/color, envoyproxy/go-control-plane, cncf/xds, golang/glog, cloud.google.com/go/compute/metadata… ~44 lines in `go.sum`. That's grpc's transitive tree, not go-plugin's fault, but it's yours now. 17 MB binaries are the consequence.

If you're going protobuf anyway (§3.2 says you are), you were paying most of this regardless.

---

## 6. WASM — the real runner-up

### 6.1 The two libraries

**wazero v1.12.0, released 2026-05-28** — a WebAssembly runtime in pure Go, zero cgo. **[MEASURED via proxy.golang.org]**. Actively maintained.

**extism go-sdk v1.7.1, released 2025-03-02; repo last pushed 2025-05-14 — 14 months ago. 181 stars.** **[MEASURED via GitHub API]**. The core extism project is active (last push 2026-06-19, 5,679 stars) but **the Go SDK specifically has not been touched in over a year.** For a Go daemon, the Go SDK is the part you'd depend on. That's a yellow flag.

Extism's model, from https://extism.org/docs/concepts/pdk/ and the go-pdk README: a plugin exports named functions; **input and output are raw bytes**; "host functions" are the daemon's functions the plugin may call. Protobuf is not mentioned anywhere in extism's docs — because extism doesn't care what's in the bytes. **That's actually a good fit: extism gives you the bytes pipe, you put protobuf in it.**

### 6.2 Can Go-compiled-to-WASM speak protobuf in 2026? I tested. Yes.

This is the question that decides WASM's viability for §3.2, and the answer has changed recently, so old advice is wrong.

Go 1.26.2 supports `//go:wasmexport` **[MEASURED — `go doc cmd/compile`]**, so a Go program can compile to a WASM *library* (not just a run-once command). The type restrictions are severe though:

> "Go types → Wasm types: bool→i32, int32/uint32→i32, int64/uint64→i64, float32→f32, float64→f64, unsafe.Pointer→i32, pointer→i32, string→(i32,i32) (only permitted as a parameters, not a result). **Any other parameter types are disallowed by the compiler.**"

**No `[]byte` in, no `[]byte` out.** To pass a protobuf message you must: have the guest export an `alloc`, write the bytes into the guest's linear memory from the host, call `handle(ptr, len)`, and have the guest return a packed `uint64` of `ptr<<32|len` that the host reads back out of guest memory. That is precisely the plumbing extism exists to hide.

I wrote it by hand and it works. `/tmp/pbwasm`:

```
compile 7.81 MB wasm: 1.582715042s
instantiate (protobuf init inside guest): 6.505125ms
PROTOBUF ROUND-TRIP HOST<->WASM: "plugin handled key=hello"
protobuf-over-wasm call: 20000 in 139.421ms = 6.97 us/call (143450/sec)
3 hot swaps: 14.550917ms total
```

**[MEASURED]** A `.proto`-generated Go type, `proto.Unmarshal` inside the WASM sandbox, `proto.Marshal` back out, host reads it. **google.golang.org/protobuf v1.36.11 runs inside a WASM module built with the stock Go toolchain.** This works because of the native toolchain's full reflection — extism's README steers you to TinyGo for smaller modules, but explicitly documents the native path:

> "As an alternative to TinyGo, you can build the plugin with the native Go toolchain using the `go build` command: `GOOS="wasip1" GOARCH="wasm" go build -o plugin.wasm -buildmode=c-shared main.go`"
> "TinyGo produces a very small WASM file—up to 5 times smaller than the standard toolchain. However, TinyGo has some limitations."

Those TinyGo limitations are mostly *reflection*, which is exactly what protobuf-go needs. **So for protobuf plugins in Go you want the native toolchain, and you pay in size.**

### 6.3 What WASM is genuinely better at

- **Speed: 6.97 µs/call with protobuf, vs go-plugin's 45.4 µs. ~6.5× faster.** [MEASURED, same machine, same protobuf messages.] (A trivial no-protobuf function was 0.34 µs — 130× faster — but that's not a fair comparison for your use case.)
- **Reload: ~1–5 ms to instantiate a fresh module instance** vs 12 ms. [MEASURED: 1.08–1.32 ms trivial; ~4.8 ms with protobuf.]
- **Sandboxing.** A WASM guest can only touch its own linear memory and only call host functions you explicitly provide. This is stronger than a subprocess: a subprocess can `open()` any file its uid can. Irrelevant for §4's trusted network today; relevant if you ever run a plugin you didn't write.
- **Fault isolation works.** I nil-dereffed inside a guest [MEASURED]:
  ```
  host SURVIVED. trap surfaced as error: wasm error: unreachable
  wasm stack trace:
      .runtime.abort(i32) i32 ... .main.boom(i32) i32
  ```
  A trap is an error, not a crash. (Caveat: I also verified the module is *technically still callable* after a trap — `alloc` returned fine — but the Go runtime inside it just took a fatal panic and its heap invariants are gone. **You must discard the instance after a trap.** wazero won't stop you from making that mistake.)
- **One artifact for all platforms.** This is the strongest argument for WASM *in your specific fleet*: your §3.2 idea is *"a plugin gets added to one node, the plugin gets synced across nodes"* and your nodes are darwin/arm64 and linux/aarch64. go-plugin means shipping **two** binaries. WASM means shipping **one** `.wasm`. That's a real, concrete win for the plugin-library idea.

### 6.4 What kills it for now

**(a) Size and compile time.** 1.86 MB `.wasm` for a hello-world; **7.81 MB for one that does a single protobuf Unmarshal/Marshal** [MEASURED]. And compiling that 7.81 MB module took **1.58 seconds** [MEASURED] — that's the cold cost of a "hot load". (Mitigable: `wazero.NewCompilationCacheWithDir(dir)` + `RuntimeConfig.WithCompilationCache` persists compiled code to disk — verified the API exists via `go doc`. Cache key includes the wazero version, so a wazero upgrade re-pays it.)

**(b) One instance = one caller at a time.** I ran 8 goroutines against one module instance [MEASURED]:
```
=== concurrency: 8 goroutines calling ONE instance ===
   concurrent-call error: module closed with exit_code(4)
   concurrent-call error: module closed with exit_code(5)
8x2000 concurrent calls in 295.959µs, errors=8
```
with a guest stack trace through `runtime.mcentral.grow → mcache.refill → mallocgc`. **Concurrent entry corrupts the guest's Go allocator and aborts the module.** You must serialize with a mutex, or maintain an instance pool — and each pooled instance is a fresh ~8 MB heap with no shared state. Every go-plugin subprocess handles concurrency for free because gRPC does. This is a real architectural tax that nobody mentions in WASM blog posts.

**(c) The guest can't do I/O on its own.** A wasip1 guest has no sockets, no goroutine that runs while the host isn't calling in. A **log-tail plugin** — one of your named candidates — wants to sit on a file and push. In WASM it has to be *pulled*, or you have to hand it host functions for everything. Arguably this is fine given §3.1 (*"the daemon would do data transport, while the plugin handled all the logic"*), but it's a constraint you'd design around rather than ignore.

**(d) The industry evidence is mixed-to-negative, and it's worth taking seriously.**
- **Vector (Datadog) added WASM in v0.10 and *removed* it in v0.17.** Their own post-mortem (https://vector.dev/highlights/2021-08-23-removing-wasm/): they *"have not been able to invest the time into it to make it a first-class feature"* and removed it to *"reduce maintenance burden and avoid pushing users to use a transform for extensions that has a poor user experience and poor performance."* **A well-funded team tried WASM plugins and gave up.**
- **Envoy's WASM HTTP filter is still marked experimental after 4+ years** (envoyproxy/envoy#36996 tracks moving it out of alpha). Envoy's own docs: *"experimental and is currently under active development"*, *"not yet recommended for production usage."* Vendors do ship it (Higress has 40+ filters) but the upstream has not blessed it.
- **Fluent Bit** supports WASM for input/filter plugins only, and Go plugins via cgo `c-shared` for output only. A patchwork.
- On the positive side: **Traefik v3 added WASM and it's the healthiest example.** Its `go.mod` on master **[MEASURED]** has `http-wasm/http-wasm-host-go v0.7.0` and `tetratelabs/wazero v1.8.0` — real, shipping, works.

**Verdict: WASM loses today, but it's a legitimate second place and a plausible later addition, not a dead end.** It loses on: 7.8 MB modules and 1.58 s compiles for protobuf plugins, the concurrency tax, guests that can't do their own I/O, the Go SDK for extism being 14 months stale, and an industry track record that includes a high-profile retreat. It wins on speed (6.5×), swap time, sandboxing, and the single-artifact story.

**Note that Traefik runs both Yaegi and WASM side by side.** A protobuf interface is transport-agnostic by construction — the same `.proto` can be served by a subprocess *or* a WASM module. **Choosing go-plugin now does not foreclose adding a WASM runtime later.** That is the single best argument for the protobuf-first instinct in §3.2, and you should bank it.

---

## 7. Embedded interpreters

### 7.1 Yaegi — the dark horse that genuinely is one, and genuinely loses

Yaegi is a Go interpreter written in Go. You hand it Go *source*, it runs it. Live reload is trivially natural: re-evaluate the source.

**And it is the only thing in this entire document that reproduces a real BEAM property.** `/tmp/yaexp` **[MEASURED]**:

```
load v1: 3.769292ms -> hello greg from interpreted V1
HOT RELOAD to v2: 3.585084ms -> HOWDY greg from interpreted V2
old v1 closure STILL WORKS side by side: hello greg from interpreted V1
interpreted call: 200000 calls in 184.480375ms = 0.92 us/call
native Go call:   200000 calls in 10.860166ms = 0.054 us/call  (yaegi is 17.0x slower)
```

**3.6 ms reload. Two versions of the plugin live simultaneously in one process, both callable.** That is literally Erlang's "current and old". No other Go option does this. It deserved a serious look and I gave it one.

**It loses on four independent counts, any one of which would be enough.**

**(a) It is functionally abandoned.** **[MEASURED via GitHub API + proxy.golang.org, 2026-07-15]**:
- Last **release**: **v0.16.1, 2024-04-03**. Over two years ago.
- Last commit: 2026-02-09 (5 months ago) — trickle maintenance, no releases.
- 182 open issues. Still 0.x after 8 years.
- Its `stdlib/` symbol maps are named `go1_21_*.go`, and its README says: *"Support the latest 2 major releases of Go (Go 1.21 and Go 1.22)."* **We are on Go 1.26.2.** It is four major Go releases behind and has shipped nothing to catch up.
- **Traefik — the project that owns Yaegi — pins `github.com/traefik/yaegi v0.16.1` in its `go.mod` and added a WASM runtime alongside it. [MEASURED from Traefik master's go.mod.]** The maintainer voted with its feet.

**(b) It doesn't implement the Go you write.** [MEASURED]:
```
generics:                    EVAL ERROR: 8:44: cannot use type func(int) string as type func(int) int
range-over-func (Go1.23):    PANIC ESCAPED TO HOST: nil type
```
A two-type-parameter generic function — `Map[T,U](xs []T, f func(T) U) []U`, about as basic as generics get, a Go 1.18 feature — fails to typecheck. Range-over-func, Go 1.23, doesn't exist. You would be writing Go-2021-with-holes and discovering the holes at runtime.

**(c) A plugin can kill your daemon, and this is unfixable.** [MEASURED] — plugin code that panics on a goroutine it spawned itself:
```
panic: plugin panicked on ITS OWN goroutine [recovered, repanicked]
goroutine 3 [running]:
github.com/traefik/yaegi/interp.runCfg.func1()
...
HOST EXIT CODE: 2
```
**The daemon died.** You cannot `recover()` another goroutine's panic in Go — it's a language rule, not a Yaegi bug, and no version of Yaegi will ever fix it. Interpreted code runs on real goroutines in your address space. It also spawns goroutines you can't reclaim [MEASURED: `plugin spawns goroutine: OK` — and that goroutine now lives forever, surviving every "reload"].

So Yaegi gives you BEAM's code loading with **anti**-supervision: the failure mode Erlang exists to prevent is the one Yaegi hands you.

**(d) It's Go-only and has no protobuf story.** §3.2 wants an interface defined independently of any code, reachable from REST/pubsub/whatever. Yaegi's interface is "a Go function value pulled out of an interpreter by name." That's the *opposite* of language-independent — it's the most Go-coupled option on the board.

**Verdict: fascinating, closest to BEAM on paper, and a trap.** Abandoned, incomplete, unsafe, and structurally opposed to the protobuf requirement.

### 7.2 Starlark — right tool, wrong job

`go.starlark.net`, pseudo-version `v0.0.0-20260708150628` — **last commit 2026-07-08, i.e. last week.** Repo pushed 2026-07-10, 2,734 stars. **[MEASURED]** Very healthy (no tagged releases; that's normal for this project).

Starlark is Python-shaped, deliberately restricted, and designed for configuration (it's Bazel's language). **[MEASURED]** `/tmp/starexp`:
```
load v1 203.75µs -> "v1 handled: ping"
HOT RELOAD 20.666µs -> "V2 HANDLED: PING"
starlark call: 0.31 us/call
```
**20 microseconds to hot reload.** Fastest reload of anything I tested, by 500×. And it has real runaway protection — `thread.SetMaxExecutionSteps(n)` will halt a script that loops forever, which no other option here offers.

But its restrictions are the point and they disqualify it: my deliberately-runaway test script was rejected outright with `bad.star:3:1: for loop not within a function` — Starlark forbids top-level loops by design. **No I/O, no goroutines, no sockets, no long-running anything, no protobuf.** A Starlark "plugin" is a pure function from values to values.

**Verdict: not a plugin system for log-tail/comms/archiving. It is, however, the right thing if you later want a routing-rule or filter language *inside* a plugin** — user-editable snippets that decide where a message goes. Keep it in your back pocket for that; don't make it the plugin substrate.

### 7.3 Lua (gopher-lua) — same shape, less relevant

v1.1.2, 2026-04-01, 6,952 stars, actively maintained **[MEASURED]**. Well-proven for embedded scripting. Everything true of Starlark's mismatch is true here, minus the sandboxing rigor and plus a language nobody in your fleet writes. Notably, Vector's WASM removal post recommends *"the `lua` transform"* as the alternative — which tells you Lua's niche is *data transformation*, not *plugins*. Loses on: not Go, no protobuf, no crash isolation, no reason to prefer it over Starlark.

---

## 8. Roll your own: subprocess + protobuf over stdio or a unix socket

This is go-plugin's model with the framework removed. Sometimes it's right — §"reuse over invention" cuts both ways, and 44 lines of `go.sum` is a real cost.

**Real precedents that do exactly this, successfully:**

**Telegraf's `execd`** (https://github.com/influxdata/telegraf/tree/master/plugins/inputs/execd): *"runs the given external program as a long-running daemon and collects the metrics ... on the process's stdout."* Config knobs: `restart_delay` (default `10s`) for auto-restart after unexpected termination, `stop_on_error`, `signal` (`STDIN` / `SIGHUP` / `SIGUSR1` / `SIGUSR2` / `none`), stderr relayed to Telegraf's log. **~100 lines of concept, in production for years.** Proof that the minimal version works.

**osquery's extensions** (https://osquery.readthedocs.io/en/stable/deployment/extensions/) — the closest analogue to your architecture of anything I found. Every extension is a **separate process**, talking **Thrift** (an IDL like protobuf) over a **unix domain socket**. `extensions_autoload` is a file of executable paths; osquery execs each as a monitored child, passing `socket`, `timeout`, `interval` as flags. Then — and this is the part that matters for you — *"During an extension's set up it will 'broadcast' all of its registered plugins to the osquery shell or daemon process."*

**That broadcast is literally §3.1's** *"There would be a registration process with the daemon, and the plugin would say what resources it was interested in/needed to react to."* **A production system already validated your registration design.** It's the same shape as go-plugin's handshake + `Dispense`, arrived at independently.

### What does go-plugin actually buy over rolling your own?

I counted: **7,858 lines of Go** in go-plugin v1.8.0 (excluding tests) **[MEASURED: `wc -l *.go`]**. Here's what that buys, each verified in the source:

| Feature | Where | Would you write it? |
|---|---|---|
| Handshake + address discovery | `client.go:836-900` | Yes, ~50 lines. Easy. |
| Protocol version negotiation (`VersionedPlugins`) | `client.go:150-153, 1040-1042` | Yes. §3.2 demands it. Fiddly. |
| Process spawn/supervise/reap, orphan cleanup | `client.go`, `process.go`, `runner/` | Yes. **This is where the bugs are** — see their CHANGELOG v1.6.2: *"Fixed a bug where reattaching to a plugin that exits could kill an unrelated process."* PID reuse. You would have shipped that bug. |
| stdout/stderr forwarding to daemon logs | `grpc_stdio.proto`, `grpc_stdio.go` | Probably. Annoying. |
| AutoMTLS (auto mutual TLS between daemon and plugin) | `mtls.go`, `client.go` | No. §4 is a trusted network. **Skip.** |
| SHA256 binary verification (`SecureConfig`) | `client.go:327-340` | **Yes — and you want it.** §3.2's plugin-library idea ("plugin gets synced across nodes") needs exactly this: verify the synced binary matches the expected checksum before exec. It's already written. |
| `GRPCBroker` — plugin opens extra streams back to the daemon | `grpc_broker.proto`, `grpc_broker.go` | Maybe later. Bidirectional plugins. |
| Reattach (daemon upgrades while plugin keeps running) | `client.go:1016`, `ReattachConfig` | The **inverse** live reload — restart the *kernel* without dropping plugins. You will want this eventually and won't want to write it. |
| yamux multiplexing (net/rpc path only) | `mux_broker.go` | No. Use gRPC. |

**Verdict: use the framework.** The 40% you'd skip (AutoMTLS, yamux, TTY preservation) is dead weight you can ignore. The 60% you'd need (process supervision edge cases, version negotiation, checksum verification, stdio, reattach) is exactly the part where nine years of other people's production bugs are already fixed. And "reuse over invention" is an explicit ground rule. **Roll your own only if the grpc dependency tree turns out to be genuinely intolerable — and it won't at 17 MB per binary and 120 MB for six plugins on a box with 37 GiB free.**

---

## 9. What real plugin-based daemons actually do — and did any get live reload right?

| System | Plugin mechanism | Live code reload? |
|---|---|---|
| **HashiCorp Vault** | go-plugin: subprocess + gRPC/protobuf, AutoMTLS, SHA256-verified | ✅ **Yes — the reference implementation.** `sys/plugins/reload/backend` kills and restarts plugin processes; Vault stays up. Lazy re-spawn on next request. Plugins are stateless; Vault owns storage. |
| **Terraform** | go-plugin: subprocess + gRPC/protobuf | Effectively — providers start/stop per graph walk. Thousands of independent third-party providers prove the IDL-contract model. |
| **Nomad / Boundary / Waypoint / Packer** | go-plugin | Same model. |
| **osquery** | Separate process, **Thrift** over unix socket, autoload + monitored children, capability broadcast on registration | ✅ Yes — extensions restart independently of osqueryd. Different IDL, same architecture. |
| **Telegraf** | Compile-time Go plugins **plus** `execd`: subprocess over stdio, `restart_delay`, signal-driven | ✅ For `execd`. Restart the child, agent stays up. Minimal, works. |
| **Traefik** | **Two runtimes: Yaegi (Go source) and WASM** (`wazero` + `http-wasm`) | Partial. Docs say Yaegi plugins are *"Hot-reloadable during development"* — a dev feature, not an ops one. Notable: **Traefik added WASM alongside Yaegi and pinned Yaegi at its last 2024 release.** WASM is not supported for provider plugins, only middleware. |
| **Caddy** | **Compile-time Go modules**, `caddy.RegisterModule()`, rebuild with `xcaddy` | ❌ **No code reload — by design.** Docs: *"The `xcaddy` command ... compiles Caddy with your plugin."* Caddy has superb zero-downtime **config** reload and deliberately zero code reload. **A top-tier Go daemon looked at this exact problem and chose "rebuild the binary."** That's the honest baseline your alternative must beat. |
| **Fluent Bit** | Go plugins via cgo `-buildmode=c-shared` (**output only**), WASM (**input/filter only**), C natively | Partial and inconsistent. The clearest evidence that shared-library Go plugins are a bad road: even the project that supports them restricts them to one plugin type. |
| **Vector (Datadog)** | Compile-time Rust feature flags | ❌ No. **Tried WASM in v0.10, removed it in v0.17** citing maintenance burden and *"poor user experience and poor performance."* |
| **Envoy** | WASM filters (proxy-wasm ABI 0.2.1) | ✅ Mechanically yes — but **still experimental after 4+ years**; upstream docs say *"not yet recommended for production usage"* (envoyproxy/envoy#36996 tracks de-alpha-ing it). |

**The pattern is unmistakable:** *every system that got live code reload right did it with **separate processes and an IDL** — Vault, osquery, Telegraf-execd. Every system that tried in-process code loading either restricted it severely (Fluent Bit), left it experimental for years (Envoy), abandoned it (Vector), or refused on principle and told you to rebuild (Caddy).*

That's not a coincidence and it's not fashion. **Process boundaries are the only isolation primitive that actually works without a VM designed for it.** Erlang built the VM. Nobody else did.

---

## 10. Comparison table

Scores are for **your** requirements (§3.2, §4, §6), not in the abstract. All performance numbers **[MEASURED]** on gb-mbp, darwin/arm64, Go 1.26.2, today.

| | **Go stdlib `plugin`** | **hashicorp/go-plugin** | **WASM (wazero/extism)** | **Roll-your-own subprocess** | **Yaegi** | **Starlark** |
|---|---|---|---|---|---|---|
| **Live reload** | ❌ **Impossible.** `plugin already loaded`. Workaround leaks ~1 MB/reload, never freed | ✅ **12 ms** warm / ~450 ms first-exec of a new binary on macOS | ✅ **~1–5 ms** instantiate; **1.58 s** to compile a 7.8 MB protobuf module (cacheable) | ✅ Same as go-plugin | ✅ **3.6 ms**, + two versions live at once | ✅ **20 µs** |
| **Crash isolation** | ❌ None — your address space | ✅ **OS processes. Verified: plugin panic → daemon lives** | ⚠️ Traps→errors ✅, but **must discard instance after trap**; no guard against that | ✅ Same | ❌ **Goroutine panic kills the daemon (exit 2)** | ⚠️ No I/O to crash on; step budget exists |
| **Language independence** | ❌ Go only, exact toolchain | ✅ **Any gRPC language.** Their repo ships a working **Python** plugin | ✅ Rust/JS/Go/C/Zig/.NET/… | ✅ Anything that speaks the socket | ❌ Go only (and not all of it) | ❌ Starlark only |
| **Protobuf fit** | ❌ Irrelevant — it's Go symbols | ✅ **Native. Transport *is* protobuf. Internals are 3 .proto services** | ⚠️ **Works** (verified protobuf-go inside wasm) but **you hand-marshal through linear memory**; extism hides it | ✅ Native — you choose | ❌ Anti-fit | ❌ No |
| **Performance** | ✅ Native speed | **45.4 µs/call, 22k/s, 640 MB/s @1 MB** | ✅ **6.97 µs/call with protobuf (6.5× faster)** | ≈ go-plugin | 0.92 µs/call (17× slower than native) | 0.31 µs/call |
| **Concurrency** | ✅ Native | ✅ Free (gRPC) | ❌ **1 instance = 1 caller. 8 goroutines → guest allocator corruption, module aborted** | ✅ Free | ⚠️ Real goroutines, unreclaimable | N/A |
| **Dev experience** | ❌ Cryptic. Two build flags to make reload work at all | ✅ **Best.** It's a normal Go program: debugger, pprof, prints, `ReattachConfig` | ⚠️ wasm stack traces; unusual build; two toolchain choices | ✅ Good, but you own the plumbing | ⚠️ Debugging an interpreter's idea of your code | ✅ Simple |
| **Cross-compile for your fleet** (darwin/arm64 + linux/aarch64) | ❌ **Impossible without a cgo cross-toolchain** — verified error | ✅ **1.8 s**, static, `CGO_ENABLED=0`. **Two artifacts** | ✅ **ONE artifact for all platforms** ← WASM's best argument | ✅ Same as go-plugin | ✅ Source is portable | ✅ Source is portable |
| **Artifact size** | 4 MB (hello world) | **17.7 MB** per plugin | **1.86 MB** trivial / **7.81 MB** with protobuf | ≈17 MB | source | source |
| **Memory** | leaks forever | **~17 MB/plugin; 120 MB for daemon+6** = 0.3% of the mac-mini's free RAM | ~8 MB/instance, ×N for concurrency | ≈17 MB | in-process | tiny |
| **Operational cost** | ❌ **Lockstep rebuild of daemon + every plugin for a one-char change in a shared package** | ⚠️ N child processes to supervise (the library does it) | ⚠️ Compile cache to manage; instance pools to size | ⚠️ You own supervision bugs | ❌ Frozen at Go 1.21/1.22 | ✅ Trivial |
| **Maintenance health** (2026-07-15) | Stdlib, but `plugin.Close()` **open since 2017, "Unplanned", 70 comments** | ✅ **v1.8.0 2026-04-29; pushed 9 days ago; MPL-2.0** | ⚠️ wazero **v1.12.0 2026-05-28 ✅**; extism go-sdk **v1.7.1, untouched 14 months** ⚠️ | n/a | ❌ **No release since 2024-04-03. Go 1.21/1.22 only. Traefik pinned it and added WASM** | ✅ pushed last week |
| **Production proof for *live reload*** | none | ✅ **Vault: `sys/plugins/reload/backend` in production** | ⚠️ Envoy still experimental after 4 yrs; **Vector removed WASM** | ✅ osquery, Telegraf `execd` | Traefik: "hot-reloadable **during development**" | Bazel (config only) |

---

## 11. Verdict

### Use `hashicorp/go-plugin`. Subprocess + gRPC + protobuf. Reload = kill the child, spawn the new binary.

**Why it wins, ranked by how much each reason matters:**

1. **It is already the thing §3.2 describes.** protobuf interface, defined in a file, independent of code, versioned, negotiated, reachable from any language. You are not adapting a tool to your requirement — the requirement *is* this tool's design.
2. **Live reload is proven in production for exactly your use case.** Vault ships it. `POST /sys/plugins/reload/backend`. Not a blog post — an API with a support contract.
3. **It's the only option that gives crash isolation you can't accidentally defeat.** [MEASURED: panic → daemon survived → reload → recovered.] The MMU enforces it. You cannot get this wrong.
4. **Live reload costs 12 ms and the daemon's PID never changes.** [MEASURED]
5. **Performance is a non-issue at your scale.** 45 µs and 22k calls/sec against a workload of a few boxes and tens of agents. ~1000× headroom.
6. **Memory is a non-issue at your scale.** 120 MB for the daemon plus all six of your §3.2 plugin candidates. 0.3% of the mac-mini's free space.
7. **Best dev experience by a mile.** Plugins are normal Go programs. Debugger, profiler, print statements, all normal.
8. **Cross-compiles to your fleet in 1.8 seconds** with no toolchain, which makes §3.2's plugin-library idea buildable.
9. **The parts you'd want are already written**, including `SecureConfig` SHA256 verification — which is precisely what "a plugin gets synced across nodes" needs.
10. **Actively maintained**, MPL-2.0, v1.8.0 released ten weeks ago.

**Why each alternative lost:**

- **Go stdlib `plugin`** — Cannot reload the same plugin *at all* (measured: `plugin already loaded`). The workaround leaks ~1 MB per reload forever (measured). One-character change to a shared package invalidates every plugin binary (measured). Cannot cross-compile without a cgo cross-toolchain (measured). No crash isolation. `plugin.Close()` unplanned since 2017. **The Go stdlib docs themselves tell you to use IPC instead.** Not close.
- **WASM (wazero/extism)** — *Genuine runner-up, and the one to revisit.* Beats go-plugin on speed (6.5×), swap time, sandboxing, and — importantly for your fleet — **one artifact instead of two**. Loses today on: 7.81 MB modules and 1.58 s compiles for protobuf plugins (measured); **one instance serves one caller — 8 concurrent callers corrupted the guest's allocator** (measured); guests can't do their own I/O, which your log-tail plugin wants; extism's Go SDK untouched for 14 months; and a track record that includes **Vector ripping WASM out** and **Envoy still calling it experimental after four years**. Lost on maturity and operational friction, not on concept. **Because the interface is protobuf, you can add a WASM runtime later against the same `.proto` — exactly what Traefik did. Bank that.**
- **Yaegi** — The only Go option that reproduces a real BEAM property (two live versions, measured, and it's genuinely impressive). But: **no release since 2024-04-03**; Go 1.21/1.22 only while you're on 1.26.2; **generics broken** (measured); **range-over-func broken** (measured); **a plugin goroutine panic kills your daemon, exit code 2** (measured, and unfixable — it's a Go language rule); Go-only; anti-fit for protobuf; and **Traefik, its own maintainer, pinned it and built a WASM runtime next to it.** It offers you BEAM's code loading with anti-supervision. That's the wrong half.
- **Starlark / Lua** — Wrong job. No I/O, no goroutines, no protobuf, not Go. **But Starlark is the right answer for a future user-editable routing/filter language *inside* a plugin** — 20 µs reload, real step budgets. Keep it in the back pocket.
- **Roll your own subprocess + protobuf** — This *is* go-plugin, minus 7,858 lines where nine years of production bugs (PID reuse on reattach, orphan reaping, version negotiation) are already fixed. Validated by Telegraf `execd` and osquery, so it's a real option if the grpc dependency tree ever becomes intolerable. It isn't. "Reuse over invention" is an explicit ground rule.
- **Compile-time modules (Caddy's model)** — Has no live reload by definition. Listed because it's the honest baseline: **a first-rate Go daemon considered this exact problem and chose "rebuild the binary,"** and Caddy is not a worse project for it. If live reload turns out to be less valuable than you expect, this is where you land, and it's not a disgrace.

---

## 12. What live reload really costs, and what it forbids

This is the section to read twice, because the constraint is the design.

### It forbids plugins holding in-memory state. Period.

**[MEASURED]** — go-plugin, reload, then read back what I wrote before the reload:
```
reload 1: 14.609333ms  | state after reload: "" (err means state was lost)
reload 2: 13.297333ms  | state after reload: ""
reload 3: 12.367916ms  | state after reload: ""
```
The plugin's map was gone. Every time. Same for WASM — `mod.Close()` and the entire linear memory, heap and all, is freed. **[MEASURED]**

This is not a bug to work around. It is the **defining property** of the whole family. BEAM avoids it only via `code_change/3`, which hands your old state to your new code and asks you to migrate it — and **nothing in Go has any equivalent, and nothing in Go can.** There is no mechanism to hand a struct built by one binary's compiler to a different binary's compiler and have it mean the same thing.

**So the rule is: a plugin must be a pure function of kernel-held state plus its inputs.**

- ✅ A **log-tail** plugin can hold its file offset... **in kernel storage**, keyed by file path. Reload, re-read the offset, resume.
- ✅ A **comms** plugin holds no state: messages live in kernel storage (§5: *"all the messages are written down"* — already the model), subscriptions live in kernel channel registrations.
- ✅ An **agent registry** plugin holds no state: the roster is in the kernel (§3.1: *"The machine roster probably should be baked in"* — already the model).
- ❌ A plugin holding an open TCP connection to a remote service, with a warm cache and a sequence number, cannot be reloaded without dropping it.

**The good news is you already asked for the fix.** §3.1: *"One thing that might be useful is to have a storage mechanism in the daemon. Then the plugins dont make up their own thing."* You wrote that for a different reason — consistency, not reload. **It turns out to be the exact precondition that makes live reload work.** Vault reached the same conclusion from the same direction: *"Plugins don't maintain persistent in-memory state... State management relies on Vault's storage backend."* You and HashiCorp independently designed the same thing.

**Make this a hard rule from day one, not a discovery in month three:** a plugin that stores something important in a Go variable is a bug. Kernel storage or nothing. If you enforce it at the start, live reload is free forever. If you don't, you'll find out the day you reload the comms plugin and lose the queue.

### The other costs, honestly

- **In-flight requests die.** `client.Kill()` is a kill. Vault's own reload docs don't promise otherwise; the reload API returns a warning if nothing reloaded and says nothing about draining. **Mitigation:** the kernel already sits between callers and plugins (§3.1 — the daemon does transport, the plugin does logic). Stop dispatching → drain → kill → spawn → resume. That's ~30 lines in the kernel and gets you most of BEAM's "nothing is interrupted", without BEAM.
- **Versioning becomes a real discipline, not a nice-to-have.** The moment a plugin can be replaced independently, you have two versions of the interface in the world. §3.2 already says *"must be versioned and probably backwards (maybe forwards?) compatible."* protobuf gives you the tools: **never renumber a field, never reuse a number, never change a type, new fields are always optional.** Follow those four rules and forwards *and* backwards compatibility are free. Break one and you get silent corruption, not an error. This is the one place go-plugin can't save you.
- **You now supervise N+1 processes.** The library handles spawn/monitor/reap, but *you* decide restart policy: backoff, crash loops, when to give up. Telegraf's answer is one knob (`restart_delay = "10s"`) and it's been fine for years. Steal that. Don't build a supervision tree DSL.
- **Reload is a lie about the daemon itself.** go-plugin reloads *plugins*. Changing the **kernel** — channels, roster, storage — still means restarting the daemon. That's the thing you actually complained about in harmonik (*"having to stop the whole system every time"*), and it's only fixed by the kernel staying genuinely small (§3: *"maybe the core (or kernal?) is actually really light"*). **The plugin system doesn't make the kernel reloadable — keeping the kernel tiny does.** go-plugin's `ReattachConfig` (README: *"Host upgrade while a plugin is running"*) is the escape hatch when you eventually need it: plugins keep running while the daemon restarts under them. Worth knowing it's there. Don't build for it on day one.
- **`.proto` files need a home and a build step.** `protoc` or `buf` in the loop, generated code checked in or generated on build. Minor, but it's new machinery you don't have today. (Note: **`protoc` is not installed on gb-mbp** — I checked; go-plugin's examples ship pre-generated `.pb.go` files, which is how I benchmarked without it. You'll need `buf` or `protoc` before writing your first real `.proto`.)

---

## 13. Open questions

1. **Is `gb-mac-mini` darwin/arm64?** Couldn't verify — `ssh 100.120.22.74` returned `Host key verification failed` from this session. `gb-mbp` is darwin/arm64 and `dgx` is linux/aarch64 [MEASURED]. If the mac-mini is arm64 Darwin, the fleet needs exactly **two** plugin artifacts. If anything is amd64, it's four. Affects §3.2's plugin-library idea, not the recommendation.
2. **Does the 450 ms first-exec penalty exist on Linux?** I measured it only on macOS, where it's almost certainly Gatekeeper/code-signature validation of an unseen binary. On `dgx` it should be closer to the 12 ms warm number. Worth one measurement before promising reload latency.
3. **Does `plugin.Client.Kill()` drain in-flight gRPC calls, or hard-kill?** I read enough of `client.go` to know it kills the process; I did not trace whether it waits on outstanding streams. Determines how much draining the kernel must do itself.
4. **Would a `wazero` compilation cache bring a 7.81 MB protobuf module's load under 50 ms?** `NewCompilationCacheWithDir` exists [verified via `go doc`], I didn't benchmark it. Only matters if WASM gets revisited.
5. **Can TinyGo compile `google.golang.org/protobuf`?** I proved the *native* Go toolchain can [MEASURED: 7.81 MB, works]. TinyGo would be ~5× smaller but its reflection limits are exactly what protobuf-go needs. Untested. Only matters if WASM gets revisited.
6. **Is Yaegi's two-live-versions property worth a hybrid?** e.g. go-plugin for real plugins, Yaegi for hot-editing a routing rule. My instinct is no — Starlark is the better fit for that niche and is actually maintained — but the option exists and Traefik does run two runtimes.

---

## Sources

**Files read (local):**
- `/Users/gb/research/2026-07-15-agent-substrate-v2/BRIEF.md` (full)
- `/opt/homebrew/Cellar/go/1.26.2/libexec/src/plugin/plugin.go` (package docs, lines 1-60)
- `/opt/homebrew/Cellar/go/1.26.2/libexec/src/plugin/plugin_dlopen.go` (lines 1-240; `dlopen` at 18-23, build tag at 5, `open()` at 41-124)
- `/opt/homebrew/Cellar/go/1.26.2/libexec/src/plugin/plugin_stubs.go` (build tag, line 5)
- `/opt/homebrew/Cellar/go/1.26.2/libexec/src/runtime/plugin.go` ("plugin already loaded" at 29/33-36; pkghash check at 54-59)
- `/opt/homebrew/Cellar/go/1.26.2/libexec/src/runtime/symtab.go` (`modulesinit()` at 544-558)
- `~/go/pkg/mod/github.com/hashicorp/go-plugin@v1.8.0/` — `README.md`, `CHANGELOG.md`, `LICENSE`, `client.go` (143-153, 301, 327-340, 619-639, 820-900, 1016-1042), `plugin.go`, `internal/plugin/*.proto`, `examples/grpc/**`, `examples/negotiated/{README.md,main.go}`, `examples/grpc/plugin-python/`
- `~/go/pkg/mod/github.com/traefik/yaegi@v0.16.1/` — `README.md` (line 20), `go.mod`, `stdlib/go1_21_*.go`

**Commands run:**
- `go version` → `go1.26.2 darwin/arm64`; `go env GOROOT GOOS GOARCH`
- `go doc plugin`; `go doc -all cmd/compile` (WebAssembly Directives section)
- `go help buildmode`; `go tool dist list | grep wasm`
- `go doc github.com/tetratelabs/wazero NewCompilationCacheWithDir`; `go doc ... RuntimeConfig`
- `go list -m -versions` for go-plugin, wazero, extism/{go-sdk,go-pdk}, yaegi, starlark, gopher-lua, caddy
- `curl https://proxy.golang.org/<mod>/@latest` for release dates of all above
- `curl https://api.github.com/repos/<repo>` for archived/pushed_at/stars: traefik/yaegi, hashicorp/go-plugin, tetratelabs/wazero, extism/{extism,go-sdk}, yuin/gopher-lua, google/starlark-go, caddyserver/caddy
- `curl https://api.github.com/repos/golang/go/issues/{20461,24880,19282}`
- `curl https://raw.githubusercontent.com/traefik/traefik/master/go.mod` (wazero/yaegi/http-wasm pins)
- `curl https://raw.githubusercontent.com/extism/go-pdk/main/README.md`
- `ssh gb@100.115.27.55 'uname -srm'` → `Linux 6.17.0-1026-nvidia aarch64`
- `wc -l ~/go/pkg/mod/github.com/hashicorp/go-plugin@v1.8.0/*.go` → 7858 total
- `which protoc protoc-gen-go buf tinygo wasmtime wasmer extism` → none installed

**Experiments written and run this session (code on disk):**
- `/tmp/pluginexp/` — Go stdlib plugin: two-version load failure; `-gcflags=-p=`/`-ldflags=-pluginpath=` workaround; 20-reload RSS leak measurement; shared-dependency hash mismatch; cross-compile failure
- `/tmp/gpexp/` — hashicorp/go-plugin v1.8.0 gRPC KV plugin: cold start, reload ×5, live binary swap with new code, deliberate plugin panic + recovery, RPC latency, payload sweep, 6-plugin memory, linux/arm64 cross-compile
- `/tmp/wzexp/` — wazero v1.12.0 + Go 1.26 `//go:wasmexport` reactor module: compile/instantiate/call timings, hot swap, guest trap isolation, 8-goroutine concurrency corruption
- `/tmp/pbwasm/` — google.golang.org/protobuf v1.36.11 inside a wasip1 module under wazero: end-to-end protobuf round trip, size, compile time, call latency
- `/tmp/yaexp/` — yaegi v0.16.1: hot reload, two-versions-live, interpreted vs native benchmark, generics failure, range-over-func failure, panic escape, goroutine-panic daemon kill (exit 2)
- `/tmp/starexp/` — go.starlark.net: hot reload timing, call benchmark, execution-step budget

**URLs fetched:**
- https://www.erlang.org/doc/system/code_loading.html
- https://www.erlang.org/doc/system/release_handling.html
- https://github.com/golang/go/issues/20461
- https://developer.hashicorp.com/vault/api-docs/system/plugins-reload-backend
- https://developer.hashicorp.com/vault/docs/plugins/plugin-architecture
- https://doc.traefik.io/traefik/extend/extend-traefik/
- https://caddyserver.com/docs/extending-caddy
- https://github.com/influxdata/telegraf/tree/master/plugins/inputs/execd
- https://osquery.readthedocs.io/en/stable/deployment/extensions/
- https://vector.dev/highlights/2021-08-23-removing-wasm/
- https://extism.org/docs/concepts/pdk/
- https://www.envoyproxy.io/docs/envoy/latest/configuration/http/http_filters/wasm_filter
- https://github.com/envoyproxy/envoy/issues/36996
- https://docs.fluentbit.io/manual/development/golang-output-plugins
- https://docs.fluentbit.io/manual/fluent-bit-for-developers/wasm-filter-plugins
- https://traefik.io/blog/traefik-3-deep-dive-into-wasm-support-with-coraza-waf-plugin
