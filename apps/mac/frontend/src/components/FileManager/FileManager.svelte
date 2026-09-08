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
    if (!n) return '0 B';
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
    if (!paired) { error = 'Phone offline. Reconnect and try again.'; return; }
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

<section aria-label="Files" class="relative flex h-full min-h-0 flex-1 flex-col overflow-hidden bg-window">
  <!-- toolbar: frost-bar 48, leading nav + center crumb/search + trailing prominent -->
  <div class="frost-bar flex h-[48px] shrink-0 items-center gap-2 border-b border-separator px-3">
    <button type="button" onclick={up} disabled={!path} title="Go up one folder" aria-label="Go up one folder" class="inline-flex h-7 w-7 items-center justify-center rounded-md border border-separator bg-control text-label transition hover:bg-altrow focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus active:translate-y-[1px] disabled:opacity-40">
      <ArrowUp size={14} />
    </button>
    <div class="mx-1 h-5 w-px bg-separator" aria-hidden="true"></div>

    <nav aria-label="Path" class="flex min-w-0 flex-1 items-center gap-0.5 overflow-hidden text-[13px]">
      {#each crumbs as c, i}
        {#if i > 0}<span class="px-0.5 text-tertiary" aria-hidden="true">›</span>{/if}
        <button type="button" onclick={() => go(c.path)} title={c.path || 'Phone'} aria-current={c.path === path ? 'page' : undefined} class="max-w-[14ch] truncate rounded-md px-1.5 py-1 text-[13px] focus-visible:outline-2 focus-visible:outline-focus hover:bg-altrow {c.path === path ? 'font-semibold text-label' : 'text-secondary'}">{c.label}</button>
      {/each}
    </nav>

    <div class="relative hidden items-center md:flex">
      <Search size={13} class="pointer-events-none absolute left-2 text-tertiary" />
      <input bind:value={query} placeholder="Filter in {breadcrumbs[breadcrumbs.length - 1] || 'Phone'}" aria-label="Filter files in this folder" class="h-7 w-[168px] rounded-md border border-separator bg-control pl-7 pr-7 text-[12px] text-label placeholder:text-tertiary focus:outline-none focus:ring-2 focus:ring-focus" onkeydown={(e) => { if (e.key === 'Escape') query = ''; }} />
      {#if query}
        <button type="button" onclick={() => query = ''} aria-label="Clear filter" class="absolute right-1.5 flex h-5 w-5 items-center justify-center rounded text-[12px] leading-none text-tertiary hover:bg-altrow hover:text-label">×</button>
      {/if}
    </div>

    <button type="button" onclick={() => void refresh()} disabled={loading} aria-label="Refresh file list" title="Refresh file list" class="inline-flex h-7 w-7 items-center justify-center rounded-md border border-separator bg-control text-secondary hover:text-label focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus active:translate-y-[1px] disabled:opacity-50">
      <RefreshCw size={14} class={loading ? 'animate-spin' : ''} />
    </button>

    <label class="inline-flex h-7 cursor-pointer items-center gap-1.5 rounded-md bg-accent px-3 text-[13px] font-medium text-accent-text transition hover:brightness-95 focus-within:outline-2 focus-within:outline-offset-2 focus-within:outline-focus active:translate-y-[1px]">
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

  <div class="flex min-h-0 flex-1 overflow-hidden">
    <!-- favorites sidebar — droppable -->
    <aside class="hidden w-[176px] shrink-0 flex-col border-r border-separator bg-sidebar md:flex">
      <p class="px-3 pb-1 pt-3 text-[10px] font-semibold uppercase tracking-[0.08em] text-tertiary">Favorites</p>
      <div class="min-h-0 flex-1 overflow-auto px-2 pb-2">
        {#each favorites as f}
          <button type="button"
            onclick={() => go(f.path)}
            data-file-drop-target="true"
            data-drop-path={f.path}
            ondragover={(e)=> onDragOver(e, f.path)}
            ondragleave={onDragLeave}
            ondrop={(e)=> onDrop(e, f.path)}
            aria-current={path === f.path ? 'page' : undefined}
            title={f.label}
            class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-[13px] transition focus-visible:outline-2 focus-visible:outline-focus {dropTarget===f.path && dragOver ? 'bg-accent/15 ring-1 ring-inset ring-accent' : path === f.path ? 'bg-accent text-accent-text' : 'text-label hover:bg-altrow'}">
            {#if f.icon}
              <!-- @ts-ignore -->
              <svelte:component this={f.icon} size={14} class="shrink-0 {dropTarget===f.path && dragOver ? 'text-accent' : path === f.path ? 'text-accent-text' : 'text-accent'}" />
            {:else}
              <Folder size={14} class="shrink-0 {path === f.path ? 'text-accent-text' : 'text-secondary'}" />
            {/if}
            <span class="truncate">{f.label}</span>
          </button>
        {/each}
      </div>
    </aside>

    <!-- list area -->
    <div class="relative flex min-h-0 min-w-0 flex-1 flex-col bg-window">
      <!-- table header -->
      <div class="sticky top-0 z-10 grid shrink-0 grid-cols-[1fr_84px_112px] items-center gap-2 border-b border-grid bg-window px-3 py-1.5 text-[10px] font-medium uppercase tracking-wide text-secondary">
        <span>Name</span>
        <span class="text-right">Size</span>
        <span class="hidden text-right sm:block">Modified</span>
        <span class="text-right sm:hidden">Date</span>
      </div>

      <div role="region" aria-label="File drop"
        data-file-drop-target="true"
        data-drop-path={path}
        ondragover={(e)=> onDragOver(e, path)}
        ondragleave={onDragLeave}
        ondrop={(e)=> onDrop(e, path)}
        oncontextmenu={openEmptyMenu}
        class="relative min-h-0 min-w-0 flex-1 overflow-auto">
        {#if dragOver}
          <div class="pointer-events-none absolute inset-3 z-20 flex items-center justify-center rounded-[10px] border border-accent bg-accent/10 opacity-100 transition-opacity duration-150">
            <div class="rounded-full bg-accent px-3 py-1.5 text-[13px] font-medium text-accent-text shadow">Drop to upload to {dropTarget || path || 'Phone'}</div>
          </div>
        {/if}

        {#if pendingUpload}
          <div class="flex shrink-0 items-center gap-2 border-b border-grid bg-altrow px-3 py-2 text-[12px] text-secondary" role="status">
            <span class="h-3 w-3 animate-spin rounded-full border-2 border-accent border-t-transparent" aria-hidden="true"></span>
            <span>Uploading to {path || 'Phone'}…</span>
          </div>
        {/if}

        {#if loading}
          <div class="divide-y divide-grid">
            {#each Array(8) as _, i}
              <div class="grid grid-cols-[1fr_84px_112px] items-center gap-2 px-3 py-2">
                <div class="flex items-center gap-2">
                  <div class="h-6 w-6 shrink-0 rounded-md bg-altrow"></div>
                  <div class="h-3 flex-1 rounded bg-altrow" style="max-width: {58 - (i % 4) * 9}%"></div>
                </div>
                <div class="h-3 rounded bg-altrow"></div>
                <div class="h-3 rounded bg-altrow"></div>
              </div>
            {/each}
          </div>
        {:else if isPermissionError}
          <div class="flex flex-col items-center px-6 py-16 text-center">
            <div class="flex h-12 w-12 items-center justify-center rounded-full bg-warn/15 text-warn"><HardDrive size={22} /></div>
            <h3 class="mt-3 text-[13px] font-semibold text-label">All files access needed</h3>
            <p class="mt-1 max-w-[38ch] text-[12px] leading-relaxed text-secondary">Your phone is blocking the file list. Grant All files access so the Mac can see storage.</p>
            <p class="mt-2 max-w-[42ch] rounded-md bg-altrow px-2.5 py-2 text-[11px] leading-relaxed text-secondary">On phone: Settings, then Apps, then FuseItAll, then allow access to all files. Then choose Refresh.</p>
            <button type="button" onclick={() => void refresh()} class="mt-4 inline-flex h-7 items-center rounded-md bg-accent px-3 text-[13px] font-medium text-accent-text transition hover:brightness-95 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus active:translate-y-[1px]">Refresh</button>
          </div>
        {:else if !filtered.length}
          <div class="flex flex-col items-center px-6 py-14 text-center">
            <div class="flex h-11 w-11 items-center justify-center rounded-full bg-accent/15 text-accent"><Folder size={20} /></div>
            {#if query}
              <p class="mt-3 text-[13px] font-medium text-label">No matches for “{query}”</p>
              <p class="mt-1 text-[12px] text-secondary">Try a shorter filter, or clear it to see everything here.</p>
              <button type="button" onclick={() => query = ''} class="mt-4 inline-flex h-7 items-center rounded-md border border-separator bg-control px-3 text-[12px] text-label transition hover:bg-altrow focus-visible:outline-2 focus-visible:outline-focus active:translate-y-[1px]">Clear filter</button>
            {:else}
              <p class="mt-3 text-[13px] font-medium text-label">This folder is empty</p>
              <p class="mt-1 max-w-[34ch] text-[12px] leading-relaxed text-secondary">Drag files or folders from Finder here, or create a folder below and upload.</p>
              <label class="mt-4 inline-flex cursor-pointer items-center gap-1.5 rounded-md bg-accent px-3 py-1.5 text-[13px] font-medium text-accent-text transition hover:brightness-95 focus-within:outline-2 focus-within:outline-focus active:translate-y-[1px]">
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
                onkeydown={(ev) => { if (ev.key === 'Enter') enter(e.path, e.is_dir); }}
                data-file-drop-target={e.is_dir ? "true" : undefined}
                data-drop-path={e.is_dir ? e.path : undefined}
                ondragover={(ev)=> { if (e.is_dir) onDragOver(ev, e.path); }}
                ondragleave={onDragLeave}
                ondrop={(ev)=> { if (e.is_dir) onDrop(ev, e.path); }}
                aria-pressed={selected === e.path}
                title={e.path}
                class="grid w-full grid-cols-[1fr_84px_112px] items-center gap-2 px-3 py-[7px] text-left transition focus-visible:outline-2 focus-visible:outline-inset focus-visible:outline-focus hover:bg-altrow {selected === e.path ? 'bg-accent/15 hover:bg-accent/15' : ''} {dropTarget===e.path && dragOver && e.is_dir ? 'bg-accent/10 ring-1 ring-inset ring-accent' : ''}">
                <span class="flex min-w-0 items-center gap-2">
                  <span class="flex h-6 w-6 shrink-0 items-center justify-center rounded-md {selected === e.path && e.is_dir ? 'bg-accent text-accent-text' : e.is_dir ? 'bg-accent/15 text-accent' : 'bg-altrow text-secondary'}">
                    {#if e.is_dir}<Folder size={13} />{:else}<FileIcon size={13} />{/if}
                  </span>
                  <span class="truncate text-[13px] {selected === e.path ? 'font-medium text-label' : 'text-label'}">{e.name}</span>
                  {#if selected === e.path}<span class="hidden min-w-0 truncate text-[11px] text-tertiary sm:inline"> · {e.path}</span>{/if}
                </span>
                <span class="text-right text-[12px] tabular-nums {selected === e.path ? 'text-label' : 'text-secondary'}">{e.is_dir ? '·' : fmtSize(e.size)}</span>
                <span class="text-right text-[12px] tabular-nums text-tertiary sm:text-secondary">{fmtTime(e.mod_time) || '·'}</span>
              </button>
            {/each}
          </div>
        {/if}
      </div>

    </div>
  </div>

  <!-- footer actions: full-width static strip (h-46). Always rendered with the
       same height and order; only enabled states and the trailing count swap,
       so nothing here depends on list content. Narrow windows scroll this row
       horizontally instead of clipping Delete. -->
  <div class="flex h-[46px] shrink-0 flex-nowrap items-center gap-2 overflow-x-auto border-t border-separator bg-control px-3">
    <div class="flex min-w-0 items-center gap-1.5">
      <input id="new-folder-input" bind:value={newFolder} placeholder="New folder name" aria-label="New folder name" title="Create a folder here, or drop Finder files anywhere to upload" class="h-7 w-full min-w-[90px] max-w-[168px] rounded-md border border-separator bg-window px-2 text-[12px] placeholder:text-tertiary focus:outline-none focus:ring-2 focus:ring-focus" onkeydown={(e) => { if (e.key === 'Enter') void doMkdir(); }} />
      <button type="button" onclick={() => void doMkdir()} title="Create folder in this location" aria-label="Create folder in this location" class="inline-flex h-7 shrink-0 items-center gap-1.5 rounded-md border border-separator bg-window px-2.5 text-[12px] text-label transition hover:bg-altrow focus-visible:outline-2 focus-visible:outline-focus active:translate-y-[1px]">
        <FolderPlus size={13} /> Create
      </button>
    </div>
    <div class="mx-1 h-5 w-px shrink-0 bg-separator" aria-hidden="true"></div>
    <button type="button" onclick={() => void doDownload()} disabled={!selected} title={selected ? `Download ${selected.split('/').pop()}` : 'Select a file first'} class="inline-flex h-7 shrink-0 items-center gap-1.5 rounded-md border border-separator bg-window px-2.5 text-[12px] text-label transition hover:bg-altrow focus-visible:outline-2 focus-visible:outline-focus active:translate-y-[1px] disabled:opacity-40">
      <Download size={13} /> Download
    </button>
    <button type="button" onclick={() => void doDownloadTo()} disabled={!selected} title={selected ? 'Choose where on this Mac to save' : 'Select a file first'} class="inline-flex h-7 shrink-0 items-center gap-1.5 rounded-md border border-separator bg-window px-2.5 text-[12px] text-label transition hover:bg-altrow focus-visible:outline-2 focus-visible:outline-focus active:translate-y-[1px] disabled:opacity-40">
      <FolderDown size={13} /> Download to…
    </button>
    <button type="button" onclick={() => { if(selected) deleteTarget=selected; }} disabled={!selected} title={selected ? `Delete ${selected.split('/').pop()}` : 'Select a file first'} class="inline-flex h-7 shrink-0 items-center gap-1.5 rounded-md bg-bad px-2.5 text-[12px] font-medium text-white transition hover:brightness-95 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus active:translate-y-[1px] disabled:opacity-40">
      <Trash2 size={13} /> Delete
    </button>
    <span class="ml-auto hidden shrink-0 pl-2 text-[11px] tabular-nums text-tertiary md:inline">{filtered.length} item{filtered.length === 1 ? '' : 's'}{query ? ` matching “${query}”` : ''}{selected ? ' · 1 selected' : ''}</span>
  </div>

  <!-- status line: full-width static strip (h-30), same geometry as Photos.
       Idle shows the folder count; active transfers swap the text in place. -->
  <div class="flex h-[30px] shrink-0 items-center gap-2 overflow-hidden border-t border-separator bg-control px-3" role="status" aria-live="polite">
    {#if activeTransfers.length}
      <span class="h-1.5 w-24 shrink-0 overflow-hidden rounded bg-grid" aria-hidden="true">
        <span class="block h-full origin-left bg-accent transition-transform duration-200 ease-linear" style="transform: scaleX({recentTransfers[0] ? recentTransfers[0].progress / 100 : 0})"></span>
      </span>
      <span class="min-w-0 flex-1 truncate text-[11px] tabular-nums text-secondary">
        {#if recentTransfers[0]}{recentTransfers[0].path.split('/').pop()} · {recentTransfers[0].progress}%{/if}{#if activeTransfers.length > 1} · {activeTransfers.length} running{/if}
      </span>
      {#if recentTransfers[0] && recentTransfers[0].status === 'running'}
        <button type="button" onclick={() => void cancelTransfer(recentTransfers[0].id).then(() => pollTransfers())} class="shrink-0 text-[11px] text-bad hover:underline">Cancel</button>
      {/if}
      {#if transfers.length > 1}
        <button type="button" onclick={() => showTransfers = !showTransfers} aria-expanded={showTransfers} class="shrink-0 text-[11px] text-tertiary hover:text-label">{showTransfers ? 'Hide' : `All (${transfers.length})`}</button>
      {/if}
    {:else if transfers.length}
      <span class="h-1.5 w-1.5 shrink-0 rounded-full {transfers.some((t) => t.status === 'error') ? 'bg-bad' : 'bg-ok'}" aria-hidden="true"></span>
      <span class="min-w-0 flex-1 truncate text-[11px] tabular-nums text-secondary">{transfers[transfers.length - 1].path.split('/').pop()} · {transfers[transfers.length - 1].status}{transfers.length > 1 ? ` · ${transfers.length} recent` : ''}</span>
      <button type="button" onclick={() => showTransfers = !showTransfers} aria-expanded={showTransfers} class="shrink-0 text-[11px] text-tertiary hover:text-label">{showTransfers ? 'Hide' : `All (${transfers.length})`}</button>
    {:else if pendingUpload}
      <span class="h-3 w-3 shrink-0 animate-spin rounded-full border-2 border-accent border-t-transparent" aria-hidden="true"></span>
      <span class="truncate text-[11px] text-secondary">Uploading…</span>
    {:else}
      <span class="h-1.5 w-1.5 shrink-0 rounded-full bg-tertiary" aria-hidden="true"></span>
      <span class="truncate text-[11px] tabular-nums text-tertiary">{filtered.length} item{filtered.length === 1 ? '' : 's'} in {path || 'Phone'}{!paired ? ' · phone offline' : ''}</span>
    {/if}
  </div>
  {#if showTransfers && transfers.length}
    <div class="absolute inset-x-3 bottom-[78px] z-30 max-h-[180px] overflow-auto rounded-lg border border-separator bg-control p-1 shadow-xl">
      {#each transfers as t (t.id)}
        <div class="flex items-center gap-2 rounded-md px-2 py-1.5 text-[11px]">
          <span class="h-1.5 w-16 shrink-0 overflow-hidden rounded bg-grid" aria-hidden="true">
            <span class="block h-full origin-left bg-accent" style="transform: scaleX({t.progress / 100})"></span>
          </span>
          <span class="flex-1 truncate {t.status === 'error' ? 'text-bad' : t.status === 'done' ? 'text-ok' : 'text-label'}">{t.direction} {t.path} · {t.status}</span>
          <span class="shrink-0 tabular-nums text-tertiary">{t.progress}%</span>
          {#if t.status === 'running'}
            <button type="button" onclick={() => void cancelTransfer(t.id).then(() => pollTransfers())} class="shrink-0 text-[11px] text-bad hover:underline">Cancel</button>
          {/if}
        </div>
      {/each}
    </div>
  {/if}

  {#if renameTarget}
    <div class="fixed inset-0 z-40 flex items-center justify-center bg-black/30 p-4" onclick={() => renameTarget=null} onkeydown={(e)=> e.key==='Escape' && (renameTarget=null)} role="presentation">
      <div role="dialog" aria-modal="true" aria-label="Rename" class="w-full max-w-[380px] rounded-[12px] border border-separator bg-control p-4 shadow-xl" onclick={(e)=> e.stopPropagation()}>
        <h3 class="text-[13px] font-semibold text-label">Rename</h3>
        <p class="mt-1 truncate text-[12px] tabular-nums text-secondary">{renameTarget}</p>
        <input bind:value={renameValue} placeholder="New name" aria-label="New name" class="mt-3 h-8 w-full rounded-md border border-separator bg-window px-2 text-[13px] focus:outline-none focus:ring-2 focus:ring-focus" onkeydown={(e)=> e.key==='Enter' && confirmRename()} />
        <div class="mt-4 flex justify-end gap-2">
          <button type="button" onclick={() => renameTarget=null} class="h-7 rounded-md border border-separator bg-window px-3 text-[13px] text-label transition hover:bg-altrow focus-visible:outline-2 focus-visible:outline-focus active:translate-y-[1px]">Cancel</button>
          <button type="button" onclick={() => void confirmRename()} class="inline-flex h-7 items-center gap-1.5 rounded-md bg-accent px-3 text-[13px] font-medium text-accent-text transition hover:brightness-95 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus active:translate-y-[1px]"><Pencil size={12} />Rename</button>
        </div>
      </div>
    </div>
  {/if}

  {#if deleteTarget}
    <div class="fixed inset-0 z-40 flex items-center justify-center bg-black/30 p-4" onclick={() => deleteTarget=null} onkeydown={(e)=> e.key==='Escape' && (deleteTarget=null)} role="presentation">
      <div role="dialog" aria-modal="true" aria-label="Delete file" class="w-full max-w-[380px] rounded-[12px] border border-separator bg-control p-4 shadow-xl" onclick={(e)=> e.stopPropagation()}>
        <div class="flex items-start gap-3">
          <span class="flex h-9 w-9 shrink-0 items-center justify-center rounded-full bg-bad/15 text-bad" aria-hidden="true"><Trash2 size={16} /></span>
          <div class="min-w-0">
            <h3 class="truncate text-[13px] font-semibold text-label">Delete “{deleteTarget.split('/').pop()}”?</h3>
            <p class="mt-1 text-[12px] leading-snug text-secondary">This removes it from the phone. This cannot be undone.</p>
          </div>
        </div>
        <div class="mt-4 flex justify-end gap-2">
          <button type="button" onclick={() => deleteTarget=null} class="h-7 rounded-md border border-separator bg-window px-3 text-[13px] text-label transition hover:bg-altrow focus-visible:outline-2 focus-visible:outline-focus active:translate-y-[1px]">Keep</button>
          <button type="button" onclick={() => void doDelete(deleteTarget!)} class="h-7 rounded-md bg-bad px-3 text-[13px] font-medium text-white transition hover:brightness-95 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus active:translate-y-[1px]">Delete</button>
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
