# Dimension 4 — codex auth & billing (the credit-burn landmine)

> Web-sourced; codex CLI **v0.137.0** (2026-06-04). Every claim cited. This mirrors harmonik's
> hard-won posture: local `claude` CLI bills the **Max subscription**; the API path bills a separate
> **credit pool** (the burn this project fought via `ANTHROPIC_API_KEY`-in-`.env` stripping).

## 1. Auth modes — codex supports BOTH

`codex login` authenticates against a ChatGPT account, an API key, or an access token
(developers.openai.com/codex/auth).
- **ChatGPT login (subscription):** `codex login` (no flags) → browser OAuth ("Sign in with ChatGPT
  is the default when no valid session exists"). Headless variant: `codex login --device-auth`
  (device-code flow). Works for Plus / Pro / Team / Business / Enterprise.
- **API key (credit pool):** `printenv OPENAI_API_KEY | codex login --with-api-key` (reads from
  stdin). Docs recommend it "for programmatic workflows, such as CI/CD."
- **Credential storage:** plaintext `~/.codex/auth.json` under `$CODEX_HOME` (default `~/.codex`),
  OR the OS keyring; config `cli_auth_credentials_store = "file"|"keyring"|"auto"`. "Treat
  `~/.codex/auth.json` like a password."
Sources: developers.openai.com/codex/auth ; cli/reference.

## 2. Billing surface per mode — THE KEY ANSWER

- **ChatGPT login → bills the SUBSCRIPTION.** "Codex uses... included ChatGPT plan credits";
  subscription-rate-limited; "fast mode" + included credits require ChatGPT sign-in.
- **API key → bills the OpenAI API CREDIT/USAGE pool.** "OpenAI bills API key usage through your
  OpenAI Platform account at standard API rates... instead of included ChatGPT plan credits."

⚠️ **Wart:** "Sign in with ChatGPT" historically **auto-generated an API key** labeled "Codex CLI
(auto-generated)" that users reported *still routed to API billing* (#2000, v0.19.0). So
"subscription login" and "an org API key exists" are NOT mutually exclusive — audit the org for an
auto-generated key and confirm `/status` shows **plan credits**, not an org key.
Sources: developers.openai.com/codex/auth ; issue #2000.

## 3. Subscription path headless? — Yes, with caveats

Login persists `~/.codex/auth.json`, reused across runs; `codex exec` "inherits authentication from
existing credentials stored via `codex login`." Tokens auto-refresh before expiry. For a daemon: log
in once interactively (or `--device-auth`), then `codex exec` reuses the token file.
**Caveat:** refresh requires the daemon's `$CODEX_HOME`/`~/.codex` to be the **same** one that was
logged in; a sandboxed/empty HOME breaks it silently. Sources: auth doc ; cli/reference.

## 4. Env-var leak risk — the harmonik-shaped landmine (VERSION-DEPENDENT)

- **Older codex (v0.21.0):** YES — silently picked up `OPENAI_API_KEY` from the env (incl.
  shell-auto-loaded `.env`) and used it OVER an active ChatGPT login, while `/status` still showed
  "Signed in with ChatGPT (Plan: Plus)" — exactly the silent-API-billing failure mode (#2341).
- **Current released codex (v0.30+, 2026):** For **interactive** sessions, a bare `OPENAI_API_KEY`
  env var does **not** auto-override a stored ChatGPT login — stored login wins; you must explicitly
  `codex login --with-api-key` (#3286).
- **`codex exec` (non-interactive — the daemon path):** a third-party reference reports `codex exec`
  honors `CODEX_API_KEY` (and historically `OPENAI_API_KEY`) from the env where interactive ignores
  it. **This is exactly the path harmonik would invoke and the riskiest.** **UNCERTAINTY:** OpenAI's
  official docs do not document `codex exec` env-var precedence; this rests on a third-party source
  and changed across versions → **test empirically on the pinned codex version.**
- **Force the subscription path:** config `forced_login_method = "chatgpt"` (values
  `"chatgpt"|"api"`), optionally `forced_chatgpt_workspace_id`.
Sources: issue #2341 ; issue #3286 ; auth doc ; claw.aguidetocloud.com (exec/CODEX_API_KEY, 2026-05-15).

## RECOMMENDATION (for the change-spec / integration passes)

**(a) Billing path:** Use the **ChatGPT-login / subscription path** — matches the harmonik goal of
billing the subscription not the API pool (same posture as the Max-subscription decision for Claude).

**(b) How to invoke to guarantee subscription:**
1. Log in once: `codex login` (or `--device-auth` on a headless box); confirm `codex login status` /
   `/status` shows a **ChatGPT plan**, not an org API key.
2. Pin `~/.codex/config.toml`: `forced_login_method = "chatgpt"` (fail-closed to subscription);
   verify it's honored by `codex exec` on the pinned version before trusting it.
3. Daemon-spawned runs use `codex exec` with the same `$CODEX_HOME` (`~/.codex`) so token +
   refresh work.

**(c) The daemon env guard the codex adapter MUST apply (defense-in-depth — do all three, because
`exec` precedence is undocumented/version-variable):**
- **STRIP `OPENAI_API_KEY` AND `CODEX_API_KEY`** from the spawned child env (allowlist env, don't
  inherit `.env`). Direct analog of the `ANTHROPIC_API_KEY` guard
  (`claudehandler_chb006_024.go:196-204,246-253,292-296`) — the single most important guard. The
  codex adapter's `LaunchSpec.env` must apply the SAME strip-then-empty-override pattern claude uses.
- **Set `forced_login_method = "chatgpt"`** in config as a belt to the env-stripping suspenders
  (the daemon should materialize/verify this in `$CODEX_HOME/config.toml`, analogous to how it
  materializes `.claude/settings.json`).
- **Post-spawn assertion:** run `codex login status` / parse `/status` and **fail-closed if it does
  not report a ChatGPT plan**. Don't rely on env-absence alone — codex has shipped silent-override
  before (#2341) and may regress.

**Flagged uncertainties (carry into the spec as MUST-TEST items):**
1. Whether current `codex exec` still auto-consumes `OPENAI_API_KEY`/`CODEX_API_KEY` is **not in
   official docs** — empirically test on the pinned codex version before production.
2. The ChatGPT-login auto-generated-API-key behavior (#2000) means audit the OpenAI org for a
   "Codex CLI (auto-generated)" key.
3. `$CODEX_HOME` must be stable and writable for token refresh; the daemon must NOT spawn codex with
   an empty/sandboxed HOME.

Sources: developers.openai.com/codex/auth · cli/reference · changelog (v0.137.0) · issues
#2341 / #3286 / #2000 · claw.aguidetocloud.com (2026-05-15) · OpenAI community thread.
