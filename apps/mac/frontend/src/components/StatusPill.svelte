<!--
  Copyright (C) 2026 FuseItAll contributors.

  This program is free software: you can redistribute it and/or modify
  it under the terms of the GNU Affero General Public License as published
  by the Free Software Foundation, version 3 of the License. See LICENSE
  for details.

  Status pill: a dot plus a short label on a tinted badge. Color never
  travels alone: the label always names the state (Online, Seen 5m ago).
-->
<script lang="ts">
  interface Props {
    kind: 'ok' | 'warn' | 'bad' | 'neutral';
    label: string;
  }

  let { kind, label }: Props = $props();
</script>

<span class="pill pill-{kind}">
  <span class="pill-dot" aria-hidden="true"></span>
  <span>{label}</span>
</span>
