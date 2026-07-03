# Transcript extraction manifest (INDEX)

Window (local UTC-7): 2026-06-24 21:00 → 2026-06-25 09:30
Window (UTC):        2026-06-25T04:00Z → 2026-06-25T16:30Z
Projects dir: `/Users/gb/.claude/projects/-Users-gb-github-harmonik`
Sessions: 16

## Sessions

| agent | session8 | local window | utc window | conf | u/a/tool | wakes | mon | commsOut | chain |
|---|---|---|---|---|---|---|---|---|---|
| ctx-watchdog | a4ca2f7c | 16:02→09:45 | 23:02Z→16:45Z | 1.0 | 81/274/147 | 40 | 0 | 38 | cold-boot→live |
| captain | 08a94090 | 18:44→23:11 | 01:44Z→06:11Z | 0.69 | 15/194/80 | 0 | 1 | 5 | cold-boot→823adb4d |
| gurney | 083fd92b | 18:55→23:26 | 01:55Z→06:26Z | 0.99 | 15/145/60 | 0 | 0 | 7 | cold-boot→0e355d65 |
| admiral | 7b1b386c | 19:01→22:13 | 02:01Z→05:13Z | 0.88 | 21/112/43 | 0 | 5 | 4 | cold-boot→196c5dde |
| admiral | 196c5dde | 22:13→23:08 | 05:13Z→06:08Z | 0.87 | 19/124/48 | 0 | 2 | 3 | 7b1b386c→0606ad8a |
| admiral | 0606ad8a | 23:08→02:26 | 06:08Z→09:26Z | 0.85 | 20/126/45 | 0 | 4 | 5 | 196c5dde→b9770237 |
| captain | 823adb4d | 23:11→00:45 | 06:11Z→07:45Z | 0.73 | 16/126/57 | 0 | 2 | 4 | 08a94090→c829a861 |
| gurney | 0e355d65 | 23:26→04:04 | 06:26Z→11:04Z | 1.0 | 34/202/73 | 0 | 11 | 10 | 083fd92b→6428bb56 |
| captain | c829a861 | 00:45→00:51 | 07:45Z→07:51Z | 0.99 | 6/51/26 | 0 | 0 | 2 | 823adb4d→7eb0e0a8 |
| captain | 7eb0e0a8 | 00:51→05:30 | 07:51Z→12:30Z | 0.97 | 30/191/61 | 6 | 16 | 9 | c829a861→35f2b340 |
| watch | 6cddfbaf | 01:14→09:43 | 08:14Z→16:43Z | 0.98 | 82/686/347 | 2 | 37 | 37 | cold-boot→live |
| admiral | b9770237 | 02:26→08:13 | 09:26Z→15:13Z | 0.96 | 36/122/46 | 0 | 0 | 7 | 0606ad8a→f49808b2 |
| gurney | 6428bb56 | 04:04→08:18 | 11:04Z→15:18Z | 1.0 | 28/220/85 | 0 | 2 | 7 | 0e355d65→cff2ea04 |
| captain | 35f2b340 | 05:30→09:49 | 12:30Z→16:49Z | 0.81 | 39/242/83 | 0 | 26 | 13 | 7eb0e0a8→live |
| admiral | f49808b2 | 08:13→09:46 | 15:13Z→16:46Z | 0.93 | 12/59/22 | 1 | 2 | 2 | b9770237→live |
| gurney | cff2ea04 | 08:18→09:03 | 15:18Z→16:03Z | 0.95 | 12/98/41 | 0 | 5 | 4 | 6428bb56→live |

## Per-agent keeper chains

- **admiral**: cold-boot → 7b1b386c → 196c5dde → 0606ad8a → b9770237 → f49808b2 → live
- **captain**: cold-boot → 08a94090 → 823adb4d → c829a861 → 7eb0e0a8 → 35f2b340 → live
- **ctx-watchdog**: cold-boot → a4ca2f7c → live
- **gurney**: cold-boot → 083fd92b → 0e355d65 → 6428bb56 → cff2ea04 → live
- **watch**: cold-boot → 6cddfbaf → live
