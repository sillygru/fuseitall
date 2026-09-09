# Files

How the Mac browses and manages phone files. Request/response over the same
lane, chunked transfer with atomic commit, sandboxed paths.

## What it does

List directories, create/rename/delete, download from the phone and upload
to it, with progress and offline caching. Drag-out to Finder uses a staged
exact-path handoff, never an unprompted background download.

## How it flows

1. Mac `backend/files.go:ListPhoneFiles` sends `file-list{path, req_id}`;
   phone `features/files/file_sync.dart` + `file_system.dart` answers one
   `file-list-resp{req_id, entries}` over the same `/files` lane
   (`req_id` 1..64 correlation, `pendingLists` 8s timeout, `lastList`
   offline cache, entries ≤500).
2. Mutations (`file-mkdir/delete/rename`) ride the same lane; `rename` is
   same-dir only.
3. Download: Mac `RequestPhoneFile` → `file-pull-req` → phone streams
   `file-chunk{transfer_id, offset==chunk_index*1MiB, total_chunks}`; Mac
   stages to `os.TempDir()/fuseitall-files` (0700) and atomically renames
   `.part.<id>` → final (`-<id6>` on collision), verifying `size` and
   `sha256` on the last chunk. Default destination `~/Downloads`.
4. Upload: browser `UploadBrowserFile(+WithRelPath)` (base64 fallback) with
   parent `mkdir` loop; same chunking in reverse.
5. Progress: `GetTransfers` (5s prune) → `transfers:changed`; `CancelTransfer`
   is local-only (no wire cancel). Drag-out: `PrepareDownloadForDrag`
   (staged-only) + `DownloadFileToExactPath` for `NSFilePromiseProvider`
   (5min waiter).

## Contract

- Capability `files`, gate `build>=5` (`packages/core/files.go`,
  `packages/proto/files.json`).
- Types on `POST /files` (+ WS): `file-list → file-list-resp`,
  `file-mkdir/delete/rename`, `file-chunk`, `file-pull-req`.
- Chunks: 1MiB raw (~1.4MiB b64) under `MaxBodyBytes 8MiB`;
  `transfer_id` 16..64 hex (`crypto/rand`); idempotent by
  `transfer_id+offset`; `total_chunks` must stay consistent.

## Key files

- Core: `files.go` (sandbox, chunk validation).
- Mac: `backend/files.go`, `backend/service_ws.go`,
  `backend/file_drag_darwin.{go,h,m}`, `frontend/src/components/FileManager/FileManager.svelte`,
  `frontend/src/backend.ts` (`listPhoneFiles/requestPhoneFile/getTransfers`).
- Android: `features/files/file_sync.dart`, `file_system.dart`,
  `file_models.dart`.

## Rules & limits

- Sandbox: rel paths only — no absolute, no `..`, no `\`, no NUL/control;
  total ≤1024, component ≤255 (`filepath.IsLocal + Clean`).
- Errors carry `error/error_code{permission_denied, not_found,
  invalid_arg, internal} + permission{files|photos}` so the UI renders
  per-viewer empty-states. Proactive `files_permission` hint vs reactive
  authoritative denial.
- `checkPeerCapability(files,5)` fail-fast → `UPDATE_REQUIRED` verbatim
  instead of a timeout.

## Failure modes

- Missing capability/old build → update banner, list stays cached.
- Permission denied → files empty-state (separate from photos).
- Transfer timeout/cancel → staged `.part` pruned, retry is a new
  `transfer_id`.
