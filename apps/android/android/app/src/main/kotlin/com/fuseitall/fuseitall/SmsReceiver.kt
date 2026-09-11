// SPDX-License-Identifier: AGPL-3.0-only

package com.fuseitall.fuseitall

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.provider.Telephony

class SmsReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context?, intent: Intent?) {
        if (intent?.action != Telephony.Sms.Intents.SMS_RECEIVED_ACTION) return
        try {
            val messages = Telephony.Sms.Intents.getMessagesFromIntent(intent)
            if (messages.isNullOrEmpty()) return

            val address = messages[0].displayOriginatingAddress ?: ""
            val timestamp = messages[0].timestampMillis
            val bodyBuilder = StringBuilder()
            for (msg in messages) {
                bodyBuilder.append(msg.displayMessageBody ?: "")
            }
            val body = bodyBuilder.toString()

            SmsHandler.onSmsReceived(address, body, timestamp)
        } catch (_: Exception) {
            // Fail-soft: bad PDU or carrier edge never crashes process
        }
    }
}
