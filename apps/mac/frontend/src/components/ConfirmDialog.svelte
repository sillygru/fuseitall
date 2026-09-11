<!--
  SPDX-License-Identifier: AGPL-3.0-only

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
  <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions: backdrop pointer-dismiss; keyboard path is Escape via onKey. -->
  <div class="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4" transition:fade={{ duration: 150 }} onclick={(e) => { if (e.target === e.currentTarget) onCancel(); }} onkeydown={onKey} role="presentation">
    <div
      role="dialog"
      aria-modal="true"
      tabindex="-1"
      aria-label={title}
      transition:scale={{ duration: 180, start: 0.96, opacity: 0 }}
      class="w-full max-w-[420px] rounded-xl bg-control p-5" style="box-shadow: var(--shadow-float);"
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
