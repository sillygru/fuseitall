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
    paired?: boolean;
    canCommand?: boolean;
    busyCmd?: string;
    onCommand?: (cmd: string) => void;
    onEnableControl?: () => void;
  }

  let { layout = 'both', playback = null, paired = false, canCommand = false, busyCmd = '', onCommand, onEnableControl }: Props = $props();
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
  // UI-clock interpolation only (never the network): pushes arrive on real
  // track/state changes; while playing the bar advances locally from
  // PositionMs + (now - UpdatedMs), capped at DurationMs. The tick runs
  // only while playing; offline the bar freezes at the disconnect moment
  // instead of advancing on stale state.
  let nowMs = $state(Date.now());
  let frozenAt = $state<number | null>(null);
  $effect(() => {
    if (paired) frozenAt = null;
    else if (frozenAt === null) frozenAt = Date.now();
  });
  $effect(() => {
    if (!isPlaying || !paired) return;
    const id = setInterval(() => {
      nowMs = Date.now();
    }, 500);
    return () => clearInterval(id);
  });
  const clockMs = $derived(frozenAt ?? nowMs);
  const displayPosition = $derived.by(() => {
    const pos = playback?.PositionMs ?? 0;
    if (playback?.State !== 'playing') return pos;
    const dur = playback?.DurationMs ?? 0;
    const updated = playback?.UpdatedMs ?? 0;
    if (!updated || dur <= 0) return pos;
    return Math.max(0, Math.min(dur, pos + Math.max(0, clockMs - updated)));
  });
  const progress = $derived(
    playback && playback.DurationMs > 0
      ? Math.max(0, Math.min(1, displayPosition / playback.DurationMs))
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

  function enableControl(): void {
    onEnableControl?.();
  }

  function send(cmd: string): void {
    if (!hasState || !paired || busyCmd || !onCommand) return;
    if (!canCommand) {
      onEnableControl?.();
    }
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
    <div class="relative overflow-hidden rounded-xl transition-all duration-300 {artSrc ? 'border border-separator/40 bg-control/20 shadow-sm' : ''}">
      {#if artSrc}
        <!-- Ambient blurred artwork bleed layer -->
        <div class="pointer-events-none absolute inset-0 -z-10 overflow-hidden select-none" aria-hidden="true">
          <img
            src={artSrc}
            alt=""
            class="h-full w-full object-cover scale-150 blur-2xl saturate-150 opacity-40 dark:opacity-45 motion-reduce:hidden"
          />
          <div class="absolute inset-0 bg-gradient-to-b from-window/35 via-window/50 to-window/70 dark:from-window/45 dark:via-window/60 dark:to-window/80 backdrop-blur-xs motion-reduce:bg-window/90"></div>
        </div>
      {/if}

      <button type="button" disabled aria-disabled="true" class="flex w-full items-center gap-1 px-2.5 pt-2 pb-0.5 text-left text-[12px] text-secondary opacity-70">
        <Music size={13} aria-hidden="true" />
        <span class="flex-1 truncate">{hasState ? title : 'Not playing'}</span>
        {#if isPlaying}<span class="h-1.5 w-1.5 flex-none rounded-full bg-ok" aria-label="Playing"></span>{/if}
        <ChevronDown size={13} aria-hidden="true" />
      </button>
      <div class="px-2.5 pb-2.5 pt-1.5">
        <div class="flex items-center gap-2.5">
          <span class="flex h-11 w-11 flex-none items-center justify-center overflow-hidden rounded-lg bg-altrow text-tertiary shadow-md shadow-black/25 ring-1 ring-black/10 dark:ring-white/10" aria-hidden="true">
            {#if artSrc}
              <img src={artSrc} alt="" class="h-11 w-11 object-cover" loading="lazy" />
            {:else}
              <Music size={16} />
            {/if}
          </span>
          <div class="min-w-0 flex-1">
            <p class="truncate text-[13px] font-medium text-label">{title}</p>
            <p class="truncate text-[11px] text-secondary">{subtitle}</p>
          </div>
        </div>
        {#if hasState && playback && playback.DurationMs > 0}
          <div class="mt-2" role="img" aria-label={`Position ${fmt(displayPosition)} of ${fmt(playback.DurationMs)}`}>
            <div class="h-1 overflow-hidden rounded-full bg-separator/80 dark:bg-white/15">
              <div class="h-full rounded-full bg-accent transition-[width]" style="width: {Math.round(progress * 100)}%"></div>
            </div>
            <div class="mt-1 flex justify-between text-[10px] tabular-nums text-tertiary" aria-hidden="true">
              <span>{fmt(displayPosition)}</span>
              <span>{fmt(playback.DurationMs)}</span>
            </div>
          </div>
        {/if}
        <div class="mt-1.5 flex items-center justify-center gap-2">
          <button
            type="button"
            aria-label="Previous track"
            disabled={!hasState || !paired || !!busyCmd}
            onclick={() => send('prev')}
            class="flex h-11 w-11 items-center justify-center rounded-full text-label transition hover:bg-altrow/80 focus-visible:outline-2 focus-visible:outline-focus disabled:opacity-40 active:translate-y-[1px]"
          ><SkipBack size={16} aria-hidden="true" /></button>
          <button
            type="button"
            aria-label={isPlaying ? 'Pause' : 'Play'}
            disabled={!hasState || !paired || !!busyCmd}
            onclick={() => send(isPlaying ? 'pause' : 'play')}
            class="flex h-11 w-11 items-center justify-center rounded-full bg-accent text-accent-text shadow-sm shadow-accent/30 transition hover:brightness-95 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus disabled:opacity-40 active:translate-y-[1px]"
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
            disabled={!hasState || !paired || !!busyCmd}
            onclick={() => send('next')}
            class="flex h-11 w-11 items-center justify-center rounded-full text-label transition hover:bg-altrow/80 focus-visible:outline-2 focus-visible:outline-focus disabled:opacity-40 active:translate-y-[1px]"
          ><SkipForward size={16} aria-hidden="true" /></button>
        </div>
        {#if hasState && !canCommand}
          <div class="mt-1.5 flex items-center justify-center gap-1.5 text-center">
            <span class="text-[10px] text-tertiary">View only</span>
            <span class="text-[10px] text-tertiary">·</span>
            <button
              type="button"
              onclick={enableControl}
              class="text-[10px] font-medium text-accent hover:underline focus-visible:outline-2 focus-visible:outline-focus"
            >
              Enable control from Mac
            </button>
          </div>
        {/if}
      </div>
    </div>
  </div>
{/if}
