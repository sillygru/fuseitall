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
   `avatar_b64` or fetched on demand via `contact-avatar-req{contact_id}` →
   `contact-avatar-resp{data_b64, mime}`. The Mac caches avatars in memory.
3. **Change Detection (Zero Polling)**: Android registers a `ContentObserver` on
   `ContactsContract.Contacts.CONTENT_URI`. When any contact is added, modified,
   or deleted on the phone, the observer debounces notifications and pushes a
   `contacts-changed` envelope over the active WebSocket. The Mac receives this
   event, invalidates its in-memory cache, and emits the `contacts:changed` Wails
   event to refresh the UI immediately without any polling loops.
4. **Action Integration**: Clicking "Message" on any phone number in the contact card
   switches directly to the Messages pane with the recipient prefilled.

## Contract

- Capability `contacts` (`packages/core/contacts.go`, `packages/proto/contacts.json`),
  stamped starting at build 13 (`0.13.0`).
- Wire types on `POST /contacts` (+ WebSocket):
  - `contacts-list-req` → `contacts-list-resp`
  - `contact-avatar-req` → `contact-avatar-resp`
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
  `contacts:changed` so panes refetch (resync-on-connect).
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
