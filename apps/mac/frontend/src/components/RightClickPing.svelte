<!--
  Copyright (C) 2026 FuseItAll contributors.

  This program is free software: you can redistribute it and/or modify
  it under the terms of the GNU Affero General Public License as published
  by the Free Software Foundation, version 3 of the License. See LICENSE
  for details.

  Right-click cursor micro-ping (HIG micro-motion): provides instant,
  tactile visual feedback at the cursor location when right-clicking.
  GPU-accelerated transform/opacity only; auto-dismisses on animation end.
-->
<script lang="ts">
  import { onMount } from 'svelte';

  interface Props {
    x: number;
    y: number;
    onDone: () => void;
  }

  let { x, y, onDone }: Props = $props();

  onMount(() => {
    const timer = setTimeout(onDone, 300);
    return () => clearTimeout(timer);
  });
</script>

<div
  class="anim-click-ping"
  style="left: {x}px; top: {y}px;"
  onanimationend={onDone}
  aria-hidden="true"
></div>
