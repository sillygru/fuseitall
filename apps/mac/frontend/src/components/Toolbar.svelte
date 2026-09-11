<!--
  SPDX-License-Identifier: AGPL-3.0-only

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

<div class="frost-bar h-[28px] flex-none pl-20 pr-3" data-tauri-drag-region aria-hidden="true"></div>
