package com.fuseitall.fuseitall

import android.content.ClipData
import android.content.ClipboardManager
import android.content.Context
import android.content.Intent
import android.os.Build
import android.os.PowerManager
import android.provider.Settings
import android.util.Base64
import io.flutter.embedding.android.FlutterActivity
import io.flutter.embedding.engine.FlutterEngine
import io.flutter.plugin.common.EventChannel
import io.flutter.plugin.common.MethodChannel

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
}
