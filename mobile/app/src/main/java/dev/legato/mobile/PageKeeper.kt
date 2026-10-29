package dev.legato.mobile

import java.net.URI

// How long a page gets to answer after the app returns to the foreground.
const val ANSWER_TIMEOUT_MS = 4_000L

// A page lost again this soon after being reopened, with the app in front the
// whole time, would only keep being lost.
const val LOST_AGAIN_MS = 30_000L

// PageKeeper keeps the open server's page on screen. Android reclaims a
// backgrounded WebView's renderer while the app itself lives on, which leaves
// a blank view; the keeper notices, on the system's report or by asking the
// page when the app returns, and reopens the page where the person was.
class PageKeeper(
    private val baseUrl: String,
    private val host: Host,
    private val now: () -> Long = System::currentTimeMillis,
) {
    // What the keeper needs from the screen showing the page.
    interface Host {
        // Shows a fresh page at the address: a WebView whose renderer is gone
        // cannot be reused.
        fun open(url: String)

        // Asks the page whether it still has something on screen, and tells it
        // the person is back. A page that is gone may never answer.
        fun ask(answer: (Boolean?) -> Unit)

        // Runs the action later; the returned function cancels it.
        fun after(delayMs: Long, action: () -> Unit): () -> Unit

        // The page cannot be shown, and why.
        fun unavailable(reason: String)
    }

    private var lastUrl = baseUrl

    // Where the page asked to be brought back to: what it shows can depend on
    // more than its address, and it is the one that knows.
    private var returnTo: String? = null
    private var reopenedAt = 0L
    private var cancelWait: (() -> Unit)? = null
    private var question = 0
    private var backgrounded = false
    private var lostInBackground: String? = null

    fun start(url: String = baseUrl) {
        lastUrl = url
        host.open(url)
    }

    fun navigated(url: String) {
        lastUrl = url
    }

    fun pageAsksToReturnTo(url: String) {
        returnTo = url
    }

    fun loadFailed(description: String) = host.unavailable(description)

    fun rendererLost(url: String?) {
        val lost = url ?: lastUrl
        when {
            // A page opened while nobody is looking would only be reclaimed
            // again, so one lost in the background waits for the person.
            backgrounded -> lostInBackground = lost
            now() - reopenedAt < LOST_AGAIN_MS -> {
                stopWaiting()
                host.unavailable("This server's page keeps stopping on this device. Open it again to retry.")
            }
            else -> reopen(lost)
        }
    }

    fun backgrounded() {
        // A page cannot be expected to answer in the background; it is asked
        // again on the next return.
        backgrounded = true
        reopenedAt = 0
        stopWaiting()
    }

    fun foregrounded() {
        if (!backgrounded) return
        backgrounded = false
        val lost = lostInBackground
        lostInBackground = null
        if (lost != null) {
            reopen(lost)
            return
        }
        // Not every lost page is reported, so ask this one whether it is still there.
        val asked = ++question
        cancelWait = host.after(ANSWER_TIMEOUT_MS) {
            if (asked == question) {
                cancelWait = null
                reopen(lastUrl)
            }
        }
        host.ask { rendered ->
            if (asked != question || rendered == null) return@ask
            stopWaiting()
            if (!rendered) reopen(lastUrl)
        }
    }

    private fun stopWaiting() {
        cancelWait?.invoke()
        cancelWait = null
        question++
    }

    private fun reopen(url: String) {
        stopWaiting()
        reopenedAt = now()
        val target = returnTo?.takeIf { samePage(it, url) } ?: url
        lastUrl = if (allowNavigation(baseUrl, target)) target else baseUrl
        host.open(lastUrl)
    }
}

private fun samePage(one: String, other: String): Boolean = try {
    val a = URI(one)
    val b = URI(other)
    allowNavigation(one, other) && (a.rawPath ?: "") == (b.rawPath ?: "")
} catch (cause: Exception) {
    false
}
