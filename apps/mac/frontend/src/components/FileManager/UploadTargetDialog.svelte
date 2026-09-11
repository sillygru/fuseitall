<!--
  SPDX-License-Identifier: AGPL-3.0-only

  Home-folder upload guard: warns that the target is the phone home
  folder and offers the folder list (Download recommended), home anyway,
  or cancel, with an optional remember-as-default checkbox.
  HIG Read: auxiliary dialog for one upload-target decision, following
  HIG Alerts, Buttons. Classic frost, no Liquid Glass.
-->
<script lang="ts">
  import { fade, scale } from 'svelte/transition';

  interface Props {
    open: boolean;
    folders: string[];
    suggested: string;
    busy?: boolean;
    onPick: (dir: string, remember: boolean) => void;
    onHomeAnyway: (remember: boolean) => void;
    onClose: () => void;
  }
  let { open, folders, suggested, busy = false, onPick, onHomeAnyway, onClose }: Props = $props();

  // Mutable selection: init to a static default and sync from props when the
  // dialog opens (effect below). Reading `suggested` here would capture only
  // the first value (state_referenced_locally).
  let selected = $state('Download');
  let remember = $state(false);

  $effect(() => {
    if (open) {
      selected = suggested || (folders.includes('Download') ? 'Download' : (folders[0] || 'Download'));
      remember = false;
    }
  });

  function onKey(e: KeyboardEvent) {
    if (e.key === 'Escape') onClose();
  }
</script>

{#if open}
  <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions: backdrop pointer-dismiss; keyboard path is Escape via onKey. -->
  <div class="fixed inset-0 z-40 flex items-center justify-center bg-black/30 p-4" transition:fade={{ duration: 150 }} onclick={(e) => { if (e.target === e.currentTarget) onClose(); }} onkeydown={onKey} role="presentation">
    <div role="dialog" aria-modal="true" tabindex="-1" aria-label="Upload to phone home folder" transition:scale={{ duration: 180, start: 0.96, opacity: 0 }} class="w-full max-w-[420px] rounded-[12px] bg-control p-4 shadow-xl">
      <h3 class="text-[13px] font-semibold text-label">Upload to phone home folder?</h3>
      <p class="mt-1 text-[12px] leading-snug text-secondary">This is the phone home folder. Files land loose at the top level. Upload to the Download folder instead, or pick a folder below.</p>
      <fieldset class="mt-3 max-h-[180px] overflow-auto rounded-md bg-window p-1" aria-label="Phone folder">
        {#each folders as f (f)}
          <label class="flex cursor-pointer items-center gap-2 rounded-md px-2 py-1.5 text-[12px] transition hover:bg-altrow">
            <input type="radio" name="upload-target" value={f} checked={selected === f} onchange={() => selected = f} disabled={busy} class="accent-[var(--color-accent)]" />
            <span class="min-w-0 flex-1 truncate text-label">{f}</span>
            {#if f === 'Download'}
              <span class="shrink-0 rounded-full bg-accent/15 px-1.5 py-0.5 text-[10px] font-medium text-accent">Recommended</span>
            {/if}
          </label>
        {:else}
          <p class="px-2 py-1.5 text-[12px] text-secondary">No folders listed. Download will be created.</p>
        {/each}
      </fieldset>
      <label class="mt-2 flex cursor-pointer items-center gap-2 px-1 text-[12px] text-secondary">
        <input type="checkbox" checked={remember} onchange={(e) => remember = (e.currentTarget as HTMLInputElement).checked} disabled={busy} class="accent-[var(--color-accent)]" />
        <span>Remember as default upload folder</span>
      </label>
      <div class="mt-4 flex justify-end gap-2">
        <button type="button" onclick={onClose} disabled={busy} class="h-7 rounded-md bg-window px-3 text-[13px] text-label transition hover:bg-altrow active:translate-y-[1px] disabled:opacity-50">Cancel</button>
        <button type="button" onclick={() => onHomeAnyway(remember)} disabled={busy} class="h-7 rounded-md bg-window px-3 text-[13px] text-label transition hover:bg-altrow active:translate-y-[1px] disabled:opacity-50">Upload here anyway</button>
        <button type="button" onclick={() => onPick(selected || 'Download', remember)} disabled={busy || !selected} class="h-7 rounded-md bg-accent px-3 text-[13px] font-medium text-accent-text transition hover:brightness-95 active:translate-y-[1px] disabled:opacity-50">Use {selected || 'Download'}</button>
      </div>
    </div>
  </div>
{/if}
