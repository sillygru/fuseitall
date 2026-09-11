# Contacts

How the Mac browses the phone contact directory and accesses contact details
(phone numbers, email addresses, avatar thumbnails) with instant push updates.

## What it does

Fast, read-only contact directory synchronization. Browse contacts in an
Apple macOS Contacts-style split view, search instantly by name, phone number,
or email address, view avatars/monograms, and initiate SMS conversations
with a single click.

## How it flows

1. **List / Browse**: Mac sends `contacts-list-req{cursor, limit}` over the
   persistent WebSocket (or `POST /contacts`) → phone queries Android
   `ContactsContract` in `DISPLAY_NAME_PRIMARY ASC` order → returns
   `contacts-list-resp{entries, next_cursor, total_count}` (limit 1..100,
   default 50).
2. **Avatars**: Contact thumbnails under 64 KB are embedded inline in
   `avatar_b64` when present or fetched on demand via `contact-avatar-req{contact_id, high_res?}` →
   `contact-avatar-resp{data_b64, mime, photo_version}`. List rows use the
   thumbnail; the detail header requests `high_res: true` (display photo,
   downsampled to <=48KB JPEG). The Mac caches both under a bounded
   versioned LRU (300 entries, `photo_version` ETags) and patches the
   directory entry so panes update without a full refetch.
3. **Extended details**: entries carry `birthday_ms`, `anniversary_ms`,
   `organization{company,title,department}`, `nickname`, `postal{formatted}`,
   `note`, `website`, `photo_uri` + `photo_version` alongside phones/emails.
   Phones also carry additive `normalized_number` (provider `NORMALIZED_NUMBER`/
   E.164 when present) for cross-format matching. Phone-side search matches
   name, phone (Data-table `data1`), and email. Pagination uses `COLLATE NOCASE`
   on both sort and boundary with placeholder-chunked (200) batch queries.
   Detail sub-queries log counts only (no PII); a page with zero phones logs a
   warning distinguishing "no data on phone" from "query dropped".
3. **Change Detection (Zero Polling)**: Android registers a `ContentObserver` on
   `ContactsContract.Contacts.CONTENT_URI`. When any contact is added, modified,
   or deleted on the phone, the observer debounces notifications and pushes a
   `contacts-changed` envelope over the active WebSocket. The Mac receives this
   event, invalidates its in-memory cache, and emits the `contacts:changed` Wails
   event to refresh the UI immediately without any polling loops.
4. **Action Integration**: Clicking "Message" on any phone number in the contact card
   switches directly to the Messages pane with the recipient prefilled.
5. **Contact Deletion**: Mac user clicks "Delete…" or chooses "Delete Contact…" from
   the context menu → macOS HIG confirmation dialog confirms permanent removal →
   Mac sends `contact-delete-req{contact_id, lookup_key}` over the active connection →
   Android phone resolves contact via `CONTENT_LOOKUP_URI` (with `CONTENT_URI` `_ID`
   fallback) and calls `ContentResolver.delete` (requires `WRITE_CONTACTS`) → returns
   `contact-delete-resp{contact_id, ok}`. On success, Mac immediately removes the contact
   and its avatars from local cache and SQLite DB, and Android's native `ContentObserver`
   pushes `contacts-changed` to ensure peer state consistency.

## Contract

- Capability `contacts` (`packages/core/contacts.go`, `packages/proto/contacts.json`),
  stamped starting at build 13 (`0.13.0`).
- Wire types on `POST /contacts` (+ WebSocket):
  - `contacts-list-req` → `contacts-list-resp`
  - `contact-avatar-req` → `contact-avatar-resp`
  - `contact-delete-req` → `contact-delete-resp`
  - `contacts-changed` (phone → Mac push event)
- Constraints:
  - `contact_id`: alphanumeric + safe symbols, 1..64 runes.
  - `display_name`: max 128 runes.
  - `phones`: numbers max 32 chars, types `mobile`, `home`, `work`, etc.
  - `emails`: addresses max 128 chars.
  - `avatar_b64`: max 64 KB.
  - Cursors are unified v2 keysets `v2.<b64name>.<rowId>` with `_id` tiebreak
    (shared codec in `packages/core/sync_reliability.go`, mirrored in
    `ContactsHandler.kt`); legacy `b64name.rowId` and bare names tolerated,
    corrupt v2 fails closed with `error_code: cursor_invalid` → drop cache,
    resync from `""`. `total_count` is a separate
    `COUNT(*)` hint, never page size. Entries carry `lookup_key` +
    `last_updated_ms` watermarks; avatars carry `photo_version` ETags.

## Reliability

- Every `*-req` stamps a `nonce`; responses echo it and correlate on `req_id`.
  Late responses after timeout/disconnect/change-wipe are dropped instead of
  resurrecting stale pages. `query` rides the wire (pinned into paging) via
  `ListContactsWithQuery`; the legacy `ListContacts` wrapper means `""`.
- Disconnect fails all contacts/avatar pendings fast with
  `error_code: timeout`; reconnect invalidates the directory and emits
  `contacts:changed` so panes refetch (resync-on-connect). Panes follow
  `next_cursor` until empty (full sync, 20-page cap) with progress.
- Filtered (`query != ""`) pages never pollute the unfiltered directory
  cache; first pages replace instead of appending (fixes forceRefresh
  duplication); `CursorGen` drops list-while-changed races.
- Pure responses resolve their waiter with no fan-out; only
  `contacts-changed` emits, fixing stale fast-path refetch churn.
- `contacts-changed` clears the directory but keeps avatars offline until the
  next list prunes non-survivors; avatar fetches are versioned LRU
  (`photo_version`) and phone-side downsampled to <=48KB JPEG so large photos
  fit the 64KB cap instead of degrading to silent null.
- `ContentObserver` uses leading + trailing debounce (all `onChange`
  overloads). Permission denial freezes (never wipes) the cache and surfaces
  `permission_denied` with the `contacts`/`sms` domain.

## Key files

- Core: `packages/core/contacts.go`, `packages/proto/contacts.json`.
- Mac:
  - `apps/mac/backend/contacts.go`: `Service.ListContacts`, `GetContactAvatar`, `SearchContacts`, `ingestContactsBody`.
  - `apps/mac/frontend/src/contacts_messages_api.ts`: API bindings and types.
  - `apps/mac/frontend/src/components/ContactsPane.svelte`: macOS Contacts split-view UI.
- Android:
  - `apps/android/android/app/src/main/kotlin/com/fuseitall/fuseitall/ContactsHandler.kt`: Native `ContentResolver` query and `ContentObserver`.
  - `apps/android/lib/features/contacts/`: `contact_models.dart`, `contact_store.dart`, `contact_sync.dart`.
