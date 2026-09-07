/*
 * Copyright (C) 2026 FuseItAll contributors.
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU Affero General Public License as published
 * by the Free Software Foundation, version 3 of the License. See LICENSE
 * for details.
 *
 * Thin Wails binding shim: re-exports the generated Service and adds
 * forward-compatible access to the remembered-device methods
 * (GetLastDevice, GetPeerDevice, SetCustomName, ReconnectToLastDevice).
 * The generated bindings refresh on the next `wails3 build`; until then
 * these wrappers degrade to null so `pnpm run check` passes and the UI
 * simply hides the offline card.
 */

import { Service } from '../bindings/fuseitall/mac/backend';

export interface LastDeviceNotice {
  HasDevice: boolean;
  Host: string;
  Port: number;
  Addr: string;
  LastSeenUnix: number;
  DeviceName: string;
  Model: string;
  BatteryPct: number | null;
  Charging: boolean | null;
  BatteryUnix: number;
  CustomName: string;
  DisplayName: string;
}

type LooseService = Record<string, ((...args: unknown[]) => Promise<unknown>) | undefined>;

const loose = Service as unknown as LooseService;

// normalizeDevice fills defaults for fields older backends do not send yet
// (pre-facts builds omit every key after LastSeenUnix), so the UI treats
// "unknown" uniformly instead of branching on undefined.
function normalizeDevice(raw: LastDeviceNotice): LastDeviceNotice | null {
  if (!raw || !raw.HasDevice) return null;
  return {
    HasDevice: true,
    Host: raw.Host ?? '',
    Port: raw.Port ?? 0,
    Addr: raw.Addr ?? '',
    LastSeenUnix: raw.LastSeenUnix ?? 0,
    DeviceName: raw.DeviceName ?? '',
    Model: raw.Model ?? '',
    BatteryPct: typeof raw.BatteryPct === 'number' ? raw.BatteryPct : null,
    Charging: typeof raw.Charging === 'boolean' ? raw.Charging : null,
    BatteryUnix: raw.BatteryUnix ?? 0,
    CustomName: raw.CustomName ?? '',
    DisplayName: raw.DisplayName ?? raw.DeviceName ?? '',
  };
}

export async function getLastDevice(): Promise<LastDeviceNotice | null> {
  try {
    const fn = loose['GetLastDevice'];
    if (typeof fn !== 'function') return null;
    const res = (await fn()) as LastDeviceNotice | null;
    if (!res) return null;
    return normalizeDevice(res);
  } catch {
    return null;
  }
}

export async function getPeerDevice(): Promise<LastDeviceNotice | null> {
  try {
    const fn = loose['GetPeerDevice'];
    if (typeof fn !== 'function') return null;
    const res = (await fn()) as LastDeviceNotice | null;
    if (!res) return null;
    return normalizeDevice(res);
  } catch {
    return null;
  }
}

export async function setCustomName(name: string): Promise<string> {
  const fn = loose['SetCustomName'];
  if (typeof fn !== 'function') {
    throw new Error('Rename is available after the next app build.');
  }
  return (await fn(name)) as string;
}

export async function reconnectToLastDevice(): Promise<string> {
  const fn = loose['ReconnectToLastDevice'];
  if (typeof fn !== 'function') {
    throw new Error('Reconnect is available after the next app build.');
  }
  return (await fn()) as string;
}

export async function forgetLastDevice(): Promise<string> {
  const fn = loose['ForgetLastDevice'];
  if (typeof fn !== 'function') {
    throw new Error('Forget is available after the next app build.');
  }
  return (await fn()) as string;
}

export interface AppSettings {
  NotificationsEnabled: boolean;
  ClipboardMode: string;
  UpdatedUnix: number;
  UpdatedBy: string;
}

export interface NotifView {
  ID: string;
  App: string;
  PackageName: string;
  IconB64: string;
  GroupKey: string;
  Title: string;
  Text: string;
  PostedUnix: number;
}

export interface NotifList {
  Items: NotifView[];
  Unseen: number;
}

function normalizeNotif(raw: unknown): NotifView {
  const r = raw as Record<string, unknown>;
  const id = (r['id'] ?? r['ID'] ?? '') as string;
  const app = (r['app'] ?? r['App'] ?? '') as string;
  const pkg = (r['package_name'] ?? r['PackageName'] ?? r['packageName'] ?? '') as string;
  const icon = (r['app_icon_b64'] ?? r['IconB64'] ?? r['iconB64'] ?? '') as string;
  const group = (r['group_key'] ?? r['GroupKey'] ?? '') as string;
  const title = (r['title'] ?? r['Title'] ?? '') as string;
  const text = (r['text'] ?? r['Text'] ?? '') as string;
  const posted = (r['posted_unix'] ?? r['PostedUnix'] ?? 0) as number;
  return {
    ID: typeof id === 'string' ? id : '',
    App: typeof app === 'string' ? app : '',
    PackageName: typeof pkg === 'string' ? pkg : '',
    IconB64: typeof icon === 'string' ? icon : '',
    GroupKey: typeof group === 'string' ? group : '',
    Title: typeof title === 'string' ? title : '',
    Text: typeof text === 'string' ? text : '',
    PostedUnix: typeof posted === 'number' ? posted : 0,
  };
}

export interface ClipNotice {
  HasText: boolean;
  Kind: string;
  Text: string;
  Mime: string;
  ImageB64: string;
  ChangedUnix: number;
  Origin: string;
  Preview: string;
  Pending: boolean;
}

export const defaultSettings: AppSettings = {
  NotificationsEnabled: true,
  ClipboardMode: 'both',
  UpdatedUnix: 0,
  UpdatedBy: '',
};

export function normalizeSettings(raw: AppSettings | null): AppSettings {
  if (!raw) return { ...defaultSettings };
  const r = raw as unknown as Record<string, unknown>;
  // Wails binding emits snake_case per Go json tags ("clipboard_mode" etc);
  // older builds used PascalCase — accept both.
  const modeRaw =
    typeof r['clipboard_mode'] === 'string'
      ? (r['clipboard_mode'] as string)
      : typeof r['ClipboardMode'] === 'string'
        ? (r['ClipboardMode'] as string)
        : 'both';
  const normMode = ['both', 'android_to_mac', 'mac_to_android', 'disabled'].includes(modeRaw) ? modeRaw : 'both';
  const notifRaw = r['notifications_enabled'] ?? r['NotificationsEnabled'];
  const unixRaw = r['updated_unix'] ?? r['UpdatedUnix'];
  const byRaw = r['updated_by'] ?? r['UpdatedBy'];
  return {
    NotificationsEnabled: typeof notifRaw === 'boolean' ? (notifRaw as boolean) : true,
    ClipboardMode: normMode,
    UpdatedUnix: typeof unixRaw === 'number' ? (unixRaw as number) : 0,
    UpdatedBy: typeof byRaw === 'string' ? (byRaw as string) : '',
  };
}

export async function getSettings(): Promise<AppSettings> {
  try {
    const fn = loose['GetSettings'];
    if (typeof fn !== 'function') return { ...defaultSettings };
    const res = (await fn()) as AppSettings | null;
    return normalizeSettings(res);
  } catch {
    return { ...defaultSettings };
  }
}

export async function setNotificationsEnabled(enabled: boolean): Promise<string> {
  const fn = loose['SetNotificationsEnabled'];
  if (typeof fn !== 'function') {
    throw new Error('Notification settings are available after the next app build.');
  }
  return (await fn(enabled)) as string;
}

export async function setClipboardMode(mode: string): Promise<string> {
  const fn = loose['SetClipboardMode'];
  if (typeof fn !== 'function') {
    throw new Error('Clipboard mode is available after the next app build.');
  }
  return (await fn(mode)) as string;
}

export async function getNotifications(): Promise<NotifList> {
  try {
    const fn = loose['GetNotifications'];
    if (typeof fn !== 'function') return { Items: [], Unseen: 0 };
    const res = (await fn()) as unknown as { Items?: unknown[]; Unseen?: number; items?: unknown[]; unseen?: number } | null;
    if (!res) return { Items: [], Unseen: 0 };
    const rawItems = (res.Items ?? res.items ?? []) as unknown[];
    const items = rawItems.map(normalizeNotif).filter((n) => n.ID);
    const unseen = (res.Unseen ?? res.unseen ?? 0) as number;
    return { Items: items, Unseen: typeof unseen === 'number' ? unseen : 0 };
  } catch {
    return { Items: [], Unseen: 0 };
  }
}

export async function markNotificationsSeen(): Promise<void> {
  try {
    const fn = loose['MarkNotificationsSeen'];
    if (typeof fn === 'function') await fn();
  } catch {
    // Badge reset is best-effort.
  }
}

export async function dismissNotification(id: string): Promise<string> {
  const fn = loose['DismissNotification'];
  if (typeof fn !== 'function') {
    throw new Error('Dismiss is available after the next app build.');
  }
  return (await fn(id)) as string;
}

export async function clearNotifications(): Promise<string> {
  const fn = loose['ClearNotifications'];
  if (typeof fn !== 'function') {
    throw new Error('Clear is available after the next app build.');
  }
  return (await fn()) as string;
}

export async function getClipboard(): Promise<ClipNotice | null> {
  try {
    const fn = loose['GetClipboard'];
    if (typeof fn !== 'function') return null;
    const res = (await fn()) as ClipNotice | null;
    if (!res || !res.HasText) return null;
    return res;
  } catch {
    return null;
  }
}

export async function pushClipboard(text: string): Promise<string> {
  const fn = loose['PushClipboard'];
  if (typeof fn !== 'function') {
    throw new Error('Clipboard sync is available after the next app build.');
  }
  return (await fn(text)) as string;
}

export async function pushClipboardImage(b64: string, mime: string): Promise<string> {
  const fn = loose['PushClipboardImage'];
  if (typeof fn !== 'function') {
    throw new Error('Clipboard image sync is available after the next app build.');
  }
  return (await fn(b64, mime)) as string;
}

export async function pushClipboardCurrent(): Promise<string> {
  const cur = loose['PushClipboardCurrent'];
  if (typeof cur === 'function') {
    return (await cur()) as string;
  }
  // Fallback for old build: try text path via clipboard API
  throw new Error('Send clipboard is available after the next app build (rebuild Mac).');
}

export async function getAppVersion(): Promise<string> {
  try {
    const fn = loose['GetAppVersion'];
    if (typeof fn !== 'function') return '0.3.0';
    const res = (await fn()) as string;
    return res || '0.3.0';
  } catch {
    return '0.3.0';
  }
}

// Files
export interface FileEntryView {
  name: string;
  path: string;
  is_dir: boolean;
  size: number;
  mod_time: number;
  mime?: string;
}
export interface FileListResult {
  path: string;
  entries: FileEntryView[];
  error?: string;
}
export interface FileTransferView {
  id: string;
  path: string;
  direction: string;
  status: string;
  progress: number;
  total_size: number;
  done_size: number;
  error?: string;
}

function normalizeFileList(raw: unknown): FileListResult {
  const r = raw as Record<string, unknown>;
  const path = (r['path'] ?? r['Path'] ?? '') as string;
  const err = (r['error'] ?? r['Error'] ?? '') as string;
  const rawEntries = (r['entries'] ?? r['Entries'] ?? []) as unknown[];
  const entries: FileEntryView[] = (rawEntries as Record<string, unknown>[]).map((e) => ({
    name: (e['name'] ?? e['Name'] ?? '') as string,
    path: (e['path'] ?? e['Path'] ?? '') as string,
    is_dir: (e['is_dir'] ?? e['IsDir'] ?? false) as boolean,
    size: (e['size'] ?? e['Size'] ?? 0) as number,
    mod_time: (e['mod_time'] ?? e['ModTime'] ?? 0) as number,
    mime: (e['mime'] ?? e['Mime'] ?? undefined) as string | undefined,
  }));
  return { path: typeof path === 'string' ? path : '', entries, error: typeof err === 'string' ? err : undefined };
}

export async function listPhoneFiles(path: string): Promise<FileListResult> {
  const fn = loose['ListPhoneFiles'];
  if (typeof fn !== 'function') throw new Error('Files requires app 0.5.0 — rebuild Mac.');
  const res = (await fn(path)) as unknown;
  return normalizeFileList(res);
}
export async function getLastFileList(): Promise<FileListResult> {
  const fn = loose['GetLastFileList'];
  if (typeof fn !== 'function') return { path: '', entries: [] };
  try {
    const res = (await fn()) as unknown;
    return normalizeFileList(res);
  } catch { return { path: '', entries: [] }; }
}
export async function mkdirPhone(path: string): Promise<string> {
  const fn = loose['MkdirPhone'];
  if (typeof fn !== 'function') throw new Error('Files requires app 0.5.0.');
  return (await fn(path)) as string;
}
export async function deletePhone(path: string): Promise<string> {
  const fn = loose['DeletePhone'];
  if (typeof fn !== 'function') throw new Error('Files requires app 0.5.0.');
  return (await fn(path)) as string;
}
export async function uploadLocalFiles(localPaths: string[], remoteDir: string): Promise<string> {
  const fn = loose['UploadLocalFiles'];
  if (typeof fn !== 'function') throw new Error('Files requires app 0.5.0.');
  return (await fn(localPaths, remoteDir)) as string;
}
export async function requestPhoneFile(remotePath: string, downloadDir: string): Promise<string> {
  const fn = loose['RequestPhoneFile'];
  if (typeof fn !== 'function') throw new Error('Files requires app 0.5.0.');
  return (await fn(remotePath, downloadDir)) as string;
}
export async function getTransfers(): Promise<FileTransferView[]> {
  const fn = loose['GetTransfers'];
  if (typeof fn !== 'function') return [];
  try {
    const res = (await fn()) as unknown;
    return (res as FileTransferView[]) ?? [];
  } catch { return []; }
}
export async function cancelTransfer(id: string): Promise<string> {
  const fn = loose['CancelTransfer'];
  if (typeof fn !== 'function') throw new Error('Cancel requires 0.5.0.');
  return (await fn(id)) as string;
}
export async function revealInFinder(id: string): Promise<string> {
  const fn = loose['RevealInFinder'];
  if (typeof fn !== 'function') return '';
  try { return (await fn(id)) as string; } catch { return ''; }
}
export async function uploadBrowserFile(b64: string, filename: string, remoteDir: string): Promise<string> {
  const fn = loose['UploadBrowserFile'];
  if (typeof fn !== 'function') throw new Error('Upload requires app 0.5.0.');
  return (await fn(b64, filename, remoteDir)) as string;
}

export { Service };
