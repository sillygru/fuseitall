<!--
  SPDX-License-Identifier: AGPL-3.0-only

  Notifications pane: grouped by app, newest-first, with auto-synced app
  icons (96px PNG base64 cached per package). Flat grouped (sticky headers,
  no collapse) — future-proof, no hidden state. Timestamp is "2m ago" style.
  Native banner attribution is fixed via NSUserNotificationCenter so the
  desktop banner shows as FuseItAll, not Script Editor.
-->
<script lang="ts">
  import { fade } from 'svelte/transition';
  import type { NotifView } from '../backend';

  interface Props {
    items: NotifView[];
    paired: boolean;
    clearing: boolean;
    onDismiss: (id: string) => void;
    onClear: () => void;
  }

  let { items, paired, clearing, onDismiss, onClear }: Props = $props();

  function timeAgo(unix: number): string {
    if (!unix) return '';
    const secs = Math.max(0, Math.floor(Date.now() / 1000 - unix));
    if (secs < 60) return 'just now';
    const mins = Math.floor(secs / 60);
    if (mins < 60) return `${mins}m ago`;
    const hrs = Math.floor(mins / 60);
    if (hrs < 24) return `${hrs}h ago`;
    const days = Math.floor(hrs / 24);
    if (days === 1) return 'yesterday';
    if (days < 7) return `${days}d ago`;
    return new Date(unix * 1000).toLocaleDateString();
  }

  function initials(app: string): string {
    const t = app.trim();
    if (!t) return '•';
    const parts = t.split(/\s+/).filter(Boolean);
    if (parts.length >= 2) return (parts[0][0] + parts[1][0]).toUpperCase();
    return t.slice(0, 2).toUpperCase();
  }

  type Group = { key: string; label: string; rows: NotifView[] };
  let groups = $derived.by<Group[]>(() => {
    const map = new Map<string, Group>();
    for (const n of items) {
      const key = n.PackageName || n.App || 'Other';
      const label = n.App || n.PackageName || 'Other';
      let g = map.get(key);
      if (!g) {
        g = { key, label, rows: [] };
        map.set(key, g);
      }
      g.rows.push(n);
    }
    return [...map.values()];
  });
</script>

<section aria-label="Notifications" class="section px-1 py-2">
  <div class="flex items-center gap-2">
    <div class="min-w-0 flex-1">
      <h2 class="text-[15px] font-semibold text-label">Notifications</h2>
      <p class="mt-0.5 text-[12px] text-secondary">
        {#if items.length}{items.length} mirrored from your phone{#if groups.length > 1} · {groups.length} apps{/if}.{:else}Nothing mirrored yet.{/if}
        {#if !paired} Dismissals send on reconnect.{/if}
      </p>
    </div>
    {#if items.length}
      <button
        type="button"
        onclick={onClear}
        disabled={clearing}
        title="Clear all notifications"
        class="flex-none rounded-md px-2.5 py-1.5 text-[12px] font-medium text-label transition hover:bg-altrow active:translate-y-[1px] disabled:cursor-not-allowed disabled:opacity-50"
      >{clearing ? 'Clearing…' : 'Clear'}</button>
    {/if}
  </div>

  {#if items.length}
    <div class="mt-3 flex flex-col gap-4">
      {#each groups as g, gi (g.key)}
        <div class="anim-row py-1" style="--i: {Math.min(gi, 5)}">
          <div class="sticky top-0 z-10 -mx-1 flex items-center gap-2 bg-window px-1 py-1">
            <span class="min-w-0 flex-1 truncate text-[11px] font-semibold tracking-wide text-secondary uppercase">{g.label}</span>
            <span class="flex-none tabular-nums text-[11px] text-tertiary">{g.rows.length}</span>
          </div>
          <ul class="mt-1 flex flex-col gap-0.5">
            {#each g.rows as n, ni (n.ID)}
              <li
                out:fade={{ duration: 150 }}
                style="--i: {Math.min(ni, 7)}"
                data-menu="notification"
                data-notif-id={n.ID}
                data-notif-app={n.App || n.PackageName}
                data-notif-pkg={n.PackageName}
                data-notif-text={`${n.Title ? n.Title + ': ' : ''}${n.Text || ''}`.trim()}
                class="anim-row group flex items-start gap-3 rounded-lg px-2 py-2.5 transition hover:bg-altrow"
              >
                <div class="flex h-8 w-8 flex-none items-center justify-center overflow-hidden rounded-lg bg-altrow">
                  {#if n.IconB64}
                    <img src={"data:image/png;base64," + n.IconB64} alt="" class="h-8 w-8 object-cover" loading="lazy" />
                  {:else}
                    <span class="text-[11px] font-semibold text-secondary">{initials(n.App || n.PackageName)}</span>
                  {/if}
                </div>
                <div class="min-w-0 flex-1">
                  <div class="flex items-baseline gap-1.5">
                    {#if n.App}
                      <p class="truncate text-[11px] font-semibold text-secondary">{n.App}</p>
                    {:else if n.PackageName}
                      <p class="truncate text-[11px] font-semibold text-secondary">{n.PackageName}</p>
                    {/if}
                    {#if n.PostedUnix}
                      <span class="flex-none text-[11px] text-tertiary">· {timeAgo(n.PostedUnix)}</span>
                    {/if}
                  </div>
                  {#if n.Title}
                    <p class="mt-0.5 text-[12px] font-medium text-label">{n.Title}</p>
                  {/if}
                  {#if n.Text}
                    <p class="mt-0.5 line-clamp-3 text-[12px] leading-snug text-secondary">{n.Text}</p>
                  {/if}
                </div>
                <button
                  type="button"
                  onclick={() => onDismiss(n.ID)}
                  title="Dismiss notification"
                  aria-label={n.Title ? `Dismiss ${n.Title}` : 'Dismiss notification'}
                  class="flex-none rounded-md px-2 py-1 text-[12px] font-medium text-secondary opacity-0 transition group-hover:opacity-100 hover:bg-altrow hover:text-label focus:opacity-100 focus-visible:opacity-100 active:translate-y-[1px]"
                >Dismiss</button>
              </li>
            {/each}
          </ul>
        </div>
      {/each}
    </div>
  {:else}
    <p class="mt-3 px-1 py-2 text-[12px] leading-relaxed text-secondary">Phone notifications will appear here once the phone posts one. Make sure Notifications are enabled on the phone and the listener permission is granted.</p>
  {/if}
</section>
