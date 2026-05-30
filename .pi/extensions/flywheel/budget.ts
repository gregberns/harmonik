// budget.ts — per-day USD budget tracking with graceful-downgrade + hard halt (CL-090)
//
// Graceful-downgrade pattern:
//   80% → downgrade Opus → Sonnet
//   90% → downgrade Sonnet → Haiku (tier ≤ 2)
//   100% → halt; emit flywheel_budget_exhausted; loop enters budget-paused
//
// Daily reset is NOT automatic. Operator MUST `harmonik supervise resume`.
// Day boundary = local-midnight (operator TZ) at v0.1 — tracked as a date string.

import { appendFileSync, mkdirSync } from "node:fs";
import { join } from "node:path";
import type { RoutingConfig } from "./router.js";

export interface BudgetConfig {
  /** Daily cap in USD. Default: no limit (Infinity). */
  limitUsd: number;
  /** Path to events.jsonl for operator observability. */
  eventsFile: string;
}

export interface BudgetState {
  /** Total USD spent today. */
  spentUsd: number;
  /** ISO date string of the last reset (YYYY-MM-DD). */
  dayKey: string;
  /** True once the budget has been exhausted this day. */
  exhausted: boolean;
}

export interface SpendRecord {
  /** Approximate USD cost for this turn. */
  turnUsd: number;
  /** Model used. */
  model: string;
}

function todayKey(): string {
  return new Date().toISOString().slice(0, 10);
}

function emitEvent(eventsFile: string, payload: Record<string, unknown>): void {
  try {
    const dir = eventsFile.replace(/\/[^/]+$/, "");
    mkdirSync(dir, { recursive: true });
    appendFileSync(eventsFile, JSON.stringify(payload) + "\n");
  } catch {
    // Best-effort; never throw from an event emitter.
  }
}

export class BudgetTracker {
  private config: BudgetConfig;
  private state: BudgetState;

  constructor(config: Partial<BudgetConfig> & { eventsFile: string }) {
    this.config = {
      limitUsd: config.limitUsd ?? Infinity,
      eventsFile: config.eventsFile,
    };
    this.state = {
      spentUsd: 0,
      dayKey: todayKey(),
      exhausted: false,
    };
  }

  /** Record spend for a completed turn. */
  recordSpend(record: SpendRecord): void {
    this.rolloverIfNewDay();
    this.state.spentUsd += record.turnUsd;

    if (!this.state.exhausted && this.ratio() >= 1.0) {
      this.state.exhausted = true;
      emitEvent(this.config.eventsFile, {
        type: "flywheel_budget_exhausted",
        ts: Date.now(),
        spent_usd: this.state.spentUsd,
        cap_usd: this.config.limitUsd,
        model: record.model,
        day_key: this.state.dayKey,
      });
    }
  }

  /**
   * Apply budget pressure to a routing config.
   * Returns a (possibly mutated copy) config, or { halt: true } when exhausted.
   */
  applyPressure(config: RoutingConfig): RoutingConfig & { halt?: boolean } {
    this.rolloverIfNewDay();
    const r = this.ratio();

    if (r >= 1.0) {
      return { ...config, halt: true };
    }

    // 80% → downgrade Opus → Sonnet
    if (r >= 0.80 && config.model?.startsWith("claude-opus")) {
      emitEvent(this.config.eventsFile, {
        type: "flywheel_budget_pressure",
        ts: Date.now(),
        reason: "budget_pressure_downgrade",
        from_model: config.model,
        to_model: "claude-sonnet-4-6-20251022",
        ratio: r,
        spent_usd: this.state.spentUsd,
        cap_usd: this.config.limitUsd,
      });
      return { ...config, model: "claude-sonnet-4-6-20251022", thinkingLevel: "low" };
    }

    // 90% → downgrade Sonnet → Haiku (tier ≤ 2 only)
    if (
      r >= 0.90 &&
      config.model?.startsWith("claude-sonnet") &&
      (config.tier ?? 2) <= 2
    ) {
      emitEvent(this.config.eventsFile, {
        type: "flywheel_budget_pressure",
        ts: Date.now(),
        reason: "budget_pressure_downgrade",
        from_model: config.model,
        to_model: "claude-haiku-4-5-20251001",
        ratio: r,
        spent_usd: this.state.spentUsd,
        cap_usd: this.config.limitUsd,
      });
      return { ...config, model: "claude-haiku-4-5-20251001", thinkingLevel: "none" };
    }

    return config;
  }

  /** True when the budget is exhausted for today. */
  isExhausted(): boolean {
    this.rolloverIfNewDay();
    return this.ratio() >= 1.0;
  }

  getState(): Readonly<BudgetState> {
    return { ...this.state };
  }

  private ratio(): number {
    if (this.config.limitUsd === Infinity || this.config.limitUsd <= 0) return 0;
    return this.state.spentUsd / this.config.limitUsd;
  }

  private rolloverIfNewDay(): void {
    const today = todayKey();
    if (today !== this.state.dayKey) {
      this.state = { spentUsd: 0, dayKey: today, exhausted: false };
    }
  }
}
