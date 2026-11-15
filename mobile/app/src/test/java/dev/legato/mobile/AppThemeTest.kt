package dev.legato.mobile

import org.junit.Assert.*
import org.junit.Test

class AppThemeTest {
    private fun catalog() = ThemeCatalog(javaClass.getResource("/palettes.json")!!.readText())

    @Test
    fun `choosing a canonical theme survives a new settings owner`() {
        var saved: String? = null
        val settings = ThemeSettings(catalog(), { saved }, { saved = it; true })
        assertEquals("legato", settings.current.id)
        assertTrue(settings.select("ocular-light"))
        val restarted = ThemeSettings(catalog(), { saved }, { saved = it; true })
        assertEquals("ocular-light", restarted.current.id)
        assertTrue(restarted.current.light)
        assertEquals("#F5F3ED", restarted.current.tokens["canvas"])
        assertEquals("#252B33", restarted.current.tokens["text"])
        assertFalse(restarted.select("unknown"))
        assertEquals("ocular-light", restarted.current.id)
    }
    @Test
    fun `keyed page requests save before delivery and acknowledge storage failures`() {
        var saved: String? = null
        var canSave = true
        var writeError: Exception? = null
        val events = mutableListOf<String>()
        val settings = ThemeSettings(catalog(), { saved }, {
            events += "save:$it"
            writeError?.let { throw it }
            if (canSave) saved = it
            canSave
        })
        val replies = mutableListOf<Pair<String, String?>>()
        val bridge = ThemeBridge("secret", { id, done ->
            val result = runCatching {
                check(settings.select(id)) { "Invalid theme" }
                events += "apply:${settings.current.id}"
            }
            done(result.exceptionOrNull()?.message)
        }, { id, error ->
            events += "reply:$id"
            replies += id to error
        })
        fun request(key: String, id: String) = bridge.handle(PageMessage.parse(
            """{"legatoShell":"theme","type":"theme","key":"$key","id":"$id"}"""))
        request("wrong", "ocular-light")
        for (data in listOf(
            """{"legatoShell":"theme","id":"ocular-light"}""",
            """{"legatoShell":"theme","key":"secret","id":7}""",
            """{"legatoShell":"theme","key":"secret","theme":"dracula"}""",
        )) bridge.handle(PageMessage.parse(data))
        assertTrue(events.isEmpty())
        request("secret", "dracula")
        assertEquals(listOf("save:dracula", "apply:dracula", "reply:dracula"), events)
        assertEquals("dracula" to null, replies.last())
        assertEquals("dracula", ThemeSettings(catalog(), { saved }, { true }).current.id)
        events.clear()
        request("secret", "arbitrary")
        assertEquals(listOf("reply:arbitrary"), events)
        assertNotNull(replies.last().second)
        events.clear()
        canSave = false
        request("secret", "ocular-light")
        assertEquals(listOf("save:ocular-light", "reply:ocular-light"), events)
        assertNotNull(replies.last().second)
        assertEquals("dracula", settings.current.id)
        assertEquals("dracula", saved)
        // A repeated request must still confirm persistence, not assume success.
        request("secret", "dracula")
        assertNotNull(replies.last().second)
        events.clear()
        writeError = java.io.IOException("Storage unavailable")
        request("secret", "ocular-dark")
        assertEquals(listOf("save:ocular-dark", "reply:ocular-dark"), events)
        assertEquals("ocular-dark" to "Storage unavailable", replies.last())
        assertEquals("dracula", settings.current.id)
    }

    @Test
    fun `every picker choice resolves the packaged canonical tokens and polarity`() {
        val json = javaClass.getResource("/palettes.json")!!.readText()
        val entries = org.json.JSONObject(json).getJSONArray("themes")
        val settings = ThemeSettings(ThemeCatalog(json), { "retired-theme" }, { true })
        assertEquals("legato", settings.current.id)
        assertFalse(settings.current.light)
        assertTrue(settings.current.tokens.isEmpty())
        assertEquals(setOf("legato", "dracula", "ocular-light", "ocular-dark"), catalog().themes.map { it.id }.toSet())
        for (i in 0 until entries.length()) {
            val entry = entries.getJSONObject(i)
            assertTrue(settings.select(entry.getString("id")))
            assertEquals(entry.getString("polarity") == "light", settings.current.light)
            val tokens = entry.getJSONObject("tokens")
            val expected = tokens.keys().asSequence().associateWith { tokens.getString(it) }.toMutableMap()
            if (entry.getString("id") == "dracula") {
                expected["muted"] = tokens.getString("text")
                expected["comment"] = tokens.getString("text")
                expected["border"] = tokens.getString("focus")
            }
            assertEquals(expected, settings.current.tokens)
            assertEquals(tokens.getString("canvas"), settings.current.pageBackground)
        }
        assertTrue(settings.select("legato"))
        assertTrue(settings.current.tokens.isEmpty())
        assertEquals("#0A0A0F", settings.current.pageBackground)
    }
}
