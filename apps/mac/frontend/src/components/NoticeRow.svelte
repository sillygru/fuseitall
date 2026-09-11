<!--
  SPDX-License-Identifier: AGPL-3.0-only

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

<div role="alert" class="notice anim-row flex items-start gap-2.5 px-4 py-2.5">
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
