# 04-Design / c1-clockport — the daemon ClockPort slice (C1) [Set A]

> Slice doc; full design in `ports-design.md` §4; pins: 00-decisions M3-D4.
> Decision: reuse `substrate.ClockPort` as a new `workLoopDeps.clock` field
> (SystemClock prod / FakeClock tests); migrate ONLY the 23 run-path sites
> (workloop ×8, reviewloop ×8, dot_cascade ×7 — 03-research/workloop-ports §4);
> the five `time.After` selects become shell timer deadlines as the Dispatch
> machine absorbs them (M3-D3); OUTER sites stay for M5. Decompose C1 answers:
> (a) deps field now, riding into RunPorts.Clock; (b) no existing daemon clock
> seam to reconcile; (c) timer-EVENTS = the After selects + ready/ack/reap
> bounds, plain Now/Since for interval reads. Task RT1; mirrors P1 T5 (30963aa0).
