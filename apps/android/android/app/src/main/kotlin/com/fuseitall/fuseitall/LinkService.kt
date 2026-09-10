package com.fuseitall.fuseitall

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.Service
import android.content.Intent
import android.os.Build
import android.os.IBinder

// Battery-efficient keepalive: a low-priority foreground service so the
// Go phone server + clipboard listener survive without the app on screen.
// No polling here — presence + features ride the persistent WebSocket;
// this service only holds process priority + shows the persistent status
// icon. Type dataSync
// (LAN mirror) is Play-safe without requesting battery-exemption.
// No battery-optimization exemption is requested: default settings must work.
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
        val sendIntent = Intent(this, ClipSendActivity::class.java).apply {
            // NEW_TASK only: never disturb the existing app task (which hosts
            // the Flutter engine this trigger notifies).
            addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
        }
        val sendPi = PendingIntent.getActivity(
            this,
            REQ_CLIPSEND,
            sendIntent,
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
        )
        val notif = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            Notification.Builder(this, CHANNEL_ID)
                .setContentTitle("FuseItAll link active")
                .setContentText("Keeping phone ↔ Mac sync ready")
                .setSmallIcon(android.R.drawable.stat_sys_data_bluetooth)
                // One-tap phone -> Mac clipboard: tapping is a BAL-exempt user
                // interaction that opens the invisible focus activity, so the
                // read succeeds on Android 10+ without opening the app.
                .addAction(android.R.drawable.ic_menu_send, "Send to Mac", sendPi)
                .build()
        } else {
            @Suppress("DEPRECATION")
            Notification.Builder(this)
                .setContentTitle("FuseItAll link active")
                .setContentText("Keeping phone ↔ Mac sync ready")
                .setSmallIcon(android.R.drawable.stat_sys_data_bluetooth)
                .addAction(android.R.drawable.ic_menu_send, "Send to Mac", sendPi)
                .build()
        }
        // Never let a permission skew FATAL the app process: missing
        // FOREGROUND_SERVICE_DATA_SYNC or POST_NOTIFICATIONS can throw
        // SecurityException at startForeground on 34+. Degrade gracefully.
        try {
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
                startForeground(NOTIF_ID, notif, android.content.pm.ServiceInfo.FOREGROUND_SERVICE_TYPE_DATA_SYNC)
            } else {
                startForeground(NOTIF_ID, notif)
            }
        } catch (e: SecurityException) {
            // Try without type as last resort (pre-34 behavior).
            try {
                startForeground(NOTIF_ID, notif)
            } catch (_: SecurityException) {
                stopSelf()
            }
        }
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        // Re-arm the opt-in clipboard auto trigger after reboot/restarts.
        try {
            ClipAutoWatcher.rearmIfEnabled(this)
        } catch (_: Exception) {
        }
        return START_STICKY
    }

    override fun onDestroy() {
        try {
            ClipAutoWatcher.stop()
        } catch (_: Exception) {
        }
        super.onDestroy()
    }

    companion object {
        const val CHANNEL_ID = "fuseitall_link"
        const val NOTIF_ID = 41
        private const val REQ_CLIPSEND = 41
    }
}
