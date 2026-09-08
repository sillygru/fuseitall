<!--
  Copyright (C) 2026 FuseItAll contributors.

  This program is free software: you can redistribute it and/or modify
  it under the terms of the GNU Affero General Public License as published
  by the Free Software Foundation, version 3 of the License. See LICENSE
  for details.

  Placeholder pane: dummy target for not yet implemented sidebar rows.
-->
<script lang="ts">
  interface Props {
    title: string;
    body: string;
  }

  let { title, body }: Props = $props();
</script>

<section aria-label={title} class="card p-4">
  <h2 class="text-[13px] font-semibold text-label">{title}</h2>
  <p class="mt-1 text-[12px] text-secondary">{body}</p>
  <p class="mt-3 rounded-lg bg-altrow p-3 text-[12px] text-secondary">Not yet available.</p>
</section>
