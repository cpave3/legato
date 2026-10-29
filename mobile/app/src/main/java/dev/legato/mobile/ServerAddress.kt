package dev.legato.mobile

import java.net.URI
import java.net.URISyntaxException

// normalizeServerUrl reduces what the person typed to the server's origin
// (`http://192.168.1.20:3080`), the form every saved connection uses.
fun normalizeServerUrl(value: String): String {
    val url = try {
        URI(value.trim())
    } catch (cause: URISyntaxException) {
        throw IllegalArgumentException("Not a server URL: ${value.trim()}")
    }
    val scheme = url.scheme?.lowercase()
    if (scheme != "http" && scheme != "https" || url.rawUserInfo != null || url.rawQuery != null || url.rawFragment != null) {
        throw IllegalArgumentException("Use an HTTP or HTTPS server URL without credentials, query, or fragment")
    }
    val host = url.host ?: throw IllegalArgumentException("The server URL has no host")
    if (!url.rawPath.isNullOrEmpty() && url.rawPath != "/") throw IllegalArgumentException("Server URL must not contain a path")
    val defaultPort = if (scheme == "https") 443 else 80
    val port = if (url.port == -1 || url.port == defaultPort) "" else ":${url.port}"
    return "$scheme://${host.lowercase()}$port"
}

// What `legato pair` puts in its QR code:
// legato://pair?url=<server>&token=<token>[&ca=<PEM certificate>]
data class PairingCode(
    val baseUrl: String,
    val token: String,
    val suggestedName: String,
    val caCertificate: String?,
)

private val pemCertificate = Regex("^-----BEGIN CERTIFICATE-----[\\s\\S]+-----END CERTIFICATE-----\\s*$")

fun parsePairUri(value: String): PairingCode {
    val uri = try {
        URI(value.trim())
    } catch (cause: URISyntaxException) {
        throw IllegalArgumentException("Not a Legato pairing URI")
    }
    if (uri.scheme != "legato" || uri.host != "pair") throw IllegalArgumentException("Not a Legato pairing URI")
    val params = queryParameters(uri.rawQuery.orEmpty())
    val urls = params["url"].orEmpty()
    val tokens = params["token"].orEmpty()
    if (urls.size != 1 || tokens.size != 1) throw IllegalArgumentException("Pairing URI must contain one URL and token")
    val baseUrl = normalizeServerUrl(urls.single())
    val token = tokens.single().trim()
    if (token.isEmpty()) throw IllegalArgumentException("Pairing token is missing")
    val ca = params["ca"]?.firstOrNull()
    if (ca != null && !pemCertificate.matches(ca)) throw IllegalArgumentException("Pairing certificate is invalid")
    return PairingCode(baseUrl, token, URI(baseUrl).host, ca)
}

/** A QR code of a pairing request: the server's address and its phrase. */
data class PhraseCode(val baseUrl: String, val words: List<String>)

/**
 * Reads `legato://pair?url=…&phrase=word-word-word-word`, which the web UI's
 * Pair a device and `legato pair` show. Null for any other link, such as a
 * code that carries a token.
 */
fun parsePhraseUri(value: String): PhraseCode? {
    val uri = try {
        URI(value.trim())
    } catch (cause: URISyntaxException) {
        return null
    }
    if (uri.scheme != "legato" || uri.host != "pair") return null
    val params = queryParameters(uri.rawQuery.orEmpty())
    val phrase = params["phrase"]?.singleOrNull() ?: return null
    val url = params["url"]?.singleOrNull() ?: throw IllegalArgumentException("Pairing code has no server address")
    val reading = PhraseInput.read(phrase)
    if (!reading.complete) throw IllegalArgumentException("Pairing code's phrase is not ${PhraseInput.WORDS} words")
    return PhraseCode(normalizeServerUrl(url), reading.words.filterNotNull())
}

// Decodes a query the way a browser's URLSearchParams does, `+` as a space.
private fun queryParameters(query: String): Map<String, List<String>> =
    query.split('&').filter { it.isNotEmpty() }.map { part ->
        val name = part.substringBefore('=')
        val value = if ('=' in part) part.substringAfter('=') else ""
        decode(name) to decode(value)
    }.groupBy({ it.first }, { it.second })

private fun decode(value: String): String = java.net.URLDecoder.decode(value, "UTF-8")

/** What a notification of a new approval opens: a server, and the approval. */
data class ApprovalLink(val baseUrl: String, val id: String) {
    /** Where on the server's page the approval is shown. */
    val path: String get() = "/?approval=$id"
}

private val approvalId = Regex("^ap-[0-9a-f]{8}$|^[A-Za-z0-9-]{1,64}$")

/**
 * Reads `legato://approval?url=…&id=…`, the link of an approval notification.
 * Null for anything else.
 */
fun parseApprovalUri(value: String): ApprovalLink? {
    val uri = try {
        URI(value.trim())
    } catch (cause: URISyntaxException) {
        return null
    }
    if (uri.scheme != "legato" || uri.host != "approval") return null
    val params = queryParameters(uri.rawQuery.orEmpty())
    val url = params["url"]?.singleOrNull() ?: return null
    val id = params["id"]?.singleOrNull() ?: return null
    if (!approvalId.matches(id)) return null
    val baseUrl = try {
        normalizeServerUrl(url)
    } catch (cause: IllegalArgumentException) {
        return null
    }
    return ApprovalLink(baseUrl, id)
}

/**
 * The saved server an approval link is for: the one at its address, or the
 * only server saved. Null when it cannot be told.
 */
fun serverForApproval(connections: List<Connection>, link: ApprovalLink): Connection? =
    connections.firstOrNull { it.baseUrl == link.baseUrl } ?: connections.singleOrNull()
