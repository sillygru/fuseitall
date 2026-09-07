<!--
  Copyright (C) 2026 FuseItAll contributors.

  This program is free software: you can redistribute it and/or modify
  it under the terms of the GNU Affero General Public License as published
  by the Free Software Foundation, version 3 of the License. See LICENSE
  for details.

  Notifications pane: mirrored phone notifications, newest first. Rows show
  the posting app plus title and body; dismissing one syncs back to the
  phone when paired (queued otherwise).
-->
<script lang="ts">
  import type { NotifView } from '../backend';

  interface Props {
    items: NotifView[];
    paired: boolean;
    clearing: boolean;
    onDismiss: (id: string) => void;
    onClear: () => void;
  }

  let { items, paired, clearing, onDismiss, onClear }: Props = $props();
</script>

<section aria-label="Notifications" class="card p-4">
  <div class="flex items-center gap-2">
    <div class="min-w-0 flex-1">
      <h2 class="text-[13px] font-semibold text-label">Notifications</h2>
      <p class="mt-0.5 text-[11px] text-secondary">
        {#if items.length}{items.length} mirrored from your phone.{:else}Nothing mirrored yet.{/if}
        {#if !paired} Dismissals send on reconnect.{/if}
      </p>
    </div>
    {#if items.length}
      <button
        type="button"
        onclick={onClear}
        disabled={clearing}
        title="Clear all notifications"
        class="flex-none rounded-md border border-separator bg-window px-2.5 py-1.5 text-[12px] font-medium text-label transition hover:border-focus focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus active:translate-y-[1px] disabled:cursor-not-allowed disabled:opacity-50"
      >{clearing ? 'Clearing…' : 'Clear'}</button>
    {/if}
  </div>

  {#if items.length}
    <ul class="mt-3 flex flex-col gap-2">
      {#each items as n (n.ID)}
        <li class="rounded-lg border border-separator bg-window p-2.5">
          <div class="flex items-start gap-2">
            <div class="min-w-0 flex-1">
              {#if n.App}
                <p class="truncate text-[11px] font-semibold text-secondary">{n.App}</p>
              {/if}
              {#if n.Title}
                <p class="mt-0.5 text-[12px] font-medium text-label">{n.Title}</p>
              {/if}
              {#if n.Text}
                <p class="mt-0.5 line-clamp-3 text-[12px] text-secondary">{n.Text}</p>
              {/if}
            </div>
            <button
              type="button"
              onclick={() => onDismiss(n.ID)}
              title="Dismiss notification"
              aria-label={n.Title ? `Dismiss ${n.Title}` : 'Dismiss notification'}
              class="flex-none rounded-md px-2 py-1 text-[12px] font-medium text-secondary transition hover:bg-altrow hover:text-label focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus active:translate-y-[1px]"
            >Dismiss</button>
          </div>
        </li>
      {/each}
    </ul>
  {:else}
    <p class="mt-3 rounded-lg bg-altrow p-3 text-[12px] text-secondary">Phone notifications will appear here once the phone posts one.</p>
  {/if}
</section>
