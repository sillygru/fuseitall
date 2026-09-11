// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

package com.fuseitall.fuseitall

import android.app.NotificationManager
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.content.IntentFilter
import android.os.Build
import android.provider.Settings
import io.flutter.plugin.common.BinaryMessenger
import io.flutter.plugin.common.EventChannel
import io.flutter.plugin.common.MethodCall
import io.flutter.plugin.common.MethodChannel

// Event-driven Do Not Disturb bridge between Android NotificationManager
// and Flutter/Mac. ACTION_INTERRUPTION_FILTER_CHANGED pushes state changes
// in real time (no polling). setInterruptionFilter sets Priority vs All.
class DndChannel(
    private val context: Context,
    messenger: BinaryMessenger
) : MethodChannel.MethodCallHandler, EventChannel.StreamHandler {

    private var receiver: BroadcastReceiver? = null
    private var eventSink: EventChannel.EventSink? = null

    init {
        MethodChannel(messenger, "fuseitall/dnd").setMethodCallHandler(this)
        EventChannel(messenger, "fuseitall/dndEvents").setStreamHandler(this)
    }

    override fun onMethodCall(call: MethodCall, result: MethodChannel.Result) {
        val nm = context.getSystemService(Context.NOTIFICATION_SERVICE) as? NotificationManager
        if (nm == null) {
            result.error("NO_SERVICE", "NotificationManager unavailable", null)
            return
        }

        when (call.method) {
            "getDndState" -> {
                result.success(currentReading())
            }
            "isDndAccessGranted" -> {
                result.success(isAccessGranted())
            }
            "openDndSettings" -> {
                try {
                    val intent = Intent(Settings.ACTION_NOTIFICATION_POLICY_ACCESS_SETTINGS).apply {
                        addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
                    }
                    context.startActivity(intent)
                    result.success(true)
                } catch (e: Exception) {
                    result.error("NO_SETTINGS", e.message, null)
                }
            }
            "setDnd" -> {
                val enabled = call.argument<Boolean>("enabled") ?: false
                if (!isAccessGranted()) {
                    result.error("PERMISSION_DENIED", "Notification policy access not granted", null)
                    return
                }
                try {
                    val targetFilter = if (enabled) {
                        NotificationManager.INTERRUPTION_FILTER_PRIORITY
                    } else {
                        NotificationManager.INTERRUPTION_FILTER_ALL
                    }
                    nm.setInterruptionFilter(targetFilter)
                    result.success(true)
                } catch (e: SecurityException) {
                    result.error("PERMISSION_DENIED", e.message, null)
                } catch (e: Exception) {
                    result.error("SET_FAILED", e.message, null)
                }
            }
            else -> result.notImplemented()
        }
    }

    override fun onListen(arguments: Any?, sink: EventChannel.EventSink) {
        eventSink = sink
        val filter = IntentFilter(NotificationManager.ACTION_INTERRUPTION_FILTER_CHANGED)
        val r = object : BroadcastReceiver() {
            override fun onReceive(ctx: Context?, intent: Intent?) {
                try {
                    sink.success(currentReading())
                } catch (_: Exception) {
                }
            }
        }
        receiver = r
        try {
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
                context.registerReceiver(r, filter, Context.RECEIVER_EXPORTED)
            } else {
                @Suppress("DEPRECATION")
                context.registerReceiver(r, filter)
            }
        } catch (_: Exception) {
        }
        try {
            sink.success(currentReading())
        } catch (_: Exception) {
        }
    }

    override fun onCancel(arguments: Any?) {
        val r = receiver
        receiver = null
        eventSink = null
        if (r != null) {
            try {
                context.unregisterReceiver(r)
            } catch (_: Exception) {
            }
        }
    }

    private fun isAccessGranted(): Boolean {
        val nm = context.getSystemService(Context.NOTIFICATION_SERVICE) as? NotificationManager
            ?: return false
        return nm.isNotificationPolicyAccessGranted
    }

    private fun currentReading(): Map<String, Any> {
        val nm = context.getSystemService(Context.NOTIFICATION_SERVICE) as? NotificationManager
        val hasPerm = nm?.isNotificationPolicyAccessGranted == true
        val filter = nm?.currentInterruptionFilter ?: NotificationManager.INTERRUPTION_FILTER_ALL
        val enabled = filter != NotificationManager.INTERRUPTION_FILTER_ALL &&
            filter != NotificationManager.INTERRUPTION_FILTER_UNKNOWN
        return mapOf(
            "enabled" to enabled,
            "has_permission" to hasPerm
        )
    }
}
