<!--
  Copyright (C) 2026 FuseItAll contributors.

  This program is free software: you can redistribute it and/or modify
  it under the terms of the GNU Affero General Public License as published
  by the Free Software Foundation, version 3 of the License. See LICENSE
  for details.

  Settings pane: notification switch + per-app filter, clipboard direction,
  playback direction + Mac output (HIG Windows/Color/Typography/Buttons).
  Reading this as: Settings pane for notifications + per-app filter + clipboard + playback, following HIG 4/5/8/11, no deviations.
-->
<script lang="ts">
  import { Sliders, Bell, Clipboard, Music, RefreshCw, Folder, ShieldAlert, Check } from '@lucide/svelte';
  import type { AppSettings, KnownNotifApp } from '../backend';

  interface Props {
    settings: AppSettings;
    knownApps: KnownNotifApp[];
    appsSource: 'phone' | 'mirror';
    appsLoading: boolean;
    appsError: string;
    saving: boolean;
    message: string;
    updatedLabel: string;
    appVersion: string;
    defaultUploadDir: string;
    onNotifToggle: (enabled: boolean) => void;
    onNotifMode: (mode: string) => void;
    onAppMuted: (pkg: string, muted: boolean) => void;
    onAppAllowed: (pkg: string, allowed: boolean) => void;
    onClipboardMode: (mode: string) => void;
    onClipboardAllowSensitive: (allow: boolean) => void;
    onPlaybackMode: (mode: string) => void;
    onPlaybackOutput: (output: string) => void;
    onUploadDefault: (dir: string) => void;
    onAppsRefresh: () => void;
  }

  let {
    settings,
    knownApps,
    appsSource,
    appsLoading,
    appsError,
    saving,
    message,
    updatedLabel,
    appVersion,
    defaultUploadDir,
    onNotifToggle,
    onNotifMode,
    onAppMuted,
    onAppAllowed,
    onClipboardMode,
    onClipboardAllowSensitive,
    onPlaybackMode,
    onPlaybackOutput,
    onUploadDefault,
    onAppsRefresh,
  }: Props = $props();

  type SettingsCategory = 'general' | 'notifications' | 'clipboard' | 'media';
  let currentCategory = $state<SettingsCategory>('general');

  const clipboardModes = [
    { v: 'both', label: 'Both directions', desc: 'Phone ↔ Mac auto-sync', hint: 'Default' },
    { v: 'android_to_mac', label: 'Phone → Mac only', desc: 'Only phone copies sync to Mac' },
    { v: 'mac_to_android', label: 'Mac → Phone only', desc: 'Only Mac copies sync to phone' },
    { v: 'disabled', label: 'Off', desc: 'Manual Send still works' },
  ] as const;

  const selectedMode = $derived(clipboardModes.find((m) => m.v === settings.ClipboardMode) ?? clipboardModes[0]);
  const onlyAllowed = $derived(settings.NotifMode === 'only_allowed');

  const playbackModes = [
    { v: 'both', label: 'Both ways', desc: 'Show it here and control from Mac', hint: 'Default' },
    { v: 'android_to_mac', label: 'Phone to Mac only', desc: 'Show only, no control from Mac' },
    { v: 'mac_to_android', label: 'Mac to Phone only', desc: 'Control only, no display here' },
    { v: 'disabled', label: 'Off', desc: 'No display and no control' },
  ] as const;

  const selectedPlayback = $derived(playbackModes.find((m) => m.v === settings.PlaybackMode) ?? playbackModes[0]);
  const systemOut = $derived(settings.PlaybackOutput === 'system');

  let appQuery = $state('');
  const visibleApps = $derived.by<KnownNotifApp[]>(() => {
    const q = appQuery.trim().toLowerCase();
    const list = q
      ? knownApps.filter((a) => a.App.toLowerCase().includes(q) || a.PackageName.toLowerCase().includes(q))
      : knownApps;
    return list.slice(0, 100);
  });

  const mutedCount = $derived(settings.MutedPackages.length);
  const allowedCount = $derived(settings.AllowedPackages.length);

  function onTabKey(e: KeyboardEvent) {
    const cats: SettingsCategory[] = ['general', 'notifications', 'clipboard', 'media'];
    const idx = cats.indexOf(currentCategory);
    if (e.key === 'ArrowRight' && idx < cats.length - 1) {
      e.preventDefault();
      currentCategory = cats[idx + 1];
    } else if (e.key === 'ArrowLeft' && idx > 0) {
      e.preventDefault();
      currentCategory = cats[idx - 1];
    } else if (e.key === '1') {
      e.preventDefault();
      currentCategory = 'general';
    } else if (e.key === '2') {
      e.preventDefault();
      currentCategory = 'notifications';
    } else if (e.key === '3') {
      e.preventDefault();
      currentCategory = 'clipboard';
    } else if (e.key === '4') {
      e.preventDefault();
      currentCategory = 'media';
    }
  }
</script>

<section aria-label="Settings" class="section flex h-full min-h-0 flex-1 flex-col px-2 py-2">
  <!-- Header with save/sync status -->
  <div class="flex items-center justify-between pb-3 px-1">
    <div>
      <h2 class="text-[17px] font-semibold tracking-tight text-label">Settings</h2>
      <p class="mt-0.5 text-[12px] text-secondary">Last change {updatedLabel} · Synced between devices</p>
    </div>
    {#if saving}
      <span class="inline-flex items-center gap-1.5 rounded-full bg-altrow px-2.5 py-1 text-[11px] font-medium text-secondary" role="status">
        <span class="h-2.5 w-2.5 animate-spin rounded-full border-2 border-accent border-t-transparent" aria-hidden="true"></span>
        <span>Saving…</span>
      </span>
    {/if}
  </div>

  <!-- Category segmented tab bar -->
  <nav
    role="tablist"
    aria-label="Settings categories"
    onkeydown={onTabKey}
    tabindex="0"
    class="flex flex-none items-center gap-1 rounded-xl border border-separator/60 bg-control p-1 shadow-sm focus:outline-none focus:ring-1 focus:ring-focus"
  >
    <button
      type="button"
      role="tab"
      aria-selected={currentCategory === 'general'}
      onclick={() => (currentCategory = 'general')}
      class="flex flex-1 items-center justify-center gap-2 rounded-lg py-1.5 px-3 text-[12px] font-medium transition active:scale-[0.98] {currentCategory === 'general'
        ? 'bg-window text-label shadow-sm font-semibold'
        : 'text-secondary hover:text-label hover:bg-window/50'}"
    >
      <Sliders size={14} class={currentCategory === 'general' ? 'text-accent' : 'text-secondary'} aria-hidden="true" />
      <span>General</span>
    </button>
    <button
      type="button"
      role="tab"
      aria-selected={currentCategory === 'notifications'}
      onclick={() => (currentCategory = 'notifications')}
      class="flex flex-1 items-center justify-center gap-2 rounded-lg py-1.5 px-3 text-[12px] font-medium transition active:scale-[0.98] {currentCategory === 'notifications'
        ? 'bg-window text-label shadow-sm font-semibold'
        : 'text-secondary hover:text-label hover:bg-window/50'}"
    >
      <Bell size={14} class={currentCategory === 'notifications' ? 'text-accent' : 'text-secondary'} aria-hidden="true" />
      <span>Notifications</span>
      {#if !settings.NotificationsEnabled}
        <span class="h-1.5 w-1.5 rounded-full bg-separator" title="Muted"></span>
      {/if}
    </button>
    <button
      type="button"
      role="tab"
      aria-selected={currentCategory === 'clipboard'}
      onclick={() => (currentCategory = 'clipboard')}
      class="flex flex-1 items-center justify-center gap-2 rounded-lg py-1.5 px-3 text-[12px] font-medium transition active:scale-[0.98] {currentCategory === 'clipboard'
        ? 'bg-window text-label shadow-sm font-semibold'
        : 'text-secondary hover:text-label hover:bg-window/50'}"
    >
      <Clipboard size={14} class={currentCategory === 'clipboard' ? 'text-accent' : 'text-secondary'} aria-hidden="true" />
      <span>Clipboard</span>
    </button>
    <button
      type="button"
      role="tab"
      aria-selected={currentCategory === 'media'}
      onclick={() => (currentCategory = 'media')}
      class="flex flex-1 items-center justify-center gap-2 rounded-lg py-1.5 px-3 text-[12px] font-medium transition active:scale-[0.98] {currentCategory === 'media'
        ? 'bg-window text-label shadow-sm font-semibold'
        : 'text-secondary hover:text-label hover:bg-window/50'}"
    >
      <Music size={14} class={currentCategory === 'media' ? 'text-accent' : 'text-secondary'} aria-hidden="true" />
      <span>Media</span>
    </button>
  </nav>

  <!-- Category Tab Panels -->
  <div class="mt-4 flex-1 overflow-y-auto px-1">
    <!-- GENERAL TAB -->
    {#if currentCategory === 'general'}
      <div class="flex flex-col gap-4">
        <!-- Default Upload Destination -->
        <div class="rounded-xl border border-separator/60 bg-control/40 p-4">
          <div class="flex items-start gap-3">
            <div class="flex h-8 w-8 flex-none items-center justify-center rounded-lg bg-accent/15 text-accent">
              <Folder size={16} aria-hidden="true" />
            </div>
            <div class="min-w-0 flex-1">
              <h3 class="text-[13px] font-semibold text-label">Default Upload Folder</h3>
              <p class="mt-0.5 text-[12px] text-secondary leading-relaxed">
                Default phone folder when Finder files are dropped onto the phone home area. Mac only, never synced to the phone.
              </p>
              <div class="mt-3 flex items-center gap-2">
                <input
                  type="text"
                  value={defaultUploadDir}
                  placeholder="Download"
                  aria-label="Default phone upload folder"
                  disabled={saving}
                  onchange={(e) => onUploadDefault((e.currentTarget as HTMLInputElement).value.trim())}
                  class="h-7 w-full min-w-0 flex-1 rounded-md border border-separator bg-window px-2.5 text-[12px] placeholder:text-tertiary focus:outline-none focus:ring-2 focus:ring-focus disabled:opacity-50"
                />
                {#if defaultUploadDir}
                  <button
                    type="button"
                    onclick={() => onUploadDefault('')}
                    disabled={saving}
                    class="inline-flex h-7 shrink-0 items-center rounded-md border border-separator bg-window px-3 text-[12px] text-label transition hover:bg-altrow focus-visible:outline-2 focus-visible:outline-focus disabled:opacity-50"
                  >Clear</button>
                {:else}
                  <button
                    type="button"
                    onclick={() => onUploadDefault('Download')}
                    disabled={saving}
                    class="inline-flex h-7 shrink-0 items-center rounded-md bg-accent px-3 text-[12px] font-medium text-accent-text transition hover:brightness-95 focus-visible:outline-2 focus-visible:outline-focus active:translate-y-[1px] disabled:opacity-50"
                  >Use Download</button>
                {/if}
              </div>
            </div>
          </div>
        </div>

        <!-- System & Sync Overview -->
        <div class="rounded-xl border border-separator/60 bg-control/40 p-4">
          <h3 class="text-[13px] font-semibold text-label">Sync Overview</h3>
          <p class="mt-1 text-[12px] leading-relaxed text-secondary">
            Settings are live-synchronized with the paired phone over the encrypted transport link. Changes take effect immediately without requiring an app restart.
          </p>
          <div class="mt-3 grid grid-cols-2 gap-2 text-[12px]">
            <div class="rounded-lg bg-window/70 p-2.5 border border-separator/40">
              <span class="text-tertiary block text-[11px]">Last Synchronized</span>
              <span class="font-medium text-label">{updatedLabel}</span>
            </div>
            <div class="rounded-lg bg-window/70 p-2.5 border border-separator/40">
              <span class="text-tertiary block text-[11px]">Client Version</span>
              <span class="font-medium text-label">FuseItAll v{appVersion}</span>
            </div>
          </div>
        </div>
      </div>

    <!-- NOTIFICATIONS TAB -->
    {:else if currentCategory === 'notifications'}
      <div class="flex flex-col gap-4">
        <!-- Master Switch -->
        <div class="rounded-xl border border-separator/60 bg-control/40 p-4">
          <div class="flex items-center justify-between gap-3">
            <div class="min-w-0 flex-1">
              <h3 class="text-[13px] font-semibold text-label">Phone Notifications</h3>
              <p class="mt-0.5 text-[12px] text-secondary">Mirror incoming phone notifications on this Mac</p>
            </div>
            <button
              type="button"
              role="switch"
              aria-checked={settings.NotificationsEnabled}
              aria-label="Phone notifications"
              disabled={saving}
              onclick={() => onNotifToggle(!settings.NotificationsEnabled)}
              class="flex h-[22px] w-[40px] flex-none items-center rounded-full px-0.5 transition focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus disabled:opacity-50 {settings.NotificationsEnabled
                ? 'justify-end bg-accent'
                : 'justify-start bg-separator'}"
            >
              <span class="h-[18px] w-[18px] rounded-full bg-control shadow-sm transition" aria-hidden="true"></span>
            </button>
          </div>
        </div>

        <!-- App Filter Mode -->
        <div class="rounded-xl border border-separator/60 bg-control/40 p-4">
          <div class="flex items-baseline justify-between gap-2">
            <h3 class="text-[13px] font-semibold text-label">Filtering Mode</h3>
            <span class="text-[11px] text-tertiary">{onlyAllowed ? `Allowed ${allowedCount}` : mutedCount ? `Muted ${mutedCount}` : 'All apps'}</span>
          </div>
          <p class="mt-0.5 text-[12px] text-secondary">Choose how notifications from apps are filtered.</p>

          <fieldset class="mt-3 flex flex-col gap-1.5" disabled={saving || !settings.NotificationsEnabled} aria-label="Notification app filter mode">
            <button
              type="button"
              role="radio"
              aria-checked={!onlyAllowed}
              disabled={saving || !settings.NotificationsEnabled}
              onclick={() => { if (onlyAllowed) onNotifMode('all_except_muted'); }}
              class="flex w-full items-center gap-3 rounded-lg border p-2.5 text-left transition focus-visible:outline-2 focus-visible:outline-focus disabled:opacity-50 {!onlyAllowed ? 'border-accent bg-accent/10 text-accent' : 'border-separator/50 bg-window text-label hover:bg-altrow'}"
            >
              <span class="flex h-4 w-4 flex-none items-center justify-center rounded-full border {!onlyAllowed ? 'border-accent bg-accent' : 'border-separator bg-window'}" aria-hidden="true">
                {#if !onlyAllowed}<span class="h-1.5 w-1.5 rounded-full bg-white"></span>{/if}
              </span>
              <div class="min-w-0 flex-1">
                <span class="block text-[12px] font-medium leading-none text-label">All except muted</span>
                <span class="mt-1 block text-[11px] leading-tight text-secondary">Mirror everything except apps you explicitly mute</span>
              </div>
              {#if !onlyAllowed}<Check size={14} class="text-accent" />{/if}
            </button>

            <button
              type="button"
              role="radio"
              aria-checked={onlyAllowed}
              disabled={saving || !settings.NotificationsEnabled}
              onclick={() => { if (!onlyAllowed) onNotifMode('only_allowed'); }}
              class="flex w-full items-center gap-3 rounded-lg border p-2.5 text-left transition focus-visible:outline-2 focus-visible:outline-focus disabled:opacity-50 {onlyAllowed ? 'border-accent bg-accent/10 text-accent' : 'border-separator/50 bg-window text-label hover:bg-altrow'}"
            >
              <span class="flex h-4 w-4 flex-none items-center justify-center rounded-full border {onlyAllowed ? 'border-accent bg-accent' : 'border-separator bg-window'}" aria-hidden="true">
                {#if onlyAllowed}<span class="h-1.5 w-1.5 rounded-full bg-white"></span>{/if}
              </span>
              <div class="min-w-0 flex-1">
                <span class="block text-[12px] font-medium leading-none text-label">Only allowed</span>
                <span class="mt-1 block text-[11px] leading-tight text-secondary">Mirror only apps you have explicitly allowed</span>
              </div>
              {#if onlyAllowed}<Check size={14} class="text-accent" />{/if}
            </button>
          </fieldset>
        </div>

        <!-- Per-App List -->
        {#if settings.NotificationsEnabled}
          <div class="rounded-xl border border-separator/60 bg-control/40 p-4">
            <div class="flex items-center justify-between gap-2">
              <h3 class="text-[13px] font-semibold text-label">Phone Apps</h3>
              <div class="flex items-center gap-2">
                <button
                  type="button"
                  onclick={onAppsRefresh}
                  disabled={saving || appsLoading}
                  class="inline-flex h-6 items-center gap-1.5 rounded-md border border-separator bg-window px-2 text-[11px] font-medium text-label transition hover:bg-altrow focus-visible:outline-2 focus-visible:outline-focus disabled:opacity-50"
                >
                  <RefreshCw size={11} class={appsLoading ? 'animate-spin' : ''} aria-hidden="true" />
                  <span>{appsLoading ? 'Refreshing…' : 'Refresh'}</span>
                </button>
                <span class="text-[11px] text-tertiary">
                  {#if appsSource === 'phone'}{knownApps.length} apps{:else}Mirrored only{/if}
                </span>
              </div>
            </div>

            {#if appsError}
              <p class="mt-2 text-[11px] text-secondary" role="note">{appsError}</p>
            {/if}

            <div class="mt-3">
              <input
                type="search"
                placeholder="Search phone apps…"
                aria-label="Search phone apps"
                bind:value={appQuery}
                disabled={saving}
                class="h-7 w-full rounded-md border border-separator bg-window px-2.5 text-[12px] text-label placeholder:text-tertiary focus:outline-none focus:ring-2 focus:ring-focus disabled:opacity-50"
              />
            </div>

            {#if visibleApps.length}
              <ul class="mt-3 max-h-[260px] overflow-y-auto divide-y divide-separator/40 rounded-lg border border-separator/40 bg-window" aria-label="Phone apps">
                {#each visibleApps as a (a.PackageName)}
                  {@const on = onlyAllowed ? a.Allowed : !a.Muted}
                  <li class="flex items-center gap-3 px-3 py-2">
                    <div class="flex h-7 w-7 flex-none items-center justify-center overflow-hidden rounded-md bg-altrow">
                      {#if a.IconB64}
                        <img src={"data:image/png;base64," + a.IconB64} alt="" class="h-7 w-7 object-cover" loading="lazy" />
                      {:else}
                        <span class="text-[10px] font-semibold text-secondary">{a.App.slice(0, 2).toUpperCase()}</span>
                      {/if}
                    </div>
                    <div class="min-w-0 flex-1">
                      <p class="truncate text-[12px] font-medium leading-none text-label">{a.App}</p>
                      <p class="mt-0.5 truncate font-mono text-[10px] leading-tight text-tertiary">{a.PackageName}{#if a.Count} · {a.Count}{/if}</p>
                    </div>
                    <button
                      type="button"
                      role="switch"
                      aria-checked={on}
                      aria-label={onlyAllowed ? `Allow ${a.App}` : `Mirror ${a.App}`}
                      disabled={saving}
                      onclick={() => { if (onlyAllowed) onAppAllowed(a.PackageName, !a.Allowed); else onAppMuted(a.PackageName, !a.Muted); }}
                      class="flex h-[22px] w-[40px] flex-none items-center rounded-full px-0.5 transition focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus disabled:opacity-50 {on ? 'justify-end bg-accent' : 'justify-start bg-separator'}"
                    >
                      <span class="h-[18px] w-[18px] rounded-full bg-control shadow-sm transition" aria-hidden="true"></span>
                    </button>
                  </li>
                {/each}
              </ul>
            {:else}
              <p class="mt-3 text-center py-4 text-[12px] text-secondary">{knownApps.length ? 'No apps match this search.' : 'Tap Refresh to load the phone app list.'}</p>
            {/if}
          </div>
        {/if}
      </div>

    <!-- CLIPBOARD TAB -->
    {:else if currentCategory === 'clipboard'}
      <div class="flex flex-col gap-4">
        <!-- Direction Radio Cards -->
        <div class="rounded-xl border border-separator/60 bg-control/40 p-4">
          <div class="flex items-baseline justify-between gap-2">
            <h3 class="text-[13px] font-semibold text-label">Sync Direction</h3>
            <span class="text-[11px] text-tertiary">{selectedMode.label}</span>
          </div>
          <p class="mt-0.5 text-[12px] text-secondary">Automatic clipboard synchronization mode between Phone and Mac.</p>

          <fieldset class="mt-3 flex flex-col gap-1.5" disabled={saving} aria-label="Clipboard auto sync direction">
            {#each clipboardModes as m (m.v)}
              {@const checked = settings.ClipboardMode === m.v}
              <button
                type="button"
                role="radio"
                aria-checked={checked}
                disabled={saving}
                onclick={() => { if (!checked) onClipboardMode(m.v); }}
                class="flex w-full items-center gap-3 rounded-lg border p-2.5 text-left transition focus-visible:outline-2 focus-visible:outline-focus disabled:opacity-50 {checked
                  ? 'border-accent bg-accent/10 text-accent'
                  : 'border-separator/50 bg-window text-label hover:bg-altrow'}"
              >
                <span class="flex h-4 w-4 flex-none items-center justify-center rounded-full border {checked ? 'border-accent bg-accent' : 'border-separator bg-window'}" aria-hidden="true">
                  {#if checked}<span class="h-1.5 w-1.5 rounded-full bg-white"></span>{/if}
                </span>
                <div class="min-w-0 flex-1">
                  <div class="flex items-center gap-2">
                    <span class="text-[12px] font-medium leading-none text-label">{m.label}</span>
                    {#if m.v === 'both' && !checked}
                      <span class="rounded bg-altrow px-1 py-0.5 text-[10px] text-tertiary">Default</span>
                    {/if}
                  </div>
                  <span class="mt-1 block text-[11px] leading-tight text-secondary">{m.desc}</span>
                </div>
                {#if checked}<Check size={14} class="text-accent" />{/if}
              </button>
            {/each}
          </fieldset>
        </div>

        <!-- Sensitive Data Switch -->
        <div class="rounded-xl border border-separator/60 bg-control/40 p-4">
          <div class="flex items-center justify-between gap-3">
            <div class="min-w-0 flex-1">
              <h3 class="text-[13px] font-semibold text-label">Auto-sync passwords and codes</h3>
              <p class="mt-0.5 text-[12px] text-secondary">
                When turned off, sensitive clips (passwords, 2FA tokens) are skipped from automatic background syncing. Manual Send in the app always works.
              </p>
            </div>
            <button
              type="button"
              role="switch"
              aria-checked={settings.ClipboardAllowSensitive}
              aria-label="Auto-sync passwords and codes"
              disabled={saving}
              onclick={() => onClipboardAllowSensitive(!settings.ClipboardAllowSensitive)}
              class="flex h-[22px] w-[40px] flex-none items-center rounded-full px-0.5 transition focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus disabled:opacity-50 {settings.ClipboardAllowSensitive
                ? 'justify-end bg-accent'
                : 'justify-start bg-separator'}"
            >
              <span class="h-[18px] w-[18px] rounded-full bg-control shadow-sm transition" aria-hidden="true"></span>
            </button>
          </div>
        </div>

        <!-- Explanatory note -->
        <div class="flex items-start gap-2.5 rounded-lg border border-separator/40 bg-window/60 p-3 text-[11px] text-secondary">
          <ShieldAlert size={14} class="text-tertiary mt-0.5 flex-none" />
          <p class="leading-relaxed">
            Clipboard data is transferred directly over the local encrypted peer connection. Large images and clips are automatically chunked.
          </p>
        </div>
      </div>

    <!-- MEDIA TAB -->
    {:else if currentCategory === 'media'}
      <div class="flex flex-col gap-4">
        <!-- Playback Sync Direction -->
        <div class="rounded-xl border border-separator/60 bg-control/40 p-4">
          <div class="flex items-baseline justify-between gap-2">
            <h3 class="text-[13px] font-semibold text-label">Playback Direction</h3>
            <span class="text-[11px] text-tertiary">{selectedPlayback.label}</span>
          </div>
          <p class="mt-0.5 text-[12px] text-secondary">Phone is the audio/media source. Changing here updates the phone.</p>

          <fieldset class="mt-3 flex flex-col gap-1.5" disabled={saving} aria-label="Playback sync direction">
            {#each playbackModes as m (m.v)}
              {@const checked = settings.PlaybackMode === m.v}
              <button
                type="button"
                role="radio"
                aria-checked={checked}
                disabled={saving}
                onclick={() => { if (!checked) onPlaybackMode(m.v); }}
                class="flex w-full items-center gap-3 rounded-lg border p-2.5 text-left transition focus-visible:outline-2 focus-visible:outline-focus disabled:opacity-50 {checked
                  ? 'border-accent bg-accent/10 text-accent'
                  : 'border-separator/50 bg-window text-label hover:bg-altrow'}"
              >
                <span class="flex h-4 w-4 flex-none items-center justify-center rounded-full border {checked ? 'border-accent bg-accent' : 'border-separator bg-window'}" aria-hidden="true">
                  {#if checked}<span class="h-1.5 w-1.5 rounded-full bg-white"></span>{/if}
                </span>
                <div class="min-w-0 flex-1">
                  <div class="flex items-center gap-2">
                    <span class="text-[12px] font-medium leading-none text-label">{m.label}</span>
                    {#if m.v === 'android_to_mac' && !checked}
                      <span class="rounded bg-altrow px-1 py-0.5 text-[10px] text-tertiary">Default</span>
                    {/if}
                  </div>
                  <span class="mt-1 block text-[11px] leading-tight text-secondary">{m.desc}</span>
                </div>
                {#if checked}<Check size={14} class="text-accent" />{/if}
              </button>
            {/each}
          </fieldset>
        </div>

        <!-- Mac Output Destination -->
        <div class="rounded-xl border border-separator/60 bg-control/40 p-4">
          <div class="flex items-baseline justify-between gap-2">
            <h3 class="text-[13px] font-semibold text-label">Mac Output Destination</h3>
            <span class="text-[11px] text-tertiary">{systemOut ? 'App and system' : 'App only'}</span>
          </div>
          <p class="mt-0.5 text-[12px] text-secondary">Control whether active playback also integrates into macOS Control Center Now Playing.</p>

          <fieldset class="mt-3 flex flex-col gap-1.5" disabled={saving} aria-label="Mac playback output">
            <button
              type="button"
              role="radio"
              aria-checked={!systemOut}
              disabled={saving}
              onclick={() => { if (systemOut) onPlaybackOutput('inapp'); }}
              class="flex w-full items-center gap-3 rounded-lg border p-2.5 text-left transition focus-visible:outline-2 focus-visible:outline-focus disabled:opacity-50 {!systemOut
                ? 'border-accent bg-accent/10 text-accent'
                : 'border-separator/50 bg-window text-label hover:bg-altrow'}"
            >
              <span class="flex h-4 w-4 flex-none items-center justify-center rounded-full border {!systemOut ? 'border-accent bg-accent' : 'border-separator bg-window'}" aria-hidden="true">
                {#if !systemOut}<span class="h-1.5 w-1.5 rounded-full bg-white"></span>{/if}
              </span>
              <div class="min-w-0 flex-1">
                <span class="block text-[12px] font-medium leading-none text-label">In app only</span>
                <span class="mt-1 block text-[11px] leading-tight text-secondary">Show player and playback controls inside FuseItAll only</span>
              </div>
              {#if !systemOut}<Check size={14} class="text-accent" />{/if}
            </button>

            <button
              type="button"
              role="radio"
              aria-checked={systemOut}
              disabled={saving}
              onclick={() => { if (!systemOut) onPlaybackOutput('system'); }}
              class="flex w-full items-center gap-3 rounded-lg border p-2.5 text-left transition focus-visible:outline-2 focus-visible:outline-focus disabled:opacity-50 {systemOut
                ? 'border-accent bg-accent/10 text-accent'
                : 'border-separator/50 bg-window text-label hover:bg-altrow'}"
            >
              <span class="flex h-4 w-4 flex-none items-center justify-center rounded-full border {systemOut ? 'border-accent bg-accent' : 'border-separator bg-window'}" aria-hidden="true">
                {#if systemOut}<span class="h-1.5 w-1.5 rounded-full bg-white"></span>{/if}
              </span>
              <div class="min-w-0 flex-1">
                <span class="block text-[12px] font-medium leading-none text-label">App and system</span>
                <span class="mt-1 block text-[11px] leading-tight text-secondary">Also mirror current track in macOS Control Center Now Playing</span>
              </div>
              {#if systemOut}<Check size={14} class="text-accent" />{/if}
            </button>
          </fieldset>
        </div>
      </div>
    {/if}
  </div>

  {#if message}
    <div class="mt-2 text-center text-[11px] text-secondary" role="status" aria-live="polite">
      {message}
    </div>
  {/if}
</section>
