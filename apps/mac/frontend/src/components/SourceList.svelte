<!--
  SPDX-License-Identifier: AGPL-3.0-only

  Source list (HIG Sidebars, macOS): one hierarchy level, succinct group
  label, persistent selection. Selected row uses the accent (which follows
  the user's system accent); icons come from the single Lucide family and
  follow the accent, state dots are fixed colors used sparingly for meaning
  only.
-->
<script lang="ts">
  import type { Component } from 'svelte';
  import { Bell, Folder, Image, MessageCircle, MonitorSmartphone, Settings, Smartphone, Users } from '@lucide/svelte';

  export interface SourceItem {
    id: string;
    label: string;
    detail: string;
    state: 'ok' | 'warn' | 'bad' | 'none';
    icon?: 'phone' | 'bell' | 'clipboard' | 'settings' | 'folder' | 'image' | 'message' | 'mirror' | 'contacts';
    badge?: number;
  }

  interface Props {
    group: string;
    items: SourceItem[];
    selectedId: string;
    onSelect: (id: string) => void;
  }

  let { group, items, selectedId, onSelect }: Props = $props();

  const icons: Record<NonNullable<SourceItem['icon']>, Component> = {
    phone: Smartphone,
    bell: Bell,
    clipboard: Bell,
    settings: Settings,
    folder: Folder,
    image: Image,
    message: MessageCircle,
    mirror: MonitorSmartphone,
    contacts: Users,
  };
</script>

<div class="flex flex-none flex-col px-2 py-2">
  <p class="px-2.5 pb-1.5 text-[11px] font-semibold uppercase tracking-wide text-secondary">{group}</p>
  <ul class="flex flex-col gap-0.5">
    {#each items as item (item.id)}
      {@const Icon = icons[item.icon ?? 'phone']}
      <li>
        <button
          type="button"
          data-menu="nav"
          data-source-id={item.id}
          data-nav-id={item.id}
          onclick={() => onSelect(item.id)}
          aria-current={item.id === selectedId}
          class="flex w-full items-center gap-2.5 rounded-lg px-2.5 py-[7px] text-left transition active:translate-y-[1px] {item.id === selectedId ? 'bg-accent' : 'hover:bg-altrow'}"
        >
          <Icon
            size={17}
            strokeWidth={2}
            aria-hidden="true"
            class="flex-none {item.id === selectedId ? 'text-accent-text' : 'text-secondary'}"
          />
          <span class="min-w-0 flex-1 truncate text-[13px] font-medium {item.id === selectedId ? 'text-accent-text' : 'text-label'}">{item.label}</span>
          {#if item.badge}
            {#key item.badge}
              <span class="anim-badge">
                <span
                  class="flex h-[18px] min-w-[18px] flex-none items-center justify-center rounded-full bg-bad px-1 text-[11px] font-semibold text-destructive-text"
                  aria-label="{item.badge} unread"
                >{item.badge > 99 ? '99+' : item.badge}</span>
              </span>
            {/key}
          {/if}
        </button>
      </li>
    {/each}
  </ul>
</div>
