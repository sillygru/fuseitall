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
  let previewId = $state<string | null>(null);
  let previewB64 = $state('');
  let previewMime = $state('image/jpeg');
  let transfers = $state<PhotoTransferView[]>([]);
  let updateNotice = $state<UpdateNotice | null>(null);

  const PAGE_LIMIT = 100;

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

  async function refresh(reset = true): Promise<void> {
    if (!paired) return;
    if (reset) { loading = true; entries = []; nextCursor = ''; thumbs = {}; thumbFailed = new Set(); selected = new Set(); }
    error = ''; info = ''; lastResult = null;
    await refreshUpdateNotice();
    try {
      const res: PhotoListResult = await listPhonePhotos(reset ? '' : nextCursor, PAGE_LIMIT);
      lastResult = res;
      if (res.error) throw new Error(res.error);
      entries = reset ? (res.entries ?? []) : [...entries, ...(res.entries ?? [])];
      nextCursor = res.next_cursor ?? '';
      void fillThumbs(res.entries ?? []);
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
      await refreshUpdateNotice();
    } finally { loading = false; loadingMore = false; }
  }

  async function loadMore(): Promise<void> {
    if (!nextCursor || loadingMore || loading) return;
    loadingMore = true;
    try {
      const res = await listPhonePhotos(nextCursor, PAGE_LIMIT);
      lastResult = res;
      if (res.error) throw new Error(res.error);
      entries = [...entries, ...(res.entries ?? [])];
      nextCursor = res.next_cursor ?? '';
      void fillThumbs(res.entries ?? []);
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    } finally { loadingMore = false; }
  }

  async function fillThumbs(batch: PhotoEntryView[]): Promise<void> {
    const queue = batch.filter((e) => !thumbs[e.photo_id] && !thumbFailed.has(e.photo_id)).slice(0, 60);
    for (const e of queue) {
      try {
        const t: PhotoThumbResult = await requestPhotoThumb(e.photo_id, 256);
        if (t.data_b64) {
          thumbs = { ...thumbs, [e.photo_id]: `data:${t.mime || 'image/jpeg'};base64,${t.data_b64}` };
        } else if (t.error) {
          thumbFailed = new Set(thumbFailed).add(e.photo_id);
        }
      } catch {
        thumbFailed = new Set(thumbFailed).add(e.photo_id);
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
    info = 'Download started — see progress below.';
  }

  async function downloadOne(id: string): Promise<void> {
    info = 'Downloading photo…';
    try { await requestPhonePhoto(id, ''); info = 'Download started — see progress below.'; }
    catch (e) { error = e instanceof Error ? e.message : String(e); }
  }

  async function deleteSelected(): Promise<void> {
    const ids = [...selected];
    if (!ids.length) return;
    try {
      const res = await deletePhonePhotos(ids);
      const failed = res.results.filter((r) => !r.ok);
      const okIds = new Set(res.results.filter((r) => r.ok).map((r) => r.photo_id));
      entries = entries.filter((e) => !okIds.has(e.photo_id));
      selected = new Set([...selected].filter((id) => !okIds.has(id)));
      if (failed.length) error = failed.map((f) => `${f.photo_id}: ${f.error || 'not deleted'}`).join('; ');
      else info = ids.length === 1 ? 'Photo deleted.' : `${ids.length} photos deleted.`;
    } catch (e) { error = e instanceof Error ? e.message : String(e); }
  }

  async function pollTransfers(): Promise<void> {
    try { transfers = await getPhotoTransfers(); } catch { /* ignore */ }
  }

  onMount(() => {
    const off1 = Events.On('photo-transfers:changed', (d: unknown) => {
      const arr = (d as { data?: unknown })?.data ?? d;
      if (Array.isArray(arr)) transfers = arr as PhotoTransferView[];
      else void pollTransfers();
    });
    const off2 = Events.On('state:changed', () => {
      void refresh(true);
    });
    void refresh(true);
    void pollTransfers();
    return () => {
      try { off1(); } catch { /* ignore */ }
      try { off2(); } catch { /* ignore */ }
    };
  });

  $effect(() => { if (paired) void refresh(true); });
</script>

<div class="flex min-h-0 flex-1 flex-col gap-3">
  <div class="frost-bar flex items-center gap-2 rounded-lg px-3 py-2">
    <h2 class="text-title-3 font-semibold">Photos</h2>
    <span class="text-footnote text-secondary-label">{entries.length} photos</span>
    <div class="ml-auto flex items-center gap-2">
      {#if selectedCount > 0}
        <span class="text-footnote text-secondary-label">{selectedCount} selected</span>
        <button class="btn-secondary" onclick={() => void downloadSelected()}><Download size={14} /> Download</button>
        <button class="btn-destructive" onclick={() => void deleteSelected()}><Trash2 size={14} /> Delete</button>
        <button class="btn-ghost" onclick={() => (selected = new Set())}><X size={14} /> Clear</button>
      {/if}
      <button class="btn-ghost" onclick={() => void refresh(true)} aria-label="Refresh photos"><RefreshCw size={14} /></button>
    </div>
  </div>

  {#if isUpdateRequired}
    <div class="rounded-lg border border-separator bg-control p-4" role="status">
      <h3 class="text-headline font-semibold">Phone needs an update</h3>
      <p class="text-body text-secondary-label">Update FuseItAll on android to {updateNotice?.RequiredVersion || '0.7.0'} (build &gt;= {updateNotice?.RequiredBuild ?? 7}) to browse photos.</p>
    </div>
  {:else if isPermissionError}
    <div class="rounded-lg border border-separator bg-control p-4" role="status">
      <h3 class="text-headline font-semibold">Photos access needed</h3>
      <p class="text-body text-secondary-label">On the phone: Settings then Apps then FuseItAll then Permissions then Photos, then allow images and video. Limited access shows only the photos you selected.</p>
    </div>
  {:else if error}
    <div class="rounded-lg border border-separator bg-control p-4" role="alert">
      <p class="text-body">{error}</p>
    </div>
  {/if}
  {#if info}<p class="text-callout text-secondary-label" role="status">{info}</p>{/if}

  {#if loading}
    <p class="text-body text-secondary-label">Loading photos…</p>
  {:else}
    {#each groups as g}
      <section aria-label={g.label}>
        <h3 class="text-subheadline font-semibold text-secondary-label">{g.label}</h3>
        <div class="grid grid-cols-[repeat(auto-fill,minmax(120px,1fr))] gap-2">
          {#each g.items as item}
            <button
              class="photo-tile"
              class:selected={selected.has(item.photo_id)}
              onclick={() => openPreview(item.photo_id)}
              aria-label={`Photo ${item.photo_id}`}
            >
              {#if thumbs[item.photo_id]}
                <img src={thumbs[item.photo_id]} alt="" loading="lazy" />
              {:else}
                <span class="photo-fallback" aria-hidden="true">No preview</span>
              {/if}
              <span
                class="photo-check"
                role="checkbox"
                tabindex={0}
                aria-checked={selected.has(item.photo_id)}
                onclick={(e) => { e.stopPropagation(); toggleSelect(item.photo_id); }}
                onkeydown={(e) => { if (e.key === ' ' || e.key === 'Enter') { e.preventDefault(); e.stopPropagation(); toggleSelect(item.photo_id); } }}
              ><Check size={12} /></span>
            </button>
          {/each}
        </div>
      </section>
    {/each}
    {#if nextCursor}
      <button class="btn-secondary self-center" onclick={() => void loadMore()} disabled={loadingMore}>
        {loadingMore ? 'Loading…' : 'Load more'}
      </button>
    {/if}
  {/if}

  {#if transfers.some((t) => t.status === 'running')}
    <div class="rounded-lg border border-separator bg-control p-3" role="status">
      {#each transfers.filter((t) => t.status === 'running') as t}
        <div class="flex items-center gap-2">
          <span class="text-footnote">Photo {t.photo_id} — {t.progress}%</span>
          <button class="btn-ghost" onclick={() => void cancelPhotoTransfer(t.id)}>Cancel</button>
        </div>
      {/each}
    </div>
  {/if}

  {#if previewEntry}
    <div class="photo-modal" role="dialog" aria-label="Photo preview">
      <div class="photo-modal-card">
        {#if previewB64}<img src={previewB64} alt="" />{/if}
        <p class="text-footnote text-secondary-label">{new Date(previewEntry.taken_at).toLocaleString()}</p>
        <div class="flex gap-2">
          <button class="btn-secondary" onclick={() => previewEntry && void downloadOne(previewEntry.photo_id)}><Download size={14} /> Download</button>
          <button class="btn-ghost" onclick={() => (previewId = null)}>Close</button>
        </div>
      </div>
    </div>
  {/if}
</div>

<style>
  .photo-tile { position: relative; aspect-ratio: 1; overflow: hidden; border-radius: 8px; border: 1px solid var(--separator); background: var(--control-bg); }
  .photo-tile img { width: 100%; height: 100%; object-fit: cover; display: block; }
  .photo-tile.selected { outline: 2px solid var(--accent); }
  .photo-fallback { display: flex; align-items: center; justify-content: center; height: 100%; font-size: 12px; color: var(--secondary-label); }
  .photo-check { position: absolute; top: 6px; right: 6px; width: 22px; height: 22px; border-radius: 9999px; display: flex; align-items: center; justify-content: center; background: color-mix(in srgb, var(--window-bg) 70%, transparent); border: 1px solid var(--separator); }
  .photo-tile.selected .photo-check { background: var(--accent); color: white; border-color: transparent; }
  .photo-modal { position: fixed; inset: 0; display: flex; align-items: center; justify-content: center; background: rgb(0 0 0 / 0.5); z-index: 50; }
  .photo-modal-card { background: var(--window-bg); border: 1px solid var(--separator); border-radius: 12px; padding: 16px; max-width: min(720px, 90vw); display: flex; flex-direction: column; gap: 12px; }
  .photo-modal-card img { max-height: 60vh; object-fit: contain; border-radius: 8px; }
</style>
