package dev.legato.mobile

import org.json.JSONArray
import org.json.JSONObject

// A Legato server the person paired with.
data class Connection(
    val id: String,
    val name: String,
    val baseUrl: String,
    val createdAt: String,
    val serverVersion: String? = null,
    val caCertificate: String? = null,
)

// Where a connection's token is kept, apart from the list.
interface TokenVault {
    fun read(id: String): String?
    fun write(id: String, token: String)
    fun remove(id: String)
}

// ConnectionStore keeps the list of servers as the JSON array the Expo app
// kept, so the list it left behind can be taken over as it is.
class ConnectionStore(private val text: Text, private val vault: TokenVault) {
    // One stored string.
    interface Text {
        fun read(): String?
        fun write(value: String)
    }

    fun load(): List<Connection> {
        val array = try {
            JSONArray(text.read() ?: return emptyList())
        } catch (cause: Exception) {
            return emptyList()
        }
        return (0 until array.length()).mapNotNull { index -> (array.opt(index) as? JSONObject)?.let(::readConnection) }
    }

    fun token(id: String): String? = vault.read(id)

    // Saves a newly paired server. Pairing again with a server already in the
    // list replaces that entry, keeping its place in the list of tokens.
    fun save(connection: Connection, token: String): Connection {
        val current = load()
        val existing = current.find { it.baseUrl == connection.baseUrl }
        val saved = if (existing != null) connection.copy(id = existing.id, createdAt = existing.createdAt) else connection
        vault.write(saved.id, token)
        write(current.filter { it.id != saved.id } + saved)
        return saved
    }

    fun update(id: String, name: String, baseUrl: String, token: String?) {
        val current = load()
        if (current.any { it.id != id && it.baseUrl == baseUrl }) {
            throw IllegalArgumentException("Another connection already uses this server URL")
        }
        if (!token.isNullOrBlank()) vault.write(id, token.trim())
        write(current.map { if (it.id == id) it.copy(name = name, baseUrl = baseUrl) else it })
    }

    fun rememberVersion(id: String, version: String) {
        val current = load()
        if (current.none { it.id == id && it.serverVersion != version }) return
        write(current.map { if (it.id == id) it.copy(serverVersion = version) else it })
    }

    fun remove(id: String) {
        vault.remove(id)
        write(load().filter { it.id != id })
    }

    // Takes over the list the Expo app saved, the first time this app runs.
    fun adoptLegacy(legacy: () -> String?) {
        if (text.read() != null) return
        val saved = legacy() ?: "[]"
        text.write(saved)
        // Keep only what this app can read.
        write(load())
    }

    private fun write(connections: List<Connection>) {
        text.write(JSONArray(connections.map(::writeConnection)).toString())
    }
}

private fun readConnection(json: JSONObject): Connection? {
    val id = json.opt("id") as? String ?: return null
    val baseUrl = json.opt("baseUrl") as? String ?: return null
    return Connection(
        id = id,
        name = json.opt("name") as? String ?: baseUrl,
        baseUrl = baseUrl,
        createdAt = json.opt("createdAt") as? String ?: "",
        serverVersion = json.opt("serverVersion") as? String,
        caCertificate = json.opt("caCertificate") as? String,
    )
}

private fun writeConnection(connection: Connection): JSONObject = JSONObject()
    .put("id", connection.id)
    .put("name", connection.name)
    .put("baseUrl", connection.baseUrl)
    .put("createdAt", connection.createdAt)
    .apply {
        connection.serverVersion?.let { put("serverVersion", it) }
        connection.caCertificate?.let { put("caCertificate", it) }
    }
