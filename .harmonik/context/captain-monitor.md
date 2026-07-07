# Captain 15-min self-monitor (operator-requested anti-stall guardrail)

The operator asked for a Claude-Code monitor that fires every 15 min so the captain
never stalls on comms (it kept missing admiral/operator directives because the recv
cursor is consumed by Monitor watchers). Implement as a **CronCreate** job.

**CronCreate is SESSION-ONLY** — it dies when the captain session ends (every keeper
/clear cycle). **Re-create it first thing on every captain resume**, verbatim:

- **Schedule (cron, 5-field local):** `7,22,37,52 * * * *`  (every 15 min, offset off the :00/:15/:30/:45 herd)
- **recurring:** true

**Prompt (paste verbatim):**

```
[15-MIN CAPTAIN MONITOR — auto-fired] Run the captain health/progress sweep. Be CONCISE; only surface to the operator if something genuinely needs their decision — otherwise a 1-2 line status is enough.

1. DRAIN COMMS (the stall guard — do this FIRST): `harmonik comms join --name captain` (presence lapses), then `harmonik comms recv --agent captain --json`. Because Monitor watchers advance the cursor, ALSO scan the raw ledger for anything I might have missed: grep `.harmonik/events/events.jsonl` for recent messages where to=captain or from in {admiral,operator} (last ~15). ACT on any directive found — do not just note it.
2. CHECK WHERE WORK IS: `harmonik comms who --json` (alia + watch + admiral present?); alia progress (br show hk-hcrvb children; is T1 hk-h6fej landed/closed? T2 hk-1yxhh?); `harmonik queue list` + `harmonik ps` for stuck/failed; watch alive? daemon/tunnel up?
3. KEEP INITIATIVES PROGRESSING:
   - If T1 (hk-h6fej) has LANDED and crew `hawat` is not yet staffed → write hawat's mission and staff it on hk-lgykq (P1 daemon-core fix; codex/claude NOT pi; dogfoods against alia's harness). This is the pinned trigger.
   - If T2 (hk-1yxhh) landed green → tell admiral (their Phase-2 twin handoff trigger).
   - If alia is wedged/idle/presence-stale → nudge it (tmux send-keys to its pane) or diagnose; don't let it stall.
   - If watch is stalled → one nudge (known hk-5266t loop; benign).
   - Any new operator/admiral directive → execute it (that's the whole point of this monitor).
4. If nothing changed and everything's progressing, reply with one line: "monitor tick: all lanes moving, nothing needs you." Keep it lean — conserve the ~98% Claude cap.
```

Note: this is a WITHIN-session guardrail. The daemon-native `internal/schedule`/cron and
the launchd `ops-monitor` are separate, durable probes; this one is the captain's own
comms-drain loop and must be re-armed per session.
