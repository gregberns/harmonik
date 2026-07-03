# Research — `pi_agent_rust` (Dicklesworthstone Rust port) as flywheel substrate

> Component: `pi-agent-rust`. Round-3. Source: sub-agent (opus) over github.com/Dicklesworthstone/pi_agent_rust + AGENTS.md + docs/, 2026-05-30. **Paradigm-shifting finding.**

## TL;DR
- **Real, ambitious, production-grade Rust port — not a toy.** 3,524 commits in 4 months, 1,083 stars, last commit yesterday (2026-05-29), ~21 MiB single binary, **`#![forbid(unsafe_code)]`**, structured-concurrency runtime (Jeffrey's own `asupersync`), v0.1.16 on crates.io, **"with Mario Zechner's blessing"** (the canonical TS Pi author).
- **THE KILLER FINDING:** extension authoring is **unchanged from TS Pi** — extensions are JS/TS files dropped in `~/.pi/agent/extensions/` running in embedded **QuickJS** (`rquickjs 0.11`) inside the Rust process. Conformance: **205/223 (91.9%) of the canonical TS-Pi extension corpus runs UNMODIFIED**. The "Rust port = Cargo-crate extensions" objection (the original reason to prefer TS) is **wrong**.
- **Verdict: viable substrate; recommend serious consideration over TS Pi.** Same JS/TS extension ergonomics + Jeffrey's engineering rigor + 21 MiB single binary (operationally aligned with harmonik Go single-binary) + materially stronger security model. Two parity items + the LICENSE rider need a 1-week shake-down before commit.

## 1. What is it
"From-scratch Rust port of Pi Agent by Mario Zechner (made with his blessing!)" — faithful behavioral port + selective re-architecture. Full surface: loop, TUI, providers, sessions, extensions, RPC, SDK. Same product surface as TS Pi (`pi`, `pi --continue`, `pi -p`, `pi --mode rpc`) PLUS added security/perf machinery (capability-gated hostcalls, two-stage `exec` mediation, trust lifecycle, deterministic hostcall reactor mesh, session-store v2 ADR). License: **"MIT + Rider"** — the rider should be read before commercial use.

## 2. Parity vs the surface flywheel needs
| TS Pi surface | Rust port status |
|---|---|
| `pi.registerTool` / `pi.tool()` | **Present** — `pi.tool()` hostcall + `pi.events("registerTool", spec)` |
| Turn-boundary hooks | **Present semantically, different names** — `turn_start`, `turn_end`, `stop`, `session_start`, `session_before_fork`, `tool_call`, `tool_result` registered via `pi.on(eventName, fn)`. **Did NOT surface a `session_before_compact` string in the matrix → 30-min source grep needed before commit.** |
| `transformContext` (per-call rewrite) | **Partially present / different shape** — input-transform pattern exists (`inline-bash.ts` tagged `interaction: input_transform`); whether arbitrary message-array rewrite is first-class needs source confirmation. **Real risk if absent.** |
| `appendEntry` / `custom_message` durable entries | **Present** — `docs/session.md` Entry types: `custom: Extension-defined structured payload` — survives session V2 ADR. Notes vehicle works. |
| `getContextUsage()` | **Present (as state surface)** — `rpc.md get_state` returns token usage; `sdk.md` exports `Usage`+`Cost`+`AgentEvent`; `compaction` settings track `reserve_tokens`/`keep_recent_tokens`. |
| `compact()` / `newSession()` reseed | **Present** — `rpc.md compact` (with `customInstructions`/`reserveTokens`/`keepRecentTokens`), `new_session`, `switch_session`, `fork`; `sdk.md AgentSessionHandle::compact()`. |
| `steer`/`followUp`/`nextTurn` queues | **Present** — `rpc.md steer`, `follow_up`, `abort`, `prompt` with `streamingBehavior: "steer"|"follow-up"`; `set_steering_mode` / `set_follow_up_mode`. |
| Multi-LLM providers | **Present, broader than TS** — anthropic, openai, openai-codex, zai, minimax, kimi, qwen, openrouter, cerebras, groq, copilot (device flow); extensions can `pi.registerProvider({streamSimple})`. |
| TUI | **Present** — Charm-stack ports (`charmed-bubbletea`/`lipgloss`/`bubbles`/`glamour` 0.2.0) + `crossterm` + `rich_rust`. NTM is also Charm-based → visual idiom alignment with harmonik. |
| RPC stdin mode | **Present, well-specified** — JSON-lines over stdin/stdout, full request/response/event protocol (`docs/rpc.md`: ~25 commands, 12 event types); stable `RpcTransportClient` Rust SDK type for embedding. Often better-specified than TS RPC. |
**Verdict:** ~85-90% parity by surface name, ~100% by semantic capability for what flywheel does. Two items need source-level confirmation before commit: `session_before_compact`-equivalent timing, and whether `transformContext`-style pre-call message rewrite is first-class.

## 3. Extension authoring — the surprise
**Extensions are JS/TS files dropped in `~/.pi/agent/extensions/` — identical to TS Pi.** `docs/extension-architecture.md`: *"Legacy JS/TS entrypoints (.js/.jsx/.ts/.mjs/.cjs/.tsx/.mts/.cts) run inside an embedded QuickJS interpreter… For legacy Pi compatibility, JS/TS entrypoints are loaded directly with no manual conversion step."* A custom `note` tool + turn-boundary handler looks IDENTICAL to TS Pi:
```javascript
export default function activate(pi) {
  pi.tool({
    name: "note",
    description: "Persist a flywheel note",
    schema: { /* json schema */ },
    run: async (input) => ({ content: [{type:"text", text:"ok"}] })
  });
  pi.on("turn_end", async (ev) => { /* boundary work */ });
}
```
Hostcalls (`pi.tool`, `pi.exec`, `pi.http`, `pi.session`, `pi.ui`, `pi.events`, `pi.log`) marshal across an mpsc channel to the Rust host. Caveats: no Node runtime → `worker_threads`, native addons, server sockets BLOCKED. Second extension lane: `*.native.json` Rust extensions; third (experimental): WIT/WASM via opt-in `wasmtime`. **Default stays JS/TS.** This kills the strongest objection.

## 4. Maturity/activity
First commit 2026-02-02 (~4 mo old). Last 2026-05-29. **~30 commits/day** average. v0.1.16, semver discipline gated by `cargo-semver-checks` CI. 1,083 stars / 130 forks. CHANGELOG documents real shipping discipline (opt-in feature flags, evidence-backed perf claims, fail-closed regressions). Recent: configurable provider-aware request timeout, dynamic model fetch with TTL cache, Copilot device flow, auto-switch active model when it loses credentials. README has an evidence-gate disclaimer for benchmark numbers — re-derive locally before quoting.

## 5. License/lang/deps/build
- **License: MIT + Rider** (custom; `LICENSE` 3.9 KB; **diligence item**).
- Rust edition **2024, nightly required** (rust-toolchain.toml). MSRV 1.85.
- Deps: `asupersync 0.3.2` (Jeffrey's runtime), `crossterm 0.29`, Charm ports 0.2.0, `rich_rust 0.2.0`, `rquickjs 0.11` (QuickJS), `swc_*` for TS parsing, `clap 4.5`, `wasmtime 41` (opt-in), `sqlmodel-sqlite 0.2.2`, `ring 0.17`, `tikv-jemallocator` (opt-in).
- Build: pure-Rust prebuilt on Linux/macOS; `cargo install pi_agent_rust` OR `curl … install.sh | bash`. **No Node, no Python.**

## 6. Why Rust here, concretely
Not raw speed (model latency dominates) — the wins are:
- **Single 21 MiB static binary** — operationally aligns with harmonik (Go, also single binary). Drop into `/usr/local/bin/`, no runtime.
- **<100 ms startup** vs ~500ms-2s for Node agents — matters for short-lived child agents per bead.
- **<50 MB idle memory** vs 200 MB+ — matters at 7-10 concurrent (per the dispatch-volume feedback).
- **`asupersync` structured concurrency** — child-task cleanup is deterministic; aligns with "every spawned thing has an owner that reaps it." TS Pi has historically had orphaned-future bugs.
- **`#![forbid(unsafe_code)]` + capability-gated hostcalls** — materially stronger security than TS Pi (two-stage `exec` mediation, trust lifecycle, kill-switch). For an unattended push-capable agent, this matters.
- **Crates.io + `cargo-semver-checks` SDK discipline** — easier to pin long-term than "whatever pi-coding-agent exports today."

## 7. Honest tradeoff
| Dim | TS Pi | pi_agent_rust |
|---|---|---|
| Extension authoring | JS/TS in `~/.pi`, edit-reload | JS/TS in `~/.pi`, runs in QuickJS (no Node — worker_threads/native addons/server sockets blocked) |
| Hook names | Canonical from Mario | Differ (`turn_start`/`turn_end`/`stop` vs `shouldStopAfterTurn`/`prepareNextTurn`) — thin translation layer |
| API completeness | Reference impl | 91.9% conformance (205/223); 4 known failure buckets documented |
| TUI | pi-tui (TS) | Charm-stack port — aligns with NTM's visual idiom |
| Ops shape | Need Node + npm | Single binary, like harmonik |
| Engineering rigor | Mature, smaller surface | Aggressive (3.5k commits/4mo), large surface, evidence-gated CI, strong security posture |
| Risk | Known surface, what you see is what you get | Newer; LICENSE rider; 8.1% corpus gap may bite a hook we need; transformContext-equivalent path needs confirmation |
| Blessing | Original | "With Mario's blessing" — not hostile fork |

Asymmetric risks: TS Pi = "slower/fatter/weaker security" (operational); Rust Pi = "one of the 18 corpus failures or two unconfirmed-parity items is exactly what we need" (build-blocker). Mitigation: 1-week shake-down writing the 4-5 specific flywheel extensions against Rust Pi before commit.

## 8. Sibling repos (Jeffrey's converging ecosystem)
- **`flywheel_gateway`** (1,494 ⭐, TS) — *"SDK-first orchestration platform for managing AI coding agent fleets"* — **Jeffrey already has a "flywheel orchestrator" concept. READ THIS BEFORE WE DESIGN OURS.**
- **`agentic_coding_flywheel_setup`** (1,494 ⭐, Shell) — bootstrap for the multi-agent dev environment; pi_agent_rust is likely a component.
- **`destructive_command_guard`** (1,080 ⭐), **`mcp_agent_mail`** (1,965 ⭐), **`beads_viewer`** (1,554 ⭐), **`beads_rust`** (920 ⭐) — same author, used together. Beads is already harmonik's task ledger. Direct evidence the Rust ecosystem here is converging.
- **`asupersync`** (179 ⭐) — the runtime pi_agent_rust depends on; capability-based context, deterministic replay. Worth learning if we go Rust.
- **`coding_agent_session_search`** (799 ⭐) and **`cross_agent_session_resumer`** (82 ⭐) — both Rust, both consume pi_agent_rust's JSONL session format. Confirms format as de-facto interchange.
- No standalone "agent supervisor" repo — supervisor concerns live inside pi_agent_rust itself.

## Verdict (restated)
`pi_agent_rust` is a credible flywheel substrate; the "Rust = compiled extensions" objection is wrong (extensions are JS/TS in `~/.pi/agent/extensions/` via embedded QuickJS); recommend a **1-week shake-down** porting the 4-5 flywheel extensions we'd actually write, with explicit gates on (a) `session_before_compact`-equivalent hook, (b) `transformContext`-style message rewrite, (c) the LICENSE rider. If all three pass, prefer pi_agent_rust over TS Pi on ops-shape and security grounds.

## Sources
github.com/Dicklesworthstone/pi_agent_rust: README, Cargo.toml, AGENTS.md, CHANGELOG, docs/{extension-architecture, ext-compat, sdk, rpc, session, skills, settings, EXTENSION_SAMPLE}.md, docs/schema/{extension_protocol.json, extension-api-matrix.json}, docs/wit/extension.wit, src/ listing. Also: github.com/Dicklesworthstone/{flywheel_gateway, agentic_coding_flywheel_setup, destructive_command_guard, mcp_agent_mail, beads_rust, asupersync, coding_agent_session_search, cross_agent_session_resumer}.
