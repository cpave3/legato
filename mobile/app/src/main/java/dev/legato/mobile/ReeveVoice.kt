package dev.legato.mobile

import org.json.JSONObject

// Which saved server's Reeve the phone talks to. Every server says who it is
// at GET /api/reeve/voice (with its web token); the one whose role is "main"
// is the main. A worker's page sends the person there (PageMessage.OpenVoice).

// Who a server is, from its voice status.
data class VoiceInstance(val id: String, val name: String, val role: String)

// The fragment that opens the voice sheet on the main's page.
const val VOICE_PATH = "/#reeve-voice"

// fetchVoiceInstance asks one server who it is. Blocking.
fun fetchVoiceInstance(http: ServerHttp): VoiceInstance {
    val (status, text) = http.call("GET", "/api/reeve/voice", null)
    val json = runCatching { JSONObject(text) }.getOrNull()
    if (status !in 200..299) throw ServerError(json?.optString("error")?.takeIf { it.isNotBlank() } ?: "The server answered $status.")
    val instance = json?.optJSONObject("instance") ?: throw ServerError("The server does not say who it is.")
    val id = instance.opt("id") as? String
    val role = instance.opt("role") as? String
    if (id.isNullOrEmpty() || role.isNullOrEmpty()) throw ServerError("The server does not say who it is.")
    return VoiceInstance(id, instance.opt("name") as? String ?: "", role)
}

// learnVoiceRoles asks every server who it is. A server that does not answer
// is left out, so what was known of it is kept. Blocking.
fun learnVoiceRoles(connections: List<Connection>, ask: (Connection) -> VoiceInstance): Map<String, VoiceInstance> =
    connections.mapNotNull { connection -> runCatching { connection.id to ask(connection) }.getOrNull() }.toMap()

// voiceMain picks the server whose Reeve to talk to: the one server that says
// it is the main; else the only server saved; else none, and the person will
// choose.
fun voiceMain(connections: List<Connection>): Connection? {
    val mains = connections.filter { it.role == "main" }
    return when {
        mains.size == 1 -> mains.single()
        mains.isEmpty() && connections.size == 1 -> connections.single()
        else -> null
    }
}
