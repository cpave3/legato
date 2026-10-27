package dev.legato.mobile

import org.junit.Assert.assertEquals
import org.junit.Assert.assertThrows
import org.junit.Test

class ServerAddressTest {
    @Test
    fun `normalizes HTTP and HTTPS origins`() {
        assertEquals("https://desktop.example", normalizeServerUrl(" https://desktop.example/ "))
        assertEquals("http://192.168.1.20:3080", normalizeServerUrl(" http://192.168.1.20:3080/ "))
        assertThrows(IllegalArgumentException::class.java) { normalizeServerUrl("https://desktop.example/path") }
    }
}

class PairingCodeTest {
    @Test
    fun `reads an encoded pairing code`() {
        assertEquals(
            PairingCode(baseUrl = "https://mac.example:3080", token = "a b", suggestedName = "mac.example", caCertificate = null),
            parsePairUri("legato://pair?url=https%3A%2F%2Fmac.example%3A3080&token=a%20b"),
        )
    }

    @Test
    fun `carries the CA certificate the server offers`() {
        val pem = "-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n"
        val code = parsePairUri("legato://pair?url=http%3A%2F%2F10.0.0.2%3A3080&token=t&ca=" + java.net.URLEncoder.encode(pem, "UTF-8"))
        assertEquals(pem, code.caCertificate)
    }

    @Test
    fun `refuses what is not a pairing code`() {
        for (bad in listOf(
            "https://pair?url=https://x&token=y",
            "legato://pair?url=https://x&url=https://y&token=z",
            "legato://pair?url=https%3A%2F%2Fx&token=%20",
            "legato://pair?url=https%3A%2F%2Fx&token=t&ca=nonsense",
        )) {
            assertThrows(bad, IllegalArgumentException::class.java) { parsePairUri(bad) }
        }
    }
}
