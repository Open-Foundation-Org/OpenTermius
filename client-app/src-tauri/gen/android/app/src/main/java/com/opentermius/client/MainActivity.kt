package com.opentermius.client

import android.Manifest
import android.app.Activity
import android.content.ClipData
import android.content.ClipboardManager
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.os.Build
import android.os.Bundle
import android.view.View
import android.view.WindowManager
import android.webkit.JavascriptInterface
import android.webkit.WebView
import androidx.activity.OnBackPressedCallback
import androidx.activity.enableEdgeToEdge
import androidx.core.app.ActivityCompat
import androidx.core.content.ContextCompat
import androidx.core.view.ViewCompat
import androidx.core.view.WindowInsetsCompat

class AndroidClipboardBridge(private val activity: Activity) {
  @JavascriptInterface
  fun readText(): String {
    val cm = activity.getSystemService(Context.CLIPBOARD_SERVICE) as? ClipboardManager ?: return ""
    val clip = cm.primaryClip ?: return ""
    if (clip.itemCount == 0) return ""
    return clip.getItemAt(0).coerceToText(activity)?.toString() ?: ""
  }

  @JavascriptInterface
  fun writeText(text: String) {
    val cm = activity.getSystemService(Context.CLIPBOARD_SERVICE) as? ClipboardManager ?: return
    cm.setPrimaryClip(ClipData.newPlainText("OpenTermius", text))
  }

  @JavascriptInterface
  fun moveToBackground() {
    activity.runOnUiThread {
      activity.moveTaskToBack(true)
    }
  }

  @JavascriptInterface
  fun setKeepAliveSessions(count: Int) {
    activity.runOnUiThread {
      if (count > 0 && Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
        val granted = ContextCompat.checkSelfPermission(activity, Manifest.permission.POST_NOTIFICATIONS) == PackageManager.PERMISSION_GRANTED
        if (!granted) {
          ActivityCompat.requestPermissions(activity, arrayOf(Manifest.permission.POST_NOTIFICATIONS), 101)
        }
      }

      val intent = Intent(activity, SshKeepAliveService::class.java).apply {
        putExtra(SshKeepAliveService.EXTRA_SESSION_COUNT, count)
      }
      if (count > 0) {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
          activity.startForegroundService(intent)
        } else {
          activity.startService(intent)
        }
      } else {
        activity.stopService(intent)
      }
    }
  }
}

class MainActivity : TauriActivity() {
  override val handleBackNavigation: Boolean = false
  private var webViewRef: WebView? = null

  private fun setupInsets() {
    val contentView = findViewById<View>(android.R.id.content) ?: return
    ViewCompat.setOnApplyWindowInsetsListener(contentView) { view, insets ->
      val systemBars = insets.getInsets(WindowInsetsCompat.Type.systemBars())
      val displayCutout = insets.getInsets(WindowInsetsCompat.Type.displayCutout())
      val ime = insets.getInsets(WindowInsetsCompat.Type.ime())

      val topInset = maxOf(systemBars.top, displayCutout.top)
      val bottomInset = maxOf(systemBars.bottom, displayCutout.bottom, ime.bottom)
      val leftInset = maxOf(systemBars.left, displayCutout.left)
      val rightInset = maxOf(systemBars.right, displayCutout.right)

      view.setPadding(leftInset, topInset, rightInset, bottomInset)
      WindowInsetsCompat.CONSUMED
    }
    ViewCompat.requestApplyInsets(contentView)
  }

  override fun onCreate(savedInstanceState: Bundle?) {
    enableEdgeToEdge()
    super.onCreate(savedInstanceState)

    @Suppress("DEPRECATION")
    window.setSoftInputMode(WindowManager.LayoutParams.SOFT_INPUT_ADJUST_RESIZE)
    setupInsets()

    onBackPressedDispatcher.addCallback(this, object : OnBackPressedCallback(true) {
      override fun handleOnBackPressed() {
        val wv = webViewRef
        if (wv != null) {
          wv.post {
            wv.evaluateJavascript("window.dispatchEvent(new CustomEvent('opentermius:android-back'))", null)
          }
        } else {
          moveTaskToBack(true)
        }
      }
    })
  }

  override fun onWebViewCreate(webView: WebView) {
    super.onWebViewCreate(webView)
    webViewRef = webView
    webView.addJavascriptInterface(AndroidClipboardBridge(this), "AndroidClipboard")
    webView.post {
      setupInsets()
    }
  }
}
