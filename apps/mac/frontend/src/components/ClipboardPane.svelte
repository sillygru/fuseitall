<!--
  Copyright (C) 2026 FuseItAll contributors.

  This program is free software: you can redistribute it and/or modify
  it under the terms of the GNU Affero General Public License as published
  by the Free Software Foundation, version 3 of the License. See LICENSE
  for details.

  Clipboard pane: shows latest synced payload (text or image) and a single
  "Send clipboard" action that sends whatever is currently on the system
  pasteboard normalized to PNG. Auto sync when mode allows; TIFF/HEIC auto-converted to PNG.
-->
<script lang="ts">
  import type { ClipNotice } from '../backend';

  interface Props {
    clip: ClipNotice | null;
    pushing: boolean;
    message: string;
    onPushCurrent: () => void;
  }

  let { clip, pushing, message, onPushCurrent }: Props = $props();

  function directionArrow(origin: string): string {
    return origin === 'mac' ? '→' : '←';
  }

  let isImage = $derived(clip?.Kind === 'image' && !!clip?.ImageB64);
  let imageSrc = $derived(isImage ? `data:${clip!.Mime};base64,${clip!.ImageB64}` : '');
</script>

<section aria-label="Clipboard" class="card p-4">
  <h2 class="text-[13px] font-semibold text-label">Clipboard</h2>
  <p class="mt-0.5 text-[11px] text-secondary">Auto sync when mode allows. {#if clip?.Pending}Waiting to send…{/if}</p>
  <div
    class="mt-2 flex gap-2 rounded-md bg-altrow px-2.5 py-2"
    role="note"
  >
    <span class="flex-none text-[11px] leading-none text-tertiary" aria-hidden="true">ⓘ</span>
    <p class="text-[11px] leading-tight text-secondary">
      Copy text or an image (PNG/JPEG/WEBP/GIF/TIFF/HEIC ≤5 MiB) — it auto-syncs when paired. TIFF/HEIC are normalized to PNG on Send.
    </p>
  </div>

  {#if clip}
    <div class="mt-3 rounded-lg bg-window p-2.5">
      <p class="text-[11px] font-semibold text-secondary">
        Latest {directionArrow(clip.Origin)} {clip.Origin === 'mac' ? 'from this Mac' : 'from phone'} · {clip.Kind === 'image' ? clip.Mime : 'text'}
      </p>
      {#if isImage}
        <img src={imageSrc} alt="Clipboard image" class="mt-2 max-h-[320px] w-auto max-w-full rounded-md border border-border object-contain" loading="lazy" />
        <p class="mt-1 text-[11px] text-tertiary">{clip.Preview}</p>
      {:else}
        <p class="mt-1 line-clamp-4 text-[12px] text-label" data-copy={clip.Text}>{clip.Preview || 'Empty'}</p>
      {/if}
    </div>
  {:else}
    <p class="mt-3 rounded-lg bg-altrow p-3 text-[12px] text-secondary">Nothing synced yet. Copy something and it auto-syncs, or press Send clipboard.</p>
  {/if}

  <div class="mt-3 flex items-center gap-2">
    <button
      type="button"
      onclick={onPushCurrent}
      disabled={pushing}
      title="Send whatever is currently on the clipboard (text or image)"
      class="inline-flex h-7 flex-none items-center gap-2 rounded-md bg-accent px-3 text-[13px] font-medium text-accent-text transition hover:brightness-95 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus active:translate-y-[1px] disabled:cursor-not-allowed disabled:opacity-50"
    >
      {#if pushing}<span class="spinner" aria-hidden="true"></span><span>Sending…</span>
      {:else}<span>Send clipboard</span>{/if}
    </button>
    {#if message}
      <p class="truncate text-[12px] text-secondary" aria-live="polite">{message}</p>
    {/if}
  </div>
</section>
