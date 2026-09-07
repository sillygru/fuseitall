<!--
  Copyright (C) 2026 FuseItAll contributors.

  This program is free software: you can redistribute it and/or modify
  it under the terms of the GNU Affero General Public License as published
  by the Free Software Foundation, version 3 of the License. See LICENSE
  for details.

  Window toolbar (HIG Toolbars, macOS): leading content title, single
  prominent trailing action. The action is optional: panes with nothing to
  do (device status) render no button at all. Text labels only; HIG permits
  text where a short label communicates more clearly than a symbol, so no
  icon dependency is needed. Busy state follows the HIG button activity
  pattern: label swap plus inline spinner.
-->
<script lang="ts">
  interface Props {
    title: string;
    primaryLabel: string | null;
    primaryBusyLabel: string;
    primaryBusy: boolean;
    primaryDisabled: boolean;
    primaryHint: string;
    onPrimary: () => void;
  }

  let { title, primaryLabel, primaryBusyLabel, primaryBusy, primaryDisabled, primaryHint, onPrimary }: Props = $props();
</script>

<div class="frost-bar flex h-[52px] flex-none items-center gap-3 pl-20 pr-3">
  <p class="truncate text-[13px] font-semibold text-label">{title}</p>
  {#if primaryLabel}
    <button
      type="button"
      onclick={onPrimary}
      disabled={primaryDisabled || primaryBusy}
      title={primaryHint}
      class="ml-auto inline-flex h-7 flex-none items-center gap-2 rounded-md bg-accent px-3 text-[13px] font-medium text-accent-text transition hover:brightness-95 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus active:translate-y-[1px] disabled:cursor-not-allowed disabled:opacity-50"
    >
      {#if primaryBusy}
        <span class="spinner" aria-hidden="true"></span>
        <span>{primaryBusyLabel}</span>
      {:else}
        <span>{primaryLabel}</span>
      {/if}
    </button>
  {/if}
</div>
