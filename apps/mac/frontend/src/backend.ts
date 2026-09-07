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
  UpdatedUnix: number;
  UpdatedBy: string;
}

export interface NotifView {
  ID: string;
  App: string;
  Title: string;
  Text: string;
  PostedUnix: number;
}

export interface NotifList {
  Items: NotifView[];
  Unseen: number;
}

export interface ClipNotice {
  HasText: boolean;
  Text: string;
  ChangedUnix: number;
  Origin: string;
  Preview: string;
  Pending: boolean;
}

export const defaultSettings: AppSettings = {
  NotificationsEnabled: true,
  UpdatedUnix: 0,
  UpdatedBy: '',
};

export function normalizeSettings(raw: AppSettings | null): AppSettings {
  if (!raw) return { ...defaultSettings };
  // Old payloads may carry ClipboardMode; ignore it.
  const r = raw as unknown as Record<string, unknown>;
  return {
    NotificationsEnabled: typeof r['NotificationsEnabled'] === 'boolean' ? (r['NotificationsEnabled'] as boolean) : true,
    UpdatedUnix: typeof r['UpdatedUnix'] === 'number' ? (r['UpdatedUnix'] as number) : 0,
    UpdatedBy: typeof r['UpdatedBy'] === 'string' ? (r['UpdatedBy'] as string) : '',
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

export async function getNotifications(): Promise<NotifList> {
  try {
    const fn = loose['GetNotifications'];
    if (typeof fn !== 'function') return { Items: [], Unseen: 0 };
    const res = (await fn()) as NotifList | null;
    if (!res) return { Items: [], Unseen: 0 };
    return { Items: res.Items ?? [], Unseen: res.Unseen ?? 0 };
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

export async function getAppVersion(): Promise<string> {
  try {
    const fn = loose['GetAppVersion'];
    if (typeof fn !== 'function') return '0.2.0';
    const res = (await fn()) as string;
    return res || '0.2.0';
  } catch {
    return '0.2.0';
  }
}

export { Service };
