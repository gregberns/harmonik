// watermark.ts — durable watermark + reacted-ledger state for the cognition loop.
//
// Implements CL-052 (state.json schema), CL-053 (effect→ledger→watermark ordering),
// CL-054 (never-regress, cold-start fallback), CL-INV-004.

import { readFileSync, writeFileSync, mkdirSync, renameSync, openSync, fsyncSync, closeSync } from "node:fs";
import { dirname } from "node:path";
import { randomUUID } from "node:crypto";

export interface WatermarkState {
  schema_version: 1;
  last_processed_event_id: string; // UUIDv7
  reacted_ledger: Record<string, string>; // event_id -> reaction_key
  updated_at: string; // ISO 8601
}

export const WATERMARK_SCHEMA_VERSION = 1 as const;

// UUIDv7 regex: 8-4-4-4-12 with version nibble = 7, variant = 8|9|a|b
const UUID_V7_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i;

export function isValidUUIDv7(s: string): boolean {
  return UUID_V7_RE.test(s);
}

// Lexicographic order on lowercase UUIDv7 string equals temporal order
// because the 48-bit ms timestamp occupies the high hex digits.
export function uuidv7Compare(a: string, b: string): number {
  const al = a.toLowerCase();
  const bl = b.toLowerCase();
  if (al < bl) return -1;
  if (al > bl) return 1;
  return 0;
}

export function maxUUIDv7(a: string | null, b: string | null): string | null {
  if (!a && !b) return null;
  if (!a) return b;
  if (!b) return a;
  return uuidv7Compare(a, b) >= 0 ? a : b;
}

// Read state.json.  Returns null on any parse error / schema mismatch / invalid
// UUIDv7 → triggers cold-start per CL-054.
export function readWatermark(statePath: string): WatermarkState | null {
  try {
    const raw = readFileSync(statePath, "utf8");
    const parsed = JSON.parse(raw) as Record<string, unknown>;
    if (parsed.schema_version !== WATERMARK_SCHEMA_VERSION) return null;
    const id = parsed.last_processed_event_id;
    if (typeof id !== "string" || !isValidUUIDv7(id)) return null;
    return parsed as unknown as WatermarkState;
  } catch {
    return null;
  }
}

// Atomic temp+rename+fsync write per CL-052 WM-026.
function atomicWriteState(statePath: string, state: WatermarkState): void {
  mkdirSync(dirname(statePath), { recursive: true });
  const updated: WatermarkState = { ...state, updated_at: new Date().toISOString() };
  const tmp = statePath + ".tmp." + randomUUID();
  writeFileSync(tmp, JSON.stringify(updated) + "\n", { encoding: "utf8" });
  const fd = openSync(tmp, "r+");
  try { fsyncSync(fd); } finally { closeSync(fd); }
  renameSync(tmp, statePath);
  // fsync parent dir to durably commit the rename
  const dirFd = openSync(dirname(statePath), "r");
  try { fsyncSync(dirFd); } finally { closeSync(dirFd); }
}

// Step 2 of CL-053: append event_id→reaction_key to reacted_ledger and fsync.
// MUST be called BEFORE advanceWatermark for the same event.
export function recordReaction(
  statePath: string,
  state: WatermarkState,
  eventId: string,
  reactionKey: string
): WatermarkState {
  const next: WatermarkState = {
    ...state,
    reacted_ledger: { ...state.reacted_ledger, [eventId]: reactionKey },
    updated_at: new Date().toISOString(),
  };
  atomicWriteState(statePath, next);
  return next;
}

// Step 3 of CL-053: advance last_processed_event_id and fsync.
// MUST be called AFTER recordReaction for the same event.
// Enforces never-regress: only advances if newId > current (CL-054, CL-INV-004).
export function advanceWatermark(
  statePath: string,
  state: WatermarkState,
  newEventId: string
): WatermarkState {
  if (uuidv7Compare(newEventId, state.last_processed_event_id) <= 0) {
    // Regression attempt: no-op (invariant already satisfied)
    return state;
  }
  const next: WatermarkState = {
    ...state,
    last_processed_event_id: newEventId,
    updated_at: new Date().toISOString(),
  };
  atomicWriteState(statePath, next);
  return next;
}

export function emptyWatermark(seedEventId: string): WatermarkState {
  return {
    schema_version: WATERMARK_SCHEMA_VERSION,
    last_processed_event_id: seedEventId,
    reacted_ledger: {},
    updated_at: new Date().toISOString(),
  };
}
