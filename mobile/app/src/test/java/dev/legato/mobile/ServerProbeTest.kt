package dev.legato.mobile

import org.junit.Assert.assertEquals
import org.junit.Assert.assertThrows
import org.junit.Test

class ServerProbeTest {
    @Test
    fun `a server that answers health is online, with its version when it gives one`() {
        assertEquals(Health(online = true, version = "v1.4.0"), readHealth(200, """{"status":"ok","version":"v1.4.0"}"""))
        assertEquals(Health(online = true, version = null), readHealth(200, """{}"""))
        assertEquals(Health(online = false, version = null), readHealth(200, """{"status":"starting"}"""))
        assertEquals(Health(online = false, version = null), readHealth(503, """{"status":"ok"}"""))
        assertEquals(Health(online = false, version = null), readHealth(200, "<html>"))
    }

    @Test
    fun `says whether this app can open the server`() {
        assertEquals(ServerMeta(Compatibility.COMPATIBLE, "v1.4.0"), readMeta(200, """{"version":"v1.4.0","mobile_shell_api":1}"""))
        assertEquals(ServerMeta(Compatibility.UNSUPPORTED, "v9"), readMeta(200, """{"version":"v9","mobile_shell_api":2}"""))
        // A server from before the meta endpoint.
        assertEquals(ServerMeta(Compatibility.LEGACY, null), readMeta(404, ""))
        assertThrows(IllegalStateException::class.java) { readMeta(500, "") }
    }
    @Test fun `metadata exposes theme snapshot and distinguishes older servers from malformed snapshots`() {
        assertEquals(ThemePreference("ocular-light", 8), readMeta(200,
            """{"mobile_shell_api":1,"theme_preference":{"id":"ocular-light","revision":8}}""").themePreference)
        assertEquals(ThemePreference("", 9), readMeta(200,
            """{"mobile_shell_api":1,"theme_preference":{"id":"","revision":9}}""").themePreference)
        assertEquals(ThemePreference("future", 10), readMeta(200,
            """{"mobile_shell_api":1,"theme_preference":{"id":"future","revision":10}}""").themePreference)
        assertEquals(null, readMeta(200, """{"mobile_shell_api":1}""").themePreference)
        for (preference in listOf("null", "{}", "{\"id\":3,\"revision\":1}",
            "{\"id\":\"legato\",\"revision\":-1}", "{\"id\":\"legato\",\"revision\":1.5}")) {
            assertThrows(IllegalStateException::class.java) {
                readMeta(200, """{"mobile_shell_api":1,"theme_preference":$preference}""")
            }
        }
    }

}
