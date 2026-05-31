// tui-panel.ts — harmonik digest TUI status panel per CL-081 / CL-082 (hk-50dxt).
//
// Renders a live status widget above the Pi editor using ctx.ui.setWidget.
// Polls `harmonik digest --json` at ~1s cadence; refresh() triggers an
// immediate update so bridge-delivered daemon events land within 1s.
// Context fullness % is fed in from index.ts via setContextFullness().
//
// Spec: specs/cognition-loop.md §CL-081 (tmux inspectability),
//       §CL-082 (digest --watch parity).

import { execFile } from "node:child_process";
import { promisify } from "node:util";
import type { ExtensionUIContext } from "@earendil-works/pi-coding-agent";

const execFileAsync = promisify(execFile);

const WIDGET_KEY = "harmonik-digest";
const POLL_MS = 1000;

// DigestJSON mirrors internal/digest/types.go DigestJSON (schema_version=1).
export interface DigestJSON {
  schema_version: number;
  generated_at: string;
  queue: {
    present: boolean;
    status?: string;
    active_run_count: number;
    pending_count: number;
    active_runs?: Array<{ bead_id: string; run_id?: string; status: string }>;
  };
  recent_commits?: Array<{ hash: string; subject: string }>;
  recent_events?: Array<{ event_id: string; type: string; run_id?: string }>;
  ready_beads?: Array<{ bead_id: string; title: string }>;
  in_progress_beads?: Array<{ bead_id: string; title: string }>;
  open_notes?: Array<{ kind: string; text: string; ts: string }>;
  kerf_next?: unknown;
  truncated?: {
    active_runs_omitted?: number;
    recent_events_omitted?: number;
    open_notes_omitted?: number;
  };
  errors?: string[];
}

export interface DigestPanel {
  /** Attach to UI and start the polling timer. */
  start(ui: ExtensionUIContext): void;
  /** Stop the timer and clear the widget. */
  stop(): void;
  /** Trigger an immediate refresh (called by bridge on daemon events). */
  refresh(): void;
  /**
   * Update the context window fullness % shown in the panel header.
   * Called from the Pi `context` event handler (index.ts) on each turn.
   * Pass null when the value is unavailable.
   */
  setContextFullness(pct: number | null): void;
}

export function createDigestPanel(
  repoRoot: string,
  harmonikBin = "harmonik"
): DigestPanel {
  let ui: ExtensionUIContext | null = null;
  let timer: ReturnType<typeof setInterval> | null = null;
  let inFlight = false;
  let lastContent = "";
  let contextFullnessPct: number | null = null;

  async function fetchAndRender(): Promise<void> {
    if (!ui || inFlight) return;
    inFlight = true;
    // Capture ui before any await so stop() nulling it mid-flight is safe.
    const capturedUi = ui;
    const now = new Date();
    try {
      const { stdout } = await execFileAsync(
        harmonikBin,
        ["digest", "--json", "--project", repoRoot],
        { timeout: 5000 }
      );
      const d = JSON.parse(stdout.trim()) as DigestJSON;
      const lines = renderLines(d, contextFullnessPct, now);
      const content = lines.join("\n");
      if (content !== lastContent && capturedUi) {
        lastContent = content;
        capturedUi.setWidget(WIDGET_KEY, lines, { placement: "aboveEditor" });
      }
    } catch {
      const lines = renderError(contextFullnessPct, now);
      const content = lines.join("\n");
      if (content !== lastContent && capturedUi) {
        lastContent = content;
        capturedUi.setWidget(WIDGET_KEY, lines, { placement: "aboveEditor" });
      }
    } finally {
      inFlight = false;
    }
  }

  return {
    start(uiCtx: ExtensionUIContext): void {
      if (ui) return; // already started
      ui = uiCtx;
      fetchAndRender().catch(() => undefined);
      timer = setInterval(() => {
        fetchAndRender().catch(() => undefined);
      }, POLL_MS);
    },

    stop(): void {
      if (timer !== null) {
        clearInterval(timer);
        timer = null;
      }
      // Null ui early so in-flight fetchAndRender skips the final setWidget.
      const capturedUi = ui;
      ui = null;
      lastContent = "";
      capturedUi?.setWidget(WIDGET_KEY, undefined);
    },

    refresh(): void {
      fetchAndRender().catch(() => undefined);
    },

    setContextFullness(pct: number | null): void {
      contextFullnessPct = pct;
    },
  };
}

// ── Rendering ────────────────────────────────────────────────────────────────

function renderError(fullnessPct: number | null, now: Date): string[] {
  const fullness = fullnessPct != null ? `  ctx=${fullnessPct}%` : "";
  return [
    `harmonik digest  ${fmtTs(now)}${fullness}  [digest unavailable]`,
  ];
}

function renderLines(
  d: DigestJSON,
  fullnessPct: number | null,
  now: Date
): string[] {
  const lines: string[] = [];

  // Header — includes context fullness % (acceptance criterion)
  const lagMs = Math.max(0, now.getTime() - new Date(d.generated_at).getTime());
  const fullness = fullnessPct != null ? `  ctx=${fullnessPct}%` : "";
  lines.push(
    `harmonik digest  ${fmtTs(now)}  lag=${lagMs}ms  v${d.schema_version}${fullness}`
  );
  lines.push("─".repeat(68));

  // Watermark age (most recent event)
  const events = d.recent_events ?? [];
  const wAge =
    events.length > 0 ? uuidv7Age(events[0].event_id, now) : "no events";
  lines.push(`watermark age:  ${wAge}`);

  // In-flight runs
  lines.push("");
  const q = d.queue;
  if (!q.present) {
    lines.push("=== In-flight === (no active queue)");
  } else {
    lines.push(
      `=== In-flight (${q.active_run_count} active, ${q.pending_count} pending) ===`
    );
    for (const r of q.active_runs ?? []) {
      const runId = r.run_id ? r.run_id.slice(0, 8) : "—";
      lines.push(`  ${pad(r.bead_id, 14)}  run=${runId}  ${r.status}`);
    }
    if ((d.truncated?.active_runs_omitted ?? 0) > 0) {
      lines.push(`  [+${d.truncated!.active_runs_omitted!} omitted]`);
    }
  }

  // Recent completions (run_completed / run_failed)
  const completions = events.filter(
    (e) => e.type === "run_completed" || e.type === "run_failed"
  );
  lines.push("");
  lines.push(`=== Completions (${completions.length}) ===`);
  if (completions.length === 0) {
    lines.push("  (none)");
  }
  for (const ev of completions.slice(0, 5)) {
    const age = uuidv7Age(ev.event_id, now);
    const ref = ev.run_id ? `  run=${ev.run_id.slice(0, 8)}` : "";
    lines.push(`  ${pad(ev.type, 16)}${ref}  ${age} ago`);
  }
  if ((d.truncated?.recent_events_omitted ?? 0) > 0) {
    lines.push(`  [+${d.truncated!.recent_events_omitted!} events omitted]`);
  }

  // Open notes
  const notes = d.open_notes ?? [];
  lines.push("");
  lines.push(`=== Notes (${notes.length}) ===`);
  if (notes.length === 0) {
    lines.push("  (none)");
  }
  for (const n of notes.slice(0, 5)) {
    const age = durationAgo(new Date(n.ts), now);
    const text = n.text.length > 55 ? n.text.slice(0, 52) + "..." : n.text;
    lines.push(`  [${pad(n.kind, 10)}]  ${text}  (${age} ago)`);
  }
  if ((d.truncated?.open_notes_omitted ?? 0) > 0) {
    lines.push(`  [+${d.truncated!.open_notes_omitted!} more]`);
  }

  // Ready beads (decisions needed / unblocked work)
  const ready = d.ready_beads ?? [];
  if (ready.length > 0) {
    lines.push("");
    lines.push(`=== Ready beads (${ready.length}) ===`);
    for (const b of ready.slice(0, 5)) {
      lines.push(`  ${b.bead_id}  ${b.title}`);
    }
    if (ready.length > 5) {
      lines.push(`  [+${ready.length - 5} more]`);
    }
  }

  // In-progress beads
  const inProg = d.in_progress_beads ?? [];
  if (inProg.length > 0) {
    lines.push("");
    lines.push(`=== In-progress beads (${inProg.length}) ===`);
    for (const b of inProg) {
      lines.push(`  ${b.bead_id}  ${b.title}`);
    }
  }

  // Non-fatal collection errors
  if (d.errors && d.errors.length > 0) {
    lines.push("");
    for (const e of d.errors) {
      lines.push(`WARN: ${e}`);
    }
  }

  return lines;
}

// ── Helpers ──────────────────────────────────────────────────────────────────

function fmtTs(d: Date): string {
  return d.toISOString().slice(0, 19).replace("T", " ");
}

function pad(s: string, width: number): string {
  return s.length >= width ? s : s + " ".repeat(width - s.length);
}

// Extract the Unix-millisecond timestamp from a UUIDv7 string.
// UUIDv7 layout: first 48 bits = Unix epoch milliseconds.
// String form: xxxxxxxx-xxxx-7xxx-xxxx-xxxxxxxxxxxx
// First 12 hex nibbles after stripping hyphens encode the ms timestamp.
function uuidv7Age(eventId: string, now: Date): string {
  if (!eventId || eventId.length < 10) return "?";
  const hex = eventId.replace(/-/g, "").slice(0, 12);
  const msec = parseInt(hex, 16);
  if (!isFinite(msec) || msec <= 0) return "?";
  const ageMs = now.getTime() - msec;
  if (ageMs < 0) return "0s";
  return fmtDuration(Math.floor(ageMs / 1000));
}

function durationAgo(from: Date, now: Date): string {
  const s = Math.max(0, Math.floor((now.getTime() - from.getTime()) / 1000));
  return fmtDuration(s);
}

function fmtDuration(s: number): string {
  if (s < 60) return `${s}s`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m${String(s % 60).padStart(2, "0")}s`;
  const h = Math.floor(m / 60);
  return `${h}h${String(m % 60).padStart(2, "0")}m`;
}
