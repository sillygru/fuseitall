<!--
  Copyright (C) 2026 FuseItAll contributors.

  This program is free software: you can redistribute it and/or modify
  it under the terms of the GNU Affero General Public License as published
  by the Free Software Foundation, version 3 of the License. See LICENSE
  for details.

  Mirror pane: viewport shell for screen sharing. Empty until the
  phone exposes a stream.
-->
<script lang="ts">
  import { MonitorSmartphone } from '@lucide/svelte';
  import ContentHeader from './ContentHeader.svelte';

  interface Props {
    paired: boolean;
    deviceLabel: string;
  }

  let { paired, deviceLabel }: Props = $props();
</script>

<div class="flex min-h-0 flex-1 flex-col gap-3">
  <ContentHeader
    title="Mirror"
    subtitle={deviceLabel ? `Phone screen · ${deviceLabel}` : 'Phone screen'}
    icon={MonitorSmartphone}
  />
  <div class="flex flex-col items-center px-6 py-16 text-center">
    <MonitorSmartphone size={22} class="text-tertiary" aria-hidden="true" />
    <p class="mt-3 text-[13px] font-medium text-label">{paired ? 'Mirror is not available yet' : 'Phone offline'}</p>
    <p class="mt-1 max-w-[38ch] text-[12px] leading-relaxed text-secondary">
      {paired
        ? 'The live phone screen will appear here once mirroring lands.'
        : 'Reconnect the phone, then open Mirror to see the phone screen here.'}
    </p>
  </div>
</div>
