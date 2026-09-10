// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

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
import android.telephony.SmsManager
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
                executor.execute {
                    try {
                        val res = sendSms(recipient, body, clientId)
                        mainHandler.post { result.success(res) }
                    } catch (e: SecurityException) {
                        mainHandler.post { result.error("PERMISSION_DENIED", e.message, null) }
                    } catch (e: Exception) {
                        mainHandler.post { result.error("SEND_FAILED", e.message, null) }
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
            override fun onChange(selfChange: Boolean) {
                super.onChange(selfChange)
                val now = System.currentTimeMillis()
                // Debounce notifications by 500ms
                if (now - lastNotify > 500) {
                    lastNotify = now
                    eventSink?.success(mapOf(
                        "type" to "changed",
                        "changed_at" to now
                    ))
                }
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
        // Query distinct threads from content://sms
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
                    val name = resolveContactName(addr)

                    val threadMap = mutableMapOf<String, Any>(
                        "thread_id" to tid,
                        "address" to addr,
                        "snippet" to body,
                        "date" to date,
                        "message_count" to 1,
                        "unread_count" to (if (read) 0 else 1),
                        "read" to read
                    )
                    if (name.isNotEmpty()) threadMap["contact_name"] = name
                    threads.add(threadMap)
                }
            }
        }

        var nextCursor = ""
        if (threads.size == limit) {
            nextCursor = (threads.last()["date"] as? Number)?.toString() ?: ""
        }

        return mapOf(
            "threads" to threads,
            "next_cursor" to nextCursor
        )
    }

    private data class ThreadDetails(
        val address: String,
        val contactName: String,
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

        val name = resolveContactName(address)
        return ThreadDetails(address, name, date, unreadCount, isRead)
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

        val cursorDate = cursor.toLongOrNull()
        if (cursorDate != null && cursorDate > 0) {
            selection += " AND ${Telephony.Sms.DATE} < ?"
            selectionArgs.add(cursorDate.toString())
        }

        val sortOrder = "${Telephony.Sms.DATE} DESC LIMIT $limit"

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

            while (it.moveToNext()) {
                val id = it.getLong(idCol)
                val tid = it.getLong(tidCol)
                val addr = if (addrCol >= 0) it.getString(addrCol) ?: "" else ""
                val body = if (bodyCol >= 0) it.getString(bodyCol) ?: "" else ""
                val date = if (dateCol >= 0) it.getLong(dateCol) else 0L
                val type = if (typeCol >= 0) it.getInt(typeCol) else Telephony.Sms.MESSAGE_TYPE_INBOX
                val read = if (readCol >= 0) it.getInt(readCol) == 1 else true
                val status = if (statCol >= 0) it.getInt(statCol) else Telephony.Sms.STATUS_NONE

                messages.add(mapOf(
                    "id" to id,
                    "thread_id" to tid,
                    "address" to addr,
                    "body" to body,
                    "date" to date,
                    "type" to type,
                    "read" to read,
                    "status" to status
                ))
            }
        }

        var nextCursor = ""
        if (messages.size == limit) {
            nextCursor = (messages.last()["date"] as? Number)?.toString() ?: ""
        }

        return mapOf(
            "thread_id" to threadId,
            "messages" to messages.reversed(), // Chronological for chat view
            "next_cursor" to nextCursor
        )
    }

    private fun sendSms(recipient: String, body: String, clientId: String): Map<String, Any> {
        val trimmedRecipient = recipient.trim()
        val trimmedBody = body.trim()
        if (trimmedRecipient.isEmpty() || trimmedBody.isEmpty()) {
            throw IllegalArgumentException("recipient and body must not be empty")
        }

        val smsManager: SmsManager = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) {
            context.getSystemService(SmsManager::class.java)
        } else {
            @Suppress("DEPRECATION")
            SmsManager.getDefault()
        }

        val parts = smsManager.divideMessage(trimmedBody)
        if (parts.size > 1) {
            smsManager.sendMultipartTextMessage(trimmedRecipient, null, parts, null, null)
        } else {
            smsManager.sendTextMessage(trimmedRecipient, null, trimmedBody, null, null)
        }

        return mapOf(
            "ok" to true,
            "client_id" to clientId
        )
    }

    private fun resolveContactName(phoneNumber: String): String {
        if (phoneNumber.isEmpty()) return ""
        val uri = Uri.withAppendedPath(
            ContactsContract.PhoneLookup.CONTENT_FILTER_URI,
            Uri.encode(phoneNumber)
        )
        return try {
            context.contentResolver.query(
                uri,
                arrayOf(ContactsContract.PhoneLookup.DISPLAY_NAME),
                null,
                null,
                null
            )?.use { c ->
                if (c.moveToFirst()) {
                    c.getString(0) ?: ""
                } else ""
            } ?: ""
        } catch (_: Exception) {
            ""
        }
    }

    companion object {
        @Volatile
        private var instance: SmsHandler? = null

        @Volatile
        private var eventSink: EventChannel.EventSink? = null

        fun onSmsReceived(address: String, body: String, timestamp: Long) {
            val sink = eventSink ?: return
            val name = instance?.resolveContactName(address) ?: ""
            val msg = mapOf(
                "id" to System.currentTimeMillis(),
                "thread_id" to 0L,
                "address" to address,
                "body" to body,
                "date" to timestamp,
                "type" to Telephony.Sms.MESSAGE_TYPE_INBOX,
                "read" to false
            )
            val map = mutableMapOf<String, Any>(
                "type" to "push",
                "message" to msg
            )
            if (name.isNotEmpty()) map["contact_name"] = name
            Handler(Looper.getMainLooper()).post {
                sink.success(map)
            }
        }
    }
}
