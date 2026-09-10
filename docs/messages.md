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
   resolves contact names via `ContactsContract` → returns `sms-threads-resp{threads, next_cursor}`
   (limit 1..100, default 50).
2. **Messages in Thread**: Mac sends `sms-messages-req{thread_id, cursor, limit}` →
   phone queries `Telephony.Sms.CONTENT_URI` for `thread_id = ?` ordered by `date ASC` →
   returns `sms-messages-resp{thread_id, messages, next_cursor}`.
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
