<!--
  Copyright (C) 2026 FuseItAll contributors.

  This program is free software: you can redistribute it and/or modify
  it under the terms of the GNU Affero General Public License as published
  by the Free Software Foundation, version 3 of the License. See LICENSE
  for details.

  Clipboard pane: shows latest synced text and a manual push box.
  Manual only: Send now posts immediately when paired, else errors.
-->
<script lang="ts">
  import type { ClipNotice } from '../backend';

  interface Props {
    clip: ClipNotice | null;
    pushing: boolean;
    message: string;
    onPush: (text: string) => void;
  }

  let { clip, pushing, message, onPush }: Props = $props();

  let draft = $state('');

  function directionArrow(origin: string): string {
    return origin === 'mac' ? '→' : '←';
  }
</script>

<section aria-label="Clipboard" class="card p-4">
  <h2 class="text-[13px] font-semibold text-label">Clipboard</h2>
  <p class="mt-0.5 text-[11px] text-secondary">Auto sync when mode allows + manual Send. {#if clip?.Pending}Waiting to send…{/if}</p>
  <div
    class="mt-2 flex gap-2 rounded-md bg-altrow px-2.5 py-2"
    role="note"
    title="Auto sync Mac to phone works. Phone to Mac auto sync is not yet available — use Send on the phone."
  >
    <span class="flex-none text-[11px] leading-none text-tertiary" aria-hidden="true">ⓘ</span>
    <p class="text-[11px] leading-tight text-secondary">
      <span class="font-medium text-label">Mac → phone</span> auto sync works. <span class="font-medium">Phone → Mac</span> auto is not yet available — use <span class="font-medium">Send</span> on the phone.
    </p>
  </div>

  {#if clip}
    <div class="mt-3 rounded-lg bg-window p-2.5">
      <p class="text-[11px] font-semibold text-secondary">
        Latest {directionArrow(clip.Origin)} {clip.Origin === 'mac' ? 'from this Mac' : 'from phone'}
      </p>
      <p class="mt-1 line-clamp-4 text-[12px] text-label" data-copy={clip.Text}>{clip.Preview || 'Empty'}</p>
    </div>
  {:else}
    <p class="mt-3 rounded-lg bg-altrow p-3 text-[12px] text-secondary">Nothing synced yet. Type text below and Send now.</p>
  {/if}

  <div class="mt-3">
    <label for="clip-draft" class="text-[12px] font-medium text-label">Push from this Mac</label>
    <textarea
      id="clip-draft"
      bind:value={draft}
      rows={3}
      maxlength={262144}
      placeholder="Type text to send to the phone…"
      class="mt-1.5 w-full resize-y rounded-lg bg-window px-2.5 py-2 text-[12px] text-label placeholder:text-tertiary focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-focus"
    ></textarea>
    <div class="mt-2 flex items-center gap-2">
      <button
        type="button"
        onclick={() => { const t = draft; draft = ''; onPush(t); }}
        disabled={pushing || !draft.trim()}
        title="Send clipboard text now"
        class="inline-flex h-7 flex-none items-center gap-2 rounded-md bg-accent px-3 text-[13px] font-medium text-accent-text transition hover:brightness-95 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus active:translate-y-[1px] disabled:cursor-not-allowed disabled:opacity-50"
      >
        {#if pushing}<span class="spinner" aria-hidden="true"></span><span>Sending…</span>
        {:else}<span>Send now</span>{/if}
      </button>
      {#if message}
        <p class="truncate text-[12px] text-secondary" aria-live="polite">{message}</p>
      {/if}
    </div>
  </div>
</section>
