// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

package com.fuseitall.fuseitall

import android.content.ContentResolver
import android.content.ContentUris
import android.content.Context
import android.database.ContentObserver
import android.database.Cursor
import android.net.Uri
import android.os.Handler
import android.os.Looper
import android.provider.ContactsContract
import android.util.Base64
import io.flutter.plugin.common.BinaryMessenger
import io.flutter.plugin.common.EventChannel
import io.flutter.plugin.common.MethodCall
import io.flutter.plugin.common.MethodChannel
import java.io.ByteArrayOutputStream
import java.util.concurrent.Executors

class ContactsHandler(
    private val context: Context,
    messenger: BinaryMessenger
) : MethodChannel.MethodCallHandler, EventChannel.StreamHandler {

    private val executor = Executors.newFixedThreadPool(2)
    private val mainHandler = Handler(Looper.getMainLooper())
    private var eventSink: EventChannel.EventSink? = null
    private var observer: ContentObserver? = null

    init {
        MethodChannel(messenger, "fuseitall/contacts").setMethodCallHandler(this)
        EventChannel(messenger, "fuseitall/contactsEvents").setStreamHandler(this)
    }

    override fun onMethodCall(call: MethodCall, result: MethodChannel.Result) {
        when (call.method) {
            "queryContacts" -> {
                val cursor = call.argument<String>("cursor") ?: ""
                val limit = (call.argument<Int>("limit") ?: 50).coerceIn(1, 100)
                val query = call.argument<String>("query") ?: ""
                executor.execute {
                    try {
                        val res = queryContacts(cursor, limit, query)
                        mainHandler.post { result.success(res) }
                    } catch (e: SecurityException) {
                        mainHandler.post { result.error("PERMISSION_DENIED", e.message, null) }
                    } catch (e: Exception) {
                        mainHandler.post { result.error("QUERY_FAILED", e.message, null) }
                    }
                }
            }
            "getContactAvatar" -> {
                val contactId = call.argument<String>("contact_id") ?: ""
                executor.execute {
                    try {
                        val res = getContactAvatar(contactId)
                        mainHandler.post { result.success(res) }
                    } catch (e: SecurityException) {
                        mainHandler.post { result.error("PERMISSION_DENIED", e.message, null) }
                    } catch (e: Exception) {
                        mainHandler.post { result.error("AVATAR_FAILED", e.message, null) }
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
                val now = System.currentTimeMillis()
                lastNotify = now
                try {
                    eventSink?.success(mapOf("changed_at" to now))
                } catch (_: Exception) {}
            }
            private fun onDirty() {
                val now = System.currentTimeMillis()
                if (now - lastNotify > 500) {
                    mainHandler.removeCallbacks(trailing)
                    trailingPosted = false
                    lastNotify = now
                    try {
                        eventSink?.success(mapOf("changed_at" to now))
                    } catch (_: Exception) {}
                } else if (!trailingPosted) {
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
                ContactsContract.Contacts.CONTENT_URI,
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

    // decodeContactsCursor parses the canonical v2 cursor
    // ("v2.<b64name>.<rowId>", shared with core EncodeKeysetCursor) with
    // fallback to legacy "b64name.rowId" and bare display names. Corrupt v2
    // fails open to first page (self-healing); the Mac rejects corrupt v2
    // fail-closed before it ever reaches the phone. Legacy branches are
    // byte-for-byte the pre-v2 behavior.
    private fun decodeContactsCursor(cursor: String): Pair<String, Long> {
        val safe = cursor.trim()
        if (safe.isEmpty()) return "" to 0L
        if (safe.startsWith("v2.")) {
            return decodeKeysetV2(safe) ?: ("" to 0L)
        }
        val idx = safe.lastIndexOf(".")
        if (idx < 0) return safe to 0L
        val idPart = safe.substring(idx + 1).toLongOrNull()
        if (idPart == null || idPart < 0) return safe to 0L
        return try {
            val raw = Base64.decode(safe.substring(0, idx), Base64.URL_SAFE or Base64.NO_WRAP or Base64.NO_PADDING)
            String(raw, Charsets.UTF_8) to idPart
        } catch (_: Exception) {
            safe to 0L
        }
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
        if (encodeContactsCursor(key, id) != cursor.trim()) return null
        return key to id
    }

    private fun encodeContactsCursor(displayName: String, rowId: Long): String {
        val id = if (rowId < 0) 0L else rowId
        val enc = Base64.encodeToString(displayName.toByteArray(Charsets.UTF_8), Base64.URL_SAFE or Base64.NO_WRAP or Base64.NO_PADDING)
        return "v2.$enc.$id"
    }

    private fun queryContacts(cursor: String, limit: Int, query: String): Map<String, Any> {
        val resolver = context.contentResolver
        val uri = ContactsContract.Contacts.CONTENT_URI
        val projection = arrayOf(
            ContactsContract.Contacts._ID,
            ContactsContract.Contacts.DISPLAY_NAME_PRIMARY,
            ContactsContract.Contacts.STARRED,
            ContactsContract.Contacts.PHOTO_THUMBNAIL_URI,
            ContactsContract.Contacts.LOOKUP_KEY,
            ContactsContract.Contacts.CONTACT_LAST_UPDATED_TIMESTAMP
        )

        var selection = "${ContactsContract.Contacts.IN_VISIBLE_GROUP} = 1"
        val selectionArgs = mutableListOf<String>()

        val trimmedQuery = query.trim()
        if (trimmedQuery.isNotEmpty()) {
            selection += " AND ${ContactsContract.Contacts.DISPLAY_NAME_PRIMARY} LIKE ?"
            selectionArgs.add("%$trimmedQuery%")
        }

        // Stable keyset: (name, _id) tiebreak survives duplicate display
        // names; NOCASE sort with binary > boundary handled by the OR leg.
        val (cursorName, cursorId) = decodeContactsCursor(cursor)
        if (cursorName.isNotEmpty() && cursorId > 0) {
            selection += " AND (${ContactsContract.Contacts.DISPLAY_NAME_PRIMARY} > ? OR (${ContactsContract.Contacts.DISPLAY_NAME_PRIMARY} = ? AND ${ContactsContract.Contacts._ID} > ?))"
            selectionArgs.add(cursorName)
            selectionArgs.add(cursorName)
            selectionArgs.add(cursorId.toString())
        } else if (cursorName.isNotEmpty()) {
            selection += " AND ${ContactsContract.Contacts.DISPLAY_NAME_PRIMARY} > ?"
            selectionArgs.add(cursorName)
        }

        val sortOrder = "${ContactsContract.Contacts.DISPLAY_NAME_PRIMARY} COLLATE NOCASE ASC, ${ContactsContract.Contacts._ID} ASC LIMIT $limit"

        val contactsList = mutableListOf<MutableMap<String, Any>>()
        val contactIds = mutableListOf<Long>()

        val cursorObj: Cursor? = resolver.query(
            uri,
            projection,
            selection,
            if (selectionArgs.isEmpty()) null else selectionArgs.toTypedArray(),
            sortOrder
        )

        cursorObj?.use { c ->
            val idCol = c.getColumnIndexOrThrow(ContactsContract.Contacts._ID)
            val nameCol = c.getColumnIndexOrThrow(ContactsContract.Contacts.DISPLAY_NAME_PRIMARY)
            val starredCol = c.getColumnIndex(ContactsContract.Contacts.STARRED)
            val lookupCol = c.getColumnIndex(ContactsContract.Contacts.LOOKUP_KEY)
            val updatedCol = c.getColumnIndex(ContactsContract.Contacts.CONTACT_LAST_UPDATED_TIMESTAMP)

            while (c.moveToNext()) {
                val id = c.getLong(idCol)
                val name = c.getString(nameCol) ?: ""
                val starred = if (starredCol >= 0) c.getInt(starredCol) == 1 else false

                val entry = mutableMapOf<String, Any>(
                    "contact_id" to id.toString(),
                    "display_name" to name,
                    "starred" to starred,
                    "phones" to mutableListOf<Map<String, Any>>(),
                    "emails" to mutableListOf<Map<String, Any>>()
                )
                if (lookupCol >= 0) {
                    val lookup = c.getString(lookupCol) ?: ""
                    if (lookup.isNotEmpty()) entry["lookup_key"] = lookup
                }
                if (updatedCol >= 0) {
                    try {
                        val updated = c.getLong(updatedCol)
                        if (updated > 0) entry["last_updated_ms"] = updated
                    } catch (_: Exception) {}
                }
                contactsList.add(entry)
                contactIds.add(id)
            }
        }

        if (contactIds.isNotEmpty()) {
            val contactMap = contactsList.associateBy { it["contact_id"] as String }

            // Batch query phone numbers for these contacts
            val inClause = contactIds.joinToString(",")
            val phoneCursor: Cursor? = resolver.query(
                ContactsContract.CommonDataKinds.Phone.CONTENT_URI,
                arrayOf(
                    ContactsContract.CommonDataKinds.Phone.CONTACT_ID,
                    ContactsContract.CommonDataKinds.Phone.NUMBER,
                    ContactsContract.CommonDataKinds.Phone.TYPE,
                    ContactsContract.CommonDataKinds.Phone.LABEL,
                    ContactsContract.CommonDataKinds.Phone.IS_PRIMARY
                ),
                "${ContactsContract.CommonDataKinds.Phone.CONTACT_ID} IN ($inClause)",
                null,
                null
            )

            phoneCursor?.use { pc ->
                val cidCol = pc.getColumnIndexOrThrow(ContactsContract.CommonDataKinds.Phone.CONTACT_ID)
                val numCol = pc.getColumnIndexOrThrow(ContactsContract.CommonDataKinds.Phone.NUMBER)
                val typeCol = pc.getColumnIndex(ContactsContract.CommonDataKinds.Phone.TYPE)
                val labelCol = pc.getColumnIndex(ContactsContract.CommonDataKinds.Phone.LABEL)
                val primCol = pc.getColumnIndex(ContactsContract.CommonDataKinds.Phone.IS_PRIMARY)

                while (pc.moveToNext()) {
                    val cid = pc.getLong(cidCol).toString()
                    val num = pc.getString(numCol) ?: ""
                    val typeInt = if (typeCol >= 0) pc.getInt(typeCol) else ContactsContract.CommonDataKinds.Phone.TYPE_MOBILE
                    val label = if (labelCol >= 0) pc.getString(labelCol) ?: "" else ""
                    val isPrim = if (primCol >= 0) pc.getInt(primCol) == 1 else false

                    val typeStr = when (typeInt) {
                        ContactsContract.CommonDataKinds.Phone.TYPE_HOME -> "home"
                        ContactsContract.CommonDataKinds.Phone.TYPE_WORK -> "work"
                        ContactsContract.CommonDataKinds.Phone.TYPE_OTHER -> "other"
                        else -> "mobile"
                    }

                    val phoneEntry = mutableMapOf<String, Any>(
                        "number" to num,
                        "type" to typeStr,
                        "is_primary" to isPrim
                    )
                    if (label.isNotEmpty()) phoneEntry["label"] = label

                    @Suppress("UNCHECKED_CAST")
                    (contactMap[cid]?.get("phones") as? MutableList<Map<String, Any>>)?.add(phoneEntry)
                }
            }

            // Batch query emails for these contacts
            val emailCursor: Cursor? = resolver.query(
                ContactsContract.CommonDataKinds.Email.CONTENT_URI,
                arrayOf(
                    ContactsContract.CommonDataKinds.Email.CONTACT_ID,
                    ContactsContract.CommonDataKinds.Email.ADDRESS,
                    ContactsContract.CommonDataKinds.Email.TYPE
                ),
                "${ContactsContract.CommonDataKinds.Email.CONTACT_ID} IN ($inClause)",
                null,
                null
            )

            emailCursor?.use { ec ->
                val cidCol = ec.getColumnIndexOrThrow(ContactsContract.CommonDataKinds.Email.CONTACT_ID)
                val addrCol = ec.getColumnIndexOrThrow(ContactsContract.CommonDataKinds.Email.ADDRESS)
                val typeCol = ec.getColumnIndex(ContactsContract.CommonDataKinds.Email.TYPE)

                while (ec.moveToNext()) {
                    val cid = ec.getLong(cidCol).toString()
                    val addr = ec.getString(addrCol) ?: ""
                    val typeInt = if (typeCol >= 0) ec.getInt(typeCol) else ContactsContract.CommonDataKinds.Email.TYPE_OTHER
                    val typeStr = when (typeInt) {
                        ContactsContract.CommonDataKinds.Email.TYPE_HOME -> "home"
                        ContactsContract.CommonDataKinds.Email.TYPE_WORK -> "work"
                        else -> "other"
                    }
                    val emailEntry = mapOf(
                        "address" to addr,
                        "type" to typeStr
                    )
                    @Suppress("UNCHECKED_CAST")
                    (contactMap[cid]?.get("emails") as? MutableList<Map<String, Any>>)?.add(emailEntry)
                }
            }
        }

        var nextCursor = ""
        if (contactsList.size == limit) {
            val last = contactsList.last()
            val lastName = last["display_name"] as? String ?: ""
            val lastId = (last["contact_id"] as? String)?.toLongOrNull() ?: 0L
            nextCursor = encodeContactsCursor(lastName, lastId)
        }

        // total_count is a directory-size hint from a separate COUNT so the
        // Mac can show progress; page size is never a valid total.
        var totalCount = contactsList.size
        try {
            resolver.query(
                uri,
                arrayOf("count(*) AS total"),
                if (trimmedQuery.isNotEmpty())
                    "${ContactsContract.Contacts.IN_VISIBLE_GROUP} = 1 AND ${ContactsContract.Contacts.DISPLAY_NAME_PRIMARY} LIKE ?"
                else
                    "${ContactsContract.Contacts.IN_VISIBLE_GROUP} = 1",
                if (trimmedQuery.isNotEmpty()) arrayOf("%$trimmedQuery%") else null,
                null
            )?.use { cc ->
                if (cc.moveToFirst()) totalCount = cc.getInt(0)
            }
        } catch (_: Exception) {
        }

        return mapOf(
            "entries" to contactsList,
            "next_cursor" to nextCursor,
            "total_count" to totalCount
        )
    }

    private fun getContactAvatar(contactId: String): Map<String, Any>? {
        val cid = contactId.toLongOrNull() ?: return null
        val uri = ContentUris.withAppendedId(ContactsContract.Contacts.CONTENT_URI, cid)
        return try {
            // Photo version ETag for Mac LRU eviction.
            var photoVersion = ""
            try {
                resolverPhotoVersion(cid)?.let { photoVersion = it }
            } catch (_: Exception) {}
            ContactsContract.Contacts.openContactPhotoInputStream(context.contentResolver, uri, true)?.use { ins ->
                val raw = ins.readBytes()
                if (raw.isEmpty()) return null
                val fitted = fitAvatarBytes(raw)
                if (fitted.isEmpty()) return null
                val b64 = Base64.encodeToString(fitted, Base64.NO_WRAP)
                val m = mutableMapOf<String, Any>("mime" to "image/jpeg", "data_b64" to b64)
                if (photoVersion.isNotEmpty()) m["photo_version"] = photoVersion
                m
            }
        } catch (_: Exception) {
            null
        }
    }

    private fun resolverPhotoVersion(cid: Long): String? {
        val uri = ContentUris.withAppendedId(ContactsContract.Contacts.CONTENT_URI, cid)
        context.contentResolver.query(
            uri,
            arrayOf(
                ContactsContract.Contacts.PHOTO_ID,
                ContactsContract.Contacts.PHOTO_FILE_ID,
                ContactsContract.Contacts.CONTACT_LAST_UPDATED_TIMESTAMP
            ),
            null, null, null
        )?.use { c ->
            if (c.moveToFirst()) {
                val photoId = try { c.getLong(0) } catch (_: Exception) { 0L }
                val fileId = try { c.getLong(1) } catch (_: Exception) { 0L }
                val updated = try { c.getLong(2) } catch (_: Exception) { 0L }
                return "$photoId:$fileId:$updated"
            }
        }
        return null
    }

    // fitAvatarBytes downsamples to a <=48KB JPEG so avatars reliably fit
    // the 64KB base64 wire cap instead of degrading to silent null.
    private fun fitAvatarBytes(raw: ByteArray): ByteArray {
        if (raw.size <= 48 * 1024) return raw
        return try {
            val opts = android.graphics.BitmapFactory.Options().apply { inJustDecodeBounds = true }
            android.graphics.BitmapFactory.decodeByteArray(raw, 0, raw.size, opts)
            var sample = 1
            var w = opts.outWidth
            var h = opts.outHeight
            while ((w / 2 >= 96 && h / 2 >= 96) || (raw.size / (sample * sample) > 48 * 1024)) {
                sample *= 2
                w /= 2
                h /= 2
                if (sample >= 8) break
            }
            val dec = android.graphics.BitmapFactory.Options().apply { inSampleSize = sample }
            val bmp = android.graphics.BitmapFactory.decodeByteArray(raw, 0, raw.size, dec) ?: return ByteArray(0)
            val out = ByteArrayOutputStream()
            var quality = 80
            bmp.compress(android.graphics.Bitmap.CompressFormat.JPEG, quality, out)
            while (out.size() > 48 * 1024 && quality > 40) {
                out.reset()
                quality -= 10
                bmp.compress(android.graphics.Bitmap.CompressFormat.JPEG, quality, out)
            }
            if (!bmp.isRecycled) bmp.recycle()
            val fitted = out.toByteArray()
            if (fitted.size <= 48 * 1024) fitted else ByteArray(0)
        } catch (_: Exception) {
            ByteArray(0)
        }
    }
}
