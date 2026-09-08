<!--
  Copyright (C) 2026 FuseItAll contributors.

  This program is free software: you can redistribute it and/or modify
  it under the terms of the GNU Affero General Public License as published
  by the Free Software Foundation, version 3 of the License. See LICENSE
  for details.

  Messages pane: search field plus conversation list shell. Renders only
  real threads; with no backend yet it shows an honest empty state.
-->
<script lang="ts">
  import { MessageSquare, Search } from '@lucide/svelte';
  import ContentHeader from './ContentHeader.svelte';

  interface Props {
    paired: boolean;
    deviceLabel: string;
  }

  let { paired, deviceLabel }: Props = $props();
  let query = $state('');
</script>

<div class="flex min-h-0 flex-1 flex-col gap-3">
  <ContentHeader
    title="Messages"
    subtitle={deviceLabel ? `No conversations · ${deviceLabel}` : 'No conversations'}
    icon={MessageSquare}
    tint="bg-accent/15 text-accent"
  />
  <div class="relative flex-none">
    <Search size={14} class="pointer-events-none absolute left-2.5 top-1/2 -translate-y-1/2 text-tertiary" aria-hidden="true" />
    <input
      bind:value={query}
      placeholder="Search"
      aria-label="Search conversations"
      disabled={!paired}
      class="h-8 w-full rounded-lg border border-separator bg-altrow pl-8 pr-3 text-[13px] text-label placeholder:text-tertiary focus:outline-none focus:ring-2 focus:ring-focus disabled:opacity-50"
    />
  </div>
  <div class="card flex flex-col items-center px-6 py-12 text-center">
    <span class="flex h-11 w-11 items-center justify-center rounded-full bg-accent/15 text-accent" aria-hidden="true">
      <MessageSquare size={20} />
    </span>
    <p class="mt-3 text-[13px] font-medium text-label">{paired ? 'No conversations yet' : 'Phone offline'}</p>
    <p class="mt-1 max-w-[36ch] text-[12px] leading-relaxed text-secondary">
      {paired
        ? 'Message threads from the phone will appear here once the phone shares them.'
        : 'Reconnect the phone to see message threads here.'}
    </p>
  </div>
</div>
