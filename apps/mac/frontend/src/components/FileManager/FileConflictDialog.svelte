<!--
  SPDX-License-Identifier: AGPL-3.0-only

  Upload conflict dialog: file exists (Overwrite, If newer, Keep both,
  Skip, Stop) and folder exists (Merge, Overwrite all, Stop), each with
  an Apply to all checkbox. Follows the FileManager modal pattern.
  HIG Read: auxiliary dialog for a file conflict choice, following HIG
  Alerts, Buttons. Classic frost, no Liquid Glass.
-->
<script lang="ts">
  import { fade, scale } from 'svelte/transition';
  import type { FileConflict, FolderConflict, FileChoice, FolderChoice } from '../../lib/fileConflict';
  import { fmtConflictDate, fmtConflictSize } from '../../lib/fileConflict';

  interface Props {
    conflict: FileConflict | FolderConflict;
    targetLabel: string;
    onPick: (choice: FileChoice | FolderChoice, applyToAll: boolean) => void;
    onClose: () => void;
  }
  let { conflict, targetLabel, onPick, onClose }: Props = $props();
  let applyToAll = $state(false);
  let primaryBtn = $state<HTMLButtonElement | null>(null);

  function pick(c: FileChoice | FolderChoice) {
    onPick(c, applyToAll);
  }

  // Focus the primary (safe, non-destructive) action when the dialog opens.
  // Replaces the HTML autofocus attribute (a11y_autofocus): SPA-safe and
  // keyboard flow preserving. Only one branch mounts, so one binding suffices.
  $effect(() => {
    conflict;
    try { primaryBtn?.focus({ preventScroll: true }); } catch { /* ignore */ }
  });
</script>

<!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions: backdrop pointer-dismiss; keyboard path is Escape via onkeydown. -->
<div
  class="fixed inset-0 z-40 flex items-center justify-center bg-black/30 p-4"
  transition:fade={{ duration: 150 }}
  onclick={(e) => { if (e.target === e.currentTarget) onClose(); }}
  onkeydown={(e) => e.key === 'Escape' && onClose()}
  role="presentation"
>
  <div
    role="dialog"
    aria-modal="true"
    tabindex="-1"
    aria-label={conflict.kind === 'file' ? 'File already exists' : 'Folder already exists'}
    transition:scale={{ duration: 180, start: 0.96, opacity: 0 }}
    class="w-full max-w-[400px] rounded-[12px] border border-separator bg-control p-4 shadow-xl"
  >
    {#if conflict.kind === 'file'}
      <h3 class="truncate text-[13px] font-semibold text-label">“{conflict.name}” already exists</h3>
      <p class="mt-1 text-[12px] leading-snug text-secondary">
        A file with this name is already in {targetLabel}. Choose how to send it.
      </p>
      <dl class="mt-3 space-y-1 rounded-md bg-window px-2.5 py-2 text-[12px]">
        <div class="flex justify-between gap-2">
          <dt class="text-tertiary">Sending</dt>
          <dd class="truncate tabular-nums text-label">{fmtConflictSize(conflict.localSize)} · {fmtConflictDate(conflict.localMtime)}</dd>
        </div>
        <div class="flex justify-between gap-2">
          <dt class="text-tertiary">On phone</dt>
          <dd class="truncate tabular-nums text-label">{fmtConflictSize(conflict.remoteSize)} · {fmtConflictDate(conflict.remoteMtime)}</dd>
        </div>
      </dl>
      <div class="mt-3 flex flex-col gap-1.5">
        <button
          type="button"
          onclick={() => pick('overwrite')}
          class="inline-flex h-7 items-center justify-center rounded-md bg-bad px-3 text-[13px] font-medium text-destructive-text transition hover:brightness-95 active:translate-y-[1px]"
        >
          Overwrite
        </button>
        <button
          type="button"
          onclick={() => pick('if_newer')}
          class="inline-flex h-7 items-center justify-center rounded-md border border-separator bg-window px-3 text-[13px] text-label transition hover:bg-altrow active:translate-y-[1px]"
        >
          Overwrite if newer
        </button>
        <button
          type="button"
          bind:this={primaryBtn}
          onclick={() => pick('keep_both')}
          class="inline-flex h-7 items-center justify-center rounded-md bg-accent px-3 text-[13px] font-medium text-accent-text transition hover:brightness-95 active:translate-y-[1px]"
        >
          Keep both
        </button>
        <button
          type="button"
          onclick={() => pick('skip')}
          class="inline-flex h-7 items-center justify-center rounded-md px-3 text-[13px] text-secondary transition hover:bg-altrow hover:text-label active:translate-y-[1px]"
        >
          Skip
        </button>
      </div>
      <div class="mt-3 flex items-center justify-between gap-2 border-t border-separator pt-3">
        <label class="inline-flex cursor-pointer items-center gap-1.5 text-[12px] text-secondary">
          <input type="checkbox" bind:checked={applyToAll} class="h-3.5 w-3.5 accent-[var(--color-accent)]" />
          <span>Apply to all files</span>
        </label>
        <button
          type="button"
          onclick={() => pick('stop')}
          class="h-7 rounded-md border border-separator bg-window px-3 text-[13px] text-label transition hover:bg-altrow active:translate-y-[1px]"
        >
          Stop
        </button>
      </div>
    {:else}
      <h3 class="truncate text-[13px] font-semibold text-label">Folder “{conflict.name}” already exists</h3>
      <p class="mt-1 text-[12px] leading-snug text-secondary">
        Merge keeps the phone folder and adds new files. Overwrite replaces matching files inside. Other files stay in both cases.
      </p>
      <div class="mt-3 flex flex-col gap-1.5">
        <button
          type="button"
          bind:this={primaryBtn}
          onclick={() => pick('merge')}
          class="inline-flex h-7 items-center justify-center rounded-md bg-accent px-3 text-[13px] font-medium text-accent-text transition hover:brightness-95 active:translate-y-[1px]"
        >
          Merge
        </button>
        <button
          type="button"
          onclick={() => pick('overwrite')}
          class="inline-flex h-7 items-center justify-center rounded-md bg-bad px-3 text-[13px] font-medium text-destructive-text transition hover:brightness-95 active:translate-y-[1px]"
        >
          Overwrite matching files
        </button>
      </div>
      <div class="mt-3 flex items-center justify-between gap-2 border-t border-separator pt-3">
        <label class="inline-flex cursor-pointer items-center gap-1.5 text-[12px] text-secondary">
          <input type="checkbox" bind:checked={applyToAll} class="h-3.5 w-3.5 accent-[var(--color-accent)]" />
          <span>Apply to all folders</span>
        </label>
        <button
          type="button"
          onclick={() => pick('stop')}
          class="h-7 rounded-md border border-separator bg-window px-3 text-[13px] text-label transition hover:bg-altrow active:translate-y-[1px]"
        >
          Stop
        </button>
      </div>
    {/if}
  </div>
</div>
