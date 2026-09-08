# ADR 0009 — Video streaming in the photos library (0.8.0)

## Context

The photos manager (0.7.0) browsed `MediaStore.Images` only: paged listing,
separate thumbs, full-file `photo-pull-req` → `photo-chunk` download to
`~/Downloads`, batch delete. Videos need to appear in the same timeline and
play on the Mac without a full download first, over the existing JSON-envelope
transport (base64 chunks, 1 MiB raw, 8 MiB body cap). A `<video>` element
cannot consume base64 data URLs for gigabyte files, and the two MediaStore
collections share row-ID space.

## Decision

- One mixed timeline under the existing `photos` capability (no new
  capability, no protocol break): entries gain `media_type photo|video`
  (absent = photo) and `duration_ms`; IDs are namespaced `img:<row>` /
  `vid:<row>` with legacy digits meaning image (`core.ParsePhotoID`, mirrored
  in Dart/Kotlin). Listing merges both collections in `(taken DESC,
  img-before-vid, row DESC)` order with a cursor both sides honor, so paging
  neither skips nor duplicates.
- Range streaming reuses `photo-chunk` with absolute `offset`/`chunk_index`/
  `total_chunks`: each chunk validates standalone under the unchanged
  sanitizer. `photo-pull-req` gains optional `offset/length` (0 = legacy full
  pull, length capped at 32 MiB per range). Old phones ignore the new fields
  and send full files; old Macs ignore the new entry fields.
- The Mac assembles chunks into sparse part files (existing seek-write path)
  and serves the open video over a loopback-only HTTP server
  (`127.0.0.1:0`, per-stream token, `http.ServeContent` for byte ranges).
  Reads past downloaded bytes pull the missing window on demand and block
  briefly, so scrubbing surfaces as buffering, never zero-filled holes.
- Gating: listing/thumb/delete stay at build 7; range pulls and
  `StartPhotoStream` require build 8, so 0.7.0 phones get `UPDATE_REQUIRED`
  verbatim instead of a timeout. `CurrentMinPeerBuild` stays 1.
- Thumbs stay JPEG (video = frame grab via `loadThumbnail` /
  `MediaMetadataRetriever`); downloads pick extensions from mime
  (`video-*.mp4`, never hardcoded `.jpg`); deletes route per-ID collection.
- Native reads never materialize whole files (`skip` loop + bounded reads;
  `openAssetFileDescriptor` for sizes) so multi-gigabyte videos cannot OOM
  the phone.

## Consequences

- Users browse one timeline with All/Photos/Videos filter, see duration
  badges, and play video inline while it streams; full download and delete
  keep working for both kinds.
- Cost: a loopback server in the Mac backend (new, small attack surface —
  127.0.0.1 only, constant-time per-stream tokens) and sparse-file bookkeeping
  per stream. MP4s without faststart still need their tail before the first
  frame; the player then shows buffering with a download fallback, never
  silence.
