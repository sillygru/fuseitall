<!--
  SPDX-License-Identifier: AGPL-3.0-only

  Content header: integrated toolbar-style title row. It stays visually
  attached to the native pane rather than reading as a floating card.
-->
<script lang="ts">
  import type { Component } from 'svelte';
  import type { Snippet } from 'svelte';

  interface Props {
    title: string;
    subtitle: string;
    icon: Component;
    tint?: string;
    actions?: Snippet;
  }

  let { title, subtitle, icon: Icon, tint: _tint = '', actions }: Props = $props();
</script>

<div class="flex min-w-0 items-center gap-2.5">
  <span class="flex-none text-secondary" aria-hidden="true">
    <Icon size={17} strokeWidth={2} />
  </span>
  <div class="min-w-0 flex-1">
    <h2 class="truncate text-[15px] font-semibold tracking-[-0.01em] text-label">{title}</h2>
    {#if subtitle}
      <p class="mt-0.5 truncate text-[12px] tabular-nums text-secondary">{subtitle}</p>
    {/if}
  </div>
  {#if actions}
    <div class="flex flex-none items-center gap-2">
      {@render actions()}
    </div>
  {/if}
</div>
