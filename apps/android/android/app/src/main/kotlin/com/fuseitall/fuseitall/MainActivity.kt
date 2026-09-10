package com.fuseitall.fuseitall

import android.content.ClipData
import android.content.ClipboardManager
import android.content.ContentUris
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.database.Cursor
import android.graphics.Bitmap
import android.graphics.Canvas
import android.graphics.drawable.BitmapDrawable
import android.graphics.drawable.Drawable
import android.content.ContentResolver
import android.os.Build
import android.os.Bundle
import android.os.CancellationSignal
import android.os.PowerManager
import android.provider.MediaStore
import android.provider.Settings
import android.util.Base64
import android.util.LruCache
import android.util.Size
import androidx.core.app.ActivityCompat
import androidx.core.content.ContextCompat
import io.flutter.embedding.android.FlutterActivity
import io.flutter.embedding.engine.FlutterEngine
import io.flutter.plugin.common.BinaryMessenger
import io.flutter.plugin.common.EventChannel
import io.flutter.plugin.common.MethodChannel
import java.io.ByteArrayOutputStream
import java.util.concurrent.Executors
import android.media.ExifInterface
import android.media.MediaMetadataRetriever
import android.net.Uri
import android.net.ConnectivityManager
import android.net.Network
import android.net.NetworkCapabilities

class MainActivity : FlutterActivity() {
    private var clipEvents: EventChannel.EventSink? = null
    private var networkEvents: EventChannel.EventSink? = null
    private var networkCallback: ConnectivityManager.NetworkCallback? = null
    private var clipListener: ClipboardManager.OnPrimaryClipChangedListener? = null
    private val photoExecutor = Executors.newFixedThreadPool(4)
    // In-memory RAM LRU cache (250 items, ~6 MB max RAM, 0 disk/SSD wear).
    // Fast path: eliminates repeated Skia JPEG encodes on re-scrolling.
    private val photoThumbCache = LruCache<String, Map<String, Any>>(250)

    override fun configureFlutterEngine(flutterEngine: FlutterEngine) {
        super.configureFlutterEngine(flutterEngine)
        clipMessenger = flutterEngine.dartExecutor.binaryMessenger
        registerNetworkMonitor(flutterEngine)
        ContactsHandler(applicationContext, flutterEngine.dartExecutor.binaryMessenger)
        SmsHandler(applicationContext, flutterEngine.dartExecutor.binaryMessenger)
        if (intent?.getBooleanExtra(EXTRA_CLIPSEND, false) == true) {
            pendingClipSend = true
            try {
                intent.removeExtra(EXTRA_CLIPSEND)
            } catch (_: Exception) {
            }
        }
        // One-tap / auto clipboard trigger bridge (ClipSendActivity focus).
        // Dart registers its onClipFocus handler; native only invokes it and
        // receives the done ack + cold-path drain here. Never throws.
        MethodChannel(flutterEngine.dartExecutor.binaryMessenger, ClipSendActivity.CHANNEL)
            .setMethodCallHandler { call, result ->
                when (call.method) {
                    "popRequested" -> {
                        val v = pendingClipSend
                        pendingClipSend = false
                        result.success(v)
                    }
                    "clipSendDone" -> {
                        try {
                            ClipSendActivity.notifyDone()
                        } catch (_: Exception) {
                        }
                        result.success(null)
                    }
                    else -> result.notImplemented()
                }
            }
        // Drains the NotifListener queue; each call clears (single consumer:
        // Dart's event-driven flush on connect/resume/native push).
        // Missing listener access yields [].
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
                    "updateNotifFilter" -> {
                        try {
                            val mode = call.argument<String>("mode")
                            @Suppress("UNCHECKED_CAST")
                            val muted = (call.argument<List<String>>("muted_packages")
                                ?: call.argument<List<*>>("muted")?.mapNotNull { it as? String })
                            @Suppress("UNCHECKED_CAST")
                            val allowed = (call.argument<List<String>>("allowed_packages")
                                ?: call.argument<List<*>>("allowed")?.mapNotNull { it as? String })
                            NotifListener.updateFilter(mode, muted, allowed)
                            result.success(null)
                        } catch (e: Exception) {
                            result.error("FILTER_FAILED", e.message, null)
                        }
                    }
                    "listNotifApps" -> {
                        try {
                            result.success(listLaunchableApps())
                        } catch (e: Exception) {
                            result.error("LIST_FAILED", e.message, null)
                        }
                    }
                    "listNotifAppsPaged" -> {
                        val cursor = call.argument<String>("cursor") ?: ""
                        val limit = (call.argument<Int>("limit") ?: 50).coerceIn(1, 50)
                        // Number (not Boolean-typed Int): Dart bools arrive fine,
                        // but be lenient when the key is absent (default true).
                        val withIcons = call.argument<Boolean>("with_icons") ?: true
                        photoExecutor.execute {
                            try {
                                val res = listNotifAppsPaged(cursor, limit, withIcons)
                                runOnUiThread { result.success(res) }
                            } catch (e: Exception) {
                                runOnUiThread { result.error("LIST_FAILED", e.message, null) }
                            }
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
                    // Stage-2 clipboard auto trigger (opt-in, adb READ_LOGS +
                    // overlay). Status queries never throw: unknown degrades
                    // to false so the UI shows setup steps.
                    "isReadLogsGranted" -> result.success(ClipAutoWatcher.isReadLogsGranted(this))
                    "isOverlayAllowed" -> result.success(ClipAutoWatcher.isOverlayAllowed(this))
                    "openOverlaySettings" -> {
                        try {
                            ClipAutoWatcher.openOverlaySettings(this)
                            result.success(true)
                        } catch (e: Exception) {
                            result.error("NO_SETTINGS", e.message, null)
                        }
                    }
                    "updateClipAuto" -> {
                        try {
                            val enabled = call.argument<Boolean>("enabled") ?: false
                            if (enabled &&
                                (!ClipAutoWatcher.isReadLogsGranted(this) || !ClipAutoWatcher.isOverlayAllowed(this))
                            ) {
                                result.error("MISSING_PERMS", "grant READ_LOGS via adb and allow overlay", null)
                            } else {
                                ClipAutoWatcher.setEnabled(this, enabled)
                                result.success(true)
                            }
                        } catch (e: Exception) {
                            result.error("AUTO_FAILED", e.message, null)
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
                    "isContactsGranted" -> result.success(isContactsGranted())
                    "requestContactsPermission" -> {
                        try {
                            ActivityCompat.requestPermissions(this, arrayOf(android.Manifest.permission.READ_CONTACTS), 1002)
                            result.success(true)
                        } catch (e: Exception) {
                            result.error("REQ_FAILED", e.message, null)
                        }
                    }
                    "isSmsGranted" -> result.success(isSmsGranted())
                    "requestSmsPermission" -> {
                        try {
                            ActivityCompat.requestPermissions(
                                this,
                                arrayOf(
                                    android.Manifest.permission.READ_SMS,
                                    android.Manifest.permission.RECEIVE_SMS,
                                    android.Manifest.permission.SEND_SMS
                                ),
                                1003
                            )
                            result.success(true)
                        } catch (e: Exception) {
                            result.error("REQ_FAILED", e.message, null)
                        }
                    }
                    else -> result.notImplemented()
                }
            }
        // Playback sync (MediaSession) via MethodChannel fuseitall/playback.
        // Reuses the notification-listener grant; no new permission.
        // Never throws: missing access yields null/false.
        // Realtime pushes ride EventChannel fuseitall/playbackEvents (no polling).
        EventChannel(flutterEngine.dartExecutor.binaryMessenger, "fuseitall/playbackEvents")
            .setStreamHandler(object : EventChannel.StreamHandler {
                override fun onListen(args: Any?, sink: EventChannel.EventSink) {
                    PlaybackMedia.setEventSink(sink)
                    try {
                        PlaybackMedia.startPush(applicationContext)
                    } catch (_: Exception) {}
                }
                override fun onCancel(args: Any?) {
                    try {
                        PlaybackMedia.stopPush()
                    } catch (_: Exception) {}
                    PlaybackMedia.setEventSink(null)
                }
            })
        MethodChannel(flutterEngine.dartExecutor.binaryMessenger, "fuseitall/playback")
            .setMethodCallHandler { call, result ->
                when (call.method) {
                    // Re-anchor: re-register MediaSession callbacks + one
                    // anchor push. Push-only design has no pull API.
                    "watch" -> {
                        try {
                            PlaybackMedia.watch(applicationContext)
                            result.success(true)
                        } catch (e: Exception) {
                            result.error("WATCH_FAILED", e.message, null)
                        }
                    }
                    "command" -> {
                        val cmd = call.argument<String>("cmd") ?: ""
                        val pkg = call.argument<String>("package_name") ?: ""
                        try {
                            result.success(PlaybackMedia.command(this, cmd, pkg))
                        } catch (e: Exception) {
                            result.error("CMD_FAILED", e.message, null)
                        }
                    }
                    "hasAccess" -> {
                        try {
                            result.success(PlaybackMedia.hasAccess(this))
                        } catch (_: Exception) {
                            result.success(false)
                        }
                    }
                    "openSettings" -> {
                        try {
                            startActivity(Intent(Settings.ACTION_NOTIFICATION_LISTENER_SETTINGS))
                            result.success(true)
                        } catch (e: Exception) {
                            result.error("NO_SETTINGS", e.message, null)
                        }
                    }
                    else -> result.notImplemented()
                }
            }
        // Photo library (MediaStore) via MethodChannel fuseitall/photos.
        // Offloads heavy disk I/O and thumbnail extraction to background pool
        // so the Android UI main thread and Flutter platform channel message loop
        // are never starved.
        MethodChannel(flutterEngine.dartExecutor.binaryMessenger, "fuseitall/photos")
            .setMethodCallHandler { call, result ->
                when (call.method) {
                    "queryPhotos" -> {
                        val cursor = call.argument<String>("cursor") ?: ""
                        val limit = (call.argument<Int>("limit") ?: 100).coerceIn(1, 200)
                        photoExecutor.execute {
                            try {
                                val res = queryPhotos(cursor, limit)
                                runOnUiThread { result.success(res) }
                            } catch (e: SecurityException) {
                                runOnUiThread { result.error("PERMISSION_DENIED", e.message, null) }
                            } catch (e: Exception) {
                                runOnUiThread { result.error("QUERY_FAILED", e.message, null) }
                            }
                        }
                    }
                    "getThumb" -> {
                        val id = call.argument<String>("photo_id") ?: ""
                        val size = (call.argument<Int>("thumb_size") ?: 256).coerceIn(64, 1024)
                        photoExecutor.execute {
                            try {
                                val res = getPhotoThumb(id, size)
                                runOnUiThread { result.success(res) }
                            } catch (e: SecurityException) {
                                runOnUiThread { result.error("PERMISSION_DENIED", e.message, null) }
                            } catch (e: Exception) {
                                runOnUiThread { result.error("THUMB_FAILED", e.message, null) }
                            }
                        }
                    }
                    "getPhotoSize" -> {
                        val id = call.argument<String>("photo_id") ?: ""
                        photoExecutor.execute {
                            try {
                                val sz = getPhotoSize(id)
                                runOnUiThread { result.success(sz) }
                            } catch (e: SecurityException) {
                                runOnUiThread { result.error("PERMISSION_DENIED", e.message, null) }
                            } catch (e: Exception) {
                                runOnUiThread { result.error("SIZE_FAILED", e.message, null) }
                            }
                        }
                    }
                    "readPhotoChunk" -> {
                        val id = call.argument<String>("photo_id") ?: ""
                        // Number (not Int): Dart ints over 2 GiB arrive as Long.
                        val offset = (call.argument<Number>("offset")?.toLong() ?: 0L)
                        val len = (call.argument<Number>("len")?.toInt() ?: 0)
                        photoExecutor.execute {
                            try {
                                val b64 = readPhotoChunk(id, offset, len)
                                runOnUiThread { result.success(b64) }
                            } catch (e: SecurityException) {
                                runOnUiThread { result.error("PERMISSION_DENIED", e.message, null) }
                            } catch (e: Exception) {
                                runOnUiThread { result.error("READ_FAILED", e.message, null) }
                            }
                        }
                    }
                    "deletePhotos" -> {
                        val ids = call.argument<List<String>>("photo_ids") ?: emptyList()
                        photoExecutor.execute {
                            try {
                                val res = deletePhotos(ids)
                                runOnUiThread { result.success(res) }
                            } catch (e: SecurityException) {
                                runOnUiThread { result.error("PERMISSION_DENIED", e.message, null) }
                            } catch (e: Exception) {
                                runOnUiThread { result.error("DELETE_FAILED", e.message, null) }
                            }
                        }
                    }
                    else -> result.notImplemented()
                }
            }
        // Event-driven clipboard: native listener pushes change events so
        // Dart syncs instantly without polling. Foreground-service keeps
        // this alive in background on Android 10+.
        // Emits text String or Map {kind,image_b64,mime[,filename][,sensitive]}
        // for images. Sensitive flag mirrors ClipDescription extras
        // (EXTRA_IS_SENSITIVE): auto watchers skip unless opted in, manual
        // Send always bypasses.
        EventChannel(flutterEngine.dartExecutor.binaryMessenger, "fuseitall/clipboardEvents")
            .setStreamHandler(object : EventChannel.StreamHandler {
                override fun onListen(args: Any?, sink: EventChannel.EventSink) {
                    clipEvents = sink
                    val cm = getSystemService(Context.CLIPBOARD_SERVICE) as ClipboardManager
                    val listener = ClipboardManager.OnPrimaryClipChangedListener {
                        val desc = cm.primaryClipDescription
                        val sensitive = try {
                            desc?.extras?.getBoolean("android.content.extra.IS_SENSITIVE") == true
                        } catch (_: Exception) { false }
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
                                                if (sensitive) map["sensitive"] = true
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
        // Event-driven battery level (ACTION_BATTERY_CHANGED, no polling) so
        // the Mac sidebar updates in real time as the phone charges or
        // discharges. Registered on the application context so events keep
        // flowing while backgrounded under the foreground link service.
        EventChannel(flutterEngine.dartExecutor.binaryMessenger, "fuseitall/battery")
            .setStreamHandler(BatteryStreamHandler(applicationContext))
        // Push network-interface changes to Dart so a stale Wi-Fi/hotspot
        // socket is discarded immediately and a fresh discovery/connect
        // attempt starts on the new LAN. No polling or heartbeat is used.
        EventChannel(flutterEngine.dartExecutor.binaryMessenger, "fuseitall/network")
            .setStreamHandler(object : EventChannel.StreamHandler {
                override fun onListen(args: Any?, sink: EventChannel.EventSink?) {
                    networkEvents = sink
                    sink?.success(isNetworkAvailable())
                }
                override fun onCancel(args: Any?) {
                    networkEvents = null
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
                    "isClipboardSensitive" -> {
                        try {
                            val cm = getSystemService(Context.CLIPBOARD_SERVICE) as ClipboardManager
                            val desc = cm.primaryClipDescription
                            val sensitive = try {
                                desc?.extras?.getBoolean("android.content.extra.IS_SENSITIVE") == true
                            } catch (_: Exception) { false }
                            result.success(sensitive)
                        } catch (e: Exception) {
                            result.error("CLIP_FAILED", e.message, null)
                        }
                    }
                    "writeText" -> {
                        val text = call.argument<String>("text")
                        val sensitive = call.argument<Boolean>("sensitive") ?: false
                        if (text == null) {
                            result.error("BAD_TEXT", "missing text", null)
                        } else {
                            try {
                                val cm = getSystemService(Context.CLIPBOARD_SERVICE) as ClipboardManager
                                val clip = ClipData.newPlainText("FuseItAll", text)
                                if (sensitive) {
                                    try {
                                        clip.description.extras = android.os.PersistableBundle().apply {
                                            putBoolean("android.content.extra.IS_SENSITIVE", true)
                                        }
                                    } catch (_: Exception) {}
                                }
                                cm.setPrimaryClip(clip)
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
                    "readLargeImageMeta" -> {
                        try {
                            val cm = getSystemService(Context.CLIPBOARD_SERVICE) as ClipboardManager
                            val desc = cm.primaryClipDescription
                            if (desc?.hasMimeType("image/*") != true) {
                                result.success(null)
                                return@setMethodCallHandler
                            }
                            val clip = cm.primaryClip
                            val uri = clip?.getItemAt(0)?.uri
                            if (uri == null) {
                                result.success(null)
                                return@setMethodCallHandler
                            }
                            val mime = desc.getMimeType(0)?.lowercase() ?: "image/png"
                            var filename = ""
                            try {
                                uri.lastPathSegment?.let { seg ->
                                    val base = seg.substringAfterLast('/').substringAfterLast('\\')
                                    if (base.matches(Regex("[A-Za-z0-9._-]{1,255}"))) filename = base
                                }
                            } catch (_: Exception) {}
                            val sensitive = try {
                                desc.extras?.getBoolean("android.content.extra.IS_SENSITIVE") == true
                            } catch (_: Exception) { false }
                            // Stream size without loading all bytes: open + count.
                            var total: Long = -1
                            try {
                                contentResolver.openAssetFileDescriptor(uri, "r")?.use { afd ->
                                    total = afd.length
                                }
                            } catch (_: Exception) {}
                            if (total < 0) {
                                // Fallback: stream-count (bounded 25 MiB).
                                try {
                                    contentResolver.openInputStream(uri)?.use { ins ->
                                        var count = 0L
                                        val buf = ByteArray(64 * 1024)
                                        while (true) {
                                            val n = ins.read(buf)
                                            if (n <= 0) break
                                            count += n
                                            if (count > 25 * 1024 * 1024) break
                                        }
                                        total = count
                                    }
                                } catch (_: Exception) {}
                            }
                            if (total <= 0) {
                                result.success(null)
                                return@setMethodCallHandler
                            }
                            val map = mutableMapOf<String, Any>(
                                "mime" to mime, "total" to total,
                            )
                            if (filename.isNotEmpty()) map["filename"] = filename
                            if (sensitive) map["sensitive"] = true
                            result.success(map)
                        } catch (e: Exception) {
                            result.error("CLIP_FAILED", e.message, null)
                        }
                    }
                    "readLargeImageChunk" -> {
                        val offset = (call.argument<Number>("offset")?.toLong() ?: 0L)
                        val len = (call.argument<Number>("len")?.toInt() ?: 0)
                        if (len <= 0 || len > 1024 * 1024) {
                            result.error("BAD_ARG", "len must be 1..1MiB", null)
                            return@setMethodCallHandler
                        }
                        try {
                            val cm = getSystemService(Context.CLIPBOARD_SERVICE) as ClipboardManager
                            val uri = cm.primaryClip?.getItemAt(0)?.uri
                            if (uri == null) {
                                result.error("NO_IMAGE", "no image uri", null)
                                return@setMethodCallHandler
                            }
                            contentResolver.openInputStream(uri)?.use { ins ->
                                var skipped = 0L
                                while (skipped < offset) {
                                    val n = ins.skip(offset - skipped)
                                    if (n <= 0) break
                                    skipped += n
                                }
                                val buf = ByteArray(len)
                                var read = 0
                                while (read < len) {
                                    val n = ins.read(buf, read, len - read)
                                    if (n <= 0) break
                                    read += n
                                }
                                if (read <= 0) {
                                    result.error("NO_DATA", "empty chunk", null)
                                    return@setMethodCallHandler
                                }
                                val slice = if (read == len) buf else buf.copyOf(read)
                                result.success(Base64.encodeToString(slice, Base64.NO_WRAP))
                                return@setMethodCallHandler
                            }
                            result.error("NO_IMAGE", "open failed", null)
                        } catch (e: Exception) {
                            result.error("CLIP_FAILED", e.message, null)
                        }
                    }
                    "beginLargeImageWrite" -> {
                        try {
                            largeWriteMime = call.argument<String>("mime") ?: "image/png"
                            largeWriteFilename = call.argument<String>("filename") ?: ""
                            largeWriteTotal = (call.argument<Number>("total")?.toLong() ?: 0L)
                            largeWriteSensitive = call.argument<Boolean>("sensitive") ?: false
                            largeWriteFile?.delete()
                            largeWriteFile = java.io.File.createTempFile("clip_large_", ".bin", cacheDir)
                            largeWriteReceived = 0L
                            result.success(true)
                        } catch (e: Exception) {
                            result.error("CLIP_FAILED", e.message, null)
                        }
                    }
                    "appendLargeImageChunk" -> {
                        val b64 = call.argument<String>("data_b64")
                        if (b64 == null) {
                            result.error("BAD_IMAGE", "missing data_b64", null)
                            return@setMethodCallHandler
                        }
                        try {
                            val bytes = Base64.decode(b64, Base64.DEFAULT)
                            val f = largeWriteFile
                            if (f == null) {
                                result.error("NO_SESSION", "begin first", null)
                                return@setMethodCallHandler
                            }
                            f.appendBytes(bytes)
                            largeWriteReceived += bytes.size
                            if (largeWriteReceived > 25 * 1024 * 1024) {
                                f.delete()
                                largeWriteFile = null
                                result.error("BAD_IMAGE", "too large", null)
                                return@setMethodCallHandler
                            }
                            result.success(true)
                        } catch (e: Exception) {
                            result.error("CLIP_FAILED", e.message, null)
                        }
                    }
                    "finishLargeImageWrite" -> {
                        try {
                            val f = largeWriteFile
                            if (f == null || !f.exists()) {
                                result.error("NO_SESSION", "begin first", null)
                                return@setMethodCallHandler
                            }
                            val bytes = f.readBytes()
                            f.delete()
                            largeWriteFile = null
                            if (bytes.isEmpty() || bytes.size > 25 * 1024 * 1024) {
                                result.error("BAD_IMAGE", "invalid size", null)
                                return@setMethodCallHandler
                            }
                            writeClipBytes(bytes, largeWriteMime, largeWriteFilename, largeWriteSensitive)
                            result.success(null)
                        } catch (e: Exception) {
                            result.error("CLIP_FAILED", e.message, null)
                        }
                    }
                    else -> result.notImplemented()
                }
            }
    }

    // Staged large-image write (chunked MethodChannel to stay under Binder
    // ~1 MiB per call). Single active session; finish assembles + publishes
    // via FileProvider exactly like writeImage.
    private var largeWriteFile: java.io.File? = null
    private var largeWriteMime: String = "image/png"
    private var largeWriteFilename: String = ""
    private var largeWriteTotal: Long = 0L
    private var largeWriteSensitive: Boolean = false
    private var largeWriteReceived: Long = 0L

    private fun writeClipBytes(bytes: ByteArray, mimeArg: String, filenameArg: String, sensitive: Boolean) {
        val mime = when {
            bytes.size >= 8 && bytes[0] == 0x89.toByte() && bytes[1] == 0x50.toByte() && bytes[2] == 0x4E.toByte() && bytes[3] == 0x47.toByte() -> "image/png"
            bytes.size >= 3 && bytes[0] == 0xFF.toByte() && bytes[1] == 0xD8.toByte() && bytes[2] == 0xFF.toByte() -> "image/jpeg"
            else -> mimeArg.lowercase()
        }
        val sanitized = when {
            filenameArg.matches(Regex("[A-Za-z0-9._-]{1,255}")) -> filenameArg
            else -> ""
        }
        val ext = when (mime) {
            "image/jpeg" -> ".jpg"
            "image/webp" -> ".webp"
            "image/gif" -> ".gif"
            "image/heic", "image/heif" -> ".heic"
            "image/tiff" -> ".tiff"
            else -> ".png"
        }
        var name = if (sanitized.isNotEmpty()) sanitized else "clip_${System.currentTimeMillis()}$ext"
        if (!name.lowercase().endsWith(ext.lowercase())) {
            name = if (name.contains(".")) name.substringBeforeLast(".") + ext else name + ext
        }
        val tmp = java.io.File(cacheDir, name)
        if (tmp.exists()) tmp.delete()
        tmp.writeBytes(bytes)
        val uri = androidx.core.content.FileProvider.getUriForFile(this, "$packageName.fileprovider", tmp)
        val clip = ClipData.newUri(contentResolver, "FuseItAll image", uri)
        if (sensitive) {
            try {
                clip.description.extras = android.os.PersistableBundle().apply {
                    putBoolean("android.content.extra.IS_SENSITIVE", true)
                }
            } catch (_: Exception) {}
        }
        (getSystemService(Context.CLIPBOARD_SERVICE) as ClipboardManager).setPrimaryClip(clip)
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

    // Launchable apps for the per-app notification filter UI. Best-effort:
    // label + package only (icons ride per-post to save bandwidth), capped
    // 500, sorted by label. Never throws across the channel.
    private fun listLaunchableApps(): List<Map<String, String>> {
        return try {
            val pm = packageManager
            val intent = Intent(Intent.ACTION_MAIN).addCategory(Intent.CATEGORY_LAUNCHER)
            val infos = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
                pm.queryIntentActivities(intent, android.content.pm.PackageManager.ResolveInfoFlags.of(0))
            } else {
                @Suppress("DEPRECATION")
                pm.queryIntentActivities(intent, 0)
            }
            infos.mapNotNull { ri ->
                val pkg = ri.activityInfo?.packageName ?: return@mapNotNull null
                if (pkg == packageName) return@mapNotNull null
                val label = try {
                    ri.loadLabel(pm)?.toString()?.trim().takeIf { !it.isNullOrEmpty() } ?: pkg
                } catch (_: Exception) {
                    pkg
                }
                mapOf("package_name" to pkg, "app" to label)
            }.distinctBy { it["package_name"] }.sortedBy { (it["app"] ?: "").lowercase() }.take(500)
        } catch (_: Exception) {
            emptyList()
        }
    }

    // In-memory icon cache for paged inventory (100 entries, RAM only).
    private val appIconCache = LruCache<String, String>(100)

    // Paged app inventory for Mac fetch (notif-apps-req). Sorted by
    // package_name (stable across locales) so cursor pagination never skips
    // or repeats rows when labels change. Runs on photoExecutor, never the
    // UI thread. Returns {entries: [{package_name, app, [app_icon_b64]}],
    // next_cursor}. Never throws: failures yield an empty page.
    private fun listNotifAppsPaged(cursor: String, limit: Int, withIcons: Boolean): Map<String, Any> {
        val safeLimit = limit.coerceIn(1, 50)
        val safeCursor = cursor.trim().take(128)
        return try {
            val pm = packageManager
            val intent = Intent(Intent.ACTION_MAIN).addCategory(Intent.CATEGORY_LAUNCHER)
            val infos = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
                pm.queryIntentActivities(intent, android.content.pm.PackageManager.ResolveInfoFlags.of(0))
            } else {
                @Suppress("DEPRECATION")
                pm.queryIntentActivities(intent, 0)
            }
            val rows = infos.mapNotNull { ri ->
                val pkg = ri.activityInfo?.packageName ?: return@mapNotNull null
                if (pkg == packageName) return@mapNotNull null
                if (pkg.length > 128) return@mapNotNull null
                val label = try {
                    ri.loadLabel(pm)?.toString()?.trim().takeIf { !it.isNullOrEmpty() } ?: pkg
                } catch (_: Exception) {
                    pkg
                }
                Pair(pkg, label.take(64))
            }.distinctBy { it.first }.sortedBy { it.first }.take(500)
            var start = 0
            if (safeCursor.isNotEmpty()) {
                val idx = rows.indexOfFirst { it.first > safeCursor }
                start = if (idx == -1) rows.size else idx
            }
            val page = rows.drop(start).take(safeLimit)
            val entries = page.map { (pkg, label) ->
                val m = mutableMapOf<String, Any>("package_name" to pkg, "app" to label)
                if (withIcons) {
                    val icon = loadAppIconB64(pkg)
                    if (icon.isNotEmpty()) m["app_icon_b64"] = icon
                }
                m.toMap()
            }
            val hasMore = start + page.size < rows.size
            val nextCursor = if (hasMore && page.isNotEmpty()) page.last().first else ""
            mapOf("entries" to entries, "next_cursor" to nextCursor)
        } catch (_: Exception) {
            mapOf("entries" to emptyList<Map<String, String>>(), "next_cursor" to "")
        }
    }

    private fun loadAppIconB64(pkg: String): String {
        if (pkg.isEmpty()) return ""
        appIconCache.get(pkg)?.let { return it }
        return try {
            val pm = packageManager
            val drawable = pm.getApplicationIcon(pkg)
            var b64 = encodeAppDrawable(drawable, 96)
            if (b64.length > 32768) {
                b64 = encodeAppDrawable(drawable, 64)
            }
            if (b64.length > 32768) b64 = ""
            if (b64.isNotEmpty()) appIconCache.put(pkg, b64)
            b64
        } catch (_: Exception) {
            ""
        }
    }

    private fun encodeAppDrawable(d: Drawable, size: Int): String {
        val bitmap = try {
            if (d is BitmapDrawable && d.bitmap != null) {
                d.bitmap
            } else {
                val b = Bitmap.createBitmap(size, size, Bitmap.Config.ARGB_8888)
                val canvas = Canvas(b)
                d.setBounds(0, 0, size, size)
                d.draw(canvas)
                b
            }
        } catch (_: Exception) {
            return ""
        }
        val scaled = if (bitmap.width != size || bitmap.height != size) {
            try { Bitmap.createScaledBitmap(bitmap, size, size, true) } catch (_: Exception) { bitmap }
        } else bitmap
        return try {
            val out = ByteArrayOutputStream()
            scaled.compress(Bitmap.CompressFormat.PNG, 100, out)
            val bytes = out.toByteArray()
            if (bytes.size > 64 * 1024) "" else Base64.encodeToString(bytes, Base64.NO_WRAP)
        } catch (_: Exception) {
            ""
        }
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

    private fun isContactsGranted(): Boolean {
        return try {
            ContextCompat.checkSelfPermission(this, android.Manifest.permission.READ_CONTACTS) == PackageManager.PERMISSION_GRANTED
        } catch (_: Exception) {
            false
        }
    }

    private fun isSmsGranted(): Boolean {
        return try {
            ContextCompat.checkSelfPermission(this, android.Manifest.permission.READ_SMS) == PackageManager.PERMISSION_GRANTED &&
                ContextCompat.checkSelfPermission(this, android.Manifest.permission.SEND_SMS) == PackageManager.PERMISSION_GRANTED
        } catch (_: Exception) {
            false
        }
    }

    // Photo ID scheme (mirrors core ParsePhotoID): legacy pure digits and
    // unknown prefixes are opaque image rows; img:<row> / vid:<row> select
    // the MediaStore collection. Returns (isVideo, rowId) or null.
    private fun parsePhotoID(photoId: String): Pair<Boolean, Long>? {
        val t = photoId.trim()
        if (t.isEmpty() || t.length > 128) return null
        if (t.startsWith("img:")) {
            val rest = t.substring(4)
            if (rest.isEmpty() || rest.contains(":") || rest.contains("/") || rest.contains("\\")) return null
            val row = rest.toLongOrNull() ?: return null
            return Pair(false, row)
        }
        if (t.startsWith("vid:")) {
            val rest = t.substring(4)
            if (rest.isEmpty() || rest.contains(":") || rest.contains("/") || rest.contains("\\")) return null
            val row = rest.toLongOrNull() ?: return null
            return Pair(true, row)
        }
        val row = t.toLongOrNull() ?: return null
        return Pair(false, row)
    }

    private fun photoUri(isVideo: Boolean, row: Long): Uri =
        if (isVideo) ContentUris.withAppendedId(MediaStore.Video.Media.EXTERNAL_CONTENT_URI, row)
        else ContentUris.withAppendedId(MediaStore.Images.Media.EXTERNAL_CONTENT_URI, row)

    private fun queryPhotos(cursor: String, limit: Int): Map<String, Any> {
        // Check permission fail-closed: throw SecurityException for Dart to map to permission_denied.
        val perm = getPhotosPermission()
        if (perm == "denied") throw SecurityException("Photos permission denied")
        // Cursor is "<taken>_<photo_id>" (photo_id namespaced for video).
        // Total order across collections: taken DESC, img before vid, row DESC.
        var cursorTaken: Long? = null
        var cursorIsVideo = false
        var cursorRow: Long? = null
        if (cursor.isNotEmpty()) {
            val cut = cursor.lastIndexOf("_")
            if (cut > 0) {
                cursorTaken = cursor.substring(0, cut).toLongOrNull()
                val idPart = cursor.substring(cut + 1)
                val parsed = parsePhotoID(idPart)
                if (parsed != null) {
                    cursorIsVideo = parsed.first
                    cursorRow = parsed.second
                } else {
                    cursorTaken = null
                }
            }
        }
        val images = queryOneCollection(false, cursorTaken, cursorIsVideo, cursorRow, limit)
        val videos = queryOneCollection(true, cursorTaken, cursorIsVideo, cursorRow, limit)
        // Merge-sort by (taken DESC, img-before-vid, row DESC).
        val merged = mutableListOf<Map<String, Any>>()
        var i = 0
        var j = 0
        while (merged.size < limit && (i < images.size || j < videos.size)) {
            val a = if (i < images.size) images[i] else null
            val b = if (j < videos.size) videos[j] else null
            val takeA = when {
                a == null -> false
                b == null -> true
                else -> comparePhotoRows(a, b) <= 0
            }
            if (takeA) { merged.add(a!!); i++ } else { merged.add(b!!); j++ }
        }
        var nextCursor = ""
        if (merged.size == limit) {
            val last = merged.last()
            nextCursor = "${last["taken_at"]}_${last["photo_id"]}"
        }
        return mapOf("entries" to merged, "next_cursor" to nextCursor)
    }

    // comparePhotoRows orders (taken DESC, img-before-vid, row DESC).
    // Returns negative when a sorts first.
    private fun comparePhotoRows(a: Map<String, Any>, b: Map<String, Any>): Int {
        val takenA = (a["taken_at"] as? Number)?.toLong() ?: 0L
        val takenB = (b["taken_at"] as? Number)?.toLong() ?: 0L
        if (takenA != takenB) return if (takenA > takenB) -1 else 1
        val aVideo = (a["media_type"] as? String) == "video"
        val bVideo = (b["media_type"] as? String) == "video"
        if (aVideo != bVideo) return if (!aVideo) -1 else 1
        val rowA = photoRowOf("${a["photo_id"]}")
        val rowB = photoRowOf("${b["photo_id"]}")
        return rowB.compareTo(rowA)
    }

    private fun photoRowOf(photoId: String): Long {
        val t = photoId.trim()
        val rest = if (t.startsWith("img:") || t.startsWith("vid:")) t.substring(4) else t
        return rest.toLongOrNull() ?: 0L
    }

    private fun queryOneCollection(
        isVideo: Boolean,
        cursorTaken: Long?,
        cursorIsVideo: Boolean,
        cursorRow: Long?,
        limit: Int,
    ): List<Map<String, Any>> {
        val dateTaken: String
        val dateModified: String
        val idColName: String
        val collection: Uri
        val projection: Array<String>
        if (isVideo) {
            collection = MediaStore.Video.Media.EXTERNAL_CONTENT_URI
            dateTaken = MediaStore.Video.Media.DATE_TAKEN
            dateModified = MediaStore.Video.Media.DATE_MODIFIED
            idColName = MediaStore.Video.Media._ID
            projection = arrayOf(
                MediaStore.Video.Media._ID,
                MediaStore.Video.Media.DATE_TAKEN,
                MediaStore.Video.Media.DATE_MODIFIED,
                MediaStore.Video.Media.WIDTH,
                MediaStore.Video.Media.HEIGHT,
                MediaStore.Video.Media.MIME_TYPE,
                MediaStore.Video.Media.SIZE,
                MediaStore.Video.Media.DURATION,
            )
        } else {
            collection = MediaStore.Images.Media.EXTERNAL_CONTENT_URI
            dateTaken = MediaStore.Images.Media.DATE_TAKEN
            dateModified = MediaStore.Images.Media.DATE_MODIFIED
            idColName = MediaStore.Images.Media._ID
            projection = arrayOf(
                MediaStore.Images.Media._ID,
                MediaStore.Images.Media.DATE_TAKEN,
                MediaStore.Images.Media.DATE_MODIFIED,
                MediaStore.Images.Media.WIDTH,
                MediaStore.Images.Media.HEIGHT,
                MediaStore.Images.Media.MIME_TYPE,
                MediaStore.Images.Media.SIZE,
                MediaStore.Images.Media.ORIENTATION,
            )
        }
        // Same-taken tie-break must match the merge order (img before vid):
        // images keep rows with (taken < T) or (taken == T and (cursor is a
        // video or row < cursorRow)); videos keep (taken < T) or
        // (taken == T and cursor is a video and row < cursorRow).
        val selection: String?
        val selectionArgs: Array<String>?
        if (cursorTaken != null && cursorRow != null) {
            if (!isVideo && cursorIsVideo) {
                selection = "($dateTaken < ? OR ($dateTaken = ?)) OR ($dateTaken IS NULL AND $dateModified * 1000 < ?)"
                selectionArgs = arrayOf(cursorTaken.toString(), cursorTaken.toString(), cursorTaken.toString())
            } else if (isVideo && !cursorIsVideo) {
                selection = "($dateTaken < ?) OR ($dateTaken IS NULL AND $dateModified * 1000 < ?)"
                selectionArgs = arrayOf(cursorTaken.toString(), cursorTaken.toString())
            } else {
                selection = "($dateTaken < ? OR ($dateTaken = ? AND $idColName < ?)) OR ($dateTaken IS NULL AND $dateModified * 1000 < ?)"
                selectionArgs = arrayOf(cursorTaken.toString(), cursorTaken.toString(), cursorRow.toString(), cursorTaken.toString())
            }
        } else {
            selection = null
            selectionArgs = null
        }
        val sortOrderSql = "$dateTaken DESC, $idColName DESC"
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
        val out = mutableListOf<Map<String, Any>>()
        cursorObj?.use { c ->
            val idCol = c.getColumnIndexOrThrow(idColName)
            val takenCol = c.getColumnIndexOrThrow(dateTaken)
            val modCol = c.getColumnIndexOrThrow(dateModified)
            val wCol = c.getColumnIndexOrThrow(if (isVideo) MediaStore.Video.Media.WIDTH else MediaStore.Images.Media.WIDTH)
            val hCol = c.getColumnIndexOrThrow(if (isVideo) MediaStore.Video.Media.HEIGHT else MediaStore.Images.Media.HEIGHT)
            val mimeCol = c.getColumnIndexOrThrow(if (isVideo) MediaStore.Video.Media.MIME_TYPE else MediaStore.Images.Media.MIME_TYPE)
            val sizeCol = c.getColumnIndexOrThrow(if (isVideo) MediaStore.Video.Media.SIZE else MediaStore.Images.Media.SIZE)
            val extraCol = c.getColumnIndex(if (isVideo) MediaStore.Video.Media.DURATION else MediaStore.Images.Media.ORIENTATION)
            var count = 0
            while (c.moveToNext() && count < limit) {
                val id = c.getLong(idCol)
                var taken = try { c.getLong(takenCol) } catch (_: Exception) { 0L }
                if (taken == 0L) {
                    taken = try { c.getLong(modCol) * 1000 } catch (_: Exception) { 0L }
                    if (taken == 0L) taken = System.currentTimeMillis()
                }
                val w = try { c.getInt(wCol) } catch (_: Exception) { 0 }
                val h = try { c.getInt(hCol) } catch (_: Exception) { 0 }
                val mime = try { c.getString(mimeCol) ?: "" } catch (_: Exception) { "" }
                val sz = try { c.getLong(sizeCol) } catch (_: Exception) { 0L }
                val entry = mutableMapOf<String, Any>(
                    "photo_id" to if (isVideo) "vid:$id" else id.toString(),
                    "taken_at" to taken,
                    "width" to w,
                    "height" to h,
                    "mime" to mime,
                    "size" to sz,
                )
                if (isVideo) {
                    entry["media_type"] = "video"
                    val dur = if (extraCol >= 0) try { c.getLong(extraCol) } catch (_: Exception) { 0L } else 0L
                    if (dur > 0) entry["duration_ms"] = dur
                } else {
                    val orient = if (extraCol >= 0) try { c.getInt(extraCol) } catch (_: Exception) { 0 } else 0
                    if (orient != 0) entry["orientation"] = orient
                }
                out.add(entry)
                count++
            }
        }
        return out
    }

    private fun getPhotoThumb(photoId: String, size: Int): Map<String, Any> {
        val cacheKey = "${photoId}_${size}"
        val cached = photoThumbCache.get(cacheKey)
        if (cached != null) {
            return cached
        }

        val parsed = parsePhotoID(photoId) ?: throw IllegalArgumentException("bad photo_id")
        val uri = photoUri(parsed.first, parsed.second)

        if (!parsed.first) {
            // Fast path (images only): raw EXIF embedded thumbnail directly
            // from the file stream with zero decoding, ~0.1ms read time.
            try {
                contentResolver.openInputStream(uri)?.use { ins ->
                    val exif = ExifInterface(ins)
                    val thumbBytes = exif.thumbnailBytes
                    if (thumbBytes != null && thumbBytes.isNotEmpty()) {
                        val b64 = Base64.encodeToString(thumbBytes, Base64.NO_WRAP)
                        val res = mapOf("mime" to "image/jpeg", "data_b64" to b64)
                        photoThumbCache.put(cacheKey, res)
                        return res
                    }
                }
            } catch (_: Exception) {}
        }

        // Video thumbs are frame grabs; images fall through to the generic path.
        val bmp: Bitmap = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
            contentResolver.loadThumbnail(uri, Size(size, size), CancellationSignal())
        } else if (parsed.first) {
            val retriever = MediaMetadataRetriever()
            try {
                retriever.setDataSource(this, uri)
                retriever.getFrameAtTime(0) ?: throw IllegalArgumentException("thumb not found")
            } finally {
                try { retriever.release() } catch (_: Exception) {}
            }
        } else {
            @Suppress("DEPRECATION")
            MediaStore.Images.Thumbnails.getThumbnail(contentResolver, parsed.second, MediaStore.Images.Thumbnails.MINI_KIND, null)
                ?: throw IllegalArgumentException("thumb not found")
        }
        val out = ByteArrayOutputStream()
        bmp.compress(Bitmap.CompressFormat.JPEG, 75, out)
        val bytes = out.toByteArray()
        bmp.recycle()
        val b64 = Base64.encodeToString(bytes, Base64.NO_WRAP)
        val res = mapOf("mime" to "image/jpeg", "data_b64" to b64)
        photoThumbCache.put(cacheKey, res)
        return res
    }


    private fun getPhotoSize(photoId: String): Long {
        val parsed = parsePhotoID(photoId) ?: throw IllegalArgumentException("bad photo_id")
        val uri = photoUri(parsed.first, parsed.second)
        val sizeCol = if (parsed.first) MediaStore.Video.Media.SIZE else MediaStore.Images.Media.SIZE
        contentResolver.query(uri, arrayOf(sizeCol), null, null, null)?.use { c ->
            if (c.moveToFirst()) {
                val idx = c.getColumnIndex(sizeCol)
                if (idx >= 0) {
                    val v = try { c.getLong(idx) } catch (_: Exception) { -1L }
                    if (v >= 0) return v
                }
            }
        }
        // Fallback: descriptor length without materializing bytes (videos can
        // be gigabytes; never readBytes() to measure).
        try {
            contentResolver.openAssetFileDescriptor(uri, "r")?.use { afd ->
                if (afd.length >= 0) return afd.length
            }
        } catch (_: Exception) {}
        return 0
    }

    private fun readPhotoChunk(photoId: String, offset: Long, len: Int): String {
        if (len < 0 || len > 1024 * 1024) throw IllegalArgumentException("bad len")
        if (offset < 0) throw IllegalArgumentException("bad offset")
        val parsed = parsePhotoID(photoId) ?: throw IllegalArgumentException("bad photo_id")
        val uri = photoUri(parsed.first, parsed.second)
        // Seek without materializing: skip in a loop (content streams may
        // short-skip), then bounded read. Videos can be gigabytes.
        contentResolver.openInputStream(uri)?.use { ins ->
            var remaining = offset
            while (remaining > 0) {
                val skipped = ins.skip(remaining)
                if (skipped <= 0) {
                    // skip() stalled: consume one byte to make progress.
                    if (ins.read() == -1) throw IllegalArgumentException("bad offset")
                    remaining--
                } else {
                    remaining -= skipped
                }
            }
            val buf = ByteArray(len)
            var read = 0
            while (read < len) {
                val n = ins.read(buf, read, len - read)
                if (n == -1) break
                read += n
            }
            if (read == 0 && len > 0) throw IllegalArgumentException("bad offset")
            return Base64.encodeToString(buf, 0, read, Base64.NO_WRAP)
        }
        throw IllegalArgumentException("photo not found")
    }

    private fun deletePhotos(ids: List<String>): Map<String, Any> {
        val results = mutableListOf<Map<String, Any>>()
        for (id in ids) {
            val parsed = parsePhotoID(id)
            if (parsed == null) {
                results.add(mapOf("photo_id" to id, "ok" to false, "error" to "invalid id", "error_code" to "invalid_arg"))
                continue
            }
            val uri = photoUri(parsed.first, parsed.second)
            try {
                val rows = contentResolver.delete(uri, null, null)
                if (rows > 0) {
                    photoThumbCache.remove("${id}_256")
                    photoThumbCache.remove("${id}_512")
                    photoThumbCache.remove("${id}_1024")
                    photoThumbCache.remove(id)
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

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        setIntent(intent)
        if (intent.getBooleanExtra(EXTRA_CLIPSEND, false)) {
            pendingClipSend = true
            intent.removeExtra(EXTRA_CLIPSEND)
        }
    }

    override fun onDestroy() {
        try {
            val cm = getSystemService(Context.CONNECTIVITY_SERVICE) as ConnectivityManager
            networkCallback?.let { cm.unregisterNetworkCallback(it) }
        } catch (_: Exception) {
        }
        networkCallback = null
        networkEvents = null
        // Engine-bound messenger dies with the engine: clear so the
        // ClipSendActivity cold path (relaunch MainActivity) applies.
        try {
            if (clipMessenger != null) clipMessenger = null
        } catch (_: Exception) {
        }
        super.onDestroy()
    }

    private fun isNetworkAvailable(): Boolean {
        return try {
            val cm = getSystemService(Context.CONNECTIVITY_SERVICE) as ConnectivityManager
            val network = cm.activeNetwork ?: return false
            val caps = cm.getNetworkCapabilities(network) ?: return false
            caps.hasCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET) &&
                caps.hasCapability(NetworkCapabilities.NET_CAPABILITY_VALIDATED)
        } catch (_: Exception) {
            false
        }
    }

    private fun registerNetworkMonitor(engine: FlutterEngine) {
        try {
            val cm = getSystemService(Context.CONNECTIVITY_SERVICE) as ConnectivityManager
            val callback = object : ConnectivityManager.NetworkCallback() {
                override fun onAvailable(network: Network) {
                    runOnUiThread { networkEvents?.success(true) }
                }

                override fun onLost(network: Network) {
                    runOnUiThread { networkEvents?.success(isNetworkAvailable()) }
                }
            }
            networkCallback = callback
            cm.registerDefaultNetworkCallback(callback)
        } catch (_: Exception) {
            // Older/emulator environments may reject the callback; Dart's
            // WebSocket watchdog and manual reconnect remain the fallback.
        }
    }

    companion object {
        /** Intent extra: ClipSendActivity cold path asks Dart for one send. */
        const val EXTRA_CLIPSEND = "fuseitall.clipsend"

        /** Live engine messenger for ClipSendActivity focus callbacks. Null when Dart is dead. */
        @Volatile
        var clipMessenger: BinaryMessenger? = null

        /** Cold-path latch, drained once by Dart via clipSend/popRequested. */
        @Volatile
        var pendingClipSend: Boolean = false
    }
}
