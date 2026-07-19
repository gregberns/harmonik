// Package keepertwin is the keeper vertical's trace-driven digital twin (T9,
// codename:session-restart-substrate; measurement-design §2).
//
// The frozen baseline records keeper OUTPUTS (durable session_keeper_* events),
// not keeper INPUTS (gauge readings, handoff-file appearance, SID flips) — so
// the recorded corpus cannot be fed directly into the pure Step reactor, whose
// event vocabulary is the input set (GaugeTick, NonceObserved, ModelDone,
// SessionChanged, TimerFired, …). This package closes that gap:
//
//	summary.json ──► SynthesizeStimulus ──► []keeper.Event  (virtual-time-stamped)
//	 (outcome)        (single decision           │ EncodeStimulus (JSONL)
//	                   table, §2.4)               ▼
//	                              substrate.Twin[keeper.Event]   (D2/D3 faults)
//	                              + keeperCodec (ReplayCodec[keeper.Event])
//	                                              │ Events(ctx)
//	                                              ▼
//	                              keeper.Cycle.Run → recording Effector
//	                                              ▼
//	                              emitted events ──compare──► corpus goldens
//
// Routing the synthesized stimulus through substrate.Twin (not a bespoke
// source) means the four generic fault modes apply to keeper stimuli with zero
// keeper-specific fault code (measurement-design §2.2, §5).
//
// The corpus lives at testdata/keeper-cycles/baseline-2026-07-13/ (507 cycles,
// composite (agent_name, cycle_id) join key — D7), built by
// scripts/extract-keeper-corpus.py from the frozen baseline log.
package keepertwin
