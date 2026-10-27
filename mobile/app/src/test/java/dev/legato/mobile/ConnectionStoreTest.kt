package dev.legato.mobile

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertThrows
import org.junit.Test

private class MemoryText : ConnectionStore.Text {
    var value: String? = null
    override fun read() = value
    override fun write(value: String) {
        this.value = value
    }
}

private class MemoryVault : TokenVault {
    val tokens = mutableMapOf<String, String>()
    override fun read(id: String) = tokens[id]
    override fun write(id: String, token: String) {
        tokens[id] = token
    }
    override fun remove(id: String) {
        tokens.remove(id)
    }
}

class ConnectionStoreTest {
    private val text = MemoryText()
    private val vault = MemoryVault()
    private val store = ConnectionStore(text, vault)
    private val desk = Connection(id = "1", name = "Desktop", baseUrl = "http://10.0.0.2:3080", createdAt = "2026-10-01T00:00:00Z")

    @Test
    fun `starts with no servers`() {
        assertEquals(emptyList<Connection>(), store.load())
    }

    @Test
    fun `keeps a server and its token`() {
        store.save(desk, "s3cret")

        assertEquals(listOf(desk), ConnectionStore(text, vault).load())
        assertEquals("s3cret", store.token("1"))
    }

    @Test
    fun `pairing again with a known server replaces it in place`() {
        store.save(desk, "old")

        store.save(desk.copy(id = "2", name = "Desk again", createdAt = "2026-10-20T00:00:00Z"), "new")

        assertEquals(listOf(desk.copy(name = "Desk again")), store.load())
        assertEquals("new", store.token("1"))
    }

    @Test
    fun `renames a server and keeps its token unless a new one is given`() {
        store.save(desk, "s3cret")

        store.update("1", name = "Studio", baseUrl = "http://10.0.0.3:3080", token = " ")

        assertEquals(listOf(desk.copy(name = "Studio", baseUrl = "http://10.0.0.3:3080")), store.load())
        assertEquals("s3cret", store.token("1"))
        store.update("1", name = "Studio", baseUrl = "http://10.0.0.3:3080", token = "fresh")
        assertEquals("fresh", store.token("1"))
    }

    @Test
    fun `refuses to give two servers the same address`() {
        store.save(desk, "a")
        store.save(desk.copy(id = "2", baseUrl = "http://10.0.0.9:3080"), "b")

        assertThrows(IllegalArgumentException::class.java) {
            store.update("2", name = "Other", baseUrl = desk.baseUrl, token = null)
        }
    }

    @Test
    fun `forgets a server and its token`() {
        store.save(desk, "s3cret")

        store.remove("1")

        assertEquals(emptyList<Connection>(), store.load())
        assertNull(store.token("1"))
    }

    @Test
    fun `reads the servers the Expo app saved`() {
        val saved = """[{"id":"a1","name":"Desktop","baseUrl":"http://192.168.1.20:3080","createdAt":"2026-07-26T09:00:00.000Z",""" +
            """"lastConnectedAt":"2026-10-11T04:00:00.000Z","serverVersion":"v1.2","compatibility":"compatible",""" +
            """"caCertificate":"-----BEGIN CERTIFICATE-----\nX\n-----END CERTIFICATE-----"}]"""

        store.adoptLegacy { saved }

        assertEquals(
            listOf(
                Connection(
                    id = "a1", name = "Desktop", baseUrl = "http://192.168.1.20:3080", createdAt = "2026-07-26T09:00:00.000Z",
                    serverVersion = "v1.2", caCertificate = "-----BEGIN CERTIFICATE-----\nX\n-----END CERTIFICATE-----",
                ),
            ),
            store.load(),
        )
    }

    @Test
    fun `takes the Expo app's servers only once`() {
        store.save(desk, "s3cret")
        var asked = false

        store.adoptLegacy { asked = true; "[]" }

        assertEquals(false, asked)
        assertEquals(listOf(desk), store.load())
    }

    @Test
    fun `skips saved entries it cannot read`() {
        text.value = """[{"id":"a1","name":"Ok","baseUrl":"http://x:1","createdAt":"t"},{"name":"no id"},7]"""

        assertEquals(listOf("a1"), store.load().map { it.id })
    }
}
