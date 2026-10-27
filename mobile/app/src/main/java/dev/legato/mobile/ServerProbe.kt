package dev.legato.mobile

import org.json.JSONObject
import java.net.HttpURLConnection
import java.net.URL

data class Health(val online: Boolean, val version: String?)

enum class Compatibility { LEGACY, COMPATIBLE, UNSUPPORTED }

data class ServerMeta(val compatibility: Compatibility, val version: String?)

// readHealth reads the answer to `GET /health`, which needs no token.
fun readHealth(status: Int, body: String?): Health {
    if (status !in 200..299) return Health(false, null)
    val json = try {
        JSONObject(body ?: "")
    } catch (cause: Exception) {
        return Health(false, null)
    }
    val state = json.opt("status")
    val version = (json.opt("version") as? String)?.takeIf { it.isNotEmpty() }
    return Health(online = state == null || state == "ok", version = version)
}

// readMeta reads the answer to `GET /api/meta`: whether this app speaks the
// server's version of the shell contract.
fun readMeta(status: Int, body: String?): ServerMeta {
    if (status == 404) return ServerMeta(Compatibility.LEGACY, null)
    check(status in 200..299) { "Server returned $status" }
    val json = try {
        JSONObject(body ?: "")
    } catch (cause: Exception) {
        throw IllegalStateException("The server's answer could not be read")
    }
    val version = json.opt("version") as? String
    val compatibility = if (json.opt("mobile_shell_api") == 1) Compatibility.COMPATIBLE else Compatibility.UNSUPPORTED
    return ServerMeta(compatibility, version)
}

// Blocking; call off the main thread.
fun probeHealth(baseUrl: String, timeoutMs: Int = 3_000): Health = try {
    get("$baseUrl/health", timeoutMs).let { (status, body) -> readHealth(status, body) }
} catch (cause: Exception) {
    Health(false, null)
}

// Blocking; call off the main thread.
fun probeServer(baseUrl: String, timeoutMs: Int = 5_000): ServerMeta =
    get("$baseUrl/api/meta", timeoutMs).let { (status, body) -> readMeta(status, body) }

private fun get(url: String, timeoutMs: Int): Pair<Int, String?> {
    val connection = URL(url).openConnection() as HttpURLConnection
    try {
        connection.connectTimeout = timeoutMs
        connection.readTimeout = timeoutMs
        connection.useCaches = false
        val status = connection.responseCode
        val stream = if (status in 200..299) connection.inputStream else connection.errorStream
        return status to stream?.use { it.readBytes().toString(Charsets.UTF_8) }
    } finally {
        connection.disconnect()
    }
}
