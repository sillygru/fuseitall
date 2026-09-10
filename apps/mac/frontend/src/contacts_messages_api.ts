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
}

export interface ContactEmail {
  address: string;
  type?: string;
  label?: string;
}

export interface ContactEntry {
  contact_id: string;
  display_name: string;
  phones?: ContactPhone[];
  emails?: ContactEmail[];
  avatar_b64?: string;
  starred?: boolean;
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
  error?: string;
}

export interface SMSThread {
  thread_id: number;
  address: string;
  contact_name?: string;
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

export async function getContactAvatar(contactId: string): Promise<ContactAvatarResult> {
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
