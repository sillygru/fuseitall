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
  import { Service, forgetLastDevice, getAppVersion, getLastDevice, getPeerDevice, reconnectToLastDevice, setCustomName } from './backend';
  import type { LastDeviceNotice } from './backend';
  import { humanizeLog } from './activity';
  import Toolbar from './components/Toolbar.svelte';
  import SourceList, { type SourceItem } from './components/SourceList.svelte';
  import StatusPill from './components/StatusPill.svelte';
  import DeviceHero from './components/DeviceHero.svelte';
  import PairCard from './components/PairCard.svelte';
  import QuickTiles, { type Tile } from './components/QuickTiles.svelte';
  import NoticeRow from './components/NoticeRow.svelte';
  import RememberedGroup from './components/RememberedGroup.svelte';
  import RenameCard from './components/RenameCard.svelte';
  import ActivityList from './components/ActivityList.svelte';
  import ContextMenu, { type MenuItem } from './components/ContextMenu.svelte';

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
  let appVersion = $state('0.1.0');

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

  // Latest update notice from the typed binding. Self = this Mac is outdated;
  // otherwise the peer must update. The message is the canonical core text.
  let updateNotice = $derived(notice && notice.Active ? notice : null);

  let activity = $derived(humanizeLog(log));

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

  // The sidebar lists the phone only once it exists (paired or remembered).
  // Pairing lives in the main hero; This Mac is the identity header, never
  // a navigation row. Details stay friendly: Online / Seen Xm ago.
  let sources = $derived.by<SourceItem[]>(() => {
    if (paired) return [{ id: 'phone', label: displayName, detail: 'Online', state: 'ok' }];
    if (lastDevice) return [{ id: 'phone', label: displayName, detail: `Seen ${seenLabel}`, state: 'warn' }];
    return [];
  });

  let title = $derived(selectedId === 'phone' && (paired || lastDevice) ? displayName : 'Pair Phone');

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
      const [pair, fp, lines, isPaired, update, remembered, peer, version] = await Promise.all([
        Service.GetPairJSON(),
        Service.GetFingerprint(),
        Service.GetLog(),
        Service.IsPaired(),
        Service.GetUpdateNotice(),
        getLastDevice(),
        getPeerDevice(),
        getAppVersion(),
      ]);
      pairJSON = pair;
      fingerprint = fp;
      log = lines ?? [];
      paired = isPaired ?? false;
      notice = update ?? null;
      lastDevice = remembered;
      peerDevice = peer;
      appVersion = version || '0.1.0';
      error = '';
      // Cold open lands on the most relevant pane; later polls never
      // steal the selection once the user has chosen.
      if (!userSelected) {
        selectedId = paired || remembered ? 'phone' : 'pair';
      }
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    }
  }

  async function copyText(text: string): Promise<void> {
    if (!text) return;
    try {
      await navigator.clipboard.writeText(text);
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    }
  }

  async function copyCode(): Promise<void> {
    const code = pair?.code ?? '';
    if (!code) return;
    try {
      await navigator.clipboard.writeText(code);
      copied = true;
      setTimeout(() => (copied = false), 2000);
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
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
    try {
      renameResult = await setCustomName(name);
    } catch (e) {
      renameResult = e instanceof Error ? e.message : String(e);
    } finally {
      renaming = false;
      await refresh();
    }
  }

  async function reconnect(): Promise<void> {
    if (reconnecting) return;
    reconnecting = true;
    reconnectResult = '';
    try {
      reconnectResult = await reconnectToLastDevice();
    } catch (e) {
      reconnectResult = e instanceof Error ? e.message : String(e);
    } finally {
      reconnecting = false;
      await refresh();
    }
  }

  async function forget(): Promise<void> {
    if (forgetting) return;
    forgetting = true;
    forgetResult = '';
    try {
      forgetResult = await forgetLastDevice();
      selectedId = 'pair';
      userSelected = false;
    } catch (e) {
      forgetResult = e instanceof Error ? e.message : String(e);
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

  function openForgetConfirm(x: number, y: number): void {
    openMenu(
      x,
      y,
      [
        { id: 'confirm', label: 'Forget This Phone', destructive: true },
        { id: 'cancel', label: 'Cancel' },
      ],
      (id) => {
        closeMenu();
        if (id === 'confirm') void forget();
      },
    );
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
    <p role="alert" class="border-b border-separator bg-control px-4 py-2 text-[12px] text-bad">{error}</p>
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
    <nav aria-label="Devices" class="frost-side w-full flex-none border-b border-separator md:flex md:w-[240px] md:flex-col md:border-b-0 md:border-r">
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
        {#if deviceFacts && (deviceFacts.Model || deviceFacts.BatteryPct != null)}
          <div class="mt-1.5 flex flex-col items-center gap-0.5" aria-live="polite">
            {#if deviceFacts.Model}
              <p class="max-w-full truncate text-[11px] text-secondary">{deviceFacts.Model}</p>
            {/if}
            {#if batteryText(deviceFacts)}
              <p class="text-[11px] {paired ? 'text-secondary' : 'text-tertiary'}">Battery {batteryText(deviceFacts)}</p>
            {/if}
          </div>
        {/if}
      </div>

      <QuickTiles tiles={tiles} onPick={pickTile} />

      {#if sources.length}
        <SourceList group="Devices" items={sources} selectedId={selectedId} onSelect={select} />
      {:else}
        <p class="px-4 py-2 text-center text-[11px] text-tertiary">Scan the code to link your first phone.</p>
      {/if}
      <p class="mt-auto px-4 pb-3 pt-2 text-center text-[11px] text-tertiary">FuseItAll v{appVersion}</p>
    </nav>

    <div class="flex min-h-0 min-w-[220px] flex-1 flex-col gap-3 overflow-y-auto bg-window p-4">
      {#if selectedId === 'phone' && (paired || lastDevice)}
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
      <ActivityList items={activity} emptyHint="Nothing here yet. Pair a phone to get started." />
    </div>
  </div>

  {#if menu}
    <ContextMenu x={menu.x} y={menu.y} items={menu.items} onPick={menu.onAction} onClose={closeMenu} />
  {/if}
</main>
