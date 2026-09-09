# Photos

How the Mac browses the phone photo library and streams video without a
full download first. One mixed timeline; thumbs, full-res, and ranges share
one chunk shape.

## What it does

Paged mixed photo+video timeline with All/Photos/Videos filter, duration
badges, JPEG thumbs, full-file download, batch delete, and inline video
playback that streams on demand (scrub = buffering, never holes).

## How it flows

1. List: Mac `ListPhonePhotos{cursor, limit}` → phone merges
   `MediaStore.Images + Video` in `(taken DESC, img-before-vid, row DESC)`
   order → `photo-list-resp{entries}` (`limit` 1..200, default 100;
   frontend 50/page + infinite sentinel + 30s `paneCache` + 5s empty TTL +
   3×1s retry for indexer lag). IDs are `img:<row>` / `vid:<row>`
   (legacy digits = image; `core.ParsePhotoID` mirrored in Dart/Kotlin).
2. Thumbs: separate `photo-thumb-req{photo_id, size 64..1024}` →
   `photo-thumb-resp` (always JPEG; video = frame grab via `loadThumbnail` /
   `MediaMetadataRetriever`; ≤2MiB b64). Mac LRU-200 RAM-only, 3-concurrent
   `IntersectionObserver` 300px; one bad thumb never fails the page.
3. Full-res: `photo-pull-req{photo_id}` (no offset = legacy full) →
   `photo-chunk*` (dedicated type, isolated from `file-chunk`; same 1MiB +
   `sha256` + atomic rename into `fuseitall-photos`; extensions from mime
   via `photoExtForMime`, never hardcoded `.jpg`).
4. Streaming: `photo-pull-req{offset, length≤32MiB}` → range chunks with
   absolute `offset/chunk_index/total_chunks` (each validates standalone).
   Mac fills sparse `.part` + `Ranges[]` (`rangeAdd/rangeCovered`) and
   serves the open video over a loopback-only HTTP server
   (`127.0.0.1:0`, per-stream `crypto/rand` token + `ConstantTimeCompare`,
   `http.ServeContent` single-range). Reads past downloaded bytes pull the
   missing window and block briefly (`waitPhotoRange`, channel-broadcast —
   no polling); MP4s without faststart show buffering + download fallback.

## Contract

- Capability `photos` (`packages/core/photos.go`,
  `packages/proto/photos.json`): listing/thumb/delete at build 7, range
  pulls + `StartPhotoStream` at build 8 (`CurrentMinPeerBuild` stays 1).
- Types on `POST /photos` (+ WS): `photo-list → photo-list-resp`,
  `photo-thumb-req → photo-thumb-resp`, `photo-pull-req → photo-chunk*`,
  `photo-delete → photo-delete-resp{results[{photo_id,ok,error,error_code}]}`.
- Entries: `media_type photo|video` (absent = photo) + `duration_ms`;
  `photo_id` opaque 1..128, no `/`. Deletes batch ≤200 deduped; partial
  success is normal.

## Key files

- Core: `photos.go`.
- Mac: `backend/photos.go`, `backend/photos_stream.go`
  (`StartPhotoStream/GetPhotoStreamURL/blockingStreamReader`),
  `backend/photos_support.go` (staging root, `rangeAdd`, LRU),
  `frontend/src/components/FileManager/PhotosViewer.svelte`,
  `frontend/src/backend.ts`.
- Android: `features/photos/photo_sync.dart`, `photo_store.dart`,
  `photo_models.dart` (+ Kotlin `skip`-loop bounded reads,
  `openAssetFileDescriptor` for sizes — never whole-file).

## Rules & limits

- Native reads never materialize whole files, so multi-GB videos can't OOM.
- Downloads pick extensions from mime (`video-*.mp4`); stems sanitize
  `:` → `_`.
- Loopback server is `127.0.0.1` only — the one small added attack surface,
  gated per-stream.
- Permission errors use `error_code=permission_denied + permission=photos`
  for a photos-specific empty-state.

## Failure modes

- 0.7.0 phone on range pull → `UPDATE_REQUIRED` verbatim, not a timeout.
- Old Mac ignores new entry fields; old phone ignores `offset/length` and
  sends full files.
- Unseekable/slow tail → buffering + full-download fallback, never silence.
