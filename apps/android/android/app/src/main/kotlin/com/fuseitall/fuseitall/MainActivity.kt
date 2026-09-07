package com.fuseitall.fuseitall

import android.content.ClipData
import android.content.ClipboardManager
import android.content.Context
import android.content.Intent
import android.os.Build
import android.os.PowerManager
import android.provider.Settings
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
        // Permissions + battery status for the Essential Services card.
        // Never throws across the channel: unknown states return false/"unknown".
        MethodChannel(flutterEngine.dartExecutor.binaryMessenger, "fuseitall/permissions")
            .setMethodCallHandler { call, result ->
                when (call.method) {
                    "isNotificationListenerEnabled" ->
                        result.success(isListenerEnabled())
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
        EventChannel(flutterEngine.dartExecutor.binaryMessenger, "fuseitall/clipboardEvents")
            .setStreamHandler(object : EventChannel.StreamHandler {
                override fun onListen(args: Any?, sink: EventChannel.EventSink) {
                    clipEvents = sink
                    val cm = getSystemService(Context.CLIPBOARD_SERVICE) as ClipboardManager
                    val listener = ClipboardManager.OnPrimaryClipChangedListener {
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
        // Local clipboard read for the watcher fallback path.
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
                    else -> result.notImplemented()
                }
            }
    }

    private fun isListenerEnabled(): Boolean {
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
