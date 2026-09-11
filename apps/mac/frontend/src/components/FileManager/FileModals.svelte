<!--
  SPDX-License-Identifier: AGPL-3.0-only

  Rename and delete confirmation dialogs for the file manager.
  HIG Read: auxiliary dialogs for rename and delete choices, following
  HIG Alerts, Buttons. Classic frost, no Liquid Glass.
-->
<script lang="ts">
  import { fade, scale } from 'svelte/transition';
  import { Pencil, Trash2 } from '@lucide/svelte';

  interface Props {
    renameTarget: string | null;
    renameValue: string;
    deleteTarget: string | null;
    onRenameValue: (v: string) => void;
    onRenameClose: () => void;
    onRenameConfirm: () => void;
    onDeleteClose: () => void;
    onDeleteConfirm: (target: string) => void;
  }
  let {
    renameTarget, renameValue, deleteTarget,
    onRenameValue, onRenameClose, onRenameConfirm,
    onDeleteClose, onDeleteConfirm,
  }: Props = $props();
</script>

{#if renameTarget}
  <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions: backdrop pointer-dismiss; keyboard path is Escape via onkeydown. -->
  <div class="fixed inset-0 z-40 flex items-center justify-center bg-black/30 p-4" transition:fade={{ duration: 150 }} onclick={(e) => { if (e.target === e.currentTarget) onRenameClose(); }} onkeydown={(e)=> e.key==='Escape' && onRenameClose()} role="presentation">
    <div role="dialog" aria-modal="true" tabindex="-1" aria-label="Rename" transition:scale={{ duration: 180, start: 0.96, opacity: 0 }} class="w-full max-w-[380px] rounded-[12px] border border-separator bg-control p-4 shadow-xl">
      <h3 class="text-[13px] font-semibold text-label">Rename</h3>
      <p class="mt-1 truncate text-[12px] tabular-nums text-secondary">{renameTarget}</p>
      <input value={renameValue} oninput={(e) => onRenameValue((e.currentTarget as HTMLInputElement).value)} placeholder="New name" aria-label="New name" class="mt-3 h-8 w-full rounded-md border border-separator bg-window px-2 text-[13px]" onkeydown={(e)=> e.key==='Enter' && onRenameConfirm()} />
      <div class="mt-4 flex justify-end gap-2">
        <button type="button" onclick={onRenameClose} class="h-7 rounded-md border border-separator bg-window px-3 text-[13px] text-label transition hover:bg-altrow active:translate-y-[1px]">Cancel</button>
        <button type="button" onclick={onRenameConfirm} class="inline-flex h-7 items-center gap-1.5 rounded-md bg-accent px-3 text-[13px] font-medium text-accent-text transition hover:brightness-95 active:translate-y-[1px]"><Pencil size={12} />Rename</button>
      </div>
    </div>
  </div>
{/if}

{#if deleteTarget}
  <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions: backdrop pointer-dismiss; keyboard path is Escape via onkeydown. -->
  <div class="fixed inset-0 z-40 flex items-center justify-center bg-black/30 p-4" transition:fade={{ duration: 150 }} onclick={(e) => { if (e.target === e.currentTarget) onDeleteClose(); }} onkeydown={(e)=> e.key==='Escape' && onDeleteClose()} role="presentation">
    <div role="dialog" aria-modal="true" tabindex="-1" aria-label="Delete file" transition:scale={{ duration: 180, start: 0.96, opacity: 0 }} class="w-full max-w-[380px] rounded-[12px] border border-separator bg-control p-4 shadow-xl">
      <div class="flex items-start gap-3">
        <span class="flex h-9 w-9 shrink-0 items-center justify-center rounded-full bg-bad/15 text-bad" aria-hidden="true"><Trash2 size={16} /></span>
        <div class="min-w-0">
          <h3 class="truncate text-[13px] font-semibold text-label">Delete “{deleteTarget.split('/').pop()}”?</h3>
          <p class="mt-1 text-[12px] leading-snug text-secondary">This removes it from the phone. This cannot be undone.</p>
        </div>
      </div>
      <div class="mt-4 flex justify-end gap-2">
        <button type="button" onclick={onDeleteClose} class="h-7 rounded-md border border-separator bg-window px-3 text-[13px] text-label transition hover:bg-altrow active:translate-y-[1px]">Keep</button>
        <button type="button" onclick={() => onDeleteConfirm(deleteTarget!)} class="h-7 rounded-md bg-bad px-3 text-[13px] font-medium text-destructive-text transition hover:brightness-95 active:translate-y-[1px]">Delete</button>
      </div>
    </div>
  </div>
{/if}
