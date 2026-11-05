package dev.legato.mobile

import org.json.JSONArray
import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test

private fun words(array: JSONArray) = (0 until array.length()).map { array.getString(it) }

private val studio = Connection(id = "c-1", name = "studio", baseUrl = "https://studio.local:3443", createdAt = "")
private val box = Connection(id = "c-2", name = "legato-1", baseUrl = "http://10.0.0.9:3080", createdAt = "", caCertificate = "PEM")
private val laptop = Connection(id = "c-3", name = "laptop", baseUrl = "http://10.0.0.4:3080", createdAt = "")

class ShellWorkersTest {
    private val told = mutableListOf<JSONObject>()
    private val asked = mutableListOf<String>()
    private var answer: ((Boolean) -> Unit)? = null
    private val opened = mutableListOf<Connection>()
    private var outcome: Result<WorkerPairing> = Result.success(WorkerPairing("http://10.0.0.9:3080", JSONArray(listOf("acorn", "bagel", "cider", "dune")), "2026-11-05T08:00:00Z"))
    private lateinit var workers: ShellWorkers

    @Before
    fun studioPageIsOpen() {
        workers = ShellWorkers(
            key = "page-key",
            page = studio,
            servers = { listOf(studio, box, laptop) },
            confirm = { message, answer ->
                asked += message
                this.answer = answer
            },
            openRequest = { server, done ->
                opened += server
                done(outcome)
            },
            tell = { told += it },
        )
    }

    @Test
    fun `lists every server the app knows but the page's own`() {
        workers.handle(PageMessage.WorkerCandidates("page-key", "r1"))

        val reply = told.single()
        assertEquals("r1", reply.get("id"))
        val servers = reply.getJSONArray("servers")
        assertEquals(2, servers.length())
        val first = servers.getJSONObject(0)
        assertEquals("c-2", first.getString("key"))
        assertEquals("legato-1", first.getString("name"))
        assertEquals("http://10.0.0.9:3080", first.getString("url"))
        assertEquals("c-3", servers.getJSONObject(1).getString("key"))
        // The page never sees another server's token or certificate.
        assertFalse(reply.toString().contains("PEM"))
    }

    @Test
    fun `answers with the page's own kind of ID`() {
        workers.handle(PageMessage.WorkerCandidates("page-key", 7))

        assertEquals(7, told.single().get("id"))
    }

    @Test
    fun `asks the person natively before pairing, in the contract's words`() {
        workers.handle(PageMessage.OpenWorkerPairing("page-key", "r2", server = "c-2", main = "studio main"))

        assertEquals(
            listOf("Make legato-1 a worker of studio main? studio main will be able to hand tasks to legato-1, which runs their agents there."),
            asked,
        )
        assertEquals(emptyList<Connection>(), opened)
        assertEquals(emptyList<JSONObject>(), told)
    }

    @Test
    fun `on yes opens the main's request on the server and hands the page its address and phrase`() {
        workers.handle(PageMessage.OpenWorkerPairing("page-key", "r2", server = "c-2", main = "studio"))
        answer!!(true)

        assertEquals(listOf(box), opened)
        val reply = told.single()
        assertEquals("r2", reply.get("id"))
        assertEquals("http://10.0.0.9:3080", reply.getString("address"))
        assertEquals(listOf("acorn", "bagel", "cider", "dune"), words(reply.getJSONArray("phrase")))
        assertEquals("2026-11-05T08:00:00Z", reply.getString("expires_at"))
    }

    @Test
    fun `on no tells the page it was cancelled`() {
        workers.handle(PageMessage.OpenWorkerPairing("page-key", "r2", server = "c-2", main = "studio"))
        answer!!(false)

        assertEquals(emptyList<Connection>(), opened)
        val reply = told.single()
        assertEquals(setOf("id", "cancelled"), reply.keys().asSequence().toSet())
        assertEquals("r2", reply.get("id"))
        assertTrue(reply.getBoolean("cancelled"))
    }

    @Test
    fun `passes on what went wrong, in the server's words`() {
        outcome = Result.failure(ServerError("this server does not pair by phrase"))
        workers.handle(PageMessage.OpenWorkerPairing("page-key", "r2", server = "c-2", main = "studio"))
        answer!!(true)

        val reply = told.single()
        assertEquals("r2", reply.get("id"))
        assertEquals("this server does not pair by phrase", reply.getString("error"))
    }

    @Test
    fun `will not pair the page's own server or one it does not know`() {
        workers.handle(PageMessage.OpenWorkerPairing("page-key", "r3", server = "c-1", main = "studio"))
        workers.handle(PageMessage.OpenWorkerPairing("page-key", "r4", server = "nope", main = "studio"))

        assertEquals(emptyList<String>(), asked)
        assertEquals(listOf("r3", "r4"), told.map { it.get("id") })
        assertTrue(told.all { it.has("error") })
    }

    @Test
    fun `names the page's server when the page names no main`() {
        workers.handle(PageMessage.OpenWorkerPairing("page-key", "r2", server = "c-2", main = "  "))

        assertTrue(asked.single().startsWith("Make legato-1 a worker of studio? studio will"))
    }

    @Test
    fun `obeys only the page's own key`() {
        workers.handle(PageMessage.WorkerCandidates("guessed", "r1"))
        workers.handle(PageMessage.OpenWorkerPairing("guessed", "r2", server = "c-2", main = "studio"))

        assertEquals(emptyList<JSONObject>(), told)
        assertEquals(emptyList<String>(), asked)
    }
}

// A server that answers from a script, recording what it was asked.
private class ScriptedServer(val answers: Map<String, Pair<Int, String>>) : ServerHttp {
    val calls = mutableListOf<Triple<String, String, JSONObject?>>()
    override fun call(method: String, path: String, body: JSONObject?): Pair<Int, String> {
        calls += Triple(method, path, body)
        return answers["$method $path"] ?: (404 to "404 page not found")
    }
}

class WorkerPairingClientTest {
    @Test
    fun `opens a main's pairing request and keeps the phrase as the server gave it`() {
        val server = ScriptedServer(mapOf("POST /api/pairing" to (200 to """{"state":"open","purpose":"main","phrase":["acorn","bagel","cider","dune"],"expires_at":"2026-11-05T08:00:00.123456789Z","attempts_left":5,"address":"http://192.168.1.9:3080"}""")))

        val pairing = WorkerPairingClient(server, "http://10.0.0.9:3080").open()

        val (method, path, body) = server.calls.single()
        assertEquals("POST", method)
        assertEquals("/api/pairing", path)
        assertEquals("main", body!!.getString("purpose"))
        // The address is the app's for the server, which the main is to reach.
        assertEquals("http://10.0.0.9:3080", pairing.address)
        assertEquals(listOf("acorn", "bagel", "cider", "dune"), words(pairing.phrase))
        assertEquals("2026-11-05T08:00:00.123456789Z", pairing.expiresAt)
        val detail = pairing.detail("r1")
        assertEquals("2026-11-05T08:00:00.123456789Z", detail.getString("expires_at"))
        assertEquals("r1", detail.get("id"))
    }

    @Test
    fun `says so in the server's words when it refuses`() {
        val server = ScriptedServer(mapOf("POST /api/pairing" to (404 to """{"error":"this server does not pair by phrase"}""")))

        val failure = assertThrows(ServerError::class.java) { WorkerPairingClient(server, "http://x").open() }

        assertEquals("this server does not pair by phrase", failure.message)
    }

    @Test
    fun `a server too old to pair mains opens a phone's request instead, which is withdrawn`() {
        val server = ScriptedServer(
            mapOf(
                "POST /api/pairing" to (200 to """{"state":"open","phrase":["a","b","c","d"],"expires_at":"2026-11-05T08:00:00Z","attempts_left":5}"""),
                "DELETE /api/pairing" to (200 to """{"state":"none"}"""),
            ),
        )

        val failure = assertThrows(ServerError::class.java) { WorkerPairingClient(server, "http://x").open() }

        assertTrue(failure.message!!.contains("too old"))
        assertEquals(listOf("POST", "DELETE"), server.calls.map { it.first })
    }

    @Test
    fun `refuses an answer with no phrase`() {
        val server = ScriptedServer(mapOf("POST /api/pairing" to (200 to """{"state":"open","purpose":"main"}""")))

        assertThrows(ServerError::class.java) { WorkerPairingClient(server, "http://x").open() }
        assertNull(server.calls.find { it.first == "DELETE" })
    }
}
