<!--
  Copyright (C) 2026 FuseItAll contributors.

  This program is free software: you can redistribute it and/or modify
  it under the terms of the GNU Affero General Public License as published
  by the Free Software Foundation, version 3 of the License. See LICENSE
  for details.

  Settings pane: clipboard direction, notification master switch, and the
  About row. Every change stamps when it happened; the newest change wins
  across devices.
-->
<script lang="ts">
  import type { AppSettings } from '../backend';

  interface Props {
    settings: AppSettings;
    saving: boolean;
    message: string;
    updatedLabel: string;
    appVersion: string;
    onMode: (mode: string) => void;
    onNotifToggle: (enabled: boolean) => void;
  }

  let { settings, saving, message, updatedLabel, appVersion, onMode, onNotifToggle }: Props = $props();

  const modes = [
    { id: 'off', label: 'Off ∅' },
    { id: 'mac_to_phone', label: 'Mac → Phone' },
    { id: 'phone_to_mac', label: '← Phone' },
    { id: 'two_way', label: 'Two-way ⇄' },
  ];
</script>

<section aria-label="Settings" class="card p-4">
  <h2 class="text-[13px] font-semibold text-label">Settings</h2>
  <p class="mt-0.5 text-[11px] text-secondary">Last change {updatedLabel}.</p>

  <div class="mt-3">
    <p class="text-[12px] font-medium text-label">Clipboard sync</p>
    <div class="mt-1.5 grid grid-cols-2 gap-1.5" role="radiogroup" aria-label="Clipboard sync direction">
      {#each modes as m (m.id)}
        <button
          type="button"
          role="radio"
          aria-checked={settings.ClipboardMode === m.id}
          disabled={saving}
          onclick={() => onMode(m.id)}
          title={m.label}
          class="rounded-lg border px-2 py-2 text-[12px] font-medium transition focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus active:translate-y-[1px] disabled:cursor-not-allowed disabled:opacity-50 {settings.ClipboardMode === m.id
            ? 'border-accent bg-accent text-accent-text'
            : 'border-separator bg-window text-label hover:border-focus'}"
        >{m.label}</button>
      {/each}
    </div>
  </div>

  <div class="mt-3 flex items-center gap-2">
    <div class="min-w-0 flex-1">
      <p class="text-[12px] font-medium text-label">Phone notifications</p>
      <p class="text-[11px] text-secondary">Mirror phone notifications on this Mac.</p>
    </div>
    <button
      type="button"
      role="switch"
      aria-checked={settings.NotificationsEnabled}
      aria-label="Phone notifications"
      disabled={saving}
      onclick={() => onNotifToggle(!settings.NotificationsEnabled)}
      class="flex h-6 w-11 flex-none items-center rounded-full px-0.5 transition disabled:opacity-50 {settings.NotificationsEnabled ? 'justify-end bg-accent' : 'justify-start bg-separator'}"
    >
      <span class="h-5 w-5 rounded-full bg-control shadow" aria-hidden="true"></span>
    </button>
  </div>

  {#if message}
    <p class="mt-2 text-[12px] text-secondary" aria-live="polite">{message}</p>
  {/if}

  <div class="mt-3 border-t border-separator pt-2.5">
    <p class="text-[11px] text-tertiary">FuseItAll v{appVersion} · Last change {updatedLabel}</p>
  </div>
</section>
