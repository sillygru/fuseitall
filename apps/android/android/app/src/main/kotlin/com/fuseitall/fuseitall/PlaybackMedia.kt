// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

package com.fuseitall.fuseitall

import android.content.ComponentName
import android.content.Context
import android.graphics.Bitmap
import android.media.MediaMetadata
import android.media.session.MediaController
import android.media.session.MediaSessionManager
import android.media.session.PlaybackState
import android.os.Build
import android.provider.Settings
import android.util.Base64
import java.io.ByteArrayOutputStream

// Native MediaSession reader for playback sync (0.10.0). Uses the same
// notification-listener grant as notifications: no new permission.
// All functions are best-effort and never throw across the channel.
object PlaybackMedia {

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

    fun current(ctx: Context): Map<String, Any?>? {
        val list = controllers(ctx)
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
            val pos = try {
                (st?.position ?: 0L).coerceIn(0, 86_400_000)
            } catch (_: Exception) { 0L }
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
            val art = artworkB64(md)
            if (art != null) {
                out["artwork_b64"] = art.first
                out["artwork_mime"] = art.second
            }
            return out
        }
        return null
    }

    private fun artworkB64(md: MediaMetadata): Pair<String, String>? {
        return try {
            val bmp = md.getBitmap(MediaMetadata.METADATA_KEY_ALBUM_ART)
                ?: md.getBitmap(MediaMetadata.METADATA_KEY_DISPLAY_ICON)
                ?: md.getBitmap(MediaMetadata.METADATA_KEY_ART)
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

    fun command(ctx: Context, cmd: String): Boolean {
        val c = cmd.trim().lowercase()
        if (c != "play" && c != "pause" && c != "toggle" && c != "next" && c != "prev") return false
        val list = controllers(ctx)
        if (list.isEmpty()) return false
        val target = list.firstOrNull {
            it.playbackState?.state == PlaybackState.STATE_PLAYING
        } ?: list.sortedBy { it.packageName }.firstOrNull() ?: return false
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
