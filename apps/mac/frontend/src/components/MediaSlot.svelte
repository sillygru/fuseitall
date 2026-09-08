<!--
  Copyright (C) 2026 FuseItAll contributors.

  This program is free software: you can redistribute it and/or modify
  it under the terms of the GNU Affero General Public License as published
  by the Free Software Foundation, version 3 of the License. See LICENSE
  for details.

  Sidebar media slot: quick controls plus an idle player placeholder.
  Never shows fake tracks; controls stay disabled until audio lands.
  Layout splits to match the reference window: controls above the nav,
  player pinned above Settings.
-->
<script lang="ts">
  import { Camera, ChevronDown, Moon, Music, Play, SkipBack, SkipForward } from '@lucide/svelte';

  interface Props {
    layout?: 'controls' | 'player' | 'both';
  }

  let { layout = 'both' }: Props = $props();
  let showControls = $derived(layout === 'controls' || layout === 'both');
  let showPlayer = $derived(layout === 'player' || layout === 'both');
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
    <div class="flex gap-2 px-2.5">
      <button
        type="button"
        disabled
        aria-disabled="true"
        title="Night mode is not available yet"
        class="flex h-11 flex-1 items-center justify-center rounded-xl border border-separator bg-altrow text-secondary opacity-60"
      ><Moon size={17} aria-hidden="true" /></button>
      <button
        type="button"
        disabled
        aria-disabled="true"
        title="Camera shortcut is not available yet"
        class="relative flex h-11 flex-1 items-center justify-center rounded-xl border border-separator bg-altrow text-secondary opacity-60"
      >
        <Camera size={17} aria-hidden="true" />
      </button>
    </div>
  </div>
{/if}

{#if showPlayer}
  <div class="px-2 pb-2 pt-2">
    <button type="button" disabled aria-disabled="true" class="flex w-full items-center gap-1 px-2.5 text-left text-[12px] text-secondary opacity-70">
      <Music size={13} aria-hidden="true" />
      <span class="flex-1 truncate">Not playing</span>
      <ChevronDown size={13} aria-hidden="true" />
    </button>
    <div class="mt-1.5 rounded-xl border border-separator bg-control px-2.5 py-2">
      <div class="flex items-center gap-2.5">
        <span class="flex h-10 w-10 flex-none items-center justify-center rounded-lg bg-altrow text-tertiary" aria-hidden="true">
          <Music size={17} />
        </span>
        <div class="min-w-0 flex-1">
          <p class="truncate text-[13px] font-medium text-label">Not playing</p>
          <p class="truncate text-[11px] text-tertiary">Nothing from the phone right now</p>
        </div>
      </div>
      <div class="mt-1.5 flex items-center justify-center gap-5 text-tertiary" aria-hidden="true">
        <SkipBack size={16} />
        <span class="flex h-8 w-8 items-center justify-center rounded-full bg-altrow"><Play size={15} /></span>
        <SkipForward size={16} />
      </div>
    </div>
  </div>
{/if}
