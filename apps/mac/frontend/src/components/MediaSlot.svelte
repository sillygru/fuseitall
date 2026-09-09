<!--
  Copyright (C) 2026 FuseItAll contributors.

  This program is free software: you can redistribute it and/or modify
  it under the terms of the GNU Affero General Public License as published
  by the Free Software Foundation, version 3 of the License. See LICENSE
  for details.

  Sidebar media slot: quick controls plus the live phone player.
  Never shows fake tracks; idle renders Not playing with disabled controls.
  Borderless rows only: no boxes in the sidebar. Layout splits to match
  the reference window: controls above the nav, player pinned above
  Settings.
-->
<script lang="ts">
  import { Camera, ChevronDown, Moon, Music, Pause, Play, SkipBack, SkipForward } from '@lucide/svelte';
  import type { PlaybackView } from '../backend';

  interface Props {
    layout?: 'controls' | 'player' | 'both';
    playback?: PlaybackView | null;
    canCommand?: boolean;
    busyCmd?: string;
    onCommand?: (cmd: string) => void;
  }

  let { layout = 'both', playback = null, canCommand = false, busyCmd = '', onCommand }: Props = $props();
  let showControls = $derived(layout === 'controls' || layout === 'both');
  let showPlayer = $derived(layout === 'player' || layout === 'both');

  const hasState = $derived(!!playback?.HasState && !!playback.Title);
  const isPlaying = $derived(playback?.State === 'playing');
  const title = $derived(hasState ? (playback?.Title ?? '') : 'Not playing');
  const subtitle = $derived(
    hasState
      ? [playback?.Artist, playback?.App].filter(Boolean).join(' · ') || 'From the phone'
      : 'Nothing from the phone right now',
  );
  const progress = $derived(
    playback && playback.DurationMs > 0
      ? Math.max(0, Math.min(1, playback.PositionMs / playback.DurationMs))
      : 0,
  );
  const artSrc = $derived(
    playback?.ArtworkB64 ? `data:${playback.ArtworkMime || 'image/jpeg'};base64,${playback.ArtworkB64}` : '',
  );

  function fmt(ms: number): string {
    if (!ms || ms <= 0) return '0:00';
    const s = Math.floor(ms / 1000);
    const m = Math.floor(s / 60);
    return `${m}:${String(s % 60).padStart(2, '0')}`;
  }

  function send(cmd: string): void {
    if (!canCommand || !hasState || busyCmd || !onCommand) return;
    onCommand(cmd);
  }
</script>

{#if showControls}
  <div class="px-2 pt-2">
    <div class="flex items-center gap-1 px-2.5 pb-1.5">
      <p class="flex-1 text-[11px] font-semibold uppercase tracking-wide text-secondary">Quick Controls</p>
      <span class="flex items-center gap-0.5 text-tertiary" aria-hidden="true">
        <span class="flex h-5 w-5 items-center justify-center rounded-full bg-altrow text-[10px]">···</span>
        <ChevronDown size={13} />
      </span>
    </div>
    <div class="flex gap-1 px-2.5">
      <button
        type="button"
        disabled
        aria-disabled="true"
        title="Night mode is not available yet"
        class="flex h-9 flex-1 items-center justify-center rounded-md text-secondary opacity-60 hover:bg-altrow"
      ><Moon size={16} aria-hidden="true" /></button>
      <button
        type="button"
        disabled
        aria-disabled="true"
        title="Camera shortcut is not available yet"
        class="relative flex h-9 flex-1 items-center justify-center rounded-md text-secondary opacity-60 hover:bg-altrow"
      >
        <Camera size={16} aria-hidden="true" />
      </button>
    </div>
  </div>
{/if}

{#if showPlayer}
  <div class="border-t border-separator px-2 pb-2 pt-2">
    <button type="button" disabled aria-disabled="true" class="flex w-full items-center gap-1 px-2.5 text-left text-[12px] text-secondary opacity-70">
      <Music size={13} aria-hidden="true" />
      <span class="flex-1 truncate">{hasState ? title : 'Not playing'}</span>
      {#if isPlaying}<span class="h-1.5 w-1.5 flex-none rounded-full bg-ok" aria-label="Playing"></span>{/if}
      <ChevronDown size={13} aria-hidden="true" />
    </button>
    <div class="mt-1.5 px-2.5 py-2">
      <div class="flex items-center gap-2.5">
        <span class="flex h-10 w-10 flex-none items-center justify-center overflow-hidden rounded-md bg-altrow text-tertiary" aria-hidden="true">
          {#if artSrc}
            <img src={artSrc} alt="" class="h-10 w-10 object-cover" loading="lazy" />
          {:else}
            <Music size={16} />
          {/if}
        </span>
        <div class="min-w-0 flex-1">
          <p class="truncate text-[13px] font-medium text-label">{title}</p>
          <p class="truncate text-[11px] text-tertiary">{subtitle}</p>
        </div>
      </div>
      {#if hasState && playback && playback.DurationMs > 0}
        <div class="mt-2" role="img" aria-label={`Position ${fmt(playback.PositionMs)} of ${fmt(playback.DurationMs)}`}>
          <div class="h-1 overflow-hidden rounded-full bg-separator">
            <div class="h-full rounded-full bg-accent transition-[width]" style="width: {Math.round(progress * 100)}%"></div>
          </div>
          <div class="mt-1 flex justify-between text-[10px] tabular-nums text-tertiary" aria-hidden="true">
            <span>{fmt(playback.PositionMs)}</span>
            <span>{fmt(playback.DurationMs)}</span>
          </div>
        </div>
      {/if}
      <div class="mt-1.5 flex items-center justify-center gap-2">
        <button
          type="button"
          aria-label="Previous track"
          disabled={!hasState || !canCommand || !!busyCmd}
          onclick={() => send('prev')}
          class="flex h-11 w-11 items-center justify-center rounded-full text-label transition hover:bg-altrow focus-visible:outline-2 focus-visible:outline-focus disabled:opacity-40 active:translate-y-[1px]"
        ><SkipBack size={16} aria-hidden="true" /></button>
        <button
          type="button"
          aria-label={isPlaying ? 'Pause' : 'Play'}
          disabled={!hasState || !canCommand || !!busyCmd}
          onclick={() => send(isPlaying ? 'pause' : 'play')}
          class="flex h-11 w-11 items-center justify-center rounded-full bg-accent text-accent-text transition hover:brightness-95 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus disabled:opacity-40 active:translate-y-[1px]"
        >
          {#if busyCmd}
            <span class="spinner border border-white/40 border-t-white" aria-hidden="true"></span>
          {:else if isPlaying}
            <Pause size={15} aria-hidden="true" />
          {:else}
            <Play size={15} aria-hidden="true" />
          {/if}
        </button>
        <button
          type="button"
          aria-label="Next track"
          disabled={!hasState || !canCommand || !!busyCmd}
          onclick={() => send('next')}
          class="flex h-11 w-11 items-center justify-center rounded-full text-label transition hover:bg-altrow focus-visible:outline-2 focus-visible:outline-focus disabled:opacity-40 active:translate-y-[1px]"
        ><SkipForward size={16} aria-hidden="true" /></button>
      </div>
      {#if hasState && !canCommand}
        <p class="mt-1 text-center text-[10px] leading-tight text-tertiary">View only for this direction</p>
      {/if}
    </div>
  </div>
{/if}
