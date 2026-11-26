package dev.legato.mobile

import org.json.JSONArray
import org.json.JSONObject
import java.io.ByteArrayOutputStream
import java.net.SocketTimeoutException
import java.security.MessageDigest

// The glasses link: what the Legato app answers the G2 glasses app
// (packages/even-hub/) on 127.0.0.1:8792. The glasses hold no Legato address
// or token; they name one of the servers saved here, and the app makes the
// call for them with that server's own token and pinned certificate. Plain
// Kotlin, so it is tested on the JVM. The protocol is Burnlite milestone
// LEG-474's; mobile/README.md § The glasses link describes it.

// A request as the loopback server read it. Header names are lower case;
// `target` is the path with its query.
class GlassesRequest(val method: String, val target: String, val headers: Map<String, String>, val body: ByteArray) {
    override fun toString() = "$method $target"
}

class GlassesResponse(val status: Int, val headers: Map<String, String>, val body: ByteArray)

object GlassesHttp {
    // What every answer carries.
    private val COMMON = mapOf(
        "Access-Control-Allow-Origin" to "*",
        "Cache-Control" to "no-store",
        "Connection" to "close",
    )

    // How long Legato may take: a read or a send, and a transcription,
    // which carries up to a minute of sound.
    const val CALL_TIMEOUT_MS = 10_000
    const val TRANSCRIBE_TIMEOUT_MS = 30_000

    private val PREFLIGHT = mapOf(
        "Access-Control-Allow-Headers" to "Authorization, Content-Type",
        "Access-Control-Allow-Methods" to "GET, POST",
        "Access-Control-Allow-Private-Network" to "true",
    )

    // The Legato calls the glasses may make through an instance.
    private sealed class Call {
        object Agents : Call()
        class Conversation(val task: String, val limit: Int) : Call()
        object Transcribe : Call()
        class Send(val task: String) : Call()
        object Reeve : Call()
        object Approvals : Call()
        class Decide(val approval: String) : Call()
    }

    // Answers one request from the glasses. `code` is the pairing code saved
    // now (none admits nothing); `instances` the saved servers, in order;
    // `upstream` gives a server's own client, with the timeout for the call,
    // or null when the app has no token for it.
    fun answer(
        request: GlassesRequest,
        code: String?,
        instances: List<Connection>,
        upstream: (Connection, Int) -> ServerHttp?,
    ): GlassesResponse {
        if (request.method == "OPTIONS") return GlassesResponse(204, COMMON + PREFLIGHT, ByteArray(0))
        if (!admits(request.headers["authorization"], code)) return error(401, "the pairing code is wrong or missing")

        val path = request.target.substringBefore('?')
        val query = if ('?' in request.target) request.target.substringAfter('?') else null
        if (request.method == "GET" && path == "/v1/instances" && query == null) {
            return json(200, JSONArray(instances.map { JSONObject().put("id", it.id).put("name", it.name) }).toString())
        }
        val segments = path.split('/')
        // "", "v1", "instances", {id}, then the Legato path.
        if (segments.size < 5 || segments[1] != "v1" || segments[2] != "instances") return NOT_FOUND()
        val call = route(request.method, segments.drop(4), query) ?: return NOT_FOUND()
        val instanceId = decodeSegment(segments[3])
        val instance = instances.find { it.id == instanceId } ?: return error(404, "unknown instance")

        // Each body is rebuilt from the one field the call needs, never
        // passed on as sent: Go's encoding/json matches keys without regard
        // to case and keeps the last, so a second TRANSCRIPTION_ONLY: false
        // could otherwise have Legato type the text into an agent's pane.
        var body: JSONObject? = null
        if (request.method == "POST") {
            val type = request.headers["content-type"]?.substringBefore(';')?.trim()?.lowercase()
            if (type != "application/json") return error(415, "the body must be JSON (Content-Type: application/json)")
            val field = when (call) {
                Call.Transcribe -> "pcm"
                Call.Reeve -> "text"
                is Call.Decide -> "decision"
                else -> "content"
            }
            val sent = try {
                JSONObject(request.body.toString(Charsets.UTF_8))
            } catch (cause: Exception) {
                return error(400, "the body is not a JSON object")
            }
            val value = sent.opt(field) as? String ?: return error(400, "$field must be a string")
            body = when (call) {
                Call.Transcribe -> JSONObject().put("transcription_only", true).put("pcm", value)
                // Always from the glasses, so the Reeve keeps its reply short.
                Call.Reeve -> JSONObject().put("text", value).put("from", "glasses")
                // Allow or deny, with one of the approval's options; or the
                // answers to an agent's questions. Nothing else: no feedback
                // or signature. Legato refuses to allow a signed approval
                // without a signature.
                is Call.Decide -> when (value) {
                    "allow", "deny" -> {
                        val decision = JSONObject().put("decision", value)
                        if (sent.has("option")) {
                            val option = sent.opt("option") as? String ?: return error(400, "option must be a string")
                            decision.put("option", option)
                        }
                        decision.put("by", "glasses")
                    }
                    "answer" -> {
                        val answers = answers(sent.opt("answers"))
                            ?: return error(400, "answers must map question IDs to lists of strings")
                        JSONObject().put("decision", "answer").put("answers", answers).put("by", "glasses")
                    }
                    else -> return error(400, "decision must be allow, deny or answer")
                }
                else -> JSONObject().put("content", value)
            }
        }
        val legatoPath = when (call) {
            Call.Agents -> "/api/agents?view=compact&status=running"
            is Call.Conversation -> "/api/agents/${encodeSegment(call.task)}/conversation?limit=${call.limit}"
            Call.Transcribe -> "/api/voice/transcribe"
            is Call.Send -> "/api/agents/${encodeSegment(call.task)}/chimera/send"
            Call.Reeve -> "/api/reeve/message"
            Call.Approvals -> "/api/approvals?status=pending"
            is Call.Decide -> "/api/approvals/${encodeSegment(call.approval)}/decision"
        }
        val timeout = if (call is Call.Transcribe) TRANSCRIBE_TIMEOUT_MS else CALL_TIMEOUT_MS
        val http = upstream(instance, timeout) ?: return error(502, "The Legato app has no token for ${instance.name}: pair it again")
        val (status, text) = try {
            http.call(request.method, legatoPath, body)
        } catch (cause: SocketTimeoutException) {
            return error(502, "${instance.name} took too long to answer")
        } catch (cause: Exception) {
            return error(502, "Can't reach ${instance.name}")
        }
        // Legato's own 401 must never read as a wrong pairing code.
        if (status == 401) return error(502, "${instance.name} refused the Legato app's token: pair it again")
        val trimmed = text.trimStart()
        val type = if (trimmed.startsWith("{") || trimmed.startsWith("[")) "application/json" else "text/plain; charset=utf-8"
        return GlassesResponse(status, COMMON + ("Content-Type" to type), text.toByteArray(Charsets.UTF_8))
    }

    // An agent asks one to four questions at a time, each answered with the
    // labels chosen or the operator's own words; these bounds leave room
    // for that and keep the body small.
    private const val MAX_QUESTIONS = 8
    private const val MAX_QUESTION_ID = 200
    private const val MAX_ANSWERS = 32
    private const val MAX_ANSWER = 4000

    // The answers rebuilt as question IDs to lists of strings, or null when
    // they are anything else.
    private fun answers(sent: Any?): JSONObject? {
        val given = sent as? JSONObject ?: return null
        val ids = given.keys().asSequence().toList()
        if (ids.isEmpty() || ids.size > MAX_QUESTIONS) return null
        val rebuilt = JSONObject()
        for (id in ids) {
            if (id.isEmpty() || id.length > MAX_QUESTION_ID) return null
            val list = given.opt(id) as? JSONArray ?: return null
            if (list.length() == 0 || list.length() > MAX_ANSWERS) return null
            val strings = JSONArray()
            for (i in 0 until list.length()) {
                val answer = list.opt(i) as? String ?: return null
                if (answer.length > MAX_ANSWER) return null
                strings.put(answer)
            }
            rebuilt.put(id, strings)
        }
        return rebuilt
    }

    // The call a method, Legato path (as segments) and query make, or null
    // for anything outside the table. A task ID is one path segment.
    private fun route(method: String, rest: List<String>, query: String?): Call? {
        if (rest.firstOrNull() != "api") return null
        val params = query?.split('&')?.map { it.substringBefore('=') to it.substringAfter('=', "") }
        return when {
            method == "GET" && rest == listOf("api", "agents") ->
                Call.Agents.takeIf { params != null && params.toSet() == setOf("view" to "compact", "status" to "running") && params.size == 2 }
            method == "POST" && rest == listOf("api", "voice", "transcribe") && query == null -> Call.Transcribe
            method == "POST" && rest == listOf("api", "reeve", "message") && query == null -> Call.Reeve
            method == "GET" && rest == listOf("api", "approvals") ->
                Call.Approvals.takeIf { params == listOf("status" to "pending") }
            method == "POST" && rest.size == 4 && rest[1] == "approvals" && rest[3] == "decision" && query == null ->
                taskId(rest[2])?.let { Call.Decide(it) }
            rest.size >= 4 && rest[1] == "agents" -> {
                val task = taskId(rest[2]) ?: return null
                val tail = rest.drop(3)
                when {
                    method == "GET" && tail == listOf("conversation") -> {
                        val limit = params?.singleOrNull()?.takeIf { it.first == "limit" }?.second
                            ?.takeIf { it.length <= 2 && it.all(Char::isDigit) }?.toInt()
                        if (limit != null && limit in 1..50) Call.Conversation(task, limit) else null
                    }
                    method == "POST" && tail == listOf("chimera", "send") && query == null -> Call.Send(task)
                    else -> null
                }
            }
            else -> null
        }
    }

    // A task ID from its path segment: one segment, decoded, that is not a
    // step up or a slash in disguise.
    private fun taskId(segment: String): String? {
        val decoded = decodeSegment(segment) ?: return null
        if (decoded.isEmpty() || decoded == "." || decoded == ".." || '/' in decoded || '\\' in decoded) return null
        return decoded
    }

    // Percent-decoding of a path segment as UTF-8 ('+' is a plus), or null
    // when it does not decode.
    private fun decodeSegment(segment: String): String? {
        val out = ByteArrayOutputStream()
        var i = 0
        while (i < segment.length) {
            val c = segment[i]
            if (c == '%') {
                if (i + 2 >= segment.length) return null
                val byte = segment.substring(i + 1, i + 3).toIntOrNull(16) ?: return null
                out.write(byte)
                i += 3
            } else {
                out.write(c.toString().toByteArray(Charsets.UTF_8))
                i++
            }
        }
        val bytes = out.toByteArray()
        val decoded = bytes.toString(Charsets.UTF_8)
        return decoded.takeIf { it.toByteArray(Charsets.UTF_8).contentEquals(bytes) }
    }

    private fun encodeSegment(value: String): String = buildString {
        for (byte in value.toByteArray(Charsets.UTF_8)) {
            val c = byte.toInt().toChar()
            if (c in 'A'..'Z' || c in 'a'..'z' || c in '0'..'9' || c in "-._~") append(c) else append("%%%02X".format(byte.toInt() and 0xff))
        }
    }

    private val NOT_FOUND = { error(404, "not served to the glasses") }

    // Whether the Authorization header carries the saved code, compared in
    // constant time.
    private fun admits(authorization: String?, code: String?): Boolean {
        if (code.isNullOrEmpty()) return false
        val given = authorization?.removePrefix("Bearer ")?.takeIf { it != authorization } ?: return false
        return MessageDigest.isEqual(given.toByteArray(Charsets.UTF_8), code.toByteArray(Charsets.UTF_8))
    }

    // One of the link's own refusals.
    fun error(status: Int, message: String): GlassesResponse = json(status, JSONObject().put("error", message).toString())

    private fun json(status: Int, text: String) =
        GlassesResponse(status, COMMON + ("Content-Type" to "application/json"), text.toByteArray(Charsets.UTF_8))
}

private const val BASE32 = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567"

// A new pairing code for the glasses: 24 base32 characters (120 bits).
fun newGlassesCode(random: java.security.SecureRandom = java.security.SecureRandom()): String {
    val bytes = ByteArray(24).also(random::nextBytes)
    // 32 divides 256, so taking the low five bits of each byte is unbiased.
    return String(CharArray(24) { BASE32[bytes[it].toInt() and 31] })
}
