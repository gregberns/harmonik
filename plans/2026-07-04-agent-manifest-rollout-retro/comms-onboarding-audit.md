# Agent-manifest comms-onboarding audit (2026-07-04, admiral-commissioned)

Root theme: the manifest landed CONTENT (type folders, `agent brief` command) but not DELIVERY
(crew boot path unwired, shared _skills empty, presence-refresh instruction never written).

## Mechanism ground-truth (from source)
- Presence "online" in `comms who` = EffectiveLastSeen within ~120s (internal/presence/presence.go:133-224).
- Refreshers: `comms join`/`leave`, ANY `comms send` (implicit refresh), and OPENING a recv/--follow/subscribe (ONE beat at connect).
- NOT refreshers: holding an idle --follow open; RECEIVING messages. So >120s without a send = reads OFFLINE despite a live stream. THIS is the recurring failure.

## Findings / fixes (ranked)
1. **[ROOT] No sub-120s presence-refresh instruction anywhere.** Add to canonical .claude/skills/agent-comms/SKILL.md + echo one line into each operating.md: "Presence expires ~120s; idle --follow does NOT refresh it; re-run `harmonik comms join --name "$HARMONIK_AGENT"` on a <=90s timer (or send traffic more often). Receiving does NOT refresh."
2. **[ROOT] Shared _skills bodies are EMPTY** (.harmonik/agents/_skills/{agent-comms,beads-cli,harmonik-dispatch}/ = .gitkeep). `agent brief` delivers no comms body. Populate from .claude/skills/... OR repoint the manifests' bare refs to path refs.
3. **`start crew` never wired to `agent brief`** (only `start captain`, T10/e6f76683). Crews (duncan) boot on old mission files, never pull operating.md. Wire crewstart seed to emit `harmonik agent brief`; update crew mission template.
4. **Captain manifest dropped its {source: comms} wake trigger** (captain/manifest.yaml:18 has only a 6h cron; draft had comms+escalations). Restore so inbound wakes captain if the boot stream dies.
5. **Captain & admiral lack dedupe-on-event_id (N3) + "keep --follow armed"** lines that crew/watch have. Add both.
6. **crew manifest never_emits blanked on landing** (crew/manifest.yaml:33 = [] ; draft had [queue_submit:main, crew_start]). Restore the marker guard.

Clean: no retired file-outbox references; all CLI verbs/flags valid. Failures are missing-cadence + unpopulated shared skill + unwired crew boot, not wrong commands.
