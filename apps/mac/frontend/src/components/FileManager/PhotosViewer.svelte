<!--
  Copyright (C) 2026 FuseItAll contributors.

  This program is free software: you can redistribute it and/or modify
  it under the terms of the GNU Affero General Public License as published
  by the Free Software Foundation, version 3 of the License. See LICENSE
  for details.

  Photos — date-grouped grid for the Android photo library.
  HIG Read: primary window detail pane for photo browsing, following HIG
  Windows, Toolbars, Sidebars/Split Views, Color, Typography, Buttons,
  Progress, Context Menus. Classic frost, no Liquid Glass.
-->
<script lang="ts">
  import { onMount } from 'svelte';
  import { RefreshCw, Download, Trash2, X, Check } from '@lucide/svelte';
  import { Events } from '@wailsio/runtime';
  import type { PhotoEntryView, PhotoListResult, PhotoThumbResult, PhotoTransferView } from '../../backend';
  import { listPhonePhotos, requestPhotoThumb, requestPhonePhoto, deletePhonePhotos, getPhotoTransfers, cancelPhotoTransfer, isPhotosPermissionError } from '../../backend';
  import { Service } from '../../backend';

  interface UpdateNotice {
    Active: boolean;
    Self: boolean;
    Message: string;
    RequiredVersion: string;
    CurrentVersion: string;
    RequiredBuild: number;
  }

  interface Props { paired: boolean }
  let { paired }: Props = $props();

  let entries = $state<PhotoEntryView[]>([]);
  let thumbs = $state<Record<string, string>>({});
  let thumbFailed = $state<Set<string>>(new Set());
  let nextCursor = $state('');
  let loading = $state(false);
  let loadingMore = $state(false);
  let error = $state('');
  let lastResult = $state<PhotoListResult | null>(null);
  let info = $state('');
  let selected = $state<Set<string>>(new Set());
  let showDeleteConfirm = $state(false);
  let deleting = $state(false);
  let previewId = $state<string | null>(null);
  let previewB64 = $state('');
  let previewMime = $state('image/jpeg');
  let transfers = $state<PhotoTransferView[]>([]);
  let updateNotice = $state<UpdateNotice | null>(null);

  // Progressive batch size: 50 items per page for quick initial load
  const PAGE_LIMIT = 50;
  const CONCURRENT_THUMB_LIMIT = 3;

  let currentGen = 0;
  let prevPaired = false;
  let activeWorkers = 0;
  const pendingThumbQueue: string[] = [];
  const queuedThumbsSet = new Set<string>();

  let sentinelEl = $state<HTMLElement | null>(null);
  let sentinelObserver: IntersectionObserver | null = null;
  let tileObserver: IntersectionObserver | null = null;

  let isPermissionError = $derived(lastResult ? isPhotosPermissionError(lastResult, error) : false);
  let isUpdateRequired = $derived.by(() => {
    if (lastResult?.error_code === 'UPDATE_REQUIRED') return true;
    if (!updateNotice?.Active) return false;
    if (updateNotice.Self) return false;
    return (updateNotice.RequiredBuild ?? 0) >= 7;
  });

  type DateGroup = { label: string; items: PhotoEntryView[] };
  let groups = $derived.by<DateGroup[]>(() => {
    const map = new Map<string, PhotoEntryView[]>();
    for (const e of entries) {
      const d = new Date(e.taken_at);
      const key = Number.isNaN(d.getTime()) ? 'Unknown date' : d.toLocaleDateString(undefined, { year: 'numeric', month: 'long', day: 'numeric' });
      const list = map.get(key) ?? [];
      list.push(e);
      map.set(key, list);
    }
    return [...map.entries()].map(([label, items]) => ({ label, items }));
  });
  let selectedCount = $derived(selected.size);
  let previewEntry = $derived(previewId ? entries.find((e) => e.photo_id === previewId) ?? null : null);

  async function refreshUpdateNotice(): Promise<void> {
    try {
      const fn = (Service as unknown as Record<string, unknown>)['GetUpdateNotice'] as (() => Promise<UpdateNotice>) | undefined;
      if (typeof fn !== 'function') return;
      updateNotice = await fn();
    } catch { /* ignore */ }
  }

  function initTileObserver(): void {
    tileObserver?.disconnect();
    tileObserver = new IntersectionObserver(
      (obsEntries) => {
        for (const entry of obsEntries) {
          if (entry.isIntersecting) {
            const el = entry.target as HTMLElement;
            const photoId = el.dataset.photoId;
            if (photoId) {
              tileObserver?.unobserve(el);
              if (!thumbs[photoId] && !thumbFailed.has(photoId) && !queuedThumbsSet.has(photoId)) {
                enqueueThumb(photoId);
              }
            }
          }
        }
      },
      { rootMargin: '300px' }
    );
  }

  function enqueueThumb(id: string): void {
    queuedThumbsSet.add(id);
    pendingThumbQueue.push(id);
    pumpThumbQueue();
  }

  function pumpThumbQueue(): void {
    while (activeWorkers < CONCURRENT_THUMB_LIMIT && pendingThumbQueue.length > 0) {
      const id = pendingThumbQueue.shift()!;
      activeWorkers++;
      const gen = currentGen;
      requestPhotoThumb(id, 256)
        .then((t: PhotoThumbResult) => {
          if (gen !== currentGen) return;
          if (t.data_b64) {
            thumbs[id] = `data:${t.mime || 'image/jpeg'};base64,${t.data_b64}`;
          } else {
            thumbFailed = new Set(thumbFailed).add(id);
          }
        })
        .catch(() => {
          if (gen !== currentGen) return;
          thumbFailed = new Set(thumbFailed).add(id);
        })
        .finally(() => {
          activeWorkers--;
          if (gen === currentGen) {
            pumpThumbQueue();
          }
        });
    }
  }

  function lazyTile(node: HTMLElement, photoId: string) {
    node.dataset.photoId = photoId;
    if (thumbs[photoId]) return;
    tileObserver?.observe(node);
    return {
      update(newId: string) {
        node.dataset.photoId = newId;
        if (!thumbs[newId] && !thumbFailed.has(newId) && !queuedThumbsSet.has(newId)) {
          tileObserver?.observe(node);
        }
      },
      destroy() {
        tileObserver?.unobserve(node);
      },
    };
  }

  async function refresh(reset = true): Promise<void> {
    if (!paired) return;
    const gen = ++currentGen;
    pendingThumbQueue.length = 0;
    queuedThumbsSet.clear();
    if (reset) {
      loading = true;
      entries = [];
      nextCursor = '';
      thumbs = {};
      thumbFailed = new Set();
      selected = new Set();
    }
    error = '';
    info = '';
    lastResult = null;
    await refreshUpdateNotice();
    try {
      const res: PhotoListResult = await listPhonePhotos(reset ? '' : nextCursor, PAGE_LIMIT);
      if (gen !== currentGen) return;
      lastResult = res;
      if (res.error) throw new Error(res.error);
      entries = reset ? (res.entries ?? []) : [...entries, ...(res.entries ?? [])];
      nextCursor = res.next_cursor ?? '';
    } catch (e) {
      if (gen !== currentGen) return;
      error = e instanceof Error ? e.message : String(e);
      await refreshUpdateNotice();
    } finally {
      if (gen === currentGen) {
        loading = false;
        loadingMore = false;
      }
    }
  }

  async function loadMore(): Promise<void> {
    if (!nextCursor || loadingMore || loading) return;
    loadingMore = true;
    const gen = currentGen;
    try {
      const res = await listPhonePhotos(nextCursor, PAGE_LIMIT);
      if (gen !== currentGen) return;
      lastResult = res;
      if (res.error) throw new Error(res.error);
      entries = [...entries, ...(res.entries ?? [])];
      nextCursor = res.next_cursor ?? '';
    } catch (e) {
      if (gen !== currentGen) return;
      error = e instanceof Error ? e.message : String(e);
    } finally {
      if (gen === currentGen) {
        loadingMore = false;
      }
    }
  }

  function toggleSelect(id: string): void {
    const next = new Set(selected);
    if (next.has(id)) next.delete(id);
    else next.add(id);
    selected = next;
  }

  async function openPreview(id: string): Promise<void> {
    previewId = id; previewB64 = thumbs[id] ?? ''; previewMime = 'image/jpeg';
    try {
      const t = await requestPhotoThumb(id, 1024);
      if (t.data_b64) {
        previewMime = t.mime || 'image/jpeg';
        previewB64 = `data:${previewMime};base64,${t.data_b64}`;
      }
    } catch { /* keep grid thumb */ }
  }

  async function downloadSelected(): Promise<void> {
    const ids = [...selected];
    if (!ids.length) return;
    info = ids.length === 1 ? 'Downloading photo…' : `Downloading ${ids.length} photos…`;
    for (const id of ids) {
      try { await requestPhonePhoto(id, ''); } catch (e) { error = e instanceof Error ? e.message : String(e); }
    }
    info = 'Download started. Watch progress below.';
  }

  async function downloadOne(id: string): Promise<void> {
    info = 'Downloading photo…';
    try { await requestPhonePhoto(id, ''); info = 'Download started. Watch progress below.'; }
    catch (e) { error = e instanceof Error ? e.message : String(e); }
  }

  async function deleteSelected(): Promise<void> {
    const ids = [...selected];
    if (!ids.length || deleting) return;
    deleting = true;
    try {
      const res = await deletePhonePhotos(ids);
      const failed = res.results.filter((r) => !r.ok);
      const okIds = new Set(res.results.filter((r) => r.ok).map((r) => r.photo_id));
      entries = entries.filter((e) => !okIds.has(e.photo_id));
      selected = new Set([...selected].filter((id) => !okIds.has(id)));
      if (failed.length) error = failed.map((f) => `${f.photo_id}: ${f.error || 'not deleted'}`).join('; ');
      else info = ids.length === 1 ? 'Photo deleted.' : `${ids.length} photos deleted.`;
    } catch (e) { error = e instanceof Error ? e.message : String(e); }
    finally {
      deleting = false;
      showDeleteConfirm = false;
    }
  }

  async function pollTransfers(): Promise<void> {
    try { transfers = await getPhotoTransfers(); } catch { /* ignore */ }
  }

  onMount(() => {
    initTileObserver();
    const off1 = Events.On('photo-transfers:changed', (d: unknown) => {
      const arr = (d as { data?: unknown })?.data ?? d;
      if (Array.isArray(arr)) transfers = arr as PhotoTransferView[];
      else void pollTransfers();
    });
    void pollTransfers();
    return () => {
      try { off1(); } catch { /* ignore */ }
      tileObserver?.disconnect();
      sentinelObserver?.disconnect();
    };
  });

  // Watch sentinel element for infinite scrolling
  $effect(() => {
    if (!sentinelEl) return;
    sentinelObserver?.disconnect();
    sentinelObserver = new IntersectionObserver(
      (obsEntries) => {
        if (obsEntries[0]?.isIntersecting && nextCursor && !loadingMore && !loading) {
          void loadMore();
        }
      },
      { rootMargin: '400px' }
    );
    sentinelObserver.observe(sentinelEl);
    return () => {
      sentinelObserver?.disconnect();
    };
  });

  // Only refresh when transitioning from unpaired to paired, or initial mount
  $effect(() => {
    if (paired && !prevPaired) {
      prevPaired = true;
      void refresh(true);
    } else if (!paired) {
      prevPaired = false;
    }
  });
</script>

<section aria-label="Photos" class="relative flex h-full min-h-0 flex-1 flex-col overflow-hidden bg-window">
  <!-- toolbar: same 48px geometry as Files so the top edge never moves.
       Photos keeps its own idiom inside: library count leading, selection
       actions + refresh trailing, one prominent Download. -->
  <div class="frost-bar flex h-[48px] shrink-0 items-center gap-2 border-b border-separator px-3">
    <h2 class="shrink-0 text-[13px] font-semibold text-label">Photos</h2>
    <span class="truncate text-[11px] tabular-nums text-secondary">{#if loading}Loading…{:else}{entries.length} photo{entries.length === 1 ? '' : 's'}{selectedCount > 0 ? ` · ${selectedCount} selected` : ''}{nextCursor ? ' · more below' : ''}{/if}</span>
    <div class="ml-auto flex shrink-0 items-center gap-1.5">
      {#if selectedCount > 0}
        <button type="button" onclick={() => (selected = new Set())} title="Clear selection" class="inline-flex h-7 items-center gap-1.5 rounded-md px-2 text-[12px] text-secondary transition hover:bg-altrow hover:text-label focus-visible:outline-2 focus-visible:outline-focus active:translate-y-[1px]">
          <X size={13} /> Clear
        </button>
        <button type="button" onclick={() => showDeleteConfirm = true} title={`Delete ${selectedCount} selected photo${selectedCount === 1 ? '' : 's'}`} class="inline-flex h-7 items-center gap-1.5 rounded-md bg-bad px-2.5 text-[12px] font-medium text-white transition hover:brightness-95 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus active:translate-y-[1px]">
          <Trash2 size={13} /> Delete{#if selectedCount > 1}&nbsp;({selectedCount}){/if}
        </button>
        <button type="button" onclick={() => void downloadSelected()} title={`Download ${selectedCount} selected photo${selectedCount === 1 ? '' : 's'}`} class="inline-flex h-7 items-center gap-1.5 rounded-md bg-accent px-3 text-[13px] font-medium text-accent-text transition hover:brightness-95 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus active:translate-y-[1px]">
          <Download size={13} /> Download{#if selectedCount > 1}&nbsp;({selectedCount}){/if}
        </button>
      {/if}
      <button type="button" onclick={() => void refresh(true)} disabled={loading} aria-label="Refresh photos" title="Refresh photos" class="inline-flex h-7 w-7 items-center justify-center rounded-md border border-separator bg-control text-secondary transition hover:text-label focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus active:translate-y-[1px] disabled:opacity-50">
        <RefreshCw size={14} class={loading ? 'animate-spin' : ''} />
      </button>
    </div>
  </div>

  {#if error}
    <div role="alert" class="flex shrink-0 items-start gap-2 border-b border-separator bg-control px-3 py-2">
      <span class="mt-1 h-1.5 w-1.5 shrink-0 rounded-full bg-bad" aria-hidden="true"></span>
      <p class="flex-1 text-[12px] leading-snug text-label">{error}</p>
      <button type="button" onclick={() => error = ''} class="shrink-0 text-[11px] text-tertiary hover:text-label">Dismiss</button>
    </div>
  {/if}
  {#if info && !error}
    <p class="shrink-0 border-b border-grid bg-altrow px-3 py-1.5 text-[12px] text-secondary" role="status">{info}</p>
  {/if}

  <div class="min-h-0 flex-1 overflow-y-auto px-4 py-3">
    {#if isUpdateRequired}
      <div class="mx-auto flex max-w-[420px] flex-col items-center rounded-[12px] border border-separator bg-control px-6 py-10 text-center" role="status">
        <span class="flex h-12 w-12 items-center justify-center rounded-full bg-warn/15 text-warn" aria-hidden="true"><RefreshCw size={22} /></span>
        <h3 class="mt-3 text-[13px] font-semibold text-label">Phone needs an update</h3>
        <p class="mt-1 max-w-[34ch] text-[12px] leading-relaxed text-secondary">Update FuseItAll on Android to {updateNotice?.RequiredVersion || '0.7.0'} (build {updateNotice?.RequiredBuild ?? 7} or newer) to browse photos.</p>
      </div>
    {:else if isPermissionError}
      <div class="mx-auto flex max-w-[420px] flex-col items-center rounded-[12px] border border-separator bg-control px-6 py-10 text-center" role="status">
        <span class="flex h-12 w-12 items-center justify-center rounded-full bg-warn/15 text-warn" aria-hidden="true"><Check size={22} /></span>
        <h3 class="mt-3 text-[13px] font-semibold text-label">Photos access needed</h3>
        <p class="mt-1 max-w-[36ch] text-[12px] leading-relaxed text-secondary">The phone is sharing no images. Allow photo access so the Mac can show the library.</p>
        <p class="mt-2 max-w-[38ch] rounded-md bg-altrow px-2.5 py-2 text-[11px] leading-relaxed text-secondary">On the phone: Settings, then Apps, then FuseItAll, then Permissions, then Photos. Allow images and video. Limited access shows only the photos you selected.</p>
        <button type="button" onclick={() => void refresh(true)} class="mt-4 inline-flex h-7 items-center rounded-md bg-accent px-3 text-[13px] font-medium text-accent-text transition hover:brightness-95 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus active:translate-y-[1px]">Retry</button>
      </div>
    {:else if loading}
      <div aria-label="Loading photos">
        {#each ['June 2026', 'May 2026'] as label, gi}
          <div class="mb-5">
            <div class="sticky top-0 z-10 -mx-1 bg-window px-1 py-1.5">
              <div class="h-3 w-28 rounded bg-altrow"></div>
            </div>
            <div class="grid grid-cols-[repeat(auto-fill,minmax(148px,1fr))] gap-2.5">
              {#each Array(8) as _, i}
                <div class="aspect-square rounded-[10px] border border-separator bg-altrow" style="opacity: {0.9 - ((gi * 8 + i) % 5) * 0.12}"></div>
              {/each}
            </div>
          </div>
        {/each}
      </div>
    {:else if !entries.length}
      <div class="mx-auto flex max-w-[420px] flex-col items-center rounded-[12px] border border-separator bg-control px-6 py-10 text-center">
        <span class="flex h-11 w-11 items-center justify-center rounded-full bg-accent/15 text-accent" aria-hidden="true"><Download size={20} /></span>
        <p class="mt-3 text-[13px] font-medium text-label">No photos yet</p>
        <p class="mt-1 max-w-[32ch] text-[12px] leading-relaxed text-secondary">Photos from the phone library will appear here once the phone shares them.</p>
        <button type="button" onclick={() => void refresh(true)} class="mt-4 inline-flex h-7 items-center rounded-md border border-separator bg-window px-3 text-[12px] text-label transition hover:bg-altrow focus-visible:outline-2 focus-visible:outline-focus active:translate-y-[1px]">Refresh</button>
      </div>
    {:else}
      {#each groups as g}
        <section aria-label={g.label} class="mb-5">
          <div class="sticky top-0 z-10 -mx-1 flex items-center gap-2 bg-window px-1 py-1.5">
            <h3 class="min-w-0 flex-1 truncate text-[11px] font-semibold uppercase tracking-wide text-secondary">{g.label}</h3>
            <span class="shrink-0 rounded-full bg-altrow px-2 py-0.5 text-[11px] font-medium tabular-nums text-tertiary">{g.items.length}</span>
          </div>
          <div class="grid grid-cols-[repeat(auto-fill,minmax(148px,1fr))] gap-2.5">
            {#each g.items as item}
              <button
                class="photo-tile"
                class:selected={selected.has(item.photo_id)}
                onclick={() => openPreview(item.photo_id)}
                aria-label={`Photo from ${g.label}`}
                aria-pressed={selected.has(item.photo_id)}
                use:lazyTile={item.photo_id}
              >
                {#if thumbs[item.photo_id]}
                  <img src={thumbs[item.photo_id]} alt="" loading="lazy" draggable="false" />
                {:else if thumbFailed.has(item.photo_id)}
                  <span class="photo-fallback" aria-hidden="true">No preview</span>
                {:else}
                  <span class="photo-skeleton" aria-hidden="true"></span>
                {/if}
                <span
                  class="photo-check"
                  role="checkbox"
                  tabindex={0}
                  aria-checked={selected.has(item.photo_id)}
                  aria-label={selected.has(item.photo_id) ? 'Deselect photo' : 'Select photo'}
                  onclick={(e) => { e.stopPropagation(); toggleSelect(item.photo_id); }}
                  onkeydown={(e) => { if (e.key === ' ' || e.key === 'Enter') { e.preventDefault(); e.stopPropagation(); toggleSelect(item.photo_id); } }}
                ><Check size={12} strokeWidth={3} /></span>
              </button>
            {/each}
          </div>
        </section>
      {/each}

      <!-- Infinite scroll sentinel -->
      {#if nextCursor}
        <div bind:this={sentinelEl} class="flex h-12 w-full items-center justify-center gap-2">
          {#if loadingMore}
            <span class="h-3 w-3 animate-spin rounded-full border-2 border-accent border-t-transparent" aria-hidden="true"></span>
            <span class="text-[11px] text-secondary">Loading more photos…</span>
          {:else}
            <span class="text-[11px] text-tertiary">Scroll for more</span>
          {/if}
        </div>
      {/if}
    {/if}
  </div>

  <!-- status line: full-width static strip (h-30), same geometry as Files.
       Always rendered; only the text swaps, never the layout. -->
  <div class="flex h-[30px] shrink-0 items-center gap-2 overflow-hidden border-t border-separator bg-control px-3" role="status" aria-live="polite">
    {#if transfers.some((t) => t.status === 'running')}
      {@const running = transfers.filter((t) => t.status === 'running')}
      <span class="h-1.5 w-24 shrink-0 overflow-hidden rounded bg-grid" aria-hidden="true">
        <span class="block h-full origin-left bg-accent transition-transform duration-200 ease-linear" style="transform: scaleX({(running[0]?.progress ?? 0) / 100})"></span>
      </span>
      <span class="min-w-0 flex-1 truncate text-[11px] tabular-nums text-secondary">Downloading {running.length} photo{running.length === 1 ? '' : 's'} · {running[0]?.progress ?? 0}%</span>
      <button type="button" onclick={() => void cancelPhotoTransfer(running[0].id)} class="shrink-0 text-[11px] text-bad hover:underline">Cancel</button>
    {:else if loadingMore}
      <span class="h-3 w-3 shrink-0 animate-spin rounded-full border-2 border-accent border-t-transparent" aria-hidden="true"></span>
      <span class="truncate text-[11px] text-secondary">Loading more photos…</span>
    {:else if loading}
      <span class="h-3 w-3 shrink-0 animate-spin rounded-full border-2 border-accent border-t-transparent" aria-hidden="true"></span>
      <span class="truncate text-[11px] text-secondary">Loading photos…</span>
    {:else if !paired}
      <span class="h-1.5 w-1.5 shrink-0 rounded-full bg-warn" aria-hidden="true"></span>
      <span class="truncate text-[11px] text-tertiary">Phone offline · showing cached photos</span>
    {:else}
      <span class="h-1.5 w-1.5 shrink-0 rounded-full bg-tertiary" aria-hidden="true"></span>
      <span class="truncate text-[11px] text-tertiary">{entries.length} photo{entries.length === 1 ? '' : 's'}{nextCursor ? ' · scroll for more' : ' · up to date'}</span>
    {/if}
  </div>

  {#if previewEntry}
    <div class="photo-modal" role="dialog" aria-modal="true" aria-label="Photo preview" onclick={() => (previewId = null)} onkeydown={(e) => { if (e.key === 'Escape') previewId = null; }}>
      <div class="photo-modal-card" role="presentation" onclick={(e) => e.stopPropagation()}>
        <div class="photo-modal-imgwell">
          {#if previewB64}<img src={previewB64} alt="" draggable="false" />{:else}<span class="photo-skeleton" aria-hidden="true"></span>{/if}
        </div>
        <div class="flex items-center gap-2">
          <p class="min-w-0 flex-1 truncate text-[11px] tabular-nums text-secondary">{new Date(previewEntry.taken_at).toLocaleString()}</p>
          {#if selected.has(previewEntry.photo_id)}
            <span class="shrink-0 rounded-full bg-accent px-2 py-0.5 text-[10px] font-semibold text-accent-text">Selected</span>
          {/if}
        </div>
        <div class="flex items-center gap-2">
          <button type="button" onclick={() => previewEntry && toggleSelect(previewEntry.photo_id)} class="inline-flex h-7 shrink-0 items-center gap-1.5 rounded-md border border-separator bg-window px-2.5 text-[12px] text-label transition hover:bg-altrow focus-visible:outline-2 focus-visible:outline-focus active:translate-y-[1px]">
            <Check size={13} /> {previewEntry && selected.has(previewEntry.photo_id) ? 'Deselect' : 'Select'}
          </button>
          <span class="flex-1"></span>
          <button type="button" onclick={() => (previewId = null)} class="inline-flex h-7 shrink-0 items-center rounded-md border border-separator bg-window px-3 text-[12px] text-label transition hover:bg-altrow focus-visible:outline-2 focus-visible:outline-focus active:translate-y-[1px]">Close</button>
          <button type="button" onclick={() => previewEntry && void downloadOne(previewEntry.photo_id)} class="inline-flex h-7 shrink-0 items-center gap-1.5 rounded-md bg-accent px-3 text-[13px] font-medium text-accent-text transition hover:brightness-95 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus active:translate-y-[1px]"><Download size={13} /> Download</button>
        </div>
      </div>
    </div>
  {/if}

  {#if showDeleteConfirm}
    <div class="fixed inset-0 z-40 flex items-center justify-center bg-black/30 p-4" onclick={() => { if (!deleting) showDeleteConfirm = false; }} onkeydown={(e) => { if (e.key === 'Escape' && !deleting) showDeleteConfirm = false; }} role="presentation">
      <div role="dialog" aria-modal="true" aria-label="Delete photos" class="w-full max-w-[380px] rounded-[12px] border border-separator bg-control p-4 shadow-xl" onclick={(e) => e.stopPropagation()}>
        <div class="flex items-start gap-3">
          <span class="flex h-9 w-9 shrink-0 items-center justify-center rounded-full bg-bad/15 text-bad" aria-hidden="true"><Trash2 size={16} /></span>
          <div class="min-w-0">
            <h3 class="text-[13px] font-semibold text-label">Delete {selectedCount} photo{selectedCount === 1 ? '' : 's'}?</h3>
            <p class="mt-1 text-[12px] leading-snug text-secondary">This removes them from the phone. This cannot be undone.</p>
          </div>
        </div>
        <div class="mt-4 flex justify-end gap-2">
          <button type="button" onclick={() => showDeleteConfirm = false} disabled={deleting} class="h-7 rounded-md border border-separator bg-window px-3 text-[13px] text-label transition hover:bg-altrow focus-visible:outline-2 focus-visible:outline-focus active:translate-y-[1px] disabled:opacity-50">Keep</button>
          <button type="button" onclick={() => void deleteSelected()} disabled={deleting} class="inline-flex h-7 items-center gap-1.5 rounded-md bg-bad px-3 text-[13px] font-medium text-white transition hover:brightness-95 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus active:translate-y-[1px] disabled:opacity-50">
            {#if deleting}<span class="spinner" aria-hidden="true"></span><span>Deleting…</span>{:else}<span>Delete</span>{/if}
          </button>
        </div>
      </div>
    </div>
  {/if}
</section>

<style>
  .photo-tile { position: relative; aspect-ratio: 1; overflow: hidden; border-radius: 10px; border: 1px solid var(--separator); background: var(--alt-row-bg); transition: transform 0.12s ease, box-shadow 0.12s ease; }
  .photo-tile:hover { transform: translateY(-1px); box-shadow: 0 4px 14px rgba(0, 0, 0, 0.12); }
  .photo-tile:focus-visible { outline: 2px solid var(--focus-ring); outline-offset: 2px; }
  .photo-tile img { width: 100%; height: 100%; object-fit: cover; display: block; }
  .photo-tile.selected { outline: 2px solid var(--accent); outline-offset: 1px; }
  .photo-skeleton { display: block; width: 100%; height: 100%; background: var(--alt-row-bg); }
  .photo-modal-imgwell .photo-skeleton { width: min(480px, 70vw); height: min(50vh, 420px); }
  .photo-fallback { display: flex; align-items: center; justify-content: center; height: 100%; font-size: 12px; color: var(--secondary-label); }
  .photo-check { position: absolute; top: 8px; right: 8px; width: 24px; height: 24px; border-radius: 9999px; display: flex; align-items: center; justify-content: center; background: color-mix(in srgb, var(--window-bg) 82%, transparent); border: 1px solid var(--separator); color: transparent; transition: transform 0.1s ease; }
  .photo-tile:hover .photo-check, .photo-tile:focus-visible .photo-check, .photo-tile.selected .photo-check { color: var(--secondary-label); }
  .photo-check:hover { transform: scale(1.08); }
  .photo-tile.selected .photo-check { background: var(--accent); color: white; border-color: transparent; }
  .photo-modal { position: fixed; inset: 0; display: flex; align-items: center; justify-content: center; background: rgb(0 0 0 / 0.45); z-index: 50; padding: 24px; }
  .photo-modal-card { background: var(--window-bg); border: 1px solid var(--separator); border-radius: 12px; padding: 16px; width: fit-content; max-width: min(920px, 94vw); max-height: 90vh; display: flex; flex-direction: column; align-items: stretch; gap: 12px; box-shadow: 0 16px 48px rgba(0, 0, 0, 0.25); }
  .photo-modal-imgwell { display: flex; align-items: center; justify-content: center; flex: 1 1 auto; min-height: 0; width: 100%; max-width: 100%; margin: 0 auto; border-radius: 8px; background: var(--alt-row-bg); overflow: hidden; }
  .photo-modal-card img { width: auto; height: auto; max-width: min(860px, 88vw); max-height: min(78vh, calc(90vh - 134px)); object-fit: contain; border-radius: 8px; display: block; }

  @media (prefers-reduced-motion: reduce) {
    .photo-tile, .photo-check { transition: none; }
    .photo-tile:hover { transform: none; box-shadow: none; }
  }
</style>
