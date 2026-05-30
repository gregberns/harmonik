// circuit-breaker.ts — reaction-rate circuit breaker (CL-091)
//
// Tracks own reaction rate (turns/min) over a sliding window.
// Sustained rate > threshold → trips the breaker; emits flywheel_circuit_tripped.
// Loop enters `circuit-tripped` until operator runs `harmonik supervise resume`.
//
// Default: 10 reactions/min over a 60-second sliding window.

import { appendFileSync, mkdirSync } from "node:fs";
import { join } from "node:path";

export interface CircuitBreakerConfig {
  /** Max reactions per minute before the breaker trips. Default: 10. */
  thresholdPerMin: number;
  /** Sliding window size in ms. Default: 60000 (1 min). */
  windowMs: number;
  /** Path to events.jsonl for operator observability. */
  eventsFile: string;
}

export type BreakerState = "closed" | "tripped";

function emitEvent(eventsFile: string, payload: Record<string, unknown>): void {
  try {
    const dir = eventsFile.replace(/\/[^/]+$/, "");
    mkdirSync(dir, { recursive: true });
    appendFileSync(eventsFile, JSON.stringify(payload) + "\n");
  } catch {
    // Best-effort; never throw from an event emitter.
  }
}

export class CircuitBreaker {
  private config: CircuitBreakerConfig;
  private reactionTimestamps: number[] = [];
  private state: BreakerState = "closed";

  constructor(config: Partial<CircuitBreakerConfig> & { eventsFile: string }) {
    this.config = {
      thresholdPerMin: config.thresholdPerMin ?? 10,
      windowMs: config.windowMs ?? 60_000,
      eventsFile: config.eventsFile,
    };
  }

  /**
   * Record a reaction (turn start / event processed).
   * Returns true if the breaker just tripped or is already tripped.
   */
  recordReaction(): boolean {
    if (this.state === "tripped") return true;

    const now = Date.now();
    this.prune(now);
    this.reactionTimestamps.push(now);

    const rate = this.currentRate();
    if (rate > this.config.thresholdPerMin) {
      this.state = "tripped";
      emitEvent(this.config.eventsFile, {
        type: "flywheel_circuit_tripped",
        ts: now,
        rate_per_min: rate,
        threshold_per_min: this.config.thresholdPerMin,
        window_ms: this.config.windowMs,
        reaction_count: this.reactionTimestamps.length,
      });
      return true;
    }

    return false;
  }

  /** True when breaker is tripped. */
  isTripped(): boolean {
    return this.state === "tripped";
  }

  /** Reset the breaker (operator-triggered resume). */
  reset(): void {
    this.state = "closed";
    this.reactionTimestamps = [];
  }

  getState(): BreakerState {
    return this.state;
  }

  /** Current rate in reactions/min over the sliding window. */
  currentRate(): number {
    const now = Date.now();
    this.prune(now);
    if (this.reactionTimestamps.length === 0) return 0;
    const windowMin = this.config.windowMs / 60_000;
    return this.reactionTimestamps.length / windowMin;
  }

  private prune(now: number): void {
    const cutoff = now - this.config.windowMs;
    while (this.reactionTimestamps.length > 0 && this.reactionTimestamps[0]! < cutoff) {
      this.reactionTimestamps.shift();
    }
  }
}
