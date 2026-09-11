<!--
  SPDX-License-Identifier: AGPL-3.0-only

  Consumer shell (HIG Windows, Sidebars, Split Views, Context Menus):
  no top strip, the native traffic lights float over the sidebar and the
  sidebar head owns the drag region, thin-divider split view with a device
  sidebar beside plain content sections (no boxes: hierarchy from type +
  hairlines + whitespace), custom context menus per target (the webview menu
  is suppressed in production builds; Option-right-click keeps it in dev
  builds). Fully opaque content, classic frost only on the sidebar.
  Appearance follows the system only (HIG Dark Mode: no app-specific
  appearance setting). Plain language everywhere: no addresses, no
  fingerprints in the primary UI (they hide under Advanced in the pair card).
  Typed backend state only (IsPaired, GetPeerDevice, GetUpdateNotice,
  GetLastDevice shim); never scrape log text.
-->
<script lang="ts">
  import { onMount } from 'svelte';
  import qrcode from 'qrcode-generator';
  import { TriangleAlert, Wifi, X, Zap } from '@lucide/svelte';
  import AppIcon from './components/AppIcon.svelte';
  import { Service, clearNotifications, dismissNotification, forgetLastDevice, friendlyPhoneAppsError, getAppVersion, getDefaultUploadDir, getDND, getKnownNotifApps, getLastDevice, getNotifications, getPairStatus, getPeerDevice, getPlayback, getSettings, isDemoMode, markNotificationsSeen, normalizeDND, normalizeNotifList, normalizePlayback, normalizeSettings, notifyLocalNetworkDown, reconnectToLastDevice, requestPhoneNotifApps, sendPlaybackCmd, setAppAllowed, setAppMuted, setClipboardAllowSensitive, setClipboardMode, setCustomName, setDefaultUploadDir, setDND, setNotifMode, setNotificationsEnabled, setPlaybackMode, setPlaybackOutput } from './backend';
  import type { AppSettings, DNDView, KnownNotifApp, LastDeviceNotice, NotifView, PairStatus, PlaybackView } from './backend';
  import { Events } from '@wailsio/runtime';
  import SourceList, { type SourceItem } from './components/SourceList.svelte';
  import DeviceHero from './components/DeviceHero.svelte';
  import PairCard from './components/PairCard.svelte';
  import NotificationsPane from './components/NotificationsPane.svelte';
  import MessagesPane from './components/MessagesPane.svelte';
  import ContactsPane from './components/ContactsPane.svelte';
  import MirrorPane from './components/MirrorPane.svelte';
  import MediaSlot from './components/MediaSlot.svelte';
  import SettingsPane from './components/SettingsPane.svelte';
  import FileManager from './components/FileManager/FileManager.svelte';
  import PhotosViewer from './components/FileManager/PhotosViewer.svelte';
  import NoticeRow from './components/NoticeRow.svelte';
  import RememberedGroup from './components/RememberedGroup.svelte';
  import RenameCard from './components/RenameCard.svelte';
  import ContextMenu, { type MenuItem } from './components/ContextMenu.svelte';
  import ConfirmDialog from './components/ConfirmDialog.svelte';
  import RightClickPing from './components/RightClickPing.svelte';
  import { resolveContextMenu } from './lib/contextMenuHelper';
  import { markThreadRead } from './contacts_messages_api';

  interface PairInfo {
    device_name: string;
    platform: string;
    host: string;
    port: number;
    code?: string;
  }

  // Typed backend state (single source of truth: never scrape log text).
  // UpdateNotice mirrors backend.UpdateNotice: Active = a version gate fired,
  // Self = this Mac is the outdated side, Message = canonical core text.
  // RequiredVersion/CurrentVersion are display-only ("", build-only peers).
  interface UpdateNotice {
    Active: boolean;
    Self: boolean;
    Message: string;
    RequiredVersion?: string;
    CurrentVersion?: string;
    RequiredBuild?: number;
  }

  interface MenuState {
    id: number;
    x: number;
    y: number;
    items: MenuItem[];
    onAction: (id: string) => void;
  }

  let menuSeq = 0;

  let pairJSON = $state('');
  let fingerprint = $state('');
  let log = $state<string[]>([]);
  let error = $state('');
  let copied = $state(false);
  let reconnecting = $state(false);
  let reconnectResult = $state('');
  let networkChanging = $state(false);
  let forgetting = $state(false);
  let forgetResult = $state('');
  let renaming = $state(false);
  let renameResult = $state('');
  let selectedId = $state('pair');
  let userSelected = $state(false);
  let menu = $state<MenuState | null>(null);
  let appVersion = $state('0.2.0');
  let showDisconnectConfirm = $state(false);
  let pings = $state<{ id: number; x: number; y: number }[]>([]);

  let pair = $derived.by<PairInfo | null>(() => {
    if (!pairJSON) return null;
    try {
      return JSON.parse(pairJSON) as PairInfo;
    } catch {
      return null;
    }
  });

  // hostPort feeds Advanced diagnostics and clipboard only. It never
  // renders in the primary UI: consumers see Online / Seen Xm ago.
  let hostPort = $derived(pair ? `${pair.host}:${pair.port}` : '');

  // The QR encodes the exact pair-payload JSON the phone scans.
  let qrSrc = $derived.by(() => {
    if (!pairJSON) return '';
    try {
      const qr = qrcode(0, 'M');
      qr.addData(pairJSON);
      qr.make();
      return qr.createDataURL(6, 4);
    } catch {
      return '';
    }
  });

  let paired = $state(false);
  let notice = $state<UpdateNotice | null>(null);
  let lastDevice = $state<LastDeviceNotice | null>(null);
  let peerDevice = $state<LastDeviceNotice | null>(null);
  let pairStatus = $state<PairStatus | null>(null);
  let demoMode = $state(false);
  let settings = $state<AppSettings>({ NotificationsEnabled: true, NotifMode: 'all_except_muted', MutedPackages: [], AllowedPackages: [], ClipboardMode: 'both', ClipboardAllowSensitive: false, PlaybackMode: 'both', PlaybackOutput: 'inapp', UpdatedUnix: 0, UpdatedBy: '' });
  let knownApps = $state<KnownNotifApp[]>([]);
  // Full phone inventory (labels + icons) fetched on demand when Settings
  // opens; knownApps stays the mirror fallback. phoneAppsAt guards
  // repeat selects within the backend cache TTL.
  let phoneApps = $state<KnownNotifApp[]>([]);
  let phoneAppsAt = $state(0);
  let phoneAppsLoading = $state(false);
  let phoneAppsError = $state('');
  let settingsApps = $derived(phoneApps.length ? phoneApps : knownApps);
  let settingsAppsSource = $derived<'phone' | 'mirror'>(phoneApps.length ? 'phone' : 'mirror');
  let notifItems = $state<NotifView[]>([]);
  let unseen = $state(0);
  let settingsSaving = $state(false);
  let settingsMsg = $state('');
  let defaultUploadDir = $state('');
  let clearingNotifs = $state(false);
  let playback = $state<PlaybackView | null>(null);
  let playbackBusy = $state('');
  let messagesRecipient = $state('');
  let messagesRecipientName = $state('');
  let unreadMessagesCount = $state(0);
  let dnd = $state<DNDView>({ Enabled: false, HasPermission: false, HasState: false, UpdatedMs: 0 });
  let busyDnd = $state(false);

  async function toggleDND() {
    if (!paired || busyDnd) return;
    busyDnd = true;
    const target = !dnd.Enabled;
    dnd = { ...dnd, Enabled: target, HasState: true };
    try {
      await setDND(target);
    } catch (e) {
      logInfo('Toggle DND failed', e);
      try {
        dnd = await getDND();
      } catch {}
    } finally {
      busyDnd = false;
    }
  }

  function handleMessageContact(address: string, displayName?: string) {
    messagesRecipient = address;
    messagesRecipientName = displayName || '';
    selectedId = 'messages';
  }

  let canPlaybackCommand = $derived(
    paired && (settings.PlaybackMode === 'both' || settings.PlaybackMode === 'mac_to_android'),
  );

  // Latest update notice from the typed binding. Self = this Mac is outdated;
  // otherwise the peer must update. The message is the canonical core text.
  let updateNotice = $derived(notice && notice.Active ? notice : null);

  function logInfo(msg: string, extra?: unknown) {
    if (extra !== undefined) console.log(`[FuseItAll] ${msg}`, extra);
    else console.log(`[FuseItAll] ${msg}`);
  }
  function logError(msg: string, extra?: unknown) {
    if (extra !== undefined) console.error(`[FuseItAll] ${msg}`, extra);
    else console.error(`[FuseItAll] ${msg}`);
  }

  // Display name: the Mac-local rename alias wins, else the phone's
  // advertised name, else the generic fallback. The phone re-advertises on
  // every presence announce, so clearing the alias reveals its current name.
  let displayName = $derived(peerDevice?.DisplayName || lastDevice?.DisplayName || 'Phone');

  // Live facts while paired, remembered facts while offline. Model and
  // battery render only when known; older phones advertise nothing.
  let deviceFacts = $derived(peerDevice ?? lastDevice);

  function batteryText(dev: LastDeviceNotice | null): string {
    if (!dev || dev.BatteryPct == null) return '';
    const pct = `${dev.BatteryPct}%`;
    return dev.Charging ? `${pct} · Charging` : pct;
  }

  let seenLabel = $derived(lastDevice ? fmtLastSeen(lastDevice.LastSeenUnix) : 'never');

  // Nav mirrors the reference window: six feature rows, no subtitles.
  let sources = $derived.by<SourceItem[]>(() => {
    const rows: SourceItem[] = [];
    if (paired || lastDevice) {
      rows.push({ id: 'files', label: 'Files', detail: '', state: 'none', icon: 'folder' });
      rows.push({ id: 'photos', label: 'Photos', detail: '', state: 'none', icon: 'image' });
      rows.push({ id: 'messages', label: 'Messages', detail: '', state: 'none', icon: 'message', badge: unreadMessagesCount || undefined });
      rows.push({ id: 'contacts', label: 'Contacts', detail: '', state: 'none', icon: 'contacts' });
      rows.push({ id: 'mirror', label: 'Mirror', detail: '', state: 'none', icon: 'mirror' });
      rows.push({ id: 'notifications', label: 'Notifications', detail: '', state: 'none', icon: 'bell', badge: unseen || undefined });
    }
    return rows;
  });

  let heroRows = $derived.by(() => {
    const rows = [{ label: 'Link', value: 'Wi-Fi' }];
    const model = deviceFacts?.Model;
    if (model) rows.push({ label: 'Model', value: model });
    const battery = batteryText(deviceFacts);
    if (battery) rows.push({ label: 'Battery', value: battery });
    if (paired) {
      return [{ label: 'Status', value: 'Online' }, ...rows];
    }
    return [...rows, { label: 'Last seen', value: seenLabel }];
  });

  let deviceModel = $derived(deviceFacts?.Model || deviceFacts?.DeviceName || displayName);

  // Cache identity for the Files/Photos RAM listings. A phone change
  // orphans the cache; a DHCP shuffle just costs one refetch.
  let peerKey = $derived(deviceFacts ? `${deviceFacts.Host}:${deviceFacts.Port}` : '');

  function fmtLastSeen(unix: number): string {
    if (!unix) return 'never';
    const secs = Math.max(0, Math.floor(Date.now() / 1000 - unix));
    if (secs < 60) return 'just now';
    const mins = Math.floor(secs / 60);
    if (mins < 60) return `${mins}m ago`;
    const hours = Math.floor(mins / 60);
    if (hours < 24) return `${hours}h ago`;
    return new Date(unix * 1000).toLocaleString();
  }

  async function refresh(): Promise<void> {
    try {
      const [pair, fp, lines, isPaired, update, remembered, peer, version, st, notifs, apps, play, uploadDefault, dndState, pStatus, demo] = await Promise.all([
        Service.GetPairJSON(),
        Service.GetFingerprint(),
        Service.GetLog(),
        Service.IsPaired(),
        Service.GetUpdateNotice(),
        getLastDevice(),
        getPeerDevice(),
        getAppVersion(),
        getSettings(),
        getNotifications(),
        getKnownNotifApps(),
        getPlayback(),
        getDefaultUploadDir(),
        getDND(),
        getPairStatus(),
        isDemoMode(),
      ]);
      pairJSON = pair;
      fingerprint = fp;
      log = lines ?? [];
      paired = isPaired ?? false;
      notice = update ?? null;
      lastDevice = remembered;
      peerDevice = peer;
      pairStatus = pStatus;
      demoMode = demo ?? false;
      appVersion = version || '0.2.0';
      settings = st;
      knownApps = apps;
      notifItems = notifs.Items;
      playback = play;
      defaultUploadDir = uploadDefault || '';
      dnd = dndState;
      if (selectedId === 'notifications') {
        // Reading the pane clears the badge; the refresh already shows the rows.
        if (notifs.Unseen > 0) void markNotificationsSeen();
        unseen = 0;
      } else {
        unseen = notifs.Unseen;
      }
      error = '';
      logInfo('refresh ok', {
        paired,
        displayName,
        lastSeen: lastDevice?.LastSeenUnix ?? 0,
        unseen,
        updateActive: notice?.Active ?? false,
        logTail: (lines ?? []).slice(-2),
      });
      if (notice?.Active) logInfo('update notice active', notice);
      // Cold open lands on the most relevant pane; later polls never
      // steal the selection once the user has chosen.
      if (!userSelected) {
        if (paired) selectedId = 'files';
        else if (remembered) selectedId = 'phone';
        else selectedId = 'pair';
      }
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e);
      error = msg;
      logError('refresh failed', msg);
    }
  }

  async function copyText(text: string): Promise<void> {
    if (!text) return;
    try {
      await navigator.clipboard.writeText(text);
      logInfo('copied to clipboard', text.slice(0, 40));
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e);
      error = msg;
      logError('copy failed', msg);
    }
  }

  async function copyCode(): Promise<void> {
    const code = pair?.code ?? '';
    if (!code) return;
    try {
      await navigator.clipboard.writeText(code);
      copied = true;
      logInfo('pair code copied');
      setTimeout(() => (copied = false), 2000);
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e);
      error = msg;
      logError('copy code failed', msg);
    }
  }

  async function saveName(name: string): Promise<void> {
    if (renaming) return;
    renaming = true;
    renameResult = '';
    logInfo('rename attempt', name);
    try {
      renameResult = await setCustomName(name);
      logInfo('rename result', renameResult);
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e);
      renameResult = msg;
      logError('rename failed', msg);
    } finally {
      renaming = false;
      await refresh();
    }
  }

  async function reconnect(): Promise<void> {
    if (reconnecting) return;
    reconnecting = true;
    reconnectResult = '';
    logInfo('reconnect start', { host: lastDevice?.Host, port: lastDevice?.Port });
    try {
      reconnectResult = await reconnectToLastDevice();
      logInfo('reconnect result', reconnectResult);
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e);
      reconnectResult = msg;
      logError('reconnect failed', msg);
    } finally {
      reconnecting = false;
      await refresh();
    }
  }

  async function forget(): Promise<void> {
    if (forgetting) return;
    forgetting = true;
    forgetResult = '';
    logInfo('forget phone start', { host: lastDevice?.Host });
    try {
      forgetResult = await forgetLastDevice();
      logInfo('forget result', forgetResult);
      selectedId = 'pair';
      userSelected = false;
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e);
      forgetResult = msg;
      logError('forget failed', msg);
    } finally {
      forgetting = false;
      await refresh();
    }
  }

  function openMenu(x: number, y: number, items: MenuItem[], onAction: (id: string) => void): void {
    menuSeq += 1;
    menu = { id: menuSeq, x, y, items, onAction };
  }

  function closeMenu(): void {
    menu = null;
  }

  function onContextMenu(e: MouseEvent): void {
    // Dev builds keep the native menu behind Option-right-click so the
    // web inspector stays reachable; production always shows app menus.
    if (import.meta.env.DEV && e.altKey) return;
    e.preventDefault();

    // Trigger tactile cursor micro-ping animation at click coordinates
    const pingId = Date.now() + Math.random();
    pings = [...pings, { id: pingId, x: e.clientX, y: e.clientY }];

    // Apply tactile spring depression micro-animation to clicked interactive target
    const target = e.target as HTMLElement | null;
    const clickable = target?.closest?.('button, [role="button"], [data-menu], .tile, .card, li, .photo-tile') as HTMLElement | null;
    if (clickable) {
      clickable.classList.remove('anim-target-depress');
      void clickable.offsetWidth; // force reflow
      clickable.classList.add('anim-target-depress');
      setTimeout(() => clickable?.classList.remove('anim-target-depress'), 250);
    }

    const res = resolveContextMenu(target, {
      paired,
      hasLastDevice: !!lastDevice,
      deviceName: deviceModel || 'Phone',
      selectedId,
      pairCode: pair?.code,
      pairHostPort: hostPort,
      fingerprint,
      handlers: {
        selectPane: select,
        copyText: (txt) => copyText(txt),
        reconnect: () => reconnect(),
        startRename: () => { renaming = true; },
        disconnect: () => { showDisconnectConfirm = true; },
        forget: () => forget(),
        refresh: () => refresh(),
        clearNotifs: () => { void clearNotifs(); },
        markNotifsSeen: () => markNotificationsSeen(),
        dismissNotif: (id) => { void dismissNotif(id); },
        mutePackage: (pkg) => { void toggleAppMuted(pkg, true); },
        sendPlaybackCmd: (cmd) => { void sendPlayback(cmd); },
        openAbout: () => { select('settings'); },
        deleteContact: (id, name, lookupKey) => {
          window.dispatchEvent(
            new CustomEvent('request-delete-contact', {
              detail: { contactId: id, contactName: name, lookupKey },
            }),
          );
        },
        markThreadRead: (threadId) => {
          void markThreadRead(threadId);
          window.dispatchEvent(
            new CustomEvent('request-mark-thread-read', {
              detail: { threadId },
            }),
          );
        },
      },
    });

    if (res && res.items.length > 0) {
      openMenu(e.clientX, e.clientY, res.items, (id) => {
        closeMenu();
        res.onPick(id);
      });
    }
  }

  function select(id: string): void {
    logInfo('nav select', id);
    selectedId = id;
    userSelected = true;
    if (id === 'notifications' && unseen > 0) {
      unseen = 0;
      void markNotificationsSeen().then(() => refresh());
    }
    if (id === 'settings' && paired && !phoneAppsLoading && Date.now() - phoneAppsAt > 30_000) {
      void fetchPhoneApps(false);
    }
  }

  async function fetchPhoneApps(refresh: boolean): Promise<void> {
    if (phoneAppsLoading || !paired) return;
    phoneAppsLoading = true;
    if (refresh) phoneAppsError = '';
    try {
      const apps = await requestPhoneNotifApps(refresh);
      if (apps.length) {
        phoneApps = apps;
        phoneAppsAt = Date.now();
        phoneAppsError = '';
        logInfo('phone apps loaded', apps.length);
      } else if (refresh) {
        phoneAppsError = "Couldn't load phone apps — showing mirrored apps.";
      }
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e);
      phoneAppsError = friendlyPhoneAppsError(msg);
      logError('phone apps failed', msg);
    } finally {
      phoneAppsLoading = false;
    }
  }

  let settingsUpdatedLabel = $derived(settings.UpdatedUnix ? fmtLastSeen(settings.UpdatedUnix) : 'never');

  async function toggleNotif(enabled: boolean): Promise<void> {
    if (settingsSaving) return;
    settingsSaving = true;
    settingsMsg = '';
    logInfo('notifications toggle', enabled);
    // Optimistic: update UI immediately, refresh will reconcile.
    const nowOptimistic = Math.floor(Date.now() / 1000);
    settings = { ...settings, NotificationsEnabled: enabled, UpdatedUnix: nowOptimistic, UpdatedBy: 'mac' };
    try {
      settingsMsg = await setNotificationsEnabled(enabled);
      logInfo('notifications toggle result', settingsMsg);
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e);
      settingsMsg = msg;
      logError('notifications toggle failed', msg);
    } finally {
      settingsSaving = false;
      await refresh();
    }
  }

  async function setClipMode(mode: string): Promise<void> {
    if (settingsSaving) return;
    settingsSaving = true;
    settingsMsg = '';
    logInfo('clipboard mode set', mode);
    // Optimistic: reflect choice instantly before poll.
    const nowOptimistic = Math.floor(Date.now() / 1000);
    settings = { ...settings, ClipboardMode: mode, UpdatedUnix: nowOptimistic, UpdatedBy: 'mac' };
    try {
      settingsMsg = await setClipboardMode(mode);
      logInfo('clipboard mode result', settingsMsg);
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e);
      settingsMsg = msg;
      logError('clipboard mode failed', msg);
    } finally {
      settingsSaving = false;
      await refresh();
    }
  }

  async function setClipSensitive(allow: boolean): Promise<void> {
    if (settingsSaving) return;
    settingsSaving = true;
    settingsMsg = '';
    settings = { ...settings, ClipboardAllowSensitive: allow, UpdatedUnix: Math.floor(Date.now() / 1000), UpdatedBy: 'mac' };
    try {
      settingsMsg = await setClipboardAllowSensitive(allow);
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e);
      settingsMsg = msg;
    } finally {
      settingsSaving = false;
      await refresh();
    }
  }

  async function setFilterMode(mode: string): Promise<void> {
    if (settingsSaving) return;
    settingsSaving = true;
    settingsMsg = '';
    logInfo('notification filter mode set', mode);
    const nowOptimistic = Math.floor(Date.now() / 1000);
    settings = { ...settings, NotifMode: mode, UpdatedUnix: nowOptimistic, UpdatedBy: 'mac' };
    try {
      settingsMsg = await setNotifMode(mode);
      logInfo('notification filter result', settingsMsg);
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e);
      settingsMsg = msg;
      logError('notification filter failed', msg);
    } finally {
      settingsSaving = false;
      await refresh();
    }
  }

  async function setPlaybackModeFn(mode: string): Promise<void> {
    if (settingsSaving) return;
    settingsSaving = true;
    settingsMsg = '';
    logInfo('playback mode set', mode);
    const nowOptimistic = Math.floor(Date.now() / 1000);
    settings = { ...settings, PlaybackMode: mode, UpdatedUnix: nowOptimistic, UpdatedBy: 'mac' };
    try {
      settingsMsg = await setPlaybackMode(mode);
      logInfo('playback mode result', settingsMsg);
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e);
      settingsMsg = msg;
      logError('playback mode failed', msg);
    } finally {
      settingsSaving = false;
      await refresh();
    }
  }

  async function setPlaybackOutputFn(output: string): Promise<void> {
    if (settingsSaving) return;
    settingsSaving = true;
    settingsMsg = '';
    logInfo('playback output set', output);
    const nowOptimistic = Math.floor(Date.now() / 1000);
    settings = { ...settings, PlaybackOutput: output, UpdatedUnix: nowOptimistic, UpdatedBy: 'mac' };
    try {
      settingsMsg = await setPlaybackOutput(output);
      logInfo('playback output result', settingsMsg);
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e);
      settingsMsg = msg;
      logError('playback output failed', msg);
    } finally {
      settingsSaving = false;
      await refresh();
    }
  }

  async function sendPlayback(cmd: string): Promise<void> {
    if (playbackBusy) return;
    playbackBusy = cmd;
    logInfo('playback command', cmd);
    try {
      if (!canPlaybackCommand && paired) {
        await setPlaybackModeFn('both');
      }
      settingsMsg = await sendPlaybackCmd(cmd);
      logInfo('playback command result', settingsMsg);
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e);
      settingsMsg = msg;
      error = msg;
      logError('playback command failed', msg);
    } finally {
      playbackBusy = '';
      await refresh();
    }
  }

  async function toggleAppMuted(pkg: string, muted: boolean): Promise<void> {
    if (settingsSaving) return;
    settingsSaving = true;
    settingsMsg = '';
    const cur = muted
      ? [...settings.MutedPackages, pkg].filter((v, i, a) => a.indexOf(v) === i).sort().slice(0, 100)
      : settings.MutedPackages.filter((p) => p !== pkg);
    settings = { ...settings, MutedPackages: cur, UpdatedUnix: Math.floor(Date.now() / 1000), UpdatedBy: 'mac' };
    try {
      settingsMsg = await setAppMuted(pkg, muted);
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e);
      settingsMsg = msg;
      logError('app mute failed', msg);
    } finally {
      settingsSaving = false;
      await refresh();
    }
  }

  async function toggleAppAllowed(pkg: string, allowed: boolean): Promise<void> {
    if (settingsSaving) return;
    settingsSaving = true;
    settingsMsg = '';
    const cur = allowed
      ? [...settings.AllowedPackages, pkg].filter((v, i, a) => a.indexOf(v) === i).sort().slice(0, 100)
      : settings.AllowedPackages.filter((p) => p !== pkg);
    settings = { ...settings, AllowedPackages: cur, UpdatedUnix: Math.floor(Date.now() / 1000), UpdatedBy: 'mac' };
    try {
      settingsMsg = await setAppAllowed(pkg, allowed);
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e);
      settingsMsg = msg;
      logError('app allow failed', msg);
    } finally {
      settingsSaving = false;
      await refresh();
    }
  }

  async function setUploadDefault(dir: string): Promise<void> {
    if (settingsSaving) return;
    settingsSaving = true;
    settingsMsg = '';
    try {
      settingsMsg = await setDefaultUploadDir(dir.trim());
      defaultUploadDir = dir.trim();
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e);
      settingsMsg = msg;
      logError('upload default failed', msg);
    } finally {
      settingsSaving = false;
      await refresh();
    }
  }

  async function dismissNotif(id: string): Promise<void> {
    logInfo('dismiss notification', id);
    try {
      await dismissNotification(id);
      logInfo('dismiss ok', id);
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e);
      error = msg;
      logError('dismiss failed', msg);
    } finally {
      await refresh();
    }
  }

  async function clearNotifs(): Promise<void> {
    if (clearingNotifs) return;
    clearingNotifs = true;
    logInfo('clear notifications');
    try {
      await clearNotifications();
      logInfo('clear notifications ok');
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e);
      error = msg;
      logError('clear failed', msg);
    } finally {
      clearingNotifs = false;
      await refresh();
    }
  }

  onMount(() => {
    // Cold start (including the return from a demo session) heals with one
    // reconnect attempt when a real phone is remembered but offline.
    // One-shot on this launch event only — never a timer.
    void (async () => {
      await refresh();
      if (!paired && lastDevice && !demoMode) {
        networkChanging = true;
        try { await reconnectToLastDevice(); } catch { /* phone on other WiFi: stay on last-device card */ }
        finally { networkChanging = false; await refresh(); }
      }
    })();
    // OS network push (not polling): browser online/offline fires on WiFi
    // drop/regain. Offline drops the half-open peer instantly via backend;
    // online refreshes once and best-effort redials (DHCP/WiFi switch heal).
    // One-shot fetches only, never a timer.
    const onOffline = () => {
      networkChanging = true;
      void (async () => {
        await refresh();
        // Guard against spurious webview offline events: if we heard from
        // the phone over LAN seconds ago, trust the fresher signal and skip
        // the nuke — a real outage still converges via the 5s WS watchdog.
        const seen = peerDevice?.LastSeenUnix ?? lastDevice?.LastSeenUnix ?? 0;
        const ageS = seen > 0 ? Date.now() / 1000 - seen : Number.POSITIVE_INFINITY;
        if (ageS < 10) return;
        try { await notifyLocalNetworkDown(); } catch { /* older build or already offline */ }
        await refresh();
      })().finally(() => { networkChanging = false; });
    };
    const onOnline = () => {
      networkChanging = true;
      void (async () => {
        await refresh();
        try { await reconnectToLastDevice(); } catch { /* phone on other WiFi: stay on last-device card */ }
        await refresh();
      })().finally(() => { networkChanging = false; });
    };
    window.addEventListener('offline', onOffline);
    window.addEventListener('online', onOnline);
    let offState: (() => void) | null = null;
    let offNotifs: (() => void) | null = null;
    let offSettings: (() => void) | null = null;
    let offPlayback: (() => void) | null = null;
    let offDnd: (() => void) | null = null;
    try {
      offState = Events.On('state:changed', (e: unknown) => {
        const data = ((e as { data?: unknown })?.data ?? e) as {
          paired?: boolean;
          peerDevice?: LastDeviceNotice | null;
          lastDevice?: LastDeviceNotice | null;
        };
        if (data && typeof data === 'object') {
          if (data.paired !== undefined) paired = data.paired;
          if (data.peerDevice !== undefined) peerDevice = data.peerDevice;
          if (data.lastDevice !== undefined) lastDevice = data.lastDevice;
          // Pairing listener stamps (accept/reject) changed: refresh the
          // typed status once so the PairCard flips off "no attempt yet".
          void getPairStatus().then((s) => { pairStatus = s; }).catch(() => {});
          if (!userSelected) {
            if (paired) selectedId = 'files';
            else if (lastDevice) selectedId = 'phone';
            else selectedId = 'pair';
          }
        }
      });
      offNotifs = Events.On('notifs:changed', (e: unknown) => {
        const data = ((e as { data?: unknown })?.data ?? e) as {
          Items?: NotifView[];
          Unseen?: number;
        };
        if (data && typeof data === 'object') {
          // Same shape guarantees as the poll path: a malformed live
          // payload must never freeze the pane on a render throw.
          if (data.Items) notifItems = normalizeNotifList(data).Items;
          if (selectedId === 'notifications') {
            if (data.Unseen && data.Unseen > 0) void markNotificationsSeen();
            unseen = 0;
          } else if (data.Unseen !== undefined) {
            unseen = data.Unseen;
          }
        }
      });
      offSettings = Events.On('settings:changed', (e: unknown) => {
        const data = ((e as { data?: unknown })?.data ?? e) as AppSettings;
        if (data && typeof data === 'object' && ('NotificationsEnabled' in data || 'NotifMode' in data || 'notif_mode' in data || 'PlaybackMode' in data || 'playback_mode' in data)) {
          settings = normalizeSettings(data);
        }
      });
      offPlayback = Events.On('playback:changed', (e: unknown) => {
        const data = ((e as { data?: unknown })?.data ?? e) as PlaybackView;
        if (data && typeof data === 'object') {
          playback = normalizePlayback(data);
        }
      });
      offDnd = Events.On('dnd:changed', (e: unknown) => {
        const data = ((e as { data?: unknown })?.data ?? e) as DNDView;
        if (data && typeof data === 'object') {
          dnd = normalizeDND(data);
        }
      });
    } catch {
      // Outside Wails (browser dev)
    }

    // Prevent accidental browser zoom (Cmd +, Cmd -, Cmd 0) and wheel pinch-zoom, plus global app shortcuts
    const onGlobalKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && (e.key === '=' || e.key === '+' || e.key === '-' || e.key === '0')) {
        e.preventDefault();
        return;
      }

      // App navigation shortcuts (Cmd+1 through Cmd+7, Cmd+. and Cmd+, for settings)
      if (e.metaKey || e.ctrlKey) {
        if (e.key === '.' || e.key === ',') {
          e.preventDefault();
          select('settings');
          return;
        }

        const navKeys: Record<string, string> = {
          '1': paired || lastDevice ? 'phone' : 'pair',
          '2': 'files',
          '3': 'photos',
          '4': 'messages',
          '5': 'contacts',
          '6': 'mirror',
          '7': 'notifications',
        };

        if (navKeys[e.key]) {
          const targetPane = navKeys[e.key];
          e.preventDefault();
          if (paired || lastDevice || targetPane === 'pair' || targetPane === 'phone' || targetPane === 'settings') {
            select(targetPane);
          }
          return;
        }
      }
    };
    const onGlobalWheel = (e: WheelEvent) => {
      if (e.ctrlKey) {
        e.preventDefault();
      }
    };
    // Prevent accidental navigation when dragging files over non-drop surfaces
    const onGlobalDragOver = (e: DragEvent) => {
      const isDropTarget = (e.target as HTMLElement | null)?.closest?.('[data-file-drop-target]');
      if (!isDropTarget) {
        e.preventDefault();
      }
    };
    window.addEventListener('keydown', onGlobalKey);
    window.addEventListener('wheel', onGlobalWheel, { passive: false });
    window.addEventListener('dragover', onGlobalDragOver);

    return () => {
      window.removeEventListener('offline', onOffline);
      window.removeEventListener('online', onOnline);
      window.removeEventListener('keydown', onGlobalKey);
      window.removeEventListener('wheel', onGlobalWheel);
      window.removeEventListener('dragover', onGlobalDragOver);
      try { offState?.(); } catch { /* ignore */ }
      try { offNotifs?.(); } catch { /* ignore */ }
      try { offSettings?.(); } catch { /* ignore */ }
      try { offPlayback?.(); } catch { /* ignore */ }
      try { offDnd?.(); } catch { /* ignore */ }
    };
  });
</script>

<main class="flex h-[100dvh] w-full flex-col overflow-hidden bg-transparent" oncontextmenu={onContextMenu}>

  {#if error}
    <div role="alert" class="anim-row flex items-center gap-2 bg-control px-4 py-2">
      <TriangleAlert size={13} strokeWidth={2} class="flex-none text-bad" aria-hidden="true" />
      <p class="min-w-0 flex-1 text-[12px] leading-snug text-label">{error}</p>
      <button
        type="button"
        onclick={() => (error = '')}
        class="flex-none rounded px-1.5 py-0.5 text-[11px] font-medium text-tertiary transition hover:bg-altrow hover:text-label"
      >Dismiss</button>
    </div>
  {/if}

  {#if updateNotice}
    <NoticeRow
      kind={updateNotice.Self ? 'self' : 'peer'}
      message={updateNotice.Message}
      requiredVersion={updateNotice.RequiredVersion ?? ''}
      currentVersion={updateNotice.CurrentVersion ?? ''}
    />
  {/if}

  <div class="flex min-h-0 flex-1 flex-col md:flex-row">
    <nav aria-label="Devices" class="frost-side no-scrollbar flex w-full flex-none flex-col overflow-x-hidden overflow-y-auto md:w-[232px] bg-window">
      <!-- Drag clearance for the floating traffic lights (Wails HiddenInset):
           transparent, no tint, no divider. Interactive rows below opt out
           implicitly: only this spacer carries the drag handle. -->
      <div class="h-7 w-full flex-none pl-20" style="--wails-draggable: drag;" aria-hidden="true"></div>
      {#if paired || lastDevice}
        <div class="flex flex-col items-center px-4 pb-1 pt-1 text-center">
          <button
            type="button"
            onclick={() => select('phone')}
            aria-label="Phone details"
            class="flex h-16 w-16 items-center justify-center rounded-full bg-control transition hover:bg-hover"
          >
            <AppIcon size={34} label="FuseItAll icon" />
          </button>
          <button
            type="button"
            onclick={() => select('phone')}
            class="mt-2 max-w-full truncate text-[13px] font-semibold text-label"
          >{displayName}</button>
          {#if demoMode}
            <span class="pill pill-neutral mt-1.5 !h-[20px] !px-2 !text-[11px]" title="Demo data — your real phone reconnects when you restart without demo.">Demo</span>
          {/if}
          <div class="mt-1.5 flex w-full items-center justify-center gap-2">
            <span class="flex min-w-0 items-center gap-1.5 text-[12px] font-medium {paired ? 'text-ok' : 'text-warn'}">
              <span class="h-2 w-2 flex-none rounded-full {paired ? 'bg-ok anim-live-dot' : 'bg-warn'}" aria-hidden="true"></span>
              <span class="truncate">{paired ? 'Connected' : `Seen ${seenLabel}`}</span>
            </span>
            {#if paired}
              <span class="pill pill-neutral !h-[20px] !px-2 !text-[11px]">
                <Wifi size={11} strokeWidth={2.5} aria-hidden="true" />
                <span>WiFi</span>
              </span>
            {/if}
            {#if paired || lastDevice}
              <button
                type="button"
                onclick={() => (showDisconnectConfirm = true)}
                aria-label="Disconnect phone"
                title="Disconnect phone"
                class="flex h-5 w-5 flex-none items-center justify-center rounded-full bg-bad/15 text-bad transition hover:bg-bad/25 active:translate-y-[1px]"
              ><X size={12} strokeWidth={2.5} aria-hidden="true" /></button>
            {/if}
          </div>
          <div class="mt-1.5 flex w-full items-center justify-center gap-1.5 text-[12px] tabular-nums text-secondary">
            <span class="truncate">{deviceModel}</span>
            {#if paired}<span class="h-1.5 w-1.5 flex-none rounded-full bg-accent" aria-label="Secured link" title="Secured link"></span>{/if}
          </div>
          {#if deviceFacts?.BatteryPct != null}
            {@const batteryPct = Math.max(0, Math.min(100, deviceFacts.BatteryPct))}
            {@const batteryLow = batteryPct <= 20}
            {@const batteryCharging = deviceFacts.Charging === true}
            {@const batteryLabel = `Phone battery ${batteryPct} percent${batteryCharging ? ', charging' : ''}${batteryLow && !batteryCharging ? ', low' : ''}`}
            {@const batteryIdleLow = batteryLow && !batteryCharging}
            <div class="mt-1.5 flex items-center justify-center gap-1.5" role="img" aria-label={batteryLabel}>
              <span class="relative flex h-3.5 w-7 items-center rounded-md bg-control p-[2px]" aria-hidden="true">
                <span
                  class="block h-full rounded-[3px] {batteryCharging ? 'bg-ok' : batteryIdleLow ? 'bg-bad' : 'bg-secondary'}"
                  style="width: {Math.max(6, batteryPct)}%"
                ></span>
                {#if batteryCharging}
                  <Zap size={9} fill="currentColor" strokeWidth={2.5} class="absolute inset-0 m-auto text-accent-text drop-shadow-sm" aria-hidden="true" />
                {/if}
                <span class="absolute -right-[3.5px] top-1/2 h-1.5 w-[2.5px] -translate-y-1/2 rounded-r-full {batteryIdleLow ? 'bg-bad' : 'bg-separator'}"></span>
              </span>
              <span class="text-[11px] tabular-nums {batteryCharging ? 'text-ok' : batteryIdleLow ? 'text-bad' : 'text-secondary'}" aria-hidden="true">{batteryPct}%</span>
            </div>
          {/if}
        </div>
        <div class="mx-4 mt-3" aria-hidden="true"></div>
        <MediaSlot layout="controls" dnd={dnd} busyDnd={busyDnd} onToggleDnd={toggleDND} paired={paired} />
        {#if sources.length}
          <SourceList group="Navigation" items={sources} selectedId={selectedId} onSelect={select} />
        {/if}
      {:else}
        <div class="flex flex-col items-center px-4 pb-1 pt-8 text-center">
          <span class="flex h-16 w-16 items-center justify-center rounded-full bg-control" aria-hidden="true">
            <AppIcon size={34} label="FuseItAll icon" />
          </span>
          <p class="mt-3 text-[13px] font-semibold text-label">Link your first phone</p>
          <p class="mt-1 px-2 text-[11px] leading-relaxed text-tertiary">Scan the code to get started.</p>
        </div>
      {/if}

      <div class="min-h-0 min-w-0 flex-1"></div>

      {#if paired || lastDevice}
        <div class="min-w-0 w-full">
          <MediaSlot layout="player" playback={playback} paired={paired} canCommand={canPlaybackCommand} busyCmd={playbackBusy} onCommand={sendPlayback} onEnableControl={() => void setPlaybackModeFn('both')} />
        </div>
      {/if}
      <div class="px-2 pb-2 pt-1">
        <div class="pt-1">
          <button
            type="button"
            data-source-id="settings"
            onclick={() => select('settings')}
            aria-current={selectedId === 'settings'}
            class="flex w-full items-center gap-2.5 rounded-lg px-2.5 py-[7px] text-left transition {selectedId === 'settings' ? 'bg-accent' : 'hover:bg-altrow'}"
          >
            <svg width="17" height="17" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" class="flex-none {selectedId === 'settings' ? 'text-accent-text' : 'text-secondary'}" aria-hidden="true"><path d="M12 15a3 3 0 1 0 0-6 3 3 0 0 0 0 6Z"/><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09a1.65 1.65 0 0 0-1-1.51 1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06A1.65 1.65 0 0 0 9 15a1.65 1.65 0 0 0-1-1.51V13a1.65 1.65 0 0 0 1-1.51A1.65 1.65 0 0 0 7.18 9.67l-.06-.06a2 2 0 0 1 2.83-2.83l.06.06A1.65 1.65 0 0 0 11.82 7.17a1.65 1.65 0 0 0 1-1.51V5a2 2 0 0 1 4 0v.67a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06A1.65 1.65 0 0 0 19.4 11a1.65 1.65 0 0 0 1 1.51V13a1.65 1.65 0 0 0-1 1Z"/></svg>
            <span class="min-w-0 flex-1 truncate text-[13px] font-medium {selectedId === 'settings' ? 'text-accent-text' : 'text-label'}">Settings</span>
          </button>
        </div>
      </div>
    </nav>

    <div class="flex min-h-0 min-w-[280px] flex-1 flex-col overflow-hidden bg-window">
      <!-- Boundary: a render throw used to freeze the previous pane in
        place while the sidebar moved on. Now it surfaces this card. -->
      <svelte:boundary>
      {#if (selectedId === 'files' || selectedId === 'photos' || selectedId === 'messages' || selectedId === 'contacts') && (paired || lastDevice)}
        <div class="flex min-h-0 flex-1 flex-col overflow-hidden">
          <div class="min-h-0 flex-1 {selectedId === 'files' ? 'flex flex-col overflow-hidden' : 'hidden'}">
            <FileManager paired={paired} deviceLabel={deviceModel} active={selectedId === 'files'} peerKey={peerKey} />
          </div>
          <div class="min-h-0 flex-1 {selectedId === 'photos' ? 'flex flex-col overflow-hidden' : 'hidden'}">
            <PhotosViewer paired={paired} deviceLabel={deviceModel} active={selectedId === 'photos'} peerKey={peerKey} />
          </div>
          <div class="min-h-0 flex-1 {selectedId === 'messages' ? 'flex flex-col overflow-hidden' : 'hidden'}">
            <MessagesPane
              paired={paired}
              deviceLabel={deviceModel}
              initialRecipient={messagesRecipient}
              initialDisplayName={messagesRecipientName}
              onClearRecipient={() => { messagesRecipient = ''; messagesRecipientName = ''; }}
              onUnreadCountChange={(count) => { unreadMessagesCount = count; }}
            />
          </div>
          <div class="min-h-0 flex-1 {selectedId === 'contacts' ? 'flex flex-col overflow-hidden' : 'hidden'}">
            <ContactsPane
              paired={paired}
              deviceLabel={deviceModel}
              onMessageContact={handleMessageContact}
            />
          </div>
        </div>
      {:else}
        <div class="anim-stagger-children flex min-h-0 flex-1 flex-col gap-3 overflow-y-auto p-4">
          {#if selectedId === 'notifications' && (paired || lastDevice)}
            <NotificationsPane
              items={notifItems}
              paired={paired}
              clearing={clearingNotifs}
              onDismiss={dismissNotif}
              onClear={clearNotifs}
            />
          {:else if selectedId === 'mirror' && (paired || lastDevice)}
            <MirrorPane paired={paired} deviceLabel={deviceModel} />
      {:else if selectedId === 'settings'}
        <SettingsPane
          settings={settings}
          knownApps={settingsApps}
          appsSource={settingsAppsSource}
          appsLoading={phoneAppsLoading}
          appsError={phoneAppsError}
          saving={settingsSaving}
          message={settingsMsg}
          updatedLabel={settingsUpdatedLabel}
          appVersion={appVersion}
          defaultUploadDir={defaultUploadDir}
          onNotifToggle={toggleNotif}
          onNotifMode={setFilterMode}
          onAppMuted={toggleAppMuted}
          onAppAllowed={toggleAppAllowed}
          onClipboardMode={setClipMode}
          onClipboardAllowSensitive={setClipSensitive}
          onPlaybackMode={setPlaybackModeFn}
          onPlaybackOutput={setPlaybackOutputFn}
          onUploadDefault={setUploadDefault}
          onAppsRefresh={() => fetchPhoneApps(true)}
        />
      {:else if selectedId === 'phone' && (paired || lastDevice)}
        {#if paired}
          <div class="flex flex-col">
            <DeviceHero
              title={displayName}
              subtitle={demoMode ? 'Demo data — no real phone connected' : 'Connected over Wi-Fi'}
              statusKind="ok"
              statusLabel={demoMode ? 'Demo' : 'Online'}
              rows={heroRows}
              note={demoMode ? 'Demo data — restart without demo to see your real phone.' : networkChanging ? 'Network changed — reconnecting to the phone…' : 'Presence refreshes on its own.'}
            />
            <RenameCard
              displayName={displayName}
              advertisedName={deviceFacts?.DeviceName || ''}
              saving={renaming}
              message={renameResult}
              onSave={saveName}
            />
          </div>
        {:else if lastDevice}
          <div class="flex flex-col">
            <DeviceHero
              title={displayName}
              subtitle="Not in reach right now"
              statusKind="warn"
              statusLabel={`Seen ${seenLabel}`}
              rows={heroRows}
              note={networkChanging ? 'Network changed — looking for the phone…' : 'It reconnects on its own once it is back on your Wi-Fi.'}
            />
            <RenameCard
              displayName={displayName}
              advertisedName={deviceFacts?.DeviceName || ''}
              saving={renaming}
              message={renameResult}
              onSave={saveName}
            />
            <RememberedGroup
              seenLabel={seenLabel}
              reconnecting={reconnecting}
              reconnectResult={reconnectResult}
              forgetting={forgetting}
              forgetResult={forgetResult}
              onReconnect={reconnect}
              onForget={forget}
            />
          </div>
        {/if}
      {:else}
        <div data-menu="pairing">
          <PairCard
            qrSrc={qrSrc}
            code={pair?.code ?? ''}
            copied={copied}
            hostPort={hostPort}
            fingerprint={fingerprint}
            pairStatus={pairStatus}
            onCopyCode={copyCode}
          />
        </div>
      {/if}
        </div>
      {/if}
        {#snippet failed(boundaryError, reset)}
          {@const _ = console.error('svelte:boundary snag caught:', boundaryError)}
          <div class="flex min-h-0 flex-1 flex-col gap-3 overflow-y-auto p-4">
            <div class="mx-auto flex max-w-[420px] flex-col items-center px-6 py-16 text-center" role="alert">
              <p class="text-[13px] font-semibold text-label">This page hit a snag</p>
              <p class="mt-1 max-w-[36ch] text-[12px] leading-relaxed text-secondary">{boundaryError instanceof Error ? boundaryError.message : 'The phone sent an update this page could not read, so it stayed put. Nothing was lost.'}</p>
              <button
                type="button"
                onclick={() => reset()}
                class="mt-4 inline-flex h-7 items-center rounded-md bg-accent px-3 text-[13px] font-medium text-accent-text transition hover:brightness-95 active:translate-y-[1px]"
              >Try again</button>
            </div>
          </div>
        {/snippet}
      </svelte:boundary>
    </div>
  </div>

  {#if menu}
    {#key menu?.id}
      <ContextMenu x={menu.x} y={menu.y} items={menu.items} onPick={menu.onAction} onClose={closeMenu} />
    {/key}
  {/if}

  {#each pings as p (p.id)}
    <RightClickPing x={p.x} y={p.y} onDone={() => { pings = pings.filter(item => item.id !== p.id); }} />
  {/each}

  <ConfirmDialog
    open={showDisconnectConfirm}
    title="Disconnect phone?"
    body="This clears the live connection. The phone will reconnect automatically when it is back on Wi-Fi, or you can tap Reconnect."
    confirmLabel="Disconnect"
    destructive={false}
    onConfirm={() => { showDisconnectConfirm = false; logInfo('disconnect requested'); }}
    onCancel={() => (showDisconnectConfirm = false)}
  />
</main>
