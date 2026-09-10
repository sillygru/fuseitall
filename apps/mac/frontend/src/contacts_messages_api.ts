/*
 * Copyright (C) 2026 FuseItAll contributors.
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU Affero General Public License as published
 * by the Free Software Foundation, version 3 of the License. See LICENSE
 * for details.
 *
 * Contacts and SMS messages API shim for Wails frontend.
 */

import { Service } from './backend';

export interface ContactPhone {
  number: string;
  type?: string;
  label?: string;
  is_primary?: boolean;
  normalized_number?: string;
}

export interface ContactEmail {
  address: string;
  type?: string;
  label?: string;
}

export interface ContactOrganization {
  company?: string;
  title?: string;
  department?: string;
}

export interface ContactPostal {
  formatted?: string;
  type?: string;
}

export interface ContactEntry {
  contact_id: string;
  display_name: string;
  phones?: ContactPhone[];
  emails?: ContactEmail[];
  avatar_b64?: string;
  starred?: boolean;
  lookup_key?: string;
  last_updated_ms?: number;
  photo_version?: string;
  photo_uri?: string;
  birthday_ms?: number;
  anniversary_ms?: number;
  nickname?: string;
  note?: string;
  website?: string;
  organization?: ContactOrganization;
  postal?: ContactPostal;
}

export interface ContactListResult {
  contacts: ContactEntry[];
  next_cursor?: string;
  total_count: number;
  error?: string;
  error_code?: string;
  permission?: string;
}

export interface ContactAvatarResult {
  contact_id: string;
  avatar_b64?: string;
  photo_version?: string;
  error?: string;
}

export interface SMSThread {
  thread_id: number;
  address: string;
  contact_name?: string;
  contact_id?: string;
  photo_version?: string;
  snippet?: string;
  date: number;
  message_count: number;
  unread_count?: number;
  read: boolean;
}

export interface SMSMessage {
  id: number;
  thread_id: number;
  address: string;
  body: string;
  date: number;
  type: number; // 1 = inbox, 2 = sent
  read: boolean;
  status?: number;
  contact_name?: string;
  contact_id?: string;
  photo_version?: string;
}

export interface SMSThreadsResult {
  threads: SMSThread[];
  next_cursor?: string;
  error?: string;
  error_code?: string;
  permission?: string;
}

export interface SMSMessagesResult {
  thread_id: number;
  messages: SMSMessage[];
  next_cursor?: string;
  error?: string;
  error_code?: string;
  permission?: string;
}

export interface SMSSendResult {
  ok: boolean;
  client_id: string;
  message_id?: number;
  thread_id?: number;
  error?: string;
  error_code?: string;
  permission?: string;
}

type LooseService = Record<string, ((...args: unknown[]) => Promise<unknown>) | undefined>;
const loose = Service as unknown as LooseService;

export async function listContacts(
  cursor = '',
  limit = 50,
  forceRefresh = false,
  query = '',
): Promise<ContactListResult> {
  const withQuery = loose['ListContactsWithQuery'];
  if (typeof withQuery === 'function' && query !== '') {
    const res = (await withQuery(cursor, limit, forceRefresh, query)) as ContactListResult;
    return {
      contacts: res?.contacts ?? [],
      next_cursor: res?.next_cursor,
      total_count: res?.total_count ?? (res?.contacts?.length ?? 0),
      error: res?.error,
      error_code: res?.error_code,
      permission: res?.permission,
    };
  }
  const fn = loose['ListContacts'];
  if (typeof fn !== 'function') {
    throw new Error('Contacts requires app 0.13.0 — update Mac and phone.');
  }
  const res = (await fn(cursor, limit, forceRefresh)) as ContactListResult;
  return {
    contacts: res?.contacts ?? [],
    next_cursor: res?.next_cursor,
    total_count: res?.total_count ?? (res?.contacts?.length ?? 0),
    error: res?.error,
    error_code: res?.error_code,
    permission: res?.permission,
  };
}

export async function getContactAvatar(
  contactId: string,
  opts: { highRes?: boolean; expectedVersion?: string } = {},
): Promise<ContactAvatarResult> {
  // Versioned path avoids refetching unchanged photos across refreshes.
  if (opts.expectedVersion !== undefined) {
    const versioned = loose['GetContactAvatarVersioned'];
    if (typeof versioned === 'function') {
      try {
        return ((await versioned(contactId, opts.expectedVersion)) as ContactAvatarResult) ?? {
          contact_id: contactId,
        };
      } catch (e: unknown) {
        return { contact_id: contactId, error: e instanceof Error ? e.message : String(e) };
      }
    }
  }
  if (opts.highRes) {
    const full = loose['GetContactAvatarFull'];
    if (typeof full === 'function') {
      try {
        return ((await full(contactId)) as ContactAvatarResult) ?? { contact_id: contactId };
      } catch (e: unknown) {
        return { contact_id: contactId, error: e instanceof Error ? e.message : String(e) };
      }
    }
  }
  const fn = loose['GetContactAvatar'];
  if (typeof fn !== 'function') {
    return { contact_id: contactId };
  }
  try {
    return ((await fn(contactId)) as ContactAvatarResult) ?? { contact_id: contactId };
  } catch (e: unknown) {
    return { contact_id: contactId, error: e instanceof Error ? e.message : String(e) };
  }
}

// listAllContacts follows next_cursor until empty for a full directory
// sync (the pane used to fetch page 1 only, silently truncating >100).
// Fail-closed: any page error freezes with rows-so-far + error.
export async function listAllContacts(
  limit = 100,
  query = '',
  onProgress?: (loaded: number, total: number) => void,
  forceRefresh = false,
): Promise<ContactListResult> {
  const all: ContactEntry[] = [];
  let cursor = '';
  let total = 0;
  for (let page = 0; page < 20; page++) {
    const res = await listContacts(cursor, limit, forceRefresh && page === 0, query);
    if (res.error) {
      return { contacts: all, next_cursor: cursor, total_count: total || all.length, error: res.error, error_code: res.error_code, permission: res.permission };
    }
    all.push(...(res.contacts ?? []));
    total = res.total_count || total;
    onProgress?.(all.length, total);
    cursor = res.next_cursor ?? '';
    if (!cursor) break;
  }
  return { contacts: all, total_count: total || all.length };
}

// listAllSMSThreads follows next_cursor until empty (was page-1 only).
export async function listAllSMSThreads(
  limit = 50,
  onProgress?: (loaded: number) => void,
  forceRefresh = false,
): Promise<SMSThreadsResult> {
  const fn = loose['ListSMSThreads'];
  if (typeof fn !== 'function') {
    throw new Error('Messages requires app 0.13.0 — update Mac and phone.');
  }
  const all: SMSThread[] = [];
  let cursor = '';
  for (let page = 0; page < 20; page++) {
    const res = (await fn(cursor, limit, forceRefresh && page === 0)) as SMSThreadsResult;
    if (res?.error) {
      return { threads: all, next_cursor: cursor, error: res.error, error_code: res.error_code, permission: res.permission };
    }
    all.push(...(res?.threads ?? []));
    onProgress?.(all.length);
    cursor = res?.next_cursor ?? '';
    if (!cursor) break;
  }
  return { threads: all };
}

export async function markThreadRead(threadId: number): Promise<void> {
  const fn = loose['MarkThreadRead'];
  if (typeof fn !== 'function') return;
  try {
    await fn(threadId);
  } catch (e) {
    console.warn('MarkThreadRead error', e);
  }
}

export async function searchContacts(query: string): Promise<ContactEntry[]> {
  const fn = loose['SearchContacts'];
  if (typeof fn !== 'function') {
    return [];
  }
  try {
    return ((await fn(query)) as ContactEntry[]) ?? [];
  } catch {
    return [];
  }
}

export async function listSMSThreads(
  cursor = '',
  limit = 50,
  forceRefresh = false,
): Promise<SMSThreadsResult> {
  const fn = loose['ListSMSThreads'];
  if (typeof fn !== 'function') {
    throw new Error('Messages requires app 0.13.0 — update Mac and phone.');
  }
  const res = (await fn(cursor, limit, forceRefresh)) as SMSThreadsResult;
  return {
    threads: res?.threads ?? [],
    next_cursor: res?.next_cursor,
    error: res?.error,
    error_code: res?.error_code,
    permission: res?.permission,
  };
}

export async function listSMSMessages(
  threadId: number,
  cursor = '',
  limit = 50,
  forceRefresh = false,
): Promise<SMSMessagesResult> {
  const fn = loose['ListSMSMessages'];
  if (typeof fn !== 'function') {
    throw new Error('Messages requires app 0.13.0 — update Mac and phone.');
  }
  const res = (await fn(threadId, cursor, limit, forceRefresh)) as SMSMessagesResult;
  return {
    thread_id: threadId,
    messages: res?.messages ?? [],
    next_cursor: res?.next_cursor,
    error: res?.error,
    error_code: res?.error_code,
    permission: res?.permission,
  };
}

export async function sendSMS(recipient: string, body: string, subId = ''): Promise<SMSSendResult> {
  const withSub = loose['SendSMSWithSubID'];
  if (typeof withSub === 'function' && subId !== '') {
    const res = (await withSub(recipient, body, subId)) as SMSSendResult;
    return res ?? { ok: false, client_id: '', error: 'Unknown response' };
  }
  const fn = loose['SendSMS'];
  if (typeof fn !== 'function') {
    throw new Error('Send SMS requires app 0.13.0 — update Mac and phone.');
  }
  const res = (await fn(recipient, body)) as SMSSendResult;
  return res ?? { ok: false, client_id: '', error: 'Unknown response' };
}

// normalizePhone mirrors core.NormalizePhone for client-side thread matching:
// trim, keep one leading "+", strip all other non-digits. "" = not dialable.
export function normalizePhone(raw: string): string {
  const trimmed = (raw ?? '').trim();
  if (!trimmed) return '';
  const hasPlus = trimmed.startsWith('+');
  const digits = trimmed.replace(/\D/g, '');
  if (!digits) return '';
  return hasPlus ? `+${digits}` : digits;
}

// phonesEqual mirrors core.PhonesEqual (suffix >=7 digits covers E.164 vs
// national). Used to avoid duplicate compose views when Contacts passes
// "+1 (555)…" but the thread stores "555…".
export function phonesEqual(a: string, b: string): boolean {
  const na = normalizePhone(a);
  const nb = normalizePhone(b);
  if (!na || !nb) return false;
  if (na === nb) return true;
  const da = na.replace(/^\+/, '');
  const db = nb.replace(/^\+/, '');
  if (da === db) return true;
  const stripOne = (d: string) => (d.length === 11 && d.startsWith('1') ? d.slice(1) : d);
  const sa = stripOne(da);
  const sb = stripOne(db);
  if (sa === sb) return true;
  const [short, long] = sa.length <= sb.length ? [sa, sb] : [sb, sa];
  if (short.length < 7) return false;
  return long.endsWith(short);
}

export function findThreadForAddress(threads: SMSThread[], address: string): SMSThread | null {
  const trimmed = (address ?? '').trim();
  if (!trimmed) return null;
  if (trimmed.includes('@')) {
    const lower = trimmed.toLowerCase();
    return threads.find((t) => (t.address ?? '').trim().toLowerCase() === lower) ?? null;
  }
  return threads.find((t) => phonesEqual(t.address ?? '', trimmed)) ?? null;
}

export async function lookupContactForAddress(
  address: string,
): Promise<{ contact_name?: string; contact_id?: string; photo_version?: string }> {
  const fn = loose['LookupContactForAddress'];
  if (typeof fn !== 'function') return {};
  try {
    const res = (await fn(address)) as Record<string, string>;
    return {
      contact_name: res?.contact_name,
      contact_id: res?.contact_id,
      photo_version: res?.photo_version,
    };
  } catch {
    return {};
  }
}

export async function findSMSThreadForAddress(address: string): Promise<number> {
  const fn = loose['FindSMSThreadForAddress'];
  if (typeof fn !== 'function') return 0;
  try {
    const id = (await fn(address)) as number;
    return typeof id === 'number' ? id : 0;
  } catch {
    return 0;
  }
}
