<!--
  SPDX-License-Identifier: AGPL-3.0-only

  Placeholder pane: dummy target for not yet implemented sidebar rows.
-->
<script lang="ts">
  interface Props {
    title: string;
    body: string;
  }

  let { title, body }: Props = $props();
</script>

<section aria-label={title} class="section px-1 py-2">
  <h2 class="text-[15px] font-semibold text-label">{title}</h2>
  <p class="mt-1 text-[12px] text-secondary">{body}</p>
  <p class="mt-3 border-t border-separator px-1 py-2 text-[12px] text-tertiary">Not yet available.</p>
</section>
