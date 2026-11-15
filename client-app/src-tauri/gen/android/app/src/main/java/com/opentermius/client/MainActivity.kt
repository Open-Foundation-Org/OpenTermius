package com.opentermius.client

import android.os.Bundle
import android.view.View
import android.view.WindowManager
import android.webkit.WebView
import androidx.activity.enableEdgeToEdge
import androidx.core.view.ViewCompat
import androidx.core.view.WindowInsetsCompat

class MainActivity : TauriActivity() {
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
  }

  override fun onWebViewCreate(webView: WebView) {
    super.onWebViewCreate(webView)
    webView.post {
      setupInsets()
    }
  }
}
