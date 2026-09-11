<!--
  SPDX-License-Identifier: AGPL-3.0-only

  Transfer status strip plus the recent-transfers popover: batch total
  progress (current file + bytes), per-file rows, Cancel batch / Cancel
  file, Retry for failed resumable uploads.
  HIG Read: primary window status strip for transfer progress, following
  HIG Progress Indicators, Buttons. Classic frost, no Liquid Glass.
-->
<script lang="ts">
  import type { FileTransferView, TransferBatchView } from '../../backend';

  interface Props {
    transfers: FileTransferView[];
    activeTransfers: FileTransferView[];
    recentTransfers: FileTransferView[];
    batches: TransferBatchView[];
    activeBatch: TransferBatchView | null;
    showTransfers: boolean;
    pendingUpload: boolean;
    loading: boolean;
    itemCount: number;
    pathLabel: string;
    paired: boolean;
    onCancel: (id: string) => void;
    onCancelBatch: (id: string) => void;
    onRetry: (t: FileTransferView) => void;
    onToggleTransfers: () => void;
  }
  let {
    transfers, activeTransfers, recentTransfers, batches, activeBatch,
    showTransfers, pendingUpload, loading, itemCount, pathLabel, paired,
    onCancel, onCancelBatch, onRetry, onToggleTransfers,
  }: Props = $props();

  function canRetry(t: FileTransferView): boolean {
    return t.direction === 'upload' && t.status === 'error' && !!t.resumable;
  }

  function fmtSize(n: number): string {
    if (!n) return '0 B';
    if (n < 1024) return `${n} B`;
    if (n < 1 << 20) return `${(n / 1024).toFixed(1)} KB`;
    if (n < 1 << 30) return `${(n / (1 << 20)).toFixed(1)} MB`;
    return `${(n / (1 << 30)).toFixed(2)} GB`;
  }

  function shortName(p: string): string {
    if (!p) return '';
    const parts = p.split('/').filter(Boolean);
    return parts.length ? parts[parts.length - 1] : p;
  }

  let batchLabel = $derived.by(() => {
    if (!activeBatch) return '';
    const cur = shortName(activeBatch.current_path);
    const files = `File ${Math.min(activeBatch.done_files + 1, Math.max(activeBatch.total_files, 1))} of ${Math.max(activeBatch.total_files, 1)}`;
    const bytes = activeBatch.total_bytes > 0
      ? `${fmtSize(activeBatch.done_bytes)} of ${fmtSize(activeBatch.total_bytes)}`
      : fmtSize(activeBatch.done_bytes);
    return cur ? `${files} · ${cur} · ${activeBatch.progress}% · ${bytes}` : `${files} · ${activeBatch.progress}% · ${bytes}`;
  });
</script>

<!-- status line: rendered only while there is transfer status to show.
     Idle shows nothing: the action bar above already carries the count. -->
{#if activeBatch || activeTransfers.length || transfers.length || pendingUpload || loading}
<div class="flex h-[30px] shrink-0 items-center gap-2 overflow-hidden bg-control px-3" role="status" aria-live="polite">
  {#if activeBatch}
    <span class="h-1.5 w-24 shrink-0 overflow-hidden rounded bg-grid" role="progressbar" aria-valuemin={0} aria-valuemax={100} aria-valuenow={activeBatch.progress} aria-label="Total upload progress" aria-hidden="false">
      <span class="block h-full origin-left bg-accent transition-transform duration-200 ease-linear" style="transform: scaleX({activeBatch.progress / 100})"></span>
    </span>
    <span class="min-w-0 flex-1 truncate text-[11px] tabular-nums text-secondary">{batchLabel}</span>
    <button type="button" onclick={() => onCancelBatch(activeBatch.id)} class="shrink-0 text-[11px] text-bad hover:underline">Cancel batch</button>
    {#if transfers.length > 1}
      <button type="button" onclick={onToggleTransfers} aria-expanded={showTransfers} class="shrink-0 text-[11px] text-tertiary hover:text-label">{showTransfers ? 'Hide' : `Files (${transfers.length})`}</button>
    {/if}
  {:else if activeTransfers.length}
    <span class="h-1.5 w-24 shrink-0 overflow-hidden rounded bg-grid" aria-hidden="true">
      <span class="block h-full origin-left bg-accent transition-transform duration-200 ease-linear" style="transform: scaleX({recentTransfers[0] ? recentTransfers[0].progress / 100 : 0})"></span>
    </span>
    <span class="min-w-0 flex-1 truncate text-[11px] tabular-nums text-secondary">
      {#if recentTransfers[0]}{recentTransfers[0].path.split('/').pop()} · {recentTransfers[0].progress}% · {fmtSize(recentTransfers[0].done_size)} of {fmtSize(recentTransfers[0].total_size)}{/if}{#if activeTransfers.length > 1} · {activeTransfers.length} running{/if}
    </span>
    {#if recentTransfers[0] && recentTransfers[0].status === 'running'}
      <button type="button" onclick={() => onCancel(recentTransfers[0].id)} class="shrink-0 text-[11px] text-bad hover:underline">Cancel</button>
    {/if}
    {#if transfers.length > 1}
      <button type="button" onclick={onToggleTransfers} aria-expanded={showTransfers} class="shrink-0 text-[11px] text-tertiary hover:text-label">{showTransfers ? 'Hide' : `All (${transfers.length})`}</button>
    {/if}
  {:else if transfers.length}
    {@const last = transfers[transfers.length - 1]}
    <span class="h-1.5 w-1.5 shrink-0 rounded-full {transfers.some((t) => t.status === 'error') ? 'bg-bad' : 'bg-ok'}" aria-hidden="true"></span>
    <span class="min-w-0 flex-1 truncate text-[11px] tabular-nums text-secondary">{last.path.split('/').pop()} · {last.status}{transfers.length > 1 ? ` · ${transfers.length} recent` : ''}{#if batches.length && batches[0]} · batch {batches[0].done_files}/{batches[0].total_files}{/if}</span>
    {#if canRetry(last)}
      <button type="button" onclick={() => onRetry(last)} class="shrink-0 text-[11px] text-accent-text hover:underline">Retry</button>
    {/if}
    <button type="button" onclick={onToggleTransfers} aria-expanded={showTransfers} class="shrink-0 text-[11px] text-tertiary hover:text-label">{showTransfers ? 'Hide' : `All (${transfers.length})`}</button>
  {:else if pendingUpload}
    <span class="h-3 w-3 shrink-0 animate-spin rounded-full border-2 border-accent border-t-transparent" aria-hidden="true"></span>
    <span class="truncate text-[11px] text-secondary">Uploading…</span>
  {:else if loading}
    <span class="h-3 w-3 shrink-0 animate-spin rounded-full border-2 border-accent border-t-transparent" aria-hidden="true"></span>
    <span class="truncate text-[11px] text-secondary">Refreshing file list…</span>
  {/if}
</div>
{/if}
{#if showTransfers && transfers.length}
  <div class="anim-pop absolute inset-x-3 bottom-[78px] z-30 max-h-[180px] overflow-auto rounded-lg bg-control p-1 shadow-xl" style="--origin: bottom center">
    {#each transfers as t (t.id)}
      <div class="flex items-center gap-2 rounded-md px-2 py-1.5 text-[11px]">
        <span class="h-1.5 w-16 shrink-0 overflow-hidden rounded bg-grid" role="progressbar" aria-valuemin={0} aria-valuemax={100} aria-valuenow={t.progress} aria-label="File progress for {shortName(t.path)}">
          <span class="block h-full origin-left bg-accent" style="transform: scaleX({t.progress / 100})"></span>
        </span>
        <span class="flex-1 truncate {t.status === 'error' ? 'text-bad' : t.status === 'done' ? 'text-ok' : 'text-label'}" title={t.error || `${t.direction} ${t.path}`}>{t.direction} {t.path} · {t.status}{#if t.error} — {t.error}{/if}</span>
        <span class="shrink-0 tabular-nums text-tertiary">{t.progress}% · {fmtSize(t.done_size)}/{fmtSize(t.total_size)}</span>
        {#if t.status === 'running'}
          <button type="button" onclick={() => onCancel(t.id)} class="shrink-0 text-[11px] text-bad hover:underline">Cancel</button>
        {/if}
        {#if canRetry(t)}
          <button type="button" onclick={() => onRetry(t)} class="shrink-0 text-[11px] text-accent-text hover:underline">Retry</button>
        {/if}
      </div>
    {/each}
  </div>
{/if}
