/*
 * SPDX-License-Identifier: AGPL-3.0-only
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
  FilesPermission?: string;
  PhotosPermission?: string;
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
    FilesPermission: ((raw as unknown as Record<string, unknown>)['FilesPermission'] ?? (raw as unknown as Record<string, unknown>)['filesPermission'] ?? undefined) as string | undefined,
    PhotosPermission: ((raw as unknown as Record<string, unknown>)['PhotosPermission'] ?? (raw as unknown as Record<string, unknown>)['photosPermission'] ?? undefined) as string | undefined,
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

export async function notifyLocalNetworkDown(): Promise<string> {
  const fn = loose['NotifyLocalNetworkDown'];
  if (typeof fn !== 'function') return 'Offline.';
  return (await fn()) as string;
}

export async function forgetLastDevice(): Promise<string> {
  const fn = loose['ForgetLastDevice'];
  if (typeof fn !== 'function') {
    throw new Error('Forget is available after the next app build.');
  }
  return (await fn()) as string;
}

export interface PairStatus {
  Listening: boolean;
  QRHost: string;
  QRPort: number;
  QRCandidates: string[];
  LastRejectKind: string;
  LastRejectUnix: number;
  LastAcceptUnix: number;
}

export async function getPairStatus(): Promise<PairStatus | null> {
  try {
    const fn = loose['GetPairStatus'];
    if (typeof fn !== 'function') return null;
    const res = (await fn()) as PairStatus | null;
    if (!res) return null;
    return {
      Listening: Boolean(res.Listening),
      QRHost: typeof res.QRHost === 'string' ? res.QRHost : '',
      QRPort: typeof res.QRPort === 'number' ? res.QRPort : 0,
      QRCandidates: Array.isArray(res.QRCandidates) ? res.QRCandidates.filter((h) => typeof h === 'string') : [],
      LastRejectKind: typeof res.LastRejectKind === 'string' ? res.LastRejectKind : '',
      LastRejectUnix: typeof res.LastRejectUnix === 'number' ? res.LastRejectUnix : 0,
      LastAcceptUnix: typeof res.LastAcceptUnix === 'number' ? res.LastAcceptUnix : 0,
    };
  } catch {
    return null;
  }
}

export interface AppSettings {
  NotificationsEnabled: boolean;
  NotifMode: string;
  MutedPackages: string[];
  AllowedPackages: string[];
  ClipboardMode: string;
  ClipboardAllowSensitive: boolean;
  PlaybackMode: string;
  PlaybackOutput: string;
  UpdatedUnix: number;
  UpdatedBy: string;
}

export interface KnownNotifApp {
  PackageName: string;
  App: string;
  IconB64: string;
  Count: number;
  Muted: boolean;
  Allowed: boolean;
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

export function normalizeNotifList(raw: unknown): NotifList {
  const r = (raw ?? {}) as Record<string, unknown>;
  const rawItems = r['Items'] ?? r['items'] ?? [];
  const arr = Array.isArray(rawItems) ? rawItems : [];
  const items = arr
    .filter((e) => e && typeof e === 'object')
    .map(normalizeNotif)
    .filter((n) => n.ID);
  const unseenRaw = r['Unseen'] ?? r['unseen'] ?? 0;
  return { Items: items, Unseen: typeof unseenRaw === 'number' ? unseenRaw : 0 };
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
  Filename: string;
  ChangedUnix: number;
  ChangedC: number;
  Origin: string;
  Preview: string;
  Pending: boolean;
  Sensitive: boolean;
}

export interface PlaybackView {
  HasState: boolean;
  Title: string;
  Artist: string;
  Album: string;
  PackageName: string;
  App: string;
  State: string;
  PositionMs: number;
  DurationMs: number;
  UpdatedMs: number;
  ArtworkB64: string;
  ArtworkMime: string;
}

export const defaultPlayback: PlaybackView = {
  HasState: false,
  Title: '',
  Artist: '',
  Album: '',
  PackageName: '',
  App: '',
  State: 'stopped',
  PositionMs: 0,
  DurationMs: 0,
  UpdatedMs: 0,
  ArtworkB64: '',
  ArtworkMime: '',
};

export function normalizePlayback(raw: unknown): PlaybackView {
  if (!raw || typeof raw !== 'object') return { ...defaultPlayback };
  const r = raw as Record<string, unknown>;
  const pick = (a: unknown, b: unknown) =>
    (typeof a === 'string' && a ? a : typeof b === 'string' && b ? b : '');
  const num = (a: unknown, b: unknown) =>
    (typeof a === 'number' && a >= 0 ? a : typeof b === 'number' && b >= 0 ? b : 0);
  const stateRaw = pick(r['state'], r['State']);
  const state = stateRaw === 'playing' || stateRaw === 'paused' ? stateRaw : 'stopped';
  return {
    HasState: Boolean(r['has_state'] ?? r['HasState'] ?? false),
    Title: pick(r['title'], r['Title']),
    Artist: pick(r['artist'], r['Artist']),
    Album: pick(r['album'], r['Album']),
    PackageName: pick(r['package_name'], r['PackageName']),
    App: pick(r['app'], r['App']),
    State: state,
    PositionMs: num(r['position_ms'], r['PositionMs']),
    DurationMs: num(r['duration_ms'], r['DurationMs']),
    UpdatedMs: num(r['updated_ms'], r['UpdatedMs']),
    ArtworkB64: pick(r['artwork_b64'], r['ArtworkB64']),
    ArtworkMime: pick(r['artwork_mime'], r['ArtworkMime']),
  };
}

export async function getPlayback(): Promise<PlaybackView> {
  try {
    const fn = loose['GetPlayback'];
    if (typeof fn !== 'function') return { ...defaultPlayback };
    return normalizePlayback(await fn());
  } catch {
    return { ...defaultPlayback };
  }
}

export async function sendPlaybackCmd(cmd: string): Promise<string> {
  const fn = loose['SendPlaybackCmd'];
  if (typeof fn !== 'function') {
    throw new Error('Playback control is available after the next app build.');
  }
  return (await fn(cmd)) as string;
}

export async function setPlaybackMode(mode: string): Promise<string> {
  const fn = loose['SetPlaybackMode'];
  if (typeof fn !== 'function') {
    throw new Error('Playback mode is available after the next app build.');
  }
  return (await fn(mode)) as string;
}

export async function setPlaybackOutput(output: string): Promise<string> {
  const fn = loose['SetPlaybackOutput'];
  if (typeof fn !== 'function') {
    throw new Error('Playback output is available after the next app build.');
  }
  return (await fn(output)) as string;
}

export interface DNDView {
  Enabled: boolean;
  HasPermission: boolean;
  HasState: boolean;
  UpdatedMs: number;
}

export const defaultDND: DNDView = {
  Enabled: false,
  HasPermission: false,
  HasState: false,
  UpdatedMs: 0,
};

export function normalizeDND(raw: unknown): DNDView {
  if (!raw || typeof raw !== 'object') return { ...defaultDND };
  const r = raw as Record<string, unknown>;
  const num = (a: unknown, b: unknown) =>
    (typeof a === 'number' && a >= 0 ? a : typeof b === 'number' && b >= 0 ? b : 0);
  return {
    Enabled: Boolean(r['enabled'] ?? r['Enabled'] ?? false),
    HasPermission: Boolean(r['has_permission'] ?? r['HasPermission'] ?? false),
    HasState: Boolean(r['has_state'] ?? r['HasState'] ?? false),
    UpdatedMs: num(r['updated_ms'], r['UpdatedMs']),
  };
}

export async function getDND(): Promise<DNDView> {
  try {
    const fn = loose['GetDND'];
    if (typeof fn !== 'function') return { ...defaultDND };
    return normalizeDND(await fn());
  } catch {
    return { ...defaultDND };
  }
}

export async function setDND(enabled: boolean): Promise<string> {
  const fn = loose['SetDND'];
  if (typeof fn !== 'function') {
    throw new Error('Do Not Disturb control is available after the next app build.');
  }
  return (await fn(enabled)) as string;
}

export const defaultSettings: AppSettings = {
  NotificationsEnabled: true,
  NotifMode: 'all_except_muted',
  MutedPackages: [],
  AllowedPackages: [],
  ClipboardMode: 'both',
  ClipboardAllowSensitive: false,
  PlaybackMode: 'both',
  PlaybackOutput: 'inapp',
  UpdatedUnix: 0,
  UpdatedBy: '',
};

function normalizeStringList(v: unknown): string[] {
  if (!Array.isArray(v)) return [];
  const out: string[] = [];
  const seen = new Set<string>();
  for (const e of v) {
    if (typeof e !== 'string') continue;
    const t = e.trim();
    if (!t || t.length > 128 || seen.has(t)) continue;
    seen.add(t);
    out.push(t);
    if (out.length >= 100) break;
  }
  return out.sort();
}

export function normalizeSettings(raw: AppSettings | null): AppSettings {
  if (!raw) return { ...defaultSettings, MutedPackages: [], AllowedPackages: [] };
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
  const sensRaw = r['clipboard_allow_sensitive'] ?? r['ClipboardAllowSensitive'];
  const notifRaw = r['notifications_enabled'] ?? r['NotificationsEnabled'];
  const unixRaw = r['updated_unix'] ?? r['UpdatedUnix'];
  const byRaw = r['updated_by'] ?? r['UpdatedBy'];
  const nmRaw =
    typeof r['notif_mode'] === 'string'
      ? (r['notif_mode'] as string)
      : typeof r['NotifMode'] === 'string'
        ? (r['NotifMode'] as string)
        : 'all_except_muted';
  const normNotifMode = nmRaw === 'only_allowed' ? 'only_allowed' : 'all_except_muted';
  const pmRaw =
    typeof r['playback_mode'] === 'string'
      ? (r['playback_mode'] as string)
      : typeof r['PlaybackMode'] === 'string'
        ? (r['PlaybackMode'] as string)
        : 'both';
  const normPlaybackMode = ['both', 'android_to_mac', 'mac_to_android', 'disabled'].includes(pmRaw) ? pmRaw : 'both';
  const poRaw =
    typeof r['playback_output'] === 'string'
      ? (r['playback_output'] as string)
      : typeof r['PlaybackOutput'] === 'string'
        ? (r['PlaybackOutput'] as string)
        : 'inapp';
  const normPlaybackOutput = poRaw === 'system' ? 'system' : 'inapp';
  return {
    NotificationsEnabled: typeof notifRaw === 'boolean' ? (notifRaw as boolean) : true,
    NotifMode: normNotifMode,
    MutedPackages: normalizeStringList(r['muted_packages'] ?? r['MutedPackages']),
    AllowedPackages: normalizeStringList(r['allowed_packages'] ?? r['AllowedPackages']),
    ClipboardMode: normMode,
    ClipboardAllowSensitive: sensRaw === true,
    PlaybackMode: normPlaybackMode,
    PlaybackOutput: normPlaybackOutput,
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

export async function setClipboardAllowSensitive(allow: boolean): Promise<string> {
  const fn = loose['SetClipboardAllowSensitive'];
  if (typeof fn !== 'function') {
    throw new Error('Sensitive clipboard setting is available after the next app build.');
  }
  return (await fn(allow)) as string;
}

export async function setNotifMode(mode: string): Promise<string> {
  const fn = loose['SetNotifMode'];
  if (typeof fn !== 'function') {
    throw new Error('Notification filter is available after the next app build.');
  }
  return (await fn(mode)) as string;
}

export async function setAppMuted(pkg: string, muted: boolean): Promise<string> {
  const fn = loose['SetAppMuted'];
  if (typeof fn !== 'function') {
    throw new Error('Per-app mute is available after the next app build.');
  }
  return (await fn(pkg, muted)) as string;
}

export async function setAppAllowed(pkg: string, allowed: boolean): Promise<string> {
  const fn = loose['SetAppAllowed'];
  if (typeof fn !== 'function') {
    throw new Error('Per-app allow is available after the next app build.');
  }
  return (await fn(pkg, allowed)) as string;
}

function normalizeKnownApp(raw: unknown): KnownNotifApp | null {
  const r = raw as Record<string, unknown>;
  const pkg =
    (r['package_name'] ?? r['PackageName'] ?? '') as string;
  if (typeof pkg !== 'string' || !pkg.trim()) return null;
  const app = (r['app'] ?? r['App'] ?? pkg) as string;
  const icon = (r['app_icon_b64'] ?? r['IconB64'] ?? '') as string;
  const count = (r['count'] ?? r['Count'] ?? 0) as number;
  return {
    PackageName: pkg,
    App: typeof app === 'string' && app ? app : pkg,
    IconB64: typeof icon === 'string' ? icon : '',
    Count: typeof count === 'number' ? count : 0,
    Muted: Boolean(r['muted'] ?? r['Muted'] ?? false),
    Allowed: Boolean(r['allowed'] ?? r['Allowed'] ?? false),
  };
}

export async function getKnownNotifApps(): Promise<KnownNotifApp[]> {
  try {
    const fn = loose['GetKnownNotifApps'];
    if (typeof fn !== 'function') return [];
    const res = (await fn()) as unknown;
    if (!Array.isArray(res)) return [];
    return res
      .map(normalizeKnownApp)
      .filter((a): a is KnownNotifApp => a !== null);
  } catch {
    return [];
  }
}

export function friendlyPhoneAppsError(msg: string): string {
  const m = (msg || '').toLowerCase();
  if (m.includes('offline') || m.includes('reconnect')) return 'Phone is offline — showing mirrored apps.';
  if (m.includes('support') || m.includes('update')) return 'Phone needs the update with app sharing — showing mirrored apps.';
  if (m.includes('timed out') || m.includes('did not respond')) return "Phone didn't answer — showing mirrored apps.";
  if (!m) return '';
  return "Couldn't load phone apps — showing mirrored apps.";
}

export async function requestPhoneNotifApps(refresh: boolean): Promise<KnownNotifApp[]> {
  const fn = loose['RequestPhoneNotifApps'];
  if (typeof fn !== 'function') return [];
  const res = (await fn(refresh)) as unknown;
  if (!Array.isArray(res)) return [];
  return res
    .map(normalizeKnownApp)
    .filter((a): a is KnownNotifApp => a !== null);
}

export async function getNotifications(): Promise<NotifList> {
  try {
    const fn = loose['GetNotifications'];
    if (typeof fn !== 'function') return { Items: [], Unseen: 0 };
    return normalizeNotifList(await fn());
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

export function onClipboardChanged(cb: (n: ClipNotice) => void): () => void {
  try {
    // Lazy require to avoid hard dep in tests; mirrors FileManager pattern.
    // eslint-disable-next-line @typescript-eslint/no-require-imports, @typescript-eslint/no-explicit-any
    const rt = (globalThis as unknown as { require?: (m: string) => unknown })['require'] as unknown;
    void rt;
    // Use dynamic import via @wailsio/runtime if available.
    // Caller should use Events.On directly; this helper is best-effort.
    // We attempt to load at runtime.
    const maybeEvents = (globalThis as unknown as Record<string, unknown>)['__wailsEvents'] as
      | { On: (name: string, fn: (e: unknown) => void) => () => void }
      | undefined;
    if (maybeEvents) {
      return maybeEvents.On('clipboard:changed', (e: unknown) => {
        const d = (e as { data?: unknown })?.data ?? e;
        if (d && typeof d === 'object') cb(d as ClipNotice);
      });
    }
  } catch {
    // ignore
  }
  return () => {};
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
  error_code?: string;
  permission?: string;
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
  source?: string;
  resumable?: boolean;
  batch_id?: string;
}
export interface TransferBatchView {
  id: string;
  total_files: number;
  done_files: number;
  total_bytes: number;
  done_bytes: number;
  current_path: string;
  status: string;
  progress: number;
}

function normalizeBatch(raw: unknown): TransferBatchView {
  const r = (raw ?? {}) as Record<string, unknown>;
  const num = (a: unknown, b: unknown) =>
    (typeof a === 'number' && a >= 0 ? a : typeof b === 'number' && b >= 0 ? b : 0);
  const str = (a: unknown, b: unknown) =>
    (typeof a === 'string' && a ? a : typeof b === 'string' && b ? b : '');
  return {
    id: str(r['id'], r['ID']),
    total_files: num(r['total_files'], r['TotalFiles']),
    done_files: num(r['done_files'], r['DoneFiles']),
    total_bytes: num(r['total_bytes'], r['TotalBytes']),
    done_bytes: num(r['done_bytes'], r['DoneBytes']),
    current_path: str(r['current_path'], r['CurrentPath']),
    status: str(r['status'], r['Status']) || 'running',
    progress: num(r['progress'], r['Progress']),
  };
}

export async function beginUploadBatch(totalFiles: number, totalBytes: number): Promise<string> {
  const fn = loose['BeginUploadBatch'];
  if (typeof fn !== 'function') throw new Error('Batched uploads require a newer Mac build.');
  return (await fn(totalFiles, totalBytes)) as string;
}
export async function getTransferBatches(): Promise<TransferBatchView[]> {
  const fn = loose['GetTransferBatches'];
  if (typeof fn !== 'function') return [];
  try {
    const res = (await fn()) as unknown;
    if (!Array.isArray(res)) return [];
    return (res as unknown[]).map(normalizeBatch).filter((b) => b.id);
  } catch { return []; }
}
export async function cancelUploadBatch(id: string): Promise<string> {
  const fn = loose['CancelUploadBatch'];
  if (typeof fn !== 'function') throw new Error('Batch cancel requires a newer Mac build.');
  return (await fn(id)) as string;
}
export async function attachTransferToBatch(transferId: string, batchId: string): Promise<string> {
  const fn = loose['AttachTransferToBatch'];
  if (typeof fn !== 'function') throw new Error('Batch attach requires a newer Mac build.');
  return (await fn(transferId, batchId)) as string;
}
export async function getDefaultUploadDir(): Promise<string> {
  const fn = loose['GetDefaultUploadDir'];
  if (typeof fn !== 'function') return '';
  try { return ((await fn()) as string) || ''; } catch { return ''; }
}
export async function setDefaultUploadDir(dir: string): Promise<string> {
  const fn = loose['SetDefaultUploadDir'];
  if (typeof fn !== 'function') throw new Error('Upload default requires a newer Mac build.');
  return (await fn(dir)) as string;
}
export async function uploadLocalFilesWithPolicyInBatch(
  localPaths: string[],
  remoteDir: string,
  policy: string,
  batchId: string,
): Promise<string> {
  const fn = loose['UploadLocalFilesWithPolicyInBatch'];
  if (typeof fn !== 'function') return uploadLocalFilesWithPolicy(localPaths, remoteDir, policy);
  return (await fn(localPaths, remoteDir, policy, batchId)) as string;
}
export interface DecidedUpload {
  localPath?: string;
  local_path?: string;
  remotePath?: string;
  remote_path?: string;
  policy: string;
}
export async function uploadDecidedFilesInBatch(
  decided: DecidedUpload[],
  batchId: string,
): Promise<string> {
  const fn = loose['UploadDecidedFiles'];
  if (typeof fn !== 'function') {
    // Legacy Mac build: same decisions, one slow round-trip per file.
    let done = 0;
    for (const d of decided) {
      const local = d.localPath ?? d.local_path ?? '';
      const remote = d.remotePath ?? d.remote_path ?? '';
      if (!d.policy) {
        await uploadLocalFileToRemotePath(local, remote, '');
      } else {
        const dir = remote.includes('/') ? remote.slice(0, remote.lastIndexOf('/')) : '';
        await uploadLocalFilesWithPolicy([local], dir, d.policy);
      }
      done++;
    }
    return `Uploaded ${done} file(s).`;
  }
  const payload = decided.map((d) => ({
    local_path: d.local_path ?? d.localPath ?? '',
    remote_path: d.remote_path ?? d.remotePath ?? '',
    localPath: d.localPath ?? d.local_path ?? '',
    remotePath: d.remotePath ?? d.remote_path ?? '',
    policy: d.policy ?? '',
  }));
  return (await fn(payload, batchId)) as string;
}
export async function uploadLocalFileToRemotePathInBatch(
  localPath: string,
  remotePath: string,
  policy: string,
  batchId: string,
): Promise<string> {
  const fn = loose['UploadLocalFileToRemotePathInBatch'];
  if (typeof fn !== 'function') return uploadLocalFileToRemotePath(localPath, remotePath, policy);
  return (await fn(localPath, remotePath, policy, batchId)) as string;
}

function normalizeFileList(raw: unknown): FileListResult {
  const r = raw as Record<string, unknown>;
  const path = (r['path'] ?? r['Path'] ?? '') as string;
  const err = (r['error'] ?? r['Error'] ?? '') as string;
  const errCode = (r['error_code'] ?? r['ErrorCode'] ?? '') as string;
  const perm = (r['permission'] ?? r['Permission'] ?? '') as string;
  const rawEntries = (r['entries'] ?? r['Entries'] ?? []) as unknown[];
  const entries: FileEntryView[] = (rawEntries as Record<string, unknown>[]).map((e) => ({
    name: (e['name'] ?? e['Name'] ?? '') as string,
    path: (e['path'] ?? e['Path'] ?? '') as string,
    is_dir: (e['is_dir'] ?? e['IsDir'] ?? false) as boolean,
    size: (e['size'] ?? e['Size'] ?? 0) as number,
    mod_time: (e['mod_time'] ?? e['ModTime'] ?? 0) as number,
    mime: (e['mime'] ?? e['Mime'] ?? undefined) as string | undefined,
  }));
  return {
    path: typeof path === 'string' ? path : '',
    entries,
    error: typeof err === 'string' && err ? err : undefined,
    error_code: typeof errCode === 'string' && errCode ? errCode : undefined,
    permission: typeof perm === 'string' && perm ? perm : undefined,
  };
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
export interface LocalFileInfo {
  path: string;
  name: string;
  size: number;
  mtime: number;
  is_dir: boolean;
}
export async function statLocalFiles(localPaths: string[]): Promise<LocalFileInfo[]> {
  const fn = loose['StatLocalFiles'];
  if (typeof fn !== 'function') throw new Error('Conflict check requires a newer Mac build.');
  const res = (await fn(localPaths)) as unknown;
  if (!Array.isArray(res)) return [];
  return (res as Record<string, unknown>[]).map((r) => ({
    path: (r['path'] ?? r['Path'] ?? '') as string,
    name: (r['name'] ?? r['Name'] ?? '') as string,
    size: (r['size'] ?? r['Size'] ?? 0) as number,
    mtime: (r['mtime'] ?? r['Mtime'] ?? 0) as number,
    is_dir: Boolean(r['is_dir'] ?? r['IsDir'] ?? false),
  }));
}
export async function uploadLocalFilesWithPolicy(
  localPaths: string[],
  remoteDir: string,
  policy: string,
): Promise<string> {
  const fn = loose['UploadLocalFilesWithPolicy'];
  if (typeof fn !== 'function') return uploadLocalFiles(localPaths, remoteDir);
  return (await fn(localPaths, remoteDir, policy)) as string;
}
export async function uploadLocalFileToRemotePath(
  localPath: string,
  remotePath: string,
  policy: string,
): Promise<string> {
  const fn = loose['UploadLocalFileToRemotePath'];
  if (typeof fn !== 'function') throw new Error('Keep both requires a newer Mac build.');
  return (await fn(localPath, remotePath, policy)) as string;
}
export async function uploadBrowserFileToRemotePath(
  b64: string,
  remotePath: string,
  policy: string,
  sourceMtime: number,
): Promise<string> {
  const fn = loose['UploadBrowserFileToRemotePath'];
  if (typeof fn !== 'function') {
    const dir = remotePath.includes('/') ? remotePath.slice(0, remotePath.lastIndexOf('/')) : '';
    const name = remotePath.split('/').pop() || 'file';
    return uploadBrowserFile(b64, name, dir);
  }
  return (await fn(b64, remotePath, policy, sourceMtime)) as string;
}
export async function uploadBrowserFileWithPolicy(
  b64: string,
  filename: string,
  remoteDir: string,
  policy: string,
  sourceMtime: number,
): Promise<string> {
  const fn = loose['UploadBrowserFileWithPolicy'];
  if (typeof fn !== 'function') return uploadBrowserFile(b64, filename, remoteDir);
  return (await fn(b64, filename, remoteDir, policy, sourceMtime)) as string;
}
export async function uploadBrowserFileWithRelPathAndPolicy(
  b64: string,
  relPath: string,
  remoteDir: string,
  policy: string,
  sourceMtime: number,
): Promise<string> {
  const fn = loose['UploadBrowserFileWithRelPathAndPolicy'];
  if (typeof fn !== 'function') return uploadBrowserFileWithRelPath(b64, relPath, remoteDir);
  return (await fn(b64, relPath, remoteDir, policy, sourceMtime)) as string;
}
export interface BrowserUploadBegin {
  transferId: string;
  remotePath: string;
}
function normalizeBrowserBegin(raw: unknown): BrowserUploadBegin {
  const r = (raw ?? {}) as Record<string, unknown>;
  return {
    transferId: ((r['transfer_id'] ?? r['TransferID'] ?? '') as string) || '',
    remotePath: ((r['remote_path'] ?? r['RemotePath'] ?? '') as string) || '',
  };
}
export async function beginBrowserUpload(
  filename: string,
  relPath: string,
  remoteDir: string,
  totalSize: number,
  policy: string,
  sourceMtime: number,
): Promise<BrowserUploadBegin> {
  const fn = loose['BeginBrowserUpload'];
  if (typeof fn !== 'function') throw new Error('Sliced upload requires a newer Mac build.');
  return normalizeBrowserBegin(await fn(filename, relPath, remoteDir, totalSize, policy, sourceMtime));
}
export async function beginBrowserUploadToPath(
  remotePath: string,
  totalSize: number,
  policy: string,
  sourceMtime: number,
): Promise<BrowserUploadBegin> {
  const fn = loose['BeginBrowserUploadToPath'];
  if (typeof fn !== 'function') throw new Error('Sliced upload requires a newer Mac build.');
  const id = (await fn(remotePath, totalSize, policy, sourceMtime)) as string;
  return { transferId: typeof id === 'string' ? id : '', remotePath };
}
export async function beginBrowserUploadInBatch(
  filename: string,
  relPath: string,
  remoteDir: string,
  totalSize: number,
  policy: string,
  sourceMtime: number,
  batchId: string,
): Promise<BrowserUploadBegin> {
  const fn = loose['BeginBrowserUploadInBatch'];
  if (typeof fn !== 'function') return beginBrowserUpload(filename, relPath, remoteDir, totalSize, policy, sourceMtime);
  const raw = await fn(filename, relPath, remoteDir, totalSize, policy, sourceMtime, batchId);
  return normalizeBrowserBegin(raw);
}
export async function beginBrowserUploadToPathInBatch(
  remotePath: string,
  totalSize: number,
  policy: string,
  sourceMtime: number,
  batchId: string,
): Promise<BrowserUploadBegin> {
  const fn = loose['BeginBrowserUploadToPathInBatch'];
  if (typeof fn !== 'function') return beginBrowserUploadToPath(remotePath, totalSize, policy, sourceMtime);
  const id = (await fn(remotePath, totalSize, policy, sourceMtime, batchId)) as string;
  return { transferId: typeof id === 'string' ? id : '', remotePath };
}
export async function sendBrowserChunk(transferId: string, b64chunk: string): Promise<boolean> {
  const fn = loose['SendBrowserChunk'];
  if (typeof fn !== 'function') throw new Error('Sliced upload requires a newer Mac build.');
  return Boolean(await fn(transferId, b64chunk));
}
export async function abortBrowserUpload(transferId: string): Promise<void> {
  try {
    const fn = loose['AbortBrowserUpload'];
    if (typeof fn === 'function') await fn(transferId);
  } catch {
    // Abort is best-effort; the idle sweep reaps the session.
  }
}
export async function resumeUpload(transferId: string): Promise<string> {
  const fn = loose['ResumeUpload'];
  if (typeof fn !== 'function') throw new Error('Resume requires a newer Mac build.');
  return (await fn(transferId)) as string;
}
export async function resumeBrowserUpload(transferId: string): Promise<number> {
  const fn = loose['ResumeBrowserUpload'];
  if (typeof fn !== 'function') throw new Error('Resume requires a newer Mac build.');
  return Number(await fn(transferId)) || 0;
}
export async function rehashBrowserChunk(transferId: string, b64chunk: string): Promise<void> {
  const fn = loose['RehashBrowserChunk'];
  if (typeof fn !== 'function') throw new Error('Resume requires a newer Mac build.');
  await fn(transferId, b64chunk);
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
export async function uploadBrowserFileWithRelPath(b64: string, relPath: string, remoteDir: string): Promise<string> {
  const fn = loose['UploadBrowserFileWithRelPath'];
  if (typeof fn !== 'function') return uploadBrowserFile(b64, relPath.split('/').pop() || 'file', remoteDir);
  return (await fn(b64, relPath, remoteDir)) as string;
}
export async function renamePhone(from: string, to: string): Promise<string> {
  const fn = loose['RenamePhone'];
  if (typeof fn !== 'function') throw new Error('Rename requires newer Mac build.');
  return (await fn(from, to)) as string;
}
export async function prepareDownloadForDrag(remotePath: string): Promise<string> {
  const fn = loose['PrepareDownloadForDrag'];
  if (typeof fn !== 'function') throw new Error('Drag requires newer Mac build.');
  return (await fn(remotePath)) as string;
}
export async function pickDownloadDir(): Promise<string> {
  const fn = loose['PickDownloadDir'];
  if (typeof fn !== 'function') return '';
  try { return (await fn()) as string; } catch { return ''; }
}

export async function startFileDrag(remotePath: string, filename: string, size: number): Promise<boolean> {
  const fn = loose['StartFileDrag'];
  if (typeof fn !== 'function') return false;
  try {
    const res = (await fn(remotePath, filename, size)) as unknown;
    return Boolean(res);
  } catch {
    return false;
  }
}

// Photos (0.8.0 adds video: media_type photo|video, duration_ms)
export interface PhotoEntryView {
  photo_id: string;
  taken_at: number;
  width?: number;
  height?: number;
  mime?: string;
  size?: number;
  orientation?: number;
  media_type?: string;
  duration_ms?: number;
}
export function isVideoEntry(e: PhotoEntryView): boolean {
  return e.media_type === 'video';
}
export function formatDuration(ms?: number): string {
  if (!ms || ms <= 0) return '';
  const s = Math.round(ms / 1000);
  const m = Math.floor(s / 60);
  const r = s % 60;
  if (m >= 60) {
    const h = Math.floor(m / 60);
    return `${h}:${String(m % 60).padStart(2, '0')}:${String(r).padStart(2, '0')}`;
  }
  return `${m}:${String(r).padStart(2, '0')}`;
}
export interface PhotoListResult {
  entries: PhotoEntryView[];
  next_cursor?: string;
  error?: string;
  error_code?: string;
  permission?: string;
}
export interface PhotoThumbResult {
  photo_id: string;
  mime?: string;
  data_b64?: string;
  error?: string;
}
export interface PhotoDeleteItemView {
  photo_id: string;
  ok: boolean;
  error?: string;
  error_code?: string;
}
export interface PhotoDeleteResult {
  results: PhotoDeleteItemView[];
  error?: string;
  error_code?: string;
  permission?: string;
}
export interface PhotoTransferView {
  id: string;
  photo_id: string;
  status: string;
  progress: number;
  total_size: number;
  done_size: number;
  stream?: boolean;
  error?: string;
}

function normalizePhotoList(raw: unknown): PhotoListResult {
  const r = raw as Record<string, unknown>;
  const rawEntries = (r['entries'] ?? r['Entries'] ?? []) as unknown[];
  const entries: PhotoEntryView[] = (rawEntries as Record<string, unknown>[]).map((e) => ({
    photo_id: (e['photo_id'] ?? e['PhotoID'] ?? '') as string,
    taken_at: (e['taken_at'] ?? e['TakenAt'] ?? 0) as number,
    width: (e['width'] ?? e['Width'] ?? 0) as number,
    height: (e['height'] ?? e['Height'] ?? 0) as number,
    mime: (e['mime'] ?? e['Mime'] ?? '') as string,
    size: (e['size'] ?? e['Size'] ?? 0) as number,
    orientation: (e['orientation'] ?? e['Orientation'] ?? 0) as number,
    media_type: ((e['media_type'] ?? e['MediaType'] ?? '') as string) || undefined,
    duration_ms: (e['duration_ms'] ?? e['DurationMs'] ?? 0) as number,
  }));
  const pick = (a: unknown, b: unknown) => (typeof a === 'string' && a ? a : typeof b === 'string' && b ? b : undefined);
  return {
    entries,
    next_cursor: pick(r['next_cursor'], r['NextCursor']),
    error: pick(r['error'], r['Error']),
    error_code: pick(r['error_code'], r['ErrorCode']),
    permission: pick(r['permission'], r['Permission']),
  };
}

export async function listPhonePhotos(cursor: string, limit: number): Promise<PhotoListResult> {
  const fn = loose['ListPhonePhotos'];
  if (typeof fn !== 'function') throw new Error('Photos requires app 0.7.0 — update Mac and phone.');
  const res = (await fn(cursor, limit)) as unknown;
  return normalizePhotoList(res);
}
export async function requestPhotoThumb(photoId: string, thumbSize: number): Promise<PhotoThumbResult> {
  const fn = loose['RequestPhotoThumb'];
  if (typeof fn !== 'function') throw new Error('Photos requires app 0.7.0.');
  return ((await fn(photoId, thumbSize)) as PhotoThumbResult) ?? { photo_id: photoId };
}
export async function requestPhonePhoto(photoId: string, downloadDir: string): Promise<string> {
  const fn = loose['RequestPhonePhoto'];
  if (typeof fn !== 'function') throw new Error('Photos requires app 0.7.0.');
  return (await fn(photoId, downloadDir)) as string;
}
export async function requestPhoneMedia(photoId: string, mime: string, downloadDir: string): Promise<string> {
  const media = loose['RequestPhoneMedia'] as ((a: string, b: string, c: string) => Promise<string>) | undefined;
  if (typeof media === 'function') return (await media(photoId, mime, downloadDir)) as string;
  const legacy = loose['RequestPhonePhoto'] as ((a: string, b: string) => Promise<string>) | undefined;
  if (typeof legacy !== 'function') throw new Error('Photos requires app 0.7.0.');
  return (await legacy(photoId, downloadDir)) as string;
}
export async function startPhotoStream(photoId: string, mime: string): Promise<{ transferId: string; url: string }> {
  const fn = loose['StartPhotoStream'];
  if (typeof fn !== 'function') throw new Error('Video streaming requires app 0.8.0 — update Mac and phone.');
  const res = (await fn(photoId, mime)) as unknown;
  if (typeof res === 'string') return { transferId: res, url: '' };
  const r = res as Record<string, unknown>;
  return {
    transferId: (r['transferId'] ?? r['transferID'] ?? r['id'] ?? '') as string,
    url: (r['url'] ?? r['URL'] ?? '') as string,
  };
}
export async function deletePhonePhotos(photoIds: string[]): Promise<PhotoDeleteResult> {
  const fn = loose['DeletePhonePhotos'];
  if (typeof fn !== 'function') throw new Error('Photos requires app 0.7.0.');
  return ((await fn(photoIds)) as PhotoDeleteResult) ?? { results: [] };
}
export async function getPhotoTransfers(): Promise<PhotoTransferView[]> {
  const fn = loose['GetPhotoTransfers'];
  if (typeof fn !== 'function') return [];
  try {
    const res = (await fn()) as unknown;
    return (res as PhotoTransferView[]) ?? [];
  } catch { return []; }
}
export async function cancelPhotoTransfer(id: string): Promise<void> {
  const fn = loose['CancelPhotoTransfer'];
  if (typeof fn !== 'function') return;
  try { await fn(id); } catch { /* ignore */ }
}

export function isFilesPermissionError(res: { error?: string; error_code?: string; permission?: string }, message: string): boolean {
  if (res.permission === 'files' && res.error_code === 'permission_denied') return true;
  const m = `${res.error ?? ''} ${message}`;
  return m.includes('All files access') || m.toLowerCase().includes('all files');
}

export function isPhotosPermissionError(res: { error?: string; error_code?: string; permission?: string }, message: string): boolean {
  if (res.permission === 'photos' && res.error_code === 'permission_denied') return true;
  const m = `${res.error ?? ''} ${message}`.toLowerCase();
  return m.includes('photos access') || (m.includes('photos') && m.includes('permission'));
}

export * from './contacts_messages_api';

export { Service };
