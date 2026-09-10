# SMS Messages

How the Mac mirrors SMS conversations, receives live incoming SMS alerts, and
sends outgoing text messages through the paired Android phone.

## What it does

Full two-way SMS messaging:
- Browse conversation threads with contact names, unread indicators, and timestamps.
- View chronological message transcripts with standard speech bubbles (sent vs received).
- Compose and send SMS messages directly from Mac through Android's cellular network.
- Receive instant incoming SMS pushes via native `BroadcastReceiver` and `ContentObserver`
  over WebSocket (0 ms push latency, zero polling).

## How it flows

1. **Threads List**: Mac sends `sms-threads-req{cursor, limit}` → phone queries
   `Telephony.Sms.Conversations.CONTENT_URI` / `Telephony.Threads.CONTENT_URI` →
   resolves contact refs (`PhoneLookup` join on documented columns only:
   `_ID/DISPLAY_NAME/CONTACT_ID/NORMALIZED_NUMBER/NUMBER/PHOTO_ID`, multi-variant
   raw/normalized/national + bounded LRU, hits cached / misses uncached so
   permission grants heal; email senders via Email filter URI, fail-open on
   denied) → returns `sms-threads-resp{threads, next_cursor}`
   (limit 1..100, default 50). The Mac shows thread photos on demand via
   `contact-avatar-req{contact_id}` (contacts capability, versioned LRU) with
   a generic icon fallback. Page 2+ uses the `(date, _id)` keyset path.
   Mac enriches number-only rows from the cached directory
   (`core.PhonesEqual` + `normalized_number`, email case-insensitive) on ingest,
   cache hit, and push; `LookupContactForAddress` / `FindSMSThreadForAddress`
   back the UI join and Contacts→Message thread reuse.
2. **Messages in Thread**: Mac sends `sms-messages-req{thread_id, cursor, limit}` →
   phone queries `Telephony.Sms.CONTENT_URI` for `thread_id = ?` ordered by `date ASC` →
   returns `sms-messages-resp{thread_id, messages, next_cursor}`. Messages carry
   additive per-message `contact_name/contact_id/photo_version` (old Mac ignores;
   new Mac renders without a thread-cache round-trip) plus the same Mac fallback.
3. **Sending SMS**:
   - User types message in Mac compose bar and presses Return.
   - Mac sends `sms-send-req{req_id, client_id, recipient, body}`.
   - Phone receives request and calls Android `SmsManager.sendMultipartTextMessage`.
   - On Android 4.4+ (API 19+), `SmsManager` automatically inserts the sent message
     into `content://sms/sent`.
   - Phone returns `sms-send-resp{req_id, client_id, ok: true, message_id, thread_id}`.
   - Mac optimistically injects the message into the local transcript for instant feedback.
4. **Live Inbound SMS Push**:
   - `SmsReceiver` catches `android.provider.Telephony.SMS_RECEIVED` broadcasts.
   - Phone extracts message parts and instantly emits a `sms-push` envelope containing
     `SMSPushPayload{message, contact_name}` over the active WebSocket.
   - Mac receives the push, appends the message to the active thread, updates the unread badge,
     and fires the `messages:changed` Wails event. Zero polling at any stage.
5. **Database Change Detection**:
   - Android registers a `ContentObserver` on `Telephony.Sms.CONTENT_URI`.
   - Any external change (e.g. user deleting a thread on the phone) emits `sms-changed`
     to keep the Mac cache synchronized.

## Contract

- Capability `messages` (`packages/core/messages.go`, `packages/proto/messages.json`),
  stamped starting at build 13 (`0.13.0`).
- Wire types on `POST /messages` (+ WebSocket):
  - `sms-threads-req` → `sms-threads-resp`
  - `sms-messages-req` → `sms-messages-resp`
  - `sms-send-req` → `sms-send-resp`
  - `sms-push` (phone → Mac push event)
  - `sms-changed` (phone → Mac push event)
- Constraints:
  - `recipient`: max 64 characters (digits, `+`, `-`, spaces, etc.).
  - `body`: max 5000 characters.
  - `type`: `1` (received / inbox), `2` (sent).
  - `sub_id`: optional Android subscription id for dual-SIM (`SendSMSWithSubID`;
    empty = default). Non-numeric rejected.
  - Cursors are unified v2 keysets `v2.<b64sortkey>.<rowId>` (sortKey =
    dateMs; shared codec in `packages/core/sync_reliability.go`, mirrored in
    `SmsHandler.kt`). Legacy `dateMs[:rowId]` tolerated; corrupt v2 fails
    closed with `error_code: cursor_invalid` → drop cache, resync from `""`.
    Page 2+ uses the grouped keyset path so paging never duplicates
    page 1; message pages use `(date, _id)` ordering so equal-millisecond
    bursts don't skip or loop.

## Reliability

- Every `*-req` stamps a `nonce` (`crypto/rand`); responses echo `nonce` and
  correlate on `req_id`. Late responses after timeout/disconnect/change-wipe
  are dropped instead of poisoning caches.
- Disconnect fails all SMS pendings fast with `error_code: timeout`
  (never hangs to 8s/12s); reconnect invalidates caches and emits
  `messages:changed` so panes refetch (resync-on-connect).
- `sms-push` is deduped by `client_id`/`id` (bounded 500-entry seen-set, plus
  per-thread id check); pushes carry `contact_id`/`photo_version`/`client_id`/`seq`
  so thread rows update identity without a refetch. `resolveThreadId` falls
  back to `Threads.getOrCreateThreadId` (normalized) after exact match;
  legacy `thread_id=0` pushes skip cache mutation and
  heal via the follow-up `sms-changed` invalidate.
- Pure responses resolve their waiter with no fan-out; only `sms-push` /
  `sms-changed` emit. Panes follow `next_cursor` until empty.
- Sends are idempotent per `client_id`: the phone keeps a bounded seen-cache
  and reconciles `message_id`/`thread_id` against `content://sms/sent` after
  the radio accepts, so the Mac reconciles real ids instead of `Date.now()`.
- `ContentObserver` uses leading + trailing debounce (all `onChange`
  overloads) so rapid bursts coalesce instead of dropping the second edit.

## Key files

- Core: `packages/core/messages.go`, `packages/proto/messages.json`.
- Mac:
  - `apps/mac/backend/messages.go`: `Service.ListSMSThreads`, `ListSMSMessages`, `SendSMS`, `ingestMessagesBody`.
  - `apps/mac/frontend/src/contacts_messages_api.ts`: API bindings and types.
  - `apps/mac/frontend/src/components/MessagesPane.svelte`: macOS Messages 2-pane UI.
- Android:
  - `apps/android/android/app/src/main/kotlin/com/fuseitall/fuseitall/SmsReceiver.kt`: BroadcastReceiver for incoming SMS.
  - `apps/android/android/app/src/main/kotlin/com/fuseitall/fuseitall/SmsHandler.kt`: Native `SmsManager` and `ContentResolver` queries.
  - `apps/android/lib/features/messages/`: `sms_models.dart`, `sms_store.dart`, `sms_sync.dart`.
