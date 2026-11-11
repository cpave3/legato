package dev.legato.mobile

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertThrows
import org.junit.Test

class ReeveVoiceTest {
    private fun server(id: String, role: String? = null) =
        Connection(id = id, name = "S$id", baseUrl = "http://10.0.0.$id:3080", createdAt = "", instanceId = role?.let { "inst-$id" }, role = role)

    @Test
    fun `the one server that says main is the main`() {
        val main = server("1", "main")
        assertEquals(main, voiceMain(listOf(server("2", "worker"), main, server("3"))))
    }

    @Test
    fun `a lone server is its own main`() {
        val only = server("1", "standalone")
        assertEquals(only, voiceMain(listOf(only)))
        assertEquals(server("2"), voiceMain(listOf(server("2"))))
    }

    @Test
    fun `no main is chosen when the servers do not say which`() {
        assertNull(voiceMain(emptyList()))
        assertNull(voiceMain(listOf(server("1", "worker"), server("2", "standalone"))))
        assertNull(voiceMain(listOf(server("1", "main"), server("2", "main"))))
    }

    @Test
    fun `reads who a server is from its voice status`() {
        val http = ServerHttp { method, path, _ ->
            assertEquals("GET", method)
            assertEquals("/api/reeve/voice", path)
            200 to """{"instance":{"id":"i-1","name":"desk","role":"main"},"ready":true,"reason":""}"""
        }
        assertEquals(VoiceInstance("i-1", "desk", "main"), fetchVoiceInstance(http))
    }

    @Test
    fun `a server without the voice route says why`() {
        val error = assertThrows(ServerError::class.java) { fetchVoiceInstance { _, _, _ -> 404 to """{"error":"not found"}""" } }
        assertEquals("not found", error.message)
        assertThrows(ServerError::class.java) { fetchVoiceInstance { _, _, _ -> 200 to """{"ready":true}""" } }
    }

    @Test
    fun `learns every server's role, keeping what it knew of one that does not answer`() {
        val known = server("2", "worker")
        val learned = learnVoiceRoles(listOf(server("1"), known)) { connection ->
            if (connection.id == "1") VoiceInstance("i-1", "desk", "main") else throw java.io.IOException("offline")
        }
        assertEquals(mapOf("1" to VoiceInstance("i-1", "desk", "main")), learned)
    }
}
