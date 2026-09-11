<!--
  Copyright (C) 2026 FuseItAll contributors.

  This program is free software: you can redistribute it and/or modify
  it under the terms of the GNU Affero General Public License as published
  by the Free Software Foundation, version 3 of the License. See LICENSE
  for details.

<!--
  Native-style context menu (HIG Context Menus): small, relevant to the
  target, unavailable actions hidden (never dimmed), destructive action
  red and listed last. Classic frost material with solid fallback.
  Supports keyboard navigation (Arrow Up/Down, Enter, Escape).
  Dismisses on Escape, outside click, scroll, or resize.
-->
<script lang="ts">
  import { onMount, tick } from 'svelte';
  import { scale } from 'svelte/transition';

  export interface MenuItem {
    id: string;
    label: string;
    destructive?: boolean;
    separator?: boolean;
    hint?: string;
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
  let origin = $state('top left');
  let activeIndex = $state(-1);

  const actionableItems = $derived(
    items
      .map((item, idx) => ({ item, idx }))
      .filter(({ item }) => !item.separator),
  );

  onMount(() => {
    left = x;
    top = y;

    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.preventDefault();
        onClose();
        return;
      }

      if (!actionableItems.length) return;

      if (e.key === 'ArrowDown') {
        e.preventDefault();
        const currentPos = actionableItems.findIndex((a) => a.idx === activeIndex);
        const nextPos = currentPos < actionableItems.length - 1 ? currentPos + 1 : 0;
        activeIndex = actionableItems[nextPos].idx;
        focusItem(activeIndex);
      } else if (e.key === 'ArrowUp') {
        e.preventDefault();
        const currentPos = actionableItems.findIndex((a) => a.idx === activeIndex);
        const nextPos = currentPos > 0 ? currentPos - 1 : actionableItems.length - 1;
        activeIndex = actionableItems[nextPos].idx;
        focusItem(activeIndex);
      } else if (e.key === 'Enter' || e.key === ' ') {
        if (activeIndex >= 0 && activeIndex < items.length) {
          const item = items[activeIndex];
          if (item && !item.separator) {
            e.preventDefault();
            onPick(item.id);
          }
        }
      }
    };

    function focusItem(idx: number) {
      if (!el) return;
      const btn = el.querySelector<HTMLButtonElement>(`[data-item-idx="${idx}"]`);
      btn?.focus();
    }

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
      left = Math.max(6, Math.min(x, window.innerWidth - rect.width - 6));
      top = Math.max(6, Math.min(y, window.innerHeight - rect.height - 6));
      // Pop outward from the anchor corner: when clamping pushes the menu
      // away from the cursor, the origin follows the cursor side.
      origin = `${top >= y ? 'top' : 'bottom'} ${left >= x ? 'left' : 'right'}`;
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
  tabindex="-1"
  style="left: {left}px; top: {top}px; transform-origin: {origin};"
  transition:scale={{ duration: 130, start: 0.95, opacity: 0 }}
  class="fixed z-50 min-w-[190px] rounded-xl bg-control/95 py-1.5 shadow-[0_12px_32px_rgba(0,0,0,0.26),0_0_0_1px_var(--separator)] backdrop-blur-md"
>
  {#each items as item, i (item.id)}
    {#if item.separator}
      <div class="mx-2 my-1 h-px bg-separator/60" role="separator"></div>
    {:else}
      <button
        type="button"
        role="menuitem"
        data-item-idx={i}
        onclick={() => onPick(item.id)}
        onmouseenter={() => (activeIndex = i)}
        tabindex={activeIndex === i ? 0 : -1}
        class="flex w-[calc(100%-8px)] mx-1 items-center justify-between rounded-md px-2.5 py-1 text-left text-[13px] font-normal transition-colors duration-75 {activeIndex === i ? 'bg-hover' : 'hover:bg-hover'} focus:bg-hover focus:outline-none {item.destructive ? 'text-bad' : 'text-label'}"
      >
        <span class="truncate">{item.label}</span>
        {#if item.hint}
          <span class="ml-3 flex-none text-[11px] tabular-nums text-tertiary">{item.hint}</span>
        {/if}
      </button>
    {/if}
  {/each}
</div>
