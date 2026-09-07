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
  import { PlugZap, Wifi } from '@lucide/svelte';
  import { Service, clearNotifications, dismissNotification, forgetLastDevice, getAppVersion, getClipboard, getLastDevice, getNotifications, getPeerDevice, getSettings, markNotificationsSeen, pushClipboardCurrent, reconnectToLastDevice, setClipboardMode, setCustomName, setNotificationsEnabled } from './backend';
  import type { AppSettings, ClipNotice, LastDeviceNotice, NotifView } from './backend';
  import Toolbar from './components/Toolbar.svelte';
  import SourceList, { type SourceItem } from './components/SourceList.svelte';
  import StatusPill from './components/StatusPill.svelte';
  import DeviceHero from './components/DeviceHero.svelte';
  import PairCard from './components/PairCard.svelte';
  import QuickTiles, { type Tile } from './components/QuickTiles.svelte';
  import NotificationsPane from './components/NotificationsPane.svelte';
  import ClipboardPane from './components/ClipboardPane.svelte';
  import SettingsPane from './components/SettingsPane.svelte';
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

  const POLL_MS = 2000;

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
  let showForgetConfirm = $state(false);
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
  let clip = $state<ClipNotice | null>(null);
  let settingsSaving = $state(false);
  let settingsMsg = $state('');
  let clipPushing = $state(false);
  let clipMsg = $state('');
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

  let statusKind = $derived.by<'ok' | 'warn' | 'neutral'>(() => {
    if (paired) return 'ok';
    if (lastDevice) return 'warn';
    return 'neutral';
  });

  let statusLabel = $derived(paired ? 'Online' : lastDevice ? `Seen ${seenLabel}` : 'Not paired');

  // Top nav: only feature rows. Phone identity + Settings live pinned at bottom.
  let sources = $derived.by<SourceItem[]>(() => {
    const rows: SourceItem[] = [];
    if (paired || lastDevice) {
      rows.push({ id: 'notifications', label: 'Notifications', detail: unseen ? `${unseen} unread` : 'Mirrored', state: 'none', icon: 'bell', badge: unseen || undefined });
      rows.push({ id: 'clipboard', label: 'Clipboard', detail: clip?.Pending ? 'Pending' : 'Send', state: 'none', icon: 'clipboard' });
    }
    return rows;
  });

  // Footer items: phone + settings pinned to bottom separator.
  let phoneSource = $derived.by<SourceItem | null>(() => {
    if (!paired && !lastDevice) return null;
    return { id: 'phone', label: displayName, detail: paired ? 'Online' : `Seen ${seenLabel}`, state: paired ? 'ok' : 'warn', icon: 'phone' };
  });
  let settingsSource = $derived<SourceItem>({ id: 'settings', label: 'Settings', detail: '', state: 'none', icon: 'settings' });

  let title = $derived.by(() => {
    switch (selectedId) {
      case 'notifications': return 'Notifications';
      case 'clipboard': return 'Clipboard';
      case 'settings': return 'Settings';
      case 'phone': return paired || lastDevice ? displayName : 'Pair Phone';
      default: return 'Pair Phone';
    }
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

  let tiles = $derived.by<Tile[]>(() => {
    if (lastDevice && !paired) {
      return [
        {
          id: 'reconnect',
          label: 'Reconnect',
          sub: `Seen ${seenLabel}`,
          busyLabel: 'Reconnecting…',
          busy: reconnecting,
          disabled: reconnecting,
          hint: 'Dial the last phone again',
        },
      ];
    }
    return [];
  });

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
      const [pair, fp, lines, isPaired, update, remembered, peer, version, st, notifs, clipboard] = await Promise.all([
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
        getClipboard(),
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
      clip = clipboard;
      error = '';
      logInfo('poll tick', {
        paired,
        displayName,
        lastSeen: lastDevice?.LastSeenUnix ?? 0,
        unseen,
        clipPending: clip?.Pending ?? false,
        updateActive: notice?.Active ?? false,
        logTail: (lines ?? []).slice(-2),
      });
      if (notice?.Active) logInfo('update notice active', notice);
      // Cold open lands on the most relevant pane; later polls never
      // steal the selection once the user has chosen.
      if (!userSelected) {
        if (paired) selectedId = 'notifications';
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

  async function primaryAction(): Promise<void> {
    await copyCode();
  }

  function pickTile(id: Tile['id']): void {
    if (id === 'reconnect') void reconnect();
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

  function openForgetConfirm(_x: number, _y: number): void {
    closeMenu();
    showForgetConfirm = true;
  }

  function onContextMenu(e: MouseEvent): void {
    // Dev builds keep the native menu behind Option-right-click so the
    // web inspector stays reachable; production always shows app menus.
    if (import.meta.env.DEV && e.altKey) return;
    e.preventDefault();
    const target = e.target as HTMLElement | null;
    const copyEl = target?.closest?.('[data-copy]') as HTMLElement | null;
    const sourceEl = target?.closest?.('[data-source-id]') as HTMLElement | null;
    const pairingEl = target?.closest?.('[data-menu="pairing"]') as HTMLElement | null;

    if (sourceEl?.dataset.sourceId === 'phone' && lastDevice) {
      const items: MenuItem[] = [];
      if (!paired) items.push({ id: 'reconnect', label: 'Reconnect' });
      items.push({ id: 'forget', label: 'Forget Phone…', destructive: true });
      openMenu(e.clientX, e.clientY, items, (id) => {
        if (id === 'reconnect') {
          closeMenu();
          void reconnect();
        } else if (id === 'forget') {
          openForgetConfirm(e.clientX, e.clientY);
        } else {
          closeMenu();
        }
      });
      return;
    }
    // 3-dot button on phone row uses data-source-menu trigger
    const menuTrigger = target?.closest?.('[data-source-menu]') as HTMLElement | null;
    if (menuTrigger && lastDevice) {
      const items: MenuItem[] = [];
      if (!paired) items.push({ id: 'reconnect', label: 'Reconnect' });
      items.push({ id: 'forget', label: 'Forget Phone…', destructive: true });
      const rect = menuTrigger.getBoundingClientRect();
      openMenu(rect.left, rect.bottom + 6, items, (id) => {
        if (id === 'reconnect') {
          closeMenu();
          void reconnect();
        } else if (id === 'forget') {
          openForgetConfirm(rect.left, rect.bottom);
        } else {
          closeMenu();
        }
      });
      return;
    }
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

  async function pushClipCurrent(): Promise<void> {
    if (clipPushing) return;
    clipPushing = true;
    clipMsg = '';
    logInfo('pushClipboardCurrent attempt');
    try {
      clipMsg = await pushClipboardCurrent();
      logInfo('pushClipboardCurrent result', clipMsg);
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e);
      clipMsg = msg;
      logError('pushClipboardCurrent failed', msg);
    } finally {
      clipPushing = false;
      await refresh();
    }
  }

  onMount(() => {
    void refresh();
    const timer = setInterval(() => void refresh(), POLL_MS);
    return () => clearInterval(timer);
  });
</script>

<main class="flex min-h-[100dvh] w-full flex-col bg-transparent" oncontextmenu={onContextMenu}>
  <Toolbar
    title={title}
    primaryLabel={paired ? null : copied ? 'Copied' : 'Copy Code'}
    primaryBusyLabel="Copying…"
    primaryBusy={false}
    primaryDisabled={!pair?.code}
    primaryHint="Copy the pairing code"
    onPrimary={primaryAction}
  />

  {#if error}
    <p role="alert" class="bg-control px-4 py-2 text-[12px] text-bad">{error}</p>
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
    <nav aria-label="Devices" class="frost-side flex w-full flex-none flex-col md:w-[240px]">
      <div class="flex flex-col items-center px-3 pb-1 pt-4 text-center">
        <span class="icon-well" aria-hidden="true">
          <PlugZap size={24} strokeWidth={2} />
        </span>
        <p class="mt-2 text-[13px] font-semibold text-label">FuseItAll</p>
        <div class="mt-2 flex items-center gap-1.5">
          <StatusPill kind={statusKind} label={statusLabel} />
          {#if paired || lastDevice}
            <span class="pill pill-neutral">
              <Wifi size={11} strokeWidth={2.5} aria-hidden="true" />
              <span>Wi-Fi</span>
            </span>
          {/if}
        </div>
      </div>

      <QuickTiles tiles={tiles} onPick={pickTile} />

      {#if sources.length}
        <SourceList group="Navigation" items={sources} selectedId={selectedId} onSelect={select} />
      {:else if !paired && !lastDevice}
        <p class="px-4 py-2 text-center text-[11px] text-tertiary">Scan the code to link your first phone.</p>
      {/if}

      <div class="flex-1"></div>

      <!-- Pinned footer: phone identity + settings -->
      <div class="">
        {#if phoneSource}
          <div class="px-2 pt-2">
            <div
              role="button"
              tabindex="0"
              data-source-id="phone"
              onclick={() => select('phone')}
              onkeydown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); select('phone'); } }}
              aria-current={selectedId === 'phone'}
              class="flex w-full items-center gap-2.5 rounded-lg px-2.5 py-2 text-left transition focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-focus cursor-pointer {selectedId === 'phone' ? 'bg-accent' : 'hover:bg-altrow'}"
            >
              <span class="flex h-7 w-7 flex-none items-center justify-center rounded-full {paired ? 'bg-ok/15 text-ok' : 'bg-warn/15 text-warn'}" aria-hidden="true">
                <span class="h-2 w-2 rounded-full {paired ? 'bg-ok' : 'bg-warn'}"></span>
              </span>
              <button
                type="button"
                onclick={() => select('phone')}
                class="min-w-0 flex-1 text-left"
              >
                <span class="block truncate text-[13px] font-medium {selectedId === 'phone' ? 'text-accent-text' : 'text-label'}">{displayName}</span>
                <span class="block truncate text-[11px] {selectedId === 'phone' ? 'text-accent-text opacity-80' : 'text-secondary'}">
                  {#if deviceFacts?.Model}{deviceFacts.Model} · {/if}{paired ? 'Online' : `Seen ${seenLabel}`}
                </span>
                {#if batteryText(deviceFacts)}
                  <span class="block truncate text-[11px] {selectedId === 'phone' ? 'text-accent-text opacity-60' : 'text-tertiary'}">Battery {batteryText(deviceFacts)}</span>
                {/if}
              </button>
              <button
                type="button"
                data-source-menu="phone"
                onclick={(e) => { e.stopPropagation(); onContextMenu(e as unknown as MouseEvent); }}
                aria-label="Phone options"
                class="flex h-7 w-7 flex-none items-center justify-center rounded-md text-tertiary hover:bg-altrow hover:text-label"
              >⋯</button>
            </div>
          </div>
        {/if}
        <div class="px-2 py-2">
          <button
            type="button"
            data-source-id="settings"
            onclick={() => select('settings')}
            aria-current={selectedId === 'settings'}
            class="flex w-full items-center gap-2.5 rounded-lg px-2.5 py-2 text-left transition focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-focus {selectedId === 'settings' ? 'bg-accent' : 'hover:bg-altrow'}"
          >
            <span class="flex h-7 w-7 flex-none items-center justify-center rounded-lg {selectedId === 'settings' ? 'bg-accent-text/15' : 'bg-altrow'}">
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" class={selectedId === 'settings' ? 'text-accent-text' : 'text-accent'} aria-hidden="true"><path d="M12 15a3 3 0 1 0 0-6 3 3 0 0 0 0 6Z"/><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09a1.65 1.65 0 0 0-1-1.51 1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06A1.65 1.65 0 0 0 9 15a1.65 1.65 0 0 0-1-1.51V13a1.65 1.65 0 0 0 1-1.51A1.65 1.65 0 0 0 7.18 9.67l-.06-.06a2 2 0 0 1 2.83-2.83l.06.06A1.65 1.65 0 0 0 11.82 7.17a1.65 1.65 0 0 0 1-1.51V5a2 2 0 0 1 4 0v.67a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06A1.65 1.65 0 0 0 19.4 11a1.65 1.65 0 0 0 1 1.51V13a1.65 1.65 0 0 0-1 1Z"/></svg>
            </span>
            <span class="min-w-0 flex-1">
              <span class="block text-[13px] font-medium {selectedId === 'settings' ? 'text-accent-text' : 'text-label'}">Settings</span>
            </span>
          </button>
        </div>
        <p class="px-4 pb-3 pt-1 text-center text-[11px] text-tertiary">FuseItAll v{appVersion}</p>
      </div>
    </nav>

    <div class="flex min-h-0 min-w-[220px] flex-1 flex-col gap-3 overflow-y-auto bg-window p-4">
      {#if selectedId === 'notifications' && (paired || lastDevice)}
        <NotificationsPane
          items={notifItems}
          paired={paired}
          clearing={clearingNotifs}
          onDismiss={dismissNotif}
          onClear={clearNotifs}
        />
      {:else if selectedId === 'clipboard' && (paired || lastDevice)}
        <ClipboardPane
          clip={clip}
          pushing={clipPushing}
          message={clipMsg}
          onPushCurrent={pushClipCurrent}
        />
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
  </div>

  {#if menu}
    <ContextMenu x={menu.x} y={menu.y} items={menu.items} onPick={menu.onAction} onClose={closeMenu} />
  {/if}

  <ConfirmDialog
    open={showForgetConfirm}
    title="Forget this phone?"
    body="This removes the pairing and the Mac generates a new QR. You will need to scan again to re-pair."
    confirmLabel={forgetting ? 'Forgetting…' : 'Forget'}
    destructive={true}
    busy={forgetting}
    onConfirm={() => { showForgetConfirm = false; void forget(); }}
    onCancel={() => (showForgetConfirm = false)}
  />
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
