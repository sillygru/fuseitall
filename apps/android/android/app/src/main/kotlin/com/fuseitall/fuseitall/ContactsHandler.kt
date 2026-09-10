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
            override fun onChange(selfChange: Boolean) {
                super.onChange(selfChange)
                val now = System.currentTimeMillis()
                // Debounce notifications by 500ms
                if (now - lastNotify > 500) {
                    lastNotify = now
                    eventSink?.success(mapOf("changed_at" to now))
                }
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

    private fun queryContacts(cursor: String, limit: Int, query: String): Map<String, Any> {
        val resolver = context.contentResolver
        val uri = ContactsContract.Contacts.CONTENT_URI
        val projection = arrayOf(
            ContactsContract.Contacts._ID,
            ContactsContract.Contacts.DISPLAY_NAME_PRIMARY,
            ContactsContract.Contacts.STARRED,
            ContactsContract.Contacts.PHOTO_THUMBNAIL_URI
        )

        var selection = "${ContactsContract.Contacts.IN_VISIBLE_GROUP} = 1"
        val selectionArgs = mutableListOf<String>()

        val trimmedQuery = query.trim()
        if (trimmedQuery.isNotEmpty()) {
            selection += " AND ${ContactsContract.Contacts.DISPLAY_NAME_PRIMARY} LIKE ?"
            selectionArgs.add("%$trimmedQuery%")
        }

        val safeCursor = cursor.trim()
        if (safeCursor.isNotEmpty()) {
            selection += " AND ${ContactsContract.Contacts.DISPLAY_NAME_PRIMARY} > ?"
            selectionArgs.add(safeCursor)
        }

        val sortOrder = "${ContactsContract.Contacts.DISPLAY_NAME_PRIMARY} COLLATE NOCASE ASC LIMIT $limit"

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
            nextCursor = contactsList.last()["display_name"] as? String ?: ""
        }

        return mapOf(
            "entries" to contactsList,
            "next_cursor" to nextCursor,
            "total_count" to contactsList.size
        )
    }

    private fun getContactAvatar(contactId: String): Map<String, Any>? {
        val cid = contactId.toLongOrNull() ?: return null
        val uri = ContentUris.withAppendedId(ContactsContract.Contacts.CONTENT_URI, cid)
        return try {
            ContactsContract.Contacts.openContactPhotoInputStream(context.contentResolver, uri, false)?.use { ins ->
                val bytes = ins.readBytes()
                if (bytes.isNotEmpty() && bytes.size <= 65536) {
                    val b64 = Base64.encodeToString(bytes, Base64.NO_WRAP)
                    mapOf("mime" to "image/jpeg", "data_b64" to b64)
                } else null
            }
        } catch (_: Exception) {
            null
        }
    }
}
