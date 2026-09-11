// SPDX-License-Identifier: AGPL-3.0-only

package com.fuseitall.fuseitall

import android.content.ComponentName
import android.content.Context
import android.graphics.Bitmap
import android.graphics.BitmapFactory
import android.media.MediaMetadata
import android.media.session.MediaController
import android.media.session.MediaSessionManager
import android.media.session.PlaybackState
import android.os.Build
import android.os.Handler
import android.os.Looper
import android.os.SystemClock
import android.provider.Settings
import android.util.Base64
import io.flutter.plugin.common.EventChannel
import java.io.ByteArrayOutputStream

// Native MediaSession reader for playback sync. Event-driven push (no
// polling): MediaController callbacks + active-sessions listener emit over
// EventChannel fuseitall/playbackEvents. Uses the same
// notification-listener grant as notifications: no new permission.
// All functions are best-effort and never throw across the channel.
object PlaybackMedia {

    private val mainHandler = Handler(Looper.getMainLooper())

    @Volatile
    private var eventSink: EventChannel.EventSink? = null

    @Volatile
    private var pushCtx: Context? = null

    private val callbacks = mutableMapOf<String, CallbackEntry>()
    private var sessionsListener: MediaSessionManager.OnActiveSessionsChangedListener? = null
    private var sessionsManager: MediaSessionManager? = null

    private data class CallbackEntry(
        val controller: MediaController,
        val callback: MediaController.Callback,
    )

    fun hasAccess(ctx: Context): Boolean {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O_MR1) {
            try {
                val nm = ctx.getSystemService(android.app.NotificationManager::class.java)
                if (nm != null && nm.isNotificationListenerAccessGranted(
                        ComponentName(ctx, NotifListener::class.java)
                    )
                ) return true
            } catch (_: Exception) {}
        }
        return try {
            val flat = Settings.Secure.getString(
                ctx.contentResolver, "enabled_notification_listeners"
            ) ?: return false
            flat.split(":").any { it.contains(ctx.packageName, ignoreCase = true) }
        } catch (_: Exception) {
            false
        }
    }

    private fun controllers(ctx: Context): List<MediaController> {
        return try {
            val msm = ctx.getSystemService(Context.MEDIA_SESSION_SERVICE) as? MediaSessionManager
                ?: return emptyList()
            val cn = ComponentName(ctx, NotifListener::class.java)
            msm.getActiveSessions(cn)
        } catch (_: Exception) {
            emptyList()
        }
    }

    private fun appLabel(ctx: Context, pkg: String): String {
        return try {
            val pm = ctx.packageManager
            pm.getApplicationLabel(pm.getApplicationInfo(pkg, 0))?.toString()?.take(64) ?: pkg
        } catch (_: Exception) {
            pkg.take(64)
        }
    }

    // No pull API by design: state flows push-only over the EventChannel.
    // Dart re-anchors via watch() (connect/resume/mode-toggle), never pulls.

    // EventChannel lifecycle: Dart subscribes once, native pushes on every
    // MediaController callback. App-scoped context keeps pushes alive while
    // backgrounded under the foreground link service. Never throws.
    @JvmStatic
    fun setEventSink(sink: EventChannel.EventSink?) {
        eventSink = sink
    }

    @JvmStatic
    fun startPush(ctx: Context) {
        try {
            val appCtx = ctx.applicationContext ?: ctx
            pushCtx = appCtx
            val msm = appCtx.getSystemService(Context.MEDIA_SESSION_SERVICE) as? MediaSessionManager
            if (msm != null) {
                sessionsManager = msm
                if (sessionsListener == null) {
                    val listener = MediaSessionManager.OnActiveSessionsChangedListener { list ->
                        rebindCallbacks(appCtx, list ?: emptyList())
                        pushCurrent(appCtx)
                    }
                    if (registerSessionsListener(msm, appCtx, listener)) {
                        sessionsListener = listener
                    } else {
                        // Loud in dev console (Dart onError): without this
                        // listener, brand-new sessions stay invisible until
                        // the next re-anchor. Registration retries on the
                        // next startPush/watch because the field stays null.
                        val sink = eventSink
                        if (sink != null) {
                            mainHandler.post {
                                try { sink.error("NO_SESSION_LISTENER", "active-session listener unavailable", null) } catch (_: Exception) {}
                            }
                        }
                    }
                }
            }
            rebindCallbacks(appCtx, controllers(appCtx))
            // Anchor push so the Mac learns current state on subscribe
            // (or re-anchor on watch) with zero pulls: a real snapshot, or
            // an explicit idle map when nothing plays.
            pushCurrent(appCtx)
        } catch (_: Exception) {}
    }

    // Re-anchor entry for Dart's watch() (connect/resume/mode-toggle).
    // Idempotent: re-registers callbacks and emits one anchor push.
    @JvmStatic
    fun watch(ctx: Context) {
        startPush(ctx)
    }

    private fun registerSessionsListener(
        msm: MediaSessionManager,
        appCtx: Context,
        listener: MediaSessionManager.OnActiveSessionsChangedListener,
    ): Boolean {
        return try {
            val cn = ComponentName(appCtx, NotifListener::class.java)
            try {
                msm.addOnActiveSessionsChangedListener(listener, cn, mainHandler)
            } catch (_: Exception) {
                @Suppress("DEPRECATION")
                msm.addOnActiveSessionsChangedListener(listener, ComponentName(appCtx, NotifListener::class.java))
            }
            true
        } catch (_: Exception) {
            false
        }
    }

    @JvmStatic
    fun stopPush() {
        try {
            for ((_, e) in callbacks) {
                try { e.controller.unregisterCallback(e.callback) } catch (_: Exception) {}
            }
        } catch (_: Exception) {}
        callbacks.clear()
        try {
            val m = sessionsManager
            val l = sessionsListener
            if (m != null && l != null) {
                try { m.removeOnActiveSessionsChangedListener(l) } catch (_: Exception) {}
            }
        } catch (_: Exception) {}
        sessionsListener = null
        sessionsManager = null
        pushCtx = null
    }

    private fun rebindCallbacks(ctx: Context, list: List<MediaController>) {
        try {
            val seen = mutableSetOf<String>()
            for (c in list) {
                // Instance identity: session tokens don't guarantee a
                // stable distinctive toString, and collisions collapse two
                // players into one entry (wrong-player control).
                val key = "${c.packageName ?: "?"}@${System.identityHashCode(c)}"
                seen.add(key)
                if (callbacks.containsKey(key)) continue
                val cb = object : MediaController.Callback() {
                    override fun onPlaybackStateChanged(state: PlaybackState?) {
                        pushCtx?.let { pushCurrent(it) }
                    }
                    override fun onMetadataChanged(metadata: MediaMetadata?) {
                        pushCtx?.let { pushCurrent(it) }
                    }
                    override fun onSessionDestroyed() {
                        val appCtx = pushCtx ?: return
                        rebindCallbacks(appCtx, controllers(appCtx))
                        pushCurrent(appCtx)
                    }
                }
                try {
                    c.registerCallback(cb, mainHandler)
                    callbacks[key] = CallbackEntry(c, cb)
                } catch (_: Exception) {}
            }
            val stale = callbacks.keys.filter { it !in seen }
            for (k in stale) {
                try { callbacks[k]?.let { it.controller.unregisterCallback(it.callback) } } catch (_: Exception) {}
                callbacks.remove(k)
            }
            pushCtx ?: run { pushCtx = ctx.applicationContext ?: ctx }
        } catch (_: Exception) {}
    }

    private fun pushCurrent(ctx: Context) {
        try {
            // Stopped playback is an explicit pushed state, never silence:
            // an empty session list emits idle so the Mac clears instead of
            // showing a phantom playing track forever. Sessions without
            // usable metadata yet emit nothing: their metadata callback
            // follows immediately with the real snapshot (no false idle).
            val ctrls = controllers(ctx)
            val out: Map<String, Any?> = snapshot(ctx, ctrls)
                ?: if (ctrls.isEmpty()) idleSnapshot() else return
            val sink = eventSink ?: return
            mainHandler.post {
                try { sink.success(out) } catch (_: Exception) {}
            }
        } catch (_: Exception) {}
    }

    private fun idleSnapshot(): Map<String, Any?> = mapOf(
        "title" to "",
        "artist" to "",
        "album" to "",
        "package_name" to "",
        "app" to "",
        "state" to "stopped",
        "position_ms" to 0,
        "duration_ms" to 0,
        "origin" to "android",
    )

    private fun snapshot(ctx: Context, list: List<MediaController>): Map<String, Any?>? {
        if (list.isEmpty()) return null
        // Prefer a playing session, else the first with metadata.
        val ordered = list.sortedWith(compareBy(
            { it.playbackState?.state != PlaybackState.STATE_PLAYING },
            { it.packageName }
        ))
        for (c in ordered) {
            val md = c.metadata ?: continue
            val title = (md.getString(MediaMetadata.METADATA_KEY_TITLE)
                ?: md.getString(MediaMetadata.METADATA_KEY_DISPLAY_TITLE)
                ?: "").trim().take(128)
            val artist = (md.getString(MediaMetadata.METADATA_KEY_ARTIST)
                ?: md.getString(MediaMetadata.METADATA_KEY_DISPLAY_SUBTITLE)
                ?: md.getString(MediaMetadata.METADATA_KEY_ALBUM_ARTIST)
                ?: "").trim().take(128)
            val album = (md.getString(MediaMetadata.METADATA_KEY_ALBUM)
                ?: "").trim().take(128)
            val dur = try {
                md.getLong(MediaMetadata.METADATA_KEY_DURATION).coerceIn(0, 86_400_000)
            } catch (_: Exception) { 0L }
            if (title.isEmpty() && artist.isEmpty()) continue
            val st = c.playbackState
            val stateStr = when (st?.state) {
                PlaybackState.STATE_PLAYING -> "playing"
                PlaybackState.STATE_PAUSED -> "paused"
                else -> "stopped"
            }
            val pos = extrapolatedPosition(st)
            val pkg = (c.packageName ?: "").trim().take(128)
            val out = mutableMapOf<String, Any?>(
                "title" to title,
                "artist" to artist,
                "album" to album,
                "package_name" to pkg,
                "app" to appLabel(ctx, pkg),
                "state" to stateStr,
                "position_ms" to pos.toInt(),
                "duration_ms" to dur.toInt(),
                "origin" to "android",
            )
            // Artwork always included when available: pushes are rare
            // (track/state changes only), so per-push bytes beat any
            // strip-and-rediscover scheme that drops covers on reorder.
            val art = artworkB64(ctx, md)
            if (art != null) {
                out["artwork_b64"] = art.first
                out["artwork_mime"] = art.second
            }
            return out
        }
        return null
    }

    private fun extrapolatedPosition(st: PlaybackState?): Long {
        return try {
            val base = (st?.position ?: 0L).coerceIn(0, 86_400_000)
            if (st?.state != PlaybackState.STATE_PLAYING) return base
            val speed = try { st.playbackSpeed } catch (_: Exception) { 1.0f }
            if (speed <= 0f) return base
            val elapsed = try {
                SystemClock.elapsedRealtime() - st.lastPositionUpdateTime
            } catch (_: Exception) { 0L }
            if (elapsed <= 0) return base
            (base + (elapsed * speed).toLong()).coerceIn(0, 86_400_000)
        } catch (_: Exception) { 0L }
    }

    private fun artworkB64(ctx: Context, md: MediaMetadata): Pair<String, String>? {
        return try {
            val bmp = md.getBitmap(MediaMetadata.METADATA_KEY_ALBUM_ART)
                ?: md.getBitmap(MediaMetadata.METADATA_KEY_DISPLAY_ICON)
                ?: md.getBitmap(MediaMetadata.METADATA_KEY_ART)
                ?: bitmapFromUri(ctx, md)
                ?: return null
            val scaled = scaleDown(bmp, 192) ?: return null
            val bos = ByteArrayOutputStream()
            scaled.compress(Bitmap.CompressFormat.JPEG, 70, bos)
            val bytes = bos.toByteArray()
            if (bytes.isEmpty() || bytes.size > 96 * 1024) return null
            val b64 = Base64.encodeToString(bytes, Base64.NO_WRAP)
            if (b64.length > 131072) return null
            Pair(b64, "image/jpeg")
        } catch (_: Exception) {
            null
        }
    }

    // URI-only players (some apps expose no embedded bitmap): resolve
    // ALBUM_ART_URI / ART_URI / DISPLAY_ICON_URI with a sampled decode so
    // huge gallery art never blows memory. Fail-soft: null means no cover.
    private fun bitmapFromUri(ctx: Context, md: MediaMetadata): Bitmap? {
        val uriStr = md.getString(MediaMetadata.METADATA_KEY_ALBUM_ART_URI)
            ?: md.getString(MediaMetadata.METADATA_KEY_ART_URI)
            ?: md.getString(MediaMetadata.METADATA_KEY_DISPLAY_ICON_URI)
            ?: return null
        return try {
            val uri = android.net.Uri.parse(uriStr.trim()) ?: return null
            val cr = ctx.contentResolver
            val bounds = BitmapFactory.Options().apply { inJustDecodeBounds = true }
            try {
                cr.openInputStream(uri)?.use { BitmapFactory.decodeStream(it, null, bounds) }
            } catch (_: Exception) {}
            val w = bounds.outWidth
            val h = bounds.outHeight
            if (w <= 0 || h <= 0) {
                try {
                    cr.openInputStream(uri)?.use { BitmapFactory.decodeStream(it) }
                } catch (_: Exception) { null }
            } else {
                var sample = 1
                while ((w / sample) > 384 || (h / sample) > 384) sample *= 2
                val opts = BitmapFactory.Options().apply { inSampleSize = sample }
                try {
                    cr.openInputStream(uri)?.use { BitmapFactory.decodeStream(it, null, opts) }
                } catch (_: Exception) { null }
            }
        } catch (_: Exception) {
            null
        }
    }

    private fun scaleDown(src: Bitmap, maxSide: Int): Bitmap? {
        return try {
            val w = src.width
            val h = src.height
            if (w <= 0 || h <= 0) return null
            val longest = maxOf(w, h)
            if (longest <= maxSide) return src
            val scale = maxSide.toFloat() / longest.toFloat()
            val nw = (w * scale).toInt().coerceAtLeast(1)
            val nh = (h * scale).toInt().coerceAtLeast(1)
            Bitmap.createScaledBitmap(src, nw, nh, true)
        } catch (_: Exception) {
            null
        }
    }

    fun command(ctx: Context, cmd: String): Boolean = command(ctx, cmd, "")

    fun command(ctx: Context, cmd: String, packageHint: String): Boolean {
        val c = cmd.trim().lowercase()
        if (c != "play" && c != "pause" && c != "toggle" && c != "next" && c != "prev") return false
        val list = controllers(ctx)
        if (list.isEmpty()) return false
        val hint = packageHint.trim()
        val target = if (hint.isNotEmpty()) {
            list.firstOrNull { (it.packageName ?: "").equals(hint, ignoreCase = true) }
                ?: list.firstOrNull { it.playbackState?.state == PlaybackState.STATE_PLAYING }
                ?: list.sortedBy { it.packageName }.firstOrNull()
                ?: return false
        } else {
            list.firstOrNull {
                it.playbackState?.state == PlaybackState.STATE_PLAYING
            } ?: list.sortedBy { it.packageName }.firstOrNull() ?: return false
        }
        return try {
            val tc = target.transportControls ?: return false
            when (c) {
                "play" -> tc.play()
                "pause" -> tc.pause()
                "next" -> tc.skipToNext()
                "prev" -> tc.skipToPrevious()
                else -> {
                    val playing = target.playbackState?.state == PlaybackState.STATE_PLAYING
                    if (playing) tc.pause() else tc.play()
                }
            }
            true
        } catch (_: Exception) {
            false
        }
    }
}
