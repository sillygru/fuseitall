// SPDX-License-Identifier: AGPL-3.0-only

package com.fuseitall.fuseitall

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.os.Build

// Re-arm the keepalive service after reboot so pairing survives restarts
// without opening the app. No network I/O here — Dart reconnects on launch.
class BootReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        if (intent.action != Intent.ACTION_BOOT_COMPLETED) return
        val svc = Intent(context, LinkService::class.java)
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            context.startForegroundService(svc)
        } else {
            context.startService(svc)
        }
    }
}
