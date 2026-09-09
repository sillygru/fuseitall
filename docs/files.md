# Files

How the Mac browses and manages phone files. Request/response over the same
lane, chunked transfer with verified commit, sandboxed paths. No drag size
limits: Finder drops stream from disk with a negotiated stride (4 MiB chunks
when the peer advertises `files-large-chunk`, else legacy 1 MiB) and up to 8
files in flight per batch, with delivery-confirmation waits pipelined off
the send slots so commits overlap the next streams; browser drops stream
in 1 MiB slices (one slice in tab memory at a time); the hard ceiling is
8 GiB per file.

## What it does

List directories, create/rename/delete, download from the phone and upload
to it, with progress, delivery confirmation, retry-from-offset, and offline
caching. Drag-out to Finder uses a staged exact-path handoff, never an
unprompted background download.

## How it flows

1. Mac `backend/files.go:ListPhoneFiles` sends `file-list{path, req_id}`;
   phone `features/files/file_sync.dart` + `file_system.dart` answers one
   `file-list-resp{req_id, entries}` over the same `/files` lane
   (`req_id` 1..64 correlation, `pendingLists` 8s timeout, `lastList`
   offline cache, entries ≤500).
2. Mutations (`file-mkdir/delete/rename`) ride the same lane; `rename` is
   same-dir only.
3. Download: Mac `RequestPhoneFile` → `file-pull-req` → phone streams
   `file-chunk{transfer_id, offset==chunk_index*stride, total_chunks}`
   (stride 1 MiB legacy or 4 MiB negotiated via the Mac's
   `files-large-chunk` capability + build >= 11, fail-closed to legacy;
   receivers accept both; phone reads with one handle and attaches a
   single-pass sha256 on the last chunk, serving up to 4 pulls
   concurrently from a bounded queue so one large file never blocks later
   ones); Mac stages to `os.TempDir()/fuseitall-files` (0700) through one
   open handle per transfer with an incremental sha256 (re-read fallback on
   gaps), fsyncs the final chunk, and atomically renames `.part.<id>` →
   final (`-<id6>` on collision), verifying `size` and `sha256` on the last
   chunk. Default destination `~/Downloads`. Startup sweeps orphan
   `.part.*` in the staging dir.
4. Upload, no size gates: drops land in the current phone folder (`path`
   state), or the hovered subfolder row (`data-drop-path`), never
   `~/Downloads` (Mac-side download dir only). Finder drops stat first via
   `StatLocalFiles`, then prompt, then stream from disk with single-pass
   sha256 (`UploadLocalFilesWithPolicy` / `UploadLocalFileToRemotePath`).
   Multi-file drops resolve conflicts first (prompts are local comparisons,
   skips never leave the UI) and send the survivors in ONE
   `UploadDecidedFiles` call with per-file exact paths + wire policies, so
   they share a single 8-wide pipelined run instead of one ack-confirmed
   round-trip per file. Folder drops stream discovery into sending: the
   walk feeds tasks as files are found (first byte after the first
   stat+open, not after the whole walk), each file carries its walk-time
   stat (verified against the open handle, no path re-stat), and mkdir
   gates order every first chunk behind its parent mkdir on the shared WS.
   Sends run 8-wide with the ack wait pipelined off the send slots
   (`sendUploadChunks` → release slot → `completeUploadSend`), so small-file
   bursts overlap commits with fresh streams; single-file uploads keep the
   blocking ack-confirmed return. Small files allocate only their bytes
   (not a full stride) and batched 1-chunk files converge on one completion
   emit instead of start+chunk+done. Stride + ack-support snapshot once per
   batch. Browser drops stream in 1 MiB slices (`BeginBrowserUpload` →
   Browser drops stream in 1 MiB slices (`BeginBrowserUpload` →
   `SendBrowserChunk*`, one slice in tab memory at a time) and look
   identical on the wire. Parent dirs are mkdir'd idempotently first.
5. Conflicts: colliding names prompt before any bytes are sent — files:
   Overwrite, Overwrite if newer, Keep both (`name (2).ext` via
   `core.KeepBothName`), Skip, Stop, each with Apply to all; folders: Merge
   (keep-both inside), Overwrite matching files, Stop, with Apply to all
   folders. Skip/Stop send nothing; Keep both resolves to a fresh path and
   sends legacy (no policy). Overwrite/if_newer ride `file-chunk{policy,
   source_mtime}` (`packages/proto/files.json`, `core.IsSourceNewer`:
   mtime seconds first, size tiebreak); old peers ignore the additive
   fields and keep the legacy suffixed copy. The phone enforces the policy
   on the final chunk from staged `.part` bytes, so a mid-batch change on
   the phone cannot clobber (TOCTOU-safe). Inner folder files follow the
   folder choice without extra listings.
6. Delivery confirmation: receivers with the `files-ack` capability verify
   sha256 over the staged bytes and answer `file-ack{transfer_id, ok,
   error}`; the sender marks done only on `ok` (5min waiter) and surfaces
   nacks/timeouts as transfer errors. Peers without the capability stay
   fire-and-forget (done after the last send). The phone validates every
   chunk strictly (offset/count/length mirror `core.SanitizeFileChunk`);
   malformed chunks are dropped before touching disk. The phone stages each
   upload through one persistent handle with an incremental sha256 (re-read
   fallback on gaps/duplicates), so bursts skip per-chunk open/close and
   the commit-time re-hash. Setup (path validation, recovery sweep, parent
   create, handle open) runs once per transfer; later chunks go straight to
   hashing and writing. Chunk commits run concurrently across transfers
   (strict FIFO per transfer id, 8-wide with a bounded queue): one file's
   disk flush never blocks another's. The ledger records committed bytes
   only, so stat/resume answers stay truthful when queued chunks drop;
   committed ids are remembered (bounded) so late duplicates racing an ack
   drop instead of staging orphan parts, cancelled generations discard
   instead of acking, and stat answers "nothing missing" for finished
   transfers.
7. Retry: a failed upload keeps its session 30min and the row offers Retry.
   `ResumeUpload` (native) and `ResumeBrowserUpload` + `RehashBrowserChunk`
   (tab) ask `file-stat-req` → `file-stat-resp{next_chunk}` and resend only
   the missing tail under the same transfer id; unknown/timeout stats
   restart from zero. A locally changed file fails closed. Browser prefix
   bytes are re-hashed locally, never re-sent.
 8. Cancel: `CancelTransfer`/`AbortBrowserUpload` mark the record, send
    `file-cancel{transfer_id, path?}`, and delete staged parts on both ends.
    Send loops poll cancellation per chunk. Swept retry sessions emit
    file-cancel too, so abandoned uploads cannot litter phone storage.
    Phone-side replaces commit via backup rename with crash recovery scoped
    to the target directory (fresh backups restore, stale ones drop).
    Batches group one user drop: `BeginUploadBatch(total_files,total_bytes)`
    → per-file transfers carry `batch_id` → `GetTransferBatches` derives
    `done_files/done_bytes/current_path/progress` live from members.
    `CancelUploadBatch` cancels every running member at once (never
    resumable). Batched members prune with the batch (60s window) instead
    of the 5s solo window so totals stay accurate. Batch state is Mac-local
    only — transfer_ids ride the wire unchanged, no proto change.
 9. Progress: `GetTransfers` (5s prune, session sweeps) → `transfers:changed`
    with `source` (local|browser) and `resumable`; failed resumable uploads
    show Retry. Drag-out: `PrepareDownloadForDrag` (staged-only) +
    `DownloadFileToExactPath` for `NSFilePromiseProvider` (5min waiter).
    Batch progress rides `batches:changed` with the same push (no polling):
    total bar by bytes, `File X of Y`, current filename, `done/total` bytes,
    per-file rows with per-file Cancel plus Cancel batch. Chunk sends retry
    3x with backoff for transient WS blips; phone nacks surface verbatim
    (sha mismatch, path escapes root, disk full) instead of generic
    `write failed`, and stay resumable via `file-stat-req` tail retry.
10. Home-folder guard: drops targeting phone home (`""`) resolve first via
    `ensureUploadTarget`. A Mac-local default (`Get/SetDefaultUploadDir`,
    `upload_prefs.json`, never synced) redirects silently; otherwise the UI
    asks once with the live folder list (Download recommended): Use
    selected (+ optional remember-as-default), Upload here anyway, or
    Cancel.

## Contract

- Capability `files`, gate `build>=5` (`packages/core/files.go`,
  `packages/proto/files.json`); additive `files-ack` capability gates
  confirmation (absence = legacy, never an update prompt); additive
  `files-large-chunk` capability gates 4 MiB sends (absence = legacy 1 MiB,
  receivers accept both).
- Types on `POST /files` (+ WS): `file-list → file-list-resp`,
  `file-mkdir/delete/rename`, `file-chunk`, `file-pull-req`,
  `file-ack`, `file-cancel`, `file-stat-req → file-stat-resp`.
- Chunks: up to 4 MiB raw (~5.6 MiB b64) under `MaxBodyBytes 8MiB`
  (negotiated: 4 MiB iff the peer advertises `files-large-chunk` at build
  >= 11, else legacy 1 MiB; receivers validate both strides); total ≤8 GiB;
  `transfer_id` 16..64 hex (`crypto/rand`); idempotent by
  `transfer_id+offset`; `total_chunks` must stay consistent.

## Key files

- Core: `files.go` (sandbox, chunk validation, policy/ack/cancel/stat).
- Mac: `backend/files.go`, `backend/files_conflict.go` (policy + slice
  sessions + `UploadDecidedFiles` batch), `backend/files_ack.go` (ack/cancel ingest), `backend/files_resume.go`
  (retry), `backend/files_batch.go` (batch grouping + `batches:changed`),
  `backend/upload_prefs.go` (Mac-only default upload dir),
  `backend/service_ws.go`,
  `backend/file_drag_darwin.{go,h,m}`,
  `frontend/src/components/FileManager/` (`FileManager`,
  `FileConflictDialog`, `FileTransfers`, `FileModals`, `UploadTargetDialog` svelte),
  `frontend/src/lib/fileConflict.ts`,
  `frontend/src/backend.ts` (`listPhoneFiles/requestPhoneFile/getTransfers`,
  `getTransferBatches`, slice + resume wrappers).
- Android: `features/files/file_sync.dart`, `file_system.dart`,
  `file_models.dart`.

## Rules & limits

- Sandbox: rel paths only — no absolute, no `..`, no `\`, no NUL/control;
  total ≤1024, component ≤255 (`filepath.IsLocal + Clean`).
- Errors carry `error/error_code{permission_denied, not_found,
  invalid_arg, internal} + permission{files|photos}` so the UI renders
  per-viewer empty-states. Proactive `files_permission` hint vs reactive
  authoritative denial.
- Empty Finder drops (no POSIX path: TCC denial, broken symlink, moved file,
  iCloud placeholder, Wails skew) are filtered at the `files-dropped` seam
  and in `uploadLocalPaths`; survivors stat via `StatLocalFiles`. Backend
  `UploadDecidedFiles`/`StatLocalFiles` fail closed on empty with sentinel
  `ErrInvalidLocal` (`invalid local path: empty drop — re-select the file in
  Finder`), branchable via `errors.Is`, never on message substrings.
- `checkPeerCapability(files,5)` fail-fast → `UPDATE_REQUIRED` verbatim
  instead of a timeout.
- Upload sessions (slice + retry) expire after 30min idle; stat answers
  come from an in-memory ledger capped at 64 transfers.

## Failure modes

- Missing capability/old build → update banner, list stays cached; old
  peers additionally skip acks (legacy done) and answer no stats (retry
  restarts from zero, same transfer id).
- Permission denied → files empty-state (separate from photos).
- Mid-transfer drop → error record + retained session (30min) with Retry;
  staged parts swept on fail/prune/sweep; sha mismatch → stage deleted +
  nack, never committed.
