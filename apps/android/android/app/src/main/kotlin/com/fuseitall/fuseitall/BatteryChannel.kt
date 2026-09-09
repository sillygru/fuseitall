// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

package com.fuseitall.fuseitall

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.content.IntentFilter
import android.os.BatteryManager
import android.os.Build
import io.flutter.plugin.common.EventChannel

// Event-driven battery level stream for real-time Mac sidebar updates.
// The OS pushes ACTION_BATTERY_CHANGED on every level/status change, so
// there is no polling: Dart forwards distinct readings over the persistent
// WebSocket as battery-only ping envelopes. Fail-soft: malformed extras
// are skipped and channel errors never throw.
class BatteryStreamHandler(private val appContext: Context) : EventChannel.StreamHandler {
    private var receiver: BroadcastReceiver? = null

    override fun onListen(args: Any?, sink: EventChannel.EventSink) {
        val filter = IntentFilter(Intent.ACTION_BATTERY_CHANGED)
        val r = object : BroadcastReceiver() {
            override fun onReceive(ctx: Context?, intent: Intent?) {
                val reading = readingOf(intent) ?: return
                try {
                    sink.success(reading)
                } catch (_: Exception) {
                }
            }
        }
        receiver = r
        // Sticky broadcast: registration also replays the current state via
        // onReceive, but the explicit emit below covers OEMs that defer it.
        // Dart dedupes identical readings, so a double emit is harmless.
        val sticky = try {
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
                appContext.registerReceiver(r, filter, Context.RECEIVER_EXPORTED)
            } else {
                @Suppress("DEPRECATION")
                appContext.registerReceiver(r, filter)
            }
        } catch (_: Exception) {
            null
        }
        val initial = readingOf(sticky)
        if (initial != null) {
            try {
                sink.success(initial)
            } catch (_: Exception) {
            }
        }
    }

    override fun onCancel(args: Any?) {
        val r = receiver
        receiver = null
        if (r != null) {
            try {
                appContext.unregisterReceiver(r)
            } catch (_: Exception) {
            }
        }
    }

    companion object {
        // Map a battery-changed intent to {battery_pct, charging}. Null when
        // the extras are missing or out of range: callers skip, never crash.
        // Pure apart from reading intent extras.
        fun readingOf(intent: Intent?): Map<String, Any>? {
            if (intent == null) return null
            val level = intent.getIntExtra(BatteryManager.EXTRA_LEVEL, -1)
            val scale = intent.getIntExtra(BatteryManager.EXTRA_SCALE, 0)
            if (level < 0 || scale <= 0) return null
            val pct = (level * 100 / scale).coerceIn(0, 100)
            val status = intent.getIntExtra(BatteryManager.EXTRA_STATUS, -1)
            val charging = status == BatteryManager.BATTERY_STATUS_CHARGING ||
                status == BatteryManager.BATTERY_STATUS_FULL
            return mapOf("battery_pct" to pct, "charging" to charging)
        }
    }
}
