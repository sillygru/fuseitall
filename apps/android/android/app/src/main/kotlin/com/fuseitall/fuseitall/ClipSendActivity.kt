// SPDX-License-Identifier: AGPL-3.0-only

// Invisible transient foreground activity that makes phone -> Mac clipboard
// sync reliable on Android 10+.
//
// Background reads are blocked by the OS (ClipboardService only allows the
// focused UID, the default IME, or a signature holder). A foreground service
// does NOT count. This activity briefly takes window focus so our UID is
// focused, then tells Dart (same process, running Flutter engine) to perform
// its normal foreground read + send. It finishes as soon as Dart acks, or on
// a failsafe timeout. No UI is ever shown: translucent theme, 1px view, no
// touch handling, excluded from recents.
//
// Triggered two ways (same activity, only the trigger differs):
// - user tap: LinkService notification action or Quick Settings tile.
// - opt-in auto: ClipAutoWatcher (logcat trigger, needs adb READ_LOGS grant).
// Push-only: no polling here, fires solely on explicit intents.
package com.fuseitall.fuseitall

import android.app.Activity
import android.content.Intent
import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.view.View
import android.view.WindowManager
import io.flutter.plugin.common.BinaryMessenger
import io.flutter.plugin.common.MethodChannel

class ClipSendActivity : Activity() {
    private var fired = false
    private val failsafe = Handler(Looper.getMainLooper())
    private val failsafeFinish = Runnable {
        try {
            finish()
        } catch (_: Exception) {
        }
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        active = this
        // 1px non-interactive view: window exists (gains focus) but nothing renders.
        setContentView(View(this), android.view.ViewGroup.LayoutParams(1, 1))
        try {
            val wlp = window.attributes
            wlp.dimAmount = 0f
            wlp.flags = wlp.flags or WindowManager.LayoutParams.FLAG_LAYOUT_NO_LIMITS or
                WindowManager.LayoutParams.FLAG_NOT_TOUCH_MODAL or
                WindowManager.LayoutParams.FLAG_NOT_TOUCHABLE
            window.attributes = wlp
        } catch (_: Exception) {
        }
        // Never strand an invisible foreground activity: Dart acks via
        // clipSendDone, otherwise this closes on its own.
        failsafe.postDelayed(failsafeFinish, FAILSAFE_MS)
    }

    override fun onWindowFocusChanged(hasFocus: Boolean) {
        super.onWindowFocusChanged(hasFocus)
        if (!hasFocus || fired) return
        fired = true
        val messenger: BinaryMessenger? = MainActivity.clipMessenger
        if (messenger == null) {
            // Dart engine is dead (process kept alive by LinkService only).
            // Bounce through the full UI: MainActivity recreates the engine
            // and Dart drains the pending request on startup.
            try {
                val main = Intent(this, MainActivity::class.java).apply {
                    addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_CLEAR_TOP or Intent.FLAG_ACTIVITY_SINGLE_TOP)
                    putExtra(MainActivity.EXTRA_CLIPSEND, true)
                }
                startActivity(main)
            } catch (_: Exception) {
            }
            finish()
            return
        }
        try {
            // The UID is focused from here until finish(): Dart's normal
            // clipboard reads (framework + fuseitall/clipboard channel, incl.
            // chunked large-image lanes) succeed, then Dart acks.
            MethodChannel(messenger, CHANNEL).invokeMethod("onClipFocus", null)
        } catch (_: Exception) {
            finish()
        }
    }

    override fun onDestroy() {
        failsafe.removeCallbacks(failsafeFinish)
        if (active === this) active = null
        super.onDestroy()
    }

    companion object {
        const val CHANNEL = "fuseitall/clipSend"
        private const val FAILSAFE_MS = 12000L

        @Volatile
        private var active: ClipSendActivity? = null

        /** Called from MainActivity's clipSendDone handler: Dart finished. */
        fun notifyDone() {
            try {
                active?.finish()
            } catch (_: Exception) {
            }
        }

        fun intentForFocus(context: android.content.Context): Intent =
            Intent(context, ClipSendActivity::class.java).apply {
                // NEW_TASK only: never disturb the app task hosting Flutter.
                addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
            }
    }
}
