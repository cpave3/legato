package dev.legato.mobile

import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.util.Base64

// A server that answers the approval endpoints from a script.
private class FakeServer(val answers: MutableMap<String, Pair<Int, String>> = mutableMapOf()) : ServerHttp {
    val calls = mutableListOf<Triple<String, String, JSONObject?>>()
    override fun call(method: String, path: String, body: JSONObject?): Pair<Int, String> {
        calls += Triple(method, path, body)
        return answers["$method $path"] ?: (404 to """{"error":"not found"}""")
    }
}

private class FakeKeys : DeviceKeys {
    val made = mutableMapOf<String, ByteArray>()
    override fun create(alias: String, challenge: ByteArray): List<ByteArray> {
        made[alias] = challenge
        return listOf(byteArrayOf(1, 2, 3), byteArrayOf(4, 5))
    }
    override fun has(alias: String) = alias in made
    override fun delete(alias: String) { made.remove(alias) }
}

private val canonical = """{"approval":"ap-1","details":{"credential":"github","operations":["git.push"],"reason":"Push the <b>fix</b>","resources":["github:cpave3/legato@refs/heads/legato/x"],"task_title":"Fix sort","agent_kind":"claude-code"},"execution":"ex-1","options":[{"id":"session","kind":"allow","label":"For this session"},{"id":"once","kind":"allow","label":"Once"},{"id":"deny","kind":"deny","label":"Deny"}],"revision":1,"server":"srv-1","source":"credential","summary":"git.push on github:cpave3/legato@refs/heads/legato/x","task":"t1","title":"Use github","v":1}"""

class ApprovalSigningTest {
    @Test
    fun `a new key needs the person for each use, in StrongBox when there is one`() {
        val p30 = keyParams(sdk = 34, alias = "approval-s1", challenge = byteArrayOf(9), strongBox = true)
        assertTrue(p30.perUseAuthentication)
        assertTrue(p30.allowDeviceCredential)
        assertTrue(p30.invalidatedByBiometricEnrollment)
        assertTrue(p30.strongBox)
        assertEquals("approval-s1", p30.alias)
        // Android 10 has no per-use PIN: a fingerprint, each time.
        val p29 = keyParams(sdk = 29, alias = "approval-s1", challenge = byteArrayOf(9), strongBox = false)
        assertTrue(p29.perUseAuthentication)
        assertFalse(p29.allowDeviceCredential)
        assertFalse(p29.strongBox)
    }

    @Test
    fun `enrolling makes a key with the server's challenge and sends its chain`() {
        val challenge = byteArrayOf(7, 7, 7)
        val server = FakeServer(mutableMapOf(
            "POST /api/devices/challenge" to (200 to """{"challenge":"${Base64.getUrlEncoder().withoutPadding().encodeToString(challenge)}","server_id":"srv-1"}"""),
            "POST /api/devices" to (201 to """{"id":"dev-1","status":"active","name":"Pixel 9 Pro","security_level":"strongbox"}"""),
        ))
        val keys = FakeKeys()
        val result = Enrollment(ApprovalClient(server), keys, "Pixel 9 Pro").enroll("approval-s1")
        assertEquals("dev-1", result.deviceId)
        assertEquals("active", result.status)
        assertTrue(keys.made["approval-s1"]!!.contentEquals(challenge))
        val body = server.calls.last().third!!
        assertEquals("Pixel 9 Pro", body.getString("name"))
        assertEquals("android", body.getString("kind"))
        assertEquals(2, body.getJSONArray("chain").length())
        assertEquals(Base64.getEncoder().encodeToString(byteArrayOf(1, 2, 3)), body.getJSONArray("chain").getString(0))
    }

    @Test
    fun `a refused enrollment removes the key it made`() {
        val server = FakeServer(mutableMapOf(
            "POST /api/devices/challenge" to (200 to """{"challenge":"AAAA"}"""),
            "POST /api/devices" to (403 to """{"error":"the device's key is not accepted: the key is not in secure hardware"}"""),
        ))
        val keys = FakeKeys()
        val error = runCatching { Enrollment(ApprovalClient(server), keys, "Phone").enroll("approval-s1") }.exceptionOrNull()
        assertTrue(error?.message?.contains("secure hardware") == true)
        assertFalse(keys.has("approval-s1"))
    }

    @Test
    fun `the message signed is the server's, byte for byte`() {
        val msg = signedMessage("Q2hhbGxlbmdl", "dev-1", "once", canonical.toByteArray())
        assertEquals("""{"challenge":"Q2hhbGxlbmdl","device":"dev-1","option":"once","request":$canonical,"v":1}""", String(msg))
    }

    @Test
    fun `the sheet is drawn from the canonical request, the reason kept as the agent's text`() {
        val sheet = parseSheet(canonical.toByteArray())
        assertEquals("Use github", sheet.title)
        assertEquals("github", sheet.credential)
        assertEquals(listOf("git.push"), sheet.operations)
        assertEquals(listOf("github:cpave3/legato@refs/heads/legato/x"), sheet.resources)
        assertEquals("Push the <b>fix</b>", sheet.reason)
        assertEquals("Fix sort", sheet.taskTitle)
        assertEquals(listOf("session", "once"), sheet.allows.map { it.id })
        assertEquals("For this session", sheet.allows.first().label)
        assertEquals("srv-1", sheet.server)
    }

    @Test
    fun `a decision carries the signature over what was signed`() {
        val server = FakeServer(mutableMapOf(
            "POST /api/approvals/ap-1/challenge" to (200 to """{"canonical":"${Base64.getEncoder().encodeToString(canonical.toByteArray())}","challenge":"Q2hhbGxlbmdl"}"""),
            "POST /api/approvals/ap-1/decision" to (200 to """{"id":"ap-1","status":"approved"}"""),
        ))
        val client = ApprovalClient(server)
        val challenge = client.challenge("ap-1", "dev-1")
        assertEquals(canonical, String(challenge.canonical))
        client.decide("ap-1", "allow", "once", "dev-1", challenge.challenge, byteArrayOf(0x30, 0x45))
        val body = server.calls.last().third!!
        assertEquals("allow", body.getString("decision"))
        assertEquals("once", body.getString("option"))
        assertEquals("phone", body.getString("by"))
        val sig = body.getJSONObject("signature")
        assertEquals("dev-1", sig.getString("device"))
        assertEquals("Q2hhbGxlbmdl", sig.getString("challenge"))
        assertEquals(Base64.getEncoder().encodeToString(byteArrayOf(0x30, 0x45)), sig.getString("value"))
    }

    @Test
    fun `open approvals are listed from every saved server, an unreachable one marked`() {
        val a = FakeServer(mutableMapOf("GET /api/approvals?status=pending" to (200 to """{"approvals":[{"id":"ap-1","title":"Use github","source":"credential","signed":true,"created_at":"2026-10-29T10:00:00Z"}]}""")))
        val b = FakeServer()
        val lists = listOpenApprovals(listOf("s1" to "Erebus", "s2" to "Laptop")) { id -> if (id == "s1") ApprovalClient(a) else ApprovalClient(b) }
        assertEquals(2, lists.size)
        assertEquals("Erebus", lists[0].serverName)
        assertEquals("ap-1", lists[0].approvals.single().id)
        assertTrue(lists[0].approvals.single().signed)
        assertNull(lists[0].error)
        assertTrue(lists[1].error != null)
    }
}
