<!--
  SPDX-License-Identifier: AGPL-3.0-only

  Device hero: plain phone header. Title plus a plain-language subtitle
  (never an address), a status pill, and detail rows separated by 1px
  hairlines. No box, no icon well: hierarchy comes from type + dividers.
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

<section aria-label={title} class="section px-1 py-4">
  <div class="flex items-center gap-2.5">
    <Smartphone size={17} strokeWidth={2} class="flex-none text-secondary" aria-hidden="true" />
    <div class="min-w-0 flex-1">
      <h2 class="truncate text-[20px] font-semibold tracking-[-0.03em] text-label">{title}</h2>
      <p class="mt-0.5 truncate text-[12px] text-secondary">{subtitle}</p>
    </div>
    <StatusPill kind={statusKind} label={statusLabel} />
  </div>
  {#if rows.length}
    <dl class="mt-5 border-t border-separator">
      {#each rows as row (row.label)}
        <div class="flex items-baseline justify-between gap-4 border-b border-separator py-2.5">
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
