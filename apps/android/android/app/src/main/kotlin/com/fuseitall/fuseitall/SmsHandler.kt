// SPDX-License-Identifier: AGPL-3.0-only

package com.fuseitall.fuseitall

import android.app.Activity
import android.app.PendingIntent
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.content.IntentFilter
import android.database.ContentObserver
import android.database.Cursor
import android.net.Uri
import android.os.Build
import android.os.Handler
import android.os.Looper
import android.provider.ContactsContract
import android.provider.Telephony
import android.telephony.PhoneNumberUtils
import android.telephony.SmsManager
import android.util.Base64
import android.util.Log
import androidx.core.content.ContextCompat
import io.flutter.plugin.common.BinaryMessenger
import io.flutter.plugin.common.EventChannel
import io.flutter.plugin.common.MethodCall
import io.flutter.plugin.common.MethodChannel
import java.util.concurrent.Executors

class SmsHandler(
    private val context: Context,
    messenger: BinaryMessenger
) : MethodChannel.MethodCallHandler, EventChannel.StreamHandler {

    private val executor = Executors.newFixedThreadPool(2)
    private val mainHandler = Handler(Looper.getMainLooper())
    private var observer: ContentObserver? = null

    // contactRefCache memoizes address -> ContactRef for the process lifetime
    // (bounded LRU, 256 entries). Threads/messages pages issue N+1 PhoneLookup
    // queries for the same few addresses; without this each 50-thread page
    // costs 50 provider round-trips. Keyed by raw address; values are
    // fail-open (misses cached as empty to avoid repeat lookups for short
    // codes). Guarded by its own monitor; never holds PII in logs.
    private val contactRefCache = LinkedHashMap<String, ContactRef>(256, 0.75f, true)

    private fun contactRefCacheGet(address: String): ContactRef? {
        synchronized(contactRefCache) { return contactRefCache[address] }
    }

    private fun contactRefCachePut(address: String, ref: ContactRef) {
        synchronized(contactRefCache) {
            contactRefCache[address] = ref
            while (contactRefCache.size > 256) {
                val oldest = contactRefCache.keys.firstOrNull() ?: break
                contactRefCache.remove(oldest)
            }
        }
    }

    init {
        instance = this
        MethodChannel(messenger, "fuseitall/sms").setMethodCallHandler(this)
        EventChannel(messenger, "fuseitall/smsEvents").setStreamHandler(this)
    }

    override fun onMethodCall(call: MethodCall, result: MethodChannel.Result) {
        when (call.method) {
            "queryThreads" -> {
                val cursor = call.argument<String>("cursor") ?: ""
                val limit = (call.argument<Int>("limit") ?: 50).coerceIn(1, 100)
                executor.execute {
                    try {
                        val res = queryThreads(cursor, limit)
                        mainHandler.post { result.success(res) }
                    } catch (e: SecurityException) {
                        mainHandler.post { result.error("PERMISSION_DENIED", e.message, null) }
                    } catch (e: Exception) {
                        mainHandler.post { result.error("QUERY_FAILED", e.message, null) }
                    }
                }
            }
            "queryMessages" -> {
                val threadId = (call.argument<Number>("thread_id")?.toLong() ?: 0L)
                val cursor = call.argument<String>("cursor") ?: ""
                val limit = (call.argument<Int>("limit") ?: 50).coerceIn(1, 100)
                executor.execute {
                    try {
                        val res = queryMessages(threadId, cursor, limit)
                        mainHandler.post { result.success(res) }
                    } catch (e: SecurityException) {
                        mainHandler.post { result.error("PERMISSION_DENIED", e.message, null) }
                    } catch (e: Exception) {
                        mainHandler.post { result.error("QUERY_FAILED", e.message, null) }
                    }
                }
            }
            "sendSms" -> {
                val recipient = call.argument<String>("recipient") ?: ""
                val body = call.argument<String>("body") ?: ""
                val clientId = call.argument<String>("client_id") ?: ""
                val subId = (call.argument<Number>("sub_id")?.toInt()
                    ?: call.argument<String>("sub_id")?.toIntOrNull() ?: -1)
                executor.execute {
                    try {
                        val res = sendSms(recipient, body, clientId, subId)
                        mainHandler.post { result.success(res) }
                    } catch (e: SecurityException) {
                        mainHandler.post { result.error("PERMISSION_DENIED", e.message, null) }
                    } catch (e: Exception) {
                        mainHandler.post { result.error("SEND_FAILED", e.message, null) }
                    }
                }
            }
            "markRead" -> {
                val threadId = (call.argument<Number>("thread_id")?.toLong() ?: 0L)
                val messageId = (call.argument<Number>("message_id")?.toLong() ?: 0L)
                val address = call.argument<String>("address") ?: ""
                executor.execute {
                    try {
                        val res = markRead(threadId, messageId, address)
                        mainHandler.post { result.success(res) }
                    } catch (e: SecurityException) {
                        mainHandler.post { result.error("PERMISSION_DENIED", e.message, null) }
                    } catch (e: Exception) {
                        mainHandler.post { result.error("MARK_READ_FAILED", e.message, null) }
                    }
                }
            }
            else -> result.notImplemented()
        }
    }

    override fun onListen(arguments: Any?, events: EventChannel.EventSink?) {
        eventSink = events
        val obs = object : ContentObserver(mainHandler) {
            private var lastNotify = 0L
            private var trailingPosted = false
            private val trailing = Runnable {
                trailingPosted = false
                lastNotify = System.currentTimeMillis()
                eventSink?.success(mapOf(
                    "type" to "changed",
                    "changed_at" to lastNotify
                ))
            }
            private fun onDirty() {
                val now = System.currentTimeMillis()
                if (now - lastNotify > 500) {
                    // Leading edge: notify immediately, suppress trailing.
                    mainHandler.removeCallbacks(trailing)
                    trailingPosted = false
                    lastNotify = now
                    try {
                        eventSink?.success(mapOf(
                            "type" to "changed",
                            "changed_at" to now
                        ))
                    } catch (_: Exception) {}
                } else if (!trailingPosted) {
                    // Burst: coalesce into one trailing flush so the second
                    // edit in a rapid pair is never silently dropped.
                    trailingPosted = true
                    mainHandler.postDelayed(trailing, 600)
                }
            }
            override fun onChange(selfChange: Boolean) {
                super.onChange(selfChange)
                onDirty()
            }
            override fun onChange(selfChange: Boolean, uri: Uri?) {
                super.onChange(selfChange, uri)
                onDirty()
            }
            override fun onChange(selfChange: Boolean, uri: Uri?, flags: Int) {
                super.onChange(selfChange, uri, flags)
                onDirty()
            }
        }
        observer = obs
        try {
            context.contentResolver.registerContentObserver(
                Telephony.Sms.CONTENT_URI,
                true,
                obs
            )
        } catch (_: Exception) {}
    }

    override fun onCancel(arguments: Any?) {
        observer?.let {
            try {
                context.contentResolver.unregisterContentObserver(it)
            } catch (_: Exception) {}
        }
        observer = null
        eventSink = null
    }

    private fun queryThreads(cursor: String, limit: Int): Map<String, Any> {
        val resolver = context.contentResolver
        // Cursor is v2 "v2.<b64date>.<rowId>" or a legacy "dateMs[:rowId]".
        // Non-empty numeric cursors use the grouped keyset path so page 2
        // never duplicates page 1.
        val (cursorKey, cursorRowId) = decodeKeysetCursor(cursor)
        val cursorDate = cursorKey.toLongOrNull() ?: 0L
        if (cursorDate > 0) {
            return queryThreadsGrouped(cursorDate, cursorRowId, limit)
        }
        // Query distinct threads from content://sms/conversations
        val uri = Uri.parse("content://sms/conversations")
        val projection = arrayOf(
            "thread_id",
            "msg_count",
            "snippet"
        )

        val threads = mutableListOf<Map<String, Any>>()
        val cursorObj: Cursor? = try {
            resolver.query(uri, projection, null, null, "date DESC LIMIT $limit")
        } catch (_: Exception) {
            null
        }

        if (cursorObj != null) {
            cursorObj.use { c ->
                val tidCol = c.getColumnIndex("thread_id")
                val countCol = c.getColumnIndex("msg_count")
                val snippetCol = c.getColumnIndex("snippet")

                while (c.moveToNext()) {
                    val tid = if (tidCol >= 0) c.getLong(tidCol) else 0L
                    val count = if (countCol >= 0) c.getInt(countCol) else 0
                    val snippet = if (snippetCol >= 0) c.getString(snippetCol) ?: "" else ""

                    // Fetch address, date, read status for this thread
                    val details = getThreadDetails(tid)

                    val threadMap = mutableMapOf<String, Any>(
                        "thread_id" to tid,
                        "address" to details.address,
                        "snippet" to snippet,
                        "date" to details.date,
                        "message_count" to count,
                        "unread_count" to details.unreadCount,
                        "read" to details.read
                    )
                    if (details.contactName.isNotEmpty()) {
                        threadMap["contact_name"] = details.contactName
                    }
                    if (details.contactId.isNotEmpty()) {
                        threadMap["contact_id"] = details.contactId
                    }
                    if (details.photoVersion.isNotEmpty()) {
                        threadMap["photo_version"] = details.photoVersion
                    }
                    threads.add(threadMap)
                }
            }
        } else {
            // Fallback: group by thread_id directly on content://sms
            val fallbackCursor: Cursor? = resolver.query(
                Telephony.Sms.CONTENT_URI,
                arrayOf(
                    Telephony.Sms.THREAD_ID,
                    Telephony.Sms.ADDRESS,
                    Telephony.Sms.BODY,
                    Telephony.Sms.DATE,
                    Telephony.Sms.READ
                ),
                null,
                null,
                "${Telephony.Sms.DATE} DESC"
            )

            fallbackCursor?.use { fc ->
                val tidCol = fc.getColumnIndex(Telephony.Sms.THREAD_ID)
                val addrCol = fc.getColumnIndex(Telephony.Sms.ADDRESS)
                val bodyCol = fc.getColumnIndex(Telephony.Sms.BODY)
                val dateCol = fc.getColumnIndex(Telephony.Sms.DATE)
                val readCol = fc.getColumnIndex(Telephony.Sms.READ)

                val seenThreads = mutableSetOf<Long>()
                while (fc.moveToNext() && threads.size < limit) {
                    val tid = if (tidCol >= 0) fc.getLong(tidCol) else 0L
                    if (tid == 0L || !seenThreads.add(tid)) continue

                    val addr = if (addrCol >= 0) fc.getString(addrCol) ?: "" else ""
                    val body = if (bodyCol >= 0) fc.getString(bodyCol) ?: "" else ""
                    val date = if (dateCol >= 0) fc.getLong(dateCol) else 0L
                    val read = if (readCol >= 0) fc.getInt(readCol) == 1 else true
                    val ref = resolveContactRef(addr)

                    val threadMap = mutableMapOf<String, Any>(
                        "thread_id" to tid,
                        "address" to addr,
                        "snippet" to body,
                        "date" to date,
                        "message_count" to 1,
                        "unread_count" to (if (read) 0 else 1),
                        "read" to read
                    )
                    if (ref.name.isNotEmpty()) threadMap["contact_name"] = ref.name
                    if (ref.contactId.isNotEmpty()) threadMap["contact_id"] = ref.contactId
                    if (ref.photoVersion.isNotEmpty()) threadMap["photo_version"] = ref.photoVersion
                    threads.add(threadMap)
                }
            }
        }

        // First-page cursor carries date:0 (row id unknown on the
        // conversations fast path); the grouped path parses the date part.
        var nextCursor = ""
        if (threads.size == limit) {
            val lastDate = (threads.last()["date"] as? Number)?.toLong() ?: 0L
            nextCursor = keysetCursor(lastDate.toString(), 0)
        }

        try {
            if (threads.isNotEmpty()) {
                var resolved = 0
                for (t in threads) {
                    if ((t["contact_id"] as? String)?.isNotEmpty() == true ||
                        (t["contact_name"] as? String)?.isNotEmpty() == true
                    ) resolved++
                }
                Log.d("SmsHandler", "threads=" + threads.size + " resolved=" + resolved)
                if (resolved == 0) {
                    Log.w("SmsHandler", "zero thread contacts resolved; check READ_CONTACTS grant")
                }
            }
        } catch (_: Exception) {}

        return mapOf(
            "threads" to threads,
            "next_cursor" to nextCursor
        )
    }

    // queryThreadsGrouped serves page 2+ via a keyset on content://sms:
    // rows older than cursorDate, grouped by thread, newest first. This
    // honors the cursor the conversations fast path cannot express.
    private fun queryThreadsGrouped(cursorDate: Long, cursorRowId: Long, limit: Int): Map<String, Any> {
        val threads = mutableListOf<Map<String, Any>>()
        val seen = LinkedHashMap<Long, MutableMap<String, Any>>()
        var lastDate = 0L
        var lastRowId = 0L
        // Keyset with _id tiebreak so equal-millisecond bursts neither
        // duplicate nor drop across pages (was DATE<? only).
        val sel: String
        val selArgs: Array<String>
        if (cursorRowId > 0) {
            sel = "(${Telephony.Sms.DATE} < ? OR (${Telephony.Sms.DATE} = ? AND ${Telephony.Sms._ID} < ?))"
            selArgs = arrayOf(cursorDate.toString(), cursorDate.toString(), cursorRowId.toString())
        } else {
            sel = "${Telephony.Sms.DATE} < ?"
            selArgs = arrayOf(cursorDate.toString())
        }
        try {
            context.contentResolver.query(
                Telephony.Sms.CONTENT_URI,
                arrayOf(
                    Telephony.Sms._ID,
                    Telephony.Sms.THREAD_ID,
                    Telephony.Sms.ADDRESS,
                    Telephony.Sms.BODY,
                    Telephony.Sms.DATE,
                    Telephony.Sms.READ
                ),
                sel,
                selArgs,
                "${Telephony.Sms.DATE} DESC, ${Telephony.Sms._ID} DESC LIMIT 500"
            )?.use { fc ->
                val idCol = fc.getColumnIndex(Telephony.Sms._ID)
                val tidCol = fc.getColumnIndex(Telephony.Sms.THREAD_ID)
                val addrCol = fc.getColumnIndex(Telephony.Sms.ADDRESS)
                val bodyCol = fc.getColumnIndex(Telephony.Sms.BODY)
                val dateCol = fc.getColumnIndex(Telephony.Sms.DATE)
                val readCol = fc.getColumnIndex(Telephony.Sms.READ)
                while (fc.moveToNext() && seen.size < limit) {
                    val rowId = if (idCol >= 0) fc.getLong(idCol) else 0L
                    val tid = if (tidCol >= 0) fc.getLong(tidCol) else 0L
                    if (tid == 0L || seen.containsKey(tid)) continue
                    val addr = if (addrCol >= 0) fc.getString(addrCol) ?: "" else ""
                    val body = if (bodyCol >= 0) fc.getString(bodyCol) ?: "" else ""
                    val date = if (dateCol >= 0) fc.getLong(dateCol) else 0L
                    val read = if (readCol >= 0) fc.getInt(readCol) == 1 else true
                    val ref = resolveContactRef(addr)
                    val m = mutableMapOf<String, Any>(
                        "thread_id" to tid,
                        "address" to addr,
                        "snippet" to body,
                        "date" to date,
                        "message_count" to 1,
                        "unread_count" to (if (read) 0 else 1),
                        "read" to read
                    )
                    if (ref.name.isNotEmpty()) m["contact_name"] = ref.name
                    if (ref.contactId.isNotEmpty()) m["contact_id"] = ref.contactId
                    if (ref.photoVersion.isNotEmpty()) m["photo_version"] = ref.photoVersion
                    seen[tid] = m
                    lastDate = date
                    lastRowId = rowId
                }
            }
        } catch (_: Exception) {
        }
        threads.addAll(seen.values)
        val nextCursor = if (threads.size == limit) keysetCursor(lastDate.toString(), lastRowId) else ""
        return mapOf("threads" to threads, "next_cursor" to nextCursor)
    }

    // keysetCursor builds the canonical v2 cursor shared with core
    // EncodeKeysetCursor: "v2.<base64url(sortKey)>.<rowId>". SortKey is the
    // decimal dateMs for SMS; the row id tiebreak survives equal-millisecond
    // bursts. Mirrors core.DecodeKeysetCursor on parse.
    private fun keysetCursor(sortKey: String, rowId: Long): String {
        val id = if (rowId < 0) 0L else rowId
        val enc = Base64.encodeToString(
            sortKey.toByteArray(Charsets.UTF_8),
            Base64.URL_SAFE or Base64.NO_WRAP or Base64.NO_PADDING
        )
        return "v2.$enc.$id"
    }

    // decodeKeysetCursor parses v2 strictly, legacy SMS leniently. Corrupt
    // v2 fails open to first page here (self-healing: the next valid page
    // resumes with a fresh v2 cursor); the Mac rejects corrupt v2 fail-closed
    // before it ever reaches the phone. Garbage legacy input also yields
    // first page, preserving pre-v2 behavior byte-for-byte.
    private fun decodeKeysetCursor(cursor: String): Pair<String, Long> {
        val safe = cursor.trim()
        if (safe.isEmpty()) return "" to 0L
        if (safe.startsWith("v2.")) {
            return decodeKeysetV2(safe) ?: ("" to 0L)
        }
        val parts = safe.split(":")
        val date = parts.firstOrNull()?.toLongOrNull() ?: 0L
        val id = parts.getOrNull(1)?.toLongOrNull() ?: 0L
        return date.toString() to id
    }

    private fun decodeKeysetV2(cursor: String): Pair<String, Long>? {
        val rest = cursor.removePrefix("v2.")
        val idx = rest.lastIndexOf(".")
        if (idx < 0) return null
        val id = rest.substring(idx + 1).toLongOrNull() ?: return null
        if (id < 0) return null
        val key = try {
            val raw = Base64.decode(rest.substring(0, idx), Base64.URL_SAFE or Base64.NO_WRAP or Base64.NO_PADDING)
            String(raw, Charsets.UTF_8)
        } catch (_: Exception) {
            return null
        }
        // Round-trip verify so a legacy string starting with "v2." fails
        // closed instead of paging wrong.
        if (keysetCursor(key, id) != cursor.trim()) return null
        return key to id
    }

    private data class ContactRef(
        val name: String,
        val contactId: String,
        val photoVersion: String,
    )

    private data class ThreadDetails(
        val address: String,
        val contactName: String,
        val contactId: String,
        val photoVersion: String,
        val date: Long,
        val unreadCount: Int,
        val read: Boolean
    )

    private fun getThreadDetails(threadId: Long): ThreadDetails {
        var address = ""
        var date = 0L
        var unreadCount = 0
        var isRead = true

        val c = context.contentResolver.query(
            Telephony.Sms.CONTENT_URI,
            arrayOf(Telephony.Sms.ADDRESS, Telephony.Sms.DATE, Telephony.Sms.READ),
            "${Telephony.Sms.THREAD_ID} = ?",
            arrayOf(threadId.toString()),
            "${Telephony.Sms.DATE} DESC"
        )

        c?.use {
            val addrCol = it.getColumnIndex(Telephony.Sms.ADDRESS)
            val dateCol = it.getColumnIndex(Telephony.Sms.DATE)
            val readCol = it.getColumnIndex(Telephony.Sms.READ)

            var isFirst = true
            while (it.moveToNext()) {
                if (isFirst) {
                    if (addrCol >= 0) address = it.getString(addrCol) ?: ""
                    if (dateCol >= 0) date = it.getLong(dateCol)
                    isFirst = false
                }
                val readVal = if (readCol >= 0) it.getInt(readCol) == 1 else true
                if (!readVal) {
                    unreadCount++
                    isRead = false
                }
            }
        }

        val ref = resolveContactRef(address)
        return ThreadDetails(address, ref.name, ref.contactId, ref.photoVersion, date, unreadCount, isRead)
    }

    private fun queryMessages(threadId: Long, cursor: String, limit: Int): Map<String, Any> {
        val resolver = context.contentResolver
        val uri = Telephony.Sms.CONTENT_URI
        val projection = arrayOf(
            Telephony.Sms._ID,
            Telephony.Sms.THREAD_ID,
            Telephony.Sms.ADDRESS,
            Telephony.Sms.BODY,
            Telephony.Sms.DATE,
            Telephony.Sms.TYPE,
            Telephony.Sms.READ,
            Telephony.Sms.STATUS
        )

        var selection = "${Telephony.Sms.THREAD_ID} = ?"
        val selectionArgs = mutableListOf(threadId.toString())

        // Cursor is v2 "v2.<b64date>.<rowId>" or legacy "dateMs[:rowId]".
        // The _id tiebreak keeps equal-millisecond bursts (multipart
        // segments) from being skipped or looped across pages.
        val (cursorKey, cursorId) = decodeKeysetCursor(cursor)
        val cursorDate = cursorKey.toLongOrNull() ?: 0L
        if (cursorDate > 0 && cursorId > 0) {
            selection += " AND (${Telephony.Sms.DATE} < ? OR (${Telephony.Sms.DATE} = ? AND ${Telephony.Sms._ID} < ?))"
            selectionArgs.add(cursorDate.toString())
            selectionArgs.add(cursorDate.toString())
            selectionArgs.add(cursorId.toString())
        } else if (cursorDate > 0) {
            selection += " AND ${Telephony.Sms.DATE} < ?"
            selectionArgs.add(cursorDate.toString())
        }

        val sortOrder = "${Telephony.Sms.DATE} DESC, ${Telephony.Sms._ID} DESC LIMIT $limit"

        val messages = mutableListOf<Map<String, Any>>()
        val c = resolver.query(uri, projection, selection, selectionArgs.toTypedArray(), sortOrder)
        c?.use {
            val idCol = it.getColumnIndexOrThrow(Telephony.Sms._ID)
            val tidCol = it.getColumnIndexOrThrow(Telephony.Sms.THREAD_ID)
            val addrCol = it.getColumnIndex(Telephony.Sms.ADDRESS)
            val bodyCol = it.getColumnIndex(Telephony.Sms.BODY)
            val dateCol = it.getColumnIndex(Telephony.Sms.DATE)
            val typeCol = it.getColumnIndex(Telephony.Sms.TYPE)
            val readCol = it.getColumnIndex(Telephony.Sms.READ)
            val statCol = it.getColumnIndex(Telephony.Sms.STATUS)

            // Per-address memo within the page: same sender repeats across
            // messages; resolve once (LRU covers cross-page).
            val pageRefs = HashMap<String, ContactRef>()
            while (it.moveToNext()) {
                val id = it.getLong(idCol)
                val tid = it.getLong(tidCol)
                val addr = if (addrCol >= 0) it.getString(addrCol) ?: "" else ""
                val body = if (bodyCol >= 0) it.getString(bodyCol) ?: "" else ""
                val date = if (dateCol >= 0) it.getLong(dateCol) else 0L
                val type = if (typeCol >= 0) it.getInt(typeCol) else Telephony.Sms.MESSAGE_TYPE_INBOX
                val read = if (readCol >= 0) it.getInt(readCol) == 1 else true
                val status = if (statCol >= 0) it.getInt(statCol) else Telephony.Sms.STATUS_NONE

                val ref = pageRefs.getOrPut(addr) { resolveContactRef(addr) }
                val m = mutableMapOf<String, Any>(
                    "id" to id,
                    "thread_id" to tid,
                    "address" to addr,
                    "body" to body,
                    "date" to date,
                    "type" to type,
                    "read" to read,
                    "status" to status
                )
                // Additive per-message identity: old Mac ignores unknown
                // fields; new Mac renders without a thread-cache round-trip.
                if (ref.name.isNotEmpty()) m["contact_name"] = ref.name
                if (ref.contactId.isNotEmpty()) m["contact_id"] = ref.contactId
                if (ref.photoVersion.isNotEmpty()) m["photo_version"] = ref.photoVersion
                messages.add(m)
            }
        }

        var nextCursor = ""
        if (messages.size == limit) {
            val last = messages.last()
            val lastDate = (last["date"] as? Number)?.toLong() ?: 0L
            val lastId = (last["id"] as? Number)?.toLong() ?: 0L
            nextCursor = keysetCursor(lastDate.toString(), lastId)
        }

        return mapOf(
            "thread_id" to threadId,
            "messages" to messages.reversed(), // Chronological for chat view
            "next_cursor" to nextCursor
        )
    }

    // sendDedup remembers recent client_id -> result so a Mac retry after a
    // timeout (same client_id) resends the cached ack instead of a duplicate
    // SMS (billed twice). Bounded LRU, evicts oldest.
    private val sendDedup = LinkedHashMap<String, Map<String, Any>>(128)

    private fun rememberSend(clientId: String, res: Map<String, Any>) {
        if (clientId.isEmpty()) return
        synchronized(sendDedup) {
            sendDedup[clientId] = res
            while (sendDedup.size > 128) {
                val oldest = sendDedup.keys.firstOrNull() ?: break
                sendDedup.remove(oldest)
            }
        }
    }

    private fun sendSms(recipient: String, body: String, clientId: String, subId: Int = -1): Map<String, Any> {
        val trimmedRecipient = recipient.trim()
        val trimmedBody = body.trim()
        if (trimmedRecipient.isEmpty() || trimmedBody.isEmpty()) {
            throw IllegalArgumentException("recipient and body must not be empty")
        }
        if (clientId.isNotEmpty()) {
            synchronized(sendDedup) {
                sendDedup[clientId]?.let { return it }
            }
        }

        val smsManager: SmsManager = if (subId >= 0 && Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) {
            context.getSystemService(SmsManager::class.java).createForSubscriptionId(subId)
        } else if (subId >= 0) {
            @Suppress("DEPRECATION")
            SmsManager.getSmsManagerForSubscriptionId(subId)
        } else if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) {
            context.getSystemService(SmsManager::class.java)
        } else {
            @Suppress("DEPRECATION")
            SmsManager.getDefault()
        }

        // Unconditional multipart: divideMessage is subId-aware and accounts
        // for GSM-7/UCS-2 UDH costs; manual 160-char splits silently fail.
        val parts = smsManager.divideMessage(trimmedBody)
        if (parts.size > 1) {
            smsManager.sendMultipartTextMessage(trimmedRecipient, null, parts, null, null)
        } else {
            smsManager.sendTextMessage(trimmedRecipient, null, trimmedBody, null, null)
        }

        // Reconcile against the provider: the system auto-writes the sent
        // row for non-default apps. Surfaces real message_id/thread_id so
        // the Mac can reconcile instead of faking Date.now() ids.
        var messageId: Long? = null
        var threadId: Long? = null
        try {
            context.contentResolver.query(
                Telephony.Sms.Sent.CONTENT_URI,
                arrayOf(Telephony.Sms._ID, Telephony.Sms.THREAD_ID, Telephony.Sms.DATE),
                "${Telephony.Sms.ADDRESS} = ? AND ${Telephony.Sms.DATE} >= ?",
                arrayOf(trimmedRecipient, (System.currentTimeMillis() - 60_000).toString()),
                "${Telephony.Sms.DATE} DESC LIMIT 1"
            )?.use { c ->
                if (c.moveToFirst()) {
                    messageId = c.getLong(0)
                    threadId = c.getLong(1)
                }
            }
        } catch (_: Exception) {
        }

        val res = mutableMapOf<String, Any>(
            "ok" to true,
            "client_id" to clientId
        )
        if (messageId != null) res["message_id"] = messageId as Long
        if (threadId != null) res["thread_id"] = threadId as Long
        rememberSend(clientId, res)
        return res
    }

    // resolveContactRef returns name + stable contact_id + photo ETag in
    // ONE PhoneLookup query (Contacts columns ride the join for free).
    // Fail-open "": SMS threads still list when READ_CONTACTS is denied
    // or the sender is a short code / email address (phone-only index).
    // Hardened: LRU cache + multi-variant normalization (raw, normalized,
    // stripped national) so "+1 (555) 123-4567" matches "5551234567".
    private fun resolveContactRef(phoneNumber: String): ContactRef {
        val raw = phoneNumber.trim()
        if (raw.isEmpty()) return ContactRef("", "", "")
        contactRefCacheGet(raw)?.let { return it }
        // Email senders never match the phone index; skip the doomed query.
        // Cache hits only: misses stay uncached so a later permission grant
        // or contact save heals on the next page/push instead of sticking.
        if (raw.contains("@")) {
            val emailRef = resolveEmailContactRef(raw)
            if (emailRef.name.isNotEmpty() || emailRef.contactId.isNotEmpty()) {
                contactRefCachePut(raw, emailRef)
                return emailRef
            }
            return ContactRef("", "", "")
        }
        for (variant in lookupVariants(raw)) {
            val hit = queryPhoneLookup(variant)
            if (hit.name.isNotEmpty() || hit.contactId.isNotEmpty()) {
                contactRefCachePut(raw, hit)
                return hit
            }
        }
        return ContactRef("", "", "")
    }

    // clearContactRefCache evicts stale names after directory edits. Called
    // from the contacts observer path via SmsHandler.instance (best-effort).
    fun clearContactRefCache() {
        synchronized(contactRefCache) { contactRefCache.clear() }
    }

    // lookupVariants yields PhoneLookup candidates in priority order: raw,
    // framework-normalized (PhoneNumberUtils strips separators/keypad letters),
    // manual digit-plus form, then national suffix (strip leading +1/1) for
    // E.164-vs-national mismatches. Deduplicated, max 4. No PII logged.
    private fun lookupVariants(raw: String): List<String> {
        val out = LinkedHashSet<String>()
        out.add(raw)
        try {
            val norm = PhoneNumberUtils.normalizeNumber(raw)
            if (norm.isNotEmpty()) out.add(norm)
        } catch (_: Exception) {}
        val manual = buildString {
            var seenPlus = false
            for (ch in raw) {
                when {
                    ch.isDigit() -> append(ch)
                    ch == '+' && !seenPlus && isEmpty() -> { append(ch); seenPlus = true }
                }
            }
        }
        if (manual.isNotEmpty()) out.add(manual)
        // National fallback: "+15551234567" -> "5551234567".
        val digits = manual.trimStart('+')
        if (digits.length == 11 && digits.startsWith("1")) {
            out.add(digits.substring(1))
        }
        return out.take(4)
    }

    private fun queryPhoneLookup(variant: String): ContactRef {
        if (variant.isEmpty()) return ContactRef("", "", "")
        val uri = Uri.withAppendedPath(
            ContactsContract.PhoneLookup.CONTENT_FILTER_URI,
            Uri.encode(variant)
        )
        return try {
            // Projection restricted to documented PhoneLookup columns only.
            // CONTACT_LAST_UPDATED_TIMESTAMP / PHOTO_FILE_ID are Contacts-table
            // columns and throw IllegalArgumentException here (was every-lookup
            // miss). Photo version is photoId-only; the Mac treats it as an
            // opaque ETag.
            context.contentResolver.query(
                uri,
                arrayOf(
                    ContactsContract.PhoneLookup._ID,
                    ContactsContract.PhoneLookup.DISPLAY_NAME,
                    ContactsContract.PhoneLookup.CONTACT_ID,
                    ContactsContract.PhoneLookup.NORMALIZED_NUMBER,
                    ContactsContract.PhoneLookup.NUMBER,
                    ContactsContract.PhoneLookup.PHOTO_ID,
                ),
                null,
                null,
                null
            )?.use { c ->
                if (c.moveToFirst()) {
                    val nameIdx = c.getColumnIndex(ContactsContract.PhoneLookup.DISPLAY_NAME)
                    val cidIdx = c.getColumnIndex(ContactsContract.PhoneLookup.CONTACT_ID)
                    val photoIdx = c.getColumnIndex(ContactsContract.PhoneLookup.PHOTO_ID)
                    val name = if (nameIdx >= 0) try { c.getString(nameIdx) } catch (_: Exception) { null } ?: "" else ""
                    val cid = if (cidIdx >= 0) try { c.getLong(cidIdx) } catch (_: Exception) { 0L } else 0L
                    val photoId = if (photoIdx >= 0) try { c.getLong(photoIdx) } catch (_: Exception) { 0L } else 0L
                    val ver = if (photoId != 0L) "$photoId" else ""
                    ContactRef(name.take(128), if (cid != 0L) cid.toString() else "", ver.take(128))
                } else ContactRef("", "", "")
            } ?: ContactRef("", "", "")
        } catch (_: SecurityException) {
            ContactRef("", "", "")
        } catch (_: Exception) {
            // Silent per-variant: callers try up to 4 variants per address and
            // queryThreads logs the summary (threads=N resolved=M). Logging
            // here spams ~200 lines per 50-thread page with no PII value.
            ContactRef("", "", "")
        }
    }

    // resolveEmailContactRef covers email-address senders via the Email
    // filter URI (PhoneLookup is phone-only). Best-effort, fail-open.
    private fun resolveEmailContactRef(email: String): ContactRef {
        return try {
            val uri = Uri.withAppendedPath(
                ContactsContract.CommonDataKinds.Email.CONTENT_FILTER_URI,
                Uri.encode(email)
            )
            context.contentResolver.query(
                uri,
                arrayOf(
                    ContactsContract.CommonDataKinds.Email.DISPLAY_NAME_PRIMARY,
                    ContactsContract.CommonDataKinds.Email.CONTACT_ID,
                    ContactsContract.CommonDataKinds.Email.PHOTO_ID,
                ),
                null, null, null
            )?.use { c ->
                if (c.moveToFirst()) {
                    val name = try { c.getString(0) } catch (_: Exception) { null } ?: ""
                    val cid = try { c.getLong(1) } catch (_: Exception) { 0L }
                    ContactRef(name, if (cid != 0L) cid.toString() else "", "")
                } else ContactRef("", "", "")
            } ?: ContactRef("", "", "")
        } catch (_: Exception) {
            ContactRef("", "", "")
        }
    }

    private fun resolveContactName(phoneNumber: String): String {
        return resolveContactRef(phoneNumber).name
    }

    companion object {
        @Volatile
        private var instance: SmsHandler? = null

        @Volatile
        private var eventSink: EventChannel.EventSink? = null

        fun onSmsReceived(address: String, body: String, timestamp: Long) {
            val sink = eventSink ?: return
            val inst = instance
            val ref = inst?.resolveContactRef(address) ?: ContactRef("", "", "")
            // Resolve the real thread_id: thread_id=0 pushes are dropped by
            // the Mac cache guards, losing every fast-path SMS. Fall back to
            // the follow-up ContentObserver changed event when unknown.
            val threadId = inst?.resolveThreadId(address, timestamp) ?: 0L
            // Stable dedup id: provider row when visible, else time-based.
            // client_id + seq let the Mac DedupCache actually dedupe.
            val clientId = "push:${address.hashCode()}:$timestamp:${body.length}"
            val msg = mutableMapOf<String, Any>(
                "id" to System.currentTimeMillis(),
                "thread_id" to threadId,
                "address" to address,
                "body" to body,
                "date" to timestamp,
                "type" to Telephony.Sms.MESSAGE_TYPE_INBOX,
                "read" to false
            )
            // Mirror identity inside message (new additive contract) AND at
            // top level (legacy threads/push shape). Old Mac ignores unknown
            // message fields; new Mac renders without thread lookup.
            if (ref.name.isNotEmpty()) msg["contact_name"] = ref.name
            if (ref.contactId.isNotEmpty()) msg["contact_id"] = ref.contactId
            if (ref.photoVersion.isNotEmpty()) msg["photo_version"] = ref.photoVersion
            val map = mutableMapOf<String, Any>(
                "type" to "push",
                "message" to msg,
                "client_id" to clientId,
                "seq" to timestamp
            )
            if (ref.name.isNotEmpty()) map["contact_name"] = ref.name
            if (ref.contactId.isNotEmpty()) map["contact_id"] = ref.contactId
            if (ref.photoVersion.isNotEmpty()) map["photo_version"] = ref.photoVersion
            Handler(Looper.getMainLooper()).post {
                try {
                    sink.success(map)
                } catch (_: Exception) {
                }
            }
        }
    }

    private fun resolveThreadId(address: String, timestamp: Long): Long {
        if (address.isEmpty()) return 0L
        // Exact match first (indexed, fastest).
        try {
            contentResolverSafe().query(
                Telephony.Sms.Inbox.CONTENT_URI,
                arrayOf(Telephony.Sms.THREAD_ID),
                "${Telephony.Sms.ADDRESS} = ?",
                arrayOf(address),
                "${Telephony.Sms.DATE} DESC LIMIT 1"
            )?.use { c ->
                if (c.moveToFirst()) {
                    val tid = c.getLong(0)
                    if (tid != 0L) return tid
                }
            }
        } catch (_: Exception) {
        }
        // Normalized fallback: provider canonicalizes formatting
        // differences (+1 (555)… vs 555…) that exact match misses.
        return try {
            android.provider.Telephony.Threads.getOrCreateThreadId(context, address)
        } catch (_: Exception) {
            0L
        }
    }

    private fun contentResolverSafe() = context.contentResolver

    private fun markRead(threadId: Long, messageId: Long, address: String): Map<String, Any> {
        val resolver = context.contentResolver
        var updatedCount = 0

        // 1. Attempt to update system SMS database directly
        try {
            val values = android.content.ContentValues().apply {
                put(Telephony.Sms.READ, 1)
                put(Telephony.Sms.SEEN, 1)
            }
            val whereClauses = mutableListOf("${Telephony.Sms.READ} = 0")
            val args = mutableListOf<String>()

            if (threadId > 0) {
                whereClauses.add("${Telephony.Sms.THREAD_ID} = ?")
                args.add(threadId.toString())
            }
            if (messageId > 0) {
                whereClauses.add("${Telephony.Sms._ID} = ?")
                args.add(messageId.toString())
            }

            val selection = whereClauses.joinToString(" AND ")
            updatedCount = resolver.update(Telephony.Sms.CONTENT_URI, values, selection, args.toTypedArray())
        } catch (_: SecurityException) {
            // Android 4.4+ restricts direct writes to Default SMS App; handled via NotifListener.
        } catch (_: Exception) {
        }

        // 2. Clear any active notifications and trigger SEMANTIC_ACTION_MARK_AS_READ
        try {
            NotifListener.markSmsRead(threadId, address)
        } catch (_: Exception) {}

        return mapOf(
            "ok" to true,
            "thread_id" to threadId,
            "message_id" to messageId,
            "updated_count" to updatedCount
        )
    }
}
