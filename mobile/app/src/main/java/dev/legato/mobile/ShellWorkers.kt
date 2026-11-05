package dev.legato.mobile

import org.json.JSONArray
import org.json.JSONObject

// Adding a worker from a server this app already knows. A main's Settings ›
// Handoff › Workers › Add a worker asks the app for its other servers, and
// for one of them, a main's pairing request opened with the token only the
// app holds. The page never sees another server's token: it gets the address
// and phrase, which it gives its own server as the manual form does, and the
// two servers pair between themselves. The web side is web/src/lib/
// mobileShell.ts; the desktop app's bridge.rs speaks the same contract.

// The main's pairing request opened on a server: what the page passes to its
// own server's Add a worker. phrase and expiresAt are as the server sent them.
class WorkerPairing(val address: String, val phrase: JSONArray, val expiresAt: Any?) {
    fun detail(id: Any): JSONObject = JSONObject()
        .put("id", id)
        .put("address", address)
        .put("phrase", phrase)
        .apply { expiresAt?.let { put("expires_at", it) } }
}

// WorkerPairingClient opens a main's pairing request on one server, with its
// token: what the server's own "Pair a main instance" does. address is the
// app's address for the server, which the main is to reach. Blocking.
class WorkerPairingClient(private val http: ServerHttp, private val address: String) {
    fun open(): WorkerPairing {
        val (status, text) = http.call("POST", "/api/pairing", JSONObject().put("purpose", "main"))
        val json = runCatching { JSONObject(text) }.getOrNull()
        if (status !in 200..299) {
            throw ServerError(json?.optString("error")?.takeIf { it.isNotBlank() } ?: "The server answered $status.")
        }
        if (json == null) throw ServerError("The server's answer does not read.")
        // A server from before handoff ignores the purpose and opens a
        // phone's request, which no main can use: take it back.
        if (json.opt("purpose") != "main") {
            runCatching { http.call("DELETE", "/api/pairing", null) }
            throw ServerError("This server is too old to be a worker. Update Legato on it, then try again.")
        }
        val phrase = json.opt("phrase") as? JSONArray
        if (phrase == null || phrase.length() == 0) throw ServerError("The server opened no pairing request.")
        return WorkerPairing(address, phrase, json.opt("expires_at"))
    }
}

// What the person is asked, in the app, before a server becomes a worker.
fun workerConfirmText(server: String, main: String) =
    "Make $server a worker of $main? $main will be able to hand tasks to $server, which runs their agents there."

private const val MAIN_NAME_LIMIT = 80

// ShellWorkers answers one page's worker requests. Only a request carrying
// the page's key is obeyed. confirm asks the person in the app, outside the
// page; openRequest runs WorkerPairingClient off the main thread and calls
// done back on it; tell answers the page.
class ShellWorkers(
    private val key: String,
    private val page: Connection,
    private val servers: () -> List<Connection>,
    private val confirm: (message: String, answer: (Boolean) -> Unit) -> Unit,
    private val openRequest: (server: Connection, done: (Result<WorkerPairing>) -> Unit) -> Unit,
    private val tell: (JSONObject) -> Unit,
) {
    private fun candidates(): List<Connection> = servers().filter { it.id != page.id }

    fun handle(message: PageMessage.WorkerCandidates) {
        if (message.key != key) return
        val list = JSONArray(candidates().map { JSONObject().put("key", it.id).put("name", it.name).put("url", it.baseUrl) })
        tell(JSONObject().put("id", message.id).put("servers", list))
    }

    fun handle(message: PageMessage.OpenWorkerPairing) {
        if (message.key != key) return
        val id = message.id
        val server = candidates().firstOrNull { it.id == message.server }
        if (server == null) {
            tell(JSONObject().put("id", id).put("error", "This app has no such server."))
            return
        }
        val main = message.main.trim().take(MAIN_NAME_LIMIT).ifEmpty { page.name }
        confirm(workerConfirmText(server.name, main)) { yes ->
            if (!yes) {
                tell(JSONObject().put("id", id).put("cancelled", true))
                return@confirm
            }
            openRequest(server) { result ->
                tell(result.fold({ it.detail(id) }, { JSONObject().put("id", id).put("error", it.message ?: it.toString()) }))
            }
        }
    }
}
