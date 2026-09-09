<!--
  Copyright (C) 2026 FuseItAll contributors.

  This program is free software: you can redistribute it and/or modify
  it under the terms of the GNU Affero General Public License as published
  by the Free Software Foundation, version 3 of the License. See LICENSE
  for details.

  Settings pane: notification switch + per-app filter and clipboard direction (HIG Windows/Color/Typography/Buttons).
  Reading this as: Settings pane for notifications + per-app filter + clipboard auto-sync, following HIG 4/5/8/11, no deviations.
-->
<script lang="ts">
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
    onNotifToggle: (enabled: boolean) => void;
    onNotifMode: (mode: string) => void;
    onAppMuted: (pkg: string, muted: boolean) => void;
    onAppAllowed: (pkg: string, allowed: boolean) => void;
    onClipboardMode: (mode: string) => void;
    onAppsRefresh: () => void;
  }

  let { settings, knownApps, appsSource, appsLoading, appsError, saving, message, updatedLabel, appVersion, onNotifToggle, onNotifMode, onAppMuted, onAppAllowed, onClipboardMode, onAppsRefresh }: Props = $props();

  const clipboardModes = [
    { v: 'both', label: 'Both directions', desc: 'Phone ↔ Mac', hint: 'Default' },
    { v: 'android_to_mac', label: 'Phone → Mac only', desc: 'Only phone copies sync to Mac' },
    { v: 'mac_to_android', label: 'Mac → Phone only', desc: 'Only Mac copies sync to phone' },
    { v: 'disabled', label: 'Off', desc: 'Manual Send still works' },
  ] as const;

  const selectedMode = $derived(clipboardModes.find((m) => m.v === settings.ClipboardMode) ?? clipboardModes[0]);
  const onlyAllowed = $derived(settings.NotifMode === 'only_allowed');
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
</script>

<section aria-label="Settings" class="section px-1 py-2">
  <h2 class="text-[15px] font-semibold tracking-tight text-label">Settings</h2>
  <p class="mt-0.5 text-[12px] text-secondary">Last change {updatedLabel} · Synced between devices</p>

  <!-- Notifications — single switch row, immediacy per HIG Buttons: switch for instant prefs -->
  <div class="mt-4 border-t border-b border-separator">
    <div class="flex items-center gap-3 px-1 py-3">
      <div class="min-w-0 flex-1">
        <p class="text-[12px] font-medium leading-none text-label">Phone notifications</p>
        <p class="mt-1 text-[11px] leading-tight text-secondary">Mirror phone notifications on this Mac</p>
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

  <!-- Per-app filter — mode radio + searchable toggle list. Progress bars are always dropped. -->
  <div class="mt-4">
    <div class="flex items-baseline justify-between gap-2">
      <h3 class="text-[11px] font-semibold uppercase tracking-wide text-secondary">Notification apps</h3>
      <span class="text-[11px] text-tertiary">{onlyAllowed ? `Allowed ${allowedCount}` : mutedCount ? `Muted ${mutedCount}` : 'All apps'}</span>
    </div>
    <p class="mt-1 text-[11px] leading-tight text-secondary">Choose which phone apps mirror here. Progress downloads never mirror. Changes sync to the phone.</p>
    <div class="mt-2 flex items-center gap-2 px-1">
      <button
        type="button"
        onclick={onAppsRefresh}
        disabled={saving || appsLoading}
        class="inline-flex h-7 flex-none items-center gap-1.5 rounded-md border border-separator bg-window px-2.5 text-[12px] font-medium text-label transition hover:bg-altrow focus-visible:outline-2 focus-visible:outline-focus disabled:opacity-50"
      >
        {#if appsLoading}
          <span class="spinner border border-separator border-t-accent" aria-hidden="true"></span>
          <span>Refreshing…</span>
        {:else}
          <span>Refresh</span>
        {/if}
      </button>
      <p class="min-w-0 flex-1 truncate text-[11px] leading-tight text-tertiary" aria-live="polite">
        {#if appsLoading}Loading phone apps…
        {:else if appsSource === 'phone'}{knownApps.length} phone apps
        {:else}Mirrored apps only{/if}
      </p>
    </div>
    {#if appsError}
      <p class="mt-1 px-1 text-[11px] leading-tight text-secondary" role="note">{appsError}</p>
    {/if}

    <fieldset class="mt-2 border-t border-b border-separator" disabled={saving || !settings.NotificationsEnabled} aria-label="Notification app filter mode">
      <legend class="sr-only">Notification app filter mode</legend>
      <button
        type="button"
        role="radio"
        aria-checked={!onlyAllowed}
        disabled={saving || !settings.NotificationsEnabled}
        onclick={() => { if (onlyAllowed) onNotifMode('all_except_muted'); }}
        class="flex w-full items-center gap-3 px-1 py-2.5 text-left transition focus-visible:outline-2 focus-visible:outline-focus focus-visible:outline-offset-[-2px] disabled:opacity-50 {!onlyAllowed ? 'text-accent' : 'text-label hover:bg-altrow active:bg-altrow'}"
      >
        <span class="flex h-4 w-4 flex-none items-center justify-center rounded-full border {!onlyAllowed ? 'border-accent bg-accent' : 'border-separator bg-window'}" aria-hidden="true">
          {#if !onlyAllowed}<span class="h-1.5 w-1.5 rounded-full bg-white"></span>{/if}
        </span>
        <span class="min-w-0 flex-1">
          <span class="block text-[12px] font-medium leading-none text-label">All except muted</span>
          <span class="mt-0.5 block text-[11px] leading-tight text-secondary">Mirror everything except apps you mute</span>
        </span>
        {#if !onlyAllowed}<span class="flex-none text-[12px] leading-none text-accent" aria-hidden="true">✓</span>{/if}
      </button>
      <button
        type="button"
        role="radio"
        aria-checked={onlyAllowed}
        disabled={saving || !settings.NotificationsEnabled}
        onclick={() => { if (!onlyAllowed) onNotifMode('only_allowed'); }}
        class="flex w-full items-center gap-3 border-t border-separator px-1 py-2.5 text-left transition focus-visible:outline-2 focus-visible:outline-focus focus-visible:outline-offset-[-2px] disabled:opacity-50 {onlyAllowed ? 'text-accent' : 'text-label hover:bg-altrow active:bg-altrow'}"
      >
        <span class="flex h-4 w-4 flex-none items-center justify-center rounded-full border {onlyAllowed ? 'border-accent bg-accent' : 'border-separator bg-window'}" aria-hidden="true">
          {#if onlyAllowed}<span class="h-1.5 w-1.5 rounded-full bg-white"></span>{/if}
        </span>
        <span class="min-w-0 flex-1">
          <span class="block text-[12px] font-medium leading-none text-label">Only allowed</span>
          <span class="mt-0.5 block text-[11px] leading-tight text-secondary">Mirror only apps you allow</span>
        </span>
        {#if onlyAllowed}<span class="flex-none text-[12px] leading-none text-accent" aria-hidden="true">✓</span>{/if}
      </button>
    </fieldset>

    {#if settings.NotificationsEnabled}
      <div class="mt-2 px-1">
        <input
          type="search"
          placeholder="Search apps"
          aria-label="Search apps"
          bind:value={appQuery}
          disabled={saving}
          class="w-full rounded-md border border-separator bg-window px-2.5 py-1.5 text-[12px] text-label placeholder:text-tertiary focus-visible:outline-2 focus-visible:outline-focus disabled:opacity-50"
        />
      </div>
      {#if visibleApps.length}
        <ul class="mt-1 border-t border-b border-separator" aria-label="Phone apps">
          {#each visibleApps as a (a.PackageName)}
            {@const on = onlyAllowed ? a.Allowed : !a.Muted}
            <li class="flex items-center gap-3 px-1 py-2 [&:not(:first-child)]:border-t [&:not(:first-child)]:border-separator">
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
        <p class="mt-2 px-1 text-[11px] leading-tight text-secondary">{knownApps.length ? 'No apps match this search.' : 'Tap Refresh to load the phone app list. Toggles stay saved either way.'}</p>
      {/if}
    {/if}
  </div>

  <!-- Clipboard — HIG radio group in a bordered control (mutually exclusive, 4 options, all visible) -->
  <div class="mt-4">
    <div class="flex items-baseline justify-between gap-2">
      <h3 class="text-[11px] font-semibold uppercase tracking-wide text-secondary">Clipboard</h3>
      <span class="text-[11px] text-tertiary">{selectedMode.label}</span>
    </div>
    <p class="mt-1 text-[11px] leading-tight text-secondary">Automatic sync direction. Changing here updates the phone.</p>

    <fieldset class="mt-2 border-t border-b border-separator" disabled={saving} aria-label="Clipboard auto sync direction">
      <legend class="sr-only">Clipboard auto sync direction</legend>
      {#each clipboardModes as m, i (m.v)}
        {@const checked = settings.ClipboardMode === m.v}
        <button
          type="button"
          role="radio"
          aria-checked={checked}
          disabled={saving}
          onclick={() => { if (!checked) onClipboardMode(m.v); }}
          class="flex w-full items-center gap-3 px-1 py-2.5 text-left transition
            focus-visible:outline-2 focus-visible:outline-focus focus-visible:outline-offset-[-2px]
            disabled:opacity-50
            {checked ? 'text-accent' : 'text-label hover:bg-altrow active:bg-altrow'}
            {i !== 0 ? 'border-t border-separator' : ''}"
        >
          <!-- Radio affordance -->
          <span
            class="flex h-4 w-4 flex-none items-center justify-center rounded-full border
              {checked ? 'border-accent bg-accent' : 'border-separator bg-window'}"
            aria-hidden="true"
          >
            {#if checked}
              <span class="h-1.5 w-1.5 rounded-full bg-white"></span>
            {/if}
          </span>

          <span class="min-w-0 flex-1">
            <span class="block text-[12px] font-medium leading-none {checked ? 'text-label' : 'text-label'}">{m.label}</span>
            <span class="mt-0.5 block text-[11px] leading-tight text-secondary">{m.desc}</span>
          </span>

          {#if m.v === 'both' && !checked}
            <span class="flex-none text-[10px] font-medium text-tertiary">Default</span>
          {/if}
          {#if checked}
            <span class="flex-none text-[12px] leading-none text-accent" aria-hidden="true">✓</span>
          {/if}
        </button>
      {/each}
    </fieldset>
    <!-- Limitation note: phone→Mac auto not yet available -->
    <div
      class="mt-2 flex gap-2 px-1 py-2"
      role="note"
      aria-label="Clipboard auto sync limitation"
      title="Auto sync phone to Mac is not yet available — use Clipboard pane Send manually. Mac to phone auto sync works."
    >
      <span class="flex-none text-[11px] leading-none text-tertiary" aria-hidden="true">ⓘ</span>
      <p class="text-[11px] leading-tight text-secondary">
        Auto sync <span class="font-medium text-label">phone → Mac</span> is not yet available — use the Clipboard pane’s <span class="font-medium">Send</span> manually. <span class="text-tertiary">Mac → phone</span> works automatically.
      </p>
    </div>
  </div>

  {#if message}
    <p class="mt-3 text-[11px] text-secondary" aria-live="polite">{message}</p>
  {/if}

  <p class="mt-4 border-t border-separator pt-3 text-[11px] text-tertiary">FuseItAll v{appVersion} · Last change {updatedLabel}</p>
</section>
