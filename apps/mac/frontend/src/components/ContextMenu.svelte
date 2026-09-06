<!--
  Copyright (C) 2026 FuseItAll contributors.

  This program is free software: you can redistribute it and/or modify
  it under the terms of the GNU Affero General Public License as published
  by the Free Software Foundation, version 3 of the License. See LICENSE
  for details.

  Native-style context menu (HIG Context Menus): small, relevant to the
  target, unavailable actions hidden (never dimmed), destructive action
  red and listed last. Fully opaque. Dismisses on Escape, outside click,
  scroll, or resize.
-->
<script lang="ts">
  import { onMount, tick } from 'svelte';

  export interface MenuItem {
    id: string;
    label: string;
    destructive?: boolean;
    separator?: boolean;
  }

  interface Props {
    x: number;
    y: number;
    items: MenuItem[];
    onPick: (id: string) => void;
    onClose: () => void;
  }

  let { x, y, items, onPick, onClose }: Props = $props();

  let el = $state<HTMLElement | null>(null);
  let left = $state(0);
  let top = $state(0);

  onMount(() => {
    left = x;
    top = y;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose();
    };
    const onPointer = (e: PointerEvent) => {
      if (el && !el.contains(e.target as Node)) onClose();
    };
    const onScroll = () => onClose();
    window.addEventListener('keydown', onKey);
    window.addEventListener('pointerdown', onPointer, true);
    window.addEventListener('scroll', onScroll, true);
    window.addEventListener('resize', onClose);
    void tick().then(() => {
      if (!el) return;
      const rect = el.getBoundingClientRect();
      left = Math.max(4, Math.min(x, window.innerWidth - rect.width - 4));
      top = Math.max(4, Math.min(y, window.innerHeight - rect.height - 4));
    });
    return () => {
      window.removeEventListener('keydown', onKey);
      window.removeEventListener('pointerdown', onPointer, true);
      window.removeEventListener('scroll', onScroll, true);
      window.removeEventListener('resize', onClose);
    };
  });
</script>

<div
  bind:this={el}
  role="menu"
  style="left: {left}px; top: {top}px;"
  class="fixed z-50 min-w-[180px] rounded-md border border-separator bg-control py-1 shadow-[0_8px_24px_rgba(0,0,0,0.28)]"
>
  {#each items as item (item.id)}
    {#if item.separator}
      <div class="mx-2 my-1 border-t border-separator" role="separator"></div>
    {:else}
      <button
        type="button"
        role="menuitem"
        onclick={() => onPick(item.id)}
        class="block w-full truncate px-3 py-1 text-left text-[13px] transition hover:bg-altrow focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-focus {item.destructive
          ? 'text-bad'
          : 'text-label'}"
      >
        {item.label}
      </button>
    {/if}
  {/each}
</div>
