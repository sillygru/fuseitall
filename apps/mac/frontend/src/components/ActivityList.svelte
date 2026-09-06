<!--
  Copyright (C) 2026 FuseItAll contributors.

  This program is free software: you can redistribute it and/or modify
  it under the terms of the GNU Affero General Public License as published
  by the Free Software Foundation, version 3 of the License. See LICENSE
  for details.

  Activity timeline: closed-world human sentences with a quiet dot rail
  instead of the old grid table. Raw slog lines never reach the window;
  failures stay greppable in the backend buffer (ADR 0004).
-->
<script lang="ts">
  import { Activity } from '@lucide/svelte';
  import type { ActivityItem } from '../activity';

  interface Props {
    items: ActivityItem[];
    emptyHint: string;
  }

  let { items, emptyHint }: Props = $props();

  const dot: Record<ActivityItem['kind'], string> = {
    ok: 'bg-ok',
    lost: 'bg-bad',
    warn: 'bg-warn',
    info: 'bg-tertiary',
  };
</script>

<section aria-label="Activity" class="card flex min-h-0 flex-1 flex-col overflow-hidden">
  <div class="flex items-center gap-2 border-b border-separator px-4 py-2.5">
    <Activity size={14} strokeWidth={2} class="text-tertiary" aria-hidden="true" />
    <h2 class="text-[13px] font-semibold text-label">Activity</h2>
  </div>
  {#if items.length}
    <ul class="min-h-[120px] flex-1 overflow-y-auto px-4 py-1 text-[12px] leading-relaxed">
      {#each items as item, i (i)}
        <li data-copy={item.text} class="flex items-start gap-2.5 border-b border-separator/60 py-2 text-secondary last:border-b-0">
          <span class="mt-1.5 h-1.5 w-1.5 flex-none rounded-full {dot[item.kind]}" aria-hidden="true"></span>
          <span class="min-w-0 break-words">{item.text}</span>
        </li>
      {/each}
    </ul>
  {:else}
    <p class="px-4 py-4 text-[13px] text-secondary">{emptyHint}</p>
  {/if}
</section>
