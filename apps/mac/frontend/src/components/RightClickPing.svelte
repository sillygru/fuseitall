<!--
  SPDX-License-Identifier: AGPL-3.0-only

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
