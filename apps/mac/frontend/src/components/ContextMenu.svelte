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
  Buttery-smooth spring open and dismiss close animations.
  Dismisses on Escape, outside click, scroll, or resize with exit animation.
-->
<script lang="ts">
  import { onMount, tick } from 'svelte';

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
    items?: MenuItem[];
    onPick: (id: string) => void;
    onClose: () => void;
  }

  let { x, y, items = [], onPick, onClose }: Props = $props();

  let el = $state<HTMLElement | null>(null);

  function portal(node: HTMLElement) {
    if (typeof document !== 'undefined' && document.body) {
      document.body.appendChild(node);
    }
    return {
      destroy() {
        if (node.parentNode) {
          node.parentNode.removeChild(node);
        }
      },
    };
  }

  // Estimated geometry avoids a single-frame jump from (0, 0) before
  // measurement. Derived so prop updates flow through; the $effect below
  // clamps the live position on x/y change.
  const estimatedW = 200;
  let estimatedH = $derived(((items && items.length) || 1) * 30 + 16);
  let initialLeft = $derived(
    typeof window !== 'undefined' ? Math.max(6, Math.min(x, window.innerWidth - estimatedW - 6)) : x,
  );
  let initialTop = $derived(
    typeof window !== 'undefined' ? Math.max(6, Math.min(y, window.innerHeight - estimatedH - 6)) : y,
  );
  let initialOrigin = $derived(`${initialTop >= y ? 'top' : 'bottom'} ${initialLeft >= x ? 'left' : 'right'}`);

  let left = $state(0);
  let top = $state(0);
  let origin = $state('top left');
  let seeded = false;

  // Seed the first frame from the estimated position before paint so the
  // menu never flashes at (0, 0). Afterwards clampPosition owns left/top.
  $effect.pre(() => {
    const il = initialLeft;
    const it = initialTop;
    const io = initialOrigin;
    if (!seeded) {
      left = il;
      top = it;
      origin = io;
      seeded = true;
    }
  });
  let activeIndex = $state(-1);
  let closing = $state(false);

  let closeTimer: ReturnType<typeof setTimeout> | null = null;

  const actionableItems = $derived(
    (items || [])
      .map((item, idx) => ({ item, idx }))
      .filter(({ item }) => !item.separator),
  );

  function clampPosition(curX: number, curY: number) {
    if (!el) {
      left = curX;
      top = curY;
      origin = `${top >= curY ? 'top' : 'bottom'} ${left >= curX ? 'left' : 'right'}`;
      return;
    }
    const rect = el.getBoundingClientRect();
    const clampedX = Math.max(6, Math.min(curX, window.innerWidth - rect.width - 6));
    const clampedY = Math.max(6, Math.min(curY, window.innerHeight - rect.height - 6));
    left = clampedX;
    top = clampedY;
    origin = `${clampedY >= curY ? 'top' : 'bottom'} ${clampedX >= curX ? 'left' : 'right'}`;
  }

  $effect(() => {
    const curX = x;
    const curY = y;
    closing = false;
    clampPosition(curX, curY);
    void tick().then(() => {
      clampPosition(curX, curY);
    });
  });

  function triggerClose(afterDone?: () => void) {
    if (closing) return;
    closing = true;

    if (closeTimer) {
      clearTimeout(closeTimer);
      closeTimer = null;
    }

    // 140ms matches the fi-menu-close animation duration
    closeTimer = setTimeout(() => {
      closeTimer = null;
      afterDone?.();
      onClose();
    }, 140);
  }

  function pickItem(id: string) {
    if (closing) return;
    triggerClose(() => onPick(id));
  }

  onMount(() => {
    const onKey = (e: KeyboardEvent) => {
      if (closing) return;

      if (e.key === 'Escape') {
        e.preventDefault();
        triggerClose();
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
            pickItem(item.id);
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
      if (closing) return;
      if (el && !el.contains(e.target as Node)) {
        triggerClose();
      }
    };
    const onScroll = () => {
      if (!closing) triggerClose();
    };

    window.addEventListener('keydown', onKey);
    window.addEventListener('pointerdown', onPointer, true);
    window.addEventListener('scroll', onScroll, true);
    window.addEventListener('resize', onScroll);

    return () => {
      if (closeTimer) {
        clearTimeout(closeTimer);
        closeTimer = null;
      }
      window.removeEventListener('keydown', onKey);
      window.removeEventListener('pointerdown', onPointer, true);
      window.removeEventListener('scroll', onScroll, true);
      window.removeEventListener('resize', onScroll);
    };
  });
</script>

<div
  use:portal
  bind:this={el}
  role="menu"
  tabindex="-1"
  style="left: {left}px; top: {top}px; --origin: {origin}; transform-origin: {origin};"
  class="fixed z-[100] min-w-[190px] rounded-xl bg-control/95 py-1.5 shadow-[0_16px_40px_rgba(0,0,0,0.32)] border-0 backdrop-blur-md select-none {closing ? 'anim-menu-close' : 'anim-menu-open'}"
>
  {#each items as item, i (item.id)}
    {#if item.separator}
      <div class="mx-2 my-1 h-px bg-separator/60" role="separator"></div>
    {:else}
      <button
        type="button"
        role="menuitem"
        data-item-idx={i}
        onclick={() => pickItem(item.id)}
        onmouseenter={() => (activeIndex = i)}
        tabindex={activeIndex === i ? 0 : -1}
        class="flex w-[calc(100%-8px)] mx-1 items-center justify-between rounded-md px-2.5 py-1 text-left text-[13px] font-normal transition-colors duration-75 {activeIndex === i ? 'bg-hover' : 'hover:bg-hover'} focus:bg-hover {item.destructive ? 'text-bad' : 'text-label'}"
      >
        <span class="truncate">{item.label}</span>
        {#if item.hint}
          <span class="ml-3 flex-none text-[11px] tabular-nums text-tertiary">{item.hint}</span>
        {/if}
      </button>
    {/if}
  {/each}
</div>
