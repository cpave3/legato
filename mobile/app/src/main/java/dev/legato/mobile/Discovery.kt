package dev.legato.mobile

/** A Legato server announced on the local network (see internal/engine/discovery). */
data class DiscoveredServer(val name: String, val baseUrl: String)

const val LEGATO_SERVICE_TYPE = "_legato._tcp"

/** The address of a server announced with [scheme] (its TXT record) at [host]:[port]. */
fun discoveredBaseUrl(scheme: String?, host: String, port: Int): String {
    val s = if (scheme == "https") "https" else "http"
    val bare = host.substringBefore('%')
    val h = if (':' in bare) "[$bare]" else bare
    return normalizeServerUrl("$s://$h:$port")
}
