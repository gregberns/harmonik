// debounce.ts — 400ms burst-debounce for Wake-LLM events per CL-062.
//
// Urgent events (CL-063) bypass this module entirely and are handled
// by the bridge before reaching the debouncer.

import type { SubscribeEvent } from "./wake-filter.js";

export const DEBOUNCE_MS = 400;

export interface Debouncer {
  add(event: SubscribeEvent): void;
  // Flush immediately, cancelling any pending timer.
  flush(): void;
  // Discard pending events and cancel timer without calling onFlush.
  clear(): void;
}

export function createDebouncer(
  onFlush: (events: SubscribeEvent[]) => void,
  debounceMs: number = DEBOUNCE_MS
): Debouncer {
  let timer: ReturnType<typeof setTimeout> | null = null;
  let pending: SubscribeEvent[] = [];

  function doFlush(): void {
    timer = null;
    if (pending.length === 0) return;
    const batch = pending.splice(0);
    onFlush(batch);
  }

  return {
    add(event: SubscribeEvent): void {
      pending.push(event);
      if (timer !== null) clearTimeout(timer);
      timer = setTimeout(doFlush, debounceMs);
    },
    flush(): void {
      if (timer !== null) { clearTimeout(timer); timer = null; }
      doFlush();
    },
    clear(): void {
      if (timer !== null) { clearTimeout(timer); timer = null; }
      pending = [];
    },
  };
}
