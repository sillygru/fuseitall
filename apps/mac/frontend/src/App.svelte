<!--
  Copyright (C) 2026 FuseItAll contributors.

  This program is free software: you can redistribute it and/or modify
  it under the terms of the GNU Affero General Public License as published
  by the Free Software Foundation, version 3 of the License. See LICENSE
  for details.

  Consumer shell (HIG Windows, Toolbars, Sidebars, Split Views,
  Context Menus): toolbar with content title plus one prominent action,
  thin-divider split view with a device sidebar beside card content,
  custom context menus per target (the webview menu is suppressed in
  production builds; Option-right-click keeps it in dev builds). Fully
  opaque, no translucency beyond the classic frosted chrome. Appearance
  follows the system only (HIG Dark Mode: no app-specific appearance
  setting). Plain language everywhere: no addresses, no fingerprints in
  the primary UI (they hide under Advanced in the pair card).
  Typed backend state only (IsPaired, GetPeerDevice, GetUpdateNotice,
  GetLastDevice shim); never scrape log text.
-->
<script lang="ts">
  import { onMount } from 'svelte';
  import qrcode from 'qrcode-generator';
  import { TriangleAlert, Wifi, X } from '@lucide/svelte';
  import AppIcon from './components/AppIcon.svelte';
  import { Service, clearNotifications, dismissNotification, forgetLastDevice, getAppVersion, getLastDevice, getNotifications, getPeerDevice, getSettings, markNotificationsSeen, normalizeNotifList, reconnectToLastDevice, setClipboardMode, setCustomName, setNotificationsEnabled } from './backend';
  import type { AppSettings, LastDeviceNotice, NotifView } from './backend';
  import { Events } from '@wailsio/runtime';
  import Toolbar from './components/Toolbar.svelte';
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
    x: number;
    y: number;
    items: MenuItem[];
    onAction: (id: string) => void;
  }

  let pairJSON = $state('');
  let fingerprint = $state('');
  let log = $state<string[]>([]);
  let error = $state('');
  let copied = $state(false);
  let reconnecting = $state(false);
  let reconnectResult = $state('');
  let forgetting = $state(false);
  let forgetResult = $state('');
  let renaming = $state(false);
  let renameResult = $state('');
  let selectedId = $state('pair');
  let userSelected = $state(false);
  let menu = $state<MenuState | null>(null);
  let appVersion = $state('0.2.0');
  let showDisconnectConfirm = $state(false);

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
  let settings = $state<AppSettings>({ NotificationsEnabled: true, ClipboardMode: 'both', UpdatedUnix: 0, UpdatedBy: '' });
  let notifItems = $state<NotifView[]>([]);
  let unseen = $state(0);
  let settingsSaving = $state(false);
  let settingsMsg = $state('');
  let clearingNotifs = $state(false);

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
  // every heartbeat, so clearing the alias reveals its current name.
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
      rows.push({ id: 'messages', label: 'Messages', detail: '', state: 'none', icon: 'message' });
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
      const [pair, fp, lines, isPaired, update, remembered, peer, version, st, notifs] = await Promise.all([
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
      ]);
      pairJSON = pair;
      fingerprint = fp;
      log = lines ?? [];
      paired = isPaired ?? false;
      notice = update ?? null;
      lastDevice = remembered;
      peerDevice = peer;
      appVersion = version || '0.2.0';
      settings = st;
      notifItems = notifs.Items;
      if (selectedId === 'notifications') {
        // Reading the pane clears the badge; the poll already shows the rows.
        if (notifs.Unseen > 0) void markNotificationsSeen();
        unseen = 0;
      } else {
        unseen = notifs.Unseen;
      }
      error = '';
      logInfo('poll tick', {
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
      logError('poll failed', msg);
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
    menu = { x, y, items, onAction };
  }

  function closeMenu(): void {
    menu = null;
  }

  function onContextMenu(e: MouseEvent): void {
    // Dev builds keep the native menu behind Option-right-click so the
    // web inspector stays reachable; production always shows app menus.
    if (import.meta.env.DEV && e.altKey) return;
    e.preventDefault();
    const target = e.target as HTMLElement | null;
    const copyEl = target?.closest?.('[data-copy]') as HTMLElement | null;
    const pairingEl = target?.closest?.('[data-menu="pairing"]') as HTMLElement | null;

    if (pairingEl) {
      const items: MenuItem[] = [];
      if (pair?.code) items.push({ id: 'code', label: 'Copy Code' });
      if (fingerprint) items.push({ id: 'fp', label: 'Copy Fingerprint' });
      if (!items.length) return;
      openMenu(e.clientX, e.clientY, items, (id) => {
        closeMenu();
        if (id === 'code') void copyText(pair?.code ?? '');
        else void copyText(fingerprint);
      });
      return;
    }
    if (copyEl?.dataset.copy) {
      const text = copyEl.dataset.copy;
      openMenu(e.clientX, e.clientY, [{ id: 'copy', label: 'Copy' }], () => {
        closeMenu();
        void copyText(text);
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
    void refresh();
    let offState: (() => void) | null = null;
    let offNotifs: (() => void) | null = null;
    let offSettings: (() => void) | null = null;
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
        if (data && typeof data === 'object' && 'NotificationsEnabled' in data) {
          settings = data;
        }
      });
    } catch {
      // Outside Wails (browser dev)
    }
    return () => {
      try { offState?.(); } catch { /* ignore */ }
      try { offNotifs?.(); } catch { /* ignore */ }
      try { offSettings?.(); } catch { /* ignore */ }
    };
  });
</script>

<main class="flex h-[100dvh] w-full flex-col overflow-hidden bg-transparent" oncontextmenu={onContextMenu}>
  <Toolbar />

  {#if error}
    <div role="alert" class="anim-row flex items-center gap-2 border-b border-separator bg-control px-4 py-2">
      <TriangleAlert size={13} strokeWidth={2} class="flex-none text-bad" aria-hidden="true" />
      <p class="min-w-0 flex-1 text-[12px] leading-snug text-label">{error}</p>
      <button
        type="button"
        onclick={() => (error = '')}
        class="flex-none rounded px-1.5 py-0.5 text-[11px] font-medium text-tertiary transition hover:bg-altrow hover:text-label focus-visible:outline-2 focus-visible:outline-focus"
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
    <nav aria-label="Devices" class="frost-side flex w-full flex-none flex-col overflow-y-auto md:w-[232px]">
      {#if paired || lastDevice}
        <div class="flex flex-col items-center px-4 pb-1 pt-4 text-center">
          <button
            type="button"
            onclick={() => select('phone')}
            aria-label="Phone details"
            class="flex h-16 w-16 items-center justify-center rounded-full bg-control ring-1 ring-separator transition hover:ring-focus focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus"
          >
            <AppIcon size={34} label="FuseItAll icon" />
          </button>
          <button
            type="button"
            onclick={() => select('phone')}
            class="mt-2 max-w-full truncate text-[13px] font-semibold text-label focus-visible:outline-2 focus-visible:outline-focus"
          >{displayName}</button>
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
                class="flex h-5 w-5 flex-none items-center justify-center rounded-full bg-bad/15 text-bad transition hover:bg-bad/25 focus-visible:outline-2 focus-visible:outline-focus active:translate-y-[1px]"
              ><X size={12} strokeWidth={2.5} aria-hidden="true" /></button>
            {/if}
          </div>
          <div class="mt-1.5 flex w-full items-center justify-center gap-1.5 text-[12px] tabular-nums text-secondary">
            <span class="truncate">{deviceModel}</span>
            {#if paired}<span class="h-1.5 w-1.5 flex-none rounded-full bg-accent" aria-label="Secured link" title="Secured link"></span>{/if}
          </div>
          {#if deviceFacts?.BatteryPct != null}
            <div class="mt-1.5 flex items-center justify-center gap-1.5">
              <span class="flex h-3.5 w-7 items-center rounded-[4px] border border-separator bg-control p-[1.5px]" aria-hidden="true">
                <span
                  class="block h-full rounded-[2px] {deviceFacts.BatteryPct > 20 ? 'bg-ok' : 'bg-warn'}"
                  style="width: {Math.max(4, Math.min(100, deviceFacts.BatteryPct))}%"
                ></span>
              </span>
              <span class="text-[11px] tabular-nums text-secondary">{deviceFacts.BatteryPct}%</span>
            </div>
          {/if}
        </div>
        <div class="mx-4 mt-3 border-t border-separator" aria-hidden="true"></div>
        <MediaSlot layout="controls" />
        {#if sources.length}
          <SourceList group="Navigation" items={sources} selectedId={selectedId} onSelect={select} />
        {/if}
      {:else}
        <div class="flex flex-col items-center px-4 pb-1 pt-8 text-center">
          <span class="flex h-16 w-16 items-center justify-center rounded-full bg-control ring-1 ring-separator" aria-hidden="true">
            <AppIcon size={34} label="FuseItAll icon" />
          </span>
          <p class="mt-3 text-[13px] font-semibold text-label">Link your first phone</p>
          <p class="mt-1 px-2 text-[11px] leading-relaxed text-tertiary">Scan the code to get started.</p>
        </div>
      {/if}

      <div class="flex-1"></div>

      {#if paired || lastDevice}
        <MediaSlot layout="player" />
      {/if}
      <div class="px-2 pb-2 pt-1">
        <div class="border-t border-separator pt-1">
          <button
            type="button"
            data-source-id="settings"
            onclick={() => select('settings')}
            aria-current={selectedId === 'settings'}
            class="flex w-full items-center gap-2.5 rounded-lg px-2.5 py-[7px] text-left transition focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-focus {selectedId === 'settings' ? 'bg-accent' : 'hover:bg-altrow'}"
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
      {#if (selectedId === 'files' || selectedId === 'photos') && (paired || lastDevice)}
        <div class="flex min-h-0 flex-1 flex-col overflow-hidden">
          <div class="min-h-0 flex-1 {selectedId === 'files' ? 'flex flex-col overflow-hidden' : 'hidden'}">
            <FileManager paired={paired} deviceLabel={deviceModel} active={selectedId === 'files'} peerKey={peerKey} />
          </div>
          <div class="min-h-0 flex-1 {selectedId === 'photos' ? 'flex flex-col overflow-hidden' : 'hidden'}">
            <PhotosViewer paired={paired} deviceLabel={deviceModel} active={selectedId === 'photos'} peerKey={peerKey} />
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
          {:else if selectedId === 'messages' && (paired || lastDevice)}
            <MessagesPane paired={paired} deviceLabel={deviceModel} />
          {:else if selectedId === 'contacts' && (paired || lastDevice)}
            <ContactsPane paired={paired} deviceLabel={deviceModel} />
          {:else if selectedId === 'mirror' && (paired || lastDevice)}
            <MirrorPane paired={paired} deviceLabel={deviceModel} />
      {:else if selectedId === 'settings'}
        <SettingsPane
          settings={settings}
          saving={settingsSaving}
          message={settingsMsg}
          updatedLabel={settingsUpdatedLabel}
          appVersion={appVersion}
          onNotifToggle={toggleNotif}
          onClipboardMode={setClipMode}
        />
      {:else if selectedId === 'phone' && (paired || lastDevice)}
        {#if paired}
          <DeviceHero
            title={displayName}
            subtitle="Connected over Wi-Fi"
            statusKind="ok"
            statusLabel="Online"
            rows={heroRows}
            note="Presence refreshes on its own."
          />
          <RenameCard
            displayName={displayName}
            advertisedName={deviceFacts?.DeviceName || ''}
            saving={renaming}
            message={renameResult}
            onSave={saveName}
          />
        {:else if lastDevice}
          <DeviceHero
            title={displayName}
            subtitle="Not in reach right now"
            statusKind="warn"
            statusLabel={`Seen ${seenLabel}`}
            rows={heroRows}
            note="It reconnects on its own once it is back on your Wi-Fi."
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
        {/if}
      {:else}
        <div data-menu="pairing">
          <PairCard
            qrSrc={qrSrc}
            code={pair?.code ?? ''}
            copied={copied}
            hostPort={hostPort}
            fingerprint={fingerprint}
            onCopyCode={copyCode}
          />
        </div>
      {/if}
        </div>
      {/if}
        {#snippet failed(_error, reset)}
          <div class="flex min-h-0 flex-1 flex-col gap-3 overflow-y-auto p-4">
            <div class="card mx-auto flex max-w-[420px] flex-col items-center px-6 py-10 text-center" role="alert">
              <p class="text-[13px] font-semibold text-label">This page hit a snag</p>
              <p class="mt-1 max-w-[36ch] text-[12px] leading-relaxed text-secondary">The phone sent an update this page could not read, so it stayed put. Nothing was lost.</p>
              <button
                type="button"
                onclick={() => reset()}
                class="mt-4 inline-flex h-7 items-center rounded-md bg-accent px-3 text-[13px] font-medium text-accent-text transition hover:brightness-95 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus active:translate-y-[1px]"
              >Try again</button>
            </div>
          </div>
        {/snippet}
      </svelte:boundary>
    </div>
  </div>

  {#if menu}
    <ContextMenu x={menu.x} y={menu.y} items={menu.items} onPick={menu.onAction} onClose={closeMenu} />
  {/if}

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
