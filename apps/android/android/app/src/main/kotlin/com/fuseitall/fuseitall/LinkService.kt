package com.fuseitall.fuseitall

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.Service
import android.content.Intent
import android.os.Build
import android.os.IBinder

// Battery-efficient keepalive: a low-priority foreground service so the
// Go phone server + clipboard listener survive without the app on screen.
// No polling here — Dart drives a 60-120s heartbeat; this service only
// holds process priority + shows the persistent status icon.
class LinkService : Service() {
    override fun onBind(intent: Intent?): IBinder? = null

    override fun onCreate() {
        super.onCreate()
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            val mgr = getSystemService(NotificationManager::class.java)
            mgr.createNotificationChannel(
                NotificationChannel(
                    CHANNEL_ID, "FuseItAll link",
                    NotificationManager.IMPORTANCE_MIN,
                ),
            )
        }
        val notif = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            Notification.Builder(this, CHANNEL_ID)
                .setContentTitle("FuseItAll link active")
                .setContentText("Keeping phone ↔ Mac sync ready")
                .setSmallIcon(android.R.drawable.stat_sys_data_bluetooth)
                .build()
        } else {
            @Suppress("DEPRECATION")
            Notification.Builder(this)
                .setContentTitle("FuseItAll link active")
                .setContentText("Keeping phone ↔ Mac sync ready")
                .setSmallIcon(android.R.drawable.stat_sys_data_bluetooth)
                .build()
        }
        // Never let a permission skew FATAL the app process: without the
        // companion permission the FGS type throws SecurityException here.
        // Degrade to no keepalive (Dart heartbeats while foregrounded).
        try {
            startForeground(NOTIF_ID, notif)
        } catch (e: SecurityException) {
            stopSelf()
        }
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int =
        START_STICKY

    companion object {
        const val CHANNEL_ID = "fuseitall_link"
        const val NOTIF_ID = 41
    }
}
