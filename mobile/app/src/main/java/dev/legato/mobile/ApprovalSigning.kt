package dev.legato.mobile

import org.json.JSONArray
import org.json.JSONObject
import java.util.Base64

// Signed approvals: this phone holds a key in its secure hardware for each
// server, unlocked only by the person's fingerprint or PIN for each use. A
// decision is signed over the request as the server built it, so neither a
// page nor an agent can change what the person signs. The server side is
// internal/service/approval_devices.go.

// One call to a server's API with its token. Blocking.
fun interface ServerHttp {
    fun call(method: String, path: String, body: JSONObject?): Pair<Int, String>
}

class ServerError(message: String) : Exception(message)

private fun read(answer: Pair<Int, String>): JSONObject {
    val (status, text) = answer
    val json = runCatching { JSONObject(text) }.getOrNull()
    if (status !in 200..299) throw ServerError(json?.optString("error")?.takeIf { it.isNotBlank() } ?: "The server answered $status.")
    return json ?: throw ServerError("The server's answer does not read.")
}

private val b64 = Base64.getEncoder()
private val b64url = Base64.getUrlDecoder()

data class EnrollResult(val deviceId: String, val status: String, val name: String, val securityLevel: String, val approvalId: String?)
data class DecisionChallenge(val canonical: ByteArray, val challenge: String)
data class OpenApproval(val id: String, val title: String, val source: String, val signed: Boolean, val createdAt: String)

// ApprovalClient speaks the signed-approval endpoints of one server.
class ApprovalClient(private val http: ServerHttp) {
    fun enrollChallenge(): ByteArray = b64url.decode(read(http.call("POST", "/api/devices/challenge", null)).getString("challenge"))

    fun enroll(name: String, chain: List<ByteArray>, challenge: ByteArray): EnrollResult {
        val body = JSONObject()
            .put("name", name).put("kind", "android")
            .put("chain", JSONArray(chain.map { b64.encodeToString(it) }))
            .put("challenge", Base64.getUrlEncoder().withoutPadding().encodeToString(challenge))
        val d = read(http.call("POST", "/api/devices", body))
        return EnrollResult(d.getString("id"), d.getString("status"), d.optString("name"), d.optString("security_level"), d.optString("approval_id").ifBlank { null })
    }

    fun challenge(approvalId: String, deviceId: String): DecisionChallenge {
        val c = read(http.call("POST", "/api/approvals/$approvalId/challenge", JSONObject().put("device", deviceId)))
        return DecisionChallenge(Base64.getDecoder().decode(c.getString("canonical")), c.getString("challenge"))
    }

    fun decide(approvalId: String, decision: String, option: String, deviceId: String, challenge: String, signature: ByteArray) {
        val body = JSONObject().put("decision", decision).put("option", option).put("by", "phone")
            .put("signature", JSONObject().put("device", deviceId).put("challenge", challenge).put("value", b64.encodeToString(signature)))
        read(http.call("POST", "/api/approvals/$approvalId/decision", body))
    }

    fun pending(): List<OpenApproval> {
        val list = read(http.call("GET", "/api/approvals?status=pending", null)).optJSONArray("approvals") ?: JSONArray()
        return (0 until list.length()).map {
            val a = list.getJSONObject(it)
            OpenApproval(a.getString("id"), a.optString("title"), a.optString("source"), a.optBoolean("signed"), a.optString("created_at"))
        }
    }
}

// The key a phone makes for a server, as parameters an Android Keystore
// spec is built from.
data class KeyParams(
    val alias: String,
    val challenge: ByteArray,
    val strongBox: Boolean,
    // Unlocked for one signature by the person each time.
    val perUseAuthentication: Boolean,
    // The device PIN unlocks it too (Android 11 and later).
    val allowDeviceCredential: Boolean,
    val invalidatedByBiometricEnrollment: Boolean,
)

fun keyParams(sdk: Int, alias: String, challenge: ByteArray, strongBox: Boolean) = KeyParams(
    alias = alias, challenge = challenge, strongBox = strongBox,
    perUseAuthentication = true, allowDeviceCredential = sdk >= 30, invalidatedByBiometricEnrollment = true,
)

// The phone's keys, one per server.
interface DeviceKeys {
    // Makes the key with the challenge and returns its attestation chain,
    // leaf first, as DER.
    fun create(alias: String, challenge: ByteArray): List<ByteArray>
    fun has(alias: String): Boolean
    fun delete(alias: String)
}

// Enrollment makes this phone's key for a server and asks the server to
// accept it. A refused key is deleted.
class Enrollment(private val client: ApprovalClient, private val keys: DeviceKeys, private val deviceName: String) {
    fun enroll(alias: String): EnrollResult {
        val challenge = client.enrollChallenge()
        if (keys.has(alias)) keys.delete(alias)
        val chain = keys.create(alias, challenge)
        return try {
            client.enroll(deviceName, chain, challenge)
        } catch (cause: Exception) {
            keys.delete(alias)
            throw cause
        }
    }
}

// signedMessage is what the key signs: the RFC 8785 form of {challenge,
// device, option, request, v}. The parts are IDs that need no escaping and
// the request is already canonical, so joining them is that form.
fun signedMessage(challenge: String, device: String, option: String, canonical: ByteArray): ByteArray =
    ("{\"challenge\":\"$challenge\",\"device\":\"$device\",\"option\":\"$option\",\"request\":" +
        String(canonical, Charsets.UTF_8) + ",\"v\":1}").toByteArray(Charsets.UTF_8)

data class SheetOption(val id: String, val label: String)

// What the approval sheet shows: everything from the request the phone
// fetched itself. The reason is the agent's text and is shown as text.
data class SheetModel(
    val approval: String,
    val server: String,
    val source: String,
    val title: String,
    val summary: String,
    val credential: String?,
    val operations: List<String>,
    val resources: List<String>,
    val reason: String?,
    val taskTitle: String?,
    val agentKind: String?,
    val allows: List<SheetOption>,
)

private fun strings(array: JSONArray?): List<String> = if (array == null) emptyList() else (0 until array.length()).map { array.optString(it) }

fun parseSheet(canonical: ByteArray): SheetModel {
    val r = JSONObject(String(canonical, Charsets.UTF_8))
    val details = r.optJSONObject("details") ?: JSONObject()
    val options = r.optJSONArray("options") ?: JSONArray()
    val allows = (0 until options.length()).map { options.getJSONObject(it) }
        .filter { it.optString("kind") == "allow" }
        .map { SheetOption(it.getString("id"), it.optString("label")) }
    return SheetModel(
        approval = r.getString("approval"), server = r.optString("server"), source = r.optString("source"),
        title = r.optString("title"), summary = r.optString("summary"),
        credential = details.optString("credential").ifBlank { null },
        operations = strings(details.optJSONArray("operations")),
        resources = strings(details.optJSONArray("resources")),
        reason = details.optString("reason").ifBlank { null },
        taskTitle = details.optString("task_title").ifBlank { null },
        agentKind = details.optString("agent_kind").ifBlank { null },
        allows = allows,
    )
}

// The open approvals of one saved server.
data class ServerApprovals(val serverId: String, val serverName: String, val approvals: List<OpenApproval>, val error: String?)

// listOpenApprovals reads every saved server's open approvals; a server
// that does not answer is listed with why.
fun listOpenApprovals(servers: List<Pair<String, String>>, client: (String) -> ApprovalClient): List<ServerApprovals> =
    servers.map { (id, name) ->
        try {
            ServerApprovals(id, name, client(id).pending(), null)
        } catch (cause: Exception) {
            ServerApprovals(id, name, emptyList(), cause.message ?: "unreachable")
        }
    }
