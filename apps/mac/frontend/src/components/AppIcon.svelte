<!-- SPDX-License-Identifier: AGPL-3.0-only -->
<script lang="ts">
  interface Props {
    size?: number;
    class?: string;
    label?: string;
  }
  let { size = 24, class: klass = '', label = 'FuseItAll' }: Props = $props();
</script>

<svg
  xmlns="http://www.w3.org/2000/svg"
  viewBox="0 0 120 120"
  width={size}
  height={size}
  role="img"
  aria-label={label}
  class={klass}
  shape-rendering="geometricPrecision"
>
  <title>{label}</title>
  <defs>
    <clipPath id="c1-{size}"><rect x="43" y="43" width="52" height="52" rx="10" /></clipPath>
    <filter id="b1-{size}" x="-20%" y="-20%" width="140%" height="140%"><feGaussianBlur stdDeviation="3.2" /></filter>
  </defs>
  <!-- Outline square (back) — visually equal 27,27 (top visual = bottom visual) -->
  <rect x="27" y="27" width="52" height="52" rx="10" fill="none" stroke="currentColor" stroke-width="2.8" />
  <!-- Soft ghost — blurred outline clipped to filled square bounds -->
  <g clip-path="url(#c1-{size})" opacity="0.9"><g filter="url(#b1-{size})" opacity="0.55"><rect x="27" y="27" width="52" height="52" rx="10" fill="none" stroke="currentColor" stroke-width="3.5" /></g></g>
  <!-- Filled square (front) — visually equal 43,43 exact 0.88 opacity -->
  <rect x="43" y="43" width="52" height="52" rx="10" fill="currentColor" opacity="0.88" />
  <!-- Hairline -->
  <rect x="43" y="43" width="52" height="52" rx="10" fill="none" stroke="white" stroke-width="1" opacity="0.12" />
</svg>
