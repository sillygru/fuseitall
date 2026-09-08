<!--
  Copyright (C) 2026 FuseItAll contributors.

  This program is free software: you can redistribute it and/or modify
  it under the terms of the GNU Affero General Public License as published
  by the Free Software Foundation, version 3 of the License. See LICENSE
  for details.

  Contacts pane: directory shell. Real entries only; empty until the
  phone shares them.
-->
<script lang="ts">
  import { Users } from '@lucide/svelte';
  import ContentHeader from './ContentHeader.svelte';

  interface Props {
    paired: boolean;
    deviceLabel: string;
  }

  let { paired, deviceLabel }: Props = $props();
</script>

<div class="flex min-h-0 flex-1 flex-col gap-3">
  <ContentHeader
    title="Contacts"
    subtitle={deviceLabel ? `Phone contacts · ${deviceLabel}` : 'Phone contacts'}
    icon={Users}
    tint="bg-accent/15 text-accent"
  />
  <div class="card flex flex-col items-center px-6 py-12 text-center">
    <span class="flex h-11 w-11 items-center justify-center rounded-full bg-altrow text-secondary" aria-hidden="true">
      <Users size={20} />
    </span>
    <p class="mt-3 text-[13px] font-medium text-label">{paired ? 'No contacts yet' : 'Phone offline'}</p>
    <p class="mt-1 max-w-[36ch] text-[12px] leading-relaxed text-secondary">
      {paired
        ? 'Contacts from the phone will appear here once the phone shares them.'
        : 'Reconnect the phone to browse contacts here.'}
    </p>
  </div>
</div>
