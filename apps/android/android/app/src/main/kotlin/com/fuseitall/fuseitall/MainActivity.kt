package com.fuseitall.fuseitall

import android.content.ClipData
import android.content.ClipboardManager
import android.content.ContentUris
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.database.Cursor
import android.graphics.Bitmap
import android.content.ContentResolver
import android.os.Build
import android.os.Bundle
import android.os.CancellationSignal
import android.os.PowerManager
import android.provider.MediaStore
import android.provider.Settings
import android.util.Base64
import android.util.Size
import androidx.core.app.ActivityCompat
import androidx.core.content.ContextCompat
import io.flutter.embedding.android.FlutterActivity
import io.flutter.embedding.engine.FlutterEngine
import io.flutter.plugin.common.EventChannel
import io.flutter.plugin.common.MethodChannel
import java.io.ByteArrayOutputStream

class MainActivity : FlutterActivity() {
    private var clipEvents: EventChannel.EventSink? = null
    private var clipListener: ClipboardManager.OnPrimaryClipChangedListener? = null

    override fun configureFlutterEngine(flutterEngine: FlutterEngine) {
        super.configureFlutterEngine(flutterEngine)
        // Drains the NotifListener queue; each call clears (single consumer:
        // Dart's heartbeat poller). Missing listener access yields [].
        MethodChannel(flutterEngine.dartExecutor.binaryMessenger, "fuseitall/notif")
            .setMethodCallHandler { call, result ->
                when (call.method) {
                    "pollNotifs" -> result.success(NotifListener.drain())
                    "dismissNotif" -> {
                        val key = call.argument<String>("id")
                        if (key.isNullOrEmpty()) {
                            result.error("BAD_ID", "missing notification id", null)
                        } else {
                            NotifListener.cancelKey(key)
                            result.success(null)
                        }
                    }
                    else -> result.notImplemented()
                }
            }
        // Push-based live notification stream for instant mirror delivery.
        EventChannel(flutterEngine.dartExecutor.binaryMessenger, "fuseitall/notifEvents")
            .setStreamHandler(object : EventChannel.StreamHandler {
                override fun onListen(args: Any?, sink: EventChannel.EventSink) {
                    NotifListener.setEventSink(sink)
                }
                override fun onCancel(args: Any?) {
                    NotifListener.setEventSink(null)
                }
            })
        // Permissions + battery status for the Essential Services card.
        // Never throws across the channel: unknown states return false/"unknown".
        MethodChannel(flutterEngine.dartExecutor.binaryMessenger, "fuseitall/permissions")
            .setMethodCallHandler { call, result ->
                when (call.method) {
                    "isNotificationListenerEnabled" -> {
                        val enabled = isListenerEnabled()
                        if (enabled) NotifListener.ensureBound(this)
                        result.success(enabled)
                    }
                    "openNotificationListenerSettings" -> {
                        try {
                            startActivity(Intent(Settings.ACTION_NOTIFICATION_LISTENER_SETTINGS))
                            result.success(true)
                        } catch (e: Exception) {
                            result.error("NO_SETTINGS", e.message, null)
                        }
                    }
                    "isBatteryUnrestricted" -> result.success(isBatteryUnrestricted())
                    "openBatterySettings" -> {
                        try {
                            startActivity(Intent(Settings.ACTION_IGNORE_BATTERY_OPTIMIZATION_SETTINGS))
                            result.success(true)
                        } catch (e: Exception) {
                            result.error("NO_SETTINGS", e.message, null)
                        }
                    }
                    "startLinkService" -> {
                        try {
                            if (isListenerEnabled()) {
                                NotifListener.ensureBound(this)
                            }
                            val svc = Intent(this, LinkService::class.java)
                            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
                                startForegroundService(svc)
                            } else {
                                startService(svc)
                            }
                            result.success(true)
                        } catch (e: Exception) {
                            result.error("SVC_FAILED", e.message, null)
                        }
                    }
                    "stopLinkService" -> {
                        try {
                            stopService(Intent(this, LinkService::class.java))
                            result.success(true)
                        } catch (e: Exception) {
                            result.error("SVC_FAILED", e.message, null)
                        }
                    }
                    "isAllFilesAccessGranted" -> result.success(isAllFilesAccessGranted())
                    "openAllFilesAccessSettings" -> {
                        try {
                            val intent = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.R) {
                                Intent(Settings.ACTION_MANAGE_APP_ALL_FILES_ACCESS_PERMISSION).apply {
                                    data = android.net.Uri.parse("package:$packageName")
                                }
                            } else {
                                Intent(Settings.ACTION_MANAGE_APP_ALL_FILES_ACCESS_PERMISSION)
                            }
                            startActivity(intent)
                            result.success(true)
                        } catch (e: Exception) {
                            try {
                                startActivity(Intent(Settings.ACTION_MANAGE_ALL_FILES_ACCESS_PERMISSION))
                                result.success(true)
                            } catch (e2: Exception) {
                                result.error("NO_SETTINGS", e2.message, null)
                            }
                        }
                    }
                    "getExternalRoot" -> {
                        try {
                            val ext = android.os.Environment.getExternalStorageDirectory()
                            result.success(ext?.absolutePath ?: "")
                        } catch (e: Exception) {
                            result.error("NO_ROOT", e.message, null)
                        }
                    }
                    "getPhotosPermission" -> result.success(getPhotosPermission())
                    "requestPhotosPermission" -> {
                        try {
                            requestPhotosPermission()
                            result.success(true)
                        } catch (e: Exception) {
                            result.error("REQ_FAILED", e.message, null)
                        }
                    }
                    "openPhotosSettings" -> {
                        try {
                            val intent = Intent(Settings.ACTION_APPLICATION_DETAILS_SETTINGS).apply {
                                data = android.net.Uri.parse("package:$packageName")
                            }
                            startActivity(intent)
                            result.success(true)
                        } catch (e: Exception) {
                            result.error("NO_SETTINGS", e.message, null)
                        }
                    }
                    else -> result.notImplemented()
                }
            }
        // Photo library (MediaStore) via MethodChannel fuseitall/photos.
        MethodChannel(flutterEngine.dartExecutor.binaryMessenger, "fuseitall/photos")
            .setMethodCallHandler { call, result ->
                when (call.method) {
                    "queryPhotos" -> {
                        try {
                            val cursor = call.argument<String>("cursor") ?: ""
                            val limit = (call.argument<Int>("limit") ?: 100).coerceIn(1, 200)
                            val res = queryPhotos(cursor, limit)
                            result.success(res)
                        } catch (e: SecurityException) {
                            result.error("PERMISSION_DENIED", e.message, null)
                        } catch (e: Exception) {
                            result.error("QUERY_FAILED", e.message, null)
                        }
                    }
                    "getThumb" -> {
                        try {
                            val id = call.argument<String>("photo_id") ?: ""
                            val size = (call.argument<Int>("thumb_size") ?: 256).coerceIn(64, 1024)
                            val res = getPhotoThumb(id, size)
                            result.success(res)
                        } catch (e: SecurityException) {
                            result.error("PERMISSION_DENIED", e.message, null)
                        } catch (e: Exception) {
                            result.error("THUMB_FAILED", e.message, null)
                        }
                    }
                    "getPhotoSize" -> {
                        try {
                            val id = call.argument<String>("photo_id") ?: ""
                            val sz = getPhotoSize(id)
                            result.success(sz)
                        } catch (e: SecurityException) {
                            result.error("PERMISSION_DENIED", e.message, null)
                        } catch (e: Exception) {
                            result.error("SIZE_FAILED", e.message, null)
                        }
                    }
                    "readPhotoChunk" -> {
                        try {
                            val id = call.argument<String>("photo_id") ?: ""
                            val offset = (call.argument<Int>("offset") ?: 0).toLong()
                            val len = call.argument<Int>("len") ?: 0
                            val b64 = readPhotoChunk(id, offset, len)
                            result.success(b64)
                        } catch (e: SecurityException) {
                            result.error("PERMISSION_DENIED", e.message, null)
                        } catch (e: Exception) {
                            result.error("READ_FAILED", e.message, null)
                        }
                    }
                    "deletePhotos" -> {
                        try {
                            val ids = call.argument<List<String>>("photo_ids") ?: emptyList()
                            val res = deletePhotos(ids)
                            result.success(res)
                        } catch (e: SecurityException) {
                            result.error("PERMISSION_DENIED", e.message, null)
                        } catch (e: Exception) {
                            result.error("DELETE_FAILED", e.message, null)
                        }
                    }
                    else -> result.notImplemented()
                }
            }
        // Event-driven clipboard: native listener pushes change events so
        // Dart syncs instantly without polling. Foreground-service keeps
        // this alive in background on Android 10+.
        // Emits text String or Map {kind,image_b64,mime} for images.
        EventChannel(flutterEngine.dartExecutor.binaryMessenger, "fuseitall/clipboardEvents")
            .setStreamHandler(object : EventChannel.StreamHandler {
                override fun onListen(args: Any?, sink: EventChannel.EventSink) {
                    clipEvents = sink
                    val cm = getSystemService(Context.CLIPBOARD_SERVICE) as ClipboardManager
                    val listener = ClipboardManager.OnPrimaryClipChangedListener {
                        val desc = cm.primaryClipDescription
                        val hasImage = desc?.hasMimeType("image/*") == true
                        if (hasImage) {
                            try {
                                val clip = cm.primaryClip
                                if (clip != null && clip.itemCount > 0) {
                                    val item = clip.getItemAt(0)
                                    val uri = item.uri
                                    if (uri != null) {
                                        contentResolver.openInputStream(uri)?.use { ins ->
                                            val bytes = ins.readBytes()
                                            if (bytes.isNotEmpty() && bytes.size <= 5 * 1024 * 1024) {
                                                // Validate magic before emitting (parity with Go SanitizeClipImage)
                                                val sniffed = when {
                                                    bytes.size >= 8 && bytes[0] == 0x89.toByte() && bytes[1] == 0x50.toByte() && bytes[2] == 0x4E.toByte() && bytes[3] == 0x47.toByte() -> "image/png"
                                                    bytes.size >= 3 && bytes[0] == 0xFF.toByte() && bytes[1] == 0xD8.toByte() && bytes[2] == 0xFF.toByte() -> "image/jpeg"
                                                    bytes.size >= 12 && bytes[0] == 'R'.code.toByte() && bytes[1] == 'I'.code.toByte() && bytes[2] == 'F'.code.toByte() && bytes[3] == 'F'.code.toByte() && bytes[8] == 'W'.code.toByte() && bytes[9] == 'E'.code.toByte() && bytes[10] == 'B'.code.toByte() && bytes[11] == 'P'.code.toByte() -> "image/webp"
                                                    bytes.size >= 6 && bytes[0] == 'G'.code.toByte() && bytes[1] == 'I'.code.toByte() && bytes[2] == 'F'.code.toByte() -> "image/gif"
                                                    bytes.size >= 4 && bytes[0] == 0x49.toByte() && bytes[1] == 0x49.toByte() && bytes[2] == 0x2A.toByte() && bytes[3] == 0x00.toByte() -> "image/tiff"
                                                    bytes.size >= 4 && bytes[0] == 0x4D.toByte() && bytes[1] == 0x4D.toByte() && bytes[2] == 0x00.toByte() && bytes[3] == 0x2A.toByte() -> "image/tiff"
                                                    bytes.size >= 12 && bytes[4] == 'f'.code.toByte() && bytes[5] == 't'.code.toByte() && bytes[6] == 'y'.code.toByte() && bytes[7] == 'p'.code.toByte() -> "image/heic"
                                                    else -> ""
                                                }
                                                if (sniffed.isEmpty()) return@OnPrimaryClipChangedListener
                                                val b64 = Base64.encodeToString(bytes, Base64.NO_WRAP)
                                                val mime = desc.getMimeType(0)?.lowercase() ?: sniffed
                                                // Extract filename from uri if any
                                                var filename = ""
                                                try {
                                                    uri.lastPathSegment?.let { seg ->
                                                        val base = seg.substringAfterLast('/').substringAfterLast('\\')
                                                        if (base.matches(Regex("[A-Za-z0-9._-]{1,255}"))) filename = base
                                                    }
                                                } catch (_: Exception) {}
                                                val map = mutableMapOf<String, Any>("kind" to "image", "mime" to mime, "image_b64" to b64)
                                                if (filename.isNotEmpty()) map["filename"] = filename
                                                sink.success(map)
                                                return@OnPrimaryClipChangedListener
                                            }
                                        }
                                    }
                                }
                            } catch (_: Exception) {}
                        }
                        val text = cm.primaryClip
                            ?.getItemAt(0)
                            ?.coerceToText(this@MainActivity)
                            ?.toString()
                        if (!text.isNullOrEmpty()) sink.success(text)
                    }
                    clipListener = listener
                    cm.addPrimaryClipChangedListener(listener)
                }

                override fun onCancel(args: Any?) {
                    val cm = getSystemService(Context.CLIPBOARD_SERVICE) as ClipboardManager
                    clipListener?.let { cm.removePrimaryClipChangedListener(it) }
                    clipListener = null
                    clipEvents = null
                }
            })
        // Local clipboard read/write for sync.
        MethodChannel(flutterEngine.dartExecutor.binaryMessenger, "fuseitall/clipboard")
            .setMethodCallHandler { call, result ->
                when (call.method) {
                    "readText" -> {
                        try {
                            val cm = getSystemService(Context.CLIPBOARD_SERVICE) as ClipboardManager
                            result.success(
                                cm.primaryClip?.getItemAt(0)
                                    ?.coerceToText(this)?.toString(),
                            )
                        } catch (e: Exception) {
                            result.error("CLIP_FAILED", e.message, null)
                        }
                    }
                    "readImage" -> {
                        try {
                            val cm = getSystemService(Context.CLIPBOARD_SERVICE) as ClipboardManager
                            val desc = cm.primaryClipDescription
                            if (desc?.hasMimeType("image/*") == true) {
                                val clip = cm.primaryClip
                                val uri = clip?.getItemAt(0)?.uri
                                if (uri != null) {
                                    contentResolver.openInputStream(uri)?.use { ins ->
                                        val bytes = ins.readBytes()
                                        if (bytes.isNotEmpty() && bytes.size <= 5 * 1024 * 1024) {
                                            val b64 = Base64.encodeToString(bytes, Base64.NO_WRAP)
                                            val mime = desc.getMimeType(0) ?: "image/png"
                                            var filename = ""
                                            try {
                                                uri.lastPathSegment?.let { seg ->
                                                    val base = seg.substringAfterLast('/').substringAfterLast('\\')
                                                    if (base.matches(Regex("[A-Za-z0-9._-]{1,255}"))) filename = base
                                                }
                                            } catch (_: Exception) {}
                                            val map = mutableMapOf<String, Any>("mime" to mime, "image_b64" to b64)
                                            if (filename.isNotEmpty()) map["filename"] = filename
                                            result.success(map)
                                            return@setMethodCallHandler
                                        }
                                    }
                                }
                            }
                            result.success(null)
                        } catch (e: Exception) {
                            result.error("CLIP_FAILED", e.message, null)
                        }
                    }
                    "writeText" -> {
                        val text = call.argument<String>("text")
                        if (text == null) {
                            result.error("BAD_TEXT", "missing text", null)
                        } else {
                            try {
                                val cm = getSystemService(Context.CLIPBOARD_SERVICE) as ClipboardManager
                                cm.setPrimaryClip(ClipData.newPlainText("FuseItAll", text))
                                result.success(null)
                            } catch (e: Exception) {
                                result.error("CLIP_FAILED", e.message, null)
                            }
                        }
                    }
                    "writeImage" -> {
                        val b64 = call.argument<String>("image_b64")
                        val mimeArg = call.argument<String>("mime") ?: "image/png"
                        val filenameArg = call.argument<String>("filename") ?: ""
                        if (b64 == null) {
                            result.error("BAD_IMAGE", "missing image_b64", null)
                        } else {
                            try {
                                val bytes = Base64.decode(b64, Base64.DEFAULT)
                                if (bytes.isEmpty() || bytes.size > 5 * 1024 * 1024) {
                                    result.error("BAD_IMAGE", "invalid size", null)
                                    return@setMethodCallHandler
                                }
                                // Sniff actual MIME from magic bytes for file extension (bytes-exact, future-proof).
                                val sniffed = when {
                                    bytes.size >= 8 && bytes[0] == 0x89.toByte() && bytes[1] == 0x50.toByte() && bytes[2] == 0x4E.toByte() && bytes[3] == 0x47.toByte() -> "image/png"
                                    bytes.size >= 3 && bytes[0] == 0xFF.toByte() && bytes[1] == 0xD8.toByte() && bytes[2] == 0xFF.toByte() -> "image/jpeg"
                                    bytes.size >= 12 && bytes[0] == 'R'.code.toByte() && bytes[1] == 'I'.code.toByte() && bytes[2] == 'F'.code.toByte() && bytes[3] == 'F'.code.toByte() && bytes[8] == 'W'.code.toByte() && bytes[9] == 'E'.code.toByte() && bytes[10] == 'B'.code.toByte() && bytes[11] == 'P'.code.toByte() -> "image/webp"
                                    bytes.size >= 6 && bytes[0] == 'G'.code.toByte() && bytes[1] == 'I'.code.toByte() && bytes[2] == 'F'.code.toByte() -> "image/gif"
                                    bytes.size >= 4 && bytes[0] == 0x49.toByte() && bytes[1] == 0x49.toByte() && bytes[2] == 0x2A.toByte() && bytes[3] == 0x00.toByte() -> "image/tiff"
                                    bytes.size >= 4 && bytes[0] == 0x4D.toByte() && bytes[1] == 0x4D.toByte() && bytes[2] == 0x00.toByte() && bytes[3] == 0x2A.toByte() -> "image/tiff"
                                    bytes.size >= 12 && bytes[4] == 'f'.code.toByte() && bytes[5] == 't'.code.toByte() && bytes[6] == 'y'.code.toByte() && bytes[7] == 'p'.code.toByte() -> "image/heic"
                                    else -> mimeArg.lowercase()
                                }
                                val mime = sniffed
                                // Sanitize filename or synthesize from mime.
                                val sanitizedFilename = when {
                                    filenameArg.matches(Regex("[A-Za-z0-9._-]{1,255}")) -> filenameArg
                                    filenameArg.trim().isNotEmpty() -> {
                                        val base = filenameArg.trim().substringAfterLast('/').substringAfterLast('\\')
                                        if (base.matches(Regex("[A-Za-z0-9._-]{1,255}"))) base else ""
                                    }
                                    else -> ""
                                }
                                // Clean old clip temp files (avoid cache bloat / file-manager noise)
                                try {
                                    cacheDir.listFiles()?.forEach { f ->
                                        if (f.name.startsWith("clip_") && (f.name.endsWith(".png") || f.name.endsWith(".jpg") || f.name.endsWith(".webp") || f.name.endsWith(".gif") || f.name.endsWith(".tiff") || f.name.endsWith(".heic"))) {
                                            if (System.currentTimeMillis() - f.lastModified() > 60_000) f.delete()
                                        }
                                    }
                                } catch (_: Exception) {}
                                val ext = when (mime) {
                                    "image/jpeg" -> ".jpg"
                                    "image/webp" -> ".webp"
                                    "image/gif" -> ".gif"
                                    "image/heic", "image/heif" -> ".heic"
                                    "image/tiff" -> ".tiff"
                                    else -> ".png"
                                }
                                var tmpName = if (sanitizedFilename.isNotEmpty()) sanitizedFilename else "clip_${System.currentTimeMillis()}$ext"
                                // Ensure correct extension for UTI/extension inference.
                                if (!tmpName.lowercase().endsWith(ext.lowercase())) {
                                    if (tmpName.contains(".")) {
                                        tmpName = tmpName.substringBeforeLast(".") + ext
                                    } else {
                                        tmpName += ext
                                    }
                                }
                                val tmp = java.io.File(cacheDir, tmpName)
                                // Ensure no traversal: tmp must stay in cacheDir
                                if (tmp.canonicalPath != java.io.File(cacheDir, tmp.name).canonicalPath) {
                                    result.error("BAD_IMAGE", "invalid filename", null)
                                    return@setMethodCallHandler
                                }
                                // If sanitized name already exists, overwrite atomically
                                if (tmp.exists()) tmp.delete()
                                tmp.writeBytes(bytes)
                                val uri = androidx.core.content.FileProvider.getUriForFile(this, "$packageName.fileprovider", tmp)
                                val clip = ClipData.newUri(contentResolver, "FuseItAll image", uri)
                                val cm = getSystemService(Context.CLIPBOARD_SERVICE) as ClipboardManager
                                cm.setPrimaryClip(clip)
                                result.success(null)
                            } catch (e: Exception) {
                                result.error("CLIP_FAILED", e.message, null)
                            }
                        }
                    }
                    else -> result.notImplemented()
                }
            }
    }

    private fun isListenerEnabled(): Boolean {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O_MR1) {
            try {
                val nm = getSystemService(android.app.NotificationManager::class.java)
                if (nm != null && nm.isNotificationListenerAccessGranted(android.content.ComponentName(this, NotifListener::class.java))) {
                    return true
                }
            } catch (_: Exception) {}
        }
        val flat = Settings.Secure.getString(
            contentResolver, "enabled_notification_listeners",
        ) ?: return false
        return flat.split(":").any { it.contains(packageName, ignoreCase = true) }
    }

    private fun isBatteryUnrestricted(): Boolean {
        return try {
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.M) {
                val pm = getSystemService(PowerManager::class.java)
                pm.isIgnoringBatteryOptimizations(packageName)
            } else {
                true
            }
        } catch (e: Exception) {
            false
        }
    }

    private fun isAllFilesAccessGranted(): Boolean {
        return try {
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.R) {
                android.os.Environment.isExternalStorageManager()
            } else {
                checkSelfPermission(android.Manifest.permission.READ_EXTERNAL_STORAGE) ==
                    android.content.pm.PackageManager.PERMISSION_GRANTED
            }
        } catch (_: Exception) {
            false
        }
    }

    private fun getPhotosPermission(): String {
        return try {
            if (Build.VERSION.SDK_INT >= 34) {
                val full = ContextCompat.checkSelfPermission(this, android.Manifest.permission.READ_MEDIA_IMAGES) == PackageManager.PERMISSION_GRANTED
                if (full) return "granted"
                val partial = ContextCompat.checkSelfPermission(this, "android.permission.READ_MEDIA_VISUAL_USER_SELECTED") == PackageManager.PERMISSION_GRANTED
                if (partial) return "limited"
                "denied"
            } else if (Build.VERSION.SDK_INT >= 33) {
                val granted = ContextCompat.checkSelfPermission(this, android.Manifest.permission.READ_MEDIA_IMAGES) == PackageManager.PERMISSION_GRANTED
                if (granted) "granted" else "denied"
            } else {
                val granted = ContextCompat.checkSelfPermission(this, android.Manifest.permission.READ_EXTERNAL_STORAGE) == PackageManager.PERMISSION_GRANTED
                if (granted) "granted" else "denied"
            }
        } catch (_: Exception) {
            "denied"
        }
    }

    private fun requestPhotosPermission() {
        val perms = when {
            Build.VERSION.SDK_INT >= 34 -> arrayOf(android.Manifest.permission.READ_MEDIA_IMAGES, android.Manifest.permission.READ_MEDIA_VIDEO, "android.permission.READ_MEDIA_VISUAL_USER_SELECTED")
            Build.VERSION.SDK_INT >= 33 -> arrayOf(android.Manifest.permission.READ_MEDIA_IMAGES, android.Manifest.permission.READ_MEDIA_VIDEO)
            else -> arrayOf(android.Manifest.permission.READ_EXTERNAL_STORAGE)
        }
        ActivityCompat.requestPermissions(this, perms, 1001)
    }

    private fun queryPhotos(cursor: String, limit: Int): Map<String, Any> {
        // Check permission fail-closed: throw SecurityException for Dart to map to permission_denied.
        val perm = getPhotosPermission()
        if (perm == "denied") throw SecurityException("Photos permission denied")
        val collection = MediaStore.Images.Media.EXTERNAL_CONTENT_URI
        val projection = arrayOf(
            MediaStore.Images.Media._ID,
            MediaStore.Images.Media.DATE_TAKEN,
            MediaStore.Images.Media.DATE_MODIFIED,
            MediaStore.Images.Media.WIDTH,
            MediaStore.Images.Media.HEIGHT,
            MediaStore.Images.Media.MIME_TYPE,
            MediaStore.Images.Media.SIZE,
            MediaStore.Images.Media.ORIENTATION,
        )
        var cursorTaken: Long? = null
        var cursorId: Long? = null
        if (cursor.isNotEmpty()) {
            val parts = cursor.split("_")
            if (parts.size == 2) {
                cursorTaken = parts[0].toLongOrNull()
                cursorId = parts[1].toLongOrNull()
            }
        }
        val selection: String?
        val selectionArgs: Array<String>?
        if (cursorTaken != null && cursorId != null) {
            selection = "(${MediaStore.Images.Media.DATE_TAKEN} < ? OR (${MediaStore.Images.Media.DATE_TAKEN} = ? AND ${MediaStore.Images.Media._ID} < ?)) OR (${MediaStore.Images.Media.DATE_TAKEN} IS NULL AND ${MediaStore.Images.Media.DATE_MODIFIED} * 1000 < ?)"
            selectionArgs = arrayOf(cursorTaken.toString(), cursorTaken.toString(), cursorId.toString(), cursorTaken.toString())
        } else {
            selection = null
            selectionArgs = null
        }
        val sortOrderSql = "${MediaStore.Images.Media.DATE_TAKEN} DESC, ${MediaStore.Images.Media._ID} DESC"
        val entries = mutableListOf<Map<String, Any>>()
        var nextCursor = ""
        // Stable, future-proof: use documented Bundle query on API 26+ (O) with
        // QUERY_ARG_* instead of injecting LIMIT into sortOrder (which Xiaomi
        // and strict tokenizers reject as "Invalid token LIMIT").
        // Fallback to legacy 5-arg query without LIMIT + client-side cap.
        val cursorObj: Cursor? = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            val args = Bundle().apply {
                if (selection != null) {
                    putString(ContentResolver.QUERY_ARG_SQL_SELECTION, selection)
                    putStringArray(ContentResolver.QUERY_ARG_SQL_SELECTION_ARGS, selectionArgs)
                }
                putString(ContentResolver.QUERY_ARG_SQL_SORT_ORDER, sortOrderSql)
                putInt(ContentResolver.QUERY_ARG_LIMIT, limit)
            }
            try {
                contentResolver.query(collection, projection, args, null)
            } catch (e: IllegalArgumentException) {
                // OEM rejected Bundle key (rare) -> fallback without LIMIT
                contentResolver.query(collection, projection, selection, selectionArgs, sortOrderSql)
            }
        } else {
            contentResolver.query(collection, projection, selection, selectionArgs, sortOrderSql)
        }
        cursorObj?.use { c ->
            val idCol = c.getColumnIndexOrThrow(MediaStore.Images.Media._ID)
            val takenCol = c.getColumnIndexOrThrow(MediaStore.Images.Media.DATE_TAKEN)
            val modCol = c.getColumnIndexOrThrow(MediaStore.Images.Media.DATE_MODIFIED)
            val wCol = c.getColumnIndexOrThrow(MediaStore.Images.Media.WIDTH)
            val hCol = c.getColumnIndexOrThrow(MediaStore.Images.Media.HEIGHT)
            val mimeCol = c.getColumnIndexOrThrow(MediaStore.Images.Media.MIME_TYPE)
            val sizeCol = c.getColumnIndexOrThrow(MediaStore.Images.Media.SIZE)
            val orientCol = c.getColumnIndex(MediaStore.Images.Media.ORIENTATION)
            var count = 0
            while (c.moveToNext() && count < limit) {
                val id = c.getLong(idCol)
                var taken = c.getLong(takenCol)
                if (taken == 0L) {
                    taken = c.getLong(modCol) * 1000
                    if (taken == 0L) taken = System.currentTimeMillis()
                }
                val w = try { c.getInt(wCol) } catch (_: Exception) { 0 }
                val h = try { c.getInt(hCol) } catch (_: Exception) { 0 }
                val mime = try { c.getString(mimeCol) ?: "" } catch (_: Exception) { "" }
                val sz = try { c.getLong(sizeCol) } catch (_: Exception) { 0L }
                val orient = if (orientCol >= 0) try { c.getInt(orientCol) } catch (_: Exception) { 0 } else 0
                entries.add(mapOf(
                    "photo_id" to id.toString(),
                    "taken_at" to taken,
                    "width" to w,
                    "height" to h,
                    "mime" to mime,
                    "size" to sz,
                    "orientation" to orient,
                ))
                count++
            }
            if (entries.isNotEmpty() && entries.size == limit) {
                val last = entries.last()
                nextCursor = "${last["taken_at"]}_${last["photo_id"]}"
            }
        }
        return mapOf("entries" to entries, "next_cursor" to nextCursor)
    }

    private fun getPhotoThumb(photoId: String, size: Int): Map<String, Any> {
        val idLong = photoId.toLongOrNull() ?: throw IllegalArgumentException("bad photo_id")
        val uri = ContentUris.withAppendedId(MediaStore.Images.Media.EXTERNAL_CONTENT_URI, idLong)
        val bmp: Bitmap = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
            contentResolver.loadThumbnail(uri, Size(size, size), CancellationSignal())
        } else {
            @Suppress("DEPRECATION")
            MediaStore.Images.Thumbnails.getThumbnail(contentResolver, idLong, MediaStore.Images.Thumbnails.MINI_KIND, null)
                ?: throw IllegalArgumentException("thumb not found")
        }
        val out = ByteArrayOutputStream()
        bmp.compress(Bitmap.CompressFormat.JPEG, 85, out)
        val bytes = out.toByteArray()
        val b64 = Base64.encodeToString(bytes, Base64.NO_WRAP)
        return mapOf("mime" to "image/jpeg", "data_b64" to b64)
    }

    private fun getPhotoSize(photoId: String): Int {
        val idLong = photoId.toLongOrNull() ?: throw IllegalArgumentException("bad photo_id")
        val uri = ContentUris.withAppendedId(MediaStore.Images.Media.EXTERNAL_CONTENT_URI, idLong)
        contentResolver.query(uri, arrayOf(MediaStore.Images.Media.SIZE), null, null, null)?.use { c ->
            if (c.moveToFirst()) {
                val idx = c.getColumnIndex(MediaStore.Images.Media.SIZE)
                if (idx >= 0) return c.getInt(idx)
            }
        }
        // Fallback: open and count.
        contentResolver.openInputStream(uri)?.use { it.readBytes().size }?.let { return it }
        return 0
    }

    private fun readPhotoChunk(photoId: String, offset: Long, len: Int): String {
        if (len < 0 || len > 1024 * 1024) throw IllegalArgumentException("bad len")
        val idLong = photoId.toLongOrNull() ?: throw IllegalArgumentException("bad photo_id")
        val uri = ContentUris.withAppendedId(MediaStore.Images.Media.EXTERNAL_CONTENT_URI, idLong)
        contentResolver.openInputStream(uri)?.use { ins ->
            val all = ins.readBytes()
            if (offset < 0 || offset > all.size) throw IllegalArgumentException("bad offset")
            val end = (offset + len).coerceAtMost(all.size.toLong()).toInt()
            val slice = all.sliceArray(offset.toInt() until end)
            return Base64.encodeToString(slice, Base64.NO_WRAP)
        }
        throw IllegalArgumentException("photo not found")
    }

    private fun deletePhotos(ids: List<String>): Map<String, Any> {
        val results = mutableListOf<Map<String, Any>>()
        for (id in ids) {
            val idLong = id.toLongOrNull()
            if (idLong == null) {
                results.add(mapOf("photo_id" to id, "ok" to false, "error" to "invalid id", "error_code" to "invalid_arg"))
                continue
            }
            val uri = ContentUris.withAppendedId(MediaStore.Images.Media.EXTERNAL_CONTENT_URI, idLong)
            try {
                val rows = contentResolver.delete(uri, null, null)
                if (rows > 0) {
                    results.add(mapOf("photo_id" to id, "ok" to true))
                } else {
                    results.add(mapOf("photo_id" to id, "ok" to false, "error" to "not found", "error_code" to "not_found"))
                }
            } catch (e: SecurityException) {
                // Android 10+ may need user consent via RecoverableSecurityException.
                results.add(mapOf("photo_id" to id, "ok" to false, "error" to (e.message ?: "permission denied"), "error_code" to "permission_denied"))
            } catch (e: Exception) {
                results.add(mapOf("photo_id" to id, "ok" to false, "error" to (e.message ?: "delete failed"), "error_code" to "internal"))
            }
        }
        return mapOf("results" to results)
    }
}
