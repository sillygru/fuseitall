<!--
  Copyright (C) 2026 FuseItAll contributors.

  This program is free software: you can redistribute it and/or modify
  it under the terms of the GNU Affero General Public License as published
  by the Free Software Foundation, version 3 of the License. See LICENSE
  for details.

  HIG Alert / confirmation dialog: destructive confirm needs explicit
  second step. No Liquid Glass — solid control surface, vibrant button.
-->
<script lang="ts">
  import { fade, scale } from 'svelte/transition';

  interface Props {
    open: boolean;
    title: string;
    body: string;
    confirmLabel: string;
    cancelLabel?: string;
    destructive?: boolean;
    busy?: boolean;
    onConfirm: () => void;
    onCancel: () => void;
  }

  let { open, title, body, confirmLabel, cancelLabel = 'Cancel', destructive = false, busy = false, onConfirm, onCancel }: Props = $props();

  function onKey(e: KeyboardEvent) {
    if (e.key === 'Escape') onCancel();
  }
</script>

{#if open}
  <!-- svelte-ignore a11y_click_events_have_key_events -->
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div class="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4" transition:fade={{ duration: 150 }} onclick={onCancel} onkeydown={onKey}>
    <div
      role="dialog"
      aria-modal="true"
      aria-label={title}
      transition:scale={{ duration: 180, start: 0.96, opacity: 0 }}
      class="w-full max-w-[420px] rounded-xl bg-control p-5 shadow-[0_12px_32px_rgba(0,0,0,0.35)] ring-1 ring-separator"
      onclick={(e) => e.stopPropagation()}
    >
      <h2 class="text-[15px] font-semibold text-label">{title}</h2>
      <p class="mt-1.5 text-[13px] leading-relaxed text-secondary">{body}</p>
      <div class="mt-5 flex justify-end gap-2">
        <button
          type="button"
          onclick={onCancel}
          disabled={busy}
          class="inline-flex h-8 items-center rounded-lg bg-window px-3.5 text-[13px] font-medium text-label disabled:opacity-50"
        >
          {cancelLabel}
        </button>
        <button
          type="button"
          onclick={onConfirm}
          disabled={busy}
          class="inline-flex h-8 items-center gap-2 rounded-lg px-3.5 text-[13px] font-medium transition disabled:opacity-50 {destructive ? 'bg-destructive text-destructive-text hover:brightness-95' : 'bg-accent text-accent-text hover:brightness-95'}"
        >
          {#if busy}<span class="spinner" aria-hidden="true"></span>{/if}
          <span>{confirmLabel}</span>
        </button>
      </div>
    </div>
  </div>
{/if}
