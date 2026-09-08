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
  import { Folder, File as FileIcon, ArrowUp, Search, Upload, Trash2, Download, FolderPlus, RefreshCw, HardDrive, Pencil, FolderDown } from '@lucide/svelte';
  import { Events } from '@wailsio/runtime';
  import type { FileEntryView, FileListResult, FileTransferView } from '../../backend';
  import { listPhoneFiles, mkdirPhone, deletePhone, renamePhone, requestPhoneFile, getTransfers, cancelTransfer, uploadLocalFiles, uploadBrowserFile, uploadBrowserFileWithRelPath, pickDownloadDir, startFileDrag, isFilesPermissionError } from '../../backend';
  import ContextMenu, { type MenuItem } from '../ContextMenu.svelte';

  interface Props { paired: boolean }
  let { paired }: Props = $props();

  const DRAG_LIMIT = 100 * 1024 * 1024;

  let path = $state('');
  let entries = $state<FileEntryView[]>([]);
  let query = $state('');
  let loading = $state(false);
  let error = $state('');
  let info = $state('');
  let dragOver = $state(false);
  let dropTarget = $state<string | null>(null);
  let newFolder = $state('');
  let selected = $state<string | null>(null);
  let transfers = $state<FileTransferView[]>([]);
  let showTransfers = $state(false);
  let menuState = $state<{ x: number; y: number; items: MenuItem[]; onPick: (id: string) => void } | null>(null);
  let renameTarget = $state<string | null>(null);
  let renameValue = $state('');
  let deleteTarget = $state<string | null>(null);
  let pendingUpload = $state(false);

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
  function fmtSizeShort(n: number): string {
    if (n < 1 << 20) return `${(n / 1024).toFixed(0)} KB`;
    return `${(n / (1 << 20)).toFixed(1)} MB`;
  }
  let breadcrumbs = $derived(path ? path.split('/').filter(Boolean) : []);
  let crumbs = $derived([{ label: 'Phone', path: '' }, ...breadcrumbs.map((s, i) => ({ label: s, path: breadcrumbs.slice(0, i + 1).join('/') }))]);
  let filtered = $derived(query.trim() ? entries.filter(e => e.name.toLowerCase().includes(query.toLowerCase())) : entries);
  let lastResult = $state<FileListResult | null>(null);
  let isPermissionError = $derived(
    lastResult ? isFilesPermissionError(lastResult, error) : (error.includes('All files access') || error.toLowerCase().includes('all files')),
  );
  let activeTransfers = $derived(transfers.filter(t => t.status === 'running'));
  let recentTransfers = $derived(transfers.slice().sort((a,b) => b.progress - a.progress));

  async function refresh(): Promise<void> {
    if (!paired) return;
    loading = true; error = ''; lastResult = null;
    try {
      const res: FileListResult = await listPhoneFiles(path);
      lastResult = res;
      if (res.error) throw new Error(res.error);
      entries = res.entries ?? [];
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    } finally { loading = false; }
  }
  const dismissedTransferIds = new Set<string>();

  function applyTransfers(next: FileTransferView[]): void {
    transfers = next.filter(t => !dismissedTransferIds.has(t.id));
    for (const t of next) {
      if (t.status === 'done' || t.status === 'error') {
        if (!dismissedTransferIds.has(t.id)) {
          setTimeout(() => {
            dismissedTransferIds.add(t.id);
            transfers = transfers.filter(x => x.id !== t.id);
            if (!transfers.some(x => x.status === 'running')) {
              showTransfers = false;
            }
          }, 3000);
        }
      }
    }
  }

  async function pollTransfers(): Promise<void> {
    try {
      const next = await getTransfers();
      applyTransfers(next);
    } catch {}
  }

  function ensurePolling(): void {
    void pollTransfers();
  }

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
    try { info = await mkdirPhone(full); newFolder = ''; await refresh(); setTimeout(() => info='', 2500); } catch (e) { error = e instanceof Error ? e.message : String(e); }
  }
  async function doDelete(target?: string) {
    const p = target ?? selected;
    if (!p) return;
    try { info = await deletePhone(p); selected = null; deleteTarget = null; await refresh(); setTimeout(()=> info='',2500);} catch (e) { error = e instanceof Error ? e.message : String(e); }
  }
  async function doDownload(target?: string, toDir?: string) {
    const p = target ?? selected;
    if (!p) return;
    const t = entries.find(e => e.path === p);
    try {
      const dir = toDir ?? '';
      await requestPhoneFile(p, dir);
      info = `Downloading ${t?.name ?? p}…`;
      showTransfers = true;
      ensurePolling();
      setTimeout(() => { if (!activeTransfers.length) info=''; }, 3500);
    } catch (e) { error = e instanceof Error ? e.message : String(e); }
  }
  async function doDownloadTo(target?: string) {
    const p = target ?? selected;
    if (!p) return;
    let dir = '';
    try {
      const picked = await pickDownloadDir();
      if (picked && picked !== '') dir = picked;
    } catch {}
    const custom = window.prompt('Save to folder on Mac (leave empty for Downloads):', dir);
    if (custom === null) return;
    await doDownload(p, custom.trim());
  }
  function startRename(target: string) {
    const base = target.split('/').pop() ?? target;
    renameTarget = target;
    renameValue = base;
  }
  async function confirmRename() {
    if (!renameTarget) return;
    const toName = renameValue.trim();
    if (!isValidMkdirName(toName)) { error = 'Name cannot contain /, \\, control characters or be . / ..'; return; }
    const dir = renameTarget.includes('/') ? renameTarget.slice(0, renameTarget.lastIndexOf('/')) : '';
    const to = dir ? `${dir}/${toName}` : toName;
    if (to === renameTarget) { renameTarget = null; return; }
    try { await renamePhone(renameTarget, to); renameTarget=null; await refresh(); info='Renamed.'; setTimeout(()=>info='',2500);} catch(e){ error = e instanceof Error ? e.message : String(e); }
  }

  // ---- drag helpers ----
  type Collected = { file: File; relPath: string; localPath?: string };

  function getMime(name: string): string {
    const ext = name.split('.').pop()?.toLowerCase() ?? '';
    if (ext==='png') return 'image/png';
    if (ext==='jpg'||ext==='jpeg') return 'image/jpeg';
    if (ext==='gif') return 'image/gif';
    if (ext==='webp') return 'image/webp';
    if (ext==='pdf') return 'application/pdf';
    return 'application/octet-stream';
  }

  async function collectFromItems(dt: DataTransfer): Promise<Collected[]> {
    const items = dt.items ? Array.from(dt.items) : [];
    // Prefer webkitGetAsEntry folder-aware path
    const hasEntry = items.some(it => (it as unknown as { webkitGetAsEntry?: unknown }).webkitGetAsEntry);
    if (hasEntry && items.length) {
      const out: Collected[] = [];
      const queue: { entry: FileSystemEntry; base: string }[] = [];
      for (const it of items) {
        const e = (it as unknown as { webkitGetAsEntry?: () => FileSystemEntry | null }).webkitGetAsEntry?.();
        if (e) queue.push({ entry: e, base: '' });
      }
      if (!queue.length) {
        // fallback to files
        for (const f of Array.from(dt.files)) out.push({ file: f, relPath: (f as unknown as { webkitRelativePath?: string }).webkitRelativePath || f.name });
        return out;
      }
      const readDir = (dir: FileSystemDirectoryEntry): Promise<FileSystemEntry[]> => new Promise((res, rej) => {
        const r = dir.createReader();
        const acc: FileSystemEntry[] = [];
        const read = () => r.readEntries((ents) => {
          if (!ents.length) res(acc);
          else { acc.push(...ents); read(); }
        }, rej);
        read();
      });
      const getFile = (fe: FileSystemFileEntry): Promise<File> => new Promise((res, rej) => fe.file(res, rej));
      while (queue.length) {
        const { entry, base } = queue.shift()!;
        if (entry.isFile) {
          const f = await getFile(entry as FileSystemFileEntry);
          const rel = base ? `${base}/${f.name}` : (entry.fullPath?.replace(/^\//,'') || f.name);
          out.push({ file: f, relPath: rel });
        } else if (entry.isDirectory) {
          const ents = await readDir(entry as FileSystemDirectoryEntry);
          const relBase = entry.fullPath?.replace(/^\//,'') || entry.name;
          const nextBase = base ? `${base}/${entry.name}` : relBase;
          for (const ch of ents) queue.push({ entry: ch, base: nextBase });
          // ensure folder exists even if empty — push as dir marker via relPath with trailing /
          if (!ents.length) out.push({ file: new File([], entry.name), relPath: nextBase+'/', localPath: undefined } as unknown as Collected);
        }
      }
      return out;
    }
    // classic FileList path (also covers Wails `path` property case handled separately)
    const out: Collected[] = [];
    for (const f of Array.from(dt.files)) {
      const rel = (f as unknown as { webkitRelativePath?: string }).webkitRelativePath || f.name;
      out.push({ file: f, relPath: rel });
    }
    return out;
  }

  function onDragOver(e: DragEvent, target?: string) {
    e.preventDefault();
    dragOver = true;
    dropTarget = target ?? path;
    if (e.dataTransfer) e.dataTransfer.dropEffect = 'copy';
  }
  function onDragLeave() { dragOver = false; dropTarget = null; }
  async function uploadCollected(collected: Collected[], targetPath: string) {
    if (!paired) { error = 'Phone offline — reconnect.'; return; }
    const total = collected.reduce((s, c) => s + (c.file.size || 0), 0);
    if (total > DRAG_LIMIT && collected.length) {
      error = `These files are too large to drag (${fmtSizeShort(total)}). Right-click → Download for large files, or drag smaller files.`;
      return;
    }
    pendingUpload = true;
    let ok = 0;
    for (const c of collected) {
      if (c.relPath.endsWith('/') && c.file.size === 0) {
        // empty folder marker — create dir
        const dirPath = c.relPath.replace(/\/$/,'');
        const full = targetPath ? `${targetPath}/${dirPath}` : dirPath;
        try { await mkdirPhone(full); ok++; } catch (e) { error = e instanceof Error ? e.message : String(e); pendingUpload=false; return; }
        continue;
      }
      const lp = (c as Collected & { localPath?: string }).localPath;
      if (lp) {
        try { await uploadLocalFiles([lp], targetPath); ok++; } catch (e) { error = e instanceof Error ? e.message : String(e); pendingUpload=false; return; }
        continue;
      }
      // browser file — need to handle relPath dirs
      try {
        const b64 = await new Promise<string>((res, rej) => {
          const r = new FileReader();
          r.onload = () => { const v = r.result as string; const i = v.indexOf(','); res(i >= 0 ? v.slice(i + 1) : v); };
          r.onerror = () => rej(r.error);
          r.readAsDataURL(c.file);
        });
        // handle folder prefix in relPath
        if (c.relPath.includes('/')) {
          await uploadBrowserFileWithRelPath(b64, c.relPath, targetPath);
        } else {
          const safe = c.file.name.replace(/[^A-Za-z0-9._-]/g, '_').slice(0, 255) || 'file';
          await uploadBrowserFile(b64, safe, targetPath);
        }
        ok++;
      } catch (e) { error = e instanceof Error ? e.message : String(e); pendingUpload=false; return; }
    }
    pendingUpload = false;
    if (ok) { info = `Uploaded ${ok} item${ok>1?'s':''} to ${targetPath || 'Phone'}`; setTimeout(()=>info='',3000); await refresh(); }
  }

  async function onDrop(e: DragEvent, targetPath?: string) {
    e.preventDefault(); dragOver=false; dropTarget=null;
    const tp = targetPath ?? path;
    const dt = e.dataTransfer;
    if (!dt) return;
    // Wails Finder path shortcut (local file with .path)
    const files = Array.from(dt.files) as unknown as (File & { path?: string })[];
    const withPaths = files.filter(f => typeof (f as unknown as Record<string,unknown>).path === 'string' && (f as unknown as Record<string,unknown>).path);
    if (withPaths.length) {
      const locals = withPaths.map(f => (f as unknown as Record<string,unknown>).path as string);
      // check total size quickly
      try {
        // no size available for local paths quickly; rely on backend cap 2GiB; frontend gate best-effort via files size if available
        const anyTotal = files.reduce((s, f) => s + (f.size || 0), 0);
        if (anyTotal > DRAG_LIMIT) { error = `These files are too large to drag (${fmtSizeShort(anyTotal)}). Use Upload button for large files.`; return; }
        await uploadLocalFiles(locals, tp);
        info = `Uploaded ${locals.length} item${locals.length>1?'s':''} to ${tp || 'Phone'}`;
        setTimeout(()=>info='',3000);
        await refresh();
      } catch (err) { error = err instanceof Error ? err.message : String(err); }
      return;
    }
    const collected = await collectFromItems(dt);
    if (!collected.length) return;
    await uploadCollected(collected, tp);
  }

  let pointerStart: { x: number; y: number; entry: FileEntryView } | null = null;
  let isDraggingOut = false;

  function onPointerDown(e: PointerEvent, en: FileEntryView) {
    if (e.button !== 0) return;
    pointerStart = { x: e.clientX, y: e.clientY, entry: en };
    isDraggingOut = false;
  }

  function onPointerMove(e: PointerEvent, en: FileEntryView) {
    if (!pointerStart || isDraggingOut || pointerStart.entry.path !== en.path) return;
    if (e.buttons !== 1) {
      pointerStart = null;
      return;
    }
    const dist = Math.hypot(e.clientX - pointerStart.x, e.clientY - pointerStart.y);
    if (dist > 4) {
      isDraggingOut = true;
      const target = pointerStart.entry;
      pointerStart = null;
      if (target.is_dir) {
        error = `Folder dragging to Mac is not supported yet. Right-click → Download instead.`;
        return;
      }
      showTransfers = true;
      ensurePolling();
      void startFileDrag(target.path, target.name, target.size).catch((err) => {
        error = err instanceof Error ? err.message : String(err);
      });
    }
  }

  function onPointerUp() {
    pointerStart = null;
    isDraggingOut = false;
  }

  function openFileMenu(e: MouseEvent, en: FileEntryView) {
    e.preventDefault();
    const isDir = en.is_dir;
    const items: MenuItem[] = [];
    if (!isDir) {
      items.push({ id: 'download', label: 'Download' });
      items.push({ id: 'downloadTo', label: 'Download to…' });
    } else {
      items.push({ id: 'download', label: 'Download folder' });
    }
    items.push({ id: 'rename', label: 'Rename…' });
    items.push({ id: 'delete', label: 'Delete', destructive: true });
    menuState = { x: e.clientX, y: e.clientY, items, onPick: (id) => {
      menuState = null;
      if (id==='download') void doDownload(en.path);
      else if (id==='downloadTo') void doDownloadTo(en.path);
      else if (id==='rename') startRename(en.path);
      else if (id==='delete') deleteTarget = en.path;
    }};
  }
  function openEmptyMenu(e: MouseEvent) {
    e.preventDefault();
    const isOnRow = (e.target as HTMLElement)?.closest?.('[data-row]');
    if (isOnRow) return;
    menuState = { x: e.clientX, y: e.clientY, items: [
      { id: 'refresh', label: 'Refresh' },
      { id: 'newFolder', label: 'New folder…' },
    ], onPick: (id) => {
      menuState=null;
      if (id==='refresh') void refresh();
      else if (id==='newFolder') document.getElementById('new-folder-input')?.focus();
    }};
  }

  onMount(() => {
    if (paired) void refresh();
    ensurePolling();
    let offFilesDrop: (() => void) | null = null;
    let offTransfers: (() => void) | null = null;
    try {
      offTransfers = Events.On('transfers:changed', (ev: unknown) => {
        const d = (ev as { data?: unknown })?.data ?? ev;
        if (Array.isArray(d)) {
          applyTransfers(d as FileTransferView[]);
        }
      });
      offFilesDrop = Events.On('files-dropped', async (ev: unknown) => {
        const d = (ev as { data?: { paths?: string[]; targetPath?: string } })?.data ?? ev as { paths?: string[]; targetPath?: string };
        const ps = d?.paths ?? [];
        const tp = d?.targetPath || path;
        if (ps.length && paired) {
          pendingUpload = true;
          try {
            await uploadLocalFiles(ps, tp);
            info = `Uploaded ${ps.length} item${ps.length > 1 ? 's' : ''} to ${tp || 'Phone'}`;
            setTimeout(() => info = '', 3000);
            await refresh();
          } catch (e) {
            error = e instanceof Error ? e.message : String(e);
          } finally {
            pendingUpload = false;
          }
        }
      });
    } catch {}
    return () => {
      try { offTransfers?.(); } catch {}
      try { offFilesDrop?.(); } catch {}
    };
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
      <input type="file" multiple class="hidden" onchange={async (e) => {
        const el = e.currentTarget as HTMLInputElement;
        if (!el.files?.length) return;
        const collected: Collected[] = Array.from(el.files).map(f => ({ file: f, relPath: (f as unknown as { webkitRelativePath?: string }).webkitRelativePath || f.name }));
        await uploadCollected(collected, path);
        el.value='';
      }} />
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
    <!-- favorites sidebar — droppable -->
    <aside class="hidden w-[172px] shrink-0 flex-col border-r border-separator bg-sidebar md:flex">
      <p class="px-3 pb-1 pt-3 text-[10px] font-semibold uppercase tracking-[0.08em] text-tertiary">Favorites</p>
      <div class="flex-1 overflow-auto px-2 pb-2">
        {#each favorites as f}
          <button type="button"
            onclick={() => go(f.path)}
            data-file-drop-target="true"
            data-drop-path={f.path}
            ondragover={(e)=> onDragOver(e, f.path)}
            ondragleave={onDragLeave}
            ondrop={(e)=> onDrop(e, f.path)}
            class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-[13px] {dropTarget===f.path && dragOver ? 'bg-accent/15 ring-1 ring-accent' : path === f.path ? 'bg-accent text-accent-text' : 'text-label hover:bg-altrow'}">
            {#if f.icon}
              <!-- @ts-ignore -->
              <svelte:component this={f.icon} size={14} class={dropTarget===f.path && dragOver ? 'text-accent' : path === f.path ? 'text-accent-text' : 'text-accent'} />
            {:else}
              <Folder size={14} class={path === f.path ? 'text-accent-text' : 'text-secondary'} />
            {/if}
            <span class="truncate">{f.label}</span>
          </button>
        {/each}
      </div>
      <div class="border-t border-separator px-3 py-2">
        <p class="text-[10px] text-tertiary">Drag files here to upload.</p>
        <p class="text-[10px] text-tertiary">Right-click a file for more.</p>
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

      <div role="region" aria-label="File drop"
        data-file-drop-target="true"
        data-drop-path={path}
        ondragover={(e)=> onDragOver(e, path)}
        ondragleave={onDragLeave}
        ondrop={(e)=> onDrop(e, path)}
        oncontextmenu={openEmptyMenu}
        class="relative flex-1 overflow-auto">
        {#if dragOver}
          <div class="pointer-events-none absolute inset-3 rounded-[10px] border border-accent bg-accent/10 flex items-center justify-center backdrop-blur-sm transition-all duration-150 {dragOver ? 'opacity-100 scale-100' : 'opacity-0 scale-95'}">
            <div class="rounded-full bg-accent px-3 py-1.5 text-[13px] font-medium text-accent-text shadow">Drop to upload to {dropTarget || path || 'Phone'}</div>
          </div>
        {/if}

        {#if pendingUpload}
          <div class="flex items-center gap-2 px-3 py-2 text-[12px] text-secondary">
            <span class="h-3 w-3 animate-spin rounded-full border-2 border-accent border-t-transparent"></span>
            <span>Uploading…</span>
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
              <p class="mt-1 max-w-[32ch] text-[12px] text-secondary">Drag files or folders from Finder here, or create a folder and upload.</p>
              <label class="mt-4 inline-flex cursor-pointer items-center gap-1.5 rounded-md bg-accent px-3 py-1.5 text-[13px] font-medium text-accent-text">
                Upload files
                <input type="file" multiple class="hidden" onchange={async (e) => {
                  const el = e.currentTarget as HTMLInputElement;
                  if (!el.files?.length) return;
                  const collected: Collected[] = Array.from(el.files).map(f => ({ file: f, relPath: f.name }));
                  await uploadCollected(collected, path);
                  el.value='';
                }} />
              </label>
            {/if}
          </div>
        {:else}
          <div class="divide-y divide-grid">
            {#each filtered as e}
              <button type="button" data-row={e.path}
                onpointerdown={(ev) => onPointerDown(ev, e)}
                onpointermove={(ev) => onPointerMove(ev, e)}
                onpointerup={onPointerUp}
                onpointercancel={onPointerUp}
                oncontextmenu={(ev)=> openFileMenu(ev,e)}
                onclick={() => selected = e.path}
                ondblclick={() => enter(e.path, e.is_dir)}
                data-file-drop-target={e.is_dir ? "true" : undefined}
                data-drop-path={e.is_dir ? e.path : undefined}
                ondragover={(ev)=> { if (e.is_dir) onDragOver(ev, e.path); }}
                ondragleave={onDragLeave}
                ondrop={(ev)=> { if (e.is_dir) onDrop(ev, e.path); }}
                class="grid w-full grid-cols-[1fr_84px_112px] items-center gap-2 px-3 py-1.5 text-left hover:bg-altrow {selected === e.path ? 'bg-accent/15 !hover:bg-accent/15' : ''} {dropTarget===e.path && dragOver && e.is_dir ? 'ring-1 ring-inset ring-accent bg-accent/10' : ''}">
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
          <input id="new-folder-input" bind:value={newFolder} placeholder="New folder name" aria-label="New folder name" class="h-7 w-[168px] rounded-md border border-separator bg-window px-2 text-[12px] placeholder:text-tertiary focus:outline-none focus:ring-2 focus:ring-focus" />
          <button type="button" onclick={() => void doMkdir()} title="Create folder" class="inline-flex h-7 items-center gap-1.5 rounded-md border border-separator bg-window px-2.5 text-[12px] text-label hover:bg-altrow active:translate-y-[1px]">
            <FolderPlus size={13} /> Create
          </button>
        </div>
        <div class="mx-1 hidden h-5 w-px bg-separator sm:block"></div>
        <button type="button" onclick={() => void doDownload()} disabled={!selected} class="inline-flex h-7 items-center gap-1.5 rounded-md border border-separator bg-window px-2.5 text-[12px] text-label hover:bg-altrow active:translate-y-[1px] disabled:opacity-40">
          <Download size={13} /> Download
        </button>
        <button type="button" onclick={() => void doDownloadTo()} disabled={!selected} class="inline-flex h-7 items-center gap-1.5 rounded-md border border-separator bg-window px-2.5 text-[12px] text-label hover:bg-altrow active:translate-y-[1px] disabled:opacity-40">
          <FolderDown size={13} /> Download to…
        </button>
        <button type="button" onclick={() => { if(selected) deleteTarget=selected; }} disabled={!selected} class="inline-flex h-7 items-center gap-1.5 rounded-md bg-bad px-2.5 text-[12px] font-medium text-white hover:brightness-95 active:translate-y-[1px] disabled:opacity-40">
          <Trash2 size={13} /> Delete
        </button>
        <span class="flex-1"></span>
        <span class="hidden text-[11px] text-tertiary sm:inline">{filtered.length} item{filtered.length === 1 ? '' : 's'}{query ? ` matching “${query}”` : ''}</span>
      </div>
    </div>
  </div>

  <!-- progress — determinate, same place, transform-based -->
  {#if transfers.length}
    <div class="flex items-center gap-2 border-t border-separator bg-control px-3 py-1.5">
      <span class="text-[11px] font-medium text-secondary">{activeTransfers.length} running</span>
      <div class="flex flex-1 items-center gap-1.5 overflow-hidden">
        {#each recentTransfers.slice(0,3) as t (t.id)}
          <div class="flex min-w-0 flex-1 items-center gap-1.5 rounded bg-window px-2 py-1 transition-opacity duration-300">
            <span class="h-1.5 flex-1 overflow-hidden rounded bg-grid">
              <span class="block h-full origin-left bg-accent transition-transform duration-200 ease-linear" style="transform: scaleX({t.progress/100})"></span>
            </span>
            <span class="truncate text-[11px] {t.status === 'error' ? 'text-bad' : t.status==='done' ? 'text-ok' : 'text-secondary'}">{t.path.split('/').pop()} {t.progress}%</span>
            {#if t.status === 'running'}<button type="button" onclick={() => void cancelTransfer(t.id).then(() => pollTransfers())} class="text-[11px] text-bad hover:underline">Cancel</button>
            {:else if t.status === 'done'}<span class="text-[11px] text-ok">Done</span>
            {:else if t.status === 'error'}<span class="text-[11px] text-bad">{t.error ?? 'Failed'}</span>{/if}
          </div>
        {/each}
      </div>
      <button type="button" onclick={() => showTransfers = !showTransfers} class="text-[11px] text-tertiary hover:text-label">{showTransfers ? 'Hide' : `Show all (${transfers.length})`}</button>
    </div>
  {/if}
  {#if showTransfers && transfers.length}
    <div class="max-h-[140px] overflow-auto border-t border-separator bg-window px-2 py-1">
      {#each transfers as t (t.id)}
        <div class="flex items-center gap-2 px-2 py-1 text-[11px] transition-opacity duration-300">
          <span class="flex-1 truncate {t.status === 'error' ? 'text-bad' : t.status==='done' ? 'text-ok' : 'text-label'}">{t.direction} {t.path} — {t.status}</span>
          <span class="tabular-nums text-tertiary">{t.progress}%</span>
        </div>
      {/each}
    </div>
  {/if}

  {#if renameTarget}
    <div class="fixed inset-0 z-40 flex items-center justify-center bg-black/30 p-4" onclick={() => renameTarget=null} onkeydown={(e)=> e.key==='Escape' && (renameTarget=null)} role="presentation">
      <div role="dialog" aria-modal="true" aria-label="Rename" class="w-full max-w-[380px] rounded-xl bg-control p-4 shadow-xl" onclick={(e)=> e.stopPropagation()}>
        <h3 class="text-[13px] font-semibold text-label">Rename</h3>
        <p class="mt-1 text-[12px] text-secondary truncate">{renameTarget}</p>
        <input bind:value={renameValue} placeholder="New name" class="mt-3 h-8 w-full rounded-md border border-separator bg-window px-2 text-[13px] focus:outline-none focus:ring-2 focus:ring-focus" onkeydown={(e)=> e.key==='Enter' && confirmRename()} />
        <div class="mt-3 flex justify-end gap-2">
          <button type="button" onclick={() => renameTarget=null} class="h-7 rounded-md bg-window px-3 text-[13px]">Cancel</button>
          <button type="button" onclick={() => void confirmRename()} class="h-7 rounded-md bg-accent px-3 text-[13px] font-medium text-accent-text"><Pencil size={12} class="inline mr-1"/>Rename</button>
        </div>
      </div>
    </div>
  {/if}

  {#if deleteTarget}
    <div class="fixed inset-0 z-40 flex items-center justify-center bg-black/30 p-4" onclick={() => deleteTarget=null} onkeydown={(e)=> e.key==='Escape' && (deleteTarget=null)} role="presentation">
      <div role="dialog" aria-modal="true" aria-label="Delete" class="w-full max-w-[380px] rounded-xl bg-control p-4 shadow-xl" onclick={(e)=> e.stopPropagation()}>
        <h3 class="text-[13px] font-semibold text-label">Delete “{deleteTarget.split('/').pop()}”?</h3>
        <p class="mt-1 text-[12px] text-secondary">This cannot be undone.</p>
        <div class="mt-3 flex justify-end gap-2">
          <button type="button" onclick={() => deleteTarget=null} class="h-7 rounded-md bg-window px-3 text-[13px]">Keep</button>
          <button type="button" onclick={() => void doDelete(deleteTarget!)} class="h-7 rounded-md bg-bad px-3 text-[13px] font-medium text-white">Delete</button>
        </div>
      </div>
    </div>
  {/if}

  {#if menuState}
    <ContextMenu x={menuState.x} y={menuState.y} items={menuState.items} onPick={menuState.onPick} onClose={() => menuState=null} />
  {/if}
</section>

<style>
  :global([data-file-drop-target].file-drop-target-active) {
    outline: 2px solid var(--color-accent, #007aff) !important;
    background-color: color-mix(in srgb, var(--color-accent, #007aff) 15%, transparent) !important;
  }
  @media (prefers-reduced-motion: reduce) {
    div { transition: none !important; }
  }
</style>
