<!--
  Copyright (C) 2026 FuseItAll contributors.

  This program is free software: you can redistribute it and/or modify
  it under the terms of the GNU Affero General Public License as published
  by the Free Software Foundation, version 3 of the License. See LICENSE
  for details.

  Content header: plain toolbar-style title row (no box, no fill, no
  icon well). Title names the content, subtitle carries one short fact,
  optional trailing actions. Glyph is a plain 17px Lucide mark in
  secondary text; the legacy `tint` prop is accepted but ignored.
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

<div class="flex items-center gap-2.5 px-1 py-1">
  <span class="flex-none text-secondary" aria-hidden="true">
    <Icon size={17} strokeWidth={2} />
  </span>
  <div class="min-w-0 flex-1">
    <h2 class="truncate text-[15px] font-semibold text-label">{title}</h2>
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
