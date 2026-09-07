<!--
  Copyright (C) 2026 FuseItAll contributors.

  This program is free software: you can redistribute it and/or modify
  it under the terms of the GNU Affero General Public License as published
  by the Free Software Foundation, version 3 of the License. See LICENSE
  for details.

  Settings pane: notification switch and clipboard direction (HIG Windows/Color/Typography/Buttons).
  Reading this as: Settings pane for notifications + clipboard auto-sync, following HIG 4/5/8/11, no deviations.
-->
<script lang="ts">
  import type { AppSettings } from '../backend';

  interface Props {
    settings: AppSettings;
    saving: boolean;
    message: string;
    updatedLabel: string;
    appVersion: string;
    onNotifToggle: (enabled: boolean) => void;
    onClipboardMode: (mode: string) => void;
  }

  let { settings, saving, message, updatedLabel, appVersion, onNotifToggle, onClipboardMode }: Props = $props();

  const clipboardModes = [
    { v: 'both', label: 'Both directions', desc: 'Phone ↔ Mac', hint: 'Default' },
    { v: 'android_to_mac', label: 'Phone → Mac only', desc: 'Only phone copies sync to Mac' },
    { v: 'mac_to_android', label: 'Mac → Phone only', desc: 'Only Mac copies sync to phone' },
    { v: 'disabled', label: 'Off', desc: 'Manual Send still works' },
  ] as const;

  const selectedMode = $derived(clipboardModes.find((m) => m.v === settings.ClipboardMode) ?? clipboardModes[0]);
</script>

<section aria-label="Settings" class="card p-4">
  <h2 class="text-[13px] font-semibold tracking-tight text-label">Settings</h2>
  <p class="mt-0.5 text-[11px] text-secondary">Last change {updatedLabel} · Synced between devices</p>

  <!-- Notifications — single switch row, immediacy per HIG Buttons: switch for instant prefs -->
  <div class="mt-4 overflow-hidden rounded-lg border border-separator">
    <div class="flex items-center gap-3 bg-window px-3 py-3">
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

  <!-- Clipboard — HIG radio group in a bordered control (mutually exclusive, 4 options, all visible) -->
  <div class="mt-4">
    <div class="flex items-baseline justify-between gap-2">
      <h3 class="text-[11px] font-semibold uppercase tracking-wide text-secondary">Clipboard</h3>
      <span class="text-[11px] text-tertiary">{selectedMode.label}</span>
    </div>
    <p class="mt-1 text-[11px] leading-tight text-secondary">Automatic sync direction. Changing here updates the phone.</p>

    <fieldset class="mt-2 overflow-hidden rounded-lg border border-separator bg-control" disabled={saving} aria-label="Clipboard auto sync direction">
      <legend class="sr-only">Clipboard auto sync direction</legend>
      {#each clipboardModes as m, i (m.v)}
        {@const checked = settings.ClipboardMode === m.v}
        <button
          type="button"
          role="radio"
          aria-checked={checked}
          disabled={saving}
          onclick={() => { if (!checked) onClipboardMode(m.v); }}
          class="flex w-full items-center gap-3 px-3 py-2.5 text-left transition
            focus-visible:outline-2 focus-visible:outline-focus focus-visible:outline-offset-[-2px]
            disabled:opacity-50
            {checked ? 'bg-accent text-accent-text' : 'bg-control text-label hover:bg-altrow active:bg-altrow'}
            {i !== 0 ? 'border-t border-separator' : ''}"
        >
          <!-- Radio affordance -->
          <span
            class="flex h-4 w-4 flex-none items-center justify-center rounded-full border
              {checked ? 'border-accent-text/70 bg-accent-text' : 'border-separator bg-window'}"
            aria-hidden="true"
          >
            {#if checked}
              <span class="h-1.5 w-1.5 rounded-full bg-accent"></span>
            {/if}
          </span>

          <span class="min-w-0 flex-1">
            <span class="block text-[12px] font-medium leading-none {checked ? 'text-accent-text' : 'text-label'}">{m.label}</span>
            <span class="mt-0.5 block text-[11px] leading-tight {checked ? 'text-accent-text/80' : 'text-secondary'}">{m.desc}</span>
          </span>

          {#if m.v === 'both' && !checked}
            <span class="flex-none rounded-full bg-altrow px-2 py-0.5 text-[10px] font-medium text-secondary">Default</span>
          {/if}
          {#if checked}
            <span class="flex-none text-[12px] leading-none text-accent-text" aria-hidden="true">✓</span>
          {/if}
        </button>
      {/each}
    </fieldset>
    <!-- Limitation note: phone→Mac auto not yet available -->
    <div
      class="mt-2 flex gap-2 rounded-md bg-altrow px-2.5 py-2"
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
