<!--
  SPDX-License-Identifier: AGPL-3.0-only

  Sidebar media slot: quick controls plus the live phone player.
  The player is intentionally part of the sidebar rhythm rather than a
  separate floating card: one shared divider, restrained artwork bleed,
  compact metadata, and the same monochrome control language as the rest
  of the app. Never shows fake tracks; idle renders Not playing with
  disabled controls.
-->
<script lang="ts">
  import { Camera, Moon, Music, Pause, Play, SkipBack, SkipForward } from '@lucide/svelte';
  import type { DNDView, PlaybackView } from '../backend';

  interface Props {
    layout?: 'controls' | 'player' | 'both';
    playback?: PlaybackView | null;
    dnd?: DNDView | null;
    paired?: boolean;
    canCommand?: boolean;
    busyCmd?: string;
    busyDnd?: boolean;
    onCommand?: (cmd: string) => void;
    onEnableControl?: () => void;
    onToggleDnd?: () => void;
  }

  let { layout = 'both', playback = null, dnd = null, paired = false, canCommand = false, busyCmd = '', busyDnd = false, onCommand, onEnableControl, onToggleDnd }: Props = $props();
  let showControls = $derived(layout === 'controls' || layout === 'both');
  // One restrained accent sampled from the cover for the play button and
  // progress only. No wash, no ambient backdrop, no glow: those read as
  // generated decoration. Muted covers pass through; vivid ones are pulled
  // toward neutral in the sampler so the button never goes neon.
  let mediaColor = $state({
    accent: 'var(--accent)',
    text: 'var(--accent-text)',
  });
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

  // Pull one restrained accent from the cover so the player feels like one
  // material surface. The image is a data URL from the phone, so canvas
  // sampling remains local and cannot introduce a cross-origin read.
  $effect(() => {
    const src = artSrc;
    if (!src) {
      mediaColor = {
        accent: 'var(--accent)',
        text: 'var(--accent-text)',
      };
      return;
    }

    let cancelled = false;
    const image = new Image();
    image.onload = () => {
      if (cancelled) return;
      try {
        const canvas = document.createElement('canvas');
        canvas.width = 32;
        canvas.height = 32;
        const context = canvas.getContext('2d', { willReadFrequently: true });
        if (!context) return;
        context.drawImage(image, 0, 0, 32, 32);
        const pixels = context.getImageData(0, 0, 32, 32).data;
        let red = 0;
        let green = 0;
        let blue = 0;
        let weightTotal = 0;

        for (let i = 0; i < pixels.length; i += 4) {
          const r = pixels[i];
          const g = pixels[i + 1];
          const b = pixels[i + 2];
          const max = Math.max(r, g, b);
          const min = Math.min(r, g, b);
          const saturation = max === 0 ? 0 : (max - min) / max;
          const brightness = (r + g + b) / (255 * 3);
          const weight = 1 + saturation * 2 + (brightness > 0.08 && brightness < 0.92 ? 0.5 : 0);
          red += r * weight;
          green += g * weight;
          blue += b * weight;
          weightTotal += weight;
        }

        const r = Math.round(red / weightTotal);
        const g = Math.round(green / weightTotal);
        const b = Math.round(blue / weightTotal);
        // Pull vivid covers a quarter of the way toward neutral gray so the
        // glow reads premium on the void rather than neon. Muted covers
        // pass through untouched.
        const gray = Math.round(0.2126 * r + 0.7152 * g + 0.0722 * b);
        const cr = Math.round(r + (gray - r) * 0.25);
        const cg = Math.round(g + (gray - g) * 0.25);
        const cb = Math.round(b + (gray - b) * 0.25);
        const luminance = (0.2126 * cr + 0.7152 * cg + 0.0722 * cb) / 255;
        const accent = `rgb(${cr} ${cg} ${cb})`;
        const text = luminance > 0.58 ? '#101010' : '#f8f8f5';

        mediaColor = { accent, text };
      } catch {
        // Keep the neutral player if an unusual artwork payload cannot be read.
      }
    };
    image.src = src;

    return () => {
      cancelled = true;
      image.onload = null;
    };
  });

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
    </div>
    <div class="flex gap-1.5 px-2.5">
      <button
        type="button"
        disabled={!paired || busyDnd}
        aria-disabled={!paired || busyDnd}
        aria-pressed={dnd?.Enabled ?? false}
        onclick={() => onToggleDnd?.()}
        title={!paired
          ? 'Connect phone to toggle Do Not Disturb'
          : dnd?.Enabled
            ? 'Do Not Disturb is on (tap to turn off)'
            : 'Do Not Disturb is off (tap to turn on)'}
        class="group flex h-9 flex-1 items-center justify-center rounded-[9px] transition-all duration-150 {!paired ? 'opacity-50 cursor-not-allowed' : 'active:translate-y-[0.5px]'} {dnd?.Enabled
          ? 'bg-dnd text-dnd-text hover:opacity-90'
          : 'bg-altrow/60 text-secondary hover:bg-altrow hover:text-label'}"
      >
        {#if busyDnd}
          <span class="spinner" style="--accent: currentColor;" aria-hidden="true"></span>
        {:else}
          <Moon
            size={16}
            fill={dnd?.Enabled ? 'currentColor' : 'none'}
            strokeWidth={dnd?.Enabled ? 1 : 2}
            class="transition-colors duration-150 {dnd?.Enabled ? 'text-dnd-text' : 'text-secondary group-hover:text-label'}"
            aria-hidden="true"
          />
        {/if}
      </button>
      <button
        type="button"
        disabled
        aria-disabled="true"
        title="Camera shortcut is not available yet"
        class="flex h-9 flex-1 items-center justify-center rounded-[9px] bg-altrow/40 text-secondary opacity-50 transition"
      >
        <Camera size={16} aria-hidden="true" />
      </button>
    </div>
  </div>
{/if}

{#if showPlayer}
  <section
    class="media-player relative my-1 w-full min-w-0 shrink-0 px-3 pb-2 pt-2.5"
    style={`--media-accent: ${mediaColor.accent}; --media-accent-text: ${mediaColor.text};`}
    aria-label="Now playing"
  >
    <div class="media-content min-w-0 w-full">
      <div class="flex items-center gap-2 px-0.5 pb-2">
        <span class="media-icon flex h-5 w-5 items-center justify-center rounded-[6px]" aria-hidden="true">
          <Music size={12} strokeWidth={2.2} />
        </span>
        <p class="flex-1 text-[11px] font-semibold uppercase tracking-[0.14em] text-secondary">Now Playing</p>
        {#if hasState && !isPlaying}
          <span class="text-[10px] text-tertiary">Paused</span>
        {/if}
      </div>

      <div class="flex items-center gap-2.5">
        <span class="flex h-12 w-12 flex-none items-center justify-center overflow-hidden rounded-[10px] bg-altrow text-tertiary" aria-hidden="true">
          {#if artSrc}
            <img src={artSrc} alt="" class="h-12 w-12 object-cover" loading="lazy" />
          {:else}
            <Music size={17} />
          {/if}
        </span>
        <div class="min-w-0 flex-1 overflow-hidden">
          <p class="truncate text-[14px] font-semibold tracking-[-0.015em] text-label">{title}</p>
          <p class="mt-0.5 truncate text-[11px] text-secondary">{subtitle}</p>
        </div>
      </div>

      {#if hasState && playback && playback.DurationMs > 0}
        <div class="mt-3" role="img" aria-label={`Position ${fmt(displayPosition)} of ${fmt(playback.DurationMs)}`}>
          <div class="h-1 overflow-hidden rounded-full bg-separator/50">
            <div class="media-progress-fill h-full rounded-full transition-[width] duration-300" style={`width: ${Math.round(progress * 100)}%; background: var(--media-accent);`}></div>
          </div>
          <div class="mt-1 flex justify-between text-[10px] tabular-nums text-tertiary" aria-hidden="true">
            <span>{fmt(displayPosition)}</span>
            <span>{fmt(playback.DurationMs)}</span>
          </div>
        </div>
      {/if}

      <div class="mt-1 flex items-center justify-center gap-1">
        <button
          type="button"
          aria-label="Previous track"
          disabled={!hasState || !paired || !!busyCmd}
          onclick={() => send('prev')}
          class="flex h-9 w-9 items-center justify-center rounded-full text-label transition hover:bg-altrow disabled:opacity-40 active:translate-y-[1px]"
        ><SkipBack size={15} aria-hidden="true" /></button>
        <button
          type="button"
          aria-label={isPlaying ? 'Pause' : 'Play'}
          disabled={!hasState || !paired || !!busyCmd}
          onclick={() => send(isPlaying ? 'pause' : 'play')}
          class="media-play-button flex h-10 w-10 items-center justify-center rounded-full transition hover:brightness-105 disabled:opacity-40 active:translate-y-[1px]"
        >
          {#if busyCmd}
            <span class="spinner border border-accent-text/40 border-t-accent-text" aria-hidden="true"></span>
          {:else if isPlaying}
            <Pause size={14} aria-hidden="true" />
          {:else}
            <Play size={14} aria-hidden="true" />
          {/if}
        </button>
        <button
          type="button"
          aria-label="Next track"
          disabled={!hasState || !paired || !!busyCmd}
          onclick={() => send('next')}
          class="flex h-9 w-9 items-center justify-center rounded-full text-label transition hover:bg-altrow disabled:opacity-40 active:translate-y-[1px]"
        ><SkipForward size={15} aria-hidden="true" /></button>
      </div>

      {#if hasState && !canCommand}
        <div class="mt-1 flex items-center justify-center gap-1.5 text-center">
          <span class="text-[10px] text-tertiary">View only</span>
          <span class="text-[10px] text-tertiary">·</span>
          <button
            type="button"
            onclick={enableControl}
            class="text-[10px] font-medium text-accent hover:underline"
          >Enable control from Mac</button>
        </div>
      {/if}
    </div>
  </section>
{/if}
