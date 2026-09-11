<!--
  SPDX-License-Identifier: AGPL-3.0-only

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

<section aria-label="Activity" class="section flex min-h-0 flex-1 flex-col overflow-hidden border-t border-separator">
  <div class="flex items-center gap-2 px-1 py-2">
    <Activity size={14} strokeWidth={2} class="text-tertiary" aria-hidden="true" />
    <h2 class="text-[13px] font-semibold text-label">Activity</h2>
  </div>
  {#if items.length}
    <ul class="min-h-[120px] flex-1 divide-y divide-separator overflow-y-auto px-1 py-1 text-[12px] leading-relaxed">
      {#each items as item, i (i)}
        <li data-copy={item.text} class="flex items-start gap-2.5 py-2 text-secondary">
          <span class="mt-1.5 h-1.5 w-1.5 flex-none rounded-full {dot[item.kind]}" aria-hidden="true"></span>
          <span class="min-w-0 break-words">{item.text}</span>
        </li>
      {/each}
    </ul>
  {:else}
    <p class="px-1 py-3 text-[13px] text-secondary">{emptyHint}</p>
  {/if}
</section>
