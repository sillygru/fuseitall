// SPDX-License-Identifier: AGPL-3.0-only

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
import android.telephony.PhoneNumberUtils
import android.util.Base64
import android.util.LruCache
import io.flutter.plugin.common.EventChannel
import java.io.ByteArrayOutputStream
import java.security.MessageDigest
import java.util.concurrent.ConcurrentLinkedQueue
import java.util.concurrent.atomic.AtomicReference

/**
 * System notification listener for FuseItAll mirroring (0.9.0, live-only).
 *
 * The service queues post/remove events while the Flutter UI is
 * backgrounded; Dart drains them via MainActivity's `fuseitall/notif`
 * MethodChannel (`pollNotifs`, which clears per call). Bodies are
 * truncated here (title 128, text 512 chars) so oversized notifications
 * never cross the channel. Nothing is logged: contents stay in memory only.
 *
 * Reliability model (0.9.0): live-only, no replay. There is no file-backed
 * queue and no active-notification sweep: notifications posted before the
 * Mac connected are intentionally dropped (TTL 5 min), so opening the Mac
 * app never floods it with stale history. Only genuinely new
 * onNotificationPosted events after connect are mirrored. Progress
 * notifications (Play Store downloads etc.) and per-app muted packages are
 * filtered here; Dart and the Mac re-check (defense in depth).
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
        val ongoing: Boolean = false,
        val hasProgress: Boolean = false,
    )

    companion object {
        private const val MAX_QUEUE = 50
        private const val MAX_TITLE = 128
        private const val MAX_TEXT = 512
        private const val MAX_ICON_B64 = 32768
        private const val ICON_SIZE = 96
        private const val ICON_SIZE_FALLBACK = 64
        // Live-only TTL: queued events older than this are dropped on drain
        // so a Mac that reconnects after hours never receives stale history.
        private const val MAX_AGE_SEC = 300L
        private const val STALE_QUEUE_FILE = "notif_queue.json"
        private val queue = ConcurrentLinkedQueue<Event>()
        private val instance = AtomicReference<NotifListener?>()
        private val iconCache = LruCache<String, String>(100)
        private val keyToIdMap = LruCache<String, String>(500)
        private val idToKeyMap = LruCache<String, String>(500)
        // IDs ever enqueued as posts (cap 500). Removals for unknown IDs are
        // orphan dismiss spam (e.g. for filtered progress posts) and dropped.
        private val sentIds = LruCache<String, Boolean>(500)
        private val mainHandler = Handler(Looper.getMainLooper())
        @Volatile
        private var eventSink: EventChannel.EventSink? = null
        private var lastRebindAttempt = 0L
        // Per-app filter snapshot pushed from Dart (copy-on-write, read-safe).
        // Defaults mirror core NotifAllExceptMuted with empty lists (allow-all).
        @Volatile
        private var filterMode: String = "all_except_muted"
        @Volatile
        private var mutedPackages: Set<String> = emptySet()
        @Volatile
        private var allowedPackages: Set<String> = emptySet()

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
         * Cap mirrors core MaxNotifIDLen (256); Dart/proto/Go enforce the same.
         */
        @JvmStatic
        fun toProtocolId(key: String): String {
            if (key.length <= 256) return key
            keyToIdMap.get(key)?.let { return it }
            return try {
                val digest = MessageDigest.getInstance("SHA-256").digest(key.toByteArray(Charsets.UTF_8))
                val hex = digest.joinToString("") { "%02x".format(it) }
                val shortId = "h:" + hex.substring(0, 32)
                keyToIdMap.put(key, shortId)
                idToKeyMap.put(shortId, key)
                shortId
            } catch (_: Exception) {
                truncateRuneStatic(key, 256)
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

        /**
         * Finds active notifications from messaging apps (matching the conversation address or
         * having mark-as-read semantic action) and triggers SEMANTIC_ACTION_MARK_AS_READ
         * action intent, then dismisses the notification.
         */
        @JvmStatic
        fun markSmsRead(threadId: Long, address: String) {
            val listener = instance.get() ?: return
            val active = try {
                listener.activeNotifications
            } catch (_: Exception) {
                null
            } ?: return

            val normAddr = PhoneNumberUtils.stripSeparators(address)?.trim() ?: ""
            val addrClean = address.trim().lowercase()

            for (sbn in active) {
                try {
                    val notif = sbn.notification ?: continue
                    val extras = notif.extras
                    val title = extras?.getCharSequence(android.app.Notification.EXTRA_TITLE)?.toString()?.trim()?.lowercase() ?: ""
                    val text = extras?.getCharSequence(android.app.Notification.EXTRA_TEXT)?.toString()?.trim()?.lowercase() ?: ""

                    var matches = false
                    if (addrClean.isNotEmpty()) {
                        val titleDigits = PhoneNumberUtils.stripSeparators(title) ?: ""
                        if (title.contains(addrClean) || (normAddr.isNotEmpty() && titleDigits.contains(normAddr))) {
                            matches = true
                        }
                    }

                    // Look through notification actions for mark as read
                    val actions = notif.actions
                    if (actions != null) {
                        for (action in actions) {
                            val isSemanticRead = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.P) {
                                action.semanticAction == android.app.Notification.Action.SEMANTIC_ACTION_MARK_AS_READ
                            } else {
                                false
                            }
                            val actionTitle = action.title?.toString()?.trim()?.lowercase() ?: ""
                            val isTitleRead = actionTitle.contains("read") || actionTitle == "mark as read" || actionTitle == "mark read"

                            if (isSemanticRead || (matches && isTitleRead)) {
                                try {
                                    action.actionIntent.send()
                                } catch (_: Exception) {}
                                if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
                                    try {
                                        listener.cancelNotification(sbn.key)
                                    } catch (_: Exception) {}
                                }
                                break
                            }
                        }
                    }
                } catch (_: Exception) {}
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
            if (e.ongoing) base["ongoing"] = true
            if (e.hasProgress) base["has_progress"] = true
            return base
        }

        /**
         * Push the per-app filter snapshot from Dart (settings-sync LWW blob).
         * Copy-on-write: readers see an atomic snapshot, never a half-update.
         * Unknown modes fall back to allow-all to match core.NormalizeNotifMode.
         */
        @JvmStatic
        fun updateFilter(mode: String?, muted: List<String>?, allowed: List<String>?) {
            val m = mode?.trim()?.lowercase()
            filterMode = if (m == "only_allowed") "only_allowed" else "all_except_muted"
            mutedPackages = muted?.mapNotNull { it.trim().takeIf { t -> t.isNotEmpty() } }
                ?.distinct()?.take(100)?.toSet() ?: emptySet()
            allowedPackages = allowed?.mapNotNull { it.trim().takeIf { t -> t.isNotEmpty() } }
                ?.distinct()?.take(100)?.toSet() ?: emptySet()
        }

        /**
         * Canonical per-app filter, mirroring core.ShouldMirrorNotif: progress
         * always drops; otherwise the mode decides. Empty package is never
         * list-filtered (absent field from old senders). Pure.
         */
        @JvmStatic
        fun shouldMirror(packageName: String, hasProgress: Boolean): Boolean {
            if (hasProgress) return false
            val pkg = packageName.trim()
            return if (filterMode == "only_allowed") {
                pkg.isNotEmpty() && allowedPackages.contains(pkg)
            } else {
                pkg.isEmpty() || !mutedPackages.contains(pkg)
            }
        }

        /** True when the notification carries progress extras (download/install
         * progress bars). String literals avoid API-level constant issues. Pure. */
        @JvmStatic
        fun hasProgressExtras(extras: android.os.Bundle?): Boolean {
            if (extras == null) return false
            return try {
                extras.containsKey("android.progress") ||
                    extras.containsKey("android.progressMax") ||
                    extras.containsKey("android.progressIndeterminate") ||
                    extras.getInt("android.progressMax", 0) > 0 ||
                    extras.getBoolean("android.progressIndeterminate", false)
            } catch (_: Exception) {
                false
            }
        }

        @JvmStatic
        fun drain(): List<Map<String, Any>> {
            val out = mutableListOf<Map<String, Any>>()
            val nowSec = System.currentTimeMillis() / 1000
            while (true) {
                val e = queue.poll() ?: break
                // Live-only TTL: drop stale backlog so a reconnecting Mac never
                // receives hours-old history as if it were new.
                if (e.kind == "post" && e.postedAt > 0 && nowSec - e.postedAt > MAX_AGE_SEC) {
                    continue
                }
                out.add(eventToMap(e))
            }
            return out
        }

        private fun enqueue(e: Event) {
            if (e.kind == "post") {
                // Defense in depth: never queue what the filter forbids, even
                // if a caller skipped wantedForMirror.
                if (!shouldMirror(e.packageName, e.hasProgress)) return
                queue.add(e)
                try {
                    sentIds.put(e.id, true)
                } catch (_: Exception) {}
            } else {
                // Drop orphan dismissals for IDs never posted (e.g. filtered
                // progress ticks): the Mac holds nothing to retract.
                val known = try {
                    sentIds.get(e.id) == true
                } catch (_: Exception) {
                    false
                }
                if (!known) return
                queue.add(e)
            }
            while (queue.size > MAX_QUEUE) {
                queue.poll()
            }
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

        /** One-time migration: delete the pre-0.9.0 file-backed queue so a
         * stale persisted backlog can never replay after upgrade. Best-effort. */
        private fun deleteStaleQueueFile() {
            val inst = instance.get() ?: return
            try {
                inst.deleteFile(STALE_QUEUE_FILE)
            } catch (_: Exception) {
            }
        }
    }

    override fun onListenerConnected() {
        instance.set(this)
        deleteStaleQueueFile()
        // Live-only (0.9.0): no active-notification sweep. Previously this
        // reposted every undismissed notification on each rebind with
        // postedAt=now, flooding the Mac with stale history whenever it
        // opened. Only genuinely new onNotificationPosted events after this
        // point are mirrored. Drop any stale backlog already queued.
        try {
            val nowSec = System.currentTimeMillis() / 1000
            queue.removeIf { e -> e.kind == "post" && e.postedAt > 0 && nowSec - e.postedAt > MAX_AGE_SEC }
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
        // Progress bars (Play Store downloads, installs, file transfers):
        // every percent tick arrives as a same-key repost and would spam the
        // Mac. Dropped here, re-checked in Dart and on the Mac.
        try {
            if (hasProgressExtras(sbn.notification.extras)) return false
        } catch (_: Exception) {
            return false
        }
        // Per-app filter snapshot from Dart (fail-closed on error: allow).
        try {
            if (!shouldMirror(sbn.packageName ?: "", false)) return false
        } catch (_: Exception) {
        }
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
        val extras = sbn.notification.extras
        val hasProgress = hasProgressExtras(extras)
        val ongoing = try {
            sbn.isOngoing
        } catch (_: Exception) {
            false
        }
        // Belt and suspenders: callers check wantedForMirror, but the event
        // channel push path must never emit filtered content.
        if (!shouldMirror(pkg, hasProgress)) return
        val appLabel = loadAppLabel(pkg)

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
        // True system post time: receivers use it for live-only staleness.
        // Fall back to now only when the platform gives nothing usable.
        val postedSec = try {
            val t = sbn.postTime
            if (t > 0) t / 1000 else System.currentTimeMillis() / 1000
        } catch (_: Exception) {
            System.currentTimeMillis() / 1000
        }
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
                postedAt = postedSec,
                ongoing = ongoing,
                hasProgress = hasProgress,
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
