package dev.legato.mobile

import org.json.JSONArray
import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import java.io.IOException
import java.net.SocketTimeoutException
import org.junit.Test

private const val CODE = "ABCDEFGHIJKLMNOPQRSTUVWX"

private val desk = Connection(id = "0b7a-desk", name = "Desk", baseUrl = "https://desk.example:3080", createdAt = "2026-11-01T00:00:00Z")
private val laptop = Connection(id = "9f21-laptop", name = "Laptop", baseUrl = "https://laptop.example:3080", createdAt = "2026-11-02T00:00:00Z")

// A Legato per instance, answering from a script and recording what it was
// asked and with which timeout.
private class Legatos(var answer: (Connection, String, String) -> Pair<Int, String> = { _, _, _ -> 200 to "[]" }) {
    data class Asked(val instance: String, val method: String, val path: String, val body: JSONObject?, val timeoutMs: Int)
    val asked = mutableListOf<Asked>()

    fun upstream(connection: Connection, timeoutMs: Int): ServerHttp = ServerHttp { method, path, body ->
        asked += Asked(connection.id, method, path, body, timeoutMs)
        answer(connection, method, path)
    }
}

private fun request(method: String, target: String, code: String? = CODE, body: String? = null, contentType: String? = "application/json") =
    GlassesRequest(
        method = method,
        target = target,
        headers = buildMap {
            if (code != null) put("authorization", "Bearer $code")
            if (body != null && contentType != null) put("content-type", contentType)
        },
        body = body?.toByteArray() ?: ByteArray(0),
    )

private fun GlassesResponse.text() = body.toString(Charsets.UTF_8)
private fun GlassesResponse.json() = JSONObject(text())

class GlassesHttpTest {
    private val legatos = Legatos()

    private fun answer(request: GlassesRequest, code: String? = CODE, instances: List<Connection> = listOf(desk, laptop)) =
        GlassesHttp.answer(request, code, instances, legatos::upstream)

    @Test
    fun `lists the saved instances by ID and name, in order, to the right code`() {
        val answer = answer(request("GET", "/v1/instances"))

        assertEquals(200, answer.status)
        val listed = JSONArray(answer.text())
        assertEquals(2, listed.length())
        assertEquals(setOf("id", "name"), listed.getJSONObject(0).keys().asSequence().toSet())
        assertEquals(listOf("0b7a-desk" to "Desk", "9f21-laptop" to "Laptop"), (0 until 2).map { listed.getJSONObject(it).let { o -> o.getString("id") to o.getString("name") } })
    }

    @Test
    fun `refuses a missing or wrong code, asking no Legato`() {
        val refused = listOf(
            request("GET", "/v1/instances", code = null),
            request("GET", "/v1/instances", code = "ABCDEFGHIJKLMNOPQRSTUVWY"),
            request("GET", "/v1/instances", code = "ABCDEFGHIJKLMNOPQRSTUVW"),
            request("GET", "/v1/instances", code = CODE.lowercase()),
            request("GET", "/v1/instances/0b7a-desk/api/agents?view=compact&status=running", code = "WRONG"),
            GlassesRequest("GET", "/v1/instances", mapOf("authorization" to CODE), ByteArray(0)),
        )

        for (request in refused) assertEquals(request.toString(), 401, answer(request).status)
        assertEquals(emptyList<Legatos.Asked>(), legatos.asked)
    }

    @Test
    fun `admits nothing when no code is saved`() {
        assertEquals(401, answer(request("GET", "/v1/instances", code = ""), code = "").status)
        assertEquals(401, answer(request("GET", "/v1/instances", code = null), code = null).status)
        assertEquals(401, answer(request("GET", "/v1/instances", code = ""), code = null).status)
    }

    @Test
    fun `answers a preflight without a code, allowing the glasses' headers from any origin`() {
        val answer = answer(GlassesRequest("OPTIONS", "/v1/instances/0b7a-desk/api/voice/transcribe", emptyMap(), ByteArray(0)), code = null)

        assertEquals(204, answer.status)
        assertEquals("*", answer.headers["Access-Control-Allow-Origin"])
        assertEquals("Authorization, Content-Type", answer.headers["Access-Control-Allow-Headers"])
        assertEquals("GET, POST", answer.headers["Access-Control-Allow-Methods"])
        assertEquals("true", answer.headers["Access-Control-Allow-Private-Network"])
        assertEquals(0, answer.body.size)
    }

    @Test
    fun `marks every answer as not to be kept, the connection as closing, and any origin as allowed`() {
        legatos.answer = { _, _, _ -> 500 to "boom" }
        val answers = listOf(
            answer(request("GET", "/v1/instances")),
            answer(request("GET", "/v1/instances", code = null)),
            answer(request("GET", "/nowhere")),
            answer(request("GET", "/v1/instances/0b7a-desk/api/agents?view=compact&status=running")),
            answer(GlassesRequest("OPTIONS", "/", emptyMap(), ByteArray(0))),
        )

        for (answer in answers) {
            assertEquals("*", answer.headers["Access-Control-Allow-Origin"])
            assertEquals("no-store", answer.headers["Cache-Control"])
            assertEquals("close", answer.headers["Connection"])
        }
    }

    @Test
    fun `passes each read on to the instance it names, with that instance's own client`() {
        answer(request("GET", "/v1/instances/0b7a-desk/api/agents?view=compact&status=running"))
        answer(request("GET", "/v1/instances/9f21-laptop/api/agents/LEG-7/conversation?limit=20"))

        assertEquals(
            listOf(
                Legatos.Asked("0b7a-desk", "GET", "/api/agents?view=compact&status=running", null, 10_000),
                Legatos.Asked("9f21-laptop", "GET", "/api/agents/LEG-7/conversation?limit=20", null, 10_000),
            ),
            legatos.asked,
        )
    }

    @Test
    fun `passes Legato's status and body back as they came`() {
        legatos.answer = { _, _, path -> if (path.endsWith("conversation?limit=50")) 409 to "agent has no native endpoint\n" else 200 to """[{"task_id":"LEG-7"}]""" }

        val agents = answer(request("GET", "/v1/instances/0b7a-desk/api/agents?view=compact&status=running"))
        val conversation = answer(request("GET", "/v1/instances/0b7a-desk/api/agents/LEG-7/conversation?limit=50"))

        assertEquals(200, agents.status)
        assertEquals("""[{"task_id":"LEG-7"}]""", agents.text())
        assertEquals(409, conversation.status)
        assertEquals("agent has no native endpoint\n", conversation.text())
    }

    @Test
    fun `sends a recording for transcription only, rebuilt from the recording alone, with longer to answer`() {
        legatos.answer = { _, _, _ -> 200 to """{"text":"Hi"}""" }
        // org.json, like Go, would read both spellings; Go would keep the last.
        val body = """{"transcription_only":false,"TRANSCRIPTION_ONLY":false,"agent_id":"LEG-7","pcm":"AQ=="}"""

        val answer = answer(request("POST", "/v1/instances/0b7a-desk/api/voice/transcribe", body = body))

        assertEquals(200, answer.status)
        val asked = legatos.asked.single()
        assertEquals("POST" to "/api/voice/transcribe", asked.method to asked.path)
        assertEquals(setOf("transcription_only", "pcm"), asked.body!!.keys().asSequence().toSet())
        assertEquals(true, asked.body!!.get("transcription_only"))
        assertEquals("AQ==", asked.body!!.getString("pcm"))
        assertEquals(30_000, asked.timeoutMs)
    }

    @Test
    fun `sends a message as its content alone`() {
        legatos.answer = { _, _, _ -> 202 to "" }
        val body = """{"content":"Run the tests","CONTENT":"rm -rf /","queue":"busy"}"""

        val answer = answer(request("POST", "/v1/instances/9f21-laptop/api/agents/LEG-7/chimera/send", body = body))

        assertEquals(202, answer.status)
        val asked = legatos.asked.single()
        assertEquals("9f21-laptop" to "/api/agents/LEG-7/chimera/send", asked.instance to asked.path)
        assertEquals(setOf("content"), asked.body!!.keys().asSequence().toSet())
        assertEquals("Run the tests", asked.body!!.getString("content"))
        assertEquals(10_000, asked.timeoutMs)
    }

    @Test
    fun `sends the Reeve a message as its text alone, marked as from the glasses`() {
        legatos.answer = { _, _, _ -> 200 to """{"status":"sent"}""" }
        val body = """{"text":"Start an agent on LEG-212","from":"web","TEXT":"rm -rf /"}"""

        val answer = answer(request("POST", "/v1/instances/0b7a-desk/api/reeve/message", body = body))

        assertEquals(200, answer.status)
        val asked = legatos.asked.single()
        assertEquals("0b7a-desk" to "/api/reeve/message", asked.instance to asked.path)
        assertEquals(setOf("text", "from"), asked.body!!.keys().asSequence().toSet())
        assertEquals("Start an agent on LEG-212", asked.body!!.getString("text"))
        assertEquals("glasses", asked.body!!.getString("from"))
        assertEquals(10_000, asked.timeoutMs)
    }

    @Test
    fun `lists the pending approvals`() {
        legatos.answer = { _, _, _ -> 200 to """{"approvals":[]}""" }

        val answer = answer(request("GET", "/v1/instances/0b7a-desk/api/approvals?status=pending"))

        assertEquals(200, answer.status)
        assertEquals("0b7a-desk" to "/api/approvals?status=pending", legatos.asked.single().instance to legatos.asked.single().path)
    }

    @Test
    fun `decides an approval with allow or deny and an option alone, marked as decided on the glasses`() {
        legatos.answer = { _, _, _ -> 200 to """{"id":"ap-1"}""" }
        val body = """{"decision":"allow","option":"once","by":"web","signature":{"device":"x"},"answers":{"q":["a"]}}"""

        val answer = answer(request("POST", "/v1/instances/0b7a-desk/api/approvals/ap-1a2b3c4d/decision", body = body))

        assertEquals(200, answer.status)
        val asked = legatos.asked.single()
        assertEquals("/api/approvals/ap-1a2b3c4d/decision", asked.path)
        assertEquals(setOf("decision", "option", "by"), asked.body!!.keys().asSequence().toSet())
        assertEquals("allow", asked.body!!.getString("decision"))
        assertEquals("once", asked.body!!.getString("option"))
        assertEquals("glasses", asked.body!!.getString("by"))

        legatos.asked.clear()
        answer(request("POST", "/v1/instances/0b7a-desk/api/approvals/ap-1a2b3c4d/decision", body = """{"decision":"deny"}"""))
        assertEquals(setOf("decision", "by"), legatos.asked.single().body!!.keys().asSequence().toSet())
    }

    @Test
    fun `answers a question with its answers alone, rebuilt from question IDs and strings, marked as from the glasses`() {
        legatos.answer = { _, _, _ -> 200 to """{"id":"ap-q"}""" }
        val body = """{"decision":"answer","answers":{"q1":["Beside the plan (Recommended)"],"q2":["A","B"]},""" +
            """"option":"once","by":"web","Answers":{"q1":["typed"]},"signature":{"device":"x"}}"""

        val answer = answer(request("POST", "/v1/instances/0b7a-desk/api/approvals/ap-q/decision", body = body))

        assertEquals(200, answer.status)
        val asked = legatos.asked.single()
        assertEquals("/api/approvals/ap-q/decision", asked.path)
        assertEquals(setOf("decision", "answers", "by"), asked.body!!.keys().asSequence().toSet())
        assertEquals("answer", asked.body!!.getString("decision"))
        assertEquals("glasses", asked.body!!.getString("by"))
        val answers = asked.body!!.getJSONObject("answers")
        assertEquals(setOf("q1", "q2"), answers.keys().asSequence().toSet())
        assertEquals("""["Beside the plan (Recommended)"]""", answers.getJSONArray("q1").toString())
        assertEquals("""["A","B"]""", answers.getJSONArray("q2").toString())
    }

    @Test
    fun `refuses answers that are not question IDs to short lists of strings`() {
        val many = (1..9).joinToString(",") { "\"q$it\":[\"a\"]" }
        val long = "x".repeat(4001)
        val refused = listOf(
            """{"decision":"answer"}""",
            """{"decision":"answer","answers":["a"]}""",
            """{"decision":"answer","answers":{}}""",
            """{"decision":"answer","answers":{"q1":"a"}}""",
            """{"decision":"answer","answers":{"q1":[]}}""",
            """{"decision":"answer","answers":{"q1":[1]}}""",
            """{"decision":"answer","answers":{"q1":["a",null]}}""",
            """{"decision":"answer","answers":{$many}}""",
            """{"decision":"answer","answers":{"q1":["$long"]}}""",
            """{"decision":"answer","answers":{"${"q".repeat(201)}":["a"]}}""",
            """{"decision":"answer","answers":{"q1":[${(1..33).joinToString(",") { "\"a\"" }}]}}""",
        )

        for (body in refused) {
            assertEquals(body.take(80), 400, answer(request("POST", "/v1/instances/0b7a-desk/api/approvals/ap-q/decision", body = body)).status)
        }
        assertEquals(emptyList<Legatos.Asked>(), legatos.asked)
    }

    @Test
    fun `refuses a decision that is not allow, deny or answer, or an option that is not a string`() {
        val refused = listOf(
            """{"decision":"ANSWER","answers":{"q1":["a"]}}""",
            """{"decision":"ALLOW"}""",
            """{"option":"once"}""",
            """{"decision":"allow","option":1}""",
        )

        for (body in refused) {
            assertEquals(body, 400, answer(request("POST", "/v1/instances/0b7a-desk/api/approvals/ap-1/decision", body = body)).status)
        }
        assertEquals(emptyList<Legatos.Asked>(), legatos.asked)
    }

    @Test
    fun `refuses a body that is not JSON or lacks the call's field`() {
        val refused = listOf(
            request("POST", "/v1/instances/0b7a-desk/api/voice/transcribe", body = "pcm"),
            request("POST", "/v1/instances/0b7a-desk/api/voice/transcribe", body = """{"pcm":1}"""),
            request("POST", "/v1/instances/0b7a-desk/api/voice/transcribe", body = "[]"),
            request("POST", "/v1/instances/0b7a-desk/api/agents/A/chimera/send", body = """{"content":["Hi"]}"""),
            request("POST", "/v1/instances/0b7a-desk/api/agents/A/chimera/send", body = """{"text":"Hi"}"""),
            request("POST", "/v1/instances/0b7a-desk/api/reeve/message", body = """{"content":"Hi"}"""),
        )

        for (request in refused) assertEquals(request.toString(), 400, answer(request).status)
        assertEquals(emptyList<Legatos.Asked>(), legatos.asked)
    }

    @Test
    fun `refuses a POST that is not JSON`() {
        val plain = request("POST", "/v1/instances/0b7a-desk/api/agents/A/chimera/send", body = """{"content":"Hi"}""", contentType = "text/plain")
        val none = request("POST", "/v1/instances/0b7a-desk/api/agents/A/chimera/send", body = """{"content":"Hi"}""", contentType = null)
        val withCharset = request("POST", "/v1/instances/0b7a-desk/api/agents/A/chimera/send", body = """{"content":"Hi"}""", contentType = "Application/JSON; charset=utf-8")

        assertEquals(415, answer(plain).status)
        assertEquals(415, answer(none).status)
        assertEquals(emptyList<Legatos.Asked>(), legatos.asked)
        legatos.answer = { _, _, _ -> 202 to "" }
        assertEquals(202, answer(withCharset).status)
    }

    @Test
    fun `answers 404 to anything outside the glasses' calls, asking no Legato`() {
        val refused = listOf(
            request("GET", "/v1/instances/"),
            request("GET", "/v1/instances?x=1"),
            request("POST", "/v1/instances", body = "{}"),
            request("GET", "/v1/instances/0b7a-desk"),
            request("GET", "/v1/instances/0b7a-desk/api/agents"),
            request("GET", "/v1/instances/0b7a-desk/api/agents?view=compact"),
            request("GET", "/v1/instances/0b7a-desk/api/agents?view=full&status=running"),
            request("GET", "/v1/instances/0b7a-desk/api/agents?view=compact&status=running&x=1"),
            request("GET", "/v1/instances/0b7a-desk/api/tasks"),
            request("GET", "/v1/instances/0b7a-desk/api/agents/LEG-7/conversation"),
            request("GET", "/v1/instances/0b7a-desk/api/agents/LEG-7/conversation?limit=0"),
            request("GET", "/v1/instances/0b7a-desk/api/agents/LEG-7/conversation?limit=51"),
            request("GET", "/v1/instances/0b7a-desk/api/agents/LEG-7/conversation?limit=x"),
            request("GET", "/v1/instances/0b7a-desk/api/agents/a/b/conversation?limit=20"),
            request("GET", "/v1/instances/0b7a-desk/api/agents/a%2Fb/conversation?limit=20"),
            request("GET", "/v1/instances/0b7a-desk/api/agents/../conversation?limit=20"),
            request("GET", "/v1/instances/0b7a-desk/api/agents/LEG-7/chimera/send"),
            request("POST", "/v1/instances/0b7a-desk/api/agents/spawn", body = "{}"),
            request("POST", "/v1/instances/0b7a-desk/api/agents/LEG-7/chimera/interrupt", body = "{}"),
            request("POST", "/v1/instances/0b7a-desk/api/voice/transcribe?agent_id=LEG-7", body = """{"pcm":"AQ=="}"""),
            request("DELETE", "/v1/instances/0b7a-desk/api/agents"),
            request("GET", "/v1/instances/0b7a-desk/api/reeve/message"),
            request("POST", "/v1/instances/0b7a-desk/api/reeve/message?from=web", body = """{"text":"Hi"}"""),
            request("POST", "/v1/instances/0b7a-desk/api/reeve/sessions", body = "{}"),
            request("GET", "/v1/instances/0b7a-desk/api/approvals"),
            request("GET", "/v1/instances/0b7a-desk/api/approvals?status=all"),
            request("GET", "/v1/instances/0b7a-desk/api/approvals/ap-1"),
            request("POST", "/v1/instances/0b7a-desk/api/approvals/ap-1/dismiss", body = "{}"),
            request("POST", "/v1/instances/0b7a-desk/api/approvals/ap-1/challenge", body = "{}"),
            request("POST", "/v1/instances/0b7a-desk/api/approvals/a%2Fb/decision", body = """{"decision":"deny"}"""),
            request("GET", "/api/agents?view=compact&status=running"),
        )

        for (request in refused) assertEquals(request.toString(), 404, answer(request).status)
        assertEquals(emptyList<Legatos.Asked>(), legatos.asked)
    }

    @Test
    fun `says when the instance is no longer saved`() {
        val answer = answer(request("GET", "/v1/instances/gone/api/agents?view=compact&status=running"))

        assertEquals(404, answer.status)
        assertEquals("unknown instance", answer.json().getString("error"))
    }

    @Test
    fun `turns Legato's refusal of the app's token into a 502, so it never reads as a wrong code`() {
        legatos.answer = { _, _, _ -> 401 to """{"error":"unauthorized"}""" }

        val answer = answer(request("GET", "/v1/instances/0b7a-desk/api/agents?view=compact&status=running"))

        assertEquals(502, answer.status)
        assertTrue(answer.json().getString("error").contains("token"))
    }

    @Test
    fun `answers 502 when Legato can't be reached or takes too long, or the app has no token for it`() {
        legatos.answer = { _, _, _ -> throw IOException("connection refused") }
        val unreachable = answer(request("GET", "/v1/instances/0b7a-desk/api/agents?view=compact&status=running"))
        legatos.answer = { _, _, _ -> throw SocketTimeoutException("read timed out") }
        val slow = answer(request("GET", "/v1/instances/0b7a-desk/api/agents?view=compact&status=running"))
        val tokenless = GlassesHttp.answer(request("GET", "/v1/instances/0b7a-desk/api/agents?view=compact&status=running"), CODE, listOf(desk)) { _, _ -> null }

        for (answer in listOf(unreachable, slow, tokenless)) {
            assertEquals(502, answer.status)
            assertFalse(answer.json().getString("error").isBlank())
        }
    }

    @Test
    fun `makes pairing codes of 24 base32 characters, a new one each time`() {
        val codes = (1..50).map { newGlassesCode() }

        for (code in codes) assertTrue(code, Regex("[A-Z2-7]{24}").matches(code))
        assertEquals(50, codes.toSet().size)
    }
}
