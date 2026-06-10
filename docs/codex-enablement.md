# Codex (2nd AI engine) — enablement plan

> How to get OpenAI's Codex working as a second implementer engine alongside Claude.
> Investigated 2026-06-10. Codex is INSTALLED but UNPROVEN (never run against a real
> account). Operator-gated on a ChatGPT subscription.

## Status

- **Installed:** `brew install codex` (Homebrew cask) → `/opt/homebrew/bin/codex`
  v0.139.0. All no-cost surfaces work: `codex --help|--version|doctor`,
  `codex login status` (→ "Not logged in"). Nothing run can incur cost until login.
- **Blocker 1 (code):** the harness hard-codes `-a never`, which codex 0.139.0
  rejects (`unexpected argument '-a'`). Filed `hk-n5lfz` (P1) — must land before any
  real run. Fix: drop `-a never`, keep `--sandbox workspace-write`.
- **Blocker 2 (operator):** a one-time ChatGPT login (see below). Codex bills via a
  **ChatGPT subscription (OAuth), NOT an API key** — the harness deliberately strips
  `OPENAI_API_KEY`/`CODEX_API_KEY` and fail-closes (`codexbillingguard.go`). So do
  NOT send an API key; it would be refused.

## Operator path (when you have a ChatGPT plan that includes Codex — Plus/Pro/Business)

1. **You sign up** for the plan. Send NO credentials over chat.
2. **One-time interactive login** — `codex login` opens a browser OAuth flow.
   Since you're mobile-remote (not on the box), the agent route is:
   - I launch `codex login` in a tmux pane on the box, read the printed auth URL,
     and relay it to you. You open it on your phone, approve, and paste the code
     back; I feed it to the pane. (Alternatively `codex login --with-access-token`
     if you can obtain a ChatGPT access token — it writes the OAuth token set to
     `~/.codex/auth.json` without an API key, which passes the guard.)
3. **Credential lands** at `~/.codex/auth.json` (default `CODEX_HOME=$HOME/.codex`).
   Must have an EMPTY/absent `OPENAI_API_KEY` field (a populated one = API-pool
   billing → the guard denies). Verify: `codex login status` flips to logged-in.

## Then I exercise it end-to-end (no operator needed)

1. Land `hk-n5lfz` (the `-a never` fix) — dispatch it to a crew/daemon.
2. `go install ./cmd/harmonik`; restart the daemon (stamp a new version tag).
3. Submit a trivial bead labeled `harness:codex` (or run the daemon with
   `--default-harness codex`).
4. Watch `.harmonik/events/events.jsonl` for `codex_billing_guard`
   (materialized → allowed) then `run_started` → `run_completed`. First green run =
   codex proven.

## Wiring reference (for the implementer)

`internal/daemon/codexlaunchspec.go` (argv + env strip), `codexbillingguard.go`
(ChatGPT-only fail-closed), `harnessresolve.go` (label/flag selection),
`harnessregistry.go:44` (registered as `NewCodexHarness("","")` → CODEX_HOME defaults
to `$HOME/.codex`; there is no `--codex-home` flag, only `--codex-binary`).
