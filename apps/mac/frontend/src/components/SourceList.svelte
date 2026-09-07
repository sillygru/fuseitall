<!--
  Copyright (C) 2026 FuseItAll contributors.

  This program is free software: you can redistribute it and/or modify
  it under the terms of the GNU Affero General Public License as published
  by the Free Software Foundation, version 3 of the License. See LICENSE
  for details.

  Source list (HIG Sidebars, macOS): one hierarchy level, succinct group
  label, persistent selection. Selected row uses the accent (which follows
  the user's system accent); icons come from the single Lucide family and
  follow the accent, state dots are fixed colors used sparingly for meaning
  only.
-->
<script lang="ts">
  import type { Component } from 'svelte';
  import { Bell, Clipboard, Settings, Smartphone } from '@lucide/svelte';

  export interface SourceItem {
    id: string;
    label: string;
    detail: string;
    state: 'ok' | 'warn' | 'bad' | 'none';
    icon?: 'phone' | 'bell' | 'clipboard' | 'settings';
    badge?: number;
  }

  interface Props {
    group: string;
    items: SourceItem[];
    selectedId: string;
    onSelect: (id: string) => void;
  }

  let { group, items, selectedId, onSelect }: Props = $props();

  const dot: Record<SourceItem['state'], string> = {
    ok: 'bg-ok',
    warn: 'bg-warn',
    bad: 'bg-bad',
    none: 'bg-tertiary',
  };

  const icons: Record<NonNullable<SourceItem['icon']>, Component> = {
    phone: Smartphone,
    bell: Bell,
    clipboard: Clipboard,
    settings: Settings,
  };
</script>

<div class="flex min-h-0 flex-1 flex-col px-2 py-2">
  <p class="px-2 pb-1 text-[11px] font-semibold text-secondary">{group}</p>
  <ul class="flex flex-col gap-px">
    {#each items as item (item.id)}
      {@const Icon = icons[item.icon ?? 'phone']}
      <li>
        <button
          type="button"
          data-source-id={item.id}
          onclick={() => onSelect(item.id)}
          aria-current={item.id === selectedId}
          class="flex w-full items-center gap-2.5 rounded-lg px-2.5 py-2 text-left transition focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-focus active:translate-y-[1px] {item.id === selectedId
            ? 'bg-accent'
            : 'hover:bg-altrow'}"
        >
          <Icon
            size={17}
            strokeWidth={2}
            aria-hidden="true"
            class="flex-none {item.id === selectedId ? 'text-accent-text' : 'text-accent'}"
          />
          <span class="min-w-0 flex-1">
            <span class="block truncate text-[13px] font-medium {item.id === selectedId ? 'text-accent-text' : 'text-label'}">{item.label}</span>
            {#if item.detail}
              <span class="block truncate text-[11px] {item.id === selectedId ? 'text-accent-text opacity-80' : 'text-secondary'}">{item.detail}</span>
            {/if}
          </span>
          {#if item.badge}
            <span
              class="flex h-[18px] min-w-[18px] flex-none items-center justify-center rounded-full bg-bad px-1 text-[11px] font-semibold text-destructive-text"
              aria-label="{item.badge} unread"
            >{item.badge > 99 ? '99+' : item.badge}</span>
          {:else}
            <span class="h-1.5 w-1.5 flex-none rounded-full {dot[item.state]}" aria-hidden="true"></span>
          {/if}
        </button>
      </li>
    {/each}
  </ul>
</div>
