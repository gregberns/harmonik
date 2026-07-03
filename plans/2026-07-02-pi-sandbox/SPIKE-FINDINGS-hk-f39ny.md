# SPIKE FINDINGS — hk-f39ny (GATE) — pi-sandbox network + Go-CLI-TLS

**By:** leto · 2026-07-03 · macOS (Seatbelt backend) · srt = `@anthropic-ai/sandbox-runtime` (installed global, `/opt/homebrew/bin/srt`)
**Verdict: GATE CLEARED.** The mechanism works. No `enableWeakerNetworkIsolation` needed for v1.

## What was proven under the sandbox (all live, real daemon + real API)

| Test | Result |
|---|---|
| `br ready` | ✅ works (reads local `.beads` SQLite — no network) |
| `harmonik comms who` | ✅ works over the daemon **unix socket** via `network.allowUnixSockets` |
| OpenRouter call (`curl`, key from `~/.config/harmonik/openrouter.key`) | ✅ real completion, `gpt-5.4-mini`, PONG, cost logged |
| `node fetch` → openrouter | ✅ **status 200** with `NODE_USE_ENV_PROXY=1` |
| `gh api rate_limit` | ❌ `tls: failed to verify certificate: x509: OSStatus -26276` |

## The Go-CLI-TLS problem: root cause + why it doesn't block v1

**Root cause (confirmed):** srt MITM-proxies allowed HTTPS domains and injects its CA via env
(`SSL_CERT_FILE`, `NODE_EXTRA_CA_CERTS`, `GIT_SSL_CAINFO`, `CURL_CA_BUNDLE`, `CARGO_HTTP_CAINFO`).
Clients that honor those env vars — **curl, node, git** — trust the proxy cert and work. **Go binaries
on macOS ignore `SSL_CERT_FILE` (they use the system keychain)**, so `gh` fails cert verification.
`network.tlsTerminate.excludeDomains: [github.com]` did **not** fix it (experimental; did not flip mode).

**Why v1 is unaffected — map each network client to its path:**
- **Pi's model call** — `pi` is a **node script** (`#!/usr/bin/env node`, node v25). Node honors the injected
  CA (`NODE_EXTRA_CA_CERTS`) and, with env-proxy on, the proxy. → **works, no weakening.**
- **`br` / `harmonik comms` / `queue submit`** — talk to the **local daemon over the unix socket**
  (`allowUnixSockets`), not the internet. No TLS at all. → **works.**
- **`gh`** — the only Go-network client, and it is **not needed inside the sandbox**: the daemon (running
  *outside* the sandbox) owns push / promote / PR. Sandboxed Pi only **commits to its own worktree branch**
  (local git, no network). So `gh`'s TLS failure never gets exercised in v1.

## TLS DECISION (recommended)

**Adopt brief option (b), refined — keep `enableWeakerNetworkIsolation: false`:**
1. Local Go CLIs (`br`, `harmonik`) reach the daemon over the **unix socket** — no MITM, no TLS issue.
2. The model API is reached by **node (pi)**, which trusts the injected CA and uses the proxy.
3. **Do not run `gh` inside the sandbox** — remote git ops stay with the daemon outside the sandbox.
4. If a future need forces a *Go* binary onto a real remote TLS endpoint *inside* the sandbox, revisit
   `tlsTerminate.excludeDomains` (needs more investigation) or accept `enableWeakerNetworkIsolation: true`
   with the documented exfil caveat — **not needed now.**

v1 network mode stays **OPEN** (locked): FS boundary is the primary guarantee.

## Build-phase wiring note (carry into hk-p7smp / hk-rlxgx)

Pi's HTTP client must honor `HTTPS_PROXY` inside the sandbox. Either the OpenAI/OpenRouter SDK pi uses
honors it natively, or the sandboxed launch env must set **`NODE_USE_ENV_PROXY=1`** (proven to make node
fetch reach OpenRouter). **Verify pi's actual SDK behavior at build time**; if it doesn't auto-honor proxy,
inject `NODE_USE_ENV_PROXY=1` in the profile generator's launch env.

## Working srt settings recipe

Committed alongside: `srt-spike-settings.json` (the proven-good base). Real schema (differs from brief):
`filesystem.{denyRead,allowRead,allowWrite,denyWrite}` + `network.{allowedDomains,deniedDomains,allowUnixSockets,allowLocalBinding,tlsTerminate}`.
The profile generator (hk-p7smp) turns this + the run's worktree/git paths (§4 writable-set) into per-run JSON.
