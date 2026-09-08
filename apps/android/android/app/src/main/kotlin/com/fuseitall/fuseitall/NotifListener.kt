package com.fuseitall.fuseitall

import android.content.ComponentName
import android.content.Context
import android.graphics.Bitmap
import android.graphics.Canvas
import android.graphics.drawable.BitmapDrawable
import android.graphics.drawable.Drawable
import android.os.Build
import android.os.Handler
import android.os.Looper
import android.service.notification.NotificationListenerService
import android.service.notification.StatusBarNotification
import android.util.Base64
import android.util.LruCache
import io.flutter.plugin.common.EventChannel
import java.io.ByteArrayOutputStream
import java.security.MessageDigest
import java.util.concurrent.ConcurrentLinkedQueue
import java.util.concurrent.atomic.AtomicReference

/**
 * System notification listener for FuseItAll mirroring (0.2.0).
 *
 * The service queues post/remove events while the Flutter UI is
 * backgrounded; Dart drains them via MainActivity's `fuseitall/notif`
 * MethodChannel (`pollNotifs`, which clears per call). Bodies are
 * truncated here (title 128, text 512 chars) so oversized notifications
 * never cross the channel. Nothing is logged: contents stay in memory
 * with a file-backed backup (filesDir/notif_queue.json, cap 100) so
 * a process kill between post and drain does not drop events.
 *
 * Enablement is user-controlled in system settings (BIND_NOTIFICATION_
 * LISTENER_SERVICE); without access the queue simply stays empty and
 * presence is unaffected. No battery-optimization exemption is requested:
 * the service relies on the system-bound NLS priority + LinkService
 * dataSync foreground service + WorkManager retry for durability.
 */
class NotifListener : NotificationListenerService() {

    data class Event(
        val kind: String,
        val id: String,
        val app: String,
        val packageName: String,
        val iconB64: String,
        val groupKey: String,
        val title: String,
        val text: String,
        val postedAt: Long,
    )

    companion object {
        private const val MAX_QUEUE = 100
        private const val MAX_TITLE = 128
        private const val MAX_TEXT = 512
        private const val MAX_ICON_B64 = 32768
        private const val ICON_SIZE = 96
        private const val ICON_SIZE_FALLBACK = 64
        private const val QUEUE_FILE = "notif_queue.json"
        private val queue = ConcurrentLinkedQueue<Event>()
        private val instance = AtomicReference<NotifListener?>()
        private val iconCache = LruCache<String, String>(100)
        private val keyToIdMap = LruCache<String, String>(500)
        private val idToKeyMap = LruCache<String, String>(500)
        private val mainHandler = Handler(Looper.getMainLooper())
        @Volatile
        private var eventSink: EventChannel.EventSink? = null
        private var lastRebindAttempt = 0L

        @JvmStatic
        fun setEventSink(sink: EventChannel.EventSink?) {
            eventSink = sink
        }

        @JvmStatic
        fun isConnected(): Boolean = instance.get() != null

        @JvmStatic
        fun ensureBound(context: Context) {
            if (instance.get() != null) return
            val now = System.currentTimeMillis()
            if (now - lastRebindAttempt < 3000) return
            lastRebindAttempt = now

            val component = ComponentName(context, NotifListener::class.java)
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.N) {
                try {
                    requestRebind(component)
                } catch (_: Exception) {}
            }
            try {
                val pm = context.packageManager
                pm.setComponentEnabledSetting(
                    component,
                    android.content.pm.PackageManager.COMPONENT_ENABLED_STATE_DISABLED,
                    android.content.pm.PackageManager.DONT_KILL_APP,
                )
                pm.setComponentEnabledSetting(
                    component,
                    android.content.pm.PackageManager.COMPONENT_ENABLED_STATE_ENABLED,
                    android.content.pm.PackageManager.DONT_KILL_APP,
                )
            } catch (_: Exception) {}
        }

        /**
         * Converts long system notification keys (which can exceed protocol length limits)
         * to safe, deterministic IDs while maintaining a reverse lookup for dismissals.
         */
        @JvmStatic
        fun toProtocolId(key: String): String {
            if (key.length <= 128) return key
            keyToIdMap.get(key)?.let { return it }
            return try {
                val digest = MessageDigest.getInstance("SHA-256").digest(key.toByteArray(Charsets.UTF_8))
                val hex = digest.joinToString("") { "%02x".format(it) }
                val shortId = "h:" + hex.substring(0, 32)
                keyToIdMap.put(key, shortId)
                idToKeyMap.put(shortId, key)
                shortId
            } catch (_: Exception) {
                truncateRuneStatic(key, 128)
            }
        }

        @JvmStatic
        fun truncateRuneStatic(s: String, max: Int): String {
            val t = s.trim()
            if (t.isEmpty()) return ""
            val runes = t.codePointCount(0, t.length)
            if (runes <= max) return t
            var idx = 0
            var count = 0
            while (idx < t.length && count < max) {
                val cp = t.codePointAt(idx)
                idx += Character.charCount(cp)
                count++
            }
            return t.substring(0, idx).trim()
        }

        /**
         * Cancel a system notification the Mac dismissed (key from the
         * original post event, or mapped shortId). No-op below API 26 (no key-based cancel)
         * or when the listener is not bound.
         */
        @JvmStatic
        fun cancelKey(keyOrId: String) {
            if (Build.VERSION.SDK_INT < Build.VERSION_CODES.O) return
            val realKey = idToKeyMap.get(keyOrId) ?: keyOrId
            try {
                instance.get()?.cancelNotification(realKey)
            } catch (_: Exception) {
                // Listener not bound or key gone: mirror already converged.
            }
        }

        @JvmStatic
        fun eventToMap(e: Event): Map<String, Any> {
            val base = mutableMapOf<String, Any>(
                "event" to e.kind,
                "id" to e.id,
                "app" to e.app,
                "title" to e.title,
                "text" to e.text,
                "posted_at" to e.postedAt,
            )
            if (e.packageName.isNotEmpty()) base["package_name"] = e.packageName
            if (e.iconB64.isNotEmpty()) base["app_icon_b64"] = e.iconB64
            if (e.groupKey.isNotEmpty()) base["group_key"] = e.groupKey
            return base
        }

        @JvmStatic
        fun drain(): List<Map<String, Any>> {
            val out = mutableListOf<Map<String, Any>>()
            while (true) {
                val e = queue.poll() ?: break
                out.add(eventToMap(e))
            }
            if (out.isNotEmpty()) clearPersistedQueue() else {
                // Still clear if queue emptied outside drain (e.g. capped).
                if (queue.isEmpty()) clearPersistedQueue()
            }
            return out
        }

        private fun enqueue(e: Event) {
            queue.add(e)
            while (queue.size > MAX_QUEUE) {
                queue.poll()
            }
            persistQueue()
            val sink = eventSink
            if (sink != null) {
                val map = eventToMap(e)
                mainHandler.post {
                    try {
                        sink.success(map)
                    } catch (_: Exception) {}
                }
            }
        }

        private fun persistQueue() {
            val inst = instance.get() ?: return
            try {
                val arr = org.json.JSONArray()
                for (ev in queue) {
                    val o = org.json.JSONObject()
                    o.put("kind", ev.kind)
                    o.put("id", ev.id)
                    o.put("app", ev.app)
                    o.put("package_name", ev.packageName)
                    o.put("app_icon_b64", ev.iconB64)
                    o.put("group_key", ev.groupKey)
                    o.put("title", ev.title)
                    o.put("text", ev.text)
                    o.put("posted_at", ev.postedAt)
                    arr.put(o)
                }
                inst.openFileOutput(QUEUE_FILE, android.content.Context.MODE_PRIVATE).use { out ->
                    out.write(arr.toString().toByteArray())
                }
            } catch (_: Exception) {
                // Persistence is best-effort; memory queue still holds events.
            }
        }

        private fun restoreQueue(svc: NotifListener) {
            if (queue.isNotEmpty()) return
            try {
                val bytes = svc.openFileInput(QUEUE_FILE).use { it.readBytes() }
                if (bytes.isEmpty()) return
                val arr = org.json.JSONArray(String(bytes))
                for (i in 0 until arr.length()) {
                    val o = arr.getJSONObject(i)
                    queue.add(
                        Event(
                            kind = o.optString("kind", "post"),
                            id = o.optString("id", ""),
                            app = o.optString("app", ""),
                            packageName = o.optString("package_name", ""),
                            iconB64 = o.optString("app_icon_b64", ""),
                            groupKey = o.optString("group_key", ""),
                            title = o.optString("title", ""),
                            text = o.optString("text", ""),
                            postedAt = o.optLong("posted_at", 0),
                        ),
                    )
                }
                while (queue.size > MAX_QUEUE) queue.poll()
            } catch (_: Exception) {
            }
        }

        private fun clearPersistedQueue() {
            val inst = instance.get() ?: return
            try {
                inst.openFileOutput(QUEUE_FILE, android.content.Context.MODE_PRIVATE).use { out ->
                    out.write("[]".toByteArray())
                }
            } catch (_: Exception) {
            }
        }
    }

    override fun onListenerConnected() {
        instance.set(this)
        restoreQueue(this)
        // Sweep active notifications so posts that arrived while unbound are
        // not lost (AOSP SystemUI pattern). Deduplicate via key inside
        // enqueue path (queue already holds recent posts).
        try {
            val active = getActiveNotifications()
            if (active != null) {
                val seenKeys = queue.map { it.id }.toSet()
                for (sbn in active) {
                    val k = sbn.key ?: continue
                    val key = toProtocolId(k)
                    if (seenKeys.contains(key)) continue
                    if (!wantedForMirror(sbn)) continue
                    enqueueFromSbn(sbn)
                }
            }
        } catch (_: Exception) {
        }
    }

    override fun onListenerDisconnected() {
        instance.compareAndSet(this, null)
        try {
            requestRebind(ComponentName(this, NotifListener::class.java))
        } catch (_: Exception) {
        }
    }

    override fun onDestroy() {
        super.onDestroy()
        instance.compareAndSet(this, null)
        try {
            requestRebind(ComponentName(this, NotifListener::class.java))
        } catch (_: Exception) {
        }
    }

    private fun wantedForMirror(sbn: StatusBarNotification): Boolean {
        if (sbn.isOngoing) return false
        if (sbn.packageName == packageName) return false
        // Group summaries are synthetic if child notifications exist.
        try {
            if ((sbn.notification.flags and android.app.Notification.FLAG_GROUP_SUMMARY) != 0) {
                val extras = sbn.notification.extras
                val text = extras.getCharSequence(android.app.Notification.EXTRA_TEXT)?.toString()
                    ?: extras.getCharSequence(android.app.Notification.EXTRA_BIG_TEXT)?.toString()
                    ?: ""
                if (text.isEmpty()) return false
                val active = instance.get()?.activeNotifications
                if (active != null) {
                    val group = sbn.notification.group
                    if (!group.isNullOrEmpty()) {
                        val hasChild = active.any { other ->
                            other.key != sbn.key &&
                            other.packageName == sbn.packageName &&
                            other.notification.group == group &&
                            (other.notification.flags and android.app.Notification.FLAG_GROUP_SUMMARY) == 0
                        }
                        if (hasChild) return false
                    }
                }
            }
        } catch (_: Exception) {
        }
        return true
    }

    private fun enqueueFromSbn(sbn: StatusBarNotification) {
        val rawKey = sbn.key ?: return
        val key = toProtocolId(rawKey)
        val pkg = sbn.packageName ?: ""
        val appLabel = loadAppLabel(pkg)
        val extras = sbn.notification.extras

        var title = extras.getCharSequence(android.app.Notification.EXTRA_TITLE)?.toString() ?: ""
        if (title.isEmpty()) {
            title = extras.getCharSequence("android.title.big")?.toString() ?: ""
        }
        if (title.isEmpty()) {
            title = extras.getCharSequence(android.app.Notification.EXTRA_CONVERSATION_TITLE)?.toString() ?: ""
        }

        var text = extras.getCharSequence(android.app.Notification.EXTRA_TEXT)?.toString() ?: ""
        if (text.isEmpty()) {
            text = extras.getCharSequence(android.app.Notification.EXTRA_BIG_TEXT)?.toString() ?: ""
        }
        if (text.isEmpty()) {
            text = extras.getCharSequence(android.app.Notification.EXTRA_SUMMARY_TEXT)?.toString() ?: ""
        }
        if (text.isEmpty()) {
            val lines = extras.getCharSequenceArray(android.app.Notification.EXTRA_TEXT_LINES)
            if (lines != null && lines.isNotEmpty()) {
                text = lines.filterNotNull().joinToString("\n") { it.toString() }
            }
        }
        if (text.isEmpty()) {
            try {
                // Handle NotificationCompat.MessagingStyle (android.messages)
                val msgs = extras.getParcelableArray("android.messages")
                if (msgs != null && msgs.isNotEmpty()) {
                    val lastMsg = msgs.lastOrNull()
                    if (lastMsg is android.os.Bundle) {
                        text = lastMsg.getCharSequence("text")?.toString() ?: ""
                        if (title.isEmpty()) {
                            title = lastMsg.getCharSequence("sender")?.toString() ?: ""
                        }
                    }
                }
            } catch (_: Exception) {}
        }
        if (text.isEmpty()) {
            val ticker = sbn.notification.tickerText?.toString() ?: ""
            if (ticker.isNotEmpty()) {
                text = ticker
            }
        }
        if (title.isEmpty() && text.isNotEmpty()) {
            title = appLabel
        }

        val cleanTitle = truncateRuneStatic(title, MAX_TITLE)
        val cleanText = truncateRuneStatic(text, MAX_TEXT)
        if (cleanTitle.isEmpty() && cleanText.isEmpty()) return

        val groupKey = sbn.notification.group ?: ""
        val iconB64 = loadIconB64(pkg)
        enqueue(
            Event(
                kind = "post",
                id = key,
                app = appLabel,
                packageName = pkg,
                iconB64 = iconB64,
                groupKey = truncateRuneStatic(groupKey, 128),
                title = cleanTitle,
                text = cleanText,
                postedAt = System.currentTimeMillis() / 1000,
            ),
        )
    }

    override fun onNotificationPosted(sbn: StatusBarNotification) {
        if (!wantedForMirror(sbn)) return
        enqueueFromSbn(sbn)
    }

    override fun onNotificationRemoved(sbn: StatusBarNotification) {
        val rawKey = sbn.key ?: return
        val key = toProtocolId(rawKey)
        enqueue(
            Event(
                kind = "remove",
                id = key,
                app = "",
                packageName = "",
                iconB64 = "",
                groupKey = "",
                title = "",
                text = "",
                postedAt = 0,
            ),
        )
    }

    private fun truncateRune(s: String, max: Int): String = truncateRuneStatic(s, max)

    private fun loadAppLabel(pkg: String): String {
        if (pkg.isEmpty()) return ""
        return try {
            val pm = packageManager
            val info = pm.getApplicationInfo(pkg, 0)
            pm.getApplicationLabel(info).toString()
        } catch (_: Exception) {
            pkg
        }
    }

    private fun loadIconB64(pkg: String): String {
        if (pkg.isEmpty()) return ""
        iconCache.get(pkg)?.let { return it }
        return try {
            val pm = packageManager
            val drawable = pm.getApplicationIcon(pkg)
            var b64 = encodeDrawable(drawable, ICON_SIZE)
            if (b64.length > MAX_ICON_B64) {
                b64 = encodeDrawable(drawable, ICON_SIZE_FALLBACK)
            }
            if (b64.length > MAX_ICON_B64) b64 = ""
            if (b64.isNotEmpty()) iconCache.put(pkg, b64)
            b64
        } catch (_: Exception) {
            ""
        }
    }

    private fun encodeDrawable(d: Drawable, size: Int): String {
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

    private fun truncate(s: String, max: Int): String {
        val t = s.trim()
        if (t.length <= max) return t
        return t.substring(0, max).trim()
    }
}
