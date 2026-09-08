<!--
  Copyright (C) 2026 FuseItAll contributors.

  This program is free software: you can redistribute it and/or modify
  it under the terms of the GNU Affero General Public License as published
  by the Free Software Foundation, version 3 of the License. See LICENSE
  for details.

  Window drag strip (HIG Windows, macOS): traffic-light clearance only.
  Titles live in the content header cards, so the strip carries no text
  and no actions. Fully opaque, no translucency beyond classic frost.
-->
<script lang="ts">
  interface Props {
    title?: string;
    primaryLabel?: string | null;
    primaryBusyLabel?: string;
    primaryBusy?: boolean;
    primaryDisabled?: boolean;
    primaryHint?: string;
    onPrimary?: () => void;
  }

  let { title: _title, primaryLabel: _primaryLabel, primaryBusyLabel: _busy, primaryBusy: _pb, primaryDisabled: _pd, primaryHint: _hint, onPrimary: _onPrimary }: Props = $props();
</script>

<div class="frost-bar h-[28px] flex-none border-b border-separator pl-20 pr-3" data-tauri-drag-region aria-hidden="true"></div>
