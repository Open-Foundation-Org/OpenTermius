package com.opentermius.client

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.Service
import android.content.Context
import android.content.Intent
import android.content.pm.ServiceInfo
import android.os.Build
import android.os.IBinder
import android.os.PowerManager
import androidx.core.app.NotificationCompat

class SshKeepAliveService : Service() {
  private var wakeLock: PowerManager.WakeLock? = null

  companion object {
    const val CHANNEL_ID = "opentermius_ssh_keepalive"
    const val NOTIFICATION_ID = 1001
    const val EXTRA_SESSION_COUNT = "session_count"
  }

  override fun onCreate() {
    super.onCreate()
    createNotificationChannel()
    val pm = getSystemService(Context.POWER_SERVICE) as? PowerManager
    wakeLock = pm?.newWakeLock(PowerManager.PARTIAL_WAKE_LOCK, "OpenTermius::SshKeepAlive")?.apply {
      setReferenceCounted(false)
    }
  }

  override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
    if (intent == null) {
      releaseAndStop()
      return START_NOT_STICKY
    }
    val count = intent.getIntExtra(EXTRA_SESSION_COUNT, 0)
    if (count <= 0) {
      releaseAndStop()
      return START_NOT_STICKY
    }

    if (wakeLock?.isHeld == false) {
      wakeLock?.acquire(12 * 60 * 60 * 1000L) // Up to 12h while sessions are open
    }

    val notification = buildNotification(count)
    if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
      startForeground(NOTIFICATION_ID, notification, ServiceInfo.FOREGROUND_SERVICE_TYPE_DATA_SYNC)
    } else {
      startForeground(NOTIFICATION_ID, notification)
    }
    return START_NOT_STICKY
  }

  override fun onTimeout(startId: Int, fgsType: Int) {
    releaseAndStop()
  }

  private fun buildNotification(count: Int): Notification {
    val launchIntent = Intent(this, MainActivity::class.java).apply {
      flags = Intent.FLAG_ACTIVITY_SINGLE_TOP or Intent.FLAG_ACTIVITY_CLEAR_TOP
    }
    val pendingIntent = PendingIntent.getActivity(
      this,
      0,
      launchIntent,
      PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE
    )

    val subtitle = if (count == 1) {
      "1 active terminal session running in background"
    } else {
      "$count active terminal sessions running in background"
    }

    return NotificationCompat.Builder(this, CHANNEL_ID)
      .setContentTitle("OpenTermius SSH Active")
      .setContentText(subtitle)
      .setSmallIcon(R.drawable.ic_stat_terminal)
      .setOngoing(true)
      .setSilent(true)
      .setPriority(NotificationCompat.PRIORITY_LOW)
      .setContentIntent(pendingIntent)
      .build()
  }

  private fun createNotificationChannel() {
    if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
      val channel = NotificationChannel(
        CHANNEL_ID,
        "Active SSH Sessions",
        NotificationManager.IMPORTANCE_LOW
      ).apply {
        description = "Keeps SSH connections alive when app is minimized or screen is locked"
        setShowBadge(false)
      }
      val nm = getSystemService(Context.NOTIFICATION_SERVICE) as? NotificationManager
      nm?.createNotificationChannel(channel)
    }
  }

  private fun releaseAndStop() {
    if (wakeLock?.isHeld == true) {
      wakeLock?.release()
    }
    if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.N) {
      stopForeground(STOP_FOREGROUND_REMOVE)
    } else {
      @Suppress("DEPRECATION")
      stopForeground(true)
    }
    stopSelf()
  }

  override fun onDestroy() {
    if (wakeLock?.isHeld == true) {
      wakeLock?.release()
    }
    super.onDestroy()
  }

  override fun onBind(intent: Intent?): IBinder? = null
}
