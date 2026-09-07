package com.fuseitall.fuseitall

import android.os.Build
import android.service.notification.NotificationListenerService
import android.service.notification.StatusBarNotification
import java.util.concurrent.ConcurrentLinkedQueue
import java.util.concurrent.atomic.AtomicReference

/**
 * System notification listener for FuseItAll mirroring (0.2.0).
 *
 * The service queues post/remove events while the Flutter UI is
 * backgrounded; Dart drains them via MainActivity's `fuseitall/notif`
 * MethodChannel (`pollNotifs`, which clears per call). Bodies are
 * truncated here (title 128, text 512 chars) so oversized notifications
 * never cross the channel. Nothing is logged: contents stay in memory.
 *
 * Enablement is user-controlled in system settings (BIND_NOTIFICATION_
 * LISTENER_SERVICE); without access the queue simply stays empty and
 * presence is unaffected.
 */
class NotifListener : NotificationListenerService() {

    data class Event(
        val kind: String,
        val id: String,
        val app: String,
        val title: String,
        val text: String,
        val postedAt: Long,
    )

    companion object {
        private const val MAX_QUEUE = 100
        private const val MAX_TITLE = 128
        private const val MAX_TEXT = 512
        private val queue = ConcurrentLinkedQueue<Event>()
        private val instance = AtomicReference<NotifListener?>()

        /**
         * Cancel a system notification the Mac dismissed (key from the
         * original post event). No-op below API 26 (no key-based cancel)
         * or when the listener is not bound.
         */
        @JvmStatic
        fun cancelKey(key: String) {
            if (Build.VERSION.SDK_INT < Build.VERSION_CODES.O) return
            try {
                instance.get()?.cancelNotification(key)
            } catch (_: Exception) {
                // Listener not bound or key gone: mirror already converged.
            }
        }

        @JvmStatic
        fun drain(): List<Map<String, Any>> {
            val out = mutableListOf<Map<String, Any>>()
            while (true) {
                val e = queue.poll() ?: break
                out.add(
                    mapOf(
                        "event" to e.kind,
                        "id" to e.id,
                        "app" to e.app,
                        "title" to e.title,
                        "text" to e.text,
                        "posted_at" to e.postedAt,
                    ),
                )
            }
            return out
        }

        private fun enqueue(e: Event) {
            queue.add(e)
            while (queue.size > MAX_QUEUE) {
                queue.poll()
            }
        }
    }

    override fun onListenerConnected() {
        instance.set(this)
    }

    override fun onListenerDisconnected() {
        instance.compareAndSet(this, null)
    }

    override fun onNotificationPosted(sbn: StatusBarNotification) {
        val key = sbn.key ?: return
        if (sbn.isOngoing) return
        if (sbn.packageName == packageName) return
        val extras = sbn.notification.extras
        val title = truncate(
            (extras.getCharSequence("android.title")?.toString() ?: ""),
            MAX_TITLE,
        )
        val text = truncate(
            (extras.getCharSequence("android.text")?.toString() ?: ""),
            MAX_TEXT,
        )
        if (title.isEmpty() && text.isEmpty()) return
        enqueue(
            Event(
                kind = "post",
                id = key,
                app = sbn.packageName ?: "",
                title = title,
                text = text,
                postedAt = System.currentTimeMillis() / 1000,
            ),
        )
    }

    override fun onNotificationRemoved(sbn: StatusBarNotification) {
        val key = sbn.key ?: return
        enqueue(
            Event(
                kind = "remove",
                id = key,
                app = "",
                title = "",
                text = "",
                postedAt = 0,
            ),
        )
    }

    private fun truncate(s: String, max: Int): String {
        val t = s.trim()
        if (t.length <= max) return t
        return t.substring(0, max).trim()
    }
}
