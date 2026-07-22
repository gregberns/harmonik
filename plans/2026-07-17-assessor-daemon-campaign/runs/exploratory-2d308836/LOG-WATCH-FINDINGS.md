# S6 — Adversarial Log Watcher findings ()

## Baseline (daemon pid 6632 @ boot)
| metric | value |
|---|---|
| fd_count (lsof) | 13 |
| thread_lines (ps -M) | 16 |
| h-assessor procs (pgrep) | 4 |
| captured | 2026-07-18T16:49:11Z |

## FAIL-set: level=error / panic / fatal / goroutine|WaitGroup|fd growth / orphan PID / held lease
(watcher appends below; empty = none observed)

## Watcher heartbeats
### S6_HIT 2026-07-18T16:49:47Z (daemon pid=6632)
```
17:time=2026-07-18T09:49:14.668-07:00 level=ERROR msg="supervisor-watchdog: revival cap reached — giving up" max_revives=3
```
- HB 2026-07-18T16:49:47Z cyc=1 daemon=UP pid=6632 fd=13 th=16
- HB 2026-07-18T16:50:07Z cyc=2 daemon=UP pid=6632 fd=13 th=16
- HB 2026-07-18T16:50:27Z cyc=3 daemon=UP pid=6632 fd=13 th=16
- HB 2026-07-18T16:50:47Z cyc=4 daemon=UP pid=6632 fd=13 th=16
- HB 2026-07-18T16:51:07Z cyc=5 daemon=UP pid=6632 fd=13 th=16
- HB 2026-07-18T16:51:27Z cyc=6 daemon=UP pid=6632 fd=13 th=16
- HB 2026-07-18T16:51:47Z cyc=7 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T16:52:08Z cyc=8 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T16:52:28Z cyc=9 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T16:52:48Z cyc=10 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T16:53:08Z cyc=11 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T16:53:28Z cyc=12 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T16:53:48Z cyc=13 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T16:54:08Z cyc=14 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T16:54:28Z cyc=15 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T16:54:48Z cyc=16 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T16:55:08Z cyc=17 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T16:55:28Z cyc=18 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T16:55:48Z cyc=19 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T16:56:08Z cyc=20 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T16:56:28Z cyc=21 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T16:56:48Z cyc=22 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T16:57:09Z cyc=23 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T16:57:29Z cyc=24 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T16:57:49Z cyc=25 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T16:58:09Z cyc=26 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T16:58:29Z cyc=27 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T16:58:49Z cyc=28 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T16:59:09Z cyc=29 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T16:59:29Z cyc=30 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T16:59:49Z cyc=31 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T17:00:09Z cyc=32 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T17:00:29Z cyc=33 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T17:00:49Z cyc=34 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T17:01:09Z cyc=35 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T17:01:29Z cyc=36 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T17:01:49Z cyc=37 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T17:02:09Z cyc=38 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T17:02:30Z cyc=39 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T17:02:50Z cyc=40 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T17:03:10Z cyc=41 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T17:03:30Z cyc=42 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T17:03:50Z cyc=43 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T17:04:10Z cyc=44 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T17:04:30Z cyc=45 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T17:04:50Z cyc=46 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T17:05:10Z cyc=47 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T17:05:30Z cyc=48 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T17:05:50Z cyc=49 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T17:06:10Z cyc=50 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T17:06:30Z cyc=51 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T17:06:50Z cyc=52 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T17:07:11Z cyc=53 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T17:07:31Z cyc=54 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T17:07:51Z cyc=55 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T17:08:11Z cyc=56 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T17:08:31Z cyc=57 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T17:08:51Z cyc=58 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T17:09:11Z cyc=59 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T17:09:31Z cyc=60 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T17:09:51Z cyc=61 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T17:10:11Z cyc=62 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T17:10:31Z cyc=63 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T17:10:51Z cyc=64 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T17:11:12Z cyc=65 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T17:11:32Z cyc=66 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T17:11:52Z cyc=67 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T17:12:12Z cyc=68 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T17:12:32Z cyc=69 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T17:12:52Z cyc=70 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T17:13:12Z cyc=71 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T17:13:32Z cyc=72 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T17:13:52Z cyc=73 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T17:14:12Z cyc=74 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T17:14:32Z cyc=75 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T17:14:52Z cyc=76 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T17:15:12Z cyc=77 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T17:15:33Z cyc=78 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T17:15:53Z cyc=79 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T17:16:13Z cyc=80 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T17:16:33Z cyc=81 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T17:16:53Z cyc=82 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T17:17:13Z cyc=83 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T17:17:33Z cyc=84 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T17:17:53Z cyc=85 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T17:18:13Z cyc=86 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T17:18:33Z cyc=87 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T17:18:53Z cyc=88 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T17:19:13Z cyc=89 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T17:19:33Z cyc=90 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T17:19:54Z cyc=91 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T17:20:14Z cyc=92 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T17:20:34Z cyc=93 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T17:20:54Z cyc=94 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T17:21:14Z cyc=95 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T17:21:34Z cyc=96 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T17:21:54Z cyc=97 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T17:22:14Z cyc=98 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T17:22:34Z cyc=99 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T17:22:54Z cyc=100 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T17:23:14Z cyc=101 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T17:23:34Z cyc=102 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T17:23:54Z cyc=103 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T17:24:14Z cyc=104 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T17:24:35Z cyc=105 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T17:24:55Z cyc=106 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T17:25:15Z cyc=107 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T17:25:35Z cyc=108 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T17:25:55Z cyc=109 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T17:26:15Z cyc=110 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T17:26:35Z cyc=111 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T17:26:55Z cyc=112 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T17:27:15Z cyc=113 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T17:27:35Z cyc=114 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T17:27:55Z cyc=115 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T17:28:15Z cyc=116 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T17:28:35Z cyc=117 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T17:28:55Z cyc=118 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T17:29:15Z cyc=119 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T17:29:36Z cyc=120 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T17:29:56Z cyc=121 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T17:30:16Z cyc=122 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T17:30:36Z cyc=123 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T17:30:56Z cyc=124 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T17:31:16Z cyc=125 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T17:31:36Z cyc=126 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T17:31:56Z cyc=127 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T17:32:16Z cyc=128 daemon=UP pid=6632 fd=13 th=17
- HB 2026-07-18T17:32:36Z cyc=129 daemon=UP pid=6632 fd=13 th=17
