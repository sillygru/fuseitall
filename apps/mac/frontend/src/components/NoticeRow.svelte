<!--
  Copyright (C) 2026 FuseItAll contributors.

  This program is free software: you can redistribute it and/or modify
  it under the terms of the GNU Affero General Public License as published
  by the Free Software Foundation, version 3 of the License. See LICENSE
  for details.

  Inline notice banner (HIG Alerts: no modal alert for information; the
  Mail-style inline indicator instead). Renders the canonical core
  version-gate message verbatim with a plain verb-led follow-up.
-->
<script lang="ts">
  import { TriangleAlert } from '@lucide/svelte';

  interface Props {
    kind: 'self' | 'peer';
    message: string;
    requiredVersion?: string;
    currentVersion?: string;
  }

  let { kind, message, requiredVersion = '', currentVersion = '' }: Props = $props();

  let detail = $derived(
    requiredVersion || currentVersion
      ? `Requires ${requiredVersion || 'newer'}${currentVersion ? `, current ${currentVersion}` : ''}`
      : '',
  );
</script>

<div role="alert" class="notice notice-{kind} anim-row mx-4 mt-3 flex items-start gap-2.5 px-3.5 py-3">
  <TriangleAlert size={16} strokeWidth={2} class="mt-px flex-none" aria-hidden="true" />
  <div class="min-w-0">
    <p class="text-[13px] font-semibold text-label">
      {kind === 'self' ? 'This Mac needs an update' : 'Phone needs an update'}
    </p>
    <p class="mt-0.5 break-words text-[12px] text-secondary">{message}</p>
    {#if detail}
      <p class="mt-0.5 break-words text-[12px] text-secondary">{detail}</p>
    {/if}
  </div>
</div>
