// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

// Opt-in automatic phone -> Mac clipboard trigger (Stage 2).
//
// Same OS wall as the manual path: only a focused UID may read. This watcher
// tails logcat for ClipboardService's denial line for our own package (which
// proves the user just copied while we were backgrounded) and then opens the
// invisible ClipSendActivity to grab it. Mirrors KDE Connect's proven
// READ_LOGS approach.
//
// Requirements (one-time, sideload/power-user):
// - adb: pm grant <pkg> android.permission.READ_LOGS
// - overlay allowed: appops set <pkg> SYSTEM_ALERT_WINDOW allow (or Settings)
//   so the background activity start is exempt (BAL_ALLOW_SAW_PERMISSION).
// Without both, enabling the toggle fails loud and Stage 1 (tap to send)
// keeps working. No polling of the clipboard itself: this tails the log
// stream (blocking read, zero work until a denial line appears).
package com.fuseitall.fuseitall

import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.os.Build
import android.provider.Settings
import android.util.Log
import java.io.BufferedReader
import java.io.InputStreamReader
import java.text.SimpleDateFormat
import java.util.Date
import java.util.Locale

object ClipAutoWatcher {
    private const val TAG = "ClipAutoWatcher"
    private const val PREFS = "fuseitall_clip_auto"
    private const val KEY_ENABLED = "enabled"

    @Volatile
    private var watchThread: Thread? = null

    @Volatile
    private var watchProc: Process? = null

    fun isEnabled(ctx: Context): Boolean =
        try {
            ctx.getSharedPreferences(PREFS, Context.MODE_PRIVATE).getBoolean(KEY_ENABLED, false)
        } catch (_: Exception) {
            false
        }

    fun isReadLogsGranted(ctx: Context): Boolean =
        try {
            ctx.checkSelfPermission(android.Manifest.permission.READ_LOGS) ==
                PackageManager.PERMISSION_GRANTED
        } catch (_: Exception) {
            false
        }

    fun isOverlayAllowed(ctx: Context): Boolean =
        try {
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.M) {
                Settings.canDrawOverlays(ctx)
            } else {
                true
            }
        } catch (_: Exception) {
            false
        }

    fun openOverlaySettings(ctx: Context) {
        try {
            val intent = Intent(
                Settings.ACTION_MANAGE_OVERLAY_PERMISSION,
                android.net.Uri.parse("package:${ctx.packageName}"),
            ).apply { addFlags(Intent.FLAG_ACTIVITY_NEW_TASK) }
            ctx.startActivity(intent)
        } catch (_: Exception) {
            try {
                ctx.startActivity(
                    Intent(Settings.ACTION_MANAGE_OVERLAY_PERMISSION).apply {
                        addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
                    },
                )
            } catch (e2: Exception) {
                Log.w(TAG, "open overlay settings failed: ${e2.message}")
            }
        }
    }

    /** Persist the toggle and start/stop the tail. Never throws. */
    fun setEnabled(ctx: Context, enabled: Boolean) {
        try {
            ctx.getSharedPreferences(PREFS, Context.MODE_PRIVATE)
                .edit().putBoolean(KEY_ENABLED, enabled).apply()
        } catch (e: Exception) {
            Log.w(TAG, "persist auto pref failed: ${e.message}")
        }
        if (enabled) start(ctx.applicationContext) else stop()
    }

    /** Re-arm after reboot/service restart when the toggle was left on. */
    fun rearmIfEnabled(ctx: Context) {
        if (isEnabled(ctx)) start(ctx.applicationContext)
    }

    @Synchronized
    fun start(appCtx: Context) {
        if (watchThread?.isAlive == true) return
        if (!isReadLogsGranted(appCtx)) {
            Log.w(TAG, "not starting: READ_LOGS not granted (adb grant required)")
            return
        }
        val pkg = appCtx.packageName
        val t = Thread({
            var proc: Process? = null
            try {
                val stamp = SimpleDateFormat("yyyy-MM-dd HH:mm:ss.SSS", Locale.US).format(Date())
                // Log tag format changed after API 35 (VANILLA_ICE_CREAM).
                val filter = if (Build.VERSION.SDK_INT > 34) "E ClipboardService" else "ClipboardService:E"
                proc = Runtime.getRuntime().exec(arrayOf("logcat", "-T", stamp, filter, "*:S"))
                watchProc = proc
                BufferedReader(InputStreamReader(proc.inputStream)).forEachLine { line ->
                    if (Thread.currentThread().isInterrupted) return@forEachLine
                    if (line.contains(pkg)) {
                        triggerSend(appCtx)
                    }
                }
            } catch (e: Exception) {
                if (!Thread.currentThread().isInterrupted) {
                    Log.w(TAG, "logcat tail ended: ${e.message}")
                }
            } finally {
                try {
                    proc?.destroy()
                } catch (_: Exception) {
                }
                if (watchProc === proc) watchProc = null
            }
        }, "clip-auto-logcat")
        t.isDaemon = true
        watchThread = t
        t.start()
    }

    @Synchronized
    fun stop() {
        try {
            watchThread?.interrupt()
        } catch (_: Exception) {
        }
        try {
            watchProc?.destroy()
        } catch (_: Exception) {
        }
        watchThread = null
    }

    private fun triggerSend(appCtx: Context) {
        try {
            appCtx.startActivity(ClipSendActivity.intentForFocus(appCtx))
        } catch (e: Exception) {
            // Background activity start can still be blocked without the
            // overlay exemption: loud in logcat, Stage 1 tap keeps working.
            Log.w(TAG, "auto trigger blocked (allow overlay): ${e.message}")
        }
    }
}
