/*
 * Copyright (C) 2026 FuseItAll contributors.
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU Affero General Public License as published
 * by the Free Software Foundation, version 3 of the License. See LICENSE
 * for details.
 *
 * Pane listing cache policy: Files/Photos listings stay in RAM and render
 * instantly on reselect. A listing refetches only when its last successful
 * fetch is older than LIST_TTL_MS. Every Mac-side mutation refreshes
 * directly (which re-stamps), so self-made changes are always fresh; the
 * TTL only bounds phone-side staleness. Photo thumbnails are immutable per
 * photo id and live for the session; forgetting the phone clears all.
 *
 * No request to the phone may hang forever: listing fetches race a
 * timeout so a sleeping phone surfaces an error with Retry instead of
 * an endless spinner. Durations are judgment calls for a LAN link.
 */

export const LIST_TTL_MS = 30_000;

export function isFresh(fetchedAt: number, now: number = Date.now()): boolean {
  if (!fetchedAt) return false;
  return now - fetchedAt < LIST_TTL_MS;
}

export const LIST_TIMEOUT_MS = 30_000;

export function withTimeout<T>(promise: Promise<T>, ms: number, label: string): Promise<T> {
  let timer: ReturnType<typeof setTimeout> | undefined;
  const timeout = new Promise<never>((_, reject) => {
    timer = setTimeout(() => reject(new Error(`${label} timed out after ${ms / 1000}s`)), ms);
  });
  return Promise.race([promise, timeout]).finally(() => {
    if (timer !== undefined) clearTimeout(timer);
  });
}
