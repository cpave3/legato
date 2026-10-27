package dev.legato.mobile

import android.annotation.SuppressLint
import android.content.Context
import android.graphics.Color
import android.net.Uri
import android.os.Handler
import android.os.Looper
import android.util.Log
import android.view.View
import android.view.ViewGroup
import android.webkit.RenderProcessGoneDetail
import android.webkit.ValueCallback
import android.webkit.WebChromeClient
import android.webkit.WebResourceError
import android.webkit.WebResourceRequest
import android.webkit.WebSettings
import android.webkit.WebView
import android.webkit.WebViewClient
import android.widget.FrameLayout
import androidx.webkit.WebViewCompat
import androidx.webkit.WebViewFeature
import java.util.UUID

private const val TAG = "LegatoPage"
private val PAGE_BACKGROUND = Color.parseColor("#0a0a0f")

// ServerPage is one server's page: the WebView that shows it, kept on screen
// by a PageKeeper, with the app recording for it through ShellVoice. The page
// outlives a trip back to the server list, so opening the same server again
// shows it at once.
class ServerPage(
    context: Context,
    val connection: Connection,
    private val token: String,
    microphone: ShellVoice.Microphone,
    private val callbacks: Callbacks,
) : PageKeeper.Host {
    interface Callbacks {
        fun canGoBackChanged(canGoBack: Boolean)
        fun unavailable(reason: String)
        fun openExternally(url: String)
        fun chooseFiles(params: WebChromeClient.FileChooserParams, callback: ValueCallback<Array<Uri>>)
    }

    private val appContext = context.applicationContext
    private val handler = Handler(Looper.getMainLooper())
    private val baseUrl = connection.baseUrl

    // Holds the current WebView; each recovery puts a new one in it.
    val view: FrameLayout = FrameLayout(context).apply { setBackgroundColor(PAGE_BACKGROUND) }
    private var webView: WebView? = null

    // The key the page sends with a request to record; only its top frame has it.
    private val shellKey = UUID.randomUUID().toString()
    private val keeper = PageKeeper(baseUrl, this)
    private val voice = ShellVoice(shellKey, microphone, tell = { evaluate(voiceEventScript(it.toJson())) }) { delay, action ->
        val tick = object : Runnable {
            override fun run() {
                action()
                handler.postDelayed(this, delay)
            }
        }
        handler.postDelayed(tick, delay)
        return@ShellVoice { handler.removeCallbacks(tick) }
    }

    private var inFront = true

    init {
        keeper.start()
    }

    // Whether the person can see the page: the app is in front and the page is
    // the screen showing. A page out of sight is judged when it comes back.
    fun setInFront(visible: Boolean) {
        if (visible == inFront) return
        inFront = visible
        if (visible) {
            keeper.foregrounded()
        } else {
            keeper.backgrounded()
            voice.release()
        }
    }

    fun reload() = webView?.reload()

    fun canGoBack(): Boolean = webView?.canGoBack() == true

    fun goBack() = webView?.goBack()

    fun destroy() {
        voice.release()
        handler.removeCallbacksAndMessages(null)
        webView?.let(::discard)
        webView = null
    }

    // PageKeeper.Host

    override fun open(url: String) {
        webView?.let(::discard)
        val page = createWebView()
        webView = page
        view.addView(page, ViewGroup.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.MATCH_PARENT))
        page.loadUrl(url)
    }

    override fun ask(answer: (Boolean?) -> Unit) {
        webView?.evaluateJavascript(RETURN_SCRIPT) { answer(readPageAnswer(it)) }
    }

    override fun after(delayMs: Long, action: () -> Unit): () -> Unit {
        val runnable = Runnable(action)
        handler.postDelayed(runnable, delayMs)
        return { handler.removeCallbacks(runnable) }
    }

    override fun unavailable(reason: String) = callbacks.unavailable(reason)

    private fun evaluate(script: String) {
        webView?.evaluateJavascript(script, null)
    }

    private fun discard(page: WebView) {
        view.removeView(page)
        page.stopLoading()
        page.destroy()
    }

    @SuppressLint("SetJavaScriptEnabled")
    private fun createWebView(): WebView {
        val page = WebView(view.context)
        page.setBackgroundColor(PAGE_BACKGROUND)
        page.overScrollMode = View.OVER_SCROLL_NEVER
        // Keep the renderer as important as the app while the app lives, so
        // the system does not reclaim it first when the app goes to the back.
        page.setRendererPriorityPolicy(WebView.RENDERER_PRIORITY_IMPORTANT, false)
        page.settings.apply {
            javaScriptEnabled = true
            domStorageEnabled = true
            mixedContentMode = WebSettings.MIXED_CONTENT_NEVER_ALLOW
            allowFileAccess = false
            allowContentAccess = false
            // A link that opens a window opens in this page, where
            // shouldOverrideUrlLoading decides where it belongs.
            setSupportMultipleWindows(false)
        }

        val bootstrap = bootstrapScript(baseUrl, token, shellKey)
        val origins = setOf(baseUrl)
        val startScripts = WebViewFeature.isFeatureSupported(WebViewFeature.DOCUMENT_START_SCRIPT)
        if (startScripts) WebViewCompat.addDocumentStartJavaScript(page, bootstrap, origins)
        if (WebViewFeature.isFeatureSupported(WebViewFeature.WEB_MESSAGE_LISTENER)) {
            // Only frames from the server itself get the bridge: an agent's
            // HTML runs in sandboxed frames with an origin of their own.
            WebViewCompat.addWebMessageListener(page, PAGE_BRIDGE, origins) { _, message, _, isMainFrame, _ ->
                if (isMainFrame) heard(message.data ?: return@addWebMessageListener)
            }
        } else {
            Log.w(TAG, "This WebView cannot take messages from the page; dictation and returning to a view are off")
        }

        page.webViewClient = object : WebViewClient() {
            override fun onPageStarted(view: WebView, url: String?, favicon: android.graphics.Bitmap?) {
                // Without document-start scripts this is the earliest moment.
                if (!startScripts && url != null && allowNavigation(baseUrl, url)) view.evaluateJavascript(bootstrap, null)
            }

            override fun shouldOverrideUrlLoading(view: WebView, request: WebResourceRequest): Boolean {
                if (!request.isForMainFrame) return false
                val url = request.url.toString()
                if (allowNavigation(baseUrl, url)) return false
                callbacks.openExternally(url)
                return true
            }

            override fun doUpdateVisitedHistory(view: WebView, url: String?, isReload: Boolean) {
                if (view !== webView) return
                url?.let(keeper::navigated)
                callbacks.canGoBackChanged(view.canGoBack())
            }

            override fun onReceivedError(view: WebView, request: WebResourceRequest, error: WebResourceError) {
                if (view === webView && request.isForMainFrame) keeper.loadFailed(error.description.toString())
            }

            override fun onRenderProcessGone(view: WebView, detail: RenderProcessGoneDetail): Boolean {
                Log.w(TAG, "The page's renderer is gone (crashed: ${detail.didCrash()})")
                val lost = view.url
                val current = view === webView
                discard(view)
                if (current) {
                    webView = null
                    keeper.rendererLost(lost)
                }
                return true
            }
        }
        page.webChromeClient = object : WebChromeClient() {
            override fun onShowFileChooser(view: WebView, callback: ValueCallback<Array<Uri>>, params: FileChooserParams): Boolean {
                callbacks.chooseFiles(params, callback)
                return true
            }
        }
        return page
    }

    private fun heard(data: String) {
        when (val message = PageMessage.parse(data)) {
            is PageMessage.Restore -> keeper.pageAsksToReturnTo(message.url)
            is PageMessage.Voice -> voice.handle(message)
            PageMessage.Unknown -> Unit
        }
    }
}
