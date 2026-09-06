/*
 * Copyright (C) 2026 FuseItAll contributors.
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU Affero General Public License as published
 * by the Free Software Foundation, version 3 of the License. See LICENSE
 * for details.
 *
 * Closed-world activity feed: every known backend line shape maps to a
 * plain consumer sentence, internal startup noise is skipped, and unknown
 * lines collapse to a generic row. Raw log text (paths, addresses, slog
 * envelopes) never reaches the window.
 */

export interface ActivityItem {
  kind: 'ok' | 'lost' | 'info' | 'warn';
  text: string;
}

function rttMs(line: string): string | null {
  const m = /rtt_ms=(\d+)/.exec(line);
  return m ? m[1] : null;
}

export function summarizeLine(line: string): ActivityItem | null {
  const lower = line.toLowerCase();
  if (lower.includes('update_required_self')) {
    return { kind: 'warn', text: line.replace(/^.*UPDATE_REQUIRED_SELF\s+/, '').trim() || 'This Mac needs an update.' };
  }
  if (lower.includes('update_required')) {
    return { kind: 'warn', text: line.replace(/^.*UPDATE_REQUIRED\s+/, '').trim() || 'The phone needs an update.' };
  }
  if (lower.includes('phone cert mismatch') || lower.includes('phone cert updated') || lower.includes('fingerprint mismatch')) {
    return { kind: 'warn', text: 'Phone identity changed. Waiting for the phone to re-pin.' };
  }
  if (lower.includes('phone peer lost') || lower.includes('ping to phone failed')) {
    return { kind: 'lost', text: 'Phone connection lost. Reconnects automatically.' };
  }
  if (lower.includes('ping to phone ok')) {
    const ms = rttMs(line);
    return { kind: 'ok', text: ms ? `Phone replied in ${ms} ms.` : 'Phone replied.' };
  }
  if (lower.includes('phone peer captured')) {
    return { kind: 'ok', text: 'Phone found.' };
  }
  if (lower.includes('phone cert pinned')) {
    return { kind: 'ok', text: 'Phone verified.' };
  }
  if (lower.includes('forgot last device')) {
    return { kind: 'info', text: 'Phone forgotten.' };
  }
  if (lower.includes('last device save failed')) {
    return { kind: 'warn', text: 'Could not remember this phone.' };
  }
  // Internal startup noise: never a row.
  if (
    lower.includes('pair state loaded') ||
    lower.includes('pair state created') ||
    lower.includes('pair state corrupt') ||
    lower.includes('pair server listening') ||
    lower.includes('auto-reconnect heartbeat started')
  ) {
    return null;
  }
  return { kind: 'info', text: 'Background event.' };
}

export function humanizeLog(lines: string[]): ActivityItem[] {
  const items: ActivityItem[] = [];
  for (const line of lines.slice(-30).reverse()) {
    const item = summarizeLine(line);
    // Skip internal noise and collapse adjacent repeats.
    if (item && (items.length === 0 || items[items.length - 1].text !== item.text)) {
      items.push(item);
    }
  }
  return items;
}
