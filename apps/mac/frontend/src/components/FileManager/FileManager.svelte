<!--
  Copyright (C) 2026 FuseItAll contributors.

  This program is free software: you can redistribute it and/or modify
  it under the terms of the GNU Affero General Public License as published
  by the Free Software Foundation, version 3 of the License. See LICENSE
  for details.

  File Manager — Finder-grade browser for Android storage.
  HIG Read: primary window detail pane for file browsing, following HIG
  Windows, Toolbars, Sidebars/Split Views, Color, Typography, Buttons,
  Progress, Context Menus. Classic frost, no Liquid Glass.
-->
<script lang="ts">
  import { onMount } from 'svelte';
  import { Folder, File as FileIcon, ArrowUp, Search, Upload, Trash2, Download, FolderPlus, RefreshCw, HardDrive } from '@lucide/svelte';
  import type { FileEntryView, FileListResult, FileTransferView } from '../../backend';
  import { listPhoneFiles, mkdirPhone, deletePhone, requestPhoneFile, getTransfers, cancelTransfer, uploadLocalFiles, uploadBrowserFile } from '../../backend';

  interface Props { paired: boolean }
  let { paired }: Props = $props();

  let path = $state('');
  let entries = $state<FileEntryView[]>([]);
  let query = $state('');
  let loading = $state(false);
  let error = $state('');
  let info = $state('');
  let dragOver = $state(false);
  let newFolder = $state('');
  let selected = $state<string | null>(null);
  let transfers = $state<FileTransferView[]>([]);
  let showTransfers = $state(false);
  let searching = $state(false);

  const favorites = [
    { label: 'Phone', path: '', icon: HardDrive },
    { label: 'DCIM', path: 'DCIM' },
    { label: 'Pictures', path: 'Pictures' },
    { label: 'Download', path: 'Download' },
    { label: 'Documents', path: 'Documents' },
    { label: 'Music', path: 'Music' },
  ];

  function fmtSize(n: number): string {
    if (!n) return '—';
    if (n < 1024) return `${n} B`;
    if (n < 1 << 20) return `${(n / 1024).toFixed(1)} KB`;
    if (n < 1 << 30) return `${(n / (1 << 20)).toFixed(1)} MB`;
    return `${(n / (1 << 30)).toFixed(2)} GB`;
  }
  function fmtTime(ts: number): string {
    if (!ts) return '';
    try { return new Date(ts * 1000).toLocaleDateString(undefined, { month: 'short', day: 'numeric', year: 'numeric' }); } catch { return ''; }
  }
  let breadcrumbs = $derived(path ? path.split('/').filter(Boolean) : []);
  let crumbs = $derived([{ label: 'Phone', path: '' }, ...breadcrumbs.map((s, i) => ({ label: s, path: breadcrumbs.slice(0, i + 1).join('/') }))]);
  let filtered = $derived(query.trim() ? entries.filter(e => e.name.toLowerCase().includes(query.toLowerCase())) : entries);
  let isPermissionError = $derived(error.includes('All files access') || error.includes('permission'));

  async function refresh(): Promise<void> {
    if (!paired) return;
    loading = true; error = '';
    try {
      const res: FileListResult = await listPhoneFiles(path);
      if (res.error) throw new Error(res.error);
      entries = res.entries ?? [];
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
      // keep stale entries visible until next success
    } finally { loading = false; }
  }
  async function pollTransfers(): Promise<void> { try { transfers = await getTransfers(); } catch {} }

  function go(p: string) { path = p; selected = null; query = ''; void refresh(); }
  function up() { if (!path) return; path = path.split('/').slice(0, -1).join('/'); selected = null; void refresh(); }
  function enter(p: string, dir: boolean) { if (dir) go(p); }

  function isValidMkdirName(s: string): boolean {
    const t = s.trim();
    if (!t || t.length > 255) return false;
    if (t === '.' || t === '..') return false;
    if (t.includes('/') || t.includes('\\') || t.includes('\x00')) return false;
    for (const ch of t) { const c = ch.charCodeAt(0); if (c < 0x20 || c === 0x7f) return false; }
    return true;
  }
  async function doMkdir() {
    const name = newFolder.trim();
    if (!name) return;
    if (!isValidMkdirName(name)) { error = 'Folder name cannot contain /, \\, control characters or be . / ..'; return; }
    const full = path ? `${path}/${name}` : name;
    try { info = await mkdirPhone(full); newFolder = ''; await refresh(); } catch (e) { error = e instanceof Error ? e.message : String(e); }
  }
  async function doDelete() {
    if (!selected) return;
    try { info = await deletePhone(selected); selected = null; await refresh(); } catch (e) { error = e instanceof Error ? e.message : String(e); }
  }
  async function doDownload() {
    if (!selected) return;
    const t = entries.find(e => e.path === selected);
    if (t?.is_dir) { error = 'Select a file to download.'; return; }
    try { await requestPhoneFile(selected, ''); info = `Downloading ${t?.name ?? selected}…`; showTransfers = true; const id = setInterval(() => void pollTransfers(), 600); setTimeout(() => clearInterval(id), 9000); } catch (e) { error = e instanceof Error ? e.message : String(e); }
  }

  function onDragOver(e: DragEvent) { e.preventDefault(); dragOver = true; if (e.dataTransfer) e.dataTransfer.dropEffect = 'copy'; }
  function onDragLeave() { dragOver = false; }
  async function uploadFiles(files: globalThis.FileList | globalThis.File[]) {
    if (!paired) { error = 'Phone offline — reconnect.'; return; }
    let ok = 0;
    for (const f of Array.from(files)) {
      const any = f as unknown as Record<string, unknown>;
      const p = (any['path'] as string) || '';
      try {
        if (p) await uploadLocalFiles([p], path);
        else {
          const b64 = await new Promise<string>((res, rej) => {
            const r = new FileReader();
            r.onload = () => { const v = r.result as string; const i = v.indexOf(','); res(i >= 0 ? v.slice(i + 1) : v); };
            r.onerror = () => rej(r.error);
            r.readAsDataURL(f);
          });
          const safe = f.name.replace(/[^A-Za-z0-9._-]/g, '_').slice(0, 255) || 'file';
          await uploadBrowserFile(b64, safe, path);
        }
        ok++;
      } catch (e) { error = e instanceof Error ? e.message : String(e); return; }
    }
    if (ok) { info = `Uploaded ${ok} file${ok > 1 ? 's' : ''} to ${path || 'Phone'}`; await refresh(); }
  }
  async function onDrop(e: DragEvent) { e.preventDefault(); dragOver = false; const fs = e.dataTransfer?.files; if (!fs?.length) return; await uploadFiles(fs); }
  function onRowDragStart(e: DragEvent, en: FileEntryView) { if (en.is_dir) { e.preventDefault(); return; } e.dataTransfer?.setData('text/plain', en.path); if (e.dataTransfer) e.dataTransfer.effectAllowed = 'copy'; }

  onMount(() => {
    if (paired) void refresh();
    const t = setInterval(() => paired && void pollTransfers(), 1800);
    const h = (ev: Event) => { const d = (ev as CustomEvent).detail as { paths?: string[] }; const ps = d?.paths ?? []; if (ps.length) void uploadLocalFiles(ps, path).then(() => refresh()).catch((e: unknown) => error = e instanceof Error ? e.message : String(e)); };
    window.addEventListener('wails:file-drop' as unknown as string, h as EventListener);
    return () => { clearInterval(t); window.removeEventListener('wails:file-drop' as unknown as string, h as EventListener); };
  });
  $effect(() => { if (paired) void refresh(); });
</script>

<section aria-label="Files" class="flex min-h-[480px] flex-1 flex-col overflow-hidden rounded-[12px] border border-separator bg-window">
  <!-- toolbar: frost-bar 48, leading nav + center crumb/search + trailing prominent -->
  <div class="frost-bar flex h-[48px] shrink-0 items-center gap-2 border-b border-separator px-3">
    <button type="button" onclick={up} disabled={!path} title="Go up" class="inline-flex h-7 w-7 items-center justify-center rounded-md border border-separator bg-control text-label transition hover:bg-altrow active:translate-y-[1px] disabled:opacity-40">
      <ArrowUp size={14} />
    </button>
    <div class="mx-1 h-5 w-px bg-separator"></div>

    <nav aria-label="Path" class="flex min-w-0 flex-1 items-center gap-0.5 overflow-hidden text-[13px]">
      {#each crumbs as c, i}
        {#if i > 0}<span class="px-0.5 text-tertiary">›</span>{/if}
        <button type="button" onclick={() => go(c.path)} class="max-w-[14ch] truncate rounded-md px-1.5 py-1 text-[13px] hover:bg-altrow {c.path === path ? 'font-semibold text-label' : 'text-secondary'}">{c.label}</button>
      {/each}
    </nav>

    <div class="relative hidden items-center md:flex">
      <Search size={13} class="pointer-events-none absolute left-2 text-tertiary" />
      <input bind:value={query} placeholder="Filter" aria-label="Filter files" class="h-7 w-[160px] rounded-md border border-separator bg-control pl-7 pr-2 text-[12px] text-label placeholder:text-tertiary focus:outline-none focus:ring-2 focus:ring-focus" />
    </div>

    <button type="button" onclick={() => void refresh()} disabled={loading} aria-label="Refresh" class="inline-flex h-7 w-7 items-center justify-center rounded-md border border-separator bg-control text-secondary hover:text-label active:translate-y-[1px] disabled:opacity-50">
      <RefreshCw size={14} class={loading ? 'animate-spin' : ''} />
    </button>

    <label class="inline-flex h-7 cursor-pointer items-center gap-1.5 rounded-md bg-accent px-3 text-[13px] font-medium text-accent-text transition hover:brightness-95 active:translate-y-[1px]">
      <Upload size={13} />
      <span>Upload</span>
      <input type="file" multiple class="hidden" onchange={async (e) => { const el = e.currentTarget as HTMLInputElement; if (el.files?.length) await uploadFiles(el.files); el.value = ''; }} />
    </label>
  </div>

  {#if error}
    <div role="alert" class="flex items-start gap-2 border-b border-separator bg-control px-3 py-2">
      <span class="mt-0.5 h-1.5 w-1.5 shrink-0 rounded-full bg-bad"></span>
      <p class="flex-1 text-[12px] leading-snug text-label">{error}</p>
      <button type="button" onclick={() => error = ''} class="text-[11px] text-tertiary hover:text-label">Dismiss</button>
    </div>
  {/if}
  {#if info && !error}
    <p class="border-b border-grid bg-altrow px-3 py-1.5 text-[12px] text-secondary">{info}</p>
  {/if}

  <div class="flex min-h-0 flex-1">
    <!-- favorites sidebar -->
    <aside class="hidden w-[172px] shrink-0 flex-col border-r border-separator bg-sidebar md:flex">
      <p class="px-3 pb-1 pt-3 text-[10px] font-semibold uppercase tracking-[0.08em] text-tertiary">Favorites</p>
      <div class="flex-1 overflow-auto px-2 pb-2">
        {#each favorites as f}
          {#if f.path === '' || true}
            <button type="button" onclick={() => go(f.path)} class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-[13px] {path === f.path ? 'bg-accent text-accent-text' : 'text-label hover:bg-altrow'}">
              {#if f.icon}
                <!-- @ts-ignore -->
                <svelte:component this={f.icon} size={14} class={path === f.path ? 'text-accent-text' : 'text-accent'} />
              {:else}
                <Folder size={14} class={path === f.path ? 'text-accent-text' : 'text-secondary'} />
              {/if}
              <span class="truncate">{f.label}</span>
            </button>
          {/if}
        {/each}
      </div>
      <div class="border-t border-separator px-3 py-2">
        <p class="text-[10px] text-tertiary">Drag files here to upload.</p>
        <p class="text-[10px] text-tertiary">Select then Download to save to Mac.</p>
      </div>
    </aside>

    <!-- list area -->
    <div class="relative flex min-w-0 flex-1 flex-col bg-window">
      <!-- table header -->
      <div class="sticky top-0 z-0 grid grid-cols-[1fr_84px_112px] items-center gap-2 border-b border-grid bg-window px-3 py-1.5 text-[10px] font-medium uppercase tracking-wide text-secondary">
        <span>Name</span>
        <span class="text-right">Size</span>
        <span class="hidden sm:block">Modified</span>
        <span class="sm:hidden text-right">Date</span>
      </div>

      <div role="region" aria-label="File drop" ondragover={onDragOver} ondragleave={onDragLeave} ondrop={onDrop} class="relative flex-1 overflow-auto">
        {#if dragOver}
          <div class="pointer-events-none absolute inset-3 rounded-[10px] border border-accent bg-accent/10 flex items-center justify-center backdrop-blur-sm">
            <div class="rounded-full bg-accent px-3 py-1.5 text-[13px] font-medium text-accent-text shadow">Drop to upload to {path || 'Phone'}</div>
          </div>
        {/if}

        {#if loading}
          <div class="divide-y divide-grid">
            {#each Array(6) as _}
              <div class="grid grid-cols-[1fr_84px_112px] gap-2 px-3 py-2">
                <div class="h-3 rounded bg-altrow"></div>
                <div class="h-3 rounded bg-altrow"></div>
                <div class="h-3 rounded bg-altrow"></div>
              </div>
            {/each}
          </div>
        {:else if isPermissionError}
          <div class="flex flex-col items-center px-6 py-16 text-center">
            <div class="flex h-12 w-12 items-center justify-center rounded-full bg-warn/15 text-warn"><HardDrive size={22} /></div>
            <h3 class="mt-3 text-[13px] font-semibold text-label">All files access needed</h3>
            <p class="mt-1 max-w-[36ch] text-[12px] leading-relaxed text-secondary">Your phone is blocking the file list. Grant All files access so the Mac can see storage.</p>
            <p class="mt-2 max-w-[40ch] text-[11px] text-tertiary">On phone: Settings → Apps → FuseItAll → Allow access to all files. Then Refresh.</p>
            <button type="button" onclick={() => void refresh()} class="mt-4 inline-flex h-7 items-center rounded-md bg-accent px-3 text-[13px] font-medium text-accent-text hover:brightness-95">Refresh</button>
          </div>
        {:else if !filtered.length}
          <div class="flex flex-col items-center px-6 py-14 text-center">
            <div class="flex h-10 w-10 items-center justify-center rounded-full bg-altrow text-secondary"><Folder size={20} /></div>
            {#if query}
              <p class="mt-3 text-[13px] font-medium text-label">No matches</p>
              <p class="text-[12px] text-secondary">No files matching “{query}”.</p>
            {:else}
              <p class="mt-3 text-[13px] font-medium text-label">This folder is empty</p>
              <p class="mt-1 max-w-[32ch] text-[12px] text-secondary">Drag files from Finder here, or create a folder and upload.</p>
              <label class="mt-4 inline-flex cursor-pointer items-center gap-1.5 rounded-md bg-accent px-3 py-1.5 text-[13px] font-medium text-accent-text">
                Upload files
                <input type="file" multiple class="hidden" onchange={async (e) => { const el = e.currentTarget as HTMLInputElement; if (el.files?.length) await uploadFiles(el.files); el.value=''; }} />
              </label>
            {/if}
          </div>
        {:else}
          <div class="divide-y divide-grid">
            {#each filtered as e}
              <button type="button"
                draggable={!e.is_dir}
                ondragstart={(ev) => onRowDragStart(ev, e)}
                onclick={() => selected = e.path}
                ondblclick={() => enter(e.path, e.is_dir)}
                class="grid w-full grid-cols-[1fr_84px_112px] items-center gap-2 px-3 py-1.5 text-left hover:bg-altrow {selected === e.path ? 'bg-accent/15 !hover:bg-accent/15' : ''}">
                <span class="flex min-w-0 items-center gap-2">
                  <span class="flex h-6 w-6 shrink-0 items-center justify-center rounded-md {e.is_dir ? 'bg-accent/15 text-accent' : 'bg-altrow text-secondary'}">
                    {#if e.is_dir}<Folder size={13} />{:else}<FileIcon size={13} />{/if}
                  </span>
                  <span class="truncate text-[13px] {selected === e.path ? 'font-medium text-label' : 'text-label'}">{e.name}</span>
                  {#if selected === e.path}<span class="hidden text-[11px] text-tertiary sm:inline">— {e.path}</span>{/if}
                </span>
                <span class="text-right text-[12px] tabular-nums {selected === e.path ? 'text-label' : 'text-secondary'}">{e.is_dir ? '—' : fmtSize(e.size)}</span>
                <span class="text-right text-[12px] tabular-nums text-tertiary sm:text-secondary">{fmtTime(e.mod_time)}</span>
              </button>
            {/each}
          </div>
        {/if}
      </div>

      <!-- inline toolbar for actions (square buttons near target per HIG 8) -->
      <div class="flex flex-wrap items-center gap-2 border-t border-separator bg-control px-3 py-2">
        <div class="flex items-center gap-1.5">
          <input bind:value={newFolder} placeholder="New folder name" aria-label="New folder name" class="h-7 w-[168px] rounded-md border border-separator bg-window px-2 text-[12px] placeholder:text-tertiary focus:outline-none focus:ring-2 focus:ring-focus" />
          <button type="button" onclick={() => void doMkdir()} title="Create folder" class="inline-flex h-7 items-center gap-1.5 rounded-md border border-separator bg-window px-2.5 text-[12px] text-label hover:bg-altrow active:translate-y-[1px]">
            <FolderPlus size={13} /> Create
          </button>
        </div>
        <div class="mx-1 hidden h-5 w-px bg-separator sm:block"></div>
        <button type="button" onclick={() => void doDownload()} disabled={!selected} class="inline-flex h-7 items-center gap-1.5 rounded-md border border-separator bg-window px-2.5 text-[12px] text-label hover:bg-altrow active:translate-y-[1px] disabled:opacity-40">
          <Download size={13} /> Download
        </button>
        <button type="button" onclick={() => void doDelete()} disabled={!selected} class="inline-flex h-7 items-center gap-1.5 rounded-md bg-bad px-2.5 text-[12px] font-medium text-white hover:brightness-95 active:translate-y-[1px] disabled:opacity-40">
          <Trash2 size={13} /> Delete
        </button>
        <span class="flex-1"></span>
        <span class="hidden text-[11px] text-tertiary sm:inline">{filtered.length} item{filtered.length === 1 ? '' : 's'}{query ? ` matching “${query}”` : ''}</span>
      </div>
    </div>
  </div>

  <!-- bottom status / transfers (HIG 10 determinate, same place) -->
  {#if transfers.length}
    <div class="flex items-center gap-2 border-t border-separator bg-control px-3 py-1.5">
      <span class="text-[11px] font-medium text-secondary">{transfers.filter(t => t.status === 'running').length} running</span>
      <div class="flex flex-1 items-center gap-1.5 overflow-hidden">
        {#each transfers.slice(-3) as t}
          <div class="flex min-w-0 flex-1 items-center gap-1.5 rounded bg-window px-2 py-1">
            <span class="h-1.5 flex-1 overflow-hidden rounded bg-grid"><span class="block h-full bg-accent transition-all" style="width: {t.progress}%"></span></span>
            <span class="truncate text-[11px] {t.status === 'error' ? 'text-bad' : 'text-secondary'}">{t.path.split('/').pop()} {t.progress}%</span>
            {#if t.status === 'running'}<button type="button" onclick={() => void cancelTransfer(t.id).then(() => pollTransfers())} class="text-[11px] text-bad hover:underline">Cancel</button>{/if}
          </div>
        {/each}
      </div>
      <button type="button" onclick={() => showTransfers = !showTransfers} class="text-[11px] text-tertiary hover:text-label">{showTransfers ? 'Hide' : `Show all (${transfers.length})`}</button>
    </div>
  {/if}
  {#if showTransfers && transfers.length}
    <div class="max-h-[140px] overflow-auto border-t border-separator bg-window px-2 py-1">
      {#each transfers as t}
        <div class="flex items-center gap-2 px-2 py-1 text-[11px]">
          <span class="flex-1 truncate {t.status === 'error' ? 'text-bad' : 'text-label'}">{t.direction} {t.path} — {t.status}</span>
          <span class="tabular-nums text-tertiary">{t.progress}%</span>
        </div>
      {/each}
    </div>
  {/if}
</section>
