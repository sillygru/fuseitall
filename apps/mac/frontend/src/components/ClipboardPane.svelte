<!--
  SPDX-License-Identifier: AGPL-3.0-only

  Clipboard pane: shows latest synced payload (text or image) and a single
  "Send clipboard" action (manual bypasses mode + sensitive gates).
  Reading this as: primary window detail pane for explicit Send, following
  HIG Buttons/Alerts/Progress, with text-labels-not-glyphs deviation.
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

  const inlineB64Cap = 7100000;
  let isImage = $derived(clip?.Kind === 'image' && !!clip?.ImageB64);
  let isLarge = $derived(isImage && (clip!.ImageB64.length > inlineB64Cap));
  let imageSrc = $derived(isImage && !isLarge ? `data:${clip!.Mime};base64,${clip!.ImageB64}` : '');
  let largeMB = $derived(isLarge ? (Math.round((clip!.ImageB64.length * 3 / 4 / 1048576) * 10) / 10).toFixed(1) : '');
</script>

<section aria-label="Clipboard" class="section px-1 py-2">
  <h2 class="text-[15px] font-semibold text-label">Clipboard</h2>
  <p class="mt-0.5 text-[12px] text-secondary">Auto sync when mode allows. Manual Send always works. {#if clip?.Pending}Waiting to send…{/if}</p>
  <div
    class="mt-2 flex gap-2 px-1 py-2"
    role="note"
  >
    <span class="flex-none text-[11px] leading-none text-tertiary" aria-hidden="true">ⓘ</span>
    <p class="text-[11px] leading-tight text-secondary">
      Copy text or an image — it auto-syncs when paired. Inline ≤5 MiB; large images send in chunks (≤25 MiB). TIFF/HEIC are normalized to PNG on Send. Sensitive clips auto-skip unless enabled; Send always works.
    </p>
  </div>

  {#if clip}
    <div class="mt-3 border-t border-b border-separator px-1 py-2.5">
      <p class="text-[11px] font-semibold text-secondary">
        Latest {directionArrow(clip.Origin)} {clip.Origin === 'mac' ? 'from this Mac' : 'from phone'} · {clip.Kind === 'image' ? clip.Mime : 'text'}{#if clip.Sensitive} · sensitive{/if}
      </p>
      {#if isImage}
        {#if isLarge}
          <p class="mt-2 text-[12px] text-label">Large image ({largeMB} MB) synced — pasted, preview skipped.</p>
        {:else}
          <img src={imageSrc} alt="Synced clipboard content" class="mt-2 max-h-[320px] w-auto max-w-full rounded-md border border-border object-contain" loading="lazy" />
        {/if}
        <p class="mt-1 text-[11px] text-tertiary">{clip.Preview}</p>
      {:else}
        <p class="mt-1 line-clamp-4 text-[12px] text-label" data-copy={clip.Text}>{clip.Preview || 'Empty'}</p>
      {/if}
    </div>
  {:else}
    <p class="mt-3 px-1 py-2 text-[12px] text-secondary">Nothing synced yet. Copy something and it auto-syncs, or press Send clipboard.</p>
  {/if}

  <div class="mt-3 flex items-center gap-2">
    <button
      type="button"
      onclick={onPushCurrent}
      disabled={pushing}
      title="Send whatever is currently on the clipboard (text or image)"
      class="inline-flex h-7 flex-none items-center gap-2 rounded-md bg-accent px-3 text-[13px] font-medium text-accent-text transition hover:brightness-95 active:translate-y-[1px] disabled:cursor-not-allowed disabled:opacity-50"
    >
      {#if pushing}<span class="spinner" aria-hidden="true"></span><span>Sending…</span>
      {:else}<span>Send clipboard</span>{/if}
    </button>
    {#if message}
      <p class="truncate text-[12px] text-secondary" aria-live="polite">{message}</p>
    {/if}
  </div>
</section>
