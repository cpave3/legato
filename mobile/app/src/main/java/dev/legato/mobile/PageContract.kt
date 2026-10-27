package dev.legato.mobile

import org.json.JSONObject
import java.net.URI

// What the app and the Legato web interface say to each other. The web side
// is web/src/lib/mobileShell.ts and web/src/hooks/useVoiceRecorder.ts; the
// page reaches the app through `window.ReactNativeWebView.postMessage`, the
// name the Expo app used, so servers built for it work unchanged.

// The name of the object the page posts to.
const val PAGE_BRIDGE ="ReactNativeWebView"

// bootstrapScript runs in the page before its own scripts. It signs the page
// in, and tells it the app can record for it (`window.legatoShell`) with a key
// the page must send back: frames an agent's HTML runs in can post to the app
// too, and must not start the microphone. Only the top frame gets either.
fun bootstrapScript(baseUrl: String, token: String, shellKey: String): String {
    val value = JSONObject.quote(token)
    return "if(window===window.top){" +
        "localStorage.setItem(\"legato:token:local\",$value);" +
        "localStorage.setItem(${JSONObject.quote("legato:token:$baseUrl")},$value);" +
        "window.legatoShell={voice:1,key:${JSONObject.quote(shellKey)}};" +
        "}"
}

// allowNavigation keeps the page on its own server; anything else is handed
// to the system.
fun allowNavigation(baseUrl: String, target: String): Boolean = origin(target)?.let { it == origin(baseUrl) } ?: false

private fun origin(url: String): String? = try {
    val uri = URI(url)
    val scheme = uri.scheme?.lowercase()
    val host = uri.host?.lowercase()
    if (scheme == null || host == null) null else "$scheme://$host:${if (uri.port != -1) uri.port else if (scheme == "https") 443 else 80}"
} catch (cause: Exception) {
    null
}

// Run in the page when the app returns to the foreground. Its value says
// whether the page still has something on screen (a loaded page whose root is
// empty has lost its interface), and the event tells the web app the person is
// back, so it can catch up on what it missed.
const val RETURN_SCRIPT =
    "(function(){var root=document.getElementById(\"root\");" +
        "var rendered=!root||root.childElementCount>0||document.readyState!==\"complete\";" +
        "try{window.dispatchEvent(new Event(\"legato:shell-return\"))}catch(e){}" +
        "return rendered})()"

// readPageAnswer reads the value RETURN_SCRIPT evaluated to. A page that
// cannot answer gives null.
fun readPageAnswer(result: String?): Boolean? = when (result) {
    "true" -> true
    "false" -> false
    else -> null
}

// voiceEventScript hands the page a `legato:shell-voice` event.
fun voiceEventScript(event: JSONObject): String =
    "(function(){try{window.dispatchEvent(new CustomEvent(\"legato:shell-voice\",{detail:$event}))}catch(e){}})()"

// What the page posts. Anything in the page can post, so nothing about a
// message is assumed.
sealed interface PageMessage {
    // Where the page wants to be brought back to if it has to be reopened.
    data class Restore(val url: String) : PageMessage

    // A request to start, stop or cancel a recording.
    data class Voice(val key: String, val action: String) : PageMessage

    data object Unknown : PageMessage

    companion object {
        fun parse(data: String): PageMessage {
            val message = try {
                JSONObject(data)
            } catch (cause: Exception) {
                return Unknown
            }
            return when (message.opt("legatoShell")) {
                "restore" -> (message.opt("url") as? String)?.let(::Restore) ?: Unknown
                "voice" -> {
                    val key = message.opt("key") as? String
                    val action = message.opt("action") as? String
                    if (key != null && action != null) Voice(key, action) else Unknown
                }
                else -> Unknown
            }
        }
    }
}
