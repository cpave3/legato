package dev.legato.mobile

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class ApprovalLinkTest {
    private fun server(id: String, url: String) = Connection(id = id, name = id, baseUrl = url, createdAt = "2026-10-29")

    @Test
    fun `reads the server and the approval a notification opens`() {
        assertEquals(
            ApprovalLink(baseUrl = "http://192.168.1.20:3080", id = "ap-1234abcd"),
            parseApprovalUri("legato://approval?url=http%3A%2F%2F192.168.1.20%3A3080%2F&id=ap-1234abcd"),
        )
    }

    @Test
    fun `reads nothing that is not an approval link`() {
        assertNull(parseApprovalUri("legato://pair?url=http%3A%2F%2Fx&token=t"))
        assertNull(parseApprovalUri("legato://approval?url=http%3A%2F%2Fx"))
        assertNull(parseApprovalUri("legato://approval?id=ap-1"))
        assertNull(parseApprovalUri("legato://approval?url=http%3A%2F%2Fx&id=..%2Fsettings"))
    }

    @Test
    fun `opens the saved server at that address, or the only one saved`() {
        val desk = server("desk", "http://192.168.1.20:3080")
        val vm = server("vm", "https://legato.example.net")
        val link = ApprovalLink("http://192.168.1.20:3080", "ap-1")

        assertEquals(desk, serverForApproval(listOf(vm, desk), link))
        assertEquals(vm, serverForApproval(listOf(vm), link))
        assertNull(serverForApproval(listOf(vm, server("other", "http://10.0.0.5:3080")), link))
        assertNull(serverForApproval(emptyList(), link))
    }

    @Test
    fun `the page opens on the approval`() {
        assertEquals("/?approval=ap-1234abcd", ApprovalLink("http://x", "ap-1234abcd").path)
    }
}
