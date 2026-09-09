/*
 * Copyright (C) 2026 FuseItAll contributors.
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU Affero General Public License as published
 * by the Free Software Foundation, version 3 of the License. See LICENSE
 * for details.
 *
 * Upload conflict helpers: pure client-side policy resolution mirroring
 * core.IsSourceNewer and core.KeepBothName. No network, no UI.
 */

export type FileChoice = 'overwrite' | 'if_newer' | 'keep_both' | 'skip' | 'stop';
export type FolderChoice = 'merge' | 'overwrite' | 'stop';

export interface RemoteEntry {
  name: string;
  path: string;
  is_dir: boolean;
  size: number;
  mod_time: number;
}

export interface FileConflict {
  kind: 'file';
  name: string;
  remotePath: string;
  localSize: number;
  localMtime: number;
  remoteSize: number;
  remoteMtime: number;
}

export interface FolderConflict {
  kind: 'folder';
  name: string;
  remotePath: string;
}

/** Mirror of core.IsSourceNewer: mtime seconds first, size tiebreak. */
export function isSourceNewer(
  sourceMtime: number,
  targetMtime: number,
  sourceSize: number,
  targetSize: number,
): boolean {
  if (sourceMtime !== targetMtime) return sourceMtime > targetMtime;
  return sourceSize !== targetSize;
}

/** Mirror of core.KeepBothName: Finder-style numbering. */
export function keepBothName(remotePath: string, existing: Set<string>): string {
  if (!existing.has(remotePath)) return remotePath;
  let base = remotePath;
  let ext = '';
  const slash = remotePath.lastIndexOf('/');
  const file = slash >= 0 ? remotePath.slice(slash + 1) : remotePath;
  const dir = slash >= 0 ? remotePath.slice(0, slash) : '';
  const dot = file.lastIndexOf('.');
  if (dot > 0) {
    ext = file.slice(dot);
    base = `${dir ? `${dir}/` : ''}${file.slice(0, dot)}`;
  }
  let i = 2;
  for (;;) {
    const candidate = `${base} (${i})${ext}`;
    if (!existing.has(candidate)) return candidate;
    i++;
  }
}

export function fmtConflictDate(ts: number): string {
  if (!ts) return 'unknown date';
  try {
    return new Date(ts * 1000).toLocaleString(undefined, {
      month: 'short',
      day: 'numeric',
      year: 'numeric',
      hour: 'numeric',
      minute: '2-digit',
    });
  } catch {
    return 'unknown date';
  }
}

export function fmtConflictSize(n: number): string {
  if (!n) return '0 B';
  if (n < 1024) return `${n} B`;
  if (n < 1 << 20) return `${(n / 1024).toFixed(1)} KB`;
  if (n < 1 << 30) return `${(n / (1 << 20)).toFixed(1)} MB`;
  return `${(n / (1 << 30)).toFixed(2)} GB`;
}
