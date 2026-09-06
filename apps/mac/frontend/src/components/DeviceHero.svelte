<!--
  Copyright (C) 2026 FuseItAll contributors.

  This program is free software: you can redistribute it and/or modify
  it under the terms of the GNU Affero General Public License as published
  by the Free Software Foundation, version 3 of the License. See LICENSE
  for details.

  Device hero: the content header card. Tinted icon well plus title and a
  plain-language subtitle (never an address), a status pill, and a short
  list of friendly detail rows (link type, last reply). Machine data such
  as IPs stays out; it lives under Advanced in the pair card.
-->
<script lang="ts">
  import { Smartphone } from '@lucide/svelte';
  import StatusPill from './StatusPill.svelte';

  interface DetailRow {
    label: string;
    value: string;
  }

  interface Props {
    title: string;
    subtitle: string;
    statusKind: 'ok' | 'warn' | 'bad' | 'neutral';
    statusLabel: string;
    rows: DetailRow[];
    note: string;
  }

  let { title, subtitle, statusKind, statusLabel, rows, note }: Props = $props();
</script>

<section aria-label={title} class="card px-4 py-4">
  <div class="flex items-center gap-3">
    <span class="icon-well" aria-hidden="true">
      <Smartphone size={22} strokeWidth={2} />
    </span>
    <div class="min-w-0 flex-1">
      <h2 class="truncate text-[15px] font-semibold text-label">{title}</h2>
      <p class="mt-0.5 truncate text-[12px] text-secondary">{subtitle}</p>
    </div>
    <StatusPill kind={statusKind} label={statusLabel} />
  </div>
  {#if rows.length}
    <dl class="mt-3">
      {#each rows as row (row.label)}
        <div class="flex items-baseline justify-between gap-4 border-t border-separator py-1.5">
          <dt class="flex-none text-[13px] text-secondary">{row.label}</dt>
          <dd class="min-w-0 truncate text-right text-[13px] text-label">{row.value}</dd>
        </div>
      {/each}
    </dl>
  {/if}
  {#if note}
    <p class="mt-2 text-[12px] text-tertiary" aria-live="polite">{note}</p>
  {/if}
</section>
