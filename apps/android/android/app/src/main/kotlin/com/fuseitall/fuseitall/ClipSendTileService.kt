// SPDX-License-Identifier: AGPL-3.0-only

// Quick Settings "Send to Mac" tile (Android 7+): one tap from anywhere
// collapses QS and opens the invisible ClipSendActivity, which takes focus
// so the clipboard read succeeds on Android 10+. Push-only: fires solely on
// user tap. API 34+ requires the PendingIntent variant of
// startActivityAndCollapse (Intent variant throws).
package com.fuseitall.fuseitall

import android.app.PendingIntent
import android.content.Intent
import android.os.Build
import android.service.quicksettings.TileService

class ClipSendTileService : TileService() {
    override fun onClick() {
        super.onClick()
        // Locked devices collapse the launch: retry after unlock.
        if (isLocked) {
            try {
                unlockAndRun { launchFocusActivity() }
            } catch (_: Exception) {
            }
            return
        }
        launchFocusActivity()
    }

    private fun launchFocusActivity() {
        try {
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.UPSIDE_DOWN_CAKE) {
                // NEW_TASK only: never disturb the app task hosting Flutter.
                val intent = Intent(this, ClipSendActivity::class.java).apply {
                    addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
                }
                val pi = PendingIntent.getActivity(
                    this,
                    REQ_CLIPSEND_TILE,
                    intent,
                    PendingIntent.FLAG_ONE_SHOT or PendingIntent.FLAG_IMMUTABLE,
                )
                startActivityAndCollapse(pi)
            } else {
                @Suppress("DEPRECATION")
                val intent = Intent(this, ClipSendActivity::class.java).apply {
                    addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
                }
                @Suppress("DEPRECATION")
                startActivityAndCollapse(intent)
            }
        } catch (_: Exception) {
        }
    }

    companion object {
        private const val REQ_CLIPSEND_TILE = 42
    }
}
