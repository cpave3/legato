package dev.legato.mobile

import org.junit.Assert.assertEquals
import org.junit.Test

class DiscoveryTest {
    @Test
    fun aFoundServerIsAddressedByItsSchemeHostAndPort() {
        assertEquals("http://192.168.1.200:3080", discoveredBaseUrl(null, "192.168.1.200", 3080))
        assertEquals("https://192.168.1.200:3080", discoveredBaseUrl("https", "192.168.1.200", 3080))
        assertEquals("http://[fe80::1]:3080", discoveredBaseUrl("http", "fe80::1%wlan0", 3080))
    }

    @Test
    fun aDefaultPortIsLeftOut() {
        assertEquals("https://192.168.1.200", discoveredBaseUrl("https", "192.168.1.200", 443))
    }
}
