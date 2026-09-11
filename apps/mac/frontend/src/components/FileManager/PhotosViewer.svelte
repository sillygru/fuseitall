<!--
  Copyright (C) 2026 FuseItAll contributors.

  This program is free software: you can redistribute it and/or modify
  it under the terms of the GNU Affero General Public License as published
  by the Free Software Foundation, version 3 of the License. See LICENSE
  for details.

  Photos — date-grouped grid for the Android photo and video library.
  HIG Read: primary window detail pane for photo and video browsing,
  following HIG Windows, Toolbars, Sidebars/Split Views, Color, Typography,
  Buttons, Progress, Context Menus. Classic frost, no Liquid Glass.
-->
<script lang="ts">
  import { onMount, untrack } from 'svelte';
  import { fade, scale } from 'svelte/transition';
  import { RefreshCw, Download, Trash2, X, Check, ChevronLeft, ChevronRight, Play, Image as ImageIcon } from '@lucide/svelte';
  import { Events } from '@wailsio/runtime';
  import type { PhotoEntryView, PhotoListResult, PhotoThumbResult, PhotoTransferView } from '../../backend';
  import { listPhonePhotos, requestPhotoThumb, requestPhoneMedia, startPhotoStream, deletePhonePhotos, getPhotoTransfers, cancelPhotoTransfer, isPhotosPermissionError, isVideoEntry, formatDuration } from '../../backend';
  import { isFresh, withTimeout, LIST_TIMEOUT_MS, LIST_TTL_MS } from '../../lib/paneCache';
  import { Service } from '../../backend';
  import ContentHeader from '../ContentHeader.svelte';

  interface UpdateNotice {
    Active: boolean;
    Self: boolean;
    Message: string;
    RequiredVersion: string;
    CurrentVersion: string;
    RequiredBuild: number;
  }

  interface Props { paired: boolean; deviceLabel?: string; active?: boolean; peerKey?: string }
  let { paired, deviceLabel = '', active = true, peerKey = '' }: Props = $props();

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

  // Library filter: one mixed timeline, narrowed in place.
  type LibraryFilter = 'all' | 'photos' | 'videos';
  let filter = $state<LibraryFilter>('all');
  // Video stream state for the open preview (loopback URL from the backend).
  let streamUrl = $state('');
  let streamTransferId = $state<string | null>(null);
  let streamLoading = $state(false);
  let streamError = $state('');

  // Progressive batch size: 50 items per page for quick initial load
  const PAGE_LIMIT = 50;
  const CONCURRENT_THUMB_LIMIT = 3;
  // Empty-success retry: the phone media indexer can lag behind pairing
  // or a permission grant, so a first list may come back with zero
  // entries. Retry a few times before surfacing "No photos yet".
  const EMPTY_RETRY_MAX = 3;
  const EMPTY_RETRY_DELAY_MS = 1000;
  // Empty listings go stale fast: a zero-entry success during phone
  // indexer lag must retry in seconds, not sit behind the 30s populated
  // TTL showing "No photos yet".
  const EMPTY_TTL_MS = 5_000;

  let currentGen = 0;
  // Listing RAM cache: reselecting Photos reuses entries while fresh.
  // Thumbnails are immutable per photo id and live for the session.
  let lastFetchAt = $state(0);
  let refreshing = $state(false);
  let inflight = false;
  // A fetch dropped because one was already in flight (tab switch racing
  // a peer-switch wipe) must run afterwards, never vanish silently.
  let needsRefresh = false;
  let prevPeerKey = '';
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
  // Absent media_type means photo (pre-0.8.0 peers).
  let visibleEntries = $derived(
    filter === 'all' ? entries : entries.filter((e) => isVideoEntry(e) === (filter === 'videos')),
  );
  let groups = $derived.by<DateGroup[]>(() => {
    const map = new Map<string, PhotoEntryView[]>();
    for (const e of visibleEntries) {
      const d = new Date(e.taken_at);
      const key = Number.isNaN(d.getTime()) ? 'Unknown date' : d.toLocaleDateString(undefined, { year: 'numeric', month: 'long' });
      const list = map.get(key) ?? [];
      list.push(e);
      map.set(key, list);
    }
    return [...map.entries()].map(([label, items]) => ({ label, items }));
  });
  let kindNoun = $derived(filter === 'videos' ? 'video' : filter === 'photos' ? 'photo' : 'item');
  let photoCountLabel = $derived(
    loading
      ? 'Loading…'
      : `${visibleEntries.length} ${kindNoun}${visibleEntries.length === 1 ? '' : 's'}${deviceLabel ? ` · ${deviceLabel}` : ''}${nextCursor ? ' · more below' : ''}`,
  );
  let selectedCount = $derived(selected.size);
  let selectedHasVideo = $derived([...selected].some((id) => entries.find((e) => e.photo_id === id)?.media_type === 'video'));
  // Active full downloads only: video streams buffer in the background and
  // never park the status line.
  let runningDownloads = $derived(transfers.filter((t) => t.status === 'running' && !t.stream));
  let previewEntry = $derived(previewId ? entries.find((e) => e.photo_id === previewId) ?? null : null);
  let previewIsVideo = $derived(previewEntry ? isVideoEntry(previewEntry) : false);

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
      previewPrefetchCursor = '';
      // Thumbs stay: they are immutable per photo id and outlive listings.
      selected = new Set();
    }
    error = '';
    info = '';
    lastResult = null;
    // Fire-and-forget: the notice backend must never park the listing
    // fetch behind it (a hanging notice stranded `loading` forever).
    void refreshUpdateNotice();
    try {
      let res: PhotoListResult = await withTimeout(
        listPhonePhotos(reset ? '' : nextCursor, PAGE_LIMIT),
        LIST_TIMEOUT_MS,
        'photo list',
      );
      if (gen !== currentGen) return;
      // Empty-success retry (first page only): the phone indexer can lag,
      // so a zero-entry list right after pairing/grant is often transient.
      // Errors, permission gates, and update-required never retry here.
      if (reset) {
        const key = peerKey;
        for (let attempt = 0; attempt < EMPTY_RETRY_MAX; attempt++) {
          if (res.error || res.error_code === 'UPDATE_REQUIRED') break;
          if ((res.entries ?? []).length > 0) break;
          if (isPhotosPermissionError(res, '')) break;
          await new Promise((r) => setTimeout(r, EMPTY_RETRY_DELAY_MS));
          if (gen !== currentGen || !paired || key !== peerKey) return;
          res = await withTimeout(listPhonePhotos('', PAGE_LIMIT), LIST_TIMEOUT_MS, 'photo list');
          if (gen !== currentGen || key !== peerKey) return;
        }
      }
      lastResult = res;
      if (res.error) throw new Error(res.error);
      const got = res.entries ?? [];
      entries = reset ? got : [...entries, ...got];
      nextCursor = res.next_cursor ?? '';
      // A transient empty (phone indexer lag) must not sit behind the
      // 30s populated TTL: stamp it nearly stale so ensureFresh retries
      // in seconds instead of parking on "No photos yet".
      lastFetchAt = got.length > 0 ? Date.now() : Date.now() - LIST_TTL_MS + EMPTY_TTL_MS;
    } catch (e) {
      if (gen !== currentGen) return;
      error = e instanceof Error ? e.message : String(e);
      void refreshUpdateNotice();
    } finally {
      // Unconditional: a stale settler may briefly hide a newer spinner,
      // but the spinner can never strand on `true` again.
      loading = false;
      loadingMore = false;
    }
  }

  async function ensureFresh(): Promise<void> {
    if (!paired) return;
    if (inflight) {
      needsRefresh = true;
      return;
    }
    if (entries.length && isFresh(lastFetchAt)) return;
    inflight = true;
    try {
      if (entries.length) await refreshQuiet();
      else await refresh(true);
    } finally {
      inflight = false;
      if (needsRefresh) {
        needsRefresh = false;
        if (paired) {
          if (entries.length && isFresh(lastFetchAt)) return;
          inflight = true;
          try {
            if (entries.length) await refreshQuiet();
            else await refresh(true);
          } finally {
            inflight = false;
          }
        }
      }
    }
  }

  // Background first-page reload: the stale grid stays in place with its
  // cached thumbs, so there is no skeleton flash and keyed rows keep
  // their DOM (no restagger). Only the status line reports progress.
  // A transient empty never wipes a good grid and never stamps fresh.
  async function refreshQuiet(): Promise<void> {
    if (!paired || refreshing) return;
    const key = peerKey;
    refreshing = true;
    try {
      const res = await withTimeout(listPhonePhotos('', PAGE_LIMIT), LIST_TIMEOUT_MS, 'photo list');
      // A peer switch mid-flight must not paint the old phone's rows.
      if (key !== peerKey) return;
      lastResult = res;
      if (res.error) throw new Error(res.error);
      const got = res.entries ?? [];
      if (got.length === 0 && entries.length > 0) return;
      entries = got;
      nextCursor = res.next_cursor ?? '';
      lastFetchAt = got.length > 0 ? Date.now() : Date.now() - LIST_TTL_MS + EMPTY_TTL_MS;
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
      void refreshUpdateNotice();
    } finally {
      refreshing = false;
    }
  }

  async function loadMore(): Promise<void> {
    if (!nextCursor || loadingMore || loading || refreshing) return;
    loadingMore = true;
    const gen = currentGen;
    try {
      const res = await withTimeout(listPhonePhotos(nextCursor, PAGE_LIMIT), LIST_TIMEOUT_MS, 'photo list');
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

  // Preview nav guards: every hi-res fetch carries a generation so rapid
  // arrowing never paints stale. Keys are handled at window level while
  // the viewer is open, so navigation never depends on dialog focus.
  let previewGen = 0;

  function stopStream(): void {
    if (streamTransferId) {
      void cancelPhotoTransfer(streamTransferId).catch(() => {});
      streamTransferId = null;
    }
    streamUrl = '';
    streamLoading = false;
    streamError = '';
  }

  async function openPreview(id: string): Promise<void> {
    const pgen = ++previewGen;
    stopStream();
    previewId = id; previewB64 = thumbs[id] ?? ''; previewMime = 'image/jpeg';
    const entry = entries.find((e) => e.photo_id === id) ?? null;
    if (entry && isVideoEntry(entry)) {
      // Video: stream over loopback so gigabyte files play before the
      // download finishes. The grid thumb doubles as poster.
      streamLoading = true;
      try {
        const started = await startPhotoStream(id, entry.mime ?? '');
        if (pgen !== previewGen || previewId !== id) {
          if (started.transferId) void cancelPhotoTransfer(started.transferId).catch(() => {});
          return;
        }
        streamTransferId = started.transferId;
        streamUrl = started.url;
        if (!streamUrl) throw new Error('Video streaming needs app 0.8.0 on this Mac.');
      } catch (e) {
        if (pgen !== previewGen || previewId !== id) return;
        streamError = e instanceof Error ? e.message : String(e);
      } finally {
        if (pgen === previewGen && previewId === id) streamLoading = false;
      }
      return;
    }
    try {
      const t = await requestPhotoThumb(id, 1024);
      // Rapid arrowing must never paint a stale hi-res over a newer one.
      if (pgen !== previewGen || previewId !== id) return;
      if (t.data_b64) {
        previewMime = t.mime || 'image/jpeg';
        previewB64 = `data:${previewMime};base64,${t.data_b64}`;
      }
    } catch { /* keep grid thumb */ }
  }

  function closePreview(): void {
    previewGen++;
    stopStream();
    previewId = null;
  }

  // Flat position of the open item: groups are display-only, so arrows
  // walk the filtered list and clamp at the ends (no wrap).
  let previewIndex = $derived(previewId ? visibleEntries.findIndex((e) => e.photo_id === previewId) : -1);
  let paneRoot = $state<HTMLElement | null>(null);

  function stepPreview(dir: 1 | -1): void {
    if (previewIndex < 0 || !visibleEntries.length) return;
    const next = Math.min(visibleEntries.length - 1, Math.max(0, previewIndex + dir));
    if (next === previewIndex) return;
    void openPreview(visibleEntries[next].photo_id);
    maybePrefetchPhotos(next);
  }

  // Finder-style vertical move: ± one grid row. Columns are measured
  // live (auto-fill layout), since every group grid shares one width.
  function gridColumns(): number {
    const grid = paneRoot?.querySelector('.photo-grid') as HTMLElement | null;
    const tile = grid?.querySelector('.photo-tile') as HTMLElement | null;
    if (!grid || !tile) return 1;
    const gap = 4;
    return Math.max(1, Math.round((grid.clientWidth + gap) / (tile.getBoundingClientRect().width + gap)));
  }

  function stepPreviewVertical(dir: 1 | -1): void {
    if (previewIndex < 0 || !visibleEntries.length) return;
    const next = Math.min(visibleEntries.length - 1, Math.max(0, previewIndex + dir * gridColumns()));
    if (next === previewIndex) return;
    void openPreview(visibleEntries[next].photo_id);
    maybePrefetchPhotos(next);
  }

  // One prefetch per listing cursor: keeps the viewer ahead near the end
  // without looping on short/empty final pages.
  let previewPrefetchCursor = '';

  function maybePrefetchPhotos(next: number): void {
    if (!nextCursor || loadingMore || loading || refreshing) return;
    if (next < visibleEntries.length - 5) return;
    if (nextCursor === previewPrefetchCursor) return;
    previewPrefetchCursor = nextCursor;
    void loadMore();
  }

  let lastSelectedPhotoId = $state<string | null>(null);

  function onPhotoClick(e: MouseEvent, photoId: string) {
    if (e.metaKey || e.ctrlKey) {
      toggleSelect(photoId);
      lastSelectedPhotoId = photoId;
    } else if (e.shiftKey && lastSelectedPhotoId) {
      const allIds = visibleEntries.map((entry) => entry.photo_id);
      const startIdx = allIds.indexOf(lastSelectedPhotoId);
      const endIdx = allIds.indexOf(photoId);
      if (startIdx !== -1 && endIdx !== -1) {
        const [low, high] = startIdx < endIdx ? [startIdx, endIdx] : [endIdx, startIdx];
        const next = new Set(selected);
        for (let i = low; i <= high; i++) {
          next.add(allIds[i]);
        }
        selected = next;
      } else {
        toggleSelect(photoId);
        lastSelectedPhotoId = photoId;
      }
    } else {
      openPreview(photoId);
    }
  }

  function onPreviewWindowKey(e: KeyboardEvent): void {
    if (!active) return;
    const target = e.target as HTMLElement | null;
    if (target?.closest('input, textarea')) return;

    if (previewEntry) {
      if (e.key === 'ArrowRight') { e.preventDefault(); stepPreview(1); }
      else if (e.key === 'ArrowLeft') { e.preventDefault(); stepPreview(-1); }
      else if (e.key === 'ArrowDown') { e.preventDefault(); stepPreviewVertical(1); }
      else if (e.key === 'ArrowUp') { e.preventDefault(); stepPreviewVertical(-1); }
      else if (e.key === 'Escape') { closePreview(); }
      else if (e.key === ' ' || e.key === 'Spacebar') {
        // Let focused buttons keep their native Space activation.
        if (target && (target.tagName === 'BUTTON' || target.tagName === 'INPUT')) return;
        e.preventDefault();
        toggleSelect(previewEntry.photo_id);
      }
      return;
    }

    // Grid view shortcuts:
    if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'a') {
      e.preventDefault();
      selected = new Set(visibleEntries.map((entry) => entry.photo_id));
      return;
    }
    if (e.key === 'Escape') {
      if (selected.size > 0) {
        e.preventDefault();
        selected = new Set();
        lastSelectedPhotoId = null;
      }
      return;
    }
    if (e.key === 'Backspace' && (e.metaKey || e.ctrlKey)) {
      if (selected.size > 0) {
        e.preventDefault();
        showDeleteConfirm = true;
      }
      return;
    }
  }

  function fmtBytes(n?: number): string {
    if (!n) return '';
    if (n < 1024) return `${n} B`;
    if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
    if (n < 1024 * 1024 * 1024) return `${(n / 1024 / 1024).toFixed(1)} MB`;
    return `${(n / 1024 / 1024 / 1024).toFixed(2)} GB`;
  }

  function mimeShort(m?: string): string {
    if (!m) return '';
    const parts = m.split('/');
    return (parts[1] || m).toUpperCase();
  }

  function mimeFor(id: string): string {
    return entries.find((e) => e.photo_id === id)?.mime ?? '';
  }

  async function downloadSelected(): Promise<void> {
    const ids = [...selected];
    if (!ids.length) return;
    const noun = selectedHasVideo ? 'item' : 'photo';
    info = ids.length === 1 ? `Downloading ${noun}…` : `Downloading ${ids.length} ${noun}s…`;
    for (const id of ids) {
      try { await requestPhoneMedia(id, mimeFor(id), ''); } catch (e) { error = e instanceof Error ? e.message : String(e); }
    }
    info = 'Download started. Watch progress below.';
  }

  async function downloadOne(id: string): Promise<void> {
    const noun = isVideoEntry(entries.find((e) => e.photo_id === id) ?? { photo_id: id, taken_at: 0 }) ? 'video' : 'photo';
    info = `Downloading ${noun}…`;
    try { await requestPhoneMedia(id, mimeFor(id), ''); info = 'Download started. Watch progress below.'; }
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
      const hadVideo = ids.some((id) => entries.find((e) => e.photo_id === id)?.media_type === 'video');
      entries = entries.filter((e) => !okIds.has(e.photo_id));
      selected = new Set([...selected].filter((id) => !okIds.has(id)));
      lastFetchAt = Date.now();
      if (failed.length) error = failed.map((f) => `${f.photo_id}: ${f.error || 'not deleted'}`).join('; ');
      else if (hadVideo) info = ids.length === 1 ? 'Item deleted.' : `${ids.length} items deleted.`;
      else info = ids.length === 1 ? 'Photo deleted.' : `${ids.length} photos deleted.`;
    } catch (e) { error = e instanceof Error ? e.message : String(e); }
    finally {
      deleting = false;
      showDeleteConfirm = false;
    }
  }

  async function refreshTransfers(): Promise<void> {
    try { transfers = await getPhotoTransfers(); } catch { /* ignore */ }
  }

  onMount(() => {
    initTileObserver();
    const off1 = Events.On('photo-transfers:changed', (d: unknown) => {
      const arr = (d as { data?: unknown })?.data ?? d;
      if (Array.isArray(arr)) transfers = arr as PhotoTransferView[];
      else void refreshTransfers();
    });
    void refreshTransfers();
    return () => {
      try { off1(); } catch { /* ignore */ }
      stopStream();
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

  // Reselecting Photos refetches only a stale listing. untrack keeps
  // entry/thumb updates from retriggering this.
  $effect(() => {
    if (active && paired) untrack(() => void ensureFresh());
  });
  // A different phone orphanages the cached listing and thumbnails.
  $effect(() => {
    const key = peerKey;
    untrack(() => {
      if (key !== prevPeerKey) {
        prevPeerKey = key;
        currentGen++;
        previewGen++;
        stopStream();
        previewId = null;
        pendingThumbQueue.length = 0;
        queuedThumbsSet.clear();
        entries = [];
        nextCursor = '';
        previewPrefetchCursor = '';
        thumbs = {};
        thumbFailed = new Set();
        selected = new Set();
        lastFetchAt = 0;
        loading = false;
        loadingMore = false;
        refreshing = false;
        needsRefresh = false;
        error = '';
        info = '';
        if (active && paired) void ensureFresh();
      }
    });
  });
</script>

<svelte:window onkeydown={onPreviewWindowKey} />

<section aria-label="Photos and videos" bind:this={paneRoot} class="anim-pane relative flex h-full min-h-0 flex-1 flex-col overflow-hidden bg-window">
  <div class="min-h-0 flex-1 overflow-y-auto px-4 py-4">
    <div class="flex flex-col gap-3">
      <ContentHeader title={filter === 'videos' ? 'Videos' : filter === 'photos' ? 'Photos' : 'Photos & Videos'} subtitle={photoCountLabel} icon={ImageIcon}>
        {#snippet actions()}
          <button type="button" onclick={() => void refresh(true)} disabled={loading} aria-label="Refresh photos and videos" title="Refresh photos and videos" class="inline-flex h-7 w-7 items-center justify-center rounded-lg bg-altrow text-secondary transition hover:brightness-95 focus-visible:outline-2 focus-visible:outline-focus active:translate-y-[1px] disabled:opacity-50">
            <RefreshCw size={14} class={loading ? 'animate-spin' : ''} />
          </button>
        {/snippet}
      </ContentHeader>

      <div role="tablist" aria-label="Library filter" class="flex items-center gap-1 px-1">
        {#each [{ id: 'all', label: 'All' }, { id: 'photos', label: 'Photos' }, { id: 'videos', label: 'Videos' }] as tab}
          <button
            type="button"
            role="tab"
            aria-selected={filter === tab.id}
            onclick={() => { filter = tab.id as LibraryFilter; }}
            class="inline-flex h-7 items-center rounded-md px-2.5 text-[12px] transition focus-visible:outline-2 focus-visible:outline-focus active:translate-y-[1px]"
            class:bg-altrow={filter === tab.id}
            class:text-label={filter === tab.id}
            class:font-medium={filter === tab.id}
            class:text-secondary={filter !== tab.id}
          >{tab.label}</button>
        {/each}
      </div>

      {#if selectedCount > 0}
        {@const selNoun = selectedHasVideo ? 'item' : 'photo'}
        <div class="flex items-center gap-1.5 border-b border-separator px-1 py-1.5">
          <span class="flex-1 truncate px-1 text-[12px] tabular-nums text-secondary">{selectedCount} selected</span>
          <button type="button" onclick={() => (selected = new Set())} title="Clear selection" class="inline-flex h-7 items-center gap-1.5 rounded-md px-2 text-[12px] text-secondary transition hover:bg-altrow hover:text-label focus-visible:outline-2 focus-visible:outline-focus active:translate-y-[1px]">
            <X size={13} /> Clear
          </button>
          <button type="button" onclick={() => showDeleteConfirm = true} title={`Delete ${selectedCount} selected ${selNoun}${selectedCount === 1 ? '' : 's'}`} class="inline-flex h-7 items-center gap-1.5 rounded-md bg-bad px-2.5 text-[12px] font-medium text-destructive-text transition hover:brightness-95 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus active:translate-y-[1px]">
            <Trash2 size={13} /> Delete{#if selectedCount > 1}&nbsp;({selectedCount}){/if}
          </button>
          <button type="button" onclick={() => void downloadSelected()} title={`Download ${selectedCount} selected ${selNoun}${selectedCount === 1 ? '' : 's'}`} class="inline-flex h-7 items-center gap-1.5 rounded-md bg-accent px-3 text-[13px] font-medium text-accent-text transition hover:brightness-95 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus active:translate-y-[1px]">
            <Download size={13} /> Download{#if selectedCount > 1}&nbsp;({selectedCount}){/if}
          </button>
        </div>
      {/if}

      {#if error}
        <div role="alert" class="anim-row flex items-start gap-2 px-1 py-2">
          <span class="mt-1 h-1.5 w-1.5 shrink-0 rounded-full bg-bad" aria-hidden="true"></span>
          <p class="flex-1 text-[12px] leading-snug text-label">{error}</p>
          <button type="button" onclick={() => error = ''} class="shrink-0 text-[11px] text-tertiary hover:text-label">Dismiss</button>
        </div>
      {/if}
      {#if info && !error}
        <p class="px-1 py-1 text-[12px] text-secondary" role="status">{info}</p>
      {/if}
    {#if isUpdateRequired}
      <div class="anim-row mx-auto flex max-w-[420px] flex-col items-center px-6 py-16 text-center" role="status">
        <RefreshCw size={22} class="text-tertiary" aria-hidden="true" />
        <h3 class="mt-3 text-[13px] font-semibold text-label">Phone needs an update</h3>
        <p class="mt-1 max-w-[34ch] text-[12px] leading-relaxed text-secondary">Update FuseItAll on Android to {updateNotice?.RequiredVersion || '0.7.0'} (build {updateNotice?.RequiredBuild ?? 7} or newer) to browse photos.</p>
      </div>
    {:else if isPermissionError}
      <div class="anim-row mx-auto flex max-w-[420px] flex-col items-center px-6 py-16 text-center" role="status">
        <Check size={22} class="text-tertiary" aria-hidden="true" />
        <h3 class="mt-3 text-[13px] font-semibold text-label">Photos access needed</h3>
        <p class="mt-1 max-w-[36ch] text-[12px] leading-relaxed text-secondary">The phone is sharing no images. Allow photo access so the Mac can show the library.</p>
        <p class="mt-2 max-w-[38ch] px-1 py-2 text-[11px] leading-relaxed text-secondary">On the phone: Settings, then Apps, then FuseItAll, then Permissions, then Photos. Allow images and video. Limited access shows only the photos you selected.</p>
        <button type="button" onclick={() => void refresh(true)} class="mt-4 inline-flex h-7 items-center rounded-md bg-accent px-3 text-[13px] font-medium text-accent-text transition hover:brightness-95 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus active:translate-y-[1px]">Retry</button>
      </div>
    {:else if loading || (refreshing && !entries.length)}
      <div aria-label="Loading photos and videos">
        {#each ['June 2026', 'May 2026'] as label, gi}
          <div>
            <div class="px-0.5 py-1.5">
              <div class="h-3 w-28 rounded bg-altrow"></div>
            </div>
            <div class="grid grid-cols-[repeat(auto-fill,minmax(104px,1fr))] gap-1">
              {#each Array(8) as _, i}
                <div class="anim-skel aspect-square rounded-md border border-separator bg-altrow" style="--i: {(gi * 8 + i) % 8}"></div>
              {/each}
            </div>
          </div>
        {/each}
      </div>
    {:else if !paired && !entries.length}
      <div class="anim-row mx-auto flex max-w-[420px] flex-col items-center px-6 py-16 text-center" role="status">
        <Download size={22} class="text-tertiary" aria-hidden="true" />
        <p class="mt-3 text-[13px] font-medium text-label">Phone offline</p>
        <p class="mt-1 max-w-[32ch] text-[12px] leading-relaxed text-secondary">Reconnect the phone to load photos and videos.</p>
      </div>
    {:else if !entries.length}
      <div class="anim-row mx-auto flex max-w-[420px] flex-col items-center px-6 py-16 text-center">
        <Download size={22} class="text-tertiary" aria-hidden="true" />
        <p class="mt-3 text-[13px] font-medium text-label">No photos yet</p>
        <p class="mt-1 max-w-[32ch] text-[12px] leading-relaxed text-secondary">Photos and videos from the phone library will appear here once the phone shares them.</p>
        <button type="button" onclick={() => void refresh(true)} disabled={!paired} class="mt-4 inline-flex h-7 items-center rounded-md border border-separator bg-window px-3 text-[12px] text-label transition hover:bg-altrow focus-visible:outline-2 focus-visible:outline-focus active:translate-y-[1px] disabled:opacity-50">Refresh</button>
      </div>
    {:else if !visibleEntries.length}
      <div class="anim-row mx-auto flex max-w-[420px] flex-col items-center px-6 py-16 text-center">
        <Download size={22} class="text-tertiary" aria-hidden="true" />
        <p class="mt-3 text-[13px] font-medium text-label">{filter === 'videos' ? 'No videos yet' : 'No photos yet'}</p>
        <p class="mt-1 max-w-[32ch] text-[12px] leading-relaxed text-secondary">{filter === 'videos' ? 'Videos from the phone library will appear here.' : 'Photos from the phone library will appear here.'}</p>
      </div>
    {:else}
      {#each groups as g, gi (g.label)}
        <section aria-label={g.label} style="--i: {Math.min(gi, 4)}" class="anim-row">
          <h3 class="px-0.5 py-1.5 text-[13px] font-semibold text-label">{g.label}</h3>
          <div class="photo-grid grid grid-cols-[repeat(auto-fill,minmax(104px,1fr))] gap-1">
            {#each g.items as item (item.photo_id)}
              {@const itemIsVideo = isVideoEntry(item)}
              <button
                class="photo-tile"
                data-menu="photo"
                data-photo-name={`${itemIsVideo ? 'Video' : 'Photo'} - ${g.label}`}
                class:selected={selected.has(item.photo_id)}
                onclick={(ev) => onPhotoClick(ev, item.photo_id)}
                aria-label={`${itemIsVideo ? 'Video' : 'Photo'} from ${g.label}${itemIsVideo && item.duration_ms ? `, ${formatDuration(item.duration_ms)}` : ''}`}
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
                {#if itemIsVideo}
                  <span class="photo-video-badge" aria-hidden="true">
                    <Play size={11} strokeWidth={2.5} />
                    {#if item.duration_ms}<span class="tabular-nums">{formatDuration(item.duration_ms)}</span>{/if}
                  </span>
                {/if}
                <span
                  class="photo-check"
                  role="checkbox"
                  tabindex={0}
                  aria-checked={selected.has(item.photo_id)}
                  aria-label={selected.has(item.photo_id) ? `Deselect ${itemIsVideo ? 'video' : 'photo'}` : `Select ${itemIsVideo ? 'video' : 'photo'}`}
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
            <span class="text-[11px] text-secondary">Loading more…</span>
          {:else}
            <span class="text-[11px] text-tertiary">Scroll for more</span>
          {/if}
        </div>
      {/if}
    {/if}
    </div>
  </div>

  <!-- status line: full-width static strip (h-30), same geometry as Files.
       Always rendered; only the text swaps, never the layout. -->
  <div class="flex h-[30px] shrink-0 items-center gap-2 overflow-hidden border-t border-separator bg-control px-3" role="status" aria-live="polite">
    {#if runningDownloads.length}
      <span class="h-1.5 w-24 shrink-0 overflow-hidden rounded bg-grid" aria-hidden="true">
        <span class="block h-full origin-left bg-accent transition-transform duration-200 ease-linear" style="transform: scaleX({(runningDownloads[0]?.progress ?? 0) / 100})"></span>
      </span>
      <span class="min-w-0 flex-1 truncate text-[11px] tabular-nums text-secondary">Downloading {runningDownloads.length} item{runningDownloads.length === 1 ? '' : 's'} · {runningDownloads[0]?.progress ?? 0}%</span>
      <button type="button" onclick={() => void cancelPhotoTransfer(runningDownloads[0].id)} class="shrink-0 text-[11px] text-bad hover:underline">Cancel</button>
    {:else if loadingMore}
      <span class="h-3 w-3 shrink-0 animate-spin rounded-full border-2 border-accent border-t-transparent" aria-hidden="true"></span>
      <span class="truncate text-[11px] text-secondary">Loading more…</span>
    {:else if loading}
      <span class="h-3 w-3 shrink-0 animate-spin rounded-full border-2 border-accent border-t-transparent" aria-hidden="true"></span>
      <span class="truncate text-[11px] text-secondary">Loading library…</span>
    {:else if refreshing}
      <span class="h-3 w-3 shrink-0 animate-spin rounded-full border-2 border-accent border-t-transparent" aria-hidden="true"></span>
      <span class="truncate text-[11px] text-secondary">Refreshing library…</span>
    {:else if !paired}
      <span class="h-1.5 w-1.5 shrink-0 rounded-full bg-warn" aria-hidden="true"></span>
      <span class="truncate text-[11px] text-tertiary">Phone offline · showing cached items</span>
    {:else}
      <span class="h-1.5 w-1.5 shrink-0 rounded-full bg-tertiary" aria-hidden="true"></span>
      <span class="truncate text-[11px] text-tertiary">{visibleEntries.length} {kindNoun}{visibleEntries.length === 1 ? '' : 's'}{nextCursor ? ' · scroll for more' : ' · up to date'}</span>
    {/if}
  </div>

  {#if previewEntry}
    {@const isFirst = previewIndex <= 0}
    {@const isLast = previewIndex >= visibleEntries.length - 1}
    {@const isSel = selected.has(previewEntry.photo_id)}
    {@const dims = previewEntry.width && previewEntry.height ? `${previewEntry.width} × ${previewEntry.height}` : ''}
    {@const typeShort = mimeShort(previewEntry.mime)}
    {@const sizeLabel = fmtBytes(previewEntry.size)}
    {@const durLabel = formatDuration(previewEntry.duration_ms)}
    {@const previewNoun = previewIsVideo ? 'video' : 'photo'}
    <div
      class="photo-modal"
      role="dialog"
      aria-modal="true"
      aria-label={`${previewIsVideo ? 'Video' : 'Photo'} preview. Arrow keys move between items, Escape or clicking the background closes.`}
      transition:fade={{ duration: 150 }}
      onclick={() => closePreview()}
    >
      <div class="photo-viewer" role="presentation" transition:scale={{ duration: 200, start: 0.96, opacity: 0 }} onclick={(e) => e.stopPropagation()}>
        <div
          class="photo-stage"
          onclick={(e) => {
            // The full-bleed viewer covers the modal backdrop, so the dim
            // itself is unreachable: treat empty stage void as the dim.
            const t = e.target as HTMLElement | null;
            if (t && t.closest('button, img, video')) return;
            closePreview();
          }}
        >
          <button
            type="button"
            onclick={() => stepPreview(-1)}
            disabled={isFirst}
            aria-label="Previous item"
            title="Previous item (←)"
            class="photo-nav"
          ><ChevronLeft size={24} /></button>
          <div class="photo-well">
                        {#key previewEntry.photo_id}
              {#if previewIsVideo}
                <div class="photo-frame photo-frame-video">
                  {#if streamUrl && !streamError}
                    <!-- svelte-ignore a11y_media_has_caption: phone camera clips carry no caption tracks; native controls expose them when present. -->
                    <video
                      src={streamUrl}
                      poster={previewB64 || undefined}
                      controls
                      preload="metadata"
                      playsinline
                      transition:fade={{ duration: 150 }}
                      onerror={() => { streamError = 'This video would not play. Download it instead.'; }}
                    ></video>
                  {:else if streamError}
                    <div class="photo-stream-error" role="alert">
                      <p>{streamError}</p>
                      <button
                        type="button"
                        onclick={() => previewEntry && void downloadOne(previewEntry.photo_id)}
                        class="photo-side-download"
                      ><Download size={14} /> Download video</button>
                    </div>
                  {:else}
                    <span class="photo-loading" role="status" aria-label="Loading video"><span class="spinner" aria-hidden="true"></span><span>Loading video…</span></span>
                  {/if}
                  <button
                    type="button"
                    onclick={() => previewEntry && toggleSelect(previewEntry.photo_id)}
                    aria-pressed={isSel}
                    aria-label={isSel ? 'Deselect video' : 'Select video'}
                    title="Select video (Space)"
                    class="photo-select-badge"
                    class:on={isSel}
                  ><Check size={14} strokeWidth={3} /></button>
                </div>
              {:else if previewB64}
                <div class="photo-frame">
                  <img src={previewB64} alt="" draggable="false" transition:fade={{ duration: 150 }} />
                  <button
                    type="button"
                    onclick={() => previewEntry && toggleSelect(previewEntry.photo_id)}
                    aria-pressed={isSel}
                    aria-label={isSel ? 'Deselect photo' : 'Select photo'}
                    title="Select photo (Space)"
                    class="photo-select-badge"
                    class:on={isSel}
                  ><Check size={14} strokeWidth={3} /></button>
                </div>
              {:else}
                <span class="photo-loading" role="status" aria-label="Loading photo"><span class="spinner" aria-hidden="true"></span><span>Loading…</span></span>
              {/if}
            {/key}

          </div>
          <button
            type="button"
            onclick={() => stepPreview(1)}
            disabled={isLast}
            aria-label="Next item"
            title="Next item (→)"
            class="photo-nav"
          ><ChevronRight size={24} /></button>
        </div>
        <aside class="photo-side" aria-label={`${previewIsVideo ? 'Video' : 'Photo'} details`}>
          <p class="photo-side-count">{previewIndex + 1} of {visibleEntries.length}{isSel ? ' · Selected' : ''}</p>
          <dl class="photo-side-rows">
            {#if previewIsVideo && durLabel}
              <div class="photo-side-row"><dt>Duration</dt><dd class="tabular-nums">{durLabel}</dd></div>
            {/if}
            {#if dims}
              <div class="photo-side-row"><dt>Dimensions</dt><dd>{dims}</dd></div>
            {/if}
            {#if typeShort}
              <div class="photo-side-row"><dt>Type</dt><dd>{typeShort}</dd></div>
            {/if}
            {#if sizeLabel}
              <div class="photo-side-row"><dt>Size</dt><dd>{sizeLabel}</dd></div>
            {/if}
          </dl>
          <span class="photo-side-spacer"></span>
          <button
            type="button"
            onclick={() => previewEntry && void downloadOne(previewEntry.photo_id)}
            title={`Download this ${previewNoun}`}
            class="photo-side-download"
          ><Download size={14} /> Download</button>
        </aside>
      </div>
    </div>
  {/if}

  {#if showDeleteConfirm}
    {@const delNoun = selectedHasVideo ? 'item' : 'photo'}
    <div class="fixed inset-0 z-40 flex items-center justify-center bg-black/30 p-4" transition:fade={{ duration: 150 }} onclick={() => { if (!deleting) showDeleteConfirm = false; }} onkeydown={(e) => { if (e.key === 'Escape' && !deleting) showDeleteConfirm = false; }} role="presentation">
      <div role="dialog" aria-modal="true" aria-label={`Delete ${delNoun}s`} transition:scale={{ duration: 180, start: 0.96, opacity: 0 }} class="w-full max-w-[380px] rounded-[12px] border border-separator bg-control p-4 shadow-xl" onclick={(e) => e.stopPropagation()}>
        <div class="flex items-start gap-3">
          <span class="flex h-9 w-9 shrink-0 items-center justify-center rounded-full bg-bad/15 text-bad" aria-hidden="true"><Trash2 size={16} /></span>
          <div class="min-w-0">
            <h3 class="text-[13px] font-semibold text-label">Delete {selectedCount} {delNoun}{selectedCount === 1 ? '' : 's'}?</h3>
            <p class="mt-1 text-[12px] leading-snug text-secondary">This removes them from the phone. This cannot be undone.</p>
          </div>
        </div>
        <div class="mt-4 flex justify-end gap-2">
          <button type="button" onclick={() => showDeleteConfirm = false} disabled={deleting} class="h-7 rounded-md border border-separator bg-window px-3 text-[13px] text-label transition hover:bg-altrow focus-visible:outline-2 focus-visible:outline-focus active:translate-y-[1px] disabled:opacity-50">Keep</button>
          <button type="button" onclick={() => void deleteSelected()} disabled={deleting} class="inline-flex h-7 items-center gap-1.5 rounded-md bg-bad px-3 text-[13px] font-medium text-destructive-text transition hover:brightness-95 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus active:translate-y-[1px] disabled:opacity-50">
            {#if deleting}<span class="spinner" aria-hidden="true"></span><span>Deleting…</span>{:else}<span>Delete</span>{/if}
          </button>
        </div>
      </div>
    </div>
  {/if}
</section>

<style>
  .photo-tile { position: relative; aspect-ratio: 1; overflow: hidden; border-radius: 6px; background: var(--alt-row-bg); transition: transform 0.12s ease, box-shadow 0.12s ease; }
  .photo-tile:hover { transform: translateY(-1px); box-shadow: 0 4px 14px rgba(0, 0, 0, 0.12); }
  .photo-tile:focus-visible { outline: none; filter: brightness(0.96); }
  .photo-tile img { width: 100%; height: 100%; object-fit: cover; display: block; transition: transform 0.25s cubic-bezier(0.16, 1, 0.3, 1); }
  .photo-tile:hover img { transform: scale(1.045); }
  .photo-tile.selected, .photo-tile.selected:hover { box-shadow: 0 0 0 2px var(--accent); }
  .photo-tile:active { transform: scale(0.97); }
  /* Gentle select confirmation: a short settle with no overshoot. The
     shared fi-badge spring is deliberately not used here — its boing
     reads as jumpy on a 26px badge. */
  @keyframes fi-check-pop {
    from { transform: scale(0.9); }
    to { transform: scale(1); }
  }
  .photo-tile.selected .photo-check { animation: fi-check-pop 0.14s ease-out; }
  .photo-skeleton { display: block; width: 100%; height: 100%; background: var(--alt-row-bg); }
  .photo-fallback { display: flex; align-items: center; justify-content: center; height: 100%; font-size: 12px; color: var(--secondary-label); }
  /* Video badge: duration + play glyph, bottom-left over the thumb.
     Opaque fill, never glass, with a text label (never color alone). */
  .photo-video-badge { position: absolute; left: 8px; bottom: 8px; display: inline-flex; align-items: center; gap: 4px; max-width: calc(100% - 16px); padding: 3px 7px; border-radius: 9999px; background: rgb(0 0 0 / 0.68); color: #fff; font-size: 11px; font-weight: 600; line-height: 1.2; }
  .photo-check { position: absolute; top: 8px; right: 8px; width: 24px; height: 24px; border-radius: 9999px; display: flex; align-items: center; justify-content: center; background: color-mix(in srgb, var(--window-bg) 82%, transparent); color: transparent; transition: transform 0.1s ease; }
  .photo-tile:hover .photo-check, .photo-tile:focus-visible .photo-check, .photo-tile.selected .photo-check { color: var(--secondary-label); }
  .photo-check:hover { transform: scale(1.08); }
  .photo-tile.selected .photo-check { background: var(--accent); color: white; }
  .photo-modal { position: fixed; inset: 0; z-index: 50; background: rgb(0 0 0 / 0.72); }
  .photo-modal:focus { outline: none; }
  /* Full-bleed native split: dimmed grid behind, black media canvas,
     opaque inspector sidebar in system materials. No floating box. */
  .photo-viewer { width: 100%; height: 100%; min-height: 0; display: flex; align-items: stretch; }
  .photo-stage { flex: 1 1 auto; min-width: 0; min-height: 0; display: flex; align-items: center; gap: 4px; }
  .photo-side { flex: none; width: 248px; background-color: var(--sidebar-bg); padding: 12px 16px 16px; display: flex; flex-direction: column; min-height: 0; overflow-y: auto; }
  .photo-side-count { font-size: 12px; color: var(--secondary-label); font-variant-numeric: tabular-nums; }
  .photo-side-rows { margin-top: 10px; }
  .photo-side-row { display: flex; align-items: baseline; justify-content: space-between; gap: 12px; padding: 8px 0; }
  .photo-side-row dt { flex: none; font-size: 11px; color: var(--secondary-label); }
  .photo-side-row dd { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 12px; color: var(--label); }
  .photo-side-spacer { flex: 1 1 auto; }
  .photo-side-download { flex: none; width: 100%; display: inline-flex; align-items: center; justify-content: center; gap: 6px; height: 32px; margin-top: 12px; border-radius: 8px; background: var(--accent); color: var(--accent-text); font-size: 13px; font-weight: 600; transition: filter 0.12s ease, transform 0.12s ease; }
  .photo-side-download:hover { filter: brightness(1.08); }
  .photo-side-download:active { transform: translateY(1px); }
  /* Tiny select checkbox overlaid on the photo's top-right corner. */
  .photo-select-badge { position: absolute; top: 10px; right: 10px; width: 26px; height: 26px; border-radius: 50%; display: flex; align-items: center; justify-content: center; background: rgba(0, 0, 0, 0.45); border: 1.5px solid rgba(255, 255, 255, 0.6); color: transparent; transition: background-color 0.15s ease, border-color 0.15s ease, color 0.15s ease, transform 0.1s ease; }
  .photo-select-badge:hover { border-color: #fff; transform: scale(1.08); }
  .photo-select-badge.on { background: var(--accent); border-color: transparent; color: var(--accent-text); animation: fi-check-pop 0.14s ease-out; }
  .photo-well { flex: 1 1 auto; min-width: 0; height: 100%; min-height: 0; display: flex; align-items: center; justify-content: center; }
  /* Shrink-to-fit frame around the rendered image, so overlays (the
     select badge) anchor to the photo itself, not the stage void. */
  .photo-frame { position: relative; display: flex; margin: auto; max-width: 100%; max-height: 100%; min-width: 0; min-height: 0; line-height: 0; }
  .photo-frame img { display: block; max-width: 100%; max-height: 100%; object-fit: contain; }
  /* Video frame keeps the player inside the stage on any window size. */
  .photo-frame-video { width: min(100%, 960px); }
  .photo-frame-video video { display: block; width: 100%; max-height: 100%; background: #000; border-radius: 8px; }
  .photo-stream-error { display: flex; flex-direction: column; align-items: center; gap: 12px; max-width: 320px; margin: auto; line-height: 1.4; }
  .photo-stream-error p { font-size: 12px; color: rgba(255, 255, 255, 0.85); text-align: center; }
  /* Explicit loading state: a dark-on-dark skeleton was invisible, so
     the viewer showed a bare black box while the photo traveled. */
  .photo-loading { display: flex; align-items: center; gap: 8px; font-size: 12px; color: rgba(255, 255, 255, 0.6); }
  .photo-loading .spinner { width: 14px; height: 14px; border: 2px solid rgba(255, 255, 255, 0.25); border-top-color: #fff; }
  .photo-nav { flex: none; width: 40px; height: 64px; border-radius: 10px; display: flex; align-items: center; justify-content: center; color: rgba(255, 255, 255, 0.8); transition: background-color 0.12s ease, color 0.12s ease, transform 0.1s ease; }
  .photo-nav:hover:not(:disabled) { background: rgba(255, 255, 255, 0.14); color: #fff; }
  .photo-nav:active:not(:disabled) { transform: scale(0.94); }
  .photo-nav:disabled { opacity: 0.25; cursor: default; }

  /* Narrow windows: inspector stacks below the photo, which keeps a
     guaranteed minimum height so it can never collapse to a void. */
  @media (max-width: 620px) {
    .photo-viewer { flex-direction: column; }
    .photo-stage { min-height: 38vh; }
    .photo-side { width: auto; border-left: 0; border-top: 1px solid var(--separator); padding: 10px 12px 12px; }
    .photo-side-rows { display: flex; gap: 16px; margin-top: 6px; }
    .photo-side-row { border-top: 0; padding: 0; gap: 6px; }
  }

  @media (prefers-reduced-transparency: reduce) {
    .photo-modal { background: #000; }
  }

  @media (prefers-reduced-motion: reduce) {
    .photo-tile, .photo-check, .photo-tile img { transition: none; }
    .photo-tile:hover { transform: none; box-shadow: none; }
    .photo-tile:hover img { transform: none; }
    .photo-tile:active { transform: none; }
    .photo-tile.selected .photo-check, .photo-select-badge.on { animation: none; }
  }
</style>
