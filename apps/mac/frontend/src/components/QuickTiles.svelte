<!--
  SPDX-License-Identifier: AGPL-3.0-only

  Quick action tiles: large tactile buttons for the actions that are real
  right now (Reconnect while remembered). Tiles are built by the caller,
  so nothing disabled or coming-soon ever renders. Presence refreshes on
  its own, so there is no manual ping tile.
-->
<script lang="ts">
  import type { Component } from 'svelte';
  import { RefreshCw } from '@lucide/svelte';

  export interface Tile {
    id: 'reconnect';
    label: string;
    sub: string;
    busyLabel: string;
    busy: boolean;
    disabled: boolean;
    hint: string;
  }

  interface Props {
    tiles: Tile[];
    onPick: (id: Tile['id']) => void;
  }

  let { tiles, onPick }: Props = $props();

  const icons: Record<Tile['id'], Component> = {
    reconnect: RefreshCw,
  };
</script>

{#if tiles.length}
  <section aria-label="Quick actions" class="px-2 py-2">
    <p class="px-2 pb-1.5 text-[11px] font-semibold text-secondary">Quick Actions</p>
    <div class="grid grid-cols-1 gap-2">
      {#each tiles as tile (tile.id)}
        {@const Icon = icons[tile.id]}
        <button
          type="button"
          onclick={() => onPick(tile.id)}
          disabled={tile.disabled || tile.busy}
          title={tile.hint}
          class="tile"
        >
          <span class="tile-icon" aria-hidden="true">
            {#if tile.busy}
              <span class="spinner"></span>
            {:else}
              <Icon size={18} strokeWidth={2} />
            {/if}
          </span>
          <span class="min-w-0 text-left">
            <span class="block truncate text-[13px] font-medium text-label">
              {tile.busy ? tile.busyLabel : tile.label}
            </span>
            <span class="block truncate text-[11px] text-secondary">{tile.sub}</span>
          </span>
        </button>
      {/each}
    </div>
  </section>
{/if}
