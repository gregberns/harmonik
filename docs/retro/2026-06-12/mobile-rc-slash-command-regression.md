# Mobile / Remote-Control Slash-Command Regression — 2026-06-12

**Status: CLOSED — root cause is UPSTREAM (Claude mobile app / remote-control channel). NOT harmonik, NOT our config, NOT the box CLI version.**

## Symptom
Client-side slash commands (`/clear`, `/context`) — the ones the Claude Code client
executes locally with **no model round-trip** — stopped executing when sent from the
**iOS mobile app (remote-control channel)** against the US box. Instead they are
forwarded to the model as plain user text (the model replies conversationally). The
operator (UTC+2) had driven sessions via the mobile app for ~2 weeks with these
commands working; they broke today.

## Onset (captain-transcript forensics)
Transcripts under `~/.claude/projects/-Users-gb-github-harmonik/*.jsonl` distinguish:
- **Executed (normal):** logged as `<command-name>/context</command-name>` — CLI ran it.
- **Bugged:** a plain `/context` user-text turn — it reached the model.

Timeline (UTC | UTC+2):
- Last clean `/context`: **14:05:07Z | 16:05** (ran fine 13:32–14:05Z).
- First bugged `/context`: **16:29:42Z | 18:29** (model replied "I'm at ~49% of the window…").
- → break occurred in the **14:05–16:29Z** window; **intermittent** thereafter
  (fires only on mobile-sent turns; clusters 16:29, 16:55, 17:01 `/clear`, 19:07, 21:24–21:34).

## Root cause: upstream channel, proven by elimination + an A/B on one session
1. Clean **keeper-free** session (`cleartest`, stripped `settings.json`) → still broke
   → NOT the keeper, NOT our hooks.
2. **Oldest box CLI** (`oldver`, 2.1.148 — the version from when it worked) → still broke
   → NOT the box CLI version. (A prior downgrade to 2.1.174 also didn't help.)
3. The same 2.1.148 binary worked 2 weeks ago, fails now → the changed variable is **off the box.**
4. **A/B on the identical `oldver` session:** `/clear` pasted via **local tmux** EXECUTED
   (pane reset to the welcome screen); the operator's `/context` via **mobile** went to the
   model as text. Same binary, settings, session — only the input channel differs.
5. No box-side change falls in the 14:05–16:29Z onset window (keeper-statusline rebuild
   13:36Z was *before* and `/context` worked after it; harmonik rebuild 17:10Z and claude
   2.1.176 at 20:37Z were *after*).

iOS App Store last release was ~5 days ago, so likely not a store update — but the app
may use React Native / **over-the-air code push**, which can change the remote-control
send path with no store update, consistent with an upstream change in the onset window.

## Workaround (until upstream fixes it)
Use the **local** channel for client-side commands: SSH into the box +
`tmux attach -t <session>`, then type `/clear` in the TUI (works). Or paste `/clear` into
the pane via local tmux (`load-buffer` → `paste-buffer -d` → `send-keys Enter`).

## Related / report
- claude-code GitHub issue **#29156** (slash commands sent as text over remote control).
- A bug report to Anthropic should include: onset 14:05–16:29Z 2026-06-12; repro = the same
  remote-controlled session executes `/clear` via local tmux but forwards it as text over the
  mobile/RC channel; reproduces on CLI 2.1.148 through 2.1.176 (so it is client/RC-side, not the CLI).

## Separately discovered (real, ours — distinct from the above)
The session-keeper injects `/session-handoff` + `Escape` + `/clear` into whatever session it
manages (incl. the captain the operator drives via mobile). Initially suspected as the cause,
then ruled out (above). Open design point: whether the keeper should ever type into a session
an operator is actively attached to. The keeper's dominant *failure* today was `handoff_timeout`
(fails at the handoff step, before `/clear`), not `/clear` execution — see `events.jsonl`.
